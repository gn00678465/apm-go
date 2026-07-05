# apm pack(marketplace.json 產生器)

Parent: `.trellis/tasks/07-03-marketplace-ecosystem`(完整背景、範圍決定、跨子任務 Non-Goals 見 parent prd.md)
權威清單:`.trellis/tasks/07-03-marketplace-ecosystem/marketplace-checklist.md` Phase M4

## 前置依賴

依賴子任務 `07-03-marketplace-authoring` 產出的 `marketplace:` 區塊資料模型與 `packages[]` 結構——需要子任務 3 完成、有實際可用的 `marketplace:` 區塊之後才能開始。apm-go 目前**完全沒有** `apm pack` 指令,這是從零實作,不是修改既有指令。

## Goal

新增 `apm pack` 指令:讀取本地 `apm.yml` 的 `marketplace:` 區塊(或 legacy `marketplace.yml`,兩者互斥),解析 `packages[]`,輸出上游 Claude-Code marketplace.json schema 子集的 `marketplace.json` 檔案。

## Requirements

實作 checklist `mkt-050` ~ `mkt-055`(見 marketplace-checklist.md Phase M4,2026-07-03 複審後 mkt-050/052 已重寫、新增 mkt-054/055),重點:

- 結構轉換**不只**改名(mkt-050 修訂版):`packages:`→`plugins:` 之外,還有 source 字串→結構化 dict 合成(含解析後 ref+sha)、version 重寫(range 不原樣輸出)、APM 專用欄位剝除、pluginRoot 剝除
- 本地套件略過 git 驗證;遠端套件依 semver range/明確 ref 對照真實 git tag(`git ls-remote`)解析
- 輸出 schema(mkt-052 修訂版):Claude 輸出頂層 `name`/`owner` + 條件式 `description`/`version`/`metadata`;plugin 級 `name`/`source` + 條件式 `description`/`version`/`author`/`license`/`repository`/`tags`/`homepage`;**`category` 只出現在 Codex 輸出**
- `outputs` 含 `codex` 時,`category` 為每個 package 必填(config 載入層 + mapper 層雙重把關)
- 輸出位置(mkt-054,新):claude → `.claude-plugin/marketplace.json`、codex → `.agents/plugins/marketplace.json`,**不是 repo 根目錄**;兩者可同時輸出;路徑可覆寫
- 閘門與 exit code(mkt-055,新):0/1/2/3/4;branch ref/HEAD → HeadNotAllowed 錯誤

## Non-Goals

- 不需要完整實作上游 Claude-Code marketplace schema 的 `hooks`/`mcpServers`/`lspServers`/`channels`/`userConfig`/`monitors` 等原生欄位——那些是 Claude-Code plugin 功能,apm 本來就不碰,schema 本身是 informational、非 OpenAPM 規範性文件
- 不含發布到 git host 的機制(`git tag`/`gh release create` 等)——原版文件本身也說這是走 git host 原生機制,`apm pack --create-tag`/`--push` 在 OpenAPM v0.1 undefined(參照 acceptance-checklist.md Phase 7 註記)

## Acceptance Criteria

- [ ] AC1:`mkt-050`~`mkt-053` 全部勾選,各自有測試佐證
- [ ] AC2:輸出的 `marketplace.json` 通過(或至少不違反)上游 schema 子集的欄位形狀驗證
- [ ] AC3:本地/遠端套件混合的 `packages[]` 都能正確解析出 `plugins[]`
- [ ] AC4:對照 Python 原版的 A/B 測試,至少涵蓋子任務 3 scaffold 出的範例專案跑 `apm pack` 後 byte-level 或語意比對輸出;腳本放 `D:\Projects\apm-dev\evals`
- [ ] AC5:`go build/vet/gofmt/test ./... -cover` 全綠

## Notes

- 這是 4 個子任務中最後執行的一個,建議等子任務 3 完成、有真實 `marketplace:` 區塊可用之後再開始規劃細節(design.md)。
