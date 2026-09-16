# Independent verification — marketplace-check-outdated-fixes

- `scope`: marketplace-check-outdated-fixes
- `spec`: specs/marketplace-check-outdated-fixes/SPEC.md
- `cap`: two rounds（預設）
- `final_verdict`: passed
- `final_verdict_source_state`: ded0995e0e523871b0676fa8ab558078d76b90f1（產品碼與測試 = fb760287f4fe8df5ed9d15ed1b2a889268e39962；round 1）。之後的變更只有 description 處置：realexec 新增一個步驟、SPEC 與 ARCHITECTURE 文字、evidence 更正；該最終狀態由 gate 第六輪覆蓋，未由 verifier 再驗證。

## Round 1

- `source_state`: ded0995（產品碼 fb76028）
- `spec_version`: v2（status revised-pending-approval；v1 approved，v2 approval not obtained）
- `inputs`: 任務契約（含使用者裁定原話）、SPEC v2、隔離 worktree（使用者 Temp 路徑下，detached ded0995）、gate entry point、draft evidence（最後才對照）；未提供 builder 對話
- `verdict`: passed（無 behavioural finding）

Verifier 所做：從隔離 worktree 完整執行 `sh tools/gate.sh -base 3d59291 -scope marketplace-check-outdated-fixes`，全部層綠且數字與 draft evidence 逐項相同（26 packages、vet 0、gofmt 0、staticcheck 0、shuffled 26、5 properties、real-execution 124/124、mutation 26/26、units 18、coverage 42/42）；以 gate 建的 binary 離線重現 D-1、D-2、D-4；讀完 22 個新測試；獨立重套 6 個 mutant 並自建 5 個 mutant（4 個被殺，唯一存活者 `TrimLeft(v)` 落在明確排除的 `vv…`）；以 importlib 載入 pin 的 `semver.py` 做 37 案例差分，只差 D-b 記錄的兩列；核對 oracle 行號（check.py、__init__.py、outdated.py、yml_schema.py）、SPEC 對 3d59291 的產品碼行號、ARCHITECTURE 新錨點；在 4378e23、7fa09ef 兩個 RED commit 重現 red.md；對 6178686 攻擊 0-byte manifest 假設（兩棵樹相同，證偽）；以 3d59291 的 binary 餵 SC-F15 兩步驟，確認非 vacuous。

| # | Finding | Grade | Disposition |
|---|---|---|---|
| 1 | [MEDIUM] failure model 最後一列（pack/doctor/package）指名的檢查不可能失敗；`pack` 對 `name: 123` 在 3d59291 成功產出、fb76028 拒絕（rc 1），無測試釘住 | description | realexec 新增 `mkt-pack-schema-name-type`（`pack --dry-run` exit 1、oracle 訊息）；SPEC failure model 與 Revisions 更正；gate 第六輪 |
| 2 | [LOW] 明確排除引用 subdir 規則為 yml_schema.py:836-838，pin 實為 842-846 | description | SPEC 更正 |
| 3 | [LOW] Must NOT「測試或 gate 連網」對 supply-chain 層的 govulncheck（讀 https://vuln.go.dev）不成立；既有事實 | description | SPEC Revisions 記為既有例外；evidence honest notes |
| 4 | [LOW] evidence 寫「`sha-commit-match-removed` 由 SC-B1 殺」，第五輪產物顯示殺手是 `TestCheckPackages_BlankVersion_NoManifestFetch`（panicProber，套件中止，SC-B1 未執行） | description | evidence 與 SPEC 更正歸因 |
| 5 | [INFO] ARCHITECTURE.md semver 列錨點三個都不對（既有） | description | 更正為 :20,79,71 |
| 6 | [INFO] cmd/apm-go/marketplace_authoring.go:475 註解引用 `__init__.py:1133-1148`，pin 起於 1139（該檔不在 diff） | description | 不改（避免擴大 diff）；記於 evidence honest notes |

Verifier 未涵蓋：parity gate（`tools/parity` 為 `//go:build unix`，需 pin 的 Oracle checkout）；真實遠端；SC-F4 在 Git for Windows 啟動器下的耗時；既有 flaky `TestFetchGit_*`；其餘 20 個 mutant 的殺手歸因。

Verifier 交人判斷（非 builder 可修）：SPEC v2 status revised-pending-approval，D-4 有使用者逐字裁定，D-5、D-6 為 orchestrator 預設且未取得核准；使用者要求不再提問。evidence 與 SPEC Approval 段已自陳此降級。

Round 1 無 behavioural finding，驗證結束。
