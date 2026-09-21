# gateway-cli 规格

## Purpose

定义 agw CLI 的网关根发现、供应商池管理、生命周期命令、agent 启动和业务项目工作区入口。

## Requirements

### Requirement: 显式网关根校验
CLI SHALL 校验用户显式提供的 `--root` 或 `AGW_ROOT` 目录为有效网关根；无效根 SHALL 在加载配置或写盘前失败，且 SHALL NOT 自动创建该根下的配置、运行时或项目产物。

#### Scenario: 显式 root 缺少 config
- **WHEN** 用户执行任意需要网关根的 `agw` 子命令并传入没有 `config/` 子目录的 `--root`
- **THEN** 命令失败并提示该 root 不是有效网关仓库
- **AND** 该 root 下不新增 `config/local.toml` 或 `.run/` 文件

#### Scenario: 无效环境变量被显式 flag 覆盖
- **WHEN** `AGW_ROOT` 指向无效目录且用户传入有效 `--root`
- **THEN** CLI 使用显式 `--root` 并成功执行

### Requirement: 项目创建前置配置校验
`agw project new` SHALL 在创建项目目录、写入项目配置或初始化 Git 前成功加载网关配置；加载失败时命令 SHALL 失败且不留下项目目录。

#### Scenario: 配置解析失败
- **WHEN** 网关根的配置文件解析失败且用户执行 `agw project new demo`
- **THEN** 命令返回配置解析错误
- **AND** `projects/demo` 不存在

#### Scenario: 配置有效时保持创建行为
- **WHEN** 网关根配置有效且用户执行 `agw project new demo`
- **THEN** 继续创建项目目录、`agw.toml`、项目 token，并保持既有 Git 初始化行为

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

### Requirement: 安装示例 base_url 契约
项目安装与快速开始文档中的 OpenAI/中转站供应商示例 SHALL 使用不带 `/v1` 路径的 `base_url`，与网关自动拼接 `/v1/*` 的实现一致。

#### Scenario: 文档示例不会产生双重 v1
- **WHEN** 维护者检查 README 与 usage guide 中的供应商示例
- **THEN** OpenAI/中转站 `base_url` 示例不包含 `/v1` 路径后缀
### Requirement: 项目令牌与目录原子性
`agw project new` SHALL 先将项目 token 持久化到 `config/local.toml`，成功后再创建项目工件；任一后续项目创建步骤失败时 SHALL 删除本次创建的项目目录并回滚本次新增 token，且 SHALL NOT 覆盖既有同名项目 token。

#### Scenario: token 保存失败
- **WHEN** 配置有效但项目 token 写入 `config/local.toml` 失败
- **THEN** 命令返回保存错误
- **AND** `projects/<name>`、`agw.toml` 与 `.git` 均不存在

#### Scenario: 项目目录写入失败
- **WHEN** token 已写入但项目目录或 `agw.toml` 创建失败
- **THEN** 命令返回项目创建错误
- **AND** 本次写入的项目 token 从 `config/local.toml` 回滚
- **AND** 本次创建的项目目录被删除

#### Scenario: 同名项目 token 已存在
- **WHEN** `config/local.toml` 已包含同名项目的 token
- **THEN** `agw project new <name>` 失败且不覆盖既有 token
### Requirement: local 配置原子持久化
`SaveLocal` SHALL 通过同目录临时文件、同步落盘与原子 rename 持久化 `config/local.toml`；写入失败时 SHALL 保留既有文件内容并返回错误，成功后最终文件 SHALL 为完整新内容且权限 `0600`。

#### Scenario: 保存成功
- **WHEN** 调用 `SaveLocal` 保存有效配置
- **THEN** `config/local.toml` 完整包含新 TOML 内容
- **AND** 文件权限为 `0600`
- **AND** 不遗留临时文件

#### Scenario: 写入或替换失败
- **WHEN** 临时文件写入、sync、权限收紧或 rename 失败
- **THEN** `SaveLocal` 返回错误
- **AND** 已存在的 `config/local.toml` 内容保持不变
- **AND** 不产生可作为配置加载的半写目标文件
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
