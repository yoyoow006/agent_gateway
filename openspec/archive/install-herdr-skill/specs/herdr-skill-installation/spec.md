## ADDED Requirements

### Requirement: Fixed-Source Codex Herdr Skill Installation

The repository SHALL install the `herdr` skill from `herdrdev/herdr` ref `v0.9.1`, path `skills/herdr/SKILL.md`, into the project-local Codex skill directory only.

#### Scenario: Project-local Codex availability

- **WHEN** the repository contains the installed skill
- **THEN** `.codex/skills/herdr/SKILL.md` SHALL exist
- **AND** their frontmatter name SHALL be `herdr`
- **AND** `.claude/skills/herdr/SKILL.md` SHALL NOT be created by this change

#### Scenario: Upstream identity is auditable

- **WHEN** the installation provenance is checked
- **THEN** the change record SHALL identify the repository, ref, path, Git blob SHA, byte count, and SHA-256 of the upstream source

### Requirement: Installation Guard

The repository SHALL preserve installation safety without replacing an existing skill.

#### Scenario: Refuse unintended replacement

- **WHEN** the local skill path already exists during installation
- **THEN** installation SHALL fail before writing the local skill file

#### Scenario: Installed content remains the fixed source

- **WHEN** the installed file is checked after installation
- **THEN** its SHA-256 SHALL equal the recorded v0.9.1 SHA-256 `03855a7a1f9d0aa1ba6444fed2e4971adf796e91e73001d8f2472f5f9e5f659f`

#### Scenario: Git management remains untouched

- **WHEN** installation completes
- **THEN** no file SHALL be staged or committed
