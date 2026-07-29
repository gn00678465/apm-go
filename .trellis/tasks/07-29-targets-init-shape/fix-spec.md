# 修復規格 — codex Tier 2 殘留發現

> 撰寫於 2026-07-30，工具執行被安全分類器阻斷期間。
> **本檔是 `trellis-implement` 的派工載荷，不是完成紀錄。**
> 每一條的「閘門」欄必須先寫進 `verify.ps1` 並經 mutation 證明會紅，才輪到修程式碼。

## 執行順序（不可對調）

```
1. 把下列 B2/B3/B4/B5 的閘門寫進 verify.ps1   ← trellis-implement
2. 主 session 對每條新閘門做 mutation 測試      ← 改壞被測那一行，閘門必須紅，然後還原
3. 修程式碼                                    ← trellis-implement
4. 主 session 獨立重跑 verify.ps1
5. codex exec -s read-only 對抗稽核
6. 回寫 prd.md 的 AC 勾選 + verification-record.md
```

**步驟 2 沒做 = 該條閘門不存在。** 上一輪 `verify.ps1` 假綠就是因為沒有任何一條做過 mutation。

---

## B2 — AC25 抓不到 `SupportedTargets` 單獨縮短

**現況（我逐行讀過）**

- `internal/manifest/target.go:38` — `SupportedTargets = deployTargets`
- `cmd/apm-go/init.go:285-286` — `targetSelectOptions` 迭代 `manifest.SupportedTargets`
- `cmd/apm-go/manifestnode_test.go:101` 迭代 `SupportedTargets` 查 `HasAdapter`
- `cmd/apm-go/manifestnode_test.go:107-111` 比 `len(opts)` 與 `len(SupportedTargets)`

**反例**：把 `target.go:38` 改成 `SupportedTargets = deployTargets[:5]`。
測試的兩端（`opts` 與 `SupportedTargets`）都源自同一個縮短後的切片 →
`TestSupportedTargetsSet_MatchesAdapterTargetsAndPromptMenu` **仍綠**。
`internal/manifest` 的 `TestAdapterTargetsSet_MatchesDeployTargets` 比的是
`adapterTargets` vs `deployTargets`（都還是 6）→ 也綠。

**同時要修的句型違規**：`target.go:28-31` 的註解寫
「are all derived from this one slice so they cannot drift apart」——
這是**不存在句**（claim-evidence-guide 第五型），且已被上述反例證偽。
不得保留原文；改為描述實際保證範圍。

**修法**：測試必須拿 `deployTargets`（來源）與 `SupportedTargets`（衍生）**雙向**比對，
不能只比兩個衍生物。`deployTargets` 未匯出，需在 `internal/manifest` 加一個
匯出的測試用讀取器，或把這條斷言移進 `internal/manifest` 的測試。

**閘門**：`verify.ps1` 新增一條 —— 暫時把 `SupportedTargets` 改為 `deployTargets[:5]`
不是閘門能做的事（閘門不改原始碼）。改為斷言：測試檔中同時出現對
`deployTargets` 與 `SupportedTargets` 的引用。**這條閘門較弱，主要保證靠 mutation 測試**。

---

## B3 — `verify.ps1` AC6 非逐字

**現況**：`verify.ps1:130-134` 對 `$head` 三行用 `-notmatch` 子字串比對。
行內多出任何字元都會通過。

**注意（更正先前摘要的說法）**：
- `$lines[($idx-4)..($idx-2)]` 是**位置精確**的三行，不是「最後三行」——這部分沒問題
- AC7 的測試 `cmd/apm-go/manifestnode_test.go:80-87` 用連續五行 `strings.Contains`，
  **那條是逐字的**，不需要修

所以 B3 的範圍只有 `verify.ps1` 的 AC6，比 codex 報告的敘述小。

**修法**：三行改為與字面量陣列逐字 `-ceq` 比對。

**閘門的 mutation 測試**：在 `targetsCommentLines()` 的任一行尾加一個空格，
`verify.ps1` 必須紅。

---

## B4 — AC29 只測 install，未測 pack

**現況**

