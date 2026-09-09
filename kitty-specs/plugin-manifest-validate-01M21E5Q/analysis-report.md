---
schema_version: 1
artifact_type: spec-kitty.analysis-report
command: /spec-kitty.analyze
mission_slug: plugin-manifest-validate-01M21E5Q
mission_id: 01M21E5QYY67PBDKTATX7HG6F1
generated_at: '2026-09-09T02:30:28.454411+00:00'
analyzer_agent: unknown
input_artifacts:
  spec.md:
    path: kitty-specs\plugin-manifest-validate-01M21E5Q\spec.md
    sha256: a379703ae6b5599b2c2ba870ab2f601d17917893b85b773f369661237f040ce6
  plan.md:
    path: kitty-specs\plugin-manifest-validate-01M21E5Q\plan.md
    sha256: de337731c30a4086481b469780a40515566237b95e1706056abedca56fe50b30
  tasks.md:
    path: kitty-specs\plugin-manifest-validate-01M21E5Q\tasks.md
    sha256: ac4c4c045bae66ed964419713ab85689e0b9a5aabb2e3f8f411d31bd8955dcdd
  charter:
    path: .kittify\charter\charter.yaml
    sha256: 6ebbf8f921b444782654fa565b6768995fea13a8f66a8b5482a22887bd0eb186
verdict: blocked
issue_counts:
  low: 2
  medium: 2
  high: 1
  critical: 1
  info: 0
findings:
- id: F001
  severity: critical
  category: charter-alignment
  summary: >-
    plan.md Gate 2 disposition (no tools/parity corpus case, no
    cases-pending entry; substitute evidence = tools/gate/realexec.sh steps
    plus a plugin.go deviation comment) conflicts with the charter
    Exception Policy (exceptions to the output contract are expressed only
    as waivers in tools/parity/waivers.json or as pending cases with a
    ticket; there is no other mechanism) and Quality Gate 2 (any change
    to user-visible output, exit codes or generated files must add or
    update a corpus case; a design deviation that cannot be waived is
    parked in tools/parity/cases-pending/ with a ticket, never silently
    dropped). plan.md own Charter Check row marks this needs ruling
    and explicitly rejects the pending-case route, but no ticket
    or explicit user ruling has been recorded to authorize that rejection.
- id: F002
  severity: high
  category: coverage-gap
  summary: >-
    SC-002 requires one automated test per spec.md edge case. Two edge
    cases (multiple candidate plugin.json locations with first match wins
    and -v lists known fields in file order) are explicitly excluded
    from WP01 test list with a note that this is WP03 concern, but WP03
    T017 subtask steps never name either edge case (T017 lists only the
    AS-numbered acceptance scenarios, --help, and the read-only snapshot).
    Neither WP owns an explicit test for them.
- id: F003
  severity: medium
  category: underspecification
  summary: >-
    Report.KnownFieldsPresent (needed by FR-011 and WP03 verbose
    rendering) has no WP01 subtask that explicitly populates it. T002
    token-order duplicate-key scan is the only plausible data source for a
    file-ordered known-field list, but reusing it for this purpose is never
    stated in any T001-T007 step.
- id: F004
  severity: medium
  category: inconsistency
  summary: >-
    Report.Findings ordering is specified two different ways: data-model.md
    states file order within a check, while WP03 T014 instructs errors
    first then warnings within that check, and its own Notes paragraph
    flags the contradiction without resolving it. The contract worked
    example never shows both levels in one check, so it cannot arbitrate
    between the two documents.
- id: F005
  severity: low
  category: coverage-gap
  summary: >-
    NFR-005 (5 MiB manifest validates within 1 second) has no subtask in
    any WP that executes a timing assertion; it is backed only by plan.md
    complexity-order rationale, not an executable check, and the
    Requirements Coverage Summary does not flag this as rationale-only.
