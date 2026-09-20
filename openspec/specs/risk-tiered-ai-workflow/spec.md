# 风险分级 AI 工作流规范

## Purpose

规定仓库 AI 助手如何按变更风险选择快速、标准或严格流程，使流程成本与风险相称，同时保留验证、用户修改保护和外部操作授权底线。

## Requirements

### Requirement: 工作流必须先按风险分流

系统 SHALL 在开始变更前把任务归入“快速、标准、严格”之一，并向用户简短说明所选模式及理由；无法可靠分类时 SHALL 自动升级到更严格的模式。用户可以要求升级模式，但不得把满足严格条件的任务降级。

#### Scenario: 任务边界明确且只维护现有事实

- **WHEN** 变更仅涉及 Markdown、纯文本、代码注释或机械格式
- **AND** 不新增或改变运行时行为、API/Schema、配置语义、安全/合规规则、工作流治理或发布行为
- **THEN** 系统选择快速模式

#### Scenario: 任务风险无法确定

- **WHEN** 系统无法确认变更是否影响运行时行为或公开契约
- **THEN** 系统不得猜测为快速模式，并至少升级到标准模式完成探索

#### Scenario: 任务满足严格条件

- **WHEN** 变更涉及权限认证、资金账务、数据删除或迁移、数据库 Schema、并发一致性、跨服务或公开运行时契约、工作流治理、破坏性操作、大范围重构中的任一项
- **THEN** 系统选择严格模式并执行完整五阶段门禁

### Requirement: Open 必须先建立需求共识

Open SHALL 先从代码、OpenSpec、共享知识层和用户输入核对权威事实，再决定是否需要用户决策；代码或仓库可回答的问题 SHALL NOT 被转交给用户。当目标、范围、术语、约束、验收标准或授权仍无法由权威事实消除时，系统 SHALL 提出一轮编号的决策问题，为每个问题给出证据、选项、推荐答案和选择影响，并等待用户回答后再继续生成规格或实施。

#### Scenario: 用户目标模糊但代码事实可查

- **WHEN** 用户只提出“优化搜索体验”而没有说明目标页面、问题指标或验收标准
- **THEN** 系统先读取相关实现、路由、测试和既有文档并列出可核对事实
- **AND** 只向用户询问会改变目标、范围、设计、验证或授权的决策问题
- **AND** 每个问题有编号、证据、选项、推荐答案和影响
- **AND** 系统在收到回答前不猜测需求或生成待确认四件套

#### Scenario: 未知信息可由仓库回答

- **WHEN** 需求理解缺少当前接口、数据结构、调用方或既有行为
- **AND** 这些信息存在于代码、测试、OpenSpec 或共享知识层
- **THEN** 系统自行查证并引用来源
- **AND** 不把该事实作为问题询问用户

#### Scenario: 用户术语与权威来源冲突

- **WHEN** 用户使用的业务词存在过载、同义或与既有规格不一致的含义
- **THEN** 系统引用可观察来源指出差异
- **AND** 请求用户确定本变更使用的 canonical term
- **AND** 后续规格、设计和任务使用该确定后的术语

#### Scenario: 回答解锁新的必要决策

- **WHEN** 用户对一轮问题的回答使另一个此前无法诚实提出的问题变为必要
- **THEN** 系统在下一轮单独提出该问题
- **AND** 不把依赖未回答问题的猜测混入当前轮

#### Scenario: 请求已经清晰

- **WHEN** 用户请求的目标、边界和验收事实可由权威来源完整核对
- **AND** 不存在影响范围或风险的未决决策
- **THEN** 系统按既有快速、标准或严格路径继续
- **AND** 不追加仪式化访谈

### Requirement: 已确认需求理解必须进入 OpenSpec 状态真源

