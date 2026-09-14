# Evidence Report — marketplace check / outdated 缺口修正與 SHA 釘選支援 (Tier 3)

- `headline`: GATE PASSED
- `command`: `evidence`
- `contract`: applied
- `scope`: marketplace-check-outdated
- `change_set`: main...HEAD（bf18093...f300338）
- `base`: main (bf18093)
- `report_language`: zh-TW
- `intent_status`: confirmed
- `intent_source`: specs/marketplace-check-outdated/SPEC.md，`spec_version: v4`，status approved（Approval 段：v2「核准 v2」、v3「核准 v3」、v4「核准 v4」）
- `ordering`: tests-first（每個行為一個 RED commit 先於 GREEN commit：c702abb→ea1894c、452cfe7→8611a82、f9f16c5→dc50c78、067d734→3e9ef67、63ad196→a3ce3b1、fdf5eca→10d68c3、c2fbdd6→a29a562；fixture 補值、coverage 補測與 gate.sh 修正為獨立 commit；RED commit 內含編譯用 stub，commit message 自陳）
- `git_facts`: complete
- `source_state`: commit=f300338d96281d91dd488e27e289f8e662a2d06b tree=fcb05acde6308f8a66a7745fc9a48ee3db6e67dc（`tools/gate/source_state.sh`，final run 前後相同）
- `source_state_exclusions`: `GATE_UNTRACKED_OK=".gate/"`（tools/gate.sh:32）；verifier 本身已提交
- `toolchain`: tools/gate/versions.env（STATICCHECK_VERSION=2026.2.1、GOVULNCHECK_VERSION=v1.7.0）；go.mod 的 go 指令；觀測到 go1.27.0 windows/amd64、git 2.53.0.windows.2
- `entry_point`: `sh tools/gate.sh -base main -scope marketplace-check-outdated`
- `reproducibility`: reproducible
- `changed_unit_command`: `tools/gate/gatetool coverage -base main ...`（gate 的 changed-units 層，產出 `.gate/marketplace-check-outdated/units.tsv`）
- `changed_unit_granularity`: symbol（每個變更行所屬的頂層宣告）

## Baseline

none — base was green：在 `main`（bf18093）的獨立 worktree 跑 `go test -count=1 ./...`，26 個套件 ok，0 失敗（scratchpad `baseline.log`）。

## Changed unit → Test

59 個單元（symbol 粒度；v4 新增 `optionalNonEmptyString`，由 schema_validation_test.go::TestLoadAuthoringConfig_EmptyVersionOrRef_Rejected 覆蓋）。型別、常數、變數與檔案層級宣告（`CommitProber`、`ManifestVersionFetcher`、`CheckDeps`、`DefaultCommitProber`、`DefaultManifestVersionFetcher`、`scratchTempRoot`、`scratchTempPrefix`、`manifestReadMaxBytes`、`errRefNotOnRemote`、`subprocessWaitDelay`、`ConfigValidationError` 型別、`DefaultPatterns`、六個 file-level 列）無可執行行，合併為一列 n-a。

