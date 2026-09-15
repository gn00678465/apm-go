# SPEC — marketplace check / outdated review 缺陷修正 (Tier 3)

- `spec_version`: v1
- `status`: approved
- `tier`: 3
- `scope`: marketplace-check-outdated-fixes
- `base_ref`: fix/markteplace-outdated (3d59291)
- `predecessor`: specs/archive/marketplace-check-outdated/SPEC.md v4（shipped，不修改；下稱「原 SPEC」）
- `oracle`: microsoft/apm pinned b75a02b1（tools/parity/oracle.pin）。本機 D:\Projects2\apm 在 v0.30.0（8c2e0d9c）；本 SPEC 引用的 `semver.py`、`tag_pattern.py`、`_shared.py`、`yml_schema.py` 對 pin 無差異。

## 背景

原 SPEC 封存後，Codex 以唯讀方式 review 本 branch（相對 main，head 3d59291），提出五項缺陷，另指出 outdated 的 Current 不保證為 `--`。orchestrator 對照程式碼、原 SPEC 與 oracle 後收錄六項；after-spec squad 在暫存副本中重現了全部六項（紀錄：`.scratch/marketplace-check-outdated-fixes/squad/after-spec.md`）。

### 編號對照與使用者裁定

使用者裁定時使用 Codex 轉交表的編號（Codex 編號）。

| 本 SPEC # | Codex 編號 | Codex 位置 | 使用者原話（2026-09-15） | 分類 |
|---|---|---|---|---|
| 1 | 1 | refcheck.go:904 | 無（orchestrator 依原 SPEC:15 分類） | class 1 |
| 2 | 2 | refcheck_sha.go:293 | 「第 2、4、5 項: fix it」 | class 2 |
| 3 | 摘要不符第 4 點 | refcheck.go:1001 | 無（orchestrator 依原 SPEC:80 分類） | class 1 |
| 4 | 4 | schema.go:712 | 「第 2、4、5 項: fix it」 | class 2 |
| 5 | 5 | refcheck.go:1063 | 「第 2、4、5 項: fix it」 | class 2 |
| 6 | 3 | tagpattern.go:158 | 「第 3 項：算是 spec 缺口」；問「第 6 項（tag 推斷接受 `1`、`1.2`，oracle 要求 x.y.z）要怎麼處理？」時選「修，對齊 oracle」 | class 2 |

## 缺陷與證據

| # | 位置 | 缺陷 | 依據 |
|---|---|---|---|
| 1 | `internal/marketplace/authoring/refcheck.go:901-909` | `refMatches` 對小寫 40-hex 也比對 ref 名稱。遠端有名稱等於 SHA S、指向另一個 commit 的 ref 時：無 version → check 通過，不探測；有 display version → manifest fetch 失敗，回 `Reachable=false` 的 `vanished` 錯誤（refcheck_sha.go:118-120）。`build/builder.go:274-276` 與 `build/mapper.go:302-306` 會把 S 寫進 marketplace.json 的 `source.ref` 與 `source.sha` | 原 SPEC:15「先比對 `git ls-remote` 的 commit 欄位，未命中再以 `git fetch --depth 1` 探測」 |
| 2 | `internal/marketplace/authoring/refcheck_sha.go:286-307` | `showAtFetchHead` 把 `git show` 的 stdout 寫入無上限的 `bytes.Buffer`，程序結束後才比對 `manifestReadMaxBytes`（:303） | 原 SPEC 未規定 |
| 3 | `refcheck.go:999-1003`，CLI `cmd/apm-go/marketplace_authoring.go:428,478-504` | CLI 由 cwd 的 `./marketplace.json` 的 `plugins[].source.ref` 建立 current map，`outdatedForPackage` 在函式開頭寫入 Current。SHA 釘選加 display version 的條目在無候選 tag（:1054-1056）、offline（:1031-1036）、ListRefs 失敗（:1039-1042）三條路徑直接 return，Current 顯示該 SHA。原測試傳入 nil current（refcheck_outdated_sha_test.go:35,93）。只在 cwd 有 `marketplace.json` 時觸發（pack 的 claude 預設輸出在 `.claude-plugin/`；oracle `commands/marketplace/__init__.py`（b75a02b1）:1139-1141 讀同一 cwd 路徑） | 原 SPEC:80 SC-C3 與 D-f「只有存在候選 tag 時才渲染 Current」 |
| 4 | `internal/marketplace/authoring/schema.go:625,629-632,706-716` | `requireNonEmptyString` 只檢查 ScalarNode 與非空，`name: 123`、`name: true` 可載入。`source: 123` 在 3bf13db 已以 exit 2 拒絕（`ValidateMarketplaceSource`），只有訊息與 oracle 不同 | oracle `yml_schema.py:830-831` 以 `_require_str` 讀 `name`、`source`；`:454` 要求 `isinstance(value, str)` |
| 5 | `refcheck.go:1063,1140` | SHA 釘選的 `version: v1.0.0` 以 `RenderTag("v{version}", …)` 渲染為 `vv1.0.0`；`LatestInRange` 以 `c.tag == row.Current` 比對，得 `--`。LatestOverall、Status、Upgradable 與 `1.0.0` 相同 | 原 SPEC:61 check 的 manifest 比對「兩邊各去除前導 `v`」 |
| 6 | `internal/semver/semver.go:118-124`、`internal/marketplace/tagpattern/tagpattern.go:158`、`refcheck.go:1192` | `semver.IsValid` 以 NPM 解析器判定，接受 `1`、`1.2`、`v1.2.3`。tags `[1, tool_v1.2.0]` 時 `Infer` 回 `{version}`，check 得 `No tag matching '^1.1.0'` | oracle `semver.py:34-38` `_SEMVER_RE`；`_shared.py:45-47` 以 `parse_semver` 過濾候選；`tag_pattern.py:148-154` `_VERSION_RX` |

