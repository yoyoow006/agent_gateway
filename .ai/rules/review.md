# 审查范围与 finding 契约

本规则只适用于标准和严格模式的完整审查与后续差异复审。快速模式继续使用权威事实核对、针对性验证和完整 diff 检查，不创建 manifest，也不要求独立 reviewer。

## 冻结与陈旧检测

完整审查由主会话或协调者先用 `.ai/tools/review_manifest.py freeze` 冻结所有受影响 Git 仓；manifest 位于 `.ai-local/reviews/<change>/`，不是 OpenSpec 状态真源。reviewer 只读，不得创建、补写、刷新或替换 manifest。

reviewer 必须在读取审查范围前和形成结论前分别执行：

```bash
python3 .ai/tools/review_manifest.py verify --manifest <manifest.json>
```

两次都必须返回 `VALID <id>`，且结论引用该 id 和逐仓 comparison base。任一次返回 `STALE` 或输入错误，reviewer 立即停止，不继续读取、不沿用旧结论，并把变化摘要交回主会话重新 freeze。manifest 只证明范围身份；规格、直接消费者、跨仓契约和验证证据仍需审查。

标准模式仍至多一次全 diff 综合审查；严格模式仍是 Build 的任务级审查，加 Verify 的规格符合性与代码质量两个独立关注面。新 manifest 不增加完整审查层数。

## finding 台账

每条 finding 使用以下固定字段；没有证据、位置或可观察影响的偏好不进入门禁：

```text
id: <稳定标识>
severity: Critical | Important | Minor
repo/path:line: <仓库与精确位置>
evidence: <可复核事实或命令证据>
observable impact: <用户、契约、数据或验证可观察影响>
status: open | resolved | not-an-issue | accepted-risk
minimal fix: <不扩大范围的最小修复方向>
verification: <证明处置结果的命令或检查>
```

台账末尾必须列出 `unverified（未验证范围）` 与 `residual risk（残余风险）`，没有也要写“无”。状态只允许：

- `open`：证据成立且尚未处置；
- `resolved`：最小修复和复验已有新鲜证据；
- `not-an-issue`：由规格、代码或验证证据证明技术主张不成立；
- `accepted-risk`：只能引用用户在看到影响与残余风险后的明确决定，reviewer 或实现者不得自行设置。

Critical/Important 只有进入 `resolved`、`not-an-issue` 或用户明确决定的 `accepted-risk` 才能解除阻断。

## 修复与差异复审

已确认范围内的最小修复沿用当前模式已有授权。修复后由主会话生成新 manifest，并用 `review_manifest.py delta` 形成从上一有效 manifest 到新 manifest 的差异；复审只覆盖该差异、直接消费者和继承的开放 finding，不无证据重读未变化范围或追加原完整审查本可发现的建议。

若修复需要新增未确认行为、依赖、迁移或外部副作用，立即停止，更新 OpenSpec 事实源并请求用户重新确认。
