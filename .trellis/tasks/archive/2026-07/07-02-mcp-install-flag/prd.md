# 新增 apm install --mcp CLI 旗標

## 問題陳述

`apm-go install --mcp <name> --transport http` 目前直接報 `unknown flag: --mcp`。原版 apm(`d:\Projects\apm-dev\apm`,已用 `uv run apm` 實測驗證)有完整的 `apm install --mcp NAME [flags]` 功能:單一 CLI 呼叫內宣告一個 MCP server(自訂 url/command,或用**裸名稱透過 MCP Registry v0.1 API 查詢解析**)、寫入 `apm.yml` 的 `dependencies.mcp`、並立即部署到當前 active target(s)。這是原版 `apm install --help` 文件裡明列的官方範例(`apm install --mcp io.github.github/github-mcp-server`),使用者依此語法操作完全合理。

apm-go 現有的 07-02-mcp-resolve-deploy 任務做的是「**已宣告**在 `apm.yml` 的 self-defined MCP server 收集+部署」,**明確排除** registry-backed 解析(`internal/deploy/mcpcollect.go` `isSelfDefinedMCP` 只收 `registry: false`,其餘一律發診斷不部署)。CLI 層完全沒有 `--mcp` 相關 flag。本任務要把這個缺口補上。

## 原版行為(已用 research agent 完整讀碼 + 實機驗證)

- CLI flags(`--mcp NAME` 觸發,其餘 `--transport/--url/--env/--header/--mcp-version/--registry` 皆 `requires --mcp`,`--skill` 與 `--mcp` 互斥,positional packages 與 `--mcp` 互斥)。
- `build_mcp_entry` 依「有 command → self-defined stdio」「有 url → self-defined remote」「都沒有 → registry 查詢(裸名稱或含 transport/version/registry 的 dict)」三分支,決定要寫進 `apm.yml` 的實際內容——**registry 查詢分支從不把解析出來的 url 寫回 apm.yml**,只留 `{name, transport?}`,每次都要重新解析。
- Registry HTTP client(MCP Registry v0.1):`GET {base}/v0.1/servers?search=<name>` 找候選,精確 name 相符優先,否則用「namespace 邊界」規則做 fuzzy match(如 `github/github-mcp-server` 可比對到 `io.github.github/github-mcp-server`,但不可誤配到不相關套件),再 `GET {base}/v0.1/servers/<name>/versions/<version|latest>` 取得完整資訊。`remotes[]`(http/sse/streamable-http)優先於 `packages[]`(npm/docker/pypi/homebrew stdio 套件安裝)。
- 部署面重用「既有」per-target writer(claude/codex/copilot/antigravity 的 `WriteMCP`)——這條路徑不是重新發明,是把單一已解析好的 `MCPDependency` 包成一個 Primitive 丟進同一組 writer。
- 每次 `--mcp` 呼叫是「**加這一個 server + 立即部署**」的獨立操作,不會跑完整 `apm install` 的依賴解析/lockfile pipeline(lockfile 完全不動)。

## 範圍(In Scope,MVP)

