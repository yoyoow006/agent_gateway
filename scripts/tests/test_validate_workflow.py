from __future__ import annotations

from collections.abc import Callable
from dataclasses import dataclass
import inspect
import json
import os
import re
import shutil
import stat
import subprocess
import sys
import tempfile
import unittest
from pathlib import Path


REPOSITORY_ROOT = Path(__file__).resolve().parents[2]
CAPABILITY_MARKER_ENV = "WORKFLOW_VALIDATOR_CONTRACT_MARKER"
CAPABILITY_TOKEN_ENV = "WORKFLOW_VALIDATOR_CONTRACT_TOKEN"
LEGACY_INNER_SENTINEL = "WORKFLOW_VALIDATOR_CONTRACT_INNER"
WORKFLOW_FIXTURE_DIRECTORIES = (".ai", ".claude", ".codex", "openspec", "scripts")


class PrePushHookTest(unittest.TestCase):
    """pre-push 钩子只随源仓分发;安装目标内没有该文件时明确跳过。"""

    def test_pre_push_hook_propagates_core_result(self) -> None:
        hook_source = REPOSITORY_ROOT / "scripts" / "hooks" / "pre-push"
        is_source_repository = (
            REPOSITORY_ROOT / "scripts" / "lib" / "install_ai_workflow.py"
        ).is_file()
        if not hook_source.is_file():
            if not is_source_repository:
                self.skipTest("pre-push hook is not shipped with this installation")
            self.fail("source repository is missing scripts/hooks/pre-push")
        if is_source_repository:
            # fileMode=false 的挂载上磁盘权限不可信,索引模式才是权威信号。
            listing = subprocess.run(
                ["git", "ls-files", "-s", "scripts/hooks/pre-push"],
                cwd=REPOSITORY_ROOT,
                stdout=subprocess.PIPE,
                stderr=subprocess.PIPE,
                text=True,
                check=True,
                timeout=30,
            )
            self.assertIn("100755 ", listing.stdout, msg=listing.stdout)
        with tempfile.TemporaryDirectory() as temporary:
            root = Path(temporary)
            (root / "scripts" / "hooks").mkdir(parents=True)
            (root / "scripts" / "lib").mkdir(parents=True)
            shutil.copy(hook_source, root / "scripts" / "hooks" / "pre-push")
            # git 会向 pre-push 传入远端名与 URL 两个参数:钩子不得把它们透传给 core。
            # 第三组的 stub 参数敏感:收到任何参数即失败,用于抓住透传回归。
            for stub, expected, hook_arguments in (
                ("exit 0", 0, []),
                ("exit 1", 1, []),
                ('[ "$#" -eq 0 ] || exit 1\nexit 0', 0, ["origin", "git@example.invalid:repo.git"]),
            ):
                with self.subTest(stub=stub, arguments=hook_arguments):
                    (root / "scripts" / "lib" / "validate-workflow-core.sh").write_text(
                        f"#!/usr/bin/env bash\n{stub}\n", encoding="utf-8"
                    )
                    result = subprocess.run(
                        [
                            "/usr/bin/bash",
                            str(root / "scripts" / "hooks" / "pre-push"),
                            *hook_arguments,
                        ],
                        stdout=subprocess.PIPE,
                        stderr=subprocess.STDOUT,
                        text=True,
                        check=False,
                        timeout=30,
                    )
                    self.assertEqual(expected, result.returncode, msg=result.stdout)


class CIPipelineTest(unittest.TestCase):
    """CI 配置仅存在于源仓库;安装目标内没有 .github 时明确跳过。"""

    def test_ci_runs_installer_suite_as_required_step(self) -> None:
        workflow = REPOSITORY_ROOT / ".github" / "workflows" / "validate.yml"
        is_source_repository = (
            REPOSITORY_ROOT / "scripts" / "lib" / "install_ai_workflow.py"
        ).is_file()
        if not workflow.is_file():
            if not is_source_repository:
                self.skipTest("CI pipeline configuration is source-repository only")
            self.fail("source repository is missing .github/workflows/validate.yml")
        content = workflow.read_text(encoding="utf-8")
        self.assertIn("bash scripts/validate-workflow.sh --require-openspec", content)
        self.assertIn(
            "python3 -B -m unittest -v scripts.tests.test_install_ai_workflow", content
        )


def _core_external_commands(core_path: Path) -> tuple[str, ...]:
    """唯一权威命令清单:从指定 core 副本解析,不维护第二份白名单。"""
    result = subprocess.run(
        ["/usr/bin/bash", str(core_path), "--print-external-commands"],
        stdout=subprocess.PIPE,
        stderr=subprocess.PIPE,
        text=True,
        check=False,
        timeout=30,
    )
    if result.returncode != 0:
        raise AssertionError(
            f"core failed to publish external commands (exit {result.returncode}):"
            f"\n{result.stderr}"
        )
    commands = tuple(line for line in result.stdout.splitlines() if line)
    if not commands:
        raise AssertionError(
            f"core published an empty external command list:\n{result.stderr}"
        )
    return commands
WORKFLOW_FIXTURE_FILES = (".gitignore", "AGENTS.md", "CLAUDE.md")
WORKFLOW_FIXTURE_ERROR = "unsafe workflow fixture source"


@dataclass(frozen=True)
class WorkflowFixtureEntry:
    name: str
    is_directory: bool
    token: tuple[int, int, int, int, int, int]
    children: tuple[WorkflowFixtureEntry, ...] = ()


def _fixture_token(value: os.stat_result) -> tuple[int, int, int, int, int, int]:
    return (
        value.st_dev,
        value.st_ino,
        value.st_mode,
        value.st_size,
        value.st_mtime_ns,
        value.st_ctime_ns,
    )


def _fixture_ignored(name: str) -> bool:
    return name == "__pycache__" or name.endswith(".pyc")


def _unsafe_fixture_source() -> None:
    raise ValueError(WORKFLOW_FIXTURE_ERROR) from None


def _open_fixture_entry(
    parent_fd: int,
    name: str,
    *,
    is_directory: bool,
    expected_token: tuple[int, int, int, int, int, int] | None = None,
) -> tuple[int, tuple[int, int, int, int, int, int]]:
    status = os.stat(name, dir_fd=parent_fd, follow_symlinks=False)
    if is_directory != stat.S_ISDIR(status.st_mode):
        _unsafe_fixture_source()
    if not is_directory and not stat.S_ISREG(status.st_mode):
        _unsafe_fixture_source()
    flags = os.O_RDONLY | os.O_NOFOLLOW
    if is_directory:
        flags |= os.O_DIRECTORY
    file_fd = os.open(name, flags, dir_fd=parent_fd)
    token = _fixture_token(os.fstat(file_fd))
    if token != _fixture_token(status) or (
        expected_token is not None and token != expected_token
    ):
        os.close(file_fd)
        _unsafe_fixture_source()
    return file_fd, token


def _scan_fixture_directory(
    directory_fd: int,
    name: str,
    token: tuple[int, int, int, int, int, int],
) -> WorkflowFixtureEntry:
    children: list[WorkflowFixtureEntry] = []
    for child_name in sorted(os.listdir(directory_fd), key=os.fsencode):
        status = os.stat(child_name, dir_fd=directory_fd, follow_symlinks=False)
        if _fixture_ignored(child_name):
            if not (
                stat.S_ISDIR(status.st_mode) or stat.S_ISREG(status.st_mode)
            ):
                _unsafe_fixture_source()
            continue
        if stat.S_ISDIR(status.st_mode):
            child_fd, child_token = _open_fixture_entry(
                directory_fd, child_name, is_directory=True
            )
            try:
                children.append(
                    _scan_fixture_directory(child_fd, child_name, child_token)
                )
            finally:
                os.close(child_fd)
        elif stat.S_ISREG(status.st_mode):
            child_fd, child_token = _open_fixture_entry(
                directory_fd, child_name, is_directory=False
            )
            os.close(child_fd)
            children.append(WorkflowFixtureEntry(child_name, False, child_token))
        else:
            _unsafe_fixture_source()
    if _fixture_token(os.fstat(directory_fd)) != token:
        _unsafe_fixture_source()
    return WorkflowFixtureEntry(name, True, token, tuple(children))


def _scan_workflow_fixture(source_fd: int) -> tuple[WorkflowFixtureEntry, ...]:
    entries: list[WorkflowFixtureEntry] = []
    for name in WORKFLOW_FIXTURE_DIRECTORIES:
        try:
            os.stat(name, dir_fd=source_fd, follow_symlinks=False)
        except FileNotFoundError:
            continue
        directory_fd, token = _open_fixture_entry(source_fd, name, is_directory=True)
        try:
            entries.append(_scan_fixture_directory(directory_fd, name, token))
        finally:
            os.close(directory_fd)
    for name in WORKFLOW_FIXTURE_FILES:
        try:
            os.stat(name, dir_fd=source_fd, follow_symlinks=False)
        except FileNotFoundError:
            continue
        file_fd, token = _open_fixture_entry(source_fd, name, is_directory=False)
        os.close(file_fd)
        entries.append(WorkflowFixtureEntry(name, False, token))
    return tuple(entries)


