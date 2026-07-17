## CRITICAL

### C1. 污染收斂方案不符合 B2-3，修完後舊污染檔案可能永久殘留

- 文件位置：PRD B2-3、AC-B2-1/3/5；Design §1.2d；Implement Phase 1 step 5
- 理由：Design 明確允許「若既有機制不清 stale 檔，就只提示 uninstall + 重裝」，但 PRD 要求再次 bare install 後「收斂佈署」。若 deploy 只是停止產生舊 skill，新 lockfile 又只記新子集，舊的 21 個檔案仍留在 target，甚至從新帳本消失，之後更難安全移除。這不是提示文字能補救的 presentation 問題。
- 具體修正建議：在設計階段先決定並實作 ownership-aware reconciliation：以 existing lockfile 的舊 `deployed_files` 減去本次新集合，只刪除仍符合舊記錄 hash／確認未被其他 dep 接管的檔案；使用者修改、共享或無法證明所有權者保留並警告。AC 必須檢查實際 target filesystem，而非只檢查新 lockfile/DeployResult。若本任務不做清理，應刪除 B2-3「自動收斂」承諾並明確降級為 migration 指引，不能兩者並存。

### C2. 「有效子集」計算時機與現有控制流程不相容，update 的 lockfile 仍可能帳實不符

- 文件位置：Design §1.2c、§1.3；Implement step 4c/e；程式碼 `buildLockfile`、`deployAndFinalize`、`update.go:228`
- 理由：Design 把 per-dep effective subset 放在 `deployAndFinalize` 建構，但 `newLock` 在進入該函式前已由 `buildLockfile` 建好。update 更以 `skillSubset=nil, requestedKeys=nil` 呼叫 `deployAndFinalize`。因此 deploy 可以從 manifest 套 persisted filter，但先前產生的 update lockfile 未必寫入同一 subset。文件宣稱「buildLockfile 記 filter map 中的值」，現有資料流卻沒有把該 map 傳給它。
- 具體修正建議：抽成唯一的 `effectiveSkillSubsets(manifest, requestedKeys, cliSubset)` 計算點，在 buildLockfile 與 deploy 之前執行，結果同時傳給兩者；update 也必須從 manifest 產生同一份 map。`'*'` reset 要先更新記憶體中的有效狀態，再供 manifest、lockfile、deploy 共用，不能由三段程式各自推導。

### C3. BUG-2 的「第三個斷點」已存在，但根因敘述錯寫成覆寫，會讓修正落錯位置

- 文件位置：PRD BUG-2 根因與 B2-2；Design §1.2c；現況 `persistPackagesToManifest`
- 理由：對已存在的 package，函式在 `if existingPkgs[pkg]` 分支直接 `continue`；非 wildcard 的第二次 `install same-repo --skill y` 根本不更新 apm.yml，不是文件所稱的「直接覆寫」。同一次操作可能形成三種狀態：deploy 使用 CLI 的 `y`、lockfile 記 `y`、manifest 仍記舊的 `x`。下一次 bare install 又回到 `x`。這是獨立於 ParseDepDict 與 global SkillFilter 的第三個斷點。
- 具體修正建議：把根因補成「既有 manifest entry 的 subset 不會被更新」。實作需定位既有 entry，將其原地改為排序去重後的 union；不得只修改新增 entry 的序列化分支。新增三步測試：`x → 同 repo y → bare install`，三個階段都斷言 manifest、lockfile、實際部署一致。

## HIGH

### H1. ParseDepDict 規格未完整對齊所附 Python 實作，缺少安全與正規化要求

- 文件位置：PRD B2-1；Design §1.2a；Implement step 2
- 理由：Python 明確還要求：空 list 報錯、字串 trim、去重、排序、`validate_path_segments` 防 traversal。Design 只寫「list、每項非空字串」。此外 YAML 的數字也可能是 ScalarNode，不能只用 Kind 判定字串，應確認字串 tag。缺少 path safety 屬 trust-boundary 缺口。
- 具體修正建議：完整列入契約與 RED 測試：空 sequence、非 sequence、非字串 scalar、空白字串、前後空白、重複值、`.`/`..`/路徑分隔符等非法名稱；合法結果必須 trim、dedupe、sort。明確定義 `skills: null` 是無子集還是錯誤。

