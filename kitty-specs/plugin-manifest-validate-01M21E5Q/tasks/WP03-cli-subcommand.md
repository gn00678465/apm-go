---
work_package_id: WP03
title: CLI subcommand -- locate, render, exit codes
dependencies:
- WP01
requirement_refs:
- C-002
- C-003
- C-005
- C-006
- FR-001
- FR-002
- FR-008
- FR-009
- FR-010
- FR-011
- FR-012
- NFR-001
- NFR-003
- NFR-005
planning_base_branch: feat/plugin-validate
merge_target_branch: feat/plugin-validate
branch_strategy: Planning artifacts for this mission were generated on feat/plugin-validate. During /spec-kitty.implement this WP may branch from a dependency-specific base, but completed changes must merge back into feat/plugin-validate unless the human explicitly redirects the landing branch.
subtasks:
- T013
- T014
- T015
- T016
- T017
phase: Phase 2 - CLI subcommand
history:
- at: '2026-09-09T02:01:55Z'
  actor: system
  action: Prompt generated via /spec-kitty.tasks
agent: claude
agent_profile: implementer-ivan
authoritative_surface: cmd/apm-go/
create_intent:
- cmd/apm-go/plugin_validate.go
- cmd/apm-go/plugin_validate_test.go
execution_mode: code_change
model: claude-sonnet-4-6
owned_files:
- cmd/apm-go/plugin_validate.go
- cmd/apm-go/plugin_validate_test.go
- cmd/apm-go/plugin.go
- internal/pack/bundle/producer.go
role: implementer
tags: []
task_type: implement
tracker_refs: []
---

# Work Package Prompt: WP03 -- CLI subcommand -- locate, render, exit codes

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

Wire WP01's `Validate(data []byte) Report` into a real `apm-go plugin validate [path] [--strict] [-v]` cobra subcommand: locate the manifest (upstream `find_plugin_json` order), cap and read it, render the result exactly as `contracts/cli-plugin-validate.md` specifies using PRODUCT.md's status-symbol vocabulary, and exit 0/1/2 per that contract's table. This WP owns `cmd/apm-go/plugin.go`'s existing deviation comment and must rewrite it as part of the same change (C-005).

Success criteria:
- `apm-go plugin validate` exists, discoverable via `apm-go plugin --help` (lists both `init` and `validate`) and documented via `apm-go plugin validate --help` (`--strict`, `-v`/`--verbose`).
- Every stdout line matches `contracts/cli-plugin-validate.md`'s exact shape and message text; no square brackets anywhere (PRODUCT.md, C-003).
- Exit codes match the contract's table exactly: 0 clean (or warnings without `--strict`), 1 on any error or `--strict`-upgraded warning or missing/unreadable manifest, 2 on usage errors (more than one positional arg, unknown flag, missing flag value).
- The target directory's file tree and every file's bytes are unchanged before and after any invocation (NFR-001) -- proven by a test, not asserted by inspection.
- `go test ./cmd/apm-go/ -run TestPluginValidate` is green; `quickstart.md`'s three manual scenarios reproduce exactly when run by hand.

## Context & Constraints

