# RED observations — marketplace-check-outdated-fixes

Each row is a run observed before the corresponding GREEN commit. Commands ran with `GOPROXY=off`.

## Baseline

- source: isolated worktree at 674ec98 (SPEC v1 approved; product code identical to 3d59291)
- command: `go test ./cmd/apm-go/... ./internal/marketplace/... ./internal/semver/...`
- result: exit 0 — `cmd/apm-go` ok 114.2s, `internal/marketplace` ok 22.7s, `authoring` ok 66.0s, `build` ok 31.5s, `tagpattern` ok 1.0s, `semver` ok 1.1s. No pre-existing failure. `TestDoctor_ExecGit_TimesOutDespiteOrphanedGrandchildHoldingPipesOpen`, reported failing by the after-spec squad in its copy, passed here.

## Group A — SHA match (RED commit 4378e23, GREEN e02bf35)

- command (worktree at 4378e23): `go test ./internal/marketplace/authoring/ -run 'TestCheckPackages_ShaPin_RefNamedLikeSha_DifferentCommit_Probes|TestCheckPackages_NonShaRef_StillMatchesByName' -v`
- SC-F1 main: FAIL — `probe calls = 0, want 1`; result `Err:<nil> RefOK:true`.
- SC-F1 WithVersion: FAIL — `ref "0123…4567" vanished from … while reading its plugin manifest`, `Reachable:false`.
- SC-F1 Exists: FAIL — `probe calls = 0, want 1`.
- SC-F2 (regression): PASS, all five subtests.
- after GREEN e02bf35: `go test ./internal/marketplace/authoring/` ok 48.6s.

## Group B — manifest read cap

- command (worktree, tests added on top of e02bf35): `go test -count=1 -run 'TestReadCapped|TestNewShowCmd' ./internal/marketplace/authoring/`
- SC-F3, SC-F16: build failed — `undefined: readCapped` (fixes_read_capped_test.go:47), `undefined: newShowCmd` (:119).
- SC-F4 and SC-F5 observed separately in a throwaway worktree at e02bf35 with only those test bodies added:
  - SC-F5: PASS 0.73s (regression, as planned).
  - SC-F4 at 8× cap: PASS 2.00s. Retried at 64× cap per the SPEC note: PASS 6.22s. The pre-fix read finishes the whole file inside the 3s deadline and still reports `exceeds`, so SC-F4 cannot show RED. Relabelled regression per the SPEC note; RED for the cap is SC-F3.
- GREEN 2d4671b: `go test -count=1 ./internal/marketplace/authoring/` ok 56.5s.

## Group C — outdated Current and leading v

- command (worktree, tests added on top of 2d4671b): `go test -count=1 -v -run 'TestOutdatedPackages_ShaPinWithVersion_NoCandidates_CurrentDashes|TestOutdatedPackages_ShaPinWithoutVersion_ErrorRows_CurrentDashes|TestOutdatedPackages_ShaPinWithVersion_LeadingV_SameAsBare' ./internal/marketplace/authoring/`
- SC-F6 NoTags, Offline, ListRefsError: FAIL (Current is the 40-hex SHA from the current map).
- SC-F6 RangeEntryKeepsMap (regression file): PASS.
- SC-F17 Offline, ListRefsError, NoHEAD: FAIL (Current is the SHA).
- SC-F8 NewerTag: FAIL `Current:"vv1.0.0" LatestInRange:"--"`; UpToDate: FAIL `Current:"vv1.0.0" LatestInRange:"--"`; NamePattern: FAIL `Current:"tool_vv1.0.0" LatestInRange:"--"`. LatestOverall, Status, Upgradable already matched the bare version.
- SC-F7 (`go test -count=1 -v -run TestMarketplaceOutdated_ShaPin_NoTags_MarketplaceJson_OmitsSha ./cmd/apm-go/`): FAIL, the table row contains the SHA.
- GREEN 8d0e288: `go test -count=1 ./internal/marketplace/authoring/` ok 52.6s; `go test -count=1 -run 'Outdated|Check' ./cmd/apm-go/` ok 17.2s.

## Group D — name and source type

- command (worktree, tests added on top of 8d0e288): `go test -count=1 -v -run 'TestLoadAuthoringConfig_NonStringName_Rejected|TestLoadAuthoringConfig_NonStringSource_OracleMessage|TestLoadAuthoringConfig_StringLikeScalars_Accepted|TestLoadAuthoringConfig_NumericVersionOrRef_Accepted' ./internal/marketplace/authoring/`
- SC-F9 Int, Bool, Float, OctalInt: FAIL — `LoadAuthoringConfig accepted an invalid config`.
- SC-F9 Sequence (`name: [a]`): PASS before the fix. The existing ScalarNode check already rejected it with the same message. Listed under SC-F9 in the SPEC; it is a regression subtest, not RED.
- SC-F18 Int: FAIL — message was `marketplace source "123" must be one of ...`; Sequence and Mapping: FAIL — message was `marketplace source is empty`.
- SC-F19 (5 subtests) and SC-F10: PASS (regression).
- GREEN 889cb92: `go test -count=1 ./internal/marketplace/authoring/` ok 49.3s; `go test -count=1 ./cmd/apm-go/... ./internal/marketplace/build/... ./internal/pack/...` all ok.

## Group E — version grammar

- oracle table check: `D:/Projects2/apm` at 8c2e0d9c, `git diff --quiet b75a02b1 HEAD -- src/apm_cli/marketplace/semver.py` → identical. `parse_semver` via importlib (with `sys.modules` registration, `PYTHONIOENCODING=utf-8`) returned the SC-F11 expectations for all 15 rows, including True for `'\u0661.\u0662.\u0663'` and `'1.2.3\n'` (the D-b deviation rows).
- SC-F11 (`go test -count=1 ./internal/marketplace/tagpattern/`): build failed — `undefined: IsOracleVersion` (oracle_version_test.go:40).
- command (worktree, tests added on top of 889cb92): `go test -count=1 -v -run 'TestCheckPackages_TagInference_RejectsShortVersions|TestVersionTagCandidates_VPrefixedCapture' ./internal/marketplace/authoring/`
- SC-F12: FAIL — check did not pass (inference picked `{version}` from tag `1`).
- SC-F13 CaptureRejected: FAIL — candidates `1.0.0` and `v1.2.0` (version `v1.2.0`) via `{version}`.
- SC-F13 FallbackInfers: FAIL — `[{tag:v1.2.0 version:v1.2.0}] via "{version}"`. The SPEC labelled this subtest regression; it is RED. Label corrected in SPEC Revisions and the subtest placed in the RED file.
- GREEN f7cd029: `go vet ./internal/marketplace/... ./internal/semver/...` clean; `go test -count=1 ./internal/marketplace/... ./internal/semver/... ./cmd/apm-go/...` all ok.

## SC-F15 — realexec step

- scenario: apm.yml with `name: 123`, `source: owner/repo`, `ref: main`; `marketplace check --offline`.
- binary built from 8d0e288 (before GREEN D): printed the table with `No cached refs (offline)` and ` x 1 entries have issues`, exit 1 — the new step (expects exit 2) would fail.
- binary built from f7cd029: ` x marketplace config error: 'packages[0].name' must be a non-empty string`, exit 2.
