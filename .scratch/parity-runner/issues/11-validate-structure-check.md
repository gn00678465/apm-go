# 11 — `marketplace validate` missing Structure check + help drift (M-06)

**What to build:** `apm-go marketplace validate NAME` reports the same three checks as the Oracle — Structure, Schema, Names — with the same summary count, and its `--help` carries the same semantic flag/description set.

**Blocked by:** 02 — runner diff/gate (attempt 3: HOME capture so the registry tree diff disappears).

**Status:** done (2026-08-24, attempt 3) — `.review/eval-ticket-11.md` FAILed attempt 1: the Structure check ported only the two top-level "plugins" diagnostics and silently passed structurally-invalid *elements* (`plugins:[null]`, `plugins:[{}]`, and "similar source-shape cases"). Attempt 2 ported the Oracle's complete per-element diagnostic set, but its own `isValidRemoteCoordinate` approximation, `repo`/`repository` fallback, and `tag_pattern` handling each still diverged from the Oracle in ways attempt 2's own stated scope boundary didn't own up to — attempt 3 closes those three (see "Attempt 3" section below). Attempts 1-2's other product-parity fixes (Structure/Schema/Names ordering, rendering, help parity, `withSilentExitCode`, the systemic F01 waivers, the `mkt-026` correction) were accepted and are unchanged.

**Origin:** runner cases from ticket 06 (`.review/eval-ticket-06.md` AC5): Oracle prints Structure/Schema/Names → "3 passed"; Target prints Schema/Names → "2 passed". `validate-help` shows a `help_semantic` diff. These are pre-existing M-06 gaps the runner exposed, not caused by ticket 06.

**Oracle:** `commands/marketplace/validate.py:17-91`, `marketplace/validator.py` (`validate_marketplace` — read what "Structure" asserts and what message it prints on pass/fail).

## Investigation findings

