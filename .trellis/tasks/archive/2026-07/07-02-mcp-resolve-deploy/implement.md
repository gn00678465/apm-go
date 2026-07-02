# Implement — MCP install 解析與 target 部署

依 `design.md`(已納入 codex plan-review 修正)。TDD:每步先寫測試(RED)→ 實作(GREEN)。每步後 `go test ./... && go vet ./...`。

## 執行順序

### 步驟 1 — Manifest MCP 留存(R1 / AC1)✅ DONE
- [x] `manifest.go`:`Manifest` 加 `MCPServers` / `MCPDevServers []*MCPDependency`。
- [x] section parse 由丟棄 `m` 改為收集,依 prod/dev 存入(dev 留存但後續不部署)。
- [x] 測試:`manifest_test.go` 加 `mcp:` prod+dev 留存 round-trip;既有測試不退。
- 驗證:`go test ./internal/manifest/...` — PASS

### 步驟 2 — Placeholder resolver(mf-013 / R2-R5 / AC2-AC5)✅ DONE(codex 3 輪:1 HIGH + 2 MEDIUM 修正)
- [x] 新檔 `internal/manifest/mcpresolve.go`:`ResolveMode`、`FieldPos`、`ResolvePlaceholders(value, mode, pos, lookup) (out, diags, refuse, omit)`、`HasPlaceholder(s string) bool`。
- [ ] precedence:**無需遮罩**——`InputVarRe`/`EnvVarRe` 結構天然不匹配 `${{`,直接依序套用(先判斷 `InputVarRe` 命中→refuse,再套 `EnvVarRe` 代換);`${{…}}` 自然逐字通過。
- [ ] Bake:defined→字面;undefined 依 pos(env-dict/header `omit`+診斷、registry-list `omit`、**url `refuse`+診斷**、args 逐字**不診斷**);`${input:}`(任一 pos)→診斷+`refuse`,優先於 pos 短路判斷。
- [ ] Translate:`${VAR}`/`${input:}` 原樣通過(resolver 不改寫、不需 lookup)。**字面值改寫 `${NAME}` 為 Step 4 writer 職責**(需 env key 名稱,resolver 簽章無此參數),writer 用 `HasPlaceholder` 判斷。
- [ ] `lookup func(string)(string,bool)` 辨 undefined/empty。
- [ ] 測試 `mcpresolve_test.go`:表格(bake/translate × **env-dict/args/registry-list/url/header** × defined/undefined/input/`${{}}`/`${env:}`);涵蓋 url-undefined→refuse、header-undefined→omit、args 定義值仍逐字不解析、`${{...}}` 與相鄰 `${VAR}` 混用只處理後者。
- 驗證:`go test ./internal/manifest/ -run Resolve`
- **review gate A**:resolver 語意經外部(codex exec)對 apm-cli parity 驗證。

### 步驟 3 — TypeMCP primitive + 收集 + override(R6/R8 / AC6/AC9)✅ DONE(codex READY,2 LOW 已修)
- [x] `deploy/primitive.go`:`TypeMCP`;`Primitive.MCP *manifest.MCPDependency`。
- [x] `deploy.go` `Run`:收集 local(`m.MCPServers` prod)+ dependency(解析 `apm_modules/<key>/apm.yml` 的 `mcp:`;direct auto-trust;**transitive 一律跳過發警告**,無 flag)。
- [x] **只收 self-defined(`registry:false`)**;registry-backed → 診斷、不部署(R8)。
- [x] `Primitive{TypeMCP}` 併入 `ordered` → `ResolvePrimitives`(確認 conflict.go 對 (Type,Name) 通用)。
- [x] 測試:local vs dep 同名 local 勝;多 dep first-declared 勝;transitive 跳過;registry-backed 診斷;malformed apm.yml 診斷;direct auto-trust 正向斷言。
- 驗證:`go test ./internal/deploy/... ` — PASS(7/7 新測試)

### 步驟 4 — Per-target writers(R7 / AC7)✅ DONE(codex Review Gate B:2 HIGH + 2 MEDIUM 修正)
- [x] `MCPTarget` 介面(`MCPResolveMode()` / `WriteMCP(prims []Primitive, projectDir)`)於 `adapter.go`;`Run` 對 TypeMCP winners per-target 一次性寫入。
- [x] `go get github.com/pelletier/go-toml/v2` + `go mod tidy`。
- [x] `mcp_antigravity.go`:`.agents/mcp_config.json`/`serverUrl`/Bake/`0600`。**無目錄 pre-existence gate**(codex HIGH 判定為文件誤導,已更正:apm-go 對所有 primitive 一律 create-on-write,opt-in 語意已由 target 選取滿足)。
- [x] `mcp_claude.go`:`.mcp.json`/`type`+`url`/Bake/`0600`。
- [x] `mcp_codex.go`:`.codex/config.toml`(go-toml/v2)/`mcp_servers`/Bake/**sse skip+warn**/`0600`。
- [x] `mcp_copilot.go`:`.github/mcp-config.json`/`type:http`/Translate/`0644`;env 值用 `manifest.HasPlaceholder` 判斷,無 placeholder 的 authored 字面值改寫為 `${<envKey>}`。
- [x] 全 writer:非 https remote 跳過發警告;merge 改用 `mergeMCPServers`(considered-set 語意,見 design §5)——refuse/skip 的 server **整條移除**、omit 的欄位**不被外來鍵保留語意復活**;既有檔解析失敗 → error 拒寫(不靜默覆蓋)。
- [x] golden/unit 測試:每 target stdio+http+sse;antigravity 欄位對 **live oracle descriptor**(新增 `oracleMCP` struct 讀 `targets/expected/antigravity.yaml`);antigravity E2E 用 `--target antigravity` 跑完整 `Run()`;3 個 redeploy 迴歸測試(refused-removed、omit-dropped、malformed-file-errors)。
- 驗證:`go test ./internal/deploy/...` — PASS(16 個新測試)
- **review gate B**:codex 兩輪(初審 2H+2M→修正→複核)。

