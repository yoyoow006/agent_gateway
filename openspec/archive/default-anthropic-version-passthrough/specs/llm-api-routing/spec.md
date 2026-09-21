# llm-api-routing Delta

## ADDED Requirements

### Requirement: Anthropic 默认版本头
当目标供应商协议为 Anthropic 时，网关 SHALL 确保转发请求携带 `Anthropic-Version`。客户端已提供该头时 SHALL 保留客户端值；客户端缺失时 SHALL 注入网关默认版本 `2023-06-01`。

#### Scenario: 同协议透传缺失版本头
- **WHEN** Anthropic 客户端向 Anthropic 供应商转发请求且未携带 `Anthropic-Version`
- **THEN** 上游请求携带 `Anthropic-Version: 2023-06-01`

#### Scenario: 客户端版本头优先
- **WHEN** Anthropic 客户端携带自定义或当前默认 `Anthropic-Version`
- **THEN** 网关保留客户端提供的值，不覆盖为网关默认值
