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

## Claude settings 权限

- `os.WriteFile` 的 0600 mode 只在创建文件时生效；重写已存在文件不会自动收紧权限。
- 含项目令牌的 `.agw/claude-settings.*.json` 每次写入后必须 stat 并按需 chmod 0600，chmod 失败要返回错误。

## 文档引用与 Go 版本

- README、usage guide、package doc 不得引用已被清空/移动的旧 OpenSpec 归档；引用前先确认 `openspec/specs/<能力>/spec.md` 存在。
- `go.mod` 当前为 `go 1.24.11`；文档最低版本必须同步写 ≥1.24.11，避免 1.24.0 用户误判可直接构建。

## 管理方法与上游认证

- `/__agw/reload` 仅 POST，`/__agw/metrics` 仅 GET；错误方法必须在 admin token 校验前返回 405 + Allow。
- provider `headers` 先应用，协议认证 `Authorization` / `X-Api-Key` 最后注入；自定义头不能覆盖上游密钥。

## 本地客户端错误

- 客户端 codec 解析失败或目标协议构建失败必须包装为本地请求错误，在记录任何 provider 指标前按客户端协议返回 400。
- 本地解析错误不得触发上游请求、failover 或熔断；`RecordRequest` 应推迟到确认不是本地构造错误之后。

## 项目 token 与工件回滚

- `project new` 先检查同名 token，再先写 token，成功后才创建目录/模板/Git；保存失败不得产生项目副作用。
- 工件失败回滚 local.toml 时只允许 `os.Remove` 空目录；绝不能用 `RemoveAll` 清理既有同名文件/目录。
- Git init 失败保持 warning-only，不触发项目回滚。

## local.toml 原子写入

- 关键配置写盘必须使用同目录临时文件 → 写入 → fsync → close → chmod 0600 → rename，失败保留旧目标并清理 tmp。
- 不要直接 `os.WriteFile` 覆盖 `config/local.toml`；该文件承载 admin/default/project token 与供应商池。
