# Research: OpenAPM v0.1 Lockfile Specification (Section 5)

- **Source**: `D:\Projects\apm-dev\apm\docs\src\content\docs\specs\openapm-v0.1.md`
- **Scope**: Section 5 (all subsections) + related normative statements
- **Date**: 2026-06-29

---

## 5.1 Top-level Structure

**Requirement**: [req-lk-001]

The lockfile is a single YAML 1.2 document at the project root, filename `apm.lock.yaml`.

### Required top-level keys

| Key | Type | Required |
|---|---|---|
| `lockfile_version` | string (`"1"` or `"2"`) | MUST |
| `dependencies` | list | MUST |

### Additional top-level keys

| Key | Type | Notes |
|---|---|---|
| `generated_at` | string (ISO 8601) | Advisory; excluded from semantic equivalence |
| `apm_version` | string | Advisory; excluded from semantic equivalence |
| `mcp_servers` | (unspecified in sec 5) | |
| `mcp_configs` | (unspecified in sec 5) | |
| `local_deployed_files` | list of paths | Self-entry: project's own deployed files |
| `local_deployed_file_hashes` | map path -> `<algo>:<hex>` | Self-entry: hashes of project's own deployed files |
| `attestations` | (reserved for v0.2) | Publisher provenance |
| `x-*` | any | Vendor extensions per [req-ext-001] |

### Minimal example

```yaml
lockfile_version: "1"
generated_at: "2026-05-10T20:14:00+00:00"
apm_version: "0.6.4"
dependencies:
  - repo_url: github.com/octocat/example
    resolved_commit: "7f3c9a4d2e1b8c7f0a9e6d5c4b3a2918f7e6d5c4"
    resolved_ref: v1.2.0
    tree_sha256: "sha256:a1b2c3d4e5f60718293a4b5c6d7e8f90112233445566778899aabbccddeeff00"
    depth: 1
    deployed_files:
      - .github/instructions/example.instructions.md
```

---

## 5.2 Per-entry Fields

Each element of `dependencies` describes one resolved package. **[req-lk-011]**: Omit fields whose values are unset (no `null` placeholders) and preserve unrecognised fields on round-trip.

### Git-sourced entry -- REQUIRED fields

Per **[req-lk-003]** and **[req-lk-015]**:

| Field | Type | Notes |
|---|---|---|
| `repo_url` | string | Canonical repo identity |
| `resolved_commit` | string | 40-character lowercase hex SHA-1 |
| `tree_sha256` | string | Hash envelope `sha256:<hex>` over canonicalised git tree |

### Registry-sourced entry -- REQUIRED fields

Per **[req-lk-003]**:

| Field | Type | Notes |
|---|---|---|
| `repo_url` | string | Package identity (not download URL) |
| `resolved_url` | string | Registry archive download URL (advisory for mirrors) |
| `resolved_hash` | string | Hash envelope `sha256:<hex>` of registry archive bytes -- trust anchor |

### Common optional fields (both source types)

| Field | Type | Notes |
|---|---|---|
| `host` | string | FQDN when not inferable from `repo_url` |
| `port` | integer | Non-standard port, validated 1..65535 |
| `registry_prefix` | string | Path prefix for registry proxy |
| `resolved_ref` | string | User-supplied ref (branch, tag, SHA) |
| `version` | string | Resolved version selector (semver for semver sources) |
| `virtual_path` | string | Subpath inside repo for virtual packages |
| `is_virtual` | boolean | |
| `depth` | integer | 0 = self, 1 = direct, >1 = transitive |
| `resolved_by` | string | `repo_url` of parent that pulled this transitive dep |
| `package_type` | string | `apm_package`, `skill_bundle`, etc. |
| `skill_subset` | list | Selected skill names for skill_bundle packages |
| `deployed_files` | list of paths | Project-relative paths the consumer wrote |
| `deployed_file_hashes` | map path -> `<algo>:<hex>` | Per-file integrity; see [req-lk-012] |
| `source` | string | `local` for path deps, `registry` for registry deps; absent for git |
| `local_path` | string | Original path for local deps |
| `content_hash` | string | Hash envelope of local package source tree |
| `is_dev` | boolean | True when declared under devDependencies |
| `attestations` | (reserved v0.2) | Publisher provenance |
| `x-<name>` | any | Vendor extension per [req-ext-001] |

### git-semver additional fields

