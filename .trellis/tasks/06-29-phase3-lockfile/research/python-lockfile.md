# Research: Python APM Lockfile Implementation

- **Query**: Study the Python apm lockfile code for Go port
- **Scope**: internal
- **Date**: 2026-06-29

## 1. Lockfile Module Location

| File Path | Description |
|---|---|
| `src/apm_cli/deps/lockfile.py` | Core lockfile data model: `LockedDependency`, `LockFile` classes, YAML serialization/deserialization, semantic equivalence |
| `src/apm_cli/install/phases/lockfile.py` | Lockfile assembly phase: `LockfileBuilder` that assembles a LockFile from install artifacts |
| `src/apm_cli/utils/content_hash.py` | SHA-256 hashing: `compute_package_hash()` (tree hash) and `compute_file_hash()` (single file) |
| `src/apm_cli/utils/yaml_io.py` | YAML I/O utilities: `yaml_to_str()`, `load_yaml()`, `dump_yaml()` |
| `src/apm_cli/install/pipeline.py` | Install pipeline orchestrator: calls phases in order |
| `src/apm_cli/install/context.py` | `InstallContext` dataclass shared across pipeline phases |
| `src/apm_cli/install/sources.py` | Dependency source strategies: Local, Cached, Fresh download |
| `src/apm_cli/install/service.py` | `InstallService` with `_enforce_frozen()` for --frozen mode |
| `src/apm_cli/install/plan.py` | `lockfile_satisfies_manifest()` for --frozen structural check |
| `src/apm_cli/install/errors.py` | `FrozenInstallError` exception definition |
| `src/apm_cli/install/integrity.py` | `enforce_require_hashes()` fail-closed gate |
| `src/apm_cli/install/phases/download.py` | Parallel download phase |
| `src/apm_cli/install/phases/integrate.py` | Sequential integration phase |
| `src/apm_cli/deps/github_downloader.py` | Git clone/download via GitPython |
| `src/apm_cli/policy/ci_checks.py` | Baseline CI checks for `apm audit --ci` (NOT frozen install) |
| `docs/src/content/docs/reference/lockfile-spec.md` | Lockfile specification docs |
| `docs/public/specs/schemas/lockfile-v0.1.schema.json` | JSON Schema for lockfile v0.1 |
| `docs/src/content/docs/specs/openapm-v0.1.md` | OpenAPM v0.1 spec (tree_sha256 definition) |

## 2. LockedDependency Serialization to YAML

### to_dict() (lockfile.py:141-210)

Serialization is conditional-emit: each field is only included when it has a truthy value (non-None, non-empty, non-default). The method builds a dict manually:

```python
def to_dict(self) -> dict[str, Any]:
    result: dict[str, Any] = {"repo_url": self.repo_url}
    if self.host:
        result["host"] = self.host
    # ... each field conditionally emitted ...
    if self.deployed_files:
        result["deployed_files"] = sorted(_dedupe_preserving_order(self.deployed_files))
    if self.deployed_file_hashes:
        result["deployed_file_hashes"] = dict(sorted(self.deployed_file_hashes.items()))
    # Unknown fields replayed LAST to never shadow known fields
    for k, v in self._unknown_fields.items():
        result.setdefault(k, v)
    return result
```

Key behaviors:
- `deployed_files` is sorted and deduped
- `deployed_file_hashes` keys are sorted
- `depth` is only emitted when != 1 (default)
- `is_dev`, `is_virtual`, `is_insecure`, `allow_insecure` only emitted when True
- `skill_subset` is sorted
- `_unknown_fields` replayed at end via `setdefault` (forward-compat)

### from_dict() (lockfile.py:213-318)

Deserialization with backward compat:
- Migrates legacy `deployed_skills` to `deployed_files`
- Port validated to 1-65535 range
- `host_type` normalized to lowercase, validated against `_ALLOWED_HOST_TYPES` (currently just `{"gitlab"}`)
- Unknown keys captured in `_unknown_fields` for forward-compat round-trip

### LockFile.to_yaml() (lockfile.py:539-581)

```python
def to_yaml(self) -> str:
    # Re-derive version from content
    self.lockfile_version = "2" if self._needs_v2() else "1"
    # Pop self-entry before serialization (it's synthetic)
    _self_dep = self.dependencies.pop(_SELF_KEY, None)
    try:
        data = {
            "lockfile_version": emit_version,
            "generated_at": self.generated_at,
        }
        if self.apm_version:
            data["apm_version"] = self.apm_version
        data["dependencies"] = [dep.to_dict() for dep in self.get_all_dependencies()]
        # mcp_servers, mcp_configs, lsp_servers, lsp_configs conditionally
        # local_deployed_files, local_deployed_file_hashes conditionally
        return yaml_to_str(data)
    finally:
        if _self_dep is not None:
            self.dependencies[_SELF_KEY] = _self_dep
```

