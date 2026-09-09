---
schema_version: 1
artifact_type: spec-kitty.analysis-report
command: /spec-kitty.analyze
mission_slug: plugin-manifest-validate-01M21E5Q
mission_id: 01M21E5QYY67PBDKTATX7HG6F1
generated_at: '2026-09-09T04:33:06.054950+00:00'
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
    sha256: bd3ed2bd5f430b4dd8aa69ccf271981374537477438d1660733ededc63efde85
  charter:
    path: .kittify\charter\charter.yaml
    sha256: 6ebbf8f921b444782654fa565b6768995fea13a8f66a8b5482a22887bd0eb186
verdict: ready
issue_counts:
  medium: 6
  low: 2
  critical: 0
  high: 0
  info: 0
findings:
- id: F012
  severity: medium
  category: inconsistency
  summary: tasks.md WP01 Requirement Refs line omits C-002 and C-007, which WP01 own frontmatter and tasks.md own Coverage Summary Table both attribute to WP01.
- id: F013
  severity: medium
  category: inconsistency
  summary: tasks.md WP03 Requirement Refs line omits C-002, C-005, C-006, which WP03 own frontmatter and tasks.md own Coverage Summary Table both attribute to WP03.
- id: F014
  severity: medium
  category: inconsistency
  summary: tasks.md WP02 Requirement Refs line claims SC-002, but the Coverage Summary Table and SC-002 own content (one test per acceptance scenario, delivered by WP01/WP03) attribute it to WP01 and WP03, not WP02.
- id: F015
  severity: medium
  category: inconsistency
  summary: tasks.md WP04 Requirement Refs line claims SC-004, but the Coverage Summary Table and SC-004 own content (read-only directory-snapshot equality, delivered by WP03 T017) attribute it to WP03, not WP04.
- id: F016
  severity: medium
  category: inconsistency
  summary: tasks.md Requirements Coverage Summary Table attributes NFR-002 solely to WP02, contradicting WP01 own Requirement Refs line and frontmatter, which both also claim NFR-002 (WP01 T002 defensive size-cap check).
- id: F017
  severity: low
  category: inconsistency
  summary: tasks.md Subtask Index marks T016 as Parallel No, but tasks/WP03-cli-subcommand.md own T016 section states Parallel Yes, relative to T013-T015 in principle.
- id: F018
  severity: low
  category: inconsistency
  summary: tasks.md Subtask Index marks T020 as Parallel No, but tasks/WP04-gate-and-docs.md own T020 section states Parallel Yes, relative to T018/T019 (different file).
- id: F019
  severity: medium
  category: inconsistency
  summary: tasks.md T020 one-liner says both new mutants target internal/pluginjson/validate.go, but tasks/WP04-gate-and-docs.md T020 step 2 places the second mutant (the strict upgrade-to-failure path) in cmd/apm-go/plugin_validate.go, a file owned by WP03.
---

## Specification Analysis Report

Mission: plugin-manifest-validate-01M21E5Q -- apm-go plugin validate

This is the third /spec-kitty.analyze pass. The prior report (commit 9dcc3c7) came back ready with one open medium finding, F011 (tasks.md T013 one-liner contradicted WP03-cli-subcommand.md corrected T013). Before the normal detection passes, this run did a dedicated systematic sweep -- because the same defect class (a tasks.md summary line left contradicting an edited WP file) has now recurred twice (F008, then F011) -- comparing every one of T001-T024 tasks.md row against its owning WP file subtask section, and every WP tasks.md summary block against that WP file frontmatter and Objectives.

### F011 verification

tasks.md line 107 (WP03 T013) now reads: "the four-candidate probe order (reused from internal/pack/bundle/producer.go, whose pluginJSONCandidates is exported as PluginJSONCandidates for this purpose, so the order has one definition; cmd/apm-go already imports that package, so C-002 adds no import edge)". This matches tasks/WP03-cli-subcommand.md T013 step 1 exactly (export-and-reuse, not duplicate-locally). F011 is resolved -- commit 16a7e6c rewrite is confirmed against the current file contents.

### Resolved Since Prior Analysis (re-verified this pass)

