# SPEC — marketplace check / outdated 缺口修正與 SHA 釘選支援 (Tier 3)

- `spec_version`: v2
- `status`: approved
- `tier`: 3
- `scope`: marketplace-check-outdated
- `base_ref`: main (bf18093)
- `oracle`: microsoft/apm pinned b75a02b1（tools/parity/oracle.pin），行為與 v0.30.0 相同（check.py 只差一行 ADO org 推導，outdated.py 零變更）

## 背景

`apm-go marketplace check` 與 `marketplace outdated` 於 M3（a3bdff4）從設計文件移植，未逐行對照 oracle。兩個指令沒有 parity corpus case（parity 工作流禁止 argv 引用網路 URL，而遠端來源只接受 URL 或 `owner/repo` 簡寫），所以缺口未被 gate 攔到。

使用者裁定（2026-09-14）：
1. `check` 對 40 字元 SHA：先比對 `git ls-remote` 的 commit 欄位，未命中再以 `git fetch --depth 1 <url> <sha>` 探測。
2. `outdated` 對 SHA 釘選：有 `version` 比 tag，無 `version` 比 default branch tip。tag 名稱與分支名稱釘選維持跳過。
3. 輸出措辭對齊 oracle；`outdated` exit 1 改為靜默離開；config 驗證錯誤 exit 2。
4. `ref: HEAD` 在 `check` 報錯，與 `pack` 同樣拒絕（訊息不同）。

squad after-spec 折入後的三項決策（使用者裁定，見 Revisions round 2、3）：
- D-a：伺服器拒絕任意 SHA fetch（`uploadpack.allowAnySHA1InWant` 關閉）時 git 回 `not our ref`，與不存在無法區分。裁定：歸為 `Ref '<sha>' not found`，限制記入 failure model。
- D-b：SHA 釘選且 `version` 為 range（`isDisplayVersion` 為 false：以 `^ ~ < > =` 開頭、含空白或 `*`）時維持 `Pinned to ref; skipped`；只有精確 semver 的 `version` 走 SC-C1。
- D-c：schema 四規則在 `LoadAuthoringConfig` 一律生效（oracle 的 `load_marketplace_config` 對 check、outdated、pack、doctor、package set 都嚴格，doctor.py:240 把驗證錯誤報為 config error）。既有測試中違反規則的 fixture 補 `ref` 或 `version`，斷言不放寬。

使用者補充裁定 5（2026-09-14，round 3）：Claude Code 安裝 plugin 時會比對 marketplace.json 的 plugin `version` 與 plugin 在該 ref 上的 manifest version，不一致拒絕安裝。`pack` 只在 `version` 是精確 semver 時把它寫進 marketplace.json（build/metadata.go `isDisplayVersion`；range 不輸出，改用遠端 apm.yml 的版本）。因此 `check` 對「有 `ref` 且 `version` 為精確 semver」的條目，取得該 ref 上的 manifest version，不相等即失敗（SC-B13 到 B19，apm-go-only）。

本 SPEC 有兩類變更：**補齊 parity**（oracle 已有、apm-go 缺）與**刻意偏離**（oracle 缺陷，apm-go 超越）。偏離處在程式碼註解標記 `apm-go-only` 並引 oracle file:line。SC-B12 的 `include_prerelease` 覆蓋屬補齊 parity（check.py:190 呼叫 `_extract_tag_versions`，__init__.py 讀 `entry.include_prerelease`）。

SHA 偵測沿用既有 `shaRefPattern`（editor.go）與 `sha40LowerRe`（build/builder.go）的小寫 40-hex 慣例；大寫或縮寫 SHA 一律視為具名 ref。

## Scenarios

每個情境對應至少一個同名測試。`RG` = 本地 `t.TempDir()` git repo，由 `initGitRepoWithTags` 建立，never network。cmd 層測試透過 `withFixtureRemoteLister` 把 `owner/repo` 導向 RG。

### A. Authoring config schema（補齊 parity；影響所有讀 `LoadAuthoringConfig` 的指令）

