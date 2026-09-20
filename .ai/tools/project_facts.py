#!/usr/bin/env python3
"""Bounded, read-only queries over the declared workspace project registry."""

from __future__ import annotations

import argparse
import json
import os
import re
import subprocess
import sys
from pathlib import Path, PurePosixPath
from typing import Any


EXIT_INPUT = 2
EXIT_ZERO_MATCH = 3
EXIT_AMBIGUOUS = 4
REQUIRED_PROJECT_FIELDS = {
    "name", "path", "build", "card", "search_roots", "applications"
}
REQUIRED_APPLICATION_FIELDS = {
    "server", "module", "main_class", "source_path"
}
REQUIRED_BUSINESS_TERM_FIELDS = {
    "term", "source_paths"
}
VERIFICATION_FIELDS = {
    "build_command", "test_command", "evidence", "verified_commit"
}
COMMIT_PATTERN = re.compile(r"[0-9a-fA-F]{40}|[0-9a-fA-F]{64}")
SENSITIVE_SUFFIXES = {
    ".key", ".pem", ".p12", ".pfx", ".crt", ".cer", ".jks", ".keystore"
}
SENSITIVE_NAMES = {
    ".env", "credentials", "credentials.json", "secrets", "secrets.json",
    "id_rsa", "id_dsa", "id_ecdsa", "id_ed25519"
}


class InputError(ValueError):
    """The registry or user input violates a declared boundary."""


def _string(value: Any, label: str) -> str:
    if not isinstance(value, str) or not value or any(c in value for c in "\t\r\n"):
        raise InputError(f"{label} must be a non-empty single-line string")
    return value


def _relative(value: Any, label: str, *, allow_dot: bool = False) -> str:
    text = _string(value, label)
    path = PurePosixPath(text)
    if path.is_absolute() or ".." in path.parts:
        raise InputError(f"{label} must be a relative path without '..'")
    if text == "." and not allow_dot:
        raise InputError(f"{label} cannot be '.'")
    return text


def _within(path: Path, parent: Path, label: str) -> Path:
    resolved = path.resolve()
    try:
        resolved.relative_to(parent.resolve())
    except ValueError as exc:
        raise InputError(f"boundary violation for {label}: {path}") from exc
    return resolved


def _validated_child(parent: Path, relative: str, label: str) -> Path:
    return _within(parent / relative, parent, label)


def _business_terms(
    raw_terms: Any, project_name: str
) -> list[dict[str, Any]]:
    if not isinstance(raw_terms, list):
        raise InputError(
            f"project {project_name} business_terms must be a list"
        )
    checked_terms: list[dict[str, Any]] = []
    seen_terms: set[str] = set()
    for term_index, raw_term in enumerate(raw_terms):
        if (
            not isinstance(raw_term, dict)
            or not REQUIRED_BUSINESS_TERM_FIELDS.issubset(raw_term)
        ):
            raise InputError(
                f"project {project_name} business_term[{term_index}] "
                "is missing required fields"
            )
        term = dict(raw_term)
        term["term"] = _string(
            term["term"],
            f"project {project_name} business_term[{term_index}].term",
        )
        if term["term"] in seen_terms:
            raise InputError(
                f"duplicate business term for project {project_name}: "
                f"{term['term']}"
            )
        seen_terms.add(term["term"])

        synonyms = term.get("synonyms", [])
        if not isinstance(synonyms, list):
            raise InputError(
                f"project {project_name} business_term[{term_index}] "
                "synonyms must be a list"
            )
        term["synonyms"] = [
            _string(
                synonym,
                f"project {project_name} business_term[{term_index}] synonym",
            )
            for synonym in synonyms
        ]

        source_paths = term["source_paths"]
        if not isinstance(source_paths, list) or not source_paths:
            raise InputError(
                f"project {project_name} business_term[{term_index}] "
                "source_paths must be a non-empty list"
            )
        term["source_paths"] = [
            _relative(
                source_path,
                f"project {project_name} business_term[{term_index}] source path",
                allow_dot=True,
            )
            for source_path in source_paths
        ]
        checked_terms.append(term)
    return checked_terms


