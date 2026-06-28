# Phase 2: Design — Dependency Resolution Engine

## Package Layout

```
internal/
  resolver/
    resolver.go       — BFS engine, diamond conflict, depth limit
    resolver_test.go
    types.go           — ResolvedDep, ResolutionResult, ConflictChain, WhyPath
    classify.go        — ReferenceKind classification (req-rs-008, req-rs-003)
    classify_test.go
    diamond.go         — intersection-pick, fail-closed logic (tested via resolver_test.go)
    why.go             — bottom-up "why" walker (req-rs-005)
    why_test.go
    lock_replay.go     — constraint equality, replay decision (req-rs-004)
    lock_replay_test.go
    update.go          — full/scoped update logic (req-rs-011/012)
    update_test.go
  semver/
    semver.go          — wrapper around deps.dev/util/semver
    semver_test.go     — oracle-driven tests (semver-dialect.json)
  lockfile/
    types.go           — LockedDependency, Lockfile structs (read-only for Phase 2)
    parse.go           — parse apm.lock.yaml via SafeLoad
    parse_test.go
```

## Key Types

### ReferenceKind (classify.go)

```go
type ReferenceKind int
const (
    KindLocal ReferenceKind = iota
    KindRegistry
    KindGitSemver
    KindGitLiteral
    KindMarketplace
)

type RefType int
const (
    RefSemver RefType = iota
    RefLiteral
    RefNone
)
```

`ClassifyReference(entry)` returns `ReferenceKind` — deterministic, no I/O.
`ClassifyRef(ref string)` returns `RefType` — checks if ref parses as semver range.

### ResolvedDep (types.go)

```go
type ResolvedDep struct {
    RepoURL      string
    VirtualPath  string
    Kind         ReferenceKind
    Constraint   string            // original manifest range (verbatim)
    ResolvedTag  string            // pinned tag (git-semver)
    ResolvedRef  string            // pinned commit/branch/tag (git-literal)
    Depth        int
    ResolvedBy   string            // chain that contributed tightest constraint
    ResolvedAt   time.Time         // advisory timestamp
    Children     []string          // unique keys of transitive deps
}
```

### Resolver Interfaces

```go
// TagLister abstracts git ls-remote for testing
type TagLister interface {
    ListTags(repoURL string) ([]TagInfo, error)
}

type TagInfo struct {
    Name   string // e.g. "v1.2.3"
    Commit string // SHA
}

// PackageLoader abstracts package download/clone for testing
type PackageLoader interface {
    LoadPackage(dep DependencyReference, installPath string) (*manifest.Manifest, error)
}
```

BFS resolver accepts these interfaces. Tests inject in-memory implementations.

### Lockfile Types (lockfile/types.go)

```go
type LockedDep struct {
    RepoURL        string
    VirtualPath    string
    Source         string            // "git" | "registry" | "local"
    Constraint     string           // verbatim from manifest at lock time
    ResolvedTag    string
    ResolvedCommit string
    ResolvedRef    string
    ResolvedURL    string           // registry download URL (advisory)
    ResolvedHash   string           // registry archive hash (authoritative)
    ResolvedBy     string
    ResolvedAt     string           // ISO 8601
    Depth          int
}

type Lockfile struct {
    Version      string           // "1" or "2"
    Dependencies []LockedDep
}
```

## Algorithm Details

### BFS Resolver with Fixpoint (resolver.go)

**Problem**: BFS first-see pins a version and expands its children. A later constraint
may narrow the intersection to a different version whose children differ. Naive
first-see-and-skip produces a stale transitive graph.

**Solution**: BFS + fixpoint re-expansion. When a new constraint changes a package's pin,
invalidate its subtree and re-expand from the new version's manifest.

```
Input: root manifest, lockfile (optional), TagLister, PackageLoader, config
Output: ResolutionResult { deps []ResolvedDep, errors []Diagnostic }

// Phase 1: BFS to collect all constraints
queue := [(rootDep, depth=1, parentChain=[]) for each dep in manifest order]
constraints := map[uniqueKey][]ConstraintEntry  // all constraints per identity
depOrder    := []string{}                       // deterministic key order (first-seen)
parentGraph := map[uniqueKey][]childKey          // who depends on whom

while queue not empty:
    entry, depth, parentChain := dequeue

    if depth > maxDepth(50):
        return error with chain at cap (req-rs-006)

    key := uniqueKey(entry)
    chain := append(parentChain, formatChainEntry(entry))

    constraints[key] = append(constraints[key], {chain, entry.ref, depth})
    if key not in depOrder:
        depOrder = append(depOrder, key)

    // Resolve version (TagLister call for git-semver)
    pin := pickHighestInIntersection(constraints[key], TagLister)
    if pin.empty:
        return fail-closed with both chains (req-rs-010)

    prevPin := pins[key]
    pins[key] = pin

    // Load sub-manifest for transitive deps
    subManifest := PackageLoader.LoadPackage(entry, pin, ...)
    if subManifest != nil:
        newChildren := extractDeps(subManifest)
        parentGraph[key] = childKeys(newChildren)
        for each subDep in newChildren:
            queue.enqueue(subDep, depth+1, chain)

    // Fixpoint: if pin changed from a previous resolution, invalidate subtree
    if prevPin != "" && prevPin != pin:
        invalidateSubtree(key, pins, constraints, parentGraph)
        // Re-enqueue this key's children from the NEW manifest
        reExpandFrom(key, pin, queue, depth)

// Phase 2: Build result in deterministic order
result := []ResolvedDep{}
for _, key := range depOrder:  // sorted by first-seen BFS order
    dep := buildResolvedDep(key, pins[key], constraints[key])
    dep.ResolvedBy = tightestChain(constraints[key])
    result = append(result, dep)
```

