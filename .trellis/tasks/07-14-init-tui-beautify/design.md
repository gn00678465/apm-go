# 技術設計 v2 — init/stdout 美化（lipgloss + huh）

## 決策

- **棄用 pterm**，改用 charmbracelet 生態：`lipgloss`（樣式/表格/邊框/顏色）+ `huh`（互動/spinner）。
- `internal/ux` **門面 API 維持與 v1 相容**，只換底層實作 → cmd 層呼叫點（前綴/串流/severity/
  互動安全）沿用 v1 的成果，不重寫。

## 依賴

- **現況（2026-07-15 核對 go.mod）**：本分支自 `main` 分出，go.mod **乾淨** —— huh / lipgloss /
  pterm **皆不在**。以下依賴需**全新 `go get`**（非「已相依」）：
- `charm.land/huh/v2` + `charm.land/huh/v2/spinner`（huh 會帶入 lipgloss v2）。
- `charm.land/lipgloss/v2` + 子套件 `.../v2/table`、`.../v2/list`、`.../v2/tree`（huh 帶入，
  但需在 go.mod 升為直接相依）。實際 module 路徑與版本以 `go get` 結果為準（Charm 目前用
  `charm.land/*` vanity；若解析失敗回退 `github.com/charmbracelet/*/v2`）。
- 顏色偵測：lipgloss v2 內部用 `github.com/charmbracelet/colorprofile`（隨 lipgloss 帶入）。
- **pterm 無需移除**（本分支從未引入）；新增依賴後 `go mod tidy`。

## 門面契約（internal/ux，API 同 v1）

```go
package ux

func Init()                    // 一次性：偵測 CanPrompt 條件、設定 huh accessible
func IsRich() bool
func CanPrompt() bool          // stdin+stderr TTY 且非 CI（不含 NO_COLOR）

// 訊息（帶符號前綴 + lipgloss 色）；每個接收目標 writer。
func Success(w io.Writer, format string, a ...any)  // ✓ 綠
func Info(w io.Writer, format string, a ...any)     // ℹ 品牌青
func Warn(w io.Writer, format string, a ...any)     // ! 琥珀
func Error(w io.Writer, format string, a ...any)    // ✗ 紅

// 結構化
type Item struct { Level int; Text string }
type TreeNode struct { Text string; Children []TreeNode }
func Table(w io.Writer, headers []string, rows [][]string)
func BulletList(w io.Writer, items []Item)
func Tree(w io.Writer, root TreeNode)
func Section(w io.Writer, title string)
func Box(w io.Writer, title string, body []string)
func Diff(w io.Writer, diffText string)

// 進度（huh/spinner）
func Spinner(w io.Writer, text string) *Spin
func (s *Spin) Update(text string); func (s *Spin) Success(msg string); func (s *Spin) Fail(msg string)

// 互動（huh，固定 stderr+stdin；非 CanPrompt 回傳預設值不阻塞）
type Option struct { Label, Value string; Selected bool }
func Confirm(prompt string, def bool) (bool, error)
func InputText(label, def string) (string, error)   // 單一欄位（少數場景）
func Password(label string) (string, error)
func MultiSelect(title string, opts []Option) ([]string, error)

// 群組表單：所有欄位在同一 huh Group 一次渲染 —— 全部同時可見、Tab/Shift+Tab
// 在欄位間回退修改（huh PrevField/NextField）。取代「呼叫 InputText N 次」的
// 逐欄獨立表單（會讓前一欄資訊消失、無法回退）。非 CanPrompt → 回傳各欄預設值。
type Field struct {
    Key, Label, Default string
    Password bool
    Validate func(string) error
}
func InputForm(title string, fields []Field) (map[string]string, error)  // 回傳 key→value
```

## 著色模型（核心改進：lipgloss 原生 per-writer）

- 樣式：`lipgloss.NewStyle().Foreground(lipgloss.Color("#…")).Bold(true).Render(text)` 產生字串。
- 輸出：**`lipgloss.Fprint(w, s)` / `lipgloss.Fprintln(w, s)`** —— 依 **該 writer** 的 colorprofile
  自動 downsample（TrueColor→ANSI256→ANSI16→mono）並在**非 TTY 時去色**。
- **已讀 `charm.land/lipgloss/v2 v2.0.5` 原始碼確認**（writer.go:81）：
  ```go
  func Fprintln(w io.Writer, v ...any) (int, error) {
      return fmt.Fprintln(colorprofile.NewWriter(w, os.Environ()), v...)
  }
  ```
  `lipgloss.Fprint/Fprintln(w, …)` 把 `w` 包進 `colorprofile.NewWriter(w, env)`，**依該 writer
  自己的 profile（TTY/NO_COLOR）downsample/去色**。
- 因此：`apm-go list > out.txt`（stdout 非 TTY）→ 檔案**自動純文字**；真 TTY → 有色（且依終端
  能力 TrueColor/256/16 降階）。**per-writer、函式庫原生、每次呼叫判定**：傳任意非終端 writer
  （檔案/`bytes.Buffer`）都會正確去色 → **徹底無 footgun**（pterm 全域旗標的洩漏問題不存在）。
- 因此 ux 輸出函式一律：`s := style.Render(text)`（或 table `.String()`）→ `lipgloss.Fprintln(w, s)`。
  不需 renderForWriter / isRichWriter / 全域旗標。

