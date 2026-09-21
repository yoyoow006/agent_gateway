# plan · project-token-save-rollback

## 目标与全局约束

- 基线：Go 1.24.11，模块 `agent_gateway`。
- 目标：消除 `agw project new` 在 token 保存或项目工件创建失败后的半成品状态。
- 安全不变量：
  1. 同名项目已有 token 时不得覆盖。
  2. 回滚删除仅允许作用于 `New()` 本次确认不存在的 `<root>/projects/<name>` 路径。
  3. 不得删除任何既有目录、父目录、`config/` 或其他项目。
  4. token 保存失败不得创建项目目录或执行 Git。
  5. git init 失败仍不是项目创建失败。
- 测试必须通过注入保存函数 / 文件系统状态触发错误，不制造跨测试全局状态。

## 职责单元 1：可注入保存与既有 token 防覆盖

### Modify/Test

- `internal/workspace/project.go`
  - 新增包内可替换函数：
    ```go
    var saveProjectConfig = config.SaveLocal
    ```
  - `New()` 使用 `saveProjectConfig(root, cfg)`。
- `internal/workspace/project_test.go`
  - 测试中临时替换并 `defer` 恢复。
- `New()` 在修改 cfg 前检查：
  ```go
  if existing, ok := cfg.Projects[name]; ok && existing.Token != "" {
      return "", fmt.Errorf("项目 %s 的令牌已存在", name)
  }
  ```
- 新测试：
  - `TestNewRefusesExistingProjectToken`
    - 预置 local.toml 有 `[projects.demo] token = "agw-existing"`
    - 调用 `New(root, "demo", fakeRunner)`
    - 断言失败
    - 断言 token 仍是 `agw-existing`
    - 断言 `projects/demo` 不存在

### Red/Green commands

- Red:
  ```bash
  GOCACHE=/tmp/agw-go-build-cache go test ./internal/workspace -run TestNewRefusesExistingProjectToken -count=1
  ```
- Green: same command exits 0.

## 职责单元 2：token 保存失败不产生项目副作用

### Modify/Test

- 顺序调整：
  1. 校验名称 / 目录不存在 / config.Load
  2. 检查既有 token
  3. 生成 token
  4. 更新内存 cfg
  5. `saveProjectConfig(root, cfg)`
  6. 保存成功后才 `MkdirAll(dir)`
  7. 写 `agw.toml`
  8. git init
- 新测试：
  - `TestNewTokenSaveFailureLeavesNoProject`
  - 替换 `saveProjectConfig` 返回 `errors.New("disk full")`
  - 断言：
    - `New` 返回错误
    - `projects/demo` 不存在
    - fakeRunner 未调用 git
- Red 预期：当前目录会先创建，测试失败。
- Green command:
  ```bash
  go test ./internal/workspace -run TestNewTokenSaveFailureLeavesNoProject -count=1
  ```

## 职责单元 3：项目工件创建失败回滚 token

### Modify/Test

- 在调用 `saveProjectConfig` 前读取并保存原 `local.toml` 内容（若存在）。
- 保存 token 后创建目录 / 写 agw.toml。
- 任一步失败：
  1. 若原文件存在，将原内容写回 `config/local.toml`，并保持 0600。
  2. 若原文件不存在，删除 `config/local.toml`。
  3. 仅当 `<root>/projects/<name>` 是本次创建路径时执行 `os.RemoveAll(dir)`。
  4. 返回：
     ```go
     fmt.Errorf("创建项目工件失败（已回滚令牌）: %w", err)
     ```
- 新测试：
  - `TestNewProjectArtifactFailureRollsBackToken`
  - 预置有效 config/default.toml，使 local.toml 原本不存在。
  - 构造失败方式：
    - 在 `projects/` 下预先创建同名只读文件 `demo`，使 `MkdirAll` 失败。
    - 注意：因目录存在性检查要求 IsDir 才报冲突，同名普通文件可进入 MkdirAll 失败路径。
  - 断言：
    - `New` 返回错误
    - `config/local.toml` 不存在或不含 demo token
    - 同名普通文件 `demo` 仍存在（证明未误删既有文件）
- 该测试同时验证安全不变量：回滚不得删除同名既有文件。

### Red/Green commands

- Red:
  ```bash
  go test ./internal/workspace -run TestNewProjectArtifactFailureRollsBackToken -count=1
  ```
- Green: same exits 0.

## 职责单元 4：现有成功路径保持

### Test

- 保留并运行：
  - `TestNewCreatesAll`
  - `TestNewGitMissingWarns`
  - `TestNewInvalidConfigLeavesNoProject`
  - `TestNewConflict`
- 新增断言（若必要）：
  - token 保存成功、工件创建成功时 local.toml 有 token。
  - git 失败不触发回滚。

## 回归验证

```bash
GOCACHE=/tmp/agw-go-build-cache go test ./internal/workspace -count=1
GOCACHE=/tmp/agw-go-build-cache go test -race ./internal/workspace -count=1
GOCACHE=/tmp/agw-go-build-cache go test ./internal/config ./internal/cli -run 'TestSaveLocal|TestProjectCommandsPersist|TestRunProject' -count=1 || true
GOCACHE=/tmp/agw-go-build-cache go build ./...
GOCACHE=/tmp/agw-go-build-cache go vet ./...
test -z "$(gofmt -l .)"
bash scripts/validate-workflow.sh --fast
openspec validate project-token-save-rollback --strict --no-interactive
git diff --check
```

若 CLI/config 测试因沙箱监听限制失败，记录完整失败原因；目标 workspace 测试必须真实通过。

## 提交计划

1. `chore(openspec): project-token-save-rollback`
2. `fix(workspace): roll back project token on partial creation`
3. `chore(review): record project rollback evidence`
4. `chore(archive): project-token-save-rollback`
5. 本地 `--no-ff` 合并回 main。
6. 不 push；推送需单独授权。

## 回滚

- 实现 commit 可整体 revert。
- 回滚实现中的删除路径仅由 `New()` 失败分支触发，并有测试锁定不删除既有普通文件。
