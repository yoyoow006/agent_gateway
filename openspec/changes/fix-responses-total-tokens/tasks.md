# tasks · fix-responses-total-tokens

## 1. 准备

- [ ] 1.1 建分支：`git checkout -b feature/fix-responses-total-tokens`（基于 main f86af3d，工作区须干净）

## 2. TDD 实现

- [ ] 2.1 红：改 `internal/protocol/openairesponses/openairesponses_test.go`——流式黄金样例 `response.completed` 期望 usage 含 `"total_tokens":<input+output>`；`BuildResponse` 用例断言 JSON 含 `"total_tokens"` 且值正确。运行 `go test ./internal/protocol/openairesponses/` 确认失败（缺字段）。
- [ ] 2.2 绿：`request.go` `wireUsage` 增加 `TotalTokens int \`json:"total_tokens"\``，`BuildResponse`（:433）填充 `resp.Usage.Input + resp.Usage.Output`；`stream.go` `response.completed`（:342）usage map 增加 `"total_tokens": ev.Usage.Input + ev.Usage.Output`。重跑 2.1 命令全绿。
- [ ] 2.3 全量回归：`go test ./...` 与 `go vet ./...` 全部通过。

## 3. 端到端验收（D5）

- [ ] 3.1 重建并部署：`go build -o /usr/local/bin/agw ./cmd/agw`；`agw stop && agw start`（或等价重启）。
- [ ] 3.2 跨协议真实复现：确保粘性首选为 yxr（`agw switch yxr`），在 `projects/demo` 以 `AGW_API_KEY=<default_token> codex -p agw exec "一句话回答：1+1等于几？"` 运行——原故障场景（model=glm-5.3 透传落 yxr 403→zhipu-anthropic 跨协议）或已映射模型（gpt-5.6-sol→MiniMax-M3 走 yxr 跨协议）均可，要求：正常出答案、无 `stream disconnected` 报错。
- [ ] 3.3 同协议回归：`agw switch zhipu-responses` 后重复 3.2，行为不劣化；切回 `agw switch yxr`。
- [ ] 3.4 记录验证证据（命令与输出摘要）到本文件或 verify 记录。

## 4. 收尾

- [ ] 4.1 提交（职责单元：测试+实现一个提交）；报告用户，进入待验证。
- [ ] 4.2 Verify 通过后 no-ff 合并 main、Archive（另按 verify/archive 技能执行）。
