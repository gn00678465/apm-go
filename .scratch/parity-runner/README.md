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
| 19 | `init`/`plugin init` output-surface gaps: apm.yml YAML cosmetics, success-output stream split, help_semantic parser limitation (spun out of 09) | — |
| 20 | `marketplace package add ./<path>\` writes a broken name/source; local sources are never stat-ed (user-reported) | — |
| 21 | derived-name rejection blames the name, not the source the user typed (spun out of 20's evaluator follow-up) | 20 |
| 22 | `marketplace validate` output: `[*]`/`[+]` glyphs and single quotes (parity backlog) | — |
| 23 | ~~`search` usage-error hints~~ CLOSED INVALID — Oracle's hint names a command path that does not exist; waivers added instead | — |
| 24 | `marketplaces.json`: `file://` URI form + create-when-empty (parity backlog; was 05's tree gap) | — |
| 25 | record the rendering-library residue as field-scoped waivers (parity backlog) | 22, 23, 24 |
| 26 | `marketplace list` missing its "Registered Marketplaces" title (spun out of 25) | — |
| 27 | embedded/serialized `apm.lock.yaml` omits `generated_at`/`deployments` when the source lockfile never had them (spun out of 17 phase 2) | — |
| 28 | marketplace producer success output: catalog, absolute paths, and Claude key order | — |
| 29 | `pack` Codex category-required error wording | — |

Frontier order: {01, 04} → {02, 03} → {06, 07, 08, 10} → {05} → 09.

Status: verification gates are GREEN (2026-08-28). `go vet ./...` and `CI=true GITHUB_ACTIONS=true go test ./... -race` pass; the pinned-Oracle parity run reports **78 cases, 0 unwaived**. CI-detection env remains pinned in TestMain so ambient `CI`/`GITHUB_ACTIONS` cannot change local-vs-Actions results.

Tickets 01-29 are closed, with 23 CLOSED INVALID. The 18-case backlog was triaged into root causes rather than symptoms and cleared: 17 (all 8 missing `pack` flags across 5 phases), 18 (audit verbose hint), 22 (validate `[*]`/`[+]` glyphs + single quotes), 23 (the Oracle's `apm marketplace search` hint names a command that was never registered), 24 (`marketplaces.json` `file://` form + materialise-on-read, 11 cases' tree), 25 (rendering-class waivers), 26 (`marketplace list`'s missing "Registered Marketplaces" title and ALL-CAPS headers), 27 (bundle lockfile's `generated_at`/`deployments`), 28 (marketplace success catalog, absolute paths, and Claude key order), and 29 (Codex category-required wording).

Two waivers were REJECTED during this run for hiding real gaps behind honest-sounding reasons (`pack-archive`'s missing size suffix and zip-migration notice; ticket 25's would-be `registry-explicit-config-dir` stdout). Both became fixes. The rule that held: a difference in WORDS is a bug; only line breaks, box-drawing, padding, timestamps, compressor output and apm-go-only flags get waived.

Open, non-blocking: 16 (dep-parser conformance rows), 19 (init output-surface gaps). Ticket 21 closed 2026-08-28 on an independent evaluator PASS (`.review/eval-ticket-21.md`).

Implementor protocol: one ticket per fresh context, via `/implement`. Evaluator verifies each ticket against `diff.jsonl`.

Escalation ladder (finalized 2026-08-24): (1) implementor implements, evaluator verifies, orchestrator coordinates/integrates; (2) after 3 implementor FAILs on the same ticket, the orchestrator takes over implementation directly; (3) after 3 further FAILs on the orchestrator's own work, STOP iterating and re-audit the spec/eval documents themselves (three-hypothesis diagnosis: capability / evaluator interpretation / document defect — ticket 11's root cause was an AC phrase with no depth bound).

Scope rule (added 2026-08-24, after ticket 11 ran to 8 attempts): a ticket's PASS/FAIL boundary is its WRITTEN acceptance criteria. A finding that is real but outside them — in particular any pre-existing gap on a shared, effectively unbounded semantic surface (e.g. the dep-string grammar) — is recorded as a new conformance-table row / new ticket, not a blocker on the current one. This is the same convention tickets 05/10/14 already used for wording and tree gaps.
