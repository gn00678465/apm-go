# Quality Guidelines

> Code quality standards for backend development.

---

## Overview

<!--
Document your project's quality standards here.

Questions to answer:
- What patterns are forbidden?
- What linting rules do you enforce?
- What are your testing requirements?
- What code review standards apply?
-->

(To be filled by the team)

---

## Forbidden Patterns

<!-- Patterns that should never be used and why -->

(To be filled by the team)

---

## Required Patterns

<!-- Patterns that must always be used -->

(To be filled by the team)

---

## Testing Requirements

<!-- What level of testing is expected -->

(To be filled by the team)

---

## Code Review Checklist

<!-- What reviewers should check -->

(To be filled by the team)

---

## Scenario: MCP Config Writers

### 1. Scope / Trigger
- Trigger: writing merged MCP config files under deploy targets (`.mcp.json`, `.codex/config.toml`, `.github/mcp-config.json`, `.agents/mcp_config.json`).
- Applies to `internal/deploy/mcp_*.go`, `internal/deploy/mcp_common.go`, and install wiring that records merged MCP files in the lockfile.

### 2. Signatures
- `writeMergedMCPJSON(path, topKey string, entries map[string]map[string]any, considered map[string]bool, perm os.FileMode) error`
- `writeMergedMCPTOML(path, topKey string, entries map[string]map[string]any, considered map[string]bool, perm os.FileMode) error`
- `writeFileWithPerm(path string, data []byte, perm os.FileMode) error`
- `buildMCPEntries(prims []Primitive, mode manifest.ResolveMode, build mcpEntryBuilder) (map[string]map[string]any, []string)`

### 3. Contracts
- Bake-mode MCP configs can contain resolved secrets and must be written with `0600` on every deploy, including redeploy over an existing looser file.
- MCP config writers must route through `writeFileWithPerm`; direct `os.WriteFile` is only acceptable for tests or non-MCP generic file copying.
- Translate-mode remote URLs may bypass the HTTPS guard only when the URL starts with a runtime placeholder, because only then is the final scheme unknown.
- Literal `http://` remote URLs remain refused even if a later path/query segment contains a placeholder.
- Pure local/MCP-only installs with an active target must still run deploy and lockfile writing with an empty resolver result.

### 4. Validation & Error Matrix
- Existing bake config mode `0644` -> redeploy must finish at `0600`.
- Existing malformed MCP config -> return error and do not overwrite.
- Translate URL `${input:mcp-url}` -> write URL verbatim for Copilot.
- Translate URL `http://host/mcp` -> skip with non-HTTPS diagnostic.
- Translate URL `http://host/${input:path}` -> skip with non-HTTPS diagnostic.
- No `dependencies.apm` and no active target -> print "No dependencies to install" and return.
- No `dependencies.apm` with active target -> deploy local/MCP primitives and write local deployed hashes.

### 5. Good/Base/Bad Cases
- Good: `writeMergedMCPJSON(..., 0600)` -> `writeFileWithPerm` pre/post chmods and writes.
- Base: Copilot URL `${input:mcp-url}` survives to `.github/mcp-config.json`.
- Bad: using `manifest.HasPlaceholder(r.URL)` alone to bypass HTTPS checks, because `http://host/${input:path}` has a known insecure scheme.

### 6. Tests Required
- POSIX-only permission regression for redeploy over an existing `0644` MCP file.
- Copilot translate placeholder URL regression covering `http`, `sse`, and `streamable-http`.
- Copilot literal HTTP regressions, including placeholder in path.
- MCP-only `runInstall` E2E that deploys and records `local_deployed_file_hashes` without a dummy `dependencies.apm` entry.

### 7. Wrong vs Correct
#### Wrong
```go
if mode == manifest.ResolveTranslate && manifest.HasPlaceholder(r.URL) {
    // bypass HTTPS guard
}
return os.WriteFile(path, data, 0600)
```

#### Correct
```go
deferredToRuntime := remoteURLDeferredToRuntime(mode, r.URL)
if r.Transport != "stdio" && !deferredToRuntime && !strings.HasPrefix(r.URL, "https://") {
    // skip
}
return writeFileWithPerm(path, data, 0600)
```

---

## Scenario: Lockfile Top-Level Local-Deployed Fields

### 1. Scope / Trigger
- Trigger: any field added to `Lockfile` that is parsed from a top-level `apm.lock.yaml` key (not per-dependency). `local_deployed_files` / `local_deployed_file_hashes` are the existing example; a new top-level field must repeat this same pattern on both the read and write side, or it will silently round-trip as empty.
- Applies to `internal/lockfile/parse.go` (`ParseLockfile`), `internal/lockfile/write.go` (`SerializeLockfile`, `IsSemanticEqual`).

