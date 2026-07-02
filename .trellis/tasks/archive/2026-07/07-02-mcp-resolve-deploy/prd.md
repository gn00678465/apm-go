# MCP install 解析與 target 部署 (req-mf-013 + mcp deploy)

## 問題陳述

apm-go 目前把 MCP 當「半個公民」:manifest `mcp:` 只被解析+驗證正確性,**解析完即丟棄**(`manifest.go:355-367`),既不留存於 `Manifest`、install 拿不到、也從未部署到任何 target。連帶 §4.5 的 placeholder 解析(req-mf-013)只有 recognition、無 resolution。

對照:tg-001/002/003(非 mcp primitives)與 pr-001/002/003 已實作且測過(`internal/deploy/`)。缺口精確集中在 **MCP 這條 primitive 從未接上端到端**。

**Scope 決策(user 2026-07-02):全部四個 target**(claude/codex/copilot/antigravity)。

## 範圍(In Scope)

MCP 端到端四段 × 四 target,**僅限 self-defined MCP server**(`registry: false`,具 command/url):

1. **留存** — `Manifest` 新增 MCP 欄位(prod + dev 皆留存);`manifest.go:355-367` 由「丟棄」改為留存。dependency 套件自身 manifest 的 `mcp:` 亦須可收集。
2. **解析(req-mf-013,per-dispatch-matrix)** — install 時**依 target resolve-vs-preserve 旗標**:
   - **bake**:antigravity/claude/codex — `${VAR}`/`${env:VAR}` 由環境解析為字面。
   - **translate**:copilot — `${VAR}`/`${input:}` **逐字保留**留 runtime;字面值改寫 `${NAME}`。
   - `${{…}}` 一律逐字保留。
3. **收集 + 覆寫** — MCP 併入 deploy pipeline,重用 pr-002/pr-003;root-first + dedup by name;**transitive self-defined server 一律跳過發警告**(本任務不加信任 flag)。
4. **寫出(四 target,格式各異)** — per-target writer + per-server shallow merge(外來鍵保留)+ opt-in 訊號目錄 gating + bake writer 一律 `0600`:

| target | resolve | 檔案(project) | 頂鍵 | http 欄位 | sse 欄位 | 權威 |
|---|---|---|---|---|---|---|
| antigravity | bake | `.agents/mcp_config.json` | `mcpServers` | **`serverUrl`**(http/streamable) | `url` | **oracle** `antigravity.yaml:9` |
| claude | bake | `.mcp.json`(`.claude/` 存在才寫) | `mcpServers` | `type`+`url` | `type`+`url` | apm-cli parity |
| codex | bake | `.codex/config.toml`(TOML) | `mcp_servers` | `url`+`id`+`http_headers` | **跳過發警告** | apm-cli parity |
| copilot | translate | `.github/mcp-config.json` | `mcpServers` | `type:"http"`+`url` | `type`+`url` | **apm-go project 延伸(非 parity)** |

非 https remote 一律跳過發警告(對齊 apm-cli codex 行為,擴及所有 target)。

## 非目標(Out of Scope)

- **registry-backed MCP server 部署**(僅 `name`、需 registry server-info 解析)——發診斷,延到 registry 功能(experimental)。
- **stale server 清除**——apm-cli 有;本任務**延後**(避免誤刪使用者手改),以 diag 記錄孤兒。parity risk 已記。
- dev MCP **部署**(留存但不部署;dev 僅供套件自身開發)。
- 不改 tg/pr 既有非-mcp 行為;不觸碰 oracle(`conformance/**` 唯讀);不做 registry/marketplace/plugin;不做 gemini/cursor/windsurf/vscode/intellij/kiro。
- **不得** gate 在 experimental flag 後(mf-013 phase 1 + mcp deploy phase 4 皆 oracle MUST 且全本地)。

## 需求(Requirements)

