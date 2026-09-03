# agent-launcher 规格（delta）

## MODIFIED Requirements

### Requirement: 一键安装 Claude Code
`agw install claude` SHALL 检测 node/npm 前置（缺失时给出安装指引并中止），通过 `npm i -g @anthropic-ai/claude-code` 安装，并预生成网关侧独立配置 `<网关根>/.agw/claude-settings.<项目|global>.json` 所需目录；**不得读取或写入 `~/.claude/settings.json`** 等用户默认配置。

#### Scenario: 首次安装
- **WHEN** npm 可用且未安装 claude
- **THEN** npm 安装成功，`.agw/` 目录就绪，输出 `agw run claude` 用法；用户 `~/.claude` 下无任何文件被创建或修改

#### Scenario: 用户配置零接触
- **WHEN** 安装前后对比 `~/.claude/settings.json` 的内容与修改时间
- **THEN** 完全一致（不存在则仍不存在）

### Requirement: 一键安装 Codex
`agw install codex` SHALL 通过 `npm i -g @openai/codex` 安装，并在 `$CODEX_HOME`（尊重环境变量，默认 `~/.codex`）写入**新文件** `agw.config.toml`（0600：`[model_providers.agw]` base_url 指向网关 `/v1`、`wire_api = "responses"`、`env_key = "AGW_API_KEY"`、顶层 `model_provider = "agw"` 与 `disable_response_storage = true`）；**不得读取或写入 `$CODEX_HOME/config.toml`**。

#### Scenario: 安装后可用 profile 启动
- **WHEN** 安装完成且网关运行，`AGW_API_KEY=<令牌>` 下执行 `codex -p agw`
- **THEN** Codex 请求到达网关 `/v1/responses`；用户 `config.toml` 内容与修改时间不变

#### Scenario: 重复执行幂等
- **WHEN** 再次 `agw install codex`
- **THEN** `agw.config.toml` 被重写为最新网关地址，其余文件不受影响

### Requirement: 项目上下文启动
`agw run claude|codex [--project <名>] [-- <agent 参数...>]` SHALL 解析项目（缺省按 cwd 推断），生成/确保独立配置后启动：claude 以 `--settings <网关根>/.agw/claude-settings.<项目|global>.json`（内容为该项目令牌的 env 两键，0600，每次启动重写）启动；codex 以 `-p agw`（profile 文件已确保）+ `AGW_API_KEY=<项目令牌>` 启动；进程以 `exec` 替换，`--` 后参数原样透传。

#### Scenario: 项目令牌经独立配置注入（claude）
- **WHEN** `agw run claude --project foo`
- **THEN** 实际 argv 含 `--settings <根>/.agw/claude-settings.foo.json`，该文件 env 含 foo 令牌与网关地址，进程在 `projects/foo` 内运行

#### Scenario: codex profile 启动
- **WHEN** `agw run codex --project foo`
- **THEN** 实际 argv 含 `-p agw`，环境含 `AGW_API_KEY=<foo 令牌>`，`$CODEX_HOME/agw.config.toml` 存在且未触碰 `config.toml`

#### Scenario: 参数透传不受影响
- **WHEN** `agw run codex -- --model gpt-5.2`
- **THEN** `--model gpt-5.2` 原样传给 codex，`-p agw` 在前

#### Scenario: 用户默认配置全程零接触
- **WHEN** 任一 install/run 流程完成后
- **THEN** `~/.claude/settings.json` 与 `$CODEX_HOME/config.toml` 未被 agw 打开写入
