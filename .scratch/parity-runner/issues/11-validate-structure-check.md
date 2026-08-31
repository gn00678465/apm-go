# 11 — `marketplace validate` missing Structure check + help drift (M-06)

**What to build:** `apm-go marketplace validate NAME` reports the same three checks as the Oracle — Structure, Schema, Names — with the same summary count, and its `--help` carries the same semantic flag/description set.

**Blocked by:** 02 — runner diff/gate (attempt 3: HOME capture so the registry tree diff disappears).

**Status:** done (2026-08-24, attempt 5, orchestrator strategy change: conformance tables replace reproducer-chasing) — `.review/eval-ticket-11.md` FAILed attempt 1: the Structure check ported only the two top-level "plugins" diagnostics and silently passed structurally-invalid *elements* (`plugins:[null]`, `plugins:[{}]`, and "similar source-shape cases"). Attempt 2 ported the Oracle's complete per-element diagnostic set, but its own `isValidRemoteCoordinate` approximation, `repo`/`repository` fallback, and `tag_pattern` handling each still diverged from the Oracle in ways attempt 2's own stated scope boundary didn't own up to — attempt 3 closed those three. The orchestrator then pinned three deeper root causes with two-sided probe evidence: `manifest.ParseDepString` itself (the dep-string parser attempt 3 started reusing) was missing the Oracle's whole-string percent-decode and FQDN host gate, `pythonRepr` could not reproduce Python's insertion-order dict repr, and a test claimed coverage it didn't have — attempt 4 closed all three. Attempt 4's own eval round found FIVE MORE reproducers (arbitrary SSH users, URL query stripping, shorthand host:port splitting, lone-surrogate repr, numeric overflow/negative-zero, duplicate dict keys) — evidence the reproducer-by-reproducer loop was not converging. Attempt 5 changes strategy: fixes all five, then builds and commits a genuine Oracle-generated conformance table (`spec/conformance/depref-accept.json`, `spec/conformance/python-repr.json`) so future gaps surface as a fixture diff instead of another eval round (see "Attempt 5" section below) — and the conformance sweep itself found and fixed a real pre-existing bug beyond the five (a literal `".."` repo/virtual-path segment was silently accepted instead of rejected as path traversal). Attempts 1-3's other product-parity fixes (Structure/Schema/Names ordering, rendering, help parity, `withSilentExitCode`, the systemic F01 waivers, the `mkt-026` correction) were accepted and are unchanged.

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

## Attempt 4: dep-parser percent-decode + FQDN gate, true Python repr, test-evidence gap closed

All three orchestrator-pinned root causes fixed in `internal/manifest/depref.go` (A) and `internal/marketplace/models.go` (B), plus C.

**A1. Whole-string percent-decode, ordered correctly.** `ParseDepString` now
runs `lenientUnquote` (a hand-port of `urllib.parse.unquote`/
`unquote_to_bytes`: an invalid `%` escape — not followed by two hex digits —
passes through completely unconsumed, then the resulting bytes are UTF-8-
decoded with `errors='replace'` semantics via `utf8.DecodeRune`'s own
maximal-subpart algorithm) ONCE, at the very top, before the control-char
check, local-path detection, or any host/owner/repo parsing — matching
`reference.py:1748-1751`'s exact order. `owner/%72epo` now parses to
`owner/repo`, matching the Oracle exactly (verified directly). A percent-
encoded traversal marker (`%2e%2e/%2e%2e/etc/passwd`) is still caught by
`containsEscape` post-decode, not smuggled past it as an opaque `%2e%2e`
segment — `TestParseDepString_PercentEncodedTraversal` locks this down.

