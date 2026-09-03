# design · fix-responses-total-tokens

## 根因复述

`openairesponses` 编码器的 usage 输出（流式 `stream.go` `response.completed` 与非流式 `request.go` `BuildResponse`）只含 `input_tokens`/`output_tokens`。OpenAI Responses 规范的 usage 另有 `total_tokens` 必填字段；Codex 0.149.1 的 serde 结构体将其设为非 Option，缺失即反序列化失败 → `stream disconnected before completion` → 客户端重试 → 跨协议链路全失败。同协议透传路径字节级透传上游原始事件，天然含该字段，不受影响（实测佐证）。

## 关键决策

### D1: total_tokens 在编码边界计算，不进 IR
- **决策**：`protocol.Usage`（IR）不增加 `Total` 字段；流式与非流式编码处在输出时计算 `Input + Output`。
- **理由**：OpenAI 语义 `total_tokens = input_tokens + output_tokens`，是派生值；进 IR 则三套解码器都要负责填充，扩大改动面且引入不一致风险。编码边界计算改动最小、语义最准。
- **替代方案**：IR 加字段——被否，理由如上。

### D2: wireUsage 增加字段而非改用 map
- **决策**：`wireUsage` struct 增加 `TotalTokens int \`json:"total_tokens"\``，`BuildResponse` 显式填充。
- **理由**：保持类型化构建与既有风格；流式侧沿用 `map[string]any`（该处本就是 map 构建）。解码侧复用同一 struct 时上游的 `total_tokens` 自动可解析（当前未消费，无行为变化），为将来需要留对称性。
- **注意**：`omitempty` 不加——total 为 0 时也应输出字段（严格客户端要的是"字段存在"，值可为 0；如 Finish() 合成的空 usage）。

### D3: 流式与非流式同修
- **决策**：两处一起修，同一缺陷类、同一规格 Scenario。
- **理由**：Codex 始终走流式，但非流式是同一 contract 的另一出口；只修一半会留下下次踩坑点，且测试成本几乎相同。

### D4: `ReasoningSummaryDelta without active item` 噪音不纳入（结论已更新）
- **决策**：不补 `response.reasoning_summary_part.added/done` 事件。
- **复测结论**：该噪音的真正根因是 D6 的事件名前缀缺失（item 事件被 codex 丢弃导致 delta 无处挂载）；D6 修复后实测噪音 0 条，无需补 part 事件。原"非致命"判断有误——噪音实际伴随回答文本丢失。

### D6: 编码器事件名补 `response.` 前缀；解码器双名兼容
- **决策**：编码器 `output_item.added`/`output_item.done`/`content_part.added`/`content_part.done` 四组事件的 SSE event 名与 data.type 统一改为 `response.` 前缀形态；解码器对 `output_item.added/done` 做带前缀/无前缀双名兼容。
- **理由**：OpenAI/智谱原生事件 type 均带 `response.` 前缀，codex 按官方枚举严格反序列化，无名形态被静默丢弃 → item 不激活 → 文本增量全部丢失（验收 3.2 实测空回答）。解码侧历史黄金样例锁定的是无前缀名，双名兼容既认原生流也不破坏既有样例（加法容错，符合"解码器对未知事件忽略不失败"原则）。
- **纳入依据**：与 total_tokens 同族（跨协议 responses 输出不合规致 codex 不可用），同文件同规格场景，属已确认范围的最小修复。

### D5: 验收以真实客户端为主证据
- **决策**：单测（黄金样例红→绿）之外，必须以 codex 0.149.1 经跨协议链路（yxr / zhipu-anthropic）真实完成一次对话作为验收证据。
- **理由**：原始缺陷即真实客户端暴露；纯单测无法证明 serde 层面兼容。

## 风险与边界

- **向后兼容**：新增字段对宽松客户端无影响（多余字段被忽略）；透传路径零改动。
- **失败路径**：`Finish()` 兜底合成 `EvStreamEnd`（空 usage）时输出 `total_tokens: 0`——字段存在，严格客户端可解析。
- **不验证范围**：`input_tokens_details`/`output_tokens_details` 等可选嵌套字段未输出（Codex 未要求）；anthropic/chat 编码器的 usage 形状不在本次范围（其客户端实测正常）。

## 安全验证方案

`go test ./internal/protocol/openairesponses/ -run 'Usage|Stream|Response' -v` 红转绿；`go test ./...` 全量；重建二进制替换 `/usr/local/bin/agw` 后重启网关，`codex exec` 经 yxr 跨协议链路真实对话成功；同协议链路（zhipu-responses）回归不劣化。
