# agent_gateway


## 显式网关根与项目创建副作用

- `--root` / `AGW_ROOT` 只做 `config/` 目录存在性校验；配置内容仍由 `config.Load` 报具体解析错误。
- `workspace.New` 必须在任何项目写盘前加载配置，否则无效 TOML 会留下 `projects/<name>/agw.toml` 和 `.git` 半成品。
- OpenAI/中转站 `base_url` 示例不得携带 `/v1`：网关按协议追加 `/v1/messages`、`/v1/chat/completions` 或 `/v1/responses`。回归测试在 `internal/config/default_toml_test.go`。
