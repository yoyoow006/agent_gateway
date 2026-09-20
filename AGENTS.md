# AI 编程助手 · Codex 工作流总纲

本仓库按风险选择流程，先分类再行动。验证证据始终保留，流程产物与审查层数随风险调整。

## 风险路由

开始修改前，用一句话向用户说明模式与理由。未知或边界不清时至少升级到标准；用户可要求更严格，不得把严格任务降级。

| 模式 | 可观察条件 | 路径 |
|---|---|---|
| 快速 | 仅维护已有事实的 Markdown、纯文本、注释或机械格式；不改变运行时、API/Schema、配置语义、安全合规、工作流治理或发布行为 | 探索事实 → 直接修改 → 针对性验证 → 汇报 |
| 标准 | 低到中风险运行时代码变更，且不命中严格条件 | Open 一次产出可执行三件套+条件 design → 一次确认 → Build → 综合 Verify → Archive |
| 严格 | 权限认证、资金账务、删除/迁移、数据库 Schema、并发一致性、跨服务或公开运行时契约、工作流治理、破坏性操作、大范围重构 | Open → Design → Build → Verify → Archive 完整门禁 |

> 标准三件套即 `proposal.md`、delta `spec.md`、`tasks.md`；存在跨模块取舍、新依赖、状态模型、重要替代方案或无法在 proposal/tasks 中清晰表达的架构决策时增加 `design.md`。

### 快速模式

- 用户清晰的修改请求即实施授权；不创建 OpenSpec、计划、feature/worktree，不用 TDD 或子代理，不归档。
- 先从代码或其他权威来源核对事实，再修改并现跑格式、链接、示例、结构或事实来源校验，最后检查 diff。
- 默认不提交或合并 Git。执行中发现必须改变行为或契约，立即停止并升级。

### 标准模式

- Open 一次创建 `proposal.md`、delta `spec.md`、含精确步骤和命令的 `tasks.md`；design 由可观察条件触发（见风险路由表注），仅当跨模块取舍、新依赖、状态模型、重要替代方案或架构决策无法在 proposal/tasks 中清晰表达时才生成；不得为无独立设计决策的变更制造空壳 design。proposal 记录`模式: 标准`并直接置为`待确认计划`。
- 唯一一次实施前确认同时覆盖范围、（如有）设计、任务与明示的本地整合策略；确认后连续推进，除非范围变化、验证失败、出现争议或需要外部副作用授权。
- 状态：`待确认计划 → 构建中 → 待验证 → 待归档 → 已归档`。标准模式不得生成独立 plan。
- 默认 feature 分支；仅脏工作区、并行实现、高冲突或用户要求时用 worktree。小任务主会话直执，完成后至多一次全 diff 综合审查。

### 严格模式

- 第二次实施前确认由不可逆风险触发：四件套确认后才写独立计划；当计划包含权限认证、资金账务、数据库 Schema/迁移、数据删除、破坏性动作、外部副作用，或引入规范未覆盖的选择/假设/依赖/范围时，必须将状态置为`待确认计划`并等待用户确认；不命中上述硬风险集合且未引入新选择的严格计划，可在完成计划自审后连续进入 Build。
- 状态：`草稿 → 待确认规范 → 设计中 → 待确认计划 → 构建中 → 待验证 → 待归档 → 已归档`。
- 默认隔离 worktree、运行时 TDD、任务级审查及 Verify 双阶段独立审查。

## 状态真源

- 活跃变更：`openspec/changes/<变更名>/`；状态看 `proposal.md`，进度看 `tasks.md`。
- 任一未归档状态可经用户明确决定转`已取消`终态：proposal 记`取消原因:`并移入 `openspec/archive/`，不合并 delta、不恢复；助手可建议、不得自行取消。
- 标准/严格模式开始工作前读取上述文件与 `.ai/rules/index.md`，从断点继续，不重做完成项。
- `openspec/` 是 `.claude` 与 `.codex` 共享的唯一数据层；两套工作流不得同时推进同一变更。快速模式没有 OpenSpec 状态。

## 技能路由

进入阶段前完整读取对应技能：

