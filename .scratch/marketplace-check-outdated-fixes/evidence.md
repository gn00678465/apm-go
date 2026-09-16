# Evidence Report — marketplace check / outdated review 缺陷修正 (Tier 3)

- `headline`: GATE PASSED
- `command`: `evidence`
- `contract`: applied
- `scope`: marketplace-check-outdated-fixes
- `change_set`: 3d59291...HEAD（3d59291...3a1cedd；產品 Go 碼最後變更於 fb76028，之後只有 realexec 步驟、SPEC／ARCHITECTURE 文字與 .scratch 紀錄）
- `base`: fix/markteplace-outdated @ 3d59291（前一 scope 封存 commit）
- `report_language`: zh-TW
- `intent_status`: confirmed（v1、v2）；v2 為事後核准（2026-09-17，晚於實作與本報告首版）
- `intent_source`: specs/marketplace-check-outdated-fixes/SPEC.md，`spec_version: v2`，status approved。Approval 段：v1「核准 v1」（AskUserQuestion，commit 5760b0c，記於 674ec98）；v2 核准問題當時的回覆原話「用戶之前已經說得很清楚功能要什麼, 為什麼還有一堆問題????」，未核准也未否決，記為降級（d71e0a4）；2026-09-17 使用者看過封存被拒的報告後回覆「核准 -> 封存 -> 更新 PR」，記為 v2 事後核准。v2 相對 v1 的差異：D-4（使用者裁定「還原為原本的值 (Recommended)」）、D-5 與 D-6（orchestrator 預設，只記錄不改程式）、SC-F20、after-implement squad 的 class 1 修正清單
- `ordering`: tests-first（每組行為一個 RED commit 先於 GREEN commit：4378e23→e02bf35、0bd463b→2d4671b、19bb45f→8d0e288、7fa09ef→889cb92、c07ce7e→f7cd029、1cd635b→fe28b5d；RED commit 只含 `_test.go`、red.md 與 throwaway-mutants.txt（1cd635b、5680959），GREEN 只含產品碼與 mutants.txt／realexec.sh（fe28b5d 另含 SPEC Revisions 1 行）。例外：`TestNewShowCmd_ExactCommandAndSecureEnv`（1cd635b）對產品碼從未 FAIL，只對 squad 手作 mutant FAIL，屬回歸測試，其有效性由常設 mutant `locale-not-pinned` 證明（第六輪 mutation.log:21）；5680959 `fixes_read_capped_edges_test.go` 是 gate 第二輪覆蓋率 41/45 之後補的測試，晚於它覆蓋的實作 2d4671b，以一次性 mutant 證明會失敗；6178686 為覆蓋率合併不可達分支的 refactor）
- `git_facts`: complete（base 存在、非 shallow、baseline 於隔離 worktree 執行）
- `source_state`: commit=3a1ceddf851cacfe3afdeded3e747577140eb30f tree=3e0a645c0eba22fd5895ef3c30b1405f1bc4e9d2（`tools/gate/source_state.sh`，gate 第六輪前後相同）
- `source_state_exclusions`: `GATE_UNTRACKED_OK=".gate/"`（tools/gate.sh:32）
- `toolchain`: tools/gate/versions.env（STATICCHECK_VERSION=2026.2.1、GOVULNCHECK_VERSION=v1.7.0）；go.mod `go 1.26.3`；觀測到 go1.27.0 windows/amd64、git 2.53.0.windows.2
- `entry_point`: `sh tools/gate.sh -base 3d59291 -scope marketplace-check-outdated-fixes`
- `reproducibility`: reproducible
- `changed_unit_command`: gate changed-units 層（`gatetool coverage -base 3d59291`，產出 `.gate/marketplace-check-outdated-fixes/units.tsv`）
- `changed_unit_granularity`: symbol（每個變更行所屬的頂層宣告）

## Baseline

