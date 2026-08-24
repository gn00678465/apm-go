# 16 — dep-parser full Oracle conformance (spun out of ticket 11)

**What to build:** Complete semantic equivalence between `manifest.ParseDepString` (+ `pythonRepr*` diagnostics rendering) and the pinned Oracle's `DependencyReference.parse` / CPython `repr()`, driven entirely by the checked-in conformance tables (`spec/conformance/depref-accept.json`, 114 rows; `spec/conformance/python-repr.json`, 46 rows + 276-cp isspace sweep) and their generator (`tools/depref_conformance_gen.py`).

**Blocked by:** nothing. **Status:** open — table-driven, incremental.

**Origin:** ticket 11 attempts 2-8. Ticket 11's own ACs (M-06: Structure check row + help drift) were satisfied by attempt 2-3 — the corpus (fields, waived) tuples have been byte-identical from attempt 3 through attempt 8 — but evaluation kept probing ever-deeper `DependencyReference.parse` boundaries (per-element diagnostics -> coordinate grammar -> percent/FQDN -> urlsplit netloc/userinfo/ports -> maximal-subpart UTF-8 -> python repr byte-equality), each finding real but pre-existing install-side parser gaps that surface through `marketplace validate`'s Structure check. That surface is effectively unbounded and belongs to its own ticket, per the project's standing convention (ticket 10 deferred wording to 14; search's tree gap to 05).

## Already fixed under ticket 11 (kept, nothing to redo)

Attempts 2-8 landed: per-element Structure diagnostics; coordinate grammar via the dep parser; repo/repository Python-truthiness; tag_pattern validate-time deferral; whole-string lenient percent-unquote + FQDN host gate; true recursive python repr (ordered dicts, surrogates incl. dict keys, number lexemes, inf/-0, dup-key first-position-last-value, Python isspace set); case-insensitive http(s) schemes; urlsplit netloc semantics (userinfo/lowercase host/port 0-65535/empty port); https strip+double-unquote vs ssh lstrip+reject_empty asymmetry; maximal-subpart U+FFFD; Oracle ref-fragment parity (no parse-time charset gate); ssh userinfo single-split model.

## Working rule

- New divergence found (by anyone) => add a row to the generator, regenerate against `tools/parity/oracle.pin`, then either fix apm-go to match or record a documented `known_gap` (apm-go-side security hardening only, both directions behaviorally locked). The conformance tests keep every settled row from regressing.
- Known open areas never probed exhaustively: ADO org/project/repo URL shapes, artifactory prefixes, GitLab nested groups (known_gap today), IPv6 hosts, SCP corner forms, registry-routed entries, alias/fragment interplay, virtual-path extension rules.

## Row backlog from eval-ticket-11 Re-scoped ruling (2026-08-24, verified real by orchestrator probe)

- `ssh://host.io/owner/repo@alias` (+ SCP equivalent): Oracle accepts path-level alias; apm-go repo-segment parser rejects.
- `ssh://alice:p%25@host.io/owner/repo`: Oracle's raw percent-userinfo safeguard rejects after first decode; apm-go accepts after password discard.
- SSH host charset: `ssh://host!bang/...`, `ssh://host_name/...`, decoded `ssh://host%20name/...` — Oracle's SSH path accepts; apm-go hostCharRe rejects (orchestrator verified host_name: Oracle exit 0 vs apm-go exit 1).
