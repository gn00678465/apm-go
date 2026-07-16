# Research: 輸出完整度稽核 (output parity audit, R7/R11 同類缺口)

- **Query**: 對 `cmd/apm-go` 每一個面向使用者的指令做「輸出完整度稽核」，找出 R7(uninstall)/R11(mcp) 同類缺口——指令已經算出某些資料但 stdout 沒印出來或壓縮成一個數字。
- **Scope**: internal (Go 原始碼閱讀，逐一函式核對回傳值/區域變數)
- **Date**: 2026-07-15

## Findings

### 涵蓋清單與稽核狀態

| 指令 | 檔案 | 稽核狀態 |
|---|---|---|
| install (含 local-bundle) | `cmd/apm-go/install.go` | 讀完全檔（1623 行），找到 2 個缺口 |
| update | `cmd/apm-go/update.go` | 讀完全檔，未發現新缺口（`printUpdateSummary` 已完整列出每個變動的 dep old->new；dry-run 與正式 run 共用同一函式） |
| compile | `cmd/apm-go/compile.go` + `internal/compile/compile.go` | 讀完全檔，找到 1 個缺口（需業務層） |
| audit（bare + `--content`） | `cmd/apm-go/audit.go`, `cmd/apm-go/audit_content.go` | 讀完全檔，bare 模式找到 1 個缺口；`--content` 已經對每個 finding 印出檔名/行號，無缺口 |
| pack | `cmd/apm-go/pack.go` | 讀完全檔，找到 1 個缺口（bundle producer 正式執行分支） |
| experimental | `cmd/apm-go/experimental.go` | 讀完全檔，無缺口（list/enable/disable 皆完整） |
| marketplace list/browse/update/remove/validate/build | `cmd/apm-go/marketplace.go` | 讀完全檔，無缺口（皆已有 `--verbose` 或本就完整） |
| marketplace init/check/outdated | `cmd/apm-go/marketplace_authoring.go` | 讀完全檔，無缺口 |
| marketplace audit | `cmd/apm-go/marketplace_authoring_audit.go` | 讀完全檔，無缺口（tree 呈現完整） |
| marketplace migrate | `cmd/apm-go/marketplace_authoring_migrate.go` | 讀完全檔，無缺口 |
| marketplace package add/set/remove | `cmd/apm-go/marketplace_package.go` | 讀完全檔，無缺口（`--verbose` 對這三個子指令刻意無作用，註解明載為對齊 Python 原版行為，非疏漏） |
| mcpinstall (`install --mcp`) | `cmd/apm-go/mcpinstall.go` | 讀完全檔，找到 1 個缺口（R11 本體，予以精確定位） |
| init | `cmd/apm-go/init.go` | 讀完全檔，無強缺口（確認前的 Box 已完整列出 name/version/description/author/targets；成功訊息不重複列印屬合理，Python 平行行為未知，不誤判） |

### 缺口總表

