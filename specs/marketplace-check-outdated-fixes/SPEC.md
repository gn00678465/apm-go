# SPEC — marketplace check / outdated review 缺陷修正 (Tier 3)

- `spec_version`: v0.1
- `status`: draft
- `tier`: 3
- `scope`: marketplace-check-outdated-fixes
- `base_ref`: fix/markteplace-outdated (3d59291)
- `predecessor`: specs/archive/marketplace-check-outdated/SPEC.md v4（shipped，不修改）
- `oracle`: microsoft/apm pinned b75a02b1（tools/parity/oracle.pin），本機 D:\Projects2\apm（v0.30.0）

## 背景

封存後，Codex 以唯讀方式 review 本 branch（相對 main，head 3d59291），提出五項缺陷與四處摘要不符。orchestrator 逐項對照程式碼、原 SPEC 與 oracle 後，本 SPEC 收錄六項。

使用者裁定（2026-09-15）：
- 第 2、4、5 項（原 SPEC 未寫的行為）：原話「fix it」。
- 第 6 項：原話「算是 spec 缺口」，處理方式選「修，對齊 oracle」。
- 第 1、3 項違反原 SPEC 已寫明的規則（class 1），直接修。

## 缺陷與證據

| # | class | 位置 | 缺陷 | 依據 |
|---|---|---|---|---|
| 1 | 1 | `internal/marketplace/authoring/refcheck.go:904` | `refMatches` 對小寫 40-hex 也比對 ref 名稱；遠端有名稱等於該 SHA、commit 不同的 ref 時，check 直接通過，不探測 | 原 SPEC:15「先比對 `git ls-remote` 的 commit 欄位，未命中再以 `git fetch --depth 1` 探測」 |
| 2 | 2 | `internal/marketplace/authoring/refcheck_sha.go:293-303` | `showAtFetchHead` 把 `git show` 的 stdout 全部寫入無上限的 `bytes.Buffer`，程序結束後才檢查 `manifestReadMaxBytes` | 原 SPEC 未規定讀取上限的時點 |
| 3 | 1 | `internal/marketplace/authoring/refcheck.go:1001`、`cmd/apm-go/marketplace_authoring.go:428,478-503` | CLI 由 `marketplace.json` 的 `plugins[].source.ref` 建立 current map；SHA 釘選且有 display version 的條目沒有候選 tag 時，`outdatedForPackage` 保留傳入的 current（該 SHA），Current 不是 `--`。原測試傳入 nil current，未涵蓋 | 原 SPEC:80 SC-C3「Current=`--`（D-f：只有存在候選 tag 時才渲染 Current）」 |
| 4 | 2 | `internal/marketplace/authoring/schema.go:706-716,629-632` | `requireNonEmptyString` 只檢查 ScalarNode 與非空；`name: 123`、`name: true` 通過。`source` 沒有型別檢查 | oracle `yml_schema.py:830-831` 以 `_require_str` 讀 `name`、`source`，`:454` 要求 `isinstance(value, str)`；`version`、`ref` 以 `str(version)` 轉換（`:853-862`），數字可通過 |
| 5 | 2 | `internal/marketplace/authoring/refcheck.go:1063,1140` | SHA 釘選的 `version: v1.0.0` 以 `RenderTag("v{version}", …)` 渲染成 `vv1.0.0`；`LatestInRange` 以 `c.tag == row.Current` 比對，永遠不相等 | 原 SPEC:61 check 的 manifest 比對「兩邊各去除前導 `v`」；outdated 未規定 |
| 6 | 2 | `internal/semver/semver.go:121`、`internal/marketplace/tagpattern/tagpattern.go:158`、`refcheck.go:1192` | `semver.IsValid` 以 NPM 解析器判定，接受 `1`、`1.2`、`v1.2.0`；推斷與候選收集因此接受 oracle 拒絕的擷取值 | oracle `semver.py:34-38` `_SEMVER_RE` 為 `^\d+\.\d+\.\d+(-pre)?(+build)?$`（無 `v`）；`_shared.py:16-41` `iter_semver_tags` 與 `tag_pattern.py:147-153` 擷取皆用此文法 |

## Scenarios

每個情境對應至少一個同名測試。`RG` = 本地 `t.TempDir()` git repo，never network。

### A. check 的 SHA 比對

- SC-F1 `CheckPackages_ShaPin_RefNamedLikeSha_DifferentCommit_Probes`：RG 有分支，名稱為 SHA S（小寫 40-hex），指向另一個 commit C；`ref: S`，S 不存在於 RG。expect CommitProber 被呼叫一次，結果 `Ref 'S' not found`、RefOK=false。子測試：S 存在於 RG（無 ref 指向）→ 探測成功，OK。
- SC-F2 `CheckPackages_NamedRef_StillMatchesByName`（回歸）：`ref: main`、`ref: v1.0.0`、`ref: refs/tags/v1.0.0` 仍以名稱比對通過，CommitProber 為 panic fake。

