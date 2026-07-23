# Implement — init clack 風格互動介面 (issue #14)

> 依據 `prd.md` / `design.md`。每步驟附驗證方式；TDD：先寫測試（RED）再實作（GREEN）。

## Step 0 — 基準

- `go test ./...` 全綠、記錄 `internal/ux` 現有覆蓋率（契約要求 ≥80%，現 89.9%）
- 驗證：測試輸出與覆蓋率數字留底，供 Step 7 比對

## Step 1 — Confirm 按鈕對齊修正（獨立 bugfix，可先行）

- `internal/ux/interactive.go`：既有 `Confirm` 的建構鏈加入 `WithButtonAlignment(lipgloss.Left)`
  （必須在 `.WithTheme(...)` 之前，因其回傳 `*Confirm` 而 `WithTheme` 回傳 `Field`）
- **不做 theme 參數化重構**（D3：主題不變，Clack 直接複用全域 prompt 函式）
- 測試：`interactive_test.go` 以 `runField` seam 捕捉建構出的 field，斷言建構成功且鏈順序不 panic
- 驗證：`go test ./internal/ux/...`；真實終端目視 `Yes  No` 靠左（併入 Step 7 smoke test）

## Step 2 — 符號集與 Unicode 偵測

- 新增 `internal/ux/clack.go`：`clackSymbols` 結構、`unicodeSymbols`/`asciiSymbols` 兩套常數
  （對映表見 design.md §2）
- 新增 `supportsUnicode()` 與其測試 seam（比照 `stdinIsTTY` 等 var 型 seam）
- 測試（先寫）：決策矩陣 —— Windows `WT_SESSION`/`TERM_PROGRAM` 非空 → true；非 Windows `TERM=linux`/空 → false；
  `NO_COLOR` 不影響結果
- 驗證：`go test ./internal/ux/ -run Unicode`

## Step 3 — Clack 輸出器（純輸出部分）

- `Clack` 型別 + `NewClack(w)`；`Banner/Intro/Bar/Step/Note/Outro`
- 一律經 `lipgloss.Fprintln(w, ...)`（per-writer 去色，契約 §3）
- `Step` 對多行答案逐行加 gutter；`Note` 以最長行決定框寬
- 測試（先寫）：golden 輸出（Unicode / ASCII 兩套）、無 ANSI escape、多行答案 gutter、`Note` 右緣對齊
- 驗證：`go test ./internal/ux/ -run Clack`

## Step 4 — Clack 的 prompt 方法

- `theme.go` **不動**（D3）
- `Clack.Confirm/Form/MultiSelect`：直接呼叫既有的全域 `Confirm/InputForm/MultiSelect`
  → 成功後呼叫 `Step(title, answer)` 補印
- 非 `CanPrompt()` 時：回傳預設值、不阻塞、**不印 transcript**
- 測試（先寫）：非互動回傳預設值 + 不印 transcript（`runWithTimeout` 守衛）；互動 seam 下確認補印格式
- 驗證：`go test ./internal/ux/...`

## Step 5 — banner 資產

- 新增 `cmd/apm-go/banner.go`：ANSI Shadow "APM-GO" art（issue #14 提供的六行版本）以 raw string 常數
- 驗證：`go build ./...`

## Step 6 — init 流程接線

- `cmd/apm-go/init.go`：互動分支建立 `ck := ux.NewClack(os.Stderr)`；依 design.md §4 置換
  Phase 2/3/4/5/7 的輸出與 prompt 呼叫
- `interactiveTargetSelect` 簽章加入 `ck *ux.Clack` 參數（原直接呼叫 `ux.MultiSelect`/`ux.Confirm`），
  `init_targetselect_test.go` 三個既有測試同步傳入 `ux.NewClack(io.Discard)`
- 非互動路徑（`--yes`/`--force`/non-TTY/CI）訊息字串**不動**
- 新增測試：`--yes` 與 non-TTY 下 stderr 不含 banner/gutter 字元
- 驗證：`go test ./cmd/apm-go/...`，特別是 `init_nontty_test.go` 維持綠

## Step 7 — 真實終端 smoke test（design.md §7，不可略過）

於 Windows Terminal 與傳統 conhost 各跑一次 `go run ./cmd/apm-go init` 於暫存目錄，目視確認：

1. banner 渲染正確、無破字
2. transcript gutter 連續、無閃爍或殘影（huh 逐 field 啟停與手動列印交錯）
3. `Yes  No` 靠左對齊於問題下方（prompt 進行中的其餘樣式維持現狀，D3）
4. `Note` 摘要框右緣對齊
5. 取消路徑（overwrite 選 No）gutter 有正確收尾

任一項失敗 → 回到對應 Step 修正，不得以「應該沒問題」帶過。

## Step 8 — 收尾

- `go fmt ./...`、`go vet ./...`、`go test ./... -cover`（`internal/ux` 覆蓋率不低於 Step 0 記錄值）
- `trellis-check` 子代理審查
- `terminal-ux-contract.md` §2/§6 補 `Clack` 型別、`supportsUnicode`、新測試要求
- commit（Phase 3.4）→ PR（連結 issue #14）→ review → `/trellis:finish-work`（**merge 前於 branch 上執行**）

## 風險與對策

| 風險 | 對策 |
|---|---|
| huh 逐 field 啟停與手動列印交錯造成閃爍 | Step 7 smoke test 驗證；若確有問題，退回「全部 prompt 跑完後一次印完整 transcript」的變體（視覺略遜但零風險） |
| Ambiguous 寬度導致 `Note` 右緣錯位 | ASCII fallback 已備；必要時 `Note` 改為不畫右緣（僅左 gutter + 底線），與既有 `ux.Box` 風險一致 |
| 既有測試因樣式斷言失敗 | 非互動路徑字串刻意不動，將衝擊面限縮在互動分支 |
