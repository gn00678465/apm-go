# Research: Antigravity（Google agentic IDE/CLI, alias `agy`）設定面 — apm-go vs Python apm_cli vs 官方文件

- **Query**: 研究 Antigravity 各種設定面（instructions/skills/hooks/mcp/agents/AGENTS.md 角色），對照 Python 版 apm_cli（`D:\Projects\apm-dev\apm`）與 apm-go（`D:\Projects\apm-dev\apm-go`）的落差，並用官方文件驗證心智模型；重點回答：(1) explicit-only vs auto-detect 分歧誰對；(2) apm-go 縮小範圍後有哪些落差；(3) AGENTS.md 的角色與指令合併/去重是否對齊。
- **Scope**: mixed（internal code diff + external web verification）
- **Date**: 2026-07-05

---

## A. Python 側（`D:\Projects\apm-dev\apm`）

### Files Found

| File Path | Description |
|---|---|
| `src/apm_cli/adapters/client/antigravity.py` (55 行) | `AntigravityClientAdapter(GeminiClientAdapter)`；只覆寫 `_get_config_dir`/`get_config_path`，MCP 格式化邏輯完全繼承自 Gemini |
| `src/apm_cli/adapters/client/gemini.py` (319 行) | `GeminiClientAdapter`：`_format_server_config`（122-258）、`configure_mcp_server`（260-318）、`update_config`（74-108，opt-in gate） |
| `src/apm_cli/core/target_detection.py` (852 行) | Target 詞彙表、`EXPLICIT_ONLY_TARGETS`（:393）、v1/v2 兩套解析演算法 |
| `src/apm_cli/integration/targets.py` (852+ 行) | `TargetProfile` 定義；antigravity 條目在 :642-687，含大量設計註解 |
| `src/apm_cli/integration/instruction_integrator.py` | `_FORMAT_CONVERTERS`（:40-46）、`_convert_to_antigravity_rules`（:705-714，去 frontmatter） |
| `src/apm_cli/integration/hook_integrator.py` | Antigravity hooks 兩形狀轉換（:278-332）、`_MERGE_HOOK_TARGETS["antigravity"]`（:357-362，`event_container_key="apm"`） |
| `src/apm_cli/install/phases/targets.py` | `_create_target_dirs`（:116-152，`auto_create=True` 才會自動建目錄） |

### Code Patterns

**1. MCP（`antigravity.py:1-55` + `gemini.py:41-121`）**
- `AntigravityClientAdapter` 100% 重用 `GeminiClientAdapter._format_server_config`（`gemini.py:122-258`），只換掉 config 目錄與檔名：
  - 專案 scope：`<project_root>/.agents/mcp_config.json`（`antigravity.py:50`），**opt-in**——`.agents/` 目錄不存在就靜默跳過（繼承自 `gemini.py:278-284` 的 `configure_mcp_server` opt-in gate）
  - user scope：`~/.gemini/config/mcp_config.json`（`antigravity.py:49`），無條件建目錄寫入
- Remote transport 欄位邏輯（`gemini.py:174-189`，繼承給 antigravity）：`transport == "sse"` → 寫 `url`；否則（`http`/`streamable-http`）→ 寫 `httpUrl`。**這是 Gemini CLI 自己的 schema，antigravity.py 沒有覆寫這段**，所以 Python 端 antigravity 的 remote 欄位其實用的是 `url`/`httpUrl`，不是官方文件目前記載的 `serverUrl`（見 C 節）。
- `_supports_runtime_env_substitution = False`（`gemini.py:62`，繼承給 antigravity）：安裝時就地解析 `${VAR}`，不支援執行期插值。