- SC-A1 `LoadAuthoringConfig_MissingName_Rejected`：packages[0] 缺 `name`，expect error 文字 `'packages[0].name' is required`；子測試 `name: ""` expect `'packages[0].name' must be a non-empty string`。
- SC-A2 `LoadAuthoringConfig_MissingSource_Rejected`：packages[0] 缺 `source`，expect `'packages[0].source' is required`（取代現有 `marketplace source is empty`；`source: ""` 空字串仍走 `manifest.ValidateMarketplaceSource` 現有訊息）。
- SC-A3 `LoadAuthoringConfig_RemoteWithoutVersionOrRef_Rejected`：遠端條目 `bare` 既無 `version` 也無 `ref`，expect `packages[0] ('bare'): remote packages require at least one of 'version' or 'ref'`；本地 `./x` 條目無此限制（子測試）。
- SC-A4 `LoadAuthoringConfig_DuplicateNameCaseInsensitive_Rejected`：`Dup` 與 `dup`，expect `Duplicate package name 'dup' (packages[0] and packages[1])`。`authoring.DuplicatePackageNames` 與 doctor 的 `checkDuplicateNames` 保留為防禦層，與 oracle 的 `_warn_duplicate_names` 及 doctor check 6 同樣在嚴格 loader 下不可達。
- SC-A5 `MarketplaceCheck_ConfigError_ExitsTwo` / `MarketplaceOutdated_ConfigError_ExitsTwo`：SC-A1 到 A4 任一，`check` 與 `outdated` 印 ` x marketplace config error: <msg>` 到 stdout，exit 2。
- SC-A6 `LoadAuthoringConfig_IsConfigValidationError`：新增判定函式，區分「缺設定檔」（exit 1，訊息不變）與「驗證錯誤」（exit 2）。
- SC-A7 `Doctor_MarketplaceConfig_ValidationError_ReportsConfigError`：doctor 對 SC-A3/A4 的設定回報 `apm.yml marketplace block has errors: <msg 截斷 60>`（oracle doctor.py:240），`passed=false`；既有 `TestDoctor_MarketplaceConfig_ApmYml` 的 fixture 與斷言依此改寫。

### B. `check` 釘選驗證

- SC-B1 `CheckPackages_ShaPin_MatchesListedCommit_NoProbe`（偏離）：RG 有 tag `v1.0.0` 指向 commit C；`ref: <C>`，expect OK，且 CommitProber 為 panic fake（證明未探測）。子測試：SHA 恰為分支 tip 而非任何 tag，同樣 OK 且不探測。
- SC-B2 `CheckPackages_ShaPin_NotAtAnyRef_ProbeSucceeds`（偏離）：RG 有兩個 commit，`ref: <第一個 commit>`（無 ref 指向），expect OK，且探測子程序為 `git fetch --depth 1 -- <url> <sha>` 進入暫存 bare repo。
- SC-B3 `CheckPackages_ShaPin_Missing_ProbeFails_NotFound`（偏離）：`ref: 0000…0001`，git 回 `not our ref`，expect `Ref '0000…0001' not found`，Reachable=true、VersionFound=false、RefOK=false，exit 1。伺服器拒絕任意 SHA fetch 時結果相同（D-a）。
- SC-B4 `CheckPackages_ShaProbe_TempDirAlwaysRemoved`：SC-B2、SC-B3 與 SC-B13、SC-B19 之後暫存目錄不存在。
- SC-B5 `CheckPackages_ShaProbe_NetworkError_Unreachable`：探測子程序回傳非 `not our ref` 的錯誤（fake git），expect Reachable=false，detail 為經 `SanitizeGitOutput` 的錯誤摘要，截斷 60 字元。
- SC-B6 `CheckPackages_ShaProbe_TimesOut`：探測逾時（`listRefsTimeout` 縮短），expect error 含 `timed out`，Reachable=false。子測試：manifest clone 逾時同樣。
- SC-B7 `CheckPackages_FullRefName_Accepted`（parity）：`ref: refs/tags/v1.0.0` 與 `ref: refs/heads/main`，expect OK。
- SC-B8 `CheckPackages_HeadRef_Rejected`（parity 拒絕 + apm-go-only 提示）：`ref: HEAD`，expect detail `Ref 'HEAD' not found (run 'apm-go marketplace package set <name> --ref HEAD' to pin a SHA)`，exit 1。括號提示是 apm-go-only；oracle check.py:183 只有 `Ref 'HEAD' not found`，pack 的拒絕訊息另為 `HeadNotAllowedError`。合成 HEAD 條目仍存在於 `ListRefs`（`set --ref HEAD` 依賴）。
- SC-B9 `CheckPackages_VersionRange_TagPatternFallback`（parity）：RG tag `tool_v1.2.0` 與分支 `v9.9.9`，`version: ^1.0.0`、`tag_pattern` 未設（預設 `v{version}`），expect OK 且 `v9.9.9` 不被視為版本。推斷順序 `v{version}`、`{version}`、`{name}_v{version}`、`{name}--v{version}`、`{name}-v{version}`，僅在設定 pattern 零匹配時啟用，只看 `refs/tags/`。
- SC-B10 `MarketplaceCheck_Verbose_PrintsResolvingLines`（parity）：`-v` 對遠端條目印 `Resolving <name> via <host>: <url>`（host 為 `github.com` 等；shorthand 顯示 `default host`），對本地條目印 `Skipping <name> -- local path, no network check`，皆在表格之前、stdout。
- SC-B11 `MarketplaceCheck_Wording_MatchesOracle`（parity）：detail 文字 `Ref '<ref>' not found`、`No tag matching '<range>'`、`No cached refs (offline)`；失敗摘要 ` x <N> entries have issues` 後 exit 1 靜默（不再印 `check failed: …`）；成功摘要 ` + All <N> entries OK`。表格欄名、標題與框線不在本 SPEC 範圍。
- SC-B12 `CheckPackages_VersionRange_ExcludesPrerelease_UnlessOptIn`（parity）：RG tag `v2.0.0-rc.1` 與 `v1.0.0`，`version: >=1.0.0`，expect 匹配 `v1.0.0`；`include_prerelease: true` 時 `v2.0.0-rc.1` 參與。

