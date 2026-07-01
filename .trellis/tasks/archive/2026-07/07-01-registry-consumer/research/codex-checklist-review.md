# Codex checklist conformance review (round 6) + fixes

Codex verified the implementation against the OpenAPM v0.1 acceptance-checklist
(registry/lockfile/security reqs) + the task ACs. Raw: `scratchpad/codex-checklist-out.md`.
Verdict was "not conformant/AC-complete yet" — 4 fixable PARTIALs + AC8 test gaps.
All addressed; PASS items below unchanged.

## PASS (codex-confirmed, no change)
req-mf-014, req-mf-015, req-sc-006, req-rs-009, req-lk-002, req-lk-003, req-sc-002,
req-sc-003, req-sc-005, req-sc-007, req-sc-008, AC6, AC7.

## Fixed PARTIALs
| item | finding | fix | test |
|---|---|---|---|
| req-lk-013 | mismatch diagnostic didn't name the entry | loader wraps `VerifyArchiveBytes` err with `ref.RepoURL`; install.go frozen local+network wrap with `dep.UniqueKey()` | `TestLoader_HashMismatch_FailsClosed` asserts entry+expected+actual |
| req-lk-016 | loader stored `chosen.Digest` verbatim → bare-hex could be re-emitted bare | normalize via `ParseHashEnvelope`→`HashEnvelope("sha256",hex)` | `TestLoader_NormalizesBareDigest` |
| req-sc-004 | client advertised `Accept: application/zip` though apm-go rejects zip | `Download`/`FetchURL` advertise `application/gzip` only (SafeExtract zip rejection unchanged) | A/B re-run shows `Accept: application/gzip` on the wire |
| AC4 | no 401/403 remediation hint | `remediateAuth` appends `APM_REGISTRY_TOKEN_<NAME>` hint (redacted) on 401/403 | `TestLoader_401_RemediationHintNoToken` (hint present, token absent) |
| AC8 | missing frozen-network-off + CLI E2E tests | added both | `TestFrozen_RegistryNetwork_RequiresExperimentalFlag`, `TestExperimentalCmd_ListEnableDisable` |

## PARTIALs judged not-a-defect (with rationale)
- **req-rs-008**: codex noted classify.go checks marketplace before git-semver/literal vs
  the comment's stated order. This is PRE-EXISTING code (not this task; only depref.go
  gained RegistryName, which codex confirmed classification ignores) and is functionally
  correct — `Source` is an explicit, mutually-exclusive field, so no input is
  misclassified. Left unchanged (surgical scope).
- **AC1/AC3 "no original apm-cli A/B evidence"**: codex reviews code/tests and cannot run
  the cross-client A/B (needs the Python apm-cli + a live server). Evidence is the
  documented live A/B (`ab-evidence.md`): identical `resolved_hash` + server-observed
  Bearer for both clients, re-confirmed 2026-07-01 with the gated+fixed binary.
- **AC2/AC5 "no real two-host server assertion"**: the drop/downgrade policy is proven
  deterministically by exercising the composed `CheckRedirect` closure directly
  (`TestClient_CheckRedirect_DropPolicy`); a two-loopback-IP live server adds fragility
  for no additional logic coverage.

## Round-7 recheck + AC4-frozen closure
Codex re-verified the 5 fixes: lk-013/lk-016/sc-004/AC8 PASS; **AC4 still PARTIAL** —
the frozen NETWORK replay path did not call the remediation helper (only the fresh
loader did). Closed: added exported `registry.RemediateFetchAuth(err, resolved_url,
registries)` (resolves the registry name from the URL) and wired it into the frozen
network branch (`cmd/apm/install.go`). Tests: `TestFrozen_Network_401_NamesEnvVar`
(hint present, token absent), `TestFrozen_Network_HashMismatch_NamesEntry`
(entry+expected+actual), `TestClient_Download_AcceptGzipOnly` (sc-004 Accept). AC4 now
closed on both fresh and frozen paths.

## Post-fix state
`go test ./...` + `go vet ./...` clean; `internal/registry` 85.4%, `internal/experimental`
82.0%. A/B parity re-confirmed with the current binary.