def _copy_fixture_entry(
    parent_fd: int, entry: WorkflowFixtureEntry, destination: Path
) -> None:
    source_fd, token = _open_fixture_entry(
        parent_fd,
        entry.name,
        is_directory=entry.is_directory,
        expected_token=entry.token,
    )
    try:
        if entry.is_directory:
            destination.mkdir(mode=stat.S_IMODE(token[2]))
            destination.chmod(stat.S_IMODE(token[2]))
            for child in entry.children:
                _copy_fixture_entry(source_fd, child, destination / child.name)
        else:
            with destination.open("xb") as stream:
                while chunk := os.read(source_fd, 1024 * 1024):
                    stream.write(chunk)
            destination.chmod(stat.S_IMODE(token[2]))
        if _fixture_token(os.fstat(source_fd)) != token:
            _unsafe_fixture_source()
    finally:
        os.close(source_fd)


def copy_workflow_fixture(
    source: Path,
    destination: Path,
    *,
    before_copy: Callable[[], None] | None = None,
) -> None:
    source_fd = -1
    try:
        source_fd = os.open(source, os.O_RDONLY | os.O_DIRECTORY | os.O_NOFOLLOW)
        source_token = _fixture_token(os.fstat(source_fd))
        entries = _scan_workflow_fixture(source_fd)
        if _fixture_token(os.fstat(source_fd)) != source_token:
            _unsafe_fixture_source()
        if before_copy is not None:
            before_copy()
        if _fixture_token(os.fstat(source_fd)) != source_token:
            _unsafe_fixture_source()
        destination.mkdir()
        for entry in entries:
            _copy_fixture_entry(source_fd, entry, destination / entry.name)
    except (OSError, ValueError):
        if destination.exists() and not destination.is_symlink():
            shutil.rmtree(destination)
        _unsafe_fixture_source()
    finally:
        if source_fd >= 0:
            os.close(source_fd)


