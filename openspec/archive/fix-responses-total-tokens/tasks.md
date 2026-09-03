# tasks · fix-responses-total-tokens

## 1. 准备

- [x] 1.1 建分支：`git checkout -b feature/fix-responses-total-tokens`（基于 main 3ddca85；default.toml 注释修正已先行独立提交）

## 2. TDD 实现

- [x] 2.1 红：`openairesponses_test.go` 非流式断言 usage 含 `total_tokens=18`、流式 wants 含 `"total_tokens":20`；实测两处 FAIL。
- [x] 2.2 绿：`wireUsage` 增加 `TotalTokens`；`BuildResponse` 与流式 `response.completed` 填充 `Input+Output`；包测试通过。
- [x] 2.3 全量回归：`go test ./...` 与 `go vet ./...` 全部通过。

## 2b. 第二根因（验收 3.2 暴露，同族最小修复，已按 D6 纳入）

- [x] 2b.1 现象：total_tokens 修复后 codex 回合完成但**回答文本为空**，`OutputTextDelta without active item` 噪音丢弃增量。对照智谱原生流定位：编码器事件 `type` 缺 `response.` 前缀（`output_item.added`/`output_item.done`/`content_part.added`/`content_part.done`），codex 按官方枚举反序列化失败静默丢弃 → item 永不激活。
- [x] 2b.2 红：`TestStreamEncoderRoundTrip` wants 改为带前缀事件名 + `TestStreamDecoderPrefixedNames`（解码侧双名兼容，原生上游带前缀）双 FAIL。
- [x] 2b.3 绿：编码器 4 组事件名补前缀；解码器 `output_item.added/done` 双名兼容；全量测试 + vet 通过。

## 3. 端到端验收（D5）

- [x] 3.1 重建部署：`go build -o ./agw ./cmd/agw`（/usr/local/bin 无写权限，`agw start` 以自身二进制拉起 serve，等价生效）；重启网关 pid=2240484。
- [x] 3.2 跨协议真实复现：preferred=yxr，codex profile model=glm-5.3（原故障场景：yxr 403→zhipu-anthropic 跨协议），`codex -p agw exec` 输出完整答案"1+1 等于 2，而我是由 Z.ai 训练的 GLM 大语言模型。"，无 `stream disconnected`，`without active item` 噪音 0 条。
- [x] 3.3 同协议回归：preferred=zhipu-responses 下 codex 正常（"OK"，tokens 5890）；claude 路径回归（/v1/messages→yxr 跨协议）正常返回 MiniMax-M3 "OK"；已切回 yxr。
- [x] 3.4 验证证据：本节命令与输出摘要 + 主会话调试记录（2026-09-03）。

## 4. 收尾

- [x] 4.1 提交（测试+实现一个提交）；状态置待验证，报告用户。
- [x] 4.2 Verify 通过后 no-ff 合并 main、Archive（verify 综合审查通过，2026-09-03）。

