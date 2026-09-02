import json
import os
import subprocess
import sys
import tempfile
import time
import unittest
from pathlib import Path


SCRIPT = Path(__file__).resolve().parents[1] / "project_facts.py"


class ProjectFactsTest(unittest.TestCase):
    def setUp(self):
        self.tempdir = tempfile.TemporaryDirectory()
        self.workspace = Path(self.tempdir.name) / "workspace"
        self.workspace.mkdir()
        self.ai = self.workspace / ".ai"
        (self.ai / "kb/projects").mkdir(parents=True)
        self.project = self.workspace / "alpha"
        self.project.mkdir()
        self.git("init", "-q")
        self.git("config", "user.email", "tests@example.invalid")
        self.git("config", "user.name", "Tests")
        (self.project / "src").mkdir()
        (self.project / "src/App.java").write_text("class App { String needle; }\n", encoding="utf-8")
        (self.project / "src/Other.java").write_text("// needle\n// needle\n", encoding="utf-8")
        (self.project / ".gitignore").write_text("ignored/\n.env\n", encoding="utf-8")
        self.git("add", ".gitignore", "src/App.java", "src/Other.java")
        self.git("commit", "-qm", "fixture")
        (self.project / "notes.txt").write_text("needle in untracked\n", encoding="utf-8")
        (self.project / "ignored").mkdir()
        (self.project / "ignored/secret.txt").write_text("needle secret\n", encoding="utf-8")
        (self.project / ".env").write_text("PASSWORD=needle\n", encoding="utf-8")
        self.card = self.ai / "kb/projects/alpha.md"
        self.card.write_text("# Alpha\n", encoding="utf-8")
        self.write_registry([self.entry()])

    def tearDown(self):
        self.tempdir.cleanup()

    def git(self, *args, cwd=None):
        return subprocess.run(
            ["git", *args], cwd=cwd or self.project, text=True,
            stdout=subprocess.PIPE, stderr=subprocess.PIPE, check=True,
        )

    def entry(self, **overrides):
        value = {
            "name": "alpha",
            "path": "alpha",
            "build": "maven",
            "card": "kb/projects/alpha.md",
            "search_roots": ["src", "notes.txt"],
            "applications": [{
                "server": "alpha-server",
                "module": "server",
                "main_class": "example.App",
                "source_path": "src/App.java",
            }],
        }
        value.update(overrides)
        return value

    def write_registry(self, projects, schema_version=1):
        (self.ai / "kb/projects/registry.json").write_text(
            json.dumps({"schema_version": schema_version, "projects": projects}),
            encoding="utf-8",
        )

    def run_cli(self, command, *args):
        return subprocess.run(
            [sys.executable, str(SCRIPT), command, "--workspace", str(self.workspace), *args],
            text=True, stdout=subprocess.PIPE, stderr=subprocess.PIPE,
        )

    def snapshot(self):
        result = {}
        for path in sorted(self.workspace.rglob("*")):
            if path.is_file() and ".git" not in path.parts:
                result[path.relative_to(self.workspace).as_posix()] = (
                    path.read_bytes(), path.stat().st_mtime_ns
                )
        return result

    def test_registry_rejects_duplicate_names(self):
        self.write_registry([self.entry(), self.entry(path="other")])
        result = self.run_cli("project-context", "--project", "alpha")
        self.assertEqual(2, result.returncode)
        self.assertIn("duplicate", result.stderr.lower())

    def test_registry_rejects_absolute_parent_and_missing_card_paths(self):
        invalid_entries = [
            (self.entry(path=str(self.project.resolve())), "relative path"),
            (self.entry(path="../alpha"), "relative path"),
            (self.entry(card="kb/projects/missing.md"), "missing card"),
        ]
        for entry, reason in invalid_entries:
            with self.subTest(entry=entry):
                self.write_registry([entry])
                result = self.run_cli("project-context", "--project", "alpha")
                self.assertEqual(2, result.returncode)
                self.assertEqual("", result.stdout)
                self.assertTrue(result.stderr.startswith("ERROR\t"), result.stderr)
                self.assertEqual(1, len(result.stderr.splitlines()))
                self.assertIn(reason, result.stderr.lower())

    def test_registry_rejects_ai_directory_and_registry_symlink_escape(self):
        outside_ai = Path(self.tempdir.name) / "outside-ai"
        self.ai.rename(outside_ai)
        self.ai.symlink_to(outside_ai, target_is_directory=True)
        escaped_ai = self.run_cli("project-context", "--project", "alpha")
        self.assertEqual(2, escaped_ai.returncode)
        self.assertEqual("", escaped_ai.stdout)
        self.assertIn("boundary", escaped_ai.stderr.lower())

        self.ai.unlink()
        outside_ai.rename(self.ai)
        registry = self.ai / "kb/projects/registry.json"
        outside_registry = Path(self.tempdir.name) / "registry.json"
        registry.rename(outside_registry)
        registry.symlink_to(outside_registry)
        escaped_registry = self.run_cli("project-context", "--project", "alpha")
        self.assertEqual(2, escaped_registry.returncode)
        self.assertEqual("", escaped_registry.stdout)
        self.assertIn("boundary", escaped_registry.stderr.lower())

    def test_registry_schema_version_requires_exact_integer_one(self):
        invalid_versions = [True, 1.0, 0, 2, None]
        registry = self.ai / "kb/projects/registry.json"
        for version in invalid_versions:
            with self.subTest(version=version):
                if version is None:
                    payload = {"projects": [self.entry()]}
                    registry.write_text(json.dumps(payload), encoding="utf-8")
                else:
                    self.write_registry([self.entry()], schema_version=version)
                result = self.run_cli("project-context", "--project", "alpha")
                self.assertEqual(2, result.returncode)
                self.assertEqual("", result.stdout)
                self.assertTrue(result.stderr.startswith("ERROR\t"), result.stderr)
                self.assertIn("schema_version", result.stderr)

    def test_registry_rejects_symlink_escape(self):
        outside = Path(self.tempdir.name) / "outside"
        outside.mkdir()
        (self.workspace / "escape").symlink_to(outside, target_is_directory=True)
        self.write_registry([self.entry(path="escape")])
        result = self.run_cli("project-context", "--project", "alpha")
        self.assertEqual(2, result.returncode)
        self.assertIn("boundary", result.stderr.lower())
        server = self.run_cli(
            "server-registry", "--server", "missing", "--project", "alpha"
        )
        self.assertEqual(2, server.returncode)
        self.assertIn("boundary", server.stderr.lower())

    def test_unselected_symlink_escape_does_not_block_filtered_query(self):
        outside = Path(self.tempdir.name) / "outside"
        outside.mkdir()
        (self.workspace / "escape").symlink_to(outside, target_is_directory=True)
        beta_card = self.ai / "kb/projects/beta.md"
        beta_card.write_text("# Beta\n", encoding="utf-8")
        escaped = self.entry(
            name="beta", path="escape", card="kb/projects/beta.md",
            applications=[],
        )
        self.write_registry([self.entry(), escaped])

        result = self.run_cli("project-context", "--project", "alpha")

        self.assertEqual(0, result.returncode, result.stderr)
        self.assertIn("PROJECT\talpha\t", result.stdout)

    def test_project_context_reports_checked_out_project(self):
        result = self.run_cli("project-context", "--project", "alpha")
        self.assertEqual(0, result.returncode, result.stderr)
        self.assertEqual(
            "PROJECT\talpha\talpha\tmaven\tkb/projects/alpha.md\tavailable\n"
            "APPLICATION\talpha-server\tserver\texample.App\tsrc/App.java\n",
            result.stdout,
        )

    def test_project_context_reports_missing_project_without_network_access(self):
        self.write_registry([self.entry(path="not-checked-out")])
        result = self.run_cli("project-context", "--project", "alpha")
        self.assertEqual(0, result.returncode, result.stderr)
        self.assertEqual(
            "PROJECT\talpha\tnot-checked-out\tmaven\tkb/projects/alpha.md\tmissing\n"
            "APPLICATION\talpha-server\tserver\texample.App\tsrc/App.java\n",
            result.stdout,
        )

    def test_unregistered_project_is_rejected(self):
        result = self.run_cli("project-context", "--project", "unknown")
        self.assertEqual(2, result.returncode)
        self.assertIn("unregistered", result.stderr.lower())

    def test_server_registry_unique_zero_ambiguous_and_project_filter(self):
        beta_card = self.ai / "kb/projects/beta.md"
        beta_card.write_text("# Beta\n", encoding="utf-8")
        beta = self.entry(
            name="beta", path="beta", card="kb/projects/beta.md",
            applications=[{
                "server": "alpha-server", "module": "beta-server",
                "main_class": "example.Beta", "source_path": "src/Beta.java",
            }],
        )
        self.write_registry([self.entry(), beta])

        filtered = self.run_cli(
            "server-registry", "--server", "alpha-server", "--project", "alpha"
        )
        self.assertEqual(0, filtered.returncode, filtered.stderr)
        self.assertEqual(
            "SERVER\talpha-server\talpha\tserver\texample.App\tsrc/App.java\n",
            filtered.stdout,
        )
        zero = self.run_cli("server-registry", "--server", "missing")
        self.assertEqual(3, zero.returncode)
        ambiguous = self.run_cli("server-registry", "--server", "alpha-server")
        self.assertEqual(4, ambiguous.returncode)

    def test_workspace_search_includes_tracked_and_unignored_untracked_only(self):
        result = self.run_cli(
            "workspace-search", "--project", "alpha", "--text", "needle",
            "--limit", "20", "--offset", "0",
        )
        self.assertEqual(0, result.returncode, result.stderr)
        self.assertEqual(
            [
                "alpha/notes.txt:1",
                "alpha/src/App.java:1",
                "alpha/src/Other.java:1",
                "alpha/src/Other.java:2",
            ],
            result.stdout.splitlines(),
        )
        self.assertNotIn("needle", result.stdout)
        self.assertNotIn("secret", result.stdout)
        self.assertNotIn("PASSWORD", result.stdout)

    def test_workspace_search_excludes_sensitive_files_even_when_tracked(self):
        sensitive = {
            "src/private.pem": "needle private key\n",
            "src/client.key": "needle key\n",
            "src/credentials.json": "needle credential\n",
            "src/id_rsa": "needle rsa\n",
            "src/id_dsa": "needle dsa\n",
            "src/id_ecdsa": "needle ecdsa\n",
            "src/id_ed25519": "needle ed25519\n",
        }
        for name, content in sensitive.items():
            path = self.project / name
            path.write_text(content, encoding="utf-8")
            self.git("add", name)
        self.git("commit", "-qm", "sensitive fixtures")
        result = self.run_cli(
            "workspace-search", "--project", "alpha", "--text", "needle",
            "--limit", "20", "--offset", "0",
        )
        self.assertEqual(0, result.returncode, result.stderr)
        for name in sensitive:
            self.assertNotIn(name, result.stdout)

    def test_workspace_search_skips_tracked_symlinks_without_reading_targets(self):
        private = self.project / "private"
        private.mkdir()
        sensitive_target = private / "credentials.json"
        sensitive_target.write_text("verify-leak-token\n", encoding="utf-8")
        ignored_target = self.project / "ignored/secret.txt"
        ignored_target.write_text("verify-leak-token\n", encoding="utf-8")
        outside_target = Path(self.tempdir.name) / "outside-secret.txt"
        outside_target.write_text("verify-leak-token\n", encoding="utf-8")
        alias = self.project / "src/alias.txt"
        targets = [
            Path("../private/credentials.json"),
            Path("../ignored/secret.txt"),
            outside_target,
        ]
        self.write_registry([self.entry(search_roots=["src"])])

        for target in targets:
            with self.subTest(target=str(target)):
                alias.symlink_to(target)
                self.git("add", "src/alias.txt")
                try:
                    result = self.run_cli(
                        "workspace-search", "--project", "alpha",
                        "--text", "verify-leak-token", "--limit", "20",
                        "--offset", "0",
                    )
                    self.assertEqual(3, result.returncode, result.stderr)
                    self.assertEqual("", result.stdout)
                    self.assertEqual("no search matches\n", result.stderr)
                finally:
                    self.git("reset", "-q", "HEAD", "--", "src/alias.txt")
                    if alias.is_symlink():
                        alias.unlink()

    def test_workspace_search_paginates_and_reports_truncation_without_content(self):
        result = self.run_cli(
            "workspace-search", "--project", "alpha", "--text", "needle",
            "--limit", "2", "--offset", "1",
        )
        self.assertEqual(0, result.returncode, result.stderr)
        self.assertEqual(
            ["alpha/src/App.java:1", "alpha/src/Other.java:1"],
            result.stdout.splitlines(),
        )
        self.assertEqual("TRUNCATED\tnext_offset=3\ttotal=4\n", result.stderr)
        self.assertNotIn("needle", result.stderr)

    def test_workspace_search_treats_registry_roots_as_literal_git_pathspecs(self):
        self.write_registry([self.entry(search_roots=[":(top)**"])])
        result = self.run_cli(
            "workspace-search", "--project", "alpha", "--text", "needle",
            "--limit", "20", "--offset", "0",
        )
        self.assertEqual(3, result.returncode)
        self.assertEqual("", result.stdout)
        self.assertEqual("no search matches\n", result.stderr)

    def test_cli_rejects_empty_or_multiline_server_project_and_search_text(self):
        cases = [
            ("project-context", ("--project", ""), "non-empty single-line"),
            ("project-context", ("--project", "alpha\nINJECT"), "non-empty single-line"),
            ("server-registry", ("--server", "", "--project", "alpha"), "non-empty single-line"),
            ("server-registry", ("--server", "alpha\nINJECT", "--project", "alpha"), "non-empty single-line"),
            ("server-registry", ("--server", "alpha-server", "--project", "alpha\tINJECT"), "non-empty single-line"),
            ("workspace-search", ("--text", "needle\nINJECT", "--project", "alpha"), "non-empty single-line"),
            ("workspace-search", ("--text", "needle", "--project", "alpha\nINJECT"), "non-empty single-line"),
        ]
        for command, args, reason in cases:
            with self.subTest(command=command, args=args):
                result = self.run_cli(command, *args)
                self.assertEqual(2, result.returncode)
                self.assertEqual("", result.stdout)
                self.assertTrue(result.stderr.startswith("ERROR\t"), result.stderr)
                self.assertEqual(1, len(result.stderr.splitlines()), result.stderr)
                self.assertIn(reason, result.stderr.lower())

    def test_workspace_search_rejects_short_query_invalid_paging_and_unregistered_project(self):
        cases = [
            (("--project", "alpha", "--text", "ab", "--limit", "5", "--offset", "0"), "at least 3"),
            (("--project", "alpha", "--text", "needle", "--limit", "0", "--offset", "0"), "limit must"),
            (("--project", "alpha", "--text", "needle", "--limit", "5", "--offset", "-1"), "offset must"),
            (("--project", "unknown", "--text", "needle", "--limit", "5", "--offset", "0"), "unregistered"),
        ]
        for args, reason in cases:
            with self.subTest(args=args):
                result = self.run_cli("workspace-search", *args)
                self.assertEqual(2, result.returncode)
                self.assertEqual("", result.stdout)
                self.assertTrue(result.stderr.startswith("ERROR\t"), result.stderr)
                self.assertEqual(1, len(result.stderr.splitlines()), result.stderr)
                self.assertIn(reason, result.stderr.lower())

    def test_all_queries_leave_workspace_content_and_mtime_unchanged(self):
        before = self.snapshot()
        time.sleep(0.01)
        commands = [
            ("project-context", "--project", "alpha"),
            ("server-registry", "--server", "alpha-server"),
            ("workspace-search", "--project", "alpha", "--text", "needle",
             "--limit", "5", "--offset", "0"),
        ]
        for command, *args in commands:
            result = self.run_cli(command, *args)
            self.assertEqual(0, result.returncode, result.stderr)
        self.assertEqual(before, self.snapshot())


if __name__ == "__main__":
    unittest.main()