Read before writing any code:
- `kitty-specs/plugin-manifest-validate-01M21E5Q/contracts/cli-plugin-validate.md` -- this is the literal contract for every line this WP prints and every exit code it returns. Copy the message and layout strings exactly.
- `kitty-specs/plugin-manifest-validate-01M21E5Q/data-model.md`'s "State transitions" section -- the fixed sequence `locate -> stat (size cap) -> read -> Validate -> render -> exit`, and that ANY step failure ends with one ` x <message>` line plus exit 1, with no filesystem change.
- `kitty-specs/plugin-manifest-validate-01M21E5Q/research.md` R-02 (manifest location order, reusing `producer.go`'s candidate list conceptually), R-05 (paths are syntax-only, this WP does not check existence), R-07 (output shape mirrors `marketplace validate` but with PRODUCT.md's plain symbols, not the Oracle-parity `[+]`/`[*]` glyphs).
- `PRODUCT.md` "Terminal UI design" -- the exact symbol table (` + `, ` i `, ` ! `, ` x `, ` > `) and "Brackets are never printed, on any stream."
- `cmd/apm-go/marketplace.go` lines 693-850 (`marketplaceValidateCmd`) -- read this fully as the *shape* reference for how a validate-style command renders passed/warning/error lines and a Summary, and how it decides its exit code via `withSilentExitCode`. Do **not** copy its `ux.Gear`/`ux.Check` calls or its `[+]`/`[*]` glyphs -- those are `marketplace validate`'s own Oracle-parity exception (ticket 22), which this WP's Oracle-less command must NOT inherit (research.md R-07, C-003). Use `ux.Progress`/`ux.Success`/`ux.Info`/`ux.Warn`/`ux.Error` instead.
- `cmd/apm-go/exitcode.go` -- `withSilentExitCode` (exit 1, no extra `[x] ...` line since the Summary line already said everything) and `withUsageError` (exit 2, Click-style Usage/Try-help preamble on stderr). Use `withSilentExitCode(1, ...)` for the validation-failure path and `withUsageError(...)` for argument-count/flag errors.
- `cmd/apm-go/plugin.go` (existing file, one of this WP's `owned_files`) -- currently `pluginCmd()` has exactly one child (`init`) and a doc comment claiming that is deliberate (AC30). This WP adds a second child and MUST rewrite that comment (C-005) to record issue #13 and this mission (`plugin-manifest-validate-01M21E5Q`) instead of claiming a single-child invariant that will no longer be true.
- `internal/pack/bundle/producer.go` lines 489-497 (`pluginJSONCandidates`) -- the four-candidate probe order and its doc comment citing `utils/helpers.py:105-129`. research.md R-02 and plan.md IC-02 both decide to REUSE this list rather than restate it, and this WP implements that decision: export the variable as `PluginJSONCandidates` and have `plugin_validate.go` reference it. `cmd/apm-go` already imports `internal/pack/bundle` in ARCHITECTURE.md §1, so this adds no import edge and C-002 is satisfied. `producer.go` is listed in this WP's `owned_files` for that one-symbol export; no other WP owns it.
- `internal/rootfs` (skim `Rel`/`Lstat`-style helpers if useful) -- NFR-003 requires not following a symlink whose target lies outside the given `path`; decide at `Lstat` time, before any `Open`/`ReadFile` call.

## Branch Strategy

- **Strategy**: single-lane worktree per work package (allocated from `lanes.json` at `finalize-tasks`)
- **Planning base branch**: feat/plugin-validate
- **Merge target branch**: feat/plugin-validate

> These fields are populated automatically by `spec-kitty agent mission tasks`.
> Do NOT change them manually unless you are certain the branch topology has changed.

## Subtasks & Detailed Guidance

### Subtask T013 -- Manifest location logic

- **Purpose**: Resolve `path` (default `.`) to exactly one manifest file, or fail with the contracted message, before any validation runs.
- **Steps**:
  1. Export the existing candidate list instead of restating it: in `internal/pack/bundle/producer.go` rename `pluginJSONCandidates` to `PluginJSONCandidates` (one declaration at line 492 and one use at line 505; run GitNexus `impact` on the symbol first, then `rename`, per AGENTS.md), keeping its doc comment citing upstream `find_plugin_json` (`utils/helpers.py:105-129`). In new `cmd/apm-go/plugin_validate.go`, probe `bundle.PluginJSONCandidates` directly, so the order has exactly one definition and cannot drift from `pack`.
  2. `Lstat` the given `path`. If it is a regular file (not a directory), that IS the manifest -- skip candidate probing entirely (spec.md US1 AS8).
  3. If it is a directory, probe each candidate in order (`filepath.Join(path, candidate)`), `Lstat` each; the first that exists (regular file or a symlink -- see NFR-003 handling below) is selected. If none exist, fail with: ` x no plugin.json found in <dir> (looked in plugin.json, .github/plugin/plugin.json, .claude-plugin/plugin.json, .cursor-plugin/plugin.json)`, `withSilentExitCode(1, ...)`, printing no Results/Summary section at all.
  4. `<dir>` in that message is the directory argument as given by the user (or `.` if defaulted) -- not resolved to an absolute path.
  5. SUPERSEDED by the final contract and by review: symlinks are refused outright, with no exception for a target that stays inside the tree and none for a path the user named directly, and every refusal after successful location reports `could not read '<path>': <reason>` rather than the not-found message. Containment is enforced by opening through an `os.Root` handle anchored at the probed directory, not by a separate Lstat-then-check step.
  6. `Stat` the (non-symlink, or safely-resolved) selected file's size. If `> 5 * 1024 * 1024` bytes, do NOT read its contents -- fail immediately with the Structure message `file exceeds 5 MiB cap (<n> bytes)` (this is the primary enforcement point for the 5 MiB cap; `Validate`'s own defensive check in WP01 is a second line of defense only).
  7. Read the file's bytes (`os.ReadFile`) only after the size check passes.
