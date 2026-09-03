# isolated-agent-configs 任务

## 本地整合策略

- 基于 main（0db538c）开 `feature/isolated-agent-configs`；主会话直执，TDD；每任务一提交；完成后 `--fast` 校验 + 全量 diff 自查（改动集中于 internal/agent 与 cli 接线，标准模式不派 reviewer，发现风险升级则停下）。

- [ ] 1.1 红线与基线：`.gitignore` 追加 `/.agw/`；建分支并提交四件套（状态`构建中`）。验证：`git status` 干净。
- [ ] 1.2 先写失败测试（internal/agent/agent_test.go 重写 install 系 + 新增独立配置测试）：`GenerateClaudeSettings(root, project, listen, token)` 生成 `.agw/claude-settings.<project|global>.json`（env 两键、0600、重复调用重写）；`EnsureCodexProfile(codexHome, listen)` 写 `agw.config.toml`（model_providers.agw 四字段、model_provider、disable_response_storage、0600、幂等、`config.toml` 不存在时不创建）；`InstallClaude/InstallCodex`（注入 HOME/Runner）后用户 `~/.claude/settings.json` 与 `~/.codex/config.toml` 内容+mtime 不变且不被创建。
- [ ] 1.3 实现：internal/agent 新增 `GenerateClaudeSettings`/`EnsureCodexProfile`（codexHome 解析：`$CODEX_HOME` > `Options.Home/.codex`）；`InstallClaude/InstallCodex` 改为 npm + 预生成独立文件，删除合并/备份/损坏拒写代码。
- [ ] 1.4 先写失败测试：`PrepareExec` claude 分支 argv = `[claude --settings <根>/.agw/claude-settings.<proj>.json ...extra]` 且文件已生成（含项目令牌）；codex 分支 argv = `[codex -p agw ...extra]`、`AGW_API_KEY` 环境不变、profile 已确保；`--` 透传与未知项目报错回归保持。验证：`go test ./internal/agent/`。
- [ ] 1.5 实现：`PrepareExec` 接入两个生成器（失败即返回错误）；CLI `run`/`install` 路径适配（run 每次重写 settings、确保 profile）。
- [ ] 1.6 文档：README 安装/启动/回滚/安全段改写（零接触语义、`--settings`/`-p` 说明、实测 codex 0.149.1 与 claude 版本、回滚=删除 `.agw/` 与 `$CODEX_HOME/agw.config.toml`）。验证：命令可复制、与实现一致。
- [ ] 1.7 收口：`go build ./... && go vet ./... && go test -race ./... && gofmt -l .`（空）+ `bash scripts/validate-workflow.sh --fast` PASS + `openspec validate isolated-agent-configs --strict --no-interactive`；手动冒烟 `agw run claude/codex` argv 到达；全勾、`待验证`→自查→归档合并回 main。

## 完成定义

- 全部验证通过且证据新鲜；用户默认配置零接触有测试固化；README/规格与实现一致。
