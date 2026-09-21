模式: 标准
状态: 构建中

# 原子写入 local 配置

## Why

`config/local.toml` 保存 admin token、全局 token、项目 token 与供应商池，是网关的关键本地状态。当前 `SaveLocal()` 直接 `os.WriteFile` 覆盖目标文件；进程中断、磁盘满或 I/O 错误可能留下截断 TOML，导致网关下次启动或热重载失败，并使用户难以恢复令牌与供应商配置。

## What Changes

- 将 `SaveLocal()` 改为同目录临时文件写入。
- 写入成功后执行文件 `Sync()`。
- 将临时文件权限收紧为 `0600`。
- 使用 `Rename()` 原子替换 `config/local.toml`。
- rename 前保留旧文件；任何写入、sync、chmod 或 rename 失败时返回错误且不删除既有 `local.toml`。
- 成功后清理临时文件；失败路径尽力清理临时文件。
- 保持既有语义：
  - 输出 TOML 内容不变；
  - 最终文件权限为 0600；
  - 保存后重建 token index；
  - `config/` 目录不存在时仍自动创建。

## Impact

- 异常中断不再把 `local.toml` 置为半写状态；要么保留旧配置，要么完整替换为新配置。
- 磁盘满 / fsync / rename 失败时旧配置仍可用。
- 配置保存失败会明确返回错误，不再静默忽略 chmod 失败。
- 不改变配置格式、加载顺序、令牌生成或热重载行为。
