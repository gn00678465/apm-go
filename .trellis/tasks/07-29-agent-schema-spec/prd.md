# 各 target agent 的 marketplace/plugin schema：spec 文件 + 可執行定義

> Child of `07-28-marketplace-plugin-parity`。
> 來源需求：`requirements-trace.md` 的 **U8**。

## Goal

把「各 target agent 的 marketplace/plugin field schema」從 task 局部的研究紀錄，
升級為**一份正式 spec 文件 + 一組可執行的 schema 定義**，並建立
**防漂移機制**確保兩者永遠一致。

## 需求來源與解讀確認

> 使用者原話（`新文件 8.txt` 第 1 行）：
> 「建立各個 target agent 的 marketplace/plugin field 的 schema」
>
> 使用者裁定（2026-07-29）：
> 「建立一分獨立的 spec 文件 + 可執行的 schema 定義，**兩者必須同步**」

先前我把它解讀成「研究並記錄」，產出 `research/agent-schema-support-matrix.md`
後就當作處理完畢，**零個 AC 涵蓋**。該解讀已被使用者否決，本 task 是修正。

## 依賴

- **前置**：無。與其他 child 完全獨立，可並行。
- **輸入素材**：parent 的 `research/agent-schema-support-matrix.md` §2、§3
  （已含 claude/codex marketplace.json 與 claude/copilot plugin.json 的實跑產物逐字形狀），
  以及 `research/eval-real-run-20260728.md` §D 的 golden 產物。
  **這些是素材，不是交付物** —— 交付物是下面的 spec 與可執行定義。

## 現況盤點（實際 grep 過）

可執行定義**已部分存在**，但沒有正式 schema、沒有 spec、沒有防漂移：

| 產物 | 現有 Go 型別 |
|---|---|
| claude marketplace.json | `internal/marketplace/build/mapper.go:30` `ClaudeDocument`、`:42` `ClaudeOwner`、`:52` `ClaudePlugin`、`:72` `RemoteSource` |
| codex marketplace.json | `internal/marketplace/build/codexmapper.go:19` `CodexDocument`、`:27` `CodexInterface`、`:36` `CodexPlugin`、`:46` `CodexPolicy`、`:57` `CodexLocalSource` |
| plugin.json（claude / copilot） | `internal/pack/bundle/pluginjson.go:30` `PluginManifest`；路徑對照在 `internal/pack/pluginmanifest/write.go:16-19` |

⇒ 本 task **不是從零建**，是「補 spec + 補正式 schema + 補防漂移」。

## Requirements

### R1 — 獨立 spec 文件

1. 於 `.trellis/spec/conformance/` 新增一份 agent schema 規格
   （與既有的 `openapm-v0.1.md`、`cli-verification-checklist.md` 同層級）。
2. 內容須涵蓋**三個產物家族**，每個逐欄位列出型別、必填/選填、預設值、
   以及該欄位在上游的出處（`file:line` 或實跑產物）：
   - `.claude-plugin/marketplace.json`（claude 輸出）
   - `.agents/plugins/marketplace.json`（codex 輸出）
   - `plugin.json`（claude 版含 `mcpServers`；copilot 版不含）
3. 須記錄**已知的上游瑕疵**並註明是刻意對齊，至少包含：
   codex 的 `source.source == "url"` 但 `url` 放的是 `owner/repo` shorthand
   而非真 URL（`internal/marketplace/build/codexmapper.go:127` 的註解已載明）。
4. 須記錄**三軸支援度**的區別（部署 targets / marketplace 輸出 / plugin.json 生態），
   因為這三張清單彼此不包含，是最容易誤解的一點。

### R2 — 可執行的 schema 定義

1. 為上述三個產物家族各提供一份**可執行的 schema**（JSON Schema 或等價形式），
   放在可被測試載入的位置。
2. schema 必須能**驗證實際產物**：把 `research/` 裡的上游實跑產物餵進去要通過，
   把刻意破壞的變體餵進去要失敗。
3. **不得**與現有 Go 型別（見上方盤點）產生第二套事實來源 ——
   schema 與 Go 型別必須有一方能推導另一方，或有測試鎖住兩者一致（見 R3）。

### R3 — 防漂移機制（本 task 的核心，不可省略）

使用者的原話是「**兩者必須同步**」。因此：

