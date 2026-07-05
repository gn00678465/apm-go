# Implement: install <plugin>@<marketplace> 解析與部署整合

TDD:每步先寫測試(RED)→ 實作(GREEN)→ `go build ./... && go vet ./... && go test ./...`。
前置:`07-03-marketplace-consumer` 的 `internal/marketplace`(registry + Fetch)已合入。

## 執行順序

### 步驟 1 — `ParseRef`(mkt-020, mkt-021)
- [ ] `internal/marketplace/ref.go`:整條輸入先 TrimSpace → 切 `#`(最多一次)→ head 的 `/`/`:` 檢查 → `^[a-zA-Z0-9._-]+@[a-zA-Z0-9._-]+$` 比對 → **有 `#` 但 ref 空 → fall through**(design 修訂規則 0/4)→ ref 含 `[~^<>=!]` 報錯
- [ ] 測試:`pkg@mkt`、`pkg@mkt#v1.0`、`pkg@mkt#feature/branch`(Go 版**要通過**——刻意 deviation,測試註解標明)、`" pkg@mkt "`(strip 後接受)、`owner/repo`、`owner/repo@alias`、`git@host:o/r`、本地路徑、**`pkg@mkt#` 與 `pkg@mkt# `(空 fragment → fall through,對齊原版)** → 依規則分流;`pkg@mkt#^1.0` → 明確錯誤且訊息含「semver range」
- 驗證:`go test ./internal/marketplace/...`

### 步驟 2 — `resolvePluginSource` 映射(mkt-025, mkt-026)
- [ ] `internal/marketplace/resolver.go`:相對字串/github/git-subdir/gitlab/url dict → canonical;`kind: npm` 變體報錯
- [ ] 測試:五種形狀 + npm 拒絕 + 本地 marketplace 相對 source 的 fast path(絕對路徑 canonical)
- [ ] 測試(mkt-026 雙層):`type: npm` 的 plugin 在 manifest 解析就不存在(PluginNotFound),`kind: npm` 才走 resolve 層錯誤
- 驗證:PASS

### 步驟 3 — `ResolvePlugin` 主流程(mkt-022, mkt-024, mkt-027, mkt-035)
- [ ] 串接 registry.FindByName + client.Fetch + resolvePluginSource;非 GitHub 家族 in-marketplace 子目錄 → 結構化 DepRef;註冊 ref 傳播
- [ ] **負向測試(mkt-022)**:專案 apm.yml 有 `marketplace:` 區塊、全域登錄檔沒有該名稱 → MarketplaceNotFound(證明不讀本地 apm.yml)
- [ ] 測試:plugin 名稱大小寫不敏感(mkt-024);`--ref feat/x` 註冊的 marketplace + 相對 source plugin → canonical 帶 `#feat/x`(mkt-035)
- 驗證:PASS

### 步驟 4 — version_spec 解析(mkt-021/033 的 range 路徑)
- [ ] semver range → `gitops.RealTagLister` + `semver.MaxSatisfying`;無相符且非嚴格 range → 回退 raw ref;非 range → 直接 `#<spec>`
- [ ] 測試:本地 git repo fixture 打 tag,range 解析到最高相符 tag;無相符 tag 的回退行為
- 驗證:PASS

### 步驟 5 — Cross-repo fail-closed 閘門(mkt-028)⚠️安全性
- [ ] `Risk` 判定:`*.ghe.com` marketplace + dict type=github + 非 in-marketplace + 裸 `owner/repo`(URL/SCP/host 限定形式全部豁免)
- [ ] install 端:`Risk != nil` → 立即拒絕,錯誤訊息含兩個修正選項(host 限定寫法)
- [ ] **負向測試**:用會 panic 的 fake 網路層證明拒絕發生在任何探測之前;host 限定(`github.com/o/r`、同 host、URL、SCP)不觸發閘門
- 驗證:PASS(Review Gate A)

### 步驟 6 — ref-swap pin + shadow 偵測(mkt-034)
- [ ] `pins.go`:`~/.apm/cache/marketplace/version-pins.json`(已對照 `version_pins.py` 驗證的原版路徑),扁平 dict `{"<mkt>/<plugin>/<version>": "<ref>"}`、鍵整串 lowercase、**fail-open**(檔案/JSON 錯誤只 log 不阻斷),atomic write;變更 → 警告
- [ ] `shadow.go`:走訪其他已註冊 marketplace(名稱排除不分大小寫)找同名 plugin → 警告;任何錯誤吞掉(consumer MVP 無快取層 → 每項 live fetch,見 design 註記,不因效能砍功能)
- [ ] 測試:pin 變更觸發警告、version 變更不誤報、pin 檔損毀時 fail-open;shadow 命中警告;shadow 內部錯誤不影響安裝結果
- 驗證:PASS

