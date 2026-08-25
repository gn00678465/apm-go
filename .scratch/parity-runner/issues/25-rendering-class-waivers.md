# 25 — record the rendering-library residue as waivers (Rich wrap, Rich tables, Click help, `APM_CONFIG_DIR`)

**What to build:** waiver entries for the parity residue that this project has *already decided* is compare-semantics-and-waive, but never recorded. This ticket writes down existing decisions; it does not make new ones.

**Blocked by:** 22, 23, 24 — every field those three fix must actually be fixed first, so no waiver here ever covers a field a fix would have closed. **Status:** open. **Clears:** the remaining fields of the 18-case backlog once 22/23/24 land.

**Origin:** orchestrator triage of the standing 18 unwaived parity cases, 2026-08-25.

## The standing decisions being applied

1. **Rich console word-wrap.** Ticket 14 attempt 3 withdrew the Rich-wrap port after four renderer defects surfaced in one review round, citing the evaluator's own eval-plan §8.3 (Rich-vs-ux rendering-library differences are compare-semantics-and-waive) and concluding "single-line messages are apm-go's own UX contract going forward". The rationale is in `internal/ux/printer.go`'s `oracleLine` doc comment and `.scratch/parity-runner/issues/14-marketplace-wording.md`'s "Attempt 3" section, and the withdrawal carried an evaluator PASS.
2. **Rich table box style.** Already recorded verbatim in the existing `doctor-healthy` waiver: "rich's HEAVY_HEAD box style vs ux.Table's ROUNDED style, plus rich's centered title line vs apm-go's left-aligned one, plus rich's per-cell word-wrap vs ux.Table's column-widening" — and that waiver names `search`'s and `browse`'s own tables as where the difference was first documented.
3. **Click vs Cobra `--help` layout.** `doctor-help` and `validate-help` are already waived on this basis.
4. **`APM_CONFIG_DIR`.** apm-go honours it; the Oracle ignores it. Removing support would be a feature regression for apm-go users, so this is a known_gap (apm-go superset), not something to "fix".

## The cases and fields to waive

The table below (as originally triaged 2026-08-25) is **stale** — 22 and 24 already closed most of it, and 23's own 3 new waivers cover the rest of the wrap-class cases the original table listed. Re-derived from a genuinely fresh corpus at the start of this ticket (HEAD `18e2889`, post-22/23/24): only **7** cases remained unwaived, not the original 14 (the 8th standing unwaived case, `pack-help`, is ticket 17's flag gap and is explicitly out of this ticket's scope):

| case | fields | class |
|---|---|---|
| `search-zero-results` | stdout | wrap |
| `search-last-at-split` | stdout | wrap |
| `search-limit-1` | stdout | table |
| `search-tag-hit` | stdout | table |
| `search-description-truncation` | stdout | table |
| `search-help` | stdout | Click-vs-Cobra help |
| `registry-explicit-config-dir` | stdout, tree | table + `APM_CONFIG_DIR` (**see finding below — stdout is NOT waived**) |

Every other case the original table listed (`list-empty`, `browse-unknown-marketplace`, `search-unknown-marketplace`, `search-empty-marketplace`, `search-empty-query`, `search-missing-at`, `search-basic-hit`) was **already waived** by the time this ticket started (22's glyph fix + 24's `tree` fix let their pre-existing ticket-14/10-era waivers finally cover every remaining field; 23 added 3 more). None of them needed a new waiver here — confirmed by re-running the corpus, not assumed from the stale table.

## Finding: `registry-explicit-config-dir`'s `stdout` is a real bug, not rendering — NOT waived

`registry-explicit-config-dir` runs `marketplace list`. Its `tree` field already had a correctly `tree_paths`-scoped waiver from ticket 15 (untouched by this ticket — see AC3). Its `stdout` field, once isolated, shows the Oracle prints a **"Registered Marketplaces" title line that apm-go never prints at all** (not a differently-aligned instance of the same words — the word is simply absent). `marketplace browse`'s own table already gets this right (`renderBrowseTable(w, fmt.Sprintf("Plugins in '%s'", name), rows)`, matching the Oracle's `title=f"Plugins in '{name}'"`) — `list` is the one command that never got an equivalent title line. Per this ticket's own Non-goals ("不 waive 任何文字差異... 字詞不同就是 bug"), this is **not eligible for a waiver here**. Spun out as **ticket 26** (`.scratch/parity-runner/issues/26-marketplace-list-missing-title.md`); `registry-explicit-config-dir` stays unwaived. Final corpus state after this ticket is therefore **2 of 69 unwaived** (`pack-help` + `registry-explicit-config-dir`), not the 0/69 (or 1/69) originally expected — reported here rather than widening a waiver to swallow a real finding, per AC6's own instruction.

