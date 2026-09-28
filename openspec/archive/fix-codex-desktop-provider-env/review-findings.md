# Review And Verification Evidence

## Task-Level Authentication Review

- Manifest: `3ad16082e6896e6d3dc868d600d8c480635b278d241ab9c596b7b2cb476d5989`
- Comparison base: worktree `HEAD` (`55c3ed4`) to the frozen working tree.
- Scope: `internal/agent/desktop_home.go`, `internal/agent/run.go`, desktop tests, and desktop credential documentation.
- Result: no Critical or Important finding.
- Checks: provider references `AGW_API_KEY` without storing the token; selected project/global token enters only the desktop child override map; sorted overrides are appended after inherited environment entries so the selected token wins; existing isolated-home, default-home protection, auth-file, backup, and reset boundaries remain unchanged.
- unverified（未验证范围）: live Codex Desktop smoke is deferred until automated verification and user coordination because an existing desktop process is not inspected or terminated.
- residual risk（残余风险）: already-running Codex Desktop processes continue using their launch-time environment; users must restart them after token/profile changes.

## Strict Verify Reviews

### Specification Conformance

- Manifest: `1682c7a9ad5341df325501b26a50fb140e8ea8a45991d10db18a07943849957b`
- Comparison base: `main` (`bf908cd`) to feature `HEAD` (`6f4bd85`).
- Result: PASS with no Critical, Important, or Minor finding.
- Coverage: all three added scenarios and the modified managed-config requirement were observed in the exact implementation diff; project token override, no token in `config.toml`, isolated `CODEX_HOME`, and unchanged `auth.json` semantics are covered by tests. Documentation states the same contract.

### Code Quality

- Manifest: `1682c7a9ad5341df325501b26a50fb140e8ea8a45991d10db18a07943849957b`
- Comparison base: `main` (`bf908cd`) to feature `HEAD` (`6f4bd85`).
- Result: PASS with no Critical, Important, or Minor finding.
- Coverage: minimal template and launcher change; sorted override entries preserve Unix last-entry semantics without mutating the caller slice; provider auth path is testable; token does not enter argv or `config.toml`; no unrelated behavior or dependency was added. Focused race test for `internal/agent` passed.

## Final Verification Evidence

- `GOCACHE=/tmp/agw-gocache go test ./internal/agent -run 'Test.*(Desktop|CodexDesktop)' -count=1`: PASS.
- `GOCACHE=/tmp/agw-gocache go test ./internal/agent -count=1`: PASS.
- `GOCACHE=/tmp/agw-gocache go test ./cmd/... ./internal/agent/... ./internal/config/... ./internal/protocol/... ./internal/provider/... ./internal/workspace/... -race`: PASS.
- `GOCACHE=/tmp/agw-gocache go test ./... -race`: FAIL only because sandbox denies `httptest` IPv6 listen in `internal/cli` and `internal/gateway`; non-race `go test ./...` reproduces the same environment-only failures, while all changed and directly related packages pass.
- `GOCACHE=/tmp/agw-gocache go vet ./...`: PASS.
- `gofmt` on all changed Go files: applied; `git diff --check`: PASS.
- `openspec validate fix-codex-desktop-provider-env --strict --no-interactive`: PASS.
- `bash scripts/validate-workflow.sh --require-openspec`: PASS with `PASS=200 FAIL=0 SKIP=0` (seven suite-internal design skips are transparently listed and excluded from gate counts).
- unverified（未验证范围）: live desktop title-generation smoke is NOT_RUN; sandbox denies starting agw on `127.0.0.1:8787`, and no Codex Desktop process was running.
- residual risk（残余风险）: running desktop instances retain their original environment and must be restarted after this change or after profile/token changes.

## Final Archive Record

- 最终 manifest ID: `1682c7a9ad5341df325501b26a50fb140e8ea8a45991d10db18a07943849957b`（该 manifest 对 Verify 后新增的任务证据提交呈 STALE；其 VALID 结论对应下方 comparison base，不用于未经审查的归档状态。）
- comparison base: `main` `bf908cd1d6c3750a83b3e59fa97550667593af2e`
- finding 状态: Critical 0 / Important 0 / Minor 0；open 0，resolved 0，not-an-issue 0，accepted-risk 0。
- 未验证范围: 真实 Codex Desktop 标题生成 smoke NOT_RUN；沙箱禁止 agw 监听 `127.0.0.1:8787`，检查时无已运行桌面进程。
- 残余风险: 已运行桌面进程保留旧启动环境；应用本修复或切换档案/令牌后必须退出并重启桌面应用。
