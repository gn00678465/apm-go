# Squad record — before-archive

- scope: marketplace-check-outdated
- cut: before-archive
- spec reviewed: specs/marketplace-check-outdated/SPEC.md v4 (approved)
- source state reviewed: HEAD 6982de4（gated state f300338）
- lenses: evidence vs git 加 verdict state（合併）、mapping honesty、ledger completeness。三個獨立唯讀 opus context；輸入為任務契約、SPEC v4、source state、lens brief。原先四個 sonnet 視角因 sonnet 額度用盡全部中斷、未產出，依使用者指示改用 opus 並合併兩個機械式 git 核對視角。
- question: evidence 對 git 是否屬實，封存這份 SPEC 是否合理？

## Findings（合併後，依嚴重度）

- [MEDIUM] .scratch/marketplace-check-outdated/evidence.md:62 — semver.go 的 `IsValid` 與 `TagInfo.Ref` 標為 pass，但 `internal/semver` 不在 `GATE_SCOPE_PKGS`，未進 changed-units 與 changed-line coverage — units.tsv 無 semver.go、coverage `files=6` — class 1 — 該列改為 unverified，並補入 Structural blind spot — status: fixed
- [MEDIUM] .scratch/marketplace-check-outdated/evidence.md:79 — SC-B20 標 pass，但測試只斷言 probe fetch 的 Env，init 與 show 兩處 `pinGitLocale` 無斷言 — 測試本體只檢查 `newProbeFetchCmd` — class 1 — 表列降為 partial，程式碼缺口揭露於 Honest notes — status: fixed
- [MEDIUM] specs/marketplace-check-outdated/SPEC.md（Failure model 最後一列）— SPEC 說 manifest 成本記入 honest notes，evidence 沒有 — grep 零命中 — class 2 — Honest notes 補 fetch 次數與未量測耗時的說明（SPEC 已承諾、非新決策）— status: fixed
- [LOW] .scratch/marketplace-check-outdated/evidence.md:67 — Stated claim 表來源寫 SPEC v3，實為 v4 — class 1 — 改為 v4 — status: fixed
- [LOW] .scratch/marketplace-check-outdated/evidence.md:12 — ordering 未列三個例外（452cfe7 的 `TagInfo.Ref`、63ad196 的 throwaway-mutants.txt、c2fbdd6 的 SC-B22 在 RED 不失敗）— git show --stat — class 1 — ordering 欄補列 — status: fixed
- [LOW] .scratch/marketplace-check-outdated/evidence.md:63 — 宣告類寫 16 列，實為 20；`optionalNonEmptyString` 無獨立列 — units.tsv 0/0 列計 20 — class 1 — 更正列數並補列 — status: fixed
- [LOW] .scratch/marketplace-check-outdated/evidence.md:41 — 測試數量 14／10 實為 13／11 — grep -c — class 1 — 更正 — status: fixed
- [LOW] .scratch/marketplace-check-outdated/evidence.md:55 — `_NoManifest（via SC-B16）` 展開後名稱不存在 — class 1 — 改為完整測試名 — status: fixed
- [LOW] .scratch/marketplace-check-outdated/evidence.md:97,164 — D-a 寫「無本地 fixture 可觸發」，與 verifier round 2 手動重現矛盾 — verification.md — class 1 — 改為「verifier 手動重現，無已提交測試」— status: fixed
- [LOW] internal/semver/semver.go:115-127 — `IsValid` 插在 `IsPrerelease` 的 doc comment 與函式之間 — godoc 歸屬錯誤 — class 1 — 移動註解（f2b2e0b，只移註解）；最終狀態重跑 gate — status: fixed
- [LOW] ARCHITECTURE.md:187 — §4 git 子程序呼叫點未列本變更新增的三處 — git grep refcheck_sha.go:161,211,290 — class 2 — 補入（fed0d84；Setup plan 已要求 ARCHITECTURE 更新，非新決策）— status: fixed
- [LOW] ARCHITECTURE.md:147 — §3.4 新段落缺 path:line 錨點 — class 2 — 補 configLoadError、CheckPackagesWith、checkPackage、fetchRefIntoScratch、showAtFetchHead、outdatedForPackage、versionTagCandidates 行號（fed0d84）— status: fixed
- [LOW] .scratch/marketplace-check-outdated/squad/after-implement.md:20 — 「evidence.md 尚未存在」標 honest notes，但 evidence 無此條 — class 3 — Honest notes 補註已隨流程消解
- [LOW] specs/marketplace-check-outdated/SPEC.md Setup plan — 排除項「subdir 感知」與已核准 SC-B17 文字上衝突；diff 中 ref 驗證與 outdated 確實不讀 subdir — class 3 — Honest notes 釐清排除範圍
- [LOW] .scratch/marketplace-check-outdated/evidence.md:76,143 — SC-D5 空清單控制只記在 verification.md；`-checks` 參數無持久化產物 — class 3 — Honest notes 揭露
- [INFO] .scratch/marketplace-check-outdated/throwaway-mutants.txt:17 — SC-B22 條目未寫明是哪一處 TrimSpace — class 3 — Honest notes 寫明
- [INFO] specs/marketplace-check-outdated/SPEC.md — GREEN commit a3ce3b1 在 v2 核准期間追加一行 Revisions，後由 v3 核准涵蓋 — class 3 — 無需處理
- [INFO] .scratch/marketplace-check-outdated/squad/before-archive.md — 紀錄檔尚不存在 — class 3 — 本檔

## 正向確認

- evidence vs git：spec_version v4 一致；gated state 之後只有 .scratch 與註解／文件變更；七組 RED→GREEN 祖先順序正確；所有引用 SHA 可解析；final verdict passed 綁 f300338，兩輪在上限內；round 1 三個 behavioural 處置指向實際改動的 commit；after-spec 早於 after-implement。
- mapping：SC-A1..A8、B1..B22、C1..C11、D1..D5 皆有 Stated claim 列與同名測試；SC-A8、B22、C3、D5 測試本體確實斷言所宣稱內容；parity 列 unverified 正確；throwaway-mutants.txt 16 個測試名皆存在。
- ledger：五項明確排除沒有被偷做；squad 紀錄 class 與 status 格式符合 spec-archive 解析；waivers.json 與 gate.sh 變更在 SPEC Revisions 與 evidence 皆有記錄。

## 讓步

- 各 lens 未重跑 gate、測試或 mutant；cmd 層測試本體未逐一讀；CI 的 git 是否帶翻譯檔未知；`build/reflister.go` 是否依賴合成 HEAD 未追完。
- 描述性修正之後的最終樹由 gate 第七輪覆蓋，verifier 未再驗證（兩輪上限已用完，差異僅為註解位置與文件）。
