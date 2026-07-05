# Design: apm pack(marketplace.json 產生器)

權威清單:`../07-03-marketplace-ecosystem/marketplace-checklist.md` Phase M4(mkt-050~055,2026-07-03 修訂版)。
前置:`07-03-marketplace-authoring` 的 `AuthoringConfig`(`marketplace:` 區塊資料模型)已合入——本設計以其為輸入契約;若 authoring 的最終欄位名有出入,以合入後的 schema.go 為準更新本文件。

## 範圍界定(重要)

原版 `apm pack` 同時負責 plugin 打包(`--format plugin/apm`、`--archive` 等)與 marketplace.json 產生。**本子任務只做 marketplace.json 產生**(parent prd 的範圍決定);指令骨架保留擴充空間,但 plugin 打包不在此輪。`apm.yml` 無 `marketplace:` 區塊時,pack 印明確訊息(「無 marketplace 區塊,無事可做」)exit 0——不要為未實作的打包功能報錯。

## 套件結構

```
internal/marketplace/build/
  builder.go     # ResolvePackages(config) ([]ResolvedPackage, error) — ref/version 解析(mkt-051)
  tagpattern.go  # tagPattern 樣板解析(全新元件,見下節)
  mapper.go      # Mapper interface + ClaudeMapper / CodexMapper(mkt-050/052/053)
  output.go      # 輸出位置解析 + atomic write(mkt-054)

cmd/apm/pack.go  # pack 指令接線 + exit codes(mkt-055)
```

## tagPattern 樣板解析(`tagpattern.go`,gaps B2——全新元件,無既有對應)

apm-go 目前**沒有**任何等價於 Python `marketplace/tag_pattern.py` 的樣板邏輯(`internal/semver`/`internal/resolver` 都處理已知形狀的 tag,不處理使用者自訂樣板)。需要新寫:

```go
// BuildTagRegex 把 "v{version}" / "{name}-v{version}" 樣板轉成 regex(對齊 tag_pattern.py:71-127):
// 佔位符只有 {version} 與 {name} 兩種;{version} → 具名群組(semver 形狀,含 prerelease/build metadata),
// {name} → regexp.QuoteMeta(name);其他文字逐字 escape。
// 產出 regex 必須 ^...$ 前後錨定、大小寫敏感(原版無 IGNORECASE)。
func BuildTagRegex(pattern, name string) (*regexp.Regexp, error)
// MatchTag 回傳 tag 是否符合樣板,並抽出 version 字串
func MatchTag(rx *regexp.Regexp, tag string) (version string, ok bool)
```

原版的 `infer_tag_pattern_from_refs` 回退**只有 outdated 指令用到**(`__init__.py:1174`),`builder.py` 不用(永遠有 pattern,預設 `v{version}`)——pack 這邊不需要實作推斷回退。

check/outdated(authoring 子任務)也需要同一份樣板邏輯——放在 `internal/marketplace/build` 或抽成 `internal/marketplace/tagpattern` 由兩個子任務共用,以先實作的子任務落點為準,後做的一方**必須重用**,不得各寫一份(比照 `mcpregistry.ResolveDeployable` 抽共用的教訓)。semver 比對本身重用 `internal/semver`(`MaxSatisfying`/`Satisfies`——與 `resolver/diamond.go` 同一組底層,不重寫)。

## 解析(`builder.go`,mkt-051)

```go
type ResolvedPackage struct {
    Entry      authoring.PackageEntry
    IsLocal    bool
    Ref        string // 解析後 tag(remote);local 為空
    SHA        string // 40 字元 commit(remote);local 為空(regex 對齊原版:^[0-9a-f]{40}$,**只收小寫**,大寫落回 ref 查詢)
    Host       string // 非預設 host 時填(預設 github.com 收斂為空)——決定 url 形式輸出
    SourceRepo string // owner/repo(供 github shorthand / url 合成)
    Subdir     string
    Tags       []string // = entry.Tags(見 authoring 的 tags+keywords 合併規則)
    // 遠端 apm.yml metadata(description/version),curator 條目值優先
    RemoteDescription, RemoteVersion string
}
```

**sourceBase 明確延後(adversarial 抽查發現)**:原版另有 `sourceBase` 組合功能(`builder.py:542-553`,`source_url = {source_base}/{entry.source}`,會讓 **github.com 也輸出 url 形式**)。checklist 未涵蓋、scaffold 不產生它,本輪**不實作**——`ResolvedPackage` 不帶 `SourceURL` 欄位,url 形式輸出只由「非預設 host」觸發;A/B 測試不得使用含 sourceBase 的 fixture,延後項目清單記錄。

