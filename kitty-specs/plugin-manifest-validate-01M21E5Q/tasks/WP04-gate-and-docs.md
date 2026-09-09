---
work_package_id: WP04
title: Verification gate and documentation
dependencies:
- WP01
- WP02
- WP03
requirement_refs:
- C-001
- C-005
planning_base_branch: feat/plugin-validate
merge_target_branch: feat/plugin-validate
branch_strategy: Planning artifacts for this mission were generated on feat/plugin-validate. During /spec-kitty.implement this WP may branch from a dependency-specific base, but completed changes must merge back into feat/plugin-validate unless the human explicitly redirects the landing branch.
subtasks:
- T018
- T019
- T020
- T021
- T022
- T023
- T024
phase: Phase 3 - Gate and docs
history:
- at: '2026-09-09T02:01:55Z'
  actor: system
  action: Prompt generated via /spec-kitty.tasks
agent: claude
agent_profile: implementer-ivan
authoritative_surface: tools/gate/
create_intent: []
execution_mode: code_change
model: claude-sonnet-4-6
owned_files:
- tools/gate/realexec.sh
- tools/gate/mutants.txt
- PRODUCT.md
- ARCHITECTURE.md
- README.md
- README.zh-TW.md
role: implementer
tags: []
task_type: implement
tracker_refs: []
---

# Work Package Prompt: WP04 -- Verification gate and documentation

## ⚡ Do This First: Load Agent Profile

Use the `/ad-hoc-profile-load` skill to load the agent profile specified in the frontmatter (or any user-defined profile), and behave according to its guidance before parsing the rest of this prompt.

- **Profile**: `implementer-ivan`
- **Role**: `implementer`
- **Agent/tool**: `claude`

If no profile is specified, run `spec-kitty agent profile list` and select the best match for this work package's `task_type` and `authoritative_surface`.

---

## ⚠️ IMPORTANT: Review Feedback

**Read this first if you are implementing this task!**

- **Has review feedback?**: Check the `review_ref` field in the event log (via `spec-kitty agent tasks status` or the Activity Log below).
- **You must address all feedback** before your work is complete. Feedback items are your implementation TODO list.
- **Report progress**: As you address each feedback item, update the Activity Log explaining what you changed.

---

## Review Feedback

*[If this WP was returned from review, the reviewer feedback reference appears in the Activity Log below or in the status event log.]*

---

## Markdown Formatting

Wrap HTML/XML tags in backticks: `<div>`, `<script>`
Use language identifiers in code blocks: `python`, `bash`

---

## Objectives & Success Criteria

Close out the mission: give `plugin validate` the real-execution and mutation-testing evidence plan.md's Gate 2 disposition promises in place of a `tools/parity` corpus case (this command has no Oracle to diff against), and update every document C-005 names in the same change as the code. This WP runs LAST, after WP01-WP03 are code-complete, and ends with a full green `sh tools/gate.sh` run.

Success criteria:
- `tools/gate/realexec.sh` has 2 new happy-path steps and 4 new adversarial steps for `plugin validate`, each asserting an exit code, a stdout substring, and (adversarial steps) a `cmp`-verified read-only guarantee.
- `tools/gate/mutants.txt` has 2 new rows targeting `internal/pluginjson/validate.go`, each with an anchor string that is unique in that file.
- `PRODUCT.md`, `ARCHITECTURE.md`, `README.md`, `README.zh-TW.md` each mention `plugin validate` where their existing structure calls for a `plugin init` mention to be joined by it.
- `sh tools/gate.sh -scope plugin-manifest-validate` exits 0, with its evidence report under `.gate/plugin-manifest-validate/`.

## Context & Constraints

