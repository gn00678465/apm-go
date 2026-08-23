# 11 — `marketplace validate` missing Structure check + help drift (M-06)

**What to build:** `apm-go marketplace validate NAME` reports the same three checks as the Oracle — Structure, Schema, Names — with the same summary count, and its `--help` carries the same semantic flag/description set.

**Blocked by:** 02 — runner diff/gate (attempt 3: HOME capture so the registry tree diff disappears).

**Status:** ready-for-agent

**Origin:** runner cases from ticket 06 (`.review/eval-ticket-06.md` AC5): Oracle prints Structure/Schema/Names → "3 passed"; Target prints Schema/Names → "2 passed". `validate-help` shows a `help_semantic` diff. These are pre-existing M-06 gaps the runner exposed, not caused by ticket 06.

**Oracle:** `commands/marketplace/validate.py:17-91`, `marketplace/validator.py` (`validate_marketplace` — read what "Structure" asserts and what message it prints on pass/fail).

## Acceptance criteria

- [ ] Target's `ValidateChecks` (or its caller) emits a Structure check with the Oracle's pass/fail message, in the Oracle's order (Structure, Schema, Names), and the summary counts it. Fixture with a structurally broken manifest (e.g. `plugins` not a list) fails Structure with the Oracle's message and exit 1.
- [ ] `validate --help`: `help_semantic` diff is empty against the Oracle (flag set, short aliases, defaults, descriptions). `--check-refs` stays hidden on both.
- [ ] Rich/`ux` symbol and quote differences in the results lines are the ONLY remaining `stdout` diff, recorded as field-precise F08 waivers per case (same rule as ticket 10) — not wording, not counts.
- [ ] Runner cases `validate-checkrefs-off`, `validate-checkrefs-on`, `validate-help`, plus new `validate-structure-fail`: `diff.jsonl` shows no `help_semantic`, no `exit_code`, no `tree` (after 02 attempt 3), and `stdout` only under a channel/symbol waiver.
