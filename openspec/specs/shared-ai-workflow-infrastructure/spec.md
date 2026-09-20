# 共享 AI 工作流基础设施规范

## Purpose

定义跨助手共享知识、确定性项目事实查询、审查范围冻结、有限代理角色和验证诚实性，使标准与严格流程可复核，同时保持快速模式轻量且不改变业务运行时。

## Requirements

### Requirement: 跨助手知识必须只有一个共享真源

系统 SHALL 使用根 `.ai/` 作为 Claude、Codex 等助手共享知识的唯一正文来源，并按 `kb`、`rules`、`memory`、`prompts`、`tools` 分离稳定事实、硬约束、跨会话经验、轻量角色契约和确定性工具。`.codex` 与 `.claude` SHALL 只保存助手特有适配或技能；旧 ai-kb 路径在迁移期只能提供指向共享真源的兼容说明，不得继续保存可独立演化的平行正文。

#### Scenario: 同一知识被两个助手读取

- **WHEN** Claude 与 Codex 需要读取同一项目事实、路由规则或踩坑记录
- **THEN** 两者解析到 `.ai/` 中同一份正文
- **AND** 校验器能够拒绝旧路径再次出现平行知识文件

#### Scenario: 助手存在工具特有行为

- **WHEN** 某项指令只适用于 Codex 或 Claude 的工具调用方式
- **THEN** 该适配保留在对应助手目录
- **AND** 不把工具特有配置提升为共享业务事实或共享治理正文

#### Scenario: 合并复活已删除的旧正文

- **WHEN** 任一合并、变基或整合决议使已删除的旧 ai-kb 平行正文重新出现在受版本控制的树中
- **THEN** 结构校验保持 FAIL，不得以"合并结果已存在"为由放行
- **AND** 处置时必须先把旧正文中共享层缺失的 memory 条目并入共享层，再删除旧路径文件
- **AND** 删除后主分支必须现跑完整工作流校验并全绿才可声称修复完成

### Requirement: 共享 memory 按模块文件维护

`.ai/memory/` SHALL 按知识模块分文件维护跨会话踩坑记录,条目格式与追加式维护规则不变;工作流治理与流程类条目 SHALL 位于 `workflow.md`,安装器契约类条目 SHALL 位于 `installer.md`,新增模块 SHALL 在出现首个条目时建同名模块文件。条目移动 SHALL NOT 改写正文;Archive 知识沉淀与日常"新坑立即写" SHALL 把条目写入对应模块文件。

#### Scenario: 安装器域新坑落盘

- **WHEN** 构建或审查发现属于便携安装器契约模块的新坑
- **THEN** 条目追加到 `.ai/memory/installer.md`,不写入 `workflow.md`
- **AND** 条目保持 `## 日期 · 来源变更` + 坑/解 固定格式

#### Scenario: 模块文件拆分保持逐字无损

- **WHEN** 维护者把既有条目在模块文件间移动
- **THEN** 每条正文(标题、坑、解)逐字不变
- **AND** 移动前后全部条目拼接比对无差异、总条目数不变

#### Scenario: 断点恢复读取模块 memory

- **WHEN** 助手按 `.ai/rules/index.md` 路由命中某模块并读取其踩坑记录
- **THEN** 对应模块文件存在且包含该模块历史条目
- **AND** 不要求读取与当前模块无关的 memory 文件

### Requirement: OpenSpec 必须继续作为变更状态唯一真源

共享知识迁移 SHALL NOT 创建第二套 tracked 变更状态。标准和严格模式的模式、状态、范围与进度仍 SHALL 由 `openspec/changes/<变更名>/proposal.md` 和 `tasks.md` 表示；快速模式仍不创建 OpenSpec 状态。

#### Scenario: 助手恢复严格变更

- **WHEN** 任一助手接手已有严格模式变更
- **THEN** 它先读取 OpenSpec proposal 状态和 tasks 进度，再按 `.ai` 路由补充知识
- **AND** 不从本地临时 manifest、prompt 或助手私有目录推断权威状态

### Requirement: 项目事实查询必须声明式、有界且只读

