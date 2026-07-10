# Research: Antigravity CLI（agy）— Agent Skills 格式、路徑與 plugin 關係

- **Query**: Antigravity CLI 的 Agent Skills：SKILL.md 格式與 frontmatter 欄位、專案/全域 scope 路徑（settle `~/.gemini/config/skills/` vs `~/.gemini/antigravity-cli/skills/`）、skills 與 plugins 的關係、enable/disable 與命名限制，以及對 apm-go 部署行為的影響。
- **Scope**: external（官方 docs via r.jina.ai）+ local read-only 實機驗證（agy 1.0.16 已安裝，含 binary strings 分析與 CLI 內建文件）
- **Date**: 2026-07-10

---

## Official docs（verbatim quotes；fetch 2026-07-10，經 r.jina.ai reader，原站為 Angular SPA、直接 WebFetch 為空殼）

### 1. https://antigravity.google/docs/cli/plugins（CLI Plugins & Skills 頁）

頁面標題順序：Plugins & skills → The extensibility model → Antigravity plugins → Plugin filesystem structure → The plugin manifest (plugin.json) → Managing plugins via CLI subcommands → **Agent skills** → Creating local workspace skills → Sharing global skills → Managing hooks → MCP → Next steps。

- Skills 定義：「Skills are declarative, human-readable markdown files that outline explicit instruction protocols, scripts, and target resources for specialized engineering tasks.」
- Slash command 化：「Once registered, **Skills convert automatically into slash commands** inside the TUI, allowing you to invoke them manually (e.g., typing `/refactor-ui`).」
- Local workspace skills（**注意：此頁描述的是「扁平 .md 檔」格式，與其他所有來源矛盾，見 Caveats**）：
  > "1. Create a directory named `.agents/skills/` at your project root. 2. Inside, draft a markdown file with a `.md` extension (such as `format-tests.md`). 3. Define the skill's Frontmatter metadata … 4. … When you run `agy` in this directory, the skill is compiled, and `/format-tests` becomes available in the prompt box."
- Global skills（**此頁寫的全域路徑與實機不符，見 Caveats**）：
  > "To share skills across all workspaces on your workstation, place the target markdown files inside your global configuration path: `~/.gemini/antigravity-cli/skills/`" … "Any markdown skill in this directory is automatically imported as a global slash command whenever you launch `agy` in any directory."
- Plugins 目錄（**同樣與實機不符**）：「`~/.gemini/antigravity-cli/plugins/<plugin_name>/`」，結構：`plugin.json`（required）+ 選配 `mcp_config.json` / `hooks.json` / `skills/` / `agents/` / `rules/`。
- plugin.json name 限制：「It must contain only alphanumeric characters, hyphens, and underscores (matches `^[a-zA-Z0-9-_]+$`).」
- Plugin 管理指令：`agy plugin list` / `install` / `enable|disable <name>` / `uninstall <name>`。

### 2. https://antigravity.google/docs/skills（通用 Agent Skills 頁）

- 定義：「Skills are reusable packages of knowledge that extend what the agent can do.」
- SKILL.md 範例（folder-based）：
  ```
  ---
  name: my-skill
  description: Helps with a specific task. Use when you need to do X or Y.
  ---
  # My Skill
  Detailed instructions for the agent go here.
  ```
- Frontmatter 欄位表（verbatim）：
  > "**name** | No | A unique identifier for the skill (lowercase, hyphens for spaces). Defaults to the folder name if not provided. | **description** | Yes | A clear description of what the skill does and when to use it."
  （即 `name` **選填**、缺省時 fallback 到資料夾名；`description` **必填**；此頁沒有記載任何其他 frontmatter 欄位。）
- 路徑表（verbatim）：「`<workspace-root>/.agents/skills/<skill-folder>/` | Workspace-specific | `~/.gemini/config/skills/<skill-folder>/` | Global (all workspaces)」
- 向後相容：「Antigravity now defaults to .agents/skills, but still maintains backward support for .agent/skills.」
- 選配子目錄：`scripts/`、`examples/`、`resources/`。
- 啟用機制（progressive disclosure）：「When a conversation starts, the agent sees a list of available skills with their names and descriptions」→「If a skill looks relevant to your task, the agent reads the full `SKILL.md` content.」無需顯式啟用，agent 自主決定；使用者也可點名。

