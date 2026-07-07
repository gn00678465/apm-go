# Design: apm uninstall

輸入契約:`uninstall-checklist.md`(權威驗收) + `research/uninstall-parity.md`(Python
13 步 + apm-go 落點)。本輪 scope:核心全做 + MCP stale 清理(定案 B);**`-g/--global`
不做**(定案 A,報未支援)。

## 架構總覽:反向清理管線(對照 Python cli.py 13 步)

新增 `cmd/apm/uninstall.go`(CLI orchestration)呼叫下列既有/新增 building block:

```
uninstall(packages, --dry-run, -v):
  1. 讀 apm.yml(prod+dev deps);不存在→err            [既有 manifest 解析]
  2. 讀 lockfile;解析每個 package 目標識別            [新:目標比對]
  3. --dry-run → 印計畫(步驟 1-3 記憶體),return       [新:dryRunPlan]
  4. 蒐集移除集的 deployed_files(改 lockfile 前)       [既有 LockedDep.DeployedFiles]
  5. apm.yml 移除條目 + dev 空殼清理                    [新:manifest 刪除]
  6. 記錄 old_mcp_servers                              [新:lockfile.MCPServers 欄位]
  7. apm_modules 實體刪除 + 清空母資料夾                [新:safeRemoveTree]
  8. transitive orphan BFS + 排除 remaining            [重用 ResolvedBy BFS]
  9. orphan 的 deployed_files 併入移除集
 10. target 反向同步 Phase1(刪 deployed_files)         [新:deploy 刪檔能力]
     + Phase2(剩餘依賴重新整合)                        [重用 deploy.Run]
 11. MCP stale 清理(diff + 各 target 反向移除)          [新:MCP reverse-remove]
 12. lockfile 刪 key;清空→刪檔                          [新:lockfile Delete]
 13. 印摘要 + not-found 警告;例外→exit 1                [既有 exitcode.go]
```

## 新增能力(逐項落點)

### N1. deploy 刪檔能力(最大工作量,un-050~053)⚠️安全紅線
- 新檔 `internal/deploy/uninstall.go`:`RemoveDeployedFiles(projectDir string, files []string, hashes map[string]string) (removed []string, kept []string, diags []string)`。
- 對每個 `deployed_files` 相對路徑:
  - path-containment 檢查(重用 `archive/extract.go` 的 `Contained`,確認在 projectDir 內)——逃逸則拒絕。
  - **hash 保護(un-053)**:讀現檔內容算 hash,與 `hashes[path]` 比對;不符(使用者改過)→ **保留 + 警告**,不刪。
  - 相符 → 刪檔;刪後清空殘留空母資料夾(重用/新增 `cleanupEmptyParents`)。
- 不動既有 additive DeployPrimitive;這是新增的反向路徑。

### N2. Phase 2 重新整合(un-054)
- 直接**重用 `deploy.Run`**:移除 apm.yml 條目後,對剩餘 resolved 依賴重跑既有部署流程,把「被 Phase1 誤刪的共用資源」還原。單一套件失敗只 warn(對照 engine.py:686-688)。
- 順序關鍵:Phase1 刪 → apm.yml/resolve 剩餘 → Phase2 `deploy.Run` 還原。

### N3. lockfile 改動(un-060, un-070~072)
- `internal/lockfile/types.go`:
  - `Lockfile` 加 `MCPServers []string`(對照 Python `LockFile.mcp_servers`);parse/write/序列化五處清單同步(比照先前 provenance 欄位的五處慣例)。
  - 新增 `Delete(key string)` / `RemoveKeys(keys []string)`:從 `Dependencies` slice 移除 + 重建 index。
- install 端須開始**寫入** `MCPServers`(deployAndFinalize 時記錄本次部署的 MCP server 名單),否則 uninstall 無 old 名單可 diff——此為前置(見風險)。

### N4. apm.yml 移除(un-020~022)
- `internal/manifest` 或 install.go 對稱新增 `removePackagesFromManifest(node, identities, isDev)`:
  - 從 `dependencies.apm` / `devDependencies.apm` 的 YAML sequence 刪除比對命中的條目(用 get_identity 等價:忽略 ref/alias)。
  - `devDependencies.apm` 空→刪 key;`devDependencies` wrapper 若本次合成且從未真用→整段刪(對照 cli.py:151-156)。
  - 保留其他條目與手動排版(舊坑 1:用 yamlcore node 級操作,非重寫整檔)。

### N5. transitive orphan(un-040~043)
- 重用/抽出 `internal/resolver/update.go:54-65` 的 `ResolvedBy` fixed-point BFS:
  - 由移除集出發,沿 `ResolvedBy` 找 children;
  - `remaining = 剩餘 apm.yml keys ∪ lockfile 中非孤兒非移除的 key`;`actualOrphans = orphans - remaining`。
- orphan 一併走 N1 刪 apm_modules + deployed_files。

