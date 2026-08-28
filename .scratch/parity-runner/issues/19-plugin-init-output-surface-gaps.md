# 19 — `init`/`plugin init` output-surface gaps (spun out of ticket 09)

**What to build:** Three real, pre-existing (not caused by ticket 09) findings surfaced while backfilling `plugin init` runner evidence, each independently waived on every affected case rather than fixed inline, per the Scope rule.

**Blocked by:** none. **Status:** open (backlog, non-blocking).

**Origin:** ticket 09 (plugin init evidence backfill). Ticket 09's own acceptance criteria are about capturing existing behavior as runner evidence, not fixing wording/rendering/tooling gaps found along the way — this ticket records them instead.

## Findings

### 1. `apm.yml`'s YAML-serialization cosmetics diverge from the Oracle's PyYAML output

Two independent, purely-cosmetic differences, both verified live for `plugin init` (and `init` too, through the shared `runInitCore`/`yamlcore.SafeDumpManifest` path):

- The `# Which agent platforms to deploy to...` comment block's wording differs entirely (apm-go's was a 4-line block naming `--target`/field/auto-detect resolution order and the accepted target values; the Oracle's no-target form is the exact four-line commented skeleton with `copilot`/`claude` examples, while its selected-target form is the exact three-line header followed by an indentless sequence).
- A non-ASCII `author` value was rendered differently: the Oracle writes it as a bare, literal-UTF-8 scalar (`author: 名😀<`); apm-go's YAML emitter (`go.yaml.in/yaml`) used to double-quote it with `\U`-escapes (`author: "名\U0001F600<"`). Both are valid, semantically-identical YAML; the emitter now matches the Oracle.

Before this verifier, every `plugin-init-*` runner case that writes `apm.yml` (11 of them) carried a `tree_paths: ["cwd/plugin-init-fixture/apm.yml"]` waiver for this. The waiver is now removed from all 11 cases; `plugin.json`/`mcp.json` remain compared byte-exact.

### 2. `init`/`plugin init`'s plain (non-interactive) success path writes to the wrong stream relative to the Oracle

The Oracle's `logger.success(...)`/`logger.progress(...)` (commands/init.py:291 and around) always render via `_get_console()`, which defaults to stdout — the entire "APM project initialized successfully!" + "Next Steps" panel lands on stdout, single-stream.

apm-go's `runInitCore` (`cmd/apm-go/init.go`, Phase 7) splits: `ux.Info(os.Stderr, ...)` (the three next-steps bullet lines) correctly redirects to stdout via `oracleLine`'s `errWriter`, but `ux.Success(os.Stderr, ...)` and `ux.Section(os.Stderr, "Next steps")` do NOT — `Success`/`Section` don't route through `oracleLine`, so they stay on stderr. The two leading/trailing blank `fmt.Fprintln(os.Stderr)` calls also stay on stderr. Net effect: apm-go's success title and "Next steps" header print on stderr while its own bullet lines print on stdout — a channel split the Oracle's single-stream output never has.

**This is NOT simply a bug to fix on sight**: `cmd/apm-go/init_clack_test.go`'s own `captureStderr` doc comment states "init writes its human-facing output straight to os.Stderr (the stream contract in terminal-ux-contract §3)" — a deliberate, pre-existing apm-go design decision (the referenced doc is not currently in the tree to consult). Revisiting it is a real design question — does apm-go's `init`/`plugin init` output contract change to match the Oracle's single-stdout-stream behavior, given ticket 10's own established "errors/warnings land on stdout" precedent might reasonably extend to success output too — not a ticket-09-sized fix.

Every `plugin-init-*` success/existing-*-yes/normalise-upper case (11 of them) carries a `stdout`+`stderr` waiver for this; historically those sat alongside Finding 1's now-removed `tree` waiver.

### 3. The runner's own `help_semantic` parser doesn't handle multi-line-wrapped flag descriptions