用户回答形成的范围、非目标、约束、术语和验收标准 SHALL 落入当前变更的 `proposal.md`、delta `spec.md`、`design.md` 或 `tasks.md` 中最合适的位置；标准/严格模式的实施前确认 SHALL 覆盖这些已确认理解。系统 SHALL NOT 通过新增 `CONTEXT.md`、ADR 或其他平行文件来维护本次变更需求状态。

#### Scenario: 用户敲定验收边界

- **WHEN** 用户回答明确了目标用户、非目标、数值边界或失败处理
- **THEN** Open 将该回答转化为 proposal 的事实与范围、spec 的 Requirement/Scenario、design 的边界或 tasks 的验证步骤
- **AND** 后续实施与审查以这些文件为准

#### Scenario: 后续回答改变已起草范围

- **WHEN** 用户在确认前补充或推翻既有理解
- **THEN** 系统先更新四件套中的对应事实
- **AND** 请求确认时呈现修订后的范围，而不是沿用旧草案

### Requirement: 快速模式必须以最小流程完成文档维护

快速模式 SHALL 使用“探索事实 → 直接修改 → 针对性验证 → 汇报”的单段流程。用户清晰提出的修改请求 SHALL 视为实施授权，不再创建四件套、实现计划或额外确认点；默认不创建 feature/worktree、不应用 TDD、不派子代理、不执行 OpenSpec 归档，也不自动提交或合并 Git，除非用户明确要求。

#### Scenario: 根据现有 Controller 补充前端文档

- **WHEN** 用户要求把已经实现且未改变的 HTTP 接口补入前端对接文档
- **AND** 系统能从当前 Controller、DTO/VO 与 Service/Mapper 核对全部事实
- **THEN** 系统直接修改文档并运行路径、字段、示例与格式校验
- **AND** 不要求用户再次确认规格或计划

#### Scenario: 快速任务执行中发现契约不一致

- **WHEN** 文档目标与运行时实现存在冲突，或者必须改变代码/API 才能满足需求
- **THEN** 系统立即停止快速模式、保留已发现证据并升级到标准或严格模式
- **AND** 未经新模式要求的确认不得继续扩大修改范围

#### Scenario: 快速任务完成声明

- **WHEN** 系统准备声明快速任务完成
- **THEN** 必须现跑与变更相关的格式、链接、示例或事实来源校验并核对 diff
- **AND** 无新鲜验证证据时不得声称完成

### Requirement: 标准模式只保留一个实施前确认点

标准模式 SHALL 由 Open 一次性产出 `proposal.md`、delta `spec.md` 和包含可执行步骤的 `tasks.md`。仅当变更存在跨模块取舍、新依赖、状态模型、重要替代方案或无法在 proposal/tasks 中清晰表达的架构决策时，才 SHALL 增加 `design.md`；不得为无独立设计决策的变更制造空壳 design。标准模式不得生成独立 `openspec/plan/<变更名>.md`。用户确认这些产物时 SHALL 同时确认范围、设计（如有）、任务与建议的本地分支整合策略，确认后系统可连续执行 Build、综合 Verify 和 Archive，除非范围变化、验证失败、出现争议或需要外部副作用授权。

#### Scenario: 标准任务没有独立架构决策

- **WHEN** 低到中风险变更只需在既有结构内修改一个职责边界
- **AND** 不引入新依赖、状态模型或跨模块取舍
- **THEN** Open 创建 proposal、delta spec 和可执行 tasks
- **AND** 不创建 design.md 或独立 plan
- **AND** 用户只进行一次实施前确认

#### Scenario: 标准任务存在重要设计取舍

- **WHEN** 标准变更存在两个以上有实质影响的实现方案或跨模块边界调整
- **THEN** Open 增加 design.md 记录决策、替代方案和风险
- **AND** 唯一一次实施前确认覆盖全部实际产物

#### Scenario: 标准模式确认后连续执行