### 步驟 7 — apm.yml dict 形式(mkt-033)+ 序列化守衛(mkt-030)
- [ ] `internal/manifest/depref.go`:`ParseDepDict` 加 `marketplace` 鍵分支——**必須排在既有 `name`/`git`/`id`/`path` 分支之前**(design 地雷註記:`:363` 的 name 分支會 shadow);RepoURL 用 `_marketplace/<mkt>/<name>` 佔位符;name 必填獨立錯誤訊息;version 選填非空字串、parse 不驗格式;大小寫保留
- [ ] `internal/resolver`:KindMarketplace dep 建樹前經 `ResolvePlugin` 收斂(root + 傳遞依賴)
- [ ] 序列化守衛:未解析 marketplace ref 進 apm.yml 寫入路徑 → error
- [ ] **負向測試(Phase V)**:(a) apm.yml 字串 `pkg@mkt` 拒絕;(b) dict `version: "~1.2.0"` 可解析;(c) `{marketplace, git}` 併用拒絕;(d) 未知鍵拒絕;(e) 未解析 ref 序列化 → error;(f) **`{name, marketplace}` 不被 name 分支吃成 git-literal**(分支順序鎖定);(g) name 缺失 → 專屬錯誤訊息
- [ ] fixture 含「已存在、手動排版過」的 apm.yml(舊坑 1)
- 驗證:PASS

### 步驟 8 — CLI 攔截 + lockfile provenance(mkt-029, mkt-031)
- [ ] `cmd/apm/install.go`:packages[] 逐一過 `ParseRef`,命中 → `ResolvePlugin` → canonical 進既有流程;`DepRef` 非 nil 優先用結構化參照
- [ ] `internal/lockfile`:LockedDep 加 4 個 provenance 欄位,**五處顯式清單改齊**(parse switch / entryFieldOrder / serializeEntry fields / knownEntryFields / depSemanticEqual,見 design);往返測試含「已含 provenance 的 lockfile round-trip 不雙重輸出」;`UniqueKey()` 不含 provenance 的鎖定測試
- [ ] 測試:marketplace 安裝後 lockfile 有 `discovered_via`/`marketplace_plugin_name`;kind=url marketplace 才有 `source_url`/`source_digest`
- 驗證:PASS

### 步驟 9 — mkt-032 provenance carry-forward ⚠️修正原版 bug(Go 變體)
> 前提已實際追碼修正:Go 單趟寫 lockfile+apm.yml、無 target 硬閘門,Python 的中止路徑不存在;真正風險是 `buildLockfile` 從零重建導致裸 `apm install` 抹掉 provenance(見 design mkt-032 節)。
- [ ] `buildLockfile`:CLI marketplace ref 的 provenance 附加到對應 `LockedDep`
- [ ] **carry-forward**:新 entry provenance 為空且 existingLock 同 `UniqueKey()` entry 有值 → 四欄複製
- [ ] `IsSemanticEqual` 對 provenance 的參與行為定義 + 鎖定測試
- [ ] **回歸測試(AC4,三段)**:(a) no-target `install pkg@mkt` → lockfile 已含 provenance(deploy 跳過);(b) 裸 `apm install` → provenance 原封保留;(c) `--target x` 重跑 → 仍在。測試註解引用 checklist mkt-032 + design 的 Go 變體分析
- 驗證:PASS(Review Gate B)

### 步驟 10 — A/B 測試(AC5)
- [ ] `D:\Projects\apm-dev\evals\ab_marketplace_install.py`(對齊 `ab_mcp_install.py` 慣例)
- [ ] 涵蓋:fall-through 辨識(`owner/repo`、`owner/repo@alias`)、marketplace 安裝成功、`#ref`、semver-range-in-ref 拒絕、mkt-022 負向;**例外清單**:`pkg@mkt#feature/branch`(Go 修正 quirk)、mkt-032 流程(Go 修正 bug)——註明預期分歧
- 驗證:對照 `uv run apm` 通過(例外項除外)

### 步驟 11 — 全域驗證
- [ ] `go build/vet/gofmt/test ./... -cover` 全綠
- [ ] 至少一輪 codex exec 唯讀審查,修正發現的問題並補回歸測試
- [ ] checklist mkt-020~035 逐條打勾(mkt-032 標 fixed)

## Review Gates
- **A**(步驟 5 後):fail-closed 閘門先於網路探測的證明方式是否可信(fake 網路層 panic 斷言,不是時序推測)
- **B**(步驟 9 後):mkt-032 修正的原子性——target 失敗時 apm.yml/lockfile 均無殘留
- **C**(步驟 10 後):A/B 例外清單是否每項都有 deviation 依據(引用 checklist 條目),不是掩蓋失敗

## Rollback Points
步驟 1-6 全在 `internal/marketplace` 新檔案,零影響。步驟 7 起碰 `depref.go`/`resolver`/`lockfile`/`install.go`——每步獨立 commit,`git revert` 可逐步回退。

## 已知延後項目
- Phase M6 `apm search`(stretch):時間允許才做,做的話補 `cmd/apm/search.go` + 快取 fetch + `--limit`,並加「`marketplace search` 子指令不存在」負向測試(mkt-060/070)
- `apm uninstall pkg@mkt`/`apm view pkg@mkt`:明確不在本子任務(parent prd Non-Goals)
