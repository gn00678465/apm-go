# apm install --mcp 驗收清單

> **用途**：比照 `.trellis/tasks/07-05-uninstall/uninstall-checklist.md` 的格式，把
> `apm install --mcp`（standalone「宣告 + 部署單一 MCP server」路徑）排成逐項可勾選、
> **完整呈現此命令全部功能**的驗收清單 —— 涵蓋①既有已實作功能（回歸保護）與②本輪修正
> （#1 apm.yml block style、#2 registry 憑證互動詢問）。每項附 apm-go 檔案/行號、Python
> 原版對照與權威來源。前次任務缺此「完整清單」步驟，本輪補齊。
>
> **權威來源**：
> - apm-go 源碼：`cmd/apm/install.go`（旗標/dispatch）、`cmd/apm/mcpinstall.go`
>   （runMCPInstall/validateMCPConflicts/buildPersistEntry/resolveFromRegistry/
>   upsertMCPEntry/deployMCPEntry）、`internal/mcpregistry/{client,resolve}.go`、
>   `internal/yamlcore/{patch,safe}.go`、`internal/deploy/mcp_*.go`
> - Python 原版：`src/apm_cli/install/mcp/{command,args,entry,writer,conflicts,registry}.py`、
>   `src/apm_cli/registry/operations.py`（互動 prompt）
> - 指令文件：`https://microsoft.github.io/apm/consumer/install-mcp-servers/`
> - 即時驗證：`apm-go install --help`（本輪實測，見 M0）、registry GET 實測（見 M3/#2）、
>   go-yaml v4 flow-style 實測（見 M4/#1）
> - 研究：`.trellis/tasks/07-06-mcp-install-parity/research/findings.md`
> - **衝突時以 live CLI 實測為準**（比照 uninstall/marketplace 清單準則）。
>
> **權威標記**：`源碼`=apm-go 原始碼；`Py`=Python 原版原始碼；`文件`=install-mcp-servers.md；
> `實測`=live CLI / registry / go-yaml 實測；多者以 `+` 併記。
> **狀態欄**：`✓`=已驗證通過；`修`=本輪需新增/修改；`回`=既有功能回歸保護；`異`=documented deviation。

## 範圍與決定

| 項目 | 決定 |
|---|---|
| 本輪修正（必做） | **#1** `dependencies.mcp` 寫成 block style（清 FlowStyle）；**#2** registry-resolved server 需憑證時互動 TTY prompt 並注入部署；**D2** 衝突條目於互動 TTY 顯示 diff + confirm 取代（對齊原版 writer.py） |
| **定案 Q1：#2 詢問對象**（使用者 2026-07-06） | 針對 registry **remote 宣告的 required header** 詢問；`Authorization`（secret）→ prompt token → 組 `Bearer <token>` 注入 header。務實對齊 apm-go remote-only 模型，不觸碰 OCI/stdio runtimeArguments |
| **定案 Q2：#2 持久化**（使用者 2026-07-06） | 只注入本次 deploy；**apm.yml 維持 registry bare string、不寫 secret**（避免明碼進版控） |
| **定案：#3 `--header requires --url`** | 原版 conflicts.py **E9** 同此規則 → apm-go 已 parity，**本輪不改碼**；補 #2 即涵蓋「registry server 帶 token」需求 |
| 既有功能（回歸保護，不可退化） | 旗標面/衝突矩陣/三分支建構/registry 解析/upsert(added·unchanged·conflict·replace)/多 target 部署/target source 輸出/錯誤時 apm.yml 不動/不洩漏 secret —— 全部既有測試須維持綠 |
| 明確不移植（documented deviation，非 parity 失敗） | 見下方「Deviation 總表」D1–D6；本輪除 #1/#2 外不擴大，若實作中觸及則於對應條目標註 |

### Deviation 總表（apm-go 相對 Python 原版，含既有 + 本輪）