系统 SHALL 提供声明式项目 registry 和项目卡，并至少提供 `project-context`、`server-registry`、`workspace-search` 三类只读查询。查询 SHALL 只进入 registry 登记且本地已检出的路径，遵循 ignore，支持显式项目过滤和输出上限，不得自动 clone、联网、修改源码、生成知识正文或输出凭据与被忽略文件内容。

项目 registry SHALL 支持每个项目声明可选 `dependencies` 项目名列表和可选 `verification` 对象。`dependencies` SHALL 只引用同 registry 中存在的其他项目，并 SHALL NOT 包含自依赖、重复项或形成循环。`verification` MAY 声明非空单行 `build_command` 与 `test_command`、相对 `.ai/` 且实际存在的 `evidence` 文档，以及 40 或 64 位十六进制 `verified_commit`。`project-context` SHALL 输出直接依赖和验证入口，并在本地项目为 Git 工作树时把 `verified_commit` 与当前 HEAD 比较为 current 或 drifted，未检出或非 Git 时输出 unavailable；比较 SHALL 保持只读且 SHALL NOT 执行 registry 中声明的 build/test 命令。

#### Scenario: 已知项目名获取上下文

- **WHEN** 助手以登记项目名调用 `project-context`
- **THEN** 工具返回项目路径、构建类型、项目卡、入口、直接依赖、验证入口、证据位置和可用状态的有界摘要
- **AND** 项目未检出时明确报告缺失，不尝试联网补齐

#### Scenario: 已知 server 名定位实现

- **WHEN** 助手以 server 名调用 `server-registry`
- **THEN** 工具在登记项目内返回唯一项目、模块和应用入口，或者明确返回零匹配/歧义
- **AND** 不要求无界扫描整个工作区

#### Scenario: 搜索跨项目消费者

- **WHEN** 助手以符号、API 或消息名调用 `workspace-search`
- **THEN** 工具只搜索显式选择或 registry 登记的已检出项目并遵守结果上限
- **AND** 截断时返回可继续分页或缩小范围的提示

#### Scenario: 查询试图越过登记边界

- **WHEN** 参数指向未登记路径、被忽略内容或要求写入知识正文
- **THEN** 工具拒绝请求并返回非零

#### Scenario: 输出多仓直接依赖

- **WHEN** registry 中 alpha 声明依赖 beta，且 beta 也是登记项目
- **THEN** `project-context --project alpha` 输出 beta 为 alpha 的声明依赖
- **AND** 工具不递归声称传递依赖真实存在

#### Scenario: 依赖图非法

- **WHEN** dependencies 引用未登记项目、自身、重复项目名或形成循环
- **THEN** registry 解析以输入错误拒绝
- **AND** 不执行任何查询输出

#### Scenario: 验证入口只被展示

- **WHEN** registry 声明 build_command 和 test_command
- **THEN** `project-context` 输出这些命令作为目标项目验证入口
- **AND** 工具不执行命令、不写入项目、不产生外部副作用

#### Scenario: 已验证 commit 与当前 HEAD 一致

- **WHEN** 目标项目是 Git 工作树且 registry 的 verified_commit 等于当前 HEAD
- **THEN** `project-context` 输出 current
- **AND** 该状态只表示验证基线 commit 一致，不表示 dirty worktree 或当前任务已经通过

#### Scenario: 已验证 commit 漂移或不可用

- **WHEN** verified_commit 不等于当前 HEAD、项目未检出、项目不是 Git 工作树或 Git 命令不可用
- **THEN** `project-context` 分别输出 drifted 或 unavailable
- **AND** 不得沿用该证据声称当前代码通过

#### Scenario: Git 元数据或间接引用越界

- **WHEN** 项目 `.git` 符号链接、gitdir 文件、commondir、alternate object store 或 Git 元数据内部 symlink 解析到声明 workspace 外
- **THEN** `project-context` 与 `workspace-search` 在输出结果前以输入错误拒绝
- **AND** 不得读取外部 Git 元数据或输出外部 HEAD

#### Scenario: Git 配置不得引入命令执行

- **WHEN** 项目 Git config 或其 include 声明 fsmonitor、hooks、外部 attributes/excludes 或 pager 等可执行/可扩展配置
- **THEN** 事实工具以安全命令行配置覆盖这些执行路径
- **AND** 查询过程中不得执行 registry/Git config 声明的脚本或产生外部副作用

