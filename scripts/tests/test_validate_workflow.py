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


class ContractFixtureTest(unittest.TestCase):
    """仅提供契约夹具与 stub 基建，不携带基类契约用例。

    需要复用夹具但不需要逐条重跑 ValidateWorkflowContractTest 的测试
    （如 fast 缓存契约）应继承本类，避免整模块执行时重复放大套件耗时。
    """

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
            "printf 'Ran 1 tests\\n\\nOK\\n'\n"
            "exit 0\n",
        )


class ValidateWorkflowContractTest(ContractFixtureTest):
    pass

    def _recording_python_script(self, marker: Path) -> str:
        return (
            "#!/bin/sh\n"
            "if test \"$1\" = \"-B\" && test \"$2\" = \"-c\"; then\n"
            f"  exec {sys.executable} \"$@\"\n"
            "fi\n"
            f"printf '%s\\n' \"$*\" >> {marker}\n"
            "printf 'Ran 1 tests\\n\\nOK\\n'\n"
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

    def test_retired_tool_scan_errors_fail_closed(self) -> None:
        real_grep = (self.stub_bin / "grep").resolve()
        (self.stub_bin / "grep").unlink()
        self._write_executable(
            "grep",
            "#!/bin/sh\n"
            "for argument in \"$@\"; do\n"
            "  if test \"$argument\" = \"multi_agent_v1__spawn_agent\"; then\n"
            "    exit 64\n"
            "  fi\n"
            "done\n"
            f"exec \"{real_grep}\" \"$@\"\n",
        )

        result = self._run_validator()

        self.assertNotEqual(result.returncode, 0, msg=result.stdout)
        self.assertIn("[FAIL] 废弃工具名零残留", result.stdout)

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

    def test_archive_index_does_not_require_gnu_find_printf(self) -> None:
        marker = self.stub_bin.parent / "gnu-find-argument-used"
        real_find = (self.stub_bin / "find").resolve()
        (self.stub_bin / "find").unlink()
        self._write_executable(
            "find",
            "#!/bin/sh\n"
            "for argument in \"$@\"; do\n"
            "  if test \"$argument\" = \"-printf\"; then\n"
            f"    : > \"{marker}\"\n"
            "    exit 64\n"
            "  fi\n"
            "done\n"
            f"exec \"{real_find}\" \"$@\"\n",
        )

        archive = self.fixture / "openspec/archive"
        backup = tempfile.TemporaryDirectory()
        self.addCleanup(backup.cleanup)
        shutil.move(str(archive), backup.name)
        archive.mkdir()
        readme = archive / "README.md"
        try:
            empty_result = self._run_validator()
            empty_used_gnu_find = marker.exists()
            marker.unlink(missing_ok=True)

            (archive / ".hidden-change").mkdir()
            readme.write_text(
                "# 归档变更索引\n\n- `.hidden-change` — 隐藏归档(严格)\n",
                encoding="utf-8",
            )
            populated_result = self._run_validator()
            populated_used_gnu_find = marker.exists()

            self.assertEqual(
                (0, False, 0, False),
                (
                    empty_result.returncode,
                    empty_used_gnu_find,
                    populated_result.returncode,
                    populated_used_gnu_find,
                ),
                msg=(
                    f"empty:\n{empty_result.stdout}\n"
                    f"populated:\n{populated_result.stdout}"
                ),
            )
        finally:
            shutil.rmtree(archive, ignore_errors=True)
            shutil.move(
                str(Path(backup.name) / "archive"), str(archive.parent)
            )

    def test_archive_index_rejects_all_directory_symlinks(self) -> None:
        archive = self.fixture / "openspec/archive"
        backup = tempfile.TemporaryDirectory()
        self.addCleanup(backup.cleanup)
        shutil.move(str(archive), backup.name)
        archive.mkdir()
        readme = archive / "README.md"
        outside = Path(self.temporary_directory.name) / "outside-archive"
        outside.mkdir()

        def write_index(*names: str) -> None:
            lines = "".join(f"- `{name}` — 受控归档(严格)\n" for name in names)
            readme.write_text(f"# 归档变更索引\n\n{lines}", encoding="utf-8")

        try:
            (archive / "real-change").mkdir()
            (archive / ".hidden-change").mkdir()
            base_names = ("real-change", ".hidden-change")
            write_index(*base_names)
            baseline = self._run_validator()

            internal_link = archive / "internal-link"
            internal_link.symlink_to("real-change", target_is_directory=True)
            write_index(*base_names, "internal-link")
            internal = self._run_validator()
            internal_link.unlink()

            external_link = archive / "external-link"
            external_link.symlink_to(outside, target_is_directory=True)
            write_index(*base_names, "external-link")
            external = self._run_validator()
            external_link.unlink()

            dangling_link = archive / "dangling-link"
            dangling_link.symlink_to("missing-change", target_is_directory=True)
            write_index(*base_names)
            dangling = self._run_validator()

            self.assertEqual(
                (0, True, True, True),
                (
                    baseline.returncode,
                    internal.returncode != 0,
                    external.returncode != 0,
                    dangling.returncode != 0,
                ),
                msg=(
                    f"baseline:\n{baseline.stdout}\n"
                    f"internal:\n{internal.stdout}\n"
                    f"external:\n{external.stdout}\n"
                    f"dangling:\n{dangling.stdout}"
                ),
            )
        finally:
            shutil.rmtree(archive, ignore_errors=True)
            shutil.move(
                str(Path(backup.name) / "archive"), str(archive.parent)
            )

    def test_archive_index_sort_errors_fail_closed(self) -> None:
        (self.stub_bin / "sort").unlink()
        self._write_executable("sort", "#!/bin/sh\nexit 64\n")

        result = self._run_validator()

        self.assertNotEqual(result.returncode, 0, msg=result.stdout)
        self.assertIn("[FAIL] 归档索引与目录 1:1", result.stdout)

    def _run_public_entry_with_contract_skip(
        self, *arguments: str
    ) -> subprocess.CompletedProcess[str]:
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
            "if test \"$1\" = \"-B\" && test \"$2\" = \"scripts/tests/run_validate_workflow_parallel.py\"; then\n"
            f"  exec {sys.executable} \"$@\"\n"
            "fi\n"
            "if test \"$1\" = \"-B\" && test \"$2\" = \"-m\" && test \"$4\" != \"discover\"; then\n"
            f"  exec {sys.executable} \"$@\"\n"
            "fi\n"
            "exit 0\n",
        )
        environment = {"LC_ALL": "C.UTF-8", "PATH": str(self.stub_bin)}
        return subprocess.run(
            ["/usr/bin/bash", "scripts/validate-workflow.sh", *arguments],
            cwd=self.fixture,
            env=environment,
            stdout=subprocess.PIPE,
            stderr=subprocess.STDOUT,
            text=True,
            check=False,
            timeout=120,
        )

    def test_public_entry_surfaces_contract_suite_internal_skips(self) -> None:
        result = self._run_public_entry_with_contract_skip()

        self.assertEqual(result.returncode, 0, msg=result.stdout)
        self.assertIn("契约套件内部设计性跳过 1 项", result.stdout)
        self.assertIn("source-repository only", result.stdout)
        # 汇总行保持末行，SKIP 字段不含套件内部跳过（openspec stub 仍为 1）。
        self.assertRegex(result.stdout, r"PASS=\d+ FAIL=0 SKIP=1\s*\Z")

    def test_public_entry_fails_closed_when_skip_count_grep_errors(self) -> None:
        real_grep = (self.stub_bin / "grep").resolve()
        (self.stub_bin / "grep").unlink()
        self._write_executable(
            "grep",
            "#!/bin/sh\n"
            "if test \"$1\" = \"-c\" && test \"$2\" = '\\.\\.\\. skipped'; then\n"
            "  exit 64\n"
            "fi\n"
            f"exec \"{real_grep}\" \"$@\"\n",
        )

        result = self._run_public_entry_with_contract_skip()

        self.assertNotEqual(result.returncode, 0, msg=result.stdout)
        self.assertIn("[FAIL] 契约套件内部跳过计数解析失败", result.stdout)

    def test_public_entry_fails_closed_when_skip_count_is_not_an_integer(self) -> None:
        real_grep = (self.stub_bin / "grep").resolve()
        (self.stub_bin / "grep").unlink()
        self._write_executable(
            "grep",
            "#!/bin/sh\n"
            "if test \"$1\" = \"-c\" && test \"$2\" = '\\.\\.\\. skipped'; then\n"
            "  printf 'x\\n'\n"
            "  exit 0\n"
            "fi\n"
            f"exec \"{real_grep}\" \"$@\"\n",
        )

        result = self._run_public_entry_with_contract_skip()

        self.assertNotEqual(result.returncode, 0, msg=result.stdout)
        self.assertIn("[FAIL] 契约套件内部跳过计数解析失败", result.stdout)

    def test_public_entry_fails_closed_when_skip_render_sed_errors(self) -> None:
        real_sed = (self.stub_bin / "sed").resolve()
        (self.stub_bin / "sed").unlink()
        self._write_executable(
            "sed",
            "#!/bin/sh\n"
            "for argument in \"$@\"; do\n"
            "  case \"$argument\" in\n"
            "    *'s/^/  - /'*) exit 64 ;;\n"
            "  esac\n"
            "done\n"
            f"exec \"{real_sed}\" \"$@\"\n",
        )

        result = self._run_public_entry_with_contract_skip()

        self.assertNotEqual(result.returncode, 0, msg=result.stdout)
        self.assertIn("[FAIL] 契约套件内部跳过明细渲染失败", result.stdout)


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


