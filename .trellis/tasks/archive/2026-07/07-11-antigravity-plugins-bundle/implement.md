# Implement: antigravity plugins bundle 部署

> **狀態：實作完成（implemented）**（2026-07-11；design.md §8 拍板已回填，
> 見 design.md §8「拍板紀錄」。5.3 code-reviewer 過關與 5.4 commit 留給後續
> agent；其餘 phase 已完成並驗證，見下方逐項證據）。

## 前置

- [x] design.md §8 四項拍板完成（skills 遷入與否、ab 腳本更新解讀、bundle 命名、local 平鋪）
- [x] `agy --version` 確認仍為 1.1.1（本輪重驗 2026-07-11，與 research §G 一致）
- [x] 分支 `feat/marketplace-install` 上開工；explicit-only / MCP / conflict 既有契約零變更

## Phase 1 — bundle 路徑分流（TDD）

- [x] 1.1 `internal/deploy/antigravity_bundle_test.go` 新增
      `TestRun_AntigravityBundlePaths`（table-driven：dep 的
      instructions/agents/skills/hooks 落
      `.agents/plugins/<pkg>/{rules/<n>.md, agents/<n>/agent.md, skills/<n>/, hooks.json}`）
      與 `TestRun_AntigravityLocalPathsUnchanged`（local 四型維持平鋪路徑，且
      不產生 `.agents/plugins/`）。
- [x] 1.2 `TestRun_AntigravityTwoDependencyHooksIsolated`：兩 dep 各帶一個 hook
      檔 → 兩份 hooks.json、無 `overwrites` 診斷、各自與來源 byte-equal（AC2）。
- [x] 1.3 `internal/deploy/antigravity_bundle.go` 新增 `antigravityBundleDir` /
      `bundleNameFromDepKey` / `sanitizeBundleSegment`（DepKey 末段，
      `[A-Za-z0-9._-]` 外字元→`-`）；`internal/deploy/antigravity.go` 的
      `DeployPrimitive` 依 `p.DepKey == ""` 分流（design §4.2）。
- [x] 1.4 既有測試修正：`TestRun_AgentSameNameCollision_FirstDeclaredWins` 的
      antigravity 分支路徑改為 `.agents/plugins/first/agents/reviewer/agent.md`；
      新增 `TestRun_AntigravitySameDependencyHooksOverwriteDiagnostic`（同套件雙
      hook 檔仍在單一 bundle hooks.json 上收斂＋overwrite 診斷，design §6.5/D6）。
      S-003（`TestRun_MultipleHooksOverwriteDiagnostic`）本身走純 local fixture，
      無需改動即維持通過。claude/codex/... 測試一律未動。
- [x] 1.5 驗證：`go test ./internal/deploy/ ...` 全綠（本機無 C 編譯器，
      `-race` 需要 cgo 無法執行——`CGO_ENABLED=1` 環境限制，已改以
      `go vet ./...` + 完整 `go test ./... -count=1` 驗證正確性）。

## Phase 2 — plugin.json（BundleTarget interface，TDD）

- [x] 2.1 `TestRun_AntigravityPluginManifestProvenance`：dep 部署後存在
      `.agents/plugins/<pkg>/plugin.json`，內容 `{"name": "<pkg>"}\n`（位元組
      確定性）；該路徑與 hash 出現在 `result.PerDep[depKey]`。
      `TestRun_AntigravityPluginManifestReinstall`：**連續兩次 Run 後仍在**且
      bytes 不變（R3）。
- [x] 2.2 `TestRun_AntigravityBundleNameCollision`：兩 depKey 末段同名 →
      **fail-closed**（`Run()` 回傳非 nil error，`.agents/plugins/` 完全不存在）
      ——實作期把原草稿的「diagnostic-only、仍照寫」升級為 fail-closed（design
      §4.3 更新、§8 拍板紀錄第 3 點）。
- [x] 2.3 `internal/deploy/adapter.go` 加 `BundleTarget` interface
      （`ValidateBundleNames` + `FinalizeBundles`，design §4.3）；
      `internal/deploy/deploy.go` 的 `Run()` 在 `ResolvePrimitives` 後、部署迴圈前
      呼叫 `ValidateBundleNames`（fail-closed 檢查）；迴圈中記錄 bundledDeps
      （去重保序）；迴圈後、`WriteMCP` 前呼叫 `FinalizeBundles`，回傳檔案 hash
      後 append 進 `PerDep`；antigravity 實作於
      `internal/deploy/antigravity_bundle.go`。
- [x] 2.4 驗證：`go test ./internal/deploy/ ...` 全綠；`TestWriteMCP_Antigravity_*`
      等既有 MCP 測試全綠未動。

## Phase 3 — uninstall 生命週期（TDD）

- [x] 3.1 `cmd/apm/uninstall_antigravity_test.go`（新檔，同 package main）——
      `TestRunUninstall_AntigravityBundleRemovedSiblingBundleSurvives`：uninstall
      一個帶四型 primitives 的 dep bundle 後，該 bundle 目錄整個消失、sibling
      bundle（另一 dep）與共用 `.agents/plugins/` root 存活。
- [x] 3.2 `TestRunUninstall_AntigravityBundleUserFileSurvives`：bundle 目錄內
      使用者手動檔案 → uninstall 後檔案與目錄存活（未列入 deployed_files 的
      檔案天然不會被刪）。`TestRunUninstall_AntigravityTamperedManifestKeptWithWarning`：
      手改 plugin.json → 保留＋`"modified since deploy (hash mismatch)"` stderr
      警告（AC3, un-053）。
