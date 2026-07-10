# Research: Antigravity CLI — Custom agents（`/agents`）靜態定義格式

- **Query**: 2026-07-05 研究結論認為 antigravity subagents 是 runtime-only（`define_subagent` 工具呼叫、無靜態檔案格式）。使用者回報 CLI 文件出現「Creating custom agents」章節——確認現在是否存在靜態/宣告式 custom agent 檔案格式；若有，捕捉完整 schema、路徑、呼叫方式，並評估 apm-go 是否能為 antigravity target 增加 `agents` primitive。
- **Scope**: external（官方文件 + 本機 `agy` 1.0.16 唯讀驗證 + 二進位 strings 分析）
- **Date**: 2026-07-10

---

## 結論（一句話）

**是，現在有靜態格式了。** Antigravity CLI（非 IDE）支援以 `agent.md`（YAML frontmatter + markdown body）宣告 custom agent，workspace scope 為 `.agents/agents/<name>/agent.md`、global scope 為 `~/.gemini/config/agents/<name>/agent.md`，由 CLI 啟動時自動掃描、透過 TUI 內 `/agents` 面板選用。此為 CLI 專屬新面；IDE（Antigravity 2.0）的 subagents 仍是 runtime-only `define_subagent`。**apm-go 可以把 `agents` primitive 映射上去**（同 claude target 的 `.claude/agents/*.md` 模式，但多一層 per-agent 目錄）。

---

## Official docs（verbatim quotes；fetch 2026-07-10，經 r.jina.ai reader，因原站為 Angular SPA）

### 1. `/docs/cli/commands/agents`（主要來源）

URL: https://antigravity.google/docs/cli/commands/agents

Overview：

> The `/agents` command opens the interactive **Agent Manager Panel**. This interface serves two distinct purposes:
> 1. **Custom Agent Selection & Discovery**: Choose between the default agent and custom workflow-specific agents, or discover where to define new agents locally and globally.
> 2. **Subagent Monitoring & Control**: Track, inspect, or terminate background subagents running concurrently during your active session.

> Antigravity CLI supports loading custom agent definitions with specialized system instructions and tool permissions.

Section「2. Creating custom agents」（全文關鍵段落）：

> The header of the `/agents` panel displays exact template locations for creating new custom agents:
> ```
> Create New Agents
>   Workspace: {workspace}/.agents/agents/{agent_name}/agent.md
>   Global: ~/.gemini/config/agents/{agent_name}/agent.md
> ```
> To create a custom agent that is available across all your workspaces and projects, place it under your global customization directory (`~/.gemini/config/agents/`). Create a directory matching your agent name and add an `agent.md` file with YAML frontmatter:
> ```
> mkdir -p ~/.gemini/config/agents/code-reviewer
> cat << 'EOF' > ~/.gemini/config/agents/code-reviewer/agent.md
> ---
> name: code-reviewer
> description: Rigorous code review specialist focusing on edge cases and security.
> ---
> You are an expert code reviewer. Analyze diffs carefully and verify edge cases.
> EOF
> ```
> When you reopen `/agents`, the CLI automatically discovers `code-reviewer` and lists it under **Available Agents**. If you need an agent scoped strictly to a single project repository, place it inside that workspace's `.agents/agents/` directory (for example, `/home/user/projects/my-app/.agents/agents/code-reviewer/agent.md`). You can also package and distribute custom agents inside Plugins.

切換行為（Switching between agents）：

> **Select**: Use `↑`/`↓` to highlight an agent (`Default agent` or a custom agent) under **Available Agents**, then press `Enter`. … **Apply & Exit**: Press `Esc` to close the panel and apply your selection.
> **Note:** If you are currently inside an active conversation, switching custom agents automatically forks your current session (`[ Switch will fork the current conversation on exit ]`) so you do not lose context.

Common mistakes 表格（佐證目錄層級是硬性要求）：

> | Putting custom agent markdown files directly inside `~/.gemini/config/agents/` or `.agents/` | The CLI scanner expects agents to live inside dedicated subdirectories under `~/.gemini/config/agents/<name>/` or `.agents/agents/<name>/` | Move your agent definition to `~/.gemini/config/agents/<name>/agent.md` |

### 2. `/docs/cli/plugins`（agent 可打包進 plugin）

URL: https://antigravity.google/docs/cli/plugins

> Plugins are namespaced bundles that package custom skills, background subagents, linting rules, Model Context Protocol definitions, and event hooks into a single deployable asset.

