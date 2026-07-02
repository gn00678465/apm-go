# Implement: 修正 --skill 子集部署的全域過濾範圍錯誤

## 執行順序

1. `internal/deploy/deploy.go`
   - [x] `depRefKey` 重新命名為 `DepRefKey`(exported),更新其唯一內部呼叫點。
   - [x] 新增 `SkillFilter` struct(`Names []string`、`DepKeys []string`)。
   - [x] `Run` 簽名:`skillSubset ...[]string` → `filter *SkillFilter`。
   - [x] 過濾邏輯改用 `filter.Names`/`filter.DepKeys` 建的 set,條件加上 `depKeySet[p.DepKey]`。
   - **verify**:`go build ./internal/deploy/...` 會因呼叫點簽名不符而先失敗——預期,下一步修。✅ 符合預期。

2. `internal/deploy/deploy_test.go`、`mcp_writers_test.go`、`mcpcollect_test.go`
   - [x] 所有既有 `Run(a, b, c, d)` 呼叫補上第 5 參數 `nil`。
   - **verify**:`go build ./internal/deploy/...` 通過。✅

3. `cmd/apm/install.go`
   - [x] `runInstall` 1b 步驟:解析 `packages` 的迴圈裡收集 `requestedKeys map[string]bool`(用 `deploy.DepRefKey(ref)`,對每個 positional package,無論是否已存在於 `m.ParsedDeps`)。
   - [x] `buildLockfile`:`packages []string` 參數 → `requestedKeys map[string]bool`;`skill_subset` 寫入條件改成逐 dep 判斷 `requestedKeys[dep.Key]`。
   - [x] `deployAndFinalize`:新增 `requestedKeys map[string]bool` 參數;呼叫 `deploy.Run` 前視 `len(skillSubset) > 0` 建構 `*deploy.SkillFilter{Names: skillSubset, DepKeys: <requestedKeys 攤平成 slice>}` 或傳 `nil`。
   - [x] `runInstall` 內對 `buildLockfile`/`deployAndFinalize` 的呼叫補上 `requestedKeys`。
   - **verify**:`go build ./cmd/apm/...` ✅

4. `cmd/apm/update.go`
   - [x] `runUpdate` 對 `buildLockfile`/`deployAndFinalize` 的呼叫補上 `nil`(對應新的 `requestedKeys` 參數位置)。
   - **verify**:`go build ./...` 全過。✅

5. 新增 regression tests
   - [x] `internal/deploy/deploy_test.go`:`TestRun_SkillFilterScopedToDepKey`(local skill 不受影響、另一個 dep 的 skill 不受影響、目標 dep 內未選 skill 仍被過濾)。
   - [x] `cmd/apm/install_test.go`:`TestBuildLockfile_SkillSubsetScopedToRequestedDep`(兩個依賴其中一個帶 `--skill`,驗證 `apm.lock.yaml` 只有目標依賴的 lock 條目有 `skill_subset`)。
   - **verify**:兩個新測試皆通過。✅

6. 全量驗證
   - [x] `go build ./...` ✅
   - [x] `go vet ./...` ✅
   - [x] `gofmt -l .`(空)✅
   - [x] `go test ./... -cover`(全過;`internal/deploy` 85.2%→86.8%,`cmd/apm` 69.5%→69.3%,無顯著退步)✅

## Review Gate

- [x] 完成後跑一輪 codex exec 唯讀審查(xhigh effort,比照本次會話先前的慣例),確認範圍限定邏輯沒有遺漏呼叫點、沒有引入新的過濾繞過。

### Codex 審查發現與處理

1. **既有 dep 判重只看 bare `RepoURL`,忽略 `VirtualPath`**(`install.go:108-124`,pre-existing 邏輯,非本次引入):若 `acme/foo` 已宣告,使用者執行 `apm install acme/foo/sub/pkg --skill x`,`existing[ref.RepoURL]` 命中導致新的 virtual-path 目標不會被加進 `m.ParsedDeps`,resolver 也就不會解析出它——`requestedKeys["acme/foo/sub/pkg"]` 因此永遠對不上任何 `result.Deps` 項目,`--skill` 靜默失效(不報錯,也不套用)。
2. **`--skill` 不搭配任何 positional package 時靜默無效**(`install.go:484`,本次修正引入的邊界情況):`requestedKeys` 為空,`deployAndFinalize` 仍會建構 `SkillFilter`,但因為沒有任何 primitive 的 `DepKey` 落在空的 `DepKeys` 集合裡,結果等同於「印出 `[i] Skill subset: x` 但實際完全不過濾」。

**處理方式**:不修既有 dep 判重邏輯(範圍更廣、與 skill-subset 無關的獨立問題,留待未來需求明確後再處理),而是在 `buildLockfile` 收尾處加一個統一的「fail loud」守門:若 `skillSubset` 非空但迴圈跑完後從未有任何 `dep.Key` 命中 `requestedKeys`(`skillSubsetApplied` 仍是 `false`),直接回傳明確錯誤,而不是靜默成功。這個守門對兩個發現都成立(不論是「沒給 package」還是「給了 package 但因判重而沒真正解析進圖裡」,從 `buildLockfile` 的視角看都是「requestedKeys 沒有任何項目命中 result.Deps」)。新增 `TestBuildLockfile_SkillSubsetNoMatch_Errors` regression test 驗證。

第二輪 codex exec(xhigh)復查此修正,確認無殘留問題。

## Rollback Point

任何一步發現設計有誤,回到 design.md 對應章節重新評估;程式碼變更前 `git status` 已確認乾淨,單一 commit 可整批 revert。
