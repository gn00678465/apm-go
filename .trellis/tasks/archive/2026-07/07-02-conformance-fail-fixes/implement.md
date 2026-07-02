# Implement — 修復 Phase 0-5 驗證確認的 FAIL/MISSING 缺口

依 `design.md`。TDD：每步先寫測試（RED）→ 實作（GREEN）。每步後 `go test ./... && go vet ./...`。
每組工作（A/B/C）完成後送 **codex exec 唯讀審查**（`codex exec --dangerously-bypass-approvals-and-sandbox --skip-git-repo-check --cd /d/Projects/apm-dev/apm-go`），prompt 必須明確禁止其修改任何檔案（前次任務曾發生審查越權直接改碼的情況）；若審查認為某個既有缺口需要修正，先回報、由使用者決定是否納入本任務或另開任務，不得自行擴大範圍。

## 執行順序

### 步驟 B — req-lk-007 skip-download 驗證（已完成實作，經 2 輪 codex review + 1 輪 advisor 覆核）

- [x] 寫測試：`internal/gitops/clone_test.go` 涵蓋 `checkoutMatchesRef` 的 tag/annotated-tag/dirty-worktree/untracked-file/ref-not-found/non-repo 各情境，以及 `LoadPackage` 的 skip-clone / re-clone-when-stale / clone-when-missing 端對端案例。
- [x] 實作：`internal/gitops/clone.go` `LoadPackage` 改為一律先呼叫 `checkoutMatchesRef`（本地 `git rev-parse <ref>^{commit}` 解析，annotated tag 需 peel）比對 HEAD 且 worktree 需乾淨才跳過 clone，否則整目錄砍掉重新 clone。
- [x] `cmd/apm/install.go` frozen 路徑（原 `os.Stat` 存在性短路）改為一律呼叫 `deps.loader.LoadPackage`，並優先傳入 `dep.ResolvedCommit`（權威 pin）而非 `dep.ResolvedRef`（可能是會變動的分支名）。
- [x] 端對端：`cmd/apm/gitcheckout_e2e_test.go` 新增 `TestRunInstall_StaleCheckoutIsRepaired`（真實 `RealPackageLoader`，local git-path 依賴，stale checkout 被正確修復回 pinned tag）。
- [x] **第 1 輪 codex review 發現並修正 3 項**：annotated tag 未 peel 導致恆為 mismatch（補 `^{commit}`）、dirty worktree 未檢查（補 `worktreeClean`）、frozen 路徑仍優先用 `resolved_ref` 而非 `resolved_commit`（已交換優先序，並補 `TestRunInstall_Frozen_PrefersResolvedCommitOverResolvedRef` 用 spyLoader 鎖住呼叫參數）。
- [x] **第 2 輪 codex review 於執行中撞到用量上限**（`ERROR: You've hit your usage limit`），改用 `advisor` 作為替代驗證管道，發現關鍵缺陷：frozen 路徑把 `resolved_commit`（40-hex SHA）當成 `resolvedRef` 傳入 `LoadPackage`，但這個值同時被 `cloneRepo` 拿去當 `git clone --depth 1 --branch <ref>` 的 `<ref>`——標準 shallow clone 的 `--branch` 不接受 raw SHA。已用實際 `git clone --depth 1 --branch <sha>` 指令實測重現（`fatal: Remote branch <sha> not found in upstream origin`，exit 128）確認為真缺陷。
- [x] **修正**：`internal/gitops/clone.go` 新增 `isCommitSHA` 判斷 + `cloneRepoAtCommit`（完整 clone 再 `git checkout <sha>`，不依賴伺服器端 `allowReachableSHA1InWant` 設定），`cloneRepo` 依 ref 是否為 SHA 分流。新增回歸測試 `TestLoadPackage_ClonesByRawCommitSHA`（端對端透過 `LoadPackage` 驗證 raw SHA 首次 clone 成功）與 `TestIsCommitSHA`（邊界案例）。詳見 design.md B.2.1。
- [x] **附帶發現、記錄但不修**：frozen 路徑重建 `DependencyReference` 時對 local git-path 依賴（`repo_url: ./remote`）的 owner/repo 解析既有缺陷（早於本次改動即存在，`git diff HEAD` 確認），只影響「frozen + local git-path 依賴」這個窄組合，列為已知限制，未列入本任務 B 範圍。
- **第 3 輪 codex review（額度恢復後，針對 SHA-clone 修正本身）**：確認 raw-SHA clone 路由正確、完整，全專案僅此一處組 `git clone --branch/--depth`。額外發現 3 項：(1) `checkoutMatchesRef` 對可變動 literal 分支名 ref 仍可能誤判——記錄為已知限制不修（req-lk-007 是 SHOULD、修正需要網路往返會抵銷優化本身意義，詳見 design.md B.5）；(2) frozen 路徑遺漏 `ref.VirtualPath`——已修正，補 `TestRunInstall_Frozen_PreservesVirtualPath`；(3) `worktreeClean` 未涵蓋 ignored 檔案——已修正（加 `--ignored`），補 `TestCheckoutMatchesRef_FalseWhenIgnoredFilePresent`。
- 驗證：`go test ./internal/gitops/... ./cmd/apm/... -run "LoadPackage|Frozen|StaleCheckout|IsCommitSHA"` PASS；`go test ./... -count=1` 全綠；`go vet ./...` / `gofmt -l` 皆乾淨；`git status conformance/` 確認唯讀 oracle 未被觸碰。
- **review gate B：通過**（3 輪 codex review + 1 輪 advisor 覆核，所有可低風險修正的發現皆已納入並補測試；1 項記錄為已知限制）。

