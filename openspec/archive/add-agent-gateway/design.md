# add-agent-gateway 设计

## 总览

```
                    ANTHROPIC_BASE_URL=http://127.0.0.1:8787          ┌────────────────────────┐
  ┌──────────────┐  ANTHROPIC_AUTH_TOKEN=agw-<虚拟令牌>              │        agw 网关         │
  │ Claude Code  ├─────────────────────────────────────────────────►│  /v1/messages          │
  └──────────────┘                                                  │  /v1/messages/count_.. │
                                                                   │  /v1/responses         │
  ┌──────────────┐  ~/.codex/config.toml → base_url=网关/v1         │  /v1/chat/completions  │
  │ Codex CLI    ├─────────────────────────────────────────────────►│  /v1/models            │
  └──────────────┘  AGW_API_KEY=agw-<虚拟令牌>                      │  /__agw/* (管理)        │
                                                                   └───────────┬────────────┘
                                     虚拟令牌 → 项目档案（全局池 ← 项目覆盖）          │
                                     粘性首选 → 优先级排序，跳过熔断中的供应商          │
                                               ┌─────────────────────┼──────────────────────┐
                                               ▼                     ▼                      ▼
                                        anthropic 协议         openai-chat 协议        openai-responses 协议
                                          （透传）              （IR 翻译适配）            （IR 翻译适配）
```

请求处理管线：`客户端请求 → 令牌鉴权/档案解析 → 候选供应商链（粘性→优先级，跳过熔断）→ 逐家尝试（同协议透传 / 跨协议 IR 翻译）→ 首字节前可重试失败则换下一家并重放 → SSE 即时回传 → 记录熔断与指标`。

## 关键决策

| # | 决策 | 备选与弃选理由 |
|---|---|---|
| D1 | 本地反向代理（agent 永远指向网关），而非直写 agent 配置切换 | cc-switch 直写模式需改 `settings.json` 且重启生效、有全量重写风险；代理模式下切换对 agent 完全透明 |
| D2 | 虚拟令牌 → 项目档案：`agw run` 注入 `ANTHROPIC_AUTH_TOKEN`/`AGW_API_KEY` 为 `agw-<token>`，网关反查项目 | 自定义头（CC 不保证透传）、每项目端口（状态爆炸）都不可靠；令牌走标准认证通道，对 CC/Codex 均零特判 |
| D3 | 请求边界 failover：请求体内存缓冲（上限 64MiB）重放；流建立后失败交还客户端重试 | 流中续传需重放已消费输出，必然重复内容、复杂度极高收益极小；CC/Codex 自带重试，落到健康供应商即任务不中断 |
| D4 | 被动熔断（连续失败阈值→指数冷却→半开单探针），真实请求兼作探针 | 主动拨测有额外费用与误判（多数 LLM 端点无廉价 GET），列 v1 非目标；`agw provider test` 提供手动探测 |
| D5 | 同协议 + 无模型映射 → 字节级透传快路径（仅换认证/Host）；有映射仅重写 `model` 字段 | 最大保真与最低开销；解析/重序列化只发生在真正跨协议时 |
| D6 | 中立 IR + 三套编解码器（anthropic、openai-chat、openai-responses），hub-and-spoke 覆盖全部组合 | 点对点翻译器需写 4 套重叠逻辑；IR 多付一点前期成本换组合完备与单一测试面 |
| D7 | 流式转换 = 每协议一套 SSE 解码器/编码器（有状态事件机），逐事件转换不聚合 | 整段缓冲再翻译破坏流式体验；事件机保持首字节延迟 |
| D8 | `agw install codex` 写入禁用响应存储（无 `previous_response_id` 依赖），每请求自包含 | 响应存储会把会话状态钉死在单一上游，failover 与跨协议翻译都会断；无状态化是"可切换"的前提 |
| D9 | `count_tokens`：链内有 anthropic 上游则转发，否则本地粗估返回 Anthropic 格式 | 部分中转站无此端点，CC 依赖它；估算误差只影响 CC 的上下文预算判断，不影响正确性 |
| D10 | TOML 三层合并 `default.toml ← local.toml ← 项目 agw.toml`；密钥优先 `env:VAR` 间接，明文只允许 0600 的 local.toml；TOML 选型与 codex 生态一致 | JSON/YAML 无收益；密钥进 git 是硬红线 |
| D11 | `agw run` 用 `syscall.Exec` 替换进程启动 agent | spawn+wait 产生中间进程，信号转发与僵尸处理复杂；exec 后 Ctrl-C 直达 agent |
| D12 | 默认绑定 127.0.0.1；`/__agw/*` 管理面要求 admin 令牌；日志密钥脱敏（前 6 位） | 本机工具最小暴露面；绑定非回环地址时启动告警 |
| D13 | 依赖仅 `spf13/cobra`、`BurntSushi/toml`；测试用标准库 `httptest` | 网络层标准库足够；依赖越少供应链面越小 |

