# Tasks — `agw reload`

- [ ] 1.1 调整 `reloadIfRunning` 签名
  - 文件:`internal/cli/common.go`
  - 步骤:把 `func reloadIfRunning(root string, cfg *config.Config)` 改为返回 `(reloaded bool, err error)`;失败时 stderr 警告改为返回 error。
  - 同步:`internal/cli/provider.go` 中 `saveAndReload` 调用点接收 error 并按 fatalf 处理。
  - 验证:`go build ./...` 通过;`go vet ./...` 通过;`go test ./internal/cli/...` 通过。
  - 预期:编译通过,无新警告。

- [ ] 1.2 新建 `internal/cli/reload.go`
  - 文件:新建 `internal/cli/reload.go`
  - 内容:
    - `var reloadCmd = &cobra.Command{Use: "reload", Short: "触发网关热重载（不写盘）", Run: runReload}`
    - `func init() { addRootFlag(reloadCmd); rootCommands = append(rootCommands, reloadCmd) }`
    - `func runReload(cmd *cobra.Command, args []string)`:
      1. `root := resolveRoot()`
      2. `cfg, err := config.Load(root)`(单独捕获解析失败,失败则 fatalf,退出码 1)
      3. 若 `!pidAlive(readPid(root))`:打印提示,退出 0
      4. `reloaded, err := reloadIfRunning(root, cfg)`:若 err != nil → stderr 警告,fatalf 退出码 2
      5. 打印成功提示,退出 0
  - 验证:`go build ./...`;`go run ./cmd/agw reload --help` 出现帮助文本。

- [ ] 1.3 单元测试 `internal/cli/reload_test.go`(可选但推荐)
  - 覆盖:
    - 网关未运行 → 退出 0,无 admin 请求
    - 网关运行 + reload 200 → 退出 0,发出 POST
    - 网关运行 + reload 500 → 退出 2,stderr 含警告
    - `local.toml` TOML 错误 → 退出 1,不发出 admin 请求
  - 方式:用一个 stub HTTP server 替换 `adminURL`/`adminRequest`(参考 `cli_test.go` 现有模式)。
  - 验证:`go test ./internal/cli/...` 全部通过。

- [ ] 1.4 文档同步
  - 文件:`README.md`(以及 `docs/` 下 usage guide 中 agw 命令章节,如有)
  - 步骤:在命令参考列表里增加 `agw reload` 一行,与 `agw start|stop|status|logs` 同节。
  - 验证:`git diff README.md` diff 自检,链接相对路径不变。

- [ ] 1.5 openspec 校验
  - 命令:`openspec validate add-agw-reload-command --strict --no-interactive`
  - 预期:全绿。

- [ ] 1.6 综合验证
  - 命令:`go vet ./...`、`go test ./...`、`scripts/validate-workflow.sh`(若适用)
  - 预期:全部 PASS。
  - 实地冒烟:启动 `agw start`,改 `local.toml` 里的某条 provider `priority`,`agw reload`,`agw status` 看到新 priority。