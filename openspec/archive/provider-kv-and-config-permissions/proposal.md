模式: 标准
状态: 已归档

# 供应商键值校验与配置权限错误

## Why

两个 CLI/配置可靠性问题：

1. `provider add` 的 `--model` / `--header` 参数经 `parseKVs()` 解析；缺少 `=`、key 为空或 value 为空时会被静默丢弃。用户 typo 后命令显示保存成功，但映射或 header 未生效。
2. `atomicWriteFile()` 在 chmod 临时文件失败时已经返回错误，但 rename 后缺少最终权限验证。此前 `SaveLocal` 曾忽略 chmod 失败；现在需要契约测试明确锁住“权限无法收紧必须失败”，防止回归。

## What Changes

- 将 provider KV 解析改为显式校验：
  - 格式必须为 `key=value`
  - key 非空
  - value 非空
  - 重复 key 报错，避免重复 flag 静默覆盖
- 校验在构造 provider 或写盘前执行；失败时列出具体参数和正确格式示例。
- `atomicWriteFile()` 成功 rename 后 stat 目标文件并验证 mode permission 为指定值。
- 验证失败或 stat/chmod 失败均返回错误。
- 增加测试：
  - 非法 `--model` / `--header` 不写 `local.toml`
  - 合法 KV 正常保存
  - 权限 helper 成功路径返回 0600

## Impact

- 用户 typo 不再静默丢失映射/header，而是得到明确错误。
- `local.toml` 权限无法达到 0600 时保存失败，不再产生安全假阳性。
- 合法 provider 配置行为不变。
- 不改变 TOML 结构、热重载或 provider 替换语义。