## 协议转换设计

### IR（中立中间表示）

- `Request`：model、system（多段）、messages（`Turn{Role, Parts[]}`）、tools、tool_choice、max_tokens、temperature、top_p、stop、stream。Part 类型：`Text`、`Image{媒体类型, base64}`、`ToolUse{ID,Name,InputJSON}`、`ToolResult{CallID,Content,IsErr}`、`Thinking{Text}`。
- `Response`：content parts、stop_reason（统一枚举：`end_turn|tool_use|max_tokens|other`）、usage（input/output/cache_read/cache_write）。
- 流事件：`StreamStart`、`BlockStart{Index,Kind}`、`TextDelta`、`ThinkingDelta`、`ToolCallDelta{Index,JSON 增量}`、`BlockStop{Index}`、`StreamEnd{StopReason,Usage}`、`StreamError`。
- 每协议编解码器六件套：`ParseRequest` / `BuildRequest`（含目标路径与默认头）/ `ParseResponse` / `BuildResponse` / `NewStreamDecoder`（上游 SSE → IR 事件）/ `NewStreamEncoder`（IR 事件 → 客户端 SSE）。

### 映射要点

- 消息：Anthropic user 消息里的 `tool_result` 块 ↔ chat 的 `role:"tool"` 消息（按 `tool_use_id`/`tool_call_id` 关联）↔ Responses 的 `function_call_output` 条目。
- 工具：Anthropic `tools[].name/description/input_schema` ↔ chat `functions`/`tools[].function` ↔ Responses `tools[]`（function 类型）。
- 流式：chat 的 `choices[0].delta` 按 index 合并；翻译到 Anthropic 需合成合法事件序列（`message_start` → `content_block_*` → `message_delta`(含 usage) → `message_stop`，工具调用块在首个参数增量时 `content_block_start`）；翻译到 Responses 需合成 `response.created` / `output_item.added` / `output_text.delta` / `response.completed`。
- 错误：上游错误体 → 客户端协议错误体（Anthropic `{type:"error",error:{type,message}}` / OpenAI `{error:{message,type,code}}`），保留状态码与 `Retry-After`。

### 保真降级表（明确可接受的损失）

| 特性 | 跨协议行为 |
|---|---|
| `cache_control` | 非 anthropic 上游丢弃 + 警告日志（缓存失效只影响成本/延迟） |
| thinking/reasoning | best-effort 双向映射（Anthropic thinking ↔ Responses reasoning ↔ chat 无对应则丢弃） |
| `top_k`、`metadata.user_id` 等 | 无对应字段的协议丢弃 + 调试日志 |
| 响应中的 `model` 字段 | 保留上游原值（仅展示用途） |
| 音视频等块 | 400 明确错误（v1 非目标） |
| Codex `additional_tools`（namespace/custom 工具编排） | 跨协议翻译丢弃 + 显式告警；Codex 请配 openai-responses 供应商（v1 已接受边界，用户 2026-09-03 决策） |