| # | 指令 | 錨點 file:line | 現在印什麼 | 已算出但沒印的資料 | presentation-only? | 建議 R 編號/嚴重度 |
|---|---|---|---|---|---|---|
| 1 | `install --mcp NAME` | `cmd/apm-go/mcpinstall.go:148-174` | `deployed, skipped, err := deployMCPEntry(...)` 算出 `deployed []string`（實際部署到的 target 清單），但成功訊息只印 `ux.Success(... "%s MCP server %q", verb, opts.Name)` + 固定的 `BulletList{"transport: ...", "apm.yml: apm.yml"}`（line 171-174，**"apm.yml: apm.yml" 是寫死字串，非真實路徑**）。`deployed` 只有在 `len(skipped)>0`（line 157-160）時才會連帶出現在 "Skipped MCP config for X (active targets: deployed...)"；沒有 skip 時 `deployed` 完全不出現在 stdout。 | `deployed []string`（成功部署的 target 名稱清單）；真實 apm.yml 絕對路徑 | 是（`deployed` 已是回傳值，`os.Getwd()+filepath.Join` 求絕對路徑亦是純函式） | **R11 本體**（已知），嚴重度高：使用者看不到到底裝到哪些 target |
| 2 | `install <bundle-path>`（local bundle 安裝） | `cmd/apm-go/install.go:715-767`（成功列印在 `install.go:765`） | `ux.Success(os.Stdout, "Installed %d file(s) from local bundle %s", len(result.Files), bundleArg)` —— 只印檔案數 | `result.Files []string`（`localbundle.IntegrateResult.Files`，deploy 出的每個檔案的相對路徑，line 748-765 已在記憶體中；`result.Hashes` 亦有）；`targets`（已知道部署到哪些 target，line 731-742 已 resolve 過，但只在 target-mismatch warning 時才提及，不在成功訊息中列出） | 是（`result.Files`/`targets` 都是既有區域變數，直接 `ux.BulletList` 即可，和 R7/R11 完全同型） | 新建議 R 編號（與 R11 同類），嚴重度中高：跟一般 `apm install` 的 `deployedFilesTree` 詳列每個 dep 部署的檔案分組相比，local-bundle install 反而退化成只印數字 |
| 3 | `uninstall`（非 `--dry-run`，收尾摘要） | `cmd/apm-go/uninstall.go:619-628`（`printUninstallSummary`），呼叫點 `uninstall.go:309` | `ux.Success(... "Removed %d package(s) (+%d transitive orphan(s))", removedPackages, len(orphans))` + `ux.Success(... "apm_modules: removed %d director%s", removedModuleDirs, ...)` —— 全部壓縮成數字 | `resolution.APMTargets`/`resolution.MCPTargets`（套件名稱，`uninstallAPMTarget.Name`/`uninstallMCPTarget.Name`，`applyUninstallPlan` 一路帶著走到 `printUninstallSummary(plan.resolution, ...)`，line 309）；`orphans map[string]bool`（transitive orphan 的 key 名稱，非 dry-run 時完全沒印，即使 `--verbose` 也不印 —— 對照 `printUninstallDryRunPlan` line 596-603 在 dry-run 模式下會用 `ux.BulletList` 逐一印出 orphan key） | 是（`plan.resolution`/`orphans` 就是傳進 `printUninstallSummary` 的參數，直接改函式簽章印出即可；dry-run 版本 `printUninstallDryRunPlan` 已經證明同一份資料印得出來） | **R7 本體**（題目已知），嚴重度高：dry-run 能看到完整套件名/orphan 清單，真正執行反而看不到，等於「預覽比實際執行更詳細」 |
| 4 | `pack`（bundle producer，非 `--dry-run`） | `cmd/apm-go/pack.go:201-258`，成功列印在 `pack.go:252` | `ux.Success(w, "Packed %d file(s) -> %s", len(result.Files), result.BundleDir)` —— 只印檔案數 | `result.Files []string`（bundle 內每個檔案的相對路徑，同一個 `result.Files` 在 `--dry-run` 分支〔line 243-250〕已經被 `ux.Section` + `ux.BulletList` 逐一列出！非 dry-run 分支卻只取 `len()`） | 是（100% presentation-only：dry-run 分支已經示範了完整寫法，只是沒有把它搬到正式執行分支） | 新建議 R 編號（與 R7/R11 同型態的「dry-run 詳細、正式執行反而簡略」），嚴重度中高：這是本次稽核中「資料明明就在，甚至上一行程式碼就示範了怎麼印」最直接的例子 |
| 5 | `audit`（bare，無 `--content`，成功案例） | `cmd/apm-go/audit.go:83-89` | `ux.Success(cmd.OutOrStdout(), "audit: %d deployed files verified", count)` —— `count` 是把 `lock.Dependencies[i].DeployedHashes` 與 `lock.LocalDeployedHashes` 兩個 map 的長度加總 | 每個 map 的 **key 就是被驗證的檔案相對路徑**（`internal/lockfile` 的 `DeployedHashes map[string]string`/`LocalDeployedHashes map[string]string`，key=deployed file path），這份清單完全沒有印出來，即使失敗分支（line 70-81）也只逐一印「不一致」的檔案，全部通過時反而只給一個數字 | 是（`lock.Dependencies[i].DeployedHashes` 的 key 集合本來就在記憶體中，遍歷一次即可列出） | 新建議 R 編號，嚴重度中：比對 R7/R11 稍弱（全部通過時「無異常」本身不算高資訊量損失，但仍是「已算出清單、只印數字」的同型缺口） |
| 6 | `install`（frozen 模式成功） | `cmd/apm-go/install.go:369-538`，成功列印在 `install.go:537` | `ux.Success(os.Stdout, "Frozen install: all dependencies pinned and verified")` —— 完全沒有數字或清單 | `existingLock.Dependencies`（已知道總共驗證了幾個 dependency、哪些是 registry/哪些是 git，這些資訊在函式體內反覆走訪，line 396-535） | 是（`len(existingLock.Dependencies)` 或逐一列出 `dep.UniqueKey()` 都可以從既有變數取得） | 新建議 R 編號，嚴重度低：比起其他項目資訊量損失較小（frozen install 本身就是「照鎖檔跑」，使用者通常已知道有哪些 dep），但仍與其他 6 項同屬「資料存在、只印固定字串」型態 |
| 7 | `compile` | `cmd/apm-go/compile.go:68-77` 呼叫 `internal/compile/compile.go:190-201`（`Run`） | `ux.Success(os.Stdout, "Compiled %d instruction(s) to %s", result.InstructionCount, result.Path)` —— `Result` struct（`internal/compile/compile.go:182-186`）只保留 `Wrote/InstructionCount/Path` 三個欄位 | `Run` 內部呼叫 `CollectInstructions` 取得的 `instructions []SourcedInstruction`（含每條 instruction 的來源檔案/dep 等資訊，`internal/compile/compile.go:72` 起的 `CollectInstructions` 簽章回傳 `[]SourcedInstruction`）在 `Run` 內被算出，只取 `len(instructions)` 存進 `Result.InstructionCount`（line 200），原始清單被丟棄，`cmd/apm-go/compile.go` 完全拿不到 | **否，需業務層資料**（`internal/compile.Result` struct 目前沒有欄位可以帶出逐條 instruction 的來源；要修的話得新增 `Result` 的欄位並讓 `Run` 填入，牽涉 `internal/compile` 套件本體，不是純 cmd 層加一行 print 可解決） | 需業務層資料，另立子任務評估是否要暴露 `SourcedInstruction` 清單；嚴重度低-中（compile 是低頻指令，且目前 `AGENTS.md` 本身就是輸出檔案，使用者可以直接看內容，不是純黑盒） |

