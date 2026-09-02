#!/usr/bin/env python3
"""Freeze, verify, and compare deterministic Git review scope manifests."""

from __future__ import annotations

import argparse
import hashlib
import json
import os
import re
import stat
import subprocess
import sys
from pathlib import Path, PurePosixPath
from typing import Any


SCHEMA_VERSION = 1
EXIT_INPUT = 2
EXIT_STALE = 3
CHANGE_RE = re.compile(r"^[A-Za-z0-9][A-Za-z0-9._-]*$")
SHA256_RE = re.compile(r"^[0-9a-f]{64}$")
COMMIT_SHA_RE = re.compile(r"^(?:[0-9a-f]{40}|[0-9a-f]{64})$")
GIT_MODE_RE = re.compile(r"^(?:100644|100755|120000)$")
WORKTREE_MODE_RE = re.compile(r"^[0-7]{4}$")


class InputError(Exception):
    """A safe, user-correctable command input error."""


def _error(message: str) -> None:
    clean = " ".join(str(message).splitlines())
    print(f"ERROR\t{clean}", file=sys.stderr)


def _git_environment() -> dict[str, str]:
    environment = os.environ.copy()
    environment["GIT_OPTIONAL_LOCKS"] = "0"
    return environment


def _git(repo: Path, *args: str, text: bool = False) -> bytes | str:
    result = subprocess.run(
        ["git", "--literal-pathspecs", "-C", str(repo), *args],
        env=_git_environment(),
        stdout=subprocess.PIPE,
        stderr=subprocess.PIPE,
        check=False,
        text=text,
    )
    if result.returncode:
        stderr = result.stderr if text else result.stderr.decode("utf-8", "replace")
        raise InputError(f"Git command failed: {stderr.strip() or 'unknown error'}")
    return result.stdout


def _canonical_bytes(value: Any) -> bytes:
    return json.dumps(
        value, ensure_ascii=False, sort_keys=True, separators=(",", ":")
    ).encode("utf-8")


def _manifest_id(payload: dict[str, Any]) -> str:
    return hashlib.sha256(_canonical_bytes(payload)).hexdigest()


def _inside(child: Path, parent: Path) -> bool:
    try:
        child.relative_to(parent)
        return True
    except ValueError:
        return False


def _validate_change(change: Any) -> str:
    if not isinstance(change, str) or not CHANGE_RE.fullmatch(change) or change in {".", ".."}:
        raise InputError("change must be a safe single path component")
    return change


def _workspace(path: str | Path) -> Path:
    workspace = Path(path).resolve()
    if not workspace.is_dir():
        raise InputError("workspace must be an existing directory")
    return workspace


def _repo(path: str | Path, workspace: Path) -> Path:
    repo = Path(path).resolve()
    if not repo.is_dir() or not _inside(repo, workspace):
        raise InputError("Git repository must be an existing directory inside workspace")
    try:
        top = Path(str(_git(repo, "rev-parse", "--show-toplevel", text=True)).strip()).resolve()
    except InputError as error:
        raise InputError(f"invalid Git repository: {repo}") from error
    if top != repo:
        raise InputError(f"Git repository path is not a toplevel: {repo}")
    return repo


def _repo_spec(raw: str, workspace: Path) -> tuple[Path, str]:
    if "::" not in raw:
        raise InputError("repo-spec must use PATH::BASE")
    raw_path, base = raw.rsplit("::", 1)
    if not raw_path or not base or any(character in base for character in "\r\n\0"):
        raise InputError("repo-spec requires a repository and base")
    return _repo(raw_path, workspace), base


def _safe_git_path(raw: bytes) -> str:
    path = raw.decode("utf-8", "surrogateescape")
    pure = PurePosixPath(path)
    if not path or pure.is_absolute() or ".." in pure.parts or any(c in path for c in "\0\r\n"):
        raise InputError("Git returned an unsafe path")
    return path


