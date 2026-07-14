# Research: 存活 local root key 空間不一致（uninstallRemainingRootKeys）

- **Query**: 定位 `uninstallRemainingRootKeys` 對存活 local root 仍輸出 `local:` 空間 key
  的確切位置；寫出可重現場景與可重用的測試 seam。
- **Scope**: internal（cmd/apm 原始碼 + 既有測試 + spec/PRD 背景 + git history）
- **Date**: 2026-07-11

## 結論摘要

- Bug 的確切位置是 `cmd/apm/uninstall.go:400-412`（`uninstallRemainingRootKeys`）的第
  406 行：`remaining[k] = true` 應改為 `remaining[uninstallRemovalKey(k)] = true`。
  這是**一行**修法，與 commit `171fd87` 的 `removedModuleKeys` 套路完全同款（該 commit
  已對「被移除」的 identity 做了這個翻譯，唯獨「存活」的 root 漏了）。
- `uninstallRemovalKey`（已存在，`uninstall.go:186-192`）對 local identity
  (`local:<path>`) 轉譯為 `localModulesKey(resolveLocalSourceAbs(path))` ==
  `_local/<base>-<sha8>`；對 git/marketplace identity 原樣返回。這個函式可以**直接複用**，
  不需新寫轉換邏輯。
- 下游三個消費者（`resolver.ActualOrphans`、`reachableFromRemainingRoots`、
  `computeUninstallStaleMCP`）全部預期 `remainingRootKeys` 是 lockfile/apm_modules 的
  module-key 空間（`dep.UniqueKey()` 即 `LockedDep.RepoURL`），但目前存活 local root
  流進去的是 identity 空間的 `local:<path>` 字串 —— 兩者永不相等，造成兩個具體壞後果
  （見下方「逐項發現」#3、#4）。
- **現有測試套件完全沒有覆蓋「local root 存活、其他 root 被移除」的情境** —
  `uninstall_test.go` 現有 local-dep 測試都是移除 local dep 本身（ag-23 修復的方向），
  `uninstall_mcp_test.go` 的兩個 MCP survive/stale 測試都只用 git 依賴當 root。這代表
  PRD 要求的「先寫失敗測試」目前必然是紅燈（bug 未被任何既有測試捕捉），修完後才會轉綠 ——
  符合 TDD 前提。
- `evals/ab_uninstall.py` 目前只有 3 個情境（standalone_mcp / not_found / global_flag），
  未覆蓋 local dep round-trip，因此「ab_uninstall.py 重跑無回歸」這條 AC 對此 bug**不會
  自動抓到迴歸**（它本來就測不到）；是否要新增 A/B 情境是待拍板事項（見下）。

## 逐項發現（file:line 證據）

### 1. Bug 的確切位置

`cmd/apm/uninstall.go:395-412`：

```go
// uninstallRemainingRootKeys computes the identity-key set of every
// dependencies.apm/devDependencies.apm entry that is NOT being removed --
// un-041's remaining_deps, reused as-is by resolver.ActualOrphans. Reuses
// uninstallIdentity (uninstall_resolve.go) so this is always the exact same
// key space removedIdentities was built from.
func uninstallRemainingRootKeys(m *manifest.Manifest, removedIdentities map[string]bool) map[string]bool {
	remaining := map[string]bool{}
	addRemaining := func(deps []*manifest.DependencyReference) {
		for _, d := range deps {
			if k, ok := uninstallIdentity(d); ok && !removedIdentities[k] {
				remaining[k] = true          // <-- BUG: k stays "local:<path>" for local roots
			}
		}
	}
	addRemaining(m.ParsedDeps)
	addRemaining(m.ParsedDevDeps)
	return remaining
}
```

`uninstallIdentity`（`cmd/apm/uninstall_resolve.go:312-324`）對 local dep 回傳
`"local:" + d.LocalPath`（第 316-317 行）——這是**匹配用**的 identity 空間，故意和
`deploy.DepRefKey`／module-key 空間不同（見同函式註解第 305-311 行）。
`uninstallRemainingRootKeys` 直接把這個匹配用 key 塞進 `remaining`，從未經過翻譯。

