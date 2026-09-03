# isolated-agent-configs 设计

## 关键决策

| # | 决策 | 备选与弃选理由 |
|---|---|---|
| D1 | Claude 用 `--settings <独立文件>`（实测为叠加层：用户 settings 继续生效，同名 env 键被覆盖） | 改用 `CLAUDE_CODE_.*` 环境变量逐个注入不可靠（版本演进快）；改写用户文件正是要消除的行为 |
| D2 | settings 文件按项目命名 `.agw/claude-settings.<项目|global>.json` 且**每次 run 重写**（0600） | 令牌随项目不同，单文件会被并发不同项目的 run 互相覆盖；重写保证与当前令牌/网关地址一致 |
| D3 | Codex 用 `-p agw`：profile 即 `$CODEX_HOME/agw.config.toml` **独立新文件**，叠加在用户 config.toml 之上（codex 0.149.1 `--help` 原文语义） | 搬 `CODEX_HOME` 整目录会把用户其余配置（模型偏好、MCP）全部隔离掉；`-c` 逐键覆盖冗长且不满足用户点名 `-p` |
| D4 | profile 文件与项目无关（密钥走 `AGW_API_KEY` env），install/run 幂等重写 | 避免每项目一个 profile；网关地址变化时下次 run 自动刷新 |
| D5 | `install` 不再合并/备份用户配置，仅 npm + 预生成独立文件 + 打印用法 | 用户要求零接触默认配置；备份/损坏拒写逻辑随触碰行为一起删除，代码面净缩小 |
| D6 | `$CODEX_HOME` 解析：环境变量优先，缺省 `~/.codex` | 与 codex 自身语义一致，测试经注入 HOME/CODEX_HOME 隔离 |
| D7 | argv 注入顺序：`[claude --settings <path> <extra>...]` / `[codex -p agw <extra>...]` | CLI 参数在用户参数之前，用户 `--` 透传原样在后 |

## 失败路径

- npm 缺失：install 给指引后中止（不变）。
- `.agw/` 或 `$CODEX_HOME` 创建失败：run 前置错误，不启动 agent。
- profile/settings 写入失败（磁盘/权限）：报错退出，不带残缺配置启动。
- 独立文件损坏（用户手改坏 JSON）：claude 自身会报错；agw 下次 run 重写覆盖——不做解析校验（文件由 agw 全权管理）。
- 用户 `~/.claude` / `$CODEX_HOME/config.toml` 不存在：不创建、不报错。

## 安全

- 独立文件含令牌（claude settings）→ 0600；`.agw/` 入网关仓库 gitignore。
- codex profile 不含密钥（env_key 引用）；同样 0600。
- 用户目录只新增文件，永不修改既有文件——"零接触"以测试反向断言（内容+mtime）固化。

## 验证

- 单元：GenerateClaudeSettings（内容/0600/按项目命名/重写）、EnsureCodexProfile（TOML 字段/幂等/CODEX_HOME 尊重）、PrepareExec argv 断言。
- 反向断言：install/run 全流程后用户默认配置内容与 mtime 不变（注入 HOME 的临时目录）。
- 回归：`--` 透传、项目解析、未知项目报错测试保持绿。
- 手动冒烟：本机 `agw run claude --project demo` 实际拉起 claude（参数到达）。