def _verification(
    raw_verification: Any, project_name: str, ai_root: Path
) -> dict[str, str]:
    if not isinstance(raw_verification, dict):
        raise InputError(f"project {project_name} verification must be an object")
    unknown_fields = set(raw_verification) - VERIFICATION_FIELDS
    if unknown_fields:
        raise InputError(
            f"project {project_name} verification has unknown fields: "
            + ", ".join(sorted(unknown_fields))
        )
    if not raw_verification:
        raise InputError(
            f"project {project_name} verification must declare at least one field"
        )

    checked = dict(raw_verification)
    for field in ("build_command", "test_command"):
        if field in checked:
            checked[field] = _string(
                checked[field], f"project {project_name} verification {field}"
            )
    if "evidence" in checked:
        checked["evidence"] = _relative(
            checked["evidence"],
            f"project {project_name} verification evidence",
        )
        evidence_path = _validated_child(
            ai_root,
            checked["evidence"],
            f"project {project_name} verification evidence",
        )
        if not evidence_path.is_file():
            raise InputError(
                f"missing verification evidence for project {project_name}: "
                f"{checked['evidence']}"
            )
        if _is_sensitive(checked["evidence"]):
            raise InputError(
                f"project {project_name} verification evidence path is sensitive: "
                f"{checked['evidence']}"
            )
    if "verified_commit" in checked:
        commit = _string(
            checked["verified_commit"],
            f"project {project_name} verification verified_commit",
        )
        if COMMIT_PATTERN.fullmatch(commit) is None:
            raise InputError(
                f"project {project_name} verified_commit must be "
                "40 or 64 hexadecimal characters"
            )
        checked["verified_commit"] = commit
    return checked


def _validate_dependencies(projects: list[dict[str, Any]]) -> None:
    names = {project["name"] for project in projects}
    graph: dict[str, list[str]] = {}
    for project in projects:
        name = project["name"]
        if "dependencies" not in project:
            continue
        dependencies = project["dependencies"]
        if not isinstance(dependencies, list):
            raise InputError(f"project {name} dependencies must be a list")
        checked: list[str] = []
        seen: set[str] = set()
        for dependency in dependencies:
            dependency = _string(dependency, f"project {name} dependency")
            if dependency not in names:
                raise InputError(
                    f"unregistered dependency for project {name}: {dependency}"
                )
            if dependency == name:
                raise InputError(f"project {name} must not depend on itself")
            if dependency in seen:
                raise InputError(
                    f"duplicate dependency for project {name}: {dependency}"
                )
            seen.add(dependency)
            checked.append(dependency)
        graph[name] = checked

    visiting: set[str] = set()
    visited: set[str] = set()
    cycle_path: list[str] = []

    def visit(name: str) -> None:
        if name in visited:
            return
        if name in visiting:
            cycle_at = cycle_path.index(name)
            cycle = cycle_path[cycle_at:] + [name]
            raise InputError("dependency cycle: " + " -> ".join(cycle))
        visiting.add(name)
        cycle_path.append(name)
        for dependency in graph.get(name, []):
            visit(dependency)
        cycle_path.pop()
        visiting.remove(name)
        visited.add(name)

    for name in sorted(graph):
        visit(name)


