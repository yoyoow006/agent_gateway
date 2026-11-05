# Tasks

## 1. Open

1. [x] 核对 agw agent-launcher 实现、测试、主规格、README、usage guide 与知识层。
2. [x] 核对本机 OpenCode 2.2.5 CLI 与官方 provider/config 语义。
3. [x] 获取用户对四件套与两项待确认决策的确认。

## 2. Design

1. [x] 读取确认后的规格，编写独立实现计划 `openspec/plan/add-opencode-2-launcher.md`。
2. [x] 自审计划是否引入权限认证、外部副作用、新依赖或未确认选择；命中硬风险时请求第二次确认。

## 3. Build

1. [x] 在默认隔离 worktree / feature 分支建立干净基线，运行与模块相称的现有测试（agent PASS；CLI 基线受本地 httptest 监听限制，已记录）。
2. [x] 先添加失败测试：OpenCode 配置内容、项目 token 覆盖、IPv6 URL、用户参数透传、用户配置零接触、install npm 约束、CLI kind 接受与未知 kind 错误。
3. [x] 实现 `KindOpenCode`、runtime 配置生成、安装函数、`PrepareExec` 分派与 CLI usage/分派。
4. [x] 运行目标 agent/CLI 测试并确认红转绿。

## 4. Docs and knowledge

1. [x] 更新 README 快速开始、命令表、agent 配置注入与回滚说明。
2. [x] 更新 usage guide 第 5 节与故障排查，明确 OpenCode 2.x-only、模型选择/覆盖、`OPENCODE_CONFIG_CONTENT` 冲突键覆盖和默认 service 行为。
3. [x] 更新 `.ai/kb/projects/agent_gateway.md` 与 `.ai/memory/agent-gateway.md`。

## 5. Verify

1. [x] `gofmt -l .` 为空并格式化变更 Go 文件。
2. [x] `go test ./... -race`（如沙箱限制则运行可用等价测试并记录边界）。
3. [x] `go vet ./...`。
4. [x] `openspec validate add-opencode-2-launcher --strict --no-interactive`。
5. [x] 检查完整 diff 与用户默认配置零接触断言。
6. [x] 执行严格任务级审查，重点单元为 OpenCode 子进程环境认证与配置覆盖边界。
7. [x] 执行规格符合性与代码质量两个独立 Verify 审查，解决全部 Critical/Important findings。
8. [x] 在允许网络/监听的条件下执行一次本机 OpenCode 2.x 启动或模型列表 smoke test；不可行时记录 NOT_RUN 与原因。

## 6. Archive

1. [ ] 持久化 review findings、manifest、未验证范围与残余风险。
2. [x] 将 delta 合并到 `openspec/specs/agent-launcher/spec.md`。
3. [x] 运行按 diff 分类自动升级的 Archive 工作流门禁（归档移动后执行，archive-light 输出 PASS=197 FAIL=0 SKIP=0）。
4. [x] 更新归档索引并把变更目录与独立计划移入 archive。
5. [ ] 保留 feature 分支/worktree，等待用户明确整合指示。

## Final Archive Evidence

- 最终 manifest ID: `309d85437aa2728ec066f9b90d7e4af5c6dd3594ac9c174aadf1a9ee860cebbf`（`VALID`）
- comparison base: `ddb66e745cb116447da2952fd19508ab0eab14b2`
- finding 状态: `resolved=1`；`not-an-issue=5`；`Critical=0`；`Important=0 open`；`Minor open=2`（OpenCode 专属继承环境覆盖顺序的直接测试缺口；真实网关模型请求烟测）
- 未验证范围: 本沙箱下完整 CLI 包受既有本地 listener 限制；真实网关-backed OpenCode 模型请求未执行；仅实测 OpenCode 2.2.5。
- 残余风险: OpenCode 未来 2.x 版本可能改变 `OPENCODE_CONFIG_CONTENT` 或 `{env:...}` 语义；用户同名 `agw` provider/模型配置会被运行时覆盖（已在文档明示）。
