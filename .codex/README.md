# Codex 工作流适配层

本目录把仓库的风险分级工作流映射到 Codex 原生能力；`openspec/` 与 `.claude` 共用，不复制状态。

## 模式

| 模式 | Codex 行为 |
|---|---|
| 快速 | 当前工作区直接修改与针对性验证，不建 OpenSpec、不派代理、不自动提交 |
| 标准 | Open 一次产出四件套并一次确认；小任务主会话直执，最多一次综合审查 |
| 严格 | 完整五阶段、隔离 worktree、任务级审查和 Verify 双阶段审查 |

风险分类、状态与共享底线以根 [AGENTS.md](../AGENTS.md) 为准；阶段细节以 `.codex/skills/<名>/SKILL.md` 为准。行为回归场景在 `scripts/workflow-pressure-scenarios.md`。

## 目录

| 路径 | 用途 |
|---|---|
| `AGENTS.md` | Codex 入口和风险路由 |
| `.codex/skills/` | 5 个阶段技能与 8 个支撑技能 |
| `.ai/` | kb、memory、rules、共享角色 prompts 与只读工具 |
| `.codex/agents/` | explorer、reviewer、test worker 的 Codex 薄适配；职责真源在 `.ai/prompts/agents/` |
| `.codex/sdd/<变更名>/` | 确需子代理时的本地简报、报告与审查包；Git 忽略 |
| `openspec/` | 标准/严格变更的共享状态、规格、计划和归档 |

## 原生工具映射

| 工作流动作 | Codex 执行方式 |
|---|---|
| 派发全新上下文子代理 | `spawn_agent`，使用 `fork_turns: "none"` |
| 给已有代理追加任务 | `followup_task`；仅传消息用 `send_message` |
| 等待代理结果 | `wait_agent` |
| 待办追踪 | `update_plan` |
| 文件修改 | `apply_patch` |
| 命令执行 | `exec_command` |

仅严格模式或标准模式中确实独立、规模较大、需要隔离判断的任务使用子代理。派发时提供边界明确的简报、相关 memory 摘要、验收命令和报告路径；审查者不继承实现者的推理。explorer、reviewer、test worker 适配只引用 `.ai/prompts/agents/`，不复制共享算法。

标准/严格完整审查先由主会话 freeze；reviewer 只读，按 `.ai/rules/review.md` 在读取前和结论前各运行 `review_manifest.py verify`，任一 `STALE` 立即停止。快速模式不创建 manifest、不派角色代理；标准仍至多一次综合审查，严格仍保留任务级和 Verify 两个独立关注面。

## SDD 产物

使用子代理时，`.codex/sdd/<变更名>/` 可存放：

- `progress.md`：跨上下文恢复台账。
- `task-N-brief.md` / `task-N-report.md`：任务契约与实现报告。
- `review-<base>..<head>.diff`：精确审查范围。
- `skill-baseline.md` / `skill-green.md`：writing-skills 红绿压力测试证据。

标准小任务和快速任务不得仅为满足形式而创建这些文件。

## 双套一致性

- 风险条件、状态、确认点、审查门禁必须在 `.claude` 与 `.codex` 保持语义一致。
- 允许差异仅限工具映射和助手适配。
- 用 `bash scripts/validate-workflow.sh` 检查结构与关键契约；技能行为用压力场景验证。