`tools/parity/help_semantic.go`'s `parseHelpFlags` scans line by line; when Click or Cobra wraps a flag's description across 2+ physical lines (e.g. a flag with a long choice-list metavar, like `--format`, whose description text starts on a continuation line with no leading `-x, --flag` prefix), only the FIRST physical line's fragment is captured — the flag's description is silently truncated mid-sentence, or (when the flag name and its description start on genuinely separate lines, as `--format`'s long metavar forces) the flag is dropped from the parsed set entirely.

Not new to ticket 09: `pack-help`'s own (already unwaived, ticket-17-tracked) `help_semantic` diff shows the identical truncation pattern on its own long flag descriptions — this is a pre-existing, cross-cutting runner-tooling limitation, not a per-command product gap. `plugin-init-help` carries a `stdout`+`help_semantic` waiver citing this directly.

## Acceptance criteria

- [x] Finding 1: match the Oracle's comment wording, target-list serialization, blank-line placement, and bare-UTF-8 author quoting exactly. The 11 plugin-init tree waivers were removed; Finding 2's stdout/stderr waivers remain unchanged.
- [ ] Finding 2: decide `init`/`plugin init`'s success-output stream contract — match the Oracle's single-stdout-stream behavior (revisit `terminal-ux-contract §3`, if recoverable, or make a fresh decision), or keep the current stderr-for-chrome design and record it as a permanent, cited deviation. If changed, `cmd/apm-go/init_clack_test.go` and every other test relying on `captureStderr` for init's plain-path output need updating in the same commit.
- [x] Finding 3: `parseHelpFlags` now joins indented continuation lines for Click and Cobra output, including Click's long-metavar `--format` shape. Real `apm pack --help` and `apm-go pack --help` excerpts cover wrapped `--archive`, `--check-clean`, and `--format` descriptions. Reverification leaves `pack-help` with only the sanctioned apm-go-only `--format` wording difference; `plugin-init-help` has no `help_semantic` difference.
- [x] Fresh corpus evidence: after the parser fix, `plugin-init-help`'s waiver drops `help_semantic` and retains only `stdout`; the corpus now has 79 cases (the verifier added `init-yes`) and zero unwaived differences, with no other waiver tuple changes.

## Evidence

The parser regression test is `go test ./tools/parity`; the manifest conformance and SSH row tests are `go test ./internal/manifest`. The corpus command was rerun with output under `/tmp/parity-verifier-2-afterparser`; its `pack-help` semantic diff contains only the expected `--format` wording, while `plugin-init-help` has no semantic diff.

### Finding 1 closure evidence (verifier brief 4, 2026-08-28)

- Oracle source inspection pinned the template and emitter to `src/apm_cli/commands/_helpers.py:665-737` and `src/apm_cli/utils/yaml_io.py:28-32` (`yaml.safe_dump(..., sort_keys=False, allow_unicode=True)`), with the accepted target catalog from `src/apm_cli/core/target_catalog.py:258-264`.
- Live Oracle probes for zero, one, and several detected targets established the exact bytes: the no-target skeleton is `# Which agent platforms to deploy to (uncomment to pin):`, commented `targets:`, commented `copilot`/`claude` examples, then one blank line; selected targets use the three-line header, the sorted accepted values `agent-skills, antigravity, claude, codex, copilot, cursor, gemini, grok-build, kiro, opencode, windsurf`, and an indentless sequence. Both `init` and `plugin init` share this helper.
- Live scalar probes recorded Oracle bytes for `a: b`, `#x`, leading/trailing spaces, `yes`, `1.0`, empty, emoji-only, and `line\x01char`. `internal/yamlcore.SafeDumpManifest` now explicitly uses Unicode, single-quote, and compact-sequence settings, repairs go-yaml's astral-rune escape limitation, and `manifestStrNode` preserves PyYAML's legacy-boolean quoting. Exact byte tables are locked in `cmd/apm-go/manifestnode_test.go`.
- `internal/manifest.DetectTargets` now sorts the detected list to match the Oracle's `sorted(...)` resolution path. `go test ./cmd/apm-go ./internal/yamlcore ./internal/manifest` and `go test ./tools/parity` pass.
- The parity corpus grew from 78 to 79 with `init-yes` (`["init", "init-fixture", "--yes"]`). The 11 existing plugin-init waivers retain only `stdout`/`stderr`; the new case waives only `stdout`/`stderr` with Finding 2's missing success-output lines listed, leaving its `apm.yml` tree unwaived. Final pinned-Oracle parity reports 79 cases and 0 unwaived differences.
