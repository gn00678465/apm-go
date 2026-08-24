# 19 — `init`/`plugin init` output-surface gaps (spun out of ticket 09)

**What to build:** Three real, pre-existing (not caused by ticket 09) findings surfaced while backfilling `plugin init` runner evidence, each independently waived on every affected case rather than fixed inline, per the Scope rule.

**Blocked by:** none. **Status:** open (backlog, non-blocking).

**Origin:** ticket 09 (plugin init evidence backfill). Ticket 09's own acceptance criteria are about capturing existing behavior as runner evidence, not fixing wording/rendering/tooling gaps found along the way — this ticket records them instead.

## Findings

### 1. `apm.yml`'s YAML-serialization cosmetics diverge from the Oracle's PyYAML output

Two independent, purely-cosmetic differences, both verified live for `plugin init` (and presumably `init` too, same shared `runInitCore`/`yamlcore.SafeDump` path):

- The `# Which agent platforms to deploy to...` comment block's wording differs entirely (apm-go's is a 4-line block naming `--target`/field/auto-detect resolution order and the accepted target values; the Oracle's is a 2-line block with a commented-out example list).
- A non-ASCII `author` value gets rendered differently: the Oracle writes it as a bare, literal-UTF-8 scalar (`author: 名😀<`); apm-go's YAML emitter (`go.yaml.in/yaml`) double-quotes it with `\U`-escapes (`author: "名\U0001F600<"`). Both are valid, semantically-identical YAML.

Every `plugin-init-*` runner case that writes `apm.yml` (11 of them) carries a `tree_paths: ["cwd/plugin-init-fixture/apm.yml"]` waiver for this — `plugin.json`/`mcp.json` are unaffected and compared byte-exact.

### 2. `init`/`plugin init`'s plain (non-interactive) success path writes to the wrong stream relative to the Oracle

The Oracle's `logger.success(...)`/`logger.progress(...)` (commands/init.py:291 and around) always render via `_get_console()`, which defaults to stdout — the entire "APM project initialized successfully!" + "Next Steps" panel lands on stdout, single-stream.

apm-go's `runInitCore` (`cmd/apm-go/init.go`, Phase 7) splits: `ux.Info(os.Stderr, ...)` (the three next-steps bullet lines) correctly redirects to stdout via `oracleLine`'s `errWriter`, but `ux.Success(os.Stderr, ...)` and `ux.Section(os.Stderr, "Next steps")` do NOT — `Success`/`Section` don't route through `oracleLine`, so they stay on stderr. The two leading/trailing blank `fmt.Fprintln(os.Stderr)` calls also stay on stderr. Net effect: apm-go's success title and "Next steps" header print on stderr while its own bullet lines print on stdout — a channel split the Oracle's single-stream output never has.

**This is NOT simply a bug to fix on sight**: `cmd/apm-go/init_clack_test.go`'s own `captureStderr` doc comment states "init writes its human-facing output straight to os.Stderr (the stream contract in terminal-ux-contract §3)" — a deliberate, pre-existing apm-go design decision (the referenced doc is not currently in the tree to consult). Revisiting it is a real design question — does apm-go's `init`/`plugin init` output contract change to match the Oracle's single-stdout-stream behavior, given ticket 10's own established "errors/warnings land on stdout" precedent might reasonably extend to success output too — not a ticket-09-sized fix.

Every `plugin-init-*` success/existing-*-yes/normalise-upper case (11 of them) carries a `stdout`+`stderr` waiver for this, alongside finding 1's `tree` waiver.

### 3. The runner's own `help_semantic` parser doesn't handle multi-line-wrapped flag descriptions

`tools/parity/help_semantic.go`'s `parseHelpFlags` scans line by line; when Click or Cobra wraps a flag's description across 2+ physical lines (e.g. a flag with a long choice-list metavar, like `--format`, whose description text starts on a continuation line with no leading `-x, --flag` prefix), only the FIRST physical line's fragment is captured — the flag's description is silently truncated mid-sentence, or (when the flag name and its description start on genuinely separate lines, as `--format`'s long metavar forces) the flag is dropped from the parsed set entirely.

Not new to ticket 09: `pack-help`'s own (already unwaived, ticket-17-tracked) `help_semantic` diff shows the identical truncation pattern on its own long flag descriptions — this is a pre-existing, cross-cutting runner-tooling limitation, not a per-command product gap. `plugin-init-help` carries a `stdout`+`help_semantic` waiver citing this directly.

## Acceptance criteria

- [ ] Finding 1: decide whether to match the Oracle's comment wording / bare-UTF-8 author quoting exactly, or record a permanent, dated deviation (style: `search.go`'s hint-text comment). Update `waivers.json` accordingly if closed.
- [ ] Finding 2: decide `init`/`plugin init`'s success-output stream contract — match the Oracle's single-stdout-stream behavior (revisit `terminal-ux-contract §3`, if recoverable, or make a fresh decision), or keep the current stderr-for-chrome design and record it as a permanent, cited deviation. If changed, `cmd/apm-go/init_clack_test.go` and every other test relying on `captureStderr` for init's plain-path output need updating in the same commit.
- [ ] Finding 3: fix `parseHelpFlags` (or replace it) to handle a flag description that spans multiple physical lines, for both Click's and Cobra's wrapping conventions. Add a case-based regression fixture (a flag with a description long enough to wrap under both frameworks). Re-verify `pack-help`'s and `plugin-init-help`'s `help_semantic` diffs shrink to genuine content gaps only (if any remain).
- [ ] Fresh corpus evidence for whichever findings are closed: the affected cases' waivers drop the closed field(s), zero `(fields, waived)` regression elsewhere.
