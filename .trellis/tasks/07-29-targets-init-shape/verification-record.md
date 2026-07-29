# 驗證紀錄 — 07-29-targets-init-shape

> **狀態：待使用者驗證。任務未標記完成。**
> 依使用者指示（2026-07-29）：「任務結束不得翻完成，使用者會介入驗證」。
> `task.py finish` **未執行**，task 狀態維持 `in_progress`。

- 實作者：`trellis-implement` 子代理（主 session 只做 dispatch 與驗證）
- 驗證者：主 session（獨立重跑，不採信子代理回報）
- 日期：2026-07-29

---

## Tier 1 — 確定性閘門（主 session 獨立重跑）

```
pwsh .trellis/tasks/07-29-targets-init-shape/verify.ps1
→ TIER 1 GREEN, EXIT=0
  17 項全綠，覆蓋率 total 86.6%（基準線 86.4%，未退步）
```

## checklist 逐條驗證（parent `checklist.md` 對應列）

| AC | 驗法 | 證據 | 結果 |
|---|---|---|---|
| AC1 | 實跑 `bin\apm-go.exe init d1 --yes --target claude,codex,opencode`，看 apm.yml | 產物含 `targets:`（複數），無 `target:` | ✅ |
| AC2/AC3 | `go test ./cmd/apm-go/ -list TestReadExistingTargets_BothKeys` 先證明非零匹配，再 `-run` | 6 個子測試全綠：plural sequence / plural scalar / singular / both-keys / **CSV sugar** / **alias 正規化** | ✅ |
| AC4 | **binary**（非 `go run`）跑 `install` 於本地 primitive fixture | 輸出 `2. Add a targets: field to apm.yml, e.g.:` + `targets:` / `- claude`；無單數殘留；exit=2 未變 | ✅ |
| AC5 | 同 AC1 產物的鍵序 | `name, version, description, author, targets, dependencies, includes, scripts` | ✅ |
| AC6 | 同上，`targets:` 上方三行 | 逐字符合；Accepted values = `agent-skills, antigravity, claude, codex, copilot, opencode`（6 個，依 D3） | ✅ |
| AC7 | `init d2 --yes --target ""` | 三行說明註解 + `# targets:` + `#   - claude`，無 active key | ✅ |
| AC24 | `init --target agent-skills` | 成功（先前被拒） | ✅ |
| AC25 | `-list` 證明非零匹配後 `-run` | `TestSupportedTargetsSet_MatchesAdapterTargetsAndPromptMenu` PASS；另有 `internal/manifest` 的 `TestAdapterTargetsSet_MatchesDeployTargets` 補雙向斷言 | ✅ |
| AC26 | 行為測試（替換來源切片） | `TestTargetsCommentLines_DerivedFromInput` PASS | ✅ |
| AC27 | `internal/manifest` | `TestCanonicalTargets_UnchangedAndCursorStillParses` PASS | ✅ |
| AC29 | 端對端 install | `TestInstall_LegacySingularTargetKey_StillDeploys` PASS | ✅ |
| AC-L0 | 原始碼 + 消費端使用 | `interactive.go` 三個 func 已為 `var`；`init_interactive_seam_test.go:58` 實際驅動 `SetPromptSeamsForTest` | ⚠️ 見下 |
| AC-L1 | build / vet / coverprofile total | 皆綠，86.6% | ✅ |
| AC-L9 | `git diff -- go.mod` + `--cached` | 兩者皆無新 `require` 行 | ✅ |

---

## 重做修掉了我先前 inline 版本的兩個真缺陷

主 session 先前（違規）自己寫的版本有兩個 bug，`trellis-implement` + 它自己跑的 codex 稽核抓到並修正：

1. **CSV sugar 與 alias 正規化遺失**
   我手寫了一個 type switch 直接讀 `doc["target"]`，繞過了 `manifest.ParseManifest`。
   結果 `target: claude,copilot`（CSV 糖）與 `target: vscode`（alias → copilot）
   兩種合法但非正規形式都會**靜默失去 MultiSelect 預選**。
   修法：改走 `SafeLoad → ParseManifest → m.Target`，與 install/pack 同一條管線，
   消除第二個 parser。

2. **「複數優先」是我自己編的規則，與 parser 相矛盾**
   我讓兩個鍵同時存在時「複數優先」。實測 `internal/manifest/manifest.go:96`：
   ```
   apm.yml must not define both 'target:' and 'targets:'; use only one
   ```
   parser 直接拒絕。子代理的版本走 ParseManifest，繼承此行為（不猜測預選），
   與 `validate` 一致。

⇒ 這兩點是「重做」而非「保留原稿」的實質收益，不只是流程合規。

---

## ⚠️ 我發現、尚未處理的一個缺口

**`internal/ux` 內沒有 `SetPromptSeamsForTest` 的自身測試。**

- 證據：`go test ./internal/ux/ -list 'Seam|Prompt'` 只回既有的 Clack 測試，無新增。
  `grep -rn "SetPromptSeamsForTest" --include=*_test.go` 只命中 `cmd/apm-go` 的消費端。
- 影響：seam 從消費端被實際驅動（AC2/AC3 有效），但**沒有測試證明 `restore()` 真的還原**。
  一個外洩的 stub 會靜默解除後續所有互動測試的武裝。
- `internal/ux` 覆蓋率 **88.5%**，仍高於 spec 要求的 80%，但低於
  `terminal-ux-contract.md` §6 記載的原始 89.9%。
- **未驗證**：這個下降是否全部來自新增的 `SetPromptSeamsForTest`（54 行未被直接測試），
  還是也有其他因素。
- 成本估計：補一個 `internal/ux` 的 seam 測試約 40 行（替換 → 斷言分流 → restore → 斷言還原）。

**這一項我沒有自行裁定為可接受**，列在此處交由使用者判斷。

---

## Tier 2 — 獨立稽核

`trellis-implement` 回報它自行跑了兩輪 codex 對抗性稽核：
- 第 1 輪：1 blocking（上述 CSV sugar 缺陷）、1 medium、2 low
- 第 2 輪：blocking 0、high 0、medium 0、low 2（1 已修，1 為既有全 repo 風格）

**主 session 未獨立複跑 Tier 2** —— 上述回報來自子代理自述，我只複驗了它宣稱修掉的
兩個缺陷（見上節，兩者皆實測屬實）。若要完整的 Tier 2 保證，需由主 session 再跑一次
`codex exec -s read-only` 對本 child 的 diff 做反向稽核。

---

## 待使用者裁定

1. `internal/ux` 的 seam 自身測試要不要補（見上方 ⚠️）
2. 是否需要主 session 獨立重跑 Tier 2 codex 稽核
3. 驗證通過後才由使用者決定何時 `task.py finish`