### (a) 已被 R7/R11 涵蓋者

- **#1（mcpinstall 的 `deployed` 清單 + 寫死路徑）**＝題目背景描述的 R11 本體，直接對應 `cmd/apm-go/mcpinstall.go:170-174`。
- **#3（uninstall 的 `printUninstallSummary` 壓縮數字）**＝題目背景描述的 R7 本體，直接對應 `cmd/apm-go/uninstall.go:619-628`（呼叫點 `uninstall.go:309`）。

其餘 5 項（#2 local-bundle install、#4 pack、#5 audit bare、#6 install frozen、#7 compile）是本次稽核新發現、與 R7/R11 同屬「已算出、未印出/被壓縮成數字」型態的缺口，尚未被既有 R 編號涵蓋。

### (b) 「需業務層資料」清單

只有 **#7 compile** 落在這個分類——`internal/compile.Result` struct 目前結構本身就沒有欄位可以承載逐條 instruction 的來源清單，需要先在 `internal/compile` 套件新增欄位（例如 `Instructions []SourcedInstruction` 或精簡過的摘要 slice）讓 `Run` 填入，才有東西可以在 `cmd/apm-go/compile.go` 印出來。這超出「純 cmd 層補一行 `ux.BulletList`」的範圍，建議另立子任務評估要不要做、要暴露多少細節（避免过度设计——多數使用者可能只想看 `AGENTS.md` 本身，不見得需要 CLI 逐條列出）。

