# 驗證 Checklist — init/stdout 美化（huh + lipgloss）

> **2026-07-15 對齊**：本 checklist 原為 pterm 版；pivot 至 charmbracelet（lipgloss + huh）後，
> 僅**函式庫機制相關**的項目（A1 依賴、A3 簽章、B2 表格、X1 門面 grep）已就地換為 lipgloss，
> **所有 PASS/FAIL 行為判定邏輯不變**。Phase A/B/C 標號對應 implement.md 的 Phase 1/2/3
> （1=重寫 internal/ux、2=套用 cmd、3=收斂驗證），僅為分組，behavioural 項目不受影響。

## 用途與原則

本 checklist 由**未參與規劃的獨立 reviewer** 建立，目的是在 `feat/init-tui-beautify`
三階段（A/B/C）實作過程中與完成後，提供**可獨立執行、二元判定（PASS/FAIL）**的驗收依據，
避免「大致上沒問題」「看起來符合預期」這類主觀陳述通過驗收。

**原則**：
1. 每一項都必須有明確的判定方式——一個可以直接貼到終端機執行的指令，或一個具體可觀察的
   檔案/輸出狀態；不接受「目視覺得可以」作為唯一判定依據。
2. 每一項都綁定 `prd.md` / `design.md` / `implement.md` 的具體條款，或實際程式碼的
   `檔案:行號`。
3. 每一項都寫明 FAIL 條件——什麼情況算沒過，不留模糊空間。
4. 本檔案的行號引用以 2026-07-14 審視當下的程式碼快照為準；若實作過程中檔案已變動，
   以「同一函式/同一輸出語意」為準重新定位，而非因行號漂移就跳過該項。

---

## 審查者附註：已知的計畫落差（實作前必讀）

以下不是 checklist 項目，是本次獨立審視發現、需要在實作規劃中明確處理的落差，
已同步反映到下方對應的 checklist 項目中：

- **既有測試硬編碼舊前綴字串**：至少 `mcpinstall_test.go:722`、
  `marketplace_authoring_test.go`（多處 `[x]`/`[+]`/`[!]`/`[i]`）、
  `uninstall_local_survivor_test.go:402`（`[dry-run]`）、
  `marketplace_e2e_test.go:988/995`（`[>]`/`[i]` 且鎖了 box-drawing 字元 `┃`/`│`
  與 120 字寬換行）、`install_test.go:1401`、`pack_test.go:886` 都用
  `strings.Contains` 或字面常數鎖定舊前綴/舊表格格式。`pack.go:535` 的
  `licenseUndeclaredWarning` 常數本身就內嵌 `"[warn] ..."` 文字，是 production
  code 直接依賴的字面值。這代表「取代舊前綴符號」與「既有測試維持綠燈」在沒有同步
  修改這些測試斷言的情況下**必然衝突**；`implement.md` 沒有把「同步改測試斷言」列為
  任何一步的具體動作，只掛在 commit gate 的「go test ./... 綠」上，容易低估工作量。
- **stdout/stderr 契約只明文保護 `normalize`**：但 `marketplace add/list/validate/
  check/outdated/migrate/package add-set-remove`、`pack`、`install` 摘要
  （`install.go:1153/1165`）、`uninstall` 摘要（`uninstall.go:607-611`）目前都用
  `cmd.OutOrStdout()` 或裸 `fmt.Println` 把**最終結果行**（非裝飾）寫到 stdout，
  不是 stderr。若 Phase A 機械式把 `[+]/[i]/[!]/[warn]` 換成
  `ux.Success/Info/Warn`（design.md 定義其預設 writer 為 stderr），會把這些指令
  現有的 stdout 結果行誤搬到 stderr，對任何用 stdout 捕捉輸出的腳本是破壞性變更，
  但驗收條件只明文保護了 `normalize` 一個指令。