| Changed unit | Test | Status |
|---|---|---|
| cmd/apm-go/marketplace_authoring.go::marketplaceCheckCmd | marketplace_check_wording_test.go::TestMarketplaceCheck_Verbose_PrintsResolvingLines, ::TestMarketplaceCheck_Wording_MatchesOracle, ::TestMarketplaceCheck_UnreachableDetail_TruncatedTo60, ::TestMarketplaceCheck_ShaPin_VerifiedThroughDefaultProber, ::TestMarketplaceCheck_ManifestMismatch_ReportedInTable; marketplace_authoring_test.go::TestMarketplaceCheck_RemotePackagePinnedRefMissing_ExitsNonZero | pass |
| cmd/apm-go/marketplace_authoring.go::resolvingLabel | marketplace_check_label_test.go::TestResolvingLabel | pass |
| cmd/apm-go/marketplace_authoring.go::configLoadError | marketplace_check_config_test.go::TestMarketplaceCheck_ConfigError_ExitsTwo, ::TestMarketplaceOutdated_ConfigError_ExitsTwo | pass |
| cmd/apm-go/marketplace_authoring.go::marketplaceOutdatedCmd | marketplace_outdated_wording_test.go::TestMarketplaceOutdated_UpgradableExitsSilently（含 VerboseCount）, ::TestMarketplaceOutdated_ShaPinWithVersion_RowThroughCLI, ::TestMarketplaceOutdated_Wording_MatchesOracle | pass |
| internal/marketplace/authoring/refcheck.go::parseRefsOutput | refcheck_sha_test.go::TestParseRefsOutput_KeepsFullRefName | pass |
| refcheck.go::CheckPackages | refcheck_test.go::TestCheckPackages_*（既有 14 個，經 CheckPackages 包裝） | pass |
| refcheck.go::checkPackage | refcheck_sha_test.go::TestCheckPackages_ShaPin_MatchesListedCommit_NoProbe, _NotAtAnyRef_ProbeSucceeds, _Missing_ProbeFails_NotFound, ::TestCheckPackages_ShaProbe_NetworkError_Unreachable, ::TestCheckPackages_FullRefName_Accepted, ::TestCheckPackages_HeadRef_Rejected, ::TestCheckPackages_VersionRange_TagPatternFallback, ::TestCheckPackages_VersionRange_ExcludesPrerelease_UnlessOptIn, ::TestCheckPackages_RefWithDisplayVersion_*（5）, ::TestCheckPackages_RefWithRangeVersion_NoManifestFetch, ::TestCheckPackages_ManifestFetch_CloneFailure_Unreachable; refcheck_sha_edges_test.go::TestCheckPackages_VersionRange_Unparsable_FailsWithParseError, ::TestCheckPackages_BlankVersion_NoManifestFetch | pass |
| refcheck.go::refMatches | refcheck_sha_test.go::TestCheckPackages_ShaPin_MatchesListedCommit_NoProbe（含 BranchTipWithoutTag）, ::TestCheckPackages_FullRefName_Accepted | pass |
| refcheck.go::effectiveTagPattern | refcheck_test.go::TestCheckPackages_RemoteVersionRange_UsesPackageTagPatternOverBuildDefault | pass |
| refcheck.go::outdatedForPackage | refcheck_outdated_sha_test.go::TestOutdatedPackages_*（14）; refcheck_test.go::TestOutdatedPackages_*（既有 10） | pass |
| refcheck.go::outdatedShaAgainstTip | refcheck_outdated_sha_test.go::TestOutdatedPackages_ShaPinWithoutVersion_TipMoved_Upgradable, _AtTip_UpToDate, _NoHeadEntry_IconX, ::TestOutdatedPackages_BlankVersion_TreatedAsNoVersion | pass |
| refcheck.go::outdatedShaAgainstTags | refcheck_outdated_sha_test.go::TestOutdatedPackages_ShaPinWithVersion_NewerTag_Upgradable（含 NamePattern）, _UpToDate, _NoMatchingTags, _DeclaredTagMissing | pass |
| refcheck.go::versionTagCandidates / versionTagCandidatesWithPattern | refcheck_sha_test.go::TestCheckPackages_VersionRange_TagPatternFallback; refcheck_outdated_sha_test.go::TestOutdatedPackages_VersionRange_TagPatternFallback; refcheck_test.go::TestOutdatedPackages_IncludePrerelease_* | pass |
| refcheck.go::truncateRunes | refcheck_sha_edges_test.go::TestTruncateRunes; refcheck_outdated_sha_test.go::TestOutdatedPackages_Wording_MatchesOracle, ::TestOutdatedPackages_ShaPin_Offline_IconX | pass |
| deleted: refcheck.go::extractOutdatedCandidates | 改名為 versionTagCandidates；build + 全套件綠，`grep` 無殘留引用（gate changed-units 列：no remaining reference） | pass |
| refcheck_sha.go::CheckPackagesWith | 同 checkPackage 各列（全部經 CheckPackagesWith） | pass |
| refcheck_sha.go::gitCommitProber.HasCommit | refcheck_sha_test.go::TestCheckPackages_ShaPin_NotAtAnyRef_ProbeSucceeds, _Missing_ProbeFails_NotFound, ::TestGitCommitProber_SanitizesTokenInError, ::TestCheckPackages_ShaProbe_TimesOut; refcheck_sha_edges_test.go::TestGitCommitProber_RemoteNotARepository_Unreachable, ::TestGitCommitProber_LocalSourceOutsideRoot_Errors, ::TestScratchGit_LocalePinnedToC | pass |
| refcheck_sha.go::gitManifestVersionFetcher.FetchManifestVersion | refcheck_sha_test.go::TestCheckPackages_RefWithDisplayVersion_ManifestMatches_OK, _ManifestMismatch_Fails, _FallsBackToApmYML, _NoManifest_SkipsComparison, _SubdirRespected; refcheck_sha_edges_test.go::TestGitManifestVersionFetcher_RefVanished_Errors, _HostileSubdir_ErrorsInsteadOfEscaping, _MalformedPluginJSON_Errors, _ApmYML_UnsafeYAML_Errors, _ApmYML_NotAMapping_NoVersion, _OversizedManifest_Errors | pass |
| refcheck_sha.go::newProbeFetchCmd | refcheck_sha_test.go::TestNewProbeFetchCmd_ShapeAndSecureEnv; refcheck_sha_edges_test.go::TestScratchGit_LocalePinnedToC | pass |
| refcheck_sha.go::pinGitLocale | refcheck_sha_edges_test.go::TestScratchGit_LocalePinnedToC（mutant locale-not-pinned 被此測試殺） | pass |
| refcheck_sha.go::fetchRefIntoScratch | refcheck_sha_test.go::TestCheckPackages_ShaProbe_TempDirAlwaysRemoved; refcheck_sha_edges_test.go::TestFetchRefIntoScratch_MissingScratchRoot_Errors, ::TestFetchRefIntoScratch_UnresponsiveRemote_TimesOut | pass |
| refcheck_sha.go::fetchTimedOut / gitFailureText | refcheck_sha_test.go::TestCheckPackages_ShaProbe_TimesOut; refcheck_sha_edges_test.go::TestGitFailureText_PrefersStderrThenExecError, ::TestFetchRefIntoScratch_UnresponsiveRemote_TimesOut | pass |
| refcheck_sha.go::removeScratch | refcheck_sha_edges_test.go::TestRemoveScratch_RetriesWhileAFileIsHeldOpen（Windows only）; refcheck_sha_test.go::TestCheckPackages_ShaProbe_TempDirAlwaysRemoved | pass |
| refcheck_sha.go::isRefNotOnRemote / showAtFetchHead | refcheck_sha_test.go::TestCheckPackages_ShaPin_Missing_ProbeFails_NotFound; refcheck_sha_edges_test.go::TestGitManifestVersionFetcher_HostileSubdir_ErrorsInsteadOfEscaping, _OversizedManifest_Errors, _NoManifest（via SC-B16） | pass |
| refcheck_sha.go::IsDisplayVersion | refcheck_sha_edges_test.go::TestIsDisplayVersion_BlankIsNotDisplay; build/metadata_test.go::isDisplayVersion 既有表（經委派） | pass |
| schema.go::ConfigValidationError.Error / Unwrap / IsConfigValidationError / asConfigValidationError | schema_validation_test.go::TestLoadAuthoringConfig_IsConfigValidationError; schema_validation_edges_test.go::TestAsConfigValidationError_NilStaysNil, ::TestLoadAuthoringConfig_LegacyValidationError_IsConfigValidationError | pass |
| schema.go::LoadAuthoringConfig | schema_validation_test.go（5）; schema_validation_edges_test.go（2）; schema_test.go 既有 | pass |
| schema.go::parsePackages / requireNonEmptyString | schema_validation_test.go::TestLoadAuthoringConfig_MissingName_Rejected（含 EmptyString）, _MissingSource_Rejected, _RemoteWithoutVersionOrRef_Rejected（含 LocalPackageExempt）, _DuplicateNameCaseInsensitive_Rejected; doctor_test.go::TestDoctor_MarketplaceConfig_ValidationError_ReportsConfigError | pass |
| build/metadata.go::isDisplayVersion（委派） | build/metadata_test.go 既有 isDisplayVersion 表 | pass |
| tagpattern.go::Infer / IsTagRef | refcheck_sha_edges_test.go::TestInfer_WithoutPackageName_SkipsNameLayouts; refcheck_sha_test.go::TestCheckPackages_VersionRange_TagPatternFallback（分支 v9.9.9 不算 tag） | pass |
| semver.go::IsValid | tagpattern Infer 路徑（TestCheckPackages_VersionRange_TagPatternFallback 依賴 `{version}` 版面被 semver 有效性擋下） | pass |
| 宣告類（型別、常數、變數、file-level，16 列） | — | n-a |

