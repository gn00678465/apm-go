# Implement: apm install --mcp CLI 旗標

TDD:每步先寫測試(RED)→ 實作(GREEN)→ `go build ./... && go vet ./... && go test ./...`。

## 執行順序

### 步驟 1 — `manifest.MCPDependency` 加 `Version` 欄位 ✅ DONE
- [x] `internal/manifest/mcp.go`:`MCPDependency` 加 `Version string`;`ParseMCPEntry` 加 `case "version"`。
- [x] 測試:`mcp_test.go` round-trip 含 `version:` 的條目(`TestParseMCPEntry_Version`)。
- 驗證:`go test ./internal/manifest/...` — PASS

### 步驟 2 — `internal/mcpregistry` package(registry v0.1 client)✅ DONE(含即時對照真實 API 修正 2 處設計假設錯誤)
- [x] 新檔 `internal/mcpregistry/client.go`:`Client`、`NewClient`、`ServerInfo`/`Remote`、`FindServerByReference`。
- [x] search + get 兩段 HTTP 呼叫,`httptest.Server` 模擬測試。
- [x] fuzzy namespace-boundary match(`isServerMatch`),表格測試涵蓋正負案例。
- [x] `HasPackages` 欄位標記 packages-only 情境(實際報錯決策留給 cmd/apm 層,步驟 5)。
- [x] **即時打 `curl https://api.mcp.github.com/v0.1/...` 對照真實回應**,發現 design.md 依 research agent 讀 Python 原始碼推論出的兩個假設是錯的:
  1. `remotes[].transport_type` 實際上是 `remotes[].type`(JSON tag 修正)。
  2. `remotes[].headers[]` 是需求描述(`{name, description, isSecret}`),**沒有 `value` 欄位**——不是原本假設的字面 header 值可直接複製;改成 `Remote.RequiredHeaders []string`(只記名稱,供診斷用,不寫入部署設定,因為本任務不做 GitHub token 自動注入)。
- [x] 新增 `TestFindServerByReference_RealRegistryResponseShape` 用真實 API 回應內容當 regression guard,鎖定這個修正,避免之後不小心改回錯的 JSON tag。
- [x] 用真實網路呼叫寫了一個 throwaway `cmd/mcpregsmoke/main.go` smoke test(對照 `io.github.github/github-mcp-server`),確認端到端解析正確後刪除。
- 驗證:`go test ./internal/mcpregistry/...` — 9/9 PASS,87.5% 覆蓋率

### 步驟 3 — CLI flags + 分流(`cmd/apm/install.go`)✅ DONE
- [x] `installCmd()` 加 `--mcp`/`--transport`/`--url`/`--env`/`--header`/`--mcp-version`/`--registry`/`--force` flags(design.md §1)。
- [x] `RunE` 用 `cmd.ArgsLenAtDash()` 切 stdio command,`mcpName != ""` 時分流到 `runMCPInstall`;`Changed()` 判斷取代值判斷(見步驟 10 第 4/5 輪修正)。
- 驗證:`go build ./cmd/apm/...` — PASS

### 步驟 4 — 衝突驗證(`cmd/apm/mcpinstall.go` 新檔)✅ DONE
- [x] `mcpInstallOpts` struct、`validateMCPConflicts(opts) error`。
- [x] requires-mcp 檢查放在 `install.go` `RunE`(`mcpFlagsGiven` 判斷),`--mcp` 名稱本身的規則留在 `validateMCPConflicts`。
- [x] 測試:11 條規則各一違反案例 + 6 個合法組合(`TestValidateMCPConflicts`/`TestValidateMCPConflicts_ValidCombinations`)。
- 驗證:PASS

