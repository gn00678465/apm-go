# Research: apm update × local deps × 零 target 閘門(F1 gap 定案)

- **Query**: `apm update` 是否該 materialize/deploy local deps?零 target 時行為?(Python oracle 實測 + 兩邊源碼對照)
- **Scope**: mixed(apm-go 源碼 + Python oracle 源碼 + live A/B 實測)
- **Date**: 2026-07-11
- **Scratch fixtures**: `%TEMP%\apm-update-research-0711\{py1,py2,py3,py3b,go1,go2,go3}`(throwaway,可重建;每個專案 = `apm.yml` + `./dep-pkg`(含 `.apm/instructions/style.instructions.md` + `.apm/agents/helper.md`),py1/py2/go1/go2 有 `target: claude`,py3/py3b/go3 無 target)

## 結論摘要(定案建議)

1. **Scope question 定案:修 parity,不是 documented deviation。** Python `apm update` 與 `apm install` 共用同一條 install pipeline(`update.py:530-541` 呼叫 `_install_apm_dependencies(update_refs=True, plan_callback=...)`),local deps 會被 materialize(`apm_modules/_local/<name>/`)且 deploy(P1 實測 exit 0、`.claude/rules`+`.claude/agents` 部署成功)。
2. **apm-go 現況比 spec Warning 說的「fails safe」更糟:`apm update` 會破壞性改寫 lockfile。** G2 實測:update 後 `_local/dep-pkg-7dd7d265` 條目(含 `deployed_files`/`deployed_file_hashes`)被換成裸條目 `repo_url: ./dep-pkg, source: local`——uninstall provenance 與 frozen 驗證依據全滅,且靜默 exit 0。
3. **零 target 閘門:Python update 有(承繼 install pipeline),但被 plan gate 前置。** 零 target + plan 會前進(無 lockfile 條目/有變更)→ exit 2 `No harness detected`(P3);零 target + plan 無變更 → 在 plan gate 就 exit 0,閘門不觸發(P3b)。apm-go update 完全無閘門:靜默 exit 0 且照樣破壞 lockfile(G3)。
4. **建議修法**:runUpdate 加 `normalizeLocalDep` 迴圈(與 install.go:306-311 同款)+ 在 `deployAndFinalize` 前加零 target 閘門(重用 `errNoDeployTarget()`)。兩個 plan-gate 順序差異記 documented deviation(詳 design.md)。

## 逐項發現

### A. apm-go 現況(file:line)

| # | 事實 | 證據 |
|---|---|---|
| A1 | `runUpdate` 讀 apm.yml 後**不呼叫** `normalizeLocalDep`;install 在 1b-2 步對 `ParsedDeps`+`ParsedDevDeps` 全量 normalize | `cmd/apm/update.go:61-72`(無 normalize);`cmd/apm/install.go:306-311`(install 的迴圈) |
| A2 | `normalizeLocalDep` 把 local dep 改寫為 `{Source:"git", RepoURL:localModulesKey(abs), LocalSourcePath:abs}`;`LocalSourcePath` runtime-only 不落盤 | `cmd/apm/install.go:1277-1300`;key 演算法 `install.go:1326-1330`(`_local/<sanitizedBase>-<sha8>`) |
| A3 | `gitops.LoadPackage`:`LocalSourcePath != ""` → `materializeLocalCopy`(RemoveAll 舊 copy → `copyTreeNoSymlinks`;dest 經 `installPath` 受 `archive.ContainedKey` 保護) | `internal/gitops/clone.go:23-35`、`:250-278`、`:287-323` |
| A4 | 未 normalize 的 local dep 在 resolver 中 `depKey = ref.LocalPath`(如 `./dep-pkg`);`LoadPackage` 走 `loadLocalPackage`(原地 parse,零 materialize) | `internal/resolver/resolver.go:369-378`;`internal/gitops/clone.go:33-35` |
| A5 | `PlanFullUpdate` = `Resolve(m, nil, ...)`(從頭解),所以 update 的 local dep 會以 `./dep-pkg` 為 key 進入 result → buildLockfile → **lockfile 被改寫成錯誤 key space** | `internal/resolver/update.go:12-21`;實測 G2 |
| A6 | `runUpdate` 無零 target 閘門:直接 `deployAndFinalize`;install 有兩道(deps-present `install.go:613-618`、local-primitives-only `install.go:538-541`,共用 `errNoDeployTarget()` `install.go:624-629` = exit 2) | `cmd/apm/update.go:157`;`cmd/apm/install.go:613-629` |
| A7 | `deployAndFinalize` 零 targets 時跳過部署但**仍寫 lockfile**(no-op 檢查在部署後,`IsSemanticEqual` 不等就寫) | `cmd/apm/install.go:822-944`(no-op `:931-934`、寫檔 `:936-944`) |
| A8 | `directGitSemverUpdateScope` 只清 `KindGitSemver`;normalize 後的 local dep(Source git、Reference "")分類為 `KindGitLiteral`,**不會**被誤清 | `cmd/apm/update.go:168-184`;`internal/resolver/classify.go:46-60`(`ClassifyRef("")==RefNone`) |
| A9 | scoped update 以 `depKey(dep)==packageName` 匹配 manifest 條目;normalize 後 local dep 的 key 變 `_local/<base>-<sha8>`,positional `./dep-pkg` 將 match 不到(修復時需 token 轉譯,先例:`uninstallRemovalKey`) | `internal/resolver/update.go:46-63`;`cmd/apm/uninstall.go:179` 附近(ag-23 pattern) |
| A10 | update 要求既有 `apm.lock.yaml`(無則 exit 1) | `cmd/apm/update.go:74-76`;實測 G1 |

