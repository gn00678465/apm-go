# apm-go

[English](README.md) | 繁體中文

> [microsoft/apm](https://github.com/microsoft/apm)(Agent Package Manager)的 Go 重新實作 — 單一靜態二進位,無 Python 執行期相依。

## 這是什麼

APM 是「AI 原生開發」的套件管理器:把分散的 `.apm/` primitive(指令、chat mode、記憶、憲章)編譯成各 AI 代理平台啟動時讀取的根 context 檔案(`AGENTS.md`、`CLAUDE.md`、`GEMINI.md` 等),並安裝/部署套件與 MCP 伺服器設定。

apm-go 以 Go 重新實作上游 `apm` 的常用指令面。二進位刻意命名為 `apm-go`(Windows 為 `apm-go.exe`),可與原版 `apm` 並存以便對照比較。

## 安裝

預編譯二進位發佈於 [GitHub Releases](https://github.com/gn00678465/apm-go/releases),涵蓋 Windows / Linux / macOS(amd64 / arm64)。安裝腳本會下載對應平台的二進位、驗證 SHA256 checksum,並加入 PATH。

### Windows(PowerShell)

```powershell
irm https://raw.githubusercontent.com/gn00678465/apm-go/main/install.ps1 | iex
```

安裝到 `%LOCALAPPDATA%\apm-go` 並加入使用者 PATH。指定版本:

```powershell
$env:APM_GO_VERSION = "0.2.1"; irm https://raw.githubusercontent.com/gn00678465/apm-go/main/install.ps1 | iex
```

### Linux / macOS

```sh
curl -fsSL https://raw.githubusercontent.com/gn00678465/apm-go/main/install.sh | sh
```

安裝到 `~/.local/bin`(若該目錄不在 PATH,會附加至 `~/.profile`)。指定版本:

```sh
curl -fsSL https://raw.githubusercontent.com/gn00678465/apm-go/main/install.sh | APM_GO_VERSION=0.2.1 sh
```

開新終端機執行 `apm-go --version` 驗證。

### 移除

```powershell
# Windows
irm https://raw.githubusercontent.com/gn00678465/apm-go/main/uninstall.ps1 | iex
```

```sh
# Linux / macOS
curl -fsSL https://raw.githubusercontent.com/gn00678465/apm-go/main/uninstall.sh | sh
```

### 從原始碼建置

需要 [Go](https://go.dev/dl/) 1.26+:

```sh
go build -o bin/apm-go ./cmd/apm-go      # Windows 為 bin/apm-go.exe
go run ./cmd/apm-go <args>               # 直接執行
```

Release 尺寸建置(去除除錯資訊與路徑,約小 29% — 與 release workflow 同旗標):

```sh
go build -trimpath -ldflags "-s -w" -o bin/apm-go ./cmd/apm-go
```

## 快速開始

```sh
apm-go init                  # 初始化 APM 專案(建立 apm.yml)
apm-go install               # 依 apm.yml 安裝相依
apm-go compile               # 將已安裝的 instructions 編譯為 AGENTS.md
```

## 指令

| 指令 | 說明 |
|---|---|
| `init` | 初始化新的 APM 專案 |
| `install` | 依 `apm.yml` 或 URL/shorthand 安裝相依;`--mcp` 可新增 MCP 伺服器 |
| `uninstall` | 移除 APM 套件、其整合檔案與 `apm.yml` 條目 |
| `update` | 重新解析相依至最新符合版本 |
| `compile` | 將已安裝的 instructions 編譯為專案 `AGENTS.md` |
| `audit` | 依 `apm.lock.yaml` 重新驗證已部署檔案完整性 |
| `marketplace` | 管理 marketplace 來源(add/list/browse/update/remove/validate) |
| `pack` | 從 `apm.yml` 產出 `marketplace.json`、plugin bundle 或獨立 `plugin.json` |
| `validate` | 以 OpenAPM 安全子集與 manifest schema 驗證 YAML 檔 |
| `normalize` | 解析並重新輸出 YAML 檔(round-trip) |
| `experimental` | 管理實驗性功能旗標 |

各指令詳細旗標見 `apm-go <command> --help`。

## 開發

```sh
go build ./...        # 編譯全部套件
go test ./...         # 執行所有測試
go test ./... -cover  # 含覆蓋率(目標 ≥ 80%)
go vet ./...          # 靜態檢查
go fmt ./...          # 格式化
```

Release 已自動化:推 `v*` tag 觸發 [release workflow](.github/workflows/release.yml) — 守門比對 tag 與 `internal/version`、以 `CGO_ENABLED=0` 交叉編譯 6 平台二進位、產生 `SHA256SUMS`、發佈 GitHub Release。