### 色票 / 符號（單一來源，colors.go）
`ColorBrand #2dd4bf`、`ColorHeading #8aa0ff`、`ColorSuccess #3fb950`、`ColorWarning #d29922`、
`ColorError #f85149`、`ColorMuted #8b949e`；`✓ ℹ ! ✗ ▸ •`。

### 表格（lipgloss/table）
```go
t := table.New().
    Border(lipgloss.RoundedBorder()).
    BorderStyle(lipgloss.NewStyle().Foreground(lipgloss.Color(ColorMuted))).
    BorderColumn(true).BorderHeader(true).      // 欄分隔 + header/body 分隔線
    Headers(headers...).Rows(rows...).
    StyleFunc(func(row, col int) lipgloss.Style {
        if row == table.HeaderRow { return headerStyle /* cyan bold */ }
        return cellStyle /* padding */
    })
lipgloss.Fprintln(w, t.String())
```
lipgloss 正確算欄寬與 CJK 全形 → **對齊**、box-drawing、可有 header 分隔線（解決 pterm 的所有問題）。

### BulletList / Tree（lipgloss 原生子套件）
- `BulletList` → `charm.land/lipgloss/v2/list`：`list.New().Items(...).Enumerator(list.Bullet).
  EnumeratorStyle(mutedStyle).ItemStyle(...)`；縮排層級用巢狀 sub-list（`list.New()` 當 item）。
- `Tree` → `charm.land/lipgloss/v2/tree`：`tree.New().Root(name).Child(...)`，原生連接線
  （`├─`/`└─`/`│`），供 install 部署摘要、marketplace audit 兩層巢狀用。

### Box（Section/建立前確認）
`lipgloss.NewStyle().Border(lipgloss.RoundedBorder()).BorderForeground(brand).Padding(0,1).Render(content)`，
標題可用 `lipgloss.JoinVertical` 或 border title 技巧。

## 符號 / 前綴 / 文字替換對照（明確）

### 訊息前綴符號（單一來源 colors.go；lipgloss Foreground 上色）
| 語意 | 新符號 | 顏色 | 取代的舊前綴 |
|---|---|---|---|
| success | `✓` | green #3fb950 | `[+]` `[*]` |
| info | `ℹ` | cyan #2dd4bf | `[i]` |
| warn | `!` | amber #d29922 | `[warn]`，`[!]`（真警告） |
| error | `✗` | red #f85149 | stderr 裸字串，`[!]`（真失敗） |
| progress | `▸`（或 spinner 動畫） | cyan | `[>]` |
| list item | `•` / `-` | muted | `-` `|--` |

- `[!]` **依語意分流**：non-fatal → `!`（warn）；緊接失敗/exit≠0 → `✗`（error）。
- refcheck 的 `[x]/[+]/[*]/[i]` Status token 是**資料**，在 cmd 渲染層對應到符號（不改 refcheck）。
- `pack.go` 的 `licenseUndeclaredWarning` 等 production 常數內嵌前綴 → 移除字面，由 `ux.Warn` 供符號。

### 文字/區塊替換（維持語意，只改呈現）
- init：`Setting up your APM project...` → `ux.Section`；`+--- About to create ---+` 手刻框 →
  `ux.Box`；`Next steps:` → `ux.Section` + `ux.BulletList`；成功行 → `ux.Success`。
- init metadata 輸入（name/version/description/author）→ **`ux.InputForm` 單一群組表單**
  （4 欄同時可見、Tab/Shift+Tab 回退修改），**取代** v1 呼叫 4 次 `ux.InputText`
  （逐欄獨立、前一欄消失、無法回退）。
- marketplace list / browse padding 表 → `ux.Table`；`source:`/`ref:` 明細 → `ux.BulletList`。
- install 部署摘要 `|-- N type -> dir` → `ux.Tree`；`Installed N dependencies` 複數修正、空 `@tag` fallback。
- uninstall/pack dry-run `[dry-run]` + 逐項 → `ux.Section` + `ux.BulletList`。
- update plan `[i] Update plan` → `ux.Section`；migrate diff → `ux.Diff`。
- audit --content 逐筆 → 依 severity 分色（`ux.Error/Warn/Info`）；marketplace audit 兩層 → `ux.Tree`。

### Spinner（huh/spinner）
`spinner.New().Title(text).Action(fn).Run()`（同步）或 `.ActionWithErr(ctx, fn)`。非 TTY/CI 下
huh spinner 需確認不阻塞（accessible / 直接執行 action 並印靜態行）；`ux.Spinner` 封裝此判斷。

## 串流契約
- 逐處保留原 writer；stdout 結果不搬 stderr；`normalize` 走 `os.Stdout.Write`，不經 ux，位元組不變。
- 互動元件固定 stderr+stdin。

## 業務邏輯層界線
`internal/manifest`·`marketplace`·`pack` **不 import ux**（維持 v1 決定）；library 層警告維持原樣。

## 相容性 / 風險
- lipgloss v2 是 beta 系列（`charm.land/lipgloss/v2 v2.0.5`）；API 以實際安裝版為準，讀 module 確認。
- huh/spinner 在非 TTY 的行為需驗證（不得在 CI 阻塞）；`ux.Spinner` 對非 CanPrompt 走靜態輸出。
- `lipgloss/table` 的 border/column 開關與 v1 mockup 的視覺對齊，需以實際渲染（含 CJK/換行）驗證。
- 移除 pterm 後確認無其他程式碼引用 pterm。

## Rollback
- 全部集中在 `internal/ux` + go.mod；新分支 `feat/init-tui-lipgloss` 隔離。舊 pterm 分支已作廢保留可參考。
