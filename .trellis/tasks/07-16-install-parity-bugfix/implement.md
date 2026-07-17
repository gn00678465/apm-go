# Implement — install parity 與業務 bug 修復

> 執行順序 = 優先序：共用 identity → BUG-2 → BUG-1 → 乙類高 → 乙類低。
> 每步標 `→ verify:`（可執行指令或明確觀察，文件更新不算 verify——codex L4）。
> 每個 commit 前 codex 閘門：`git diff --cached | codex exec - -c model_reasoning_effort="medium"`
> （prompt 首行加「不要執行本機指令」約束）。
> 流程：sonnet 子代理（trellis-implement）實作、主會話審核。
> 測試名為佔位者（`TestXxx`），實作時以實際名稱回填本文件的 verify 指令。

## Phase 0 — 共用 canonical identity（design §0）

1. **RED**：identity 等價測試——GitHub host 大小寫、https/ssh/scp/短格式等價；
   自架 host 保留大小寫；ref/virtual path 不參與。
   → verify: `go test ./internal/manifest/... -run TestCanonicalRepoIdentity -count=1` 失敗。
2. **GREEN**：實作 `CanonicalRepoIdentity`（單一函式，暫不接線）。
   → verify: 同上指令綠；`go build ./...` 綠。

## Phase 1 — BUG-2：--skill 子集失憶

3. **A/B 基準**：Python apm 跑兩步驟情境 + 同 repo 三階段（x→y→bare）+ unknown skill，
   記錄 apm.yml/lockfile/檔案系統三份狀態與 Python 版本、repo commit SHA（codex L3 可重現性）。
   → verify: `research/bug2-python-baseline.md` 含上述三情境的三份狀態記錄。
4. **加欄位（可編譯小步，codex L2）**：`DependencyReference.SkillSubset` 欄位 + zero-value
   行為（nil = 全量），parser 尚不填。
   → verify: `go build ./... && go test ./internal/manifest/... -count=1` 綠（行為未變）。
5. **RED（parser 行為）**：`ParseDepDict` skills: 讀回測試——合法（trim/去重/排序）、
   非 sequence、空 sequence、非字串 scalar、空白字串、`.`/`..`/含分隔符、`skills: null`、
   非 git 來源分支報 unknown key。
   → verify: `go test ./internal/manifest/... -run TestParseDepDict_Skills -count=1`
     失敗於 assertion（非 compile error）。
6. **GREEN（parser）**：依 design §1.2a 實作（含來源矩陣的 reject 分支）。
   → verify: 步驟 5 指令綠。
7. **RED（e2e 污染重現）**：fixture 兩假 repo（每 skill 多檔、兩 target），兩步驟後斷言
   （codex M5 斷言模型）：target 路徑集合、apm.yml、lockfile `skill_subset`+`deployed_files`、
   apm_modules 唯一目錄。
   → verify: `go test ./cmd/apm-go -run TestInstall_SkillSubsetPollution -count=1`
     失敗（現況全量佈署）。
