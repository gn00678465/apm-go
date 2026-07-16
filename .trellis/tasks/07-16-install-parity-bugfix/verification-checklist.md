# install parity 與業務 bug 修復－硬性驗收 Checklist

本清單供未參與實作的 reviewer 執行最終驗收。每項只有 **PASS / FAIL**；任何指令不存在、測試未被收錄、結果不穩定、僅以文件或人工觀感宣稱完成，均判定 **FAIL**。

> **定位原則：判定以實作當下程式碼為準，行號漂移時，以同一函式、資料流或語意重新定位。**
>
> 所列測試名稱若實作時調整，必須在 `implement.md` 回填可直接執行的實際名稱；不得以名稱漂移為由略過驗收。

## Phase 0 — Canonical identity

- [ ] **V0-1｜GitHub repository identity 等價歸一**（design.md §0；AC-B1-1）
  - **判定：**執行 `go test ./internal/manifest/... -run TestCanonicalRepoIdentity -count=1`，測試必須涵蓋短格式、`github.com`、HTTPS、SSH URL、SCP syntax，以及 owner/repo 大小寫差異，且全部得到同一 identity。
  - **FAIL：**任一等價形式產生不同 identity；測試只覆蓋大小寫而未覆蓋 URL 形式；測試不存在或零案例執行。

- [ ] **V0-2｜自架 Git host 不做 GitHub 式 case-fold**（design.md §0；AC-B1-2）
  - **判定：**同一測試群明確斷言自架 host 上 `Owner/Repo` 與 `owner/repo` 不會因通用 lower-case 被合併。
  - **FAIL：**所有 host 一律 `ToLower`；自架 host 的大小寫差異被靜默合併；沒有自架 host 反例。

- [ ] **V0-3｜Selector 保留原值且不參與 repository identity**（design.md §0）
  - **判定：**測試涵蓋大小寫敏感 ref、version constraint、virtual path、alias；序列化及 resolution 使用的 selector byte-identical 保留原值，同 identity 不同 selector 產生明確警告或衝突。
  - **FAIL：**selector 被 lower-case；不同 selector 被無警告合併；canonical identity 字串混入 selector。

- [ ] **V0-4｜Canonical identity 單一實作，無 split-brain**（design.md §0、§1.2b–e；H2/H6）
  - **判定：**執行 `rg -n "CanonicalRepoIdentity|(?i)strings\\.ToLower|(?i)strings\\.EqualFold" internal cmd`；resolver、requested/existing map、manifest 更新/reset、lockfile 比對及 deploy filter 均呼叫同一 canonical identity 函式，沒有各自重造 repo key。
  - **FAIL：**任一使用點自行拼接或 lower-case identity；存在第二套等價識別函式；manifest、lockfile、deploy 對同一 dep 算出不同 key。

- [ ] **V0-5｜Phase 0 可獨立編譯與回歸**（design.md §0）
  - **判定：**`go test ./internal/manifest/... -count=1` 與 `go build ./...` 均 exit 0。
  - **FAIL：**任一非零退出；identity 測試必須依賴網路或使用者環境才會通過。

## Phase 1 — BUG-2：`--skill` 子集失憶

- [ ] **V2-1｜`skills:` 完整解析與正規化**（AC-B2-1；design.md §1.2a）
  - **判定：**`go test ./internal/manifest/... -run TestParseDepDict_Skills -count=1` exit 0；涵蓋 trim、去重、排序、`skills: null`、scalar dep 無子集。
  - **FAIL：**合法 sequence 未讀入 `SkillSubset`；nil 與空集合語意混用；測試只驗證解析成功而不驗證實際欄位值。

