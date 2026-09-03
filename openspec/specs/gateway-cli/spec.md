# gateway-cli 规格

## Purpose

本能力定义 agw 命令行对网关生命周期、供应商管理、粘性切换、配置三层合并与热重载、安全基线（回环绑定、admin 令牌、日志脱敏）的行为。

## Requirements

### Requirement: 网关生命周期管理
`agw serve` SHALL 前台运行网关；`agw start` 以分离进程启动（写 `.run/agw.pid` 与 `.run/agw.log`）；`agw stop` 发送 SIGTERM 并等待优雅退出（排空在途请求，默认上限 15s）；`agw status` 展示运行状态、监听地址、各供应商熔断状态与请求/失败计数（支持 `--json`）；`agw logs [-f]` 查看日志。

#### Scenario: 启动与停止
- **WHEN** `agw start` 后执行 `agw status` 再 `agw stop`
- **THEN** status 先显示运行中（pid、端口、供应商表），stop 后 pid 失效、端口释放

#### Scenario: 重复启动
- **WHEN** 网关已运行时再次 `agw start`
- **THEN** 报错退出且不影响现有进程

### Requirement: 供应商管理与粘性切换
`agw provider list/add/remove/enable/disable/test` SHALL 管理供应商条目（写回 `config/local.toml` 并触发热重载）；`agw provider test <名>` 对该供应商发最小探测请求并报告延迟与错误；`agw switch <名>` 设置档案粘性首选（健康时优先于优先级，减少 prompt cache 失效）。

#### Scenario: 添加供应商即生效
- **WHEN** `agw provider add relay1 --protocol openai-chat --base-url ... --api-key-env RELAY1_KEY` 成功
- **THEN** 配置写回、运行中网关热重载，下一请求即可路由到 relay1，无需重启

#### Scenario: 粘性首选
- **WHEN** `agw switch relay1` 后 relay1 健康
- **THEN** 所有档案内请求先走 relay1，即使其优先级数字较大

### Requirement: 配置三层合并与密钥安全
配置 SHALL 按 `config/default.toml` ← `config/local.toml` ← `projects/<名>/agw.toml` 深合并（后者覆盖前者，路由相关字段生效）；`config/local.toml` 与项目令牌文件以 0600 权限创建；`api_key` 支持 `env:VAR` 间接引用。

#### Scenario: 项目覆盖优先级
- **WHEN** 全局池 A>B，项目 foo 配置只启用 B
- **THEN** foo 档案请求只尝试 B

#### Scenario: .env 提供密钥
- **WHEN** `.env` 含 `RELAY_KEY=sk-xxx`，供应商配置 `api_key_env = "RELAY_KEY"`，且进程环境未设置该变量
- **THEN** 网关启动后该供应商密钥解析成功，无需手动 export

#### Scenario: 真实环境优先
- **WHEN** `.env` 与进程环境中同名变量同时存在
- **THEN** 使用进程环境的值，`.env` 不覆盖

#### Scenario: 语法错误快速失败
- **WHEN** `.env` 存在无法解析的行（如缺 `=`）
- **THEN** 启动报错并指明行号，不静默跳过

#### Scenario: 无 .env 行为不变
- **WHEN** 根目录没有 `.env` 文件
- **THEN** 启动照常，密钥仍走 local.toml 明文或真实环境变量

#### Scenario: 密钥不落盘明文
- **WHEN** 供应商以 `--api-key-env` 添加
- **THEN** 配置文件只含 `env:` 引用，密钥值仅在进程内从环境解析

### Requirement: 热重载
运行中的网关 SHALL 在收到 SIGHUP 或管理端点 reload 指令后原子重载配置；重载失败保留旧配置继续服务并记录错误。

#### Scenario: 错误配置不致崩溃
- **WHEN** 重载时 TOML 语法错误
- **THEN** 网关继续用旧配置服务，日志记录重载失败原因，进程不退出

### Requirement: 安全基线
网关 SHALL 默认绑定 127.0.0.1；管理端点 `/__agw/*`（metrics、reload、healthz 除外）要求 admin 令牌；日志与错误输出不出现任何 api key、令牌全量值（脱敏为前 6 位 + `***`）。

#### Scenario: 日志脱敏
- **WHEN** 请求失败记录上游错误
- **THEN** 日志中认证头与密钥字段被脱敏
