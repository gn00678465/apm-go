# Design — install parity 與業務 bug 修復

> 對應 prd.md。BUG-2 → BUG-1 → 乙類的順序設計；每段標明「動哪層、不動哪層」。
> 2026-07-16 依 codex 對抗審查（research/codex-plan-review-run4.md，3C/8H/7M/4L）全面修訂。

## 0. 共用基礎：canonical identity（先於 BUG-1/BUG-2，codex H2/H6/H7）

BUG-1 與 BUG-2 都依賴「dep 是不是同一個」的判定，且 codex 證實現況至少有
resolver key、`requestedKeys`/`existing` map、`persistPackagesToManifest` 的
`existingPkgs`（原始字串）、lockfile key 四處各自比對——先建單一函式再修兩個 bug，
避免 split-brain。

```go
// internal/manifest（或 internal/depid）——單一共用，禁止呼叫端自行 ToLower
func CanonicalRepoIdentity(ref *DependencyReference) string
```

- **repository identity**：host 正規化（https/ssh/scp/短格式等價歸一）+ owner/repo。
  **僅 GitHub host**（github.com 與預設 host）對 owner/repo case-fold；
  自架 host 保守保留原大小寫。
- **resolution selector**（ref / version constraint / virtual path / alias）**不參與** identity、
  不 lower-case（git ref 大小寫敏感）。
- 同 identity 不同 selector：不靜默合併——沿用既有 first-declared 慣例 + 警告。
- 使用點（全部改經此函式）：resolver 去重、requestedKeys/existing、manifest 查找/更新/
  reset、lockfile 比對、deploy filter map key。

## 1. BUG-2 ｜ --skill 子集失憶（三斷點：parse 讀回 / filter / 既有 entry 更新）

### 1.1 資料流現況 vs 目標

```
現況（三斷點）：
  apm.yml {git: pkg, skills: [x]} --ParseDepDict--> skills 被丟棄            ← 斷點1
  CLI --skill -> deploy.SkillFilter{Names, DepKeys}（全域一組）              ← 斷點2
  persistPackagesToManifest: existingPkgs[pkg] → continue（既有 entry 不更新）← 斷點3（codex C3）

目標：
  effectiveSkillSubsets()（唯一計算點）
    ├─> persistPackagesToManifest（新 entry 寫入 + 既有 entry 原地 union 更新）
    ├─> buildLockfile（skill_subset = 有效子集，含未點名 dep）
    └─> deploy.SkillFilter{Subsets map[identity][]string}（per-dep 過濾）
```

### 1.2 變更點

**a. `internal/manifest/depref.go` — skills: 讀回（斷點 1）**
- `DependencyReference` 新增 `SkillSubset []string`（`nil` = 無子集 = 全量）。
- `ParseDepDict` git 分支讀 `skills:` sequence，驗證規格**完整對齊 Python
  `reference.py:915-945`（codex H1，不只「list + 非空」）**：
  - 非 sequence → 錯誤 `'skills' field must be a list of skill names`
  - 空 sequence → 錯誤（Python 同）
  - 每項必須是字串 scalar（檢查 tag `!!str`，數字 scalar 不得矇混）且 trim 後非空
  - **path safety**：拒絕 `.`、`..`、含 `/` `\` 的名稱（防 traversal；對齊
    `validate_path_segments`）
  - 正規化：trim、去重、排序後存入
  - `skills: null` → 視為無子集（等同 key 不存在），以測試固定
- **來源矩陣（codex H5）**——`skills:` key 的接受範圍：

  | dict 來源分支 | `skills:` 語意 |
  |---|---|
  | git | 支援（上述規格） |
  | registry / marketplace / local(path) / parent | **明確報錯 unknown key**（不得靜默丟棄；marketplace 分支現已 reject unknown keys，比照） |

**b. `internal/deploy/deploy.go` — SkillFilter 改 per-dep map（斷點 2）**
- `type SkillFilter struct { Subsets map[string][]string }`
  （key = canonical identity；value = 非空白名單）。
- **不變式（codex H6）**：map 中不得存在空 slice——建構時遇空集合即為程式錯誤（panic/error）；
  「key 不存在」是唯一的「全量」表示法。
- 佈署 skill primitive 時以自己 dep 的 canonical identity 查表；**prod 與 dev deps
  （`ParsedDeps` + `ParsedDevDeps`）都走同一查表**（codex M6）。
- 既有 `deploy_test.go` 兩支 SkillFilter 測試改新形狀、語意保留（scoping + wildcard）。

**c. 有效子集唯一計算點（codex C2）**
```go
// cmd/apm-go/install.go
func effectiveSkillSubsets(m *manifest.Manifest, requestedKeys map[string]bool,
    cliSubset []string) (map[string][]string, error)
