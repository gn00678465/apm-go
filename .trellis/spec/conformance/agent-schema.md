# Target Agent Marketplace / Plugin Field Schema

> **用途**：`07-29-agent-schema-spec`（`07-28-marketplace-plugin-parity` 的 child, U8）的交付物之一 —— 把「各 target agent 的
> marketplace/plugin field schema」從研究紀錄升級為正式 spec，逐欄位列出型別、必填/選填、預設值、上游出處。
>
> **與可執行 schema 的關係（防漂移契約）**：本檔每個產物家族段落（Claude marketplace.json、
> Codex marketplace.json、plugin.json）底下的 markdown 表格，第一欄的每個反引號欄位名（例如 `` `name` ``）都會被
> `internal/marketplace/build/schema_sync_test.go` 與 `internal/pack/bundle/schema_sync_test.go` 的
> `TestSchemaSync_SpecMatchesSchemaFieldSet` 解析出來，與對應的可執行 JSON Schema（`testdata/apm-*.schema.json`）
> 的 `properties` 鍵集合做雙向比對。**修改這裡的欄位表，必須同時修改對應 schema 檔，否則測試會轉紅**——
> 這是本檔案存在的第一目的，不是事後補充的檢查。
>
> **來源素材**（是素材不是本檔的權威版本，衝突時以下方逐欄位表 + Go 型別本身為準）：
> `.trellis/tasks/07-28-marketplace-plugin-parity/research/agent-schema-support-matrix.md` §2、§3、§4，
> `.trellis/tasks/07-28-marketplace-plugin-parity/research/eval-real-run-20260728.md` §D。
>
> **事實來源裁定**：現有 Go 型別（`internal/marketplace/build/mapper.go`、`internal/marketplace/build/codexmapper.go`、
> `internal/pack/bundle/pluginjson.go`）是 apm-go 實際行為的事實來源。本檔與可執行 schema 若與 Go 型別不一致，
> 錯的是本檔或 schema，不是 Go 型別。

---

## 0. 三軸支援度的區別

apm-go 對每個 target agent 的支援度分成三個彼此**不包含**的軸，是本文最容易被誤讀成「有缺口」的地方：

1. **部署**（deployment）—— 該 target 能不能被 `apm install --target <x>` 實際部署一份 `.agents/`/`.claude/` 等產物。
   這一軸本身又分三層集合（apm.yml 詞彙層 `CanonicalTargets`、真的有 adapter 的 `adapterTargets`、
   `init --target` 白名單 `SupportedTargets`），細節見 research 第 1 節,本檔不重複。
2. **marketplace 輸出**（marketplace.json 產出）—— 該 target 是否有自己的 `marketplace.json` 方言。
   **目前只有 claude、codex 兩種**（`internal/marketplace/build/output.go:33` 的
   `KnownOutputFormats = map[string]bool{"claude": true, "codex": true}`）。
3. **plugin.json 生態**（plugin.json 消費者）—— 該 target 是否消費 `plugin.json`。
   **目前只有 claude、copilot 兩種**（`internal/pack/pluginmanifest/write.go:16-19` 的 `PluginEcosystemPaths`）。

三張清單彼此不包含：例如 `codex` 有部署也有 marketplace 輸出，但沒有 plugin.json 生態；`copilot` 有部署也有
plugin.json 生態，但沒有 marketplace 輸出；`opencode`/`antigravity` 只有部署，另外兩軸都沒有。
完整交叉表見 `research/agent-schema-support-matrix.md` §4 的支援度總表。

本檔以下三節，各自對應**一個產物家族**：Claude marketplace.json、Codex marketplace.json、plugin.json
（claude/copilot 共用同一個 Go 型別、同一節）。

---

## Claude marketplace.json（`.claude-plugin/marketplace.json`）

輸出者：`internal/marketplace/build/mapper.go` 的 `ClaudeMapper`。上游對照：`output_mappers.py:53-223`
（`ClaudeMarketplaceMapper.compose`）。實跑產物見 `research/agent-schema-support-matrix.md` §2.1、
`research/eval-real-run-20260728.md` §D1。

### 文件層（`ClaudeDocument`, `mapper.go:30`）

| 欄位 | 型別 | 必填/選填 | 預設值 | 上游出處 |
|---|---|---|---|---|
| `name` | string | 必填 | - | `output_mappers.py:120-141`（頂層恆有 `config.name`，經 `_sanitized_name_with_diagnostic` 正規化；`cfg.Name`） |
| `description` | string | 選填（`omitempty`） | 省略 | `output_mappers.py:120-141`（只有使用者明確覆寫、`description_overridden` 為真才輸出；`cfg.DescriptionOverridden`） |
| `version` | string | 選填（`omitempty`） | 省略 | `output_mappers.py:120-141`（`version_overridden` 同款邏輯；`cfg.VersionOverridden`） |
| `owner` | object | 必填 | - | `output_mappers.py:120-141`（恆有；見下方 owner 表） |
| `metadata` | object | 選填（`omitempty`） | 省略 | `output_mappers.py:120-141`（`config.metadata` 非空時整包透傳，含 `pluginRoot`；`cfg.Metadata`） |
| `plugins` | array | 必填 | - | `output_mappers.py:120-141`（恆有；見下方 plugins[] 表） |