## 決定

- D-a（設計，隨核准確認）：oracle 版本文法實作在 `internal/marketplace/tagpattern`（新增匯出函式 `IsOracleVersion(s string) bool`），`Infer` 與 `versionTagCandidatesWithPattern` 改呼叫它；刪除本 branch 新增、只有這兩個呼叫者的 `semver.IsValid`。理由：tagpattern 已在 gate scope（`tools/gate.sh:38-39`），不需要修改 gate；`internal/semver` 其他匯出函式不動。
- D-b（設計）：Go regex 只接受 ASCII 數字，且不接受結尾換行。oracle 的 Python `\d` 接受 Unicode 數字、`$` 可停在結尾換行前（實跑 `parse_semver("١.٢.٣")`、`parse_semver("1.2.3\n")` 為接受）。記為 apm-go 偏離：git tag 名稱不會含結尾換行，Unicode 數字 tag 不是版本。
- D-c（設計）：`readCapped(r io.Reader, max int) ([]byte, error)` 讀到 `max+1` 位元組即停止並回錯誤；`showAtFetchHead` 以 `StdoutPipe` 串流，超限時取消 context 結束 git，再 `Wait`。錯誤文字沿用現有 `"%s at the pinned ref exceeds %d bytes"`。`git show` 命令建構抽成 `newShowCmd`，比照 `newProbeFetchCmd` 可斷言。
- D-1（使用者裁定 2026-09-15，原話「依 YAML 1.2 的 !!str (Recommended)」）：`name`、`source` 的「字串」判準：以 go.yaml.in/yaml/v4 解析後的 tag 判定，`ShortTag() == "!!str"` 才算字串。與 oracle（PyYAML，YAML 1.1）的差異記為偏離：`yes`/`on`/`off` 在 apm-go 是字串（接受）、oracle 是 bool（拒絕）；`0o17`、`1e3` 在 apm-go 是數字（拒絕）、oracle 是字串（接受）。理由：editor 寫出 `name: yes` 不加引號（editor.go:285-286），改用 YAML 1.1 語意需同時改 editor 的引號策略。
- D-2（使用者裁定 2026-09-15，原話「顯示 -- (Recommended)」）：無 version 的 SHA 釘選（tip 路徑）在 offline、ListRefs 失敗、`Remote advertised no HEAD` 三條路徑的 Current 為 `--`，與 D-f 同一原則（Current 只來自與遠端的比較結果），不再顯示 current map 的 40 字元 SHA。
- D-3（使用者裁定 2026-09-15，原話「只去掉一個小寫 v (Recommended)」）：`v` 前綴規則：只去除一個小寫 `v`（`semver.StripVPrefix`），與 check 的 manifest 比對一致；`V1.0.0`、`vv1.0.0` 行為不變，列入明確排除。

