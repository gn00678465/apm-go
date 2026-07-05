# apm uninstall 驗收清單

> **用途**:比照 `.trellis/tasks/archive/2026-07/07-03-marketplace-ecosystem/marketplace-checklist.md`
> 的格式,把「移植 Python 原版 `apm uninstall`」排成逐項可勾選、**完整呈現此命令全部功能**
> 的驗收清單。權威來源是 **Python 原版原始碼行為 + CLI 文件 + live CLI 實測**,逐項附
> 檔案路徑/函式名/行號。
>
> **權威來源**:
> - 原始碼:`D:\Projects\apm-dev\apm\src\apm_cli\commands\uninstall\cli.py`(13 步 orchestration)、
>   `.../uninstall/engine.py`(純邏輯 helper)、`.../core/scope.py`(InstallScope)、
>   `.../deps/lockfile.py`(LockFile.mcp_servers)、`.../models/dependency/{reference,identity}.py`
>   (get_identity/unique_key)、`.../integration/mcp_integrator.py`(remove_stale)
> - 指令文件:`D:\Projects\apm-dev\apm\docs\src\content\docs\reference\cli\uninstall.md`
> - 即時驗證:`uv run apm uninstall --help`(2026-07-05 實測,見 U0)
> - 對照研究:`.trellis/tasks/07-05-uninstall/research/uninstall-parity.md`(13 步逐行引用 + apm-go 缺口)
> - **衝突時以 live CLI 實測為準**(比照 marketplace 清單的準則)。
>
> **權威標記**:`源碼` = Python 原始碼;`文件` = uninstall.md;`實測` = live CLI;
> 多者以 `+` 併記。

## 範圍與決定

| 項目 | 決定 |
|---|---|
| 核心(必做,不可縮) | CLI 介面、目標解析與比對、apm.yml 移除、apm_modules 實體刪除、transitive orphan 清理、**target 反向同步(依 lockfile `deployed_files` 只刪自己部署的檔案 + 兩階段從剩餘套件重新整合)**、lockfile 更新、`--dry-run`、輸出/exit code |
| 前置缺口(核心的必要基礎) | apm-go 的 `internal/deploy` **目前完全沒有刪檔能力**(只有 additive copy);lockfile 已有 `DeployedFiles`/`DeployedHashes` provenance(install.go:705-717)可精準反向清理。核心的反向同步**建立在新增「依 provenance 刪檔」能力之上**,此為本 task 最大工作量 |
| **待使用者定案 A:`-g/--global`** | Python 有(user scope `~/.apm/`)。apm-go **完全無 InstallScope/user-scope 概念**(install/update 都寫死 cwd),自成一個子系統大工程。**建議本輪不做**,`-g` 旗標不出現或明確報「未支援」,標為 documented deviation;另開 task |
| **待使用者定案 B:MCP stale 清理** | Python 有(`_cleanup_stale_mcp` + `MCPIntegrator.remove_stale`,對各 runtime 設定檔清 stale server)。apm-go lockfile **無 `mcp_servers` 欄位**、`mcp_common.go:222-224` 註解明文把 stale 清理排除。要做需先給 lockfile 加 `mcp_servers` provenance + 各 target 反向移除 MCP entry。**建議納入**(否則 uninstall 會留下孤兒 MCP 設定,是真實正確性缺口),但屬額外工作量,需確認 |
| marketplace 記法 uninstall(`name@marketplace`) | Python 接受(lockfile 離線優先 → registry fallback → supply-chain guard)。`#ref` 片段被忽略。**建議納入**(已有 `ParseRef`/`ResolvePlugin` 可重用),但「lockfile 離線優先 + supply-chain guard」比對邏輯需新寫 |
| `apm prune` | 文件 Related 提到的姊妹指令(不指名移除孤兒)。**本輪不做**,獨立指令 |
| 明確不移植 | 無(uninstall.md 未發現原版文件錯誤或真實 bug 需排除);若實作中發現,於對應條目標註 |

### 本任務要防的「舊坑」(沿用專案先前教訓)

