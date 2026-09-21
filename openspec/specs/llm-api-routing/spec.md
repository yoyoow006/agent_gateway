# llm-api-routing 规格

## Purpose

定义 agw 网关的端点服务、虚拟令牌档案路由、供应商优先级 failover、被动熔断、同协议透传、流式超时、认证注入与 count_tokens 兜底行为。

## Requirements

### Requirement: 本地协议端点服务
网关 SHALL 在本地监听并提供 Anthropic 协议端点与 OpenAI 协议端点。

#### Scenario: Claude Code 请求 Messages
- **WHEN** 客户端向 `/v1/messages` 发送流式 Messages 请求
- **THEN** 网关按命中的项目档案选择供应商链并回传 SSE 增量

### Requirement: 虚拟令牌路由
网关 SHALL 将客户端虚拟令牌映射到全局或项目档案；未知令牌按客户端协议返回 401。

#### Scenario: 项目令牌路由
- **WHEN** 请求携带 `agw run` 注入的项目令牌
- **THEN** 使用该项目档案合并后的供应商链与模型映射

### Requirement: 供应商池与优先级 failover
请求 SHALL 按档案供应商顺序尝试；网络错误、连接/首字节超时、401、403、408、429、500、502、503、504 或 529 时换下一家重放；全部失败时返回最后一次上游错误。

#### Scenario: 首供应商失败
- **WHEN** 首选供应商返回可重试状态且第二家健康
- **THEN** 请求重放到第二家，客户端收到健康供应商响应

### Requirement: 被动熔断
每供应商 SHALL 维护连续失败熔断器；达到阈值后打开并指数退避，冷却后半开放行单探针。

#### Scenario: 熔断跳过
- **WHEN** 供应商处于打开状态且有新请求
- **THEN** 该供应商被跳过且不产生上游连接

### Requirement: 同协议透传
客户端与供应商协议一致时，网关 SHALL 保持请求体透传语义，仅替换认证、Host，并按模型映射改写 `model`。

#### Scenario: 透传保真
- **WHEN** Claude Code 经 anthropic 供应商转发
- **THEN** 请求体除映射后的 `model` 外保持原语义

### Requirement: Anthropic 默认版本头
当目标供应商协议为 Anthropic 时，网关 SHALL 确保转发请求携带 `Anthropic-Version`。客户端已提供该头时 SHALL 保留客户端值；客户端缺失时 SHALL 注入网关默认版本 `2023-06-01`。

#### Scenario: 同协议透传缺失版本头
- **WHEN** Anthropic 客户端向 Anthropic 供应商转发请求且未携带 `Anthropic-Version`
- **THEN** 上游请求携带 `Anthropic-Version: 2023-06-01`

#### Scenario: 客户端版本头优先
- **WHEN** Anthropic 客户端携带自定义或当前默认 `Anthropic-Version`
- **THEN** 网关保留客户端提供的值，不覆盖为网关默认值
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