| id | 內容 | 依據 | 本輪處置 |
|---|---|---|---|
| D1 | `--mcp` 路徑**不更新** `apm.lock.yaml`（Python `command.py` 更新 `mcp_servers`/`mcp_configs`） | `mcpinstall.go` runMCPInstall doc；`command.py:120-127` | 維持既有 deviation，不改 |
| D2 | 衝突條目**無互動 confirm-replace**（Python `writer.py` 於 TTY 顯示 diff+`click.confirm`）；apm-go 僅 force/error 兩態 | `mcpinstall.go:437`；`writer.py:98-121` | **本輪納入**（使用者 2026-07-06）→ 補三態，見 mi-054/mi-V10 |
| D3 | registry 解析**僅 remote**（Python 另支援 package/stdio 解析） | `resolve.go:37-43` | 既有非目標，維持 |
| D4 | registry URL precedence **僅 2 層**（`--registry`>`MCP_REGISTRY_URL`）；Python 有第 3 層 `apm config` | `mcpinstall.go:300`；`registry.py:182-195` | 既有 deviation，維持 |
| D5 | #2 token 來源為 **remote header**（Python 來自 OCI `runtimeArguments`，用於 docker env）；**行為等價**（都取 PAT 讓 server 認證） | `research/findings.md`；registry GET 實測 | 本輪刻意設計，A/B 標註 |
| D6 | `parseKVPairs` 錯誤**不 echo value**（Python `args.py` echo `{raw}` 有洩漏風險）；apm-go 為安全改進 | `mcpinstall.go:590-604`；`args.py:27-30` | 維持（安全較佳） |
| D7 | **codex** header 佔位處理（bake→translate 或 `bearer_token_env_var`/`env_http_headers`）；原版把 `${VAR}` 寫 `http_headers`，codex 文件顯示應用 env 專屬欄位 | `mcp_codex.go`；`codex.py:281`；codex 官方文件 | **本輪納入**（M8，DEC-1 定案） |
| D8 | **claude/opencode** header 由 bake 改為保留變數（claude `${VAR}` / opencode `{env:VAR}`）；對齊原版 header 保留 `${VAR}` + 安全（不烘 secret） | `mcp_claude.go`/`mcp_opencode.go`；claude/opencode 官方文件 | **本輪納入**（M8） |

### 驗證進度（2026-07-06 session）

- **本輪修正全部完成並驗證**：#1 block style（mi-056/059/V01–V03，含 **live E2E** 實測）、
  #2 憑證互動（mi-047–04A/074/V04–V06）、D2 conflict 三態（mi-054/054b/V10）。
- **既有 M0–M6 行為**：由既有測試套件回歸保護，`go test ./...` **全綠**（mi-V07）；本輪未退化。
- **建置/檢查**：`go build ./...`、`go vet ./...`、`go test ./...` 全綠，cmd/apm 覆蓋率 **83.0%**（≥80%）。
  `-race` 因本機無 gcc/cgo 未跑（環境限制，非程式問題，mi-V08 標記待補）。
- **M8（憑證佔位 per-target，claude/opencode/codex）✅ 完成並驗證**：DEC-1=B、DEC-2/3=只改 header。
  `resolveMCPServer` 拆 header/env mode；claude 保留 `${VAR}`、opencode `{env:VAR}`、codex
  `bearer_token_env_var`/`env_http_headers`。單元測試 5 個 + **live 4-target E2E** 皆綠；`go test ./...` 全綠。
- **待辦**：mi-V09 A/B 對照腳本（`D:\Projects\apm-dev\evals`，AC7）尚未產出。
- 過程中曾誤把 `install --mcp` 跑在專案根目錄污染 `apm.yml`/`.codex/config.toml` 等，
  已 `git checkout` + 刪除還原乾淨（`git status` 僅剩本任務合法變更）。

### 本任務要防的「舊坑」

1. **不能只用全新 fixture** —— #1 的測試矩陣須含「已存在、手動排版、含 `dependencies.apm`
   與其他無關內容/註解」的 apm.yml，證明只改 `dependencies.mcp` span、其餘位元組不動。
2. **同類 bug 全庫掃描** —— FlowStyle 繼承問題若還出現在其他序列（如 `dependencies.apm`），
   須一併評估；本輪只修 mcp 但需 grep 確認無同型漏改被誤當已修。
3. **讀源碼不夠，對照 live CLI** —— CLI 面/輸出以 `apm-go install --help` 與 `uv run apm`
   實測為準。
4. **secret 紅線** —— #2 不可 echo/log token、不可把明碼 secret 寫入 apm.yml。
5. **回歸不可退化** —— 既有 30+ 條 mcp install 測試全綠是硬門檻，不可為 #1/#2 犧牲。

