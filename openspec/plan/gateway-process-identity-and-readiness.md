# plan · gateway-process-identity-and-readiness

## 目标与全局约束

- 基线：Go 1.24.11，Linux x64 优先；本变更的身份读取基于 Linux `/proc`。
- 目标：
  1. `agw stop` 发送 SIGTERM 前验证 PID 身份，防 PID 复用误杀。
  2. `agw start` 只有 `/__agw/healthz` 可用后才报成功。
- 信号安全不变量：
  - 旧纯 PID pidfile 不发信号。
  - JSON pidfile 的 start-time/exe 任一可验证且不一致时不发信号。
  - `/proc` 信息不可读时保守失败，不降级为“只看 PID 存活”。
- 就绪不变量：
  - 子进程退出优先判失败。
  - healthz 未成功前不写最终 pidfile。
  - 成功后 pidfile 权限 0600。
- 测试不得依赖本地 socket 监听；使用注入 `selfPath`、health checker 与临时脚本进程。

## 职责单元 1：身份模型与 /proc 读取

### Create/Modify/Test

- Modify `internal/cli/lifecycle.go`
  - 新增：
    ```go
    type pidIdentity struct {
        Pid       int    `json:"pid"`
        StartTime int64  `json:"start_time"`
        Exe       string `json:"exe"`
        Root      string `json:"root"`
    }
    ```
  - `readPidIdentity(root string) (pidIdentity, bool, error)`：
    - 文件缺失 → zero,false,nil
    - JSON 解析成功且 Pid>0 → identity,true,nil
    - 内容是纯数字 → zero,false,nil（legacy）
    - 其他损坏 → zero,false,error
  - `processIdentity(pid int) (startTime int64, exe string, ok bool)`
    - 读取 `/proc/<pid>/stat`
    - 解析第 22 个字段（starttime）
    - 读取 `/proc/<pid>/exe` symlink
    - 任一关键信息不可读 → ok=false
  - `sameGatewayProcess(identity pidIdentity) bool`
    - 当前 pid、start-time、exe 全部匹配
    - `/proc` 不可读 → false
- Tests:
  - 当前进程身份可读取且 pid/exe 与 `os.Executable()` 一致
  - 不存在 PID 返回不可用
  - pidfile JSON 可读写，legacy 内容返回 `legacy=true`

## 职责单元 2：start 就绪状态机

### Modify/Test

- 增加可注入函数：
  ```go
  var healthReady = func(root, listen string) bool { ... }
  ```
- URL：
  - `net.SplitHostPort(listen)`
  - `net.JoinHostPort(host, port)`
  - `http.Get("http://"+hostPort+"/__agw/healthz")`
  - 2xx 即 ready
- `StartGateway(root, listen)`：
  1. 既有身份运行中则报错
  2. 建 `.run/`
  3. 启动子进程
  4. 轮询周期 50ms，超时 5s
  5. 每次循环先非阻塞读取 `waitCh`
     - 退出 → 删除临时/最终 pidfile，返回日志尾部错误
  6. healthReady=true →
     - 读取当前子进程 `/proc` 身份
     - 写 JSON pidfile 0600
     - 身份不可读 → 返回错误并清理 pidfile
  7. timeout → 返回错误；不发送信号、不写最终 pidfile，保留进程自然存续并提示日志
- Tests:
  - ready path：注入 healthReady=true，fake selfPath 为存活脚本，成功并 pidfile JSON 0600
  - exited path：fake selfPath 为秒退脚本，healthReady 永远 false，返回日志尾部，无 pidfile
  - timeout path：fake selfPath 存活脚本，healthReady=false，缩短 timeout 注入，返回超时错误，无 pidfile，测试清理子进程

## 职责单元 3：stop/status/reload 身份判定

### Modify/Test

- 替换 `pidAlive(readPid(root))` 判断为 `gatewayRunning(root)`：
  1. 读取 JSON 身份
  2. legacy → running=false
  3. 身份匹配 → running=true
  4. 不匹配/不可验证 → false
- `StopGateway`：
  - 不运行：
    - legacy 单独提示“旧 pidfile 缺少身份，拒绝停止；请确认后删除或重启网关”
    - 身份不匹配提示“PID 身份不匹配，可能被复用；已清理 pidfile”
    - 均清理 pidfile，不发信号
  - 运行：
    - 发送 SIGTERM
    - 等待退出时仍用身份匹配 + PID 存活；退出后清理 pidfile
- `StatusInfo.Running` 使用 `gatewayRunning`
- `reloadIfRunning` 使用 `gatewayRunning`
- Tests:
  - 伪造 JSON 身份 pidfile（pid=self，start-time wrong / exe wrong）
    - `StopGateway` 不发信号、清理 pidfile、返回复用错误
  - legacy pidfile
    - `StopGateway` 不发信号、清理、返回旧格式错误
  - 身份匹配路径可通过当前进程 pidfile 验证 `gatewayRunning=true`，不实际调用 Stop 信号

## 职责单元 4：文档与知识

### Modify

- `docs/usage-guide.md`
  - `agw start`：说明健康检查成功后才报启动成功
  - `agw stop`：说明 PID 身份校验与旧 pidfile 处理
- `README.md`
  - 生命周期命令表补充身份校验语义
- `.ai/memory/agent-gateway.md`
  - 归档时沉淀 `/proc` 身份、legacy pidfile 与 healthz readiness 规则

## 回归验证

```bash
GOCACHE=/tmp/agw-go-build-cache go test ./internal/cli -run 'TestPid|TestGateway|TestStartGateway|TestStopGateway|TestReload|TestProcess|TestLifecycle' -count=1
GOCACHE=/tmp/agw-go-build-cache go test ./internal/cli -count=1 || true
GOCACHE=/tmp/agw-go-build-cache go build ./...
GOCACHE=/tmp/agw-go-build-cache go vet ./...
test -z "$(gofmt -l .)"
bash scripts/validate-workflow.sh --fast
openspec validate gateway-process-identity-and-readiness --strict --no-interactive
git diff --check
```

如完整 CLI suite 因既有 `httptest.NewServer` 沙箱监听限制失败，记录原因；目标生命周期测试必须真实通过。

## 提交计划

1. `chore(openspec): gateway-process-identity-and-readiness`
2. `fix(cli): verify gateway pid identity and readiness`
3. `chore(review): record process readiness evidence`
4. `chore(archive): gateway-process-identity-and-readiness`
5. 本地 `--no-ff` 合并回 main
6. 不 push；推送单独授权。

## 回滚

- 实现集中在 `internal/cli/lifecycle.go` 与目标测试，revert 实现 commit 即恢复旧行为。
- 归档后回滚需按 OpenSpec 新变更处理。
