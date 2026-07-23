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
func Success(w io.Writer, format string, a ...any)  // + 綠
func Info(w io.Writer, format string, a ...any)     // i 品牌青
func Warn(w io.Writer, format string, a ...any)     // ! 琥珀
func Error(w io.Writer, format string, a ...any)    // x 紅

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

// clack 風格連接線 transcript（目前只有 init 使用；見 D6）。
type Clack struct { /* ... */ }
func NewClack(w io.Writer) *Clack          // 依 supportsUnicode() 決定 Unicode / ASCII 符號集
func (c *Clack) Banner(art string)          // 區塊藝術字 + 空行；非 Unicode 終端不印
func (c *Clack) Intro(title string)         // ┌  title
func (c *Clack) Bar()                       // │
func (c *Clack) Detail(text string)         // │  text（muted 補充說明）
func (c *Clack) Warn(format string, a ...any) // │  ! text（保留 §4 的警告 severity）
func (c *Clack) Step(title, answer string)  // ◇  title / │  answer（多行答案逐行加 gutter）
func (c *Clack) Note(title string, body []string)  // ◇ title ──╮ / │ … │ / ├──╯
func (c *Clack) Outro(msg string)           // │ / └  msg
// 以下三個跑對應的全域 prompt，成功後自動 Step(...) 補印；非 CanPrompt → 回傳預設值且不輸出。
func (c *Clack) Confirm(title string, def bool) (bool, error)
func (c *Clack) Form(title string, fields []Field) (map[string]string, error)
func (c *Clack) MultiSelect(title string, opts []Option) ([]string, error)

// 主題：Theme() 為全域共用；clackTheme(sym) 為 Clack 專用的 init-local 變體，
// 讓進行中的 huh 欄位（含 Group 標題、blurred 欄位）也坐在 transcript 的連接線上。
func Theme() huh.Theme
// clackTheme(sym clackSymbols) huh.Theme  —— 未匯出，僅供 Clack 使用

// 測試 seam：強制 stdin/stdout/stderr TTY 狀態。
func SetTTYSeamsForTest(stdinTTY, stdoutTTY, stderrTTY bool) (restore func())
```

單一色票/符號來源（`internal/ux/colors.go`）：
`ColorBrand #2dd4bf`、`ColorHeading #8aa0ff`、`ColorSuccess #3fb950`、`ColorWarning #d29922`、
`ColorError #f85149`、`ColorMuted #8b949e`；`SymbolSuccess +`、`SymbolInfo i`、`SymbolWarn !`、
`SymbolError x`、`SymbolProgress >`、`SymbolList *`（R8/P4-7：原符號集 ✓ ℹ ✗ ▸ • 除 `!` 外皆為
East-Asian Ambiguous 寬度，換成寬度確定為 1 的 ASCII 等義符號，見 colors.go 註解）。

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
| `NO_COLOR`（非空） | lipgloss 去色（每個 writer 純文字）；**不**關閉互動；**不**影響 `Clack` 的 Unicode 符號選擇 |
| `CI`（非空） | 去色/不動畫**且**關閉互動（走非互動預設值） |
| `WT_SESSION` / `TERM_PROGRAM` / `ConEmuANSI=ON` / `TERM`（Windows） | 任一成立 → `Clack` 用 Unicode 符號集；全不成立（裸 conhost）→ ASCII fallback |
| `TERM=dumb` 或 `TERM=linux` | `Clack` 一律用 ASCII fallback、不印 banner |

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
| clack transcript 進行中的 non-fatal 警告 | `Clack.Warn`（**不可**用 `Detail` 降級為 muted 說明） |
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
  - `Clack`：Unicode / ASCII 兩套符號集的 golden 輸出；`Note` 每行視覺寬度一致（右緣對齊）；
    banner 只在 Unicode 終端輸出；`Confirm/Form/MultiSelect` 在非 CanPrompt 或 prompt 失敗時
    **不留下 transcript**（否則等於謊報使用者答過）；`defaultSupportsUnicode` 的環境矩陣。
  - `Confirm` 的按鈕縮排**不隨問題長度改變**（huh 預設 `lipgloss.Center` 的迴歸，見 §7）。
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

// (f) 用 huh 預設建 Confirm —— buttonAlignment 預設 lipgloss.Center，按鈕列被置中在
//     max(問題寬, 按鈕寬) 的框內，問題越長按鈕越偏右（issue #14）
huh.NewConfirm().Title(prompt).Value(&val).WithTheme(Theme())

// (g) 期待 huh 把答完的 prompt 留在畫面上 —— Form.View() 在 quitting 時回傳 ""，
//     bubbletea 把空 view 收合為 0 高度並抹除，什麼都不會留下
ok, _ := ux.Confirm("...", true)   // 畫面上不會有任何紀錄
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

