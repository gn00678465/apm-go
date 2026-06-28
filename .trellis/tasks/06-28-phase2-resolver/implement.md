# Phase 2: Implementation Plan — Dependency Resolution Engine

## Step 1: Semver Engine (req-rs-007, req-rs-014)

### 1.1 Add `deps.dev/util/semver` dependency
```bash
go get deps.dev/util/semver
```

### 1.2 Create `internal/semver/semver.go`
- `Satisfies(version, rangeExpr string) (bool, error)` — wrapper around `depsdev.NPM.ParseConstraint` + `Match`
- `MaxSatisfying(versions []string, rangeExpr string) (string, error)` — filter + sort + pick highest
- `CompareVersions(a, b string) int` — wrapper around `depsdev.NPM.Compare`
- `IsSemverRange(ref string) bool` — attempt parse as NPM constraint, return success
- `TieBreakBuildMeta(tagA, tagB string) string` — bytewise ASCII compare of full tag strings

### 1.3 Create `internal/semver/semver_test.go`
- Load `conformance-kit/oracle/resolution/semver-dialect.json`
- Test all 24 cases: 20 range_match, 3 tag_selection, 1 build_metadata_tie
- Table-driven tests for `IsSemverRange` (semver ranges vs literal refs)

**Verify**: `go test ./internal/semver/ -v` — all 24 oracle cases pass.

## Step 2: Reference Kind Classification (req-rs-008, req-rs-003)

### 2.1 Create `internal/resolver/classify.go`
- `ReferenceKind` enum (Local/Registry/GitSemver/GitLiteral/Marketplace)
- `RefType` enum (Semver/Literal/None)
- `ClassifyReference(ref *manifest.DependencyReference) ReferenceKind` — priority-ordered classification
- `ClassifyRef(ref string) RefType` — uses `semver.IsSemverRange`

### 2.2 Create `internal/resolver/classify_test.go`
- Table-driven: local paths → KindLocal
- Table-driven: registry entries (id + registry) → KindRegistry
- Table-driven: semver refs (`^1.2.0`, `~2.0`) → KindGitSemver
- Table-driven: literal refs (SHA, tag, branch) → KindGitLiteral
- Table-driven: marketplace entries → KindMarketplace
- Edge cases: no ref → KindGitLiteral, `git: parent` → follows parent's kind

**Verify**: `go test ./internal/resolver/ -run TestClassify -v` — all pass.

## Step 3: Lockfile Types (read-side only)

### 3.1 Create `internal/lockfile/types.go`
- `LockedDep` struct (RepoURL, VirtualPath, Source, Constraint, ResolvedTag, ResolvedCommit, ResolvedRef, ResolvedURL, ResolvedHash, ResolvedBy, ResolvedAt, Depth, plus raw Node for round-trip)
- `Lockfile` struct (Version, Dependencies, raw Node)
- `UniqueKey(dep LockedDep) string` — `(repo_url, virtual_path)` composite

### 3.2 Create `internal/lockfile/parse.go`
- `ParseLockfile(doc *yaml.Node) (*Lockfile, error)` — walk Node, extract fields
- Validate `lockfile_version` ("1" or "2", reject unknown per req-lk-004)
- Preserve unknown fields and x-* keys in raw Node (req-lk-011, req-lk-014)

### 3.3 Create `internal/lockfile/parse_test.go`
- Test against oracle fixtures: `lockfile/v1-git-only.yml`, `lockfile/v2-with-registry.yml`
- Test unknown version rejection
- Test round-trip preservation of unknown fields

**Verify**: `go test ./internal/lockfile/ -v` — oracle fixtures accepted.

## Step 4: Resolver Core (req-rs-001, req-rs-006, req-rs-013)

### 4.1 Create `internal/resolver/types.go`
- `ResolvedDep` struct
- `ResolutionResult` struct (Deps, Diagnostics, Errors)
- `ConstraintChain` struct (chain of `owner/repo@constraint` entries)
- `TagLister` interface
- `PackageLoader` interface
- `ResolverConfig` struct (MaxDepth int, defaults to 50)

### 4.2 Create `internal/resolver/resolver.go`
- `Resolve(rootManifest *manifest.Manifest, lock *lockfile.Lockfile, tags TagLister, loader PackageLoader, cfg ResolverConfig) (*ResolutionResult, error)`
- BFS queue (slice-based FIFO) with fixpoint re-expansion
- Constraints map: `map[uniqueKey][]ConstraintEntry` — accumulates all constraints per identity
- Pins map: `map[uniqueKey]TagInfo` — current winning resolution per identity
- `depOrder []string` — deterministic first-seen BFS order (never iterate maps for output)
- On second constraint for same key: recompute intersection, re-pin if changed, invalidate subtree
- Depth check against maxDepth
- `conflict_resolution: nest` rejection (req-rs-013)

### 4.3 Create `internal/resolver/diamond.go`
- `pickHighestInIntersection(constraints []ConstraintEntry, tags []TagInfo) (TagInfo, error)`
- For semver constraints: AND-combine all ranges, filter tags, pick highest via MaxSatisfying
- For literal constraints: all refs must be identical; mismatch → fail-closed
- `formatConflictDiagnostic(chains []ConstraintEntry) string` — req-rs-010 format
- Sort chains deterministically before formatting

