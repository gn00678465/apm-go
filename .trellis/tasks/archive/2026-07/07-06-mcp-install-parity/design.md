# Design — install --mcp parity（#1 block style + #2 憑證互動）

對照基準：Python 原版。已定案決策（來自規劃問答）：
- **#2 詢問對象**：針對 registry remote 宣告的 required header 詢問；`Authorization`
  類（secret）→ prompt token → 組 `Bearer <token>` 注入 header。務實對齊 apm-go
  remote-only 模型，不觸碰 OCI/stdio runtimeArguments。
- **#2 持久化**：只注入本次 deploy；apm.yml 維持 registry bare string，**不寫 secret**。

## 邊界與現有流程（`cmd/apm/mcpinstall.go: runMCPInstall`）

```
read apm.yml → ParseManifest
buildPersistEntry(opts)      → entryNode（寫進 apm.yml 的值）
upsertMCPEntry(...)          → status(added/replaced/unchanged)   ← #1 改這裡
  unchanged → 印訊息 return
buildDeployDep(opts)         → deployDep(+diags)                  ← #2 改這裡(registry 分支)
  registry 分支 = resolveFromRegistry()
PatchMappingPath → WriteFile apm.yml   （entryNode 決定內容，#2 不動它）
deployMCPEntry(m, target, deployDep)   （deployDep.Headers 會被各 adapter 寫入 target）
```

關鍵事實（已查證）：
- `entryNode`（apm.yml）與 `deployDep`（部署）是**兩條獨立資料流**；#2 只碰後者即可
  達成「只部署、apm.yml 不變」。
- `manifest.MCPDependency.Headers map[string]string` 會被 claude/codex/antigravity/
  common adapter 寫入各 target MCP 設定 → 注入 `deployDep.Headers` 即生效。

## #1 — apm.yml block style

**改動點**：`upsertMCPEntry`（`cmd/apm/mcpinstall.go`）。

在 add / replace 兩個分支，對 `mcpSeq` 清掉 flow bit：

```go
mcpSeq.Style &^= yamllib.FlowStyle   // 繼承自 init 的空 `mcp: []` flow → 轉 block
```

- `unchanged` 分支不寫檔，不需處理。
- 僅影響 `dependencies.mcp` 序列節點；`dependencies.apm`、註解、其他位元組不變
  （`PatchMappingPath` 既有 surgical patch 行為不變）。
- 根因與修法已在專案 module 內以 go-yaml v4 實測驗證。
- **不改 `init.go`**：go-yaml 對空序列一律輸出 `[]`（flow），無法在 init 端產生 block
  空序列；正確修法在「mutation 當下」清 flow，同時涵蓋 init 既存與未來檔案。

## #2 — registry remote 憑證互動詢問

### 新增純函式（可測，不碰 TTY）
`cmd/apm/mcp_prompt.go`：
```go
// collectHeaderValues 依 requiredHeaders 逐一向 ask 取值並組成 header map。
// ask(label, secret) 回傳使用者輸入（"" = 略過該 header）。
// - Authorization（不分大小寫）：label="token", secret=true, value="Bearer "+輸入
// - 其他：label=headerName, secret=looksSecret(name), value=原始輸入
func collectHeaderValues(requiredHeaders []string, ask func(label string, secret bool) string) map[string]string

func looksSecret(name string) bool // 含 token/key/secret/password/api（不分大小寫）
```

### 互動閘門
```go
func canPromptCreds() bool { return isInteractive() && !isNonInteractiveEnv() }
func isNonInteractiveEnv() bool // APM_E2E_TESTS / CI / GITHUB_ACTIONS / TRAVIS / JENKINS_URL / BUILDKITE 任一存在
```
- `isInteractive()` 沿用 `init.go` 既有實作。
- 非互動（管線 / CI / e2e）→ 不 prompt、不阻塞；既有測試以管線 stdin 執行 → 走此路徑
  → 行為不變、綠燈。

### 隱藏輸入
- 新增相依 `golang.org/x/term`（標準、輕量），用 `term.ReadPassword(int(os.Stdin.Fd()))`
  讀取 secret（跨平台不回顯）；非 secret 用既有 `getScanner()`。
- 安全：不 echo、不 log token 值；錯誤訊息不含輸入內容（比照 `parseKVPairs` 既有註記）。

### 接線（`resolveFromRegistry`）
`ResolveDeployable` 回傳 `requiredHeaders` 之後：
```go
if len(requiredHeaders) > 0 && canPromptCreds() {
    hdrs := collectHeaderValues(requiredHeaders, ttyAsk) // ttyAsk 用 ReadPassword/scanner
    if len(hdrs) > 0 { dep.Headers = mergeHeaders(dep.Headers, hdrs) }
}
if len(requiredHeaders) > 0 && len(dep.Headers) == 0 {
    // 只有在「沒收到憑證」時才印既有診斷（避免互動已輸入還被 nag）
    diags = append(diags, 既有 "requires header(s) ... add --header ..." 訊息)
}
```
- `dep.Headers` 只進 `deployDep`；`entryNode`/apm.yml 不動（bare string 保留）。
- github 案例：`Authorization`(isSecret) → prompt「token」→ `Bearer <token>` → 寫入
  各 target MCP 設定（`.vscode/mcp.json`、claude 設定等），安裝後即可用。

### 已知 deviation（記錄於 A/B）
- 原版 prompt 的 `token` 源自 OCI runtimeArguments 並用於 docker `GITHUB_PERSONAL_
  ACCESS_TOKEN`；apm-go 改為對 remote `Authorization` header 詢問並組 Bearer。**行為
  等價（都取得使用者 PAT 並讓 server 認證），來源與注入位置不同**。
