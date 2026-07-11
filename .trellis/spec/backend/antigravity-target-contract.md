# Antigravity Target Contract

> Executable contracts for the `antigravity` deploy target (Google Antigravity, CLI alias `agy`), established in task 07-05-antigravity-research (branch `feat/marketplace-install`, commits `d72dc6a` / `c6ef3f7` / `3471e45`). Evidence: official docs + agy **1.0.16** binary/live verification (`.trellis/tasks/07-05-antigravity-research/research/cli-*.md`).
>
> **Language**: English.

---

> See also: [Compile Contract](./compile-contract.md) — `apm-go compile` is
> the only apm-go path that produces an `AGENTS.md` for antigravity (this
> adapter's own `DeployPrimitive` only byte-copies instructions to
> `.agents/rules/`, §3/§4 below; it never maintains `AGENTS.md`).

## 1. MCP writer: `serverUrl` for ALL remote transports (d72dc6a)

- Signature: `antigravityMCPEntry(r *ResolvedMCPServer) (map[string]any, bool, string)` in `internal/deploy/mcp_antigravity.go`.
- Output file `.agents/mcp_config.json`, top-level key `mcpServers`, mode `ResolveBake` (agy has NO runtime `${VAR}` interpolation — verified: no `os.ExpandEnv` on the MCP path in the binary).

| Transport | Fields written |
|---|---|
| `stdio` | `command` (+ `args`, `env` when present) |
| `sse`, `http`, `streamable-http` (any non-stdio) | `serverUrl` (+ `headers` when present) |

**Wrong vs correct**:

```go
// WRONG (pre-d72dc6a): sse special case
if r.Transport == "sse" { e["url"] = r.URL } else { e["serverUrl"] = r.URL }

// CORRECT: all remote transports
e := map[string]any{"serverUrl": r.URL}
```

Why: official docs verbatim — "Legacy fields like `url` or `httpUrl` are not supported"; the agy 1.0.16 binary validator only accepts `command|serverUrl` as transport discriminators ("MCP server %q must have either command or serverUrl"). Test lock: `TestWriteMCP_Antigravity_SSEUsesServerUrlField` (`mcp_writers_test.go`) — sse entry MUST have `serverUrl`, MUST NOT have `url`.

---

## 2. Explicit-only target selection (c6ef3f7, BREAKING)

- `explicitOnlyTargets` (`internal/deploy/adapter.go`) contains `antigravity` (and `agent-skills`); `allAutoDetectableTargets()` = `{claude, codex, copilot, opencode}` — antigravity excluded.
- `SignalWhitelist` (`internal/manifest/detect.go`) has NO GEMINI.md/AGENTS.md entries: those are cross-tool files (also read by opencode/agent-skills tooling) and must not auto-enable antigravity. Aligns with Python `EXPLICIT_ONLY_TARGETS={"agent-skills","antigravity"}` (user decision 2026-07-05).
- Alias: `TargetAliases["agy"] = "antigravity"` (`internal/manifest/target.go`). BOTH selection paths canonicalize through `manifest.ValidateTarget`: the `--target` flag via `deploy.SplitTargetFlag`, apm.yml `target:` via `parseTargetField` (`internal/manifest/manifest.go`) — so the alias needs exactly one table entry and `filterSupported` never sees a raw `agy`.

| Selection | Result |
|---|---|
| `--target antigravity` / `--target agy` | deploys ✓ |
| apm.yml `target: [antigravity]` / `[agy]` | deploys ✓ |
| `--target all` / apm.yml `target: [all]` | antigravity NOT included |
| GEMINI.md / AGENTS.md present, no explicit selection | NOT detected, nothing deployed |

**BREAKING**: users who relied on GEMINI.md/AGENTS.md auto-detection must now select antigravity explicitly. Test lock: `TestResolveTargets_AntigravityExplicitSelection` (+ `..._FlagAllExcludesAntigravity`, `..._ManifestAllExcludesAntigravity`, `..._AntigravityNotAutoDetected`).

---

## 3. Agents primitive (3471e45 — documented extension ahead of Python upstream)

- `antigravityAdapter.SupportedTypes()` includes `TypeAgents`; mapping:
  ```go
  case TypeAgents:
      return deployFileToPath(p, fmt.Sprintf(".agents/agents/%s/agent.md", p.Name), projectDir)
  ```
