# Phase 5 Implementation Plan

## Pre-flight
- `python conformance/conformance-kit/runner/run_conformance.py` with no APM_BIN to confirm baseline.
- Confirm `req-sc-006` already passes (Phase 1): `invalid-registry-scheme.yml` rejects `http://` with `bad`.
- Confirm `runner/run_conformance.py` is NOT listed in `oracle/CHECKSUMS.sha256` before any wiring.

## Step 1: internal/archive — SafeExtract
**Files:** `internal/archive/extract.go`
**Verify:** `go build ./internal/archive/`
- `Limits{MaxBytes, MaxEntries}` + `normalize()` (0 → 100 MB / 10000).
- Container peek (gzip `1f 8b` ok; zip `50 4b` → name `application/zip`; else unrecognized).
- gzip→tar loop: entry-count cap (before write), path guard (`..` / absolute / escapes stage),
  link guard (`tar.TypeSymlink`/`tar.TypeLink` → message contains `link`), reg-file copy with
  running uncompressed byte cap, dir mkdir.
- Staging dir + rename-on-success; `RemoveAll(stage)` on any error (partial cleanup).

## Step 2: archive tests vs oracle
**Files:** `internal/archive/extract_test.go`
**Verify:** `go test ./internal/archive/ -run TestSafeExtract`
- Load real `../../conformance/conformance-kit/oracle/integrity/*.tar.gz` (skip-if-absent like Phase 4-T).
- zip-slip → err contains `..`, nothing left on disk.
- symlink-escape → err contains `link`, nothing left.
- four-entry with `Limits{MaxEntries:3}` → fail closed (entry-count message); with default → ok.
- good.tar.gz → extracts, returns expected files, dest committed.
- Synthetic: zip container → names `application/zip`; absolute-path entry → rejected; oversize
  (small MaxBytes) → rejected. Assert SUBSTRINGS, not just error presence.

## Step 3: internal/credsec — hostclass (sc-005)
**Files:** `internal/credsec/hostclass.go`, `hostclass_test.go`
**Verify:** `go get golang.org/x/net/publicsuffix && go mod tidy && go test ./internal/credsec/ -run TestHostClass`
- `HostClass` strips port, returns eTLD+1 via `publicsuffix.EffectiveTLDPlusOne`.
- `SameHostClass(a,b,aliases)`.
- Table: `github.contoso.com`~`contoso.com`; `github.contoso.com`≠`github.com`; alias group same;
  unrelated different.

## Step 4: credsec — redirect (sc-003), attach (sc-008), redact (sc-007)
**Files:** `internal/credsec/redirect.go`, `attach.go`, `redact.go` + `_test.go`
**Verify:** `go test ./internal/credsec/`
- `NewAuthDropRedirect(aliases)` → `http.Client.CheckRedirect`; drops `Authorization` on
  cross-class hop. Test with `httptest`: server A 302 → server B (different class) asserts no
  Authorization on hop 2; same-class hop keeps it.
- `ShouldAttachCredential(rawURL, insecure)`: https→true; http+loopback→true; http+insecure→true;
  http otherwise→false.
- `Redactor.Redact` scrubs literals → `[REDACTED]`; `MatchesSecretPattern` matches
  `.env`,`.env.x`,`a.pem`,`a.key`,`id_rsa`,`id_ed25519`; rejects `readme.md`.

## Step 5: lockfile audit + archive verify
**Files:** `internal/lockfile/audit.go`, `internal/lockfile/archive.go` + tests
**Verify:** `go test ./internal/lockfile/ -run 'TestVerifyDeployedState|TestVerifyArchiveBytes'`
- `VerifyDeployedState(lock, root) []Violation` (reuse hash logic; structured path/expected/observed).
- `VerifyArchiveBytes(recorded, data)`: envelope + bare-64-hex tolerance (lk-016); mismatch err
  names `expected` + `actual`.

## Step 6: apm audit command (sc-001)
**Files:** `cmd/apm/audit.go`, register in `cmd/apm/main.go`
**Verify:** `go build ./cmd/apm`
- Reads lockfile only; runs `VerifyDeployedState`; prints `content-integrity violation: <path>
  (expected .. observed ..)`, non-zero on any; else "audit: N deployed files verified".