class FastValidationCacheTest(ContractFixtureTest):
    """--fast 输入指纹缓存：命中沿用、变化重跑、FAIL 不缓存、异常按未命中。"""

    CACHE_DIR = Path(".ai-local") / "validation-cache"
    PROJECT_FACTS_LABEL = "事实工具必需测试（registry/边界/分页/ignore）"
    REVIEW_MANIFEST_LABEL = "Review manifest 必需测试（freeze/STALE/delta）"
    OPENSPEC_LABEL = "OpenSpec validate --all --no-interactive"

    def _run_core(self, *, cache_enabled: bool) -> subprocess.CompletedProcess[str]:
        environment = {
            "LC_ALL": "C.UTF-8",
            "PATH": str(self.stub_bin),
        }
        if cache_enabled:
            environment["WORKFLOW_FAST_CACHE"] = "1"
        return subprocess.run(
            ["/usr/bin/bash", "scripts/lib/validate-workflow-core.sh"],
            cwd=self.fixture,
            env=environment,
            stdout=subprocess.PIPE,
            stderr=subprocess.STDOUT,
            text=True,
            check=False,
            timeout=30,
        )

    def _cache_file(self, check_id: str) -> Path:
        return self.fixture / self.CACHE_DIR / f"{check_id}.cache"

    def _cache_files(self) -> list[Path]:
        directory = self.fixture / self.CACHE_DIR
        if not directory.is_dir():
            return []
        return sorted(path for path in directory.iterdir() if path.is_file())

    def test_cache_hit_annotates_and_reuses(self) -> None:
        first = self._run_core(cache_enabled=True)
        self.assertEqual(0, first.returncode, msg=first.stdout)
        self.assertIn(f"[PASS] {self.PROJECT_FACTS_LABEL}", first.stdout)
        self.assertIn(f"[PASS] {self.REVIEW_MANIFEST_LABEL}", first.stdout)
        self.assertNotIn("沿用", first.stdout)
        self.assertTrue(self._cache_file("project-facts-required-tests").is_file())
        self.assertTrue(self._cache_file("review-manifest-required-tests").is_file())

        second = self._run_core(cache_enabled=True)
        self.assertEqual(0, second.returncode, msg=second.stdout)
        self.assertIn(f"[PASS] {self.PROJECT_FACTS_LABEL}（指纹", second.stdout)
        self.assertIn("未变，沿用", second.stdout)
        self.assertIn("实际执行结果）", second.stdout)
        self.assertIn(f"[PASS] {self.REVIEW_MANIFEST_LABEL}（指纹", second.stdout)

    def test_input_change_invalidates_only_affected_check(self) -> None:
        first = self._run_core(cache_enabled=True)
        self.assertEqual(0, first.returncode, msg=first.stdout)
        facts_tool = self.fixture / ".ai" / "tools" / "project_facts.py"
        facts_tool.write_text(
            facts_tool.read_text(encoding="utf-8") + "\n# cache invalidation probe\n",
            encoding="utf-8",
        )

        second = self._run_core(cache_enabled=True)
        self.assertEqual(0, second.returncode, msg=second.stdout)
        self.assertIn(f"[PASS] {self.PROJECT_FACTS_LABEL}\n", second.stdout)
        self.assertNotIn(f"{self.PROJECT_FACTS_LABEL}（指纹", second.stdout)
        self.assertIn(f"[PASS] {self.REVIEW_MANIFEST_LABEL}（指纹", second.stdout)

    def test_failed_check_never_writes_cache(self) -> None:
        self._write_executable(
            "python3",
            "#!/bin/sh\n"
            "if test \"$1\" = \"-B\" && test \"$2\" = \"-c\"; then\n"
            f"  exec {sys.executable} \"$@\"\n"
            "fi\n"
            "exit 1\n",
        )
        result = self._run_core(cache_enabled=True)
        self.assertNotEqual(0, result.returncode, msg=result.stdout)
        self.assertIn(f"[FAIL] {self.PROJECT_FACTS_LABEL}", result.stdout)
        self.assertIn(f"[FAIL] {self.REVIEW_MANIFEST_LABEL}", result.stdout)
        self.assertEqual([], self._cache_files())

    def test_failed_execution_clears_existing_cache(self) -> None:
        first = self._run_core(cache_enabled=True)
        self.assertEqual(0, first.returncode, msg=first.stdout)
        facts_cache = self._cache_file("project-facts-required-tests")
        review_cache = self._cache_file("review-manifest-required-tests")
        self.assertTrue(facts_cache.is_file())
        self.assertTrue(review_cache.is_file())

        facts_tool = self.fixture / ".ai" / "tools" / "project_facts.py"
        original_facts = facts_tool.read_bytes()
        facts_tool.write_text(
            original_facts.decode("utf-8") + "\n# failing input probe\n",
            encoding="utf-8",
        )
        review_tool = self.fixture / ".ai" / "tools" / "review_manifest.py"
        original_review = review_tool.read_bytes()
        review_tool.write_text(
            original_review.decode("utf-8") + "\n# failing input probe\n",
            encoding="utf-8",
        )
        self._write_executable(
            "python3",
            "#!/bin/sh\n"
            "if test \"$1\" = \"-B\" && test \"$2\" = \"-c\"; then\n"
            f"  exec {sys.executable} \"$@\"\n"
            "fi\n"
            "exit 1\n",
        )
        failed = self._run_core(cache_enabled=True)
        self.assertNotEqual(0, failed.returncode, msg=failed.stdout)
        self.assertFalse(facts_cache.exists())
        self.assertFalse(review_cache.exists())

        facts_tool.write_bytes(original_facts)
        review_tool.write_bytes(original_review)
        self._enable_profile_parser()
        restored = self._run_core(cache_enabled=True)
        self.assertEqual(0, restored.returncode, msg=restored.stdout)
        self.assertIn(f"[PASS] {self.PROJECT_FACTS_LABEL}\n", restored.stdout)
        self.assertNotIn(f"{self.PROJECT_FACTS_LABEL}（指纹", restored.stdout)
        self.assertNotIn(f"{self.REVIEW_MANIFEST_LABEL}（指纹", restored.stdout)
        self.assertTrue(facts_cache.is_file())
        self.assertTrue(review_cache.is_file())

    def test_missing_input_disables_cache_entry(self) -> None:
        kb_readme = self.fixture / ".ai" / "kb" / "projects" / "README.md"
        kb_readme.unlink()
        for run in ("first", "second"):
            with self.subTest(run=run):
                result = self._run_core(cache_enabled=True)
                self.assertEqual(0, result.returncode, msg=result.stdout)
                self.assertIn(f"[PASS] {self.PROJECT_FACTS_LABEL}\n", result.stdout)
                self.assertNotIn(f"{self.PROJECT_FACTS_LABEL}（指纹", result.stdout)
        self.assertFalse(self._cache_file("project-facts-required-tests").exists())
        self.assertTrue(self._cache_file("review-manifest-required-tests").is_file())

    def test_corrupt_cache_reruns_execution(self) -> None:
        first = self._run_core(cache_enabled=True)
        self.assertEqual(0, first.returncode, msg=first.stdout)
        facts_cache = self._cache_file("project-facts-required-tests")
        facts_cache.write_text("garbage-without-valid-format", encoding="utf-8")

        second = self._run_core(cache_enabled=True)
        self.assertEqual(0, second.returncode, msg=second.stdout)
        self.assertIn(f"[PASS] {self.PROJECT_FACTS_LABEL}\n", second.stdout)
        self.assertNotIn(f"{self.PROJECT_FACTS_LABEL}（指纹", second.stdout)
        self.assertIn(f"[PASS] {self.REVIEW_MANIFEST_LABEL}（指纹", second.stdout)
        lines = facts_cache.read_text(encoding="utf-8").splitlines()
        self.assertEqual(2, len(lines))
        self.assertRegex(lines[0], r"^[0-9a-f]{64}$")
        self.assertRegex(lines[1], r"^\d{4}-\d{2}-\d{2}T\d{2}:\d{2}:\d{2}Z$")

    def test_cache_ignored_without_fast_env(self) -> None:
        first = self._run_core(cache_enabled=True)
        self.assertEqual(0, first.returncode, msg=first.stdout)
        facts_cache = self._cache_file("project-facts-required-tests")
        before = facts_cache.read_bytes()

        second = self._run_core(cache_enabled=False)
        self.assertEqual(0, second.returncode, msg=second.stdout)
        self.assertIn(f"[PASS] {self.PROJECT_FACTS_LABEL}", second.stdout)
        self.assertNotIn("沿用", second.stdout)
        self.assertNotIn("指纹", second.stdout)
        self.assertEqual(before, facts_cache.read_bytes())

    def test_openspec_check_cached_when_cli_present(self) -> None:
        self._write_executable("openspec", "#!/bin/sh\nexit 0\n")
        first = self._run_core(cache_enabled=True)
        self.assertEqual(0, first.returncode, msg=first.stdout)
        self.assertIn(f"[PASS] {self.OPENSPEC_LABEL}", first.stdout)
        self.assertNotIn(f"{self.OPENSPEC_LABEL}（指纹", first.stdout)
        openspec_cache = self._cache_file("openspec-validate")
        self.assertTrue(openspec_cache.is_file())

        second = self._run_core(cache_enabled=True)
        self.assertEqual(0, second.returncode, msg=second.stdout)
        self.assertIn(f"[PASS] {self.OPENSPEC_LABEL}（指纹", second.stdout)

        self._write_executable("openspec", "#!/bin/sh\nexit 1\n")
        for cache_file in self._cache_files():
            cache_file.unlink()
        third = self._run_core(cache_enabled=True)
        self.assertNotEqual(0, third.returncode, msg=third.stdout)
        self.assertIn(f"[FAIL] {self.OPENSPEC_LABEL}", third.stdout)
        self.assertFalse(openspec_cache.is_file())

    def test_openspec_change_file_invalidates_cache(self) -> None:
        self._write_executable("openspec", "#!/bin/sh\nexit 0\n")
        active_change = self._prepare_active_change_probe()
        first = self._run_core(cache_enabled=True)
        self.assertEqual(0, first.returncode, msg=first.stdout)
        active_change.write_text(
            active_change.read_text(encoding="utf-8") + "\n# cache probe\n",
            encoding="utf-8",
        )

        second = self._run_core(cache_enabled=True)
        self.assertEqual(0, second.returncode, msg=second.stdout)
        self.assertIn(f"[PASS] {self.OPENSPEC_LABEL}\n", second.stdout)
        self.assertNotIn(f"{self.OPENSPEC_LABEL}（指纹", second.stdout)

    def _prepare_active_change_probe(self) -> Path:
        """返回夹具内一个活跃 change 的 proposal；无活跃变更时自建合法最小变更。

        测试不得依赖源仓某个特定变更仍处于 openspec/changes/（归档后会漂移）。
        """
        changes = self.fixture / "openspec" / "changes"
        for candidate in sorted(changes.glob("*/proposal.md")):
            return candidate
        probe = changes / "cache-fixture-change"
        probe.mkdir(parents=True)
        proposal = probe / "proposal.md"
        proposal.write_text(
            "# 变更：缓存夹具探针\n\n模式: 标准\n状态: 待确认计划\n",
            encoding="utf-8",
        )
        return proposal

    def test_core_change_invalidates_cache(self) -> None:
        first = self._run_core(cache_enabled=True)
        self.assertEqual(0, first.returncode, msg=first.stdout)
        core_script = self.fixture / "scripts" / "lib" / "validate-workflow-core.sh"
        core_script.write_text(
            core_script.read_text(encoding="utf-8") + "\n# cache probe\n",
            encoding="utf-8",
        )

        second = self._run_core(cache_enabled=True)
        self.assertEqual(0, second.returncode, msg=second.stdout)
        self.assertIn(f"[PASS] {self.PROJECT_FACTS_LABEL}\n", second.stdout)
        self.assertNotIn(f"{self.PROJECT_FACTS_LABEL}（指纹", second.stdout)
        self.assertNotIn(f"{self.REVIEW_MANIFEST_LABEL}（指纹", second.stdout)
        self.assertTrue(self._cache_file("project-facts-required-tests").is_file())
        self.assertTrue(self._cache_file("review-manifest-required-tests").is_file())