#### Scenario: 验证声明非法

- **WHEN** verification 命令为空或多行、evidence 是绝对路径、包含 `..`、指向 `.ai` 边界外或文件不存在，或 verified_commit 不是 40/64 位十六进制
- **THEN** registry 解析以输入错误拒绝

### Requirement: 业务词上下文路由必须保持声明式且有界

项目 registry SHALL 支持每个项目声明可选 `business_terms` 列表；每条业务词 SHALL 包含非空单行 `term`、非空 `source_paths` 列表，并 MAY 包含同义词列表。事实工具 SHALL 提供 `business-terms` 只读查询，支持重复项目过滤、默认包含匹配、显式精确匹配、分页和确定性输出。查询 SHALL NOT 联网、clone、写入项目或 registry、读取并输出匹配正文，或访问 registry 白名单和项目边界之外的路径。

#### Scenario: 按业务词路由项目

- **WHEN** 目标项目 registry 中某项目声明 term 为“合同”且存在同义词“租约”
- **AND** 助手以“租约”执行包含匹配
- **THEN** 查询返回该业务词、项目、项目卡和声明的相对源码路径
- **AND** 不输出源码正文或凭据内容

#### Scenario: 精确过滤与分页

- **WHEN** 查询使用 `--exact`、重复 `--project`、`--limit` 和 `--offset`
- **THEN** 仅所选项目的精确 term/synonym 匹配按确定性顺序返回
- **AND** 超出上限时在 stderr 输出下一页提示

#### Scenario: 非法声明被拒绝

- **WHEN** registry 中业务词、同义词或源码路径为空、多行、绝对路径或包含 `..`
- **THEN** 查询以输入错误退出
- **AND** 不回退到未声明白名单的路径扫描

#### Scenario: 查询保持只读

- **WHEN** 连续执行 `business-terms` 查询
- **THEN** workspace 文件内容与 mtime 均不改变

### Requirement: 项目验证证据复用必须可判定且不得削弱新鲜验证

系统 SHALL 提供通用验证证据文档契约，使目标项目能记录验证范围、命令、代码基线、环境类别、执行结果、日志或报告位置、覆盖边界和残余限制。证据 MAY 在代码 commit、相关输入、命令和运行环境等价时复用，用于避免无意义重复执行；但证据 SHALL NOT 成为 OpenSpec 之外的第二任务状态，SHALL NOT 把 NOT_RUN、UNKNOWN、失败或旧环境结果当作通过，也 SHALL NOT 替代当前变更完成前与风险相称的新鲜目标/回归验证。

#### Scenario: 等价输入复用

- **WHEN** 证据记录的 commit、相关输入、命令、环境类别和覆盖范围均与当前请求等价
- **AND** 用户请求仅为确认同一事实而非声明新的代码变更完成
- **THEN** 助手可引用该证据避免重复执行
- **AND** 必须披露证据位置、基线和未覆盖范围

#### Scenario: 代码或输入变化

- **WHEN** 当前项目 HEAD 不等于证据基线、相关输入变化、命令不同或环境类别不匹配
- **THEN** 该证据不可用于声称当前代码通过
- **AND** 助手必须执行与风险相称的新验证

#### Scenario: 证据状态未知

- **WHEN** 证据缺少退出结果、基线、命令、范围或环境身份
- **THEN** 该证据按 UNKNOWN 处理
- **AND** 不得在完成声明中计为通过

#### Scenario: 证据包含敏感内容

- **WHEN** 目标项目记录验证证据
- **THEN** 证据文档只保留脱敏命令、路径、摘要和必要标识
- **AND** 不保存凭据、生产数据、完整敏感日志或私有连接串

### Requirement: 标准和严格 Review 必须冻结并校验精确范围

标准与严格模式的完整审查 SHALL 在 reviewer 读取前生成本地 freeze manifest。manifest SHALL 对每个受影响 Git 仓记录仓路径、comparison base 输入及解析 SHA、HEAD、committed/staged/unstaged 范围、未忽略 untracked 路径和内容哈希。reviewer SHALL 在读取范围前和形成结论前执行只读 verify；任一记录发生变化时 SHALL 返回 `STALE` 并停止沿用该结论。

