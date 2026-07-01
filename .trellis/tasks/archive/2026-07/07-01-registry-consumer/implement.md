# Implement — Registry HTTP Consumer + credsec wiring

TDD throughout: write the failing test/AC probe first, then the minimal code.
External verification only (original apm-cli A/B + sub-agent/`codex exec`) — never
self-verify. Validation after every step: `go test ./... && go vet ./...`.

## Step 0 — Preflight (research already done)
- [ ] Loopback handling for raw `127.0.0.2` is CONFIRMED (codex): `isLoopbackHost`
      uses `net.IP.IsLoopback()` (127/8), and `SameHostClass` gives each raw IP its
      own class. No code change needed — only lock in regression tests:
      `TestShouldAttachCredential_RawLoopbackIPs` and
      `TestSameHostClass_RawIPsDifferentClass` (`SameHostClass("127.0.0.1","127.0.0.2")
      == false`).

## Step 1 — manifest parser gaps (R9)  [rollback point: additive fields]
- [ ] `DependencyReference`: add `RegistryName string`.
- [ ] `ParseDepDict` `id:` branch: read `registry:` → RegistryName; accept `version:`
      or `ref:` → Reference.
- [ ] `Manifest`: add `DefaultRegistry`; `parseRegistries` captures `default:` key.
- [ ] Effective registry-name resolution helper (dep.registry else DefaultRegistry;
      empty → error).
- → verify: `go test ./internal/manifest/` — table tests for id+registry+version,
      id+default, id+missing-default(error), version-vs-ref precedence.

## Step 2 — `internal/registry` client + auth (R1/R2/R4)  [new pkg]
- [ ] `auth.go`: `ResolveCredential(name) Credential` — `APM_REGISTRY_TOKEN_{NAME}`
      (bearer) / `USER`+`PASS` (basic); `{NAME}` uppercase, `-`/`.`→`_`; bearer wins.
- [ ] `client.go`: `NewClient(base, auth, aliases map[string][]string, insecure)`
      builds `*http.Client` with a **composed** `CheckRedirect` closure that drops
      `credentialHeaders` when `!SameHostClass(origin, target, aliases)` (sc-003) OR
      `!ShouldAttachCredential(target, insecure)` (sc-008 downgrade). Build the
      `aliases` map from `Registry.Aliases` as `{primaryHost: aliases}`. `ListVersions`,
      `Download`, `FetchURL`, `ArchiveURL`. Attach `Authorization` only when
      `credsec.ShouldAttachCredential(base, insecure)` (R2). Build all errors through
      `credsec.Redactor` (R4).
- → verify (apm-go-only, httptest): attach-when-token, no-attach-when-none,
      no-attach-non-loopback-http base (AC5a), **same-host https→non-loopback-http
      redirect drops Authorization (AC5b)**, token-absent-from-error on 401 (AC4),
      redirect-cross-class drops Authorization (AC2), same-host same-scheme redirect
      retains it.

## Step 3 — hash-verify + safe-extract path (R5/R6)  [reuse + tiny helper]
- [ ] Add `lockfile.VerifyArchiveBytes(b []byte, envelope string) error` (hashes bytes,
      reuses the existing envelope parse; non-sha256 fails closed per Phase-5 S2).
      Refactor `VerifyArchiveHash(path, …)` to read the file then delegate — no
      behavior change to the frozen/offline path (guard with existing tests).
- [ ] Registry loader verifies downloaded bytes via `VerifyArchiveBytes` against the
      advertised/locked `sha256:` envelope **before** extraction, then
      `SafeExtract(bytes.NewReader(b), apm_modules/<key>, …)` with `archive.Contained`
      guard.
- → verify: mutated-bytes → fail closed, no extraction dir created (AC3 negative);
      existing `VerifyArchiveHash` file-path tests still pass.

## Step 4 — registry loader + resolution sink  [wiring; rollback: composite dispatch]
- [ ] Registry loader implementing `resolver.PackageLoader`: registry deps →
      list/pick-exact/download/verify/extract/parse-submanifest; record
      `RegistryResolution{ResolvedURL,ResolvedHash,Version}` in a sink; delegate
      non-registry refs to the wrapped git loader.
- [ ] `cmd/apm/install.go`: construct composite loader with the merged registries map
      + sink; after `resolver.Resolve`, fill lockfile `Source/Version/ResolvedURL/
      ResolvedHash` for `KindRegistry` from the sink.
- → verify: `go test ./cmd/apm/ ./internal/resolver/` — fresh install writes v2
      registry entry (AC6 fresh).

## Step 5 — replay/frozen network fallback (R7)
- [ ] Extend the frozen registry block (install.go:162) to `FetchURL(resolved_url)` +
      `VerifyArchiveHash(resolved_hash)` + SafeExtract when no local archive exists.
- → verify: second install (lock present) fetches resolved_url, re-verifies, no
      `/versions` call (assert server log) (AC6 replay).

## Step 6 — A/B substrate + acceptance (decision B)
- [ ] Go conformant server (seed from `scratchpad/reg-probe/server.py`) built to a
      **GET-only subset**: §3.1 `/versions`, §3.2 `/download` + §2 auth + §4 error
      cases needed for the ACs. NO `PUT`/publish/immutability fixtures (out of scope).
      Validate standalone first. Add a configurable redirect endpoint for AC2/AC5b.
- [ ] Two loopback IPs `127.0.0.1`/`127.0.0.2`; free port (bind :0 or avoid reserved
      ranges).
- [ ] A/B script: same server, apm-go vs `abtest-venv` apm-cli (`experimental enable
      registries`) → assert AC1 attach parity + AC3 hash/byte parity.
- → verify: AC1–AC6 all green; capture server header logs + both lockfiles as evidence.

## Step 7 — regression + coverage (AC7)
- [ ] `go test ./... -cover` (new pkg ≥ 80%); `go vet ./...` clean; offline/frozen
      tests still pass.
- [ ] Full-scope check: every AC has evidence pointing to a test or A/B log line.

## Step 8 — external verification gate
- [ ] Independent verifier (sub-agent opus OR `codex exec`) reviews credsec wiring on
      the real download path — NOT self. Provide server logs + diffs.
- [ ] Address CRITICAL/HIGH before declaring done.

## Validation commands
```
go test ./... && go vet ./...
go test ./internal/registry/ -run . -cover
# A/B (bash tool, server needs sandbox-disabled bind):
python scratchpad/reg-probe/server.py <freeport> <log>   # or the Go server
APM_REGISTRY_TOKEN_LOCAL=... apm install --target claude  # original oracle
```

## Review gates
- After Step 2 (credsec client): mini-review — attach/gate/drop/redact all covered.
- After Step 6: A/B evidence review before Step 8.
- Step 8: mandatory external verification before finish-work.

## Rollback points
- Steps 1/2/3 additive → revert file-by-file.
- Step 4 composite loader → revert construction to git-only.
- No lockfile schema change → no data migration to unwind.
