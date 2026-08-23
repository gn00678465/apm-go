# 05 — `apm-go search QUERY@MARKETPLACE`

**What to build:** A top-level `search` command that finds plugins in one registered marketplace by case-insensitive substring over name, description, and tags — with upstream's exact parsing rules, messages, rendering, and exit codes — and runner cases proving each behaviour against the Oracle.

**Blocked by:** 02 — runner diff/gate (attempt 2: bytes normalisation + help_semantic); 10 — cross-command error-output contract.

**Status:** attempt 1 FAIL (c156aee + 5c1cc2a); evaluator ruling .review/eval-ticket-05.md. Attempt 2 is BLOCKED by 02 (attempt 2) and 10. Remains FAIL until 10 decides the error-output contract.

**Oracle:** `commands/marketplace/__init__.py:1351-1444` (command), `marketplace/models.py:375-382` (`matches_query`), `marketplace/client.py:1238-1246` (`search_marketplace`), `cli.py:224` (registered top-level). Spec stories 1–13. Evaluator guardrails: `.review/ticket-review.md` §F "03".

## Acceptance criteria

### Behaviour (command-level tests, `cmd.Execute()` with `isolatedMarketplaceRegistry`)
- [ ] Registered as top-level `apm-go search`, NOT under `marketplace`. `apm-go marketplace search` remains unknown.
- [ ] No `@` → "Invalid format: 'X'. Use QUERY@MARKETPLACE, e.g.: apm-go search security@skills", exit 1.
- [ ] Split on the LAST `@`; `foo@bar@skills` → query `foo@bar`, marketplace `skills`.
- [ ] Either side empty → "Both QUERY and MARKETPLACE are required. Use QUERY@MARKETPLACE, e.g.: apm-go search security@skills", exit 1.
- [ ] Unregistered marketplace → "Marketplace 'X' is not registered. Use 'apm-go marketplace list' to see registered marketplaces.", exit 1.
- [ ] Matching: lowercase `q` substring of name OR description OR any tag; results in manifest order; `--limit` (default 20, `show_default`) applied after matching.
- [ ] Zero results → warning "No plugins found matching 'Q' in 'M'. Try 'apm-go marketplace browse M' to see all plugins.", exit 0.
- [ ] Rich/TTY: blank line, table titled "Search Results: 'Q' in M" with columns Plugin / Description / Install; description `--` when empty, truncated to 57 chars + `...` when > 60; Install column is `name@M`. Then "Install: apm-go install <plugin-name>@M".
- [ ] Non-rich: "Found N plugin(s):" then `  name@M -- description` per hit (no ` -- ` suffix when description empty), then the Install hint. All through `ux`.
- [ ] Fetch/registry failure → "Search failed: <err>", exit 1; traceback-equivalent detail only with `-v`.
- [ ] `--help` matches upstream's help/flag wording (`--limit` shows default 20, `-v/--verbose`).
- [ ] The only `apm`→`apm-go` substitutions are in the three hint strings above; no other wording changes.

### Runner evidence
- [ ] Fixture: a local `marketplace.json` with ≥4 plugins (one matches by name, one by description, one by tag, one with a >60-char description, one with empty description) registered in both sides' isolated config via a preceding `marketplace add ./fixture` step (the runner supports `setup_argv` in `case.json` if needed — add it in this ticket with a test).
- [ ] Cases (ids prefixed `search-`): missing-at, empty-query, empty-marketplace, last-at-split, unknown-marketplace, basic-hit, tag-hit, description-truncation, limit-1, zero-results, help. All `rewrite_binary_name: true`; `expected_taxonomy` set per case.
- [ ] `diff.jsonl` shows zero unwaived fields for every `search-*` case. Any remaining diff must be a field-precise waiver entry with reason, not a broadened one.

## Attempt-2 notes (first real runner run, /tmp/p5)

Three things the attempt-1 waivers covered up. Fix the first two in the command; the third is ticket 02's bytes normalisation — do NOT waive it.

1. **Rendering path mismatch — RESOLVED by evaluator (eval-ticket-05.md §C).** Upstream `_get_console()` (`commands/_helpers.py:72-93`) checks NOTHING about TTY/CI/NO_COLOR; it returns a Rich Console whenever Rich imports, so the table ALWAYS renders (box-drawing, no ANSI under NO_COLOR). The `if not console` plain branch is unreachable in a normal install. Therefore `search` must take the table path unconditionally — do not gate on `ux.IsRich()`/`CanPrompt()`. Keep the plain branch in code only as the literal upstream fallback (renderer unavailable), never selected at runtime. Then the table itself must match: title line, column headers Plugin/Description/Install, 57+`...` truncation, and the `Install:` hint after it. Box-drawing characters and padding are the only acceptable `stdout` waiver, and it must be field-precise and say so.
2. **Error stream + prefix + hint text — BLOCKED on a decision.** Oracle error paths print to STDOUT with a `[x] ` prefix; Target prints `Error: …` to stderr. Verified this is NOT search-specific: `marketplace browse nonexistent` shows the same split on every existing apm-go command. Making `search` alone match the Oracle would leave apm-go internally inconsistent. The orchestrator has asked the evaluator for a ruling (eval-ticket-05.md). Until then: keep `search`'s error stream consistent with its sibling commands (stderr), fix ONLY the hint text to upstream's literal `apm-go marketplace search security@skills`, and leave the stream/prefix diff UNWAIVED with `expected_taxonomy: ["F08"]` so it shows in diff.jsonl as an open systemic finding.
3. **`tree` diff on `config/marketplaces.json`** is the registry storing each side's absolute fixture path. That is a runner normalisation gap (ticket 02 attempt 2). Until it lands, this case set will show `tree` diffs; leave them UNWAIVED and note it in the commit message.

Acceptance for attempt 2: `diff.jsonl` for every `search-*` case shows either no fields, or `stdout` only with a waiver whose reason names box-drawing/padding and nothing else. No `stderr`, `exit_code`, or `help_semantic` waivers.
