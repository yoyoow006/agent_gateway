# agent-launcher Delta

## ADDED Requirements

### Requirement: OpenCode 2.x 零接触启动
agw SHALL provide `opencode` as an agent kind that starts a locally installed OpenCode 2.x CLI using OpenCode runtime configuration injection. agw SHALL NOT read, modify, back up, or create the user's default OpenCode configuration, project configuration, or credential store while preparing or launching the process.

#### Scenario: 启动全局 OpenCode 档案
- **WHEN** the user runs `agw run opencode` with a valid gateway root and global profile
- **THEN** the child runs in the gateway root with `AGW_API_KEY` set to the global virtual token
- **AND** OpenCode receives runtime provider configuration pointing to the loopback gateway
- **AND** the user's global and project OpenCode configuration files and credential store are not changed

#### Scenario: 启动项目 OpenCode 档案
- **WHEN** the user runs `agw run opencode --project demo`
- **THEN** the child working directory is `<root>/projects/demo` and `AGW_API_KEY` is the `demo` virtual token
- **AND** runtime provider configuration uses the same selected token by environment reference

#### Scenario: 用户参数透传
- **WHEN** the user runs `agw run opencode --project demo -- --model agw/custom --standalone`
- **THEN** the child argv starts with `opencode` and contains only the user-provided arguments after it
- **AND** agw does not add interactive-mode, standalone, server, or prompt flags on its own

### Requirement: OpenCode Runtime Provider Configuration
`agw run opencode` SHALL inject `OPENCODE_CONFIG_CONTENT` containing valid JSON for an OpenAI-compatible provider named `agw`, with `baseURL` derived from the configured gateway listen address using host/port normalization, authentication resolved from `{env:AGW_API_KEY}`, and at least one selectable model. The injected content SHALL NOT contain the virtual token value.

#### Scenario: OpenAI-compatible provider 注入
- **WHEN** `PrepareExec` prepares the global or project profile with `listen = 127.0.0.1:8787`
- **THEN** `OPENCODE_CONFIG_CONTENT` defines provider `agw` using `@ai-sdk/openai-compatible`
- **AND** the provider `baseURL` is `http://127.0.0.1:8787/v1`
- **AND** `options.apiKey` is `{env:AGW_API_KEY}` and no token value appears in the JSON

#### Scenario: IPv6 网关地址
- **WHEN** `listen = "[::1]:8787"` and OpenCode runtime configuration is prepared
- **THEN** the provider `baseURL` is `http://[::1]:8787/v1`

#### Scenario: 外部环境值不覆盖选定档案
- **WHEN** the launching environment already contains a different `AGW_API_KEY`
- **AND** `agw run opencode --project demo` prepares execution
- **THEN** the child `AGW_API_KEY` is set after the inherited value to the `demo` virtual token

### Requirement: OpenCode 安装边界
`agw install opencode` SHALL install the OpenCode 2.x CLI from npm under the major-version constraint `@opencode/cli@^2` and SHALL NOT write or alter any OpenCode user configuration, project configuration, or credential data.

#### Scenario: 安装 OpenCode 2.x
- **WHEN** the user runs `agw install opencode`
- **THEN** agw checks npm availability and installs `@opencode/cli@^2`
- **AND** reports that OpenCode configuration is injected only at `agw run opencode` startup

#### Scenario: npm 不可用
- **WHEN** npm cannot be executed during `agw install opencode`
- **THEN** installation fails with actionable Node.js/npm guidance
- **AND** no OpenCode configuration or credential file is created

## MODIFIED Requirements

### Requirement: 零接触 agent 配置
agw SHALL NOT 读取、修改或备份用户默认 `~/.claude/settings.json`、`~/.codex/config.toml` 或用户默认/项目 OpenCode 配置与凭据；Claude SHALL 使用独立 settings 文件，Codex SHALL 使用独立 profile，OpenCode SHALL 使用运行时 inline 配置覆盖。

#### Scenario: 启动 Claude
- **WHEN** 用户执行 `agw run claude`
- **THEN** Claude 使用 `<root>/.agw/claude-settings.<project|global>.json` 叠加配置，用户默认 settings 不被修改

#### Scenario: 启动 OpenCode
- **WHEN** 用户执行 `agw run opencode`
- **THEN** OpenCode 通过 `OPENCODE_CONFIG_CONTENT` 获得冲突键最高优先级的 agw provider 配置
- **AND** 用户默认 OpenCode 配置、项目配置与凭据存储不被修改
