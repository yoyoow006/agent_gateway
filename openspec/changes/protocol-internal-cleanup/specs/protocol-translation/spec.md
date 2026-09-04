# protocol-translation 规格（delta）

## MODIFIED Requirements

### Requirement: 流式事件映射
...（现有不变）

#### Scenario: 工具调用流缺 index 容错
- **WHEN** 上游为 openai-chat 协议且流式响应中工具调用的 delta 缺 `index` 字段（部分代理/中转不规范发送）
- **THEN** 解码器按 cc-switch 风格启发式处理：`index` 缺失 + 新 ID 未在已知键 → 分配新 IR 块；`index` 缺失 + ID 空或重复 → 坍缩到最后已知 IR 块；客户端收到的工具调用序列完整且 `tool_use_id` 准确