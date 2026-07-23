# Design — init clack 風格互動介面 (issue #14)

> 依據：`prd.md`、`research/huh-clack-style.md`（huh v2.0.3 / lipgloss v2.0.5 原始碼證據）
> 既有契約：`.trellis/spec/backend/terminal-ux-contract.md`

## 1. 邊界與決策總覽

| 決策 | 選擇 | 理由 |
|---|---|---|
| D1 互動引擎 | 續用 huh v2，不引入 go-clack、不手刻 bubbletea | go-clack 為 huh 的完整替代面（8 stars、單一維護者），只為 init 引入會造成雙 prompt 棧並存；huh 已整合且 `mcp_prompt.go` 依賴之 |
| D2 transcript 產生方式 | **field 完成後**由呼叫端補印 `◇ 標題 / │ 答案` | huh `Form.View()` 於 quitting 回傳 `""`（`form.go:655-657`），bubbletea 將空 view 收合為 0 高度並抹除（`cursed_renderer.go:262-264`）—— prompt 一定會自我清除，沒有原生殘留機制；補印是 huh README 自身建議模式，且**不需任何游標操作**（風險最低） |
| D3 樣式作用域 | **完全不改 huh 主題**：進行中的 prompt 維持現況（含 `┃` gutter），clack 視覺只由結束後補印的 transcript 承擔 | 使用者決策 2026-07-23「保持目前的樣式，只調整 `WithButtonAlignment`」。同時避免波及 `cmd/apm-go/mcp_prompt.go:103,109,127` 的 credential prompts（PRD AC5），並省掉一整層 init-local theme 的複雜度 |
| D4 Confirm 按鈕對齊 | `WithButtonAlignment(lipgloss.Left)` 加在**全域** `ux.Confirm` | 這是 bug 修正而非樣式偏好（huh 預設 `lipgloss.Center` 把按鈕推離左緣，`field_confirm.go:52-53`）；theme 無法修正（`WithButtonAlignment` 是 `*Confirm` builder method，`field_confirm.go:361-364`） |
| D5 glyph 選擇 | 執行期偵測終端 Unicode 支援，二選一符號集 | `colors.go:19-37` 既有政策為避免 East-Asian-Ambiguous 寬度破壞固定寬度欄位對齊；go-clack 上游同樣以 `s(unicode, ascii)` gating。本設計沿用同一取捨 |
| D6 banner 位置 | art 常數放 `cmd/apm-go/banner.go`，渲染經 `ux` | terminal-ux-contract §1 禁止 cmd 直接呼叫 lipgloss；art 本身是產品資產故留在 cmd |

### D2 補充：為何不做「即時改寫已印出的步驟符號」

clack 原生會把作答中的 `◆` 於送出後改寫為 `◇`（游標上移重繪）。本設計**不做改寫**：
一律以 `◇`（submitted）印出持久化 transcript，作答中的視覺狀態由 huh 自身的 live prompt 承擔。
理由：跨平台游標操作（Windows console + huh 剛結束的 raw mode）風險遠高於收益，且 transcript
的價值在於**留存的最終狀態**。此為刻意的視覺簡化，非未驗證的宣稱。

---

## 2. 新增 API（`internal/ux/clack.go`）

```go
// Clack 是 init 專用的 clack/vercel 風格輸出器：每個互動步驟完成後於 w 補印
// 持久化的連接線 transcript。它同時封裝 clack 主題的 huh prompt，避免 cmd 層
// 自行組裝 huh（terminal-ux-contract §1）。
type Clack struct {
    w   io.Writer
    sym clackSymbols   // Unicode 或 ASCII 符號集，建構時決定
}

func NewClack(w io.Writer) *Clack

// 純輸出
func (c *Clack) Banner(art string)                   // logo（無 gutter），muted/brand 上色
func (c *Clack) Intro(title string)                  // "┌  title"
func (c *Clack) Bar()                                // "│"（步驟間空連接線）
func (c *Clack) Step(title, answer string)           // "◇  title" + "│  answer"（多行答案逐行加 gutter）
func (c *Clack) Note(title string, body []string)    // "◇ title ───╮" / "│ ... │" / "├───╯"
func (c *Clack) Outro(msg string)                    // "│" + "└  msg"

// 互動（跑 clack 主題的 huh field，完成後自動 Step(...) 補印 transcript）
func (c *Clack) Confirm(title string, def bool) (bool, error)
func (c *Clack) Form(title string, fields []Field) (map[string]string, error)
func (c *Clack) MultiSelect(title string, opts []Option) ([]string, error)
```

