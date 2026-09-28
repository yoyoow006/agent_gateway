# agent-launcher Delta

## ADDED Requirements

### Requirement: Linux Codex Desktop 独立 Home 启动
agw SHALL provide `codex-desktop` as a distinct agent kind that starts a locally installed Codex / ChatGPT desktop application on Linux with a gateway-managed isolated Codex home, without modifying the user's default Codex home.

#### Scenario: 启动全局桌面档案
- **WHEN** the user runs `agw run codex-desktop` on Linux with a gateway root and supported desktop application
- **THEN** the child process receives `CODEX_HOME=<root>/.agw/codex-desktop` and the global agw virtual token
- **AND** the user's default `~/.codex/config.toml` and `auth.json` are not read or modified

#### Scenario: 启动项目桌面档案
- **WHEN** the user runs `agw run codex-desktop` beneath `projects/demo`
- **THEN** agw infers and uses the `demo` project profile and token
- **AND** desktop application requests route through agw to that project profile

#### Scenario: 保持 Codex CLI 独立 Profile
- **WHEN** the user runs the existing `agw run codex`
- **THEN** Codex CLI continues to use the user's `$CODEX_HOME/agw.config.toml` profile
- **AND** does not use `.agw/codex-desktop` as its Codex home

### Requirement: Linux Codex Desktop 受管配置生成
The isolated desktop home SHALL contain an agw-managed `config.toml` with a Responses provider pointing to the loopback gateway and `disable_response_storage = true`, and an `auth.json` containing only the selected agw token under `OPENAI_API_KEY`. agw SHALL validate both planned files before replacement, preserve an unchanged pair without changing mtimes, and create timestamped backups before changing either file.

#### Scenario: 生成全局配置
- **WHEN** the global profile is prepared with `listen = 127.0.0.1:8787`
- **THEN** `config.toml` selects the agw provider whose `base_url` is `http://127.0.0.1:8787/v1` and `wire_api = responses`
- **AND** `auth.json` contains only `OPENAI_API_KEY` set to the global virtual token

#### Scenario: 内容不变时刷新
- **WHEN** both managed files already contain exactly the planned content and permissions
- **THEN** no backup is created and neither file mtime changes

#### Scenario: 拒绝半更新
- **WHEN** either atomic replacement fails
- **THEN** the command reports the failure and attempts to restore the prior managed pair
- **AND** does not leave a newly updated config paired with an old auth token

### Requirement: Linux Codex Desktop Home 安全与重置
agw SHALL confine all desktop configuration and authentication writes to `<root>/.agw/codex-desktop`. The directory SHALL resolve to an absolute gateway-root-contained path without symlink escape. `--reset` SHALL remove and recreate only this managed directory and SHALL NOT remove or alter the user's default Codex home, any ancestor directory, or unrelated paths.

#### Scenario: 独立 home 权限
- **WHEN** managed configuration and authentication files are created
- **THEN** `auth.json` is `0600`, directory permissions are no broader than `0700`, and token-bearing files do not leak into logs

#### Scenario: 拒绝未识别内容
- **WHEN** the managed home contains unexpected files or invalid managed TOML/JSON
- **THEN** normal preparation fails without modifying the user's default Codex home
- **AND** the error directs the user to run the explicit reset command

#### Scenario: 显式重置
- **WHEN** the user runs `agw run codex-desktop --reset`
- **THEN** agw removes only `<root>/.agw/codex-desktop` after resolving and validating the exact path
- **AND** recreates an empty managed directory without starting the desktop application

#### Scenario: 禁止路径逃逸
- **WHEN** the managed home path contains a symlink that escapes the gateway root
- **THEN** preparation or reset fails before writes or recursive removal

### Requirement: Linux Codex Desktop 应用定位
agw SHALL resolve a Linux desktop application from an explicit `AGW_CODEX_APP` path when supplied, otherwise from the supported Linux default roots and recognized application names. It SHALL fail before configuration writes when no supported executable is found, and SHALL NOT discover or start desktop applications on non-Linux platforms.

#### Scenario: 显式覆盖
- **WHEN** `AGW_CODEX_APP` points to a recognized executable or application directory
- **THEN** agw normalizes it to an executable path and does not scan default roots

#### Scenario: Linux 默认候选
- **WHEN** no explicit path is supplied on Linux
- **THEN** agw searches `/usr/lib`, `/opt`, `~/Applications`, and `~/.local/share` for recognized ChatGPT/Codex application directories and their `app` subdirectories
- **AND** selects an executable named `ChatGPT`, `chatgpt`, `Codex`, or `codex`

#### Scenario: 应用不存在
- **WHEN** no supported Linux executable is found
- **THEN** agw fails before writing managed configuration files
- **AND** the error explains the supported roots, executable names, and `AGW_CODEX_APP` override

#### Scenario: 非 Linux 平台
- **WHEN** `codex-desktop` automatic discovery is requested on macOS or Windows
- **THEN** agw fails with an explicit unsupported-platform error
- **AND** does not scan platform desktop application locations

### Requirement: Linux Codex Desktop 启动边界
`agw run codex-desktop` SHALL launch the resolved Linux desktop executable with the project working directory and isolated Codex home, and SHALL NOT inject scripts, attach a debugger, modify application files, or terminate an already running Codex / ChatGPT desktop process.

#### Scenario: 启动应用
- **WHEN** configuration preparation succeeds
- **THEN** agw starts the resolved executable with `CODEX_HOME` set to the managed isolated home
- **AND** passes through only user-provided arguments after `--`

#### Scenario: 已有桌面进程
- **WHEN** a Codex / ChatGPT desktop process is already running
- **THEN** agw does not inspect or terminate that process
- **AND** reports only the launch outcome of the requested command
