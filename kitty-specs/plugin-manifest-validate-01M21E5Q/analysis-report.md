---
schema_version: 1
artifact_type: spec-kitty.analysis-report
command: /spec-kitty.analyze
mission_slug: plugin-manifest-validate-01M21E5Q
mission_id: 01M21E5QYY67PBDKTATX7HG6F1
generated_at: '2026-09-09T04:20:58.090354+00:00'
analyzer_agent: unknown
input_artifacts:
  spec.md:
    path: kitty-specs\plugin-manifest-validate-01M21E5Q\spec.md
    sha256: a379703ae6b5599b2c2ba870ab2f601d17917893b85b773f369661237f040ce6
  plan.md:
    path: kitty-specs\plugin-manifest-validate-01M21E5Q\plan.md
    sha256: 711c59dd970562a24692481dfaa90f66bac28ef559421f9207ef4f4b1cca13be
  tasks.md:
    path: kitty-specs\plugin-manifest-validate-01M21E5Q\tasks.md
    sha256: f757dc8dd5842819b981cedf8931ac896f5a47b968c671ec6cb3af459899e88e
  charter:
    path: .kittify\charter\charter.yaml
    sha256: 6ebbf8f921b444782654fa565b6768995fea13a8f66a8b5482a22887bd0eb186
verdict: ready
issue_counts:
  high: 0
  low: 0
  critical: 0
  medium: 1
  info: 0
findings:
- id: F011
  severity: medium
  category: inconsistency
  summary: tasks.md T013's one-line description still says the plugin.json candidate list is duplicated locally with no cross-package export, contradicting WP03-cli-subcommand.md's corrected T013 (export pluginJSONCandidates as PluginJSONCandidates and reuse it) -- the actual fix applied for prior finding F009 landed in the WP file but was never resynced into tasks.md.
---

## Specification Analysis Report

**Mission**: plugin-manifest-validate-01M21E5Q — `apm-go plugin validate`

This is a rerun of `/spec-kitty.analyze` after commit b6b258d remediated the three findings (F008, F009, F010) from the prior report (commit d47afde). Every prior finding and this rerun's new finding were verified by reading the current file contents directly, not by trusting the prior report's summary.

### Resolved Since Prior Analysis

| Prior ID | Severity | Original Issue | Resolution Evidence |
|---|---|---|---|
| F001 | critical (charter alignment) | Charter text forbade any output-contract exception mechanism other than waivers/pending cases. | Re-verified: `.kittify/charter/charter.md` Quality Gates (Gate 2) and Exception Policy still carry the third, narrowly-scoped mechanism for a ticket-named apm-go-only command whose pinned Oracle lacks it, naming ticket 34, void if the Oracle later gains the command, "never extends by analogy to another command." Still consistent. |
| F007 | low (inconsistency) | spec.md's "one check emits both an error and a warning" example was attached to WP01 T002 (Structure), which short-circuits on any error and cannot produce that mix. | Re-verified in `tasks/WP01-validator-core.md` T002 step 8: the mixed error+warning example is attached to the **Fields** check, with an explicit note that Structure's step 6 short-circuits before a duplicate-key warning could sit beside a Structure error. Still correct. |
| F008 | medium (inconsistency) | tasks.md's T018/T019 one-liners described weaker (substring/single-file) realexec verification than the WP04 prompt file's and ticket 34's full-strength requirement. | Verified in `kitty-specs/plugin-manifest-validate-01M21E5Q/tasks.md` (current) T018/T019: both now read "...each asserting exit 0/the exact exit code, the complete stdout against a recorded expectation, an empty stderr, and a recursive before/after comparison of the fixture tree (ticket 34 verification strength)" — matches `tasks/WP04-gate-and-docs.md`'s T018/T019 exactly. Both artifacts resynced. Fully resolved. |
| F009 | medium (inconsistency) | research.md R-02 and plan.md IC-02 prescribe reusing `pack/bundle`'s plugin.json candidate list; WP03 T013 instead duplicated it locally, citing a C-002 rationale that does not hold since `cmd/apm-go` already imports `pack/bundle`. | Verified in `tasks/WP03-cli-subcommand.md` T013 step 1 (current): now instructs exporting `pluginJSONCandidates` as `PluginJSONCandidates` in `internal/pack/bundle/producer.go` and referencing `bundle.PluginJSONCandidates` directly from `plugin_validate.go`, matching research.md R-02 and plan.md IC-02. `owned_files` for WP03 now lists `internal/pack/bundle/producer.go`; grepped the whole mission folder and confirmed no other WP's `owned_files` includes that path. The rationale is accurate: `cmd/apm-go` already imports `internal/pack/bundle` today (confirmed via grep — `pack.go`, `install.go`, `audit_content.go`, plus test files — all pre-dating this mission), so exporting one symbol from an already-imported package adds no new edge to ARCHITECTURE.md §1. **However**, the fix was applied only to the WP03 prompt file, not to `tasks.md` itself — see new finding F011 below, which is this rerun's only open item. |
| F010 | low (ambiguity) | Two WP prompt subtask steps (WP01 T002 step 1, WP03 T014 step 1) retained a stray mid-sentence editorial self-correction ("-- wait," / "...no --"). | Re-verified: both steps now read as direct instructions with no self-correction language. Fully resolved. |