`yaml_to_str` uses `yaml.safe_dump` with `default_flow_style=False, sort_keys=False, allow_unicode=True`.

### LockFile.from_yaml() (lockfile.py:583-617)

Deserialization:
- Reads all known top-level fields
- Iterates `dependencies` list, calling `LockedDependency.from_dict()` + `lock.add_dependency()`
- If `local_deployed_files` is non-empty, synthesizes a virtual self-entry keyed by `"."` with `repo_url="<self>"`, `source="local"`, `local_path="."`, `depth=0`, `is_dev=True`

## 3. deployed_file_hashes Computation

### compute_file_hash() (content_hash.py:73-93)

Single file SHA-256, format `"sha256:<hex>"`:

```python
def compute_file_hash(file_path: Path) -> str:
    if not file_path.is_file() or file_path.is_symlink():
        return _EMPTY_HASH  # "sha256:" + sha256(b"").hexdigest()
    hasher = hashlib.sha256()
    hasher.update(file_path.read_bytes())
    return f"sha256:{hasher.hexdigest()}"
```

### compute_deployed_hashes() (install/phases/lockfile.py:29-47)

Called for each dependency to hash its deployed files:

```python
def compute_deployed_hashes(rel_paths, project_root: Path) -> dict:
    out = {}
    for _rel in rel_paths or ():
        _full = project_root / _rel
        if _full.is_file() and not _full.is_symlink():
            try:
                out[_rel] = compute_file_hash(_full)
            except Exception:
                pass
    return out
```

Returns `{rel_path: "sha256:<hex>"}`. Symlinks and unreadable paths silently omitted. Directory entries (trailing `/`) naturally skipped since `is_file()` returns False for directories.

### Where called

In `LockfileBuilder._attach_deployed_files()` (lockfile.py:138-166): iterates each dependency in the new lockfile, calls `compute_deployed_hashes(current_files, project_root)`, then unions with prior lockfile hashes via `union_preserving()` for target-scoped reconciliation.

## 4. content_hash (Package Tree Hash) Computation

### compute_package_hash() (content_hash.py:24-70)

This is the **content_hash** field. It is a deterministic walk-and-hash of the entire package directory:

```python
def compute_package_hash(package_path: Path) -> str:
    hasher = hashlib.sha256()
    # Collect all regular files, skip .git, __pycache__, symlinks
    regular_files = []
    for item in package_path.rglob("*"):
        if item.is_symlink(): continue
        rel = item.relative_to(package_path)
        if any(part in {".git", "__pycache__"} for part in rel.parts): continue
        if item.is_file():
            if len(rel.parts) == 1 and rel.name in {".apm-pin"}: continue
            regular_files.append(rel)
    # Sort lexicographically by POSIX path for determinism
    regular_files.sort(key=lambda p: p.as_posix())
    for rel_path in regular_files:
        hasher.update(rel_path.as_posix().encode("utf-8"))
        hasher.update((package_path / rel_path).read_bytes())
    return f"sha256:{hasher.hexdigest()}"
```

Algorithm:
1. Walk the package directory recursively
2. Exclude `.git`, `__pycache__` dirs and symlinks
3. Exclude `.apm-pin` marker at root level only
4. Sort files by POSIX path lexicographically
5. For each file: feed `relative_path_posix_utf8 + file_bytes` into SHA-256
6. Return `"sha256:<hex>"`

### tree_sha256 (OpenAPM v0.1 spec, sec 5.6.4)

The spec defines a SEPARATE field `tree_sha256` using a canonical git tree hash format:

```
<line>           ::= <mode-octal> SP <name-utf8> SP <blob-sha256-hex> LF
<canonical-tree> ::= <line>*   (entries sorted lexicographically by name)
```

The Python implementation does NOT compute or emit `tree_sha256`. The string `tree_sha256` does not appear anywhere in `src/`. It is defined in the JSON Schema and in the OpenAPM v0.1 spec doc, and appears in spec-conformance test fixtures where it is round-tripped as an unknown field.

### Where content_hash is called

In all three DependencySource strategies:
- `LocalDependencySource.acquire()`: `ctx.package_hashes[dep_key] = _compute_hash(install_path)`
- `CachedDependencySource.acquire()`: same pattern
- `FreshDependencySource.acquire()`: same pattern, plus supply-chain mismatch check

