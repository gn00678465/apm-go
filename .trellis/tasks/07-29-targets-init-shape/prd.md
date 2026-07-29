# targets 單複數與 init 產物形狀對齊

> Child of `07-28-marketplace-plugin-parity`。
> 研究依據、技術設計、Out of Scope 一律以 parent 的
> `research/`、`design.md`、`prd.md` 為準，本檔不重複。

## Goal

把 apm-go 的 targets 處理與 apm.yml 產物形狀對齊上游 v0.26.0，
並收斂本專案內部三個各自宣告的 target 集合。

## 依賴

- **前置**：無。（`install-dev`、`marketplace-add-fixes`、`agent-schema-spec`
  同樣沒有前置，四者可並行；本 task 不是「唯一」沒有前置的。）
- **後續依賴本 task 的**：`07-29-plugin-init` —— 它需要本 task 的
  (a) 有序 YAML 產生器、(b) 統一後的 `SupportedTargets`、**(c) ux 測試 seam**。

### ⚠️ ux seam 歸屬（2026-07-29 修正循環依賴）

parent `implement.md` 的 **Step 3（ux 測試 seam）屬於本 task**，不屬於 `plugin-init`。

**修正前是一個循環**：AC2/AC3 斷言 MultiSelect 的**預選狀態**，
而預選狀態只存在於傳給 `multiSelectWith` 的 opts 裡
（parent `design.md:268` 自己寫明「沒有這個 seam 就無法斷言」）；
但 Step 3 原本被指派給 `plugin-init`，而 `plugin-init` 又宣告依賴本 task
→ 本 task 需要 plugin-init，plugin-init 需要本 task。

由 codex 第二輪稽核抓到（`review/codex-audit-round2.md` 阻斷 3）。

## Requirements

對應 parent `prd.md` 的 **R1、R2、R8**（逐字沿用，不在此改寫）。摘要：

- **R1** targets 單複數全面對齊複數：`readExistingTargets` 收雙鍵、
  `buildManifestData` 寫複數、`install.go:829-832` 教學文字改複數、
  `internal/manifest` 的雙鍵解析不得改動。
- **R2** init 產物形狀：語意鍵序、`targets:` 上方三行註解、
  註解清單須與 `SupportedTargets` 同源、無 targets 時輸出註解骨架。
- **R8** 三個 target 集合單一來源：`SupportedTargets` 補 `agent-skills`、
  刪除 `promptTargetsOrdered` 改推導、四者同源、`CanonicalTargets` 不得更動。

## Acceptance Criteria

沿用 parent `prd.md` 的 **AC1–AC7、AC24–AC27**。
各條完整文字（含「驗收紀律」段落）以 parent 為準；此處只列編號與一句話。

> **勾選依據**：`verify.ps1` 2026-07-30 全綠（32 項），且每一條的閘門檢查都經
> mutation 測試證明會紅。逐條證據見 `verification-record.md`。
> **勾選不等於 task 完成** —— 依使用者指示，`task.py finish` 未執行。

- [x] AC1 — `init --yes` 產出使用複數 `targets:`
- [x] AC2 — 單數 `target:` 既有檔案的 MultiSelect 預選不遺失
- [x] AC3 — 複數 `targets:` 同上
- [x] AC4 — install 的 no-deploy-target 錯誤輸出印複數
- [x] AC5 — apm.yml 語意鍵序
- [x] AC6 — 三行註解，第三行為六個 target
- [x] AC7 — 無 target 時**逐字**五行註解骨架（不可只驗開頭）
- [x] AC24 — `init --target agent-skills` 成功
- [x] AC25 — 三集合同源的鎖定測試（測試位於 `cmd/apm-go`，**不是** `internal/manifest`）
- [x] AC26 — 註解清單為**行為測試**（替換來源切片觀察輸出），不可只 grep
- [x] AC27 — `CanonicalTargets` 未動，`targets: [cursor]` 仍解析成功並產生 req-tg-004 warning

由 `plugin-init` 移入本 task（codex 稽核阻斷 3：分錯 child）：

- [x] AC29 — 對只有單數 `target:` 的既有 apm.yml，端對端跑 `install` 仍能正確部署。
      **2026-07-30 補**：parent AC29 要求 install **與 pack** 兩條鏈；
      pack 有自己的 `SafeLoad → ParseManifest` 進入點（`pack.go:185-189`），
      原本零覆蓋，已補 `TestPack_LegacySingularTargetKey_StillResolves`。
      **這條對應 R1.4／parent C4**（`internal/manifest` 雙鍵解析不得改壞），
      屬本 task 的 R1，不屬 `plugin-init` 的 R3/R4。

本 task 專屬：

- [x] AC-L0 — **ux 測試 seam 建立**：`internal/ux/interactive.go:84,149,195` 的
      `confirmWith` / `multiSelectWith` / `inputFormWith` 改為 package-level var
      （函式體不動、簽章不動、不新增相依）。這是 AC2/AC3 的前置。
- [x] AC-L3 — **seam 自身在 `internal/ux` 內有測試**，且涵蓋 `restore()` 確實還原。
      驗法：`go test ./internal/ux/ -list 'PromptSeam|SeamRestore'` 先證明非零匹配，再 `-run`；
      另檢查 `internal/ux` 覆蓋率不低於 spec 下限 80%。
      理由：消費端（`cmd/apm-go`）用到 seam **不等於** seam 被測到。沒有 restore 測試時，
      一個外洩的 stub 會靜默解除後續所有互動測試的武裝，而且不會有任何紅燈。
      （由主 session 驗證 `07-29-targets-init-shape` 時發現的缺口，2026-07-29）
- [x] AC-L1 — `go build ./...`、`go vet ./...` exit 0；
      覆蓋率 **total** ≥ 80%。
      驗法（PowerShell）：
      `go test ./... -coverprofile=cover.out; go tool cover -func=cover.out | Select-Object -Last 1`
      （不要用 `tail -1`，本專案主要開發環境是 PowerShell）
- [x] AC-L2 — **parent C5 的本地閘門**：未新增任何第三方相依。
      驗法：`git diff -- go.mod; git diff --cached -- go.mod`
      —— 兩者都不得出現新的 `require` 行。
      （`git status --porcelain` 只給 `M go.mod`，看不出新增了什麼，不足以判定。）

## 驗收紀律（全域，沿用 parent）

- 驗 exit code 一律用預先 build 的 `bin/apm-go.exe`，**禁止 `go run`**
- 以 `go test -run <pattern>` 當閘門前，先 `go test -list <pattern>` 證明匹配非空
- `t.Skip` 不算通過
- 任何判斷句遵守 `.trellis/spec/guides/claim-evidence-guide.md`

## 執行步驟

對應 parent `implement.md` 的 **Step 1（部分）、Step 2、Step 4、Step 5**。
**逐步 codex 閘門規則同樣適用**（見 parent `implement.md` 開頭）。

順序：Step 2（target 集合單一來源）→ Step 4（有序 YAML 產生器）→ Step 5（雙鍵讀取）。
Step 2 必須最先 —— Step 4 的註解清單要從它推導。