class WrapperFastCacheSwitchTest(unittest.TestCase):
    """wrapper 只在 --fast 路径向 core 注入 WORKFLOW_FAST_CACHE=1。"""

    STUB_CORE = (
        "#!/usr/bin/env bash\n"
        "printf 'WORKFLOW_FAST_CACHE=%s\\n' \"${WORKFLOW_FAST_CACHE:-0}\"\n"
        "printf 'INTERNAL_RESULT PASS=1 FAIL=0 SKIP=0\\n'\n"
        "exit 0\n"
    )

    def _stage_tree(self, root: Path) -> None:
        (root / "scripts" / "lib").mkdir(parents=True)
        (root / "scripts" / "lib" / "validate-workflow-core.sh").write_text(
            self.STUB_CORE, encoding="utf-8"
        )
        (root / "scripts" / "validate-workflow.sh").write_text(
            (REPOSITORY_ROOT / "scripts" / "validate-workflow.sh").read_text(
                encoding="utf-8"
            ),
            encoding="utf-8",
        )
        tests = root / "scripts" / "tests"
        tests.mkdir()
        (tests / "test_validate_workflow.py").write_text(
            "import unittest\n"
            "class SentinelTest(unittest.TestCase):\n"
            "    def test_sentinel(self) -> None: self.assertTrue(True)\n",
            encoding="utf-8",
        )
        runner_source = (
            REPOSITORY_ROOT / "scripts" / "tests" / "run_validate_workflow_parallel.py"
        )
        (tests / "run_validate_workflow_parallel.py").write_text(
            runner_source.read_text(encoding="utf-8")
            if runner_source.is_file()
            else "import unittest\n",
            encoding="utf-8",
        )
        (root / ".ai-local").mkdir()

    def _run_wrapper(
        self,
        *arguments: str,
        environment_extras: dict[str, str] | None = None,
    ) -> subprocess.CompletedProcess[str]:
        with tempfile.TemporaryDirectory() as temporary:
            root = Path(temporary)
            self._stage_tree(root)
            environment = os.environ.copy()
            environment.update(environment_extras or {})
            return subprocess.run(
                ["/usr/bin/bash", str(root / "scripts" / "validate-workflow.sh"), *arguments],
                cwd=root,
                env=environment,
                stdout=subprocess.PIPE,
                stderr=subprocess.STDOUT,
                text=True,
                check=False,
                timeout=60,
            )

    def test_fast_mode_enables_cache_env(self) -> None:
        result = self._run_wrapper("--fast")
        self.assertEqual(0, result.returncode, msg=result.stdout)
        self.assertIn("WORKFLOW_FAST_CACHE=1", result.stdout)

    def test_default_mode_does_not_enable_cache_env(self) -> None:
        result = self._run_wrapper()
        self.assertEqual(0, result.returncode, msg=result.stdout)
        self.assertIn("WORKFLOW_FAST_CACHE=0", result.stdout)
        self.assertIn("[PASS] 工作流顶层契约测试", result.stdout)

    def test_inherited_cache_env_is_neutralized_outside_fast(self) -> None:
        inherited = {"WORKFLOW_FAST_CACHE": "1"}
        for arguments in ((), ("--require-openspec",), ("--archive-light",)):
            with self.subTest(arguments=arguments or ("default",)):
                result = self._run_wrapper(*arguments, environment_extras=inherited)
                self.assertEqual(0, result.returncode, msg=result.stdout)
                self.assertIn("WORKFLOW_FAST_CACHE=0", result.stdout)

    def test_conflicting_mode_combinations_fail_closed(self) -> None:
        for arguments in (
            ("--fast", "--require-openspec"),
            ("--fast", "--archive-light"),
        ):
            with self.subTest(arguments=arguments):
                result = self._run_wrapper(*arguments)
                self.assertEqual(2, result.returncode, msg=result.stdout)
                self.assertIn("conflict", result.stdout.lower())