- 本地(`./`)套件:跳過 git,直接帶入(mkt-051)
- 遠端套件:`git ls-remote --tags --heads`(複用/擴充 `internal/gitops` 的 lister)
  - 明確 ref:40-hex SHA 直接接受;tag/branch 名比對;**branch ref 與 `HEAD` → HeadNotAllowed 錯誤**(mkt-055;不提供 allow-head 旗標,對齊原版)
  - semver range:tag 依 `tagPattern`(`{version}`/`{name}` 佔位)過濾 → `internal/semver.MaxSatisfying` 取最高相符
  - 無相符 → NoMatchingVersion 錯誤(exit 1)
- `--offline`:只用快取 refs(若 consumer 子任務未做快取層,`--offline` 直接報「無快取可用」,不静默降級)
- metadata enrichment:remote 套件抓其 repo 的 apm.yml 讀 description/version;curator(marketplace 條目)已有值時 curator 勝(mkt-050 修訂版 (c))。抓取失敗降級為「無 metadata」警告,不中斷 build

## 輸出轉換(`mapper.go`,mkt-050/052/053 修訂版;以下全部依 `output_mappers.py` **全文抽查**(gaps A1/A2)寫成,含逐欄觸發條件)

**Claude 輸出**(`ClaudeMapper.compose` 的精確演算法,對照 `output_mappers.py:53-223`):

頂層(依序):
| 欄位 | 觸發條件 |
|---|---|
| `name` | 恆有(config.name) |
| `description` | 僅當使用者**明確覆寫**(config 有非空 description 且標記 overridden)|
| `version` | 同上(version_overridden)|
| `owner` | 恆有:`name` 恆有、`email` 有值才出、`url` 有值才出(**注意 email 也會輸出**,先前設計漏了)|
| `metadata` | config.metadata 非空時整包透傳(含 pluginRoot)|
| `plugins` | 恆有 |

plugin 級(依序):
| 欄位 | 觸發條件 |
|---|---|
| `name` | 恆有 |
| `description`/`version` | **curator-wins 優先序**:curator 條目值優先(有值即用,與 metadata 不同時記 verbose 診斷);否則用 remote/本地套件 apm.yml 的 metadata;都沒有 → 省略。遠端套件的 curator `version` 只在 `_is_display_version` 為真時採用:非 `^~><=` 開頭、無空白、無 `*`、最後一段非 `x` 萬用——即 semver range **不會**進輸出 |
| `author` | curator 條目有 author dict 才出 |
| `license` | curator 條目有才出 |
| `repository` | curator 條目有才出 |
| `tags` | pkg.tags 非空才出 |
| `homepage` | **僅本地套件**且 curator 條目有才出 |
| `source` | 恆有,合成規則見下 |

`source` 合成(有先後順序,對照 `:150-201`):
1. 本地 → 相對路徑**字串** + pluginRoot 剝除:`_subtract_plugin_root` 語意——去 `./` 前綴後做 PurePosixPath relative_to;結果為空/絕對/含 `..` → BuildError;source 在 root 之外 → 警告並原樣輸出
2. 遠端有 subdir → `{"source":"git-subdir","url":<remoteURL 或 owner/repo>,"path":<subdir>}`
3. 遠端 host-prefixed(pkg 有 source_url 或非預設 host)→ `{"source":"url","url":"https://{host}/{owner/repo}"}`(GHE 等非 github.com host 必須走 URL 形式,github shorthand 只解析到 github.com)
4. 其餘 → `{"source":"github","repo":"owner/repo"}`
5. 2-4 一律追加 `ref`(有值)與 `sha`(有值)
- 另有 duplicate-name 警告(同名 plugin 提示 consumers 會看到重複條目)

**Codex 輸出**(`CodexMapper`,對照 `:226-309`——**與 Claude 差異很大,不是「基礎同 Claude + category」**):
- 頂層:`name`、**`interface: {displayName: <config.name>}`**、`plugins`(無 description/version/owner/metadata)
- plugin 級:`name`、`source`、**`policy: {installation: "AVAILABLE", authentication: "ON_INSTALL"}`**(固定值)、`category`(必填——config 載入層 + mapper 層雙重把關,mkt-053);**無** description/version/author/license/repository/tags/homepage
- Codex 的 `source`:本地 → **dict** `{"source":"local","path":<source>}`(不是字串!);遠端 subdir → git-subdir 同 Claude;其餘遠端一律 `{"source":"url","url":...}`(**沒有** github shorthand 形式);ref/sha 同樣追加