| Prior ID | Severity | Original Issue | Resolution Evidence |
|---|---|---|---|
| F001 | critical (charter alignment) | Charter forbade any output-contract exception mechanism other than waivers/pending cases. | Re-verified: .kittify/charter/charter.md Quality Gates (Gate 2) and Exception Policy still carry the third, ticket-scoped mechanism naming ticket 34; ticket 34 file itself (.scratch/parity-runner/issues/34-oracle-less-command-output-contract.md) still records the same ruling, quote, and 4-surface verification strength plan.md cites. Still consistent. |
| F007 | low (inconsistency) | The "one check emits both an error and a warning" example was attached to Structure, which short-circuits and cannot produce that mix. | Re-verified in tasks/WP01-validator-core.md T002 step 8: the mixed example is attached to the Fields check, with an explicit note that Structure step 6 short-circuits first. Still correct. |
| F008 | medium (inconsistency) | tasks.md T018/T019 described weaker (substring) realexec verification than WP04 full-strength requirement. | Re-verified: both tasks.md and tasks/WP04-gate-and-docs.md now read the complete stdout against a recorded expectation, an empty stderr, and a recursive before/after comparison of the fixture tree (ticket 34 verification strength) for T018 and T019. Still resolved. |
| F009 | medium (inconsistency) | WP03 T013 duplicated the candidate list locally instead of reusing pack/bundle. | Re-verified in tasks/WP03-cli-subcommand.md T013 step 1: exports and reuses bundle.PluginJSONCandidates. Still resolved (this is the same fix F011 re-checks at the tasks.md-sync level). |
| F010 | low (ambiguity) | Stray mid-sentence editorial self-correction in WP01 T002 step 1 and WP03 T014 step 1. | Re-verified: both read as direct instructions with no self-correction language. Still resolved. |

### Systematic Sweep: tasks.md rows vs owning WP files (T001-T024)

Every subtask tasks.md one-liner (Included Subtasks list and Subtask Index table) was compared against its WP file subtask section. T001-T015, T017, T021-T024 approach, file list, and parallel markers match their WP files exactly. Two classes of drift were found, both new:

1. Parallel-marker disagreement (F017, F018): tasks.md Subtask Index table is the sole place recording a Parallel yes/no verdict per subtask distinct from the WP file own Parallel line. T016 and T020 disagree between the two documents (tasks.md says No for both; the owning WP file says Yes for both). This affects execution scheduling guidance only -- it does not change what gets built.
2. File-target disagreement (F019): T020 tasks.md one-liner names a single file (internal/pluginjson/validate.go) for both new mutants, but WP04 own T020 step 2 places the second mutant in cmd/apm-go/plugin_validate.go -- the strict upgrade-to-failure logic lives in WP03 CLI layer, not WP01 validator core. This is the same defect class as F008/F009/F011: a tasks.md summary line describing an approach/location the owning WP file no longer (or never did) state.

### Systematic Sweep: WP summary blocks vs WP file frontmatter/Objectives

Each WP Goal, Independent Test, Dependencies, and Risks lines in tasks.md were compared against the corresponding WP file Objectives/Success-Criteria/Dependencies/Risks sections. Goal, Independent Test, and Dependencies text match in all four WPs. The Requirement Refs line -- a field with no direct WP-file analogue other than the frontmatter requirement_refs list -- disagrees with both the owning WP file frontmatter and tasks.md own Requirements Coverage Summary Table in four places (F012-F015), plus one place where the Coverage Summary Table itself disagrees with a WP own claim (F016):

- WP01 line omits C-002, C-007 (present in its frontmatter and in the Coverage Table C-002/C-007 rows).
- WP03 line omits C-002, C-005, C-006 (present in its frontmatter and in the Coverage Table rows).
- WP02 line claims SC-002, but SC-002 (one test per acceptance scenario) is delivered by WP01/WP03 test files, not WP02 fuzz/property/schema-sync files -- the Coverage Table agrees with this, not with WP02 own header line.
- WP04 line claims SC-004, but SC-004 (read-only directory-snapshot equality) is delivered by WP03 T017 snapshot test, not WP04 gate/doc work -- the Coverage Table agrees with this, not with WP04 own header line.
- The Coverage Table NFR-002 row names only WP02, but WP01 own header line and frontmatter also claim NFR-002 (WP01 T002 implements a defensive last-line-of-defense size check for it) -- a three-way disagreement where two sources (WP01 header plus WP01 frontmatter) outvote the Coverage Table.

