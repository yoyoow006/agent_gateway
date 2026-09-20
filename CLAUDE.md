# AI 编程助手 · 风险分级工作流总纲

本仓库先按风险分类，再选择与风险相称的流程。未知风险至少升级到标准；用户可要求升级，不得把严格任务降级。

## 三级模式

| 模式 | 条件 | 路径 |
|---|---|---|
| 快速 | 只维护已有事实的 Markdown、纯文本、注释或机械格式，且不影响运行时、API/Schema、配置语义、安全合规、工作流治理或发布 | 探索事实 → 直接修改 → 针对性验证 → 汇报 |
| 标准 | 不命中严格条件的低到中风险运行时代码变更 | Open 一次产出可执行三件套+条件 design → 一次确认 → Build → 综合 Verify → Archive |
| 严格 | 权限认证、资金账务、删除/迁移、数据库 Schema、并发一致性、跨服务或公开运行时契约、工作流治理、破坏性操作、大范围重构 | Open → Design → Build → Verify → Archive |

开始修改前向用户简短说明模式和理由。

> 标准三件套即 `proposal.md`、delta `spec.md`、`tasks.md`；存在跨模块取舍、新依赖、状态模型、重要替代方案或无法在 proposal/tasks 中清晰表达的架构决策时增加 `design.md`。

### 快速模式

- 清晰请求即修改授权；不创建 OpenSpec、计划、feature/worktree，不用 TDD、子代理或归档，默认不提交/合并。
- 核对权威事实后直接修改，现跑格式、链接、示例、结构或事实来源校验并检查 diff。
- 发现必须改变行为或契约时停止并升级。

### 标准模式

- Open 一次创建 proposal、delta spec、可执行 tasks；仅存在跨模块取舍、新依赖、状态模型、重要替代方案或无法在 proposal/tasks 中清晰表达的架构决策时增加 design。proposal 写`模式: 标准`并置为`待确认计划`。
- 一次确认覆盖范围、设计、任务和明示的本地整合策略；确认后连续执行，范围变化、失败、争议或外部副作用授权除外。
- 状态：`待确认计划 → 构建中 → 待验证 → 待归档 → 已归档`；不创建独立 `openspec/plan`。
- 默认 feature 分支；脏工作区、并行实现、高冲突或用户要求时才用 worktree。小任务主会话直执，至多一次全 diff 综合审查。

### 严格模式

- 四件套确认后才写独立计划；当计划包含权限认证、资金账务、数据库 Schema/迁移、数据删除、破坏性动作、外部副作用，或引入规范未覆盖的选择/假设/依赖/范围时，计划再次确认后才实现。未命中硬风险且未引入新选择的计划，可在自审后连续进入 Build。
- 状态：`草稿 → 待确认规范 → 设计中 → 待确认计划 → 构建中 → 待验证 → 待归档 → 已归档`。
- 默认隔离 worktree、运行时 TDD、任务级审查和 Verify 双阶段独立审查。

## 状态真源与技能

- 标准/严格变更状态看 `openspec/changes/<变更名>/proposal.md`，进度看 `tasks.md`；开始前读取它们和 `.ai/rules/index.md`，从断点继续。
- 任一未归档状态可经用户明确决定转`已取消`终态：proposal 记`取消原因:`并移入 `openspec/archive/`，不合并 delta、不恢复；助手可建议、不得自行取消。
- `openspec/` 是 `.claude` 与 `.codex` 的共享数据层；快速模式没有状态。
- 新需求/分类用 open；仅严格独立计划用 design；实现用 build；审查终验用 verify；收尾用 archive。
- 支撑技能只在 description 的触发条件命中时加载。
- 宿主插件技能与仓库技能职责重叠时，以仓库技能为准；插件技能仅在仓库技能未覆盖的空缺时补充使用。

## 模式门禁

- **快速**：权威事实核对、针对性验证、diff 检查缺一不可。
- **标准**：实际 OpenSpec 产物未获一次明确确认不得实现；tasks/测试未全绿或综合审查仍有 Critical/Important 不得归档。
- **严格 G1–G4**：四件套未确认不写计划；命中硬风险的计划未确认不实现；tasks/测试/证据不全不交 Verify；任务级和双阶段审查未通过不归档。

## 共享审查与有限角色

- 标准/严格完整审查执行 `.ai/rules/review.md`：主会话 freeze，reviewer 读取前和结论前各运行 `review_manifest.py verify`；任一 `STALE` 立即停止。快速模式不创建 manifest、不派角色代理。
- 标准仍至多一次综合审查；严格仍为任务级审查加 Verify 两个独立关注面，不因 manifest 增加完整审查层数。
- finding 记录证据、影响、处置、未验证范围和残余风险；已确认范围内最小修复沿用授权，扩大行为、依赖、迁移或外部副作用才重新确认。
- explorer、reviewer、test worker 仅在既有独立边界命中时使用共享 `.ai/prompts/agents/`；标准小任务继续主会话直执。

### 审查单元

任务级审查对象为高风险实现单元（并发不变量、权限边界、跨服务契约、资金/账务不可逆性、Schema/迁移/数据删除边界等）；多个 checklist 共享同一不变量时合并为一次审查。Verify 双阶段（规格符合性、代码质量）保持独立关注面。

### 归档门禁

Archive 默认运行 `bash scripts/validate-workflow.sh --archive-light`；当最新有效 Verify 完整门禁通过后发生了工作流可执行文件、助手入口、技能语义、契约测试或治理规格变化，Archive 自动升级为 `bash scripts/validate-workflow.sh --require-openspec`。严格模式 OpenSpec 与仓库自带必需测试不得 SKIP，CLI 不可用时 required 门禁必须非零。

### .ai-local 缓存清理

OpenSpec 归档文件写入最终 manifest ID、comparison base、finding 状态、未验证范围、残余风险后，可删除对应 `.ai-local/reviews/<change>/`（仅该子路径）；活跃 review、STALE manifest、未持久化最终证据的目录不得自动清理。

## 共享底线

- 保护用户数据和未提交修改；外部或破坏性动作必须明确授权。
- 范围扩大或风险升级时暂停、更新事实源并重新确认；不确定时不猜。
- 审查意见先验证再处理；完成声明前现跑与风险相称的验证并读取退出结果。
- TDD 适用于运行时行为与缺陷修复；纯文档、注释、知识库和流程文本使用内容、结构、示例、格式及 diff 校验。
- 提交按可独立回滚的职责单元组织。
- Open 按 `.ai/rules/index.md` 路由；新坑立即写 memory；Archive 完成主规格与 memory/kb/rules 沉淀。
