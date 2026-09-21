# Review Findings

## Manifests

- Build signal-safety invariant: `5410b08da243c2ca86dc555c992890c93843f074e4d1d2252c960fda571882f7`
- Verify specification compliance: `5410b08da243c2ca86dc555c992890c93843f074e4d1d2252c960fda571882f7`
- Verify code quality: `5410b08da243c2ca86dc555c992890c93843f074e4d1d2252c960fda571882f7`
- Recorded final: `d19655e0e57c46d6d74103b4026e6795371a9a08230907d7f94688dcf7485d83`
- Comparison base: `main` @ `5d7d4142e7b8ec8924d8c678a2eb0103ebebda25`

## Verdict

PASS.

## Findings

无 Critical/Important/Minor finding。

## Evidence

- pidfile now stores JSON `{pid,start_time,exe,root}` with mode 0600.
- `/proc/<pid>/stat` field 22 parsing starts after the final `)` to tolerate spaces in comm.
- `/proc/<pid>/exe` is read via symlink; unavailable `/proc` data makes identity matching fail closed.
- `StopGateway` reaches `SIGTERM` only after JSON format and exact pid/start-time/exe match.
- Legacy numeric pidfiles return a clear error, are removed, and never receive a signal.
- PID mismatch returns “身份不匹配，可能已被复用”， removes pidfile, and does not signal.
- `status` and reload use `gatewayRunning`, which requires exact identity match.
- `StartGateway` polls healthz every 50ms with an injectable 5s timeout, prioritizes child exit, writes pidfile only after readiness and successful `/proc` identity capture.
- Timeout does not write pidfile and does not signal the child.
- health URL uses `net.SplitHostPort` and `net.JoinHostPort` for IPv6-safe formatting.
- README and usage guide document readiness and identity semantics, with a documentation contract test.

## Verification

- Red:
  - New identity/readiness APIs initially failed compilation as expected before implementation.
  - Initial test corrections exposed missing run directory and too-short timeout race; fixed before green.
  - Existing alive-path test initially failed after behavior change because health was not injected; updated to inject readiness and assert JSON identity.
- Green target tests:
  - `TestPidIdentityFileAndProcessIdentity`
  - `TestStopGatewayRejectsMismatchedAndLegacyPidfiles`
  - `TestStartGatewayReadyTimeoutAndExitPaths`
  - `TestStartGatewayStillDetectsImmediateExit`
  - `TestStartGatewayAlivePath`
  - All PASS.
- Regression:
  - `go test ./internal/cli -run 'TestPid|TestGateway|TestStartGateway|TestStopGateway|TestReload|TestProcess|TestLifecycle' -count=1` — PASS
  - `go test ./internal/config -run TestDocumentationProcessIdentityAndReadiness -count=1` — PASS
- `go build ./...` — PASS
- `go vet ./...` — PASS
- `gofmt -l .` — no output
- `bash scripts/validate-workflow.sh --fast` — `PASS=199 FAIL=0 SKIP=0`
- `openspec validate gateway-process-identity-and-readiness --strict --no-interactive` — valid
- `git diff --check` — PASS
- Manifest verify before/after each review focus — VALID for the IDs above.
- Recorded final verify — `VALID d19655e0e57c46d6d74103b4026e6795371a9a08230907d7f94688dcf7485d83`.

## Unverified

- Real `agw start` HTTP healthz was not exercised because this sandbox forbids local listeners; readiness behavior is covered by injected checker.
- Cross-platform `/proc` behavior is not verified; project currently prioritizes Linux and declares Windows out of scope.
- PID-reuse was represented by mismatched start-time/exe identity rather than waiting for OS PID recycling.
- Full CLI suite remains blocked by unrelated `httptest.NewServer` listener restrictions.

## Residual risk

- If `/proc` access is unavailable while a valid gateway is running, `stop/status/reload` conservatively treat it as not running; users must inspect manually.
- On readiness timeout the child is intentionally left running without a pidfile to avoid destructive action; user may need to inspect logs or terminate it manually.
- Pidfile writes are not atomic; process interruption during pidfile write can leave an invalid JSON file, which is safely rejected as a malformed pidfile rather than signaled.
