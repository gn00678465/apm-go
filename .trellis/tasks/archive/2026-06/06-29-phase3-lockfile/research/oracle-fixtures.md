# Research: Phase 3 Oracle Fixtures (Lockfile + Integrity)

- **Query**: Study all oracle fixtures related to Phase 3 lockfile and integrity
- **Scope**: internal (conformance-kit/oracle/)
- **Date**: 2026-06-29

---

## 1. Lockfile Fixtures (`oracle/lockfile/`)

### 1.1 `v1-git-only.yml`

**Purpose**: Valid lockfile version 1 with a single git-sourced dependency.

**Requirement**: req-lk-001 (Phase 3, section 5.1)

**Expected outcome**: `accept`

**Verify intent**: "top-level mapping with lockfile_version + dependencies"

**YAML structure**:
```yaml
lockfile_version: "1"
generated_at: "2026-05-10T20:14:00+00:00"
apm_version: "0.6.4"
dependencies:
  - repo_url: github.com/octocat/example
    resolved_commit: "7f3c9a4d2e1b8c7f0a9e6d5c4b3a2918f7e6d5c4"
    resolved_ref: v1.2.0
    tree_sha256: "sha256:a1b2c3d4..."
    depth: 1
    deployed_files:
      - .github/instructions/example.instructions.md
```

**Key fields**: lockfile_version=1, git-only (no `source`/`resolved_url`/`resolved_hash`), has `repo_url`, `resolved_commit`, `resolved_ref`, `tree_sha256`, `depth`, `deployed_files`.

---

### 1.2 `v2-with-registry.yml`

**Purpose**: Valid lockfile version 2 with a registry-sourced dependency (not git).

**Requirements**: req-lk-002, req-lk-003 (Phase 3, sections 5.4, 5.2)

**Expected outcome**: `accept`

**Verify intent**:
- req-lk-002: "version 2 when registry entry; monotonic no demote"
- req-lk-003: "git: repo_url+resolved_commit; registry: resolved_url+resolved_hash"

**YAML structure**:
```yaml
lockfile_version: "2"
generated_at: "2026-05-10T20:14:00+00:00"
apm_version: "0.7.0"
dependencies:
  - repo_url: github.com/contoso/common-prompts
    source: registry
    resolved_url: https://registry.example.com/contoso/common-prompts/-/1.4.2.tar.gz
    resolved_hash: "sha256:9f86d081..."
    version: "1.4.2"
    depth: 1
    deployed_files:
      - .github/prompts/review.prompt.md
    deployed_file_hashes:
      .github/prompts/review.prompt.md: "sha256:9f86d081..."
```

**Key fields**: lockfile_version=2, `source: registry`, `resolved_url`, `resolved_hash` (sha256 envelope), `version`, `deployed_file_hashes` map.

---

### 1.3 `round-trip-unknown-fields.yml`

**Purpose**: Test that unknown/future fields and x-* extensions are preserved on round-trip (read then write produces byte-identical output).

**Requirements**: req-lk-011, req-lk-014, req-cf-001 (Phase 3, section 5.2)

**Expected outcome**: `roundtrip_byte_equal`

**Verify intent**:
- req-lk-011: "omit unset (no null); preserve unknown fields"
- req-lk-014: "preserve x-* at every lockfile level on round-trip"

**YAML structure**:
```yaml
lockfile_version: "2"
x-acme-top: keep                        # top-level x-* extension
dependencies:
  - repo_url: github.com/acme/foo
    resolved_commit: "a1b2c3d4..."
    resolved_ref: v1.0.0
    tree_sha256: "sha256:0102030405..."
    depth: 1
    future_unknown_field: preserve-me    # unknown standard field
    x-acme-pin: hard                     # dependency-level x-* extension
```

**Key fields**: `x-acme-top` (top-level extension), `future_unknown_field` (unknown field that must be preserved), `x-acme-pin` (dep-level extension). All must survive a read-write cycle byte-for-byte.

---

### 1.4 `invalid-unknown-version.yml`

**Purpose**: Lockfile with an unrecognized version ("99") -- must be rejected.

**Requirement**: req-lk-004 (Phase 3, section 5.4)

**Expected outcome**: `reject`

**must_contain**: `["99"]` (diagnostic must mention the unknown version)

**Verify intent**: "refuse unknown lockfile_version with upgrade/regenerate diagnostic"

**YAML structure**:
```yaml
lockfile_version: "99"
dependencies: []
```

