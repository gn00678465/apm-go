# 08 — Runner evidence backfill: `doctor`

**What to build:** Every `apm-go doctor` behaviour that the earlier three manual evaluator rounds marked PASS is re-established as runner evidence: each check's name/detail/severity, exit code, non-TTY rendering, git stderr translation, and token non-leak — using fault-injection fixtures, not only a healthy environment.

**Blocked by:** 02 — runner diff/gate; 03 — Oracle pin preflight.

**Status:** ready-for-agent

**Oracle:** `commands/doctor.py`, `commands/marketplace/doctor.py:103-329`, `marketplace/git_stderr.py`. Spec section "doctor" (earlier spec `.review/spec-plugin-init-format-and-doctor.md` stories 15–31). Evaluator guardrails: `.review/ticket-review.md` §F "06A".

## Fault-injection fixture mechanism

- [ ] The runner gains `case.path_prepend`: a fixture subdirectory placed at the front of `PATH` for both sides. Ticket adds this with a unit test. Fixtures ship a fake `git` shell script whose behaviour is selected by `FAKE_GIT_MODE` (set via `case.env`): `ok`, `missing` (script absent — the dir simply has no `git`), `nonzero`, `hang` (sleeps past timeout — use a short per-case `timeout_s` field, add it), `dns-fail`, `auth-fail`, `not-found`, `tls-fail`. Each failure mode emits a stderr sample taken from real git output.

## Cases (`doctor-`), each with `expected_taxonomy`
- [ ] healthy
- [ ] git-missing (exit 1, both critical rows `[x]`)
- [ ] git-nonzero
- [ ] network-dns-fail, network-auth-fail, network-not-found, network-tls-fail — each asserting the translated hint line, not raw stderr
- [ ] network-timeout (`hang` + `timeout_s: 8`; detail "Network check timed out (5s)")
- [ ] token-present / token-absent (`case.env.GITHUB_TOKEN`); the runner's raw-output scan asserts the token value appears in NEITHER side's stdout/stderr/fs (add a `forbid_substrings` field to `case.json`, fail the case if found).
- [ ] config-none, config-apmyml-valid, config-legacy, config-both, config-apmyml-malformed, config-legacy-malformed, config-duplicate-names
- [ ] help

## Evidence
- [ ] `diff.jsonl` clean for every `doctor-*` case except field-precise waivers for: the `apm-go` binary-name substitution in hint text (`rewrite_binary_name: true` handles most; any residual is waived per case/field), and the three out-of-scope informational checks the Oracle prints and apm-go does not (`format coverage`, `version alignment`, `executable trust`) — waived as `stdout` only, `taxonomy: "F01"`, `eval_plan_ref: "spec Out of Scope"`.
- [ ] The earlier manual reports (`.review/eval-report-plugin-init-doctor*.md`) are annotated at the top with "superseded by runner evidence <out>/diff.jsonl @ <commit>".
