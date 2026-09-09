---
schema_version: 1
artifact_type: spec-kitty.analysis-report
command: /spec-kitty.analyze
mission_slug: plugin-manifest-validate-01M21E5Q
mission_id: 01M21E5QYY67PBDKTATX7HG6F1
generated_at: '2026-09-09T04:43:12.575827+00:00'
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
    sha256: 003fda67b3cf5eeb64a74b1932269ab7f75a2dba69051ce1163fa27e05fcecb8
  charter:
    path: .kittify\charter\charter.yaml
    sha256: 6ebbf8f921b444782654fa565b6768995fea13a8f66a8b5482a22887bd0eb186
verdict: ready
issue_counts:
  medium: 1
  low: 2
  high: 0
  critical: 0
  info: 0
findings:
- id: F020
  severity: low
  category: inconsistency
  summary: plan.md's Implementation Concern Map still attributes SC-002 to IC-03 and SC-004 to IC-04, contradicting tasks.md's Coverage Summary Table (SC-002 -> WP01/WP03, SC-004 -> WP03) which F014/F015 already corrected at the tasks.md level.
- id: F021
  severity: medium
  category: inconsistency
  summary: research.md R-08's Decision sentence says realexec verification is 'stdout substring + exit code + cmp read-only', directly contradicted two sentences later by the same bullet's Ruling sentence ('complete stdout/stderr/exit code/recursive tree, not substring') -- the same weak-verification defect class F008 already fixed in tasks.md/WP04, left unfixed here.
- id: F022
  severity: low
  category: underspecification
  summary: quickstart.md's step 4 gate command is plain 'sh tools/gate.sh', omitting the '-scope plugin-manifest-validate' flag that tasks.md T024 and WP04 require to actually produce the '.gate/plugin-manifest-validate/' evidence path the same line claims.
---

## Specification Analysis Report

Mission: plugin-manifest-validate-01M21E5Q -- apm-go plugin validate

This is the fourth `/spec-kitty.analyze` pass. The prior report (commit aeb7cd8) came back `ready` with eight open medium/low findings, F012-F019, all in the "tasks.md summary out of sync with the authoritative WP file" class. Commit 5210471 applied all eight fixes. This pass verifies each of those eight fixes against the current file contents, re-confirms F001/F007-F011 are still resolved, repeats the full T001-T024 sweep, and extends the sweep to plan.md, research.md, quickstart.md, data-model.md, and contracts/cli-plugin-validate.md, which the two prior passes had already cleared but this pass re-read in full.

### Verification of F012-F019 (commit 5210471)

| ID | Original Issue | Verified Current State |
|---|---|---|
| F012 | WP01 header omitted C-002, C-007 | tasks.md:29 now reads `...NFR-003, C-002, C-004, C-007, SC-002` -- C-002/C-007 present, matches WP01 frontmatter (`tasks/WP01-validator-core.md:6-15`). Resolved. |
| F013 | WP03 header omitted C-002, C-005, C-006 | tasks.md:103 now reads `...NFR-005, C-002, C-003, C-005, C-006, SC-002, SC-004` -- all three present, matches WP03 frontmatter (`tasks/WP03-cli-subcommand.md:7-20`). Resolved. |
| F014 | WP02 header wrongly claimed SC-002 | tasks.md:67 now reads `NFR-002, NFR-004, C-004, SC-003` -- SC-002 removed; SC-002 now sits on WP01 (tasks.md:29) and WP03 (tasks.md:103) instead, matching the Coverage Table. Resolved. |
| F015 | WP04 header wrongly claimed SC-004 | tasks.md:139 now reads `C-001, C-005, SC-001, SC-005, SC-006` -- SC-004 removed; SC-004 now sits on WP03 (tasks.md:103), matching the Coverage Table. Resolved. |
| F016 | Coverage Table NFR-002 row under-attributed | tasks.md Coverage Table row now reads `| NFR-002 | WP01, WP02 |`, matching WP01's header and frontmatter. Resolved. |
| F017 | T016 Subtask Index Parallel disagreed with WP03 | tasks.md Subtask Index now shows `T016 | ... | WP03 | P1 | Yes`, matching `tasks/WP03-cli-subcommand.md`'s own T016 "Parallel? Yes" line. Resolved. |
| F018 | T020 Subtask Index Parallel disagreed with WP04 | tasks.md Subtask Index now shows `T020 | ... | WP04 | P2 | Yes`, matching `tasks/WP04-gate-and-docs.md`'s own T020 "Parallel? Yes" line. Resolved. |
| F019 | T020 one-liner named only one target file | tasks.md:145 now names both `internal/pluginjson/validate.go` and `cmd/apm-go/plugin_validate.go`, matching WP04's T020 steps 1-2. Resolved. |

