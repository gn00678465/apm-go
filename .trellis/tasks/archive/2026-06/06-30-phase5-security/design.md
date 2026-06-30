# Phase 5 Design: Security Hardening

## Authority

Behavior is graded against the immutable oracle `conformance-kit/oracle/`:
- `integrity/{good,zip-slip,symlink-escape,four-entry}.tar.gz` + `*.frozen.yaml`
- `EXPECTATIONS.yaml` (per-fixture outcome + required diagnostic substrings)

Spec text: `apm/docs/.../openapm-v0.1.md` §10.3–§10.9. Spec wins over Python apm.

## Architecture

Two new leaf packages + extensions to `lockfile`, `install.go`, and a new `audit` command.

```
internal/archive/                 # sc-002, sc-004 (+ feeds lk-013 extraction)
  extract.go        SafeExtract, container detection, caps, path/link guard, staging+cleanup
  extract_test.go   native tests vs oracle tarballs
internal/credsec/                 # sc-003, sc-005, sc-007, sc-008 (§10.3 cohesive)
  hostclass.go      HostClass(host), SameHostClass(a,b,aliases)            [sc-005]
  redirect.go       NewAuthDropRedirect(origURL, aliases) http CheckRedirect [sc-003]
  attach.go         ShouldAttachCredential(rawURL, insecure)               [sc-008]
  redact.go         Redactor, MatchesSecretPattern(path)                   [sc-007]
  *_test.go
internal/lockfile/                # extend
  audit.go          VerifyDeployedState(lock, root) []Violation            [sc-001/lk-017]
  archive.go        ParseHashEnvelope, VerifyArchiveBytes(expected, data)  [lk-013/lk-016]
cmd/apm/
  audit.go          `apm audit` command                                    [sc-001]
  install.go        restructure frozen branch; add --max-entries           [lk-013/017, sc-002/004]
  main.go           register audit command
```

`go.mod`: add `golang.org/x/net` (publicsuffix, embedded list).

## 1. internal/archive — SafeExtract (sc-002, sc-004)

```go
type Limits struct {
    MaxBytes   int64 // default 100 << 20 (100 MB), uncompressed
    MaxEntries int   // default 10000
}

type Extracted struct{ Files []string } // relative paths written

// SafeExtract reads a gzip(tar) stream, enforces limits, and extracts into a
// fresh staging dir which is renamed to dest on success. On ANY error the
// staging dir is removed (partial-extraction cleanup, sc-002).
func SafeExtract(r io.Reader, dest string, lim Limits) (*Extracted, error)
```

Algorithm:
1. **Container check (sc-004a):** peek first 2 bytes.
   - gzip magic `0x1f 0x8b` → ok.
   - zip magic `0x50 0x4b` (`PK`) → error `archive: rejected application/zip container (v0.1 requires application/gzip tar.gz)`.
   - else → error naming "unrecognized archive container".
   Use `bufio.Reader` + `Peek(2)` so the bytes stay in the stream for gzip.
2. `gzip.NewReader` → `tar.NewReader`.
3. Stage dir: `dest + ".apmtmp"` (same parent; created fresh, RemoveAll first).
4. Per entry (loop):
   - entry count++; if > MaxEntries → fail closed `archive: entry count exceeds limit N` (sc-004c). Check **before** writing the entry. (No must_contain → no substring conflict.)
   - **link guard (sc-002) — FIRST among content guards:** `hdr.Typeflag == tar.TypeSymlink || tar.TypeLink` → error containing `link` (must_contain `link`). (Covers symlink + hardlink.)
     **Must precede the path guard:** if a symlink entry's *name* itself contains `..`, a path-first order would emit `..` and fail the `link` substring assertion for `symlink-escape.tar.gz`. Link-first is strictly safe — zip-slip is a regular file (no link flag) so it still reaches the `..` path guard.
   - **path guard (sc-002):** `clean := path.Clean(hdr.Name)`.
     - reject if `hdr.Name` is absolute (`path.IsAbs` or Windows `filepath.IsAbs` or has volume) → "absolute path".
     - reject if `clean == ".." || strings.HasPrefix(clean, "../")` or contains a `..` segment → error containing `..` (must_contain `..`).
     - final safety: compute `target = filepath.Join(stage, clean)`; reject if not within stage (`!strings.HasPrefix(target+sep, stage+sep)`).
   - **type handling:** `TypeDir` → mkdir; `TypeReg` → create+copy with a hard byte cap: copy through an `io.LimitReader`/running counter; if cumulative uncompressed bytes > MaxBytes → fail closed `archive: uncompressed size exceeds limit` (sc-004b, anti gzip-bomb). Other unusual types → reject.
   - record relative path.
