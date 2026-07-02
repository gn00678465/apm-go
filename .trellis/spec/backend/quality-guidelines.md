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

---

## Scenario: Git Checkout Skip-Download Safety (req-lk-007)

### 1. Scope / Trigger
- Trigger: deciding whether an existing `apm_modules/<key>` checkout can be trusted as-is instead of re-cloning. Applies to `internal/gitops/clone.go` (`RealPackageLoader.LoadPackage`, `checkoutMatchesRef`, `cloneRepo`).
- Any caller of `LoadPackage(ref, resolvedRef)` (install, update, or future commands) inherits this behavior automatically — the safety check lives inside `LoadPackage` itself, not in each caller.

### 2. Signatures
- `checkoutMatchesRef(installDir, resolvedRef string) bool`
- `resolveRefLocally(repoDir, ref string) (string, error)` — `git rev-parse <ref>^{commit}`
- `worktreeClean(repoDir string) bool` — `git status --porcelain --ignored`
- `isCommitSHA(ref string) bool` — 40-hex check
- `(r *RealPackageLoader) cloneRepoAtCommit(url, dir, commit string) error`

### 3. Contracts
- `resolvedRef` passed into `LoadPackage` may be a tag, a branch name, or a raw 40-hex commit SHA — callers must not assume any single shape. For git-semver deps it is usually a tag name (`currentPin`); for git-literal deps it can be any of the three; frozen install intentionally passes `dep.ResolvedCommit` (a SHA) when available, preferring the authoritative pin over `dep.ResolvedRef`.
- Skipping a re-clone requires ALL of: checkout exists, `resolveRefLocally` succeeds and equals current HEAD, and `worktreeClean` is true (no uncommitted, untracked, OR ignored changes — a fresh clone never contains an ignored file, so one being present means this checkout already diverges from what a fresh clone would produce).
- `resolveRefLocally` uses `^{commit}` peeling — an annotated tag's bare `rev-parse` resolves to the tag OBJECT's own SHA, not the commit it points at, which would otherwise make every annotated-tag checkout report a false mismatch (safe but defeats the optimization, not a correctness bug on its own).
- Any local git command failure (ref not found, not a git repo, etc.) is treated as a mismatch — fail-safe, not fail-open.
- `cloneRepo` MUST NOT pass a raw commit SHA as `git clone --branch <ref>` — standard shallow clone only accepts branch/tag names for `--branch`; a raw SHA fails with `fatal: Remote branch <sha> not found in upstream origin` (exit 128), confirmed via direct reproduction. `isCommitSHA(ref)` routes SHA-shaped refs to `cloneRepoAtCommit` (full clone + explicit `git checkout <sha>`) instead.

### 4. Validation & Error Matrix
- Checkout HEAD == locally-resolved `resolvedRef`, worktree clean -> skip clone.
- Checkout HEAD mismatch, or `resolvedRef` not resolvable locally, or dirty/untracked/ignored files present -> `os.RemoveAll` the checkout, re-clone.
- `resolvedRef` is a raw 40-hex SHA and no existing checkout -> `cloneRepoAtCommit` (full clone, then `git checkout <sha>`), never `--branch <sha>`.
- `resolvedRef` is a tag/branch name and no existing checkout -> normal `git clone --depth 1 --branch <ref>`.

### 5. Good/Base/Bad Cases
- Good: frozen install re-downloads a git-semver dep whose `resolved_commit` is a SHA, from an empty `apm_modules` — routes through `cloneRepoAtCommit`, succeeds.
- Base: a clean checkout already at the pinned tag with no local changes skips the clone entirely.
- Bad: passing a raw SHA straight to `git clone --branch <sha>` (the original bug) — fails outright for any first-time/from-scratch frozen install of a dependency resolved to a SHA.

### 6. Tests Required
- `internal/gitops/clone_test.go`: `checkoutMatchesRef` true/false cases (tag, annotated tag, dirty worktree, untracked file, ignored file, ref not found, non-repo, empty ref); `LoadPackage` skip/re-clone/from-scratch cases; `TestLoadPackage_ClonesByRawCommitSHA` (the specific regression — a from-scratch clone with a SHA-shaped `resolvedRef` must succeed, not attempt `--branch <sha>`); `TestIsCommitSHA` boundary cases.
- Any new caller of `LoadPackage` with a SHA-shaped `resolvedRef` and an empty `apm_modules` should get equivalent coverage — this exact combination is what the original bug needed to be caught.