8. **GREEN（鏈路）**：依 design §1.2b-e 實作——
   a. `deploy.SkillFilter` 改 `Subsets map[string][]string`（空 slice 不變式，經
      `deploy.CanonicalDepKey(ref)` = `manifest.CanonicalRepoIdentity` + virtualPath 建鍵）+
      `deploy_test.go` 兩支改形狀（`TestRun_SkillFilterScopedToDepKey` 保留舊名改新形狀；
      `TestRun_SkillFilterWildcardDeploysAll` 更名 `TestRun_SkillFilterAbsentKeyDeploysAll`，
      語意由「'*' 字面值」改為「key 不存在 = 全量」）；prod+dev deps 同走 `depCanonKeys` 查表；
   b. `cmd/apm-go/install.go` 新增 `effectiveSkillSubsets()` 唯一計算點（union 用
      `unionSortedSkills` / 混合 wildcard 整條 RESET / key 用 `deploy.CanonicalDepKey`）；
      `resolvedDepCanonicalKey()` 橋接 `resolver.ResolvedDep` 缺 Owner/Repo 的限制；
   c. 接線 `buildLockfile`（新增 `effectiveSubsets` 參數，含未點名 dep）與
      `deployAndFinalize`（新增 `effectiveSubsets` 參數，`SkillFilter{Subsets: effectiveSubsets}`）；
      `update.go` 同接（`effectiveSkillSubsets(m, nil, nil)`）；
   d. `persistPackagesToManifest`（簽章改 `effectiveSubsets map[string][]string`）既有 entry
      原地 union 更新——`entryDepString`/`canonicalIdentityForDepString`（內部呼叫既有
      `normalizeLocalDep`，讓本地路徑 dep 與 deploy 端算出同一把 key）建 `existingByIdentity`
      索引，`setEntrySkillSubset` 統一改寫（取代並移除舊 `clearPersistedSkillSubset`，
      reset 與既有更新走同一路徑，不再是獨立函式）；
      附帶修復（同一批次發現，非獨立 bug）：`runInstall` 的 `existing` map 現在先對
      `m.ParsedDeps` 跑一輪 `normalizeLocalDep` 再建立，修正本地路徑 dep 二次 CLI 安裝時
      key space 不一致造成的重複 entry。
   → verify: 步驟 7 綠；同 repo 三階段測試
     `go test ./cmd/apm-go -run TestInstall_SkillSubsetSameRepoUnion -count=1` 綠（C3 迴歸）；
     wildcard 迴歸群：
     `go test ./cmd/apm-go ./internal/deploy -run 'SkillWildcard|SkillFilter' -v -count=1` 綠
     （`TestRunInstall_SkillWildcardDeploysAllSkills`、
     `TestPersistPackagesToManifest_SkillWildcard_NewPackageWritesStringForm`、
     `TestPersistPackagesToManifest_SkillWildcard_ClearsExistingSubset`、
     `TestBuildLockfile_SkillWildcardDoesNotRecordSubset`、
     `TestRun_SkillFilterScopedToDepKey`、`TestRun_SkillFilterAbsentKeyDeploysAll`）；
     update 一致性 `go test ./cmd/apm-go -run TestUpdate_RespectsSkillSubset -count=1` 綠；
     全量 `go build ./... && go vet ./... && go test ./... -count=1` 全綠。
9. **unknown skill 政策（H3）**：先驗證後持久化的順序調整 + 新名報錯原子性測試 +
   persisted 消失警告測試。**已完成**（`cmd/apm-go/install.go` 新增
   `validateNewSkillNames`，在 `effectiveSkillSubsets` 之後、`buildLockfile` 之前
   呼叫——早於 `deploy.Run` 的任何 target 寫入與 `apm.lock.yaml`/`apm.yml` 寫入；
   `internal/deploy/deploy.go` 的 `Run` 新增 `skillSubsetDiags`，對「persisted
   子集內的名稱找不到對應 primitive」發出 warning，而非 error）。
   → verify: `go test ./cmd/apm-go -run TestInstall_UnknownSkill -count=1` 綠
     （`TestInstall_UnknownSkill_NewNameErrorsAtomically`：純新名/新名+既有名混合，
     兩者皆報錯且 apm.yml byte 級不變、apm.lock.yaml 未寫入、target 目錄空；
     `TestInstall_UnknownSkill_PersistedNameDisappearsWarnsAndKeeps`：persisted
     名稱因上游更新消失時，bare install 不報錯、stderr 含警告、manifest/lockfile
     子集維持不變）——已跑綠。
10. **污染收斂（C1）**：先寫觀察測試確認既有 re-deploy 對 stale 檔的行為；
    不足則實作 ownership-aware reconciliation（hash 驗證 + 接管檢查，重用 uninstall helper）。
    **已完成**（觀察確認舊行為「不收斂」——`deploy.Run` 純加法式部署，縮小
    subset 後舊檔案永久殘留；`cmd/apm-go/install.go` 新增
    `reconcileStaleSkillDeployments`，在 deploy 完成、`newLock` 各 dep 的
    `DeployedFiles`/`LocalDeployedFiles`/MCP 合併檔都填好之後呼叫，比較
    `existingLock`（舊帳本）與 `newLock`（本次全庫已宣告的路徑集合）的差集，
    對每個 dep 的 stale 子集重用 `internal/deploy/uninstall.go` 既有的
    `RemoveDeployedFiles`（hash 驗證 + 已存在性檢查），不重造 wheel）。
    → verify: `go test ./cmd/apm-go -run TestInstall_StaleSkillReconciliation -count=1` 綠
      （全量安裝兩個 skill 後縮窄到其中一個：3 份未修改的殘留複本被清除，1 份
      使用者手動修改過的複本被保留並在 stderr 產生警告，查實際檔案系統
      而非只查 lockfile/DeployResult）——已跑綠。