- **Files**: `cmd/apm-go/plugin_validate.go` (new); `internal/pack/bundle/producer.go` (export-only rename, no behaviour change).
- **Parallel?**: No -- everything else in this WP depends on locate-then-read working correctly first.
- **Notes**: Two or more positional arguments must be rejected before any of this locate logic runs at all -- wire `cobra.MaximumNArgs(1)` and map its resulting parse error to `withUsageError` (see T015) rather than letting it surface as cobra's default message.

### Subtask T014 -- Result rendering

- **Purpose**: Turn a WP01 `Report` into the exact stdout shape `contracts/cli-plugin-validate.md` specifies.
- **Steps**:
  1. Print the progress line only once a manifest path is known, i.e. after T013 successfully selects a candidate. If no candidate is found, the "no plugin.json found" error is the only output, per the contract's "找不到 manifest" row: ` > Validating plugin '<relative manifest path>'...` via `ux.Progress`. `<relative manifest path>` is the selected file's path relative to the current working directory (or relative to the given `path` argument, matching quickstart.md's `bin/apm-go plugin validate demo` example, which would print something like `demo/plugin.json`).
  2. If `-v`/`--verbose` was given AND the file was successfully read and JSON-decoded far enough to know `Report.KnownFieldsPresent` (i.e. even if `StructureFailed` is later true, verbose listing happens after a successful read+decode, since `KnownFieldsPresent` is independent per data-model.md -- but if Structure itself fails at the JSON-decode step, there IS no known-fields list to show; use an empty/absent verbose section in that case), print each entry in `Report.KnownFieldsPresent` as its own ` i <field>` line, in file order, before the `Validation Results:` header.
  3. Print a blank line, then ` i Validation Results:` via `ux.Info`.
  4. For each `Check` in fixed order (Structure, Name, Fields, Paths, Unrecognized): if `Report.StructureFailed` is true, print ONLY the Structure check's findings and skip the other four entirely (no "passed" lines for them either). Otherwise: a check with zero findings prints one ` + <Check>: passed` line via `ux.Success`; a check with findings prints one line per finding, errors first then warnings within that check, via `ux.Error`/`ux.Warn` respectively, each formatted `<Check>: <Message>` with NO extra indent, per `contracts/cli-plugin-validate.md`'s worked example, which is authoritative over this prompt's earlier `marketplace validate` analogy; the symbol comes from `ux.Error`/`ux.Warn`, never `[x]`/`[!]`.
  5. Print a blank line, then ` i Summary: N passed, N warnings, N errors` via `ux.Info`, where `passed` counts checks with zero findings (0 when `StructureFailed`), `warnings`/`errors` count individual findings by level across all reported checks.
  6. No line is printed after Summary, ever (contract: "Exit codes... no extra line after Summary").
- **Files**: `cmd/apm-go/plugin_validate.go`.
- **Parallel?**: No -- depends on T013 producing bytes to validate, and on WP01's `Report` shape.
- **Notes**: Ordering rule (data-model.md `Report.Findings`, settled by analysis finding F004): checks render in fixed order (Structure, Name, Fields, Paths, Unrecognized); within one check, errors print before warnings, each group in file order. WP01 stores `Report.Findings` in exactly that order, so this WP renders `Findings` as-is without re-sorting; add one test where a single check carries both an error and a warning to lock the order.

### Subtask T015 -- --strict and exit codes

- **Purpose**: The three-way exit code decision, matching the contract's table exactly.
- **Steps**:
  1. Add `--strict` (bool flag, no shorthand) and `-v`/`--verbose` (bool, shorthand `v`) to the cobra command.
  2. After rendering (T014), compute the final exit condition: `errors > 0` -> exit 1. Else if `--strict` and `warnings > 0` -> exit 1. Else -> exit 0. Use `withSilentExitCode(1, fmt.Errorf(...))` for the exit-1 path (the Summary line already told the user everything; no extra `[x] ...` line, matching `marketplace validate`'s own convention read in context) -- return `nil` for exit 0.
  3. Missing-manifest and oversized-file failures (T013) also use `withSilentExitCode(1, ...)` -- same "no extra line" contract.
  4. `cobra.MaximumNArgs(1)`'s own parse error, and any unknown-flag error cobra raises, must map to `withUsageError(...)` (exit 2, stderr Usage/Try-help block) rather than being allowed to surface as cobra's raw default. Look at how `pluginInitCmd` (`cmd/apm-go/plugin.go`) or the shared `--format` flag error handling (`setInitFormatFlagErrorFunc`, referenced there) intercepts flag errors, and apply the same technique here for `validate`'s own flag set if a comparable interception point exists; if cobra's own `Args: cobra.MaximumNArgs(1)` error is not naturally interceptable the same way, wrap it explicitly inside `RunE`'s own arg-count check instead of relying on cobra's `Args` field, so you control the exact error and its `withUsageError` wrapping.
- **Files**: `cmd/apm-go/plugin_validate.go`.
- **Parallel?**: No -- depends on T013/T014's control flow structure being in place.
- **Notes**: Double-check against the contract's exit table one more time before considering this done: `errors == 0 且（無 --strict 或 warnings == 0）` -> 0; `errors > 0`，或 `--strict` 且 `warnings > 0`，或找不到 manifest，或無法讀取 -> 1；位置參數 > 1、未知 flag -> 2.

### Subtask T016 -- Wire into plugin.go + rewrite deviation comment

- **Purpose**: Make the new subcommand discoverable, and correct the now-false "exactly one child" claim (C-005).
- **Steps**:
  1. In `cmd/apm-go/plugin.go`'s `pluginCmd()`, add `cmd.AddCommand(pluginValidateCmd())` alongside the existing `cmd.AddCommand(pluginInitCmd())`.
  2. Rewrite `pluginCmd()`'s doc comment (currently: "Upstream has exactly one subcommand (commands/plugin/__init__.py:16-21), so this group intentionally has exactly one child (AC30).") to instead say something like: "Upstream (`commands/plugin/__init__.py:16-21`, pinned v0.29.0 and `main`) has exactly one subcommand (`init`); `validate` is an apm-go-only addition closing the second half of issue #13, with no Oracle counterpart -- see `pluginValidateCmd`'s own doc comment (this mission: `plugin-manifest-validate-01M21E5Q`) for its output-contract disposition." Do not simply delete the old comment's upstream citation -- keep it, since it is still true of `init`, and add the new fact alongside it.
  3. Add a doc comment on `pluginValidateCmd()` itself recording: this command has no Oracle equivalent; its output contract is fixed by `tools/gate/realexec.sh`'s new steps (WP04) rather than a `tools/parity` corpus case, per plan.md's Gate 2 disposition; cite `contracts/cli-plugin-validate.md` as the source of its exact wording.
- **Files**: `cmd/apm-go/plugin.go` (existing).
- **Parallel?**: Yes, relative to T013-T015 in principle (it's a small, independent edit) -- but do it only once `pluginValidateCmd()` exists as a real function to reference, so do it last or interleaved once T013-T015 compile.
- **Notes**: This is the ONE existing file this WP is allowed to modify outside its two new files -- keep the diff to exactly the `AddCommand` line and the two doc comments; do not restructure `pluginInitCmd` or anything else in this file.

### Subtask T017 -- plugin_validate_test.go end-to-end scenarios

- **Purpose**: Prove the whole command (locate + render + exit code) against every spec.md scenario, plus the read-only guarantee.
- **Steps**:
  1. Create `cmd/apm-go/plugin_validate_test.go` following this package's existing cobra end-to-end test conventions (see other `cmd/apm-go/*_test.go` files for the pattern: build the command tree, set args, capture `OutOrStdout`/`OutOrStderr`, run, assert stdout/stderr/exit behavior).
  2. Write one subtest per spec.md acceptance scenario (US1 AS1-AS8, US2 AS1-AS5, US3 AS1-AS5), using `t.TempDir()` to build each fixture manifest inline (AGENTS.md testing pattern -- no global fixtures). Name subtests after their scenario ID, matching WP01's `validate_test.go` naming convention for consistency (e.g. `t.Run("US1-AS1-clean-claude-scaffold", ...)`).
  3. Add a `--help` subtest: `apm-go plugin --help` output lists both `init` and `validate`; `apm-go plugin validate --help` output documents `--strict` and `-v, --verbose`.
  4. Add a read-only snapshot subtest (NFR-001): for at least the US3 adversarial inputs (invalid JSON, oversized file, non-UTF-8) AND a legitimate manifest, snapshot the target directory's file list and every file's bytes (e.g. via a recursive walk + per-file checksum, or `filepath.Walk` + `os.ReadFile` comparison) before invoking the command and again after, asserting byte-for-byte identity.
  5. Confirm the two-positional-argument case exits 2 with the Usage/Try-help block on stderr (T015's usage-error path).
  6. Add the spec.md Edge Case subtest `multiple-candidate-locations`: a directory containing both a valid `plugin.json` and an invalid (`{`) `.claude-plugin/plugin.json` validates the root `plugin.json` (first in the upstream order), exits 0, and the first stdout line names `plugin.json` as the selected relative path. WP01 deliberately does not cover this (it is location logic, T013).
  7. Add the spec.md Edge Case subtest `verbose-lists-known-fields`: with `-v`, a manifest `{"name":"x","version":"1.0.0","descripton":"d"}` prints exactly ` i name` and ` i version` (file order, unknown `descripton` omitted) before `Validation Results:`; without `-v` those lines are absent. WP01 deliberately does not cover the rendering half (FR-011, T014 step 2).
- **Files**: `cmd/apm-go/plugin_validate_test.go` (new).
- **Parallel?**: No -- this subtask's tests are this WP's primary RED-then-GREEN evidence; write the scenario table first against a not-yet-implemented `pluginValidateCmd` (or a minimal stub), observe failures, then implement T013-T016 to turn them green, recording both states in the Activity Log.
- **Notes**: For the oversized-file test, you do not need to commit a literal 5 MiB+ fixture to the repo -- create it at test time inside `t.TempDir()` (e.g. `os.WriteFile` with a byte slice sized just over the cap), so nothing large lands in git.

## Test Strategy

- `go test ./cmd/apm-go/ -run TestPluginValidate -v` -- every acceptance scenario as a named subtest, plus `--help` and read-only snapshot subtests (T017).
- Manually reproduce all three `quickstart.md` scenarios by hand once the build is green: `go build -o bin/apm-go ./cmd/apm-go`, then the three `bin/apm-go plugin validate ...` invocations quickstart.md lists, confirming stdout/exit code match by eye.
- `go vet ./cmd/apm-go/...` must be clean.

## Risks & Mitigations

- Exit-code/usage-error mapping is this WP's highest-risk area (see WP01's own risk note echoing this) -- re-verify every branch against the contract's table directly before considering T015 done, not from memory of similar commands.
- The verbose-listing-before-Structure-failure edge case (T014 step 2) is subtle -- re-read data-model.md's Report shape once more; `KnownFieldsPresent` and `StructureFailed` are independent fields, but if the JSON never successfully decoded at all, there is no meaningful known-fields list to produce, regardless of what the field's zero-value technically is.
- Symlink escape handling (NFR-003) is easy to get backwards (checking after reading instead of before) -- structure T013 so the escape check happens strictly before any `ReadFile` call, not as a post-hoc validation.

## Review Guidance

- Confirm every stdout example in this WP's tests matches `contracts/cli-plugin-validate.md`'s worked example and Messages table byte-for-byte (no paraphrased text).
- Confirm no `[` or `]` character appears in any status-line output this command produces (grep the test's expected strings and the actual implementation).
- Confirm `plugin.go`'s rewritten comment states facts (issue #13, this mission's Oracle-less disposition) rather than repeating the now-false "exactly one child" claim unmodified.

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
