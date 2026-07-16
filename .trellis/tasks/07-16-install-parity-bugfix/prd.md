# PRD — install 輸出 parity 與業務 bug 修復（乙/丙 + BUG-2 --skill 子集失憶污染）

> 承接 `archive/2026-07/07-14-init-tui-beautify` 的方案 A 分流：該任務只做甲類（純美化，已完成、
> 已併入 main PR #3）。本任務接手 **乙類**（內容 parity，R7/R11–R18）、**丙類**（業務 bug BUG-1），
> 並新增 **BUG-2（--skill 子集失憶污染，2026-07-16 實測發現，最高優先）**。
> 乙類條款全文以移交清單形式收錄於本文件（自舊 prd.md §R7–R18 複製，錨點行號以當時快照為準，
> 甲類 R8/R9/R10/R14/R19 已完成、不在本任務範圍）。

## 背景

- 甲類美化已上 main（`4b4a9cb` merge PR #3，版本 0.2.0）。
- A/B 盤點資料：`archive/2026-07/07-14-init-tui-beautify/research/output-parity-audit.md`、
  `research/full-ab-parity-sweep.md`。
- Python apm 參照：`uv --project /d/Projects/apm-dev/apm run apm`（v0.21.0）。
- A/B 測試環境：`evals/`（`test1`、`bundle-demo` 等 fixture）。

## 優先序

1. **BUG-2**（--skill 子集失憶污染）— 嚴重、使用者實測踩到、資料污染型
2. **BUG-1**（大小寫重複 dep）— resolver/manifest 層
3. **乙類高優先**：R17（第一次設定必踩、高能見度）、R7、R11、R12a/b、R13、R15、R16
4. **乙類低優先**：R12c/d、R18；R12e（compile）需業務層新欄位 → 本任務內評估，過大則再分流

---

## BUG-2 ｜ `--skill` 子集失憶：後續 install 重佈署既有 repo 的全部 skill（最高優先）

### 現象（2026-07-16 實測，apm-go 0.2.0）

```
# 第 1 次
./apm-go install mattpocock/skills --skill grill-me
mattpocock/skills
└── 1 skill -> .agents/skills/, .claude/skills/        ← 正確：只裝 1 個

# 第 2 次（裝另一個 repo 的另一個 skill）
./apm-go install antfu/skills --skill vue
mattpocock/skills
└── 22 skills -> .agents/skills/, .claude/skills/      ← 污染：前一個 repo 的 22 個 skill 全部灌入
antfu/skills
└── 1 skill -> .agents/skills/, .claude/skills/
```

每多裝一個 repo，前面所有以 `--skill` 窄化過的 repo 都會被重新「全量」佈署，
skill 數量持續膨脹，完全違背 `--skill` 的用意。

### 根因（已定位，2026-07-16 分析；2026-07-16 codex 對抗審查補強 C3 第三斷點）

**三個斷點**（前兩個是「寫入端有、讀取端無」，第三個是「寫入端本身對既有 entry 失效」）：

1. **讀取端斷鏈**：
   - `internal/manifest/depref.go:308` `ParseDepDict` 解析 apm.yml dependency 物件時
     **不讀 `skills:` key**；`DependencyReference` struct（`depref.go:13-38`）**沒有 Skills 欄位**
     → 子集在 parse 層被靜默丟棄。
   - （寫入端存在：`persistPackagesToManifest`（`install.go:1386`）新 entry 會寫
     `{git: <pkg>, skills: [...]}` 物件形式；`buildLockfile`（`install.go:955-966`）記
     `skill_subset` 進 lockfile。）
2. **deploy 過濾只看當次 CLI flag**：
   - `deploy.SkillFilter`（`cmd/apm-go/install.go:1053-1059`）只由**本次** `--skill` flags +
     `requestedKeys` 建構；未被本次點名的既有 dep 走 `filter == nil` 分支 → 全量佈署。
3. **既有 manifest entry 的子集不會被更新（codex C3）**：
   - `persistPackagesToManifest` 對已存在的 package 直接 `continue`
     （`install.go:1452-1461`，僅 `--skill '*'` reset 會 `clearPersistedSkillSubset`）。
   - 即：對**同一 repo** 二次 `install same-repo --skill y`（非 wildcard）**根本不更新 apm.yml**
     → 同次操作可分裂成三種狀態：deploy 用 CLI 的 `y`、lockfile 記 `y`、manifest 仍記舊 `x`；
     下次 bare install 又讀回 `x`。此斷點獨立於前兩者，修 1+2 不修 3 仍會帳實不符。
   - 附帶：`existingPkgs` 用**原始字串**比對（`install.go:1428-1442`）→ 與 BUG-1 大小寫
     問題直接交互（`Owner/Repo` vs `owner/repo` 會判成不同 entry、追加第二筆）。