manifest version 比對（裁定 5，apm-go-only）：條目同時有 `ref`（任何形式）與精確 semver 的 `version` 時，在 ref 驗證通過後，以 `ManifestVersionFetcher` 取得該 ref 上的 manifest version：先讀 `<subdir>/.claude-plugin/plugin.json` 的 `version`，沒有該檔再讀 `<subdir>/apm.yml` 的 `version`（與 `pack` 補 metadata 讀的同一檔）。production fetcher 是一次 clone（SHA 用完整 clone 加 checkout，具名 ref 用 `--depth 1 --branch`，同 build/metadata.go `cloneAtRef`），共用 `listRefsTimeout`，暫存目錄必移除。比對為字串相等，兩邊各去除前導 `v`。

- SC-B13 `CheckPackages_RefWithDisplayVersion_ManifestMatches_OK`：RG commit C 含 `.claude-plugin/plugin.json` `{"version":"1.0.0"}`；`ref: <C>`、`version: 1.0.0`，expect OK。子測試：`ref: v1.0.0`（tag 名稱）同樣比對並 OK。
- SC-B14 `CheckPackages_RefWithDisplayVersion_ManifestMismatch_Fails`：plugin.json version `1.1.0`，expect detail `Version '1.0.0' does not match plugin manifest version '1.1.0' at ref '<C>'`，Reachable=true、VersionFound=true、RefOK=false，exit 1。
- SC-B15 `CheckPackages_RefWithDisplayVersion_FallsBackToApmYML`：無 plugin.json，`apm.yml` `version: 1.0.0` expect OK；`version: 2.0.0` expect SC-B14 的 mismatch detail（manifest 值 `2.0.0`）。
- SC-B16 `CheckPackages_RefWithDisplayVersion_NoManifest_SkipsComparison`：兩檔皆無，expect OK（無法比對不視為失敗）。
- SC-B17 `CheckPackages_RefWithDisplayVersion_SubdirRespected`：`subdir: plugins/x`，manifest 只放在 `plugins/x/.claude-plugin/plugin.json`，expect 讀到並比對。
- SC-B18 `CheckPackages_RefWithRangeVersion_NoManifestFetch`：`ref: <C>`、`version: ^1.0.0`，fetcher 為 panic fake，expect OK 且未取 manifest。無 `ref` 的條目同樣不取。
- SC-B19 `CheckPackages_ManifestFetch_CloneFailure_Unreachable`：fake git 使 clone 失敗，expect Reachable=false，detail 為 `SanitizeGitOutput` 後的摘要截斷 60 字元；逾時與暫存目錄清除納入 SC-B4、SC-B6 的斷言範圍。

