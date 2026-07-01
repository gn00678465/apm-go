# Design — Registry HTTP Consumer + credsec wiring

## 1. Boundaries

New package **`internal/registry`** owns all HTTP + credential logic. It depends on
`internal/credsec` (attach/gate/drop/redact), `internal/archive` (SafeExtract),
`internal/lockfile` (VerifyArchiveHash / hash-envelope). Nothing in `internal/registry`
knows about the resolver graph or cobra — it is a pure unit (accept interfaces,
return structs).

Wiring lives at the install layer (`cmd/apm/install.go`) via a **composite loader**:
git deps keep going through `gitops.RealPackageLoader`; `source: registry` deps go
through the new registry loader. Parser changes live in `internal/manifest`.

```
apm.yml (id: dep) ─► manifest parse (RegistryName+Version) ─► resolver.Resolve
                                                                  │ loader.LoadPackage
                                                                  ▼
                                    registryLoader ── internal/registry.Client
                                        │  list_versions → pick exact → download
                                        │  credsec: attach + gate + redirect-drop + redact
                                        │  VerifyArchiveHash (lk-013) → SafeExtract (sc-002/004)
                                        ▼  record RegistryResolution{url,hash,version} in sink
install.go lockfile build ◄── read sink ── writes source/version/resolved_url/resolved_hash
```

## 2. Contracts

### 2.1 `internal/registry`
```go
type Client struct { /* baseURL, *http.Client, auth */ }
// aliases is credsec's host-class map (map[primaryHost][]alias); build it from
// Registry.Aliases as {primaryHost: aliases} — NewAuthDropRedirect takes
// map[string][]string, NOT []string (redirect.go:30).
func NewClient(base string, auth Credential, aliases map[string][]string, insecure bool) (*Client, error)

// GET /v1/packages/{owner}/{repo}/versions
func (c *Client) ListVersions(owner, repo string) ([]VersionEntry, error)
// GET /v1/packages/{owner}/{repo}/versions/{version}/download → bytes, contentType
func (c *Client) Download(owner, repo, version string) ([]byte, string, error)
// absolute-URL fetch for lockfile replay
func (c *Client) FetchURL(rawURL string) ([]byte, string, error)
func (c *Client) ArchiveURL(owner, repo, version string) string

type VersionEntry struct { Version, Digest, PublishedAt string }
type Credential struct { Scheme string /* "bearer"|"basic"|"" */; Value string }
```
- `*http.Client` uses a **composed** `CheckRedirect` (registry-local wrapper), which
  drops `credentialHeaders` when EITHER `!SameHostClass(origin, target, aliases)`
  (sc-003 cross-class, via `NewAuthDropRedirect`'s logic) OR
  `!ShouldAttachCredential(target, insecure)` (sc-008 non-https gate on the redirect
  target — catches same-host `https→http` downgrade, which the host-class check alone
  misses). No custom RoundTripper (see §5.1).
- Credential attach: `Authorization` header set on each request **only when**
  `credsec.ShouldAttachCredential(baseURL, insecure)` is true (sc-008 gate, R2). The
  redirect gate above re-applies the same predicate to every hop's target.
- Redaction: errors are built with `credsec.Redactor`; the raw token never enters an
  error/log string (sc-007, R4). Env-var resolution + `{NAME}` derivation lives in
  `internal/registry/auth.go` (`ResolveCredential(registryName) Credential`).

### 2.2 registry loader (`cmd/apm/` or `internal/registry`)
Implements `resolver.PackageLoader`. For `ref.Source=="registry"`:
1. `owner,repo = split(ref.RepoURL)`; `base = registries[ref.RegistryName]`.
2. `ListVersions` → pick the entry whose `Version == ref.Reference` (exact; R8).
3. `Download` → verify the in-memory bytes' SHA-256 against `entry.Digest` (lk-013,
   R5) → `SafeExtract(bytes.NewReader(b), ...)` into `apm_modules/<key>` (sc-002/004,
   R6, `Contained`-guarded). NOTE: `lockfile.VerifyArchiveHash` takes a **file path**
   (`hash.go` → `HashFileBytes`), not bytes — add a small
   `lockfile.VerifyArchiveBytes(b []byte, envelope string)` helper and have
   `VerifyArchiveHash` wrap it (avoids a temp-file round-trip; SafeExtract already
   consumes an `io.Reader`).
