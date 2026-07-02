# Design: apm install --mcp CLI 旗標

## 架構總覽

`--mcp` 是獨立於現有 `runInstall` 主流程的一次性操作:解析 flags → 驗證衝突 → 解出一個 `*manifest.MCPDependency`(自訂或 registry 查詢)→ upsert 進 `apm.yml` → 直接呼叫既有 per-target `WriteMCP` 部署。**不**碰 `apm.lock.yaml`,**不**跑 `resolver.Resolve`/`buildLockfile`/`deployAndFinalize`。這對齊原版 `_handle_mcp_install`/`run_mcp_install` 是一條與完整 install pipeline 平行的獨立路徑。

```
runInstall(deps, frozen, noProvenance, targetFlag, skillSubset, packages, mcpFlags)
  └─ if mcpFlags.Name != "": return runMCPInstall(mcpFlags, targetFlag)   // 提早分流,不進 1b 之後的任何步驟
```

## 1. CLI Flags(`cmd/apm/install.go` `installCmd()`)

```go
var mcpName string
var mcpTransport string
var mcpURL string
var mcpEnvPairs []string
var mcpHeaderPairs []string
var mcpVersion string
var mcpRegistry string
var force bool

cmd.Flags().StringVar(&mcpName, "mcp", "", "add an MCP server entry to apm.yml and deploy it (mutually exclusive with positional packages and --skill)")
cmd.Flags().StringVar(&mcpTransport, "transport", "", "MCP transport: stdio, http, sse, streamable-http (requires --mcp)")
cmd.Flags().StringVar(&mcpURL, "url", "", "MCP server URL for http/sse/streamable-http transports (requires --mcp)")
cmd.Flags().StringArrayVar(&mcpEnvPairs, "env", nil, "environment variable KEY=VALUE for a stdio MCP server, repeatable (requires --mcp)")
cmd.Flags().StringArrayVar(&mcpHeaderPairs, "header", nil, "HTTP header KEY=VALUE for a remote MCP server, repeatable (requires --mcp and --url)")
cmd.Flags().StringVar(&mcpVersion, "mcp-version", "", "pin the MCP registry entry to a specific version (requires --mcp)")
cmd.Flags().StringVar(&mcpRegistry, "registry", "", "MCP registry URL for resolving --mcp NAME (requires --mcp; not valid with --url or a stdio command)")
cmd.Flags().BoolVar(&force, "force", false, "overwrite a conflicting existing --mcp entry non-interactively")
```

stdio command 讀 `cmd.ArgsLenAtDash()`:cobra 遇到 `--` 會把它之後的 token 全部塞進 `args`(positional),`ArgsLenAtDash()` 回傳 `--` 前的 positional 數量,兩者相減就是 stdio command argv。這比原版手動重解析 `os.Args` 乾淨——cobra 原生支援這個切法,不需要 workaround。

```go
RunE: func(cmd *cobra.Command, args []string) error {
    dashAt := cmd.ArgsLenAtDash()
    var prePackages, stdioCommand []string
    if dashAt >= 0 {
        prePackages = args[:dashAt]
        stdioCommand = args[dashAt:]
    } else {
        prePackages = args
    }
    if mcpName != "" {
        return runMCPInstall(deps, mcpInstallOpts{
            Name: mcpName, Transport: mcpTransport, URL: mcpURL,
            EnvPairs: mcpEnvPairs, HeaderPairs: mcpHeaderPairs,
            Version: mcpVersion, Registry: mcpRegistry, Force: force,
            Command: stdioCommand, PrePackages: prePackages,
            SkillSubset: skillFlags, TargetFlag: targetFlag,
        })
    }
    if dashAt >= 0 {
        return fmt.Errorf("'--' stdio command syntax is only valid with --mcp")
    }
    return runInstall(deps, frozen, noProvenance, targetFlag, skillFlags, args)
},
```

## 2. 衝突驗證(`cmd/apm/mcpinstall.go` 新檔,`validateMCPConflicts`)

依原版 E-matrix,只搬移與本任務 flag 集合相關的規則(對照 prd.md §範圍 2):

