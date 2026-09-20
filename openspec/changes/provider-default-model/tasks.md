# tasks

## 1. 配置模型与 CLI

- [ ] 1.1 TDD：在 `internal/config/config_test.go` 增加三层合并断言——`default_model` 在 local 覆盖 default，项目引用的供应商保留该字段，空值不覆盖。运行 `go test ./internal/config -run 'TestLoadMerge|TestDefaultModel'`，预期全绿后实现 `Provider.DefaultModel`、TOML tag 与 `mergeProviderMap`。
- [ ] 1.2 TDD：在 `internal/cli/cli_test.go` 覆盖 `provider add --default-model` 写入与更新语义。运行 `go test ./internal/cli -run TestProviderAddDefaultModel`，预期先失败后实现 flag 与赋值。

## 2. 转发路径

- [ ] 2.1 TDD：在 `internal/gateway/server_test.go` 增加同协议未知模型测试：仅 `default_model` 时重写未知模型、其余字节保持不变；`model_map` 命中优先于兜底；未配置兜底继续透传。运行 `go test ./internal/gateway -run 'TestPassthroughDefaultModel|TestPassthroughModelMapRewritesOnlyModel'`。
- [ ] 2.2 TDD：增加跨协议测试：档案级/供应商级映射合并后未命中时应用供应商 `default_model`。运行 `go test ./internal/gateway -run TestTranslatedDefaultModel`。
- [ ] 2.3 实现统一 `resolveModel` 与两条路径接入；同协议继续使用 `applyModelMap` 精确替换。复跑 2.1/2.2 命令，预期全绿。

## 3. 文档与配置示例

- [ ] 3.1 更新 `config/default.toml`、`README.md`、`docs/usage-guide.md`，说明 `default_model`、优先级、只在映射未命中时生效与未配置时保持透传。
- [ ] 3.2 检查 `/v1/models` 既有测试与行为：不新增发布 `default_model`；如需断言则补最小测试。

## 4. 综合验证与收尾

- [ ] 4.1 运行 `go test ./...`、`go vet ./...`、`gofmt -l .`，预期分别全绿、无输出、无输出。
- [ ] 4.2 运行 `bash scripts/validate-workflow.sh && openspec validate provider-default-model --strict --no-interactive`，预期通过。
- [ ] 4.3 主会话执行一次全 diff 综合审查，确认无 Critical/Important 后更新状态并请求归档确认。

## 本地整合策略

- 默认不提交、不合并、不推送；所有修改保留在当前 `feature/provider-default-model` 工作区，等待用户验收。
- 完成后仅保留源码、测试、文档与 OpenSpec 变更；不生成临时文件或个人配置。
