# Checklist — release-install(自 prd.md 機械推導)

## Acceptance criteria

- [ ] AC1 — 推 `v0.2.1` 後 release 含 7 assets(6 二進位 + SHA256SUMS)· evidence: 待首次 release
- [ ] AC2 — Windows `irm | iex` 實裝後新 shell `apm-go --version` = 0.2.1 · evidence: 待首次 release
- [x] AC3 — `sh -n install.sh` 通過(輸出 sh-n-OK);shellcheck 本機不可用;WSL 實跑至下載階段 · evidence: 2026-07-17 session 實測輸出
- [x] AC4 — act 本地真跑 workflow:tag v0.0.0 → `::error::tag v0.0.0 does not match internal/version.Version=0.2.1` + Job failed;tag v0.2.1 → `version check ok: 0.2.1` + 後續全過 · evidence: act 2026-07-18 輸出(並發現 CRLF 缺陷已修,見下)
- [x] AC5(URL 半)— 兩腳本對 404:sh=RC-NONZERO+明確錯誤;PS=exit 1+明確錯誤 · evidence: WSL/pwsh 實測輸出;checksum-mismatch 半待 release(需真資產)
- [x] R2 — 6/6 組 CGO_ENABLED=0 交叉編譯成功,產物 apm-go-<os>-<arch>[.exe] · evidence: dist-test ls 輸出;release.yml:45
- [x] R8 — version bump 獨立 commit · evidence: git log(chore(release): 版本更新至 0.2.1)
- [ ] AC6 — Windows uninstall 實跑(真安裝後):目錄移除 + PATH 剔除 · evidence: 待 release;未安裝冪等雙跑已過(run1/run2 exit=0)
- [x] AC7 — `sh -n uninstall.sh` 通過;WSL 冪等雙跑 IDEMPOTENT-OK · evidence: 2026-07-17 實測輸出

## act 本地 workflow 驗證(2026-07-18 追加)

- [x] checkout/setup-go(Go 1.26.3)/守門/6 平台編譯(36.5s)/SHA256SUMS 於 ubuntu container 全過 · evidence: act 輸出逐步 ✅
- [x] 發現並修復:守門 sed 的 `"$` 錨點在 CRLF working tree 下 extract 失敗 → 改 `[^"]*` 無錨點(fix(ci) commit);真實 GitHub checkout 為 LF 原不受影響,健壯化後兩者皆可
- [ ] Create release step:act 映像無 gh CLI(`command not found`)無法本地驗;GitHub ubuntu-latest 預裝 gh(runner-images 文件),最終證據 = AC1 首次真 release

## Decisions(每項:決定仍成立的證據)

- [ ] D1 — `releases/latest/download` URL 302(待首次 release);腳本無 API/jq 依賴已驗:grep api.github.com = 0/0 · evidence: grep -c 輸出
- [x] D2 — 命名三處一致:release.yml:45 `apm-go-$os-$arch$ext`、install.sh `$BINARY_NAME-$os-$arch`、install.ps1:28 `apm-go-windows-$arch.exe` · evidence: grep 輸出
- [x] D3 — 無 goreleaser;release.yml 共 61 行(<120)· evidence: wc -l 輸出

## Deferrals(證明延後成立,不是跳過)

- [ ] X1 — CI e2e 安裝驗證延後成立:AC2/AC3 首發人工驗證確實執行且通過(此即補位證據);威脅模型與成本已載於 PRD · evidence: AC2/AC3 勾選狀態
- ~~X2~~ — 使用者已追加 uninstall 為 R9/R10,此 Deferral 撤銷(對應 check 轉為 AC6/AC7)

## Tripwire sweep

- [ ] 本任務 artifacts 中的 延後/範圍外/不影響 等詞均帶證據三件套(PRD Deferrals 段落逐條核對)