### 步驟 5 — `buildMCPEntry` 三分支 ✅ DONE(含即時對照真實 registry API 修正 2 處設計假設錯誤,詳見步驟 2)
- [x] `buildMCPEntry(opts) (entryNode *yamllib.Node, deployDep *manifest.MCPDependency, diags []string, err error)`——直接建 `*yamllib.Node`(而非先建 `any` 再轉換),對齊 `install.go` 既有 `persistPackagesToManifest` 的 node-literal 風格。
- [x] registry 分支:`resolveFromRegistry` 呼叫 `mcpregistry.Client`,失敗時回傳 error、完全不寫檔。**測試技巧**:`opts.Registry` 直接指向 `httptest.Server.URL`(http scheme),不需要額外的 client 注入機制。
- [x] `buildRegistryPersistEntryNode`:裸名稱 / `{name,transport}` / `{name,version}` / `{name,registry}` 四種形態。
- [x] **端到端真實驗證**:用剛編譯出的 `bin/apm-go.exe` 對 `io.github.github/github-mcp-server --transport http` 實跑,apm.yml 與 `.mcp.json` 輸出與原版實測結果(url/type/server key 完全一致);唯一差異是 `headers` 欄位原版輸出空物件 `{}`、apm-go 省略該欄位並改印診斷提醒使用者手動補 `--header`(design.md 已記錄的刻意差異)。
- [x] 測試:`TestBuildMCPEntry_SelfDefinedStdio`/`URL`/`URL_InvalidTransport`/`RegistryLookup_Success`/`RegistryLookup_NotFound`/`RegistryLookup_PackagesOnly`。
- 驗證:PASS

### 步驟 6 — apm.yml upsert writer(`upsertMCPEntry`)✅ DONE(範圍調整:拿掉 `--dev`)
- [x] added/unchanged/error-without-force/replaced 四種狀態(**拿掉 design.md 原提的 dev-section 分支**——查證後 apm-go `install` 目前完全沒有 `--dev` flag,prd.md 非目標已排除,design.md 這裡有一處遺留描述未同步更新,以實作與 prd.md 為準)。
- [x] 直接操作 `*yamllib.Node`(`findOrCreateMappingChild`/`findOrCreateSeqChild`,對齊既有 `persistPackagesToManifest` 風格),equality 比較用 `nodeToValue` 正規化後 `reflect.DeepEqual`。
- [x] 測試:`TestUpsertMCPEntry_Added`/`UnchangedDoesNotError`/`DifferentWithoutForce_Errors`/`DifferentWithForce_Replaces`。
- 驗證:PASS

### 步驟 7 — 部署(`deployMCPEntry`)✅ DONE
- [x] 重用 `deploy.ResolveTargets`/`deploy.Adapters`/`deploy.MCPTarget`,完全沒有新增或修改任何既有 writer。
- [x] 測試:`TestDeployMCPEntry_ClaudeWritesConfig`(實際檔案內容)、`TestDeployMCPEntry_NonMCPTargetIsSkippedNotErrored`(agent-skills 正確 skip)。
- 驗證:PASS

### 步驟 8 — `runMCPInstall` 組裝 + stdout ✅ DONE
- [x] validate → 讀 apm.yml(不存在則報錯,不 auto-bootstrap)→ buildMCPEntry → upsertMCPEntry → (non-unchanged 才)寫檔 + deployMCPEntry → 印訊息。
- [x] AC6 順序保證用測試鎖定:`TestRunMCPInstall_RegistryLookupFailure_ApmYmlUntouched`(注意到:順序是「先 upsert 決定/寫檔、才 deploy」,所以 registry 解析失敗完全不會走到 upsert,apm.yml 內容位元組不變;但如果是**部署**階段才失敗——例如 target adapter 寫檔錯誤——apm.yml 這時已經寫入了,這是設計上合理的取捨,因為到了部署階段代表宣告本身已經驗證通過,不屬於 AC6 涵蓋的「registry 解析失敗」情境)。
- [x] 測試:`TestRunMCPInstall_SelfDefinedURL_E2E`/`SkillCombined_Errors`/`ExistingConflictWithoutForce_ApmYmlUntouched`/`RegistryLookupFailure_ApmYmlUntouched`/`NoApmYml_Errors`。
- 驗證:`go build/vet/test -cover` 全綠,`cmd/apm` 69.5%→73.3%,`internal/mcpregistry` 87.5%。

