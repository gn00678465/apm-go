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
   a. `deploy.SkillFilter` 改 `Subsets map[string][]string`（空 slice 不變式）+
      deploy_test.go 兩支改形狀；prod+dev deps 同走查表；
   b. `effectiveSkillSubsets()` 唯一計算點（union / 混合 wildcard RESET / identity key）；
   c. 接線 buildLockfile（含未點名 dep）與 deployAndFinalize；update 路徑同接；
   d. `persistPackagesToManifest` 既有 entry 原地 union 更新（canonical identity 比對）+
      `clearPersistedSkillSubset` identity 化。
   → verify: 步驟 7 綠；同 repo 三階段測試
     `go test ./cmd/apm-go -run TestInstall_SkillSubsetSameRepoUnion -count=1` 綠（C3 迴歸）；
     wildcard 迴歸群綠（實作時回填實際 -run pattern）；
     update 一致性 `go test ./cmd/apm-go -run TestUpdate_RespectsSkillSubset -count=1` 綠。
9. **unknown skill 政策（H3）**：先驗證後持久化的順序調整 + 新名報錯原子性測試 +
   persisted 消失警告測試。
   → verify: `go test ./cmd/apm-go -run TestInstall_UnknownSkill -count=1` 綠
     （斷言錯誤時 manifest/lockfile/檔案系統 byte 級不變）。
10. **污染收斂（C1）**：先寫觀察測試確認既有 re-deploy 對 stale 檔的行為；
    不足則實作 ownership-aware reconciliation（hash 驗證 + 接管檢查，重用 uninstall helper）。
    → verify: `go test ./cmd/apm-go -run TestInstall_StaleSkillReconciliation -count=1` 綠
      （污染 fixture → bare install → stale 未修改檔被清、修改檔保留 + 警告，查實際檔案系統）。
11. **邊界批次**：混合 wildcard、參數順序、dev deps、多 positional、寫入失敗原子性注入。
    → verify: 對應測試群綠（實作時回填 -run pattern）。
12. **全量驗證 + 閘門 + commit**（`fix(deps): --skill 子集持久化與重佈署尊重`；
    Phase 0 的 identity 若無獨立價值可併入此 commit，有則先行獨立 commit）。
    → verify: `go build ./...`、`go vet ./...`、`go test ./... -count=1` 三條各自 exit 0；
      codex 閘門無 CRITICAL/HIGH。

## Phase 2 — BUG-1：大小寫重複 dep

13. **RED**：同 repo 大小寫兩來源 → 斷言 Resolved 1 + apm.yml 單 entry + lockfile 單 dep +
    apm_modules 單目錄 + 無 shadow 噪音（codex M7 全狀態斷言）。
    → verify: `go test ./cmd/apm-go -run TestInstall_CaseFoldDedup -count=1` 失敗。
14. **GREEN**：resolver 去重 / requestedKeys / lockfile 比對接線 §0 identity。
    → verify: 步驟 13 綠。
15. **守衛**：不同 repo 不受影響；F4 shadowed 仍輸出；混合大小寫舊 lockfile 升級相容；
    BUG-1×BUG-2 交互（`RepoA --skill a → repoa --skill b → REPOA --skill '*'`）。
    → verify: 四支守衛測試綠（實作時回填名稱）。
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
