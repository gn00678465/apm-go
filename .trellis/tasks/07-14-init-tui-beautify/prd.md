# 美化 init 選單與 stdout 輸出（charmbracelet 生態：lipgloss + huh）

> **v2 計畫（改用 lipgloss，棄用 pterm）**。原 pterm 版分支 `feat/init-tui-beautify` 已作廢
> （pterm v0.12.83 的寬度引擎無法渲染「對齊的 box-drawing 表格」，且顏色是全域旗標、
> 有 per-writer 洩漏 footgun）。新分支 `feat/init-tui-lipgloss`。

## Goal

為 `apm-go` 建立統一的終端機視覺主題，重整散落各檔、手刻括號前綴的互動選單與 stdout 輸出。
以 **charmbracelet 單一生態** 承接：`lipgloss`（樣式 / 表格 / 邊框 / 顏色）+ `huh`（互動表單 /
spinner，建於 bubbletea）。

## 為何 lipgloss 而非 pterm

- **表格對齊**：`lipgloss/table` 正確處理欄寬與 CJK 全形（終端排版的黃金標準），能渲染
  對齊的 box-drawing 表格（`RoundedBorder`/`NormalBorder`、可控 header 分隔線與欄分隔）。
  pterm 對多位元組 box-drawing 分隔會算錯寬度導致跑版。
- **per-writer 顏色（原生）**：`lipgloss.Fprint(w, s)` 透過 `colorprofile` **依該 writer 是否為
  TTY 自動 downsample / 去色**。這是函式庫原生行為 —— 徹底取代先前手刻的 `renderForWriter`，
  且**沒有全域旗標 footgun**（pterm 的 per-writer ANSI 洩漏問題不存在）。
- **單一生態**：huh 已建於 bubbletea，`charm.land/lipgloss/v2` 已是直接相依。改用 lipgloss
  可**移除 pterm 相依**，全部收斂到 charm 生態。

## Constraints

- 裝飾性 / 人類可讀輸出走 **os.Stderr**；`normalize` 等機器可讀的 **os.Stdout** 保持乾淨、不經 ux。
- 非互動 / `--yes` / 非 TTY / `NO_COLOR` / CI 自動降階為純文字（lipgloss/colorprofile 原生處理）。
- 只動輸出與互動層，不改業務邏輯；業務邏輯層（`internal/manifest`·`marketplace`·`pack`）
  **禁止 import `internal/ux`**。
- exit code 全部保留（`audit`/`audit --content` 0/1/2、`marketplace validate`/`audit --strict`）。
- 串流保留：逐處把該行原本的 `os.Stdout`/`os.Stderr`/`cmd.OutOrStdout()`/`cmd.ErrOrStderr()` 傳給 ux。

## Requirements

1. 新增 `internal/ux` 門面（**API 與 v1 相容**，只換底層實作），集中主題（色票 / 符號 /
   lipgloss Style / huh Theme）與封裝：
   - 訊息：`Success/Info/Warn/Error(w, format, a...)` — lipgloss Style + `lipgloss.Fprint(w, …)`。
   - 表格：`Table(w, headers, rows)` — `lipgloss/table`（box-drawing、對齊、header 分隔線）。
   - 結構化：`BulletList`、`Tree`、`Section`、`Box`（lipgloss border）、`Diff`。
   - 進度：`Spinner` — huh/spinner（`.Title().Action().Run()`）。
   - 互動：`Confirm/InputText/Password/MultiSelect` — huh（不變）。
2. 統一符號：`✓ success`、`ℹ info`、`!` warn、`✗ error`、`▸ progress`、`•` list（取代
   `[+] [i] [!] [warn] [>] [*] [x] [dry-run] [-]` 混用前綴；`[!]` 依語意分流 warn/error）。
3. 全指令覆蓋（延續 v1 的 26 點盤查）：init 互動、install/marketplace/uninstall/update 主要輸出、
   audit/pack/experimental/marketplace authoring 家族/marketplace package/main validate 子指令報表。
