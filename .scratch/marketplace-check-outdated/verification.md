# Independent verification — marketplace-check-outdated

- `scope`: marketplace-check-outdated
- `spec`: specs/marketplace-check-outdated/SPEC.md
- `cap`: two rounds（預設）
- `verdict`: pending（round 2 進行中）

## Round 1

- `source_state`: 3cff0ed0cfd208800720c8aaedacef868b5f9b35
- `spec_version`: v3
- `inputs`: 任務契約、SPEC v3、隔離 worktree `C:\Users\gn006\AppData\Local\Temp\apm-verify`、`sh tools/gate.sh -base main -scope marketplace-check-outdated`；未提供 builder 對話與 draft evidence
- `verdict`: failed

| # | Finding | Grade（builder 提議，人類裁定） | Disposition |
|---|---|---|---|
| 1 | gate 的 staticcheck 層在 Temp 路徑下的樹失敗（設定檔未被讀取；main 上同樣失敗） | behavioural（gate 無法在任意位置重現） | 使用者「授權，現在改」→ SC-D5，f300338 |
| 2 | `TestFetchGit_CleansUpTempCloneOnFailure` 等讀全域 temp 目錄計數，並行時 flaky | pre-existing，本變更之外 | evidence honest notes |
| 3 | plugin.json version 的 TrimSpace 刪除後測試仍綠（存活 mutant） | behavioural（無殺傷測試） | SC-B22，c2fbdd6，一次性突變證明 |
| 4 | 顯式 `version: ""` / `ref: ""` 未比照 oracle yml_schema.py:853-862 拒絕 | behavioural（parity 缺口） | 使用者「納入 D-c，現在修」→ SC-A8，c2fbdd6→a29a562 |
| 5 | SPEC Revisions「探測與 manifest 共用一次 fetch」描述不完全成立 | description | SPEC v4 Revisions 更正，無程式變更 |

Behavioural 修正後需以新 context 重新驗證（round 2）。

## Round 2

- `source_state`: 待填（v4 GREEN 與 evidence commit 之後）
- `verdict`: 待填
