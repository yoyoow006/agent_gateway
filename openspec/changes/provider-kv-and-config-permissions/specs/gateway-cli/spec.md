# gateway-cli Delta

## ADDED Requirements

### Requirement: Provider 键值参数校验
`provider add` SHALL 在加载配置后、写入 `config/local.toml` 前校验全部 `--model` 与 `--header` 条目；每个条目 SHALL 为非空 key、非空 value 的 `key=value` 格式，重复 key SHALL 报错；非法输入 SHALL 不产生配置写入。

#### Scenario: 缺少等号
- **WHEN** 用户传入 `--model claude-sonnet-5`
- **THEN** `provider add` 失败并提示应使用 `key=value`
- **AND** `config/local.toml` 不被写入

#### Scenario: 空 value
- **WHEN** 用户传入 `--header X-Title=`
- **THEN** `provider add` 失败并列出该参数
- **AND** `config/local.toml` 不被写入

#### Scenario: 合法键值
- **WHEN** 用户传入 `--model claude-sonnet-5=claude-relay` 与 `--header X-Title=agw`
- **THEN** provider 保存对应 model map 与 header

### Requirement: local 配置最终权限验证
`SaveLocal` 的原子写入 SHALL 在 rename 后验证最终文件权限等于 `0600`；无法达到该权限时 SHALL 返回错误。

#### Scenario: 权限收紧失败
- **WHEN** 临时文件 chmod 或 rename 后权限验证失败
- **THEN** `SaveLocal` 返回错误
- **AND** 不报告保存成功