class ValidateWorkflowContractTest(unittest.TestCase):
    def setUp(self) -> None:
        self.temporary_directory = tempfile.TemporaryDirectory()
        self.addCleanup(self.temporary_directory.cleanup)
        temporary_root = Path(self.temporary_directory.name)
        self.fixture = temporary_root / "repository"
        copy_workflow_fixture(REPOSITORY_ROOT, self.fixture)
        subprocess.run(
            ["git", "init", "-q"],
            cwd=self.fixture,
            check=True,
            stdout=subprocess.PIPE,
            stderr=subprocess.PIPE,
            text=True,
        )
        self.stub_bin = temporary_root / "bin"
        self.stub_bin.mkdir()
        for command in _core_external_commands(
            self.fixture / "scripts" / "lib" / "validate-workflow-core.sh"
        ):
            executable = shutil.which(command)
            self.assertIsNotNone(executable, msg=f"missing test prerequisite: {command}")
            (self.stub_bin / command).symlink_to(executable)
        self._enable_profile_parser()

    def _write_executable(self, name: str, content: str) -> Path:
        path = self.stub_bin / name
        path.write_text(content, encoding="utf-8")
        path.chmod(path.stat().st_mode | stat.S_IXUSR)
        return path

    def _run_validator(self, *arguments: str) -> subprocess.CompletedProcess[str]:
        environment = {
            "LC_ALL": "C.UTF-8",
            "PATH": str(self.stub_bin),
        }
        return subprocess.run(
            ["/usr/bin/bash", "scripts/lib/validate-workflow-core.sh", *arguments],
            cwd=self.fixture,
            env=environment,
            stdout=subprocess.PIPE,
            stderr=subprocess.STDOUT,
            text=True,
            check=False,
            timeout=30,
        )

    def _enable_profile_parser(self) -> None:
        self._write_executable(
            "python3",
            "#!/bin/sh\n"
            "if test \"$1\" = \"-B\" && test \"$2\" = \"-c\"; then\n"
            f"  exec {sys.executable} \"$@\"\n"
            "fi\n"
            "exit 0\n",
        )

    def _recording_python_script(self, marker: Path) -> str:
        return (
            "#!/bin/sh\n"
            "if test \"$1\" = \"-B\" && test \"$2\" = \"-c\"; then\n"
            f"  exec {sys.executable} \"$@\"\n"
            "fi\n"
            f"printf '%s\\n' \"$*\" >> {marker}\n"
            "exit 0\n"
        )

    def _enable_mutating_profile_parser(self, action: str) -> None:
        self._write_executable(
            "python3",
            "#!/bin/sh\n"
            "if test \"$1\" = \"-B\" && test \"$2\" = \"-c\"; then\n"
            f"  token=\"$({sys.executable} \"$@\")\" || exit $?\n"
            f"{action}\n"
            "  printf \"%s\" \"$token\"\n"
            "  exit 0\n"
            "fi\n"
            "exit 0\n",
        )

    def _write_profile(self, assistant: str) -> None:
        profile = self.fixture / ".ai/assistant-profile.json"
        profile.write_text(
            "{\n  \"schema_version\": 1,\n"
            f"  \"assistant\": \"{assistant}\"\n}}\n",
            encoding="utf-8",
        )

    def _canonical_assistant(self) -> str:
        profile = self.fixture / ".ai/assistant-profile.json"
        if profile.is_file() and not profile.is_symlink():
            value = json.loads(profile.read_text(encoding="utf-8"))
            self.assertEqual(set(value), {"schema_version", "assistant"})
            self.assertIs(type(value["schema_version"]), int)
            self.assertEqual(value["schema_version"], 1)
            self.assertIn(value["assistant"], ("codex", "claude"))
            self.assertTrue(self._assistant_assets_available(value["assistant"]))
            return value["assistant"]
        available = [
            assistant
            for assistant in ("codex", "claude")
            if self._assistant_assets_available(assistant)
        ]
        self.assertTrue(available, msg="fixture has no canonical assistant assets")
        return "codex" if "codex" in available else available[0]

    def _primary_entry(self) -> str:
        return "AGENTS.md" if self._canonical_assistant() == "codex" else "CLAUDE.md"

    def _assistants_for_portability_test(self) -> tuple[str, ...]:
        installer = REPOSITORY_ROOT / "scripts/install-ai-workflow.sh"
        if installer.is_file():
            return ("codex", "claude")
        return (self._canonical_assistant(),)

    def _install_selected_metadata(self, assistant: str) -> None:
        installer = REPOSITORY_ROOT / "scripts/install-ai-workflow.sh"
        if not installer.is_file():
            self.assertEqual(assistant, self._canonical_assistant())
            self.assertTrue((self.fixture / ".ai/assistant-profile.json").is_file())
            self.assertTrue(self._assistant_assets_available(assistant))
            return

        target = self.fixture.parent / f"installed-{assistant}"
        target.mkdir()
        result = subprocess.run(
            [
                "/usr/bin/bash",
                str(installer),
                "--target",
                str(target),
                "--assistant",
                assistant,
            ],
            cwd=REPOSITORY_ROOT,
            stdout=subprocess.PIPE,
            stderr=subprocess.PIPE,
            text=True,
            check=False,
        )
        self.assertEqual(result.returncode, 0, msg=result.stderr or result.stdout)
        selected_root = self.fixture / f".{assistant}"
        if not selected_root.exists():
            shutil.copytree(REPOSITORY_ROOT / f".{assistant}", selected_root)
        selected_entry = "AGENTS.md" if assistant == "codex" else "CLAUDE.md"
        if not (self.fixture / selected_entry).exists():
            shutil.copy2(REPOSITORY_ROOT / selected_entry, self.fixture / selected_entry)
        (self.fixture / ".gitignore").write_bytes((target / ".gitignore").read_bytes())
        (self.fixture / ".ai/assistant-profile.json").write_bytes(
            (target / ".ai/assistant-profile.json").read_bytes()
        )
        unselected = "claude" if assistant == "codex" else "codex"
        shutil.rmtree(self.fixture / f".{unselected}", ignore_errors=True)
        entry = "CLAUDE.md" if unselected == "claude" else "AGENTS.md"
        (self.fixture / entry).unlink(missing_ok=True)

    def _assistant_assets_available(self, assistant: str) -> bool:
        entry = "AGENTS.md" if assistant == "codex" else "CLAUDE.md"
        return (self.fixture / f".{assistant}/skills").is_dir() and (self.fixture / entry).is_file()

    def _assistant_required_in_fixture(self, assistant: str) -> bool:
        profile = self.fixture / ".ai/assistant-profile.json"
        if not profile.exists():
            return True
        try:
            value = json.loads(profile.read_text(encoding="utf-8"))
        except (OSError, UnicodeError, json.JSONDecodeError):
            return True
        return value.get("assistant") == assistant

    def _assert_mutation_rejected(
        self, relative_path: str, sentence: str | None = None
    ) -> None:
        target = self.fixture / relative_path
        target.parent.mkdir(parents=True, exist_ok=True)
        if sentence is None:
            target.write_text("# 重新引入的平行知识正文\n", encoding="utf-8")
        else:
            with target.open("a", encoding="utf-8") as stream:
                stream.write(f"\n{sentence}\n")

        result = self._run_validator()

        self.assertNotEqual(
            result.returncode,
            0,
            msg=f"validator accepted forbidden mutation in {relative_path}:\n{result.stdout}",
        )
        self.assertIn("FAIL", result.stdout)

    def test_default_reports_skip_when_openspec_is_missing(self) -> None:
        result = self._run_validator()

        self.assertEqual(result.returncode, 0, msg=result.stdout)
        self.assertIn("[SKIP]", result.stdout)
        self.assertIn("openspec", result.stdout.lower())
        self.assertRegex(result.stdout, r"PASS=\d+ FAIL=0 SKIP=1\s*\Z")

    def test_base_fixture_accepts_a_valid_profile(self) -> None:
        self._write_profile(self._canonical_assistant())

        result = self._run_validator()

        self.assertEqual(result.returncode, 0, msg=result.stdout)
        self.assertRegex(result.stdout, r"PASS=\d+ FAIL=0 SKIP=1\s*\Z")

    def test_required_mode_fails_when_openspec_is_missing(self) -> None:
        result = self._run_validator("--require-openspec")

        self.assertNotEqual(result.returncode, 0, msg=result.stdout)
        self.assertIn("FAIL", result.stdout)
        self.assertIn("openspec", result.stdout.lower())

    def test_required_mode_passes_with_openspec_stub(self) -> None:
        marker = self.stub_bin.parent / "openspec-called"
        self._write_executable(
            "openspec",
            "#!/bin/sh\n"
            f"touch {marker}\n"
            'test "$*" = "validate --all --no-interactive"\n',
        )

        result = self._run_validator("--require-openspec")

        self.assertEqual(result.returncode, 0, msg=result.stdout)
        self.assertTrue(marker.exists(), msg="validator did not execute OpenSpec validation")

    def test_required_mode_rejects_failed_openspec_validation(self) -> None:
        self._write_executable("openspec", "#!/bin/sh\nexit 1\n")

        result = self._run_validator("--require-openspec")

        self.assertNotEqual(result.returncode, 0, msg=result.stdout)
        self.assertIn("[FAIL]", result.stdout)
        self.assertIn("openspec", result.stdout.lower())

    def test_unknown_argument_is_rejected(self) -> None:
        result = self._run_validator("--unknown")

        self.assertNotEqual(result.returncode, 0, msg=result.stdout)
        self.assertIn("[FAIL]", result.stdout)
        self.assertIn("--unknown", result.stdout)

    def test_print_external_commands_publishes_unique_sorted_list(self) -> None:
        result = self._run_validator("--print-external-commands")

        self.assertEqual(result.returncode, 0, msg=result.stdout)
        lines = result.stdout.splitlines()
        self.assertTrue(lines, msg="empty external command list")
        self.assertEqual(lines, sorted(lines), msg="external commands must be sorted")
        self.assertEqual(len(lines), len(set(lines)), msg="external commands must be unique")
        for line in lines:
            self.assertRegex(line, r"\A[a-z0-9_.-]+\Z", msg=f"unexpected entry: {line!r}")
        joined = "\n".join(lines)
        for expected in ("awk", "bash", "cat", "git", "grep", "sed", "sort", "tr", "wc"):
            self.assertIn(expected, joined, msg=f"missing required command: {expected}")

    def test_internal_core_runs_tools_without_contract_suite(self) -> None:
        marker = self.stub_bin.parent / "python-called"
        self._write_executable("python3", self._recording_python_script(marker))

        result = self._run_validator()

        self.assertEqual(result.returncode, 0, msg=result.stdout)
        calls = marker.read_text(encoding="utf-8")
        self.assertIn("test_project_facts.py", calls)
        self.assertIn("test_review_manifest.py", calls)
        unit_calls = [line for line in calls.splitlines() if "unittest" in line]
        self.assertTrue(unit_calls, msg=calls)
        self.assertTrue(all(line.startswith("-B ") for line in unit_calls), msg=calls)
        self.assertNotIn("scripts.tests.test_validate_workflow", calls)

    def test_public_entry_statically_requires_core_and_contract_suite(self) -> None:
        entry = (self.fixture / "scripts/validate-workflow.sh").read_text(
            encoding="utf-8"
        )
        contract_command = (
            "python3 -B -m unittest -v scripts.tests.test_validate_workflow"
        )
        core_command = "bash scripts/lib/validate-workflow-core.sh"

        def valid_public_entry(content: str) -> bool:
            return (
                core_command in content
                and contract_command in content
                and CAPABILITY_MARKER_ENV not in content
                and CAPABILITY_TOKEN_ENV not in content
                and LEGACY_INNER_SENTINEL not in content
            )

        self.assertTrue(valid_public_entry(entry), msg=entry)

        mutated = entry.replace(contract_command, "true", 1)
        self.assertFalse(valid_public_entry(mutated), msg=mutated)

    def test_forged_same_uid_capability_cannot_skip_contract_suite(self) -> None:
        forged_marker = self.stub_bin.parent / "forged-contract-capability"
        forged_token = "caller-controlled-token"
        forged_marker.write_text(f"{forged_token}\n", encoding="utf-8")
        forged_marker.chmod(0o600)
        calls = self.stub_bin.parent / "forged-python-calls"
        self._write_executable("python3", self._recording_python_script(calls))
        environment = {
            "LC_ALL": "C.UTF-8",
            CAPABILITY_MARKER_ENV: str(forged_marker),
            CAPABILITY_TOKEN_ENV: forged_token,
            LEGACY_INNER_SENTINEL: "1",
            "PATH": str(self.stub_bin),
        }

        result = subprocess.run(
            ["/usr/bin/bash", "scripts/validate-workflow.sh"],
            cwd=self.fixture,
            env=environment,
            stdout=subprocess.PIPE,
            stderr=subprocess.STDOUT,
            text=True,
            check=False,
        )

        self.assertEqual(result.returncode, 0, msg=result.stdout)
        self.assertIn(
            "scripts.tests.test_validate_workflow", calls.read_text(encoding="utf-8")
        )
        self.assertNotIn("[SKIP] 工作流顶层契约测试", result.stdout)

    def test_arbitrary_capability_environment_cannot_skip_contract_suite(self) -> None:
        wrong_marker = self.stub_bin.parent / "wrong-contract-capability"
        wrong_marker.write_text("actual-token\n", encoding="utf-8")
        wrong_marker.chmod(0o600)
        cases = (
            ("missing", None, None),
            ("wrong-token", str(wrong_marker), "different-token"),
            ("wrong-marker", str(wrong_marker) + "-missing", "actual-token"),
        )
        for name, marker, token in cases:
            with self.subTest(case=name):
                environment = {
                    "LC_ALL": "C.UTF-8",
                    LEGACY_INNER_SENTINEL: "1",
                    "PATH": str(self.stub_bin),
                }
                if marker is not None:
                    environment[CAPABILITY_MARKER_ENV] = marker
                if token is not None:
                    environment[CAPABILITY_TOKEN_ENV] = token
                result = subprocess.run(
                    ["/usr/bin/bash", "scripts/validate-workflow.sh"],
                    cwd=self.fixture,
                    env=environment,
                    stdout=subprocess.PIPE,
                    stderr=subprocess.STDOUT,
                    text=True,
                    check=False,
                )
                self.assertEqual(result.returncode, 0, msg=result.stdout)
                self.assertIn("[PASS] 工作流顶层契约测试", result.stdout)

    def test_outer_validator_fails_when_contract_suite_fails(self) -> None:
        contract_test = self.fixture / "scripts/tests/test_validate_workflow.py"
        contract_test.write_text(
            "import unittest\n\n"
            "class IntentionallyBrokenContract(unittest.TestCase):\n"
            "    def test_broken(self):\n"
            "        self.fail('intentional contract failure')\n",
            encoding="utf-8",
        )
        environment = os.environ.copy()
        environment["PYTHONDONTWRITEBYTECODE"] = "1"
        environment.pop(LEGACY_INNER_SENTINEL, None)
        environment.pop(CAPABILITY_MARKER_ENV, None)
        environment.pop(CAPABILITY_TOKEN_ENV, None)
        result = subprocess.run(
            ["/usr/bin/bash", "scripts/validate-workflow.sh"],
            cwd=self.fixture,
            env=environment,
            stdout=subprocess.PIPE,
            stderr=subprocess.STDOUT,
            text=True,
            check=False,
        )

        self.assertNotEqual(result.returncode, 0, msg=result.stdout)
        self.assertIn("[FAIL] 工作流顶层契约测试", result.stdout)

    def test_fast_mode_skips_contract_suite_and_keeps_summary(self) -> None:
        environment = os.environ.copy()
        environment["PYTHONDONTWRITEBYTECODE"] = "1"
        environment.pop(LEGACY_INNER_SENTINEL, None)
        environment.pop(CAPABILITY_MARKER_ENV, None)
        environment.pop(CAPABILITY_TOKEN_ENV, None)

        result = subprocess.run(
            ["/usr/bin/bash", "scripts/validate-workflow.sh", "--fast"],
            cwd=self.fixture,
            env=environment,
            stdout=subprocess.PIPE,
            stderr=subprocess.STDOUT,
            text=True,
            check=False,
            timeout=60,
        )

        self.assertEqual(result.returncode, 0, msg=result.stdout)
        self.assertNotIn("工作流顶层契约测试", result.stdout)
        self.assertRegex(result.stdout, r"PASS=\d+ FAIL=0 SKIP=\d+\s*\Z")

    def test_second_validator_instance_exits_while_locked(self) -> None:
        if shutil.which("flock") is None:
            self.skipTest("flock is required to exercise the concurrency lock")
        lock_path = self.fixture / ".ai-local" / ".validate.lock"
        lock_path.parent.mkdir(parents=True, exist_ok=True)
        holder = subprocess.Popen(
            [
                sys.executable,
                "-c",
                "import fcntl, sys, time\n"
                "handle = open(sys.argv[1], 'a+')\n"
                "fcntl.flock(handle, fcntl.LOCK_EX)\n"
                "print('locked', flush=True)\n"
                "time.sleep(60)\n",
                str(lock_path),
            ],
            stdout=subprocess.PIPE,
            text=True,
        )
        try:
            self.assertEqual("locked", holder.stdout.readline().strip())
            environment = os.environ.copy()
            environment["PYTHONDONTWRITEBYTECODE"] = "1"
            result = subprocess.run(
                ["/usr/bin/bash", "scripts/validate-workflow.sh"],
                cwd=self.fixture,
                env=environment,
                stdout=subprocess.PIPE,
                stderr=subprocess.STDOUT,
                text=True,
                check=False,
                timeout=30,
            )
            self.assertEqual(2, result.returncode, msg=result.stdout)
            self.assertIn("另一校验实例运行中", result.stdout)
        finally:
            holder.kill()
            holder.wait()

    def test_missing_flock_degrades_to_unlocked_with_notice(self) -> None:
        degraded_bin = self.stub_bin.parent / "bin-without-flock"
        degraded_bin.mkdir()
        for command in _core_external_commands(
            self.fixture / "scripts" / "lib" / "validate-workflow-core.sh"
        ):
            if command == "flock":
                continue
            executable = shutil.which(command)
            self.assertIsNotNone(executable, msg=f"missing test prerequisite: {command}")
            (degraded_bin / command).symlink_to(executable)
        self._enable_profile_parser()
        python_stub = degraded_bin / "python3"
        python_stub.write_text(
            self._recording_python_script(degraded_bin.parent / "degraded-calls"),
            encoding="utf-8",
        )
        python_stub.chmod(python_stub.stat().st_mode | stat.S_IXUSR)

        environment = {
            "LC_ALL": "C.UTF-8",
            "PATH": str(degraded_bin),
        }
        result = subprocess.run(
            ["/usr/bin/bash", "scripts/validate-workflow.sh"],
            cwd=self.fixture,
            env=environment,
            stdout=subprocess.PIPE,
            stderr=subprocess.STDOUT,
            text=True,
            check=False,
            timeout=60,
        )

        self.assertEqual(result.returncode, 0, msg=result.stdout)
        self.assertIn("flock 不可用，降级为无锁并发保护", result.stdout)
        self.assertIn("[PASS] 工作流顶层契约测试", result.stdout)

    def test_wrapper_print_external_commands_passthrough(self) -> None:
        environment = {
            "LC_ALL": "C.UTF-8",
            "PATH": str(self.stub_bin),
        }
        result = subprocess.run(
            ["/usr/bin/bash", "scripts/validate-workflow.sh", "--print-external-commands"],
            cwd=self.fixture,
            env=environment,
            stdout=subprocess.PIPE,
            stderr=subprocess.STDOUT,
            text=True,
            check=False,
            timeout=30,
        )

        self.assertEqual(result.returncode, 0, msg=result.stdout)
        lines = result.stdout.splitlines()
        self.assertTrue(lines, msg="empty external command list via wrapper")
        self.assertIn("cat", lines)

    def test_unusable_lock_infrastructure_degrades_with_notice(self) -> None:
        # 前提:flock 存在(本机/CI),但 .ai-local 是普通文件 → mkdir -p 失败:
        # 应降级为无锁并正常完成,而不是误诊为"另一校验实例运行中"。
        if shutil.which("flock") is None:
            self.skipTest("flock is required to exercise the lock infrastructure path")
        locked_bin = self.stub_bin.parent / "bin-with-flock"
        locked_bin.mkdir()
        for command in _core_external_commands(
            self.fixture / "scripts" / "lib" / "validate-workflow-core.sh"
        ):
            executable = shutil.which(command)
            self.assertIsNotNone(executable, msg=f"missing test prerequisite: {command}")
            (locked_bin / command).symlink_to(executable)
        (locked_bin / "flock").symlink_to(shutil.which("flock"))
        self._enable_profile_parser()
        python_stub = locked_bin / "python3"
        python_stub.write_text(
            self._recording_python_script(locked_bin.parent / "locked-calls"),
            encoding="utf-8",
        )
        python_stub.chmod(python_stub.stat().st_mode | stat.S_IXUSR)

        lock_dir = self.fixture / ".ai-local"
        lock_dir.write_text("not a directory\n", encoding="utf-8")

        environment = {
            "LC_ALL": "C.UTF-8",
            "PATH": str(locked_bin),
        }
        result = subprocess.run(
            ["/usr/bin/bash", "scripts/validate-workflow.sh"],
            cwd=self.fixture,
            env=environment,
            stdout=subprocess.PIPE,
            stderr=subprocess.STDOUT,
            text=True,
            check=False,
            timeout=60,
        )

        self.assertEqual(result.returncode, 0, msg=result.stdout)
        self.assertIn("锁文件不可用，降级为无锁并发保护", result.stdout)
        self.assertIn("[PASS] 工作流顶层契约测试", result.stdout)
        lock_dir.unlink()

    def test_rejects_deleted_or_broken_role_adapters(self) -> None:
        adapters = {
            ".codex/agents/explorer.toml": ".ai/prompts/agents/explorer.md",
            ".codex/agents/reviewer.toml": ".ai/prompts/agents/reviewer.md",
            ".codex/agents/test_worker.toml": ".ai/prompts/agents/test-worker.md",
            ".claude/agents/explorer.md": ".ai/prompts/agents/explorer.md",
            ".claude/agents/reviewer.md": ".ai/prompts/agents/reviewer.md",
            ".claude/agents/test-worker.md": ".ai/prompts/agents/test-worker.md",
        }
        for relative_path, shared_prompt in adapters.items():
            assistant = relative_path.split("/", 1)[0][1:]
            if not self._assistant_required_in_fixture(assistant):
                continue
            target = self.fixture / relative_path
            original = target.read_text(encoding="utf-8")
            with self.subTest(adapter=relative_path, mutation="deleted"):
                target.unlink()
                result = self._run_validator()
                self.assertNotEqual(result.returncode, 0, msg=result.stdout)
                self.assertIn("[FAIL]", result.stdout)
                target.write_text(original, encoding="utf-8")
            with self.subTest(adapter=relative_path, mutation="broken-reference"):
                self.assertIn(shared_prompt, original)
                target.write_text(
                    original.replace(
                        shared_prompt, ".ai/prompts/agents/missing-contract.md"
                    ),
                    encoding="utf-8",
                )
                result = self._run_validator()
                self.assertNotEqual(result.returncode, 0, msg=result.stdout)
                self.assertIn("[FAIL]", result.stdout)
                target.write_text(original, encoding="utf-8")
            with self.subTest(adapter=relative_path, mutation="legacy-body-reference"):
                target.write_text(
                    original
                    + "\n读取 .codex/ai-kb/memory/reintroduced.md 作为正文。\n",
                    encoding="utf-8",
                )
                result = self._run_validator()
                self.assertNotEqual(result.returncode, 0, msg=result.stdout)
                self.assertIn("[FAIL]", result.stdout)
                target.write_text(original, encoding="utf-8")

    def test_python_cache_paths_are_ignored_and_checked(self) -> None:
        probes = (
            "scripts/tests/__pycache__/probe.cpython-314.pyc",
            ".ai/tools/tests/probe.pyc",
            ".ai/tools/tests/probe.pyo",
        )
        for probe in probes:
            ignored = subprocess.run(
                ["git", "check-ignore", "-q", probe], cwd=self.fixture, check=False
            )
            self.assertEqual(ignored.returncode, 0, msg=f"not ignored: {probe}")

        result = self._run_validator()
        self.assertEqual(result.returncode, 0, msg=result.stdout)
        self.assertIn("[PASS] Python 缓存路径已忽略", result.stdout)

    def test_validator_supports_non_git_workflow_root(self) -> None:
        shutil.rmtree(self.fixture / ".git")

        result = self._run_validator()

        self.assertEqual(result.returncode, 0, msg=result.stdout)
        self.assertIn("[PASS] Python 缓存路径已忽略", result.stdout)
        for assistant, label in (
            ("codex", "SDD 草稿区已忽略"),
            ("claude", "Claude SDD 草稿区已忽略"),
        ):
            if self._assistant_required_in_fixture(assistant):
                self.assertIn(f"[PASS] {label}", result.stdout)
            else:
                self.assertNotIn(f"[PASS] {label}", result.stdout)

    def test_rejects_reintroduced_parallel_ai_kb_body(self) -> None:
        legacy_body_roots = (
            ".codex/ai-kb/kb",
            ".codex/ai-kb/rules",
            ".codex/ai-kb/memory",
            ".claude/ai-kb/kb",
            ".claude/ai-kb/rules",
            ".claude/ai-kb/memory",
        )
        for root in legacy_body_roots:
            for suffix in (
                "reintroduced-parallel-source.md",
                "nested/README.md",
            ):
                target = f"{root}/{suffix}"
                with self.subTest(root=root, path=suffix):
                    try:
                        self._assert_mutation_rejected(target)
                    finally:
                        (self.fixture / target).unlink(missing_ok=True)

    def test_rejects_removal_of_gate_tier_and_arbitration_rules(self) -> None:
        cases = (
            ("CLAUDE.md", "以仓库技能为准"),
            ("AGENTS.md", "以仓库技能为准"),
            (".claude/skills/verify/SKILL.md", "validate-workflow.sh --fast"),
            (".codex/skills/verify/SKILL.md", "validate-workflow.sh --fast"),
            (".claude/skills/archive/SKILL.md", "openspec/archive/README.md"),
            (".codex/skills/archive/SKILL.md", "openspec/archive/README.md"),
        )
        for relative_path, phrase in cases:
            with self.subTest(path=relative_path):
                target = self.fixture / relative_path
                if not target.is_file():
                    # 单助手安装目标没有另一侧入口/技能,缺失侧由 core 的
                    # assistant_required 门控豁免,这里只测实际存在的文件。
                    continue
                original = target.read_text(encoding="utf-8")
                stripped = "\n".join(
                    line for line in original.split("\n") if phrase not in line
                )
                self.assertNotEqual(stripped, original, msg=f"phrase absent: {phrase}")
                target.write_text(stripped, encoding="utf-8")
                try:
                    result = self._run_validator()
                    self.assertNotEqual(result.returncode, 0, msg=result.stdout)
                    self.assertIn("[FAIL]", result.stdout)
                finally:
                    target.write_text(original, encoding="utf-8")

    def test_rejects_manifest_requirement_for_quick_mode(self) -> None:
        self._assert_mutation_rejected(
            self._primary_entry(), "快速模式必须创建 freeze manifest 后才能修改文件。"
        )

    def test_rejects_second_full_review_for_standard_mode(self) -> None:
        self._assert_mutation_rejected(
            self._primary_entry(), "标准模式必须在修复后执行第二次完整综合审查。"
        )

    def test_rejects_mandatory_agent_for_every_task(self) -> None:
        self._assert_mutation_rejected(
            self._primary_entry(), "所有任务必须调用角色代理后才能开始。"
        )

    def test_rejects_retired_codex_tool_names(self) -> None:
        cases = (
            (".codex/skills/parallel-agents/SKILL.md", "multi_agent_v1__spawn_agent"),
            (".claude/skills/parallel-agents/SKILL.md", "close_agent"),
            ("scripts/ai-workflow-assets/codex/.codex/skills/parallel-agents/SKILL.md", "send_input"),
            (".codex/README.md", "fork_context"),
        )
        for relative_path, token in cases:
            target = self.fixture / relative_path
            if not target.is_file():
                # 单助手安装目标没有另一侧路径；该例天然无法注入。
                continue
            with self.subTest(path=relative_path, token=token):
                original = target.read_text(encoding="utf-8")
                target.write_text(
                    f"{original}\n旧工具 `{token}` 残留示例。\n", encoding="utf-8"
                )
                try:
                    result = self._run_validator()
                    self.assertNotEqual(result.returncode, 0, msg=result.stdout)
                    self.assertIn("[FAIL] 废弃工具名零残留", result.stdout)
                finally:
                    target.write_text(original, encoding="utf-8")

    def test_rejects_parallel_agents_note_without_current_tool_names(self) -> None:
        relative_path = ".codex/skills/parallel-agents/SKILL.md"
        if not (self.fixture / relative_path).is_file():
            self.skipTest("codex assistant is not present in this fixture")
        target = self.fixture / relative_path
        original = target.read_text(encoding="utf-8")
        self.assertIn("wait_agent", original)
        target.write_text(
            original.replace("wait_agent", "wait_legacy"), encoding="utf-8"
        )
        try:
            result = self._run_validator()
            self.assertNotEqual(result.returncode, 0, msg=result.stdout)
            self.assertIn("[FAIL] 注记现行工具名", result.stdout)
        finally:
            target.write_text(original, encoding="utf-8")

    def test_rejects_migration_notes_in_guarded_skills(self) -> None:
        guarded = (
            ".claude/skills/writing-skills/SKILL.md",
            ".codex/skills/writing-skills/SKILL.md",
            ".claude/skills/parallel-agents/SKILL.md",
            ".codex/skills/parallel-agents/SKILL.md",
            ".claude/skills/systematic-debugging/SKILL.md",
            ".codex/skills/systematic-debugging/SKILL.md",
        )
        for relative_path in guarded:
            target = self.fixture / relative_path
            if not target.is_file():
                # 单助手安装目标没有另一侧路径；该例天然无法注入。
                continue
            with self.subTest(path=relative_path):
                original = target.read_text(encoding="utf-8")
                target.write_text(
                    f"{original}\n（源技能伴生文档未随本仓库迁移，此处注记。）\n",
                    encoding="utf-8",
                )
                try:
                    result = self._run_validator()
                    self.assertNotEqual(result.returncode, 0, msg=result.stdout)
                    self.assertIn("[FAIL] 技能迁移注记零残留", result.stdout)
                finally:
                    target.write_text(original, encoding="utf-8")

    def test_rejects_writing_skills_without_char_metric(self) -> None:
        for relative_path in (
            ".claude/skills/writing-skills/SKILL.md",
            ".codex/skills/writing-skills/SKILL.md",
        ):
            target = self.fixture / relative_path
            if not target.is_file():
                continue
            agent = relative_path.split("/")[0]
            with self.subTest(path=relative_path):
                original = target.read_text(encoding="utf-8")
                self.assertIn("tr -d", original)
                self.assertIn("wc -m", original)
                target.write_text(
                    original.replace("tr -d", "wc -w"), encoding="utf-8"
                )
                try:
                    result = self._run_validator()
                    self.assertNotEqual(result.returncode, 0, msg=result.stdout)
                    self.assertIn(
                        f"[FAIL] {agent} writing-skills 字符口径锚串",
                        result.stdout,
                    )
                finally:
                    target.write_text(original, encoding="utf-8")

    def test_rejects_writing_skills_without_scenario_binding(self) -> None:
        for relative_path in (
            ".claude/skills/writing-skills/SKILL.md",
            ".codex/skills/writing-skills/SKILL.md",
        ):
            target = self.fixture / relative_path
            if not target.is_file():
                continue
            agent = relative_path.split("/")[0]
            with self.subTest(path=relative_path):
                original = target.read_text(encoding="utf-8")
                self.assertIn("workflow-pressure-scenarios.md", original)
                target.write_text(
                    original.replace(
                        "workflow-pressure-scenarios.md", "scenario-file.md"
                    ),
                    encoding="utf-8",
                )
                try:
                    result = self._run_validator()
                    self.assertNotEqual(result.returncode, 0, msg=result.stdout)
                    self.assertIn(
                        f"[FAIL] {agent} writing-skills 场景重跑绑定",
                        result.stdout,
                    )
                finally:
                    target.write_text(original, encoding="utf-8")

    def test_rejects_archive_index_drift(self) -> None:
        # 自包含受控归档：不依赖源仓 README（安装目标按设计没有该文件）。
        archive = self.fixture / "openspec/archive"
        backup = tempfile.TemporaryDirectory()
        self.addCleanup(backup.cleanup)
        shutil.move(str(archive), backup.name)
        archive.mkdir()
        readme = archive / "README.md"
        index_line = "- `alpha-change` — 受控归档(标准)\n"
        try:
            # 完好状态：目录与索引 1:1，校验必须通过。
            (archive / "alpha-change").mkdir()
            readme.write_text(
                f"# 归档变更索引\n\n{index_line}", encoding="utf-8"
            )
            result = self._run_validator()
            self.assertEqual(result.returncode, 0, msg=result.stdout)

            def expect_index_fail() -> None:
                result = self._run_validator()
                self.assertNotEqual(result.returncode, 0, msg=result.stdout)
                self.assertIn("[FAIL] 归档索引与目录 1:1", result.stdout)

            # 缺失：索引行被删。
            readme.write_text("# 归档变更索引\n", encoding="utf-8")
            expect_index_fail()
            # 悬空：索引指向不存在的目录。
            readme.write_text(
                f"# 归档变更索引\n\n{index_line}- `ghost-change` — 不存在(标准)\n",
                encoding="utf-8",
            )
            expect_index_fail()
            # 重复：同一目录两行索引。
            readme.write_text(
                f"# 归档变更索引\n\n{index_line}{index_line}", encoding="utf-8"
            )
            expect_index_fail()
        finally:
            shutil.rmtree(archive, ignore_errors=True)
            shutil.move(str(Path(backup.name) / "archive"), str(archive.parent))

    def test_empty_archive_without_index_still_passes(self) -> None:
        archive = self.fixture / "openspec/archive"
        backup = tempfile.TemporaryDirectory()
        self.addCleanup(backup.cleanup)
        shutil.move(str(archive), backup.name)
        archive.mkdir()
        try:
            result = self._run_validator()
            self.assertEqual(result.returncode, 0, msg=result.stdout)
        finally:
            shutil.rmtree(archive, ignore_errors=True)
            shutil.move(
                str(Path(backup.name) / "archive"), str(archive.parent)
            )

    def test_public_entry_surfaces_contract_suite_internal_skips(self) -> None:
        contract_test = self.fixture / "scripts/tests/test_validate_workflow.py"
        contract_test.write_text(
            "import unittest\n\n"
            "class InstalledContract(unittest.TestCase):\n"
            "    def test_ok(self):\n"
            "        self.assertTrue(True)\n\n"
            "    def test_skipped(self):\n"
            "        self.skipTest('source-repository only')\n",
            encoding="utf-8",
        )
        # 默认 stub 只处理 profile 解析；这里让契约套件真正跑起来，
        # 但 tools discover 仍走 stub（保持秒级）。
        self._write_executable(
            "python3",
            "#!/bin/sh\n"
            "if test \"$1\" = \"-B\" && test \"$2\" = \"-c\"; then\n"
            f"  exec {sys.executable} \"$@\"\n"
            "fi\n"
            "if test \"$1\" = \"-B\" && test \"$2\" = \"-m\" && test \"$4\" != \"discover\"; then\n"
            f"  exec {sys.executable} \"$@\"\n"
            "fi\n"
            "exit 0\n",
        )
        environment = {"LC_ALL": "C.UTF-8", "PATH": str(self.stub_bin)}
        result = subprocess.run(
            ["/usr/bin/bash", "scripts/validate-workflow.sh"],
            cwd=self.fixture,
            env=environment,
            stdout=subprocess.PIPE,
            stderr=subprocess.STDOUT,
            text=True,
            check=False,
            timeout=120,
        )
        self.assertEqual(result.returncode, 0, msg=result.stdout)
        self.assertIn("契约套件内部设计性跳过 1 项", result.stdout)
        self.assertIn("source-repository only", result.stdout)
        # 汇总行保持末行，SKIP 字段不含套件内部跳过（openspec stub 仍为 1）。
        self.assertRegex(result.stdout, r"PASS=\d+ FAIL=0 SKIP=1\s*\Z")