def load_registry(workspace_arg: str) -> tuple[Path, list[dict[str, Any]]]:
    workspace_path = Path(workspace_arg)
    if not workspace_path.is_absolute():
        raise InputError("workspace must be an absolute path")
    workspace = workspace_path.resolve()
    if not workspace.is_dir():
        raise InputError(f"workspace does not exist: {workspace_arg}")
    ai_root = _validated_child(workspace, ".ai", "shared .ai directory")
    registry_path = _validated_child(
        ai_root, "kb/projects/registry.json", "project registry"
    )
    try:
        data = json.loads(registry_path.read_text(encoding="utf-8"))
    except (OSError, json.JSONDecodeError) as exc:
        raise InputError(f"cannot read registry: {exc}") from exc
    if (
        not isinstance(data, dict)
        or type(data.get("schema_version")) is not int
        or data["schema_version"] != 1
    ):
        raise InputError("registry schema_version must be 1")
    projects = data.get("projects")
    if not isinstance(projects, list):
        raise InputError("registry projects must be a list")

    seen: set[str] = set()
    validated: list[dict[str, Any]] = []
    for index, raw in enumerate(projects):
        if not isinstance(raw, dict) or not REQUIRED_PROJECT_FIELDS.issubset(raw):
            raise InputError(f"project[{index}] is missing required fields")
        project = dict(raw)
        name = _string(project["name"], f"project[{index}].name")
        if name in seen:
            raise InputError(f"duplicate project name: {name}")
        seen.add(name)
        project["path"] = _relative(project["path"], f"project {name} path")
        project["card"] = _relative(project["card"], f"project {name} card")
        project["build"] = _string(project["build"], f"project {name} build")
        card_path = _validated_child(ai_root, project["card"], f"project {name} card")
        if not card_path.is_file():
            raise InputError(f"missing card for project {name}: {project['card']}")

        roots = project["search_roots"]
        if not isinstance(roots, list) or not roots:
            raise InputError(f"project {name} search_roots must be a non-empty list")
        project["search_roots"] = [
            _relative(root, f"project {name} search root", allow_dot=True)
            for root in roots
        ]

        applications = project["applications"]
        if not isinstance(applications, list):
            raise InputError(f"project {name} applications must be a list")
        checked_apps = []
        for app_index, raw_app in enumerate(applications):
            if not isinstance(raw_app, dict) or not REQUIRED_APPLICATION_FIELDS.issubset(raw_app):
                raise InputError(
                    f"project {name} application[{app_index}] is missing required fields"
                )
            app = dict(raw_app)
            for field in ("server", "main_class"):
                app[field] = _string(app[field], f"project {name} application {field}")
            app["module"] = _relative(
                app["module"], f"project {name} application module", allow_dot=True
            )
            app["source_path"] = _relative(
                app["source_path"], f"project {name} application source_path"
            )
            checked_apps.append(app)
        project["applications"] = checked_apps
        if "dependencies" in project:
            raw_dependencies = project["dependencies"]
            if not isinstance(raw_dependencies, list):
                raise InputError(f"project {name} dependencies must be a list")
            project["dependencies"] = [
                _string(dependency, f"project {name} dependency")
                for dependency in raw_dependencies
            ]
        if "verification" in project:
            project["verification"] = _verification(
                project["verification"], name, ai_root
            )
        if "business_terms" in project:
            project["business_terms"] = _business_terms(
                project["business_terms"], name
            )
        project["_workspace"] = workspace
        validated.append(project)
    _validate_dependencies(validated)
    return workspace, validated


def resolve_project_root(project: dict[str, Any]) -> Path:
    workspace: Path = project["_workspace"]
    name = project["name"]
    project_root = _validated_child(workspace, project["path"], f"project {name}")
    for root in project["search_roots"]:
        _validated_child(project_root, root, f"project {name} search root")
    for app in project["applications"]:
        _validated_child(
            project_root, app["source_path"], f"project {name} application"
        )
    for business_term in project.get("business_terms", []):
        for source_path in business_term["source_paths"]:
            _validated_child(
                project_root,
                source_path,
                f"project {name} business term source path",
            )
    return project_root


def _git_metadata_target(
    raw_target: str, base: Path, workspace: Path, label: str
) -> Path:
    target = Path(raw_target)
    if not target.is_absolute():
        target = base / target
    return _within(target.resolve(), workspace, label)


def _validate_metadata_path(path: Path, workspace: Path, label: str) -> None:
    components: list[Path] = []
    current = path
    while current != current.parent:
        components.append(current)
        current = current.parent
    for component in reversed(components):
        if component.is_symlink():
            _within(
                component.resolve(),
                workspace,
                label,
            )