> **`additionalProperties` 例外（`metadata`）**：`apm-claude-marketplace.schema.json` 的
> `properties.metadata` 只宣告 `{"type":"object"}`，**沒有** `additionalProperties:false`——這是刻意的
> 白名單例外（`internal/marketplace/build/schema_sync_test.go` 的 `additionalPropertiesExceptions`
> 鎖定 `root.properties.metadata`），因為 `metadata` 是自由透傳物件（`cfg.Metadata`，任意 key/value，
> 本專案從不解讀其內容），沒有固定欄位集合可收斂成 `additionalProperties:false`。

### owner（`ClaudeOwner`, `mapper.go:42`）

| 欄位 | 型別 | 必填/選填 | 預設值 | 上游出處 |
|---|---|---|---|---|
| `name` | string | 必填 | - | `output_mappers.py:120-141`（`owner` 恆有，`name` 恆有；`cfg.Owner.Name`） |
| `email` | string | 選填（`omitempty`） | 省略 | `output_mappers.py:120-141`（有值才出——「注意 email 也會輸出，先前設計漏了」，archive/2026-07/07-03-marketplace-pack/design.md:77；`cfg.Owner.Email`） |
| `url` | string | 選填（`omitempty`） | 省略 | `output_mappers.py:120-141`（有值才出；`cfg.Owner.URL`） |

### plugins[]（`ClaudePlugin`, `mapper.go:52`）

| 欄位 | 型別 | 必填/選填 | 預設值 | 上游出處 |
|---|---|---|---|---|
| `name` | string | 必填 | - | `output_mappers.py:53-223`（plugin 級恆有；archive/2026-07/07-03-marketplace-pack/design.md:84 逐欄位表；`entry.Name`） |
| `description` | string | 選填（`omitempty`） | 省略 | `output_mappers.py:53-223`（curator-wins 優先序：curator 條目值優先，否則 remote/本地 apm.yml 的 metadata，都沒有則省略；design.md:85；local 優先 `entry.Description`，否則 remote 值） |
| `version` | string | 選填（`omitempty`） | 省略 | `output_mappers.py:53-223`（同上 curator-wins；遠端套件的 curator version 只在 `_is_display_version` 為真才採用，semver range 不進輸出；design.md:85；local 優先 `entry.Version`，否則 remote 值） |
| `author` | object（`map[string]string`） | 選填（`omitempty`） | 省略 | `output_mappers.py:53-223`（curator 條目有 author dict 才出；design.md:86；`entry.Author`） |
| `license` | string | 選填（`omitempty`） | 省略 | `output_mappers.py:53-223`（curator 條目有才出；design.md:87；`entry.License`） |
| `repository` | string | 選填（`omitempty`） | 省略 | `output_mappers.py:53-223`（curator 條目有才出；design.md:88；`entry.Repository`） |
| `tags` | array[string] | 選填（`omitempty`） | 省略 | `output_mappers.py:53-223`（`pkg.tags` 非空才出；design.md:89；`pkg.Tags`） |
| `homepage` | string | 選填（`omitempty`） | 省略 | `output_mappers.py:53-223`（僅本地套件且 curator 條目有才出；design.md:90） |
| `source` | string 或 object | 必填 | - | `output_mappers.py:150-201`（source 合成，design.md:93-98 逐規則對照；local 為純字串；remote 為下方 source 表的物件） |
| `category` | string | 選填（schema-only，見下方說明） | 省略 | **上游會輸出、apm-go 刻意不輸出**：見下方「已知的 schema-only 白名單」 |

> **`additionalProperties` 例外（`author`）**：`apm-claude-marketplace.schema.json` 的
> `$defs.plugin.properties.author` 宣告 `{"type":"object","additionalProperties":{"type":"string"}}`，
> 不是純粹的 `additionalProperties:false`——同樣是刻意的白名單例外（`schema_sync_test.go` 的
> `additionalPropertiesExceptions` 鎖定 `$defs.plugin.properties.author`），因為
> `ClaudePlugin.Author` 的 Go 型別是 `map[string]string`（`mapper.go:56`），沒有固定欄位名集合，
> 只能用「值必須是 string」這種約束（`additionalProperties:{"type":"string"}`）取代「已知欄位 + 全關」。

