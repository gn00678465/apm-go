# Deploy pipeline 架構筆記(MCP 接入點,design.md 種子)

讀 `internal/deploy/{adapter,deploy,primitive,antigravity}.go` 後的設計 grounding。

## 現有 pipeline(`deploy.go:24` `Run`)

1. **Collect**:`CollectLocalPrimitives` + 逐 dep `CollectDependencyPrimitives` → `[]Primitive`(檔案型,含 `SrcPath`)。
2. **Filter**:`--skill` 子集過濾。
3. **Conflict**:`ResolvePrimitives`(pr-002 local wins / pr-003 first-declared)→ winners。
4. **Deploy**:每 target adapter × 每 winner → `adapter.DeployPrimitive(p, projectDir)` → files → hash 入 lockfile。

## `TargetAdapter` 介面(`adapter.go:12`)

`Name() / DeployRoots() / SupportedTypes() []PrimitiveType / DeployPrimitive(p, projectDir) ([]string, error)`。
部署動作 = `copyFile` / `deploySkill`(遞迴複製)/ `deployFileToPath`。**全是檔案複製**。

## MCP 為何不合現有模型(核心設計決策)

- `Primitive`(`primitive.go:20`)以 `SrcPath` 指向來源**檔案**;MCP server 是 manifest `mcp:` 宣告的**結構物件**(`MCPDependency`),無來源檔可複製。
- N 個 MCP server 要 merge 成**單一** `mcp_config.json`(頂鍵 `mcpServers`);現有模型是「1 primitive → 1~多檔複製」,無「N winners → merge 成 1 JSON」路徑。
  - 對照:hooks 目前也是單檔(`.agents/hooks.json`),但走 `deployFileToPath` 複製既有檔;MCP 沒有既成 JSON 可複製,需**組建**。
- 各 adapter `SupportedTypes()` 目前**皆不含 mcp**(antigravity `antigravity.go:11-13` = instructions/skills/hooks)。

## 設計選項(留 design.md 定稿,待 oracle 確認 JSON shape)

- **選項 A(傾向)**:新增 `TypeMCP` primitive,`Primitive` 攜帶結構化 MCP 資料(新欄位,非 SrcPath)。好處:直接重用 `ResolvePrimitives`(pr-002/003)免重寫 override(對齊 advisor 約束 #3)。deploy 時各 target 累積 winners → 收尾 merge 寫一次 `mcp_config.json`。
- **選項 B**:MCP 走獨立 collect+merge 後處理路徑。分離乾淨但重複 override 邏輯。

## 待 oracle(`original-apm-mf013-mcp.md`)回填的 design 缺口

1. per-target dispatch matrix:哪些 target install 時解析、哪些留 runtime。
2. mcp_config.json 逐 server JSON shape(stdio vs http:`serverUrl`/type/headers)。
3. `${input:}` 非互動 install 語意 → 決定 resolver 錯誤/拒寫路徑。
4. 既有 mcp_config.json 存在時 merge vs overwrite。