4. 互動偵測統一 `ux.CanPrompt()`（stdin+stderr TTY 且非 CI，排除 NO_COLOR）。
5. 顏色降階：交給 lipgloss `colorprofile`（TrueColor→ANSI256→ANSI16→mono→strip），
   `NO_COLOR`/非 TTY 原生去色，**per-writer**，無全域旗標、無 footgun。

## Acceptance Criteria

- [ ] `internal/ux` 改以 lipgloss + huh 實作，**移除 pterm 相依**（`go mod tidy` 後 go.mod 無 pterm）。
- [ ] `go build ./...`、`go vet ./...` 通過；`go test ./...` 全綠（CI 補 `-race`）。
- [ ] 表格為 lipgloss/table：box-drawing、**對齊**、有 header/body 分隔線、CJK 全形正確。
- [ ] `apm-go list > out.txt`（stdout 非 TTY）→ 檔案無 ANSI；真 TTY → 有色。per-writer 由
      `lipgloss.Fprint` 原生處理，無「全域旗標 + 非終端 writer 洩漏」footgun。
- [ ] init 在 TTY 呈現 huh 表單；建立前確認用 lipgloss Box；`--yes`/非 TTY 純文字不阻塞。
- [ ] stdout 前綴全面統一新符號，cmd production 無殘留舊前綴。
- [ ] 全指令 exit code 不變；`normalize` stdout 位元組不變。

---

## Requirements — v2.1 實測回饋補充（apm-go vs Python apm 對照）

> 來源：實機執行 `install` / `uninstall` 與 Python apm 對照，發現輸出**資訊遺漏、間隔不一致、
> 語意色彩缺失、部署摘要過長且缺 hash**。以下 R7–R18 為本次補強；仍受既有硬性規則約束
> （只動輸出/互動層、exit code 與機器 stdout 不變、業務邏輯不改）。

### 範圍收斂（方案 A · 已採納：分流）
盤點（含 `research/full-ab-parity-sweep.md` 實跑）後三分，**本任務（07-14）自此只實作「甲」**：
- **甲 · 純美化（＝本任務範圍）**：R8 符號統一寬度、R9 既存/重複灰化、R10a hash、R10b 樹聚合、
  R14 local 標籤（`<project root> (local)`，純文案）。性質是「怎麼呈現既有資料」的樣式層 —— 先做先交付。
- **乙 · 內容 parity（分流 → 新子任務「輸出 parity」）**：R7、R11、R12、R13、R15、R16、R17、R18。
  性質是「補印缺失內容」，presentation-only 但與「美化」不同，獨立規劃驗收。
- **丙 · 業務 bug（分流 → 新子任務）**：BUG-1（大小寫重複 dep）。F1/F4 為 Python 側問題、apm-go 不需修。
- **跨線守衛（本任務實作 R10b 時即須遵守）**：F4 —— 樹聚合不得吞掉 `shadowed`/`deployed 0 files` 衝突警告。
> 乙/丙的 R 編號與盤點**保留於本文件作為移交清單**，實作移出本任務。下方 R7/R11–R18、BUG-1 段落即移交內容。

**R7. uninstall 輸出資訊補齊（parity，不需逐字對齊 Python 措辭）**
現況過度精簡（`uninstall.go:623-627` 僅 `✓ Removed N package(s)` + `✓ apm_modules: removed N director`），
相比 Python apm 遺漏關鍵資訊：被移除的**套件名**、apm.yml 已更新（**路徑**）、清理的 **integrated 檔案數**。
需補齊到「關鍵資訊不遺漏」。錨點 `uninstall.go:623-627`（非 verbose 摘要）、`486-506`（module 移除）。
清理檔案數若非現成資料，可由 deploy 層回傳（屬呈現資料、非邏輯變更）。

