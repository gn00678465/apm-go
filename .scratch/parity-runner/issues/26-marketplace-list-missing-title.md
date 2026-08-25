# 26 — `marketplace list`'s table is missing its "Registered Marketplaces" title

**What to build:** `marketplace list`'s table needs a title line naming "Registered Marketplaces", the same way `marketplace browse`'s table already carries "Plugins in '<name>'".

**Blocked by:** none. **Status:** open (found during ticket 25's own investigation — a real content gap, not something ticket 25 is allowed to waive).

**Origin:** ticket 25's fresh-corpus re-derivation of `registry-explicit-config-dir`'s residual `stdout` diff, 2026-08-26.

## The finding

Ticket 24 fixed `registry-explicit-config-dir`'s `tree` diff (the `APM_CONFIG_DIR` bit already has its own, correctly-scoped `tree_paths` waiver from ticket 15). With `tree` gone, the case's `stdout` diff stands alone — and it is **not** pure rendering:

```
oracle: 
                           Registered Marketplaces                           
        ┏━━━━━━━━┳━━...
        ┃ Name   ┃ Source ...

target: ┏━━ (no title line at all) ━━┓
        ╭────────┬────...
        │ NAME   │ SOURCE ...
```

The Oracle's `list_cmd` builds its table with `Table(title="Registered Marketplaces", ...)` (`commands/marketplace/__init__.py:877`). apm-go's `marketplaceListCmd` (`cmd/apm-go/marketplace.go`) calls `ux.Table(w, headers, rows)` directly, with no preceding title line at all.

This is **not** the already-accepted box-style/title-*alignment* rendering-library difference (ticket 10/14's `doctor-healthy`/`search-basic-hit` precedent, where both sides print the *same words* as a title, just centered-in-box vs left-aligned-above-box): here apm-go never prints the word "Registered Marketplaces" — or any equivalent — anywhere. Compare `marketplace browse`, which already does this correctly: `renderBrowseTable(w, fmt.Sprintf("Plugins in '%s'", name), rows)` prints an equivalent title line before its table, matching the Oracle's `title=f"Plugins in '{name}'"` (`__init__.py:936`) in content (only alignment/box-style differs — the accepted class). `list` is the one command missing the equivalent line entirely.

## Acceptance criteria

- [x] AC1 — `marketplace list` prints a title line reading "Registered Marketplaces" before (or as part of) its table, mirroring how `renderBrowseTable`/`renderSearchTable` already print their own title lines before `ux.Table(...)`. Exact placement/formatting is an apm-go rendering choice (left-aligned above the box, matching the existing browse/search convention) — the box-drawing/centering difference itself remains the already-accepted rendering-library class, not something to chase further.
- [x] AC2 — `registry-explicit-config-dir`'s `stdout` field shows no diff once ignoring only the already-accepted box-style/alignment difference (verify against the pinned Oracle directly, not just visually).
- [x] AC3 — Go tests locking the title line's presence and exact text on both the empty-registry path (unaffected — `list-empty`'s own message is separate and unaffected by this fix) and the non-empty path.
- [x] AC4 — Zero regression elsewhere: every other case's `(fields, waived)` tuple in a fresh `tools/parity` corpus is unchanged, and `registry-explicit-config-dir` either goes fully waivable under the existing table-class precedent or is re-scoped precisely if some other gap remains.

## AC2 second finding: header casing

Verifying AC2 directly against the Oracle (not just visually) surfaced a **second**, distinct content gap the title fix alone didn't close: `marketplace list`'s headers were hardcoded ALL-CAPS (`"NAME"`, `"SOURCE"`, `"REF"`, `"HOST"`, `"PATH"`), diverging from the Oracle's Title Case column names (`table.add_column("Name"/"Source"/"Ref"/"Path", ...)`, `commands/marketplace/__init__.py:878-881`) — confirmed this is genuinely `list`-specific, not an apm-go-wide styling choice, by checking `doctor`'s and `search`'s own tables, which already use Title Case headers matching the Oracle exactly (`Check`/`Status`/`Detail`; `Plugin`/`Description`/`Install`). Fixed in the same commit as AC1, since it's the same command, the same class of "bring `list` in line with the other two tables' already-correct convention" fix, and necessary for AC2 to actually hold. The verbose-only `"Host"` column has no Oracle counterpart at all (the Oracle's `list_cmd`'s `verbose` flag only affects exception-traceback printing, never adds a column) — left as apm-go's own superset feature, untouched beyond aligning its casing for consistency.

## Implementation

`cmd/apm-go/marketplace.go` (`marketplaceListCmd`):
- Added `fmt.Fprintln(w, "Registered Marketplaces")` immediately before `ux.Table(w, headers, rows)`, on the non-empty-registry path only (the empty-registry `ux.Info` message, ticket 24, is untouched).
- Changed `headers` from `{"NAME","SOURCE","REF","PATH"}` / `{"NAME","SOURCE","REF","HOST","PATH"}` to `{"Name","Source","Ref","Path"}` / `{"Name","Source","Ref","Host","Path"}`.

`tools/parity/waivers.json`: `registry-explicit-config-dir`'s existing (ticket 15) waiver extended from `fields: ["tree"]` to `fields: ["stdout", "tree"]`. Its `tree_paths` (already correctly scoped) was **not** touched. The `reason` now documents both halves separately: (1) the pre-existing `APM_CONFIG_DIR` tree difference, verbatim from before; (2) the table-class `stdout` difference, following the exact style of ticket 25's `search-limit-1`/`search-tag-hit` waivers — citing ticket 10's `doctor-healthy` precedent and explicitly naming the two real bugs (missing title, ALL-CAPS headers) this ticket fixed before the field became eligible for a waiver at all.

## Tests

- `cmd/apm-go/marketplace_e2e_test.go`: `TestMarketplaceList_TableIncludesEveryRegisteredSource` updated (Title Case header assertions, title-line assertion added). New `TestMarketplaceList_PrintsRegisteredMarketplacesTitle` (exact title line, immediately followed by the table's top border). New `TestMarketplaceList_HeaderCasingMatchesOracle` (asserts every Title Case header present via `--verbose`, asserts no ALL-CAPS header remains). New `TestMarketplaceList_EmptyRegistry_NoTitlePrinted` (the empty-registry path must not gain a spurious title).

## Evidence

- `go build ./...` / `go vet ./...`: clean. `go test ./...`: all packages `ok`. `go test ./cmd/apm-go/... ./tools/parity/... -race -count=1`: both `ok`, no data races.
- Live check: `marketplace add ./fixture --name skills` then `marketplace list` prints `Registered Marketplaces` immediately above the table, with Title Case headers.
- AC2, verified directly against the pinned Oracle (not visually): after both fixes, `registry-explicit-config-dir`'s `stdout` diff is reduced to exactly the `doctor-healthy`/`search-basic-hit` table-class shape — title text, every header, every row's cell content, and the trailing "Use 'apm marketplace browse <name>' to see plugins" line are byte-identical on both sides; only box-drawing characters and title-centering-vs-left-alignment differ.
- Parity corpus (AC4): built pre-fix (`8f0a3ed`, stashed) and post-fix binaries, ran all 69 cases against the pinned Oracle. Base: 2 of 69 unwaived. Fix: **1 of 69 unwaived** (`pack-help` only — ticket 17's flag gap, confirmed untouched). A tuple-level `(fields, waived)` diff shows **exactly 1 change**: `registry-explicit-config-dir`, `(stdout, tree)` unwaived → `(stdout, tree)` waived. All other 68 cases, including `pack-help` itself, byte-for-byte unchanged.

## Non-goals

- Chasing Rich's box-drawing/title-centering/cell-wrap-vs-widening further. That is ticket 10/14's already-settled compare-semantics-and-waive territory (see the `doctor-healthy` waiver). This ticket only adds the missing *word content*.
- Auditing every other `ux.Table` call site for a similar gap. `browse` and `search` were checked directly and already have their own title lines; `doctor`/`check`/`outdated`/`update`(`resolved`)'s tables were not part of this investigation and are out of scope unless a future ticket finds the same shape there.
