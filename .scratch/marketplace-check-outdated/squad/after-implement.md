# Squad record — after-implement

- scope: marketplace-check-outdated
- cut: after-implement
- spec reviewed: specs/marketplace-check-outdated/SPEC.md v2 (approved)
- source state: a3ce3b18b6f51a2ee1419172dc3bc4922c0a1d02（gate 第四輪全綠的樹）
- lenses: contract vs implementation, live evidence, input space at code level, diff hygiene and scope（四個獨立唯讀 sonnet context；輸入為任務契約、SPEC v2、source state、lens brief）
- question: 程式碼是否做了 SPEC 所說的全部，且沒有多做？

## Findings（合併後，依嚴重度）

- [MEDIUM] internal/marketplace/authoring/refcheck_sha.go:268-269,285-289 — `isRefNotOnRemote` / `showAtFetchHead` 只比對英文 git 訊息，子程序未鎖定語系；非英文 git 語系下「not our ref」與「path does not exist」會退化為一般錯誤 — 靜態閱讀 + `gitops.SecureGitEnv` 無 `LC_ALL` — class 2 — 送審決策 D-d：三個子程序加 `LC_ALL=C`、`LANGUAGE=C`，並以 zh_TW 環境變數測試證明覆寫 — status: folded into v3
- [MEDIUM] internal/marketplace/authoring/schema.go:611-693 — 懷疑 parsePackages 把重複名稱檢查與逐欄驗證交錯，與 oracle 順序不同 — 駁回：oracle yml_schema.py:1303-1310 的迴圈同樣在每個 entry 解析後立即檢查重複，順序一致 — class 3（不成立）— status: dismissed
- [LOW] internal/marketplace/authoring/refcheck_sha.go:302-317 — `IsDisplayVersion(" ")` 回傳 true，空白 version 會觸發 manifest 比對 — class 2 — 送審決策 D-e：全空白視為非 display version — status: folded into v3
- [LOW] internal/marketplace/authoring/refcheck.go:1051-1058 — SHA 釘選加 version 且無候選 tag 時，Current 仍以設定 pattern 渲染，與 Note「No matching tags found」並存 — class 2 — 送審決策 D-f：無候選時 Current 維持 `--` — status: folded into v3
- [LOW] specs SC-B11 / SC-C9 — 未有與情境同名的測試（斷言內容齊全，只是命名） — class 1 — 改名 `TestMarketplaceCheck_Wording_MatchesOracle`、`TestMarketplaceOutdated_Wording_MatchesOracle` — status: fixed
- [INFO] internal/marketplace/authoring/refcheck.go:1130-1150 — 宣告版本的 tag 已從遠端消失時 LatestInRange 為 `--`，SPEC §C 設計文字有此規則但無測試 — class 1 — 新增 `TestOutdatedPackages_ShaPinWithVersion_DeclaredTagMissing` — status: fixed
- [MEDIUM] ARCHITECTURE.md:60 — `IsDisplayVersion` 行號 273 過時（refactor 後為 302） — class 2（文件） — 最終文件 commit 重新量測全部行號 — status: fixed
- [LOW] internal/marketplace/authoring/refcheck.go:96-99 — `newListRefsCmd` 未設 `WaitDelay` — class 3（本變更之外，SPEC Revisions 已記） — honest notes
- [LOW] .scratch/marketplace-check-outdated/evidence.md 尚未存在 — 流程順序使然（evidence 在 squad 之後產出） — class 3 — honest notes
- [INFO] RED commits 內含編譯用 stub（schema.go 的恆 false、refcheck_sha.go 的 not implemented / panic、semver.Ref 欄位） — class 3 — commit message 已自陳，GREEN commit 確有取代

## 正向確認

- contract vs implementation：每個情境的斷言具體且可證偽；偽件只落在 RefLister / CommitProber / ManifestVersionFetcher 三個邊界；`sha-match-by-name-only` 手動套用後立即被殺；七個新 mutant anchor 唯一；Must NOT 逐條核對，無斷言放寬、無測試刪除。
- live evidence：schema 四規則 exit 2、`--offline`、本地套件、`-v` 在 binary 上逐字符合 SPEC；PATH 無 git 時 `--offline` 與本地套件仍能執行。SHA 探測、manifest 比對、tip 比較無法離線在 binary 重現，只有函式層測試 — evidence 的 structural blind spot。
- diff hygiene：所有異動落在 Setup plan 範圍；RED 先於 GREEN；無舊措辭殘留；`.scratch/marketplace-check-outdated/*` 已追蹤。

## 讓步

- 各 lens 皆未跑完整 `tools/gate.sh`（由 orchestrator 的 gate 第四輪覆蓋）；未逐行對照 oracle 措辭（由 orchestrator 在 SPEC 撰寫時逐字核對）。
