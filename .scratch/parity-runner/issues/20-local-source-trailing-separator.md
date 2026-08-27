# 20 — `marketplace package add ./<path>\` silently writes a broken package name/source

**What to build:** a local (`./…`) package source must be verified to exist at `add` time, and a package name must not contain a path separator. Today a trailing `\` (a Windows/PowerShell tab-completion artifact) rides through `add` → `apm.yml` → `pack` → `marketplace.json` and produces a plugin Claude Code cannot load.

**Blocked by:** none. **Status:** CLOSED (2026-08-28) (user-reported, 2026-08-25).

**Origin:** user bug report — "使用 `apm-go marketplace package add ./<local_path>\` 會寫入結尾的 `\`, 然後使用 `apm-go pack` 所產生的 marketplace 設定會出現 `llm-wiki\`".

## Reproducer (verified at `c040519`, v0.3.0-beta.3)

```sh
$ mkdir -p llm-wiki                      # note: "llm-wiki", NOT "llm-wiki\"
$ apm-go marketplace package add './llm-wiki\'
 + Added package "llm-wiki\\" from ./llm-wiki\
$ apm-go pack
[*] Built marketplace.json [claude] (1 package(s)) -> .claude-plugin/marketplace.json
$ cat .claude-plugin/marketplace.json
{
  ...
  "plugins": [
    { "name": "llm-wiki\\", "source": "./llm-wiki\\" }
  ]
}
$ apm-go marketplace check
│ +  │ llm-wiki\ │ +  │ +  │ +  │ OK │      # ← claims REACHABLE for a path that does not exist
 + all 1 package(s) verified
