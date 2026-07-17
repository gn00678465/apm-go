# PRD — GitHub Actions release + irm/bash 安裝腳本

## 背景

- 參考實作:`D:\Projects\apm-go\install.sh` / `install.ps1`(另一個 repo)為 local-build 模式,兩者皆留有 `TODO(phase 2): replace the local go build step with a GitHub release download`(install.sh:12-13、install.ps1:9-10,實際讀過)。本任務即該 phase 2,落在本 repo(`gn00678465/apm-go`)。
- 本 repo 現況:無 `.github/workflows/`、無 `install.*`(2026-07-17 `ls` 實測);`internal/version/version.go` 已有未 commit 的 `0.2.1` bump(使用者已確認併入本分支)。
- 使用者決定:平台矩陣 = win/linux/mac × amd64+arm64(6 個二進位)。

## 需求

- R1:GitHub Actions release workflow,`v*` tag push 觸發,建立 GitHub Release 並上傳 assets。
- R2:交叉編譯 6 個二進位(windows/linux/darwin × amd64/arm64),`CGO_ENABLED=0`,二進位名固定 `apm-go` / `apm-go.exe`(AGENTS.md 規定,不可用 `apm`)。
- R3:產生 `SHA256SUMS` 並隨 release 上傳。
- R4:tag 與 `internal/version.Version` 不一致時 release 失敗(fail-closed 守門)。
- R5:`install.ps1`(Windows)— `irm <raw url> | iex` 可執行;從 release 下載 windows 資產 → `%LOCALAPPDATA%\apm-go\apm-go.exe` → 加 user PATH(路徑與階段結構沿用參考腳本 install.ps1:15-16、65-84)。
- R6:`install.sh`(Linux/macOS)— `curl -fsSL <raw url> | sh` 可執行;POSIX sh;偵測 `uname -s/-m` → 下載對應資產 → `~/.local/bin/apm-go` → PATH 檢查(沿用參考腳本 install.sh:21、72-79)。
- R7:兩支安裝腳本皆驗證 SHA256(對照 release 的 `SHA256SUMS`),不符即中止。
- R8:`version.go` bump `0.2.1` 以獨立 commit 併入本分支。
- R9:`uninstall.ps1` — 移除 `%LOCALAPPDATA%\apm-go` 並自 user PATH 剔除;冪等(未安裝時安全跑)。參考腳本 uninstall.ps1:16-44 可近乎原樣移植。
- R10:`uninstall.sh` — 移除 `~/.local/bin/apm-go`;`~/.profile` 的 PATH 行不動(`~/.local/bin` 為共用目錄);冪等。參考腳本 uninstall.sh:21-28 可近乎原樣移植。

## Decisions

- D1:資產下載走 `github.com/gn00678465/apm-go/releases/latest/download/<asset>`(或指定版本的 `releases/download/v<ver>/<asset>`)— 不依賴 GitHub API/jq,腳本零額外相依。
- D2:資產命名 `apm-go-<os>-<arch>[.exe]`(raw binary,不打包 tar/zip)— 安裝腳本免解壓縮相依;成本:無壓縮(單檔 ~10-20MB 可接受)。
- D3:workflow 手寫 build 迴圈(actions/checkout + setup-go + `gh release create`),不引入 goreleaser — 理由:僅 6 資產 + checksums,無 changelog/homebrew/docker 需求;goreleaser 列為未來需求擴大時的升級路徑。

## Deferrals

- X1:release 後在 CI 內自動 e2e 驗證 install.sh(ubuntu runner 實跑安裝)— 延後。威脅模型:腳本壞掉只影響安裝體驗、不影響已裝使用者;首次 release 以人工實測補位(AC2/AC3 涵蓋首發驗證);成本:多一個 workflow job(~30 行),需求出現(安裝腳本頻繁改動)再加。
- ~~X2:uninstall 腳本~~ — 已由使用者追加為 R9/R10,不再是 Deferral。

## 驗收標準(AC)

- AC1:推 `v0.2.1` tag 後,GitHub Release 存在且含 7 個 assets(6 二進位 + SHA256SUMS)。證據:release 頁面 asset 清單。
- AC2:本機 Windows 實跑 `irm .../install.ps1 | iex`,結束後新 shell `apm-go --version` 輸出 `0.2.1`。證據:命令輸出。
- AC3:`install.sh` 通過 `sh -n`(語法)+ shellcheck(若可用);若本機有 WSL 則實跑驗證。證據:命令輸出。
- AC4:tag 與 version.go 不匹配時 workflow 失敗於版本守門步驟。證據:Actions run 失敗紀錄,或該守門步驟腳本邏輯之本機等價實測。
- AC5:兩腳本在下載或 checksum 失敗時以非零退出並輸出明確錯誤(負路徑)。證據:人為改壞 URL/checksum 的實測輸出。
- AC6:Windows 實跑 `uninstall.ps1` 後 `%LOCALAPPDATA%\apm-go` 不存在且 user PATH 無該項;連跑第二次不報錯(冪等)。證據:命令輸出。
- AC7:`uninstall.sh` 通過 `sh -n`;WSL(若可用)實跑移除 + 冪等重跑。證據:命令輸出。
