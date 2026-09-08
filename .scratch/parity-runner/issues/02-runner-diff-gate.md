# 02 — Parity runner: normalise, diff, classify, waiver gate, self-test

**What to build:** After 01's raw capture, the runner produces `diff.jsonl` (one line per case listing exactly which fields differ between Oracle and Target), exits non-zero on any unwaived diff, and refuses to report success unless its own fault-injection self-test proves the diff path works.

**Blocked by:** 01 — Parity runner: isolated execution and raw capture.

**Status:** done — 9eff5ce, ed14fd6, 2fa1071 (implementor ×3) + 0c1ba73, 1dc231a (orchestrator); verified .review/eval-ticket-02-r4.md PASS

**Spec:** stories 29–32. Evaluator guardrails: `.review/ticket-review.md` §F "01B", §E3 (waiver governance, folded in here), §C note on taxonomy.

## Acceptance criteria

### Normalisation (applied to a COPY; raw records untouched)
- [ ] Only these substitutions, nothing else: the run's temp cwd → `<TMP>`, `APM_CONFIG_DIR` → `<CFG>`, `HOME` → `<HOME>`; `\b\d+(\.\d+)?(ms|s)\b` → `<DUR>`; 7–40 hex-char tokens bounded by non-hex → `<SHA>`; ISO-8601 timestamps → `<TS>`. Binary name `apm-go`↔`apm` ONLY when `case.rewrite_binary_name` is true.
- [ ] A unit test feeds a string containing command wording, field order, and an exit code next to a temp path, and asserts only the path changed.

### Diff
- [ ] Per case compare: `exit_code`, normalised `stdout`, normalised `stderr`, `tree` (path set, per-path kind/size/sha), and raw bytes of every file present on both sides. Missing-on-one-side files are a tree diff.
- [ ] `<out>/diff.jsonl` line: `{ "id", "fields": ["exit_code","stdout",…], "taxonomy": {"expected": [...from case], "heuristic": [...]}, "waived": bool, "waiver_reason": "" }`. Raw field-level diffs are kept in `<out>/diff/<id>.json` (old/new per field). Heuristic classes (exit→F08, help text→F01, tree/bytes→F09/F03) are advisory; `expected_taxonomy` from the case is authoritative and both are always emitted.
- [ ] Also emit `<out>/summary.txt` via `ux.Table`: id, fields differing, waived, taxonomy.

### Waiver gate
- [ ] `waivers.json` schema: array of `{ "id", "fields": [...], "taxonomy": "F0x", "oracle_commit", "reason", "owner", "eval_plan_ref" }`. `fields` must be explicit names — no wildcards, no empty. Unknown `id`, empty `reason`, or `oracle_commit` ≠ the pinned baseline → runner exits 2 with a validation error before running anything.
- [ ] A diff is waived only if its `id` matches and EVERY differing field is listed in that waiver's `fields`. Partial coverage = unwaived.
- [ ] Exit 0 iff every case's diff is empty or fully waived AND the self-test passed. Exit 1 otherwise. `diff.jsonl` still shows `waived: true` lines so waivers are visible, not hidden.

### Self-test (runs automatically before the real cases; `-selftest-only` runs just this)
- [ ] Uses stub scripts as both sides, in-process, never the real Oracle.
- [ ] Case S1: identical stubs → zero diff fields.
- [ ] Case S2: stub differs only in one stdout byte → `fields == ["stdout"]`.
- [ ] Case S3: differs only in exit code → `fields == ["exit_code"]`.
- [ ] Case S4: Target writes one extra file → `fields == ["tree"]`.
- [ ] Case S5: S2 with a matching waiver → `waived: true`; with a waiver that lists only `exit_code` → `waived: false`.
- [ ] If any of S1–S5 fails, the runner exits 3 and does not run real cases.
- [ ] The two product cases from 01 (`--version`, `doctor --help`) remain; `--version` is expected to diff and is waived with `taxonomy: "negative-control"` — that tag is reserved for runner cases and never accepted for product waivers.

## Attempt-2 additions (from eval-ticket-02.md and the first real search run)

- [ ] **Ordering (W2):** `LoadCases` → validate `waivers.json` against known ids + `preflight.oracle_commit` → ONLY THEN execute any case. A test proves an invalid waiver file results in zero Oracle/Target invocations (stub marker files absent).
- [ ] **negative-control reserved (W3):** a product case (any id not under the runner's own self-test set) whose waiver has `taxonomy: "negative-control"` → exit 2 at validation. Test it.
- [ ] **Raw old/new (D2):** `diff/<id>.json` keeps BOTH `raw` and `normalized` old/new for stdout/stderr (raw read from the `.bin` files).
- [ ] **Bytes normalisation:** the same path substitutions applied to stdout/stderr are applied to the bytes of every text file under `<CFG>` and `<TMP>` before the tree/bytes compare (UTF-8-decodable files only; binary files compare raw). Otherwise a registry file that stores the fixture's absolute path diffs on every case. Test with a stub that writes its cwd into a file.
- [ ] **help_semantic field:** for any case whose `argv` ends in `--help`, additionally extract from each side's stdout the set of `{long_flag, short_alias, default_if_shown, description}` and the first description paragraph, and compare as a separate diff field `help_semantic`. Click/Cobra layout differences then land in `stdout` only and can be waived there, while a flag/description drift surfaces in `help_semantic` and cannot be hidden by a `stdout` waiver.
- [ ] **No bulk waivers:** `waivers.json` in this ticket contains exactly: `version` (negative-control) and `doctor-help` (`fields: ["stdout"]`, F01, the evaluator's draft text in eval-ticket-02.md). Remove every `search-*` waiver added by ticket 05 attempt 1 — those diffs are real and belong to ticket 05.

## Attempt-3 items (from eval-ticket-02-r2.md)

- [ ] **Capture scope must include `HOME`.** The Oracle ignores `APM_CONFIG_DIR` entirely and writes its registry to `$HOME/.apm/` (`eval-ticket-02-r2.md` Issue 1). The runner already isolates `HOME` to a temp dir, so the real `~/.apm` is safe — but the tree/fs evidence never looked there. Fix: tree listing and fs/ copy cover cwd ∪ `APM_CONFIG_DIR` ∪ `HOME`, with `HOME`-rooted paths recorded under `home/` and normalised to `<HOME>`. Test with a stub that writes `$HOME/.apm/x.json`. This is a ticket 01 contract amendment; update 01's AC5 wording in the same commit.
- [ ] **help_semantic: strip the default annotation from `description`** after extracting `default_if_shown` (apply the same `defaultAnnotationRe` removal, then trim/collapse whitespace). Add a parser test: Click `"Max results to show  [default: 20]"` and Cobra `"Max results to show (default 20)"` both → `description: "Max results to show"`, `default_if_shown: "20"`.
- [ ] **negative-control is runner-owned, not self-declared.** Eligibility comes from a constant list of ids inside the runner (currently just `version`), NOT from `expected_taxonomy` in `case.json`. A product case that declares `expected_taxonomy: ["negative-control"]` in its manifest AND has a matching waiver must still be rejected at validation (exit 2). Test with the evaluator's reproducer (`product-negative`).
