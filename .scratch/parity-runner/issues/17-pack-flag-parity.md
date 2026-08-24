# 17 — `pack` flag/feature parity (spun out of ticket 13)

**What to build:** Implement the `apm pack` flags apm-go's `pack` command does not expose at all today, so `pack --help`'s `help_semantic` diff closes to just the ticket-07 `--format` description sentence.

**Blocked by:** nothing (each flag below is close to independent; `-o/--output` is the cheapest, `--archive`/`--check-versions`/`--check-clean` are real feature work). **Status:** open.

**Origin:** ticket 13 ("`pack` success output + `--help` parity"). That ticket's own written finding assumed `pack --help`'s `help_semantic` diff was empty except the ticket-07 `--format` deviation sentence. Verifying this directly (`diff/pack-help.json`, both sides' live `--help` output) found the assumption was wrong: apm-go is missing **nine** flags entirely, several representing real unimplemented features, not wording drift. Per the project's Scope rule (`.scratch/parity-runner/README.md`, added after ticket 11), a real finding outside a ticket's written acceptance criteria is spun out to its own ticket rather than chased inline — ticket 13 closes on its own three findings (success-output line set, relative paths, usage-error boilerplate) with this one recorded, not blocked on it.

## The finding

`apm-go pack --help` vs the pinned Oracle's `apm pack --help` (`commands/pack.py`'s `@click.option` decorators), verified directly:

| Flag | Oracle | apm-go | Note |
|---|---|---|---|
| `-o`, `--output PATH` | ✅ (default `./build`) | ❌ | `runBundleProducer` hardcodes `OutputDir: filepath.Join(".", "build")` — cheapest fix, no new feature, just wire a flag through. |
| `--archive` | ✅ | ❌ | Real feature: produce a `.zip` (or `.tar.gz`) instead of a directory. `internal/pack/bundle` has no archive-writing path at all yet. |
| `--archive-format [zip\|tar.gz]` | ✅ (default `zip`) | ❌ | Depends on `--archive` existing first. |
| `-t`, `--target TARGET` | ✅ (deprecated, informational-only metadata) | ❌ | Oracle docs it as already deprecated/ignored by install — low-risk to add as a pure metadata field once the plumbing exists. |
| `--check-versions` | ✅ | ❌ | Release gate: verify per-package versions against `marketplace.versioning.strategy`; exits 3 on misalignment. Real feature (`bundle/packer.py`'s version-alignment report). |
| `--check-clean` | ✅ | ❌ | Release gate: regenerate every marketplace output to a temp representation and diff against on-disk; exits 4 on drift. Real feature. |
| `--json` | ✅ | ❌ | Machine-readable JSON envelope to stdout, logs to stderr (`_emit_json_error_or_raise`, `BuildReport.failure_to_json_dict`). Touches every error/success path in `pack.py`. |
| `--legacy-skill-paths` | ✅ | ❌ | Deploy skill files to per-client paths instead of the shared `.agents/skills/` directory. |
| `--format`'s description | ✅ (Oracle's own wording) | ✅ (Oracle's wording + one ticket-07-sanctioned trailing sentence: "apm-go currently implements only the Claude plugin bundle; agent-plugin and apm are accepted but refused.") | The ONE deviation ticket 13's finding correctly named — already fine, not part of this ticket. |

Also: the pack command's `Long` description text is a completely different, apm-go-authored paragraph (`packCmd()`'s own three-bullet summary of the three producers) rather than the Oracle's `_PACK_HELP` docstring (usage examples, exit-code table). Matching this is cheap (pure text) but pointless to do in isolation while 8 flags are still missing — the `help_semantic` diff will stay large regardless until the flags above exist. Fold the description-text fix into whichever of the flag items above is tackled first, not as a standalone pass.

## Acceptance criteria

- [ ] Each flag above exists on `apm-go pack` with the Oracle's exact `--help` description text (verified via `help_semantic`), OR is deliberately deferred with a `known_gap`-style comment citing why (e.g. if `--json`'s envelope shape needs its own design pass).
- [ ] `pack --help`'s `help_semantic` diff is empty except the `--format` sentence ticket 07/13 already established as sanctioned.
- [ ] Each new flag has apm-go's own runner case(s) if it changes observable output (an `--archive` fixture producing a real `.zip`, a `--check-versions`/`--check-clean` fixture exercising the exit-3/4 gates, a `--json` fixture asserting the envelope shape).
- [ ] Fresh corpus evidence: zero `(fields, waived)` regression on the existing `pack-*` cases; new cases for whichever flags land.

## Suggested order (cheapest/most isolated first)

1. `-o`/`--output` (wire an existing hardcoded value through a flag; no new logic).
2. `-t`/`--target` (pure metadata field, Oracle already treats it as ignored/deprecated).
3. `--archive`/`--archive-format` (real feature, but self-contained: an archive-writing step after the existing directory bundle is built).
4. `--legacy-skill-paths` (a deploy-path variant, likely shares code with existing per-target skill placement logic).
5. `--check-versions`/`--check-clean` (release gates — depend on `marketplace.versioning.strategy` config and a temp-regenerate-and-diff mechanism; more design work).
6. `--json` (touches every success/error path in `pack.go`; do last so its envelope shape can capture whatever the other flags added).