- parent `prd.md` 的 AC29 要求 install **與 pack** 兩條鏈
- `cmd/apm-go/targets_shape_test.go:31-60` 只跑 `runInstall`
- `cmd/apm-go/pack.go:185-189` 有自己的 `SafeLoad → ParseManifest` 進入點
  （`loadPackManifest`），與 install 不共用

**修法**：新增 `TestPack_LegacySingularTargetKey_StillResolves`，
用只有 `target:` 單數鍵的 apm.yml 走 pack 路徑。

**閘門**：`verify.ps1` 的 `$acTests` 陣列加這個測試名（先 `-list` 證明非零匹配再 `-run`）。

---

## B5 — `readExistingTargets` 因不相關錯誤丟失合法 targets

**現況**：`cmd/apm-go/init.go:314-328`

```go
m, _, err := manifest.ParseManifest(node)
if err != nil {
    return nil
}
return m.Target
```

**實測反例**：`name: p\ntarget: claude\n`（缺 `version:`）→ `ParseManifest` 失敗 → 回 `[]`。
使用者仍在跑互動式 init，卻整組失去既有 targets 的預選。
改走 ParseManifest 之前這個檔案是能讀出 `claude` 的 —— **這是本 task 引入的行為回歸**。

**設計決定（主 session 提案，已於 2026-07-30 向使用者說明後獲授權執行）**：

1. 先試 `ParseManifest` —— 成功就用 `m.Target`，保住 CSV 糖
   （`target: claude,copilot`）與 alias 正規化（`vscode` → `copilot`）
2. `ParseManifest` 失敗時，**退回寬鬆讀取**：直接讀 `targets` / `target` 原始鍵，
   走 `manifest.ValidateTarget` 逐項正規化，讀不懂的項目丟棄而非整組放棄
3. 兩鍵同時存在時**不猜測**，回 nil —— 與 `manifest.go:96` 的
   「must not define both」一致，不得自創「複數優先」

**這會改變使用者可見行為**（壞掉的 apm.yml 現在會有預選），是刻意的：
回復本 task 之前的寬容度，同時保留新增的 CSV/alias 能力。

**閘門**：新增測試 `TestReadExistingTargets_LenientOnUnrelatedParseError`，
用 `name: p\ntarget: claude\n` 斷言回傳 `["claude"]`。
加進 `verify.ps1` 的 `$acTests`。

**mutation 測試**：把退回分支刪掉，閘門必須紅。

---

## Majors（同一輪一起修）

| # | 位置 | 問題 | 修法 |
|---|---|---|---|
| M1 | `cmd/apm-go/manifestnode.go:85-87` | plugin 分支（`spec.Plugin=true`）零測試 | 補 `devDependencies` 鍵序與內容的測試 |
| M2 | `cmd/apm-go/targets_shape_test.go:14-23` | AC4 只測 `errNoDeployTarget()` 字串，未測 CLI | 已有 binary 端對端驗證於 verification-record，補成自動化測試 |
| M3 | `internal/ux/testhooks.go:19-41` | package-level 可變狀態，CI 跑 `-race` 有風險 | 評估：ux 測試是否併發。若否，在註解記錄此前提；若是，需加鎖 |
| M4 | `internal/ux/testhooks_test.go:55-68` | 第一個測試的 `restore` 未用 `t.Cleanup`，`t.Fatalf` 路徑會外洩 stub | 改為 `t.Cleanup(restore)` 並在末尾顯式呼叫驗證 |
| M5 | `cmd/apm-go/manifestnode.go:79` | 無 targets 時註解掛在 `dependencies` 節點，位置脆弱 | 已有註解說明（`:68-78`）。補一條測試鎖住 author→dependencies 相鄰 |
| M6 | `cmd/apm-go/manifestnode.go:94-96` | name/description 含冒號、引號、換行時無測試 | 補特殊字元 round-trip 測試（產生後必須能被 `ParseManifest` 讀回） |

---

## 完成條件

- `verify.ps1` 全綠，且**每一條新閘門都經過 mutation 測試**（改壞→紅→還原）
- codex `exec -s read-only` 對抗稽核無阻斷級
- `prd.md` 的 16 條 AC 逐條勾選，每條附可執行證據
- **不跑 `task.py finish`** —— 依使用者指示由使用者驗證後決定
