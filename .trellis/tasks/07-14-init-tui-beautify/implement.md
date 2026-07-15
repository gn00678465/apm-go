# 執行計畫 v2 — lipgloss + huh（分支 feat/init-tui-lipgloss）

> 策略：`internal/ux` 門面 API 與 v1 相容 → **cmd 層的 ux 遷移可直接沿用 v1 成果**
> （前綴統一、串流保留、severity 分流、互動安全、一致性 sweep、About-to-create Box），
> 只需**重寫 internal/ux 的底層實作（pterm → lipgloss/huh）**。

## Phase 1 — internal/ux 以 lipgloss 重寫

1. **依賴**：確認 `charm.land/lipgloss/v2` 為直接相依；改用 `.../v2/table`、`.../huh/v2/spinner`。
   → verify: `go build ./internal/ux/...`（pterm 稍後移除）
2. **colors.go / theme**：色票 + 符號 + lipgloss Style（Success/Info/Warn/Error 各自 Foreground）
   + huh Theme（沿用 v1）。
3. **訊息輸出**：`Success/Info/Warn/Error(w, …)` = `lipgloss` style Render + **`lipgloss.Fprint(w, …)`**
   （per-writer 去色）。確認 `lipgloss.Fprint` 對任意 w 偵測 profile；若否，快取
   `colorprofile.NewWriter(w, os.Environ())`。
4. **表格**：`Table` = `lipgloss/table`（RoundedBorder、BorderColumn、BorderHeader、
   StyleFunc header cyan bold）→ `lipgloss.Fprintln(w, t.String())`。
5. **結構化**：`BulletList` = `lipgloss/list`（Bullet enumerator + 縮排巢狀）、
   `Tree` = `lipgloss/tree`（原生 ├─└─ 連接線）、`Section`（標題樣式）、
   `Box`（lipgloss border + 標題）、`Diff`（+/- 上色）。
6. **Spinner**：`Spinner` = huh/spinner；非 CanPrompt 走靜態行、不阻塞。
7. **互動**：`Confirm/InputText/Password/MultiSelect` = huh（沿用 v1）。
8. **ux.go**：`Init`/`IsRich`/`CanPrompt`/TTY 偵測（`golang.org/x/term`）；著色交給 lipgloss
   per-writer，**不需全域旗標 / renderForWriter / isRichWriter**。保留測試 seam
   `SetTTYSeamsForTest`（供 cmd 測試）。
   → verify: `go build ./internal/ux/...`、`go test ./internal/ux/...`（單元測試 + 渲染證據）

## Phase 2 — 套用到 cmd（沿用 v1 façade 呼叫）

9. 從作廢分支取回 cmd 層 ux 遷移：
   `git checkout feat/init-tui-beautify -- cmd/apm-go`（這些檔只呼叫 ux.*，與底層無關）。
   逐一 reconcile：確認所有 ux.* 呼叫的簽章與新 internal/ux 一致（含 `ux.Box`、
   `ux.CanPrompt`、`SetTTYSeamsForTest`）。
   **init metadata 例外**：舊分支呼叫 4 次 `ux.InputText`（逐欄、前一欄消失、不可回退）→
   改用 `ux.InputForm`（單一群組表單，4 欄同時可見、Tab/Shift+Tab 回退修改）。
   → verify: `go build ./...`、`go run ./cmd/apm-go init` 手動確認 4 欄同畫面可回退

## Phase 3 — 移除 pterm + 全量驗證

10. **移除 pterm**：確認無任何檔案 import pterm → `go mod tidy` → go.mod 無 pterm。
    → verify: `grep -rn "pterm" --include=*.go .` 應為空；`grep pterm go.mod` 為空
11. **全量驗證**：
    - `go build ./...`、`go vet ./...`、`go test ./... -count=1` 全綠（-race 待 CI）。
    - **表格渲染實測**（含 CJK + 換行）：`apm-go experimental list` / `marketplace browse`
      對齊、box-drawing、有 header 分隔線。
    - **per-writer 顏色實測**：`apm-go … > out.txt` → 檔案無 ANSI；真 TTY → 有色。
    - exit code 不變（audit/validate --strict）；`normalize` stdout 位元組不變。
- **【review gate】** codex `exec` 對抗審核（`git diff main...HEAD`，stdin 餵法）→ 修 CRITICAL/HIGH → commit

## 硬性規則
- 只動輸出/互動層；業務邏輯層不 import ux；exit code / stdout 機器輸出不變。
- 非互動 / CI / 重導向純文字、不阻塞。

## Rollback
- 全部集中 internal/ux + go.mod；新分支隔離。舊 pterm 分支已作廢（PR #2 已關）保留可參考。