> ⚠️ **與上游的刻意差異：`category`**。上游 apm 0.26.0 的 claude 輸出**會**帶 `category`
> （`eval-real-run-20260728.md:263`：「`category` 在 claude 輸出裡也會被帶出（雖然只有 codex 才強制要求）」，
> 逐字產物見 `eval-real-run-20260728.md:243-261` 與本檔對應的
> `internal/marketplace/build/testdata/upstream-claude-marketplace.golden.json`）。
> 但 apm-go 的 `ClaudePlugin`（`mapper.go:52-62`）**不含** `category` 欄位——這是刻意對齊 Go 版本的行為，
> 由 `internal/marketplace/build/mapper_test.go:561` 的 `TestClaudeMapper_Output_NoCategoryOrAPMFieldsInJSON`
> 鎖定（斷言輸出 JSON 絕不含 `category`/`tagPattern`/`include_prerelease`/`build`），mkt-052 修訂版的既有裁定。
>
> **已知的 schema-only 白名單**：因為 AS4 要求「把上游實跑產物餵進 schema 要 validate 通過」，而上游產物確實
> 帶 `category`，`apm-claude-marketplace.schema.json` 的 `$defs.plugin.properties` 因此把 `category` 宣告為
> **optional**——這是本檔唯一一個「schema 有、Go 型別故意沒有」的欄位，不是漂移，是刻意的相容窗口。
> `internal/marketplace/build/schema_sync_test.go` 的 `TestSchemaDrift_GoTypesMatchSchemaProperties` 用一個
> 顯式白名單 `{"category"}` 承認這個例外，並額外斷言白名單裡的欄位**不得**出現在 `ClaudePlugin` 的 json tag
> 裡——如果日後 Go 型別真的加了 `category`，這條反向斷言會轉紅，提醒維護者把白名單那一條移除
> （因為屆時它就不再是「schema-only」了）。除了 `category`，schema 與 Go 型別的欄位集合仍要求完全相等。

### source（`RemoteSource`, `mapper.go:72`；local 套件時 `source` 是純字串，不進這張表）

四種組成規則：local → 純字串（`composeClaudePlugin` 的 `pkg.IsLocal` 分支，`mapper.go:176-192`，
不經過 `composeRemoteSource`）；其餘三種 remote 形狀都在 `composeRemoteSource`（`mapper.go:214-244`）：
有 subdir → `git-subdir`；非預設 host → `url`；其餘 → `github`。

| 欄位 | 型別 | 必填/選填 | 預設值 | 上游出處 |
|---|---|---|---|---|
| `source` | string（enum: `github`、`url`、`git-subdir`） | 必填 | - | `output_mappers.py:150-201`（四規則的判別欄位；design.md:94-97） |
| `repo` | string | 選填（`omitempty`） | 省略 | `output_mappers.py:150-201`（規則 4，design.md:97；只在 `"github"` 形狀出現） |
| `url` | string | 選填（`omitempty`） | 省略 | `output_mappers.py:150-201`（規則 2/3，design.md:95-96；只在 `"url"`/`"git-subdir"` 形狀出現） |
| `path` | string | 選填（`omitempty`） | 省略 | `output_mappers.py:150-201`（規則 2，design.md:95；只在 `"git-subdir"` 形狀出現） |
| `ref` | string | 選填（`omitempty`） | 省略 | `output_mappers.py:150-201`（規則 5，design.md:98；已知時附加） |
| `sha` | string | 選填（`omitempty`） | 省略 | `output_mappers.py:150-201`（規則 5，design.md:98；已知時附加） |

> ⚠️ **空 `source` 目前不會被上游驗證層擋下——這是已知產品缺陷候選，不是「已知前置條件」**：
> 主 session 已用實際編譯的 `bin/apm-go.exe` 端到端重現：對一份 marketplace `apm.yml` 塞入
> `packages: [{name: ghost-pkg, source: "", ref: <40 碼 hex>}]`，`apm-go pack` **成功**（只印警告，
> 不報錯），claude 輸出產生
> `{"name":"ghost-pkg","source":{"source":"github","ref":"aaa…","sha":"aaa…"}}`——缺 `repo`，本
> schema 正確拒絕這個形狀（`required` 規則擋下，等同 `TestSchemaReject_RemoteSourceVariants` 的
> 「缺 repo」案例）。
>
> 根因：`internal/marketplace/authoring/schema.go:492` 的 `parsePackages` 只在
> `source != ""` 時才呼叫 `manifest.ValidateMarketplaceSource`（:493）——空字串 `source` **完全跳過**
> 這條驗證，直接進入 `PackageEntry`，一路通過 `LoadAuthoringConfig` 與 `pack` 到 `Compose`，
> 產出上述缺 `repo` 的畸形 entry。
>
> **本 schema 的立場不變**：它描述的是「合法輸出」的形狀，用 `required` 擋下這個畸形 entry
> 是刻意且正確的行為——schema 不需要、也不應該去容忍上游驗證層的漏洞。
> **是否要在載入層（`authoring/schema.go`）拒絕空 `source`，屬產品行為裁定，不在本 task
> （spec/schema-only）範圍內**，由使用者裁定並記入 task 的 verification-record。
>
> **成本估計**（codex round-7 MAJOR-1 要求，claim-evidence-guide 的「時序」句型）：
> 修法本身是 `internal/marketplace/authoring/schema.go:492` 拿掉 `source != ""` 這個條件、
> 讓 `manifest.ValidateMarketplaceSource`（`internal/manifest/mcp.go:300-303`）對空字串無條件執行——
> 該函式本身**已經**對空字串回傳 `"marketplace source is empty"`（`mcp.go:301-303`），
> 所以修法確實只有 1 行（拿掉 `if` 判斷式，讓呼叫變成無條件）。回歸風險：對
> `internal/marketplace/authoring/` 與 `internal/marketplace/resolver_test.go` 全文 grep
> `Source:\s*""` 只找到一處（`resolver_test.go:45`），但那是 `MarketplacePlugin.Source`
> （marketplace 內部 plugin 相對路徑解析，`resolver.go` 的完全不同程式碼路徑），
> 不經過 `parsePackages`/`ValidateMarketplaceSource`——`authoring` 套件本身沒有任何既有測試
> 依賴「空字串跳過驗證」這個行為（同一 grep 對 `internal/marketplace/authoring/` 零匹配）。
> 估計總成本：1 行產品碼 + 1 個新測試（斷言空 `source` 現在回錯誤，約 10–15 行）+
> 不需要調整既有測試。**成本很小，但仍是產品行為變更**（會讓現在「成功但印警告」的
> `apm-go pack` 呼叫改為直接失敗），且與 `07-29-marketplace-add-fixes` 已交付範圍有時序
> 重疊風險（同一驗證函式的呼叫慣例），因此仍由使用者裁定是否併入、以及併入哪個 task，
> 不在本 task（spec/schema-only）自行變更產品碼。