#### Scenario: 审查范围在结论前未变化

- **WHEN** freeze 后各仓 base、HEAD、tracked 差异、untracked 路径和哈希均未变化
- **THEN** verify 返回有效
- **AND** 审查结论引用 manifest 标识和逐仓范围

#### Scenario: 审查期间文件发生变化

- **WHEN** freeze 后任一纳入范围的文件、HEAD、base 或未跟踪文件集合发生变化
- **THEN** verify 返回 `STALE` 和变化摘要
- **AND** reviewer 不得把旧结论作为当前文件树的通过证据

#### Scenario: 快速模式维护现有事实

- **WHEN** 任务符合快速模式
- **THEN** 系统继续使用权威事实核对、针对性验证和完整 diff 检查
- **AND** 不强制创建 freeze manifest 或独立 reviewer

### Requirement: 审改必须使用有界 finding 台账和差异复审

完整审查 SHALL 一次列出有证据的阻断项、非阻断建议、未验证范围和残余风险。修复后 SHALL 只审上一有效 manifest 到当前 manifest 的差异、直接消费者以及继承的开放阻断项；不得无新证据重读未变化范围或追加原本可在完整审查发现的建议。风险接受 SHALL 只有用户在看到影响和残余风险后明确设置。

#### Scenario: 完整审查发现阻断项

- **WHEN** reviewer 发现影响正确性、安全、契约或可验证性的 Critical/Important 问题
- **THEN** 台账记录位置、证据、可观察影响、状态、最小修复方向和复验方式
- **AND** 问题在解决、被证明不成立或用户明确接受风险前阻断归档

#### Scenario: 已确认范围内修复 finding

- **WHEN** finding 的最小修复不扩大已确认需求范围
- **THEN** 按当前模式的既有授权规则实施并生成新的 delta manifest
- **AND** 复审限于该差异、直接消费者和继承问题

#### Scenario: 修复需要扩大范围

- **WHEN** finding 只能通过新增未确认行为、依赖、迁移或外部副作用解决
- **THEN** 系统暂停并更新事实源，请求用户重新确认

### Requirement: 代理角色必须有限、只在命中边界时启用

系统 SHALL 为 explorer、reviewer、test worker 提供最小共享角色契约和助手适配。角色 SHALL 分别限制为只读探索、独立只读审查、测试设计与验证；是否委派仍由现有风险、独立性和可验收边界决定。快速任务和标准小任务 SHALL NOT 因存在角色配置而默认使用子代理。

#### Scenario: 严格模式需要独立审查

- **WHEN** 严格模式进入任务级或 Verify 独立审查
- **THEN** reviewer 使用全新上下文、有效 manifest、规格和验证证据
- **AND** 不继承实现者的结论或修改工作树

#### Scenario: 标准小任务边界集中

- **WHEN** 标准任务规模小且没有可独立验收的并行边界
- **THEN** 主会话直接执行
- **AND** 不为使用角色配置而机械委派

### Requirement: 工作流验证必须显式区分通过、失败和未运行

工作流校验 SHALL 对每项检查输出 PASS、FAIL 或 SKIP；依赖不可用时不得静默省略。默认本地校验可以显式 SKIP 可选工具，但严格 Verify/Archive 使用的门禁 SHALL 能要求 OpenSpec CLI 和全部必需测试实际执行。`--fast` 的输入指纹等价缓存 SHALL 仅作为 PASS 的透明来源标注出现，SHALL NOT 新增第四种检查状态或改变顶层 PASS/FAIL/SKIP 字段口径；缓存记录解析失败、指纹输入缺失或命令漂移均 SHALL 按未命中处理并实际执行，不得假绿也不得假红；非 PASS 结果 SHALL NOT 写入缓存。共享知识结构、旧路径双真源、项目 registry、工具单测、查询边界、manifest stale 检测、助手适配与风险分级 mutation SHALL 进入自动化回归。行为回退注入 SHALL 额外覆盖：快速模式被要求自动提交或合并、标准模式被允许跳过唯一实施前确认直接实现、归档被允许跳过校验直接移动目录。镜像归一化豁免 SHALL 受登记数守卫：以受豁免前缀开头的适配注记行全仓数量 SHALL 等于显式登记值，超出即校验失败。适配注记及两套技能树（含安装器资产树，存在时）SHALL NOT 含已废弃工具名；Codex 侧 parallel-agents 技能 SHALL 含现行派发工具名。归档索引 SHALL 与归档目录严格一致：每个已归档变更目录在 `openspec/archive/README.md` 恰好一行索引，缺失、悬空或重复即校验失败。公共门禁 SHALL 在最终汇总行之前逐条列明契约套件内部的设计性跳过（测试与原因）并给出计数注解；该注解 SHALL NOT 改变顶层 PASS/FAIL/SKIP 字段语义，汇总行 SHALL 保持输出末行。CI 的动作引用 SHALL 以 commit SHA 固定并注明对应版本。

