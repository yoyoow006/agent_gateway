模式: 严格
状态: 已归档

# 项目创建令牌保存失败回滚

## Why

`agw project new` 目前先创建 `projects/<name>/`、写入 `agw.toml`、执行 `git init`，最后才把项目 token 写入 `config/local.toml`。前置配置解析失败已修复，但 token 保存仍可能因磁盘满、权限变化、序列化或 I/O 错误失败。此时命令失败却留下没有 token 的项目目录和 Git 仓库，用户重试会遇到“项目已存在”，且项目隔离令牌未建立。

## What Changes

- 将 `workspace.New()` 的持久化顺序调整为先写入项目 token，成功后再创建项目目录。
- 若 token 写入失败：
  - 返回原始保存错误；
  - 不创建项目目录；
  - 不写 `agw.toml`；
  - 不执行 `git init`。
- 若 token 写入成功但后续项目目录 / `agw.toml` 创建失败：
  - 回滚本次新增到 `config/local.toml` 的项目 token；
  - 删除本次创建且未成功完成的项目目录；
  - 返回带上下文的错误。
- `git init` 失败仍保持现有行为：仅警告，不判定项目创建失败。
- 通过可注入保存函数实现失败测试，不制造真实磁盘损坏。
- 增加项目名既有配置存在时的防冲突语义：如果 `config/local.toml` 已有同名项目 token，`project new` 应失败，不得覆盖既有 token。

## Impact

- `config/local.toml` 写入失败时不再留下半成品项目。
- 项目目录创建失败时会回滚刚写入的 token，避免配置中悬挂项目条目。
- 已存在同名项目配置时不会被 `project new` 覆盖 token。
- 成功路径仍创建：项目 token、项目目录、`agw.toml`、Git 仓库。
- Git 缺失或 init 失败仍为非致命警告。
