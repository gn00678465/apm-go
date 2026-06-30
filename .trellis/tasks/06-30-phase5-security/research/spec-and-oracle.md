# Phase 5 Research — Security Hardening (spec §10 + oracle + current state)

## Sources read
- `conformance-kit/acceptance-checklist.md` Phase 5 + Phase V (anti-cheat).
- `conformance-kit/oracle/EXPECTATIONS.yaml` + `oracle/integrity/*` fixtures.
- `conformance-kit/runner/run_conformance.py` (black-box driver skeleton).
- `apm/docs/src/content/docs/specs/openapm-v0.1.md` §10.3–§10.9 (normative text).
- `cmd/apm/install.go` (current `--frozen` path), `internal/lockfile/frozen.go`.

## The 8 requirements (spec-derived)

| req | kw | § | behavior |
|---|---|---|---|
| sc-001 | MUST | 10.4 | every deployed file records SHA-256 (per lk-012); re-verify on **audit**; on-disk hash ≠ recorded → content-integrity violation naming the path |
| sc-002 | MUST | 10.9 | reject archive entry whose path has `..`, is absolute, or is sym/hard link; fail closed on **first**; clean up partial extraction |
| sc-003 | MUST | 10.3 | on HTTP 3xx whose target is a **different host class** (per sc-005), drop originating `Authorization` (+ other creds) before redirect; dest-class creds MAY be re-resolved; cred scope observable in diagnostics |
| sc-004 | MUST | 10.5 | container MUST be tar.gz (`application/gzip`); reject `application/zip`; uncompressed size cap default **100 MB**; entry-count cap default **10,000**; violations fail closed **before** extraction |
| sc-005 | MUST | 10.3 | two hosts = same class **iff** identical eTLD+1 (Public Suffix List) **or** explicit `registries.<n>.aliases`; MUST NOT collapse via DNS CNAME / TLS SAN / HTTP redirect. Example: `github.contoso.com` ~ `contoso.com`, ≠ `github.com` |
| sc-007 | MUST | 10.3 | creds (token/basic-auth/bearer) MUST NOT appear in diagnostic/log/error/packed bundle/lockfile/audit; refer by **source descriptor** (e.g. `GITHUB_APM_PAT env var`); Producer pack MUST refuse files matching secret-pattern set (default `.env`,`.env.*`,`*.pem`,`*.key`,`id_rsa`,`id_ed25519`; policy-extensible) |
| sc-008 | SHOULD | 10.3 | refuse attaching a cred to a git-over-HTTP fetch whose scheme ≠ `https://`, unless target is loopback (`127.0.0.0/8`,`::1`) or registry `insecure:true` |

(sc-006 is Phase 1 — `registries.<n>.url` http-scheme reject; verify already passing.)

## Oracle contract (immutable; read-only to implementer)

`EXPECTATIONS.yaml` integrity group — driven black-box by `run_conformance.py`:

| fixture | outcome | reqs | notes |
|---|---|---|---|
| `good.frozen.yaml` | accept | lk-006, lk-013 | registry entry, `resolved_hash` == sha256(good.tar.gz). Runner uses `validate` (has lockfile_version) |
| `bare-hex-reader.frozen.yaml` | accept | lk-016 | bare 64-hex hash tolerated as sha256 |
| `hash-mismatch.frozen.yaml` | fail_closed | lk-013 | `resolved_hash` = 0000…; archive good.tar.gz in CWD; must_contain `expected`+`actual`; **must_not_extract** |
| `deployed-file-mismatch.frozen.yaml` | fail_closed | lk-017, **sc-001** | git entry + workspace with TAMPERED `.github/instructions/demo.instructions.md`; must_contain that path |
| `zip-slip.tar.gz` | fail_closed | **sc-002** | must_contain `..` |
| `symlink-escape.tar.gz` | fail_closed | **sc-002** | must_contain `link` |
| `four-entry.tar.gz` | fail_closed | **sc-004** | `run_with: --max-entries 3` |