其餘 #1-#6 全部是 **presentation-only**：資料已经是函式内的具體回傳值/區域變數（`deployed []string`、`result.Files []string`、`plan.resolution`/`orphans`、`result.Files`〔pack〕、`lock.Dependencies[i].DeployedHashes` 的 key 集合、`existingLock.Dependencies`），只要在 cmd 層加印（`ux.BulletList`/`ux.Tree`）即可，不需要改動任何業務邏輯或新增計算。

### (c) 最高優先前 5 名（依「資訊損失程度 x 修復成本低」排序）

1. **#3 uninstall 收尾摘要**（`uninstall.go:619-628`，R7 本體）—— dry-run 能看到完整套件名+orphan 清單，正式執行卻只有數字，落差最大且是本任務背景明確點名的案例；修復成本低（`plan.resolution`/`orphans` 已經傳進函式）。
2. **#1 mcpinstall 的 `deployed` 清單 + 寫死 `apm.yml: apm.yml` 路徑**（`mcpinstall.go:170-174`，R11 本體）—— 同上，題目背景明確點名；`deployed` 是現成回傳值，路徑用 `os.Getwd()` 即可修正。
3. **#4 pack bundle producer 正式執行分支**（`pack.go:252`）—— 全稽核中最直接的「資料就在隔壁幾行」案例：`--dry-run` 分支已經用 `ux.Section`+`ux.BulletList` 印出同一個 `result.Files`，正式執行分支卻只取 `len()`，修復幾乎是複製貼上。
4. **#2 local-bundle install 成功摘要**（`install.go:765`）—— 一般 `apm install` 已經有非常完整的 `deployedFilesTree` 分組列印（見 `install.go:1183-1211` 的 `deployedFilesTree`），唯獨 local-bundle 這條路徑退化成純數字，反差明顯；`result.Files`/`result.Hashes` 都是現成變數。
5. **#5 audit bare 成功案例**（`audit.go:88`）—— 失敗分支已經逐一印出每個違規檔案（line 70-81），成功分支卻只給總數，是「錯誤路徑比成功路徑資訊更豐富」的不對稱，且 map key 即檔案路徑，取值零成本。

（#6 install frozen 成功摘要與 #7 compile 的業務層缺口，資訊損失/使用頻率相對較低，列在總表中但不進前五。）

## Caveats / Not Found

- `validate`/`normalize`（`cmd/apm-go/main.go:42-113`）不在題目列出的稽核範圍內，未深入比對 Python 對應指令的輸出，僅粗略確認其現有輸出邏輯本身無明顯「算出但沒印」的缺口（`validate` 成功且無 diagnostics 時不印任何東西，是否為 Python parity 缺口未查證，不在本次結論內）。
- 本稽核僅比對「apm-go 自己算出的資料 vs 自己印出的資料」，**未** 逐一對照 Python `apm` 原版的實際輸出文字（沒有執行 Python 版本做逐字比對）；每一列的「presentation-only」判定純粹基於 Go 原始碼中該資料是否已作為函式回傳值/區域變數存在，未臆測 Python 端是否真的印出對應內容。
- `marketplace package add/set/remove` 的 `--verbose` 刻意無作用一節，已在原始碼註解中明確說明是刻意對齊 Python 原版行為（"mirrored here as-is"），因此未列為缺口；但如果之後要做「比 Python 更好」的體驗優化，仍可以考慮讓 `--verbose` 真的印出點什麼（非本次稽核範圍，僅記錄於此供參考）。