校验器自身实现 SHALL fail-closed：契约套件跳过计数解析、跳过明细渲染、归档目录枚举与索引解析排序、废弃工具名扫描所依赖的外部命令（grep、sed、sort、awk 等）返回非预期退出码时 SHALL 判 FAIL 并计入失败数，不得当作无匹配、空集合或无跳过继续；跳过计数注解 SHALL 由经校验的非负整数解析产生，解析失败即 FAIL。归档目录枚举 SHALL NOT 依赖 GNU 专有 find 扩展（如 `-printf`）；在非 GNU 环境 SHALL 保持「空归档 vacuous 通过、非空归档必检」语义。`openspec/archive/` 直接子项中的任何符号链接 SHALL 使校验失败，不得以链接方式绕过 1:1 索引约束。

#### Scenario: OpenSpec CLI 不可用的普通诊断

- **WHEN** 运行默认工作流校验且环境没有 OpenSpec CLI
- **THEN** 输出明确的 `SKIP`、原因和未覆盖范围
- **AND** 不宣称 OpenSpec 严格校验通过

#### Scenario: 严格终验缺少 OpenSpec CLI

- **WHEN** 严格 Verify 或 Archive 以 required 模式运行校验且 OpenSpec CLI 不可用
- **THEN** 校验返回非零并阻止完成声明

#### Scenario: fast 缓存来源透明

- **WHEN** `--fast` 模式某重检查命中指纹缓存
- **THEN** 输出仍为 PASS，并在该检查行标注指纹、上次实际执行时间与沿用来源
- **AND** 顶层 PASS/FAIL/SKIP 汇总口径保持不变

#### Scenario: 缓存损坏按未命中处理

- **WHEN** 缓存文件缺失、格式非法、哈希不匹配或记录的历史结果非 PASS
- **THEN** 该检查重新实际执行
- **AND** 不因缓存异常出现假绿或假红

#### Scenario: 事实工具发生回归

- **WHEN** registry 解析、查询边界、分页、敏感内容过滤或 manifest stale 检测测试失败
- **THEN** 主校验返回非零并指出失败测试
- **AND** 失败结果不会被后续缓存命中掩盖

#### Scenario: 新类行为回退被注入

- **WHEN** 向入口或阶段技能注入“快速模式必须自动提交合并”“标准模式无需用户确认即可实现”或“归档前无需校验直接移动目录”类规则
- **THEN** 结构校验返回非零并指出该注入

#### Scenario: 未登记的适配注记出现

- **WHEN** 双套技能树中出现超出登记数量的受豁免前缀适配注记行
- **THEN** 结构校验返回非零，要求显式登记后再放行

#### Scenario: 适配注记回退已废弃工具名

- **WHEN** 任一技能、`.codex/README.md` 或安装资产树中出现已废弃工具名，或 Codex 侧 parallel-agents 技能缺失现行派发工具名
- **THEN** 结构校验返回非零并指出位置

#### Scenario: 归档目录缺索引行

- **WHEN** `openspec/archive/` 存在变更目录但 `README.md` 缺失或缺少对应索引行
- **THEN** 结构校验返回非零，归档前必须补齐索引

#### Scenario: 悬空或重复索引行

- **WHEN** `README.md` 索引行指向不存在的归档目录，或同一目录有重复索引行
- **THEN** 结构校验返回非零