Real tarballs exist on disk: `oracle/integrity/{good,zip-slip,symlink-escape,four-entry}.tar.gz`. No hostclass/credential fixtures — those reqs are spec-derived unit tests.

## Load-bearing current-state gaps (`install.go` frozen path)

The integrity fixtures provide **only `apm.lock.yaml` + on-disk files** — never `apm.yml`, never an `apm_modules/` checkout. Current frozen install fails this 3 ways:
1. `install.go:56` reads `apm.yml` unconditionally → every integrity fixture dies at "read apm.yml" before any verify.
2. `install.go:124-125` frozen **skips registry deps** → hash-mismatch never verified.
3. `install.go:147-157` re-downloads the (fake) git commit and verifies `tree_sha256` against a nonexistent `apm_modules/` **before** the deployed-hash check at `:160` → deployed-file-mismatch fails with the wrong error (missing the required path substring).

⇒ Core integration work = **restructure frozen-install so disk-only integrity verification (deployed-file + registry-archive) runs from lockfile+disk, ungated by manifest parsing or source re-fetch**, plus add `apm audit`.

## Decisions (confirmed with advisor)

- **Build all 7 sc-reqs now**; the no-fixture ones (sc-003/005/007/008) are **pure decision functions**, not HTTP infra:
  - sc-005 hostclass = pure fn (PSL embedded list); test from spec example.
  - sc-003 = `http.Client.CheckRedirect` callback over sc-005; test with `httptest` 3xx across class. **No downloader built.**
  - sc-008 = pure predicate `ShouldAttachCredential(scheme,host,insecure)`.
  - sc-007 = redactor + secret-pattern path matcher; **Producer-pack consumer is Phase 7** — build+test matcher now, wire to pack later.
- **Do NOT build a registry HTTP download pipeline.** Archive path operates on local/already-present bytes (registry HTTP wire is v0.2-deferred anyway).
- Phase 5 **necessarily completes** lk-013 / lk-016-reader / lk-017-for-registry archive verification that Phase 3 left unwired (no archive pipeline then). Phase 5 touches `lk-*`, not only `sc-*`. (User approved full scope.)
- **Offline archive resolution (impl-defined):** registry archive looked up in CWD as `<basename(repo_url)>.tar.gz` (`registry.example.com/demo/good` → `good.tar.gz`). Deterministic; no runner change; consistent with v0.1 having no registry HTTP wire.
- **PSL dependency:** add `golang.org/x/net/publicsuffix` (embedded list; offline+deterministic). Sanctioned by checklist Impl note; do NOT hand-roll "last two segments".

## Verification strategy (anti-cheat, Phase V)

- **Primary = native `go test` against the immutable oracle** (Phase 4-T pattern): load real `oracle/integrity/*.tar.gz`, assert `SafeExtract` fails closed with EXPECTATIONS substrings; replay frozen fixtures through `install --frozen` / `audit`.
- Assert the **substring**, not just exit code (four-entry currently false-passes because `--max-entries` is an unknown flag → cobra non-zero exit for the wrong reason; the flag must be real, default 10,000).
- The python runner is a **DRIVER SKELETON** (its header sanctions wiring the CONTRACT command lines) living outside `oracle/`. Before touching it: confirm it is **not** under `oracle/CHECKSUMS.sha256`; only ever change command *invocation*, never `must_contain`/outcomes (Phase V.G player-≠-judge).
- External verification: Codex (`codex exec`) black-box + opus sub-agent (never self-verify).

## Traps to design against
- sc-002 "partial extractions MUST be cleaned up" → extract to staging dir, rename/commit on success; `RemoveAll` staging on any failure.
- Container detection: gzip magic `1f 8b`; reject zip magic `50 4b` naming `application/zip`.
- Size cap is **uncompressed** (guard the decompressed stream, not the file size) — defend against gzip bombs by counting bytes during copy with a hard limit.
