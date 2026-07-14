# Research: Antigravity CLI — MCP configuration（serverUrl vs url、路徑、schema）

- **Query**: Antigravity（agy）MCP 設定的權威 schema：remote transport 到底該寫 `serverUrl` 還是 `url`/`httpUrl`（PRD decision #2）；project/global scope 路徑；完整 entry schema；env var 插值支援。官方文件 + 本機 agy 1.0.16 實機/二進位驗證雙軌查證。
- **Scope**: external（官方文件）+ local read-only verification（agy binary strings、`~/.gemini/` 實機檔案）
- **Date**: 2026-07-10

---

## Official docs（verbatim quotes + URL + fetch date）

### https://antigravity.google/docs/mcp（fetched 2026-07-10，經 r.jina.ai reader；原站 Angular SPA 直抓只有 JS shell）

**Remote Connection Schema（逐字引文，與 2026-07-05 快照完全相同）：**

> "When declaring remote SSE, Streamable HTTP, or websocket-based MCP connections, you must define the `serverUrl` field. Legacy fields like `url` or `httpUrl` are not supported."

**其餘要點（同頁）：**
- 設定檔頂層是單一 `mcpServers` object，每個 server 是 key-value entry。
- 路徑：**Global** = `~/.gemini/config/mcp_config.json`；**Project（workspace）** = `.agents/mcp_config.json`。
- Entry schema（文件列出的欄位）：
  - Transport 二選一：`command`（stdio 執行檔路徑）或 `serverUrl`（remote Streamable HTTP / SSE URL）。
  - Optional：`args` (string[])、`env` (object，stdio 進程環境變數)、`cwd` (string，stdio 工作目錄)、`headers` (object，remote 自訂 HTTP header)、`authProviderType`（支援 `"google_credentials"`）、`oauth`（clientId/clientSecret）、`disabled` (bool)、`disabledTools` (string[])。
- **無** `${VAR}` 環境變數插值的任何記載。
- CLI 段落：Antigravity CLI 同時支援 stdio 與 remote；在 prompt panel 輸入 `/mcp` 開啟 Interactive MCP Manager（是**會話內 slash command**，不是 `agy mcp` 子指令）。

### CLI docs 索引
- https://antigravity.google/docs/cli/using（fetched 2026-07-10）：只講 `~/.gemini/antigravity-cli/settings.json` / `keybindings.json` 與 `/config`、`/settings`、`/permissions` 等；**零 MCP 內容**。
- https://antigravity.google/docs/cli/commands/mcp：**不存在**——SPA route fallback 到 getting-started 內容（download/installation/slash commands），無 MCP CLI 專頁。
- docs 首頁 nav（r.jina.ai 渲染）只露出 Getting Started 節點，側欄為 lazy-load，無法完整枚舉；已另試 `/docs/cli/commands/mcp` 確認 404-fallback。

---

## Local agy verification（commands run + output）

環境：`agy` **1.0.16**，`C:\Users\gn006\AppData\Local\agy\bin\agy.exe`（153,661,592 bytes，Go binary）。全程 read-only。

### 1. CLI surface：沒有 `mcp` 子指令

```
$ agy --version        → 1.0.16
$ agy --help           → subcommands: changelog, help, install, models, plugin, plugins, update（無 mcp）
$ agy help mcp         → Error: unknown subcommand: mcp
```
MCP 管理只能走會話內 `/mcp`（與 docs 一致）。

### 2. 實機檔案：global 路徑之爭定案

```
$ ls ~/.gemini/config/
  config.json  import_manifest.json  mcp_config.json  plugins/  projects/  sidecars/ ...
$ cat ~/.gemini/config/mcp_config.json
  { "mcpServers": { "codebase-memory-mcp": { "command": "C:/.../codebase-memory-mcp.exe" } } }

$ ls ~/.gemini/antigravity-cli/
  settings.json  keybindings.json  AGENTS.md  bin/  cache/  conversations/  log/  mcp/ ...
$ ls ~/.gemini/antigravity-cli/mcp/codebase-memory-mcp/
  delete_project.json  detect_changes.json  get_architecture.json ...（14 個 per-tool JSON）
```

**判讀**：`~/.gemini/config/mcp_config.json` 真實存在且被 CLI 使用——`~/.gemini/antigravity-cli/mcp/<server>/` 底下是**由該 config 連線後產生的 per-tool schema 快取**（server 名稱 `codebase-memory-mcp` 與 config entry 一致；mtime 相關：config 2026-07-05 09:18 → 快取目錄 09:30）。`~/.gemini/antigravity-cli/` 是 CLI 的 app-data 目錄（settings/keybindings/logs/conversations/快取），**不是** MCP 設定檔位置。issue #60 留言「CLI 實際用 `~/.gemini/antigravity-cli/mcp_config.json`」的推測與本機觀察**不符**——該目錄下沒有 mcp_config.json，只有 tool-schema 快取。官方文件寫的 global 路徑是對的。