**Key fields**: `lockfile_version: "99"` -- parser must reject with a diagnostic containing "99".

---

## 2. Integrity Fixtures (`oracle/integrity/`)

> **Important**: The `integrity/` directory spans **two phases**. Frozen YAML lockfiles are Phase 3 (lockfile integrity). Raw tarballs test Phase 5 (security extraction). Some fixtures are dual-tagged.

### 2.1 `good.frozen.yaml` (Phase 3)

**Purpose**: Happy-path frozen install. `resolved_hash` matches the actual SHA-256 of `good.tar.gz`. Verifies that frozen install can verify hash and extract.

**Requirements**: req-lk-006, req-lk-013 (Phase 3, section 5.5/5.2)

**Expected outcome**: `accept`

**Archive**: `good.tar.gz`

**Verify intent**:
- req-lk-006: "frozen-install: never rewrite; fail on missing direct pin"
- req-lk-013: "verify resolved_hash before extract; fail-closed no partial extract"

**YAML structure**:
```yaml
# Happy path: resolved_hash == sha256(good.tar.gz). Frozen install verifies & extracts.
lockfile_version: "2"
dependencies:
  - repo_url: registry.example.com/demo/good
    source: registry
    resolved_url: https://registry.example.com/demo/good/-/1.0.0.tar.gz
    resolved_hash: "sha256:0939d28bf5ec68566166e9e1384cf49cea3b6a1d984ca6149c4f8a9ecfc24b5a"
    version: "1.0.0"
    depth: 1
```

**Verified hash**: `good.tar.gz` SHA256 = `0939d28bf5ec68566166e9e1384cf49cea3b6a1d984ca6149c4f8a9ecfc24b5a` -- matches `resolved_hash` exactly.

**Archive contents**: `good.tar.gz` contains a single file: `skill/SKILL.md` (48 bytes).

---

### 2.2 `bare-hex-reader.frozen.yaml` (Phase 3)

**Purpose**: Tests reader tolerance for bare 64-character hex hashes (without the `sha256:` prefix). Must be treated as equivalent to `sha256:<hex>`.

**Requirement**: req-lk-016 (Phase 3, section 5.2)

**Expected outcome**: `accept`

**Archive**: `good.tar.gz` (same archive as above)

**Verify intent**: "hash envelope <algo>:<hex>; read bare-hex as sha256; write envelope"

**YAML structure**:
```yaml
# req-lk-016 reader tolerance: bare 64-char hex == sha256:<hex>. Frozen install must accept.
lockfile_version: "2"
dependencies:
  - repo_url: registry.example.com/demo/good
    source: registry
    resolved_url: https://registry.example.com/demo/good/-/1.0.0.tar.gz
    resolved_hash: "0939d28bf5ec68566166e9e1384cf49cea3b6a1d984ca6149c4f8a9ecfc24b5a"
    version: "1.0.0"
    depth: 1
```

**Key difference from good.frozen.yaml**: `resolved_hash` is bare hex `0939d28b...` (no `sha256:` prefix). The reader must accept this and treat it as SHA-256. When writing back, the envelope form `sha256:<hex>` should be used.

---

### 2.3 `hash-mismatch.frozen.yaml` (Phase 3)

**Purpose**: Archive hash does NOT match `resolved_hash`. Must fail closed with no extraction.

**Requirement**: req-lk-013 (Phase 3, section 5.2)

**Expected outcome**: `fail_closed`

**Archive**: `good.tar.gz` (hash is `0939d28b...` but recorded hash is all-zeros)

**must_contain**: `["expected", "actual"]` (diagnostic must mention both the expected and actual hash)

**must_not_extract**: `true`

**Verify intent**: "verify resolved_hash before extract; fail-closed no partial extract"

**YAML structure**:
```yaml
# req-lk-013: archive bytes hash != recorded resolved_hash -> fail closed, no extract.
lockfile_version: "2"
dependencies:
  - repo_url: registry.example.com/demo/good
    source: registry
    resolved_url: https://registry.example.com/demo/good/-/1.0.0.tar.gz
    resolved_hash: "sha256:0000000000000000000000000000000000000000000000000000000000000000"
    version: "1.0.0"
    depth: 1
```

**Key fields**: `resolved_hash` is all zeros -- deliberately wrong. The actual archive hash is `0939d28b...`. Implementation must compare hashes BEFORE extraction and fail closed (no partial extraction).