---

## Local agy verification（agy 1.0.16 @ `C:\Users\gn006\AppData\Local\agy\bin\agy.exe`；全部 read-only；2026-07-10）

**1. `agy --help`**：頂層子命令只有 `changelog / help / install / models / plugin / plugins(alias) / update`。**沒有 `skills` 子命令**（`agy help skills` → `Error: unknown subcommand: skills`）。skill 管理在 TUI 內：binary strings 有「`Use /skills to browse and manage agent skills`」及 `commands.(*skillsCommand)` symbol —— `/skills` 是 TUI slash command，不是 CLI 子命令。

**2. `agy plugin --help`**：
```
list | import [source]（Import plugins from gemini or claude）| install <target>（supports plugin@marketplace）
uninstall <name> | enable <name> | disable <name> | validate [path] | link <mp> <target> | help
```
enable/disable 只有 **plugin 粒度**；沒有任何 per-skill enable/disable 指令。

**3. 實機目錄（`~/.gemini/`）**：
- `~/.gemini/config/` 存在，內含 **`plugins/`**（7 個已安裝 plugin：android-cli-plugin、chrome-devtools-plugin、firebase、google-antigravity-sdk、modern-web-guidance-plugin、review-forge、science）、`projects/`、`sidecars/`、`config.json`、`import_manifest.json`、`mcp_config.json`、**`.migrated`**（空 marker，暗示發生過路徑遷移）。
- `~/.gemini/config/skills/` **不存在**（Test-Path False）——本機沒裝過全域 standalone skill，但同層 `plugins/` 由 `agy plugin install` 實際寫入，證明 CLI 的全域 customization root 是 `~/.gemini/config/`。
- `~/.gemini/antigravity-cli/` 存在但**沒有** `skills/` 或 `plugins/`；內容是 CLI app data：`settings.json`、`keybindings.json`、`conversations/`、`log/`、`builtin/` 等。
- `~/.gemini/antigravity-cli/builtin/skills/{agy-customizations,antigravity_guide}/SKILL.md`：CLI 隨附的 built-in skills，本身就是 folder-based `SKILL.md` 格式（含 `docs/`、`references/` 子目錄）。

**4. `agy plugin list`** 輸出 import manifest JSON：review-forge 於 2026-06-12 從 `claude-code` import，`components: ["skills"]` —— plugin 是 skills 的容器之一。

**5. 實機 plugin 佈局**（`~/.gemini/config/plugins/review-forge/`）：
```
plugin.json
skills/review-forge/SKILL.md      ← plugin 內 skills 為 folder-based
skills/review-forge/{agents,assets,evals}/...
```
plugin.json 在野外有兩種形態：最小 `{"name": "review-forge"}`，以及 marketplace 版含 `version / description / author{name,email} / repository / license / keywords`（chrome-devtools-plugin 等）。

**6. `agy plugin validate ~/.gemini/config/plugins/review-forge`**：
```
[ok] ...\review-forge
  ✔ skills      : 1 processed
  - agents      : skipped (not found)
  - commands    : skipped (not found)
  - mcpServers  : skipped (not found)
  - hooks       : skipped (not found)
```
→ CLI 認得的 plugin 元件是 **skills / agents / commands / mcpServers / hooks** 五種（比 builtin 文件寫的 skills/rules/hooks/mcp 多出 agents 與 commands，對應 claude/gemini plugin import 相容）。

**7. Binary strings（`grep -a` on agy.exe）**——路徑模板直接寫死在二進位裡：
- `{workspace}/.agents/skills/{skill_name}/SKILL.md` ← workspace skill 路徑模板
- `{appDataDir}/skills/{skill_name}/SKILL.md` ← 全域 skill 路徑模板（appDataDir 見下）
- `~/.gemini/config`（多處，含「Global Discovery: ~/.gemini/config/」）；`AppDataDir` 是 protobuf 欄位（`app_data_dir`）
- `skills.json` / `plugins.json`（declared-config 檔名）、`enable-customization-skills`、`Reloading system slash commands and skills`