none — base was green：在 674ec98（產品碼與 3d59291 相同）的隔離 worktree 跑 `go test ./cmd/apm-go/... ./internal/marketplace/... ./internal/semver/...`，6 個套件 ok，0 失敗（scratchpad `baseline.log`）。after-spec squad 在其副本觀察到 `TestDoctor_ExecGit_TimesOutDespiteOrphanedGrandchildHoldingPipesOpen` 單獨執行失敗，在此 baseline 通過。

## Changed unit → Test

18 個單元（symbol 粒度）：4 個 file-level（refcheck.go、refcheck_sha.go、schema.go、tagpattern.go）、2 個 var（`errReadCapExceeded`、`oracleVersionRe`）、12 個符號。宣告類無可執行行，合併為一列 n-a。`semver.go::IsValid` 只有刪除行，不是 units.tsv 的單元，下表另列為刪除佐證。

| Changed unit | Test | Status |
|---|---|---|
| refcheck.go::refMatches | fixes_sha_match_test.go::TestCheckPackages_ShaPin_RefNamedLikeSha_DifferentCommit_Probes（含 WithVersion、Exists）; fixes_sha_match_regression_test.go::TestCheckPackages_NonShaRef_StillMatchesByName（5 子測試）; 既有 refcheck_sha_test.go::TestCheckPackages_ShaPin_MatchesListedCommit_NoProbe | pass |
| refcheck.go::outdatedForPackage | fixes_current_test.go::TestOutdatedPackages_ShaPinWithVersion_NoCandidates_CurrentDashes（3）, ::TestOutdatedPackages_ShaPinWithoutVersion_ErrorRows_CurrentDashes（3）; fixes_current_regression_test.go::…_RangeEntryKeepsMap; fixes_after_implement_test.go::TestOutdatedPackages_LocalShaPin_KeepsCurrentMap（2）; cmd marketplace_outdated_current_test.go::TestMarketplaceOutdated_ShaPin_NoTags_MarketplaceJson_OmitsSha | pass |
| refcheck.go::outdatedShaAgainstTags | fixes_current_test.go::TestOutdatedPackages_ShaPinWithVersion_LeadingV_SameAsBare（3）; fixes_after_implement_test.go::…_LeadingV_SameAsBare_BuildTag | pass |
| refcheck.go::versionTagCandidatesWithPattern | fixes_tag_inference_test.go::TestCheckPackages_TagInference_RejectsShortVersions, ::TestVersionTagCandidates_VPrefixedCapture（CaptureRejected、FallbackInfers） | pass |
| refcheck_sha.go::showAtFetchHead | fixes_read_capped_regression_test.go::TestShowAtFetchHead_Oversized_StopsBeforeTimeout（含 ApmYML）, ::TestFetchManifestVersion_ManifestAtLimit_Reads; fixes_read_capped_edges_test.go::TestShowAtFetchHead_GitNotStartable_Errors; 既有 TestGitManifestVersionFetcher_* | pass |
| refcheck_sha.go::newShowCmd | fixes_after_implement_test.go::TestNewShowCmd_ExactCommandAndSecureEnv; fixes_read_capped_test.go::TestNewShowCmd_ShapeAndSecureEnv | pass |
| refcheck_sha.go::readCapped | fixes_read_capped_test.go::TestReadCapped_StopsAtLimit（含 ExactlyAtLimit） | pass |
| schema.go::parsePackages / requireNonEmptyString / isStringNode | fixes_schema_type_test.go::TestLoadAuthoringConfig_NonStringName_Rejected（5）, ::TestLoadAuthoringConfig_NonStringSource_OracleMessage（3）; fixes_schema_type_regression_test.go::TestLoadAuthoringConfig_StringLikeScalars_Accepted（5）, ::TestLoadAuthoringConfig_NumericVersionOrRef_Accepted; 既有 schema_test.go::TestLoadAuthoringConfig_EmptySource_Rejected | pass |
| tagpattern.go::IsOracleVersion | tagpattern/oracle_version_test.go::TestIsOracleVersion_Table（15 列） | pass |
| tagpattern.go::Infer | fixes_tag_inference_test.go::TestCheckPackages_TagInference_RejectsShortVersions（經 versionTagCandidatesWithPattern 的 fallback） | pass |
| deleted: semver.go::IsValid（非 units.tsv 單元） | 呼叫者只有 tagpattern.go 與 refcheck.go，皆改呼叫 IsOracleVersion；build 與全套件綠；`git grep IsValid` 在程式碼中無殘留（squad diff lens） | pass |
| 宣告類（3 file-level、2 var） | — | n-a |

