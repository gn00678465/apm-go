# Design — GitHub Actions release + 安裝腳本

## 1. Release workflow(`.github/workflows/release.yml`)

- 觸發:`on: push: tags: ['v*']`
- 單一 job(ubuntu-latest),步驟:
  1. `actions/checkout@v4`
  2. `actions/setup-go@v5`(`go-version-file: go.mod`,Go 1.26.3)
  3. **版本守門(R4)**:比對 `${GITHUB_REF_NAME#v}` 與 `internal/version/version.go` 內 `Version = "..."`(grep 抽取);不一致 → `exit 1`,訊息列出兩值。
  4. build 迴圈:對 `windows linux darwin` × `amd64 arm64`,`CGO_ENABLED=0 GOOS=$os GOARCH=$arch go build -trimpath -ldflags "-s -w" -o dist/apm-go-$os-$arch$ext ./cmd/apm-go`
  5. `(cd dist && sha256sum * > SHA256SUMS)`
  6. `gh release create "$GITHub_REF_NAME" dist/* --title ... --generate-notes`(`GH_TOKEN: ${{ secrets.GITHUB_TOKEN }}`)
- permissions:`contents: write`(建 release 所需,最小權限)。

### 資產命名(D2)

`apm-go-windows-amd64.exe` · `apm-go-windows-arm64.exe` · `apm-go-linux-amd64` · `apm-go-linux-arm64` · `apm-go-darwin-amd64` · `apm-go-darwin-arm64` · `SHA256SUMS`

## 2. `install.ps1`(repo 根目錄)

契約:

- 執行形式:`irm https://raw.githubusercontent.com/gn00678465/apm-go/main/install.ps1 | iex`(無參數,裝 latest);進階:下載後 `.\install.ps1 -Version 0.2.1`。
- 參數:`-Version <semver>`(可選,預設 latest)。`irm | iex` 形式下亦可用 `$env:APM_GO_VERSION` 指定(iex 無法傳參)。
- 階段(沿用參考腳本結構,build 換成 download):
  1. 偵測 arch:`$env:PROCESSOR_ARCHITECTURE`(`ARM64` → arm64,其餘 amd64)
  2. 組 URL:latest → `releases/latest/download/<asset>`;指定版 → `releases/download/v<ver>/<asset>`
  3. `Invoke-WebRequest` 下載 asset + `SHA256SUMS` 到 temp
  4. checksum:`Get-FileHash -Algorithm SHA256` 對照 SHA256SUMS 內對應行;不符 → 錯誤退出(R7)
  5. 測試:`& $tmp\apm-go.exe --version`
  6. 安裝:`%LOCALAPPDATA%\apm-go\apm-go.exe`;user PATH 加入(同參考腳本 72-84 邏輯)
  7. finally 清 temp
- 錯誤處理:`$ErrorActionPreference = "Stop"`;每階段失敗訊息明確(AC5)。

## 3. `install.sh`(repo 根目錄)

契約:

- 執行形式:`curl -fsSL https://raw.githubusercontent.com/gn00678465/apm-go/main/install.sh | sh`;指定版本:`... | APM_GO_VERSION=0.2.1 sh`(env 傳遞,pipe 下無 argv)。
- POSIX sh(`#!/bin/sh` + `set -e`),無 bash-ism。
- 階段:
  1. 偵測 `uname -s`(Linux/Darwin;其他 → 錯誤)與 `uname -m`(x86_64→amd64、aarch64/arm64→arm64;其他 → 錯誤)
  2. 組 URL(同 PS 邏輯)
  3. `curl -fsSL` 下載 asset + SHA256SUMS 到 `mktemp -d`(trap 清理)
  4. checksum:`sha256sum -c`(Linux)/ `shasum -a 256 -c`(macOS fallback);皆無 → 警告並中止(fail-closed)
  5. 測試 `--version`
  6. 安裝到 `~/.local/bin/apm-go` + `chmod +x`;PATH 檢查與 `~/.profile` 追加(同參考腳本 72-79)
- 依賴僅 curl + sha256sum/shasum + 標準 coreutils。

## 4. `uninstall.ps1` / `uninstall.sh`(repo 根目錄)

- 與下載模式無關,自參考腳本近乎原樣移植(路徑相同):
  - PS:移除 `%LOCALAPPDATA%\apm-go` 目錄 + user PATH 剔除 + 當前 session PATH 剔除;冪等(uninstall.ps1:16-44)。
  - sh:`rm -f ~/.local/bin/apm-go`;`~/.profile` 不動(共用目錄);冪等(uninstall.sh:21-28)。

## 5. 相容性/安全備註

- checksum 與二進位同源(同一 release)— 防的是傳輸損壞與 CDN 竄改單一資產,不防整個 release 被替換(那需要簽章,範圍外,參 PRD X1 同級)。
- 腳本內 repo slug 硬編 `gn00678465/apm-go`(單一 repo 安裝器,無需可配置)。
