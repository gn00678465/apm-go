# 27 — embedded/serialized `apm.lock.yaml` omits `generated_at`/`deployments` when the source lockfile never had them

**What to build:** `internal/lockfile.SerializeLockfile` (via `write.go`) and, downstream, `internal/pack/bundle.EnrichLockfileForPack`'s embedded-bundle lockfile must always serialize `generated_at` and `deployments` the way the pinned Oracle does, instead of omitting the key entirely when the in-memory value is empty/absent.

**Blocked by:** none. **Status:** CLOSED (2026-08-26, orchestrator).

**Origin:** ticket 17 phase 2's `pack-archive` parity case (2026-08-26) — the first parity case to both (a) run a `pack` invocation to a real, non-refused success AND (b) carry a real on-disk `apm.lock.yaml` fixture through to a content-level comparison of the *embedded* copy `bundle.Produce` writes inside the built bundle. No earlier case exercises this combination: `pack-refuse-agent-plugin`/`pack-refuse-apm` both refuse (exit 2) before `embedPackLockfile` ever runs; `pack-no-flag`'s own fixture has no `apm.lock.yaml` at all, so its bundle build never calls `embedPackLockfile` either (the `if opts.Lockfile != nil` guard in `internal/pack/bundle/producer.go` is never true for that case).

## The finding

Given a minimal fixture `apm.lock.yaml` containing only `lockfile_version: "1"\n` (no `generated_at:` or `deployments:` keys at all), running `pack --archive` and extracting the embedded `apm.lock.yaml` from both sides' produced archive:

```
# Oracle's embedded apm.lock.yaml (tail):
lockfile_version: '1'
generated_at: ''
dependencies: []
deployments: []

# apm-go's embedded apm.lock.yaml (tail):
lockfile_version: "1"
dependencies: []
```

The Oracle's `enrich_lockfile_for_pack`/lockfile serialization ALWAYS writes both `generated_at` (defaulting to an empty string when the source lockfile had none) and `deployments` (defaulting to an empty list) as top-level keys — these are declared fields of its lockfile schema with defaults, always present in the serialized form regardless of whether the original file set them.

apm-go's `internal/lockfile.Lockfile` struct has a `GeneratedAt string` field, but `write.go:45-46` only emits the `generated_at:` line `if lf.GeneratedAt != ""` — an empty value is treated as "field absent," not "field present with an empty value," so a lockfile that never had `generated_at:` keeps not having it after round-tripping through `SerializeLockfile`/`EnrichLockfileForPack`. `deployments` is worse: apm-go's `Lockfile` struct has **no field for this concept at all** — it has never been ported, so there is no code path that could emit it even if the omission-on-empty behavior above were fixed.

## Why this matters / reproduction

