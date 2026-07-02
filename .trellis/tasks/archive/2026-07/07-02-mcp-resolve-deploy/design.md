# Design — MCP install 解析與 target 部署

依 `prd.md`。裁決來源:`research/*`。已納入 codex plan-review(2026-07-02)修正。

## 決策定案(D1-D5)

| # | 決策 | 定案 | 理由 |
|---|---|---|---|
| D1 | antigravity HTTP 欄位 | **`serverUrl`** | oracle descriptor `antigravity.yaml:9` 權威 |
| D2 | copilot 路徑 | **project `.github/mcp-config.json`(apm-go 自訂延伸,非 apm-cli parity)** | 與 project 模型一致、golden 可測(user 定案) |
| D3 | codex TOML | **引入 `github.com/pelletier/go-toml/v2`** | 完整正確 TOML marshal(user 定案) |
| D4 | `${input:}` 非互動 | **bake target 發診斷 + 拒寫該 server;translate target(copilot)逐字保留** | checklist:78「不得靜默」;translate 由 runtime 解析 |
| D5 | deploy 模型 | **新 `TypeMCP` primitive + per-target `MCPTarget` writer 介面** | 重用 `ResolvePrimitives`(pr-002/003 已按 (Type,Name) dedup) |

## 0. 範圍界線(codex CRITICAL 修正)

- **只部署 self-defined MCP**(`registry: false`,具 command/url)。`ParseMCPEntry` 允許 scalar registry entry(只有 `Name`,`mcp.go:23-25`)、`ValidateMCP` 對非 `registry:false` 略過 command/url 驗證(`mcp.go:81-85`)——這類 **registry-backed server 無可部署欄位**。
- registry-backed entry 於 **`deploy.Run` 收集階段發診斷 + 跳過**(不產生 `Primitive`),writer 因此只見 self-defined server(R8/AC9)。延到 registry 功能(experimental)解析 server-info。

## 1. 邊界與資料流

```
manifest 解析 → Manifest.MCPServers / MCPDevServers ([]*MCPDependency)   (R1 留存 prod+dev)
                          │ install: 收集 local prod + 各 dep 的 mcp(self-defined)
                          ▼
   deploy.Run: 收集 TypeMCP(registry-backed 於此診斷+跳過, R8) → ResolvePrimitives(pr-002/003)
                          ▼ per target(實作 MCPTarget 者)
   MCPTarget.WriteMCP(prims []Primitive, projectDir)   ← prims 皆 self-defined
        ├─ 逐值 ResolvePlaceholders(mode = Bake|Translate, pos)   ← mf-013
        ├─ 組 per-target 結構(JSON/TOML)
        └─ 讀既有檔 → per-server shallow merge(外來鍵保留)→ 寫(bake:0600)
                          ▼
   Run 由 prims.Source 建 MCPProvenance;檔 hash 一次(pr-001, §6)
```

## 2. 資料模型(internal/manifest)

- `Manifest` 新增:`MCPServers []*MCPDependency`(prod)、`MCPDevServers []*MCPDependency`(dev)。
- `manifest.go:355-367` `mcp` 分支由丟棄改為收集回傳,依 prod/dev section 存入。
- **dev 政策**:留存(parse 保真)但**不部署**(deploy 只收集 prod)。與現有 `ParsedDeps` vs `ParsedDevDeps` 一致。
- `MCPDependency`(`mcp.go:12`)欄位足夠,不改。

## 3. Placeholder resolver(mf-013)— internal/manifest/mcpresolve.go(新檔)

```go
type ResolveMode int
const ( ResolveBake ResolveMode = iota; ResolveTranslate )
type FieldPos int
const ( PosEnvDict FieldPos = iota; PosArgs; PosRegistryList; PosURL; PosHeader )

// lookup 回傳 (值, 是否定義);refuse=true 表該 server 應被拒寫(D4 bake+input)
func ResolvePlaceholders(value string, mode ResolveMode, pos FieldPos,
    lookup func(string) (string, bool)) (out string, diags []string, refuse, omit bool)
```

