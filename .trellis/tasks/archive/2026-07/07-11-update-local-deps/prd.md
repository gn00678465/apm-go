# apm update 不 materialize local deps（F1 gap）

## Goal

定案並修復 `apm update` 對 local path deps 的行為：目前 `cmd/apm/update.go`
`runUpdate` 不呼叫 `normalizeLocalDep`，local deps 在 update 時不會
copy-materialize（fails safe：什麼都不部署），與 `install` 行為不一致。

## 背景（見 spec backend/install-marketplace-contracts.md §4 Warning；archive/2026-07/07-05-runtime-parity-gaps/prd.md follow-up 4）

- install 契約（F1）：local dep（相對/絕對路徑）以 **COPY** materialize 到
  `apm_modules/_local/<sanitizedBase>-<sha8>/`，never git clone；入口是
  `normalizeLocalDep` 設 `LocalSourcePath` → `gitops.LoadPackage` 走
  `materializeLocalCopy`。
- update 缺口：`runUpdate` 不走 `normalizeLocalDep`，local dep 無
  `LocalSourcePath`，不會 materialize。fails safe（沒部署東西）但與 install
  不一致。
- **Scope question（先定案）**：`apm update` 是否本來就該處理 local deps？
  以 Python 原版 `uv run apm update` 行為為 oracle 查證；若 Python 也不處理，
  記 documented deviation / parity-by-accident 即可。
- **同族問題**（07-11-instructions-applyto-parity prd follow-up 3）：`update`
  不走「零 target 閘門」（manifest 有 deps 但零 target 時 install 會擋，
  update 靜默過）。本 task 一併定案處置。

## Requirements

- 先 A/B 查證 Python `apm update` 對 local dep 的實際行為（materialize？
  部署？零 target 閘門？），把定案寫回本 PRD。
- 若修：update 對 local deps 與 install 走同一條 `normalizeLocalDep` →
  `materializeLocalCopy` 路徑，安全不變式不得弱化（`archive.ContainedKey`
  guard、symlink 拒絕、`copyTreeNoSymlinks`）。
- 若不修：documented deviation 記錄於 spec §4，移除 Warning 改為決策記錄。
- 零 target 閘門同族問題同規則處置（修 or 記錄）。
- 既有 update/install 測試與 ab_mcp_install_parity.py 等重跑無回歸。

## Acceptance Criteria

- [x] Python oracle 行為查證記錄（update × local dep × 零 target 三情境）
      —見 `research/findings.md`（2026-07-11 live A/B，矩陣 §C P1–P3b/G1–G3）
- [x] 修復路徑：TDD 先紅後綠；update 後 local dep 部署結果與 install 一致
      （或 documented deviation 落檔）
- [x] 全 repo `go build/vet/test ./...` 綠；相關 A/B 腳本重跑無回歸
- [x] spec `backend/install-marketplace-contracts.md` §4 Warning 更新為
      已修（含 commit）或決策記錄

## Python oracle 行為查證記錄（定案）

見 `research/findings.md` 全文（2026-07-11 live A/B + 兩邊源碼對照，含本輪
research agent 的二次獨立驗證，證據見 design.md §2 C2「排序修正」與 §6）。
定案摘要：

1. **Scope 定案：修 parity，非 documented deviation。** Python `apm update`
   與 `apm install` 共用同一條 install pipeline
   (`update.py:530-541` → `_install_apm_dependencies(update_refs=True, ...)`)，
   local dep 會被 materialize 到 `apm_modules/_local/<name>/` 且部署
   （findings P1）。
2. **apm-go 修復前的現況比 spec Warning 描述的「fails safe」更糟**：
   `apm update` 會把既有 `_local/...` lockfile 條目（含
   `deployed_files`/`deployed_file_hashes`）破壞性改寫成裸
   `repo_url: ./x, source: local` 條目（findings G2），且靜默 exit 0。
3. **零 target 閘門**：Python update 有（承繼 install pipeline），但被
   plan gate 前置——plan 有變更時 exit 2「No harness detected」（findings
   P3）；plan 無變更時在 plan gate 就先 exit 0，閘門不觸發（findings P3b）。
   apm-go 無 plan gate，故採 unconditional 版本（deps 存在 + 零 target 一律
   exit 2），比 Python 嚴，記 deviation D3（design.md §4）。
4. **修復機制**：`runUpdate` 加入與 `install.go:306-311` 相同的
   `normalizeLocalDep` 迴圈 + 在 `deployAndFinalize` 前加零 target 閘門
   （重用 `errNoDeployTarget()`）+ scoped update positional token 的
   local-path 轉譯（mirror `uninstallRemovalKey`）。三個 Python/apm-go 行為
   差異記為 documented deviations D1（update 要求既有 lockfile）/D2（update
   每次重新 materialize+部署 local dep，冪等但非 Python 的 plan-unchanged
   短路）/D3（零 target 閘門 unconditional）——見 design.md §4 與 spec
   `backend/install-marketplace-contracts.md` 底部 deviations 表。

## 完成記錄（2026-07-11）

- 修復 commit：`105b2f6`（update.go normalize 迴圈 + 零 target 閘門 + scoped
  token 轉譯 + Update plan heading；update_local_test.go 5 測試；7 個既有
  fixture 補 target:）。
- 硬性 checklist（codex 產出 35 項）對抗性驗證兩輪：第 1 輪 CONFIRMED 15/
  FAIL 19（大宗為 checklist 自身 PowerShell helper `$Args` 遮蔽缺陷，實作側
  真缺陷 2 項：heading 未印、gofmt/CRLF）；harness 修復 + 實作補修後第 2 輪
  CONFIRMED 34/FAIL 0；ULD-25 於 commit 後補驗——最終 35/35。
- 教訓：第一輪實作 agent 的 live smoke 聲稱 heading 有印，經 codex 對抗性驗
  證證偽——佐證 checklist 逐項重驗（claim 不採信）的必要性。

## Non-Goals

- 不改 update 的 git dep 更新語意。
- 不處理 `apm update` 的其他 parity 面（僅 local deps + 零 target 閘門）。
