模式: 标准
状态: 构建中

# 文档对齐项目供应商顺序、热重载超时与 failover 状态码

## Why

代码审查确认三项文档与当前运行时行为不一致。用户已决定保持现有代码逻辑，仅更新文档：

1. F-003：项目 `providers` 列表用于筛选供应商子集，但最终顺序仍由全局 `priority` 与名称排序决定；文档目前写成“按此顺序”。
2. F-004：供应商 `connect_timeout_sec` / `first_byte_timeout_sec` 变化后热重载仍复用同名 HTTP client，需重启网关生效；文档目前统称 provider 字段热重载后生效。
3. F-005：failover 重试状态码是固定清单，不包含所有 5xx；README 与 usage guide 目前写“5xx”，与 protocol-flow 及实现不一致。

## What Changes

- 修改 README 与 usage guide 中项目 `providers` 的说明：列表定义候选子集，路由顺序仍按 provider `priority`、同优先级名称排序，`preferred` 只将健康首选置顶。
- 修改项目模板 `internal/workspace/project.go` 中对应注释，保持文案与行为一致；不改变 `ResolveProfile()` 排序实现。
- 修改热重载 / FAQ 文档：明确认证、协议、header、模型映射等配置热重载生效；连接超时与首字节超时修改后需重启网关生效。
- 修改 README 与 usage guide 的 failover 状态码描述，列出精确清单：401、403、408、429、500、502、503、504、529；明确 501/505 等非清单 5xx 原样回传。
- 增加文档契约测试，防止这三类语义再次漂移。

## Impact

- 文档与当前运行时行为一致，用户不再预期项目列表顺序可控制路由、所有 5xx 自动切换或 timeout 热重载即时生效。
- `internal/workspace/project.go` 仅修改注释，不改变可执行行为。
- 不修改 `ResolveProfile()`、HTTP client 缓存、`retryableStatus()` 或网关运行时逻辑。