5. On any error: `os.RemoveAll(stage)`; return error (nothing left behind).
6. On success: `os.RemoveAll(dest)` then `os.Rename(stage, dest)`.

Notes:
- `..` and `link` are the exact required substrings; keep them literally in the messages.
- Default `Limits{}` (zero value) must resolve to the spec defaults — `normalize()` fills 0 → 100 MB / 10000.

## 2. internal/credsec

### hostclass.go (sc-005)
```go
// HostClass returns the registrable domain (eTLD+1) per the Public Suffix List.
func HostClass(host string) (string, error)   // strip port; publicsuffix.EffectiveTLDPlusOne

// SameHostClass reports whether a and b are one credential scope: identical
// eTLD+1, OR b is listed under a's aliases (or vice versa). No CNAME/SAN/redirect.
func SameHostClass(a, b string, aliases map[string][]string) bool
```
- `github.contoso.com` & `contoso.com` → both eTLD+1 `contoso.com` → same.
- `github.contoso.com` & `github.com` → `contoso.com` ≠ `github.com` → different.
- aliases: a flat set; if a∈aliases-group and b∈same group → same class.
- **Degenerate hosts** (IPs, `localhost`, single-label): `publicsuffix.EffectiveTLDPlusOne`
  errors. Fail safe — treat such a host as its own singleton class (HostClass returns the host
  verbatim). `SameHostClass` then yields true only for byte-identical degenerate hosts
  (`localhost`~`localhost`; `localhost`≠`127.0.0.1`) → never shares credentials across them.

### redirect.go (sc-003)
```go
// NewAuthDropRedirect returns an http.Client.CheckRedirect that strips
// Authorization (and other originating creds) when the redirect target's host
// class differs from the original request's host class.
func NewAuthDropRedirect(aliases map[string][]string) func(req *http.Request, via []*http.Request) error
```
- Compare `via[0].URL.Host` class vs `req.URL.Host` class via SameHostClass.
- Different → `req.Header.Del("Authorization")` (and any other cred headers we set).
- Tested with `httptest` issuing a 3xx to a different-class host; assert Authorization absent on the second hop and present on a same-class hop.
- **Consumer deferral:** no live authenticated downloader exists yet; this is the reusable policy. Documented.

### attach.go (sc-008)
```go
// ShouldAttachCredential reports whether a credential may be attached to a
// git-over-HTTP fetch at rawURL. https → yes; http → only if loopback or insecure.
func ShouldAttachCredential(rawURL string, insecure bool) (bool, error)
```
- scheme https → true. scheme http → loopback host (`127.0.0.0/8`, `::1`) or `insecure` → true; else false.

### redact.go (sc-007)
```go
type Redactor struct{ secrets []string }              // literal cred values to scrub
func NewRedactor(values ...string) *Redactor
func (r *Redactor) Redact(s string) string            // replace each value with [REDACTED]

// MatchesSecretPattern reports whether a file path matches the default
// producer secret set: .env, .env.*, *.pem, *.key, id_rsa, id_ed25519.
func MatchesSecretPattern(path string) bool
```
- Redactor scrubs known credential literals from any string before it is logged / written.
- `MatchesSecretPattern` is the matcher the future `apm pack` (Phase 7) uses to refuse files.
  Built + unit-tested now; pack wiring deferred (documented).

## 3. lockfile audit + archive verify

### audit.go (sc-001 / lk-017)
```go
type Violation struct{ Path, Expected, Observed string }

// VerifyDeployedState re-hashes every deployed file recorded in the lockfile
// (per-dep deployed_file_hashes + local_deployed_file_hashes) against disk under
// root and returns content-integrity violations (path/expected/observed).
func VerifyDeployedState(lock *Lockfile, root string) []Violation
```
Reuses existing `VerifyDeployedHashes` logic but returns structured violations (so both
`apm audit` and frozen install render the same `path` substring the oracle requires).