Plugin filesystem structure 中有一行：

> `├── agents/                     # Optional subagent definition templates`

### 3. `/docs/subagents`（IDE / Antigravity 2.0 頁——對照組，內容與 07-05 快照一致）

URL: https://antigravity.google/docs/subagents

> ### Custom Subagents
> Agents can define their own custom subagents dynamically using the `define_subagent` tool.
> * **Configuration**: Define a custom system prompt and specific toolsets for read-only, write (including running terminal commands), and subagent delegation capabilities.
> * **Scope**: Once defined, the custom subagent can be invoked repeatedly for the remainder of the conversation.

（內建 subagents 仍為 `research` / `browser` / `self`；`invoke_subagent` 工具生成；無任何靜態檔案格式描述——**IDE 側沒變**。）

### 4. `/docs/cli/subagents`（CLI 的 subagents 頁）

URL: https://antigravity.google/docs/cli/subagents

> **Tip:** You can also select and switch between custom agents (or fork conversations) from this panel. See the `/agents` command reference for full details on custom agent discovery and panel keybindings.

---

## Local `agy` verification（commands run + output；agy 1.0.16 @ `C:\Users\gn006\AppData\Local\agy\bin\agy.exe`）

1. `agy --version` → `1.0.16`。
2. `agy --help`（頂層 help）→ subcommands 只有 `changelog / help / install / models / plugin / plugins / update`。**沒有 `agents` 頂層子指令**——`/agents` 是 TUI 內 slash command，不是 CLI 子指令。
3. `agy help agents` → `Error: unknown subcommand: agents`（exit 1）。
4. `agy agents list` → **不會**報 unknown subcommand，而是直接啟動互動式 TUI session（背景執行時掛住，需手動終止）。結論：`agents list` 沒有非互動介面；勿在腳本中使用。
5. `agy changelog`（唯讀，顯示到 **1.1.0**，即上游已釋出比本機 1.0.16 新的版本）——兩條決定性條目：
   - **1.1.0**: 「Fixed the `/agents` view header displaying `agent.json` instead of `agent.md` when creating new subagents.」及「Fixed the `/agents` panel's `"Create New Agents"` section displaying the wrong global configuration directory (`~/.gemini/antigravity-cli/` instead of `~/.gemini/config/`), **ensuring users create global subagents in the location actively scanned during startup discovery**.」
   - **1.0.16**: 「Fixed dynamically defined subagents by **transitioning definitions from JSON to Markdown format**, fixing an issue where dynamically created subagents failed to invoke.」
6. 二進位 strings 分析（`tr -c '[:print:]' '\n' < agy.exe | grep …`，唯讀）：
   - 1.0.16 二進位內含面板模板字串 `{workspace}/.agents/agents/{agent_name}/agent.json` 與 `{appDataDir}/agents/{agent_name}/agent.json`（即 1.1.0 changelog 所修的兩個**顯示** bug：`agent.json` 應為 `agent.md`、`{appDataDir}`＝`~/.gemini/antigravity-cli/` 應為 `~/.gemini/config/`）。
   - 同時含 `agent.md`（與 `SKILL.md` 相鄰的檔名表）、`writing agent.md`、`formatting agent.md` 字串——佐證 1.0.16 實際定義格式已是 markdown（changelog 條目所述的 JSON→Markdown 轉換）。
   - 含 `Create New Agents`、`Available Agents`、`Default agent` 等面板字串——custom agent 選擇面板在 1.0.16 已存在。
