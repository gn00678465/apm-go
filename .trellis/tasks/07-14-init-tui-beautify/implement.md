# 執行計畫 — init/stdout 美化（三階段，同一 task）

> 分支：`feat/init-tui-beautify`。每階段結尾有 review gate + 可獨立 build/test 驗證 + commit。
>
> **Gate 對抗審核機制（實測後定案）**：本機 codex read-only sandbox helper
> （`codex-windows-sandbox-setup.exe`）缺失 → `/codex:adversarial-review` companion runtime
> 雖能啟動，但 codex 內部仍需跑 shell 抓 diff 而失敗，回傳「無法審查」空結果 → **本機不可用**。
> 實際採用：`git diff [--cached|<base>...HEAD] | codex exec - -c model_reasoning_effort=medium`
> （diff 從 stdin 餵入，codex 不需跑 shell，繞過壞掉的 sandbox），prompt 採官方 adversarial-review
> 的對抗 stance。Phase A/B gate 已用此法驗證可用。

## Phase A — 基礎：internal/ux + 全域前綴

1. **加依賴** → verify: `go get charm.land/huh/v2@latest github.com/pterm/pterm && go mod tidy && go build ./...`
2. **`internal/ux` 骨架**：色票 / 符號 / TTY 偵測 / pterm printer / huh Theme + 門面函式（先 stub）
   → verify: `go build ./...`
3. **`internal/ux` 單元測試**：關色模式下 `Success/Info/Warn/Error/Table/BulletList/Tree` 純文字 golden；
   TTY=false 時 `Confirm/Input/MultiSelect` 回傳預設值 → verify: `go test ./internal/ux/... -v`（先紅後綠）
4. **全域前綴替換（全指令）**：install / compile / uninstall / update / marketplace / mcpinstall /
   audit / pack / experimental / marketplace_package / marketplace_authoring* / main(validate) 的
   完整前綴集 `[+] [i] [!] [warn] [>] [*] [x] [dry-run] [-]` → `ux.Success/Info/Warn/Error`。
   含 production 常數 `pack.go:535 licenseUndeclaredWarning`。
   **串流保留**：每處把原本的 `os.Stdout`/`cmd.OutOrStdout()`/`os.Stderr`/`cmd.ErrOrStderr()`
   原樣傳給 `ux.*` 的 `w`，不得把 stdout 結果行改到 stderr（見 design.md 串流契約）。
   → verify: production 檔無殘留 →
   `grep -rn -- '\[+\]\|\[i\]\|\[!\]\|\[warn\]\|\[>\]\|\[\*\]\|\[x\]\|\[dry-run\]\|\[-\]' cmd/apm-go internal --include='*.go' | grep -v '_test.go'` 應為空
4b. **同步更新測試斷言**（advisor 風險 #1）：`mcpinstall_test.go`、`marketplace_authoring_test.go`、
   `uninstall_local_survivor_test.go`、`install_test.go`、`pack_test.go` 等硬編碼舊前綴的
   `strings.Contains` 斷言改為新符號 → verify: `go test ./cmd/... ` 綠
- **【review gate A】** 輸出層 review + **`codex exec` 對抗式審核** 本階段 `git diff`（回報 CRITICAL/HIGH）
  → 修 CRITICAL/HIGH → `go test ./...` 綠 → **commit**

## Phase B — 主要指令結構化 + init 互動

5. **主要指令 pterm 結構化**：
   - marketplace list（`marketplace.go:374`）+ browse（`marketplace_browse_table.go`）→ `ux.Table`
     **（連帶：本步就更新 `marketplace_e2e_test.go:967-1012 TestMarketplaceBrowse_RendersPluginTable`
     的 `┃`/`│`/120 寬/`[>]`/`[i]` 斷言 —— golden 更新屬 Phase B 不是 Phase C，advisor 風險 #3）**
   - uninstall dry-run（`uninstall.go:575`）→ `ux.Section`+`ux.BulletList`
   - install 部署摘要（`install.go:1055` + `printDeploySummary` :1165）→ pterm TreePrinter；
     已安裝依賴清單（:1153）→ BulletList；順修複數 / 空 `@` tag / 型別贅字尾 `(s)`
   - update plan（`update.go:326`）→ Section；fetch/resolve 進度 → `ux.Spinner`
   → verify: `go build ./...`、手動 `go run ./cmd/apm-go marketplace list`
6. **init 互動改 huh**：
   - `promptWithDefault`（init.go:207）→ `ux.InputText`
   - `interactiveTargetSelect`（init.go:236）→ `ux.MultiSelect`（保留 detected 後綴；採 huh 預設鍵、
     放棄 all/none；移除孤兒 `parseToggleInput` init.go:316）
   - 建立前確認（init.go:117）→ pterm box 摘要 + `ux.Confirm`
   → verify: `go run ./cmd/apm-go init` 手動走 TTY；`init --yes` 走非互動
7. **其餘確認整併**：marketplace `readYesNo`/`confirmOrRequireYes`、mcp `promptReplaceMCP` → `ux.Confirm`；
   mcp `ttyAsk` 機密欄位 → `ux.Password`；清掉不再引用的 `golang.org/x/term`
   → verify: `go build ./...` + `go mod tidy`
- **【review gate B】** review + **`codex exec` 對抗式審核** 本階段 diff + `go test ./... -race` 綠 → **commit**

## Phase C — 子指令報表（Workflow 盤查補漏）

8. **指令專屬報表**：
   - audit --content（`audit_content.go:59`）→ 依 Severity 分色 BulletList + Spinner + stat
   - marketplace check（`marketplace_authoring.go:276`）→ BulletList + 通過率 stat
   - marketplace outdated（`marketplace_authoring.go:336`）+ experimental list（`experimental.go:25`）→ `ux.Table`
   - marketplace audit 兩層巢狀（`marketplace_authoring_audit.go:81`）→ pterm TreePrinter
   - marketplace migrate diff（`marketplace_authoring_migrate.go:34`）→ Section + +/- 上色
   - marketplace validate finding + Summary（`marketplace.go:610`）→ 依 level 上色 + 多欄 stat
   - pack dry-run（`pack.go:244`）→ BulletList；main validate diagnostics（`main.go:72`）→ Warning + BulletList
   → verify: 逐指令手動跑一次
9. **（選配）help/banner**：root 補 Long/Example 或自訂 cobra usage template → verify: `go run ./cmd/apm-go --help`
- **【review gate C】** 全量 `go test ./... -race`、`go vet ./...`、覆蓋率 ≥ 80%；更新受影響 golden/快照；
  **`codex exec` 對抗式審核完整 diff（`git diff main...HEAD`）** → 修 CRITICAL/HIGH → **commit** → **跑 40 項 checklist** → **task.py finish**

## Rollback points

- Phase A 依賴出問題：`git checkout -- go.mod go.sum`。
- Phase B huh 造成 CI hang：退 B 案，只改 `internal/ux` 互動實作，cmd 層不動。
- 三階段各自 commit，任一階段可獨立 revert。

## 非互動保證（每步都要守）

- `--yes` / 非 TTY / `NO_COLOR` / CI 路徑不得出現 spinner 動畫或阻塞式表單。
- 裝飾輸出全走 stderr；`normalize` 的 stdout 位元組不得改變。
