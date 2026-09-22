# Implementation Plan

## Global Constraints

- Baseline commit: `578270794ab6be6a33b961ae2299abf9fbddab22`.
- Preserve all untracked Herdr artifacts exactly:
  - `.codex/skills/herdr/`
  - `openspec/changes/install-herdr-skill/`
  - `openspec/plan/install-herdr-skill.md`
- Do not generate `.ai/assistant-profile.json`.
- Do not implement, download, or invoke an installer.
- Do not change validator runtime behavior; this change repairs test classification and README documentation.
- Use the existing authoritative source marker `scripts/lib/install_ai_workflow.py` for installer capability, with the portable entrypoint retained as an execution signal.

## Task 1: Focused Red Baseline

Run:

```bash
python3 -m unittest -v \
  scripts.tests.test_validate_workflow.WorkflowSemanticSyncTest.test_entrypoints_and_user_documents_use_current_semantics \
  scripts.tests.test_validate_workflow.WorkflowProfileTests.test_installer_selected_only_metadata_allows_core_validation \
  scripts.tests.test_validate_workflow.WorkflowProfileTests.test_installer_selected_only_metadata_allows_public_validation
```

Expected before implementation: exit nonzero with exactly three failures matching the recorded baseline (README semantic anchors and two missing profile assertions).

## Task 2: README Workflow Semantics

Modify `README.md` in the `## 开发` section before the validation commands.

Add a concise paragraph that states:

- Standard mode uses the three-piece suite: `proposal.md`, delta `spec.md`, and `tasks.md`.
- Add `design.md` only when an independent architectural decision cannot be expressed clearly in proposal/tasks.
- Strict mode starts from the confirmed suite and produces an independent implementation plan.
- The strict second implementation confirmation is triggered only by the hard-risk set: permissions/authentication, funds/accounting, database Schema/migration, data deletion/destructive actions, external side effects, or newly unconfirmed choices/assumptions/dependencies/scope.

Do not use any retired phrase from `OLD_STANDARD_PHRASES` or `OLD_ARCHIVE_PHRASES`.

Content validation:

```bash
grep -n '三件套' README.md && grep -n '硬风险' README.md
```

Expected: both anchors found.

## Task 3: Installer Capability Test Classification

Modify `scripts/tests/test_validate_workflow.py`.

1. Add a helper near fixture/profile helpers:

```python
def _is_installer_capable_source() -> bool:
    return (
        REPOSITORY_ROOT / "scripts" / "lib" / "install_ai_workflow.py"
    ).is_file() or (REPOSITORY_ROOT / "scripts/install-ai-workflow.sh").is_file()
```

2. Change `_assistants_for_portability_test()`:
   - if the repository is installer-capable, return `("codex", "claude")`;
   - otherwise return `(self._canonical_assistant(),)`.
3. Keep `_install_selected_metadata()` unchanged. In a repository without an installer, `test_installed_fixture_without_installer_uses_canonical_profile` remains the explicit regression that writes the profile before invoking installed-metadata validation.
4. Add a regression test under `WorkflowProfileTests` that inspects `_assistants_for_portability_test` and asserts:
   - installer-capable repositories request both assistants;
   - repositories without installer implementation or entrypoint use the canonical assistant only;
   - absence of only the entrypoint does not make a non-installer repository look installer-capable.

Focused validation:

```bash
python3 -m unittest -v \
  scripts.tests.test_validate_workflow.WorkflowProfileTests.test_installer_capability_follows_authoritative_source \
  scripts.tests.test_validate_workflow.WorkflowProfileTests.test_installer_selected_only_metadata_allows_core_validation \
  scripts.tests.test_validate_workflow.WorkflowProfileTests.test_installer_selected_only_metadata_allows_public_validation \
  scripts.tests.test_validate_workflow.WorkflowSemanticSyncTest.test_entrypoints_and_user_documents_use_current_semantics
```

Expected: exit 0, zero failures.

## Task 4: Full Contract And Required Gate

Run:

```bash
python3 -m unittest -v scripts.tests.test_validate_workflow
bash scripts/validate-workflow.sh --require-openspec
git diff --check
```

Expected:

- unittest: zero failures;
- required gate: final `PASS=... FAIL=0 SKIP=0`;
- whitespace check: exit 0.

This task may run for several minutes. Read the final summary and exit code; do not infer success from earlier `[PASS]` lines.

## Task 5: Branch Boundary

Because the current checkout contains another user-requested untracked change, do not move or copy it. Before implementation:

1. Record current branch (`main`).
2. Create and switch to `feature/repair-required-workflow-baseline`.
3. Stage only:
   - `openspec/changes/repair-required-workflow-baseline/`
   - `openspec/plan/repair-required-workflow-baseline.md`
   - later, the exact README/test files modified by this change.
4. Keep `.codex/skills/herdr/`, `install-herdr-skill`, and its plan untracked.

No merge, push, PR, branch deletion, or worktree deletion is authorized by this plan.

## Review Focus

- Test classification must not silently skip installer coverage in an installer-capable source.
- README wording must preserve the current risk-tiered semantics and avoid retired old wording.
- No validator/profile runtime behavior changes are intended.