7. 本機目錄（唯讀 ls）：
   - `~/.gemini/config/agents/` **不存在**（`Test-Path` False；使用者未建立任何 custom agent）。`~/.gemini/config/` 現有：`plugins/ projects/ sidecars/ config.json import_manifest.json mcp_config.json .migrated`。
   - `~/.gemini/antigravity-cli/agents/` 不存在；整個 `~/.gemini/` 遞迴搜尋找不到任何 `agent.md`。
   - 專案根 `D:\Projects\apm-dev\apm-go\.agents\` 不存在。
8. CLI 內建 skill `~/.gemini/antigravity-cli/builtin/skills/agy-customizations/`（1.0.16 隨附的官方 customization 指南）：Quick Reference 表只列 Rules / Skills / Plugins / Hooks / MCP Servers 五類，docs/ 底下也**沒有 agents.md**——1.0.16 出貨時的內建文件尚未涵蓋 custom agents（此為新面，文件補在網站上）。

---

## Agent definition schema（靜態格式，已存在）

| 面向 | 值 | 依據 |
|---|---|---|
| 檔案格式 | Markdown + YAML frontmatter，**檔名固定 `agent.md`** | docs 範例 + common-mistakes 表 |
| 目錄結構 | 每個 agent 一個目錄，目錄名＝agent 名：`agents/<name>/agent.md`；直接把 .md 放在 agents/ 根目錄**不會被掃到** | common-mistakes 表 |
| 已文件化 frontmatter 欄位 | `name`（string）、`description`（string）——**僅此兩欄有官方範例** | docs 範例 |
| Body | frontmatter 之後的 markdown 全文＝該 agent 的 system instructions | docs 範例（"You are an expert code reviewer…"） |
| 未文件化欄位 | Overview 提到 custom agent 可帶「specialized system instructions **and tool permissions**」，且 `define_subagent`（同一套定義、1.0.16 起持久化為 markdown）可設定 read-only/write/delegation toolsets——但 `agent.md` 的 tools/model 等欄位 key **官方頁面沒有給出**，未經實測不可假設 | docs overview + `/docs/subagents` |
| Workspace scope | `{workspace}/.agents/agents/{agent_name}/agent.md` | docs |
| Global scope | `~/.gemini/config/agents/{agent_name}/agent.md`（1.0.16 面板誤顯示為 `~/.gemini/antigravity-cli/`，1.1.0 修正；changelog 措辭表明啟動掃描的真實位置一直是 `~/.gemini/config/`） | docs + changelog 1.1.0 |
| Plugin scope | plugin bundle 內 optional `agents/` 目錄（"subagent definition templates"） | `/docs/cli/plugins` |
| 發現機制 | CLI 啟動時掃描（startup discovery）；TUI 內重開 `/agents` 即列出 | docs + changelog 1.1.0 |
| 呼叫/選用 | 互動式：TUI 打 `/agents` → ↑/↓ 選取 → Enter → Esc 套用；在進行中的對話切換會 **fork conversation**。無 `agy agents …` 非互動子指令（1.0.16 實測） | docs + 本機驗證 |
| 版本時間線 | ≤1.0.15：定義為 JSON（`agent.json`）→ **1.0.16**：定義轉為 Markdown（`agent.md`），但面板 header 仍顯示舊字串 → **1.1.0**：面板顯示修正為 `agent.md` + `~/.gemini/config/` | changelog + binary strings |

---

## Implications for apm-go（新 `agents` primitive mapping）

**可以部署，且映射非常直接。** apm-go 已有 `TypeAgents` primitive（`internal/deploy/primitive.go:15`，來源 `.apm/agents/`），claude/codex/copilot/opencode 四個 adapter 都支援；antigravity 目前明確排除（`internal/deploy/deploy_test.go:1078` 鎖定 `TypeAgents` 在 unsupported 清單）。對照 claude adapter 的做法（`internal/deploy/claude.go:23-24`：`deployFileToPath(p, fmt.Sprintf(".claude/agents/%s.md", p.Name), projectDir)`），antigravity 的映射只差一層目錄：

```go
case TypeAgents:
    return deployFileToPath(p, fmt.Sprintf(".agents/agents/%s/agent.md", p.Name), projectDir)