- **Per-agent directory, fixed filename `agent.md`** — unlike claude's flat `.claude/agents/<name>.md`. Static format exists since Antigravity CLI **1.0.16** (JSON→Markdown transition per changelog); live-verified discovered by agy.
- Byte-copy, no frontmatter transform (adapter-wide convention). Python upstream has NO antigravity agents mapping — this is an intentional apm-go extension (user decision 2026-07-10).
- Collision semantics: same-name agents are resolved BEFORE any adapter runs, in `ResolvePrimitives` (`internal/deploy/conflict.go`) — first-declared wins (req-pr-003), local overrides dependency (req-pr-002). The `writtenBy` overwrite diagnostic in `deploy.go` only covers different-named primitives converging on one FIXED path (e.g. `.agents/hooks.json`), never per-name paths. Test lock: `TestRun_AgentSameNameCollision_FirstDeclaredWins` (table-driven claude + antigravity).
- Uninstall: fully generic over `DeployPrimitive`-returned paths (lockfile `deployed_files`/`deployed_file_hashes` → `deploy.RemoveDeployedFiles` → `cleanupEmptyParents` prunes the empty per-agent dir, stops at non-empty ancestors). Test lock: `TestRemoveDeployedFiles_AntigravityAgentDirPrunedSiblingSurvives`.

---

## 4. Documented deviations (intentional — keep apm-go behavior)

| id | apm-go | Python | evidence / rationale |
|---|---|---|---|
| instructions frontmatter | byte-copy, frontmatter NOT stripped (`.agents/rules/<name>.md`) | strips YAML frontmatter (`_convert_to_antigravity_rules`) | live-verified: agy discovers and can follow a rule WITH frontmatter; no official frontmatter semantics |
| agents primitive | deployed (`.agents/agents/<name>/agent.md`) | no mapping | CLI ≥1.0.16 static format; apm-go ahead of upstream |
| rules enforcement expectation | files deployed; **agy CLI does NOT auto-inject workspace rules into system context** (only `user_global`) — rules are discoverable/readable by the agent on demand | n/a | 6-probe live matrix on agy 1.0.16 `--print` (task prd.md "Step 0 實機驗證結果"); deployment stays correct — activation semantics are agy's own concern |

---

## 5. agy operational gotchas (for verification work)

> **Warning**: agy only scans workspace `.agents/` when the workspace is **registered as a project** (`--new-project` on first run, `--project <id>` to reuse). Under the default `default-cli-project` (no folder resources), workspace rules/skills/agents are ALL invisible — a probe that skips registration falsely concludes "not supported".

- `agy --print "<prompt>"` runs a single prompt non-interactively (with `--print-timeout`) — use it for automated verification; there is NO `agy mcp`/`agy skills`/`agy agents` subcommand (TUI slash commands only).
- `agy plugin validate <dir>` verifies apm-go output formats with the real binary: accepts nested `agents/<name>/agent.md`, reports skills/agents/commands/mcpServers/hooks processed, and **resolves stdio `command` on PATH** (a fake command aborts validation) — use a real executable in fixtures. It does not check `rules/`.
- Global customization root is `~/.gemini/config/` (NOT `~/.gemini/antigravity-cli/`, which is app-data; several official CLI doc pages state stale paths).

---

## 6. Verification pattern (A/B without the Python oracle)

Python apm is not the oracle for antigravity CLI features. Pattern used (task Step 4, all PASS):

