# adapt-codex-desktop 实现计划

## 目标与全局约束

- 新增 `agw run codex-desktop [--project 名] [--reset] [-- 额外参数...]`，仅自动发现并启动 Linux Codex / ChatGPT 桌面应用。
- 桌面子进程使用独立 `CODEX_HOME=<网关根>/.agw/codex-desktop`，不读取、不写入、不备份用户默认 `$CODEX_HOME/config.toml` / `auth.json`。
- 现有 `agw run codex` 的 `-p agw` 独立 profile 与 `AGW_API_KEY` 注入语义不变。
- 不引入第三方依赖，不复制 CodexPlusPlus 代码；只实现必要的路径解析、TOML 生成、JSON 生成与原子文件操作。
- 所有运行时行为按 TDD 执行：先写失败测试，再实现；每个任务必须记录实际命令与退出结果。
- 实现在隔离 worktree `.worktrees/adapt-codex-desktop`，不触碰当前未完成 Herdr 变更。

## 架构

- `internal/agent/desktop.go`
  - `KindCodexDesktop` 常量与 desktop resolver。
  - 显式 `AGW_CODEX_APP` 输入优先；Linux 默认 roots 为 `/usr/lib`、`/opt`、`$HOME/Applications`、`$HOME/.local/share`。
  - 识别应用目录名 `ChatGPT`、`chatgpt`、`Codex`、`codex`、`codex-beta` 及其 `app` 子目录；识别可执行名 `ChatGPT`、`chatgpt`、`Codex`、`codex`。
  - 仅检查文件存在、普通文件/可执行文件属性和有限白名单路径；不解析 `.desktop`、不执行 shell、不做全盘扫描。
  - resolver API 设计为注入 `runtime.GOOS`、搜索 roots、文件系统谓词与环境变量，保证 macOS/Windows 测试无需构建标签也能覆盖“自动发现不支持”。
- `internal/agent/desktop_home.go`
  - `DesktopHome(root)` 固定返回 `<root>/.agw/codex-desktop`。
  - 路径安全：clean 后必须绝对化并位于 canonical 网关根 `.agw` 下；若目标路径/祖先存在 symlink，必须 EvalSymlinks 后仍位于 canonical root 内。
  - 受管 home 只允许 `config.toml`、`auth.json`、`backups/`，发现其它顶层条目或无效受管内容时普通启动失败。
  - 生成确定性 `config.toml`：`model_provider = "agw"`、`disable_response_storage = true`、`[model_providers.agw]`、`base_url = <BaseURL(listen)>/v1`、`wire_api = "responses"`。
  - 生成确定性 `auth.json`：仅 `OPENAI_API_KEY`，带结尾换行。
  - 写入流程：解析/校验现有受管文件 → 生成计划 bytes → no-op 直接返回 → 创建 `backups/<UTC ns>-config.toml` 与 `...-auth.json`（缺失标记为空文件）→ 同目录临时文件 `0600` → `Sync` → `Rename`；第二个失败时恢复两个旧文件并清理本次新备份，恢复失败合并报错。
  - `--reset` 不启动应用：仅对 canonical 后的精确 `<root>/.agw/codex-desktop` 执行 guarded `RemoveAll`，再 `MkdirAll(0700)`；任何符号链接逃逸或路径不一致立即失败。
- `internal/agent/run.go`
  - `PrepareExec` 分派 `codex-desktop`：解析项目 token → 先 resolve app → 再确保独立 home → 返回 child env `CODEX_HOME=<managed home>`、项目目录、`argv = [executable, extraArgs...]`。
  - `PrepareResetDesktop` 独立处理 reset，避免启动路径与删除路径耦合。
- `internal/agent/exec.go` 或复用 `run.go`
  - 桌面应用不能 `syscall.Exec` 后丢失父进程输出语义假设；新增 `ExecDetachedOrWait` 可注入启动函数。默认 `exec.Command`，设置工作目录/env/args，启动并等待；测试用 fake executable。是否 detach 以最小实现等待为准，不添加调试参数。
- `internal/cli/run.go`
  - 扩展合法 kind、usage 与 `--reset` flag。`--reset` 仅对 `codex-desktop` 合法。
  - 缺 app 时输出可行动错误：支持 roots、可执行名、`AGW_CODEX_APP`、Linux-only。
- 文档
  - README 与 usage guide 增加 Linux-only、独立 home、token-only auth、官方登录/会话隔离、备份/reset/卸载、故障排查。