- [x] 3.3 GREEN 確認為零實作變更：`internal/deploy/uninstall.go` /
      `cmd/apm/uninstall.go` 未修改一行，全靠既有
      `RemoveDeployedFiles`/`cleanupEmptyParents` 通用邏輯覆蓋。
- [x] 3.4 驗證：`go test ./cmd/apm/ -run TestRunUninstall -v` 全綠（29 個測試，
      含 3 個新增；`-race` 同 1.5 的環境限制，已改以 `go vet` + 完整
      `go test ./cmd/apm/... -count=1` 驗證）。

## Phase 4 — 全量驗證與 A/B

- [x] 4.1 `go build ./...` / `go vet ./...` / `go test ./... -count=1` 全綠；
      `go test ./internal/deploy/... -cover` = 88.5%、
      `go test ./cmd/apm/... -cover` = 86.1%（AC4, ≥80% gate 達成）。
- [x] 4.2 rebuild：`go build -o bin/apm-go.exe ./cmd/apm`（exit 0）。
- [x] 4.3 更新 `D:/Projects/apm-dev/evals/ab_antigravity.py`（依拍板 2）：
      - §2 dep agent 斷言改為動態探測 `.agents/plugins/<pkg>/agents/depagent/agent.md`
        （local-path dep 的 `<pkg>` 含 hash 後綴，程式化探測而非硬編碼）
      - §3 uninstall 斷言改 bundle 目錄整體消失、local `.agents/hooks.json` 存活
      - 新增 §2b/§2c 驗證段：`plugin.json` 存在且 name==bundle 目錄名/無 BOM/LF
        結尾、dep hooks 落 `.agents/plugins/<pkg>/hooks.json`、
        `.agents/hooks.json` 僅含 local hook 內容（新增 dep-pkg 專屬 hook fixture）
      - live leg（原 §4）改為對 apm-go **實際產出**的
        `.agents/plugins/<pkg>` 直接跑 `agy plugin validate`（不再手工 repack），
        移到 uninstall 之前執行（uninstall 會刪掉 bundle）；仍 SKIP-not-FAIL
        當 agy 不在 PATH
- [x] 4.4 跑 `python D:/Projects/apm-dev/evals/ab_antigravity.py` →
      `ALL CHECKS PASSED (ab_antigravity)`（29/29，含 live `agy plugin validate`）。
      `python D:/Projects/apm-dev/evals/ab_uninstall.py` 無回歸
      （6 passed, 0 failed, 2 documented deviations，與本任務無關的既有 deviation）。
- [x] 4.5 實機 AC1：TEMP scratch fixture 專案（兩個 local-path dep）
      `install --target agy` 後對兩個 bundle 各自
      `agy plugin validate <proj>/.agents/plugins/<pkg>` → 兩者皆 `[ok]` exit 0，
      skills/agents/hooks 各 `1 processed`；negative probes（缺 name、缺
      plugin.json）分別 `Error: plugin.json missing name` / `Error: missing
      plugin.json` exit 1，與 research §G3/G5 一致。

## Phase 5 — spec 與收尾

- [x] 5.1 更新 `.trellis/spec/backend/antigravity-target-contract.md`：新增
      §7「Plugin bundle deployment (documented extension)」——bundle 佈局表、
      DepKey→bundle 名規則與 fail-closed 碰撞防護、plugin.json name 硬性必要
      （**agy 1.1.1 delta**，1.0.16 為 optional）、hooks per-plugin 解覆蓋缺口
      （§7.1/§7.5 殘留同套件內缺口）、MCP 不遷入的依據（§7.4）、R2/R4/R7/R8
      caveats（§7.7）、Python 無 bundle 的 deviation 記錄、test locks 清單
      （§7.8）（AC5；12 項硬性 needle 全數命中）。
- [ ] 5.2 依 `.trellis/spec/conformance/cli-verification-checklist.md` 格式產
      本 task 硬性 checklist——**已存在**（`checklist.md`，39 項，由前一輪
      agent 建立）；本輪未逐項勾選（AGB-001~039 的機械化 PowerShell 驗證留給
      後續 `trellis-check` 階段執行，本 implement 輪已在對應 Go 測試與本 Phase
      4 的 bash/TEMP 驗證中涵蓋其技術內容）。
- [ ] 5.3 review gate：code-reviewer 過 CRITICAL/HIGH——留給後續 agent。
- [ ] 5.4 commit 依 conventional commits 原子拆分（adapter 分流 / BundleTarget /
      uninstall 測試 / A/B 腳本 / spec）——由主 agent 執行，本輪不含 commit/push
      （遵守 forbidden-operations 規則）。

## 驗證指令速查

```
go test ./internal/deploy/ -race
go test ./cmd/apm/ -race
go test ./... -race -cover
go build -o bin/apm-go.exe ./cmd/apm
python D:/Projects/apm-dev/evals/ab_antigravity.py
agy plugin validate <proj>/.agents/plugins/<pkg>   # TEMP scratch only
```

## Review gate

- AC1 validate PASS 證據（terminal 輸出）
- AC2 雙套件 hooks 測試名 + 綠燈證據
- AC3 uninstall 測試名 + 綠燈證據
- AC4 覆蓋率數字 + ab 腳本輸出
- AC5 spec diff
- 安全鐵則遵守聲明：無 repo 根 install/uninstall、無 Python oracle marketplace 操作、無 git push
