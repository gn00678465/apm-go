# 19 — `init`/`plugin init` output-surface gaps (spun out of ticket 09)

**What to build:** Three real, pre-existing (not caused by ticket 09) findings surfaced while backfilling `plugin init` runner evidence, each independently waived on every affected case rather than fixed inline, per the Scope rule.

**Blocked by:** none. **Status:** CLOSED (2026-08-28).

**Origin:** ticket 09 (plugin init evidence backfill). Ticket 09's own acceptance criteria are about capturing existing behavior as runner evidence, not fixing wording/rendering/tooling gaps found along the way — this ticket records them instead.

## Findings

### 1. `apm.yml`'s YAML-serialization cosmetics diverge from the Oracle's PyYAML output

Two independent, purely-cosmetic differences, both verified live for `plugin init` (and `init` too, through the shared `runInitCore`/`yamlcore.SafeDumpManifest` path):

- The `# Which agent platforms to deploy to...` comment block's wording differs entirely (apm-go's was a 4-line block naming `--target`/field/auto-detect resolution order and the accepted target values; the Oracle's no-target form is the exact four-line commented skeleton with `copilot`/`claude` examples, while its selected-target form is the exact three-line header followed by an indentless sequence).
- A non-ASCII `author` value was rendered differently: the Oracle writes it as a bare, literal-UTF-8 scalar (`author: 名😀<`); apm-go's YAML emitter (`go.yaml.in/yaml`) used to double-quote it with `\U`-escapes (`author: "名\U0001F600<"`). Both are valid, semantically-identical YAML; the emitter now matches the Oracle.

Before this verifier, every `plugin-init-*` runner case that writes `apm.yml` (11 of them) carried a `tree_paths: ["cwd/plugin-init-fixture/apm.yml"]` waiver for this. The waiver is now removed from all 11 cases; `plugin.json`/`mcp.json` remain compared byte-exact.

### 2. `init`/`plugin init`'s success output now matches the Oracle's channel and content contract

This finding is closed. The pinned Oracle's `logger.success(...)`, created-files table, next-steps panel, conditional agentrc guidance, and Docs/Star footer all render through its default stdout console (commands/init.py:291-400). Its success title is the shared `APM project initialized successfully!` string for both consumer and plugin initialization, including the `plugin init` wrapper.

apm-go's `runInitCore` (`cmd/apm-go/init.go`, Phase 7) now renders the same success block on stdout for both plain and interactive paths: progress records, success title, Created Files table with Oracle `*` markers and headers, mode-specific next steps, agentrc branch/tip, `.codex` tip, and footer. Successful invocations emit zero bytes on stderr. The old `terminal-ux-contract §3` citation in `cmd/apm-go/init_clack_test.go` was retired because it described the superseded stderr-only chrome design; the capture helper remains for interactive transcript and other stderr assertions.

The remaining `stdout` waivers on `init-yes` and the 11 plugin-init success variants are limited to Rich-versus-ux presentation: box-drawing style, padding/alignment, and terminal wrapping. A mechanical proof over fresh raw corpus evidence strips those rendering characters, folds wrapped lines, and normalizes the sanctioned `apm`/`apm-go` binary spelling; all 12 cases then have identical token sequences. No stderr tuple remains waived for this finding.

The one-off `init --name x` surface is also closed: both `init --name x` and `plugin init --name x` now return exit 2, empty stdout, and the exact Click UsageError block on stderr (`Usage: apm ... [OPTIONS] [PROJECT_NAME]`, `Try ... --help`, `Error: No such option: --name`). The scoped parser hook applies only to these init commands; unrelated `pack` flag behavior remains unchanged.

### 3. The runner's own `help_semantic` parser doesn't handle multi-line-wrapped flag descriptions

