# support-env-file-keys

模式: 标准
状态: 已归档

## Why

当前密钥两种配置方式：`config/local.toml` 明文（0600）或 `api_key_env = "VAR"` 环境变量间接。环境变量方式要求用户在启动网关前手动 export（或 `set -a; source .env`），后台 `agw start` 场景尤其实际不便。用户希望直接把密钥写在 `.env` 文件里，网关自动加载。

### 已确认的需求共识

- 网关启动时自动加载 `<网关根>/.env`（存在才加载，缺失不是错误）。
- `.env` 只作为环境变量来源，与 `api_key_env` 配合使用；`local.toml` 明文 `api_key` 优先级不变。
- 真实环境变量优先于 `.env` 同名值（不覆盖已存在的进程环境，dotenv 惯例）。
- `.env` 含密钥，必须加入 `.gitignore`；提交一份 `.env.example` 占位模板。
- 非目标：变量插值（`$VAR` 展开）、多文件分层（`.env.local` 等）、自动 chmod 用户文件。

## What Changes

- 新增 `.env` 解析与加载：`internal/config/` 增加极简 dotenv 解析器（KEY=VALUE、`#` 注释、空行、可选引号、可选 `export ` 前缀；无插值），`agw serve`/`agw start` 启动早期（buildServer）加载进进程环境，已存在的变量不覆盖；语法错误（缺 `=` 等）带行号快速失败。
- `provider test`（经 buildServer 之外的 ProbeProvider 路径）同样先加载 `.env`。
- `.gitignore` 追加 `/.env`；新增 `.env.example` 模板（注释齐全、无真实密钥）。
- README 配置参考与快速开始更新。

## Impact

- 运行时行为：密钥解析新增一个来源（进程环境前置注入），`ResolveAPIKey` 与配置合并逻辑零改动。
- 安全：`.env` 进 gitignore 防泄漏；加载时若发现文件权限宽于 0600 记警告日志（不阻止）。
- 兼容：无 `.env` 时行为与现状完全一致；真实环境变量优先保证容器/systemd 场景不受影响。
- 非目标（v1）：变量插值、多 .env 分层、Windows。