### 語意釐清（重要，避免修錯方向）

- 「第 2 次 install 重新解析既有 repo」**本身是設計**（full-manifest install，
  與 Python apm 相同；舊 checklist P4-11 亦守衛此行為），**不是** bug。
- bug 在於：重佈署既有 dep 時**忽略了它已持久化的 skills 子集**。
- 正確修法方向：讓 persisted 子集在 parse → resolve → deploy 全鏈路傳遞，
  deploy filter = 各 dep 自身的 persisted 子集 ∪ 本次 CLI `--skill`（僅作用於 requestedKeys）。
  **不是**「跳過解析既有 repo」。

### 需求

**B2-1. apm.yml `skills:` 子集讀回**：`ParseDepDict` 解析物件形式的 `skills:` sequence，
`DependencyReference` 新增對應欄位；scalar 形式（無子集）語意不變。

**B2-2. deploy 佈署時尊重 per-dep persisted 子集 + 三處狀態單一來源**：
- 既有 dep 帶 persisted 子集時，重佈署只佈署子集內的 skill。
- 本次 CLI `--skill` 對 requestedKeys 對應 dep 採 **additive union**（CLI 子集 ∪ persisted
  子集，對齊 Python `normalize_and_merge_skill_subset`，issue #1771，
  `apm/src/apm_cli/install/package_resolution.py:155-180`）。
- **既有 manifest entry 必須被原地更新**為 union 結果（修 C3 斷點：不得只在新增分支寫入）。
- **混合輸入契約（codex M4）**：`--skill x --skill '*'` 只要含 `'*'` 即整體 RESET（現行
  `containsSkillWildcard` 語意，正式納入契約）；RESET 清 requestedKeys 對應 dep 的子集 → 全量，
  且不影響其他 dep 的子集。
- **有效子集單一計算點（codex C2）**：manifest 寫入、lockfile `skill_subset`、deploy filter
  三處必須用同一份「有效子集」結果，不得各自推導（含 update 路徑）。

**B2-3. 污染收斂（codex C1 定案：ownership-aware reconciliation）**：
修復後 re-install（含 bare install）時，以**舊 lockfile 的 `deployed_files`** 減去本次新佈署
集合得出 stale 檔案，僅清除「仍符合舊帳本記錄（hash 相符）且未被其他 dep 接管」者；
使用者已修改、共享或無法證明所有權的檔案**保留並警告**（不靜默刪）。
驗收檢查**實際 target 檔案系統**，不得只檢查新 lockfile/DeployResult。
（若實作中發現既有 deploy/uninstall 機制已涵蓋，沿用之並以測試固定；若完全不可行，
必須把本條降級為「migration 指引」並回頭修改本 PRD——兩者不得並存。）

**B2-4. lockfile `skill_subset` 一致性**：修復後 lockfile 記錄與實際佈署一致；
不得出現「lockfile 記 1 個 skill、實際佈署 22 個」的帳實不符。
一致性定義（codex H4）：`skill_subset` == 有效 skill 名集合；`deployed_files` == 本次
實際佈署管理的路徑集合——**skill 數與檔案數不可直接劃等號**（一個 skill 可含多檔、
部署到多個 target）。

**B2-5b. 命令 × 來源行為矩陣（codex H5）**：design.md 必須定案
`install / bare install / update / frozen / uninstall` × `git / registry / marketplace /
manifest-local / local-bundle` 每格的 skills 子集語意（支援、明確拒絕、不適用）；
不支援的 dict 組合應報 unknown/invalid key，**不得靜默丟棄**（假成功防護）。
特別注意：update 共用 `deployAndFinalize` 但 lockfile 先建（C2）；local-bundle 在 manifest
讀取前 early-exit 完全繞過新鏈路；frozen 不 deploy 但污染帳本如何呈現需定義。

**B2-6. 不存在的 skill 名稱政策（codex H3）**：
- **新輸入**：CLI `--skill <name>` 若 `<name>` 不匹配該 repo 任何 skill → **報錯失敗**，
  且 apm.yml / lockfile / target 檔案**均不得被部分更新**（原子性）。
- **既有 persisted 名稱**因上游 update 消失時：**警告 + 保留**（記錄於輸出，不靜默、不自動 prune）。
- 兩項政策先以 Python A/B 驗證其行為，若 Python 語意不同，以「不污染帳本」原則為準並記錄差異。

