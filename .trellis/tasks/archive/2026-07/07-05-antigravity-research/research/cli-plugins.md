# Research: Antigravity CLI — Plugins 系統（`agy plugin`）

- **Query**: Antigravity CLI 的 plugin 是什麼（結構/manifest/可包含內容）、如何安裝（來源/落地路徑/scope）、與 agent skills 的關係、enabled 狀態記錄在哪、以及 plugin 格式是否可作為 apm-go 的 install/deploy target primitive。
- **Scope**: external（官方 docs via r.jina.ai）+ local read-only verification（實機 `agy` 1.0.16 @ `C:\Users\gn006\AppData\Local\agy\bin\agy.exe`：help 輸出、`plugin list`、`plugin validate` 對 scratchpad fixture、`~/.gemini/` 目錄清點、binary strings/embedded-doc 抽取）
- **Date**: 2026-07-10

---

## A. Official docs（verbatim quotes；fetch 2026-07-10）

### A1. `https://antigravity.google/docs/cli/plugins`（"Plugins & Skills" 頁）

直接 WebFetch 拿到空 SPA shell；改用 `https://r.jina.ai/https://antigravity.google/docs/cli/plugins`（curl raw，存於 scratchpad `cli-plugins-raw.md`）成功。以下為 verbatim：

**What is a plugin（#antigravity-plugins）：**
> "Plugins are namespaced bundles that package custom skills, background subagents, linting rules, Model Context Protocol definitions, and event hooks into a single deployable asset."

**Plugin filesystem structure（#plugin-filesystem-structure）：**
> "When you install or import a plugin, the CLI stages the bundle files within your global configuration path: `~/.gemini/antigravity-cli/plugins/<plugin_name>/`"

```
~/.gemini/antigravity-cli/plugins/<plugin_name>/
├── plugin.json                 # Required package marker file
├── mcp_config.json             # Optional Model Context Protocol servers
├── hooks.json                  # Optional pre/post tool event hooks
├── skills/                     # Optional specialized skills directory
├── agents/                     # Optional subagent definition templates
└── rules/                      # Optional custom codebase rules files
```

（注意：文件寫的 staging 路徑 `~/.gemini/antigravity-cli/plugins/` 與實機不符，見 B4/Caveats。）

**Manifest（#the-plugin-manifest-pluginjson）：**
```json
{
  "$schema": "https://antigravity.google/schemas/v1/plugin.json",
  "name": "my-plugin",
  "description": "A brief description of what my plugin does."
}
```
- `name`：String，**Required**（docs 版），pattern `^[a-zA-Z0-9-_]+$`，"used to reference the plugin in CLI commands"
- `description`：String，optional
- 頁面附完整 JSON Schema：`required: ["name"]`、`additionalProperties: false`、schema URL `https://antigravity.google/schemas/v1/plugin.json`

**CLI 管理指令（#managing-plugins-via-cli-subcommands）：**
> "The CLI exposes a `plugin` (or plural `plugins`) subcommand pipeline"
- `agy plugin list` — "Show active packages and their loaded components."
- `agy plugin install /path/to/local/plugin` — "Install a local or remote plugin: Stage a package directory into your local profile."
- `agy plugin disable <plugin_name>` / `agy plugin enable <plugin_name>` — "Suspend a plugin's tools without deleting its assets."
- `agy plugin uninstall <plugin_name>` — "Purge the package directory and clean up registries."

**Plugins 與 skills 的關係（同頁 #agent-skills）：**
> "Skills are declarative, human-readable markdown files that outline explicit instruction protocols, scripts, and target resources for specialized engineering tasks."
> "Once registered, **Skills convert automatically into slash commands** inside the TUI, allowing you to invoke them manually (e.g., typing `/refactor-ui`)."
- Workspace skills：`.agents/skills/` at project root（`.md` 檔 + frontmatter `name`/`description`）
- Global skills：`~/.gemini/antigravity-cli/skills/`（"automatically imported as a global slash command"）
- Hooks："defined inside a plugin's `hooks.json` or configured inside your primary `settings.json`"；TUI `/hooks` 檢視

### A2. `https://antigravity.google/docs/cli/commands/plugins`

**不存在**——SPA 把此 URL 導回一般文件內容（Antigravity 2.0 setup/導覽），r.jina.ai 渲染後也沒有任何 plugin command 內容。plugin 指令文件就在 A1 那一頁。

