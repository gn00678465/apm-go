# Implement: opencode MCP writer

TDD:先寫測試(RED)→ 實作(GREEN)→ `go build/vet/test ./...`。
輸入:`design.md` + `research/opencode-mcp-parity.md`。

## 執行順序

### 步驟 1 — 測試先行(RED)
- [x] `internal/deploy/mcp_writers_test.go` 加 opencode 測試:
  - stdio entry 形狀:`type:"local"`、`command` 為 `[cmd, ...args]` 單陣列、
    `environment`(非 env)、`enabled:true`;頂層 key `mcp`;檔名 `opencode.json`;perm 0600
  - remote entry:`type:"remote"`、`url`、可選 `headers`、`enabled:true`;
    **sse 與 http 走同一形狀**(無 transport 分支、無 SSE skip)——各測一案
  - interface 斷言 `var _ MCPTarget = (*opencodeAdapter)(nil)`
  - merge:預先放含使用者自訂鍵的 `opencode.json`,WriteMCP 後使用者鍵保留、mcp 區塊更新
  - redeploy 判重:同 server 二次寫入不重複堆疊(驗 managedMCPKeys 擴充)
- [x] 跑 `go test ./internal/deploy/... -run MCP.*[Oo]pencode -count=1` 確認**紅燈**

### 步驟 2 — 實作(GREEN)
- [x] 新檔 `internal/deploy/mcp_opencode.go`:`MCPResolveMode()=ResolveBake` + `WriteMCP()`
  (用 `buildMCPEntries` + `writeMergedMCPJSON(..., "mcp", ..., 0600)`) + `opencodeMCPEntry`
  (stdio command 合併單陣列 + environment;remote 統一 url;enabled:true)
- [x] `mcp_common.go` 的 `managedMCPKeys` 加 `"environment"`、`"enabled"`
- [x] 跑步驟 1 測試轉綠

### 步驟 3 — 全域驗證
- [x] `go build ./... && go vet ./... && go test ./... -count=1` 全綠(尤其既有 4 個 MCP
  writer 的 merge 測試不因 managedMCPKeys 擴充而破)
- [x] `gofmt -l` 對新檔/改檔乾淨(以 staged LF blob 判定)

### 步驟 4 — A/B(evals)
- [x] `D:\Projects\apm-dev\evals\ab_opencode_mcp.py`:對照 `uv run apm` 在有 `.opencode/`
  的專案安裝 MCP 後,兩邊 `opencode.json` 的 `mcp` 區塊 entry 形狀比對
  (stdio command 陣列/environment/enabled、remote type:remote/url)
- [x] deviation 清單:apm-go 不加 `.opencode/` gate(對齊自家慣例)、未定義 env 省略而非
  原樣落盤——註明為 documented deviation

## 中樞驗證檢查清單(subagent 完成後,中樞逐項親跑)
1. [x] 重跑 build/vet/test,親眼確認 17+ 套件全綠、既有 MCP 測試未破
2. [x] 重建二進位,真機:在含 `.opencode/` 的暫存專案 `install --mcp <name> --target opencode`
   → 檢查 `opencode.json` 的 `mcp` 區塊形狀正確(stdio 單陣列 command + environment)
3. [x] 跑 A/B,要求 0 failed(deviation 除外)
4. [x] 抽查:頂層 key 是 `mcp` 非 mcpServers、command 是單陣列、env key 是 environment、
   managedMCPKeys 確實擴充、remote 無 transport 分支
5. [x] 派 adversarial Explore 查:merge 保留使用者鍵、redeploy 判重、refuse/非 https 處理

## Review Gates
- A(步驟 2 後):entry 形狀與 Python `_to_opencode_format` 逐欄一致;managedMCPKeys 擴充
  不破既有 4 target
- B(步驟 4 後):A/B 非 tautology(至少一個欄位級 diff 證明比對器能抓差異)

## 已知 deviation(非缺陷)
- 不加 `.opencode/` 目錄閘門(對齊 apm-go 既有 4 writer)
- 未定義 env placeholder 省略而非原樣落盤(mf-013 既有 bake 決策)
