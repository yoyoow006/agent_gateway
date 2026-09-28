# Design

## Context

`agw run codex` 已经验证了 Codex provider 的认证模型：`agw.config.toml` 只写 `env_key = "AGW_API_KEY"`，真实虚拟令牌由 launcher 注入子进程环境。桌面方案此前选择 `auth.json` 承载 `OPENAI_API_KEY`，但宿主 `codex-desktop 0.154.0-alpha.6.2` 的标题生成服务实际发起的结构化 `/v1/responses` 请求没有读取该文件生成 Authorization，导致 agw 返回 401。

## Decision

复用 CLI 已验证的 provider 环境变量模型：

1. 受管桌面 `config.toml` 的 `[model_providers.agw]` 增加 `env_key = "AGW_API_KEY"`。
2. `PrepareExec(KindCodexDesktop)` 在返回的 env map 中写入当前全局/项目虚拟令牌。
3. 保留 `auth.json` 的 `OPENAI_API_KEY`，因为它服务于桌面普通登录态/API key 存储，不迁移官方 OAuth；删除它可能改变桌面启动语义，且不是修复 401 的必要条件。
4. `Exec` 继续把返回 env 追加到 `os.Environ()` 尾部，因此即使启动 shell 已有同名变量，最终子进程也使用 agw 选定档案的 token。

## Alternatives Considered

- **仅提示用户手工 export `AGW_API_KEY`**：不改变 agw 代码，但无法区分全局/项目 token，且与 `agw run / agw install 自动注入` 的产品承诺冲突。
- **删除 `auth.json`**：减少一份 token 落盘，但会改变已确认桌面登录行为，可能让应用进入登录向导；超出本缺陷最小修复。
- **在 `config.toml` 内嵌 token**：避免环境变量，但把密钥写入普通配置文件，安全边界变差。
- **修改网关为桌面免认证**：破坏现有虚拟令牌认证契约，不接受。

## Security And Rollback

- `config.toml` 只包含环境变量名，不包含 token。
- token 进入桌面子进程环境是认证所必需的最小传递；不进入 argv、日志或 profile。
- `EnsureDesktopHome` 会在内容变化时按既有备份/原子替换流程更新 `config.toml`；若启动失败，可按既有 `--reset` 边界恢复受管 home。
