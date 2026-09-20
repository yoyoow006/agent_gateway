---
project: example-project
kind: example-kind
primary_domains: []
related_domains: []
last_verified: YYYY-MM-DD
verified_commit: <40-or-64-character-git-commit>
sources:
  - example-project/example/path.rb
---

# Example Project 项目卡

> 本模板只定义通用结构；目标项目应替换全部占位符，并从权威代码、构建清单、契约或运行文档复核事实。不要把源工作流仓库的业务项目写入本模板。

## 职责

- 负责：
- 不负责：

## 高频入口

- `ExampleTerm` → `example-project/example/path.rb` / `ExampleSymbol` — 说明该入口回答什么问题。
- `example-server` → `example-project/example/application.rb` — 说明服务或应用入口。

## 跨仓库关系

- 上游依赖：
  - `example-project` 依赖 `other-example-project` 的原因、契约或产物。
- 下游消费者：
  - 哪些项目消费本项目接口、artifact、事件或数据。
- 共享契约：
  - 契约文件、API 定义、事件 schema 或数据 owner。

## 搜索锚点

- 业务词：
  - `ExampleTerm`、`ExampleAlias`。
- 类型 / 注解 / 包名：
  - `ExampleNamespace::ExampleType`、`ExampleAnnotation`。
- 特例：
  - 同名概念、历史目录、多端实现或其他容易误定位的差异；没有则删除本节。

## 验证入口

- 构建：`example build --project example-project`
- 测试：`example test --project example-project`
- 证据：[`../../verification-evidence.md`](../verification-evidence.md)
- 基线：frontmatter 的 `verified_commit` 只表示登记证据对应 commit；它不证明当前工作区、当前任务或后续 commit 已通过验证。

## 证据与维护

- 事实来源：
  - 记录路径、构建清单、契约或运行文档。
- 最近复核：
  - 记录日期、commit 和复核范围。
- 残余不确定：
  - 明确未验证的运行环境、部署状态或外部契约；没有则写“无”。
