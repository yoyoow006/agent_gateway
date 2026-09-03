# tasks · translate-additional-tools

## 1. 准备

- [ ] 1.1 建分支 `feature/translate-additional-tools`（基于 main 03fb4c7）；提交四件套，状态置`构建中`。

## 2. TDD 实现（每任务先红后绿）

- [ ] 2.1 IR 扩展：`internal/protocol/ir.go` `ToolDef` 增加 `Custom bool`。黄金样例测试：openairesponses `ParseRequest` 消化抓包真实形态的 `additional_tools`（namespace functions/collaboration、function×9、custom exec）→ `req.Tools` 得 10 个 ToolDef，`functions.exec` 为 Custom=true，名字点连；input 中 additional_tools 条目被消费不再进 turns、无 NotifyDrop。跑 `go test ./internal/protocol/openairesponses/` 确认红。
- [ ] 2.2 实现 2.1 解析：request.go 移除 `case "additional_tools"` 丢弃分支，新增展开逻辑（namespace 递归、function 直取、custom 标记）。全绿。
- [ ] 2.3 历史条目：测试 `custom_tool_call`（input JS 文本）→ IR tool_use（ToolInputJSON 含 code）；`custom_tool_call_output` → tool result；namespaced `function_call`/`function_call_output` 复用既有路径。红→绿（同文件）。
- [ ] 2.4 chat 侧构建：openaichat BuildRequest 对 Custom ToolDef 合成 `{code:string}` schema function；测试断言上游 tools JSON 形态（含 exec description 透传）。红→绿。
- [ ] 2.5 anthropic 侧构建：同 2.4 语义（tool 定义 name/description/input_schema）。红→绿。
- [ ] 2.6 响应往返：openaichat/anthropic 解析上游 tool_use 命中 Custom 名单 → IR part 保留 custom 语义；openairesponses BuildResponse/stream encoder 输出 `custom_tool_call`（name、input=code、call_id），流式三事件 added/done/completed 形态按 fix-responses-total-tokens 结论。测试：chat tool_calls(`functions.exec`, `{"code":"return 1"}`) → SSE 含 `"type":"custom_tool_call"` 且 input 为原始文本。红→绿。
- [ ] 2.7 全量回归：`go vet ./...` 与 `go test ./...` 全绿；透传路径回归（同协议请求零变化，既有测试覆盖）。

## 3. 端到端验收

- [ ] 3.1 重建部署：`go build -o ./agw ./cmd/agw`，重启网关。
- [ ] 3.2 yxr 跨协议真实工具调用：preferred=yxr，demo 下 `codex -p agw exec "创建文件 hello.txt 内容为 hi"`（model 用已映射名），验证：请求不再出现 additional_tools 丢弃告警；模型发起工具调用并执行；文件真实创建；会话完整结束无 stream 断流。
- [ ] 3.3 回归：同协议（zhipu-responses）codex 干活不劣化；Claude Code 路径（/v1/messages→yxr）回归正常；透传链路字节级不变（既有测试）。
- [ ] 3.4 记录证据到本文件。

## 4. 收尾

- [ ] 4.1 提交（实现+测试职责单元）；状态置`待验证`，报告用户。
- [ ] 4.2 Verify 通过后 no-ff 合并 main、Archive。