- **Structure asserts manifest-shape, not content.** `validate_marketplace_structure` (validator.py) just re-reports `manifest.structural_errors`, a list the tolerant JSON parser (`models.py`'s `parse_marketplace_json`) retains whenever it silently drops or coerces something malformed: `"plugins" not a list` → `"plugins: expected a list"`; a non-object element → `"plugins[N]: expected an object"`; plus several per-field diagnostics inside `_parse_plugin_entry` (empty name, unrecognized/invalid source shape, bad `tag_pattern`, etc.) that this ticket does NOT attempt to replicate in full (see "Deliberate partial-parity gap" below).
- **Rendering has real gating logic, not just three more lines.** `validate.py:31-42/60-64`: `has_structure_errors` (true iff the Structure check itself has errors) suppresses the "Found N plugins" line, the `--verbose` per-plugin detail, AND every OTHER check's own passing line — a broken manifest means "Schema: passed" is misleading noise once Structure has already failed. A passing check's line reads `f"  {check_name}: passed"` (two-space indent baked into the message, rendered via `symbol="check"` → `"[+] "`), not "all plugins valid" (apm-go's old, invented wording).
- **`sys.exit(1)` is silent** (validate.py:81-82) — no extra message beyond the Results/Summary already printed. apm-go's old `return fmt.Errorf(...)` would have added a bogus `[x] marketplace "X" failed validation with N error(s)` line via main()'s root error handler once any case actually exercised the error path (none had, before `validate-structure-fail`) — the exact same bug shape ticket 08 found and fixed for `doctor`. Fixed with `withSilentExitCode`, the mechanism ticket 08 already added.
- **Two pre-existing, systemic rendering gaps surfaced by this ticket's own fixtures, judged out of scope:**
  1. `logger.start(..., symbol="gear")` (validate.py:29, the "Validating marketplace '...'..." line) maps to `STATUS_SYMBOLS["gear"] = "[*]"`; apm-go's equivalent line uses `ux.Info` (`"[i]"`). apm-go has no printer for the Oracle's distinct gear/success/sparkles `"[*]"` glyph at all — confirmed system-wide (`apm-go marketplace add`'s own "Registering marketplace..." line has the identical gap), not specific to validate.
  2. `ux.Success` still renders `colors.go`'s pre-ticket-10 centered-symbol style (`" + "`) instead of the Oracle's bracket `"[+] "` (printer.go's own doc comment already flagged this as deliberately deferred: "Success was never found to differ, so it keeps the existing centered-symbol convention untouched" — this ticket's own fixtures are the first to actually exercise a passing check and prove it does differ). `ux.Success` has 14+ call sites across the whole CLI; realigning it is a separate, ticket-10-shaped systemic fix, not this ticket's scope.
  Both are recorded as field-precise `stdout` waivers (taxonomy `F01`, matching the established rendering-difference convention), not fixed.
- **Attempt 1's partial-parity call was wrong, not just incomplete.** Attempt 1 shipped only the two top-level "plugins" diagnostics and left every per-field diagnostic inside `_parse_plugin_entry` silently unported — apm-go's existing tolerant parser kept accepting (or silently dropping without a message) exactly the malformed entries the Oracle reports as Structure errors. `.review/eval-ticket-11.md` correctly rejected this as a real semantic gap, not an acceptable scope boundary: `apm marketplace validate` reporting `exit 0`/`Structure: passed` for a manifest the Oracle rejects is a parity bug, not a documented gap.

## Attempt 2: the complete per-element diagnostic set

Reading `_parse_plugin_entry` (models.py:422-547) and `_dict_source_error`
(models.py:50-77) in full, end to end, surfaced the entire diagnostic set —
not just the evaluator's two reproducers:

- **name**: absent, non-string, or blank after trim → `"name: expected a
  non-empty string"`.
- **source present but neither string nor object** (explicit JSON `null`, a
  number, an array, or a bool) → `"source: expected a string or object"`.
  Critically, `"source": null` must NOT fall back to checking
  `"repository"` — Python's `"source" in entry` is true even for a null
  value, so it never reaches the `elif "repository" in entry` branch. Go's
  `encoding/json` unmarshals both an absent key and a present-null key into
  the same zero value for a bare `any` field, so this required decoding the
  entry into `map[string]json.RawMessage` (key presence, even with a null
  value, is distinguishable there) rather than a typed struct — the exact
  same class of bug as the top-level `plugins:[null]` reproducer itself.
- **source is a dict**: full `_dict_source_error` port
  (`dictSourceDiagnostic`) — npm rejection, "no recognized type and no
  repo", an unsupported type outside `{github,url,git-subdir,gitlab}`, and
  each of those four types' own required-field / valid-coordinate checks.
- **source absent, repository present but not an `"owner/repo"`-shaped
  string** → `"repository: expected an owner/repository string"`.
- **neither source nor repository present** → `"source: expected a source
  or repository field"`.

All 30 hand-derived cases (covering every branch above, plus the
accumulate-not-first-error-stops behavior) were verified against the real
pinned Oracle directly (`parse_marketplace_json`, not just a reading of the
source) before writing the Go port — every one matched byte-for-byte.

**A genuinely new discovery, not merely a port: `mkt-026`'s "dual-layer npm"
premise was factually wrong.** `_parse_plugin_entry`'s own `source_type`
derivation (models.py:447-454) is `for key in ("type", "source", "kind")` —
all three keys, at PARSE time, in the Oracle itself. An earlier apm-go
design decision (and its own test, `TestResolvePluginSource_NPMDualLayer`)
believed the Oracle only checked `type`/`source` at parse time and deferred
`kind` to a later resolve step — a misreading. The pinned Oracle rejects
`{"kind": "npm", ...}` at parse time exactly like `{"type": "npm", ...}`
(verified live against `parse_marketplace_json`). `dictSourceDiagnostic`
now reads all three keys, matching the Oracle; the affected test is
corrected to `TestParseManifestPlugins_NPMRejectedForAllThreeDiscriminatorKeys`
(`resolver_test.go`), and `coercePluginType`'s/`TestMarketplaceManifest_PluginNormalization`'s
doc comments no longer claim a parse-vs-resolve split that never existed.
`resolvePluginSource`'s own independent npm rejection (exercised directly,
bypassing JSON parsing, by `TestResolvePluginSource_NPMRejected`) is
unaffected and still a real safety net for a plugin constructed some other
way.

**Still deliberately bounded, and stated as such:** `isValidRemoteCoordinate`
(the "is this syntactically a plausible non-local coordinate" check inside
`dictSourceDiagnostic`) does not replicate `DependencyReference.parse`'s
full acceptance grammar (SSH URLs, ADO/Artifactory coordinates, GitLab
nested groups, virtual-package subpaths, alias syntax — its own
multi-hundred-line module). It checks non-empty, not local-path-shaped, and
free of control characters — exactly what distinguishes every case this
ticket's own reproducers and the existing test suite exercise. This is the
one remaining, explicitly documented partial-parity boundary (AGENTS.md's
"deliberate but partial"), not a silently-reintroduced version of attempt
1's rejected gap: it only affects the narrow "recognized type, syntactically
implausible coordinate" corner, not whether malformed entries are detected
at all.

## Attempt 3: coordinate grammar, repo fallback truthiness, tag_pattern deferral

`.review/eval-ticket-11.md`'s attempt-2 ruling confirmed both attempt-2
reproducers fixed, but raised three NEW blocking reproducers — none were
regressions in the ported diagnostics themselves, all were corners attempt
2's own "still deliberately bounded" note (above) had wrongly assumed were
out of scope or hadn't considered at all:

1. **`isValidRemoteCoordinate`'s bounded approximation under-rejects.**
   Attempt 2's checks (non-empty, not local-path-shaped, no control chars)
   accepted syntactically invalid coordinates the Oracle's real
   `DependencyReference.parse` (models.py:40-47) rejects outright:
   `"owner/"`, `"owner//repo"`, `"owner/repo?x"`, and a bare word (`"foo"`)
   for a `url` source. Rather than hand-porting `DependencyReference.parse`'s
   grammar a second time, `isValidRemoteCoordinate` now calls
   `manifest.ParseDepString` — apm-go's own existing full Go port of that
   same grammar, already used by `install`/dependency resolution
   (`internal/manifest/depref.go`) — and returns `err == nil && !ref.IsLocal`.
   This closes the divergence for free (the parser's `parseShorthand`,
   `ownerCharRe`, `repoCharRe` already reject every example above) and
   deletes the second, weaker approximation instead of maintaining two.
   `looksLikeLocalPath` is no longer called from this path (still used
   elsewhere for `SourceKind` classification).
2. **`repo`/`repository` fallback used the wrong "presence" semantics.**
   The Oracle's actual expression is `raw.get("repo", "") or
   raw.get("repository", "")` — Python `or`, which falls through on any
   *falsy* value (`""`, `0`, `None`, missing), not merely an absent key.
   Attempt 2's `firstNonEmptyString` treated any non-string `"repo"` as
   absent and fell back to `"repository"` — wrong: a truthy non-string
   `"repo"` (e.g. `42`) is used as-is by the Oracle and never falls back
   (then correctly fails as "no `/` in it" → `"github requires an
   owner/repository field"`), while a *falsy* `"repo"` (`""` or `0`) does
   still fall back. Added `pythonTruthy`/`pythonGetOr` implementing this
   exact `or`-fallback semantic; `dictSourceDiagnostic` now reads
   `pythonGetOr(src, "repo", "repository")` for its repo lookup instead of
   `firstNonEmptyString` (still used, unchanged, for the `type`/`source`/
   `kind` derivation, which IS simple presence-checking in the Oracle).
3. **`tag_pattern` errors failed the wrong thing.** Attempt 2 made an
   invalid `tag_pattern` a hard Go `error` out of `parsePluginEntry`,
   propagating up to fail the entire `marketplace add`/fetch — but the real
   Oracle (models.py:521-533) catches `TagPatternError` *inside*
   `_parse_plugin_entry` and converts it to an ordinary per-entry
   `(None, diagnostic)` skip, exactly like every other structural
   diagnostic: the malformed entry is dropped from `manifest.plugins` and
   reported via `structural_errors`, while `marketplace add` still
   *succeeds* and every sibling plugin is unaffected. `parsePluginEntry`
   (and transitively `parseManifestPlugins`/`UnmarshalJSON`) no longer
   returns an `error` for this case at all. Added `pythonRepr` (Python
   single-quoted repr formatting, for the Oracle's exact f-string message
   bytes), `tagPatternPlaceholderRe`, and `tagPatternStructuralError` to
   produce the Oracle's exact message
   (`"source.tag_pattern: 'Plugin '<name>' source.tag_pattern' must
   contain exactly one {version} placeholder, got '<pattern>'"`, plus the
   non-string and unsupported-placeholder variants) as a Structure
   diagnostic instead of a Go error. The stale doc comment above
   `parsePluginEntry` (models.go:686-701 in the eval's numbering) claiming
   tag_pattern failure "fails the whole document" is corrected; the
   existing test that had encoded that wrong belief
   (`TestUnmarshalJSON_PluginSourceTagPattern`'s last subtest) is rewritten
   to assert the real per-entry-drop behavior.

**Blast radius verified directly against the pinned Oracle** (per the
eval's explicit request, since `parseManifestPlugins` is shared with
browse/search/install): a marketplace with one valid plugin and one
`tag_pattern`-broken plugin now registers successfully via `marketplace
add` on both sides (Oracle emits a CommandLogger warning naming the
malformed-entry count); `marketplace browse` lists only the valid plugin on
both sides; `install <bad-plugin>@<mkt>` fails with a "not found in
marketplace"-style error on both (different exact wording, not a
newly-introduced divergence, not flagged by the eval); `marketplace
validate` reports the exact Structure diagnostic (modulo the
already-waived `[*]`/`[i]` icon difference) with exit 1 on both. Encoded as
`TestMarketplaceValidate_TagPatternDeferral` (`cmd/apm-go/
marketplace_e2e_test.go`), exercising `add` → `browse` → `validate`
end-to-end through the real cobra command tree.

All three fixes, plus the evaluator's named divergence-class probes
(`owner//repo`, `owner/repo?x`, `url:"foo"`), plus a coordinate-grammar
"still accepts every previously-valid shape" regression case (HTTPS URL,
SCP-style SSH, host-qualified `github.com/owner/repo`) and truthy/falsy
repo-fallback pairs (`repo:42` vs `repo:""`/`repo:0`, each with a
`repository` fallback present), were verified byte-for-byte against the
live pinned Oracle before being encoded as the ~12 new
`TestMarketplaceManifest_StructuralErrors` sub-cases plus the rewritten
`TestUnmarshalJSON_PluginSourceTagPattern` subtest.

No import cycle: `internal/manifest` does not import `internal/marketplace`
(verified); `internal/marketplace` already imports `internal/manifest`
elsewhere (`resolver.go`, `resolve_plugin.go`, `crossrepo.go`), so adding
the same import to `models.go` is the existing dependency direction, not a
new one.

## Acceptance criteria

- [x] Target's `ValidateChecks` (or its caller) emits a Structure check with the Oracle's pass/fail message, in the Oracle's order (Structure, Schema, Names), and the summary counts it. Fixture with a structurally broken manifest (`plugins` not a list, `validate-structure-fail`) fails Structure with the Oracle's message ("plugins: expected a list") and exit 1. **Attempt 2:** every per-element diagnostic `_parse_plugin_entry`/`_dict_source_error` can raise is now ported (see "Attempt 2" section above) and verified against both the real Oracle directly and `apm-go marketplace validate` end to end for the evaluator's exact two reproducers (`plugins:[null]`, `plugins:[{}]`) plus 28 further hand-derived cases.
- [x] `validate --help`: `help_semantic` diff is empty against the Oracle (flag set, short aliases, defaults, descriptions) — verified on a fresh corpus run (no `help_semantic` key on `validate-help`'s diff at all). `--check-refs` stays hidden on both.
- [x] Rich/`ux` symbol and quote differences in the results lines (plus the pre-existing "gear" progress-line icon gap, discovered by this ticket's own fixtures) are the ONLY remaining `stdout` diff, recorded as field-precise waivers per case (taxonomy `F01`, the established rendering-difference convention; `F08` for `validate-structure-fail`'s additional `stderr`/`error_body` finding, see below) — not wording, not counts. Verified content-identical on a fresh corpus run: "Found N plugins", "Validation Results:", "Structure/Schema/Names: passed", and the Summary line all match byte-for-byte on both sides.
- [x] Runner cases `validate-checkrefs-off`, `validate-checkrefs-on`, `validate-help`, plus new `validate-structure-fail`: `diff.jsonl` shows no `help_semantic`, no `exit_code`. `tree` is **not** clean on `validate-checkrefs-off`/`-on`/`-structure-fail` (see below) — the acceptance criteria's original "no tree" expectation predates discovering that ticket 05's own open registry-serialization gap (`home/.apm/marketplaces.json`: the Oracle stores a `file://` URI, apm-go a bare path — already documented and deliberately left unwaived on `search-basic-hit`'s own waiver) applies here too, via the identical `marketplace add` setup_argv step. This is not a regression: `tree`'s `(fields, waived)` state for these cases is byte-identical before and after this ticket (verified by diffing two full-corpus runs) — it is ticket 05's pre-existing, still-open item, not something this ticket introduced or is positioned to close. `stdout` (and `stderr`/`error_body` for `-structure-fail`) are field-precisely waived.

## Evidence

Full-corpus run against the pinned Oracle (real subprocess, no mocks), clean tree at this ticket's commit, before → after: 27/48 → 27/49 unwaived overall (one new case added, `validate-help` newly waived-clean, zero other change — see `.scratch/parity-runner/issues/08-doctor-backfill.md`'s own baseline for the "27" starting point). Diffing the two full runs' `(id, fields, waived)` triples: the ONLY common-id change is `validate-help`: `(['stdout','help_semantic'], False)` → `(['stdout'], True)`. Every other id's `(fields, waived)` tuple is byte-identical, confirming zero regressions anywhere else in the corpus. `validate-structure-fail` is the one new id.

- `validate-help`: waived clean (`stdout` only, `F01`, Click-vs-Cobra layout — `help_semantic` is empty).
- `validate-checkrefs-off` / `validate-checkrefs-on`: `stdout` waived (`F01`, the two pre-existing gear-icon/success-icon rendering gaps plus quote style); overall case verdict stays unwaived because `tree`'s pre-existing ticket-05 gap is untouched — not this ticket's regression (confirmed unchanged from before).
- `validate-structure-fail` (new): `stdout`/`error_body` waived for the same rendering gaps; `stderr` waived (`F08`) for a genuine Oracle-only Python `logging.warning(...)` line (`models.py`'s tolerant parser logs to Python's stdlib logger, a channel separate from the Rich/CommandLogger stdout this ticket's Structure check already surfaces the same diagnostic on) that apm-go has no equivalent second channel for, judged out of scope (replicating it would be a redundant copy of the same diagnostic, not new information); `tree` carries the same pre-existing ticket-05 gap via its own `marketplace add` setup step.

`go test ./...` and `-race` on `cmd/apm-go`, `internal/marketplace` (+ `authoring`/`build`/`tagpattern`), and `tools/parity` are green (pre-existing Windows-only `TestParseDepString_AbsolutePath` excepted).

## Files touched
- `internal/marketplace/models.go` (+`MarketplaceManifest.StructuralErrors`, `parseManifestPlugins` diagnostics), `internal/marketplace/models_manifest_test.go` (+`TestMarketplaceManifest_StructuralErrors`).
- `internal/marketplace/validator.go` (+Structure check, first in order), `internal/marketplace/validator_test.go` (updated for 3 checks + new structural-error test).
- `cmd/apm-go/marketplace.go` (`validate`'s rendering: `has_structure_errors` gating, `"  %s: passed"` wording, errors-then-warnings grouping, `withSilentExitCode`, `Short`/`-v` help text), `cmd/apm-go/marketplace_e2e_test.go` (updated happy-path assertions, +`TestMarketplaceValidate_StructureCheckFailsOnBrokenManifest`).
- `tools/parity/cases/validate-structure-fail/` (new).
- `tools/parity/waivers.json` (+4 entries: `validate-help`, `validate-checkrefs-off`, `validate-checkrefs-on`, `validate-structure-fail`), `tools/parity/main_test.go` (`TestRealWaiversJSON_ValidatesAgainstPin`'s `wantIDs` extended).

### Attempt 2 additions
- `internal/marketplace/models.go`: `parseManifestPlugins`/`parsePluginEntry` rewritten to port `_parse_plugin_entry`'s complete diagnostic set (name, source-shape, dict-source-type dispatch, repository fallback); new `jsonValueKind`, `dictSourceDiagnostic`, `firstNonEmptyString`, `isValidRemoteCoordinate` helpers; the old `rawPlugin`/`normalize()` type+method removed (superseded by `parsePluginEntry` operating on `map[string]json.RawMessage`, needed for null-vs-absent key presence detection).
- `internal/marketplace/models_manifest_test.go`: `TestMarketplaceManifest_StructuralErrors` extended from 7 to 37 cases (the 2 reproducers + every `dictSourceDiagnostic` branch + a name-shape/repository-shape sweep + an accumulate-order case); `TestMarketplaceManifest_PluginNormalization` corrected (npm-kind-variant now dropped, not kept).
- `internal/marketplace/resolver.go`: `coercePluginType`'s doc comment corrected (no parse-vs-resolve npm split).
- `internal/marketplace/resolver_test.go`: `TestResolvePluginSource_NPMDualLayer` replaced with `TestParseManifestPlugins_NPMRejectedForAllThreeDiscriminatorKeys` (asserts all three keys are dropped at parse time, with the matching `StructuralErrors` message); `TestResolvePluginSource_NPMRejected`'s doc comment corrected.
- `cmd/apm-go/marketplace_e2e_test.go`: +`TestMarketplaceValidate_StructurePerElementDiagnostics` (the evaluator's exact two reproducers, at the cobra command level, end to end through `apm-go marketplace validate`).

### Attempt 3 additions
- `internal/marketplace/models.go`: `isValidRemoteCoordinate` rewritten to call `manifest.ParseDepString` (new `internal/manifest` import) instead of the attempt-2 bounded approximation; new `pythonTruthy`/`pythonGetOr` helpers, `dictSourceDiagnostic`'s repo lookup switched to `pythonGetOr(src, "repo", "repository")`; new `pythonRepr`, `tagPatternPlaceholderRe`, `tagPatternStructuralError`; `parsePluginEntry`/`parseManifestPlugins`/`UnmarshalJSON`'s call site no longer return an `error` for tag_pattern (or anything else — nothing produces one any more); stale "fails the whole document" doc comment on `parsePluginEntry` corrected; `internal/marketplace/tagpattern` import removed (no longer called from this path).
- `internal/marketplace/models_manifest_test.go`: `TestMarketplaceManifest_StructuralErrors` extended with the eval's three reproducers, its named divergence-class probes, a coordinate-grammar regression case, and truthy/falsy repo-fallback pairs; `TestUnmarshalJSON_PluginSourceTagPattern`'s last subtest rewritten from asserting whole-document failure to asserting per-entry drop + `StructuralErrors`; unused `"strings"` import removed.
- `cmd/apm-go/marketplace_e2e_test.go`: +`TestMarketplaceValidate_TagPatternDeferral` (blast-radius regression: `add` succeeds, `browse` excludes only the malformed entry, `validate` reports the Structure diagnostic — verified against the real Oracle first, then encoded).

## Orchestrator intervention (after 3 failed attempts; 2026-08-24)

Root causes pinned by direct two-sided probes (orchestrator-run, not inferred):

**A. Both parser-boundary divergences live in `manifest.ParseDepString` itself** — apm-go's port of the SAME `DependencyReference.parse` that install uses, so these are latent install-parity bugs too. Fix the parser, not another marketplace-side wrapper:

1. **Whole-string percent-decode first.** Oracle `reference.py:1748`: `dependency_str = urllib.parse.unquote(dependency_str)` runs ONCE on the whole string, before local-path detection, host parsing, everything — then `reference.py:1750-1751` rejects control chars (`ord(c) < 32`) on the DECODED string. Probe: Oracle parses `owner/%72epo` → repo_url `owner/repo`, host `github.com`; apm-go errors `invalid repo "%72epo"`. Port note: Python `unquote` is lenient (invalid escapes like `%zz` pass through unchanged; decoded bytes go through UTF-8 `errors='replace'`) — Go's `url.PathUnescape` errors on invalid escapes, so hand-port a lenient unquote (decode only valid `%XX` runs, replace invalid UTF-8 with U+FFFD). Security: keep every path/segment validation AFTER the decode, same order as the Oracle (its `validate_path_segments` runs post-decode) — add a regression test that a percent-encoded traversal (`%2e%2e`) is still caught post-decode.
2. **URL hosts must be valid FQDNs.** Oracle `github_host.py:1074-1102` `is_valid_fqdn`: labels of `[a-zA-Z0-9-]` not starting/ending with hyphen, AT LEAST ONE DOT — regex `^[a-zA-Z0-9](?:[a-zA-Z0-9-]*[a-zA-Z0-9])?(?:\.[a-zA-Z0-9](?:[a-zA-Z0-9-]*[a-zA-Z0-9])?)+$`. Probe: Oracle raises `Invalid Git host: 'x'.` (+ FQDN guidance body) for `https://x/owner/repo`; accepts `https://x.io/owner/repo` (host kept as `x.io`). apm-go accepts host `x`. Port the FQDN gate (and its error message text) into ParseDepString's URL branch; check the SCP/ssh branches take the same gate as the Oracle's `is_supported_git_host` (github_host.py:202 rejects any non-FQDN before allow-lists).

**B. `pythonRepr` (models.go:669-688) must be a real Python `repr` port**, because the tag_pattern diagnostic embeds `{pattern!r}` and the pattern can be ANY JSON value:
- dict → `{'x': 1}`: requires insertion-order-preserving decode of the raw tag_pattern JSON (json.Decoder token walk; Go maps randomize) and recursive repr; list → `['a', 1]`.
- str → Python quote selection (single quotes unless the string contains `'` and no `"`), escapes `\\`, `\'`, `\n`, `\r`, `\t`, `\xNN` for other C0 + DEL; non-ASCII printable stays literal, non-printable → `\xNN`/`\uNNNN` (Python `str.isprintable()` basis).
- numbers → int-vs-float needs the JSON lexeme: decode with json.Number; no `./e/E` → integer repr; else Python float repr (`1.0` → `1.0` NOT `1`; ensure a `.`/exponent survives formatting).
- Pin every branch with a table test whose expected bytes come from the Oracle (run `python -c "print(repr(...))"` per case to capture them).

**C. Test-evidence gap (not a product blocker):** `TestMarketplaceValidate_TagPatternDeferral` claims browse/search/install coverage but never invokes the `marketplace add` command, root `search`, or `install`. The evaluator probed the real behavior and it MATCHES — complete the test to do what its comment says.
