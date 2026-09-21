# Tasks

- [ ] 1. TDD 红阶段
  - [x] 1.1 在 `internal/agent/agent_test.go` 增加测试：预置同名 Claude settings 为 0644，重写后断言 0600。
  - [x] 1.2 运行目标测试，确认按权限残留问题失败。
- [ ] 2. 最小实现
  - [x] 2.1 在 `GenerateClaudeSettings` 写入后检查并 chmod 0600；失败返回错误。
  - [x] 2.2 运行目标测试转绿。
- [ ] 3. 验证
  - [x] 3.1 运行 `go test ./internal/agent -count=1`。
  - [x] 3.2 运行 `go test -race ./internal/agent -count=1`。
  - [x] 3.3 运行 `go build ./... && go vet ./... && gofmt -l .`。
  - [x] 3.4 运行 `bash scripts/validate-workflow.sh --fast`。
  - [x] 3.5 运行 `openspec validate tighten-claude-settings-permissions --strict --no-interactive`。
- [ ] 4. 审查与归档
  - [x] 4.1 一次全 diff 综合审查，确认不改变 settings 内容和 Codex 行为。
  - [x] 4.2 记录 findings、未验证范围和残余风险。
  - [x] 4.3 通过后合并 delta、更新知识沉淀并归档。
