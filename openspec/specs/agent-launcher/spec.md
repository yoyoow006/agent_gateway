# agent-launcher 规格

## Purpose

定义 agw 安装与启动 Claude Code / Codex 的零接触配置策略、项目上下文注入和独立配置文件行为。

## Requirements

### Requirement: 零接触 agent 配置
agw SHALL NOT 读取、修改或备份用户默认 `~/.claude/settings.json` 与 `~/.codex/config.toml`；Claude SHALL 使用独立 settings 文件，Codex SHALL 使用独立 profile。

#### Scenario: 启动 Claude
- **WHEN** 用户执行 `agw run claude`
- **THEN** Claude 使用 `<root>/.agw/claude-settings.<project|global>.json` 叠加配置，用户默认 settings 不被修改

### Requirement: Claude settings 权限收紧
`GenerateClaudeSettings` SHALL 在写入包含虚拟令牌的 settings 文件后确保其 Unix 权限为 `0600`；既有文件权限过宽时 SHALL 收紧，无法收紧时 SHALL 返回错误。

#### Scenario: 重写已过宽的 settings 文件
- **WHEN** Claude settings 文件已存在且权限为 `0644`
- **AND** `GenerateClaudeSettings` 重写该文件
- **THEN** 文件最终权限为 `0600`

#### Scenario: 新建 settings 文件
- **WHEN** settings 文件不存在
- **AND** `GenerateClaudeSettings` 创建该文件
- **THEN** 文件权限为 `0600`
### Requirement: Agent 网关 URL Host 规范化
Claude settings 与 Codex profile SHALL 使用 `net.JoinHostPort` 重组监听 host/port 后生成本地网关 URL，确保 IPv6 地址生成合法 URL。

#### Scenario: IPv6 Claude settings
- **WHEN** `listen = "[::1]:8787"` 且生成 Claude settings
- **THEN** `ANTHROPIC_BASE_URL` 为 `http://[::1]:8787`

#### Scenario: IPv6 Codex profile
- **WHEN** `listen = "[::1]:8787"` 且生成 Codex profile
- **THEN** `base_url` 为 `http://[::1]:8787/v1`
