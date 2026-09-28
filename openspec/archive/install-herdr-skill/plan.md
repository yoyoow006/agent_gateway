# Implementation Plan

## Global Constraints

- Source is fixed to `herdrdev/herdr` ref `v0.9.1`, path `skills/herdr/SKILL.md`.
- Expected byte count: `13867`.
- Expected SHA-256: `03855a7a1f9d0aa1ba6444fed2e4971adf796e91e73001d8f2472f5f9e5f659f`.
- Destination is only `.codex/skills/herdr/SKILL.md`.
- Do not create `.claude/skills/herdr/`, invoke the `herdr` CLI, stage files, or commit.
- Preserve all pre-existing user modifications.

## Task 1: Install The Reviewed Skill

1. Confirm `/tmp/herdr-SKILL.md` still has the expected byte count and SHA-256.
2. Confirm `.codex/skills/herdr` does not exist.
3. Create `.codex/skills/herdr/`.
4. Copy `/tmp/herdr-SKILL.md` to `.codex/skills/herdr/SKILL.md` without transformation.
5. Read the installed file's size, SHA-256, and frontmatter name.

Validation command:

```bash
test "$(wc -c < /tmp/herdr-SKILL.md)" = 13867 && test "$(sha256sum /tmp/herdr-SKILL.md | awk '{print $1}')" = 03855a7a1f9d0aa1ba6444fed2e4971adf796e91e73001d8f2472f5f9e5f659f && test ! -e .codex/skills/herdr
```

Expected result before copy: exit `0`. If nonzero, stop; do not download a replacement or overwrite a destination.

Post-install validation:

```bash
test "$(wc -c < .codex/skills/herdr/SKILL.md)" = 13867 && test "$(sha256sum .codex/skills/herdr/SKILL.md | awk '{print $1}')" = 03855a7a1f9d0aa1ba6444fed2e4971adf796e91e73001d8f2472f5f9e5f659f && sed -n '2p' .codex/skills/herdr/SKILL.md | grep -Fx 'name: herdr'
```

Expected result: exit `0`.

## Task 2: Repository And Governance Checks

1. Validate the active OpenSpec change strictly.
2. Check whitespace errors.
3. Confirm `.claude/skills/herdr` is absent.
4. Confirm no file is staged and no tracked implementation path changed.

Validation:

```bash
openspec validate install-herdr-skill --strict --no-interactive && git diff --check && test ! -e .claude/skills/herdr && test -z "$(git diff --cached --name-only)"
```

Expected result: exit `0`; `git status --short` shows only the intended untracked OpenSpec and skill paths.

## Review Focus

- Exact source-to-destination byte identity.
- No hidden files or scripts installed.
- No Claude copy, routing mutation, Git staging, commit, or Herdr CLI invocation.
