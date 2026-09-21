# plan · harden-admin-method-and-upstream-auth

## 目标与全局约束

- 基线：Go 1.24.11、模块 `agent_gateway`。
- 目标：
  1. `/__agw/reload` 仅接受 POST；错误方法在 admin token 校验前返回 405 + `Allow: POST`。
  2. `/__agw/metrics` 仅接受 GET；错误方法在 admin token 校验前返回 405 + `Allow: GET`。
  3. provider `headers` 不能覆盖网关注入的上游 `Authorization` / `X-Api-Key`；非认证自定义头保留。
- 不改变模型转发语义、failover、熔断、admin token 强度、监听地址或 `/healthz`。
- 测试不得依赖 `httptest.NewServer` 本地监听（当前执行环境禁止）；使用 `httptest.NewRequest`、`httptest.NewRecorder` 和注入 `http.RoundTripper`。
- 每个运行时行为先红后绿；红阶段失败原因必须是断言/行为缺失，不是环境错误。

## 职责单元 1：管理端点方法限制

### Create/Modify/Test

- Modify `internal/gateway/server.go`
  - 新增私有 wrapper，例如 `requireMethod(method string, next http.HandlerFunc) http.HandlerFunc`。
  - 语义：
    - `r.Method != method` 时立即 `w.Header().Set("Allow", method)`、`w.WriteHeader(http.StatusMethodNotAllowed)`；
    - 匹配时调用 `next`。
  - 路由包装顺序：`mux.HandleFunc("/__agw/metrics", s.requireMethod(http.MethodGet, s.admin(...)))`
  - reload 同理使用 `http.MethodPost`。
- Modify `internal/gateway/server_test.go`
  - 新增 `TestAdminEndpointsRejectWrongMethodsBeforeAuth`。
  - 构造 `httptest.NewRequest`：
    - GET `/__agw/reload`，无 Authorization；
    - POST `/__agw/metrics`，无 Authorization；
  - 断言均为 405、Allow 正确、body 不包含 metrics/provider JSON 或 reload 成功标识。
- Test command / expected
  - Red: `GOCACHE=/tmp/agw-go-build-cache go test ./internal/gateway -run TestAdminEndpointsRejectWrongMethodsBeforeAuth -count=1`
    - Expected before implementation: wrong method reaches admin and returns 401, test fails.
  - Green: same command exits 0.

## 职责单元 2：上游认证最终值不可覆盖

### Create/Modify/Test

- Modify `internal/gateway/forward.go`
  - 当前顺序：认证注入 → provider headers。
  - 改为：provider headers → 协议认证注入。
  - 保持 Anthropic 使用 `X-Api-Key`，OpenAI 系使用 `Authorization: Bearer`。
- Modify `internal/gateway/server_test.go`
  - 复用/扩展 `roundTripFunc` 与 `TestAttemptAnthropicVersionHeaders` 的直接 `attempt()` 测试模式。
  - 新增 `TestAttemptProviderHeadersCannotOverrideAuth`：
    1. Anthropic provider:
       - APIKey `sk-real-anthropic`
       - Headers `{"X-Api-Key":"spoofed","X-Title":"agw"}`
       - Assert upstream `X-Api-Key == sk-real-anthropic`, `X-Title == agw`
    2. OpenAI-chat provider:
       - APIKey `sk-real-openai`
       - Headers `{"Authorization":"Bearer spoofed","X-Title":"agw"}`
       - Assert upstream `Authorization == Bearer sk-real-openai`, `X-Title == agw`
- Test command / expected
  - Red: `GOCACHE=/tmp/agw-go-build-cache go test ./internal/gateway -run TestAttemptProviderHeadersCannotOverrideAuth -count=1`
    - Expected before implementation: spoofed header remains, test fails.
  - Green: same command exits 0.

## 职责单元 3：文档安全边界

### Modify

- `config/default.toml`
  - `headers` 注释补充：认证头由网关注入，配置 `Authorization` / `X-Api-Key` 不会覆盖上游密钥。
- `docs/usage-guide.md`
  - `--header K=V` 描述补充同样边界。
- `README.md`
  - 配置参考 headers 注释补充同样边界。

### Validation

- 增加/扩展文档契约测试到 `internal/config/default_toml_test.go`：
  - 断言三处文本包含“不能/不会覆盖认证头”的安全提示。
- Command:
  - `GOCACHE=/tmp/agw-go-build-cache go test ./internal/config -run TestDocumentationProviderHeadersCannotOverrideAuth -count=1`
- Expected:
  - Red before doc update;
  - Green after.

## 回归验证

必须在实现后依次运行并读取退出码：

```bash
GOCACHE=/tmp/agw-go-build-cache go test ./internal/gateway -run 'TestAdminEndpointsRejectWrongMethodsBeforeAuth|TestAttemptProviderHeadersCannotOverrideAuth' -count=1
GOCACHE=/tmp/agw-go-build-cache go test ./internal/gateway -run 'TestAttemptAnthropicVersionHeaders|TestAdminEndpointsRejectWrongMethodsBeforeAuth|TestAttemptProviderHeadersCannotOverrideAuth' -count=1
GOCACHE=/tmp/agw-go-build-cache go test ./internal/gateway ./internal/cli -count=1 || true
GOCACHE=/tmp/agw-go-build-cache go build ./...
GOCACHE=/tmp/agw-go-build-cache go vet ./...
test -z "$(gofmt -l .)"
bash scripts/validate-workflow.sh --fast
openspec validate harden-admin-method-and-upstream-auth --strict --no-interactive
git diff --check
```

说明：`go test ./internal/gateway ./internal/cli -count=1` 在当前沙箱可能因既有 `httptest.NewServer` 监听限制失败；如失败，输出完整原因并记录为环境限制，不得当作目标测试失败。

## 提交计划

1. OpenSpec 四件套 + plan + `状态: 构建中`：`chore(openspec): harden-admin-method-and-upstream-auth`
2. TDD 测试 + 最小实现 + 文档：`fix(gateway): enforce admin methods and auth headers`
3. 审查证据与状态：`chore(review): record gateway security evidence`
4. 归档与知识沉淀：`chore(archive): harden-admin-method-and-upstream-auth`
5. 本地合并回 main：`merge: harden-admin-method-and-upstream-auth`
6. 不 push；推送需用户单独授权。

## 回滚

- 职责单元 1/2 分别由独立测试锁定，实现 commit 可整体 revert。
- 文档安全提示与测试同一实现提交，回滚后契约测试同步回退。