### Open Findings

| ID | Category | Severity | Location(s) | Summary | Recommendation |
|----|----------|----------|-------------|---------|----------------|
| F011 | Inconsistency | MEDIUM | `kitty-specs/plugin-manifest-validate-01M21E5Q/tasks.md` line 107 (WP03 T013 one-liner) vs `tasks/WP03-cli-subcommand.md` T013 step 1 (lines 130-131) | F009's remediation (commit b6b258d) rewrote `tasks/WP03-cli-subcommand.md`'s T013 to export `pluginJSONCandidates` as `PluginJSONCandidates` from `internal/pack/bundle/producer.go` and reuse it from `plugin_validate.go` — matching research.md R-02 and plan.md IC-02 as intended. But `tasks.md`'s own one-line summary for T013 was never resynced: it still reads "the four-candidate probe order (duplicated locally with a doc comment citing `internal/pack/bundle/producer.go`'s `pluginJSONCandidates` as the source of truth — no new cross-package export, per C-002)" — the exact opposite of what the WP file now instructs. This is the same class of defect F008 named (tasks.md not resynced with a corrected WP file), recurring on a sibling finding's fix. tasks.md itself states "Treat this file as the high-level checklist; keep deep implementation detail inside the prompt files," so the WP file's instruction governs implementation — but a reader relying on tasks.md alone (as the implement gate and reviewers often do) is told the wrong design decision. | Rewrite tasks.md's T013 one-liner to state the export-and-reuse approach (or shorten it to defer to the WP file), so tasks.md and `tasks/WP03-cli-subcommand.md` no longer describe opposite C-002 dispositions for the same subtask. |

**Coverage Summary Table:**

| Requirement Key | Has Task? | Task IDs | Notes |
|-----------------|-----------|----------|-------|
| FR-001..FR-002, FR-008..FR-012 | Yes | WP03 (T013-T017) | CLI surface, locate, strict, output, exit codes, verbose, help |
| FR-003..FR-007 | Yes | WP01 (T001-T007) | Structure/Name/Fields/Paths/Unrecognized checks |
| NFR-001 | Yes | WP03 (T017) | read-only snapshot test |
| NFR-002 | Yes | WP02 (T011-T012) | fuzz |
| NFR-003 | Yes | WP01, WP03 (T013) | no symlink/parent escape |
| NFR-004 | Yes | WP02 (T010) | property test |
| NFR-005 | Yes (rationale-only) | WP03 | plan.md explicitly records this as an intentional non-executable, documented deviation (Low priority) — not a gap |
| C-001..C-007 | Yes | WP01/WP03/WP04 as mapped in tasks.md's own Requirements Coverage Summary | verified against tasks.md table, no zero-coverage rows found |
| SC-001..SC-006 | Yes | WP01-WP04 per tasks.md table | no zero-coverage rows found |

No requirement, constraint, or success criterion was found with zero mapped task coverage.

**Charter Alignment Issues:** None open. F001 (the one charter-alignment issue ever raised) remains resolved; the third exception mechanism for an Oracle-less apm-go-only command is still present in `.kittify/charter/charter.md`'s Quality Gates and Exception Policy, correctly scoped to ticket 34.

**Unmapped Tasks:** None found — every T001-T024 subtask traces to a WP whose Requirement Refs are covered in the spec.

**Metrics:**

- Total Requirements (FR+NFR): 17 (12 FR + 5 NFR)
- Total Constraints: 7
- Total Success Criteria: 6
- Total Subtasks: 24 (T001-T024)
- Coverage %: 100% (requirements with >=1 task)
- Ambiguity Count: 0
- Duplication Count: 0
- Inconsistency Count: 1 (F011)
- Critical Issues Count: 0

## Next Actions

- Only one open finding (F011), severity MEDIUM: verdict is `ready`, so `/implement` may proceed for WP01-WP04 without waiting on this fix.
- Recommended before or during WP03's implementation: resync `tasks.md`'s T013 one-liner (line 107) to match `tasks/WP03-cli-subcommand.md`'s actual instruction (export-and-reuse), since WP03's own `owned_files`/Context section already commit to that approach — leaving tasks.md stating the opposite is a documentation-quality risk, not a functional blocker.
- No other artifact requires refinement; `/spec-kitty.plan` and `/spec-kitty.tasks` do not need to be rerun.