---

## M0 — CLI 介面與旗標（源碼+Py+文件+實測）

| ✓ | id | 權威 | 驗證內容 | 對照 |
|---|----|----|----------|------|
| [ ] | `mi-001` | 實測+文件 | 指令形態 `apm-go install --mcp NAME [flags]`，standalone 宣告+部署單一 MCP，與一般套件 install 分流 | `install --help`；`command.py:38` |
| [ ] | `mi-002` | 源碼+實測 | 旗標集**恰含**：`--mcp`、`--transport`、`--url`、`--env`(repeatable)、`--header`(repeatable)、`--mcp-version`、`--registry`、`--force`、`--` 後 stdio command | `install.go:137-144`;`install --help` |
| [ ] | `mi-003` | 源碼 | requires-mcp gating：任一 MCP-only 旗標（含 `--force`、`--` command）未帶 `--mcp` → error | `install.go:75-82` |
| [ ] | `mi-004` | 源碼 | 明確空值拒絕：`--url/--transport/--registry/--mcp-version ""` → "cannot be empty" | `install.go:93-104` |
| [ ] | `mi-005` | 源碼 | dispatch 用 `Flags().Changed("mcp")`（非值判斷）：`--mcp ""` 走 mcp 路徑報 empty-name，不誤落套件安裝 | `install.go:75,109`;test `ExplicitEmptyMCPName` |
| [ ] | `mi-006` | 源碼 | `--mcp` 與 positional packages / `--skill` 互斥 | `mcpinstall.go:174,177` |

## M1 — 衝突矩陣（源碼+Py conflicts.py E1–E15）

| ✓ | id | 權威 | 驗證內容 | 對照 |
|---|----|----|----------|------|
| [ ] | `mi-010` | 源碼+Py | name 空 → "--mcp requires a server name" | `mcpinstall.go:168`;`conflicts.py:69` |
| [ ] | `mi-011` | 源碼+Py | name 以 `-` 開頭 → error（疑似漏帶值） | `mcpinstall.go:171`;`conflicts.py:70` |
| [ ] | `mi-012` | 源碼+Py | **E1** positional packages + `--mcp` → error | `mcpinstall.go:174`;`conflicts.py:73` |
| [ ] | `mi-013` | 源碼 | `--skill` + `--mcp` → error | `mcpinstall.go:177` |
| [ ] | `mi-014` | 源碼+Py | **E9** `--header` 無 `--url` → "--header requires --url"（**#3 parity，不改**） | `mcpinstall.go:180`;`conflicts.py:98-100` |
| [ ] | `mi-015` | 源碼+Py | **E14** `--env` 無 stdio command → error（env 屬 stdio；remote 用 --header） | `mcpinstall.go:183`;`conflicts.py:114-116` |
| [ ] | `mi-016` | 源碼+Py | **E11** `--url` + stdio command 互斥 | `mcpinstall.go:186`;`conflicts.py:102-104` |
| [ ] | `mi-017` | 源碼+Py | **E12** `--transport stdio` + `--url` → error | `mcpinstall.go:189`;`conflicts.py:106-108` |
| [ ] | `mi-018` | 源碼 | `--transport stdio` 無 command 無 url → error（registry 只解 remote） | `mcpinstall.go:192` |
| [ ] | `mi-019` | 源碼+Py | **E13** remote transport + stdio command → error | `mcpinstall.go:195`;`conflicts.py:110-112` |
| [ ] | `mi-020` | 源碼+Py | **E15** `--registry` + (`--url`\|command) → error | `mcpinstall.go:198`;`conflicts.py:118-123` |
| [ ] | `mi-021` | 源碼 | `--mcp-version` + (`--url`\|command) → error | `mcpinstall.go:201` |
| [ ] | `mi-022` | 源碼 | unknown transport（非 stdio/http/sse/streamable-http）→ error | `mcpinstall.go:204` |
| [ ] | `mi-023` | 異 | Python E2(`--global`)/E3(`--only apm`)/E4(transport-selection flags) 在 apm-go **無對應旗標** → N/A，非缺漏 | `conflicts.py:77-92` |

## M2 — 條目建構 build entry（源碼+Py entry.py）