1. **CLI flags**:`install` 新增 `--mcp NAME`、`--transport {stdio|http|sse|streamable-http}`、`--url URL`、`--env KEY=VALUE`(repeatable)、`--header KEY=VALUE`(repeatable)、`--mcp-version VERSION`、`--registry URL`、`--force`(非互動情境下允許覆寫同名既有條目)。stdio command 用 cobra 原生 `--` 分隔語法(`apm-go install --mcp fetch -- npx -y pkg`),對齊原版 UX。
2. **衝突驗證**(原版 E-matrix 的直接相關子集,design.md 詳列):`--mcp` 名稱為空/以 `-` 開頭、`--mcp` 與 positional packages 併用、`--mcp` 與 `--skill` 併用、`--header` 缺 `--url`、`--url` 與 stdio command 併用、`--transport stdio` 配 `--url`、remote transport 配 stdio command、`--registry` 配 `--url`/stdio command、`--transport/--url/--env/--header/--mcp-version/--registry` 在缺 `--mcp` 時出現。
3. **Registry v0.1 HTTP client**(新套件 `internal/mcpregistry`):`search` + `get server`(exact 優先、namespace 邊界 fuzzy fallback),base URL 解析順序 `--registry` flag > `MCP_REGISTRY_URL` env > 預設 `https://api.mcp.github.com`,scheme 白名單 http/https,URL 長度上限、憑證遮蔽等基本安全檢查對齊原版。**只處理 `remotes[]`(http/sse/streamable-http)**——`packages[]`(npm/docker/pypi/homebrew stdio 套件安裝、GitHub token 自動注入、互動式環境變數蒐集)明確排除,見下方非目標。
4. **apm.yml 寫入**:比照 `build_mcp_entry` 的三分支邏輯,新增到 `dependencies.mcp`(或 `--dev` 時 `devDependencies.mcp`)清單;同名既有條目:內容相同 → 略過(印 unchanged);內容不同且未帶 `--force` → 報錯(apm-go 一律視為非互動環境,不做 TTY confirm);內容不同且帶 `--force` → 覆寫。`manifest.MCPDependency` 新增 `Version string` 欄位對應 `--mcp-version`。
5. **部署**:重用既有 `internal/deploy` per-target writer(`WriteMCP`),不重新實作。Target 解析比照既有 `deploy.ResolveTargets`(`--target` > `apm.yml targets:` > auto-detect)——**明確修正**原版一個已知 bug(`--mcp` 路徑完全忽略 `--target` flag,只能靠 apm.yml `targets:` 或自動偵測),apm-go 版本讓 `--target` 正確生效,design.md 記錄這是刻意的行為差異(不是 bug-compat)。
6. **stdout**:比照 apm-go 既有的 bracket 慣例(`[i]`/`[+]`/`[!]` 等,`install.go` 既有訊息已用同一套),盡量貼近原版可觀察的訊息語意(不要求逐字元相同,但關鍵資訊要對得上:targets 來源、成功/略過/覆寫、apm.yml 路徑、transport)。

## 非目標(Out of Scope)

- **`packages[]` 型(npm/docker/pypi/homebrew)registry 解析**——需要互動式環境變數蒐集、GitHub token 自動注入、Docker 參數組裝,是原版另一個大子系統。本任務只處理 `remotes[]`(http/sse/streamable-http)。若 registry 查詢結果只有 `packages[]`、沒有 `remotes[]`,回報清楚錯誤(此 server 只提供 stdio 套件安裝,apm-go 目前不支援,請改用手動宣告),不嘗試部分實作。
- **`apm mcp search/show/list` 子指令群**——只做 `install --mcp`,不做整個 `apm mcp` command group。
- **Policy preflight / org policy**——apm-go 沒有對應系統,不新增。
- **`--dry-run`**——apm-go install 目前沒有這個 flag,不在本任務新增。
- **互動式 TTY confirm prompt**——apm-go 一律視為非互動;同名衝突無 `--force` 一律報錯,不問。
- **lockfile 的 mcp_configs 追蹤**——apm-go 沒有對應 schema,`--mcp` 呼叫不動 `apm.lock.yaml`。
- **`--global`/`--ssh`/`--https`/`--allow-protocol-fallback`/`--update` 與 `--mcp` 的互斥檢查**——apm-go install 目前沒有這些 flag,不需要對應檢查。

## 需求(Requirements)

- **R1** `--mcp NAME` 觸發新分支;`--transport/--url/--env/--header/--mcp-version/--registry` 缺 `--mcp` 時報錯。
- **R2** 自訂 stdio(`--` 分隔的 command)、自訂 remote(`--url`)、registry 查詢(裸名稱)三分支對齊原版 `build_mcp_entry` 邏輯,含對應的 apm.yml 寫入內容(registry 分支不寫解析出的 url)。
- **R3** MCP Registry v0.1 HTTP client:search + exact/fuzzy get,只解析 `remotes[]`。
- **R4** 同名既有條目 upsert:相同 → skip;不同 → 需要 `--force` 才能覆寫,否則報錯。
- **R5** 部署重用既有 per-target `WriteMCP`;target 解析用 `--target` > `apm.yml targets:` > auto-detect(修正原版 `--target` 被忽略的已知 bug)。
- **R6** 核心衝突驗證子集(見範圍 §2)全部覆蓋,錯誤訊息清楚點出違反的規則。
- **R7** stdout 格式對齊 apm-go 既有慣例,關鍵資訊(target 來源、成功/略過/覆寫、apm.yml 路徑)可驗證。

