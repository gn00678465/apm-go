# 裁決:conformance oracle vs 原版 apm-cli 0.21.0(MCP writer + mf-013)

背景研究 `original-apm-mf013-mcp.md`(原版行為)與 conformance oracle 在 MCP writer 上**分歧**。本檔記錄權威裁決,供 design.md / AC 定稿。**原則:apm-go 對 conformance kit 評分,oracle 為權威;apm-cli 為 parity 參考,分歧處以 oracle 為準。**

## 決定性證據

- `conformance/conformance-kit/oracle/targets/expected/antigravity.yaml:9`
  `mcp: { file: .agents/mcp_config.json, key: mcpServers, http_field: serverUrl, var_interpolation: false }`
- `conformance/conformance-kit/oracle/EXPECTATIONS.yaml:44-45`
  `targets._input { req:[req-tg-001/002/003], outcome: golden, expected_dir: expected }`
- `targets/_input/.apm/` 內容 = agents/commands/instructions/skills,**無 mcp**。
- `EXPECTATIONS.yaml` grep `mf-013` / `mcp` / `input:` = **零命中**。

## 裁決表

| 議題 | 原版 apm-cli 0.21.0 | oracle / checklist | **apm-go 採用** |
|---|---|---|---|
| antigravity HTTP 欄位 | `httpUrl`(http/streamable)/`url`(sse) | `serverUrl`(`antigravity.yaml:9`) | **`serverUrl`** |
| antigravity mcp 檔/鍵 | `.agents/mcp_config.json` / `mcpServers` | 同 | 同 |
| var interpolation | legacy 於 install 解析(部分) | `var_interpolation: false` | **install 時解析,no-interp target 不留 undefined 字面** |
| `${input:}` 非互動 | warn + 寫字面(不 prompt/不 raise) | 「不得靜默當字面;發診斷並可拒寫」 | **發診斷 + 拒寫該 server(fail-closed)** |
| undefined `${VAR}`(env dict) | 留字面 `${VAR}` on disk | 「不得靜默當字面」 | **發診斷,不靜默留字面**(design 定 omit 或 refuse) |
| `${{…}}` | 逐字保留(regex 天然不吃內層 `{`) | 原樣保留 | **逐字保留** |
| 不支援 placeholder | warn + 寫字面 | 「發診斷並可拒寫」 | **發診斷(+可拒寫)** |
| precedence | root-first + dedup first-wins by name | pr-002/pr-003 | **重用 pr-002/003** |

## Scope 收窄(重要)

- **oracle 具體只為 antigravity 定義 mcp writer 格式**;claude/codex/copilot/opencode 的 `expected/*.yaml` **無 mcp**,agent-skills 明列 mcp `not_deployed`。
- `_input` fixture 無 mcp server → 現行 golden **不產出任何 mcp_config.json**;antigravity `mcp:` 行是**格式描述**,非 golden 輸出。
- mf-013 與 mcp 目前**無 graded fixture**(oracle 唯讀,不可補)→ **驗證靠 apm-go 自身測試**(unit + 依 descriptor 的 golden + 負向)+ 外部 sub-agent/codex 驗證。A/B 對 apm-cli 僅供 parity,且 writer 欄位分歧(serverUrl vs httpUrl),**不作為 writer 格式權威**。

## 對 PRD/design 的結論

1. **必做(oracle 對齊)**:mf-013 resolution **依分派矩陣(per-dispatch-matrix,§4.5「依分派矩陣」)** + **antigravity** mcp writer(serverUrl / resolve-at-install)。
   - ⚠️ **關鍵更正**:mf-013 **非 target-agnostic**。分派矩陣(見上表 + `original-apm-mf013-mcp.md`):**resolve-at-install target**(antigravity/gemini/claude/codex,`var_interpolation:false`)→ install 時解析、undefined 發診斷、`${input:}` 可拒寫;**interpolating target**(copilot,translate 模式)→ `${VAR}`/`${input:}` **逐字保留留 runtime**,不解析、不拒寫。故 AC2/AC3 的「發診斷/拒寫」只適用 resolve-at-install target;interpolating target 逐字 passthrough 才是正確輸出。
2. **延伸(apm-cli parity,非 oracle-graded)**:claude(`.mcp.json`)/codex(`.codex/config.toml` TOML,鍵 `mcp_servers`)/copilot mcp writer,格式各異——列為後續或 design 標註,不列入本任務 MUST。
3. resolver 語意比原版嚴(不照抄 undefined→字面 的 bug);`${input:}` fail-closed。