## Stated claim → Test

來源：SPEC v3（human-approved）。

| Claim | Test | Status |
|---|---|---|
| SC-A1..A4 schema 四規則、oracle 原文 | schema_validation_test.go（同名） | pass |
| SC-A5 `marketplace config error:` exit 2 | marketplace_check_config_test.go（同名）; realexec `mkt-check-schema-{name,source,verref,dup}`, `mkt-outdated-schema-verref` | pass |
| SC-A6 / SC-A7 | schema_validation_test.go::TestLoadAuthoringConfig_IsConfigValidationError; doctor_test.go（同名） | pass |
| SC-A8 顯式空字串 version/ref 拒絕、有值去空白、null 為未設定 | schema_validation_test.go::TestLoadAuthoringConfig_EmptyVersionOrRef_Rejected（含 BlankRef、PaddedValuesStripped、NullIsUnset） | pass |
| SC-B22 manifest 端 version 去空白 | refcheck_sha_edges_test.go::TestGitManifestVersionFetcher_PaddedVersionInPluginJSON_Trimmed（一次性突變證明） | pass |
| SC-D5 staticcheck 顯式 checks、缺設定檔 fail-closed | gate final run 印出 `staticcheck -checks all,-ST1000,…,-ST1018` 且 0 findings；Temp 路徑副本以顯式 -checks 重跑 rc=0；負向控制見下 | pass |
| SC-B1..B9, B12..B19 | refcheck_sha_test.go（同名） | pass |
| SC-B10 / SC-B11 | marketplace_check_wording_test.go（同名）; realexec `mkt-check-local-verbose`, `mkt-check-offline` | pass |
| SC-B20 語系鎖定 | refcheck_sha_edges_test.go::TestScratchGit_LocalePinnedToC | pass |
| SC-B21 空白 version | refcheck_sha_edges_test.go::TestIsDisplayVersion_BlankIsNotDisplay, ::TestCheckPackages_BlankVersion_NoManifestFetch; refcheck_outdated_sha_test.go::TestOutdatedPackages_BlankVersion_TreatedAsNoVersion | pass |
| SC-C1..C11 | refcheck_outdated_sha_test.go（同名） | pass |
| SC-C9 cmd 層靜默 exit、`-v` 格式 | marketplace_outdated_wording_test.go（同名）; realexec `mkt-outdated-offline` | pass |
| SC-D1..D3 | tools/gate/realexec.sh 步驟（final run 120/120） | pass |
| SC-D4 八個 mutant | tools/gate/mutants.txt（final run 19/19 killed） | pass |
| Must NOT：本地套件與 `--offline` 不觸網 | refcheck_test.go panicLister 測試; realexec 步驟; squad live-evidence 在 PATH 無 git 下重現 | pass |
| Must NOT：`package add/set --ref HEAD` 不變 | refcheck_test.go::TestGitRefLister_ListRefs_IncludesHEAD; editor_test.go TestSetPackage_*（斷言不變） | pass |
| Must NOT：pack/doctor/package 對合法設定不變 | 全套件 26 ok；realexec 既有 pack/mk 步驟 | pass |
| Must NOT：測試與 gate 不連網 | 所有新測試用 t.TempDir() repo；parity.yml 靜態禁網；realexec 無網路 URL | pass |
| Must NOT：每套件一次 ls-remote；探測與 manifest 各至多一次 | refcheck_sha_test.go panicProber/panicManifestFetcher 測試（SC-B1、B18、C7、C11） | pass |
| Must NOT：新 git 子程序經 secure env、錯誤經 SanitizeGitOutput | refcheck_sha_test.go::TestNewProbeFetchCmd_ShapeAndSecureEnv, ::TestGitCommitProber_SanitizesTokenInError（mutant probe-error-unsanitized） | pass |
| Must NOT：parity 現有 case 無新未 waive 差異 | 本機無法執行 tools/parity（build constraint 排除 Windows；需 pinned oracle）；waiver `doctor-config-duplicate-names` 理由已更新 | unverified |
| Must NOT：include_prerelease 語意不變 | refcheck_test.go::TestOutdatedPackages_IncludePrerelease_*, _PerPackageIncludePrereleaseOverridesGlobalFlag | pass |
| Must NOT：無測試刪除、無斷言放寬 | squad diff-hygiene 與 contract lenses 逐條核對；SPEC 明列的斷言變更以外無變更 | pass |
| Failure model：探測無限等待 | TestCheckPackages_ShaProbe_TimesOut; TestFetchRefIntoScratch_UnresponsiveRemote_TimesOut（真 git，發現並修正 WaitDelay） | pass |
| Failure model：暫存目錄殘留 | TestCheckPackages_ShaProbe_TempDirAlwaysRemoved（發現並修正 named-return closure） | pass |
| Failure model：token 洩漏 | TestGitCommitProber_SanitizesTokenInError | pass |
| Failure model：伺服器拒絕 SHA fetch | 已知限制（D-a），無本地 fixture 可觸發 | unverified |
| Failure model：SHA 釘選誤報最新 | SC-C1、SC-C4 測試 | pass |
| Failure model：cobra 吞掉 exit code | cmd 測試斷言 exitCodeOf==1 && isSilentExit | pass |
| Failure model：name-only 回歸 / 大小寫 / Current 渲染 / manifest 放行 | mutants 19/19 killed | pass |

