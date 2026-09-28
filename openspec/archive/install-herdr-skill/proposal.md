# 安装 Herdr 技能

模式: 严格
状态: 已归档

## Why

用户明确要求将 `herdrdev/herdr` 仓库 `v0.9.1` 标签下的 `skills/herdr/SKILL.md` 安装到当前项目。该技能会改变 Codex 和 Claude 的可用技能语义，并允许助手在满足 `HERDR_ENV=1` 时检查和控制终端、进程、其他 agent 与保存的 SSH machine，属于工作流治理和潜在外部副作用，因此必须按严格模式处理。

## What Changes

- 以 GitHub `v0.9.1` 标签和 Git blob SHA 为固定来源，审查并安装上游 `skills/herdr/SKILL.md`。
- 仅在当前项目 Codex 技能目录放置技能副本：`.codex/skills/herdr/SKILL.md`。
- 不安装到 `.claude/skills/`，不新增测试文件，不修改共享路由知识。
- 不安装 Herdr 二进制，不启动、连接或修改任何 Herdr 会话。
- 初始安装按当时用户要求保留为未跟踪文件；用户后续明确授权将该技能与 OpenSpec 记录提交到当前 main 并推送。

## Impact

- **目标**：当前项目内 Codex 的技能集合获得 `herdr` 技能；下次加载技能时可按用户显式请求使用。
- **非目标**：不改上游仓库；不安装或升级 CLI；不实际调用 Herdr 控制 pane、workspace、agent 或 SSH machine；不把技能安装到全局 `~/.codex/skills`；不改共享路由和 Claude 技能树。
- **用户修改保护**：安装时除本变更 OpenSpec 产物外工作区干净，目标目录不存在；后续 Git 整合时仅提交本变更路径，不触碰其他未完成工作。
- **安全边界**：上游内容已审查，未发现脚本执行、凭据读取或数据外传指令；该技能本身描述了控制当前终端会话和远端 machine 的能力，实际使用时必须遵守其 `HERDR_ENV=1` 前置检查和本仓库外部副作用授权规则。

## Source Fingerprint

- Repository: `https://github.com/herdrdev/herdr`
- Ref: `v0.9.1`（`git ls-remote` 校验为真实 tag，commit `8544776216a8d28088db59a5344ea21ee2d05d2b`）
- Path: `skills/herdr/SKILL.md`
- GitHub contents API Git blob SHA: `bcb22ba8d7b8259f25fa2bfd76dd5e520fd52fbc`
- Size: `13867` bytes
- Download SHA-256: `03855a7a1f9d0aa1ba6444fed2e4971adf796e91e73001d8f2472f5f9e5f659f`
- Directory listing: `skills/herdr/` 在该 ref 下仅含 `SKILL.md`，无支持脚本或隐藏文件。