### H2. BUG-1 的 case-fold 未涵蓋 manifest 寫入與 wildcard reset，BUG-1×BUG-2 仍可重現

- 文件位置：Design §2；Implement step 8；`persistPackagesToManifest`、`clearPersistedSkillSubset`
- 理由：現況 `existingPkgs[pkg]`、`gitVal == pkg` 都是原始字串精確比對。即使 resolver 去重，`Owner/Repo` 與 `owner/repo` 仍可能在 apm.yml 形成兩筆；`--skill '*'` 也可能無法清掉不同大小寫 entry。Design 僅寫「盤查」而未指定共同 identity 函式與所有必改呼叫點。
- 具體修正建議：建立單一、host-aware canonical identity，供 resolver、`DepRefKey`、requested/existing map、manifest 查找/更新/reset、lockfile lookup 共用。只 case-fold GitHub host 與 owner/repo；不要 lower-case git ref、virtual path或可能大小寫敏感的自架 host path。增加 `RepoA --skill x → repoa --skill y → REPOA --skill '*'` 整合測試，最終只能有一個 manifest/lock dep。

### H3. subset 中不存在的 skill 沒有規格，會產生「lockfile 有 subset、實際零部署」

- 文件位置：PRD B2-4；Design §1.2b/c；測試策略 §4
- 理由：per-dep filter 只做白名單比對。若 `--skill typo` 不存在，可能成功、寫入 manifest/lockfile，卻部署 0 files。update 後 repo 移除某個 persisted skill 也有同樣問題。這直接違反帳實一致要求，也可能被 `deployed 0 files` 噪音掩蓋。
- 具體修正建議：定義兩種政策：新 CLI 名稱不存在應失敗且不寫 manifest/lockfile；已持久化名稱因 update 消失時，至少警告並明確決定保留、移除或失敗。測試單 skill typo、部分存在部分不存在、update 後消失，以及操作失敗時檔案與兩份 metadata 不變。

### H4. AC-B2-1/4 使用「subset 大小 == deployed 檔案數」作判定，模型本身錯誤

- 文件位置：PRD AC-B2-1、AC-B2-4；Implement step 3
- 理由：一個 skill 可以包含多個檔案，也可能部署到多個 target；範例中的「1 skill」不等於 filesystem 只有一個檔案。lockfile `skill_subset` 是 skill 名稱，`deployed_files` 是輸出路徑，不能直接做數量相等。
- 具體修正建議：fixture 至少讓每個 skill 含多檔並部署到兩個 target。驗收改為：每個 target 只存在所選 skill 的預期相對路徑；任何未選 skill 的路徑都不存在；lockfile subset 等於有效 skill 名集合；`deployed_files` 等於實際由本次部署管理的路徑集合。

### H5. update、local-bundle、frozen、registry/marketplace 的行為矩陣沒有定案

- 文件位置：Design §1.2a、§1.3；Implement Phase 1；`runInstall` early exit；`update.go:228`
- 理由：
  - update 明確共用 `deployAndFinalize`，卻沒有獨立驗收。
  - local-bundle 在 manifest 讀取與一般 `--skill` guard 前 early-exit，完全繞過新鏈路。
  - frozen 不 deploy，但舊污染 lockfile/額外檔案如何處理未定義。
  - Design 宣稱「Python 亦僅 git 支援 skills」，但提供節錄不足以證明 registry/marketplace/local 的拒絕或忽略語意；ParseDepDict 若在不支援分支靜默忽略 `skills`，會製造假成功。
- 具體修正建議：在 design 加命令×來源矩陣：install、bare install、update、frozen、uninstall × git、registry、marketplace、local、local-bundle。每格明定支援、拒絕或不適用；不支援的 dict 組合應報 unknown/invalid key，不得靜默丟棄。

### H6. per-dep map 的 key 與「存在但空集合」語意未定義