### C. `outdated` SHA 釘選

Current 欄的來源：以有效 tag_pattern（設定值，零匹配時套 SC-B9 推斷）渲染 `version`，例如 `RenderTag("v{version}", name, "1.0.0")` = `v1.0.0`。不做 SHA→tag 反查；SHA 與該 tag 的 commit 是否一致是 `check` 的職責，`outdated` 不判定。

- SC-C1 `OutdatedPackages_ShaPinWithVersion_NewerTag_Upgradable`（偏離）：RG tag `v1.0.0`（commit C）與 `v1.1.0`；`ref: <C>`、`version: 1.0.0`，expect Current=`v1.0.0`、Range=`--`、LatestInRange=`v1.0.0`、LatestOverall=`v1.1.0`、Status=`[!]`、Upgradable=true。子測試：`tag_pattern: "{name}_v{version}"` 時 Current=`tool_v1.0.0`。
- SC-C2 `OutdatedPackages_ShaPinWithVersion_UpToDate`：同上但只有 `v1.0.0`，expect Status=`[+]`、Upgradable=false。
- SC-C3 `OutdatedPackages_ShaPinWithVersion_NoMatchingTags`：無 tag，expect Status=`[!]`、Note=`No matching tags found`、Upgradable=false（同 oracle 的 version-range 路徑）。
- SC-C4 `OutdatedPackages_ShaPinWithoutVersion_TipMoved_Upgradable`（偏離）：`ref: <舊 commit>`，default branch tip 為新 commit，expect Current=`<sha12>`、LatestOverall=`<tip sha12>`、Note=`Default branch tip moved`、Status=`[!]`、Upgradable=true。
- SC-C5 `OutdatedPackages_ShaPinWithoutVersion_AtTip_UpToDate`：`ref` 等於 tip，expect `[+]`。
- SC-C6 `OutdatedPackages_ShaPinWithoutVersion_NoHeadEntry_IconX`：`ListRefs` 無 `HEAD` 條目，expect `[x]`、Note=`Remote advertised no HEAD`。
- SC-C7 `OutdatedPackages_NamedRefPin_StillSkipped`（parity）：`ref: v1.0.0`、`ref: main`、大寫 40-hex，expect `[i]`、Note=`Pinned to ref; skipped`、lister 為 panic fake。
- SC-C8 `OutdatedPackages_VersionRange_TagPatternFallback`（parity）：同 SC-B9 的推斷規則。
- SC-C9 `MarketplaceOutdated_Wording_MatchesOracle`（parity）：Note 為 `Pinned to ref; skipped`、`No version range`、`No matching tags found`；offline Note 為 `Offline mode: no cached refs for '<source>' (package '<name>'). Run a build online first.` 截斷 60 字元；ls-remote 失敗 Note 為錯誤文字截斷 60 字元；upgradable>0 時 exit 1 靜默，不再印 ` x outdated: …`；`-v` 印 `    <N> upgradable entries`（四空格，無符號）。apm-go 沒有 oracle 的非預期例外路徑（每列錯誤都成為 `[x]`），若日後新增必須印 `Failed to check outdated packages: <err>` 後 exit 1。
- SC-C10 `OutdatedPackages_ShaPin_Offline_IconX`：SHA 釘選在 `--offline` 下 expect `[x]` 與 offline Note，不觸網。
- SC-C11 `OutdatedPackages_ShaPinWithRangeVersion_Skipped`（D-b）：`ref: <C>`、`version: ^1.0.0`，expect `[i]`、Note=`Pinned to ref; skipped`、不觸網。

### D. Gate 覆蓋（realexec，無網路）

- SC-D1 realexec 步驟 `mkt-check-schema-{name,source,verref,dup}`：四種 schema 錯誤各一，assert rc=2 與 stdout 含 oracle 訊息。
- SC-D2 realexec 步驟 `mkt-check-offline`（遠端 SHA 釘選 + `--offline`，assert rc=1、stdout 含 `No cached refs (offline)`）與 `mkt-outdated-offline`（assert rc=0、stdout 含 `Offline mode: no cached refs`）。
- SC-D3 realexec 步驟 `mkt-check-local-verbose`：全本地套件 `check -v`，assert rc=0、stdout 含 `Skipping <name> -- local path, no network check` 與 `All 1 entries OK`。
- SC-D4 `tools/gate/mutants.txt` 新增六個 mutant：`sha-match-by-name-only`（移除 commit 比對）、`probe-skipped`（探測恆回 true）、`dup-name-case-sensitive`、`tip-compare-inverted`、`current-render-ignores-pattern`、`version-mismatch-ignored`（manifest 比對恆相等）。確切 `old`/`new` 字串在對應 GREEN commit 內寫入，並以 `tools/gate/mutate.sh` 驗證 `old` 唯一。

