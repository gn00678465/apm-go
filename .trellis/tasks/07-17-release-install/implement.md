# Implement — 執行計畫

分支:`feat/release-install`(已建)。每步一個原子 commit。

1. **version bump(R8)** — commit 既有的 `version.go` 0.2.1 修改
   → verify: `go build ./...` + `go run ./cmd/apm-go --version` 輸出 0.2.1
2. **`.github/workflows/release.yml`(R1-R4)**
   → verify: 本機以 `bash -n` 檢查內嵌 shell;版本守門邏輯以本機 shell 實測兩種輸入(匹配/不匹配)(AC4 本機等價);6 組 GOOS/GOARCH 本機交叉編譯全過
3. **`install.sh`(R6, R7)**
   → verify: `sh -n install.sh`;shellcheck(若可用);WSL 實跑(若可用)
4. **`install.ps1`(R5, R7)**
   → verify: PowerShell 語法解析(`[scriptblock]::Create`);負路徑實測(壞 URL)(AC5 一半)
5. **`uninstall.sh` + `uninstall.ps1`(R9, R10)** — 自參考腳本移植,一個 commit
   → verify: `sh -n`;PS 語法解析;Windows 實跑冪等測試(AC6;安裝目錄不存在時跑)
6. **PR 建立**(不合併;真實 release 驗證需 tag,待 PR 併入 main 後由使用者推 tag)
   → verify: PR CLEAN;checklist.md 全項核對
7. **(合併後)推 `v0.2.1` tag** — 使用者執行或授權後執行
   → verify: AC1(7 assets)、AC2(irm 實裝)、AC5 checksum 負路徑、AC6(uninstall 實跑)

回滾點:每步獨立 commit,單步 revert 不影響其他;workflow 檔在 tag 推出前無任何副作用。
