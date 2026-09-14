# Independent verification — marketplace-check-outdated

- `scope`: marketplace-check-outdated
- `spec`: specs/marketplace-check-outdated/SPEC.md
- `cap`: two rounds（預設）
- `verdict`: passed（round 2）

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

- `source_state`: f300338d96281d91dd488e27e289f8e662a2d06b
- `spec_version`: v4
- `inputs`: 任務契約（含 v4 決策）、SPEC v4、全新隔離 worktree `C:\Users\gn006\AppData\Local\Temp\apm-verify2`（刻意位於使用者 Temp 路徑）、gate entry point；新 context，未提供 builder 對話、draft evidence 或 round 1 報告
- `verdict`: passed

Verifier 所做：從該 worktree 完整執行 `sh tools/gate.sh -base main -scope marketplace-check-outdated`，全部層綠（tests 26/26、vet 0、gofmt 0、staticcheck 0、shuffled 26/26、property pass、real-execution 120/120、mutation 19/19、changed-line coverage 316/316、source-state-after 等於 HEAD、exit 0）；對 D:\Projects2\apm 逐字核對 oracle 措辭與 pin 到 v0.30.0 的 diff；以真 git 重現 D-a（`uploadpack.allowAnySHA1InWant=false` 下孤立 commit 與不存在 SHA 皆回 `not our ref`）；對 SC-D5 餵缺檔與空清單兩個負向控制，皆 fail-closed；雙向核對 SC 與同名測試、SC-D4 mutants 與 ARCHITECTURE 行號。

| # | Finding | Grade（builder 提議） | Disposition |
|---|---|---|---|
| 1 | [LOW] `TestScratchGit_LocalePinnedToC` 的「真 git 在 zh_TW 下」行為半段在本機無法獨立失敗：git-for-windows 無翻譯檔，git 永遠輸出英文；對 `locale-not-pinned` mutant 的實際殺傷來自 Env 檢查半段 | description（測試證明的範圍比 SPEC 文字小） | evidence honest notes 揭露；無程式變更，不開新一輪 |

Round 2 未發現 behavioural finding，驗證結束。
