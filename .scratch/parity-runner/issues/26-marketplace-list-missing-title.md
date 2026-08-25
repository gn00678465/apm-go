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

- [ ] AC1 — `marketplace list` prints a title line reading "Registered Marketplaces" before (or as part of) its table, mirroring how `renderBrowseTable`/`renderSearchTable` already print their own title lines before `ux.Table(...)`. Exact placement/formatting is an apm-go rendering choice (left-aligned above the box, matching the existing browse/search convention) — the box-drawing/centering difference itself remains the already-accepted rendering-library class, not something to chase further.
- [ ] AC2 — `registry-explicit-config-dir`'s `stdout` field shows no diff once ignoring only the already-accepted box-style/alignment difference (verify against the pinned Oracle directly, not just visually).
- [ ] AC3 — Go tests locking the title line's presence and exact text on both the empty-registry path (unaffected — `list-empty`'s own message is separate and unaffected by this fix) and the non-empty path.
- [ ] AC4 — Zero regression elsewhere: every other case's `(fields, waived)` tuple in a fresh `tools/parity` corpus is unchanged, and `registry-explicit-config-dir` either goes fully waivable under the existing table-class precedent or is re-scoped precisely if some other gap remains.

## Non-goals

- Chasing Rich's box-drawing/title-centering/cell-wrap-vs-widening further. That is ticket 10/14's already-settled compare-semantics-and-waive territory (see the `doctor-healthy` waiver). This ticket only adds the missing *word content*.
- Auditing every other `ux.Table` call site for a similar gap. `browse` and `search` were checked directly and already have their own title lines; `doctor`/`check`/`outdated`/`update`(`resolved`)'s tables were not part of this investigation and are out of scope unless a future ticket finds the same shape there.
