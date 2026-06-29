# Phase 3: Design — Lockfile Write + Integrity + Install CLI

## Package Layout

```
internal/
  lockfile/
    types.go           — (Phase 2, exists) LockedDep, Lockfile
    parse.go           — (Phase 2, exists) ParseLockfile
    write.go           — NEW: SerializeLockfile, BuildNode
    write_test.go
    hash.go            — NEW: hash envelope, deployed file hashes
    hash_test.go
    treehash.go        — NEW: tree_sha256 via git ls-tree (NOT filesystem walk)
    treehash_test.go
    frozen.go          — NEW: frozen install checks, CI env detection
    frozen_test.go
  gitops/
    tags.go            — NEW: real TagLister (git ls-remote)
    clone.go           — NEW: real PackageLoader (git clone + parse)
    tags_test.go
    clone_test.go
cmd/apm/
    install.go         — NEW: install command (DI for TagLister/PackageLoader)
    install_test.go
```

## Key Types and Functions

### Lockfile Serialization (write.go)

```go
func SerializeLockfile(lf *Lockfile, original *yaml.Node) (*yaml.Node, error)
func WriteLockfile(lf *Lockfile, original *yaml.Node) ([]byte, error)

// IsSemanticEqual ignores generated_at, apm_version, AND resolved_at (all advisory).
func IsSemanticEqual(a, b *Lockfile) bool

func DetermineVersion(deps []LockedDep, existingVersion string) string
func SortDependencies(deps []LockedDep)
```

Serialization strategy: build yaml.Node tree directly (not yaml.Marshal) to control field ordering and omit-empty. When an original node exists (round-trip), merge new values into the existing node structure to preserve unknown fields and x-* keys.

### Hash Operations (hash.go)

```go
func HashEnvelope(algo string, hex string) string
func ParseHashEnvelope(s string) (algo, hex string, err error)
func HashFileBytes(path string) (string, error)
func ComputeDeployedFileHashes(files []string, rootDir string) (map[string]string, error)
func VerifyDeployedHashes(hashes map[string]string, rootDir string) error

// VerifyArchiveHash checks registry archive SHA-256 before extraction (req-lk-013).
// The verify function exists here; actual download is Phase 4.
func VerifyArchiveHash(archivePath string, expectedHash string) error
```

### tree_sha256 via git (treehash.go) — BLOCKER #1 FIX

**NOT a filesystem walk.** Uses `git ls-tree -r <commit>` to enumerate tracked files
with their git mode bits (100644/100755/120000/040000). This avoids:
1. Windows mode-bit inaccuracy (Go's `os.FileInfo.Mode()` doesn't report Unix exec bit)
2. `.git/` directory inclusion
3. Untracked/ignored file contamination

```go
// ComputeTreeSHA256 computes canonical git tree hash per spec §5.6.4.
// Drives from `git ls-tree -r <commit>` for correct mode bits and tracked-file enumeration.
func ComputeTreeSHA256(repoDir string, commit string) (string, error)

// VerifyTreeSHA256 re-computes tree_sha256 from working tree at commit and compares.
func VerifyTreeSHA256(expected string, repoDir string, commit string) error
```

Algorithm (git-driven):
```
1. Run `git ls-tree -r --full-tree <commit>` in repoDir
2. Parse each line: "<mode> <type> <sha1>\t<path>"
3. Read blob bytes from git: `git cat-file blob <sha1>`
4. Compute sha256 of blob bytes
5. Build recursive tree structure from paths
6. For each tree level, sort entries by name, format as:
   "<mode> <name> <blob-sha256-hex>\n"
7. SHA-256 of the canonical tree representation
```

Test verification: compute tree_sha256 on a fixture repo, cross-check against
an independently computed value (NOT from our own function — ADR-0002 anti-tautology).

### Frozen Install (frozen.go)

```go
func IsCIEnvironment() bool
func IsTruthyCI(val string) bool
func CheckFrozenInstall(manifest *manifest.Manifest, lock *Lockfile) error
```

### Git Operations (gitops/)

```go
type RealTagLister struct{}
func (r *RealTagLister) ListTags(repoURL string) ([]semver.TagInfo, error)

type RealPackageLoader struct {
    ModulesDir string
    Lock       *lockfile.Lockfile
}
func (r *RealPackageLoader) LoadPackage(ref *manifest.DependencyReference, resolvedRef string) (*manifest.Manifest, error)
```

