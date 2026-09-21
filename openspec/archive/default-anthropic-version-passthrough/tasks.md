# Tasks

- [ ] 1. TDD 红阶段
  - [x] 1.1 在 `internal/gateway/server_test.go` 增加同协议 Anthropic 透传缺失 `Anthropic-Version` 的测试，断言上游收到 `2023-06-01`。
  - [x] 1.2 增加客户端已传版本头时保留原值的测试。
  - [x] 1.3 运行目标测试，确认缺失头场景按预期失败、保留原值场景通过。
- [ ] 2. 最小实现
  - [x] 2.1 在 `attempt()` 中为 Anthropic 供应商在版本头缺失时注入默认值，位置不得覆盖客户端已有值。
  - [x] 2.2 运行目标测试转绿。
- [ ] 3. 回归验证
  - [x] 3.1 运行 `go test ./internal/gateway -run 'TestPassthrough|TestTranslate' -count=1`（若沙箱禁止 httptest 监听，记录环境限制）。
  - [x] 3.2 运行 `go build ./... && go vet ./... && gofmt -l .`。
  - [x] 3.3 运行 `bash scripts/validate-workflow.sh --fast`。
  - [x] 3.4 运行 `openspec validate default-anthropic-version-passthrough --strict --no-interactive`。
- [ ] 4. 审查与归档
  - [x] 4.1 一次全 diff 综合审查，确认只影响 Anthropic 版本头缺失场景。
  - [x] 4.2 记录 findings、未验证范围和残余风险。
  - [x] 4.3 通过后合并 delta、更新知识沉淀并归档。
