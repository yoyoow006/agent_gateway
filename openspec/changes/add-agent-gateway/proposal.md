# add-agent-gateway

模式: 标准
状态: 待归档

## Why

本机同时驱动 Claude Code 与 Codex 时，上游大模型供应商（官方 API、各类中转站）经常限流、超时或临时不可用，且并非每家都同时暴露 Anthropic 与 OpenAI Responses 协议。现有切换工具（cc-switch 直写模式）要改写 `~/.claude/settings.json` 并重启 agent 才能换供应商，正在运行的长任务会被打断。

本变更在 agent 与供应商之间增加一个本地路由网关 `agw`（Go 实现）：agent 一次性指向网关，网关在请求边界自动切换健康供应商，并在上游协议与客户端协议不一致时自动翻译适配，实现"不重启 agent、任务不中断、任意供应商可用"；同时补齐两个 agent 的一键安装/启动和业务项目工作区管理。

### 已确认的需求共识

- 协议适配（修订后）：网关内置协议转换。客户端协议（Anthropic Messages / OpenAI Responses）与供应商协议（anthropic / openai-chat / openai-responses）不一致时自动翻译；一致时走字节级透传快路径。Codex 侧必须完整支持 Responses API（最新版 Codex 只说该协议），且要能用仅有 Chat Completions 或仅有 Anthropic 协议的供应商。
- 切换语义：请求边界 failover——首字节前（连接失败/超时/408/429/5xx/529）自动切下一优先级供应商并重放请求；失败供应商被动熔断+冷却；流中失败返回错误，由 agent 自带重试落到健康供应商。转换路径不依赖会话状态，保证任意切换。
- 管理形态：纯 CLI（`agw` 子命令 + `status` 表格/JSON）。
- 配置粒度：全局供应商池 + 每项目覆盖（`projects/<名>/agw.toml`），`agw run` 启动时自动应用项目配置。
- 平台假设：Linux x64 优先；npm 为安装前置；密钥存本地 0600 配置不入 git；网关仅绑定 127.0.0.1；网关仓库本身 git init 并用 feature 分支开发。

## What Changes

- 新建 Go 模块（`go.mod`、`cmd/agw`、`internal/{cli,config,gateway,provider,protocol,agent,workspace}`），当前仓库无任何 Go 代码，全部为新增。
- 协议层：中立中间表示（IR）+ 三套编解码器（anthropic、openai-chat、openai-responses），每套含请求/响应解析构建与 SSE 流式解码/编码；覆盖请求映射（system/messages/tools/tool_choice/参数/图片 base64）、stop_reason 与 usage 映射、错误体映射；黄金样例保证保真度。
- 网关核心：本地 HTTP 服务暴露 Anthropic 协议（`/v1/messages`、`/v1/messages/count_tokens`）与 OpenAI 协议（`/v1/responses`、`/v1/chat/completions`、`/v1/models`）端点；虚拟令牌→项目档案路由；优先级 failover + 被动熔断；同协议透传快路径；SSE 即时透传；认证注入与模型名映射。
- 运维 CLI：`agw serve/start/stop/status/logs`、`agw provider list/add/remove/test/enable/disable`、`agw switch`（粘性首选）、配置三层合并与热重载。
- agent 适配：`agw install claude|codex`（npm 安装 + 安全合并 agent 配置，备份、仅动自有键；codex 侧写入 `wire_api="responses"` 并禁用响应存储以保证无状态可切换）、`agw run claude|codex [--project]`（项目目录 + 项目令牌环境变量 + exec 替换进程）。
- 项目工作区：`agw project new/list`，业务项目位于 `projects/`，独立 git 仓库，网关仓库 `.gitignore` 隔离 `projects/`、`.run/`、`config/local.toml`。
- 新增 `README.md` 快速开始文档。

## Impact

- 全新子系统，不修改既有工作流资产（`.ai/`、`openspec/` 现有规格、`scripts/`）。
- 协议转换是最高风险点：工具调用、流式事件序列、usage/缓存字段映射可能存在保真度损失（如 `cache_control` 在非 Anthropic 上游被丢弃并记警告）；用黄金样例测试与透传快路径（同协议零解析）控制。
- 用户目录副作用限于 `agw install` 执行时：写 `~/.claude/settings.json`、`~/.codex/config.toml`（先备份，仅合并自有键；TOML 重写可能丢注释，备份兜底）。
- 仓库从纯脚手架变为 Go 工程；需要 git init 建立基线后开 feature 分支（标准模式默认路径）。
- 非目标（v1）：TUI/Web 控制台、主动健康拨测、负载均衡（仅优先级+failover）、Windows、用量计费统计、多用户、音视频等多模态块（文本/工具/图片之外给出明确错误）。