**R8. 符號統一顯示寬度（定案：`Width(3)` 置中）**
所有門面符號一律以固定寬度置中渲染，一次解決「間隔不一致」與「符號寬度不協調」：
- **方案**：`lipgloss.NewStyle().Bold(true).Foreground(<語意色>).AlignHorizontal(lipgloss.Center).Width(3)`
  渲染符號 → `symStyle.Render(sym) + msg`（Width 已含右 padding，**不再另加空格**）→ `lipgloss.Fprintln(w, …)`。
- a. `ux.BulletList` 的 `•` enumerator 改用 `Width(3).Align(Center)`（現況 `•text` 無空格；改後與訊息符號同寬對齊）。錨點 `internal/ux/output.go`。
- b. 訊息符號 `✓ ℹ ! ✗ ▸` 全門面（printer/section/spinner）統一走上述 3 格置中，訊息自第 4 格起對齊。錨點 `internal/ux/printer.go`。
- c. `Background` 不設（透明）；只 Foreground 語意色。**實作期須以實際終端截圖確認 Ambiguous 寬度（`✓ ℹ •`）對齊**，錯位則改符號或 `Width(4)`；決策記錄於 design.md（P4-7）。

**R9. 語意色彩 + 重複/既存項目灰化**（對 Python `[+]` 重複項轉灰）
- 新增「muted / 已存在」呈現語意：已在 apm.yml 的**既存 dep**、重複項目以 `ColorMuted` 灰色標示。
- install 摘要**區分**「本次新增」vs「已存在」。程式已可判定（`requestedKeys` `install.go:239` / `existing` `install.go:258`），屬**呈現層**改動。

**R10. install 部署摘要精簡 + 顯示 hash**
- a. **短 hash**：dep 標籤在 `ResolvedTag`/`ResolvedRef` 皆空（unpinned）時，fallback 顯示短 commit（`@e9fcdf95`，前 8 碼；源 `dep.ResolvedCommit`/`dep.Commit`/`dep.ResolvedHash`）。錨點 `install.go:1159-1169`（Installed 摘要）、`deployedFilesTree` label `1183/1210`。
- b. **部署樹聚合**：現況 `deployedFilesTree`（`install.go:1183-1211`）依 `(kind, 最深層 dir)` 分組 → 每個 skill 各自子目錄成獨立節點 → 數十行牆。改為依 primitive **kind** 聚合、目標目錄收斂到 **root**（`.agents/skills/`、`.claude/skills/`），輸出如 `22 skill(s) -> .agents/skills/, .claude/skills/`（對 Python `22 skill(s) integrated -> ...`）。
- c. 已存在於 apm.yml 的 dep **不以「新安裝」呈現**（灰化或標註，承 R9）。

**R11. `install --mcp`（mcpinstall）輸出補強（parity，presentation-only）**
現況 `mcpinstall.go:170-174` 僅印 `✓ Added MCP server "X"` + `transport` + `apm.yml: apm.yml`（相對、寫死），
相比 Python apm 遺漏「配置到哪些 target」與 apm.yml 絕對路徑。
- a. **顯示已配置的目標清單** —— `deployMCPEntry` 回傳的 `deployed` slice（`mcpinstall.go:148/170` **已有此資料、只是沒印**），呈現如 `✓ Configured for copilot, claude, codex` 或逐 target 行。
- b. `apm.yml: apm.yml` → **絕對路徑**（`filepath.Abs`）。
- c. `•transport` 間隔（同 R8a，一併涵蓋）。
- **重要框定**：pre-migration apm-go **亦同樣簡略**（ux 遷移為 1:1 `fmt.Printf→ux.*`，非回歸）；此為 **parity 補強、presentation-only**（`deployed` 已算出、不改業務邏輯、無需動 deploy 層）。錨點 `mcpinstall.go:170-174`。