對照：`removedIdentities`（被移除的 root）在 `prepareUninstallPlan`
（`cmd/apm/uninstall.go:127-132`）已經有一份平行的、正確翻譯過的
`removedModuleKeys`（第 128、131 行：`removedModuleKeys[uninstallRemovalKey(t.IdentityKey)] = true`）。
**只有存活 root 的路徑漏了同款翻譯** —— 這正是 PRD 描述的缺口。

### 2. `uninstallRemovalKey`（既有、可直接複用的翻譯函式）

`cmd/apm/uninstall.go:173-192`：

```go
func uninstallRemovalKey(identity string) string {
	path, isLocal := strings.CutPrefix(identity, "local:")
	if !isLocal {
		return identity
	}
	return localModulesKey(resolveLocalSourceAbs(path))
}
```

- `resolveLocalSourceAbs`：`cmd/apm/install.go:1307-1318`（相對路徑用 cwd 解析為絕對路徑，
  跟 install 端 `normalizeLocalDep` 用同一套邏輯，見同檔第 1290-1299 行
  `ref.RepoURL = localModulesKey(abs)`）。
- `localModulesKey`：`cmd/apm/install.go:1326-1330`（`"_local/" + sanitizePathSegment(base) + "-" + sha256(abs)[:8]`）。
- 這是 commit `171fd87` 引入的函式（見下方 git history），對 git/marketplace identity
  是恆等函式，對 local identity 才做轉換 —— 套用到 `uninstallRemainingRootKeys` 完全
  無副作用（不影響任何非 local root 的既有行為）。

### 3. 壞後果 A：reachability BFS 不保護存活 local root 的傳遞依賴（誤刪風險）

`reachableFromRemainingRoots`（`cmd/apm/uninstall.go:324-349`）：

```go
func reachableFromRemainingRoots(remainingRootKeys map[string]bool, lock *lockfile.Lockfile, projectDir string) map[string]bool {
	reachable := make(map[string]bool, len(remainingRootKeys))
	queue := make([]string, 0, len(remainingRootKeys))
	for k := range remainingRootKeys {
		reachable[k] = true
		queue = append(queue, k)
	}
	...
	for len(queue) > 0 {
		key := queue[0]
		queue = queue[1:]
		deps, _ := deploy.LoadDependencyDeps(key, filepath.Join(projectDir, "apm_modules", key))
		...
```

- 若存活 local root A 的 key 是 `local:./dep-pkg`（bug 現況），BFS 會嘗試讀
  `apm_modules/local:./dep-pkg/apm.yml`（`deploy.LoadDependencyDeps`，
  `internal/deploy/mcpcollect.go:151-163`）—— 這個路徑不存在（真正的檢出目錄是
  `apm_modules/_local/<base>-<sha8>/`），`os.ReadFile` 失敗，函式直接
  `return nil, nil`（`mcpcollect.go:152-155`）。BFS 因此**永遠不會走進 A 的真實依賴樹**，
  A 宣告的傳遞依賴（`dependencies.apm` 裡的其他套件）不會被加進 `reachable`。
- A 自己的真實 module key（`_local/<base>-<sha8>`）也**從未被種進佇列**（因為
  `remainingRootKeys` 裡根本沒有這個字串），所以即使有 diamond dependency
  （A 和被移除的 B 共享同一個傳遈依賴 X，且 lockfile 的 `ResolvedBy` 記錄的 parent
  剛好是 B —— 這正是 commit `171fd87` comment 裡 "CRITICAL #1" 描述的單親欄位限制場景），
  `reachableFromRemainingRoots` 不會把 X 標記為 reachable，`orphans` 就不會被
  `applyUninstallPlan`（`uninstall.go:223-230`）從候選刪除清單中剔除 —— X 會被誤刪。
- 既有回歸測試 `TestReachableFromRemainingRoots_MultiLevelChain`
  （`cmd/apm/uninstall_test.go:862-897`）驗證的是 BFS 本身多層走訪正確；它**用的是純
  git-key 字串**（`"acme/root"` 等），完全沒有測到 local-key 翻譯缺口，因此目前是綠燈，
  無法暴露此 bug。

### 4. 壞後果 B：存活 local root 自己宣告的 MCP 被誤判 stale 並清除

`computeUninstallStaleMCP`（`cmd/apm/uninstall_mcp.go:47-77`）：

