---
name: archive
description: 用于标准或严格模式审查终验已通过、需要合并规格与收尾本地工作时。
---

# Archive——规格、知识与本地收尾

快速模式不归档。前置是 proposal `状态: 待归档`、tasks 全勾、对应模式审查零未决、终验有新鲜证据；最新审查 manifest 再次 `review_manifest.py verify` 为 `VALID`，且 `.ai/rules/review.md` 台账中的 Critical/Important 均已关闭。任一 `STALE` 立即停止，不沿用旧结论。

## 取消路径（用户明确决定）

任一`已归档`前状态的变更可经用户明确决定取消；助手只能建议，不得自行置为`已取消`。

1. proposal 置`状态: 已取消`，追加一行`取消原因: <一句话>`。
2. 把 `openspec/changes/<变更名>/` 整体移入 `openspec/archive/`；delta 不合并进主规格，不执行本章其余流程（主规格合并、知识沉淀、归档后验证、分支整合）。
3. feature 分支、worktree 与未提交修改的去留由用户明确指示；删除未合并工作仍需独立授权。
4. 已取消变更只作历史记录：不恢复、不合并 delta；同类新需求按新变更目录重新进入。

## 1. 合并 delta 到主规格

逐能力处理 `openspec/changes/<变更名>/specs/<能力>/spec.md`：

- `ADDED`：主规格不存在则新增；已存在时核对语义后合并 Scenario，禁止重复 Requirement。
- `MODIFIED`：用完整新 Requirement 替换旧版本，保留 delta 未明确删除且仍有效的场景。
- `REMOVED`：删除目标 Requirement；找不到时停止调查，不能静默成功。
- `RENAMED`（如使用）：只改名；同时改行为则另用 MODIFIED。

合并后主规格必须自洽，不依赖 delta 才能读懂。

## 2. 知识沉淀

完成共享 `.ai` 三写：

- memory：归整构建和审查期的新坑与解决办法；
- kb：同步已改变的架构或模块事实；
- rules：新增模块、别称或关键词时更新路由。

Claude 与 Codex 共享 `.ai/`。知识沉淀写可复用事实，不复制变更叙事。

## 3. 归档数据

1. 状态置为`已归档`。
2. 把 `openspec/changes/<变更名>/` 移到 `openspec/archive/<变更名>/`。
3. 严格模式把 `openspec/plan/<变更名>.md` 移为归档目录的 `plan.md`；标准模式没有独立 plan，不制造空文件。
4. 追加 `openspec/archive/README.md` 索引行（变更名—主旨—模式）。
5. 检查主规格、归档目录、共享 `.ai`、finding 的未验证范围/残余风险和 Git 状态；`accepted-risk` 必须引用用户明确决定。

## 4. 归档后强制验证

主规格合并和目录移动会产生 Verify 阶段尚未审过的新状态，提交/整合前必须现跑：

按 proposal 模式选择校验入口：标准模式运行默认诊断；严格模式运行 required 门禁。

```bash
# 标准模式
bash scripts/validate-workflow.sh

# 严格模式
bash scripts/validate-workflow.sh --require-openspec

git diff --check
git status --short
```

校验失败立即停止归档并修复。严格模式的 OpenSpec 和仓库自带必需测试不得 SKIP；CLI 不可用时 required 门禁必须非零，不得声称已经归档或完成 OpenSpec 严格校验。

归档变更以一个职责单元提交：

```bash
git add openspec/ .ai/ && git commit -m "chore(archive): <变更名>"
```

提交前核对暂存清单，避免带入用户或本地草稿文件。

## 5. 分支整合

### 标准模式

按唯一实施确认中明示的本地策略连续执行，无需再次询问。例如已确认“全绿后本地 `--no-ff` 合回 main”，归档后即可执行并在合并结果上重跑验证。

策略未明示、验证失败、范围变化或出现争议时暂停。推送、创建 PR、强推、删除未合并工作及其他外部/破坏性动作始终单独授权；删除已合并分支/worktree 也只在已确认策略包含清理时执行。

### 严格模式

向用户提供并等待选择：

1. 本地 `--no-ff` 合回目标分支并复验；
2. 保留 feature，授权后推送/创建 PR；
3. 保留分支和 worktree 稍后处理。

不得替用户选择，不得在未合并时删除分支或 worktree。

## 6. 完成声明

只有主规格合并、知识沉淀、归档移动、归档后验证、提交和所选本地整合全部成功，且最终目标上现跑验证通过，才能声称已归档/已合并。

## 常见错误

| 错误 | 正确处理 |
|---|---|
| 标准模式归档时再次询问已确认的本地合并 | 按预授权连续收尾 |
| 标准模式强造 plan.md | 只有严格模式有独立 plan |
| 合并 delta 后沿用 Verify 旧证据 | 重新跑 workflow 与 OpenSpec 校验 |
| 外部动作套用本地预授权 | 推送、PR、强推仍单独授权 |
| delta 直接复制为第二份真源 | 合并成可独立阅读的主规格 |
