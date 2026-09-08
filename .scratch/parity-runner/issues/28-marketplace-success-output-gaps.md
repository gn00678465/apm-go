# 28 — the successful marketplace-pack output surface was never compared, and had three real gaps

**What to build:** bring `pack`'s successful marketplace path in line with the pinned Oracle — the missing artifact-catalog block, the wrong path form on the completion line, and the wrong key order in the produced `marketplace.json` — and add the parity cases that make this surface visible at all.

**Blocked by:** none. **Status:** CLOSED (2026-08-27, orchestrator).

**Origin:** ticket 17 phase 4's `pack-check-versions-fail`/`pack-check-clean-fail` waivers, which named a follow-up ticket 28 that was never filed (the implementor died mid-ticket). Re-derived and widened during the 2026-08-27 "確認未完成項目" audit, which found the ticket file absent while two committed waivers cited it.

## Why this went unnoticed for the whole backlog

**No parity case ran a marketplace pack to a real success.** The corpus had 74 cases and 15 `pack-*` cases, and not one of them reached `_render_marketplace_result`'s success tail:

| case | why it never gets there |
|---|---|
| `pack-check-versions-fail`, `pack-check-clean-fail` | `-m none` — deliberately chosen to isolate the gates, so zero marketplace outputs are selected |
| `pack-refuse-agent-plugin`, `pack-refuse-apm` | exit 2 before the producers run |
| `pack-no-flag`, `pack-format-*`, `pack-legacy-skill-paths`, `pack-archive` | fixtures have no `marketplace:` block — bundle producer only |
| `pack-json` | `--json` skips the deferred renders entirely (pack.py:551-556) |
| `pack-help` | help text only |

So `parity`'s green **0 unwaived** gate was measuring less than it appeared to: this was not a waived known gap, it was an untested surface. That is the finding that matters most here — the three product bugs below are its symptoms.

## The three findings

All three verified live, both sides, same invocation, against the pinned Oracle (`c8d6cdec596e773a84b0839c33c28b6b0a217637`).

### 1. The artifact catalog block was missing entirely

The Oracle's `_render_marketplace_catalog` (pack.py:763-777), called from `_render_marketplace_result`'s tail (pack.py:748-749) whenever at least one output was written and the run is not a dry-run, prints:

```
[i] Marketplace artifacts ready:
[i]   [claude] <absolute path>
[i] How consumers install from this marketplace varies by AI assistant.
[i] See: https://microsoft.github.io/apm/producer/publish-to-a-marketplace/#consume-from-any-assistant
```

None of those strings existed anywhere in apm-go (`grep` returned empty). This is a **missing** block, not an apm-go superset — the pack-check-* waivers' own description of ticket 28 as "an apm-go-only extra line" had the direction backwards, and is corrected here.

Row format: `  [{profile.ljust(label_width)}] {path}`, where `label_width` is the widest profile name — so with both outputs active the codex row is `  [codex ] ...`, padded. The Oracle's untagged fallback branch (pack.py:773-775) is unreachable in apm-go: its `outputs`-without-`output_reports` path has no apm-go equivalent, and `build.KnownOutputFormats` guarantees every active output is a named profile.

### 2. The completion line printed a relative path; the Oracle's is absolute

`resolve_effective_output_path` (marketplace/output_profiles.py:134-136) joins any relative configured path onto the absolute project root before returning, so `MarketplaceOutputReport.output_path` is absolute at **every** use site — the success line (pack.py:738), the catalog row (pack.py:771), and the JSON payload (builder.py:242) alike.

apm-go printed `r.outputPath` (relative) on the text line while `--json` already carried `r.absPath`. That split came from a ticket 17 assumption — recorded in `marketplaceRender.absPath`'s own comment as "the text completion line prints the RELATIVE outputPath (ticket 13), but the Oracle's JSON payload carries [...] absolute" — which was **never verified**, because no case compared the text line. Ticket 13's `displayPath` relativisation is genuinely correct for *bundles* (the Oracle's `bundle_path` really is the unresolved user-facing `./build` string) and was wrongly generalised to this path.

### 3. `marketplace.json`'s `plugins[]` entries emitted `category` in the wrong position