def _name_status(repo: Path, *args: str) -> list[dict[str, Any]]:
    raw = bytes(_git(repo, *args, "--name-status", "-z", "--find-renames"))
    fields = raw.split(b"\0")
    if fields and fields[-1] == b"":
        fields.pop()
    entries: list[dict[str, Any]] = []
    index = 0
    while index < len(fields):
        status = fields[index].decode("ascii", "strict")
        index += 1
        path_count = 2 if status[:1] in {"R", "C"} else 1
        if index + path_count > len(fields):
            raise InputError("Git returned malformed name-status output")
        paths = [_safe_git_path(item) for item in fields[index:index + path_count]]
        index += path_count
        entries.append({"status": status, "paths": paths})
    return sorted(entries, key=lambda item: (item["paths"], item["status"]))


def _untracked(repo: Path) -> list[str]:
    raw = bytes(_git(repo, "ls-files", "-z", "--others", "--exclude-standard"))
    paths = sorted(_safe_git_path(item) for item in raw.split(b"\0") if item)
    for path in paths:
        if (repo / path).is_dir():
            raise InputError(
                f"untracked directory or nested Git repository must be ignored "
                f"by the parent and frozen as an independent repo-spec: {path}"
            )
    return paths


def _worktree_status_paths(repo: Path) -> set[str]:
    raw = bytes(
        _git(
            repo,
            "status",
            "--porcelain=v1",
            "-z",
            "--untracked-files=no",
        )
    )
    fields = raw.split(b"\0")
    if fields and fields[-1] == b"":
        fields.pop()
    changed: set[str] = set()
    index = 0
    while index < len(fields):
        record = fields[index]
        index += 1
        if len(record) < 4 or record[2:3] != b" ":
            raise InputError("Git returned malformed porcelain status output")
        try:
            index_status = chr(record[0])
            worktree_status = chr(record[1])
        except ValueError as error:
            raise InputError("Git returned malformed porcelain status output") from error
        paths = [_safe_git_path(record[3:])]
        if index_status in {"R", "C"} or worktree_status in {"R", "C"}:
            if index >= len(fields):
                raise InputError("Git returned malformed porcelain rename output")
            paths.append(_safe_git_path(fields[index]))
            index += 1
        if worktree_status != " ":
            changed.update(paths)
    return changed


def _unstaged(repo: Path) -> list[dict[str, Any]]:
    changed = _worktree_status_paths(repo)
    return [
        entry
        for entry in _name_status(repo, "diff-files")
        if any(path in changed for path in entry["paths"])
    ]


def _sha(data: bytes) -> str:
    return hashlib.sha256(data).hexdigest()


def _git_entry(repo: Path, revision: str, path: str) -> tuple[str, str] | None:
    if revision:
        raw = bytes(_git(repo, "ls-tree", "-z", revision, "--", path))
        records = [record for record in raw.split(b"\0") if record]
        if not records:
            return None
        if len(records) != 1 or b"\t" not in records[0]:
            raise InputError(f"cannot resolve Git identity for path: {path}")
        metadata = records[0].split(b"\t", 1)[0].split()
        if len(metadata) != 3:
            raise InputError(f"malformed Git tree identity for path: {path}")
        return (
            metadata[0].decode("ascii", "strict"),
            metadata[2].decode("ascii", "strict"),
        )

    raw = bytes(_git(repo, "ls-files", "-s", "-z", "--", path))
    records = [record for record in raw.split(b"\0") if record]
    if not records:
        return None
    stage_zero: list[tuple[str, str]] = []
    for record in records:
        if b"\t" not in record:
            raise InputError(f"malformed Git index identity for path: {path}")
        metadata = record.split(b"\t", 1)[0].split()
        if len(metadata) != 3:
            raise InputError(f"malformed Git index identity for path: {path}")
        if metadata[2] == b"0":
            stage_zero.append(
                (
                    metadata[0].decode("ascii", "strict"),
                    metadata[1].decode("ascii", "strict"),
                )
            )
    if len(stage_zero) != 1:
        raise InputError(f"unmerged Git index path cannot be frozen: {path}")
    return stage_zero[0]


