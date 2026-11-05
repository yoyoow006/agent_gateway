# Implementation Plan: add-opencode-2-launcher

## Goal

Add OpenCode 2.x as an agw-managed coding agent without changing gateway routing semantics or touching the user's OpenCode configuration. `agw run opencode` must launch the locally installed OpenCode 2.x CLI with an OpenAI-compatible provider whose authentication resolves from the selected agw virtual-token environment variable. `agw install opencode` must install only the OpenCode 2.x major line.

## Architecture and invariants

- Runtime behavior lives in `internal/agent`:
  - `KindOpenCode = "opencode"`.
  - `OpenCodeConfigContent(listen string) (string, error)` renders a compact, deterministic JSON object for `OPENCODE_CONFIG_CONTENT`.
  - `InstallOpenCode(opts Options) error` reuses `npmInstall` with package `@opencode/cli@^2`.
  - `PrepareExec` adds an `opencode` branch that sets only child overrides `AGW_API_KEY` and `OPENCODE_CONFIG_CONTENT`, then builds `argv = ["opencode", extraArgs...]`.
- CLI acceptance lives in `internal/cli/run.go`; no new flags.
- The OpenCode provider object is exactly:
  - `npm: "@ai-sdk/openai-compatible"`
  - `name: "agw"`
  - `options.baseURL: <config.BaseURL(listen)> + "/v1"`
  - `options.apiKey: "{env:AGW_API_KEY}"`
  - `models.agw.name: "agw"`
- Token handling:
  - JSON never contains a token value.
  - The selected token exists only in the child environment map and is appended after inherited environment entries by existing `appendOverrideEnv`.
  - Errors and CLI launch output do not print the token or full inline JSON.
- Configuration boundary:
  - Do not read, stat, create, modify, back up, or remove `~/.config/opencode`, project `opencode.json`, OpenCode auth storage, logs, service state, or cache.
  - OpenCode continues to merge user non-conflicting settings; agw controls conflicting keys through the official runtime override source.
- Existing behavior:
  - Claude, Codex CLI, and Codex Desktop preparation remain byte-for-byte semantically unchanged.
  - `--reset` remains valid only for `codex-desktop`.
  - Gateway protocol translation, provider selection, failover, breaker, project profiles, and `/v1/models` semantics are not modified.

## Global constraints (copy exactly into implementation review)

1. 只支持 OpenCode 2.x；安装约束必须是 `@opencode/cli@^2`。
2. `OPENCODE_CONFIG_CONTENT` 必须是合法 JSON，且不得包含任何 agw token 值。
3. 认证必须通过 `apiKey: "{env:AGW_API_KEY}"` 与子进程环境 `AGW_API_KEY` 组合解析。
4. `baseURL` 必须来自 `config.BaseURL(listen)` 再追加 `/v1`，不得手写 host/port 拼接。
5. 除 `AGW_API_KEY` 与 `OPENCODE_CONFIG_CONTENT` 外，`PrepareExec("opencode")` 不新增子进程覆盖项。
6. 不读取、写入、备份或删除任何用户 OpenCode 配置、项目配置、凭据、日志、缓存或 service 数据。
7. `agw run opencode` 只自动添加 argv[0] `opencode`；其余 argv 必须完全来自 `--` 后的用户参数。
8. 测试不得真实执行 npm、联网、启动 OpenCode service，或输出完整虚拟令牌。

## Task 1: Isolated implementation baseline

Create/modify/test paths:
- Create branch `feature/add-opencode-2-launcher`.
- Commit only this change's proposal, delta spec, design, tasks, implementation plan, and status transition.
- Create isolated worktree under ignored `.worktrees/add-opencode-2-launcher` according to `.codex/skills/git-worktrees/SKILL.md`.

Steps:
1. Record `git branch --show-current`, `git status --short --branch`, and `git worktree list --porcelain`.
2. Ensure only this change's OpenSpec files are uncommitted.
3. Create the feature branch, stage exactly:
   - `openspec/changes/add-opencode-2-launcher/`
   - `openspec/plan/add-opencode-2-launcher.md`
4. Set proposal status to `构建中`, include that status in the same baseline commit, and return the original baseline branch to its prior state.
5. Mount the committed feature branch in `.worktrees/add-opencode-2-launcher`.
6. In the worktree run:
   - `git status --short --branch` (expected clean on `feature/add-opencode-2-launcher`)
   - `git log -1 --oneline` (expected OpenSpec baseline commit)
   - `GOCACHE=/tmp/agw-gocache go test ./internal/agent ./internal/cli -count=1` (expected existing tests PASS)