- **WHEN** 用户确认标准模式产物及建议的本地整合策略
- **THEN** 系统无需在 Build 完成和本地 Archive 合并前重复请求确认
- **AND** 推送、创建 PR、强推、删除未合并工作或其他外部/破坏性动作仍须遵守独立授权规则

#### Scenario: 标准模式状态推进

- **WHEN** Open 已完成标准模式实际需要的产物并准备请求唯一一次实施前确认
- **THEN** 状态直接置为 `待确认计划`
- **AND** 确认后依次推进 `构建中 → 待验证 → 待归档 → 已归档`
- **AND** 不经过严格模式专用的 `待确认规范` 与 `设计中`

#### Scenario: 普通低到中风险代码变更

- **WHEN** 变更影响运行时代码但不满足严格模式条件
- **THEN** Open 将范围、行为契约和逐步实施/验证命令写入必要产物
- **AND** 只有存在独立架构决策时才增加 design.md
- **AND** 用户只需进行一次实施前确认

#### Scenario: 标准模式范围发生变化

- **WHEN** 构建或审查发现必须新增已确认范围之外的行为、依赖或数据变更
- **THEN** 系统暂停实施、更新当前变更产物并请求用户重新确认

### Requirement: 标准模式必须消除重复审查

标准模式 SHALL 由主会话直接执行小型任务；仅在任务确实可独立并行、规模较大或需要隔离判断时使用子代理。Build 不再默认对每个任务同时执行规格与质量双审；完成后执行一次覆盖整个 diff 的综合审查，同时检查规格符合性、正确性、安全与可维护性。严格模式继续执行任务级审查与 Verify 双阶段独立审查。

#### Scenario: 单文件或单一职责的小型代码任务

- **WHEN** 标准模式只有一个小型、边界清晰的实现任务
- **THEN** 主会话直接按测试与验证步骤执行
- **AND** 不创建任务简报、实现子代理、任务审查子代理或全分支双审组合

#### Scenario: 标准模式完成构建

- **WHEN** 所有 tasks 已完成且相关测试全绿
- **THEN** 系统派发至多一个独立综合审查者或由独立审查能力完成一次全 diff 审查
- **AND** Critical/Important 归零并通过终验后即可归档

#### Scenario: 严格模式审查

- **WHEN** 任务被分类为严格模式
- **THEN** 继续使用任务级审查、规格符合性审查和质量审查，不因标准模式简化而降低门禁

### Requirement: 分支、worktree、TDD 和提交粒度必须按风险启用

快速模式默认沿用当前工作区且不自动提交；标准模式默认使用 feature 分支，但仅在工作区脏、存在并行工作、冲突概率高或用户要求隔离时创建 worktree；严格模式默认使用隔离 worktree。TDD 仅适用于运行时行为变更和缺陷修复，不适用于纯文档、注释、知识库和流程说明。提交 SHALL 以可独立回滚的职责单元为粒度，不再要求每个 checklist 条目至少一次提交。

#### Scenario: 标准模式工作区干净且无并行冲突

- **WHEN** 当前工作区干净、没有其他活跃实现且变更范围集中
- **THEN** 系统可直接创建 feature 分支就地工作，不额外询问或创建 worktree

#### Scenario: 标准模式存在工作区冲突风险

- **WHEN** 当前工作区包含用户未提交修改、另一个活跃变更或并行实现
- **THEN** 系统使用隔离 worktree 或暂停请求用户选择，不得覆盖或混入现有修改

#### Scenario: 文档变更不适用 TDD

- **WHEN** 修改不产生可执行的运行时行为
- **THEN** 系统使用内容契约、结构、示例解析或 diff 检查验证，不制造形式化的红—绿 TDD

### Requirement: 所有模式共享不可削弱的底线

所有模式 SHALL 保留用户数据与既有修改保护、破坏性及外部副作用授权、范围变化重新确认、审查意见先验证、以及完成声明前的新鲜验证证据。流程简化 SHALL NOT 被解释为允许猜测需求、跳过失败处理、覆盖用户修改或在验证失败时继续归档。