`tools/parity/help_semantic.go`'s `parseHelpFlags` scans line by line; when Click or Cobra wraps a flag's description across 2+ physical lines (e.g. a flag with a long choice-list metavar, like `--format`, whose description text starts on a continuation line with no leading `-x, --flag` prefix), only the FIRST physical line's fragment is captured — the flag's description is silently truncated mid-sentence, or (when the flag name and its description start on genuinely separate lines, as `--format`'s long metavar forces) the flag is dropped from the parsed set entirely.

Not new to ticket 09: `pack-help`'s own (already unwaived, ticket-17-tracked) `help_semantic` diff shows the identical truncation pattern on its own long flag descriptions — this is a pre-existing, cross-cutting runner-tooling limitation, not a per-command product gap. `plugin-init-help` carries a `stdout`+`help_semantic` waiver citing this directly.

## Acceptance criteria

- [x] Finding 1: match the Oracle's comment wording, target-list serialization, blank-line placement, and bare-UTF-8 author quoting exactly. The 11 plugin-init tree waivers were removed.
- [x] Finding 2: align `init`/`plugin init` success output to stdout, update the dependent capture tests, remove stderr from the success waivers, and match the verified unknown-flag UsageError surface with exit 2.
- [x] Finding 3: `parseHelpFlags` now joins indented continuation lines for Click and Cobra output, including Click's long-metavar `--format` shape. Real `apm pack --help` and `apm-go pack --help` excerpts cover wrapped `--archive`, `--check-clean`, and `--format` descriptions. Reverification leaves `pack-help` with only the sanctioned apm-go-only `--format` wording difference; `plugin-init-help` has no `help_semantic` difference.
- [x] Fresh corpus evidence: after the parser fix, `plugin-init-help`'s waiver drops `help_semantic` and retains only `stdout`; the corpus now has 80 cases (the verifier added `init-yes` and `init-unknown-flag`) and zero unwaived differences. Finding 2's 12 success tuples dropped `stderr`; the new unknown-flag case carries a `stderr`-only waiver (Click-vs-Cobra usage layout).

## Evidence

The parser regression test is `go test ./tools/parity`; the manifest conformance and SSH row tests are `go test ./internal/manifest`. The corpus command was rerun with output under `/tmp/parity-verifier-2-afterparser`; its `pack-help` semantic diff contains only the expected `--format` wording, while `plugin-init-help` has no semantic diff.

### Finding 2 closure evidence (verifier brief 5, 2026-08-28)

- Oracle source inspection pinned the success sequence to `src/apm_cli/commands/init.py:168-174, 249-305, 316-400`, the shared plugin wrapper to `src/apm_cli/commands/plugin/init.py:22-73`, and the default stdout `CommandLogger` console path. Live probes covered consumer named/current-directory/existing-file cases, plugin default/Claude/Agent formats, overwrite warnings, agentrc installed/absent, existing instruction files, and `.codex` tips. The pinned Oracle emits `Created project directory` for a named project directory even when `mkdir(exist_ok=True)` found it already present; apm-go preserves that observed Oracle behavior.
- `cmd/apm-go/init.go` now uses one stdout success renderer for both modes and the interactive final block, with Oracle file-table rows/headers, mode-specific panel text, conditional agentrc guidance, footer, and empty stderr. `os/exec.LookPath` is used only to mirror the Oracle's `shutil.which("agentrc")` branch; no external program is executed.
- `cmd/apm-go/init_clack_test.go`, `doctor_test.go`, and `root_error_test.go` were updated for the retired stderr citation and stdout-only success contract. `cmd/apm-go/init_output_test.go` covers consumer/plugin/Agent output, all agentrc/instruction branches, interactive final output, and stderr emptiness. `TestRootError_InitUnknownFlagMatchesOracle` covers both exact UsageError transcripts.
- The corpus added `tools/parity/cases/init-unknown-flag/case.json` with `expected_taxonomy: ["F01", "F08"]`. The 12 success waivers (`init-yes` plus 11 plugin-init success cases) changed from `stdout, stderr` to `stdout`; every reason now records the narrowed Rich/ux presentation waiver and ticket-19 reference. The final run is `/tmp/parity-verifier-5-final3.JfLzwh`: 80 cases, 73 cases with diffs all covered by existing scoped waivers, zero unwaived differing fields. Fresh evidence showed `stderr=0/0` for all 12 success variants, semantic token equality after mechanical rendering/binary-name normalization, and, for the unknown-flag case, identical `Try ... --help` and `Error: No such option: --name` lines after the binary-name fold.

**Orchestrator review (2026-08-28):** the verifier's first cut hardcoded the Oracle's `Usage: apm init [OPTIONS] [PROJECT_NAME]` spelling per command (`oracleInitUnknownFlagUsage`). Rejected: apm-go prints its own name via cobra's `UseLine`/`CommandPath` and the runner folds it; the Click-vs-Cobra usage layout is a waived rendering class, handled exactly like `plugin-init-unknown`. The helper was removed, `init-unknown-flag` got a `stderr`-only waiver, and the `Error:` line remains byte-identical.
- Global applicability was checked: `pack --name x` retains its established pre-ticket-13 plain error contract; only `init` and `plugin init` receive the new Click-style unknown-option adapter.

### Finding 1 closure evidence (verifier brief 4, 2026-08-28)

- Oracle source inspection pinned the template and emitter to `src/apm_cli/commands/_helpers.py:665-737` and `src/apm_cli/utils/yaml_io.py:28-32` (`yaml.safe_dump(..., sort_keys=False, allow_unicode=True)`), with the accepted target catalog from `src/apm_cli/core/target_catalog.py:258-264`.
- Live Oracle probes for zero, one, and several detected targets established the exact bytes: the no-target skeleton is `# Which agent platforms to deploy to (uncomment to pin):`, commented `targets:`, commented `copilot`/`claude` examples, then one blank line; selected targets use the three-line header, the sorted accepted values `agent-skills, antigravity, claude, codex, copilot, cursor, gemini, grok-build, kiro, opencode, windsurf`, and an indentless sequence. Both `init` and `plugin init` share this helper.
- Live scalar probes recorded Oracle bytes for `a: b`, `#x`, leading/trailing spaces, `yes`, `1.0`, empty, emoji-only, and `line\x01char`. `internal/yamlcore.SafeDumpManifest` now explicitly uses Unicode, single-quote, and compact-sequence settings, repairs go-yaml's astral-rune escape limitation, and `manifestStrNode` preserves PyYAML's legacy-boolean quoting. Exact byte tables are locked in `cmd/apm-go/manifestnode_test.go`.
- `internal/manifest.DetectTargets` now sorts the detected list to match the Oracle's `sorted(...)` resolution path. `go test ./cmd/apm-go ./internal/yamlcore ./internal/manifest` and `go test ./tools/parity` pass.
- The parity corpus grew from 78 to 79 with `init-yes` (`["init", "init-fixture", "--yes"]`). The 11 existing plugin-init waivers retain only `stdout`/`stderr`; the new case waives only `stdout`/`stderr` with Finding 2's missing success-output lines listed, leaving its `apm.yml` tree unwaived. Final pinned-Oracle parity reports 79 cases and 0 unwaived differences.