1. **不能只用全新產生的 fixture** —— 反向同步/apm.yml 移除的測試矩陣必須含「已存在、手動排版過、含其他無關依賴與使用者手寫檔案」的 fixture,證明**只刪自己部署的、不誤刪使用者手動檔案**(這是 uninstall 最高風險)。
2. **同一類 bug 全庫掃描** —— 刪檔的 path-containment 防護(safe_rmtree 等價)若要調整,須 grep 全庫所有刪檔呼叫點一起改,不可只修一處。
3. **讀源碼不夠,對照 live CLI** —— 每條 CLI 面/輸出/exit code 條目以 `uv run apm uninstall` 實測為準;源碼與清單衝突時信清單標註的實測。
4. **「不在範圍」要標權威依據** —— 範圍表每個「不做/deviation」都已標依據(缺 infra/獨立指令),實作時不可自行擴大排除。
5. **安全刪除是不可妥協的紅線** —— 任何刪檔路徑,寧可保留+警告也不可誤刪:hash 與 lockfile provenance 不符者一律保留。

---

## U0 — CLI 介面與參數(源碼+文件+實測)

| ✓ | id | 權威 | 驗證內容 | 對照 |
|---|----|----|----------|------|
| [ ] | `un-001` | 實測+文件 | 指令存在:`apm-go uninstall [OPTIONS] PACKAGES...`,short help「Remove APM packages, their integrated files, and apm.yml entries」 | `uninstall --help`;`cli.py:23-48` |
| [ ] | `un-002` | 實測 | `PACKAGES...` 為 variadic 必填(≥1);零套件 → 用法錯誤(非零 exit) | `uninstall --help` |
| [ ] | `un-003` | 實測 | 旗標集**恰為**:`--dry-run`、`-v/--verbose`、`-g/--global`、`--help`;無其他旗標 | `uninstall --help` |
| [ ] | `un-004` | 源碼 | root command 於 `main.go` 註冊 `uninstallCmd()`(目前缺,`main.go:20-28`) | `cmd/apm/main.go` |

## U1 — 目標解析與比對(源碼+文件)

