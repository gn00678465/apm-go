# init 互動介面改為 clack 風格 (issue #14)

## Goal

美化 `apm-go init` 的互動體驗（GitHub issue #14）：

1. 開頭顯示 APM-GO 的 ASCII art logo（ANSI Shadow 字型，issue 內附範例）
2. 互動流程改為 @clack/prompts / Vercel CLI 風格的連接線敘事：已完成步驟以 `◇  <標題>` + `│  <答案>` 持續顯示在畫面上，步驟間以 `│` 串接，結尾 `└  Done! ...`，摘要以 `◇ <標題> ───╮ ... ├───╯` 框呈現
3. 修正 Confirm 的 `Yes  No` 按鈕右偏排版（huh 預設 `buttonAlignment: lipgloss.Center`，`field_confirm.go:52-53`）

## Background（research 摘要，全文見 research/huh-clack-style.md）

- `┃` 左邊框來自 `huh.ThemeBase` 的 `Focused.Base`（`theme.go:111`），theme 可換字元；按鈕偏移只能靠 `*Confirm.WithButtonAlignment(lipgloss.Left)`（builder method，theme 改不了，`field_confirm.go:361-364`）
- huh form 結束時 `View()` 回傳空字串（`form.go:655-657`），bubbletea 會清掉整個 prompt —— **huh 沒有 clack 式「已答步驟殘留」功能**，殘留 transcript 必須由呼叫端在每個 field 完成後手動印出（這也是 huh README 自己的建議模式）
- `ux.Confirm/InputText/Password` 另有 `cmd/apm-go/mcp_prompt.go:103,109,127` 三個呼叫點 —— 全域 theme 改動會影響 credential prompts
- `internal/ux/colors.go:19-37` 已有既定政策：避免 East-Asian-Ambiguous-width Unicode glyph（部分終端字型把 `◇` 等字元渲染為兩欄寬）；go-clack 上游也用 `s(unicode, asciiFallback)` 做同樣的 gating
- 採**選項 (b)**：保留 huh 做輸入，手動印 clack transcript。不引入 go-clack（8 stars 小型庫，且會與 huh 雙棧並存）、不手刻 bubbletea

## Requirements

### R1 — ASCII art logo
- `init` 在互動模式（`CanPrompt()` 且 rich）開頭印出 APM-GO ANSI Shadow banner（issue #14 附的六行版本），輸出到 stderr
- 非互動 / `--yes` / non-TTY / CI 情境**不印** banner（stdout/stderr 行為與現行完全一致，不得破壞 non-tty 測試）
- `NO_COLOR` **不**抑制 banner：該變數的語意是「不要加顏色」而非「不要輸出圖形」，與 `CanPrompt` 既有的「NO_COLOR 不關閉互動」同一推理（`internal/ux/ux.go:47-51`）。此時 lipgloss 會 per-writer 去色，banner 以純文字呈現
- 以 Go raw string 常數硬編碼，不引入 figlet 類相依

### R2 — clack 風格 transcript（init-only）
- 每個互動步驟完成後，於 stderr 印出持續可見的紀錄行：`◇  <步驟標題>` 與 `│  <使用者答案>`，步驟間以 `│` 空行串接
- 摘要（現 Phase 5 的 `ux.Box`）改為 clack 風格框：`◇ <標題> ─...─╮`、內容行以 `│` 開頭、`├─...─╯` 收尾
- 流程結尾以 `└  <完成訊息>` 收束
- transcript 渲染做成 `internal/ux` 的新 helper（clack gutter printer），**只有 init.go 呼叫**；`mcp_prompt.go` 等其他指令的輸出樣式不變
- glyph 依既有 `colors.go` 政策 gating：rich 模式用 Unicode（`◇ │ └ ╮ ├ ╯ ─`），非 rich／不支援時退回 ASCII（比照 go-clack 的 fallback 對映：`o | — + -`）

### R3 — Confirm 排版修正（全域 bugfix，最小改動）
- `ux.Confirm` 加上 `WithButtonAlignment(lipgloss.Left)`：`Yes  No` 按鈕靠左對齊在問題正下方，不再置中偏移。此為 bugfix，全域生效（mcp_prompt.go 的 confirm 一併受益）
- 全域 `Theme()` 的樣式不變（credential prompts 外觀不受影響）

