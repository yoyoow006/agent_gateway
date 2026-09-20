# Review Findings

## Manifest

- Final ID: `8cd549444e2c683be8e4e60241295cf0e4b9ba5fe861c5a495aac9f9e8e83930`
- Review ID: `3af2214e971d83e8b180dd3627ceb551b2678c21c14b5c1bf8fd0da8f46e3aa4` (delta after recording findings)
- Repo: `/media/shitou/石头/wksource/git_me_prj/agent_gateway/projects/agent_gateway`
- Comparison base: `main` @ `7ceb0d1293f75ad8501453eba48bd58f7512e497`
- Scope: branch `feature/fix-install-root-and-project-rollback` full diff including unstaged implementation files.

## Verdict

PASS.

## Findings

无 Critical/Important/Minor finding。

## Evidence

- F-001：
  - Red: `go test ./internal/config -run TestDocumentationProviderBaseURLsOmitV1Path -count=1` initially failed on three `/v1` examples.
  - Green after documentation fix.
  - Current grep for `relay.example/v1` and `api.openai.com/v1` returns no matches.
- F-002：
  - Red: `resolveRootE` tests initially failed to compile because only permissive `resolveRoot` existed.
  - Green: invalid `--root`, invalid `AGW_ROOT`, and valid `--root` over invalid env are covered.
  - All root-requiring commands use the fail-fast `resolveRoot()` wrapper.
- F-007：
  - Red: `TestNewInvalidConfigLeavesNoProject` initially failed because `projects/demo` remained after invalid config.
  - Green: config load now happens before project directory creation.
  - Existing valid creation and conflict tests pass.

## Verification

- `go test ./internal/cli -run 'TestResolveRoot' -count=1` — PASS
- `go test ./internal/workspace -count=1` — PASS
- `go test ./internal/config -run 'TestDocumentationProviderBaseURLsOmitV1Path|TestDefaultTOMLLoadable' -count=1` — PASS
- `go test -race ./internal/protocol/... ./internal/provider ./internal/workspace ./internal/agent -count=1` — PASS
- `go build ./...` — PASS
- `go vet ./...` — PASS
- `gofmt -l .` — no output
- `bash scripts/validate-workflow.sh --fast` — `PASS=199 FAIL=0 SKIP=0`
- `openspec validate fix-install-root-and-project-rollback --strict --no-interactive` — valid
- `git diff --check` — PASS
- Manifest verify before and after review — `VALID 3af2214e971d83e8b180dd3627ceb551b2678c21c14b5c1bf8fd0da8f46e3aa4`
- Final manifest delta covers only review-findings.md and tasks.md; final verify — `VALID 8cd549444e2c683be8e4e60241295cf0e4b9ba5fe861c5a495aac9f9e8e83930`

## Unverified

- Full `go test -race ./...` cannot run in this sandbox: `httptest` local listeners are denied, and the pre-existing `TestFindRoot` is affected by the session's inherited `AGW_ROOT`.
- Real external provider requests and real Claude/Codex installation were not exercised.

## Residual risk

- Explicit root validation checks existence of the `config/` directory, as specified; it does not additionally validate that the directory contains a loadable config file. Subsequent config loading still reports concrete parse errors.
