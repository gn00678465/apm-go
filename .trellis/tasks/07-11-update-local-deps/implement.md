# Implement: apm update × local deps + 零 target 閘門

> **draft — 待 review**
> 執行順序固定;每步附驗證指令。契約與依據見 `design.md` / `research/findings.md`(design.md 另含本輪 research agent 的二次獨立 live 驗證校正,見 design.md §2 C2 的「排序修正」與 §6)。

## Phase 0 — 前置

- [ ] 讀 `.trellis/spec/backend/install-marketplace-contracts.md` §2/§4 與 `cmd/apm/install.go:306-311`、`:1258-1352`(normalizeLocalDep 家族)、`cmd/apm/update.go` 全檔。
- [ ] 基線綠:`go build ./... && go vet ./... && go test ./cmd/apm/ ./internal/gitops/ ./internal/resolver/`

## Phase 1 — C1/C3:update materialize local deps(TDD)

- [ ] **RED**:`cmd/apm/update_test.go` 新增(fixture 模式參考 `TestRunUpdate_Full_ReResolvesToNewestAndRewritesLock` 的 t.TempDir + chdir 慣例):
  1. `TestRunUpdate_LocalDep_MaterializesAndDeploys` — 佈置:apm.yml(`./dep-pkg` local dep + `target: claude`)、dep-pkg 含 `.apm/instructions/*.instructions.md` 與 `.apm/agents/*.md`、先跑 `runInstall` 產 lockfile;變更 dep 源碼內容;跑 `runUpdate(deps,false,true,"")`。斷言:(a) `apm_modules/_local/dep-pkg-<sha8>/` 內容 = 新源碼;(b) `.claude/rules/`、`.claude/agents/` 部署檔內容刷新;(c) lockfile 條目 key 仍為 `_local/dep-pkg-<sha8>` 且 `deployed_files`/`deployed_file_hashes` 非空(堵 findings C#2 破壞)。
  2. `TestRunUpdate_LocalDep_LockfileKeyStable_NoBarePathEntry` — 同佈置,斷言 update 後 lockfile **不含** `repo_url: ./dep-pkg` / `source: local` 裸條目。
  3. `TestRunUpdate_Scoped_LocalPathToken_Matches` — `runUpdate(deps,false,true,"./dep-pkg")` 不得回 "package not found";行為同 full update 對該 dep(C3)。
  - 驗證:`go test ./cmd/apm/ -run TestRunUpdate_LocalDep -v` 三紅,失敗訊息符合現況(deployed 0 files / bare entry / not found)。
- [ ] **GREEN**:`cmd/apm/update.go`:
  - `manifest.ParseManifest` 成功後插入 normalize 迴圈(ParsedDeps + ParsedDevDeps;與 install.go:306-311 同款,附註解引用 F1/本 task)。
  - scoped token 轉譯 helper(cmd 層,mirror `uninstallRemovalKey` 先例):token parse 為 local(`manifest.ParseDepString` → `IsLocal` 或 `IsAbsoluteLocalPath`)→ `localModulesKey(resolveLocalSourceAbs(path))`;套用於 `runUpdate` 的 `pkg`(進 `directGitSemverUpdateScope` 與 `PlanScopedUpdate` 前)。
  - 驗證:`go test ./cmd/apm/ -run TestRunUpdate -v` 全綠(含既有 12 個 TestRunUpdate_*)。
- [ ] 安全不變式自查(design C4):diff 僅 cmd/apm 層;無新 filepath.Join(apm_modules, 非key);`LocalSourcePath` 未進任何序列化路徑。

## Phase 2 — C2:零 target 閘門(TDD)

- [ ] **RED**:`TestRunUpdate_DepsPresentZeroTarget_ExitsWithTeachingMessage`(mirror `install_test.go:1023` 同名 install 測試):佈置 local dep 專案 + lockfile(先 install -t claude 後刪 `.claude/`,或直接手工 lockfile),apm.yml 無 target、目錄無 harness 訊號;跑 `runUpdate`。斷言:err 非 nil、`exitCodeOf(err)==2`、訊息含 `no deployment target detected`、**lockfile bytes 不變**(零 partial write)。
  - 驗證:`go test ./cmd/apm/ -run TestRunUpdate_DepsPresentZeroTarget -v` 紅(現況 silent exit 0)。
- [ ] **GREEN**:`runUpdate` 在 `printUpdateSummary` **之後**、`deployAndFinalize` **之前**插入閘門(design C2 snippet,注意順序——閘門必須排在 plan 輸出之後,對齊 Python 已實測的 stdout 順序;重用 `errNoDeployTarget()`、印 targetDiags 到 stderr)。
  - 同一 commit 內修補 design.md §6 列出的 5 個既有測試 fixture,補 `target:\n  - claude\n`(不動它們原本斷言的 git-semver/frozen 邏輯):
    - `TestRunUpdate_Full_ReResolvesToNewestAndRewritesLock`
    - `TestRunUpdate_Scoped_OnlyNamedPackageChanges`
    - `TestRunUpdate_Scoped_NoFrozenOverridesCIAutoFrozen`
    - `TestRunUpdate_GitSemver_InstallPathClearedEvenWhenTagUnchanged`
    - `TestRunUpdate_GitSemver_DevDependency_InstallPathClearedEvenWhenTagUnchanged`
  - 驗證:該測試綠;`go test ./cmd/apm/ -run TestRunUpdate -v` 全綠(新舊合計 15+ 個 case,無一因新閘門翻紅)。

## Phase 3 — 全量驗證

- [ ] `go fmt ./... && go vet ./...`
- [ ] `go build ./... && go build -o bin/apm-go.exe ./cmd/apm`
- [ ] `go test ./...`(全 repo)+ `go test ./cmd/apm/ -cover`(維持 ≥80%,新增行覆蓋)
- [ ] Live 重跑 findings §C 矩陣 Go 側(scratch:系統 TEMP 下自建,**絕不在 repo 根**):
  - C#2 情境 → update 後 materialize+deploy 內容刷新、lockfile key `_local/...` 含 hashes(與 install 結果 diff 一致)
  - C#3b 情境 → exit 2 + 教學訊息、lockfile 不變、**stdout 含 update plan 且順序在教學訊息之前**(對齊 Python)
  - C#4 情境 → `update ./dep-pkg` 可用
  - (Python 側 oracle 證據已凍結於 findings §C,不需重跑;若重跑僅限 scratch 專案,**禁止 marketplace add/remove/update**)
- [ ] A/B 回歸:`ab_mcp_install_parity.py`、`ab_instructions_applyto.py`、`ab_uninstall.py`(涉 lockfile/deploy provenance 的三支)無回歸。

## Phase 4 — Spec / PRD 收尾

- [ ] `.trellis/spec/backend/install-marketplace-contracts.md`:
  - §4 `:72` Warning 改為決策/修復記錄(含 commit hash、oracle 證據指標 → 本 task findings)。
  - deviations 表補 D1/D2/D3(design §4 表)。
  - §2 矩陣若敘及 update,同步一行「update 同 install 閘門(D3 附註)」。
- [ ] PRD `prd.md`:AC 勾選 + 「Python oracle 行為查證記錄」段落指向 `research/findings.md`。
- [ ] Review gate:
  - [ ] code-review(或 codex rescue)過 CRITICAL/HIGH = 0
  - [ ] 對照 design.md 契約逐條驗收(C1–C4)
  - [ ] commit 拆分:`fix(update): materialize local deps via normalizeLocalDep(F1)` / `fix(update): zero-target gate(exit 2)` / `docs(spec): ...`(commit-message skill;不含未經同意的 push)

## 驗證指令速查

```bash
go test ./cmd/apm/ -run 'TestRunUpdate' -v
go test ./cmd/apm/ -run 'TestRunUpdate_LocalDep|TestRunUpdate_DepsPresentZeroTarget|TestRunUpdate_Scoped_LocalPathToken' -v
go build ./... && go vet ./... && go test ./...
go build -o bin/apm-go.exe ./cmd/apm
```