### R5 — 進行中的 prompt 也要在連接線上（2026-07-23 目視驗證後追加）
- 初版只改結束後的 transcript，實跑發現 metadata 群組表單同時出現三種左緣：focus 欄位 `┃`（huh `ThemeBase` 粗邊框）、blurred 欄位無邊框（本專案 theme 設為 HiddenBorder）、Group 標題無邊框，整段浮在連接線之外
- 新增 init-local `clackTheme(sym)`：focus/blurred 欄位、Group 標題／說明一律改為 `│` 邊框 + `PaddingLeft(2)`，與 transcript 的 `│  ` 對齊；ASCII fallback 同步用 `|`
- 欄位間的 gutter 空行由 `PaddingBottom(1)` 產生，**不可**放進 `FieldSeparator`（lipgloss 多行補寬會造成縮排偏移／雙空行，詳見 design.md 與 spec D6）
- huh 的 keybinding footer 在 transcript 中一律關閉（`Group.View` 於 footer 前寫死一行無 gutter 的空字串，樣式碰不到）；MultiSelect 的按鍵提示改放進欄位 `Description`，避免 R19 的「space 切換無從得知」迴歸
- 全域 `Theme()` 與非 transcript 指令的 footer 不得受影響

### R4 — 行為不變性
- init 的**功能流程**（overwrite 確認 → metadata form → target 選擇 → 摘要確認 → 寫檔）與所有分支邏輯（`--yes`/`--force`/`--target`/non-TTY fallback/取消路徑）完全不變，只改視覺呈現
- 所有輸出仍走 stderr（stdout 保持乾淨，符合 terminal-ux-contract）
- 既有測試（`init_nontty_test.go`、`init_targetselect_test.go`、`internal/ux` 測試）行為斷言不因樣式改動而失效；樣式斷言需更新者一併更新

## Out of Scope

- 其他指令（install/uninstall/compile 等）的 clack 化 —— 只做 init（issue #14 範圍）。使用者決策 2026-07-23：先限制 init，目視驗證後再決定是否擴充
- 更換 huh 進行中 prompt 的邊框/主題（見 R3）
- 引入 go-clack 或替換 huh
- Python oracle parity：oracle 的 init 無 banner 無 gutter（rich/click 樣式），issue #14 為刻意的樣式分歧，不列 parity gate

## Acceptance Criteria

- [ ] AC1: 互動模式 `apm-go init` 開頭顯示 APM-GO ASCII banner；`--yes` 與 non-TTY 模式完全不顯示
- [ ] AC2: 互動流程結束後，終端上留有 clack 風格 transcript（`◇` 步驟標題、`│` 答案、`│` 串接線、`└` 結尾），並包含 clack 風格摘要框
- [ ] AC3: overwrite 與其餘 Confirm 的 `Yes  No` 按鈕靠左對齊於問題下方（不再右偏）
- [ ] AC8: init 進行中的 prompt（Confirm／群組表單／MultiSelect）每一行都以 gutter 起始，欄位之間恰好一條空 gutter 行，且不出現 huh 的 footer 或其前導空行；`mcp_prompt.go` 的 prompt 與 `ux.MultiSelect` 維持 huh 原本外觀（含 footer）
- [ ] AC9: init 的 MultiSelect 顯示 `space to toggle, enter to confirm` 提示（取代被關閉的 footer）
- [ ] AC4: 不支援 Unicode 的終端輸出 ASCII fallback glyph 且不印 banner，無亂碼；`NO_COLOR` 只去色、不改符號集
- [ ] AC5: `mcp_prompt.go` 的 prompt 除按鈕對齊修正外樣式不變
- [ ] AC6: init 全部既有功能測試通過（`go test ./...`），non-TTY stdout/stderr 契約不變
- [ ] AC7: 新增 transcript helper 與 banner gating 的單元測試，`internal/ux` 覆蓋率不低於現狀

## Notes

- 實作前需在真實 TTY 做 smoke test：research 未能驗證 huh 逐 field 啟停 + 手動列印交錯是否有閃爍（source-derived 推論，見 research Q3）。使用者決策 2026-07-23：目視確認為必要門檻，不可略過
- `.trellis/spec/backend/terminal-ux-contract.md` 若 init 的 prompt 模式因此與文件描述分歧，完成後需同步更新（Phase 3）
