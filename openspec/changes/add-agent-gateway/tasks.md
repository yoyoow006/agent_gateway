# add-agent-gateway 任务

## 本地整合策略

- 网关仓库先 `git init` 提交脚手架基线（main），随后开 `feature/add-agent-gateway` 分支实施；不建 worktree（脚手架仓库、单人实施、无并行冲突）。
- 主会话直执；每个任务一个提交（可独立回滚）；运行时行为 TDD（先失败测试后实现）。
- 完成后一次全量 diff 综合审查 + `verify`（fast），通过后归档合并。

## 1. 工程基线

- [x] 1.1 git 基线与忽略规则：更新 `.gitignore`（追加 `/projects/`、`/.run/`、`/config/local.toml`、`/agw`、`/dist/`），`git init && git add -A && git commit -m "chore: workflow scaffold baseline"`，创建并切换 `feature/add-agent-gateway`。验证：`git status` 干净、`git log --oneline` 有基线提交、当前分支为 feature 分支。
- [x] 1.2 Go 模块与 CLI 骨架：确认 `go version` ≥1.22；`go mod init agent_gateway`；引入 `spf13/cobra`、`BurntSushi/toml`；创建 `cmd/agw/main.go`（cobra 根命令 `agw`，暂无子命令）与 `internal/{cli,config,gateway,provider,protocol,agent,workspace}` 空包（doc.go 占位）。验证：`go build ./...` 通过、`go run ./cmd/agw --help` 输出帮助、`gofmt -l .` 与 `go vet ./...` 干净。

## 2. 配置层（internal/config）

- [x] 2.1 先写失败测试 `config_test.go`：三层合并优先级（项目覆盖 local、local 覆盖 default）、`env:VAR` 密钥解析（含缺失环境变量报错）、未知字段不致解析失败。
- [x] 2.2 实现 `config.go`：`Config{Gateway, Providers[], Projects map[string]ProjectProfile}`、`Provider{Name, Protocol(enum: anthropic|openai-chat|openai-responses), BaseURL, APIKey, APIKeyEnv, Priority, Enabled, Preferred, ModelMap, Headers, ConnectTimeout, FirstByteTimeout}`；`Load(repoRoot)` 深合并三层；`SaveLocal` 以 0600 写回；`NewToken()`（crypto/rand 32B hex）及令牌→项目索引。验证：`go test ./internal/config/` 全绿（含 2.1）。
- [x] 2.3 生成 `config/default.toml`（提交版示例：监听 `127.0.0.1:8787`、注释齐全的 provider 模板、无密钥）。验证：以 default.toml 单独 Load 成功且无明文密钥字段。

## 3. IR 与三协议编解码（internal/protocol*）

- [x] 3.1 SSE 帧工具 `protocol/sse.go` + 测试：按 `\n\n` 切帧、容忍 `\r\n`、`data:` 多行拼接、流中断在中途时返回已缓冲错误。
- [x] 3.2 IR 类型 `protocol/ir.go`（Request/Response/Part/Event，见 design）+ 编解码器接口 `protocol/codec.go`（六件套）。验证：编译通过；接口文档注释完整。
- [x] 3.3 anthropic 编解码器 `protocol/anthropic/`（TDD）：黄金样例 `testdata/`（含 system 多段、tool_use/tool_result、base64 图片、cache_control、count_tokens 请求）；`ParseRequest/BuildRequest/ParseResponse/BuildResponse` 往返一致；流式 Decoder 产出 `message_start→content_block_*→message_delta(usage)→message_stop` IR 事件，Encoder 反向合成合法 SSE（含 ping 透传策略：不主动造 ping）。验证：`go test ./internal/protocol/anthropic/`。
- [x] 3.4 openaichat 编解码器 `protocol/openaichat/`（TDD）：黄金样例（多轮、`tool_calls`、`role:"tool"`、图片 `image_url` data URL、流式 `choices[].delta` 按 index 合并、`finish_reason` 全枚举）。验证：`go test ./internal/protocol/openaichat/`。
- [x] 3.5 openairesponses 编解码器 `protocol/openairesponses/`（TDD）：黄金样例（`instructions`+`input` 条目、`function_call`/`function_call_output`、`store:false`、`stream` 事件 `response.created/output_item.added/output_text.delta/output_item.done/response.completed`、错误体）。验证：`go test ./internal/protocol/openairesponses/`。
- [x] 3.6 跨协议黄金样例测试 `protocol/translate_test.go`：四组组合（anthropic↔chat、anthropic↔responses、responses↔chat、responses↔anthropic）请求→IR→构建请求体断言；上游样例响应/SSE→IR→客户端格式断言；`cache_control` 丢弃产生一条警告。验证：`go test ./internal/protocol/...`。