def _git_layer(repo: Path, revision: str, path: str) -> dict[str, Any]:
    entry = _git_entry(repo, revision, path)
    if entry is None:
        return {"kind": "missing", "sha256": None, "mode": None}
    mode, object_id = entry
    if mode == "160000":
        return {
            "kind": "gitlink",
            "sha256": _sha(object_id.encode("ascii")),
            "mode": mode,
        }
    content = bytes(_git(repo, "cat-file", "blob", object_id))
    return {
        "kind": "git-blob",
        "sha256": _sha(content),
        "mode": mode,
    }


def _directory_flags() -> int:
    return (
        os.O_RDONLY
        | os.O_DIRECTORY
        | os.O_NOFOLLOW
        | getattr(os, "O_CLOEXEC", 0)
    )


def _open_gitlink_directory(root_fd: int, path: str) -> int | None:
    current_fd = os.dup(root_fd)
    try:
        for component in PurePosixPath(path).parts:
            try:
                next_fd = os.open(
                    component,
                    _directory_flags(),
                    dir_fd=current_fd,
                )
            except FileNotFoundError:
                return None
            except OSError as error:
                raise InputError(
                    f"gitlink path component is not a stable directory: {path}"
                ) from error
            os.close(current_fd)
            current_fd = next_fd
        result_fd = current_fd
        current_fd = -1
        return result_fd
    finally:
        if current_fd >= 0:
            os.close(current_fd)


def _same_directory(left_fd: int, right_fd: int) -> bool:
    left = os.fstat(left_fd)
    right = os.fstat(right_fd)
    return (left.st_dev, left.st_ino) == (right.st_dev, right.st_ino)


def _verify_gitlink_binding(
    root_fd: int, path: str, bound_fd: int | None,
) -> None:
    try:
        reopened_fd = _open_gitlink_directory(root_fd, path)
    except InputError as error:
        raise InputError(
            f"gitlink path changed or replacement detected: {path}"
        ) from error
    try:
        if bound_fd is None:
            if reopened_fd is not None:
                raise InputError(
                    f"gitlink path changed or replacement detected: {path}"
                )
        elif reopened_fd is None or not _same_directory(bound_fd, reopened_fd):
            raise InputError(
                f"gitlink path changed or replacement detected: {path}"
            )
    finally:
        if reopened_fd is not None:
            os.close(reopened_fd)


def _run_git_fd(
    directory_fd: int, *args: str, text: bool = False,
) -> subprocess.CompletedProcess[Any]:
    return subprocess.run(
        [
            "git", "--literal-pathspecs",
            "-C", f"/proc/self/fd/{directory_fd}",
            *args,
        ],
        stdout=subprocess.PIPE,
        stderr=subprocess.PIPE,
        check=False,
        text=text,
        pass_fds=(directory_fd,),
        env=_git_environment(),
    )


def _git_fd(directory_fd: int, *args: str, text: bool = False) -> bytes | str:
    result = _run_git_fd(directory_fd, *args, text=text)
    if result.returncode:
        stderr = result.stderr if text else result.stderr.decode("utf-8", "replace")
        detail = stderr.strip() or "unknown error"
        raise InputError(f"Git command failed: {detail}")
    return result.stdout


def _uninitialized_gitlink_identity(
    root_fd: int,
    path: str,
    directory_fd: int | None,
    marker: dict[str, Any],
) -> dict[str, Any]:
    if directory_fd is not None:
        try:
            entries = os.listdir(directory_fd)
        except OSError as error:
            raise InputError(
                f"cannot inspect uninitialized gitlink directory: {path}"
            ) from error
        if entries:
            raise InputError(
                f"non-empty uninitialized gitlink directory: {path}"
            )
    _verify_gitlink_binding(root_fd, path, directory_fd)
    return {
        **marker,
        "sha256": _sha(b"gitlink\0uninitialized"),
    }