---

### 2.4 `deployed-file-mismatch.frozen.yaml` (Phase 3 + Phase 5)

**Purpose**: On-disk deployed file content does not match the recorded hash. Frozen install and audit must fail closed.

**Requirements**: req-lk-017 (Phase 3, section 5.2), req-sc-001 (Phase 5, section 10.4)

**Expected outcome**: `fail_closed`

**Workspace**: `deployed-file-mismatch.workspace/`

**must_contain**: `[".github/instructions/demo.instructions.md"]`

**Verify intent**:
- req-lk-017: "re-verify deployed hashes on frozen+audit; fail-closed"
- req-sc-001: "deployed file SHA-256 + re-verify on audit; report content-integrity violation"

**YAML structure**:
```yaml
# req-lk-017 / req-sc-001: on-disk deployed file != recorded hash -> audit/frozen fail closed.
lockfile_version: "1"
dependencies:
  - repo_url: github.com/demo/pkg
    resolved_commit: "1111111111111111111111111111111111111111"
    resolved_ref: v1.0.0
    tree_sha256: "sha256:1111111111111111111111111111111111111111111111111111111111111111"
    depth: 1
    deployed_files:
      - .github/instructions/demo.instructions.md
    deployed_file_hashes:
      .github/instructions/demo.instructions.md: "sha256:21c5ebd4761bc9fafc284444c20b168af1e542eba59ed9b56aa6a846acabe901"
```

**Workspace contents**: `deployed-file-mismatch.workspace/.github/instructions/demo.instructions.md` contains the text `TAMPERED` (9 bytes). The SHA-256 of this file (`314cbe44...` per CHECKSUMS) does NOT match the recorded `deployed_file_hashes` value (`21c5ebd4...`). This simulates a file that was modified after deployment.

---

### 2.5 `zip-slip.tar.gz` (Phase 5)

**Purpose**: Archive contains a path-traversal entry (`../evil.txt`). Must be rejected.

**Requirement**: req-sc-002 (Phase 5, section 10.9)

**Expected outcome**: `fail_closed`

**must_contain**: `[".."]`

**Verify intent**: "reject .. / absolute / symlink entries; fail-closed; cleanup partial"

**Archive contents**: Single entry `../evil.txt` (path traversal attack).

---

### 2.6 `symlink-escape.tar.gz` (Phase 5)

**Purpose**: Archive contains a symlink pointing to `/etc/passwd`. Must be rejected.

**Requirement**: req-sc-002 (Phase 5, section 10.9)

**Expected outcome**: `fail_closed`

**must_contain**: `["link"]`

**Verify intent**: "reject .. / absolute / symlink entries; fail-closed; cleanup partial"

**Archive contents**: Single entry `link` which is a symlink to `/etc/passwd`.

---

### 2.7 `four-entry.tar.gz` (Phase 5)

**Purpose**: Archive with exactly 4 entries, used with `--max-entries 3` flag to test entry-count cap enforcement.

**Requirement**: req-sc-004 (Phase 5, section 10.5)

**Expected outcome**: `fail_closed`

**run_with**: `"--max-entries 3"`

**Verify intent**: "tar.gz only; reject zip; size cap 100MB; entry cap 10000; fail before extract"

**Archive contents**: 4 files: `a.txt`, `b.txt`, `c.txt`, `d.txt`. With `--max-entries 3`, the 4th entry exceeds the cap.

---

## 3. Related Policy Fixture

### 3.1 `policy/security-integrity.yml`

**Purpose**: Policy file that enables integrity enforcement.

**Requirements**: req-pl-013, req-pl-014 (Phase 6, section 6.8)

**YAML structure**:
```yaml
name: secure
enforcement: block
security:
  integrity:
    require_hashes: true
  audit:
    fail_on_drift: true
```

**Key fields**: `require_hashes: true` means non-local deps must have `content_hash`. `fail_on_drift: true` means non-zero exit on drift detection. This is Phase 6 but referenced from integrity test expectations.

---

## 4. Hash Verification Chain

The integrity fixtures form a verification chain centered on `good.tar.gz`:

| Artifact | SHA-256 | Notes |
|---|---|---|
| `good.tar.gz` | `0939d28bf5ec68566166e9e1384cf49cea3b6a1d984ca6149c4f8a9ecfc24b5a` | Verified from CHECKSUMS and computed |
| `good.frozen.yaml` resolved_hash | `sha256:0939d28b...` | Matches -- accept |
| `bare-hex-reader.frozen.yaml` resolved_hash | `0939d28b...` (no prefix) | Bare hex form -- must be accepted as sha256 |
| `hash-mismatch.frozen.yaml` resolved_hash | `sha256:0000...0000` | All zeros -- mismatch -- fail_closed |
| `good.tar.gz` inner file `skill/SKILL.md` | `c46d5f8cf318ef381def1236f0dee5df5b3aec9bc5b8845a0c6459c94bb200c4` (48 bytes) | Matches CHECKSUMS entry for targets/_input equivalent |

---

## 5. Phase Boundary Summary

| Fixture | Phase 3 reqs | Phase 5 reqs |
|---|---|---|
| `lockfile/v1-git-only.yml` | req-lk-001 | -- |
| `lockfile/v2-with-registry.yml` | req-lk-002, req-lk-003 | -- |
| `lockfile/round-trip-unknown-fields.yml` | req-lk-011, req-lk-014, req-cf-001 | -- |
| `lockfile/invalid-unknown-version.yml` | req-lk-004 | -- |
| `integrity/good.frozen.yaml` | req-lk-006, req-lk-013 | -- |
| `integrity/bare-hex-reader.frozen.yaml` | req-lk-016 | -- |
| `integrity/hash-mismatch.frozen.yaml` | req-lk-013 | -- |
| `integrity/deployed-file-mismatch.frozen.yaml` | req-lk-017 | req-sc-001 |
| `integrity/zip-slip.tar.gz` | -- | req-sc-002 |
| `integrity/symlink-escape.tar.gz` | -- | req-sc-002 |
| `integrity/four-entry.tar.gz` | -- | req-sc-004 |

**Pure Phase 3 fixtures**: All 4 lockfile files + `good.frozen.yaml`, `bare-hex-reader.frozen.yaml`, `hash-mismatch.frozen.yaml`.

**Dual-phase**: `deployed-file-mismatch.frozen.yaml` (Phase 3 req-lk-017 + Phase 5 req-sc-001).

**Pure Phase 5**: `zip-slip.tar.gz`, `symlink-escape.tar.gz`, `four-entry.tar.gz`.

---

## 6. Discrepancies: Referenced but Absent Fixtures

Two fixture references in `acceptance-coverage.yml` do not match what exists in the oracle:

### 6.1 `integrity/security-baseline-2.3.1.frozen.yaml` (ABSENT)

- **Referenced by**: acceptance-coverage.yml line 147 for req-lk-006 (Phase 3, section 5.5)
- **Actual mapping in EXPECTATIONS.yaml**: req-lk-006 is covered by `good.frozen.yaml` (line 36)
- **Status**: File does not exist in the oracle directory. EXPECTATIONS.yaml uses `good.frozen.yaml` instead.

### 6.2 `integrity/oversize.tar.gz` (ABSENT)

- **Referenced by**: acceptance-coverage.yml line 164 for req-sc-004 (Phase 5, section 10.5)
- **Actual mapping in EXPECTATIONS.yaml**: req-sc-004 is covered by `four-entry.tar.gz` with `run_with: "--max-entries 3"` (line 42)
- **Status**: File does not exist in the oracle directory. EXPECTATIONS.yaml uses `four-entry.tar.gz` with a runtime flag instead of a dedicated oversize archive.

---

## 7. Phase 3 Requirements Without Fixtures

The following Phase 3 requirements from acceptance-coverage.yml have `fixture: null` (no oracle fixture):

| Req ID | Section | Verify Intent |
|---|---|---|
| req-lk-012 | 5.2 | deployed_file_hashes = SHA-256 of bytes; dirs no hash |
| req-lk-015 | 5.6.4 | tree_sha256 canonical tree; recompute+fail-closed on frozen/audit |
| req-lk-005 | 5.5 | content-equivalence ignoring generated_at/apm_version; ordered by (repo_url,virtual_path) |
| req-lk-008 | 5.6 | git-semver: record constraint(verbatim)/resolved_tag/resolved_at(advisory) |
| req-lk-009 | 5.6 | replay resolved_tag when constraint equal; else re-resolve |
| req-lk-010 | 5.6 | update direct git-semver purges install path before re-resolve |
| req-lk-007 | 5.5 | skip download when checkout matches; no observable change |
| req-lk-018 | 5.5 | default frozen when CI truthy; user may override |