**B2-5. A/B 驗證**：以相同兩步驟（repo_a --skill x → repo_b --skill y）跑 Python apm，
確認其行為（預期：既有 repo 維持子集）；apm-go 修復後行為對齊（不需逐字對齊輸出措辭）。
（Python 讀回鏈路已於 2026-07-16 原始碼確認：`models/dependency/reference.py:84`
`skill_subset` 欄位、`:920-940` `parse_from_dict` 讀 `skills:` key 並驗證
「必須是 list、每項非空字串」——apm-go 的 B2-1 解析驗證應對齊此規格。）

### 驗收（可獨立執行、二元判定；判定模型依 codex H4 改為「路徑集合」而非「數量相等」）

fixture 要求：每個 skill 含**多個檔案**、部署到**至少兩個 target**（避免 1 skill == 1 檔的
假陽性）。

- [ ] AC-B2-1：兩步驟情境後，每個 target 下**只存在**所選 skill 的預期相對路徑，
      任何未選 skill 的路徑**不存在**；lockfile `skill_subset` == 有效名集合、
      `deployed_files` == 實際佈署路徑集合。
- [ ] AC-B2-2：`--skill '*'` RESET 語意不回歸（wildcard 迴歸測試群
      `go test ./cmd/apm-go -run 'TestInstall.*[Ww]ildcard|TestInstall.*[Rr]eset' -count=1` 綠，
      實作時以實際測試名固定此指令）；混合輸入 `--skill x --skill '*'` == 整體 RESET。
- [ ] AC-B2-3：第 3 次 install（repo_c --skill z）後 repo_a、repo_b 均維持各自子集
      （以 AC-B2-1 的路徑集合模型判定）。
- [ ] AC-B2-4：**同 repo 三階段**（C3 迴歸）：`install r --skill x` → `install r --skill y`
      → bare `install`，三階段後 manifest（union [x,y]）、lockfile、實際佈署三者一致。
- [ ] AC-B2-5：bare `apm-go install`（既有專案含子集 dep）依 apm.yml 子集佈署；
      `apm-go update` 路徑同樣尊重子集且 lockfile 一致（C2 迴歸）。
- [ ] AC-B2-6：不存在的 skill 名（新輸入）報錯且 manifest/lockfile/檔案系統零變更（H3）。
- [ ] AC-B2-7：污染收斂（C1）：以「已污染」fixture（target 含 22 個 skill 檔、apm.yml 子集 1）
      跑修復後 bare install，stale 且未被修改的 skill 檔被清除、被使用者修改者保留 + 警告
      （檢查實際檔案系統）。
- [ ] AC-B2-8：全測試綠 `go test ./... -count=1`；exit code 語意不變。

---

## BUG-1 ｜ 大小寫重複依賴（丙類，移交自舊任務）

`install chrome-devtools-mcp@…` 解析出 `chromedevtools/chrome-devtools-mcp` 與
`ChromeDevTools/chrome-devtools-mcp` **兩個** dep（同 repo、大小寫不同）→ `Resolved 2`（實際 1）、
6 條 `shadowed…first-declared wins`（`internal/deploy/conflict.go`）、第二個 `deployed 0 files`、
Summary 幽靈 bullet。

- 根因：dep-key 未做 case-fold 正規化（resolver / manifest 層）。
- **canonical identity 契約（codex H2/H7，取代先前籠統的「case-fold」）**：
  - 拆分「**repository identity**」與「**resolution selector**」兩個概念：
    - repository identity = host（正規化）+ owner/repo（**僅 GitHub host** case-fold；
      自架 host 保守不動）。等價形式（`github.com/X/Y`、HTTPS、SSH/SCP、預設 host 短格式）
      指向同 repo 時視為同一 identity。
    - resolution selector = ref / version constraint / virtual path / alias——**一律保留原值、
      不得 lower-case**（git ref 大小寫敏感）。
  - 同 identity、不同 selector：**不靜默合併**——依既有 first-declared 規則 + 警告，或明確報衝突。
  - 此 canonical identity 為**單一共用函式**，resolver 去重、`requestedKeys`/`existing` map、
    manifest 查找/更新/reset（`persistPackagesToManifest` 的 `existingPkgs`、
    `clearPersistedSkillSubset`）、lockfile 比對全部經由它——禁止各處自行 `ToLower`（split-brain 防護）。
