# 添加 OpenCode 2.x CLI 启动适配

模式: 严格
状态: 已归档

## Why

用户要求把本机已安装的 OpenCode 2.x CLI 加入 agw 的 agent 启动能力。当前 agw 只适配 Claude Code、Codex CLI 与 Linux Codex Desktop；OpenCode 2.x 官方支持自定义 OpenAI-compatible provider、`OPENCODE_CONFIG_CONTENT` 运行时覆盖配置，以及配置值中的 `{env:VAR}` 环境变量替换。该扩展让 OpenCode 请求经 agw 的 `/v1/chat/completions` OpenAI-compatible 入口路由，复用项目档案令牌、供应商池、协议转换、failover 与熔断。

## What Changes

- 新增 agent kind `opencode`，支持 `agw run opencode [--project 名] [-- 用户参数...]`：
  - 按现有项目/全局档案规则选择目录与虚拟令牌。
  - 子进程环境注入 `OPENCODE_CONFIG_CONTENT` 与 `AGW_API_KEY`。
  - inline 配置声明 provider `agw`（`@ai-sdk/openai-compatible`）、`baseURL=<网关>/v1`、`apiKey="{env:AGW_API_KEY}"`，并声明一个模型占位。
  - 只透传 `--` 之后的用户参数，不注入 OpenCode 交互模式 flag。
- 新增 `agw install opencode`：
  - 使用 npm 安装/升级 `@opencode/cli@^2`。
  - 不生成、读取、修改或备份用户默认 `~/.config/opencode/opencode.json`、项目 `opencode.json` 或 OpenCode 凭据存储。
- 保持 Claude、Codex CLI 与 Codex Desktop 的既有启动语义不变。
- 更新 README 与 usage guide，说明 OpenCode 2.x 前置条件、启动方式、inline 覆盖边界、模型占位语义、零接触默认配置与回滚方式。
- 更新 agent-launcher 主规格与项目知识/memory 记录。

## Impact

- **目标**：`agw run opencode` 在全局或项目上下文中启动 OpenCode 2.x，所有模型请求携带所选 agw 虚拟令牌经本地网关路由。
- **非目标**：
  - 不支持 OpenCode 1.x。
  - 不安装 OpenCode 插件、不解析或迁移 OpenCode 凭据、不改用户配置文件。
  - 不新增网关协议端点；OpenCode 按 OpenAI-compatible `/v1/chat/completions` 使用既有入口。
  - 不管理 OpenCode 后台 service 进程；由 OpenCode 自身按默认行为管理。
  - 不为 OpenCode 的内置登录、订阅或模型目录做认证代理。
- **用户修改保护**：当前 Git 工作区干净。严格实现将在默认隔离 worktree / feature 分支中进行；不触碰用户 OpenCode 全局配置、项目配置、日志与凭据。
- **安全边界**：
  - inline 配置不写 token，只引用 `AGW_API_KEY`；完整虚拟令牌仅通过子进程环境传递。
  - `OPENCODE_CONFIG_CONTENT` 作为 agw 注入的运行时最高优先级配置源，可覆盖用户/项目配置中的同名 provider 或模型设置；该行为会在命令说明与文档中明示。
  - npm 安装是本地外部副作用，只有用户执行 `agw install opencode` 时发生；本变更实现与测试仅使用注入 Runner，不真实执行 npm。
- **权威事实**：
  - 本机 `opencode --version` 为 `v2.2.5`，安装包为 `@opencode/cli@2.2.5`。
  - OpenCode 2.2.5 `--help` 显示顶层支持目录参数、`--standalone`、`--server`、`--prompt` 与用户参数透传。
  - OpenCode 官方 Config 文档说明配置合并顺序中 `OPENCODE_CONFIG_CONTENT` 是 runtime overrides，晚于 global/custom/project 配置生效；`{env:VARIABLE_NAME}` 支持环境变量替换，缺失时替换为空字符串。
  - OpenCode 官方 Providers 文档说明自定义 provider 使用 `provider.<id>.npm="@ai-sdk/openai-compatible"`、`options.baseURL` 与 `options.apiKey`；模型 ID 应匹配 `GET /v1/models` 返回。
  - agw 现有 `GET /v1/models` 是本地注册摘要；有 `model_map` 时返回映射键，否则返回 `via-<供应商名>` 占位。
- **成功标准**：
  1. `PrepareExec(root, "opencode", project, extra)` 返回正确项目目录、`AGW_API_KEY`、合法 JSON 的 `OPENCODE_CONFIG_CONTENT`、`baseURL`（含 IPv6 Host 规范化）和只追加用户 extra 的 argv。
  2. CLI 接受 `opencode` install/run kind；未知 kind 错误信息完整；现有 kinds 与 `--reset` 边界不变。
  3. `InstallOpenCode` 只执行 npm 探测与 `@opencode/cli@^2` 安装，不产生任何配置文件写入；所有既有用户默认配置快照保持不变。
  4. README、usage guide、OpenSpec 主规格与知识沉淀同步，文档准确描述边界与回滚。
  5. 全量 Go 测试、vet、格式检查、OpenSpec strict 验证与工作流门禁通过（或记录不可复现的环境限制）。

## 待确认决策

1. OpenCode 模型入口默认使用占位模型 ID `agw`（在 OpenCode 中选择 `agw/agw`）还是新增 CLI flag？
   - 证据: agw `/v1/models` 只返回本地注册摘要，且 OpenCode 自定义 provider 必须声明至少一个模型；用户本次要求是加入 CLI，不要求新增启动参数。
   - 选项: A. 固定 `agw` 占位；B. 新增 `--model` flag；C. 每次查询网关后生成动态模型清单。
   - 推荐: A，最小实现；用户仍可用 `-- --model <provider/model>` 覆盖。
   - 影响: A 不扩展 CLI 契约，但用户首次需要知道可覆盖；B/C 扩大实现与测试范围。
2. 是否保留 OpenCode 用户全局配置、项目配置与插件的正常合并？
   - 证据: OpenCode 官方配置合并语义保留非冲突键，仅 `OPENCODE_CONFIG_CONTENT` 的冲突键最终生效。
   - 选项: A. 保留用户配置，仅冲突键由 agw 覆盖；B. 完全隔离到一个新的 OpenCode home/config 目录。
   - 推荐: A，符合 Claude/Codex CLI 的叠加层策略且无需复制用户生态。
   - 影响: A 会影响 OpenCode 已声明的同名 `agw` provider/模型；B 需要更重的独立目录与迁移边界。