## Stated claim → Test

來源：SPEC v2。v1 部分 human-approved；v2 新增部分（D-4..D-6、SC-F20、SC-F8 build-tag、SC-F16 加強、SC-F15 outdated 步驟）核准未取得。

| Claim | Test | Status |
|---|---|---|
| SC-F1 SHA 只比對 commit，同名 ref 不免除探測（偏離） | fixes_sha_match_test.go（同名，3 案例） | pass |
| SC-F2 非小寫 40-hex 仍以名稱比對 | fixes_sha_match_regression_test.go（同名，5 子測試） | pass |
| SC-F3 readCapped 讀到 N+1 即停 | fixes_read_capped_test.go::TestReadCapped_StopsAtLimit | pass |
| SC-F4 超限在期限內停止、暫存目錄移除（回歸） | fixes_read_capped_regression_test.go（同名） | pass |
| SC-F5 恰為上限可讀 | fixes_read_capped_regression_test.go（同名） | pass |
| SC-F16 show 命令為 `git -C <dir> show FETCH_HEAD:<path>`、完整安全環境、LC_ALL=C、WaitDelay | fixes_after_implement_test.go::TestNewShowCmd_ExactCommandAndSecureEnv（完整 Args 與 `gitops.SecureGitEnv()` 全部鍵）; fixes_read_capped_test.go::TestNewShowCmd_ShapeAndSecureEnv（WaitDelay）。ExactCommand 測試對產品碼未曾 FAIL（回歸；見 ordering） | pass |
| SC-F6 SHA 加 display version 無候選／offline／ListRefs 錯誤時 Current `--`；range 條目保留 map | fixes_current_test.go、fixes_current_regression_test.go（同名） | pass |
| SC-F17 無 version 的 SHA 在錯誤列 Current `--`（D-2） | fixes_current_test.go（同名，3 子測試） | pass |
| SC-F7 CLI 讀 marketplace.json 時 SHA 不出現 | cmd marketplace_outdated_current_test.go（同名；斷言 sha[:12] 不出現，40 字元必含其前綴） | pass |
| SC-F8 `v1.0.0` 與 `1.0.0` 結果相同（含 build-tag） | fixes_current_test.go（同名，3 子測試）; fixes_after_implement_test.go::…_BuildTag | pass |
| SC-F20 本機 SHA 列保留 current map（D-4） | fixes_after_implement_test.go::TestOutdatedPackages_LocalShaPin_KeepsCurrentMap | pass |
| SC-F9 非字串 name 拒絕、check/outdated exit 2 | fixes_schema_type_test.go（同名）; realexec `mkt-check-schema-name-type`、`mkt-outdated-schema-name-type` | pass |
| SC-F18 非字串 source 的 oracle 訊息 | fixes_schema_type_test.go（同名） | pass |
| SC-F19 字串型 scalar 接受、既有訊息不變 | fixes_schema_type_regression_test.go（同名） | pass |
| SC-F10 數字 version/ref 保留原文 | fixes_schema_type_regression_test.go（同名） | pass |
| SC-F11 oracle 文法表格（含 D-b 兩列偏離） | tagpattern/oracle_version_test.go::TestIsOracleVersion_Table；期望值由本機 Python 載入 oracle `semver.py`（對 pin 無差異）實跑 15 列取得（red.md Group E） | pass |
| SC-F12 推斷拒絕短版本 | fixes_tag_inference_test.go（同名） | pass |
| SC-F13 `{version}` 擷取拒絕 `v1.2.0`；零匹配時 fallback 推斷 `v{version}` | fixes_tag_inference_test.go::TestVersionTagCandidates_VPrefixedCapture（2 子測試） | pass |
| SC-F14 mutants：6 個新增 + `leading-v-compare-not-stripped` + 2 個更新 | tools/gate/mutants.txt；gate 第五、六輪皆 26/26 killed（`sha-name-match-restored` 由 SC-F1 殺；`sha-commit-match-removed` 由 `TestCheckPackages_BlankVersion_NoManifestFetch` 殺——其 panicProber 讓套件中止，SC-B1 未執行到，但該 mutant 在隔離副本另由 SC-B1 單獨殺過，見 Negative controls） | pass |
| SC-F15 realexec `name: 123` check 與 outdated `--offline` exit 2 | tools/gate/realexec.sh；gate 第六輪 126/126 | pass |
| Must NOT：原 SPEC v4 Must NOT 全部仍有效 | 原 scope 全部測試與 realexec 步驟在第五、六輪通過；`git diff 3d59291..HEAD -- '*_test.go' \| grep '^-[^-]'` 為空 | pass |
| Must NOT：不修改 specs/archive/marketplace-check-outdated/ | `git diff --name-only 3d59291..HEAD -- specs/archive/marketplace-check-outdated/` 為空 | pass |
| Must NOT：Compile/ExtractVersion/RenderTag/FilterTags/Validate、IsDisplayVersion、semver 其他匯出函式不變 | diff 中這些函式無變更（squad diff lens 逐一核對）；tagpattern 既有測試與 property 測試通過 | pass |
| Must NOT：既有測試不刪、斷言不放寬；既有 mutant 只更新錨點 | 上述 grep 為空；mutants.txt 的 `sha-match-by-name-only` 改名為 `sha-commit-match-removed`（語意：移除 commit 比對，仍被殺，見 SC-F14 列） | pass |
| Must NOT：非 SHA 列與本機列的 Current 來源不變 | fixes_current_regression_test.go::…_RangeEntryKeepsMap; SC-F20 | pass |
| Must NOT：version/ref 數字不拒絕 | SC-F10 | pass |
| Must NOT：非小寫 40-hex 比對不變 | SC-F2 | pass |
| Must NOT：無新增 git 子程序；fetch/init/show 保留安全環境、WaitDelay、pinGitLocale；錯誤經 SanitizeGitOutput | SC-F16（show）; 既有 TestNewProbeFetchCmd_ShapeAndSecureEnv（fetch）; init 的 `ApplySecureGitEnv`／`pinGitLocale` 無斷言（同原 scope 的 SC-B20 partial）; mutants `locale-not-pinned`、`probe-error-unsanitized` killed | partial：init 無斷言 |
| Must NOT：測試與 gate 不連網 | 所有新測試用 t.TempDir() repo 或 fake lister；realexec 新步驟 check／outdated 兩步為 `--offline`，pack 步驟（`pack --dry-run`，無此旗標）在 LoadAuthoringConfig 即失敗、未達網路；供應鏈層無新 capability import。既有例外兩處（皆非本變更引入，Honest notes）：供應鏈層 govulncheck 讀 vuln.go.dev；既有 `TestDoctor_ExecGit_TimesOutDespiteOrphanedGrandchildHoldingPipesOpen` 在 Windows 實際執行真 git 的 `ls-remote origin` | partial：兩處既有連網 |
| Failure model：SHA 被同名 ref 繞過 | SC-F1 + mutant `sha-name-match-restored` | pass |
| Failure model：記憶體耗盡／超限後 git 未結束 | SC-F3 + mutant `manifest-read-uncapped`；SC-F4 耗時斷言（回歸；對現行實作仍為活檢查：去掉超限分支的 `cancel()` 會撐到 3s 逾時而失敗，但無常設 mutant 釘此害，見 Honest notes） | pass |
| Failure model：Current 顯示 SHA／誤清本機列 | SC-F6、F17、F7、F20 + mutant `current-map-kept-on-no-candidates` | pass |
| Failure model：`v` 前綴誤判 | SC-F8 + mutants `leading-v-not-stripped`、`leading-v-compare-not-stripped` | pass |
| Failure model：非字串 name 流入／誤拒合法值 | SC-F9、F18、F19、F10 + mutant `name-tag-unchecked` | pass |
| Failure model：文法未收緊或誤拒 | SC-F11、F12、F13 + mutant `oracle-grammar-loosened` | pass |
| Failure model：pack/doctor/package 行為改變 | pack：realexec `mkt-pack-schema-name-type`（`pack --dry-run` 對 `name: 123` exit 1 並印 oracle 訊息；3d59291 的 binary 會成功產出，故非 vacuous）；doctor、package：既有測試與 realexec 步驟通過 | pass |

