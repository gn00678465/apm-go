# Implement: apm pack(marketplace.json 產生器)

TDD:每步先寫測試(RED)→ 實作(GREEN)→ `go build ./... && go vet ./... && go test ./...`。
前置:`07-03-marketplace-authoring` 已合入(輸入契約是其 `AuthoringConfig`)。開工前先對照 authoring 最終 schema.go 校正 design.md 的欄位名。

## 執行順序

### 步驟 1 — tagPattern 樣板 + ref/version 解析(mkt-051, mkt-055 的 HeadNotAllowed)
- [ ] `tagpattern.go`(**全新元件**,gaps B2——apm-go 無既有對應):`BuildTagRegex("v{version}"/"{name}-v{version}")` + `MatchTag`;若 authoring 子任務已先做同邏輯則重用其落點,不得兩份
- [ ] 測試:`v{version}` 比對 `v1.2.3`/拒 `x1.2.3`;`{name}-v{version}` 逐字比對套件名(含特殊字元 QuoteMeta);抽出的 version 字串正確
- [ ] `internal/marketplace/build/builder.go`:`ResolvePackages`——本地跳過、40-hex 直接接受、tag 比對、tagPattern 過濾 + `semver.MaxSatisfying`(重用 `internal/semver`,不重寫比對)
- [ ] **branch ref / `HEAD` → HeadNotAllowed 錯誤**(無 allow-head 旗標)
- [ ] 測試:本地 git repo fixture 打 tag(`v1.0.0`/`v2.0.0-rc.1`);range 取最高相符;`--include-prerelease` 行為;無相符 → NoMatchingVersion;branch ref → HeadNotAllowed;本地套件零網路(fake lister panic 斷言)
- 驗證:`go test ./internal/marketplace/build/...`

### 步驟 2 — metadata enrichment(mkt-050 修訂版 (c))
- [ ] remote 套件抓 repo 的 apm.yml 讀 description/version;curator 條目值優先;抓取失敗 → 警告 + 繼續
- [ ] 測試:curator 有值時勝出;curator 無值時用 remote;抓取失敗不中斷 build
- 驗證:PASS

### 步驟 3 — ClaudeMapper(mkt-050, mkt-052 修訂版)
- [ ] `mapper.go`:頂層/plugin 級欄位照 design 的**逐欄觸發條件表**(含 `owner.email` 有值才出、description/version 僅 overridden 才出);source 合成四規則(本地字串+pluginRoot 剝除/git-subdir/url(host-prefixed)/github shorthand + ref/sha 追加);curator-wins 優先序 + `_is_display_version` 等價規則;duplicate-name 警告
- [ ] 測試:本地/遠端/subdir/GHE host-prefixed 混合的 packages[] → 逐欄位斷言輸出形狀;**斷言輸出無 `category`、無 build/tagPattern 等 APM 欄位**;semver range 不出現在輸出 version;pluginRoot 剝除的三個錯誤邊界(空/絕對/`..`)+ root 外警告
- 驗證:PASS(Review Gate A)

### 步驟 4 — CodexMapper + category 閘門(mkt-052/053)
- [ ] CodexMapper 照 design 的 **Codex 專屬形狀**(⚠️與 Claude 差異大):頂層 `name`+`interface.displayName`+`plugins`;plugin 級 `name`/`source`/`policy{installation:"AVAILABLE",authentication:"ON_INSTALL"}`/`category`;本地 source 是 **dict** `{"source":"local","path":...}`;遠端無 github shorthand(一律 url/git-subdir)
- [ ] config 載入層:outputs 含 codex 且缺 category → 硬錯誤 exit 2;mapper 層防禦性 BuildError
- [ ] 測試:codex 輸出形狀逐欄斷言(interface/policy/local-dict);缺 category → exit 2 且錯誤訊息指名 package;outputs 只有 claude 時缺 category **不**報錯(codex 條件式)
- 驗證:PASS