**8. CLI 內建文件（`~/.gemini/antigravity-cli/builtin/skills/agy-customizations/`，隨 1.0.16 出貨，等同離線官方文件）**：
- `SKILL.md`：Discovery locations —— workspace root 為 **`.agents/`（或 `.agent/`、`_agents/`、`_agent/`）**，agent 從 CWD 向上走到 repo root 逐層發現；全域為 **`~/.gemini/config/`**。載入優先序（高→低）：**Workspace Project → workspace 宣告的 `skills.json`/`plugins.json` → Global Discovery（`~/.gemini/config/`）→ Built-in → Global declared configs**；同名衝突由高優先者覆蓋。Skills 走 progressive disclosure（只注入 name+description，啟用才讀全文）。
- `docs/skills.md`：「A skill must be structured as a **directory** within a `skills/` folder inside a customization root」；`SKILL.md` **required**，選配 `scripts/`、`examples/`、`resources/`、`references/`。Frontmatter：**`name`（string, required）… lowercase and hyphenated**；**`description`（string, required）**——注意此處 `name` 標 required，與線上 /docs/skills 的「No（defaults to folder name）」不一致，實務上兩個都寫最安全。
- `docs/plugins.md`：plugin 位於 customization root 的 `plugins/` 子目錄（例 `.agents/plugins/`——**workspace 層也能放 plugin**）；`plugin.json` 為 marker，`name` 選填、缺省用目錄名；plugin 內 skills 佈局為 `plugins/<name>/skills/<skill_name>/SKILL.md`；啟用後自動 ingestion、必要時 namespacing。
- `docs/json_configs.md`：`skills.json`/`plugins.json`（放在 `.agents/` 或 `~/.gemini/config/`）可用 `entries[].path`（絕對 / `~/` / workspace-relative）+ `include_only`/`exclude`（regex）+ `inherits` 註冊非標準位置的 skills —— team 共享的替代管道。

---

## Schema / path 摘要表

| 面向 | 值（以實機 CLI 1.0.16 為準） | 佐證 |
|---|---|---|
| Skill 單位 | 目錄 `skills/<skill_name>/`，內含必要 `SKILL.md` | builtin docs/skills.md、binary 模板、實機 builtin+plugin skills |
| SKILL.md frontmatter | `name`（lowercase-hyphen；線上文件說選填、缺省=資料夾名；builtin 文件說必填）、`description`（必填，決定啟用判斷）；無其他已記載欄位 | /docs/skills、builtin docs/skills.md |
| 選配子目錄 | `scripts/`、`examples/`、`resources/`、`references/`（可自由擴充） | 同上 |
| 專案 scope | `<repo>/.agents/skills/<name>/SKILL.md`；向後相容 `.agent/`、`_agents/`、`_agent/`；由 CWD 向上逐層發現 | binary 模板 `{workspace}/.agents/skills/{skill_name}/SKILL.md`、builtin SKILL.md |
| 全域 scope | **`~/.gemini/config/skills/<name>/SKILL.md`**（= `{appDataDir}/skills/…`）；`~/.gemini/antigravity-cli/` 是 app data（settings/log/builtin），非 customization root | binary 模板+`~/.gemini/config` strings、實機 `config/plugins/` 由 CLI 寫入、builtin SKILL.md「Global: ~/.gemini/config/」 |
| Built-in skills | `~/.gemini/antigravity-cli/builtin/skills/`（優先序第 4，低於 workspace/global） | 實機目錄、builtin SKILL.md 優先序 |
| Skills 與 plugins | 互相獨立又可組合：standalone skill 直接放 `skills/`；plugin 是含 `plugin.json` 的 bundle，skills 放 `plugins/<plugin>/skills/<skill>/SKILL.md`；plugin 可放 workspace（`.agents/plugins/`）或全域（`~/.gemini/config/plugins/`） | builtin docs/plugins.md、實機 review-forge |
| plugin.json | 最小 `{"name": …}`（`^[a-zA-Z0-9-_]+$`；缺省=目錄名）；marketplace 版另有 version/description/author/repository/license/keywords | /docs/cli/plugins、實機 7 個 plugin |
| Enable/disable | 只有 plugin 粒度：`agy plugin enable|disable <name>`；skill 無 CLI 開關，TUI 內用 `/skills` 瀏覽管理；`skills.json` 的 `include_only`/`exclude` regex 可做宣告式過濾 | `agy plugin --help`、binary strings、docs/json_configs.md |
| 命名限制 | skill `name`：lowercase + hyphens（會變成 TUI slash command `/<name>`，資料夾名=預設 name）；plugin `name`：`^[a-zA-Z0-9-_]+$` | /docs/skills、/docs/cli/plugins |
| 同名衝突 | 依載入優先序覆蓋（workspace > declared > global > builtin > global-declared） | builtin SKILL.md |