### Install Command (install.go) — DI for testability

```go
type installDeps struct {
    Tags   resolver.TagLister
    Loader resolver.PackageLoader
}

// installCmd accepts injected deps for testing. Production wires RealTagLister/RealPackageLoader.
// Pipeline:
//   1. Parse apm.yml
//   2. Load existing apm.lock.yaml (if any)
//   3. Check frozen mode (--frozen or CI env)
//   4. Resolve dependencies (Phase 2 resolver)
//   5. Download/clone packages
//   6. Compute hashes (deployed files, tree_sha256)
//   7. Write apm.lock.yaml (unless frozen or no-op)
```

## Scope Decision: req-lk-013 (Registry Hash Verification)

**Decision**: The `VerifyArchiveHash` function is implemented in Phase 3 (verify bytes before extract).
The actual registry download (HTTP fetch of tarball) is deferred to Phase 4.
Phase 3 tests exercise `VerifyArchiveHash` against oracle fixtures (`hash-mismatch.frozen.yaml`, `good.tar.gz`).
The `lockfile_version: "2"` write path is exercised via fixtures, not via a live registry install.

## Serialization Rules

### Field Ordering (per-entry) — derived from oracle fixtures

Order derived from `v1-git-only.yml` and `round-trip-unknown-fields.yml` (NOT from Python output):
1. `repo_url`
2. `host` (if present)
3. `port` (if present)
4. `source` (if present)
5. `resolved_commit`
6. `resolved_ref`
7. `resolved_tag`
8. `resolved_url`
9. `resolved_hash`
10. `constraint`
11. `resolved_at`
12. `resolved_by`
13. `version`
14. `virtual_path`
15. `tree_sha256` ← BEFORE depth (matches oracle)
16. `depth`
17. `content_hash`
18. `deployed_files`
19. `deployed_file_hashes`
20. `x-*` (preserve original order)
21. Other unknown fields (preserve original order)

### Omit-empty Rules (req-lk-011)

- String fields: omit if `""`
- Integer fields: omit if `0` (self-entry depth:0 is not written to YAML)
- List/Map fields: omit if nil/empty
- Boolean fields: omit if false
- Never write `null`

### Round-trip Strategy

When updating an existing lockfile:
1. Parse existing file → original yaml.Node
2. Build new Lockfile from resolution
3. For each entry: if it exists in original, update fields in the original node (preserving unknown keys); if new, build fresh node
4. Remove entries no longer in resolution
5. Update top-level fields (version, generated_at, etc.)
6. Preserve top-level x-* and unknown keys from original

## A/B Testing Strategy

**A/B against Python apm is a semantic field-subset comparison, not byte-level.**

Known divergences (Go follows spec, Python doesn't):
- `tree_sha256`: present in Go, absent in Python
- Version monotonicity: Go enforces, Python doesn't
- Top-level `x-*`: Go preserves, Python drops
- YAML formatting: PyYAML vs Go yaml.v4 produce different whitespace/quoting

A/B comparator checks only the field subset where Python is spec-correct:
`repo_url`, `resolved_commit`, `resolved_ref`, `source`, `depth`, `deployed_files`,
`resolved_url`, `resolved_hash`, `constraint`, `resolved_tag`.

The conformance oracle is the real referee for full compliance.

## No-op Detection (req-lk-005) — resolved_at trap

`IsSemanticEqual` must ignore **three** advisory fields, not two:
- `generated_at` (explicitly in spec)
- `apm_version` (explicitly in spec)
- `resolved_at` (advisory per req-lk-008, regenerated on replay)

Without ignoring `resolved_at`, every `install` rewrites the lockfile.

**Verify**: run install twice with no manifest change → second run must not rewrite.

## Testing Strategy

1. **Serialization**: round-trip oracle fixtures (write → read → byte-compare)
2. **Hash envelope**: table-driven parse/format tests
3. **tree_sha256**: git ls-tree driven; expected hash computed independently (NOT by our function)
4. **Frozen install**: mock lockfile with missing pins
5. **CI detection**: set/unset CI env var in tests
6. **No-op**: compare two lockfiles differing only in generated_at/apm_version/resolved_at
7. **Version monotonicity**: table-driven version transition tests
8. **Install CLI**: integration test with injected mock TagLister/PackageLoader
9. **Archive hash verify**: oracle fixture `good.tar.gz` + `hash-mismatch.frozen.yaml`