### A3. `https://antigravity.google/docs/cli/gcli-migration`（佐證 import 與路徑，fetch 2026-07-10）

- > "import Gemini CLI extensions as native plugins" / "Since Gemini CLI launched, the industry has standardized on the term **plugins**. You can manually convert your legacy Gemini extensions to native Antigravity plugins"（對應 `agy plugin import gemini`；頁面 code block 被 reader 吃掉，指令名以實機 help 為準）
- Skills 路徑對照表（verbatim）：Global `~/.gemini/skills/` → `~/.gemini/antigravity-cli/skills/`；Workspace `.gemini/skills/` → `.agents/skills/`
- MCP："Global servers: `~/.gemini/config/mcp_config.json`；Workspace servers: `.agents/mcp_config.json`"；schema："Legacy schema keys: `url` or `httpUrl`；Modern schema key: `serverUrl`"（再次確認 07-05 研究 C 節的 serverUrl 結論）

---

## B. Local agy verification（agy 1.0.16，2026-07-10，全程 read-only；validate 只跑 scratchpad fixture）

### B1. `agy plugin --help`（`plugins` 為 alias，輸出相同）

```
Usage: agy.exe plugin <command> [arguments]

Commands:
  list                   List imported plugins
  import [source]        Import plugins from gemini or claude
  install <target>       Install a plugin (supports plugin@marketplace)
  uninstall <name>       Uninstall a plugin
  enable <name>          Enable a plugin
  disable <name>         Disable a plugin
  validate [path]        Validate a plugin
  link <mp> <target>     Generate link to a marketplace
  help                   Show this help
```

- 比 docs 多三個指令：`import [source]`（來源 gemini 或 claude）、`validate [path]`、`link <mp> <target>`。
- 子指令沒有各自的 --help（`agy plugin help <sub>` 一律回同一份總表）。無參數錯誤訊息：`install requires a target`、`link requires marketplace name and target`、`uninstall requires a plugin name`、`enable requires a plugin name`。

### B2. `agy plugin list`（實機輸出）

```json
{
  "imports": [
    {
      "name": "review-forge",
      "source": "claude-code",
      "importedAt": "2026-06-12T15:11:01Z",
      "components": ["skills"]
    }
  ]
}
```

- 輸出就是 `~/.gemini/config/import_manifest.json` 的內容（逐位元組相同）。**只列 imported plugins**——同機 `~/.gemini/config/plugins/` 底下另有 6 個非 import 來的 plugin（android-cli-plugin、chrome-devtools-plugin、firebase、google-antigravity-sdk、modern-web-guidance-plugin、science）完全沒出現在 list 輸出。這些應是 Antigravity 2.0 GUI/IDE 裝的（marketplace 安裝，帶 `installed_version.json`）。

### B3. `agy plugin validate`（scratchpad fixture，揭露真實 component 集合）

對含 `plugin.json` + `skills/demo/SKILL.md` + `agents/my-agent.md` + `commands/demo-cmd.md` + `rules/demo-rule.md` + `mcp_config.json` + `hooks.json` 的 fixture：

```
  [ok]    <path>/test-plugin
          ✔ skills      : 1 processed
          ✔ agents      : 1 processed
          ✔ commands    : 1 processed (converted to skills)
          ✔ mcpServers  : 1 processed
          ✔ hooks       : 1 processed
```

- **Validator 認的 component 是 5 種：skills / agents / commands / mcpServers / hooks**。`commands/` 會被「converted to skills」（呼應 07-05 研究「Gemini commands 上游併入 skills」）。
- **`rules/` 不在 validate 輸出中**（不報 processed 也不報 skipped）——但 embedded doc（B5）與官方頁都說 rules 是 plugin 的合法內容，推測 validate 只是沒檢查 rules。
- 無 `plugin.json` 時：`Error: missing plugin.json: ...`（manifest 是 plugin 的存在性標記）。
- 只有 `plugin.json`+skills 時其餘顯示 `- agents : skipped (not found)` 等——全部 optional。

### B4. `~/.gemini/` 實機盤點（ls/cat only）