### Scope 邊界（重要）
> **本任務性質澄清**：`07-14-init-tui-beautify` 原始目標是**美化既有輸出**（符號/色彩/表格/間隔/hash/樹聚合）。
> R7（uninstall 缺資訊）與 R11（mcp 缺目標清單）本質是 **apm-go ↔ Python apm 的「內容 parity」**，
> 非美化回歸 —— 但兩者所缺資料**都已在程式內算出、只是沒印**，故仍歸類為 presentation-only、可在本任務內補。
> 若後續發現需**新增業務層資料**才能 parity 的缺口，應另立「輸出 parity」子任務，不塞進美化任務。

**R12. 消除「dry-run/錯誤路徑比正式/成功路徑更詳細」的輸出不對稱**
（來源：`research/output-parity-audit.md` 一次性稽核；R7/R11 同型，資料皆已算出）
- a. **pack 正式執行**（`pack.go:252`）：`--dry-run` 分支（`243-250`）已用 `ux.BulletList` 印 `result.Files`，
  正式分支只印 `len()` → 補印檔案清單（近乎複製貼上）。**presentation-only、高優先**。
- b. **local-bundle install**（`install.go:765`）：一般 install 有 `deployedFilesTree`，local-bundle 只印數字 →
  用既有 `result.Files`（+`Hashes`）補摘要，**須沿用 R10b 的聚合樹**（依 kind→target root，避免逐檔洗版）。
  **presentation-only、高優先**。
- c. **audit bare 成功**（`audit.go:88`）：失敗逐列違規檔，成功只印總數。**低優先**：預設保留數字（逐列會洗版），
  細節走 `--verbose`（避免重蹈 R10b 牆狀輸出）。`DeployedHashes` key 即檔案路徑，資料已在。
- d. **install frozen 成功**（`install.go:537`）：只印固定字串 → 補驗證 dep 數（清單走 `--verbose`）。**低優先**。
- e. **compile**（`compile.go:73` / `internal/compile.Result` struct `compile.go:182-201`）：需在 `Result` **新增欄位**
  才能暴露 `SourcedInstruction` 來源清單 → **需業務層變更、不在美化任務範圍**，另立子任務評估（低頻指令，AGENTS.md 本身即輸出）。

> R12 修復原則：補「清單」時**沿用既有聚合/樹樣式**（R10b），高頻/大量項目**預設精簡、細節走 --verbose**，
> 不因補資訊反而製造洗版。a/b 高優先（反差明顯、近複製貼上）；c/d 低優先；e 出範圍。

**R13. install 主流程 MCP 部署摘要（presentation-only）**
安裝含 MCP server 的 plugin 時，MCP **已被部署**（`deployResult.MCPProvenance` 已有每個 server→targets 資料，
`install.go:1115-1124`），但 install 流程 stdout **完全沒印** MCP 配置摘要（對比 Python apm 有「MCP Servers (N) …
Configured N server」整段）。應在部署摘要區印出「配置了哪些 MCP server → 哪些 target」。與 R11（standalone `install --mcp`）
同型、不同呼叫點。錨點 `install.go:1060-1066`（明細區）/`1115-1124`（資料源 `MCPProvenance`）。

**R14. 部署樹 `(local)` 節點標籤語意化（presentation-only）**
`install.go:1062-1063` 對 local 部署印 `(local)`，語意不明；改為 `<project root> (local)`（或等義），
表明「來自 `.apm/` 專案本地的部分」。對比 Python apm `[+] <project root> (local)`。

**R15. install 摘要含 MCP server 計數（presentation-only）**
`install.go:1158` 現況 `✓ Installed N dependencies` 未反映本次配置的 MCP server；改為
`Installed N dependencies and M MCP server(s)`（用 `len(newLock.MCPServers)`；計時 optional、低優先）。
對比 Python apm `Installed 2 APM dependencies and 1 MCP server in 11.7s`。

