# agent-gateway（agw）项目卡

稳定架构事实（权威来源：openspec/specs/ 五能力主规格、README.md、internal/ 源码）。

- 定位：本地大模型 API 路由网关（Go ≥1.22），默认监听 127.0.0.1:8787；Claude Code 走 /v1/messages(+count_tokens)，Codex 走 /v1/responses，另有 /v1/chat/completions 与 /v1/models。
- 分层：cmd/agw（cobra 入口）→ internal/cli（serve/start/stop/status/logs/provider/switch/install/run/project）→ internal/gateway（路由、failover、翻译管线、/__agw/* 管理）→ internal/provider（熔断+指标）、internal/protocol（IR+SSE）与三协议编解码（anthropic/openaichat/openairesponses）、internal/config（TOML 三层合并）、internal/agent（npm 安装+配置安全合并+exec 启动）、internal/workspace（projects/ 工作区）。
- 核心语义：虚拟令牌→项目档案；同协议字节透传（model_map 时仅重写 model 字节）；跨协议经 IR 翻译；请求边界 failover（408/429/5xx/529/401/403/网络失败换家重放，64MiB 缓冲上限 413）；被动熔断（3 败→60s 指数冷却上限 15m→半开单探针，热重载不复位）；粘性首选；Codex additional_tools/namespace/custom 工具编排跨协议经展开翻译（function 直取 schema、custom 合成 `{code:string}`）送达上游，响应/历史路径还原 custom_tool_call（无会话状态，名单随请求 IR 携带）。
- 构建/验证：`go build ./... && go vet ./... && go test -race ./...`、`gofmt -l .` 空、`bash scripts/validate-workflow.sh`。
- 密钥红线：明文只允许 0600 的 config/local.toml；config/default.toml 无密钥；日志脱敏。