1. **Primary — structural compare**: fixture project → `apm-go install --target agy` → assert file tree + field-level JSON (`serverUrl` not `url`; byte-identity for agents/rules/skills/hooks) against binary-verified schemas.
2. **Supplemental — `agy plugin validate`** on outputs packaged as a plugin (must not gate the task: plugin subsystem changes shouldn't fail target work).
3. **Live discovery**: register fixture as agy project, `--print` ask to list agents/skills/rules — apm-go-deployed names must appear.

---

## 7. Plugin bundle deployment (documented extension — task 07-11-antigravity-plugins-bundle, commit `6a66e07`)

> Evidence: agy **1.1.1** binary (`agy --version`), live-verified 2026-07-11
> (`.trellis/tasks/07-11-antigravity-plugins-bundle/research/antigravity-bundle-notes.md`
> §G; design.md; `internal/deploy/antigravity_bundle.go`,
> `internal/deploy/antigravity.go`, `internal/deploy/adapter.go`,
> `internal/deploy/deploy.go`). Python upstream has **no plugin bundle
> concept at all** (`apm_cli/integration/targets.py` antigravity profile is
> flat instructions/skills/hooks only; `hook_integrator.py` merges hooks into
> one shared file's `"apm"` container key) — this entire section is an
> apm-go extension ahead of Python, not a parity fix.

### 7.1 Why: the hooks.json "overwrite, not merge" gap

Section 3's `writtenBy` overwrite diagnostic (`deploy.go`) means every
dependency writing a hook file into the single shared `.agents/hooks.json`
silently overwrites the previous one, keeping only the last-declared
package's hooks. The plugin bundle route gives every dependency its own
physical `hooks.json`, eliminating the **cross-package** instance of this gap
(PRD AC2). It does not, and does not claim to, eliminate the gap **within** a
single package that ships more than one `.apm/hooks/*.json` file — see §7.5.

### 7.2 Bundle layout (dependency primitives only)

```
.agents/
├── rules/<name>.md            # LOCAL instructions -- unchanged, flat (§4)
├── agents/<name>/agent.md     # LOCAL agents -- unchanged, flat (§3)
├── skills/<name>/             # LOCAL skills -- unchanged, flat (req-tg-003)
├── hooks.json                 # LOCAL hooks -- unchanged, flat (§1 table)
├── mcp_config.json            # ALL MCP servers (local + every dependency),
│                               # still merged here -- see §7.4
└── plugins/
    └── <pkg>/                 # one directory per dependency that has at
        │                       # least one antigravity-supported primitive
        ├── plugin.json         # {"name": "<pkg>"}, required (§7.3)
        ├── rules/<name>.md     # dependency instructions, byte-copy
        ├── agents/<name>/agent.md  # dependency agents, byte-copy
        ├── skills/<name>/...   # dependency skills, recursive byte-copy
        └── hooks.json          # dependency hooks, byte-copy
```

Only `Primitive.DepKey != ""` (dependency-sourced) primitives are bundled;
project-owned (`DepKey == ""`) primitives keep every pre-existing flat path
in the table above unchanged (design.md D1) — no `--target all,agy`
multi-target install, no project without dependencies, and no existing
`antigravity-target-contract.md` §1–§6 assertion about LOCAL output paths is
affected by this section. `commands`/`prompts` remain unsupported for
antigravity (§1 table; PRD Non-Goals) and never appear in a bundle.

`DeployPrimitive` (`internal/deploy/antigravity.go`) routes on `p.DepKey ==
""` per primitive type; a non-empty `DepKey` goes to
`antigravityBundleDir(p.DepKey)` + the type's sub-path
(`internal/deploy/antigravity_bundle.go`).

### 7.3 Bundle naming and the `plugin.json` manifest

- **Bundle directory name** = the last `/`-separated segment of the
  dependency's `DepKey` (mirrors `skillNameFromDepKey`,
  `internal/deploy/primitive.go`), reduced to a single safe path segment:
  every character outside `[A-Za-z0-9._-]` becomes `-`, and an empty,
  `.`/`..`, or hidden (leading-`.`) result is replaced with a `pkg-`-prefixed
  fallback (`bundleNameFromDepKey`/`sanitizeBundleSegment`,
  `antigravity_bundle.go`). A materialized local-path dependency's
  `_local/<base>-<hash8>` key collapses to `<base>-<hash8>` — the same
  content hash `cmd/apm/install.go`'s `localModulesKey` already appends means
  two different local-path sources sharing a basename never collide (live-
  verified 2026-07-11: `./acme/tool` and `./other-org/tool` produced
  `tool-79daa3bf` and `tool-ca8a114f`, not `tool`/`tool`).
- **Collision (two DepKeys, same bundle name) is fail-closed, not
  diagnostic-only**: `BundleTarget.ValidateBundleNames`
  (`internal/deploy/adapter.go`) runs for every target BEFORE any primitive
  is deployed to ANY target in that `Run()` call; on a collision it returns a
  non-nil error naming both dependencies, and `deploy.Run` aborts the whole
  call — nothing is written for either colliding dependency (or for any
  other target in the same install). This is a deliberate deviation from
  this task's own design.md draft (which had proposed diagnostic-only +
  write-anyway): mixing two unrelated packages' rules/agents/skills/hooks
  files into one physical directory, with `plugin.json`'s `name` able to
  represent only one of them, was judged a data-integrity problem, not a
  cosmetic one. Live-reproducible collision requires two dependencies whose
  `DepKey` is NOT a materialized local path (e.g. two git deps `acme/tool`
  and `other-org/tool`) — test lock `TestRun_AntigravityBundleNameCollision`.
