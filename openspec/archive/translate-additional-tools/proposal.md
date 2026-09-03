# translate-additional-tools

模式: 标准
状态: 已归档

## Why

NEW-01（2026-09-03 用户决策的 v1 已接受边界）：Codex ≥0.149 以 input 条目 `additional_tools`（namespace/custom 形态）携带工具编排，跨协议翻译时被整体丢弃并告警，导致 Codex 只能配 openai-responses 协议供应商走透传。用户的 yxr 中转仅支持 openai-chat 且不可改（原生 `/v1/responses` 实测 `not implemented`），Codex 经 yxr 跨协议时无工具可用，无法执行任何实际任务。

实测（2026-09-03，codex-cli 0.153.0，本地捕获服务器抓包）完整确认了线上格式，翻译在机制上完全可行：

- `additional_tools`（role: developer）→ namespace `functions` / `collaboration` → 内嵌工具
- **9/10 为标准 function 型**（有 `parameters`+`strict` JSON schema）：wait、request_user_input、followup_task、interrupt_agent、list_agents、send_message、spawn_agent、wait_agent 等——现有 IR 翻译已支持 function 型定义与调用往返
- **唯一 custom 型**：`functions.exec`（V8 JS 编排沙箱，`format` 无 schema）——需合成 function schema 并在响应/历史路径还原为 `custom_tool_call`
- 调用名格式：`<namespace>.<name>` 点连接（如 `functions.exec`），echo 条目 `custom_tool_call`，结果条目 `custom_tool_call_output`
- `features.code_mode.enabled=false` 实测不能移除该编排（0.153 仍发送），codex 侧无退出开关

## What Changes

- `internal/protocol/openairesponses/request.go`：`ParseRequest` 解析 `additional_tools`——namespace 展开为 `<ns>.<name>` 的 IR ToolDef（function 型直取 schema；custom 型标记 `Custom=true` 并由下游合成 schema）；历史条目 `custom_tool_call`/`custom_tool_call_output` 映射进 IR turns（对齐现有 function_call 处理）；移除整体丢弃与 NotifyDrop。
- `internal/protocol/ir.go`：`ToolDef` 增加 `Custom bool`（custom 型标记，编解码双侧可见，保持无状态）。
- `internal/protocol/openaichat` 与 `internal/protocol/anthropic`：Build 侧把 custom 型 ToolDef 合成为单参数 function 定义（`{code: string}`，description 原样携带）；解析侧对 custom 名字的 tool 调用按 `{code}` 包装/解包往返。
- `internal/protocol/openairesponses` 响应侧（stream.go/request.go 构建路径）：IR tool_use 命中 `Custom` 名单时输出 `custom_tool_call`（input 为原始文本）而非 `function_call`；流式事件 `output_item.added/done` 携带 custom_tool_call item。
- 黄金样例：以本次抓包的真实载荷（additional_tools 结构、custom_tool_call 往返、call 名格式）构造测试。
- 规格改写：`protocol-translation`「无状态可切换」的 "Codex 工具编排形态（已接受边界）" Scenario 更新为翻译支持；「请求语义映射」补 additional_tools 映射。

## Impact

- 变更文件：`internal/protocol/`（ir、openairesponses、openaichat、anthropic）+ 各自测试。
- 行为影响：跨协议链路 Codex 工具定义不再丢弃；`/v1/responses` 同协议透传零变化。
- 已知残余（模型侧，非翻译职责）：MiniMax-M3/glm 未针对该编排训练，模型能否用好 `functions.exec` 取决于其 function calling 能力；翻译正确性以黄金样例与往返保真锁定，模型行为作为残余风险披露。
- 真正无法映射的形态（未来新增的非 function/custom 类型）继续 NotifyDrop 显式告警。
- 本地整合策略：feature 分支 `feature/translate-additional-tools` 主会话直执，TDD；Verify 通过后 no-ff 合并 main（沿用仓库惯例）。
