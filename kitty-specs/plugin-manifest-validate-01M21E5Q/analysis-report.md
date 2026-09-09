---
schema_version: 1
artifact_type: spec-kitty.analysis-report
command: /spec-kitty.analyze
mission_slug: plugin-manifest-validate-01M21E5Q
mission_id: 01M21E5QYY67PBDKTATX7HG6F1
generated_at: '2026-09-09T04:00:08.960400+00:00'
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
    sha256: ac4c4c045bae66ed964419713ab85689e0b9a5aabb2e3f8f411d31bd8955dcdd
  charter:
    path: .kittify\charter\charter.yaml
    sha256: 6ebbf8f921b444782654fa565b6768995fea13a8f66a8b5482a22887bd0eb186
verdict: ready
issue_counts:
  critical: 0
  low: 1
  high: 0
  medium: 2
  info: 0
findings:
- id: F008
  severity: medium
  category: inconsistency
  summary: tasks.md's T018/T019 rows still describe realexec verification as substring/single-file checks, weaker than the WP04 prompt file's and ticket 34's full stdout/stderr/exit-code/recursive-tree strength.
- id: F009
  severity: medium
  category: inconsistency
  summary: research.md R-02 and plan.md IC-02 prescribe reusing pack/bundle's plugin.json candidate list; WP03 T013 instead duplicates it locally, citing a C-002 rationale that does not hold since cmd/apm-go already imports pack/bundle.
- id: F010
  severity: low
  category: ambiguity
  summary: Two WP prompt subtask steps (WP01 T002 step 1, WP03 T014 step 1) retain a stray mid-sentence editorial self-correction ('-- wait,' / '...no --') that is confusing but not blocking.
---

## Specification Analysis Report

**Mission**: plugin-manifest-validate-01M21E5Q — `apm-go plugin validate`

### Resolved Since Prior Analysis

| Prior ID | Severity | Original Issue | Resolution Evidence |
|---|---|---|---|
| F001 | critical (charter alignment) | Charter text forbade any output-contract exception mechanism other than waivers/pending cases ("there is no other mechanism"), yet plan.md relied on a user ruling (ticket 34) to pin `plugin validate`'s contract via `tools/gate/realexec.sh` instead. | Verified directly in the working tree (commits `802e272`, `a7cff7f`): `.kittify/charter/charter.md` Quality Gates (Gate 2) and Exception Policy now name a third, narrowly scoped mechanism — applies only to a ticket-named apm-go-only command, only with ticket evidence the pinned Oracle lacks it, void and re-rulable if the Oracle later gains the command, "never extends by analogy to another command." `.kittify/charter/interview/answers.yaml` carries identical wording under `quality_gates` (lines 34-56) and `exception_policy` (lines 136-145). `PRODUCT.md` (checked on disk, lines 71 and 80) adds the realexec-contract sentence to the Output contract bullet and a `realexec contract` term to Terminology, both naming ticket 34 and the no-analogy limit. `.scratch/parity-runner/issues/34-oracle-less-command-output-contract.md` gained a "Verification strength" section requiring full stdout (line-for-line, not substring), stderr (asserted empty or compared in full), exact exit code, and a recursive file-tree comparison. `kitty-specs/.../tasks/WP04-gate-and-docs.md`'s success criteria and T018/T019 subtask steps (lines 88-89, 121-136) now instruct exactly that strength, plus the two new helpers (separate stdout/stderr capture; byte-for-byte comparison against a recorded expectation) ticket 34 demands. plan.md's Gate 2 row and research.md R-08 both cite the amended charter and the verification-strength upgrade. The exception is genuinely scoped, not a blanket carve-out. |
| F007 | low (inconsistency) | spec.md's illustrative "one check emits both an error and a warning" example was attached to WP01 T002 (Structure check), which short-circuits on any error and cannot produce that mix. | Verified in `kitty-specs/.../tasks/WP01-validator-core.md` T002 step 8 (line 152): the example test is now explicitly attached to the **Fields** check ("a manifest carrying both a type-error field and a non-object `metadata` puts an error and a warning in the same check"), with an explicit note that "Structure cannot produce that mix: step 6 short-circuits before a duplicate-key warning could sit beside a Structure error." Correctly resolved, not merely reworded. |