**R16. 空 apm.yml + 有 local 部署時的輸出矛盾（一致性，presentation 為主）**
apm.yml 無遠端 dep（`dependencies.apm: []`）但有 `.apm/` local primitives 時，輸出自相矛盾：先印
`ℹ No dependencies to install`（`install.go:544` 無條件），接著**仍部署 local 並印樹**，最後
`✓ Installed 0 dependencies`（`install.go:1158` 只算遠端 dep）。三句互相矛盾。對比 Python apm：不印矛盾行、
summary 為 `Installed 1 APM dependency`（把 local `<project root>` 算 1）。
- a. 有 local primitives 待部署時，**不印/改寫** `No dependencies to install`（`install.go:543-561`）。
- b. summary（`install.go:1158`）在 `len(result.Deps)==0` 但有 local 部署時，**不印誤導的 `Installed 0 dependencies`**；
  改為反映 local 部署（比照 apm 計 local 為 1，或 `Installed local project`）。
- presentation-only：**部署邏輯不變**，只改「何時印哪句 + summary 描述」。
- 附註（R10b 聚合精進）：Python `3 agents integrated -> 3 targets` 揭示同一 kind 跨多 target 同名目錄時，
  可用「N kind -> M targets」形式，比逐 target 列 dir 更精簡；R10b 實作可採此。

**R17. install 無 target 偵測失敗的錯誤訊息（F3，乙、高能見度）**
無 target 時 apm-go 印 `Error: no deployment target detected…` 後接**整包 Cobra flag 用法（14 行）**，
對「怎麼補 target」毫無幫助。對比 Python `[x] No harness detected` + 列出掃描過的 14 種 harness marker
（`.claude/`、`.cursor/`、`.github/copilot-instructions.md`…）+ 3 個具體修法 + apm.yml 範例。
apm-go 的 signal-detection **本來就掃過這些路徑**（資料已算出、只是沒印，與 R7/R11 同型），且此為
**使用者第一次設定最易踩的畫面、能見度極高**。要點：(a) 印結構化診斷（掃了哪些、怎麼修）；
(b) 該錯誤路徑抑制 Cobra usage dump（`SilenceUsage`）。錨點 `errNoDeployTarget()`（install.go）。

**R18. 純 local 安裝的 `.gitignore` 訊息（F5，乙、低優先）**
Python 純 local 安裝也印 `[i] Added apm_modules/ to .gitignore` 並建立檔；apm-go 無任何對應行為/訊息。
嚴重度低（無 `apm_modules/` 時該條目無實際作用）。記錄供判斷是否補；若補須先確認是「建立 .gitignore」（行為）
或「僅訊息」，避免越界動非輸出行為。

### 已知業務邏輯 bug（出本任務範圍，另立任務）
> 實測 plugin 安裝時發現的問題**非美化能解決**，明確劃出本任務範圍，避免用 ux 遮蔽真 bug。

- **BUG-1（最高優先，另立 resolver/manifest 任務）｜大小寫重複依賴**：
  `install chrome-devtools-mcp@…` 解析出 `chromedevtools/chrome-devtools-mcp` 與 `ChromeDevTools/chrome-devtools-mcp`
  **兩個** dep（同 repo、大小寫不同）→ `Resolved 2`（實際 1）、6 條 `shadowed…first-declared wins`
  （`internal/deploy/conflict.go`）、第二個 `deployed 0 files`、Summary 幽靈 bullet。
  根因：dep-key 未做 case-fold 正規化（resolver / manifest 層）。
  **⚠️ 嚴禁在 ux 層用灰化/隱藏去蓋掉衍生噪音**（shadow/0-files/幽靈 bullet）—— 那會掩蓋真 bug；
  修 BUG-1 後這些噪音會自動消失。本美化任務**不碰**此 bug。