- 不實作原版「已設某 env 變數就沿用」——remote header 無 registry 宣告的 env 變數名可對；
  apm-go 以「非互動不 prompt」＋「未輸入則不注入(unauth)」對齊原版「未提供則不帶 auth」
  的最終效果。

## D2 — 衝突條目互動 confirm-replace

**改動點**：`upsertMCPEntry` 加一個 confirm callback 參數，決策移交 caller（保持可測）。

```go
type confirmReplaceFunc func(name string, diff []string) (bool, error) // nil = 非互動

func upsertMCPEntry(doc *yamllib.Node, name string, entryNode *yamllib.Node,
    force bool, confirm confirmReplaceFunc) (status string, err error)
```
found && different 分支：
- `force` → 取代（"replaced"，清 FlowStyle）。
- `confirm == nil`（非互動）→ error `"MCP server %q already exists in apm.yml. Use --force to replace (non-interactive)."`（apm.yml 不動）。
- 否則 → `ok, err := confirm(name, diffEntry(existing, entryNode))`；`!ok` → `"skipped"`（不改節點）；`ok` → 取代（"replaced"，清 FlowStyle）。

新增純函式：
```go
func diffEntry(old, new *yamllib.Node) []string // port _diff_entry：bare↔bare→"old -> new"；mapping→"k: old -> new"，缺值 <absent>
```

`runMCPInstall` 接線：
- confirm callback：`isInteractive()` 為真時提供一個「印 `MCP server "X" already exists. Replacement diff:` + 各 diff 行，讀 `Replace "X"? [y/N]`(預設 N)」的實作（沿用 `getScanner`/`confirmPrompt` 風格）；否則傳 `nil`。
- 新增 `status == "skipped"` 處理：比照 `unchanged` 印 `[i] MCP server "X" unchanged` 並 return（不 deploy）。
- 既有 4 個 `upsertMCPEntry` 測試呼叫點與生產呼叫點補上第 5 參數（衝突測試傳 `nil` → 維持 "use --force" 期望）。

## 相容性 / 風險
- 既有測試：#2 全部走非互動閘門 → 不受影響；#1 只改序列 style，不改語意。
- registry 無憑證需求的 server：`requiredHeaders` 空 → 兩項改動皆無副作用。
- rollback：兩處改動互相獨立，可各自 revert（#1 一行、#2 一組新函式+接線）。

## 測試策略
- #1：unit — 對含 `mcp: []`(flow) 的 apm.yml upsert bare-string / mapping 條目，斷言
  輸出為 block、其他內容不變。
- #2：unit — `collectHeaderValues` 以 stub ask 覆蓋 Authorization→Bearer、其他 header、
  空輸入略過、looksSecret 判斷；`isNonInteractiveEnv` env 矩陣。
- 迴歸：`go test ./...` 全綠；覆蓋率 ≥ 80%。
- A/B：`D:\Projects\apm-dev\evals` 腳本對照 `uv run apm` 與 apm-go 的 apm.yml 輸出與
  互動流程，deviation 明列。

## M8 — 憑證佔位 per-target header 編碼（bake→translate）

決策：DEC-1=B（codex 用 env 專屬欄位）、DEC-2/3=只改 remote header（env dict 維持 bake）。
研究：`research/cred-placeholder-parity.md`。

### 拆開 header / env 的 resolve mode
`internal/deploy/mcp_common.go`：`resolveMCPServer(s, mode)` → `resolveMCPServer(s, envMode, headerMode)`。
- args / env / url 用 `envMode`（維持現狀）；**headers 用 `headerMode`**。
- `buildMCPEntries(prims, mode, build)` → `buildMCPEntries(prims, envMode, headerMode, build)`。
- 各 adapter 呼叫：
  - claude / codex / opencode：`(Bake, Translate)`（env 續 bake、header 保留 `${VAR}`）
  - copilot：`(Translate, Translate)`（不變）
  - antigravity：`(Bake, Bake)`（不變，非本輪範圍）

header 經 `headerMode=Translate` 後 `r.Headers` 保留 `${VAR}`（不讀 os.environ）。各 adapter 的
entry builder 再依自家語法「編碼」header：

### 各 adapter header 編碼
- **claude**（`claudeMCPEntry`）：`r.Headers` 直接放入 `headers`（`${VAR}` 原生支援），不變。
- **opencode**（`opencodeMCPEntry`）：新增 `translateOpencodeHeaders`：`${VAR}` / `${env:VAR}`
  → `{env:VAR}`（regex，idempotent），套用於 `headers`（env dict 本輪不動）。
- **codex**（`codexMCPEntry`）：新增 `encodeCodexHeaders`，把 `http_headers` 拆成三桶：
  - `Authorization: Bearer ${VAR}`（或 `${env:VAR}`）→ `bearer_token_env_var = "VAR"`。
  - header 值**恰為** `${VAR}` → `env_http_headers[name] = "VAR"`。
  - 其餘（無佔位的靜態值）→ `http_headers[name] = value`。
  - 含佔位但無法乾淨對映（如 `X-Foo: pre ${VAR} post`）→ 保留進 `http_headers` + 診斷。

### 安全
三 target 部署設定檔不再從 os.environ 烘 header secret；header 以佔位 / env-ref 呈現
（#1152 精神）。env dict 的既有 bake 行為不變（非本輪，已與原版 parity）。

### 相容 / 測試
- 只有 claude/codex/opencode 的 **header** 行為改變；env/args/url、copilot、antigravity 不變。
- 既有 deploy 測試中斷言「header 烘 literal」者須改為新行為（保留 `${VAR}` / `{env:VAR}` /
  codex 三桶）。新增各 adapter header 編碼的 unit test（含 github Authorization 案例）。
- E2E：`--header 'Authorization=Bearer ${MY_TOKEN}'` 部署後三 target 檔案符合 mp-V01~V03。