---

## Codex marketplace.json（`.agents/plugins/marketplace.json`）

輸出者：`internal/marketplace/build/codexmapper.go` 的 `CodexMapper`。上游對照：`output_mappers.py:226-309`
（`CodexMarketplaceMapper.compose`/`_codex_source`）。實跑產物見
`research/agent-schema-support-matrix.md` §2.2、`research/eval-real-run-20260728.md` §D2。
**與 Claude 方言差異很大**（`codexmapper.go:1-8` 的檔案層註解），不是「Claude 形狀加 category」。

### 文件層（`CodexDocument`, `codexmapper.go:19`）

| 欄位 | 型別 | 必填/選填 | 預設值 | 上游出處 |
|---|---|---|---|---|
| `name` | string | 必填 | - | `output_mappers.py:287-327`（`CodexMarketplaceMapper.compose` 組裝順序首欄；research/agent-schema-support-matrix.md:161；`cfg.Name`） |
| `interface` | object | 必填 | - | `output_mappers.py:287-327`（見下方 interface 表；**無** `description`/`version`/`owner`/`metadata`） |
| `plugins` | array | 必填 | - | `output_mappers.py:287-327`（見下方 plugins[] 表） |

### interface（`CodexInterface`, `codexmapper.go:27`）

| 欄位 | 型別 | 必填/選填 | 預設值 | 上游出處 |
|---|---|---|---|---|
| `displayName` | string | 必填 | - | `output_mappers.py:287-327`（用**未經 sanitize** 的 `config.name`；頂層 `name` 才是 sanitize 過的；`cfg.Name`） |

### plugins[]（`CodexPlugin`, `codexmapper.go:36`）

| 欄位 | 型別 | 必填/選填 | 預設值 | 上游出處 |
|---|---|---|---|---|
| `name` | string | 必填 | - | `output_mappers.py:287-327`（`pkg.Entry.Name`） |
| `source` | object | 必填 | - | `output_mappers.py:226-309`（`_codex_source`；local/remote 兩種形狀，見下方 local source / remote source 表） |
| `policy` | object | 必填 | - | `output_mappers.py:287-327`（固定值，見下方 policy 表） |
| `category` | string | 必填 | - | `output_mappers.py:311-314`（缺就 `BuildError`；本專案 `CategoryRequiredError`，`codexmapper.go:62-77,97-99`），這是唯一強制要求 category 的產物家族 |

### policy（`CodexPolicy`, `codexmapper.go:46`；每個 plugin 都相同、從不由輸入推導的固定值）

| 欄位 | 型別 | 必填/選填 | 預設值 | 上游出處 |
|---|---|---|---|---|
| `installation` | string（enum: `AVAILABLE`） | 必填 | 固定 `"AVAILABLE"` | `output_mappers.py:287-327`（archive/2026-07/07-03-marketplace-pack/design.md:103；`codexmapper.go:103`） |
| `authentication` | string（enum: `ON_INSTALL`） | 必填 | 固定 `"ON_INSTALL"` | `output_mappers.py:287-327`（design.md:103；`codexmapper.go:103`） |

### local source（`CodexLocalSource`, `codexmapper.go:57`；unlike Claude，codex 本地 source 永遠是 dict，不是純字串）