## RED reconstruction

`tools/gate/red.sh -base 3d59291`（scratchpad `fixes-red-base.md`；只重建新增的 `_test.go`，以檔案為單位）：

| Test file | Result at base | Note |
|---|---|---|
| cmd/apm-go/marketplace_outdated_current_test.go | failed (assertion) | 1 test |
| authoring/fixes_sha_match_test.go | failed (assertion) | 1 test（3 案例） |
| authoring/fixes_current_test.go | failed (assertion) | 3 tests |
| authoring/fixes_schema_type_test.go | failed (assertion) | 2 tests |
| authoring/fixes_tag_inference_test.go | failed (assertion) | 2 tests |
| authoring/fixes_read_capped_test.go | failed (collection) | undefined readCapped（weaker RED；SC-F16 同檔） |
| authoring/fixes_after_implement_test.go | failed (collection) | undefined outdatedRowWithCurrent（helper 在 fixes_current_test.go，base 沒有）；每個測試的 RED 見下 |
| tagpattern/oracle_version_test.go | failed (collection) | undefined IsOracleVersion（weaker RED） |
| authoring/fixes_{sha_match,read_capped,current,schema_type}_regression_test.go | passed | 回歸保護，SPEC 標明 |
| authoring/fixes_read_capped_edges_test.go | passed | 覆蓋率後補；一次性 mutant 證明（throwaway-mutants.txt） |

