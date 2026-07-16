# 執行計畫 v2 — lipgloss + huh（分支 feat/init-tui-lipgloss）

> 策略：`internal/ux` 門面 API 與 v1 相容 → **cmd 層的 ux 遷移可直接沿用 v1 成果**
> （前綴統一、串流保留、severity 分流、互動安全、一致性 sweep、About-to-create Box），
> 只需**重寫 internal/ux 的底層實作（pterm → lipgloss/huh）**。

## Phase 1 — internal/ux 以 lipgloss 重寫

1. **依賴（go.mod 目前乾淨，需全新新增）**：`go get charm.land/huh/v2 charm.land/lipgloss/v2`
   （huh 帶入 lipgloss；再把 lipgloss 升為直接相依）；子套件 `.../v2/table`、`.../v2/list`、
   `.../v2/tree`、`.../huh/v2/spinner`。module 路徑/版本以 `go get` 實際結果為準
   （`charm.land/*` 解析失敗則用 `github.com/charmbracelet/*/v2`）。
   → verify: `go get` exit 0、`grep -E "huh|lipgloss" go.mod` 各至少一筆、`go build ./internal/ux/...`
2. **colors.go / theme**：色票 + 符號 + lipgloss Style（Success/Info/Warn/Error 各自 Foreground）
   + huh Theme（沿用 v1）。
3. **訊息輸出**：`Success/Info/Warn/Error(w, …)` = `lipgloss` style Render + **`lipgloss.Fprint(w, …)`**
   （per-writer 去色）。確認 `lipgloss.Fprint` 對任意 w 偵測 profile；若否，快取
   `colorprofile.NewWriter(w, os.Environ())`。
4. **表格**：`Table` = `lipgloss/table`（RoundedBorder、BorderColumn、BorderHeader、
   StyleFunc header cyan bold）→ `lipgloss.Fprintln(w, t.String())`。
5. **結構化**：`BulletList` = `lipgloss/list`（Bullet enumerator + 縮排巢狀）、
   `Tree` = `lipgloss/tree`（原生 ├─└─ 連接線）、`Section`（標題樣式）、
   `Box`（lipgloss border + 標題）、`Diff`（+/- 上色）。
6. **Spinner**：`Spinner` = huh/spinner；非 CanPrompt 走靜態行、不阻塞。
7. **互動**：`Confirm/InputText/Password/MultiSelect` = huh（沿用 v1）。
8. **ux.go**：`Init`/`IsRich`/`CanPrompt`/TTY 偵測（`golang.org/x/term`）；著色交給 lipgloss
   per-writer，**不需全域旗標 / renderForWriter / isRichWriter**。保留測試 seam
   `SetTTYSeamsForTest`（供 cmd 測試）。
   → verify: `go build ./internal/ux/...`、`go test ./internal/ux/...`（單元測試 + 渲染證據）

## Phase 2 — 套用到 cmd（沿用 v1 façade 呼叫）

9. 從作廢分支取回 cmd 層 ux 遷移：
   `git checkout feat/init-tui-beautify -- cmd/apm-go`（這些檔只呼叫 ux.*，與底層無關）。
   逐一 reconcile：確認所有 ux.* 呼叫的簽章與新 internal/ux 一致（含 `ux.Box`、
   `ux.CanPrompt`、`SetTTYSeamsForTest`）。
   **init metadata 例外**：舊分支呼叫 4 次 `ux.InputText`（逐欄、前一欄消失、不可回退）→
   改用 `ux.InputForm`（單一群組表單，4 欄同時可見、Tab/Shift+Tab 回退修改）。
   → verify: `go build ./...`、`go run ./cmd/apm-go init` 手動確認 4 欄同畫面可回退

## Phase 3 — 移除 pterm + 全量驗證

10. **依賴收斂**：`go mod tidy` → 確認 go.mod 有 huh+lipgloss、無 pterm（本分支本就無 pterm）。
    → verify: `grep -rn "pterm" --include=*.go .` 應為空；`grep pterm go.mod` 為空；
    `grep -E "huh|lipgloss" go.mod` 各至少一筆
11. **全量驗證**：
    - `go build ./...`、`go vet ./...`、`go test ./... -count=1` 全綠（-race 待 CI）。
    - **表格渲染實測**（含 CJK + 換行）：`apm-go experimental list` / `marketplace browse`
      對齊、box-drawing、有 header 分隔線。
    - **per-writer 顏色實測**：`apm-go … > out.txt` → 檔案無 ANSI；真 TTY → 有色。
    - exit code 不變（audit/validate --strict）；`normalize` stdout 位元組不變。