| ✓ | id | 權威 | 驗證內容 | 對照 |
|---|----|----|----------|------|
| [ ] | `un-010` | 文件+源碼 | PACKAGE 接受:`owner/repo` 簡寫、HTTPS URL、SSH URL、FQDN、marketplace 記法 `name@marketplace`;各自解析成 apm.yml 內的 canonical 識別 | `uninstall.md:26`;`engine.py:186-299` |
| [ ] | `un-011` | 源碼 | 比對用 `get_identity()`(**忽略 ref/alias**),非字串相等——同套件不同 ref/alias 視為同一目標 | `reference.py:348-374`;`engine.py:264-281` |
| [ ] | `un-012` | 源碼 | 掃描來源同時含 `dependencies.apm` **與** `devDependencies.apm`(dev 裝的套件必須可被 uninstall,#1549 回歸) | `cli.py:86-113` |
| [ ] | `un-013` | 文件 | 命令列上某名稱在 apm.yml **找不到** → 警告並繼續處理其餘;**全部**都找不到 → 不做任何變更即退出 | `uninstall.md:87`;`cli.py:277-288` |
| [ ] | `un-014` | 源碼+文件 | marketplace 記法解析:lockfile 離線比對優先 → registry fallback(`--dry-run` 時跳過網路) | `engine.py:49-183`;`uninstall.md:62-66` |
| [ ] | `un-015` | 源碼+文件 | **supply-chain guard**:registry 解出的 canonical 若不在 `apm.lock.yaml` 中 → 拒絕移除並警告(具名 canonical),防止被污染 registry 誘導移除無關套件 | `engine.py:143-151`;`uninstall.md:91-93` |
| [ ] | `un-016` | 文件 | marketplace 記法的 `#ref` 片段**被忽略**(uninstall 只用 canonical name 識別) | `uninstall.md:95-97` |
| [ ] | `un-017` | 文件 | marketplace ref 完全無法解析(lockfile 與 registry 皆無) → log error + 跳過該套件(不中斷其餘) | `uninstall.md:89` |
| [ ] | `un-018` | 文件 | **no-lockfile 行為**:無 `apm.lock.yaml` 時 marketplace 記法無離線錨點,supply-chain guard 無法交叉檢查;仍嘗試 registry 解析,canonical 命中 apm.yml 才續行(完整性較弱) | `uninstall.md:99-101` |

## U2 — apm.yml 移除(源碼)

| ✓ | id | 權威 | 驗證內容 | 對照 |
|---|----|----|----------|------|
| [ ] | `un-020` | 源碼+文件 | 從 `dependencies.apm` 或 `devDependencies.apm` 移除對應條目(先判斷屬 prod 或 dev) | `cli.py:140-147`;`uninstall.md:78` |
| [ ] | `un-021` | 源碼 | `devDependencies.apm` 移除後變空 → 刪掉該 key;若 `devDependencies` wrapper 本身是本次合成、從未真用過 → 整段刪除(不留空殼) | `cli.py:151-156` |
| [ ] | `un-022` | 源碼 | 寫回 apm.yml 保留其他無關依賴與使用者手動排版/內容(舊坑 1:含既有內容的 fixture) | `cli.py:140-162` |

## U3 — apm_modules 實體刪除(源碼+文件)

| ✓ | id | 權威 | 驗證內容 | 對照 |
|---|----|----|----------|------|
| [ ] | `un-030` | 源碼+文件 | 對每個移除套件解析 `get_install_path`,存在則刪整個套件目錄 `apm_modules/owner/repo/` | `engine.py:347-386`;`uninstall.md:79` |
| [ ] | `un-031` | 源碼 | 刪除用 path-traversal 防護(`safe_rmtree(path, apm_modules_dir)` 等價;apm-go 用 `archive/extract.go` 的 `Contained` 封裝),路徑逃逸 → 拒絕 | `engine.py`;`internal/archive/extract.go:204-234` |
| [ ] | `un-032` | 源碼+文件 | 刪除後清空殘留的空母資料夾(`cleanup_empty_parents`) | `engine.py`;`uninstall.md:85` |

## U4 — transitive orphan 清理(源碼+文件)

| ✓ | id | 權威 | 驗證內容 | 對照 |
|---|----|----|----------|------|
| [ ] | `un-040` | 源碼+文件 | 對移除套件的 `repo_url` 用 `resolved_by` 建 parent→children 索引跑 BFS,找出連帶孤兒(npm 式 pruning,依 `apm.lock.yaml` 計算) | `engine.py:22-35,389-472`;`uninstall.md:80` |
| [ ] | `un-041` | 源碼 | **排除仍被需要的**:重讀更新後 apm.yml,把仍在 `dependencies.apm` 的 key 與 lockfile 中非孤兒非被移除的依賴視為 `remaining_deps`,`actual_orphans = orphans - remaining_deps`——僅真孤兒被刪 | `engine.py:389-472` |
| [ ] | `un-042` | 源碼 | 真孤兒套件目錄同樣 `safe_rmtree` + 清空母資料夾 | `engine.py:389-472` |
| [ ] | `un-043` | 源碼 | apm-go 可重用 `internal/resolver/update.go:54-65` 的 `ResolvedBy` fixed-point BFS(邏輯等價,需接上「移除後找孤兒」呼叫端) | `resolver/update.go` |

## U5 — target 反向同步(核心,源碼+文件)⚠️最高風險

| ✓ | id | 權威 | 驗證內容 | 對照 |
|---|----|----|----------|------|
| [ ] | `un-050` | 源碼 | 改 lockfile **之前**先蒐集移除套件(含 orphan)在 lockfile 記錄的 `deployed_files`,正規化成 `all_deployed_files`——這是反向同步唯一輸入 | `cli.py:178-196` |
| [ ] | `un-051` | 源碼+文件 | **Phase 1(移除)**:把 `all_deployed_files` 依 target/primitive 分桶,對每個 primitive 傳入該桶 `managed_files`,**只刪 lockfile `deployed_files` 追蹤的檔案**;使用者手寫的同資料夾內容不動 | `engine.py:475-690`;`uninstall.md:20,81` |
| [ ] | `un-052` | 源碼 | apm-go 前置:`internal/deploy` 新增「依 `LockedDep.DeployedFiles`/`DeployedHashes` 刪檔」能力(目前只有 additive copy,零刪除路徑)——本 task 最大工作量 | `research §B`;`install.go:705-717` |
| [ ] | `un-053` | 源碼 | 刪檔前 hash 比對:檔案內容與 lockfile `DeployedHashes` 不符(使用者改過) → **保留 + 警告**,不刪(舊坑 5 紅線) | `DeployedHashes`;安全性要求 |
| [ ] | `un-054` | 源碼+文件 | **Phase 2(重新整合)**:對 apm.yml 剩餘每個依賴重新 walk primitives 並重新整合——修復「移除套件同時清掉了其他套件也貢獻的共用資源」情境;單一套件重整失敗只 warn 不中斷 | `engine.py:475-690`;`uninstall.md`(隱含) |
| [ ] | `un-055` | 源碼+文件 | Hooks 反向同步:移除套件貢獻的 hook 條目(`.claude/settings.json`、`.cursor/hooks.json`、`.gemini/settings.json`、`.kiro/hooks/` 等) | `engine.py`;`uninstall.md:82` |
| [ ] | `un-056` | 源碼 | claude 的 `.claude/skills` 額外複本(見 opencode-mcp session 前的 skills 修正)也要一併反向清理——deployed_files 應已涵蓋兩處 | `deploy/claude.go`(skills 雙寫) |

## U6 — MCP stale 清理(待定案 B,源碼+文件)

| ✓ | id | 權威 | 驗證內容 | 對照 |
|---|----|----|----------|------|
| [ ] | `un-060` | 源碼 | 移除前擷取 `old_mcp_servers = set(lockfile.mcp_servers)`;apm-go 前置:lockfile 需**新增 `mcp_servers` 欄位**(目前無) | `cli.py:164-167`;`lockfile.py:484` |
| [ ] | `un-061` | 源碼+文件 | 重算 `new_mcp_servers`(剩餘 transitive + root MCP deps),`stale = old - new`,對各 target 設定檔清掉 stale server 條目 | `engine.py:693-724`;`uninstall.md:83` |
| [ ] | `un-062` | 源碼 | 各 MCP target 反向移除單一 server:claude(`.mcp.json`)、codex(`.codex/config.toml`)、copilot、antigravity(`.agents/mcp_config.json`)、opencode(`opencode.json`)各自讀寫清除 | `mcp_integrator.py:538+`;apm-go `mcp_*.go` |
| [ ] | `un-063` | 源碼 | 更新 `lockfile.mcp_servers` 為新名單 | `cli.py:261-275`;`mcp_integrator update_lockfile` |

## U7 — lockfile 更新(源碼+文件)

| ✓ | id | 權威 | 驗證內容 | 對照 |
|---|----|----|----------|------|
| [ ] | `un-070` | 源碼+文件 | 把移除套件與 orphan 的 key 從 `lockfile.dependencies` 刪除;apm-go 前置:`Lockfile` 需新增刪除 API(目前只有 `FindByKey` 查找,無 Delete) | `cli.py:198-224`;`lockfile/types.go` |
| [ ] | `un-071` | 源碼+文件 | 移除後 lockfile 若清空(零依賴) → **直接刪除 `apm.lock.yaml` 檔案**,否則寫回 | `cli.py:198-224`;`uninstall.md:84` |
| [ ] | `un-072` | 源碼 | key 比對用 `get_unique_key()`:非預設 host(非 github.com)才加 host 前綴 | `identity.py:29-65`;`lockfile.py:113-123` |

## U8 — `--dry-run`(源碼+文件)

| ✓ | id | 權威 | 驗證內容 | 對照 |
|---|----|----|----------|------|
| [ ] | `un-080` | 源碼+文件 | `--dry-run` 執行步驟 1-3(記憶體中):印將刪的 apm.yml 條目、`apm_modules/` 路徑是否存在、BFS 算出將連帶移除的 transitive 依賴;**零寫入** | `engine.py:302-344`;`uninstall.md:103` |
| [ ] | `un-081` | 文件 | `--dry-run` **跳過 registry fallback**:不在 lockfile 的 marketplace ref 無法預覽,訊息提示改用 `owner/repo` 或去掉 `--dry-run` | `uninstall.md:32,103` |

## U9 — `-g/--global` user scope(待定案 A,源碼)

| ✓ | id | 權威 | 驗證內容 | 對照 |
|---|----|----|----------|------|
| [ ] | `un-090` | 源碼+文件 | `-g` 對 user scope `~/.apm/` 操作(manifest/apm_dir/modules/deploy_root 全換 user 路徑);apm-go **完全無 InstallScope**,需自成子系統 | `scope.py:48-165`;`cli.py:57-75` |
| [ ] | `un-091` | 決定 | **若本輪不做**(建議):`-g` 旗標不出現或明確報「未支援」,並於 A/B 標為 documented deviation | 範圍表定案 A |

## U10 — 輸出、exit code、摘要(源碼+文件+實測)

| ✓ | id | 權威 | 驗證內容 | 對照 |
|---|----|----|----------|------|
| [ ] | `un-100` | 源碼 | `apm.yml` 不存在 → 報錯結束(非零 exit) | `cli.py:57-75` |
| [ ] | `un-101` | 源碼 | 結尾摘要:移除幾個套件、`apm_modules/` 移除數;`packages_not_found` 印警告 | `cli.py:277-288` |
| [ ] | `un-102` | 源碼 | 例外一律 `logger.error` + exit 1(apm-go 用 `exitcode.go` 的 `withExitCode`) | `cli.py:277-288`;`cmd/apm/exitcode.go` |
| [ ] | `un-103` | 實測 | `-v/--verbose` 印詳細移除資訊(每個刪除的檔案/目錄) | `uninstall --help` |

## Phase V — 驗證完整性(適用全程)

| ✓ | id | 權威 | 驗證內容 | 對照 |
|---|----|----|----------|------|
| [ ] | `un-V01` | — | **install→uninstall 往返**:先 `install` 一個含 skills/agents/commands/instructions/hooks/MCP 的套件到多 target,再 `uninstall`,斷言 deployed_files 全消失、apm.yml/lockfile/apm_modules 乾淨 | — |
| [ ] | `un-V02` | — | **只刪自己的**:兩個套件部署到同 target,uninstall 其一,另一的檔案完好;含使用者手寫檔案的 fixture 證明手寫檔不被刪 | 舊坑 1 |
| [ ] | `un-V03` | — | **hash 保護**:使用者改過的已部署檔案 → uninstall 保留 + 警告 | `un-053` |
| [ ] | `un-V04` | — | **transitive orphan**:A 依賴 B,uninstall A → B 若無他人依賴則刪、有則留 | `un-040/041` |
| [ ] | `un-V05` | — | **共用資源 Phase 2**:兩套件貢獻同名 skill,uninstall 其一後另一的 skill 仍在(Phase 2 重整還原) | `un-054` |
| [ ] | `un-V06` | — | **lockfile 清空刪檔**:移除最後一個依賴 → `apm.lock.yaml` 被刪 | `un-071` |
| [ ] | `un-V07` | — | **A/B 對照** `uv run apm uninstall`:apm.yml/lockfile/apm_modules/各 target 檔案的最終狀態逐項比對;deviation(`-g` 不做、MCP 若延後)明確記錄,非掩蓋 | marketplace A/B 慣例 |
| [ ] | `un-V08` | — | `go build/vet/test ./...` 全綠,新功能覆蓋 ≥ 80%;每個刪檔路徑有 path-containment 負向測試 | — |

## 每個 Phase 完成時的自我聲明範本

> 「已完成 U<n>(un-0xx~un-0yy),親自重跑 build/vet/test 全綠;對照 `uv run apm uninstall`
> 實測 <場景> 一致(deviation:<清單>);舊坑 fixture(含既有內容 + 使用者手寫檔)已納入
> 測試矩陣並證明只刪自己部署的檔案。Completed: M/N。」
