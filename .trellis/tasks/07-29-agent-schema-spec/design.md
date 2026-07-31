# Design — agent-schema-spec

> 關閉 prd.md「尚待 design 階段裁定」的唯一問題，並固定檔案佈局與防漂移機制，
> 使實作與 `verify.ps1` 的既有檢查完全對齊。

## D1 — JSON Schema 驗證路線（已裁定，零成本）

PRD 列的三條路線 (a) 嚴格解碼 / (b) 手寫 validator / (c) 新增相依，**全部不必**。
實測事實（2026-07-31 主 session 親自驗證）：

```
go.mod:11    github.com/santhosh-tekuri/jsonschema/v5 v5.3.1   ← 已是直接相依
internal/marketplace/build/schema_test.go:27   已 import 並使用該套件
internal/marketplace/build/schema_test.go:40   compileMarketplaceSchema() 為現成載入/編譯先例
internal/marketplace/build/testdata/claude-code-marketplace.schema.json   上游 schema 副本已存在
```

⇒ 走「既有相依」路線：不新增任何 `require` 行，AC-L9 無風險。
上游 schema 副本（~90KB、informational）**保留原樣**，不作為 R2 交付物 ——
R2 的 schema 是描述 apm-go 實際輸出的**自撰最小 schema**（Go 型別是事實來源，
schema 錯則改 schema，見 prd.md Constraints）。

## D2 — 檔案佈局（與 verify.ps1 既有檢查對齊，不得偏離）

| 交付物 | 路徑 |
|---|---|
| spec 文件 | `.trellis/spec/conformance/agent-schema.md` |
| claude marketplace schema | `internal/marketplace/build/testdata/apm-claude-marketplace.schema.json` |
| codex marketplace schema | `internal/marketplace/build/testdata/apm-codex-marketplace.schema.json` |
| plugin.json schema（claude 版，含 `mcpServers`） | `internal/pack/bundle/testdata/apm-plugin-claude.schema.json` |
| plugin.json schema（copilot 版，不含 `mcpServers`） | `internal/pack/bundle/testdata/apm-plugin-copilot.schema.json` |
| 上游 golden 產物 | 同上兩個 testdata/ 目錄，自 research 逐字抽出（見 D4） |

spec 文件**必須逐字包含**這三個家族路徑字串（AS1 閘門用 regex 精確比對）：
`.claude-plugin/marketplace.json`、`.agents/plugins/marketplace.json`、
`.github/plugin/plugin.json`。

## D3 — 防漂移機制（R3 的具體形狀）

兩層測試，測試名稱必須匹配閘門 pattern `SchemaDrift|SchemaSync`：

1. **Go 型別 ↔ schema**（每個家族一條）：用 `reflect` 走訪 Go struct 的 json tag
   集合，與 schema 的 `properties` 鍵集合做**雙向**集合相等比對
   （Go 多欄位 → 紅；schema 多欄位 → 紅）。`required` 清單另比對非 omitempty 欄位。
   對象 struct（prd.md 現況盤點表）：
   `ClaudeDocument`/`ClaudeOwner`/`ClaudePlugin`（mapper.go）、
   `CodexDocument`/`CodexInterface`/`CodexPlugin`/`CodexPolicy`/`CodexLocalSource`
   （codexmapper.go）、`PluginManifest`（pluginjson.go）。
2. **schema ↔ spec 文件**：測試從 repo root（自 cwd 向上找 `go.mod` 定位）讀
   `.trellis/spec/conformance/agent-schema.md`，解析各家族段落 markdown 表格
   第一欄的 `` `fieldName` ``，與 schema `properties` 鍵集合比對。
   ⇒ **spec 文件的欄位表因此是機器可讀契約**：每家族一張表、
   欄位名放第一欄、用反引號包住 —— 寫 spec 時必須遵守，否則測試紅。

「靠人記得同步」不算機制（prd.md R3.2）；上述兩層合起來覆蓋 R3.1 的三種漂移。

## D4 — 測試命名與素材（閘門契約，逐字）

| AC | 測試名稱必須含 | 備註 |
|---|---|---|
| AS4 | `SchemaGolden` 或 `SchemaValidateUpstream` | 載入 testdata golden JSON 全部 validate 通過 |
| AS5 | `SchemaReject` 或 `SchemaNegative` | **至少 3 個頂層 Test 函式**（`-list` 只見頂層；閘門另有 `-minCount 3`）：codex 缺 `category`、claude 缺 `owner`、plugin.json `author` 給字串 |
| AS6 | `SchemaDrift` 或 `SchemaSync` | D3 的兩層 |

golden 素材來源（是素材不是交付物）：
`.trellis/tasks/07-28-marketplace-plugin-parity/research/agent-schema-support-matrix.md` §2/§3、
`research/eval-real-run-20260728.md` §D —— 內含上游實跑 marketplace.json（claude/codex）
與 plugin.json（claude/copilot）逐字形狀，抽成 testdata JSON 檔。

## D5 — AS6 Tier 2 動作

「實際加一個欄位 → 測試轉紅 → 還原」由實作方做過一次並把指令與輸出記進
`implement.jsonl`；主 session 於驗收時**獨立重做一次**（同 mutation 紀律），
兩份證據都要存在才算 AS6 關閉。