def _validated_git_entry(project: dict[str, Any]) -> Path | None:
    project_root = resolve_project_root(project)
    git_entry = project_root / ".git"
    if not git_entry.exists():
        return None

    workspace: Path = project["_workspace"]
    original_entry = git_entry
    if git_entry.is_symlink():
        original_entry = _git_metadata_target(
            str(git_entry),
            git_entry.parent,
            workspace,
            f"project {project['name']} Git metadata",
        )
    elif git_entry.is_file():
        try:
            content = git_entry.read_text(encoding="utf-8").strip()
        except (OSError, UnicodeError) as exc:
            raise InputError(
                f"cannot read project {project['name']} Git metadata: {exc}"
            ) from exc
        if not content.startswith("gitdir:"):
            raise InputError(
                f"invalid project {project['name']} Git metadata file"
            )
        original_entry = _git_metadata_target(
            content.removeprefix("gitdir:").strip(),
            git_entry.parent,
            workspace,
            f"project {project['name']} Git metadata",
        )

    metadata_roots = [original_entry]
    current = original_entry
    seen_common: set[Path] = set()
    while True:
        commondir = current / "commondir"
        _validate_metadata_path(
            commondir,
            workspace,
            f"project {project['name']} Git metadata symlink",
        )
        if not commondir.is_file():
            break
        if current in seen_common:
            raise InputError(
                f"project {project['name']} Git commondir cycle"
            )
        seen_common.add(current)
        try:
            raw_common = (current / "commondir").read_text(
                encoding="utf-8"
            ).strip()
        except (OSError, UnicodeError) as exc:
            raise InputError(
                f"cannot read project {project['name']} Git commondir: {exc}"
            ) from exc
        current = _git_metadata_target(
            raw_common,
            current,
            workspace,
            f"project {project['name']} Git commondir",
        )
        metadata_roots.append(current)

    for metadata_root in metadata_roots:
        alternates = metadata_root / "objects/info/alternates"
        _validate_metadata_path(
            alternates,
            workspace,
            f"project {project['name']} Git metadata symlink",
        )
        if alternates.is_file():
            try:
                alternate_lines = alternates.read_text(encoding="utf-8").splitlines()
            except (OSError, UnicodeError) as exc:
                raise InputError(
                    f"cannot read project {project['name']} Git alternates: {exc}"
                ) from exc
            for alternate in alternate_lines:
                if alternate and not alternate.startswith("#"):
                    _git_metadata_target(
                        alternate,
                        metadata_root / "objects",
                        workspace,
                        f"project {project['name']} Git alternate object store",
                    )
        for path in metadata_root.rglob("*"):
            if path.is_symlink():
                _within(
                    path.resolve(),
                    workspace,
                    f"project {project['name']} Git metadata symlink",
                )
    return git_entry


def _git_environment() -> dict[str, str]:
    environment = os.environ.copy()
    for name in list(environment):
        if name == "GIT" or name.startswith("GIT_"):
            environment.pop(name, None)
    environment["GIT_CONFIG_NOSYSTEM"] = "1"
    environment["GIT_CONFIG_GLOBAL"] = "/dev/null"
    environment["GIT_CONFIG_SYSTEM"] = "/dev/null"
    environment["GIT_OPTIONAL_LOCKS"] = "0"
    environment["PAGER"] = "cat"
    return environment


def _git_command(project_root: Path, *command: str) -> list[str]:
    return [
        "git", "--literal-pathspecs", "-C", str(project_root),
        "--work-tree", str(project_root),
        "-c", "core.fsmonitor=false",
        "-c", "core.hooksPath=/dev/null",
        "-c", "core.attributesFile=/dev/null",
        "-c", "core.excludesFile=/dev/null",
        "-c", "core.pager=cat",
        *command,
    ]


def _current_git_head(project_root: Path) -> str | None:
    try:
        result = subprocess.run(
            _git_command(project_root, "rev-parse", "HEAD"),
            stdout=subprocess.PIPE,
            stderr=subprocess.PIPE,
            check=False,
            env=_git_environment(),
        )
    except OSError:
        return None
    current = result.stdout.decode("utf-8", errors="replace").strip()
    if result.returncode == 0 and COMMIT_PATTERN.fullmatch(current):
        return current
    return None


def select_projects(projects: list[dict[str, Any]], names: list[str] | None) -> list[dict[str, Any]]:
    if not names:
        return projects
    by_name = {project["name"]: project for project in projects}
    selected = []
    for raw_name in names:
        name = _string(raw_name, "project")
        if name not in by_name:
            raise InputError(f"unregistered project: {name}")
        if by_name[name] not in selected:
            selected.append(by_name[name])
    return selected


