# Design: install <plugin>@<marketplace> 解析與部署整合

權威清單:`../07-03-marketplace-ecosystem/marketplace-checklist.md` Phase M2(mkt-020~035)。
前置:`07-03-marketplace-consumer` 的 `internal/marketplace`(登錄檔 + Fetch)已可用。

## 整體資料流

```
CLI: apm install pkg@mkt#ref
  └─ cmd/apm/install.go: 逐一檢查 packages[] → marketplace.ParseRef() 命中?
       ├─ 否 → 既有 manifest.ParseDepString() 路徑(不動)
       └─ 是 → marketplace.ResolvePlugin(plugin, mkt, ref)
                 ├─ registry.FindByName(mkt)   ← 只查 ~/.apm/marketplaces.json(mkt-022)
                 ├─ client.Fetch(source)        ← 複用 consumer 子任務
                 ├─ resolvePluginSource(...)    → canonical "owner/repo[/path][#ref]" 或結構化 git+path
                 ├─ fail-closed 閘門(mkt-028)  ← 在任何網路探測之前
                 └─ Resolution{Canonical, DepRef, Provenance, Risk}
       → 以 canonical 走既有 install 流程;provenance 進 lockfile

apm.yml: dependencies.apm 的 dict 條目 {name, marketplace, version}
  └─ internal/manifest/depref.go: ParseDepDict() → Source="marketplace" 的 DependencyReference
       └─ internal/resolver: KindMarketplace → 呼叫 marketplace.ResolvePlugin() 收斂成一般 git/local dep
```

沒有新的 primitive 型別(mkt-029):marketplace 解析完成後就是一般 git/local 依賴。

## 新增/修改的套件

```
internal/marketplace/
  ref.go        # ParseRef(s string) (plugin, mkt, ref string, ok bool, err error)  — mkt-020/021
  resolver.go   # ResolvePlugin(plugin, mkt string, opts) (*Resolution, error)      — mkt-022~028, 034, 035
  pins.go       # ref-swap pin 記錄(~/.apm/marketplace_ref_pins.json)               — mkt-034(a)
  shadow.go     # 同名 plugin shadow 偵測                                            — mkt-034(b)

internal/manifest/depref.go   # ParseDepDict 加 {name, marketplace, version} 分支    — mkt-033
internal/resolver/            # KindMarketplace 從純分類標籤變成真的可解析            — mkt-029/033
internal/lockfile/types.go    # LockedDep 加 4 個 provenance 欄位                     — mkt-031
cmd/apm/install.go            # CLI 攔截 + provenance 傳遞 + mkt-032 順序修正
```

## `ParseRef`(mkt-020/021)

```go
// 規則(對齊 resolver 語意,「先切 # 再檢查」;刻意不複製原版 install 層檢查整條字串的 quirk)。
// ⚠️ adversarial 抽查修正(2026-07-03):原版是對「整條字串」跑 _MARKETPLACE_RE(fragment 為
// (?:#(.+))?$,存在 # 時 ref 至少 1 字元),head-only 重寫必須補回兩個約束,否則接受集合不同:
// 0. 先對整條輸入 strings.TrimSpace(對齊原版 s = specifier.strip())
// 1. s 以 "#" 切成 head / ref(最多一次;"pkg@mkt#a#b" → ref="a#b")
// 2. head 含 "/" 或 ":" → ok=false(fall through 一般解析);本地路徑同樣 fall through
// 3. head 需符合 ^[a-zA-Z0-9._-]+@[a-zA-Z0-9._-]+$ → (plugin, mkt)
// 4. 有 "#" 但 ref 為空(如 "pkg@mkt#")→ ok=false fall through(原版整串 regex 不匹配即回 None,
//    不是 error 也不是接受空 ref)——負向測試鎖定 "pkg@mkt#" 與 "pkg@mkt# "
// 5. ref 含 [~^<>=!] 任一字元 → error(訊息指名 semver range 不支援,請用 raw tag/branch/SHA)
```

**Deviation(記入 A/B 例外清單)**:`pkg@mkt#feature/branch` 在 Go 版可用(原版 install 層因 `"/" not in package` 的粗閘門而壞掉,uninstall 層卻可用)。