**2. Target detection — explicit-only（`target_detection.py`）**
- `EXPLICIT_ONLY_TARGETS = frozenset({"agent-skills", "antigravity"})`（:393），註解（:388-392）明講原因：「Antigravity 的 workspace config 放在跨工具共用的 `.agents/` 根目錄下，沒有 Antigravity 專屬的目錄可偵測」。
- `ALL_CANONICAL_TARGETS`（:376-378）**不含** antigravity，所以 `--target all` 不會展開出 antigravity；只有顯式 `--target antigravity`（或 `--target all,antigravity` 這種「all + explicit-only token」的疊加寫法，見 :565-580）才會啟用。
- v2 解析演算法的 `SIGNAL_WHITELIST`（:686-702）與 `CANONICAL_TARGETS_ORDERED`（:705-714）**完全沒有 antigravity 條目**——新版偵測邏輯裡 antigravity 連候選都不是。
- 但 `should_compile_agents_md`（:226-252）的清單**包含** antigravity（:246），代表：即使不自動偵測，只要顯式選了 antigravity 當 target，`apm compile` 仍會把 instructions 編譯進 `AGENTS.md`（`compile_family="agents"`，`targets.py:685`）。
- alias：`TARGET_ALIASES["agy"] = "antigravity"`（:400）。

**3. TargetProfile（`targets.py:642-687`）**
```python
"antigravity": TargetProfile(
    name="antigravity", root_dir=".agents",
    primitives={
        "instructions": PrimitiveMapping("rules", ".md", "antigravity_rules", output_compare=True),
        "skills": PrimitiveMapping("skills", "/SKILL.md", "skill_standard"),
        "hooks": PrimitiveMapping("", "hooks.json", "antigravity_hooks"),
    },
    auto_create=True, detect_by_dir=False,
    user_supported="partial", user_root_dir=".gemini/antigravity-cli",
    unsupported_user_primitives=("instructions", "hooks"),
    compile_family="agents", hooks_config_display=".agents/hooks.json",
)
```
- 沒有 `agents`（動態子代理）與 `commands` primitive——註解（:655-656）說「舊版 Gemini commands 已在上游併入 skills，antigravity 沒有 TOML command 面」。
- `auto_create=True` 但 `detect_by_dir=False`：意思是「一旦顯式選中 antigravity，就允許自動建立 `.agents/`」，但**偵測階段不會因為 `.agents/` 存在就自動選中它**（因為 `.agents/` 是跨 target 共用目錄，見 `target_detection.py:388-392` 的理由）。
- `unsupported_user_primitives=("instructions", "hooks")`：user scope 只支援 skills + MCP，理由（:659-660）是 Antigravity 把使用者層設定分散在異質的 `~/.gemini/` 子目錄下，instructions/hooks 沒有單一 user-scope 檔案可對應。

**4. Instructions 轉換（`instruction_integrator.py:705-714`）**
```python
@staticmethod
def _convert_to_antigravity_rules(content: str) -> str:
    """... Strips YAML frontmatter (Antigravity rules are plain markdown with no frontmatter) ..."""
    fm_match = re.match(r"^---\s*\n(.*?)\n---\s*\n?", content, re.DOTALL)
    if fm_match:
        return fm_match.string[fm_match.end():].lstrip("\n")
```
- 與 `_convert_to_claude_rules`（:669-703，把 `applyTo` 轉成 `paths:` frontmatter）、`_convert_to_cursor_rules` 等不同：antigravity **完全丟棄 `applyTo`**，不轉換成任何等效欄位——因為官方 UI 的規則啟用模式（Manual/Always On/Model Decision/Glob）是在 IDE 面板設定的，不是靠檔案 frontmatter（見 C 節 `/docs/rules-workflows`）。
- `output_compare=True`（`targets.py:671`）：因為輸出必然和來源不同位元組，`RULE_FORMATS` 機制（`targets.py:28-39`）要求用轉換後內容比對，而非來源位元組。

