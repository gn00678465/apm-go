# codex agents MD→TOML 轉換 parity

## Goal

修復 `internal/deploy/codex.go` 對 agents primitive 的部署：現況 byte-copy
markdown 內容到 `.codex/agents/<name>.toml`（非合法 TOML，Codex CLI 無法
解析），改為對照 Python `_write_codex_agent` 的真轉換。

## 背景

live 驗證發現的既有缺口（binary `53acf46`，evals/test1 fixture），完整證據
與 oracle 精確語意見 `research/findings.md`（含兩邊 file:line 對照）。
根因：`codex.go:20` 用 `deployFileToPath`；Python 是
`agent_integrator.py:302 _write_codex_agent`（frontmatter 取 name/description、
body → developer_instructions、TOML 序列化）。

## Requirements

- codex adapter 的 `TypeAgents` 部署改為 MD→TOML 轉換，語意精確對照
  `research/findings.md` §「Python `_write_codex_agent` 精確語意」六點
  （name 剝 `.agent` fallback、frontmatter 解析失敗靜默、description 預設空、
  body strip、無 frontmatter 全文入 developer_instructions）。
- 重用 repo 既有 TOML 依賴（codex MCP config.toml writer 同款），不新增依賴。
- claude/opencode/copilot 的 agents copy 行為是 parity，不得更動。
- 部署檔名 `<p.Name>.toml` 維持不變。
- lockfile provenance / uninstall / audit 走既有 generic 機制，不需特例。

## Acceptance Criteria

- [x] TDD 先紅後綠：轉換函式 unit 測試（含 frontmatter 有/無、YAML 壞損
      靜默、name fallback、body strip）+ 部署層測試斷言 `.codex/agents/*.toml`
      可被 TOML parser 解析且三鍵齊全
      【紅燈以 stash -u 移除實作重現（8 處 undefined）；codex 獨立重現確認】
- [x] oracle A/B（scratch 一次性）：同 fixture 兩邊部署，**解析後語意相等**
      （key set + name/description/developer_instructions 三值；非 byte 比對）
      【三 fixture（frontmatter-override / malformed-YAML / no-frontmatter）
      全符；實作 agent 與 codex 驗證者各自獨立實跑】
- [x] 全 repo `go build/vet/test ./...` 綠；觸碰檔 gofmt 乾淨
      【18 套件綠；internal/deploy 88.3%】
- [x] evals/test1 場景重跑：`.codex/agents/*.toml` 為合法 TOML；audit 通過
      【scratch 複本重現，原目錄未動；三值正確、audit 8/8】
- [x] spec 記錄：codex agents 轉換契約落檔【install-marketplace-contracts.md
      §9，commit `197fe98`；CAT-18 驗證腳本 PASS】

## 完成記錄（2026-07-12）

- 修復 commit：`197fe98`（codex_agent.go 轉換 + codex.go 一 case 改路由 +
  9 個新測試；重用 go-toml/v2 與 claudeFrontmatterRE，零新依賴）。
- 硬性 checklist（21 項）codex 對抗性驗證：第 1 輪 CONFIRMED 20/FAIL 0/
  DEFERRED 1（CAT-18 待 SHA），commit 後補驗——最終 21/21。
  過程另修 CAT-03/CAT-19 兩處 checklist harness 缺陷。
- 來源：使用者 live 測試（evals/test1）暴露的既有缺口；主 session 驗證輸出
  檔時以 lockfile hash 相同為證定位 byte-copy 根因。

## Non-Goals

- 不動 claude/opencode/copilot agents 部署。
- 不做 codex commands/其他 primitive 的新 parity 面。
- 不遷移使用者既有已部署的 markdown-in-toml 檔（重新 install 自然覆寫）。
