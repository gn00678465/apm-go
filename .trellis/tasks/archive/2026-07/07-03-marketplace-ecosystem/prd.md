# 新增 marketplace 生態系指令(消費端+發布端)

## 背景

apm-go 目前完全沒有 `apm marketplace` 指令組,也不支援 `apm install <plugin>@<marketplace>` 語法。`internal/manifest`/`internal/resolver` 雖然已經把 "marketplace" 列為一種依賴來源分類(`Source: "marketplace"`、`KindMarketplace`),但完全沒有下游解析/下載邏輯——純粹是分類標籤,沒有實作。

這是使用者主動發現、要求開新任務補上的落差,分支 `feat/marketplace-install`(從 `main` 切出)。範圍決定(已與使用者確認):**消費端 + 發布端全部**,不只是「能裝」,也要能「發布」。

**重要澄清**:OpenAPM v0.1 規範把 marketplace 明文列為「v0.1 非規範性,純 producer 端 authoring artifact」(`req-rs-008`),只有 `marketplace.packages[].source` 的欄位驗證(`req-mf-017`)是規範性要求。也就是說,本任務**不是**修補 OpenAPM 合規性缺口,而是跟 Python 原版 CLI 的**行為對齊**(parity),如同 `apm update`/`apm audit` 一樣屬於「原版有、但規範不強制」的指令。

## 完整驗收清單

見同目錄 `marketplace-checklist.md`——比照 `conformance/conformance-kit/acceptance-checklist.md` 格式,由背景研究 agent 對照 Python 原版原始碼 + 文件 + live CLI 實測(`uv run apm`,v0.21.0)逐條產出,含:

- Phase M0:資料模型與 `~/.apm/marketplaces.json` 登錄檔
- Phase M1:消費端子指令(add/list/browse/update/remove/validate)
- Phase M2:`install <plugin>@<marketplace>[#ref]` 解析與部署整合
- Phase M3:發布端子指令(init/check/outdated/audit/migrate/package add/remove/set)
- Phase M4:`apm pack`(marketplace.json 產生器,apm-go 目前完全沒有這個指令,需從零實作)
- Phase M5:文件落差修正(`marketplace search`/`doctor`/`publish`/`browse --json` 皆為原版文件錯誤,不可移植)
- Phase M6(stretch):`apm search QUERY@MARKETPLACE`
- Phase V:驗證完整性控制(沿用 acceptance-checklist.md 的反作弊控制,額外強調「已存在檔案」fixture 測試與負向測試)

清單同時記錄了 2 個**刻意不移植的原版真實 bug**(mkt-032:兩段式安裝流程遺失 marketplace provenance;mkt-046:`package add` 對本地來源實質上裝不進去),Go 版本要修正這兩個缺陷,不要原樣複製。

## 子任務拆分

因為範圍橫跨資料模型、CLI 指令、HTTP client(GitHub/GitLab API)、git sparse clone、YAML 保留註解編輯、semver/tag-pattern 工具、JSON schema 相容輸出,規模遠超單一任務,拆成 4 個子任務(依相依順序):

1. **`07-03-marketplace-consumer`**(Phase M0+M1):資料模型 + `~/.apm/marketplaces.json` 登錄檔 + add/list/browse/update/remove/validate 六個消費端子指令。無前置依賴,可先開始。
2. **`07-03-marketplace-install-ref`**(Phase M2,含 Phase M6 stretch):`install <plugin>@<marketplace>[#ref]` 解析、apm.yml dict 形式 marketplace 依賴(mkt-033)、resolver 整合、lockfile provenance、ref-swap/shadow 偵測(mkt-034)、(視時間)`apm search`。依賴子任務 1 的 fetch/登錄檔邏輯。
3. **`07-03-marketplace-authoring`**(Phase M3):init/check/outdated/audit/migrate/package(add/remove/set),操作本地 `apm.yml` 的 `marketplace:` 區塊。無前置依賴,可與子任務 1/2 平行進行。
4. **`07-03-marketplace-pack`**(Phase M4):`apm pack`(marketplace.json 產生器)。依賴子任務 3 產出的 `marketplace:` 區塊資料模型。

Phase M5(文件落差修正)不是獨立子任務,是每個子任務各自的「非目標」清單項(見各子任務 prd.md 的 Non-Goals)。

## Non-Goals(整個 marketplace 生態系,跨全部子任務)

- `marketplace search`/`marketplace doctor`/`marketplace publish` 子指令——實際不存在(search/doctor 是頂層獨立指令;publish 完全不存在。歸因細節見 checklist mkt-060~062:doctor/publish 是文件錯誤,search 的文件其實是對的、錯的是原版程式碼殘留字串)
- `marketplace browse --json`——原版不存在
- `MarketplacePlugin.registry` 欄位的**路由行為**——原版只出貨了解析層、無任何路由消費端;但 manifest 解析必須**容忍**含 `registry` 鍵的條目(見 checklist 範圍表,mkt-005)
- `apm uninstall pkg@mkt`/`apm view pkg@mkt`——原版存在但本輪不做,install 完成後另開任務補齊語法對稱性(見 checklist 範圍表)
- 完整複製上游 Claude-Code marketplace.json schema(hooks/mcpServers/lspServers/channels/userConfig/monitors)——那些是 Claude-Code 原生 plugin 功能,apm 只需相容輸出子集

## Acceptance Criteria

- [ ] AC1:`marketplace-checklist.md` 每一條 `mkt-XXX`(Phase M0-M4,不含 stretch M6)在對應子任務完成時勾選,並有可執行測試佐證
- [ ] AC2:Phase M5 各項文件落差(mkt-060~064)**不**出現在 Go 版本的指令樹/文件中(各自有測試斷言「這個位置不存在該子指令」或「存在於正確位置」)
- [ ] AC3:mkt-032、mkt-046 兩個原版已知 bug 在 Go 版本有回歸測試證明修正、不重現
- [ ] AC4:每個子任務各自 `go build/vet/gofmt/test ./... -cover` 全綠
- [ ] AC5:至少 Phase M1(消費端)與 Phase M2(install 整合)有對照 Python 原版的 A/B 測試(比照先前 `--mcp` 任務的 `evals/` 慣例,腳本放 `D:\Projects\apm-dev\evals`,不進 apm-go repo)

## Notes

- 4 個子任務用 `task.py create "<title>" --slug <name> --parent .trellis/tasks/07-03-marketplace-ecosystem` 建立,彼此依賴順序在子任務各自 prd.md 的「前置依賴」段落註記,不是 Trellis 的強制相依機制。
- 本 parent task 本身通常不直接承接實作,由子任務個別走 Plan→Execute→Finish 全流程。