```go
for i := range lock.Dependencies {
	dep := &lock.Dependencies[i]
	key := dep.UniqueKey()                              // 真實空間: "_local/<base>-<sha8>"
	if removalKeys[key] || !remainingRootKeys[key] {     // remainingRootKeys 裡是 "local:<path>"，恆為 false
		continue                                          // -> 被當成「只是 depth>1 的傳遞依賴」跳過
	}
	servers, _ := deploy.LoadDependencyMCP(key, filepath.Join("apm_modules", key))
	for _, s := range servers {
		newMCP[s.Name] = true
	}
}
```

- `dep.UniqueKey()`（`internal/lockfile/types.go:35-40`）對存活 local root 回傳
  `_local/<base>-<sha8>`（因為 install 端 `normalizeLocalDep` 把這個字串寫進
  `LockedDep.RepoURL`，見 `cmd/apm/install.go:1298`）。
- `remainingRootKeys[key]` 用真實 key 去查一個裝著 `local:<path>` 的 map，**永遠查不到**
  → `!remainingRootKeys[key]` 為真 → 該 local root 被誤判成「只是深度 >1 的傳遞依賴，
  deploy.Run 從不自動信任其自身宣告的 MCP」（見同檔第 39-46 行的說明性註解）→ A 自己
  `dependencies.mcp` 裡宣告的 server **不會被加進 `newMCP`**。
- `applyUninstallPlan`（`uninstall.go:266-282`）之後拿 `oldMCP - newMCP` 算 `stale`，
  A 的 MCP server 因為不在 `newMCP` 裡而被判定 stale，被
  `deploy.RemoveMCPServersFromTargets` 從所有 target 設定檔（如 `.mcp.json`）反向移除，
  且 `lock.MCPServers` 也不再保留它 —— 即使 A 完全沒被動到。
- 既有回歸測試 `TestRunUninstall_DirectDepRegistryBackedMCPServerSurvives`
  （`cmd/apm/uninstall_mcp_test.go:20-98`）驗證的正是「存活 direct root 自己的 MCP 應存活」
  這個語意，但 fixture 用的 root 是**git dep**（`acme/pkgB`），不是 local dep，所以現行
  綠燈完全沒測到 local-key 翻譯缺口。

### 5. `uninstallRemainingRootKeys` 的呼叫點（修復後全部受益，無需個別調整）

`cmd/apm/uninstall.go`：

| 行號 | 呼叫 | 用途 |
|---|---|---|
| 138 | `remainingRootKeys := uninstallRemainingRootKeys(m, removedIdentities)` | 餵給 `resolver.ActualOrphans`（第 139 行） |
| 148 | `reachable := reachableFromRemainingRoots(remainingRootKeys, lock, ".")` | dry-run/plan 階段的防呆 veto |
| 223 | `remainingRootKeys := uninstallRemainingRootKeys(m, plan.removedIdentities)` | 實際寫入前重算一次（同一份邏輯，`applyUninstallPlan`） |
| 224 | `reachable := reachableFromRemainingRoots(remainingRootKeys, lock, ".")` | 實際刪除前的 defence-in-depth veto |
| 268 | `computeUninstallStaleMCP(m, lock, plan.mcpNames, allRemovalKeys, remainingRootKeys)` | stale-MCP diff |

四個消費點全部預期同一個 module-key 空間，`uninstallRemainingRootKeys` 是唯一產生者 ——
修一處即可讓所有下游一致，符合 PRD Non-Goals「不重構 uninstall 其他 key 處理」。

### 6. `resolver.ActualOrphans` 對此 bug 的影響範圍（非全面失效，但仍有風險）

`internal/resolver/orphans.go:47-77`：`remaining` 集合除了直接使用
`remainingRootKeys` 之外，還有一個 fallback union（第 60-68 行）：對 lockfile 裡「既不是
orphan 候選、也不是被移除」的每個 key 額外加進 `remaining`。這代表**存活 local root 自己**
（因為它不在 orphans 候選、也不在 removedKeys）多半仍會透過這個 fallback 被保護住，不會
被直接刪除。真正受影響的是它的**傳遞依賴**（第 3 節描述的 diamond-dependency 場景）——
`TransitiveOrphans`（`orphans.go:13-32`）只沿著 `ResolvedBy` 單親鏈走，若某傳遞依賴的
`ResolvedBy` 剛好記錄成被移除的 root，而它同時也被存活的 local root 依賴，
`ActualOrphans` 本身不會發現這個 diamond；靠的正是 `reachableFromRemainingRoots`
這個獨立 BFS 來 veto —— 而這個 BFS 正是被本 bug 破壞的那一個（見第 3 節）。

