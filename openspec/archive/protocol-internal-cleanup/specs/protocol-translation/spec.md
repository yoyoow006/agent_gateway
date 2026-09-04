# protocol-translation 规格（delta）

## MODIFIED Requirements

### Requirement: 响应与流式事件映射
非流式与流式响应 SHALL 映射内容块、结束原因（`end_turn/turn_stop/stop/completed`、`tool_use/tool_calls/function_call`、`max_tokens/length`）与 usage（输入/输出/缓存读写 token 对应字段）；输出为 Responses 格式时（跨协议重编码路径），usage SHALL 同时包含 `total_tokens` 且恒等于 `input_tokens + output_tokens`（流式 `response.completed` 事件与非流式响应体一致），满足将该字段视为必填的严格客户端（如 Codex 0.149+）完整解析；流式转换按目标协议合成合法事件序列（Anthropic：`message_start`、`content_block_*`、`message_delta`、`message_stop`、心跳；Responses：`response.created`、`response.output_item.added`、`response.output_text.delta`、`response.output_item.done`、`response.completed` 等，事件 SSE 名与 data.type 一致采用官方 `response.*` 前缀形态），并保持工具调用参数增量正确拼接；Responses 流式解码 SHALL 同时接受带与不带 `response.` 前缀的 item 事件（原生上游与历史样例兼容）；openai-chat 流式解码 SHALL 对缺 `index` 的 tool_calls 增量容错——`index` 缺失且携带未知调用 ID 时分配新 IR 块，`index` 缺失且 ID 已知时复用该 ID 对应 IR 块，`index` 缺失且 ID 为空时坍缩到最后活跃 IR 块，保证工具调用序列完整且 `tool_use_id` 准确。

#### Scenario: Anthropic 客户端事件序列合法
- **WHEN** chat 上游流式返回两个文本块与一个工具调用
- **THEN** 客户端收到 `message_start`、按序 `content_block_start/delta/stop`、含 usage 的 `message_delta`、`message_stop`，事件索引连续

#### Scenario: usage 映射
- **WHEN** chat 上游返回 `usage{prompt_tokens, completion_tokens}`
- **THEN** Anthropic 客户端收到 `usage{input_tokens, output_tokens}`；Responses 客户端收到对应 token 字段

#### Scenario: Responses usage 完整性（严格客户端）
- **WHEN** 客户端协议为 openai-responses 且供应商协议非 openai-responses（跨协议重编码），流式或非流式返回
- **THEN** usage 含 `input_tokens`、`output_tokens`、`total_tokens` 三字段，且 `total_tokens = input_tokens + output_tokens`；Codex 0.149.1 完整解析 `response.completed` 不中断、不重试

#### Scenario: Responses 事件名前缀合规
- **WHEN** 跨协议重编码输出 Responses 流式事件
- **THEN** item/part 级事件的 SSE 事件名与 data.type 均为 `response.output_item.added`、`response.content_part.added`、`response.output_item.done` 等带 `response.` 前缀的官方形态；Codex 0.149.1 按事件增量追踪 item，回答文本完整送达无丢弃

#### Scenario: Responses 解码双名兼容
- **WHEN** 上游为 openai-responses 协议供应商（跨协议解码路径），事件 type 带 `response.` 前缀（OpenAI/智谱原生）
- **THEN** 解码器正确识别 item 开始/结束并产出对应 IR 事件，无前缀历史形态同样兼容

#### Scenario: 工具调用流缺 index 容错
- **WHEN** 上游为 openai-chat 协议且流式响应中工具调用的 delta 缺 `index` 字段（部分代理/中转不规范发送）
- **THEN** 解码器按启发式处理：`index` 缺失 + 新 ID → 分配新 IR 块；`index` 缺失 + ID 已知 → 复用该 ID 对应 IR 块；`index` 缺失 + ID 空 → 坍缩到最后活跃 IR 块；混合形态（首片带 index+ID、续接只带 ID）不拆块；客户端收到的工具调用序列完整且 `tool_use_id` 准确
