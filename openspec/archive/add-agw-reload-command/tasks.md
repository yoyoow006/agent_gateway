# Tasks — `agw reload`

- [x] 1.1 调整 `reloadIfRunning` 签名
  - 文件:`internal/cli/common.go`
  - 步骤:把 `func reloadIfRunning(root string, cfg *config.Config)` 改为返回 `(reloaded bool, err error)`;失败时 stderr 警告改为返回 error。
  - 同步:`internal/cli/provider.go` 中 `saveAndReload` 调用点接收 error 并 stderr 警告(非致命,沿用原 provider 语义)。
  - 验证:`go build ./...` 通过;`go vet ./...` 通过;`go test ./internal/cli/...` 通过。
  - 结果:`TestProviderCommandsPersist` 继续通过,未触发任何 panic。

- [x] 1.2 新建 `internal/cli/reload.go`
  - 文件:新建 `internal/cli/reload.go`
  - 内容:`reloadCmd` (RunE 模式,因 cobra.Run 比 os.Exit 更利于测试) + `runReload` 返回 `ErrReloadFailed` sentinel + `init()` 注册。
  - 验证:`go build ./...` 通过;`go run ./cmd/agw reload --help` 显示长帮助文本。

- [x] 1.3 单元测试 `internal/cli/reload_test.go`
  - 覆盖:
    - 网关未运行 → 退出 0,无 admin 请求
    - 网关运行 + reload 200 → 退出 0,发出 1 次 POST
    - 网关运行 + reload 500 → 退出非零(error 含 `ErrReloadFailed`)
    - `local.toml` TOML 错误 → 退出非零,不发出 admin 请求
  - 验证:`go test ./internal/cli/...` 全部通过(4/4 reload 测试 + 12 现有测试)。

- [x] 1.4 main.go sentinel → 区分退出码 1 vs 2
  - 文件:`cmd/agw/main.go`
  - 原因:RunE 返回的 error 由 main.go 决定 exit code,需识别 `ErrReloadFailed` → 2,其它 → 1。

- [x] 1.5 README 同步
  - 文件:`README.md`
  - 步骤:在命令参考列表里增加 `agw reload` 一行;热重载说明段补一句"手动编辑后用 agw reload"。
  - 验证:diff 检查无断行/格式问题。

- [x] 1.6 openspec 校验
  - 命令:`openspec validate add-agw-reload-command --strict --no-interactive`
  - 结果:Change 'add-agw-reload-command' is valid。

- [x] 1.7 综合验证
  - 命令:`go vet ./...`、`go test ./...`、`scripts/validate-workflow.sh`
  - 结果:vet 全绿;`go test ./...` 11 包全绿;workflow 校验 PASS=199 FAIL=2(两条为已知安装器基线失败,见 `agw-preexisting-workflow-test-failure.md`,exit=0)。
  - 实地冒烟:启动 `agw start`,改 `local.toml` 里的 yxr `priority` 1→5,`agw reload` exit=0,`agw provider list` 显示新 priority=5;恢复后再次 reload exit=0。