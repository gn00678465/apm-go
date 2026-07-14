# 技術設計 — init/stdout 美化（huh + pterm）

## 決策（已與使用者確認）

- **A 案**：pterm 主導輸出，huh 主導互動。兩相依。
- browse box table **遷移** 到 `pterm.Table`（放棄 Python rich 視覺 parity）。

## 依賴

- `charm.land/huh/v2`（連帶 bubbletea/v2、lipgloss/v2、bubbles/v2）— 互動表單。
- `github.com/pterm/pterm` — 輸出樣式與 Table/Spinner/BulletList/Section。
- 移除：`golang.org/x/term`（若 MCP 密碼輸入改用 huh Password 後不再被引用，屬本次變更造成的孤兒，才移除）。

## 邊界與契約

新增 `internal/ux`，為唯一的輸出/互動門面。對外契約（初版）：

```
package ux

// 一次性初始化：偵測 TTY / NO_COLOR / CI，設定 pterm 關色與 huh accessible。
func Init()

// 前綴輸出 —— 每個函式明確接受 io.Writer，由呼叫端傳入「該行原本的串流」。
// 絕不預設 blanket 走 stderr（見下方「串流契約」修正）。
func Success(w io.Writer, format string, a ...any)
func Info(w io.Writer, format string, a ...any)
func Warn(w io.Writer, format string, a ...any)
func Error(w io.Writer, format string, a ...any)

// 結構化輸出（同樣接受 writer）
func Table(w io.Writer, headers []string, rows [][]string)
func BulletList(w io.Writer, items []Item)
func Section(w io.Writer, title string)
func Spinner(w io.Writer, text string) *Spin   // Spin.Success()/Fail()/Update()

// 互動（一律走 stderr + stdin；非 TTY 時回傳預設值 + ok=false，由呼叫端決定 fallback）
func Confirm(prompt string, def bool) (bool, error)
func InputText(label, def string) (string, error)
func Password(label string) (string, error)
func MultiSelect(title string, opts []Option) ([]string, error)
```

### 串流契約（advisor 修正 — 重要）

**不可** 讓前綴/結構化函式 blanket 預設走 stderr。現況中許多指令的「最終結果行」
本來就在 **stdout**（`cmd.OutOrStdout()` 或裸 `fmt.Println`）：`marketplace list/validate/check/
outdated/migrate/package`、`pack`、`install` 摘要（install.go:1153）、`uninstall` 摘要
（uninstall.go:607-611）。機械式替換若把這些搬到 stderr，會破壞任何用 stdout 捕捉輸出的腳本。

規則：**逐處保留該行原本的串流** —— 替換時把原本 `os.Stdout` / `cmd.OutOrStdout()` /
`os.Stderr` / `cmd.ErrOrStderr()` 原樣當作 `w` 傳給 `ux.*`。互動元件（Confirm/Input/…）
固定走 stderr+stdin（不影響機器可讀 stdout）。驗收不只保護 `normalize`，而是所有指令的
stdout 位元組在美化後對「結果內容」不變（僅樣式碼在 TTY 下附加、非 TTY 下無）。

### 色票 / 符號（單一來源）

| 語意 | 色碼 | 符號 | huh(lipgloss) | pterm |
|---|---|---|---|---|
| brand | #2dd4bf | ▸ | 主色 | FgCyan/RGB |
| heading | #8aa0ff | — | 標題 | FgLightBlue |
| success | #3fb950 | ✓ | — | FgGreen |
| warning | #d29922 | ! | — | FgYellow |
| error | #f85149 | ✗ | — | FgRed |
| muted | #8b949e | • | dim | FgGray |

- huh：`huh.ThemeFunc(func(isDark bool) *huh.Styles { s := huh.ThemeBase(isDark); …套色…; return s })`。
- pterm：自訂 `PrefixPrinter`（`pterm.Success/Info/Warning/Error` 覆寫 Prefix + Style），
  或在 `internal/ux` 內建對應 printer 實例。

## 資料流

```
cmd/apm-go/*.go
      │  呼叫
      ▼
internal/ux  ──(TTY?)──► huh 表單 / pterm 動畫
      │                └(非TTY/NO_COLOR/CI)► 純文字到 stderr
      ▼
   os.Stderr（裝飾）        os.Stdout（normalize 等機器輸出，不經 ux）
```

## TTY / 降階

- `ux.Init` 內判斷：`isInteractive()`（沿用 init.go 既有邏輯）+ `os.Getenv("NO_COLOR")` + `pterm.RunsInCi()`。
- 非互動：pterm `DisableStyling()`；huh 走 `WithAccessible(true)` 或直接回傳預設值。
- 現有 `isInteractive()`（init.go:190）抽到 `internal/ux` 或保留、由 ux 呼叫。

## 相容性 / 風險

- huh v2 vanity path 需 `go get charm.land/huh/v2@latest`；go.mod 依賴樹變大，需 `go mod tidy`。
- 互動 e2e 測試多以 `--yes` 繞過，理論上不受影響；仍需跑 `go test ./...` 驗證。
- browse table 遷移後與 Python 版視覺不同：需更新對應的 golden / 快照測試（若有）。
- huh 表單需 stdin 為真 TTY；CI 下務必走非互動分支，否則會 hang → `ux.Init` 必須正確偵測。

## Rollback

- 全部變更集中在 `internal/ux` + 各 cmd 檔的輸出行；以 git 分支 `feat/init-tui-beautify` 隔離。
- 若 huh 依賴或 CI hang 出問題，退回 B 案（pterm-only）僅需替換 `internal/ux` 互動實作，門面不變。
