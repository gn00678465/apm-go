# install --mcp 補 opencode 設定(opencode.json)

## Goal

讓 `opencodeAdapter` 支援 MCP 部署:實作 `MCPTarget`(`MCPResolveMode` + `WriteMCP`),
把 MCP server 寫進專案根 `opencode.json` 的 `mcp` key,對照 Python 原版格式。

研究:`research/opencode-mcp-parity.md`。

## 關鍵發現（來自研究）

- 確認 100% 缺口:無 `mcp_opencode.go`,`opencodeAdapter` 未實作 `MCPTarget`,零測試/oracle。
- Python `adapters/client/opencode.py`:寫專案根 `opencode.json` 的 `mcp` key
  （非 `mcp.json`）;**opt-in gate**——僅在 `.opencode/` 目錄存在時才寫。
- **實作 gotcha**:`mcp_common.go` 的 `managedMCPKeys` 集合不含 `environment`/`enabled`,
  opencode entry 形狀需要它們,必須連同新 writer 一起擴充(否則 merge/redeploy 會漏)。
- 有現成 4 個 adapter(claude/codex/copilot/antigravity)可完全參照 WriteMCP 骨架與
  共用 helper(`buildMCPEntries`/`writeMergedMCPJSON`/`ResolvedMCPServer`)。

## Requirements

- 新增 `internal/deploy/mcp_opencode.go`:`opencodeAdapter` 實作 `MCPResolveMode()`
  + `WriteMCP()`,寫 `opencode.json` 的 `mcp` key。
- entry 形狀對照 Python `_to_opencode_format`:stdio 與 remote(http/sse)各自欄位
  (含 opencode 特有的 `environment`/`enabled` 等,依研究確認的欄位名)。
- **opt-in gate**:對照 Python「僅 `.opencode/` 存在才寫」的語意(在 apm-go 對應落點實作)。
- merge 策略:保留 `opencode.json` 內使用者其他設定,只動 `mcp` 區塊。
- `ResolveMode`:依 Python 對 remote transport 的處理決定 bake vs translate(design 定案)。
- 擴充 `mcp_common.go` 的 `managedMCPKeys`,涵蓋 opencode 需要的欄位。
- 不破壞既有 4 個 MCP adapter 行為與測試。

## Acceptance Criteria

- [ ] `install --mcp <name> --target opencode`(且 `.opencode/` 存在)→ `opencode.json`
      的 `mcp` key 含該 server,格式對照 Python
- [ ] `.opencode/` 不存在 → 不寫(opt-in gate,對照 Python)
- [ ] stdio 與 remote transport 的 entry 形狀各自正確
- [ ] 既有 `opencode.json` 的其他設定在 merge 後保留
- [ ] `managedMCPKeys` 擴充後,redeploy/merge 對 opencode 欄位正確判重、不重複堆疊
- [ ] A/B 對照 Python opencode MCP 輸出通過
- [ ] `go build/vet/test ./...` 全綠,新測試覆蓋 ≥ 80%

## Non-Goals

- 不改其他 target 的 MCP 行為。
- 不新增 Python 沒有的 opencode 欄位。
