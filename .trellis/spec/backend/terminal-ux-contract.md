# Terminal UX Contract (`internal/ux`)

> 所有面向終端機使用者的輸出與互動，必須經 `internal/ux` 門面。禁止在 `cmd/apm-go`
> 直接呼叫 `pterm`/`huh`，或用裸 `fmt.Print*` 印帶語意前綴的訊息。
> 來源任務：`07-14-init-tui-beautify`（huh + pterm 統一主題）。

---

## 1. Scope / Trigger

觸發 code-spec 深度的原因：這是**跨層共享的輸出/互動契約**（infra 整合 pterm/huh、
TTY/串流邊界行為）。任何新增指令輸出、互動 prompt、或報表都受此契約約束。

適用範圍：`cmd/apm-go/*.go`（呈現層）。**業務邏輯層**（`internal/manifest`、
`internal/marketplace`、`internal/pack` …）**禁止 import `internal/ux`**（見 Design Decision D3）。

---

## 2. Signatures

```go
package ux

// 一次性初始化（main() 第一行呼叫一次）。之後不得再變動 pterm 全域樣式旗標。
func Init()
func IsRich() bool     // 「是否上色」的整體判定（含 NO_COLOR/CI）
func CanPrompt() bool  // 「能否在 stdin 阻塞式互動」（stdin+stderr 皆 TTY 且非 CI；不含 NO_COLOR）

// 前綴輸出 —— 每個都接受目標 writer；非終端 writer 自動去色（純文字）。
func Success(w io.Writer, format string, a ...any)  // ✓ 綠
func Info(w io.Writer, format string, a ...any)     // ℹ 品牌青
func Warn(w io.Writer, format string, a ...any)     // ! 琥珀
func Error(w io.Writer, format string, a ...any)    // ✗ 紅

// 結構化輸出 —— 同樣接受 writer，非終端去色。
type Item struct { Level int; Text string }          // BulletList 縮排項
type TreeNode struct { Text string; Children []TreeNode }
func Table(w io.Writer, headers []string, rows [][]string)  // boxed；headers 可 nil
func BulletList(w io.Writer, items []Item)
func Tree(w io.Writer, root TreeNode)
func Section(w io.Writer, title string)
func Diff(w io.Writer, diffText string)              // unified diff +/- 上色

// 進度
type Spin struct { /* ... */ }
func Spinner(w io.Writer, text string) *Spin
func (s *Spin) Update(text string)
func (s *Spin) Success(msg string)
func (s *Spin) Fail(msg string)

// 互動（固定走 stderr + stdin）；非 CanPrompt 時回傳預設值、不阻塞。
type Option struct { Label, Value string; Selected bool }
func Confirm(prompt string, def bool) (bool, error)
func InputText(label, def string) (string, error)
func Password(label string) (string, error)          // 遮蔽輸入
func MultiSelect(title string, opts []Option) ([]string, error)

// 測試 seam：強制 stdin/stderr TTY 狀態。
func SetTTYSeamsForTest(stdinTTY, stderrTTY bool) (restore func())
```

單一色票/符號來源（`internal/ux/colors.go`）：
`ColorBrand #2dd4bf`、`ColorHeading #8aa0ff`、`ColorSuccess #3fb950`、`ColorWarning #d29922`、
`ColorError #f85149`、`ColorMuted #8b949e`；`SymbolSuccess ✓`、`SymbolInfo ℹ`、`SymbolWarn !`、
`SymbolError ✗`、`SymbolProgress ▸`、`SymbolList •`。

---

## 3. Contracts

### Writer 契約（串流保留）
- 每個輸出函式的第一參數 `w` 必須是**該行原本的串流**：把現況的 `os.Stdout` /
  `cmd.OutOrStdout()` / `os.Stderr` / `cmd.ErrOrStderr()` 原樣傳入。