逐測試的 RED 觀察（各 RED commit 的樹，非 base）記於 `.scratch/marketplace-check-outdated-fixes/red.md`：Group A 3 個案例 FAIL；Group C 12 個子測試 FAIL；Group D 7 個子測試 FAIL（`name: [a]` 為回歸）；Group E 3 個 FAIL 加 1 個編譯失敗；v2 follow-up：SC-F20 2 個 FAIL、SC-F8 build-tag FAIL、SC-F16 加強版對產品碼 PASS（弱的是舊測試）並對 squad 的 mutant FAIL。SC-F4 在修正前 8 倍與 64 倍皆 PASS，依 SPEC 註改標回歸。

## Gate (final fresh run)

`sh tools/gate.sh -base 3d59291 -scope marketplace-check-outdated-fixes`，2026-09-17 01:36 起，source state 3a1cedd（前後相同；第六輪，記錄檔 `fixes-gate-run6.log`）。第五輪在 fb76028（產品碼相同）同樣全綠（realexec 124/124，其餘數字相同）。第六輪 mutation 層期間電腦於 01:42 睡眠、06:01 喚醒（Kernel-Power 事件 42／107），`rel-exact-parent-escapes` 的 `go test` 跨過睡眠、回報 15560s，並在 cmd/apm-go 與 authoring 兩個套件觸發 `test timed out after 10m0s`；rootfs 的真正殺手 `TestRelRefusesExactParentProperty` 也在同一份 log。該 mutant 事後以 `GATE_MUTANTS` 單行檔在隔離副本重跑（`.gate/rerun-one/`，記錄檔 `fixes-gate-run6-mutant-rerun.log`）：1/1 killed，無 timeout。其餘 25 個 mutant 各約 50 秒，無 timeout。

