# 22 — `marketplace validate` output: wrong status symbols and wrong quote style

**What to build:** `marketplace validate` must use the Oracle's status symbols and quote style on its header line and its per-check result rows.

**Blocked by:** none. **Status:** CLOSED (2026-08-28). **Clears:** the `stdout`/`error_body` fields of `validate-checkrefs-off`, `validate-checkrefs-on`, `validate-structure-fail` (3 of the 18-case parity backlog).

**Origin:** orchestrator triage of the standing 18 unwaived parity cases, 2026-08-25. These are *not* rendering-library differences — they are apm-go emitting a different glyph and a different quote character from the same logical call, i.e. the ticket-10/13 output-contract family.

## The diffs (verbatim, from `parity-out/diff/`)

`validate-checkrefs-off`:

```
oracle: [*] Validating marketplace 'skills'...
target: [i] Validating marketplace "skills"...
        ^^^                        ^       ^
oracle: [+]   Structure: passed
target:  +    Structure: passed
        ^^^
```

(same for `Schema:` and `Names:` rows, and identically in `validate-checkrefs-on`.)

`validate-structure-fail` — header only; its `[x]   Structure: plugins: expected a list` row already matches:

```
oracle: [*] Validating marketplace 'broken'...
target: [i] Validating marketplace "broken"...
```

Note the `error_body` diff on `validate-structure-fail` is the *same* header line (the runner's error_body extractor takes the first line), so fixing the header fixes both fields.

## What the fix is

Both glyphs already exist in `internal/ux/printer.go`:

- `[*]` is `ux.Sparkle` (`oracleSparklePrefix`), added in ticket 13 for `CommandLogger.success`'s **default** `symbol="sparkles"`. The header line is a `logger.success(...)` call with no symbol override, exactly like `pack`'s "Built marketplace.json" line that ticket 13 already converted.
- `[+]` is `symbol="check"` — the per-check "passed" rows. `Sparkle`'s own doc comment already names `marketplace validate`'s per-check rows as the example of a call site that overrides to `check`. apm-go currently renders those through the width-3-centered `ux.Success` (` + `), which is the pre-ticket-10 convention.

The quote style is a plain `%q` → `'%s'` change on the header's format string; the Oracle uses single quotes throughout (`f"Validating marketplace '{name}'..."`).

## Acceptance criteria

- [x] AC1 — Cite the Oracle file:line for both the header's `logger.success(..., symbol=...)` call and the per-check rows' call, and confirm from the source (not from the diff alone) which symbol each one actually resolves to. If the source contradicts the observed bytes, the source wins and this ticket's premise is wrong — say so rather than forcing the bytes to match.
- [x] AC2 — Header line renders `[*] Validating marketplace 'NAME'...` — `[*]` glyph, single quotes.
- [x] AC3 — Per-check pass rows render `[+]   Structure: passed` (and Schema/Names), preserving the existing three-space indent exactly. The existing `[x]` failure row must not change.
- [x] AC4 — Do **not** widen `ux.Success` → `ux.Sparkle` globally. Ticket 13 deliberately scoped `Sparkle` to verified call sites; change only the call sites this ticket's evidence covers.
- [x] AC5 — `validate-checkrefs-off`, `validate-checkrefs-on`, `validate-structure-fail` each report **no `stdout` and no `error_body` diff** in a fresh runner corpus. Their remaining `tree` diff is ticket 24's and their `stderr` diff is out of scope (see below) — those may stay.
- [x] AC6 — Zero regression elsewhere: every other case's `(fields, waived)` tuple is unchanged from the pre-change baseline.
- [x] AC7 — Go tests asserting the exact bytes of both line shapes.

## AC1 finding: the ticket's premise about the header call was wrong (bytes still right)

Checked the Oracle source directly rather than reverse-engineering from the diff, per AC1:

- **Header** — `commands/marketplace/validate.py:29`: `logger.start(f"Validating marketplace '{name}'...", symbol="gear")`. `CommandLogger.start` (`core/command_logger.py:81-83`) delegates to `_rich_info` (`utils/console.py:170-172`, color="blue"), **not** `CommandLogger.success`/`_rich_success` (color="green", bold) as the ticket's "What the fix is" section assumed. So the header is an INFO-channel call, not a SUCCESS-channel one.
- However, `STATUS_SYMBOLS` (`utils/console.py:37-61`) maps **both** `"gear"` and `"sparkles"` to the identical `"[*]"` glyph. So the required output byte (`[*]`) is exactly what the ticket predicted — only the *reason* (which Oracle method/channel produces it) was misstated. This does not change AC2's requirement; it changes which apm-go printer is the right one to reuse (see Implementation).
- **Per-check pass rows** — `commands/marketplace/validate.py:66`: `logger.success(f"  {r.check_name}: passed", symbol="check")` → `_rich_success` (green, bold) → `STATUS_SYMBOLS["check"]` = `"[+]"`. This one matches the ticket's premise exactly.

Reported as required by AC1 rather than silently forcing the bytes to match a wrong citation.

## Implementation

`internal/ux/printer.go`:
- New `oracleCheckPrefix = "[+] "` const (alongside the existing bracket-prefix consts).
- New `Gear(w, format, a...)`: same `oracleSparklePrefix` ("[*] ") as `Sparkle`, but styled with `infoStyle` (blue), not `successStyle` — because the Oracle call it mirrors (`logger.start(..., symbol="gear")`) is genuinely an info-channel call (see AC1 finding above), not a `Sparkle`-shaped `logger.success` default. Kept as its own function rather than a second call to `Sparkle`, so `Sparkle`'s own doc comment (scoped exactly to `logger.success`'s default "sparkles" case) stays accurate.
- New `Check(w, format, a...)`: `oracleCheckPrefix` ("[+] ") with `successStyle` — this *is* the `logger.success(..., symbol="check")` case `Sparkle`'s own doc comment already named as the "different call site" example (ticket 13).