### 步驟 C — Target 自動偵測修正（C.1/C.2 已完成並經 codex exec 唯讀審查；C.3 待使用者確認，暫不實作）

- [x] C.1 antigravity：`internal/deploy/adapter.go` 的 `explicitOnlyTargets` 移除 `antigravity`（只留 `agent-skills`）；修正第 79 行附近註解，說明只有 agent-skills 是規範明文 explicit-only。
- [x] 測試：`internal/deploy/deploy_test.go` 原本斷言「antigravity 被排除」的 `TestResolveTargets_AntigravityNotAutoDetected` 已改寫為 `TestResolveTargets_AntigravityAutoDetected` + `TestResolveTargets_AntigravityAutoDetectedFromAgentsMDAlone`，斷言 antigravity 會被正確自動偵測。相關測試檔（`internal/deploy/mcp_writers_test.go`、`cmd/apm/mcp_e2e_test.go`）中「antigravity 是 explicit-only」的過時註解已一併修正（測試本身用 `--target antigravity` 顯式指定，行為不受影響，只是註解說法過時）。
- [x] C.2 copilot：`internal/manifest/detect.go` `SignalWhitelist` 移除 `.github/instructions/`、`.github/agents/`、`.github/prompts/`、`.github/hooks/` 四筆，只留 `.github/copilot-instructions.md`。
- [x] 測試：`internal/manifest/detect_test.go` 的 `TestDetectTargets_AllSignals` 表格中原本斷言這四個訊號會觸發 copilot 偵測的案例已反轉為斷言「單獨存在時不觸發」（`nil`）。
- [x] **codex exec 審查追加發現並修正**：`internal/deploy/adapter.go` 的 `allAutoDetectableTargets()`（供 `--target all` / manifest `target: all` 展開用）仍只回傳 `claude/codex/copilot/opencode`，未含 antigravity——C.1 修正後 antigravity 已是正常自動偵測 target，理應也算入 `all` 展開範圍（`agent-skills` 維持排除，規範明文永不自動偵測）。已加入 antigravity 並新增 `TestResolveTargets_FlagAllIncludesAntigravity` 回歸測試。
- 驗證：`go test ./internal/deploy/... ./internal/manifest/... -run "Detect|ResolveTargets"` — PASS；`go test ./... -count=1` 全綠；`go vet ./...` / `gofmt -l` 皆乾淨；`git status conformance/` 確認唯讀 oracle 未被觸碰。
- **review gate C（C.1/C.2）**：codex exec 唯讀審查（明確禁止改檔，本輪確認未越權）——確認兩項修正正確完整，核對 `antigravity.yaml`/`copilot.yaml` oracle 無衝突；發現並經人工核實後修正上述 `allAutoDetectableTargets()` 缺口。
- **C.3 minimal fallback（最終決議：descoped，本任務不實作）**：PRD/design 已規劃（新增 `internal/deploy/minimal.go` 的 `CompileMinimalAgentsMD` + `ResolveTargets` 無訊號時的 fallback 介接），但這是新行為而非單純修錯（advisor 覆核意見），使用者澄清問題僅涵蓋 C.1/C.2，尚未取得明確執行許可。`trellis-check` 於 Final 驗證階段獨立重新確認此缺口存在、詢問使用者是否納入，第二次詢問同樣逾時無回應——依本 session 建立的「逾時採用 Recommended 選項」慣例，最終決議：本任務以 A/B/C.1/C.2 完成收尾，C.3 另開新 Trellis 任務處理，`req-tg-001` 記錄為部分修復（2/3 子項）。詳見 prd.md 的 AC-C3 決議說明。

