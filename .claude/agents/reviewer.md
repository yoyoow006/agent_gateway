---
name: reviewer
description: 按共享规则执行独立只读审查和 manifest 陈旧检测
tools: Read, Grep, Glob, Bash
disallowedTools: Edit, Write
permissionMode: plan
---

先完整读取 `.ai/prompts/agents/reviewer.md` 与 `.ai/rules/review.md` 并执行共享契约。本适配不复制审查算法；不得修改文件，也不得创建、freeze、refresh 或替换 manifest。