| 規則 | 條件 | 錯誤訊息 |
|---|---|---|
| 名稱必填 | `Name == ""` | `"--mcp requires a server name"` |
| 名稱格式 | `strings.HasPrefix(Name, "-")` | `"--mcp name cannot start with '-'; did you forget a value for --mcp?"` |
| 與 positional 互斥 | `len(PrePackages) > 0` | `"cannot mix --mcp with positional packages"` |
| 與 --skill 互斥 | `len(SkillSubset) > 0` | `"--skill cannot be combined with --mcp"` |
| requires --mcp | 呼叫端保證(見下)| `"--%s requires --mcp"` |
| --header 需要 --url | `len(HeaderPairs) > 0 && URL == ""` | `"--header requires --url"` |
| --url 與 stdio 互斥 | `URL != "" && len(Command) > 0` | `"cannot specify both --url and a stdio command"` |
| stdio transport 配 --url | `Transport == "stdio" && URL != ""` | `"stdio transport doesn't accept --url"` |
| remote transport 配 stdio | `Transport in {http,sse,streamable-http} && len(Command) > 0` | `"remote transports don't accept a stdio command"` |
| --registry 與 self-defined 互斥 | `Registry != "" && (URL != "" \|\| len(Command) > 0)` | `"--registry only applies to registry-resolved MCP servers; remove --url or the stdio command, or drop --registry"` |

**requires-mcp 檢查**在 cobra flag 定義層做不到(cobra 沒有原生 flag 依賴宣告),改在 `RunE` 最前面手動檢查:`mcpName == "" && (mcpTransport != "" || mcpURL != "" || len(mcpEnvPairs) > 0 || len(mcpHeaderPairs) > 0 || mcpVersion != "" || mcpRegistry != "" || dashAt >= 0)` → 逐一報出第一個違規的 flag 名稱。

**不搬移**:`--global`/`--ssh`/`--https`/`--allow-protocol-fallback`/`--update` 相關(apm-go 沒有這些 flag)、`--only apm`(apm-go 沒有 `--only`)。

## 3. `manifest.MCPDependency` 擴充

```go
type MCPDependency struct {
    Name      string
    Transport string
    Command   string
    Args      *[]string
    URL       string
    Env       map[string]string
    Headers   map[string]string
    Registry  any    // nil=default(registry-backed), false=self-defined, string=custom registry URL
    Version   string // NEW: --mcp-version pin, only meaningful when Registry != false
}
```

`ParseMCPEntry` 加一個 `case "version": m.Version = v.Value`。`ValidateMCP` 不需要改——`Version` 只在 registry-backed 分支有意義,現有函式只驗證 self-defined(`Registry == false`)分支,天然不受影響。

## 4. `build_mcp_entry` 對應邏輯(`cmd/apm/mcpinstall.go` `buildMCPEntry`)

輸入:`mcpInstallOpts`(已通過衝突驗證)。輸出:`(persistEntry any, deployDep *manifest.MCPDependency, err error)`——`persistEntry` 是要寫進 apm.yml 的值(可能是純字串或 map),`deployDep` 是要拿去部署的、**保證有 concrete transport/url/command** 的完整物件。

三分支,對齊原版:

```go
switch {
case len(opts.Command) > 0:
    // self-defined stdio
    dep := &manifest.MCPDependency{Name: opts.Name, Registry: false, Transport: "stdio", Command: opts.Command[0]}
    if len(opts.Command) > 1 { args := opts.Command[1:]; dep.Args = &args }
    if len(opts.EnvPairs) > 0 { dep.Env = parseKVPairs(opts.EnvPairs) }
    return mcpEntryToYAMLValue(dep), dep, manifest.ValidateMCP(dep)

case opts.URL != "":
    transport := opts.Transport
    if transport == "" { transport = "http" }
    dep := &manifest.MCPDependency{Name: opts.Name, Registry: false, Transport: transport, URL: opts.URL}
    if len(opts.HeaderPairs) > 0 { dep.Headers = parseKVPairs(opts.HeaderPairs) }
    return mcpEntryToYAMLValue(dep), dep, manifest.ValidateMCP(dep)

default:
    // registry lookup -- persistEntry is bare-name-or-dict WITHOUT the resolved url;
    // deployDep is built from the registry response (§5), Registry left nil (registry-backed)
    // for persistence but the deployDep itself is what actually gets written to disk this
    // call, matching the original's "resolve fresh every --mcp call" semantics.
    resolved, err := resolveFromRegistry(opts)  // §5
    if err != nil { return nil, nil, err }
    persistEntry := buildRegistryPersistEntry(opts) // {name, transport?}/{name, version}/{name, registry}/bare name
    return persistEntry, resolved, nil
}
```