#### Scenario: 快速模式遇到用户未提交修改

- **WHEN** 目标文件与用户现有未提交修改重叠
- **THEN** 系统不得覆盖该修改，并应先隔离、绕开或请求用户决定

#### Scenario: 任一模式准备完成

- **WHEN** 系统准备声明变更完成、已修复、可合并或已归档
- **THEN** 必须运行与风险和变更范围相称的完整验证并读取退出结果

### Requirement: 标准/严格变更可以经用户明确决定取消

系统 SHALL 允许处于`已归档`之前任一状态的活跃变更经用户明确决定进入`已取消`终态。助手可以建议取消，但 SHALL NOT 在没有用户明确决定时把任何变更置为`已取消`。取消时系统 SHALL 把 proposal 的`状态:`置为`已取消`、记录一句取消原因，并把变更目录整体移入 `openspec/archive/`；该变更的 delta 规格 SHALL NOT 合并进 `openspec/specs/` 主规格。已取消变更所在 feature 分支、worktree 与未提交修改的处置 SHALL 由用户在取消时明确指示；删除未合并工作仍 SHALL 遵守独立授权规则。

#### Scenario: 用户在实施前确认时拒绝方案并要求取消

- **WHEN** 变更处于`待确认规范`或`待确认计划`，用户明确表示不实施该方案并要求取消
- **THEN** 系统把 proposal 状态置为`已取消`并记录取消原因
- **AND** 变更目录移入 `openspec/archive/`，delta 不合并进主规格
- **AND** 系统不创建 feature 分支、worktree 或提交实现代码

#### Scenario: 构建中途取消

- **WHEN** 变更处于`构建中`或`待验证`，用户明确决定取消
- **THEN** 系统把 proposal 状态置为`已取消`、记录原因并移入 `openspec/archive/`
- **AND** feature 分支、worktree 与未提交修改的去留等待用户明确指示，不自动删除
- **AND** 后续不再对该变更执行 Verify、Archive 合并或知识沉淀

#### Scenario: 断点恢复不得复活已取消变更

- **WHEN** 后续会话在 `openspec/changes/` 恢复断点或用户新提同类需求
- **THEN** 已取消变更只作为 `openspec/archive/` 中的历史记录存在，不被恢复、不合并 delta
- **AND** 新需求按新变更目录重新分类进入

#### Scenario: 取消不破坏 OpenSpec 严格门禁

- **WHEN** 已取消变更（可能含未完成四件套）已移入 `openspec/archive/`
- **THEN** `openspec validate --all --strict` 与 `bash scripts/validate-workflow.sh --require-openspec` 仍按既有规则通过或失败，不因归档目录中的取消记录而新增失败项

#### Scenario: 助手不得自行取消

- **WHEN** 助手认为变更应当放弃但用户未明确决定
- **THEN** 助手只提出取消建议及理由，等待用户决定
- **AND** 不把`状态:`置为`已取消`，不移动变更目录

### Requirement: 高风险流程路径必须有施压场景

工作流行为契约 SHALL 以施压场景覆盖以下高风险路径，场景使用与其他场景相同的结构（共同要求＋逐字场景文本＋可判通过条件）：严格实现前的分支与 worktree 原子顺序、归档的 delta 合并与用户取消处置、审查中途 manifest 陈旧的处理。场景文件 SHALL 与镜像资产副本保持一致，结构校验 SHALL 守护这些场景的存在与关键通过条件。安装目标已有助手入口的冲突处置场景（场景 I）SHALL 与 `codex-workflow-target-installation` 规格的冲突入口条款语义一致：通过条件要求先 dry-run 或等价完整预检，差异入口按保留再替换处置（重命名 `AGENTS.pre-codex-workflow.md`＋SHA-256 校验一致＋安装器显式清单创建新入口），不得要求盲覆盖、输出既有正文或建议 `--force`；通过条件 SHALL 要求说明可由维护者选择在临时空目录生成模板人工整合（替代方案），其措辞 SHALL 为行为性（可按是否主动说明判定）。场景契约与安装规格任一侧修改冲突处置语义时，SHALL 以独立变更同步另一侧，不得单方面漂移。工作流技能正文变更的 Verify 终验 SHALL 重跑 `scripts/workflow-pressure-scenarios.md` 中与该技能行为契约相关的场景，相关性无法判定时重跑全部场景；重跑结果 SHALL 记录于该变更的 OpenSpec 目录。

