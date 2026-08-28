# 16 — dep-parser full Oracle conformance (spun out of ticket 11)

**What to build:** Complete semantic equivalence between `manifest.ParseDepString` (+ `pythonRepr*` diagnostics rendering) and the pinned Oracle's `DependencyReference.parse` / CPython `repr()`, driven entirely by the checked-in conformance tables (`spec/conformance/depref-accept.json`, 172 rows; `spec/conformance/python-repr.json`, 46 rows + 276-cp isspace sweep) and their generator (`tools/depref_conformance_gen.py`).

**Blocked by:** nothing. **Status:** open — table-driven, incremental.

**Origin:** ticket 11 attempts 2-8. Ticket 11's own ACs (M-06: Structure check row + help drift) were satisfied by attempt 2-3 — the corpus (fields, waived) tuples have been byte-identical from attempt 3 through attempt 8 — but evaluation kept probing ever-deeper `DependencyReference.parse` boundaries (per-element diagnostics -> coordinate grammar -> percent/FQDN -> urlsplit netloc/userinfo/ports -> maximal-subpart UTF-8 -> python repr byte-equality), each finding real but pre-existing install-side parser gaps that surface through `marketplace validate`'s Structure check. That surface is effectively unbounded and belongs to its own ticket, per the project's standing convention (ticket 10 deferred wording to 14; search's tree gap to 05).

## Already fixed under ticket 11 (kept, nothing to redo)

Attempts 2-8 landed: per-element Structure diagnostics; coordinate grammar via the dep parser; repo/repository Python-truthiness; tag_pattern validate-time deferral; whole-string lenient percent-unquote + FQDN host gate; true recursive python repr (ordered dicts, surrogates incl. dict keys, number lexemes, inf/-0, dup-key first-position-last-value, Python isspace set); case-insensitive http(s) schemes; urlsplit netloc semantics (userinfo/lowercase host/port 0-65535/empty port); https strip+double-unquote vs ssh lstrip+reject_empty asymmetry; maximal-subpart U+FFFD; Oracle ref-fragment parity (no parse-time charset gate); ssh userinfo single-split model.

## Working rule

- New divergence found (by anyone) => add a row to the generator, regenerate against `tools/parity/oracle.pin`, then either fix apm-go to match or record a documented `known_gap` (apm-go-side security hardening only, both directions behaviorally locked). The conformance tests keep every settled row from regressing.
- Known open areas never probed exhaustively: ADO org/project/repo URL shapes, artifactory prefixes, GitLab nested groups (known_gap today), IPv6 hosts, SCP corner forms, registry-routed entries, alias/fragment interplay, virtual-path extension rules.

## Row backlog from eval-ticket-11 Re-scoped ruling (2026-08-24, verified real by orchestrator probe)

- [x] `ssh://host.io/owner/repo@alias` (+ SCP equivalent): Oracle accepts path-level alias and returns `repo_url='owner/repo'`, `host='host.io'`, `explicit_scheme='ssh'`, `reference=None`, `alias='alias'`, `ssh_user='git'`. apm-go now preserves the alias for both forms.
- [x] `ssh://alice:p%25@host.io/owner/repo`: Oracle rejects after its first decode with the exact message `Percent-encoded characters are not allowed in SSH userinfo. Use the literal username (e.g. 'ssh://myuser@host/...').` apm-go now matches this message.
- [x] SSH host charset: `ssh://host!bang/owner/repo`, `ssh://host_name/owner/repo`, and decoded `ssh://host%20name/owner/repo` — Oracle accepts with `host='host!bang'`, `host='host_name'`, and `host='host name'` respectively, each with `repo_url='owner/repo'`, `explicit_scheme='ssh'`, `reference=None`, `alias=None`, `ssh_user='git'`; apm-go now accepts the same inputs.

The rows above were probed first against Oracle commit `c8d6cdec` and then added to `tools/depref_conformance_gen.py`; the regenerated `spec/conformance/depref-accept.json` records all six rows, including the exact rejection text for the percent-userinfo case. `go test ./internal/manifest -run TestParseDepString_OracleConformance -count=1` passes.

## Backlog round 2

- [x] SCP port rejection: `git@host.io:2222/owner/repo` and the no-path, `.git`, `#ref`, and `@alias` variants now match the Oracle's actionable errors; `0`, `65536`, and non-numeric first segments retain the Oracle's fall-through behavior.
- [x] Bare shorthand `@alias` rejection: the exact migration message, printable preview sanitization/truncation, explicit URL/SCP pass-through, and version-suffix exception are covered. The pinned Oracle rejects `v1.0.1-rc.1+build` because its `_REF_VERSION_SUFFIX_RE` does not admit a second separator; this is recorded despite the verifier brief's broader boundary wording.
- [x] Virtual file extension whitelist: dependency references now accept only `.prompt.md`, `.instructions.md`, and `.agent.md`, with exact generic and legacy `.collection.yml` rejection messages. `.chatmode.md` handling inside compiled packages remains in compile/deploy code and is intentionally unchanged.
- [x] Artifactory VCS prefix: `artifactory/{repo-key}` is parsed into `ArtifactoryPrefix`, removed from `RepoURL`, and restored for clone URLs across shorthand and URL forms, including case-insensitive detection and the three-segment non-match.
- [x] Azure DevOps segment handling: `dev.azure.com` and legacy `*.visualstudio.com` forms normalize `_git`, organization, project, repository, and virtual tails identically for shorthand and HTTPS inputs; the former known-gap rows are settled.
- [x] GitLab nested groups: extensionless 3-, 4-, and 5-segment paths remain repository paths; recognized virtual-file tails split at the Oracle's observed boundary, including the generic-host comparison. The former known-gap row is settled.
