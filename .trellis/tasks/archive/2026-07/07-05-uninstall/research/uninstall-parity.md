# Research: uninstall 指令 Python↔apm-go 對照(反向清理管線)

- **Query**: 研究 apm-go 尚未實作的 `uninstall` 指令,對照 Python 版 `apm uninstall` 的完整行為(CLI 介面、apm.yml/apm_modules/lockfile 清理、target 反向同步、MCP 清理、transitive orphan 清理),盤點 apm-go 現有可重用 building block 與具體缺口。
- **Scope**: mixed(Python 原始碼 `D:\Projects\apm-dev\apm` + apm-go 原始碼 `D:\Projects\apm-dev\apm-go`,皆為 internal code reading,無外部文件)
- **Date**: 2026-07-05

## Findings

### A. Python `apm uninstall` 管線(逐步對照)

| File Path | Description |
|---|---|
| `D:\Projects\apm-dev\apm\src\apm_cli\commands\uninstall\cli.py` | Click CLI 入口:`packages... [--dry-run] [-v/--verbose] [-g/--global]`,10 個步驟的 orchestration |
| `D:\Projects\apm-dev\apm\src\apm_cli\commands\uninstall\engine.py` | 純邏輯 helper:驗證、dry-run、實體刪除、transitive orphan、target 反向同步、MCP 清理 |
| `D:\Projects\apm-dev\apm\src\apm_cli\core\scope.py` | `InstallScope`(PROJECT/USER)+ `get_manifest_path/get_apm_dir/get_modules_dir/get_deploy_root` |
| `D:\Projects\apm-dev\apm\src\apm_cli\deps\lockfile.py` | `LockFile`(含 `mcp_servers: list[str]`)、`LockedDependency`(含 `get_unique_key`) |
| `D:\Projects\apm-dev\apm\src\apm_cli\models\dependency\reference.py` | `DependencyReference.get_identity()`(忽略 ref/alias 的比對用識別)、`get_unique_key()`、`get_install_path()`、`is_local_path()` |
| `D:\Projects\apm-dev\apm\src\apm_cli\models\dependency\identity.py` | `build_dependency_unique_key`:非預設 host 才加 host 前綴(github.com 省略) |
| `D:\Projects\apm-dev\apm\src\apm_cli\integration\mcp_integrator.py` | `MCPIntegrator.collect_transitive/deduplicate/get_server_names/remove_stale/update_lockfile` |

CLI 步驟(`cli.py:36-289`):