def project_context(projects: list[dict[str, Any]], name: str) -> int:
    project = select_projects(projects, [name])[0]
    project_root = resolve_project_root(project)
    verification = project.get("verification", {})
    git_entry = (
        _validated_git_entry(project)
        if "verified_commit" in verification
        else None
    )
    current_head = _current_git_head(project_root) if git_entry else None
    status = "available" if project_root.is_dir() else "missing"
    print("\t".join([
        "PROJECT", project["name"], project["path"], project["build"],
        project["card"], status,
    ]))
    for app in project["applications"]:
        print("\t".join([
            "APPLICATION", app["server"], app["module"], app["main_class"],
            app["source_path"],
        ]))
    for dependency in project.get("dependencies", []):
        print(f"DEPENDENCY\t{dependency}")
    for field, label in (
        ("build_command", "build"),
        ("test_command", "test"),
    ):
        if field in verification:
            print(f"VERIFICATION\t{label}\t{verification[field]}")
    if "evidence" in verification:
        print(f"VERIFICATION\tevidence\t{verification['evidence']}")
    if "verified_commit" in verification:
        declared = verification["verified_commit"]
        status = "unavailable"
        if current_head is not None:
            status = (
                "current"
                if current_head.lower() == declared.lower()
                else "drifted"
            )
        print(f"VERIFICATION\tverified_commit\t{status}\t{declared}")
    return 0


def server_registry(
    projects: list[dict[str, Any]], server: str, names: list[str] | None
) -> int:
    server = _string(server, "server")
    selected = select_projects(projects, names)
    matches = [
        (project, app)
        for project in selected
        for app in project["applications"]
        if app["server"] == server
    ]
    boundary_projects = (
        selected if names
        else list({project["name"]: project for project, _ in matches}.values())
    )
    for project in boundary_projects:
        resolve_project_root(project)
    if not matches:
        print(f"no server match: {server}", file=sys.stderr)
        return EXIT_ZERO_MATCH
    if len(matches) > 1:
        locations = ",".join(sorted(project["name"] for project, _ in matches))
        print(f"ambiguous server: {server} ({locations})", file=sys.stderr)
        return EXIT_AMBIGUOUS
    project, app = matches[0]
    print("\t".join([
        "SERVER", app["server"], project["name"], app["module"],
        app["main_class"], app["source_path"],
    ]))
    return 0


def _git_candidates(
    project: dict[str, Any], project_root: Path, roots: list[str]
) -> list[str]:
    if _validated_git_entry(project) is None:
        raise InputError(f"project is not a Git working tree: {project_root.name}")
    command = _git_command(
        project_root,
        "ls-files", "-z", "--cached", "--others",
        "--exclude-standard", "--", *roots,
    )
    try:
        result = subprocess.run(
            command,
            stdout=subprocess.PIPE,
            stderr=subprocess.PIPE,
            check=False,
            env=_git_environment(),
        )
    except OSError as exc:
        raise InputError(
            f"cannot execute Git for project {project['name']}: {exc}"
        ) from exc
    if result.returncode:
        message = result.stderr.decode("utf-8", errors="replace").strip()
        raise InputError(f"cannot list project files: {message}")
    candidates = set()
    for raw_path in result.stdout.split(b"\0"):
        if not raw_path:
            continue
        relative = raw_path.decode("utf-8", errors="surrogateescape")
        if any(character in relative for character in "\t\r\n"):
            raise InputError("Git path contains a TSV-unsafe control character")
        candidates.add(relative)
    return sorted(candidates)


def _has_symlink_component(project_root: Path, relative: str) -> bool:
    current = project_root
    for part in PurePosixPath(relative).parts:
        current /= part
        if current.is_symlink():
            return True
    return False


def _is_sensitive(relative: str) -> bool:
    path = PurePosixPath(relative)
    lowered = [part.lower() for part in path.parts]
    name = lowered[-1]
    if name in SENSITIVE_NAMES or name.startswith(".env."):
        return True
    if any(name.endswith(suffix) for suffix in SENSITIVE_SUFFIXES):
        return True
    return any(part in {"secrets", "credentials", ".ssh", ".gnupg"} for part in lowered)


