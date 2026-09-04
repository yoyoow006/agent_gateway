# agw 编译安装与使用指南

> agw（agent gateway）是本地大模型 API 路由网关：Claude Code 与 Codex 一次性指向网关，
> 网关负责**协议转换、请求边界故障切换、被动熔断与项目档案路由**——换供应商不重启 agent，正在运行的任务不中断。
>
> 快速上手可先看根目录 [README.md](../README.md)，本文是完整的安装与使用说明。

## 目录

1. [环境要求](#1-环境要求)
2. [编译安装](#2-编译安装)
3. [配置](#3-配置)
4. [启动与停止网关](#4-启动与停止网关)
5. [安装与启动 Claude Code / Codex](#5-安装与启动-claude-code--codex)
6. [业务项目工作区](#6-业务项目工作区)
7. [日常运维](#7-日常运维)
8. [故障切换行为说明](#8-故障切换行为说明)
9. [协议转换矩阵与已知边界](#9-协议转换矩阵与已知边界)
10. [安全说明](#10-安全说明)
11. [故障排查 FAQ](#11-故障排查-faq)
12. [卸载与回滚](#12-卸载与回滚)

---

## 1. 环境要求

| 组件 | 要求 | 用途 |
|---|---|---|
| Go | ≥ 1.24（开发实测 1.24.11） | 编译网关 |
| Node.js + npm | ≥ 18 | `agw install` 安装 claude/codex（仅安装时需要） |
| git | 任意近期版本 | 业务项目版本管理（可选） |
| 操作系统 | Linux x64 优先 | Windows 为 v1 非目标 |

不需要 root；网关默认只监听 `127.0.0.1:8787`。

## 2. 编译安装

### 2.1 获取与编译

```bash
git clone <你的仓库地址> agent_gateway   # 或直接进入现有仓库目录
cd agent_gateway

go build -o agw ./cmd/agw        # 产出可执行文件 agw
./agw --help                     # 验证
```

### 2.2 放进 PATH（可选，推荐）

```bash
mkdir -p ~/bin && cp agw ~/bin/
echo 'export PATH="$HOME/bin:$PATH"' >> ~/.bashrc && source ~/.bashrc
agw --help
```

> 网关仓库根目录的识别：命令从当前目录向上探测 `config/default.toml` 或 `go.mod`；
> 也可以用 `--root <目录>` 或环境变量 `AGW_ROOT` 显式指定。下文均假设你在仓库根目录执行。

### 2.3 编译验证（开发者）

```bash
go build ./... && go vet ./... && go test -race ./...   # 应全绿
gofmt -l .                                               # 应无输出
```

## 3. 配置

### 3.1 配置文件与合并顺序

```
config/default.toml   ← config/local.toml（密钥，0600，不入 git） ← projects/<名>/agw.toml（项目覆盖）
```

后者覆盖前者；`config/default.toml` 是提交版示例（无密钥），首次 `agw start` 会自动在
`config/local.toml` 生成**admin 令牌**和**全局默认令牌**两类；
业务项目令牌由 `agw project new <名>` 在创建时单独生成（也在 `config/local.toml`）。

### 3.2 供应商池

```bash
# anthropic 协议（官方/中转）
agw provider add official --protocol anthropic --base-url https://api.anthropic.com \
    --api-key-env OFFICIAL_KEY --priority 10

# openai-chat 协议（绝大多数中转站）
agw provider add relay --protocol openai-chat --base-url https://relay.example/v1 \
    --api-key-env RELAY_CHAT_KEY --priority 1 \
    --model claude-sonnet-5=claude-sonnet-5-relay \
    --header X-Title=agw

# openai-responses 协议（新版 Codex 必需，见第 9 节）
agw provider add openai --protocol openai-responses --base-url https://api.openai.com/v1 \
    --api-key-env OPENAI_KEY --priority 20
```

- `--protocol`：`anthropic | openai-chat | openai-responses`（三选一，必填）
- `--priority`：数字越小越优先；请求按此顺序逐家尝试
- `--model from=to`：请求模型名 → 该供应商实际模型名（可重复）；缺省透传
- `--header K=V`：附加给上游的自定义头（部分中转站需要，可重复）
- 密钥：`--api-key-env VAR`（推荐）或 `--api-key`（明文只写入 0600 的 local.toml）
- 探测连通性：`agw provider test <名称>`（GET `/v1/models`，报告延迟）

### 3.3 密钥三选一（优先级从高到低）

1. `config/local.toml` 明文 `api_key`（0600）
2. 真实环境变量（export / 容器注入）
3. **`.env` 文件**（仓库根目录，自动加载，已被 git 忽略）

```bash
cp .env.example .env && chmod 600 .env
# 编辑 .env：
#   OFFICIAL_KEY=sk-ant-xxx
#   RELAY_CHAT_KEY=sk-xxx
```

`.env` 规则：`KEY=VALUE`、`#` 注释、可选引号、可选 `export ` 前缀；语法错误启动即报错（带行号）；
权限宽于 0600 有警告；**只在网关启动时加载一次**，改完需 `agw stop && agw start`。

### 3.4 项目覆盖（可选）

`projects/<名>/agw.toml`（提交到业务仓库，不含密钥）：

```toml
[project]
providers = ["relay"]       # 只用这几家（按此顺序）；留空继承全局池
preferred = "relay"         # 粘性首选

[project.model_map]
"claude-sonnet-5" = "gpt-5.2"
```

## 4. 启动与停止网关

```bash
agw start            # 后台启动（分离进程；pidfile .run/agw.pid，日志 .run/agw.log）
agw status           # 运行状态 + 供应商熔断/计数表（--json 机器可读）
agw status --json
agw logs -f          # 跟随日志
agw stop             # 优雅停止（SIGTERM，排空在途请求，上限 15s）
agw serve            # 前台运行（调试用；Ctrl-C 退出）
```

- 端口被占用时 `agw start` **立即报错**并给出日志尾部。
- 配置热重载：`agw provider add/remove/enable/disable`、`agw switch` 会自动通知网关；
  也可 `kill -HUP $(cat .run/agw.pid)`。**坏配置保留旧配置继续服务**。
- 监听地址默认 `127.0.0.1:8787`（`config/local.toml` 的 `[gateway] listen` 可改）；
  改成非回环地址启动时会显著告警。

## 5. 安装与启动 Claude Code / Codex

### 5.1 一键安装（零接触你的默认配置）

```bash
agw install claude    # npm i -g @anthropic-ai/claude-code + 预生成 .agw/ 目录
agw install codex     # npm i -g @openai/codex + 生成 $CODEX_HOME/agw.config.toml
```

**agw 不读取、不修改、不备份 `~/.claude/settings.json` 与 `~/.codex/config.toml`。**

### 5.2 在项目里启动

```bash
agw project new demo          # 创建 projects/demo（git init + agw.toml 模板 + 项目令牌）
agw run claude --project demo # → claude --settings <根>/.agw/claude-settings.demo.json
agw run codex  --project demo # → codex -p agw（+ 环境变量 AGW_API_KEY=<demo 项目令牌>）
agw run codex  -- --model gpt-5.2   # `--` 之后的参数原样透传给 agent
```

- 不带 `--project` 时按当前目录推断（cwd 在 `projects/<名>` 下即用该项目，否则全局档案）。
- 进程以 exec 替换方式启动：Ctrl-C 直达 agent，无中间进程。
- **Claude Code**：独立 settings 文件（0600，每次启动按当前项目令牌重写）作为叠加层——
  你自己的 `~/.claude/settings.json` 继续生效，仅 `ANTHROPIC_BASE_URL`/`ANTHROPIC_AUTH_TOKEN` 两个 env 键被覆盖。
- **Codex**：`-p agw` 使用独立 profile 文件 `$CODEX_HOME/agw.config.toml`（`CODEX_HOME` 环境变量优先），
  叠加在你的 `config.toml` 之上；裸跑 `codex`（不带 `-p`）不受任何影响。

## 6. 业务项目工作区

```bash
agw project new <名称>   # 名称规则 ^[a-z0-9][a-z0-9_-]*$；目录 + git init + agw.toml 模板 + 令牌
agw project list         # 名称/分支/脏状态/覆盖配置摘要
```

业务项目位于 `projects/` 下，各自是**独立 git 仓库**；网关仓库的 `.gitignore` 已排除
`projects/`、`.run/`、`config/local.toml`、`.env`、`.agw/`，两边互不污染。

## 7. 日常运维

```bash
agw provider list                        # 查看供应商池
agw provider test relay                  # 探测某家延迟与连通性
agw provider disable relay               # 临时停用
agw provider enable relay
agw provider remove relay
agw switch relay                         # 设粘性首选：健康时优先路由（减少 prompt cache 失效）
agw status                               # 各供应商状态：closed/open/half-open + 请求/失败计数
```

## 8. 故障切换行为说明

**对正在运行的 agent 完全无感**，规则如下：

- **首字节前失败**（连接失败、超时、408/429/5xx/529、上游 401/403）：自动换下一优先级供应商并
  **重放原请求**——客户端收到的就是健康供应商的成功响应。
- **流建立后中断**：按 SSE 语义输出协议错误帧并终止该响应，agent 自带重试会把下一个请求
  落到健康供应商——任务级不中断。
- **被动熔断**：某家连续 3 次失败 → 打开（跳过、零连接），冷却 60s 起指数退避（上限 15 分钟），
  半开放行单探针，成功即恢复。热重载不会复位冷却中的熔断。
- **全部失败**：返回最后一次真实上游错误（保留状态码与 `Retry-After`）。
- 请求体缓冲上限 64MiB（failover 重放需要），超限返回 413 不触上游。

## 9. 协议转换矩阵与已知边界

| 客户端 \ 供应商协议 | anthropic | openai-chat | openai-responses |
|---|---|---|---|
| **Claude Code (anthropic)** | 字节透传 | 翻译 | 翻译 |
| **Codex (openai-responses)** | 翻译 | 翻译 | 字节透传 |

- 同协议永远**字节级透传**（仅替换认证/Host；配了 `model_map` 时只重写 model 字段），保真度最高。
- 跨协议翻译覆盖：消息、工具调用与结果、base64 图片、stop_reason、usage、错误体、SSE 流式事件。
- 明确降级：`cache_control` 跨协议丢弃（日志告警，仅影响成本）；thinking/reasoning best-effort。
- **Codex ≥0.149 跨协议工具编排（已支持，2026-09）**：Codex 的内置 exec、MCP 工具以及
  `code_mode` 的 V8 JS 沙箱（`functions.exec`）通过 input 的 `additional_tools`（namespace
  树内嵌 function/custom）携带；网关跨协议时做翻译：namespace 展平为点连名、function 直取 schema、
  custom 合成 `{code:string}` schema，响应/历史还原 `custom_tool_call`（详见
  `openspec/archive/translate-additional-tools/`）。同协议（Codex↔openai-responses）仍走字节级透传，零开销。
- Codex 0.149+ 对 Responses 流式 `usage` 缺 `total_tokens` 严格校验（兼容处理：编码边界补 `input+output`）；
  item/part 事件名 `response.` 前缀在编码器统一、解码器双名兼容（兼容无前缀历史流）。
- `count_tokens` 优先转发 anthropic 上游，不可用时本地粗估（字节/4，CJK 混合场景的保守近似，只影响上下文预算判断）。
- `GET /v1/responses/{id}` 单响应拉取未路由（禁用响应存储后 Codex 不使用该路径）。

## 10. 安全说明

- 默认仅绑定回环 `127.0.0.1`；管理端点 `/__agw/metrics|reload` 需 admin 令牌（`healthz` 除外）。
- 密钥三处落盘均 0600 且不入 git：`config/local.toml`、`.env`、`.agw/`（claude settings 含项目令牌）。
- 日志与错误输出全程脱敏（令牌前 6 位 + `***`）；无任何遥测。
- 虚拟令牌即路由身份：全局令牌（`[gateway] default_token`）、项目令牌（`[projects.<名>] token`）、
  admin 令牌，均首次启动自动生成，泄露即"换一把"：删除对应行后重启会重新生成。

## 11. 故障排查 FAQ

| 现象 | 原因与处理 |
|---|---|
| agent 报 401 "未知或缺失虚拟令牌" | 没经 `agw run` 启动或令牌被改过；重新 `agw run ...`，或查 `config/local.toml` 令牌 |
| 401 但上游密钥正常 | 项目 `agw.toml` 引用了不存在的供应商（503 配置错误文案会点名）；`agw provider list` 核对 |
| 503 "档案内没有启用的供应商" | 全部被 disable 或项目覆盖列表为空；`agw provider list` 检查 |
| 429/529 频繁 | 正在故障切换；`agw status` 看哪家 open、`agw logs -f` 看切换记录；考虑加备用家 |
| `agw start` 报 address already in use | 端口被占（错误含日志尾部）；`agw stop` 后重试，或改 `[gateway] listen` |
| Codex 工具调用失败/丢工具 | Codex ≥0.149 跨协议工具编排已自动翻译（`additional_tools`/namespace/custom，见第 9 节）。若仍异常，检查目标供应商是否真支持对应工具；日志里若见"翻译降级 …"说明上游该字段无对应。 |
| 日志里"翻译降级 cache_control …" | 正常：cache_control 跨协议丢弃，只影响缓存成本 |
| 改了 .env 不生效 | `.env` 只在启动时加载：`agw stop && agw start` |
| 改超时/协议/header 不生效 | 这些是 provider 字段，`agw provider add <同名>` 会自动通知网关热重载（无需重启）。`.env` 里的密钥例外——只在 `agw start` 时加载一次，改完需 `agw stop && agw start` |

## 12. 卸载与回滚

```bash
agw stop                                  # 停网关
rm -f "$CODEX_HOME/agw.config.toml"       # 删 codex 独立 profile（CODEX_HOME 缺省 ~/.codex）
rm -rf <网关根>/.agw                      # 删 claude 独立 settings（含项目令牌）
npm rm -g @anthropic-ai/claude-code @openai/codex   # 如需连 agent 一起卸载
```

你的 `~/.claude/settings.json` 与 `~/.codex/config.toml` 从未被 agw 修改，无需恢复；
裸跑 `claude` / `codex` 即回到你自己的配置。业务项目在 `projects/` 下独立成仓，删网关目录不影响。

---

*本文命令与行为对应 agw 当前 main 分支实现；规格真源见 `openspec/specs/`，架构决策见归档的 design。*