### Re-verification of F001, F007-F011

All six remain resolved, re-checked against current file contents:

- **F001** (charter, critical originally): `.kittify/charter/charter.md` Quality Gates (Gate 2) and Exception Policy still carry the ticket-34-scoped realexec exception; ticket 34 file unchanged. Still consistent with plan.md's Charter Check Gate 2 row.
- **F007**: WP01 T002 step 8 (`tasks/WP01-validator-core.md:152`) still attaches the mixed error+warning example to the Fields check, with the explicit note that Structure's step 6 short-circuits first. Still correct.
- **F008**: tasks.md T018/T019 (lines 143-144) and WP04's own T018/T019 sections both still read "the complete stdout against a recorded expectation... an empty stderr... a recursive before/after comparison" (ticket 34 verification strength). Still resolved **at the tasks.md/WP04 level** -- see F021 below for a newly-found regression of this exact defect class at the research.md level.
- **F009**: WP03 T013 step 1 (`tasks/WP03-cli-subcommand.md:130`) still exports and reuses `bundle.PluginJSONCandidates` rather than duplicating the list. Still resolved.
- **F010**: WP01 T002 step 1 and WP03 T014 step 1 both read as direct instructions, no stray self-correction language. Still resolved.
- **F011**: tasks.md:107 (WP03 T013 one-liner) still matches WP03's corrected export-and-reuse text verbatim. Still resolved.

### Systematic Sweep: tasks.md rows vs owning WP files (T001-T024)

Every subtask one-liner (Included Subtasks list, Subtask Index table) was re-compared against its WP file's subtask section: file paths, approach, and Parallel markers for T001-T024 all agree between tasks.md and the four WP files. No new drift in this class found.

### New findings from extending the sweep to plan.md, research.md, and quickstart.md

Three prior analyze passes limited the "tasks.md vs WP file" defect-class search to tasks.md itself. This pass additionally re-read plan.md, research.md, data-model.md, contracts/cli-plugin-validate.md, and quickstart.md end-to-end and found the same defect class (a planning document left stale after a downstream document was corrected) recurring in two of them, plus one unrelated command-accuracy gap:

- **F020** (low, inconsistency): plan.md's Implementation Concern Map (`plan.md:126`, IC-03 "Relevant requirements": `C-004、SC-002`; `plan.md:134`, IC-04 "Relevant requirements": `C-001、C-005、SC-001、SC-004、SC-005、SC-006`) still attributes SC-002 to IC-03 (which became WP02) and SC-004 to IC-04 (which became WP04). This is exactly the attribution F014/F015 corrected at the tasks.md level -- SC-002 (one test per acceptance scenario) is delivered by WP01/WP03's scenario tests, not WP02's schema-sync/fuzz/property tests; SC-004 (read-only directory-snapshot equality) is delivered by WP03's T017 snapshot test, not WP04's gate/doc work. plan.md's IC map was never revisited when F014/F015 were fixed downstream. Rated low, not medium, because IC-to-WP decomposition is explicitly documented as non-bijective (tasks.md preamble: "one concern may become multiple WPs; multiple concerns may merge into one WP") and tasks.md's Coverage Table -- the document implementers actually consult -- already carries the correct attribution; this is a residual plan.md documentation lag, not a coverage gap.
- **F021** (medium, inconsistency): research.md R-08 (`research.md:49-52`) contradicts itself within one bullet. The Decision sentence (line 49) says the realexec output contract is carried by "`tools/gate/realexec.sh` 的固定步驟（stdout 子字串 + exit code + `cmp` 只讀）" -- i.e., **substring**-based stdout checking. The same bullet's Ruling sentence (line 52), three sentences later, says "realexec 的驗證強度與 corpus 對齊：完整 stdout、stderr、exit code、遞迴檔案樹，**不用子字串**" -- i.e., explicitly **not** substring-based. This is the identical weak-verification defect class F008 found and fixed in tasks.md T018/T019 and WP04's own text (both of which correctly say "complete stdout... not a substring check"), but the fix was never propagated to research.md's own R-08 Decision line. Rated medium (matching F008's original severity for the same defect class), since research.md is one of the mission's canonical planning documents and DIRECTIVE_037 (living documentation sync) requires it to stay consistent with the corrected downstream artifacts.
- **F022** (low, underspecification): quickstart.md:22's copy-pasteable gate command is plain `sh tools/gate.sh`, with a trailing comment claiming `# evidence under .gate/plugin-manifest-validate/`. Every other reference to this command in the mission (tasks.md:137, tasks.md:149, `tasks/WP04-gate-and-docs.md` at four separate lines) uses `sh tools/gate.sh -scope plugin-manifest-validate`. Running quickstart.md's literal command as written does not target the mission's scope and will not produce evidence at the claimed path.

