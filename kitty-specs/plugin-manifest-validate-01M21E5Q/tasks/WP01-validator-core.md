---
work_package_id: WP01
title: Validator core -- rule table, five checks, Report model
dependencies: []
requirement_refs:
- C-002
- C-004
- C-007
- FR-003
- FR-004
- FR-005
- FR-006
- FR-007
- NFR-002
- NFR-003
planning_base_branch: feat/plugin-validate
merge_target_branch: feat/plugin-validate
branch_strategy: Planning artifacts for this mission were generated on feat/plugin-validate. During /spec-kitty.implement this WP may branch from a dependency-specific base, but completed changes must merge back into feat/plugin-validate unless the human explicitly redirects the landing branch.
subtasks:
- T001
- T002
- T003
- T004
- T005
- T006
- T007
phase: Phase 1 - Validator core
history:
- at: '2026-09-09T02:01:55Z'
  actor: system
  action: Prompt generated via /spec-kitty.tasks
agent_profile: implementer-ivan
authoritative_surface: internal/pluginjson/
create_intent:
- internal/pluginjson/validate.go
- internal/pluginjson/validate_test.go
execution_mode: code_change
model: claude-sonnet-4-6
owned_files:
- internal/pluginjson/validate.go
- internal/pluginjson/validate_test.go
role: implementer
tags: []
task_type: implement
tracker_refs: []
---

# Work Package Prompt: WP01 -- Validator core -- rule table, five checks, Report model

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

Build the pure validation core for `apm-go plugin validate`: a function `Validate(data []byte) Report` in a new `internal/pluginjson/validate.go`, backed by one hand-written rule table, implementing all five check categories the spec requires (Structure, Name, Fields, Paths, Unrecognized). No CLI, no filesystem I/O, no flags -- this WP is finished when `go test ./internal/pluginjson/ -run TestValidate` is green with one subtest per every acceptance scenario and edge case in `spec.md` that IC-01 covers, and the Charter Check's TDD requirement (tests committed first, observed RED, then implementation) is honored and recorded in this file's Activity Log.

Success criteria for this WP:
- `Validate([]byte) Report` exists in `internal/pluginjson/validate.go`, imports stdlib only (`encoding/json`, `unicode`, `unicode/utf8`, plus whatever else stdlib offers -- no new module dependency, C-002).
- Every rule table entry carries a `Source` tag (`schema` / `docs` / `apm-go`) per `data-model.md`'s Rule table, so WP02's schema-sync test can mechanically check it later.
- `internal/pluginjson/validate_test.go` has one table-driven subtest per spec.md acceptance scenario ID and edge case (SC-002).
- `go test ./internal/pluginjson/...` is green; `go vet ./internal/pluginjson/...` is clean.

## Context & Constraints

