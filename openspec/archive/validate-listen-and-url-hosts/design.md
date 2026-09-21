# design · validate-listen-and-url-hosts

## 决策

### D1: 配置层校验而不是各消费点容错

`config.Load()` 是 default/local/project 合并后的唯一出口。所有 CLI 与 agent 路径都先加载配置，因此在此校验可最早发现错误并避免各处发散解析。校验只验证地址结构，不解析 DNS、不探测端口，保持配置加载纯本地且快速。

### D2: 不自动修正无括号 IPv6

`::1:8787` 语义歧义，自动猜测 host 边界容易掩盖错误。保留用户输入并返回带 `[::1]:8787` 示例的结构性错误，让配置本身保持 canonical。

### D3: 共享 `BaseURL(listen)` helper

集中处理 `SplitHostPort` + `JoinHostPort`，供 admin URL、Claude settings、Codex profile 和 health URL 复用。所有调用方已持有经校验的 listen；helper 对无效输入返回 error，防止未来绕过配置校验时生成畸形 URL。

### D4: 保留原样存储 listen

配置中的 listen 不重写、不格式化，仅校验。这样 metrics/status/日志仍显示用户配置值，避免热重载写回造成不必要 diff。

## 备选方案

- 每个消费点自行拼接并容错：重复逻辑且错误时机不一致，弃用。
- 自动为无括号 IPv6 加括号：掩盖配置错误，弃用。
- 在 serve 启动时才校验：无法保护 agent 配置生成，弃用。

## 风险与边界

- hostname 或 DNS 解析仍由网络层处理，本变更只校验结构。
- `:8787` 空 host 保持允许，与原 `http.Server` 行为一致。
- URL 构造仅支持当前本地 HTTP 网关语义，不引入 TLS 配置。