### 3. Binary strings：schema 與驗證邏輯

```
$ grep -aoE 'serverUrl|httpUrl|"url"|mcp_config\.json|mcpServers|...' agy.exe | sort | uniq -c
  33 serverUrl / 28 authProviderType / 24 disabledTools / 16 mcpServers / 13 mcp_config.json / 3 "url" / 1 httpUrl
```

**驗證錯誤訊息（決定性證據）：**
```
MCP server %q must have either command or serverUrl
MCP server %q cannot have both command and serverUrl
```
→ config loader 的 transport 判別欄位**只認 `command` 與 `serverUrl`**。

**完整 config entry struct**（binary 內 Go type descriptor，型別 `mcp.ConfigSchemaJsonMcpServersValue`，顯然由 JSON schema 產生）：

```
struct {
  Args             []string  json:"args,omitempty"
  AuthProviderType *enum     json:"authProviderType,omitempty"
  Command          *string   json:"command,omitempty"
  Cwd              *string   json:"cwd,omitempty"
  Disabled         *bool     json:"disabled,omitempty"
  DisabledTools    []string  json:"disabledTools,omitempty"
  EnabledTools     []string  json:"enabledTools,omitempty"     ← 文件未載
  Env              map       json:"env,omitempty"
  Headers          map       json:"headers,omitempty"
  Oauth            *struct   json:"oauth,omitempty"
  ServerUrl        *string   json:"serverUrl,omitempty"
  Tools            ?         json:"tools,omitempty"            ← 文件未載
  Url              *string   json:"url,omitempty"              ← 見下方 nuance
}
```
- **沒有 `trust` 欄位**（Gemini CLI 有、Antigravity 沒有）。
- **`url` 欄位存在於 parse struct**，但：驗證訊息只提 `serverUrl`；binary 內找不到任何 `url`→`serverUrl` fallback/normalize/deprecation 字串；`"url"` 字串的其餘出現全在無關 context（elicitation 錯誤訊息）。無任何證據顯示 runtime 會採用 `url` 值。
- `httpUrl` 唯一一次出現是無關的 messages/HTTP client struct tag（`HttpURL json:"httpUrl"` 緊鄰 `BaseURL json:"baseURL"`），與 MCP config 無關。

**內嵌 agent-facing MCP 文件**（binary 內 markdown，`/mcp` 或 knowledge 用）：
```json
{
  "mcpServers": {
    "sqlite-helper": { "command": "sqlite-mcp-server", "args": ["/path/to/database.db"], "env": { "DB_READONLY": "true" } },
    "remote-service": { "serverUrl": "https://mcp.mycompany.com/sse" }
  }
}
```
> "### 2. SSE Transport (Remote) ... **`serverUrl`** (string, required): The HTTP(S) URL of the remote MCP endpoint."

→ **SSE 範例本身就用 `serverUrl`**（URL 還刻意是 `/sse` endpoint）。內嵌文件列出的位置：Global = `~/.gemini/config/mcp_config.json`、Plugin = `plugins/<plugin_name>/mcp_config.json`；另一份內嵌 customizations 文件說 `mcp_config.json`/`hooks.json` 可放在「任一 customization root」（global root + workspace root）。

**workspace root `.agents/` 佐證**：binary 內有 `.agents/hooks.json`、`.agents/rules/`、`.agents/skills/`、`.agents/plugins/` 字串（mcp_config.json 路徑為 runtime join，無完整字串，但 `.agents/mcp_config.json` 另有官方 docs + issue #60 workaround 實證）。

**env var 插值**：`os.ExpandEnv` 在 binary 只被一個無關的 google_api cert 套件引用；MCP config 解析路徑無任何插值證據 → **不支援 runtime `${VAR}` 插值**，apm-go 的 `ResolveBake`（安裝時就地解析）正確。

---

## Schema summary table

| 面向 | 值 | 依據 |
|---|---|---|
| 頂層 key | `mcpServers` | docs + binary struct + 實機檔案 |
| Project scope | `.agents/mcp_config.json` | docs（2026-07-10 再確認）+ issue #60 workaround |
| Global scope | `~/.gemini/config/mcp_config.json` | docs + **實機檔案存在且有 tool-schema 快取佐證** |
| Plugin scope | `plugins/<name>/mcp_config.json` | binary 內嵌文件 |
| stdio 欄位 | `command`（必）、`args`、`env`、`cwd` | docs + binary struct |
| remote 欄位 | `serverUrl`（必，**涵蓋 sse/streamable-http/websocket 全部**）、`headers` | docs 逐字引文 + binary 驗證訊息 + 內嵌 SSE 範例 |
| 其他欄位 | `authProviderType`、`oauth`、`disabled`、`disabledTools`；未文件化：`enabledTools`、`tools` | docs + binary struct |
| 不存在 | `trust`、`httpUrl`；`url` 僅 parse-struct 存在、無 runtime 採用證據 | binary struct + 驗證訊息 |
| env 插值 | 不支援（bake at install time 正確） | docs 無載 + binary 無 ExpandEnv 於 MCP 路徑 |
| CLI 管理 | 無 `agy mcp` 子指令；會話內 `/mcp` Interactive MCP Manager | `agy help mcp` 實測 + docs |