## Step 7: install.go frozen restructure (lk-013/016/017, sc-002/004)
**Files:** `cmd/apm/install.go`
**Verify:** `go build ./cmd/apm && go test ./cmd/apm`
- `apm.yml` optional in frozen mode (empty manifest if absent).
- Reorder: (A) disk integrity — deployed-file hashes (BEFORE any download) → local hashes →
  registry archive hash-verify (offline `<basename(repo_url)>.tar.gz`) + SafeExtract on success;
  (B) source materialization (git download + tree_sha256) only when sources expected.
- Add `--max-entries` (default 10000) + `--max-archive-bytes` (default 100MB) flags → `Limits`.
- Keep diagnostics naming path / expected+actual.
- Confirm any existing `install_test.go` case asserting a missing-`apm.yml` error still holds
  after making the manifest optional in frozen mode (the optionality is frozen-only).

## Step 8: cmd/apm integration tests (replay fixtures + end-to-end extract proof)
**Files:** `cmd/apm/audit_test.go`, extend `cmd/apm/install_test.go`
**Verify:** `go test ./cmd/apm`
- Replay `deployed-file-mismatch.workspace` + frozen lockfile through audit and frozen install →
  fail naming the path.
- Replay `hash-mismatch.frozen.yaml` + good.tar.gz → fail expected/actual, no extract leak.
- `good.frozen.yaml` validate → accept; `bare-hex-reader.frozen.yaml` → accept.
- **END-TO-END EXTRACT PROOF (advisor #2 — NOT optional, the genuine registry-path proof).**
  The malicious tarballs must flow through install's real `container→hash→SafeExtract→limits`
  path, else nothing verifies install actually calls `SafeExtract` with the configured limits
  (a dropped wiring or un-threaded `--max-entries` would pass every other test). For each of
  zip-slip / symlink-escape / four-entry: build a temp CWD with a frozen `apm.lock.yaml` entry
  `{source: registry, repo_url: x/<name>, resolved_hash: sha256(<name>.tar.gz)}` (compute the
  real digest in-test so the lk-013 hash gate passes and execution reaches extraction), drop
  `<name>.tar.gz` in CWD (basename rule `x/zip-slip` → `zip-slip.tar.gz`), run frozen install:
  - zip-slip → fail closed, output contains `..`, nothing extracted.
  - symlink-escape → fail closed, output contains `link`, nothing extracted.
  - four-entry (`--max-entries 3`) → fail closed for the entry-cap reason; default → would extract.

## Step 9: wire python runner (optional, command-invocation only)
**Files:** `conformance/conformance-kit/runner/run_conformance.py` (only if not under CHECKSUMS)
**Verify:** `APM_BIN=./bin/apm.exe python run_conformance.py`
- Adapt `assert_fail_closed` for the 3 raw-`.tar.gz` fixtures to drive the extraction path.
- NEVER edit `EXPECTATIONS.yaml` / `must_contain` / outcomes. If wiring is awkward, leave runner
  and rely on native tests + Codex (document the choice).

## Step 10: full verification
**Verify:** `go build ./... && go test ./... -race -cover && go vet ./... && go fmt ./...`
- All existing tests pass (no regressions).
- New archive/credsec coverage ≥ 80%.
- External: Codex `codex exec --sandbox danger-full-access` black-box on built binary
  (zip-slip/symlink/four-entry/hash-mismatch/deployed-file-mismatch + hostclass spec example);
  opus sub-agent independent review of each req. **Never self-verify.**

## Validation Commands
```bash
go build ./...
go test ./... -race -cover
go vet ./...
go fmt ./...
APM_BIN=./bin/apm.exe python conformance/conformance-kit/runner/run_conformance.py
```

## Rollback points
- Each step is an isolated package or additive command; revert a step's files without touching others.
- install.go frozen restructure is the only edit to existing behavior — keep the diff surgical and
  guard with `cmd/apm` regression tests (existing frozen tests must still pass).
