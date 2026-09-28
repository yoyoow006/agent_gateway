# Fix Codex Desktop Provider Env

模式: 严格
状态: 已归档

## Why

Linux Codex/ChatGPT 桌面应用在请求 agw 时出现 401：

```text
Structured turn failed: unexpected status 401 Unauthorized: 未知或缺失虚拟令牌
url: http://127.0.0.1:8787/v1/responses
```

用户判断应在受管桌面 `config.toml` 的 agw provider 上设置 `env_key = "AGW_API_KEY"`。仓库事实支持该判断：

- `internal/agent/desktop_home.go` 生成的桌面 provider 目前只有 `name/base_url/wire_api`，没有 `env_key`。
- 同一实现为桌面生成的 `auth.json` 只有 `OPENAI_API_KEY`，但标题生成等桌面内置结构化请求仍未携带 agw 虚拟令牌。
- 用户 shell 当前存在 `AGW_API_KEY=<agw token>`；`internal/agent/run.go` 的桌面子进程没有主动设置该变量，实际认证依赖继承的外部环境，不可复现且泄漏面不清晰。
- CLI 方案 `internal/agent/install.go` 已在同一 provider 形态下使用 `env_key = "AGW_API_KEY"`，并由 `agw run codex` 注入变量。
- 宿主安装的 Codex 桌面内置二进制 `0.154.0-alpha.6.2` 及其日志显示该请求确实指向 agw `/v1/responses`，失败发生在桌面的标题生成服务。

## What Changes

- 修改 `internal/agent/desktop_home.go` 的受管桌面配置模板：agw Responses provider 增加 `env_key = "AGW_API_KEY"`。
- 修改 `internal/agent/run.go`：仅 `codex-desktop` 子进程环境追加 `AGW_API_KEY=<当前选择的虚拟令牌>`，同时保留 `CODEX_HOME=<root>/.agw/codex-desktop`。
- 保留 `auth.json` 仅含 `OPENAI_API_KEY` 的现有语义，不迁移或修改官方 OAuth、默认 `~/.codex`。
- 更新桌面相关测试与用户文档，明确 provider `env_key` 与继承变量是桌面认证路径的一部分。
- 不改变 token 解析、项目路由、网关认证校验和桌面应用发现逻辑。

## Impact

- **目标**：通过 `agw run codex-desktop` 启动的桌面应用（包括标题生成等结构化请求）使用与所选全局/项目档案一致的 agw 虚拟令牌。
- **非目标**：不修复其他来源的 401；不修改用户默认 Codex home；不支持 macOS/Windows；不改变 `agw run codex` CLI profile；不把 token 写入 `config.toml` 或日志。
- **用户修改保护**：当前有无关未跟踪变更 `install-herdr-skill`；本变更将在严格隔离 worktree/feature 分支实施，不触碰该变更。
- **安全边界**：`config.toml` 仍不含密钥，只引用环境变量名；`AGW_API_KEY` 会进入桌面子进程环境，但 token 本身仍不进入日志、argv 或 profile 文件。密钥环境变量对同用户进程可见是 Unix 环境传递的既有属性。