- id: F006
  severity: low
  category: coverage-gap
  summary: >-
    WP01 requirement_refs frontmatter lists NFR-003 (read scope and
    symlink safety), but Validate([]byte) Report is filesystem-free by
    construction and no WP01 subtask implements or tests symlink or
    parent-directory restrictions -- that behavior is entirely WP03 T013.
    The mapping is not wrong but is not self-evidently traceable without a
    note explaining why WP01 satisfies it trivially.
---

## Specification Analysis Report

| ID | Category | Severity | Location(s) | Summary | Recommendation |
|---|---|---|---|---|---|
| F001 | Charter Alignment | Critical | plan.md Charter Check Gate 2 row (line 40); plan.md Complexity Tracking (line 96); research.md R-08 (lines 47-51); tasks/WP04-gate-and-docs.md T018-T020; charter .kittify/charter/charter.md Quality Gates (line 14) and Exception Policy (line 308) | Plan's chosen Gate-2 disposition (realexec.sh + code comment, no parity/pending case) conflicts with the charter's explicit "there is no other mechanism" exception policy for the output contract. | Before WP04 executes: either record an explicit user ruling quoted verbatim in the PR description plus a .scratch/parity-runner/issues/ ticket, or add a tools/parity/cases-pending/ entry with that ticket, per the charter's own exception mechanism. Keep realexec.sh/mutants as supplementary evidence, not a substitute. |
| F002 | Coverage Gap | High | tasks/WP01-validator-core.md T007 step 3 (line ~226); tasks/WP03-cli-subcommand.md T017 steps 1-5 (lines 181-186) | Two spec.md edge cases (multiple candidate manifest locations; -v known-fields listing) are explicitly deferred by WP01 to WP03, but WP03's T017 never names either as a required subtest. | Add explicit bullets to WP03 T017 naming both edge-case tests so SC-002's coverage promise is not dropped at the WP hand-off boundary. |
| F003 | Underspecification | Medium | tasks/WP01-validator-core.md T001 (line 132), T002 (lines 141-153); data-model.md line 42 (KnownFieldsPresent) | No subtask explicitly populates Report.KnownFieldsPresent, which WP03's verbose rendering (T014) depends on. | Add an explicit step to T002 (or a new micro-step) instructing that the file-ordered list of recognized top-level keys be captured into Report.KnownFieldsPresent, reusing the token-stream pass already specified for duplicate-key detection. |
| F004 | Inconsistency | Medium | data-model.md line 41; tasks/WP03-cli-subcommand.md T014 step 4 and Notes (lines 147-152); contracts/cli-plugin-validate.md worked example (lines 16-28) | Report.Findings ordering is stated as file order within a check in data-model.md but as errors before warnings within a check in WP03's T014; the contract's example never exercises the mixed case. | Pick one ordering rule and align data-model.md and WP03's T014 before WP01 fixes Report.Findings storage order. |
| F005 | Coverage Gap | Low | spec.md NFR-005; plan.md Performance Goals (line 22); no corresponding step in WP01/WP02/WP03 | NFR-005's 1-second budget has no executable timing check anywhere in tasks.md. | Acceptable to close via documented rationale given Low priority and the validator's small footprint, but tasks.md's coverage table should say "rationale-only" rather than implying a functional test exercises it. |
| F006 | Coverage Gap | Low | tasks/WP01-validator-core.md frontmatter requirement_refs (NFR-003); tasks/WP03-cli-subcommand.md T013 step 5 | WP01 lists NFR-003 as a requirement ref but implements none of its symlink/parent-dir restrictions (that is entirely WP03's T013); the mapping holds only because Validate has no filesystem access at all. | No code change needed; add a one-line note in WP01's Context section explaining why NFR-003 is trivially satisfied there, to save a future reviewer's trace-back effort. |

## Coverage Summary Table

| Requirement | Has Task? | Task IDs | Notes |
|---|---|---|---|
| FR-001 | Yes | WP03 (T013-T015) | |
| FR-002 | Yes | WP03 (T013) | |
| FR-003 | Yes | WP01 (T002) | |
| FR-004 | Yes | WP01 (T003) | |
| FR-005 | Yes | WP01 (T004) | |
| FR-006 | Yes | WP01 (T005) | |
| FR-007 | Yes | WP01 (T006) | |
| FR-008 | Yes | WP03 (T015) | |
| FR-009 | Yes | WP03 (T014) | |
| FR-010 | Yes | WP03 (T015) | |
| FR-011 | Partial | WP03 (T014) | Rendering covered; backing data population has no WP01 subtask (F003) |
| FR-012 | Yes | WP03 (T017) | |
| NFR-001 | Yes | WP03 (T017) | Read-only snapshot test |
| NFR-002 | Yes | WP02 (T011, T012) | 30s fuzz run |
| NFR-003 | Yes (concrete in WP03) | WP01 (implicit), WP03 (T013) | WP01's share is by absence of FS access, not an explicit subtask (F006) |
| NFR-004 | Yes | WP02 (T010) | testing/quick property, >=100 iterations |
| NFR-005 | No executable check | -- | Rationale-only in plan.md (F005) |
| C-001 | Yes | WP04 (T024) | Contingent on F001's resolution |
| C-002 | Yes | WP01, WP03 | stdlib-only; no new import edges |
| C-003 | Yes | WP03 (T014) | PRODUCT.md symbol vocabulary |
| C-004 | Yes | WP01 (T001), WP02 (T009) | Schema-sync anti-drift test |
| C-005 | Yes | WP03 (T016), WP04 (T021-T023) | |
| C-006 | Yes | WP03 (by omission - no auto-call added) | |
| C-007 | Yes | WP01 (T005, syntax-only) | |
| SC-001 | Yes | WP04 (T018) | |
| SC-002 | Partial | WP01 (T007), WP03 (T017) | Two edge cases unassigned (F002) |
| SC-003 | Yes | WP02 (T011, T012) | |
| SC-004 | Yes | WP03 (T017) | |
| SC-005 | Yes | WP04 (T024) | Contingent on F001's resolution |
| SC-006 | Yes | WP04 | |

## Charter Alignment Issues

- F001 (critical): the mission's Gate-2 disposition for an Oracle-less command bypasses the charter's stated "no other mechanism" exception policy for the output contract. This is the mission's single blocking item; everything else in tasks.md is internally consistent with the charter's TDD, locality-of-change, and documentation-sync directives (WP-level Activity Logs, owned_files non-overlap, and the four-document C-005 update are all correctly structured).

## Unmapped Tasks

None. Every subtask T001-T024 maps to at least one FR/NFR/C requirement via its work package's requirement_refs.

## Metrics

- Total requirements: 30 (12 FR + 5 NFR + 7 C + 6 SC)
- Total tasks: 24 subtasks across 4 work packages
- Coverage %: 27/30 fully covered (90%), 2 partial (FR-011, SC-002), 1 rationale-only with no executable check (NFR-005)
- Ambiguity count: 1 (F004 - Findings ordering)
- Duplication count: 0
- Critical count: 1 (F001)

## Next Actions

1. Blocking: Resolve F001 before WP04 runs - record an explicit user ruling (quoted verbatim in the PR description) plus a .scratch/parity-runner/issues/ ticket, or add a tools/parity/cases-pending/ entry citing that ticket, per the charter's Exception Policy.
2. Add the two missing edge-case tests (multiple candidate locations; -v listing) to WP03's T017 step list (F002).
3. Add an explicit Report.KnownFieldsPresent population step to WP01's T002 (F003).
4. Reconcile data-model.md's and WP03 T014's conflicting statements about Report.Findings ordering (F004).
5. Mark NFR-005 as rationale-only (or add a trivial timing assertion) in tasks.md's coverage table (F005).
6. Optional: add a one-line note to WP01 explaining its trivial satisfaction of NFR-003 (F006).