| ✓ | id | 權威 | 驗證內容 | 對照 |
|---|----|----|----------|------|
| [ ] | `mi-030` | 源碼+Py | self-defined **stdio**：`{name, registry:false, transport:stdio, command, args?, env?}` | `mcpinstall.go:257,385`;`entry.py:48-57` |
| [ ] | `mi-031` | 源碼+Py | self-defined **url**：`{name, registry:false, transport(預設 http), url, headers?}` | `mcpinstall.go:276,404`;`entry.py:62-71` |
| [ ] | `mi-032` | 源碼+Py | registry shorthand **bare string**（無 overlay 時） | `mcpinstall.go:379`;`entry.py:99` |
| [ ] | `mi-033` | 源碼+Py | registry `+version` → `{name, version, transport?, registry?}` | `mcpinstall.go:361`;`entry.py:76-88` |
| [ ] | `mi-034` | 源碼 | registry `+transport`（無 version）→ `{name, transport, registry?}` | `mcpinstall.go:370` |
| [ ] | `mi-035` | 源碼 | registry `+registry_url`（含 env 導出）→ `{name, registry}`；env-derived 亦持久化 | `mcpinstall.go:376`;test `PersistsEnvDerivedRegistry` |
| [ ] | `mi-036` | 源碼 | registry 條目**不持久化 resolved URL**（只留可重複 lookup 的依據） | test `RegistryLookup_Success`（persisted 無 url） |
| [ ] | `mi-037` | 源碼 | build entry **不做網路呼叫**（buildPersistEntry 純本地，unchanged 可零網路短路） | `mcpinstall.go:59-72`;test `Unchanged...NeverContactsRegistry` |

## M3 — registry 解析（源碼+Py registry.py/operations.py + 實測）

| ✓ | id | 權威 | 驗證內容 | 對照 |
|---|----|----|----------|------|
| [ ] | `mi-040` | 源碼 | search → exact-name match 優先，否則 namespace-boundary fuzzy fallback | `client.go:161-200` |
| [ ] | `mi-041` | 源碼 | version pin → `GET .../versions/<v>`（"" → latest） | `client.go:216-220`;test `UsesPinnedVersion` |
| [ ] | `mi-042` | 源碼 | 取 `remotes[0]`；transport override 僅允許 remote transports | `resolve.go:45-58` |
| [ ] | `mi-043` | 源碼+異 | **remote-only**：packages-only → "package-based (stdio) not supported"；無 remotes 無 packages → "no deployable remote endpoint"（**D3**） | `resolve.go:37-43`;tests `PackagesOnly`/`NoRemotesNoPackages` |
| [ ] | `mi-044` | 源碼 | credentialed remote URL（`user:pass@`）→ 拒絕（ValidateMCP embedded credentials），不部署 | `resolve.go:72-74`;test `RejectsCredentialedRemoteURL` |
| [ ] | `mi-045` | 源碼+異 | registry URL precedence `--registry` > `MCP_REGISTRY_URL`；normalize 去尾斜線（**D4**：無第 3 層 config） | `mcpinstall.go:300-315`;tests `UsesMCPRegistryURLEnv`/`NormalizesTrailingSlash` |
| [ ] | `mi-046` | 源碼 | 診斷不洩漏 path-embedded token（NewClient 拒 userinfo/query；not-found 錯誤不含 registry URL） | `mcpinstall.go:324-333`;test `NotFound`（不含 srv.URL） |
| [x] | `mi-047` | **修 #2** | registry remote 宣告 required header **且互動 TTY** → prompt；`Authorization`/secret 類隱藏輸入；**非互動（非 TTY/CI/e2e）不 prompt** | `mcp_prompt.go` `canPromptCreds`/`collectHeaderValues`;接 `resolveFromRegistry`（測試綠） |
| [x] | `mi-048` | **修 #2** | prompt 到的 header 設為 `dep.Headers` → 部署寫入各 target；未輸入 → 不注入（server 走 unauth） | `mcpinstall.go` resolveFromRegistry;`collectHeaderValues` 測試 |
| [x] | `mi-049` | **修 #2** | 僅在**未收到憑證**時才 append 既有 "requires header(s)… add --header" 診斷（互動已輸入不再 nag） | `mcpinstall.go` resolveFromRegistry if/else;`_Success` 測試（非互動→diag） |
| [x] | `mi-04A` | **修 #2 異** | **D5**：token 源自 remote `Authorization` header（非 OCI runtimeArguments）；行為等價，A/B 標 documented deviation | `research/findings.md`；registry GET 實測 |

