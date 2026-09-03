# agent-launcher 规格（delta）

## ADDED Requirements

### Requirement: 一键安装 Claude Code
`agw install claude` SHALL 检测 node/npm 前置（缺失时给出安装指引并中止），通过 `npm i -g @anthropic-ai/claude-code` 安装，然后安全合并 `~/.claude/settings.json`：先做时间戳备份，仅写自有键 `env.ANTHROPIC_BASE_URL`（指向网关）与 `env.ANTHROPIC_AUTH_TOKEN`（全局虚拟令牌），保留用户其余全部字段。

#### Scenario: 首次安装
- **WHEN** npm 可用且未安装 claude
- **THEN** 安装成功，settings.json 备份生成，自有键写入，输出回滚指引

#### Scenario: 保护用户配置
- **WHEN** settings.json 已有用户自定义键
- **THEN** 合并后用户键原样保留，仅自有两个键被更新

### Requirement: 一键安装 Codex
`agw install codex` SHALL 通过 `npm i -g @openai/codex` 安装，然后合并 `~/.codex/config.toml`（先备份）：写入 `[model_providers.agw]`（name、base_url 指向网关 `/v1`、`wire_api = "responses"`、`env_key = "AGW_API_KEY"`、禁用响应存储的等效配置）并设置 `model_provider = "agw"`。

#### Scenario: 安装后直连网关
- **WHEN** 安装完成且网关运行，设置 `AGW_API_KEY=<全局令牌>` 后运行 `codex`
- **THEN** Codex 的请求到达网关 `/v1/responses`，经档案路由到供应商

#### Scenario: 保留既有 provider
- **WHEN** config.toml 已有用户自定义 model_providers
- **THEN** 仅新增/更新 `agw` 条目与 `model_provider` 键，其余保留（备份兜底注释丢失）

### Requirement: 项目上下文启动
`agw run claude|codex [--project <名>] [-- <agent 参数...>]` SHALL 解析项目（缺省按 cwd 推断，须位于 `projects/` 下），切换到项目目录，注入项目虚拟令牌环境变量（claude：`ANTHROPIC_BASE_URL`/`ANTHROPIC_AUTH_TOKEN`；codex：`AGW_API_KEY`），并 `exec` 替换当前进程启动 agent，使信号直达、无僵尸进程。

#### Scenario: 项目令牌启动
- **WHEN** `agw run claude --project foo`
- **THEN** claude 进程在 `projects/foo` 内运行，其请求命中 foo 档案

#### Scenario: 透传 agent 参数
- **WHEN** `agw run codex -- --model gpt-5.2`
- **THEN** 参数原样传给 codex 进程

#### Scenario: 项目不存在
- **WHEN** 指定的项目目录不存在
- **THEN** 报错并列出可用项目，不启动任何进程
