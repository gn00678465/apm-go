# 存活 local root key 空間不一致修復

## Goal

修復 `uninstallRemainingRootKeys` 對「存活的 local root」仍輸出 `local:` 空間
key 的問題，使其與 reachability BFS / stale-MCP 檢查使用的 `_local/<base>-<sha8>`
空間一致。

## 背景（見 archive/2026-07/07-05-antigravity-research/prd.md「新 follow-up」段；spec backend/install-marketplace-contracts.md §4）

- uninstall 修復 ag-23/ag-25（commit `171fd87`）時，`uninstallRemovalKey` 已做
  key 空間翻譯（`local:<path>` → `_local/<base>-<sha8>`），但只翻譯了**被移除**
  的 root；**存活**的 local root 走 `uninstallRemainingRootKeys`，仍在 `local:`
  空間。
- 影響：存活 local dep 的傳遞依賴可能不被 reachability BFS 保護（誤刪風險）、
  其 MCP 可能被 stale-MCP 檢查誤判 stale。
- 修法已知：同 `171fd87` 款的一行 key 空間翻譯。

## Requirements

- `uninstallRemainingRootKeys` 對 local root 輸出 `_local/<base>-<sha8>` key，
  與 reachability / stale-MCP 檢查的 key 空間對齊。
- 先寫失敗測試重現（TDD）：安裝 local dep A（帶傳遞依賴/MCP）+ git dep B，
  uninstall B，驗證 A 的傳遞依賴不被清、A 的 MCP 不被判 stale。
- 既有 uninstall 測試與 ab_uninstall.py 重跑無回歸。

## Acceptance Criteria

- [ ] 重現測試先紅後綠（存活 local root 的傳遞依賴受 reachability 保護、
      MCP 不誤判 stale）
- [ ] 全 repo `go build/vet/test ./...` 綠；ab_uninstall.py 重跑無回歸
- [ ] spec `backend/install-marketplace-contracts.md` 的 follow-up 註記改為
      已修（含 commit）

## Non-Goals

- 不動 `uninstallRemovalKey` 已修好的路徑。
- 不重構 uninstall 其他 key 處理。

## Notes

- 輕量任務：PRD-only 即可，不需 design.md / implement.md。