## 4. 供应商注册与熔断（internal/provider）

- [x] 4.1 先写失败测试：连续失败达阈值（默认 3）→ 打开并跳过；冷却（60s 起、指数×2、上限 15m，测试注入时钟）后半开放行单请求；探针成功关闭、失败重开；并发计数安全（`-race`）。
- [x] 4.2 实现 `breaker.go`（状态机+注入时钟）与 `registry.go`（快照供 status；请求/失败/在途计数）。验证：`go test -race ./internal/provider/`。

## 5. 网关 HTTP 核心（internal/gateway）

- [ ] 5.1 先写失败测试 `server_test.go`（httptest）：令牌→档案（全局令牌/项目令牌/未知令牌 401 且错误体按端点协议）；`/v1/models` 返回配置模型；`/__agw/healthz` 免认证 200；`/__agw/metrics|reload` 无 admin 令牌 401。
- [ ] 5.2 实现 `server.go`：路由表（`/v1/messages`、`/v1/messages/count_tokens`、`/v1/responses`（POST/GET）、`/v1/chat/completions`、`/v1/models`、`/__agw/*`）、令牌中间件、协议错误体构造器、优雅停机（15s 排空）。验证：`go test ./internal/gateway/`。
- [ ] 5.3 透传转发 `proxy.go`（TDD）：同协议字节级透传（认证替换、Host 重写、hop-by-hop 头剔除、查询串保留、SSE 逐写 Flusher、GET 无体）；有 `model_map` 时仅重写 model 字段（`json.RawMessage` 最小解析）；上游错误体按客户端协议透传。验证：httptest 断言上游收到的字节与客户端发送一致（model 外）。

## 6. failover 引擎（internal/gateway）

- [ ] 6.1 先写失败测试 `failover_test.go`：双假上游（第一家 529/429/连接拒绝/首字节超时）→ 第二家成功且客户端无感；全链失败返回最后一次错误（含 Retry-After）；熔断打开的供应商零连接；粘性首选优先于优先级数字；>64MiB 请求体 413；成功/失败计入熔断与指标。
- [ ] 6.2 实现 `failover.go`：内存缓冲请求体（上限可配）、可重试分类（网络错误/408/429/5xx/529）、候选排序（preferred→priority、跳过 open）、每家尝试（透传或翻译）、失败换家重放、熔断与指标记录。验证：`go test -race ./internal/gateway/`。

## 7. 翻译管线集成（internal/gateway）

- [ ] 7.1 先写失败测试 `translate_test.go`（每组合一条流式 e2e）：anthropic 客户端→chat 上游、anthropic→responses、responses(含工具)→chat、responses→anthropic；断言客户端收到自身协议的合法 SSE 事件序列、工具参数增量拼接正确、usage/stop_reason 映射正确；上游 401 全链失败后错误体已转成客户端协议格式。
- [ ] 7.2 实现 `translate.go`：按（客户端协议, 供应商协议）选择编解码器对；`model_map` 在 BuildRequest 前重写 IR.Model；不可映射块 400 明确错误；流式 Decoder→Encoder 直连逐事件转换。验证：`go test ./internal/gateway/`。
- [ ] 7.3 `count_tokens` 兜底：链内有 anthropic 上游则转发（同 6.2 failover），全 404/405 或无则本地估算（字节/4）返回 `{input_tokens:N}`。TDD 覆盖两种档案。验证：`go test ./internal/gateway/ -run CountTokens`。

## 8. 运维 CLI（internal/cli）