1. `cli.py:57-75` — 依 `-g` 決定 `scope`(`InstallScope.USER`/`PROJECT`),解出 `manifest_path`/`apm_dir`/`deploy_root`;`apm.yml` 不存在就報錯結束。
2. `cli.py:86-113` — 讀 `apm.yml`,同時掃 `dependencies.apm` **與** `devDependencies.apm`(`current_deps = prod_deps + dev_deps`,回歸測試針對 #1549 dev-installed 套件必須可被 uninstall)。
3. `cli.py:117-129` — 提前讀 lockfile(`LockFile.read`),呼叫 `_validate_uninstall_packages`(engine.py:186-299):
   - 接受 `owner/repo` 字串、本地路徑、或 marketplace 記法 `NAME@MARKETPLACE[#REF]`。
   - marketplace 記法先過 `_resolve_marketplace_packages`(engine.py:49-183):lockfile 離線比對優先 → registry fallback(`--dry-run` 時跳過網路呼叫)→ supply-chain guard(registry 解析出的 canonical 若不在 lockfile 中就拒絕,防止被污染的 registry 移除不相關套件,engine.py:143-151)。
   - 比對用 `DependencyReference.get_identity()`(忽略 ref/alias 差異),而非單純字串相等(engine.py:264-281)。
4. `cli.py:130-138` — `--dry-run` 進 `_dry_run_uninstall`(engine.py:302-344):印出將刪除的 apm.yml 條目、`apm_modules/` 路徑是否存在、以及用 `_build_children_index`(engine.py:22-35,依 `resolved_by` 建 parent→children 索引)做 BFS 找出「將被連帶移除」的 transitive 依賴,全程不寫檔。
5. `cli.py:140-162` — 正式從 `apm.yml` 移除:先判斷套件在 `dev_deps` 或 `prod_deps`(`cli.py:142-147`),寫回後,若 `devDependencies.apm` 變空就整個刪掉該 key,若連 `devDependencies` 本身也是本次合成的(從未真的被使用過)就整段刪除(`cli.py:151-156`,避免留下空殼污染沒用過 `--dev` 的專案)。
6. `cli.py:164-167` — 移除前先擷取 `_pre_uninstall_mcp_servers = set(lockfile.mcp_servers)`,供 Step 10 diff 用。
7. `cli.py:170` — `_remove_packages_from_disk`(engine.py:347-386):對每個要移除的套件解析 `get_install_path`,存在就用 `safe_rmtree(path, apm_modules_dir)`(path-traversal 防護)刪除整個套件目錄,並呼叫 `BaseIntegrator.cleanup_empty_parents` 清空剩餘的空母資料夾。
8. `cli.py:172-176` — `_cleanup_transitive_orphans`(engine.py:389-472):對移除套件的 `repo_url` 再跑一次同樣的 BFS(比 dry-run 的版本多一步「排除仍被其他保留依賴需要的」——重新讀取更新後的 `apm.yml`,把仍在 `dependencies.apm` 裡的 key 與 lockfile 中「非孤兒且非被移除」依賴的 key 都視為 `remaining_deps`,`actual_orphans = orphans - remaining_deps`),對真正孤兒的套件目錄同樣 `safe_rmtree` + 清空母資料夾。
9. `cli.py:178-196` — 在改 lockfile **之前**,先把即將被移除套件(含 orphan)在 lockfile 中記錄的 `deployed_files` 蒐集成 `all_deployed_files`(用 `BaseIntegrator.normalize_managed_files` 正規化)——這是 Step 9 target 反向同步唯一的輸入依據。
10. `cli.py:198-224` — 更新 lockfile:把移除套件與 orphan 的 key 從 `lockfile.dependencies` 刪掉;若清空了就直接刪除 lockfile 檔案,否則寫回。
11. `cli.py:225-260` — `_sync_integrations_after_uninstall`(engine.py:475-690),**兩階段**:
    - **Phase 1(移除)**:`clear_discovery_cache()` 避免用到刪除前的 primitive 快照;用 `resolve_targets` 解出目前 targets;把 Step 9 蒐集的 `all_deployed_files` 依 target/primitive 分桶(`partition_managed_files`),對每個支援 `sync_for_target` 的 primitive 呼叫,傳入該桶的 `managed_files` 讓 integrator 只刪自己管的檔案(不會誤刪使用者手動加的檔案)。Skills 有額外處理:掃 `cowork://` URI(OneDrive 型態的動態路徑)、`copilot-app-db://` URI(SQLite 資料庫列,而非檔案系統路徑)。Hooks 用 `sync_integration`(多 target 一次處理)。
    - **Phase 2(重新整合)**:再次 `clear_discovery_cache()`,對 `apm.yml` 剩下的每個依賴,重新 walk 其 primitives 並呼叫對應 integrator 的 `integrate_*` 方法——確保「移除套件同時清掉了其他套件也貢獻的同名/共用資源」的情境下,剩餘套件的檔案會被還原,而不是被 Phase 1 誤刪後就消失。任何單一套件重新整合失敗只 log warning,不中斷整體流程(engine.py:686-688)。
12. `cli.py:261-275` — `_cleanup_stale_mcp`(engine.py:693-724):用 `MCPIntegrator.collect_transitive` 重新蒐集(過渡)+ `apm_package.get_mcp_dependencies()`(根層)算出 `new_mcp_servers`,`stale_servers = old_mcp_servers - new_mcp_servers`,呼叫 `MCPIntegrator.remove_stale`(mcp_integrator.py:538+,對 vscode/copilot/codex/claude 等每個 runtime 各自讀寫設定檔清掉 stale server 條目),最後 `MCPIntegrator.update_lockfile` 把新的 server 名單寫回 `lockfile.mcp_servers`。
13. `cli.py:277-288` — 印摘要(移除幾個套件、`apm_modules/` 移除數),`packages_not_found` 印警告;例外一律 `logger.error` + `sys.exit(1)`。

關鍵資料結構/識別語意:
- `DependencyReference.get_identity()`(reference.py:348-374)= 不含 ref/alias 的 canonical 形式,用於「使用者輸入的移除目標」與「apm.yml 既有條目」比對——同一套件不同 ref/alias 視為同一識別。
- `LockedDependency.get_unique_key()`(lockfile.py:113-123)透過 `build_dependency_unique_key`(identity.py:29-65):**非預設 host(非 github.com)才加 host 前綴**;registry-proxy 依賴永遠用裸 key(避免 proxy host 干擾比對)。
- `LockFile.mcp_servers: list[str]`(lockfile.py:484)是 uninstall 前後 diff 的唯一依據,獨立於 `dependencies` dict 存在。

### B. apm-go 現況(逐一確認「有/無」,含 file:line)

**結論:apm-go 目前完全沒有 `uninstall` 指令,也沒有任何檔案刪除/回收管線。** 以下逐項對照 A 的每個步驟:

| Python 步驟 | apm-go 對應現況 | 證據 |
|---|---|---|
| CLI 指令註冊 | **不存在** | `cmd/apm/main.go:20-28` 只註冊 `validate/normalize/init/install/update/audit/experimental/marketplace/pack`;`Glob cmd/apm/*.go` 確認無 `uninstall.go`。 |
| `-g/--global` user-scope | **完全不存在** InstallScope 概念 | 全 repo 搜尋 `InstallScope`/`--global`/user-scope 僅命中文件與 task 說明,無任何 `.go` 原始碼;`install.go`/`update.go` 一律用相對路徑常數 `"apm.yml"`(install.go:173, update.go:61)、`"apm.lock.yaml"`(install.go:255, update.go:74)、`"apm_modules"`(install.go:124, deploy.go 內建假設)寫死在 cwd。 |
| 讀 `dependencies.apm` + `devDependencies.apm` | **只有 parse,無移除** | `manifest.go:132,137` 已解析出 `m.ParsedDevDeps`,但 `persistPackagesToManifest`(install.go:813-902)只會 `apmSeq.Content = append(...)`(install.go:895-898)寫入 `dependencies.apm`,**沒有任何函式從 YAML node 刪除既有條目**,也沒有處理 `devDependencies.apm`/`devDependencies` 空殼清理。 |
| marketplace ref 解析(`NAME@MARKETPLACE[#REF]`) | **既有 building block 可重用** | `internal/marketplace/ref.go`(`ParseRef`)+ `internal/marketplace/resolve_plugin.go`(`ResolvePlugin`)已被 `install.go:917-948 resolvePositionalPackage` 使用;uninstall 若要支援同語法可直接複用,但目前**沒有**任何「lockfile 離線優先 + registry fallback + supply-chain guard」的比對邏輯(Python engine.py:49-183)。 |
| dry-run 預覽 | **無 uninstall 專用,但已有 CLI 慣例** | `cmd/apm/pack.go:41,56,66` 與 `internal/marketplace/authoring/migrate.go` 已用 `--dry-run bool` flag 慣例(可沿用寫法),但沒有任何函式做 orphan BFS 預覽。 |
| 實體刪除 `apm_modules/<pkg>` | **無任何刪除邏輯** | 全 repo 對 `internal/deploy` 搜尋 `RemoveAll`/`os.Remove` 零命中;`os.RemoveAll` 僅出現在 `update.go:126`(`directGitSemverUpdateScope` 清掉即將重新下載的目錄,屬於「重建前清空」,非「移除依賴」語意)。path-containment 防護 primitive 已存在可重用:`internal/archive/extract.go:204-234`(`Contained`/`ContainedKey`),但沒有等價 Python `safe_rmtree` 的封裝函式。 |
| transitive orphan 偵測 | **資料存在,邏輯未串接** | `lockfile.LockedDep.ResolvedBy`(types.go:14)已記錄 parent unique key,與 Python 的 `resolved_by` 對應資訊完全對等;`internal/resolver/update.go:54-65`(`PlanScopedUpdate`)已有幾乎一模一樣的 fixed-point BFS(`for changed { for _, dep := range lock.Dependencies { if exclude[dep.ResolvedBy] ... } }`)——但這是為了「限定 scoped update 範圍」而寫,**沒有任何呼叫端把它用在「移除依賴後找孤兒」的場景**。 |
| lockfile 依賴項刪除 | **無刪除 API** | `Lockfile` struct(types.go:42-51)與 `FindByKey`(types.go:53-65)只支援查找,沒有 `Delete(key)`/`Remove(key)` 方法;`cmd/apm` 全目錄搜尋找不到任何 `delete(...Dependencies...)` 或等價切片刪除操作。 |
| target 反向同步(移除已部署檔案 + 從剩餘套件重新整合) | **完全不存在(檔案型 primitive)** | `internal/deploy/adapter.go:174-191`(`deployFileToPath`)、`:148-172`(`copyDirRecursive`)、`claude.go:17-30`(`DeployPrimitive`)全部只有「寫入/複製」路徑,**沒有任何函式讀取 lockfile 的 `DeployedFiles` 並刪除對應磁碟檔案**;也沒有 Phase 2「從剩餘套件重新整合」邏輯。lockfile 端已具備必要資料:`LockedDep.DeployedFiles`/`DeployedHashes`(types.go:20-21)在 `deployAndFinalize`(install.go:705-717)已確實填入,是可重用的資料來源,只是目前無人消費它做刪除。 |
| MCP 設定檔 stale entry 清理 | **明確被排除在現有實作範圍外** | `internal/deploy/mcp_common.go:218-224`(`mergeMCPServers` 函式註解原文):"Names outside considered are left completely untouched: they are no longer declared at all, and **stale-server cleanup is explicitly out of scope for this task (design.md §5)**."——即:目前 `WriteMCP` 只會清掉「這次仍被評估但被 refuse/skip」的 server(見 `mcp_writers_test.go:424-456 TestWriteMCP_Redeploy_RefusedServerIsRemoved`,該測試兩次呼叫用的是**同一個** primitive 名稱,只是第二次變成 refused),**若某依賴整個從 `m.ParsedDeps` 移除、其宣告的 MCP primitive 根本不會出現在這次 `prims` 清單裡,則它在 `.mcp.json`/`config.toml` 等設定檔中的既有條目會被完全略過、原封不動留著**。另外 `Lockfile` struct(types.go)**沒有 `mcp_servers: []string` 欄位**,無法比照 Python 做「移除前後 diff」。 |
| Exit code / 錯誤處理慣例 | 已有慣例可沿用 | `cmd/apm/exitcode.go:1-38`(`exitCodeError`/`withExitCode`/`exitCodeOf`)提供非預設 1 的 exit code 掛載機制,uninstall 若需要特殊 exit code(比照 Python `sys.exit(1)`)可直接複用。 |

### C. 具體缺口清單(3-5 項,Python 佐證 + apm-go 佐證)

1. **`uninstall` 指令本身不存在** — Python:`cli.py:23-48`(完整 click command,含 `packages/--dry-run/-v/-g`)。apm-go:`cmd/apm/main.go:20-28` 的 root command 清單中沒有 `uninstallCmd()`;`Glob("cmd/apm/*.go")` 確認無 `uninstall.go` 檔案。

2. **檔案型 primitive(skills/agents/instructions/commands)完全沒有「刪除已部署檔案」的能力,即使日後补上 uninstall 指令並重跑 `deploy.Run`,舊檔案也不會消失** — Python:`engine.py:475-690 _sync_integrations_after_uninstall` 的 Phase 1 明確先移除 `all_deployed_files` 涵蓋的檔案(依 `BaseIntegrator.sync_for_target`/`sync_integration` 拿 `managed_files` 只刪自己管的)。apm-go:`internal/deploy/adapter.go:182-191`(`deployFileToPath`)、`:133-146`(`deploySkillTo`)只有 `os.MkdirAll` + `copyFile`/`copyDirRecursive`,通篇 `internal/deploy` 目錄搜尋 `RemoveAll`/`os.Remove` 零命中——這是比「沒有 uninstall 指令」更底層的缺口:deploy 管線本身沒有「同步刪除」語意,只有「新增覆蓋」。

3. **MCP 設定檔的 stale-entry 清理被程式碼註解明文排除在範圍外** — Python:`cli.py:164-167`(擷取 `_pre_uninstall_mcp_servers`)+ `engine.py:693-724 _cleanup_stale_mcp`(diff 舊/新 server 名單並呼叫 `MCPIntegrator.remove_stale`)。apm-go:`internal/deploy/mcp_common.go:222-224` 註解逐字寫明 "stale-server cleanup is explicitly out of scope for this task (design.md §5)";且 `Lockfile` 型別(`internal/lockfile/types.go:42-51`)完全沒有 `mcp_servers` 欄位可供 diff——連資料結構都尚未預留。

4. **`apm.yml` 沒有「刪除既有 dependencies/devDependencies 條目」的寫回能力** — Python:`cli.py:140-156`(`prod_deps.remove(package)` / `dev_deps.remove(package)` + 清空殼 `devDependencies` wrapper)。apm-go:`install.go:813-902 persistPackagesToManifest` 只有「找不到就 append」邏輯(`install.go:871-898`),沒有對稱的刪除函式;`manifest.Manifest` 也沒有區分「這條 dep 屬於 prod 或 dev」的欄位可供未來刪除邏輯判斷该刪哪個 YAML 區塊。

5. **lockfile 沒有刪除 API,且沒有 user/global scope(`-g`)概念** — Python:`deps/lockfile.py` 的 `LockFile.dependencies` 是 dict,天生支援 `del self.dependencies[key]`(cli.py:208,212 直接用);`core/scope.py:48-165` 的 `InstallScope` 讓同一套邏輯可在 `~/.apm/`(user)與 cwd(project)間切換。apm-go:`internal/lockfile/types.go` 的 `Lockfile.Dependencies` 是 slice + 一份 lazy `index map[string]int`(`FindByKey`,types.go:54-65),**沒有任何刪除方法**;且全 repo 找不到 `InstallScope`/`--global`/user-scope 的任何 Go 原始碼(僅 task 文件提及),`install.go`/`update.go` 一律假設 cwd 就是唯一 scope。

### 可重用 building block(非缺口,供未來實作參考)

- `internal/resolver/update.go:54-65` 的 `ResolvedBy` fixed-point BFS,與 Python `_build_children_index` + orphan walk(engine.py:22-35, 405-417)邏輯等價,理論上可抽出重用於「移除後找孤兒」。
- `internal/archive/extract.go:204-234`(`Contained`/`ContainedKey`)可作為未來 `safe_rmtree` 等價封裝的底層防護,對應 Python `utils/path_security.py` 的 `PathTraversalError`/`safe_rmtree`。
- `LockedDep.DeployedFiles`/`DeployedHashes`(types.go:20-21)已在 `deployAndFinalize`(install.go:705-717)確實填入,是未來「刪除本套件部署過的檔案」的現成資料來源。
- `cmd/apm/pack.go:41,56,66` 的 `--dry-run bool` flag 寫法可直接沿用作為 uninstall `--dry-run` 的 CLI 慣例。
- `cmd/apm/exitcode.go` 的 `withExitCode`/`exitCodeOf` 機制可用於未來 uninstall 特殊 exit code 需求。
- `internal/marketplace/ref.go`(`ParseRef`)+ `resolve_plugin.go`(`ResolvePlugin`)可重用於 marketplace 記法(`NAME@MARKETPLACE[#REF]`)的解析,但目前**沒有**「lockfile 離線優先 + registry fallback + supply-chain guard」的比對邏輯,需要另外實作(對照 engine.py:49-183)。

## Caveats / Not Found

- 本研究只涵蓋 uninstall 反向清理管線本身;`internal/deploy` 各 target adapter(claude/codex/copilot/antigravity/opencode)寫入格式細節、`--target all` 展開規則等,已由既有 spec/task(`07-05-runtime-parity-gaps`、`07-05-opencode-mcp`、`07-05-antigravity-research`)分別涵蓋,本檔不重複展開。
- 未追蹤 Python `BaseIntegrator.partition_managed_files`/`normalize_managed_files`/`cleanup_empty_parents` 的逐行實作細節(僅確認其在 `_sync_integrations_after_uninstall` 中的角色);若後續要動手實作 apm-go 對應版本,建議另開一份 research 深入這幾個函式的行為邊界(尤其 `cowork://`/`copilot-app-db://` 這類非檔案系統路徑的特殊處理,engine.py:566-634)。
- `MCPIntegrator.remove_stale`(mcp_integrator.py:538+)涵蓋 vscode/copilot/codex/claude 等多個 runtime 各自的設定檔讀寫細節,本檔只讀到 vscode/copilot/codex 開頭(mcp_integrator.py:614-638 附近),未逐一讀完全部 runtime 分支(claude 之後的 antigravity/opencode 等分支未展開)——若要重建 apm-go 對應邏輯需要再補讀。
- Task 目前的 `prd.md`(`.trellis/tasks/07-05-uninstall/prd.md`)本身仍是樣板(`Goal: TBD`),尚未真正定義本 task 的驗收範圍與 MVP 邊界(例如 `-g/--global` 是否納入本輪、orphan 清理與 target 反向同步的優先順序);此份 research 僅回答「兩邊現況對照」,規劃/取捨仍待後續 brainstorm 或 design 階段決定。
