# Research: Python APM Install CLI Surface

- **Query**: What does `apm install` do end-to-end, and what CLI surface is needed in apm-go for A/B testing?
- **Scope**: internal (D:\Projects\apm-dev\apm\)
- **Date**: 2026-06-29

## 1. `apm install` CLI Interface

Source: `src/apm_cli/commands/install.py` (Click command, lines 885-1098)

### Positional Arguments

| Argument | Description |
|---|---|
| `PACKAGES` | Zero or more package specifiers (`nargs=-1`). When absent, installs from `apm.yml`. When present, validates, adds to `apm.yml`, then installs. |

### Key Flags (relevant to A/B testing)

| Flag | Type | Description |
|---|---|---|
| `--dry-run` | bool | Show what would be installed without installing |
| `--update` | bool | Re-resolve refs to latest (deprecated: prefer `apm update`) |
| `--frozen` | bool | Refuse if `apm.lock.yaml` is missing or out of sync with `apm.yml` (CI-safe) |
| `--verbose` / `-v` | bool | Detailed output |
| `--force` | bool | Overwrite collisions |
| `--only` | choice: `apm`/`mcp` | Install only specific dependency type |
| `--runtime` | string | Target specific runtime (copilot, claude, codex, cursor, etc.) |
| `--exclude` | string | Exclude specific runtime |
| `--target` / `-t` | string(s) | Target harness(es) to deploy to (comma-separated) |
| `--parallel-downloads` | int (default 4) | Concurrent downloads |
| `--dev` | bool | Install as devDependency |
| `--global` / `-g` | bool | Install to user scope (`~/.apm/`) |
| `--ssh` / `--https` | bool | Protocol preference |
| `--allow-insecure` | bool | Allow `http://` deps |
| `--skill NAME` | multiple | Install only named skill(s) from bundle |
| `--no-policy` | bool | Skip org policy enforcement |
| `--refresh` | bool | Re-fetch all deps from upstream |
| `--frozen` | bool | Refuse if lockfile missing/stale |
| `--mcp NAME` | string | Add an MCP server entry (separate flow) |
| `--root DIR` | path | Install into DIR instead of $PWD |

### Full Flag List (for completeness, less relevant to A/B)

`--allow-insecure-host`, `--allow-protocol-fallback`, `--trust-transitive-mcp`, `--trust-canvas-extensions`, `--legacy-skill-paths`, `--as ALIAS`, `--registry URL`, `--transport`, `--url`, `--env KEY=VALUE`, `--header KEY=VALUE`, `--mcp-version`, `--audit off/warn/block`, `--no-audit`.

## 2. `apm install` End-to-End Pipeline

Source: `_install_apm_packages()` (install.py lines 1643-1985), `_install_apm_dependencies()` (lines 2030-2098), `install/service.py`, `install/pipeline.py`

### Phase Sequence

```
1. CLI arg parsing + scope resolution
2. Auto-bootstrap apm.yml if packages given but no manifest
3. Package validation: canonicalize, check accessibility, add to apm.yml
4. Parse apm.yml -> APMPackage model
5. Insecure dependency check
6. Dry-run branch (renders preview and exits) OR continue
7. Migrate legacy lockfile (apm.lock -> apm.lock.yaml)
8. APM install pipeline (via InstallService):
   a. Resolve phase: BFS dependency resolution (APMDependencyResolver)
   b. Download phase: parallel git clone/fetch to apm_modules/
   c. Integration phase: copy primitives to target dirs (.github/, .claude/, etc.)
   d. Lockfile write: generate apm.lock.yaml with resolved commits, content hashes
   e. Policy gate: org policy enforcement
   f. Audit: optional security scan
9. Collect transitive MCP dependencies from resolved packages
10. MCP integration: write MCP server configs to target runtime configs
11. LSP integration
12. Post-install summary (timing, counts, diagnostics)
```

### Key Observation for A/B Testing

The **resolver** (step 8a) is the core logic being ported to Go. The resolver:
- Takes a manifest (parsed `apm.yml`) + existing lockfile + tag lister + package loader
- Outputs a list of resolved dependencies with pinned refs/commits
- This is exactly what `apm-go/internal/resolver/resolver.go` implements

## 3. Observable Outputs for A/B Testing

### 3a. `apm install --dry-run` Output

Source: `install/presentation/dry_run.py`

```
[i] Dry run mode - showing what would be installed:
APM dependencies (N):
  - owner/repo#ref -> install
MCP dependencies (N):
  - mcp-server-name
Files that would be removed (...): N
[v] Dry run complete - no changes made
```

