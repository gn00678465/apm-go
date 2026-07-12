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

**`apm update` shares this gate too** (fixed, task 07-11-update-local-deps): `runUpdate` runs the identical `len(result.Deps) > 0 && len(targets) == 0` check (`deploy.ResolveTargets("", m.Target, ".")` — update has no `--target` flag) after `printUpdateSummary` and before `deployAndFinalize`, reusing `errNoDeployTarget()` so wording/exit code never drift from install's. Unlike Python (which short-circuits at an unchanged-plan gate before target resolution ever runs — D3 below), apm-go's update gate is unconditional whenever deps are present: a deps-present update with zero resolvable target always exits 2, even when nothing would have changed, so it can never silently rewrite `apm.lock.yaml` with no deployment to show for it.

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

> **Fixed (task 07-11-update-local-deps; commit: `105b2f6`, 2026-07-11)**: `cmd/apm/update.go` `runUpdate` now runs the same `normalizeLocalDep` loop over `m.ParsedDeps`/`m.ParsedDevDeps` install.go's 1b-2 step does, immediately after `manifest.ParseManifest` succeeds. Python oracle live A/B (`research/findings.md` §A-C, 2026-07-11) confirmed `apm update` and `apm install` share Python's install pipeline (`update.py:530-541` → `_install_apm_dependencies(update_refs=True, ...)`), so a local dep IS materialized+deployed by `apm update` there too — this was scope parity to fix, not a documented deviation. Pre-fix, apm-go's gap was worse than "fails safe": `PlanFullUpdate` re-resolved the un-normalized dep under its bare `LocalPath` key and `buildLockfile` silently rewrote the existing `_local/<base>-<sha8>` lockfile entry (with its `deployed_files`/`deployed_file_hashes`) into a bare `repo_url: ./x, source: local` entry, destroying uninstall/frozen provenance (findings C#2). A scoped `apm update ./dep-pkg` token is translated into the same `_local/<base>-<sha8>` key via a `manifest.ParseDepString`+`IsLocal` check (mirrors `uninstallRemovalKey`), so it keeps matching the now-normalized manifest entry. Repro/regression tests: `cmd/apm/update_local_test.go`.

**Uninstall key translation (ag-23 fix, commit 171fd87)**: `uninstall`'s matching space (`uninstallIdentity` → synthetic `local:<path>`) is NOT the lockfile/apm_modules key space. `cmd/apm/uninstall.go` `uninstallRemovalKey` translates a matched local identity to `localModulesKey(resolveLocalSourceAbs(path))` before it reaches `SafeRemoveModuleDir`/`lock.RemoveKeys`/deployed-provenance; git/marketplace identities pass through unchanged. The apm.yml splice stays in identity space (`internal/manifest/remove.go` computes the same synthetic `local:` key for local entries whose `IdentityKey()` is empty). **Wrong**: feeding `local:./x` to any apm_modules/lockfile consumer (invalid Windows path + never matches).
> Fixed (commit `3c9910c`, 2026-07-11, task 07-11-local-root-key-space): `uninstallRemainingRootKeys` now translates a SURVIVING local root's `local:<path>` identity through `uninstallRemovalKey` into `_local/<base>-<sha8>` before storing it, matching the reachability BFS / stale-MCP key space. Repro tests: `cmd/apm/uninstall_local_survivor_test.go` (diamond transitive protection, MCP non-stale, dry-run, devDeps).

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

## 9. Codex agents deployment: MD -> TOML (task 07-12-codex-agent-toml)

**Contract**: the codex adapter's `TypeAgents` deployment converts a markdown
agent source into a three-key TOML document at `.codex/agents/<p.Name>.toml`
— it does NOT byte-copy the markdown (byte-copying produces a `.toml`
extension over non-TOML content, which Codex CLI cannot parse). This
mirrors Python's `_write_codex_agent` (`agent_integrator.py:302-335`)
exactly; claude/opencode/copilot agents deployment is unchanged (still
byte-copy, still parity).

- Signature: `transformCodexAgent(sourcePath string) (codexAgentDoc, error)`
  + `deployCodexAgentTOML(p Primitive, projectDir string) ([]string, error)`
  (`internal/deploy/codex_agent.go`); serialized via the same
  `github.com/pelletier/go-toml/v2` module already used by the codex MCP
  `config.toml` writer (`internal/deploy/mcp_codex.go`) — no new dependency.
- Output document, exactly three keys, in this order:
  `{name, description, developer_instructions}`.

**Six-point oracle semantics** (`agent_integrator.py:302-335`):

