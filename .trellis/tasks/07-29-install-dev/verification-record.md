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