## Scenarios

每個情境對應至少一個同名測試。`RG` = 本地 `t.TempDir()` git repo，never network。每個情境標註在 3bf13db 的狀態：**RED**（必須先看到失敗）或**回歸**（在 3bf13db 已通過，保護行為不變）。

### A. check 的 SHA 比對（偏離：oracle `check.py`（b75a02b1）:166-175 只以名稱比對）

- SC-F1 `CheckPackages_ShaPin_RefNamedLikeSha_DifferentCommit_Probes`
  - 主案例（RED）：RG 先取得 commit C 的 SHA，再以另一個不存在的 40-hex S 建立分支 `S` 指向 C。`ref: S`、無 version，CommitProber 為回傳 false 的計數 fake。expect 計數 1，`Ref 'S' not found`、RefOK=false。
  - 子測試 with-version（RED）：同 fixture，`version: 1.0.0`。expect `Ref 'S' not found`、Reachable=true，不出現 `vanished`。
  - 子測試 exists（RED）：RG 中 S 為真實存在、無 ref 指向的 commit，另建分支 `S` 指向 C（先取 SHA 再建分支，之後不對 S 執行 rev-parse）。以計數器包住真實 `gitCommitProber`。expect 計數 1，OK。
  - GREEN 時更正 `refMatches` 註解的 oracle 行號並標 `apm-go-only`。
- SC-F2 `CheckPackages_NonShaRef_StillMatchesByName`（回歸）：各自獨立 RG，分支名以 `git rev-parse --abbrev-ref HEAD` 取得。子測試：預設分支名、`v1.0.0`、`refs/tags/v1.0.0`、大寫 40-hex 名稱的分支、7 字元縮寫名稱的分支。expect 以名稱比對通過，CommitProber 為 panic fake。

### B. manifest 讀取上限

- SC-F3 `ReadCapped_StopsAtLimit`（RED，函式不存在，編譯失敗）：有限計數 reader 可交出 N+1000 位元組。`readCapped(r, N)` 在 goroutine 內執行，測試以 `time.After(5s)` 設期限。expect 回錯誤、reader 被讀取的位元組數 ≤ N+1。子測試：恰為 N 位元組 → 回傳完整內容、無錯誤。
- SC-F4 `ShowAtFetchHead_Oversized_StopsBeforeTimeout`（回歸；依下方註改標，見 Revisions）：`listRefsTimeout` 設為 3s；RG commit 的 `.claude-plugin/plugin.json` 大小為 `manifestReadMaxBytes` 的 8 倍。expect 錯誤含 `exceeds`、不含 `timed out`，耗時 < 3s，暫存目錄已移除。子測試 apm-yml：無 plugin.json、`apm.yml` 超限，結果相同。
  - 註：在 3bf13db，此測試的斷言可能因整檔讀完仍 < 3s 而通過。若 RED 階段觀察到通過，改以 `manifestReadMaxBytes` 的 64 倍檔案重試並記錄；仍通過則把 SC-F4 改標回歸，RED 由 SC-F3 承擔，記入 Revisions。
- SC-F5 `FetchManifestVersion_ManifestAtLimit_Reads`（回歸）：大小恰為上限的合法 JSON 可讀出 version。與既有 `TestGitManifestVersionFetcher_OversizedManifest_Errors` 合起來固定 `>` 的邊界。
- SC-F16 `NewShowCmd_ShapeAndSecureEnv`（RED，函式不存在）：斷言命令為 `git -C <dir> show FETCH_HEAD:<path>`，Env 含 `GIT_TERMINAL_PROMPT=0` 等 `ApplySecureGitEnv` 鍵與 `LC_ALL=C`，`WaitDelay == subprocessWaitDelay`。

### C. outdated 的 Current

- SC-F6 `OutdatedPackages_ShaPinWithVersion_NoCandidates_CurrentDashes`：current map 含 `tool: <SHA>`。
  - 子測試 no-tags（RED）：無候選 tag。expect Current=`--`、Note=`No matching tags found`。
  - 子測試 offline（RED）：`offline=true`。expect Current=`--`、Status=`[x]`。
  - 子測試 listrefs-error（RED）：lister 回錯誤。expect Current=`--`、Status=`[x]`。
  - 子測試 range-entry-keeps-map（回歸）：`version: ^1.0.0`、無 ref、無 tag，current map 含 `tool: v0.9.0`。expect Current=`v0.9.0`（oracle `outdated.py:92-103`）。