## RED reconstruction

`tools/gate/red.sh -base main`（scratchpad `red.md`）：

| Test file | Result at base | Note |
|---|---|---|
| cmd/apm-go/marketplace_check_config_test.go | failed (assertion) | 2 tests |
| cmd/apm-go/marketplace_check_wording_test.go | failed (assertion) | 5 tests |
| cmd/apm-go/marketplace_outdated_wording_test.go | failed (assertion) | 3 tests |
| cmd/apm-go/marketplace_check_label_test.go | failed (collection) | undefined resolvingLabel；coverage 補測，一次性突變證明 |
| internal/marketplace/authoring/schema_validation_test.go | failed (collection) | undefined IsConfigValidationError（weaker RED） |
| internal/marketplace/authoring/refcheck_sha_test.go | failed (collection) | undefined CheckDeps（weaker RED） |
| internal/marketplace/authoring/refcheck_outdated_sha_test.go | failed (collection) | TagInfo.Ref 不存在（weaker RED） |
| internal/marketplace/authoring/refcheck_sha_edges_test.go / schema_validation_edges_test.go | failed (collection) | coverage 補測；13 + 1 個測試各以一次性突變在隔離副本證明會失敗（`.scratch/marketplace-check-outdated/throwaway-mutants.txt`） |

補強（git 可重現，非 base）：在各 RED commit 的樹上重放新測試，全部為斷言失敗、0 編譯錯誤：c702abb 5、452cfe7 18、067d734 10、fdf5eca 5、c2fbdd6 1（SC-A8）。`TestOutdatedPackages_VersionRange_TagPatternFallback` 於 RED 時已通過（推斷邏輯隨 group B 先落地），保留為回歸保護；`TestOutdatedPackages_ShaPinWithVersion_DeclaredTagMissing` 為既有行為的補測，以一次性突變證明。修改而非新增的測試（斷言改為 oracle 措辭者）無法重放，SPEC Must NOT 逐一列出。

