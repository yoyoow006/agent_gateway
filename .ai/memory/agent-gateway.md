# agent-gateway（agw）实现备忘

- 新版 Codex（实测 0.149.1）不再用顶层 `tools` 传工具，而是 input 条目 `additional_tools` 内嵌 `namespace`/`custom` 形态（含 exec 内置工具与 MCP 工具）；跨协议翻译只能覆盖 function 型，custom/namespace 不可映射——已由用户决策（2026-09-03）记为 v1 已接受边界：Codex 跨协议场景工具编排不可用，需配 openai-responses 协议供应商走透传。后续若要支持需立范围变更。
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
