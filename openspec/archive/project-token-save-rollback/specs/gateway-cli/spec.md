# gateway-cli Delta

## MODIFIED Requirements

### Requirement: 项目创建前置配置校验
`agw project new` SHALL 在创建项目目录、写入项目配置或初始化 Git 前成功加载网关配置；加载失败时命令 SHALL 失败且不留下项目目录。

#### Scenario: 配置解析失败
- **WHEN** 网关根的配置文件解析失败且用户执行 `agw project new demo`
- **THEN** 命令返回配置解析错误
- **AND** `projects/demo` 不存在

#### Scenario: 配置有效时保持创建行为
- **WHEN** 网关根配置有效且用户执行 `agw project new demo`
- **THEN** 继续创建项目目录、`agw.toml`、项目 token，并保持既有 Git 初始化行为

## ADDED Requirements

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