11. **邊界批次**：混合 wildcard、參數順序、dev deps、多 positional、寫入失敗原子性注入。
    **已完成**（混合 wildcard 與多 positional 共用 --skill 兩項促成
    `validateNewSkillNames` 的一處設計修正：驗證語意從「每個 targeted dep 都要有
    這個名稱」放寬為「至少一個 targeted dep 有這個名稱」——否則兩個不同 repo
    共用同一組 --skill 清單、各自只占其中一個名稱時會被誤判為 typo 而整體報錯；
    寫入失敗原子性採**記錄限制**而非 FS 權限注入測試，見 design.md §4 BUG-2
    小節新增決策段落——`os.Chmod` 唯讀在 CI 常見的 root 執行環境下會被略過，
    是不可靠的驗證手段）。
    → verify:
      `go test ./cmd/apm-go -run TestRunInstall_SkillMixedWildcardResetsToFull -count=1` 綠
      （narrow 後 `--skill x --skill '*'` 全量重置，apm.yml/apm.lock.yaml 子集皆清空）；
      `go test ./cmd/apm-go -run TestRunInstall_DevDependency_SkillSubsetHonored -count=1` 綠
      （devDependencies.apm 持久化子集同樣被 SkillFilter 尊重）；
      `go test ./cmd/apm-go -run TestRunInstall_MultiplePositionalPackages_SharedSkillFlag -count=1` 綠
      （兩個不同 repo 共用 --skill 清單，各自只部署自己擁有的名稱，不報錯，
      並記錄「持久化子集含對方 repo 名稱」的既有行為，非本任務新增缺陷）；
      既有迴歸不受影響：
      `go test ./cmd/apm-go ./internal/deploy -run 'SkillWildcard|SkillFilter|SkillSubset' -count=1` 綠。
12. **全量驗證 + 閘門 + commit**（`fix(deps): --skill 子集持久化與重佈署尊重`；
    Phase 0 的 identity 若無獨立價值可併入此 commit，有則先行獨立 commit）。
    → verify: `go build ./...`、`go vet ./...`、`go test ./... -count=1` 三條各自 exit 0；
      codex 閘門無 CRITICAL/HIGH。

## Phase 2 — BUG-1：大小寫重複 dep

13. **RED**：同 repo 大小寫兩來源 → 斷言 Resolved 1 + apm.yml 單 entry + lockfile 單 dep +
    apm_modules 單目錄 + 無 shadow 噪音（codex M7 全狀態斷言）。
    → verify: `go test ./cmd/apm-go -run TestInstall_CaseFoldDedup -count=1` 失敗。
    **已完成**：fixture 透過 `GIT_CONFIG_GLOBAL` + `insteadOf`（`caseFoldGitConfig`，
    `cmd/apm-go/install_casefold_test.go`）把 `Owner/Repo`／`owner/repo` 兩個大小寫
    positional package 導向同一個本地 git repo（`gitSkillRepo`，多檔多 target），
    不需真實網路；RED 階段確認在修復前會產生 `Resolved 2`＋apm.yml/lockfile 各兩筆
    ＋apm_modules 兩目錄＋shadow 噪音。
