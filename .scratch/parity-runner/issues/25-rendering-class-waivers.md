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

Expect these to be the residue **after** 22/23/24 land. Re-derive the exact field list from a fresh corpus rather than trusting this table — if a field listed here is already green, do not waive it.

| case | fields | class |
|---|---|---|
| `list-empty` | stdout | wrap |
| `browse-unknown-marketplace` | stdout, error_body | wrap |
| `search-unknown-marketplace` | stdout, error_body | wrap |
| `search-zero-results` | stdout | wrap |
| `search-last-at-split` | stdout | wrap |
| `search-empty-marketplace` | stdout | wrap |
| `search-empty-query` | stdout | wrap |
| `search-missing-at` | stdout | wrap |
| `search-basic-hit` | stdout | table |
| `search-limit-1` | stdout | table |
| `search-tag-hit` | stdout | table |
| `search-description-truncation` | stdout | table |
| `registry-explicit-config-dir` | stdout, tree | table + `APM_CONFIG_DIR` |
| `search-help` | stdout | Click-vs-Cobra help |

`error_body` accompanies `stdout` on the wrap cases because the runner's error_body extractor takes the first line, so a wrapped Oracle line is truncated mid-sentence — the same root cause, not a second one. Say so in each such waiver's reason.

## Acceptance criteria

- [ ] AC1 — Every waiver's `reason` states which of the four classes above it belongs to, points at that class's own source of record (the ticket, the code comment, or the existing waiver it inherits from), and — for the wrap cases — shows the Oracle and target byte sequences are identical once line breaks are ignored. A reason that says only "rendering difference" is not acceptable.
- [ ] AC2 — Waivers are **field-scoped**. Never waive a whole case when only one field differs; never include a field that 22/23/24 fixed.
- [ ] AC3 — `registry-explicit-config-dir`'s `tree` waiver uses `tree_paths` to scope itself to the `APM_CONFIG_DIR`-induced paths specifically, following the `doctor-healthy` precedent. It must not blanket-waive that case's tree.
- [ ] AC4 — Each entry carries `oracle_commit` = the pinned SHA, `owner`, and `eval_plan_ref`, matching the shape of the 57 existing entries.
- [ ] AC5 — **A waiver that is not needed is a defect.** The runner's waiver gate is fail-closed on ghost waivers; a fresh corpus must report zero ghosts.
- [ ] AC6 — After 22/23/24 + this ticket, a fresh corpus reports **0 of 69 unwaived** and the `parity` CI job goes green. If any case still differs on a field not in the table above, that is a new finding — report it as its own ticket rather than widening a waiver to swallow it.

## Non-goals

- Re-litigating whether apm-go should emulate Rich. Ticket 14 settled it, with an evaluator PASS. This ticket records that outcome; it does not reopen it.
- Waiving anything textual. Every waiver here must be provably about line breaks, box-drawing characters, padding, or an apm-go-only env var. A difference in *words* is a bug, and belongs in ticket 22/23 or a new one.
