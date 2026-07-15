# Phase B/C + 最終 gate 記錄

## Commit 結構（branch feat/init-tui-beautify）
- 436d15d docs — 規劃文件 + Phase A gate 記錄
- 524a46e feat(ux) — Phase A：internal/ux 門面 + 全域前綴
- 84cd1c6 feat(cmd) — Phase B：init huh 互動 + 主要指令結構化
- 93b5377 feat(cmd) — Phase C：子指令報表
- d7b4383 fix(init) — 最終 HIGH 修正：統一互動偵測 ux.CanPrompt

## Gate 對抗審核（codex，git diff | codex exec - + 官方 adversarial stance）
註：/codex:adversarial-review companion runtime 因本機 codex read-only sandbox
helper 缺失、codex 內部無法跑 shell 抓 diff 而回傳空結果 → 本機不可用；改用
stdin 餵 diff 的方式（Phase A/B 已驗證），繞過壞掉的 sandbox。

- Gate A：3 輪，抓修 3 HIGH（per-writer 著色、x/term TTY 偵測、spinner 全域旗標 race）→ 通過
- Gate B：2 輪，抓修 3 HIGH（init 錯誤無限遞迴、CanPrompt 解耦互動/著色、還原真實非互動安全測試）→ 通過
- Gate C：1 輪，無 CRITICAL/HIGH（exit code / counts / 串流 / refcheck 對應皆正確）→ 通過
- 最終 holistic（全 branch）：抓修 1 HIGH（init 用不一致 TTY 偵測導致非互動靜默同意）→ 複審通過『branch 可出貨』

## 最終驗證證據
- go build ./... : exit 0
- go vet ./... : exit 0
- go test ./... : 24 套件全綠、無 FAIL/panic
- 覆蓋率：cmd/apm-go 83.5%、internal/ux 90.1%（均 ≥ 80%）
- go test -race：**本機無法驗證**（CGO_ENABLED=0、無 gcc）→ 設計上無 race（Init 後不再變動 pterm 全域旗標），需 CI 補跑 `go test ./... -race`

## 已知範圍邊界（遵循 prd「只動輸出與互動層，不改業務邏輯」）
- internal/manifest·marketplace/source·marketplace/build·pack/bundle·pack/pluginmanifest
  的 library 層直印警告維持原前綴（未 import ux）。若要一併統一，需 diagnostics-return
  重構，屬額外 scope（Phase C agent 曾越界 import ux，已回退）。

## 決策定案
- 策略 A（pterm 輸出 + huh 互動，兩相依）
- browse box table 遷移 pterm.Table（放棄 rich parity）
- init 多選採 huh 預設鍵（放棄 all/none，移除 parseToggleInput）
- 全指令 26 點分三階段同一 task
- 互動偵測統一 ux.CanPrompt（stdin+stderr TTY 且非 CI，排除 NO_COLOR）