**攔截層決策(gaps A1)**:攔截邏輯**只存在一份**——`internal/marketplace.ParseRef`。CLI 層(install,以及未來的 uninstall/view)一律呼叫它判斷是否為 marketplace 參照,**不得**在 cmd 層另寫任何 `strings.Contains(pkg, "/")` 之類的前置判斷(那正是原版 install/uninstall 行為分歧的根源)。取捨:採 resolver 語意(先切 `#`)犧牲與原版 install 的逐 byte 對齊,換取 install/uninstall/view 三處永遠一致 + 修掉原版已知矛盾;A/B 測試以例外清單吸收此差異。

## `ResolvePlugin`(mkt-022~028、034、035)

```go
type Resolution struct {
    Canonical  string                       // "owner/repo[/path][#ref]" 或本地路徑
    DepRef     *manifest.DependencyReference // 結構化 git+path(非 GitHub 家族 host 的 in-marketplace 子目錄 plugin,mkt-027);否則 nil
    Provenance *Provenance                  // discovered_via / marketplace_plugin_name / source_url / source_digest
    Risk       *CrossRepoRisk               // mkt-028;非 nil 時呼叫端必須 fail-closed
}
```

步驟(對齊原版 `resolve_marketplace_plugin`):

1. `registry.FindByName(mkt)` — 只查全域登錄檔;查無 → MarketplaceNotFound。**絕不**讀專案 apm.yml 的 `marketplace:` 區塊(mkt-022,負向測試)。
2. `client.Fetch(source)` → manifest;plugin 名稱查詢不分大小寫,查無 → PluginNotFound(mkt-024)。
3. 本地 fast path(mkt-025):`Kind()==local` 且 `plugin.Source` 是字串 → 直接拼絕對路徑 canonical,return。
4. `resolvePluginSource`(mkt-026):github→`owner/repo[/path][#ref]`;git-subdir/gitlab→同形;url→重新推導;相對字串→`mktOwner/mktRepo/rel`;npm→error。
   **npm 雙層的具體 Go 邏輯(gaps A4)**:`MarketplacePlugin.Source any` 的 dict 型別判斷用一個共用函式 `coercePluginType(m map[string]any) string`,依序讀 `type`→`source`→`kind` 三鍵(取第一個非空字串,lower+trim;都沒有時依 `repo`+`subdir`/`path` 推斷 github/git-subdir)——對齊原版 `_coerce_dict_plugin_type`。**manifest 解析層**(consumer 的 models.go)只認 `type`/`source` 兩鍵判 npm 並丟棄整個 plugin(debug log);**resolve 層**用 `coercePluginType`(認三鍵),故 `kind: npm` 變體會通過 manifest 解析、在 resolve 層才報「npm 不支援」錯誤。兩層的鍵集合差異是刻意對齊原版,測試各鎖一案例。
5. 非 GitHub 家族 host + in-marketplace 子目錄 plugin → 結構化 `{git, path, ref}` DepRef(mkt-027)。
6. 註冊 ref 傳播(mkt-035):source.Ref 非 ""/main/HEAD、plugin 是相對字串 source、canonical 無 `#` → 附加 `#<source.Ref>`。
7. version_spec(來自 `#REF` 或 dict `version:`):semver range → 對 tag 解析(**重用 `internal/semver.MaxSatisfying` + `gitops.RealTagLister`**——與 `resolver/diamond.go::pickHighestInIntersection` 同一組底層工具;單一 range 情境直接用 MaxSatisfying,不 export resolver 內部的 diamond 函式,gaps B2),無相符且非嚴格 range → 回退 raw ref;否則直接 `#<spec>`(mkt-021/033)。
8. Cross-repo 閘門(mkt-028):marketplace host 是 GitHub 家族 enterprise、plugin dict source type=github、非 in-marketplace、repo 欄位是裸 `owner/repo`(URL/SCP/host 限定形式全部豁免)→ 填 `Risk`。呼叫端(install)在**任何**網路探測前拒絕。
   **enterprise 邊界(gaps A5,已對照 `github_host.py:170-196` 驗證)**:判斷式是 `isGitHubHostname(host) && host != "github.com"`,其中 isGitHubHostname = `github.com` ∪ `*.ghe.com` ∪ **`GITHUB_HOST` 環境變數設定的 GHES host**——不是只有 `.ghe.com` 後綴。Go 版的等價函式要涵蓋 GHES env 這一支,測試三案例:`corp.ghe.com` 觸發、`GITHUB_HOST=ghes.corp.io` 時 `ghes.corp.io` 觸發、`gitlab.example.com` 不觸發。