- **`~/.gemini/antigravity-cli/plugins/` 不存在**；**`~/.gemini/antigravity-cli/skills/` 也不存在**（docs A1/A3 寫的兩個路徑實機都沒有）。builtin skills 在 `~/.gemini/antigravity-cli/builtin/skills/`（agy-customizations、antigravity_guide）。
- **實際 plugin root 是 `~/.gemini/config/plugins/<name>/`**，7 個 plugin 實例：
  - 每個都有 `plugin.json`；marketplace 裝的另有 `installed_version.json`（如 `{"version": "1.0.0"}`；science 的 plugin.json 是 1.1.0 但 installed_version 1.0.4，可見版本追蹤/更新機制）。
  - manifest 實例：最小 `{"name": "firebase"}`（僅 name）；完整的有 `version`/`description`/`author{name,email}`/`repository`/`license`/`keywords`——**實機 manifest 欄位遠多於官方 schema 的 name+description（且 schema 寫 `additionalProperties: false`，Google 自家 plugin 全都違反）**。
  - 內容形狀：review-forge = `plugin.json` + `skills/review-forge/SKILL.md`（+agents/assets/evals 子資料夾，是 skill 的附件）；science = `plugin.json` + `skills/<n>/SKILL.md` × 多個（含 scripts/references/docs）；chrome-devtools-plugin 與 firebase 只有 `plugin.json`（推測 MCP-only plugin 的 stub 或延遲載入）；modern-web-guidance-plugin 同時帶 `gemini-extension.json`（Gemini extension 相容殘留）。
- **`import_manifest.json`**（`~/.gemini/config/`）：記錄 import 來源（`source: "claude-code"`）、時間、components。
- 沒有任何 `plugins.json`/`skills.json`（宣告式設定檔，見 B5）存在於 `~/.gemini` 下——都是走自動發現。
- enabled/disabled 狀態：**沒有集中式 enabled 清單檔**。binary strings 中有 `plugin.json.disabled` 字串（與 `import_manifest.json` 相鄰），推斷 disable 的實作是把 `plugin.json` 改名/標記為 `plugin.json.disabled`（marker-file 方式，plugin 目錄留在原地）——與 docs "Suspend a plugin's tools without deleting its assets" 一致。未實際執行 disable 驗證（避免改動）。

### B5. `agy.exe` binary 內嵌文件（用 grep -aob + dd 抽取，非官網內容——這是 CLI 自帶、模型可讀的 customization 說明文件）

**內嵌 Plugins 文件（verbatim 摘錄）：**
> "Plugins are namespaced, shareable bundles that package **Skills**, **Rules**, **Hooks**, and **MCP Server Configurations** into a single deployable unit. They are the recommended way to distribute complex, feature-rich customizations to your team."
>
> "A plugin must be contained within a subdirectory of a `plugins/` folder in a customization root (e.g., **`.agents/plugins/`**)."

```
plugins/<plugin_name>/
    plugin.json       # Required: Manifest file
    mcp_config.json   # Optional: MCP servers exposed by the plugin
    hooks.json        # Optional: Lifecycle hooks run by the plugin
    rules/            # Optional: Rules applied when plugin is active
        *.md
    skills/           # Optional: Skills exposed by the plugin
        <skill_name>/
            SKILL.md
```

> "**`name`** (string, optional): The display name of the plugin. If omitted, it defaults to the directory name."（與官網 schema 的 required 矛盾；實機 firebase/review-forge 只有 name，validate 也接受）
>
> How Plugins Work: "1. **Automatic Ingestion**: All skills, rules, hooks, and MCP servers defined within the plugin's directory structure are automatically loaded. 2. **Namespacing**: Tools and skills exposed by the plugin are namespaced if necessary to prevent collisions... 3. **Lifecycle Scoping**: Hooks defined in `plugins/<name>/hooks.json` are registered... MCP Servers defined in `plugins/<name>/mcp_config.json` are launched... Rules in `plugins/<name>/rules/` are merged into the active rule set."
>
> Enabling Plugins: "Plugins can be discovered automatically if placed in standard customization roots, or they can be explicitly registered and enabled using `plugins.json`."

**內嵌 Customizations 總覽文件（發現位置與優先序，verbatim 摘錄）：**
> Discovery Locations: "1. **Workspace Customizations**: Path: `.agents/` (or `.agent/`, `_agents/`, `_agent/`) at the root of your project... The agent walks from your current working directory up to the repository root... 3. **Global Configuration**: Path: `~/.gemini/config/`"
>
> Priority（高→低）: "1. Workspace Project ... 2. **Declared Configurations**: Customizations explicitly listed in `skills.json` or `plugins.json` in your workspace. 3. Global Discovery: `~/.gemini/config/` 4. Built-in Customizations 5. Global Declared Configurations"
>
> Plugins 定位: "**Plugins** | `plugins/<name>/plugin.json` | Bundle | Packaging related skills, rules, and MCP configs into a single unit."

