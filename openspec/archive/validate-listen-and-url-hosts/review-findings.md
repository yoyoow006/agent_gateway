# Review Findings

## Manifest

- Final ID: `c27336eb6f6de0f8b70b6adc072425d69c802f71caabae17a185ba645ef97efb`
- Comparison base: `main` @ `f26d6dffcf4c3e1e1078d5fc60805b96df7451fd`
- Scope: full diff from `main` for config validation, shared URL construction, CLI/agent consumers, docs, and tests.

## Verdict

PASS.

## Findings

无 Critical/Important/Minor finding。

## Evidence

- `config.BaseURL` centrally parses host/port with `net.SplitHostPort`, validates numeric port, and rebuilds the authority with `net.JoinHostPort`.
- `Config.validate()` rejects missing host/port, non-numeric/empty port, and ambiguous unbracketed IPv6 while allowing IPv4, bracketed IPv6, hostname, and empty host.
- No remaining consumer directly concatenates `http://` with `Gateway.Listen`.
- Consumers now call `config.BaseURL`:
  - admin URL
  - healthz readiness URL
  - Claude `ANTHROPIC_BASE_URL`
  - Codex profile `base_url`
- IPv6 tests prove:
  - Claude URL is `http://[::1]:8787`
  - Codex URL is `http://[::1]:8787/v1`
  - Admin reload URL is `http://[::1]:8787/__agw/reload`
- README and usage guide document bracketed IPv6 format, with contract tests.

## Verification

- Red:
  - `TestBaseURLNormalizesListenHost` initially failed to compile because `BaseURL` did not exist.
  - `TestValidateListenAddress` and IPv6/URL tests drove the new validation and helper behavior.
- Green:
  - `go test ./internal/config -run 'TestValidateListenAddress|TestBaseURLNormalizesListenHost' -count=1` — PASS
  - `go test ./internal/agent -run TestIPv6GatewayURLs -count=1` — PASS
  - `go test ./internal/cli -run TestAdminURLNormalizesIPv6Listen -count=1` — PASS
  - `go test ./internal/config -run TestDocumentationIPv6ListenFormat -count=1` — PASS
  - `go test ./internal/config ./internal/agent -count=1` — PASS
  - CLI targeted URL/root/process tests — PASS
- `go build ./...` — PASS
- `go vet ./...` — PASS
- `gofmt -l .` — no output
- `bash scripts/validate-workflow.sh --fast` — `PASS=199 FAIL=0 SKIP=0`
- `openspec validate validate-listen-and-url-hosts --strict --no-interactive` — valid
- `git diff --check` — PASS
- Review manifest — `VALID 7974d90a692474061121fc60098b7d9e2635a73aa998c35fc9a4086093a8f611`
- Recorded final verify — `VALID c27336eb6f6de0f8b70b6adc072425d69c802f71caabae17a185ba645ef97efb`

## Unverified

- Real IPv6 listening and HTTP requests were not exercised because the sandbox prohibits local sockets.
- Hostname DNS resolution remains delegated to the network stack and was not resolved during validation.
- Full CLI/Gateway suites remain blocked by unrelated `httptest.NewServer` listener restrictions.

## Residual risk

- `adminURL` currently returns an empty string if called with bypassed invalid configuration. Normal paths load and validate config first; the empty URL cannot be dereferenced successfully, but future callers should propagate an error if this function is broadened.
- Port range is validated as numeric but not restricted to 1–65535; OS bind still rejects invalid port semantics.