#### Scenario: 安装目标内部跳过透明可见

- **WHEN** 安装目标运行全量公共门禁且契约套件内部存在设计性跳过（如源仓专属的 CI/pre-push 检查）
- **THEN** 输出在最终汇总行之前逐条列明每个内部跳过的测试与原因及计数注解
- **AND** 顶层 `PASS/FAIL/SKIP` 字段不因内部跳过改变，汇总行仍为输出末行

#### Scenario: CI 动作引用漂移回 tag

- **WHEN** CI 工作流中的动作引用回退为不带 commit SHA 的浮动 tag
- **THEN** 维护者可通过登记的 SHA 注释直接识别；该加固随本规格落地后不得移除

#### Scenario: 底层命令错误不得假绿

- **WHEN** 跳过计数 grep、跳过明细渲染 sed、归档索引 sort/awk/cmp 或废弃名扫描 grep 返回错误退出码（非 0/1 语义）
- **THEN** 对应检查判 FAIL 并计入失败数，汇总行不得出现假绿

#### Scenario: 跳过计数解析失败即失败

- **WHEN** 跳过计数 grep 正常返回但结果不是非负整数（如空串或含非数字字符）
- **THEN** 公共门禁判 FAIL，不得按 0 或省略注解继续

#### Scenario: 非 GNU find 环境保持判定语义

- **WHEN** 归档目录枚举运行在不支持 `-printf` 的 find 环境
- **THEN** 空归档且无索引仍 vacuous 通过，非空归档仍强制索引 1:1 校验
- **AND** 不因枚举工具差异出现假绿或假红

#### Scenario: 归档目录含符号链接

- **WHEN** `openspec/archive/` 直接子项中存在符号链接（仓内、外部或 dangling）
- **THEN** 结构校验返回非零，不得以链接绕过 1:1 索引约束

### Requirement: 契约测试基设不得污染安装器资产树

为统计或校验而加载随包契约套件（如 `exec_module` 动态加载资产副本）的测试基设 SHALL NOT 在 `scripts/ai-workflow-assets/` 写入任何文件（含 `__pycache__`/`*.pyc`）。资产 manifest 的物理枚举一致性 SHALL 在套件以任意通行方式（带或不带 `-B`）运行时均成立。

#### Scenario: 无 -B 手工复跑安装器套件

- **WHEN** 维护者不带 `-B` 运行 `python3 -m unittest scripts.tests.test_install_ai_workflow`
- **THEN** 套件不为统计目的在资产树产生 `__pycache__`
- **AND** `test_manifest_exactly_enumerates_sorted_physical_assets` 不因测试自身的中间产物失败

### Requirement: 迁移必须可回退且不得改变业务运行时

共享层迁移 SHALL 分阶段完成，每阶段在删除旧正文前验证新入口、引用、助手适配和回归测试。迁移 SHALL 保护用户未提交修改，不得修改业务项目运行时代码、API、Schema、部署配置或自动执行远端操作。

#### Scenario: 旧 ai-kb 引用尚未迁移完成

- **WHEN** 校验发现仍有入口引用旧正文路径
- **THEN** 迁移停止在兼容阶段，不删除对应旧内容
- **AND** 报告引用位置和剩余风险

#### Scenario: 业务项目目录存在未提交修改

- **WHEN** 工具或迁移发现登记业务仓存在用户修改
- **THEN** 只读查询可以继续按边界运行
- **AND** 任何会触及该仓的写入、清理或分支操作必须停止并另行处理

### Requirement: 工作流校验必须在 CI 自动运行

本仓库 SHALL 配置 CI，在每次推送到 main 和每个拉取请求上自动运行 `bash scripts/validate-workflow.sh --require-openspec`，并在运行前安装 OpenSpec CLI（`@fission-ai/openspec`）。CI SHALL 另以独立步骤现跑安装器套件 `python3 -B -m unittest -v scripts.tests.test_install_ai_workflow` 与 Bash 安装器套件 `python3 -B -m unittest -v scripts.tests.test_install_workflow`；两步 SHALL NOT 被跳过且任一失败 SHALL 使 CI 任务失败。安装器套件内部的耗时优化 SHALL NOT 免除真实用例执行、失败传播或源仓专属覆盖。校验输出中任一 `FAIL` SHALL 使 CI 任务失败以阻断合并；仓库自带的必需测试 SHALL NOT 在 CI 中被跳过。CI 配置 SHALL 仅存在于本仓库，SHALL NOT 进入安装器 `manifest.json` 或随安装资产分发。