`cmd/apm-go/marketplace.go` (`marketplaceValidateCmd`):
- Header: `ux.Info(w, "Validating marketplace %q...", name)` → `ux.Gear(w, "Validating marketplace '%s'...", name)`.
- Per-check pass row: `ux.Success(w, "  %s: passed", check.CheckName)` → `ux.Check(w, "  %s: passed", check.CheckName)` (format string unchanged — its own 2-space indent plus `Check`'s 1-space-after-`]` prefix combine to the exact 3-space gap AC3 requires; the pre-existing `[x]`/`[!]` rows already work this same way, untouched).

AC4: `ux.Success` was **not** touched anywhere else; only these 2 call sites (both individually verified against Oracle source per AC1) were changed. No global `Success` → `Sparkle`/`Check` sweep.

## Tests

- `internal/ux/printer_test.go`: extended `TestOracleLine_BracketPrefixNoExtraSpace` with `Sparkle` (previously untested at the unit level), `Gear`, and `Check` rows — locks the exact `"[*] "`/`"[+] "` prefix + no-extra-space byte shape (AC7).
- `cmd/apm-go/marketplace_e2e_test.go`:
  - `TestMarketplaceValidate_HappyPathPrintsSummaryAndSucceeds`: updated its existing `Validating marketplace "acme"...` assertion to the corrected `Validating marketplace 'acme'...` (single quotes).
  - New `TestMarketplaceValidate_HeaderAndPassRows_ExactOracleBytes` (AC7): asserts the exact lines `[*] Validating marketplace 'acme'...`, `[+]   Structure: passed`, `[+]   Schema: passed`, `[+]   Names: passed` are present, and explicitly asserts the *old* renderings (`Validating marketplace "acme"` double-quoted, `[i] Validating marketplace`) are absent.

## Evidence

- `go build ./...`: clean. `go vet ./...`: clean.
- `go test ./...`: all packages `ok`, zero failures.
- `go test ./cmd/apm-go/... ./internal/ux/... -race -count=1`: both `ok`, no data races.
- Parity corpus (AC5/AC6): built pre-fix (`810aba6`, stashed) and post-fix binaries, ran all 69 `tools/parity` cases against the pinned Oracle (`c8d6cdec596e773a84b0839c33c28b6b0a217637`, confirmed at that exact commit, no drift from pin) for each. Both runs report "18 of 69 case(s) have unwaived diffs" (expected: no NEW waivers were added, and the 3 target cases still carry their out-of-scope `tree`/`stderr` residue, so the raw count is unchanged by design). A tuple-level `(fields, waived)` diff across all 69 case ids shows **exactly 3 changes, all the 3 target cases, all in the expected direction**:
  - `validate-checkrefs-off`: `(stdout, tree)` → `(tree,)`
  - `validate-checkrefs-on`: `(stdout, tree)` → `(tree,)`
  - `validate-structure-fail`: `(error_body, stderr, stdout, tree)` → `(stderr, tree)`

  All other 66 cases: **zero change**.

## Out of scope

`validate-structure-fail`'s `stderr` diff. The Oracle emits a Python `logging` record to stderr:

```
WARNING apm_cli.marketplace.models marketplace.json 'plugins' field is not a list in 'broken'
```

apm-go emits nothing. Whether apm-go should reproduce Python's logging surface at all is a separate question from this ticket's glyph/quote fix — record it as its own ticket rather than deciding it here.