- **precedence(codex 實作審查修正,2026-07-02,兩輪)**:「不需遮罩」的簡化**有反例**——`${{…}}` 起始位置雖無法被 `EnvVarRe`/`InputVarRe` 匹配,但若**內容中恰好包含** `${VAR}`/`${input:x}` 形狀子字串(如 `${{ '${input:x}' }}`),regex 掃描整個字串仍會在該子位置命中。第一輪修正用 sentinel(`\x00A<i>\x00`)遮罩還原,但 codex 第二輪抓到**新問題**:`SafeLoad` 不擋 scalar 內的 NUL byte,使用者 manifest 值若恰好含相同 sentinel bytes,全域字串取代會**破壞使用者原始資料**。**最終方案:index-based 排除法**,不做任何字串插入/還原——先用 `ActionsRe.FindAllStringIndex` 找出 `${{…}}` span,`InputVarRe`/`EnvVarRe` 用 `FindAllStringSubmatchIndex` 取得 match 位置,過濾掉與 Actions span 重疊者(`outsideActions` helper),只對通過過濾的 match 做代換。**value 本身從未被改寫**,故無碰撞可能,比 sentinel 方案更簡單也更穩固。
- **Bake**(antigravity/claude/codex)——`${VAR}`/`${env:VAR}` defined → 字面;**undefined 依 position**:
  - `PosEnvDict` / `PosHeader` → 診斷 + `omit=true`(不寫該 key/header,不留字面)。
  - `PosRegistryList` → `omit`,**不發診斷**(對齊 apm-cli:registry env schema 非本地開發者的 authoring surface,原版對此也不 warn,只有 `${input:}` 才 warn)。
  - `PosURL` → 診斷 + `refuse=true`(URL 不可 omit,無法解析即拒寫該 server)。
  - `PosArgs` → 逐字保留、**不診斷**(對齊 apm-cli:args 中 `${VAR}` 即使已定義也不解析)。
  - `${input:<id>}`(任一 position,含 args）→ 診斷 + `refuse=true`(D4),於 `InputVarRe` 判斷階段優先處理,不受 `pos` 短路影響。
- **Translate**(copilot):
  - `${VAR}`/`${env:VAR}` → **逐字寫出**(resolver 不改寫,原樣通過);undefined 不特別處理(runtime 解析,resolver 不需 lookup)。
  - `${input:<id>}` → 逐字寫出(不 refuse)。
  - **authored 字面值(無 placeholder)改寫 `${NAME}` 避免 secret 落盤**:此步驟需要 env key 名稱,`ResolvePlaceholders` 簽章無此參數 → **移至 Step 4 writer 職責**(writer 迴圈 `for key, val := range env` 才知道 key)。resolver 提供 `HasPlaceholder(s string) bool`(沿用 `ActionsRe`/`InputVarRe`/`EnvVarRe`)供 writer 判斷「此值完全無 placeholder → translate mode 需重寫為 `${key}`」。
- regex 沿用 `mcp.go` 現有 `EnvVarRe`/`InputVarRe`/`ActionsRe`(與原版 byte-identical)。

## 4. Deploy 模型(internal/deploy)

- `primitive.go`:`TypeMCP PrimitiveType = "mcp"`;`Primitive` 加 `MCP *manifest.MCPDependency`(非 mcp nil)。
- **收集**(於 `deploy.Run`,MCP 非 `.apm/` 檔案型,不經 `collectFromAPMDir`):
  - local:`m.MCPServers`(僅 prod)。
  - dependency:解析各 `apm_modules/<key>/apm.yml` 的 `mcp:`;**direct(深度1)auto-trust,transitive(深度>1)一律跳過發警告**(不加 flag,codex HIGH 修正)。
  - **registry-backed(`Registry != false`)於此發診斷 + 跳過**(不產生 `Primitive`)——writer 只見 self-defined(R8)。
  - self-defined 產 `Primitive{TypeMCP, Name, Source/DepKey, MCP}` → 併入 `ordered` → `ResolvePrimitives`(pr-002/003)。
- **寫出**:`Run` 對 TypeMCP winners 做 **per-target 一次性** 寫入:

```go
type MCPTarget interface {
    ResolveMode() ResolveMode
    // prims 皆 self-defined;每個 Primitive 自帶 .Source("local"|"dependency:<key>") 與 .MCP
    WriteMCP(prims []Primitive, projectDir string) (files []string, diags []string, err error)
}
```
adapter 實作 `MCPTarget` 才寫;`Run` 收集該 target TypeMCP winners → 呼叫一次。resolution 在 writer 內依 `ResolveMode()`。provenance 由 `Run` 從 `prims[].Source` 建(§6),writer 不回傳 provenance。

## 5. Per-target writers(internal/deploy/mcp_*.go 新檔)

| target | 檔案 | 頂鍵 | mode | http | streamable-http | sse | stdio | perm |
|---|---|---|---|---|---|---|---|---|
| antigravity | `.agents/mcp_config.json` | `mcpServers` | Bake | `serverUrl` | `serverUrl` | `url` | command/args/env | 0600 |
| claude | `.mcp.json` | `mcpServers` | Bake | `type:"http"`+`url` | `type`+`url` | `type`+`url` | `type:"stdio"`+cmd/args/env | 0600 |
| codex | `.codex/config.toml`(TOML) | `mcp_servers` | Bake | `url`+`id`+`http_headers` | `url`+`id` | **skip+warn** | command/args/env | 0600 |
| copilot | `.github/mcp-config.json` | `mcpServers` | Translate | `type:"http"`+`url` | `type`+`url` | `type`+`url` | `type:"local"`+cmd/args/env | 0644 |

- **無目錄 pre-existence gate(codex Review Gate B 判定為文件用詞誤導,實作已更正)**:先前表格寫「opt-in `.claude/`」等字樣,源自 apm-cli 研究(其 project-scope MCP 寫入要求訊號目錄已存在才寫)。但 apm-go 自身既有慣例(`deployFileToPath`,`claude.go`/`codex.go`/`antigravity.go` 用於 instructions/agents/commands 等)**一律 create-on-write、從未檢查訊號目錄是否預先存在**——opt-in 語意已由 `ResolveTargets` 的 target 選取機制(explicit --target / auto-detect / manifest target:)滿足;target 一旦 active,所有 primitive(含 mcp)都應一致地依需建立目錄。加目錄門檻只會讓 MCP 與其餘 primitive 行為不一致,故**不加**,已從表格移除該文字。
- **非 https remote 一律跳過發警告**(對齊 apm-cli codex,擴及所有 target)。
- **merge(codex Review Gate B 修正,HIGH + MEDIUM)**:讀既有 → 對「本次已評估」的 server name(considered,來自傳入的 winners,無論最終寫出或被 refuse/skip)完整重建——寫出者 = 舊外來鍵(managed key 剔除)+ 新 entry;refuse/skip 者 = **整條移除**(不得因 shallow-merge 而讓前次成功寫入的殘留繼續存活,或讓被 omit 的欄位透過「外來鍵保留」語意復活)。未被評估(即不在本次 declared 清單)的既有 server 完全不動(見下 stale 清除)。JSON 用 `encoding/json`;TOML 用 go-toml/v2。**既有檔案存在但解析失敗 → 回傳 error 拒寫**(codex MEDIUM:先前靜默視為空值會覆蓋使用者資料,現在拒絕覆蓋一個看不懂的檔案)。
- **stale 清除**:**本任務不做**(codex HIGH:與 PRD 已一致移出 MUST)——這裡指「manifest 已不再宣告的 server」,與上方 merge 修正(仍宣告但本次 refuse/skip)不同,後者仍會清除。以 diag 記錄未清除孤兒。`ponytail: stale-cleanup deferred; 重裝殘留 server 需手動清,apm-cli 有自動清除(parity gap)`。

## 6. Lockfile / source 歸屬(pr-001,codex HIGH 修正)

- 合併檔(如 `.mcp.json`)由多 dep 貢獻 → **檔案 hash 記錄一次**(沿用 `DepDeployResult.Hashes`),歸於寫入該檔的 target artifact。
- **per-server provenance**:`Run` 於呼叫 `WriteMCP` 後,從 winner `prims[].Source` 建立 `DeployResult.MCPProvenance []MCPProv{Server, Source, File}`(Source = `"local"|"dependency:<key>"`,取自 Primitive)。writer 不負責 provenance——它只寫檔;歸屬由 `Run` 依已知 primitive source 記錄,供 lockfile / `why` 檢視,滿足 pr-001。
- `DeployResult` 新增 `MCPProvenance []MCPProv`;`install.go` 寫入 lockfile。
- 多來源測試:兩 dep 各貢獻一 server 到同一 `.mcp.json` → 檔 hash 正確 + `MCPProvenance` 兩筆(各自 source)可檢視。

## 7. 不 gate(AC10)

MCP 路徑不加 `experimental.RequireEnabled`。負向:空 `APM_CONFIG_DIR` 下 `go test ./...` 綠燈。

## 8. 相容 / rollback

- 新增欄位/檔案,不改既有 tg/pr;既有 deploy golden(`oracle_test.go`)不受影響(`_input` 無 mcp server → 不觸發 mcp 寫入)。
- antigravity explicit-only(`adapter.go:67`):MCP E2E 用 `--target antigravity`(codex MED 修正)。
- rollback:移除 `TypeMCP` 收集 + writers + Manifest 欄位即回現狀;go-toml/v2 僅 codex 使用。

## 9. 測試策略

- unit:`ResolvePlaceholders` 表格(bake/translate × env-dict/args/registry-list × defined/undefined/input/`${{}}`)。
- manifest:`mcp:` prod+dev 留存 round-trip;dependency mcp 收集。
- deploy golden:每 target `_input`(stdio+http+sse)→ `expected` 比對;antigravity 對 oracle descriptor;codex sse skip+warn;非 https skip。
- 負向:bake `${input:}` fail-closed;copilot `${input:}` 逐字;undefined bake omit+診斷;registry-backed 診斷跳過。
- override + lockfile:local vs dep 同名;多來源 provenance + hash。
- 不 gate:空 config 綠燈。
- 外部(AC11):codex exec 驗 mf-013 per-matrix + 四 writer。

## 10. 風險

- D1 `serverUrl` vs 真實 antigravity-cli `httpUrl` → conformance-correct,interop 未驗證。
- D2 copilot project 路徑自訂 → 真實 Copilot CLI 讀 user home,interop 存疑。
- codex bake secret 落 `.codex/config.toml`(0600 緩解)。
- stale-cleanup deferred → 重裝殘留(parity gap,已記)。