The dry-run path does NOT run the resolver. It just lists declared deps from `apm.yml`.

### 3b. `apm.lock.yaml` (the lockfile)

Source: `deps/lockfile.py`

This IS the canonical resolver output. Format:

```yaml
lockfile_version: '1'
generated_at: '2026-06-16T23:58:41.693187+00:00'
apm_version: 0.20.0
dependencies:
- repo_url: owner/repo
  host: github.com           # optional
  resolved_commit: abc123...
  resolved_ref: main
  version: 1.2.3
  depth: 1
  resolved_by: ""            # empty for direct deps
  package_type: apm_package
  deployed_files: [...]
  deployed_file_hashes: {...}
  content_hash: sha256:...
  is_dev: false
  constraint: "^1.2.0"       # if semver
  resolved_tag: "v1.2.3"     # if semver
  source: local              # for local deps
  local_path: ./packages/foo # for local deps
  virtual_path: subdir       # for virtual subdirectory
  skill_subset: [skill-a]    # if --skill filter
```

**This is the primary artifact for A/B comparison.** Comparing Python's `apm.lock.yaml` against Go's resolution output (mapped to the same structure) lets you verify resolver correctness.

### 3c. Exit Codes

| Code | Meaning |
|---|---|
| 0 | Success |
| 1 | General error (auth, policy, dep failure, etc.) |
| 2 | Usage error (invalid flags, conflict) |

### 3d. `apm update` Output (Plan)

Source: `commands/update.py`, `install/plan.py`

The `apm update` command shows an interactive plan before applying:

```
[i] Update plan for apm.yml

  [~] owner/repo
      ref: main (abc1234 -> def5678)
      files: .agents/skills/foo/SKILL.md, +3 more

  [+] new-org/new-repo
      ref: v1.0.0 (abc1234, new)

  [-] old-org/old-repo
      ref: main (abc1234, removed)

  2 updated, 1 added, 1 removed
  [~] updated  [+] added  [-] removed

Apply these changes? [y/N]
```

The `UpdatePlan` / `PlanEntry` dataclass (install/plan.py) is a structured diff:
- `action`: update / add / remove / unchanged
- `dep_key`: unique key
- `old_resolved_ref`, `old_resolved_commit`
- `new_resolved_ref`, `new_resolved_commit`
- `deployed_files`

## 4. `apm lock` Command

Source: `commands/lock.py`

**Purpose:** Resolve deps and write `apm.lock.yaml` WITHOUT deploying any files.

This is the **ideal A/B testing surface**:
- Runs the full resolver + download
- Writes `apm.lock.yaml`
- Does NOT copy files to targets
- Exit code: 0 on success, 1 on error

```bash
apm lock                 # Resolve and write lockfile
apm lock --update        # Re-resolve to latest SHAs
apm lock --verbose       # Show resolution details
```

Flags: `--verbose`, `--global`, `--update`, `--no-policy`, `--target`, `--parallel-downloads`.

## 5. `apm deps why <package>`

Source: `commands/deps/why.py`, `deps/why_walker.py`

### CLI Interface

```bash
apm deps why <package>          # Text output
apm deps why <package> --json   # JSON output
apm deps why <package> --global # User-scope lockfile
```

### Text Output Format

For a **direct** dependency:
```
[i] owner/repo@v1.2.3  (direct dependency)

    owner/repo   [declared in apm.yml]
```

For a **transitive** dependency:
```
[i] owner/shared-utils@abc1234  (transitive)

    root/pkg   [constraint: ^1.2.0, declared in apm.yml]
    +-- mid/pkg   [constraint: ^2.0.0]
        +-- owner/shared-utils
```

### JSON Output Format

```json
{
  "package": {
    "repo_url": "owner/repo",
    "version": "v1.2.3",
    "source": "git",
    "is_direct": false
  },
  "paths": [
    {
      "chain": [
        {"repo_url": "root/pkg", "constraint": "^1.2.0", "is_direct": true},
        {"repo_url": "mid/pkg", "constraint": "^2.0.0", "is_direct": false},
        {"repo_url": "owner/repo", "constraint": null, "is_direct": false}
      ]
    }
  ]
}
```

### Exit Codes

| Code | Meaning |
|---|---|
| 0 | Target found and explained |
| 1 | Package not installed or query ambiguous |
| 2 | No lockfile / project misconfiguration |

### apm-go Status

Already implemented in `internal/resolver/why.go` with `ComputeWhy()` function that returns `[]WhyPath`.