### archive.go (lk-013 / lk-016)
```go
// VerifyArchiveBytes compares sha256(data) to the recorded hash envelope.
// Accepts "sha256:<hex>", "sha384:..", "sha512:..", and bare 64-hex == sha256 (lk-016).
func VerifyArchiveBytes(recorded string, data []byte) error // err names expected+actual
```

## 4. cmd/apm/audit.go (sc-001)

`apm audit`:
1. Read `apm.lock.yaml` (required). Do **not** require `apm.yml`.
2. `viol := lockfile.VerifyDeployedState(lock, ".")`.
3. If any → print each `content-integrity violation: <path> (expected <e>, observed <o>)` to
   stderr, exit non-zero. Else print "audit: N deployed files verified", exit 0.

## 5. install.go frozen restructure (lk-013/016/017, sc-002/004)

Current bugs (research): apm.yml read unconditionally; registry deps skipped; tree_sha256
verified before deployed-hash, against a nonexistent checkout.

New frozen branch ordering — **disk-integrity first, source-materialization second**:
```
frozen:
  lock := load apm.lock.yaml         # required
  m    := load apm.yml if present    # OPTIONAL — empty manifest if absent
  CheckFrozenInstall(m, lock)        # empty m → no direct deps → vacuously ok

  # (A) disk-only integrity — runs from lockfile+disk, no network, no apm.yml needed
  for dep with deployed_file_hashes:        # lk-017 / sc-001  (BEFORE any download)
      viol := VerifyDeployedState(...); if viol → fail closed naming path
  verify local_deployed_file_hashes
  for dep where source==registry && resolved_hash != "":   # lk-013
      data := read offline archive  <basename(repo_url)>.tar.gz in CWD
      VerifyArchiveBytes(resolved_hash, data)   # mismatch → fail closed expected/actual, NO extract
      on success: archive.SafeExtract(data, deployRoot, Limits{MaxEntries: flag})  # sc-002/004

  # (B) source materialization — only when sources are expected (apm_modules present / manifest deps)
  for git dep with no checkout: download; verify tree_sha256 (lk-015)
  ...
```
- `--max-entries` flag (default 10000) threads into `Limits`. Also a `--max-archive-bytes`
  (default 100 MB) for symmetry; only `--max-entries` is exercised by the oracle.
- Offline archive resolution: `filepath.Join(cwd, path.Base(repo_url)+".tar.gz")`.
  `registry.example.com/demo/good` → `good.tar.gz` (matches good/hash-mismatch fixtures).
- Ordering guarantees deployed-file-mismatch reports the file path (check A) **before** any
  git download/tree check could fail with a different message.

## Diagnostic substrings (oracle-pinned — keep literal)
| fixture | must contain |
|---|---|
| zip-slip | `..` |
| symlink-escape | `link` |
| hash-mismatch | `expected`, `actual` |
| deployed-file-mismatch | `.github/instructions/demo.instructions.md` |

## Verification (anti-cheat, Phase V)
- **Native go tests** load the real oracle tarballs/frozen fixtures (read-only) and assert
  outcomes + substrings — primary, anti-cheat clean.
- Replay frozen fixtures through `runInstall(frozen)` / `apm audit` in `cmd/apm` tests.
- python `run_conformance.py`: confirm it is NOT under `oracle/CHECKSUMS.sha256`; if wiring its
  CONTRACT command lines, change invocation only (never must_contain/outcomes). Adapt
  `assert_fail_closed` for the 3 raw-tarball fixtures (they are `.tar.gz`, not lockfiles) to call
  the extraction path; do NOT make `install` treat a gzip `apm.lock.yaml` as an archive.
- External: Codex `codex exec` black-box on the built binary + opus sub-agent review. Never self-verify.

## Explicitly Out of Scope
- Registry HTTP download/mirror fetch (v0.2). `apm pack` (Phase 7). Live wiring of redirect/attach
  policies. Policy-driven secret-pattern extension (Phase 6). TLS-only wire / attestations (v0.2).