### 4.4 Create `internal/resolver/resolver_test.go`
- In-memory `TagLister` returning canned tags
- In-memory `PackageLoader` returning canned manifests per (repo, version)
- Test: linear 3-level BFS (A→B→C) — correct traversal order
- Test: diamond with compatible constraints — intersection-pick selects highest
- Test: diamond with incompatible constraints — fail-closed with chain diagnostic
- **Test: fixpoint re-expansion** — diamond where intersection-pick selects a version
  different from first-seen, and the two versions declare different transitive deps.
  Assert final graph holds the re-pinned version's children (not the stale first-seen children).
- Test: depth limit exceeded — fail with chain
- Test: `conflict_resolution: nest` — rejected with req-rs-013 diagnostic
- Test: declaration order preserved in BFS
- Test: deterministic output (run twice, compare)

**Verify**: `go test ./internal/resolver/ -run TestResolve -v` — all pass.

## Step 5: Lock Replay (req-rs-004, req-rs-009, req-lk-009)

### 5.1 Create `internal/resolver/lock_replay.go`
- `ShouldReplay(manifestConstraint, lockedConstraint string) bool` — character-equal
- `ReplayDecision(dep DependencyReference, lock *lockfile.Lockfile) ReplayAction`
  - `ReplayAction` enum: Replay / ReResolve / NewDep
- `ValidateRegistryHash(expected, actual string) error` — req-rs-009

### 5.2 Create `internal/resolver/lock_replay_test.go`
- Table-driven: exact match → replay
- Table-driven: whitespace-only diff → re-resolve
- Table-driven: semantic-equivalent but different string → re-resolve
- Table-driven: new dep (no lock entry) → NewDep
- Hash match/mismatch tests for registry deps

**Verify**: `go test ./internal/resolver/ -run TestReplay -v` — all pass.

## Step 6: "Why" Diagnostic (req-rs-005)

### 6.1 Create `internal/resolver/why.go`
- `ComputeWhy(lock *lockfile.Lockfile, targetKey string) ([]WhyPath, error)`
- Build reverse index: `resolvedBy → []LockedDep`
- Bottom-up walk with cycle detection (visited set)
- Sort paths lexicographically by path tuple
- `WhyPath` struct: chain of `WhyEdge{ParentKey, ChildKey, Constraint}`

### 6.2 Create `internal/resolver/why_test.go`
- Fixture lockfile with known chains
- Test: direct dep — single trivial path
- Test: transitive dep — correct chain
- Test: multiple paths — lexicographic order
- Test: cycle detection — terminates without infinite loop

**Verify**: `go test ./internal/resolver/ -run TestWhy -v` — all pass.

## Step 7: Update Logic (req-rs-011, req-rs-012, req-lk-010)

### 7.1 Create `internal/resolver/update.go`
- `PlanFullUpdate(manifest, lock, tags, loader, cfg) (*ResolutionResult, error)` — re-resolve all direct deps, hold constraints, rewrite pins
- `PlanScopedUpdate(manifest, lock, tags, loader, cfg, packageName string) (*ResolutionResult, error)` — scope to named package + subtree, hold other pins
- Both check frozen install flag

### 7.2 Create `internal/resolver/update_test.go`
- Test: full update re-resolves all direct deps to new highest
- Test: scoped update only changes named package + subtree
- Test: scoped update holds other pins unchanged
- Test: refuse on frozen install without override

**Verify**: `go test ./internal/resolver/ -run TestUpdate -v` — all pass.

## Step 8: Integration + Coverage

### 8.1 Run full test suite
```bash
go test ./... -cover
go vet ./...
```

### 8.2 Coverage targets
- `internal/semver/` ≥ 90% (oracle-driven)
- `internal/resolver/` ≥ 80%
- `internal/lockfile/` ≥ 80%

### 8.3 Self-verification checklist
- [ ] 24/24 semver oracle cases pass
- [ ] All 5 reference kinds classified correctly
- [ ] BFS order matches declaration order
- [ ] Diamond intersection-pick selects highest
- [ ] Diamond fail-closed produces correct chain diagnostic
- [ ] Nest rejection works with correct diagnostic
- [ ] Depth limit triggers at configured value
- [ ] Lock replay: character-equal → replay, different → re-resolve
- [ ] Why diagnostic: correct chains, lexicographic, cycle-safe
- [ ] Full update re-resolves all; scoped update limits scope
- [ ] No network calls in tests (all via interfaces)
- [ ] `go vet` clean
- [ ] Coverage ≥ 80%

## Step Dependency Graph

```
Step 1 (semver) ─────────────────────────────────────┐
Step 2 (classify) ──── depends on Step 1 ────────────┤
Step 3 (lockfile types) ─────────────────────────────┤
Step 4 (resolver core) ── depends on Steps 1,2,3 ───┤
Step 5 (lock replay) ── depends on Steps 3,4 ────────┤
Step 6 (why) ── depends on Step 3 ───────────────────┤
Step 7 (update) ── depends on Steps 4,5 ─────────────┤
Step 8 (integration) ── depends on all ──────────────┘
```

Steps 1, 2, 3 can be done in parallel.
Step 4 depends on 1+2+3.
Steps 5 and 6 can be done in parallel after 3+4.
Step 7 depends on 4+5.
Step 8 is the final verification.
