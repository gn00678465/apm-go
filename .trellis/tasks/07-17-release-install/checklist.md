# Checklist — release-install(自 prd.md 機械推導)

## Acceptance criteria

- [ ] AC1 — 推 `v0.2.1` 後 release 含 7 assets(6 二進位 + SHA256SUMS)· evidence: release asset 清單輸出
- [ ] AC2 — Windows `irm | iex` 實裝後新 shell `apm-go --version` = 0.2.1 · evidence: 命令輸出
- [ ] AC3 — `sh -n install.sh` 通過;shellcheck/WSL(若可用)· evidence: 命令輸出
- [ ] AC4 — tag/version 不匹配 → 守門步驟失敗 · evidence: Actions 失敗紀錄或本機等價實測輸出
- [ ] AC5 — 壞 URL / 壞 checksum → 兩腳本非零退出 + 明確錯誤 · evidence: 實測輸出
- [ ] R2 — 6 組 GOOS/GOARCH 皆 `CGO_ENABLED=0` 交叉編譯成功,輸出名 `apm-go[-*][.exe]` 非 `apm` · evidence: 本機交叉編譯輸出 + workflow 檔行號
- [ ] R8 — version bump 為獨立 commit · evidence: git log hash
- [ ] AC6 — Windows uninstall 實跑:目錄移除 + PATH 剔除 + 冪等重跑 · evidence: 命令輸出
- [ ] AC7 — `sh -n uninstall.sh` 通過;WSL 實跑(若可用)· evidence: 命令輸出

## Decisions(每項:決定仍成立的證據)

- [ ] D1 — `releases/latest/download` URL 實際可 302 到資產(首次 release 後驗證);腳本無 API/jq 依賴 · evidence: curl -I 輸出 + 腳本 grep 無 `api.github.com`
- [ ] D2 — raw binary 命名與 workflow 產物、兩腳本組 URL 三處一致 · evidence: 三處 file:line 對照
- [ ] D3 — workflow 未引入 goreleaser;總行數與複雜度可讀(<120 行)· evidence: wc -l

## Deferrals(證明延後成立,不是跳過)

- [ ] X1 — CI e2e 安裝驗證延後成立:AC2/AC3 首發人工驗證確實執行且通過(此即補位證據);威脅模型與成本已載於 PRD · evidence: AC2/AC3 勾選狀態
- ~~X2~~ — 使用者已追加 uninstall 為 R9/R10,此 Deferral 撤銷(對應 check 轉為 AC6/AC7)

## Tripwire sweep

- [ ] 本任務 artifacts 中的 延後/範圍外/不影響 等詞均帶證據三件套(PRD Deferrals 段落逐條核對)
