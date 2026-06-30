# Phase 5: Security Hardening

## Overview

Phase 5 adds cross-cutting security controls over parse / download / extract / deploy:
archive path-traversal & resource-cap enforcement, deployed-file integrity audit, and
credential/host-class safety. It also **completes** the registry-archive integrity
verification (lk-013 / lk-016-reader / lk-017-for-registry) that Phase 3 specified but
left unwired because no archive pipeline existed yet.

Scope confirmed with user: **full** — all 7 `req-sc-*` reqs (sc-006 is Phase 1) plus the
lk-* registry-archive completion. No-fixture reqs are built as pure decision functions.

## Requirements

### Archive safety (§10.9 / §10.5)

| req | kw | description |
|-----|----|-------------|
| req-sc-002 | MUST | Reject any tar entry whose extracted path contains `..`, is absolute, or is a symbolic/hard link. Fail closed on the **first** such entry. Clean up any partial extraction. |
| req-sc-004 | MUST | Container MUST be tar.gz (`application/gzip`); reject `application/zip`/others. Uncompressed-size cap default **100 MB**; entry-count cap default **10,000**. Violations fail closed **before** extraction proceeds. |

### Deployed-file integrity (§10.4)

| req | kw | description |
|-----|----|-------------|
| req-sc-001 | MUST | Every deployed file has a recorded SHA-256 (per lk-012). `apm audit` (and frozen install, lk-017) re-verifies on-disk bytes; a file in `deployed_files` whose hash ≠ recorded MUST be reported as a content-integrity violation naming the path. |

### Registry-archive integrity completion (§5.2 / §10.5 — Phase 3 follow-through)

| req | kw | description |
|-----|----|-------------|
| req-lk-013 | MUST | Before extraction, verify registry archive bytes' SHA-256 == `resolved_hash`; mismatch → fail closed listing entry/expected/actual; MUST NOT (partially) extract. |
| req-lk-016 | MUST | Read tolerance: bare 64-char hex == `sha256:<hex>`; frozen install accepts it. (Writer already emits envelope.) |
| req-lk-017 | MUST | Frozen install re-verifies every `deployed_file_hashes` / `local_deployed_file_hashes` against disk; mismatch fail-closed listing path/expected/observed. (Shared with sc-001.) |

### Credential / host-class safety (§10.3)

| req | kw | description |
|-----|----|-------------|
| req-sc-005 | MUST | Two hosts share a host class **iff** identical eTLD+1 (Public Suffix List) **or** an explicit `registries.<n>.aliases` entry. MUST NOT collapse via CNAME / TLS SAN / HTTP redirect. |
| req-sc-003 | MUST | On an HTTP 3xx whose target classifies into a different host class, drop the originating `Authorization` (and other originating-class credentials) before the redirected request. Credential scope observable in diagnostics. |
| req-sc-007 | MUST | Credentials MUST NOT appear in any diagnostic/log/error/packed bundle/lockfile/audit; reference by source descriptor. Producer pack MUST refuse files matching the secret-pattern set (default `.env`,`.env.*`,`*.pem`,`*.key`,`id_rsa`,`id_ed25519`; policy-extensible). |
| req-sc-008 | SHOULD | Refuse to attach a credential to a git-over-HTTP fetch whose scheme ≠ `https://`, unless target is loopback (`127.0.0.0/8`, `::1`) or registry `insecure:true`. |

(sc-006 — `registries.<n>.url` http-scheme reject — is Phase 1; verify it is already passing, no new work.)

## Constraints

- **Oracle is immutable & read-only.** Production code changes to pass; never edit
  `oracle/**`, `EXPECTATIONS.yaml`, or `acceptance-coverage.yml`. (Phase V anti-cheat.)
- Integrity fixtures provide **only `apm.lock.yaml` + on-disk files** — no `apm.yml`, no
  `apm_modules/`. Disk-integrity verification MUST be reachable from lockfile + disk alone.
- **No registry HTTP download pipeline** is built (registry HTTP wire is v0.2-deferred).
  Archive verification operates on local bytes; offline archive located in CWD as
  `<basename(repo_url)>.tar.gz`.
- sc-003/005/007/008 built as **decision functions / policy primitives**; their network /
  pack consumers are wired in later phases (documented deferral, not silent skip).
- PSL via `golang.org/x/net/publicsuffix` (embedded, offline). No hand-rolled approximation.
- Must not break `validate`, `normalize`, `init`, `install` (incl. Phase 4 deploy).
- Style matches codebase: small packages, table-driven tests, accept-interfaces-return-structs.
- **Verification never self-only**: native go tests vs immutable oracle + external Codex/opus.

## Acceptance Criteria

1. `internal/archive.SafeExtract` rejects zip-slip (`..`), symlink/hardlink escape (`link`),
   absolute paths; fails on first; cleans up partial extraction; verified against the real
   `oracle/integrity/zip-slip.tar.gz` & `symlink-escape.tar.gz` with required substrings.
2. SafeExtract rejects non-gzip container (names `application/zip`); enforces 100 MB / 10,000
   defaults; `--max-entries 3` makes `four-entry.tar.gz` fail closed for the **right** reason.
3. `apm audit` re-verifies deployed-file hashes from lockfile+disk and reports a
   content-integrity violation naming the path; passes `deployed-file-mismatch` fixture.
4. Frozen install verifies registry archive bytes before extract (hash-mismatch →
   expected/actual, no extract) and re-verifies deployed-file hashes; runs from lockfile+disk
   without requiring `apm.yml`; bare-hex hash tolerated.
5. `hostclass`: `github.contoso.com` ~ `contoso.com`, ≠ `github.com`; aliases honored;
   no CNAME/SAN/redirect collapse.
6. Redirect policy drops `Authorization` on cross-host-class 3xx (httptest-verified); keeps it
   same-class.
7. `ShouldAttachCredential` refuses non-https unless loopback/insecure.
8. Redactor scrubs credential values; secret-pattern matcher matches the default set; neither
   credential literals nor secret files leak into lockfile/audit/diagnostics.
9. All existing tests pass (no regressions); new tests cover each req; archive/credsec coverage ≥ 80%.
10. Verified by external sub-agent (opus) + Codex black-box, not self-verification.

## Explicitly Out of Scope

- Registry HTTP download / mirror fetch wire (v0.2; offline local archive only).
- `apm pack` Producer command (Phase 7) — sc-007 secret-pattern matcher built but pack wiring deferred.
- Wiring sc-003 redirect policy / sc-008 predicate into a live authenticated fetcher (no such
  consumer until registry-HTTP / git-auth phase) — primitives + tests delivered, consumers deferred.
- Policy-driven secret-pattern extension (`security.*` policy block) — Phase 6 governance.
- TLS-only registry wire, sigstore/attestation provenance — v0.2.
