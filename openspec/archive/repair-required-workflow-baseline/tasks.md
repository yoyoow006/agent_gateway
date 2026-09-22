# Tasks

## 1. Build

1. [x] Capture focused red test evidence on the current baseline for:
   - semantic documentation anchor failure;
   - two installer selected-only metadata failures.
2. [x] Add the current workflow semantics to the README development/workflow section:
   - standard suite: `proposal.md`, delta `spec.md`, `tasks.md`;
   - conditional `design.md` only for independent architectural decisions;
   - strict mode uses the confirmed suite plus an independent plan;
   - strict second implementation confirmation is triggered by the hard-risk set.
3. [x] Introduce an installer-capability predicate using the existing authoritative source marker (`scripts/lib/install_ai_workflow.py`) and/or the portable entrypoint.
4. [x] Limit `_assistants_for_portability_test()` coverage expansion to installer-capable repositories, while keeping canonical installed-fixture coverage explicit.
5. [x] Add a regression asserting that installer-only tests are not silently skipped merely because the installer entrypoint is missing.

## 2. Verify

1. [x] Run the three focused red tests and require 0 failures.
2. [x] Run `python3 -m unittest -v scripts.tests.test_validate_workflow` and require 0 failures.
3. [x] Run `bash scripts/validate-workflow.sh --require-openspec` and require `FAIL=0`.
4. [x] Run `git diff --check`.
5. [x] Confirm Herdr untracked artifacts remain untouched and this change remains a separate OpenSpec unit.

## 3. Archive

1. [x] Resolve review findings without expanding scope.
2. [x] Merge the requirement into the workflow governance specification.
3. [x] Persist final manifest, findings, unverified scope, residual risk, and archive evidence.
4. [x] Run the archive gate selected by the repository validator.

## Local Integration Strategy

The current checkout is on `main` and contains another deliberately untracked, user-requested Herdr change. To avoid mixing changes, create `feature/repair-required-workflow-baseline` in the current checkout without switching away until OpenSpec files are staged, then follow the strict flow. Do not push, PR, or merge without explicit authorization.
