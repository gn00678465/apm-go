# Before-archive squad — marketplace-check-outdated-fixes

- `cut`: before-archive
- `question`: 證據對 git 為真嗎？結案有正當理由嗎？
- `source_state`: 6615b9d（產品碼 fb76028）
- `spec_version`: v2（status revised-pending-approval）
- `lenses`: evidence vs git + verdict state；mapping honesty；ledger completeness（三個 opus 唯讀 delegate，四項輸入，無對話）
- `disposition_rule`: 依使用者回饋（gate 過久、過頻），本輪只做文字處置：不新增 mutant、不新增 gate 步驟、不重跑 gate；建議新增 mutant 的項目記入 Honest notes。

## Findings

- [HIGH] specs/marketplace-check-outdated-fixes/SPEC.md:4 — status `revised-pending-approval`、v2 專屬行為（D-4／SC-F20）已在產品碼，v2 approval not obtained — evidence.md:8-11、SPEC Approval 段、d71e0a4 — class 1 — 非 builder 可修：需人類核准 v2 或明示接受降級；`spec-archive` 將 fail-closed 拒絕，照實回報 — status: fixed（處置＝照實記錄並交人裁決；本 squad 無權放行，spec-archive 拒絕即為預期結果）
- [MEDIUM] .scratch/marketplace-check-outdated-fixes/evidence.md:12 — `ordering` 例外漏列 `TestNewShowCmd_ExactCommandAndSecureEnv`（1cd635b 新增，對產品碼從未 FAIL，只對 squad 手作 mutant FAIL） — red.md「v2 follow-up」段；evidence:56 標 pass 未註 — class 1 — ordering 例外補列，SC-F16 列註明回歸並指向常設 mutant `locale-not-pinned` — status: fixed
- [MEDIUM] .scratch/marketplace-check-outdated-fixes/evidence.md:117 — Suite health 列 seed 1789576153 為第五輪；第六輪為 1789580294 — fixes-gate-run6.log:52,81 — class 2 — 改為 1789580294 — status: fixed
- [MEDIUM] .scratch/marketplace-check-outdated-fixes/evidence.md:149-167 — SPEC:171 寫「每個遠端套件至多一次 ls-remote」無計數測試「（evidence 標為未量測）」，evidence Honest notes 無此項 — SPEC.md:171 vs evidence 全文 — class 1 — Honest notes 補一行 — status: fixed
- [LOW] specs/marketplace-check-outdated-fixes/SPEC.md:26 — tag 推斷裁定原話「修，對齊 oracle (Recommended)」被截為「修，對齊 oracle」，與 D-1..D-4 的逐字記法不一致 — SPEC.md:26 vs 任務契約 — class 1 — 補回 `(Recommended)`，Revisions 記為引用更正 — status: fixed
- [LOW] .scratch/marketplace-check-outdated-fixes/evidence.md:120 — 新增步驟的檢查數寫法錯：三個步驟各 2 個檢查（rc + must_grep）共 6 個，124→126 的差是 pack 那 2 個 — realexec.log:59-64 — class 2 — 改為「3 個步驟、6 個檢查」 — status: fixed
- [LOW] .scratch/marketplace-check-outdated-fixes/evidence.md:12 — `ordering` 稱 RED 只含 `_test.go` 與 red.md、GREEN 只含產品碼與 mutants.txt／realexec.sh；1cd635b、5680959 另含 throwaway-mutants.txt，fe28b5d 另含 SPEC.md 1 行 — `git show --stat` — class 2 — 補註 — status: fixed
- [LOW] .scratch/marketplace-check-outdated-fixes/evidence.md:28,42 — 單元組成寫「3 個 file-level」，units.tsv 為 4 個 file-level、2 var、12 符號；`deleted: semver.go::IsValid` 不在 units.tsv（semver.go 只有刪除行） — units.tsv — class 2 — 改計數；IsValid 列註明非 unit — status: fixed
- [LOW] .scratch/marketplace-check-outdated-fixes/evidence.md:79 — 稱「realexec 新步驟皆 `--offline`」，`mkt-pack-schema-name-type` 是 `pack --dry-run`，無該旗標（在 LoadAuthoringConfig 即失敗，未達網路） — tools/gate/realexec.sh:242,248 — class 2 — 改寫 — status: fixed
- [LOW] .scratch/marketplace-check-outdated-fixes/evidence.md:81 — Failure model「超限後 git 未結束」只由 SC-F4（回歸）承擔，無 mutant 打在 `showAtFetchHead` 超限分支的 `cancel()` — refcheck_sha.go:303-307；mutation.log:22 — class 2 — 不新增 mutant（處置規則）；Honest notes 記為無 mutant 覆蓋 — status: fixed
- [LOW] .scratch/marketplace-check-outdated-fixes/evidence.md:60 — SC-F8 build-tag 斷言為關係式（兩邊相等），不釘絕對值；SPEC:82 原文即只要求相同，常設 mutant `leading-v-compare-not-stripped` 由它殺 — fixes_after_implement_test.go:36-39 — class 2 — Honest notes 記為已知弱點 — status: fixed
- [LOW] .scratch/marketplace-check-outdated-fixes/evidence.md:103 — `TestShowAtFetchHead_GitNotStartable_Errors` 唯一失敗證據是一次性 mutant，未進 mutants.txt；units.tsv 把 showAtFetchHead 14/14 行掛在它名下 — red.md「Gate follow-up」段 — class 2 — 不併入 mutants.txt（處置規則）；Honest notes 記錄 — status: fixed
- [LOW] .scratch/marketplace-check-outdated-fixes/red.md:50 — 稱 oracle 對 15 列全部回傳 SC-F11 期望值，但 D-b 兩列 oracle 回 True、測試期望 false — oracle_version_test.go:37-38 vs evidence:66 — class 2 — 改為「13 列取自實跑，2 列為 D-b 偏離」 — status: fixed
- [LOW] .scratch/marketplace-check-outdated-fixes/evidence.md:149-167 — after-spec squad 三項 class 3（`+build` tie-break、fetch stderr／ls-remote stdout 無上限、annotated tag SHA 不探測）只在 SPEC:168-170 明確排除，未進 Honest notes — squad/after-spec.md — class 2 — Honest notes 補一行指向 SPEC:168-170 — status: fixed
- [LOW] ARCHITECTURE.md §3.4、§4 — Setup plan（SPEC:154）只授權 §2 tagpattern 入口與錨點，diff 另改 §3.4 敘述與 §4 gitops 呼叫點行號（AGENTS.md 要求同一變更內更新事實） — `git diff 3d59291..HEAD -- ARCHITECTURE.md` — class 2 — SPEC Revisions 補一句擴大授權範圍 — status: fixed
- [LOW] .scratch/marketplace-check-outdated-fixes/evidence.md:14 — source_state 3a1cedd 而 HEAD 6615b9d 只改 evidence 本身；已於 change_set 揭露 — `git show --stat 6615b9d` — class 2 — 無須動作（證據報告自我指涉的固有落差） — status: fixed

## Verdicts

- evidence vs git：不予結案——證據對 git 為真、六輪數字與產物可逐項核對、RED 先於 GREEN、squad 紀錄與 commit 序合規；唯一實質阻礙是 SPEC 非 `approved`。
- mapping honesty：close justified——兩張映射表與 failure model 無實質 over-claim，每列點名的測試都能因所述理由失敗；八項皆敘述精確度。
- ledger completeness：帳本誠實且近乎完整（四項缺口皆為補寫）；封存不成立，因 SPEC status 非 `approved`，需人裁決。

## Synthesis

三個 lens 對「證據是否為真」一致：真。對「結案是否正當」的分歧只在 v2 核准：兩個 lens 判不予結案，一個判 close justified 但把核准列為制度性殘留。裁決：不由 squad 放行；執行 `spec-archive`，其 fail-closed 拒絕即為對人類的正式回報。

## Concessions

- 三個 lens 皆未重跑 gate、未套用 mutant、未在 RED commit 樹上重跑測試；殺傷與 RED 觀察取自 `.gate/` 產物與 red.md，無法排除產物事後被改。
- 未驗 ARCHITECTURE／oracle 行號錨點、未執行 parity gate、SC-F11 的 oracle 期望值本機無法覆核（mapping lens）。
