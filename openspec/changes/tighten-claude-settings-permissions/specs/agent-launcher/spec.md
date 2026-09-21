# agent-launcher Delta

## ADDED Requirements

### Requirement: Claude settings 权限收紧
`GenerateClaudeSettings` SHALL 在写入包含虚拟令牌的 settings 文件后确保其 Unix 权限为 `0600`；既有文件权限过宽时 SHALL 收紧，无法收紧时 SHALL 返回错误。

#### Scenario: 重写已过宽的 settings 文件
- **WHEN** Claude settings 文件已存在且权限为 `0644`
- **AND** `GenerateClaudeSettings` 重写该文件
- **THEN** 文件最终权限为 `0600`

#### Scenario: 新建 settings 文件
- **WHEN** settings 文件不存在
- **AND** `GenerateClaudeSettings` 创建该文件
- **THEN** 文件权限为 `0600`
