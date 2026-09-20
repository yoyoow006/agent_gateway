# gateway-cli Delta

## ADDED Requirements

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

## ADDED Requirements

### Requirement: 安装示例 base_url 契约
项目安装与快速开始文档中的 OpenAI/中转站供应商示例 SHALL 使用不带 `/v1` 路径的 `base_url`，与网关自动拼接 `/v1/*` 的实现一致。

#### Scenario: 文档示例不会产生双重 v1
- **WHEN** 维护者检查 README 与 usage guide 中的供应商示例
- **THEN** OpenAI/中转站 `base_url` 示例不包含 `/v1` 路径后缀