- SC-F17 `OutdatedPackages_ShaPinWithoutVersion_ErrorRows_CurrentDashes`（依 D-2，RED）：current map 含該 SHA。子測試 offline、listrefs-error、no-HEAD。expect Current=`--`。
- SC-F7 `MarketplaceOutdated_ShaPin_NoTags_MarketplaceJson_OmitsSha`（cmd 層，RED）：`chdirTemp` 內寫入 `marketplace.json`，`plugins[].source.ref` 為 SHA；`withFixtureRemoteLister` 提供無 tag 的 refs。expect stdout 不含該 SHA 的 40 字元與 12 字元前綴。
- SC-F8 `OutdatedPackages_ShaPinWithVersion_LeadingV_SameAsBare`：SC-C1、SC-C2 的 fixture，`version: v1.0.0`。expect Current=`v1.0.0`、LatestInRange 與 `version: 1.0.0` 相同（RED）；LatestOverall、Status、Upgradable 相同（回歸）。子測試：`tag_pattern: "{name}_v{version}"` 時 Current=`tool_v1.0.0`（RED）。

### D. schema 型別（依 D-1）

- SC-F9 `LoadAuthoringConfig_NonStringName_Rejected`（RED）：`name: 123`、`name: true`、`name: 1.0`、`name: 0o17`、`name: [a]` 各自 expect `'packages[0].name' must be a non-empty string`；`check` 與 `outdated` exit 2。
- SC-F18 `LoadAuthoringConfig_NonStringSource_OracleMessage`：`source: 123`、`source: [a/b]`、`source: {a: b}` expect `'packages[0].source' must be a non-empty string`（RED，訊息改變；exit 2 為回歸）。
- SC-F19 `LoadAuthoringConfig_StringLikeScalars_Accepted`（回歸）：`name: "123"`、`name: yes`（D-1 偏離）通過；`source: ""` 仍為 `marketplace source is empty`（原 SC-A2，`TestLoadAuthoringConfig_EmptySource_Rejected`）；`name: ""` 仍為 `must be a non-empty string`；缺 `name` 仍為 `is required`。
- SC-F10 `LoadAuthoringConfig_NumericVersionOrRef_Accepted`（回歸）：`version: 1.10` 讀入 `"1.10"`、`ref: 123` 讀入 `"123"`，不報錯（apm-go 保留原文，見明確排除）。

### E. 版本文法（依 D-a、D-b）

- SC-F11 `IsOracleVersion_Table`（RED，函式不存在）：接受 `1.2.3`、`1.2.3-rc.1`、`1.2.3+build.1`、`1.2.3-rc.1+b`、`01.2.3`、`1.2.3-01`；拒絕 `1`、`1.2`、`v1.2.3`、`1.2.3.4`、`1.2.3-`、`1.2.3+`、`1.2.3-rc..1`、`١.٢.٣`（D-b）、`"1.2.3\n"`（D-b）。
  - 期望值來源寫在測試註解：2026-09-15 以 Python 3 執行 `importlib.util.spec_from_file_location("sv", "D:/Projects2/apm/src/apm_cli/marketplace/semver.py")`，執行模組前先 `sys.modules["sv"] = module`，再呼叫 `parse_semver`；oracle 8c2e0d9c 的 `semver.py` 對 pin b75a02b1 無差異。最後兩列為 D-b 偏離。