### 步驟 A — `apm update` 指令（已完成實作，經 2 輪 codex review）

- [x] 抽出共用邏輯：`cmd/apm/install.go` 抽出 `buildLockfile`（result → 完整 `*lockfile.Lockfile`，含 header + per-dep tree_sha256/resolved_commit 計算）與 `deployAndFinalize`（deploy.Run + no-op 檢查 + 寫 lockfile + persist apm.yml + 最終摘要列印）兩個函式，`runInstall` 尾端改為呼叫這兩個函式；抽取後 `go test ./cmd/apm/...` 全綠，codex 覆核確認「install path 行為未變（除了 B 段已審過的 frozen `LoadPackage` 改動外無其他行為差異）」。
- [x] 新增 `cmd/apm/update.go`：`updateCmd()` + `runUpdate()`，依 design.md A.1/A.2。
- [x] `cmd/apm/main.go` 註冊 `root.AddCommand(updateCmd())`。
- [x] 明確 update 對本次更新範圍內的 git-semver direct dep，下載前 `os.RemoveAll(apm_modules/<key>)`（req-lk-010），範圍由 `directGitSemverUpdateScope` 計算（全量 update 涵蓋所有 direct git-semver dep；scoped update 只清該 dep 本身，不含其 transitive 子樹）。
- [x] 測試（TDD）：`TestRunUpdate_Full_ReResolvesToNewestAndRewritesLock`、`TestRunUpdate_Scoped_OnlyNamedPackageChanges`（明確斷言 acme/b 沒被動到，不只檢查 acme/a）、`TestRunUpdate_Scoped_FrozenRefusedWithoutOverride`、`TestRunUpdate_GitSemver_InstallPathClearedEvenWhenTagUnchanged`、`TestRunUpdate_NoManifest`/`TestRunUpdate_NoLockfile`，另加真實 git 端對端 `TestRunUpdate_RealGitSemver_ResolvesToNewTag`（吸取 B 段「mock 測試曾漏掉真實 clone-by-SHA 缺陷」的教訓，證明整條 CLI→resolver→RealTagLister→RealPackageLoader 鏈路對真實 git 有效）。
- [x] **第 1 輪 codex review 發現並修正 3 項（皆為安全/正確性缺陷）**：(1) scoped update 對 frozen install 的拒絕檢查發生在 `os.RemoveAll` 清除 apm_modules 之後，導致「被拒絕」的操作仍有副作用——已改為 frozen 檢查移到 `runUpdate` 最前面、任何檔案 I/O 之前執行；(2) CI 環境自動觸發 frozen 時沒有可用的 override 旗標（即使既有 resolver 錯誤訊息本身就承諾「使用 --no-frozen 覆寫」）——已新增 `--no-frozen` 旗標並與 `--frozen` 設為 `MarkFlagsMutuallyExclusive`；(3) `apm_modules` 清除路徑沒有防護，`virtual_path` 只做字元集驗證、不像 local-path 依賴會檢查 `..`（`manifest.validateVirtualPath` 沒有擋 `..`，`lockfile.validatePathComponent` 才有）——已加上 `archive.Contained` 防護（沿用 install.go frozen registry 解壓路徑既有的相同模式）。三項皆補上回歸測試，含一個刻意示範路徑跳脫攻擊的測試（`TestRunUpdate_RefusesVirtualPathEscapingApmModules`）。
- [x] **第 2 輪 codex review 追加發現 1 項（更精確的同類缺陷）**：`archive.Contained` 只擋「跳到 apm_modules 之外」，擋不住「`..` 解析後仍落在 apm_modules 內、但是別的目錄」這種情況（例如 `path: ".."` 配 `acme/a` 會清理成 `apm_modules/acme`，刪掉整個 owner 命名空間下的其他套件，而非只刪 `acme/a` 自己）——已加上 `keyHasParentSegment` 在 `filepath.Join`/`Contained` 之前就直接擋下任何含 `..` segment 的 key，並補上 `TestRunUpdate_RefusesParentSegmentStayingInsideApmModules` 精確重現此情境（斷言 sibling 套件目錄不會被誤刪）。同輪也補齊先前 coverage gap：CI 自動 frozen 測試、`--no-frozen` override 測試、真實 git 的「tag 未變仍強制重下載」端對端測試（`TestRunUpdate_RealGitSemver_UnchangedTagStillRecloned`）。
- 驗證：`go test ./cmd/apm/... -run TestRunUpdate` PASS（12 個測試，含 3 個真實 git 端對端）；`go test ./... -count=1` 全綠；`go vet ./...` / `gofmt -l` 皆乾淨；`git status conformance/` 確認唯讀 oracle 未被觸碰。
- **review gate A：通過**（2 輪 codex exec 唯讀審查，全部發現皆已修正並補測試，第 2 輪確認無殘留問題）。req-rs-012「其餘維持原 pin」已在 `TestRunUpdate_Scoped_OnlyNamedPackageChanges` 明確斷言（非僅檢查目標套件）。

