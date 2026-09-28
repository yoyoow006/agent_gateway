# Review And Verification Evidence

## Task-Level Authentication Review

- Manifest: `3ad16082e6896e6d3dc868d600d8c480635b278d241ab9c596b7b2cb476d5989`
- Comparison base: worktree `HEAD` (`55c3ed4`) to the frozen working tree.
- Scope: `internal/agent/desktop_home.go`, `internal/agent/run.go`, desktop tests, and desktop credential documentation.
- Result: no Critical or Important finding.
- Checks: provider references `AGW_API_KEY` without storing the token; selected project/global token enters only the desktop child override map; sorted overrides are appended after inherited environment entries so the selected token wins; existing isolated-home, default-home protection, auth-file, backup, and reset boundaries remain unchanged.
- unverified（未验证范围）: live Codex Desktop smoke is deferred until automated verification and user coordination because an existing desktop process is not inspected or terminated.
- residual risk（残余风险）: already-running Codex Desktop processes continue using their launch-time environment; users must restart them after token/profile changes.
