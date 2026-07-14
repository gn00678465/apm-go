# 美化 init 選單與 stdout 輸出（huh + pterm 統一主題）

## Goal

導入 `charmbracelet/huh`（互動表單）與 `pterm/pterm`（輸出樣式），建立一套統一的
APM 終端機視覺主題，重整目前散落各檔、手刻前綴的互動選單與 stdout 輸出。

## Constraints

- 裝飾性 / 人類可讀輸出一律寫到 **os.Stderr**；`normalize` 等真正機器可讀的
  **os.Stdout** 輸出保持乾淨、不得混入樣式碼。
- 非互動 / `--yes` / 非 TTY / `NO_COLOR` / CI 路徑必須自動降階為純文字（無動畫、無色）。
- 只動輸出與互動層，不改業務邏輯；既有測試維持綠燈。
- 套件策略：**pterm 主導輸出 + huh 主導互動**（已與使用者確認 A 案）。
- `marketplace browse` 的 box table 遷移到 `pterm.Table`（已確認，接受與 Python rich parity 的視覺差異）。

## Requirements

1. 新增 `internal/ux` package，集中：色票、符號、huh Theme、pterm printer 設定，
   並提供 `Success/Info/Warn/Error/Table/Spinner/Confirm/Input/MultiSelect/Password` 封裝。
2. 統一符號系統：`✓` success、`ℹ` info、`!` warn、`✗` error、`▸` progress、`•` list，
   取代現有混用前綴 —— **完整清單**：`[+] [i] [!] [warn] [>] [*] [x] [dry-run] [-]`
   （`[x]` 見 `marketplace_authoring.go:276`；`[dry-run]` 見 uninstall/pack）。**注意**：
   現有 `[!]` 同時被用於警告與真正的失敗，需逐處按真實嚴重度分流（真警告→`!`、真失敗→`✗`），非無腦替換。
   `pack.go:535 licenseUndeclaredWarning` 是 production 常數內嵌 `"[warn] …"`，也需一併改。
3. init 互動流程改用 huh：metadata Input、Target MultiSelect、建立前 Confirm。
   Target 多選採 huh 預設鍵（space 逐項切換），**放棄** 現有 `all`/`none` 批次切換與 `1-3`
   範圍語法（已確認）；連帶移除孤兒函式 `parseToggleInput`（init.go:316）。
4. marketplace / mcp 的 y/N 確認（`readYesNo`/`confirmOrRequireYes`/`promptReplaceMCP`）
   整併為 huh Confirm；MCP 機密輸入改用 huh Input Password。
5. pterm 承接：全域前綴、Spinner（fetch/resolve）、Table（marketplace list + browse）、
   BulletList/Section（uninstall dry-run、install 依賴樹、update plan）。
6. 單一 TTY/CI 偵測入口，統一決定 huh accessible 模式與 pterm 關色。
7. **全指令覆蓋（Workflow 24-agent 盤查補漏）** —— 前綴/報表美化的檔案範圍不限主要指令，
   須含以下獨立子指令：
   - `audit` / `audit --content`（`audit.go`、`audit_content.go`）：完整性錯誤前綴 +
     結構化安全掃描報表（依 Severity 分色 + Spinner + stat 摘要）。
   - `pack`（`pack.go`）：三個 producer 的 success/info/warn + dry-run 檔案清單。
   - `marketplace` authoring 家族：`check` 逐包清單、`outdated` 表格、`audit` 兩層巢狀報告
     （TreePrinter）、`migrate` dry-run diff、`validate` finding + Summary。
   - `marketplace package` add/set/remove（`marketplace_package.go`）前綴。
   - `experimental list` 表格（`experimental.go`）。
   - `apm-go validate` diagnostics 逐筆清單（`main.go:72`）。
8. **（選配）** help/banner 客製化：root 補 Long/Example 或自訂 cobra usage template。

## Acceptance Criteria

- [ ] `internal/ux` package 存在並有單元測試：關色模式輸出純文字 golden 通過。
- [ ] `go build ./...`、`go vet ./...` 通過；`go test ./...` 維持綠燈。
- [ ] init 在 TTY 下呈現 huh 表單（Input / MultiSelect / Confirm）；`--yes` 與非 TTY 下維持純文字。
- [ ] stdout 前綴全面統一為新符號系統，無殘留 `[+]/[i]/[!]/[warn]/[>]/[*]`
      （含 audit / pack / experimental / marketplace authoring 家族 / marketplace package / main validate）。
- [ ] audit --content 報表依 Severity 分色；marketplace audit 兩層報告用 TreePrinter。
- [ ] marketplace list 與 browse 皆用 `pterm.Table` 渲染。
- [ ] 進度訊息（marketplace fetch、install resolve）為 spinner，完成收斂為 `✓`。
- [ ] 裝飾輸出全走 stderr；`normalize` stdout 位元組輸出不變。