**內嵌 JSON Configs 文件（`plugins.json`/`skills.json` schema，verbatim 摘錄）：**
> "Each customization type has its own configuration file, placed in your customization root directory (e.g., `.agents/` in your project, or `~/.gemini/config/` globally): **Skills**: `skills.json`；**Plugins**: `plugins.json`"

```json
{
  "inherits": [
    { "path": "/path/to/shared/skills.json", "include_only": ["linter-skill"], "exclude": ["deprecated-skill"] }
  ],
  "entries": [
    { "path": "path/to/my/project/skills", "exclude": ["experimental-.*"] },
    { "path": "~/personal-skills" }
  ]
}
```
> Path resolution: absolute（`/`）、home-relative（`~/`）、其餘為 workspace-relative（"resolved relative to the repository root"）。`include_only`/`exclude` 為 regex 陣列。

**Binary 其他佐證（函式/字串名，非執行驗證）：**
- 內部套件檔名：`plugins/command.go`、`plugins/github_external.go`、`plugins/installer_external.go`、`plugins/handler.go`、`plugins/manager.go`、`plugins/plugin.go`。
- 函式：`plugins.ParseGitHubURL`、`plugins.gitClone`、`plugins.getRemoteBranches`、`plugins.downloadToTempFile`、`plugins.extractZip`、`plugins.HandleExternalInstall`、`plugins.installFromDirectory`、`plugins.installSingle`、`plugins.installBulk`、`plugins.isBulkDirectory`、`plugins.claudeHandler`、`plugins.geminiHandler`、`plugins.agyHandler`、`plugins.NewChainedHandler`、`plugins.ImportManifest`/`ImportRecord`/`ReadManifest`/`WriteManifest` → **install 支援 local dir（單一或 bulk）、GitHub URL（git clone / zip 下載）**；import/install 有 claude/gemini/agy 三種格式 handler 鏈（`.claude-plugin` 字串在 binary 中，即認得 Claude Code 的 `.claude-plugin/plugin.json` 佈局）。
- Marketplace：help 明示 `plugin@marketplace` 語法；binary 有 `GetSkillMarketplaceLink`(+Request/Response) protobuf RPC 與 `SetMarketplaceUrl`/`GetMarketplaceUrl` → `agy plugin link <mp> <target>` 是呼叫後端 skill-marketplace 服務產生分享連結；marketplace 解析走伺服器端，**本機沒有 marketplace.json 之類的檔案**。
- 進度字串：`Installing plugin %q...`、`installed plugin %q`；TUI 說明字串「installed plugins along with the skills and subagents they expose. You can use them just like re…」→ plugin 對使用者的呈現就是其展開的 skills + subagents。

---

## C. Plugin anatomy summary（綜合官方頁 + 內嵌文件 + 實機）

| 面向 | 結論 |
|---|---|
| 定義 | Namespaced bundle，把 skills / rules / hooks / MCP servers（+ agents、commands）打包成單一可散佈單元；官方定位「distribute customizations to your team 的推薦方式」 |
| 存在性標記 | `plugin.json`（必要）；`name` 實務上 optional（缺省 = 目錄名），官網 schema 寫 required 但 Google 自家 plugin 都超出 schema 欄位 |
| 可包含 | `skills/<n>/SKILL.md`（→ 自動變 TUI slash command）、`rules/*.md`（merge 進 active rule set）、`hooks.json`、`mcp_config.json`、`agents/*.md`（subagent 模板；validate 確認）、`commands/*.md`（安裝時轉成 skills） |
| Scope | 專案：`<repo>/.agents/plugins/<name>/`（自動發現，可進 VCS 團隊共享；`.agent/`/`_agents/`/`_agent/` 亦為合法 root）；全域：實機 `~/.gemini/config/plugins/<name>/`（docs 寫 `~/.gemini/antigravity-cli/plugins/`，實機不存在） |
| 安裝來源 | local path（單一 plugin dir 或 bulk dir）、GitHub URL（clone/zip）、`plugin@marketplace`（後端 marketplace）、`agy plugin import gemini|claude`（轉換既有 Gemini extensions / Claude Code plugins） |
| 狀態記錄 | import 記錄：`~/.gemini/config/import_manifest.json`（`agy plugin list` 只讀這份）；版本：各 plugin 目錄內 `installed_version.json`；disable：`plugin.json.disabled` marker（binary 字串推斷）；**另有宣告式 `plugins.json`（entries/inherits/include_only/exclude）可顯式註冊非標準位置的 plugin**，實機尚未見使用 |
| 與 skills 關係 | 同一份文件頁、同一個心智模型：skill 是單體 markdown customization，plugin 是「多個 skills（+rules/hooks/MCP/agents）的包裝與散佈格式」；plugin 內 skills 載入後與 standalone skills 等價（progressive disclosure、slash command），必要時加 namespace 防撞名 |