Rollback: keep main and worktree untouched; delete only the newly created branch/worktree if baseline creation fails before implementation.

## Task 2: Red tests for OpenCode runtime configuration and launcher

Create/modify/test paths:
- Create `internal/agent/opencode_test.go`.
- Do not modify production code in this task.

Test contract:
1. `TestOpenCodeConfigContent`:
   - Call `OpenCodeConfigContent("127.0.0.1:8787")`.
   - `json.Unmarshal` must succeed.
   - Assert provider `agw` has npm/name/options/model fields listed in Architecture.
   - Assert `options.apiKey == "{env:AGW_API_KEY}"`.
   - Assert serialized JSON does not contain `agw-` or a concrete token fixture.
2. `TestOpenCodeConfigContentIPv6`:
   - Call with `[::1]:8787`.
   - Assert `baseURL == "http://[::1]:8787/v1"`.
3. `TestPrepareExecOpenCodeProjectProfile`:
   - Build a temporary gateway root with global token, `projects/foo` directory, and project token `agw-foo`.
   - Pre-create unrelated user config fixtures under a temporary home:
     - `.config/opencode/opencode.json`
     - `projects/foo/opencode.json`
   - Snapshot content and mtime before preparation.
   - Call `PrepareExec(root, "opencode", "foo", []string{"--model", "agw/custom", "--standalone"})`.
   - Assert returned dir is `<root>/projects/foo`.
   - Assert env has exactly `AGW_API_KEY=agw-foo` and `OPENCODE_CONFIG_CONTENT`.
   - Assert inline JSON uses `{env:AGW_API_KEY}` and no token.
   - Assert argv is exactly `["opencode", "--model", "agw/custom", "--standalone"]`.
   - Assert both user config snapshots remain identical, same mtime, and no new files appear under the temporary OpenCode config directory.
4. `TestInstallOpenCodeMajorConstraintAndZeroTouch`:
   - Inject `okRunner`; retain its command records.
   - Pre-create a user `opencode.json` snapshot.
   - Call `InstallOpenCode(Options{Home: tempHome, Root: tempRoot, Listen: "127.0.0.1:8787", Runner: runner})`.
   - Assert commands are exactly:
     1. `npm --version`
     2. `npm install -g @opencode/cli@^2`
   - Assert user config snapshot and directory entries remain unchanged.
5. Extend the existing install zero-touch test to invoke `InstallOpenCode` and preserve its assertions for Claude/Codex.

Commands:
```bash
GOCACHE=/tmp/agw-gocache go test ./internal/agent -run 'Test(OpenCode|PrepareExecOpenCode|InstallOpenCode|InstallZeroTouch)' -count=1
```
Expected result before implementation: compile failure for missing `KindOpenCode` / `OpenCodeConfigContent` / `InstallOpenCode`, followed by assertion failures if compilation is staged separately. Record the exact red output; do not count it as a gate failure.

## Task 3: Green agent implementation

Create/modify/test paths:
- Modify `internal/agent/install.go`.
- Modify `internal/agent/run.go`.
- No files outside `internal/agent`.

Interface:
```go
const KindOpenCode = "opencode"

func OpenCodeConfigContent(listen string) (string, error)
func InstallOpenCode(opts Options) error
```

Implementation details:
1. Add `KindOpenCode` to the kind constants.
2. Add `OpenCodeConfigContent`:
   - Resolve `config.BaseURL(listen)`.
   - Build the exact provider object from Architecture with typed anonymous structs or a small unexported struct.
   - Marshal with JSON; do not write a file.
   - Return marshal/baseURL errors wrapped with `生成 opencode 配置失败: %w`.
3. Add `InstallOpenCode`:
   - Call `opts.fill()`.
   - Call `npmInstall(opts.Runner, "@opencode/cli@^2")`.
   - Print usage and rollback guidance: running `agw run opencode` injects runtime config; uninstall is handled by OpenCode/npm and agw creates no OpenCode config.
   - Never call `os.WriteFile`, `os.MkdirAll`, or OpenCode-path helpers.
4. Extend `PrepareExec`:
   - Generate inline config first.
   - On error return nil values and the wrapped error.
   - Set `env["AGW_API_KEY"]=token`.
   - Set `env["OPENCODE_CONFIG_CONTENT"]=content`.
   - Set `argv=append([]string{"opencode"}, extraArgs...)`.
