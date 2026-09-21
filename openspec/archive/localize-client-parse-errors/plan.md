# plan · localize-client-parse-errors

## 目标与全局约束

- 基线：Go 1.24.11，模块 `agent_gateway`。
- 目标：把跨协议请求的客户端解析/目标构建错误从供应商 failover 失败中剥离。
- 新行为：
  1. 客户端 codec `ParseRequest(body)` 失败 → 本地错误。
  2. 目标 codec `BuildRequest(ir)` 失败 → 本地错误。
  3. 本地错误 → 按客户端协议 400；不调用 `RecordRequest` / `RecordFailure`；不继续供应商链。
- 不改变：传输错误、上游 HTTP 状态、认证错误、熔断、合法请求转换、同协议透传。
- 测试不得依赖 `httptest.NewServer` 本地监听；优先 `httptest.NewRequest` + `httptest.NewRecorder` + 注入 `http.RoundTripper`。
- 每个运行时行为先红后绿。

## 职责单元 1：错误分类

### Create/Modify/Test

- Modify `internal/gateway/forward.go`
  - 新增局部类型：
    ```go
    type localRequestError struct{ err error }
    func (e localRequestError) Error() string { return e.err.Error() }
    func (e localRequestError) Unwrap() error { return e.err }
    ```
  - 在跨协议路径中：
    - `ParseRequest` 失败包装 `localRequestError`
    - `BuildRequest` 失败包装 `localRequestError`
  - 保持错误文案可诊断，例如：
    - `解析客户端请求: ...`
    - `构建 openai-chat 请求: ...`
- 不改变 URL 构造、认证、传输错误分类。

## 职责单元 2：forward 立即响应

### Create/Modify/Test

- Modify `internal/gateway/forward.go`
  - 在 `resp, err := s.attempt(...)` 错误分支最前面：
    ```go
    var localErr localRequestError
    if errors.As(err, &localErr) {
        writeError(w, clientCodec, 400, localErr.Error())
        return
    }
    ```
  - 该分支必须在 `RecordFailure` 之前。
  - 需要引入 `errors` 包。
- 语义约束：
  - 不记录请求
  - 不记录失败
  - 不影响 in-flight
  - 不继续后续 provider
  - 不写入 lastStatus/lastBody

## 职责单元 3：回归测试

### Modify `internal/gateway/server_test.go`

新增 `TestForwardLocalParseErrorReturns400WithoutProviderMetrics`：

- 配置两个 openai-chat provider（名字可区分），挂载到 `s.clients` 的 RoundTripper：
  - 如果被调用则记录并返回测试失败信号。
- 客户端协议设为 Anthropic，请求 body 为非法 JSON。
- 直接构造：
  - `httptest.NewRequest("POST", "/v1/messages", strings.NewReader("{invalid"))`
  - 带全局虚拟令牌
  - `httptest.NewRecorder`
  - `s.Handler().ServeHTTP(rec, req)`
- 断言：
  - `rec.Code == 400`
  - Content-Type 为 JSON
  - 响应 body 为 Anthropic 错误结构，包含 `error`
  - 两个 provider 的 `Snapshot()`：
    - `Requests == 0`
    - `Failures == 0`
    - `InFlight == 0`
  - RoundTripper 未被调用

新增或扩展目标构建失败测试：

- 如果现有协议实现缺少稳定的用户可控不可映射输入，则不为测试引入新业务行为。
- 可选方案：抽一个包内 `buildRequestError` 测试 helper 或直接调用 `attempt` 断言错误类型。
- 最低要求：增加 `TestAttemptBuildRequestErrorIsLocal`，用一个不可映射 IR / 单元级 fake 不合适时，静态审查必须在 review 记录该场景通过类型包装覆盖。

实际测试以 `TestForwardLocalParseErrorReturns400WithoutProviderMetrics` 为主要可执行红绿证据。

## 职责单元 4：文档

### Modify

- `docs/protocol-flow.md`
  - 在转发路径说明中补充：客户端解析/目标构建失败为本地 400，不触发供应商 failover 或熔断。
- `README.md`
  - failover 语义处补充：无效客户端请求在本地返回 400，不计供应商失败。

### Validation

- 扩展 `internal/config/default_toml_test.go` 文档契约：
  - 断言 README 与 protocol-flow 包含“本地 400”和“不计供应商失败”语义。
- 命令：
  ```bash
  GOCACHE=/tmp/agw-go-build-cache go test ./internal/config -run TestDocumentationLocalClientErrors -count=1
  ```

## 回归验证

实现后依次运行：

```bash
GOCACHE=/tmp/agw-go-build-cache go test ./internal/gateway -run 'TestForwardLocalParseErrorReturns400WithoutProviderMetrics|TestAttemptProviderHeadersCannotOverrideAuth|TestAdminEndpointsRejectWrongMethodsBeforeAuth|TestAttemptAnthropicVersionHeaders' -count=1
GOCACHE=/tmp/agw-go-build-cache go test ./internal/config -run 'TestDocumentationLocalClientErrors|TestDocumentationProviderHeadersCannotOverrideAuth' -count=1
GOCACHE=/tmp/agw-go-build-cache go test ./internal/gateway ./internal/cli -count=1 || true
GOCACHE=/tmp/agw-go-build-cache go build ./...
GOCACHE=/tmp/agw-go-build-cache go vet ./...
test -z "$(gofmt -l .)"
bash scripts/validate-workflow.sh --fast
openspec validate localize-client-parse-errors --strict --no-interactive
git diff --check
```

若 `go test ./internal/gateway ./internal/cli -count=1` 因沙箱禁止本地监听失败，完整记录失败原因；不得将其当作目标测试失败或声称全量通过。

## 提交计划

1. `chore(openspec): localize-client-parse-errors`
   - 四件套/计划、状态构建中。
2. `fix(gateway): return local 400 for client parse errors`
   - 测试、实现、文档。
3. `chore(review): record local parse error evidence`
4. `chore(archive): localize-client-parse-errors`
5. 本地 `--no-ff` 合并回 main。
6. 不 push；推送需单独授权。

## 回滚

- 实现集中在 `forward.go`，整体 revert `fix(gateway)` 提交即可回滚。
- 文档契约测试与实现同提交，回滚后同步消失。