| 任务 | 技能 |
|---|---|
| 新需求与风险分类 | `open` |
| 严格模式独立计划 | `design` |
| 已确认方案的实现 | `build` |
| 构建后审查与终验 | `verify` |
| 规格、知识和分支收尾 | `archive` |

按触发条件加载 tdd、systematic-debugging、verification、git-worktrees、code-review、subagent-driven、parallel-agents、writing-skills；未命中条件不加载。

宿主插件技能与仓库技能职责重叠时，以仓库技能为准；插件技能仅在仓库技能未覆盖的空缺时补充使用。

## 模式门禁

- **Q1 快速**：没有权威事实核对、针对性验证和 diff 检查，不得声称完成。
- **S1 标准**：实际 OpenSpec 产物未获一次明确确认，不得实现；tasks/测试未全绿或综合审查仍有 Critical/Important，不得归档。
- **G1–G4 严格**：四件套未确认不写计划；命中硬风险的计划未确认不实现；tasks/测试/证据不全不交 Verify；任务级和双阶段审查未通过不归档。

## 共享审查与有限角色

- 标准/严格完整审查执行 `.ai/rules/review.md`：主会话 freeze，reviewer 读取前和结论前各运行 `review_manifest.py verify`；任一 `STALE` 立即停止。快速模式不创建 manifest、不派角色代理。
- 标准仍至多一次综合审查；严格仍为任务级审查加 Verify 两个独立关注面，不因 manifest 增加完整审查层数。
- finding 记录证据、影响、处置、未验证范围和残余风险；已确认范围内最小修复沿用授权，扩大行为、依赖、迁移或外部副作用才重新确认。
- explorer、reviewer、test worker 仅在既有独立边界命中时使用共享 `.ai/prompts/agents/`；标准小任务继续主会话直执。

### 审查单元

任务级审查对象为高风险实现单元（并发不变量、权限边界、跨服务契约、资金/账务不可逆性、Schema/迁移/数据删除边界等）；多个 checklist 共享同一不变量时合并为一次审查，冻结范围时按不变量名聚合。Verify 双阶段（规格符合性、代码质量）保持独立关注面，不因审查单元合并而省略。

### 归档门禁

Archive 默认运行 `bash scripts/validate-workflow.sh --archive-light`；当最新有效 Verify 完整门禁通过后发生了工作流可执行文件、助手入口、技能语义、契约测试或治理规格变化，Archive 自动升级为 `bash scripts/validate-workflow.sh --require-openspec`；判定依据为 Verify 后 diff 分类（按工作流可执行文件/助手入口/技能语义/契约测试/治理规格分组），禁止人工口头跳过完整门禁。严格模式 OpenSpec 与仓库自带必需测试不得 SKIP，CLI 不可用时 required 门禁必须非零。

### .ai-local 缓存清理

OpenSpec 归档文件写入最终 manifest ID、comparison base、finding 状态、未验证范围、残余风险后，可删除对应 `.ai-local/reviews/<change>/`（仅该子路径）；活跃 review、STALE manifest、未持久化最终证据的目录不得自动清理；不得递归删除整个 `.ai-local`；不得把缓存清理当作关闭 finding 的手段。

## 共享底线

- 保护用户数据与未提交修改；目标重叠时先隔离、绕开或请求决定。
- 外部或破坏性动作（推送、PR、强推、删除未合并工作等）必须有明确授权。
- 范围扩大或风险升级时暂停，更新事实源并重新确认；不确定时不猜。
- 审查意见先验证正确性再处理；完成声明前必须现跑与风险相称的验证并读取退出结果。
- TDD 用于运行时行为变更和缺陷修复；纯文档、注释、知识库和流程文本使用内容契约、解析、格式和 diff 校验。
- 提交按可独立回滚的职责单元组织，不按 checklist 机械拆分。

## Codex 适配与知识库

- 工具映射见 `.codex/README.md`；待办用 `update_plan`，文件修改用 `apply_patch`，命令执行用 `exec_command`。
- Open 按 `.ai/rules/index.md` 路由；解决新坑立即写 `.ai/memory/<模块>.md`；Archive 合并主规格并完成 memory/kb/rules 沉淀。
