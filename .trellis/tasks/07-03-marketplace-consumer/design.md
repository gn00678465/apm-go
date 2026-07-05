# Design: marketplace 消費端指令

## 套件結構

新增 `internal/marketplace`(資料模型、登錄檔、fetch client),`cmd/apm/marketplace.go`(CLI 接線)。

```
internal/marketplace/
  models.go      # MarketplaceSource, MarketplacePlugin, MarketplaceManifest
  source.go      # ParseMarketplaceSource(raw, host string) (*MarketplaceSource, error)
  registry.go    # ~/.apm/marketplaces.json CRUD (atomic read/write)
  client.go      # Fetch(ctx, *MarketplaceSource) (*MarketplaceManifest, error) — dispatch by Kind()
  client_github.go / client_gitlab.go / client_git.go / client_local.go / client_url.go
  validator.go   # Validate(*MarketplaceManifest) []Finding

cmd/apm/marketplace.go   # marketplaceCmd(): add/list/browse/update/remove/validate
```

不重用 `internal/gitops.RealPackageLoader.LoadPackage`(耦合在 apm_modules 依賴安裝流程,語意是「裝一個套件」不是「讀一個 marketplace.json」)。`client_git.go` 自己用 shallow clone 到暫存目錄、讀檔、清理,模式類似 `internal/gitops/clone.go` 但獨立實作,不共用那個函式(避免耦合兩個語意不同的操作)。

## 資料模型

```go
package marketplace

type SourceKind string

const (
    KindLocal  SourceKind = "local"
    KindURL    SourceKind = "url"
    KindGitHub SourceKind = "github"
    KindGitLab SourceKind = "gitlab"
    KindGit    SourceKind = "git"
)

type MarketplaceSource struct {
    Name  string
    URL   string // canonical URL or local path
    Ref   string // default "main"
    Path  string // manifest path within source, default "marketplace.json"
    Owner string
    Repo  string
    Host  string // default "github.com"
}

func (s *MarketplaceSource) Kind() SourceKind { ... }

type MarketplacePlugin struct {
    Name        string
    Source      any // string (relative path) | map[string]any ({type, ...})
    Description string
    Version     string
    Tags        []string
}

type MarketplaceManifest struct {
    Name      string
    Owner     string
    Plugins   []MarketplacePlugin
    SourceURL    string // provenance only, empty for local
    SourceDigest string // sha256, provenance only
}
```

`MarketplacePlugin.Registry` 欄位**不做路由**,但 manifest 解析必須**容忍** `registry` 鍵(遇到時忽略值、不報錯,加一個含 `registry` 鍵的 fixture 測試佐證;mkt-005 修訂版——原版已出貨解析層,只有路由未實作)。

## SOURCE 判別(`ParseMarketplaceSource`,mkt-010)

判別順序(第一個符合的規則勝出,不是「最像的」而是「第一條命中的」,對齊原版 `_parse_marketplace_source`):

1. 本地路徑形式:以 `/`、`./`、`../`、`~/`、`file://` 開頭,或符合 Windows 磁碟機代號(`^[A-Za-z]:[\\/]`)→ `KindLocal`,`URL` 存絕對路徑
2. 裸 `http://` → 明確拒絕,錯誤訊息指名「不支援 http://,請用 https:// 或省略 scheme」
3. SCP 式 SSH(`^[^/]+@[^:]+:.+`)→ `KindGit`(或依 host 判斷 github/gitlab 家族細分為對應 Kind,細節見 client 分派)
4. 完整 `https://` URL → 若路徑(去尾斜線後)以 `/marketplace.json` 結尾視為直接指向 manifest 檔(`KindURL`;對齊原版 `url_names_remote_manifest`,**不是**任何 `.json` 都算);否則依 host 判斷 github.com→`KindGitHub`、gitlab.com 或含 "gitlab"→`KindGitLab`、其餘→`KindGit`
5. `OWNER/REPO` 或 `HOST/OWNER/REPO` 簡寫 → 依 `--host`(預設 `github.com`)或路徑中的 HOST 判斷 Kind

`--host` 行為分三種(mkt-011 修訂版):與規則 4 完整 URL 的 host **衝突** → **硬錯誤 exit 1**(含 marketplace.json 直連 URL);與 URL host 相符、或規則 1(本地)、或規則 3 SCP host 不符 → 忽略並印警告;其餘(簡寫)→ 生效。

`add` 另支援 SOURCE 的 `#ref` fragment(與 `--ref` 同給時硬錯誤)、未 pin ref 的 https git URL 警告、`--name` 未給時 alias 回退 manifest.name(需通過 alias 格式檢查,不合法時警告並退回 repo 名)(mkt-018)。`marketplace build` 子指令保留墓碑:呼叫時硬錯誤並指向 `apm pack`(mkt-019)。

## 登錄檔(`registry.go`,mkt-002)