## Task 1 — Linux 应用解析器

### Create/Modify/Test

- Modify: `internal/agent/desktop.go`
- Modify: `internal/agent/agent_test.go` 或新增 `internal/agent/desktop_test.go`

### 红灯测试

新增测试覆盖：

1. `AGW_CODEX_APP` 指向可执行文件时直接返回该文件，不扫描 roots。
2. `AGW_CODEX_APP` 指向应用目录时按白名单可执行名归一化。
3. 默认 roots 搜索顺序与目录候选顺序正确。
4. `/usr/lib/chatgpt/app/ChatGPT` 这类 `app` 子目录可解析。
5. 白名单外目录/可执行名拒绝。
6. 未找到 app 时错误包含 roots、可执行名与 `AGW_CODEX_APP`。
7. 自动发现 `GOOS != linux` 时返回 unsupported-platform，且不调用搜索。
8. 空/不存在/不可执行路径拒绝。

命令（先确认失败，后实现至绿）：

```bash
go test ./internal/agent -run 'TestResolve.*Desktop|Test.*Desktop.*(App|Resolve|Unsupported)' -count=1
```

预期：红灯阶段编译失败或断言失败；实现后全部 PASS。

### 实现要点

- 不引入 `filepath.WalkAll`；最多枚举有限 root 一级与应用目录一级。
- Windows `.exe` 不纳入 Linux 自动发现；显式路径也不得绕过 Linux-only 边界。
- 测试 fake FS 使用真实临时文件与 mode bits；非 Linux 下测试可执行性按 `unix.Executable` 分支处理，测试用普通文件注入谓词。

## Task 2 — 独立 home 与受管配置写入

### Create/Modify/Test

- Create: `internal/agent/desktop_home.go`
- Create: `internal/agent/desktop_home_test.go`

### 红灯测试

新增测试覆盖：

1. 全局 token + IPv4 生成正确 TOML/JSON、URL 带 `/v1`。
2. IPv6 `[::1]:8787` 生成 `http://[::1]:8787/v1`。
3. 首次创建目录 `0700`、两文件 `0600`、备份记录缺失前状态。
4. token/listen 变化时生成新备份并替换两文件。
5. 内容与权限均不变时无新备份且 mtime 不变。
6. 权限过宽但内容相同时收紧权限并备份。
7. `backups/` 之外出现未识别条目时失败并提示 `--reset`。
8. 现有 `config.toml` / `auth.json` 无效时失败，不触碰用户默认 home。
9. 符号链接让 managed home 逃出 root 时失败。
10. reset 只删除并重建 managed home，不删除默认 `~/.codex` 或 sibling。
11. reset 遇 symlink escape 失败且不删除。
12. 原子替换第二文件失败时恢复旧 pair 并报告 rollback 状态。
13. 备份创建失败时不改 live files。
14. 生成的 TOML 可被现有 BurntSushi decoder 解析，JSON 可被 `encoding/json` 解析。

命令（先红后绿）：

```bash
go test ./internal/agent -run 'Test.*(DesktopHome|DesktopConfig|DesktopAuth|DesktopAtomic|DesktopReset)' -count=1
```

### 实现要点

- 禁止 `os.WriteFile` 直接写最终 live 文件；所有替换必须临时文件 + `Sync` + `Rename`。
- no-op 判定先比对内容，再比对 mode；权限收紧视为变更。
- 备份文件名使用 UTC nanoseconds，保留旧内容；缺失文件写入空备份文件。
- 一次 preparation 最多生成一个备份前缀，避免 config/auth 时间戳不一致。
- rollback 采用旧内容/缺失语义，不依赖备份目录。
- 不实现 backup 修剪、迁移或跨项目历史保留；这是后续扩展。

## Task 3 — Run preparation、CLI 与 reset 集成

### Create/Modify/Test

- Modify: `internal/agent/run.go`
- Modify: `internal/cli/run.go`
- Modify: `internal/agent/agent_test.go`、`internal/cli/cli_test.go`

### 红灯测试

Agent 层：

1. `PrepareExec(root, "codex-desktop", "foo", ...)` 使用项目 token、项目目录、managed `CODEX_HOME`、resolved executable。
2. global 档案使用 default token。
3. app 缺失时在写 managed files 前失败。
4. 现有 `codex` 测试继续证明 CLI profile 不使用 managed home。
5. 用户默认 `$CODEX_HOME/config.toml` 与 `auth.json` 内容/mtime 不变。
6. `PrepareResetDesktop` 成功后目录为空且不调用 app resolver/exec。
7. 非 Linux 自动发现错误可测试。
8. extraArgs 只出现在 executable 后。