**5. Hooks（`hook_integrator.py`）**
- `_MERGE_HOOK_TARGETS["antigravity"]`（:357-362）：`config_filename="hooks.json"`、`require_dir=True`（`.agents/` 不存在就整個跳過寫入，即使 `auto_create=True` 允許建目錄——兩個機制不是同一道 gate，hooks 走的是更保守的「已存在才寫」規則）、`event_container_key="apm"`（合併進 `hooks.json` 底下一個叫 `"apm"` 的 key，和使用者自己命名的其他 hook-name 並存，不互相覆蓋）。
- 事件形狀分派（:278-332）：`PreToolUse`/`PostToolUse` 走巢狀 `[{"matcher":..., "hooks":[...]}]`；`PreInvocation`/`PostInvocation`/`Stop` 走「攤平的 handler 陣列」，`matcher` 無意義。
- Key 改名（`_copilot_keys_to_antigravity`，:321-331）：`bash`/`powershell`/`windows` → `command`；`timeoutSec`（秒）→ `timeout`，**維持秒**（跟 Gemini 轉毫秒不同，:329-331 有明確註解區分兩者）。
- 命名大小寫：hook 名稱用 `PascalCase`（`hook_integrator.py:158`，同 vscode/claude/cursor/codex/gemini/windsurf，只有 kiro 用 camelCase）。

### Related Specs

- 無對應 `.trellis/spec/` 條目（Python 端非本 repo）。

---

## B. apm-go 側（`D:\Projects\apm-dev\apm-go`）

### Files Found

