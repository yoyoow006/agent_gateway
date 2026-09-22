# OpenSpec Archive Index

- `fix-install-root-and-project-rollback` — 修正安装 base_url 示例、显式网关根校验与项目创建前置配置校验 — 标准
- `document-runtime-routing-semantics` — 文档对齐项目供应商顺序、热重载超时边界与精确 failover 状态码 — 标准
- `default-anthropic-version-passthrough` — 同协议 Anthropic 透传补默认版本头 — 标准
- `tighten-claude-settings-permissions` — 重写 Claude settings 时收紧 0600 权限 — 标准
- `harden-admin-method-and-upstream-auth` — 管理端点方法限制与上游认证头不可覆盖 — 严格
- `localize-client-parse-errors` — 客户端解析错误本地 400 且不污染供应商指标 — 严格
- `project-token-save-rollback` — 项目 token 保存与工件创建失败回滚 — 严格
- `atomic-local-config-writes` — local.toml 同目录临时文件与原子替换 — 标准
- `gateway-process-identity-and-readiness` — 网关 PID 身份校验与 healthz 就绪判定 — 严格
- `validate-listen-and-url-hosts` — 监听地址校验与 IPv6 URL Host 统一 — 标准
- `provider-kv-and-config-permissions` — provider 键值校验与 local 配置最终权限验证 — 标准
- `repair-required-workflow-baseline` — 修复 README 语义锚点与 installer 测试分类，恢复 required 门禁 — 严格
