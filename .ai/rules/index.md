# 共享模块路由表

| 模块 | 权威路径 | 用途 |
|---|---|---|
| 风险分级工作流 | `AGENTS.md` 或 `CLAUDE.md` | 快速、标准、严格模式与门禁 |
| 共享基础设施 | `.ai/` | 知识、审查证据、角色与校验 |
| 助手阶段技能 | 所选助手的 `skills/` | Open、Design、Build、Verify、Archive |
| 项目事实 | `.ai/kb/projects/registry.json` 和项目卡 | 有界只读查询范围 |
| 变更状态 | `openspec/changes/<变更名>/` | proposal 状态、tasks 进度与 delta |
| 工作流校验 | `scripts/validate-workflow.sh` | 结构、mutation、工具测试与门禁 |

新增模块后从权威来源核对并更新本表、registry 与项目卡；不要记录临时检出状态。
