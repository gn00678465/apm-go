# Terminal UX Contract (`internal/ux`)

> 所有面向終端機使用者的輸出與互動，必須經 `internal/ux` 門面。禁止在 `cmd/apm-go`
> 直接呼叫 `lipgloss`/`huh`，或用裸 `fmt.Print*` 印帶語意前綴的訊息。
> 來源任務：`07-14-init-tui-beautify`（charmbracelet 生態：lipgloss v2 + huh v2）。
> **v2 已棄用 pterm**：pterm 寬度引擎無法對齊 box-drawing 表格，且顏色是全域旗標、有
> per-writer 洩漏 footgun。改用 lipgloss 後，著色為**函式庫原生 per-writer**（見 D2）。

---

## 1. Scope / Trigger

觸發 code-spec 深度的原因：這是**跨層共享的輸出/互動契約**（infra 整合 lipgloss/huh、
TTY/串流邊界行為）。任何新增指令輸出、互動 prompt、或報表都受此契約約束。

適用範圍：`cmd/apm-go/*.go`（呈現層）。**業務邏輯層**（`internal/manifest`、
`internal/marketplace`、`internal/pack` …）**禁止 import `internal/ux`**（見 D3）。

依賴：`charm.land/lipgloss/v2`（+ `/table`、`/list`、`/tree`）、`charm.land/huh/v2`
（+ 內部帶入 `charm.land/bubbles/v2`、`github.com/charmbracelet/colorprofile`）。無 pterm。

---

## 2. Signatures

