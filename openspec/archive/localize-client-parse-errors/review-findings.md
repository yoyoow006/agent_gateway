# Review Findings

## Manifests

- Build local-error invariant: `28143d2d5c22034333cb875f9f7f9a18035e5bf77ff88608ffcd56950206ddc9`
- Verify specification compliance: `28143d2d5c22034333cb875f9f7f9a18035e5bf77ff88608ffcd56950206ddc9`
- Verify code quality: `28143d2d5c22034333cb875f9f7f9a18035e5bf77ff88608ffcd56950206ddc9`
- Recorded final: `34990265ed3f38e24a40b44874b1f2b71bbcc1447ab613599b9733555ab06f54`
- Comparison base: `main` @ `83ce6222ef90abc3c40edbd79bd6b06f34fa8cb0`

## Verdict

PASS.

## Findings

无 Critical/Important/Minor finding。

## Evidence

- `localRequestError` wraps both client `ParseRequest` and target `BuildRequest` failures.
- `forward()` checks this error before any `RecordRequest` or `RecordFailure` for the current provider.
- On a local error, it writes a client-protocol 400 and returns without advancing to the next provider.
- The regression test initially failed with 502 and then exposed one request/in-flight pollution; after the fix it passed with both providers at requests=0, failures=0, in-flight=0.
- The upstream RoundTripper count remains 0 in the test, proving no network attempt occurs.
- Existing transport/upstream error paths retain request/failure recording after the local-error branch.
- README and protocol-flow document local 400 behavior and provider-metric isolation.

## Verification

- Red:
  - `go test ./internal/gateway -run TestForwardLocalParseErrorReturns400WithoutProviderMetrics -count=1`
    - Initial result: 502 instead of 400.
    - Intermediate result: one provider request/in-flight polluted.
  - Documentation contract initially had no matching semantic anchors.
- Green:
  - `go test ./internal/gateway -run TestForwardLocalParseErrorReturns400WithoutProviderMetrics -count=1 -v` — PASS
  - `go test ./internal/gateway -run 'TestForwardLocalParseErrorReturns400WithoutProviderMetrics|TestAttemptProviderHeadersCannotOverrideAuth|TestAdminEndpointsRejectWrongMethodsBeforeAuth|TestAttemptAnthropicVersionHeaders' -count=1` — PASS
  - `go test ./internal/config -run 'TestDocumentationLocalClientErrors|TestDocumentationProviderHeadersCannotOverrideAuth' -count=1` — PASS
- Full attempted suites:
  - `go test ./internal/gateway ./internal/cli -count=1` — blocked by sandbox prohibition on local `httptest.NewServer` listeners; target tests use recorder/RoundTripper and pass.
- `go build ./...` — PASS
- `go vet ./...` — PASS
- `gofmt -l .` — no output
- `bash scripts/validate-workflow.sh --fast` — `PASS=199 FAIL=0 SKIP=0`
- `openspec validate localize-client-parse-errors --strict --no-interactive` — valid
- `git diff --check` — PASS
- Manifest verify before/after each review focus — VALID for the IDs above.
- Recorded final verify — `VALID 34990265ed3f38e24a40b44874b1f2b71bbcc1447ab613599b9733555ab06f54`.

## Unverified

- No stable public target-protocol `BuildRequest` failure fixture exists in the current codecs; that branch is covered by the same local error type and handler path, but not exercised by a dedicated end-to-end test.
- Full networked gateway/CLI suites remain blocked by sandbox listener restrictions.
- Real external provider requests were not run.

## Residual risk

- For non-local errors, `RecordRequest` now occurs after `attempt()` returns rather than before it. This preserves metrics while excluding local parse failures, but it slightly changes the timing of in-flight accounting around request construction.
- Empty-body cross-protocol requests currently return a generic transport-style error rather than `localRequestError`; they were not part of the confirmed scenario and remain unchanged.