### 7. Wrong vs Correct
#### Wrong
```go
func (r *RealPackageLoader) cloneRepo(url, dir, ref string) error {
    args := []string{"clone", "--depth", "1"}
    if ref != "" {
        args = append(args, "--branch", ref) // fails if ref is a raw commit SHA
    }
    ...
}
```

#### Correct
```go
func (r *RealPackageLoader) cloneRepo(url, dir, ref string) error {
    if isCommitSHA(ref) {
        return r.cloneRepoAtCommit(url, dir, ref) // full clone + git checkout <sha>
    }
    args := []string{"clone", "--depth", "1"}
    if ref != "" {
        args = append(args, "--branch", ref)
    }
    ...
}
```

**Known limitation (documented, not a bug to fix reflexively)**: `checkoutMatchesRef` resolves refs locally with no network call, so a git-literal dependency pinned to a MUTABLE branch name (not a tag/SHA) can report a false "match" if the remote branch has moved since the last fetch. This is the inherent tradeoff of an offline-only optimization for req-lk-007 (a SHOULD, not a MUST) — closing it would require a network round-trip on every skip decision, which defeats the point of skipping. Don't "fix" this without discussing the tradeoff first.

---

## Scenario: Path-Traversal Guard for `apm_modules`-Relative Filesystem Operations

### 1. Scope / Trigger
- Trigger: any code that joins a manifest-derived field (`DependencyReference.RepoURL`, `.VirtualPath`) into a filesystem path used for a destructive operation (`os.RemoveAll`, extraction, etc.) under `apm_modules`.
- Applies today to `cmd/apm/update.go` (`directGitSemverUpdateScope` + the req-lk-010 purge loop in `runUpdate`); applies to any future call site that does the same kind of join.

### 2. Signatures
- `keyHasParentSegment(key string) bool` (`cmd/apm/update.go`)
- `archive.Contained(root, target string) bool` (`internal/archive/extract.go`, pre-existing, reused)