## M4 — upsert 進 apm.yml（源碼+Py writer.py）

| ✓ | id | 權威 | 驗證內容 | 對照 |
|---|----|----|----------|------|
| [ ] | `mi-050` | 源碼+Py | **added**：name 不存在 → append `dependencies.mcp` | `mcpinstall.go:444`;`writer.py:96` |
| [ ] | `mi-051` | 源碼 | **unchanged**：name 存在且語意相同 → no-op（不寫檔、不 deploy、印 unchanged） | `mcpinstall.go:434,78-86` |
| [ ] | `mi-052` | 源碼+Py | **conflict**：name 存在且不同、無 `--force`（非互動）→ error "use --force"，**apm.yml 不動** | `mcpinstall.go:437`;`writer.py:118`;test `ExistingConflictWithoutForce_ApmYmlUntouched` |
| [ ] | `mi-053` | 源碼+Py | **replace**：`--force` → 原地取代（"replaced"） | `mcpinstall.go:440`;`writer.py:103` |
| [x] | `mi-054` | **修 D2** | **conflict 三態**（對齊原版 writer.py）：`--force`→靜默取代；**互動 TTY**→顯示 `已存在。取代 diff:` + 各差異行 + `Replace…? [y/N]`（default N），yes→replaced、no→**skipped（視同 unchanged，不寫不 deploy）**；**非 TTY**→error "Use --force to replace (non-interactive)" | `upsertMCPEntry` confirm 參數;`TestUpsertMCPEntry_ConflictConfirm`(4 態) |
| [x] | `mi-054b` | **修 D2** | `diffEntry`（port `_diff_entry`）：bare↔bare→`old -> new`；mapping→逐 key `k: old -> new`（缺值 `<absent>`）；相同→無 diff（=unchanged，早於 confirm） | `TestDiffEntry` |
| [ ] | `mi-055` | 源碼 | name 比對支援 bare string 與 mapping `name` 兩形態 | `mcpinstall.go:448-460` mcpEntryName |
| [x] | `mi-056` | **修 #1** | 新增/取代後 `dependencies.mcp` 序列以 **block style** 輸出（清 FlowStyle）；bare/mapping 皆是；對齊原版 `dump_yaml` block | `upsertMCPEntry` `Style &^= FlowStyle`;`TestUpsertMCPEntry_WritesBlockStyle…`;**live E2E**（`mcp: []`→block 實測） |
| [ ] | `mi-057` | 源碼 | apm.yml **surgical patch**：只重寫 `dependencies.mcp` span，其他位元組/註解/排版保留（PatchMappingPath；不合則 fallback SafeDump） | `mcpinstall.go:105-114`;`patch.go:36` |
| [ ] | `mi-058` | 源碼 | 行尾一致（CRLF 文件維持 CRLF） | `patch.go:184-199` matchLineEndings |
| [x] | `mi-059` | **修 #1** | sibling `dependencies.apm`（同為 init 的 flow `[]`）**不受影響**（本輪只清 mcp 序列 style） | `TestUpsertMCPEntry_WritesBlockStyle…` 斷言 `apm: []` 不變;live E2E 同證 |

## M5 — 部署到 targets（源碼+Py + 實測）

| ✓ | id | 權威 | 驗證內容 | 對照 |
|---|----|----|----------|------|
| [ ] | `mi-060` | 源碼 | ResolveTargets 優先序 `--target` > apm.yml `target:` > auto-detect；印 `[i] Targets: … (source: …)` | `mcpinstall.go:123-135`;test `PrintsTargetSource` |
| [ ] | `mi-061` | 源碼 | 各 MCP target adapter 寫入設定，**headers 一併寫入**：claude `.mcp.json`、codex `http_headers`、copilot、antigravity `.agents/mcp_config.json` | `deploy/mcp_{claude,codex,antigravity}.go`;`mcp_common.go:73-76` |
| [ ] | `mi-062` | 源碼 | 非 MCP-capable target → skipped（不 error），印 skipped 清單 | `mcpinstall.go:146-149`;test `NonMCPTargetIsSkipped` |
| [ ] | `mi-063` | 源碼 | deployed 為空（writer 內部過濾，如非 https url）→ 印 "not deployed to any target"，**不謊報成功** | `mcpinstall.go:155-158`;test `FilteredByWriter_DoesNotClaimSuccess` |
| [ ] | `mi-064` | 異 | **D1**：`--mcp` 路徑**不寫 apm.lock.yaml**（Python 更新 lockfile mcp_servers/mcp_configs） | `mcpinstall.go` runMCPInstall doc;`command.py:120-127` |

