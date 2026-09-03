# tasks · translate-additional-tools

## 1. 准备

- [x] 1.1 建分支 `feature/translate-additional-tools`（基于 main 03fb4c7）；提交四件套，状态置`构建中`。

## 2. TDD 实现（每任务先红后绿）

- [x] 2.1 IR 扩展：`internal/protocol/ir.go` `ToolDef` 增加 `Custom bool`。黄金样例测试：openairesponses `ParseRequest` 消化抓包真实形态的 `additional_tools`（namespace functions/collaboration、function×9、custom exec）→ `req.Tools` 得 10 个 ToolDef，`functions.exec` 为 Custom=true，名字点连；input 中 additional_tools 条目被消费不再进 turns、无 NotifyDrop。跑 `go test ./internal/protocol/openairesponses/` 确认红。
- [x] 2.2 实现 2.1 解析：request.go 移除 `case "additional_tools"` 丢弃分支，新增展开逻辑（namespace 递归、function 直取、custom 标记）。全绿。
- [x] 2.3 历史条目：测试 `custom_tool_call`（input JS 文本）→ IR tool_use（ToolInputJSON 含 code）；`custom_tool_call_output` → tool result；namespaced `function_call`/`function_call_output` 复用既有路径。红→绿（同文件）。
- [x] 2.4 chat 侧构建：openaichat BuildRequest 对 Custom ToolDef 合成 `{code:string}` schema function；测试断言上游 tools JSON 形态（含 exec description 透传）。红→绿。
- [x] 2.5 anthropic 侧构建：同 2.4 语义（tool 定义 name/description/input_schema）。红→绿。
- [x] 2.6 响应往返：openaichat/anthropic 解析上游 tool_use 命中 Custom 名单 → IR part 保留 custom 语义；openairesponses BuildResponse/stream encoder 输出 `custom_tool_call`（name、input=code、call_id），流式三事件 added/done/completed 形态按 fix-responses-total-tokens 结论。测试：chat tool_calls(`functions.exec`, `{"code":"return 1"}`) → SSE 含 `"type":"custom_tool_call"` 且 input 为原始文本。红→绿。
- [x] 2.7 全量回归：`go vet ./...` 与 `go test ./...` 全绿；透传路径回归（同协议请求零变化，既有测试覆盖）。

## 3. 端到端验收

- [x] 3.1 重建部署：`go build -o ./agw ./cmd/agw`，重启网关。
- [x] 3.2 yxr 跨协议真实工具调用：preferred=yxr，demo 下 `codex -p agw exec "创建文件 hello.txt 内容为 hi"`（model 用已映射名），验证：请求不再出现 additional_tools 丢弃告警；模型发起工具调用并执行；文件真实创建；会话完整结束无 stream 断流。
- [x] 3.3 回归：同协议（zhipu-responses）codex 干活不劣化；Claude Code 路径（/v1/messages→yxr）回归正常；透传链路字节级不变（既有测试）。
- [x] 3.4 记录证据到本文件（修复审查 F4）。

### 3.4 证据（2026-09-03，commit 9a90f38 之后）

**E2E 真实工具调用（3.2）**

- 网关重建：`go build -o ./agw ./cmd/agw`，新 pid 2755846，监听 127.0.0.1:8787（前实例运行 5196s 退出）。
- 真实执行：`AGW_API_KEY=$TOKEN codex -p agw -c 'model="gpt-5.6-sol"' exec "创建文件 hello.txt，内容为 hi-agw"`，codex 报「已创建 hello.txt，内容为 hi-agw」并完整收尾，无 stream 断流。
- 文件系统验证：`hello.txt` 存在，6 字节，内容 `hi-agw`（真实执行的工具调用落盘，已收尾清理）。
- 会话开销：28,026 tokens，3 次请求 / 0 失败，会话完整结束。

**回归矩阵（3.3）**

- 跨协议自定义路径：网关日志扫描 0 条 `additional_tools` drop 告警；熔断器视角 yxr 完成 3 请求 / 0 失败。
- 同协议（zhipu-responses）：codex 经 zhipu-responses 返回 `"OK"`（7,082 tokens），无劣化；之后 profile 已切回 yxr。
- Claude Code 路径：`POST /v1/messages` 跨协议转 yxr，返回 MiniMax-M3 合法 message（带 thinking 块、text `"OK"`、end_turn）。
- 字节级透传：既有测试覆盖同协议链路零字节变化。

**测试套件（2.7 + 验证）**

- `go vet ./...` 无告警；`go test -count=1 ./...` 全绿。
- 7 个新增 custom/additional 测试：`TestParseAdditionalTools`（含 9 function + 1 custom、custom 标记、点连名、扁平）、`TestCustomToolHistory`、`TestUnwrapCustomInput`（含空 code 兜底，审查 F1 红→绿）、`TestChatCodecCustomToolBuild`、`TestAnthropicCodecCustomToolBuild`、`TestResponseCustomToolCall`（含 custom_tool_call 输出且非 function_call）、`TestTranslateCustomToolCallResponsesClientToChatUpstream`（审查 F5 网关接线，0.005s 首跑即绿）。

## 4. 收尾

- [x] 4.1 提交（实现+测试职责单元）；状态置`待验证`，报告用户。
- [ ] 4.2 Verify 通过后 no-ff 合并 main、Archive。