def _gitlink_worktree(
    repo: Path, path: str, declared_repos: set[str],
) -> dict[str, Any]:
    marker = {"kind": "gitlink", "mode": "160000"}
    try:
        root_fd = os.open(repo, _directory_flags())
    except OSError as error:
        raise InputError(f"cannot bind parent repository directory: {repo}") from error
    try:
        directory_fd = _open_gitlink_directory(root_fd, path)
        if directory_fd is None:
            return _uninitialized_gitlink_identity(
                root_fd, path, directory_fd, marker
            )
        try:
            probe = _run_git_fd(
                directory_fd, "rev-parse", "--show-prefix", text=True
            )
            if probe.returncode or probe.stdout.strip():
                return _uninitialized_gitlink_identity(
                    root_fd, path, directory_fd, marker
                )
            head = str(
                _git_fd(
                    directory_fd,
                    "rev-parse",
                    "--verify",
                    "HEAD^{commit}",
                    text=True,
                )
            ).strip()
            dirty = bytes(
                _git_fd(
                    directory_fd,
                    "status",
                    "--porcelain=v1",
                    "-z",
                    "--untracked-files=all",
                )
            )
            _verify_gitlink_binding(root_fd, path, directory_fd)
            declared_path = str(repo.joinpath(*PurePosixPath(path).parts))
            if dirty and declared_path not in declared_repos:
                raise InputError(
                    f"dirty gitlink requires an explicit repo-spec: {declared_path}"
                )
            return {
                **marker,
                "sha256": _sha(
                    b"gitlink\0initialized\0" + head.encode("ascii")
                ),
            }
        finally:
            os.close(directory_fd)
    finally:
        os.close(root_fd)


def _worktree_layer(repo: Path, path: str) -> dict[str, Any]:
    target = repo / path
    try:
        metadata = os.lstat(target)
    except FileNotFoundError:
        return {"kind": "missing", "sha256": None, "mode": None}
    except OSError as error:
        raise InputError(f"cannot stat worktree path {path}: {error}") from error

    mode = f"{stat.S_IMODE(metadata.st_mode):04o}"
    try:
        if stat.S_ISLNK(metadata.st_mode):
            link_bytes = os.readlink(target).encode("utf-8", "surrogateescape")
            return {"kind": "symlink", "sha256": _sha(link_bytes), "mode": mode}
        if stat.S_ISREG(metadata.st_mode):
            return {"kind": "file", "sha256": _sha(target.read_bytes()), "mode": mode}
        if stat.S_ISDIR(metadata.st_mode):
            raise InputError(
                f"untracked directory or nested Git repository is unsupported: {path}"
            )
        raise InputError(f"unsupported worktree file type: {path}")
    except OSError as error:
        raise InputError(f"cannot hash worktree path {path}: {error}") from error


def _file_identity(
    repo: Path, merge_base: str, head: str, path: str,
    declared_repos: set[str],
) -> dict[str, Any]:
    layers = {
        "base": _git_layer(repo, merge_base, path),
        "head": _git_layer(repo, head, path),
        "index": _git_layer(repo, "", path),
    }
    if any(layer["kind"] == "gitlink" for layer in layers.values()):
        worktree = _gitlink_worktree(repo, path, declared_repos)
    else:
        worktree = _worktree_layer(repo, path)
    return {
        "kind": worktree["kind"],
        "sha256": worktree["sha256"],
        "mode": worktree["mode"],
        **layers,
    }

