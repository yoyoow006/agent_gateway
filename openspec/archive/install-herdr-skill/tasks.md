# Tasks

## 1. Build

1. [x] Before writing the destination, assert the path is absent:
   - `test ! -e .codex/skills/herdr`
2. [x] Install the reviewed `/tmp/herdr-SKILL.md` as `.codex/skills/herdr/SKILL.md`.
3. [x] Do not create `.claude/skills/herdr/`, add tests, modify routing, stage files, or commit.

## 2. Verify

1. [x] Verify byte count and SHA-256:
   - expected 13867 bytes and `03855a7a1f9d0aa1ba6444fed2e4971adf796e91e73001d8f2472f5f9e5f659f`.
2. [x] Parse frontmatter and confirm `name: herdr`.
3. [x] Run `openspec validate install-herdr-skill --strict --no-interactive`.
4. [x] Run `git diff --check`.
5. [x] Inspect status/diff and confirm nothing is staged and no tracked implementation file changed.
6. [x] Run `bash scripts/validate-workflow.sh --require-openspec`; it fails on three pre-existing HEAD failures recorded in `review-findings.md`, reproduced independently with `git archive HEAD`.

## 3. Archive

1. Resolve review findings, if any, without expanding scope.
2. Update task evidence and status.
3. [x] Record that the initial request intentionally kept the skill untracked; later user authorization changed the disposition to commit and push this exact change.

## Local Integration Strategy

Implement directly in the current checkout without a branch, worktree, staging, or commit because the user explicitly requested a project-local environment skill excluded from Git management. Do not invoke the Herdr CLI during installation or verification.

## 4. Git Integration

1. [x] With explicit user authorization, stage only the project-local Codex skill, this OpenSpec change, and its strict plan.
2. [x] Commit the exact audited skill and governance record to `main` and push after required validation passes.
