# agw —— 本地大模型 API 路由网关

agw（agent gateway）在 AI agent（Claude Code / Codex）与大模型供应商之间做本地路由：**agent 一次性指向网关，网关负责协议转换、故障切换与项目档案路由——切换供应商不需要重启 agent，正在运行的任务不中断。**

```text
Claude Code ──ANTHROPIC_BASE_URL──►┐
                                   │  agw (127.0.0.1:8787)
Codex ──config.toml base_url──────►┤  ├─ 令牌→项目档案
                                   │  ├─ 同协议字节级透传（零解析）
                                   │  ├─ 跨协议翻译（anthropic ↔ openai-chat ↔ openai-responses）
                                   │  └─ 请求边界 failover + 被动熔断
                                   ▼  ▼  ▼
                            供应商池（官方 API / 中转站 / 任意协议）
```

## 快速开始

前置：Go ≥1.22（构建）、Node.js ≥18 + npm（安装 agent）、git。

```bash
# 1. 构建
go build -o agw ./cmd/agw

# 2. 密钥写进 .env（已被 git 忽略；建议 chmod 600），供应商用环境变量名引用
cp .env.example .env && chmod 600 .env   # 编辑 .env 填入 OFFICIAL_KEY / RELAY_KEY
./agw provider add official --protocol anthropic --base-url https://api.anthropic.com \
    --api-key-env OFFICIAL_KEY --priority 10
./agw provider add relay --protocol openai-chat --base-url https://relay.example/v1 \
    --api-key-env RELAY_KEY --priority 1 --model claude-sonnet-5=claude-sonnet-5-relay

# 3. 启动网关（首次自动生成 admin/全局令牌到 config/local.toml）
./agw start
./agw status

# 4. 一键安装并指向网关（npm 安装 + 备份后安全合并 agent 配置）
./agw install claude
./agw install codex

# 5. 在项目里启动 agent（项目令牌自动注入，exec 替换进程）
./agw project new demo
./agw run claude --project demo
./agw run codex --project demo -- --model gpt-5.2
```

从这一刻起：Claude Code 与 Codex 的所有请求都经过 agw。任何一家供应商限流（429）、过载（529）、超时或宕机，网关在**下一个请求**自动切到健康供应商——agent 与正在运行的任务完全无感。

## 核心语义

### 不中断切换（请求边界 failover）

- **首字节前**（连接失败 / 超时 / 408 / 429 / 5xx / 529 / 上游 401/403）：自动换下一优先级供应商并**重放原请求**，客户端无感知。
- **流建立后**中断：按 SSE 语义终止该响应，由 agent 自带重试落到健康供应商——任务级不中断。
- **被动熔断**：供应商连续 3 次失败进入打开（跳过、零连接），冷却 60s 起指数退避（上限 15 分钟），半开放行单探针恢复。
- **粘性首选**：`agw switch <名>` 后健康时优先路由，减少 prompt cache 失效。

### 协议转换（任意供应商可用）

客户端协议 × 供应商协议全组合（中立 IR + 三套编解码器）：

| 客户端 \ 供应商协议 | anthropic | openai-chat | openai-responses |
|---|---|---|---|
| **Claude Code (anthropic)** | 字节透传 | 翻译 | 翻译 |
| **Codex (openai-responses)** | 翻译 | 翻译 | 字节透传 |

- 请求/响应/流式 SSE 全量映射：消息、工具调用与结果、base64 图片、stop_reason、usage。
- 明确的降级：`cache_control` 在非 anthropic 上游丢弃（日志告警，仅影响成本）；thinking/reasoning best-effort；音视频块返回 400。
- `agw install codex` 写入 `wire_api = "responses"` 与响应存储禁用键（认得该键的旧版 codex 生效；新版 codex 默认即无状态）：每请求自包含，无 `previous_response_id` 依赖——这是"任意切换"的前提。

### 虚拟令牌 → 项目档案

| 令牌（config/local.toml，0600） | 档案 |
|---|---|
| `[gateway] default_token` | 全局供应商池 |
| `[projects.<名>] token` | 该项目覆盖（供应商子集/粘性/模型映射） |

`agw run claude --project X` 注入 `ANTHROPIC_BASE_URL` + `ANTHROPIC_AUTH_TOKEN`；`agw run codex` 注入 `AGW_API_KEY`。不传项目时按 cwd 推断。

## 配置参考