`mcpEntryToYAMLValue`/`buildRegistryPersistEntry` 回傳可以是 `string`(裸名稱)或 `map[string]any`(其餘所有情況),交給下一步的 YAML writer 判斷型別後寫節點。

## 5. MCP Registry v0.1 Client(新套件 `internal/mcpregistry`)

```go
package mcpregistry

const DefaultBaseURL = "https://api.mcp.github.com"

type Client struct {
    BaseURL string
    HTTP    *http.Client
}

func NewClient(baseURL string) (*Client, error) // 驗證 scheme http/https、長度上限 2048、正規化 rstrip "/"

type ServerInfo struct {
    ID          string
    Name        string
    Remotes     []Remote
    HasPackages bool // true if the response also carried a non-empty packages[]
}
type Remote struct {
    TransportType   string // "http", "sse", "streamable-http" (blank -> "http")
    URL             string
    RequiredHeaders []string // header NAMES the registry says the server needs; no values (see below)
}

// FindServerByReference: search -> exact match -> namespace-boundary fuzzy fallback -> get server (version or "latest").
// Returns (nil, nil) on no match (caller turns that into "not found in registry" -- not an HTTP error).
func (c *Client) FindServerByReference(ctx context.Context, reference, version string) (*ServerInfo, error)
```

**Search**:`GET {base}/v0.1/servers?search={reference}`。**Get**:`GET {base}/v0.1/servers/{urlEncode(name)}/versions/{urlEncode(version)}`(`version` 空字串 → `"latest"`)。回應解析只認 `{"servers": [{"server": {...}}]}` / `{"server": {...}}` 這個 v0.1 巢狀結構,不做 legacy 欄位別名(那是原版為了相容更舊 registry 版本留的,apm-go 是全新實作,不需要背這個相容包袱——**唯一** deviation:只認 v0.1 shape,若遇到非 v0.1 回應直接報錯而非嘗試多種相容解析,design 上更簡單也更容易驗證正確性)。

**Fuzzy match**(`isServerMatch`,對齊原版邏輯):`reference` 含 `/` 時,`server.Name` 必須以 `"." + reference` 或 `reference` 結尾且該結尾前一個字元是 `.` 或字串開頭(namespace 邊界,避免 `microsoftdocs/mcp` 誤配到 `com.supabase/mcp`)。不含 `/` 時退化成比對 `server.Name` 最後一個 `/` 之後的 slug。

**只認 `Remotes`**:若 `ServerInfo.Remotes` 為空(表示只有 `packages[]`,即 npm/docker 型 stdio 套件),回傳明確錯誤:`fmt.Errorf("MCP server %q only provides package-based (stdio) installation, which apm-go does not yet support; declare it manually with --command", reference)`。

**Remote 選擇**:第一個 `URL != ""` 的 remote(對齊原版 `_select_remote_with_url`)。`TransportType` 空字串正規化為 `"http"`;只接受 `http/sse/streamable-http`,其餘報錯。**不**做原版的「所有 remote 一律降級成 `type: http`」——apm-go 既有 writer(`mcp_claude.go` 等)已經知道怎麼處理 http/sse/streamable-http 三種 transport(是既有 self-defined 路徑本來就支援的),沒有理由在這裡強制降級,維持 registry 回應的原始 transport 類型更準確。這是**刻意的行為差異**,design 理由:apm-go 的 writer 早於本任務就正確處理三種 transport,原版的降級是 Copilot/Claude adapter 的歷史包袱,不是 spec 要求。

**Headers(design 修正,對照真實 API 回應後發現原假設有誤)**:直接打 `curl https://api.mcp.github.com/v0.1/...` 驗證後發現 `remotes[].headers[]` 是**需求描述**(`{name, description, isSecret}`),**沒有 `value` 欄位**——不是最初以為的字面 key/value 可以直接複製到 `dep.Headers`。改成:`Remote.RequiredHeaders []string` 只記錄需要哪些 header 名稱(如 `"Authorization"`),`dep.Headers` 一律維持 `nil`(apm-go 不做 GitHub token 自動注入,沒有值可以填,填空字串比不填更誤導),`resolveFromRegistry`(§4/步驟 5)在有 `RequiredHeaders` 時印一行診斷提醒使用者可能要手動補 `--header`。也順便修正了 `transport_type` 應為 `type` 的 JSON tag 誤植(同樣是對照真實回應才發現,`internal/mcpregistry/client_test.go` 的 `TestFindServerByReference_RealRegistryResponseShape` 用真實回應內容鎖定這兩個修正)。