## 5. Version Field ("1" vs "2", Monotonicity)

### Version bumping (lockfile.py:497-504, 522-537, 539-547)

The lockfile uses **opportunistic** version bumping:

```python
def _needs_v2(self) -> bool:
    for d in self.dependencies.values():
        if d.source == "registry":
            return True
        if d.constraint or d.resolved_tag or d.resolved_at:
            return True
    return False

def to_yaml(self) -> str:
    self.lockfile_version = "2" if self._needs_v2() else "1"
    # ...
```

- `"1"`: default for git-only projects
- `"2"`: any dependency with `source == "registry"` OR any git-source semver resolution fields (`constraint`, `resolved_tag`, `resolved_at`)
- **Bidirectional**: if every registry/semver dep is removed, the next write demotes back to `"1"`
- `add_dependency()` also eagerly bumps to `"2"` when adding a qualifying dep

**Python vs Spec divergence**: The Python implementation demotes v2 back to v1 when the triggering deps are removed. The OpenAPM v0.1 spec (sec 7.10 example) states: "Once written as '2', this lockfile MUST NOT be demoted to '1' on subsequent rewrites." This is a decision point for the Go port -- see Caveats.

## 6. Frozen Install Mode

### --frozen flag (install.py:913-916)

CLI option: `--frozen` is a boolean flag, mutually exclusive with `--update`.

### CI env detection

Verified: there is NO automatic frozen mode activation based on CI environment variables. The `--frozen` flag is always explicit. `policy/ci_checks.py` handles `apm audit --ci` checks (a separate command), not frozen install auto-detection. No code in `install/service.py` or `commands/install.py` reads `CI`, `GITHUB_ACTIONS`, `TF_BUILD`, or similar env vars to auto-enable frozen mode.

### Enforcement (service.py:100-148)

`InstallService._enforce_frozen()` runs BEFORE the pipeline (before any resolve/download work):

1. Find `apm.lock.yaml` next to `apm.yml`
2. If missing: raise `FrozenInstallError("--frozen requires apm.lock.yaml to exist")`
3. If unreadable: raise `FrozenInstallError`
4. Call `lockfile_satisfies_manifest(lockfile, manifest_deps)`

### Structural check (plan.py:382-416)

```python
def lockfile_satisfies_manifest(lockfile, manifest_deps):
    locked_keys = {key for key in lockfile.dependencies if key != _SELF_KEY}
    reasons = []
    for dep in manifest_deps:
        if getattr(dep, "is_local", False):
            continue  # local deps skipped
        key = _dep_ref_key(dep)
        if key not in locked_keys:
            reasons.append(f"  - {key} is declared in apm.yml but missing from apm.lock.yaml")
    return (not reasons, reasons)
```

Key points:
- Only checks direct deps from manifest (regular + dev)
- Local deps are skipped
- Only checks that each manifest dep has an entry in the lockfile (structural presence)
- Does NOT compare resolved refs or verify content integrity
- Transitive dep drift and removed deps are allowed
- Mirrors how `uv` treats `--frozen` and how `npm ci` enforces direct-dep presence

### After install (install.py:1581-1590)

Post-install hint: "Lockfile presence verified. Run 'apm audit' for on-disk content integrity."

## 7. No-op Install Detection

### is_semantically_equivalent() (lockfile.py:735-763)

Used by `LockfileBuilder._write_if_changed()` to skip lockfile rewrite:

```python
def is_semantically_equivalent(self, other: LockFile) -> bool:
    # Ignores generated_at and apm_version
    if self.lockfile_version != other.lockfile_version:
        return False
    if set(self.dependencies.keys()) != set(other.dependencies.keys()):
        return False
    for key, dep in self.dependencies.items():
        other_dep = other.dependencies[key]
        if dep.to_dict() != other_dep.to_dict():
            return False
    # Also compare mcp_servers, mcp_configs, lsp_servers, lsp_configs
    # And local_deployed_files, local_deployed_file_hashes
    return True
```

### _write_if_changed() (lockfile.py phases:284-298)

```python
def _write_if_changed(self, lockfile, lockfile_path, _LF):
    existing_lockfile = _LF.read(lockfile_path) if lockfile_path.exists() else None
    if existing_lockfile and lockfile.is_semantically_equivalent(existing_lockfile):
        # Skip write -- "apm.lock.yaml unchanged"
    else:
        lockfile.save(lockfile_path)
```

### _is_no_work_install() (pipeline.py:376-394)

Early exit before the pipeline even starts:
- If no apm deps AND no root primitives AND no old local deployed files AND no orphan deps
- In lockfile_only mode (`apm lock`), writes an empty lockfile before returning

