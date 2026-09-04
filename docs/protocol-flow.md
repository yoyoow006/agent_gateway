# 协议翻译流程与事件对照

本文是 agw 跨协议翻译的速查参考：转发路径、三协议事件序列对照与工具调用/usage 映射。行为契约以 `openspec/specs/protocol-translation/spec.md` 为准，本文不另立规范。

## 1. 转发路径

```
agent (claude/codex)
   │  虚拟令牌（Authorization: Bearer / X-Api-Key）
   ▼
agw /v1/messages | /v1/responses | /v1/chat/completions   ← withAuth: 令牌→档案→供应商链
   │
   ├─ 同协议供应商 ──→ 字节级透传（copyStream，零解析；仅同协议重写 model 字段）
   │
   └─ 跨协议供应商 ──→ 解码→IR→构建
        客户端codec.ParseRequest(body) ──→ IR（model_map 改写）──→ 供应商codec.BuildRequest
              │                                        │
              │                                        ▼
              │                                 上游 HTTP（认证按协议注入）
              │                                        │
              ▼                                        ▼
        SSE：translateStream（解码事件→标记custom→编码事件）
        非流式：ParseResponse→IR→BuildResponse
```

要点：

- **透传优先**：客户端协议 == 供应商协议时走 `copyStream`，不做任何解析（`forward.go`）。
- **无状态切换**：每次请求自包含完整上下文，可 failover 到任意协议供应商；custom 工具名单（`ExtractCustomTools`）随请求从 body 提取，不建会话状态。
- **failover 语义**：首字节前失败（连接/超时/401/403/408/429/5xx/529）换下一家重放；流中失败回错误由 agent 自带重试。

## 2. 转换矩阵（客户端 × 供应商）

| 客户端 \ 供应商 | anthropic | openai-chat | openai-responses |
|---|---|---|---|
| **anthropic**（Claude Code） | 透传 | 翻译：IR←chat 编解码 | 翻译：IR←responses 编解码 |
| **openai-responses**（Codex） | 翻译：IR←anthropic 编解码 | 翻译：IR←chat 编解码 | 透传 |

每个 codec 六项能力：请求解析/构建、非流式响应解析/构建、SSE 流式解码/编码（`protocol.Codec` 接口）。

## 3. 流式事件对照（以 IR 事件为轴）

| IR 事件 | anthropic 客户端收到 | responses 客户端收到 | chat 上游发出/收到 |
|---|---|---|---|
| stream_start | `message_start` | `response.created` | 首个 `choices[].delta{role}` chunk |
| block_start(text) | `content_block_start{text}` | `response.output_item.added{message}` + `response.content_part.added` | —（内容随 delta） |
| block_start(thinking) | `content_block_start{thinking}` | `response.output_item.added{reasoning}` | —（reasoning 字段视上游） |
| block_start(tool_use) | `content_block_start{tool_use}` | `response.output_item.added{function_call}`（custom 型→`custom_tool_call`） | `delta.tool_calls[]{index,id,function{name}}` |
| text_delta | `content_block_delta{text_delta}` | `response.output_text.delta` | `delta.content` |
| thinking_delta | `content_block_delta{thinking_delta}` | `response.reasoning_summary_text.delta` | —（或 reasoning 内容） |
| tool_call_delta | `content_block_delta{input_json_delta}` | `response.function_call_arguments.delta`（custom 型缓冲不发） | `delta.tool_calls[].function.arguments` 分片 |
| block_stop | `content_block_stop` | `response.content_part.done` + `response.output_item.done` | — |
| stream_end | `message_delta{stop_reason,usage}` + `message_stop` | `response.completed`（usage 必含 `total_tokens`） | `finish_reason` chunk + `usage` chunk + `data: [DONE]` |
| stream_error | `event: error` | `response.failed` | HTTP 错误体 / 流中断 |

解码侧：responses 上游事件 type 带 `response.` 前缀（OpenAI/智谱原生），解码器双名兼容；chat 上游不发送 `tool_calls[].index` 时按 ID 启发式归键（P1-1 容错）。

## 4. 工具调用往返（Codex additional_tools 编排）

| 环节 | responses 侧形态 | 翻译后（chat/anthropic 上游） |
|---|---|---|
| 定义 | `additional_tools`（namespace 树内嵌 function/custom） | 展平为 `<ns>.<name>` 点连名工具；custom 合成 `{code: string}` schema |
| 调用（流中） | `custom_tool_call`（input 为原始 JS 文本） | `tool_calls`/`tool_use`，参数 `{"code": "<JS>"}` |
| 调用（function 型） | `function_call`（arguments JSON） | 同构直译 |
| 结果回传 | `custom_tool_call_output` / `function_call_output` | `role:"tool"` 消息 / `tool_result` 块 |

## 5. usage 映射

| IR | anthropic | responses | chat |
|---|---|---|---|
| Input | `input_tokens` | `input_tokens` | `prompt_tokens` |
| Output | `output_tokens` | `output_tokens` | `completion_tokens` |
| CacheRead | `cache_read_input_tokens` | —（丢弃告警） | — |
| 派生 | — | `total_tokens = input + output`（必填，Codex 严格校验） | — |
