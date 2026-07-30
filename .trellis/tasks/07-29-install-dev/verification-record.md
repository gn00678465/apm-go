# 驗證紀錄 — 07-29-install-dev

> **狀態：待使用者驗證。任務未標記完成。** `task.py finish` 未執行。

- 實作者：`trellis-implement`（兩輪）
- 驗證者：主 session（獨立重跑與重現，不採信子代理回報）
- 外部稽核：fresh-context subagent（codex 不可用，見下）
- 日期：2026-07-30

---

## Tier 1

```
pwsh -NoProfile -File .trellis/tasks/07-29-install-dev/verify.ps1
→ TIER 1 GREEN · 13 項 · 覆蓋率 86.9%
```

### 閘門本身先被修過

這支閘門原本有與 `targets-init-shape` 同一批的假綠缺陷，已於 commit `7a1845f` 修復。
其中一項是主 session 實測證明的：在 `internal/lockfile/archivebytes_test.go` 插入
`t.Errorf("MUTATION")` 後，舊版閘門仍回報 `ok [AS7/coverage 86.9%]`。

另有一項是子代理發現、我沒列出的：**重複 `Pop-Location`** 讓第一個 probe 之後的
檢查跑在錯的目錄，導致 `go.mod` 新增相依檢查因 git 報錯而匹配不到，**靜默變成 no-op
並回報假 ok**。

### mutation 測試

| # | Mutation | 結果 |
|---|---|---|
| 1 | 拿掉 `install.go:432-436` 的 `if deps.dev` 路由 | ✅ `TestRunInstall_Dev_WritesDevDependenciesNotDependencies` 紅 |
| 1b | 同上，觀察閘門的 AC42/AC43 **CLI 探針** | ⚠️ **仍 ok** —— 探針只看 apm.yml，而 `persistPackagesToManifest` 是依旗標寫檔，與記憶體路由是兩條獨立路徑 |
| 2 | `verify.ps1` AC42 反向斷言的 regex | 見下節 |

**#1b 的意義**：抓到這個 mutation 的是**強化後才有的全套件 `go test ./... -count=1`
檢查**。強化之前這支閘門會報 GREEN。這是閘門強化價值的具體證據，不是推論。

### 閘門自身的一個 regex 缺陷（主 session 發現並修）

`verify.ps1` 的 AC42 反向斷言原本用 `-match`（PowerShell 預設**不分大小寫**），
而 `"devDependencies:"` 含子字串 `"Dependencies:"` → 在**正確**產物上誤報。實測三種寫法：

| fixture | `-match` | `-cmatch` | `^` 錨定 + `-cmatch` |
|---|---|---|---|
| 只有 devDependencies（正確） | True（誤報）| False | False |
| 真的重複寫入 dependencies | True | True | **True** |

即加上錨定與大小寫敏感後**仍抓得到真缺陷**，只是不再誤報。已改為 `^` + `-cmatch`。

---

## Tier 2 — 外部對抗性稽核

`codex exec -s read-only` 不可用（`codex-windows-sandbox-setup.exe` 遺失，
`.sandbox-bin` 只有 `codex.exe` 與 `codex-command-runner`）。
**未以 `--sandbox danger-full-access` 繞過** —— 那會拿掉唯讀保證。
改用 `AGENTS.md` §5 認可的 fresh-context subagent。

### 阻斷級：跨區段重複宣告

**根因（兩層守門都是單區段視野）**

1. `install.go:295` 的 `existing` / `existingByIdentity` 只用 `m.ParsedDeps` 播種
2. `persistPackagesToManifest`（`install.go:2118`）只掃它要寫的**那一個**區段

**主 session 獨立重現（修復前）**

```
A: 已在 devDependencies，跑 install（無 --dev）→ deps含=true dev含=true  重複
B: 已在 dependencies，跑 install --dev        → deps含=true dev含=true  重複
```

兩者 `runInstall` 都回 `err=<nil>`，零錯誤訊號。

**連帶損害（情境 B）**：`existing[key]` 為 true → `install.go:423` 提早 `continue`
→ `m.ParsedDevDeps` 從未被加進這筆 → `install.go:787` 的 AC45 標記迴圈看到空的
`ParsedDevDeps` → lockfile 記錄拿到 `package_type=""`。

> **我前兩次重現嘗試失敗，原因是我自己的錯**：第一次把套件放進了 `skillSubset`
> 參數位置，第二次 fixture 缺 `targets:`。差一點就把「我沒重現出來」誤報成
> 「稽核者的主張不成立」。**參數位置與 fixture 有效性要先驗證，再下結論。**