---

## D. Implications for apm-go（deployable primitive? 結論：是，而且是高價值目標）

1. **Plugin 是純靜態檔案格式，apm-go 完全可以 GENERATE/DEPLOY**——不需要跑 `agy`。把一個 apm package 的 primitives 寫成 `<project>/.agents/plugins/<pkg-name>/`（`plugin.json` 只需 `{"name": ...}` + skills/ + rules/ + hooks.json + mcp_config.json），agy 啟動即自動發現載入。這與 apm-go 現有 per-primitive 部署（`.agents/rules/`、`.agents/skills/`、`.agents/hooks.json`、`.agents/mcp_config.json`）是**平行的另一條路**，且結構幾乎 1:1 對映 apm 的 package 概念。
2. **直接解掉兩個已知落差**（07-05 研究 (2) 節）：
   - hooks 覆蓋問題：現行多個 hook primitive 搶寫同一份 `.agents/hooks.json` 只能後蓋前＋警告；改成每 package 一個 plugin，各自帶 `plugins/<pkg>/hooks.json`，由 agy 自己做 lifecycle 合併——不需要在 apm-go 實作 JSON 合併。
   - MCP 同理：`plugins/<pkg>/mcp_config.json` 自成一格，免去單檔合併。
   - rules 由 agy「merged into the active rule set」，且 customization 有 resolved-path 去重。
3. **agents 面出現了靜態格式**：plugin 的 `agents/`（validate: `agents : 1 processed`）代表 antigravity 在 plugin 內接受 subagent 定義模板——07-05 結論「antigravity 無 agents primitive 可部署（subagent 是純執行期）」在 plugin 情境下不再完全成立。若走 plugin 路線，apm-go 的 `TypeAgents` 有落點了（格式細節未驗證，見 Caveats）。
4. **`commands/` 會被轉成 skills**：apm-go 的 `TypeCommands` 也可經 plugin 落地（agy 自己做轉換），不必在 apm-go 內轉。
5. **與 marketplace-install 工作的關係**（本 repo 現在的 feat/marketplace-install）：agy 的 `plugin@marketplace`/`link` 是它自家的後端 marketplace，與 apm 的 registry 是兩套獨立系統；apm-go 不需要也不應該對接 agy marketplace。apm-go 的角色是「把 apm package 展開成 `.agents/plugins/<name>/` 檔案樹」（deploy primitive），不是呼叫 `agy plugin install`。
6. **User/global scope 若未來要做**：落點應是 `~/.gemini/config/plugins/<name>/`（實機驗證的自動發現 root），不是 docs 寫的 `~/.gemini/antigravity-cli/plugins/`；或更保守——用宣告式 `plugins.json` 的 `entries[].path` 指向 apm 管理的目錄（workspace-relative path 支援，適合 VCS 共享），完全不碰 agy 的目錄。
7. **驗證管道現成**：`agy plugin validate <dir>` 可當 conformance 檢查（apm-go 產出的 plugin 樹 → validate 應回報各 component processed），適合進 conformance-kit。
8. 若維持現行 per-primitive 部署不動，plugin 至少該進 target 詞彙認知：`.agents/plugins/` 是官方定義的 customization 子樹，apm-go 的 uninstall/清點邏輯不應誤觸他人 plugin 目錄。

---

## E. Deltas vs 2026-07-05 research（`antigravity-settings.md`）

