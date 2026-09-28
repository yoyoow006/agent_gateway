# 适配 Linux Codex / ChatGPT 桌面应用

模式: 严格
状态: 构建中

## Why

用户要求参考 `BigPizzaV3/CodexPlusPlus` v1.4.0 的处理方式，让 agw 支持 Codex / ChatGPT 桌面应用，并在确认轮中收窄了两个关键边界：只支持 Linux 桌面应用；不修改用户默认 `$CODEX_HOME/config.toml` / `auth.json`，改用 agw 管理的独立 Codex home。CodexPlusPlus 的可借鉴经验是：专用 launcher 在启动桌面前应用当前供应商配置，桌面应用发现按平台候选目录定位，配置与认证写入必须校验、备份、原子化并保留既有内容。本项目把这些原则转移到 Linux 独立 home，避免接触用户默认 Codex 数据。

## What Changes

- 新增 `agw run codex-desktop [--project 名] [-- 额外参数...]`：
  - 仅在 Linux 上自动发现并启动 Codex / ChatGPT 桌面应用；其它平台直接失败并说明不支持。
  - 支持显式 `AGW_CODEX_APP` 覆盖应用目录或可执行文件路径；该覆盖在所有平台可供测试与显式使用，但产品支持边界仍为 Linux 桌面应用。
  - 为子进程设置 `CODEX_HOME=<网关根>/.agw/codex-desktop`，使桌面应用读写 agw 管理的独立 `config.toml` 与 `auth.json`。
  - 在独立 home 中生成指向回环网关的 Responses provider 配置，并把当前全局/项目虚拟令牌写入独立 `auth.json` 的 `OPENAI_API_KEY`。
  - 独立配置采用同目录临时文件、校验、原子替换；首次或内容变化时先写入时间戳备份，内容不变时不改 mtime、不新增备份。
- 现有 `agw run codex` 继续使用 `$CODEX_HOME/agw.config.toml` 独立 profile 和 `AGW_API_KEY` 环境；`agw run claude` 与用户默认 Codex/Claude 配置保持零接触。
- 提供 `agw run codex-desktop --reset`：删除并重建仅 agw 管理的独立 home；该删除受路径守卫约束，绝不触碰用户默认 `~/.codex`、其祖先或其它目录。
- 更新 README 与 usage guide，说明 Linux 前置条件、独立 home 位置、官方登录缺失现象、reset、平台边界与回滚方式。

## Impact

- **目标**：Linux 用户通过一个命令启动本机 Codex / ChatGPT 桌面应用，其请求经 agw 路由到全局或项目档案，同时用户默认 Codex 配置、登录态和历史数据不被修改。
- **非目标**：
  - 不支持 macOS / Windows 桌面应用发现与启动。
  - 不安装、升级、打包或捆绑桌面应用；不修改应用 bundle、`.desktop` 文件或系统菜单。
  - 不实现 CodexPlusPlus 的界面注入、CDP 调试、插件市场、会话管理、自动更新、聚合供应商 GUI、微信远控等功能。
  - 不改动网关协议转换、failover、熔断、项目档案和 token 解析语义。
  - 不把官方 OAuth 登录态复制到独立 home；独立 home 只承载 agw 虚拟令牌和 agw 生成的 Codex 配置。
- **用户修改保护**：
  - 当前工作区存在另一未完成 `install-herdr-skill` 变更与未跟踪文件；本变更在严格隔离 worktree 中实施，避免触碰该用户修改。
  - 桌面模式不读取、不写入、不备份用户默认 `$CODEX_HOME/config.toml` / `auth.json`。
  - `.agw/codex-desktop` 属 agw 管理数据；发现未识别内容、解析失败或路径解析异常时命令失败，`--reset` 才按用户显式请求重建。
- **安全边界**：
  - 独立 `auth.json` 含虚拟令牌，权限必须为 `0600`，日志与错误输出不得泄露完整 token。
  - `.agw/codex-desktop` 必须解析为绝对路径且位于网关根内，禁止符号链接逃逸；递归删除仅允许该精确目录并需再次校验。
  - Linux 应用发现只检查有限白名单目录与文件名；不做全盘扫描、脚本解析或 shell 注入。
  - 启动桌面进程是本地外部副作用，用户执行 `agw run codex-desktop` 即为明示授权；`--reset` 是破坏性但限定于 agw 管理目录的显式授权。
- **参考事实与适配差异**：
  - CodexPlusPlus commit `27cdd21`（tag `v1.4.0`）`app_paths.rs` 在 Linux 下搜索 `/usr/lib`、`/opt`、`~/Applications`、`~/.local/share`，识别 `ChatGPT/chatgpt/Codex/codex/codex-beta` 目录及 `app` 子目录，可执行名包含 `ChatGPT/chatgpt/Codex/codex`。
  - 同项目 `launcher.rs` 在启动桌面前应用当前供应商配置；`relay_config.rs` 对配置和认证执行校验、备份、原子写入与失败恢复。
  - 差异：CodexPlusPlus 写入用户 live `$CODEX_HOME`；本变更按用户确认改为网关根内独立 home，不迁移或合并用户凭据。
- **成功标准**：
  1. Linux 解析器覆盖默认候选、显式覆盖、可执行文件归一化和缺失失败。
  2. 独立配置内容正确、权限正确、原子写入，失败时不留下半更新 pair。
  3. no-op 刷新不改 mtime；未识别内容拒绝；`--reset` 只删除目标独立 home。
  4. 现有 Claude / Codex CLI 测试全部保持通过，并新增断言用户默认配置未被触碰。
  5. 非 Linux 平台自动发现失败路径可测，真实桌面应用启动为可选手动验收。

## Confirmed Decisions

1. 独立新增 `codex-desktop`；不改变现有 `codex` CLI 语义。
2. 不修改用户默认 `$CODEX_HOME/config.toml` / `auth.json`；桌面子进程使用 `<root>/.agw/codex-desktop` 作为 `CODEX_HOME`。
3. 仅支持 Linux 桌面应用；不支持 macOS / Windows 桌面应用。
4. 在隔离 worktree `feature/adapt-codex-desktop` 实施；不推送、不合并不删除 worktree。