Read before editing anything:
- `kitty-specs/plugin-manifest-validate-01M21E5Q/plan.md`'s Charter Check row for "Gate 2" and its Complexity Tracking table -- the exact rationale for why this command gets NO `tools/parity` case and instead gets `realexec.sh` steps + a `plugin.go` deviation comment (already written by WP03's T016).
- `tools/gate/realexec.sh` (existing, one of this WP's `owned_files`) -- read the WHOLE file first. Note its existing structure: a sandboxed `HOME`/`APM_CONFIG_DIR`, a `step NAME EXPECTED_RC CMD...` helper, `must_exist`/`must_not_exist`/`must_grep` helpers, and an explicit "happy path" section followed by an "adversarial" section. Your new steps follow the SAME pattern and MUST run inside the same sandbox (`$SB`), never writing outside it.
- `tools/gate/mutants.txt` (existing, one of this WP's `owned_files`) -- TAB-separated `name<TAB>file<TAB>old<TAB>new`; `old` must occur EXACTLY once in `file` (`tools/gate.sh`'s own self-test enforces this, and the selftest layer runs before your mutants are ever exercised, so an ambiguous anchor fails loudly, not silently).
- `tools/gate.sh` header (lines 1-80 already read) -- `SCOPE_PATHS`/`GATE_SCOPE_PKGS` already include `internal/pluginjson` and every `cmd/apm-go/*.go` file relevant to this mission's surface; confirm (do not need to edit) that `cmd/apm-go/plugin.go` and `internal/pluginjson` are already covered by the existing `SCOPE_PATHS`/`GATE_SCOPE_PKGS` lines -- if `plugin_validate.go`/`plugin_validate_test.go` are new files under `cmd/apm-go/`, they are automatically covered by the existing `cmd/apm-go/...` glob-style scope package reference; you do not need to add a new explicit path for them.
- `PRODUCT.md`'s "Capabilities and Constraints" section (`Command surface (apm-go --help)`) and its Constraints bullet list -- this is where `plugin validate` needs to appear as a documented capability, and where the "no Oracle equivalent" deviation belongs as a constraint bullet (mirroring how other apm-go-only additions are documented in the Positioning section's list, e.g. `pack --claude-source-style url`).
- `ARCHITECTURE.md` §2's package table -- the `pluginjson` row's "Entry points" column currently lists `Scaffold`/`ScaffoldFiles`/`ScaffoldAgent`; this WP adds `Validate` to that same cell. `ARCHITECTURE.md` §3.5 ("`init` / `plugin init`") describes the `runInitCore` data flow this validate command deliberately does NOT join -- add one sentence there (not a new subsection) noting the distinction, per this WP's own task list wording.
- `README.md` / `README.zh-TW.md` -- find wherever `plugin init` is already listed in a command table or bullet list and add `plugin validate` alongside it, matching that document's existing formatting and (for the zh-TW file) its Traditional Chinese phrasing conventions.

## Branch Strategy

- **Strategy**: single-lane worktree per work package (allocated from `lanes.json` at `finalize-tasks`)
- **Planning base branch**: feat/plugin-validate
- **Merge target branch**: feat/plugin-validate

> These fields are populated automatically by `spec-kitty agent mission tasks`.
> Do NOT change them manually unless you are certain the branch topology has changed.

## Subtasks & Detailed Guidance

### Subtask T018 -- realexec.sh happy-path steps (2)

- **Purpose**: Prove `plugin validate` against apm-go's own two legitimate scaffolds with zero findings -- this WP's slice of SC-001.
- **Steps**:
  1. In `tools/gate/realexec.sh`'s existing "happy path: plugin init -> pack -> pack --archive" section (or immediately after it, still before the "adversarial" section), add: `step plugin-validate-claude 0 "$BIN" plugin validate demo` (reusing the already-created `demo` directory from the existing `plugin-init`/`pack` steps -- run this BEFORE any `pack`-driven mutation of `demo/`, i.e. right after `plugin-init`, or confirm `pack` does not alter `demo/plugin.json`'s bytes before deciding where in the sequence to insert this).
  2. `must_grep plugin-validate-claude "0 errors"` (and optionally `"0 warnings"` too, matching SC-001's "0 warnings、0 errors").
  3. Add a second happy-path step scaffolding the OTHER format: create a fresh directory (e.g. `mkdir demo-agent && cd demo-agent`, or reuse the existing sandbox convention) via `"$BIN" plugin init --format agent-plugin` (or whatever exact invocation produces the `$schema`/`extensions` scaffold per quickstart.md/spec.md AS2), then `step plugin-validate-agent 0 "$BIN" plugin validate demo-agent` (or the equivalent path) with the same `must_grep ... "0 errors"` check.
  4. Add a `cmp`-based read-only check immediately after each of these two steps, comparing the manifest's bytes before and after the `plugin validate` invocation (save a copy right before the step, per this file's existing `cp apm.yml "$SB/mk-apm.yml.before"` convention seen later in the adversarial section) -- confirm byte-identity, incrementing `steps`/`failures` the same way the existing final `cmp` check in this file does.
