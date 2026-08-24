# 13 — `pack` success output + `--help` parity (pre-existing)

**What to build:** `apm-go pack`'s success output matches the Oracle's line set and wording, paths are printed relative to cwd as the Oracle prints them, and `pack --help` carries the Oracle's semantic flag set.

**Blocked by:** 10 — error/warning output contract (so channel/prefix are already aligned and only content remains).

**Status:** done (2026-08-24, attempt 2) — `.review/eval-ticket-13.md` FAILed attempt 1 on two findings. Finding 1 (help AC) was formally re-scoped to ticket 17 by the orchestrator (see that item below). Finding 2 (the usage-preamble hook's fallback branch introduced unverified message drift for every OTHER flag-parse error) is fixed in attempt 2 — see "Attempt 2" section below. Everything else attempt 1 verified (success lines, `displayPath`, `errorBody` cases, the nine `pack-*` tuple improvements, zero regression elsewhere) stands unchanged.

**Origin:** runner cases from ticket 07 (`.review/eval-ticket-07.md` §7.3). Selector/refusal/lockfile/next-steps all PASS; these are pre-existing `pack` gaps the runner exposed.

**Oracle:** `commands/pack.py` (success path logging), `bundle/packer.py` (what is printed per file), `commands/pack.py:155-200` (help).

## Findings to close

- [x] **Success-output line set.** Verified directly against the pinned Oracle (`pack-no-flag`'s own fixture, side by side): default verbosity prints exactly `[*] Packed N file(s) -> <path>`, `[i] Claude plugin bundle ready -- contains plugin.json plus plugin-native directories and an embedded apm.lock.yaml.`, `[i] Share with: apm install <path>` — no per-file listing. `pack.py`'s `_render_bundle_result` gates the real (non-dry-run) file list behind `logger.verbose_detail` (verbose-only); `logger.tree_item` a few lines up (dry-run's own listing) is unconditional and was already correct in apm-go. Fixed:
  - `ux.Success` renders `+ ` (colors.go's centered-symbol convention), but `logger.success()`'s OWN default symbol is `"sparkles"` -> `[*]`, not `[+]` (that's `symbol="check"`, used by e.g. `marketplace validate`'s per-check "passed" lines, a DIFFERENT call site). Added `ux.Sparkle` (`internal/ux/printer.go`) for the `"[*]"` glyph family (sparkles/gear/cogs), used ONLY at the two `pack.go` call sites verified to need it (bundle producer's and marketplace producer's `logger.success(...)` calls, both no-symbol-override) — deliberately NOT a blanket swap of every other `ux.Success` call site (30+ others across the CLI, none individually verified against their own Oracle symbol; a cross-cutting audit is its own, separate finding if ever needed).
  - The real-run per-file listing (`ux.BulletList`) is now gated on `opts.verbose`, matching `logger.verbose_detail`'s own gate. `TestRunPack_DependenciesOnly_ListsPackedFiles` split into `..._DefaultVerbosityOmitsFileList` and `..._Verbose_ListsPackedFiles`.
  - Wording fixed to the Oracle's exact CLAUDE_PLUGIN-branch text (`_render_bundle_result`'s `elif fmt == BundleFormat.CLAUDE_PLUGIN:` string) -- the ONLY branch reachable through `apm-go pack` today, since Agent Plugin and legacy APM formats are refused before this code ever runs (`bundle_format.go`'s own comment; `pack-refuse-agent-plugin`/`pack-refuse-apm`).
  - Marketplace producer's `ux.Success` call (`packOneOutput`) also switched to `ux.Sparkle` -- same Oracle default-symbol call site (`_render_marketplace_result`'s `logger.success(f"Built {message}")`).
- [x] **Relative output paths.** `bundle.Produce`'s `ProduceResult.BundleDir` is intentionally absolute (used internally for real filesystem writes) -- added `displayPath` (`pack.go`), used ONLY at the print call sites (success line, share-with line), computing `filepath.Rel(cwd, abs)` with a same-value fallback on error. The marketplace producer's own success line was already using the relative `outputPath`, not an absolute one -- no change needed there beyond the symbol swap above.
- [x] **Usage-error boilerplate — decided or, per each row's OWN verified Oracle behavior, YES with one exception.** Recorded in ticket 10's file (`.scratch/parity-runner/issues/10-error-output-contract.md`) as a new, dated decision addendum: apm-go mirrors Click's `UsageError.show()` contract for a genuine CLI-usage mistake -- `Usage: <cmd> [flags]\nTry '<cmd> --help' for help.\n\nError: <message>`, on **stderr**, plain `Error: ` prefix -- a narrower, separate contract from decision (A)'s stdout/`[x]` rule for ordinary runtime errors, since Click's own exception-handling layer (used for `click.UsageError`/`ClickException`, completely bypassing the Oracle's custom Console/`_rich_error`) never goes through the code path decision (A) analyzed. Verified directly, ALL FOUR of pack's format-selector error shapes, with one real exception found along the way: the "flag needs an argument" case (`pack --format` with no value) prints the bare `Error: Option '--format' requires an argument.` line with **no** Usage/Try-help preamble at all on the Oracle side (Click raises this before a `Context` exists to render `get_usage()` from) -- a genuine, narrower Click behavior, not an inconsistency to paper over.
  - Implemented at ONE shared location: `main()`'s `root.Execute()` error branch (now `root.ExecuteC()`, to get the failing `*cobra.Command` for `UseLine()`/`CommandPath()`). New `withUsageError`/`withBareUsageError` wrappers in `exitcode.go` (a `usage`/`bare` pair of bits on the existing `exitCodeError`), applied ONLY at the verified call sites: the shared `--format`/`--claude-plugin` selector (`bundle_format.go`'s `resolveBundleFormat` error and `setBundleFormatFlagErrorFunc`'s flag-error callback -- used by BOTH `pack` and `plugin init`, so both get this fix from the one shared helper) and `plugin.go`'s own resolve-format call site (already cited `click.UsageError` in its own comment). Deliberately NOT retrofitted onto every OTHER existing `withExitCode(2, ...)` call site across the CLI (`audit --content`'s warning-count gate, `compile`'s target-not-implemented message, `install`'s structured no-deploy-target teaching block, `marketplace package add/set/remove`'s mkt-045 edit-failure convention) -- surveyed each; none cite an observed Oracle `click.UsageError` for that specific operation, several already print their own error text directly (would double-print), and `install.go`'s own comment explicitly says its no-deploy-target block is "not a flag/argument mistake". Retrofitting those without verifying each against the live Oracle first would be guessing, not fixing.
  - `tools/parity/diff.go`'s `errorBody` extractor updated: it used to take the first non-empty line unconditionally, which -- once apm-go started printing the Usage preamble -- would compare the preamble's OWN Cobra-vs-Click wording ("apm-go pack [flags]" vs "apm pack [OPTIONS]") instead of the actual error message, defeating error_body's whole purpose (verify the real message matches independent of channel/prefix cosmetics). `isUsagePreambleLine` now makes it skip "Usage: ..." and "Try '...' for help." lines and continue to the real `Error: ...` line. `TestErrorBody_TableDriven` gained three cases (full preamble, Cobra-spelled preamble, the bare no-preamble case).
- [~] **`pack --help` semantic parity — FORMALLY RE-SCOPED to ticket 17 (orchestrator, 2026-08-24, post eval-ticket-13 finding 1).** The evaluator correctly ruled the original spin-out invalid under the Scope rule (this was a written AC, not an out-of-scope finding). Re-scope rationale, on the record: the AC as written rested on a false factual premise — eval-ticket-07 §7.3 characterized the gap as help WORDING drift, but the live diff shows nine entirely missing flags, several being unimplemented FEATURES (archive creation, JSON mode, release gates), two of which (`--check-versions`/`--check-clean`) design.md already records as deliberate deferrals with a standing test (`TestPackCmd_DoesNotExposeDeferredFlags`). Implementing six-plus features is not "help parity" and would block the beta.3 line for weeks. Ticket 13's help AC is therefore re-scoped to: `--format`'s description carries ONLY the ticket-07-sanctioned trailing sentence (verified true), and the full flag/feature backlog is ticket 17's charter with `pack-help` remaining an HONEST unwaived `stdout,help_semantic` diff until 17 lands (no waiver added — the gap stays visible in every corpus run). **USER AUTHORIZATION (2026-08-24): the user explicitly approved this re-scope** (options presented: approve / veto-keep-in-13 / partial; user selected approve). The help/flag feature backlog is ticket 17's charter; ticket 13 closes on its remaining written findings.
  (original spin-out note follows)** Verified directly (`diff/pack-help.json`, both sides' live `--help`): the finding as written ("empty except the ticket-07 `--format` sentence") was WRONG -- apm-go is missing NINE flags entirely (`-o`/`--output`, `--archive`, `--archive-format`, `-t`/`--target`, `--check-versions`, `--check-clean`, `--json`, `--legacy-skill-paths`, plus a completely different `Long` description paragraph), several representing real unimplemented features (archive creation, a JSON output mode, release gates), not wording drift. `TestPackCmd_DoesNotExposeDeferredFlags` already independently confirms `check-versions`/`check-clean` are a KNOWN, pre-existing, intentional deferral (design.md), corroborating this isn't a new regression -- just a bigger, already-somewhat-tracked gap than this ticket's own finding assumed. Per the Scope rule, recorded as `.scratch/parity-runner/issues/17-pack-flag-parity.md` rather than implementing 6+ new features inline. `--format`'s own description IS correctly just the Oracle's wording plus the one ticket-07-sanctioned trailing sentence -- that part of the original finding was right.
- [x] **Runner evidence.** All 12 `pack-*` cases: `pack-format-missing-arg` is now a perfect clean match (no diff at all). Five bundle-success cases (`pack-no-flag`, `pack-claude-plugin-flag`, `pack-format-claude`, `pack-format-claude-plugin`, `pack-format-plugin`) carry one new field-precise `stdout` F01 waiver each (the Oracle's Rich console word-wraps the "Claude plugin bundle ready..." line at terminal width; apm-go doesn't). Three usage-error cases (`pack-format-conflict`, `pack-format-empty`, `pack-format-unknown`) carry one new field-precise `stderr` F01 waiver each (the Usage line's own "apm pack [OPTIONS]" vs "apm-go pack [flags]" spelling, same category as `--help`'s "Options:"/"Flags:" convention) -- `error_body` on all three now shows NO diff (the real error text matches byte-for-byte). `pack-help` is deliberately left UNWAIVED on `stdout`/`help_semantic` -- a real, tracked gap (ticket 17), not a rendering artifact. `pack-refuse-agent-plugin`/`pack-refuse-apm` unchanged (still the two pre-existing, already-waived exporter-not-implemented refusals). No ticket-12 `tree` paths appear on any pack-* case beyond what was already true before this ticket.

## Attempt 2: constrain the usage-preamble hook to verified selector errors

`.review/eval-ticket-13.md`'s attempt-1 finding 2: `setBundleFormatFlagErrorFunc`'s
FALLBACK branch wrapped `err` in `withUsageError(err)` for absolutely ANY
cobra flag-parse error on `pack`/`plugin init`, not just the verified
`--format`/`--claude-plugin` cases — so an unrelated mistake like `pack
--bogus` got the Usage/Try-help preamble treatment based on cobra's own
raw error text, with no Oracle verification that the preamble (or the
message itself) was even correct there. Probed directly:

- `pack --bogus` on the pinned Oracle: `No such option: --bogus Did you
  mean --verbose?` — Click's own "did you mean" suggestion machinery,
  nothing like cobra's `unknown flag: --bogus`.
- `pack -m` (a shorthand missing its argument): the Oracle says `Option
  '-m' requires an argument.`; apm-go's EXISTING (pre-ticket-13) "flag
  needs an argument" reformatting — unconditional, not gated on the flag
  name — already produced the wrong, garbled `Option ''m' in -m' requires
  an argument.` before this ticket ever touched the file. Ticket 13
  attempt 1 additionally wrapped this in the Usage preamble via the SAME
  unverified fallback path this attempt fixes.

Fixed by narrowing `setBundleFormatFlagErrorFunc` to gate the
preamble/bare-usage-error treatment on the exact verified flag name
(`"--format"` only): a missing `--format` argument still gets
`withBareUsageError` (unchanged, correct); every OTHER "flag needs an
argument" case keeps the pre-existing (still-buggy, NOT this ticket's job
to fix) reformatted message but reverts to plain `withExitCode(2, ...)`
(no preamble, `ux.Error`/`"[x] "` on stdout); every non-"flag needs an
argument" flag-parse error (unknown flag, etc.) also reverts to plain
`withExitCode(2, err)` with cobra's raw, untouched message. Verified
byte-for-byte against a binary built from `843bbc5` (f3ff78f's parent,
the last pre-ticket-13 commit) for `pack --bogus`, `pack -m`, and `plugin
init --bogus`: all three now match EXACTLY, including the pre-existing
`-m` wording bug (deliberately not fixed here — that's ticket 17's "did
you mean"/missing-argument-wording backlog item, added in this attempt).
`plugin init`'s own four selector-error shapes (conflict/empty/unknown/
missing-arg) re-verified directly against the Oracle too — all four
still carry the correct, unchanged preamble shapes (its own 4-choice list
renders correctly since `coerceBundleFormat`'s message is built from the
caller's own `choices` slice).

New regression test `TestRootError_UnverifiedFlagErrorsMatchPreTicket13Shape`
(`cmd/apm-go/root_error_test.go`) locks down the reverted shape for all
three probed cases; `TestRootError_VerifiedFormatSelectorErrorsKeepPreambleShape`
locks down that the verified `--format` cases still render correctly
through the same code path. Both exercise `main()`'s actual error-
rendering logic (extracted into a new `renderRootError`/`buildRootCmd`
pair specifically so this is testable via the existing `captureStdout`/
`captureStderr` helpers, without spawning a real subprocess).

## Evidence

Attempt 1: fresh full-corpus run against the pinned Oracle, clean tree at that commit, five-point provenance verified, diffed against `/tmp/ticket11a8-evidence-d5f349c-exact` (zero regressions outside the `pack-*` ids this ticket touched).

Attempt 2: fresh full-corpus run against the pinned Oracle, clean tree at this attempt's commit, five-point provenance verified, diffed against `/tmp/ticket13-evidence-f3ff78f-cleanoracle2` (the evaluator's own attempt-1 baseline): `pack-format-missing-arg` still a perfect clean match, the three selector rows (`pack-format-conflict`/`-empty`/`-unknown`) still `stderr`-only waived, zero `(fields, waived)` drift anywhere else. `go test ./...` and `-race` on `cmd/apm-go`, `internal/ux`, `tools/parity` green (pre-existing Windows-only `internal/manifest:TestParseDepString_AbsolutePath` excepted).

## Files touched

### Attempt 1
- `internal/ux/printer.go`: `oracleSparklePrefix`, `Sparkle` (new).
- `cmd/apm-go/pack.go`: `runBundleProducer`'s success/file-listing/wording/share-with lines; `packOneOutput`'s success line; `displayPath` (new); `resolveBundleFormat`'s error wrapped via `withUsageError`.
- `cmd/apm-go/bundle_format.go`: `setBundleFormatFlagErrorFunc`'s two branches now `withBareUsageError`/`withUsageError`.
- `cmd/apm-go/plugin.go`: `resolvePluginFormat`'s error wrapped via `withUsageError` (same shared selector as pack).
- `cmd/apm-go/exitcode.go`: `exitCodeError.usage`/`.bare` fields, `withUsageError`, `withBareUsageError`, `isUsageError`, `isBareUsageError` (new).
- `cmd/apm-go/main.go`: `root.Execute()` -> `root.ExecuteC()`; error branch renders the Usage/Try-help/Error: block for a usage error instead of `ux.Error`.
- `cmd/apm-go/pack_test.go`: `TestRunPack_DependenciesOnly_ListsPackedFiles` split into default/verbose variants; +`TestDisplayPath`.
- `cmd/apm-go/exitcode_test.go`: +`TestUsageError`, `TestWithExitCode_NilErrPassesThrough` extended.
- `tools/parity/diff.go`: `errorBody`/`isUsagePreambleLine` (new) skip a Usage-preamble line before extracting the real error body.
- `tools/parity/diff_test.go`: `TestErrorBody_TableDriven` +3 cases.
- `tools/parity/waivers.json`: +8 entries (`pack-no-flag`, `pack-claude-plugin-flag`, `pack-format-claude`, `pack-format-claude-plugin`, `pack-format-plugin`, `pack-format-conflict`, `pack-format-empty`, `pack-format-unknown`).
- `tools/parity/main_test.go`: `TestRealWaiversJSON_ValidatesAgainstPin`'s `wantIDs` extended with the 8 new ids.
- `.scratch/parity-runner/issues/10-error-output-contract.md`: new dated decision addendum for the Usage-error boilerplate.
- `.scratch/parity-runner/issues/17-pack-flag-parity.md` (new): the spun-out `--help` flag/feature gap.

### Attempt 2
- `cmd/apm-go/bundle_format.go`: `setBundleFormatFlagErrorFunc` narrowed -- the preamble/bare treatment now gates on `name == "--format"`; every other flag-parse error (including any other "flag needs an argument" case) reverts to plain `withExitCode(2, ...)`.
- `cmd/apm-go/main.go`: `buildRootCmd`/`renderRootError` extracted from `main()` (same logic, now independently testable).
- `cmd/apm-go/root_error_test.go` (new): `TestRootError_UnverifiedFlagErrorsMatchPreTicket13Shape`, `TestRootError_VerifiedFormatSelectorErrorsKeepPreambleShape`.
- `.scratch/parity-runner/issues/17-pack-flag-parity.md`: +note that unknown-flag "did you mean" wording and per-flag missing-argument wording are part of ticket 17's Click-parity backlog.
