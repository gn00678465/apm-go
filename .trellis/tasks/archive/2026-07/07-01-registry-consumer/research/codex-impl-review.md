# Codex implementation review + fixes

Reviewer: `codex exec` read the actual implementation (not docs) against the ACs.
Raw: `scratchpad/codex-impl-out.md`. Verdict was "not AC-complete" — 3 real defects
found, all fixed + regression-tested. All independently re-verified against code.

| # | sev | finding | fix | test |
|---|-----|---------|-----|------|
| 1 | HIGH | Frozen registry replay could silently skip verification: empty `resolved_hash` → `continue` (:169); missing `resolved_url` → `continue` (:211) — frozen could "succeed" without verifying/fetching | `install.go`: registry entry with empty `resolved_hash` → error; not-materialized + no local archive + no `resolved_url` → error (fail closed). Materialized-skip kept (deployed files already verified by A1; apm_modules is a rebuilt cache, not deployed in frozen mode) | `TestFrozen_RegistryMissingHash_FailsClosed`, `TestFrozen_RegistryNoURLNoArchive_FailsClosed` |
| 2 | HIGH | URL-embedded credentials (userinfo) bypass the attach gate + redactor and would be persisted into `resolved_url` in the lockfile (Go synthesizes Basic auth from `URL.User`) | reject `URL.User` in `manifest.parseRegistries` (base URL) and in `client.get` (every request incl. `FetchURL` replay) | `TestParseRegistries_RejectsUserinfo`, `TestClient_RefusesEmbeddedUserinfo` |
| 3 | MED | Basic-auth redactor only knew the base64 blob, not raw user/pass a server could echo decoded | `Credential.redact` carries `{user, pass, base64}`; client redactor built from `cred.Value` + `cred.redact` | `TestClient_RedactsBasicRawSecrets` |

## Codex AC verdicts (pre-fix) and post-fix status
- AC1 met (unchanged). AC2 met (unchanged). AC7 met (codex ran `go test ./...`, vet, cover).
- AC3 was "partially met" (frozen skip) → CLOSED by #1.
- AC4 "not met" (userinfo/basic leak) → CLOSED by #2 + #3.
- AC5 "not met" (userinfo bypass) → CLOSED by #2.
- AC6 "not met" (frozen skip paths) → CLOSED by #1.

## Codex re-verify (round 3) + Fix-1 residual closure
Round-3 re-verify: **Fix 2, Fix 3 VERIFIED clean**. Fix 1 was PARTIAL — the
"already materialized → continue" skip still accepted a pre-existing (possibly
tampered) `apm_modules` tree without re-verification (same latent property the git
frozen path has). Closed: the frozen registry loop now, when a trust anchor exists
(local archive or resolved_url), REMOVES any pre-existing tree and re-materializes
from freshly hash-verified bytes; it only skips the anchor-less already-materialized
case (harmless — deployed files verified by A1, cache not deployed in frozen mode).
Regression test `TestFrozen_RegistryReplacesStaleMaterializedTree` proves a stale
`STALE.txt` is removed and the verified tree re-fetched (1×/download).

## Codex final verify (round 4) — TOCTOU on offline local-archive path
Round-4 confirmed points 1/2/3/5 of the frozen restructure but flagged a **pre-existing
Phase-5 TOCTOU** (not introduced by this task, and only on the offline local-archive
branch — the registry HTTP path was already TOCTOU-free): the local branch hashed the
archive by path (`VerifyArchiveHash`), closed it, then reopened via `os.Open` for
extraction, so a swap between verify and reopen could slip unverified bytes through.
Closed anyway (security task): the local branch now `os.ReadFile`s once, verifies those
in-memory bytes with `VerifyArchiveBytes`, and extracts the SAME bytes via
`bytes.NewReader` — no reopen. Both frozen paths (local + network) are now
verify-then-extract on one in-memory buffer. Phase-5 tests still green (message still
carries expected/actual per req-lk-013).

## Codex final TOCTOU re-verify (round 4b) — CLEAN
"frozen registry replay is fully fail-closed for this claim, with no remaining
verify/extract byte-stream gap." Both local and network paths read once → verify →
extract the same in-memory buffer. Codex ran the Phase-5 frozen tests: pass.

## Post-fix state
`go test ./...` + `go vet ./...` clean; `internal/registry` coverage 84.8%. No
regression to Phase-5 frozen/offline tests. A/B parity (research/ab-evidence.md)
unaffected — fixes are fail-closed hardening on paths the happy-path A/B does not hit.

## Experimental gate (R10) — added + codex-verified
Registry access gated behind `apm experimental enable registries` (parity with original),
persisted to `$APM_CONFIG_DIR/config.json`. Codex round-5 verify: **6/6 VERIFIED**,
verdict "gate is at the correct network boundary and is oracle-safe; no oracle-required
path gated." Key: gate covers live network (fresh resolve fast-fail + frozen network
replay) only; parsing / lockfile schema / frozen offline extract stay unconditional.
Codex re-ran Phase-5 offline frozen tests with flag OFF → pass (proves oracle paths
ungated). Security invariant preserved (flag gates availability, not hash/credsec).
Coverage: `internal/experimental` 82.0%.

## External-verification loop summary (never self-verified)
- Round 1 (plan review): 3 doc mismatches (redirect gate / VerifyArchiveHash shape /
  alias type) → corrected before task.py start.
- Round 2 (impl review): 2 HIGH (frozen silent-skip, URL userinfo bypass) + 1 MED
  (Basic redaction) → fixed + tested.
- Round 3 (re-verify): Fix 2/3 clean; Fix 1 PARTIAL (materialized skip) → closed.
- Round 4 (frozen closure): materialized closure VERIFIED; found offline-archive TOCTOU
  (pre-existing Phase-5) → fixed.
- Round 4b (TOCTOU): fully fail-closed, no remaining gap. ✅ CONVERGED.