Both prior findings are dropped from the open findings list below; they are not carried forward.

### Open Findings

| ID | Category | Severity | Location(s) | Summary | Recommendation |
|----|----------|----------|-------------|---------|----------------|
| F008 | Inconsistency | MEDIUM | `kitty-specs/plugin-manifest-validate-01M21E5Q/tasks.md` T018/T019 rows (lines 143-144) vs `tasks/WP04-gate-and-docs.md` T018/T019 (lines 88-89, 121-136) and `.scratch/parity-runner/issues/34-oracle-less-command-output-contract.md` "Verification strength" | tasks.md's high-level checklist still says T018/T019 will assert "exit 0 and `\"0 errors\"` in stdout, plus a `cmp` read-only check of the manifest" — a substring/single-file check. The authoritative WP04 prompt file was correctly rewritten (a7cff7f) to the ticket's full strength (complete stdout compared to a recorded expectation, stderr asserted empty, recursive fixture-tree comparison), but tasks.md's own summary rows were not resynced, so a reader of tasks.md alone would underestimate what T018/T019 actually require. | Resync tasks.md's T018/T019 one-line descriptions to state the same strength as the WP04 prompt file (or shorten them to point at the prompt file as authoritative), so the two artifacts do not describe different verification strengths for the same subtasks. |
| F009 | Inconsistency | MEDIUM | `research.md` R-02 (Decision); `plan.md` IC-02 Risks row; `tasks/WP03-cli-subcommand.md` T013 step 1 | research.md's R-02 decision and plan.md's IC-02 risk note both say to directly reuse `internal/pack/bundle/producer.go`'s plugin.json candidate-location list ("apm-go 的 ... 已有同一清單，直接重用" / "建議直接重用該清單而非再抄一份"). WP03's T013 instead duplicates the list into a new local, unexported variable in `plugin_validate.go`, justified as "per this WP's prompt's own C-002 rationale" — a self-referential citation. C-002 bars a *new* module dependency or import edge outside ARCHITECTURE.md §1's graph; `cmd/apm-go` already imports `pack/bundle` in that graph, so exporting and reusing the existing candidate list would add no new edge. research.md was not updated to record this reversal, so the mission's own decision log now disagrees with what the tasks actually instruct. | Either (a) update research.md R-02 and plan.md IC-02 to record the decision to duplicate rather than reuse, with the real rationale (avoiding a public export of an internal implementation-detail slice, not an import-edge concern), or (b) change WP03 T013 to actually export and reuse `pluginJSONCandidates` from `pack/bundle` as originally decided. Either is fine; leaving research.md and the task instructions in disagreement is not. |
| F010 | Ambiguity | LOW | `tasks/WP01-validator-core.md` T002 step 1; `tasks/WP03-cli-subcommand.md` T014 step 1 | Two subtask steps retain a stray mid-sentence editorial self-correction from drafting: WP01 T002 step 1 reads "...checks `len(data) > 5*1024*1024` -- wait, per data-model.md the 5 MiB cap is enforced by the CLI layer..."; WP03 T014 step 1 reads "...even before checking whether a manifest was found... no -- print it only once a manifest PATH is known...". Both sentences resolve correctly if read to the end, but the "wait,"/"...no --" phrasing could momentarily read as if the first half is the instruction and the second half is a note, rather than the second half being the actual (corrected) instruction. | Cosmetic: rewrite both sentences to state the final instruction directly, dropping the self-correction language, the next time either file is touched. Not blocking implementation. |

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

**Charter Alignment Issues:** None open. (F001 above was the one charter-alignment issue; it is resolved.)

**Unmapped Tasks:** None found — every T001-T024 subtask traces to a WP whose Requirement Refs are covered in the spec.

**Metrics:**

- Total Requirements (FR+NFR): 17 (12 FR + 5 NFR)
- Total Constraints: 7
- Total Success Criteria: 6
- Total Subtasks: 24 (T001-T024)
- Coverage %: 100% (requirements with >=1 task)
- Ambiguity Count: 1 (F010)
- Duplication Count: 0
- Critical Issues Count: 0