class WrapperRequiredForwardingTest(unittest.TestCase):
    def test_wrapper_forwards_required_openspec_to_core(self) -> None:
        with tempfile.TemporaryDirectory() as temporary:
            root = Path(temporary)
            scripts = root / "scripts"
            core = scripts / "lib" / "validate-workflow-core.sh"
            core.parent.mkdir(parents=True)
            core.write_text(
                "#!/usr/bin/env bash\n"
                "case \" $* \" in\n"
                "  *' --require-openspec '*)"
                " printf 'CORE_RECEIVED_REQUIRED=1\\n' ;;\n"
                "  *) printf 'CORE_RECEIVED_REQUIRED=0\\n' ;;\n"
                "esac\n"
                "printf 'INTERNAL_RESULT PASS=1 FAIL=0 SKIP=0\\n'\n",
                encoding="utf-8",
            )
            (scripts / "validate-workflow.sh").write_text(
                (REPOSITORY_ROOT / "scripts" / "validate-workflow.sh").read_text(
                    encoding="utf-8"
                ),
                encoding="utf-8",
            )
            tests = scripts / "tests"
            tests.mkdir()
            (tests / "run_validate_workflow_parallel.py").write_text(
                "print('Ran 1 tests\\n\\nOK')\n",
                encoding="utf-8",
            )
            environment = {"LC_ALL": "C.UTF-8", "PATH": os.environ.get("PATH", "")}
            result = subprocess.run(
                ["/usr/bin/bash", "scripts/validate-workflow.sh", "--require-openspec"],
                cwd=root,
                env=environment,
                stdout=subprocess.PIPE,
                stderr=subprocess.STDOUT,
                text=True,
                check=False,
                timeout=30,
            )

            self.assertEqual(0, result.returncode, msg=result.stdout)
            self.assertIn("CORE_RECEIVED_REQUIRED=1", result.stdout)
            self.assertIn("[PASS] 工作流顶层契约测试（1 tests）", result.stdout)


class ParallelContractRunnerTest(unittest.TestCase):
    """契约套件并行执行器：结果聚合、失败传播、跳过兼容与 worker 崩溃隔离。"""

    RUNNER = REPOSITORY_ROOT / "scripts" / "tests" / "run_validate_workflow_parallel.py"

    def _write_module(self, root: Path, body: str) -> None:
        (root / "sample_tests.py").write_text(body, encoding="utf-8")

    def _run_runner(self, root: Path, *arguments: str) -> subprocess.CompletedProcess[str]:
        self.assertTrue(self.RUNNER.is_file(), msg="parallel runner asset is missing")
        environment = os.environ.copy()
        environment["PYTHONPATH"] = str(root)
        return subprocess.run(
            [sys.executable, "-B", str(self.RUNNER), *arguments],
            cwd=root,
            env=environment,
            stdout=subprocess.PIPE,
            stderr=subprocess.STDOUT,
            text=True,
            check=False,
            timeout=60,
        )

    def _stage_wrapper_tree(
        self,
        root: Path,
        *,
        stub_runner: bool,
        contract_summary: str = "Ran 1 tests\n",
    ) -> Path:
        (root / "scripts" / "lib").mkdir(parents=True)
        (root / "scripts" / "lib" / "validate-workflow-core.sh").write_text(
            "#!/usr/bin/env bash\nprintf 'INTERNAL_RESULT PASS=1 FAIL=0 SKIP=0\\n'\nexit 0\n",
            encoding="utf-8",
        )
        (root / "scripts" / "validate-workflow.sh").write_text(
            (REPOSITORY_ROOT / "scripts" / "validate-workflow.sh").read_text(
                encoding="utf-8"
            ),
            encoding="utf-8",
        )
        tests = root / "scripts" / "tests"
        tests.mkdir()
        (tests / "test_validate_workflow.py").write_text(
            "import unittest\n"
            "class SentinelTest(unittest.TestCase):\n"
            "    def test_sentinel(self) -> None: self.assertTrue(True)\n",
            encoding="utf-8",
        )
        marker = root / "runner-called"
        if stub_runner:
            (tests / "run_validate_workflow_parallel.py").write_text(
                "#!/usr/bin/env python3\n"
                "from pathlib import Path\n"
                "Path(__file__).resolve().parents[2].joinpath('runner-called').write_text('1')\n"
                f"print({contract_summary!r}, end='')\n",
                encoding="utf-8",
            )
        else:
            (tests / "run_validate_workflow_parallel.py").write_text(
                self.RUNNER.read_text(encoding="utf-8"), encoding="utf-8"
            )
        (root / ".ai-local").mkdir()
        return marker

    def _run_staged_wrapper(
        self,
        *,
        stub_runner: bool,
        contract_summary: str = "Ran 1 tests\n",
        environment_extras: dict[str, str] | None = None,
    ) -> tuple[subprocess.CompletedProcess[str], bool]:
        with tempfile.TemporaryDirectory() as temporary:
            root = Path(temporary)
            marker = self._stage_wrapper_tree(
                root,
                stub_runner=stub_runner,
                contract_summary=contract_summary,
            )
            environment = os.environ.copy()
            environment.update(environment_extras or {})
            result = subprocess.run(
                [
                    "/usr/bin/bash",
                    str(root / "scripts" / "validate-workflow.sh"),
                ],
                cwd=root,
                env=environment,
                stdout=subprocess.PIPE,
                stderr=subprocess.STDOUT,
                text=True,
                check=False,
                timeout=60,
            )
            return result, marker.is_file()

    def test_mixed_results_propagate(self) -> None:
        with tempfile.TemporaryDirectory() as temporary:
            root = Path(temporary)
            self._write_module(
                root,
                "import unittest\n"
                "class SampleTests(unittest.TestCase):\n"
                "    def test_ok(self) -> None: self.assertTrue(True)\n"
                "    def test_fail(self) -> None: self.fail('intentional failure')\n"
                "    @unittest.skip('planned skip')\n"
                "    def test_skipped(self) -> None: self.assertTrue(False)\n",
            )
            result = self._run_runner(root, "--module", "sample_tests", "--jobs", "2")
            self.assertNotEqual(0, result.returncode, msg=result.stdout)
            self.assertIn("test_ok ... ok", result.stdout)
            self.assertIn("test_fail ... FAIL", result.stdout)
            self.assertIn("test_skipped ... skipped", result.stdout)
            self.assertIn("Ran 3 tests", result.stdout)
            self.assertIn("FAILED (failures=1, skipped=1)", result.stdout)
            self.assertIn("AssertionError", result.stdout)

    def test_skip_output_remains_parseable(self) -> None:
        with tempfile.TemporaryDirectory() as temporary:
            root = Path(temporary)
            self._write_module(
                root,
                "import unittest\n"
                "class SampleTests(unittest.TestCase):\n"
                "    def test_ok(self) -> None: self.assertTrue(True)\n"
                "    @unittest.skip('planned skip')\n"
                "    def test_skipped(self) -> None: self.assertTrue(False)\n",
            )
            result = self._run_runner(root, "--module", "sample_tests", "--jobs", "2")
            self.assertEqual(0, result.returncode, msg=result.stdout)
            self.assertIn(" ... skipped", result.stdout)
            self.assertIn("Ran 2 tests", result.stdout)
            self.assertIn("OK (skipped=1)", result.stdout)

    def test_worker_crash_becomes_error(self) -> None:
        with tempfile.TemporaryDirectory() as temporary:
            root = Path(temporary)
            self._write_module(
                root,
                "import os\n"
                "import unittest\n"
                "class CrashTests(unittest.TestCase):\n"
                "    def test_ok(self) -> None: self.assertTrue(True)\n"
                "    def test_crash(self) -> None: os._exit(3)\n",
            )
            result = self._run_runner(root, "--module", "sample_tests", "--jobs", "2")
            self.assertNotEqual(0, result.returncode, msg=result.stdout)
            self.assertIn("test_crash ... ERROR", result.stdout)
            self.assertIn("test_ok ... ok", result.stdout)
            self.assertIn("Ran 2 tests", result.stdout)
            self.assertIn("errors=1", result.stdout)

    def test_expected_failure_semantics_match_unittest(self) -> None:
        with tempfile.TemporaryDirectory() as temporary:
            root = Path(temporary)
            self._write_module(
                root,
                "import unittest\n"
                "class SampleTests(unittest.TestCase):\n"
                "    def test_ok(self) -> None: self.assertTrue(True)\n"
                "    @unittest.expectedFailure\n"
                "    def test_expected_fail(self) -> None: self.fail('planned')\n"
                "    @unittest.expectedFailure\n"
                "    def test_unexpected_pass(self) -> None: self.assertTrue(True)\n",
            )
            reference = subprocess.run(
                [sys.executable, "-B", "-m", "unittest", "-v", "sample_tests"],
                cwd=root,
                env={**os.environ, "PYTHONPATH": str(root)},
                stdout=subprocess.PIPE,
                stderr=subprocess.STDOUT,
                text=True,
                check=False,
                timeout=60,
            )
            self.assertNotEqual(0, reference.returncode, msg=reference.stdout)

            result = self._run_runner(root, "--module", "sample_tests", "--jobs", "2")
            self.assertNotEqual(0, result.returncode, msg=result.stdout)
            self.assertIn("Ran 3 tests", result.stdout)
            self.assertIn("UNEXPECTEDLY SUCCEEDED", result.stdout)
            self.assertIn("EXPECTED FAILURE", result.stdout)
            self.assertIn("expected failures=1", result.stdout)
            self.assertIn("unexpected successes=1", result.stdout)

    def test_wrapper_uses_runner_by_default(self) -> None:
        result, runner_called = self._run_staged_wrapper(stub_runner=True)
        self.assertEqual(0, result.returncode, msg=result.stdout)
        self.assertTrue(runner_called, msg=result.stdout)
        self.assertIn("[PASS] 工作流顶层契约测试（1 tests）", result.stdout)

    def test_wrapper_reports_contract_test_count_by_default(self) -> None:
        result, _ = self._run_staged_wrapper(stub_runner=True)
        self.assertEqual(0, result.returncode, msg=result.stdout)
        self.assertIn("[PASS] 工作流顶层契约测试（1 tests）", result.stdout)
        self.assertIn("PASS=2 FAIL=0 SKIP=0", result.stdout)

    def test_wrapper_jobs_one_falls_back_to_unittest(self) -> None:
        result, runner_called = self._run_staged_wrapper(
            stub_runner=True, environment_extras={"WORKFLOW_TEST_JOBS": "1"}
        )
        self.assertEqual(0, result.returncode, msg=result.stdout)
        self.assertFalse(runner_called, msg=result.stdout)
        self.assertIn("[PASS] 工作流顶层契约测试（1 tests）", result.stdout)

    def test_wrapper_fails_closed_when_contract_success_count_is_missing(self) -> None:
        result, runner_called = self._run_staged_wrapper(
            stub_runner=True, contract_summary=""
        )
        self.assertNotEqual(0, result.returncode, msg=result.stdout)
        self.assertTrue(runner_called, msg=result.stdout)
        self.assertIn("[FAIL] 契约套件用例计数解析失败", result.stdout)
        self.assertNotIn("[PASS] 工作流顶层契约测试", result.stdout)
        self.assertIn("PASS=1 FAIL=1 SKIP=0", result.stdout)


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