- 文件位置：Design §1.2b
- 理由：`map[string][]string` 需要區分「key 不存在＝全量」與「key 存在但空 slice＝部署零個」。Parser 雖應拒絕空 list，但 union/normalization、失效 skill 或錯誤 plumbing 仍可能產生空 slice。另 manifest 的 `DepRefKey`、resolver `dep.Key`、lockfile `UniqueKey` 未證明完全相同，尤其 alias、virtual dependency、大小寫正規化後可能 miss filter 而退回全量。
- 具體修正建議：明定 invariant：map 中不得存在空集合；建構時遇到空集合即錯誤。以同一 canonical deploy key 產生 map 與 primitives。加入 alias、virtual path、devDependency、混合大小寫及 direct/transitive key 測試。

### H7. BUG-1 的「相同 dep」定義不足，可能誤合併不同 ref 或漏合併等價 URL

- 文件位置：PRD BUG-1；Design §2
- 理由：只寫 `strings.ToLower(owner/repo)`，未說明 `github.com/X/Y`、HTTPS、SSH、SCP、預設 host 是否等價；也未定義同 repo 但不同 ref/version/virtual path 的衝突語意。Git ref 可大小寫敏感，不能整個 key lower-case。
- 具體修正建議：拆開 repository identity 與 resolution selector。只正規化 host及 GitHub owner/repo；ref、版本 constraint、virtual path 保持原值。相同 repository identity 但 selector 不同時要明確報衝突或依既有 first-declared 規則並警告，不能靜默合併。

### H8. R17 的 `SilenceUsage: true` 範圍過大

- 文件位置：Design §3 R17；Implement step 11
- 理由：若設在共用 Cobra command 上，可能抑制所有 install 錯誤的 usage，而需求只要求 no-target 路徑不印 dump；也可能影響同一 command object 的後續測試。
- 具體修正建議：用可辨識的 no-target error type，在 Execute/error mapping 層僅針對該錯誤 suppress usage；或保證每次 command 新建並只在該次錯誤設定。測試 no-target 無 usage、一般 flag/argument 錯誤仍維持既有 usage 與 exit code。

## MEDIUM

### M1. R7 用 lockfile `DeployedFiles` 計數，不等於實際清理檔案數

- 文件位置：Design §3 R7；Implement step 12
- 理由：lockfile 只能表示預期管理路徑；檔案可能已不存在、被使用者修改而跳過、跨 target 重複，或目錄清理失敗。直接輸出成「清理 N 個」會誤報成功。
- 具體修正建議：從實際 uninstall 結果累計 removed/skipped/failed；若只能取得 lockfile，措辭改成「處理 N 個記錄」，不能稱為實際清理數。測試 missing、modified、共享/衝突檔案。

### M2. R16、R18 並非純 presentation-only，scope 分類需要修正

- 文件位置：PRD 乙類、R16、R18；Design §3
- 理由：R16 必須判定是否存在可部署 local primitive，若為了顯示而提前 collect，可能改變掃描時機與 I/O；把 local 計為一個 installed unit 也改變 summary 的業務計量定義。R18 若建立或修改 `.gitignore` 是明確 filesystem 行為。
- 具體修正建議：將 R16 標為「presentation + 使用既有部署結果」，summary 在 deploy 後依 `DeployResult.PerDep[""]` 決定，不要另做一次掃描。R18 維持不補，或拆成獨立行為需求並加入 idempotency、既有內容保留、換行格式測試。

### M3. R13/R15 的資料來源可能錯計總數或重複項

- 文件位置：PRD R13/R15；Design §3；Implement step 15
- 理由：`MCPProvenance` 是 slice，未保證 server→targets 已聚合去重；`len(newLock.MCPServers)` 可能是整體 desired-state 數，不一定是本次實際成功配置數，也可能包含 local/dependency來源。輸出「Installed M」可能誤導。
- 具體修正建議：先定義 summary 是「目前鎖定總數」或「本次成功部署數」。若是本次部署，從 DeployResult 的成功結果聚合 server identity 與 target set；測試同 server 多來源、同 target 重複、零 target、部署部分失敗。

### M4. wildcard 與 additive union 的混合輸入語意只藏在程式註解