5. Keep existing kind branches and error behavior unchanged.

Commands:
```bash
gofmt -w internal/agent/install.go internal/agent/run.go internal/agent/opencode_test.go
GOCACHE=/tmp/agw-gocache go test ./internal/agent -run 'Test(OpenCode|PrepareExecOpenCode|InstallOpenCode|InstallZeroTouch)' -count=1
GOCACHE=/tmp/agw-gocache go test ./internal/agent -count=1
```
Expected result: all targeted and package tests PASS.

Rollback: revert only the three agent files; OpenSpec baseline remains committed.

## Task 4: Red CLI acceptance tests

Create/modify/test paths:
- Modify `internal/cli/cli_test.go`.
- Do not modify `internal/cli/run.go` in this task.

Test contract:
1. `TestRunOpenCodeUsesRuntimeConfig`:
   - Build a valid root and `projects/demo`.
   - Replace package `execAgent` with a capture function.
   - Invoke root command args:
     - `run --root <root> opencode --project demo -- --model agw/custom`
   - Assert captured env has both overrides, project token, and valid inline JSON.
   - Assert captured argv begins `opencode` and ends with exactly the user-provided flags.
   - Restore `execAgent` with defer.
2. `TestAgentCommandUsageIncludesOpenCode`:
   - Find `install` and `run`.
   - Assert install usage contains `opencode`.
   - Assert run usage contains `opencode`.
   - Assert reset flag help still says only `codex-desktop`.
3. Preserve existing tests asserting unknown agent failure and `--reset` rejection.

Commands:
```bash
GOCACHE=/tmp/agw-gocache go test ./internal/cli -run 'TestRunOpenCode|TestAgentCommandUsageIncludesOpenCode|TestRunRejectsResetOutsideCodexDesktop' -count=1
```
Expected result before CLI implementation: failure because command validation rejects `opencode` or usage omits it. Record exact red output.

## Task 5: Green CLI integration

Create/modify/test paths:
- Modify only `internal/cli/run.go`.

Implementation details:
1. Update command usage strings:
   - `install <claude|codex|opencode>`
   - `run <claude|codex|codex-desktop|opencode> [--project 名] [-- agent 参数...]`
2. Add `KindOpenCode` to both kind acceptance checks while retaining current `codex-desktop` acceptance behavior.
3. In `runInstall`, dispatch `InstallOpenCode` for `KindOpenCode`; retain existing Claude/Codex branches.
4. Do not add flags, environment variables, reset behavior, service management, or model selection.
5. Unknown-kind messages list `opencode`.
6. Startup log remains `agw: 启动 opencode（项目=... 目录=...）` and never prints env values.

Commands:
```bash
gofmt -w internal/cli/run.go internal/cli/cli_test.go
GOCACHE=/tmp/agw-gocache go test ./internal/cli -run 'TestRunOpenCode|TestAgentCommandUsageIncludesOpenCode|TestRunRejectsResetOutsideCodexDesktop' -count=1
GOCACHE=/tmp/agw-gocache go test ./internal/cli -count=1
```
Expected result: targeted and full CLI package tests PASS.

Rollback: revert only `internal/cli/run.go` and CLI test changes.

## Task 6: Documentation and stable knowledge

Create/modify/test paths:
- Modify `README.md`.
- Modify `docs/usage-guide.md`.
- Modify `.ai/kb/projects/agent_gateway.md`.
- Modify `.ai/memory/agent-gateway.md`.

README content contract:
1. Short description and diagram include OpenCode alongside Claude Code and Codex.
2. Prerequisites state OpenCode is optional and only OpenCode 2.x is supported.
3. Quick start adds:
   - `agw install opencode`
   - `agw run opencode --project demo -- --model agw/custom`
4. Agent launch section explains:
   - runtime injection via `OPENCODE_CONFIG_CONTENT`;
   - token only via child `AGW_API_KEY`;
   - no user OpenCode config/credential mutation;
   - fixed selectable placeholder model `agw`;
   - OpenCode user settings/plugins continue to merge except conflicting keys;
   - OpenCode background service remains managed by OpenCode.
5. Command table adds install/run rows.
6. Rollback says agw creates no persistent OpenCode config; remove the npm package with OpenCode/npm if desired.

