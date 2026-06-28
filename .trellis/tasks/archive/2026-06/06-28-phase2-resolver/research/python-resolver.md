# Research: Python APM Dependency Resolution -- Full Implementation Analysis

- **Query**: Study the Python apm implementation's dependency resolution code for Go port
- **Scope**: internal (D:\Projects\apm-dev\apm\src\apm_cli\)
- **Date**: 2026-06-28

---

## 1. The Resolver Module

### Key Files

| File | Role |
|---|---|
| `src/apm_cli/deps/apm_resolver.py` | BFS engine (`APMDependencyResolver`): tree build, cycle detection, flattening |
| `src/apm_cli/deps/dependency_graph.py` | Data structures: `DependencyNode`, `DependencyTree`, `FlatDependencyMap`, `ConflictInfo`, `DependencyGraph` |
| `src/apm_cli/deps/lockfile.py` | `LockedDependency`, `LockFile` -- serialization/deserialization to `apm.lock.yaml` |
| `src/apm_cli/deps/git_reference_resolver.py` | Resolves git refs (branch/tag/commit/semver) to concrete SHAs via ls-remote or clone |
| `src/apm_cli/deps/git_semver_resolver.py` | Resolves semver range constraints (`^1.2.0`) against remote git tags |
| `src/apm_cli/deps/why_walker.py` | Inverted graph walker for `apm deps why` diagnostics |
| `src/apm_cli/drift.py` | Drift detection: ref change, orphan, config, stale-file drift; `build_download_ref` for lockfile replay |
| `src/apm_cli/install/phases/resolve.py` | Pipeline phase 1: wires resolver, download callback, lockfile loading |
| `src/apm_cli/install/phases/_skip_logic.py` | `_compute_skip_download` and `_should_use_locked_ref` predicates |
| `src/apm_cli/models/dependency/reference.py` | `DependencyReference` dataclass: parsing, identity, install paths |
| `src/apm_cli/models/dependency/identity.py` | `build_dependency_unique_key`, `build_canonical_dependency_string` |
| `src/apm_cli/models/dependency/types.py` | Enums: `GitReferenceType`, `VirtualPackageType`; dataclasses: `RemoteRef`, `ResolvedReference` |
| `src/apm_cli/marketplace/semver.py` | `SemVer` dataclass, `parse_semver`, `satisfies_range` -- the semver engine |
| `src/apm_cli/deps/registry/semver.py` | `is_semver_range`, `match_version`, `pick_best` -- registry-layer wrappers |

### Data Structures

#### `DependencyReference` (dataclass, ~100 fields)

```
repo_url: str                    # e.g. "owner/repo"
host: str | None                 # e.g. "github.com", "dev.azure.com"
host_type: str | None            # "gitlab" only currently
port: int | None                 # non-standard port
explicit_scheme: str | None      # "ssh", "https", "http", or None
reference: str | None            # branch/tag/commit/semver-range
alias: str | None
virtual_path: str | None         # for subdirectory or file virtual packages
is_virtual: bool
ado_organization/ado_project/ado_repo: str | None  # Azure DevOps
is_local: bool
local_path: str | None
is_parent_repo_inheritance: bool # git: parent expansion
artifactory_prefix: str | None
is_insecure: bool
allow_insecure: bool
skill_subset: list[str] | None
ssh_user: str | None
source: str | None               # "git" | "registry" | "local" | None
registry_name: str | None
is_marketplace: bool
marketplace_name/plugin_name/version_spec: str | None
```

