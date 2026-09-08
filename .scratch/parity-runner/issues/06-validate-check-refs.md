# 06 — `marketplace validate --check-refs` hidden no-op

**What to build:** `apm-go marketplace validate NAME --check-refs` is accepted (hidden from help), prints upstream's "not yet implemented" warning, performs no ref lookup or network call, and otherwise behaves identically to `validate` without the flag — with runner cases proving it.

**Blocked by:** 02 — Parity runner: normalise, diff, classify, waiver gate, self-test.

**Status:** done (command-local) — 3b7e055; verified .review/eval-ticket-06.md AC1–4 PASS. AC5 runner-evidence FAIL is a pre-existing validate gap split into ticket 11; 06's cases will go clean when 02-attempt-3 (tree) and 11 (Structure/help) land.

**Oracle:** `commands/marketplace/validate.py:16-54`. Spec stories 14–16. Evaluator guardrails: `.review/ticket-review.md` §F "04".

## Acceptance criteria

- [ ] `--check-refs` is a bool flag marked hidden; `apm-go marketplace validate --help` does not list it; passing it parses without error.
- [ ] When set, after validation results are computed and before they are rendered (upstream ordering, lines 49–54), emit `ux.Warn` with exactly "Ref checking not yet implemented -- skipping ref reachability checks".
- [ ] Exit code, rendered results, and every other output line are byte-identical to the same invocation without the flag. A test runs both and diffs captured stderr minus the one warning line.
- [ ] No network / no ref resolver: the test installs a fake `git` on PATH that records invocations and asserts it was never called during `validate --check-refs`; additionally the marketplace fetch seam used by `validate` is the local-file path for the fixture, so no HTTP occurs.
- [ ] Runner cases (`validate-checkrefs-on`, `validate-checkrefs-off`, `validate-help`) on the same local-marketplace fixture as ticket 05; `diff.jsonl` clean for all three.
