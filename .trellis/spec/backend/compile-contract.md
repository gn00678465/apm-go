# Compile Contract (`apm-go compile`)

> Executable contract for `apm-go compile` — the minimal agents-family
> subset of the Python oracle's `apm compile`, established in task
> 07-11-agents-md-compile (branch `feat/marketplace-install`, commit
> `7cb779a`). Evidence:
> `apm_cli` 0.21.0 source (`compilation/`, `primitives/`,
> `commands/compile/cli.py`) + live scratch probes (2026-07-11,
> `uv run apm compile --single-agents --no-links --no-constitution -t
> antigravity`). See task `design.md`/`implement.md`/`research/findings.md`
> for the full decision record.
>
> **Language**: English.

---

## 1. What this is (and is not)

`apm-go compile` reads local + dependency `*.instructions.md` primitives
and compiles them into a **single project-root `AGENTS.md`**, for the three
target adapters whose Python `compile_family` is `"agents"`:
**antigravity, codex, opencode**. It is the smallest slice of the oracle's
`compile` surface that closes a real functional gap: codex and opencode
have no `TypeInstructions` support in `internal/deploy` at all (their
adapters never deploy instructions anywhere), and antigravity's adapter
only byte-copies instructions to `.agents/rules/`, never maintaining an
`AGENTS.md`. `apm-go compile` is the only way any of the three gets a
compiled `AGENTS.md` today.

**Non-goals (v1, all intentional — not "not yet implemented" bugs):**

- `distributed` layout (subdirectory `AGENTS.md` files via applyTo +
  filesystem-tree placement) — v1 is single-file only, matching the
  oracle's `--single-agents` legacy mode.
- `claude`/`gemini`/`vscode` compile families (`CLAUDE.md`, `GEMINI.md`,
  `.github/copilot-instructions.md`) — a `-t claude`/`-t copilot`-only
  resolution is a hard error (§3), not a silent no-op.
- constitution injection, markdown link resolution, `chatmode`, `--watch`,
  `--validate`, `--root`, `--clean`, `--dry-run`, `--no-dedup`.
- `managed_section` mode, **user scope** compile (out of scope per PRD).
- vscode-only `dedup` (instructions omitted from `AGENTS.md` when
  `.github/instructions/` already has them) — structurally impossible to
  need here: `can_dedup_agents_md_instructions` is `True` **only** for
  target `"vscode"` in the oracle (`target_detection.py`); antigravity/
  codex/opencode never dedup, so this subset needs zero dedup logic.

---

## 2. CLI surface

```
apm-go compile [-t|--target <name[,name...]>]
```

Only `-t/--target` is exposed. No `--dry-run`/`--watch`/`--validate`/
`--root`/`--single-agents`/`--no-links`/`--no-constitution`/`--clean`/
`--output` — all oracle-only flags, all documented non-goals (§1). An
unrecognized flag is Cobra's standard usage error, **exit 1**.

### Target resolution

Reuses `deploy.ResolveTargets` unchanged (flag > `apm.yml target:` >
filesystem auto-detection — `internal/deploy/adapter.go`), then filters to
the agents-family set `{antigravity, codex, opencode}`:

| Resolved (pre-filter) targets | agents-family subset | Result |
|---|---|---|
| at least one of `{antigravity, codex, opencode}` | non-empty | compile once, one `AGENTS.md` (§4) |
| non-empty, but **none** are agents-family (e.g. `claude`, `copilot`) | empty | `exit 2`, `compile for target(s) <names> not implemented in apm-go yet` |
| empty (no signal detected, or an unknown `-t` token) | empty | `exit 2`, `compile for target(s) none not implemented in apm-go yet` |

A `-t codex,opencode,antigravity` multi-target request compiles **once**
(one root `AGENTS.md`) — content is target-independent (§4 has no
per-target variation), so there is no "compile per target" loop to run.

