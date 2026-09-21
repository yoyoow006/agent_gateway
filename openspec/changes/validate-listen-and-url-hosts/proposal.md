模式: 标准
状态: 构建中

# 校验监听地址并统一 URL Host 格式

## Why

`gateway.listen` 当前只在 `http.Server.ListenAndServe()` 时才由网络层解析，CLI 与 agent 配置生成则直接拼接 `http://`。IPv6 回环必须写成 `[::1]:8787` 才有效，但无括号 `::1:8787` 或缺 port 会在配置加载后被静默接受，直到 serve/admin/agent URL 生成阶段才失败，错误离根因远且难以诊断。同时 admin URL、Claude settings 与 Codex profile 分散手写 URL，容易再次出现 host 格式差异。

## What Changes

- 配置加载时校验 `gateway.listen`：
  - 必须能用 `net.SplitHostPort` 解析；
  - port 必须非空且为合法数字；
  - host 允许 IPv4、IPv6（带括号）、hostname、空 host；
  - IPv6 不做自动加括号，解析失败时返回明确错误并提示示例 `[::1]:8787`。
- 新增共享 URL 构造函数：
  - 解析 listen；
  - `net.JoinHostPort` 重组 host/port；
  - 返回 `http://<hostPort>`。
- `adminURL()` 使用共享构造。
- Claude `ANTHROPIC_BASE_URL` 使用共享构造。
- Codex `base_url` 使用共享构造 + `/v1`。
- 保留生命周期 healthz 现有 `SplitHostPort + JoinHostPort` 语义，可复用共享构造。
- 更新 README / usage guide 的 IPv6 listen 示例与说明。
- 增加配置校验、URL 构造和文档契约测试。

## Impact

- 无效 listen 在配置加载阶段 fail-fast，不再延迟到启动或配置生成。
- 标准与 IPv6 listen 生成合法本地 URL。
- 无括号 IPv6 / 缺 port 明确报错。
- 不改变默认 `127.0.0.1:8787`、非回环告警或代理转发行为。