### 7. Commit `171fd87`（同款先例，PRD 指定要對照的修法）

```
commit 171fd87f771e5507a6185dd994ebb50730dd9560
fix(uninstall): local path dep 反向清理 key 對齊 install F1 key
```

- 對 `removedIdentities`（被移除的 root）新增平行的 `removedModuleKeys`
  （`uninstall.go:127-132`），所有觸碰 lockfile/apm_modules 的地方（`allRemovalKeys`、
  `orphans` 種子、`SafeRemoveModuleDir`、`lock.RemoveKeys`、
  `collectUninstallDeployedProvenance`）全部改吃 `removedModuleKeys`；`removedIdentities`
  保留給 apm.yml splice（`writeUninstallManifest`）繼續用 identity 空間。
- `internal/manifest/remove.go`（同 commit）另外修了 apm.yml splice 端：對
  `IdentityKey()` 為空的 local entry 補上合成 `local:<path>` key，讓 splice 找得到。
  這一塊**不受本 bug 影響**（apm.yml splice 從頭到尾都該留在 identity 空間，PRD
  Non-Goals 已明確排除）。
- 本 bug 是同一次修法「應該但沒有」延伸到的第三個集合：`removedIdentities` →
  `removedModuleKeys`（已修）、`removedIdentities`（splice 用，本就該留 identity，不用修）、
  `uninstallRemainingRootKeys` 的輸出（**未修，即本 task**）。

### 8. spec 與 PRD 交叉引用

- `.trellis/spec/backend/install-marketplace-contracts.md:74-75`：F1 節已記錄
  `uninstallRemovalKey` 的修法，並在第 75 行明確留下 follow-up 註記：
  > `uninstallRemainingRootKeys` still emits `local:` keys for SURVIVING local roots,
  > mismatching the reachability BFS / stale-MCP `_local/` space
  （PRD AC 第三條要求把這條註記改為「已修（含 commit）」。）
- `.trellis/tasks/archive/2026-07/07-05-antigravity-research/prd.md:186-189`：
  「新 follow-up（記錄，未修）」段落，文字與 spec 一致，並註明「修法同款一行翻譯」。

## 可重現場景（TDD 用）

**場景**：專案宣告兩個 root —— local dep A（`./dep-pkg`，帶一個傳遞依賴
`acme/transitive-of-a` 和一個自己宣告的 MCP server `srvA`）+ git dep B（`acme/pkgB`，
與 A 無關）。執行 `apm-go uninstall acme/pkgB`。

**預期（修復後）**：
1. A 本身、A 的傳遞依賴 `acme/transitive-of-a`、兩者的 apm_modules 目錄、lockfile entry
   全部存活（不受 `uninstall acme/pkgB` 影響）。
2. A 宣告的 MCP server `srvA` 在 target 設定檔（如 `.mcp.json`）與 `lock.MCPServers`
   中都繼續存在，不被判定為 stale 反向移除。
3. B 本身、B 的 apm_modules、lockfile entry、部署檔案正常被移除（既有行為，不應變化）。

**目前（修復前，紅燈證據鏈）**：
1. `uninstallRemainingRootKeys(m, removedIdentities)` 對 A 產出 `local:./dep-pkg`
   （而非 `_local/<base>-<sha8>`）。
2. `reachableFromRemainingRoots` 讀 `apm_modules/local:./dep-pkg/apm.yml` 失敗（路徑不存在），
   BFS 不會走進 A 的真實模組目錄，`acme/transitive-of-a` 若被 lockfile 記錄為
   `ResolvedBy: "acme/pkgB"`（diamond 場景）就會被誤刪。
3. `computeUninstallStaleMCP` 用 `dep.UniqueKey()`（`_local/<base>-<sha8>`）去查
   `remainingRootKeys[key]`，查不到，A 被誤判為「深度>1 的傳遞依賴」，`srvA` 不進
   `newMCP`，被判定 stale 並反向移除。

## 可重用的測試 seam

