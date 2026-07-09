# All-Changes Checklist — install / uninstall 修正 session

本 session 於 `feat/marketplace-install` 上的**全部修改**驗收清單。分兩批:
(A) review-forge 多模型審查修正,(B) 真實套件 install↔uninstall 修正。

- `[x]` = 已實作且已驗證(附證據)。`[ ]` = 未做/延後。
- 驗證方式:單元/整合測試(`go test`)、真 CLI 實跑、與 Python 原版 A/B。
- 全域回歸基準:`go test ./... -count=1` → **全綠**(每個 commit 後複驗)。
- 已知環境限制:本機無 C compiler,`-race` 無法執行 → 以非 race 全套件替代,建議 CI 補跑。

---

## A. review-forge 多模型審查修正

多模型(codex-gpt-5 / gemini-3.5)review → synthesize → cross-vote → report → fix → verify。
產物於 `.review/marketplace-install/`(gitignored,未提交)。每項經 revert-verify(抽掉 fix →
測試轉 RED)證明 load-bearing。

### [x] MI1 — resolver:marketplace 子依賴 placeholder key 殘留 · `f58758c`
- 檔案:`internal/resolver/resolver.go`、`marketplace_resolve_test.go`
- 問題:transitive marketplace subDep 以未解析 placeholder key 記入 `childrenOf`,但 `processed`
  用解析後真實 key → parent re-pin 時 `invalidateChildren` 刪不到 → 殘留已不可達套件
- 修法:child loop 內先解析 marketplace subDep,使 childrenOf/processed key 一致
- 驗證:`TestResolve_FixpointReExpansion_MarketplaceChild` RED→GREEN;revert-verify 確認 load-bearing;
  獨立對抗性 agent CONFIRMED-FIXED;resolver 全套 + 全 repo 綠

### [x] MI2 — install 定位式去重 identity 不一致 · `ac5b5b4`
- 檔案:`cmd/apm/install.go`、`install_test.go`
- 問題:去重 map 用裸 `RepoURL`,他處用含 VirtualPath 的 `DepRefKey` → 同 monorepo 第二個
  virtual-path 套件被靜默略過、never appended
- 修法:去重改用 `deploy.DepRefKey`(跳過 local/parent 空 key),skip 檢查對齊同一 key
- 驗證:`TestRunInstall_PositionalDedup_KeysByVirtualPath` + `_TrueDuplicateStillSkipped` RED→GREEN;
  revert-verify load-bearing;獨立對抗性 CONFIRMED-FIXED

### [x] MI4 — marketplace registry/pins 原子提交無 rename 重試 · `0a7706c`
- 檔案:`internal/marketplace/rename.go`(新)、`rename_test.go`(新)、`registry.go`、`pins.go`
- 問題:Windows 上目標檔被 AV/並行短暫鎖定 → `os.Rename` ACCESS_DENIED/SHARING_VIOLATION 失敗
- 修法:`renameWithRetry`(3 次/60ms、可注入 renameFn),接入 SaveRegistry + saveRefPins
- 驗證:transient-then-success / always-fail 兩測;獨立對抗性 CONFIRMED-FIXED

### [x] MI6 — resolver:共用依賴 re-pin 後遺失 VirtualPath · `f58758c`
- 檔案:`internal/resolver/resolver.go`、`resolver_test.go`
- 問題(驗證期新發現的既有 bug):同一 child 被兩獨立 parent 依賴,其一 re-pin →
  `invalidateChildren` 刪 `depRefs` 但 `kinds` gate 擋住重填 → 存活共用 dep 的 `RepoURL` 未 trim、
  `VirtualPath` 遺失
- 修法:`depRefs` 重填自 first-seen `kinds` gate 解耦(`else if !ok` 重填)
- 驗證:`TestResolve_SharedChildAcrossIndependentParents_KeepsVirtualPath` RED→GREEN;revert-verify
  load-bearing(以純 git dep 同形重現,證明與 MI1 正交)

### [x] MI3 — un-054 共用部署檔(決策:接受) · `66c58ed`
- 檔案:`uninstall-checklist.md`(記錄限制段)
- 兩模型確認缺口真實,但已文件化(un-054、與 Python parity)。**本輪決策=接受不補實作**
  (觸發窄、Phase 2 重整屬獨立 task)。如需完整正確性另開 follow-up 依 design.md N2

### [x] MI5 — standalone/stale MCP 兩路徑「勿合併」不變式(決策:記 spec) · `66c58ed`
- 檔案:`design.md` N7
- 非 bug;記錄設計不變式避免後續 refactor 誤合併 `removeUninstallStandaloneMCP` 與
  `computeUninstallStaleMCP`

---

## B. 真實套件 install↔uninstall 修正(由真 CLI 實測逼出)

> 背景:先前「verified」用的是合成 block-style fixture;實跑 `install/uninstall mattpocock/skills`
> 才暴露以下三個真 bug。本批全部以**真 binary + 真套件 + Python A/B** 驗證。

### [x] #1 — uninstall 撞 flow-style apm.yml 崩潰 · `4e4d0ef`
- 檔案:`internal/yamlcore/splice_sequence_flow.go`(新)、`internal/manifest/remove.go`、
  `mcp_remove.go` + flow 測試 ×2
- 問題:flow-style `dependencies.apm`/`mcp`(如 `apm: [pkg]`)在清空/部分移除時,
  `SpliceSequenceElement`/`ReplaceSequenceWithEmptyFlow` 拒絕 flow → 報
  「unexpected document shape while emptying」;涵蓋 4 條路徑(apm/mcp × empty/partial)