**A2. FQDN host gate — applied only where the Oracle actually applies it.**
The user-provided lead named `github_host.py:202` as evidence that SCP/ssh
forms should be gated identically to the HTTPS/HTTP form. Direct two-sided
probing against the pinned Oracle (`DependencyReference.parse`, not just
reading the source) showed this is **not correct**: `ssh://git@host:7999/
owner/repo.git` and `git@host:owner/repo.git` (bare, non-dotted `"host"`)
both **succeed** on the real Oracle, unchanged — neither
`_parse_ssh_protocol_url` nor `_parse_ssh_url` ever calls
`is_supported_git_host`, and `parse()`'s own post-parse validation
(`_validate_final_repo_fields`) only checks `repo_url` segment characters,
never host FQDN-ness. The gate genuinely fires in exactly two places,
confirmed by direct probes and now ported to match:
  - `_validate_url_repo_path` (`reference.py:1492-1493`): the HTTPS/HTTP URL
    form. `https://x/owner/repo` → `Invalid Git host: 'x'.`;
    `https://x.io/owner/repo` → succeeds, host `x.io`. Ported into
    `parseHTTPURL`.
  - `_detect_virtual_package` (`reference.py:1125-1141`): shorthand's own
    `"." in first_segment` heuristic (the SAME one apm-go's `parseShorthand`
    already used to decide "does this look like a host") gates on
    `is_supported_git_host` once triggered. `-x.io/owner/repo`,
    `x-.io/owner/repo` (hyphen-boundary labels), `x..io/owner/repo` (empty
    label) all raise. Ported into `parseShorthand`'s existing host branch.

  `isValidFQDN` ports `is_valid_fqdn` (`github_host.py:1074-1102`)
  verbatim as a regex. `TestParseDepString_FQDNHostGate` and
  `TestParseDepString_URLForm`'s existing `ssh://git@host:7999/...` cases
  together lock down both halves: gated where the Oracle gates, ungated
  where it doesn't. Existing tests survey: no manifest/install test used a
  non-FQDN HTTPS host or a dotted-but-invalid shorthand host, so nothing
  needed updating there — the only existing cases touching this code path
  (`ssh://git@host:7999/...`, twice, in `TestParseDepString_URLForm`) stay
  green precisely because ssh:// is confirmed ungated.

**B. `pythonRepr` rewritten as a real recursive Python-repr port.** The
prior version handled only plain scalars (a single-key Go map happened to
round-trip correctly by accident, but Go map iteration order is
unspecified — a multi-key dict would have been non-deterministic, not just
misordered). Now:
  - `decodeOrderedJSON`/`decodeOrderedValue` walk the raw JSON via
    `json.Decoder`'s token stream (not `Unmarshal`) into an
    insertion-order-preserving tree (`orderedValues` for objects, `[]any`
    for arrays — already ordered — `json.Number` for numbers via
    `UseNumber()`), so `pythonReprValue({'b':1,'a':2})` reprs `"{'b': 1, 'a':
    2}"`, original key order, not alphabetical — verified directly against
    the pinned Oracle for both a single-key and a multi-key dict.
  - `pythonReprNumber` distinguishes int from float by JSON lexeme (no
    `./e/E` → integer, echoed verbatim — this also gets arbitrary-precision
    integers right for free, no bignum arithmetic needed); `pythonFloatRepr`
    adds the `.0` Go's `FormatFloat` omits for a whole-number float
    (`1.0` → `"1.0"` not `"1"`) — Go's shortest-round-trip digits and
    `e+NN`/`e-NN` exponent spelling already match Python's byte-for-byte
    (verified: `1e+20`, `1e-05`).
  - `pythonReprString` ports the quote-selection (single unless the string
    has `'` and no `"`), backslash/quote/`\n`/`\r`/`\t` escapes, `\xNN` for
    other C0+DEL, and `\xNN`/`\uNNNN`/`\UNNNNNNNN` for non-ASCII
    non-printable characters (`unicode.IsPrint` as the printability test).
  - `printableASCIIText` ports `diagnostics.py:52-55`'s
    `printable_ascii_text` — `_parse_plugin_entry` wraps EVERY
    `TagPatternError` message in this (`models.py:533`) before returning
    it, squashing any character `repr()` itself left as a literal non-ASCII
    codepoint (e.g. `'é'`) down to `'?'`, while a `repr()`-produced ASCII
    escape sequence (`\xNN`, `\uNNNN`) survives untouched since it's already
    pure ASCII text. `tagPatternStructuralError` applies this to the whole
    composed inner message, matching the Oracle's own two-stage pipeline
    exactly, not just the repr'd value in isolation.
  - `parsePluginEntry` now separately decodes `sourceRaw` into
    `map[string]json.RawMessage` to get `tag_pattern`'s ORIGINAL bytes
    (`tagPatternRaw`), passed to `tagPatternStructuralError` instead of the
    already-decoded `any` from `srcMap` — the same "raw JSON vs. decoded
    Go type loses information" class of fix as the ticket 11 attempt
    2/3 null-vs-absent-key bugs.
  - `TestPythonReprValue` (23 cases) and `TestPrintableASCIIText` (5 cases)
    pin every branch against bytes captured directly from the pinned
    Oracle via `python3 -c "print(repr(x))"` (not recomputed the way the
    port itself computes them) — including the evaluator's exact
    `{'x': 1}` case and a multi-key `{'b': 1, 'a': 2}` case.