| 欄位 | 型別 | 必填/選填 | 預設值 | 上游出處 |
|---|---|---|---|---|
| `source` | string（enum: `local`） | 必填 | - | `output_mappers.py:226-309`（`_codex_source` 本地分支：`{"source":"local","path":<source>}`；design.md:104） |
| `path` | string | 必填 | - | `output_mappers.py:226-309`（design.md:104；`pkg.Entry.Source` 原樣；**不做** pluginRoot 剝除，與 Claude 不同） |

### remote source（`RemoteSource`，與 Claude 共用同一個 Go 型別；codex 只用 `url`/`git-subdir` 兩形狀，**無** github shorthand）

| 欄位 | 型別 | 必填/選填 | 預設值 | 上游出處 |
|---|---|---|---|---|
| `source` | string（enum: `url`、`git-subdir`） | 必填 | - | `output_mappers.py:226-309`（`_codex_source`；design.md:104；本專案 `composeCodexSource`, `codexmapper.go:129-155`） |
| `url` | string | 必填 | - | `output_mappers.py:226-309`（⚠️ 見下方「已知上游瑕疵」；`composeCodexSource` 兩個 remote 分支都無條件賦值 `urlOrRepo`，codex **沒有** Claude 那種可以不設 url 的 github 分支，所以對 codex 而言 url 恆有——這點與 Claude 的 source 表不同，那邊 url 只在部分變體出現） |
| `path` | string | 選填（`omitempty`） | 省略 | `output_mappers.py:226-309`（design.md:104 的 git-subdir 分支；只在 `"git-subdir"` 出現） |
| `ref` | string | 選填（`omitempty`） | 省略 | `output_mappers.py:226-309`（design.md:104「ref/sha 同樣追加」；已知時附加） |
| `sha` | string | 選填（`omitempty`） | 省略 | `output_mappers.py:226-309`（design.md:104；已知時附加） |

> ⚠️ **`repo` 是 Go-only 欄位，schema 刻意不宣告**：`RemoteSource` Go 型別的 `Repo` 欄位是與 Claude
> 共用的（Claude 的 github 變體會用到），但 `composeCodexSource`（`codexmapper.go:129-155`）在
> codex 的兩個 remote 分支（`url`/`git-subdir`）都**從不**賦值 `Repo`——因此 codex 的兩份可執行 schema
> 分支都不宣告 `repo` 這個 property（帶 `repo` 的 codex 文件會被 `additionalProperties:false` 拒絕）。
> `internal/marketplace/build/schema_sync_test.go` 的 `TestSchemaDrift_GoTypesMatchSchemaProperties`
> 用 `goOnlyAllowed: {"RemoteSource(codex)": ["repo"]}` 精確鎖定這個例外（與 `category` 的
> schema-only 白名單方向相反：這裡是「Go 有、schema 故意沒有」）。

### 已知上游瑕疵：`source.url` 的 owner/repo shorthand（刻意對齊，不是 bug）

當 codex 輸出在**預設 host**且無 subdir 時，`source.source` 是字面 `"url"`，但 `source.url` 欄位放的其實是
**`owner/repo` shorthand，不是真正的 URL**。`internal/marketplace/build/codexmapper.go:122-127` 的註解明寫：

> "url is the non-default host's https:// URL when pkg.Host is set, otherwise pkg.SourceRepo verbatim
> (mirroring Python's own `_remote_source_url(pkg) or pkg.source_repo` fallback -- on the default host
> this is a bare "owner/repo" string, not a full URL, exactly as the Python original composes it)."

這源自上游 Python `_remote_source_url` 在無 `source_url`、無 `host` 時回傳 `None`、呼叫端 fallback 到
`pkg.source_repo` 所致的瑕疵；`research/agent-schema-support-matrix.md` §2.2 已核對實跑產物與這段註解一致。
apm-go **刻意照做對齊**（不「修正」它），因為 codex 端本來就依賴這個 shorthand 格式讀取。

---

## plugin.json（claude: `.claude-plugin/plugin.json`；copilot: `.github/plugin/plugin.json`）

由 `apm.yml` 合成（`internal/pack/bundle/pluginjson.go` 的 `PluginManifest`/`Synthesize`/`ToJSONValue`）。
上游對照：`deps/plugin_parser.py:930-992`（`synthesize_plugin_json_from_apm_yml`）、
`core/plugin_manifest.py:366-382`（`build_plugin_manifest`）。實跑產物見
`research/agent-schema-support-matrix.md` §3、`research/eval-real-run-20260728.md` §D3。

**claude 與 copilot 共用同一個 Go 型別**（`PluginManifest`）；`mcpServers` 是唯一的生態差異
（只在 claude 生態賦值，見下方「claude / copilot 差異」）——因此本檔把兩者當**同一個產物家族**處理，
對應兩份可執行 schema（`apm-plugin-claude.schema.json` 含 `mcpServers`、`apm-plugin-copilot.schema.json` 不含）。