Per **[req-lk-008]** -- all three REQUIRED on every git-semver entry:

| Field | Type | Notes |
|---|---|---|
| `constraint` | string | Original semver range from the manifest (verbatim) |
| `resolved_tag` | string | Literal tag string the range resolved to |
| `resolved_at` | string | ISO 8601 UTC timestamp of resolution event; **advisory only** -- MUST NOT be used as tie-breaker in replay |

---

## 5.3 Self-entry Semantics and Vendor Extensions

### Self-entry

A project that ships its own primitives records deployed files at the **top level** via:
- `local_deployed_files` -- list of paths
- `local_deployed_file_hashes` -- map of `path -> <algo>:<hex>`

Consumers MAY synthesize an in-memory virtual dependency entry keyed by `"."` for uniform iteration. The synthesized entry shape:

```yaml
repo_url: "<self>"
source: local
local_path: "."
depth: 0
is_dev: true
```

**Critical rule**: The synthesized entry **MUST NOT be written back to YAML**. The flat `local_deployed_*` top-level fields are the on-disk source of truth.

**Rationale**: Isolation prevents the orphan-cleanup logic of one dependency from removing files attributed to another.

### Vendor extension fields

Per **[req-ext-001]** and **[req-ext-002]**:

- Keys matching `x-[a-z][a-z0-9-]*` at **any** nesting level are vendor extensions
- MUST be ignored during semantic interpretation
- MUST NOT cause parse-time errors
- MUST be preserved byte-equivalent on round-trip
- Vendors SHOULD namespace as `x-<vendor>-<name>` (e.g. `x-acme-telemetry`)
- The spec MUST NOT define normative keys starting with `x-`

Per **[req-lk-014]**: Vendor-extension keys MUST be preserved at every mapping level of the lockfile (both top-level and per-entry) on round-trip.

Per **[req-lk-011]**: Unrecognised fields (including vendor extensions) MUST be preserved on round-trip.

---

## 5.4 Lockfile Versions ("1" vs "2") and Monotonicity

**[req-lk-002]**:

| Condition | Required lockfile_version |
|---|---|
| At least one entry has `source: registry` | MUST be `"2"` |
| No entry has `source: registry` | MAY be `"1"` or `"2"` |

**Monotonicity rule**: Once a consumer writes `lockfile_version: "2"` to a given lockfile, subsequent rewrites by **any** conforming consumer MUST NOT demote the version to `"1"`, even if the registry-sourced entry is removed.

**[req-lk-004]**: A consumer MUST refuse to operate on a lockfile whose `lockfile_version` is not recognised, with a diagnostic that explicitly offers the user a choice of either:
1. Upgrading the consumer, **or**
2. Regenerating the lockfile from the manifest

**Field availability is monotonic**: Fields defined in Section 5.2 are valid in both `"1"` and `"2"`. The earlier draft annotation `"v2 only"` is removed.

A consumer SHOULD tolerate reading either `"1"` or `"2"` regardless of which version it prefers on write.

---

## 5.5 Semantic Equivalence, Frozen Install, CI Default, No-op Detection

### Semantic equivalence and no-op detection

**[req-lk-005]**:

Two lockfiles are **semantically equivalent** if they differ only in:
- `generated_at`
- `apm_version`

A **no-op install** MUST NOT rewrite a lockfile whose only changed fields would be these two.

Consumers operating in privacy-sensitive deployments:
- MAY omit `generated_at` and `apm_version` entirely
- Their absence MUST NOT affect content-equivalence comparison
- SHOULD expose a `--no-provenance` (or equivalent) flag to suppress these fields

### Dependencies ordering (part of req-lk-005)

When writing the lockfile, the `dependencies` list MUST be ordered **ascending lexicographically** by the tuple `(repo_url, virtual_path)`. Entries without `virtual_path` sort as if `virtual_path` were the empty string.

Two lockfiles differing only in entry order are semantically equivalent, but a write-back MUST canonicalise to the pinned order so frozen-install diffs are stable across implementations.

### Frozen install mode

**[req-lk-006]**:

- The lockfile is **never written or rewritten**
- Install fails on any **direct dependency** for which the lockfile has no pin
- Opt-in in v0.1 via `--frozen` (or equivalent)
- Future: default will flip to "frozen when a lockfile is present" (deferred to v0.x minor)

**[req-lk-017]** (frozen install integrity):

