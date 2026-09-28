# Tasks

## 1. Open / Design

1. [x] Record the observed desktop 401 and reconcile it with launcher/gateway implementation and installed desktop behavior.
2. [x] Confirm this four-artifact specification before any implementation.
3. [x] Obtain the required second implementation confirmation because this change alters desktop authentication behavior.

## 2. Build

1. [x] Add a failing agent test asserting the managed desktop provider contains `env_key = "AGW_API_KEY"` and `config.toml` contains no token value.
2. [x] Add a failing `PrepareExec` desktop test asserting the returned environment contains both the isolated `CODEX_HOME` and the selected project token, with the project token value overriding any pre-existing launcher variable semantics represented by the returned map.
3. [x] Implement the provider field in `internal/agent/desktop_home.go` and set `AGW_API_KEY` only for `KindCodexDesktop` in `internal/agent/run.go`.
4. [x] Update README and usage-guide wording for the desktop credential path.
5. [x] Run focused tests with `GOCACHE=/tmp/agw-gocache go test ./internal/agent -run 'Test.*(Desktop|CodexDesktop)' -count=1`; expected exit 0.

## 3. Verify

1. [x] Run `GOCACHE=/tmp/agw-gocache go test ./... -race`; sandbox local-listen restrictions caused `internal/cli` and `internal/gateway` httptest panics. `GOCACHE=/tmp/agw-gocache go test ./cmd/... ./internal/agent/... ./internal/config/... ./internal/protocol/... ./internal/provider/... ./internal/workspace/... -race` passed, and non-race `go test ./...` reproduced the same two pre-existing sandbox-only listen failures.
2. [x] Run `go vet ./...`, `gofmt` on changed Go files, `openspec validate fix-codex-desktop-provider-env --strict --no-interactive`, and `git diff --check`.
3. [x] Freeze the strict authentication invariant with `.ai/tools/review_manifest.py freeze`, perform task-level review, and resolve every Critical/Important finding (manifest `3ad16082e6896e6d3dc868d600d8c480635b278d241ab9c596b7b2cb476d5989`; no Critical/Important finding).
4. [x] Perform two independent Verify reviews with fresh manifests for specification conformance and code quality (manifest `1682c7a9ad5341df325501b26a50fb140e8ea8a45991d10db18a07943849957b`; both PASS).
5. [ ] Run one local desktop smoke launch through `agw run codex-desktop`, then create a thread and verify the gateway receives an authorized structured title request; do not print the token.

## 4. Archive

1. [ ] Persist final evidence, comparison bases, unverified scope, and residual risk.
2. [ ] Merge the delta into `openspec/specs/agent-launcher/spec.md`.
3. [ ] Update `.ai/kb/projects/agent_gateway.md` and `.ai/memory/agent-launcher.md`.
4. [ ] Run `bash scripts/validate-workflow.sh --archive-light`; if workflow/contract/governance files changed after effective Verify, run the required full gate instead.
5. [ ] Move the change to `openspec/archive/` and retain the feature branch/worktree until explicit user integration authorization.

## Local Integration Strategy

Create `feature/fix-codex-desktop-provider-env`, commit only this change’s confirmed OpenSpec artifacts and later implementation files, then mount the branch in an ignored strict worktree because the current checkout contains unrelated `install-herdr-skill` work. Do not touch, stage, merge, push, or delete that unrelated work.
