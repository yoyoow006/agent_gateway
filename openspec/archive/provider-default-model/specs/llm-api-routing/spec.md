# llm-api-routing 规格（delta）

## MODIFIED Requirements

### Requirement: 认证注入与请求改写
转发 SHALL 按目标供应商协议替换认证（Anthropic 上游：`x-api-key` + `anthropic-version`；OpenAI 系上游：`Authorization: Bearer`）；供应商配置 `headers` 时附加自定义头；配置 `model_map` 时重写 `model` 字段；供应商配置 `default_model` 且请求模型未命中该次生效映射表时，SHALL 将 `model` 重写为 `default_model`。

#### Scenario: 精确映射优先
- **WHEN** 供应商配置 `"claude-x" = "claude-relay"` 且 `default_model = "claude-safe"`，请求模型为 `claude-x`
- **THEN** 上游收到 `claude-relay`，不使用 `claude-safe`

#### Scenario: 未知模型回退
- **WHEN** 供应商配置 `default_model = "claude-safe"`，客户端发送映射表不存在的 `claude-new`
- **THEN** 同协议透传路径仅重写顶层 `model` 为 `claude-safe`，其余请求字节保持不变；跨协议翻译路径构建上游请求时同样使用 `claude-safe`

#### Scenario: 未配置兜底保持兼容
- **WHEN** 供应商未配置 `default_model`，请求模型不在 `model_map`
- **THEN** 网关继续按原模型透传或翻译，不改变既有行为

#### Scenario: 自定义头中转站
- **WHEN** 供应商配置 `headers = {"X-Title": "agw"}`
- **THEN** 上游请求携带该头，客户端原始认证头被移除