14. **GREEN**：resolver 去重 / requestedKeys / lockfile 比對接線 §0 identity。
    → verify: 步驟 13 綠。
    **已完成**：
    a. `internal/resolver/resolver.go` 新增 `bfsKey(ref)`（§0 identity + virtualPath
       後綴，local/parent 落回既有 `depKey` 原始路徑）取代 BFS 記帳用的
       `key := depKey(entry.ref)`／`childKey := depKey(subDep)`——`constraints`/
       `pins`/`processed`/`depRefs`/`depOrder`/`childrenOf`/`depDepth` 全部改用
       `bfsKey`，讓大小寫不同但同 identity 的兩個 queue entry 收斂成同一個 BFS
       節點（`Resolved 2`→`Resolved 1` 的根本修法，且對 diamond 衝突機制免費適用：
       同 identity 不同 selector 現在會自然撞上既有 `checkLiteralConflict`）。
       結果建構迴圈新增 `displayKey := depKey(depRefs[key])`，`ResolvedDep.Key`/
       `RepoURL` 一律用 displayKey（= first-declared 原始大小寫）而非內部 canonical
       key，避免外洩 canonical 格式（尤其 registry/marketplace 的 prefixed 格式）
       到 `regLoader.Resolutions()`／`buildLockfile` 等下游查表點；
       `result.MarketplaceProvenance` 兩處仍刻意保留 `depKey(resolved)`（raw）作
       key，因為它是跟 displayKey 對應查找，不能用 canonical bfsKey（曾一度誤改，
       導致 `TestRunInstall_MarketplaceDictDep_*` 迴歸，已修正回 depKey 並加註解）。
    b. `cmd/apm-go/install.go` `runInstall`：`existingByIdentity map[string]string`
       （canonical identity → first-declared `deploy.DepRefKey`）新增於既有
       `existing` 掃描迴圈；positional package 迴圈內，若這次 pkg 的 `CanonicalDepKey`
       命中 `existingByIdentity`（apm.yml 既有宣告 **或** 這次呼叫更早的 positional
       package），`key` 折疊成 first-declared key，並在折疊時比較新舊 ref 的
       `Reference`（selector）——不同則印 `ux.Warn`（"conflicts with
       already-declared ... keeping the first-declared ref"，first-declared 規則
       + 警告，符合 design.md §0/§2 的「不靜默合併」要求）；`appendedThisCall`
       （與 `existing` 分離，避免污染 R9/R10c 的「already in apm.yml」判定）防止
       同一次呼叫內第三個大小寫變體重複 append。
    c. `cmd/apm-go/install.go` `persistPackagesToManifest`：既有的
       `existingByIdentity`/`existingPkgs`（BUG-2 的 C3 修法）先前只在迴圈**之前**
       建立一次、迴圈內從未更新——同一次呼叫的第二個大小寫變體因此仍會各自
       append 成 apm.yml 的第二筆 entry。修正為每次 append 新 entry 後立即登記
       `existingByIdentity[identity] = len(apmSeq.Content)-1`（或
       `existingPkgs[pkg]=true`），使同呼叫內第二個變體改為呼叫
       `setEntrySkillSubset` 就地更新，而非重複新增。
    d. **附帶修正（同批次發現的獨立既有 bug，非 BUG-1 本體但擋住 AC-B1-3 驗收）**：
       `internal/lockfile/write.go` `depSemanticEqual` 從未比較 `SkillSubset`
       欄位——導致「只有 skill_subset 改變、其餘欄位不變」的重佈署（例如
       `--skill '*'` RESET）被 `IsSemanticEqual` 誤判為 no-op，`deployAndFinalize`
       在寫入 apm.lock.yaml/apm.yml **之前**就以「Already up to date」提早返回，
       RESET 结果從未落盤。已加入 `slicesEqual(a.SkillSubset, b.SkillSubset)`。
15. **守衛**：不同 repo 不受影響；F4 shadowed 仍輸出；混合大小寫舊 lockfile 升級相容；
    BUG-1×BUG-2 交互（`RepoA --skill a → repoa --skill b → REPOA --skill '*'`）。
    → verify: 四支守衛測試綠（實作時回填名稱）。
    **已完成**，皆位於 `cmd/apm-go/install_casefold_test.go`：
    - `TestInstall_CaseFoldDedup_DifferentReposNotMerged`（AC-B1-2：不同 owner
      不同 repo 維持兩筆 apm.yml/lockfile entry，未被過度 case-fold 誤合併）；
    - `TestInstall_CaseFoldDedup_SelectorConflictNotSilentlyMerged`（同 identity
      不同 ref/selector：first-declared 勝出 + `ux.Warn` 警告，不靜默）；
    - `TestInstall_CaseFoldWildcardReset`（BUG-1×BUG-2：`RepoA/x --skill skillA`
      → `repoa/x --skill skillB`（union）→ `REPOA/x --skill '*'`（RESET）三階段，
      manifest/lockfile/apm_modules/實際部署檔案全程單一且一致）；
    - `TestInstall_CaseFoldDedup_LockfileUpgradeCompat`（AC-B1-4：手動注入
      pre-fix 風格的混合大小寫 lockfile 污染後重跑 install，收斂回單一 dependency、
      apm_modules 目錄數不增長、first-declared 拼寫保留）。
    - 另有 `TestInstall_CaseFoldDedup`（步驟 13/14 對應的核心 GREEN 測試，含
      bare install 與 `runUpdate` 之後仍保持單一的 V1-2 迴歸）。
    → verify: `go test ./cmd/apm-go -run TestInstall_CaseFold -v -count=1` 五支全綠。