- **`[x]` 前綴未被 PRD 的舊前綴列舉涵蓋**：Requirement #2 只列 `[+] [i] [!] [warn]
  [>] [*]`，但 `marketplace_authoring.go:276`（package check 失敗行）與其測試都用
  `[x]`；`implement.md` Phase A 的驗證 grep 指令本身也不完整（範例只寫
  `\[+\]\|\[i\]\|\[warn\]`，漏了 `[!]`、`[>]`、`[*]`，更完全漏掉 `[x]`、
  `[dry-run]`、`[-]`），照抄該指令會得到假的 PASS。
- **`marketplace browse` 表格遷移會確定打掉一支既有 e2e 測試**：
  `TestMarketplaceBrowse_RendersPluginTable`（`marketplace_e2e_test.go:967-1012`）
  直接斷言 box-drawing 字元 `┃`/`│`、120 字寬換行行為，以及 `[>]`/`[i]` 前綴。
  這支測試需要更新的事實在 Phase B 步驟 5（browse 遷到 `pterm.Table`）就會發生，
  但 implement.md 只在 Phase C 的 review gate 提到「更新受影響 golden/快照」，
  容易被誤以為是 Phase C 才要處理。

---

## Phase A — 基礎：internal/ux + 全域前綴替換

- [ ] **A1. 依賴加入且可編譯**
  判定：`go get charm.land/huh/v2 charm.land/lipgloss/v2 && go mod tidy && go build ./...`
  全部指令 exit code 為 0。
  FAIL：任何一步非 0，或 `go.mod` 未包含 `huh` 與 `lipgloss`
  （`grep -E "huh|lipgloss" go.mod` 應各至少一筆）；或 go.mod 殘留 `pterm`。

- [ ] **A2. `internal/ux` package 存在**
  判定：`Glob internal/ux/**/*.go` 有結果；`go build ./internal/ux/...` exit code 0。
  FAIL：目錄不存在，或編譯失敗。

- [ ] **A3. `internal/ux` 對外契約符合 design.md 門面契約（第 18-61 行）**
  判定：`grep -n "^func " internal/ux/*.go`，逐一核對存在（**per-writer：訊息/結構化函式第一參數為
  `w io.Writer`**）：
  `Init()`、`IsRich() bool`、`CanPrompt() bool`、
  `Success/Info/Warn/Error(w io.Writer, format string, a ...any)`、
  `Table(w io.Writer, headers []string, rows [][]string)`、`BulletList(w io.Writer, items []Item)`、
  `Tree(w io.Writer, root TreeNode)`、`Section(w io.Writer, title string)`、
  `Box(w io.Writer, title string, body []string)`、`Diff(w io.Writer, diffText string)`、
  `Spinner(w io.Writer, text string) *Spin`、
  `Confirm(prompt string, def bool) (bool, error)`、
  `InputText(label, def string) (string, error)`、
  `Password(label string) (string, error)`、
  `MultiSelect(title string, opts []Option) ([]string, error)`、
  `InputForm(title string, fields []Field) (map[string]string, error)`。
  FAIL：任一函式簽名缺漏或簽名不符（尤其訊息/結構化函式**漏掉 `w io.Writer` 首參**，或非
  CanPrompt 互動函式未回傳預設值語意）。

- [ ] **A4. 關色模式（NO_COLOR / CI / 非 TTY）純文字 golden 測試存在且通過**
  判定：`go test ./internal/ux/... -run Golden -v`（或等效測試名）；輸出斷言中不含
  任何 ANSI escape（`\x1b[`），可用
  `go test ./internal/ux/... -v 2>&1 | grep -P "\x1b\["` 應為空驗證。
  FAIL：測試不存在、測試失敗，或 golden 輸出中偵測到 ANSI escape sequence。

- [ ] **A5. 非 TTY 下互動函式回傳預設值而非阻塞**
  判定：針對 `Confirm/InputText/MultiSelect` 各寫一支單元測試，在
  `Init` 偵測為非 TTY（例如 stdin 重導向自 `/dev/null` 或 `os.Pipe` 不給輸入）情境下呼叫，
  斷言在 1 秒內回傳且值等於呼叫端傳入的預設值。
  判定指令：`go test ./internal/ux/... -run NonTTY -timeout 5s`。
  FAIL：測試逾時（代表真的呼叫了會阻塞的 huh 表單），或回傳值不等於預設值。

- [ ] **A6. `internal/ux` 覆蓋率 ≥ 80%**
  判定：`go test ./internal/ux/... -cover` 輸出的 `coverage: X% of statements` 中 X ≥ 80。
  FAIL：X < 80，或指令因編譯錯誤未能輸出覆蓋率。

- [ ] **A7. 舊前綴符號全滅（擴充版 grep，涵蓋 `implement.md` 遺漏的 `[x]`/`[dry-run]`/`[-]`）**
  判定：
  ```
  grep -rn '\[+\]\|\[i\]\|\[!\]\|\[warn\]\|\[>\]\|\[\*\]\|\[x\]' cmd/apm-go --include=*.go | grep -v _test.go
  ```
  應為空（僅檢查非測試檔的 production 輸出行；`_test.go` 內殘留視為 A8 的責任）。
  FAIL：任何一個 production `.go` 檔（非 `_test.go`）內還有上述任一 pattern 出現在
  `fmt.Print*`/`fmt.Fprint*` 呼叫的字串字面值中。

- [ ] **A8. `[dry-run]` / `[-]` 的去留有明確、書面決定**
  判定：檢查 `design.md` 或 `implement.md` 是否新增一段文字，明確說明 `[dry-run]` 前綴
  （`uninstall.go:575/587/592/600`、`pack.go:243`）與 `[-]` 前綴
  （`marketplace.go:555`、`marketplace_package.go:259`）是否納入本次符號替換範圍。
  FAIL：程式碼中仍存在 `[dry-run]` 或 `[-]` 字面前綴，且規劃文件中找不到「這是刻意保留、
  不在本次範圍」或「已改為新符號」的任一書面說明。

- [ ] **A9. 既有測試斷言同步更新，`go test ./...` 全綠**
  判定：`go test ./... -race` exit code 0；並且針對下列已知會受影響的測試逐一確認其
  斷言字串已更新為新符號（而非測試被刪除/弱化來規避失敗）：
  `cmd/apm-go/mcpinstall_test.go:722`、
  `cmd/apm-go/marketplace_authoring_test.go`（`[x]`/`[+]`/`[!]`/`[i]` 相關斷言）、
  `cmd/apm-go/install_test.go:1401`（`wantAllowExecutablesWarning`）、
  `cmd/apm-go/pack_test.go:886`（`wantLicenseUndeclaredWarning`）、
  `cmd/apm-go/pack.go:535`（`licenseUndeclaredWarning` 常數本身的前綴文字）。
  判定指令：`git diff --stat <phaseA起點>..HEAD -- cmd/apm-go/*_test.go` 應能看到上述
  檔案有變更。
  FAIL：`go test ./...` 未全綠；或上述任一測試檔案在 diff 中完全沒有變更卻仍然通過
  （代表測試斷言本來就沒鎖定舊前綴，需人工複查是否測試本身已被繞過，例如斷言被刪除或
  改成不檢查前綴內容）。

- [ ] **A10. review gate A 前的 commit 邊界**
  判定：`git log --oneline -1` 對應的 commit 只包含 `internal/ux/**`、`cmd/apm-go/*.go`
  的輸出行變更、`go.mod`/`go.sum`；`git show --stat HEAD` 檢查有無誤觸業務邏輯檔案
  （例如 `internal/deploy`、`internal/lockfile` 等非輸出層 package）。
  FAIL：commit 內出現非輸出層/非依賴宣告的業務邏輯檔案變更，且沒有對應的必要性說明。

---

## Phase B — 主要指令結構化 + init 互動改 huh

- [ ] **B1. `marketplace list` 改用 `ux.Table` 且欄位資料不變**
  判定：`go run ./cmd/apm-go marketplace list`（先 `marketplace add` 至少一筆）人工比對
  欄位（NAME/SOURCE/REF/[HOST]/PATH）內容與 Phase A 之前的輸出資料值一致，只有邊框/顏色改變。
  FAIL：任何一筆資料值（不含表格邊框字元）與美化前不同，或欄位順序改變。

- [ ] **B2. `marketplace browse` 遷至 `lipgloss/table` 且對應測試已更新**
  判定：`go test ./cmd/apm-go/... -run TestMarketplaceBrowse -v`。
  FAIL：`TestMarketplaceBrowse_RendersPluginTable`（`marketplace_e2e_test.go:967`）
  仍斷言舊 box-drawing 字元 `┃`/`│` 卻測試失敗（代表測試沒同步更新）；或該測試被直接刪除
  而非改寫（用 `git diff` 確認測試函式簽名 `TestMarketplaceBrowse_RendersPluginTable`
  仍存在於檔案中）。

- [ ] **B3. `uninstall --dry-run` 改用 `ux.Section`+`ux.BulletList`，資訊完整度不變**
  判定：`go test ./cmd/apm-go/... -run TestUninstall.*[Dd]ry[Rr]un -v`；額外人工執行
  `go run ./cmd/apm-go uninstall <pkg> --dry-run` 確認仍列出：欲移除的
  `apm.yml` 條目、transitive orphans、`apm_modules` 各路徑的 exists/missing 狀態
  （對應 `uninstall.go:575-599` 原始四個區塊）。
  FAIL：任一區塊（移除清單/orphans/apm_modules 狀態）在美化後從輸出中消失。

- [ ] **B4. install 部署摘要改 TreePrinter，且複數/空 tag/贅字尾修正生效**
  判定：`go run ./cmd/apm-go install <單一套件>`，人工確認：
    (a) 恰好 1 個 dependency 時輸出 `1 dependency`（非 `1 dependencies`）；
    (b) `ResolvedTag` 與 `ResolvedRef` 皆為空時，摘要不出現空字串 `@`（例如不應輸出
        `pkg@ (depth 0)`，對應 `install.go:1155-1157` 的 fallback 邏輯需覆蓋此邊界）；
    (c) 型別贅字尾 `(s)` 不再出現在單數情境（若原輸出有 `file(s)`/`director(s)`
        這類寫法，改為依實際數量單複數判斷，對照 `install.go:1165` 起的
        `printDeploySummary` 與 `uninstall.go:611` 的 `pluralYIES`）。
  判定指令：可寫一支表格驅動單元測試覆蓋 count=0/1/2 三種情況，
  `go test ./cmd/apm-go/... -run TestInstall.*[Ss]ummary -v` 全綠。
  FAIL：count=1 時仍輸出複數形式；或空 tag 情境輸出裸 `@`；或無對應單元測試覆蓋
  count=1 邊界（只手動跑一次不算數，需有可重複執行的自動化測試）。

- [ ] **B5. update plan 改 Section，fetch/resolve 進度改 Spinner**
  判定：`NO_COLOR=1 go run ./cmd/apm-go update` 在無變更套件時仍輸出
  `"Already up to date"`（對照 `install.go:1124-1126` 的既有 no-op 分支邏輯，
  update 走相同 `deployAndFinalize`）；有變更時仍輸出 `printUpdateSummary`
  （`update.go:326`）原有的 heading 語意（現況為 `"[i] Update plan for apm.yml"`，
  美化後應為新符號但語意相同的一行 heading）。
  FAIL：no-op 情境的 `"Already up to date"` 文字消失或改變；或有變更時 heading 整行消失
  （C2 zero-target gate 依賴這行先出現，見 `update.go:322-324` 註解）。

- [ ] **B6. init 互動流程在真 TTY 下呈現 huh 表單**
  判定：於支援虛擬終端的環境（例如 `winpty`/`script` 包一層，或直接在互動式終端機手動執行）
  跑 `go run ./cmd/apm-go init`，人工觀察：
    (a) metadata 輸入（name/version/description/author）呈現 huh Input 表單樣式；
    (b) Target 多選呈現 huh MultiSelect，用 space 逐項切換（非數字輸入 `1-3` 或 `all`/`none`）；
    (c) 建立前確認呈現 huh Confirm。
  FAIL：任一步驟仍是舊的 `bufio.Scanner` 純文字問答（例如仍看到
  `promptWithDefault`/`confirmPrompt` 的 `"%s [%s]: "` 格式提示）。

- [ ] **B7. `init --yes` 與非 TTY 下不呈現任何表單、不阻塞**
  判定：
  ```
  cd <空目錄> && go run ./cmd/apm-go init --yes < /dev/null
  echo "exit=$?"
  ```
  以及
  ```
  cd <空目錄> && echo | go run ./cmd/apm-go init < /dev/null
  ```
  各自加上 `timeout 10s`（PowerShell 下用 `Start-Process ... -Wait -Timeout 10`）。
  FAIL：任一指令執行超過 10 秒未結束（代表誤入互動表單阻塞等待 stdin）；或 exit code 非 0
  且非預期的驗證錯誤。

- [ ] **B8. `parseToggleInput`（init.go:316）已移除**
  判定：`grep -n "func parseToggleInput" cmd/apm-go/init.go` 應為空；
  `go build ./...` 綠（確認沒有殘留呼叫端造成的編譯錯誤）。
  FAIL：函式仍存在，或函式已改名但邏輯（`1-3` 範圍語法/`all`/`none` 批次切換）
  仍可從 CLI 觸發到。

- [ ] **B9. `readYesNo`/`confirmOrRequireYes`/`promptReplaceMCP` 整併為 `ux.Confirm`**
  判定：`grep -n "func readYesNo\|func confirmOrRequireYes" cmd/apm-go/marketplace.go`、
  `grep -n "func promptReplaceMCP" cmd/apm-go/mcp_prompt.go` ——三者要嘛函式體內部改呼叫
  `ux.Confirm`，要嘛函式本身被移除、呼叫端改直接呼叫 `ux.Confirm`。
  判定方式：`go vet ./...` 綠 + 人工讀 diff 確認呼叫鏈最終落在 `ux.Confirm`。
  FAIL：三者中任一個仍是獨立的 `bufio.Scanner`/`fmt.Scanln` 手刻問答邏輯，未收斂到 `ux.Confirm`。

- [ ] **B10. MCP 機密輸入改用 huh Password**
  判定：`grep -n "func ttyAsk" cmd/apm-go/mcp_prompt.go` 讀函式體，確認 `secret=true`
  分支呼叫 `ux.Password`（而非沿用 `golang.org/x/term.ReadPassword`）。
  FAIL：仍直接呼叫 `golang.org/x/term.ReadPassword` 或其他非 `ux.Password` 的隱藏輸入實作。

- [ ] **B11. 不再引用的 `golang.org/x/term` 已清除**
  判定：
  ```
  grep -rn "golang.org/x/term" cmd/apm-go internal
  go mod tidy && git diff go.mod go.sum
  ```
  FAIL：`grep` 仍有 import 命中卻 `go.mod` 未列出該依賴（不一致狀態）；或
  `x/term` 已無任何 import 命中，但 `go.mod` 的 `require` 區塊仍列著它
  （`go mod tidy` 後應自動清除，若沒清除代表 B10 尚未完全生效或有其他隱藏引用）。

- [ ] **B12. review gate B：`go test ./... -race` 全綠**
  判定：`go test ./... -race` exit code 0，且輸出不含 `DATA RACE` 字樣。
  FAIL：任何一個 package 測試失敗，或偵測到 data race。

- [ ] **B13. Phase B 的 stdout/stderr 流向沒有意外改變**
  判定：針對下列「目前輸出結果行本來就在 stdout」的呼叫點，各跑一次指令並確認結果行
  仍出現在 `1>` 導出的 stdout 檔而非 `2>` 導出的 stderr 檔：
  `marketplace list`（`marketplace.go:374` 起的表格本體）、
  `marketplace validate`（`marketplace.go:616-619` 的 finding 與 Summary 行）、
  `install`（`install.go:1153` 起的 `Installed N dependencies` 摘要）、
  `uninstall --dry-run`（`uninstall.go:575-600`）、
  `update`（`update.go:351` 起的 plan heading + 明細行）。
  判定指令範例：
  ```
  go run ./cmd/apm-go marketplace list 1>out.txt 2>err.txt
  grep -q "NAME" out.txt   # 表格本體仍在 stdout
  ```
  FAIL：任一原本在 stdout 的結果行，美化後只出現在 `err.txt` 而不在 `out.txt`
  （代表被誤導向 `ux.*` 預設的 stderr writer，且規劃文件中沒有明確記錄「此指令的結果行
  刻意改走 stderr」這項決策）。

---

## Phase C — 子指令報表（Workflow 24-agent 盤查補漏）

- [ ] **C1. `audit --content` 依 Severity 分色 + Spinner + stat 摘要，exit code 語意不變**
  判定：對照 `audit_content.go:43-76` 的三段 exit code 邏輯寫三支測試（或確認既有測試
  `audit_content_test.go` 已覆蓋）：
    - 0 個 finding 或僅 info → exit 0（`audit_content.go:53-56`, `73-76`）
    - 至少 1 個 critical → exit 1（`audit_content.go:64-67`）
    - 有 warning、無 critical → exit 2（`audit_content.go:68-71`）
  判定指令：`go test ./cmd/apm-go/... -run TestAuditContent -v`，並加一行
  `echo $LASTEXITCODE`（PowerShell）/`echo $?`（bash）在對應情境的手動執行後核對。
  FAIL：任一情境 exit code 與上述不符；或分色只套用在文字上卻改變了 `all` 陣列的排序/
  內容判斷邏輯，導致 `hasCritical`/`counts` 計算路徑被連帶改動。

- [ ] **C2. `marketplace check` 改 BulletList + 通過率 stat，`[x]` 失敗行語意不變**
  判定：`go test ./cmd/apm-go/... -run TestMarketplaceCheck -v`；核對
  `marketplace_authoring.go:270-287` 的 `failed`/`results` 計數邏輯未被觸碰，
  只有輸出呈現方式改變。
  FAIL：`failed > 0` 時仍應回傳 `fmt.Errorf("check failed: %d/%d ...")` 這個 error
  文字語意（供上層 exit code 判斷），若這段訊息被移除或 error 判斷邏輯被連帶改掉即 FAIL。

- [ ] **C3. `marketplace outdated` + `experimental list` 改 `ux.Table`，資料值不變**
  判定：`go run ./cmd/apm-go marketplace outdated`（先造至少一筆有/無 upgrade 的套件）、
  `go run ./cmd/apm-go experimental list`，人工比對欄位資料
  （`marketplace_authoring.go:330-350` 的 Package/Current/latest-in-range/latest/note；
  `experimental.go:20-26` 的 name/status/description）與美化前一致。
  FAIL：任何一筆資料值消失或錯位；`outdated` 的 exit code（有 upgradable 時非 0，
  對照 `marketplace_authoring.go:352-355`）改變。

- [ ] **C4. `marketplace audit` 兩層巢狀改 TreePrinter，bypass 明細不遺漏**
  判定：`go test ./cmd/apm-go/... -run TestMarketplaceAudit -v`；核對
  `marketplace_authoring_audit.go:68-97` 的四個分支（`FetchOK` 有/無 issue、
  `FetchNoManifest`/`FetchUnsupportedSource`、其他 unverifiable）在 TreePrinter 下
  仍各自可見，特別是每個 bypass issue 底下的兩行明細
  （`dep` 與 `hint: suggestion`，`marketplace_authoring_audit.go:82-84`）沒有被摺疊消失。
  FAIL：任一 bypass issue 的 `dep`/`hint` 明細在新輸出格式下遺失或無法從輸出文字中找到。

- [ ] **C5. `marketplace migrate` dry-run diff 改 Section + +/- 上色，diff 內容位元組不變**
  判定：`go run ./cmd/apm-go marketplace migrate --dry-run`，比對輸出中 diff 本體
  （`marketplace_authoring_migrate.go:34-39` 的 `diff` 字串）美化前後逐行內容一致，
  只允許新增顏色 ANSI code 或前綴符號，不允許 diff 行本身的 `+`/`-`/內容字元改變。
  判定方式：`NO_COLOR=1` 下跑一次，用 `diff` 指令比對美化前後兩次輸出的純文字版本。
  FAIL：`NO_COLOR=1` 下純文字版本與美化前不完全一致（逐字元比對）。

- [ ] **C6. `marketplace validate` finding + Summary 依 level 上色，Summary 數字不變**
  判定：`go run ./cmd/apm-go marketplace validate <name>`，核對
  `marketplace.go:610-619` 的 `Summary: %d passed, %d warnings, %d errors` 三個數字
  與美化前一致；`errs > 0` 時仍回傳 non-nil error（`marketplace.go:620-622`）。
  FAIL：三個數字任一不符；或 `errs > 0` 卻 exit code 變成 0。

- [ ] **C7. `pack` dry-run 改 BulletList，`main validate` diagnostics 改 Warning+BulletList**
  判定：`go run ./cmd/apm-go pack --dry-run`（`pack.go:242-247`）人工確認每個檔案路徑
  仍逐行列出，數量與美化前一致；
  `go run ./cmd/apm-go validate <含至少一個 diagnostic 的檔案>`（`main.go:64-75`）
  確認每個 `diags` 的 `Message` 仍逐行可見，且 `main.go:65` 的
  「`lockfile_version` 存在時直接 return nil，跳過 diagnostics」這條分支邏輯不變
  （即：對 lockfile 格式的檔案跑 validate 不應輸出任何 diagnostics 相關文字）。
  FAIL：任一檔案路徑/diagnostic 訊息在美化後從輸出中消失；或 lockfile 分支意外印出
  diagnostics 相關文字。

- [ ] **C8. `normalize` stdout 位元組輸出完全不變（硬性 byte-diff）**
  判定：
  ```
  go run ./cmd/apm-go normalize <fixture.yaml> > before.out   # Phase A 之前跑一次存底
  go run ./cmd/apm-go normalize <fixture.yaml> > after.out    # Phase C 完成後再跑一次
  diff before.out after.out   # 或 cmp before.out after.out
  ```
  對至少 2 個 fixture（一個含 anchors/aliases、一個含多層巢狀 mapping）各跑一次。
  FAIL：`diff`/`cmp` 回報任何差異（含結尾換行符差異）。這是 PRD 唯一明文的
  「stdout 位元組不得改變」條款（prd.md:54），任何差異都是 FAIL，沒有「差異很小可接受」
  這種例外。

- [ ] **C9. 全量測試 + 靜態檢查 + 覆蓋率**
  判定：
  ```
  go build ./...
  go vet ./...
  go test ./... -race -cover
  ```
  三個指令都要 exit code 0；`internal/ux` 那一行 coverage ≥ 80%。
  FAIL：任一指令非 0；或 `internal/ux` 覆蓋率 < 80%。

- [ ] **C10. 三階段各自可獨立 revert**
  判定：`git log --oneline` 確認三個 commit 邊界清楚對應 Phase A/B/C；對最新一個
  Phase 的 commit 執行 `git revert --no-commit <該commit的sha>` 後跑
  `go build ./... && go test ./...`，確認能回到「上一 Phase 完成」的可編譯可測試狀態
  （驗證完成後 `git revert --abort` 還原，不留下這次演練的痕跡）。
  FAIL：`git revert` 產生無法自動解決的衝突，或 revert 後專案無法編譯/測試。

- [ ] **C11. 非互動保證跨全指令複查（implement.md 第 58-61 行）**
  判定：對 Phase C 新增/修改到的每個子指令（`audit --content`、
  `marketplace check/outdated/audit/migrate/validate`、`pack --dry-run`）各跑一次：
  ```
  NO_COLOR=1 CI=true go run ./cmd/apm-go <cmd> ... < /dev/null
  ```
  並用 timeout 包裹（10 秒）。
  FAIL：任一指令逾時未結束，或輸出中仍出現 ANSI color code
  （`grep -P "\x1b\["` 有命中）或 spinner 動畫殘留字元（如 `⠋⠙⠹` 等 braille 字元）。

---

## 跨階段總驗收（三階段全部完成後）

- [ ] **X1. `internal/ux` 是唯一輸出/互動門面**
  判定：`grep -rn "huh\.\|lipgloss\." cmd/apm-go/*.go` 應為空（`_test.go` 除外）——
  `cmd/apm-go` 下的業務程式碼不得直接 import/呼叫 `huh`/`lipgloss`，一律經
  `internal/ux`。
  FAIL：任何 `cmd/apm-go` 下的非測試檔直接呼叫 `huh.*`/`lipgloss.*` API。

- [ ] **X2. 舊前綴符號在全專案範圍內清零（含 acceptance criteria 明列的六個家族）**
  判定：
  ```
  grep -rln '\[+\]\|\[i\]\|\[!\]\|\[warn\]\|\[>\]\|\[\*\]\|\[x\]' cmd/apm-go --include=*.go | grep -v _test.go
  ```
  應為空，且明確逐一確認涵蓋 prd.md Requirement 7 列出的家族：
  `audit.go`、`audit_content.go`、`pack.go`、`marketplace_authoring*.go`、
  `marketplace_package.go`、`experimental.go`、`main.go`。
  FAIL：上述任一家族的檔案仍有命中。

- [ ] **X3. 裝飾輸出全走 stderr，機器可讀輸出（`normalize`）不受影響**
  判定：對每個有互動/裝飾輸出的指令執行
  `go run ./cmd/apm-go <cmd> ... 2>/dev/null`，確認裝飾性文字（symbol/color/表格框線）
  消失只剩「指令本來就該在 stdout 的資料」（例如 `normalize` 的 YAML、
  `marketplace list` 的表格資料列——若 X3 與 B13/C8 判定 marketplace list 資料本體
  應留在 stdout，則此處應仍看得到資料列，只是沒有顏色）。
  FAIL：`2>/dev/null` 後任何指令的**資料本體**（非裝飾符號）也消失，代表資料行被誤寫進
  stderr。

- [ ] **X4. 既有 exit code 全部不變（audit / audit --content / marketplace validate --strict 等）**
  判定：對下列指令在美化前後各跑一次，比對 exit code 完全相同：
  `apm-go audit`（干淨/有問題兩種情境）、
  `apm-go audit --content`（0/1/2 三種情境，見 C1）、
  `apm-go marketplace validate --strict`（有/無 error 兩種情境）、
  `apm-go marketplace package add/remove/set`（成功/編輯失敗兩種情境，
  對照 `marketplace_package.go` 註解提到的「非 guard 錯誤路徑固定 exit 2」）。
  判定指令：`echo "exit=$?"`（bash）/`echo "exit=$LASTEXITCODE"`（PowerShell）
  逐一記錄，前後比對。
  FAIL：任一情境 exit code 與美化前不同。

- [ ] **X5. Workflow 24-agent 盤查補漏的七個子指令家族全數覆蓋（prd.md Requirement 7）**
  判定：checklist C1-C7 逐一對應打勾；額外用一份表格核對 prd.md 第 33-41 行列出的
  七個項目（audit/audit --content、pack、marketplace 家族四個子指令、
  marketplace package、experimental list、apm-go validate）與已完成項目一一對應。
  FAIL：七個項目中有任何一個找不到對應的已驗證 checklist 項目。

- [ ] **X6. 三階段合併後的最終 `go test ./... -race -cover` 一次性全綠**
  判定：從乾淨的 `feat/init-tui-beautify` 分支（三個 Phase commit 都在）跑
  `go test ./... -race -cover`。
  FAIL：任何失敗、任何 race、或整體 coverage 相較美化前的 baseline 下降
  （用 `go test ./... -cover` 美化前後兩次輸出比對總覆蓋率百分比）。

- [ ] **X7. `codex exec` 對抗式審核（每個 review gate 皆執行，最終 gate 審完整 diff）**
  判定：每階段 commit 前對該階段 `git diff` 跑 `codex exec`（對抗式審核提示，聚焦
  串流誤搬、exit code 改變、殘留舊前綴、測試斷言遺漏）；最終對 `git diff main...HEAD` 跑一次。
  FAIL：codex 回報任何 CRITICAL/HIGH 未被處理（修掉或明確記錄為誤報並說明理由）。

---

**總計項目數：以上共 41 項可獨立勾選的 PASS/FAIL 判定項（Phase A 10 項、Phase B 13 項、
Phase C 11 項、跨階段總驗收 7 項）。含 X7 = `codex exec` 對抗式審核關卡。**
