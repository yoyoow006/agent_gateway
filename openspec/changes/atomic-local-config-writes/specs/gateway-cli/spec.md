# gateway-cli Delta

## ADDED Requirements

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