> **AS4「餵上游實跑產物」對 plugin.json 家族已滿足**：`research/eval-real-run-20260728.md` 全文只有兩個
> json 逐字區塊（都是 D1/D2 的 marketplace.json，已由 `internal/marketplace/build/
> testdata/upstream-*-marketplace.golden.json` 覆蓋），plugin.json 的 D3（:307-308）在 research 裡
> **只有欄位集合描述，沒有逐字 JSON**。但 eval 的原始工作目錄（`apm-plugin-verify/single-plugin-repo/`）
> 2026-07-31 仍在磁碟上，主 session 直接讀取後逐字轉錄為
> `internal/pack/bundle/testdata/upstream-plugin-init.golden.json`（repo 根 `plugin.json`，
> `apm plugin init` 產物）與 `upstream-plugin-pack.golden.json`（`.claude-plugin/plugin.json`，
> `apm pack` 產物，無 license）——兩者現在是**真·逐字上游產物**，不再是依欄位集合推導的 fixture
> （曾短暫存在的 `derived-plugin-*.golden.json` 已被取代並刪除）。
> `internal/pack/bundle/schema_sync_test.go` 的 `TestSchemaGolden_UpstreamPluginInit/Pack_
> ValidatesAgainstBothSchemas` 驗證兩者都能通過 claude 與 copilot 兩份 schema。
> 另外，`TestSchemaGolden_LiveOutput_*`（呼叫真正的 `ToJSONValue()`，並與 committed golden 逐棵樹比對）
> 補上「實際序列化器輸出與 golden 完全一致、且通過 schema」這一層，覆蓋純讀檔測試無法抓到的
> 序列化器迴歸（例如 `authorValue` 被改壞）。

### 欄位（`PluginManifest`, `pluginjson.go:41`）

| 欄位 | 型別 | 必填/選填 | 預設值 | 上游出處 |
|---|---|---|---|---|
| `name` | string | 必填 | - | `deps/plugin_parser.py:963-990`（`synthesize_plugin_json_from_apm_yml`/`build_plugin_manifest` 的欄位順序；apm.yml 的 `name:`；缺/空回錯，mf-002 同款檢查） |
| `version` | string | 選填（`omitempty`） | 省略 | `deps/plugin_parser.py:963-990`（apm.yml 的 `version:`） |
| `description` | string | 選填（`omitempty`） | 省略 | `deps/plugin_parser.py:963-990`（apm.yml 的 `description:`） |
| `author` | object | 選填（`omitempty`） | 省略 | `deps/plugin_parser.py:963-990`（見下方 author 表；scalar author 會被合成為 `{"name": …}`） |
| `license` | string | 選填（`omitempty`） | 省略 | `deps/plugin_parser.py:963-990`（apm.yml 的 `license:`；`apm.yml` 通常沒有此欄位，`plugin init` 硬寫的 license 不會回填進 apm.yml，見 research §3.4） |
| `homepage` | string | 選填（`omitempty`） | 省略 | `deps/plugin_parser.py:963-990`（apm.yml 的 `homepage:`） |
| `repository` | string | 選填（`omitempty`） | 省略 | `deps/plugin_parser.py:963-990`（apm.yml 的 `repository:`） |
| `keywords` | array[string] | 選填（`omitempty`） | 省略 | `deps/plugin_parser.py:988-990`（`synthesizeKeywords` 逐字對照；apm.yml 的 `keywords:`；單一 scalar 會被包成一元素陣列） |
| `mcpServers` | object | 選填（`omitempty`） | 省略；**copilot 生態一律不輸出** | `core/plugin_manifest.py:372-378`（`build_plugin_manifest` 的 `manifest.pop`/條件賦值；本專案 `pluginmanifest/producer.go:47-52`），且已剝除帶憑證鍵（`env`/`environment`/`headers`/`authorization`，`plugin_manifest.py:77-78`） |

### author（`Author`, `pluginjson.go:17`）

| 欄位 | 型別 | 必填/選填 | 預設值 | 上游出處 |
|---|---|---|---|---|
| `name` | string | 必填 | - | `deps/plugin_parser.py:969-981`（`synthesizeAuthor` 逐字對照；scalar author 值，或 mapping 的 `name:`——缺 name 則整個 author 欄位被丟棄） |
| `email` | string | 選填（`omitempty`） | 省略 | `deps/plugin_parser.py:969-981`（mapping 的 `email:`） |
| `url` | string | 選填（`omitempty`） | 省略 | `deps/plugin_parser.py:969-981`（mapping 的 `url:`） |

### claude / copilot 差異

| 生態 | 輸出路徑 | `mcpServers` |
|---|---|---|
| claude | `.claude-plugin/plugin.json` | 有（`.mcp.json` 存在時收錄，已剝除憑證鍵） |
| copilot | `.github/plugin/plugin.json` | **一律省略**（不屬於 Copilot plugin manifest schema） |

覆寫政策（兩生態相同）：既有 `plugin.json` 預設保留不覆寫，只有 `--force` 才覆蓋
（`internal/pack/pluginmanifest/write.go:57-64`）。

---

## 可執行 schema 對照表