- SC-F12 `CheckPackages_TagInference_RejectsShortVersions`（RED）：RG tags `1`、`tool_v1.2.0`，`version: ^1.1.0`、未設 tag_pattern。expect check OK，推斷 pattern 為 `{name}_v{version}`。選 `^1.1.0` 是因為 `^1.0.0` 下 `1` 會被 NPM 當成 1.0.0 而滿足範圍，無法呈現 RED。oracle 實跑：`infer_tag_pattern("1","tool")` 為 None，`infer_tag_pattern("tool_v1.2.0","tool")` 為 `{name}_v{version}`。
- SC-F13 `VersionTagCandidates_VPrefixedCapture`：
  - 子測試 capture-rejected（RED）：pattern `{version}`，tags `1.0.0`、`v1.2.0`。expect 候選只有 `1.0.0`（`{version}` 有命中，fallback 不觸發；oracle `_VERSION_RX` 不匹配 `v1.2.0`）。
  - 子測試 fallback-infers（RED；原標回歸有誤，見 Revisions）：pattern `{version}`，tags 只有 `v1.2.0`。expect 候選為 `v1.2.0`（version `1.2.0`），usedPattern 為 `v{version}`（oracle `commands/marketplace/__init__.py`（b75a02b1）:1178-1190 推斷；實跑 `infer_tag_pattern("v1.2.0")="v{version}"`）。

### F. gate

- SC-F14 `tools/gate/mutants.txt`：
  - 新增：`sha-name-match-restored`（SC-F1 殺）、`manifest-read-uncapped`（SC-F3 殺）、`current-map-kept-on-no-candidates`（SC-F6 殺）、`leading-v-not-stripped`（作用在 Current 渲染，SC-F8 殺）、`name-tag-unchecked`（SC-F9 殺）、`oracle-grammar-loosened`（SC-F11、SC-F12 殺）。錨點取含函式特有 token 的整行，以 `gatetool replace` 驗證唯一。
  - 更新：`sha-match-by-name-only` 與 `current-render-ignores-pattern` 的錨點隨實作更新；語意不變（移除 commit 比對／忽略 pattern），仍分別由 SC-B1、SC-C1 子測試殺死。
- SC-F15 `tools/gate/realexec.sh` 新增離線步驟：`name: 123` 執行 `marketplace check --offline`，expect exit 2 並印 `'packages[0].name' must be a non-empty string`。

## Must NOT

- Must NOT：原 SPEC v4 的 Must NOT（原 SPEC:98-109）全部仍有效；本 SPEC 只收緊第 104 行「fetch 探測只在『小寫 40-hex 且 ls-remote 無 commit 命中』時發生」的含義：名稱命中不再豁免探測。
- Must NOT：修改 `specs/archive/marketplace-check-outdated/` 下任何檔案。
- Must NOT：改變 `tagpattern.Compile`、`ExtractVersion`、`RenderTag`、`FilterTags`、`Validate` 的行為；改變 `authoring.IsDisplayVersion` 的行為；改變 `internal/semver` 中 `IsValid` 以外的匯出函式。
- Must NOT：既有測試被刪除或斷言放寬。本 SPEC 明列要改的斷言：無。既有 mutant 只允許更新錨點（SC-F14）。
- Must NOT：非 SHA 列（version range 條目）的 Current 來源改變（仍取自 current map）。
- Must NOT：`version`、`ref` 的數字 scalar 被拒絕。
- Must NOT：非小寫 40-hex 的 ref 比對行為改變。
- Must NOT：新增 git 子程序；`fetch`、`init`、`show` 失去 `ApplyCloneEnv`／`ApplySecureGitEnv`、`WaitDelay`、`pinGitLocale`；錯誤文字未經 `SanitizeGitOutput`。
- Must NOT：測試或 gate 連網。

## Failure model (Tier 3)

| Failure mode | Check that catches it |
|---|---|
| SHA 驗證被同名 ref 繞過，marketplace.json 寫入不存在的 commit | SC-F1 + mutant `sha-name-match-restored` |
| 修正 SHA 比對時誤改具名 ref 比對 | SC-F2 + 原 SPEC SC-B7 系列 |
| 遠端 manifest 過大造成記憶體耗盡 | SC-F3 + mutant `manifest-read-uncapped` |
| 超限後 git 未結束，拖到逾時 | SC-F4 耗時與錯誤文字斷言 |
| 串流改寫漏掉安全環境、WaitDelay、語系 | SC-F16 |
| 上限邊界差一位元組 | SC-F3 恰為 N 子測試、SC-F5 |
| Current 顯示 SHA，與 Note 或狀態矛盾 | SC-F6、SC-F17、SC-F7 + mutant `current-map-kept-on-no-candidates` |
| 修 Current 時誤清 range 條目的 Current | SC-F6 range-entry-keeps-map |
| `v` 前綴造成 Current 錯誤 | SC-F8 + mutant `leading-v-not-stripped` |
| 非字串 name 流入 marketplace.json | SC-F9 + mutant `name-tag-unchecked` |
| 型別檢查誤拒合法值或改變既有訊息 | SC-F19、SC-F10、原 SC-A2 |
| 文法收緊後誤拒合法 tag | SC-F11 表格 + 原 SC-B9、SC-B12、SC-C1..C3 + SC-F13 fallback-infers |
| 文法未收緊，推斷選錯 layout | SC-F12、SC-F13 capture-rejected + mutant `oracle-grammar-loosened` |
| 新拒絕規則使 pack／doctor／package 行為改變 | 原 SPEC:102 Must NOT；既有 pack、doctor、package 測試與 realexec 全綠 |