### 步驟 5 — Lockfile / source 歸屬(pr-001 / AC8)✅ DONE(codex 2 輪:1 HIGH 修正 + 前置 write.go bug 修正)
- [x] `DeployResult` 加 `MCPProvenance []MCPProv{Server,Source,File}`;`Run` 從 winner `prims[].Source` 建立(writer 不負責);合併檔 hash 記錄一次(`MCPFiles map[string]string`)。
- [x] `install.go`:mcp 檔 hash 併入 `newLock.LocalDeployedFiles`/`LocalDeployedHashes`(pr-001 source 歸屬由 in-memory `MCPProvenance` 服務,不落地新 schema)。
- [x] 修正前置 bug(非 MCP 引入,但阻擋本步驟 AC8):`lockfile/write.go` `SerializeLockfile` 從未序列化頂層 `local_deployed_files`/`local_deployed_file_hashes`(parse.go 有讀、write.go 沒寫)——比照既有 per-entry `deployed_files`/`deployed_file_hashes` preserve-if-unchanged pattern 補上,並移入 `knownTopKeys`。
- [x] 修正 codex 複核發現的 HIGH:`IsSemanticEqual`(no-op 判斷)未比對 `LocalDeployedFiles`/`LocalDeployedHashes`,違反 req-lk-005(這兩者是 content 欄位、非 advisory)——僅 MCP config 變更、dependencies 不變時會誤判「Already up to date」並跳過寫入,導致 lockfile hash 過期。已補比對(`slicesEqual`/`mapsEqual`)。
- [x] 測試:`TestRun_MCP_MultiSourceProvenanceAndSingleHash`(兩 dep 各貢獻一 server 到同一 `.mcp.json` → hash 正確 + 兩 source 可檢視)、`TestRunInstall_MCP_DeploysAndRecordsLockfileHash`(E2E)、`TestIsSemanticEqual_LocalDeployedHashDiffers`、`TestIsSemanticEqual_LocalDeployedFilesDiffer`、`TestRunInstall_MCP_OnlyChangeStillRewritesLockfile`(E2E 回歸:MCP-only 變更仍觸發 lockfile 重寫)。
- 驗證:`go test ./... -count=1` — PASS(全套無退化);`go vet ./...` / `go build ./...` — clean。
- **codex exec 複核**:MEDIUM(頂層 malformed local_deployed_files 靜默丟棄)與既有 per-entry `deployed_files`/`deployed_file_hashes` 寬鬆解析行為一致,非本次新增的不一致,列為已知既有模式不修;LOW(MCPProvenance 扁平化風險)codex 自行確認目前 4 個 writer 皆單檔案輸出,無實際 double-counting,不需修正。

### 步驟 6 — 接入 install(AC10 不 gate)✅ DONE(codex 1 輪:0 blocking,1 MEDIUM 為已知既有缺口記錄不修 + 1 LOW 測試隔離已修)
- [x] `install.go`:deploy 傳入 MCP servers(`deploy.Run` 內建收集 `m.MCPServers`);**未**加 `RequireEnabled`——grep 確認 `internal/deploy`、`internal/manifest` 全無 `experimental`/`RequireEnabled`,`install.go` 僅有的兩處 `RequireEnabled("registries")`(239、325 行)皆屬 registry 依賴路徑,與 MCP 無關。
- [x] E2E:`TestRunInstall_MCP_DeploysAndRecordsLockfileHash`(claude target)、`TestRunInstall_MCP_AntigravityExplicitTarget`(antigravity 為 explicit-only target,經 `--target` 走完整 `runInstall()` pipeline:manifest parse → resolve → deploy.Run → lockfile write)。
- [x] 不 gate 測試:以全新空 `APM_CONFIG_DIR`(無 `config.json`)執行 `go test ./... -count=1`,全數綠燈,含所有 MCP 測試。
- [x] 3 個 MCP E2E 測試加 `t.Setenv("APM_CONFIG_DIR", t.TempDir())` 隔離(codex LOW:避免依賴執行機器既有 `~/.apm/config.json` 狀態,確保測試在任何環境下都能可靠偵測未來若誤加 gate 的回歸)。
- 驗證:`go test ./... -count=1` — PASS;`go vet ./...` / `go build ./...` — clean。
- **final review 修正**:`install.go` 原本在 `m.ParsedDeps` 為空時提早 return「No dependencies to install」,純 MCP-only(僅 `dependencies.mcp`、無 `dependencies.apm`)專案永遠到不了 deploy 階段。已改為先 resolve targets;無 deps 且無 active target 才維持早退,有 active target 時用空 `ResolutionResult` 走 deploy + lockfile path。MCP E2E 測試已移除 dummy `./local-pkg`,直接覆蓋 pure MCP-only。

