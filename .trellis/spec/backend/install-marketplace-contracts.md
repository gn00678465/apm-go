# Install / Marketplace CLI Contracts

> Executable contracts for `apm-go install` and `apm-go marketplace`, established/hardened on branch `feat/marketplace-install` (task 07-05-uninstall). Oracle = Python `microsoft/apm` (`apm/src/apm_cli/`); conformance ids = `.trellis/spec/conformance/openapm-v0.1.md`.
>
> **Language**: English.

---

## 1. Insecure dependency gate (S1, req-sc-006/008)

**Contract**: `apm-go install` refuses any dependency whose resolved `Scheme == "http"` unless `--allow-insecure` is passed.

- Signature: `manifest.CheckInsecureDependencyScheme(dep *DependencyReference, allowInsecure bool, defaultHost string) error`
- Scope: every direct dep — CLI positionals + `dependencies.apm` + `devDependencies.apm` (via `allDirectDeps(m)`), checked **before any git clone** (install.go step "1c").
- **Flag-only, NO host exemption**: loopback/RFC1918/localhost are refused like any other host — mirrors Python `install.py` `_check_insecure_dependencies` (which has no host exemption for git deps). The loopback exemption in the codebase (`isLoopbackOrPrivate`) belongs to the *registries* gate (`manifest.go`), not this one.
- Error names the dep via a reconstructed display URL, never leaks credentials.

| Condition | Result |
|---|---|
| `http://` dep, no `--allow-insecure` | error, exit 1, no clone |
| `http://` dep + `--allow-insecure` | proceeds |
| `https`/`ssh`/local/parent (Scheme `""`) | no-op |