**使用者裁定（AskUserQuestion，2026-07-30）：搬家。**
理由：npm 慣例（`npm i -D X` 會把 X 從 `dependencies` 移到 `devDependencies`），
最貼近使用者打這個指令的意圖。搬家有 `ux.Info` 訊息，不靜默；原區段保留 `apm: []`。

**修復後主 session 複驗**

```
A: → deps含=true  dev含=false lock有package_type=false   （正確：已成 prod dep）
B: → deps含=false dev含=true  lock有package_type=true    （連帶損害也修好）
```

### 次要：舊 lockfile 升級路徑

`depSemanticEqual` 現在比較 `PackageType`。稽核者實測：舊 lockfile（無
`package_type` 鍵）升級後第一次 `install` 會整份重寫，第二次穩定回報
`Already up to date` —— **不是無窮重寫迴圈**，行為正確，已補回歸測試。

---

## 這一輪的流程問題

**子代理三次未把新測試加進閘門。** 前兩次在 `targets-init-shape`，這次是
`AC42-cross` 的兩個測試。測試存在但閘門不知道 → 明天被刪掉不會有紅燈。
三次都由主 session 事後補上。

**教訓**：子代理回報「全綠」時，必須另外查 `verify.ps1` 有沒有真的納入新測試，
不能只看閘門輸出的顏色。閘門檢查數應該隨新測試增加而增加，數字不變就是訊號。

---

## 未處理 / 交由使用者裁定

- **`verify.ps1:81` 的 AC42/AC43 CLI 探針會打網路**（`install --dev pbakaus/impeccable`）。
  離線時會誤紅。已在該處加註解說明，**未改成離線** —— 改掉需要一個 local fixture
  registry，成本未估。目前靠全套件 `go test` 提供離線的等價保護。
- **AC45 的閘門檢查是 `git grep PackageType`**，只證明「欄位存在」，
  不證明「值正確」。真正的斷言在 Go 測試裡（AC42-cross 兩個測試會驗
  `package_type == "marketplace_plugin"`）。閘門這條偏弱，明列於此。

---

## 第三輪 — 2026-07-30 第二次根因分析後的兩個修復（同一 session 的 codex 交叉驗證）

外部稽核（`codex exec -s read-only`，透過 node_repl fallback）對前一輪的修復本身
再做一次根因分析，找出兩個先前輪次遺漏的缺陷，皆在 `cmd/apm-go/install.go`
（本 task 的直接檔案範圍）：

### 問題 1 — 跨區段搬家丟失 entry metadata（ref:/alias: 等）

**根因**：`removeMatchingEntry`（`install.go:2361` 原版）原本只回傳 `bool`
（是否移除了東西），呼叫端（`persistPackagesToManifest` 的搬家分支）因此只能
用 `pkg` 字串重建一個全新的 `{git, skills}`/純量 entry，把原 entry 的
`ref:`、`alias:`、`path:` 等欄位全部丟掉。與 `setEntrySkillSubset`
（同檔 `:2398` 附近既有的同區段更新路徑）已經修過的問題**同源但範圍不同**——
`setEntrySkillSubset` 只保護「同區段更新」，「跨區段搬家」這條路徑當時漏補。

**修復**：`removeMatchingEntry` 改回傳被移除的 `*yamllib.Node` 本身（而非
`bool`），呼叫端改為**搬移原節點**並僅呼叫 `setEntrySkillSubset` 調整
`skills:` 欄位，不再重建整個 entry。

**回歸測試**：`TestRunInstall_Dev_CrossSectionMove_PreservesEntryMetadata`
（`cmd/apm-go/install_dev_test.go:320`）—— fixture 為
`{git: acme/foo, ref: stable}`，斷言搬到 devDependencies 後 `ref: stable` 仍在。

**Mutation 驗證**：把 `switch` 的搬家分支改成 `case false:`（等同還原成呼叫端
只拿 bool 的舊行為），重跑該測試 → 紅（`ref: stable` 消失，`ParsedDevDeps[0].Reference`
變空字串）。復原後重跑 → 綠。

### 問題 3 — `--frozen --dev X` 印出「已搬家」卻從未真正持久化

**根因**：`moveDependencyBetweenSections` 的 `ux.Info("...moved...")`
是 step 1b（`install.go` 前段，位於任何 frozen 判斷之前）的純記憶體操作副作用；
frozen 模式在更後面的「3. Frozen install」分支直接 `return nil`
（`install.go:705` 附近），**永遠不會走到** `persistPackagesToManifest`
（`install.go:1850` 附近）。結果：`--frozen --dev X`（X 已宣告在 `dependencies`）
會印出「moved from dependencies.apm to devDependencies.apm (--dev)」，
但 apm.yml 從未被寫入 —— 訊息形狀是成功，實際是 no-op。

