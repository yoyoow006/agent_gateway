# 修复 required 工作流基线

模式: 严格
状态: 待验证

## Why

`bash scripts/validate-workflow.sh --require-openspec` 在当前 `main` 基线存在 3 个既有断言失败，导致任何严格变更无法完成 required 门禁。干净 `git archive HEAD` 复现证明失败与 Herdr 技能安装无关：

- `README.md` 是 `WorkflowSemanticSyncTest.test_entrypoints_and_user_documents_use_current_semantics` 的必检用户文档，但缺少规范要求的“三件套”和“硬风险”语义锚点。
- `WorkflowProfileTests.test_installer_selected_only_metadata_allows_core_validation/public_validation` 在源仓库缺少便携安装器时，把夹具误判为安装目标并要求 `.ai/assistant-profile.json`；本仓库实际没有该文件，两个用例必然失败。

修复触及工作流契约测试与治理文档，属于严格条件。

## What Changes

- 在 README 开发/工作流说明中补充当前标准三件套、条件 design 与严格硬风险二次确认语义，不引入旧“固定四件套”口径。
- 将安装器专用 metadata 测试限定为真实源仓（存在 `scripts/lib/install_ai_workflow.py` 或便携安装入口）时执行；当前随包/无安装器仓库不再伪装成安装目标。
- 保持无安装器夹具中 canonical profile 回归测试的现有覆盖：`test_installed_fixture_without_installer_uses_canonical_profile` 已显式写入并验证 profile。
- 增加防回归断言，确保修复不会让安装器用例在源仓不可用时静默跳过或降低覆盖。

## Impact

- **目标**：当前基线 focused 3 failures 归零；`--require-openspec` 外层汇总回到 `FAIL=0`，使严格变更可归档。
- **非目标**：不恢复或实现便携安装器；不生成 `.ai/assistant-profile.json`；不修改 validator 运行语义；不处理与这两个失败无关的测试。
- **用户修改保护**：Herdr 未跟踪产物保持原样；已存在的 `install-herdr-skill` 活跃变更保持独立，不并入本变更。

## Baseline Evidence

- Current HEAD: `578270794ab6be6a33b961ae2299abf9fbddab22`
- Current worktree focused run: 3 failures / 201 tests.
- Clean `git archive HEAD` focused run reproduced the same 3 failures.
- Outer required gate before fix: `PASS=199 FAIL=1 SKIP=0` (one failed outer check containing 3 unittest failures).