#### Scenario: 新增高风险场景

- **WHEN** 维护者查看 `scripts/workflow-pressure-scenarios.md`
- **THEN** 存在覆盖 worktree 原子顺序、归档合并与取消、manifest STALE 的三个场景
- **AND** 每个场景的通过条件可判（引用明确的禁止动作与正确动作）
- **AND** 结构校验的 压力契约 检查包含这三个场景的标识与关键条件词

#### Scenario: 场景 I 与安装规格语义一致

- **WHEN** 维护者对照场景 I 通过条件与 `codex-workflow-target-installation` 规格的冲突入口条款
- **THEN** 两者对差异入口的处置同为保留再替换（备份＋SHA-256 校验＋安装器显式清单）
- **AND** 场景 I 不含冲突关闭零写入要求，且保留预检、不输出既有正文、不建议 `--force` 的条件
- **AND** 场景 I 的替代方案条款为行为性措辞，与安装规格的上报极简性条款同向

#### Scenario: 场景 I 关键条件被守护

- **WHEN** 向场景 I 注入移除保留再替换关键条件（备份路径、SHA-256 校验或不输出既有正文）的修改
- **THEN** 结构校验返回非零并指出该场景

#### Scenario: 单侧漂移被阻止

- **WHEN** 一个变更只修改场景 I 或 `codex-workflow-target-installation` 规格其中一侧的冲突处置语义
- **THEN** 其 Verify 对照本条款发现另一侧未同步，变更不得进入待归档

#### Scenario: 场景与资产副本漂移

- **WHEN** 实体场景文件与 `scripts/ai-workflow-assets/shared/scripts/` 下副本内容不一致
- **THEN** 字节一致性核对失败并要求同步

#### Scenario: 技能正文变更重跑场景

- **WHEN** 一个变更修改了任一工作流技能的正文
- **THEN** 其 Verify 终验现跑相关施压场景并逐场景记录 PASS/FAIL
- **AND** 任一场景 FAIL 时不得进入待归档，须回退该处措辞或按新事实重新确认范围

### Requirement: Codex 与 Claude 工作流必须保持一致

风险分流、模式边界、状态推进（含取消路径）、硬门禁和双套共存技能的语义 SHALL 同步体现在 `AGENTS.md`、`CLAUDE.md`、`.codex/skills/`、`.claude/skills/`、适配说明与校验脚本中。结构校验 SHALL 对全部双套共有的阶段与支撑技能执行镜像检查；镜像比对 SHALL 仅豁免声明的助手适配差异（技能目录路径前缀改写与显式标注的助手适配注记行），其余任何语义分叉 SHALL 使校验失败。结构校验 SHALL 能发现两套模式定义缺失、快速模式被错误要求四件套、或标准模式仍被强制独立计划/双审的回归。

#### Scenario: 工作流镜像发生漂移

- **WHEN** Codex 与 Claude 任一侧缺少快速/标准/严格模式或关键门禁定义
- **THEN** `scripts/validate-workflow.sh` 返回非零并指出缺失项

#### Scenario: 未受镜像检查的技能语义分叉

- **WHEN** 任一双套共存技能（含支撑技能）在两套运行时之间出现路径前缀与适配注记之外的语义差异
- **THEN** `scripts/validate-workflow.sh` 返回非零并指出该技能

