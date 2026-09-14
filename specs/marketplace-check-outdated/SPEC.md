# SPEC — marketplace check / outdated 缺口修正與 SHA 釘選支援 (Tier 3)

- `spec_version`: v0.1
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
4. `ref: HEAD` 在 `check` 報錯，與 `pack` 一致。

本 SPEC 有兩類變更：**補齊 parity**（oracle 已有、apm-go 缺）與**刻意偏離**（oracle 缺陷，apm-go 超越）。偏離處在程式碼註解標記 `apm-go-only` 並引 oracle file:line。

## Scenarios

每個情境對應至少一個同名測試。`RG` = 本地 `t.TempDir()` git repo，由 `initGitRepoWithTags` 建立，never network。cmd 層測試透過 `withFixtureRemoteLister` 把 `owner/repo` 導向 RG。

### A. Authoring config schema（補齊 parity；影響所有讀 `LoadAuthoringConfig` 的指令）

- SC-A1 `LoadAuthoringConfig_MissingName_Rejected`：packages[0] 缺 `name`，expect error 文字 `'packages[0].name' is required`。
- SC-A2 `LoadAuthoringConfig_MissingSource_Rejected`：packages[0] 缺 `source`，expect `'packages[0].source' is required`（取代現有 `marketplace source is empty`；`source: ""` 空字串仍走 `manifest.ValidateMarketplaceSource` 現有訊息）。
- SC-A3 `LoadAuthoringConfig_RemoteWithoutVersionOrRef_Rejected`：遠端條目 `bare` 既無 `version` 也無 `ref`，expect `packages[0] ('bare'): remote packages require at least one of 'version' or 'ref'`；本地 `./x` 條目無此限制（子測試）。
- SC-A4 `LoadAuthoringConfig_DuplicateNameCaseInsensitive_Rejected`：`Dup` 與 `dup`，expect `Duplicate package name 'dup' (packages[0] and packages[1])`；`authoring.DuplicatePackageNames` 保留為防禦層。
- SC-A5 `MarketplaceCheck_ConfigError_ExitsTwo` / `MarketplaceOutdated_ConfigError_ExitsTwo`：SC-A1 到 A4 任一，`check` 與 `outdated` 印 ` x marketplace config error: <msg>` 到 stdout，exit 2。
- SC-A6 `LoadAuthoringConfig_IsConfigValidationError`：新增判定函式，區分「缺設定檔」（exit 1，訊息不變）與「驗證錯誤」（exit 2）。

### B. `check` 釘選驗證

- SC-B1 `CheckPackages_ShaPin_MatchesListedCommit_NoProbe`（偏離）：RG 有 tag `v1.0.0` 指向 commit C；`ref: <C>`，expect OK，且 CommitProber 為 panic fake（證明未探測）。
- SC-B2 `CheckPackages_ShaPin_NotAtAnyRef_ProbeSucceeds`（偏離）：RG 有兩個 commit，`ref: <第一個 commit>`（無 ref 指向），expect OK，且探測子程序為 `git fetch --depth 1 -- <url> <sha>` 進入暫存 bare repo。
- SC-B3 `CheckPackages_ShaPin_Missing_ProbeFails_NotFound`（偏離）：`ref: 0000…0001`，expect `Ref '0000…0001' not found`，Reachable=true、VersionFound=false、RefOK=false，exit 1。
- SC-B4 `CheckPackages_ShaProbe_TempDirAlwaysRemoved`：SC-B2 與 SC-B3 之後暫存目錄不存在。
- SC-B5 `CheckPackages_ShaProbe_NetworkError_Unreachable`：探測子程序回傳非 `not our ref` 的錯誤（fake git），expect Reachable=false，detail 為經 `SanitizeGitOutput` 的錯誤摘要，截斷 60 字元。
- SC-B6 `CheckPackages_ShaProbe_TimesOut`：探測逾時（`listRefsTimeout` 縮短），expect error 含 `timed out`，Reachable=false。
- SC-B7 `CheckPackages_FullRefName_Accepted`（parity）：`ref: refs/tags/v1.0.0` 與 `ref: refs/heads/main`，expect OK。
- SC-B8 `CheckPackages_HeadRef_Rejected`（parity + 提示）：`ref: HEAD`，expect detail `Ref 'HEAD' not found (run 'apm-go marketplace package set <name> --ref HEAD' to pin a SHA)`，exit 1。合成 HEAD 條目仍存在於 `ListRefs`（`set --ref HEAD` 依賴）。
- SC-B9 `CheckPackages_VersionRange_TagPatternFallback`（parity）：RG tag `tool_v1.2.0`，`version: ^1.0.0`、`tag_pattern` 未設（預設 `v{version}`），expect OK。推斷順序 `v{version}`、`{version}`、`{name}_v{version}`、`{name}--v{version}`、`{name}-v{version}`，僅在設定 pattern 零匹配時啟用，只看 `refs/tags/`。
- SC-B10 `MarketplaceCheck_Verbose_PrintsResolvingLines`（parity）：`-v` 對遠端條目印 `Resolving <name> via <host>: <url>`（host 為 `github.com` 等；shorthand 顯示 `default host`），對本地條目印 `Skipping <name> -- local path, no network check`，皆在表格之前、stdout。
- SC-B11 `MarketplaceCheck_Wording_MatchesOracle`（parity）：detail 文字 `Ref '<ref>' not found`、`No tag matching '<range>'`、`No cached refs (offline)`；失敗摘要 ` x <N> entries have issues` 後 exit 1 靜默（不再印 `check failed: …`）；成功摘要 ` + All <N> entries OK`。表格欄名、標題與框線不在本 SPEC 範圍。
- SC-B12 `CheckPackages_VersionRange_ExcludesPrerelease_UnlessOptIn`（parity）：RG tag `v2.0.0-rc.1` 與 `v1.0.0`，`version: >=1.0.0`，expect 匹配 `v1.0.0`；`include_prerelease: true` 時 `v2.0.0-rc.1` 參與。

