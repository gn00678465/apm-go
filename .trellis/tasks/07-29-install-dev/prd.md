# install --dev 與 devDependencies 寫入

> Child of `07-28-marketplace-plugin-parity`。
> 研究依據與技術設計見 parent 的 `research/`、`design.md` §11。

## Goal

補上 `apm-go install --dev`，讓 plugin 作者流程
（`plugin init` → `install --dev` → `pack`）能真的走完。

## 這個 task 的由來（重要，不要重蹈覆轍）

本項原本被裁定為「成本大、貫穿 install/lock/update/uninstall，另開 task」
（parent 原 D5）。**該估計是錯的**，被 codex 對抗性稽核推翻。

實際讀過的證據 —— dev 相依的**讀取端已經全通**：

| 子系統 | 既有的 dev 支援 |
|---|---|
| install | `cmd/apm-go/install.go:433` 正規化 `ParsedDevDeps`、`:457` `hasAnyDeps` 含 dev、`:1030-1035` 解析時合併 |
| update | `cmd/apm-go/update.go:94`、`:303-305` 明載 dev 同 scope |
| uninstall | `cmd/apm-go/uninstall_resolve.go:58` 的 `IsDev`、`:253`/`:273` 掃 dev、`uninstall.go:586` 分流 |
| pack | `cmd/apm-go/pack.go:299-300` 建 `devKeys` |
| compile | `internal/compile/compile.go:76` 合併 dev |
| 測試 | `cmd/apm-go/install_test.go:126` `TestRunInstall_DevDependency_ResolvedDeployedAndLocked` 已證明手寫 dev entry 能解析→部署→入 lockfile |

⇒ **缺的只有寫入端**。成本估計 100–250 LOC / 5–8 檔。
完整經過見 parent `review/codex-audit-checklist.md` 阻斷 1。

## 依賴

- **前置**：無（可與 `07-29-targets-init-shape` 並行）。
- **後續依賴本 task 的**：`07-29-plugin-init` —— 它的 Next Steps 要印
  `apm-go install --dev <owner>/<repo>`，這個指令得先能跑。

## Requirements

對應 parent `prd.md` 的 **R9**（逐字沿用）。

1. `install` 新增 `--dev` 旗標：positional packages 寫入 `devDependencies.apm`。
2. `cmd/apm-go/install.go:2012-2035` 的 `persistPackagesToManifest` 目前把
   `"dependencies"` 寫死（`:2024`），參數化為可指定區段；
   缺 `devDependencies` 鍵時比照 `:2029-2035` 既有邏輯自動建立。
3. **不重做既有的 dev 讀取鏈** —— 已有測試覆蓋，動它只增加回歸面。
4. lockfile `package_type`：`internal/lockfile/write.go:20` 已有此鍵在欄位排序白名單，
   `:494`/`:501` 的排除清單也提到，但全 repo `grep PackageType` 零命中 ——
   宣告了卻沒有對應 Go 欄位，目前永不輸出。需補欄位 + parse/write/equality。

**鍵序約束**：新建的 `devDependencies` 必須落在 `includes` 與 `scripts` 之間，
與 `07-29-targets-init-shape` 的有序產生器是同一個鍵序契約。
若該 task 尚未完成，本 task 仍須自行保證這個位置正確。

## Acceptance Criteria

沿用 parent `prd.md` 的 **AC42–AC45**（AC46 屬 `07-29-plugin-init`）。

> **勾選依據**：`verify.ps1` 2026-07-30 全綠（13 項），關鍵檢查經 mutation 測試證明會紅。
> 逐條證據見 `verification-record.md`。**勾選不等於 task 完成** —— `task.py finish` 未執行。

- [x] AC42 — `install --dev owner/repo` 寫入 `devDependencies.apm`，**不**寫入 `dependencies.apm`
      **2026-07-30 補（Tier 2 阻斷級）**：X 已存在於另一區段時原本會重複寫入兩邊。
      使用者裁定為**搬家**（npm 慣例）。見 AC42-cross 兩個測試。
- [x] AC43 — apm.yml 原無 `devDependencies` 鍵時自動建立，且鍵序在 `includes` 與 `scripts` 之間
- [x] AC44 — **回歸閘門**：不加 `--dev` 時行為與現況完全一致
- [x] AC45 — `--dev` 裝進來的套件在 `apm.lock.yaml` 有 `package_type`；非 dev 既有行為不變

本 task 專屬：

- [x] AC-L1 — R9.3 的守門：**三個**既有 dev 測試在 R9 落地後全部維持綠燈 ——
      `cmd/apm-go/install_test.go:135` `TestRunInstall_DevDependency_ResolvedDeployedAndLocked`、
      `:193` `TestRunInstall_DevDependency_SecondBareInstallIsNoOp`、
      `:2265` `TestRunInstall_DevDependency_SkillSubsetHonored`。
      驗法：`go test ./cmd/apm-go/ -run 'TestRunInstall_DevDependency' -v`，
      **先跑 `go test ./cmd/apm-go/ -list 'TestRunInstall_DevDependency'` 確認匹配到 3 個**。
      （AC44 的字面只覆蓋「不加 `--dev` 的路徑」，沒有要求既有讀取鏈的測試被重驗，
      這是 checklist 重新推導時發現的缺口 G1。）
- [x] AC-L2 — `go build ./...`、`go vet ./...` exit 0；coverprofile total ≥ 80%

- [x] AC-L9 — **parent C5 的本地閘門**：未新增任何第三方相依。
      驗法：`git diff -- go.mod; git diff --cached -- go.mod` —— 都不得出現新 `require` 行。
      （`git status --porcelain` 只給 `M go.mod`，看不出新增了什麼，不足以判定。）

## 驗收紀律（全域，沿用 parent）

- 驗 exit code 一律用預先 build 的 `bin/apm-go.exe`，**禁止 `go run`**
- 以 `go test -run <pattern>` 當閘門前，先 `go test -list <pattern>` 證明匹配非空
- `t.Skip` 不算通過
- 任何判斷句遵守 `.trellis/spec/guides/claim-evidence-guide.md`

## 執行步驟

對應 parent `implement.md` 的 **Step 8b**。逐步 codex 閘門規則適用。