```

`./totally-missing` (a plain nonexistent path, no backslash) behaves the same: `add` succeeds, exit 0.

## This is NOT a parity bug — it is a shared upstream gap

Checked against the pinned Oracle (`c8d6cdec596e773a84b0839c33c28b6b0a217637`); apm-go reproduces it **exactly**, at every stage:

| stage | Oracle | apm-go | verdict |
|---|---|---|---|
| source shape filter | `SOURCE_RE` accepts the bare alternative `\./.*` — anything after `./` (`marketplace/yml_schema.py:101-108`) | `manifest.ValidateMarketplaceSource` | same |
| path guard | `validate_path_segments(source, allow_current_dir=True)`: `path_str.replace("\\", "/").split("/")` → segments `.`, `llm-wiki`, `""`; `reject_empty` is **false** here, so the empty tail passes (`utils/path_security.py:64-82`) | `resolveLocalSourceAgainstRoot` (`internal/marketplace/authoring/refcheck.go:305-321`) — containment only, never touches the filesystem | same |
| name derivation | `_default_name_from_source`: `rstrip("/")` (leaves `\`), strip `.git`, `rsplit("/", 1)[-1]` → `llm-wiki\` (`marketplace/yml_editor.py:138-143`) | `defaultNameFromSource` (`internal/marketplace/authoring/editor.go:614-621`) → `llm-wiki\` | same |
| name charset | none — `PackageEntry.name` is only required to be a non-empty string (`yml_schema.py:216`) | none | same |
| `pack` | `_resolve_entry` short-circuits `entry.is_local` with no `stat` (`marketplace/builder.py:613-628`) | same | same |
| `check` | `entry.is_local` → `_CheckResult(reachable=True, version_found=True, ref_ok=True)` unconditionally (`commands/marketplace/check.py:121-134`) | same | same |

So closing this is a **deliberate deviation** (apm-go stricter than the Oracle, never looser) — the same class as the existing `containsEscape` local-path hardening already recorded as a `known_gap`. Per AGENTS.md it must carry a comment at each deviation site citing the Oracle file:line it departs from and why.

Two things checked and found **not** to be bugs, so they are out of scope:

- apm-go does not enforce the Oracle's "at least one of `version` or `ref`" rule (`yml_editor.py:185-186`). That is an **already-documented intentional deviation** — see `AddPackage`'s doc comment (`editor.go:694-708`): prd.md AC3 requires `package add ./pkgs/tool` with zero flags to succeed. Do not "fix" it.
- `add`'s success line differs in wording from the Oracle's (`[+] Added package 'x' from y` vs apm-go's ` + Added package "x" from y` — glyph, quote style, and Go `%q` backslash escaping). That belongs to the ticket-13/14 output-surface family, not here. Record it, do not fix it in this ticket.

## Acceptance criteria

- [x] AC1 — `marketplace package add ./<path>` fails when the resolved path does not exist, or exists but is not a directory: exit 2, a clear error naming the path, and **nothing written to `apm.yml`** (verify the file is byte-identical after the failed run).
- [x] AC2 — `--no-verify` skips the new existence check (it already means "skip the reachability check"). The containment/traversal guard in `resolveLocalSourceAgainstRoot` still runs unconditionally, with or without `--no-verify` — do not move it behind the flag.
- [x] AC3 — a package name containing a path separator (`/` or `\`), or equal to `.` or `..`, or containing whitespace or a control character, is rejected — whether it came from `--name` or from `defaultNameFromSource`. Keep the guard to clearly-broken names only: do **not** apply `init`'s strict `pluginNameRe` (`^[a-z][a-z0-9-]{0,63}$`), which would reject legitimate existing names like `My_Tool`.
- [x] AC4 — `marketplace check` no longer reports `REACHABLE +` for a local entry whose path is missing; it reports a failure row with a detail naming the path, and the command's exit code reflects the failure the same way a remote failure does.
- [x] AC5 — each of AC1/AC3/AC4 carries a comment at the deviation site naming the Oracle file:line it departs from (from the table above) and the reason, per AGENTS.md's deviation rule.
- [x] AC6 — Go tests for every AC, using `t.TempDir()` per the project's testing pattern. No parity-case regression: the `tools/parity` corpus stays tuple-identical (`(fields, waived)`) to the pre-change baseline. Note there are currently **no** `marketplace package add` cases in `tools/parity/cases/`, so this should be a no-op there — confirm rather than assume.
- [x] AC7 — the deviation is recorded where the project records deviations (the `known_gap`/waiver corpus), so the parity gate does not later read it as an unexplained divergence.

## Implementation

- `internal/marketplace/authoring/editor.go`:
  - `verifyPackageSource` now, for a local source: always runs `resolveLocalSourceAgainstRoot` (containment, unconditional — AC2), then — unless `--no-verify` — `verifyLocalSourceExists` (AC1). Doc comment cites the Oracle table above (AC5).
  - New `verifyLocalSourceExists(source, resolvedPath string) error`: `os.Stat`, rejecting a missing path or a non-directory.
  - New `validatePackageName(name string) error` (AC3): rejects `/`, `\`, `.`, `..`, whitespace, control characters; deliberately narrower than `init`'s `pluginNameRe`. Wired into `AddPackage` right after `name` is resolved (covers both `--name` and `defaultNameFromSource`).
- `internal/marketplace/authoring/refcheck.go`:
  - `CheckPackages`/`checkPackage` gained a `dir` parameter (the project root, mirroring `AddPackage`'s own parameter) so `check`'s local branch can re-run the same `resolveLocalSourceAgainstRoot` + `verifyLocalSourceExists` pair `add` runs, instead of unconditionally returning `pass` (AC4). Doc comments cite `commands/marketplace/check.py:121-134` (AC5).
- `cmd/apm-go/marketplace_authoring.go`: `CheckPackages` call site now passes `"."`, matching the existing `LoadAuthoringConfig(".")` call.

AC7: recorded via AC5's code comments (editor.go/refcheck.go), each citing the specific Oracle file:line the deviation departs from and the reason — this repo's established deviation-recording convention per `AGENTS.md`'s "Oracle parity" section ("Where apm-go deviates on purpose... say so in a comment at the deviation with the reason"). No `tools/parity/waivers.json` entry was added: AC6 confirmed there are zero `marketplace package add`/`check` cases in the corpus for one to attach to; a future ticket that adds such cases will find the deviation already documented at the code site instead of hitting an unexplained divergence.

## Tests

New (all in `internal/marketplace/authoring/editor_test.go` unless noted):
- AC1: `TestAddPackage_LocalSource_NonexistentPath_Rejected`, `TestAddPackage_LocalSource_TrailingSeparator_Rejected` (ticket's exact `./llm-wiki\` reproducer), `TestAddPackage_LocalSource_ExistsButIsAFile_Rejected`; CLI end-to-end in `cmd/apm-go/marketplace_package_test.go`: `TestMarketplacePackageAdd_LocalSourceTrailingSeparator_RejectedEndToEnd` (asserts exit 2 + byte-identical apm.yml), `TestMarketplacePackageAdd_NonexistentLocalSource_RejectedEndToEnd`.
- AC2: `TestAddPackage_LocalSource_NoVerify_SkipsExistenceCheck`, `TestAddPackage_LocalSource_NoVerify_ContainmentGuardStillRuns`.
- AC3: `TestValidatePackageName_RejectsBrokenNames`, `TestValidatePackageName_AcceptsLegitimateNames` (locks the non-goal: `My_Tool` etc. still accepted), `TestAddPackage_NameFlag_TrailingSeparator_Rejected`.
- AC4 (in `internal/marketplace/authoring/refcheck_test.go`): `TestCheckPackages_LocalSource_MissingPath_FailsCheck`, `TestCheckPackages_LocalSource_ExistsButIsAFile_FailsCheck`, `TestCheckPackages_LocalSource_EscapesRoot_FailsCheck` (check now also re-runs the containment guard, not just existence); CLI end-to-end in `cmd/apm-go/marketplace_authoring_test.go`: `TestMarketplaceCheck_LocalPackageMissingPath_FailsWithDetail`.

Updated: every pre-existing test that constructed a local (`./...`) source without a real fixture directory now creates one via `os.MkdirAll` (`AddPackage`'s existence check would otherwise reject them for the wrong reason) — 9 tests in `editor_test.go`, 13 in `cmd/apm-go/marketplace_package_test.go` and `marketplace_authoring_test.go` combined, plus every `CheckPackages` call site in `refcheck_test.go` gained the new `dir` parameter. `TestAddPackage_DuplicateNameCaseInsensitive_Errors`/`TestMarketplacePackageAdd_DuplicateName_ExitsCode2`/`TestEditPackagesFile_ForcedValidationFailure_LeavesFileByteExactUnchanged` also gained an explicit error-message assertion so they keep testing what their names claim now that a nonexistent path would otherwise fail first, for the wrong reason.

## Evidence

- `go build ./...`: clean.
- `go test ./...`: all packages `ok`, zero failures.
- `go test ./cmd/apm-go/... ./internal/marketplace/authoring/... -race -count=1`: both `ok`, no data races.
- Parity corpus: built the pre-fix (stashed) and post-fix binaries, ran the full `tools/parity` corpus (69 cases) against the pinned Oracle (`c8d6cdec596e773a84b0839c33c28b6b0a217637`) for each; both runs report the same pre-existing "18 of 69 case(s) have unwaived diffs". A tuple-level `(fields, waived)` diff across all 69 case ids shows **zero changes** — confirming AC6's "should be a no-op" prediction empirically rather than assuming it. (Confirms the ticket's own note: no `marketplace package add`/`check` cases exist in the corpus at all.)

## Non-goals

- Normalising (rewriting) the stored `source` string. `./llm-wiki/` with a trailing `/` is Oracle-faithful, resolves correctly, and must keep round-tripping verbatim — AC1 rejects the *nonexistent* path, it does not rewrite the user's input.
- Adopting `init`'s strict plugin-name charset for marketplace package names (see AC3).
- The `add` success-line wording divergence (ticket 13/14 family).
