# Phase 2: Dependency Resolution Engine

## Goal

Build the dependency resolution engine that powers `apm-go install` and `apm-go update`. This is the deterministic core of the package manager: given a manifest (apm.yml) and optionally an existing lockfile, produce a resolved dependency graph with pinned versions.

**Spec requirement**: Two independent implementations presented with the same input MUST produce equivalent results (§7 preamble).

## Scope

14 conformance requirements from the acceptance checklist Phase 2 (req-rs-*), plus dependent lockfile types (req-lk-008/009/010) needed for lock replay.

**In scope**: resolution engine, semver evaluation, reference classification, BFS traversal, diamond conflict detection, lock replay logic, "why" diagnostic, update semantics.

**Out of scope**: lockfile serialization/write (Phase 3), actual package download/clone (Phase 3-4), target deploy (Phase 4), security hardening (Phase 5), policy gate (Phase 6). These phases consume the resolution engine's output.

## Requirements

### R1: Reference Kind Classification (req-rs-008)

Classify every dependency entry into exactly one of 5 kinds, evaluated in priority order, as a **deterministic function of the entry alone** (no remote calls, no defaults):

| Priority | Kind | Condition |
|----------|------|-----------|
| 1 | local | local-path-form string, or object with `path:` and no `git:`/`id:` |
| 2 | registry | object with `id:` and (implicit/explicit) `registry:` |
| 3 | git-semver | git entry whose `ref:` parses as a semver range |
| 4 | git-literal | git entry with literal ref (SHA/tag/branch) or no ref |
| 5 | marketplace | `marketplace:` sourced entry |

### R2: Ref Sub-Classification (req-rs-003)

Classify the `ref:` field of any git dependency into:
- **semver**: parses as a semver range per §7.3.1
- **literal**: commit SHA, tag name (v?\d+.\d+.\d+ or non-semver), branch name
- **none**: no `ref:` field

### R3: Node-Semver Dialect (req-rs-007)

Evaluate all semver range expressions under the **node-semver** dialect. No implementation-defined hedging. Operators: `^`, `~`, `>=/>/<=/< /=`, `*`, space (AND), `||` (OR), hyphen range. Critical: `^0.x` narrowing per semver 2.0.0 §4.

### R4: Build-Metadata Tie-Breaking (req-rs-014)

Two tags differing only in build-metadata (`+...`) compare equal under semver precedence. Break ties by selecting the tag whose **full tag string** compares highest under **bytewise ASCII ordering**.

### R5: Git-Semver Resolution (req-rs-002)

7-step flow:
1. List remote git tags
2. Peel annotated tags to commit
3. Discard non-semver tags silently (no diagnostic)
4. Filter by manifest range
5. Pin highest matching tag
6. Exclude pre-release unless opt-in (same-tuple rule or explicit `prerelease: true`)
7. Record `constraint`, `resolved_tag`, `resolved_at` per req-lk-008

### R6: BFS Traversal + Diamond Conflict (req-rs-001)

Breadth-first traversal in **declaration order** of each manifest. Diamond conflict tri-modal:

1. **Intersection-pick** (default): all constraints have non-empty intersection → select highest satisfying version, record `resolved_by` (chain contributing tightest constraint)
2. **Fail-closed**: empty intersection → fail with diagnostic naming both root-to-conflict chains (req-rs-010 format)
3. **Nest reject**: `conflict_resolution: nest` → refuse with diagnostic citing §7.2(3) reserved for v0.2 (req-rs-013)

**Critical**: Python apm uses first-wins (non-conformant). Our implementation MUST do intersection-pick per spec.

### R7: Empty-Intersection Diagnostic (req-rs-010)

Format: each chain as `<owner>/<repo>@<constraint>` entries separated by `->`. Both chains named. Deterministic for a given install plan.

### R8: Depth Limit (req-rs-006)

Default **50**. Governance MAY tighten via `policy.dependencies.max_depth`. Exceeding → fail with diagnostic naming the chain at cap.

### R9: Lock Replay (req-rs-004, req-lk-009)

Replay locked `resolved_tag` when manifest constraint is **character-equal** to locked `constraint`. Any difference (including whitespace) triggers re-resolution.

### R10: Mirror Resolution (req-rs-009)

Registry dep satisfied by any registry/mirror IF bytes hash == `resolved_hash`. `resolved_url` is advisory (URL mismatch alone does NOT fail). Hash mismatch → fail-closed per req-lk-013.

### R11: "Why" Diagnostic (req-rs-005)

Bottom-up walk from target entry to root in lockfile. Return root-to-target chains in **lexicographic order** of path tuple. Offline, cycle-safe, deterministic.

### R12: Full Update (req-rs-011)

`apm update` (no args): re-resolve every direct dep against current manifest constraints. Rewrite lockfile pins. Re-resolve all transitive deps. Honour `require_pinned_constraint`.

### R13: Scoped Update (req-rs-012)

`apm update <name>`: scope to named package + subtree. Hold other pins. Refuse on frozen install without override flag.

### R14: Update Purge (req-lk-010)

On explicit update of a git-semver dep, purge install path before re-resolve (force download callback even if resolved tag unchanged).

## Constraints

1. **Semver library**: `deps.dev/util/semver` (Apache-2.0, passes 23/23 oracle cases). Only custom code: `MaxSatisfying` helper + build-metadata tie-break.
2. **No network in resolution tests**: use interfaces for git tag listing and package loading. Test with in-memory fixtures.
3. **yaml.Node architecture**: continue using SafeLoad → Node for any YAML parsing (lockfile types for replay).
4. **Determinism**: every function must be a pure function of its inputs (no hidden state, no ordering by map iteration).
5. **Anti-cheat**: test against oracle fixtures (semver-dialect.json). Code changes to pass; oracle never changes.

## Acceptance Criteria

- [ ] AC1: `deps.dev/util/semver` integrated, `MaxSatisfying` + build-metadata tie-break pass all 24 semver-dialect.json oracle cases
- [ ] AC2: Reference kind classifier passes table-driven tests for all 5 kinds + edge cases (local path, registry, semver ref, literal ref, marketplace)
- [ ] AC3: BFS resolver produces correct traversal order on a 3-level dependency graph fixture
- [ ] AC4: Diamond conflict: intersection-pick selects highest in intersection; fail-closed on empty intersection with correct `->` chain diagnostic
- [ ] AC5: `conflict_resolution: nest` rejected with diagnostic citing §7.2(3)
- [ ] AC6: Depth limit at 50 (and configurable) triggers fail with chain diagnostic
- [ ] AC7: Lock replay: character-equal constraint → reuse locked tag; whitespace-only change → re-resolve
- [ ] AC8: "Why" diagnostic produces lexicographically ordered chains, handles cycles
- [ ] AC9: Full update re-resolves all direct deps; scoped update limits to named package + subtree
- [ ] AC10: `go test ./...` passes, coverage ≥ 80% for new packages
- [ ] AC11: `go vet ./...` clean
- [ ] AC12: No actual network calls in unit tests (all behind interfaces)

## Dependencies

- Phase 1 complete (manifest parsing, DependencyReference, ParseDepString/Dict) ✅
- `deps.dev/util/semver` (new dependency)
- Oracle fixture: `conformance-kit/oracle/resolution/semver-dialect.json`

## Research References

- `.trellis/tasks/research-phase2-spec-resolution.md` — spec extraction (18 req-IDs)
- `.trellis/tasks/research-phase2-python-resolver.md` — Python apm analysis
- `.trellis/tasks/research-phase2-semver-libs.md` — Go semver library evaluation
