## ADDED Requirements

### Requirement: Linux Codex Desktop Provider Environment Authentication

`agw run codex-desktop` SHALL provide the selected agw virtual token to the desktop child process via `AGW_API_KEY`, and the managed provider SHALL resolve authentication from that named environment key. The managed `config.toml` SHALL NOT contain the token value.

#### Scenario: 桌面 provider 使用环境变量认证
- **WHEN** a desktop home is prepared for the global or project profile
- **THEN** the agw provider contains `env_key = "AGW_API_KEY"` while `config.toml` contains no token value
- **AND** `auth.json` continues to contain only the selected token under `OPENAI_API_KEY`

#### Scenario: 桌面内置结构化请求认证
- **WHEN** `PrepareExec` resolves `codex-desktop`
- **THEN** the returned child environment contains `CODEX_HOME=<root>/.agw/codex-desktop` and `AGW_API_KEY=<selected virtual token>`
- **AND** desktop internal structured Responses requests, including thread title generation, can authenticate to agw with the same selected profile token

#### Scenario: 外部环境值不覆盖选定档案
- **WHEN** `AGW_API_KEY` already exists in the launching environment with a different value and `agw run codex-desktop --project demo` is prepared
- **THEN** agw sets the child `AGW_API_KEY` to the `demo` project virtual token after the inherited environment value

## MODIFIED Requirements

### Requirement: Linux Codex Desktop 受管配置生成

The isolated desktop home SHALL contain an agw-managed `config.toml` with a Responses provider pointing to the loopback gateway, `disable_response_storage = true`, and `env_key = "AGW_API_KEY"`.

#### Scenario: 生成全局配置
- **WHEN** the global profile is prepared with `listen = 127.0.0.1:8787`
- **THEN** `config.toml` selects the agw provider whose `base_url` is `http://127.0.0.1:8787/v1`, `wire_api = responses`, and `env_key = AGW_API_KEY`
- **AND** `auth.json` contains only `OPENAI_API_KEY` set to the global virtual token