9. ref-swap pin(mkt-034a):canonical 有 `#ref` 時,以 `(mkt, plugin, plugin.Version)` 為鍵比對 pin 檔,變更 → 警告「may indicate a ref swap attack」;寫回新 pin(atomic write)。
   **儲存機制(gaps A7,已對照 `version_pins.py:1-65` 驗證)**:原版存 `~/.apm/cache/marketplace/version-pins.json`,**扁平 dict** `{"<mkt>/<plugin>/<version>": "<ref>"}`,鍵整串 lowercase,version 為空時鍵省略第三段;所有操作 **fail-open**(檔案/JSON 錯誤只 log,不阻斷解析)。Go 版對齊同一路徑與格式(未來若做快取清理,pin 與 manifest 快取同目錄一起管理)。
10. shadow 偵測(mkt-034b):逐一走訪**其他**已註冊 marketplace(排除當前者,名稱比對不分大小寫),`fetch_or_cache` 語意找同名 plugin → 警告;任何錯誤吞掉(不得中斷安裝)。
    **注意(gaps A7)**:原版靠 manifest 快取讓這一步「通常零額外網路」;Go 版 consumer MVP 沒有快取層,shadow 偵測會對每個其他登錄項做一次 live fetch——行為正確但變慢。設計決定:仍照做(語意優先),每個 fetch 沿用既有 timeout,失敗靜默;若之後補快取層自然改善。不因效能砍掉這個安全警告。

## 已知刻意行為(2026-07 adversarial 複審發現,對齊 Python,非 bug)

複審過程中發現兩個 mkt-027 鄰近行為,乍看像 bug,實為刻意的 Python parity——記錄於此避免日後被誤判成缺陷而重工:

- **B-A:pluginRoot backfill 只在其中一個 canonical builder 生效**:`resolve_plugin.go::extractInRepoPathAndRef` 的 `pluginRoot`(`metadata.pluginRoot`)回填邏輯只出現在 `case string:` 分支(plugin 用相對路徑字串宣告 source 時,`rel = root + "/" + rel`);`case map[string]any:`(dict source,`github`/`git-subdir`/`gitlab` 型別)完全不套用 `pluginRoot`。這不是漏寫——dict source 的 `path`/`subdir` 欄位語意上已經是相對 repo root 的顯式路徑,`pluginRoot` 這個「裸名稱 plugin 放在哪個子目錄下」的概念只對「裸相對路徑字串」形式的 source 有意義,對齊原版 `_extract_in_repo_path_and_ref`(resolver.py:406-460)同樣的不對稱行為。**不要**讓兩個分支對 pluginRoot 的處理「看起來一致」而改動 dict 分支。
- **B-B:`version_spec` 對已產生結構化 DepRef 的路徑會被整段略過**:`ResolvePlugin` 內 `if opts.VersionSpec != "" && depRef == nil { ... applyVersionSpec ... }`——一旦 mkt-027 已經算出結構化 `DepRef`(非 GitHub 家族 host 的 in-marketplace 子目錄 plugin),CLI 的 `#REF` 後綴或 apm.yml dict 的 `version:` 欄位會被**靜默忽略**,不報錯也不警告。這對齊原版 `if version_spec and dep_ref is None`(resolver.py 同區塊)的行為,不是遺漏。使用者對這類 plugin 指定 version_spec 目前沒有效果——是已知、刻意的行為落差,非本任務範圍,不要在這個子任務裡「順手修掉」。

## apm.yml dict 形式(mkt-033)

