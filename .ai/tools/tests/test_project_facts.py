import json
import os
import shutil
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

    def business_entry(self, **overrides):
        value = self.entry(
            business_terms=[
                {
                    "term": "contract",
                    "synonyms": ["lease"],
                    "source_paths": ["src/App.java", "src/Other.java"],
                },
                {
                    "term": "contract draft",
                    "source_paths": ["notes.txt"],
                },
            ]
        )
        value.update(overrides)
        return value

    def verification_entry(self, verified_commit=None, **overrides):
        if verified_commit is None:
            verified_commit = self.git("rev-parse", "HEAD").stdout.strip()
        (self.ai / "verification").mkdir(exist_ok=True)
        (self.ai / "verification/alpha.md").write_text(
            "# Alpha verification evidence\n", encoding="utf-8"
        )
        (self.ai / "verification/credentials.json").write_text(
            "{}\n", encoding="utf-8"
        )
        value = self.entry(
            dependencies=["beta"],
            verification={
                "build_command": "example-build --all",
                "test_command": "example-test --all",
                "evidence": "verification/alpha.md",
                "verified_commit": verified_commit,
            },
        )
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
            env=None,
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

    def test_project_context_outputs_dependencies_and_verification(self):
        beta_card = self.ai / "kb/projects/beta.md"
        beta_card.write_text("# Beta\n", encoding="utf-8")
        beta = self.entry(
            name="beta", path="beta", card="kb/projects/beta.md",
            applications=[],
        )
        self.write_registry([self.verification_entry(), beta])

        result = self.run_cli("project-context", "--project", "alpha")

        self.assertEqual(0, result.returncode, result.stderr)
        self.assertEqual(
            [
                "PROJECT\talpha\talpha\tmaven\tkb/projects/alpha.md\tavailable",
                "APPLICATION\talpha-server\tserver\texample.App\tsrc/App.java",
                "DEPENDENCY\tbeta",
                "VERIFICATION\tbuild\texample-build --all",
                "VERIFICATION\ttest\texample-test --all",
                "VERIFICATION\tevidence\tverification/alpha.md",
                "VERIFICATION\tverified_commit\tcurrent\t"
                + self.git("rev-parse", "HEAD").stdout.strip(),
            ],
            result.stdout.splitlines(),
        )

    def test_project_context_reports_drifted_and_unavailable_verified_commit(self):
        beta_card = self.ai / "kb/projects/beta.md"
        beta_card.write_text("# Beta\n", encoding="utf-8")
        beta = self.entry(
            name="beta", path="beta", card="kb/projects/beta.md",
            applications=[],
        )
        drifted_entry = self.verification_entry(verified_commit="a" * 40)
        self.write_registry([drifted_entry, beta])
        drifted = self.run_cli("project-context", "--project", "alpha")
        self.assertEqual(0, drifted.returncode, drifted.stderr)
        self.assertIn(
            "VERIFICATION\tverified_commit\tdrifted\t" + "a" * 40,
            drifted.stdout,
        )

        missing_entry = self.verification_entry(path="not-checked-out")
        self.write_registry([missing_entry, beta])
        unavailable = self.run_cli("project-context", "--project", "alpha")
        self.assertEqual(0, unavailable.returncode, unavailable.stderr)
        self.assertIn(
            "VERIFICATION\tverified_commit\tunavailable\t"
            + self.git("rev-parse", "HEAD").stdout.strip(),
            unavailable.stdout,
        )

        declared_commit = self.git("rev-parse", "HEAD").stdout.strip()
        non_git_entry = self.verification_entry(verified_commit=declared_commit)
        self.write_registry([non_git_entry, beta])
        shutil.rmtree(self.project / ".git")
        non_git = self.run_cli("project-context", "--project", "alpha")
        self.assertEqual(0, non_git.returncode, non_git.stderr)
        self.assertIn(
            "PROJECT\talpha\talpha\tmaven\tkb/projects/alpha.md\tavailable",
            non_git.stdout,
        )
        self.assertIn(
            "VERIFICATION\tverified_commit\tunavailable\t" + declared_commit,
            non_git.stdout,
        )

    def test_registry_rejects_invalid_dependencies_and_verification(self):
        beta_card = self.ai / "kb/projects/beta.md"
        beta_card.write_text("# Beta\n", encoding="utf-8")
        (self.ai / "verification").mkdir(exist_ok=True)
        (self.ai / "verification/alpha.md").write_text(
            "# Alpha verification evidence\n", encoding="utf-8"
        )
        beta = self.entry(
            name="beta", path="beta", card="kb/projects/beta.md",
            applications=[],
        )
        valid_verification = {
            "build_command": "example-build --all",
            "test_command": "example-test --all",
            "evidence": "verification/alpha.md",
            "verified_commit": "a" * 40,
        }
        invalid_entries = [
            self.entry(dependencies=["unknown"]),
            self.entry(dependencies=["alpha"]),
            self.entry(dependencies=["beta", "beta"]),
            self.entry(verification="invalid"),
            self.entry(verification={}),
            self.entry(verification={**valid_verification, "unexpected": "value"}),
            self.entry(verification={**valid_verification, "build_command": ""}),
            self.entry(verification={**valid_verification, "test_command": "x\ny"}),
            self.entry(verification={**valid_verification, "evidence": "/tmp/evidence.md"}),
            self.entry(verification={**valid_verification, "evidence": "../outside.md"}),
            self.entry(verification={**valid_verification, "evidence": "verification/missing.md"}),
            self.entry(verification={**valid_verification, "evidence": "verification/credentials.json"}),
            self.entry(verification={**valid_verification, "verified_commit": "abc"}),
        ]
        for index, entry in enumerate(invalid_entries):
            with self.subTest(index=index, entry=entry):
                self.write_registry([entry, beta])
                result = self.run_cli("project-context", "--project", "alpha")
                self.assertEqual(2, result.returncode)
                self.assertEqual("", result.stdout)
                self.assertTrue(result.stderr.startswith("ERROR\t"), result.stderr)
                self.assertEqual(1, len(result.stderr.splitlines()), result.stderr)

    def test_registry_rejects_dependency_cycle(self):
        beta_card = self.ai / "kb/projects/beta.md"
        beta_card.write_text("# Beta\n", encoding="utf-8")
        beta = self.entry(
            name="beta", path="beta", card="kb/projects/beta.md",
            dependencies=["alpha"], applications=[],
        )
        self.write_registry([self.entry(dependencies=["beta"]), beta])

        result = self.run_cli("project-context", "--project", "alpha")

        self.assertEqual(2, result.returncode)
        self.assertEqual("", result.stdout)
        self.assertIn("dependency cycle", result.stderr.lower())

    def test_git_metadata_escape_is_rejected_for_context_and_search(self):
        external = Path(self.tempdir.name) / "external-repository"
        external.mkdir()
        self.git("init", "-q", cwd=external)
        self.git("config", "user.email", "external@example.invalid", cwd=external)
        self.git("config", "user.name", "External", cwd=external)
        self.git("commit", "-q", "--allow-empty", "-m", "external", cwd=external)
        external_head = self.git("rev-parse", "HEAD", cwd=external).stdout.strip()

        shutil.rmtree(self.project / ".git")
        (self.project / ".git").symlink_to(
            external / ".git", target_is_directory=True
        )
        (self.ai / "verification").mkdir(exist_ok=True)
        (self.ai / "verification/alpha.md").write_text(
            "# Alpha verification evidence\n", encoding="utf-8"
        )
        self.write_registry([
            self.verification_entry(
                verified_commit=external_head, dependencies=[]
            )
        ])

        context = self.run_cli("project-context", "--project", "alpha")
        search = self.run_cli(
            "workspace-search", "--project", "alpha", "--text", "needle",
            "--limit", "5", "--offset", "0",
        )

        for result in (context, search):
            self.assertEqual(2, result.returncode)
            self.assertEqual("", result.stdout)
            self.assertIn("Git metadata", result.stderr)
            self.assertIn("boundary", result.stderr.lower())

        (self.project / ".git").unlink()
        (self.project / ".git").mkdir()
        (self.project / ".git/HEAD").write_text(
            external_head + "\n", encoding="utf-8"
        )
        (self.project / ".git/commondir").write_text(
            str(external / ".git") + "\n", encoding="utf-8"
        )
        common_context = self.run_cli("project-context", "--project", "alpha")
        common_search = self.run_cli(
            "workspace-search", "--project", "alpha", "--text", "needle",
            "--limit", "5", "--offset", "0",
        )
        for result in (common_context, common_search):
            self.assertEqual(2, result.returncode)
            self.assertEqual("", result.stdout)
            self.assertIn("Git commondir", result.stderr)
            self.assertIn("boundary", result.stderr.lower())

    def test_workspace_search_disables_git_config_fsmonitor_execution(self):
        config_directory = self.workspace / "config-external"
        config_directory.mkdir()
        marker = self.workspace / "fsmonitor-marker"
        script = config_directory / "fsmonitor.sh"
        script.write_text(
            f'#!/bin/sh\nprintf executed > {marker}\n',
            encoding="utf-8",
        )
        script.chmod(0o755)
        (config_directory / "config").write_text(
            "[core]\n"
            f"\tfsmonitor = {script}\n",
            encoding="utf-8",
        )
        with (self.project / ".git/config").open("a", encoding="utf-8") as config:
            config.write(f"\n[include]\n\tpath = {config_directory / 'config'}\n")

        result = self.run_cli(
            "workspace-search", "--project", "alpha", "--text", "needle",
            "--limit", "5", "--offset", "0",
        )

        self.assertEqual(0, result.returncode, result.stderr)
        self.assertFalse(marker.exists())

    def test_git_metadata_control_symlinks_are_rejected_before_read(self):
        (self.ai / "verification").mkdir(exist_ok=True)
        (self.ai / "verification/alpha.md").write_text(
            "# Alpha verification evidence\n", encoding="utf-8"
        )
        self.write_registry([self.verification_entry(dependencies=[])])

        external_control = Path(self.tempdir.name) / "invalid-control"
        external_control.write_bytes(b"\xff\xfe\x00invalid")
        (self.project / ".git/commondir").symlink_to(external_control)
        common = self.run_cli("project-context", "--project", "alpha")

        self.assertEqual(2, common.returncode)
        self.assertEqual("", common.stdout)
        self.assertTrue(common.stderr.startswith("ERROR\t"), common.stderr)
        self.assertIn("Git metadata symlink", common.stderr)

        (self.project / ".git/commondir").unlink()
        info = self.project / ".git/objects/info"
        info.mkdir(parents=True, exist_ok=True)
        (info / "alternates").symlink_to(external_control)
        alternate = self.run_cli("project-context", "--project", "alpha")

        self.assertEqual(2, alternate.returncode)
        self.assertEqual("", alternate.stdout)
        self.assertTrue(alternate.stderr.startswith("ERROR\t"), alternate.stderr)
        self.assertIn("Git metadata symlink", alternate.stderr)

    def test_missing_git_binary_produces_stable_results(self):
        (self.ai / "verification").mkdir(exist_ok=True)
        (self.ai / "verification/alpha.md").write_text(
            "# Alpha verification evidence\n", encoding="utf-8"
        )
        self.write_registry([self.verification_entry(dependencies=[])])
        environment = os.environ.copy()
        environment["PATH"] = ""

        context = subprocess.run(
            [
                sys.executable, str(SCRIPT), "project-context",
                "--workspace", str(self.workspace), "--project", "alpha",
            ],
            text=True, stdout=subprocess.PIPE, stderr=subprocess.PIPE,
            env=environment,
        )
        search = subprocess.run(
            [
                sys.executable, str(SCRIPT), "workspace-search",
                "--workspace", str(self.workspace), "--project", "alpha",
                "--text", "needle", "--limit", "5", "--offset", "0",
            ],
            text=True, stdout=subprocess.PIPE, stderr=subprocess.PIPE,
            env=environment,
        )

        self.assertEqual(0, context.returncode, context.stderr)
        self.assertNotIn("Traceback", context.stderr)
        self.assertIn(
            "VERIFICATION\tverified_commit\tunavailable",
            context.stdout,
        )
        self.assertEqual(2, search.returncode)
        self.assertEqual("", search.stdout)
        self.assertTrue(search.stderr.startswith("ERROR\t"), search.stderr)

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

    def test_business_terms_route_synonyms_exact_filter_and_pagination(self):
        beta_card = self.ai / "kb/projects/beta.md"
        beta_card.write_text("# Beta\n", encoding="utf-8")
        beta = self.business_entry(
            name="beta",
            path="beta",
            card="kb/projects/beta.md",
            business_terms=[{
                "term": "contract",
                "source_paths": ["src/Contract.java"],
            }],
        )
        self.write_registry([self.business_entry(), beta])

        contained = self.run_cli(
            "business-terms", "--project", "alpha", "--text", "lease",
            "--limit", "20", "--offset", "0",
        )
        self.assertEqual(0, contained.returncode, contained.stderr)
        self.assertEqual(
            [
                "BUSINESS_TERM\tcontract\talpha\tkb/projects/alpha.md\tsrc/App.java",
                "BUSINESS_TERM\tcontract\talpha\tkb/projects/alpha.md\tsrc/Other.java",
            ],
            contained.stdout.splitlines(),
        )

        exact = self.run_cli(
            "business-terms", "--project", "alpha", "--text", "contract draft",
            "--exact", "--limit", "20", "--offset", "0",
        )
        self.assertEqual(0, exact.returncode, exact.stderr)
        self.assertEqual(
            ["BUSINESS_TERM\tcontract draft\talpha\tkb/projects/alpha.md\tnotes.txt"],
            exact.stdout.splitlines(),
        )

        paged = self.run_cli(
            "business-terms", "--text", "contract", "--limit", "2", "--offset", "1",
        )
        self.assertEqual(0, paged.returncode, paged.stderr)
        self.assertEqual(
            [
                "BUSINESS_TERM\tcontract\talpha\tkb/projects/alpha.md\tsrc/Other.java",
                "BUSINESS_TERM\tcontract draft\talpha\tkb/projects/alpha.md\tnotes.txt",
            ],
            paged.stdout.splitlines(),
        )
        self.assertEqual("TRUNCATED\tnext_offset=3\ttotal=4\n", paged.stderr)

    def test_business_terms_reports_zero_match_and_rejects_invalid_input(self):
        zero = self.run_cli(
            "business-terms", "--text", "missing", "--limit", "5", "--offset", "0"
        )
        self.assertEqual(3, zero.returncode)
        self.assertEqual("", zero.stdout)
        self.assertEqual("no business term matches\n", zero.stderr)

        cases = [
            (("--text", ""), "non-empty single-line"),
            (("--text", "contract\nINJECT"), "non-empty single-line"),
            (("--text", "contract", "--limit", "0"), "limit must"),
            (("--text", "contract", "--limit", "5", "--offset", "-1"), "offset must"),
            (("--text", "contract", "--project", "unknown"), "unregistered"),
        ]
        for args, reason in cases:
            with self.subTest(args=args):
                result = self.run_cli("business-terms", *args)
                self.assertEqual(2, result.returncode)
                self.assertEqual("", result.stdout)
                self.assertTrue(result.stderr.startswith("ERROR\t"), result.stderr)
                self.assertEqual(1, len(result.stderr.splitlines()), result.stderr)
                self.assertIn(reason, result.stderr.lower())

    def test_business_terms_registry_rejects_invalid_declarations(self):
        invalid_entries = [
            self.business_entry(business_terms="contract"),
            self.business_entry(business_terms=[{"synonyms": [], "source_paths": ["src"]}]),
            self.business_entry(business_terms=[{"term": "contract", "source_paths": []}]),
            self.business_entry(business_terms=[{
                "term": "contract", "source_paths": [str(self.project.resolve())]
            }]),
            self.business_entry(business_terms=[{
                "term": "contract", "source_paths": ["../outside"]
            }]),
            self.business_entry(business_terms=[{
                "term": "contract", "synonyms": ["lease\nINJECT"], "source_paths": ["src"]
            }]),
        ]
        for entry in invalid_entries:
            with self.subTest(entry=entry):
                self.write_registry([entry])
                result = self.run_cli("business-terms", "--text", "contract")
                self.assertEqual(2, result.returncode)
                self.assertEqual("", result.stdout)
                self.assertTrue(result.stderr.startswith("ERROR\t"), result.stderr)
                self.assertEqual(1, len(result.stderr.splitlines()), result.stderr)

    def test_business_terms_rejects_source_path_symlink_escape(self):
        outside = Path(self.tempdir.name) / "business-outside"
        outside.mkdir()
        escape = self.project / "escape"
        escape.symlink_to(outside, target_is_directory=True)
        self.write_registry([self.business_entry(business_terms=[{
            "term": "contract", "source_paths": ["escape"]
        }])])

        result = self.run_cli("business-terms", "--text", "contract")

        self.assertEqual(2, result.returncode)
        self.assertEqual("", result.stdout)
        self.assertIn("boundary", result.stderr.lower())

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
        beta_card = self.ai / "kb/projects/beta.md"
        beta_card.write_text("# Beta\n", encoding="utf-8")
        beta = self.entry(
            name="beta", path="beta", card="kb/projects/beta.md",
            applications=[],
        )
        (self.ai / "verification").mkdir(exist_ok=True)
        (self.ai / "verification/alpha.md").write_text(
            "# Alpha verification evidence\n", encoding="utf-8"
        )
        self.write_registry([
            self.business_entry(
                dependencies=["beta"],
                verification={
                    "build_command": "example-build --all",
                    "test_command": "example-test --all",
                    "evidence": "verification/alpha.md",
                    "verified_commit": self.git("rev-parse", "HEAD").stdout.strip(),
                },
            ),
            beta,
        ])
        before = self.snapshot()
        time.sleep(0.01)
        commands = [
            ("project-context", "--project", "alpha"),
            ("server-registry", "--server", "alpha-server"),
            ("workspace-search", "--project", "alpha", "--text", "needle",
             "--limit", "5", "--offset", "0"),
            ("business-terms", "--text", "lease", "--limit", "5", "--offset", "0"),
        ]
        for command, *args in commands:
            result = self.run_cli(command, *args)
            self.assertEqual(0, result.returncode, result.stderr)
        self.assertEqual(before, self.snapshot())


if __name__ == "__main__":
    unittest.main()
