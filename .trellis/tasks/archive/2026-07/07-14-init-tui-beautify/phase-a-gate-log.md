# Phase A — Gate 記錄

## 完成內容
- Step 1 依賴：huh v2.0.3 / lipgloss v2.0.5 / pterm v0.12.83
- Step 2-3 `internal/ux` 門面（色票/符號/TTY 偵測/pterm printer/huh Theme/互動）+ 測試，覆蓋率 95.9%
- Step 4/4b 全指令括號前綴 → `ux.Success/Info/Warn/Error`，`[!]` 依語意分流，串流保留；同步更新測試斷言

## Gate A — codex exec 對抗式審核（3 輪）
- 環境障礙：codex Windows read-only sandbox helper 缺失 → 改用「git diff 從 stdin 餵入 + 預設 sandbox + medium reasoning」繞過。
- 第 1 輪：抓到 2 個 HIGH — (1) 著色只看 stdin，stdout 重導向會漏 ANSI；(2) os.ModeCharDevice 誤判 /dev/null。→ 修正為 per-writer 著色 + x/term.IsTerminal。
- 第 2 輪：抓到 1 個 HIGH — per-call 翻轉 pterm 全域旗標與 spinner 背景 goroutine 產生 data race。→ 改為 Init 設定一次 + pterm.RemoveColorFromString 去色，天生無 race。
- 第 3 輪：**無 CRITICAL/HIGH，通過 gate A**。

## 已知環境限制
- `-race` 本機無法跑（無 gcc、CGO_ENABLED=0）；設計上無 race，需 CI 補驗 `go test ./... -race`。

## 驗證
- go build ./... / go vet ./... 通過；go test ./...（含 internal/ux）全綠。