- **禁止**把原本在 stdout 的「結果行」改到 stderr（會破壞用 stdout 捕捉輸出的腳本）。
- `normalize` 等機器可讀輸出用 `os.Stdout.Write(bytes)`，**不經 ux**，位元組不得改變。
- 互動函式（Confirm/InputText/Password/MultiSelect）固定 stderr + stdin，不污染 stdout。

### 環境鍵
| 鍵 | 效果 |
|---|---|
| `NO_COLOR`（非空） | 關閉著色（純文字）；**不**關閉互動 |
| `CI`（非空） | 關閉著色**且**關閉互動（走非互動預設值） |

### 全域樣式旗標
- `Init()` 依整體 TTY 狀態把 pterm 全域樣式設定**一次**；之後任何輸出路徑**不得再翻轉**
  `pterm.RawOutput`/`color.Enable`/`PrintColor`（否則與 spinner 背景 goroutine data race）。
- 非終端 writer 的去色由 `pterm.RemoveColorFromString`（render-then-strip）達成，**非**翻轉全域。

---

## 4. Validation & Error Matrix

| 條件 | 著色（per-writer） | 互動（Confirm/Input/…） |
|---|---|---|
| `w` 是 TTY、無 NO_COLOR、非 CI | 上色 | — |
| `w` 非 TTY（重導向/pipe/`/dev/null`/`NUL`/buffer） | 純文字（去色） | — |
| `NO_COLOR` 設定 | 純文字 | **仍可互動**（若 stdin+stderr TTY） |
| `CI` 設定 | 純文字 | 回傳預設值、不阻塞 |
| stdin 或 stderr 非 TTY（`CanPrompt()`=false） | — | 回傳預設值、不阻塞 |

TTY 偵測：**必須用 `golang.org/x/term.IsTerminal(int(f.Fd()))`**，
不可用 `os.ModeCharDevice`（會把 `/dev/null`/`NUL` 誤判為終端）。

診斷/訊息 severity 對應（依「後續控制流」判定，非字面）：
| 情境 | 函式 |
|---|---|
| 成功結果 | `Success` |
| 中性資訊/no-op | `Info` |
| non-fatal 警告 | `Warn` |
| **緊接失敗回傳（如 `errNoDeployTarget`、exit≠0）的直接原因** | `Error`（**不可**降級為 `Warn`） |

exit code 不變：美化不得改動任何指令的退場碼（`audit`/`audit --content` 0/1/2、
`marketplace validate`/`audit --strict`）。

---

## 5. Good / Base / Bad Cases

- **Good**：`ux.Success(cmd.OutOrStdout(), "Installed %d dependencies", n)` —— 保留 stdout、
  TTY 上色、非 TTY 純文字、單複數正確。
- **Base**：`ux.Info(os.Stdout, "Already up to date")` —— 中性訊息，重導向下為純文字 `ℹ Already up to date`。
- **Bad**：`ux.Warn(os.Stderr, "%s", diag)` 但下一行 `return errNoDeployTarget()` ——
  失敗原因被誤標為警告（應 `ux.Error`）。

---

## 6. Tests Required

- `internal/ux`（≥ 80% 覆蓋，現 90.1%）：
  - 非終端 writer（`bytes.Buffer`）即使全域強制 rich，`Success/Info/Warn/Error/Table` 輸出
    **不含 ANSI（`\x1b[`）**（leaked-ANSI 迴歸）。
  - `isTerminalWriter(bytes.Buffer)==false`；`*os.File` 指向 `os.DevNull` == false。
  - `CanPrompt` 對 NO_COLOR/CI/TTY 矩陣正確（NO_COLOR 不關互動）。
  - 非互動時 `Confirm/InputText/MultiSelect` 回傳預設值、不阻塞（timeout 守衛）。
- `cmd/apm-go`：以 `ux.SetTTYSeamsForTest(false,false)` 模擬非 TTY，斷言破壞性操作
  （`marketplace remove`、`init` 覆寫）**要求 `--yes`、不阻塞、不誤刪/誤寫**。
