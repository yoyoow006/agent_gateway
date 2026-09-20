# 共享知识库

本目录保存可由不同助手共同读取的稳定事实，不保存临时任务状态或助手工具细节。

| 路径 | 内容 |
|---|---|
| `overview.md` | 工作流与模块总览 |
| `repository-ignore-rules.md` | 根仓库忽略规则事实 |
| `contracts/` | 已核对的跨端或跨服务契约 |
| `projects/registry.json` | 声明式项目登记、业务词路由与有界搜索范围 |
| `projects/_template.md` | 通用项目卡模板 |
| `projects/<project>.md` | 项目卡、入口、约定和风险提示 |
| `verification-evidence.md` | 验证证据记录与等价复用边界 |

## 维护规则

- 写入前核对代码、主规格或项目权威文档，并在项目卡注明事实来源。
- registry 只登记已知项目，不通过目录扫描自动扩张；本地可用状态不写入正文。
- 变更状态与 tasks 始终写入 `openspec/`，不在本目录建立第二套 checklist。
