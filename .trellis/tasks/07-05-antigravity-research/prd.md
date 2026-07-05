# 研究並補齊 antigravity 各種設定

## Goal

徹底釐清 antigravity(Google agentic IDE/CLI, alias `agy`)的設定面,對照 Python
原版與官方文件,定案 apm-go 的兩個分歧,並產出後續實作 child 的缺口清單。

研究報告(已完成):`research/antigravity-settings.md`。

## 研究已產出的關鍵結論

1. **explicit-only vs auto-detect 分歧（真實,需定案）**
   - Python `target_detection.py:393` `EXPLICIT_ONLY_TARGETS={"agent-skills","antigravity"}`
     ——**explicit-only**,理由:`.agents/` 是跨工具共用目錄,無 antigravity 專屬目錄可偵測;
     `ALL_CANONICAL_TARGETS` 也不含它。
   - apm-go `adapter.go:79-90` 卻 **auto-detect** antigravity(signals: GEMINI.md/AGENTS.md)。
   - 待定:apm-go 的偏離是否該保留。核心論點——「antigravity IDE runtime 會自動讀
     AGENTS.md」≠「apm 該在偵測到 AGENTS.md 時自動部署到 antigravity」,因為 AGENTS.md /
     `.agents/` 也被 opencode、agent-skills 共用,auto-detect 會誤啟用 antigravity。
2. **疑似 bug:sse transport 欄位（需驗證定案）**
   - apm-go `mcp_antigravity.go` 對 sse 寫 `url`(有測試鎖 `mcp_writers_test.go:116-133`)。
   - 官方文件(2026-07-05 抓取)稱 sse/streamable-http/websocket 僅 `serverUrl` 合法,
     legacy `url`/`httpUrl` 不支援。
   - Python 端繼承 Gemini schema:sse→`url`、http→`httpUrl`(也非 `serverUrl`)。
   - 三方不一致——需確認官方現況並定 apm-go 該用哪個。
3. **primitive 覆蓋對照**:Python antigravity 支援 instructions(→rules,去 frontmatter)、
   skills、hooks、mcp;**無 agents/commands**(上游已併入 skills)。apm-go 現況相符
   (Instructions/Skills/Hooks + MCP)。
4. **AGENTS.md**:Python 用 `compile_family="agents"` + `agents_compiler.py` 產生
   AGENTS.md;**apm-go 完全沒有 compile 指令/套件**,AGENTS.md/GEMINI.md 只是唯讀偵測
   訊號,從不生成/更新。此為較大的結構性缺口(可能超出本 parent 範圍)。

## Requirements（本 child = 研究 + 定案,不含大型實作）

- 產出研究報告(已完成)並在本 PRD 記錄結論(已完成)。
- 定案兩個分歧,產出「決策 + 理由 + 建議行動」:
  - explicit-only vs auto-detect:建議 apm-go 對齊 Python 改回 explicit-only(附理由),
    或明確保留 auto-detect 並記為 documented deviation(附官方依據)。
  - sse `url` vs `serverUrl`:確認官方現況,決定是否修 mcp_antigravity.go(若修→小型 fix)。
- 產出 apm-go 缺口清單,標示各項屬「本輪修」「另開 task」「不做」。

## Acceptance Criteria

- [ ] 研究報告完成且結論可執行(已完成,待中樞複核)
- [ ] explicit-only vs auto-detect 有明確定案(對齊 Python / 保留 deviation 二擇一 + 理由)
- [ ] sse url/serverUrl 有明確定案(修 or 不修 + 依據);若修,附最小 fix + 回歸測試
- [ ] AGENTS.md 生成缺口有明確處置決定(本 parent 不做則記錄為後續 task)
- [ ] 缺口清單交付,供 parent 決定是否再開實作 child

## Non-Goals

- 不在本 child 實作 compile/AGENTS.md 生成(結構性大工程,另議)。
- sse 修正若成立僅做該最小 fix,不順手改其他 MCP adapter。
