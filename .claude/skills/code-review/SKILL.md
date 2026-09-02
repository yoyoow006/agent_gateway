---
name: code-review
description: 用于变更达到审查边界、合并前需要独立判断，或收到审查意见需要技术核实时。
---

# 代码审查

审查层数由模式和风险决定。审查意见是待验证的技术主张，不是必须表演性接受的命令。

## 何时审查

| 情形 | 审查策略 |
|---|---|
| 快速模式 | 不派独立审查；核对权威事实、针对性验证和完整 diff |
| 标准小任务 | Build 不做任务级审查；Verify 至多一次全 diff 综合审查 |
| 标准大型或独立任务 | 可在高风险边界按需审查，但不得叠加成每任务双审＋整分支＋双终审 |
| 严格模式 | Build 每职责单元规格与质量审查；Verify 再做规格符合性、代码质量双阶段独立审查 |
| 合并/发布前或用户明确要求 | 对明确 BASE..HEAD 做与风险相称的审查 |

## 审查输入

给审查者：模式、已确认规格/计划和禁止项；由主会话 freeze 的有效 manifest 与精确 diff；测试命令与退出结果；需聚焦的安全、迁移、并发或兼容风险。reviewer 只读，不得创建或刷新 manifest，并须按 `.ai/rules/review.md` 在读取前和结论前运行 `review_manifest.py verify`；任一 `STALE` 立即停止。

审查者不得依赖实现者结论。标准综合审查一次覆盖规格、正确性、安全、性能、测试有效性和可维护性；严格审查按阶段只做自己的关注面。

## 固定输出契约

完整 finding/status/delta 算法以 `.ai/rules/review.md` 为唯一共享正文，输出至少使用：

```text
Verdict: PASS | FAIL
Manifest: <id 与逐仓范围>
Findings:
- id: <ID>
  severity: Critical | Important | Minor
  repo/path:line: <位置>
  evidence: <证据>
  observable impact: <可观察影响>
  status: open | resolved | not-an-issue | accepted-risk
  minimal fix: <最小修复>
  verification: <复验方式>
Unverified（未验证范围）: <范围或“无”>
Residual risk（残余风险）: <风险或“无”>
Verification: <实跑命令、退出结果、未运行项及原因>
Unresolved: <未决 ID 或“无”>
```

没有 finding 时也必须显式写“无”。每条 finding 必须可定位、可验证、可裁决；`accepted-risk` 只能引用用户明确决定，泛泛的“建议优化”不进入门禁。

## 严重级别

- **Critical**：数据损坏、安全漏洞、核心功能不可用、不可安全合并。
- **Important**：真实规格缺口、明显错误、关键测试缺失或高概率回归。
- **Minor**：不阻塞的命名、样式或可选改进。

Critical/Important 必须修复、以证据驳回或交用户裁决。不得用大量 Minor 掩盖无阻塞问题。

## 收到意见

1. 读完整意见并复述技术主张。
2. 在当前代码、规格和测试中复现或核对。
3. 正确则按适用的 TDD/验证规则修复；错误则引用证据说明。
4. 已确认范围内最小修复沿用现有授权；主会话重冻后只复审 delta、直接消费者和继承 finding，并重跑相关回归。新增未确认行为、依赖、迁移或外部副作用时才停止并重新确认。

## 常见错误

| 错误 | 正确处理 |
|---|---|
| 标准小任务每步都派审查者 | 留到 Verify 做一次综合审查 |
| 严格任务用一次综合审查替代双阶段 | 保留任务级和两个独立关注面 |
| 只说“有问题”不写 file:line/证据 | 按固定输出契约给可审计 finding |
| 审查者说有问题就立即改 | 先验证技术正确性 |
| 没有 BASE..HEAD 就审整个仓库 | 构造精确范围，避免噪音 |
