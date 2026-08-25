# 23 — `search`'s usage-error hints name the wrong command path

**What to build:** `search`'s two usage-error messages must cite `apm marketplace search`, the command path the Oracle names, not `apm search`.

**Blocked by:** none. **Status:** open. **Clears:** the `stdout`/`error_body` fields of `search-empty-marketplace`, `search-empty-query`, `search-missing-at` (3 of the 18-case parity backlog).

**Origin:** orchestrator triage of the standing 18 unwaived parity cases, 2026-08-25.

## The diffs

`search-empty-marketplace` and `search-empty-query` (identical message):

```
oracle: [x] Both QUERY and MARKETPLACE are required. Use QUERY@MARKETPLACE, e.g.: apm marketplace search security@skills
target: [x] Both QUERY and MARKETPLACE are required. Use QUERY@MARKETPLACE, e.g.: apm search security@skills
                                                                                      ^^^^^^^^^^^^
```

`search-missing-at`:

```
oracle: [x] Invalid format: 'nope'. Use QUERY@MARKETPLACE, e.g.: apm marketplace search security@skills
target: [x] Invalid format: 'nope'. Use QUERY@MARKETPLACE, e.g.: apm search security@skills
```

(The Oracle's captured bytes wrap these across two lines at console width. That wrap is a separate, waivable rendering difference — ticket 25 — and is **not** what this ticket fixes. Compare word sequences, not line breaks.)

## Why this is a real gap and not an intentional deviation

The invocation really is `apm search` / `apm-go search` — the Oracle's own `search --help` says `Usage: apm search [OPTIONS] QUERY@MARKETPLACE`, and apm-go mirrors that. So the Oracle's *hint text* names a path (`apm marketplace search`) that differs from its own usage line. That looks like an upstream inconsistency, but AGENTS.md's rule is to match the Oracle's observable bytes where it handles a case, and the runner already rewrites `apm-go` → `apm` so the binary name is not the difference here.

Before changing anything, confirm this is not already recorded as a deliberate deviation: `cmd/apm-go/*.go` marks intentional gaps in a comment at the deviation. If a comment already justifies `apm search` here, this ticket is invalid — say so instead of changing the text.

## Acceptance criteria

- [ ] AC1 — Check for an existing deviation comment at the call site (see above). If one exists, stop and report; do not change the message.
- [ ] AC2 — Cite the Oracle file:line for both message strings.
- [ ] AC3 — Both messages name `apm marketplace search`; every other word, including the `e.g.:` punctuation and the `'nope'` single quotes, stays byte-identical to what the Oracle emits.
- [ ] AC4 — `search-empty-marketplace`, `search-empty-query`, `search-missing-at` each report **no `error_body` diff** in a fresh runner corpus. Their `stdout` diff may remain — the residue is the Rich wrap alone, which ticket 25 waives. State explicitly in the report which of the two fields each case still differs on and why.
- [ ] AC5 — Zero regression elsewhere: every other case's `(fields, waived)` tuple unchanged from the pre-change baseline.
- [ ] AC6 — Go tests asserting the exact message bytes.