## Setup plan

- Tools to install: none。
- Git isolation：worktree `.claude/worktree/fix-marketplace-outdated`，分支 `fix/markteplace-outdated`（未 push、無 PR），base 3d59291。Cadence：SPEC 核准 commit → 每組行為一個 RED commit（僅測試）+ 一個 GREEN commit（僅實作）→ gate／evidence commit。
- 測試檔配置（配合 `tools/gate/red.sh:27` 只重建新增的 `_test.go`）：每組 RED 情境一個新檔，回歸情境另放新檔。
  - `internal/marketplace/authoring/fixes_sha_match_test.go`（SC-F1）、`fixes_sha_match_regression_test.go`（SC-F2）
  - `fixes_read_capped_test.go`（SC-F3、SC-F4、SC-F16）、`fixes_read_capped_regression_test.go`（SC-F5）
  - `fixes_current_test.go`（SC-F6 RED 子測試、SC-F17、SC-F8）、`fixes_current_regression_test.go`（SC-F6 range-entry-keeps-map）
  - `fixes_schema_type_test.go`（SC-F9、SC-F18）、`fixes_schema_type_regression_test.go`（SC-F19、SC-F10）
  - `fixes_tag_inference_test.go`（SC-F12、SC-F13 capture-rejected）、`fixes_tag_inference_regression_test.go`（SC-F13 fallback-infers）
  - `internal/marketplace/tagpattern/oracle_version_test.go`（SC-F11）
  - `cmd/apm-go/marketplace_outdated_current_test.go`（SC-F7）
- 修改的產品檔案：`internal/marketplace/authoring/refcheck.go`、`refcheck_sha.go`、`schema.go`、`internal/marketplace/tagpattern/tagpattern.go`、`internal/semver/semver.go`（僅刪除 `IsValid`）、`tools/gate/mutants.txt`、`tools/gate/realexec.sh`、`ARCHITECTURE.md`（§2 tagpattern 入口與行號錨點）。不修改 `tools/gate.sh`。
- RED 前基準：在 worktree 以 3d59291 跑一次 `go test ./cmd/apm-go/... ./internal/marketplace/...`，記錄既有失敗（squad 在副本觀察到 `TestDoctor_ExecGit_TimesOutDespiteOrphanedGrandchildHoldingPipesOpen` 單獨執行失敗，原因未調查）。
- Evidence：`.scratch/marketplace-check-outdated-fixes/evidence.md`；squad 紀錄在 `.scratch/marketplace-check-outdated-fixes/squad/`（`git add -f`）。diff 形狀的 Must NOT 以 `git diff --name-only 3d59291..HEAD -- specs/archive/marketplace-check-outdated/` 與 `git diff 3d59291..HEAD -- '*_test.go' | grep '^-[^-]'` 檢查，指令與輸出記入 evidence。
- Gate：`sh tools/gate.sh -base 3d59291 -scope marketplace-check-outdated-fixes`。
- New dependencies: none。
- 明確排除：
  - oracle 以 `str(value)` 正規化 `version`、`ref` 的數值（`1.10`→`1.1`、`0x1F`→`31`、`true`→`True`）與接受 list；apm-go 保留原始 scalar 文字、拒絕非 scalar，行為不變。
  - oracle 同一字串規則的其他欄位：`subdir`、`tag_pattern`（yml_schema.py:836-838、878-880）、`owner.name`（:609）、marketplace 的 `name`／`description`／`version`（:1108-1125）。
  - `V1.0.0`、`vv1.0.0`（D-3）。
  - 未設 package name 時 oracle 以 `[^/]+` 取代 `{name}` 的推斷差異。
  - SHA 釘選與 `+build` tag 並存時 `CompareVersions` 字串 tie-break 誤報。
  - fetch 的 stderr 與 ls-remote 的 stdout 無上限緩衝。
  - annotated tag 的 tag object SHA 命中 commit 欄位而不探測。
  - `RefLister.ListRefs` 無 `WaitDelay`；既有 flaky `TestFetchGit_*`；「每個遠端套件至多一次 ls-remote」無計數測試（evidence 標為未量測）。