> **雜湊封印 = PRD R3.1 第 2 條「schema 改了，spec 文件沒跟著改 → 紅」的字面完備實作**。
> 下表最後一欄的 SHA-256 是四份自撰 schema 檔（build 兩份、bundle 兩份；上游副本
> `claude-code-marketplace.schema.json` 與 golden/upstream-golden 檔**不**納入——它們不是
> R3.1 所指的「schema」本體）**原始 bytes** 的雜湊，逐位元組鎖死整份檔案內容。上面/下面
> 描述的 `TestSchemaDrift_*`/`TestSchemaSync_SpecMatchesSchema*` 等**語意投影測試是診斷層**：
> 它們負責在雜湊轉紅時告訴你「差在哪個欄位/型別/enum」，但它們各自的投影深度終究有限
> （見本檔案歷史稽核紀錄逐輪收斂的過程）——**雜湊封印本身沒有這個問題**，因為它比對的是
> 整份檔案的每一個 byte，不經過任何投影，schema 檔內容只要有一位元組不同，雜湊就不同。
> 兩者分工：雜湊封印負責「有沒有漏改」（完備、但只回答是非題）；語意投影負責「改在哪裡、
> 改得對不對」（診斷、但投影深度有限）。`TestSchemaSync_SchemaFileHashesMatchSpec`
> （build 與 bundle 套件各一份）即為此封印的測試本體。

| 產物家族 | schema 檔 | golden（正向） | 對應 Go 型別 | SHA-256（schema 檔原始 bytes） |
|---|---|---|---|---|
| Claude marketplace.json | `internal/marketplace/build/testdata/apm-claude-marketplace.schema.json` | `internal/marketplace/build/testdata/apm-claude-marketplace.golden.json` | `ClaudeDocument`/`ClaudeOwner`/`ClaudePlugin`/`RemoteSource` | `511457e2a6ff8a932931f1c80bc8139977d64133219c206af153fbfa408807b0` |
| Codex marketplace.json | `internal/marketplace/build/testdata/apm-codex-marketplace.schema.json` | `internal/marketplace/build/testdata/apm-codex-marketplace.golden.json` | `CodexDocument`/`CodexInterface`/`CodexPlugin`/`CodexPolicy`/`CodexLocalSource`/`RemoteSource` | `842586b03b7c9ad4284e0d7feb4d57211719253ab67d491275531ef7c5dacc62` |
| plugin.json（claude） | `internal/pack/bundle/testdata/apm-plugin-claude.schema.json` | `internal/pack/bundle/testdata/apm-plugin-claude.golden.json` | `PluginManifest`/`Author` | `3d815c47be218a51e53c473e441ddc60ab8109bbc7ebae1a16bc2858fde28e35` |
| plugin.json（copilot） | `internal/pack/bundle/testdata/apm-plugin-copilot.schema.json` | `internal/pack/bundle/testdata/apm-plugin-copilot.golden.json` | `PluginManifest`/`Author`（`mcpServers` 恆不出現） | `45970197c017188fd995154a36c9ca9d8620abcbee42d376c028c3cd3161f3ef` |

> **更新這欄的正確順序**：改 schema 檔 → 先確認 spec 本文（欄位表/型別/enum 描述）是否也要
> 同步改 → 兩者都改完、`TestSchemaSync_SpecMatchesSchema*` 系列已綠 → 最後才重算並填入這欄的
> SHA-256（順序反過來——改完先補 hash 再看語意測試——等於繞過了封印存在的意義）。

防漂移測試：`internal/marketplace/build/schema_sync_test.go`、`internal/pack/bundle/schema_sync_test.go`
的 `TestSchemaDrift_*`（Go 型別 ↔ schema）與 `TestSchemaSync_*`（schema ↔ 本文件，含雜湊封印
`TestSchemaSync_SchemaFileHashesMatchSpec`）。

`TestSchemaSync_SpecMatchesSchemaFieldSet` 同步**欄位集合**（第一欄，逐字段名）；
`TestSchemaSync_SpecMatchesSchemaTypesAndRequiredness` 進一步同步**第二欄（型別，含 enum）與
第三欄（必填/選填）**：

- **型別欄**：比對走**封閉字面量集合**（`allowedTypeLiterals`，逐字 exact match，不是 prefix）——
  集合外的任何文字一律 fail-closed 轉紅（例如 `string（garbage-not-a-type）` 這種夾帶在括號裡的
  垃圾字串不會被放行）。集合目前有 11 個字面量：`string`、`object`、`array`、`array[string]`、
  `object（\`map[string]string\`）`、`string 或 object`（僅 `ClaudePlugin.source` 這一格允許,寫死格位鎖）,
  以及 5 個 enum 字面量 `string（enum: \`X\`、\`Y\`）` 形式（`policy.installation`/`authentication`各一、
  三張 source 表的判別欄各一）。