### Normal Detection Passes (re-run this pass)

- **Charter alignment**: No conflicts. Gate 2's ticket-34 exception is present, correctly scoped, and consistent across charter.md, plan.md, research.md R-08's Ruling (not Decision -- see F021), tasks.md, and WP04.
- **Duplication**: None found among FR/NFR/C/SC requirements or between plan.md's ICs and tasks.md's WPs.
- **Ambiguity**: None found (no vague adjectives without measurable criteria; no unresolved placeholders in any of the nine artifacts read this pass).
- **Underspecification**: None found beyond F022. FR-006's path-typed field list, data-model.md's IsPath invariants, and WP01's Context field list still agree exactly.
- **Coverage gaps**: None. Every FR/NFR/C/SC in spec.md maps to at least one WP in tasks.md's Coverage Summary Table.
- **Cross-artifact message-text consistency**: contracts/cli-plugin-validate.md's Messages table, spec.md's acceptance-scenario wording, and WP01's per-check message instructions still agree verbatim everywhere checked.
- **Known-field set**: research.md R-03, WP01's Context section, and data-model.md's Rule-table invariants still agree exactly (22 schema fields, 5 docs-only fields, 1 apm-go field).

Coverage Summary Table: unchanged from the prior pass -- 100% of FR/NFR/C/SC requirements map to at least one WP; no requirement, constraint, or success criterion has zero task coverage.

Charter Alignment Issues: None open.

Unmapped Tasks: None found.

Metrics:

- Total Requirements (FR+NFR): 17 (12 FR + 5 NFR)
- Total Constraints: 7
- Total Success Criteria: 6
- Total Subtasks: 24 (T001-T024)
- Coverage %: 100%
- Ambiguity Count: 0
- Duplication Count: 0
- Inconsistency Count: 2 (F020, F021)
- Underspecification Count: 1 (F022)
- Critical Issues Count: 0

## Next Actions

- Verdict is `ready` (no high/critical findings) -- `/implement` may proceed for WP01-WP04 without waiting on these fixes.
- Recommended cleanup (all mechanical, no design change):
  - plan.md:126 -- remove `SC-002` from IC-03's "Relevant requirements" line.
  - plan.md:134 -- remove `SC-004` from IC-04's "Relevant requirements" line.
  - research.md:49 -- change "stdout 子字串" to the full-strength wording already used in the same bullet's Ruling sentence ("完整 stdout、stderr、exit code、遞迴檔案樹"), removing the substring characterization.
  - quickstart.md:22 -- change `sh tools/gate.sh` to `sh tools/gate.sh -scope plugin-manifest-validate`.
- This is now the second consecutive pass where the same "downstream document corrected, upstream document left stale" defect class recurs (previously tasks.md-vs-WP-file; now plan.md-vs-tasks.md and research.md's own internal self-contradiction). Consider a standing cross-document resync check at the end of `/spec-kitty.tasks` (as the prior report already recommended) extended to also diff plan.md's IC map and research.md's decision bullets against whatever tasks.md ends up saying, not just tasks.md against the WP files.