### 3. Contracts
- `manifest.DependencyReference.RepoURL`/`.VirtualPath` are only charset-validated at parse time (`^[A-Za-z0-9._-]+$`), which does **not** reject a `".."` segment — the charset itself allows `.`. This is unlike local-path deps (`containsEscape` explicitly rejects `..`) and unlike `lockfile.LockedDep.VirtualPath` (`internal/lockfile/parse.go`'s `validatePathComponent` explicitly rejects `..`). Any code consuming the manifest-side fields for a filesystem path cannot assume they're traversal-safe.
- A single `archive.Contained(root, target)` check performed AFTER `filepath.Join` is **not sufficient by itself**: `..` can resolve to a location that is still technically "inside" `root` but is the wrong directory entirely (e.g. `path: ".."` on a 2-segment `RepoURL` like `acme/a` cleans to `apm_modules/acme` — a sibling owner namespace, not the intended dependency's own directory). `Contained` only rejects paths that end up OUTSIDE the root; it cannot detect "inside the root but not the intended entry."
- The correct guard is two layers, in this order: (1) `keyHasParentSegment` rejects any key containing a literal `".."` path segment BEFORE `filepath.Join`/`Clean` gets to resolve it away, closing the in-root-wrong-location case; (2) `archive.Contained` as a second-layer catch-all for anything else that still manages to land outside the root (e.g. an absolute path string).

### 4. Validation & Error Matrix
- Key contains a `..` segment (regardless of where it resolves to) -> reject before any path join, error mentions `apm_modules` and the `..` segment.
- Key resolves (after join) to outside `apm_modules` -> reject via `archive.Contained`, error mentions `apm_modules`.
- Key is a normal `owner/repo` or `owner/repo/virtual/path` with no `..` -> proceed with `os.RemoveAll`.

### 5. Good/Base/Bad Cases
- Good: `path: "../../../evil"` on `acme/a` (escapes `apm_modules` entirely) -> rejected by `keyHasParentSegment` before `Contained` is even reached.
- Base: `path: ".."` on `acme/a` (resolves to `apm_modules/acme`, still nominally "inside" `apm_modules`) -> rejected by `keyHasParentSegment`; `Contained` alone would have wrongly allowed this one through.
- Bad: relying on `archive.Contained` alone after `filepath.Join` — catches the outside-root escape but misses the in-root-wrong-directory case entirely (found in a second codex review round, after the first round's `Contained`-only fix was already merged).

### 6. Tests Required
- `cmd/apm/update_test.go`: `TestRunUpdate_RefusesVirtualPathEscapingApmModules` (outside-root case, with a canary file outside `apm_modules` that must survive) and `TestRunUpdate_RefusesParentSegmentStayingInsideApmModules` (in-root-wrong-location case, with a sibling package's marker file that must survive an over-broad `RemoveAll`).
- Any new call site doing the same join needs both test shapes, not just the outside-root one.

### 7. Wrong vs Correct
#### Wrong
```go
installDir := filepath.Join("apm_modules", key)
if !archive.Contained("apm_modules", installDir) {
    return fmt.Errorf("refusing to clear %q outside apm_modules", key)
}
os.RemoveAll(installDir) // "path: .." on "acme/a" still gets through: cleans to
                          // apm_modules/acme, which Contained reports as "inside"
```

#### Correct
```go
if keyHasParentSegment(key) {
    return fmt.Errorf("refusing to clear %q outside apm_modules: contains \"..\" path segment", key)
}
installDir := filepath.Join("apm_modules", key)
if !archive.Contained("apm_modules", installDir) {
    return fmt.Errorf("refusing to clear %q outside apm_modules", key)
}
os.RemoveAll(installDir)
```

---

## Pattern: Shared `buildLockfile` / `deployAndFinalize` Tail for CLI Commands

### Problem
`apm install` and `apm update` both need the same sequence after dependency resolution produces a `*resolver.ResolutionResult`: turn it into a `*lockfile.Lockfile`, deploy primitives to targets, check for a no-op, write `apm.lock.yaml`, and (for `install`) persist positional packages to `apm.yml`. Duplicating this ~180-line sequence per command would drift out of sync (e.g. a fix to tree_sha256 computation in one copy but not the other).

### Solution
`cmd/apm/install.go` exposes two shared functions that any command with this shape can call after its own resolution step:
- `buildLockfile(result *resolver.ResolutionResult, existingLock *lockfile.Lockfile, regLoader *registry.Loader, skillSubset []string, requestedKeys map[string]bool, noProvenance bool) (*lockfile.Lockfile, error)` — pure, no disk I/O; returns a fully-formed lockfile (header + per-dep fields, including tree_sha256/resolved_commit computation against `apm_modules`). `requestedKeys` scopes `skillSubset` to only the dependency key(s) this call's positional packages resolved to (req-pr-001/req-tg-003 fix: `skill_subset` must not be stamped onto unrelated, already-declared dependencies) — pass `nil` when there is no `--skill` filter for this call (e.g. `runUpdate`).
- `deployAndFinalize(m *manifest.Manifest, targetFlag string, skillSubset []string, requestedKeys map[string]bool, packages []string, result *resolver.ResolutionResult, newLock, existingLock *lockfile.Lockfile, existingNode, node *yaml.Node) error` — does all disk-touching steps: `deploy.Run` (built with a `*deploy.SkillFilter{Names: skillSubset, DepKeys: requestedKeys}` when `skillSubset` is non-empty, `nil` otherwise), no-op comparison (`lockfile.IsSemanticEqual`), `apm.lock.yaml` write, `apm.yml` persist (if `packages` non-empty), final summary print.

`cmd/apm/update.go`'s `runUpdate` calls `resolver.PlanFullUpdate`/`PlanScopedUpdate` instead of `resolver.Resolve` for its resolution step, then calls the exact same `buildLockfile`/`deployAndFinalize` pair (passing `nil` for both `skillSubset` and `requestedKeys` — `apm update` has no `--skill` flag) — no duplicated lockfile-building or deploy logic.

```go
// Any future command with a "resolve -> lockfile -> deploy" shape follows this:
result, err := resolver.SomeNewResolutionFn(...)  // command-specific resolution
if err != nil { return err }
newLock, err := buildLockfile(result, existingLock, regLoader, skillSubset, requestedKeys, noProvenance)
if err != nil { return err }
return deployAndFinalize(m, targetFlag, skillSubset, requestedKeys, packages, result, newLock, existingLock, existingNode, node)
```

### Why
Both functions re-derive `deploy.ResolveTargets(targetFlag, m.Target, ".")` internally rather than taking it as a parameter from the caller — one extra cheap filesystem-signal read per call, traded for a function boundary that doesn't leak an extra parameter through every caller. Keeping the boundary at "resolution in, disk-touching tail shared" means a future command only needs to write its own resolution step, not re-derive or copy-paste the lockfile/deploy/write sequence.

---

## Scenario: Dependency-Key-Scoped Filter + Fail-Loud Partial-Application Guard (`--skill`)

### 1. Scope / Trigger
- Trigger: any CLI flag that names a subset of things to keep (a name whitelist, e.g. `--skill <name>`, repeatable) but is semantically meant to apply **only to a specific target of this call** (here, the positional package(s) given in the same invocation) — not to everything in scope that happens to share a name.
- Applies today to `apm install <pkg> --skill <name>` (`cmd/apm/install.go`, `internal/deploy/deploy.go`). Applies to any future flag with the same "scope a name-selection to only this call's target" shape.

### 2. Signatures
- `deploy.SkillFilter{Names []string, DepKeys []string}` (`internal/deploy/deploy.go`)
- `deploy.Run(targets []string, projectDir string, m *manifest.Manifest, resolved *resolver.ResolutionResult, filter *SkillFilter) (*DeployResult, error)`
- `deploy.DepRefKey(ref *manifest.DependencyReference) string` (exported; must produce the same key format as `resolver.ResolvedDep.Key` / `Primitive.DepKey`)
- `buildLockfile(..., skillSubset []string, requestedKeys map[string]bool, ...)` (`cmd/apm/install.go`) — see the shared-tail pattern above for the rest of the signature.

### 3. Contracts
- A bare name-whitelist (`map[string]bool` of allowed names, checked against `Primitive.Name` alone) cannot distinguish "this primitive belongs to the package `--skill` targeted" from "this primitive belongs to something else that happens to not share that name." The original bug: `deploy.Run`'s filter checked only `p.Name`, so it silently suppressed **local** primitives (`DepKey == ""`) and every **other already-declared dependency's** skills whenever `--skill` was used at all, not just the unselected skills within the targeted package.
- The fix scopes by key, not just by name: `SkillFilter.DepKeys` is the set of dependency keys (`repo_url` or `repo_url/virtual_path`) the flag was requested for, computed once in the CLI layer via `deploy.DepRefKey(ref)` for each positional package argument. The filter (`deploy.go`'s step-2 collect-then-filter) only drops a `TypeSkills` primitive when **both** `depKeySet[p.DepKey]` and `!nameSet[p.Name]` hold — anything whose `DepKey` isn't in the target set passes through untouched regardless of its name.
- A scoped filter that matches **nothing** must fail loud, not silently no-op and report success. Three ways "nothing matched" happens in practice: (a) the flag was given with zero positional packages; (b) the flag was combined with a mode that skips resolution entirely (`--frozen` — no `buildLockfile` call happens at all on that path); (c) the targeted package string never made it into the resolved dependency graph (e.g. it collided with an already-declared dependency during positional-arg dedup, which is keyed by bare `repo_url` and blind to a `virtual_path` suffix — a separate, pre-existing, out-of-scope limitation this guard does not fix but does fail loudly against).
- `buildLockfile` tracks `matchedKeys[dep.Key] = true` for every resolved dep whose key is in `requestedKeys`. After the loop, it requires **every** key in `requestedKeys` to be in `matchedKeys` — not just "at least one." Checking only "any matched" is insufficient: with multiple positional packages, one valid match masks another that silently never resolved (found in a second codex review round, after a first "any matched" version of this guard had already passed a first review round). Unmatched keys are named in the error, sorted for determinism.
- The CLI entry point (`runInstall`) additionally validates **before any file I/O** — right after CI-environment auto-frozen detection, before `apm.yml` is even read — that the flag requires at least one positional package and rejects the flag combined with `--frozen`.

### 4. Validation & Error Matrix
- `--skill` given, `--frozen` (explicit or CI-auto-detected) also active -> reject immediately: `"--skill is not supported with a frozen install"`.
- `--skill` given, zero positional packages -> reject immediately: `"--skill requires at least one positional package to install"`.
- `--skill` given, packages given, but `requestedKeys` ends up empty at `buildLockfile` time (defense in depth for any caller that reaches `buildLockfile` without going through `runInstall`'s upfront check) -> reject: `"--skill ... requires at least one resolved package to scope to"`.
- `--skill` given, some but not all requested keys matched a resolved dependency -> reject, naming the unmatched key(s): `"--skill ...: package(s) ... did not resolve into the dependency graph"`.
- `--skill` given, every requested key matched -> proceed; `skill_subset` is written only on the matched dependency's lock entry, and the deploy-time filter only touches that dependency's skill primitives.

### 5. Good/Base/Bad Cases
- Good: `apm install acme/skills --skill deploy` where `acme/skills` has skills `deploy` and `preview` -> `deploy` deploys, `preview` does not, local skills and any other already-declared dependency are untouched.
- Base: `apm install good/pkg collided/pkg/sub --skill x` where `collided/pkg/sub` collides with an already-declared bare `collided/pkg` during dedup and is silently never added to the resolved graph -> rejected, naming `collided/pkg/sub` as unmatched, instead of quietly locking/deploying only `good/pkg`'s subset while pretending `collided/pkg/sub` was handled too.
- Bad: checking only `len(skillSubset) > 0 && !skillSubsetApplied` (at least one match) instead of per-key matching — passes the good case, silently hides the base case.

### 6. Tests Required
- `internal/deploy/deploy_test.go`: `TestRun_SkillFilterScopedToDepKey` — local skill and another dependency's skill both survive a `--skill` scoped to a third dependency; the targeted dependency's unselected skill still does not deploy.
- `cmd/apm/install_test.go`: `TestBuildLockfile_SkillSubsetScopedToRequestedDep` (only the targeted dep's lock entry gets `skill_subset`), `TestBuildLockfile_SkillSubsetNoMatch_Errors` (empty `requestedKeys`), `TestBuildLockfile_SkillSubsetPartialMatch_Errors` (two requested keys, only one resolves), `TestRunInstall_SkillWithoutPackages_Errors`, `TestRunInstall_SkillWithFrozen_Errors`.

### 7. Wrong vs Correct
#### Wrong
```go
// deploy.go: name-only filter, no key scoping
var skillFilter map[string]bool
for _, s := range skillSubset {
    skillFilter[s] = true
}
if p.Type == TypeSkills && !skillFilter[p.Name] {
    continue // drops ANY skill anywhere in the project not in the whitelist
}
```

#### Correct
```go
// deploy.go: scoped by both name and dep key
if p.Type == TypeSkills && skillDepKeys[p.DepKey] && !skillNames[p.Name] {
    continue // only drops unselected skills belonging to the targeted dependency
}
```
```go
// install.go buildLockfile: fail loud instead of silent partial/zero application
if len(skillSubset) > 0 {
    if len(requestedKeys) == 0 {
        return nil, fmt.Errorf("--skill %s requires at least one resolved package to scope to", strings.Join(skillSubset, ", "))
    }
    var unmatched []string
    for key := range requestedKeys {
        if !matchedKeys[key] {
            unmatched = append(unmatched, key)
        }
    }
    if len(unmatched) > 0 {
        sort.Strings(unmatched)
        return nil, fmt.Errorf("--skill %s: package(s) %s did not resolve into the dependency graph", strings.Join(skillSubset, ", "), strings.Join(unmatched, ", "))
    }
}
```
