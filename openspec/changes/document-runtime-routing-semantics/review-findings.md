# Review Findings

## Manifest

- Final ID: `b9a479790f5447cd015b3b4b957c9a61c71851c77ca7b9e0ff6598b2d870ff6c`
- Comparison base: `main` @ `0e320a7dd21cd3b6b8a419ac4da25d9681dc7ff45`
- Scope: full diff from `main` to working tree for README, usage guide, documentation contract test, and project template comment.

## Verdict

PASS.

## Findings

无 Critical/Important/Minor finding。

## Evidence

- F-003 / project ordering:
  - Implementation `internal/config/config.go:384-406` sorts enabled providers by `priority`, then name, and only moves `preferred` to the front.
  - README and usage guide now state candidate-subset semantics, priority/name ordering, and preferred moving the healthy choice to the top.
  - Project template comment now says candidate subset and priority/name ordering.
- F-004 / timeout reload:
  - `internal/gateway/forward.go:541-570` caches one HTTP client per provider name, so transport timeout fields require a gateway restart.
  - README and usage guide now explicitly separate hot-reload fields from restart-required timeout fields.
- F-005 / failover statuses:
  - `internal/gateway/forward.go:24-29` retryable statuses are exactly 401, 403, 408, 429, 500, 502, 503, 504, 529.
  - README and usage guide now use the exact list and document pass-through behavior for non-list 5xx such as 501/505.
- Contract test `TestDocumentationRoutingSemanticsMatchRuntime` initially failed on all three stale semantics, then passed after documentation updates.

## Verification

- `go test ./internal/config -run TestDocumentationRoutingSemanticsMatchRuntime -count=1` — PASS
- `go test ./internal/config -run 'TestDocumentationRoutingSemanticsMatchRuntime|TestDefaultTOMLLoadable|TestLoadMergePrecedence|TestProviderDefaultModel' -count=1` — PASS
- `go test ./internal/workspace ./internal/config -count=1` — FAIL only pre-existing `TestFindRoot` due inherited `AGW_ROOT`; unaffected targeted config tests and all workspace tests pass.
- `go build ./...` — PASS
- `go vet ./...` — PASS
- `gofmt -l .` — no output
- `bash scripts/validate-workflow.sh --fast` — `PASS=199 FAIL=0 SKIP=0`
- `openspec validate document-runtime-routing-semantics --strict --no-interactive` — valid
- `git diff --check` — PASS
- Manifest verify before/after review — `VALID d5a163f2d47810dd179ac8134e312655d96bbe921df1cc280c1d625be76c5f60`
- Recorded final verify — `VALID b9a479790f5447cd015b3b4b957c9a61c71851c77ca7b9e0ff6598b2d870ff6c`

## Unverified

- Full config package suite remains blocked in this environment by the inherited-`AGW_ROOT` sensitivity of the pre-existing `TestFindRoot`.
- Full suites containing `httptest` local listeners are blocked by sandbox networking restrictions.
- No runtime behavior was intentionally changed, so no external provider integration was run.

## Residual risk

- Documentation contract tests use semantic anchors rather than parsing the entire Markdown structure; unrelated wording changes remain possible and must be caught by review.
