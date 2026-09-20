---
project: agent_gateway
kind: go-cli
primary_domains: [llm-api-routing, protocol-translation, agent-launcher]
related_domains: [gateway-cli, project-workspace, provider-failover]
last_verified: 2026-09-20
verified_commit: b35d30936777e83d53290c5c93ca9fc317a04a31
sources:
  - go.mod
  - cmd/agw/main.go
  - config/default.toml
  - internal/config/config.go
  - internal/gateway/server.go
  - internal/gateway/forward.go
  - internal/protocol/codec.go
  - internal/provider/breaker.go
  - internal/agent/run.go
  - internal/workspace/project.go
  - README.md
  - docs/usage-guide.md
---

# agent_gateway 项目卡

> agw 是 Go 编写的本地大模型 API 路由网关，用于让 Claude Code 与 Codex 指向同一本地入口，并由网关完成供应商选择、协议转换、故障切换与项目档案路由。

## 职责

- 负责：
  - 以 `agw` CLI 启动本地网关并管理供应商池、项目工作区与 agent 启动配置。
  - 在 Claude Code / Codex 与 Anthropic、OpenAI Chat、OpenAI Responses 供应商之间做同协议透传与跨协议翻译。
  - 在请求边界按供应商链执行 failover，并用被动熔断跳过连续失败供应商。
  - 把虚拟令牌解析为全局或项目档案，隔离不同业务项目的供应商子集、首选和模型映射。
  - 生成 Claude / Codex 独立配置并以项目上下文启动 agent，不修改用户默认配置文件。
- 不负责：
  - TUI/Web 控制台、主动拨测、负载均衡、Windows 支持、计费统计和音视频多模态。
  - 代理 `GET /v1/responses/{id}` 单响应拉取；Codex 按无响应存储模式自包含上下文。
  - 替代业务项目仓库或其构建、发布流程。

## 高频入口

- `cmd/agw/main.go` / `main` — 程序入口；调用 `internal/cli.Execute()`，并把 `ErrReloadFailed` 映射为退出码 2。
- `internal/cli/` — Cobra 命令树：`serve/start/stop/status/logs`、`provider`、`reload`、`install`、`run`、`project`。
- `internal/config/config.go` / `Load`, `FindRoot`, `ResolveProfile` — 配置三层合并、网关根发现、令牌到档案的解析。
- `internal/gateway/server.go` / `Server.New`, `Server.Handler` — HTTP 路由、认证、健康检查、指标、热重载与配置快照。
- `internal/gateway/forward.go` / `forward` — 供应商链尝试、可重试错误切换、同协议透传与跨协议翻译。
- `internal/protocol/` — 中立 IR、SSE 读写和三套协议编解码器；根包 `Codec` 定义请求、响应、流式与错误映射接口，`anthropic`、`openaichat`、`openairesponses` 子包分别实现三种线上协议。
- `internal/provider/breaker.go` / `Breaker`, `Registry` — 每供应商连续失败计数、指数退避冷却、半开探针与运行指标。
- `internal/agent/install.go`, `internal/agent/run.go` — 安装 Claude/Codex、生成独立配置、解析项目令牌并 exec 启动 agent。
- `internal/workspace/project.go` / `New`, `List` — `projects/<名>/` 业务工作区创建、git 初始化和列表。
- `config/default.toml` — 提交版默认配置与配置语义说明；密钥不得写入本文件。

## 跨仓库关系

- 上游依赖：
  - Go 构建依赖声明于 `go.mod`：Burntushi TOML 解析、Cobra 命令行框架、pflag 参数解析。
  - 安装/运行 Claude Code 与 Codex 依赖外部 npm 包和用户机器上的 Node.js 环境；本仓库不锁定这些外部 CLI 的版本。
- 下游消费者：
  - Claude Code 经独立 `--settings` 文件指向 agw。
  - Codex 经 `$CODEX_HOME/agw.config.toml` 的 `agw` profile 指向 agw。
  - `projects/<名>/` 中创建的业务项目是运行时项目档案消费者；当前仓库未登记具体业务项目。
- 共享契约：
  - 客户端端点：`POST /v1/messages`、`POST /v1/messages/count_tokens`、`POST|GET /v1/responses`、`POST /v1/chat/completions`、`GET /v1/models`。
  - 管理端点：`GET /__agw/healthz`、`GET /__agw/metrics`、`POST /__agw/reload`；管理端点要求 admin 令牌。
  - 供应商协议枚举：`anthropic`、`openai-chat`、`openai-responses`。

## 搜索锚点

- 业务词：
  - `failover`、`故障切换`、`供应商切换`。
  - `protocol translation`、`协议转换`、`IR`。
  - `virtual token`、`虚拟令牌`、`project profile`、`项目档案`。
  - `passive breaker`、`被动熔断`、`half-open`、`半开探针`。
  - `hot reload`、`热重载`。
  - `agent launcher`、`agent 启动器`。
- 类型 / 包名：
  - `config.Config`、`config.Provider`、`config.Profile`。
  - `gateway.Server`。
  - `protocol.Codec`、`protocol.Request`、`protocol.Response`。
  - `provider.Breaker`、`provider.Registry`。
  - `agent.PrepareExec`、`agent.Exec`。
  - `workspace.New`、`workspace.List`。
- 特例：
  - 仓库 / Go module 名为 `agent_gateway`，CLI 与服务名使用 `agw`；不要把二者当成两个项目。
  - `config/` 顶层目录是配置资产；配置业务代码位于 `internal/config/`。
  - `internal/protocol/` 下还有 `anthropic`、`openaichat`、`openairesponses` 协议子包，用户提到的 `internal` 是业务代码根而不是单一包。
  - `projects/` 是运行时业务项目目录，不等同于本仓库的项目登记 `.ai/kb/projects/`。

## 验证入口

- 构建：`go build ./...`
- 测试：`go vet ./... && go test -race ./...`
- 格式：`gofmt -l .`（期望无输出）
- 工作流：`bash scripts/validate-workflow.sh --fast`
- 证据：[`../verification-evidence.md`](../verification-evidence.md)
- 基线：frontmatter 的 `verified_commit` 只表示登记证据对应 commit；它不证明当前工作区、当前任务或后续 commit 已通过验证。

## 证据与维护

- 事实来源：
  - `go.mod`：模块名、Go 版本与构建依赖。
  - `cmd/agw/main.go`、`internal/cli/`：入口和命令面。
  - `internal/config/config.go`、`config/default.toml`：配置结构、合并、根发现与默认语义。
  - `internal/gateway/server.go`、`internal/gateway/forward.go`：端点、认证、failover 与翻译路径。
  - `internal/protocol/codec.go` 及协议子包：中立 IR 与编解码契约。
  - `internal/provider/breaker.go`：熔断状态机。
  - `internal/agent/`、`internal/workspace/`：agent 启动与项目工作区。
  - `README.md`、`docs/usage-guide.md`：用户可见行为、安全边界和已知边界。
- 最近复核：
  - 2026-09-20 · `b35d30936777e83d53290c5c93ca9fc317a04a31` · 复核入口、模块、配置、路由、协议、熔断、agent 与工作区事实；本登记本身不修改运行时。构建与 `go vet` 在本地沙箱通过；`go test -race ./...` 因沙箱禁止本地监听端口且 `FindRoot` 受外部仓库环境影响失败，不能作为全量测试通过证据。
- 残余不确定：
  - 未验证真实外部供应商可用性、npm 安装网络行为、Claude Code / Codex 特定版本的全部兼容性，以及 Windows 运行行为；这些均不属于当前本地构建与测试覆盖。
