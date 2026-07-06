# Implement — install --mcp parity（#1 + #2）

排序：先 #1（獨立、最小、低風險）→ 再 #2（新函式 + 接線）→ 迴歸 + A/B。
每步先寫/更新測試（TDD），再實作，最後全量驗證。

## Step 1 — #1 block style（R1 / AC1, AC2）

- [ ] 1a. 測試先行：`cmd/apm/mcpinstall_test.go` 新增
  - 輸入 apm.yml 含 `dependencies:\n  apm: []\n  mcp: []`（flow）。
  - upsert bare-string 條目 → 斷言輸出 `mcp:` 為 block（`\n    - io.github...`），
    且 `apm: []` 與其他位元組不變。
  - upsert mapping 條目（self-defined 或 registry+version）→ 斷言 block。
  - 驗證：`go test ./cmd/apm/ -run MCP.*Block`（先紅）。
- [ ] 1b. 實作：`upsertMCPEntry` 的 add / replace 分支加 `mcpSeq.Style &^= yamllib.FlowStyle`。
- [ ] 1c. 驗證：`go test ./cmd/apm/ -run MCP`（綠）。→ review gate（diff 僅該序列 style）。

## Step 2 — #2 純函式 + 閘門（R2 / AC3, AC4, AC5）

- [ ] 2a. 測試先行：新增 `cmd/apm/mcp_prompt_test.go`
  - `collectHeaderValues`：stub `ask`
    - `["Authorization"]` + ask 回 "ghp_x" → `{"Authorization":"Bearer ghp_x"}`。
    - 其他 header 名 → 原始值；ask 回 "" → 該 header 略過（不入 map）。
    - `looksSecret`：token/key/secret/password/api（大小寫）true，其餘 false。
  - `isNonInteractiveEnv`：逐一設 `APM_E2E_TESTS/CI/GITHUB_ACTIONS/...` 斷言 true；
    全空斷言 false（用 `t.Setenv`）。
  - 驗證：`go test ./cmd/apm/ -run Header|NonInteractive`（先紅）。
- [ ] 2b. 實作 `cmd/apm/mcp_prompt.go`：`collectHeaderValues`、`looksSecret`、
  `isNonInteractiveEnv`、`canPromptCreds`，及 TTY `ask`（secret 用 `term.ReadPassword`、
  非 secret 用 `getScanner()`）。
- [ ] 2c. `go mod tidy` 引入 `golang.org/x/term`；`go build ./...`。
- [ ] 2d. 驗證：`go test ./cmd/apm/ -run Header|NonInteractive`（綠）。

## Step 3 — #2 接線 resolveFromRegistry（R2 / AC3, AC5）

- [ ] 3a. `resolveFromRegistry`：`ResolveDeployable` 後，`len(requiredHeaders)>0 &&
  canPromptCreds()` → `collectHeaderValues(...)` 併入 `dep.Headers`；只有在
  `len(dep.Headers)==0` 時才 append 既有 "requires header(s)..." 診斷。
- [ ] 3b. 確認 `entryNode`/apm.yml 路徑未被改動（apm.yml 仍 bare string；grep 確認
  `buildPersistEntry`/`buildRegistryPersistEntryNode` 未變）。
- [ ] 3c. 驗證：既有 registry 測試（非互動）仍綠；手動或注入式測試覆蓋互動注入路徑。

## Step 3.5 — D2 conflict 互動 confirm-replace（R3 / AC8）

- [ ] d1. 測試先行：`mcpinstall_test.go`
  - `diffEntry`：bare↔bare→`old -> new`；mapping 改 transport→`transport: 'stdio' -> 'http'` 樣式；缺值 `<absent>`；相同→nil。
  - `upsertMCPEntry` conflict：stub confirm →（a）true→"replaced"；（b）false→"skipped"、節點不變；（c）`nil`→error "non-interactive"；（d）force=true→"replaced" 不呼叫 confirm。
- [ ] d2. 實作：`upsertMCPEntry` 加 `confirm confirmReplaceFunc` 參數 + `diffEntry`；更新 4 個既有測試呼叫點（傳 nil）與 `runMCPInstall` 呼叫點。
- [ ] d3. `runMCPInstall`：互動時建 confirm callback（印 diff + `Replace? [y/N]` 預設 N）；`status=="skipped"` 比照 unchanged 印訊息 return。
- [ ] d4. 驗證：`go test ./cmd/apm/ -run Upsert|Diff|Conflict`（綠）；既有衝突測試仍綠。

## Step 4 — 迴歸 + 覆蓋率（AC6）

- [ ] 4a. `go fmt ./... && go vet ./... && go build ./...`。
- [ ] 4b. `go test ./... -race`（全綠）。
- [ ] 4c. `go test ./... -cover`，新增邏輯 ≥ 80%。

## Step 5 — A/B 對照（AC7）

- [ ] 5a. `D:\Projects\apm-dev\evals` 新增對照腳本（比照 marketplace 慣例）：
  - 全新專案 `install --mcp io.github.github/github-mcp-server`，比對 apm.yml
    （apm-go block vs 原版 block）。
  - 互動流程：apm-go prompt token（Bearer 注入 target）vs 原版 prompt token；
    deviation（來源 OCI vs remote header）明列於報告。
- [ ] 5b. 記錄結果與 deviation。

## Review gates
- Step 1c、Step 3c 後各做一次 diff 審視（surgical、無越界改動）。
- commit 前跑 Step 4 全量驗證；安全檢查：apm.yml 無明碼 secret、無 token echo/log。

## Rollback points
- #1、#2 兩處獨立；任一出問題可單獨 revert 不影響另一項。
