# Review Findings

## Manifests

- Build rollback-safety invariant: `4b6657af3af6591b978c8376babc04e7b87b67f01d3b28099ea4da279876ad21`
- Verify specification compliance: `4b6657af3af6591b978c8376babc04e7b87b67f01d3b28099ea4da279876ad21`
- Verify code quality: `4b6657af3af6591b978c8376babc04e7b87b67f01d3b28099ea4da279876ad21`
- Recorded final: `a92fb3d2c41ba3deb07d390baaebe3777d4203ddd45a09aec23704944069c78e`
- Comparison base: `main` @ `a780572ea000841c5851feb1c36fccd6eaf9d4ce`

## Verdict

PASS.

## Findings

无 Critical/Important/Minor finding。

## Evidence

- Existing token guard runs after config load and before token generation, preventing overwrite.
- Token persistence occurs before project directory/template/Git creation.
- Injected save failure leaves no project path and invokes no Git runner.
- Artifact failure restores the prior `local.toml`; when it did not previously exist, the newly created file is removed.
- Cleanup uses `os.Remove` only when the target is an empty directory; it cannot remove an existing non-empty project or the same-name ordinary file used in the test.
- Git initialization failure remains warning-only and does not trigger rollback.
- Existing success, config-error, conflict, and Git-missing tests continue to pass.

## Verification

- Red:
  - `TestNewRefusesExistingProjectToken` initially succeeded in creating a new project and overwrote the existing token path semantics.
  - `TestNewTokenSaveFailureLeavesNoProject` initially created the project despite injected save failure.
  - Initial compile red for injectable `saveProjectConfig` was expected before the implementation hook existed.
- Green:
  - `go test ./internal/workspace -run 'TestNew(RefusesExistingProjectToken|TokenSaveFailureLeavesNoProject|ProjectArtifactFailureRollsBackToken)' -count=1 -v` — PASS
  - `go test ./internal/workspace -count=1` — PASS
  - `go test -race ./internal/workspace -count=1` — PASS
  - `go test ./internal/config ./internal/cli -run 'TestSaveLocal|TestProjectCommandsPersist|TestRunProject' -count=1` — config PASS; CLI no matching tests
- `go build ./...` — PASS
- `go vet ./...` — PASS
- `gofmt -l .` — no output
- `bash scripts/validate-workflow.sh --fast` — `PASS=199 FAIL=0 SKIP=0`
- `openspec validate project-token-save-rollback --strict --no-interactive` — valid
- `git diff --check` — PASS
- Manifest verify before/after each review focus — VALID for the IDs above.
- Recorded final verify — `VALID a92fb3d2c41ba3deb07d390baaebe3777d4203ddd45a09aec23704944069c78e`.

## Unverified

- Real disk-full and power-loss rollback were not simulated; failure behavior is tested through injection and a preexisting read-only file.
- Full networked CLI/Gateway suites remain blocked by sandbox local-listener restrictions.

## Residual risk

- Rollback is best effort and ignores rollback I/O errors while returning the original artifact error. A failed rollback could leave a token entry; this is disclosed rather than recursively attempting destructive recovery.
- If `git init` itself partially creates `.git` and then fails, it remains warning-only by existing contract and the project is considered created.
