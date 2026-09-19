# gateway-cli (delta)

> ADDED Requirements 标记本次新增;原有 MODIFIED/REMOVED 见主规格 `openspec/specs/gateway-cli/spec.md`。

## ADDED Requirements

### Requirement: `agw reload` Subcommand

agw SHALL 提供顶层子命令 `agw reload`,用于在不动磁盘的前提下重新载入网关配置。

#### Scenario: gateway running, reload succeeds
- WHEN 用户在网关根目录(或 `--root` 指向的目录)执行 `agw reload`
- AND 网关进程存活(pidfile 指向进程可信号 0 探测)
- AND 管理端点 `POST /__agw/reload` 返回 200
- THEN CLI 打印一行成功提示到 stdout,以退出码 0 退出

#### Scenario: gateway not running
- WHEN 用户执行 `agw reload`
- AND pidfile 不存在或对应进程已退出
- THEN CLI 打印"提示:网关未运行,配置将在下次启动时生效"到 stdout
- AND 以退出码 0 退出(幂等,不算失败)

#### Scenario: reload request fails (network error or non-2xx)
- WHEN 用户执行 `agw reload`
- AND 网关进程存活但 `POST /__agw/reload` 因网络错误、超时或返回非 2xx 而失败
- THEN CLI 打印"警告:热重载请求失败(<原因>);网关重启后生效"到 stderr
- AND 以非零退出码(2)退出,供脚本检测

#### Scenario: invalid local.toml
- WHEN 用户执行 `agw reload`
- AND `local.toml` 解析失败(例如 TOML 语法错误或必填字段缺失)
- THEN CLI 打印解析错误到 stderr
- AND 以非零退出码(1)退出,不向管理端点发送 reload 请求

#### Scenario: explicit --root
- WHEN 用户执行 `agw reload --root /path/to/gateway`
- THEN CLI 使用该路径作为网关根,而不依赖 cwd 向上探测