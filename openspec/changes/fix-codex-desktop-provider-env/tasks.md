# Tasks

## 1. Open / Design

1. [x] Record the observed desktop 401 and reconcile it with launcher/gateway implementation and installed desktop behavior.
2. [x] Confirm this four-artifact specification before any implementation.
3. [x] Obtain the required second implementation confirmation because this change alters desktop authentication behavior.

## 2. Build

1. [ ] Add a failing agent test asserting the managed desktop provider contains `env_key = "AGW_API_KEY"` and `config.toml` contains no token value.
2. [ ] Add a failing `PrepareExec` desktop test asserting the returned environment contains both the isolated `CODEX_HOME` and the selected project token, with the project token value overriding any pre-existing launcher variable semantics represented by the returned map.
3. [ ] Implement the provider field in `internal/agent/desktop_home.go` and set `AGW_API_KEY` only for `KindCodexDesktop` in `internal/agent/run.go`.
4. [ ] Update README and usage-guide wording for the desktop credential path.
5. [ ] Run focused tests with `GOCACHE=/tmp/agw-gocache go test ./internal/agent -run 'Test.*(Desktop|CodexDesktop)' -count=1`; expected exit 0.

## 3. Verify

1. [ ] Run `GOCACHE=/tmp/agw-gocache go test ./... -race`; if the sandbox blocks an unavoidable listen, run `GOCACHE=/tmp/agw-gocache go test ./...` plus available focused race tests and record the exact limitation.
2. [ ] Run `go vet ./...`, `gofmt` on changed Go files, `openspec validate fix-codex-desktop-provider-env --strict --no-interactive`, and `git diff --check`.
3. [ ] Freeze the strict authentication invariant with `.ai/tools/review_manifest.py freeze`, perform task-level review, and resolve every Critical/Important finding.
4. [ ] Perform two independent Verify reviews with fresh manifests for specification conformance and code quality.
5. [ ] Run one local desktop smoke launch through `agw run codex-desktop`, then create a thread and verify the gateway receives an authorized structured title request; do not print the token.

## 4. Archive

1. [ ] Persist final evidence, comparison bases, unverified scope, and residual risk.
2. [ ] Merge the delta into `openspec/specs/agent-launcher/spec.md`.
3. [ ] Update `.ai/kb/projects/agent_gateway.md` and `.ai/memory/agent-launcher.md`.
4. [ ] Run `bash scripts/validate-workflow.sh --archive-light`; if workflow/contract/governance files changed after effective Verify, run the required full gate instead.
5. [ ] Move the change to `openspec/archive/` and retain the feature branch/worktree until explicit user integration authorization.

## Local Integration Strategy

Create `feature/fix-codex-desktop-provider-env`, commit only this change’s confirmed OpenSpec artifacts and later implementation files, then mount the branch in an ignored strict worktree because the current checkout contains unrelated `install-herdr-skill` work. Do not touch, stage, merge, push, or delete that unrelated work.