**C. Test-evidence gap closed.** `TestMarketplaceValidate_TagPatternDeferral`
now runs the real `marketplace add` COMMAND (not `marketplace.AddSource`
directly), root `search` (`plugin@broken`), and `install` (via
`runInstall` with the same network-free `installDeps` mock pattern
`TestRunInstall_MarketplacePackage_LockfileProvenanceAndPersistedCanonical`
already uses) end to end: `add` succeeds, `browse`/`search` both list only
the valid plugin, `install good-plugin@broken` succeeds, `install
bad-plugin@broken` fails not-found, `validate` reports the Structure
diagnostic. (`install` runs BEFORE `search` in the test body: `runSearchCmd`
sets `CI=1` for the rest of the test via `t.Setenv`, which would otherwise
push `install` into a frozen/lockfile-required mode it isn't set up for.)

**Verification.** All three of the evaluator's named reproducers re-checked
directly against the live pinned Oracle AND the built `apm-go` binary side
by side, in one manifest: `url: "https://x/owner/repo"` →
`Structure: plugins[0].source: url requires a valid non-local url field`
on both, exit 1; `github repo "owner/%72epo"` → accepted on both (shows up
as a registered plugin, no Structure error); `tag_pattern: {"x": 1}` →
`Structure: plugins[N].source.tag_pattern: 'Plugin '<name>' source.tag_pattern'
must be a non-empty string, got {'x': 1}` on both, byte-identical. All of
attempt 3's own divergence-class probes (`owner/`, `owner//repo`,
`owner/repo?x`, `url: "foo"`) re-verified still correctly rejected after
this attempt's refactor of `isValidRemoteCoordinate`'s dependency
(`ParseDepString` itself changed; the function that calls it did not).

`go build`/`vet`/`gofmt` clean. `go test ./...` green (pre-existing
Windows-only `TestParseDepString_AbsolutePath` excepted). `go test -race
./cmd/apm-go/... ./internal/marketplace/... ./internal/manifest/...` clean.

