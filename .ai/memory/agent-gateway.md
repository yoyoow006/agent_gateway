# agent_gateway


## 显式网关根与项目创建副作用

- `--root` / `AGW_ROOT` 只做 `config/` 目录存在性校验；配置内容仍由 `config.Load` 报具体解析错误。
- `workspace.New` 必须在任何项目写盘前加载配置，否则无效 TOML 会留下 `projects/<name>/agw.toml` 和 `.git` 半成品。
- OpenAI/中转站 `base_url` 示例不得携带 `/v1`：网关按协议追加 `/v1/messages`、`/v1/chat/completions` 或 `/v1/responses`。回归测试在 `internal/config/default_toml_test.go`。

## 路由与热重载文档语义

- 项目 `providers` 列表只筛选候选子集；实际顺序按全局 `priority`，同优先级按名称，`preferred` 只在健康时置顶。
- `connect_timeout_sec` / `first_byte_timeout_sec` 由按 provider 名称缓存的 HTTP client 持有，热重载不会重建，需重启网关。
- failover 精确重试清单是 401、403、408、429、500、502、503、504、529；501/505 等非清单 5xx 原样回传。

## Anthropic 版本头

- 目标供应商为 Anthropic 且客户端未携带 `Anthropic-Version` 时，网关统一注入 `2023-06-01`；客户端已有值优先，不覆盖。