### N6. 目標解析與比對(un-010~019)
- owner/repo·URL·SSH·FQDN → 既有 `manifest`/`install.go` 的正規化;比對用**忽略 ref/alias 的 identity**(需確認 apm-go 既有 canonical key 是否已忽略 ref;`lockfile` unique key 對照 identity.py)。
- marketplace 記法 `name@marketplace`:重用 `marketplace.ParseRef`;**新寫**「lockfile 離線優先 → registry fallback(--dry-run 跳過) → supply-chain guard(registry canonical 不在 lockfile 則拒絕)」(對照 engine.py:49-183);`#ref` 忽略(un-016)。
- **standalone MCP(un-019,apm-go 增強)**:apm 套件識別找不到時,再比對 `dependencies.mcp` 的 server `name`;命中則歸類為 MCP-removal 目標(走 N7 standalone 分支)。解析順序:apm 套件優先 → mcp server 名稱。
- not-found → 警告續行;全部 not-found → 不變更退出(un-013)。

### N7. MCP 清理(un-060~065,定案 B)——兩層共用「per-target 移除單一 server」底層
- **共用底層**:各 MCP target(claude `.mcp.json`/codex `.codex/config.toml`/copilot/antigravity `.agents/mcp_config.json`/opencode `opencode.json`)新增「讀既有設定 → 刪指定 server key → 寫回」路徑(擴充各自 merge/write helper)。
- **設計不變式(review MI5,勿合併)**:上層有兩條**刻意分離**的路徑,共用上述底層但語意不同,後續 refactor **不可**把它們合併:
  - `removeUninstallStandaloneMCP`(un-064/065):使用者 `uninstall <mcp-name>` 明確命中 `dependencies.mcp` 的 standalone server → 直接移除該 server(apm.yml 條目 + 各 target + `lockfile.mcp_servers`)。
  - `computeUninstallStaleMCP`(un-061):移除**套件**的副作用——重算 old−new 的 transitive stale MCP,且必須 depth-aware(存活直接依賴貢獻全部、transitive 零;見已修 CRITICAL `2a58aa3`)。
  - 兩者觸發條件、輸入集合、depth 語意皆不同;合併會讓 standalone 直接移除誤帶 stale-diff 的 depth 邏輯(或反之)。review-forge 交叉票 confirm 為準確設計說明,無需改 code。
- **(a) transitive stale(un-060~063)**:`old = lockfile.MCPServers`;`new = 剩餘依賴的 MCP deps 名單`;`stale = old - new`;對 stale 走共用底層移除;`lockfile.MCPServers = new`。
- **(b) standalone 直接移除(un-064~065,apm-go 增強)**:N6 命中 `dependencies.mcp` 的 server 名稱時,從 apm.yml `dependencies.mcp` sequence 刪該條目(對稱 `upsertMCPEntry`,yamlcore node 級保留排版),走共用底層從各 target 移除該 server,並從 `lockfile.MCPServers` 移除。
- 前置:lockfile 舊版無 `mcp_servers` 欄位時 fail-open(當空集,不誤刪)。

### N8. dry-run(un-080~081)
- `dryRunPlan`:步驟 1-3 記憶體版,印將刪 apm.yml 條目 / apm_modules 路徑存否 / orphan BFS 結果;marketplace ref 跳過 registry fallback(un-081)。

### N9. `-g/--global` 不做(un-090/091,定案 A)
- `-g` 旗標存在但回明確錯誤「user scope (-g) 尚未支援;請在專案目錄操作」,或不註冊該旗標。design 選:**註冊旗標但報未支援**(讓使用者得到清楚訊息而非 unknown flag),A/B 標 deviation。

## 邊界與相依

- 對既有檔的修改:`lockfile/types.go`+`parse.go`+`write.go`(MCPServers + Delete)、`install.go`(寫 MCPServers)、`main.go`(註冊 uninstallCmd)。其餘皆新檔。
- **install 寫 MCPServers 是前置**:沒有它 uninstall 無 old 名單。可先補這一小步並確認既有 install 測試不破。

## 風險與舊坑對映

- **誤刪使用者檔案(最高風險)**:N1 的 hash 保護 + path-containment 是紅線;測試矩陣必含「使用者手寫檔 + 手改過的已部署檔」(舊坑 1、un-V02/V03)。
- **Phase1/Phase2 順序**:共用資源必須靠 Phase2 還原,不可只做 Phase1(un-V05)。
- **MCP diff 依賴 install 先寫 MCPServers**:若 lockfile 舊版無此欄位,fail-open(當作空集,不誤刪)。
- **path 防護全庫一致**:所有刪檔走同一 `safeRemoveTree`,不散落(舊坑 2)。

## Rollback

大量新檔 + 少量既有檔擴充;每個 checklist 分節獨立 commit,`git revert` 可逐步回退。
install 寫 MCPServers 若出問題可單獨 revert(uninstall 對缺欄位 fail-open)。
