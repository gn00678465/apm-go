# A/B evidence — apm-go vs original apm-cli 0.21.0 (decision B, live)

Both clients driven against the SAME local server (`scratchpad/reg-probe/server.py`,
`http://127.0.0.1:7777`, serves acme/sample@1.0.0 tar.gz, records headers), 2026-07-01.
apm-go binary: `bin/apm-go.exe` (this branch). Original: `abtest-venv` apm.exe 0.21.0.

## AC3 — resolved_hash / source / resolved_url parity (IDENTICAL)
Server digest = `sha256:79587ad4f536afdafc4ac9acbe13bb8585dab5954f034fa7f1f100c0b5f238d9`
= sha256(fixture bytes).

| field | apm-go | original 0.21.0 |
|---|---|---|
| source | `registry` | `registry` |
| resolved_url | `http://127.0.0.1:7777/v1/packages/acme/sample/versions/1.0.0/download` | same |
| resolved_hash | `sha256:79587ad4…238d9` | `sha256:79587ad4…238d9` |

Byte-for-byte identical. Both verify the same archive hash before extraction (lk-013).

## AC1 — credential attach on the real /download (server-observed)
```
/download  Authorization=Bearer ab-token-GO     (apm-go)
/download  Authorization=Bearer ab-token-ORIG   (original)
```
Both resolve `APM_REGISTRY_TOKEN_LOCAL` (registry name `local` → `_LOCAL`) and attach
Bearer on the real download over loopback http. credsec sc-008 attach is wired at
runtime, matching the original.

## Divergences observed (expected, documented)
- Original apm-cli 0.21.0 **rejects** `insecure: true` in a `registries:` entry
  ("unknown fields: ['insecure']"). apm-go accepts it (sc-006). A/B uses the common
  subset (no `insecure`); apm-go accepts loopback http without it (isLoopbackOrPrivate).

## Re-run 2 (gated + checklist-fixed binary, 2026-07-01)
Both clients again produced identical `resolved_hash`
`sha256:a74aa3732a80a859e52cb0fec0a0b653f26314f12a83cae37e662503841ac173`.
Server-observed `/download` Authorization: apm-go `Bearer tok-GO2`, original `Bearer
tok-ORIG2`. Note the sc-004 fix is visible on the wire: apm-go now advertises
`Accept: application/gzip` (gzip-only; it rejects zip), vs the original's
`application/gzip, application/zip`. Confirms AC1/AC3 parity with the current binary.

## Experimental-gate parity (added after A/B, R10)
apm-go now also gates live registry access behind `apm experimental enable registries`
(persisted to `$APM_CONFIG_DIR/config.json`), matching the original. Verified via the
built binary: flag OFF → `Error: experimental feature "registries" is not enabled; enable
it with: apm experimental enable registries`; after `apm-go experimental enable registries`
→ install succeeds, lockfile `source: registry` + `resolved_hash`. Both clients are now
symmetric (each requires enabling its flag). apm-go gates network access only — offline
frozen extraction + registries/lockfile parsing stay unconditional (oracle-required).

## Coverage of remaining ACs (Go tests, this branch)
- AC2 host-class drop + AC5b downgrade drop: `internal/registry` `TestClient_CheckRedirect_DropPolicy`.
- AC4 401 redaction: `TestClient_401_RedactsToken`.
- AC5a non-loopback-http gate: `TestClient_NoAttach_NonLoopbackHTTP`.
- AC3 negative (mutated bytes fail closed): `TestLoader_HashMismatch_FailsClosed`.
- AC1/AC3/AC6 end-to-end (real pipeline): `cmd/apm` `TestRegistryInstall_EndToEnd`.
- AC7: `go test ./...` + `go vet ./...` clean; `internal/registry` coverage 84.6%.