---

## Recommendation for apm-go（PRD decision #2 — 定案）

**所有 remote transport（`sse`、`http`、`streamable-http`、websocket）一律寫 `serverUrl`。刪除 sse→`url` 特例分支。**

具體變更：
1. `internal/deploy/mcp_antigravity.go:36-41`：`antigravityMCPEntry` 移除 `if r.Transport == "sse" { e["url"] = r.URL }` 分支，非 stdio 一律 `e["serverUrl"] = r.URL`。
2. `internal/deploy/mcp_writers_test.go:116-133`：`TestWriteMCP_Antigravity_SSEUsesURLField` 改為斷言 sse 也輸出 `serverUrl`、不得有 `url`（測試名稱同步更新）。
3. `conformance/conformance-kit/oracle/targets/expected/antigravity.yaml` 的 `http_field: serverUrl` 已是通用寫法，無需改；如有 sse 例外註記則刪除。
4. stdio（`command`/`args`/`env`）、`headers`、`mcpServers` key、`.agents/mcp_config.json` 路徑、`ResolveBake` 全部維持現狀——皆與官方/實機一致。

理由鏈：官方文件逐字明言 legacy `url` 不支援 → binary 驗證訊息只認 `command|serverUrl` → binary 內嵌 SSE 範例/文件均用 `serverUrl` → 唯一反向訊號（parse struct 有 `Url` 欄位）沒有任何 runtime 採用證據，且寫 `serverUrl` 在「`url` 其實可用」的世界裡也不會出錯（`serverUrl` 必然被認得），是嚴格佔優的選擇。

---

## Deltas vs 2026-07-05 research（antigravity-settings.md §C）

1. **官方 wording 零變化**：serverUrl 段落逐字相同——07-05 快照仍然有效。
2. **Global 路徑之爭定案（07-05 Caveat 消除）**：07-05 記載三方說法不一（docs 說 `~/.gemini/config/`、issue #60 留言的 binary 分析說 CLI 用 `~/.gemini/antigravity-cli/`）。本次實機驗證：`~/.gemini/config/mcp_config.json` 存在、內容為 `mcpServers`、且 `~/.gemini/antigravity-cli/mcp/<server>/` 有對應 tool-schema 快取 → **CLI 確實讀 `~/.gemini/config/mcp_config.json`**；`antigravity-cli/` 只是 app-data + 快取。issue #60 留言的該項推測不成立。
3. **新增 binary 層證據**：07-05 只有文件層；本次取得驗證錯誤訊息（只認 command/serverUrl）、完整 config struct、內嵌 SSE=serverUrl 範例。
4. **新 nuance**：parse struct 含文件未載的 `url`、`enabledTools`、`tools` 欄位；`httpUrl` 確認與 MCP 無關；無 `trust` 欄位。
5. **新確認**：`agy` 1.0.16 無 `mcp` 子指令（07-05 未實測）；`/docs/cli/commands/mcp` 頁不存在。
6. **07-05 的「sse→url 分支與官方文件不一致」疑慮升級為定論**：apm-go 現行 sse 特例應移除（見 Recommendation）。

---

## Caveats / Not found

- **`url` 欄位的 runtime 行為未做黑箱實測**：僅靠 strings 分析無法 100% 排除「loader 把 `url` 當 `serverUrl` fallback」的可能（struct 欄位存在但無相關字串）。未實際跑 agy 會話測試（避免非 read-only 副作用：建 project/conversation、連網）。但此不確定性不影響建議——寫 `serverUrl` 在兩種世界都正確。
- **binary 內嵌 MCP 文件只提 Stdio 與 SSE 兩種 transport**，未提 Streamable HTTP/websocket；官方網頁 docs 則三者都歸 `serverUrl`。可能是內嵌文件較舊或簡化，不影響結論（binary 有 `mcp.NewStreamableHTTPConnector`，Streamable HTTP 實際支援）。
- **docs 側欄無法完整枚舉**（SPA lazy-load），不排除有未發現的 MCP 相關頁；已試 `/docs/mcp`、`/docs/cli/using`、`/docs/cli/commands/mcp` 三個 URL。
- **TUI 字串見 `Global: / Shared:` scope 標籤**（`/mcp` manager UI），「Shared」是否即 workspace scope 未驗證。
- **`oauth`/`authProviderType`/`enabledTools`/`tools` 欄位語意未深查**——apm-go 目前不產生這些欄位，暫無需求。