// (f) 按鈕靠左；WithButtonAlignment 回傳 *Confirm，必須排在回傳 Field 的 WithTheme 之前
huh.NewConfirm().Title(prompt).Value(&val).
    WithButtonAlignment(lipgloss.Left).WithTheme(Theme())

// (g) 要留紀錄就自己補印（huh README 自身的建議模式）；init 用 ux.Clack 把
//     「跑 prompt → 補印 transcript」綁成一組動作
ok, _ := ck.Confirm("...", true)   // 完成後留下 "◇ 問題 / │ 答案"
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

### D6 — clack transcript 由呼叫端補印，huh 主題不動（issue #14 / 任務 07-23-init-clack-style-ui）
**Context**：`init` 想要 @clack/prompts 風格的連接線敘事（已答步驟持續留在畫面上）。查證
huh v2.0.3 原始碼：`Form.View()` 在 quitting 時回傳 `""`（`form.go:655-657`），bubbletea 把
空 view 收合為 0 高度並抹除（`cursed_renderer.go:262-264`）—— **huh 沒有任何「保留已答步驟」
的機制**，且 theme 也無法在 Title 前注入符號（`Title.Render(text)` 永遠用呼叫端傳入的字串，
`field_confirm.go:243-244`）。
**Decision**：(1) 保留 huh 做輸入，**不引入第二套 prompt 棧**（評估過 `github.com/orochaa/go-clack`：
它是 huh 的完整替代面而非外掛，只為 init 引入會造成雙棧並存）；(2) transcript 由呼叫端在
prompt 結束後補印 —— 這也是 huh README 自身的建議模式，且**不需任何游標操作**；(3) 進行中的
prompt 以 **init-local `clackTheme(sym)`** 讓 huh 的欄位框也坐在同一條連接線上，全域 `Theme()`
**不變**，`mcp_prompt.go` 的 credential prompts 維持原樣。
**為何 (3) 必要**：初版曾決定「進行中的樣式完全不改」，實跑 metadata 群組表單後推翻 ——
huh 一次渲染全部欄位，focus 欄位是 `ThemeBase` 的粗邊框 `┃`、blurred 欄位被本專案 theme 設為
HiddenBorder（只剩縮排）、Group 標題無邊框，**同一個表單出現三種左緣**，整段浮在連接線之外。
**代價**：不做 clack 原生的「送出後把 `◆` 改寫為 `◇`」，一律直接印 `◇` —— 跨平台游標操作
（Windows console + huh 剛退出 raw mode）的風險高於收益。
**附帶修正**：`Confirm` 的按鈕改 `lipgloss.Left`（huh 預設 `Center`，`field_confirm.go:52-53`），
此為 bug 修正故**全域生效**，`mcp_prompt.go` 一併受益。

**huh 的 keybinding footer 無法拉上連接線**：`Group.View` 在 footer 前**寫死**一行空字串
（`group.go:374`），任何樣式都碰不到；而 footer 本身雖走 `Group.Base`（`group.go:407`，與
`Form.Base` 不同），一旦給它邊框，`showHelp=false` 時的 `Base.Render("")` 會回傳一條多餘的
gutter 空行而非 `""`。故 **Clack 一律 `showHelp=false`**，MultiSelect 改把按鍵提示放進欄位
的 `Description`（畫在欄位邊框內側，天然在線上）。非 transcript 的指令維持 huh 原本的 footer。

**gutter 顏色語意**：邊框字元統一之後，**顏色是唯一的 focus 提示** —— brand 綠 = 使用者
正在編輯的欄位，其餘（blurred 欄位、Group 標題／說明）一律 muted。Group 標題永遠不會被
focus，若也塗成 brand 會出現一條「永遠亮著卻不隨 Tab 移動」的綠線，顏色即失去意義。
transcript 那側同理：`Bar`/`Step`/`Note` 的直線一律 muted，只有 `◇` 標記用 brand。

**`clackTheme` 的兩個踩雷點**（改動前必讀）：
1. 欄位之間的 gutter 空行**必須**來自各欄位的 `PaddingBottom(1)`（padding 在邊框內側，
   故該行也會畫出 bar），**不可**改成把 bar 放進 `FieldSeparator`：lipgloss 對任何多行
   render 都會把每行補齊到最寬行（`lipgloss/style.go:489-496`），`"\n<bar>\n"` 的尾行空字串
   會被補成一個空格而讓下一個欄位縮排一格；且與 `PaddingBottom` 併用時會疊成兩條空行。
   `FieldSeparator` 只能是單一 `"\n"`（各行皆空、補寬目標為 0，故無副作用）。
2. `Group.Title` / `Group.Description` 需**單獨**把 `PaddingBottom` 設回 0，否則群組標題
   下方會多一條空行。