def _snapshot_repository(
    repo: Path, base_input: str, declared_repos: set[str],
) -> dict[str, Any]:
    try:
        base_resolved = str(
            _git(
                repo, "rev-parse", "--verify",
                f"{base_input}^{{commit}}", text=True,
            )
        ).strip()
    except InputError as error:
        raise InputError(f"base does not resolve to a commit: {base_input}") from error
    head = str(_git(repo, "rev-parse", "--verify", "HEAD^{commit}", text=True)).strip()
    merge_base = str(_git(repo, "merge-base", base_resolved, head, text=True)).strip()
    committed = _name_status(repo, "diff", merge_base, head)
    staged = _name_status(repo, "diff", "--cached", head)
    unstaged = _unstaged(repo)
    untracked = _untracked(repo)
    paths = set(untracked)
    for entries in (committed, staged, unstaged):
        for entry in entries:
            paths.update(entry["paths"])
    files = {
        path: _file_identity(repo, merge_base, head, path, declared_repos)
        for path in sorted(paths)
    }
    return {
        "path": str(repo),
        "base": {"input": base_input, "resolved": base_resolved},
        "merge_base": merge_base,
        "head": head,
        "committed": committed,
        "staged": staged,
        "unstaged": unstaged,
        "untracked": untracked,
        "files": files,
    }


def _payload(change: str, workspace: Path, specs: list[tuple[Path, str]]) -> dict[str, Any]:
    paths = [str(repo) for repo, _ in specs]
    if len(paths) != len(set(paths)):
        raise InputError("duplicate repository in repo-spec")
    declared_repos = set(paths)
    repositories = [
        _snapshot_repository(repo, base, declared_repos)
        for repo, base in specs
    ]
    repositories.sort(key=lambda item: item["path"])
    return {
        "schema_version": SCHEMA_VERSION,
        "change": change,
        "workspace": str(workspace),
        "repositories": repositories,
    }


def _output_path(raw: str | Path, workspace: Path, change: str) -> Path:
    output = Path(raw)
    if not output.is_absolute():
        output = workspace / output
    output = output.resolve()
    review_root = (workspace / ".ai-local" / "reviews" / change).resolve()
    local_root = (workspace / ".ai-local").resolve()
    if not _inside(local_root, workspace) or not _inside(review_root, local_root):
        raise InputError(".ai-local/reviews path escapes workspace")
    if output.parent != review_root or output.suffix != ".json":
        raise InputError(f"output must be a JSON file directly under .ai-local/reviews/{change}")
    return output


def freeze(args: argparse.Namespace) -> int:
    change = _validate_change(args.change)
    workspace = _workspace(args.workspace)
    specs = [_repo_spec(raw, workspace) for raw in args.repo_spec]
    if not specs:
        raise InputError("at least one repo-spec is required")
    output = _output_path(args.output, workspace, change)
    payload = _payload(change, workspace, specs)
    _validate_payload(payload)
    manifest = dict(payload)
    manifest["id"] = _manifest_id(payload)
    output.parent.mkdir(parents=True, exist_ok=True)
    rendered = json.dumps(manifest, ensure_ascii=False, sort_keys=True, indent=2) + "\n"
    output.write_text(rendered, encoding="utf-8")
    print(f"FROZEN {manifest['id']} {output}")
    return 0


def _require_keys(value: Any, expected: set[str], label: str) -> dict[str, Any]:
    if not isinstance(value, dict):
        raise InputError(f"{label} must be an object")
    actual = set(value)
    missing = sorted(expected - actual)
    unknown = sorted(actual - expected)
    if missing:
        raise InputError(f"{label} missing field: {missing[0]}")
    if unknown:
        raise InputError(f"{label} has unknown field: {unknown[0]}")
    return value


def _required_string(value: Any, label: str) -> str:
    if (
        not isinstance(value, str)
        or not value
        or any(character in value for character in "\0\r\n")
    ):
        raise InputError(f"{label} must be a non-empty single-line string")
    return value


def _absolute_path(value: Any, label: str) -> str:
    raw = _required_string(value, label)
    path = Path(raw)
    if not path.is_absolute() or str(path) != raw or ".." in path.parts:
        raise InputError(f"{label} must be an absolute normalized path")
    return raw


def _relative_path(value: Any, label: str) -> str:
    raw = _required_string(value, label)
    path = PurePosixPath(raw)
    if (
        path.is_absolute()
        or ".." in path.parts
        or str(path) != raw
        or any(character in raw for character in "\t")
    ):
        raise InputError(f"{label} must be a safe relative path")
    return raw


