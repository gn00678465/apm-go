# Research — Registry HTTP Consumer + credsec wiring

## Why this task exists
User rejected deferring the Phase 5 credential controls (sc-003/005/007/008) as unwired
primitives ("B. 不接受"). apm-go currently makes **zero** authenticated HTTP requests — all git
ops go through the `git` CLI (`git clone`/`git ls-remote`); the only `net/http` in the repo is the
credsec primitive itself. So the credential controls have **no consumer to protect**. The natural
(and spec-defined) consumer is the **registry archive download over HTTPS**, which apm-go stubbed.
This task builds that Consumer path and wires credsec so the controls are enforced at runtime, then
A/B-verifies against the original apm-cli ("C. 必須對照真實 registry 存取").

## Reference: original apm-cli (v0.21.0) registry Consumer
- **Wire**: `GET /v1/packages/{owner}/{repo}/versions/{version}/download` → body is `application/gzip`
  (tar.gz) or `application/zip`; client dispatches on Content-Type. `list_versions` →
  `/v1/packages/{owner}/{repo}/versions`. `resolved_url` = the `.../download` URL.
- **Auth** (`deps/registry/auth.py`): `APM_REGISTRY_TOKEN_{NAME}` → `Authorization: Bearer <token>`;
  or `APM_REGISTRY_USER/PASS_{NAME}` → Basic. `{NAME}` = uppercased registry name, `-`/`.`→`_`.
  Anonymous first; remediation only on 401/403. **Credential referenced by env-var source
  descriptor, never by literal (== req-sc-007).**
- **Host/redirect safety** — CORRECTION (codex): the HTTPS-only / cookie-clearing
  `_archive_get` in `utils/archive.py:342-347` is the **generic git/tarball** helper,
  NOT the registry consumer. The registry path uses `RegistryClient.download_archive`
  (`deps/registry/client.py:169-175,237-242`) + in-memory extractor
  (`resolver.py:298-314`), which sends requests to the configured base URL **with no
  HTTPS-only gate** — it accepts `http://` (matches the verified P1 local-http
  substrate). So apm-go's sc-008 non-https gate is a deliberate *stricter* divergence,
  not a parity requirement.
- **Integrity**: caller verifies `sha256` of bytes before extract (== req-lk-013); safe-extract
  (path traversal / symlink / caps) — already matched by apm-go `internal/archive.SafeExtract`.
- **Registries are configured**, not defaulted: `apm.yml` `registries:` block maps name→URL. A
  registry dep needs a `registry_name`; "unconfigured default registry" is an error.
  ⇒ **There is NO built-in public default registry.** The C A/B target must be a concrete
  registry endpoint; resolve it at the A/B step (user provides, or a local stand-in mirrors the wire).

## Spec reqs this task must satisfy (Consumer class)
- req-rs-009 (§7.5.1): registry dep satisfied by any registry/mirror **iff** bytes hash ==
  `resolved_hash`; `resolved_url` advisory (URL mismatch ≠ failure); hash mismatch fail-closed.
- req-lk-002/003 (§5.2/5.4): lockfile v2 for registry entries; `repo_url` + `resolved_url` +
  `resolved_hash` (+ `version`).
- req-lk-013 (§5.2): verify archive bytes' SHA-256 before extraction. — HAVE `VerifyArchiveHash`.
- req-sc-003/005/007/008 (§10.3): credential host-class scoping, cross-class redirect drop,
  redaction, non-https refusal. — HAVE credsec primitives; **wire them here**.
- req-sc-004 / sc-002 (§10.5/10.9): container/caps + traversal. — HAVE `SafeExtract`.
- req-mf-014 / sc-006 (§4.2.3): `registries.<n>.url` scheme. — HAVE (manifest.go).

## What apm-go already has (reuse)
- `internal/credsec`: HostClass/SameHostClass (sc-005), NewAuthDropRedirect (sc-003),
  ShouldAttachCredential (sc-008), Redactor + MatchesSecretPattern (sc-007).
- `internal/archive.SafeExtract` (sc-002/004), `internal/archive.Contained`.
- `internal/lockfile.VerifyArchiveHash` (lk-013), hash envelope parsing (lk-016).
- `manifest.Registries` (name→URL, insecure) parsed (sc-006 scheme check).
- `resolver.KindRegistry` classification; frozen-install offline archive verify (Phase 5).

## Gap to build
1. `internal/registry`: HTTP client — resolve URL from `apm.yml` registries block; attach creds
   per host-class via `APM_REGISTRY_TOKEN_{NAME}` (Bearer)/USER+PASS (Basic); `http.Client` with
   `NewAuthDropRedirect` (sc-003) + `ShouldAttachCredential` gate (sc-008); redact creds in all
   diagnostics (sc-007); `GET .../download`; return bytes+content-type.
2. Wire into install/resolver: registry-source dep → (list versions / use lockfile resolved_url) →
   download → `VerifyArchiveHash` (lk-013) → `SafeExtract` → deploy; write lockfile v2 entry.
3. A/B: same registry dep + same registry endpoint through apm-go vs original apm-cli; compare
   resolved_hash, downloaded bytes, credential handling, redirect/host-class behavior.

## Open decisions for PRD/advisor
- **Scope depth**: full registry install (list_versions + semver pick + lockfile v2 write) vs
  download-by-recorded-url (lockfile replay path) only. The latter is smaller and still exercises
  all of credsec + lk-013; the former is the complete Consumer.
- **A/B registry**: no public default exists. Need a concrete endpoint. Option: stand up a local
  static registry serving the `/v1/packages/.../download` wire (real HTTPS via httptest/TLS) to
  A/B both clients deterministically, plus (if user provides) a real remote registry.
- **v0.2 boundary**: spec reserves the registry *server* wire (req-rg-001) for v0.2, but the
  *Consumer* download behavior is in-scope v0.1. Stay Consumer-only; do not implement a server.
