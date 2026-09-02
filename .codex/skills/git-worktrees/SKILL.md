---
name: git-worktrees
description: 用于变更需要工作区隔离、存在未提交修改或并行冲突，或严格模式即将实施时。
---

# 使用 Git Worktree

worktree 由冲突和风险触发，不是每次实现计划的固定步骤。

## 模式决策

| 模式/条件 | 动作 |
|---|---|
| 快速模式 | 沿用当前工作区，不主动切分支或建 worktree；与用户修改重叠时停止 |
| 标准模式，工作区干净、无并行、高冲突概率低 | 就地创建/使用 feature 分支，不建 worktree |
| 标准模式，脏工作区、并行实现、高冲突或用户要求隔离 | 创建/复用隔离 worktree |
| 严格模式 | 默认隔离 worktree；用户明确拒绝时说明风险并保护现有修改 |

## 先探测

```bash
git rev-parse --git-dir
git rev-parse --git-common-dir
git rev-parse --show-superproject-working-tree
git branch --show-current
git status --short
git worktree list --porcelain
```

`git-dir` 与 `git-common-dir` 不同且不是子模块，说明已在链接 worktree，直接复用并汇报路径/分支。不要嵌套创建。

## 创建

1. 有平台原生 worktree 能力时优先使用。
2. 否则选择用户指定目录；未指定时优先已有 `.worktrees/`，再用项目根 `.worktrees/`。
3. 项目内目录必须先通过：

```bash
git check-ignore -q .worktrees
```

未忽略时先加入根锚定 `.gitignore` 规则并提交，防止整棵工作树被误纳入。

4. 目标分支不存在时：

```bash
git worktree add ".worktrees/<变更名>" -b "feature/<变更名>"
```

分支已存在且未被其他 worktree 检出时：

```bash
git worktree add ".worktrees/<变更名>" "feature/<变更名>"
```

沙箱阻止创建时明确报告，并只在能保护现有修改的前提下就地工作；否则停下请求决定。

## 基线

进入 worktree 后按项目文件探测依赖安装和测试命令。先运行与仓库相称的干净基线；失败时报告具体命令和失败，询问继续调查还是接受已知失败。

## 清理

- 未完成或未合并：保留并汇报完整路径和分支。
- 已合并且确认的整合策略包含清理：确认工作树干净后移除 worktree，再删除本流程创建的已合并分支。
- 有未提交修改或未合并提交：不得删除；先请求用户决定。

## 常见错误

| 错误 | 正确处理 |
|---|---|
| 标准模式一律询问并创建 worktree | 先看脏工作区、并行和冲突条件 |
| 快速文档任务切 feature/worktree | 留在当前工作区并保护用户修改 |
| 严格模式因“改动小”就地 main | 风险条件优先，默认隔离 |
| 未检查 ignore 就创建项目内 worktree | 先运行 `git check-ignore` |