## M6 — 輸出、exit code、安全（源碼+實測）

| ✓ | id | 權威 | 驗證內容 | 對照 |
|---|----|----|----------|------|
| [ ] | `mi-070` | 源碼 | unchanged → `[i] MCP server "X" unchanged`；success → `[+] Added/Replaced MCP server "X"` + transport + apm.yml 行 | `mcpinstall.go:84,159-162` |
| [ ] | `mi-071` | 源碼 | apm.yml 不存在 → error 引導 `apm-go init first`（非零 exit） | `mcpinstall.go:45`;test `NoApmYml_Errors` |
| [ ] | `mi-072` | 源碼 | registry 解析失敗 → error，**apm.yml 不寫**（entryNode 只在記憶體） | `mcpinstall.go:92-95`;test `RegistryLookupFailure_ApmYmlUntouched` |
| [ ] | `mi-073` | 源碼+異 | **D6**：`parseKVPairs`（--env/--header）錯誤**不 echo value**（安全） | `mcpinstall.go:590-604`;test `Malformed…NeverEchoesValue` |
| [x] | `mi-074` | **修 #2** | prompt 到的 token **不 echo/log**；隱藏輸入；任何錯誤訊息不含 token | `mcp_prompt.go` `ttyAsk`（`term.ReadPassword`，不 log） |

## M7 — #3 `--header requires --url`（parity，本輪不改）

> **live A/B 已驗證（2026-07-06）**：同一條 `--mcp <registry> --header …`（無 --url），
> apm-go 與 `uv run apm` **都**回 `Error: --header requires --url`（逐字相同）→ parity。
>
> **原版 `--header` 正確用法**（實測）：**搭 `--url` 用於 self-defined remote**，例如
> `--mcp linear --url https://mcp.linear.app/sse --transport sse --header Authorization="Bearer ${LINEAR_TOKEN}"`
> → 原版 `[+] Added`。registry server 不走 --header，靠 #2 互動詢問 / env token。
>
> **非互動帶 token 給 registry server（如 github）的正途 = FORM B**（apm-go 現況即支援、與原版一致）：
> `apm-go install --mcp github --url https://api.githubcopilot.com/mcp/ --transport streamable-http --header Authorization="Bearer ${MY_TOKEN}"`
> → 已實測寫入 apm.yml(self-defined url+headers) 並部署 `.mcp.json`。實務用 `${ENV}` 佔位避免明碼。
>
> **「放寬 E9 讓 registry 也能 --header」增強 → 使用者 2026-07-06 決定不做**（FORM B 已覆蓋，
> 不值得為窄縫偏離原版）。若未來要做需另建 checklist（D7）。

| ✓ | id | 權威 | 驗證內容 | 對照 |
|---|----|----|----------|------|
| [x] | `mi-080` | 源碼+Py+**實測A/B** | `--header` 無 `--url` → error，與原版 E9 **一致**（live A/B 逐字相同）；registry server 帶 token 正途＝#2 互動詢問，非互動＝FORM B(self-defined --url) | `mcpinstall.go:190`;`conflicts.py:98-100`;live A/B |

## M8 — 憑證佔位 per-target 處理（bake→translate，本輪新增，使用者 2026-07-06）