**Zero-output-silent-success is deliberately rejected.** The oracle
produces `CLAUDE.md` for `-t claude`; silently exiting 0 with no file for
the same input in apm-go would be a worse user experience than a loud,
actionable `exit 2`. This is the one place v1 diverges from "match the
oracle's exit code" — recorded as a documented deviation (§6), asserted by
`evals/ab_agents_compile.py`.

### Project gate (checked in order, before target resolution)

1. No `apm.yml` in the working directory → stderr `Not an APM project - no
   apm.yml found`, **exit 1**. (Oracle: `commands/compile/cli.py:347-351`,
   identical message.)
2. `apm.yml` present, but no `apm_modules/` directory AND no
   `.apm/instructions/*.instructions.md` file → stderr `No instruction
   files found in .apm/ directory`, **exit 1**. (Simplified from the
   oracle's broader constitution/chatmode-aware gate,
   `cli.py:353-385` — v1 compiles neither.)

---

## 3. Instruction collection & priority

Reuses `internal/deploy`'s existing collection primitives — **does not**
duplicate or reimplement primitive discovery:

1. **Local**: `deploy.CollectLocalPrimitives(projectDir)`, filtered to
   `TypeInstructions` — `.apm/instructions/*.instructions.md` only (a
   plain `.md` file, or a directory literally named `*.instructions.md`,
   is never collected — pre-existing `deploy` behavior, `primitive.go`).
2. **Direct dependencies**, in `apm.yml` declaration order
   (`dependencies.apm` + `devDependencies.apm`, deduplicated by
   `deploy.DepRefKey`): `deploy.CollectDependencyPrimitives(key,
   apm_modules/<key>)`.
3. **Transitive dependencies**, sorted by `(RepoURL, VirtualPath)` from
   `apm.lock.yaml` (read directly — compile never re-resolves over the
   network; a missing/unreadable lockfile just means zero transitive
   deps, not an error).

Same-name conflicts resolve via the pre-existing, unmodified
`deploy.ResolvePrimitives` (req-pr-002/003): **local always wins**; among
non-local candidates, **first-declared wins**. This is the identical
priority engine `deploy.Run` (install) already uses — `apm-go compile`
does not introduce a second conflict-resolution scheme.