```
- 規則：
  1. 走訪 manifest dep refs：`SkillSubset != nil` → `map[identity] = persisted`。
  2. CLI `--skill`（非 wildcard）對 requestedKeys 對應 identity：
     `map[id] = union(persisted, cli)`（trim/去重/排序，對齊 Python
     `normalize_and_merge_skill_subset`，issue #1771）。
  3. 含 `'*'`（混合輸入亦然，codex M4）：requestedKeys 對應 identity 從 map **移除**
     （RESET → 全量），其他 dep 子集不受影響。
- **呼叫時機：在 `buildLockfile` 與 `deploy.Run` 之前算好，同一份結果傳給
  manifest 寫入、lockfile、deploy 三處**——update 路徑（`update.go:228` 以
  `skillSubset=nil, requestedKeys=nil` 進 `deployAndFinalize`）也必須先從 manifest
  算出同一份 map，使 update 的 lockfile 與 deploy 一致（C2 的核心）。

**d. `persistPackagesToManifest` — 既有 entry 原地更新（斷點 3，codex C3）**
- 現況 `existingPkgs[pkg] → continue`（僅 wildcard 走 `clearPersistedSkillSubset`）。
- 改為：非 wildcard 且該 pkg 有有效子集 → **定位既有 entry**（scalar 或 object form，
  以 canonical identity 比對而非原始字串——同時修 BUG-1 交互），原地改寫為
  `{git: <原字串>, skills: [<union 後>]}`；scalar entry 需升級為 object form。
- wildcard RESET：`clearPersistedSkillSubset` 同樣改 canonical identity 比對
  （修「`--skill '*'` 清不掉不同大小寫 entry」的 H2 交互）。

**e. `buildLockfile` — 記有效子集**
- `ld.SkillSubset = effectiveSubsets[identity]`（含未被本次點名但有 persisted 子集的 dep；
  現況只記 requestedKeys 是帳實不符的另一半）。

**f. 不存在的 skill 名稱（codex H3 / prd B2-6）**
- 佈署前以解析出的 skill 清單驗證有效子集：
  - **新 CLI 名稱**查無 → 報錯退出，**在 manifest/lockfile 寫入之前**（原子性：
    錯誤路徑三份狀態零變更——寫入順序調整為「先驗證、後持久化」）。
  - **persisted 名稱**因上游 update 消失 → `ux.Warn` 保留（不 prune、不失敗）。

**g. 污染收斂（codex C1 / prd B2-3）— ownership-aware reconciliation**
- 資料源：**舊 lockfile 的 `deployed_files` + hash**（帳本）vs 本次新佈署集合。
- stale = 舊帳本有、新集合無。逐檔判定：
  - 檔案存在且 hash == 舊帳本記錄 且未被其他 dep 的新帳本接管 → 刪除。
  - hash 不符（使用者改過）/ 被其他 dep 接管 / 已不存在 → 保留（或跳過）+ `ux.Warn`。
- 優先重用既有 uninstall 的檔案移除 helper（uninstall 已有「按 lockfile 記錄刪 integrated
  檔案」的機制）；實作第一步先寫觀察測試確認既有 re-deploy 是否已含此行為，已含則以
  測試固定，不重複造。
- 驗收查**實際檔案系統**（prd AC-B2-7）。

### 1.3 不動的部分
- full-manifest re-resolve 行為不變（P4-11 守衛延續）。
- resolver 不加欄位：filter 統一在 cmd 層由 manifest refs 建構後傳入
  （`deploy.Run` 雖收 `m`，仍以顯式傳 map 為準，利於測試）。
- `--skill` 既有 CLI guard（無 positional 報錯、與 --frozen/--mcp 互斥）不變。
- local-bundle 路徑（early-exit，不經 manifest）：本次不改其子集行為，
  於矩陣中標記「不適用（自帶 --skill 過濾）」並以既有測試守衛。
- frozen：不 deploy、不寫 manifest —— 子集語意不適用；污染帳本下 frozen 的行為
  以一支觀察測試固定現況即可。

## 2. BUG-1 ｜ 大小寫重複 dep

- 以 §0 的 `CanonicalRepoIdentity` 為唯一比對點；**顯示與序列化保留原大小寫**
  （first-declared 寫法勝出）。
- 影響點（全部改經共用函式，禁止散落 ToLower）：resolver 去重、requestedKeys/existing、
  lockfile dep 比對、`persistPackagesToManifest.existingPkgs`、`clearPersistedSkillSubset`、
  deploy filter map key、`apm_modules/` 目錄 key 的比對（目錄名本身保留 first-declared 寫法）。
- 相容性：舊 lockfile 含混合大小寫 key → 讀取時以 canonical identity 歸戶，不重複安裝
  （prd AC-B1-4）。
- 同 identity 不同 selector（ref/virtual path）→ 不合併，first-declared + 警告（§0）。

## 3. 乙類（R7/R11–R18）— 呈現層

- 全部走 `internal/ux` 既有門面，無新 ux API 需求。
- **R7**：計數優先取 uninstall 實際移除結果（removed/skipped/failed）；若引入
  「刪除結果計數」回傳欄位屬 result contract 變更，**明確標記非純 presentation**（codex M1）。
  退而求其次用 lockfile 記錄時措辭為「處理 N 筆記錄」。
- **R11**：mcpinstall 印 `deployed` slice + `filepath.Abs(apm.yml)`。presentation-only。
- **R13/R15**：從 **deploy result 成功 provenance** 聚合去重（server identity → target set）；
  R15 的 M = 本次實際成功配置數（codex M3）。
- **R16**：判定與 summary 依 **deploy 後 `DeployResult.PerDep[""]`**，不提前掃描（codex M2）；
  措辭定案 `Installed local project`；測成功/零檔案/全衝突三情境。
- **R17**：`errNoDeployTarget` 改 typed error；在 error-mapping 層**僅對該型別** suppress
  usage（codex H8——不對整個 command 設 `SilenceUsage`）；加「一般 flag 錯誤仍印 usage」
  反向守衛測試；exit code 2 不變。
- **R12e（compile）分流準則（codex L3，語意取代行數）**：若僅「新增既有計算結果的只讀
  欄位 + 呈現」→ 留本任務；若需改 compile 決策、序列化格式或跨 package API → 分流。
- **R18**：預設「僅記錄、不補」。若日後補，屬行為變更需求（idempotency、既有內容保留、
  換行格式）另立。

## 4. 測試策略

- **BUG-2（TDD 硬性）**：
  - fixture：兩個假 repo；每 skill **多檔**、**兩個 target**（codex H4）。
  - RED 順序（codex M4/L2 修正）：先加 `SkillSubset` 欄位（可編譯）→ parser 行為 RED
    （讀回測試失敗於「欄位未被填」而非 compile error）→ e2e 污染 RED（兩步驟後斷言
    路徑集合）。每步都停在**明確 assertion failure**。
  - 斷言模型（codex M5/M7）：核心 e2e 同時查 (1) 實際 integrated files
    (2) apm.yml entry (3) lockfile dep + `skill_subset` + `deployed_files`
    (4) `apm_modules` 唯一目錄 (5) 第三次 install 與 update 後仍成立。
  - 邊界（codex H3/M4/M6）：unknown skill（單一/部分存在）、`--skill x --skill '*'`
    混合、參數順序互換、dev dependency persisted 子集、prod/dev 同 repo、
    多 positional 共用 --skill、requested 未 resolve。
  - 原子性（codex M5）：注入 manifest/lockfile 寫入失敗，斷言舊檔不被部分覆寫；
    若架構無交易能力，在 design 記錄限制與恢復策略。
- **BUG-1（TDD 硬性）**：RED `Resolved 2` → GREEN；守衛：不同 repo 不受影響、
  F4 shadowed 仍在、混合大小寫 lockfile 升級相容、BUG-1×BUG-2 交互
  （`RepoA --skill a → repoa --skill b → REPOA --skill '*'`）。
- **乙類**：每 R 一支輸出斷言測試 + 非 TTY 無 ANSI；R17 加 exit code == 2 與
  反向 usage 守衛。
- 全程 `go test ./... -count=1` 綠；`-race` 交 CI。

## 5. 風險與回滾

- SkillFilter 形狀變更觸及 deploy 層 API（internal 內）——deploy_test.go 改寫非刪除。
- canonical identity 引入影響面廣（resolver/manifest/lockfile/deploy 交會）——
  以 §0 單一函式 + 全鏈路整合測試控制；升級相容測試（AC-B1-4）必備。
- reconciliation（§1.2g）是唯一會**刪檔**的新行為——hash 驗證 + 其他 dep 接管檢查
  雙保險，任何不確定即保留 + 警告；獨立 commit 便於 revert。
- commit 邊界：§0 identity / BUG-2 / BUG-1 / 乙類各自獨立 commit，每個 commit 前
  codex stdin 閘門。