def _commit_sha(value: Any, label: str) -> str:
    if not isinstance(value, str) or not COMMIT_SHA_RE.fullmatch(value):
        raise InputError(f"{label} must be a commit SHA")
    return value


def _sha256(value: Any, label: str, *, allow_none: bool) -> str | None:
    if value is None and allow_none:
        return None
    if not isinstance(value, str) or not SHA256_RE.fullmatch(value):
        raise InputError(f"{label} must be a SHA-256")
    return value


def _validate_layer(value: Any, label: str) -> None:
    layer = _require_keys(value, {"kind", "sha256", "mode"}, label)
    kind = layer["kind"]
    if (
        not isinstance(kind, str)
        or kind not in {"missing", "git-blob", "gitlink"}
    ):
        raise InputError(f"{label} kind is invalid")
    _sha256(layer["sha256"], f"{label} sha256", allow_none=kind == "missing")
    mode = layer["mode"]
    if kind == "git-blob":
        if layer["sha256"] is None:
            raise InputError(f"{label} git-blob requires sha256")
        if not isinstance(mode, str) or not GIT_MODE_RE.fullmatch(mode):
            raise InputError(f"{label} mode must be a Git file mode")
    elif kind == "gitlink":
        if layer["sha256"] is None:
            raise InputError(f"{label} gitlink requires sha256")
        if mode != "160000":
            raise InputError(f"{label} gitlink mode must be 160000")
    else:
        if layer["sha256"] is not None:
            raise InputError(f"{label} missing identity cannot have sha256")
        if mode is not None:
            raise InputError(f"{label} missing identity cannot have mode")


def _validate_identity(value: Any, label: str) -> None:
    identity = _require_keys(
        value, {"kind", "sha256", "mode", "base", "head", "index"}, label
    )
    kind = identity["kind"]
    if (
        not isinstance(kind, str)
        or kind not in {"missing", "file", "symlink", "gitlink"}
    ):
        raise InputError(f"{label} worktree kind is invalid")
    _sha256(identity["sha256"], f"{label} sha256", allow_none=kind == "missing")
    mode = identity["mode"]
    if kind == "missing":
        if identity["sha256"] is not None:
            raise InputError(f"{label} missing identity cannot have sha256")
        if mode is not None:
            raise InputError(f"{label} missing identity cannot have mode")
    elif kind == "gitlink":
        if identity["sha256"] is None:
            raise InputError(f"{label} gitlink requires sha256")
        if mode != "160000":
            raise InputError(f"{label} gitlink mode must be 160000")
    else:
        if identity["sha256"] is None:
            raise InputError(f"{label} {kind} requires sha256")
        if not isinstance(mode, str) or not WORKTREE_MODE_RE.fullmatch(mode):
            raise InputError(f"{label} mode must be a deterministic lstat mode")
    for layer in ("base", "head", "index"):
        _validate_layer(identity[layer], f"{label} {layer}")

def _validate_status_entry(value: Any, label: str) -> list[str]:
    entry = _require_keys(value, {"status", "paths"}, label)
    status = _required_string(entry["status"], f"{label} status")
    paths = entry["paths"]
    if not isinstance(paths, list):
        raise InputError(f"{label} paths must be a list")
    rename_or_copy = status[:1] in {"R", "C"}
    if rename_or_copy:
        score = status[1:]
        if not score.isdigit() or not 0 <= int(score) <= 100 or len(paths) != 2:
            raise InputError(f"{label} rename/copy status requires score and two paths")
    elif status not in {"A", "D", "M", "T", "U", "X", "B"} or len(paths) != 1:
        raise InputError(f"{label} status or path count is invalid")
    checked = [
        _relative_path(path, f"{label} path")
        for path in paths
    ]
    if len(checked) != len(set(checked)):
        raise InputError(f"{label} contains duplicate paths")
    return checked