4. Parse extracted `apm.yml` → return sub-manifest (transitive deps) or nil.
5. Record `RegistryResolution{ResolvedURL: ArchiveURL(...), ResolvedHash: "sha256:"+hex,
   Version}` into a sink keyed by `ref` unique key.
Non-registry refs delegate to the wrapped git loader.

### 2.3 manifest parser (`internal/manifest/depref.go`, `manifest.go`)
- `DependencyReference` gains `RegistryName string` and reuses `Reference` for the
  version selector.
- `ParseDepDict` `id:` branch: read `registry:` (→ RegistryName), accept `version:`
  **or** `ref:` (→ Reference). `Source="registry"` unchanged.
- `Manifest` gains `DefaultRegistry string`; `parseRegistries` captures the `default:`
  key (currently skipped). Effective RegistryName = dep's `registry:` else
  `DefaultRegistry`. If still empty → error (unconfigured default).

### 2.4 lockfile
Reuse existing `LockedDep` fields `Source`, `Version`, `ResolvedURL`, `ResolvedHash`
(already present — frozen path reads them). Install lockfile-build loop fills them for
`KindRegistry` deps from the sink. Replay path reuses the existing frozen block
(install.go:162) but gains a **network fetch** when no local `<basename>.tar.gz`
exists: `FetchURL(resolved_url)` → `VerifyArchiveHash(resolved_hash)` → SafeExtract.

## 3. Data flow — fresh vs replay

- **Fresh** (`apm install`, dep in apm.yml, not in lock): resolver → registryLoader →
  list/pick/download/verify/extract → sink → lockfile v2 written.
- **Replay** (`apm install --frozen`, or lock present): read `resolved_url` +
  `resolved_hash` from lock → `FetchURL` → verify against **lockfile** hash (not API
  digest) → SafeExtract. `resolved_url` is the trust anchor, not the registry name.

## 4. A/B verification substrate (decision B)

Go test harness (`internal/registry` or `cmd/apm`), using `net/http/httptest` **or** a
small server bound to two loopback IPs. Built to a **GET-only conformant subset** of
registry-http-api.md — §3.1 `/versions`, §3.2 `/download` (format dispatch), plus the
§2 auth and §4 error-model cases the ACs need. **Excludes** §9's `PUT`/publish,
immutability, and publish-validation fixtures (those pull the server/publish wire into
scope — out per PRD). Validated standalone, then driven by both clients. Seeded from
`scratchpad/reg-probe/server.py` (P1 proven).