```go
func RegistryPath() (string, error) // ~/.apm/marketplaces.json,目錄 0700(最佳努力,Windows 上該位元被忽略不視為錯誤)
func LoadRegistry() ([]MarketplaceSource, error) // 檔案不存在視為空清單,不報錯
func SaveRegistry(sources []MarketplaceSource) error // atomic:temp file + os.Rename
func FindByName(name string) (*MarketplaceSource, error) // 名稱比對不分大小寫(mkt-006)
func AddSource(s MarketplaceSource) error // 同名(不分大小寫)靜默取代既有項目,對齊原版(mkt-006);不報錯不確認
func RemoveSource(name string) error // 不分大小寫;名稱不存在 → 報錯
```

**AC3 對應**:`SaveRegistry` 測試必須包含至少一個「登錄檔已有其他無關 marketplace 項目」的 fixture,驗證 add/remove 只動目標項目、其餘 bytes/內容不變(不要求逐位元組相同,因為 JSON 本來就沒有 apm.yml 那種手動排版問題,但語意上「其他項目一個欄位都不能變」)。

## Fetch dispatch(`client.go`,mkt-023)

```go
func Fetch(ctx context.Context, s *MarketplaceSource) (*MarketplaceManifest, error) {
    switch s.Kind() {
    case KindGitHub: return fetchGitHub(ctx, s)   // Contents API + Accept: application/vnd.github.v3.raw(回應=原始檔案內容,**無 base64**)
    case KindGitLab: return fetchGitLab(ctx, s)   // REST v4 /projects/{urlenc(owner/repo)}/repository/files/{urlenc(path)}/raw?ref=(純文字回應)
    case KindGit:    return fetchGit(ctx, s)      // shallow clone 暫存目錄、讀檔、清理
    case KindLocal:  return fetchLocal(ctx, s)    // 直接讀檔(工作樹);裸 repo git show 可延後
    case KindURL:    return fetchURL(ctx, s)      // 直接 HTTPS GET + SHA-256 digest + ETag 快取(mkt-002 provenance)
    }
}
```

探測路徑順序(mkt-003):`s.Path` 若使用者未指定,依序嘗試 `marketplace.json` → `.github/plugin/marketplace.json` → `.claude-plugin/marketplace.json`,第一個存在的用。

信任 host 判斷(mkt-011):只有 `github.com`/`*.github.com`/`gitlab.com`/自架 GitLab(host 含 "gitlab")才轉發 `GITHUB_APM_PAT`/`GITLAB_APM_PAT`(讀環境變數),其餘 host 純用 subprocess `git`、不帶 token。

API 細節(已對照原版源碼 + live curl 驗證,2026-07-03):
- GitHub:`GET {api_base}/repos/{owner}/{repo}/contents/{path}?ref={urlenc(ref)}`,header `Accept: application/vnd.github.v3.raw` + `Authorization: token <PAT>`——**raw 媒體型別回應就是檔案原始內容**(live 驗證:無此 header 才是 base64 JSON envelope),直接 `json.Unmarshal`,不做 base64 解碼;raw 型別同時避開 Contents API 對 >1MB 檔案的 base64 限制
- GitLab:`GET {api_base}/projects/{urlenc(owner/repo)}/repository/files/{urlenc(path)}/raw?ref={urlenc(ref)}`(注意 project path 與 file path 都要整段 URL-encode 含 `/`),header `PRIVATE-TOKEN: <PAT>`,回應為純文字
- 原版另有 registry-proxy 前置嘗試(`_try_proxy_fetch`,env 閘控的企業功能)——本輪**不移植**,列為已知排除

## CLI 子指令(`cmd/apm/marketplace.go`)

```go
func marketplaceCmd() *cobra.Command // "marketplace",AddCommand(add/list/browse/update/remove/validate)
```

- `add`:呼叫 `ParseMarketplaceSource` → `Fetch`(驗證可達)→ `AddSource`。`-n/--name` 預設用 repo 名稱推導。
- `list`:`LoadRegistry` → 表格輸出(Rich 風格對齊,可先用純文字表格,不強求顏色)。
- `browse NAME`:`FindByName` → 強制重新 `Fetch`(略過任何快取)→ 表格輸出 → 印 `[i] apm install <plugin-name>@{name}` 提示。
- `update [NAME]`:給定名稱只刷新一個;省略時全部刷新(逐一呼叫 Fetch,任何一個失敗記診斷、不中斷其餘)。
- `remove NAME [-y]`:無 `-y` 且為互動終端才問確認;非互動無 `-y` → exit 1。
- `validate NAME`:`FindByName` → `Fetch` → `Validate` → 印 summary,任何 error → exit 1。

## 快取策略(MVP 範圍)

原版有 ETag/digest 快取層。Go 版本第一輪先做**無快取**(每次 `browse`/`validate`/`update` 都重新 fetch),因為快取正確性(失效策略、跨平台快取目錄)本身是額外複雜度,且不影響功能正確性,只影響效能。快取列為本子任務**明確 Non-Goal**,若後續需要再開獨立任務補上(記錄在 implement.md)。`add` 首次註冊時的 fetch 結果**不**寫入本地快取檔,只用於驗證可達性。

## 錯誤處理原則

比照本專案既有慣例(`internal/mcpregistry`):
- 網路/HTTP 錯誤訊息不得回顯完整 URL 或憑證(沿用 credsec 的教訓)
- SOURCE 判別失敗、host 不支援等,錯誤訊息清楚指名违规值,但不回顯任何可能含憑證的部分
