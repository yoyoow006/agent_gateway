模式: 严格
状态: 已归档

# 网关进程身份与启动就绪

## Why

当前后台生命周期管理存在两个安全问题：

1. pidfile 只保存数字 PID，`agw stop` 只要 PID 存活就发送 SIGTERM。系统重启或 PID 复用后，可能误杀无关进程。
2. `agw start` 启动子进程后只等待 700ms；700ms 内未退出即报告成功。慢启动、稍后失败或监听尚未就绪时都会误报。

## What Changes

- pidfile 从纯 PID 改为 JSON 身份记录：
  - PID
  - 进程启动时间（Unix 秒，取自 `/proc/<pid>/stat`）
  - 可执行文件路径
  - 网关根
- 读取 pidfile 时兼容旧纯 PID 格式，但旧格式视为身份未知。
- `agw stop` 在发送 SIGTERM 前校验：
  - 进程存在；
  - 当前 `/proc/<pid>/stat` 启动时间与记录一致；
  - 当前可执行文件路径与记录一致（可读取时必须一致）。
- 任一身份校验失败时：
  - 不发送信号；
  - 删除失效 pidfile；
  - 返回“PID 身份不匹配 / 可能被复用”错误。
- 对旧格式 pidfile：
  - 不直接发送 SIGTERM；
  - 返回需要重启或人工确认的错误，避免继续依赖无身份的 PID。
- `StartGateway` 改为轮询 `GET /__agw/healthz`：
  - 子进程退出 → 立即失败并展示日志尾部；
  - healthz 成功 → 成功；
  - 超时（默认 5s）→ 失败并展示日志尾部；
  - 成功后写入身份 pidfile。
- 健康检查 URL 使用 `net.SplitHostPort` + `net.JoinHostPort` 规范化 IPv6 host。
- 注入 health checker 以便测试 ready / exited / timeout 路径，不依赖本地监听。

## Impact

- `agw stop` 不再可能因 PID 复用误杀无关进程。
- `agw start` 只有在 healthz 可用后才报告成功。
- 新 pidfile 格式包含本地网关根与进程身份信息；文件权限保持 0600。
- 旧纯 PID pidfile 不会被用于发信号，需要用户重启网关或确认后清理。
- 现有 status/reload 对“运行中”的判定同步使用身份校验。
- 不改变 serve 端口、HTTP API、热重载语义或优雅停机时长。
