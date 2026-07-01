# Registry HTTP Consumer + credsec wiring

## Goal

Give apm-go a real registry download consumer so the Phase-5 credential controls
(`internal/credsec`: sc-003/005/007/008) are **enforced at runtime on an actual
authenticated HTTP request**, not left as unwired primitives. The consumer fetches
a package archive from an APM REST registry (`GET /v1/packages/{owner}/{repo}/versions`
+ `.../{version}/download`), verifies its SHA-256 (lk-013), safe-extracts it (sc-002/004),
and records a lockfile v2 registry entry. Correctness is proven by an A/B test against
the original apm-cli 0.21.0 over a **shared local conformant server** (decision B,
substrate verified — see `research/p1-substrate-gate.md`).

## Scope

**In scope** — the apm **package registry** consumer only:
- `source: registry` dependencies (apm.yml `id:` object form). This is the sole
  dependency kind that reaches the HTTPS archive fetch where credsec applies.
- URL resolution from the `registries:` block, per-host-class credential attach,
  non-https gate, cross-host-class redirect credential drop, secret redaction,
  hash-verify, safe-extract, lockfile v2 write, and lockfile-replay re-install.

**Out of scope** (record as explicitly deferred, not silently dropped):
- Marketplace source resolution (`KindMarketplace` — different, credential-less path).
- MCP-registry resolution (different protocol: `MCP_REGISTRY_URL`).
- Registry **server** wire (`PUT`/publish — v0.2 per spec).
- Full-catalogue semver range matching beyond a single exact-version pick (thin
  resolution; see design).
- Default-registry rerouting of bare `owner/repo#ref` shorthand — an original-apm
  experimental behavior **beyond OpenAPM v0.1**; apm-go stays spec-correct
  (`ClassifyReference` is a deterministic function of the entry alone, req-rs-008)
  and must NOT reroute. Do not A/B against that out-of-spec behavior.

## Requirements

- **R1 (sc-008 attach + env resolution).** For a `source: registry` dep whose
  registry is `https://` (or `http://` loopback/insecure), attach the credential
  resolved from `APM_REGISTRY_TOKEN_{NAME}` (Bearer) or
  `APM_REGISTRY_USER_{NAME}`+`APM_REGISTRY_PASS_{NAME}` (Basic) — `{NAME}` =
  registry name uppercased, `-`/`.`→`_`. Bearer wins when both set. No token →
  anonymous request.
- **R2 (sc-008 gate — base AND redirect target).** Never attach a credential to a
  non-`https` URL unless the host is loopback or the registry is marked
  `insecure: true`. A non-loopback `http://` registry must not receive credentials.
  **This gate also applies to redirect targets:** a redirect (even same-host) to a
  non-loopback `http://` URL must have credential headers dropped before the request
  is sent — otherwise a same-host `https→http` downgrade leaks the token over
  plaintext (the `NewAuthDropRedirect` host-class check alone does not catch this).
- **R3 (sc-003 drop).** On a redirect that crosses host-class (eTLD+1 differs), the
  credential headers (`Authorization`, `Proxy-Authorization`, `Cookie`) must not be
  sent to the redirect target. A same-host redirect retains them **only when the
  target still passes the R2 non-https gate**.
- **R4 (sc-007 redaction).** No credential value (token/basic secret) may appear in
  any error, log, or diagnostic — including 401/403 remediation output.
- **R5 (lk-013 integrity).** Verify the downloaded archive bytes' SHA-256 against
  the registry-advertised digest (fresh install) or the lockfile `resolved_hash`
  (replay) **before** extraction. Mismatch fails closed, no extraction.
- **R6 (safe-extract).** Extraction goes through `internal/archive.SafeExtract`
  (path/link/size/entry guards) into `apm_modules/`, `Contained`-guarded.
- **R7 (lockfile v2).** A registry install writes `source: registry`, `version`,
  `resolved_url`, `resolved_hash` for the dep; re-install replays from
  `resolved_url` and re-verifies against `resolved_hash` (not the registry name).
- **R8 (thin resolution).** Fresh install performs one `/versions` call, picks the
  exact requested version, then `/download`. No client-side range matching required
  for v0.1 acceptance (exact-version selector).
