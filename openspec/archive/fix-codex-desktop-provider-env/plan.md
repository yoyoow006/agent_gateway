# Implementation Plan: fix-codex-desktop-provider-env

## Goal

Make Codex Desktop internal structured Responses requests—including thread title generation—authenticate to agw with the selected global/project virtual token by:

1. adding `env_key = "AGW_API_KEY"` to the managed desktop provider; and
2. injecting `AGW_API_KEY=<selected token>` into the `codex-desktop` child environment.

## Architecture And Constraints

- Runtime: Go 1.24 module `agent_gateway`.
- Primary files:
  - Modify `internal/agent/desktop_home.go`.
  - Modify `internal/agent/run.go`.
  - Modify `internal/agent/desktop_home_test.go` and `internal/agent/desktop_exec_test.go`.
  - Update `README.md` and `docs/usage-guide.md`.
- Global constraints, copied verbatim from the confirmed proposal:
  - “`config.toml` 仍不含密钥，只引用环境变量名。”
  - “`AGW_API_KEY` 会进入桌面子进程环境，但 token 本身仍不进入日志、argv 或 profile 文件。”
  - “不修改用户默认 Codex home；不支持 macOS/Windows；不改变 `agw run codex` CLI profile；不把 token 写入 `config.toml` 或日志。”
  - “保留 `auth.json` 仅含 `OPENAI_API_KEY` 的现有语义，不迁移或修改官方 OAuth、默认 `~/.codex`。”
- `CODEX_HOME` remains exactly `<gateway root>/.agw/codex-desktop`.
- Existing app discovery, token selection, project inference, gateway auth validation, backup/rollback behavior, and reset boundaries remain unchanged.
- The unrelated untracked `install-herdr-skill` change is out of scope and must remain untouched.

## Task 1: Red tests for managed provider authentication

Create/modify tests in `internal/agent/desktop_home_test.go`:

1. Extend the TOML decode struct used by `assertDesktopConfig` with `EnvKey string tom:"env_key"`.
2. Assert `cfg.ModelProviders["agw"].EnvKey == "AGW_API_KEY"` for every generated desktop home.
3. In the direct home-writer test, read generated `config.toml` as text and assert it does not contain the literal test token.
4. Run:
   ```bash
   GOCACHE=/tmp/agw-gocache go test ./internal/agent -run 'TestEnsureDesktopHome|TestDesktop' -count=1
   ```
   Expected before implementation: failure because generated provider has no `env_key`.
5. Implement only the template addition in `internal/agent/desktop_home.go`:
   ```toml
   env_key = "AGW_API_KEY"
   ```
6. Re-run step 4. Expected: exit 0.

## Task 2: Red tests and implementation for child token injection

Modify `internal/agent/desktop_exec_test.go`:

1. In `TestPrepareExecCodexDesktopUsesProjectAndIsolatedHome`, set a conflicting launcher value with `t.Setenv("AGW_API_KEY", "external-token")`.
2. Replace the current “desktop token must not enter process env” assertion with:
   - `env["AGW_API_KEY"] == "agw-foo"`;
   - `env["CODEX_HOME"] == DesktopHome(root)`.
3. Add or extend a unit test for final environment construction:
   - input environment contains `AGW_API_KEY=external-token`;
   - override map contains `AGW_API_KEY=agw-foo` and `CODEX_HOME=<managed>`;
   - resulting duplicate list has the override occurrence after the inherited occurrence;
   - parsing from left to right yields `agw-foo`.
4. Run:
   ```bash
   GOCACHE=/tmp/agw-gocache go test ./internal/agent -run 'TestPrepareExecCodexDesktop|Test.*Environment|Test.*Exec' -count=1
   ```
   Expected before implementation: desktop token assertion fails.
5. Add a narrow internal helper in `internal/agent/run.go` that appends sorted override variables after the inherited environment, use it from `Exec`, and set `env["AGW_API_KEY"] = token` only in the `KindCodexDesktop` branch after `EnsureDesktopHome` succeeds.
6. Re-run step 4. Expected: exit 0.
7. Run:
   ```bash
   GOCACHE=/tmp/agw-gocache go test ./internal/agent -count=1
   ```
   Expected: exit 0.

## Task 3: Documentation

Modify:

- `README.md`: in the desktop overview and launcher table/protocol section, state that `codex-desktop` receives both isolated `CODEX_HOME` and `AGW_API_KEY`, while `config.toml` references the variable and contains no token.
- `docs/usage-guide.md`: in “独立配置” and “登录与会话”, explain that provider authentication and internal structured requests use `AGW_API_KEY`; `auth.json` remains `OPENAI_API_KEY` only.

Validation:

```bash
grep -RIn --exclude-dir=.git 'env_key = "AGW_API_KEY"' README.md docs/usage-guide.md internal/agent
git diff --check
```

Expected: docs describe the field, implementation defines it once, and diff check exits 0.

## Task 4: Strict verification

Run all of:

```bash
GOCACHE=/tmp/agw-gocache go test ./... -race
go vet ./...
gofmt -w internal/agent/desktop_home.go internal/agent/run.go internal/agent/desktop_home_test.go internal/agent/desktop_exec_test.go
openspec validate fix-codex-desktop-provider-env --strict --no-interactive
git diff --check
```

Expected every command to exit 0. If sandbox networking makes `-race` unavailable, run `GOCACHE=/tmp/agw-gocache go test ./...` plus focused race tests and record the exact limitation; do not silently skip.

Then:

1. Freeze the desktop authentication invariant with `.ai/tools/review_manifest.py freeze`.
2. Perform task-level review for the authentication boundary; verify manifests before reading and before conclusion.
3. Resolve every Critical/Important finding with minimal in-scope fixes and fresh evidence.
4. Run independent specification-conformance and code-quality Verify reviews with fresh manifests.
5. Perform local smoke only after the user closes any existing Codex Desktop process:
   ```bash
   ./agw run codex-desktop --project <relevant-project>
   ```
   Create a thread and confirm title generation no longer logs the observed 401. Do not print, trace, or paste the token.

## Task 5: Archive

1. Persist final evidence, comparison bases, unverified scope, and residual risk.
2. Merge delta requirements into `openspec/specs/agent-launcher/spec.md`.
3. Update `.ai/kb/projects/agent_gateway.md` and `.ai/memory/agent-launcher.md`.
4. Run:
   ```bash
   bash scripts/validate-workflow.sh --archive-light
   ```
   If workflow-executable, assistant-entry, skill semantic, contract-test, or governance-spec files changed after effective Verify, run the required full gate instead.
5. Move the change to `openspec/archive/`, update the archive index, and retain the feature branch/worktree until the user explicitly authorizes integration.

## Branch / Worktree Strategy

After second confirmation:

1. Record current baseline `main`.
2. Create and checkout `feature/fix-codex-desktop-provider-env` while preserving unrelated untracked work.
3. Stage only:
   - `openspec/changes/fix-codex-desktop-provider-env/`
   - `openspec/plan/fix-codex-desktop-provider-env.md`
4. Set proposal status to `构建中`, commit only those files, and switch back to `main`.
5. Mount the already-existing untracked feature branch under ignored `.worktrees/fix-codex-desktop-provider-env` using the repository git-worktrees procedure.
6. Verify the worktree branch/status and that `main` still contains the untouched unrelated work.
7. Do not push, merge, rebase, delete work, or commit unrelated files.

## Commit Strategy

1. Commit confirmed OpenSpec artifacts plus plan before implementation.
2. Commit the red/green authentication implementation and tests as one independently revertible unit.
3. Commit documentation separately.
4. Commit verification evidence/archive integration separately when all gates pass.
