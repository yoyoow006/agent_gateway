## ADDED Requirements

### Requirement: Required Gate Baseline Must Be Green

The repository baseline SHALL keep the required workflow gate runnable without pre-existing failures unrelated to an active change.

#### Scenario: User documentation preserves workflow semantics

- **WHEN** `WorkflowSemanticSyncTest.test_entrypoints_and_user_documents_use_current_semantics` runs on this repository
- **THEN** `README.md` SHALL contain the current standard “三件套” semantic anchor and the strict “硬风险” second-confirmation anchor
- **AND** it SHALL NOT reintroduce retired fixed-four-piece or unconditional-full-archive wording

#### Scenario: Installer-only tests require an installer-capable source

- **WHEN** installer selected-only metadata tests execute
- **THEN** they SHALL run for every assistant only when the repository provides the portable installer source
- **AND** a repository that ships neither the installer implementation nor installer entrypoint SHALL NOT be treated as an installed target solely because the entrypoint is absent

#### Scenario: Canonical installed fixture coverage remains explicit

- **WHEN** a test fixture removes the installer entrypoint and represents a single-assistant installed target
- **THEN** it SHALL create the canonical assistant profile explicitly before invoking installed-metadata validation
- **AND** the regression SHALL keep asserting that the selected canonical assistant is used

#### Scenario: Required gate recovers

- **WHEN** the focused repaired tests and the full required workflow gate run
- **THEN** both SHALL exit successfully with zero failures
- **AND** the gate SHALL NOT rely on skipping these formerly failing cases in a repository without an installer