## Must NOT

- Must NOT：本地（`./`）套件在 `check` 與 `outdated` 觸發任何 git 子程序；`--offline` 下任何遠端條目觸發網路。
- Must NOT：`marketplace package add/set --ref HEAD` 解析為 SHA 的行為改變（`TestGitRefLister_ListRefs_IncludesHEAD` 與 editor 測試的斷言不變）。
- Must NOT：`pack`、`doctor`、`marketplace package` 對合法設定的行為與 exit code 改變；它們對本 SPEC 新拒絕的設定回傳與 oracle 相同的錯誤訊息，exit code 維持各自現有對應。
- Must NOT：測試或 gate 連網；所有 git 操作對 `t.TempDir()` repo。
- Must NOT：每個遠端套件超過一次 `git ls-remote`；fetch 探測只在「小寫 40-hex 且 ls-remote 無 commit 命中」時發生；manifest clone 只在「有 `ref` 且 `version` 為精確 semver 且 ref 驗證已通過」時發生，每個條目至多一次；`outdated` 永不取 manifest；每次探測或 clone 後暫存目錄被移除。
- Must NOT：新的 git 子程序未經 `gitops.ApplyCloneEnv` / `ApplySecureGitEnv`；錯誤文字未經 `SanitizeGitOutput`。
- Must NOT：`tools/parity/cases/` 任何現有 case 出現新的未 waive 差異。
- Must NOT：`include_prerelease` 旗標與條目層級覆蓋的既有語意改變。
- Must NOT：現有測試被刪除或斷言被放寬。本 SPEC 明列要改的**斷言**：`TestMarketplaceCheck_DuplicatePackageNames_WarnsButExitsZero`（改為 exit 2）、`TestDoctor_MarketplaceConfig_ApmYml`（改走 config error 路徑，SC-A7）、`TestOutdatedPackages_IconI_PinnedRefLocalOrNoRange_NeverTouchesNetwork`（`no-range` 條目改為 schema 層拒絕，測試改用 `ref: v1.0.0` 與本地條目）、`TestCheckPackages_UnpinnedRemotePackage_NothingToVerify`（保留，記錄為 in-memory cfg 的防禦行為）、以及斷言舊措辭的測試逐一改為 oracle 措辭。只改 **fixture**（補 `ref: main` 或 `version`）不改斷言的測試（2026-09-14 以 grep `source: owner/repo$` 無後續 version/ref 量測）：editor_test.go 的五個 `TestSetPackage_*`、schema_test.go 的 `TestLoadAuthoringConfig_PackageEntryFields`、`…_CodexOutput_MissingCategory_NoErrorAtLoadTime`、`…_CodexOutput_AllPackagesHaveCategory_NoError`。RED 階段跑全套件後若再發現同類 fixture，補值並記入 Revisions，不視為 SPEC 變更。
- Must NOT：`marketplace config error:` 以外的 `LoadAuthoringConfig` 訊息（缺設定檔、兩檔並存）改變。

## Failure model (Tier 3)