- **R1 留存**:`mcp:` 解析結果留存於 `Manifest`(prod+dev,parse 保真);dependency manifest 的 `mcp:` 可被 install 收集。
- **R2 解析-matrix**:mf-013 依 per-target 旗標;bake target 解析、translate target 逐字保留。
- **R3 解析-input**:`${input:<id>}` 於 **bake target** 非互動 install 發診斷 + 拒寫該 server(fail-closed);**translate target 逐字保留**。
- **R4 preserve**:`${{…}}` 逐字保留;resolver 先遮 `ActionsRe` 再套 env/input regex。
- **R5 診斷/undefined**:resolver 用 `lookup(string)(string,bool)` 辨 undefined/empty;bake target undefined 依 position(design §3 權威):env-dict/header → 診斷+omit、registry-list → omit、url → 診斷+refuse、args → 逐字(對齊 apm-cli,不診斷)。除 args 外不靜默留字面。
- **R6 覆寫**:MCP 套用 pr-002/pr-003;transitive self-defined 一律跳過發警告。
- **R7 寫出**:四 target writer,格式依上表;per-server shallow merge、外來鍵保留;bake writer `0600`;sse/streamable/非https 依表處理。
- **R8 registry-backed 診斷**:非 `registry:false` 的 MCP entry 於部署發診斷(不部署),不靜默略過。

## 驗收標準(Acceptance Criteria)

> 裁決:`research/oracle-vs-apmcli-reconcile.md` + `original-apm-mf013-mcp.md`。antigravity writer 對齊 **oracle descriptor**;其餘對齊 **apm-cli parity**;copilot 路徑為 apm-go project 延伸。oracle 無 mf-013/mcp 具體 fixture → 驗證靠 apm-go committed 測試 + 外部 review gate。

- [ ] **AC1 留存**:含 `mcp:`(prod+dev)的 manifest 解析後,`Manifest` 可取回全部 MCP servers;dependency manifest 的 `mcp:` 可收集;既有 `manifest_test.go` 綠燈不退。
- [ ] **AC2 解析-bake**:bake target——`${VAR}`/`${env:VAR}` defined 解析字面;undefined 依 position(R5/design §3):env-dict/header 診斷+omit、registry-list omit、url 診斷+refuse、args 逐字(不診斷)。除 args 外不靜默留字面。
- [ ] **AC3 解析-translate**:copilot——`${VAR}`/`${input:}` 逐字寫出;authored 字面改寫 `${NAME}`;undefined 發警告但 placeholder 仍逐字寫出(runtime 解析)。
- [ ] **AC4 input(bake-only)**:bake target `${input:<id>}` 非互動 → 發診斷 + 拒寫該 server;copilot 逐字保留(負向測試涵蓋兩者)。
- [ ] **AC5 preserve**:含 `${{ matrix.x }}` 的值經 resolver **逐字節不變**;混用 `${VAR}` 只按 target 旗標處理後者。
- [ ] **AC6 覆寫**:local vs dep 同名 local 勝(pr-002);多 dep first-declared 勝(pr-003);transitive self-defined 跳過發警告。
- [ ] **AC7 寫出**:四 target 部署後檔案對齊上表(欄位/路徑/頂鍵);per-server shallow merge、外來鍵保留;bake writer `0600`;sse/非https 依表跳過發警告;golden 測試涵蓋。antigravity E2E 用 `--target antigravity`(explicit-only)。
- [ ] **AC8 lockfile 歸屬**:多 dep 貢獻同一 mcp 檔時,lockfile 記錄該檔 hash 且 per-server source(local | dependency:<name>)可檢視(pr-001);多來源測試涵蓋。
- [ ] **AC9 registry 診斷**:registry-backed MCP entry 部署時發診斷、不寫出、不靜默略過。
- [ ] **AC10 不 gate**:flag 預設 off 時 mf-013 + mcp deploy 仍完整運作(`go test ./...` with 空 `APM_CONFIG_DIR` 綠燈);無 `RequireEnabled` 擋 MCP。
- [ ] **AC11 外部驗證(review gate)**:實作由外部(codex exec)驗 mf-013 per-matrix + 四 writer 格式;acceptance 本身靠 committed 測試,外部驗證為 gate 非 CI 依賴。

## 待 review gate 確認的決策(design.md)
- D1 antigravity `serverUrl`(oracle;真實 antigravity-cli 用 httpUrl,interop 未驗證)。
- D2 copilot project 路徑 `.github/mcp-config.json`(自訂延伸;真實 Copilot CLI 讀 user home,interop 存疑)。
- D3 codex TOML 引入 `pelletier/go-toml/v2`。
- D4 `${input:}` bake-only fail-closed。
- D5 MCP 成 `TypeMCP` primitive。

## 交付定義(Done)

四段 × 四 target(self-defined only)皆有 committed 測試(unit + per-target golden + 負向 + bake/translate 對照 + 多來源 lockfile),`go test ./... -race` 綠燈,覆蓋率 ≥80%,antigravity 對 oracle、其餘對 apm-cli parity,外部 review gate clean。
