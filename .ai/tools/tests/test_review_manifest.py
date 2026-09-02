import hashlib
import importlib.util
import json
import os
import subprocess
import sys
import tempfile
import time
import unittest
from unittest import mock
from pathlib import Path


SCRIPT = Path(__file__).resolve().parents[1] / "review_manifest.py"


class ReviewManifestTest(unittest.TestCase):
    def setUp(self):
        self.tempdir = tempfile.TemporaryDirectory()
        self.workspace = Path(self.tempdir.name) / "workspace"
        self.workspace.mkdir()
        self.repo = self.workspace / "alpha"
        self.repo.mkdir()
        self.git("init", "-q")
        self.git("config", "user.email", "tests@example.invalid")
        self.git("config", "user.name", "Tests")
        (self.repo / ".gitignore").write_text(".ai-local/\nignored/\n", encoding="utf-8")
        (self.repo / "tracked.txt").write_text("base\n", encoding="utf-8")
        (self.repo / "rename-me.txt").write_text("rename\n", encoding="utf-8")
        self.git("add", ".gitignore", "tracked.txt", "rename-me.txt")
        self.git("commit", "-qm", "base")
        self.base = self.git("rev-parse", "HEAD").stdout.strip()

    def tearDown(self):
        self.tempdir.cleanup()

    def git(self, *args, cwd=None, check=True):
        return subprocess.run(
            ["git", *args], cwd=cwd or self.repo, text=True,
            stdout=subprocess.PIPE, stderr=subprocess.PIPE, check=check,
        )

    def manifest_path(self, name="manifest.json"):
        return self.workspace / ".ai-local/reviews/change" / name

    def run_cli(self, command, *args):
        return subprocess.run(
            [sys.executable, str(SCRIPT), command, *args], text=True,
            stdout=subprocess.PIPE, stderr=subprocess.PIPE,
        )

    def freeze(self, output=None, repo_specs=None, change="change"):
        output = output or self.manifest_path()
        specs = repo_specs or [(self.repo, self.base)]
        args = [
            "--change", change, "--workspace", str(self.workspace),
            "--output", str(output),
        ]
        for repo, base in specs:
            args.extend(["--repo-spec", f"{repo}::{base}"])
        return self.run_cli("freeze", *args)

    def load(self, path=None):
        return json.loads((path or self.manifest_path()).read_text(encoding="utf-8"))

    def git_index_path(self, repo):
        git_dir = self.git(
            "rev-parse", "--absolute-git-dir", cwd=repo
        ).stdout.strip()
        return Path(git_dir) / "index"

    def snapshot_path(self, path):
        return path.read_bytes(), path.stat().st_mtime_ns

    def touch_for_git_refresh(self, path):
        metadata = path.stat()
        os.utime(
            path,
            ns=(metadata.st_atime_ns, metadata.st_mtime_ns + 10_000_000_000),
        )

    def test_freeze_clean_schema_and_canonical_id(self):
        result = self.freeze()
        self.assertEqual(0, result.returncode, result.stderr)
        manifest = self.load()
        self.assertEqual(1, manifest["schema_version"])
        self.assertEqual("change", manifest["change"])
        self.assertEqual(str(self.workspace.resolve()), manifest["workspace"])
        self.assertRegex(manifest["id"], r"^[0-9a-f]{64}$")
        repo = manifest["repositories"][0]
        self.assertEqual(str(self.repo.resolve()), repo["path"])
        self.assertEqual(self.base, repo["base"]["input"])
        self.assertEqual(self.base, repo["base"]["resolved"])
        self.assertEqual(self.base, repo["merge_base"])
        self.assertEqual(self.base, repo["head"])
        for key in ("committed", "staged", "unstaged", "untracked"):
            self.assertEqual([], repo[key])
        self.assertEqual({}, repo["files"])
        self.assertEqual(f"FROZEN {manifest['id']} {self.manifest_path()}\n", result.stdout)

    def test_freeze_records_committed_staged_unstaged_untracked_delete_rename_and_symlink(self):
        (self.repo / "committed.txt").write_text("committed\n", encoding="utf-8")
        self.git("add", "committed.txt")
        self.git("commit", "-qm", "committed")
        (self.repo / "staged.txt").write_text("staged\n", encoding="utf-8")
        self.git("add", "staged.txt")
        (self.repo / "tracked.txt").write_text("unstaged\n", encoding="utf-8")
        (self.repo / "untracked.txt").write_text("untracked\n", encoding="utf-8")
        self.git("rm", "rename-me.txt")
        (self.repo / "renamed.txt").write_text("rename\n", encoding="utf-8")
        self.git("add", "renamed.txt")
        os.symlink("tracked.txt", self.repo / "link")

        result = self.freeze()

        self.assertEqual(0, result.returncode, result.stderr)
        repo = self.load()["repositories"][0]
        self.assertIn({"status": "A", "paths": ["committed.txt"]}, repo["committed"])
        self.assertIn({"status": "R100", "paths": ["rename-me.txt", "renamed.txt"]}, repo["staged"])
        self.assertIn({"status": "A", "paths": ["staged.txt"]}, repo["staged"])
        self.assertIn({"status": "M", "paths": ["tracked.txt"]}, repo["unstaged"])
        self.assertEqual(["link", "untracked.txt"], repo["untracked"])
        self.assertEqual("missing", repo["files"]["rename-me.txt"]["kind"])
        self.assertEqual("symlink", repo["files"]["link"]["kind"])
        self.assertRegex(repo["files"]["link"]["mode"], r"^[0-7]{4}$")
        for layer in ("base", "head", "index"):
            self.assertIsNone(repo["files"]["link"][layer]["mode"])
        for path in ("committed.txt", "renamed.txt", "staged.txt", "tracked.txt", "untracked.txt", "link"):
            self.assertRegex(repo["files"][path]["sha256"], r"^[0-9a-f]{64}$")

    def test_freeze_excludes_ignored_files_and_manifest_itself(self):
        (self.repo / "ignored").mkdir()
        (self.repo / "ignored/secret.txt").write_text("secret\n", encoding="utf-8")
        (self.repo / ".ai-local/reviews/change").mkdir(parents=True)
        (self.repo / ".ai-local/reviews/change/prior.json").write_text("{}\n", encoding="utf-8")
        output = self.manifest_path()
        first = self.freeze(output=output)
        self.assertEqual(0, first.returncode, first.stderr)
        second = self.freeze(output=output)
        self.assertEqual(0, second.returncode, second.stderr)
        repo = json.loads(output.read_text(encoding="utf-8"))["repositories"][0]
        rendered = json.dumps(repo, sort_keys=True)
        self.assertNotIn("ignored/secret.txt", rendered)
        self.assertNotIn("prior.json", rendered)

    def test_freeze_supports_multiple_repositories_in_stable_order(self):
        beta = self.workspace / "beta"
        beta.mkdir()
        self.git("init", "-q", cwd=beta)
        self.git("config", "user.email", "tests@example.invalid", cwd=beta)
        self.git("config", "user.name", "Tests", cwd=beta)
        (beta / "file.txt").write_text("beta\n", encoding="utf-8")
        self.git("add", "file.txt", cwd=beta)
        self.git("commit", "-qm", "base", cwd=beta)
        beta_base = self.git("rev-parse", "HEAD", cwd=beta).stdout.strip()

        result = self.freeze(repo_specs=[(beta, beta_base), (self.repo, self.base)])

        self.assertEqual(0, result.returncode, result.stderr)
        paths = [item["path"] for item in self.load()["repositories"]]
        self.assertEqual(sorted(paths), paths)

    def test_freeze_rejects_invalid_repo_base_output_and_change(self):
        outside = Path(self.tempdir.name) / "outside.json"
        cases = [
            (["--change", "change", "--workspace", str(self.workspace), "--repo-spec", f"{self.workspace / 'missing'}::{self.base}", "--output", str(self.manifest_path())], "git"),
            (["--change", "change", "--workspace", str(self.workspace), "--repo-spec", f"{self.repo}::missing-base", "--output", str(self.manifest_path())], "base"),
            (["--change", "change", "--workspace", str(self.workspace), "--repo-spec", f"{self.repo}::{self.base}", "--output", str(outside)], ".ai-local/reviews"),
            (["--change", "../escape", "--workspace", str(self.workspace), "--repo-spec", f"{self.repo}::{self.base}", "--output", str(self.manifest_path())], "change"),
        ]
        for args, reason in cases:
            with self.subTest(reason=reason):
                result = self.run_cli("freeze", *args)
                self.assertEqual(2, result.returncode)
                self.assertEqual("", result.stdout)
                self.assertIn(reason, result.stderr.lower())

    def test_verify_rejects_non_integer_schema_version(self):
        self.assertEqual(0, self.freeze().returncode)
        path = self.manifest_path()
        manifest = self.load()
        manifest["schema_version"] = True
        payload = {key: value for key, value in manifest.items() if key != "id"}
        canonical = json.dumps(payload, ensure_ascii=False, sort_keys=True, separators=(",", ":")).encode("utf-8")
        manifest["id"] = hashlib.sha256(canonical).hexdigest()
        path.write_text(json.dumps(manifest), encoding="utf-8")

        result = self.run_cli("verify", "--manifest", str(path))

        self.assertEqual(2, result.returncode)
        self.assertIn("schema_version", result.stderr)

    def test_verify_valid_is_read_only_for_bytes_and_mtime(self):
        self.assertEqual(0, self.freeze().returncode)
        path = self.manifest_path()
        before = (path.read_bytes(), path.stat().st_mtime_ns)
        time.sleep(0.01)

        result = self.run_cli("verify", "--manifest", str(path))

        self.assertEqual(0, result.returncode, result.stderr)
        self.assertEqual(f"VALID {self.load()['id']}\n", result.stdout)
        self.assertEqual(before, (path.read_bytes(), path.stat().st_mtime_ns))

    def test_verify_reports_stale_for_head_base_path_set_and_content_changes(self):
        mutations = {
            "head": lambda: self._commit_new("head.txt", "head\n"),
            "base": self._move_base_reference,
            "untracked-set": lambda: (self.repo / "new.txt").write_text("new\n", encoding="utf-8"),
            "content": lambda: (self.repo / "tracked.txt").write_text("changed\n", encoding="utf-8"),
        }
        for name, mutate in mutations.items():
            with self.subTest(name=name):
                path = self.manifest_path(f"{name}.json")
                base_input = "refs/heads/review-base" if name == "base" else self.base
                if name == "base":
                    self.git("branch", "review-base", self.base)
                frozen = self.freeze(output=path, repo_specs=[(self.repo, base_input)])
                self.assertEqual(0, frozen.returncode, frozen.stderr)
                before = (path.read_bytes(), path.stat().st_mtime_ns)
                mutate()
                result = self.run_cli("verify", "--manifest", str(path))
                self.assertEqual(3, result.returncode, result.stderr)
                self.assertTrue(result.stdout.startswith(f"STALE {self.load(path)['id']}\n"), result.stdout)
                self.assertEqual(before, (path.read_bytes(), path.stat().st_mtime_ns))
                self._restore_fixture(name)

    def _commit_new(self, name, content):
        (self.repo / name).write_text(content, encoding="utf-8")
        self.git("add", name)
        self.git("commit", "-qm", name)

    def _move_base_reference(self):
        self._commit_new("base-moved.txt", "base moved\n")
        moved = self.git("rev-parse", "HEAD").stdout.strip()
        self.git("reset", "--hard", self.base)
        self.git("branch", "-f", "review-base", moved)

    def _restore_fixture(self, name):
        if name == "head":
            self.git("reset", "--hard", self.base)
        elif name == "base":
            self.git("branch", "-D", "review-base")
        elif name == "untracked-set":
            (self.repo / "new.txt").unlink()
        elif name == "content":
            (self.repo / "tracked.txt").write_text("base\n", encoding="utf-8")

    def test_verify_detects_symlink_target_change(self):
        os.symlink("tracked.txt", self.repo / "link")
        self.assertEqual(0, self.freeze().returncode)
        (self.repo / "link").unlink()
        os.symlink("rename-me.txt", self.repo / "link")
        result = self.run_cli("verify", "--manifest", str(self.manifest_path()))
        self.assertEqual(3, result.returncode)

    def test_delta_reports_stable_difference_for_same_base_and_repo_set(self):
        old = self.manifest_path("old.json")
        new = self.manifest_path("new.json")
        self.assertEqual(0, self.freeze(output=old).returncode)
        (self.repo / "tracked.txt").write_text("changed\n", encoding="utf-8")
        self.assertEqual(0, self.freeze(output=new).returncode)

        result = self.run_cli("delta", "--from-manifest", str(old), "--to-manifest", str(new))

        self.assertEqual(0, result.returncode, result.stderr)
        payload = json.loads(result.stdout)
        self.assertEqual(self.load(old)["id"], payload["from_id"])
        self.assertEqual(self.load(new)["id"], payload["to_id"])
        self.assertEqual(["tracked.txt"], payload["repositories"][0]["changed_paths"])

    def test_delta_rejects_base_or_repository_set_change(self):
        old = self.manifest_path("old.json")
        self.assertEqual(0, self.freeze(output=old).returncode)
        self._commit_new("next.txt", "next\n")
        other_base = self.git("rev-parse", "HEAD").stdout.strip()
        changed_base = self.manifest_path("changed-base.json")
        self.assertEqual(0, self.freeze(output=changed_base, repo_specs=[(self.repo, other_base)]).returncode)
        result = self.run_cli("delta", "--from-manifest", str(old), "--to-manifest", str(changed_base))
        self.assertEqual(2, result.returncode)
        self.assertIn("base", result.stderr.lower())

        beta = self.workspace / "beta"
        beta.mkdir()
        self.git("init", "-q", cwd=beta)
        self.git("config", "user.email", "tests@example.invalid", cwd=beta)
        self.git("config", "user.name", "Tests", cwd=beta)
        (beta / "file").write_text("x\n", encoding="utf-8")
        self.git("add", "file", cwd=beta)
        self.git("commit", "-qm", "base", cwd=beta)
        beta_base = self.git("rev-parse", "HEAD", cwd=beta).stdout.strip()
        changed_repos = self.manifest_path("changed-repos.json")
        self.assertEqual(0, self.freeze(output=changed_repos, repo_specs=[(self.repo, self.base), (beta, beta_base)]).returncode)
        result = self.run_cli("delta", "--from-manifest", str(old), "--to-manifest", str(changed_repos))
        self.assertEqual(2, result.returncode)
        self.assertIn("repository", result.stderr.lower())


    def write_canonical_manifest(self, path, manifest):
        payload = {key: value for key, value in manifest.items() if key != "id"}
        canonical = json.dumps(
            payload, ensure_ascii=False, sort_keys=True, separators=(",", ":")
        ).encode("utf-8")
        manifest["id"] = hashlib.sha256(canonical).hexdigest()
        path.write_text(json.dumps(manifest), encoding="utf-8")

    def test_freeze_rejects_unignored_nested_git_repository(self):
        nested = self.repo / "nested"
        nested.mkdir()
        self.git("init", "-q", cwd=nested)
        self.git("config", "user.email", "tests@example.invalid", cwd=nested)
        self.git("config", "user.name", "Tests", cwd=nested)
        (nested / "inside.txt").write_text("inside\n", encoding="utf-8")
        self.git("add", "inside.txt", cwd=nested)
        self.git("commit", "-qm", "base", cwd=nested)

        result = self.freeze()

        self.assertEqual(2, result.returncode)
        self.assertEqual("", result.stdout)
        self.assertIn("nested git repository", result.stderr.lower())

    def test_ignored_nested_repository_can_be_frozen_as_independent_repo(self):
        nested = self.repo / "nested"
        nested.mkdir()
        self.git("init", "-q", cwd=nested)
        self.git("config", "user.email", "tests@example.invalid", cwd=nested)
        self.git("config", "user.name", "Tests", cwd=nested)
        (nested / "inside.txt").write_text("inside\n", encoding="utf-8")
        self.git("add", "inside.txt", cwd=nested)
        self.git("commit", "-qm", "base", cwd=nested)
        nested_base = self.git("rev-parse", "HEAD", cwd=nested).stdout.strip()
        (self.repo / ".gitignore").write_text(
            ".ai-local/\nignored/\nnested/\n", encoding="utf-8"
        )

        frozen = self.freeze(repo_specs=[(self.repo, self.base), (nested, nested_base)])
        self.assertEqual(0, frozen.returncode, frozen.stderr)
        (nested / "inside.txt").write_text("changed\n", encoding="utf-8")
        verified = self.run_cli("verify", "--manifest", str(self.manifest_path()))
        self.assertEqual(3, verified.returncode)
        self.assertIn(str(nested.resolve()), verified.stdout)

    def test_freeze_rejects_repo_and_output_symlink_escape(self):
        outside_repo = Path(self.tempdir.name) / "outside-repo"
        outside_repo.mkdir()
        self.git("init", "-q", cwd=outside_repo)
        (self.workspace / "repo-link").symlink_to(
            outside_repo, target_is_directory=True
        )
        repo_escape = self.freeze(
            repo_specs=[(self.workspace / "repo-link", self.base)]
        )
        self.assertEqual(2, repo_escape.returncode)
        self.assertIn("inside workspace", repo_escape.stderr.lower())

        outside_local = Path(self.tempdir.name) / "outside-local"
        outside_local.mkdir()
        (self.workspace / ".ai-local").symlink_to(
            outside_local, target_is_directory=True
        )
        output_escape = self.freeze()
        self.assertEqual(2, output_escape.returncode)
        self.assertIn("escapes workspace", output_escape.stderr.lower())

    def test_freeze_records_independent_deleted_file_status(self):
        (self.repo / "tracked.txt").unlink()

        result = self.freeze()

        self.assertEqual(0, result.returncode, result.stderr)
        repo = self.load()["repositories"][0]
        self.assertEqual(
            [{"status": "D", "paths": ["tracked.txt"]}], repo["unstaged"]
        )
        self.assertEqual("missing", repo["files"]["tracked.txt"]["kind"])
        self.assertIsNone(repo["files"]["tracked.txt"]["sha256"])
        self.assertIsNone(repo["files"]["tracked.txt"]["mode"])
        for layer in ("base", "head", "index"):
            self.assertEqual("100644", repo["files"]["tracked.txt"][layer]["mode"])

    def test_verify_and_delta_reject_canonical_but_malformed_schema(self):
        (self.repo / "schema.txt").write_text("schema\n", encoding="utf-8")
        self.git("add", "schema.txt")
        good = self.manifest_path("good.json")
        self.assertEqual(0, self.freeze(output=good).returncode)
        original = self.load(good)

        def missing_head(value):
            del value["repositories"][0]["head"]

        def bad_commit_sha(value):
            value["repositories"][0]["head"] = "not-a-commit"

        def bad_base_object(value):
            value["repositories"][0]["base"] = []

        def bad_scope_type(value):
            value["repositories"][0]["committed"] = {}

        def unsafe_scope_path(value):
            value["repositories"][0]["untracked"] = ["../escape"]

        def malformed_status(value):
            value["repositories"][0]["unstaged"] = [
                {"status": "R100", "paths": ["tracked.txt"]}
            ]

        def bad_file_hash(value):
            value["repositories"][0]["files"]["schema.txt"]["sha256"] = "bad"

        def bad_worktree_mode(value):
            value["repositories"][0]["files"]["schema.txt"]["mode"] = "755"

        def missing_git_mode(value):
            del value["repositories"][0]["files"]["schema.txt"]["base"]["mode"]

        def bad_git_mode(value):
            value["repositories"][0]["files"]["schema.txt"]["index"]["mode"] = "0100644"

        def forged_worktree_gitlink(value):
            value["repositories"][0]["files"]["schema.txt"]["kind"] = "gitlink"
            value["repositories"][0]["files"]["schema.txt"]["mode"] = "0644"

        def forged_layer_gitlink(value):
            value["repositories"][0]["files"]["schema.txt"]["index"]["kind"] = "gitlink"
            value["repositories"][0]["files"]["schema.txt"]["index"]["mode"] = "100644"

        def relative_workspace(value):
            value["workspace"] = "relative"

        def unknown_payload_field(value):
            value["unexpected"] = True

        def relative_repo_path(value):
            value["repositories"][0]["path"] = "relative/repo"

        def bad_base_sha(value):
            value["repositories"][0]["base"]["resolved"] = "bad"

        def bad_merge_base(value):
            value["repositories"][0]["merge_base"] = "bad"

        def bad_staged_type(value):
            value["repositories"][0]["staged"] = {}

        def bad_unstaged_type(value):
            value["repositories"][0]["unstaged"] = {}

        def bad_untracked_type(value):
            value["repositories"][0]["untracked"] = {}

        def malformed_entry(value):
            value["repositories"][0]["unstaged"] = ["not-an-entry"]

        def malformed_paths(value):
            value["repositories"][0]["unstaged"] = [
                {"status": "M", "paths": "tracked.txt"}
            ]

        def bad_files_type(value):
            value["repositories"][0]["files"] = []

        def missing_identity_layer(value):
            del value["repositories"][0]["files"]["schema.txt"]["index"]

        cases = [
            ("missing-field", missing_head, "head"),
            ("bad-commit", bad_commit_sha, "commit sha"),
            ("bad-base", bad_base_object, "base"),
            ("bad-scope", bad_scope_type, "committed"),
            ("unsafe-path", unsafe_scope_path, "safe relative"),
            ("bad-status", malformed_status, "rename"),
            ("bad-hash", bad_file_hash, "sha256"),
            ("bad-worktree-mode", bad_worktree_mode, "lstat mode"),
            ("missing-git-mode", missing_git_mode, "mode"),
            ("bad-git-mode", bad_git_mode, "git file mode"),
            ("forged-worktree-gitlink", forged_worktree_gitlink, "160000"),
            ("forged-layer-gitlink", forged_layer_gitlink, "160000"),
            ("relative-workspace", relative_workspace, "absolute"),
            ("unknown-payload", unknown_payload_field, "unknown field"),
            ("relative-repo", relative_repo_path, "absolute"),
            ("bad-base-sha", bad_base_sha, "commit sha"),
            ("bad-merge-base", bad_merge_base, "commit sha"),
            ("bad-staged", bad_staged_type, "staged"),
            ("bad-unstaged", bad_unstaged_type, "unstaged"),
            ("bad-untracked", bad_untracked_type, "untracked"),
            ("bad-entry", malformed_entry, "object"),
            ("bad-paths", malformed_paths, "paths"),
            ("bad-files", bad_files_type, "files"),
            ("missing-layer", missing_identity_layer, "index"),
        ]
        for name, mutate, reason in cases:
            with self.subTest(name=name):
                malformed = json.loads(json.dumps(original))
                mutate(malformed)
                path = self.manifest_path(f"malformed-{name}.json")
                self.write_canonical_manifest(path, malformed)
                commands = (
                    ("verify", ("--manifest", str(path))),
                    (
                        "delta",
                        (
                            "--from-manifest", str(good),
                            "--to-manifest", str(path),
                        ),
                    ),
                )
                for command, args in commands:
                    result = self.run_cli(command, *args)
                    self.assertEqual(2, result.returncode, result.stderr)
                    self.assertEqual("", result.stdout)
                    self.assertTrue(result.stderr.startswith("ERROR\t"), result.stderr)
                    self.assertIn(reason, result.stderr.lower())
                    self.assertNotIn("traceback", result.stderr.lower())

    def test_verify_and_delta_reject_tampered_id_and_duplicate_repo(self):
        good = self.manifest_path("good.json")
        self.assertEqual(0, self.freeze(output=good).returncode)
        tampered = self.manifest_path("tampered.json")
        manifest = self.load(good)
        manifest["id"] = "0" * 64
        tampered.write_text(json.dumps(manifest), encoding="utf-8")
        duplicate = self.manifest_path("duplicate.json")
        manifest = self.load(good)
        manifest["repositories"].append(
            json.loads(json.dumps(manifest["repositories"][0]))
        )
        self.write_canonical_manifest(duplicate, manifest)

        for path, reason in ((tampered, "canonical"), (duplicate, "duplicate")):
            with self.subTest(path=path.name):
                commands = (
                    ("verify", ("--manifest", str(path))),
                    (
                        "delta",
                        (
                            "--from-manifest", str(good),
                            "--to-manifest", str(path),
                        ),
                    ),
                )
                for command, args in commands:
                    result = self.run_cli(command, *args)
                    self.assertEqual(2, result.returncode)
                    self.assertEqual("", result.stdout)
                    self.assertIn(reason, result.stderr.lower())


    def test_delta_compares_valid_historical_manifests_without_live_repo(self):
        old = self.manifest_path("historical-old.json")
        new = self.manifest_path("historical-new.json")
        self.assertEqual(0, self.freeze(output=old).returncode)
        (self.repo / "tracked.txt").write_text("changed\n", encoding="utf-8")
        self.assertEqual(0, self.freeze(output=new).returncode)
        self.repo.rename(self.workspace / "repo-no-longer-present")

        result = self.run_cli(
            "delta", "--from-manifest", str(old), "--to-manifest", str(new)
        )

        self.assertEqual(0, result.returncode, result.stderr)
        payload = json.loads(result.stdout)
        self.assertEqual(["tracked.txt"], payload["repositories"][0]["changed_paths"])


    def test_verify_reports_stale_when_unstaged_tracked_mode_changes(self):
        (self.repo / "tracked.txt").write_text("changed\n", encoding="utf-8")
        self.assertEqual(0, self.freeze().returncode)
        os.chmod(self.repo / "tracked.txt", 0o755)

        result = self.run_cli(
            "verify", "--manifest", str(self.manifest_path())
        )

        self.assertEqual(3, result.returncode)
        self.assertTrue(result.stdout.startswith("STALE "))

    def test_verify_reports_stale_when_untracked_mode_changes(self):
        untracked = self.repo / "untracked.txt"
        untracked.write_text("same content\n", encoding="utf-8")
        self.assertEqual(0, self.freeze().returncode)
        os.chmod(untracked, 0o755)

        result = self.run_cli(
            "verify", "--manifest", str(self.manifest_path())
        )

        self.assertEqual(3, result.returncode)
        self.assertTrue(result.stdout.startswith("STALE "))

    def test_delta_reports_mode_only_change(self):
        old = self.manifest_path("mode-old.json")
        new = self.manifest_path("mode-new.json")
        (self.repo / "tracked.txt").write_text("changed\n", encoding="utf-8")
        self.assertEqual(0, self.freeze(output=old).returncode)
        os.chmod(self.repo / "tracked.txt", 0o755)
        self.assertEqual(0, self.freeze(output=new).returncode)

        result = self.run_cli(
            "delta", "--from-manifest", str(old), "--to-manifest", str(new)
        )

        self.assertEqual(0, result.returncode, result.stderr)
        payload = json.loads(result.stdout)
        self.assertEqual(
            ["tracked.txt"], payload["repositories"][0]["changed_paths"]
        )


    def add_submodule_fixture(self):
        source = Path(self.tempdir.name) / "submodule-source"
        source.mkdir()
        self.git("init", "-q", cwd=source)
        self.git("config", "user.email", "tests@example.invalid", cwd=source)
        self.git("config", "user.name", "Tests", cwd=source)
        (source / "inside.txt").write_text("old\n", encoding="utf-8")
        self.git("add", "inside.txt", cwd=source)
        self.git("commit", "-qm", "old", cwd=source)
        old_sha = self.git("rev-parse", "HEAD", cwd=source).stdout.strip()
        self.git(
            "-c", "protocol.file.allow=always", "submodule", "add", "-q",
            str(source), "sub",
        )
        self.git("commit", "-qam", "add submodule")
        self.base = self.git("rev-parse", "HEAD").stdout.strip()
        sub = self.repo / "sub"
        self.git("config", "user.email", "tests@example.invalid", cwd=sub)
        self.git("config", "user.name", "Tests", cwd=sub)
        return sub, old_sha

    def advance_submodule(self, sub):
        (sub / "inside.txt").write_text("new\n", encoding="utf-8")
        self.git("add", "inside.txt", cwd=sub)
        self.git("commit", "-qm", "new", cwd=sub)
        return self.git("rev-parse", "HEAD", cwd=sub).stdout.strip()

    def test_verify_valid_does_not_refresh_parent_or_gitlink_indexes(self):
        sub, _ = self.add_submodule_fixture()
        sub_base = self.git("rev-parse", "HEAD", cwd=sub).stdout.strip()
        specs = [(self.repo, self.base), (sub, sub_base)]
        self.assertEqual(0, self.freeze(repo_specs=specs).returncode)
        manifest = self.manifest_path()
        manifest_before = self.snapshot_path(manifest)
        indexes = [self.git_index_path(self.repo), self.git_index_path(sub)]
        self.touch_for_git_refresh(self.repo / "tracked.txt")
        self.touch_for_git_refresh(sub / "inside.txt")
        indexes_before = {path: self.snapshot_path(path) for path in indexes}

        verified = self.run_cli(
            "verify", "--manifest", str(manifest)
        )

        self.assertEqual(0, verified.returncode, verified.stderr)
        self.assertTrue(verified.stdout.startswith("VALID "))
        self.assertEqual(manifest_before, self.snapshot_path(manifest))
        for path, before in indexes_before.items():
            with self.subTest(index=path):
                self.assertEqual(before, self.snapshot_path(path))
                self.assertFalse(path.with_name("index.lock").exists())

    def test_freeze_stale_and_error_do_not_refresh_or_lock_git_indexes(self):
        sub, _ = self.add_submodule_fixture()
        sub_base = self.git("rev-parse", "HEAD", cwd=sub).stdout.strip()
        specs = [(self.repo, self.base), (sub, sub_base)]
        indexes = [self.git_index_path(self.repo), self.git_index_path(sub)]
        self.touch_for_git_refresh(self.repo / "tracked.txt")
        self.touch_for_git_refresh(sub / "inside.txt")
        before_freeze = {path: self.snapshot_path(path) for path in indexes}

        frozen = self.freeze(repo_specs=specs)

        self.assertEqual(0, frozen.returncode, frozen.stderr)
        for path, before in before_freeze.items():
            with self.subTest(phase="freeze", index=path):
                self.assertEqual(before, self.snapshot_path(path))
                self.assertFalse(path.with_name("index.lock").exists())

        (self.repo / "tracked.txt").write_text("stale content\n", encoding="utf-8")
        self.touch_for_git_refresh(sub / "inside.txt")
        before_stale = {path: self.snapshot_path(path) for path in indexes}
        stale = self.run_cli(
            "verify", "--manifest", str(self.manifest_path())
        )
        self.assertEqual(3, stale.returncode, stale.stderr)
        for path, before in before_stale.items():
            with self.subTest(phase="stale", index=path):
                self.assertEqual(before, self.snapshot_path(path))
                self.assertFalse(path.with_name("index.lock").exists())

        (sub / "inside.txt").write_text("dirty child\n", encoding="utf-8")
        before_error = {path: self.snapshot_path(path) for path in indexes}
        rejected = self.freeze(repo_specs=[(self.repo, self.base)])
        self.assertEqual(2, rejected.returncode)
        self.assertIn("dirty gitlink", rejected.stderr.lower())
        for path, before in before_error.items():
            with self.subTest(phase="error", index=path):
                self.assertEqual(before, self.snapshot_path(path))
                self.assertFalse(path.with_name("index.lock").exists())

    def test_literal_pathspec_names_freeze_and_verify(self):
        tracked = self.repo / ":(glob)**"
        staged = self.repo / "*"
        untracked = self.repo / "colon:name"
        tracked.write_text("tracked\n", encoding="utf-8")
        self.git("--literal-pathspecs", "add", "--", tracked.name)
        self.git("commit", "-qm", "literal tracked")
        staged.write_text("staged\n", encoding="utf-8")
        self.git("--literal-pathspecs", "add", "--", staged.name)
        untracked.write_text("untracked\n", encoding="utf-8")

        frozen = self.freeze()

        self.assertEqual(0, frozen.returncode, frozen.stderr)
        repo = self.load()["repositories"][0]
        self.assertIn(
            {"status": "A", "paths": [":(glob)**"]}, repo["committed"]
        )
        self.assertIn({"status": "A", "paths": ["*"]}, repo["staged"])
        self.assertIn("colon:name", repo["untracked"])
        tracked.write_text("changed tracked\n", encoding="utf-8")
        os.chmod(staged, 0o755)
        untracked.write_text("changed untracked\n", encoding="utf-8")
        verified = self.run_cli(
            "verify", "--manifest", str(self.manifest_path())
        )
        self.assertEqual(3, verified.returncode)

    def test_gitlink_pointer_identity_and_change_are_stale(self):
        sub, old_sha = self.add_submodule_fixture()
        new_sha = self.advance_submodule(sub)
        self.git("add", "sub")

        frozen = self.freeze()

        self.assertEqual(0, frozen.returncode, frozen.stderr)
        identity = self.load()["repositories"][0]["files"]["sub"]
        self.assertEqual("gitlink", identity["kind"])
        self.assertEqual("160000", identity["mode"])
        self.assertEqual("160000", identity["base"]["mode"])
        self.assertEqual("160000", identity["head"]["mode"])
        self.assertEqual("160000", identity["index"]["mode"])
        self.assertNotEqual(
            identity["head"]["sha256"], identity["index"]["sha256"]
        )
        self.git("checkout", "-q", old_sha, cwd=sub)
        verified = self.run_cli(
            "verify", "--manifest", str(self.manifest_path())
        )
        self.assertEqual(3, verified.returncode)
        self.assertNotEqual(old_sha, new_sha)

    def test_gitlink_initialization_state_change_is_stale(self):
        sub, _ = self.add_submodule_fixture()
        self.advance_submodule(sub)
        self.git("add", "sub")
        self.assertEqual(0, self.freeze().returncode)
        self.git("submodule", "deinit", "-q", "-f", "--", "sub")

        verified = self.run_cli(
            "verify", "--manifest", str(self.manifest_path())
        )

        self.assertEqual(3, verified.returncode)

    def test_dirty_gitlink_requires_explicit_repo_spec(self):
        sub, _ = self.add_submodule_fixture()
        (sub / "inside.txt").write_text("dirty\n", encoding="utf-8")

        rejected = self.freeze()

        self.assertEqual(2, rejected.returncode)
        self.assertIn("dirty gitlink", rejected.stderr.lower())
        sub_base = self.git("rev-parse", "HEAD", cwd=sub).stdout.strip()
        accepted = self.freeze(
            repo_specs=[(self.repo, self.base), (sub, sub_base)]
        )
        self.assertEqual(0, accepted.returncode, accepted.stderr)
        (sub / "inside.txt").write_text("dirtier\n", encoding="utf-8")
        verified = self.run_cli(
            "verify", "--manifest", str(self.manifest_path())
        )
        self.assertEqual(3, verified.returncode)
        self.assertIn(str(sub.resolve()), verified.stdout)


    def test_new_staged_gitlink_freezes_and_change_is_stale(self):
        source = Path(self.tempdir.name) / "new-submodule-source"
        source.mkdir()
        self.git("init", "-q", cwd=source)
        self.git("config", "user.email", "tests@example.invalid", cwd=source)
        self.git("config", "user.name", "Tests", cwd=source)
        (source / "inside.txt").write_text("old\n", encoding="utf-8")
        self.git("add", "inside.txt", cwd=source)
        self.git("commit", "-qm", "old", cwd=source)
        self.git(
            "-c", "protocol.file.allow=always", "submodule", "add", "-q",
            str(source), "new-sub",
        )
        sub = self.repo / "new-sub"
        self.git("config", "user.email", "tests@example.invalid", cwd=sub)
        self.git("config", "user.name", "Tests", cwd=sub)

        frozen = self.freeze()

        self.assertEqual(0, frozen.returncode, frozen.stderr)
        identity = self.load()["repositories"][0]["files"]["new-sub"]
        self.assertEqual("missing", identity["base"]["kind"])
        self.assertEqual("missing", identity["head"]["kind"])
        self.assertEqual("gitlink", identity["index"]["kind"])
        self.assertEqual("160000", identity["index"]["mode"])
        self.advance_submodule(sub)
        verified = self.run_cli(
            "verify", "--manifest", str(self.manifest_path())
        )
        self.assertEqual(3, verified.returncode)


    def prepare_deinitialized_gitlink(self):
        sub, _ = self.add_submodule_fixture()
        self.advance_submodule(sub)
        self.git("add", "sub")
        self.git("submodule", "deinit", "-q", "-f", "--", "sub")
        return sub

    def load_review_manifest_module(self):
        spec = importlib.util.spec_from_file_location(
            "review_manifest_under_test", SCRIPT
        )
        module = importlib.util.module_from_spec(spec)
        self.assertIsNotNone(spec.loader)
        spec.loader.exec_module(module)
        return module

    def test_gitlink_symlink_swap_never_queries_replacement_repository(self):
        sub, _ = self.add_submodule_fixture()
        module = self.load_review_manifest_module()
        replacement = Path(self.tempdir.name) / "external-replacement"
        self.git("clone", "-q", str(sub), str(replacement))
        held = self.repo / "held-sub"
        real_run = subprocess.run
        swapped = False
        replacement_git_queried = False

        def swap_before_git(command, *args, **kwargs):
            nonlocal swapped, replacement_git_queried
            if not swapped and "rev-parse" in command and (
                "--show-toplevel" in command or "--show-prefix" in command
            ):
                sub.rename(held)
                sub.symlink_to(replacement, target_is_directory=True)
                swapped = True
            if swapped and "-C" in command:
                git_dir = Path(command[command.index("-C") + 1])
                try:
                    replacement_git_queried |= (
                        os.stat(git_dir).st_ino == os.stat(replacement).st_ino
                    )
                except OSError:
                    pass
            return real_run(command, *args, **kwargs)

        with mock.patch.object(module.subprocess, "run", side_effect=swap_before_git):
            with self.assertRaisesRegex(module.InputError, "changed|replacement"):
                module._file_identity(
                    self.repo, self.base, self.base, "sub", set()
                )
        self.assertTrue(swapped)
        self.assertFalse(replacement_git_queried)

    def test_gitlink_rename_replacement_is_rejected_after_bound_queries(self):
        sub, _ = self.add_submodule_fixture()
        module = self.load_review_manifest_module()
        replacement = Path(self.tempdir.name) / "directory-replacement"
        self.git("clone", "-q", str(sub), str(replacement))
        held = self.repo / "held-sub"
        real_run = subprocess.run
        swapped = False
        replacement_git_queried = False

        def replace_before_git(command, *args, **kwargs):
            nonlocal swapped, replacement_git_queried
            if not swapped and "rev-parse" in command and (
                "--show-toplevel" in command or "--show-prefix" in command
            ):
                sub.rename(held)
                replacement.rename(sub)
                swapped = True
            if swapped and "-C" in command:
                git_dir = Path(command[command.index("-C") + 1])
                try:
                    replacement_git_queried |= (
                        os.stat(git_dir).st_ino == os.stat(sub).st_ino
                    )
                except OSError:
                    pass
            return real_run(command, *args, **kwargs)

        with mock.patch.object(module.subprocess, "run", side_effect=replace_before_git):
            with self.assertRaisesRegex(module.InputError, "changed|replacement"):
                module._file_identity(
                    self.repo, self.base, self.base, "sub", set()
                )
        self.assertTrue(swapped)
        self.assertFalse(replacement_git_queried)

    def test_uninitialized_gitlink_replacement_during_fd_enumeration_is_rejected(self):
        sub = self.prepare_deinitialized_gitlink()
        module = self.load_review_manifest_module()
        held = self.repo / "held-empty-sub"
        real_listdir = os.listdir
        enumerated_fd = False
        enumerated_names = None

        def replace_while_enumerating(path):
            nonlocal enumerated_fd, enumerated_names
            if isinstance(path, int) and not enumerated_fd:
                enumerated_fd = True
                sub.rename(held)
                sub.mkdir()
                (sub / "external-marker").write_bytes(b"do not read")
            enumerated_names = real_listdir(path)
            return enumerated_names

        with mock.patch.object(module.os, "listdir", side_effect=replace_while_enumerating):
            with self.assertRaisesRegex(module.InputError, "changed|replacement"):
                module._file_identity(
                    self.repo, self.base, self.base, "sub", set()
                )
        self.assertTrue(enumerated_fd)
        self.assertEqual([], enumerated_names)

    def test_empty_deinitialized_gitlink_freezes(self):
        sub = self.prepare_deinitialized_gitlink()
        self.assertEqual([], list(sub.iterdir()))

        frozen = self.freeze()

        self.assertEqual(0, frozen.returncode, frozen.stderr)
        identity = self.load()["repositories"][0]["files"]["sub"]
        self.assertEqual("gitlink", identity["kind"])
        self.assertEqual("160000", identity["mode"])

    def test_nonempty_deinitialized_gitlink_is_rejected_before_freeze(self):
        sub = self.prepare_deinitialized_gitlink()
        (sub / ".unknown").write_text("unknown\n", encoding="utf-8")

        frozen = self.freeze()

        self.assertEqual(2, frozen.returncode)
        self.assertEqual("", frozen.stdout)
        self.assertIn("non-empty uninitialized gitlink", frozen.stderr.lower())

    def test_deinitialized_gitlink_empty_to_nonempty_verify_fails_closed(self):
        sub = self.prepare_deinitialized_gitlink()
        self.assertEqual(0, self.freeze().returncode)
        (sub / "unknown").write_text("unknown\n", encoding="utf-8")

        verified = self.run_cli(
            "verify", "--manifest", str(self.manifest_path())
        )

        self.assertEqual(2, verified.returncode)
        self.assertEqual("", verified.stdout)
        self.assertIn("non-empty uninitialized gitlink", verified.stderr.lower())

    def test_uninitialized_gitlink_iteration_error_fails_closed(self):
        spec = importlib.util.spec_from_file_location(
            "review_manifest_under_test", SCRIPT
        )
        module = importlib.util.module_from_spec(spec)
        self.assertIsNotNone(spec.loader)
        spec.loader.exec_module(module)
        target = self.repo / "empty-gitlink"
        target.mkdir()
        with mock.patch.object(
            module.os, "listdir", side_effect=PermissionError("denied")
        ):
            with self.assertRaisesRegex(module.InputError, "cannot inspect"):
                module._gitlink_worktree(self.repo, target.name, set())

if __name__ == "__main__":
    unittest.main()