| Failure mode | Check that catches it |
|---|---|
| SHA 探測對私有或無回應遠端無限等待 | SC-B6：逾時測試，`listRefsTimeout` 縮短後探測必須在期限內回錯 |
| 探測留下暫存 bare repo | SC-B4：成功與失敗路徑後斷言目錄不存在 |
| 探測錯誤文字洩漏 URL 內的 token | SC-B5 以含 `x-access-token:ghp_…` 的 URL 跑 fake git，斷言輸出不含 token（沿用 refcheck_test 現有模式） |
| 伺服器拒絕任意 SHA fetch，存在的 SHA 被報 not found | 已知限制（D-a），無法用本地 fixture 觸發；記入 evidence honest notes，程式碼註解說明 |
| schema 收緊誤拒合法設定，`pack`/`doctor`/`package` 連帶失效 | 既有 `pack`、`doctor`、`package add/set/remove` 套件測試 + realexec 現有 pack 步驟全綠 |
| SHA 釘選誤報「最新」 | SC-C1、SC-C4：新 tag / 新 tip 必須計入 Upgradable 並 exit 1 |
| exit 1 改為靜默後 cobra 吞掉 exit code | cmd 測試斷言 `exitCodeOf(err)==1` 且 stdout 無額外 error 行 |
| name-only 比對回歸 | mutants `sha-match-by-name-only`、`probe-skipped` 必須被殺 |
| 推斷 fallback 把分支名當 tag | SC-B9：RG 建立分支 `v9.9.9`，斷言不被視為版本 |
| 重複名稱檢查大小寫敏感回歸 | mutant `dup-name-case-sensitive` |
| Current 渲染忽略 tag_pattern | SC-C1 子測試 + mutant `current-render-ignores-pattern` |
| manifest version 不一致被放行，cc 安裝時才拒絕 | SC-B14、SC-B15 + mutant `version-mismatch-ignored` |
| manifest clone 對大型 repo 耗時，SHA 需完整 clone | 共用 `listRefsTimeout`（SC-B6 子測試）；成本記入 evidence honest notes |

## Setup plan

- Tools to install: none（git、go 已存在）。
- Git isolation: 現有 worktree `.claude/worktree/fix-marketplace-outdated`，分支 `fix/markteplace-outdated`，base `main`。Cadence：SPEC 核准 commit → 每個行為一個 RED commit（僅測試）+ 一個 GREEN commit（僅實作）→ refactor 獨立 commit → gate/evidence commit。fixture 補值的 commit 與對應 GREEN 分開。
- Files the gate will add/modify, by path: 無新檔；修改 `tools/gate/realexec.sh`（SC-D1 到 D3 步驟）、`tools/gate/mutants.txt`（SC-D4）。Evidence 落在 `.scratch/marketplace-check-outdated/evidence.md`，squad 紀錄在 `.scratch/marketplace-check-outdated/squad/`（`.scratch/` 在 .gitignore，以 `git add -f` 追蹤，與 `.scratch/parity-runner/issues/` 相同）。
- New dependencies: none。
- 新增內部 seam：`authoring.CommitProber` 介面與 `DefaultCommitProber`（`git fetch --depth 1`）、`authoring.ManifestVersionFetcher` 介面與 `DefaultManifestVersionFetcher`（clone at ref 後讀 plugin.json / apm.yml），皆與 `RefLister` 同一注入模式；`cmd` 層測試透過 `withFixtureRemoteLister` 現有 hook 加 prober 與 fetcher 版本。`isDisplayVersion` 由 build/metadata.go 移到 authoring 匯出為 `IsDisplayVersion`，build 改呼叫它（build 已 import authoring，方向合法）。clone 邏輯與 build/metadata.go `cloneAtRef` 重複，因 ARCHITECTURE §1 禁止 authoring → build，下沉共用是另案。
- 文件：README 未描述這兩個指令，不需更新。`ARCHITECTURE.md` §2 的 `marketplace/authoring` 條目補 `CommitProber` 入口；`AGENTS.md` 無變更。
- 明確排除：表格欄名與框線（展示層）；`No marketplace config found` 訊息（跨六個指令共用）；parity corpus case（結構上無法離線）；ADO / sourceBase 解析（apm-go 未支援 sourceBase，另案）；subdir 感知（oracle check.py、outdated.py 不讀 `subdir` 欄位，`ls-remote` 也無檔案層級資訊）。

## Approval

- 2026-09-14 — approves v2 — "核准 v2"（AskUserQuestion 結構化回覆，問題明示 commit 98fd72c 與 Setup plan 授權範圍）

## Revisions

