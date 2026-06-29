# Phase 3: Implementation Plan — Lockfile Write + Integrity + Install CLI

## Advisor Fixes Applied

- **Blocker 1**: tree_sha256 uses `git ls-tree -r <commit>`, NOT filesystem walk (mode bits, .git, untracked)
- **Blocker 2**: req-lk-013 verify function in Phase 3; actual registry download deferred to Phase 4
- **Should-fix**: Field order derived from oracle fixtures (tree_sha256 before depth)
- **Should-fix**: A/B testing is semantic field-subset, not byte-level
- **Should-fix**: no-op detection ignores resolved_at (advisory, regenerated)
- **Should-fix**: install.go uses DI (installDeps struct) for testability

## Step 1: Hash Operations (req-lk-016, req-lk-012, req-lk-013)

### 1.1 Create `internal/lockfile/hash.go`
- `HashEnvelope(algo, hex string) string`
- `ParseHashEnvelope(s string) (algo, hex string, err error)` — parse `<algo>:<hex>` or bare 64-char hex
- `HashFileBytes(path string) (string, error)` — SHA-256 envelope
- `ComputeDeployedFileHashes(files []string, rootDir string) (map[string]string, error)`
- `VerifyDeployedHashes(hashes map[string]string, rootDir string) error` — req-lk-017
- `VerifyArchiveHash(archivePath, expectedHash string) error` — req-lk-013

### 1.2 Create `internal/lockfile/hash_test.go`
- Envelope format/parse round-trip
- Bare hex tolerance
- Deployed file hash computation on temp directory
- Verify mismatch detection
- Archive hash verify against oracle `good.tar.gz` + `hash-mismatch.frozen.yaml`

**Verify**: `go test ./internal/lockfile/ -run TestHash -v`

## Step 2: tree_sha256 via git (req-lk-015)

### 2.1 Create `internal/lockfile/treehash.go`
- `ComputeTreeSHA256(repoDir, commit string) (string, error)` — git ls-tree driven
- `VerifyTreeSHA256(expected, repoDir, commit string) error`
- Internal: parse `git ls-tree -r --full-tree <commit>`, read blobs via `git cat-file blob <sha1>`, build recursive tree, compute canonical hash

### 2.2 Create `internal/lockfile/treehash_test.go`
- Create a fixture git repo (git init + add files + commit)
- Compute tree_sha256 via our function
- Independently compute expected hash (manually construct canonical tree representation, sha256 it)
- Cross-check both match (anti-tautology: expected NOT from our function)

**Verify**: `go test ./internal/lockfile/ -run TestTreeSHA -v`

## Step 3: Lockfile Serialization (req-lk-001, req-lk-002, req-lk-003, req-lk-005, req-lk-011, req-lk-014)

### 3.1 Create `internal/lockfile/write.go`
- `SerializeLockfile(lf *Lockfile, original *yaml.Node) (*yaml.Node, error)`
- `WriteLockfile(lf *Lockfile, original *yaml.Node) ([]byte, error)`
- `IsSemanticEqual(a, b *Lockfile) bool` — ignores generated_at, apm_version, resolved_at
- `DetermineVersion(deps []LockedDep, existingVersion string) string`
- `SortDependencies(deps []LockedDep)`
- Field order: from oracle fixtures (tree_sha256 before depth)
- Omit-empty: no null, no zero-value fields

### 3.2 Create `internal/lockfile/write_test.go`
- Oracle round-trip: v1-git-only.yml, v2-with-registry.yml, round-trip-unknown-fields.yml
- Version monotonicity tests (no "2" → "1")
- Sort order tests
- Semantic equivalence: differs only in generated_at → equal
- Semantic equivalence: differs only in resolved_at → equal
- Unknown field + x-* preservation at top and entry level

**Verify**: `go test ./internal/lockfile/ -run TestWrite -v`

## Step 4: Frozen Install (req-lk-006, req-lk-017, req-lk-018)

### 4.1 Create `internal/lockfile/frozen.go`
- `IsCIEnvironment() bool`
- `IsTruthyCI(val string) bool`
- `CheckFrozenInstall(m *manifest.Manifest, lock *Lockfile) error`

### 4.2 Create `internal/lockfile/frozen_test.go`
- CI env truthy/falsy table: `"true"`, `"1"`, `"yes"` → true; `""`, `"0"`, `"false"`, `"FALSE"` → false; absent → false
- Frozen: missing pin → error; all present → success

**Verify**: `go test ./internal/lockfile/ -run TestFrozen -v`

## Step 5: Git Operations (real TagLister + PackageLoader)

### 5.1 Create `internal/gitops/tags.go`
- `RealTagLister` via `git ls-remote --tags`
- Parse output into `[]semver.TagInfo`

### 5.2 Create `internal/gitops/clone.go`
- `RealPackageLoader` via `git clone --depth 1`
- Checkout specific ref, parse sub-manifest

### 5.3 Tests (integration, skippable with `-short`)

**Verify**: `go test ./internal/gitops/ -v -short`

## Step 6: Install CLI Command

### 6.1 Create `cmd/apm/install.go`
- `installCmd` cobra command
- Flags: `--frozen`, `--no-provenance`, `--yes`
- `installDeps` struct for DI (TagLister, PackageLoader)
- Pipeline: parse → load lock → frozen check → resolve → clone → hash → write lock

### 6.2 Create `cmd/apm/install_test.go`
- Test with injected mock TagLister/PackageLoader
- Test frozen mode rejection
- Test --no-provenance omits generated_at/apm_version

### 6.3 Wire into `main.go`

**Verify**: `go build ./cmd/apm/ && bin/apm-go install --help`

## Step 7: Integration + A/B

### 7.1 Full test suite
```bash
go test ./... -cover
go vet ./...
```

### 7.2 A/B (semantic field-subset)
- Create test manifest with known git deps
- Run both apm-go and Python apm
- Compare: repo_url, resolved_commit, resolved_ref, source, depth (ignore tree_sha256, formatting, x-*)

## Step Dependency Graph

```
Step 1 (hash) ──────────────────────────────┐
Step 2 (tree_sha256) ───────────────────────┤
Step 3 (serialization) ── depends on 1,2 ──┤
Step 4 (frozen) ───────────────────────────┤
Step 5 (gitops) ───────────────────────────┤
Step 6 (install CLI) ── depends on 1-5 ────┤
Step 7 (integration) ── depends on all ────┘
```