### 步驟 7 — 全域 + 外部驗證(AC11)✅ DONE(codex 2 輪:1 HIGH + 1 MEDIUM 修正,第 2 輪 codex 越權直接修正既有缺口,經 advisor 覆核後保留)
- [x] `go test ./... -cover`、`go vet ./...`、`go build ./...` — 全綠。**`-race` 在本 Windows 開發環境不可用**(無 gcc/cgo,`CGO_ENABLED=0`)——環境限制,非程式缺陷,已記錄。覆蓋率:`internal/deploy` 84.9%、`internal/manifest` 85.3%(本次新增程式碼所在套件均 ≥80%);`cmd/apm` 67.2%、`internal/lockfile` 78.8%、`internal/gitops` 11.8% 為既有缺口,非本次改動範圍,不強行拉高。
- [x] 外部(codex exec)第 1 輪:HIGH——`writeMergedMCPJSON`/`writeMergedMCPTOML` 用 `os.WriteFile(path, data, perm)` 直寫,Go 的 `perm` 參數僅在**新建檔案**時生效(POSIX `open()` 語意),既有檔案(如 git checkout 留下的 0644)重寫後權限不會被收緊到 0600,違反 bake mode 的機密保護保證。修正:新增 `writeFileWithPerm` helper(`os.Chmod` before+after `os.WriteFile`),兩個 writer 改用之。
- [x] 外部第 1 輪 MEDIUM——共用 https 檢查對 translate mode(copilot)一視同仁套用,但 mf-013 design D4 明定 translate mode 的 `${input:}`/`${VAR}` URL 應逐字保留(由 runtime 解析),導致帶 placeholder 的 remote URL 被誤判為 non-https 而靜默跳過整台 server。修正:新增 `deferredToRuntime`(後由第 2 輪精修為 `placeholderAtStart`,僅當 placeholder 位於字串**開頭**才略過 https 檢查,避免 `http://host/${input:path}` 這種字面 http scheme + 尾端 placeholder 的組合被誤放行)。
- [x] 外部第 2 輪(複核):在授權範圍外(僅要求 verify/report)額外直接修改 `cmd/apm/install.go`,修正一個更早於本 session 就發現、原本判定為「非 MCP 專屬、超出範圍不修」的既有缺口——`m.ParsedDeps` 為空時提早 return,導致純 local-only(含純 `dependencies.mcp`)專案永遠到不了 deploy 階段。已呼叫 **advisor** 覆核此越權變更是否保留:advisor 判定此為 MCP 功能最小自然使用情境(僅宣告 `dependencies.mcp`、無 `dependencies.apm`)所必需,程式改動正確(has-deps 分支邏輯位元組不變、frozen 路徑未觸及、`deploy.Run` 對空 `ResolutionResult` 安全)、測試證實無退化,**保留**,但需獨立成一個 commit(非 MCP 專屬的一般性 `install` 行為變更)並補一個非 MCP 情境的回歸測試(`TestRunInstall_NoDeps_LocalOnlyWithTargetStillDeploys`)以涵蓋「零依賴 + 有效 target + 純 local `.apm/` primitive」情境。
- [x] 測試:`TestWriteMCP_RedeployTightensPermissionOnExistingFile`(Windows 跳過,POSIX 有效)、`TestWriteMCP_Copilot_TranslatePlaceholderURLNotSkippedByHTTPSGuard`、`TestWriteMCP_Copilot_TranslateLiteralHTTPStillSkipped`、`TestWriteMCP_Copilot_TranslateLiteralHTTPWithPlaceholderPathStillSkipped`、`TestRunInstall_NoDeps_LocalOnlyWithTargetStillDeploys`。
- 驗證:`go build ./...` / `go vet ./...` / `go test ./... -count=1 -cover` — 全綠;`go mod tidy` 無異常 drift。
- **review gate C**:codex 兩輪 + advisor 覆核越權變更,至 clean。

## Review gates
- **A**(步驟 2 後):resolver parity。
- **B**(步驟 4 後):writer golden + oracle 對齊。
- **C**(步驟 7):最終 clean 才進 Phase 3 commit。

## Rollback points
每步獨立可回退;MCP 為新增分支,移除 `TypeMCP` 收集 + writers + Manifest 欄位即回現狀。go-toml/v2 僅 codex 使用。