- MUST re-verify every entry in `deployed_file_hashes` and `local_deployed_file_hashes` against bytes written to disk
- MUST fail closed on mismatch
- Diagnostic MUST name: offending path, expected envelope, observed envelope
- Same re-verification MUST run on `apm audit`

**[req-lk-015]** (frozen install tree integrity):

- MUST re-compute `tree_sha256` from the working tree at `resolved_commit`
- MUST fail closed when recomputed value differs from recorded value
- Diagnostic MUST name: entry, expected envelope, observed envelope

### CI environment variable detection

**[req-lk-018]**:

- SHOULD default to frozen-install when `CI` environment variable is **truthy**
- Truthy defined as: present AND NOT any of:
  - `""` (empty string)
  - `"0"`
  - `"false"` (case-insensitive)
- User MAY override the SHOULD-default via explicit non-frozen invocation
- This is a transition step toward the v0.x default flip in req-lk-006

### Download skip optimisation

**[req-lk-007]**:

- Consumer SHOULD skip the download step when a local checkout already matches the locked commit
- This optimisation MUST NOT change observable behaviour
- Post-install workspace state MUST be identical to a fresh install

---

## 5.6 git-semver Fields (constraint, resolved_tag, resolved_at)

### Recording requirements

**[req-lk-008]**: On every git-semver lockfile entry, the consumer MUST record:

1. `constraint` -- the original semver range from the manifest (**verbatim**)
2. `resolved_tag` -- the literal tag string the range resolved to
3. `resolved_at` -- ISO 8601 UTC timestamp of the resolution event; **advisory only**, MUST NOT be used as tie-breaker in replay

These fields are valid in both `lockfile_version: "1"` and `"2"`.

### Replay semantics

**[req-lk-009]**:

- Replay the previously locked git-semver resolution (reuse locked `resolved_tag`) **when and only when** the manifest's current semver constraint is **character-equal** to the locked `constraint`
- A **different** manifest constraint MUST trigger re-resolution against the remote
- Any difference, including whitespace, triggers re-resolution

### Explicit update behaviour

**[req-lk-010]**:

- When performing an explicit update operation against a direct git-semver dependency, the consumer MUST **purge the dependency's install path** before re-resolving
- This ensures the download callback re-runs even when the resolved tag is unchanged
- Guards against the regression where a cached install path masks a re-resolution event

---

## 5.6.4 tree_sha256 -- Git-source Tree Integrity Hash

### Purpose

`resolved_commit` is a SHA-1 identifier. SHA-1 alone is below the 2026 collision-resistance floor and MUST NOT be relied on as the sole integrity anchor. `tree_sha256` closes this gap.

### Canonical git tree hash algorithm

The SHA-256 is computed over the following byte representation:

```
<line>           ::= <mode-octal> SP <name-utf8> SP <blob-sha256-hex> LF
<canonical-tree> ::= <line>*   (entries sorted lexicographically by name)
```

**Field definitions**:

| Component | Description |
|---|---|
| `<mode-octal>` | 4 or 6 digit POSIX-style file mode: `100644`, `100755`, `120000`, `040000` |
| SP | single space character (0x20) |
| `<name-utf8>` | filesystem name, UTF-8 encoded |
| `<blob-sha256-hex>` | lowercase hexadecimal SHA-256 of the blob bytes |
| LF | line feed character (0x0A) |

**Subdirectory recursion**: A subdirectory entry uses mode `040000` and its blob-sha256 is itself the SHA-256 of the subdirectory's **canonical tree representation** (same format, recursively).

**Sorting**: Entries sorted **byte-wise by name** (lexicographic).

**Encoding**: Lines are LF-terminated and UTF-8 encoded.

### Normative requirement

**[req-lk-015]**:

- Consumer MUST compute and record `tree_sha256` for **every** git-sourced lockfile entry
- On frozen install and on `apm audit`: MUST re-compute from working tree at `resolved_commit` and fail closed on mismatch
- Diagnostic MUST name: entry, expected envelope, observed envelope

### Editorial notes

- `resolved_commit` retained as canonical pointer (SHA-1); `tree_sha256` provides collision-resistant integrity
- Future: `resolved_commit_sha256` once git SHA-256 object-format is widely deployed
- Canonical-tree definition for local-path `content_hash` is reserved for v0.2

---

## Hash/Digest Envelope Format

**[req-lk-016]**:

### Format

```
<algo>:<hex>
```

Example: `sha256:abcd1234...`

### Allowed algorithms

`sha256`, `sha384`, `sha512` (per [req-mf-018])

### Positions where this format is used

- `resolved_hash` (registry archive bytes)
- `deployed_file_hashes` (each value)
- `local_deployed_file_hashes` (each value)
- `content_hash` (local package source tree)
- `tree_sha256` (git tree integrity)
- Any future hash field

### Backward compatibility

| Actor | Rule |
|---|---|
| **Readers** (v0.1) | MUST accept bare 64-character lowercase hex as `sha256:<hex>` |
| **Writers** (v0.1) | MUST emit the explicit `<algo>:<hex>` envelope form |
| v0.2 | Will remove reader tolerance; bare-hex will be rejected |

Writers SHOULD already emit the envelope on every hash field for forward compatibility.

---

## Deployed File Hashes Computation

**[req-lk-012]**:

- Computed as SHA-256 hash envelopes (`sha256:<hex-lowercase>`) of the **deployed file bytes as written to disk**
- Directory entries (paths ending in `/`) MUST NOT have a hash entry
- The `<algo>:<hex>` envelope form per [req-lk-016] applies uniformly
- Applies to both `deployed_file_hashes` (per-dependency) and `local_deployed_file_hashes` (self-entry at top level)

**[req-lk-013]** (registry archive integrity):

- Consumer MUST verify `resolved_hash` against the actual SHA-256 of registry archive bytes **before** extracting to disk
- On mismatch: install MUST fail closed with diagnostic naming: entry, expected hash, actual hash
- MUST NOT extract or partially extract the archive

---

## Serialization Rules Summary

| Rule | Requirement ID | Description |
|---|---|---|
| No null placeholders | [req-lk-011] | Omit fields with unset values |
| Preserve unknown fields | [req-lk-011] | Round-trip unknown fields intact |
| Preserve vendor extensions | [req-lk-014] | At every mapping level (top-level and per-entry) |
| Dependencies ordering | [req-lk-005] | Ascending lexicographic by `(repo_url, virtual_path)` |
| No-op detection | [req-lk-005] | Only `generated_at` and `apm_version` differ = no rewrite |
| Hash envelope format | [req-lk-016] | `<algo>:<hex>` everywhere |
| YAML safe subset | [req-mf-020] | No anchors/aliases, no custom tags, no YAML 1.1 octal coercion |

---

## Normative Statement Index (Section 5)

| ID | Category | Summary |
|---|---|---|
| req-lk-001 | Top-level structure | MUST emit mapping with `lockfile_version` + `dependencies` |
| req-lk-002 | Version | MUST set "2" when registry entries exist; monotonic (no demotion) |
| req-lk-003 | Per-entry | Git: MUST have `repo_url` + `resolved_commit`. Registry: MUST have `resolved_url` + `resolved_hash` |
| req-lk-004 | Version | MUST refuse unrecognised `lockfile_version` |
| req-lk-005 | Equivalence | Semantic equiv ignores `generated_at`/`apm_version`; no-op = no rewrite; ordering rule |
| req-lk-006 | Frozen | Never write lockfile; fail on missing pin; opt-in via `--frozen` |
| req-lk-007 | Optimisation | SHOULD skip download when local matches locked commit |
| req-lk-008 | git-semver | MUST record `constraint`, `resolved_tag`, `resolved_at` |
| req-lk-009 | Replay | Replay locked tag when constraint is char-equal; else re-resolve |
| req-lk-010 | Update | Purge install path before re-resolving on explicit update |
| req-lk-011 | Serialization | No null placeholders; preserve unknown fields |
| req-lk-012 | Hashes | deployed_file_hashes = SHA-256 of file bytes on disk |
| req-lk-013 | Registry integrity | Verify resolved_hash before extracting; fail closed on mismatch |
| req-lk-014 | Vendor extensions | Preserve x-* keys at every level on round-trip |
| req-lk-015 | tree_sha256 | Compute for every git entry; re-verify on frozen/audit |
| req-lk-016 | Hash format | `<algo>:<hex>` envelope everywhere; reader tolerates bare hex in v0.1 |
| req-lk-017 | Frozen integrity | Re-verify deployed_file_hashes on frozen install and audit |
| req-lk-018 | CI | SHOULD default frozen when `CI` env var is truthy |