### B. manifest 讀取上限

- SC-F3 `ReadCapped_StopsAtLimit`：以永不結束的 reader 呼叫讀取函式，上限 N。expect 回傳錯誤（文字含 `exceeds N bytes`），讀取的位元組數 ≤ N+1，函式在測試 timeout 內返回。
- SC-F4 `FetchManifestVersion_OversizedManifest_Errors`：RG commit 的 `.claude-plugin/plugin.json` 大小為上限 +1 位元組。expect 錯誤文字含 `exceeds`，暫存目錄已移除，git 子程序已結束。
- SC-F5 `FetchManifestVersion_ManifestAtLimit_Reads`（邊界）：大小恰為上限的合法 JSON 可讀出 version。

### C. outdated 的 Current

- SC-F6 `OutdatedPackages_ShaPinWithVersion_NoMatchingTags_IgnoresCurrentMap`：current map 含 `tool: <SHA>`，無候選 tag。expect Current=`--`、Note=`No matching tags found`。
- SC-F7 `MarketplaceOutdated_ShaPin_NoTags_MarketplaceJson_CurrentDashes`（cmd 層）：工作目錄有 `marketplace.json`，`plugins[].source.ref` 為該 SHA。expect 表格該列 Current 欄為 `--`。
- SC-F8 `OutdatedPackages_ShaPinWithVersion_LeadingV_SameAsBare`：`version: v1.0.0` 與 `version: 1.0.0` 在 SC-C1、SC-C2 的 fixture 下產生相同的 Current（`v1.0.0`）、LatestInRange、LatestOverall、Status、Upgradable。子測試：`tag_pattern: "{name}_v{version}"` 時 Current=`tool_v1.0.0`。

### D. schema 型別

- SC-F9 `LoadAuthoringConfig_NonStringNameOrSource_Rejected`：`name: 123`、`name: true`、`source: 123` 各自 expect `'packages[0].name' must be a non-empty string` 或 `'packages[0].source' must be a non-empty string`；`check` 與 `outdated` exit 2。YAML 引號字串 `name: "123"` 通過。
- SC-F10 `LoadAuthoringConfig_NumericVersionOrRef_Accepted`（parity 回歸）：`version: 1.0` 讀入為 `"1.0"`、`ref: 123` 讀入為 `"123"`，不報錯。

### E. 版本文法

- SC-F11 `SemverIsValid_OracleGrammar`：表格測試。接受 `1.2.3`、`1.2.3-rc.1`、`1.2.3+build.1`、`1.2.3-rc.1+b`、`01.2.3`；拒絕 `1`、`1.2`、`v1.2.3`、`1.2.3.4`、`1.2.3-`。期望值是 2026-09-15 以本機 Python 3 載入 oracle `semver.py` 執行 `parse_semver` 的實際結果，記入測試註解。
- SC-F12 `CheckPackages_TagInference_RejectsShortVersions`：RG tags `1`、`tool_v1.2.0`（依此順序列出）；`version: ^1.1.0`、未設 tag_pattern。expect 推斷為 `{name}_v{version}`，check OK。oracle 實跑：`infer_tag_pattern("1","tool")` 為 None、`infer_tag_pattern("tool_v1.2.0","tool")` 為 `{name}_v{version}`。
- SC-F13 `VersionTagCandidates_VPrefixedCaptureRejected`：pattern `{version}`、tag `v1.2.0`。expect 不成為候選（oracle 擷取不含 `v`）；pattern `v{version}` 下同一 tag 是候選。

### F. gate

- SC-F14 `tools/gate/mutants.txt` 新增 mutant：`sha-name-match-restored`（SC-F1 殺）、`manifest-read-uncapped`（SC-F3 殺）、`current-map-kept-on-no-tags`（SC-F6 殺）、`leading-v-not-stripped`（SC-F8 殺）、`name-type-unchecked`（SC-F9 殺）、`semver-npm-grammar`（SC-F11 或 SC-F12 殺）。確切 `old`/`new` 在對應 GREEN commit 寫入。
- SC-F15 `tools/gate/realexec.sh` 新增離線步驟：`name: 123` 的 `check` exit 2 並印 `'packages[0].name' must be a non-empty string`。

## Must NOT

