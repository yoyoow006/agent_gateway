模式: 标准
状态: 待归档

# Why

agw 网关运行时不监听 `config/local.toml` 文件变化,用户手动编辑后必须触发热重载才能生效。当前唯一可触发路径是 `agw provider add|enable|disable|remove|switch`,这些子命令**同时会写回 `local.toml`**——这导致两个问题:

1. 用户只改了文件、想生效,被迫调用一个会重写文件的命令,语义错误(可能误覆盖编辑器未保存的更改)。
2. 缺少纯粹的"重载"语义,自动化脚本无法干净地"应用已存在的配置文件"。

`POST /__agw/reload` 管理端点(`internal/gateway/server.go:124`)和 CLI 帮助函数 `reloadIfRunning`(`internal/cli/common.go:71`)已存在,但没有顶层 CLI 命令暴露它。

# What Changes

- 新增顶层 CLI 子命令 `agw reload`,语义单一:**只触发热重载,不写盘、不读取 local.toml 之外的任何状态**。
- 复用 `reloadIfRunning` 帮助函数;调整其退出码语义,使脚本可检测失败。
- 不引入新依赖,不修改 `/__agw/reload` 端点契约,不增加文件监听、不动 `agw provider` 现有命令。

# Impact

- 受影响文件:
  - `internal/cli/common.go` — 调整 `reloadIfRunning` 签名使其返回 `(changed bool, err error)`
  - `internal/cli/reload.go` — 新增,定义 `reloadCmd` 与 `runReload`
  - `internal/cli/root.go` 或 `reload.go` init — 注册 `reloadCmd` 到 `rootCommands`
  - `openspec/specs/gateway-cli/spec.md` — 增加 ADDED Requirements
  - `README.md` — 增加命令参考(简短一行)
  - `internal/cli/*_test.go` — 增加 `agw reload` 单元测试(可选)
- 受影响用户:仅 CLI 调用方;agent 端无变化。
- 受影响运行时:`/__agw/reload` 端点的调用方式不变(仍是 admin token + POST)。
- 风险点:退出码语义变化可能影响依赖旧语义的脚本;现有 `agw provider` 子命令通过 `saveAndReload → reloadIfRunning` 链路也会受影响,但只是退出码从"始终 0"变成"失败时非零",对人工操作无感。