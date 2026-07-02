# Design: 修正 --skill 子集部署的全域過濾範圍錯誤

## 邊界與現況

- `cmd/apm/install.go:70` `runInstall(deps, frozen, noProvenance, targetFlag, skillSubset []string, packages []string)`——`skillSubset` 來自 `--skill`(repeatable flag),`packages` 是 positional CLI args(如 `acme/foo`)。
- `runInstall` 1b 步驟把 `packages` 解析成 `*manifest.DependencyReference` 並 append 進 `m.ParsedDeps`,但**沒有保留「這次呼叫解析出的 dep key」**——之後 `buildLockfile`/`deployAndFinalize` 只看得到 `skillSubset`(名稱清單)與 `packages`(原始字串,给 `persistPackagesToManifest` 寫 `apm.yml` 用),兩者都不含「這個名稱清單該套用在哪個 dep key 上」的資訊。
- `internal/deploy/deploy.go:35` `Run(targets, projectDir, m, resolved, skillSubset ...[]string)`——用 variadic-of-slice 技巧模擬「optional []string」,只有一個生產呼叫點(`install.go:479`),其餘 ~11 個測試呼叫點都省略此參數。內部過濾(`deploy.go:90-100`)只比對 `p.Name`,未比對 `p.DepKey`。

## 修正方案

引入「dep key 範圍」作為 `--skill` 過濾的第二維度,兩處都需要它:

1. `internal/deploy/deploy.go`:
   - 將既有 unexported `depRefKey(ref *manifest.DependencyReference) string` 重新命名為 exported `DepRefKey`,供 `cmd/apm/install.go` 呼叫(邏輯只有一份,不重複實作)。
   - 新增：
     ```go
     // SkillFilter scopes a --skill name whitelist to the specific dependency
     // key(s) it was requested for. Only primitives whose DepKey is in
     // DepKeys are subject to the Names whitelist; everything else (local
     // primitives, or any other already-declared dependency) passes through
     // untouched, regardless of whether its skill names appear in Names.
     type SkillFilter struct {
         Names   []string
         DepKeys []string
     }
     ```
   - `Run` 簽名從 `skillSubset ...[]string` 改為 `filter *SkillFilter`(不再是 variadic——語意從「可省略的名稱清單」變成「可省略的範圍化過濾器」,用 `nil` 表示「這次呼叫沒有 --skill」)。
   - 過濾邏輯(`deploy.go:90-100`)從單看 `p.Name` 改為 `p.Type == TypeSkills && depKeySet[p.DepKey] && !nameSet[p.Name]`——local primitives 的 `DepKey == ""`,不在 `depKeySet` 內,天然被排除,不需要額外特判。

2. `cmd/apm/install.go`:
   - `runInstall` 1b 步驟:在既有的「解析 `packages` → `ref`」迴圈裡,額外用 `deploy.DepRefKey(ref)` 收集 `requestedKeys map[string]bool`(對**這次呼叫的所有 positional packages**收集,不論是否為新加入 `m.ParsedDeps` 的套件——因為即使套件已存在,`--skill` 這次呼叫仍然是針對它)。
   - `buildLockfile`:把 `packages []string` 參數換成 `requestedKeys map[string]bool`;原本 `len(skillSubset) > 0 && len(packages) > 0` 的整體 gate,改成逐 dep 判斷 `len(skillSubset) > 0 && requestedKeys[dep.Key]`。
   - `deployAndFinalize`:新增 `requestedKeys map[string]bool` 參數(`packages []string` 保留,因為 `persistPackagesToManifest` 仍需要原始 CLI 字串寫 `apm.yml`)。呼叫 `deploy.Run` 前,只在 `len(skillSubset) > 0` 時建構 `*deploy.SkillFilter{Names: skillSubset, DepKeys: keysOf(requestedKeys)}`,否則傳 `nil`。
   - `runUpdate`(`cmd/apm/update.go`)呼叫 `buildLockfile`/`deployAndFinalize` 時原本就傳 `nil, nil`,新增的 `requestedKeys` 參數比照傳 `nil`。

## 為什麼不做別的方案

- **方案 B(在 `Run` 內部從 `m.ParsedDeps` 反推「新增的套件」)**:`Run` 拿到的 `m` 是 1b 步驟 append 之後的最終狀態,無法區分「這次呼叫新增」與「本來就存在、這次呼叫又指定了它」——後者(套件已存在,使用者只是想調整它的 skill 子集)在語意上也應該被過濾範圍涵蓋,用「diff 前後 ParsedDeps」的方式反而會漏掉這個情境。改成「明確傳入這次呼叫的 dep keys」語意更清楚、也更好測試。
- **方案 C(維持 `skillSubset ...[]string` variadic,另外塞一個 magic 值編碼 dep key)**:會讓一個字串陣列同時混用兩種語意(名稱 vs. key),違反「不要為了保留舊簽名而犧牲清晰度」的判斷;既然只有一個生產呼叫點,直接改簽名成本很低。

## 影響範圍(需要同步修改的呼叫點)

- `internal/deploy/deploy.go`:`Run` 簽名 + 內部過濾邏輯 + `depRefKey`→`DepRefKey` 重新命名(含它唯一的內部呼叫點)。
- `internal/deploy/deploy_test.go`、`mcp_writers_test.go`、`mcpcollect_test.go`:所有 `Run(...)` 呼叫點(~11 處)補上第 5 個參數 `nil`(機械式修改,行為不變)。
- `cmd/apm/install.go`:`runInstall`、`buildLockfile`、`deployAndFinalize` 簽名與內部邏輯。
- `cmd/apm/update.go`:`runUpdate` 對 `buildLockfile`/`deployAndFinalize` 的呼叫補上新參數(傳 `nil`)。

## 測試計畫

- `internal/deploy/deploy_test.go` 新增:`SkillFilter` 指定單一 dep key + 名稱子集時,同一個 `ordered` 清單裡「該 dep key 的未選 skill」被過濾,而「local skill」與「另一個 dep key 的 skill」都不受影響(取代先前手動重現後刪除的 scratch 測試,並補上「多依賴同時存在」的情境,原本的 repro 只有 local skill,沒有覆蓋到「其他依賴」這個分支)。
- `cmd/apm/install_test.go` 新增:`runInstall` 對兩個依賴中的其中一個下 `--skill`,驗證 `apm.lock.yaml` 只有目標依賴的條目有 `skill_subset`,另一個依賴的條目沒有。
- 既有 `TestRunInstall_*` 系列(全部傳 `nil, nil`)必須維持通過,確認新參數不影響既有零值路徑。

## Rollback

單一 commit,若發現回歸可直接 `git revert`;無資料遷移或不可逆步驟。