- [ ] 8.1 `serve`：前台运行、SIGHUP 热重载（失败保旧配置并记日志）、启动时确保 local.toml 与三令牌（admin/全局/项目随建）存在、非回环监听告警。TDD：重载成功/失败两例。验证：`go test ./internal/cli/ -run Serve`。
- [ ] 8.2 `start/stop/status/logs`：start 分离进程（pidfile `.run/agw.pid`、日志 `.run/agw.log`、已运行报错不覆盖）；stop SIGTERM 等待退出；status 展示 pid/监听/供应商熔断与计数（`--json`）；logs 查看/跟随。TDD（临时目录 + 短生命周期 serve）。验证：`go test ./internal/cli/ -run 'Start|Stop|Status'`。
- [ ] 8.3 `provider add/remove/enable/disable/list/test` 与 `switch`：写回 local.toml 并调用 `/__agw/reload`；`provider test` 按协议发最小请求报延迟/错误；`switch` 写 Preferred。TDD：写回内容、配置变更后热生效、switch 后排序变化（用 fake 网关）。验证：`go test ./internal/cli/ -run 'Provider|Switch'`。
- [ ] 8.4 日志脱敏工具 `redact.go` + 测试：密钥/令牌/Authorization 头替换为前 6 位 + `***`。验证：`go test ./internal/cli/ -run Redact`。

## 9. agent 适配（internal/agent）

- [ ] 9.1 先写失败测试：settings.json 安全合并（保留用户未知键、仅更新 `env.ANTHROPIC_BASE_URL/ANTHROPIC_AUTH_TOKEN`、生成时间戳备份、损坏 JSON 拒绝写入）；config.toml 合并（新增 `model_providers.agw`、`model_provider="agw"`、禁用响应存储键、保留用户 provider、备份）；npm/git 探测（可注入 runner，缺失时返回指引错误）。
- [ ] 9.2 实现 `install.go`：`InstallClaude`（npm 探测→`npm i -g @anthropic-ai/claude-code`→合并 settings.json→输出回滚指引）；`InstallCodex`（npm 安装→合并 `~/.codex/config.toml`，`wire_api="responses"`、`env_key="AGW_API_KEY"`；实现时对照已装 codex 核实响应存储开关键名，README 记录）。验证：`go test ./internal/agent/ -run Install`。
- [ ] 9.3 实现 `run.go`：项目解析（`--project` 或 cwd 位于 projects/ 下推断，未知名牌列出可用项目）、chdir、注入环境（claude：BASE_URL/AUTH_TOKEN=项目令牌；codex：`AGW_API_KEY`=项目令牌）、`syscall.Exec` 替换进程；`--` 后参数透传。TDD：环境/目录/参数组装断言（exec 本体用可注入命令验证）。验证：`go test ./internal/agent/ -run Run`。

## 10. 项目工作区（internal/workspace）

- [ ] 10.1 先写失败测试：名称校验（`^[a-z0-9][a-z0-9_-]*$`）、创建（目录+git init+agw.toml 模板+令牌入 local 配置）、已存在报错不覆盖、list（名称/分支/脏状态/覆盖摘要）。
- [ ] 10.2 实现 `project.go`：`New/List`，git 经可注入 runner（缺失告警跳过），模板含注释示例。验证：`go test ./internal/workspace/`。

## 11. 端到端与文档

- [ ] 11.1 e2e 测试 `e2e_test.go`：进程内起完整网关 + 双假上游（其一前 2 次 529 后恢复）：anthropic 与 responses 两种客户端各走通一次 failover；熔断冷却后（注入时钟）原上游恢复接流；SSE 首字节延迟断言（无聚合缓冲）。验证：`go test ./internal/gateway/ -run E2E -race`。
- [ ] 11.2 `README.md`：快速开始（init→provider add→start→install→run）、配置参考（三层合并/协议/model_map/超时）、翻译矩阵与降级表、failover 与熔断语义、安全说明（密钥/令牌/绑定）、回滚（install 备份、git 分支）。验证：人工核对命令可复制执行、与实现一致。
- [ ] 11.3 全量验证收口：`go build ./... && go vet ./... && go test -race ./...` 全绿、`gofmt -l .` 为空、`bash scripts/validate-workflow.sh --fast` PASS、`openspec validate add-agent-gateway --strict --no-interactive`（CLI 缺失记录跳过）；勾选全部任务、proposal 状态推进到`待验证`，进入 verify 流程。

## 完成定义

- 上述验证全部通过且证据新鲜；tasks 全勾；一次全量 diff 综合审查无 Critical/Important 遗留；verify（含 fast 工作流校验）通过后按 archive 归档合并规格。
