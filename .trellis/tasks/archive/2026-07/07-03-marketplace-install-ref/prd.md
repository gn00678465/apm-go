# install <plugin>@<marketplace> 解析與部署整合

Parent: `.trellis/tasks/07-03-marketplace-ecosystem`(完整背景、範圍決定、跨子任務 Non-Goals 見 parent prd.md)
權威清單:`.trellis/tasks/07-03-marketplace-ecosystem/marketplace-checklist.md` Phase M2(+ 視時間決定是否納入 Phase M6 stretch)

## 前置依賴

依賴子任務 `07-03-marketplace-consumer` 的 `~/.apm/marketplaces.json` 登錄檔讀取與 fetch(github/gitlab/git/local/url 五種 source kind)邏輯——不要重新實作一份,直接複用。

## Goal

在 `cmd/apm/install.go` 的依賴字串解析加入 `PLUGIN@MARKETPLACE[#REF]` 語法辨識,解析出的依賴收斂成一般 git/local 依賴(不新增 primitive 型別),並在 lockfile 寫入 marketplace provenance 附加欄位。同時修正 Python 原版兩段式安裝流程會遺失 provenance 的已知 bug。

## Requirements

實作 checklist `mkt-020` ~ `mkt-035`(見 marketplace-checklist.md Phase M2,含 2026-07-03 複審新增的 mkt-033/034/035),重點:

- `PLUGIN@MARKETPLACE`/`PLUGIN@MARKETPLACE#REF` 語法辨識,`#` 之前含 `/` 或 `:` 一律不攔截(falls through 到一般 git 依賴解析);**攔截規則實作在「先切 `#` 再檢查」的 resolver 語意上**——不複製原版 install 層檢查整條字串的 quirk(`pkg@mkt#feature/branch` 在原版 install 會壞),此為刻意 deviation,A/B 測試需列例外(mkt-020 修訂版)
- `#REF` 拒絕 semver range 字元(`[~^<>=!]`),只接受原始 git ref——**此限制僅限 CLI `#REF` 後綴**,不可套到 dict 形式的 `version:`(mkt-021 修訂版)
- **apm.yml dict 形式 marketplace 依賴**(mkt-033,新):`{name: X, marketplace: Y, version: Z}`,root 與傳遞依賴都要解析;`version:` 支援 semver range(對真實 tag 解析、無相符且非嚴格 range 時回退原始 ref);`marketplace` 鍵不可與 `git`/`path`/`registry`/`id` 併用、未知鍵拒絕;字串形式 `pkg@mkt` 在 apm.yml **拒絕**(負向測試)
- Marketplace 名稱查詢**只**對照全域登錄檔,**不**讀當前專案自己的 `apm.yml`(反直覺行為,需要負向測試)
- `plugin.source` 各種形狀(相對路徑/github/git-subdir/gitlab/url dict)映射到一般依賴參照;`npm` 形狀拒絕(注意 mkt-026 修訂版的雙層行為:`type: npm` 在 manifest 解析就丟棄)
- Cross-repo dependency-confusion fail-closed 閘門(enterprise GitHub 家族 + 未限定 host 的裸 `repo:` 依賴),拒絕先於任何網路探測
- **ref-swap 與 shadow 偵測**(mkt-034,新):記錄 `(marketplace, plugin, version)` → ref 的 pin,變更時警告;同名 plugin 出現在其他已註冊 marketplace 時警告(偵測失敗不得中斷安裝)
- **註冊 ref 傳播**(mkt-035,新):marketplace 以非 main/HEAD ref 註冊時,相對字串 source 的 plugin canonical 自動附加該 ref
- `apm.yml` 只寫已解析的純參照,未解析狀態不可序列化(不變式守衛)
- Lockfile provenance 附加欄位(`discovered_via`/`marketplace_plugin_name`/`source_url`/`source_digest`),純附加不影響依賴身分(mkt-031 修訂版:`source_url`/`source_digest` 只在 kind=url 的 marketplace 有值)

## Non-Goals / 刻意改善(不移植原版 bug)

- `mkt-032`:**必須修正**原版「兩段式安裝流程遺失 provenance」的資料遺失 bug,不可原樣複製——設計時就要避免這個路徑(例如要求同一次呼叫帶齊 marketplace 參照 + target,或把 provenance 暫存起來跨呼叫保留)
- Phase M6(`apm search`)是 stretch,時間不夠時可以先不做,留給後續獨立任務

## Acceptance Criteria

- [ ] AC1:`mkt-020`~`mkt-031`、`mkt-033`~`mkt-035` 全部勾選,各自有測試佐證
- [ ] AC1b:mkt-033 的兩個負向測試(apm.yml 字串形式 `pkg@mkt` 拒絕;dict 形式 semver range 可解析)通過
- [ ] AC2:mkt-022(不讀本地 apm.yml)有明確負向測試
- [ ] AC3:mkt-028(cross-repo 閘門)有安全性負向測試,且驗證閘門在任何網路呼叫之前就擋下
- [ ] AC4:mkt-032 有回歸測試,模擬原版會遺失 provenance 的兩段式安裝流程,證明 Go 版本不會重現
- [ ] AC5:對照 Python 原版的 A/B 測試,至少涵蓋:一般 install(語法辨識正確 fall-through)、marketplace 安裝成功案例、`#ref` 語法、semver-range-in-ref 的拒絕案例;腳本放 `D:\Projects\apm-dev\evals`
- [ ] AC6:`go build/vet/gofmt/test ./... -cover` 全綠

## Notes

- 這個子任務跟 install.go 既有的依賴字串解析邏輯耦合最深,實作前建議先讀一遍 `cmd/apm/install.go` 目前的解析分派邏輯,找到正確的插入點,不要另開一條平行路徑。
