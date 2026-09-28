# Tasks

## 1. Open / Design

1. [x] Obtain user confirmation for scope, isolated desktop home, Linux-only support, and worktree strategy.
2. [x] Read `design`, produce `openspec/plan/adapt-codex-desktop.md`, and self-review it against the confirmed spec.
3. [ ] Decide whether the plan introduces new choices, dependencies, or unconfirmed external side effects; if so, request the required second confirmation.

## 2. Build: Linux desktop app resolver

1. [x] Add failing tests for `AGW_CODEX_APP`, default root ordering, app-directory normalization, executable-name recognition, missing-app failure, and unsupported automatic discovery outside Linux.
2. [x] Implement the bounded Linux resolver and executable normalization in `internal/agent/`.
3. [x] Run `GOCACHE=/tmp/agw-gocache go test ./internal/agent -run 'TestResolve.*Desktop|Test.*Desktop.*(App|Resolve|Unsupported)' -count=1` and confirm the new tests pass.

## 3. Build: isolated managed Codex home writer

1. [x] Add failing tests for config/auth content, IPv4 and IPv6 gateway URLs, permissions, no-op mtime preservation, timestamped backup, atomic write failure rollback, invalid managed content rejection, symlink escape rejection, and reset path safety.
2. [x] Implement `<root>/.agw/codex-desktop` path validation, managed TOML/JSON generation, same-directory atomic replacement, backup lifecycle, rollback, and guarded reset.
3. [x] Run `GOCACHE=/tmp/agw-gocache go test ./internal/agent -run 'Test.*(DesktopHome|DesktopConfig|DesktopAuth|DesktopAtomic|DesktopReset)' -count=1` and confirm all cases pass.

## 4. Build: CLI integration and docs

1. [x] Add failing agent/CLI tests proving `codex-desktop` is distinct from `codex`, infers project profile, sets only the child `CODEX_HOME`, resolves the app before writes, supports `--reset`, and preserves existing CLI zero-touch assertions.
2. [x] Implement `KindCodexDesktop`, preparation API, CLI acceptance and flags, executable launch/delegation behavior, and actionable Linux-only errors.
3. [x] Update `README.md` and `docs/usage-guide.md` with prerequisites, isolated home semantics, login/token tradeoff, reset/uninstall behavior, platform limitation, and troubleshooting.
4. [x] Run `GOCACHE=/tmp/agw-gocache go test ./internal/agent -count=1` and targeted CLI tests; full CLI suite is blocked by the pre-existing sandbox local-listen restriction recorded in baseline evidence.

## 5. Verify

1. [ ] Run full regression: `go test ./... -race` (if sandbox networking constraints block it, run `go test ./...` plus available narrow race tests and record the exact limitation).
2. [ ] Run `go vet ./...`, format changed Go files with `gofmt`, and run `git diff --check`.
3. [ ] Run `openspec validate adapt-codex-desktop --strict --no-interactive`.
4. [ ] Inspect the complete diff and confirm no CodexPlusPlus code or third-party assets are copied.
5. [ ] Perform strict task-level review for the isolated authentication-home mutation and guarded reset unit using a shared review manifest; resolve all Critical/Important findings.
6. [ ] Run two independent Verify reviews for specification conformance and code quality using fresh manifests.
7. [ ] If a supported Linux desktop application is installed on the host, perform one manual launch smoke test without exposing the token; otherwise record the desktop launch environment as NOT_RUN and retain automated fake-app evidence.

## 6. Archive

1. [ ] Resolve every review finding and persist final evidence, comparison bases, unverified scope, and residual risk.
2. [ ] Merge the accepted delta into `openspec/specs/agent-launcher/spec.md`.
3. [ ] Update `.ai/kb/projects/agent_gateway.md` and add a concise `.ai/memory/agent-launcher.md` lesson about isolated Linux Codex desktop configuration.
4. [ ] Run `bash scripts/validate-workflow.sh --archive-light`; if workflow/contract/governance files changed after the latest effective Verify, run the required full gate instead.
5. [ ] Move the change to `openspec/archive/`, update the archive index, and retain the feature branch/worktree until the user explicitly requests integration.

## Local Integration Strategy

Implement in a strict isolated worktree rooted at ignored `.worktrees/adapt-codex-desktop` on `feature/adapt-codex-desktop`, because the current checkout has unrelated untracked work. Keep the unrelated `install-herdr-skill` change untouched. Run tests and reviews in that worktree. Do not push, merge to main, or delete the worktree without explicit user authorization.