class WorkflowInstalledPortabilityRegressionTests:
    def _select_only(self, assistant: str) -> None:
        self._write_profile(assistant)
        unselected = "claude" if assistant == "codex" else "codex"
        shutil.rmtree(self.fixture / f".{unselected}", ignore_errors=True)
        entry = "CLAUDE.md" if unselected == "claude" else "AGENTS.md"
        (self.fixture / entry).unlink(missing_ok=True)

    def test_installed_fixture_without_installer_uses_canonical_profile(self) -> None:
        global REPOSITORY_ROOT
        assistant = self._canonical_assistant()
        self._select_only(assistant)
        (self.fixture / "scripts/install-ai-workflow.sh").unlink(missing_ok=True)
        original_root = REPOSITORY_ROOT
        self.addCleanup(globals().__setitem__, "REPOSITORY_ROOT", original_root)
        REPOSITORY_ROOT = self.fixture
        self._install_selected_metadata(assistant)

    def test_internal_recording_stub_delegates_profile_parser(self) -> None:
        self._select_only(self._canonical_assistant())
        marker = self.stub_bin.parent / "python-called"
        self._write_executable("python3", self._recording_python_script(marker))
        result = self._run_validator()
        self.assertEqual(result.returncode, 0, msg=result.stdout)

    def test_public_recording_stub_delegates_profile_parser(self) -> None:
        self._select_only(self._canonical_assistant())
        calls = self.stub_bin.parent / "python-calls"
        self._write_executable("python3", self._recording_python_script(calls))
        result = subprocess.run(
            ["/usr/bin/bash", "scripts/validate-workflow.sh"],
            cwd=self.fixture,
            env={"LC_ALL": "C.UTF-8", "PATH": str(self.stub_bin)},
            stdout=subprocess.PIPE,
            stderr=subprocess.STDOUT,
            text=True,
            check=False,
        )
        self.assertEqual(result.returncode, 0, msg=result.stdout)

    def test_entry_mutations_derive_the_fixture_primary_entry(self) -> None:
        for method_name in (
            "test_rejects_manifest_requirement_for_quick_mode",
            "test_rejects_second_full_review_for_standard_mode",
            "test_rejects_mandatory_agent_for_every_task",
        ):
            source = inspect.getsource(getattr(ValidateWorkflowContractTest, method_name))
            self.assertIn("self._primary_entry()", source, msg=method_name)
            self.assertNotIn('"AGENTS.md"', source, msg=method_name)

    def test_profile_targets_derive_the_fixture_canonical_assistant(self) -> None:
        base_source = inspect.getsource(
            ValidateWorkflowContractTest.test_base_fixture_accepts_a_valid_profile
        )
        mutation_source = inspect.getsource(
            WorkflowProfileMutationTests.test_profile_changes_after_parse_are_rejected
        )
        self.assertIn("self._canonical_assistant()", base_source)
        self.assertIn("self._canonical_assistant()", mutation_source)
        for method_name in (
            "test_internal_recording_stub_delegates_profile_parser",
            "test_public_recording_stub_delegates_profile_parser",
        ):
            source = inspect.getsource(
                getattr(WorkflowInstalledPortabilityRegressionTests, method_name)
            )
            self.assertIn("_select_only(self._canonical_assistant())", source)
        installed_source = inspect.getsource(
            WorkflowInstalledPortabilityRegressionTests.test_installed_fixture_without_installer_uses_canonical_profile
        )
        self.assertIn("assistant = self._canonical_assistant()", installed_source)

    def test_proposal_mutation_is_independent_of_historical_changes(self) -> None:
        shutil.rmtree(
            self.fixture / "openspec/changes/streamline-risk-tiered-ai-workflow",
            ignore_errors=True,
        )
        result = self._run_validator()
        self.assertEqual(result.returncode, 0, msg=result.stdout)
        core = (self.fixture / "scripts/lib/validate-workflow-core.sh").read_text(
            encoding="utf-8"
        )
        self.assertNotIn("streamline-risk-tiered-ai-workflow", core)