#### Scenario: 旧规则重新强制统一重流程

- **WHEN** 总纲或阶段技能再次声明所有文档变更必须创建四件套、所有标准任务必须独立 Design 确认、或所有任务必须双阶段审查
- **THEN** 结构校验返回非零，阻止流程简化被后续修改悄悄回退

### Requirement: 验证门禁按阶段分层

公共校验入口 SHALL 提供 `--fast` 模式，仅运行内部 core 结构校验并跳过顶层契约套件，退出码与汇总口径与全量模式一致；未传参默认 SHALL 保持全量行为不变。`--fast` 模式 MAY 对指定的重检查使用输入指纹等价缓存：当检查命令、core 校验器内容与声明输入文件集合的指纹与上次实际执行的 PASS 记录一致时，MAY 沿用该结果，并 MUST 在该检查行内透明标注指纹、上次实际执行时间与“沿用”来源；任一输入、命令或实现变化，缓存缺失、损坏或格式非法，或上次结果非 PASS 时 SHALL 重新实际执行。`--require-openspec` 与默认全量模式 SHALL NOT 读取或写入该缓存。标准模式 Verify 终验 SHALL 运行 `--fast` 加变更相关目标与回归测试；严格模式 Verify SHALL 运行 `--require-openspec`。Archive SHALL 按“Archive 验证必须按 Verify 后变化选择层级”执行轻量或升级后的 required 门禁，而不是按模式无条件重复完整门禁。变更若触及工作流入口、技能、校验器或安装资产，其标准模式 Verify 终验 SHALL 亦使用全量门禁。

#### Scenario: 标准模式内容变更的 Verify 终验

- **WHEN** 一个未触及工作流治理资产的标准变更到达 Verify 终验
- **THEN** 主会话现跑 `bash scripts/validate-workflow.sh --fast` 与目标/回归测试并读取退出结果
- **AND** Archive 按 Verify 后 diff 分类运行轻量或升级后的 required 门禁，FAIL=0 才可声称归档完成

#### Scenario: fast 暖缓存命中

- **WHEN** `--fast` 模式下某重检查的命令、输入文件与 core 实现均与上次实际执行的 PASS 记录一致
- **THEN** 该检查输出 PASS 并透明标注指纹、上次实际执行时间与沿用来源
- **AND** 顶层汇总仍按 PASS 计数，不新增第四种检查状态

#### Scenario: 缓存输入变化

- **WHEN** 重检查任一输入文件、检查命令或 core 实现在上次 PASS 后发生变化
- **THEN** 该检查重新实际执行并按新结果更新或清除缓存

#### Scenario: 严格模式不得降级

- **WHEN** 严格模式变更进入 Verify
- **THEN** 门禁命令保持 `bash scripts/validate-workflow.sh --require-openspec`
- **AND** 不因任何分层或缓存能力改为 `--fast`，也不读取缓存结果
- **AND** 其 Archive 若发生 Verify 后治理语义变化，同样升级为 required 门禁

#### Scenario: 治理资产变更保持全量

- **WHEN** 标准模式变更修改了 CLAUDE/AGENTS、技能、校验器或安装资产
- **THEN** 其 Verify 终验运行全量门禁而非 `--fast`

### Requirement: 仓库技能优先于宿主插件技能

当宿主环境提供的插件技能与仓库技能职责重叠(如同名或同触发条件的 TDD、调试、审查类技能)时,助手 SHALL 以仓库技能(`.claude/skills/`、`.codex/skills/` 及其共享正文)为准;插件技能 SHALL 仅在仓库技能未覆盖的空缺时补充使用。该仲裁 SHALL 同步声明于 `CLAUDE.md` 与 `AGENTS.md`,并随安装资产的通用入口分发。

#### Scenario: 宿主插件提供同名 TDD 技能

