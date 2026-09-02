#!/usr/bin/env python3
"""Bounded, read-only queries over the declared workspace project registry."""

from __future__ import annotations

import argparse
import json
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
        project["_workspace"] = workspace
        validated.append(project)
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
    return project_root


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


def _git_candidates(project_root: Path, roots: list[str]) -> list[str]:
    if not (project_root / ".git").exists():
        raise InputError(f"project is not a Git working tree: {project_root.name}")
    command = [
        "git", "--literal-pathspecs", "-C", str(project_root), "ls-files", "-z", "--cached", "--others",
        "--exclude-standard", "--", *roots,
    ]
    result = subprocess.run(
        command, stdout=subprocess.PIPE, stderr=subprocess.PIPE, check=False
    )
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
        for relative in _git_candidates(project_root, roots):
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
    return parser


def main(argv: list[str] | None = None) -> int:
    args = build_parser().parse_args(argv)
    try:
        _, projects = load_registry(args.workspace)
        if args.command == "project-context":
            return project_context(projects, args.project)
        if args.command == "server-registry":
            return server_registry(projects, args.server, args.project)
        return workspace_search(
            projects, args.project, args.text, args.limit, args.offset
        )
    except InputError as exc:
        print(f"ERROR\t{exc}", file=sys.stderr)
        return EXIT_INPUT


if __name__ == "__main__":
    raise SystemExit(main())