| File Path | Description |
|---|---|
| `internal/deploy/antigravity.go` (27 行) | `antigravityAdapter`：`SupportedTypes` = instructions/skills/hooks；`DeployPrimitive` 全部走「原檔複製」 |
| `internal/deploy/mcp_antigravity.go` (46 行) | `WriteMCP`：`.agents/mcp_config.json`，key `mcpServers`；`antigravityMCPEntry` transport 分派 |
| `internal/deploy/adapter.go` | `explicitOnlyTargets`（:84-86，只有 `agent-skills`）；antigravity **不在**此表，且 :79-83 有明確註解說明理由 |
| `internal/manifest/detect.go` | `SignalWhitelist`（:16-24）：`GEMINI.md` **與** `AGENTS.md` 都映射到 `"antigravity"` |
| `internal/manifest/target.go` | `CanonicalTargets`（含 `gemini` 但無 adapter）、`SupportedTargets`（不含 gemini）、antigravity 註解「pre-standard, tracking microsoft/apm#1650」 |
| `internal/deploy/mcpresolve.go`(`internal/manifest/mcpresolve.go`) | `ResolveBake`（antigravity 用這個，安裝時就地解析）vs `ResolveTranslate`（只有 copilot 用） |
| `internal/deploy/deploy.go` | Hooks/固定路徑檔案的「後寫覆蓋前寫、只發警告」邏輯（:130-180），**沒有 JSON 層級合併** |
| `internal/deploy/deploy_test.go` | `:930-936` target×primitive 支援矩陣（含 antigravity 明確排除 commands/prompts/**agents**）；`:1086-1125` 驗證 hooks 是「位元組對位元組複製」；`:890-912` 驗證多來源 hooks 覆蓋只發診斷不合併 |
| `internal/deploy/mcp_writers_test.go:116-133` | `TestWriteMCP_Antigravity_SSEUsesURLField`：**明確測試鎖定** sse transport 用 `url`、不可有 `serverUrl` |
| `internal/deploy/primitive.go` | Primitive 收集（`.apm/instructions|agents|commands|hooks|prompts|skills/`），**全域都沒有 frontmatter 處理邏輯** |
| `conformance/conformance-kit/acceptance-checklist.md`（**git-ignored，本機限定**，mtime 2026-06-27） | 「OpenAPM v0.1 改寫驗收清單」，§ Target 政策明確記錄 antigravity 是「pre-standard 已接受 target」、且已用背景研究**推翻「explicit-only」假設** |
| `conformance/conformance-kit/oracle/targets/expected/antigravity.yaml` | Golden 部署樹描述（`detect: ["GEMINI.md", "AGENTS.md"]`、`mcp.http_field: serverUrl`、`var_interpolation: false`） |
| `.trellis/tasks/07-05-runtime-parity-gaps/prd.md` | 本任務的父任務 PRD，**仍把 explicit-only 分歧列為「待定」**——與下方 conformance-kit 的既有結論不一致（見 Caveats） |

### Code Patterns

**1. Auto-detect 判定（`internal/manifest/detect.go:16-24` + `internal/deploy/adapter.go:79-90`）**
```go
var SignalWhitelist = []TargetSignal{
    {".claude/", true, "claude"},
    {"CLAUDE.md", false, "claude"},
    {".github/copilot-instructions.md", false, "copilot"},
    {".codex/", true, "codex"},
    {".opencode/", true, "opencode"},
    {"GEMINI.md", false, "antigravity"},
    {"AGENTS.md", false, "antigravity"},
}
```
```go
// explicitOnlyTargets must never be activated by auto-detection (req-tg-001).
// agent-skills is the only target the spec designates explicit-only;
// antigravity DOES auto-detect via GEMINI.md/AGENTS.md (see
// acceptance-checklist.md's research note -- an earlier companion-doc
// assumption that it was explicit-only was incorrect).
var explicitOnlyTargets = map[string]bool{"agent-skills": true}

func allAutoDetectableTargets() []string {
    return []string{"claude", "codex", "copilot", "opencode", "antigravity"}
}
```
- apm-go **明確且有意**把 antigravity 當成「會自動偵測」的 target（訊號是專案根的 `GEMINI.md` 或 `AGENTS.md`），並且會被 `--target all` 展開包含在內（`allAutoDetectableTargets()` 含 antigravity）——這兩點都與 Python 端相反（Python：explicit-only，且 `ALL_CANONICAL_TARGETS` 不含 antigravity）。
- `internal/manifest/target.go`：`gemini` 是合法詞彙但**沒有 adapter**（`SupportedTargets` 不含它），`GEMINI.md` 訊號直接對應到 `"antigravity"` 而非某個獨立的 `"gemini"` target——即 apm-go 把「Gemini 衍生的 antigravity」與「舊 Gemini CLI」在偵測層合併成同一支旗標。

**2. Deploy primitives（`internal/deploy/antigravity.go`）**
```go
func (a *antigravityAdapter) DeployPrimitive(p Primitive, projectDir string) ([]string, error) {
    switch p.Type {
    case TypeSkills:
        return deploySkill(p, projectDir)
    case TypeInstructions:
        return deployFileToPath(p, fmt.Sprintf(".agents/rules/%s.md", p.Name), projectDir)
    case TypeHooks:
        return deployFileToPath(p, ".agents/hooks.json", projectDir)
    default:
        return nil, nil
    }
}
```
- `deployFileToPath`/`copyFile`（`adapter.go:174-191`）都是**原始位元組複製**，沒有任何內容轉換：
  - Instructions：來源 `.apm/instructions/<name>.instructions.md` 若帶 `applyTo:` frontmatter，會被原封不動複製進 `.agents/rules/<name>.md`——Python 端會先剝除 frontmatter（見 A 節）。
  - Hooks：多個 hook primitive 若都落在同一路徑 `.agents/hooks.json`，`deploy.go:172-180` 只會發「overwrites」診斷、**後者覆蓋前者**；不像 Python 有 `event_container_key="apm"` + 巢狀/攤平事件合併邏輯（`hook_integrator.py:284-332`），也沒有 key 改名（`bash`→`command` 等）。`deploy_test.go:890-912`/`:1086-1125` 明確測試並鎖定這個「複製、不合併、不轉換」的行為，代表這是**已知且刻意的簡化**，並非遺漏。
- Test matrix（`deploy_test.go:934`）明確鎖定 antigravity **不**部署 `TypeCommands`/`TypePrompts`/`TypeAgents`，與 Python（無 commands/agents primitive mapping）一致。

**3. MCP writer（`internal/deploy/mcp_antigravity.go`）**
```go
func antigravityMCPEntry(r *ResolvedMCPServer) (map[string]any, bool, string) {
    if r.Transport == "stdio" { ... return e, true, "" }
    e := map[string]any{}
    if r.Transport == "sse" {
        e["url"] = r.URL
    } else {
        e["serverUrl"] = r.URL
    }
    ...
}
```
- 對 `http`/`streamable-http` transport 用 `serverUrl`（符合官方目前文件，見 C 節），但對 `sse` transport 特別分支寫 `url`，且 `mcp_writers_test.go:116-133` 明確鎖定「sse 必須用 `url`、不可有 `serverUrl`」。這個分支從 antigravity MCP writer 第一個 commit（`6739ea1`, 2026-07-02）就存在，commit message 只寫「serverUrl」沒提 sse 例外；golden oracle（`antigravity.yaml:9`）也只寫通用的 `http_field: serverUrl`，沒有另外註記 sse 例外——這個 sse/url 分支的依據沒有在 repo 內留下引用來源。與官方文件的落差見 C 節。
- `MCPResolveMode() = ResolveBake`（就地解析 `${VAR}`/`${env:VAR}`，不支援 runtime 插值），與 Python 的 `_supports_runtime_env_substitution=False` 結論一致。
- 沒有 user/global scope 概念——`internal/deploy` 整個套件沒有任何 user-scope 路徑處理，這是 apm-go 目前**全 repo 性**的限制（非 antigravity 專屬），對應 Python 的 `~/.gemini/antigravity-cli/skills/`（skills）與 `~/.gemini/config/mcp_config.json`（MCP）user-scope 支援在 apm-go 完全不存在。

**4. AGENTS.md 的角色 — apm-go 目前完全沒有「compile」步驟**
- 全 repo 搜尋 `AGENTS.md` 只出現在 4 個 `.go` 檔（`deploy_test.go`、`adapter.go`、`detect_test.go`、`detect.go`），全部是「偵測訊號」用途；`cmd/apm` 底下**沒有 `compile` 指令**，`internal` 底下也沒有對應 Python `compilation/agents_compiler.py` 的套件。
- 結論：apm-go 目前把 `AGENTS.md`/`GEMINI.md` 純粹當「這個專案已經有人在用 antigravity/gemini 風格」的**偵測訊號**（存在即觸發 target），**不會**去讀取 `.apm/instructions/*` 產生或更新 `AGENTS.md` 內容；Python 端則有 `compile_family="agents"`（`targets.py:685`）+ `agents_compiler.py`，會把 instructions 編譯進 `AGENTS.md`。兩邊「AGENTS.md 由誰寫、寫什麼」目前不是同一件事——apm-go 是「讀」，Python 是「讀+寫」。
- 因為沒有 compile 步驟，apm-go 也就沒有指令去重/合併邏輯需要對齊（無論是 vscode 的 `can_dedup_agents_md_instructions` 那套，還是 antigravity 專屬的東西）——這一塊在 apm-go 是空白，不是「做了但跟 Python 不一樣」，而是「還沒開始做」。

### Related Specs

- `.trellis/tasks/07-05-runtime-parity-gaps/prd.md`：父任務 PRD，列出 antigravity 為三個 runtime 缺口之一，並把 explicit-only 分歧標成「待定」。
- `conformance/conformance-kit/acceptance-checklist.md`（git-ignored）：已有的驗收清單/決策記錄，寫作時間早於本次研究任務（2026-06-27 vs 2026-07-05 的父任務 PRD），內容已經回答了「待定」問題（見 Caveats）。

---

## C. 官方文件驗證（antigravity.google/docs，透過 r.jina.ai reader 取得渲染後內容，因原站為 Angular SPA、`curl` 拿不到內文；fetch 時間 2026-07-05）

### External References

- [MCP](https://antigravity.google/docs/mcp) — MCP Configuration Structure 一節：`mcpServers` object；每個 entry 的 transport 二選一是 `command`（stdio）或 **`serverUrl`**（remote，涵蓋 Streamable HTTP **與** SSE）。原文明講：「**Remote Connection Schema**: When declaring remote SSE, Streamable HTTP, or websocket-based MCP connections, you must define the `serverUrl` field. **Legacy fields like `url` or `httpUrl` are not supported.**」專案 scope 路徑：`.agents/mcp_config.json`；全域路徑：`~/.gemini/config/mcp_config.json`（Antigravity CLI 一節重申同一組路徑）。
- [Hooks](https://antigravity.google/docs/hooks) — `hooks.json` 的頂層 key 是**hook 名稱**（如 `"my-linter-hook"`），底下才是事件 key（`PreToolUse`/`PostToolUse` 為巢狀 `{matcher, hooks:[...]}`；`PreInvocation`/`PostInvocation`/`Stop` 為攤平 handler 陣列）；handler 欄位 `type`（目前只支援 `"command"`）/`command`/`timeout`（秒，預設 30）；輸入輸出走 stdin/stdout JSON，欄位 camelCase。5 個事件的 stdin/stdout 契約都有詳列（`decision: allow/deny/ask/force_ask` 等）。
- [Skills](https://antigravity.google/docs/skills) — 專案：`.agents/skills/<name>/SKILL.md`；全域：`~/.gemini/config/skills/<name>/SKILL.md`（**與 Python 註解的 `~/.gemini/antigravity-cli/skills/` 不同路徑**，見 Caveats）；標記「Antigravity now defaults to `.agents/skills`, but still maintains backward support for `.agent/skills`」（單複數兩種都吃）。
- [Rules / Workflows](https://antigravity.google/docs/rules-workflows) — 全域規則：`~/.gemini/GEMINI.md`（單一檔案，非目錄）；workspace 規則：`.agents/rules/` 目錄；規則的啟用模式（Manual/Always On/Model Decision/Glob）是在 IDE 的 Customizations 面板設定，**不是**檔案 frontmatter 決定——佐證 Python `_convert_to_antigravity_rules` 直接丟棄 `applyTo` 是對的（沒有對應欄位可轉換）。同樣有「defaults to `.agents/rules`, backward-compat `.agent/rules`」的註記。
- [CLI: Using AGY](https://antigravity.google/docs/cli/using) — Antigravity CLI 自己的 settings/keybindings 放在 `~/.gemini/antigravity-cli/settings.json` / `keybindings.json`（跟 skills 全域路徑 `~/.gemini/config/` 不同根，CLI 顯然有自己一份設定，Global skill/MCP 用另一份）。
- [CLI: Best Practices](https://antigravity.google/docs/cli/best-practices) — 明文：「Create a `GEMINI.md` or `AGENTS.md` file at your workspace root ... **The agent automatically parses these rules on startup**」——直接證實 apm-go 把 `GEMINI.md`/`AGENTS.md` 兩個檔名都當 antigravity 自動偵測訊號（`internal/manifest/detect.go:22-23`）是有官方依據的，不是猜的。
- [Subagents](https://antigravity.google/docs/subagents) — 子代理（`research`/`browser`/`self` 內建 + `define_subagent` 動態自訂）是**執行期工具呼叫**建立的，沒有對應的靜態檔案格式（不像 Codex 的 `.toml` agents 或 Claude 的 `.claude/agents/*.md`）。佐證 apm-go/Python 兩邊都不提供 antigravity 的 `agents` primitive 是正確的（沒有東西可部署，不是遺漏）。
- [GitHub Issue google-antigravity/antigravity-cli#60](https://github.com/google-antigravity/antigravity-cli/issues/60) — 標題與原始重現指向 **`<workdir>/.antigravitycli/mcp_config.json`**（不是 `.agents/mcp_config.json`）被讀取但 `mcpServers` 遭忽略的 bug。**但**後續留言（`cheerc`, 2026-06-01，附 binary strings 分析）指出：`.antigravitycli/` 其實是「專案 metadata / UUID symlink 目錄」，根本不是 customization root；真正的 workspace customization root 是 `.agents/`，並提供 workaround 驗證「把 `mcp_config.json` 放到 `.agents/` 底下可以正常載入且正確 scope 到該 workspace」。**也就是說這個 issue 實際上不影響 apm-go/Python 實際寫入的 `.agents/mcp_config.json` 路徑**（見 Caveats，這點修正了 conformance-kit 既有研究筆記的一個引用）。

---

## 三個問題的結論

**(1) explicit-only vs auto-detect：apm-go 現況 = auto-detect（已定案，非誤植）。**
apm-go 的 `internal/deploy/adapter.go:79-90` 明確把 antigravity 排除在 `explicitOnlyTargets` 之外、並列入 `allAutoDetectableTargets()`；`internal/manifest/detect.go:22-23` 用 `GEMINI.md`/`AGENTS.md` 兩個訊號驅動偵測；本機的 `conformance/conformance-kit/acceptance-checklist.md`（mtime 2026-06-27）記錄了這是一次背景研究後**推翻「companion 文件」explicit-only 假設**的結果，並引用官方 `docs/cli/best-practices` 的「代理在啟動時自動解析 GEMINI.md 或 AGENTS.md」作為依據——本次研究對官方文件的直接查證（見 C 節）與此结論一致。Python 端（`target_detection.py:393`）則仍是 explicit-only，且新版 v2 偵測演算法（`SIGNAL_WHITELIST`）連候選都沒放 antigravity。兩邊行為不同，但依目前查到的官方文件與 apm-go 本機既有研究記錄，**apm-go 的 auto-detect 判斷有站得住腳的證據支持**；Python 端可能是尚未跟上 antigravity 產品行為變化（或原作者在做決策時尚未確認官方自動載入行為）。

**(2) apm-go 縮小範圍後的落差（相對 Python，不含「原本就沒做」的 user-scope）：**
- **Hooks 不做 JSON 合併**：多個 hook primitive 落在同一個 `.agents/hooks.json` 時只有「後寫覆蓋前寫 + 診斷警告」（`deploy.go:172-180`，`deploy_test.go:890-912` 鎖定此行為），不像 Python 的 `event_container_key="apm"` + 巢狀/攤平事件合併 + key 改名（`hook_integrator.py:284-332`）。且來源檔案本身要嘛已經是 antigravity 原生 schema（頂層是 hook 名稱），要嘛就會被原封不動複製進去——`deploy_test.go:1086-1125` 明確驗證「輸出與來源逐位元組相同」。
- **Instructions 不剝除 frontmatter**：`deployFileToPath` 純複製，若來源 `.instructions.md` 帶 `applyTo:` YAML frontmatter，會整段原樣寫進 `.agents/rules/<name>.md`；Python 的 `_convert_to_antigravity_rules`（`instruction_integrator.py:705-714`）會先剝除。官方文件（`docs/rules-workflows`）沒有提到 frontmatter 語意，佐證 Python 的做法比較貼近產品實際行為。
- **MCP 的 sse 分支用 `url` 而非 `serverUrl`**：`mcp_antigravity.go` 對 `sse` transport 寫 `url`，並有測試（`mcp_writers_test.go:116-133`）明確鎖定此行為；但官方文件（`docs/mcp`）目前明文「remote SSE/Streamable HTTP/websocket 都要用 `serverUrl`，`url`/`httpUrl` 等 legacy 欄位不支援」。這個分支從第一個 commit 就存在，找不到 repo 內的引用依據，**可能是延續 Python 端 Gemini schema 的既有假設，但與目前查到的官方文件不一致**。
- **無 AGENTS.md compile 步驟**：見下一點。
- **無 user/global scope**：apm-go 全 repo 都沒有 user-scope 部署概念，Python 的 `~/.gemini/antigravity-cli/skills/`（skills）與 `~/.gemini/config/mcp_config.json`（MCP，user scope）在 apm-go 完全不存在——但這是 apm-go 現階段全域限制，不是 antigravity 專屬簡化。

**(3) AGENTS.md 的角色 —— 目前兩邊不對齊，且 apm-go 這塊是空白而非「做了但不同」。**
apm-go 只把 `AGENTS.md`（連同 `GEMINI.md`）當**偵測訊號**用（`internal/manifest/detect.go`），repo 內沒有任何 compile/agents_compiler 對應物，代表 apm-go 目前不會依據 `.apm/instructions/*` 去產生或更新 `AGENTS.md` 內容，也就談不上指令合併/去重邏輯。Python 端把 antigravity 歸類進 `compile_family="agents"`（會產生/更新 `AGENTS.md`），且官方文件證實 Antigravity CLI 啟動時確實會解析工作目錄根的 `GEMINI.md`/`AGENTS.md`（兩者等效）。所以：apm-go 目前的行為是「看到 `AGENTS.md`/`GEMINI.md` 存在 → 判定使用者在用 antigravity → 部署 `.agents/rules`、`.agents/skills`、`.agents/hooks.json`、`.agents/mcp_config.json`」，但不觸碰 `AGENTS.md` 本身的內容；Python 端除了偵測，還會主動维护 `AGENTS.md` 的內容。

---

## Caveats / Not Found

- **父任務 PRD 與本機既有研究記錄有落差**：`.trellis/tasks/07-05-runtime-parity-gaps/prd.md`（mtime 2026-07-05，本次任務建立時）仍把 explicit-only vs auto-detect 列為「待定的分歧」，但 `conformance/conformance-kit/acceptance-checklist.md`（mtime 2026-06-27，git-ignored、本機限定）已經記載了背景研究的結論（auto-detect 為真、以 GEMINI.md/AGENTS.md 為訊號）。兩份文件沒有互相參照，撰寫父任務 PRD 時可能沒注意到 conformance-kit 裡已有的研究筆記。
- **conformance-kit 引用的 GitHub issue #60 可能誤引**：checklist 原文寫「antigravity 專案 scope MCP 目前有『只認 HOME 層、專案層被忽略』的已知 bug（google-antigravity/antigravity-cli#60）」，但完整讀完 issue 內容（含後續留言）後，該 bug 實際重現對象是 `.antigravitycli/mcp_config.json`（專案 metadata 目錄），**不是** `.agents/mcp_config.json`；同串留言的 binary strings 分析與 workaround 驗證都指出 `.agents/mcp_config.json` 是正確的 workspace customization root、可正常載入 MCP server。也就是說這條「已知 bug」的適用性存疑，不確定是否真的影響 apm-go/Python 實際寫入的路徑。
- **User-scope 全域路徑本身在生態系內部也有分歧、未完全查證**：官方 `/docs/mcp`、`/docs/skills` 頁面寫全域路徑是 `~/.gemini/config/{mcp_config.json,skills/}`；但 issue #60 的留言（`cheerc`）用 binary strings 分析主張「CLI 二進位實際用的 app data dir 是 `~/.gemini/antigravity-cli/`（`mcp_config.json` 同名），`~/.gemini/config/` 是 Antigravity 2.0 GUI 的路徑，CLI 文件寫的路徑可能過時」；`/docs/cli/using` 也證實 CLI 自己的 `settings.json`/`keybindings.json` 放在 `~/.gemini/antigravity-cli/`。三方（官方 docs、Python 註解、issue 留言的二進位分析）對 CLI 場景下全域路徑到底是 `~/.gemini/config/` 還是 `~/.gemini/antigravity-cli/` **沒有完全一致的說法**；本研究未能實際安裝 CLI 驗證，此處留待日後有實機環境時再確認。由於 apm-go 目前無 user-scope 實作，此分歧暫不影響 apm-go 現況。
- **sse→`url` 分支的原始依據找不到**：如上所述，`mcp_antigravity.go` 的 sse 例外從 `6739ea1`（2026-07-02）第一次出現就有，commit message、golden oracle（`antigravity.yaml`）都沒有特別提及這個例外，無法確認是刻意研究後的決定還是沿用 Gemini schema 的假設帶入。
- **未查證項目**：Antigravity 2.0（GUI）與 Antigravity CLI（agy）在 hooks/skills 全域路徑上是否完全共用同一份設定（本研究只查了文件，未實機驗證）；apm-go 是否有計畫中的 `compile` 指令（只確認目前不存在，未查是否列在 roadmap）。