## 6. `apm update` / `apm deps update`

Source: `commands/update.py`

### CLI Interface

```bash
apm update                         # Interactive: resolve, plan, prompt, install
apm update --dry-run               # Show plan only
apm update --yes                   # Skip prompt (CI-safe)
apm update org/pkg-a org/pkg-b     # Refresh only named packages
apm update -g                      # User-scope
```

### Flags

| Flag | Description |
|---|---|
| `--yes` / `-y` | Skip confirmation prompt |
| `--dry-run` | Render plan and exit |
| `--verbose` / `-v` | Show unchanged deps in plan |
| `--global` / `-g` | User-scope (~/.apm/) |
| `--force` | Overwrite collisions |
| `--parallel-downloads` | Max concurrent downloads |
| `--target` / `-t` | Agent target(s) |

### apm-go Status

Already has `PlanFullUpdate()` and `PlanScopedUpdate()` in `internal/resolver/update.go`.

## 7. A/B Testing Strategy Recommendation

### Minimum Viable A/B Surface

The minimum CLI surface for meaningful A/B testing of the resolver:

1. **`apm-go lock`** (equivalent to `apm lock`):
   - Parse `apm.yml` -> manifest
   - Run resolver (already implemented)
   - Write `apm.lock.yaml` (lockfile serialization already implemented in `internal/lockfile/`)
   - Compare output lockfile against Python's `apm lock` output

2. **`apm-go deps why <package>`**:
   - Already implemented in Go (`ComputeWhy`)
   - Just needs CLI wiring + output formatting

3. **`apm-go update --dry-run`**:
   - Already has `PlanFullUpdate` / `PlanScopedUpdate`
   - Just needs CLI wiring + plan rendering

### What's Missing in apm-go for A/B

| Component | Status | Gap |
|---|---|---|
| Manifest parsing | Done (`internal/manifest/`) | None |
| Lockfile parsing | Done (`internal/lockfile/`) | None |
| Semver resolution | Done (`internal/semver/`) | None |
| BFS resolver | Done (`internal/resolver/`) | None |
| Tag listing (git ls-remote) | **Interface only** (`TagLister`) | Need real git implementation |
| Package loading (git clone + parse sub-manifest) | **Interface only** (`PackageLoader`) | Need real git implementation |
| Lockfile WRITING | **Not checked** | May need serialization |
| CLI commands (`lock`, `deps why`, `update`) | **Not wired** | Need cobra commands |
| `--dry-run` plan rendering | Not implemented | Need text rendering |

### Comparison Methodology

```bash
# Python side
cd project-with-deps
apm lock
cp apm.lock.yaml apm.lock.yaml.python

# Go side
apm-go lock
cp apm.lock.yaml apm.lock.yaml.go

# Compare resolution results (ignoring timing, deployed_files, content_hash)
diff <(yq '.dependencies[] | {repo_url, resolved_commit, resolved_ref, depth, resolved_by}' apm.lock.yaml.python) \
     <(yq '.dependencies[] | {repo_url, resolved_commit, resolved_ref, depth, resolved_by}' apm.lock.yaml.go)
```

The fields that MUST match for A/B correctness:
- `repo_url` (identity)
- `resolved_commit` (exact SHA)
- `resolved_ref` or `resolved_tag` (ref name)
- `depth` (graph depth)
- `resolved_by` (resolution chain)
- `constraint` (if semver)

Fields that CAN differ (deployment-specific):
- `deployed_files`, `deployed_file_hashes` (Go doesn't deploy)
- `content_hash` (requires file tree)
- `generated_at`, `apm_version`

## Caveats / Not Found

- The Python resolver (`deps/apm_resolver.py`) uses parallel BFS with `ThreadPoolExecutor` (configurable via `APM_RESOLVE_PARALLEL` env var). The Go resolver already uses a simpler sequential BFS with fixpoint re-expansion which is architecturally different but should produce equivalent results.
- Python has many post-resolution phases (integration, MCP, LSP, policy, audit) that are irrelevant for A/B testing of the resolver itself.
- The `--dry-run` flag on `apm install` does NOT run the resolver -- it just lists declared deps. For resolver A/B, use `apm lock` or `apm update --dry-run`.
- Local deps (`source: local`, `local_path: ./packages/foo`) bypass the git-based resolver entirely. They are copied directly. A/B testing should focus on git-source deps only.
- The Go `TagLister` and `PackageLoader` interfaces need real implementations (hitting git remotes) before A/B testing against live repos. For unit-level parity, mock implementations with the same fixture data suffice.
