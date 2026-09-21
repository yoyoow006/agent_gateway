# agent-launcher Delta

## ADDED Requirements

### Requirement: Agent 网关 URL Host 规范化
Claude settings 与 Codex profile SHALL 使用 `net.JoinHostPort` 重组监听 host/port 后生成本地网关 URL，确保 IPv6 地址生成合法 URL。

#### Scenario: IPv6 Claude settings
- **WHEN** `listen = "[::1]:8787"` 且生成 Claude settings
- **THEN** `ANTHROPIC_BASE_URL` 为 `http://[::1]:8787`

#### Scenario: IPv6 Codex profile
- **WHEN** `listen = "[::1]:8787"` 且生成 Codex profile
- **THEN** `base_url` 为 `http://[::1]:8787/v1`