**修復**：仿照既有的 `--frozen --skill` 組合防呆（`install.go:235-242`），
在同一位置加上對稱的 `--frozen --dev` / `--dev` 缺 positional package 兩種防呆，
提早以明確錯誤拒絕，不讓使用者看到誤導性的「已搬家」訊息。

**回歸測試**：
- `TestRunInstall_DevWithFrozen_Errors`（`install_dev_test.go:449`）
- `TestRunInstall_DevWithoutPackages_Errors`（`install_dev_test.go:469`）

**Mutation 驗證**：把新增的 `if deps.dev { ... }` guard 改成
`if false && deps.dev { ... }`，兩個測試都轉紅（分別變成
`validate apm.lock.yaml: lockfile: missing lockfile_version`〔繼續往下執行到
frozen 分支〕與 `<nil>`〔繼續往下執行到 "No dependencies to install"〕）。
復原後重跑 → 綠。

### 這一輪也順手修的關聯項

`moveDependencyBetweenSections` 與 `deployAndFinalize` 的no-op 檢查
（`existingLock != nil && lockfile.IsSemanticEqual(...)`）也一併補了
`manifestSectionMoved` 旗標：搬家只改變 apm.yml 的區段歸屬，不一定改變
lockfile 的可比較欄位（例如搬出 dev 時 `PackageType` 算出來是 `""`，
若既有 lock 是**舊版**、從未寫過 `package_type` 欄位，其值也剛好是 `""` ——
兩者巧合相等），單靠 `IsSemanticEqual` 會誤判為「無變化」而連 `apm.yml`
的搬家寫入都一起跳過（`persistPackagesToManifest` 只在 no-op 檢查之後才會被呼叫）。

**回歸測試**：`TestRunInstall_Dev_MoveOutOfDevWithLegacyLock_StillPersistsManifestMove`
（`install_dev_test.go:372`）—— 先用 `--dev` 跑一次取得真實 lockfile，手動剝除
`package_type: marketplace_plugin` 那一行模擬舊版 lock，再跑一次不帶 `--dev`
觸發搬家，斷言 apm.yml 的搬家確實持久化（而非印出訊息後被 no-op 檢查吃掉）。

**Mutation 驗證**：把 no-op 檢查的 `&& !manifestSectionMoved` 拿掉，重跑該測試 →
紅（印出 `Already up to date`，`m.ParsedDeps`/`m.ParsedDevDeps` 都還是搬家前的狀態）。
復原後重跑 → 綠。

### 範圍界定（本輪刻意不動的項目）

同一次 codex 對抗性稽核另外指出的項目，**不在本 task 檔案範圍內**（分屬其他
sibling task 的 `verify.ps1`，或屬於既有程式碼註解的 claim-evidence 覆核，
非本 task 的程式行為缺陷），未在本輪處理：
`plugin-init/verify.ps1` 的 absence-only AC 未驗 exit code、
`targets-init-shape/verify.ps1` 與 `agent-schema-spec/verify.ps1` 的 `Exec`
呼叫模式、`internal/ux/testhooks.go` 的既有註解覆核。這些若要處理，
應在對應 task 的 `verify.ps1`/context 內進行，不應由 install-dev 的
implement 迭代跨界修改。

### 全套件驗證（本輪修復後）

```
go build ./...  → exit 0
go vet ./...    → exit 0
go test ./... -count=1              → 全綠（23 個套件）
go test ./... -coverprofile         → total 86.9%（≥ 80% 門檻）
pwsh verify.ps1（含新增的 AC42-followup 四項）→ TIER 1 GREEN
git diff -- go.mod / git diff --cached -- go.mod → 皆空（無新相依）
```

**未執行外部（codex CLI）二次覆核** —— 本輪由主 session 直接依 codex
（透過稽核者提供的 node_repl fallback 產出的根因分析）指出的兩個具體缺陷
逐一定位、修復、mutation 驗證，未另外再跑一輪獨立 subagent 覆核。
若需要更高信賴度，建議下一輪由 fresh-context subagent 對本次 diff
（`git diff cmd/apm-go/install.go cmd/apm-go/update.go cmd/apm-go/install_dev_test.go`）
再做一次反向稽核。**`task.py finish` 未執行，狀態仍為待驗證。**
