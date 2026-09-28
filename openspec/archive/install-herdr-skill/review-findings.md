# Review And Verification Evidence

## Build Checks

- Source preflight: 13,867 bytes; SHA-256 `03855a7a1f9d0aa1ba6444fed2e4971adf796e91e73001d8f2472f5f9e5f659f`; destination absent.
- Installed `.codex/skills/herdr/SKILL.md`: 13,867 bytes; same SHA-256; frontmatter `name: herdr`; directory contains only `SKILL.md`.
- `openspec validate install-herdr-skill --strict --no-interactive`: exit 0, valid.
- `git diff --check`: exit 0.
- Claude destination absent; index empty; no tracked implementation file modified.
- Review manifests:
  - `full-1.json`: `a16de8624426ea98d99511c72d3f04f731b6004a029d3fb011d46347e9a3d600` (staled after task/status updates).
  - `full-2.json`: `b28c66a2a08834626274c7f6a80046c886849921e60122e287056ead81a7bf93`, VALID immediately around content review.

## Strict Gate Result

`bash scripts/validate-workflow.sh --require-openspec` exited 1 with `PASS=199 FAIL=1 SKIP=0` at the outer summary. Its contract unittest run reported 3 failures.

### HRD-001

- severity: Important
- repo/path: `README.md`
- evidence: `WorkflowSemanticSyncTest.test_entrypoints_and_user_documents_use_current_semantics` requires `README.md` to contain `三件套`; HEAD `5782707` does not contain it. A clean `git archive HEAD` reproduction fails identically.
- observable impact: Required workflow gate cannot pass for any change on this baseline.
- status: open
- minimal fix: Separate workflow documentation change to restore the required README semantic anchor.
- verification: Re-run focused semantic test and required workflow gate after fix.

### HRD-002

- severity: Important
- repo/path: `scripts/tests/test_validate_workflow.py:412-424`
- evidence: `_assistants_for_portability_test()` treats absence of `scripts/install-ai-workflow.sh` as installed-fixture mode and then requires `.ai/assistant-profile.json`. This source checkout has neither file. Both installer metadata tests fail identically in a clean `git archive HEAD` baseline.
- observable impact: Two required contract tests fail independently of the Herdr skill.
- status: open
- minimal fix: Separate workflow test/profile fixture correction; do not couple it to this skill installation.
- verification: Re-run the two focused `WorkflowProfileTests` cases and required workflow gate after fix.

## Unverified

Repository required strict gate cannot be claimed passing until HRD-001 and HRD-002 are fixed in a separate authorized change. Archive has not run.

## Residual Risk

The installed skill itself remains an untracked, environment-local Codex skill. It will not be committed or shared through Git unless the user later asks for that different integration.

## Post-Repair Recheck

- Baseline repair was independently completed and merged as `repair-required-workflow-baseline` before `main` commit `ba6ab91`.
- Current `bash scripts/validate-workflow.sh --require-openspec`: PASS with `PASS=200 FAIL=0 SKIP=0` (rechecked after the user authorized Git integration).
- HRD-001: resolved by the separate baseline repair; current README contains the required semantic anchor.
- HRD-002: resolved by the separate baseline test/profile fixture repair.
- Final disposition: the user explicitly authorized committing and pushing this project-local Codex skill and its OpenSpec record.
- unverified（未验证范围）: no Herdr CLI invocation or live terminal/machine control was performed by this repository change.
- residual risk（残余风险）: future use of the skill can inspect/control Herdr-managed terminals and saved machines when `HERDR_ENV=1`; each such use remains subject to explicit user authorization.

## Final Archive Record

- 最终 manifest ID: `b28c66a2a08834626274c7f6a80046c886849921e60122e287056ead81a7bf93`（该历史 manifest 对后续授权的 Git 整合状态呈 STALE，其 VALID 内容审查结论对应安装时冻结范围。）
- comparison base: pre-installation `main` checkout; installation was untracked locally until user-authorized integration.
- finding 状态: Critical 0 / Important 0 / Minor 0；HRD-001 与 HRD-002 均由独立已合并基线修复解决，当前 open 0。
- 未验证范围: 未调用 Herdr CLI，未控制任何终端、agent 或 SSH machine。
- 残余风险: 技能后续实际使用具备终端/进程/远端控制能力；每次使用仍需满足 `HERDR_ENV=1` 和用户显式授权。