class MutationStandardThreePieceSuiteTest(unittest.TestCase):
    """mutation: AGENTS.md 必须明确"标准模式三件套+条件 design"，注入旧四件套契约必须被检出。"""

    AGENTS_MD = REPOSITORY_ROOT / "AGENTS.md"
    OPEN_SKILL = REPOSITORY_ROOT / ".codex" / "skills" / "open" / "SKILL.md"

    def _require_agents_md(self) -> None:
        if not self.AGENTS_MD.is_file():
            self.skipTest("codex assistant is not present in this fixture")

    def _require_codex_skill(self, path: Path) -> Path:
        if not path.is_file():
            self.skipTest("codex assistant is not present in this fixture")
        return path

    def test_agents_md_states_three_piece_suite(self) -> None:
        self._require_agents_md()
        text = self.AGENTS_MD.read_text(encoding="utf-8")
        self.assertIn("三件套", text, "AGENTS.md 必须明确三件套")
        # 旧四件套契约不应再存在
        self.assertNotIn("Open 一次创建 `proposal.md`、delta `spec.md`、`design.md`、含精确步骤和命令的 `tasks.md`", text,
                         "AGENTS.md 不应再保留旧四件套契约")

    def test_open_skill_states_three_piece_suite(self) -> None:
        open_skill = self._require_codex_skill(self.OPEN_SKILL)
        text = open_skill.read_text(encoding="utf-8")
        self.assertIn("三件套", text, ".codex/skills/open/SKILL.md 必须明确三件套")
        self.assertNotIn("一次产出可执行四件套", text,
                         ".codex/skills/open/SKILL.md 不应再保留旧四件套契约")

    def test_design_skill_states_risk_triggered_second_confirm(self) -> None:
        design_skill = REPOSITORY_ROOT / ".codex" / "skills" / "design" / "SKILL.md"
        design_skill = self._require_codex_skill(design_skill)
        text = design_skill.read_text(encoding="utf-8")
        self.assertIn("不可逆风险触发", text,
                      ".codex/skills/design/SKILL.md 必须含'不可逆风险触发'段")
        for keyword in ["权限认证", "资金账务", "数据库 Schema", "数据删除", "破坏性动作", "外部副作用"]:
            self.assertIn(keyword, text, f"design SKILL.md 必须含硬风险关键词: {keyword}")


class WorkflowSemanticSyncTest(unittest.TestCase):
    """mutation: 双入口、用户文档与安装资产不得回流旧流程口径。"""

    SOURCE_ENTRYPOINTS = (
        REPOSITORY_ROOT / "AGENTS.md",
        REPOSITORY_ROOT / "CLAUDE.md",
    )
    SOURCE_DOCUMENTS = (
        REPOSITORY_ROOT / "README.md",
        REPOSITORY_ROOT / "docs" / "ai-workflow-intro.md",
        REPOSITORY_ROOT / "openspec" / "project.md",
        REPOSITORY_ROOT / ".ai" / "kb" / "overview.md",
    )
    SOURCE_BEHAVIOR_FILES = (
        REPOSITORY_ROOT / "openspec" / "specs" / "risk-tiered-ai-workflow" / "spec.md",
        REPOSITORY_ROOT / ".codex" / "skills" / "build" / "SKILL.md",
        REPOSITORY_ROOT / ".claude" / "skills" / "build" / "SKILL.md",
        REPOSITORY_ROOT / "scripts" / "workflow-pressure-scenarios.md",
    )
    ASSET_SEMANTIC_FILES = (
        REPOSITORY_ROOT / "scripts" / "ai-workflow-assets" / "claude" / "CLAUDE.md",
        REPOSITORY_ROOT / "scripts" / "ai-workflow-assets" / "codex" / "AGENTS.md",
        REPOSITORY_ROOT / "scripts" / "ai-workflow-assets" / "codex" / ".codex" / "README.md",
        REPOSITORY_ROOT / "scripts" / "ai-workflow-assets" / "shared" / ".ai" / "kb" / "overview.md",
        REPOSITORY_ROOT / "scripts" / "ai-workflow-assets" / "shared" / "openspec" / "project.md",
        REPOSITORY_ROOT / "scripts" / "ai-workflow-assets" / "shared" / "openspec" / "specs" / "risk-tiered-ai-workflow" / "spec.md",
        REPOSITORY_ROOT / "scripts" / "ai-workflow-assets" / "claude" / ".claude" / "skills" / "build" / "SKILL.md",
        REPOSITORY_ROOT / "scripts" / "ai-workflow-assets" / "codex" / ".codex" / "skills" / "build" / "SKILL.md",
        REPOSITORY_ROOT / "scripts" / "ai-workflow-assets" / "shared" / "scripts" / "workflow-pressure-scenarios.md",
    )
    OLD_STANDARD_PHRASES = (
        "Open 一次产出可执行四件套",
        "Open 一次产出四件套",
        "标准模式使用四件套",
        "一次产出含可执行步骤的四件套",
        "区分本变更四件套",
        "落入 OpenSpec 四件套",
        "均已获两次确认",
        "每任务规格与质量审查",
    )
    OLD_ARCHIVE_PHRASES = (
        "归档后跑全量",
        "Archive 永远复用 Verify 结果",
        "标准模式 Archive 归档后验证 SHALL 运行全量默认门禁",
        "严格模式 Verify 与 Archive SHALL 始终运行",
    )

    @property
    def _is_source_repository(self) -> bool:
        return (
            REPOSITORY_ROOT / "scripts" / "lib" / "install_ai_workflow.py"
        ).is_file()

    def _require_source_file(self, path: Path) -> None:
        if not path.is_file():
            self.skipTest(f"selected installation does not ship {path.name}")

    def test_entrypoints_and_user_documents_use_current_semantics(self) -> None:
        paths = [*self.SOURCE_ENTRYPOINTS, *self.SOURCE_DOCUMENTS]
        for path in paths:
            with self.subTest(path=path):
                self._require_source_file(path)
                text = path.read_text(encoding="utf-8")
                self.assertIn("三件套", text, f"{path} 必须说明标准三件套")
                self.assertIn(
                    "硬风险", text,
                    f"{path} 必须说明严格二次确认由硬风险触发",
                )
                for phrase in (*self.OLD_STANDARD_PHRASES, *self.OLD_ARCHIVE_PHRASES):
                    self.assertNotIn(
                        phrase, text,
                        f"{path} 保留旧流程口径: {phrase}",
                    )

    def test_source_behavior_surfaces_use_current_semantics(self) -> None:
        required_tokens = {
            self.SOURCE_BEHAVIOR_FILES[0]: ("Archive SHALL 按", "计划风险触发"),
            self.SOURCE_BEHAVIOR_FILES[1]: ("实际 OpenSpec 产物", "高风险实现单元审查"),
            self.SOURCE_BEHAVIOR_FILES[2]: ("实际 OpenSpec 产物", "高风险实现单元审查"),
            self.SOURCE_BEHAVIOR_FILES[3]: ("硬风险", "proposal、delta spec"),
        }
        for path in self.SOURCE_BEHAVIOR_FILES:
            with self.subTest(path=path):
                self._require_source_file(path)
                text = path.read_text(encoding="utf-8")
                for token in required_tokens[path]:
                    self.assertIn(token, text, f"{path} 缺少语义锚点: {token}")
                for phrase in (*self.OLD_STANDARD_PHRASES, *self.OLD_ARCHIVE_PHRASES):
                    self.assertNotIn(
                        phrase, text,
                        f"{path} 保留旧流程口径: {phrase}",
                    )

    def test_active_openspec_names_do_not_collide_with_archive(self) -> None:
        if not self._is_source_repository:
            self.skipTest("installer template does not carry active source changes")
        changes = REPOSITORY_ROOT / "openspec" / "changes"
        archive = REPOSITORY_ROOT / "openspec" / "archive"
        active_names = {
            path.name for path in changes.iterdir()
            if path.is_dir() and not path.is_symlink()
        }
        archived_names = {
            path.name for path in archive.iterdir()
            if path.is_dir() and not path.is_symlink()
        }
        self.assertFalse(
            active_names & archived_names,
            f"OpenSpec active/archive identities collide: {sorted(active_names & archived_names)}",
        )

    def test_installer_assets_use_current_semantics(self) -> None:
        if not self._is_source_repository:
            self.skipTest("installer asset tree is source-repository only")
        required_tokens = {
            self.ASSET_SEMANTIC_FILES[0]: ("三件套", "硬风险", "--archive-light"),
            self.ASSET_SEMANTIC_FILES[1]: ("三件套", "硬风险", "--archive-light"),
            self.ASSET_SEMANTIC_FILES[2]: ("三件套", "硬风险"),
            self.ASSET_SEMANTIC_FILES[3]: ("三件套", "硬风险", "--archive-light"),
            self.ASSET_SEMANTIC_FILES[4]: ("三件套", "硬风险"),
            self.ASSET_SEMANTIC_FILES[5]: ("计划风险触发", "轻量门禁", "Archive SHALL 按"),
            self.ASSET_SEMANTIC_FILES[6]: ("实际 OpenSpec 产物", "高风险实现单元审查"),
            self.ASSET_SEMANTIC_FILES[7]: ("实际 OpenSpec 产物", "高风险实现单元审查"),
            self.ASSET_SEMANTIC_FILES[8]: ("proposal、delta spec", "硬风险"),
        }
        for path in self.ASSET_SEMANTIC_FILES:
            with self.subTest(path=path):
                self.assertTrue(path.is_file(), f"missing installer asset: {path}")
                text = path.read_text(encoding="utf-8")
                for token in required_tokens[path]:
                    self.assertIn(token, text, f"{path} 必须同步语义锚点: {token}")
                for phrase in (*self.OLD_STANDARD_PHRASES, *self.OLD_ARCHIVE_PHRASES):
                    self.assertNotIn(
                        phrase, text,
                        f"{path} 保留旧流程口径: {phrase}",
                    )