def _validate_repository(value: Any, workspace: str, index: int) -> str:
    label = f"repository[{index}]"
    repo = _require_keys(
        value,
        {
            "path", "base", "merge_base", "head", "committed", "staged",
            "unstaged", "untracked", "files",
        },
        label,
    )
    path = _absolute_path(repo["path"], f"{label} path")
    try:
        Path(path).relative_to(Path(workspace))
    except ValueError as error:
        raise InputError(f"{label} path must be inside workspace") from error
    base = _require_keys(repo["base"], {"input", "resolved"}, f"{label} base")
    _required_string(base["input"], f"{label} base input")
    _commit_sha(base["resolved"], f"{label} base resolved")
    _commit_sha(repo["merge_base"], f"{label} merge_base")
    _commit_sha(repo["head"], f"{label} head")

    scope_paths: set[str] = set()
    for scope in ("committed", "staged", "unstaged"):
        entries = repo[scope]
        if not isinstance(entries, list):
            raise InputError(f"{label} {scope} must be a list")
        normalized = []
        for entry_index, entry in enumerate(entries):
            checked = _validate_status_entry(
                entry, f"{label} {scope}[{entry_index}]"
            )
            scope_paths.update(checked)
            normalized.append(entry)
        if normalized != sorted(
            normalized, key=lambda item: (item["paths"], item["status"])
        ):
            raise InputError(f"{label} {scope} must be stably sorted")
        serialized = [_canonical_bytes(entry) for entry in normalized]
        if len(serialized) != len(set(serialized)):
            raise InputError(f"{label} {scope} contains duplicate entries")

    untracked = repo["untracked"]
    if not isinstance(untracked, list):
        raise InputError(f"{label} untracked must be a list")
    checked_untracked = [
        _relative_path(path_value, f"{label} untracked path")
        for path_value in untracked
    ]
    if checked_untracked != sorted(set(checked_untracked)):
        raise InputError(f"{label} untracked must be unique and sorted")
    scope_paths.update(checked_untracked)

    files = repo["files"]
    if not isinstance(files, dict):
        raise InputError(f"{label} files must be an object")
    checked_file_paths = {
        _relative_path(path_value, f"{label} files key")
        for path_value in files
    }
    if checked_file_paths != scope_paths:
        raise InputError(f"{label} files keys must equal the review scope paths")
    for path_value in sorted(files):
        _validate_identity(files[path_value], f"{label} files[{path_value}]")
    return path


def _validate_payload(payload: Any) -> None:
    root = _require_keys(
        payload, {"schema_version", "change", "workspace", "repositories"},
        "manifest payload",
    )
    if (
        type(root["schema_version"]) is not int
        or root["schema_version"] != SCHEMA_VERSION
    ):
        raise InputError("unsupported manifest schema_version")
    _validate_change(root["change"])
    workspace = _absolute_path(root["workspace"], "workspace")
    repositories = root["repositories"]
    if not isinstance(repositories, list) or not repositories:
        raise InputError("manifest repositories must be a non-empty list")
    paths = [
        _validate_repository(repo, workspace, index)
        for index, repo in enumerate(repositories)
    ]
    if len(paths) != len(set(paths)):
        raise InputError("manifest contains duplicate repository path")
    if paths != sorted(paths):
        raise InputError("manifest repositories must be stably sorted")


def _validate_manifest(manifest: Any) -> dict[str, Any]:
    root = _require_keys(
        manifest,
        {"schema_version", "change", "workspace", "repositories", "id"},
        "manifest",
    )
    manifest_id = _sha256(root["id"], "manifest id", allow_none=False)
    payload = {key: value for key, value in root.items() if key != "id"}
    _validate_payload(payload)
    if manifest_id != _manifest_id(payload):
        raise InputError("manifest id does not match canonical content")
    return root


def _read_manifest(path: str | Path) -> dict[str, Any]:
    try:
        raw = Path(path).read_bytes()
        manifest = json.loads(raw)
    except (OSError, json.JSONDecodeError) as error:
        raise InputError(f"cannot read manifest: {error}") from error
    return _validate_manifest(manifest)

