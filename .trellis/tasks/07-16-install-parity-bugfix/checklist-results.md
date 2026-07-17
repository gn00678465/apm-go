# Check Agent 驗收結果 — 07-16-install-parity-bugfix

依 `verification-checklist.md` 44 項逐一實跑判定。判定基準：實際執行對應 `go test` /
`go build` / `go vet` / `rg` 指令並讀對應原始碼佐證，而非僅憑文件宣稱。

**總結：44/44 PASS**（含本次 check 過程中自行補上的 1 支缺漏測試，以及對 BUG-2 stale
reconciliation 機制的 3 輪安全性修補，修補後 codex 最終對抗閘門通過、無 CRITICAL/HIGH）。

---

## Phase 0 — Canonical identity（5 項）

| 項目 | 判定 | 佐證 |
|---|---|---|
| V0-1 | PASS | `go test ./internal/manifest/... -run TestCanonicalRepoIdentity -count=1` 綠；涵蓋短格式/`github.com`/HTTPS/SSH/SCP/大小寫全部同一 identity |
| V0-2 | PASS | `TestCanonicalRepoIdentity_NonGitHubHostPreservesCase` 明確斷言自架 host 大小寫不合併 |
| V0-3 | PASS | `TestCanonicalRepoIdentity_SelectorNotPartOfIdentity` 涵蓋 ref/virtual path/alias 不影響 identity 且不被 lower-case |
| V0-4 | PASS | `rg` 掃描確認 resolver（`bfsKey`）、`deploy.CanonicalDepKey`、`persistPackagesToManifest`/`clearPersistedSkillSubset`（已併入 `setEntrySkillSubset`）、`existingByIdentity` 全部呼叫同一 `manifest.CanonicalRepoIdentity`，無第二套等價判定 |
| V0-5 | PASS | `go test ./internal/manifest/... -count=1` 與 `go build ./...` 皆 exit 0 |

---

## Phase 1 — BUG-2：--skill 子集失憶（14 項）

