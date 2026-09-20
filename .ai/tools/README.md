# 共享 AI 工具

## 一键安装 AI 工作流

从本仓库根目录运行安装器，目标必须是已存在的项目目录，并且只能选择 Codex 或 Claude 一侧：

```bash
# 相对路径：安装 Codex
bash scripts/install-ai-workflow.sh --target ../other-project --assistant codex

# 绝对路径：安装 Claude
bash scripts/install-ai-workflow.sh --target /srv/example-project --assistant claude

# 只预览计划，不写文件
bash scripts/install-ai-workflow.sh --target ../other-project --assistant codex --dry-run
```

安装内容是显式清单中的共享 `.ai` 核心、OpenSpec 空白基线、校验入口和所选助手适配，另生成 `.ai/assistant-profile.json` 与 `.gitignore` 受管块。不安装另一侧助手，不复制源项目的业务项目卡、业务规格、活跃变更、归档、`.ai-local` 或现有 memory 正文。

安装器默认不覆盖：任一同路径内容不同、类型不符或涉及符号链接都在写入前整体拒绝；完全相同的文件标记为 `UNCHANGED`，重复执行保持字节和元数据不变。`.gitignore` 是唯一允许合成的既有文件，安装器只追加带固定起止标记的受管块。

如果目标已有 `AGENTS.md` 或 `CLAUDE.md`，先在临时空目录预览或安装对应入口，对比后由维护者人工整合；安装器不猜测 Markdown 合并语义，也不会跳过该冲突。不提供 `--force`、uninstall 或 `both`。

## 升级已安装目标

```bash
bash scripts/install-ai-workflow.sh --target ../other-project --assistant codex --upgrade
bash scripts/install-ai-workflow.sh --target ../other-project --assistant codex --upgrade --dry-run
```

安装与升级都会生成/更新安装器私有的 `.ai/installer-ledger.json` 台账：记录每个 manifest 文件安装时的内容 SHA-256（`.ai/assistant-profile.json` 保持校验器契约格式，不携带台账）。升级按台账逐文件判定：

| 判定 | 行动 |
|---|---|
| 目标内容＝台账哈希（未被目标修改）且≠新版 | `UPGRADED`：替换为新版，事务内同步台账 |
| 目标内容＝新版 | `UNCHANGED` |
| 目标缺失 | `CREATED` |
| 台账不匹配且≠新版 | `SKIPPED`：保留目标内容并在报告中提示人工比对 |
| 台账有、新版 manifest 已移除且未修改 | `REMOVED` |
| 台账有、已移除但目标已修改 | `KEPT`：保留并报告 |

- 台账记"资产谱系哈希"：SKIPPED/KEPT 沿用旧条目，目标回退到任一版资产内容后自动恢复升级资格。
- 旧安装（无台账）首次升级时仅"内容已等于新版"自动通过，其余 `SKIPPED` 报告；完成一次安装/升级后即建立台账。
- 逐文件跳过不产生失败：`SKIPPED`/`KEPT` 属报告，退出码仍为 0；symlink/类型/受管块损坏仍按结构性冲突返回 3。
- 升级与安装共用同一事务：文件与台账同批原子发布，中断后整体回滚到升级前状态。

退出码：`0` 表示帮助、预览或安装成功；`1` 表示内部或事务失败；`2` 表示参数用法错误；`3` 表示目标、边界或内容冲突。首版事务发布依赖 Linux `renameat2` 原子语义，未声明跨操作系统可移植性。

安装器不联网、不安装依赖、不执行 `openspec init/update`，也不创建或切换 Git 分支、修改 Git 配置、提交或推送。安装后由用户在目标项目运行校验、审查 `git diff` / `git status`，再自行决定是否提交。

OpenSpec CLI 不在安装包内；可按目标项目需要安装到已忽略的本地工具目录，临时加入 `PATH`。未安装时，`bash scripts/validate-workflow.sh` 成功但对 OpenSpec 精确记一项 `SKIP`；`bash scripts/validate-workflow.sh --require-openspec` 因 CLI 缺失失败。

`project_facts.py` 只读取 `--workspace/.ai/kb/projects/registry.json` 登记的项目。它不扫描未登记项目、不联网、不 clone，也不写项目、registry、项目卡或缓存。