**接線點(gaps B1)**:`internal/manifest/depref.go::ParseDepDict`(280 行起)——仿照既有 `keys["id"]`/`keys["git"]`/`keys["path"]` 分支模式(`git: parent` 分支約 320 行可當範本)加一個 `if keys["marketplace"]` 分支,**不另寫平行解析器**。`DependencyReference` 沿用既有 `Source: "marketplace"` 分類欄位(`depref.go:26` 註解已預留),新增 `MarketplaceName`/`MarketplacePluginName`/`MarketplaceVersionSpec` 三個欄位。

**兩個資料位置不可混淆(gaps B3)**:manifest 頂層 `marketplace:` 是 authoring 區塊,其 `packages[].source` 驗證(req-mf-017)**已有現成實作**(`manifest.go:151` case "marketplace" → `validateMarketplaceBlock`,`:426`)——本子任務不碰;mkt-033 處理的是 `dependencies.apm` 清單裡的 dict 條目,是完全不同的位置與 schema。

`ParseDepDict` 新分支:條目含 `marketplace` 鍵 →
- ⚠️ **分支順序是地雷(adversarial 抽查發現)**:`depref.go:363-368` 既有 `keys["name"]` 分支會把任何含 `name` 鍵的條目直接當 `RepoURL=name` 吃掉——marketplace dict 一定含 `name`,所以 **marketplace 分支必須排在所有既有分支(含 name/git/id/path)之前**(對齊原版 `reference.py:749` 的 `"marketplace" in entry` 最先判),否則整個功能被靜默 shadow。加一個「`{name, marketplace}` 條目不會被解析成 git-literal」的負向測試鎖住順序
- 與 `git`/`path`/`registry`/`id` 併用 → error「Ambiguous dependency」(互斥檢查在 marketplace 分支內最先做)
- 允許鍵集合僅 `{name, marketplace, version}`,未知鍵 → error
- `name` **必填**且非空白,缺失時獨立錯誤訊息「Marketplace dependency must have a non-empty 'name' field」(原版 `reference.py:763-766`,先於 regex 檢查)
- name/marketplace 需符合 `^[a-zA-Z0-9._-]+$`;**只 strip、大小寫保留**(不 lowercase——大小寫不敏感發生在後續 plugin 查詢,測試鎖定)
- `version` 選填;給了就必須是非空字串,**parse 階段不做任何格式/semver 檢查**(range 合法性留到解析時,原版 `reference.py:781-785`)
- 產出 `DependencyReference{RepoURL: "_marketplace/" + mkt + "/" + name, Source: "marketplace", MarketplaceName, MarketplacePluginName, MarketplaceVersionSpec}`——**RepoURL 佔位符對齊原版**(`reference.py:787`),讓未解析的 marketplace dep 在 dedup/depKey 上有穩定身分,避免多個未解析條目在空 RepoURL 上互撞(Go 的 `depKey()`/`UniqueKey()` 都以 RepoURL 為鍵)

`ParseDepString` **不加** marketplace 分支:apm.yml 字串 `pkg@mkt` 維持拒絕(對齊原版;負向測試)。CLI 的 `pkg@mkt` 只在 cmd 層攔截,不進 ParseDepString。

resolver:`KindMarketplace` 的 dep 在建樹前先 `ResolvePlugin` 收斂成一般 dep(root 與傳遞依賴同一路徑);解析失敗記入 resolution errors,不 panic。

## 序列化不變式(mkt-030)

`DependencyReference` 增加守衛:`Source=="marketplace"`(未解析)的 ref 被要求序列化進 apm.yml 時回傳 error(對齊原版 raise ValueError)。install 寫入 apm.yml 的一律是解析後 canonical(既有 `persistPackagesToManifest` 路徑)。

## Lockfile provenance(mkt-031)

`LockedDep` 加 `DiscoveredVia`/`MarketplacePluginName`/`SourceURL`/`SourceDigest`(omitempty):
- 不參與 `UniqueKey()`(現行實作本來就只用 RepoURL/VirtualPath,加測試鎖定)
- `SourceURL`/`SourceDigest` 只在 kind=url 的 marketplace 有值
- ⚠️ **序列化是顯式清單、非反射(adversarial 抽查確認),要改齊五處**:(1) `parse.go:114-179` 的 switch 加 4 個 case;(2) `write.go:12-22` `entryFieldOrder`;(3) `write.go:134-150` `serializeEntry` 的 fields map;(4) `write.go:449-458` `knownEntryFields`——**漏了 (4) 會踩雙重輸出陷阱**(passthrough 迴圈把「未知欄位」再 emit 一次);(5) `write.go:296-312` `depSemanticEqual` 加入 4 欄(與 Python `is_semantically_equivalent` 對齊;搭配 carry-forward 後重建值相同,不會造成無謂 rewrite)。往返測試要涵蓋「lockfile 已含 provenance 再 round-trip 不重複輸出」

