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

Frontier order: {01, 04} → {02, 03} → {06, 07, 08, 10} → {05} → 09.

Status: 01 02 03 04 10 12 15 ✅ · 06 07 ✅(command-local) · 10 closed at attempt 3 (6de0793; eval-ticket-10-r3 Round 4 PASS, evidence /tmp/p10-evidence @ bbf60df) · 08 closed (all 18 doctor-* cases waived-clean; 2 real apm-go bugs found+fixed: doctor's silent-exit contract, execGit's hang on an orphaned-grandchild-holding-pipes fixture; see .scratch/parity-runner/issues/08-doctor-backfill.md) · 05 command+evidence landed, convergence reduced by 10a3 ([>] progress + [i] prefix), remaining gaps open (marketplaces.json tree, error-case wording) · 11 13 14 ready · 09 unblocked (08 landed).

Implementor protocol: one ticket per fresh context, via `/implement`. Evaluator verifies each ticket against `diff.jsonl`; orchestrator intervenes after 3 failed attempts on the same ticket.