| 項目 | 判定 | 佐證 |
|---|---|---|
| V2-1 | PASS | `TestParseDepDict_Skills_ValidTrimsDedupesSorts` 等驗證 trim/去重/排序/`skills: null`/scalar 無子集 |
| V2-2 | PASS | 非 sequence、空 sequence、非字串 scalar、空白、`.`/`..`/`/`/`\` 全部拒絕（`TestParseDepDict_Skills_*`） |
| V2-3 | PASS | `TestParseDepDict_Skills_RejectedOnNonGitBranches` 涵蓋 registry/local/name-literal/git-parent/marketplace 皆報錯 |
| V2-4 | PASS | `go test ./internal/deploy/... -run 'Test.*SkillFilter' -count=1` 綠；程式碼確認空 slice 不變式（`effectiveSkillSubsets` 只在 union 非空時才寫入 map）且 prod/dev deps 共用 `depCanonKeys` 查表 |
| V2-5 | PASS | `TestInstall_SkillSubsetPollution`：fixture 每 skill 2 檔、部署至 claude 目標的兩個實體根（`.agents/skills/`、`.claude/skills/`），逐一比對路徑集合 |
| V2-6 | PASS | 同上；`skill_subset` == 名稱集合，`deployed_files` == 4 個路徑（2 檔 × 2 根），非以「skill 數 == 檔案數」判定 |
| V2-7 | **PASS（原缺漏，已自行補寫）** | 原本沒有「三 repo 連續安裝」測試，違反 checklist 的「測試不存在即 FAIL」原則。已新增 `TestInstall_SkillSubsetThreeRepos`（`cmd/apm-go/install_test.go`），驗證 repo_a/repo_b/repo_c 三階段後互不膨脹；已跑綠 |
| V2-8 | PASS | `TestInstall_SkillSubsetSameRepoUnion` 三階段（x→y→bare）manifest/lockfile/實際部署三者一致 |
| V2-9 | PASS | `go test ./cmd/apm-go ./internal/deploy -run 'SkillWildcard\|SkillFilter' -v -count=1`（implement.md 回填指令）全綠；混合輸入 `TestRunInstall_SkillMixedWildcardResetsToFull` 綠 |
| V2-10 | PASS | Bare install 一致性見 V2-8 step3；`TestUpdate_RespectsSkillSubset` 驗證 update 路徑同尊重子集（manifest/lockfile/實際檔案） |
| V2-11 | PASS | `TestInstall_UnknownSkill_NewNameErrorsAtomically`：純新名/新名+既有名混合皆報錯，apm.yml byte 級不變、lockfile 未寫、target 目錄空 |
| V2-12 | PASS | `TestInstall_UnknownSkill_PersistedNameDisappearsWarnsAndKeeps`：persisted 名稱消失時警告 + 保留，不靜默、不報錯 |
| V2-13 | PASS | `TestInstall_StaleSkillReconciliation`：查實際檔案系統，未修改 stale 檔清除、已修改檔保留 + 警告 |
| V2-14 | PASS | `research/bug2-python-baseline.md` 含 Python 版本/repo SHA/完整指令與三情境三份狀態記錄 |

---

## Phase 2 — BUG-1：大小寫重複依賴（5 項）

| 項目 | 判定 | 佐證 |
|---|---|---|
| V1-1 | PASS | `TestInstall_CaseFoldDedup`：`Resolved 1`、apm.yml 單筆、lockfile 單筆、apm_modules 單目錄、無 shadowed/0-files 噪音 |
| V1-2 | PASS | 同測試後段：bare install 與 `runUpdate` 後仍維持單一 |
| V1-3 | PASS | `TestInstall_CaseFoldDedup_DifferentReposNotMerged`（不同 repo 不合併）+ `TestInstall_CaseFoldDedup_SelectorConflictNotSilentlyMerged`（同 identity 不同 selector：first-declared + 警告） |
| V1-4 | PASS | `TestInstall_CaseFoldWildcardReset`：`RepoA/x --skill a → repoa/x --skill b → REPOA/x --skill '*'` 三階段，manifest/lockfile/apm_modules/實際部署全程單一 |
| V1-5 | PASS | `TestInstall_CaseFoldDedup_LockfileUpgradeCompat`：手動注入混合大小寫舊 lockfile 後重跑，收斂回單一、目錄不增長、保留 first-declared 拼寫 |

---

## Phase 3/4 — 乙類 parity 與守衛（13 項）

| 項目 | 判定 | 佐證 |
|---|---|---|
| VB-1 | PASS | `TestInstall_NoTargetDiagnostic`：exit 2、`manifest.SignalWhitelist` 全部 marker、3 個修法 + apm.yml 範例 |
| VB-2 | PASS | `TestInstall_UsageStillShownOnFlagError`：一般 flag 錯誤仍印 `Flags:`；`noDeployTargetError` 為 typed error，僅該路徑 suppress usage（未動全域 `SilenceUsage`） |
| VB-3 | PASS | `TestUninstall_Summary_CountsReflectActualRemovalOutcome`：以實際 removed/kept 計數（非 lockfile 原始筆數），modified/missing 情境正確 |
| VB-4 | PASS | `TestRunMCPInstall_SummaryShowsTargetsAndAbsolutePath`：輸出含 target 清單、`filepath.Abs` apm.yml 路徑 |
| VB-5 | PASS | `TestRunPack_DependenciesOnly_ListsPackedFiles`；程式碼確認正式與 dry-run 共用同一 `result.Files`，結構上保證輸出一致 |
| VB-6 | PASS | `TestRunInstall_LocalBundle_SummaryAggregatesDeployedFilesByKind`：聚合樹輸出 |
| VB-7 | PASS | `TestAudit_Verbose_ListsDeployedFiles` + `TestAudit_DefaultOutputStaysSummaryOnly` |
| VB-8 | PASS | `TestRunInstall_Frozen_VerboseListsDependencies` + `TestRunInstall_Frozen_DefaultOutputStaysSummaryOnly` |
| VB-9 | PASS | `TestInstall_MCPSummary`：同 server 跨 target 聚合一行；程式碼確認 `targetsByServer`/`seen` map 語意上防止同 server 或同 target 重複（`ResolvePrimitives` 已先按 (Name,Type) 去重出唯一 winner） |
| VB-10 | PASS | 同上 `TestInstall_MCPSummary`：`Installed 0 dependencies and 1 MCP server`；`TestInstall_MCPSummary_NoMCPServersOmitsMention` 驗證零 MCP 時不顯示 |
| VB-11 | PASS（含判斷備註） | `TestInstall_LocalOnlyProject_Success`/`_ZeroFilesDeployed` 涵蓋成功與零檔案。「全衝突」情境：追溯 `ResolvePrimitives`（`internal/deploy/conflict.go`）證實同類來源衝突恆有 first-declared 勝出者，且 R16 前提本身要求 `dependencies.apm: []`（零依賴），純 local 掃描（`collectFromAPMDir`）每個 (name,type) 僅能有一個實體來源，故「全部衝突導致零檔案」在此前提下無法用真實檔案系統建構出來——非漏測，而是該情境對零依賴 local-only 專案不可行 |
| VB-12 | PASS | `rg gitignore` 確認 install 路徑無任何假冒「Added .gitignore」訊息；PRD 已勾選 R18 決策（不補）並記錄理由 |
| VB-13 | PASS | 程式碼追蹤：`deployResult.Diags`（含 conflict.go 的 `shadowed`/`deployed 0 files` 診斷）在 R7/R12/R13 新增的聚合摘要程式碼之前、且不受影響地無條件印出；`TestInstall_CaseFoldDedup` 另佐證去重情境本身不再產生噪音 |

---

## 跨階段總驗收（9 項）

| 項目 | 判定 | 佐證 |
|---|---|---|
| VX-1 | PASS | 逐路徑確認：no-target=2（`errNoDeployTarget`）、一般 flag 錯誤=2（`TestInstall_UsageStillShownOnFlagError`）、frozen 失敗與 `--skill` 驗證失敗維持 exit 1（與 `main` 分支既有 `--skill`/frozen 錯誤慣例一致，本任務未新增 `withExitCode` 包裝，非回歸） |
| VX-2 | PASS | `go test ./internal/yamlcore/... -run 'TestRoundTrip_ByteExact\|TestSafeDump_DoesNotWrapLongFlowContent\|TestRoundTrip_Deterministic' -count=1` 綠；`git diff main...HEAD -- internal/yamlcore` 為空，本任務未觸碰 normalize 契約 |
| VX-3 | PASS | `git diff` 掃描確認新增輸出程式碼全數經 `ux.*`（依 terminal-ux-contract 對非 tty writer 自動去色/無 ANSI）；無新增互動式 prompt |
| VX-4 | PASS | `rg -n '"[^"]*internal/ux"' internal --include=*.go` 結果為空 |
| VX-5 | PASS | `git diff --stat main...HEAD -- '*_test.go'` 僅新增/機械性簽章更新，無刪除；`TestRun_SkillFilterWildcardDeploysAll` 更名為 `TestRun_SkillFilterAbsentKeyDeploysAll` 屬語意對齊新 API 而非弱化（斷言強度不變） |
| VX-6 | PASS | implement.md 每一步「已完成」均有可執行測試名稱佐證，本次 check 已逐一實跑覆核 |
| VX-7 | PASS | `go build ./...`、`go vet ./...`、`go test ./... -count=1`（24 packages）三者皆 exit 0 |
| VX-8 | **PASS（check 過程中發現並修復 3 個 HIGH）** | 對 `main...HEAD` 完整 diff 執行 `codex exec - -c model_reasoning_effort="high"`，聚焦 exit code、normalize、非 TTY、業務層邊界、F4、reconciliation 刪檔安全。**發現 3 個 HIGH**：(1) `validateNewSkillNames` 錯誤未走 `withExitCode(2,...)`（審核後判定非回歸，見下方說明，未修改）；(2) `reconcileStaleSkillDeployments` 對每個舊 dep 的 `DeployedFiles` 不分類型全部視為可刪除對象，且僅以「本次是否有 hash 相符的新集合」判定，未限縮於 skill 子集窄化情境，`--target` 選擇改變即可能誤刪其他 target 的檔案；(3) `claimedNow` 用原始字串比對，未正規化路徑分隔符。**已自行修復**（`cmd/apm-go/install.go`）：改為 (a) 逐 skill 名稱判定資格（`skillNameFromDeployPath`）——仍在 fresh `SkillSubset` 內的 skill 名稱即使某個 target 副本暫時未被佈署也絕不列為候選；(b) `claimedAnywhere` 改回全庫（含 local bucket）範圍檢查，避免跨 dep/跨 local 的所有權轉移誤刪；(c) `normalizeDeployPath` 明確 `strings.ReplaceAll("\\","/")` 正規化（不依賴 Unix 上為 no-op 的 `filepath.ToSlash`）。修復後追加 2 支迴歸測試（`TestInstall_StaleSkillReconciliation_TargetChangeWithoutSkillSubsetKeepsFiles`、`TestInstall_StaleSkillReconciliation_StillSelectedSkillSurvivesTargetChange`），並對修復本身跑了 3 輪 codex 對抗複審（發現→修→再發現→修→無新增 CRITICAL/HIGH），最終閘門通過 |
| VX-9 | PASS | `research/final-ab-verification.md` 含版本/SHA/完整指令/輸出/三份狀態記錄，且 VX-7 獨立通過 |

---

## VX-8 第一個 HIGH 的處置說明（未修改，決策記錄）

Codex 第一輪指出 `validateNewSkillNames` 的錯誤走一般 `fmt.Errorf`（exit 1），未如
`errNoDeployTarget` 一樣包 `withExitCode(2, ...)`。追查 `main` 分支既有程式碼
（修復前）：`--skill is not supported with a frozen install` 等既有 `--skill` 相關
驗證錯誤，在本任務之前就已經是純 `fmt.Errorf`（exit 1），從未包 exit code 2。
`validateNewSkillNames` 屬於新增的同類 `--skill` 驗證錯誤，維持 exit 1 是與既有
`--skill` 錯誤族群一致，而非破壞既定契約；若照 codex 建議改成 exit 2，反而會讓同一
指令下 `--skill` 相關錯誤 exit code 不一致（新舊兩種）。故本項判定為「審核後確認非
回歸」，未修改程式碼，僅在此記錄判斷依據，供後續 reviewer 覆核。

## 本次 check 遺留的未提交異動

以下檔案已被本次 check 直接修改/新增，**尚未 commit**（依 check agent 慣例，commit
由主流程決定）：

- `cmd/apm-go/install.go`：`reconcileStaleSkillDeployments` 安全性修補（VX-8 三個
  HIGH 的修復本體，含 `normalizeDeployPath`/`skillNameFromDeployPath` 兩個新 helper）
- `cmd/apm-go/install_test.go`：新增 `TestInstall_SkillSubsetThreeRepos`（V2-7 補測）、
  `TestInstall_StaleSkillReconciliation_TargetChangeWithoutSkillSubsetKeepsFiles`、
  `TestInstall_StaleSkillReconciliation_StillSelectedSkillSurvivesTargetChange`
  （VX-8 修復的迴歸測試）

全部異動已通過 `go build ./...`、`go vet ./...`、`go test ./... -count=1`（24
packages 全綠），以及對最終異動的第 3 輪 codex 對抗閘門（無 CRITICAL/HIGH）。