- **【review gate】** codex `exec` 對抗審核（`git diff main...HEAD`，stdin 餵法）→ 修 CRITICAL/HIGH → commit

## 硬性規則
- 只動輸出/互動層；業務邏輯層不 import ux；exit code / stdout 機器輸出不變。
- 非互動 / CI / 重導向純文字、不阻塞。

## Phase 4 — 實測回饋修正（R7–R10；prd.md v2.1）

> Phase 1-3 已完成並推送。Phase 4 依實機 apm-go vs Python apm 對照補強輸出。
> 仍守硬性規則：只動輸出/互動層、exit code 與機器 stdout 不變、業務邏輯不改。

### 範圍（方案 A · 分流，本任務只做甲）
- **本任務（甲·純美化）執行**：步驟 **12、13**（符號統一寬度）、**14**（R9 既存/重複灰化）、
  **15**（R10b 樹聚合，含 R14 local 標籤）、**16**（R10a hash）、**17g**（F4 守衛）、**18**（驗證+閘門）。
- **分流至「輸出 parity」子任務（乙）**：步驟 **17**（R7）、**17b**（R11）、**17c**（R12）、
  **17d**（R13/R15 MCP 摘要與計數）、**17e**（R16）、**17f**（R17）。— 本任務不執行，保留供子任務移交。
- **分流至 resolver bug 子任務（丙）**：BUG-1。

12. **符號統一寬度（R8，定案 `Width(3)` 置中）**：
    - `internal/ux/colors.go`（或 printer）建 `symStyle = lipgloss.NewStyle().Bold(true).Foreground(<語意色>).
      AlignHorizontal(lipgloss.Center).Width(3)`；`printer.go` 的 `printLine` 改 `symStyle.Render(symbol) + msg`
      （**移除**中間的 `" "`），維持 `lipgloss.Fprintln(w, …)`。
    - `output.go` `newBulletList` 的 enumerator 改 `EnumeratorStyle(mutedStyle.Width(3).Align(lipgloss.Center))`
      （取代加空格）；符號與訊息同寬對齊。
    → verify: golden 斷言符號區佔 3 格、訊息對齊（如 render `✓`→`" ✓ "`）；`• ` 有間隔；per-writer 去色下空白 padding 仍在。
13. **Ambiguous 寬度實測（R8c 收尾）**：以實際終端截圖確認 `✓ ℹ •` 在目標終端對齊；於 design.md 記錄最終決策
    （已預寫 `Width(3)`；若錯位改符號或 `Width(4)`）。
    → verify: design.md 有書面決策（P4-7）；截圖對齊無錯位。
14. **muted / 既存語意（R9 / R10c）**：`internal/ux` 提供既存項目灰化途徑（如 `ux.Item` 增 `Muted bool`
    走 `ColorMuted`，或等效最小 API）；`install.go` 用 `existing`(:258)/`requestedKeys`(:239) 把既存 dep
    標為 muted、與新增區分。**不改部署集合**（scope 邊界）。
    → verify: 既存 dep 行以灰色呈現；install 摘要可見「新增 vs 既存」區分。
15. **deploy 樹聚合（R10b）**：改寫 `deployedFilesTree`（`install.go:1183-1211`）依 kind 聚合、dir 收斂到
    目標 root（去掉每 skill 子目錄），一 kind 一行、逗號列多 root。
    → verify: 安裝多 skill 的 dep 只出現 `N skill -> <root>, <root>` 一行 per kind（非數十行）。
16. **短 hash fallback（R10a）**：Installed 摘要（`install.go:1159-1169`）與 deploy label，tag/ref 空時
    fallback `@<短 commit 8 碼>`（源 `ResolvedCommit`/`Commit`/`ResolvedHash`）。
    → verify: unpinned dep 顯示 `@<8碼>`。
17. **uninstall 補資訊（R7）**：`uninstall.go:623-627` 摘要補套件名 + apm.yml 已更新（路徑）+ 清理檔案數
    （清理數若無現成資料，於 deploy 層加回傳值——呈現資料、非邏輯變更）。
    → verify: uninstall 輸出含套件名與清理數。
