# Phase 5 review cycle (external verification)

## Round 1 — independent opus code review (fresh-context, did not write the code)

Verdicts: sc-001 CORRECT, sc-002 **DEVIATION (HIGH)**, sc-003 CORRECT (primitive only),
sc-004 CORRECT, sc-005 CORRECT (real PSL), sc-006 CORRECT (Phase 1), sc-007 CORRECT (primitive),
sc-008 CORRECT (primitive), lk-013/016/017 CORRECT.

### HIGH defect found (and proven against the built binary)
Archive extraction escaped `apm_modules` via **unsanitized lockfile `repo_url`**:
`install.go` extracted to `filepath.Join("apm_modules", dep.UniqueKey())`, and `UniqueKey()`
returns `repo_url` verbatim with no validation. A tampered lockfile `repo_url: ../../escape`
+ a hash-matching local `escape.tar.gz` wrote payload to a sibling of the project dir, exit 0.
SafeExtract's entry guards protect the staging dir, but the dest itself was attacker-controlled
— defeating the §10.9/§10.4 containment Phase 5 exists to provide.

Also: LOW read-path traversal via `deployed_file` paths; two weak/missing tests.

## Fixes applied (this branch)
1. **`internal/lockfile/parse.go`** — `validatePathComponent()` rejects `..` segments /
   absolute / Windows-volume; applied to `repo_url`, `virtual_path`, `deployed_files`,
   `deployed_file_hashes` keys, `local_deployed_files`, `local_deployed_file_hashes` keys.
   Broad fail-closed defense for ALL consumers (extract, git materialization, audit reads).
2. **`internal/archive/extract.go`** — exported `Contained(root, target)`.
3. **`cmd/apm/install.go`** — defense-in-depth `archive.Contained("apm_modules", destDir)`
   guard before `SafeExtract`.
4. Tests: `TestParseLockfile_RejectsPathTraversal`, `TestFrozen_RepoURLTraversal_FailsClosed`,
   strengthened `TestAudit_DeployedFileMismatch` (captures stderr + asserts path),
   `TestHostClass_RequiresRealPSL` (foo.github.io ≠ bar.github.io, locks in real PSL).

### Empirical confirmation (black-box, rebuilt binary)
Repro now fails closed: `Error: validate apm.lock.yaml: lockfile: dependency repo_url
"../../escape" must not contain ".." path segments`, exit 1, **no payload written outside the
project**. Black-box driver 7/7; full `go test ./...` green; archive 83.9%, credsec 90.9%,
lockfile 80.2%; `go vet` clean.

## Round 2 — independent opus re-verification of the fix: in flight.

## Note: python runner unusable
`conformance/conformance-kit/runner/run_conformance.py` crashes parsing the oracle's own
`EXPECTATIONS.yaml` under PyYAML (spaceless flow maps, line 10) — pre-existing, oracle-side, not
Phase 5. Its `assert_fail_closed` also feeds raw `.tar.gz` as `apm.lock.yaml` (never reaches
SafeExtract). Verification therefore relies on native go tests + external black-box, matching the
Phase 4-T pattern. The 3 raw-tarball cases are covered by `TestFrozen_RegistryExtract_EndToEnd`.
