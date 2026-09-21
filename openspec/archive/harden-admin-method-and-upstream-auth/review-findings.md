# Review Findings

## Manifests

- Build security invariant: `9406a54b4fd42b9fab1f662d6c2672044652d1edfa817c3d1c3c4a54a6ba5769`
- Verify specification compliance: `9406a54b4fd42b9fab1f662d6c2672044652d1edfa817c3d1c3c4a54a6ba5769`
- Verify code quality: `9406a54b4fd42b9fab1f662d6c2672044652d1edfa817c3d1c3c4a54a6ba5769`
- Recorded final: `2a4300f2542afa291667628219c344c36b38ca168ddd9aee66f080efd95eef5a`
- Comparison base: `main` @ `232a7e974f93338a30da7064aa31ad4f843e96c1`

## Verdict

PASS.

## Findings

无 Critical/Important/Minor finding。

## Evidence

- Delta requirement 1 / admin methods:
  - `requireMethod` runs before `s.admin`, so wrong methods cannot reach token validation or handlers.
  - Routes bind metrics to GET and reload to POST.
  - Regression test initially observed 401 for both wrong methods, proving the old behavior; after implementation it asserts 405 and the exact Allow values.
- Delta requirement 2 / auth finality:
  - `attempt()` now applies provider `headers` before protocol authentication.
  - Anthropic test proves spoofed `X-Api-Key` is replaced by the real provider key.
  - OpenAI test proves spoofed `Authorization` is replaced by the real Bearer key.
  - `X-Title` proves non-auth custom headers remain effective.
- Documentation:
  - default config, README, and usage guide all state that Authorization / X-Api-Key cannot override gateway-injected authentication.
  - Contract test failed on all three files before update and passed afterward.

## Verification

- Red:
  - `go test ./internal/gateway -run 'TestAdminEndpointsRejectWrongMethodsBeforeAuth|TestAttemptProviderHeadersCannotOverrideAuth' -count=1` — wrong methods returned 401 and spoofed auth headers remained.
  - `go test ./internal/config -run TestDocumentationProviderHeadersCannotOverrideAuth -count=1` — all three docs lacked guidance.
- Green:
  - Target gateway tests — PASS.
  - Target gateway tests combined with Anthropic-version regression — PASS.
  - Documentation contract test — PASS.
- Full attempted suites:
  - `go test ./internal/gateway ./internal/cli -count=1` — blocked by sandbox prohibition on `httptest.NewServer` local listeners (`listen tcp6 [::1]:0: operation not permitted`); target tests avoid listeners and pass.
- `go build ./...` — PASS
- `go vet ./...` — PASS
- `gofmt -l .` — no output
- `bash scripts/validate-workflow.sh --fast` — `PASS=199 FAIL=0 SKIP=0`
- `openspec validate harden-admin-method-and-upstream-auth --strict --no-interactive` — valid
- `git diff --check` — PASS
- Manifest `verify` before and after each review focus — VALID for IDs above.
- Recorded final verify — `VALID 2a4300f2542afa291667628219c344c36b38ca168ddd9aee66f080efd95eef5a`.

## Unverified

- Real cross-process `agw reload` request was not exercised because local network listeners are denied in this environment; the handler-level test covers routing behavior directly.
- Real external Anthropic/OpenAI upstream requests were not performed.
- HEAD/OPTIONS behavior is intentionally not introduced and remains unspecified.

## Residual risk

- Provider `headers` can still override other upstream headers, including protocol construction headers such as `Content-Type` or `Anthropic-Version`; the confirmed scope protects authentication only.
- Existing deployments that relied on headers spoofing authentication must move the credential to provider `api_key` / `api_key_env`; this migration is documented.
