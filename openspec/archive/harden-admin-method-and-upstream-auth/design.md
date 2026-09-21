# design · harden-admin-method-and-upstream-auth

## 决策

### D1: 方法检查先于 admin 认证
- 决策：对 reload/metrics 包装 `requireMethod`，在 `admin()` 前执行。
- 理由：方法不匹配是路由/协议错误，不应消耗或暴露 admin token 校验路径；最小权限顺序更清晰。
- 替代：在处理器内部检查方法。弃用，因为认证 wrapper 仍会先执行且职责分散。

### D2: 每个端点显式声明唯一方法
- 决策：reload=POST，metrics=GET；405 响应设置 `Allow`。
- 理由：与现有 CLI/文档行为一致，避免引入 OPTIONS/HEAD 语义或缓存面。
- 替代：允许 HEAD metrics。弃用，当前无消费者，避免未确认行为。

### D3: 自定义 headers 先写，认证最后写
- 决策：调整 `attempt()` 顺序为：基础转发头 → 构建头 → provider 自定义 headers → 协议认证头。
- 理由：认证是网关安全边界，必须是最终值；非认证自定义头仍保留。
- 替代：过滤认证键。弃用，大小写/规范化逻辑易漏，且无法表达“网关注入优先”的清晰不变量。

## TDD / 验证计划

1. 为 handler 方法限制新增不依赖本地监听的 `httptest.NewRequest + ResponseRecorder` 测试，覆盖 GET reload、POST metrics、405/Allow。
2. 为 `attempt()` 注入 `http.RoundTripper`，覆盖：
   - Anthropic provider headers 试图设置 `X-Api-Key`，最终仍为真实密钥；
   - 非 Authorization 的自定义头仍发送；
   - OpenAI provider headers 试图设置 `Authorization`，最终仍为 Bearer 真实密钥。
3. 跑 gateway 目标测试、全项目 build/vet/gofmt、workflow fast 与 OpenSpec strict。
4. 审查重点：方法检查顺序、Allow 大小写、Anthropic 与 OpenAI 两类认证、跨协议 extraHeader 是否仍可提供必要协议头且不被误覆盖。

## 风险与边界

- 极少数用户可能依赖 provider headers 覆盖认证头；这是安全边界修复，需在文档中明确迁移。
- `extraHeader` 中协议构建头（如 Content-Type、Anthropic-Version）在自定义 headers 之前设置，仍可被 provider headers 覆盖；本变更只保护认证头。
- 管理端点仍绑定本机且要求 token；方法限制是纵深防御，不替代网络边界。