class WorkflowProfileTests(
    WorkflowInstalledPortabilityRegressionTests, ValidateWorkflowContractTest
):
    def test_other_assistant_installation_coverage_is_available(self) -> None:
        if not (REPOSITORY_ROOT / "scripts/install-ai-workflow.sh").is_file():
            self.skipTest(
                "source installer unavailable; current selected side remains covered"
            )

    def setUp(self) -> None:
        super().setUp()
        self._enable_profile_parser()

    def test_missing_profile_still_requires_both_assistants(self) -> None:
        if not all(self._assistant_assets_available(value) for value in ("codex", "claude")):
            self.skipTest("single-assistant installation lacks the compatibility fixture")
        for relative_path in ("AGENTS.md", "CLAUDE.md"):
            with self.subTest(path=relative_path):
                target = self.fixture / relative_path
                original = target.read_bytes()
                target.unlink()
                result = self._run_validator()
                self.assertNotEqual(result.returncode, 0, msg=result.stdout)
                target.write_bytes(original)

    def test_selected_profile_allows_the_other_assistant_to_be_absent(self) -> None:
        cases = (
            ("codex", ".claude", "CLAUDE.md"),
            ("claude", ".codex", "AGENTS.md"),
        )
        for assistant, adapter_dir, entry in cases:
            if not self._assistant_assets_available(assistant):
                continue
            with self.subTest(assistant=assistant):
                self._write_profile(assistant)
                shutil.rmtree(self.fixture / adapter_dir, ignore_errors=True)
                (self.fixture / entry).unlink(missing_ok=True)
                result = self._run_validator()
                self.assertEqual(result.returncode, 0, msg=result.stdout)
                self.assertRegex(result.stdout, r"FAIL=0 SKIP=1\s*\Z")
                if (REPOSITORY_ROOT / adapter_dir).exists():
                    shutil.copytree(REPOSITORY_ROOT / adapter_dir, self.fixture / adapter_dir)
                if (REPOSITORY_ROOT / entry).exists():
                    shutil.copy2(REPOSITORY_ROOT / entry, self.fixture / entry)

    def test_installer_selected_only_metadata_allows_core_validation(self) -> None:
        for assistant in self._assistants_for_portability_test():
            with self.subTest(assistant=assistant):
                self._install_selected_metadata(assistant)
                result = self._run_validator()
                self.assertEqual(result.returncode, 0, msg=result.stdout)
                self.assertRegex(result.stdout, r"FAIL=0 SKIP=1\s*\Z")

    def test_installer_selected_only_metadata_allows_public_validation(self) -> None:
        contract_test = self.fixture / "scripts/tests/test_validate_workflow.py"
        contract_test.write_text(
            "import unittest\n\n"
            "class InstalledContract(unittest.TestCase):\n"
            "    def test_contract(self):\n"
            "        self.assertTrue(True)\n",
            encoding="utf-8",
        )
        for assistant in self._assistants_for_portability_test():
            with self.subTest(assistant=assistant):
                self._install_selected_metadata(assistant)
                environment = {"LC_ALL": "C.UTF-8", "PATH": str(self.stub_bin)}
                result = subprocess.run(
                    ["/usr/bin/bash", "scripts/validate-workflow.sh"],
                    cwd=self.fixture,
                    env=environment,
                    stdout=subprocess.PIPE,
                    stderr=subprocess.STDOUT,
                    text=True,
                    check=False,
                    timeout=30,
                )
                self.assertEqual(result.returncode, 0, msg=result.stdout)
                summaries = re.findall(
                    r"^PASS=\d+ FAIL=0 SKIP=1$", result.stdout, re.MULTILINE
                )
                self.assertEqual(len(summaries), 1, msg=result.stdout)

    def test_selected_profile_rejects_entry_agent_and_skill_mutations(self) -> None:
        cases = {
            "codex": ("AGENTS.md", ".codex/agents/reviewer.toml", ".codex/skills/build/SKILL.md"),
            "claude": ("CLAUDE.md", ".claude/agents/reviewer.md", ".claude/skills/build/SKILL.md"),
        }
        for assistant, paths in cases.items():
            if not self._assistant_assets_available(assistant):
                continue
            self._write_profile(assistant)
            for relative_path in paths:
                with self.subTest(assistant=assistant, path=relative_path):
                    target = self.fixture / relative_path
                    original = target.read_bytes()
                    target.unlink()
                    result = self._run_validator()
                    self.assertNotEqual(result.returncode, 0, msg=result.stdout)
                    self.assertIn("[FAIL]", result.stdout)
                    target.write_bytes(original)

    def test_selected_profile_rejects_broken_and_legacy_adapter_references(self) -> None:
        cases = (
            ("codex", ".codex/agents/reviewer.toml", ".ai/prompts/agents/reviewer.md"),
            ("claude", ".claude/agents/reviewer.md", ".ai/prompts/agents/reviewer.md"),
        )
        for assistant, relative_path, shared_prompt in cases:
            if not self._assistant_assets_available(assistant):
                continue
            self._write_profile(assistant)
            target = self.fixture / relative_path
            original = target.read_text(encoding="utf-8")
            for mutation in ("broken-reference", "legacy-reference"):
                with self.subTest(assistant=assistant, mutation=mutation):
                    if mutation == "broken-reference":
                        self.assertIn(shared_prompt, original)
                        content = original.replace(shared_prompt, ".ai/prompts/agents/missing.md")
                    else:
                        content = original + f"\n读取 .{assistant}/ai-kb/memory/reintroduced.md。\n"
                    target.write_text(content, encoding="utf-8")
                    result = self._run_validator()
                    self.assertNotEqual(result.returncode, 0, msg=result.stdout)
                    self.assertIn("[FAIL]", result.stdout)
                    target.write_text(original, encoding="utf-8")

    def test_unselected_broken_remnants_are_ignored(self) -> None:
        cases = (
            ("codex", ".claude/agents/reviewer.md"),
            ("claude", ".codex/agents/reviewer.toml"),
        )
        for assistant, relative_path in cases:
            if not self._assistant_assets_available(assistant):
                continue
            with self.subTest(assistant=assistant):
                self._write_profile(assistant)
                target = self.fixture / relative_path
                original = target.read_bytes() if target.exists() else None
                target.parent.mkdir(parents=True, exist_ok=True)
                target.write_text(
                    "broken and references .codex/ai-kb/memory/secret-body.md\n",
                    encoding="utf-8",
                )
                result = self._run_validator()
                self.assertEqual(result.returncode, 0, msg=result.stdout)
                if original is None:
                    target.unlink()
                else:
                    target.write_bytes(original)

    def test_malformed_profiles_fail_closed_without_leaking_content(self) -> None:
        profile = self.fixture / ".ai/assistant-profile.json"
        outside = self.fixture.parent / "outside-profile.json"
        malformed_files = {
            "invalid-utf8": b"\xff{\"schema_version\":1,\"assistant\":\"codex\"}",
            "duplicate-key": b"{\"schema_version\":1,\"assistant\":\"codex\",\"assistant\":\"claude\"}",
            "extra-key": b"{\"schema_version\":1,\"assistant\":\"codex\",\"secret\":\"DO_NOT_LEAK\"}",
            "bool-schema": b"{\"schema_version\":true,\"assistant\":\"codex\"}",
            "assistant-type": b"{\"schema_version\":1,\"assistant\":1}",
            "top-level-type": b"[1,2]",
            "wrong-schema": b"{\"schema_version\":2,\"assistant\":\"codex\"}",
            "unknown-assistant": b"{\"schema_version\":1,\"assistant\":\"other\"}",
            "case-sensitive": b"{\"schema_version\":1,\"assistant\":\"Codex\"}",
        }
        for name, content in malformed_files.items():
            with self.subTest(case=name):
                profile.unlink(missing_ok=True)
                profile.write_bytes(content)
                result = self._run_validator()
                self.assertNotEqual(result.returncode, 0, msg=result.stdout)
                self.assertIn("[FAIL]", result.stdout)
                self.assertNotIn("Traceback", result.stdout)
                self.assertNotIn("DO_NOT_LEAK", result.stdout)

        profile.unlink(missing_ok=True)
        outside.write_text(
            "{\"schema_version\":1,\"assistant\":\"codex\"}\n", encoding="utf-8"
        )
        profile.symlink_to(outside)
        result = self._run_validator()
        self.assertNotEqual(result.returncode, 0, msg=result.stdout)
        self.assertNotIn(str(outside), result.stdout)
        self.assertNotIn("Traceback", result.stdout)

        profile.unlink()
        profile.mkdir()
        result = self._run_validator()
        self.assertNotEqual(result.returncode, 0, msg=result.stdout)
        self.assertNotIn("Traceback", result.stdout)
        profile.rmdir()

        os.mkfifo(profile)
        try:
            result = self._run_validator()
        finally:
            profile.unlink()
        self.assertNotEqual(result.returncode, 0, msg=result.stdout)
        self.assertNotIn("Traceback", result.stdout)


