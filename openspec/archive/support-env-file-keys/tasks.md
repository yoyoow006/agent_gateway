# support-env-file-keys 任务

## 本地整合策略

- 基于 main（0353f05）开 `feature/support-env-file-keys` 分支；主会话直执，TDD；每任务一提交；完成后 `--fast` 校验 + 一次全量 diff 自查（标准模式，改动面小不派 reviewer，除非发现风险升级）。

- [x] 1.1 分支与红线：`.gitignore` 追加 `/.env`；创建 `.env.example`（注释说明优先级与 0600 建议）。验证：`git status` 无 `.env` 泄漏风险、example 无真实密钥。
- [x] 1.2 先写失败测试 `internal/config/dotenv_test.go`：解析（KEY=VALUE/注释/空行/单双引号/export 前缀/行内空白）、坏行报错含行号、LoadEnvFile 不覆盖已存在环境变量、文件缺失返回 nil。
- [x] 1.3 实现 `internal/config/dotenv.go`：`ParseEnvFile([]byte) (map[string]string, error)` 与 `LoadEnvFile(path string) error`（存在才读、Setenv 仅缺失时、0600 检查警告经回调返回）。验证：`go test ./internal/config/`。
- [x] 1.4 先写失败测试 `internal/cli/cli_test.go` 增例：临时根写 `.env`（RELAY_KEY）+ local.toml 供应商 `api_key_env` → `buildServer` 后经 httptest 假上游发请求，断言上游收到 `Bearer sk-from-env-file`；真实环境变量存在时 `.env` 值不生效（t.Setenv 预置）。验证：`go test ./internal/cli/ -run EnvFile`。
- [x] 1.5 接线：`buildServer`（serve.go）在 `config.Load` 前调用 `LoadEnvFile(filepath.Join(root, ".env"))` 并把权限警告写入 logger/stderr；`runProviderTest` 路径同样先加载。验证：新增集成测试绿。
- [x] 1.6 文档：README 密钥段与快速开始补 `.env` 用法（示例 + 优先级说明）。验证：人工核对命令可复制。
- [x] 1.7 收口：`go build ./... && go vet ./... && go test -race ./... && gofmt -l .`（空）+ `bash scripts/validate-workflow.sh --fast` PASS + `openspec validate support-env-file-keys --strict --no-interactive`；全勾任务、proposal 置`待验证`，走 verify（改动面小：主会话自查 + 必要时快速终验）后归档合并回 main。

## 完成定义

- 上述验证全部通过且证据新鲜；README/example/规格与实现一致；`.env` 已入 gitignore。