This is **completely orthogonal to `--archive`/`--archive-format`** (ticket 17 phase 2's actual scope) — the gap is in the shared lockfile serialization layer, reachable from ANY successful `pack` invocation that embeds a real lockfile, archived or not:

```sh
# minimal repro, no --archive needed:
mkdir -p /tmp/repro/.apm/agents && cd /tmp/repro
cat > apm.yml <<'EOF'
name: demo
version: 1.0.0
dependencies:
  apm: []
EOF
echo 'lockfile_version: "1"' > apm.lock.yaml
echo x > .apm/agents/foo.md
apm-go pack   # or: uv run apm pack, for the Oracle side
cat build/demo-1.0.0/apm.lock.yaml   # apm-go: no generated_at/deployments keys
```

No existing parity case catches this today because none combine "succeeds" with "has a real lockfile" — `pack-no-flag` (succeeds, no lockfile) and `pack-refuse-*` (has a lockfile, but refuses before reaching it) each individually miss the combination.

## Acceptance criteria

- [x] `internal/lockfile.SerializeLockfile` (or `EnrichLockfileForPack`, whichever is the correct architectural layer — `SerializeLockfile` is shared by top-level `apm.lock.yaml` writes, not just pack's embedded copy, so check both the plain top-level lockfile write path and the embedded-bundle path against the Oracle's own behavior for each) always serializes `generated_at:` (empty string when absent), matching the Oracle's unconditional field.
- [x] A `deployments` concept is added to `internal/lockfile.Lockfile` (or, if genuinely out of scope for a full port, at minimum the embedded-bundle lockfile always writes `deployments: []` when the Oracle's own semantics guarantee it's always empty for anything apm-go currently tracks — needs verifying what `deployments` represents in the Oracle's schema first, e.g. `grep -n deployments` in `lockfile_enrichment.py`/the lockfile Pydantic model, before deciding whether this is "always empty for apm-go today" or "a real feature apm-go hasn't ported at all").
- [x] Verify against the pinned Oracle directly whether this omission-when-empty pattern affects OTHER fields too (this ticket's investigation only checked `generated_at`/`deployments` because those are the two that showed up in one specific fixture's diff — a fresh, deliberate audit of every field `SerializeLockfile`/`EnrichLockfileForPack` can omit-when-empty, compared field-by-field against the Oracle's own lockfile Pydantic model's serialization, is warranted before considering this closed).
- [x] A new (or extended) parity case exercises a successful `pack` (not `--archive`-specific) with a real, minimal `apm.lock.yaml` fixture and asserts the embedded copy's field set matches the Oracle's, once `packed_at`/`target` (already-documented, separately-tracked deviations — ticket 17 phase 1's `target` auto-detect gap, and the inherent live-timestamp normalization class) are accounted for.
- [x] Once fixed, ticket 17 phase 2's `pack-archive` case's `tree` field diff should be re-evaluated: it currently has NO waiver at all (deliberately, per the evaluator's ticket 17 phase 2 follow-up review) specifically because this gap was folded into the .zip's own undifferentiated byte difference. After this ticket lands, re-derive `pack-archive`'s tree diff fresh: what remains should be describable purely as "compressor + packed_at + the already-documented `target` deviation," matching the `stdout` waiver's own accepted-residual class, at which point a `tree`/`tree_paths`-scoped waiver naming exactly the remaining archive-byte differences becomes appropriate.

## Non-goals

- Fixing every possible lockfile-field omission speculatively. This ticket is scoped to `generated_at`/`deployments`, the two fields the `pack-archive` investigation actually found differing — the "audit every field" AC above is a verification step (confirm there isn't a THIRD field silently affected too), not license to rewrite the serializer wholesale.
- Re-opening ticket 17 phase 1's `target: minimal` vs `target: all` deviation (Oracle's `detect_target()` auto-fill) — that is a separate, already-documented, deliberate scope decision, unaffected by this ticket.


---

## Outcome

Fixed in `internal/pack/bundle`'s new `ensureOraclePackLockfileFields`, called from `EnrichLockfileForPack`.

### The layer question the ACs asked about — answered by a failing test

The first attempt put the fix in `internal/lockfile.SerializeLockfile`, which is what the Oracle's shape suggests (it has one `to_yaml` serving both the top-level lockfile and pack's embedded copy). `TestWriteLockfile_RoundTrip_ProvenanceNoDoubleEmit` and `TestWriteLockfile_RoundTrip_PackageTypeNoDoubleEmit` immediately failed, and they were right to: they assert `string(out) == yamlSrc`, i.e. a **byte-identical round-trip of an unchanged user lockfile**.

That is a guarantee apm-go deliberately makes and the Oracle does not — AGENTS.md's YAML round-trip convention. Making the two keys unconditional in the shared serializer would mean every `install` silently rewrites the user's `apm.lock.yaml` to add them. `EnrichLockfileForPack`'s own doc comment already anticipated exactly this: *"SerializeLockfile 不動，lockfile_pack.go 獨立包裝層"*. So the fix belongs in the pack-only wrapping layer, and the ACs' "check both paths" instruction resolves to: change the embedded copy, leave the top-level write alone.

### Audit of other omit-when-empty fields (AC3)

Reading `LockFile.to_yaml` (`deps/lockfile.py:815-843`) end to end: it seeds the dict with `lockfile_version` and `generated_at`, then assigns `dependencies` and `deployments` — **those four and only those four are outside any guard**. Every remaining field (`apm_version`, `mcp_*`, `lsp_*`, `local_deployed_*`) is `if <truthy>`, which is exactly apm-go's existing omit-when-empty behaviour. So there is no third affected field; the audit is complete, not merely sampled.

### `deployments` and the duplicate-key trap

`"deployments"` is deliberately absent from `SerializeLockfile`'s `knownTopKeys`, so an existing key is already carried through verbatim by the unknown-key preservation loop. Appending a default unconditionally would emit the key **twice** for an Oracle-written lockfile carrying real ledger rows — a duplicate mapping key, worse than the omission being fixed. The default is therefore written only when the source had none. `TestEnrichLockfileForPack_ExistingDeploymentsNotDuplicated` locks this.

### Verification

Both sides' embedded `apm.lock.yaml`, extracted from the produced archives, now carry the identical field set. What remains in the `.zip` bytes is four implementation differences with no missing feature and no differing word among them: the two zip compressors (951 vs 1024 bytes for identical input), the live `packed_at`, the already-documented `target: minimal` vs `all` deviation, and PyYAML-vs-go-yaml scalar quote style. `pack-archive`'s `tree` is waived on that basis, `tree_paths`-scoped to the archive itself.

Corpus: **2 → 1 of 73 unwaived**; only `pack-help` remains (ticket 17's `--json`). `go test ./...` 26/26.
