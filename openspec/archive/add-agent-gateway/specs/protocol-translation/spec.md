# protocol-translation 规格（delta）

## ADDED Requirements

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
翻译 SHALL 覆盖：system/instructions、多轮消息、文本与 base64 图片、工具定义、tool_choice、max_tokens、temperature、top_p、stop；`cache_control` 等仅 Anthropic 有的字段在非 Anthropic 上游被丢弃并记录警告日志；无法映射的块（音视频等 v1 非目标）返回 400 明确错误。

#### Scenario: 工具往返
- **WHEN** Anthropic 请求含 tool_use 与 tool_result
- **THEN** 翻译到 chat 后为 `tool_calls` 与 `role:"tool"` 消息，上游返回的工具调用翻译回 `tool_use` 块

#### Scenario: 缓存控制降级
- **WHEN** 请求携带 `cache_control` 且上游协议非 anthropic
- **THEN** 该字段被移除，日志输出一条警告，请求继续

### Requirement: 响应与流式事件映射
非流式与流式响应 SHALL 映射内容块、结束原因（`end_turn/turn_stop/stop/completed`、`tool_use/tool_calls/function_call`、`max_tokens/length`）与 usage（输入/输出/缓存读写 token 对应字段）；流式转换按目标协议合成合法事件序列（Anthropic：`message_start`、`content_block_*`、`message_delta`、`message_stop`、心跳；Responses：`response.created`、`output_text.delta`、`response.completed` 等），并保持工具调用参数增量正确拼接。

#### Scenario: Anthropic 客户端事件序列合法
- **WHEN** chat 上游流式返回两个文本块与一个工具调用
- **THEN** 客户端收到 `message_start`、按序 `content_block_start/delta/stop`、含 usage 的 `message_delta`、`message_stop`，事件索引连续

#### Scenario: usage 映射
- **WHEN** chat 上游返回 `usage{prompt_tokens, completion_tokens}`
- **THEN** Anthropic 客户端收到 `usage{input_tokens, output_tokens}`；Responses 客户端收到对应 token 字段

### Requirement: 错误映射
上游错误响应 SHALL 按客户端协议格式返回（Anthropic `{type:"error",error:{...}}` / OpenAI `{error:{...}}`），保留状态码与可映射字段（如 429 的 Retry-After）。

#### Scenario: 上游 401
- **WHEN** 翻译路径上游返回 401
- **THEN** 该供应商计入失败参与切换；全链失败时客户端收到自身协议格式的 401 错误体

### Requirement: 无状态可切换
翻译路径 SHALL 不依赖跨请求会话状态（每个请求自包含完整上下文），使任意一次请求都可 failover 到任意协议的供应商；Codex 配置由 `agw install codex` 写入禁用响应存储，避免 `previous_response_id` 依赖。

#### Scenario: Codex 工具编排形态（已接受边界）
- **WHEN** Codex 以 `additional_tools`（namespace/custom 工具编排）携带工具且上游协议非 openai-responses
- **THEN** 这些工具无法映射为 function 型定义而被丢弃并记录显式告警；Codex 应配置 openai-responses 协议供应商走透传（v1 已接受边界，用户 2026-09-03 决策）

#### Scenario: 会话中途换协议
- **WHEN** 同一会话第 N 个请求因故障从 chat 供应商切到 anthropic 供应商
- **THEN** 请求携带完整历史上下文，上游正常处理，客户端无感知