> **問題**：apm-go 對 claude/codex/opencode 用 **bake**，把 header 的 `${VAR}` 從
> `os.LookupEnv` 展開烘進被 commit 的設定檔（secret 外洩）；變數未設時警告 + 省略 header。
> **原版**：header 一律走 `_resolve_variable_placeholders`（**保留 `${VAR}`，MUST NOT read
> os.environ**，issue #1152）→ apm-go 對 header bake 是**偏離原版 + 安全問題**。
> 詳見 `research/cred-placeholder-parity.md`。範圍 = claude + opencode + codex（使用者定）。
>
> **各 target 正確寫法（官方文件實證，語法各異）**：
> - **claude** `.mcp.json`：`${VAR}` / `${VAR:-default}` inline（command/args/env/url/headers）
> - **opencode** `opencode.json`：**`{env:VAR}`** inline（headers/env；語法不同）
> - **codex** `config.toml`：**不支援 inline `${VAR}`**；用 `bearer_token_env_var="VAR"`
>   （bearer）/ `env_http_headers={header:"VAR"}` / `http_headers`（僅靜態）
>
> **✅ 決策已定（使用者 2026-07-06）**：
> - **DEC-1 = B**：codex 用 `bearer_token_env_var`（Authorization bearer）/ `env_http_headers`
>   （其他 header 名→env var 名）/ `http_headers`（靜態）。實際可用、偏離原版（原版寫 `${VAR}` 進 http_headers）。
> - **DEC-2/3 = 只改 remote header**：拆開 `resolveMCPServer`，**header 用 per-adapter 編碼、env dict 維持
>   現有 bake**（最貼原版：原版正是 header 保留佔位、env 依旗標 bake）。本輪不動 stdio `env` 佔位。

| ✓ | id | 權威 | 驗證內容 | 對照 |
|---|----|----|----------|------|
| [x] | `mp-001` | 修 parity+安全 | **claude**：headerMode→Translate；header `${VAR}` 保留不烘（`.mcp.json` 原生支援）；部署檔無明碼 secret。**live 實測 `Bearer ${MY_TOKEN}` 保留** | `mcp_claude.go`;`TestMCPEntry_HeaderPlaceholderPerTarget` |
| [x] | `mp-002` | 修 parity+安全 | **opencode**：headerMode→Translate **+ `translateOpencodeHeaders`** `${VAR}`/`${env:VAR}`→`{env:VAR}`。**live 實測 `Bearer {env:MY_TOKEN}`** | `mcp_opencode.go`;`TestTranslateOpencodeHeaders` |
| [x] | `mp-003` | 修（DEC-1=B） | **codex**：`encodeCodexHeaders` → `Bearer ${VAR}`→`bearer_token_env_var="VAR"`；整值 `${VAR}`→`env_http_headers`；靜態→`http_headers`。**live 實測 `bearer_token_env_var='MY_TOKEN'`** | `mcp_codex.go`;`TestEncodeCodexHeaders` |
| [x] | `mp-004` | 新增 | `translateOpencodeHeaders` + codex `soleEnvVar`/`bearerEnvVar`（重用 `manifest.EnvVarRe`，idempotent） | `TestSoleEnvVar`/`TestBearerEnvVar` |
| [x] | `mp-005` | 安全紅線 | 三 target 部署設定檔**不再含明碼 secret**（header 佔位保留/轉換/env-ref）；`resolveMCPServer` 拆 header/env mode，header 不讀 os.environ | `mcp_common.go` resolveMCPServer;live 4-target 驗證 |
| [x] | `mp-006` | 迴歸 | 既有 deploy 測試（用字面 header 值）不受影響仍綠；copilot/antigravity 行為不變；`resolveMCPServer` test 呼叫補參數 | `go test ./internal/deploy 全綠` |
| [x] | `mp-007` | P0 驗證 | codex 用 `bearer_token_env_var`/`env_http_headers`（DEC-1=B，非 inline `${VAR}`）；opencode `{env:VAR}` 用於 headers；env dict 本輪不動（DEC-2/3） | 官方文件 + live E2E |
| [x] | `mp-008` | 異 | Deviation D7（codex bearer_token_env_var）、D8（claude `${VAR}`/opencode `{env:VAR}` header）已記於總表；env dict 未動（維持 parity） | Deviation 總表 D7/D8 |

## Phase V — 驗證完整性（適用全程）

