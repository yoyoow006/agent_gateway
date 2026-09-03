# fix-responses-total-tokens

模式: 标准
状态: 构建中

## Why

实测（2026-09-03，codex-cli 0.149.1）发现：Codex 以 openai-responses 协议客户端身份走跨协议链路（供应商为 openai-chat 或 anthropic）时，网关重编码的流式响应在收尾事件 `response.completed` 上被 Codex 拒绝：

```
ERROR: stream disconnected before completion: failed to parse ResponseCompleted: missing field `total_tokens`
```

根因：`internal/protocol/openairesponses` 的 usage 输出只有 `input_tokens`/`output_tokens`，缺 OpenAI Responses 规范的必填字段 `total_tokens`。Codex 0.149 的反序列化将 `usage.total_tokens` 视为必填，解析失败即判定断流并重试，导致跨协议链路上 Codex **每次请求都失败**。非流式响应体（`BuildResponse`）同样缺失该字段。

同协议透传路径不受影响（字节级透传，实测 zhipu-responses 直连 Codex 正常）；Claude Code（anthropic 客户端）路径不受影响。边界隔离实验与根因定位证据见 `.run/agw.log` 及调试记录。

## What Changes

- `internal/protocol/openairesponses/request.go`：`wireUsage` 增加 `TotalTokens` 字段；`BuildResponse` 填充 `total_tokens = Input + Output`。
- `internal/protocol/openairesponses/stream.go`：流式 `response.completed` 事件的 usage 增加 `total_tokens = Input + Output`。
- 黄金样例与单元测试更新：锁定流式与非流式两处 usage 均含 `total_tokens` 且等于输入输出之和（TDD，先红后绿）。
- 不改 IR `protocol.Usage`（OpenAI 语义 total = input + output，编码边界计算即可）；不改解码逻辑与透传路径。

## Impact

- 变更文件：`internal/protocol/openairesponses/{request.go,stream.go,openairesponses_test.go}`。
- 行为影响：跨协议 responses 输出多一个字段，对宽松客户端（curl、旧版 SDK）无影响（向后兼容）；对严格客户端（Codex 0.149+）从致命错误恢复为正常。
- 风险：低——字段为纯增量，透传快路径零字节变化；黄金样例更新即锁定。
- 已知残余（不在本变更范围）：跨协议流式缺 `response.reasoning_summary_part.added/done` 事件导致 Codex 记录 `ReasoningSummaryDelta without active item` 噪音日志，实测非致命；修复后复测确认不阻塞，若需消除另立变更。
- 本地整合策略：feature 分支 `feature/fix-responses-total-tokens` 主会话直执，验证通过后 no-ff 合并 main（沿用仓库既有惯例）。