### B. Python oracle 源碼(file:line)

| # | 事實 | 證據 |
|---|---|---|
| B1 | `apm update` = install pipeline + plan/consent gate:`_install_apm_dependencies(staged_pkg, update_refs=True, plan_callback=...)` | `apm/src/apm_cli/commands/update.py:530-541` |
| B2 | pipeline 順序:resolve → **plan gate** → policy → **targets** → download → integrate → cleanup → lockfile;plan_callback 回 False 就 `return InstallResult()`(不 deploy、不寫 lockfile) | `apm/src/apm_cli/install/pipeline.py:565-603`(plan gate `:597-603`)、targets `:633-636` |
| B3 | local dep 在 **resolve 階段**就經 `download_callback` → `_copy_local_package` copy 進 `apm_modules/_local/<name>/`——發生在 plan gate **之前**(副作用不受 gate 管) | `apm/src/apm_cli/install/phases/resolve.py:573-621`;實測 P2b/P3 |
| B4 | resolve 有 cache short-circuit:install path 已存在 → 直接 return,不 re-copy(`_force_semver_resolve` 條件排除 local) | `apm/src/apm_cli/install/phases/resolve.py:487-496` |
| B5 | download/integrate 階段 `LocalDependencySource.acquire()` **每次都** `_copy_local_package`(plan 前進時 local dep 必然刷新+重部署) | `apm/src/apm_cli/install/sources.py:172-233`;實測 P2c |
| B6 | plan 以 `local_path` 為 local dep 的 key;比較僅看 resolved_ref/commit——**內容變更不算變更**;lockfile 有條目且 ref/commit 相同 → `unchanged` | `apm/src/apm_cli/install/plan.py:57-71`、`:216-229` |
| B7 | 零 target:targets phase 丟 `NoHarnessError`(`click.UsageError` 子類 → exit 2,「No harness detected」) | `apm/src/apm_cli/core/errors.py:22-31`、`:71-94`;`core/target_detection.py:807,836` |
| B8 | Python update 無 lockfile 也能跑(全部視為 `[+] added`);Python lockfile 的 local 條目 = `repo_url: _local/<name>` + `source: local` + `local_path: ./dep-pkg` + deployed hashes | 實測 P1(lockfile dump) |

### C. Live A/B 實測矩陣(2026-07-11;Python = `uv --project D:/Projects/apm-dev/apm run apm`,Go = `bin/apm-go.exe` @ commit 5c869ca)