### 步驟 9 — A/B 測試腳本(design.md §10,PRD 最低驗收門檻)✅ DONE,15/15 assertions PASS(已依使用者要求搬遷位置)
- [x] ~~`cmd/apm/mcpinstall_ab_test.go`~~ 原本以 Go test 實作(5 情境、5/5 PASS),經 codex 審查第 1-10 輪反覆修正邏輯期間持續維護。**使用者事後明確要求**:apm-go repo 內不應放 A/B 測試,一律移到 `D:\Projects\apm-dev\evals`(該目錄已有 `ab_phase0.py`/`ab_phase1.py` 建立的 standalone Python script 慣例)。
- [x] 已刪除 `cmd/apm/mcpinstall_ab_test.go`,確認 `go build/vet/test ./...`(apm-go repo 自身)移除後仍全綠。
- [x] 改寫為 `D:\Projects\apm-dev\evals\ab_mcp_install.py`,對齊既有 `ab_phase0.py`/`ab_phase1.py` 的 `subprocess` + `result()`/`skip()` 計數器 + 總結 exit code 慣例;因為 `--mcp` 是完整 CLI 操作(非窄範圍函式呼叫比對),兩側皆用 `subprocess`(apm-go 呼叫編譯好的 `bin/apm-go.exe`,原版用 `uv --project <apm-py-path> run apm`)。
- [x] 涵蓋同樣 5 個情境(registry lookup **[使用者原始指令情境,優先驗證]**、self-defined stdio、self-defined url、`--skill` 衝突、既有條目無 `--force` 衝突),共 15 個斷言。
- 驗證:`python D:\Projects\apm-dev\evals\ab_mcp_install.py` — **15/15 PASS**(含真實網路 registry 查詢)。

### 步驟 10 — 全域驗證 + codex review loop ✅ DONE(11 輪 codex + 1 輪人工複審,收斂)
- [x] `go build ./...`、`go vet ./...`、`gofmt -l .`、`go test ./... -cover` 全綠(`cmd/apm` 76.4%、`internal/mcpregistry` 89.6%、`internal/manifest` 85.5%、`internal/deploy` 86.9%)。
- [x] codex exec 唯讀審查跑了 **11 輪**(xhigh effort),每輪發現的問題都修正並補 regression test 後才進下一輪,累計發現並修正 **31 個真實問題**,詳見下方逐輪紀錄。使用者在第 11 輪後反映 codex 額度消耗過快,要求「提升完成速度」;此要求一度被誤讀為「停止驗證迴圈」,經使用者澄清後修正為:迴圈本身不停,但改用不耗費 codex 額度的方式收尾——改為對 `cmd/apm/mcpinstall.go`、`internal/manifest/mcp.go`、`internal/mcpregistry/client.go` 三個核心檔案做逐行人工複審(套用與前 11 輪相同的檢查視角:憑證外洩、position-agnostic placeholder 處理、identity 一致性、網路呼叫前置順序),未發現新問題,判定迴圈已收斂(dry)。
- [x] A/B 測試(已搬遷至 `evals/ab_mcp_install.py`,見步驟 9)全程保持 15/15 PASS,每輪修正後都有重跑確認;人工複審收斂後重跑一次最終確認(含 `go build/vet/gofmt/test ./... -cover` 全綠 + A/B 15/15 PASS),涵蓋使用者原始回報的確切失敗指令。

#### Codex 審查逐輪紀錄(共 31 個真實發現,全部修正 + 補 regression test)

