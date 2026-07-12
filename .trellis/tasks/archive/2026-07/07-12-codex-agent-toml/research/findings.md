# Research: codex agents 部署 MD→TOML 轉換缺失（live 驗證發現）

- **發現方式**: 2026-07-12 使用者於 `D:/Projects/apm-dev/evals/test1` 以最新
  binary（vcs.revision `53acf46`）跑 `install --target copilot,claude,opencode,codex,antigravity`
  後，主 session 逐檔驗證輸出。
- **Date**: 2026-07-12（主 session 直接查證，來源皆附 file:line）

## 缺陷

`.codex/agents/accessibility-runtime-tester.toml` 副檔名為 TOML，內容卻是
markdown 原文（YAML frontmatter + body）。lockfile 中其 sha256 與其他
byte-copy 目的地完全相同（`53d57ab…`），證實是未轉換的複製。非合法 TOML，
Codex CLI 無法解析。

## 兩邊實作對照（file:line）

| 端 | 位置 | 行為 |
|---|---|---|
| apm-go | `internal/deploy/codex.go:20` | `deployFileToPath(p, ".codex/agents/<p.Name>.toml", ...)` —— byte-copy |
| Python | `apm/src/apm_cli/integration/agent_integrator.py:302-335`（`_write_codex_agent`） | 真轉換（見下） |
| Python 分派 | `agent_integrator.py:168-169` | `format_id == "codex_agent"` → `_write_codex_agent`；claude/opencode/copilot 走 `copy_agent`（copy 為 parity，無需動） |
| Python mapping | `integration/targets.py:695` | `"agents": PrimitiveMapping("agents", ".toml", "codex_agent")` |

## Python `_write_codex_agent` 精確語意（oracle 契約）

1. 拒讀 symlink 來源（`source.is_symlink()` → raise）。
2. `name` 預設 = 檔名 stem，若以 `.agent` 結尾則剝除（`accessibility-runtime-tester.agent.md` → `accessibility-runtime-tester`）。
3. frontmatter regex `^---\s*\n(.*?)\n---\s*\n?`（DOTALL）match 開頭；有 match 時
   body = match 之後全文，frontmatter 以 YAML safe_load 解析，
   `name`/`description` 覆蓋預設值；**解析失敗靜默忽略**（except: pass，
   body 仍已切掉 frontmatter）。其餘 frontmatter key（model/tools 等）忽略。
4. `description` 預設 `""`。
5. 輸出 TOML 文件三鍵：`{name, description, developer_instructions}`，
   `developer_instructions` = body `.strip()`。
6. 無 frontmatter 時 body = 全文（strip 後進 developer_instructions）。

## apm-go 落地注意

- apm-go 已有 TOML 依賴（codex MCP 的 `config.toml` writer，07-02 任務）——
  重用同一個 TOML 庫序列化，不新增依賴。
- **A/B 比對基準是「解析後語意相等」不是 byte 相等**：Python `toml.dumps` 與
  Go TOML 庫的字串跳脫/多行格式可能不同；驗證應 parse 兩邊 TOML 比
  key set + 三個值。
- 部署檔名 `<p.Name>.toml` 兩邊已一致（stem 剝 `.agent`），不必動。
- symlink 拒讀：apm-go 收集層已有 symlink 防護；轉換函式層是否需要再加
  防護以鏡射 oracle，由 checklist 定。
- 下游不變式：lockfile provenance 記錄的是部署後檔案 hash（generic），
  轉換後 hash 改變不影響 uninstall/audit 機制本身；既有部署過的
  markdown-in-toml 檔在重新 install 時會被覆寫為合法 TOML。

## 範圍

- 只修 codex agents 的轉換；claude/opencode/copilot 的 copy 行為已是 parity，不碰。
- 不動 evals 既有腳本；oracle A/B 以 checklist 內的一次性 scratch 對照為準
  （parse-TOML 語意比對）。