## 8. x-* Vendor Extension Preservation

### Per-entry level (lockfile.py:107-111, 206-209, 246-282)

Per-entry vendor extensions are handled via `_unknown_fields`:

```python
# from_dict():
_known_keys = {"repo_url", "host", ...all known fields...}
unknown_fields = {k: v for k, v in data.items() if k not in _known_keys}
# Stored in _unknown_fields dataclass field

# to_dict():
for k, v in self._unknown_fields.items():
    result.setdefault(k, v)  # Replay LAST so they never shadow known fields
```

This means `x-acme-attestation-id`, `tree_sha256`, and any other unknown per-entry fields are preserved through a round-trip.

### Top-level lockfile x-* fields

The current Python implementation does NOT preserve top-level `x-*` keys. The `LockFile.from_yaml()` method only reads known top-level keys (`lockfile_version`, `generated_at`, `apm_version`, `dependencies`, `mcp_servers`, etc.) and ignores everything else. Top-level `x-acme-build-id` would be lost on round-trip.

The JSON Schema allows them (`"patternProperties": {"^x-[a-z][a-z0-9-]*$": {}}`), and the OpenAPM v0.1 spec requires preservation, but the Python implementation doesn't preserve them. This is another Python-vs-spec divergence.

### Schema definition

```json
{
  "patternProperties": {
    "^x-[a-z][a-z0-9-]*$": {}
  },
  "additionalProperties": true
}
```

Both top-level and per-entry schemas permit `x-*` keys and additional properties.

## 9. Hash/Digest Envelope Format

All hashes use the envelope format `"<algo>:<hex>"`:

- **Format**: `sha256:<64_hex_chars>`
- **Pattern** (from JSON Schema): `^(sha256:[0-9a-f]{64}|sha384:[0-9a-f]{96}|sha512:[0-9a-f]{128}|[0-9a-f]{64})$`
- **Bare hex**: Readers MUST accept bare 64-char hex as `sha256:<hex>` for v0.1 backward-compat; writers MUST emit the `sha256:` prefix
- **Empty hash**: `"sha256:" + sha256(b"").hexdigest()` = `"sha256:e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855"`

Hash fields in the lockfile:
- `deployed_file_hashes` (per-entry): `{path: "sha256:<hex>"}`
- `local_deployed_file_hashes` (top-level): same format
- `content_hash` (per-entry): `"sha256:<hex>"`
- `resolved_hash` (per-entry, registry only): `"sha256:<hex>"`
- `source_digest` (per-entry, marketplace): `"sha256:<hex>"`
- `tree_sha256` (OpenAPM spec, NOT yet implemented in Python): `"sha256:<hex>"`

## 10. Install Command End-to-End (After Resolution)

### Pipeline phases (pipeline.py:397-934)

The install pipeline after resolution:

```
1. resolve       -- dependency resolution + lockfile check
2. policy_gate   -- org-policy enforcement
3. targets       -- target detection + integrator initialization (skipped in lockfile_only)
4. policy_target_check -- target-aware policy check
5. auth preflight -- (--update mode only)
6. download      -- parallel package pre-download (ThreadPoolExecutor)
7. integrate     -- sequential integration loop + root primitives
8. cleanup       -- orphan cleanup + intra-package stale-file removal (skipped in lockfile_only)
9. skill_path_migration -- legacy skill path migration (skipped in lockfile_only)
10. lockfile     -- LockfileBuilder.build_and_save()
11. require_hashes gate -- fail-closed if policy requires hashes
12. post_deps_local -- local .apm/ content (skipped in lockfile_only)
13. audit        -- optional install-time content audit (skipped in lockfile_only)
14. finalize     -- emit stats, return InstallResult
```

### Download phase (phases/download.py)

- Parallel downloads via `ThreadPoolExecutor`
- Skips: local packages, BFS callback-resolved packages, registry-sourced packages, lockfile SHA match
- Content-hash verification fallback when git check fails (`.git` removed after download)
- `build_download_ref()` uses locked commit for reproducibility, manifest ref when ref_changed

### Integration phase (phases/integrate.py)

- Three source strategies via factory `make_dependency_source()`:
  - `LocalDependencySource`: copies from filesystem, computes `content_hash`
  - `CachedDependencySource`: reuses existing apm_modules, resolves commit SHA from lockfile
  - `FreshDependencySource`: downloads from git/registry, verifies supply-chain hash
- Each source `acquire()` returns a `Materialization` consumed by `run_integration_template()`
- Template handles: security gate, primitive integration, diagnostics

