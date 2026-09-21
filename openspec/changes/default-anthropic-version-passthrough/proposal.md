模式: 标准
状态: 构建中

# 同协议 Anthropic 透传补默认版本头

## Why

跨协议转换到 Anthropic 上游时，网关会生成 `Anthropic-Version: 2023-06-01`；但客户端与上游同为 Anthropic 协议的字节透传路径只复制客户端请求头。通用 Anthropic 客户端如果省略该版本头，上游可能返回 400。现有透传测试手动携带版本头，未覆盖缺失场景。

## What Changes

- 为同协议 Anthropic 透传补齐默认 `Anthropic-Version`：
  - 客户端已携带时保留客户端值；
  - 客户端缺失且目标供应商协议为 Anthropic 时，注入 `2023-06-01`。
- 保持跨协议现有行为不变。
- 保持非 Anthropic 协议不变。
- 增加同协议透传缺失版本头的回归测试。

## Impact

- 通用 Anthropic 客户端不传版本头时也能正常访问 Anthropic 协议上游。
- Claude Code 已显式传版本头的行为不受影响。
- 不改变请求体字节透传、认证替换、模型映射或其他协议头处理。
