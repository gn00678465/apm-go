# Research: opencode MCP parity (apm-go vs Python apm-cli)

- **Query**: apm-go 的 opencodeAdapter 缺少 MCP 部署能力(WriteMCP 寫 opencode.json)。Python 原版 apm-cli 位於 `D:\Projects\apm-dev\apm`,apm-go 位於 `D:\Projects\apm-dev\apm-go`。
- **Scope**: mixed(internal code + Python 原版原始碼)
- **Date**: 2026-07-05

## Findings

### A. Python opencode MCP 語意(`src/apm_cli/adapters/client/opencode.py`)

`OpenCodeClientAdapter(CopilotClientAdapter)` — 繼承 Copilot adapter,只覆寫 config 讀寫與格式轉換,`_format_server_config`(組 command/args/env、http remote 的 server_info→config 轉換邏輯)整段沿用父類 Copilot 版本。

- **設定檔位置**:專案根 `opencode.json`(`get_config_path`, opencode.py:53-55),**不是** `mcp.json`。
- **頂層 key**:`mcp`(opencode.py:1-27 docstring、L72-73),對照 Claude/Codex/Copilot/Antigravity 皆用 `mcpServers`。這是 opencode 專屬的鍵名分歧。
- **Opt-in 目錄閘門**:`update_config`(opencode.py:57-68)與 `configure_mcp_server`(opencode.py:110-112)都先檢查 `self.project_root / ".opencode"` 是否為目錄,**不存在則靜默 return**(`update_config` 回傳 None;`configure_mcp_server` 回傳 `False`)——不寫檔、不報錯。此閘門在 bulk-install 路徑(`mcp_integrator_install.py:217-223, 274-275`)也重複一份:`dir_signal = {"opencode": ".opencode", ...}`,`_runtime_is_present` 用 `(project_root / ".opencode").is_dir()` 判斷 opencode runtime 是否 present,不 present 則整個 opencode 目標被跳過。
- **ResolveMode(bake vs translate)**:`_supports_runtime_env_substitution: bool = False`(opencode.py:47-51,附註解「opencode 的 runtime env 替換支援尚未逐一稽核(#1152),暫沿用 legacy 安裝期解析,之後再議」)。這代表 opencode 走 **legacy/bake 模式**——env 值在安裝當下解析成字面值,不是 emit `${VAR}` 讓 runtime 自行解析。與既有 `original-apm-mf013-mcp.md` 研究記錄一致(該檔 L58-61 列出 `opencode=False`)。
- **`_to_opencode_format`(靜態方法,opencode.py:131-156)**——把 Copilot-shape entry 轉成 OpenCode-shape entry:
  ```python
  entry = {"type": "local", "enabled": enabled}
  cmd = copilot_entry.get("command", ""); args = copilot_entry.get("args", [])
  if cmd:
      entry["command"] = [cmd] + list(args)         # 單一陣列,cmd 併入第一個元素
  elif "url" in copilot_entry:
      entry["type"] = "remote"
      entry["url"] = copilot_entry["url"]
      headers = copilot_entry.get("headers")
      if headers: entry["headers"] = dict(headers)
  env = copilot_entry.get("env") or {}
  if env: entry["environment"] = dict(env)           # key 改名 env → environment
  return entry
  ```
  重點差異(相對 apm-go 現有 4 個 writer 的欄位命名):
  - `command` 是**單一字串陣列**(`[cmd, ...args]`),不是分開的 `command`+`args` 兩個欄位。
  - env 欄位鍵名是 `environment`,不是 `env`。
  - 一律帶 `enabled: true`(bulk-install 與一次性 `apm mcp install` 路徑,`enabled` 皆維持預設 `True`——全 repo 掃過 `configure_mcp_server(...)`/`update_config(...)` 呼叫點,沒有任何地方在標準 CLI 安裝流程中傳 `enabled=False`;唯一 `enabled=0` 的案例是 `copilot_app_workflow_integrator.py:489`,屬於 GitHub Copilot App 的另一套子系統,與 CLI adapters 無關)。
  - remote(http/sse/streamable-http)**統一**用 `type: "remote"` + `url` + 可選 `headers`,**不像 codex 排除 SSE、也不像 antigravity 依 transport 切換 `url` vs `serverUrl`**——因為 Copilot 的 `_format_server_config`(copilot.py:471-479)本身已把三種 remote transport 全部統一寫成 `{"type": "http", "url": ...}`,OpenCode 只看 `"url" in copilot_entry` 就轉,不再區分底層 transport。
- **env 值解析路徑**(`base.py:480-541`,legacy/dict-shape,opencode 走這條):逐 key 呼叫 `_resolve_env_variable`(base.py:543-585)。三種 placeholder 語法(`${VAR}`、`${env:VAR}`、legacy `<VAR>`)皆優先查 `env_overrides` → `os.environ` → (非互動環境跳過)prompt;**若仍未定義,回傳 `match.group(0)`,即把未解析的 placeholder 原樣寫入磁碟**,不是省略該 key。這與 apm-go 既有 bake 模式決策不同(見下方「決策待定點」)。

### B. apm-go 現有 4 個 MCP adapter 模式(`internal/deploy/`)

- **介面**(`adapter.go:26-29`):
  ```go
  type MCPTarget interface {
      MCPResolveMode() manifest.ResolveMode
      WriteMCP(prims []Primitive, projectDir string) (files []string, written []string, diags []string, err error)
  }
  ```
  `Adapters` map(`adapter.go:31-38`)已註冊 `"opencode": &opencodeAdapter{}`,但 `opencodeAdapter`(`internal/deploy/opencode.go:1-26`)**只實作 `TargetAdapter`**(`Name`/`DeployRoots`/`SupportedTypes`/`DeployPrimitive`),**沒有** `MCPResolveMode()`/`WriteMCP()` 方法。
- **呼叫端型別斷言**(`deploy.go:220-223`):
  ```go
  mcpAdapter, ok := adapter.(MCPTarget)
  if !ok { continue }
  ```
  因為 `opencodeAdapter` 目前不滿足 `MCPTarget`,這個型別斷言對 opencode 一律 `false`,迴圈 `continue`——**opencode 目標今天完全不會寫任何 MCP 檔案,連診斷訊息都不會產生**(靜默跳過,非報錯)。
- **共用 helper**(`internal/deploy/mcp_common.go`):
  - `ResolvedMCPServer` 結構(L18-28):`Command string`、`Args []string`(分開兩欄,非陣列合併)、`Env map[string]string`(鍵名固定 `Env`,序列化時各 target writer 自行決定 JSON key 名)、`Headers map[string]string`、`URL`、`Transport`、`Refused`、`Diags`。
  - `resolveMCPServer(s *manifest.MCPDependency, mode manifest.ResolveMode) *ResolvedMCPServer`(L39-113):跑 mf-013(`manifest.ResolvePlaceholders`)解析 args/env/headers/url。**bake 模式下 env 若無 placeholder,不做任何事**(只有 translate 模式才會把裸字面值改寫成 `${key}`,L66-67:`if mode == manifest.ResolveTranslate && !manifest.HasPlaceholder(v) { out = fmt.Sprintf("${%s}", k) }`)。
  - `buildMCPEntries(prims, mode, build mcpEntryBuilder)`(L155-186):對每個 primitive 呼叫 `resolveMCPServer`,再呼叫 target 專屬的 `build` 函式產生 per-server entry map;統一處理 refuse(拒寫)與非 https 遠端跳過。
  - `mcpEntryBuilder` 型別(L150):`func(r *ResolvedMCPServer) (entry map[string]any, ok bool, skipReason string)` —— **這就是新 `mcp_opencode.go` 需要實作的函式簽章**(比照 `antigravityMCPEntry`/`claudeMCPEntry` 等)。
  - `managedMCPKeys`(L213-216):
    ```go
    var managedMCPKeys = map[string]bool{
        "command": true, "args": true, "env": true, "headers": true,
        "url": true, "serverUrl": true, "type": true, "id": true, "http_headers": true,
    }
    ```
    **沒有 `"environment"` 或 `"enabled"`**。這組 key 用在 `mergeMCPServers`(L225-251)判斷「重新部署時哪些既有欄位算 apm-go 自己管理的(該被新值取代/清除),哪些算使用者手寫的外來欄位(要保留)」。若 opencode writer 沿用 Python 的 `environment`/`enabled` 命名而不擴充這個 set,**redeploy 時若某次執行沒有再產生 `environment` 或 `enabled` 欄位(例如 env 清空、或未來加入 disable 語意),舊值會被誤判為「外來鍵」而殘留**,不會被正確清除——這是新增 writer 時要注意的既有共用邏輯耦合點。
  - `writeMergedMCPJSON(path, topKey string, entries, considered, perm)`(L273-292):讀既有檔→依 `topKey`(對 opencode 應為 `"mcp"`)merge→寫回。JSON-based,四個既有 target 有三個(claude/copilot/antigravity)直接用這個 helper;codex 走 TOML 版本(`writeMergedMCPTOML`,mcp_codex.go:49-67)。opencode 應可直接沿用 `writeMergedMCPJSON`,只是 `topKey` 換成 `"mcp"`。
  - `writeFileWithPerm`(L301-309):所有 bake 模式 writer 用 `0600`(claude/codex/antigravity 皆是,mcp_claude.go:19、mcp_codex.go:22、mcp_antigravity.go:19);唯一 translate 模式(copilot)用 `0644`(mcp_copilot.go:21)。opencode 是 bake 模式,依現有慣例應為 `0600`。
- **四個現有 writer 檔案(逐一模式對照)**:

  | target | 檔案 | ResolveMode | 頂層 key | perm | stdio 欄位 | remote 欄位 | 特殊處理 |
  |---|---|---|---|---|---|---|---|
  | claude(`mcp_claude.go:9,25-41`) | `.mcp.json` | Bake | `mcpServers` | 0600 | `type:"stdio"`+`command`+`args`+`env` | `type:<transport>`+`url`+`headers` | — |
  | codex(`mcp_codex.go:11,28-47`) | `.codex/config.toml`(TOML) | Bake | `mcp_servers` | 0600 | `command`+`args`+`env` | `url`+`id`+`http_headers` | **SSE 跳過**(`if r.Transport == "sse" { return nil, false, "SSE transport is not supported by codex; skipped" }`) |
  | copilot(`mcp_copilot.go:9,27-43`) | `.github/mcp-config.json` | **Translate** | `mcpServers` | 0644 | `type:"local"`+`command`+`args`+`env` | `type:<transport>`+`url`+`headers` | project-scoped 自訂延伸,非 apm-cli parity(Python copilot 寫 `~/.copilot/mcp-config.json`,見 mcp_copilot.go:11-14 註解) |
  | antigravity(`mcp_antigravity.go:9,25-46`) | `.agents/mcp_config.json` | Bake | `mcpServers` | 0600 | `command`+`args`+`env` | http/streamable-http→`serverUrl`;sse→`url`;皆可選 `headers` | 依 transport 切換 http 欄位名 |
  | **opencode(缺)** | **`opencode.json`(缺)** | **應為 Bake**(對齊 Python `_supports_runtime_env_substitution=False`) | **`mcp`(非 `mcpServers`)** | **應為 0600** | **`type:"local"`+`command`(單陣列 cmd+args 合併)+`environment`(非 `env`)+`enabled:true`** | **`type:"remote"`+`url`+可選 `headers`(不分 transport)** | **無 SSE 特殊處理(Python 不分 transport)** |

### C. 缺口確認(apm-go 現況 = 完全未實作)

- `internal/deploy/opencode.go`(全 26 行)沒有 `MCPResolveMode()`/`WriteMCP()`,`opencodeAdapter` 不滿足 `MCPTarget` 介面 → `deploy.go:220-223` 型別斷言失敗 → **opencode target 今天不會寫任何 MCP 設定,亦不產生診斷**。
- 全 repo 沒有 `mcp_opencode.go` 檔案(`internal/deploy/` 目前只有 `mcp_claude.go`/`mcp_codex.go`/`mcp_copilot.go`/`mcp_antigravity.go`/`mcp_common.go`/`mcp_writers_test.go`)。
- `internal/deploy/mcp_writers_test.go`(693 行全文已讀)**沒有任何 opencode 相關測試**(全文 grep "opencode" 零命中)——claude/codex/copilot/antigravity 四者皆有專屬 golden/單元測試,opencode 完全缺席。
- Oracle 描述檔 `conformance/conformance-kit/oracle/targets/expected/opencode.yaml`(7 行)**沒有 `mcp:` 欄位**——對照 `antigravity.yaml:9`(唯一有 `mcp: { file, key, http_field, var_interpolation }` 欄位的 target)。`internal/deploy/oracle_test.go:13-24` 定義的 `oracleMCP` struct 支援 `file`/`key`/`http_field`/`var_interpolation` 四欄;`mcp_writers_test.go` 只有 `TestWriteMCP_Antigravity_MatchesOracleDescriptor` 這一個測試會讀 `exp.MCP`(nil 則 `t.Skip`)。若要幫 opencode 補 oracle-gated golden 測試,需要先在 `opencode.yaml` 補 `mcp:` 區塊(`file: opencode.json`、`key: mcp`),但目前 claude/codex/copilot 三個 target 的測試根本不靠 oracle 的 `mcp:` 欄位(直接寫死 shape 斷言),只有 antigravity 這樣做,所以並非必要模式,是否照做屬設計選擇。
- 全 repo 搜尋 "opencode"(大小寫不分)之下,程式碼命中僅:`internal/deploy/adapter.go`(map 註冊 + `allAutoDetectableTargets`)、`internal/deploy/opencode.go`(現有 primitive-only adapter)、`internal/manifest/detect.go`/`detect_test.go`(target 偵測)、`cmd/apm/init.go`。沒有任何 evals 目錄或 `*_test.go` 涵蓋 opencode 的 MCP A/B 對照(`.trellis/tasks/07-05-runtime-parity-gaps/prd.md:41` 已明訂此為 `07-05-opencode-mcp` 子任務交付範圍之一)。
- 舊設計文件 `.trellis/tasks/archive/2026-07/07-02-mcp-resolve-deploy/design.md` §5(L88-96)的「per-target writers」表格**完全沒有 opencode 這一列**——只有 antigravity/claude/codex/copilot 四個,證實 opencode MCP 從一開始的 mf-013 設計階段就未被納入範圍(非後來遺漏,是原始設計就沒排上)。同一份研究(`research/original-apm-mf013-mcp.md:132-137`)已記錄 Python 原版的 opt-in 目錄閘門包含 `.opencode/`,但當時的決策(design.md L97 codex review 意見)是 apm-go **不採用目錄預先存在閘門**——四個既有 writer 皆是 create-on-write,opt-in 語意改由 `ResolveTargets` 的 target 選取機制(`--target`/`targets:`/auto-detect)承擔。這個既有決策若沿用到 opencode,代表 apm-go 版本可能**不需要**複製 Python `.opencode/` 目錄檢查那段邏輯——但這是待新 child task 明確定案的分歧點(見下)。

## 決策待定點(3-5 項,供實作前定案)

1. **`opencode.json` 目錄存在閘門是否比照既有 4 target 慣例(不加閘門)**——Python 原版在 `configure_mcp_server`/`update_config` 內部都會檢查 `.opencode/` 目錄是否存在,不存在則靜默不寫;但 apm-go 既有 4 個 MCP writer(claude/codex/copilot/antigravity)已在 `design.md` 明確決策**不加**任何目錄預先存在檢查(create-on-write,opt-in 語意交給 target 選取機制)。若 opencode writer 沿用 apm-go 自家慣例,就是刻意偏離 Python 行為,需記錄為 deviation。
2. **`managedMCPKeys`(mcp_common.go:213-216)需擴充 `"environment"` 與 `"enabled"`**,否則 opencode 的 redeploy 在某些欄位被省略時(例如 env 由非空變空、或未來加入 disable 語意)會因為這兩個 key 被視為「外來鍵」而不被正確清除/覆蓋。這是共用 helper 的耦合點,不只是新檔案本身的事。
3. **stdio 的 command/args 合併語意**——apm-go `ResolvedMCPServer` 是 `Command string` + `Args []string` 兩個獨立欄位;Python opencode 格式是單一陣列 `command: [cmd, ...args]`。新 `mcp_opencode.go` 的 entry builder 需要自行做這個合併(`append([]string{r.Command}, r.Args...)`),现有四個 writer 都沒有這個轉換前例可以直接抄,需要新寫。
4. **remote transport 是否統一(不分 http/sse/streamable-http)**——Python opencode 對所有 remote transport 一律用 `type:"remote"`+`url`,不像 codex 排除 SSE、不像 antigravity 依 transport 切換欄位名。若 apm-go 新 writer 要對齊 Python,`opencodeMCPEntry` 的 remote 分支不應該有 antigravity 式的 transport 判斷,也不應該有 codex 式的 SSE skip。
5. **`${VAR}`/`${env:VAR}` 在 env 值中未定義時的落盤行為**——Python legacy/bake 路徑(`base.py:583`,`_resolve_env_variable._replace` 回傳 `match.group(0)`)未定義時**把 placeholder 原樣寫入磁碟**;而 apm-go 既有 `resolveMCPServer`(mcp_common.go:57-58)在 `PosEnvDict` 位置未定義時是**診斷+省略該 key**(`omit=true`,不留字面 placeholder)。這是 mf-013 階段已對全部 bake target(claude/codex/antigravity)做出的既有分歧決策,非 opencode 專屬新問題——沿用即可,不需要為 opencode 另外決定,但實作時應注意這不是「先前遺漏」,而是「已知且已套用到其他 3 個 bake target 的既有差異」。

## Caveats / Not Found

- 未在 Python 原版中找到任何 `apm install`(bulk,非 `apm mcp install <server>` 一次性指令)路徑對 opencode 傳入 `enabled=False` 的呼叫點;全 repo grep `configure_mcp_server(`/`update_config(`/`enabled=` 的結果显示標準 CLI 安裝流程一律 `enabled=True`(唯一 `enabled=0` 案例屬 GitHub Copilot App 子系統,語意不同,已排除)。若之後 apm-go 想支援「停用某 MCP server 但保留設定」的語意,目前 Python 與 apm-go 都沒有現成的資料流可以驅動這個欄位——只能寫死 `true`。
- 未驗證 opencode CLI/runtime 實際讀取 `opencode.json` 的行為(黑盒);本研究僅涵蓋 Python `apm-cli` 寫入端與 apm-go 現有程式碼,未對 opencode 本身的原始碼或文件做外部查證。
- 未查核是否有 opencode 的 user-scope(`~/.config/opencode/` 或類似)設定路徑——`OpenCodeClientAdapter.supports_user_scope: bool = False`(opencode.py:43),故 Python 原版本身也不支援 user-scope,無需為 apm-go 額外調查。
