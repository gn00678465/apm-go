# Checklist — release-install(自 prd.md 機械推導)

## Acceptance criteria

- [x] AC1 — release v0.2.1 恰含 7 assets(6 二進位 + SHA256SUMS)· evidence: `gh release view` 清單(2026-07-18;tag 自 feat/release-install e8a6ba9 觸發,內容與待合併 main 相同)
- [x] AC2 — `irm | iex` 實裝(分支 raw URL):下載→Checksum OK→`apm-go version 0.2.1`→user PATH 加入,`%LOCALAPPDATA%\apm-go\apm-go.exe --version` 實跑 0.2.1 · evidence: pwsh 輸出
- [x] AC3 — `sh -n install.sh` 通過(輸出 sh-n-OK);shellcheck 本機不可用;WSL 實跑至下載階段 · evidence: 2026-07-17 session 實測輸出
- [x] AC4 — act 本地真跑 workflow:tag v0.0.0 → `::error::tag v0.0.0 does not match internal/version.Version=0.2.1` + Job failed;tag v0.2.1 → `version check ok: 0.2.1` + 後續全過 · evidence: act 2026-07-18 輸出(並發現 CRLF 缺陷已修,見下)
- [x] AC5 — 404 負路徑:sh=RC-NONZERO、PS=exit 1,皆明確錯誤;checksum-mismatch:對真資產竄改 SHA256SUMS,sh Stage-3 同款管線 TAMPER-REJECTED/GENUINE-ACCEPTED,PS 比對邏輯 TAMPER-REJECTED/GENUINE-ACCEPTED · evidence: 2026-07-18 實測輸出(PS 竄改測試為 Stage-3 邏輯級;e2e 正向 checksum 已由 AC2 全程覆蓋)
- [x] R2 — 6/6 組 CGO_ENABLED=0 交叉編譯成功,產物 apm-go-<os>-<arch>[.exe] · evidence: dist-test ls 輸出;release.yml:45
- [x] R8 — version bump 獨立 commit · evidence: git log(chore(release): 版本更新至 0.2.1)
- [x] AC6 — 真安裝後 `irm uninstall.ps1 | iex`:dir-REMOVED-OK + PATH-CLEANED-OK + 冪等重跑 exit=0;WSL `curl|sh uninstall.sh`:REMOVED-OK + IDEMPOTENT-OK · evidence: 2026-07-18 實測輸出
- [x] AC7 — `sh -n uninstall.sh` 通過;WSL 冪等雙跑 IDEMPOTENT-OK · evidence: 2026-07-17 實測輸出

## act 本地 workflow 驗證(2026-07-18 追加)

- [x] checkout/setup-go(Go 1.26.3)/守門/6 平台編譯(36.5s)/SHA256SUMS 於 ubuntu container 全過 · evidence: act 輸出逐步 ✅
- [x] 發現並修復:守門 sed 的 `"$` 錨點在 CRLF working tree 下 extract 失敗 → 改 `[^"]*` 無錨點(fix(ci) commit);真實 GitHub checkout 為 LF 原不受影響,健壯化後兩者皆可
- [x] Create release step:act 映像無 gh CLI 無法本地驗 → 已由真 Actions run 29616001903 全綠(含 Create release ✓)關閉 · evidence: gh run watch 輸出

## Decisions(每項:決定仍成立的證據)

- [x] D1 — `releases/latest/download/apm-go-linux-amd64` 實測 `HTTP/1.1 302 Found`;腳本無 API/jq 依賴(grep api.github.com = 0/0)· evidence: curl -sI + grep -c 輸出
- [x] D2 — 命名三處一致:release.yml:45 `apm-go-$os-$arch$ext`、install.sh `$BINARY_NAME-$os-$arch`、install.ps1:28 `apm-go-windows-$arch.exe` · evidence: grep 輸出
- [x] D3 — 無 goreleaser;release.yml 共 61 行(<120)· evidence: wc -l 輸出

## Deferrals(證明延後成立,不是跳過)

- [x] X1 — CI e2e 安裝驗證延後成立:AC2(Windows irm 實裝)與 install.sh WSL 實裝均已執行且通過,首發人工補位證據到位;威脅模型與成本載於 PRD · evidence: 上方 AC2/AC6 勾選與輸出
- ~~X2~~ — 使用者已追加 uninstall 為 R9/R10,此 Deferral 撤銷(對應 check 轉為 AC6/AC7)

## Tripwire sweep

- [x] 本任務 artifacts 絆線詞核對:PRD X1(延後)帶威脅模型+成本+補位證據;X2 已撤銷改為 R9/R10;無其他無據收斂詞 · evidence: prd.md Deferrals 段落逐條
