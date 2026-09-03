# design · translate-additional-tools

## 事实基础（2026-09-03 实测抓包，codex-cli 0.153.0）

- `additional_tools`：input 条目，role developer，`tools` 数组内为 namespace（`{type,name,description,tools[]}`）嵌套 function（`{type,name,description,parameters,strict}`）与 custom（`{type,name,description,format}`，无 schema）。
- 调用名：`<namespace>.<name>`（实测 `functions.exec` 被 codex 接受执行）。
- 往返条目：echo `custom_tool_call` `{id,call_id,name,input,status}`；结果 `custom_tool_call_output` `{id,call_id,output}`。
- custom `exec` 描述即用法说明书（V8 沙箱 + 嵌套工具目录），必须原样带给模型。
- 事件序列要求（沿用 fix-responses-total-tokens 结论）：`response.*` 前缀 + usage `total_tokens` 必填；custom_tool_call 用 added+done+completed 三事件即被接受（实测）。

## 关键决策

### D1: namespace 展平为点连名，随请求无状态携带
- **决策**：ToolDef 名直接用 `functions.exec` 形态；custom 标记加在 IR `ToolDef.Custom bool`。
- **理由**：调用名与定义名同形，上游模型回叫的名字无需反查映射表；custom 名单随每次请求的 IR 走，响应翻译可判——满足「无状态可切换」（failover 到任何供应商都不依赖会话状态）。
- **替代**：网关侧维护会话级名字映射表——被否，破坏无状态 failover 前提。

### D2: custom 型合成 `{code: string}` 单参数 schema
- **决策**：chat/anthropic 侧把 custom ToolDef 构建为 function，parameters 合成 `{"type":"object","properties":{"code":{"type":"string"}},"required":["code"],"additionalProperties":false}`，description 原样。
- **理由**：chat/anthropic 工具调用都要求 JSON 参数；code 承载原始 JS 文本是最小无损包装。响应侧见 Custom 名单即解包 `code` 还原 `custom_tool_call.input`。
- **边界**：模型若不按 schema 返回 `{"code":...}`（如裸字符串）——解包失败时把整段 arguments 文本作为 input 兜底，不 400（宽容解析，Codex 沙箱自行报错）。

### D3: 历史条目映射与 function_call 对齐
- **决策**：`custom_tool_call` 历史 → IR tool_use part（ToolInputJSON=`{"code":...}`）；`custom_tool_call_output` → IR tool result part；namespaced function_call/_output 走既有路径（名字已是点连名，天然兼容）。
- **理由**：与现有「工具往返」能力同构，改动集中在 openairesponses 解析侧。

### D4: 响应侧仅 openairesponses 需要 custom_call 特判
- **决策**：chat/anthropic 上游回来的 tool_use，仅当客户端协议为 openai-responses 且名字命中请求 IR 的 Custom 名单时，输出 `custom_tool_call`；其余维持 `function_call`。anthropic 客户端（Claude Code 不会发 additional_tools）不受影响。
- **理由**：custom 形态是 Responses 私有；最小触点。

### D5: 模型能力为披露残余，不属翻译缺陷
- **决策**：验收以黄金样例往返保真 + 真实 codex 跨协议会话发起并执行一次工具调用为准；不承诺 MiniMax-M3/glm 的编排质量。
- **理由**：翻译层职责是形态保真；模型未受训导致的低质量调用属上游能力问题，用户已知情（yxr 仅有 chat 可用是硬约束）。

## 风险与边界

- **体积**：additional_tools（含 exec 长描述）每请求随行——与现状一致（透传路径本就如此），翻译后同样单份。
- **strict schema**：chat 侧部分实现忽略 strict；合成 schema 不设 strict，避免兼容性问题。
- **未知新类型**：`additional_tools` 内未来新增第三种内嵌类型 → 该工具 NotifyDrop 告警并继续（不整体丢弃），与 v1 行为对齐但粒度 finer。
- **失败路径**：解包 custom 调用参数失败走兜底原文；additional_tools JSON 损坏 → 400（与现有无法映射块行为一致）。

## 安全验证方案

TDD 黄金样例（红→绿）：additional_tools 展开（9 function + 1 custom 断言上游 tools 形态）、custom 调用响应往返（chat tool_calls → custom_tool_call）、历史 custom_tool_call/_output → 上游消息形态；`go test ./...` 全量；端到端：网关 + yxr 真实链路，codex 发起一次真实工具调用（如 exec_command 建文件）完整执行。