- **Files**: `tools/gate/realexec.sh` (existing).
- **Parallel?**: No -- needs the real `apm-go` binary (built by `tools/gate.sh` before this script runs) with WP01-WP03's `plugin validate` actually working.
- **Notes**: Keep these two steps physically adjacent to the existing `plugin-init`/`pack`/`pack-archive` happy-path block, not scattered -- this file's existing structure groups by user journey (plugin authoring, then marketplace authoring, then adversarial), and `plugin validate` belongs in the plugin-authoring journey.

### Subtask T019 -- realexec.sh adversarial steps (4)

- **Purpose**: Prove the four failure modes spec.md and the contract call out most sharply, each with its own read-only guarantee.
- **Steps**:
  1. **Invalid JSON**: create a directory with a deliberately broken `plugin.json` (e.g. `printf '{' > x/plugin.json`, matching quickstart.md's own "broken" example), then `step plugin-validate-invalid-json 1 "$BIN" plugin validate x` and `must_grep plugin-validate-invalid-json "invalid JSON"`.
  2. **Path traversal field**: a `plugin.json` with a path-typed field value escaping the plugin root (e.g. `{"name":"x","commands":"../outside/x.md"}`, per spec.md US1 AS6) -- `step plugin-validate-traversal 1 "$BIN" plugin validate y` and `must_grep plugin-validate-traversal "'\\.\\.'"` (grep for the literal message substring `must not contain '..'`, escaping as needed for the shell).
  3. **Typo'd field with --strict**: a `plugin.json` with `{"name":"x","descripton":"d"}` (per spec.md US2 AS1-AS2) -- `step plugin-validate-strict-typo 1 "$BIN" plugin validate z --strict` and `must_grep plugin-validate-strict-typo "descripton"`.
  4. **Missing manifest**: an empty directory with none of the four candidate files -- `step plugin-validate-missing 1 "$BIN" plugin validate empty` and `must_grep plugin-validate-missing "no plugin.json found"`.
  5. For EACH of the four steps above, snapshot the relevant file/directory's state (a `cmp`-style before/after byte comparison of the manifest file where one exists, or a `must_not_exist`/directory-listing comparison for the missing-manifest case) immediately around the `step` call, following this file's own existing `cmp -s apm.yml "$SB/mk-apm.yml.before"` pattern (near the end of the adversarial section) rather than inventing a new verification style.
- **Files**: `tools/gate/realexec.sh` (existing).
- **Parallel?**: No -- same script as T018; sequence these steps together for readability.
- **Notes**: Place these four inside the file's existing "adversarial: hostile inputs must be refused and must not escape" section, alongside the existing `marketplace add`/`pack`/`plugin init` adversarial steps, not as a separate new section.

### Subtask T020 -- mutants.txt +2

- **Purpose**: Give the mutation-testing layer two concrete, plausible bugs in the new validator to catch.
- **Steps**:
  1. Pick ONE Fields-type-check branch inside `internal/pluginjson/validate.go` (from WP01's T004) whose exact source line is unique in that file, and add a mutant row inverting it -- e.g. if the real code has a line like `if !isArrayOfStrings(raw) {` guarding an array-of-string mismatch, the mutant's `new` column would flip the condition (`if isArrayOfStrings(raw) {`) so the check fires backwards. Confirm the EXACT line text you pick from the actual shipped WP01 code (not a guess from this prompt) before writing the row -- open `internal/pluginjson/validate.go` and copy the real line verbatim into the `old` column.
  2. Pick the `--strict` upgrade-to-failure code path (from WP03's T015, inside `cmd/apm-go/plugin_validate.go`) and add a second mutant row that makes `--strict` fail to upgrade a warning-only result to exit 1 -- e.g. if the real code has `if strict && warnings > 0 {`, the mutant's `new` column removes the strict gate entirely or negates it, again copying the exact real line first.
  3. Format each new row as `name<TAB>file<TAB>old<TAB>new`, appended to `tools/gate/mutants.txt`, matching the existing rows' style (short kebab-case name, real repo-relative file path, tab-separated).
  4. Before finalizing, run (or ask the gate's mutation layer to run) `tools/gate/gatetool replace -file <file> -old <old> -new <new>` mentally/manually to confirm the `old` string occurs EXACTLY once in the target file -- a non-unique anchor is rejected by the tool at gate time (`tools/gate.sh`'s own selftest layer proves this rejection behavior generically); do not discover this the first time during T024's full gate run if you can catch it now by grepping the file yourself.
- **Files**: `tools/gate/mutants.txt` (existing).
- **Parallel?**: Yes, relative to T018/T019 (different file) -- but both mutant anchors depend on WP01 and WP03's actual shipped code, so this subtask cannot start meaningfully until those WPs are done.
- **Notes**: A mutant that changes behavior in a way the existing test suite (WP01's `validate_test.go`, WP03's `plugin_validate_test.go`) does NOT already catch is a gap in those tests, not a valid reason to weaken the mutant -- if T024's gate run shows a mutant survives, go back and strengthen the relevant WP's tests rather than picking an easier mutant.

### Subtask T021 [P] -- PRODUCT.md update

- **Purpose**: C-005's documentation commitment for the product-facts document.
- **Steps**:
  1. In "Capabilities and Constraints" > "Command surface (`apm-go --help`)", find the `plugin` entry (it should already be part of the top-level command list, e.g. "...`pack`, `plugin`, `search`...") -- the subcommand-level detail (`init`, `validate`) does not need to be spelled out at that top-level list's granularity, since other multi-subcommand entries there (e.g. `marketplace`) are already shown with their subcommands in parentheses (`marketplace (add/list/browse/update/remove/validate/init/package/audit)`); add the same style for `plugin`: `plugin (init/validate)`.
  2. In the Constraints bullet list immediately below, add one new bullet documenting the deviation: `plugin validate` has no upstream Oracle equivalent (apm-go-only, closing the second half of issue #13); its output contract is fixed by `tools/gate/realexec.sh`, not a `tools/parity` corpus case.
- **Files**: `PRODUCT.md` (existing).
- **Parallel?**: Yes, relative to T022/T023 and to T018-T020.
- **Notes**: Do not restate the Terminal UI design section's symbol table here -- this WP's command already follows it exactly (WP03's own responsibility); this document only needs the command-surface and constraints-list additions.

### Subtask T022 [P] -- ARCHITECTURE.md update

- **Purpose**: C-005's documentation commitment for the architecture document.
- **Steps**:
  1. In §2's package table, find the `pluginjson` row's "Entry points" cell (currently something like `` `Scaffold` `pluginjson.go:28`; `ScaffoldFiles` `:34`; `ScaffoldAgent` `:55` ``) and append `; `Validate` `validate.go:<line>`` (use the real line number from WP01's shipped `validate.go`, not a placeholder).
  2. In §3.5 ("`init` / `plugin init`"), append one sentence after its existing numbered list (do not renumber or restructure the existing list) noting that `plugin validate` is a separate, read-only sibling command living in the same `pluginjson` package but joining none of `runInitCore`'s data flow -- e.g.: "`plugin validate` (`cmd/apm-go/plugin_validate.go`) is a read-only sibling command in the same package that does not join `runInitCore`'s flow; it has no Oracle equivalent and its output contract is fixed by `tools/gate/realexec.sh` (`ARCHITECTURE.md` §5) rather than the parity gate."
- **Files**: `ARCHITECTURE.md` (existing).
- **Parallel?**: Yes, relative to T021/T023 and to T018-T020.
- **Notes**: Keep this to the two additions named above -- do not add a new §3.x data-flow subsection for this command; the plan.md Structure Decision explicitly chose not to give it one (it is a single-step, single-package operation with no multi-stage flow worth diagramming).

### Subtask T023 [P] -- README.md / README.zh-TW.md update

- **Purpose**: C-005's documentation commitment for the two user-facing README files.
- **Steps**:
  1. Find wherever `README.md` lists `plugin init` (a command table, a bullet list, or a usage example) and add a `plugin validate` row/bullet alongside it, matching that section's existing format and level of detail (a one-line description is enough, matching the style of neighboring entries).
  2. Do the same in `README.zh-TW.md`, writing the description in Traditional Chinese following ASD-STE100 conventions per the global writing rules (short, plain sentences; no mannered prose), matching that file's existing phrasing style for `plugin init`'s own entry.
- **Files**: `README.md`, `README.zh-TW.md` (existing).
- **Parallel?**: Yes, relative to T021/T022 and to T018-T020.
- **Notes**: If either README has no existing per-subcommand table (only a top-level command list), add `plugin validate` at the same granularity the existing list already uses -- do not introduce a new level of detail neither file currently has for other commands.

### Subtask T024 -- Run tools/gate.sh, attach evidence

- **Purpose**: The mission's actual completion gate -- every other WP's evidence claims are only as good as this run confirming them together.
- **Steps**:
  1. Ensure WP01, WP02, and WP03 are all merged/present in the working tree (this WP depends on all three).
  2. Run: `sh tools/gate.sh -scope plugin-manifest-validate`
  3. If any layer fails (build, tests, vet, lint-format, staticcheck, suite-health, property, supply-chain, real-execution, mutation, changed-units, changed-line-coverage, source-state), do NOT weaken the gate or the failing layer's check -- return to the OWNING work package's files and fix the underlying issue (e.g. a surviving mutant means WP01 or WP03's tests need strengthening, not that T020's mutant should be swapped for an easier one).
  4. Re-run until `sh tools/gate.sh -scope plugin-manifest-validate` exits 0.
  5. Record the final command, its exit code, and the evidence directory path (`.gate/plugin-manifest-validate/`) in this file's Activity Log. This is the mission's SC-005 evidence (`go test ./...` all-green + parity gate's existing 96/0 unchanged) combined with this WP's own realexec/mutation evidence.
- **Files**: none (verification-only subtask; no new files -- `.gate/` artifacts are gitignored per `tools/gate.sh`'s own header comment).
- **Parallel?**: No -- this is the WP's and the mission's final step, strictly after T018-T023.
- **Notes**: `tools/gate.sh`'s own `GATE_EXPECTED_LAYERS` list runs `source-state-before`/`source-state-after` checks confirming the working tree is byte-identical before and after the gate run except for `.gate/` itself -- if this fails, something in T018-T023's edits (or an earlier WP's) left an untracked or modified file outside the expected set; resolve that before re-running, don't add it to an untracked-ok allowlist casually.

## Test Strategy

- `sh tools/gate.sh -scope plugin-manifest-validate` is this WP's whole test strategy -- it subsumes `go build`, `go test ./...`, `go vet`, staticcheck, mutation testing, changed-line coverage, and the new realexec steps, all in one command.
- Manually spot-check the four new adversarial `realexec.sh` steps' `must_grep` substrings against the actual binary's output once, outside the full gate run, if iterating quickly (`cd .gate/<scope>/sandbox/work && ../../../bin/apm-go plugin validate ...`) before committing to a full gate run each time.

## Risks & Mitigations

- Mutant anchor drift: if WP01/WP03 land with slightly different exact source lines than this prompt's illustrative examples, T020 MUST use the real lines, not the illustrative ones -- copy-paste directly from the shipped files.
- realexec step ordering: inserting the two happy-path steps in the wrong place relative to `pack`'s own mutation of the `demo/` directory could make the "before/after byte-identical" check compare the WRONG state -- validate against `demo/plugin.json` before any `pack` step ever runs, or re-copy a fresh scaffold if the ordering is awkward.
- Documentation drift: a reviewer checking C-005 will diff all four documents against this WP's actual code changes -- do not describe capabilities or constraints that WP01-WP03 did not actually ship (e.g. do not claim symlink-following behavior if WP03 implemented the opposite).

## Review Guidance

- Confirm `sh tools/gate.sh -scope plugin-manifest-validate` was actually run to a green exit, with the evidence path pasted into the Activity Log, not merely claimed.
- Confirm the two new mutants are each killed (not surviving) by the current test suite -- a surviving mutant blocks this WP, not the reviewer's job to notice separately.
- Confirm all four document updates (T021-T023) are present and match the described additions -- spot check `PRODUCT.md`'s new constraints bullet and `ARCHITECTURE.md`'s new §3.5 sentence directly.

## Activity Log

> **CRITICAL**: Activity log entries MUST be in chronological order (oldest first, newest last).

### How to Add Activity Log Entries

**When adding an entry**:

1. Scroll to the bottom of this Activity Log section
2. **APPEND the new entry at the END** (do NOT prepend or insert in middle)
3. Use exact format: `- YYYY-MM-DDTHH:MM:SSZ – agent_id – <action>`
4. Timestamp MUST be current time in UTC (check with `date -u "+%Y-%m-%dT%H:%M:%SZ"`)
5. Agent ID should identify who made the change (claude-sonnet-4-5, codex, etc.)

**Format**:

```
- YYYY-MM-DDTHH:MM:SSZ – <agent_id> – <brief action description>
```

**Initial entry**:

- 2026-09-09T02:01:55Z – system – Prompt created.

---

### Updating Status

Status is managed via `status.events.jsonl`. Use `spec-kitty agent tasks move-task <WPID> --to <status>` to change WP status.

### Optional Phase Subdirectories

For large features, organize prompts under `tasks/` to keep bundles grouped while maintaining lexical ordering.
