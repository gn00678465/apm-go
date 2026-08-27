# 24 — `marketplaces.json`: local sources stored as a bare path, and the file is never created when empty

**What to build:** the registry file `~/.apm/marketplaces.json` must match the Oracle's bytes: a local source is stored as a `file://` URI, and the file exists (as `{"marketplaces": []}`) after any command that reads the registry, even when nothing is registered.

**Blocked by:** none. **Status:** CLOSED (2026-08-28). **Clears:** the `tree` field of 11 of the 18 backlog cases — every `search-*` hit case, both `validate-checkrefs-*`, `list-empty`, and `browse-unknown-marketplace`.

**Origin:** orchestrator triage of the standing 18 unwaived parity cases, 2026-08-25. Sub-gap (a) is the item recorded against ticket 05 as "the `home/.apm/marketplaces.json` tree gap (Oracle stores a `file://` URI, apm-go a bare path)"; sub-gap (b) surfaced in the same triage.

## The two sub-gaps

**(a) `file://` URI vs bare path** — 9 cases show this as a `changed` tree entry:

```
oracle: { "marketplaces": [ { "name": "skills", "url": "file:///tmp/.../cwd/fixture" } ] }
target: { "marketplaces": [ { "name": "skills", "url": "/tmp/.../cwd/fixture" } ] }
                                                        ^^^^^^^ missing scheme
```

**(b) the file is never created when the registry is empty** — `list-empty` and `browse-unknown-marketplace` show it as `only-in-oracle`:

```
oracle wrote: home/.apm/marketplaces.json  ->  { "marketplaces": [] }
target wrote: (nothing)
```

Both commands only *read* the registry, so the Oracle is creating-on-read (or creating-on-load-miss). Determine which, from the Oracle source, before implementing — "write an empty file whenever anything reads the registry" and "materialise the default on a load miss" are different behaviours that happen to agree on these two cases, and picking the wrong one will diverge on some case not in the corpus.

## Compatibility — read this before changing the write path

`marketplaces.json` is a **user file that already exists on disk** for anyone running an earlier apm-go, containing bare paths. Changing the write format must not orphan those entries:

- The reader must keep accepting a bare path **and** a `file://` URI, indefinitely. This is not a migration with an end date.
- Do not rewrite a user's existing file as a side effect of an unrelated read. If a rewrite happens it must be because the user ran a command that legitimately mutates the registry.
- Round-trip: after `marketplace add ./x` then any read, the entry must resolve to the same directory it did before this change.

## Acceptance criteria

- [x] AC1 — Cite the Oracle file:line for (i) how the `url` field is produced for a local source and (ii) exactly when the file is created. Quote the code, do not infer the rule from the two corpus cases alone.
- [x] AC2 — A local source registered via `marketplace add ./path` is stored as a `file://` URI byte-identical to the Oracle's for the same input, including how an absolute path with no host is spelled (`file:///abs/path`, three slashes) and how any character needing escaping is encoded.
- [x] AC3 — The reader accepts bare paths and `file://` URIs alike; an existing bare-path registry keeps resolving. Regression test with a hand-written bare-path `marketplaces.json` fixture.
- [x] AC4 — The file-creation behaviour matches whatever AC1(ii) established, at the same trigger point.
- [x] AC5 — Non-local sources (`OWNER/REPO`, HTTPS, SSH) are untouched — no scheme added, no re-encoding. Regression test each shape.
- [x] AC6 — In a fresh runner corpus, the 11 named cases report **no `tree` diff**. Several will still differ on `stdout`/`error_body`; those are tickets 22/23/25 and may remain.
- [x] AC7 — Zero regression elsewhere: every other case's `(fields, waived)` tuple unchanged from the pre-change baseline. Note `registry-explicit-config-dir` has its own, different tree diff (apm-go honours `APM_CONFIG_DIR`, the Oracle ignores it) — it is ticket 25's waiver, not this ticket's, and must not be "fixed" by removing `APM_CONFIG_DIR` support.
- [x] AC8 — Go tests for AC2/AC3/AC4/AC5.

## AC1 — quoted from the Oracle source (not inferred from the 2 cases)

**(i) How `url` is produced** — `commands/marketplace/__init__.py:288` (inside `_parse_marketplace_source`, run at `marketplace add` time, not at read time):

```python
url = raw if raw.lower().startswith("file://") else f"file://{_expand_local_path(raw)}"
```

`_expand_local_path` (`__init__.py:433-443`):

```python
def _expand_local_path(raw: str) -> str:
    import os.path as _osp
    return _osp.abspath(_osp.expanduser(raw))
```

