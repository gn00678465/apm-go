# 本地缺口圖 — apm-go MCP 端到端(req-mf-013 + mcp deploy)

驗證日期 2026-07-02。所有結論附 apm-go 現碼 `file:line` 證據。外部行為 oracle(原版 apm-cli §4.5)另見 `original-apm-mf013-mcp.md`。

## 前提校正(先前「tg/pr 偷懶未實作」判斷不精確)

| req | 狀態 | 證據 |
|---|---|---|
| req-tg-001/002/003 | ✅ 已實作(非 mcp primitives) | `internal/deploy/adapter.go`、`agentskills.go` 直接註解引用;`go test ./internal/deploy/...` = ok |
| req-pr-001/002/003 | ✅ 已實作 | `internal/deploy/conflict.go`(pr-002 local wins、pr-003 first-declared wins);`deploy.go:35,39` |
| **req-mf-013** | ❌ 真缺口 | 見下 §1 |
| **MCP target deploy** | ❌ 真缺口 | 見下 §2/§3 |

tg/pr 主體已完成且測過;真缺口集中在 **MCP 這條 primitive 從未接上**。

## §1 req-mf-013 — 只有 recognition,無 resolution

- `internal/manifest/mcp.go:110` 註解自陳「recognition only, no parse-time rejection」。
- 現有 regex(`mcp.go:112-116`):
  - `EnvVarRe = \$\{(?:env:)?([A-Za-z_][A-Za-z0-9_]*)\}` — 認 `${VAR}` / `${env:VAR}`
  - `InputVarRe = \$\{input:([^}]+)\}` — 認 `${input:<id>}`
  - `ActionsRe = \$\{\{.*?\}\}` — 認 `${{…}}`
- `RecognizePlaceholders`(`mcp.go:132`)只回傳分類清單,**沒有任何解析/取值/寫出/拒寫邏輯**。
- spec 要求(`acceptance-checklist.md:78`):
  > 依**分派矩陣**解析 `${VAR}`/`${env:VAR}`/`${input:<id>}`;不支援的 placeholder 不得靜默當字面寫出,**發診斷並可拒寫**;GitHub Actions `${{…}}` 原樣保留
- ⚠️ regex 隱患(待 resolver 處理 precedence):`${{FOO}}` 開頭是 `${{`,`EnvVarRe` 需 `${` 後接 `env:` 或字母,故不會誤吃 `${{…}}`;但 resolver 必須**先遮 `ActionsRe` 再套 EnvVarRe/InputVarRe**,避免相鄰構造誤配。

## §2 MCP 解析後即丟棄 — Manifest 無留存(上游缺口,比預期更深)

- `internal/manifest/manifest.go:355-367`:`mcp:` 逐 entry `ParseMCPEntry` + `ValidateMCP`,但結果 `m` **未 append 到任何欄位,直接丟棄**。
- `Manifest` struct(`manifest.go:34-53`)**沒有 MCP 欄位**(只有 `ParsedDeps` / `ParsedDevDeps`,均為 `DependencyReference`,不含 `MCPDependency`)。
- 後果:MCP servers 目前只被「驗證正確性」,**不留存 → install 拿不到 → 無法 deploy**。

## §3 install 與 deploy 對 MCP 零引用

- `grep -niE "mcp" cmd/apm/install.go internal/deploy/*.go`(排除 _test)= **零命中**。
- `internal/deploy/primitive.go:11-18` 的 `PrimitiveType` 常數:instructions/agents/skills/commands/hooks/prompts — **無 mcp**。
- `collectFromAPMDir`(`primitive.go:73`)掃 `.apm/<subdir>/` 檔案型 primitive;MCP 是 manifest `mcp:` 宣告型,**不會經此收集**(與 advisor 約束 #3 一致,且更嚴重:連 Manifest 都沒存)。

## 真實 scope = MCP 端到端四段

1. **留存**:`Manifest` 加 MCP 欄位;`manifest.go:355-367` 改為 append(目前丟棄)。dependency 套件自身 manifest 的 `mcp:` 也要能收集。
2. **解析(mf-013)**:install 時依 dispatch matrix 解析 `${VAR}`/`${env:VAR}`/`${input:}`;不支援者發診斷/拒寫;`${{…}}` 保留。resolver 需處理 regex precedence。
3. **收集 + override**:MCP 併入 deploy pipeline,套用 pr-002(local wins)/pr-003(first-declared wins)。決策:MCP 是否成為新 `PrimitiveType`,或走獨立 MCP 收集路徑(manifest-declared 非 file-primitive,不天然流經 `collectFromAPMDir`)——留 design.md 定。
4. **寫出**:per-target adapter 寫 `mcp_config.json`(鍵 `mcpServers`、HTTP 欄位 `serverUrl`);merge vs overwrite 語意待 oracle 確認。

## 約束(來自 experimental-flags.md 與 advisor)

- **不得** gate 在 experimental 後:mf-013(phase 1)+ mcp deploy(phase 4)是 oracle MUST 且全本地,違反 `experimental-flags.md` 規則 #2(不得 gate graded behavior)。
- MCP override 必須**重用** pr-*,不另立 silo。
- 所有 89 req 在 oracle 仍 `test_status: TODO`(oracle 唯讀,不改);apm-go 進度以自身 Go 測試為準。