## 命令

```bash
python3 .ai/tools/project_facts.py project-context --workspace /path/to/workspace --project example-project
python3 .ai/tools/project_facts.py server-registry --workspace /path/to/workspace --server example-server --project example-project
python3 .ai/tools/project_facts.py workspace-search --workspace /path/to/workspace --project example-project --text ExampleSymbol --limit 20 --offset 0
python3 .ai/tools/project_facts.py business-terms --workspace /path/to/workspace --text ExampleTerm --exact --limit 20 --offset 0
```

- `project-context` 输出 `PROJECT` 与零个或多个 `APPLICATION`、`DEPENDENCY`、`VERIFICATION` TSV 行；未检出项目标记为 `missing`，不会自动获取。`DEPENDENCY` 是 registry 声明的直接依赖；`VERIFICATION` 展示 build/test 入口、证据路径和 `verified_commit` 的 `current`/`drifted`/`unavailable` 状态。工具不执行 registry 命令，`current` 也不证明当前任务通过。
- `server-registry` 精确匹配登记的 `server`；可重复传入 `--project` 限定项目。
- `workspace-search` 可重复传入 `--project`，查询至少 3 个字符。结果只含 `project/path:line`，不会输出匹配正文；分页截断提示写 stderr。
- `business-terms` 可重复传入 `--project`，默认对登记的 term/synonym 做包含匹配，`--exact` 改为精确匹配。结果输出 `BUSINESS_TERM`、canonical term、项目、项目卡和相对源码路径；它只做声明式路由，不读取正文，也不证明业务语义仍有效。

所有路径先进行相对路径和 `Path.resolve()` 边界校验；`.ai`、registry、项目路径、搜索根、业务词路径、验证证据路径或候选文件的符号链接逃逸 workspace 时均拒绝。Git 搜索使用 literal pathspec，registry 内容不能注入 pathspec magic 扩大范围。候选只来自 Git tracked 与未忽略 untracked 文件；候选路径任一组件是 symlink 时在解析和读取前跳过，并排除 `.env`、凭据、证书、私钥扩展名和 `id_rsa`、`id_dsa`、`id_ecdsa`、`id_ed25519`。CLI 的 server、project、search text 与 business term 必须是非空单行字符串。依赖必须已登记且无环；验证命令必须非空单行，证据必须存在，verified commit 必须为 40/64 位十六进制。

退出码：`0` 成功，`2` 输入、registry 或边界错误，`3` 零匹配，`4` server 歧义。错误和截断提示写 stderr，正常结果写 stdout，输出顺序稳定。

## Review 范围清单

`review_manifest.py` 为标准/严格 Review 冻结一个或多个显式 Git 仓的精确本地范围。它只使用 Python 标准库和参数数组形式的本地 Git 命令，不联网、不更新仓库；只有 `freeze` 会写文件，且输出必须直接位于 workspace 根的 `.ai-local/reviews/<change>/`。该目录由根 `.gitignore` 忽略，不是 OpenSpec 状态真源。

```bash
python3 .ai/tools/review_manifest.py freeze \
  --change example --workspace "$PWD" --repo-spec "$PWD::main" \
  --output .ai-local/reviews/example/full-1.json
python3 .ai/tools/review_manifest.py verify \
  --manifest .ai-local/reviews/example/full-1.json
python3 .ai/tools/review_manifest.py delta \
  --from-manifest .ai-local/reviews/example/full-1.json \
  --to-manifest .ai-local/reviews/example/full-2.json
```