## 6. apm.yml Upsert Writer(`cmd/apm/mcpinstall.go` `upsertMCPEntry`)

新函式,仿照 `install.go` 既有 `persistPackagesToManifest` 的 yaml.Node 操作風格,但目標是 `dependencies.mcp`(或 `--dev` 時 `devDependencies.mcp`)**序列**,不是 `dependencies.apm`:

1. 找/建 `dependencies`(或 `devDependencies`)mapping node → 找/建 `mcp` sequence node。
2. 逐一走訪既有條目(string 或 map 都要能取出 `name`),找同名項目。
3. 沒找到 → append 新節點,狀態 `"added"`。
4. 找到且新舊值語意相同(`reflect.DeepEqual` 正規化後的 Go 值,而非 YAML 節點逐字比較——順序無關的 map 比較)→ 狀態 `"unchanged"`,**完全不寫檔**(比照原版「identical entry 不動 apm.yml」)。
5. 找到且不同、`Force == false` → 回傳 error(不修改 node),caller 保證這個 error 發生在**任何檔案寫入之前**。
6. 找到且不同、`Force == true` → 原地替換該節點,狀態 `"replaced"`。

回傳 `(status string, err error)`,caller 只在 `status != "unchanged"` 時才真的呼叫 `yamlcore.Save`(或既有的等效輸出函式——需要在實作時確認 apm-go 現有的 YAML 節點寫回檔案的既有 helper 名稱並重用,不重新發明)。

## 7. 部署(`cmd/apm/mcpinstall.go` `deployMCPEntry`)

```go
func deployMCPEntry(m *manifest.Manifest, targetFlag string, dep *manifest.MCPDependency) (deployedTargets, skippedTargets []string, err error) {
    targets, targetDiags := deploy.ResolveTargets(targetFlag, m.Target, ".")
    for _, d := range targetDiags { fmt.Fprintln(os.Stderr, d) }
    prims := []deploy.Primitive{{Name: dep.Name, Type: deploy.TypeMCP, Source: "local", MCP: dep}}
    for _, t := range targets {
        adapter, ok := deploy.Adapters[t]
        if !ok { continue }
        mcpAdapter, ok := adapter.(deploy.MCPTarget)
        if !ok { skippedTargets = append(skippedTargets, t); continue }  // target doesn't support MCP (e.g. agent-skills)
        files, written, diags, err := mcpAdapter.WriteMCP(prims, ".")
        for _, d := range diags { fmt.Fprintf(os.Stderr, "[!] %s\n", d) }
        if err != nil { return deployedTargets, skippedTargets, fmt.Errorf("deploy to %s: %w", t, err) }
        if len(written) > 0 { deployedTargets = append(deployedTargets, t) }
    }
    return deployedTargets, skippedTargets, nil
}
```

重用 `deploy.ResolveTargets`(現有函式,不新增)、`deploy.Adapters`(現有 map)、`deploy.MCPTarget`/`WriteMCP`(現有介面,四個既有 writer 檔案完全不改)。`Primitive.Source = "local"` 對齊「這是這個專案自己宣告的」語意(對照既有 `collectMCPPrimitives` 對 local MCP 的 source 標記)。

**與原版的刻意差異**:原版 `--mcp` 路徑完全不把 `--target` 傳進 gate 函式(只能靠 apm.yml `targets:` 或自動偵測)——research agent 已確認這是原版的 bug,不是設計。apm-go 這裡讓 `--target` 正常生效(直接傳進 `deploy.ResolveTargets`,跟既有 `runInstall` 完全一致的 precedence),AC 與 A/B 測試**不**要求重現這個 bug;A/B 腳本針對「有 `.claude/` 等真實 marker」的情境比對即可,`--target` 顯式覆寫的情境只驗證 apm-go 自身行為正確,不強求原版逐字對齊(因為原版那條路是壞的)。

## 8. stdout 格式(`runMCPInstall` 主體)

比照 apm-go 既有慣例(`install.go` 已有的 `[i]`/`[!]` 前綴):

```
[i] Targets: claude  (source: auto-detect)
[+] Added MCP server 'io.github.github/github-mcp-server'
  transport: http
  apm.yml: apm.yml
```

`"[i] Skipped MCP config for %s  (active targets: %s)"`(維持原版雙空格排版,方便未來若真的要做逐字元比對時容易對齊)當 `deployMCPEntry` 回傳非空 `skippedTargets`。`verb` 是 `"Added"`/`"Replaced"`/`"unchanged"` 三選一,對齊 upsert 狀態。

