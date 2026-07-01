# Codex external review of prd/design/implement — resolutions

Reviewer: `codex exec` (codex-cli 0.142.4), read-only, cross-checked every concrete
claim against apm-go source + apm docs. Full raw output:
`scratchpad/codex-review-out.md`. Verdict: **implementable after correcting the
redirect-target non-HTTPS gate and the VerifyArchiveHash / alias-contract mismatches.**
All findings independently re-verified against code before applying.

| # | sev | finding | evidence | resolution |
|---|-----|---------|----------|------------|
| 1 | HIGH | same-host `https→http` downgrade redirect KEEPS `Authorization` — `NewAuthDropRedirect` checks host-class only, not scheme; stdlib copies creds on same host | `redirect.go:30-40` (host-class only), `attach.go` gate applied to base URL only | design §2.1/§5.1: **composed CheckRedirect** also drops when `!ShouldAttachCredential(target)`. PRD R2/R3 + AC5b added; verified in code (`redirect.go:36` no scheme check) |
| 2 | HIGH | `VerifyArchiveHash(bytes, digest)` call shape does not exist — it takes a **file path** (`HashFileBytes`) | `internal/lockfile/hash.go:95-104` | design §2.2 + implement Step 3: add `lockfile.VerifyArchiveBytes(b, envelope)`; `VerifyArchiveHash` wraps it; SafeExtract consumes `bytes.NewReader` |
| 3 | MED | `NewAuthDropRedirect` needs `map[string][]string`, design passed `[]string`; won't compile | `redirect.go:30`, `manifest.go:28-31` (Aliases is `[]string`) | design §2.1 + implement Step 2: `NewClient(... aliases map[string][]string ...)`; build `{primaryHost: aliases}` from `Registry.Aliases`. Verified sig in code |
| 4 | LOW | "127.0.0.2 may not be loopback" caveat already answered | `attach.go:33-34` (`net.IP.IsLoopback()` = 127/8), publicsuffix per-IP class | design §8 marked RESOLVED; implement Step 0 → regression tests only. Verified in code |
| 5 | LOW | research note misattributed `utils/archive.py` HTTPS-only to the registry consumer | `deps/registry/client.py:169-175`, `resolver.py:298-314` (no https gate) | `reference-and-scope.md` corrected: registry client accepts configured http; sc-008 gate is a stricter divergence, not parity |
| 6 | LOW | "§9 fixtures" pulls PUT/publish into scope | `registry-http-api.md:417-443` (§9 has PUT/immutability) | design §4 + implement Step 6: **GET-only subset** (§3.1/§3.2 + auth/error); publish excluded |

## Claims codex confirmed correct (no change)
1,2,5,6,8: no `RegistryName` field today (`depref.go:12-27`); `id:` reads `ref`
(`depref.go:347-353`); `parseRegistries` skips `default` (`manifest.go:221-232`);
`LockedDep` already has Source/Version/ResolvedURL/ResolvedHash (`types.go:7-16`);
`PackageLoader` returns only `(*Manifest, error)` → sink justified (`types.go:16-18`);
frozen path is local `<basename>.tar.gz` only, no HTTP today (`install.go:162-196`);
original wire + Bearer-over-Basic + env-var auth all match docs.

## AC judgment (codex)
7 ACs falsifiable via header logs / lockfile / stderr. AC2 two-loopback-IP trick VALID
(different host classes, both loopback attach). AC5 extended to redirect downgrade
(AC5b). AC7 now names the specific frozen regression tests.