- 退場碼測試（audit/validate --strict）維持綠，**不得放寬斷言**。
- `-race`：CI 必跑 `go test ./... -race`（本機無 gcc 時無法驗證，設計上 Init 後無全域變動 → 無 race）。

---

## 7. Wrong vs Correct

### Wrong
```go
// (a) 用「是否上色」predicate 決定能否互動 —— NO_COLOR 會誤關互動
if ux.IsRich() { ans, _ = ux.Confirm(...) }

// (b) per-call 翻轉 pterm 全域旗標 —— 與 spinner 背景 goroutine data race
pterm.EnableStyling(); defer pterm.DisableStyling(); printer.WithWriter(w).Println(s)

// (c) 用 ModeCharDevice 判定 TTY —— /dev/null 被誤判為互動
interactive := os.Stdin.Stat().Mode()&os.ModeCharDevice != 0

// (d) 把 stdout 結果行改到 stderr
ux.Success(os.Stderr, "Installed %d deps", n)   // 破壞腳本捕捉
```

### Correct
```go
// (a) 互動 gate 用 CanPrompt（stdin+stderr TTY 且非 CI，不含 NO_COLOR）
if ux.CanPrompt() { ans, err := ux.Confirm(...) } else { /* 要求 --yes 或用預設 */ }

// (b) Init 設定一次；per-writer render-then-strip，不碰全域
s := printer.Sprintfln(...); if !isTerminalWriter(w) { s = pterm.RemoveColorFromString(s) }; fmt.Fprint(w, s)

// (c) 用 term.IsTerminal
interactive := term.IsTerminal(int(os.Stdin.Fd()))

// (d) 保留原串流
ux.Success(os.Stdout, "Installed %d deps", n)
```

---

## Design Decisions

### D1 — 策略 A：pterm（輸出）+ huh（互動），兩相依
**Context**：pterm 本身也有互動元件，可單一相依。**Decision**：仍採 huh 做互動表單
（Input/MultiSelect/Confirm/Password 體驗較佳），pterm 做樣式/表格/樹/spinner。
代價：huh v2 帶入 `charm.land/huh/v2` + bubbletea/lipgloss/bubbles v2 依賴樹。

### D2 — 著色 per-writer，Init 後全域不變
**Context**：pterm 樣式是全域狀態，但輸出可能同時寫 stdout 與 stderr（可各自重導向）。
**Decision**：`Init()` 依整體 TTY 設定全域一次；每次輸出依**目標 writer** 是否為終端決定
是否 `RemoveColorFromString` 去色。避免 per-call 翻轉全域（會與 spinner 背景 goroutine race）。

### D3 — 業務邏輯層禁止 import `internal/ux`
**Context**：曾有變更讓 `internal/manifest`/`marketplace`/`pack` import ux 以統一警告前綴。
**Decision**：**回退**。domain 層不得依賴 UI 呈現層（分層倒置 + `internal/ux` 拉入 huh/pterm 重樹）。
library 層若要 UI 化的警告，應**回傳 diagnostics 給 cmd 層**由 cmd 以 ux 呈現，而非自行 import ux。
目前 library 層維持原前綴直印（可接受的小不一致）。

### D4 — 互動偵測統一 `ux.CanPrompt()`
**Context**：`init` 曾用自己的 `isInteractive()`（ModeCharDevice）決定分支，與 ux 的
`term.IsTerminal` gate 不一致 → `init </dev/null` 進互動分支但 prompt 立即回預設 → 最終 Confirm
靜默同意、未帶 `--yes` 卻建立 apm.yml。**Decision**：所有互動分支判斷統一用 `ux.CanPrompt()`。

### D5 — browse box table 遷移 pterm.Table
放棄與 Python `rich` `HEAVY_HEAD` 的視覺 parity，換取全專案表格一致。