class ProjectContextEvidenceContractTest(unittest.TestCase):
    """mutation: 项目上下文模板与验证证据复用边界不得漂移。"""

    @property
    def _is_source_repository(self) -> bool:
        return (
            REPOSITORY_ROOT / "scripts" / "lib" / "install_ai_workflow.py"
        ).is_file()

    def test_project_template_defines_complete_context(self) -> None:
        path = REPOSITORY_ROOT / ".ai" / "kb" / "projects" / "_template.md"
        self.assertTrue(path.is_file(), "missing generic project card template")
        text = path.read_text(encoding="utf-8")
        for token in (
            "project: example-project", "职责", "高频入口", "跨仓库关系",
            "搜索锚点", "验证入口", "verified_commit",
        ):
            self.assertIn(token, text, f"project template lacks {token}")

    def test_verification_evidence_preserves_fresh_gate(self) -> None:
        path = REPOSITORY_ROOT / ".ai" / "kb" / "verification-evidence.md"
        self.assertTrue(path.is_file(), "missing verification evidence contract")
        text = path.read_text(encoding="utf-8")
        for token in (
            "不是任务状态", "当前项目 HEAD 等于证据 commit", "环境类别",
            "结果：", "不可复用条件",
            "新鲜验证", "NOT_RUN", "UNKNOWN", "不替代",
        ):
            self.assertIn(token, text, f"verification evidence lacks {token}")
        self.assertNotIn("，或与结论相关", text)

    def test_verification_skills_route_evidence_contract(self) -> None:
        skill_paths = [
            REPOSITORY_ROOT / assistant / "skills" / "verification" / "SKILL.md"
            for assistant in (".codex", ".claude")
        ]
        skill_paths = [path for path in skill_paths if path.is_file()]
        self.assertTrue(skill_paths, "no verification skill is installed")
        for path in skill_paths:
            text = path.read_text(encoding="utf-8")
            self.assertIn("verification-evidence.md", text)
            self.assertIn("不表示当前任务通过", text)

    def test_installer_assets_include_project_context_evidence(self) -> None:
        if not self._is_source_repository:
            self.skipTest("installer asset tree is source-repository only")
        asset_root = (
            REPOSITORY_ROOT / "scripts" / "ai-workflow-assets" / "shared" / ".ai"
        )
        template = asset_root / "kb" / "projects" / "_template.md"
        evidence = asset_root / "kb" / "verification-evidence.md"
        self.assertTrue(template.is_file(), "installer lacks project template")
        self.assertTrue(evidence.is_file(), "installer lacks evidence contract")
        self.assertEqual(
            template.read_bytes(),
            (REPOSITORY_ROOT / ".ai" / "kb" / "projects" / "_template.md").read_bytes(),
        )
        self.assertEqual(
            evidence.read_bytes(),
            (REPOSITORY_ROOT / ".ai" / "kb" / "verification-evidence.md").read_bytes(),
        )


class StrictSecondConfirmHardSetTest(unittest.TestCase):
    """mutation: AGENTS.md 严格模式节必须含硬风险集合 + 连续 Build 句。"""

    AGENTS_MD = REPOSITORY_ROOT / "AGENTS.md"

    def _require_agents_md(self) -> None:
        if not self.AGENTS_MD.is_file():
            self.skipTest("codex assistant is not present in this fixture")

    def test_strict_hard_risk_set_listed(self) -> None:
        self._require_agents_md()
        text = self.AGENTS_MD.read_text(encoding="utf-8")
        for keyword in ["权限认证", "资金账务", "数据库 Schema", "数据删除", "破坏性动作", "外部副作用"]:
            self.assertIn(keyword, text, f"AGENTS.md 严格模式节必须含硬风险关键词: {keyword}")

    def test_strict_continuous_build_clause(self) -> None:
        self._require_agents_md()
        text = self.AGENTS_MD.read_text(encoding="utf-8")
        self.assertIn("不命中上述硬风险集合且未引入新选择的严格计划", text,
                      "AGENTS.md 严格模式节必须含'连续 Build'句")

    def test_old_double_confirm_clause_removed(self) -> None:
        """mutation 注入: 旧"保留两次实施前确认"句应被移除"""
        self._require_agents_md()
        text = self.AGENTS_MD.read_text(encoding="utf-8")
        self.assertNotIn("保留两次实施前确认", text,
                         "AGENTS.md 不应再保留旧双确认句")


class ReviewRuleHighRiskInvariantTest(unittest.TestCase):
    """mutation: review.md 必须含高风险不变量 + manifest 清理约束。"""

    REVIEW_MD = REPOSITORY_ROOT / ".ai" / "rules" / "review.md"

    def test_review_md_has_high_risk_invariant(self) -> None:
        text = self.REVIEW_MD.read_text(encoding="utf-8")
        self.assertIn("高风险不变量", text, "review.md 必须含高风险不变量定义")
        self.assertIn("合并为一次审查", text, "review.md 必须含合并审查条款")

    def test_review_md_has_manifest_cleanup_constraint(self) -> None:
        text = self.REVIEW_MD.read_text(encoding="utf-8")
        self.assertIn("不得递归删除整个 `.ai-local`", text,
                      "review.md 必须禁止递归删除 .ai-local")
        # mutation 注入：禁止 .ai-local reviews/<change>/ 以外的形式
        self.assertNotIn("rm -rf .ai-local", text,
                         "review.md 不应含 rm -rf .ai-local 字面（无子路径）")


