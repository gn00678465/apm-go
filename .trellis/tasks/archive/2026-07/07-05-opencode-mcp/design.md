# Design: opencode MCP writer

輸入契約:`research/opencode-mcp-parity.md`（Python 語意 + apm-go 4-adapter 模式 +
mcp_common.go 落點,皆附 file:line）。

## 邊界與檔案

- **唯一新檔**:`internal/deploy/mcp_opencode.go`（`opencodeAdapter` 的 MCP 擴充）。
- **既有檔唯一修改**:`internal/deploy/mcp_common.go` 的 `managedMCPKeys` 加兩個 key。
- 不動 `opencode.go`（primitive-only 部分）、不動其他 4 個 MCP writer。

## opencode.json entry 形狀（對照 Python `_to_opencode_format`）

- config 檔:專案根 `opencode.json`;頂層 key **`mcp`**（非 `mcpServers`）。
- ResolveMode:**Bake**（Python `_supports_runtime_env_substitution=False`）。
- 檔案權限:`0600`（bake 慣例,同 claude/codex/antigravity）。
- **stdio**:`{"type":"local","enabled":true,"command":[cmd, ...args],"environment":{...}}`
  - `command` 是**單一字串陣列**（cmd 併入首元素）——需 `append([]string{r.Command}, r.Args...)`。
  - env 的 JSON key 是 **`environment`**（非 `env`）;env 空則不出這個 key。
- **remote**:`{"type":"remote","enabled":true,"url":...,"headers":{...}?}`
  - **統一**,不分 http/sse/streamable-http（不做 antigravity 式 transport 切換,不做 codex 式 SSE skip）。
- 一律 `enabled:true`（Python 標準 CLI 流程無 disable 語意,寫死）。

## 實作骨架

```go
// mcp_opencode.go
func (a *opencodeAdapter) MCPResolveMode() manifest.ResolveMode { return manifest.ResolveBake }

func (a *opencodeAdapter) WriteMCP(prims []Primitive, projectDir string) ([]string, []string, []string, error) {
    entries, diags := buildMCPEntries(prims, manifest.ResolveBake, opencodeMCPEntry)
    if len(prims) == 0 { return nil, nil, diags, nil }
    rel := "opencode.json"
    if err := writeMergedMCPJSON(filepath.Join(projectDir, rel), "mcp", entries, consideredNames(prims), 0600); err != nil {
        return nil, nil, diags, err
    }
    return []string{rel}, entryNames(entries), diags, nil
}

func opencodeMCPEntry(r *ResolvedMCPServer) (map[string]any, bool, string) {
    e := map[string]any{"enabled": true}
    if r.Transport == "stdio" || r.Command != "" {
        e["type"] = "local"
        e["command"] = append([]string{r.Command}, r.Args...)
        if len(r.Env) > 0 { e["environment"] = r.Env }
    } else {
        e["type"] = "remote"
        e["url"] = r.URL
        if len(r.Headers) > 0 { e["headers"] = r.Headers }
    }
    return e, true, ""
}
```
（實際 stdio/remote 判別條件、以及 buildMCPEntries 對 refuse/非 https 的既有處理,實作時對齊 antigravityMCPEntry 的既有寫法。）

## managedMCPKeys 擴充（mcp_common.go）

`managedMCPKeys` 加入 `"environment": true, "enabled": true`。理由:opencode entry 用這兩個
key,若不納入,redeploy 時（某次未再產生該欄位）舊值會被誤判外來鍵而殘留。此為共用
merge 邏輯的耦合點,必須連同 writer 一起改。加入後須確認不影響其他 4 個 target 的
既有 merge 測試（它們的 entry 不含這兩鍵,新增到「受管」集合對它們是無害的 no-op）。

## 決策（含刻意 deviation,均記錄）

1. **不加 `.opencode/` 目錄閘門** —— 對齊 apm-go 既有 4 個 writer 的 create-on-write
   慣例（mf-013 已定案:opt-in 語意交給 target 選取,不做目錄預先存在檢查）。偏離
   Python 的 `.opencode/` gate,但**與 apm-go 自家其他 target 一致**;只為 opencode 加
   gate 反而不一致。→ A/B 記為 documented deviation（同其他 4 target 已有的偏離）。
2. **remote 統一 url** —— 不分 transport,對齊 Python opencode。
3. **未定義 env placeholder** —— 沿用 apm-go bake 既有行為（診斷+省略 key），非 Python
   的「原樣落盤」。此為 mf-013 已套用到全部 bake target 的既有差異,沿用不另議。

## 測試

- 單元（`mcp_writers_test.go` 風格）:stdio entry 形狀（command 單陣列、environment、
  type:local、enabled）、remote entry（type:remote、url、headers、無 transport 分支）、
  頂層 key 是 `mcp`、寫到 `opencode.json`、perm 0600。
- merge:既有 `opencode.json` 使用者其他設定保留;redeploy 判重不重複堆疊（驗證
  managedMCPKeys 擴充生效)。
- interface 斷言:`var _ MCPTarget = (*opencodeAdapter)(nil)`。
- A/B（evals）:對照 Python `opencode.json` 的 `mcp` 區塊 field 形狀（含 deviation 清單）。

## Rollback

新檔 + 一行 managedMCPKeys 擴充;`git revert` 即可。既有 4 target 零行為變更。