### Supply-chain hash verification (sources.py:789-821)

After a fresh download, if the lockfile already records a `content_hash` for the dep:

```python
if (not ctx.update_refs
    and dep_key not in _expected_hash_deps
    and dep_locked_chk and dep_locked_chk.content_hash
    and dep_key in ctx.package_hashes):
    _fresh_hash = ctx.package_hashes[dep_key]
    if _fresh_hash != dep_locked_chk.content_hash:
        safe_rmtree(install_path, ctx.apm_modules_dir)
        _rich_error("Content hash mismatch ... supply-chain attack ...")
        sys.exit(1)
```

### Lockfile phase (phases/lockfile.py)

`LockfileBuilder.build_and_save()` orchestrates:

1. `LockFile.from_installed_packages()` -- creates lockfile from `ctx.installed_packages`
2. `_attach_deployed_files()` -- per-dep deployed-file manifests with target-scoped union
3. `_attach_package_types()` -- package_type per dep
4. `_attach_skill_subset_override()` -- CLI --skill override
5. `_attach_content_hashes()` -- content_hash per dep from `ctx.package_hashes`
6. `_attach_declared_licenses()` -- SPDX license provenance
7. `_attach_marketplace_provenance()` -- marketplace metadata
8. `_merge_existing()` -- merge old lockfile entries for partial installs or failed downloads
9. `_maybe_merge_partial()` -- for `apm install <pkg>` subset installs
10. `_preserve_existing_mcp_state()` -- carry forward MCP config
11. `_preserve_existing_local_state()` -- carry forward local content
12. `_preserve_existing_revision_pin_tags()` -- carry resolved_tag for unchanged deps
13. `_write_if_changed()` -- semantic equivalence check, only write if changed
14. `_sync_cache_pin_markers()` -- write `.apm-pin` markers for supply-chain audit

### Git clone/download (deps/github_downloader.py)

Uses `GitPython` (`git` package) for:
- `git clone` with fallback strategies (bare clone + materialize)
- Transport selection (HTTPS vs SSH)
- Auth integration via `AuthResolver`
- Reference resolution via `git ls-remote`

## Caveats / Open Decisions for Go Port

### Python-vs-Spec divergences (decision: spec-conformance vs Python-parity?)

Three areas where the Python implementation diverges from the OpenAPM v0.1 spec. The Go port needs to decide whether to match the spec or match the Python behavior:

1. **tree_sha256**: The OpenAPM v0.1 spec (req-lk-015) requires computing and recording `tree_sha256` for every git-sourced lockfile entry. The Python implementation does NOT compute or emit it; it only preserves the field as an unknown key via `_unknown_fields` round-trip. Python uses `content_hash` (a simpler walk-sort-hash) instead.

2. **Version monotonicity**: The OpenAPM v0.1 spec (sec 7.10) says once a lockfile is written as `"2"`, it "MUST NOT be demoted to '1'". The Python implementation demotes v2 back to v1 when all registry/semver deps are removed (bidirectional derivation in `_needs_v2()`).

3. **Top-level x-* preservation**: The OpenAPM v0.1 spec (req-ext-001) requires preserving vendor-extension keys at every mapping level on round-trip. The Python `LockFile.from_yaml()` drops unknown top-level keys; only per-entry unknown fields are preserved via `_unknown_fields`.

### YAML output byte-equivalence risk

PyYAML emits long map keys using YAML's explicit `? key\n: value` syntax (visible in the real `apm.lock.yaml` at lines ~476-526 for long `deployed_file_hashes` key paths). Most Go YAML libraries (e.g. `gopkg.in/yaml.v3`) will NOT replicate this form -- they use the inline `key: value` form. Given the spec's req-ext-001 byte-equivalent round-trip requirement and req-lk-005's cross-implementation stable-diff requirement, this is a concrete divergence risk. The Go port should test YAML output against real lockfiles from the Python implementation to identify formatting differences.

### Other notes

- **Self-entry (".")**: Synthetic in-memory entry for the project itself. NOT written to YAML. Created on load from `local_deployed_files`/`local_deployed_file_hashes`.
- **Lockfile filename**: `apm.lock.yaml` (current), with migration from legacy `apm.lock`.
- **Semantic equivalence**: Ignores `generated_at` and `apm_version` when comparing. Compares dependency dicts, MCP/LSP state, local deployed state.
- **content_hash algorithm**: Feeds `path_posix_utf8 + file_bytes` sequentially into a single SHA-256 hasher (no separator between path and content, no length prefix). This is distinct from the spec's `tree_sha256` which uses mode/name/blob-sha256 lines.
