# gateway-cli Delta

## MODIFIED Requirements

### Requirement: 安装与使用文档运行时语义
安装与使用文档 SHALL 准确描述项目供应商顺序、热重载生效边界与 failover 重试状态码，并 SHALL 与当前 `ResolveProfile()`、HTTP client 缓存和 `retryableStatus()` 行为一致。

#### Scenario: 项目 providers 子集语义
- **WHEN** 用户查看项目覆盖配置文档或项目模板注释
- **THEN** 文档说明 `providers` 列表定义启用候选子集，最终顺序仍按全局 `priority` 与同优先级名称排序
- **AND** 文档说明 `preferred` 只是在健康时把首选供应商置顶

#### Scenario: 超时配置生效边界
- **WHEN** 用户修改供应商的连接超时或首字节超时并触发热重载
- **THEN** 文档说明这些 timeout 修改需重启网关后生效
- **AND** 文档不将 timeout 归入可由热重载立即生效的 provider 字段

#### Scenario: failover 状态码清单
- **WHEN** 用户查看 README 或 usage guide 的故障切换说明
- **THEN** 文档列出精确可重试状态码 401、403、408、429、500、502、503、504、529
- **AND** 文档说明 501、505 等非清单 5xx 原样回传并终止当前请求链