### 符號集（`clackSymbols`）

對映沿用 go-clack `prompts/symbols/symbols.go` 的 unicode/ascii 兩套：

| 用途 | Unicode | ASCII fallback |
|---|---|---|
| StepSubmit | `◇` | `o` |
| BarStart | `┌` | `T` |
| Bar | `│` | `\|` |
| BarEnd | `└` | `-` |
| BarH | `─` | `-` |
| CornerTopRight | `╮` | `+` |
| ConnectLeft | `├` | `+` |
| CornerBottomRight | `╯` | `+` |

`supportsUnicode()`（新增於 `internal/ux/ux.go` 或 `clack.go`，含測試 seam）：

- Windows：`WT_SESSION`（Windows Terminal）、`TERM_PROGRAM`（vscode 等）任一非空 → true；否則 false
  （傳統 conhost 預設 codepage 無法保證 box-drawing）
- 非 Windows：`TERM` 不為 `linux`/空 → true
- `NO_COLOR` **不影響** 此判定（NO_COLOR 只關顏色，不關字元集；與 `CanPrompt` 的既有理由一致）

> 註：`Banner` 的 ANSI Shadow art 由 `█ ╗ ╝ ═` 等 box-drawing/block 組成 —— 非 Unicode 終端
> 下不印 banner（見 §4 R1 gating），不做 ASCII 版 art。

### 為何 Clack 帶自己的 prompt 方法（而非讓 init 呼叫全域 `ux.Confirm` 再自行補印）

1. 「跑 prompt → 補印 transcript」是一組不可分的動作，分散到 cmd 層會被遺漏；
2. 避免在 cmd 層出現 huh/lipgloss import（契約 §1）。

因 D3 已決定不換主題，`Clack.Confirm/Form/MultiSelect` 內部**直接呼叫既有的全域
`Confirm/InputForm/MultiSelect`**，成功後再 `Step(title, answer)` 補印 —— 不需要
`confirmWith`/`formWith`/`multiSelectWith` 這層 theme 參數化重構（YAGNI）。
`WithButtonAlignment(lipgloss.Left)` 直接加在既有的 `ux.Confirm` 內（D4），
兩條路徑（init 與 mcp_prompt）自動都修好。

---

## 3. 主題：不變（D3）

`internal/ux/theme.go` **不修改**。進行中的 huh prompt 維持現有外觀（含 `ThemeBase` 繼承來的
`┃` 左邊框），只有 `interactive.go` 的 `Confirm` 建構鏈加入按鈕靠左（D4）。

視覺上的取捨：作答中的 `┃` 與 transcript 的 `│` 不是同一個字元，但 huh 結束時會自我清除
（D2 引述的 `form.go:655-657`），兩者不會同時停留在畫面上，因此不構成持續性的不一致。

---

## 4. init 流程改寫（`cmd/apm-go/init.go`）

流程與分支邏輯完全不變（PRD R4），只在互動分支插入 clack 輸出：

```
Phase 1 專案名解析                     ── 不變
Phase 2 apm.yml 覆寫檢查
        └ 互動：ck.Banner(art) → ck.Intro("apm-go init")
                ck.Confirm("apm.yml already exists. Continue and overwrite?", false)
Phase 3 metadata                       ── ck.Form(...)（取代 ux.InputForm）
Phase 4 target 選擇                    ── ck.MultiSelect(...)
Phase 5 摘要 + 確認                    ── ck.Note("About to create", body) 取代 ux.Box
                                          ck.Confirm("Is this OK?", true)
Phase 6 寫檔                           ── 不變
Phase 7 成功輸出                       ── ck.Outro("Done! Install a package: apm-go install <owner>/<repo>")
```

