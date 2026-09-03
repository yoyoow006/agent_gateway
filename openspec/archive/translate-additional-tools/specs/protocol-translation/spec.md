# protocol-translation 规格（delta）

## MODIFIED Requirements

### Requirement: 请求语义映射
翻译 SHALL 覆盖：system/instructions、多轮消息、文本与 base64 图片、工具定义、tool_choice、max_tokens、temperature、top_p、stop；Responses 输入条目 `additional_tools`（Codex ≥0.149 工具编排：namespace 分组、function 与 custom 内嵌形态）SHALL 展开为扁平工具定义参与翻译——namespace 展开为 `<namespace>.<name>` 调用名，function 型直取 JSON schema，custom 型合成单参数 `code` 文本 schema 并在响应与历史路径还原 custom 调用形态；`cache_control` 等仅 Anthropic 有的字段在非 Anthropic 上游被丢弃并记录警告日志；无法映射的块（音视频等 v1 非目标、未来新增未知工具类型）返回 400 明确错误或 NotifyDrop 显式告警。

#### Scenario: 工具往返
- **WHEN** Anthropic 请求含 tool_use 与 tool_result
- **THEN** 翻译到 chat 后为 `tool_calls` 与 `role:"tool"` 消息，上游返回的工具调用翻译回 `tool_use` 块

#### Scenario: 缓存控制降级
- **WHEN** 请求携带 `cache_control` 且上游协议非 anthropic
- **THEN** 该字段被移除，日志输出一条警告，请求继续

#### Scenario: additional_tools 展开翻译
- **WHEN** Codex 请求 input 含 `additional_tools`（namespace `functions` 内嵌 custom `exec` 与 function `wait`、namespace `collaboration` 内嵌 function 群），上游协议为 openai-chat 或 anthropic
- **THEN** 上游收到扁平工具定义：`functions.wait`、`collaboration.*` 为原始 schema 的 function，`functions.exec` 为合成 `{code: string}` schema 的 function（description 原样）；请求历史中不再出现 additional_tools 条目；无整体丢弃告警

#### Scenario: custom 调用往返保真
- **WHEN** 上游返回名为 `functions.exec` 的工具调用（chat `tool_calls` / anthropic `tool_use`，参数为 `{"code":"<JS 源码>"}`），客户端协议为 openai-responses
- **THEN** Codex 收到 `custom_tool_call` 条目（`name="functions.exec"`，`input` 为原始 JS 文本）；Codex 回传的 `custom_tool_call_output` 在后续请求翻译中映射为上游 tool 结果消息

### Requirement: 无状态可切换
翻译路径 SHALL 不依赖跨请求会话状态（每个请求自包含完整上下文，custom 工具名单随请求 IR 携带），使任意一次请求都可 failover 到任意协议的供应商；Codex 配置由 `agw install codex` 写入禁用响应存储，避免 `previous_response_id` 依赖。

#### Scenario: Codex 工具编排跨协议可用（边界已解除）
- **WHEN** Codex 以 `additional_tools`（namespace/custom 工具编排）携带工具且上游协议非 openai-responses
- **THEN** 工具定义经展开翻译送达上游，模型可发起 `functions.*` 调用并经翻译回传执行，Codex 跨协议链路工具可用；未来新增的无法映射工具类型仍显式告警丢弃（模型侧行为能力为披露的残余风险，非翻译缺陷）

#### Scenario: 会话中途换协议
- **WHEN** 同一会话第 N 个请求因故障从 chat 供应商切到 anthropic 供应商
- **THEN** 请求携带完整历史上下文，上游正常处理，客户端无感知