- **WHEN** 宿主同时加载了仓库 tdd 技能与插件 TDD 技能
- **THEN** 助手按仓库 tdd 技能的红—绿—重构与适用边界执行
- **AND** 不因插件技能的存在产生第二套触发或仪式

#### Scenario: 仓库技能未覆盖的空缺

- **WHEN** 某任务无对应仓库技能而插件技能可用
- **THEN** 可以使用插件技能,并在汇报中注明所用技能来源

### Requirement: 工作流技能正文必须精炼且示例可迁移

工作流技能正文 SHALL 不含对未随仓库迁移的源文档的叙事性注记；技能示例 SHALL 引用本仓库实际技术栈（bash/python 安装器与校验器）或保持技术无关，不得以其他技术栈专有场景为主要示例。writing-skills 的 token 效率验证 SHALL 使用对中文正文可判的去空白字符口径，并 SHALL 遵守其为本仓库技能定义的同一结构与效率规则。结构校验 SHALL 守护：writing-skills、parallel-agents、systematic-debugging 双树正文迁移注记零残留，且字符口径验证命令与技能编辑场景重跑绑定句在 writing-skills 双树中存在。

#### Scenario: 技能正文无迁移叙事残留

- **WHEN** 维护者查看 writing-skills、parallel-agents、systematic-debugging 的双树正文
- **THEN** 不出现"未随本仓库迁移"字样
- **AND** 示例不引用 TypeScript 测试文件名或 macOS 专有签名/钥匙串命令

#### Scenario: 注入迁移注记必红

- **WHEN** 向任一受守护技能的任一侧正文注入一行"未随本仓库迁移"注记
- **THEN** 结构校验返回非零并指出该技能

#### Scenario: 字符口径可判中文

- **WHEN** writing-skills 的 token 效率验证命令运行于中文 SKILL.md
- **THEN** 输出为去空白字符数，可对照技能内登记的字符阈值判定

### Requirement: 严格模式的第二次确认必须由计划风险触发

严格模式 SHALL 在 Open 四件套获得规范确认后进入独立 Design。若独立计划包含权限或资金行为、数据库 Schema/迁移、数据删除、破坏性动作、外部副作用，或引入规范确认中没有覆盖的选择、假设、依赖及范围，系统 SHALL 将状态置为`待确认计划`并等待第二次实施前确认。若计划只把已确认设计展开为可执行步骤且不包含上述风险，系统 MAY 在完成计划自审后直接进入 Build；用户明确要求查看计划时仍 SHALL 等待确认。

#### Scenario: Schema 变更生成实施计划

- **WHEN** 严格计划包含数据库字段、索引、迁移或数据回填
- **THEN** 系统必须请求第二次计划确认
- **AND** 未确认前不得实现

#### Scenario: 工作流文本与测试按已确认方案展开

- **WHEN** 严格变更不涉及业务运行时、数据迁移或外部副作用
- **AND** 独立计划没有引入规范之外的新选择或范围
- **THEN** 系统完成计划自审后可以连续进入 Build
- **AND** 计划和状态推进仍须记录在 OpenSpec 真源中

#### Scenario: 用户要求查看所有严格计划

- **WHEN** 用户明确要求实施前查看独立计划
- **THEN** 系统将状态置为`待确认计划`并等待用户确认

### Requirement: 严格任务级审查必须绑定高风险实现单元

严格模式 SHALL 对权限、资金、Schema/迁移、数据删除、并发一致性、跨服务契约和不可逆副作用等高风险实现单元执行任务级审查；多个 checklist 若共同构成同一风险边界 SHALL 合并为一次审查。纯机械适配、文档同步和同一风险单元内的测试补充 SHALL NOT 各自触发完整任务级审查。Verify 仍 SHALL 保留规格符合性与代码质量两个独立关注面。

#### Scenario: 多个步骤共同实现一个并发边界