## Gate (final fresh run)

`sh tools/gate.sh -base main -scope marketplace-check-outdated`，2026-09-14，source state f300338（前後相同）。

| Layer | Command | Threshold | Result |
|---|---|---|---|
| Tests | `go test -count=1 ./...` | 0 new failures vs baseline | 26 packages ok, 0 failed（baseline 0 pre-existing） |
| Types / vet | `go vet ./...` | 0 findings | 0 |
| Lint / format | gofmt on changed .go（gate lint-format 層） | 0 drift | 0 |
| Static | `staticcheck@2026.2.1 -checks all,-ST1000,-ST1003,-ST1016,-ST1020,-ST1021,-ST1022,-ST1005,-ST1018 ./...` | 0 findings | 0 findings |
| Suite health | `go test -count=1 -shuffle=1789381129 ./...` | randomized order, 0 failures — 先於 mutation 與 coverage | 26 packages ok (seed 1789381129) |
| Property-based | `go test -run Property -v ./internal/marketplace/... ./internal/rootfs/...` | all pass, ≥1 ran | 5 properties passed |
| Supply chain | `govulncheck@v1.7.0 ./...` + go.mod delta + imports diff | 0 vulns; new deps justified | No vulnerabilities found; go.mod 無變更；新 import 皆為 stdlib 或既有內部套件（net/url, encoding/json, path, time, gitops, yamlcore, go.yaml.in/yaml/v4） |
| Real execution | `tools/gate/realexec.sh` | 0 FAIL | 120/120 checks passed（含 8 個 mkt-* 步驟） |
| Mutation | `tools/gate/mutate.sh`（manual, sequential, isolated copy） | 0 survived, 0 broken | 19/19 killed（8 個本 SPEC 新增） |
| Changed units | gatetool coverage | symbol granularity | 59 units |
| Changed-line coverage | gatetool coverage | 100%, 0 unmapped | 316/316 executable lines; 458 non-executable; 0 unmapped; 0 platform-excluded |
| Source state | `tools/gate/source_state.sh` before/after | identical | identical |

