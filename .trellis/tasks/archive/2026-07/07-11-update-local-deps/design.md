# Design: apm update × local deps materialization + 零 target 閘門

> **draft — 待 review**
> 依據:`research/findings.md`(2026-07-11 live A/B + 源碼對照)+ 本輪 research agent 對同一問題的二次獨立 live 驗證(證據見本 task 的 session 交付紀錄,未持久化為獨立檔案——harness 政策不允許 research agent 另寫 findings/report 類檔案,細節改在 session 回覆文字中交付)。定案:**修 parity**(Python `apm update` 確實 materialize + deploy local deps),同時堵住 update 破壞 lockfile provenance 的實測缺陷(findings C#2/C#3b)。

## 1. 邊界(Scope)

**改**:
- `cmd/apm/update.go` `runUpdate`:(a) local dep normalization;(b) 零 target 閘門;(c) scoped-update positional token 的 local-path 轉譯。
- `cmd/apm/update_test.go`(+ 必要時 `cmd/apm` 共用 test helper):新增 TDD 測試;修補既有 5 個無 target 的 `TestRunUpdate_*` fixture(見 §6 回歸清單)。
- `.trellis/spec/backend/install-marketplace-contracts.md` §4:Warning(:72)改為修復記錄 + deviations 表補三列。

**不改**(Non-goals,PRD 對齊):
- update 的 git dep 更新語意、`--frozen` 語意、registry 路徑。
- 不新增 update `--target` flag(Python 有、Go 無——既有面差,範圍外)。
- 不引入 Python 的 plan/consent gate(`--dry-run`/`--yes` prompt)——另一 feature。
- 不動 `internal/gitops`、`internal/resolver`、`internal/deploy`(重用既有路徑,零安全面變更)。
- update 要求既有 `apm.lock.yaml`(`update.go:74-76`)維持不變(documented deviation)。

## 2. 契約(Contracts)

### C1 — update local dep materialization(與 install F1 同一條路)

`apm-go update` 對 manifest 內 local dep(相對/絕對路徑、prod+dev)的行為必須與 `apm-go install` 一致:

| 條件 | 結果 |
|---|---|
| local dep + 可解析 target | copy-materialize 到 `apm_modules/_local/<sanitizedBase>-<sha8>/`(fresh copy,stale 先清)→ 部署 primitives → lockfile 條目 key 維持 `_local/...`、`deployed_files`/`deployed_file_hashes` 重算 |
| local dep 源碼內容變更後 update | 部署結果 = 重新 install 的結果(內容刷新;**比 Python 嚴**,Python plan-unchanged 不刷新 — deviation D2) |
| lockfile 既有 `_local/...` 條目 | **不得**被改寫成 `repo_url: <原始路徑>` 裸條目(修掉 findings C#2 的破壞) |

實作機制:`runUpdate` 在 `manifest.ParseManifest` 成功後(update.go:72 之後)插入與 install.go:306-311 相同的迴圈:

```go
for _, dep := range m.ParsedDeps { normalizeLocalDep(dep) }
for _, dep := range m.ParsedDevDeps { normalizeLocalDep(dep) }
```

之後一切走既有機制:`depKey` 變 `_local/...`(resolver.go:369-378)→ `LoadPackage` 見 `LocalSourcePath` → `materializeLocalCopy`(clone.go:30-31)→ deploy → buildLockfile。

### C2 — update 零 target 閘門(同 install §2 語意)

| 條件 | 結果 |
|---|---|
| 解析出 deps(`len(result.Deps) > 0`)+ 零 resolvable target | 印 `printUpdateSummary` plan → 印 targetDiags(stderr)→ `errNoDeployTarget()`(exit 2、教學訊息與 install 逐字相同);**閘門位於任何 lockfile/apm.yml 寫入之前**(零 partial write) |
| 零 deps(空 manifest)update | 行為不變(不觸發閘門) |
| deps + target 可解析 | 行為不變 |

實作機制:`runUpdate` 在 `printUpdateSummary` **之後**、`deployAndFinalize` **之前**(update.go:155-157 之間,即第 155 行呼叫完後、第 157 行 `return deployAndFinalize(...)` 之前)插入:

```go
printUpdateSummary(existingLock, newLock)

targets, targetDiags := deploy.ResolveTargets("", m.Target, ".")
if len(result.Deps) > 0 && len(targets) == 0 {
    for _, d := range targetDiags { fmt.Fprintln(os.Stderr, d) }
    return errNoDeployTarget()
}

return deployAndFinalize(m, "", nil, nil, nil, result, newLock, existingLock, existingNode, node)
```

(targetFlag 恆為 `""`——Go update 無 --target flag。與 install.go:613-618 同款、共用 `errNoDeployTarget()` 防措辭漂移。)

**排序修正(本輪 research agent 二次 live 驗證,2026-07-11)**:初版草稿曾把閘門插在 `printUpdateSummary` 之前。二次驗證以真 Python binary 重跑「deps 存在、零 target、有新 local dep(強制 plan 有變更)」情境,實測 stdout 順序為:

```
[i] Update plan for apm.yml
  [+] ./vendor/localdep3
      ref: - (-, new)
  1 added
[x] No harness detected
...
```

即 Python 先印 update plan,才印教學錯誤訊息。閘門必須放在 `printUpdateSummary` **之後**,才能在輸出順序上與 oracle 一致(功能上——零 partial write——兩種插入順序皆成立,`printUpdateSummary` 本身無副作用;此為 UX 對齊,非正確性必要條件,但可避免不必要的 apm-go/Python 逐字輸出差異)。

### C3 — scoped update 接受 local path token(防回歸)

`apm update ./dep-pkg`(或絕對路徑)在 normalization 後仍必須匹配該 dep(今日可用、Python 可用——findings C#4)。機制:positional `pkg` 在傳入 `PlanScopedUpdate`/`directGitSemverUpdateScope` 前做一次轉譯——若 token 解析為 local path(`manifest.ParseDepString` → `IsLocal`,或 `IsAbsoluteLocalPath`),則 `pkg = localModulesKey(resolveLocalSourceAbs(<path>))`;其他 token 原樣通過。先例:`uninstallRemovalKey`(uninstall.go:179 附近,ag-23)。

### C4 — 安全不變式(不得弱化)

全部重用 install 既有實作,零新寫檔路徑:
- `archive.ContainedKey` guard:materialize dest 經 `installPath`(clone.go:250-254);update 的清目錄迴圈本就有 guard(update.go:122-124)。
- symlink/非 regular file 拒絕:`copyTreeNoSymlinks`(clone.go:287-323)。
- `LocalSourcePath` runtime-only,永不序列化(install.go:1258-1300 契約)。
- lockfile `..` 檢查、絕對 repo_url 政策不動(spec §4)。

## 3. 資料流(修復後)

```
apm.yml ── ParseManifest ──> m.ParsedDeps/ParsedDevDeps
                │
                ▼  (新增) normalizeLocalDep × 全量
        local dep: {Source:git, RepoURL:_local/<base>-<sha8>, LocalSourcePath:<abs>}
                │
                ▼
   PlanFullUpdate/PlanScopedUpdate ──> resolver BFS(depKey=_local/...)
                │                         └─ LoadPackage → materializeLocalCopy(fresh copy)
                ▼
        buildLockfile(key=_local/...,與 install 同 key space)
                │
                ▼  printUpdateSummary(plan 輸出,無副作用)
                ▼  (新增) 零 target 閘門:result.Deps>0 && targets==0 → exit 2(無寫檔)
                ▼
        deployAndFinalize(部署 + deployed_files/hashes 回填 + 寫 lockfile)
```

## 4. 相容性

| 面向 | 影響 |
|---|---|
| 既有 `_local/...` lockfile(install 產) | update 後 key 不變、hashes 重算——**修復**了現行破壞;無 migration 需求 |
| 被舊 update 破壞的 lockfile(`repo_url: ./x, source: local`) | `ParseLockfile` 本就接受(findings C#2 追測);修復後下一次 update/install 以 `_local/...` 重建正確條目(自癒,同 findings C#5;本輪二次驗證以獨立 scratch fixture 重現同一自癒行為) |
| `apm update <git-pkg>` scoped | 不受影響(token 轉譯只動 local path 形狀) |
| CI(auto-frozen) | 不變:frozen 只擋 scoped update(update.go:57-59) |
| A/B 腳本 | `ab_mcp_install_parity.py` 等既有腳本不涉 update+local dep;重跑防回歸即可 |

### Documented deviations(寫入 spec §4 / deviations 表)

| id | apm-go(修復後) | Python | 依據/理由 |
|---|---|---|---|
| D1 | update 要求既有 apm.lock.yaml(exit 1) | 無 lockfile 也可 update(視為全 add) | findings C#1;Go fail-loud,維持 |
| D2 | update 每次 re-copy + 重部署 local dep(冪等) | plan unchanged 時不刷新(deployed 檔可停留舊版,見 findings P2/P2b 漂移) | Go 為超集且自癒漂移;不引入 plan gate |
| D3 | deps+零 target 的 update **一律** exit 2 | plan unchanged 時 plan gate 先 exit 0,僅 plan 前進時 exit 2 `No harness detected` | findings C#3/C#3b(本輪二次驗證以「新增 local dep 強制 plan 有變更」情境再次確認 exit 2 語意與教學訊息逐字相符);Go 與 install §2 矩陣一致、且必須擋以免零 target 寫出無 provenance lockfile |

## 5. Rollback 形狀

- 兩個原子 commit:(1) `fix(update): normalize local deps`(C1+C3+測試);(2) `fix(update): zero-target gate`(C2+測試+spec 更新)。彼此獨立可 revert。
- 純行為修復、無 schema/migration/持久狀態變更;revert 後回到現狀(已知損壞可由 install 自癒)。
- 測試錨點:revert 任一 commit 會讓對應新測試轉紅,不影響既有測試。

## 6. 既有測試回歸清單(本輪二次驗證新增發現)

C2 的零 target 閘門會讓以下**現有** `cmd/apm/update_test.go` 測試從綠翻紅——它們的 apm.yml fixture 都沒有 `target:` 欄位、t.TempDir() 底下也沒有任何 harness 訊號目錄(`.claude/` 等),目前之所以能綠是因為 runUpdate 完全沒有零 target 檢查。實作 C2 時必須同步在這些 fixture 加一行 `target:\n  - claude\n`(不影響它們原本要驗的 git-semver 邏輯):

1. `TestRunUpdate_Full_ReResolvesToNewestAndRewritesLock`
2. `TestRunUpdate_Scoped_OnlyNamedPackageChanges`
3. `TestRunUpdate_Scoped_NoFrozenOverridesCIAutoFrozen`
4. `TestRunUpdate_GitSemver_InstallPathClearedEvenWhenTagUnchanged`
5. `TestRunUpdate_GitSemver_DevDependency_InstallPathClearedEvenWhenTagUnchanged`

其餘既有測試(`Scoped_FrozenRefusedWithoutOverride`、`Scoped_CIAutoFrozenRefused`、`RefusesVirtualPathEscapingApmModules`、`RefusesParentSegmentStayingInsideApmModules`、`RegistryDevDependency_RequiresExperimentalFlag`、`NoManifest`、`NoLockfile`)都在新閘門插入點**之前**就已 return/error,不受影響——已逐一追蹤其 return 路徑確認早於 buildLockfile/printUpdateSummary。

## 7. 驗證策略摘要(細節見 implement.md)

- TDD 先紅後綠:update×local dep 的 materialize/deploy/lockfile-key 三斷言、零 target exit 2 斷言(含既有測試回歸清單 §6)、scoped local token 斷言。
- 全量 `go build/vet/test ./...`;live A/B 以 findings §C 矩陣重跑 Go 側逐格比對(#2 → 與 install 結果一致;#3b → exit 2)。
- Python 側證據已凍結於 findings(oracle 不需重跑;重跑僅限 scratch 專案,禁 marketplace 子命令)。