Read before writing any code:
- `kitty-specs/plugin-manifest-validate-01M21E5Q/spec.md` -- User Scenarios (US1 AS1-8, US2 AS1-5, US3 AS1-5) and Edge Cases are this WP's test list, verbatim. Do not paraphrase the message text; the exact strings live in `contracts/cli-plugin-validate.md`'s Messages table (see below) and must match exactly, because WP03's CLI layer renders `Finding.Message` unchanged.
- `kitty-specs/plugin-manifest-validate-01M21E5Q/data-model.md` -- the exact shape of `Manifest`, `Rule`, `Finding`, `Report`, including the schema-sync invariants WP02 will assert against your rule table.
- `kitty-specs/plugin-manifest-validate-01M21E5Q/contracts/cli-plugin-validate.md` -- the Messages table is the literal text every `Finding.Message` must produce. Copy these strings exactly; do not reword.
- `kitty-specs/plugin-manifest-validate-01M21E5Q/research.md` -- R-03 (known-fields = schema ∪ docs ∪ apm-go `extensions`), R-05 (paths are syntax-only), R-06 (5 MiB cap, non-UTF-8, duplicate keys, deep nesting).
- `AGENTS.md`, `PRODUCT.md`, `ARCHITECTURE.md` §1-§2 -- `internal/pluginjson` currently owns only plugin.json *scaffolding* (`pluginjson.go`); this WP adds *validation* to the same package (plan.md's Structure Decision: "the name and responsibility of `internal/pluginjson`... exactly cover validation").
- `internal/pluginjson/pluginjson.go` (read-only reference) -- existing package conventions (doc comments citing oracle file:line, `bundle.JSONField`/`bundle.ObjectValue` helpers). Your new file does NOT need `bundle` at runtime (stdlib only) -- `pluginjson.go`'s existing `bundle` import is for scaffolding, unrelated to validation.
- `internal/pack/bundle/schema_sync_test.go` (read-only reference for WP02's benefit, but skim it now) -- the anti-drift pattern this mission's WP02 will mirror; knowing its shape now helps you name your rule table fields (`Source`, `IsPath`) so WP02 does not need to guess.

The full known-field set (research.md R-03), with each field's `Source`:
- From schema (`schema`): `$schema`, `agents`, `author`, `channels`, `commands`, `dependencies`, `description`, `homepage`, `hooks`, `keywords`, `license`, `lspServers`, `mcpServers`, `monitors`, `name`, `outputStyles`, `repository`, `settings`, `skills`, `themes`, `userConfig`, `version`.
- From docs only (`docs`): `displayName`, `defaultEnabled`, `metadata`, `experimental`, `workflows`.
- From apm-go's own scaffold (`apm-go`): `extensions` (written by `ScaffoldAgent` in `internal/pluginjson/pluginjson.go`; must never warn).

Path-typed fields (`IsPath = true`, per data-model.md): `skills`, `commands`, `agents`, `workflows`, `hooks`, `mcpServers`, `outputStyles`, `lspServers` (string or string-array forms), plus `experimental.themes` and `experimental.monitors` when given as strings or string arrays.

## Branch Strategy

- **Strategy**: single-lane worktree per work package (allocated from `lanes.json` at `finalize-tasks`)
- **Planning base branch**: feat/plugin-validate
- **Merge target branch**: feat/plugin-validate

> These fields are populated automatically by `spec-kitty agent mission tasks`.
> Do NOT change them manually unless you are certain the branch topology has changed.

## Subtasks & Detailed Guidance

### Subtask T001 [P] -- Define Finding/Report/Rule types and rule table skeleton

- **Purpose**: Establish the shared vocabulary every other subtask writes against, and the anchor WP02's schema-sync test will read.
- **Steps**:
  1. In `internal/pluginjson/validate.go`, define `type Check int` (or string) with values `Structure`, `Name`, `Fields`, `Paths`, `Unrecognized` in that fixed order (data-model.md: "依 Check 順序").
  2. Define `type Level int` with `LevelError`, `LevelWarning`.
  3. Define `type Finding struct { Check Check; Level Level; Message string }`.
  4. Define `type Report struct { Findings []Finding; KnownFieldsPresent []string; StructureFailed bool }`.
  5. Define `type ruleKind int` with the seven kinds from data-model.md: string, object, bool, array-of-string, string-or-array, string-array-or-object, dependency-list.
  6. Define `type ruleSource int` (or string) with `sourceSchema`, `sourceDocs`, `sourceApmGo`.
  7. Define `type rule struct { Name string; Kind ruleKind; Mismatch Level; IsPath bool; Source ruleSource }` and build the static `[]rule` table using the full field list above (top-level fields only here; `experimental.*` sub-fields are handled inside T004/T006's logic, not as top-level rule-table rows, since they nest one level down).
  8. Stub `func Validate(data []byte) Report { return Report{} }` so the package compiles; later subtasks fill in the body.
- **Files**: `internal/pluginjson/validate.go` (new).
- **Parallel?**: Yes -- this is the first subtask; nothing else in this WP can start until it exists, but it has no dependency itself.
- **Notes**: Only `metadata` and `experimental` get `Mismatch: LevelWarning`; every other rule's `Mismatch` is `LevelError` (FR-005). Double-check the rule table against research.md R-03's field list before moving on -- WP02's sync test will fail loudly if you drop or misname one, but catching it now saves a round trip.

### Subtask T002 -- Structure check

- **Purpose**: One check that must run before all others and, on failure, suppresses every other check (data-model.md: `StructureFailed`).
- **Steps**:
  1. `Validate` first checks `len(data) > 5*1024*1024` -- wait, per data-model.md the 5 MiB cap is enforced by the CLI layer (WP03) via `Stat` before the file is even read; `Validate` itself receives already-capped bytes. Still add a defensive length check here as the last line of defense (`file exceeds 5 MiB cap (<n> bytes)`) in case a caller passes oversized bytes directly (this keeps `Validate` safe to fuzz standalone in WP02, per NFR-002).
  2. `utf8.Valid(data)` -- if false, one Structure error: `invalid UTF-8`.
  3. Decode with `json.Unmarshal(data, &map[string]json.RawMessage{})` inside a `defer recover()`-free path -- `encoding/json` does not panic on deep nesting, it returns an error; if `Unmarshal` errors, one Structure error: `invalid JSON: <decoder message>` (use the underlying error's `.Error()` text verbatim after the prefix).
  4. If decode succeeds but the top-level JSON value was not an object (detect via a preliminary `json.RawMessage` peek, or by checking the raw bytes' first non-whitespace byte is `{`), one Structure error: `top-level value must be an object`.
  5. Duplicate-key detection: run a second pass with `json.NewDecoder(bytes.NewReader(data))` reading raw tokens (`Token()` in a loop) to find the top-level object's key tokens in file order; if the same key string appears twice at the top level, emit one Structure **warning** (not error) per duplicate key: `duplicate key '<k>' (last value wins)`. This does NOT set `StructureFailed`.
  6. If any Structure **error** (not warning) was found, set `Report.StructureFailed = true` and return immediately -- no other check runs (data-model.md: "为 true 時只有 Structure 的 findings").
- **Files**: `internal/pluginjson/validate.go`.
- **Parallel?**: No -- gates every other check; do this before T003-T006's logic is wired into `Validate`'s body (their functions can be written in parallel, but the call order inside `Validate` must put Structure first).
- **Notes**: Edge cases from spec.md US3: `{` -> invalid JSON; `[]` -> valid JSON, not an object; empty file -> invalid JSON (`unexpected end of JSON input` or equivalent decoder message); 0xFF byte -> invalid UTF-8; `[[[[...` (1 MiB) -> either invalid JSON (unbalanced) or, if balanced and deep, `encoding/json`'s internal depth error -- either way, one Structure error, never a panic; 6 MiB file -> size cap error (defensive check from step 1, though the primary enforcement point is WP03's `Stat`).

### Subtask T003 -- Name check

- **Purpose**: `name` is the one schema-required field (`required: ["name"]`) and carries the most edge cases (bidi/control characters are a real supply-chain concern for typosquatting-style plugin names).
- **Steps**:
  1. Look up `name` in the decoded map. Missing -> error `missing required field 'name'`.
  2. If present but not a JSON string -> error `'name' must be a string`.
  3. If it is a string: empty (`""`) -> error `'name' must not be empty`.
  4. Contains a space or any Unicode whitespace (`unicode.IsSpace`) -> error `'name' must not contain spaces`.
  5. Contains a control character (`unicode.IsControl`) -> error `'name' must not contain control characters`.
  6. Contains any bidi formatting character (exactly U+200E, U+200F, U+202A-U+202E, U+2066-U+2069) -> error `'name' must not contain bidirectional formatting characters`.
  7. If none of the above errors fired, and the string is not kebab-case (lowercase ASCII letters, digits, and single `-` separators only -- no leading/trailing `-`, no `_`, no uppercase) -> warning `'name' is not kebab-case`.
  8. Each of steps 2-6 is mutually exclusive with the others triggering (a name can only be one invalid shape at a time in the test fixtures spec.md describes) but do not assume that in code -- check them in the listed order and stop at the first error found for `name` (do not emit both "must not contain spaces" and "not kebab-case" for the same string; kebab-case is checked only when no error fired).
- **Files**: `internal/pluginjson/validate.go`.
- **Parallel?**: No -- runs after Structure, order-independent relative to T004-T006's checks within `Validate`'s body, but write it before T007's tests need it.
- **Notes**: Edge cases from spec.md: empty string, contains space, contains control character, contains bidi character (U+200E/U+200F/U+202A-U+202E/U+2066-U+2069) -- each is its own Name error, one per test. `My_Plugin` (non-kebab, e.g. underscore or mixed case) is a warning, not an error.

### Subtask T004 -- Fields type check

- **Purpose**: The bulk of the rule table's enforcement -- every recognized field checked against its documented `Kind`.
- **Steps**:
  1. For every top-level key present in the decoded map that matches a rule table entry (by `Name`), type-check its `json.RawMessage` against that rule's `Kind`:
     - `string`: must decode as a JSON string.
     - `object`: must decode as a JSON object (`{...}`), not array/string/number/bool.
     - `bool`: must decode as JSON `true`/`false`.
     - `array-of-string`: must decode as a JSON array where every element is a string.
     - `string-or-array`: JSON string, OR array-of-string.
     - `string-array-or-object`: JSON string, array-of-string, OR object.
     - `dependency-list`: JSON array; each element must be an object containing a `name` field that is itself a string (see step 4 below for the exact sub-errors).
  2. On a type mismatch, emit one Fields finding at `rule.Mismatch` level (`LevelError` for everything except `metadata`/`experimental`, which are `LevelWarning`) with the message from `contracts/cli-plugin-validate.md`'s Fields row matching the expected kind (e.g. `'keywords' must be an array of strings`, `'metadata' should be an object; Claude Code ignores other values`).
  3. `author` (Kind `object`) additionally requires: if present and an object, every one of `author.name`/`author.email`/`author.url` that is itself present must be a string, else error `'author.<k>' must be a string` (only check keys that exist; `author` has no fixed required sub-fields as far as validate.go is concerned -- this mission does not require presence-checking inside `author`).
  4. `dependencies` (Kind `dependency-list`) additionally requires, per element `i`:
     - Element is not an object (e.g. a bare number or string) -> error `'dependencies[<i>]' must be a string or object`.
     - Element is an object missing a `name` key -> error `'dependencies[<i>]' is missing 'name'`.
     - Element has `name` present but `version` present-and-not-a-string -> error `'dependencies[<i>].version' must be a string`.
  5. `experimental` (Kind `object`, `Mismatch: LevelWarning` when not an object at all): when it IS an object, walk its own keys with a small inline sub-rule set for `themes` and `monitors` (Kind `string-or-array`, `Source: docs`, `IsPath: true`) -- a type mismatch inside `experimental.themes`/`experimental.monitors` is a Fields **error** (only the top-level `experimental` key itself downgrades to warning when it's the wrong shape entirely), message `'experimental.<k>' must be a string or array`. Any OTHER key inside `experimental` that is not `themes`/`monitors` is NOT a Fields concern -- it belongs to T006's Unrecognized check.
- **Files**: `internal/pluginjson/validate.go`.
- **Parallel?**: No -- depends on T001's rule table and runs after T002/T003 in `Validate`'s body; independent of T005/T006's *code* but all three write into the same file's `Validate` function, so sequence your edits to avoid merge friction (this is a single-developer WP, not multiple parallel agents, so this is just an ordering note for yourself).
- **Notes**: spec.md US2 AS4-AS5 are this subtask's key edge cases: `{"name":"x","metadata":"a","experimental":[]}` -> 2 warnings (both non-object); `{"name":"x","experimental":{"themes":1,"foo":1}}` -> `experimental.themes` type error (number instead of string-or-array) AND `experimental.foo` unrecognized warning (T006's job, not this one -- make sure you do not ALSO try to type-check `foo` here).

### Subtask T005 -- Paths syntax check

- **Purpose**: Catch path-traversal-shaped values without ever touching the filesystem (NFR-003, R-05).
- **Steps**:
  1. For every rule with `IsPath: true` that is present and passed T004's type check (only check syntax on values that are already confirmed to be strings or string arrays -- a type-mismatched value has already been reported by T004 and should not also get a Paths finding), extract each string value: for a bare string field, one value; for an array field, each element (name it `'<field>[<i>]'` in error messages, 0-indexed).
  2. Each value must start with `./` -- else error `'<field>' must start with './'` (or the array-indexed variant).
  3. Each value must not be an absolute path (starts with `/` on POSIX or a drive letter/UNC prefix on Windows -- but since these are manifest *string values*, not OS paths, treat any value starting with `/` OR matching `^[A-Za-z]:[\\/]` as absolute) -- else error `'<field>' must not be an absolute path`.
  4. Each value must not contain a `..` path segment (split on `/` and `\`, check for a literal `..` component) -- else error `'<field>' must not contain '..'`.
  5. Check all three conditions independently per value (a single value could theoretically trigger more than one, but only report the first that fires, matching the message table's singular phrasing per field).
- **Files**: `internal/pluginjson/validate.go`.
- **Parallel?**: No -- depends on T001 (rule table's `IsPath`) and T004 (only checks already-type-valid values).
- **Notes**: spec.md US1 AS5 (`{"name":"x","skills":"skills/"}` -> missing `./` prefix) and AS6 (`{"name":"x","commands":"../outside/x.md"}` or an absolute path -> traversal/absolute error) are the primary tests. `experimental.themes`/`experimental.monitors` follow the same rule when given as strings or string arrays (T004 already type-checked them).

### Subtask T006 -- Unrecognized check

- **Purpose**: Warn on typos and misplaced fields without blocking load, matching Claude Code's own "unknown fields are ignored, not fatal" behavior.
- **Steps**:
  1. For every top-level key in the decoded map that has NO matching entry in the rule table, emit a Fields... no -- an **Unrecognized** warning: `unrecognized field '<k>'`.
  2. Compute Damerau-Levenshtein distance from `<k>` to every known top-level rule name; if the minimum distance is ≤ 2, append ` (did you mean '<known>'?)` to the message (pick the closest known name; break ties by whichever known name in the rule table has the lower distance, or the first alphabetically if truly tied).
  3. For every key inside `experimental` (when `experimental` is present and an object) that is not `themes` or `monitors`, emit `unrecognized field 'experimental.<k>'` with the same suggestion logic against a small known-set of `{themes, monitors}` (a distance-2 match here is unlikely but implement the same helper, not a special case).
  4. Top-level `themes` and `monitors` (i.e. NOT nested under `experimental`) are a special case: they ARE recognized top-level fields (per the official schema), so they do NOT get an "unrecognized" warning -- instead emit a *different* Unrecognized-category warning: `'themes' belongs under 'experimental'` (respectively `'monitors' belongs under 'experimental'`).
  5. Implement Damerau-Levenshtein distance yourself (stdlib only, no new dependency) -- a standard dynamic-programming table over rune slices, allowing insertion/deletion/substitution/adjacent-transposition, is sufficient; keep it a private helper function in this same file.
- **Files**: `internal/pluginjson/validate.go`.
- **Parallel?**: No -- depends on T001's rule table; independent of T002-T005's logic but shares the same `Validate` function body.
- **Notes**: spec.md US2 AS1 (`descripton` -> did-you-mean `description`) and AS3 (`publisher` -> no suggestion, no known field within distance 2) are the two canonical tests -- verify your distance function actually returns > 2 for `publisher` against every known field before wiring this up, or AS3 will falsely get a suggestion.

### Subtask T007 -- validate_test.go table-driven scenarios

- **Purpose**: SC-002's literal requirement: one automated test per spec.md acceptance scenario and edge case. This is also this WP's RED-first evidence -- write this subtask's test table BEFORE T002-T006's bodies are filled in (only T001's skeleton needs to exist), run it, confirm it is RED (fails, ideally by returning empty findings when errors were expected), then implement T002-T006 to turn it GREEN.
- **Steps**:
  1. Create `internal/pluginjson/validate_test.go` with a table of `{name string; input string; wantChecks map[Check][]Finding}` or an equivalent structure that lets you assert exact `Finding` values (Check/Level/Message) per scenario, plus `StructureFailed` and `KnownFieldsPresent` where relevant.
  2. Name every subtest after its spec.md scenario ID, e.g. `t.Run("US1-AS3-missing-name", ...)`, `t.Run("US3-AS1-invalid-json", ...)`, `t.Run("EdgeCase-name-bidi-char", ...)` -- this is what SC-002 means by "each has a same-named automated test."
  3. Cover, at minimum: every US1 acceptance scenario (AS1-AS8), every US2 scenario (AS1-AS5), every US3 scenario (AS1-AS5), and every bullet in the Edge Cases section (name edge cases x4 error + 1 warning, multiple-candidate-location note is WP03's concern not this WP's, `dependencies` element-is-number and missing-name, `author.name` non-string, `hooks`/`mcpServers`/`lspServers` as inline objects being legal, `themes`/`monitors` top-level warning, verbose field listing is WP03's concern).
  4. Also add the two scaffold-based zero-finding cases from SC-001: feed this function the *exact* JSON bytes `internal/pluginjson.Scaffold` and `ScaffoldAgent` would write (construct via `bundle.MarshalIndent` + the same field lists, or inline literal JSON matching them) and assert zero findings -- this is the sharpest possible regression test against your own rule table disagreeing with your own scaffold.
  5. Run `go test ./internal/pluginjson/ -run TestValidate -v` and confirm every subtest passes.
- **Files**: `internal/pluginjson/validate_test.go` (new).
- **Parallel?**: No -- this subtask's early skeleton-only run against T001 is your RED evidence; its final green run (after T002-T006) is your GREEN evidence. Record both in the Activity Log below.
- **Notes**: Use `t.TempDir()` is not needed here -- `Validate` takes `[]byte` directly, no filesystem. Do not use a global fixture; build each scenario's input bytes inline (AGENTS.md testing pattern).

## Test Strategy

- Primary: `go test ./internal/pluginjson/ -run TestValidate -v` -- every acceptance scenario and edge case as a named subtest (T007).
- Before implementing T002-T006's logic, run the test file against T001's stub `Validate` (returns `Report{}`) and confirm it fails (RED) -- paste the failing test count into the Activity Log.
- After each of T002-T006 lands, re-run and confirm the newly-covered subtests turn green one check category at a time.
- `go vet ./internal/pluginjson/...` must be clean before marking this WP done.

## Risks & Mitigations

- Rule table omissions: cross-check the full known-field list in this prompt's Context section against `research.md` R-03 one more time before calling T001 done -- a missing field here becomes a WP02 schema-sync failure, which is a slower feedback loop than catching it now.
- Duplicate-key detection double-reading the same bytes twice (`json.Unmarshal` once, `json.Decoder.Token()` once) is intentional and cheap at ≤5 MiB (NFR-005 is a non-issue at this scale) -- do not try to unify the two passes into one clever decoder loop; keep it simple and readable.
- Damerau-Levenshtein must allow adjacent-transposition (not plain Levenshtein) or `descripton`->`description` may compute a distance > 2 depending on exact edit path; verify with a quick manual trace before trusting the implementation.

## Review Guidance

- Confirm every message string in `validate_test.go`'s expectations was copied character-for-character from `contracts/cli-plugin-validate.md`'s Messages table, not retyped from memory.
- Confirm the rule table's `Source` tags are accurate against research.md R-03 -- WP02 depends on this being right, not just present.
- Confirm `Validate` never imports `os`, `io`, or anything filesystem-related (grep the import block).

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
