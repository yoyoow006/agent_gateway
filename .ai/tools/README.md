# 共享 AI 工具与校验入口

## 工作流校验

```bash
bash scripts/validate-workflow.sh
bash scripts/validate-workflow.sh --require-openspec
```

默认模式在 OpenSpec CLI 缺失时明确报告 `SKIP`；严格 Verify 和 Archive 使用 `--require-openspec`，不得跳过。OpenSpec CLI 可由维护者安装在项目本地且已忽略的工具目录，再临时加入 `PATH`。安装器不会联网、安装 CLI，也不会执行 `openspec init` 或 `openspec update`。

`project_facts.py` 只读取显式登记，不联网、不获取代码、不写入项目。`review_manifest.py` 为标准或严格审查冻结本地 Git 范围，reviewer 在读取前和结论前各执行一次 `verify`。

一键安装只写显式清单、所选助手 profile 和 `.gitignore` 受管块。相同内容允许幂等重跑；任一不同内容、类型或符号链接冲突会整体拒绝。首版不支持 `force`、升级、卸载或同时安装两种助手。入口冲突需维护者比较目标规则后人工整合。