```

注意事項：

1. **格式相容性好**：antigravity `agent.md` 與 `.claude/agents/*.md` 同為「YAML frontmatter（name/description）+ markdown system prompt」。apm 來源的 agents primitive 原封複製即符合官方文件範例。
2. **多餘 frontmatter 欄位風險**：apm 來源 agent 若帶 claude 風格的 `tools:`/`model:` 欄位，antigravity 是否容忍未知欄位未文件化、未實測——與 apm-go 現行「原位元組複製、不轉換」路線一致的話就直接照抄，但需在驗收時實測一次（或先實測 antigravity 對未知 frontmatter key 的行為）。
3. **版本門檻**：靜態 markdown 格式自 CLI 1.0.16 起有效（1.0.15 以前是 JSON）；1.1.0 起面板提示才正確。部署產物對 ≥1.0.16 的 CLI 應可被發現（掃描位置與格式在 1.0.16 已就位，僅顯示字串有誤）——此推論來自 changelog 措辭，未在本機以真實 agent 檔案實測（見 Caveats）。
4. **僅 project scope**：global scope（`~/.gemini/config/agents/`）存在，但 apm-go 目前全 repo 無 user-scope 部署概念（見 07-05 研究 B 節），不受影響。
5. **需要同步更新的位置**（若實作）：`internal/deploy/antigravity.go`（SupportedTypes + DeployPrimitive case）、`internal/deploy/deploy_test.go:1078`（support matrix）、conformance golden `conformance/conformance-kit/oracle/targets/expected/antigravity.yaml`（若要記錄 agents 路徑）。
6. **上游 Python apm 尚無此 mapping**：`targets.py:642-687` 的 antigravity TargetProfile 沒有 `agents` PrimitiveMapping——apm-go 若先做，會超前上游（parity 決策點，非純技術問題）。

---

## Deltas vs 2026-07-05 research（`antigravity-settings.md` C 節 Subagents bullet）

07-05 快照寫的是：「子代理（research/browser/self 內建 + define_subagent 動態自訂）是**執行期工具呼叫**建立的，**沒有對應的靜態檔案格式**……佐證 apm-go/Python 兩邊都不提供 antigravity 的 agents primitive 是正確的（沒有東西可部署，不是遺漏）。」

2026-07-10 的差異：

1. **CLI 側出現靜態格式**：`/docs/cli/commands/agents` 新增（或 07-05 未查到）「2. Creating custom agents」章節，明確給出 `agent.md` + 兩個 scope 路徑。07-05 研究只查了 `/docs/subagents`（IDE 頁），沒有查 CLI 的 commands/agents 頁；且本機 CLI 1.0.16 的 changelog 顯示 JSON→Markdown 轉換正是在 1.0.16（本機安裝版本）發生、面板修正在 1.1.0——**時間上這是 07-05 前後才成形的新面**，07-05 的結論以當時證據論並沒有錯，但已過時。
2. **IDE 側未變**：`/docs/subagents` 今日重抓內容與 07-05 描述一致，`define_subagent` 仍是 runtime-only。「無靜態格式」的結論現在只對 IDE 成立，對 CLI 不成立。
3. **「沒有東西可部署」的推論失效**：antigravity（CLI）現在有可部署的 agents 面；apm-go 的 `deploy_test.go:1078` 排除 `TypeAgents` 的理由需要重新評估。
4. **附帶更正一個小點**：07-05 研究 Caveats 提到 user-scope 路徑三方說法不一（`~/.gemini/config/` vs `~/.gemini/antigravity-cli/`）。1.1.0 changelog 直接裁決了 agents 這一項：**啟動掃描的是 `~/.gemini/config/agents/`**，`~/.gemini/antigravity-cli/` 是面板顯示 bug。這強化了「`~/.gemini/config/` 是 customization 掃描根、`~/.gemini/antigravity-cli/` 是 CLI app-data」的分工模型。

---

## Caveats / Not found

- **frontmatter 完整 schema 未文件化**：官方頁只示範 `name` + `description`；是否支援 `tools`/`model`/toolset 限制等欄位（overview 說 custom agent 有「tool permissions」）沒有欄位級文件。未實測。
- **未做寫入式實測**：本任務限唯讀，沒有實際建立 `.agents/agents/<name>/agent.md` 驗證 1.0.16 掃描行為。「1.0.16 已可發現靜態 agent.md」是由 changelog 措辭（1.1.0 修的是*顯示*、1.0.16 完成 JSON→Markdown 轉換）與二進位字串（`writing agent.md` 等）推論的，建議實作前用本機 agy 建一個試驗 agent 確認（然後升級到 ≥1.1.0 再確認一次）。
- **`agy agents list` 副作用**：驗證過程中該指令啟動了一個互動 TUI session（已終止）；可能在 `~/.gemini/antigravity-cli/conversations/` 留下一筆空對話記錄。
- **`define_subagent` 動態定義與靜態 `agent.md` 的關係**：1.0.16 changelog 說動態定義「transitioned from JSON to Markdown」，暗示動態 subagent 也持久化為 agent.md（可能在 app-data 或 brain 目錄），但本機找不到任何 agent.md 樣本（使用者沒用過此功能），無法確認動態定義檔的實際落點與欄位。
- **`/docs/cli/commands/agents` 頁面何時上線無法確認**：無 archive 快照可查，只能確定 07-05 研究未涵蓋此 URL、今日內容如上。
- **plugin `agents/` 目錄的內部結構**（是否也是 `<name>/agent.md` 兩層）文件未明示，僅有目錄樹一行註解。