1. A symlink source is rejected outright (`os.Lstat`, never followed) —
   `transformCodexAgent` errors before reading content.
2. `name` defaults to the filename stem with a trailing `.agent` suffix
   stripped (e.g. `accessibility-runtime-tester.agent.md` ->
   `accessibility-runtime-tester`).
3. A frontmatter block matching `^---\s*\n(.*?)\n---\s*\n?` (DOTALL,
   anchored at the absolute start of the file — the same regex as
   `claudeFrontmatterRE` in `internal/deploy/instructions_claude.go`) is cut
   from the body regardless of whether it parses as YAML. When the captured
   block DOES parse (`go.yaml.in/yaml/v4`, into `map[string]any`), string
   `name`/`description` keys override the defaults; **a YAML parse failure
   is swallowed silently** (mirrors Python's bare `except: pass`) — the
   frontmatter is still cut from the body, and name/description stay at
   their defaults. Any other key (`model`, `tools`, ...) is always ignored.
4. `description` defaults to `""` (never omitted, never null).
5. `developer_instructions` = body with frontmatter removed, then
   `strings.TrimSpace` (Python `.strip()`) — only the two ends are trimmed;
   internal blank lines/whitespace are preserved verbatim.
6. When no frontmatter block matches (including the case where `---...---`
   appears after a leading blank line — the anchor requires the very first
   byte of the file), the entire file content becomes
   `developer_instructions` after `strings.TrimSpace`.

| Input | `name` | `description` | `developer_instructions` |
|---|---|---|---|
| `foo.agent.md`, no frontmatter | `foo` | `""` | full body, trimmed |
| frontmatter with `name`/`description` | frontmatter value | frontmatter value | body after frontmatter, trimmed |
| frontmatter with unterminated quote (invalid YAML) | fallback (`.agent` stripped) | `""` | body after frontmatter, trimmed (frontmatter still cut) |
| frontmatter present but no `description` key | fallback or frontmatter `name` | `""` | body after frontmatter, trimmed |

**Wrong vs correct**: `deployFileToPath(p, ".codex/agents/<name>.toml", ...)`
(pre-07-12 behavior, byte-copy) writes markdown bytes under a `.toml`
extension — a file that fails to parse in any TOML reader; the fix must
always go through `transformCodexAgent`.

**Downstream**: filename (`<p.Name>.toml`), lockfile provenance (hash of the
transformed TOML bytes), `audit`, and `uninstall` all continue through the
existing generic per-file mechanisms in `deploy.Run`/`cmd/apm/audit.go` — no
Codex-specific lock/uninstall/audit branch was added.

Fixed (task 07-12-codex-agent-toml; commit: `197fe98`, 2026-07-12): oracle A/B (scratch, `uv --project D:/Projects/apm-dev/apm run apm install --target codex` vs. `bin/apm-go.exe install --target codex`) confirmed parsed-TOML semantic equality (key set + all three values) across a frontmatter-override fixture, a malformed-YAML fixture, and a no-frontmatter fixture; `evals/test1`'s `accessibility-runtime-tester.agent.md` replayed in a throwaway scratch copy produces a Codex TOML that parses with the expected `name`/`description`/`developer_instructions`.

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
| D1 | `update` requires an existing `apm.lock.yaml` (exit 1 without one) | `update` runs without a lockfile too (treats every dep as `[+] added`) | task 07-11-update-local-deps; pre-existing apm-go behavior (`update.go:74-76`, unchanged by this task), fail-loud kept over silent full-add |
| D2 | `update` re-copies + redeploys a local dep on every run, even when nothing else in the plan changed (idempotent) | plan-unchanged short-circuits BEFORE resolve's local-dep copy/deploy step; a content-only change to a local dep's source is invisible to a subsequent `update` until something else changes the plan | task 07-11-update-local-deps (findings P2/P2b); apm-go is a strict superset (always fresh) and self-heals the copy/deploy drift Python's own plan-unchanged short-circuit can produce |
| D3 | `update` with deps present and zero resolvable target **always** exits 2 (`no deployment target detected`), independent of whether the plan changed | zero-target failure (`NoHarnessError`, exit 2) only surfaces when the plan gate lets resolution proceed; a zero-target update whose plan is unchanged exits 0 before target resolution ever runs | task 07-11-update-local-deps (findings C#3/C#3b); apm-go has no plan/consent gate, so this is the stricter (fail-loud) direction, consistent with install's own zero-target matrix (§2) — required so a doomed update can never silently rewrite `apm.lock.yaml` |