## Files touched (attempt 4 additions)
- `internal/manifest/depref.go`: `isValidFQDN`/`fqdnRe` (new); `ParseDepString` decodes via `lenientUnquote` before the control-char check and all other parsing; `lenientUnquote`/`lenientUnquoteBytes`/`hexNibble`/`utf8ReplaceInvalid` (new); `parseHTTPURL` and `parseShorthand`'s host-qualified branch gate on `isValidFQDN` instead of (respectively, in addition to) `hostCharRe`; `parseSSHURL`/`parseSCPURL` deliberately left ungated (see isValidFQDN's doc comment for the probe evidence).
- `internal/manifest/depref_test.go`: +`TestParseDepString_PercentDecodeThenParse`, +`TestParseDepString_PercentEncodedTraversal`, +`TestParseDepString_FQDNHostGate`.
- `internal/marketplace/models.go`: `pythonRepr` replaced by `pythonReprValue`/`pythonReprNumber`/`pythonReprString` + `decodeOrderedJSON`/`decodeOrderedValue` (+`orderedPair`/`orderedValues` types) + `printableASCIIText`; `tagPatternStructuralError` takes `json.RawMessage` instead of a pre-decoded `any` and applies `printableASCIIText` to its composed message; `parsePluginEntry` extracts `tagPatternRaw` from a fresh `map[string]json.RawMessage` decode of `sourceRaw` instead of reading `srcMap["tag_pattern"]`.
- `internal/marketplace/models_manifest_test.go`: +4 `TestMarketplaceManifest_StructuralErrors` cases (the evaluator's 3 reproducers plus a multi-key dict variant), +`TestPythonReprValue` (23 cases), +`TestPrintableASCIIText` (5 cases).
- `cmd/apm-go/marketplace_e2e_test.go`: `TestMarketplaceValidate_TagPatternDeferral` rewritten to invoke the real `marketplace add`/`search`/`install` commands instead of `marketplace.AddSource` directly.

## Attempt 5: five more reproducers, then conformance tables to end the loop

`.review/eval-ticket-11.md`'s attempt-4 ruling found FIVE new reproducers
in the same two files attempt 4 had just rewritten. The orchestrator's
framing for this attempt was explicit: stop fixing reproducers one at a
time and build a table generated directly from the Oracle instead, so the
NEXT gap (there will always be a next gap in a ~2000-line multi-form
parser) shows up as a fixture-regeneration diff, not another eval round.

### Part 1: the five reproducers

1. **Arbitrary SSH user, both ssh:// and SCP.** `_parse_ssh_protocol_url`
   (reference.py:558-571) accepts any user matching `validate_ssh_user`,
   defaulting to `"git"` only when userinfo is absent; SCP shorthand's
   `SCP_LIKE_RE` (cache/url_normalize.py) is the same story. apm-go's
   `parseSSHURL`/`parseSCPURL` required the literal `"git@"`/`"ssh://git@"`
   prefix. Fixed: `ParseDepString` now dispatches on a bare `"ssh://"`
   prefix and on `scpLikeRe` (SCP_LIKE_RE ported verbatim) instead of a
   hardcoded user; both parsers extract and `validateSSHUser` the actual
   userinfo (github_host.py's `_SSH_USER_RE`/`_SSH_USER_MAX_LEN` ported
   verbatim), stored on a new `DependencyReference.SSHUser` field (empty
   for the implicit `"git"` default, so every existing caller/serialization
   stays byte-identical) -- NOT yet wired into the actual git-clone URL
   builder (`internal/gitops/clone.go` hardcodes `"git@"`), which is a
   materialize-time concern the doc comment marks as a separate, later
   scope.
2. **URL query strings, and shorthand host:port.** `_parse_standard_url`/
   `_parse_ssh_protocol_url` both parse via `urllib.parse.urlparse`, which
   structurally separates `?query` from the path for free; apm-go's manual
   string-splitting treated `repo?x` as one repo segment. Fixed: a new
   `stripQuery` helper cuts the path at the first `?`, applied in
   `parseHTTPURL` and `parseSSHURL` (NOT `parseShorthand` or `parseSCPURL`
   -- confirmed the Oracle doesn't apply it there either; a bare
   `owner/repo?x` shorthand stays rejected, `_parse_ssh_url` has no
   `urlparse` call at all). Separately, `identity.py`'s
   `_split_shorthand_host_port` splits an optional `":port"` suffix off
   shorthand's leading segment BEFORE it is checked for host-shape at all
   (`"x.io:443/owner/repo"` -> host `"x.io"`, port 443 normalized away as
   the HTTPS default) -- ported as `splitShorthandHostPort`, called
   unconditionally at the top of `parseShorthand` exactly where the Oracle
   calls it, including its "even a segment that turns out to be a plain
   owner still gets port-shape-validated" quirk (`owner:1234/repo` is
   accepted with the port silently discarded, matching a direct probe).
3. **Lone UTF-16 surrogate repr.** A Python str can hold an unpaired
   `\uXXXX` surrogate; `repr('\ud800')` is `'\ud800'`. Go strings must be
   valid UTF-8, so `encoding/json`'s own string decoder replaces a lone
   surrogate with U+FFFD before any caller can intervene. Fixed: replaced
   `decodeOrderedJSON`'s `encoding/json`-based token walk with a hand-
   rolled recursive-descent JSON parser (`jsonScanner`) whose string
   decoder combines a valid UTF-16 surrogate PAIR into one astral rune (the
   ordinary case) but keeps a lone surrogate as a bare `rune` value in
   0xD800-0xDFFF (`pyStr`, a `[]rune`, not a Go `string`) -- unreachable via
   `encoding/json.Decoder.Token()` no matter how the caller post-processes
   its result, since the normalization already happened inside the
   tokenizer.