- [ ] **V2-2｜非法 `skills:` 嚴格拒絕**（AC-B2-1；design.md §1.2a）
  - **判定：**同一測試群涵蓋非 sequence、空 sequence、非字串 scalar、空白、`.`、`..`、`/`、`\`，各案例均回傳明確錯誤。
  - **FAIL：**數字被 YAML 隱式轉成字串接受；path traversal 名稱被接受；非法值被靜默當成全量。

- [ ] **V2-3｜來源矩陣與 unknown key 防護**（B2-5b；design.md §1.2a、§1.3）
  - **判定：**執行對應來源矩陣測試；git 支援 `skills:`，registry、marketplace、manifest-local/path、parent 明確拒絕；local-bundle 與 frozen 行為符合 design 定義。
  - **FAIL：**任何不支援組合靜默忽略 `skills:` 後成功；矩陣有空格未定案；只在文件列矩陣但沒有可執行測試。

- [ ] **V2-4｜Per-dep filter 與空 slice 不變式**（AC-B2-1、AC-B2-5；design.md §1.2b；H6）
  - **判定：**`go test ./internal/deploy/... -run 'Test.*SkillFilter' -count=1` exit 0；測試證明 key 不存在才代表全量，map 內空 slice 會在建構時回錯或 panic，prod/dev dep 均以 canonical identity 查表。
  - **FAIL：**空 slice 被放入 filter map；空 slice 被解讀為全量或零部署；prod/dev 使用不同查找邏輯。

- [ ] **V2-5｜兩 repo 兩步驟不再污染**（AC-B2-1）
  - **判定：**`go test ./cmd/apm-go -run TestInstall_SkillSubsetPollution -count=1` exit 0；fixture 每個 skill 含多檔且部署至至少兩個 target，逐一比對所有 target 的預期相對路徑集合。
  - **FAIL：**任一未選 skill 路徑存在；只比較 skill 數量；fixture 仍是「一 skill 一檔一 target」。

- [ ] **V2-6｜H4 帳本採路徑集合，不把 skill 數當檔案數**（AC-B2-1、AC-B2-4）
  - **判定：**V2-5 fixture 明確形成 `skill 數 != deployed_files 路徑數`；斷言 `skill_subset` 等於有效名稱集合，`deployed_files` 等於實際受管理路徑集合。
  - **FAIL：**以兩者數量相等作為一致性；僅驗證非空或長度；lockfile 路徑集合與 target 實況不同。

- [ ] **V2-7｜第三個 repo 不使既有子集膨脹**（AC-B2-3）
  - **判定：**三 repo 連續安裝測試 exit 0；第三次後 repo_a、repo_b、repo_c 在每個 target 均只含各自有效子集路徑。
  - **FAIL：**只驗證最新 repo；任何較早 repo 被全量重部署；測試以跳過既有 repo 解析來通過。

- [ ] **V2-8｜同 repo additive union 與三份狀態一致**（AC-B2-4；design.md §1.2c–e）
  - **判定：**`go test ./cmd/apm-go -run TestInstall_SkillSubsetSameRepoUnion -count=1` exit 0；執行 `x → y → bare install` 後，manifest 為單一 entry 且子集 `[x,y]`，lockfile 與實際 target 路徑集合一致。
  - **FAIL：**manifest 仍為 `[x]`、新增第二筆 entry、lockfile 與 deploy 各自推導結果，或 bare install 回退至舊子集。

- [ ] **V2-9｜Wildcard 與混合輸入皆為 RESET**（AC-B2-2）
  - **判定：**`go test ./cmd/apm-go -run 'TestInstall.*[Ww]ildcard|TestInstall.*[Rr]eset' -count=1` exit 0；`--skill x --skill '*'` 與反向順序結果相同，只清 requested dep 子集。
  - **FAIL：**混合輸入形成 `[x,*]` union；參數順序改變結果；RESET 誤清其他 dep 子集。

- [ ] **V2-10｜Bare install 與 update 共用同一有效子集**（AC-B2-5；design.md §1.2c）
  - **判定：**`go test ./cmd/apm-go -run 'TestInstall_Bare.*SkillSubset|TestUpdate_RespectsSkillSubset' -count=1` exit 0；兩路徑均同時核對 manifest、lockfile 與 target 路徑。
  - **FAIL：**update 在建立 lockfile 後才算子集；bare install 正確但 update 全量部署；只檢查 stdout。

- [ ] **V2-11｜新 unknown skill 原子失敗**（AC-B2-6；design.md §1.2f）
  - **判定：**`go test ./cmd/apm-go -run TestInstall_UnknownSkill -count=1` exit 0；單一不存在、部分存在、多 positional 未匹配案例均非零退出，且 apm.yml、lockfile、target 檔案以 byte/hash 快照證明零變更。
  - **FAIL：**任何部分寫入、部分部署或成功 exit；僅檢查錯誤文字；錯誤後靠清理恢復而非避免提交。

- [ ] **V2-12｜Persisted skill 上游消失：警告且保留**（B2-6；design.md §1.2f）
  - **判定：**對應測試 exit 0；update 後輸出警告，manifest/lockfile 仍保留名稱，既有使用者檔案不被自動 prune。
  - **FAIL：**靜默忽略、靜默移除、整個命令無警告成功，或當成新輸入直接破壞既有狀態。

- [ ] **V2-13｜污染收斂檢查真實檔案系統**（AC-B2-7；design.md §1.2g）
  - **判定：**`go test ./cmd/apm-go -run TestInstall_StaleSkillReconciliation -count=1` exit 0；未修改且 hash 相符、未被接管的 stale 路徑被刪除；修改過、共享、無法證明所有權者保留並警告。
  - **FAIL：**只檢查新 lockfile/`DeployResult`；修改檔被刪；共享檔被刪；不確定所有權時靜默刪除；stale 實體檔仍留存卻宣稱已收斂。

- [ ] **V2-14｜Python A/B 基準可重現**（AC-B2-5）
  - **判定：**`research/bug2-python-baseline.md` 記錄 Python 版本、repo SHA、完整指令，以及兩步驟、同 repo 三階段、unknown skill 的 manifest/lockfile/檔案系統結果；依記錄重跑能得到同一語意。
  - **FAIL：**只有結論或截圖；缺版本/SHA/指令；把文件存在本身當成 BUG-2 實作驗證。

## Phase 2 — BUG-1：大小寫重複依賴

- [ ] **V1-1｜同 GitHub repo 大小寫來源只解析一次**（AC-B1-1）
  - **判定：**`go test ./cmd/apm-go -run TestInstall_CaseFoldDedup -count=1` exit 0；同時斷言 `Resolved 1`、apm.yml 一筆、lockfile 一 dep、`apm_modules` 一目錄，且無該重複造成的 shadowed、0-files、幽靈 summary。
  - **FAIL：**只檢查 stdout（M7）；任何持久狀態仍重複；在 UX 層隱藏噪音但 resolver 仍解析兩次。

- [ ] **V1-2｜Bare install 與 update 後仍保持單一**（AC-B1-1）
  - **判定：**V1-1 情境後再執行 bare install 與 update，兩階段均重新檢查 manifest、lockfile、modules 及 target 實體路徑。
  - **FAIL：**首次 install 正確但後續重新分裂；只斷言命令 exit 0。

- [ ] **V1-3｜不同 repo 與 selector 衝突不被誤合併**（AC-B1-2；design.md §2）
  - **判定：**守衛測試證明不同 repo 維持兩筆；同 identity 不同 selector 依既有規則警告或報衝突。
  - **FAIL：**過度 case-fold 導致不同 repo 合併；不同 ref 被靜默吃掉。

- [ ] **V1-4｜BUG-1 × BUG-2 RESET 交互**（AC-B1-3）
  - **判定：**整合測試依序執行 `RepoA/x --skill a → repoa/x --skill b → REPOA/x --skill '*'`；最終 manifest/lockfile 各一筆、子集已清除、實際部署為全量且無第二個 module 目錄。
  - **FAIL：**RESET 找不到不同大小寫 entry；union 或 reset 產生第二筆；三份狀態任一不一致。

- [ ] **V1-5｜舊 lockfile 混合大小寫升級相容**（AC-B1-4）
  - **判定：**載入含混合大小寫 key 的舊 lockfile 後執行 install/update；測試斷言不重複下載、解析、部署或新增 module 目錄，並保留 first-declared 顯示寫法。
  - **FAIL：**升級後多出 dep、目錄或部署紀錄；為通過測試而直接丟棄舊 lockfile。

## Phase 3/4 — 乙類 parity 與守衛

- [ ] **VB-1｜R17 no-target 結構化診斷**（R17；design.md §3）
  - **判定：**`go test ./cmd/apm-go -run TestInstall_NoTargetDiagnostic -count=1` exit 0；stderr 含實際掃描 marker 清單、至少三個可操作修法及 apm.yml 範例，命令 exit code 為 2。
  - **FAIL：**仍只有單行泛化錯誤；marker 清單與實際 detector 不一致；exit code 改變。

- [ ] **VB-2｜R17 只抑制 no-target usage**（R17；design.md §3；H8）
  - **判定：**`go test ./cmd/apm-go -run 'TestInstall_NoTargetDiagnostic|TestInstall_UsageStillShownOnFlagError' -count=1` exit 0；no-target 無 `Usage:`/`Flags:`，一般 flag/參數錯誤仍有 usage。
  - **FAIL：**command/root command 設全域 `SilenceUsage: true`；任何非 no-target 錯誤也失去 usage；僅靠字串比對錯誤訊息 suppress。

- [ ] **VB-3｜R7 uninstall 關鍵資訊與誠實計數**（R7）
  - **判定：**`go test ./cmd/apm-go -run TestUninstall_Summary -count=1` exit 0；摘要含套件名、已更新 apm.yml 路徑，以及實際 removed/skipped/failed 計數；若只持有帳本資料，措辭必須是「處理 N 筆記錄」。
  - **FAIL：**把 lockfile `DeployedFiles` 數量稱為實際刪除檔案數；missing/modified 檔仍算成功刪除。

- [ ] **VB-4｜R11 MCP install 目標與絕對路徑**（R11）
  - **判定：**`go test ./cmd/apm-go -run TestRunMCPInstall -count=1` exit 0；輸出包含實際 `deployed` target 清單，apm.yml 路徑經 `filepath.Abs` 且為絕對路徑。
  - **FAIL：**仍印寫死的 `apm.yml: apm.yml`；列的是請求 target 而非成功部署 target。

- [ ] **VB-5｜R12a pack 正式執行檔案清單**（R12a）
  - **判定：**`go test ./cmd/apm-go -run 'TestPack_.*Files|TestPack_.*DryRun' -count=1` exit 0；正式執行與 dry-run 對相同輸入列出相同檔案路徑集合。
  - **FAIL：**正式路徑只有計數；順序不穩定；測試只檢查某個檔名 substring。

- [ ] **VB-6｜R12b local-bundle 聚合樹**（R12b）
  - **判定：**`go test ./cmd/apm-go -run TestInstall_LocalBundle -count=1` exit 0；以 `result.Files` 產生聚合樹，路徑集合完整且無重複。
  - **FAIL：**為輸出另掃檔案系統；漏檔、重複；聚合時吞掉衝突警告。

- [ ] **VB-7｜R12c audit verbose 對稱**（R12c）
  - **判定：**audit 成功測試證明預設只保留數字摘要，`--verbose` 才顯示明細；預設輸出與既定 baseline byte-identical。
  - **FAIL：**預設輸出被洗版；verbose 沒有新增明細；只用文件敘述驗證。

- [ ] **VB-8｜R12d frozen verbose 對稱**（R12d）
  - **判定：**frozen 成功測試證明預設包含驗證 dep 數、`--verbose` 額外列清單，且不 deploy、不改 manifest/target。
  - **FAIL：**frozen 因顯示需求發生部署或寫入；預設缺 dep 數；verbose 與預設相同。

- [ ] **VB-9｜R13 MCP server→targets 聚合去重**（R13）
  - **判定：**`go test ./cmd/apm-go -run TestInstall_MCPSummary -count=1` exit 0；同 server 多來源、同 target 重複時只列一次，targets 集合等於成功 provenance。
  - **FAIL：**直接逐筆輸出 slice；同 server 或 target 重複；包含失敗部署 target。

- [ ] **VB-10｜R15 MCP server 成功計數**（R15）
  - **判定：**同一測試群證明有成功 MCP 時 summary 為 `N dependencies and M MCP server(s)`，M 為本次成功 server identity 去重數；無 MCP 時不顯示該子句。
  - **FAIL：**使用 lockfile 總數；部分失敗仍全算；同 server 多來源重複計數。

- [ ] **VB-11｜R16 local-only 三情境無矛盾**（R16）
  - **判定：**`go test ./cmd/apm-go -run TestInstall_LocalOnlyProject -count=1` exit 0；涵蓋成功、零檔案、全衝突，僅成功情境依部署後 `PerDep[""]` 印 `Installed local project`。
  - **FAIL：**同時出現 `No dependencies to install` 與 local 部署樹；印 `Installed 0 dependencies`；為輸出提前額外掃描。

- [ ] **VB-12｜R18 決策邊界**（R18；design.md §3）
  - **判定：**若採既定「不補」，PRD 明確標示已決策且程式碼無假訊息；若改為建立 `.gitignore`，必須有 idempotency、保留既有內容與換行格式測試。
  - **FAIL：**只印「Added」但未建立；建立時覆寫既有內容；僅更新文件卻宣稱行為已驗證。

- [ ] **VB-13｜F4 衝突警告守衛**（AC-B1-2；F4）
  - **判定：**執行不同 repo 同名 skill 碰撞測試；stdout/stderr 仍包含 `shadowed` 與 `deployed 0 files`，並能對應實際衝突狀態。
  - **FAIL：**R7/R12/R13 聚合後吞掉任一警告；為修 BUG-1 在 UX 層全面過濾這些文字。

## 跨階段總驗收

- [ ] **VX-1｜Exit code 契約不變**（AC-B2-8、AC-B1-5、R17）
  - **判定：**執行 CLI exit-code 測試矩陣，至少涵蓋成功、一般參數錯誤、no-target=2、unknown skill、frozen 驗證失敗；全部符合變更前契約。
  - **FAIL：**任何路徑因 typed error、usage suppress 或新驗證改變既定 exit code。

- [ ] **VX-2｜Normalize stdout byte-identical**（流程硬性規則）
  - **判定：**執行 normalize golden/baseline 測試；相同語意輸入的 normalized stdout 逐 byte 比對完全一致。
  - **FAIL：**只比較去空白後文字、substring 或結構化語意；換行、空格、排序發生未核准漂移。

- [ ] **VX-3｜非 TTY/CI/重導無 ANSI 且不阻塞**（流程硬性規則）
  - **判定：**執行非 TTY 輸出測試，stdout/stderr 不匹配 ANSI escape regex `\x1b\[[0-9;?]*[ -/]*[@-~]`，命令在既定 timeout 內結束。
  - **FAIL：**任一 ANSI 控制碼、spinner 殘留、等待互動輸入或重導後卡住。

- [ ] **VX-4｜業務層不得 import `internal/ux`**（流程硬性規則；design.md §3）
  - **判定：**執行 `rg -n '"[^"]*internal/ux"' internal -g '*.go'`；除既定 presentation 邊界外結果必須為空，manifest/resolver/deploy/compile 等業務 package 不得 import UX。
  - **FAIL：**為警告或摘要讓業務層直接依賴 UX；以 import alias 或 wrapper 規避邊界。

- [ ] **VX-5｜測試未被刪除、跳過或弱化規避**（design.md §4、§5）
  - **判定：**`git diff --stat main...HEAD -- '*_test.go'`、`git diff main...HEAD -- '*_test.go'` 與 `go test ./... -list .` 交叉檢查；既有 SkillFilter、wildcard、F4、exit code、normalize 測試仍存在，新增測試包含實際狀態與負向斷言。
  - **FAIL：**刪測試、加 `t.Skip`、縮小 fixture、移除負向斷言、把精確集合改成非空/substring、或改 golden 來掩蓋回歸。

- [ ] **VX-6｜文件不能代替執行驗證**（implement.md 全階段；L4）
  - **判定：**每個完成項均能對應至少一個 exit 0 的測試/建置指令或可重現狀態檢查；research、PRD 勾選、design 更新只能作佐證。
  - **FAIL：**任何功能僅以「文件已更新」「決策已記錄」「人工看起來正確」判定通過。

- [ ] **VX-7｜全專案品質閘門**（AC-B2-8、AC-B1-5）
  - **判定：**依序執行 `go build ./...`、`go vet ./...`、`go test ./... -count=1`，三者均 exit 0。
  - **FAIL：**任一非零；只跑受影響 package；依賴 test cache；以已知失敗名單豁免本次造成的失敗。

- [ ] **VX-8｜Codex 最終對抗閘門**（design.md §5；implement.md Phase 4）
  - **判定：**執行 `git diff main...HEAD | codex exec - -c model_reasoning_effort="high"`；輸入為完整 diff，報告對 exit code、normalize、非 TTY、業務層邊界、F4、reconciliation 刪檔安全逐項審查，最終無 CRITICAL/HIGH。
  - **FAIL：**只餵 staged/部分 diff；未檢查刪檔安全；仍有任何 CRITICAL/HIGH；以更新文件代替重新跑閘門。

- [ ] **VX-9｜最終 A/B 證據可重現但不取代測試**（AC-B2-5；implement.md Phase 4）
  - **判定：**`research/final-ab-verification.md` 含 BUG-2 三情境、BUG-1、主要乙類命令的版本、SHA、完整指令、輸出與三份狀態；且 VX-7 仍獨立通過。
  - **FAIL：**缺版本/SHA/狀態；只比較措辭；以 A/B 文件取代 Go 測試或實際 target 驗證。

---

**總計：44 項**（V0：5、V2：14、V1：5、VB：13、VX：9）。