16. **全量驗證 + 閘門 + commit**（`fix(resolver): dep-key 大小寫正規化`）。
    → verify: 同步驟 12 三條指令；codex 閘門無 CRITICAL/HIGH。

## Phase 3 — 乙類高優先（R17 / R7 / R11 / R12a/b / R13 / R15 / R16）

> 每 R 一支輸出斷言測試（+ 非 TTY 無 ANSI）；資料必須「已算出、只是沒印」，
> 例外（R7 result contract）明確標記。

17. **R17**：typed no-target error + error-mapping 層針對性 suppress usage。
    → verify: `go test ./cmd/apm-go -run TestInstall_NoTargetDiagnostic -count=1` 綠
      （含 exit code 2 + stderr 有 marker 清單 + 無 `Flags:`）；
      反向守衛 `-run TestInstall_UsageStillShownOnFlagError` 綠；
      實跑 `cd evals/bundle-demo && go run ../../cmd/apm-go install; echo exit=$?`
      → stderr 無 Cobra dump、exit=2。
18. **R7**：uninstall 摘要（套件名 + apm.yml 路徑 + 實際移除計數或「處理 N 筆記錄」措辭）。
    → verify: `go test ./cmd/apm-go -run TestUninstall_Summary -count=1` 綠
      （含 missing/modified 檔案情境的計數正確性）。
19. **R11**：mcpinstall deployed 清單 + 絕對路徑。
    → verify: `go test ./cmd/apm-go -run TestRunMCPInstall -count=1` 綠（斷言含 target 清單
      與 `filepath.Abs` 路徑）。
20. **R12a/b**：pack 正式清單 + local-bundle 聚合樹。
    → verify: `go test ./cmd/apm-go -run 'TestPack_|TestInstall_LocalBundle' -count=1` 綠；
      F4 守衛斷言（warning 不被聚合吞掉）包含在測試內。
21. **R13 + R15**：MCP 摘要（聚合去重）+ 成功配置計數。
    → verify: `go test ./cmd/apm-go -run TestInstall_MCPSummary -count=1` 綠
      （含同 server 多來源去重、部署部分失敗計數情境）。
22. **R16**：矛盾訊息消除（依 PerDep[""] 後置判定；措辭 `Installed local project`）。
    → verify: `go test ./cmd/apm-go -run TestInstall_LocalOnlyProject -count=1` 綠
      （成功/零檔案/全衝突三情境）；實跑 `evals/test1`：stdout 不同時含
      `No dependencies to install` 與 local 部署樹、summary 非 `Installed 0 dependencies`。
23. **閘門 + commit（分批：R17 獨立；R7+R11；R12/R13/R15/R16 install/pack 群）**。
    → verify: 每 commit codex 無 CRITICAL/HIGH；`go test ./... -count=1` 綠。

## Phase 4 — 乙類低優先 + 收尾

24. **R12c/d**：audit/frozen 成功細節走 `--verbose`。
    → verify: 預設輸出與變更前 byte-diff 為空（P4-28 同法：兩次執行輸出 `cmp`）；
      `--verbose` 輸出含明細清單（測試斷言）。
25. **R12e 評估（語意準則，codex L3）**：只讀欄位 + 呈現 → 做；改 compile 決策/序列化/
    跨 package API → 分流。
    → verify: 評估結論 + 依據寫入 prd（決策記錄，非驗證）；若做，測試綠；若分流，
      新子任務已建立。
26. **R18 決策**：預設不補，記錄理由。
    → verify: prd 勾選（決策記錄）。
27. **最終閘門**：`git diff main...HEAD | codex exec - -c model_reasoning_effort="high"`
    （聚焦：exit code、normalize byte、串流、業務層越界、F4 守衛、reconciliation 刪檔安全）。
    → verify: 無 CRITICAL/HIGH。
28. **A/B 終驗**：BUG-2 三情境 + BUG-1 情境 + 乙類主要指令對照 Python。
    → verify: `research/final-ab-verification.md` 含指令、版本、SHA、輸出與結論
      （codex L3 可重現性要求）。
29. spec 更新（trellis-update-spec：canonical identity 契約、SkillFilter per-dep 形狀）
    → commit → finish-work。