1. 必須有一個**會失敗的測試**，在下列任一情況轉紅：
   - Go 型別新增/刪除/改名了欄位，但 schema 沒跟著改
   - schema 改了，但 spec 文件沒跟著改
   - spec 文件描述的欄位集合與 schema 不一致
2. 「靠人記得同步」**不算**防漂移機制。
3. 若採「單一事實來源 + 產生器」路線（例如從 Go 型別產生 schema 與 spec 表格），
   則需有測試確認產生結果與 committed 檔案逐位元組相同
   （golden-file 比對，`go test` 時檢查，不是只在 CI）。

## Acceptance Criteria

> 本 task 的 AC 用 **AS** 前綴，**不與 parent 的 AC 編號共用命名空間** ——
> 它對應的是 `requirements-trace.md` 的 U8，是 parent AC1–AC51 之外的新需求。
> （原本用 AC1–AC7 會與 parent 的 AC1–AC7 撞號，機械式分割檢查會誤判為重複認領。）

- [ ] AS1：`.trellis/spec/conformance/` 下存在該 spec 文件，
      且涵蓋三個產物家族的全部欄位。
      驗法：對照 `research/agent-schema-support-matrix.md` §2、§3 的欄位清單逐項核對，
      不得有欄位只出現在 research 而不在 spec。
- [ ] AS2：spec 記錄了 codex `source.url` shorthand 這個上游瑕疵並註明刻意對齊。
      驗法：grep 該檔含 `shorthand` 或等義說明，且引用
      `codexmapper.go:127` 的實際行號。
- [ ] AS3：spec 記錄了三軸支援度的區別。
      驗法：grep 該檔同時含「部署」「marketplace 輸出」「plugin.json 生態」三者。
- [ ] AS4：可執行 schema 存在，且**上游實跑產物驗證通過**。
      驗法：測試載入 `research/` 引用的 golden JSON（claude/codex marketplace.json、
      兩種 plugin.json），全部 validate 成功。
- [ ] AS5：**負向測試**：刻意破壞的變體 validate 失敗。
      至少包含：codex 產物缺 `category`、claude 產物缺 `owner`、
      plugin.json 的 `author` 給成字串而非物件。
- [ ] AS6：**防漂移測試存在且真的會紅**。
      驗法：實際在 Go 型別上加一個欄位（暫時），跑測試確認轉紅，再還原。
      **這條必須實際做過一次，不能只是「有寫測試」**。
- [ ] AS7：`go build ./...`、`go vet ./...` exit 0；
      `go test ./... -coverprofile=cover.out` 後
      `go tool cover -func=cover.out | Select-Object -Last 1`（PowerShell） 的 total ≥ 80%。

- [ ] AC-L9 — **parent C5 的本地閘門**：未新增任何第三方相依。
      驗法：`git diff -- go.mod; git diff --cached -- go.mod` —— 都不得出現新 `require` 行。
      （`git status --porcelain` 只給 `M go.mod`，看不出新增了什麼，不足以判定。）

## Constraints

- **不得新增第三方相依**（與 parent C5 一致）。若 JSON Schema 驗證需要函式庫，
  先評估用 Go 標準庫 + 既有相依能否達成；不行的話要在 design 階段回報並取得裁定。
- **不得產生第二套事實來源**。現有 Go 型別是 `pack` 的實際行為來源，
  schema 若與它不一致，錯的是 schema。
- 任何判斷句遵守 `.trellis/spec/guides/claim-evidence-guide.md`。

## 驗收紀律（全域，沿用 parent）

- 驗 exit code 一律用預先 build 的 `bin/apm-go.exe`，**禁止 `go run`**
- 以 `go test -run <pattern>` 當閘門前，先 `go test -list <pattern>` 證明匹配非空
- `t.Skip` 不算通過
- 逐步 codex 閘門規則適用（見 parent `implement.md` 開頭）

## 尚待 design 階段裁定

- **未驗證**：JSON Schema 驗證在不新增相依的前提下如何實作
  （Go 標準庫沒有 JSON Schema validator）。可能的路線：
  (a) 用 Go 型別 + `encoding/json` 的嚴格解碼當作 schema；
  (b) 手寫最小 validator；
  (c) 取得裁定後新增一個相依。
  **三者成本未估**，需在 `design.md` 補上再開工。
