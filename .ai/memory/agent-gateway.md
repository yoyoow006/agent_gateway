# agent-gateway（agw）实现备忘

- codex 0.149.1 二进制不含 `disable_response_storage` 键（grep 0 次），且默认 `store=false` 无 `previous_response_id`——install 写入的该键是给认得它的旧版本的兼容项，非必需。
- `agw start` 判活必须用 `cmd.Wait()` 通道：serve 子进程秒退后是僵尸，`kill(pid,0)` 对僵尸返回存活，signal-0 探活会误报成功（曾致 LFC-01）。
- 熔断器注册（Registry.Upsert）必须保留既有 breaker 实例，否则热重载会复位冷却中的熔断（曾致 BRK-01）；网关侧时钟经 `func(){ return s.now() }` 动态闭包注入，测试才能事后替换。
- cobra 命令需要 `--` 透传参数时不能用 `ExactArgs`：pflag 会把 `--` 之后的参数并入 positional args（曾致 FWD-01，文档化命令直接报错）。
- Anthropic 空轮陷阱：消息 content 为空（如 thinking 被丢弃后）会产出空 text 块被上游 400，需跳过整条消息；thinking 历史块无 signature 也不能回传。
- 中转站 SSE 怪癖的缓解原则：同协议永远字节级透传（零解析），解码器对未知事件忽略不失败。

## 2026-09-03 · 来源变更 fix-responses-total-tokens
**坑**：Codex 0.149+ 对 Responses 流式输出两处严格校验，跨协议重编码路径全挂：① usage 缺 `total_tokens` 必填字段 → ResponseCompleted 反序列化失败判流断开，每次请求失败；② item/part 级事件名缺 `response.` 前缀（`output_item.added` 等）→ 事件被静默丢弃、item 永不激活，回答文本全部丢失且仅报 "without active item" 噪音。
**解**：usage 在编码边界补 `total_tokens = input + output`（流式与非流式两处）；编码器四组 item/part 事件统一 `response.` 前缀（SSE 名与 data.type 一致）；解码器对 item 事件做带/不带前缀双名兼容（原生 OpenAI/智谱流带前缀，历史黄金样例不带）。排查利器：抓网关输出流与智谱原生流逐事件 diff，事件名差异一目了然。
- 已知开放项（审查 F1，2026-09-03）：`response.failed` 载荷只含 id/status/error、无 usage（stream.go EvStreamError 分支，早于 fix-responses-total-tokens 存在）；若未来严格客户端要求 failed 也带 usage.total_tokens，需按 D2 思路补零值 usage，另立变更。

## 2026-09-03 · 来源变更 translate-additional-tools
**坑**：Codex ≥0.149 的工具编排走 input 条目 `additional_tools`（namespace 树内嵌 function/custom），此前跨协议整体丢弃（NEW-01 边界）；custom 型 `functions.exec`（V8 JS 沙箱）无 JSON schema，且 codex 0.153 无开关可退回经典形态（`features.code_mode.enabled=false` 无效）。
**解**：namespace 展平为点连名（调用名=定义名，免反查）；function 直取 schema；custom 合成 `{code:string}` schema、响应侧按请求提取的 custom 名单（`ExtractCustomTools`，无会话状态）打标 `Part.CustomTool` 还原 `custom_tool_call`（历史路径 `custom_tool_call/_output` 同步映射）。抓包利器：临时 CODEX_HOME + 本地捕获服务器伪造模型 SSE，可直接逼出 codex 的回传格式。

## 2026-09-03 · 跨项目对比方法论纠偏
**坑**：拿 cc-switch（Rust 重度优化、Codex 单客户端三向翻译）的实现模式直接对比 agent_gateway（Go 全互译 IR + codec 八方法），把"流截断必须发协议终止帧"列为 P0 缺陷——实际上 `forward.go:414-416` 注释与 `TestTranslatedStreamTruncationEmitsError`（断言不含 `message_stop`）显式锁定"截断 = 错误帧 + 不收尾"为有意设计（failover + agent 重试依赖流中断信号，STR-01 已 resolved）。
**解**：跨项目对比时，报告"对方有我们没有"前必须查 `.ai-local/reviews/` 与 OpenSpec 历史决议——本仓可能有相反的显式回归测试；不要假设对方是默认基线。判别标准：① 检查 `.ai-local/reviews/*/findings-ledger.md` 与 `delta-review.md` 是否已 resolved；② 检查仓库显式回归测试断言（如 `if strings.Contains(out, "event: message_stop") { t.Fatalf(...) }` 反向断言）；③ 检查源文件顶部注释的设计意图。下次跨项目对比报告里若再出现"P0 缺陷"，必须先给出复现证据（手工或测试）而不是仅靠"主流 SDK 可能 X"的推断。