- **顯示**保留原大小寫（mkt-033 慣例）；first-declared 的寫法勝出。
- ⚠️ 修好後 shadow/0-files/幽靈 bullet 噪音應**自動消失**——不得在 ux 層遮蔽。

### 驗收

- [ ] AC-B1-1：重現情境（大小寫不同、同 repo 的兩個 dep 來源）→ `Resolved 1`，且
      **apm.yml 單一 entry、lockfile 單一 dep、apm_modules 單一目錄、無 shadowed/0-files 噪音**
      （codex M7：不得只看 stdout）；下一次 bare install 與 update 後仍維持單一。
- [ ] AC-B1-2：真正不同的 repo（大小寫相同、名稱不同）不受影響；F4 shadowed 警告
      （不同 repo 同名 skill 碰撞）仍照常輸出（守衛，見下方 F4）。
- [ ] AC-B1-3：BUG-1×BUG-2 交互：`RepoA/x --skill a` → `repoa/x --skill b` → `REPOA/x --skill '*'`
      整合測試——最終 manifest/lockfile 各只有一筆、RESET 生效。
- [ ] AC-B1-4：舊 lockfile 含混合大小寫 key 的升級相容（讀取後不重複安裝）。
- [ ] AC-B1-5：全測試綠；exit code 不變。

---

## 乙類 ｜ 內容 parity（R7/R11–R18，移交自舊任務 prd.md §R7–R18 全文）

**R7. uninstall 輸出資訊補齊（parity，不需逐字對齊 Python 措辭）**
現況過度精簡（`uninstall.go:623-627` 僅 `+ Removed N package(s)` + `+ apm_modules: removed N director`），
相比 Python apm 遺漏：被移除的**套件名**、apm.yml 已更新（**路徑**）、清理的 **integrated 檔案數**。
需補齊到「關鍵資訊不遺漏」。錨點 `uninstall.go:623-627`（非 verbose 摘要）、`486-506`（module 移除）。
**計數語意（codex M1）**：優先用 uninstall 實際移除結果（removed/skipped/failed）；
若只能取得 lockfile `DeployedFiles`，措辭必須是「處理 N 筆記錄」而非「清理 N 個檔案」
（lockfile 是預期帳本，檔案可能已不存在/被修改跳過）。若需 deploy 層回傳實際結果計數，
屬 result contract 變更，明確標記（非純 presentation）。

**R11. `install --mcp`（mcpinstall）輸出補強（parity，presentation-only）**
現況 `mcpinstall.go:170-174` 僅印 `+ Added MCP server "X"` + `transport` + `apm.yml: apm.yml`（相對、寫死）。
- a. **顯示已配置的目標清單**——`deployMCPEntry` 回傳的 `deployed` slice（資料已有、只是沒印）。
- b. `apm.yml: apm.yml` → **絕對路徑**（`filepath.Abs`）。
- （c 間隔項已由甲類 R8 完成。）

**R12. 消除「dry-run/錯誤路徑比正式/成功路徑更詳細」的輸出不對稱**
- a. **pack 正式執行**（`pack.go:252`）：補印檔案清單（dry-run 分支已有）。presentation-only、高優先。
- b. **local-bundle install**（`install.go:765`）：用既有 `result.Files` 補摘要，**沿用 R10b 聚合樹**。高優先。
- c. **audit bare 成功**（`audit.go:88`）：細節走 `--verbose`，預設保留數字。低優先。
- d. **install frozen 成功**（`install.go:537`）：補驗證 dep 數（清單走 `--verbose`）。低優先。
- e. **compile**（`internal/compile.Result` 需新增欄位暴露 `SourcedInstruction`）：**需業務層變更**
  → 本任務內評估，過大則再分流。

**R13. install 主流程 MCP 部署摘要（presentation-only）**
`deployResult.MCPProvenance` 已有 server→targets 資料（`install.go:1115-1124`）但 stdout 沒印。
在部署摘要區印「配置了哪些 MCP server → 哪些 target」。
**聚合去重（codex M3）**：`MCPProvenance` 是 slice，輸出前按 server identity 聚合 target 集合，
同 server 多來源/同 target 重複不得重複列印。

**R15. install 摘要含 MCP server 計數（presentation-only）**
`install.go:1158` `+ Installed N dependencies` → 有 MCP 時 `Installed N dependencies and M MCP server(s)`；
無 MCP 時不顯示該子句。
**計數語意定案（codex M3）**：M = **本次實際成功配置**的 server 數（由 deploy result 成功
provenance 去重計算），非 `len(newLock.MCPServers)` 的 lockfile 總數（可能含既有/local 來源）。

