# SPEC — marketplace check / outdated 缺口修正與 SHA 釘選支援 (Tier 3)

- `spec_version`: v1
- `status`: draft
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

squad after-spec 折入後的三項預設決策（送審時一併裁定，見 Revisions round 2）：
- D-a：伺服器拒絕任意 SHA fetch（`uploadpack.allowAnySHA1InWant` 關閉）時 git 回 `not our ref`，與不存在無法區分。預設：歸為 `Ref '<sha>' not found`，限制記入 failure model。
- D-b：SHA 釘選且 `version` 為 range（含 `^ ~ < > = x *` 或空白）時，預設維持 `Pinned to ref; skipped`；只有精確 semver 的 `version` 走 SC-C1。
- D-c：schema 四規則在 `LoadAuthoringConfig` 一律生效（oracle 的 `load_marketplace_config` 對 check、outdated、pack、doctor、package set 都嚴格，doctor.py:240 把驗證錯誤報為 config error）。既有測試中違反規則的 fixture 補 `ref` 或 `version`，斷言不放寬。

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
- SC-B4 `CheckPackages_ShaProbe_TempDirAlwaysRemoved`：SC-B2 與 SC-B3 之後暫存目錄不存在。
- SC-B5 `CheckPackages_ShaProbe_NetworkError_Unreachable`：探測子程序回傳非 `not our ref` 的錯誤（fake git），expect Reachable=false，detail 為經 `SanitizeGitOutput` 的錯誤摘要，截斷 60 字元。
- SC-B6 `CheckPackages_ShaProbe_TimesOut`：探測逾時（`listRefsTimeout` 縮短），expect error 含 `timed out`，Reachable=false。
- SC-B7 `CheckPackages_FullRefName_Accepted`（parity）：`ref: refs/tags/v1.0.0` 與 `ref: refs/heads/main`，expect OK。
- SC-B8 `CheckPackages_HeadRef_Rejected`（parity 拒絕 + apm-go-only 提示）：`ref: HEAD`，expect detail `Ref 'HEAD' not found (run 'apm-go marketplace package set <name> --ref HEAD' to pin a SHA)`，exit 1。括號提示是 apm-go-only；oracle check.py:183 只有 `Ref 'HEAD' not found`，pack 的拒絕訊息另為 `HeadNotAllowedError`。合成 HEAD 條目仍存在於 `ListRefs`（`set --ref HEAD` 依賴）。
- SC-B9 `CheckPackages_VersionRange_TagPatternFallback`（parity）：RG tag `tool_v1.2.0` 與分支 `v9.9.9`，`version: ^1.0.0`、`tag_pattern` 未設（預設 `v{version}`），expect OK 且 `v9.9.9` 不被視為版本。推斷順序 `v{version}`、`{version}`、`{name}_v{version}`、`{name}--v{version}`、`{name}-v{version}`，僅在設定 pattern 零匹配時啟用，只看 `refs/tags/`。
- SC-B10 `MarketplaceCheck_Verbose_PrintsResolvingLines`（parity）：`-v` 對遠端條目印 `Resolving <name> via <host>: <url>`（host 為 `github.com` 等；shorthand 顯示 `default host`），對本地條目印 `Skipping <name> -- local path, no network check`，皆在表格之前、stdout。
- SC-B11 `MarketplaceCheck_Wording_MatchesOracle`（parity）：detail 文字 `Ref '<ref>' not found`、`No tag matching '<range>'`、`No cached refs (offline)`；失敗摘要 ` x <N> entries have issues` 後 exit 1 靜默（不再印 `check failed: …`）；成功摘要 ` + All <N> entries OK`。表格欄名、標題與框線不在本 SPEC 範圍。
- SC-B12 `CheckPackages_VersionRange_ExcludesPrerelease_UnlessOptIn`（parity）：RG tag `v2.0.0-rc.1` 與 `v1.0.0`，`version: >=1.0.0`，expect 匹配 `v1.0.0`；`include_prerelease: true` 時 `v2.0.0-rc.1` 參與。

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
- SC-D4 `tools/gate/mutants.txt` 新增五個 mutant：`sha-match-by-name-only`（移除 commit 比對）、`probe-skipped`（探測恆回 true）、`dup-name-case-sensitive`、`tip-compare-inverted`、`current-render-ignores-pattern`。確切 `old`/`new` 字串在對應 GREEN commit 內寫入，並以 `tools/gate/mutate.sh` 驗證 `old` 唯一。