def _recompute(manifest: dict[str, Any]) -> dict[str, Any]:
    workspace = _workspace(manifest.get("workspace"))
    specs: list[tuple[Path, str]] = []
    for item in manifest["repositories"]:
        if not isinstance(item, dict) or not isinstance(item.get("base"), dict):
            raise InputError("manifest repository entry is invalid")
        specs.append((_repo(item.get("path"), workspace), item["base"].get("input")))
    return _payload(manifest["change"], workspace, specs)


def verify(args: argparse.Namespace) -> int:
    manifest = _read_manifest(args.manifest)
    expected = {key: value for key, value in manifest.items() if key != "id"}
    current = _recompute(manifest)
    if current == expected:
        print(f"VALID {manifest['id']}")
        return 0
    print(f"STALE {manifest['id']}")
    old_by_path = {repo["path"]: repo for repo in expected["repositories"]}
    new_by_path = {repo["path"]: repo for repo in current["repositories"]}
    for path in sorted(set(old_by_path) | set(new_by_path)):
        if old_by_path.get(path) != new_by_path.get(path):
            print(f"CHANGED {path}")
    return EXIT_STALE


def _delta_repo(old: dict[str, Any], new: dict[str, Any]) -> dict[str, Any]:
    paths = sorted(set(old["files"]) | set(new["files"]))
    changed_paths = [path for path in paths if old["files"].get(path) != new["files"].get(path)]
    changed_categories = [
        key for key in ("head", "committed", "staged", "unstaged", "untracked")
        if old[key] != new[key]
    ]
    return {
        "path": old["path"],
        "from_head": old["head"],
        "to_head": new["head"],
        "changed_categories": changed_categories,
        "changed_paths": changed_paths,
        "from_scope": {key: old[key] for key in ("committed", "staged", "unstaged", "untracked")},
        "to_scope": {key: new[key] for key in ("committed", "staged", "unstaged", "untracked")},
    }


def delta(args: argparse.Namespace) -> int:
    old = _read_manifest(args.from_manifest)
    new = _read_manifest(args.to_manifest)
    old_by_path = {repo["path"]: repo for repo in old["repositories"]}
    new_by_path = {repo["path"]: repo for repo in new["repositories"]}
    if set(old_by_path) != set(new_by_path):
        raise InputError("delta requires the same repository set")
    for path in sorted(old_by_path):
        old_repo, new_repo = old_by_path[path], new_by_path[path]
        if old_repo["base"] != new_repo["base"] or old_repo["merge_base"] != new_repo["merge_base"]:
            raise InputError(f"delta requires the same base for repository: {path}")
    payload = {
        "schema_version": SCHEMA_VERSION,
        "from_id": old["id"],
        "to_id": new["id"],
        "repositories": [
            _delta_repo(old_by_path[path], new_by_path[path])
            for path in sorted(old_by_path)
        ],
    }
    print(json.dumps(payload, ensure_ascii=False, sort_keys=True, separators=(",", ":")))
    return 0


def parser() -> argparse.ArgumentParser:
    root = argparse.ArgumentParser(description=__doc__)
    commands = root.add_subparsers(dest="command", required=True)
    freeze_parser = commands.add_parser("freeze")
    freeze_parser.add_argument("--change", required=True)
    freeze_parser.add_argument("--workspace", required=True)
    freeze_parser.add_argument("--repo-spec", action="append", required=True)
    freeze_parser.add_argument("--output", required=True)
    freeze_parser.set_defaults(handler=freeze)
    verify_parser = commands.add_parser("verify")
    verify_parser.add_argument("--manifest", required=True)
    verify_parser.set_defaults(handler=verify)
    delta_parser = commands.add_parser("delta")
    delta_parser.add_argument("--from-manifest", required=True)
    delta_parser.add_argument("--to-manifest", required=True)
    delta_parser.set_defaults(handler=delta)
    return root


def main() -> int:
    args = parser().parse_args()
    try:
        return args.handler(args)
    except InputError as error:
        _error(str(error))
        return EXIT_INPUT


if __name__ == "__main__":
    sys.exit(main())