---

## Implications for apm-go

1. **現行專案 scope 部署完全正確，不用改**：`internal/deploy/adapter.go:162-165` 的 `deploySkill` 複製整個 skill 目錄到 `.agents/skills/<name>/`，與 binary 寫死的 `{workspace}/.agents/skills/{skill_name}/SKILL.md` 模板逐字吻合；`SKILL.md` 在目錄根、附帶子目錄（scripts/references/…）原樣複製也符合官方結構。antigravity adapter（`internal/deploy/antigravity.go:18`）沿用同一 canonical path，無需 antigravity 特例。
2. **若日後加 user/global scope**：目標應是 **`~/.gemini/config/skills/<name>/`**，不是 Python 註解的 `~/.gemini/antigravity-cli/skills/`（該路徑在 1.0.16 實機上不存在也不被 CLI 建立；`antigravity-cli/` 只是 app data）。Python `targets.py` 的 `user_root_dir=".gemini/antigravity-cli"` 以現況看是錯的（或已過時）。
3. **命名**：skill 目錄名會成為預設 `name` 並轉成 TUI slash command `/<name>`——apm-go 部署時保留來源目錄名即可，但若要加驗證，規則是 lowercase+hyphen；`description` 是唯一硬性必填 frontmatter，apm-go 目前不做內容轉換（原樣複製），與 CLI 期望相容（frontmatter 由 skill 作者負責）。
4. **不需要 plugin 包裝**：standalone `.agents/skills/` 是一級公民且優先序最高；plugin（`plugin.json` bundle）是另一條發佈管道（`agy plugin install`、marketplace、從 claude/gemini import），apm-go 的 marketplace-install 功能若未來想對接 agy 生態，可考慮輸出 plugin 佈局（`plugin.json` + `skills/<name>/SKILL.md`），但就「把 APM skill primitive 部署給 antigravity 用」而言，現行做法已是官方正道。
5. **免複製的替代管道存在但不建議採用**：`.agents/skills.json` 的 `entries[].path` 可指向 repo 內任意目錄（例如直接指向 `.apm/skills/`），可省一次複製；但這會偏離 apm-go 全 target 一致的 canonical-copy 模型，且 `skills.json` 屬使用者/團隊自管檔案，寫入有合併風險——維持現行複製即可。
6. **衝突語意友善**：多 target 共用 `.agents/skills/` 時（claude/codex/copilot/antigravity 都走同一 canonical path），CLI 端同名以 workspace 優先且去重，apm-go 不需額外 namespacing。

---

## Deltas vs 2026-07-05 research（`antigravity-settings.md`）

