# 23 — `search`'s usage-error hints name the wrong command path

**What to build:** `search`'s two usage-error messages must cite `apm marketplace search`, the command path the Oracle names, not `apm search`.

**Blocked by:** none. **Status:** CLOSED — INVALID (2026-08-25). The premise was wrong; no wording change was made. See the outcome section at the bottom. **Clears:** the `stdout`/`error_body` fields of `search-empty-marketplace`, `search-empty-query`, `search-missing-at` (3 of the 18-case parity backlog).

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


---

## Outcome: closed as INVALID

AC1 did its job. The implementor checked for an existing deviation comment before touching anything, found one at `cmd/apm-go/search.go`'s `searchCmd`, and stopped. The orchestrator then verified the claim against the Oracle source directly:

- `commands/marketplace/__init__.py:1351` declares the command with a bare `@click.command(...)`. **Every other command in that file** uses `@marketplace.command(...)`.
- `cli.py:224` is the only registration: `cli.add_command(marketplace_search, name="search")`. It is never added to the `marketplace` group.
- The Oracle's own `search --help` prints `Usage: apm search [OPTIONS] QUERY@MARKETPLACE`.

So **`apm marketplace search` does not exist in the Oracle**. Its three hint strings (`__init__.py:1361, 1371, 1379`) name an invocation that would fail if a user typed it — an upstream copy-paste slip. Transliterating it into apm-go would mean shipping a hint that points users at a dead command. apm-go's `apm-go search ...` is correct and already deliberate.

This ticket's framing ("AGENTS.md says match the Oracle's observable bytes") was too literal: the rule exists so apm-go behaves as users expect, and here the Oracle's bytes contradict the Oracle's own behaviour.

### Two real defects found while verifying, both fixed

1. **The deviation comment overclaimed, twice.** It asserted the Oracle registers the command "under two names: `apm marketplace search` and, via cli.py:224, top-level `apm search`" — it does not, there is one name. And it asserted that four parity cases "carry a waiver on this exact, deliberate difference" — only `search-unknown-marketplace` had any waiver, and that waiver's reason is about a different string entirely (the `Marketplace '%s' is not registered...` message at `search.go:82`). Rewritten with the correct registration facts and an explicit note about what the old text got wrong.
2. **`search-missing-at`, `search-empty-query`, `search-empty-marketplace` had zero waiver coverage** — which is the actual reason they were sitting in the 18-case backlog. Not wrong wording; a missing record for an already-correct deviation. Added, each waiver naming both differences on the line (the sanctioned wording deviation, and the ticket-14 Rich wrap residue) rather than hiding one behind the other.

Corpus after: **18 → 15 unwaived**, exactly those three cases flipping to waived, zero drift elsewhere, no ghost waivers.