合并顺序：`config/default.toml` ← `config/local.toml`（密钥，gitignore）← `projects/<名>/agw.toml`（项目覆盖，提交到业务仓库）。

```toml
[gateway]
listen = "127.0.0.1:8787"   # 仅回环；改 0.0.0.0 启动时会显著告警
# default_token / admin_token 首次启动自动生成

[[providers]]
name = "relay"
protocol = "openai-chat"    # anthropic | openai-chat | openai-responses
base_url = "https://relay.example/v1"
api_key_env = "RELAY_KEY"   # 或 api_key（只应出现在 local.toml）
priority = 1                # 数字越小越优先
enabled = true

[providers.model_map]       # 请求模型 → 实际模型
"claude-sonnet-5" = "claude-sonnet-5-relay"

[providers.headers]         # 中转站需要的自定义头
"X-Title" = "agw"

# connect_timeout_sec = 5   # 连接超时
# first_byte_timeout_sec = 60
```

项目覆盖（`projects/demo/agw.toml`）：

```toml
[project]
providers = ["relay"]       # 只用这几家（按此顺序）；留空继承全局池
preferred = "relay"

[project.model_map]
"claude-sonnet-5" = "gpt-5.2"
```

## 命令一览

| 命令 | 说明 |
|---|---|
| `agw serve` / `start` / `stop` | 前台 / 后台（pidfile `.run/agw.pid`、日志 `.run/agw.log`）/ 优雅停止 |
| `agw status [--json]` / `logs [-f]` | 供应商熔断状态与计数 / 日志 |
| `agw provider list/add/remove/enable/disable/test` | 供应商池管理；`test` 探测 `/v1/models` 延迟 |
| `agw switch <名>` | 粘性首选 |
| `agw install claude\|codex` | npm 安装 + agent 配置安全合并（自动备份） |
| `agw run claude\|codex [-p 项目] [-- 参数]` | 项目上下文启动 agent |
| `agw project new/list` | 业务项目工作区（独立 git 仓库） |

热重载：配置变更后 `agw provider add/switch` 自动通知网关；也可 `kill -HUP <pid>`。坏配置保留旧配置继续服务。

## 安全

- 默认仅绑定 `127.0.0.1`；管理端点 `/__agw/metrics|reload` 需 admin 令牌（`healthz` 除外）。
- 密钥三选一（优先级从高到低）：`config/local.toml` 明文 `api_key`（0600）> 真实环境变量 > 根目录 `.env`（自动加载、已被 git 忽略、建议 0600；语法错误启动即报错，权限过宽有警告；见 `.env.example`）；提交版 `config/default.toml` 不含密钥。
- 日志与错误输出全部脱敏（令牌前 6 位 + `***`）；无遥测。
- 请求体 failover 重放缓冲上限 64MiB（超限 413，不触上游）。

## 回滚

- agent 配置：`~/.claude/settings.json` / `~/.codex/config.toml` 旁的 `.agw-backup-<时间戳>` 恢复（TOML 重写可能丢注释，以备份为准）。
- 网关自身：本仓库 feature 分支按任务提交，可逐提交回退。
- 临时绕过网关：删除 settings.json env 两个键 / 把 `model_provider` 改回原供应商即可。

## 开发

```bash
go build ./... && go vet ./... && go test -race ./...   # 全量验证
gofmt -l .                                               # 应为空
bash scripts/validate-workflow.sh --fast                 # 工作流校验
```

架构与决策详见 `openspec/changes/add-agent-gateway/design.md`；规格见同目录 `specs/`。

## 已知边界

- **Codex 跨协议场景的工具编排不可用（已接受边界）**：新版 Codex（实测 0.149.1）以 `additional_tools`/`namespace`/`custom` 形态在 input 中携带内置工具（exec 等）与 MCP 工具，超出 v1 的 function 型翻译面——跨协议（Codex→仅 chat/anthropic 供应商）时这些工具会被丢弃并记日志告警。**Codex 请配置 openai-responses 协议供应商**（同协议字节透传，不受影响）；Claude Code 全场景不受影响。
- Codex 的 `GET /v1/responses/{id}` 单响应拉取**未路由**（返回 404）；禁用响应存储后 Codex 不使用该路径，每请求自包含全量上下文。
- `count_tokens` 优先转发 anthropic 上游；不可用时返回本地粗估（字节/4），误差只影响上下文预算判断。
- v1 非目标：TUI/Web 控制台、主动拨测、负载均衡、Windows、计费统计、音视频多模态。
