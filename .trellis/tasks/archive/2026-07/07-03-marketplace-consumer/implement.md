# Implement: marketplace 消費端指令

TDD:每步先寫測試(RED)→ 實作(GREEN)→ `go build ./... && go vet ./... && go test ./...`。

## 執行順序

### 步驟 1 — `internal/marketplace` 資料模型(mkt-001, mkt-005)
- [x] `models.go`:`SourceKind`、`MarketplaceSource`(+`.Kind()`)、`MarketplacePlugin`、`MarketplaceManifest`
- [x] 測試:`Kind()` 對各種 URL/path 形狀分類正確
- [x] 測試:manifest plugin 條目含 `registry` 鍵時**容忍**(解析成功、值忽略,mkt-005 修訂版)
- 驗證:`go test ./internal/marketplace/...`

### 步驟 2 — SOURCE 判別(mkt-010, mkt-011)
- [x] `source.go`:`ParseMarketplaceSource(raw, host string) (*MarketplaceSource, error)`,依 design.md 判別順序實作
- [x] 測試:本地路徑(`/abs`、`./rel`、`../rel`、`~/home`、`C:\...`、`.\`/`..\`/`~\`/裸 `~`)、裸 `http://` 拒絕、SCP SSH、完整 https URL(直連判斷限 `/marketplace.json` 結尾,任意 `.json` 不算)、`OWNER/REPO`/`HOST/OWNER/REPO` 簡寫
- [x] 測試(mkt-011 修訂版):`--host` 與完整 URL host **衝突 → 硬錯誤**;相符/本地/SCP 不符 → 警告並忽略
- 驗證:PASS,對照 marketplace-checklist.md mkt-010/011 逐條打勾

### 步驟 3 — 登錄檔(mkt-002, mkt-006)
- [x] `registry.go`:`RegistryPath`/`LoadRegistry`/`SaveRegistry`/`FindByName`/`AddSource`/`RemoveSource`,atomic write
- [x] 測試:空檔案、已有其他項目的 fixture(AC3)、**同名(含大小寫不同)靜默取代**(mkt-006,對齊原版——不是拒絕)、`FindByName`/`RemoveSource` 不分大小寫、`RemoveSource` 對不存在名稱報錯
- 驗證:PASS

### 步驟 4 — Fetch dispatch:local + url(mkt-023,先做最簡單的兩種)
- [x] `client_local.go`:直接讀工作樹檔案;探測路徑順序(mkt-003)
- [x] `client_url.go`:HTTPS GET,SHA-256 digest
- [x] 測試:`httptest.Server` 模擬 url kind;暫存目錄模擬 local kind
- 驗證:PASS

### 步驟 5 — Fetch dispatch:github + gitlab(mkt-023)
- [x] `client_github.go`:Contents API + `Accept: application/vnd.github.v3.raw`(回應=原始檔案內容,**不做 base64 解碼**——已 live 驗證,見 design.md API 細節),`Authorization: token` 帶 `GITHUB_APM_PAT`(僅信任 host,mkt-011)
- [x] `client_gitlab.go`:REST v4 `/repository/files/{path}/raw`(純文字端點,project/file path 整段 URL-encode),`PRIVATE-TOKEN` 帶 `GITLAB_APM_PAT`
- [x] 測試:`httptest.Server` 模擬兩種 API 回應形狀;不信任 host 時確認不轉發 PAT
- 驗證:PASS。**此步驟需要對照真實 API 回應形狀**(比照先前 MCP registry client 任務的教訓,不能只憑讀 Python 源碼假設欄位形狀)——若能找到公開、無需認證的小型 marketplace.json 範例可用 live 呼叫驗證一次欄位形狀,否則至少對照 Python 原版原始碼裡實際解析回應的欄位存取路徑逐一核對。

### 步驟 6 — Fetch dispatch:git(mkt-023)
- [x] `client_git.go`:shallow clone 到暫存目錄、讀檔、清理(defer RemoveAll)
- [x] 測試:用本地真實 git repo(`t.TempDir()` 內 `git init`)當 remote,驗證端對端
- 驗證:PASS

### 步驟 7 — Validator(mkt-016 的依賴)
- [x] `validator.go`:對 `MarketplaceManifest` 做基本結構驗證(name 非空、plugins 各自 name 非空且不重複等),回傳 `[]Finding{Level, Message}`
- 驗證:PASS

### 步驟 8 — CLI 接線(mkt-010~mkt-016, mkt-018, mkt-019)
- [x] `cmd/apm/marketplace.go`:`marketplaceCmd()` + 六個子指令
- [x] `add` 的 mkt-018 行為:SOURCE `#ref` fragment(與 `--ref` 互斥)、未 pin 警告、alias 回退 manifest.name(不合法時警告並退 repo 名)
- [x] `build` 墓碑子指令:硬錯誤指向 `apm pack`(mkt-019)+ 測試
- [x] `main.go` 註冊 `root.AddCommand(marketplaceCmd())`
- [x] 每個子指令的 E2E 測試(仿 `cmd/apm/mcp_e2e_test.go` 模式);含 Phase M5 負向斷言:`marketplace search`/`doctor`/`publish` 子指令不存在、`browse` 無 `--json`、`validate` 無 `--check-refs`
- 驗證:`go build/vet/gofmt/test ./... -cover` 全綠

### 步驟 9 — A/B 測試(AC4,PRD 最低驗收門檻)
- [x] `D:\Projects\apm-dev\evals\ab_marketplace_consumer.py`,對齊既有 `ab_phase0.py`/`ab_mcp_install.py` 慣例
- [x] 涵蓋:add 各種 SOURCE 形狀、list、remove(至少 5 情境)——實際 23 斷言:add 本地/3 種負向形狀、list、silent replace、browse、remove、與 Python CLI 的雙向登錄檔互通;2026-07-04 全過。A/B 首輪即抓到 manifest owner 物件形解析 bug(commit 55a6064 修正)
- 驗證:對照真實 `uv run apm` 全數通過 ✅

### 步驟 10 — 全域驗證
- [ ] `go build/vet/gofmt/test ./... -cover` 全綠
- [ ] 至少一輪 codex exec 唯讀審查(比照本專案慣例),修正發現的問題並補回歸測試
- [ ] A/B 測試全綠

## Review Gates
- **A**(步驟 2 後):SOURCE 判別邏輯的正確性(最容易寫錯的部分,規則順序敏感)
- **B**(步驟 6 後):三種網路 fetch(github/gitlab/git)的憑證處理(不信任 host 不得轉發 PAT,錯誤訊息不回顯憑證)
- **C**(步驟 9 後):A/B 測試斷言是否有意義(不是 tautology)

## Rollback Points
步驟 1-3(資料模型+登錄檔)完全獨立,不影響任何既有檔案。步驟 8 起才會修改 `main.go`(新增一行 `AddCommand`),影響範圍小、易回退。

## 已知延後項目(非本子任務目標,不要在這裡做)
- 快取層(ETag/digest)——見 design.md「快取策略」段落
- `--check-refs` 空殼旗標——明確不移植(mkt-017)