### 步驟 Final — 全域驗證

- [x] `go build ./...`、`go vet ./...`、`go test ./... -count=1 -cover`（`-race` 沿用本 session 已知的 Windows 環境限制，不強求）— 全綠，見下方覆蓋率。
- [x] `git status` 確認未修改任何 `conformance/conformance-kit/oracle/**` 檔案 — 確認乾淨。
- [x] A/B/C 三組整體交互已在各自 review gate 內確認（B 的 `LoadPackage`/`checkoutMatchesRef` 修正對 update 路徑同樣生效，`TestRunUpdate_RealGitSemver_UnchangedTagStillRecloned` 即為交互驗證）。
- [x] 更新本檔勾選狀態 + 每步驟的 review 結論摘要。

## Review gates
- **B**（步驟 B 後）：通過。gitops 下載短路修正正確性，3 輪 codex + 1 輪 advisor。
- **C**（步驟 C 後）：通過（C.1/C.2）。target 偵測三項修正 + oracle 未被破壞。C.3 暫緩。
- **A**（步驟 A 後）：通過。update 指令完整流程 + install.go 重構無回歸，2 輪 codex。
- **Final**：通過。三組整體 + 全域測試皆綠，oracle 未被觸碰。

## Rollback points
三組工作互相獨立，任一組出問題可單獨 revert 對應檔案而不影響其他兩組（A 依賴 B 的 `LoadPackage` 修正建議順序 B→C→A，但 A 本身不強制依賴 C）。
