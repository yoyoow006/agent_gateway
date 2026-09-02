# llm-api-routing 规格（delta）

## ADDED Requirements

### Requirement: 本地协议端点服务
网关 SHALL 在本地监听（默认 `127.0.0.1:8787`，可配置）并提供两组客户端端点：Anthropic 协议（`POST /v1/messages`、`POST /v1/messages/count_tokens`）与 OpenAI 协议（`POST|GET /v1/responses`、`POST /v1/chat/completions`、`GET /v1/models`）。

#### Scenario: Claude Code 请求 /v1/messages
- **WHEN** 客户端向 `/v1/messages` 发送流式 Messages 请求（含任意查询参数）
- **THEN** 网关按命中的项目档案选择供应商链，经透传或翻译转发，并将 SSE 增量即时回传客户端

#### Scenario: Codex 请求 /v1/responses
- **WHEN** 客户端向 `/v1/responses` 发送 Responses 请求（POST 创建，GET 拉取）
- **THEN** 网关按档案路由转发；GET 无请求体时同样参与故障切换

#### Scenario: 模型列表
- **WHEN** 客户端请求 `GET /v1/models`
- **THEN** 网关从当前配置的模型映射与档案返回模型列表，不依赖上游可用性

### Requirement: 虚拟令牌路由
网关 SHALL 把请求携带的 `Authorization: Bearer` 或 `x-api-key` 解析为虚拟令牌并映射到项目档案（含全局默认档案）；令牌未知名牌时按客户端协议返回对应格式的 401。

#### Scenario: 项目令牌路由
- **WHEN** 请求携带 `agw run` 注入的项目令牌
- **THEN** 使用 `projects/<名>/agw.toml` 合并后的档案（供应商优先级、模型映射）

#### Scenario: 未知令牌
- **WHEN** 令牌不在配置中
- **THEN** Anthropic 端点返回 Anthropic 错误体、OpenAI 端点返回 OpenAI 错误体，状态码 401

### Requirement: 供应商池与优先级 failover
每个档案 SHALL 维护按协议分组的启用供应商有序列表；请求按粘性首选→优先级顺序尝试；发生可重试失败（网络错误、连接/首字节超时、408/429/5xx/529）时换下一家并重放原请求；全部失败时把最后一次上游错误按客户端协议格式返回。

#### Scenario: 首供应商 529 过载
- **WHEN** 首选供应商返回 529 且第二家健康
- **THEN** 请求体重放到第二家，客户端收到第二家的成功响应，无感知切换

#### Scenario: 全部失败
- **WHEN** 链内所有供应商均失败
- **THEN** 返回最后一次上游错误（保留状态码与 Retry-After 等头），不返回网关内部错误

### Requirement: 被动熔断
每供应商 SHALL 维护熔断器：连续失败达到阈值（默认 3）进入打开并按指数退避冷却（默认 60s 起，上限 15m）；冷却后半开放行一个真实请求作探针；成功关闭熔断，失败重新打开。

#### Scenario: 熔断跳过
- **WHEN** 供应商处于打开状态且有新请求
- **THEN** 该供应商被跳过直接尝试下一家，不产生上游连接

### Requirement: 同协议透传快路径
客户端协议与供应商协议一致且该供应商无模型映射时，网关 SHALL 字节级透传请求体与响应体（仅替换认证头与 Host）；配置了模型映射时仅重写请求 JSON 的 `model` 字段，不解析其余内容。

#### Scenario: 透传保真
- **WHEN** Claude Code 经 anthropic 协议供应商转发
- **THEN** 上游收到的请求体与客户端发出的字节在 `model` 之外完全一致

### Requirement: 流式与超时
SSE 流式响应 SHALL 即时透传或转换（不整体缓冲）；每供应商可配置连接超时与首字节超时；流建立后的中断按原样终止连接，交由客户端重试。

#### Scenario: 流式低延迟
- **WHEN** 上游 SSE 按块输出
- **THEN** 客户端在每个块到达时即收到，无聚合缓冲延迟

### Requirement: 认证注入与请求改写
转发 SHALL 按目标供应商协议替换认证（Anthropic 上游：`x-api-key` + `anthropic-version`；OpenAI 系上游：`Authorization: Bearer`）；供应商配置 `headers` 时附加自定义头；配置 `model_map` 时重写 `model` 字段。

#### Scenario: 自定义头中转站
- **WHEN** 供应商配置 `headers = {"X-Title": "agw"}`
- **THEN** 上游请求携带该头，客户端原始认证头被移除

### Requirement: 请求重放缓冲
failover 重放 SHALL 依赖内存请求体缓冲；超过上限（默认 64MiB）时返回 413，不得静默截断。

#### Scenario: 超大请求体
- **WHEN** 请求体超过缓冲上限
- **THEN** 返回 413，不尝试任何上游

### Requirement: count_tokens 兜底
`/v1/messages/count_tokens` SHALL 优先转发给链内 anthropic 协议供应商；链内不存在或全部 404/405 时返回符合 Anthropic 格式的本地粗估值（`input_tokens`）。

#### Scenario: 仅 chat 协议供应商
- **WHEN** 档案只有 openai-chat 供应商且客户端请求 count_tokens
- **THEN** 返回 200 与本地估算值，Claude Code 不因该端点失败而中断
