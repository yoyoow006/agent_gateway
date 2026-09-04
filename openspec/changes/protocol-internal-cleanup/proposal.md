# protocol-internal-cleanup

模式: 标准
状态: 待验证

## Why

跨项目对比（cc-switch Rust 实现）后识别出 agent_gateway 协议层 4 项可执行的低风险改进，零行为变化或纯防御性增强：

- **P1-1 chat 解码器 tool-call key 容错**：当前 `openaichat/stream.go:232-235` 对 `tc.Index == nil` 默认 `chatIdx=0`，与首个真实 tool 的 index 冲突，遇到不发送 `index` 的上游会丢工具调用。借鉴 cc-switch `resolve_tool_key_without_index` 启发式。
- **P2-4 错误辅助代码去重**：ParseError/BuildError/errorType/errorCode/orDefault 在三 codec 镜像实现共 11 处，纯重复。
- **P2-5 Event.ErrStatus 死字段**：`protocol.Event.ErrStatus int`（`ir.go:142`）声明但全仓零使用。
- **P4 协议流程文档**：`docs/` 仅 usage-guide.md，缺跨协议事件序列对比；`forward()` 是 70 行核心编排但无流程图。

不包含：P1-3（custom schema embed）、P2-6（ToolDef.CustomSpec）、P3-7/8/9（协议演进兼容项）——均需独立设计交互形态或超 codec 接口范围。

## What Changes

- `internal/protocol/openaichat/stream.go`：chat 流式解码器增加 tool-call index 容错启发式（无 index + 新 ID → 新键；缺 index + ID 空/重复 → 坍缩到最后键）
- `internal/protocol/openaichat/openaichat_test.go`：新增 `TestStreamDecoderToolCallMissingIndex`
- 新建 `internal/protocol/internal/errdef.go`：三协议各自等价的状态映射（`ErrorTypeOpenAIChat` / `ErrorTypeAnthropic` / `ErrorCodeResponses`；原设想的统一 MapHTTPStatusToErrorType 会改变 529/413 等取值，故按协议分表实现，零行为变化）
- 新建 `internal/protocol/internal/common.go`：`OrDefault`、`FormatErrorBody`、`ParseMessageField`
- `internal/protocol/anthropic/{codec,request}.go`、`openaichat/{codec,request}.go`、`openairesponses/{codec,request}.go`、`anthropic/stream.go`、`openaichat/stream.go`、`openairesponses/stream.go`：调用点替换
- `internal/protocol/ir.go`：删除 `Event.ErrStatus` 字段
- 新建 `docs/protocol-flow.md`：三协议 × 形态 × 角色矩阵表
- `internal/gateway/forward.go` 第 56 行注释下补 ASCII 流程图

## Impact

- 变更文件：4 codec 包 + IR + forward.go + 新建 internal/protocol/internal/ + 新建 docs/protocol-flow.md + 测试文件
- 行为影响：P1-1 容错仅在上游缺 index 时生效，正常路径零变化；P2-4 除平铺 message 容错（ParseErrorMessage 额外接受顶层 `{"message":...}`，属有意防御）外零行为变化；P2-5 零行为变化；P4 纯文档
- 测试影响：1 新测试用例；现有错误/流式测试守护 P2-4 行为等价
- 风险等级：低。P1-1 是纯防御增强，P2-4 是行为等价重构，P2-5 是死字段删除，P4 是文档

## 本地整合策略

feature 分支 `feature/protocol-internal-cleanup` 主会话直执，TDD；Verify 通过后 no-ff 合并 main（沿用仓库惯例）。