- **`plugin.json`** is the minimal manifest `{"name": "<bundle-dir-name>"}`,
  UTF-8 no BOM, single trailing LF, byte-deterministic
  (`BundleTarget.FinalizeBundles`, called once per target after every
  primitive has been deployed — mirrors `MCPTarget.WriteMCP`'s once-per-target
  shape, `internal/deploy/deploy.go`). **agy 1.1.1 delta**: the 07-05 research
  (agy 1.0.16) observed `name` as optional (embedded docs said "defaults to
  directory name"); live-reverified 2026-07-11 against agy **1.1.1**, `{}`
  now fails hard: `agy plugin validate` on a manifest with no `name` exits 1
  with `Error: plugin.json missing name`, and a bundle with no `plugin.json`
  at all exits 1 with `Error: missing plugin.json: ...`. Both are asserted as
  negative probes (research §G3/G5). `FinalizeBundles` is idempotent: every
  `Run()` call — including a re-install that only touches other primitives —
  rewrites and re-reports the manifest path+hash into that dependency's
  `PerDep.Files/Hashes`, so it never silently drops out of
  `deployed_files`/`deployed_file_hashes` provenance on a second install
  (test lock `TestRun_AntigravityPluginManifestReinstall`).

### 7.4 MCP is NOT migrated into bundles

`mcp_antigravity.go`'s `WriteMCP` and the `.agents/mcp_config.json` merge
writer (§1, `mcp_common.go`) are unchanged: MCP servers from local and every
dependency continue to merge, by name, into the single shared
`mcp_config.json` at the workspace root. Rationale: the merge-by-name writer
already has no overwrite gap (unlike hooks' fixed single-file copy), and
`agy plugin validate` passes with no `mcp_config.json` present in a bundle at
all (research §G4) — there is no defect to fix and no format requirement to
satisfy by moving it.

### 7.5 Residual gap: same-package multiple hook files

Two hook files **within the same dependency** (e.g. `.apm/hooks/pre.json` +
`.apm/hooks/post.json`) still collapse onto that dependency's one
`plugins/<pkg>/hooks.json`, hitting the pre-existing `writtenBy` overwrite
diagnostic — the same mechanism as the single shared `.agents/hooks.json`
had before this task, just scoped down from "whole install" to "one
package". This is expected, not a bug: PRD AC2 is a cross-package guarantee
("兩套件各帶 hooks 同時安裝互不覆蓋"); a package publishing two hook files is
a plugin-format constraint (one `hooks.json` per bundle), not something
apm-go's collector should silently rename around. Test lock:
`TestRun_AntigravitySameDependencyHooksOverwriteDiagnostic` (single package,
overwrite diagnostic still fires) vs.
`TestRun_AntigravityTwoDependencyHooksIsolated` (two packages, no
cross-package diagnostic — AC2).

### 7.6 Uninstall: zero bundle-specific code

`deploy.RemoveDeployedFiles` + `cleanupEmptyParents`
(`internal/deploy/uninstall.go`) already delete exactly the paths a
`LockedDep`'s `deployed_files` lists (never scanning a directory for "extra"
content) and prune now-empty ancestor directories up to but excluding the
project root — a bundle directory (including its `plugin.json`) is just
another set of deployed paths under that model, so **no uninstall code
changed for this task**. Carried-over safety lines, unchanged:

- **`ContainedKey`** path-escape guard (`internal/archive/extract.go`) is the
  same containment check every other deployed-file path already goes
  through; `bundleNameFromDepKey`'s sanitization additionally guarantees a
  bundle directory name can never be `""`, `.`, `..`, or hidden before that
  guard even runs.
- **Hash mismatch** (un-053): a hand-edited `plugin.json` (or any bundle
  file) whose on-disk sha256 no longer matches `deployed_file_hashes` is
  *kept*, not force-deleted, with a `"modified since deploy (hash mismatch)"`
  stderr warning — leaving a known-limitation non-empty "orphaned" bundle
  directory behind (test lock
  `TestRunUninstall_AntigravityTamperedManifestKeptWithWarning`). A
  correctly-hashed sibling file in the same bundle is still removed
  normally.
- A file the user hand-placed inside a bundle directory (never in
  `deployed_files`) is never touched, and its presence alone keeps that
  bundle directory from being pruned (test lock
  `TestRunUninstall_AntigravityBundleUserFileSurvives`).
- Uninstalling one dependency prunes only its own bundle directory; a
  sibling dependency's bundle, and the shared `.agents/plugins/` root it
  still occupies, survive (test lock
  `TestRunUninstall_AntigravityBundleRemovedSiblingBundleSurvives`).
- **`SafeExtract`/`copyTreeNoSymlinks`** symlink-escape guards
  (`internal/archive/extract.go`, `internal/gitops/clone.go`) are upstream of
  bundle deployment (they gate what ever lands under `apm_modules/.apm/` in
  the first place) and are unmodified by this task; a symlink inside a
  dependency's `.apm/skills/` is skipped before `DeployPrimitive` ever runs,
  so it cannot reach a bundle either — live-verified 2026-07-11 with an
  external-secret symlink fixture (zero leak into `apm_modules` or
  `.agents/plugins`).

### 7.7 Known caveats (recorded, not blocking)

- **Cross-target skill duplication**: `.agents/skills/<name>/` is the
  cross-target canonical skill path shared by every adapter that supports
  skills (req-tg-003). If a project installs antigravity *and* another
  `.agents/skills/`-writing target in the same `install --target
  all,agy`-style call, a dependency's skill lands in both the canonical path
  (written by the other target's adapter) and its antigravity plugin bundle
  (written by antigravity's adapter) — two physical copies agy may discover
  twice. Antigravity installed alone has no such duplication. Not addressed
  by this task (PRD Non-Goals scope); recorded as a known limitation.
- **Re-install after a version upgrade** does not retroactively clean up
  files a previous apm-go version deployed to now-abandoned paths (e.g. a
  pre-bundle install's flat dependency files) — `deployed_files` is replaced
  wholesale per dependency on each `install` (`cmd/apm/install.go`), so old
  paths simply lose their provenance record. This mirrors the project's
  existing "changed target output shape between versions" behavior for every
  other adapter change, not something new to bundles. A clean migration is
  `uninstall` on the old version's lockfile (or delete `apm.lock.yaml`) then
  `install` fresh.
- **Discovery-layer** (agy actually loading a workspace bundle in an
  interactive/`--print` session) is not re-verified by this task; only
  `agy plugin validate`'s static structural check is (§6 pattern item 2, now
  run directly against the real generated bundle rather than a hand-packaged
  copy — `evals/ab_antigravity.py`). §6 item 3's live-discovery pattern
  already covers LOCAL primitives' discoverability and is unaffected.

### 7.8 Test locks

`internal/deploy`: `TestRun_AntigravityBundlePaths`,
`TestRun_AntigravityLocalPathsUnchanged`,
`TestRun_AntigravityPluginManifestProvenance`,
`TestRun_AntigravityPluginManifestReinstall`, `TestBundleNameFromDepKey`,
`TestAntigravityBundleDir`, `TestRun_AntigravityBundleNameCollision`,
`TestRun_AntigravityTwoDependencyHooksIsolated`,
`TestRun_AntigravitySameDependencyHooksOverwriteDiagnostic`,
`TestRun_AgentSameNameCollision_FirstDeclaredWins` (antigravity subtest,
path updated to the bundle route). `cmd/apm`:
`TestRunUninstall_AntigravityBundleRemovedSiblingBundleSurvives`,
`TestRunUninstall_AntigravityBundleUserFileSurvives`,
`TestRunUninstall_AntigravityTamperedManifestKeptWithWarning`. Live/A-B:
`evals/ab_antigravity.py` (bundle section + live `agy plugin validate`
against the generated bundle).
