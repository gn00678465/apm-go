# codex plan-review 記錄(2026-07-02)

planning 文件(prd/design/implement)由 codex exec 外部審查,4 輪迭代至 READY。原始輸出於 scratchpad `codex-plan-*-out.md`。

## Round 1 — 初審:NOT READY(1 CRIT + 6 HIGH + 4 MED + 1 LOW)

同時**確認 4 個關鍵決策正確**(不得改):antigravity `serverUrl`(oracle 權威)、「無 mf-013/mcp graded fixture」為真、dispatch matrix bake=antigravity/claude/codex、`ResolvePrimitives` 重用結構合理。

| 嚴重度 | 發現 | 處置 |
|---|---|---|
| CRITICAL | registry-backed MCP 無 command/url 無法部署 | 收窄:只部署 self-defined(`registry:false`),registry-backed 收集階段診斷+跳過 |
| HIGH | resolver `env func` 無法辨 undefined/empty | 改 `lookup func(string)(string,bool)` + 逐 position policy |
| HIGH | stale cleanup PRD 要求/design 延後(矛盾) | 兩處一致移出 MUST、明確 defer + parity risk |
| HIGH | 合併檔 lockfile/source 歸屬未定義(pr-001) | `Run` 從 `prims[].Source` 建 `MCPProvenance`,檔 hash 一次 |
| HIGH | dev MCP 不一致 | 留存 prod+dev、只部署 prod |
| HIGH | `--trust-transitive-mcp` 引用未規劃 | 改:一律跳過 transitive self-defined 發警告(不加 flag) |
| HIGH | 只 antigravity 0600 | 所有 bake writer(antigravity/claude/codex)一律 0600 |
| MED | `${input:}` fail-closed 未分 target | bake-only refuse;copilot 逐字保留 |
| MED | copilot 路徑誤標 apm-cli parity | 改標 apm-go project 延伸 |
| MED | SSE/streamable 映射不全 | design §5 per-target transport→欄位表 |
| MED | antigravity explicit-only E2E | 測試用 `--target antigravity` |
| LOW | AC 外部驗證非可重現 | review gate + committed 測試 |

## Round 2 — 再審:9/12 RESOLVED,3 PARTIAL

- #2 PARTIAL:undefined policy 漏 PosURL/PosHeader;copilot「不落盤」措辭矛盾 → 補五 position(url refuse、header omit)、修 copilot 為「逐字寫出+警告」。
- #4 PARTIAL:`WriteMCP` 簽章沒回傳 provenance 卻說會 store → 定案由 `Run` 從 `prims[].Source` 建,writer 不回傳。
- NEW:registry 診斷位置矛盾(collection vs writer)→ 統一在 collection,writer 只見 self-defined。

## Round 3 — B/C RESOLVED,A 剩 doc-sync

- B(provenance contract)、C(registry handoff)RESOLVED。
- A:design 的 PosURL/PosHeader 未帶進 implement step 2 / PRD AC2 → 補齊。

## Round 4 — READY

- AC2 措辭同步五 position(args verbatim-no-diag、url refuse、header omit)。
- codex 終判定:**READY,other blocking findings: none.**

## 驗證紀律

依 never-self-verify:planning 全程由 codex exec 外審,非自證。實作階段續用 review gate A/B/C(見 implement.md)。