#### Scenario: 推送触发自动校验

- **WHEN** 有新提交推送到 main 分支或针对本仓库打开拉取请求
- **THEN** CI 自动检出代码、安装 OpenSpec CLI 并运行 `bash scripts/validate-workflow.sh --require-openspec`
- **AND** 随后独立步骤分别运行 `python3 -B -m unittest -v scripts.tests.test_install_ai_workflow` 与 `python3 -B -m unittest -v scripts.tests.test_install_workflow`
- **AND** 任务退出码与校验汇总一致，任一 FAIL 或测试失败使任务失败

#### Scenario: OpenSpec CLI 预装后不得 SKIP

- **WHEN** CI 环境已安装 `@fission-ai/openspec`
- **THEN** `--require-openspec` 模式下的 OpenSpec 严格校验与仓库自带必需测试实际运行，不出现因工具缺失导致的 SKIP

#### Scenario: CI 配置不随安装器分发

- **WHEN** 安装器向目标项目安装工作流资产
- **THEN** 目标项目不获得 CI 配置文件，`manifest.json` 不含 `.github/` 路径
- **AND** 目标项目需要 CI 时另行评估，不由本变更引入

#### Scenario: 安装器套件回归被 CI 拦截

- **WHEN** 安装器、随包校验资产或其测试发生任一用例失败
- **THEN** CI 在合并前失败,不得依赖人工终验清单才发现

### Requirement: 校验器外部命令清单必须单一来源

校验器使用的外部命令清单 SHALL 由 `scripts/lib/validate-workflow-core.sh` 以 `--print-external-commands` 模式唯一发布;校验相关的测试沙箱(源仓契约测试与安装器套件)SHALL 在运行时从该模式解析白名单,SHALL NOT 维护第二份手写命令元组。core 新增依赖任一外部命令时 SHALL 同步其清单,否则受限 PATH 沙箱用例 SHALL 失败暴露不一致。

#### Scenario: core 新增外部命令

- **WHEN** 维护者向 core 新增一个清单外的外部命令调用
- **THEN** 受限 PATH 契约用例因缺少该命令而失败
- **AND** 更新 `--print-external-commands` 清单为唯一修复点,无需同步第二份白名单

#### Scenario: 安装目标内自包含

- **WHEN** 随资产安装的校验器与契约测试在目标项目运行
- **THEN** 白名单从目标内随包 core 的 `--print-external-commands` 解析
- **AND** 不依赖源仓任何未随包文件

### Requirement: 校验器必须防并发并提供本地推送防护

公共校验入口 SHALL 在可用 `flock` 的环境以排他锁串行化同一工作树的并发校验,第二实例 SHALL 立即失败退出而非与首实例互踩;`flock` 不可用时 SHALL 降级为无锁继续并输出提示。仓库 SHALL 提供 `scripts/hooks/pre-push` 钩子,在推送前现跑秒级 core 结构校验,任一 FAIL SHALL 阻断本次 push;钩子 SHALL 为源仓自愿启用(`git config core.hooksPath scripts/hooks`),SHALL NOT 进入安装资产。

#### Scenario: 两实例并发

- **WHEN** 一个校验实例运行期间另一实例在同工作树启动
- **THEN** 第二实例立即报错退出,两个实例不产生交叉的 mutation 假失败

#### Scenario: flock 不可用降级

- **WHEN** 运行环境没有 flock 工具或锁基础设施不可用
- **THEN** 校验降级为无锁执行并在输出中提示,功能不受影响

#### Scenario: 推送前本地拦截

- **WHEN** 已启用钩子的工作树在 main 门禁为红时执行 git push
- **THEN** pre-push 运行 core 校验失败并阻断 push
- **AND** 未启用钩子的环境不发生任何行为变化

### Requirement: 契约套件必须支持有界并行执行