The Oracle builds the entry dict by insertion order (marketplace/output_mappers.py:197-208):

```
name, description, version, author, license, repository, tags, category, homepage, source
```

`ClaudePlugin` declared `Category` third, immediately after `Description` — and `encoding/json` writes struct fields in declaration order, so this is a byte difference in the produced artifact for any package declaring **both** a version and a category:

```
Oracle: { "name": "pkg-a", "version": "1.0.0", "category": "tools", "source": "./pkg-a" }
apm-go: { "name": "pkg-a", "category": "tools", "version": "1.0.0", "source": "./pkg-a" }
```

The existing golden fixture (`testdata/upstream-claude-marketplace.golden.json`) could not catch this: its single entry has no `version`/`author`/`license`/`repository`/`tags`, so it renders **identically under both orderings**. The field comment nonetheless cited that golden as pinning the position "immediately after description ... the strongest available evidence" — an over-reading of an example that never distinguished the two. The golden still passes unchanged after the fix.

## Acceptance criteria

- [x] AC1 — `pack` prints the Oracle's catalog block after its per-output success lines, gated on "at least one output actually written AND not a dry-run", with the Oracle's `ljust` tag padding.
- [x] AC2 — Every user-facing rendering of a marketplace output path (completion line, dry-run notice, catalog row, `--json`) uses the absolute path, and the reason ticket 13's `displayPath` does not apply here is recorded at the deviation site.
- [x] AC3 — `ClaudePlugin`'s field order reproduces the Oracle's insertion order, with a test that populates every field so the ordering is unambiguous (the golden fixture cannot serve as that test).
- [x] AC4 — New parity cases cover a successful marketplace pack, single-output AND multi-output, so this surface can never silently regress again.
- [x] AC5 — Fresh corpus: zero `(fields, waived)` regression on the pre-existing 74 cases; the new cases' only residue is the already-accepted Rich-wrap class, waived field-scoped with the byte evidence.

## Implementation

`cmd/apm-go/pack.go`:
- New `marketplaceDocsURL` const, verbatim from the Oracle's `MARKETPLACE_DOCS_URL` (pack.py:21-23).
- New `renderMarketplaceCatalog`, called from `runPack`'s deferred-render block right after the per-output loop (and inside the same `if !opts.jsonOutput` guard, since the Oracle returns before its own rendering block under `--json`).
- `renderMarketplaceOutput` switched from `r.outputPath` to `r.absPath` on both the success and dry-run lines; `marketplaceRender.absPath`'s comment rewritten to record why (it previously asserted the opposite).

`internal/marketplace/build/mapper.go`: `ClaudePlugin.Category` moved from third position to between `Tags` and `Homepage`; the comment now cites `output_mappers.py:197-208` for the position and explains why the golden fixture could not pin it.

`tools/parity/cases/pack-marketplace-success/` (`pack -m claude`) and `tools/parity/cases/pack-marketplace-multi-output/` (`pack -m all`, two profiles, `category:` declared so the codex gate passes). Neither fixture ships a pre-existing `marketplace.json`, so the write is a fresh create and the `tree` comparison covers the produced bytes.

## Tests

- `cmd/apm-go/pack_marketplace_catalog_test.go`: exact catalog bytes; `ljust` padding with two profiles; dry-run suppression; empty-renders suppression; absolute path on both the success and dry-run completion lines.
- `internal/marketplace/build/mapper_keyorder_test.go`: full-field key order against the Oracle's insertion order, plus a minimal `name/version/category/source` regression guard for the exact swap found live.

## Non-goals

- The codex `category`-required error wording. Probing finding 3 surfaced a **fourth**, unrelated difference: the Oracle says `Error: marketplace config error: packages must define 'category' when marketplace.outputs includes 'codex' (missing: pkg-a)` where apm-go says `[x] package "pkg-a" is missing category required for Codex output`. That is a real wording gap on a different (validation, not success-output) surface, outside this ticket's written ACs — recorded as **ticket 29** per the Scope rule rather than fixed inline.
- Auditing every other producer's success tail for a similarly uncompared surface. The bundle producer's tail has its own coverage (`pack-no-flag`, `pack-archive`); the plugin-manifest producer's does not, and is worth its own look, but not here.