## Acceptance criteria

- [x] AC1 — Every waiver's `reason` states which of the four classes above it belongs to, points at that class's own source of record (the ticket, the code comment, or the existing waiver it inherits from), and — for the wrap cases — shows the Oracle and target byte sequences are identical once line breaks are ignored. A reason that says only "rendering difference" is not acceptable.
- [x] AC2 — Waivers are **field-scoped**. Never waive a whole case when only one field differs; never include a field that 22/23/24 fixed.
- [x] AC3 — `registry-explicit-config-dir`'s `tree` waiver uses `tree_paths` to scope itself to the `APM_CONFIG_DIR`-induced paths specifically, following the `doctor-healthy` precedent. It must not blanket-waive that case's tree.
- [x] AC4 — Each entry carries `oracle_commit` = the pinned SHA, `owner`, and `eval_plan_ref`, matching the shape of the 57 existing entries.
- [x] AC5 — **A waiver that is not needed is a defect.** The runner's waiver gate is fail-closed on ghost waivers; a fresh corpus must report zero ghosts.
- [x] AC6 — After 22/23/24 + this ticket, a fresh corpus reports **2 of 69 unwaived** (not 0 — see the finding above) and the `parity` CI job's remaining red is fully attributed to 2 tracked, out-of-this-ticket's-scope items (`pack-help` = ticket 17; `registry-explicit-config-dir`'s `stdout` = new ticket 26). No case differs on a field not accounted for by a ticket.

## Non-goals

- Re-litigating whether apm-go should emulate Rich. Ticket 14 settled it, with an evaluator PASS. This ticket records that outcome; it does not reopen it.
- Waiving anything textual. Every waiver here must be provably about line breaks, box-drawing characters, padding, or an apm-go-only env var. A difference in *words* is a bug, and belongs in ticket 22/23 or a new one.

## Implementation

`tools/parity/waivers.json`: 6 new entries (`search-zero-results`, `search-last-at-split` — wrap class; `search-limit-1`, `search-tag-hit`, `search-description-truncation` — table class; `search-help` — Click-vs-Cobra help class), each with a `reason` that names its class, cites that class's own source of record, and — for the two wrap cases — quotes the exact Oracle byte sequence and shows the word sequence is identical once each wrapped line is right-stripped and rejoined with a single space. `registry-explicit-config-dir`'s existing (pre-ticket-25, from ticket 15) `tree`-only waiver was left untouched — it already satisfies AC3 (`tree_paths` scoped to exactly `home/.apm/marketplaces.json`, `cwd/altcfg`, `cwd/altcfg/marketplaces.json`) and does not need re-recording. No new waiver was added for its `stdout` field (see the Finding above).

`tools/parity/main_test.go`: `TestRealWaiversJSON_ValidatesAgainstPin`'s hardcoded `wantIDs` extended with the 6 new ids, in file order.

`.scratch/parity-runner/issues/26-marketplace-list-missing-title.md`: new ticket for the `registry-explicit-config-dir` finding.

## Evidence

- `go build ./...` / `go vet ./...`: clean. `go test ./...`: all packages `ok`. `go test ./tools/parity/... -race -count=1`: `ok`, no data races.
- Fresh corpus at ticket start (HEAD `18e2889`): confirmed the stale triage table's 14-case list had shrunk to exactly 7 (plus `pack-help`, out of scope) — matched the orchestrator's own independently-supplied 7-case list exactly, field-for-field.
- AC5 (no ghost waivers): the corpus run with the new `waivers.json` completed successfully end to end (`validateWaivers` runs BEFORE any case executes and hard-fails the whole run with an unrecoverable exit if any waiver id is not a real, loaded case) — a failed/ghost id would have aborted before producing any `diff.jsonl` at all. All 6 new waivers show `waived: true` on their respective case, confirming each is genuinely consulted (not dead weight either).
- Final corpus run (with the 6 new waivers applied): **2 of 69 unwaived** — `pack-help` (ticket 17, untouched) and `registry-explicit-config-dir` (its `stdout` field genuinely unwaived per the Finding above; its `tree` field's pre-existing waiver still applies, confirmed unchanged). Every one of the other 67 cases' `(fields, waived)` tuple matches the fresh pre-ticket-25 corpus exactly, plus the 6 newly-waived cases flipping to `waived: true` with no other field changes.
