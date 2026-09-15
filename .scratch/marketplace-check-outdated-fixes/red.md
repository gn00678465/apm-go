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
