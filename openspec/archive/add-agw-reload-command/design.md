# Design — `agw reload`

## 关键决策

### 1. 调整 `reloadIfRunning` 签名而非另写一套
**选择**:把 `internal/cli/common.go:71` 的 `func reloadIfRunning(root string, cfg *config.Config)` 改为 `func reloadIfRunning(root string, cfg *config.Config) (reloaded bool, err error)`。

**理由**:`agw provider add|enable|disable|remove` 已经在调用 `reloadIfRunning`(经 `saveAndReload` 链路);另写一份只会让维护成本翻倍。退出码变化对 `agw provider` 是**改善**(原来静默失败,现在会冒泡),不破坏既有用例。

**替代方案**(已否决):
- (a) 在 `reload.go` 里完全复制 `reloadIfRunning` 的逻辑 → 重复代码,后续若管理端点变化(如 `/__agw/v2/reload`)要改两处。
- (b) 给 `reloadIfRunning` 增加一个 "exitNonZeroOnFail" 参数 → 引入 bool 形参,惯用做法是用返回值表达成败。

### 2. 不引入文件监听
**选择**:本次只加 CLI 命令,不在网关进程内加 `fsnotify` 或定时轮询监听 `local.toml`。

**理由**(已在需求确认阶段与用户达成):
- 用户明确选了"加 `agw reload` CLI 子命令"而不是"加文件监听自动热重载"。
- 文件监听引入依赖、跨平台差异、编辑器写回风暴、并发场景下的 reload 风暴抑制等额外问题,与"补一个 reload 触发点"的轻量诉求不匹配。

### 3. 退出码策略
| 场景 | 退出码 | 输出流 |
|---|---|---|
| 网关未运行 | 0 | stdout(提示) |
| 重载成功(200) | 0 | stdout(成功) |
| 重载失败(网络/非 2xx) | 2 | stderr(警告) |
| `local.toml` 解析失败 | 1 | stderr(错误) |

**理由**:
- "网关未运行"对 reload 而言是合法的幂等态(本次不做事、配置会在下次 start 时生效),不算失败。
- "请求发出但服务端拒绝"是真正的失败,2 表示外部边界错误,与 1(本地输入/解析错误)区分。

### 4. 命令注册位置
**选择**:新增 `internal/cli/reload.go`,在 `init()` 中 `addRootFlag(reloadCmd)` 并 `rootCommands = append(rootCommands, reloadCmd)`。

**理由**:与现有 `lifecycle.go`(start/stop/status/logs)、`provider.go` 的 init 注册模式一致。

## 风险与边界

- **风险 1**: `reloadIfRunning` 签名变化会让 `saveAndReload` 调用点出 lint 警告或编译错。
  - **缓解**:同步修改 `saveAndReload` 与 `reloadIfRunning` 的两处调用方,作为同一任务的一部分。
- **风险 2**: 解析失败时不再走 reload 请求,可能与"用户以为我已 reload"的预期不一致。
  - **缓解**:错误信息明确指出"local.toml 解析失败",并显示原 TOML 错误位置。
- **风险 3**: 网关 reload 端点本身可能因配置损坏而失败(管理端点返回 5xx)。当前 `internal/gateway/server.go:79` 的 `reload` 函数未处理 `newCfg` 校验失败的细节;若校验失败网关通常返回 500。
  - **缓解**:CLI 把所有非 2xx 都归到退出码 2 的"重载失败"分支,提示用户看网关日志。

## 未覆盖范围

- 文件监听 / 自动 reload(本次不做)
- `agw diff`(本次不做)
- `default.toml` 监听(本次不做)
- reload 命令的 `--dry-run` / `--show-config` 选项(超出本次最小需求)

## 验收

- 在已运行的网关环境下执行 `vim config/local.toml`(改一处)→ `agw reload` → 下一条请求按新配置路由。
- 在未运行的网关环境下执行 `agw reload` → 退出码 0,提示"网关未运行"。
- 在 `local.toml` 中故意引入 TOML 语法错误 → `agw reload` → 退出码 1,显示 TOML 错误位置。
- 在已运行的网关环境下,把 admin token 改坏 → `agw reload` → 退出码 2(网关侧 reload 端点会因鉴权失败返回 401)。
- `openspec validate add-agw-reload-command --strict --no-interactive` 通过。
- `go vet ./...`、`go test ./...` 全部绿色。