- Must NOT：修改 `specs/archive/marketplace-check-outdated/` 下任何檔案。
- Must NOT：改變 `tagpattern.Compile`、`ExtractVersion`、`RenderTag` 的行為（`pack` 的 build 與 version-check 路徑共用）。
- Must NOT：原 SPEC v4 的既有情境測試被刪除或斷言放寬。本 SPEC 明列要改的斷言：無。若 RED 階段發現既有 fixture 使用 `1.2` 類 tag 或非字串 name，補值並記入 Revisions，不視為 SPEC 變更。
- Must NOT：`version`、`ref` 的數字值被拒絕（oracle 接受）。
- Must NOT：具名 ref（非小寫 40-hex）的比對行為改變。
- Must NOT：新增 git 子程序；既有子程序失去 `ApplyCloneEnv` / `ApplySecureGitEnv`、`WaitDelay`、`pinGitLocale`；錯誤文字未經 `SanitizeGitOutput`。
- Must NOT：測試或 gate 連網。
- Must NOT：每個遠端套件超過一次 `git ls-remote`；fetch 探測只在「小寫 40-hex 且 ls-remote 無 commit 命中」時發生（SC-F1 後，名稱命中不再豁免探測）。

## Failure model (Tier 3)

| Failure mode | Check that catches it |
|---|---|
| SHA 驗證被同名 ref 繞過，marketplace.json 寫入不存在的 commit | SC-F1 + mutant `sha-name-match-restored` |
| 修正 SHA 比對時誤改具名 ref 比對 | SC-F2 + 原 SPEC SC-B7 系列測試 |
| 遠端 manifest 過大造成記憶體耗盡 | SC-F3（無限 reader 必須在期限內返回）+ mutant `manifest-read-uncapped` |
| 上限截斷後 git 子程序未結束或暫存目錄殘留 | SC-F4 斷言程序結束與目錄移除 |
| 上限邊界差一位元組 | SC-F4、SC-F5 |
| Current 顯示 SHA，與 Note 矛盾 | SC-F6、SC-F7 + mutant `current-map-kept-on-no-tags` |
| `v` 前綴造成 outdated 誤報或漏報 | SC-F8 + mutant `leading-v-not-stripped` |
| 非字串 name 流入 marketplace.json | SC-F9 + mutant `name-type-unchecked` |
| 型別檢查誤拒數字 version/ref | SC-F10 |
| 版本文法收緊後誤拒合法 tag | SC-F11 表格（期望值來自 oracle regex 實跑）+ 原 SPEC SC-B9、SC-B12、SC-C1..C3 回歸 |
| 版本文法未收緊，推斷選錯 layout | SC-F12 + mutant `semver-npm-grammar` |

## Setup plan

- Tools to install: none。
- Git isolation: 現有 worktree `.claude/worktree/fix-marketplace-outdated`，分支 `fix/markteplace-outdated`（未 push、無 PR），base 為 3d59291。Cadence：SPEC 核准 commit → 每個行為一個 RED commit（僅測試）+ 一個 GREEN commit（僅實作）→ gate/evidence commit。
- Files the change will modify, by path：`internal/marketplace/authoring/refcheck.go`、`refcheck_sha.go`、`schema.go`、`internal/semver/semver.go`、`internal/marketplace/tagpattern/tagpattern.go`（僅 `Infer` 的驗證呼叫，若需要）、對應 `_test.go`、`cmd/apm-go/` 的 outdated 與 check 測試、`tools/gate/mutants.txt`、`tools/gate/realexec.sh`、`ARCHITECTURE.md`（行號錨點變動時）。
- Evidence：`.scratch/marketplace-check-outdated-fixes/evidence.md`；squad 紀錄 `.scratch/marketplace-check-outdated-fixes/squad/`（`git add -f`）。
- Gate：`sh tools/gate.sh -base 3d59291 -scope marketplace-check-outdated-fixes`。`internal/semver` 目前不在 `GATE_SCOPE_PKGS`（原 evidence 已揭露）；本 SPEC 修改它，是否把它加入 scope 列為待決定（D-1）。
- New dependencies: none。
- 明確排除：oracle 在未設 package name 時以 `[^/]+` 取代 `{name}` 的推斷差異（apm-go 跳過含 `{name}` 的 layout）；`RefLister.ListRefs` 無 `WaitDelay`；既有 flaky `TestFetchGit_*`；摘要檔的文字錯誤（不在 repo）。

## 待決定

- D-1：`tools/gate.sh` 的 `GATE_SCOPE_PKGS` 是否加入 `internal/semver`，讓 SC-F11 的變更進入 changed-line coverage 與 mutation。建議：加入（本 SPEC 修改其行為；不加入則 coverage 層對該檔的宣稱為 unverified）。

## Approval

- （待核准）

## Revisions

- 2026-09-15 — v0.1 草稿。