## Negative controls

- gate selftest（tools/gate.sh 自檢）：缺層、未知層、重複層、失敗 rc、gatetool 非唯一 anchor、空 coverage subject 皆在每輪開頭驗證會失敗；本輪通過。
- source-state 檢查：第一輪誤把 log 放在產品樹根，selftest 立即以「untracked product path」失敗（非 final run），證明 fail-closed。
- 一次性突變證明：coverage 補測 13 個 + DeclaredTagMissing + UnresponsiveRemote + PaddedVersionInPluginJSON，各在全新隔離副本套用一個 mutant，16/16 觀察到失敗。
- SC-D5 staticcheck 設定：在使用者 Temp 目錄的樹副本上，依賴自動發現的舊呼叫報出 ST1018（rc=1，設定被忽略）；改為顯式 `-checks` 後 rc=0；移除 `staticcheck.conf` 時該層依 `[ -f staticcheck.conf ] || return 2` 失敗。
- Mutation baseline：mutate.sh 在隔離副本先跑未突變基線，通過後才逐一（sequential，無並行 job）套用；無並行競爭來源。
- Mutation kill sample：在 3cff0ed（v3 final state，v4 未改動這些函式）上單獨重套 4 個 caught mutant（sha-match-by-name-only、dup-name-case-sensitive、version-mismatch-ignored、locale-not-pinned），4/4 被殺，失敗測試皆可由該 mutant 解釋；樣本 4/19，只能佐證前兩項控制。Verifier round 1 在 3cff0ed 另行手動套用 8 個 SPEC mutant 全數被殺。
- Kill attribution 核對：final run 每個 killed 行的失敗測試都與 mutant 所在函式有因果關係（例如 sha-match-by-name-only → BlankVersion_NoManifestFetch 的 panicProber）。

## Layers not run as specified