1. **第 1 輪**(5 個):registry 查詢 URL 缺少 userinfo/query 拒絕、deploy identity 與 persist identity 不一致導致可繞過 `--force` 覆寫無關項目、`--env` 搭配 `--url` 靜默丟棄、缺少 `MCP_REGISTRY_URL` env fallback、`--skill`/no-packages 情境下的假成功回報。
2. **第 2 輪**(3 個):conflict gate 只擋 `--mcp` 本身缺 `--mcp` 情境、`--transport stdio` 誤入 registry 分支產生壞掉的 deploy dep、`--env`/`--mcp-version` 在自訂分支被靜默丟棄未驗證。
3. **第 3 輪**(4 個):registry 查詢在「unchanged」判定前就發生(即使沒有任何變更也會打網路+可能因斷線失敗)、缺少 `[i] Targets: ... (source: ...)` 行(R7 要求)、`MCP_REGISTRY_URL` 選到的 registry 未持久化進 apm.yml、`--mcp ""`(明確傳空字串)被當成沒給 `--mcp`。
4. **第 4 輪**(1 個):`--mcp` 之外的其他 MCP-only flag(`--transport ""` 等)以及 `--force` 本身也有同一類「明確傳空值卻被當沒給」的 dispatch gate 漏洞。
5. **第 5 輪**(1 個,同上一輪但更深一層):flag 通過了 dispatch gate 之後,`opts.URL`/`opts.Registry` 等欄位內部仍然無法區分「明確傳空字串」與「完全沒傳」,同一類問題在 CLI 層修過還會在 `runInstall` 內部再犯一次。
6. **第 6 輪**(4 個,聚焦憑證外洩):registry 解析出的 remote URL 完全沒過 `ValidateMCP` 的 userinfo 檢查、`ValidateMCP` 的 credential 檢查在 `url.Parse` 失敗時直接放行(fail-open)、`mcpregistry.NewClient` 的錯誤訊息在驗證分支之前就把原始 baseURL(含可能的憑證)印出來、既有的 stdio command 空白檢查錯誤訊息把整條指令字串印出來。
7. **第 7 輪**(4 個,持續深挖憑證外洩面):`parseKVPairs` 把整組 malformed 的 `KEY=VALUE` 原文印進錯誤訊息、`ValidateMCP` 新加的 url-parse-失敗錯誤訊息也把原始 URL 印出來、完全沒有「URL 必須是絕對路徑」的檢查(相對字串會通過驗證,寫進 apm.yml 後才在部署時默默失敗)、既有 stdio 檢查修正時漏掉一併处理。
8. **第 8 輪**(4 個):`registryURL` 在多個 diagnostic/error 訊息裡被印出(即使已經擋掉 query/userinfo,path 裡藏 token 還是會外洩)、`getJSON` 的 network/HTTP 錯誤路徑用 `%w` 包住底層 `*url.Error`(Go 本身會把完整 request URL 塞進錯誤訊息)、`resolveFromRegistry`「not found」錯誤印出 `client.BaseURL`、`MCP_REGISTRY_URL` 來源的 diagnostic 印出完整 URL。
9. **第 9 輪**(2 個):round 8 新增的「URL 必須絕對路徑」檢查範圍過寬,把 translate-mode 合法的 placeholder URL(如 `${input:mcp-url}`)也一併擋掉;`NewClient` 的 unsupported-scheme 錯誤訊息還是把解析出來的 scheme 值印出來。
10. **第 10 輪**(2 個):round 9 的 placeholder 例外處理是「只要出現任何 placeholder 就整段跳過解析」,導致 placeholder 以外的畸形字面內容(例如壞掉的 percent-encoding)也一併被放行;registry unreachable/404 的 diagnostic 仍間接透過底層錯誤把 request URL 帶出來。
11. **第 11 輪**(2 個,codex 審查的最後一輪;第 12 輪起改為人工複審,見步驟 10):`ValidateMCP` 只驗證「宣告時」的原文值,`${VAR}` 這種 placeholder 在**部署時**被替換成真正環境變數值後(可能就是含憑證的 URL)完全沒有再檢查一次,會被寫進 target 設定檔;round 10 用「把每個 placeholder 換成固定 `"x"` token 再整段解析」的作法本身有 bug——mf-013 的 placeholder 替換是逐字取代、跟 URL 文法位置無關(可以合法出現在 port、IPv6 host 位置),`"x"` 不是所有位置都合法(例如當 port 會被 `url.Parse` 判定無效),改成「直接對原始字串做 percent-encoding 格式檢查,不做結構化 URL parse」來繞過這個位置相依問題。

## Review Gates
- **A**(步驟 2 後):registry client 的 fuzzy-match 邏輯正確性(容易寫錯的部分)。
- **B**(步驟 8 後):完整 CLI 行為,codex 對照 design.md 檢查是否有遺漏原版行為的地方。
- **C**(步驟 9 後):A/B 測試結果本身,確認測試斷言確實有意義(不是 tautology)。

## Rollback Points
每步獨立可回退。步驟 1(manifest 欄位)最保守,不影響既有解析行為(新增可選欄位)。步驟 3 起才會改到 `install.go` 既有檔案,且只新增分流判斷,不修改既有 `runInstall` 邏輯本身。