- **Plugins 在 07-05 快照中完全沒有出現**——當時查的頁面（/docs/mcp、/docs/hooks、/docs/skills、/docs/rules-workflows、/docs/cli/using、/docs/cli/best-practices、/docs/subagents）都不含 plugin 概念；`/docs/cli/plugins` 這頁是本次新查到的（無法確認是 07-05 之後新上線還是當時漏查，但當時的 CLI 頁面清單裡沒有它）。**對 apm-go 而言這是一個全新的能力面**，不是舊資訊的修訂。
- **07-05「subagent 無靜態格式、agents primitive 無物可部署」的結論被部分推翻**：plugin 的 `agents/` 目錄是實機 validator 承認的 component（"agents : 1 processed"），官方頁也寫 "agents/ # Optional subagent definition templates"。standalone（非 plugin）情境下該結論仍成立。
- **07-05 Caveat「user-scope 全域路徑分歧（`~/.gemini/config/` vs `~/.gemini/antigravity-cli/`）」新增實機證據**：plugin/skills 的實際全域 root 是 `~/.gemini/config/`（plugins/ 實存 7 個、mcp_config.json 實存），而 docs/cli 頁寫的 `~/.gemini/antigravity-cli/{plugins,skills}/` 實機皆不存在——**CLI docs 的路徑陳述再次與二進位實作不一致，`~/.gemini/config/` 才是可信的全域 customization root**（與 issue #60 留言的 binary 分析結論相反方向：該留言主張 CLI 用 antigravity-cli/，實機觀察不支持）。
- **新發現、07-05 未涵蓋**：宣告式 `plugins.json`/`skills.json`（inherits/entries/regex 過濾/workspace-relative path）、customization 優先序五層模型、`.agent(s)`/`_agent(s)` 四種合法 workspace root 名、`agy plugin import`（claude-code/gemini 轉換器 + `import_manifest.json`）、`plugin@marketplace` 與 `GetSkillMarketplaceLink` 後端 RPC、`installed_version.json` 版本追蹤、`plugin.json.disabled` disable 機制、`commands/`→skills 自動轉換。
- 07-05 的 serverUrl 結論（MCP modern key）由 gcli-migration 頁再次確認，無變化。

---

## F. Caveats / Not found

- **官方 docs 與實作有三處明顯不一致**（都以實機為準）：(1) plugin 安裝落地路徑——docs 寫 `~/.gemini/antigravity-cli/plugins/`，實機在 `~/.gemini/config/plugins/`；(2) global skills 路徑——docs 寫 `~/.gemini/antigravity-cli/skills/`，實機不存在該目錄（07-05 查的 /docs/skills 又寫 `~/.gemini/config/skills/`，實機也不存在——本機從未放過 global skill，無法裁決哪個會被讀）；(3) manifest `name` required（docs schema）vs optional（內嵌文件 + validate 實測接受、且 Google 自家 plugin manifest 欄位超出 schema 的 `additionalProperties:false`）。
- **`agy plugin list` 只列 import 來的 plugin**：對 `~/.gemini/config/plugins/` 裡 GUI/marketplace 裝的 6 個 plugin 視而不見——用 list 判斷「已安裝 plugin 全集」不可靠，盤點要直接看目錄。
- **未實際執行**（避免任何修改）：`plugin install`（含 GitHub URL / `plugin@marketplace` 形式的實際解析行為）、`uninstall`、`enable`/`disable`（`plugin.json.disabled` marker 為 binary 字串推斷，未實測 rename 行為）、`import`、`link`（會打後端 RPC）。
- **plugin 內 `agents/*.md` 的檔案格式未驗證**：validate 接受我放的「frontmatter(name/description)+body」markdown，但 processed 不代表格式正確；subagent 模板的實際 schema（欄位、是否吃 Claude 風格 agents）需另行研究。
- **`rules/` 不出現在 validate 輸出**：無法從 validate 確認 rules 是否被載入（僅內嵌文件與官方頁背書）；未實測 plugin rules 在 TUI 中的生效情況。
- **marketplace 的內容來源不明**：`plugin@marketplace` 的 marketplace 名稱如何註冊/解析（`link <mp> <target>` 的 `<mp>` 從哪來）走後端 RPC，本機無 marketplace 設定檔可查；binary 有 `.claude-plugin` 字串，暗示可能相容 Claude Code marketplace 佈局，但未驗證。
- 本頁另提到 `docs/cli/sandbox`（"Configure security containment rings around your custom plugins and MCP servers"）未展開研究。
- Raw 抓取檔存於 scratchpad（`cli-plugins-raw.md`、`gcli-migration-raw.md`），session 結束後不保留；關鍵段落已 verbatim 收錄於本文。
