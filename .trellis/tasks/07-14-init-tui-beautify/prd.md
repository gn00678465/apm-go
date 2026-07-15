# 美化 init 選單與 stdout 輸出（charmbracelet 生態：lipgloss + huh）

> **v2 計畫（改用 lipgloss，棄用 pterm）**。原 pterm 版分支 `feat/init-tui-beautify` 已作廢
> （pterm v0.12.83 的寬度引擎無法渲染「對齊的 box-drawing 表格」，且顏色是全域旗標、
> 有 per-writer 洩漏 footgun）。新分支 `feat/init-tui-lipgloss`。

## Goal

為 `apm-go` 建立統一的終端機視覺主題，重整散落各檔、手刻括號前綴的互動選單與 stdout 輸出。
以 **charmbracelet 單一生態** 承接：`lipgloss`（樣式 / 表格 / 邊框 / 顏色）+ `huh`（互動表單 /
spinner，建於 bubbletea）。

## 為何 lipgloss 而非 pterm

- **表格對齊**：`lipgloss/table` 正確處理欄寬與 CJK 全形（終端排版的黃金標準），能渲染
  對齊的 box-drawing 表格（`RoundedBorder`/`NormalBorder`、可控 header 分隔線與欄分隔）。
  pterm 對多位元組 box-drawing 分隔會算錯寬度導致跑版。
- **per-writer 顏色（原生）**：`lipgloss.Fprint(w, s)` 透過 `colorprofile` **依該 writer 是否為
  TTY 自動 downsample / 去色**。這是函式庫原生行為 —— 徹底取代先前手刻的 `renderForWriter`，
  且**沒有全域旗標 footgun**（pterm 的 per-writer ANSI 洩漏問題不存在）。
- **單一生態**：huh 已建於 bubbletea，`charm.land/lipgloss/v2` 已是直接相依。改用 lipgloss
  可**移除 pterm 相依**，全部收斂到 charm 生態。

## Constraints

- 裝飾性 / 人類可讀輸出走 **os.Stderr**；`normalize` 等機器可讀的 **os.Stdout** 保持乾淨、不經 ux。
- 非互動 / `--yes` / 非 TTY / `NO_COLOR` / CI 自動降階為純文字（lipgloss/colorprofile 原生處理）。
- 只動輸出與互動層，不改業務邏輯；業務邏輯層（`internal/manifest`·`marketplace`·`pack`）
  **禁止 import `internal/ux`**。
- exit code 全部保留（`audit`/`audit --content` 0/1/2、`marketplace validate`/`audit --strict`）。
- 串流保留：逐處把該行原本的 `os.Stdout`/`os.Stderr`/`cmd.OutOrStdout()`/`cmd.ErrOrStderr()` 傳給 ux。

## Requirements

1. 新增 `internal/ux` 門面（**API 與 v1 相容**，只換底層實作），集中主題（色票 / 符號 /
   lipgloss Style / huh Theme）與封裝：
   - 訊息：`Success/Info/Warn/Error(w, format, a...)` — lipgloss Style + `lipgloss.Fprint(w, …)`。
   - 表格：`Table(w, headers, rows)` — `lipgloss/table`（box-drawing、對齊、header 分隔線）。
   - 結構化：`BulletList`、`Tree`、`Section`、`Box`（lipgloss border）、`Diff`。
   - 進度：`Spinner` — huh/spinner（`.Title().Action().Run()`）。
   - 互動：`Confirm/InputText/Password/MultiSelect` — huh（不變）。
2. 統一符號：`✓ success`、`ℹ info`、`!` warn、`✗ error`、`▸ progress`、`•` list（取代
   `[+] [i] [!] [warn] [>] [*] [x] [dry-run] [-]` 混用前綴；`[!]` 依語意分流 warn/error）。
3. 全指令覆蓋（延續 v1 的 26 點盤查）：init 互動、install/marketplace/uninstall/update 主要輸出、
   audit/pack/experimental/marketplace authoring 家族/marketplace package/main validate 子指令報表。
4. 互動偵測統一 `ux.CanPrompt()`（stdin+stderr TTY 且非 CI，排除 NO_COLOR）。
5. 顏色降階：交給 lipgloss `colorprofile`（TrueColor→ANSI256→ANSI16→mono→strip），
   `NO_COLOR`/非 TTY 原生去色，**per-writer**，無全域旗標、無 footgun。

## Acceptance Criteria

- [ ] `internal/ux` 改以 lipgloss + huh 實作，**移除 pterm 相依**（`go mod tidy` 後 go.mod 無 pterm）。
- [ ] `go build ./...`、`go vet ./...` 通過；`go test ./...` 全綠（CI 補 `-race`）。
- [ ] 表格為 lipgloss/table：box-drawing、**對齊**、有 header/body 分隔線、CJK 全形正確。
- [ ] `apm-go list > out.txt`（stdout 非 TTY）→ 檔案無 ANSI；真 TTY → 有色。per-writer 由
      `lipgloss.Fprint` 原生處理，無「全域旗標 + 非終端 writer 洩漏」footgun。
- [ ] init 在 TTY 呈現 huh 表單；建立前確認用 lipgloss Box；`--yes`/非 TTY 純文字不阻塞。
- [ ] stdout 前綴全面統一新符號，cmd production 無殘留舊前綴。
- [ ] 全指令 exit code 不變；`normalize` stdout 位元組不變。