### C. `outdated` SHA 釘選

- SC-C1 `OutdatedPackages_ShaPinWithVersion_NewerTag_Upgradable`（偏離）：RG tag `v1.0.0`（commit C）與 `v1.1.0`；`ref: <C>`、`version: 1.0.0`，expect Current=`v1.0.0`、Range=`--`、LatestInRange=`v1.0.0`、LatestOverall=`v1.1.0`、Status=`[!]`、Upgradable=true。
- SC-C2 `OutdatedPackages_ShaPinWithVersion_UpToDate`：同上但只有 `v1.0.0`，expect Status=`[+]`、Upgradable=false。
- SC-C3 `OutdatedPackages_ShaPinWithVersion_NoMatchingTags`：無 tag，expect Status=`[!]`、Note=`No matching tags found`、Upgradable=false（同 oracle 的 version-range 路徑）。
- SC-C4 `OutdatedPackages_ShaPinWithoutVersion_TipMoved_Upgradable`（偏離）：`ref: <舊 commit>`，default branch tip 為新 commit，expect Current=`<sha12>`、LatestOverall=`<tip sha12>`、Note=`Default branch tip moved`、Status=`[!]`、Upgradable=true。
- SC-C5 `OutdatedPackages_ShaPinWithoutVersion_AtTip_UpToDate`：`ref` 等於 tip，expect `[+]`。
- SC-C6 `OutdatedPackages_ShaPinWithoutVersion_NoHeadEntry_IconX`：`ListRefs` 無 `HEAD` 條目，expect `[x]`、Note=`Remote advertised no HEAD`。
- SC-C7 `OutdatedPackages_NamedRefPin_StillSkipped`（parity）：`ref: v1.0.0` 與 `ref: main`，expect `[i]`、Note=`Pinned to ref; skipped`、lister 為 panic fake。
- SC-C8 `OutdatedPackages_VersionRange_TagPatternFallback`（parity）：同 SC-B9 的推斷規則。
- SC-C9 `MarketplaceOutdated_Wording_MatchesOracle`（parity）：Note 為 `Pinned to ref; skipped`、`No version range`、`No matching tags found`；offline Note 為 `Offline mode: no cached refs for '<source>' (package '<name>'). Run a build online first.` 截斷 60 字元；ls-remote 失敗 Note 為錯誤文字截斷 60 字元；exit 1 靜默，不再印 ` x outdated: …`；`-v` 印 `    <N> upgradable entries`（四空格，無符號）。
- SC-C10 `OutdatedPackages_ShaPin_Offline_IconX`：SHA 釘選在 `--offline` 下 expect `[x]` 與 offline Note，不觸網。

### D. Gate 覆蓋（realexec，無網路）

- SC-D1 realexec 步驟 `mkt-check-schema-{name,source,verref,dup}`：四種 schema 錯誤各一，assert rc=2 與 stdout 含 oracle 訊息。
- SC-D2 realexec 步驟 `mkt-check-offline`（遠端 SHA 釘選 + `--offline`，assert rc=1、stdout 含 `No cached refs (offline)`）與 `mkt-outdated-offline`（assert rc=0、stdout 含 `Offline mode: no cached refs`）。
- SC-D3 realexec 步驟 `mkt-check-local-verbose`：全本地套件 `check -v`，assert rc=0、stdout 含 `Skipping <name> -- local path, no network check` 與 `All 1 entries OK`。
- SC-D4 `tools/gate/mutants.txt` 新增：`sha-match-by-name-only`（移除 commit 比對）、`probe-skipped`（探測恆回 true）、`dup-name-case-sensitive`、`tip-compare-inverted`。

## Must NOT