17b. **mcp 輸出補強（R11）**：`mcpinstall.go:170-174` 加印已配置目標清單（用既有 `deployed` slice，
    如 `✓ Configured for <targets>`）；`apm.yml: apm.yml` 改絕對路徑（`filepath.Abs`）；`•transport` 間隔由 12 修正。
    **不動 deploy 層**（資料已回傳）。
    → verify: `install --mcp` 輸出含目標清單 + 絕對路徑；`• ` 有空格。
17c. **輸出不對稱修正（R12，稽核於 research/output-parity-audit.md）**：
    - a. `pack.go:252` 正式分支補印 `result.Files`（照 `243-250` dry-run 寫法）。
    - b. `install.go:765` local-bundle 用 `result.Files` 補摘要，**沿用 R10b 聚合樹**（依 kind→root，勿逐檔）。
    - c/d. `audit.go:88`（成功）/`install.go:537`（frozen）細節走 `--verbose`，預設維持精簡數字。
    - e. compile（`internal/compile.Result` 需加欄位）**不做**，於 prd 標記另立子任務。
    → verify: pack 正式執行列出檔案；local-bundle 摘要為聚合樹非逐檔；audit/frozen 預設輸出位元組不變。
17d. **install plugin 輸出補強（R13/R14/R15，presentation-only）**：
    - R13：部署摘要區（`install.go:1060-1066`）加印 MCP 配置摘要（用既有 `deployResult.MCPProvenance`：server→targets）。
    - R14：`install.go:1062-1063` local 標籤 `(local)` → `<project root> (local)`。
    - R15：`install.go:1158` summary 補 `and M MCP server(s)`（`len(newLock.MCPServers)`）。
    - **不碰 BUG-1**（大小寫重複 dep 屬 resolver/manifest 業務層）；**不得**用 ux 灰化/隱藏其 shadow/0-files/幽靈 bullet。
    → verify: 裝含 MCP 的 plugin 時有 MCP 摘要；local 節點語意化；summary 含 MCP 計數；未新增遮蔽邏輯。
17e. **空 apm.yml + local 部署的矛盾訊息（R16，presentation-only）**：
    - `install.go:543-561`：有 local primitives 待部署時不印/改寫 `No dependencies to install`。
    - `install.go:1158`：`len(result.Deps)==0` 但有 local 部署時，summary 不印 `Installed 0 dependencies`，改反映 local。
    - **部署邏輯不變**（不改 hasAnyDeps/CollectLocalPrimitives 判斷，只改輸出時機與文案）。
    → verify: 在 `evals/test1`（apm.yml deps 空、有 .apm/）跑 `install`，輸出無「No deps + Installed 0」矛盾。
17f. **install 無 target 錯誤訊息（R17，F3）**：改寫 `errNoDeployTarget()`（install.go）印結構化診斷
    （列出掃描過的 harness marker + 具體修法 + apm.yml 範例，資料來自既有 signal-detection）；該錯誤路徑
    設 `cmd.SilenceUsage = true`（不砸整包 Cobra flag）。**不改偵測邏輯**，只改訊息與 usage 抑制。
    → verify: 在 `evals/bundle-demo`（無 target）跑 install，輸出為結構化診斷、無整包 flag dump；exit 2 不變。
17g. **F4 守衛（實作 R7/R10b/R12 時遵守）**：做部署樹聚合（R10b）與摘要精簡（R7/R12）時，
    `shadowed`/`deployed 0 files` 衝突警告**必須照實保留**（走 stderr），不得被聚合/精簡吞掉。
    → verify: 造跨 repo 同名 skill 情境（bundle-demo 補充案）跑 install，聚合後仍見衝突 `!` 警告。
18. **全量驗證 + 閘門**：`go build/vet ./...`、`go test ./... -count=1`（-race 待 CI）、per-writer golden、
    exit code 不變、`normalize` byte 不變；per-issue 實跑截圖比對；**codex 對抗閘門**（`git diff` stdin 餵法）
    → 修 CRITICAL/HIGH → 原子 commit（依 R 分組）。

## Rollback
- 全部集中 internal/ux + go.mod；新分支隔離。舊 pterm 分支已作廢（PR #2 已關）保留可參考。
- Phase 4 變更集中 `internal/ux/output.go`、`cmd/apm-go/install.go`、`cmd/apm-go/uninstall.go`，可獨立 revert。