- **R9 (parser gaps closed).** The `id:` object form must carry a registry name
  (`registry:` key, else effective default) and accept a `version:` key (docs use
  `version:`; current parser reads only `ref`).
- **R10 (experimental gate — parity with original).** Live registry access is
  experimental (API may change substantially), matching the original apm-cli's
  `apm experimental enable registries`. apm-go MUST refuse any live registry HTTP
  resolution (fresh `/versions`+`/download`, frozen network replay) with a
  remediation hint unless the `registries` flag is enabled, via an `apm experimental
  enable|disable|list` command persisted to user config. The gate covers network
  access ONLY — it MUST NOT gate `registries:` block parsing, lockfile v2 schema, or
  frozen OFFLINE archive verify/extract, all of which the OpenAPM v0.1 oracle requires
  unconditionally (mf-014/015, sc-006, lk-002/003/013, sc-002/004). Per the original's
  invariant, the flag gates feature AVAILABILITY, never a security control.

## Constraints

- OpenAPM v0.1 oracle is immutable/read-only; production code changes to conform.
- Verification MUST be external: original apm-cli 0.21.0 as A/B oracle and/or an
  independent sub-agent / `codex exec` — never self-verification.
- Reuse existing primitives: `internal/credsec`, `internal/archive`,
  `internal/lockfile.VerifyArchiveHash`. No new HTTP or crypto deps beyond stdlib.
- Local dev/test http on loopback only; no production plain-http.

## Acceptance Criteria

Each AC is a falsifiable, server-side (or lockfile) observation on the real
download path. A/B = apm-go vs original apm-cli 0.21.0 against the same server.

- [ ] **AC1 attach.** With `APM_REGISTRY_TOKEN_{NAME}` set, the server records
  `Authorization: Bearer <token>` on `GET .../download` for apm-go, matching the
  original client. With no token, both send no `Authorization`.
- [ ] **AC2 host-class drop.** A `/download` that 30x-redirects to a different
  host-class (second loopback IP) records **no** `Authorization` on the second
  host for apm-go; a same-host redirect retains it.
- [ ] **AC3 hash parity.** apm-go's `resolved_hash` == original apm-cli's
  `resolved_hash` == `sha256(archive bytes)`; both extract identical bytes. A
  server serving mutated bytes (digest mismatch) makes apm-go fail closed before
  extraction.
- [ ] **AC4 redaction.** Forcing a 401, apm-go's stderr contains a remediation hint
  but **not** the token value.
- [ ] **AC5 non-https gate (base + redirect).** apm-go refuses to attach a credential
  to a non-loopback `http://` registry (and attaches for loopback/`insecure`); AND on
  a same-host `https://`→non-loopback-`http://` redirect the http target records **no**
  `Authorization` (server-side assertion).
- [ ] **AC6 lockfile v2 + replay.** Fresh install writes v2 registry fields; a
  second `--frozen`/replay install fetches `resolved_url`, re-verifies
  `resolved_hash`, and succeeds without re-querying `/versions`.
- [ ] **AC8 experimental gate.** With the `registries` flag off, a fresh registry
  install and a frozen network replay are both refused with a hint pointing at
  `apm experimental enable registries`; with it on, both proceed. Frozen OFFLINE
  archive extraction and registries/lockfile parsing succeed regardless of the flag
  (oracle-required). `apm experimental list/enable/disable` reflect state.
- [ ] **AC7 conformance/regression.** `go test ./...` and `go vet ./...` clean;
  the existing offline/frozen registry path stays green — specifically
  `cmd/apm/security_test.go` `TestFrozen_RegistryExtract_EndToEnd`,
  `TestFrozen_HashMismatch_NoExtract`, `TestFrozen_Good_VerifiesAndExtracts`;
  coverage of the new `internal/registry` package ≥ 80%.

## Notes

- The docs contain **no reachable registry** (all `example.com`/`acme/*`
  placeholders); the git sample `microsoft/apm-sample-package@v1.0.0` exercises the
  git path only. Hence the local conformant server is the A/B substrate.
- Reuse `scratchpad/reg-probe/server.py` + fixture as the seed for the Go test
  harness; build the server to registry-http-api.md §9 fixtures, validate it, then
  run both clients (avoid an echo chamber).
