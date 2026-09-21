# gateway-cli Delta

## ADDED Requirements

### Requirement: 网关进程身份校验
agw SHALL 在向 pidfile 记录的进程发送信号前校验进程身份；PID 对应进程的启动时间或可执行文件路径与记录不一致时 SHALL NOT 发送信号，并 SHALL 清理失效 pidfile 且返回身份不匹配错误。

#### Scenario: PID 被复用
- **WHEN** pidfile 记录 pid/start-time/exe，但当前同 PID 进程的启动时间或 exe 不匹配
- **THEN** `agw stop` 不发送 SIGTERM
- **AND** pidfile 被清理
- **AND** 命令返回 PID 可能复用的明确错误

#### Scenario: 身份匹配
- **WHEN** pidfile 记录的 pid/start-time/exe 均匹配当前进程
- **THEN** `agw stop` 保持现有 SIGTERM 优雅停止行为

#### Scenario: 旧格式 pidfile
- **WHEN** pidfile 仅包含旧版纯 PID 格式
- **THEN** `agw stop` 不发送 SIGTERM
- **AND** 返回要求重建 pidfile / 重启网关的错误

### Requirement: 后台启动就绪判定
`agw start` SHALL 在子进程健康检查成功后才报告启动成功；子进程退出或健康检查超时 SHALL 报告失败并展示日志尾部。

#### Scenario: 健康检查成功
- **WHEN** 子进程启动后 `GET /__agw/healthz` 返回成功
- **THEN** `StartGateway` 返回成功并写入身份 pidfile

#### Scenario: 子进程退出
- **WHEN** 健康检查成功前子进程退出
- **THEN** `StartGateway` 返回失败
- **AND** 错误包含日志尾部
- **AND** 不保留 pidfile

#### Scenario: 健康检查超时
- **WHEN** 子进程未退出但 healthz 在超时时间内不可用
- **THEN** `StartGateway` 返回失败
- **AND** 错误包含日志尾部与超时原因

### Requirement: 身份 pidfile 格式
agw SHALL 将后台网关身份持久化为 JSON，至少包含 pid、进程启动时间、可执行文件路径和网关根，并 SHALL 保持文件权限 `0600`。

#### Scenario: 成功启动后写入
- **WHEN** `StartGateway` 成功
- **THEN** pidfile 包含 pid/start-time/exe/root 且权限为 0600