## Approval

- 2026-09-15 — approves v1 — "核准 v1"（AskUserQuestion 結構化回覆，問題明示 commit 5760b0c 與內容來源 7abf1e5）

## Revisions

- 2026-09-15 — v0.1 草稿（3bf13db）。
- 2026-09-15 — v0.1 → v1：after-spec squad 四個 lens（scope、input space、repo reality、test mapping）的 findings 折入，紀錄 `.scratch/marketplace-check-outdated-fixes/squad/after-spec.md`。更正：SC-F13 期望值與 oracle 相反（改為兩個子測試）；使用者原話的編號（「第 3 項」而非「第 6 項」，加對照表）；SC-F3 改用有限 reader 並命名函式；SC-F4、SC-F5 重新設計或標回歸；D-f 補 offline 與 ListRefs 失敗路徑；source 型別檢查保留空字串既有訊息；沿用原 SPEC Must NOT；既有 mutant 錨點更新規則；SC-F15 加 `--offline`；測試檔配置配合 red.sh；每個情境標 RED／回歸。新增 SC-F16..F19。v0.1 的 D-1（gate scope）以 D-a 設計解消除。新增待決定 D-1（字串判準）、D-2（tip 路徑 Current）、D-3（v 前綴規則）。
- 2026-09-15 — v1 → v2：寫入使用者對 D-1、D-2、D-3 的裁定（皆為建議項，原話見各決定）。使用者對 v1 的核准回覆為「不核准，先修改」，未附修改內容；本版只記錄三項裁定，情境、Must NOT、Setup plan 未改。
- 2026-09-15 — 版本編號更正：7112da6 在 D-1..D-3 未定案時標為「v1」，7abf1e5 在未核准時升為「v2」，兩者都違反 evidence-first 規則（`~/.agents/workflows/evidence-first.md:56-63,109-110`：核准前草稿為 v0.N，v1 是待決定事項清空後第一個送核准的版本，只有推翻已核准內容才升版）。兩個 commit 皆為未核准草稿。本 commit 起的內容才是 v1；核准紀錄以 commit hash 綁定。內容與 7abf1e5 相同，只改編號。
- 2026-09-15 — 依 SC-F4 註執行（非 SPEC 變更，SPEC 已預先授權）：在修正前的程式碼（e02bf35 的暫存 worktree）執行 SC-F4，上限 8 倍（2.00s）與 64 倍（6.22s）皆 PASS，因為修正前的讀取在 3s 期限內讀完整檔後仍報 `exceeds`。SC-F4 改標回歸，移到 `fixes_read_capped_regression_test.go`；讀取上限的 RED 由 SC-F3（`readCapped` 不存在，編譯失敗）承擔。SC-F5 在同一 worktree PASS（回歸，符合預期）。
- 2026-09-15 — 情境標註更正（非行為變更）：SC-F13 fallback-infers 在修正前（889cb92 加測試）FAIL：`{version}` 的擷取 `v1.2.0` 經 NPM 判定為有效版本，fallback 不觸發，得 usedPattern `{version}`、version `v1.2.0`。原標「回歸」有誤，改標 RED，併入 `fixes_tag_inference_test.go` 的 `TestVersionTagCandidates_VPrefixedCapture/FallbackInfers`；Setup plan 列的 `fixes_tag_inference_regression_test.go` 不建立。
- 2026-09-15 — 引用更正（非行為變更）：`__init__.py:942` 與 `:979-990` 是本機 v0.30.0 的行號；pin b75a02b1 對應 `:1139-1141`（`_load_current_versions`）與 `:1178-1190`（推斷 fallback），已更正。
