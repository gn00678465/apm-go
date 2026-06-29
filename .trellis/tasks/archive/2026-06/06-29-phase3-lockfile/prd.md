# Phase 3: Lockfile Write + Integrity

## Goal

Implement lockfile serialization (`apm.lock.yaml` write), integrity verification (hash envelopes, deployed file hashes, tree_sha256), frozen install mode, and the `apm-go install` CLI command that wires together Phase 2's resolver with actual git operations and lockfile output.

This phase bridges the gap between "internal resolver engine" and "observable CLI output" — enabling A/B testing against the Python apm.

## Scope

18 conformance requirements from acceptance checklist Phase 3 (req-lk-*).

**In scope**: lockfile serialization, version monotonicity, hash envelope format, deployed file hash computation, tree_sha256 computation, frozen install, CI env detection, no-op detection, `apm-go install` CLI command, real git TagLister (ls-remote), basic PackageLoader (git clone).

**Out of scope**: target deploy (Phase 4), security hardening beyond hash verification (Phase 5), policy gate (Phase 6).

## Requirements

### R1: Lockfile Structure (req-lk-001)
Top-level mapping with `lockfile_version` (string) and `dependencies` (list). Additional keys: `generated_at`, `apm_version`, `local_deployed_files`, `local_deployed_file_hashes`, `x-*`.

### R2: Version Monotonicity (req-lk-002)
- Any `source: registry` entry → version MUST be `"2"`
- No registry entry → `"1"` or `"2"` both valid
- Once `"2"` written, MUST NOT demote to `"1"`
- Read both versions

### R3: Required Entry Fields (req-lk-003)
- Git: `repo_url` + `resolved_commit` + `tree_sha256`
- Registry: `repo_url` + `resolved_url` + `resolved_hash`

### R4: Unknown Field Preservation (req-lk-011, req-lk-014)
- Omit unset fields (no `null` placeholders)
- Preserve unrecognised fields on round-trip (including `x-*` at both top-level and per-entry)

### R5: Hash Envelope Format (req-lk-016)
- All digests as `<algo>:<hex>` envelope (`sha256`, `sha384`, `sha512`)
- Read: tolerate 64-char bare hex as sha256
- Write: MUST emit envelope form

### R6: Deployed File Hashes (req-lk-012)
- `deployed_file_hashes` = SHA-256 of bytes written to disk
- Directories (trailing `/`) have no hash

### R7: tree_sha256 (req-lk-015)
- Canonical git tree hash per spec §5.6.4 algorithm
- Required for every git-sourced entry
- Frozen install / audit: recompute from working tree, fail-closed on mismatch

### R8: Semantic Equivalence + No-op (req-lk-005)
- Lockfiles differing only in `generated_at`/`apm_version` are equivalent
- No-op install MUST NOT rewrite lockfile
- `--no-provenance` suppresses both fields
- Dependencies sorted ascending by `(repo_url, virtual_path)`

### R9: git-semver Entry Fields (req-lk-008)
- Record `constraint` (verbatim), `resolved_tag` (literal), `resolved_at` (ISO 8601 UTC)
- Already partially handled by Phase 2 resolver

### R10: Lock Replay (req-lk-009)
- Character-equal constraint → replay locked tag
- Already implemented in Phase 2 resolver

### R11: Update Purge (req-lk-010)
- Explicit update → purge install path before re-resolve
- Already handled in Phase 2 update logic

### R12: Unknown Version Rejection (req-lk-004)
- Reject unrecognised `lockfile_version` with diagnostic offering upgrade or regeneration
- Already implemented in Phase 2 lockfile parser

### R13: Frozen Install (req-lk-006)
- Lockfile never written/rewritten
- Any direct dep without pin → fail
- Opt-in via `--frozen`

### R14: Download Skip (req-lk-007, SHOULD)
- Skip download when local checkout matches locked commit
- Must not change observable result

### R15: CI Default (req-lk-018, SHOULD)
- `CI` env var truthy → default to frozen
- Truthy = present AND NOT `""`, `"0"`, `"false"` (case-insensitive)

### R16: Registry Hash Verification (req-lk-013)
- Before decompression: verify archive SHA-256 == `resolved_hash`
- Mismatch → fail-closed, no partial extraction

### R17: Deployed File Integrity (req-lk-017)
- Frozen install + audit: re-verify `deployed_file_hashes` against disk bytes
- Mismatch → fail-closed with path/expected/observed

## Constraints

1. **yaml.Node architecture**: lockfile serialization via SafeLoad → Node manipulation → SafeDump for round-trip fidelity
2. **No download in Phase 3 unit tests**: real git operations behind interfaces, tested with fixtures
3. **Python apm divergences**: Our Go impl follows the SPEC, not Python. Key differences:
   - tree_sha256: we implement it (Python doesn't)
   - Version monotonicity: we enforce it (Python doesn't)
   - Top-level x-*: we preserve them (Python drops them)
4. **Anti-cheat**: test against oracle fixtures (4 lockfile + 7 integrity)

## Acceptance Criteria

- [ ] AC1: Lockfile serialization produces valid YAML matching oracle fixtures (v1-git-only, v2-with-registry)
- [ ] AC2: Round-trip preserves unknown fields and x-* keys at all levels
- [ ] AC3: Version monotonicity enforced (no "2" → "1" demotion)
- [ ] AC4: Hash envelope format on all digests; bare-hex read tolerance
- [ ] AC5: Dependencies sorted by (repo_url, virtual_path) ascending
- [ ] AC6: No-op detection: only generated_at/apm_version differ → no rewrite
- [ ] AC7: Frozen install: --frozen flag, missing pin → fail, lockfile untouched
- [ ] AC8: CI env detection: truthy CI → default frozen
- [ ] AC9: tree_sha256 computation matches spec algorithm
- [ ] AC10: `apm-go install` CLI command functional with real git repos
- [ ] AC11: `go test ./...` passes, coverage ≥ 80% for new code
- [ ] AC12: `go vet ./...` clean
- [ ] AC13: A/B testable against Python apm on at least 5 test cases

## Dependencies

- Phase 2 complete (resolver engine, lockfile read) ✅
- Oracle fixtures: `conformance-kit/oracle/lockfile/` + `oracle/integrity/`

## Research References

- `.trellis/tasks/06-29-phase3-lockfile/research/spec-lockfile.md` — spec extraction (18 reqs)
- `.trellis/tasks/06-29-phase3-lockfile/research/python-lockfile.md` — Python apm analysis
- `.trellis/tasks/06-29-phase3-lockfile/research/oracle-fixtures.md` — oracle fixture inventory