- **WHEN** 三个 checklist 分别增加条件写、竞争测试和日志，但共同保证同一个并发不变量
- **THEN** 系统按该并发不变量形成一个任务级审查单元
- **AND** 不机械执行三次完整审查

#### Scenario: 严格变更包含多个独立高风险边界

- **WHEN** 同一变更同时修改权限判定和数据库迁移
- **THEN** 两个风险边界分别获得任务级审查
- **AND** Verify 阶段继续执行两个独立关注面

### Requirement: Archive 验证必须按 Verify 后变化选择层级

Archive SHALL 对归档后的主规格、目录结构、OpenSpec、diff 与 Git 状态运行新鲜校验。若最新有效 Verify 完整门禁通过后仅发生 proposal 状态更新、delta 到主规格的语义保持合并、目录移动、索引和知识沉淀，Archive SHALL 使用归档专用轻量门禁，不重复顶层契约套件。若工作流可执行文件、助手入口、技能语义、契约测试或其他运行时/治理语义在 Verify 后发生变化，Archive SHALL 重新运行完整 required 门禁。

#### Scenario: 归档只产生机械状态变化

- **WHEN** Verify 后仅移动已审查文件、合并已审查 delta、更新状态与索引
- **THEN** Archive 运行 core、OpenSpec required、diff 和 Git 状态检查
- **AND** 不重复运行耗时的顶层契约套件

#### Scenario: 归档阶段修正工作流规则

- **WHEN** Archive 为通过校验而修改阶段技能、校验脚本或治理语义
- **THEN** 轻量门禁升级为完整 required 门禁
- **AND** 完整门禁失败时不得归档

### Requirement: 已归档审查缓存必须可安全清理

`.ai-local` SHALL 只保存本地运行时锁和活跃审查缓存，不作为变更状态或永久证据真源。Archive 在持久归档记录包含最终 manifest ID、逐仓 comparison base、finding 处置、未验证范围和残余风险后 MAY 删除该变更的本地 review 目录；活跃变更、STALE 待处理审查或尚未持久化最终证据的目录 SHALL NOT 被自动清理。

#### Scenario: 变更完成归档

- **WHEN** 最终审查身份与结论已经写入归档 tasks 或 review 记录
- **AND** 变更状态已置为已归档
- **THEN** 对应 `.ai-local/reviews/<change>/` 可作为缓存清理
- **AND** 删除缓存不影响 OpenSpec 状态、规格或审查可追溯性

#### Scenario: 变更仍在复审

- **WHEN** manifest 为 STALE、仍有开放 finding 或 proposal 尚未归档
- **THEN** 系统保留该 review 目录
- **AND** 不把缓存清理当作关闭 finding 的手段

### Requirement: 流程语义必须在双运行时文档和安装资产中保持一致

Codex 与 Claude 根入口、用户文档、共享知识总览、阶段技能和便携安装资产 SHALL 表达同一套风险分级语义：标准模式三件套加条件 design、严格模式第二次确认由硬风险触发、任务级审查按高风险实现单元组织，以及 Archive 按 Verify 后变化选择轻量或完整门禁。自动化回归 SHALL 覆盖双入口、关键文档和安装资产中的旧口径回归。

#### Scenario: Claude 入口保留旧标准口径

- **WHEN** Claude 根入口或其安装资产重新要求标准模式固定创建 design
- **THEN** 工作流契约测试失败
- **AND** 安装产物不得被判为与 Codex 语义一致

#### Scenario: 用户文档保留旧归档口径

- **WHEN** README、使用介绍或共享 overview 要求归档后一律重复完整契约套件
- **THEN** mutation/契约测试失败
- **AND** 文档必须同步 archive-light 与自动升级语义

#### Scenario: 安装资产保留旧严格确认口径

- **WHEN** 便携安装资产要求所有严格计划无条件二次确认
- **THEN** 工作流回归失败
- **AND** 安装器不得把旧语义发布到目标项目
