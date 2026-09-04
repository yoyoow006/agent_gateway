# protocol-internal-cleanup · 设计

## 1. P1-1 chat 解码器 tool-call key 容错

**当前代码**（`internal/protocol/openaichat/stream.go:230-241`）：
```go
for _, tc := range delta.ToolCalls {
    chatIdx := 0                           // ← 缺陷：nil 默认 0 与首 tool 冲突
    if tc.Index != nil {
        chatIdx = *tc.Index
    }
    irIdx, seen := d.toolIR[chatIdx]
    if !seen {
        ...
        d.toolIR[chatIdx] = irIdx
        ...
    }
}
```

**目标行为**（借鉴 cc-switch `resolve_tool_key_without_index` 启发式）：

| 条件 | 键分配 |
|---|---|
| `tc.Index != nil` | `*tc.Index`（当前主路径） |
| `tc.Index == nil && tc.ID != "" && ID 未在已知键` | `d.nextIdx++` 分配新键 |
| `tc.Index == nil && tc.ID == ""` 或 ID 已在已知键 | 坍缩到最后已知 IR 块索引 |

**实现要点**：
- 维护 `d.idToChatKey map[string]int`（chat 键 → IR 块）和 `d.chatKeyToID map[int]string`（反向，便于坍缩时查找）
- `d.lastChatKey int = -1`（最后使用的 chat 键，坍缩兜底目标）
- 主循环在每次成功处理 tool delta 后更新 `d.lastChatKey`

**测试**（`internal/protocol/openaichat/openaichat_test.go`）：
- `TestStreamDecoderToolCallMissingIndexDistinctIDs`：2 tool，每个全 delta 缺 index 但 ID 唯一 → 期望 2 个独立 IR 块
- `TestStreamDecoderToolCallMissingIndexCollapsesToLast`：1 个 tool + 后续 delta 缺 index 但 ID 与首 tool 重复 → 期望合并到首 IR 块

## 2. P2-4 错误辅助代码去重

**新建 `internal/protocol/internal/errdef.go`**：
- `MapHTTPStatusToErrorType(status int) (string, string)` — 返回 `(type, code)`
  - 401 → ("authentication_error", "")
  - 403 → ("permission_error", "")
  - 404 → ("not_found_error", "")
  - 408 → ("timeout_error", "")
  - 409 → ("invalid_request_error", "")
  - 413 → ("request_too_large", "")
  - 429 → ("rate_limit_error", "")
  - 500/502/503/504/529 → ("api_error", "server_error") 类
  - 其它 → ("api_error", "")
- 统一在三 codec 中使用

**新建 `internal/protocol/internal/common.go`**：
- `OrDefault(s, def string) string` — 替换三处私有定义
- `FormatErrorBody(rootKey string, fields map[string]any) []byte` — 替换 BuildError 的 JSON marshaling
- `ParseMessageField(body []byte, rootKey, msgKey string) string` — 替换 ParseError 的字段提取

**替换点**（验证后）：
| 文件 | 函数 | 行 |
|---|---|---|
| `anthropic/codec.go` | ParseError | 50–61 |
| `anthropic/codec.go` | BuildError | 84–93 |
| `anthropic/codec.go` | errorType | 64–81 |
| `anthropic/stream.go` | orDefault | 358 |
| `openaichat/request.go` | ParseError | 410–420 |
| `openaichat/request.go` | BuildError | 423–428 |
| `openaichat/request.go` | errorType | 430–439 |
| `openaichat/stream.go` | orDefault | 400 |
| `openairesponses/request.go` | ParseError | 493–503 |
| `openairesponses/request.go` | BuildError | 506–514 |
| `openairesponses/request.go` | errorCode | 516–525 |
| `openairesponses/stream.go` | orDefault | 408 |

**重构顺序**（每步跑全测试）：
1. 建 internal 包 + helper 函数
2. 替换 anthropic → 测试
3. 替换 openaichat → 测试
4. 替换 openairesponses → 测试
5. 删除三处私有 orDefault

## 3. P2-5 Event.ErrStatus 删除

`internal/protocol/ir.go:142` 删除 `ErrStatus int` 字段。

验证：`grep -rn ErrStatus .` 应仅返回历史 commit 信息，无 `.go` 文件命中。

## 4. P4 文档

**新建 `docs/protocol-flow.md`**：

```
# 协议流矩阵

| Client | Server | Request | Response (non-SSE) | Stream (SSE) |
|---|---|---|---|---|
| anthropic | anthropic | 字节透传 + applyModelMap | 字节透传 | 字节透传 |
| anthropic | openai-chat | ParseRequest → BuildRequest | ParseResponse → BuildResponse | translateStream (IR 事件序列 → chat SSE) |
| anthropic | openai-responses | 同上 | 同上 | translateStream (IR 事件序列 → responses SSE) |
| openai-responses | anthropic | ParseRequest → BuildRequest | 同上 | translateStream (IR 事件序列 → anthropic SSE) |
| openai-responses | openai-chat | 同上 | 同上 | translateStream (IR 事件序列 → chat SSE) |
| openai-responses | openai-responses | 字节透传 + applyModelMap | 字节透传 | 字节透传 |
| openai-chat | anthropic | ParseRequest → BuildRequest | 同上 | translateStream |
| openai-chat | openai-chat | 字节透传 + applyModelMap | 字节透传 | 字节透传 |
| openai-chat | openai-responses | ParseRequest → BuildRequest | 同上 | translateStream |
```

**`internal/gateway/forward.go:56` 函数顶部注释下补**：
```go
// forward 按候选链逐家尝试；首字节前失败换下一家，全败返回最后一次错误。
//
// 流程：
//   body → ExtractCustomTools(responses only)
//        → for each provider:
//            → attempt:
//                same-proto: applyModelMap (byte-level model rewrite)
//                cross-proto: ParseRequest → BuildRequest
//            → 2xx: relaySuccess
//                  non-SSE: ParseResponse → BuildResponse
//                  SSE:     translateStream
//            → 4xx/5xx: relayError (ParseError → BuildError)
```

## 关键文件

- 详见 plan `/home/shitou/.claude/plans/curious-bouncing-horizon.md`