- 2026-09-14 — exploration round 1：四項設計決策由使用者裁定（見背景）。其餘缺口採補齊 oracle 行為之預設，未另詢問。
- 2026-09-14 — v0.1 → v1：evidence-squad after-spec 十六項 finding 折入（紀錄：`.scratch/marketplace-check-outdated/squad/after-spec.md`）。新增 SC-A7、SC-C11、SC-B1 子情境、SC-A1 子情境、第五個 mutant；SC-B8 更正「與 pack 一致」為「同樣拒絕、訊息不同」並標記提示句 apm-go-only；SC-C1 明定 Current 來源；SC-C9 明定靜默 exit 只限 upgradable 路徑；Must NOT 補列 fixture 需補值的測試；三項預設決策 D-a、D-b、D-c 隨本版送審。
- 2026-09-14 — v1 → v2（round 3）：使用者裁定 D-a「報 not found，記錄限制」、D-b「維持 Pinned to ref; skipped」、D-c「loader 一律嚴格」；並補充「當同時設定 version 與 ref 時，cc 安裝 plugin 會比對，不相同會拒絕安裝」，選擇「新增，作為 check 的失敗條件」。新增 SC-B13 到 B19、第六個 mutant、兩列 failure model、`ManifestVersionFetcher` seam。v1 未取得核准即被本輪資訊取代。
- 2026-09-14 — group A GREEN 後全套件掃描（非 SPEC 變更，依 Must NOT 條款記錄）：實際需要補值的 fixture 是 editor_test 五個 `TestSetPackage_*`、schema_test 的 `TestLoadAuthoringConfig_SourceValidation_AcceptsValidShapes`、`_TagPatternValidatedAtLoad`、`_ValidTagPatternsStillLoad`、doctor_test 的 `TestDoctor_MarketplaceConfig_Legacy_PointsAtMigrate`；v2 列出的三個 `_CodexOutput_*` / `_PackageEntryFields` 其實已有 version 或 ref，不需補值。parity waiver `doctor-config-duplicate-names` 的 reason 原本記載「apm-go 不在 load 時拒絕重複名稱」的結構性差異，D-c 生效後該差異消失，reason 已更新，taxonomy 與 fields 不變（剩餘差異仍是 rendering）。
- 2026-09-14 — group B/C/D 實作紀錄（行為與 SPEC 相同，機制與描述的差異記於此）：(1) 探測與 manifest 讀取共用一次 `git fetch --depth 1 -- <url> <ref>` 進暫存 bare repo，manifest 以 `git show FETCH_HEAD:<path>` 讀取，不做完整 clone；本地 git 對 fetch 非 tip SHA 預設允許，實測通過。(2) `TestOutdatedPackages_IconI_PinnedRefLocalOrNoRange_NeverTouchesNetwork` 未改 fixture：`no-range` 條目在函式層仍走 `No version range` 分支（oracle outdated.py:56-71 同樣保留該分支），只有 loader 層拒絕，測試在函式層仍有效。(3) `TestOutdatedPackages_VersionRange_TagPatternFallback` 於 RED 時已通過，因推斷邏輯隨 group B 的共用 `versionTagCandidates` 先落地；保留為回歸保護。(4) SC-B4 在 GREEN 首輪抓到真實缺陷：named return 被 closure 捕捉導致錯誤路徑不清除暫存目錄，已修正。(5) mutants 新增第七個 `probe-error-unsanitized`，覆蓋 token 遮罩；`sha-match-by-name-only` 與 `current-render-ignores-pattern` 的替換字串調整為可編譯形式（未使用變數會讓 mutant 變成 broken 而非 killed）。(6) realexec 另加 `mkt-outdated-schema-verref`，證明 outdated 與 check 共用 exit 2 路徑。
- 2026-09-14 — gate 第一輪在 changed-line-coverage 停下（273/294）：21 行未覆蓋皆為錯誤分支（manifest 解析、size cap、hostile subdir、scratch root 缺失、fetch 逾時與空 stderr、range 無法解析、legacy 驗證、resolvingLabel 的 host 簡寫）。處置：fetch 逾時與失敗文字抽成 `fetchTimedOut` / `gitFailureText`（refactor，行為不變），其餘各補一個行為測試；13 個新測試各以一次性突變在隔離副本證明會失敗（`.scratch/marketplace-check-outdated/throwaway-mutants.txt`）。`truncateRunes` 短路徑、`asConfigValidationError(nil)`、apm.yml 非 mapping 三個測試屬 trivial armor，未做突變證明。gate 的 mutation 層另揭露既有測試 `TestFetchGit_CleansUpTempCloneOnFailure` 讀全域 temp 目錄計數、在並行套件下 flaky（base 與 HEAD 在乾淨 temp 下皆 8/8 通過），屬本變更之外，記入 evidence honest notes。
