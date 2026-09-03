# isolated-agent-configs

模式: 标准
状态: 已归档

## Why

当前 `agw install` 会合并写入用户默认配置（`~/.claude/settings.json`、`~/.codex/config.toml`，先备份）。用户要求完全不触碰本地默认配置文件，改用独立配置文件按 CLI 参数注入：

- Claude Code 以 `--settings <文件>` 启动（实测本机 `claude --help`：`--settings` 为叠加层，用户自身 settings 继续生效，仅同名键被覆盖）。
- Codex 以 `-p <profile>` 启动（实测本机 codex 0.149.1 `--help`：`-p, --profile` 语义为 "Layer `$CODEX_HOME/<name>.config.toml` on top of the base user config"——profile 本身就是独立文件，叠加在用户 config.toml 之上）。

### 已确认的需求共识

- 不再读取/写入/备份 `~/.claude/settings.json` 与 `~/.codex/config.toml`（默认配置零接触）。
- Claude Code：`agw run claude` 时生成 `<网关根>/.agw/claude-settings.<项目|global>.json`（含 env 两个键，0600），argv 注入 `--settings <该文件>`。
- Codex：在 `$CODEX_HOME`（默认 `~/.codex`，尊重环境变量）写**新文件** `agw.config.toml`（model_providers.agw + model_provider + disable_response_storage，0600），argv 注入 `-p agw`；密钥仍走 `AGW_API_KEY` 环境变量。
- `agw install` 语义简化：npm 安装 + 预生成上述独立文件 + 打印用法；不再有用户配置合并/备份/回滚指引。
- 非目标：迁移已有用户配置内容、`--legacy` 兼容模式、修改 profile 叠加语义。

## What Changes

- `internal/agent/`：新增 `GenerateClaudeSettings`（按项目令牌生成 settings 文件）与 `EnsureCodexProfile`（幂等写 profile 文件）；`PrepareExec` 的 claude 分支改为生成文件并追加 `--settings` 参数，codex 分支确保 profile 存在并追加 `-p agw`。
- `InstallClaude/InstallCodex` 重写：npm 探测安装 + 预生成独立文件；删除 settings.json/config.toml 合并、备份与损坏拒写逻辑（不再触碰用户文件）。
- CLI `run`/`install` 接线；README（安装/回滚/安全段）与 agent-launcher 主规格同步。
- 回滚语义变化：回滚 = 删除 agw 生成的独立文件（`.agw/` 目录与 `$CODEX_HOME/agw.config.toml`），用户默认配置从未被改动。

## Impact

- 行为替换：agent-launcher 两个 Requirement 的落地方式整体切换（安装不再写用户目录既有文件；启动多两个 CLI 参数）。网关路由/翻译/failover 零改动。
- 兼容：用户裸跑 `claude`/`codex`（不带 agw 参数）行为不变；叠加层保证用户自身偏好（模型、MCP、主题等）在 agw 启动的会话内继续生效。
- 测试：重写 install 系测试（原"安全合并/备份/损坏拒写"断言随之作废），新增"用户文件零接触"反向断言（mtime/内容不变）。
- 风险：`--settings`/`-p` 的叠加优先级依赖 CLI 版本行为（已实测本机版本；README 注明版本）。