Banner/Intro 的 gating（PRD R1）：

```go
interactive := !yes && ux.CanPrompt()
```

- `--yes` / `--force` / non-TTY / CI → 走原本非互動路徑，**完全不建立 Clack、不印 banner/transcript**，
  stdout/stderr 位元組行為與現況一致（保住 `init_nontty_test.go`）
- banner 另需 `supportsUnicode()` 為真才印（block art 無 ASCII 版）
- **非互動路徑的訊息維持現行 `ux.Info/Success/Section`**，不改字串（避免破壞既有斷言）

取消路徑（overwrite 選 No / summary 選 No / 無 target 且不續行）：以 `ck.Outro("Initialization cancelled.")`
之類收尾，確保 gutter 有結束線；具體字串沿用現行訊息文字，只改呈現。

---

## 5. 影響面與相容性

| 面向 | 影響 |
|---|---|
| `cmd/apm-go/mcp_prompt.go` | 只受 D4 按鈕靠左修正影響，其餘樣式不變（AC5） |
| stdout 契約 | 無變更；所有 init 輸出仍走 stderr |
| exit code | 無變更 |
| 既有測試 | `init_nontty_test.go` 走非互動路徑，不受影響；`init_targetselect_test.go` 需確認是否斷言 `ux.MultiSelect` 呼叫（改為 `Clack.MultiSelect` 時需同步 seam） |
| spec | `terminal-ux-contract.md` §2 Signatures 需補 `Clack` 型別與 `supportsUnicode`；§6 補測試要求；§7 補「Confirm 按鈕靠左」的 Wrong/Correct（Phase 3 更新） |

## 6. 測試策略

1. `internal/ux/clack_test.go`
   - `Banner/Intro/Bar/Step/Note/Outro` 對 `bytes.Buffer` 的 golden 輸出：Unicode 與 ASCII 兩套符號集各一組；斷言**不含** ANSI escape（`\x1b[`，per-writer 去色契約 §6）
   - 多行答案的 gutter 逐行前綴正確
   - `Note` 的框線寬度以最長行為準、右緣對齊
2. `supportsUnicode` 決策矩陣測試（`WT_SESSION`/`TERM_PROGRAM`/`TERM=linux`/`NO_COLOR` 不影響），以 env seam + `t.Setenv`
3. `interactive_test.go` 擴充：`Confirm` 建構帶 `Left` 對齊（透過既有 `runField` seam 捕捉 field，斷言其可觀察屬性；若 huh 未暴露 alignment getter，改以「建構不 panic + 呼叫鏈順序正確」的建構測試 + 在 §7 smoke test 目視驗證）
4. `Clack.Confirm/Form/MultiSelect` 在非 `CanPrompt` 時回傳預設值、不阻塞、**不印 transcript**（沿用 `runWithTimeout` 守衛）
5. `cmd/apm-go`：新增測試斷言 `--yes` 與 non-TTY 下 stderr **不含** banner 字元；既有 non-tty 測試維持綠

## 7. 未驗證項目（實作時必須做的 smoke test）

research 明確標示未在真實 TTY 驗證（`research/huh-clack-style.md` Q3 caveat）：

- huh 逐 field 啟停與手動列印交錯是否有閃爍／殘影
- Windows Terminal 與傳統 conhost 下 `◇ │ └ ╮ ├ ╯` 與 banner block art 的實際渲染寬度
- `Note` 框在 Ambiguous 寬度被渲染為 2 欄時的右緣對齊（既有 `ux.Box`/`Table` 已承擔同類風險）
- `WithButtonAlignment(lipgloss.Left)` 的實際視覺結果（huh 未暴露 alignment getter，單元測試無法斷言）

以上四項須在實作階段以 `go run ./cmd/apm-go init` 於真實終端目視確認後，才可視為完成
（使用者決策 2026-07-23：目視確認為必要門檻）。
