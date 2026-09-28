# Review Findings

## Task-level: managed authentication home and guarded reset

- Manifest: `a6260715a68dc57d1e0ef21dfc1c7c31dd281e8a4fc1d0512e3ead83c0e653a6`
- Findings:
  - `I1` Important: rollback used direct write and could leave partial managed file.
  - `I2` Important: backup writer did not sync before rename.
  - `I3` Important: success path cleared write flags to suppress rollback, creating unsafe maintenance semantics.
  - `M1` Minor: intermediate root-contained symlink remains accepted; final managed-home symlink is rejected.
  - `M2` Minor: concurrent parent removal after validation fails closed during reset.
- Disposition: I1/I2/I3 fixed in `2eef836`.
- Delta manifest: `3f72f77d0830faa9240c49792ff861e4500f20322dee292c2e4838ae92b17312`
- Delta disposition: all Important findings resolved; no open Critical/Important.

## Final verification evidence

- `GOCACHE=/tmp/agw-gocache go test ./internal/agent ./internal/cli -run 'Test.*(Desktop|RunDashDash|PrepareExec)' -count=1` — PASS.
- `GOCACHE=/tmp/agw-gocache go test ./internal/agent -race -count=1` — PASS.
- `GOCACHE=/tmp/agw-gocache go vet ./...` — PASS.
- `gofmt -l .` — empty.
- `git diff --check` — PASS.
- `openspec validate adapt-codex-desktop --strict --no-interactive` — PASS.
- `bash scripts/validate-workflow.sh --archive-light` — `PASS=199 FAIL=0 SKIP=0`.

## Prior verification evidence

- `GOCACHE=/tmp/agw-gocache go test ./internal/agent -run 'Test.*(DesktopHome|DesktopConfig|DesktopAuth|DesktopAtomic|DesktopReset|Rollback)' -count=1` — PASS.
- `GOCACHE=/tmp/agw-gocache go test ./internal/agent -count=1` — PASS.
- `GOCACHE=/tmp/agw-gocache go test ./internal/agent ./internal/cli -run 'Test.*(Desktop|RunDashDash|PrepareExec)' -count=1` — PASS.
- `GOCACHE=/tmp/agw-gocache go vet ./...` — PASS.
- `gofmt -w internal/agent internal/cli`, `git diff --check`, and `openspec validate adapt-codex-desktop --strict --no-interactive` — PASS.
- `GOCACHE=/tmp/agw-gocache go test ./... -race -count=1` — blocked by sandbox local-listen restriction in pre-existing CLI/gateway tests (`httptest` cannot bind `[::1]:0`). `internal/agent` race suite passed; all non-listener packages passed. Same restriction exists at baseline `9a6658f`.
- Real desktop launch: NOT_RUN. The host has `/usr/lib/chatgpt/ChatGPT`, but launching the real GUI was not authorized. Automated fake-executable tests cover preparation and launch arguments.

## Verify: specification conformance

- Manifest: `0cbf378a4cb0a3d759d8ad9aca316d57480bb65e0217e8da2a29818db50b26af`
- Result: PASS, no Critical/Important findings.
- Minor S1: README rollback wording remains accurate because desktop mode uses an isolated home; no change required.

## Verify: code quality

- Manifest: `0cbf378a4cb0a3d759d8ad9aca316d57480bb65e0217e8da2a29818db50b26af`
- Findings:
  - `Q2` Important: hidden `AGW_DESKTOP_GOOS` production override. Fixed in `7db2035`.
  - `Q3` Important: spec did not clearly express explicit arbitrary executable override. Delta clarified in `7db2035`.
  - `Q4` Minor: custom substring helpers. Fixed.
  - `Q5` Minor: redundant TOML wrapper. Fixed.
- Delta manifest: `3d3933c74db9fc136acd75d6abb5c4a922f12a9768cc4ff7c8cb0f4c158fb567`
- Delta result: PASS; all Important findings resolved, no open Critical/Important findings.
