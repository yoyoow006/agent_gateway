# Design: 添加 OpenCode 2.x CLI 启动适配

## Confirmed input

- 用户目标：把已安装的 OpenCode CLI 添加到当前 agw 项目，只需支持 OpenCode 2.x。
- OpenCode 是运行中的外部 CLI；agw 的职责是启动与模型 API 路由，不接管其 TUI/service 生命周期。
- 本机版本 `2.2.5`，npm 包名为 `@opencode/cli`。

## Key decisions

1. **使用 OpenCode 运行时 inline 配置而不是写配置文件。**
   - `OPENCODE_CONFIG_CONTENT` 是官方支持的 runtime override，优先级晚于 global/custom/project 配置，因此可保留用户非冲突配置与插件，同时确保 `provider.agw` 与认证语义对 agw 可控。
   - 该方案不产生需要回滚或权限管理的持久配置文件，最接近现有“零接触默认配置”原则。
2. **认证只在子进程环境中传递。**
   - provider JSON 写 `apiKey: "{env:AGW_API_KEY}"`，不包含 token。
   - `PrepareExec` 返回的 `AGW_API_KEY` 由现有 `appendOverrideEnv` 排序追加到继承环境之后，外部同名变量不能覆盖选定档案。
3. **选择 OpenAI-compatible provider。**
   - OpenCode 官方自定义 provider 指南推荐 `@ai-sdk/openai-compatible`；agw 已实现 `/v1/chat/completions` 入口，因此无需新增网关协议。
   - `baseURL` 经现有 `config.BaseURL(listen)` 生成，再追加 `/v1`，复用 IPv6 Host 规范化。
4. **默认声明模型占位 `agw`。**
   - OpenCode provider 需要至少一个模型才可选择；固定占位保持 CLI 契约最小。
   - 用户可通过 OpenCode 原生 `-- --model ...` 覆盖；agw `/v1/models` 的本地摘要语义不变。
5. **安装使用 `@opencode/cli@^2`。**
   - 用户明确只需 2.x；semver 约束允许 2.x 内补丁/小版本升级，避免意外安装未来 3.x。
   - 安装函数与现有 Claude/Codex 一样只做 npm 探测与安装，不写 OpenCode 配置。

## Alternatives

- **写 `<root>/.agw/opencode.json` 并设置 `OPENCODE_CONFIG`**：可持久审计，但需要文件权限、路径与陈旧 token 生命周期管理；且该来源优先级低于项目配置，不能保证项目档案切换时覆盖正确。
- **完全独立 OpenCode home/config 目录**：隔离最强，但 OpenCode 2.x 的数据/配置路径并不等价于 Codex 的单一 `CODEX_HOME`，会引入更多版本化路径假设并丢失用户插件/命令生态。
- **动态查询 `/v1/models` 生成模型清单**：对用户更便利，但把启动流程耦合到网关可用性，并需处理超时、认证与模型 schema；不适合本次最小适配。

## Risks and mitigations

- **运行时覆盖用户同名配置**：文档与命令输出明示 `OPENCODE_CONFIG_CONTENT` 是冲突键最高优先级来源；非冲突键继续合并。
- **OpenCode 2.x future minor 变更 inline/环境变量语义**：任务中包含本机 2.x smoke test；版本边界写入规格、文档与 memory。
- **OpenCode 默认后台 service 与权限模型**：agw 不修改、停止或重置该 service；文档说明用户可用 `--standalone` 等 OpenCode 原生参数选择运行方式。
- **模型占位不代表上游真实模型**：文档说明占位仅用于 OpenCode UI 选择，最终模型名仍按网关 model map/default 语义路由。
