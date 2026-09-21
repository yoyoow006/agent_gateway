# protocol-translation Delta

## ADDED Requirements

### Requirement: 客户端构造错误本地处理
当客户端请求无法解析为客户端协议 IR，或 IR 无法构建为目标供应商协议请求时，网关 SHALL 将其视为本地客户端错误，按客户端协议返回 400，并 SHALL NOT 记录供应商请求或失败、消耗熔断探针或继续尝试其他供应商。

#### Scenario: 客户端发送非法 JSON
- **WHEN** Anthropic 客户端向 openai-chat 供应商发送无法解析的请求体
- **THEN** 网关返回 Anthropic 格式 400 错误体
- **AND** 所有供应商的 requests、failures 与 in-flight 均保持为 0

#### Scenario: 目标协议无法映射请求内容
- **WHEN** 客户端请求可解析但包含目标协议无法映射的内容
- **THEN** 网关按客户端协议返回 400
- **AND** 不发起上游请求或记录供应商失败

#### Scenario: 上游错误仍参与 failover
- **WHEN** 客户端请求解析成功且上游发生传输错误或可重试 HTTP 错误
- **THEN** 既有供应商失败记录与 failover 行为保持不变
