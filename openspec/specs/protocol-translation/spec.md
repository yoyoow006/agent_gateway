# protocol-translation 规格

## Purpose

本能力定义 anthropic / openai-chat / openai-responses 三协议经中立 IR 的互译：请求/响应/流式事件/错误映射、保真降级与无状态可切换约束。

## Requirements

### Requirement: 转换矩阵
网关 SHALL 支持任意客户端协议（anthropic、openai-responses）到任意供应商协议（anthropic、openai-chat、openai-responses）的组合；不一致时经中立中间表示（IR）翻译，一致时走透传快路径。

#### Scenario: Claude Code 使用仅 chat 协议供应商
- **WHEN** 客户端发 Anthropic `/v1/messages` 请求，命中供应商协议为 openai-chat
- **THEN** 请求翻译为 Chat Completions，上游 SSE 翻译回 Anthropic 事件序列，客户端正常收到流式回复

#### Scenario: Codex 使用仅 chat 协议供应商
- **WHEN** 客户端发 Responses 请求，命中供应商协议为 openai-chat
- **THEN** 请求翻译为 Chat Completions，响应翻译回 Responses 格式（含 SSE）

#### Scenario: Codex 使用 Anthropic 协议供应商
- **WHEN** 客户端发 Responses 请求，命中供应商协议为 anthropic
- **THEN** 请求翻译为 Messages API，响应翻译回 Responses 格式（含 SSE）

#### Scenario: Claude Code 使用仅 responses 协议供应商
- **WHEN** 客户端发 Anthropic 请求，命中供应商协议为 openai-responses
- **THEN** 请求翻译为 Responses API，响应翻译回 Anthropic 格式（含 SSE）

### Requirement: IR 编解码器
anthropic、openai-chat、openai-responses 三协议 SHALL 各提供请求解析/构建、非流式响应解析/构建、SSE 流式解码/编码六项能力；每项能力有黄金样例测试锁定输入输出映射。

#### Scenario: 往返保真
- **WHEN** 任一协议的黄金样例请求被解析进 IR 再构建回该协议
- **THEN** 语义字段（消息、工具、参数）与样例一致，未知字段不导致解析失败

### Requirement: 请求语义映射
翻译 SHALL 覆盖：system/instructions、多轮消息、文本与 base64 图片、工具定义、tool_choice、max_tokens、temperature、top_p、stop；Responses 输入条目 `additional_tools`（Codex ≥0.149 工具编排：namespace 分组、function 与 custom 内嵌形态）SHALL 展开为扁平工具定义参与翻译——namespace 展开为 `<namespace>.<name>` 调用名，function 型直取 JSON schema，custom 型合成单参数 `code` 文本 schema 并在响应与历史路径还原 custom 调用形态；`cache_control` 等仅 Anthropic 有的字段在非 Anthropic 上游被丢弃并记录警告日志；无法映射的块（音视频等 v1 非目标、未来新增未知工具类型）返回 400 明确错误或 NotifyDrop 显式告警。

#### Scenario: 工具往返
- **WHEN** Anthropic 请求含 tool_use 与 tool_result
- **THEN** 翻译到 chat 后为 `tool_calls` 与 `role:"tool"` 消息，上游返回的工具调用翻译回 `tool_use` 块

#### Scenario: 缓存控制降级
- **WHEN** 请求携带 `cache_control` 且上游协议非 anthropic
- **THEN** 该字段被移除，日志输出一条警告，请求继续

#### Scenario: additional_tools 展开翻译
- **WHEN** Codex 请求 input 含 `additional_tools`（namespace `functions` 内嵌 custom `exec` 与 function `wait`、namespace `collaboration` 内嵌 function 群），上游协议为 openai-chat 或 anthropic
- **THEN** 上游收到扁平工具定义：`functions.wait`、`collaboration.*` 为原始 schema 的 function，`functions.exec` 为合成 `{code: string}` schema 的 function（description 原样）；请求历史中不再出现 additional_tools 条目；无整体丢弃告警

#### Scenario: custom 调用往返保真
- **WHEN** 上游返回名为 `functions.exec` 的工具调用（chat `tool_calls` / anthropic `tool_use`，参数为 `{"code":"<JS 源码>"}`），客户端协议为 openai-responses
- **THEN** Codex 收到 `custom_tool_call` 条目（`name="functions.exec"`，`input` 为原始 JS 文本）；Codex 回传的 `custom_tool_call_output` 在后续请求翻译中映射为上游 tool 结果消息

### Requirement: 响应与流式事件映射
非流式与流式响应 SHALL 映射内容块、结束原因（`end_turn/turn_stop/stop/completed`、`tool_use/tool_calls/function_call`、`max_tokens/length`）与 usage（输入/输出/缓存读写 token 对应字段）；输出为 Responses 格式时（跨协议重编码路径），usage SHALL 同时包含 `total_tokens` 且恒等于 `input_tokens + output_tokens`（流式 `response.completed` 事件与非流式响应体一致），满足将该字段视为必填的严格客户端（如 Codex 0.149+）完整解析；流式转换按目标协议合成合法事件序列（Anthropic：`message_start`、`content_block_*`、`message_delta`、`message_stop`、心跳；Responses：`response.created`、`response.output_item.added`、`response.output_text.delta`、`response.output_item.done`、`response.completed` 等，事件 SSE 名与 data.type 一致采用官方 `response.*` 前缀形态），并保持工具调用参数增量正确拼接；Responses 流式解码 SHALL 同时接受带与不带 `response.` 前缀的 item 事件（原生上游与历史样例兼容）。

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

### Requirement: 错误映射
上游错误响应 SHALL 按客户端协议格式返回（Anthropic `{type:"error",error:{...}}` / OpenAI `{error:{...}}`），保留状态码与可映射字段（如 429 的 Retry-After）。

#### Scenario: 上游 401
- **WHEN** 翻译路径上游返回 401
- **THEN** 该供应商计入失败参与切换；全链失败时客户端收到自身协议格式的 401 错误体

### Requirement: 无状态可切换
翻译路径 SHALL 不依赖跨请求会话状态（每个请求自包含完整上下文，custom 工具名单随请求 IR 携带），使任意一次请求都可 failover 到任意协议的供应商；Codex 配置由 `agw install codex` 写入禁用响应存储，避免 `previous_response_id` 依赖。

#### Scenario: Codex 工具编排跨协议可用（边界已解除）
- **WHEN** Codex 以 `additional_tools`（namespace/custom 工具编排）携带工具且上游协议非 openai-responses
- **THEN** 工具定义经展开翻译送达上游，模型可发起 `functions.*` 调用并经翻译回传执行，Codex 跨协议链路工具可用；未来新增的无法映射工具类型仍显式告警丢弃（模型侧行为能力为披露的残余风险，非翻译缺陷）

#### Scenario: 会话中途换协议
- **WHEN** 同一会话第 N 个请求因故障从 chat 供应商切到 anthropic 供应商
- **THEN** 请求携带完整历史上下文，上游正常处理，客户端无感知