| Layer | Command | Threshold | Result |
|---|---|---|---|
| Tests | `go test -count=1 ./...` | 0 new failures vs baseline | 26 packages ok, 0 failed |
| Types / vet | `go vet ./...` | 0 findings | 0 |
| Lint / format | gofmt on changed .go | 0 drift | 0 |
| Static | `staticcheck@2026.2.1 -checks all,-ST1000,-ST1003,-ST1016,-ST1020,-ST1021,-ST1022,-ST1005,-ST1018 ./...` | 0 findings | 0 |
| Suite health | `go test -count=1 -shuffle=1789580294 ./...` | 0 failures，先於 mutation 與 coverage | 26 packages ok |
| Property-based | `go test -run Property -v ./internal/marketplace/... ./internal/rootfs/...` | all pass, ≥1 ran | 5 properties passed |
| Supply chain | `govulncheck@v1.7.0 ./...` + go.mod delta + imports diff | 0 vulns; new deps justified | No vulnerabilities found；go.mod 無變更；無新 capability-bearing import（產品碼新 import 只有 `io`；`imports-added.txt`） |
| Real execution | `tools/gate/realexec.sh` | 0 FAIL | 126/126（含 3 個本 SPEC 新增的 mkt-*-schema-name-type 步驟，各 2 個檢查——exit code 與訊息——共 6 個；124→126 的差是第六輪才加入的 pack 步驟） |
| Mutation | `tools/gate/mutate.sh`（manual, sequential, isolated copy） | 0 survived, 0 broken | 26/26 killed（7 個本 SPEC 新增，2 個更新） |
| Changed units | gatetool coverage | symbol granularity | 18 units |
| Changed-line coverage | gatetool coverage | 100%, 0 unmapped | 42/42 executable lines; 66 non-executable; 0 unmapped; 0 platform-excluded; files=4 |
| Source state | `tools/gate/source_state.sh` before/after | identical | identical |

## Negative controls

- gate selftest：每輪開頭驗證缺層、未知層、失敗 rc、非唯一 anchor 皆會失敗；第五、六輪通過。
- 第一輪（28a2fac）mutation 層以 BROKEN 停下：`sha-match-by-name-only` 改寫後迴圈變數未使用、無法編譯，gate fail-closed；修正後在隔離副本確認該 mutant 可編譯且被 `TestCheckPackages_ShaPin_MatchesListedCommit_NoProbe` 殺。
- 第二輪（eb59560）changed-line coverage 41/45 停下，列出 4 個未覆蓋行；補測試與合併不可達分支後第三輪 41/41。
- 一次性 mutant：`git-show-start-error-swallowed`（殺於 TestShowAtFetchHead_GitNotStartable_Errors）、`show-cmd-insecure-env`（殺於 TestNewShowCmd_ExactCommandAndSecureEnv，舊測試 TestNewShowCmd_ShapeAndSecureEnv 存活，證明加強有效），皆在隔離副本觀察。
- squad diff lens：`sha-name-match-restored` 舊版（`if false {`）連 commit 比對一起移除，SC-B1 單獨可殺，不能證明 SC-F1 必要；改為 `if r.Commit == ref || r.Name == ref {` 後只恢復名稱比對，第五、六輪皆由 SC-F1 殺。
- Kill attribution：第六輪每個 killed 行的失敗測試與 mutant 所在函式有因果關係（例如 `oracle-grammar-loosened` → cmd 層 NoMatchingTags 測試因短 tag 變成候選而失敗）；唯一例外是睡眠汙染的 `rel-exact-parent-escapes`，已單獨重跑（Gate 段）。
- 單 mutant 重跑第一次以 scratchpad 為副本位置時 baseline 失敗：`TestDoctor_ExecGit_TimesOutDespiteOrphanedGrandchildHoldingPipesOpen` 在非 git 目錄下 20ms 內得到真 git 的 `No remote configured`，而在 `.gate/` 副本（worktree 之內）真 git 對 origin 做 `ls-remote` 約 660ms 而「超時」通過。mutate.sh 對 baseline 失敗 fail-closed（round void），副本改回 worktree 內後重跑。此測試在 Windows 是 vacuous（Honest notes）。