**Termination guarantee**: each fixpoint iteration strictly narrows a pin (intersection
can only shrink). With finite versions per package, the loop terminates. Bounded by
`maxDepth * identityCount` iterations.

**Determinism**: `depOrder` tracks first-seen order for output. Conflict diagnostics
iterate `depOrder` (not map keys). All sorting uses stable comparisons.

### Diamond Intersection (diamond.go)

```go
// pickHighestInIntersection evaluates all constraints for a package identity
// and returns the highest version satisfying ALL of them.
func pickHighestInIntersection(
    constraints []ConstraintEntry,
    tags []TagInfo,  // from TagLister
) (TagInfo, error)
```

For git-semver deps: AND-combine all semver ranges, filter tags, pick highest.
For git-literal deps: all refs must be identical string; any mismatch → fail-closed.
For registry deps: version range intersection against available versions.

When intersection is non-empty: pick highest version, record `resolved_by` as the
chain contributing the tightest lower bound.
When intersection is empty: fail-closed with both chains formatted per req-rs-010.

**Chain format (req-rs-010)**: `<owner>/<repo>@<constraint> -> <owner>/<repo>@<constraint>`.
Both chains from root to conflict point. Sorted deterministically.

### Semver Wrapper (semver/semver.go)

```go
import depsdev "deps.dev/util/semver"

type TagInfo struct {
    Name   string // full tag string, e.g. "v1.2.3+build.1"
    Commit string // SHA
}

func Satisfies(version, rangeExpr string) (bool, error)
func MaxSatisfying(tags []TagInfo, rangeExpr string) (TagInfo, bool, error)
func CompareVersions(a, b string) int
func IsSemverRange(ref string) bool
```

`MaxSatisfying` operates on `TagInfo` (not bare strings) so it can:
1. Strip the leading `v` for semver parsing while preserving the original tag name
2. Apply pre-release exclusion (same-tuple rule)
3. Break build-metadata ties by **bytewise ASCII** on the full `TagInfo.Name` (req-rs-014)
4. Return the winning `TagInfo` with its commit SHA intact for lockfile recording

### Lock Replay (lock_replay.go)

```go
func ShouldReplay(manifestConstraint, lockedConstraint string) bool {
    return manifestConstraint == lockedConstraint  // character-equal, including whitespace
}
```

### "Why" Diagnostic (why.go)

```go
func ComputeWhy(lockfile *Lockfile, targetKey string) ([]WhyPath, error)
```

Bottom-up walk from target to root via `ResolvedBy` chain. Cycle detection via visited set. Returns paths sorted lexicographically by path tuple.

### Update Logic (update.go)

```go
func PlanFullUpdate(manifest, lockfile, tagLister) ResolutionResult
func PlanScopedUpdate(manifest, lockfile, tagLister, packageName) ResolutionResult
```

Full update: re-resolve all direct deps. Scoped update: only named package + subtree, hold other pins.

## Boundary with Phase 3

Phase 2 produces `ResolutionResult` (in-memory). Phase 3 consumes it to:
- Serialize to `apm.lock.yaml`
- Download/clone packages
- Verify hashes

The `LockedDep` struct defined here is the read-side contract. Phase 3 adds write-side serialization.

## Boundary with Phase 1

Phase 1's `DependencyReference` (from `ParseDepString`/`ParseDepDict`) is the input to `ClassifyReference`. No changes to Phase 1 code needed — the resolver consumes `DependencyReference` as-is.

## Error Strategy

All errors are `Diagnostic` structs (reusing Phase 1's type) with:
- `Level`: error/warning
- `Req`: req-ID (e.g. "req-rs-001")
- `Message`: human-readable, includes chain format for conflicts

## Testing Strategy

1. **Semver**: oracle-driven (semver-dialect.json, 24 cases)
2. **Classification**: table-driven for all 5 kinds + edge cases
3. **BFS**: in-memory PackageLoader returning canned manifests
4. **Diamond**: fixture graphs with known intersection/empty-intersection cases
5. **Lock replay**: table-driven string equality tests
6. **Why**: fixture lockfiles with known chains
7. **No network**: TagLister + PackageLoader are interfaces with in-memory test implementations