### 2. Signatures
- `SerializeLockfile(lf *Lockfile, original *yaml.Node) (*yaml.Node, error)`
- `IsSemanticEqual(a, b *Lockfile) bool`
- `knownTopKeys map[string]bool` (in `SerializeLockfile`) must list every field `ParseLockfile` reads from the top-level mapping, or that field's on-disk value passes through as an opaque "unknown key" forever instead of being serialized from the `Lockfile` struct.

### 3. Contracts
- A field that `ParseLockfile` reads (`lf.<Field>`) must have a matching write in `SerializeLockfile` — parse-without-write is a silent data-loss bug: the value is correctly read into the struct in memory, but the next write drops it, and a caller relying on it round-tripping to disk (e.g. verifying a hash after a second install) will observe stale or missing data instead of an error.
- A field is either **advisory** (excluded from `IsSemanticEqual`, e.g. `generated_at`/`apm_version`/`resolved_at` — req-lk-005) or **content** (must be compared in `IsSemanticEqual`). Default to content: an omission here makes `IsSemanticEqual` return a false "equal", so a real change is silently skipped as a no-op and the lockfile is never rewritten.
- `local_deployed_files`/`local_deployed_file_hashes` are content, not advisory (they are hashes of what is actually on disk after deploy) — compared via the existing `slicesEqual`/`mapsEqual` helpers, same as the per-dependency `DeployedFiles`/`DeployedHashes` fields already were.
- New top-level list/map fields should reuse the existing preserve-if-unchanged pattern (`deployedFilesMatch`/`deployedHashesMatch` against `buildOriginalTopPairs`) rather than always emitting a fresh node, to avoid needless YAML formatting churn on unrelated writes.

### 4. Validation & Error Matrix
- Field read by `ParseLockfile` but absent from `SerializeLockfile`'s emit logic -> value present in memory, silently dropped on next write (bug class this scenario guards against).
- Field present in both parse and write, but absent from `IsSemanticEqual` -> a content-only change to that field is treated as a no-op; `apm.lock.yaml` is not rewritten even though on-disk state changed.
- Field intentionally advisory (e.g. a new timestamp) -> must be excluded from `IsSemanticEqual` explicitly, not just omitted by oversight; state the reason in a comment (see `IsSemanticEqual`'s doc comment listing all advisory fields).

### 5. Good/Base/Bad Cases
- Good: add field to `Lockfile` struct -> add `case` in `ParseLockfile` -> add emit block in `SerializeLockfile` + add to `knownTopKeys` -> decide advisory vs content and update `IsSemanticEqual` accordingly -> add a round-trip test and (if content) an `IsSemanticEqual` regression test.
- Base: a purely advisory field (like `generated_at`) needs parse + write, but is deliberately left out of `IsSemanticEqual` — this is correct, not an omission, as long as it's one of the documented advisory fields.
- Bad: a field added to `ParseLockfile` and left off `SerializeLockfile`'s `knownTopKeys` — it round-trips as an "unknown key" copied verbatim from the original file, meaning it never reflects an in-memory update, and disappears entirely on a from-scratch (`original == nil`) write.

### 6. Tests Required
- `TestIsSemanticEqual_<Field>Differs`: two `Lockfile` values differing only in the new field must NOT compare equal (unless deliberately advisory).
- Round-trip test: parse -> serialize -> parse again, value unchanged.
- If content and mutable via a code path outside tests (e.g. install.go), an E2E-level regression that changes only that field between two `runInstall()` calls and asserts the lockfile is rewritten with the new value (see `TestRunInstall_MCP_OnlyChangeStillRewritesLockfile` in `cmd/apm/mcp_e2e_test.go` for the pattern).

### 7. Wrong vs Correct
#### Wrong
```go
// parse.go: field is read into the struct...
case "local_deployed_files":
    lf.LocalDeployedFiles = append(lf.LocalDeployedFiles, item.Value)

// write.go: ...but SerializeLockfile never emits it, and IsSemanticEqual
// never compares it. The value silently vanishes on the next write, and a
// content-only change to it is treated as a no-op.
func IsSemanticEqual(a, b *Lockfile) bool {
    if a.Version != b.Version { return false }
    // (no LocalDeployedFiles/LocalDeployedHashes comparison)
    if len(a.Dependencies) != len(b.Dependencies) { return false }
    ...
}
```

#### Correct
```go
func IsSemanticEqual(a, b *Lockfile) bool {
    if a.Version != b.Version { return false }
    if !slicesEqual(a.LocalDeployedFiles, b.LocalDeployedFiles) { return false }
    if !mapsEqual(a.LocalDeployedHashes, b.LocalDeployedHashes) { return false }
    if len(a.Dependencies) != len(b.Dependencies) { return false }
    ...
}
// and SerializeLockfile emits local_deployed_files/local_deployed_file_hashes
// (preserve-if-unchanged pattern) + lists both in knownTopKeys.
```