| ✓ | id | 權威 | 驗證內容 | 對照 |
|---|----|----|----------|------|
| [x] | `mi-V01` | 修 #1 | **block style（bare）**：apm.yml 含 `mcp: []`(flow) → upsert bare-string registry → 輸出 `mcp:\n  - …` block，非 `[…]` | `TestUpsertMCPEntry_WritesBlockStyle…/bare_string` |
| [x] | `mi-V02` | 修 #1 | **block style（mapping）**：upsert self-defined/registry+version mapping → block | `…/mapping` |
| [x] | `mi-V03` | 修 #1 | **surgical**：含 `dependencies.apm`+其他內容/註解/手工排版的既有 apm.yml，upsert 後只有 mcp span 變、其餘位元組不動；`apm: []` 不變（舊坑 1） | 測試斷言 `apm: []` 不變 + PatchMappingPath 既有測試綠;live E2E |
| [x] | `mi-V04` | 修 #2 | **互動注入**：stub `ask` → `Authorization`→`Bearer <token>`、其他 header 原值、空輸入略過、`looksSecret` 判斷 | `TestCollectHeaderValues`+`TestLooksSecret` |
| [x] | `mi-V05` | 修 #2 | **非互動**：管線 stdin / `CI`/`APM_E2E_TESTS` env → 不 prompt、不阻塞；`isNonInteractiveEnv` env 矩陣 | `TestIsNonInteractiveEnv`;`withNonInteractiveStdin` 迴歸 |
| [x] | `mi-V06` | 修 #2 | **apm.yml 無明碼 secret**：#2 後 registry 條目仍 bare string、不含 token（Q2） | resolveFromRegistry 只設 dep.Headers、不動 entryNode;`_Success` persisted 無 url/secret |
| [x] | `mi-V10` | 修 D2 | **conflict 三態**：stub confirm →（a）accept→replaced+寫檔；（b）decline→skipped、apm.yml 不動、不 deploy；（c）confirm=nil(非TTY)→error "non-interactive"；（d）force→replaced 不問 | `TestUpsertMCPEntry_ConflictConfirm`(4 子測試) |
| [x] | `mi-V07` | 回 | **既有回歸**：M1 衝突矩陣、M2 三分支建構、M3 registry(success/notfound/pinned/env/credentialed)、M4 upsert(added/unchanged/conflict/replace)、M5 deploy、E2E(SelfDefinedURL/TargetSource/FilteredByWriter) 全部既有測試維持綠 | `go test ./... 全綠`(cmd/apm 15.2s) |
| [~] | `mi-V08` | — | `go build ./…`、`go vet ./…`、`go test ./…` 全綠；覆蓋率 **83.0%** (≥80%✓)。**`-race` 因本機無 gcc/cgo 無法跑**（環境限制，非程式問題）→ 須於有 gcc 的環境補跑 | build/vet/test 綠;`cover 83.0%` |
| [ ] | `mi-V09` | — | **A/B vs `uv run apm`**（`D:\Projects\apm-dev\evals`）：apm.yml 輸出(block) + 互動 prompt 流程比對；Deviation 總表 D1–D8 明列，非掩蓋 | **待做**(AC7) |
| [x] | `mp-V01` | 修 M8 | **claude**：`--header 'Authorization=Bearer ${MY_TOKEN}'` 部署後 `.mcp.json` = `Bearer ${MY_TOKEN}`（佔位保留、無警告、無 omit）。**live 實測✓** | `mp-001` |
| [x] | `mp-V02` | 修 M8 | **opencode**：`opencode.json` = `Bearer {env:MY_TOKEN}`（語法轉換正確）。**live 實測✓** | `mp-002,004` |
| [x] | `mp-V03` | 修 M8 | **codex**（DEC-1=B）：`.codex/config.toml` `bearer_token_env_var = 'MY_TOKEN'`。**live 實測✓** | `mp-003` |
| [x] | `mp-V04` | 安全 | 三 target header 以佔位/`{env:VAR}`/`bearer_token_env_var` 呈現，**不烘明碼 token**（header 不讀 os.environ）；copilot 亦保留 `${VAR}` | `mp-005`;live 4-target |
| [x] | `mp-V05` | 迴歸 | `go build`、`go vet`、`go test ./...` **全綠**；deploy 測試（含既有字面 header）綠；copilot/antigravity 不變 | 全庫測試 |

## 每個 Phase 完成時的自我聲明範本

> 「已完成 M<n>（mi-0xx~mi-0yy），親自重跑 `go build/vet/test ./...` 全綠；#1 對照 go-yaml
> block 輸出、#2 對照 `uv run apm` 互動流程一致（deviation：D1–D6 已列）；舊坑 fixture
> （含既有 `dependencies.apm`+使用者手寫內容）已納入測試矩陣並證明只改 mcp span。
> Completed: M/N。」
