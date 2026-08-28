# 16 — dep-parser full Oracle conformance (spun out of ticket 11)

**What to build:** Complete semantic equivalence between `manifest.ParseDepString` (+ `pythonRepr*` diagnostics rendering) and the pinned Oracle's `DependencyReference.parse` / CPython `repr()`, driven entirely by the checked-in conformance tables (`spec/conformance/depref-accept.json`, 214 rows; `spec/conformance/python-repr.json`, 46 rows + 276-cp isspace sweep) and their generator (`tools/depref_conformance_gen.py`).

**Blocked by:** nothing. **Status:** open — table-driven, incremental.

**Origin:** ticket 11 attempts 2-8. Ticket 11's own ACs (M-06: Structure check row + help drift) were satisfied by attempt 2-3 — the corpus (fields, waived) tuples have been byte-identical from attempt 3 through attempt 8 — but evaluation kept probing ever-deeper `DependencyReference.parse` boundaries (per-element diagnostics -> coordinate grammar -> percent/FQDN -> urlsplit netloc/userinfo/ports -> maximal-subpart UTF-8 -> python repr byte-equality), each finding real but pre-existing install-side parser gaps that surface through `marketplace validate`'s Structure check. That surface is effectively unbounded and belongs to its own ticket, per the project's standing convention (ticket 10 deferred wording to 14; search's tree gap to 05).

## Already fixed under ticket 11 (kept, nothing to redo)

Attempts 2-8 landed: per-element Structure diagnostics; coordinate grammar via the dep parser; repo/repository Python-truthiness; tag_pattern validate-time deferral; strict URL-path percent decoding with encoded shorthand preservation + FQDN host gate; true recursive python repr (ordered dicts, surrogates incl. dict keys, number lexemes, inf/-0, dup-key first-position-last-value, Python isspace set); case-insensitive http(s) schemes; urlsplit netloc semantics (userinfo/lowercase host/port 0-65535/empty port); https/ssh path asymmetry; maximal-subpart U+FFFD; Oracle ref-fragment parity (no parse-time charset gate); ssh userinfo single-split model.

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

## Re-baseline b75a02b1

The pinned Oracle moved to `b75a02b1cfab3ffa5e1952916045b6d5374090ae` (v0.29.0). Upstream commit `645a5a53da93204b0c97663821507b242753a58a` (`fix: allow percent-encoded sourceBase path segments`) introduced the strict encoded-path grammar in `reference.py`, `identity.py`, and the marketplace `yml_schema.py` path helpers.

The new `DependencyReference.parse` observations and exact errors are:

| Input | Oracle result |
|---|---|
| `owner/%72epo` | `ValueError: Invalid repository path component: %72epo` |
| `https://x.io//owner/repo` | `ValueError: Invalid repository URL path: path segments must not be empty` |
| `https://x.io/owner/repo/` | `ValueError: Invalid repository URL path: path segments must not be empty` |
| `https://x.io/owner/%2572epo` | `ValueError: Invalid repository URL path: residual percent-encoding is not allowed` |
| `https://x.io/owner//repo` | `ValueError: Invalid repository URL path: path segments must not be empty` (error type changed from `PathTraversalError`) |
| `%2e%2e/%2e%2e/etc/passwd` | `PathTraversalError: Invalid repository path '%2e%2e/%2e%2e': segment '%2e%2e' is a traversal sequence` (settled) |

The generator now also covers encoded shorthand and HTTPS segments (single/double encoding, `%2F`, `%2e`, uppercase `%5C`), empty bare/host-prefixed/HTTPS segments, and sourceBase-shaped HTTPS paths. `../../../etc/passwd` and `../secret` remain documented security known gaps because the new Oracle still accepts them as local paths while apm-go rejects relative-root escapes. The Go authoring package has no `sourceBase` parser or resolver; its documented design scope defers that larger feature, so the Oracle `parse_source_base`/`split_source_base` surface is recorded as out of scope rather than silently claimed as covered.
