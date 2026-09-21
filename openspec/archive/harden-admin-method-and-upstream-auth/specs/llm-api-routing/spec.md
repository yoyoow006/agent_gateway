# llm-api-routing Delta

## MODIFIED Requirements

### Requirement: 管理端点方法限制
管理端点 SHALL 限制 HTTP 方法：reload 仅接受 POST，metrics 仅接受 GET；不匹配方法 SHALL 在执行 admin token 校验或处理器逻辑前返回 405，并携带对应 `Allow` 头。

#### Scenario: GET reload 被拒绝
- **WHEN** 客户端对 `/__agw/reload` 发送 GET
- **THEN** 响应状态码为 405
- **AND** `Allow` 头为 POST
- **AND** 网关不执行热重载

#### Scenario: POST metrics 被拒绝
- **WHEN** 客户端对 `/__agw/metrics` 发送 POST
- **THEN** 响应状态码为 405
- **AND** `Allow` 头为 GET
- **AND** 网关不输出指标数据

### Requirement: 上游认证头不可覆盖
网关 SHALL 在应用供应商自定义 headers 后注入目标协议认证头；供应商自定义 headers SHALL NOT 覆盖上游 `Authorization` 或 `X-Api-Key` 认证值。

#### Scenario: 自定义 headers 试图覆盖认证
- **WHEN** Anthropic 供应商配置 `headers = {"X-Api-Key": "spoofed"}`
- **THEN** 上游请求的 `X-Api-Key` 为网关解析的供应商密钥

#### Scenario: 自定义非认证头仍生效
- **WHEN** 供应商配置 `headers = {"X-Title": "agw"}`
- **THEN** 上游请求携带 `X-Title: agw`