None of F012-F019 changes what code gets written; they are traceability/bookkeeping metadata used for requirement-coverage auditing (DIRECTIVE_003, DIRECTIVE_010), which is why they are rated medium/low rather than high.

### Normal Detection Passes

- Charter alignment: No conflicts. The Gate 2 exception (ticket 34) is present, correctly scoped, and its 4-surface verification strength (stdout/stderr/exit code/file tree, not substrings) is carried consistently through charter.md, plan.md, tasks.md (T018/T019), ticket 34, and WP04 own text.
- Duplication: None found among FR/NFR/C/SC requirements.
- Ambiguity: None found (no vague adjectives without measurable criteria; no unresolved placeholders).
- Underspecification: None found. FR-006 path-typed field list, data-model.md IsPath set, and WP01 Context field list all agree exactly (skills, commands, agents, workflows, hooks, mcpServers, outputStyles, lspServers, experimental.themes, experimental.monitors).
- Coverage gaps: None. Every FR/NFR/C/SC in spec.md maps to at least one WP in tasks.md Coverage Summary Table.
- Cross-artifact inconsistency (message text): contracts/cli-plugin-validate.md Messages table, spec.md acceptance-scenario wording, and WP01 per-check message instructions agree verbatim everywhere checked (Structure, Name, Fields, Paths, Unrecognized messages, and the exit-code table).
- Known-field set: research.md R-03 schema/docs/apm-go field lists, WP01 Context section full known-field set, and data-model.md Rule-table invariants agree exactly (22 schema fields, 5 docs-only fields, 1 apm-go field).

Coverage Summary Table:

| Requirement Key | Has Task? | Task IDs | Notes |
|-----------------|-----------|----------|-------|
| FR-001..FR-002, FR-008..FR-012 | Yes | WP03 (T013-T017) | CLI surface, locate, strict, output, exit codes, verbose, help |
| FR-003..FR-007 | Yes | WP01 (T001-T007) | Structure/Name/Fields/Paths/Unrecognized checks |
| NFR-001 | Yes | WP03 (T017) | read-only snapshot test |
| NFR-002 | Yes | WP01, WP02 (T011-T012) | fuzz; see F016 (Coverage Table itself under-attributes this) |
| NFR-003 | Yes | WP01, WP03 (T013) | no symlink/parent escape |
| NFR-004 | Yes | WP02 (T010) | property test |
| NFR-005 | Yes (rationale-only) | WP03 | plan.md records this as an intentional non-executable, documented deviation (Low priority) |
| C-001..C-007 | Yes | WP01/WP03/WP04 per tasks.md Coverage Table | see F012/F013 for header-line omissions |
| SC-001..SC-006 | Yes | WP01-WP04 per tasks.md Coverage Table | see F014/F015 for header-line misattributions |

No requirement, constraint, or success criterion was found with zero mapped task coverage.

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
- Inconsistency Count: 8 (F012-F019)
- Critical Issues Count: 0

## Next Actions

- Verdict is ready (no high/critical findings) -- /implement may proceed for WP01-WP04 without waiting on these fixes.
- Recommended cleanup (all mechanical, no design change): resync tasks.md four WP Requirement Refs lines against each WP file frontmatter requirement_refs (F012, F013), and against the Coverage Summary Table actual SC attributions (F014, F015); fix the Coverage Table NFR-002 row to include WP01 (F016); align the two Subtask Index Parallel cells with their WP files (F017, F018); and correct T020 one-liner to name both target files -- internal/pluginjson/validate.go and cmd/apm-go/plugin_validate.go (F019).
- Given this is the third consecutive pass to find a tasks.md/WP-file sync gap (F008 to F011 to F012-F019), consider adding this resync check as a standing step at the end of /spec-kitty.tasks generation rather than relying on /spec-kitty.analyze to keep catching it.
