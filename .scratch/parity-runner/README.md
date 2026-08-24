# parity-runner — ticket index

Spec: `.review/spec-surface-gaps-and-parity-runner.md`
Evaluator review that shaped the split: `.review/ticket-review.md`

| # | Ticket | Blocked by |
|---|---|---|
| 01 | Runner: isolated execution and raw capture | — |
| 02 | Runner: normalise, diff, classify, waiver gate, self-test | 01 |
| 03 | Runner: Oracle/Target pin preflight | 01 |
| 04 | Shared bundle-format selector resolver (prefactor) | — |
| 05 | `apm-go search QUERY@MARKETPLACE` | 02, 10 |
| 06 | `marketplace validate --check-refs` hidden no-op | 02 |
| 07 | `pack --format` / `--claude-plugin` selector | 02, 04 |
| 08 | Runner evidence backfill: `doctor` | 02, 03 |
| 09 | Runner evidence backfill: `plugin init` + CI gate | 02, 03, 04, 07 |
| 10 | Cross-command error/warning output contract (systemic F08) | 02 |
| 11 | `validate` missing Structure check + help drift (M-06) | 02 |
| 12 | Global `~/.apm/config.json` + `last_version_check` parity (F09) | 02 |
| 13 | `pack` success output + `--help` parity (pre-existing) | 10 |
| 14 | marketplace command wording parity (browse/list first) | 10 |
| 15 | Runner: drop APM_CONFIG_DIR injection; registry root = sandbox HOME (F04) | 12 |
| 16 | dep-parser full Oracle conformance (table-driven; spun out of 11) | — |
| 17 | `pack` flag/feature parity (spun out of 13) | — |
| 18 | `marketplace audit` missing "Run with --verbose for details." hint (spun out of 14 attempt 2) | — |

Frontier order: {01, 04} → {02, 03} → {06, 07, 08, 10} → {05} → 09.

Status: 01 02 03 04 08 10 11 12 13 14 15 ✅ · 06 07 ✅(command-local) · 13 closed at attempt 2 (a0b3f82; help/feature backlog user-re-scoped to 17) · 14 closed at attempt 3 (PASS; wording fixes from attempt 1 stand -- marketplaceNotRegisteredErr's Oracle-fixed-format rewrite, `marketplace list`'s empty-registry sentence. Attempt 2's Rich-console-wrap emulation, added to chase byte-exact parity on the two long messages, was withdrawn by orchestrator direction after fixing 4 renderer defects still left an unbounded emulation surface behind them (Unicode COLUMNS digits, ZWJ graphemes, tab stops, ANSI passthrough) -- the ticket-11 pattern. Per eval-plan §8.3, Rich-vs-ux rendering differences are compare-semantics-and-waive territory (doctor-healthy precedent), not chase-byte-parity; apm-go's messages are single-line by product contract. wrap.go and its conformance fixture deleted; the 5 pack-* stdout waivers and browse/list/search-unknown's stdout(+error_body) waivers restored/updated to name the single-line-vs-wrapped mechanism) · 11 closed (attempt 8, re-scoped PASS) · 05 command+evidence landed, gaps open · 09 ready · 16 17 18 open (backlog, non-blocking).

Implementor protocol: one ticket per fresh context, via `/implement`. Evaluator verifies each ticket against `diff.jsonl`.

Escalation ladder (finalized 2026-08-24): (1) implementor implements, evaluator verifies, orchestrator coordinates/integrates; (2) after 3 implementor FAILs on the same ticket, the orchestrator takes over implementation directly; (3) after 3 further FAILs on the orchestrator's own work, STOP iterating and re-audit the spec/eval documents themselves (three-hypothesis diagnosis: capability / evaluator interpretation / document defect — ticket 11's root cause was an AC phrase with no depth bound).

Scope rule (added 2026-08-24, after ticket 11 ran to 8 attempts): a ticket's PASS/FAIL boundary is its WRITTEN acceptance criteria. A finding that is real but outside them — in particular any pre-existing gap on a shared, effectively unbounded semantic surface (e.g. the dep-string grammar) — is recorded as a new conformance-table row / new ticket, not a blocker on the current one. This is the same convention tickets 05/10/14 already used for wording and tree gaps.