| # | 情境 | Python | apm-go |
|---|---|---|---|
| 1 | fresh(無 lockfile)、local dep、target claude、`update --yes` | **exit 0**;materialize `apm_modules/_local/dep-pkg/`;deploy `.claude/rules/style.md`(applyTo→paths 轉換)+`.claude/agents/helper.md`;寫 lockfile 含 deployed_files/hashes(P1) | **exit 1** `update requires an existing apm.lock.yaml`;零副作用(G1) |
| 2 | install 後、dep 源碼內容改 v1→v2、`update` | **exit 0**「All dependencies already at their latest matching refs.」;**不** re-copy、**不**重部署(deployed 仍 v1;plan gate 判 unchanged)(P2) | **exit 0**;印 `+ ./dep-pkg@ (new)` + `[!] warning: ./dep-pkg deployed 0 files to any target`;不 materialize、不部署;**lockfile 破壞性改寫**:`_local/dep-pkg-7dd7d265`(含 deployed_files/hashes)→ 裸 `repo_url: ./dep-pkg, source: local, depth: 1`(G2) |
| 2b | 同上 + 先刪 `apm_modules`、`update --yes` | exit 0(plan gate 仍擋部署)但 **resolve 已把 v2 re-copy 進 apm_modules**(部署檔仍 v1 → Python 自身也有 copy/deploy 漂移)(P2b) | (Go 無論如何不 materialize,見 #2) |
| 2c | 同上 + 刪 lockfile(plan 前進)、`update --yes` | **exit 0**;re-copy + 重部署 v2(P2c)⇒ Python 語意:「plan 前進時 local dep 必刷新」 | n/a(Go 無 lockfile 直接 exit 1) |
| 3 | deps 存在、**零 target**、fresh、`update --yes` | **exit 2**「No harness detected」;apm_modules 已 materialize(resolve 副作用);**不寫 lockfile**(P3)——與 install 同款閘門 | 無法到達(先撞 lockfile 要求 exit 1)(G1) |
| 3b | deps 存在、零 target、lockfile 在、plan unchanged、`update --yes` | **exit 0**(plan gate 先短路,閘門不觸發);lockfile 不動(P3b) | **exit 0 靜默**;無閘門;照樣破壞性改寫 lockfile(G3) |
| 4 | scoped `update ./dep-pkg` | 接受 local path token,exit 0(unchanged) | 現況接受(pre-normalize key=LocalPath),exit 0「Already up to date」;**修復後若不轉譯 token 會回歸成 "package not found"** |
| 5 | 事後 `install`(修復力) | — | exit 0;re-materialize+重部署 v2+lockfile 還原 `_local/...` 條目 ⇒ 損壞可自癒(G2 追測) |

### D. 同族比較:install 契約(既有,不動)

- install 零 target 矩陣見 spec `backend/install-marketplace-contracts.md:28-43`(§2);F1 契約 `:56-77`(§4);本 task 要把 `:72` 的 Warning 換成修復記錄。
- archive follow-up 來源:`archive/2026-07/07-11-instructions-applyto-parity/prd.md:82`(follow-up 3:update 不走零 target 閘門)、`archive/2026-07/07-05-runtime-parity-gaps/prd.md:81`(follow-up 4:update 不 materialize local deps)。

## 風險

1. **(現況風險,修復動機)lockfile provenance 破壞**:G2/G3 證明任何含 local dep 的專案跑一次 `apm-go update` 就丟失 deployed_files/hashes——`uninstall` 的部署檔清理與 `install --frozen` 的 `VerifyDeployedState` 都依賴它。嚴重度應從「fails safe」上修。
2. **(修復風險)scoped update 回歸**:normalize 後 manifest key space 變 `_local/...`,`apm update ./dep-pkg` 若不做 token 轉譯會從「可用」變「package not found」(A9)。
3. **(修復風險)零 target 閘門過嚴**:Go 無 plan gate,unconditional 閘門會使「零 target + 無變更」exit 2,而 Python 該情境 exit 0(P3b)。屬 fail-loud 方向的偏差,與 Go install §2 矩陣一致;建議記 documented deviation 而非複製 plan gate(見 design)。
4. **(修復風險)update 會開始每次重部署 local dep**(deploy 無條件跑):Python plan unchanged 時不重部署(P2)。Go 行為是超集且冪等,還能自癒 Python 那種 copy/deploy 漂移(P2b);記 deviation。
5. 安全不變式無弱化空間:修復重用 install 既有路徑(`normalizeLocalDep` → `materializeLocalCopy`),`ContainedKey`/symlink 拒絕/`copyTreeNoSymlinks` 全在原位(A2/A3)。

## 需拍板事項(附選項與建議)

1. **零 target 閘門形狀**
   - A(建議):unconditional——`len(result.Deps)>0 && len(targets)==0` → 印 targetDiags + `errNoDeployTarget()`(exit 2),置於 lockfile 寫入前(零 partial write)。與 install §2 矩陣一致、同時堵住 G3 的 provenance 破壞;「無變更+零 target」比 Python 嚴(P3b),記 deviation。
   - B:先仿 Python 把 no-op 檢查前移(semantic-equal → exit 0),再閘門。parity 較貼但把「Already up to date 不重部署」這個更大的行為變更拖進來,超出 PRD 範圍。
2. **scoped update token 轉譯**:是否接受 `apm update <local-path>`?建議照 uninstallRemovalKey 先例轉譯(parse token → IsLocal → `localModulesKey(resolveLocalSourceAbs(path))`),維持現況與 Python 可用性;不做則需在 PRD 記錄破壞。
3. **update 要求 lockfile(A10/G1)**:Python 不要求。本 task 建議維持(pre-existing 明確報錯、fail-loud),記 documented deviation,不展開。
4. **update 每次重部署 local dep(風險 4)**:建議接受(冪等、可自癒漂移),記 deviation;不建議引入 plan gate(那是另一個 feature 的 scope)。