class ArchiveLightPromotionMutationTest(unittest.TestCase):
    """mutation: 注入违反 diff 分类升级规则必须被检出。"""

    WRAPPER = REPOSITORY_ROOT / "scripts" / "validate-workflow.sh"

    def test_wrapper_recognizes_archive_light_flag(self) -> None:
        text = self.WRAPPER.read_text(encoding="utf-8")
        self.assertIn("--archive-light", text, "wrapper 必须识别 --archive-light")
        self.assertIn("WORKFLOW_ARCHIVE_GATE", text, "wrapper 必须支持 WORKFLOW_ARCHIVE_GATE env")

    def test_wrapper_rejects_conflict(self) -> None:
        text = self.WRAPPER.read_text(encoding="utf-8")
        self.assertTrue("conflict" in text.lower(),
                        "wrapper 必须检测 --archive-light 与 --require-openspec 冲突")


class ArchiveLightGateTest(unittest.TestCase):
    """归档轻量门禁：--archive-light 跳过顶层契约套件；diff 分类自动升级。"""

    STUB_CORE = (
        "#!/usr/bin/env bash\n"
        "printf 'INTERNAL_RESULT PASS=1 FAIL=0 SKIP=0\\n'\n"
        "exit 0\n"
    )


    def _stage_wrapper_tree(self, root: Path, core_text: str | None = None) -> None:
        """在临时目录里布置 wrapper + stub core + stub unittest + .ai-local"""
        scripts_dir = root / "scripts" / "lib"
        (root / "scripts").mkdir(parents=True)
        scripts_dir.mkdir(parents=True)
        (scripts_dir / "validate-workflow-core.sh").write_text(
            core_text
            or "#!/usr/bin/env bash\nprintf 'INTERNAL_RESULT PASS=1 FAIL=0 SKIP=0\\n'\nexit 0\n",
            encoding="utf-8",
        )
        wrapper_text = (REPOSITORY_ROOT / "scripts" / "validate-workflow.sh").read_text(
            encoding="utf-8"
        )
        (scripts_dir.parent / "validate-workflow.sh").write_text(wrapper_text, encoding="utf-8")
        (root / "scripts" / "tests").mkdir()
        (root / "scripts" / "tests" / "test_validate_workflow.py").write_text(
            'import unittest\n'
            'class SentinelTest(unittest.TestCase):\n'
            '    def test_placeholder(self) -> None: self.assertTrue(True)\n',
            encoding="utf-8",
        )
        (root / "scripts" / "tests" / "run_validate_workflow_parallel.py").write_text(
            (REPOSITORY_ROOT / "scripts" / "tests" / "run_validate_workflow_parallel.py")
            .read_text(encoding="utf-8")
            if (REPOSITORY_ROOT / "scripts" / "tests" / "run_validate_workflow_parallel.py")
            .is_file()
            else "import unittest\n",
            encoding="utf-8",
        )
        (root / ".ai-local").mkdir()

    def _run_wrapper(self, *arguments: str, core_status: int = 0,
                     stub_core: str | None = None) -> subprocess.CompletedProcess:
        with tempfile.TemporaryDirectory() as temporary:
            root = Path(temporary)
            scripts_dir = root / "scripts" / "lib"
            (root / "scripts").mkdir(parents=True)
            scripts_dir.mkdir(parents=True)
            (scripts_dir / "validate-workflow-core.sh").write_text(
                stub_core or self.STUB_CORE, encoding="utf-8"
            )
            wrapper = (REPOSITORY_ROOT / "scripts" / "validate-workflow.sh").read_text(
                encoding="utf-8"
            )
            wrapper_path = scripts_dir.parent / "validate-workflow.sh"
            wrapper_path.write_text(wrapper, encoding="utf-8")
            (root / "scripts" / "tests").mkdir()
            (root / "scripts" / "tests" / "test_validate_workflow.py").write_text(
                'import unittest\n'
                'class SentinelTest(unittest.TestCase):\n'
                '    def test_placeholder(self) -> None: self.assertTrue(True)\n',
                encoding="utf-8",
            )
            (root / "scripts" / "tests" / "run_validate_workflow_parallel.py").write_text(
                (REPOSITORY_ROOT / "scripts" / "tests" / "run_validate_workflow_parallel.py")
                .read_text(encoding="utf-8")
                if (REPOSITORY_ROOT / "scripts" / "tests" / "run_validate_workflow_parallel.py")
                .is_file()
                else "import unittest\n",
                encoding="utf-8",
            )
            (root / ".ai-local").mkdir()
            cmd = ["/usr/bin/bash", str(wrapper_path), *arguments]
            return subprocess.run(
                cmd, cwd=root, stdout=subprocess.PIPE,
                stderr=subprocess.PIPE, text=True, check=False, timeout=60,
            )

    def test_archive_light_skips_contract_suite_when_no_semantic_diff(self) -> None:
        result = self._run_wrapper("--archive-light")
        self.assertEqual(0, result.returncode, msg=result.stdout + result.stderr)
        # 顶层契约套件应被跳过：unittest 失败/不存在
        self.assertNotIn("[PASS] 工作流顶层契约测试", result.stdout)
        self.assertIn("PASS=1 FAIL=0 SKIP=0", result.stdout)

    def test_archive_light_runs_pure_core_when_no_archive_files(self) -> None:
        # --archive-light 单独使用 = 纯轻量门禁（仅 core，无 diff 分类升级）
        result = self._run_wrapper("--archive-light")
        self.assertEqual(0, result.returncode, msg=result.stdout + result.stderr)
        self.assertIn("archive-light gate engaged", result.stdout)
        self.assertIn("files=0", result.stdout)
        self.assertNotIn("[PASS] 工作流顶层契约测试", result.stdout)

    def test_archive_light_rejects_conflict_with_require_openspec(self) -> None:
        result = self._run_wrapper("--archive-light", "--require-openspec")
        self.assertEqual(2, result.returncode, msg=result.stdout + result.stderr)
        combined = result.stdout + result.stderr
        self.assertIn("conflict", combined.lower())


    def test_archive_light_promotes_to_require_openspec_on_workflow_diff(self) -> None:
        """diff 分类自动升级：WORKFLOW_ARCHIVE_GATE=1 + --archive-files + git diff 非空 → 改跑契约套件"""
        with tempfile.TemporaryDirectory() as temporary:
            root = Path(temporary)
            self._stage_wrapper_tree(root)
            fake_bin = root / "fakebin"
            fake_bin.mkdir()
            stub_path = root / "git_stub.sh"
            stub_path.write_text(
                "#!/usr/bin/env bash\n"
                "if [ \"$1\" = \"diff\" ] && [ \"$2\" = \"--name-only\" ]; then\n"
                "  echo scripts/validate-workflow.sh\n"
                "  exit 0\n"
                "fi\n"
                "exit 1\n",
                encoding="utf-8",
            )
            (fake_bin / "git").write_text(
                f'#!/usr/bin/env bash\nbash {stub_path} "$@"\n',
                encoding="utf-8",
            )
            (fake_bin / "git").chmod(0o755)
            import os
            env = os.environ.copy()
            env["PATH"] = str(fake_bin) + ":" + env.get("PATH", "")
            env["WORKFLOW_ARCHIVE_GATE"] = "1"
            env["WORKFLOW_ARCHIVE_BASE"] = "HEAD"
            result = subprocess.run(
                ["/usr/bin/bash", str(root / "scripts" / "validate-workflow.sh"),
                 "--archive-light", "--archive-files", "scripts/validate-workflow.sh"],
                cwd=root, env=env, stdout=subprocess.PIPE,
                stderr=subprocess.PIPE, text=True, check=False, timeout=60,
            )
            self.assertEqual(0, result.returncode, msg=result.stdout + result.stderr)
            combined = result.stdout + result.stderr
            self.assertIn("promoted to --require-openspec", combined)
            self.assertTrue(
                "[PASS] 工作流顶层契约测试" in result.stdout
                or "SentinelTest" in result.stdout
                or "test_placeholder" in result.stdout,
                msg=result.stdout + result.stderr,
            )

    def test_archive_light_promotion_sends_required_semantics_to_core(self) -> None:
        """F-ARCHIVE-CORE-001：promotion 不是只补契约套件，core 必须收到 required 参数。"""
        with tempfile.TemporaryDirectory() as temporary:
            root = Path(temporary)
            core_text = (
                "#!/usr/bin/env bash\n"
                "printf 'CORE_ARGS=%s\\n' \"$*\"\n"
                "printf 'INTERNAL_RESULT PASS=1 FAIL=0 SKIP=0\\n'\n"
                "exit 0\n"
            )
            self._stage_wrapper_tree(root, core_text=core_text)
            fake_bin = root / "fakebin"
            fake_bin.mkdir()
            stub_path = root / "git_stub.sh"
            stub_path.write_text(
                "#!/usr/bin/env bash\n"
                "if [ \"$1\" = \"diff\" ] && [ \"$2\" = \"--name-only\" ]; then\n"
                "  echo scripts/validate-workflow.sh\n"
                "  exit 0\n"
                "fi\n"
                "exit 1\n",
                encoding="utf-8",
            )
            (fake_bin / "git").write_text(
                f'#!/usr/bin/env bash\nbash {stub_path} "$@"\n',
                encoding="utf-8",
            )
            (fake_bin / "git").chmod(0o755)
            import os
            env = os.environ.copy()
            env["PATH"] = str(fake_bin) + ":" + env.get("PATH", "")
            env["WORKFLOW_ARCHIVE_GATE"] = "1"
            env["WORKFLOW_ARCHIVE_BASE"] = "HEAD"
            result = subprocess.run(
                ["/usr/bin/bash", str(root / "scripts" / "validate-workflow.sh"),
                 "--archive-light", "--archive-files", "scripts/validate-workflow.sh"],
                cwd=root, env=env, stdout=subprocess.PIPE,
                stderr=subprocess.PIPE, text=True, check=False, timeout=60,
            )
            self.assertEqual(0, result.returncode, msg=result.stdout + result.stderr)
            self.assertIn("CORE_ARGS=--require-openspec", result.stdout)
            self.assertIn("[PASS] 工作流顶层契约测试", result.stdout)

    def test_archive_light_diff_failure_fails_closed(self) -> None:
        """F-ARCHIVE-DIFF-001：非法 base/pathspec 不得被吞成无变化轻量路径。"""
        with tempfile.TemporaryDirectory() as temporary:
            root = Path(temporary)
            self._stage_wrapper_tree(root)
            fake_bin = root / "fakebin"
            fake_bin.mkdir()
            stub_path = root / "git_stub.sh"
            stub_path.write_text(
                "#!/usr/bin/env bash\n"
                "if [ \"$1\" = \"diff\" ] && [ \"$2\" = \"--name-only\" ]; then\n"
                "  echo 'fatal: bad revision definitely-missing-ref' >&2\n"
                "  exit 128\n"
                "fi\n"
                "exit 1\n",
                encoding="utf-8",
            )
            (fake_bin / "git").write_text(
                f'#!/usr/bin/env bash\nbash {stub_path} "$@"\n',
                encoding="utf-8",
            )
            (fake_bin / "git").chmod(0o755)
            import os
            env = os.environ.copy()
            env["PATH"] = str(fake_bin) + ":" + env.get("PATH", "")
            env["WORKFLOW_ARCHIVE_GATE"] = "1"
            env["WORKFLOW_ARCHIVE_BASE"] = "definitely-missing-ref"
            result = subprocess.run(
                ["/usr/bin/bash", str(root / "scripts" / "validate-workflow.sh"),
                 "--archive-light", "--archive-files", "scripts/validate-workflow.sh"],
                cwd=root, env=env, stdout=subprocess.PIPE,
                stderr=subprocess.PIPE, text=True, check=False, timeout=60,
            )
            self.assertEqual(2, result.returncode, msg=result.stdout + result.stderr)
            combined = result.stdout + result.stderr
            self.assertIn("Archive diff 分类失败", combined)
            self.assertIn("bad revision", combined)

    def test_archive_light_no_promotion_when_git_diff_empty(self) -> None:
        """diff 分类无变化时不升级，保持轻量"""
        with tempfile.TemporaryDirectory() as temporary:
            root = Path(temporary)
            self._stage_wrapper_tree(root)
            fake_bin = root / "fakebin"
            fake_bin.mkdir()
            stub_path = root / "git_stub.sh"
            stub_path.write_text(
                "#!/usr/bin/env bash\nexit 0\n",  # diff 输出空
                encoding="utf-8",
            )
            (fake_bin / "git").write_text(
                f'#!/usr/bin/env bash\nbash {stub_path} "$@"\n',
                encoding="utf-8",
            )
            (fake_bin / "git").chmod(0o755)
            import os
            env = os.environ.copy()
            env["PATH"] = str(fake_bin) + ":" + env.get("PATH", "")
            env["WORKFLOW_ARCHIVE_GATE"] = "1"
            env["WORKFLOW_ARCHIVE_BASE"] = "HEAD"
            result = subprocess.run(
                ["/usr/bin/bash", str(root / "scripts" / "validate-workflow.sh"),
                 "--archive-light", "--archive-files", "scripts/validate-workflow.sh"],
                cwd=root, env=env, stdout=subprocess.PIPE,
                stderr=subprocess.PIPE, text=True, check=False, timeout=60,
            )
            self.assertEqual(0, result.returncode, msg=result.stdout + result.stderr)
            self.assertNotIn("promoted to --require-openspec", result.stdout + result.stderr)
            self.assertNotIn("[PASS] 工作流顶层契约测试", result.stdout)

    def test_archive_light_propagates_core_failure_exit(self) -> None:
        bad_stub = "#!/usr/bin/env bash\nexit 1\n"
        result = self._run_wrapper("--archive-light", stub_core=bad_stub)
        self.assertNotEqual(0, result.returncode, msg=result.stdout + result.stderr)
        self.assertNotIn("[PASS] 工作流顶层契约测试", result.stdout)

    def test_archive_light_promoted_path_renders_internal_skips(self) -> None:
        """F-Q1 regression：promoted 路径必须复用 render_contract_suite_skips；
        若契约套件内部出现 ... skipped 行，必须在汇总前可见（与默认路径同语义）。"""
        with tempfile.TemporaryDirectory() as temporary:
            root = Path(temporary)
            self._stage_wrapper_tree(root)
            # 让 stub unittest 输出 ... skipped 行（触发现实仓库 PrePushHookTest 等）
            (root / "scripts" / "tests" / "test_validate_workflow.py").write_text(
                'import unittest\n'
                'class SentinelTest(unittest.TestCase):\n'
                '    def test_pass(self) -> None: self.assertTrue(True)\n'
                '    def test_skipped(self) -> None: self.skipTest("source-only stub")\n',
                encoding="utf-8",
            )
            fake_bin = root / "fakebin"
            fake_bin.mkdir()
            stub_path = root / "git_stub.sh"
            stub_path.write_text(
                "#!/usr/bin/env bash\n"
                "if [ \"$1\" = \"diff\" ] && [ \"$2\" = \"--name-only\" ]; then\n"
                "  echo scripts/validate-workflow.sh\n"
                "  exit 0\n"
                "fi\n"
                "exit 1\n",
                encoding="utf-8",
            )
            (fake_bin / "git").write_text(
                f'#!/usr/bin/env bash\nbash {stub_path} "$@"\n',
                encoding="utf-8",
            )
            (fake_bin / "git").chmod(0o755)
            import os
            env = os.environ.copy()
            env["PATH"] = str(fake_bin) + ":" + env.get("PATH", "")
            env["WORKFLOW_ARCHIVE_GATE"] = "1"
            env["WORKFLOW_ARCHIVE_BASE"] = "HEAD"
            result = subprocess.run(
                ["/usr/bin/bash", str(root / "scripts" / "validate-workflow.sh"),
                 "--archive-light", "--archive-files", "scripts/validate-workflow.sh"],
                cwd=root, env=env, stdout=subprocess.PIPE,
                stderr=subprocess.PIPE, text=True, check=False, timeout=60,
            )
            self.assertEqual(0, result.returncode, msg=result.stdout + result.stderr)
            # 关键断言：promoted 路径必须包含"内部设计性跳过"渲染段
            self.assertIn("内部设计性跳过", result.stdout,
                          msg=result.stdout + result.stderr)

    def test_archive_light_fails_closed_when_git_missing(self) -> None:
        """F-Q2 regression：WORKFLOW_ARCHIVE_GATE=1 + --archive-files 非空时，
        若 git 不可用必须 fail-closed（stderr 提示 + 非零退出码）。"""
        with tempfile.TemporaryDirectory() as temporary:
            root = Path(temporary)
            self._stage_wrapper_tree(root)
            # 构造一个没有 git 的 PATH（指向空目录）
            empty_bin = root / "emptybin"
            empty_bin.mkdir()
            import os
            env = os.environ.copy()
            env["PATH"] = str(empty_bin)  # 仅含空目录，无 git
            env["WORKFLOW_ARCHIVE_GATE"] = "1"
            env["WORKFLOW_ARCHIVE_BASE"] = "HEAD"
            result = subprocess.run(
                ["/usr/bin/bash", str(root / "scripts" / "validate-workflow.sh"),
                 "--archive-light", "--archive-files", "scripts/validate-workflow.sh"],
                cwd=root, env=env, stdout=subprocess.PIPE,
                stderr=subprocess.PIPE, text=True, check=False, timeout=60,
            )
            self.assertNotEqual(0, result.returncode,
                                msg="git 缺失时 WORKFLOW_ARCHIVE_GATE=1 必须非零退出: "
                                + result.stdout + result.stderr)
            combined = result.stdout + result.stderr
            self.assertIn("git 不可用", combined,
                          msg=combined)


if __name__ == "__main__":
    unittest.main()