**R16. 空 apm.yml + 有 local 部署時的輸出矛盾（一致性；codex M2 修正分類：
presentation + 使用既有部署結果，非純 presentation）**
`dependencies.apm: []` 但有 `.apm/` local primitives 時：先印 `i No dependencies to install`
（`install.go:544`）、又佈署 local 印樹、最後 `+ Installed 0 dependencies`——三句矛盾。
- a. 有 local primitives 待佈署時不印/改寫 `No dependencies to install`。
- b. summary **定案措辭**：`Installed local project`（不把 local 硬算成 1 個 dependency，
  避免改變計量語意）；判定依 **deploy 後的 `DeployResult.PerDep[""]` 實際成功結果**，
  不得為了顯示提前另做一次掃描（改變掃描時機/IO 即越界）。
- 部署邏輯不變；測 local 成功、零檔案、全衝突三種情況。

**R17. install 無 target 偵測失敗的錯誤訊息（F3，高能見度）**
現況印 `Error: no deployment target detected…` 後接整包 Cobra flag 用法（14 行）。
對比 Python：列出掃描過的 14 種 harness marker + 3 個具體修法 + apm.yml 範例。
apm-go 的 signal-detection 本來就掃過這些路徑（資料已算出、只是沒印）。
- a. 印結構化診斷（掃了哪些、怎麼修）；
- b. **僅該錯誤路徑**抑制 Cobra usage dump（codex H8：用可辨識的 typed error 在 error-mapping
  層針對性 suppress，**不得**對整個 command 設 `SilenceUsage: true` 波及其他錯誤）；
  一般 flag/參數錯誤的 usage 輸出與 exit code 不變（加反向守衛測試）；no-target exit code 維持 2。
錨點 `errNoDeployTarget()`（install.go）。

**R18. 純 local 安裝的 `.gitignore` 訊息（F5，低優先）**
Python 純 local 安裝也印 `[i] Added apm_modules/ to .gitignore` 並建檔；apm-go 無對應行為。
先決策「建立 .gitignore（行為）」或「僅訊息」，避免越界；記錄決策即可，可判定不補。

### 守衛（乙類全程有效）

- **F4 守衛**：R7/R12 等輸出精簡/聚合**不得**吞掉 `shadowed`/`deployed 0 files` 衝突警告
  （apm-go 對 Python 的資料完整性優勢）。
- **F1 勿抄**：Python MCP runtime auto-detect fallback `["vscode"]` 的錯誤設計不得引入。
- **F2**：`antigravity` target 為 apm-go 專屬，無 Python 可比，單邊評品質即可。
- 既有硬性規則不變：exit code 全保留、normalize stdout byte-identical、
  非互動/CI/重導純文字非阻塞、業務層不 import `internal/ux`。

### 驗收（乙類，沿用舊任務移交清單）

- [ ] R7：uninstall 摘要含套件名 + apm.yml 已更新（路徑）+ 清理檔案數。
- [ ] R11：`install --mcp` 顯示已配置目標清單 + apm.yml 絕對路徑。
- [ ] R12a：pack 正式執行印出檔案清單（同 dry-run）；R12b：local-bundle install 用聚合樹補檔案摘要。
- [ ] R12c/d：audit/frozen 成功細節走 `--verbose`（預設不洗版）；R12e：compile 評估結果已記錄（做或再分流）。
- [ ] R13：install 主流程印出 MCP 部署摘要（server→targets）。
- [ ] R15：summary 反映 MCP server 計數。
- [ ] R16：空 apm.yml + 有 local 部署時消除矛盾訊息。
- [ ] R17：install 無 target 時印結構化診斷並抑制 Cobra usage dump；exit code 仍 2。
- [ ] F4 守衛：輸出精簡/聚合未吞掉 `shadowed`/`deployed 0 files` 警告。
- [ ] R18：`.gitignore` 落差已記錄、決策補或不補。

---

## 流程約束（沿用前任務慣例）

- sonnet 子代理實作、主會話審核；**每個 commit 前 codex 對抗式閘門**
  （`git diff … | codex exec - -c model_reasoning_effort="medium"`，stdin 餵法）。
- 原子化 conventional commits、繁體中文、無 attribution。
- BUG-1/BUG-2 屬業務層修改（resolver/manifest/deploy），**必須 TDD**：先寫重現失敗測試（RED）再修（GREEN）。
- 乙類為呈現層：只動 cmd 層輸出行與 `internal/ux`，資料須為「已算出、只是沒印」；
  需新增業務層資料的（R7 清理檔案數、R12e）明確標記並最小化。