- **測試檔案**：`cmd/apm/uninstall_test.go`（已有 local-dep fixture 套路）+
  `cmd/apm/uninstall_mcp_test.go`（已有 direct-root-MCP-survives 套路）—— 建議新測試
  放在其中之一，或新開一個小檔案（如 `uninstall_local_survivor_test.go`），三者皆可，
  屬於待拍板的命名/歸位決定（見下）。
- **可直接複製改寫的既有測試模板**：
  - `TestRunUninstall_DirectDepRegistryBackedMCPServerSurvives`
    （`cmd/apm/uninstall_mcp_test.go:20-98`）——把其中一個 git root 換成 local dep 即可
    重現 MCP 誤判 stale 的那一半；斷言 `.mcp.json` 仍含目標 server、`lock.MCPServers`
    仍含之。
  - `TestRunUninstall_LocalPathDependencyRemovesModulesLockAndDeployedFiles`
    （`cmd/apm/uninstall_test.go:1002-1096`）—— fixture 建置手法（`chdirTemp`、
    `localModulesKey(resolveLocalSourceAbs(...))`、`apm_modules/_local/...` 目錄、
    `writeUninstallLockfileFixture`、`readManifestParsed`）完全可重用，只需要把「移除
    local dep」改成「移除另一個 git dep、local dep 存活」，並額外加一個傳遞依賴
    （`ResolvedBy` 指向被移除的 git dep，模擬 diamond）來驗證 reachability 保護。
  - `TestReachableFromRemainingRoots_MultiLevelChain`
    （`cmd/apm/uninstall_test.go:862-897`）—— 若想寫純 unit-level（不經過完整
    `runUninstall`）的 `uninstallRemainingRootKeys` 回歸測試，這個測試的 lockfile/
    apm_modules fixture 手法可直接套用，只要把其中一個 root 換成 local dep 並斷言
    `uninstallRemainingRootKeys` 回傳 `_local/<base>-<sha8>` 而非 `local:<path>`。
- **共用 helper**（`cmd/apm/uninstall_test.go:16-64`）：`chdirTemp`
  （定義於 `cmd/apm/marketplace_authoring_test.go:16`）、`writeUninstallDeployedFile`、
  `writeUninstallLockfileFixture`、`readManifestParsed` —— 四個都是 package-private
  helper，同 package 內任何新測試檔案都能直接呼叫。
- **關鍵斷言點**：
  - Unit 層：直接呼叫 `uninstallRemainingRootKeys(m, removedIdentities)`，斷言 local root
    的 key 是 `localModulesKey(resolveLocalSourceAbs("./dep-pkg"))` 而非
    `"local:./dep-pkg"`（最小、最快的紅燈）。
  - E2E 層：透過 `runUninstall([]string{"acme/pkgB"}, uninstallOptions{})` 整條路徑，
    斷言 `apm_modules/_local/.../` 存活、`apm_modules/acme/transitive-of-a` 存活、
    `.mcp.json` 仍含 `srvA`、`apm.lock.yaml` 仍鎖著兩者。

## 修復方案（已知，供實作階段直接採用）

`cmd/apm/uninstall.go:406`：

```diff
 		for _, d := range deps {
 			if k, ok := uninstallIdentity(d); ok && !removedIdentities[k] {
-				remaining[k] = true
+				remaining[uninstallRemovalKey(k)] = true
 			}
 		}
```

- 對 git/marketplace root：`uninstallRemovalKey` 是恆等函式，行為完全不變。
- 對 local root：輸出從 `local:<path>` 變成 `_local/<base>-<sha8>`，與
  `dep.UniqueKey()` / `LoadDependencyDeps`/`LoadDependencyMCP` 的 `depKey` 對齊。
- 不需要動 `removedIdentities` 參數本身（第 404 行的 `!removedIdentities[k]` 比較仍在
  identity 空間，兩邊都是 identity 空間，比較正確 —— 只有「存進 `remaining` 的值」需要
  翻譯，這正是 commit `171fd87` 對 `removedModuleKeys` 的同款處理）。

## 風險

1. **範圍是否要擴大到 `resolver.ActualOrphans` 的 fallback union**（第 6 節）——
   目前判斷 local root 本身多半被 fallback 保護住，真正受害的是它的傳遞依賴，且
   `reachableFromRemainingRoots` 修完就足以堵住這個洞；但若之後有更複雜的 orphan
   語意變化，建議在實作測試時一併對 `resolver.ActualOrphans` 加一個 local-root 版的
   直接單元測試，避免只靠 E2E 覆蓋。
