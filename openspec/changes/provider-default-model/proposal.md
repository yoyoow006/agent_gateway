# provider-default-model

模式: 标准
状态: 待归档

## Why

Claude Code / Codex 更新后可能发送配置映射表中不存在的新模型名；当前 `model_map` 缺省透传该模型，导致按旧模型名工作的中转供应商或代理配置直接请求失败。

## What Changes

- 为每个供应商新增 `default_model` 配置：仅当请求模型不在该次生效映射表中时，才改写为 `default_model`。
- 保持优先级不变：精确 `model_map` 命中优先；未命中且配置了 `default_model` 才回退；未配置则继续透传，兼容既有行为。
- 覆盖同协议字节透传（仅重写 `model` 字段）与跨协议翻译两条路径。
- 扩展 `agw provider add --default-model`，支持新增/更新供应商时写入该配置。
- 更新默认配置注释、README、使用指南与 `/v1/models` 展示逻辑。

## Impact

- 运行时行为变化范围有限：只有显式配置 `default_model` 的供应商会对未知模型改写；未配置供应商行为不变。
- 配置新增可选字段，三层合并保持“非空字段覆盖”语义，不引入迁移。
- 客户端可见性：精确映射的请求模型仍出现在 `/v1/models`；仅有兜底配置且没有精确映射时显示 `via-<provider>`，避免让客户端把未知模型自动枚举为可用模型。