## Must NOT

- Must NOT：本地（`./`）套件在 `check` 與 `outdated` 觸發任何 git 子程序；`--offline` 下任何遠端條目觸發網路。
- Must NOT：`marketplace package add/set --ref HEAD` 解析為 SHA 的行為改變（`TestGitRefLister_ListRefs_IncludesHEAD` 與 editor 測試的斷言不變）。
- Must NOT：`pack`、`doctor`、`marketplace package` 對合法設定的行為與 exit code 改變；它們對本 SPEC 新拒絕的設定回傳與 oracle 相同的錯誤訊息，exit code 維持各自現有對應。
- Must NOT：測試或 gate 連網；所有 git 操作對 `t.TempDir()` repo。
- Must NOT：每個遠端套件超過一次 `git ls-remote`；fetch 探測只在「小寫 40-hex 且 ls-remote 無 commit 命中」時發生，且每次探測後暫存目錄被移除。
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

## Setup plan

- Tools to install: none（git、go 已存在）。
- Git isolation: 現有 worktree `.claude/worktree/fix-marketplace-outdated`，分支 `fix/markteplace-outdated`，base `main`。Cadence：SPEC 核准 commit → 每個行為一個 RED commit（僅測試）+ 一個 GREEN commit（僅實作）→ refactor 獨立 commit → gate/evidence commit。fixture 補值的 commit 與對應 GREEN 分開。
- Files the gate will add/modify, by path: 無新檔；修改 `tools/gate/realexec.sh`（SC-D1 到 D3 步驟）、`tools/gate/mutants.txt`（SC-D4）。Evidence 落在 `.scratch/marketplace-check-outdated/evidence.md`，squad 紀錄在 `.scratch/marketplace-check-outdated/squad/`（`.scratch/` 在 .gitignore，以 `git add -f` 追蹤，與 `.scratch/parity-runner/issues/` 相同）。
- New dependencies: none。
- 新增內部 seam：`authoring.CommitProber` 介面與 `DefaultCommitProber`（`git fetch --depth 1`），與 `RefLister` 同一注入模式；`cmd` 層測試透過 `withFixtureRemoteLister` 現有 hook 加 prober 版本。
- 文件：README 未描述這兩個指令，不需更新。`ARCHITECTURE.md` §2 的 `marketplace/authoring` 條目補 `CommitProber` 入口；`AGENTS.md` 無變更。
- 明確排除：表格欄名與框線（展示層）；`No marketplace config found` 訊息（跨六個指令共用）；parity corpus case（結構上無法離線）；ADO / sourceBase 解析（apm-go 未支援 sourceBase，另案）；subdir 感知（oracle check.py、outdated.py 不讀 `subdir` 欄位，`ls-remote` 也無檔案層級資訊）。

## Approval

（待 v1 送審）

## Revisions

- 2026-09-14 — exploration round 1：四項設計決策由使用者裁定（見背景）。其餘缺口採補齊 oracle 行為之預設，未另詢問。
- 2026-09-14 — v0.1 → v1：evidence-squad after-spec 十六項 finding 折入（紀錄：`.scratch/marketplace-check-outdated/squad/after-spec.md`）。新增 SC-A7、SC-C11、SC-B1 子情境、SC-A1 子情境、第五個 mutant；SC-B8 更正「與 pack 一致」為「同樣拒絕、訊息不同」並標記提示句 apm-go-only；SC-C1 明定 Current 來源；SC-C9 明定靜默 exit 只限 upgradable 路徑；Must NOT 補列 fixture 需補值的測試；三項預設決策 D-a、D-b、D-c 隨本版送審。