CLI 层：

1. `agw run codex-desktop` 被 `execAgent` 捕获，`CODEX_HOME` env 正确。
2. `--reset` 走 reset 函数且不调用 `execAgent`。
3. `claude|codex --reset` 拒绝。
4. usage/invalid kind 错误更新。
5. `--` passthrough 保持原语义。

命令：

```bash
go test ./internal/agent ./internal/cli -run 'Test.*(CodexDesktop|DesktopExec|DesktopReset|RunDashDash)' -count=1
```

### 实现要点

- app resolution 必须先于 managed home 写入，满足“缺 app 不改配置”。
- `PrepareExec` 返回 env 中不得包含完整 token 的日志输出；CLI 输出仅项目名/目录/应用路径。
- 不安装桌面应用、不启动 Codex CLI、不读取默认 Codex home。
- `--reset` 是显式破坏性动作，只允许 managed home。

## Task 4 — 文档与集成回归

### Create/Modify/Test

- Modify: `README.md`
- Modify: `docs/usage-guide.md`

### 内容契约

1. Linux-only 表述明确；macOS/Windows 不支持。
2. 独立 home 路径、配置内容与权限写清楚。
3. 说明官方 OAuth 不迁移：独立 home 只含 agw token，可能需要重新接受桌面应用初始化/登录提示；请求模型走 agw。
4. 说明已有桌面进程不会被杀，需用户手动关闭/重启。
5. reset 命令、备份位置、失败回滚和卸载删除 `.agw/codex-desktop`。
6. 缺 app 错误与 `AGW_CODEX_APP` 指引。
7. 现有 CLI 零接触承诺不被弱化。

### 命令

```bash
go test ./internal/agent ./internal/cli -count=1
go test ./... -race
go vet ./...
gofmt -w internal/agent internal/cli
git diff --check
openspec validate adapt-codex-desktop --strict --no-interactive
grep -R "codex-desktop" -n README.md docs/usage-guide.md
```

预期：全部退出 0；`grep` 至少命中 README 与 usage guide 的支持边界、独立 home 和 reset 文档段落。若 sandbox 限制导致 `-race` 网络类测试失败，记录具体失败与替代的 `go test ./...`。

## Task 5 — 严格审查与 Verify

### 审查单元

- 单元 A：managed authentication home 写入、原子性、rollback、路径与 reset 删除守卫。
- Verify 阶段 1：规格符合性（逐条 delta scenario 对应代码/测试/文档）。
- Verify 阶段 2：代码质量、错误处理、并发/权限、跨平台与文档一致性。

### 必跑命令

```bash
go test ./... -race
go vet ./...
bash scripts/validate-workflow.sh --archive-light
openspec validate adapt-codex-desktop --strict --no-interactive
git diff --check
```

### 审查流程

1. 主会话按 `.ai/rules/review.md` freeze 精确范围；reviewer 读取前和结论前运行 `review_manifest.py verify`。
2. 任一 `STALE` 立即停止并重新审查。
3. Critical/Important 必须修复并复审；Minor 可记录残余风险。
4. 手动 Linux 桌面 smoke test 仅在真实应用存在且用户授权时执行；否则记录 NOT_RUN，不伪造通过。

## Task 6 — 提交与归档准备

- 按职责单元组织提交：
  1. OpenSpec 四件套 + 设计计划 + 状态（已在 strict worktree 前置提交完成）。
  2. Linux resolver + managed home + run/CLI 行为 + 测试（可按 resolver/home/CLI 拆为 2–3 个可回滚提交）。
  3. 文档与知识沉淀。
- Verify 全部通过后将 proposal 置为`待归档`，不自动移动归档；等待用户决定本地整合。
- 归档时合并 delta、更新项目卡/memory、运行 archive gate，并保留 NOT_RUN 与残余风险。

## 失败路径与回滚

- 测试红灯不得通过放宽断言转绿；先用 systematic-debugging 定位。
- managed home 写入失败：实现内置 pair rollback；若 rollback 也失败，错误中包含两个路径与用户应执行的 reset/备份指引。
- 工作区污染：只操作本变更明确文件；发现重叠未提交修改立即停止。
- Git 回滚：删除 feature worktree/分支前必须用户授权；未合并前仅报告路径。