- **Two host classes = `127.0.0.1` and `127.0.0.2`** (both loopback → sc-008 attaches
  to both, isolating sc-003's cross-class drop as the only variable). Verify
  `credsec.ShouldAttachCredential`/attach recognizes raw `127.0.0.2` as loopback
  before relying on it (raw-IP handling may differ from `localhost`).
- Server records received headers per request → ACs assert on them.
- A/B against original apm-cli 0.21.0 (`abtest-venv`, `apm experimental enable
  registries`) for AC1/AC3 parity; apm-go-only httptest for AC2/AC4/AC5/AC6.
- Port selection: bind `:0` and read back, or avoid Windows reserved ranges
  (`netsh …excludedportrange`); **8734–8833 etc. are reserved** (P1 hit WinError 10013).

## 5. Tradeoffs / decisions

### 5.1 No custom RoundTripper (drop-only sc-003)
sc-003 is DROP-only. Go stdlib already strips `Authorization`/`Cookie` on cross-host
redirects **before** `CheckRedirect`; our `NewAuthDropRedirect` drops on cross-host-
**class** (eTLD+1). Together they satisfy R3. KEEP-on-same-eTLD1-different-host is not
required by any req — **do not** build a RoundTripper for it. The AC2 "retain" case
uses an **identical-host** redirect (stdlib keeps it), proving the drop is conditional,
not blanket. Known limitation (documented, not built): a real registry that 401s after
a same-eTLD1 CDN host change would need KEEP; out of v0.1 scope.

But the DROP side is NOT only cross-host-class. `NewAuthDropRedirect` checks host-class
only (not scheme), so a **same-host `https→http` downgrade** would KEEP `Authorization`
and leak it over plaintext — violating the sc-008 non-https gate (R2). The composed
`CheckRedirect` (§2.1) therefore also drops credential headers when
`!ShouldAttachCredential(target, insecure)`. This is still no RoundTripper — just a
second predicate in the same `CheckRedirect` closure. AC5 covers it server-side.

### 5.2 Plain http on loopback, no TLS
TLS adds CA-injection friction for the original client and exercises no branch the
Phase-5 unit tests don't already cover. Loopback http is spec-allowed for dev and is
what sc-008's loopback gate is for. Two loopback IPs give the host-class split.

### 5.3 Thin resolution (exact version only)
v0.1 acceptance uses an exact `version:` selector: one `/versions` + exact pick +
`/download`. Full semver range matching against the catalogue is deferred (out of
scope) — it exercises no additional credsec branch.

## 6. Compatibility & divergences

- **Frozen/offline path unchanged** except the added network fallback when no local
  archive exists; existing offline-archive tests must still pass (AC7).
- **apm-go must NOT reroute bare shorthand** `owner/repo#ref` to the registry
  (`ClassifyReference` stays deterministic-from-entry, req-rs-008). Original apm's
  default-rerouting is experimental/out-of-v0.1 — the A/B uses the `id:` form on both
  sides so we never compare against that out-of-spec behavior.
- **Parser divergence closed:** docs' `id:` form uses `version:` (original reads that);
  apm-go currently reads only `ref` → R9 accepts both.
- Original registry support is experimental → A/B script runs `apm experimental enable
  registries` first (does not affect apm-go).

## 7. Rollback

Registry loader is additive (composite dispatch on `Source`); reverting the composite
wiring restores git-only behavior. Parser fields are additive. No lockfile schema
change (v2 fields already exist). Rollback = drop `internal/registry` + revert the
install.go loader construction + manifest field reads.

## 8. Risks

- ~~Raw-IP loopback detection may not treat `127.0.0.2` as loopback~~ — RESOLVED
  (codex-verified): `isLoopbackHost` → `net.ParseIP(host).IsLoopback()` covers all of
  127.0.0.0/8 (`attach.go:33-34`); `SameHostClass` treats each raw IP as its own class
  (publicsuffix). So both loopback IPs attach AND classify as different classes — AC2
  holds. Only a regression test is owed (Step 0).
- `http.Client` default follows redirects; ensure `CheckRedirect` returns
  `http.ErrUseLastResponse`? No — we WANT to follow, just with creds dropped. Confirm
  `NewAuthDropRedirect` returns `nil` (proceed) after mutating headers, not an error.
- Digest algorithm: registry advertises `sha256:<hex>`; reuse `VerifyArchiveHash`
  envelope parsing (non-sha256 fails closed, per Phase-5 S2).

## 9. Experimental gate (R10) — network-only boundary

New `internal/experimental` package: static flag registry (`registries` flag,
default off), persisted to `$APM_CONFIG_DIR/config.json` (default `~/.apm/config.json`;
env-relocatable for tests). `IsEnabled/Enable/Disable/RequireEnabled/All`. CLI:
`apm experimental list|enable|disable <flag>` (`cmd/apm/experimental.go`).

**Gate placement — network access ONLY (oracle-safety constraint):**
- Fresh install: `runInstall` scans `m.ParsedDeps`; if any `source: registry` and the
  flag is off → `RequireEnabled` error before `Resolve` (fail fast, no network).
- Frozen: the `(A2)` NETWORK replay branch (`FetchURL`) is gated; the OFFLINE
  local-archive branch is NOT.
- NOT gated: `manifest.parseRegistries` (mf-014/015, sc-006), lockfile v2 schema
  (lk-002/003), frozen offline archive verify/extract (lk-013, sc-002/004). Gating any
  of these would fail OpenAPM v0.1 oracle acceptance, which requires them
  unconditionally (the original apm-cli gates parsing too, but it is not graded against
  this oracle). This gives the same user-visible behavior (registry install refused
  until enabled) while staying conformant.

Security invariant (from the original): the flag gates AVAILABILITY, never a security
control — when enabled, hash-verify / credsec / safe-extract all still run.