**不**印原版那個「無論成功失敗都印」的 `[!] Install interrupted after Ns.` 計時 footer——那是原版整個 `install()` 指令共用的 `finally` block,apm-go 的 `--mcp` 是完全獨立的小函式,沒有對應的計時基礎設施,而且這行對 A/B 驗證沒有實質資訊量(純粹計時),不列入必須比對的關鍵行。

## 9. 錯誤處理與退出碼

全部走 Go 慣例:`runMCPInstall` 回傳 `error`,`main.go` 的既有機制(cobra `RunE` 回傳非 nil error → 非 0 exit code)已經處理,不需要額外設計。**不**區分原版的 exit 1 vs exit 2(UsageError vs 其他)——apm-go 現有其他指令(`--skill` 的 fail-loud guard 等)也沒有這個區分,保持一致,一律非 0。

**AC6 的刻意收斂**(比原版更嚴謹):原版「registry 查無 server」時,apm.yml 已經寫入、只有部署失敗,產生一個「宣告了但沒東西可用」的中間態(見 research §6b)。apm-go 版本**先做 registry 解析,解析失敗就直接回傳 error,完全不碰 apm.yml**——`runMCPInstall` 的呼叫順序是「先 `buildMCPEntry`(含 registry 解析,§4)成功 → 才呼叫 `upsertMCPEntry`(§6)」,而不是原版的「先寫 apm.yml → 才解析/部署」。這個順序反轉是本任務唯一一處主動修正原版行為的地方,design 理由記錄於此:寫入使用者的 `apm.yml` 前就該確保這個宣告是可用的,不留半殘狀態。

## 10. A/B 測試腳本

**設計變更(實作後調整)**:原規劃是 apm-go repo 內的 `cmd/apm/mcpinstall_ab_test.go`(Go test,`t.Skip` 條件式跳過)。使用者事後要求移除 apm-go repo 內的 A/B 測試,改放到 `D:\Projects\apm-dev\evals`——這個目錄已有 `ab_phase0.py`/`ab_phase1.py` 建立的慣例(standalone Python script,無 pytest/unittest 框架,`subprocess` 呼叫已編譯的 `apm-go.exe`,自訂 `result()`/`skip()` 計數器,最後印總結 + exit code)。因為 `apm install --mcp` 是完整 CLI 操作(寫檔、真實網路查詢、部署),不是 phase0/1 那種「呼叫 Python 函式比對資料結構」的窄範圍檢查,兩側都用 `subprocess`(apm-go 呼叫編譯好的二進位,原版用 `uv --project <apm-py-path> run apm`)。

新增 `D:\Projects\apm-dev\evals\ab_mcp_install.py`。每個情境:

1. 在乾淨 temp dir(`tempfile.mkdtemp`)建立最小 `apm.yml` + 目標 marker(`.claude/`)。
2. 分別跑 `uv --project <apm-py-path> run apm install --mcp ...`(獨立 temp dir)與 `apm-go.exe install --mcp ...`。
3. 比對:exit code 是否同為 0/非 0、apm.yml 的 `dependencies.mcp` 內容(子字串比對)、部署出的 target 檔案內容(`.mcp.json` 的 `url`/`type`/`command`/`args` 欄位,解析 JSON 後結構化比對)。

前置條件(script 開頭直接檢查,不通過就 `sys.exit` 帶清楚訊息,不是靜默略過):`bin/apm-go.exe` 必須已編譯、`uv` 必須在 PATH 上、`apm` Python checkout 必須存在。這是獨立於 apm-go 自身 `go test ./...` 的手動驗證腳本,不掛在 CI/一般測試流程上(比照 `evals/` 既有 `ab_phase0.py`/`ab_phase1.py` 的定位——這整個目錄本身就是「手動 A/B 驗證」的角色,不是自動化測試套件)。

已實測 5 個情境、15 個斷言全數 PASS(見 implement.md 步驟 9)。

## Rollback

新增檔案為主(`internal/mcpregistry/*.go`、`cmd/apm/mcpinstall*.go`),`install.go` 只新增 flag 定義與 `RunE` 開頭的一個分流判斷,`manifest/mcp.go` 只新增一個欄位——移除 `--mcp` 相關程式碼即可完整回退,不影響既有 `runInstall`/`buildLockfile`/`deployAndFinalize`/現有 MCP writer 的任何既有行為。
