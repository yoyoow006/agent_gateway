# 共享模块路由表

| 模块名 | 代码路径 | 别称 | 关键词 |
|---|---|---|---|
| 风险分级工作流 | `AGENTS.md`, `CLAUDE.md`, `openspec/specs/risk-tiered-ai-workflow/` | 宪法、总纲、流程路由、需求理解 | 快速模式、标准模式、严格模式、单确认、状态路径、取消路径、共享底线、权威事实优先、编号提问、canonical term、标准三件套条件 design、严格风险触发二次确认、归档轻量门禁、`.ai-local` 精确清理 |
| Codex 阶段与支撑技能 | `.codex/skills/` | Codex 编排器、执行器 | Open、Design、Build、Verify、Archive、TDD、审查、worktree |
| Claude 阶段与支撑技能 | `.claude/skills/` | Claude 编排器、执行器 | Open、Design、Build、Verify、Archive、TDD、审查、worktree |
| 助手适配 | `.codex/README.md`, `AGENTS.md`, `CLAUDE.md` | 工具映射、入口 | spawn_agent、update_plan、apply_patch、宿主能力 |
| 共享知识层 | `.ai/` | kb、memory、rules、prompts、tools | 路由、踩坑、项目卡、知识沉淀、事实查询 |
| 项目登记 | `.ai/kb/projects/registry.json`, `.ai/kb/projects/` | registry、项目卡、项目模板、业务词、依赖、验证入口 | project-context、server-registry、workspace-search、business-terms、dependencies、verification |
| 验证证据 | `.ai/kb/verification-evidence.md` | 证据复用、验证基线 | verified_commit、等价输入、证据失效、NOT_RUN、UNKNOWN、新鲜验证 |
| 安装器知识分层 | `.gitignore`, `.ai/kb/projects/README.md`, `openspec/specs/installer-knowledge-separation/` | 源仓库业务知识、目标项目登记 | 空白 registry、通用骨架、业务项目卡、防回流 |
| 便携安装器契约 | `openspec/specs/portable-ai-workflow-installer/`, `scripts/install-ai-workflow.sh`, `scripts/lib/install_ai_workflow.py`, `.ai/installer-ledger.json`(安装目标侧运行时产物,源仓库不存在) | 安装器、跨 Python 兼容、资产契约、升级路径、台账 | 加载时类型别名、Path 包含 helper、入口契约、preview/--help、事务回滚、--upgrade、installer-ledger、SHA-256 判定 |
| 双运行时一键安装器 | `scripts/install-workflow.sh`, `scripts/tests/test_install_workflow.py`, `openspec/specs/workflow-installer/` | bash 安装器、双运行时安装 | 资产树三树复制、单一来源、--force 逐文件备份、memory 永不覆盖、旧布局 ai-kb.bak 自愈、--fast 装后自检、sdd 星式忽略、无 profile 双侧必检 |
| Codex 目标安装 | `openspec/specs/codex-workflow-target-installation/` | 外部目录安装、真实路径 | 非 Git 根、符号链接、AGENTS 备份、嵌套业务仓库、幂等安装 |
| 测试登录项目 | `projects/test-login/` | 登录演示、随机验证 | PBKDF2、验证码、session token、一次性挑战 |
| OpenSpec 数据层 | `openspec/` | 变更目录 | 模式字段、proposal、delta、tasks、plan、archive |
| 工作流校验器 | `scripts/validate-workflow.sh`, `scripts/tests/run_validate_workflow_parallel.py`, `scripts/hooks/pre-push` | 结构校验、回归守卫、本地推送防护 | mutation、镜像、非法状态、旧重流程、假绿、required、required 参数转发、OpenSpec strict、非 Git 根、PASS/FAIL/SKIP、--fast 分层、--fast 指纹缓存、指纹沿用、FAIL 清缓存、缓存模式 fail-closed、并行契约执行器、随包并行执行器、WORKFLOW_TEST_JOBS、unexpected success、成功用例计数、--archive-light 轻量门禁、--archive-files diff 分类自动升级、--print-external-commands、命令清单单一来源、flock 并发锁、废弃工具名、注记现行工具名、归档索引 1:1、套件内部跳过透明化、fail-closed 退出码、非 GNU find、空数组守卫、契约调用收敛 run_contract_suite、安装器集成去重、sentinel required 探针 |
| 仓库忽略规则 | `.gitignore` | Git 忽略、本地路径 | `.worktrees`、`.codex/sdd`、`.claude/sdd`、外部项目 |
| Git 仓库基线 | `.git/`, `.gitignore`, `openspec/specs/git-repository-baseline/` | 初始提交、main 基线 | root commit、git status、OpenSpec 基线、本地草稿忽略、独立远程授权 |
| Git 基线安全 | `.git/`, `.gitignore`, `openspec/specs/git-baseline-secret-hygiene/` | secret scan、凭据清理、历史重建 | 字面凭据、reachable history、reflog、prune、外部轮换 |
| Git 远程配置 | `.git/config`, `openspec/specs/git-remote-configuration/` | origin、远程仓库 | remote URL、fetch、push、upstream、网络授权 |
