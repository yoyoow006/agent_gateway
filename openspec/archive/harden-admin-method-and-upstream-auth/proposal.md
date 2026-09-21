模式: 严格
状态: 已归档

# 管理端点方法与上游认证头边界

## Why

两个安全边界与文档或最小权限预期不一致：

1. 文档约定 `agw reload` 触发 `POST /__agw/reload`，但管理处理器不限制 HTTP 方法；持有 admin token 的 GET 也会执行重载。虽然端点绑定本机且需要令牌，但安全操作应满足方法最小权限，避免跨方法请求、代理或未来暴露面扩展时扩大可触发面。
2. 供应商自定义 `headers` 在网关注入上游认证之后写入，可用 `Authorization` 或 `X-Api-Key` 覆盖上游密钥。这会使请求绕过网关认证策略、导致难以诊断的 401/403，并可能把配置值当作认证材料发送。

## What Changes

- 管理 reload 端点限制为 `POST`；其他方法在 admin 认证前返回 405 和 `Allow: POST`。
- 管理 metrics 端点限制为 `GET`；其他方法在 admin 认证前返回 405 和 `Allow: GET`。
- 上游自定义 headers 先于协议认证写入；认证注入最后执行，确保上游 `Authorization` / `X-Api-Key` 无法被 provider `headers` 覆盖。
- 增加回归测试锁定方法限制与认证头最终值。
- 更新配置/使用文档，说明自定义 headers 不能覆盖上游认证头。

## Impact

- `agw reload` 现有 POST 行为不变。
- `GET /__agw/reload`、跨方法 metrics 请求不再触发处理器。
- 若用户曾依赖 provider headers 覆盖认证头，该用法不再有效；应改为配置真实上游认证。
- 不改变普通模型转发、failover、熔断、管理 token 校验强度或端点监听地址。
