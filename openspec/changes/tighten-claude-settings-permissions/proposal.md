模式: 标准
状态: 构建中

# 重写 Claude settings 时收紧文件权限

## Why

`.agw/claude-settings.<project|global>.json` 包含网关虚拟令牌。当前 `GenerateClaudeSettings` 使用 `os.WriteFile(path, ..., 0600)`；Unix 上该 mode 只影响新文件，若既有文件曾变成 0644，后续重写仍保持 0644，违背“含令牌文件必须 0600”的安全预期。

## What Changes

- 写入 Claude settings 后检查文件权限。
- 如果权限不是 owner-read/write/execute 语义中的 `0600`，调用 `os.Chmod` 收紧为 `0600`。
- chmod 失败时返回错误，不静默继续。
- 增加回归测试：预置 0644 文件并重写后，断言最终权限为 0600。

## Impact

- 修复既有 Claude settings 权限过宽时反复重写仍暴露项目令牌的问题。
- 新建文件行为不变。
- Codex profile 不含密钥，本次不改。
- 不改变 settings 内容、文件命名或生成时机。