### A/B 實跑補充（F1/F2/F4 —— 皆非 apm-go 需修的缺口，見 research/full-ab-parity-sweep.md）
- **F4（apm-go 優勢，須保留 → 對 R7/R10b/R12 的守衛）**：兩個不同 repo 內含同名 skill 碰撞時，apm-go
  偵測並印 `! …shadowed…` / `! …deployed 0 files…` 且 lockfile 正確歸屬單一 owner；**Python 靜默覆寫、
  lockfile 把同檔案雙重歸屬到兩個 dep（髒帳）**。apm-go 行為正確。**約束：R7/R10b/R12 做輸出精簡/樹聚合時，
  不得把 `shadowed`/`deployed 0 files` 衝突警告聚合掉/吞掉** —— 這是 apm-go 相對 Python 的資料完整性優勢。
- **F1（Python bug，勿抄）**：Python MCP runtime auto-detect fallback `["vscode"]` 誤觸 target 白名單、
  擋掉合法專案的 MCP 部署、錯誤訊息誤導。apm-go 無此問題（照 apm.yml 部署）。約束：apm-go 勿引入此設計。
- **F2（能力邊界，非 bug）**：`antigravity` target 為 apm-go 專屬擴充，Python 不支援 → `antigravity`/`design`
  fixture **無內容 parity 可比**（Python 直接 `Unknown target` abort），只能單邊評 apm-go 輸出品質。稽核勿誤當可逐字對照。

install 目前每次解析/部署**整個 manifest**（`result.Deps` 為全依賴圖，`install.go:608` `Resolved N`），
故輸出會含既存的 emilkowalski/skills。「是否只部署新增 delta」屬**業務邏輯**，**不在本 ux 任務範圍**；
ux 範圍內僅做「既存 vs 新增」的**呈現區分**（R9 / R10c）。若要改實際部署集合，另立業務層任務。

## Acceptance Criteria — v2.1
- [ ] R7：uninstall 摘要含套件名 + apm.yml 已更新 + 清理檔案數。
- [ ] R8a：`ux.BulletList` 輸出 `• ` 項目間有空格；R8b：全門面符號後固定單空格；R8c：符號寬度決策有書面記錄。
- [ ] R9 / R10c：既存 dep 以灰色呈現，install 摘要區分新增 / 既存。
- [ ] R10a：unpinned dep 標籤顯示短 hash fallback（`@<8碼>`）。
- [ ] R10b：部署樹每 kind 一行、目標目錄為 root、逗號列多 root；不再逐 skill 列出。
- [ ] R11：`install --mcp` 顯示已配置目標清單（用既有 `deployed`）+ apm.yml 絕對路徑 + `• ` 間隔。
- [ ] R12a：pack 正式執行印出檔案清單（同 dry-run）；R12b：local-bundle install 用聚合樹補檔案摘要。
- [ ] R12c/d：audit/frozen 成功細節走 `--verbose`（預設不洗版）；R12e：compile 標記出範圍、另立子任務。
- [ ] R13：install 主流程印出 MCP 部署摘要（server→targets，用既有 `MCPProvenance`）。
- [ ] R14：部署樹 local 節點標為 `<project root> (local)`；R15：summary 反映 MCP server 計數。
- [ ] R16：空 apm.yml + 有 local 部署時消除矛盾訊息（不印「No deps」+「Installed 0」的自相矛盾）。
- [ ] R17：install 無 target 時印結構化診斷（掃過的 marker + 修法）並抑制 Cobra usage dump。
- [ ] F4 守衛：R7/R10b/R12 輸出精簡/聚合**未**吞掉 `shadowed`/`deployed 0 files` 衝突警告。
- [ ] R18（次要）：純 local 安裝的 `.gitignore` 落差已記錄、決定補或不補。
- [ ] BUG-1（大小寫重複 dep）標記為出範圍、另立任務；本任務**未**用 ux 遮蔽其衍生噪音。
- [ ] exit code / `normalize` stdout 位元組不變；per-writer 去色不受影響；`-race` CI 驗證。