class WorkflowProfileMutationTests(ValidateWorkflowContractTest):
    def setUp(self) -> None:
        super().setUp()
        self._enable_profile_parser()

    def test_shared_gates_remain_required_for_each_profile(self) -> None:
        mutations = (
            (".ai/rules/index.md", "delete"),
            (".ai/prompts/agents/reviewer.md", "delete"),
            ("openspec/specs", "delete-tree"),
            ("scripts/workflow-pressure-scenarios.md", "truncate"),
        )
        for assistant in ("codex", "claude"):
            if not self._assistant_assets_available(assistant):
                continue
            self._write_profile(assistant)
            for relative_path, mutation in mutations:
                with self.subTest(assistant=assistant, path=relative_path):
                    target = self.fixture / relative_path
                    if mutation == "delete-tree":
                        shutil.move(target, target.with_name(target.name + ".saved"))
                    else:
                        original = target.read_bytes()
                        if mutation == "delete":
                            target.unlink()
                        else:
                            target.write_text("missing pressure contract\n", encoding="utf-8")
                    result = self._run_validator()
                    self.assertNotEqual(result.returncode, 0, msg=result.stdout)
                    self.assertIn("[FAIL]", result.stdout)
                    if mutation == "delete-tree":
                        shutil.move(target.with_name(target.name + ".saved"), target)
                    else:
                        target.write_bytes(original)

    def test_legacy_bodies_remain_rejected_for_each_profile(self) -> None:
        for assistant in ("codex", "claude"):
            if not self._assistant_assets_available(assistant):
                continue
            self._write_profile(assistant)
            for legacy_assistant in ("codex", "claude"):
                with self.subTest(assistant=assistant, legacy=legacy_assistant):
                    target = self.fixture / f".{legacy_assistant}/ai-kb/memory/reintroduced.md"
                    target.parent.mkdir(parents=True, exist_ok=True)
                    target.write_text("private body\n", encoding="utf-8")
                    result = self._run_validator()
                    self.assertNotEqual(result.returncode, 0, msg=result.stdout)
                    target.unlink()

    def test_profile_cannot_bypass_public_contract_suite(self) -> None:
        contract_test = self.fixture / "scripts/tests/test_validate_workflow.py"
        contract_test.write_text(
            "import unittest\n\n"
            "class IntentionallyBrokenContract(unittest.TestCase):\n"
            "    def test_broken(self):\n"
            "        self.fail(\"intentional contract failure\")\n",
            encoding="utf-8",
        )
        for assistant in ("codex", "claude"):
            if not self._assistant_assets_available(assistant):
                continue
            with self.subTest(assistant=assistant):
                self._write_profile(assistant)
                environment = os.environ.copy()
                environment[CAPABILITY_MARKER_ENV] = "forged"
                environment[CAPABILITY_TOKEN_ENV] = "forged"
                environment[LEGACY_INNER_SENTINEL] = "1"
                result = subprocess.run(
                    ["/usr/bin/bash", "scripts/validate-workflow.sh"],
                    cwd=self.fixture,
                    env=environment,
                    stdout=subprocess.PIPE,
                    stderr=subprocess.STDOUT,
                    text=True,
                    check=False,
                )
                self.assertNotEqual(result.returncode, 0, msg=result.stdout)
                self.assertIn("[FAIL] 工作流顶层契约测试", result.stdout)

    def test_profile_changes_after_parse_are_rejected(self) -> None:
        assistant = self._canonical_assistant()
        replacement_assistant = "claude" if assistant == "codex" else "codex"
        replacement = self.fixture / "replacement-profile.json"
        replacement.write_text(
            f"{{\"schema_version\":1,\"assistant\":\"{replacement_assistant}\"}}\n", encoding="utf-8"
        )
        actions = {
            "replace": "  cp replacement-profile.json .ai/assistant-profile.json",
            "delete": "  rm -f .ai/assistant-profile.json",
            "symlink": (
                "  rm -f .ai/assistant-profile.json\n"
                "  /bin/ln -s ../replacement-profile.json .ai/assistant-profile.json"
            ),
        }
        for mutation, action in actions.items():
            with self.subTest(mutation=mutation):
                self._write_profile(assistant)
                self._enable_mutating_profile_parser(action)
                result = self._run_validator()
                self.assertNotEqual(result.returncode, 0, msg=result.stdout)
                self.assertIn("[FAIL]", result.stdout)
                self.assertNotIn("Traceback", result.stdout)
                self.assertNotIn("replacement-profile.json", result.stdout)