4. **Numeric overflow and negative zero.** `float("1e400")` is `inf` in
   Python; `strconv.ParseFloat`/`json.Number.Float64` returns an
   out-of-range error apm-go's old `pythonReprNumber` treated as "fall back
   to the original lexeme", producing `got 1e400` instead of `got inf`.
   Separately, `int("-0")` is `0`, but a JSON integer lexeme `-0` is not.
   Fixed: `pythonReprNumber` now uses `strconv.ParseFloat` directly and
   USES its returned value even on `ErrRange` (verified directly: Go's
   `ParseFloat` still returns the correctly saturated `+Inf`/`-Inf`), and
   special-cases an all-zero integer lexeme (with or without a leading
   `-`) to `"0"`. A float lexeme's own negative zero (`-0.0`) is unaffected
   and correctly stays `"-0.0"` (Python: `repr(-0.0) == '-0.0'`, distinct
   from `repr(int('-0')) == '0'` -- verified both are already right without
   further special-casing, since `strconv.FormatFloat(-0.0, ...)` already
   emits `"-0"`).
5. **Duplicate JSON object keys.** `json.loads('{"a":1,"b":2,"a":3}')`
   builds its dict via repeated `d[key] = value`: first key POSITION, last
   VALUE. The `jsonScanner` rewrite (item 3 above) also fixed this as part
   of replacing `decodeOrderedJSON`'s object-decoding loop: `parseObject`
   tracks each key's index and overwrites `pairs[i].val` on a repeat
   instead of appending a second pair.

All five verified byte-for-byte against the pinned Oracle directly before
being encoded as new `TestMarketplaceManifest_StructuralErrors` cases
(marketplace-level) and `TestPythonReprValue`/`TestParseDepString_*`
cases (unit-level).

**A sixth fix, found by the conformance sweep itself, not the eval:**
`owner/../repo` (and its percent-encoded form `owner/%2e%2e/repo`) parsed
successfully as `Repo=".."` `VirtualPath="repo"` instead of being rejected
-- `ownerCharRe`/`repoCharRe`'s character class (`[A-Za-z0-9._-]+`) and
`validateVirtualPath`'s `segmentRe` both happen to accept a literal `".."`
segment (it consists only of allowed `.` characters), and nothing else
checked for the exact value. The Oracle's `validate_path_segments`
(path_security.py:32-77) rejects any `"."`/`".."` segment in a repo_url or
virtual path outright (`PathTraversalError`), confirmed directly:
`DependencyReference.parse("owner/../repo")` raises. Fixed: a new
`isDotSegment` helper, checked alongside the existing char-class check at
all four owner/repo validation call sites and inside
`validateVirtualPath`'s per-segment loop.

**A genuine, pre-existing, INTENTIONAL divergence the conformance sweep
also surfaced and did NOT "fix":** the Oracle's `is_local_path` branch
(reference.py:1754-1768) performs no traversal check at all -- ANY
relative path whose basename isn't `"."`/`".."` is accepted as a local
dependency, so `DependencyReference.parse("../../../etc/passwd")`
succeeds with `is_local=True` on the Oracle. apm-go's `containsEscape`
(depref.go, pre-dating this ticket) is a deliberate security hardening
beyond the Oracle that rejects this outright. Matching the Oracle here
would be a security regression, not a parity fix -- this row is recorded
in the conformance fixture as a `known_gap` with that reasoning, not
silently passed over.

### Part 2: the conformance tables

