# Review Findings

## Manifest

- Final ID: `d681817c45156890375dd1e076399c6ce1888e0cf73ba3c0cc707c1d665069c1`
- Comparison base: `main` @ `2db73fc2f75ca5e1841045347d354701a569905a`
- Scope: full diff from `main` for provider add key/value validation and local config final permission verification.

## Verdict

PASS.

## Findings

无 Critical/Important/Minor finding。

## Evidence

- `provider add` now validates every `--model` / `--header` before constructing or persisting the provider.
- Invalid formats fail with the offending flag and item:
  - missing `=`
  - empty key
  - empty value
  - duplicate key
- Invalid input tests assert `config/local.toml` is not created.
- Valid input tests assert model map and headers persist.
- Cobra command changed from `Run` to `RunE` so validation errors return instead of exiting the process.
- Repeated StringArray values are held in package variables and cleared after each command, preventing same-process test leakage.
- `atomicWriteFile` now:
  - chmods temp file to 0600
  - atomically renames it
  - stats final file
  - verifies final permission equals 0600
- Existing atomic save and provider-add tests pass.

## Verification

- Red:
  - `TestProviderAddRejectsInvalidKV` initially failed for all invalid cases because they were silently accepted and written.
  - First test evolution exposed process-level StringArray leakage, fixed by package-backed arrays cleared in defer.
- Green:
  - `go test ./internal/cli -run 'TestProviderAdd(RejectsInvalidKV|AcceptsValidKV|DefaultModel)' -count=1` — PASS
  - `go test ./internal/cli -run 'TestProviderAdd' -count=1` — PASS
  - `go test ./internal/config -run 'TestSaveLocal|TestAtomic' -count=1` — PASS
- `go build ./...` — PASS
- `go vet ./...` — PASS
- `gofmt -l .` — no output
- `bash scripts/validate-workflow.sh --fast` — `PASS=199 FAIL=0 SKIP=0`
- `openspec validate provider-kv-and-config-permissions --strict --no-interactive` — valid
- `git diff --check` — PASS
- Review manifest — `VALID 9e424e61f8eb89d94de97e57079a27f9ff57c6408be377ae66a5237060cae8af`
- Recorded final verify — `VALID d681817c45156890375dd1e076399c6ce1888e0cf73ba3c0cc707c1d665069c1`

## Unverified

- A filesystem state that changes mode between chmod and final stat was not deterministically reproduced; success path verifies 0600 and failure branch is covered by code inspection.
- Full CLI suite remains blocked by unrelated `httptest.NewServer` listener restrictions.

## Residual risk

- If final permission verification fails after rename, the destination has already been replaced with complete new content, but `SaveLocal` returns an error and does not claim success.
- Duplicate keys are rejected across repeated `--model` and repeated `--header` independently; the same string used once as model and once as header remains allowed by design.