2. **`ab_uninstall.py` 目前測不到這個 bug**（無 local-dep 情境），修復後若不補 A/B
   情境，未來若 Go 端再度回歸，只能靠 Go 單元測試守住，A/B 對這條路徑沒有防護力。
3. **devDependencies 裡的 local root**：`uninstallRemainingRootKeys` 對
   `m.ParsedDevDeps` 用同一個 `addRemaining` closure（`uninstall.go:410`），所以修復
   天然涵蓋 dev 依賴的 local root；但目前找到的既有測試都只覆蓋
   `dependencies.apm`，若要求嚴謹，重現測試應該至少有一個 case 用
   `devDependencies.apm` 底下的 local root 驗證同一條路徑。

## 需拍板事項（附選項與建議）

1. **重現測試放哪個檔案？**
   - 選項 A：加進 `cmd/apm/uninstall_mcp_test.go`（貼近 MCP 斷言，跟現有兩個
     survive/stale 測試放一起，方便對照）。
   - 選項 B：加進 `cmd/apm/uninstall_test.go`（貼近既有 local-dep fixture 與
     `reachableFromRemainingRoots` 測試）。
   - 選項 C：新開 `cmd/apm/uninstall_local_survivor_test.go`（單一場景測試橫跨
     reachability + MCP 兩個斷言，檔案自成一組）。
   - **建議**：C。理由：這個場景同時斷言 reachability 保護與 MCP 非-stale
     兩件事，放單一新檔案比塞進既有兩個檔案的其中一個更清楚對應本 task 的範圍，且
     `.trellis/tasks/07-11-local-root-key-space` 是獨立 child task，日後要單獨
     git blame/追蹤這次修復時更容易定位。
2. **是否要另外寫一個純 unit-level 的 `uninstallRemainingRootKeys` 直接呼叫測試（不經
   `runUninstall`）？**
   - **建議**：要。理由：E2E 測試（透過 `runUninstall`）斷言的是最終可觀察行為，能證明
     bug 修好了，但不易一眼看出「key 空間」這個根因；額外加一個直接呼叫
     `uninstallRemainingRootKeys` 斷言回傳值等於 `localModulesKey(resolveLocalSourceAbs(...))`
     的最小單元測試（仿造 `TestPrepareUninstallPlan_LocalDepRemovalKeysUseModulesKey`
     的風格）能最快定位未來的回歸，且執行成本極低。
3. **是否要在 `evals/ab_uninstall.py` 補一個 local-dep round-trip 情境？**
   - 選項 A：補。local dep 不需要真實網路（copy-materialize），A/B 兩側都能無網路建置
     local path 依賴模擬 A/B round-trip，可行。
   - 選項 B：不補，維持 PRD 現況「ab_uninstall.py 重跑無回歸」（因為它本來就沒測到這裡，
     重跑只是確認既有 3 個情境沒被順手破壞）。
   - **建議**：B（維持 PRD 範圍，不擴大 A/B 覆蓋）。理由：PRD Non-Goals 明確「不重構
     uninstall 其他 key 處理」，且 Python oracle 對 local path dep 的 uninstall 行為
     本身也未必有等價語意可對照（Python 端從未出現這個 key-space 問題，因為它不做
     `_local/<base>-<sha8>` 這種合成 key），A/B 對照的意義有限；Go 單元測試已經是
     這條路徑的權威 oracle。若主 agent 認為值得做，可另開一個更小的 follow-up 記錄。
4. **`reachableFromRemainingRoots` 是否需要額外的直接單元測試覆蓋「local root 存活時
   BFS 走進真實模組目錄」這個子場景？**
   - **建議**：需要，比照第 2 點 unit-level 建議，仿造
     `TestReachableFromRemainingRoots_MultiLevelChain`（`uninstall_test.go:862-897`）
     寫一個 local-root 版本，直接呼叫 `reachableFromRemainingRoots` 並傳入修復後的
     `_local/<base>-<sha8>` key，斷言傳遞依賴被納入 reachable 集合。這比整條 E2E
     路徑更精準地鎖定 BFS 本身的行為。