**Symlink defense in depth**: `deploy.CollectLocalPrimitives`/
`CollectDependencyPrimitives` do not themselves filter symlinks.
`internal/compile` adds its own `os.Lstat`-based filter (mirroring
`gitops.copyTreeNoSymlinks`'s established idiom) **before** conflict
resolution, so a symlinked `*.instructions.md` pointing outside the
source tree can never leak external file content into a compiled
`AGENTS.md`. Test lock: `TestCollectInstructions_IgnoresWrongSuffixAndSymlink`.

### Frontmatter: `applyTo` grouping key is the RAW string

`internal/compile` parses each instruction's `applyTo` value and keeps it
**verbatim** as the grouping key — it does **not** call `deploy`'s
`parseApplyTo` (the comma/brace-aware splitter `install`'s Claude-rules
conversion uses). `applyTo: "**/src/**, **/api/**"` is ONE heading
`` ## Files matching `**/src/**, **/api/**` ``, not two groups. A YAML
list value (flow `['**/*.py', '**/*.rb']` or block `- '**/*.py'\n  -
'**/*.rb'`) yields its **first non-null element** only (oracle:
`primitives/parser.py:95-119` `_normalize_apply_to`) — `**/*.py`, never
both. Test lock: `TestRender_RawApplyToCommaAndBrace`,
`TestParseInstruction_ApplyToScalarListAndNoFrontmatter`.

---

## 4. Output format (single-file mode)

```
# AGENTS.md
<!-- Generated by APM CLI from .apm/ primitives -->
<!-- Build ID: <12-hex> -->
<!-- APM Version: <version> -->

## Global Instructions

<!-- Source: <relpath> -->
<body, stripped>
<!-- End source: <relpath> -->

## Files matching `<pattern>`

<!-- Source: <relpath> -->
<body, stripped>
<!-- End source: <relpath> -->

---
*This file was generated by APM CLI. Do not edit manually.*
*To regenerate: `apm compile`*
```

- **Sections**: `## Global Instructions` (instructions with no `applyTo`)
  always first, then `## Files matching \`<pattern>\`` groups sorted
  lexically by the raw pattern string. Within a group, instructions sort
  by `<!-- Source: -->` relpath (lexical).
- **Orphan headings are a confirmed oracle quirk, mirrored intentionally**:
  a group's heading is emitted whenever **at least one** instruction
  belongs to it, even if *every* instruction in that group has an empty
  body — only the per-instruction `Source`/body/`End source` block is
  skipped for an empty body. Live-verified 2026-07-11: a project with
  exactly one no-op instruction (empty body, no `applyTo`) still produces
  `## Global Instructions` followed immediately by `---`, with nothing in
  between. Test lock: `TestRender_FiltersEmptyBodies` (subtests
  `orphan heading for all-empty ... group`).
- **relpath**: forward-slash, relative to the project root. Local:
  `.apm/instructions/x.instructions.md`. Dependency:
  `apm_modules/<key>/.apm/instructions/x.instructions.md` — live-verified
  against the oracle for a git-style key (`apm_modules/acme/dep/...`).
  **Known deviation for a *local-path* dependency** (`./dep-pkg` in
  `apm.yml`): the oracle's local scanner walks the ORIGINAL declared path
  (`dep-pkg/.apm/instructions/...`, a project-root sibling, not the
  staged `apm_modules/_local/dep-pkg/...` copy) — apm-go always shows the
  `apm_modules/_local/<name>/...` staged path instead, consistent with
  every other apm-go dependency-relpath convention. Not asserted in
  `evals/ab_agents_compile.py` (which uses a git-style dependency to stay
  on the oracle-matching path); recorded here so a future local-path A/B
  case does not misread it as a regression.
- **Line endings: apm-go emits LF only.** The oracle writes platform line
  endings — **CRLF on Windows** — apm-go intentionally never does,
  matching the project's pre-existing `instructions_claude.go` LF
  convention. `evals/ab_agents_compile.py` strips all `\r` bytes before
  comparing. Test lock: `TestRender_UTF8LFAndTrailingNewline`.
- **`APM Version`**: apm-go writes its own self-reported version
  (`"0.1.0"`, matching `cmd/apm/install.go`'s `newLock.APMVersion`), never
  the oracle's `apm_cli` version string. Since the version string feeds
  the Build ID hash (§5), the Build ID also necessarily differs between
  the two CLIs for byte-identical instruction content.
  `evals/ab_agents_compile.py` normalizes both the `APM Version` line and
  the `Build ID` line before comparing, and separately re-derives each
  side's own Build ID from its own content to check self-consistency.
  Test lock: `TestVersionLine_IsOnlyVersionSpecificTemplateDifference`.
- **`distributed`-mode markers never appear**: the header's `Generated by
  APM CLI from .apm/ primitives` line and per-instruction `Source`
  attribution are the **single-file** mode's contract (oracle:
  `template_builder.py:153-167,189-224`). The oracle's distributed mode
  (default when no `--single-agents`) uses a different header
  (`Generated by APM CLI from distributed .apm/ primitives`), places
  subdirectory `AGENTS.md` files by `applyTo` + filesystem-tree
  intersection, and defaults `Source` attribution OFF — none of that is
  in apm-go v1 (§1 non-goals); `evals/ab_agents_compile.py` pins the
  oracle to `--single-agents` so both sides stay comparable.

---

## 5. Build ID (idempotency without timestamps)

Algorithm (identical to oracle `compilation/build_id.py:22-39`):

1. Render content with a placeholder line, `<!-- Build ID: __BUILD_ID__
   -->`.
2. Split into lines (Python `str.splitlines()` semantics — no trailing
   empty element for content ending in `\n`; apm-go's
   `splitLinesLikePython` mirrors this exactly for LF-only content).
3. Remove the placeholder line (not blank it — the hash must never be
   self-referential); join the rest with `\n`; SHA256; keep the first 12
   hex chars.
4. Replace the placeholder line with `<!-- Build ID: <hash> -->`; preserve
   the original trailing newline.

Test lock: `TestBuildID_OracleAlgorithm` (byte-exact re-derivation,
idempotent re-stabilization, changed-content-changes-ID).

---

## 6. Write & idempotency

- If `AGENTS.md` already exists at the project root and its content is
  **byte-identical** to the freshly rendered + stabilized content, nothing
  is written — `mtime` and bytes are both unchanged. CLI prints `No
  changes detected; preserving existing AGENTS.md for idempotency`
  (matches the oracle's observable message, `cli.py:687-723`).
- Otherwise: temp file + atomic rename (`internal/compile/writer.go`,
  `atomicWrite` — a package variable so
  `TestWriteFile_AtomicFailurePreservesExisting` can inject a failing
  stub without OS-specific permission tricks). A failure at any point in
  the write leaves a pre-existing `AGENTS.md` byte-unchanged.
- **Overwrite policy matches the oracle's full mode: unconditional.**
  There is no hand-authored-file marker check before overwriting — the
  oracle's single-file mode has none either (marker protection only
  exists for `copilot-instructions.md` and the CLAUDE.md deletion path,
  neither of which apm-go compile touches). **This means `apm-go compile`
  will silently replace a hand-written project-root `AGENTS.md`** — by
  design, matching the oracle. A future, more conservative "refuse to
  write without a `Generated by APM CLI` marker" mode was considered and
  rejected for v1 (would diverge from the oracle and complicate A/B); it
  remains a candidate if the upstream project ever adds marker protection
  to its own full mode.
- **Test safety invariant**: this repo's own root `AGENTS.md` is
  hand-authored. Every test, live probe, and `evals/ab_agents_compile.py`
  run MUST use a fresh temp/scratch directory — `apm-go compile` is never
  invoked against this repo's own root.

---

## 7. Exit codes

| Code | Meaning |
|---|---|
| `0` | Compiled (wrote or idempotent no-op) |
| `1` | Project gate failure: no `apm.yml`, or no compilable content |
| `2` | No agents-family target resolved (unsupported-only target(s), unknown target token, or no signal) |

Cobra's own flag-parsing errors (unknown flag, `-t` with no value) exit
`1` before `RunE` ever runs — standard Cobra behavior, not compile-specific
code.

---

## 8. Documented deviations (summary)

| Area | apm-go | Oracle | Where asserted |
|---|---|---|---|
| unsupported-only target | `exit 2`, loud message | `exit 0`, emits `CLAUDE.md`/`GEMINI.md`/etc. | `evals/ab_agents_compile.py` (`-t claude` case) |
| line endings | LF only | CRLF on Windows | `evals/ab_agents_compile.py` normalize step; `TestRender_UTF8LFAndTrailingNewline` |
| `APM Version` / `Build ID` lines | apm-go's own version + derived hash | oracle's `apm_cli` version + derived hash | `evals/ab_agents_compile.py` normalize step; `TestVersionLine_IsOnlyVersionSpecificTemplateDifference` |
| local-path dependency relpath | `apm_modules/_local/<name>/...` (staged copy) | original declared sibling path | §4 note; not exercised by the A/B script (git-style dep used instead) |
| distributed layout, constitution, links, chatmode, watch/validate/root/clean, managed_section, vscode dedup, user scope | not implemented | implemented | §1 non-goals |

---

## See also

- [Antigravity Target Contract](./antigravity-target-contract.md) — the
  `antigravity` deploy adapter's own instruction handling
  (`.agents/rules/`, byte-copy, no `AGENTS.md` maintenance); `compile` is
  the only apm-go path that produces an `AGENTS.md` for antigravity/codex/
  opencode.
- [Install / Marketplace Contracts](./install-marketplace-contracts.md) —
  the shared primitive-collection, path-containment, and symlink-defense
  conventions `compile` reuses without modification.
- Task `07-11-agents-md-compile`: `prd.md`, `design.md`, `implement.md`,
  `research/findings.md`, `checklist.md`.