- Must NOT：本地（`./`）套件在 `check` 與 `outdated` 觸發任何 git 子程序；`--offline` 下任何遠端條目觸發網路。
- Must NOT：`marketplace package add/set --ref HEAD` 解析為 SHA 的行為改變（`TestGitRefLister_ListRefs_IncludesHEAD` 與 editor 測試不變）。
- Must NOT：`pack`、`doctor`、`marketplace package` 對合法設定的行為與 exit code 改變；它們對本 SPEC 新拒絕的設定回傳與 oracle 相同的錯誤訊息，exit code 維持各自現有對應。
- Must NOT：測試或 gate 連網；所有 git 操作對 `t.TempDir()` repo。
- Must NOT：每個遠端套件超過一次 `git ls-remote`；fetch 探測只在「40 字元小寫 hex 且 ls-remote 無 commit 命中」時發生，且每次探測後暫存目錄被移除。
- Must NOT：新的 git 子程序未經 `gitops.ApplyCloneEnv` / `ApplySecureGitEnv`；錯誤文字未經 `SanitizeGitOutput`。
- Must NOT：`tools/parity/cases/` 任何現有 case 出現新的未 waive 差異。
- Must NOT：`include_prerelease` 旗標與條目層級覆蓋的既有語意改變。
- Must NOT：現有測試被刪除或斷言被放寬。本 SPEC 明列要改的斷言：`TestMarketplaceCheck_DuplicatePackageNames_WarnsButExitsZero`（改為 exit 2）、`TestOutdatedPackages_IconI_PinnedRefLocalOrNoRange_NeverTouchesNetwork`（`no-range` 條目改為 schema 層拒絕，測試改用 `ref: v1.0.0` 與本地條目）、`TestCheckPackages_UnpinnedRemotePackage_NothingToVerify`（保留，記錄為 in-memory cfg 的防禦行為）、以及斷言舊措辭的測試逐一改為 oracle 措辭。
- Must NOT：`marketplace config error:` 以外的 `LoadAuthoringConfig` 訊息（缺設定檔、兩檔並存）改變。

## Failure model (Tier 3)

| Failure mode | Check that catches it |
|---|---|
| SHA 探測對私有或無回應遠端無限等待 | SC-B6：逾時測試，`listRefsTimeout` 縮短後探測必須在期限內回錯 |
| 探測留下暫存 bare repo | SC-B4：成功與失敗路徑後斷言目錄不存在 |
| 探測錯誤文字洩漏 URL 內的 token | SC-B5 以含 `x-access-token:ghp_…` 的 URL 跑 fake git，斷言輸出不含 token（沿用 refcheck_test 現有模式） |
| schema 收緊誤拒合法設定，`pack`/`doctor` 連帶失效 | 既有 `pack`、`doctor`、`package add/set/remove` 套件測試 + realexec 現有 pack 步驟全綠 |
| SHA 釘選誤報「最新」 | SC-C1、SC-C4：新 tag / 新 tip 必須計入 Upgradable 並 exit 1 |
| exit 1 改為靜默後 cobra 吞掉 exit code | cmd 測試斷言 `exitCodeOf(err)==1` 且 stdout 無額外 error 行 |
| name-only 比對回歸 | mutants `sha-match-by-name-only`、`probe-skipped` 必須被殺 |
| 推斷 fallback 把分支名當 tag | SC-B9：RG 建立分支 `v9.9.9`，斷言不被視為版本 |
| 重複名稱檢查大小寫敏感回歸 | mutant `dup-name-case-sensitive` |

## Setup plan

- Tools to install: none（git、go 已存在）。
- Git isolation: 現有 worktree `.claude/worktree/fix-marketplace-outdated`，分支 `fix/markteplace-outdated`，base `main`。Cadence：SPEC 核准 commit → 每個行為一個 RED commit（僅測試）+ 一個 GREEN commit（僅實作）→ refactor 獨立 commit → gate/evidence commit。
- Files the gate will add/modify, by path: 無新檔；修改 `tools/gate/realexec.sh`（SC-D1 到 D3 步驟）、`tools/gate/mutants.txt`（SC-D4）。Evidence 落在 `.scratch/marketplace-check-outdated/evidence.md`，squad 紀錄在 `.scratch/marketplace-check-outdated/squad/`。
- New dependencies: none。
- 新增內部 seam：`authoring.CommitProber` 介面與 `DefaultCommitProber`（`git fetch --depth 1`），與 `RefLister` 同一注入模式；`cmd` 層測試透過 `withFixtureRemoteLister` 現有 hook 加 prober 版本。
- 文件：README 未描述這兩個指令，不需更新。`ARCHITECTURE.md` §2 的 `marketplace/authoring` 條目補 `CommitProber` 入口；`AGENTS.md` 無變更。
- 明確排除：表格欄名與框線；`No marketplace config found` 訊息（跨六個指令共用）；parity corpus case（結構上無法離線）；ADO / sourceBase 解析（apm-go 未支援 sourceBase，另案）；subdir 感知。

## Approval

（待 v1 送審）

## Revisions

- 2026-09-14 — exploration round 1：四項設計決策由使用者裁定（見背景）。其餘缺口採補齊 oracle 行為之預設，未另詢問。