### 步驟 5 — 輸出位置 + CLI 接線(mkt-054, mkt-055)
- [ ] `output.go`:claude → `.claude-plugin/marketplace.json`、codex → `.agents/plugins/marketplace.json`;覆寫兩形式(`marketplace.outputs.<fmt>.path` map 形式優先 + `marketplace.<fmt>.output` 相容形式)+ CLI `--marketplace-path FORMAT=PATH`(可重複、FORMAT 限 known、PATH 過 traversal 驗證);**不實作** `APM_MARKETPLACE_*_PATH` 環境變數(原版宣告未實作);atomic write
- [ ] `cmd/apm/pack.go`:`--offline`/`--include-prerelease`/`--dry-run`/`-m`/`--marketplace-path`/`-v`;exit codes 0/1/2;無 `marketplace:` 區塊 → 訊息 + exit 0
- [ ] `main.go` 註冊 `root.AddCommand(packCmd())`
- [ ] 測試:兩個 outputs → 兩份檔案在正確路徑(**斷言 repo 根目錄沒有 marketplace.json**);覆寫路徑生效;`--dry-run` 零寫入;`-m none` 零輸出;exit code 三類各一案例
- [ ] 負向斷言:`--check-versions`/`--check-clean`/`--allow-head` 旗標**不存在**(本輪不做空殼,見 design 範圍界定)
- 驗證:`go build/vet/gofmt/test ./... -cover` 全綠

### 步驟 6 — schema 子集相容驗證(AC2)
- [ ] 用原版 `tests/fixtures/schemas/claude-code-marketplace.schema.json`(informational)對 Go 輸出跑一次 JSON schema 驗證(測試內嵌 schema 副本或指向 fixture 路徑)
- [ ] 測試:本地/遠端混合輸出通過 schema 子集驗證
- 驗證:PASS

### 步驟 7 — A/B 測試(AC4)
- [ ] `D:\Projects\apm-dev\evals\ab_marketplace_pack.py`(對齊 `ab_mcp_install.py` 慣例)
- [ ] 用 authoring 子任務 scaffold 出的範例專案(+ 本地 git fixture)雙邊跑 pack,比對 `.claude-plugin/marketplace.json` 語意相等(欄位級,容忍鍵序/縮排差異;若可 byte-level 更好);codex 輸出同理
- [ ] 例外清單:原版 `--check-versions`/`--check-clean`/plugin 打包相關差異註明「本輪不實作」
- 驗證:對照 `uv run apm pack` 通過(例外項除外)

### 步驟 8 — 全域驗證
- [ ] `go build/vet/gofmt/test ./... -cover` 全綠
- [ ] 至少一輪 codex exec 唯讀審查,修正發現的問題並補回歸測試
- [ ] checklist mkt-050~055 逐條打勾(mkt-055 的 exit 3/4 標「延後,未實作旗標」)

## Review Gates
- **A**(步驟 3 後):mapper 輸出欄位與 mkt-050/052 修訂版逐欄比對——特別確認 `category` 不在 Claude 輸出、semver range 不外洩、APM 欄位全剝除
- **B**(步驟 5 後):輸出位置正確性(根目錄無殘留檔)+ exit code 對映
- **C**(步驟 7 後):A/B 語意比對不是 tautology(至少一個欄位級 diff 案例證明比對器能抓到差異)

## Rollback Points
全部在新套件 `internal/marketplace/build` + 新檔 `cmd/apm/pack.go`;對既有檔案唯一修改是 `main.go` 一行 `AddCommand`。每步獨立 commit。

## 已知延後項目
- `--check-versions`(exit 3)/`--check-clean`(exit 4)閘門:本輪不實作、旗標不出現(mkt-055 對應欄位標註延後);後續獨立任務
- plugin 打包(`--format`/`--archive`/`-o` 等):不在本子任務(design 範圍界定)
- `--json` 機器可讀輸出:原版有,本輪延後