## 輸出位置(`output.go`,mkt-054)

| profile | 預設路徑 |
|---|---|
| claude | `.claude-plugin/marketplace.json` |
| codex | `.agents/plugins/marketplace.json` |

- `outputs` 含幾個就寫幾份;**不寫 repo 根目錄**
- 覆寫語法(gaps A3,已對照 `pack.py`/`yml_schema.py:675-747` 驗證,原版有**兩種** YAML 形式):
  1. `marketplace.outputs.<name>.path`(map 形式,scaffold 樣式:`outputs: {claude: {path: ...}}`)——主要形式
  2. `marketplace.claude.output` / `marketplace.codex.output`(專屬子區塊)——相容形式
  Go 版兩種都支援,map 形式優先;`APM_MARKETPLACE_<NAME>_PATH` 環境變數在原版**只宣告未實作**(profile 註解明示 planned v0.15)——Go 版**不實作**,不要看到欄位就接
- CLI:`--marketplace-path FORMAT=PATH`,**可重複**(每格式一次);FORMAT 限 known outputs(claude/codex),PATH 過 path-traversal 驗證;格式錯誤/未知 FORMAT → usage 錯誤
- atomic write;JSON 2-space indent + 尾換行(A/B byte 比對前先確認原版縮排格式)

## CLI 與 exit codes(`cmd/apm/pack.go`,mkt-055)

本輪旗標:`--offline`、`--include-prerelease`、`--dry-run`(印將寫入的路徑與 plugin 數,不寫檔)、`-m/--marketplace`(過濾輸出格式:`claude,codex`/`all`/`none`)、`--marketplace-path FORMAT=PATH`、`-v`。

Exit codes(**adversarial 抽查後修正**——實際追碼:pack 路徑的 marketplace config 錯誤(含 codex category 缺失)被包成 BuildError → ClickException → **exit 1**,不是 2;`sys.exit(2)` 只存在於已移除的 `marketplace build` 指令,pack.py 頂部 docstring 寫的 exit 2 屬 plugin 打包 schema 驗證、不在本輪):`0` 成功、`1` build 錯誤 **與** marketplace config/schema 錯誤(ls-remote 失敗/NoMatchingVersion/HeadNotAllowed/codex category 缺失/req-mf-017 違規)。`3`(`--check-versions`)與 `4`(`--check-clean`)兩個閘門**本輪不實作**——旗標不出現(不做空殼,呼應 mkt-017 的原則),checklist mkt-055 對應欄位標註「exit 3/4 延後」;若後續需要另開任務。延後閘門的語意記錄(gaps A4,對照 pack.py 旗標 help 與 Agent 抽查):`--check-versions` = 驗證各套件版本符合 `marketplace.versioning.strategy`(lockstep | tag_pattern | per_package),不符 exit 3;`--check-clean` = 把每個 output 重新產生到暫存路徑、與磁碟上檔案 diff,有差異(marketplace.json 過期未 regen)exit 4,閘門本身永不寫檔——後續實作時以此為行為基準。

## 錯誤處理

- HeadNotAllowed 錯誤訊息說明原因(branch 是可變參照)並建議 pin tag。⚠️ 原版錯誤訊息提示「pass --allow-head to override」但 pack **根本沒有這個旗標**(`errors.py:106` 的誤導字串)——Go 版錯誤訊息**不可照抄**,只建議 pin tag
- HeadNotAllowed 觸發邊界(已抽查):裸 branch 名/完整 `refs/heads/...`/`HEAD`(不分大小寫);**同名 tag 優先於 branch**(tag 先比對,同名時走 tag 不報錯)
- Codex mapper 對 `entry_by_name` 查無的 package 是靜默跳過(正常管線碰不到,但與 Claude 的降級行為不對稱)——Go 版兩個 mapper 統一為「查無 entry 即 internal error」,fail loud
- ls-remote 失敗訊息不回顯憑證(credsec 慣例)
- 單一 package 的 metadata 抓取失敗 → 警告 + 繼續;ref 解析失敗 → 整體失敗(exit 1),因為輸出會缺 sha