**Deviation**: transitive `http://` deps (a dependency's own sub-dep) are NOT checked — Python guards those via `--allow-insecure-host` / `_guard_transitive_insecure_dependencies`, unimplemented here (tracked, deferred).

---

## 2. `--target` resolution (F2, req-mf-005 / tg-001..004)

- Signature: `install --target|-t <csv>`; `deploy.SplitTargetFlag(flag string) ([]string, error)`; `deploy.ResolveTargets(flag string, manifestTargets []string, projectDir string) ([]string, []string)`.
- `--target` has shorthand `-t`; value is comma-split (trim, drop empty); each token validated via `manifest.ValidateTarget` (same validator as apm.yml `target:`). Unknown token → error naming it (exit 2 via `withExitCode`).
- Known-but-adapterless canonical targets (cursor/gemini/windsurf) PASS validation but get a non-fatal `checkUnsupported` "no registered handler" diagnostic (req-tg-004) — never a hard error.

| Condition | Result |
|---|---|
| `-t claude,codex` | deploys to both |
| `--target bogus` (unknown vocab) | exit 2, names token |
| deps present + zero resolvable target | exit 2 + teaching msg, **diags printed**, no partial write |
| zero deps + zero local prims + zero target | exit 0 (`hasAnyDeps` gate; empty project) |
| zero deps + local `.apm/` primitives present + zero target | exit 2 + same teaching msg, **diags printed**, nothing deployed/written (task 07-11-instructions-applyto-parity; Python parity: "No harness detected" fires for anything to integrate — deps OR local primitives) |

**Wrong vs correct**: the zero-target exit-2 guard MUST reuse step-4's `targets`/`targetDiags` (so the adapterless diagnostic still prints) and run **before** `deployAndFinalize` (no partial lockfile/apm.yml write).

---

## 3. devDependencies in resolution (F3)

- `resolver.collectResolutionRootDeps(m)` seeds the **true** BFS root with `ParsedDeps` then `ParsedDevDeps` (prod-then-dev, mirrors Python `build_dependency_tree`).
- The shared `collectRootDeps` (prod-only) is used for **transitive** sub-manifest expansion — a dependency's own devDependencies do NOT propagate to consumers.
- `install`/`update`/`deploy.Run`/frozen check use `allDirectDeps`/`hasAnyDeps` (prod+dev), so a dev-only manifest still resolves/deploys/locks/frozen-verifies.

> **Warning / follow-up (F3 gap)**: `collectResolutionRootDeps` does NOT dedupe by key. Same package in both `dependencies.apm` and `devDependencies.apm` with incompatible constraints → apm-go errors (diamond conflict) where Python keeps prod. Fix needs a pre-queue dedupe (prod wins) + determinism test.

---

## 4. Local / absolute-path dependency materialization (F1)

**Contract**: a local dependency (relative or absolute path) is materialized by **COPY** into `apm_modules/_local/<sanitizedBase>-<sha8>/`, never `git clone`.

- `cmd/apm`: `normalizeLocalDep` sets a runtime-only `DependencyReference.LocalSourcePath` (never serialized) and `RepoURL = localModulesKey(abs)` = `"_local/" + sanitizePathSegment(base) + "-" + sha256(cleanAbs)[:8]`. Deterministic (round-trip reuses the key), collision-resistant, single separator-free segment.
- `gitops.LoadPackage`: if `LocalSourcePath != ""` → `materializeLocalCopy` (dest via `installPath`, still `archive.ContainedKey`-guarded) → `copyTreeNoSymlinks` (skips symlinks AND non-regular files — no follow-out-of-tree escape).
- Persistence: `apm.yml` stores a `./`-relative path when the source is **under** the project root; the **absolute** path when out-of-tree (`localPathForManifest`). Both round-trip.
- `depref.ParseDepString` accepts absolute local paths (POSIX/Windows/UNC via `isAbsoluteLocalPath`) as `IsLocal` deps, **skipping** the relative-escape guard (an explicit absolute path is user-intended, not a relative traversal). Relative paths still run `containsEscape`.
- `lockfile.ParseLockfile`: absolute `repo_url` allowed **only** for `source == "git"` (`validateRepoURLAbsoluteness`); `registry`/`local`/unset still rejected; the `..`-segment check (`validateNoDotDotSegments`) runs **unconditionally** on every `repo_url`.

| Producer | `repo_url` in lockfile |
|---|---|
| in-tree local marketplace plugin | `_local/<base>-<sha8>` (apm.yml: relative) |
| out-of-tree local dep | `_local/<base>-<sha8>` (apm.yml: absolute) |
| tampered `source:registry` + absolute path | **rejected** |

> **Warning / follow-up (F1 gap)**: `cmd/apm/update.go` `runUpdate` does NOT call `normalizeLocalDep`, so local deps are not copy-materialized on `apm update` (fails safe: nothing deployed). Inconsistent with `install`; scope question (should `update` deploy local deps?).

**Uninstall key translation (ag-23 fix, commit 171fd87)**: `uninstall`'s matching space (`uninstallIdentity` → synthetic `local:<path>`) is NOT the lockfile/apm_modules key space. `cmd/apm/uninstall.go` `uninstallRemovalKey` translates a matched local identity to `localModulesKey(resolveLocalSourceAbs(path))` before it reaches `SafeRemoveModuleDir`/`lock.RemoveKeys`/deployed-provenance; git/marketplace identities pass through unchanged. The apm.yml splice stays in identity space (`internal/manifest/remove.go` computes the same synthetic `local:` key for local entries whose `IdentityKey()` is empty). **Wrong**: feeding `local:./x` to any apm_modules/lockfile consumer (invalid Windows path + never matches).
> Follow-up: `uninstallRemainingRootKeys` still emits `local:` keys for SURVIVING local roots, mismatching the reachability BFS / stale-MCP `_local/` space (recorded in task 07-05-antigravity-research prd).

**Security invariant (do not weaken)**: no WRITE-side guard was relaxed — `archive.Contained`/`ContainedKey`, symlink refusal, plugin.json resolver, lockfile `..` check all still fail-closed. Only the READ source-path policy (which paths apm may read a dep FROM) was widened.

---

## 5. Marketplace `package add`/`set` (S2, F4)

- `--subdir` rejects any `.`/`..` segment and absolute paths (`validateSubdir`, mirrors Python `validate_path_segments`) on both `add` and `set`. Error → exit 2 (cmd-layer `withExitCode`).
  - Follow-up (LOW): does not percent-decode segments (`%2e%2e`); non-exploitable today (no local-fs consumer of `Subdir`).
- `--ref` mutable value (branch / `HEAD` / short tag, i.e. not a 40-hex SHA) is resolved to a concrete SHA via `RefLister.ListRefs` before write (`resolveRef`); a 40-hex SHA is stored verbatim; an empty ref short-circuits (no lister call — preserves mkt-046 zero-flag local add). `set` always resolves a given ref (no `--no-verify` escape on `set`).
  - `RefLister` runs `git ls-remote <url> HEAD refs/tags/* refs/heads/*` (NOT `--tags --heads`, which never emits a `HEAD` line) so `--ref HEAD` resolves.
- `package set` with zero field flags → error `"No fields specified..."` exit 1 (matches Python `set.py`; NOT `withExitCode(2)`).
- `marketplace add` validates `--name` (alias regex `^[a-zA-Z0-9._-]+$`) and `--host` (FQDN) before any network; invalid → exit 1 naming the value.

---

## 6. MCP `--url` scheme (C9)

`manifest.ValidateMCP`: a **literal** (non-placeholder) MCP server URL must have scheme `http` or `https` (`allowedMCPURLSchemes`, mirrors Python `_ALLOWED_URL_SCHEMES`). Placeholder URLs (`${VAR}`) skip the check; embedded-credential and empty-scheme/host rejections unchanged; error names only the scheme (no URL echo — query strings can carry tokens).

---

## 7. Confirmation prompts (C10)

`marketplace remove` and `marketplace package remove` share `confirmOrRequireYes(label, errMsg)` + `readYesNo`:

| Input state | Result |
|---|---|
| non-interactive stdin, no `--yes` | error `requires -y/--yes...`, exit ≠0, **not removed** |
| interactive, confirm read EOF/error (`ok=false`) | same error, exit ≠0, **not removed** |
| interactive explicit `n` (`ok=true, yes=false`) | clean `Aborted.`, exit 0, not removed |
| `--yes` / explicit `y` | proceeds |

**Wrong vs correct**: a failed/EOF confirm read MUST NOT be treated as a decline (old bug: git-bash pipe → prompt → EOF → `Aborted` + **exit 0 without removing** = CI footgun, since exit 0 reads as success).

---

## 8. Instructions pipeline: applyTo -> per-target formats (task 07-11-instructions-applyto-parity)

**Pairing contract** (mirrors Python): the `*.instructions.md` filename and the `applyTo` frontmatter are ONE contract. `.apm/instructions/` collects ONLY `*.instructions.md` (`extractInstructionName`, plain `.md` fallback removed); the `applyTo` value is translated to each target's native scoping semantic where one exists.

- Signature: `convertToClaudeRules(content []byte) []byte` + `parseApplyTo(string) []string` + `yamlDoubleQuote(string) string` (`internal/deploy/instructions_claude.go`), oracle = Python `_convert_to_claude_rules` / `patterns.py`.
- claude: `applyTo: "**/*.go"` -> `paths:` YAML list frontmatter; no applyTo / no frontmatter -> unconditional rule (existing frontmatter STRIPPED, leading blank lines trimmed). `parseApplyTo` splits on TOP-LEVEL commas only (brace alternation `{a,b}` commas are part of the glob); values are quote-stripped (cutset). Emitted frontmatter is LF; body bytes preserved.
- copilot: NO transform (applyTo is copilot-native). antigravity: NO transform (07-05 documented deviation, byte-copy). Both locked by `TestDeployOtherTargets_InstructionsStayByteIdentical`.
- The transform sits at the claude adapter layer -> applies to local AND dependency instructions; lockfile hashes are computed post-write so uninstall provenance is unaffected.

| Input | claude output |
|---|---|
| `applyTo: "**/*.{css,scss},**/*.py"` | `paths:` with 2 entries (brace comma not split) |
| frontmatter without applyTo | body only (frontmatter stripped) |
| no frontmatter | passthrough minus leading blank lines |
| glob containing `"` or `\` | YAML 1.2 double-quote escaped |

**Wrong vs correct**: byte-copying `applyTo` to `.claude/rules/` silently voids scoping (Claude Code reads `paths:`, not `applyTo`) -- the pre-04f4e58 behavior.

> **Follow-up (Codex verify LOW-B)**: Python's `find_files_by_glob` also collects `*.instructions.md` from the PACKAGE ROOT (not just `.apm/instructions/`) and skips symlinks/hardlinks; apm-go scans `.apm/instructions/` only, no link filter. Pre-existing scope difference, recorded in task 07-11 prd -- decide parity vs documented deviation.

---

## Documented deviations (intentional — keep apm-go behavior, record in conformance statement)

| id | apm-go | Python | rationale |
|---|---|---|---|
| C3 | corrupt `marketplaces.json` → hard error (fail-fast) | silent empty-registry fallback | fail-fast surfaces corruption |
| C4 | `list` SOURCE = raw URL; `--verbose` adds HOST col; `browse -v` extra summary | compact `display_source`, no HOST col | cosmetic |
| C7 | `package add/set/remove` no-config / both-config guard → exit 2 | exit 1 | uniform exit-2 for package edit failures |
| C8 | `uninstall` zero-args → exit 1 (cobra) | exit 2 (click) | cobra default |
| — | `add --ref`+`--branch` mutex msg = cobra template | custom sentence | cosmetic |
| — | deployed agents keep source bytes (LF) | text-mode rewrite (CRLF on Windows) | apm-go is more faithful; content identical |
| — | no automatic `.gitignore` (apm_modules/) write | appends `apm_modules/` to .gitignore | cosmetic; user-managed file left alone |