- `tools/depref_conformance_gen.py`: a one-shot generator, run via
  `uv run --project <oracle> python3 tools/depref_conformance_gen.py
  --oracle-src <oracle>/src --out-accept spec/conformance/depref-accept.json
  --out-repr spec/conformance/python-repr.json`. `DEPREF_INPUTS` enumerates
  every accept/reject branch reachable from a marketplace source
  coordinate: shorthand (bare/ref/virtual-path/host-qualified/host:port),
  https/http/ssh/SCP (users, ports, queries/fragments), percent-encoding
  (valid/invalid escapes, traversal, control chars), local paths
  (relative/absolute/home/UNC/Windows-drive), empty/whitespace, IPv6/IP-
  literal/odd hosts, and Azure-DevOps/GitLab-nested-group/Artifactory
  shapes the Oracle supports but apm-go's shorthand parser does not
  attempt (each such row carries a `known_gap` string explaining why, so
  the gap is a recorded finding, not a silent one -- AGENTS.md: "a
  deviation that is not recorded there is a finding, not a feature").
  `PYTHON_REPR_INPUTS` covers every JSON value shape `pythonReprValue` is
  meant to reproduce Python's `repr()` for: scalars, quote/escape
  selection (including surrogate pairs, lone surrogates, non-printable
  non-ASCII), int-vs-float/overflow/negative-zero, nested lists/dicts, and
  duplicate keys. `python-repr.json`'s repr values come from CPython's OWN
  builtin `repr()`; `depref-accept.json`'s come from the pinned Oracle's
  own `DependencyReference.parse`, never re-derived.
- `spec/conformance/depref-accept.json` (81 rows, 6 `known_gap`),
  `spec/conformance/python-repr.json` (42 rows): committed fixtures
  (runtime inputs tracked in git, not generated at test time -- same
  pattern as `internal/marketplace/build`'s schema-sync tests, AGENTS.md).
- `internal/manifest/depref_conformance_test.go`
  (`TestParseDepString_OracleConformance`) and
  `internal/marketplace/python_repr_conformance_test.go`
  (`TestPythonReprValue_OracleConformance`): assert every non-`known_gap`
  row's `accepted`/`is_local` (dep-string) or `repr` (Python-repr) output
  matches the fixture; a `known_gap` row is logged, not asserted, so a
  future accidental narrowing OR widening of a documented gap surfaces the
  next time the fixture is regenerated and re-diffed, not silently.
- Comment corrections (the eval's "Standards: FAIL -- comments claim an
  exact port but omit accepted grammar and JSON-representation cases"):
  `isValidRemoteCoordinate`'s doc comment no longer claims
  `manifest.ParseDepString` is "a full port of this exact grammar" or "the
  one remaining explicitly documented partial-parity boundary" -- both
  false after four rounds of finding more accepted-Oracle-grammar gaps.
  `ParseDepString` itself gained a doc comment (it had none) stating its
  true scope and pointing at the conformance table as the actual, living
  scope statement instead of a historical claim. `tagPatternStructuralError`
  now also points at `TestPythonReprValue_OracleConformance` for the same
  reason.
- `TestMarketplaceValidate_TagPatternDeferral`'s doc comment corrected: its
  install assertion calls the internal `runInstall` function (mocked,
  network-free), not the root Cobra `install` command -- `installCmd()`'s
  RunE hardcodes `gitops.RealTagLister`/`RealPackageLoader` with no seam to
  inject the same mocks, and the fixture's plugin sources aren't real
  repositories, so routing through the real command would trade a fast
  hermetic test for a flaky network-dependent one. Not changed to use the
  real command; the comment now says so instead of overclaiming.

**Verification.** All five reproducers plus the sixth (dot-segment
traversal) fix verified directly against the pinned Oracle. Fresh
full-corpus evidence from the clean committed tree, five-point provenance
verified, diffed against `/tmp/ticket11a4-evidence-6aaa47c-review` (the
orchestrator's own attempt-4 baseline): zero `(fields, waived)` regression
across all 49 cases. `go build`/`vet`/`gofmt` clean; `go test ./...` green
(pre-existing Windows-only `TestParseDepString_AbsolutePath` excepted);
`go test -race ./cmd/apm-go/... ./internal/marketplace/...
./internal/manifest/...` clean.

## Files touched (attempt 5 additions)
- `internal/manifest/depref.go`: `DependencyReference.SSHUser` (new field); `scpLikeRe`/`sshUserRe`/`shorthandPortRe` (new); `validateSSHUser`, `stripQuery`, `splitShorthandHostPort`, `isDotSegment` (new); `ParseDepString` dispatches on bare `"ssh://"` and `scpLikeRe` instead of hardcoded `"git@"`; `parseHTTPURL`/`parseSSHURL` call `stripQuery`; `parseSSHURL`/`parseSCPURL` extract and validate an arbitrary user; `parseShorthand` calls `splitShorthandHostPort` unconditionally on its leading segment; all four owner/repo validation call sites and `validateVirtualPath` additionally reject `isDotSegment`; `ParseDepString` gained a doc comment stating its true (approximate, conformance-table-tracked) scope.
- `internal/marketplace/models.go`: `decodeOrderedJSON`/`decodeOrderedValue` (json.Decoder-based) replaced by `jsonScanner` (a hand-rolled recursive-descent JSON parser) + `pyStr`/`jsonNumber` leaf types, preserving lone UTF-16 surrogates and implementing Python's first-key-position/last-value duplicate-key semantics; `pythonReprString` renamed `pythonReprPyStr` (operates on `pyStr`, not `string`) plus `pyStrTrimSpace`; `pythonReprNumber` uses `strconv.ParseFloat` directly with `ErrRange` handled as "use the saturated Inf" and an all-zero-lexeme special case; `pythonFloatRepr` adds `math.IsInf`/`IsNaN` handling; `tagPatternStructuralError` decodes via `decodeOrderedJSON` throughout (including its own "is this a string" check) instead of `encoding/json.Unmarshal`; `isValidRemoteCoordinate`'s and `tagPatternStructuralError`'s doc comments corrected to point at the conformance tables instead of claiming a fixed, now-false scope.
- `internal/marketplace/models_manifest_test.go`: +6 `TestMarketplaceManifest_StructuralErrors` cases (the five reproducers plus the SCP-form variant of reproducer 1), +6 `TestPythonReprValue` cases (surrogate, +inf/-inf, integer -0, float -0.0, duplicate keys).
- `internal/manifest/depref_conformance_test.go` (new), `internal/marketplace/python_repr_conformance_test.go` (new): the conformance test suite, see "Part 2" above.
- `tools/depref_conformance_gen.py` (new), `spec/conformance/depref-accept.json` (new), `spec/conformance/python-repr.json` (new): the conformance fixtures, see "Part 2" above.
- `cmd/apm-go/marketplace_e2e_test.go`: `TestMarketplaceValidate_TagPatternDeferral`'s doc comment corrected (install-command claim).

## Attempt 6 (orchestrator-completed, 2026-08-24)

Per the user's protocol correction, the orchestrator took over after attempt 5's FAIL (the intervention rule means hands-on implementation after 3 failures, not more spec-writing). The implementor's in-flight attempt-6 work was interrupted, reviewed, KEPT (it was correct and well-researched -- including the discovery that `SSH://` stays case-sensitive in the Oracle while `HTTPS://`/`HTTP://` fold), and completed by the orchestrator:

- Implementor (kept): case-insensitive https/http scheme dispatch (`hasFoldPrefix`, reference.py:1626); `pyStr` dict KEYS end-to-end with `pyStrHashKey` for duplicate detection (lone surrogates in keys no longer collapse to U+FFFD); `pyIsSpace` porting Python's `str.isspace()` (Go's `unicode.IsSpace` + U+001C-U+001F); generator gained the Oracle pin embedding, mixed-case-scheme/surrogate-key/whitespace-sweep case classes, and `apmgo_accepted`/`apmgo_is_local` known-gap locking; `depref_conformance_test.go` asserts the pin and both directions of every known gap; both fixtures regenerated (88 depref rows, 46 repr rows + 276-code-point isspace sweep).
- Orchestrator (completed): `python_repr_conformance_test.go` updated to the new doc shape -- pin assertion (same tools/parity/oracle.pin rule as the depref test) and the per-code-point `pyIsSpace` sweep assertion.

Orchestrator verification (all first-hand, not relayed):
- Fixture honesty: regenerated both fixtures against the pinned Oracle with the checked-in generator -- byte-identical to the committed files.
- All three attempt-5 reproducers byte-identical across sides (modulo the pre-waived rich word-wrap): `HTTPS://x.io/owner/repo` -> Structure passed/exit 0 both sides; tag_pattern {"\ud800":1} -> got {'\ud800': 1} exit 1 both sides; tag_pattern "\u001c" -> must be a non-empty string, got '\x1c' exit 1 both sides.
- `go test ./...` green (Windows-only exception), `-race` clean on internal/manifest, internal/marketplace, cmd/apm-go; gofmt/vet clean.

## Attempt 7 (orchestrator-implemented, 2026-08-24)

Attempt 6's ruling left four reproducers, all one root cause: the URL forms were hand-split instead of following urlsplit's netloc/path model. Orchestrator implemented directly (intervention protocol):

- New `netlocHostPort` (urlsplit semantics): userinfo dropped after the LAST '@', hostname lowercased, empty port absent, digits-only port 0-65535 (port ZERO valid -- unlike shorthand's parseHostPort, whose grammar the Oracle also rejects port 0 in: conformance rows url-port-zero vs shorthand-port-zero).
- `parseHTTPURL`: path.strip('/') both ends (leading '//' and trailing '/' collapse), terminal '.git' off the raw path pre-split, and every part percent-unquoted AGAIN on top of the entry-level whole-string unquote (reference.py:1748 then :1502 -- a genuine double decode, row url-double-encoded).
- `parseSSHURL`: path.lstrip('/') LEFT-only + reject_empty (internal '//' and trailing '/' rejected, leading '//' collapses) -- deliberately asymmetric with https, matching _parse_ssh_protocol_url + validate_path_segments(reject_empty=True).
- `utf8ReplaceInvalid` now implements Python bytes.decode('utf-8','replace')'s MAXIMAL-SUBPART rule (one U+FFFD per maximal ill-formed subsequence; new `maximalSubpartLen`): '%e0%a0' decodes to ONE replacement, not two.
- Ref fragments now mirror reference.py:580-583 exactly: whitespace-stripped, empty means absent, NO parse-time charset gate (the old VCHAR-only `refRe` was removed -- probed the Oracle accepting '#-evil'/'#a b'/'#v1..2', and apm-go's git calls are already argument-injection-safe: gitops/clone.go passes the ref only as `--branch <ref>`'s value with `--` before positionals).
- Generator: +20 rows locking the four classes and their corners (userinfo, double slashes both forms, ports 0/empty/65536 across url/ssh/shorthand, uppercase host, double-encoding, truncated-UTF-8 percent classes); fixtures regenerated from the pinned Oracle (108 depref rows).

Orchestrator verification, all first-hand: 108-row conformance test passes; all four attempt-6 reproducers MATCH side-by-side at the marketplace layer (add+validate, isolated HOMEs: userinfo/double-slash/port-0/truncated-utf8 all 'Summary: 3 passed' exit 0 on both sides); go test ./... green (Windows-only exception), -race clean on manifest/marketplace/cmd, gofmt/vet clean.

## Attempt 8 (orchestrator-implemented, 2026-08-24)

Attempt 7's ruling accepted the four URL/percent fixes and the refRe removal, leaving one defect: parseSSHURL still extracted the user at the FIRST '@' and validated the raw userinfo whole. Fixed to the Oracle's single-urlsplit model (reference.py:558-575): userinfo = netloc before the LAST '@' (so 'one@two@host' validates 'one@two' and is rejected), username = userinfo up to the FIRST ':' (a ':password' is split off and ignored: 'alice:pw@host' validates 'alice'), and an EMPTY username falls back to the default 'git' with no validation ('ssh://@host/...' and 'ssh://:pw@host/...' parse). Six new conformance rows lock the class (double-at x2, password x2, empty x2); fixtures regenerated from the pinned Oracle (114 depref rows). Verified first-hand: conformance tests pass, full suite green (Windows-only exception), -race clean, gofmt clean.