1. **全域路徑分歧已 settle（07-05 Caveat 第 3 條）**：當時三方說法不一（官方 docs `~/.gemini/config/` vs Python 註解 `~/.gemini/antigravity-cli/` vs issue #60 留言的 binary 分析）且無實機可驗。本次在真機（agy 1.0.16）驗證：**全域 customization root = `~/.gemini/config/`** —— 證據鏈：(a) binary 模板 `{appDataDir}/skills/{skill_name}/SKILL.md` + 多處 `~/.gemini/config` strings；(b) `agy plugin install` 實際寫入 `~/.gemini/config/plugins/`；(c) CLI 隨附 builtin 文件明寫「Global Configuration: `~/.gemini/config/`」。issue #60 留言主張的「CLI 用 `~/.gemini/antigravity-cli/`」對 1.0.16 而言不成立（該目錄只放 settings/keybindings/log/builtin）；`~/.gemini/config/.migrated` marker 暗示中間發生過遷移，留言可能描述的是舊版。
2. **`/docs/cli/plugins` 頁面本身是過時的**（07-05 未讀此頁）：它寫全域 skills 在 `~/.gemini/antigravity-cli/skills/`、plugins 在 `~/.gemini/antigravity-cli/plugins/`，且描述「扁平 `.md` 檔」的 skill 格式（`format-tests.md` 直接放 `skills/` 下）——三點都與 binary/實機/其他文件矛盾。`/docs/skills` 頁（07-05 已引用）才與實機一致。
3. **新增資訊（07-05 未涵蓋）**：(a) workspace root 向後相容不只 `.agent/`，還有 `_agents/`、`_agent/`；(b) 發現機制是從 CWD 向上逐層走到 repo root（skills 可放在子目錄層的 `.agents/`）；(c) `skills.json`/`plugins.json` 宣告式註冊機制（entries/inherits/include_only/exclude）；(d) 五層載入優先序與同名覆蓋規則；(e) skills 會自動變 TUI slash command，TUI 內有 `/skills` 管理面板，但 CLI 沒有 `agy skills` 子命令；(f) enable/disable 只有 plugin 粒度；(g) plugin 可含 skills/agents/commands/mcpServers/hooks 五種元件（validate 輸出），支援從 claude/gemini import。
4. **frontmatter 必填性差異**：07-05 只引 `/docs/skills`；本次發現 CLI 內建 docs/skills.md 把 `name` 標為 required（線上頁標選填、缺省=資料夾名）。實務建議：兩者都寫。
5. **07-05 對 apm-go skills 部署的既有判斷維持不變**：`skill_standard`（`skills/<name>/SKILL.md`）格式與 `.agents/skills/` 專案路徑再次獲實機證實。

---

## Caveats / Not found

- **`/docs/cli/plugins` 的扁平 `.md` skill 格式未實測**：binary 只有 folder/`SKILL.md` 模板，該頁描述（`skills/format-tests.md` 直接成為 `/format-tests`）疑為早期版本殘留；read-only 約束下未建測試檔驗證 CLI 是否仍吃扁平檔。對 apm-go 無影響（apm-go 輸出的本來就是 folder 格式）。
- **`~/.gemini/config/skills/` 在本機不存在**：因為沒裝過全域 standalone skill，「CLI 會從該路徑載入」是由 binary 路徑模板 + builtin 文件 + 同層 `plugins/` 行為推得的強推論，非直接觀察 skill 載入；未在 read-only 約束下放測試 skill 實測。
- **per-skill enable/disable 的持久化未確認**：binary strings 有 `skills.txt`/`agents.txt`/`plugins.txt` 等疑似狀態檔名，但本機找不到實例，TUI `/skills` 面板具體能做什麼（僅瀏覽 vs 可停用）未實測（需進互動 TUI，超出 read-only 命令範圍）。
- **`name` frontmatter 必填性文件內部矛盾**（線上選填 vs builtin 必填）未以實測裁決；缺 `description` 時的行為也未實測。
- **plugin namespacing 細節**：builtin 文件說「namespaced if necessary」，具體規則（前綴格式、何時觸發）無文件、未實測。
- **`agy plugin install plugin@marketplace` 與 `link` 的 marketplace 生態**未深入（超出本題 scope，與 07-05 任務的 marketplace-install 分支可能相關，值得後續獨立研究）。