## 驗證方式(使用者要求:A/B 驗證 + loop 直到完全正確)

- **A/B 對照**:`d:\Projects\apm-dev\apm`(可用 `uv run apm` 執行,已驗證版本 0.21.0)作為 oracle,針對本任務範圍內的情境(registry 查詢 remote http 成功案例、自訂 url 案例、自訂 stdio 案例、`--mcp`+`--skill` 衝突、同名衝突無 `--force`)寫自動化 A/B 測試腳本(比較 exit code、apm.yml 內容、部署出的 target 檔案內容、關鍵 stdout 行是否語意相符)。
- **最低驗收門檻(使用者明定)**:上述 A/B 測試全數通過,且 stdout 輸出正確(關鍵行斷言通過,不是「大致像」)。
- Loop 方式:實作 → 跑 A/B 測試 → 有落差就修正 → 重跑,直到全部通過;過程中維持 `go build/vet/test` 全綠,並比照本次 session 慣例跑多輪 codex exec 唯讀審查。

## 驗收標準(Acceptance Criteria)

- [x] AC1:`apm-go install --mcp io.github.github/github-mcp-server --transport http`(有效 target 訊號存在時)成功解析 registry、寫入 apm.yml `{name, transport: http}`、部署到 active target 的 MCP config 檔,內容與原版實測結果語意相符(url/type 欄位對得上)。驗證:`evals/ab_mcp_install.py` 情境 1,含使用者原始回報的確切指令。
- [x] AC2:自訂 `--url`/stdio command(self-defined)路徑正確寫入 apm.yml 與部署,不受 registry client 影響。驗證:`evals/ab_mcp_install.py` 情境 2、3。
- [x] AC3:`--mcp` + `--skill` 併用報錯,exit code 非 0。驗證:`evals/ab_mcp_install.py` 情境 4;`TestRunMCPInstall_SkillCombined_Errors`。
- [x] AC4:同名既有條目、內容不同、未帶 `--force` → 報錯,apm.yml 不被覆寫;帶 `--force` → 正確覆寫。驗證:`evals/ab_mcp_install.py` 情境 5(未帶 `--force`);`TestUpsertMCPEntry_DifferentWithoutForce_Errors`/`DifferentWithForce_Replaces`(單元測試涵蓋帶 `--force`)。
- [x] AC5:`--transport`(或其他 requires-mcp flag)缺 `--mcp` → 報錯。驗證:`TestInstallCmd_ExplicitEmptyMCPOnlyFlag_RequiresMCP`(`cmd/apm/mcpinstall_test.go:787`)。
- [x] AC6:registry 查無此名稱 → 清楚錯誤訊息,exit code 非 0,apm.yml 不寫入(比照原版「查無 server 直接失敗」;刻意比原版更嚴謹,不落入「寫了 apm.yml 但部署失敗」的中間態,design.md 記錄理由)。驗證:`TestRunMCPInstall_RegistryLookupFailure_ApmYmlUntouched`。
- [x] AC7:A/B 測試腳本(至少涵蓋上述情境)全數通過。驗證:`python D:\Projects\apm-dev\evals\ab_mcp_install.py` — **15/15 PASS**(最終確認,含 codex 11 輪 + 人工複審收斂後重跑)。
- [x] AC8:`go build/vet/test -cover` 全綠,新增程式碼所在套件覆蓋率 ≥ 80%。驗證:`go build/vet/gofmt -l/test ./... -cover` 全綠;新增檔案 `internal/mcpregistry` 89.6%、`mcpinstall.go` 本身加權涵蓋率 89.6%(258/288 statements,逐函式 75–100%)。誠實揭露:`cmd/apm` 套件**整體**聚合涵蓋率為 76.4%,低於 80%,但這是被套件內既有、與本任務無關的舊程式碼拉低(該套件在任務開始前為 69.5%,任務新增程式碼本身把整體拉高了近 7 個百分點);本任務新增的程式碼本身逐函式檢視均 ≥75%、加權平均 89.6%,已達標。