Usage guide content contract:
1. Section 5 adds OpenCode install/run examples.
2. Explain OpenCode 2.x-only package constraint `@opencode/cli@^2`.
3. Explain default model placeholder and native `-- --model` override.
4. Explain inline runtime config precedence and the conflict-key override effect.
5. State agw does not manage or reset OpenCode service and does not touch credentials.
6. Troubleshooting covers:
   - missing OpenCode → install or ensure 2.x;
   - wrong model → use OpenCode-native model flag;
   - user config conflict → rename user provider/model or rely on agw override;
   - service issue → use OpenCode-native service/standalone behavior.

Knowledge/memory content contract:
- Project card records launcher API, runtime override, OpenAI-compatible route, token boundary, and OpenCode 2.x-only fact.
- Memory records that `OPENCODE_CONFIG_CONTENT` is runtime override and must never contain a token; use `{env:AGW_API_KEY}` plus sorted child env override.

Validation:
```bash
grep -RIn "opencode" README.md docs/usage-guide.md .ai/kb/projects/agent_gateway.md .ai/memory/agent-gateway.md
git diff --check
```
Expected: all four files mention OpenCode; no whitespace errors.

## Task 7: Full verification and strict reviews

Commands:
```bash
gofmt -l .
GOCACHE=/tmp/agw-gocache go test ./... -race -count=1
GOCACHE=/tmp/agw-gocache go vet ./...
openspec validate add-opencode-2-launcher --strict --no-interactive
git diff --check
```
If sandbox networking/listening restrictions block race/full tests, run `GOCACHE=/tmp/agw-gocache go test ./... -count=1` plus the narrow race tests already known to be safe, and record exact command, failure, and uncovered boundary as NOT_RUN; never silently skip.

Review units:
1. Authentication/configuration unit:
   - selected project/global token;
   - inherited `AGW_API_KEY` override ordering;
   - no token in JSON/logs/errors;
   - exact runtime config JSON;
   - user config zero-touch.
2. CLI boundary unit:
   - install/run kind dispatch;
   - exact extra-argument passthrough;
   - no reset/service behavior;
   - existing kinds unchanged.

Use `.ai/rules/review.md` and `.ai/tools/review_manifest.py`:
- Freeze one manifest for the authentication/configuration unit.
- Reviewer must run manifest verification before reading files and again before conclusions.
- Resolve every Critical/Important finding and persist evidence.

Then run two independent Verify reviews with fresh manifests:
1. Specification conformance against delta and this plan.
2. Code quality/regression risk across the complete diff.

Optional host smoke test, only if it can run without exposing token or mutating user config:
```bash
GOCACHE=/tmp/agw-gocache go build -o /tmp/agw-opencode-test ./cmd/agw
AGW_API_KEY=agw-dummy OPENCODE_CONFIG_CONTENT="$(...)" opencode --version
```
Prefer an automated or temporary-home model-list test only if local listen restrictions permit. Otherwise record NOT_RUN with the exact environmental blocker.

## Task 8: Archive and local integration

Steps:
1. Ensure every task/test/review item is green and findings persisted.
2. Merge accepted delta into `openspec/specs/agent-launcher/spec.md` without losing existing requirements.
3. Update archive index according to current archive README conventions.
4. Classify all files changed after effective Verify:
   - if workflow executable, assistant entry, skill semantics, contract test, or governance specs changed, run required full gate:
     ```bash
     bash scripts/validate-workflow.sh --require-openspec
     ```
   - otherwise:
     ```bash
     bash scripts/validate-workflow.sh --archive-light
     ```
5. Move `openspec/changes/add-opencode-2-launcher/` and the independent plan to `openspec/archive/`.
6. Set final proposal status to `已归档`.
7. Commit implementation/docs/spec archive as independently rollbackable units.
8. Do not push, merge to main, delete worktree/branch, or run real npm install without explicit user authorization.

## Commit organization

1. OpenSpec baseline: confirmed four artifacts plus plan and `构建中`.
2. Agent runtime implementation plus red/green tests.
3. CLI integration plus tests.
4. Documentation/knowledge updates.
5. Review evidence and any minimal fixes.
6. Spec merge/archive transition.

Each commit must leave `go test ./internal/agent ./internal/cli -count=1` and relevant docs checks passing where possible; full gates run before archive.

## Self-review conclusion

The plan covers every delta requirement and scenario. It introduces no new product choice beyond the two user-confirmed decisions. It does hit the strict hard-risk category of authentication behavior because it injects the selected agw virtual token into a child process. Therefore implementation requires explicit second confirmation after this plan is reviewed by the user.