Plain string concatenation (`f"file://{...}"`) — **no percent-encoding of any character**, and no separator conversion; `models.py:141-165`'s own comment documents the Windows form as deliberately "malformed" (`file://C:\path`) for exactly this reason. A raw argument already starting with `file://` is kept **100% verbatim** (`url = raw`), no re-expansion at all — this ticket does not touch that already-`file://`-prefixed-input case (out of the named diffs' evidence; apm-go's pre-existing `resolveLocalPath` strip-then-reabs behavior for that input shape is left as-is).

**(ii) When the file is created** — `marketplace/registry.py:_load()` (`:61-81`) unconditionally calls `_ensure_file()` (`:30-39`) at the **start** of every load, before any name lookup runs:

```python
def _ensure_file() -> str:
    ensure_config_exists()
    path = _marketplaces_path()
    if not os.path.exists(path):
        with open(path, "w", encoding="utf-8") as f:
            json.dump({"marketplaces": []}, f, indent=2)
    return path

def _load() -> list[MarketplaceSource]:
    ...
    path = _ensure_file()   # <- called unconditionally, before any lookup
    ...
```

This settles the ticket's own open question in favor of **"any read materialises the file"**, not "materialise the default only on a lookup miss": `get_registered_marketplaces()` (`marketplace list`'s call, no per-name lookup at all) funnels through the same `_load()` and writes the file exactly the same way a name-lookup miss would. `list-empty` is the corpus proof — it never performs a name lookup, yet the Oracle still writes `{"marketplaces": []}`.

## AC2/AC4 discovery beyond the 2 named cases

