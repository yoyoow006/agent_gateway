# gateway-cli 规格（delta）

## MODIFIED Requirements

### Requirement: 配置三层合并与密钥安全
配置 SHALL 按 `config/default.toml ← config/local.toml ← projects/<名>/agw.toml` 深合并（后者覆盖前者，路由相关字段生效）；`config/local.toml` 与项目令牌文件以 0600 权限创建；`api_key` 支持 `env:VAR` 间接引用。网关启动时 SHALL 自动加载 `<根>/.env`（存在时）作为环境变量来源：真实环境变量优先于 `.env` 同名值；解析失败带行号快速失败；文件权限宽于 0600 时记警告。

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