def business_terms(
    projects: list[dict[str, Any]], names: list[str] | None, text: str,
    limit: int, offset: int, exact: bool,
) -> int:
    text = _string(text, "business term")
    if limit < 1 or offset < 0:
        raise InputError("limit must be positive and offset must be non-negative")
    selected = select_projects(projects, names)
    matches: list[str] = []
    for project in selected:
        resolve_project_root(project)
        for declaration in project.get("business_terms", []):
            candidates = [declaration["term"], *declaration["synonyms"]]
            matched = (
                any(text == candidate for candidate in candidates)
                if exact
                else any(text in candidate for candidate in candidates)
            )
            if not matched:
                continue
            for source_path in declaration["source_paths"]:
                matches.append("\t".join([
                    "BUSINESS_TERM", declaration["term"], project["name"],
                    project["card"], source_path,
                ]))
    page = matches[offset:offset + limit]
    if not page:
        print("no business term matches", file=sys.stderr)
        return EXIT_ZERO_MATCH
    for match in page:
        print(match)
    next_offset = offset + len(page)
    if next_offset < len(matches):
        print(f"TRUNCATED\tnext_offset={next_offset}\ttotal={len(matches)}", file=sys.stderr)
    return 0


def workspace_search(
    projects: list[dict[str, Any]], names: list[str] | None, text: str,
    limit: int, offset: int,
) -> int:
    text = _string(text, "search text")
    if len(text) < 3:
        raise InputError("search text must contain at least 3 characters")
    if limit < 1 or offset < 0:
        raise InputError("limit must be positive and offset must be non-negative")
    selected = select_projects(projects, names)
    matches: list[str] = []
    for project in selected:
        project_root = resolve_project_root(project)
        if not project_root.is_dir():
            if names:
                raise InputError(f"project is not checked out: {project['name']}")
            continue
        roots = project["search_roots"]
        for relative in _git_candidates(project, project_root, roots):
            if _has_symlink_component(project_root, relative):
                continue
            candidate = _validated_child(
                project_root, relative, f"project {project['name']} search candidate"
            )
            if _is_sensitive(relative) or not candidate.is_file():
                continue
            try:
                content = candidate.read_text(encoding="utf-8", errors="ignore")
            except OSError as exc:
                raise InputError(f"cannot read search candidate: {relative}: {exc}") from exc
            for line_number, line in enumerate(content.splitlines(), start=1):
                if text in line:
                    matches.append(f"{project['name']}/{relative}:{line_number}")
    matches.sort()
    page = matches[offset:offset + limit]
    if not page:
        print("no search matches", file=sys.stderr)
        return EXIT_ZERO_MATCH
    for match in page:
        print(match)
    next_offset = offset + len(page)
    if next_offset < len(matches):
        print(f"TRUNCATED\tnext_offset={next_offset}\ttotal={len(matches)}", file=sys.stderr)
    return 0


def build_parser() -> argparse.ArgumentParser:
    parser = argparse.ArgumentParser(description=__doc__)
    commands = parser.add_subparsers(dest="command", required=True)

    context = commands.add_parser("project-context")
    context.add_argument("--workspace", required=True)
    context.add_argument("--project", required=True)

    server = commands.add_parser("server-registry")
    server.add_argument("--workspace", required=True)
    server.add_argument("--server", required=True)
    server.add_argument("--project", action="append")

    search = commands.add_parser("workspace-search")
    search.add_argument("--workspace", required=True)
    search.add_argument("--text", required=True)
    search.add_argument("--project", action="append")
    search.add_argument("--limit", type=int, default=20)
    search.add_argument("--offset", type=int, default=0)

    terms = commands.add_parser("business-terms")
    terms.add_argument("--workspace", required=True)
    terms.add_argument("--text", required=True)
    terms.add_argument("--project", action="append")
    terms.add_argument("--exact", action="store_true")
    terms.add_argument("--limit", type=int, default=20)
    terms.add_argument("--offset", type=int, default=0)
    return parser


def main(argv: list[str] | None = None) -> int:
    args = build_parser().parse_args(argv)
    try:
        _, projects = load_registry(args.workspace)
        if args.command == "project-context":
            return project_context(projects, args.project)
        if args.command == "server-registry":
            return server_registry(projects, args.server, args.project)
        if args.command == "business-terms":
            return business_terms(
                projects, args.project, args.text, args.limit, args.offset,
                args.exact,
            )
        return workspace_search(
            projects, args.project, args.text, args.limit, args.offset
        )
    except InputError as exc:
        print(f"ERROR\t{exc}", file=sys.stderr)
        return EXIT_INPUT


if __name__ == "__main__":
    raise SystemExit(main())
