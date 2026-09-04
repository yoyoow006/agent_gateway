# 归档索引

- `add-agent-gateway` Go 本地大模型 API 路由网关 agw：三协议翻译、请求边界 failover、被动熔断、虚拟令牌项目档案、Claude Code/Codex 一键安装启动、projects/ 工作区（标准模式）
- `support-env-file-keys` .env 密钥环境文件自动加载（真实环境优先、语法错误快速失败、gitignore/example）（标准模式）
- `isolated-agent-configs` agent 启动改独立配置：claude --settings、codex -p agw，零接触用户默认配置（标准模式）
- `fix-responses-total-tokens` responses 跨协议输出补 total_tokens 与 response.* 事件名前缀（Codex 0.149+ 严格校验兼容），解码器双名兼容（标准模式）
- `translate-additional-tools` Codex additional_tools/namespace/custom 工具编排跨协议翻译（namespace 展平为点连名、function 直取 schema、custom 合成 `{code:string}` schema、响应/历史还原 custom_tool_call），解除 NEW-01 边界（标准模式）
- `protocol-internal-cleanup` chat 解码缺 index 容错、错误辅助下沉 internal 分表、ErrStatus 死字段删除、协议流程文档（标准模式）