Key methods:
- `parse(dependency_str)` -> classmethod, handles all input formats (shorthand, HTTPS, SSH/SCP, ssh://, local path)
- `parse_from_dict(entry)` -> classmethod, handles object-form YAML entries (`git:`, `path:`, `marketplace:`, `registry:`)
- `get_unique_key()` -> dedup/lockfile key (repo_url + host for non-github, or local_path, or repo_url/virtual_path)
- `get_identity()` -> same as unique_key but without ref/alias (for duplicate detection)
- `get_install_path(apm_modules_dir)` -> canonical filesystem path under apm_modules/
- `ref_kind` -> property: returns `"semver"` | `"literal"` | `None` (routing decision)
- `to_canonical()` -> scheme-free identity string
- `to_apm_yml_entry()` -> string or dict for manifest serialization

#### `DependencyNode` (dataclass)

```
package: APMPackage
dependency_ref: DependencyReference
depth: int = 0
children: list[DependencyNode]
parent: DependencyNode | None
is_dev: bool
```

- `get_id()` -> `unique_key + "#" + reference` (includes ref for tree node identity)
- `get_unique_key()` -> delegates to `dependency_ref.get_unique_key()` (excludes ref)
- `get_ancestor_chain()` -> breadcrumb string for error messages

#### `DependencyTree` (dataclass)

```
root_package: APMPackage
nodes: dict[str, DependencyNode]           # keyed by get_id() (includes #ref)
_nodes_by_depth: dict[int, list[DependencyNode]]
_nodes_by_unique_key: dict[str, DependencyNode]  # keyed by get_unique_key() (excludes ref)
max_depth: int
resolution_errors: list[str]
```

- `add_node(node)` -> inserts into all three indexes
- `get_node(unique_key)` -> looks up by unique_key (NOT by id), falls back to nodes dict

#### `FlatDependencyMap` (dataclass)

```
dependencies: dict[str, DependencyReference]  # keyed by unique_key
conflicts: list[ConflictInfo]
install_order: list[str]                       # ordered unique_keys
```

- `add_dependency(dep_ref, is_conflict)` -> first-wins: only the first entry for a unique_key is kept; conflicts are recorded but never block

#### `ConflictInfo` (dataclass)

```
repo_url: str
winner: DependencyReference
conflicts: list[DependencyReference]
reason: str  # always "first declared dependency wins"
```

#### `DependencyGraph` (dataclass)

```
root_package: APMPackage
dependency_tree: DependencyTree
flattened_dependencies: FlatDependencyMap
circular_dependencies: list[CircularRef]
resolution_errors: list[str]
```

---

## 2. The Install Command

### Entry Point

File: `src/apm_cli/commands/install.py` (Click command, re-exports from submodules)

The install command:
1. Parses `apm.yml` into `APMPackage`
2. Calls `run_install_pipeline(apm_package, ...)` from `src/apm_cli/install/pipeline.py`

### Pipeline Phases (in `run_install_pipeline`)

```
1. resolve     -- dependency resolution + lockfile check
2. policy_gate -- org policy enforcement
3. targets     -- target detection + integrator initialization
4. policy_target_check -- target-aware policy
5. download    -- parallel package pre-download
6. integrate   -- sequential integration loop + root primitives
7. cleanup     -- orphan cleanup + stale-file removal
8. lockfile    -- generate apm.lock.yaml
9. finalize    -- emit stats, return InstallResult
```

The resolve phase (`install/phases/resolve.py`) does:
1. Load existing `apm.lock.yaml` -> `ctx.existing_lockfile`
2. Create `apm_modules/` directory
3. Build auth resolver + `GitHubPackageDownloader`
4. Create `APMDependencyResolver` with a `download_callback` closure
5. Call `resolver.resolve_dependencies(manifest_anchor)` -> `DependencyGraph`
6. Extract `flat_deps.get_installation_list()` -> `ctx.deps_to_install`
7. Optionally apply `--only` filter
8. Compute `ctx.intended_dep_keys` for orphan detection

---

## 3. The Update Command

File: `src/apm_cli/commands/update.py`

`apm update` is equivalent to `apm install --update` plus an interactive plan-and-confirm gate:

1. **Parse manifest** -> `APMPackage.from_apm_yml("apm.yml")`
2. **Stage a deep copy** of the package (so declined/dry-run paths don't mutate the original)
3. **Resolve revision-pin updates** (SHA pins for annotated tags) via `resolve_revision_pin_updates` -- does bounded `git ls-remote` pass
4. **Call `_install_apm_dependencies`** with `update_refs=True` and a `plan_callback`
5. Inside the pipeline, after resolve phase, the pipeline calls `plan_callback`:
   - `build_update_plan(old_lockfile, ctx.deps_to_install)` produces an `UpdatePlan`
   - Plan is rendered as ASCII text (`[~]` updated, `[+]` added, `[-]` removed)
   - User is prompted "Apply these changes? [y/N]" (default No)
   - On confirm: revision pins are written to `apm.yml`, pipeline continues
   - On decline/dry-run: pipeline returns early with empty `InstallResult`
6. After pipeline completes, annotate lockfile with resolved tag names

### `UpdatePlan` data model (from `install/plan.py`)

```
PlanEntry:
  action: "update" | "add" | "remove" | "unchanged"
  dep_key: str
  old_ref/new_ref: str | None
  old_sha/new_sha: str | None
  deployed_files: list[str]

UpdatePlan:
  entries: list[PlanEntry]
  has_changes: bool
```

---

## 4. Reference Kind Classification

File: `src/apm_cli/models/dependency/reference.py` -- `DependencyReference.ref_kind` property

The property classifies `reference` into three kinds:

| Kind | Condition | Routing |
|---|---|---|
| `None` | `reference` is empty/None | Use remote's default branch |
| `"semver"` | `_is_valid_registry_semver_range(reference)` returns True | Route to `GitSemverResolver` |
| `"literal"` | Non-empty, does not parse as semver range | Branch name, tag name, SHA -- use as-is |

`_is_valid_registry_semver_range` (in `identity.py`) delegates to `deps/registry/semver.py:is_semver_range()`:
- Splits on spaces (AND-combined constraints)
- Each component must be: a wildcard (`1.2.x`), a comparison-prefixed version (`>=1.2.3`), a caret/tilde-prefixed version (`^1.2.3`, `~1.2.3`), an equality-prefixed version (`=1.2.3`), or a bare exact version (`1.2.3`)

Invalid semver-looking refs (e.g. `^foo`) raise `InvalidSemverRangeError` at parse time.

### Five source kinds (not formally enumerated, but implicit in routing):

| Source | Indicators | Handler |
|---|---|---|
| **Local** | `is_local=True`, `local_path` set | `_copy_local_package` |
| **Registry** | `source="registry"` | `RegistryPackageResolver.download_package` |
| **Git-semver** | `ref_kind == "semver"`, not local/registry/proxy | `GitSemverResolver` -> concrete tag -> git clone |
| **Git-literal** | `ref_kind == "literal"` or `ref_kind is None` | `GitHubPackageDownloader.download_package` |
| **Marketplace** | `is_marketplace=True` | `resolve_marketplace_plugin` -> resolves to one of the above |
| **Artifactory** | `artifactory_prefix` set | `GitHubPackageDownloader` with proxy routing |

---

## 5. BFS Traversal

File: `src/apm_cli/deps/apm_resolver.py` -- `APMDependencyResolver.build_dependency_tree`

### Algorithm

Level-batched parallel BFS with ThreadPoolExecutor:

```python
processing_queue: deque[(dep_ref, depth, parent_node, is_dev)]
queued_keys: set[str]  # O(1) dedup on get_unique_key()

# Seed with root deps (depth=1) and root devDeps (depth=1, is_dev=True)
# Marketplace deps resolved before enqueuing

while processing_queue:
    # Drain one level (all items at current_depth)
    level_items = [popleft() while front.depth == current_depth]

    # Phase A (main thread): dedup + node creation
    for each item:
        queued_keys.discard(key)
        if depth > max_depth(50): skip
        existing = tree.get_node(unique_key)  # by unique_key, NOT get_id
        if existing and existing.depth <= depth:
            # prod wins over dev: promote if needed
            # attach to parent's children list
            continue  # FIRST-WINS: earlier/shallower node wins
        create DependencyNode, add to tree
        append to work_items

    # Phase B (workers): load packages via _try_load_dependency_package
    if max_parallel == 1: sequential
    else: ThreadPoolExecutor(max_workers=min(max_parallel, len(work_items)))
    # executor.map preserves submission order for deterministic enqueuing

    # Phase C (main thread): integrate results, enqueue sub-deps
    for each loaded_package:
        node.package = loaded_package
        for sub_dep in loaded_package.get_apm_dependencies():
            expand git:parent if needed
            expand/reject remote local paths
            resolve marketplace deps
            if sub_dep.get_unique_key() not in queued_keys:
                processing_queue.append((sub_dep, depth+1, node, is_dev))
                queued_keys.add(sub_dep.get_unique_key())
```

### `_try_load_dependency_package` (the download callback wrapper)

1. If install_path doesn't exist and download_callback is set:
   - Check dedup set `_downloaded_packages` under `_download_lock`
   - Reserve the slot atomically, then call the callback outside the lock
   - On failure, release the reservation
2. If install_path exists (or was just downloaded):
   - Look for `apm.yml` -> `APMPackage.from_apm_yml` (enables transitive resolution)
   - Look for `SKILL.md` -> create minimal `APMPackage` (no transitive deps)
   - Neither -> return None

### Concurrency Model

- Default: 4 workers per level (`_DEFAULT_RESOLVE_PARALLEL = 4`)
- Controlled via `APM_RESOLVE_PARALLEL` env var (diagnostic knob, not user-facing)
- `_download_lock` (threading.Lock) protects `_downloaded_packages` set and `_rejected_remote_local_keys` set
- Tree mutations (add_node, children.append, queue operations) are all on the main thread
- Worker pool only runs `_try_load_dependency_package` (I/O: git clone, file copy)

**Go porting note**: Replace `ThreadPoolExecutor` with `errgroup.Group` or a worker pool pattern with goroutines + channels. The GIL-atomic dict/set mutations in CPython need explicit `sync.Mutex` in Go.

---

## 6. Diamond Conflict Handling

### Actual strategy: uniform first-wins (NOT tri-modal)

The resolver uses a single conflict strategy everywhere:

1. **Tree-build dedup** (`build_dependency_tree`):
   - `tree.get_node(unique_key)` checks by `get_unique_key()` which EXCLUDES the `#ref` suffix
   - If an existing node at equal-or-shallower depth exists, the new entry is dropped
   - The only special case: if existing was `is_dev=True` and new is `is_dev=False`, the existing node is promoted to prod

2. **Flattening** (`flatten_dependencies`):
   - Walks depth-by-depth (breadth-first order)
   - First occurrence of each `unique_key` is kept
   - Subsequent occurrences are recorded as `ConflictInfo` with `is_conflict=True` but the winner is always the first-declared

3. **ConflictInfo**:
   - `reason` is always `"first declared dependency wins"`
   - Conflicts are recorded but NEVER block the install
   - No version reconciliation, no semver compatibility check between conflicting refs

### The three resolution/replay paths (what "tri-modal" may refer to)

In the `download_callback` closure (`install/phases/resolve.py`), there are three distinct source-resolution paths:

| Path | Gate | Behavior |
|---|---|---|
| **Registry** | `dep_ref.source == "registry"` | Uses `RegistryPackageResolver`; lockfile replay via `resolved_url`+`resolved_hash` |
| **Local** | `dep_ref.is_local and dep_ref.local_path` | Uses `_copy_local_package` |
| **Git** (semver + literal) | Everything else | Runs `_maybe_resolve_git_semver` then `build_download_ref` then `downloader.download_package` |

Each path has its own "honor the lock" logic but the conflict resolution is always first-wins.

---

## 7. Semver Range Evaluation

### Core engine: `src/apm_cli/marketplace/semver.py`

#### `SemVer` dataclass (frozen, hashable)

```
major: int
minor: int
patch: int
prerelease: str  # empty = no prerelease
build_meta: str  # ignored in comparisons
```

Comparison uses `_cmp_tuple()`:
- `(major, minor, patch, 1, ())` for releases (1 sorts after 0 for prerelease)
- `(major, minor, patch, 0, tuple_of_identifiers)` for prereleases
- Numeric identifiers sort before alphanumeric (per semver 2.0.0 spec)

#### `parse_semver(text) -> SemVer | None`

Regex: `^(\d+)\.(\d+)\.(\d+)(?:-([0-9A-Za-z-]+(?:\.[0-9A-Za-z-]+)*))?(?:\+([0-9A-Za-z-]+(?:\.[0-9A-Za-z-]+)*))?$`

#### `satisfies_range(version, range_spec) -> bool`

Supports:
- **Caret**: `^1.2.3` -> `>=1.2.3, <2.0.0`; `^0.2.3` -> `>=0.2.3, <0.3.0`; `^0.0.3` -> exact `0.0.3`
- **Tilde**: `~1.2.3` -> `>=1.2.3, <1.3.0`
- **Comparison**: `>=`, `>`, `<=`, `<` followed by version
- **Equality**: `=1.2.3` (explicit, NOT `==`)
- **Wildcard**: `1.2.x` or `1.2.*`
- **Exact**: `1.2.3`
- **Combined (AND)**: space-separated, e.g. `>=1.0.0 <2.0.0`

### Git-source semver resolution: `GitSemverResolver` (`deps/git_semver_resolver.py`)

Algorithm:
1. Call `RefResolver.list_remote_refs(owner_repo)` -> list of `RemoteRef`
2. For each tag ref, try patterns in order: `v{version}`, `{name}--v{version}`, `{name}-v{version}`
3. If no match, try fallback: `{version}` (bare version tag)
4. Filter: skip pre-release unless `include_prerelease=True`
5. Filter: must `satisfies_range(version, constraint)`
6. Pick highest matching version (sort by `SemVer` ordering)
7. Return `GitSemverResolution(constraint, resolved_version, resolved_tag, resolved_sha, matched_pattern, resolved_at)`

### Registry semver: `deps/registry/semver.py`

Thin wrappers:
- `is_semver_range(spec)` -> validates all space-separated components
- `match_version(spec, version)` -> parse + satisfies_range
- `pick_best(spec, versions)` -> highest matching version from a list

---

## 8. Lockfile Replay (Honor the Lock)

### Core predicate: `_should_use_locked_ref` (`install/phases/_skip_logic.py`)

```python
def _should_use_locked_ref(locked_ref, update_refs) -> bool:
    return bool(locked_ref) and locked_ref != "cached" and not update_refs
```

### `build_download_ref` (`drift.py`, line 290)

When `existing_lockfile` exists AND `not update_refs` AND `not ref_changed`:
1. Look up `locked_dep` by `dep_ref.get_unique_key()`
2. Build `overrides` dict:
   - Restore locked host + registry_prefix (for air-gapped reproducibility)
   - Restore `is_insecure`/`allow_insecure` flags
   - Registry deps: set `reference = locked_dep.version`, `source = "registry"`
   - Git deps: if `_should_use_locked_ref(locked_dep.resolved_commit)` -> set `reference = locked_dep.resolved_commit`
   - Proxy deps without commit SHA: preserve `locked_dep.resolved_ref`
3. Return `dataclasses.replace(dep_ref, **overrides)` (new instance, original untouched)

### `detect_ref_change` (`drift.py`, line 88)

Returns True when re-download is needed:
- `update_refs=True` -> always False (intentional re-resolve)
- `locked_dep is None` -> False (new package, not drift)
- Source flip (manifest changed resolver) -> True
- Registry deps: check if locked version still satisfies manifest range
- Git-semver deps: compare `dep_ref.reference` vs `locked_dep.constraint`
- Git/local deps: compare `dep_ref.reference` vs `locked_dep.resolved_ref`
- HTTP/HTTPS transport flip -> True

### Git-semver lockfile replay (`install/phases/resolve.py`, `_maybe_resolve_git_semver`)

When `not update_refs` and lockfile has `constraint == dep_ref.reference` and all fields present:
- Rebuild `GitSemverResolution` from lockfile fields directly
- NO `git ls-remote` call -- pure replay
- Only re-resolves on `--update`, `--refresh`, or when manifest constraint changed

### Skip-download predicate (`_compute_skip_download`)

```python
return install_path_exists and (
    (is_cacheable and not update_refs)
    or (already_resolved and not update_refs)
    or lockfile_match
)
```

---

## 9. "Why" Diagnostics

### Walker: `src/apm_cli/deps/why_walker.py`

#### Query resolution (`resolve_package_query`)

Supports four query forms (tried in order):
1. Full `repo_url` exact match
2. `owner/repo` short form
3. Bare basename (e.g. `shared-utils`), if unique
4. Unique key from `LockedDependency.get_unique_key()`

Raises `PackageNotInstalledError` or `AmbiguousPackageError`.

#### `compute_why(lockfile, target) -> WhyResult`

Walks the `resolved_by` chain from target back to root:

```python
# Build lookup: repo_url -> LockedDependency (canonical: non-virtual, lowest-depth)
by_url: dict[str, LockedDependency]

# Direct dep (resolved_by is None): single trivial path with parent_key=None

# Transitive dep: iterative worklist
worklist = [(target, initial_chain, visited_set)]
while worklist:
    current, chain, visited = worklist.pop()
    parent_url = current.resolved_by
    if parent_url is None: record chain, continue
    parent = by_url.get(parent_url)
    if parent is None: record chain (corrupt/partial)
    if parent in visited: record chain (cycle)
    new_edge = WhyEdge(parent_key=parent.resolved_by, child_key=parent.repo_url)
    worklist.append((parent, (new_edge, *chain), visited | {parent.repo_url}))
```

Defensive bounds: `max_depth = max(64, len(by_url)+1)`, `max_paths = 256`

#### Result types

```
WhyEdge(parent_key: str|None, child_key: str, constraint: str|None)
WhyPath(chain: tuple[WhyEdge, ...])
WhyResult(target: LockedDependency, is_direct: bool, paths: tuple[WhyPath, ...])
```

#### CLI rendering (`src/apm_cli/commands/deps/why.py`)

- Human output: ASCII tree with `+--` indentation, annotations like `[constraint: ^1.2.0, declared in apm.yml]`
- JSON output: structured payload with `package`, `paths[].chain[].repo_url/constraint/is_direct`

---

## Go Porting Notes

### Python-isms to translate

| Python | Go Equivalent |
|---|---|
| `@dataclass` with `field(default_factory=...)` | Struct with constructor function |
| `dataclasses.replace(obj, **overrides)` | Manual copy + field overrides, or a `With*` method |
| `collections.deque` | `container/list` or a slice-based queue |
| `ThreadPoolExecutor` with `executor.map` | `errgroup.Group` with goroutines; results collected via channel or slice+mutex |
| `threading.Lock` guarding sets/dicts | `sync.Mutex` or `sync.RWMutex` |
| Protocol (duck typing) | Interface |
| `from __future__ import annotations` (lazy eval) | Not needed in Go |
| `frozenset` in visited sets | map[string]struct{} |
| `inspect.signature` for callback compat | Not needed -- define interface upfront |
| YAML via `yaml.safe_load`/`yaml.dump` | `gopkg.in/yaml.v3` |
| `re.compile` regex | `regexp.MustCompile` |
| Enum (GitReferenceType, VirtualPackageType) | `type X int` with `iota` constants, or string constants |
| `tuple[SemVer, str, str, str]` | Named struct |
| `builtins.set` / `builtins.list` / `builtins.dict` | Native Go types (the Python code shadows builtins due to Click command name collision) |

### Architecture considerations for Go port

1. **Unique key vs ID**: `get_unique_key()` (excludes `#ref`) is the dedup/lockfile key; `get_id()` (includes `#ref`) is the tree node identity. The Go port needs both and must be clear about which is used where.

2. **First-wins conflict**: No version reconciliation needed. The flattener simply checks if a key is already seen.

3. **Lockfile format**: YAML with two schema versions (v1 and v2). V2 adds registry and git-semver fields. Forward-compatible: unknown fields are preserved through `_unknown_fields` dict.

4. **Three resolution paths in download callback**: Registry, Local, and Git (with sub-paths for semver vs literal refs). Each has its own lockfile-replay logic.

5. **Level-batched parallel BFS**: The tree is built level-by-level. Phase A (dedup) and Phase C (enqueue children) run on the main goroutine; Phase B (download/clone) runs on workers. This is the critical concurrency boundary.

6. **Semver**: The semver engine is self-contained (~240 LOC) and dependency-free. Port it directly or use a Go semver library that supports caret/tilde/wildcard/comparison/combined ranges.

7. **DependencyReference.parse**: The parser is ~700 LOC handling SSH, SCP, HTTPS, shorthand, local paths, Azure DevOps, GitLab, Artifactory, and virtual packages. This is the most complex single function to port.
