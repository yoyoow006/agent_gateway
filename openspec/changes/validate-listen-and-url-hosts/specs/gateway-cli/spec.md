# gateway-cli Delta

## ADDED Requirements

### Requirement: 监听地址结构校验
配置加载 SHALL 校验 `gateway.listen` 可解析为 host/port 且 port 为非空合法数字；无效地址 SHALL 在加载阶段失败并提示 IPv6 应使用括号格式。

#### Scenario: 标准 IPv4 地址
- **WHEN** 配置 `listen = "127.0.0.1:8787"`
- **THEN** 配置加载成功

#### Scenario: 标准 IPv6 地址
- **WHEN** 配置 `listen = "[::1]:8787"`
- **THEN** 配置加载成功

#### Scenario: 无括号 IPv6
- **WHEN** 配置 `listen = "::1:8787"`
- **THEN** 配置加载失败
- **AND** 错误提示 IPv6 示例 `[::1]:8787`

#### Scenario: 缺少端口
- **WHEN** 配置 `listen = "127.0.0.1"`
- **THEN** 配置加载失败并说明缺少 host/port

### Requirement: 本地管理 URL Host 规范化
管理端点 URL SHALL 使用 `net.JoinHostPort` 重组监听 host/port，确保 IPv6 地址生成合法 URL。

#### Scenario: IPv6 admin URL
- **WHEN** `listen = "[::1]:8787"` 且构造 `/__agw/reload` URL
- **THEN** URL 为 `http://[::1]:8787/__agw/reload`