## mkt-032 修正(原版資料遺失 bug)

原版(Python)壞法:apm.yml 先寫入 → target 閘門 SystemExit → apm.yml 不回滾且 lockfile 從未寫入 → 第二次 install 讀到純字串,provenance 永久消失。

**前提已對 apm-go 實際追碼修正(2026-07-03,`cmd/apm/install.go` 一手驗證)**——Go 的流程與 Python 根本不同,原本設計的「順序重排」是解一個 Go 沒有的問題:

- Go 現行順序(`runInstall`):parse apm.yml(記憶體)→ positional packages 併入記憶體 deps(`:197-218`)→ 讀舊 lockfile → **targets 解析(`:409`)→ 依賴解析 → `buildLockfile`(`:477`)→ deploy(僅當 targets>0,`:588`)→ 寫 lockfile(`:668`)→ 最後才寫 apm.yml(`:673-684`)**。
- Go **沒有 target 硬閘門**:no targets 只是跳過 deploy(diags 印到 stderr),lockfile 與 apm.yml 照樣在**同一趟**寫入 → Python 那種「中止在 lockfile 之前」的資料遺失路徑在 Go 結構上不存在(這也是一個既有 Go/Python 行為差異:Python no-harness 是 exit 2 硬錯,Go 非致命——屬既有 deviation,不在本任務範圍,A/B 測試需列例外)。
- **Go 真正的風險形態**:`buildLockfile` 對每個 dep **從零重建** `LockedDep`(`:492-551`),不從 existingLock 帶任何欄位。若 provenance 只在 CLI 帶 `pkg@mkt` 時附加,則第二次**裸** `apm install`(從 apm.yml 重新解析,已是純 git 字串)重建出的 lockfile 沒有 provenance → 覆寫舊檔 → **provenance 在重建時被抹掉**——同一個 bug 的 Go 變體。

**Go 版修法(carry-forward,取代原「順序重排」方案)**:
1. CLI 帶 marketplace ref 的安裝:provenance 在 `buildLockfile` 附加到對應 `LockedDep`(同一趟寫入,天然原子)。
2. **carry-forward 規則**:`buildLockfile` 重建每個 entry 時,若新 entry 的 provenance 欄位為空、且 existingLock 有相同 `UniqueKey()` 的 entry 帶 provenance → 把 `discovered_via`/`marketplace_plugin_name`/`source_url`/`source_digest` 四欄複製過來。純附加、不影響身分,重建永不丟 provenance。
3. `IsSemanticEqual` 對 provenance 的參與與否要**明確定義並測試**(建議:參與比較——與 Python `is_semantically_equivalent` 對齊,carry-forward 保證重建值相同,故不會造成無謂 rewrite)。

回歸測試(AC4,三段):(a) no-target 專案 `install pkg@mkt` → deploy 跳過但 lockfile **已含**全部 provenance;(b) 接著裸 `apm install` → provenance **原封保留**(carry-forward);(c) 接著 `apm install --target x` → deploy 發生、provenance 仍在。測試註解引用 checklist mkt-032 的 Python 壞法與本節的 Go 變體分析。

## 錯誤處理

- MarketplaceNotFound 訊息提示 `apm marketplace list`;PluginNotFound 提示 `apm marketplace browse <mkt>`。
- 網路錯誤不回顯憑證(credsec 慣例)。
- shadow/pin 偵測任何失敗只 debug log,不影響 exit code。

## Phase M6 stretch(`apm search`)

若納入:頂層 `cmd/apm/search.go`,`QUERY@MARKETPLACE` 以 `rsplit @` 切分,走**快取** fetch(與 browse 的 force-refresh 不同,mkt-070);`--limit` 預設 20。不做 `marketplace search` 子指令(負向測試)。
