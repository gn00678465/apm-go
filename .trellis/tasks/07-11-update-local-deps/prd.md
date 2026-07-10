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

- [ ] Python oracle 行為查證記錄（update × local dep × 零 target 三情境）
- [ ] 修復路徑：TDD 先紅後綠；update 後 local dep 部署結果與 install 一致
      （或 documented deviation 落檔）
- [ ] 全 repo `go build/vet/test ./...` 綠；相關 A/B 腳本重跑無回歸
- [ ] spec `backend/install-marketplace-contracts.md` §4 Warning 更新為
      已修（含 commit）或決策記錄

## Non-Goals

- 不改 update 的 git dep 更新語意。
- 不處理 `apm update` 的其他 parity 面（僅 local deps + 零 target 閘門）。