- **enum 同步**：帶 enum 的型別欄額外會被拆出反引號逐一列出的值集合，與 schema 裡對應欄位的
  `enum` 宣告（oneOf branch 內的判別值取聯集）做集合相等比對——雙向：spec 宣稱 enum 但 schema
  沒有、或 schema 有 enum 但 spec 沒宣稱，都會轉紅。
- **必填/選填欄**：開頭「必填」→ 應在 schema 的 `required`；開頭「選填」→ 不應在（oneOf 分支的 def
  用交集語意：交集內即所有變體都要求 = 必填，見 D3 與 `schemaNodePropsAndRequired` 註解）——雙向
  嚴格相等，不接受「有些變體要求、有些不要求」這種模糊地帶（那由
  `TestSchemaReject_RemoteSourceVariants`/`TestSchemaGolden_RemoteSourceVariantsMinimal` 個別鎖定）。

## 機器同步的殘餘（誠實記錄）

上面兩個 `TestSchemaSync_*` 測試涵蓋了 PRD R3.1 字面要求的「欄位集合」，並擴充到型別（含 enum 精確值）
與必填/選填——但本檔逐欄位表還有兩欄**不做機器同步**，因為它們是自由散文，無法可靠 parse：

1. **預設值欄**（第四欄）——例如「省略」「固定 `"AVAILABLE"`」「省略；copilot 生態一律不輸出」，
   語意五花八門，沒有統一的 parse 規則。
2. **上游出處欄**（第五欄）——file:line 引用 + 敘述文字混合，見上方各表；已盡力補齊
   （不留空格、不留純本地變數名），但沒有機器檢查它是否過期或指向錯誤行號。

這兩項若要機器同步，需要把預設值欄與上游出處欄也改成結構化格式（例如固定的 key=value 語法）而非自由散文，
超出本 task 的範圍；目前靠人工審閱維持一致，變更這些欄位時**務必**同時檢查對應 schema 檔的
`description` 是否也要更新。

> **反例（codex round-7 MAJOR-1 要求，證明目前確實不同步）**：`internal/marketplace/build/
> schema_sync_test.go:527` 的 `tableRowRe`（`internal/pack/bundle/schema_sync_test.go:256` 同款）
> 只捕捉每列的前三個 cell（欄位/型別/必填選填），`specRow` struct（`schema_sync_test.go:516-520`）
> 也只有 `name`/`typeRaw`/`requiredRaw` 三個欄位——第四、五欄的文字完全不會被讀進任何測試。
> 可重現：把本檔 `license` 列（plugin.json 家族）的第四欄從「省略」改成「固定 `"MIT"`」
> 這種與 `pluginjson.go`（`ToJSONValue`，omitempty 語意）矛盾的錯誤敘述，
> 執行 `go test -run 'SchemaSync' ./internal/pack/bundle/... -v` 仍然全綠——沒有任何測試會發現這個矛盾。
>
> **成本估計**：
> 1. **預設值欄**：JSON Schema draft-07 本身有 `"default"` 關鍵字，目前四份 schema 檔完全沒有使用；
>    要機器同步需要 (a) 對每個有預設值的 property 補上 `"default"` 宣告（4 份 schema 檔、
>    約 20–25 個有預設值的欄位，每個 1 行，約 20–30 行）、(b) 在 `specTypeCategory`/
>    `assertSpecTableMatchesSchema` 旁新增一個「預設值欄 → schema `default`」的 parse + 比對函式
>    （兩個套件各自複製一份，參照現有 `specRequiredness` 的複雜度，約 30–40 行 × 2 package = 60–80 行）、
>    (c) 涵蓋「固定值」「省略」「省略；copilot 生態一律不輸出」等異質語意需要的 parse 規則本身
>    也要人工設計一套詞彙表（類似 `allowedTypeLiterals` 但語意更複雜，因為「固定 `"AVAILABLE"`」
>    這種字串同時混雜了型別與值）。**預估總計約 150–200 行**，且需要新設計一套詞彙表 schema，
>    有中度導入新錯誤的風險。
> 2. **上游出處欄**：file:line 引用混著敘述文字（例如「curator-wins 優先序：curator 條目值優先，
>    否則 remote/本地 apm.yml 的 metadata」），沒有『開頭必為 file:line』這種固定格式可 parse——
>    要機器同步，第一步得先設計一套引用微格式（例如強制欄位開頭是
>    `` `path/to/file.go:123` ``，敘述文字移到欄位其餘部分），這是一個**先於實作的設計決定**，
>    不是單純加測試；且機器檢查『行號是否過期』需要讀取實際原始碼並比對該行內容語意（不是單純
>    字串比對），屬於更大範圍的靜態分析工作。**沒有可信的行數估計**（設計未定，無法估工），
>    保守判斷這比第 1 項的規模更大。
>
> 兩項合計已超過本 task 其餘任何單一交付物的規模（現有防漂移機制的核心三層本身也就
> 幾百行），且第 2 項需要一個獨立的格式設計裁定——這是「超出範圍」判斷成立的具體依據，
> 不只是形容詞。