## 失败路径

- 配置缺失/语法错误：`agw start` 启动时快速失败并指出文件与行；运行中重载失败保留旧配置。
- 档案链为空：按客户端协议返回 503 错误体，不挂起。
- 端口占用：启动报错退出，提示占用进程。
- 上游连接/TLS/首字节超时/408/429/5xx/529：可重试，换下一家；全部失败返回最后一次错误。
- 请求体超 64MiB：413，不触上游。
- npm/git 缺失：install/project 给出指引后中止；网关本身不依赖二者。
- 客户端令牌未知：401（按端点协议返回对应错误体）。
- 翻译遇到不可映射块：400 明确指出块类型与位置。

## 安全

- 密钥仅存在于 0600 的 `config/local.toml` 或环境变量；`config/default.toml` 只放非敏感默认值并作为示例。
- 令牌（全局/项目/admin）首次生成用 `crypto/rand` 32 字节 hex；日志脱敏。
- 默认 `127.0.0.1` 监听；配置非回环地址时启动显著告警。
- 无任何遥测或外发数据；日志只在本地。

## 工程结构

```
go.mod / go.sum
cmd/agw/main.go                 # cobra 入口
internal/cli/                   # serve、start/stop/status/logs、provider、switch、install、run、project
internal/config/                # 加载、三层深合并、0600 写回、令牌管理、env: 解析
internal/gateway/               # HTTP 服务、端点、令牌→档案、failover 引擎、翻译管线、管理端点
internal/provider/              # 供应商注册表、熔断器、指标
internal/protocol/              # IR、SSE 帧、编解码接口
internal/protocol/anthropic/    # Anthropic Messages 编解码
internal/protocol/openaichat/   # Chat Completions 编解码
internal/protocol/openairesponses/  # Responses 编解码
internal/agent/                 # install/run claude 与 codex（npm、配置安全合并、exec）
internal/workspace/             # projects/ 管理、git init
config/default.toml             # 提交的默认配置（无密钥）
projects/                       # 业务项目（gitignore）
.run/                           # pid/log（gitignore）
```

Go ≥1.22；TDD：每个运行时行为先写失败测试（`httptest` 假上游 + `testdata` 黄金样例），再实现转绿。

## 验证策略

- 单元：`go test ./...` 覆盖配置合并、熔断、failover 分类、三协议编解码黄金样例、四组翻译对流式序列的断言、agent 配置安全合并、工作区。
- 静态：`gofmt -l .` 为空、`go vet ./...` 通过。
- 工作流：`bash scripts/validate-workflow.sh --fast` PASS；`openspec validate add-agent-gateway --strict --no-interactive`（CLI 缺失时记录并跳过）。
- 手动冒烟（构建后）：假上游起两个进程模拟 529→failover；`agw run claude --project demo` 走通档案路由。

## 风险与缓解

| 风险 | 缓解 |
|---|---|
| 翻译保真度缺陷（工具调用/流式事件）导致 agent 行为异常 | 黄金样例锁定映射；同协议永远走透传；翻译路径字段级日志可开 `-v` 排查 |
| 中转站 SSE 私有怪癖（非标准事件、分块方式） | 透传路径零解析；解码器对未知事件忽略并 debug 记录，不失败 |
| Codex 配置键随版本变化（如响应存储开关名） | install 任务在实现时对照已安装 codex 版本核实键名，写前备份，README 记录回滚 |
| 大请求内存缓冲放大 | 64MiB 硬上限 + 413；流式响应不缓冲 |
| prompt cache 因切换失效推高成本 | 粘性首选（D2/switch）减少无谓切换；日志提示切换事件 |
| settings.json/TOML 合并破坏用户配置 | 时间戳备份 + 仅写自有键 + 金样例测试保护未知键 |