- N-A：型別檢查獨立層（Go 編譯即型別檢查，由 build 與 vet 層覆蓋）。
- UNAVAILABLE：`tools/parity` 輸出契約 gate — 本機 Windows 被 build constraint 排除且需要 pinned oracle checkout；由 CI parity.yml 執行。本變更未新增 corpus case（結構上無法離線），只更新一則 waiver 理由。
- SUBSTITUTED：無。
- NOT REACHED：無。
- DEPENDENCY UNMET：無。

## Dismissed concerns

- squad input-space「parsePackages 重複名稱檢查順序與 oracle 不同」— 駁回：oracle yml_schema.py:1303-1310 在每個 entry 解析後立即檢查重複，順序一致。
- mutation 第一輪 `probe-error-unsanitized` 被歸因給 `TestFetchGit_CleansUpTempCloneOnFailure`（internal/marketplace，不 import authoring）— 該失敗與 mutant 無因果；同一 log 中 `TestGitCommitProber_SanitizesTokenInError` 以 token 洩漏訊息失敗才是真 kill；final run 歸因已正確。

## Structural blind spot

- SHA 探測、manifest 版本比對、default-branch tip 比對三類核心新行為無法在 built binary 上離線重現：CLI 的 `owner/repo` 一律解析到 github.com，本地路徑來源被視為本地套件跳過。它們只由函式層測試（真 git 對 t.TempDir() repo）與 cmd 層以 fixture seam 注入的測試證明；realexec 只釘離線契約。
- 伺服器端 `uploadpack.allowAnySHA1InWant` 關閉時的行為（D-a）無本地 fixture。
- parity gate 未在本機執行。

## Honest notes

- Verifier round 1（3cff0ed，verdict failed）：finding 1 staticcheck 層在使用者 Temp 目錄的 worktree 上失敗（設定檔未被讀取，main 上同樣重現）→ SC-D5 修正 gate.sh；finding 3 manifest 端 TrimSpace 無殺傷測試 → SC-B22；finding 4 顯式空字串 version/ref 未比照 oracle 拒絕 → SC-A8；finding 5 描述更正（探測與 manifest 讀取只在 SHA 已被 ls-remote 列出時共用一次 fetch，需探測且有精確 version 時各一次）已寫入 SPEC Revisions；finding 2 為下列既有 flaky。第六輪 gate 於 f300338 全綠。紀錄：`.scratch/marketplace-check-outdated/verification.md`。
- 第一輪 gate 於 changed-line-coverage 停下（273/294）；補 13 個行為測試與一個 refactor 後 297/298；補「無回應遠端」測試時發現真實缺陷：deadline 殺掉 git fetch 後 `Wait` 因 git-remote-https 孫程序握住管線而無限等待，以 `WaitDelay` 修正（63ad196→a3ce3b1）。第二輪 mutation 因 refactor 移動 anchor 而以「anchor 不存在」失敗（fail-closed），修 anchor 後重跑。第四輪全綠於 a3ce3b1；squad after-implement 後新增 v3 三項並重跑，第五輪全綠於 3cff0ed。
- 既有測試 `TestFetchGit_CleansUpTempCloneOnFailure`（internal/marketplace）讀全域 temp 目錄計數，並行套件執行時會 flaky；base 與 HEAD 在乾淨 temp 下皆 8/8 通過。本變更之外。
- 既有 `ListRefs` 的 `git ls-remote` 未設 `WaitDelay`，與本變更新增的三個子程序不一致。本變更之外。
- parity waiver `doctor-config-duplicate-names` 的 reason 已更新以反映 D-c；taxonomy/fields 不變。
- `git worktree add` 在本機的長路徑失敗兩次（scratchpad 路徑過長），改以 `core.longpaths=true` 重建；baseline 與 RED 重建的 worktree 皆位於 base。
- squad after-implement 的 class-3 觀察：RED commit 內含編譯用 stub（commit message 自陳）；apm.yml 非 mapping、`truncateRunes` 短路徑、`asConfigValidationError(nil)` 三個測試為 trivial armor，未做突變證明。
