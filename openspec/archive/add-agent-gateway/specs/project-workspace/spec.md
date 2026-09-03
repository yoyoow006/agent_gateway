# project-workspace 规格（delta）

## ADDED Requirements

### Requirement: 项目创建
`agw project new <名>` SHALL 校验名称（小写字母、数字、`-`、`_`），创建 `projects/<名>/`，执行 `git init`（git 缺失时警告并跳过），生成带注释示例的 `agw.toml` 覆盖模板，并为项目生成虚拟令牌写入受 0600 保护的本地配置。

#### Scenario: 创建即用
- **WHEN** `agw project new demo`
- **THEN** 目录、git 仓库、agw.toml 模板、项目令牌就绪，`agw run claude --project demo` 可直接使用

#### Scenario: 名称冲突
- **WHEN** `projects/demo` 已存在
- **THEN** 报错退出，不覆盖任何内容

### Requirement: 项目列举
`agw project list` SHALL 列出 `projects/` 下所有项目：名称、当前 git 分支、是否有未提交修改、覆盖配置摘要（启用供应商/模型映射）。

#### Scenario: 概览
- **WHEN** 存在两个项目且其一有脏工作区
- **THEN** 列表准确区分脏/干净状态与各自覆盖配置

### Requirement: 网关仓库与业务项目隔离
网关仓库 SHALL 在 `.gitignore` 排除 `projects/`、`.run/`、`config/local.toml`；业务项目作为独立 git 仓库存在，不嵌套进网关仓库提交。

#### Scenario: 互不污染
- **WHEN** 在业务项目内提交、在网关仓库查看状态
- **THEN** 网关仓库不显示业务项目内容，业务项目历史不含网关文件