```go
package ux

// 一次性初始化（main() 早期呼叫一次）。偵測 TTY/NO_COLOR/CI，設定 richMode 與
// styleEnabled。著色本身不需此呼叫（lipgloss 每次呼叫依 writer 自動判定）。
func Init()
func IsRich() bool     // 互動 prompt 是否應呈現（stdin+stderr TTY、無 NO_COLOR、非 CI）
func CanPrompt() bool  // 「能否在 stdin 阻塞式互動」（stdin+stderr 皆 TTY 且非 CI；**不含 NO_COLOR**）

// 前綴輸出 —— 每個都接受目標 writer；著色由 lipgloss 依「該 writer」自動決定（見 D2）。
func Success(w io.Writer, format string, a ...any)  // ✓ 綠
func Info(w io.Writer, format string, a ...any)     // ℹ 品牌青
func Warn(w io.Writer, format string, a ...any)     // ! 琥珀
func Error(w io.Writer, format string, a ...any)    // ✗ 紅

// 結構化輸出 —— 同樣接受 writer，per-writer 著色同上。
type Item struct { Level int; Text string }          // BulletList 縮排項（巢狀 sub-list）
type TreeNode struct { Text string; Children []TreeNode }
func Table(w io.Writer, headers []string, rows [][]string)  // lipgloss/table RoundedBorder；headers 可 nil（無 header 分隔線）
func BulletList(w io.Writer, items []Item)           // lipgloss/list Bullet enumerator
func Tree(w io.Writer, root TreeNode)                // lipgloss/tree 原生 ├─└─│ 連接線
func Section(w io.Writer, title string)
func Box(w io.Writer, title string, body []string)   // lipgloss RoundedBorder；title 為框內首行
func Diff(w io.Writer, diffText string)              // unified diff +/- 上色

// 進度（自刻 ticker 驅動 bubbles spinner；huh/spinner 只有同步 Run，無法 mid-flight Update）
type Spin struct { /* ... */ }
func Spinner(w io.Writer, text string) *Spin         // 非 rich writer → 印一行靜態 Info、不動畫
func (s *Spin) Update(text string)
func (s *Spin) Success(msg string)                   // finish() 冪等（sync.Once），可重複/併發呼叫
func (s *Spin) Fail(msg string)

// 互動（固定走 stderr + stdin）；非 CanPrompt 時回傳預設值、不阻塞。
type Option struct { Label, Value string; Selected bool }
func Confirm(prompt string, def bool) (bool, error)
func InputText(label, def string) (string, error)
func Password(label string) (string, error)          // 遮蔽輸入
func MultiSelect(title string, opts []Option) ([]string, error)

// 群組表單：所有欄位在同一 huh Group 一次渲染，全部同時可見、Tab/Shift+Tab 回退修改。
// 取代「呼叫 InputText N 次」（逐欄、前一欄消失、無法回退）。非 CanPrompt → 回傳各欄 Default。
type Field struct { Key, Label, Default string; Password bool; Validate func(string) error }
func InputForm(title string, fields []Field) (map[string]string, error)  // 回傳 key→value

// 測試 seam：強制 stdin/stdout/stderr TTY 狀態。
func SetTTYSeamsForTest(stdinTTY, stdoutTTY, stderrTTY bool) (restore func())
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
- 互動函式（Confirm/InputText/Password/MultiSelect/InputForm）固定 stderr + stdin，不污染 stdout。

### 著色：lipgloss 原生 per-writer（核心）
- 輸出函式一律 `s := style.Render(...)`（或 table/list/tree 的 `.String()`）→
  **`lipgloss.Fprintln(w, s)`**。`lipgloss.Fprint/Fprintln` 內部把 `w` 包進
  `colorprofile.NewWriter(w, os.Environ())`，依**該 writer 自己**的能力自動降階
  `TrueColor → ANSI256 → ANSI16 → mono → strip`，非 TTY / `NO_COLOR` 下自動去色。
- 因此**傳任意非終端 writer（檔案、`bytes.Buffer`、pipe）都會正確去色** —— 無全域旗標、
  無 footgun。pterm 時代的「全域著色 + 非終端 writer 洩漏」限制**已不存在**。
- `styleEnabled`（`Init()` 設定）**只**決定 `Spinner` 是否動畫，**不**決定著色。

### 環境鍵
| 鍵 | 效果 |
|---|---|
| `NO_COLOR`（非空） | lipgloss 去色（每個 writer 純文字）；**不**關閉互動 |
| `CI`（非空） | 去色/不動畫**且**關閉互動（走非互動預設值） |

### huh 輸出綁定 stderr（gotcha，見 §Common Mistakes）
- 互動元件的 huh Form/Field **必須** `.WithOutput(os.Stderr)`。normal 模式預設 stderr，但
  `TERM=dumb` 觸發的 **accessible 模式預設 stdout** —— 不綁定會把 prompt 寫進被重導向的
  stdout（污染 + 看似卡住）。`runField` 用 `huh.NewForm(huh.NewGroup(f)).WithShowHelp(false).
  WithOutput(os.Stderr)` 包裝單一欄位；`InputForm` 的 form 亦 `.WithOutput(os.Stderr)`。

---

## 4. Validation & Error Matrix

| 條件 | 著色（**per-writer**，每個 writer 各自判定） | 互動（Confirm/Input/…） |
|---|---|---|
| writer 為 TTY、無 NO_COLOR | 該 writer 上色（依能力降階） | — |
| writer 非 TTY（重導向/pipe/`/dev/null`/`NUL`/buffer） | 該 writer 純文字（lipgloss 去色） | — |
| `NO_COLOR` 設定 | 所有 writer 純文字 | **仍可互動**（若 stdin+stderr TTY） |
| `CI` 設定 | 純文字、不動畫 | 回傳預設值、不阻塞 |
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
`marketplace validate`/`audit --strict`、`marketplace package` 編輯失敗 exit 2）。

---

## 5. Good / Base / Bad Cases

- **Good**：`ux.Success(cmd.OutOrStdout(), "Installed %d dependencies", n)` —— 保留 stdout、
  TTY 上色、`> out.txt` 自動純文字、單複數正確。
- **Base**：`ux.Info(os.Stdout, "Already up to date")` —— 中性訊息，重導向下純文字 `ℹ Already up to date`。
- **Bad**：`ux.Warn(os.Stderr, "%s", diag)` 但下一行 `return errNoDeployTarget()` ——
  失敗原因被誤標為警告（應 `ux.Error`）。

---

## 6. Tests Required

- `internal/ux`（≥ 80% 覆蓋，現 89.9%）：
  - **per-writer 去色 golden**：對 `bytes.Buffer`（非 TTY writer）呼叫
    `Success/Info/Warn/Error/Table/BulletList/Tree/Section/Box/Diff`，輸出**不含** ANSI
    escape（`\x1b[`），且純文字含正確符號/資料。lipgloss 依 writer 自動去色 —— 不需全域旗標。
  - `CanPrompt` 對 NO_COLOR/CI/TTY 矩陣正確（**NO_COLOR 不關互動**）。
  - `Init` 的 richMode / styleEnabled 決策矩陣（both-TTY + 無 NO_COLOR + 非 CI）。
  - 非互動時 `Confirm/InputText/MultiSelect/InputForm` 回傳預設值、不阻塞（timeout 守衛）。
  - `Spinner` 非 rich → 靜態行不動畫；`Success` 後再 `Fail` **不 panic**（finish 冪等）。
  - `isTerminalWriter(bytes.Buffer)==false`（供 `Spinner` 判斷能否動畫用）。
- `cmd/apm-go`：以 `ux.SetTTYSeamsForTest(false,false,false)` 模擬非 TTY，斷言破壞性操作
  （`marketplace remove`、`init` 覆寫）**要求 `--yes`、不阻塞、不誤刪/誤寫**。
- 退場碼測試（audit/validate --strict）維持綠，**不得放寬斷言**。
- `-race`：CI 必跑 `go test ./... -race`（本機無 gcc 無法驗證）。Spinner 有背景 goroutine +
  ticker + mutex，**必須**在 CI 以 -race 驗證（`mu` 護 `text`；`stop→done` handshake；
  `finish` sync.Once）。

---

## 7. Wrong vs Correct

### Wrong
```go
// (a) 用「是否上色」predicate 決定能否互動 —— NO_COLOR 會誤關互動
if ux.IsRich() { ans, _ = ux.Confirm(...) }

// (b) 自刻 per-writer 去色（render-then-strip）—— lipgloss 已原生處理，多此一舉
s := style.Render(msg); if !isTTY(w) { s = stripANSI(s) }; fmt.Fprintln(w, s)

// (c) 用 ModeCharDevice 判定 TTY —— /dev/null 被誤判為互動
interactive := os.Stdin.Stat().Mode()&os.ModeCharDevice != 0

// (d) 把 stdout 結果行改到 stderr
ux.Success(os.Stderr, "Installed %d deps", n)   // 破壞腳本捕捉

// (e) huh Form 不綁 output —— TERM=dumb accessible 模式洩漏 prompt 到 stdout
huh.NewForm(huh.NewGroup(f)).Run()
```

### Correct
```go
// (a) 互動 gate 用 CanPrompt（stdin+stderr TTY 且非 CI，不含 NO_COLOR）
if ux.CanPrompt() { ans, err := ux.Confirm(...) } else { /* 要求 --yes 或用預設 */ }

// (b) 直接 lipgloss.Fprintln，去色交給 colorprofile 依 writer 判定
lipgloss.Fprintln(w, style.Render(msg))

// (c) 用 term.IsTerminal
interactive := term.IsTerminal(int(os.Stdin.Fd()))

// (d) 保留原串流
ux.Success(os.Stdout, "Installed %d deps", n)

// (e) huh Form 綁 stderr（覆蓋 normal 與 accessible 兩模式）
huh.NewForm(huh.NewGroup(f)).WithShowHelp(false).WithOutput(os.Stderr).Run()
```

---

## Common Mistakes

### huh accessible 模式（TERM=dumb）洩漏 prompt 到 stdout
**Symptom**：`TERM=dumb apm-go marketplace remove x > out.txt` 通過 `CanPrompt()`
（stdin/stderr 仍 TTY），但確認 prompt 被寫進 `out.txt`，使用者看不到提示、指令似卡住。
**Cause**：huh 對 `TERM=dumb` 自動切 accessible 模式，該模式輸出**預設 stdout**（normal
模式才預設 stderr）。**Fix**：所有 huh Form/Field 一律 `.WithOutput(os.Stderr)`。
**Prevention**：互動一律經 `runField`/`InputForm`，兩者已內建 stderr 綁定；不要在 cmd 層自建 huh Form。

### Spinner 終止呼叫非冪等 → close of closed channel panic
**Symptom**：`sp.Success(...)` 後又 `sp.Fail(...)`（或 defer 收尾 + 明確失敗路徑）panic。
**Cause**：`finish()` 直接 `close(s.stop)`，重複呼叫二次關閉已關 channel。
**Fix**：`finish()` 以 `sync.Once` 包住 `close(stop); <-done`，保證只執行一次。

---

## Design Decisions

### D1 — charmbracelet 生態：lipgloss（輸出）+ huh（互動）
**Context**：v1 曾用 pterm（輸出）+ huh（互動）。**Decision**：棄用 pterm，全部收斂到
charm 生態 —— lipgloss 做樣式/表格/list/tree/border/spinner-frames、huh 做互動表單。
`charm.land/lipgloss/v2` 由 huh 帶入、升為直接相依，**移除 pterm 相依**。
**理由**：lipgloss/table 正確算欄寬與 CJK 全形（對齊的 box-drawing），且著色原生 per-writer。

### D2 — 著色：lipgloss 原生 per-writer（取代 pterm 全域旗標）
**Context**：pterm 著色是**全域狀態**（`pterm.RawOutput`），無 per-writer 機制 —— 對 TTY
印過再對檔案 writer 印時容易漏關色（footgun）；v1 只能全域 both-TTY 一次決定，或自刻
`renderForWriter` 事後去色。**Decision**：改用 `lipgloss.Fprint/Fprintln(w, s)`，內部
`colorprofile.NewWriter(w, os.Environ())` 依**該 writer 自己**的 profile 自動降階/去色。
**結果**：著色是**每次呼叫、per-writer** 決定 —— 傳任意非終端 writer 都正確去色，無全域
狀態、無洩漏風險。`styleEnabled` 職責縮到只管 `Spinner` 動畫（把動畫寫進非終端 writer 無意義）。
**代價**：無（相較 v1 是純改進）。已讀 `charm.land/lipgloss/v2 v2.0.5` writer.go 確認機制。

### D3 — 業務邏輯層禁止 import `internal/ux`
**Context**：曾有變更讓 `internal/manifest`/`marketplace`/`pack` import ux 以統一警告前綴。
**Decision**：**回退**。domain 層不得依賴 UI 呈現層（分層倒置 + `internal/ux` 拉入 huh/lipgloss 重樹）。
library 層若要 UI 化的警告，應**回傳 diagnostics 給 cmd 層**由 cmd 以 ux 呈現，而非自行 import ux。

### D4 — 互動偵測統一 `ux.CanPrompt()`
**Context**：`init` 曾用自己的 `isInteractive()`（ModeCharDevice）決定分支，與 ux 的
`term.IsTerminal` gate 不一致 → `init </dev/null` 進互動分支但 prompt 立即回預設 → 最終
Confirm 靜默同意、未帶 `--yes` 卻建立 apm.yml。**Decision**：所有互動分支統一用 `ux.CanPrompt()`。

### D5 — 表格全面 lipgloss/table（含 browse），init metadata 群組表單
**Context**：browse 原模仿 Python `rich` `HEAVY_HEAD`；init metadata 原呼叫 4 次 InputText
（逐欄、前一欄消失、無法回退）。**Decision**：(1) browse 遷 `lipgloss/table` RoundedBorder，
放棄 rich parity 換全專案表格一致；(2) init metadata 改單一 `ux.InputForm` 群組表單（4 欄
同畫面、Tab/Shift+Tab 回退）。**注意**：群組表單無法反應式重算預設值 —— description 預設
由 cwd basename 算一次，若使用者改了 name 但未動 description，cmd 層事後同步
（`if desc==defaultDesc && name!=defaultName { desc = "APM project for "+name }`）。