顶层契约套件 SHALL 通过仓库自带的零第三方依赖执行器并行运行相互独立的用例。源仓库与随包安装资产 SHALL 分发同一执行器，且随包 wrapper SHALL 与源仓 wrapper 保持等价的单一契约调用点；`WORKFLOW_TEST_JOBS=1` SHALL 完整回退为原串行 unittest 执行语义。默认并行度 SHALL 为 `min(CPU 核数, 8)`。执行器 SHALL 聚合每个用例的通过、失败、错误与跳过结果，任一用例失败或错误 SHALL 使套件退出码非零，失败明细 SHALL 完整保留且不得被并行输出吞没。执行器输出 SHALL 保持 wrapper 对 `... skipped` 行的统计与解析兼容，wrapper 成功路径 SHALL 透出实际运行的用例数。并行执行 SHALL NOT 引入共享可变状态或使多个用例写同一路径。

#### Scenario: 默认并行且失败传播

- **WHEN** 契约套件在默认并行度下运行且任一用例失败或错误
- **THEN** 套件以非零退出，失败用例名与错误明细完整可见
- **AND** 其余用例的结果仍被完整聚合

#### Scenario: 串行回退

- **WHEN** 设置 `WORKFLOW_TEST_JOBS=1`
- **THEN** 契约套件按原串行 unittest 命令执行
- **AND** 输出、退出码与跳过统计行为与回退前一致

#### Scenario: 安装目标使用随包并行执行器

- **WHEN** 安装器把工作流资产复制到目标项目并在目标内运行公共门禁
- **THEN** 目标使用随包 `run_validate_workflow_parallel.py` 执行契约套件
- **AND** manifest、物理资产与 wrapper 调用保持一致，不得静默退化为源仓专属路径或串行命令

#### Scenario: 成功输出保留审计计数

- **WHEN** 契约套件全部通过
- **THEN** wrapper 透出实际用例数与设计性跳过明细
- **AND** 顶层 PASS/FAIL/SKIP 汇总语义与末行口径不变

#### Scenario: 跳过统计保持兼容

- **WHEN** 契约套件内部存在设计性跳过
- **THEN** wrapper 仍能从执行器输出统计 `... skipped` 行并在汇总前逐条列明
- **AND** 顶层 PASS/FAIL/SKIP 口径不变

#### Scenario: 并行用例相互隔离

- **WHEN** 多个契约用例并行运行
- **THEN** 各用例使用独立临时目录与夹具，不发生交叉污染
- **AND** 不因并行引入非确定性失败

### Requirement: 安装器集成回归必须去除重复完整门禁

安装器集成测试 SHALL 保留每个 assistant 的真实资产落位、幂等安装、工具测试和完整公共门禁覆盖；同一 assistant 的单个集成路径 SHALL NOT 重复执行多份完整真实契约套件来验证可由一份执行结果证明的行为。required 模式缺少 OpenSpec CLI 的失败路径 SHALL 用独立 sentinel 探针验证 core 失败后 wrapper 仍实际调用契约套件并传播非零退出，SHALL NOT 通过跳过契约调用、测试专用后门或只检查最终退出码来缩短耗时。

#### Scenario: 每个助手保留一次真实完整门禁

- **WHEN** 安装器集成测试分别验证 codex 与 claude 单侧安装
- **THEN** 每个安装目标实际运行一次完整公共门禁并覆盖全部随包契约用例
- **AND** 用例数、允许的设计性跳过原因、工具测试与资产状态断言仍然生效

#### Scenario: required 缺 CLI 使用 sentinel 探针

- **WHEN** 安装目标缺少 OpenSpec CLI 且测试执行 `--require-openspec`
- **THEN** 探针显示 OpenSpec 缺失 FAIL、契约套件被实际调用且任务退出非零
- **AND** 探针不重复执行全部真实契约用例，也不修改真实安装目标断言所用的资产树

#### Scenario: 性能优化不得降低覆盖

- **WHEN** 随包 wrapper、并行执行器或安装器集成测试发生回退
- **THEN** CI 或仓库契约测试失败并指出漂移
- **AND** 不得以跳过安装器套件、缓存通过结果或删除源仓专属用例作为修复方式