## Layers not run as specified

- N-A：型別檢查獨立層（Go 編譯即型別檢查，由 build 與 vet 層覆蓋）。
- UNAVAILABLE：`tools/parity` 輸出契約 gate — 本機 Windows 被 build constraint 排除；由 CI parity.yml 執行。本變更未新增或修改 corpus case 與 waiver。
- SUBSTITUTED：無。NOT REACHED：無。DEPENDENCY UNMET：無。

## Structural blind spot

- SHA 探測、manifest 讀取上限、tip 比對無法在 built binary 上離線重現（CLI 把 `owner/repo` 解析到 github.com）；只由函式層測試（真 git 對 t.TempDir() repo）與 cmd 層 fixture seam 證明。realexec 只釘離線契約。
- SC-F4 的 3s 耗時餘裕在 Git for Windows 啟動器（`Git\cmd\git.exe`）下為約 2s（WaitDelay），gate 使用 mingw64 git（約 24ms）；超限後孫程序是否殘留未量測（squad contract lens）。
- 真實遠端（GitHub/GitLab）對 40-hex 分支名與 SHA fetch 的行為未驗證。
- parity gate 未在本機執行。

## Honest notes

- **v2 核准為事後取得。** 2026-09-17 使用者以「核准 -> 封存 -> 更新 PR」核准，晚於 v2 實作、gate 與 verifier；以下為核准前的紀錄： after-implement squad 的 class 2 三項（D-4 使用者裁定、D-5、D-6 預設）與 class 1 修正寫成 v2（102f282）送審，使用者回覆「用戶之前已經說得很清楚功能要什麼, 為什麼還有一堆問題????」並要求不再提問。orchestrator 依原始要求（修 Codex findings、其他行為不變）與 v1 已核准的內容完成 v2 實作；D-5、D-6 未改程式，符合「其他行為不變」。`spec-archive` 對 status 非 `approved` 的 SPEC 會拒絕封存；本報告不繞過該檢查。
- 版本編號曾標錯：7112da6 在待決定未清空時標「v1」，7abf1e5 未核准時標「v2」；5760b0c 更正為 v1 並記於 Revisions。現行 v2（102f282）與 7abf1e5 的「v2」無關。
- SPEC 標註更正三處（皆記於 Revisions）：SC-F4 改標回歸（修正前 8×、64× 皆 PASS）；SC-F13 FallbackInfers 改標 RED；SC-F9 `name: [a]` 為回歸。oracle `__init__.py` 引用行號由本機 v0.30.0 改為 pin b75a02b1（`:1139-1141`、`:1178-1190`）。
- D-6：refactor 6178686 讓一條真實 git 下到不了的路徑（git 正常結束後非超限讀取錯誤）少了 `git show <path>:` 前綴、附帶部分資料、Wait 前未 cancel；呼叫者先檢查 err，部分資料不外流。commit 說明「no behaviour change for callers」的範圍應為「只在不可達路徑改變錯誤形狀」。
- D-5：`IsOracleVersion` 接受超出 uint64 的數字，`semver.IsPrerelease` 解析失敗回 false，`v99999999999999999999.0.0-rc.1` 在未開 include-prerelease 時仍成候選；oracle 會過濾。實際 tag 不會出現，記為偏離。
- D-1 的偏離例子不完整：apm-go 另接受 `name: 1:20`、`190:20:30`、`=`（PyYAML 為 int、int、ConstructorError），另拒絕 `09`、`+.5`、`1.0e3`（PyYAML 為 str）。判準（`!!str`）不變。
- `git show` 逾時時錯誤文字為空（`git show <path>: `），修正前即如此；本變更之外。
- 原 scope 的 SC-B20 partial 仍在：`git init` 子程序的 `pinGitLocale` 無斷言；本變更新增的 SC-F16 補上了 show 的斷言。
- ARCHITECTURE.md §2 `semver` 列的行號錨點曾為前一 scope 留下的舊值（`semver.go:16,75,67`），verifier round 1 指出後於 3a1cedd 更正為 `:20,79,71`。
- `cmd/apm-go/marketplace_authoring.go:475` 註解引用 `__init__.py:1133-1148`，pin b75a02b1 的對應段落起於 1139；該檔不在本變更 diff 內，未改（verifier round 1 finding 6）。
- 供應鏈層 govulncheck 讀 https://vuln.go.dev，與「gate 不連網」Must NOT 不符；前一 scope 即如此，記為既有例外（verifier round 1 finding 3）。
- 既有 `TestDoctor_ExecGit_TimesOutDespiteOrphanedGrandchildHoldingPipesOpen`（cmd/apm-go/doctor_test.go:382）在 Windows 是 vacuous：它寫入的假 `git` 無副檔名，`exec.LookPath` 找不到，真 git 執行；cwd 在 git repo 內時 `ls-remote origin` 連網約 660ms 超過 200ms 而「通過」，cwd 在 repo 外則 20ms 失敗。每輪 gate 的 tests、suite-health、mutation baseline 與每個 mutant 都因此各連網一次。本變更之外（class 3），未改。
- 獨立驗證（verifier round 1，`.scratch/marketplace-check-outdated-fixes/verification.md`）：verdict passed，0 個 behavioural finding，6 個 description finding；處置於 3a1cedd（realexec pack 步驟、SPEC 引用與 Revisions、ARCHITECTURE 錨點、本報告歸因更正）。最終狀態 3a1cedd 的產品碼與 verifier 驗過的 fb76028 相同，未再送 verifier。
- 「每個遠端套件至多一次 ls-remote」（SPEC 明確排除段）無計數測試，未量測；沿前一 scope。
- after-spec squad 的三項 class 3（SHA 釘選與 `+build` tag 並存時 `CompareVersions` tie-break 誤報；fetch stderr 與 ls-remote stdout 無上限緩衝；annotated tag 的 tag object SHA 命中 commit 欄位而不探測）只記於 SPEC 明確排除段（SPEC.md:168-170），未修。
- 無常設 mutant 覆蓋 `showAtFetchHead` 超限分支的 `cancel()`（refcheck_sha.go:303-307）；只由 SC-F4 的 3s 耗時斷言承擔。`TestShowAtFetchHead_GitNotStartable_Errors` 的失敗證據只有一次性 mutant `git-show-start-error-swallowed`（throwaway-mutants.txt），未併入 mutants.txt。SC-F8 build-tag 斷言為關係式（兩邊相等），不釘絕對值。三者依 before-archive 的處置規則（不新增 mutant、不重跑 gate）記錄不改。
- before-archive squad（`.scratch/marketplace-check-outdated-fixes/squad/before-archive.md`，三 lens，source state 6615b9d）：16 項，全為描述類，文字處置；三 lens 一致認定證據對 git 為真，結案唯一阻礙為 v2 核准未取得。
- gate 共跑六輪，其中第四輪因 session 中斷白跑，第六輪只因 realexec 新增一步（description finding 的處置選擇）而完整重跑；產品碼自 fb76028 起未變。
- 既有 flaky `TestFetchGit_*`（internal/marketplace，讀全域 temp 目錄計數）與 `ListRefs` 無 WaitDelay：本變更之外，沿前一 scope 記錄。
- squad 紀錄：`.scratch/marketplace-check-outdated-fixes/squad/after-spec.md`（四 lens，全部 class 1 已修）、`squad/after-implement.md`（三 lens，class 1 全部 status: fixed，class 2 依 D-4..D-6 處置，class 3 記於本節）。
- 第四輪 gate 在 mutation 層中途被中斷（session 中斷），第五輪從頭重跑，全綠。