- 文件位置：PRD B2-2；Design §1.2c；現況 `hasSkillWildcard`
- 理由：目前行為是只要出現 `'*'`，即使同時有 `x` 也整體 RESET。這與 union 不矛盾，但規劃文件沒有把混合輸入列為正式契約，容易在 manifest、lockfile、deploy 三處實作不一致。
- 具體修正建議：明訂 `--skill x --skill '*'` 等同全量 reset；測試參數順序互換、重複 wildcard、reset 後 bare install，以及只清 requested dep、不清其他 dep。

### M5. 測試缺少 metadata 與 filesystem 的失敗原子性

- 文件位置：Design §4；Implement Phase 1
- 理由：目前主要驗證成功結果，未涵蓋 deploy 成功後 manifest/lockfile 寫入失敗、manifest 成功而 lockfile 失敗、非法 subset、requested key 未匹配等情況。這些路徑可能留下新一輪帳實不符。
- 具體修正建議：至少加入一個可注入寫入失敗的整合測試，確認錯誤時舊 manifest/lockfile 不被部分覆寫；若現有架構無交易能力，文件需明列限制與恢復策略。

### M6. 測試沒有覆蓋 devDependencies 與多個 requested package

- 文件位置：Design §4；Implement steps 2–5
- 理由：deploy 明確合併 `ParsedDeps` 與 `ParsedDevDeps`；per-dep filter 若只走 production deps，dev subset 會退回全量。一次多個 positional package 共用 `--skill` 時，也需確認每個 key 都獨立 union、驗證不存在名稱。
- 具體修正建議：加入 dev dependency persisted subset、prod/dev 同 repo、兩個 requested packages、其中一個未 resolve／缺 skill 的測試。

### M7. BUG-1 測試只看 `Resolved 1`，不足以證明沒有資料遺留

- 文件位置：PRD AC-B1-1；Design §4；Implement steps 7–9
- 理由：stdout 顯示 1 不代表 manifest、module directory、lockfile、summary 與部署結果都只有一份；UX 遮蔽也能讓此測試誤綠。
- 具體修正建議：同時斷言 apm.yml 單 entry、lockfile 單 dep、單一 module identity、沒有第二份 PerDep 結果、無 shadow/0-files，並在下一次 bare install 與 update 後仍保持單一。

## LOW

### L1. Implement 的 verify 多處不是可執行的二元判定

- 文件位置：steps 4、11–16、18
- 理由：`install_test.go:1169-1263` 不能作為 `go test` selector；`P4-xx 判定可沿用`、`evals 實跑無矛盾` 沒有具體命令、預期 exit code或斷言。Step 6 的 `go build ./... && ...` 也依賴 PowerShell 版本。
- 具體修正建議：為每項列出實際 test name 與獨立命令，例如 `go test ./cmd/apm-go -run '^TestInstallSkillWildcardReset$' -count=1`；build、vet、test 分三條執行；eval 指定 fixture、完整參數、stdout/stderr matcher、filesystem matcher 與 exit code。

### L2. RED manifest 測試以「欄位不存在」造成 compile failure，訊號品質較差

- 文件位置：Implement step 2
- 理由：編譯失敗無法證明 parser 行為確實為紅，也可能阻斷同 package 其他測試。
- 具體修正建議：先以現有可觀察輸出或最小 struct 欄位提交建立可編譯 RED，再實作解析；或把「資料模型欄位」與「parser behavior」拆成同一 TDD commit 中可追蹤的兩個小步驟。

### L3. `>100 行` 作為 R12e 分流門檻缺乏風險意義

- 文件位置：Design §3 R12e；Implement step 19
- 理由：行數不能判定是否越界；新增一個 Result 欄位可能很小但改變 API 契約，反之測試較多也可能超過 100 行但風險低。
- 具體修正建議：改用行為準則：若只增加既有計算結果的只讀欄位且不改 compile 決策即可納入；若需改解析、選擇或寫檔流程則分流。

### L4. 文件更新被當作驗證結果，可能形成「改文件讓測試通過」

- 文件位置：Implement steps 5、19、20
- 理由：「更新 design 決策」「prd 勾選」不是產品驗證；尤其 step 5 在發現不清 stale 後只改提示/文件，會掩蓋 B2-3 未完成。
- 具體修正建議：文件更新只能是交付物，verify 必須是測試或明確人工觀察；若觀察結果不滿足 AC，Phase 1 不得進入 commit gate。