class WorkflowFixtureCopyTests(unittest.TestCase):
    def _single_assistant_source(
        self, temporary_root: Path, assistant: str = "codex"
    ) -> tuple[Path, Path]:
        source = temporary_root / "source"
        destination = temporary_root / "fixture"
        source.mkdir()
        for name in (".ai", f".{assistant}", "openspec", "scripts"):
            (source / name).mkdir()
        entry = "AGENTS.md" if assistant == "codex" else "CLAUDE.md"
        (source / entry).write_text(assistant, encoding="utf-8")
        (source / ".gitignore").write_text("ignored", encoding="utf-8")
        return source, destination

    def _assert_unsafe_source_rejected(
        self,
        source: Path,
        destination: Path,
        *,
        before_copy: Callable[[], None] | None = None,
    ) -> None:
        with self.assertRaises(ValueError) as caught:
            copy_workflow_fixture(source, destination, before_copy=before_copy)
        self.assertEqual(str(caught.exception), "unsafe workflow fixture source")
        self.assertFalse(destination.exists())

    def test_copy_is_limited_to_validator_workflow_roots(self) -> None:
        with tempfile.TemporaryDirectory() as temporary_directory:
            temporary_root = Path(temporary_directory)
            source = temporary_root / "source"
            destination = temporary_root / "fixture"
            source.mkdir()
            allowed_directories = (".ai", ".codex", ".claude", "openspec", "scripts")
            allowed_files = ("AGENTS.md", "CLAUDE.md", ".gitignore")
            for name in allowed_directories:
                path = source / name
                path.mkdir()
                (path / "sentinel.txt").write_text(name, encoding="utf-8")
            for name in allowed_files:
                (source / name).write_text(name, encoding="utf-8")
            unrelated = source / "unrelated-business-data"
            unrelated.mkdir()
            (unrelated / "large-sensitive.bin").write_bytes(b"sensitive" * 1024)
            (source / ".env.secret").write_text("DO_NOT_COPY", encoding="utf-8")

            copy_workflow_fixture(source, destination)

            self.assertEqual(
                {path.name for path in destination.iterdir()},
                {*allowed_directories, *allowed_files},
            )
            for name in allowed_directories:
                self.assertEqual(
                    (destination / name / "sentinel.txt").read_text(encoding="utf-8"),
                    name,
                )
            for name in allowed_files:
                self.assertEqual(
                    (destination / name).read_text(encoding="utf-8"), name
                )

    def test_copy_accepts_a_single_assistant_source(self) -> None:
        with tempfile.TemporaryDirectory() as temporary_directory:
            temporary_root = Path(temporary_directory)
            for assistant, other in (("codex", "claude"), ("claude", "codex")):
                with self.subTest(assistant=assistant):
                    case_root = temporary_root / assistant
                    case_root.mkdir()
                    source, destination = self._single_assistant_source(
                        case_root, assistant
                    )

                    copy_workflow_fixture(source, destination)

                    entry = "AGENTS.md" if assistant == "codex" else "CLAUDE.md"
                    other_entry = "CLAUDE.md" if other == "claude" else "AGENTS.md"
                    self.assertTrue((destination / f".{assistant}").is_dir())
                    self.assertTrue((destination / entry).is_file())
                    self.assertFalse((destination / f".{other}").exists())
                    self.assertFalse((destination / other_entry).exists())


    def test_copy_rejects_a_top_level_directory_symlink(self) -> None:
        with tempfile.TemporaryDirectory() as temporary_directory:
            temporary_root = Path(temporary_directory)
            source, destination = self._single_assistant_source(temporary_root)
            shutil.rmtree(source / ".ai")
            outside = temporary_root / "outside"
            outside.mkdir()
            sentinel = outside / "sentinel"
            sentinel.write_text("DO_NOT_COPY", encoding="utf-8")
            (source / ".ai").symlink_to(outside, target_is_directory=True)

            self._assert_unsafe_source_rejected(source, destination)

            self.assertEqual(sentinel.read_text(encoding="utf-8"), "DO_NOT_COPY")

    def test_copy_rejects_a_top_level_dangling_symlink(self) -> None:
        with tempfile.TemporaryDirectory() as temporary_directory:
            temporary_root = Path(temporary_directory)
            source, destination = self._single_assistant_source(temporary_root)
            shutil.rmtree(source / ".ai")
            (source / ".ai").symlink_to(temporary_root / "missing")

            self._assert_unsafe_source_rejected(source, destination)

    def test_copy_rejects_a_nested_directory_symlink(self) -> None:
        with tempfile.TemporaryDirectory() as temporary_directory:
            temporary_root = Path(temporary_directory)
            source, destination = self._single_assistant_source(temporary_root)
            outside = temporary_root / "outside"
            outside.mkdir()
            sentinel = outside / "sentinel"
            sentinel.write_text("DO_NOT_COPY", encoding="utf-8")
            (source / ".ai/external").symlink_to(outside, target_is_directory=True)

            self._assert_unsafe_source_rejected(source, destination)

            self.assertEqual(sentinel.read_text(encoding="utf-8"), "DO_NOT_COPY")

    def test_copy_rejects_a_nested_file_symlink(self) -> None:
        with tempfile.TemporaryDirectory() as temporary_directory:
            temporary_root = Path(temporary_directory)
            source, destination = self._single_assistant_source(temporary_root)
            sentinel = temporary_root / "sentinel"
            sentinel.write_text("DO_NOT_COPY", encoding="utf-8")
            (source / ".ai/external").symlink_to(sentinel)

            self._assert_unsafe_source_rejected(source, destination)

            self.assertEqual(sentinel.read_text(encoding="utf-8"), "DO_NOT_COPY")

    def test_copy_rejects_symlinks_even_with_ignored_cache_names(self) -> None:
        for name in ("escape.pyc", "__pycache__"):
            with self.subTest(name=name), tempfile.TemporaryDirectory() as temporary:
                temporary_root = Path(temporary)
                source, destination = self._single_assistant_source(temporary_root)
                sentinel = temporary_root / "sentinel"
                sentinel.write_text("DO_NOT_COPY", encoding="utf-8")
                (source / ".ai" / name).symlink_to(sentinel)

                self._assert_unsafe_source_rejected(source, destination)

                self.assertEqual(sentinel.read_text(encoding="utf-8"), "DO_NOT_COPY")

    def test_copy_rejects_a_nested_fifo(self) -> None:
        with tempfile.TemporaryDirectory() as temporary_directory:
            temporary_root = Path(temporary_directory)
            source, destination = self._single_assistant_source(temporary_root)
            os.mkfifo(source / ".ai/sensitive-fifo")

            self._assert_unsafe_source_rejected(source, destination)

    def test_copy_rejects_a_symlink_replacement_after_preflight(self) -> None:
        with tempfile.TemporaryDirectory() as temporary_directory:
            temporary_root = Path(temporary_directory)
            source, destination = self._single_assistant_source(temporary_root)
            replaceable = source / ".ai/replaceable"
            replaceable.mkdir()
            (replaceable / "benign").write_text("safe", encoding="utf-8")
            outside = temporary_root / "outside"
            outside.mkdir()
            sentinel = outside / "sentinel"
            sentinel.write_text("DO_NOT_COPY", encoding="utf-8")

            def replace_after_preflight() -> None:
                shutil.rmtree(replaceable)
                replaceable.symlink_to(outside, target_is_directory=True)

            self._assert_unsafe_source_rejected(
                source, destination, before_copy=replace_after_preflight
            )

            self.assertEqual(sentinel.read_text(encoding="utf-8"), "DO_NOT_COPY")


if __name__ == "__main__":
    unittest.main()