- 修法:新增 `RebuildSequenceValueDropping`(任意 style 整值重繪,documented fallback),
  接進 `removeSeqIndices`(partial 共用)與兩個 empty-case;block 保持 byte-identical
- 驗證:
  - [x] 15 個 manifest/yamlcore 單元測試 RED→GREEN(含 block byte-identical 回歸)
  - [x] 真 CLI 矩陣:flow single→`apm: []`、flow two-partial→`apm: [b]`、block 回歸保留、
    flow mcp standalone→`mcp: []`,皆 exit 0 且可重新 parse
  - [x] revert-verify:抽掉 fix → 測試真 RED

### [x] #2 — install 將 dependencies.apm 寫成 flow · `3e7955e`
- 檔案:`cmd/apm/install.go`、`install_persist_manifest_test.go`(新)
- 問題:沿用既有 flow 序列節點(如 scaffold `apm: []`)保留 FlowStyle → SafeDump 輸出
  `apm: [pkg]`,與 mcp/Python 不一致(也是 #1 flow 檔的來源之一)
- 修法:實際 append 時清除該序列 FlowStyle bit → block;未變更不動
- 驗證:
  - [x] 5 個 `persistPackagesToManifest` 單元測試 RED→GREEN(flow-empty→block、flow-with-entries→
    block、fresh→block、object form、mcp block 不受影響)
  - [x] 真 CLI E2E:`apm: []` 專案 install → block;install→uninstall 完整往返成功
  - [ ] apm.yml **dash 縮排**與 Python(ruamel flush-dash)不同 —— 屬 SafeDump-wide 既有差異、
    cosmetic、go-yaml 難精確對齊。**未處理,待決策**

### [x] #3 — install 部署 0 skills(缺 plugin.json 支援) · `6a555fa`
- 檔案:`internal/deploy/plugin.go`(新)、`plugin_test.go`(新)、`primitive.go`、`cmd/apm/install.go`
- 問題:具 `.claude-plugin/plugin.json` 的 Claude 外掛集合(如 mattpocock/skills,技能巢狀於
  `skills/<category>/<name>/`),apm-go 完全未讀 plugin.json → 部署 0 skills 卻回報成功
- 修法:`CollectDependencyPrimitives` 讀 plugin.json,依其 `skills`/`agents`/`commands` 宣告發出
  primitive(skills 以 leaf 名稱部署宣告目錄);宣告 skills 時停用舊單層掃描;install 對 0 檔依賴警告
- 安全防護(attacker-controlled manifest,mandatory):
  - [x] 拒絕絕對路徑、`..` 路徑段、非 module-contained 路徑(`archive.Contained`)、任何 symlink
    路徑成分(`os.Lstat` 走查,不跟隨)—— 對齊 Python `_is_within_plugin`
  - [x] 測試涵蓋 `..` escape / 絕對路徑 / symlink 目錄 / symlink file → 僅跳過該項,合法兄弟仍部署
- 驗證:
  - [x] 16 個 `internal/deploy` 單元測試 RED→GREEN(含 20-skill mattpocock 形狀)
  - [x] **真套件 A/B**:`apm-go install mattpocock/skills` 部署的 `.claude/skills` 集合與 Python
    原版**逐一相同(20 個)**;支援檔(DEEPENING.md 等)一併複製
  - [x] 完整往返:install(20 skills)→ uninstall → `.claude/skills` 清空、apm.yml `apm: []`、
    apm_modules 移除,exit 0
- 已知範圍(未實作,待決策):
  - [ ] agents/commands 為 flat-file **非遞迴**(Python 為遞迴 copytree)
  - [ ] plugin.json 的 `mcpServers` / `lspServers` / `hooks` 欄位未實作
  - [ ] install summary 印 `|-- N skill(s) -> .../<skill>/` 的 N 為該目錄檔數,語意易誤解(cosmetic)

---

## 全域驗證狀態

- [x] `go build ./...` → exit 0
- [x] `go vet ./...` → clean
- [x] `go test ./... -count=1` → **全套件 PASS**(committed tree 上複驗)
- [x] gofmt:所有變更檔以 LF blob 複驗 clean(Windows autocrlf 假陽性已排除)
- [ ] `-race`:本機無 C compiler,未執行 → **建議 CI 補跑**
- [x] 真 CLI 往返:`install mattpocock/skills` → `uninstall mattpocock/skills`(=Python 20 skills、清乾淨)

## Commits(本 session,`feat/marketplace-install`)

| Commit | 類型 | 內容 |
|---|---|---|
| `f58758c` | fix(resolver) | MI1 marketplace child key + MI6 共用依賴 depRefs |
| `ac5b5b4` | fix(install) | MI2 定位式去重 identity |
| `0a7706c` | fix(marketplace) | MI4 rename 重試 |
| `66c58ed` | docs(task) | MI3 接受決策 + MI5 不變式 |
| `4e4d0ef` | fix(uninstall) | #1 flow-style 序列移除 |
| `3e7955e` | fix(install) | #2 apm.yml 正規化 block |
| `6a555fa` | feat(install) | #3 Claude plugin.json 支援 |

## 待決策 / follow-up(未提交)

1. #2 apm.yml dash 縮排是否對齊 Python(SafeDump-wide,go-yaml 限制)
2. #3 plugin.json 的 mcp/lsp/hooks 欄位 + 遞迴 agents/commands
3. #3 install summary「N skill(s)」文案 cosmetic
4. `-race` 於 CI 補跑
5. MI3 un-054 Phase 2 重整(若要完整正確性)