- `freeze` 解析 comparison base，记录 merge-base、HEAD、committed/staged/unstaged、未忽略 untracked，以及 base/HEAD/index 各层文件 SHA-256 与 Git mode、worktree 文件 SHA-256 与确定性 lstat 权限 mode；schema 版本为 1，`id` 是不含自身的 canonical JSON SHA-256。写入前与读取后都执行同一套深层 schema 校验。
- 父仓若把未忽略的嵌套 Git 仓折叠为 untracked 目录，`freeze` 会 fail closed 并返回 `2`，绝不记录 `directory/null` 身份。应先在父仓忽略该目录，再把子仓作为独立 `--repo-spec` 冻结。
- 所有来自 Git 的路径再次用于查询时均启用 global `git --literal-pathspecs -C ...`；内容先按字面路径解析对象 OID，再用 `cat-file` 读取，因此 `:(glob)**`、`*` 和含冒号文件名不会扩张 pathspec。
- 所有 Git 子进程都复制继承环境并强制 `GIT_OPTIONAL_LOCKS=0`，保留 PATH、locale 等调用环境；unstaged 范围由只读 `diff-files` 候选与禁锁 porcelain status 交叉确认，避免普通 `git diff` 的 index refresh。除 `freeze` 指定的 manifest 外，不创建或改写父仓、普通仓或 gitlink 子仓 index/lock；`verify` 的 VALID、STALE 与错误路径同样只读。
- gitlink 使用 mode `160000`：base/HEAD/index 哈希父指针，worktree 哈希初始化状态与当前子仓 HEAD。gitlink 路径从父仓目录 fd 逐组件以 `O_DIRECTORY|O_NOFOLLOW` 打开；子仓 Git 查询仅绑定 `/proc/self/fd/<fd>` 并传递该 fd，查询前后重开原路径比较 device/inode，拒绝 symlink、rename 或 replacement 竞态。非 Git toplevel 只允许路径缺失或通过 fd 枚举确认的空目录；非空目录、枚举错误或绑定变化均 fail closed，且不递归读取未知内容。dirty 子仓必须同时作为显式 `--repo-spec`；否则 `freeze` fail closed，且不会联网初始化或获取子模块。
- `verify` 严格只读；完全一致输出 `VALID <id>` 并返回 `0`，任一 HEAD、base、路径集合或内容变化输出 `STALE <id>` 与仓路径摘要并返回 `3`。输入或边界错误返回 `2`。
- `delta` 只接受相同仓集合、相同 base 与 merge-base 的两个 manifest，稳定输出 ID、HEAD、范围类别和内容变化路径，供差异复审及继承 finding 使用；base 或仓集合变化返回 `2`。

manifest 只证明范围身份，不证明实现语义正确。reviewer 仍需检查规格、直接消费者、跨仓契约与验证证据，并在读取前和形成结论前分别执行 `verify`。

## 工作流总校验

```bash
bash scripts/validate-workflow.sh
bash scripts/validate-workflow.sh --require-openspec
```

每一项检查都输出 `[PASS]`、`[FAIL]` 或 `[SKIP]`，最后汇总 `PASS=n FAIL=n SKIP=n`。只要存在 `FAIL` 就返回非零；未知参数按参数错误返回非零。

- 默认模式实际运行仓库自带的结构、mutation、事实工具和 Review manifest 测试。缺少 OpenSpec CLI 时只允许该外部校验显式 `[SKIP]`，并列出未覆盖命令。
- `--require-openspec` 用于严格 Verify/Archive；缺少 CLI 或 `openspec validate --all --no-interactive` 失败都记为 `[FAIL]`。仓库自带的必需 Python 测试在两种模式下都不得跳过。
- 公共 `scripts/validate-workflow.sh` 是 Verify/Archive 的唯一门禁入口；它对任何调用环境都先运行 `scripts/lib/validate-workflow-core.sh` 的全部结构、mutation、事实工具、Review manifest 与 OpenSpec 检查，再无条件实际运行 `scripts.tests.test_validate_workflow`，合并真实计数并只输出一个最终 `PASS=n FAIL=n SKIP=n`。内部 core 明确输出 `INTERNAL_RESULT`，仅供公共 wrapper 与契约 fixture 使用，直接运行它不能作为 Verify/Archive 证据。公共入口不存在环境变量、marker、token 或 flag 可跳过 contract。
- validator 为所有 Python 调用同时设置 `PYTHONDONTWRITEBYTECODE=1` 与 `-B`；根 `.gitignore` 也忽略 `__pycache__/` 和 `*.py[cod]`，确保按文档字面运行 Python 测试不会让 Review manifest 变为 STALE。

完整回归命令：

```bash
python3 -m unittest -v scripts.tests.test_validate_workflow
python3 -m unittest discover -v -s .ai/tools/tests -p 'test_*.py'
```
