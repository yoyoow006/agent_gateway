# tasks · protocol-internal-cleanup

## 1. 准备

- [x] 1.1 建分支 `feature/protocol-internal-cleanup`（基于 main）；提交四件套，状态置`构建中`。

## 2. P1-1 chat 解码器 tool-call key 容错（TDD）

- [x] 2.1 红：写 `TestStreamDecoderToolCallMissingIndexDistinctIDs` + `TestStreamDecoderToolCallMissingIndexCollapsesToLast`，跑 `go test ./internal/protocol/openaichat/` 确认红。
- [x] 2.2 绿：在 `streamDecoder` 加 `idToChatKey`、`chatKeyToID`、`lastChatKey` 字段；实现 `resolveToolKeyWithoutIndex` 启发式；更新主循环。跑测试确认绿。
- [x] 2.3 回归：`go test ./internal/protocol/...` 与 `go test ./internal/gateway/` 全绿。

## 3. P2-5 Event.ErrStatus 删除

- [x] 3.1 删除 `internal/protocol/ir.go:142` 的 `ErrStatus int` 字段。
- [x] 3.2 验证：`grep -rn ErrStatus .` 应无 `.go` 文件命中；`go build ./...` 通过。

## 4. P2-4 错误辅助代码去重

- [x] 4.1 新建 `internal/protocol/internal/errdef.go`（`MapHTTPStatusToErrorType`）+ `common.go`（`OrDefault`、`FormatErrorBody`、`ParseMessageField`）；写包内单测。
- [x] 4.2 替换 `internal/protocol/anthropic/{codec,stream}.go`：ParseError/BuildError/errorType/orDefault → internal 包调用。跑 `go test ./internal/protocol/anthropic/`。
- [x] 4.3 替换 `internal/protocol/openaichat/{request,stream}.go`：ParseError/BuildError/errorType/orDefault → internal 包调用。跑 `go test ./internal/protocol/openaichat/`。
- [x] 4.4 替换 `internal/protocol/openairesponses/{request,stream}.go`：ParseError/BuildError/errorCode/orDefault → internal 包调用。跑 `go test ./internal/protocol/openairesponses/`。
- [x] 4.5 删除三 codec 私有 orDefault 定义。
- [x] 4.6 全量回归：`go test -count=1 ./...` 全绿。

## 5. P4 文档

- [x] 5.1 新建 `docs/protocol-flow.md`：三协议 × 形态 × 角色矩阵表。
- [x] 5.2 `internal/gateway/forward.go` 第 56 行函数注释下补 ASCII 流程图。
- [x] 5.3 校验：`gofmt -l .` 空；`git diff --check` 空。

## 6. 收尾

- [x] 6.1 提交（按职责单元：P1-1 一个 commit，P2-5 一个 commit，P2-4 一个 commit，P4 一个 commit）；状态置`待验证`，报告用户。
- [ ] 6.2 Verify 通过后 no-ff 合并 main、Archive。
## 完成证据（2026-09-04 复核续作）

- P1-1/P2-5/P2-4 由并行会话实现（ca8c5a7/6cf978b/0cd5904），本会话逐项复核：两个 ToolCallMissingIndex 测试 PASS、`grep -rn ErrStatus --include=*.go` 零命中、私有 orDefault 零残留（internal 包接管）、`go test -count=1 ./...` 11 包全绿。
- P4 本会话完成：docs/protocol-flow.md（路径/矩阵/事件对照/工具往返/usage）、forward.go ASCII 流程图、gofmt -l 清零（含并行会话遗留的 internal 包两文件）、git diff --check 干净。
- 状态字段曾被改回"待确认计划"与已实现进度矛盾，已纠回"构建中"（四件套提交 c86174c 原始即为构建中）。