Since `.URL` for a `KindLocal` source stopped being a plain filesystem path, every consumer that used it as one needed the same `LocalFilesystemPath` (file://-strip) treatment — found by grepping every `.URL` use site, not just the two named diffs:
- `internal/marketplace/client_local.go` (`fetchLocal` — the actual fetch/read path)
- `internal/marketplace/resolver.go` (`resolveLocalRelativeSource` — plugin-source resolution against a local marketplace)
- `cmd/apm-go/marketplace_authoring_audit.go` (`marketplace audit`'s local-root detection)

Additionally: the Oracle **never** shows a local source's raw `url` to the user — `list`, `add --verbose`, `remove`'s confirmation prompt, and `update --verbose` all print `.display_source` (`commands/marketplace/__init__.py:701,888,1033,...`), whose `local_path` half (`models.py:267-296`) strips the `file://` scheme back off before display. Without an equivalent fix, `marketplace list` etc. would have started leaking the new `file://` storage detail into user-facing output — a real regression this ticket's own change would have caused, not present before it (no parity case exercises this, but it is the same "a fresh corpus case discovering the wrong choice" risk AC1 warned about). Added `displaySource()` (`cmd/apm-go/marketplace.go`) and wired it into the 7 call sites that previously printed `.URL` raw; every other Kind's display is untouched (apm-go's own pre-existing `list` gap for github/gitlab — showing the full URL instead of the Oracle's `owner/repo` — is real but unrelated to this ticket and left alone, per the Scope rule).

## Implementation

- `internal/marketplace/source.go`: `parseLocalSource` now stores `URL: "file://" + resolved` (the `os.Stat` existence/file-vs-directory check still runs against the bare `resolved` path).
- `internal/marketplace/models.go`: new exported `LocalFilesystemPath(rawURL string) string` — `strings.TrimPrefix(rawURL, "file://")`, a no-op on a bare path.
- `internal/marketplace/registry.go`: `LoadRegistry()` now calls `SaveRegistry(nil)` (which nil-guards to `{"marketplaces": []}`, byte-identical to Python's `json.dump(..., indent=2)`, verified both ways) when the file doesn't exist, before returning the empty slice. A registry file that **does** exist is never touched by a read (verified by a dedicated test).
- `internal/marketplace/client_local.go`, `resolver.go`, `cmd/apm-go/marketplace_authoring_audit.go`: each now resolves through `LocalFilesystemPath` before treating `.URL` as a filesystem path.
- `cmd/apm-go/marketplace.go`: new `displaySource()` helper; wired into `add --verbose`, `list`'s table (both verbose and non-verbose row shapes), `update`'s single-name and all-marketplaces `--verbose` bullets, and `remove`'s confirmation prompt + `--verbose` bullet.

## Tests (AC8)

- `internal/marketplace/source_test.go`: `TestParseMarketplaceSource_LocalPaths` (updated to assert the `file://` prefix + absolute-path-when-stripped, all 10 local-path shapes), `TestParseMarketplaceSource_LocalPathPointingToFile` (updated), new `TestParseMarketplaceSource_NonLocalShapes_NoFileURIAdded` (AC5: OWNER/REPO, HTTPS, SCP-SSH — asserts unchanged URL and no `file://` leak).
- `internal/marketplace/models_test.go`: new `TestLocalFilesystemPath` (bare path unchanged, `file://` stripped, empty string, no false-positive mid-string match).
- `internal/marketplace/client_local_test.go`: new `TestFetchLocal_AcceptsBarePathAndFileURI_SameDirectory` (AC3, direct fetch-level compatibility).
- `internal/marketplace/resolver_test.go`: new `TestResolveLocalRelativeSource_FileURI` (AC3 for the plugin-source-resolution consumer).
- `internal/marketplace/registry_test.go`: new `TestLoadRegistry_MissingFile_MaterialisesEmptyRegistryOnDisk` (AC4, byte-exact content assertion) and `TestLoadRegistry_ExistingFile_NeverRewritten` (Compatibility section's "no side-effect rewrite" rule).
- `cmd/apm-go/marketplace_e2e_test.go`: `TestMarketplaceAdd_LocalPath_FallsBackToManifestNameAlias`/`TestMarketplaceAdd_SameNameSilentlyReplaces` (updated to expect the `file://` URI), new `TestMarketplaceList_LocalSource_ShowsPlainPathNotFileURI` (the `displaySource` regression guard).
- `tools/parity/main_test.go`: fixed a pre-existing, unrelated `go test` failure inherited from ticket 23's waiver commit (`9ca56ee`) — `TestRealWaiversJSON_ValidatesAgainstPin`'s hardcoded `wantIDs` list was never updated for the 3 new `search-*` waiver ids. One-line addition, not part of ticket 24's own feature work, but necessary to keep `go test ./...` green.

## Evidence

- `go build ./...` / `go vet ./...`: clean.
- `go test ./...`: all packages `ok`, zero failures.
- `go test ./internal/marketplace/... ./cmd/apm-go/... ./tools/parity/... -race -count=1`: all `ok`, no data races.
- Live check: `marketplace add ./fixture --name skills` under an isolated `APM_CONFIG_DIR` writes `"url": "file:///tmp/.../fixture"`; `marketplace list` immediately after shows the plain path `/tmp/.../fixture` (not the URI). A fresh `APM_CONFIG_DIR` with `marketplace list` (nothing registered) writes `home/.apm/marketplaces.json` as `{"marketplaces": []}`.
- Parity corpus (AC6/AC7): built pre-fix (`9ca56ee`, stashed) and post-fix binaries, ran all 69 cases against the pinned Oracle (`c8d6cdec...`). Base: 15 of 69 unwaived. Fix: **8 of 69 unwaived**. A tuple-level `(fields, waived)` diff shows exactly 12 changed cases, all in the expected direction:
  - `validate-checkrefs-off`, `validate-checkrefs-on`: `(tree,)` → `()` — **fully byte-identical to the Oracle now**.
  - `browse-unknown-marketplace`: `(error_body,stdout,tree)`→`(error_body,stdout)`, now **waived** (a pre-existing ticket-14-era waiver on exactly those 2 fields could not apply while `tree` also differed; AC6's "several will still differ... those may remain" — here the remainder happens to already be covered).
  - `list-empty`: `(stdout,tree)`→`(stdout,)`, now **waived** (same mechanism, pre-existing `stdout`-only waiver).
  - `search-basic-hit`: `(stdout,tree)`→`(stdout,)`, now **waived**.
  - `search-unknown-marketplace`: `(error_body,stdout,tree)`→`(error_body,stdout)`, now **waived**.
  - `validate-structure-fail`: `(stderr,tree)`→`(stderr,)`, now **waived** (a pre-existing waiver already covered `stdout`/`stderr`/`error_body` from before ticket 22 fixed `stdout`; the `stderr` Python-logging gap this ticket explicitly leaves alone was already accounted for).
  - `search-description-truncation`, `search-last-at-split`, `search-limit-1`, `search-tag-hit`, `search-zero-results`: `(stdout,tree)`→`(stdout,)`, **still unwaived** — no waiver exists yet for these 5; their `tree` diff is gone (this ticket's fix), their `stdout` Rich-rendering residue remains exactly as AC6 anticipated, left for ticket 25.
  - All other 57 cases, **including `registry-explicit-config-dir`** (explicitly checked: `(stdout,tree)`, unwaived, byte-identical before and after): **zero change**. `APM_CONFIG_DIR` support was not touched.
