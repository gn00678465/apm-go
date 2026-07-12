# Research: antigravity plugins bundle 部署

- **Query**: apm-go 把套件的 antigravity primitives 部署為
  `.agents/plugins/<pkg>/` plugin bundle（解 hooks 覆蓋不合併缺口）的實作前研究——
  bundle 結構/優先序/plugin.json schema 實況、apm-go 現行 antigravity 部署面
  （rules/skills/hooks/mcp 各寫哪裡）、uninstall 反向清理模型、bundle 佈局映射
  決策點盤點。
- **Scope**: mixed（internal 讀碼 + agy CLI live 實測，read-only，全程 TEMP throwaway）
- **Date**: 2026-07-11

> **注**：本檔完成時 `.trellis/tasks/07-11-antigravity-plugins-bundle/design.md`
> 與 `implement.md` 已存在完整草稿（同日較早時間戳，狀態「draft — 待
> review」）。下方逐項發現與該二檔的決策（D1–D6）**獨立交叉核對後一致**；
> 第 G 節額外做了 agy 1.1.1 的 live 再驗證（design.md §2.2 引用的關鍵主張逐條
> 重跑確認）。本檔按四段式（結論摘要／逐項發現／風險／需拍板事項）呈現，供
> review gate 對照；design.md/implement.md 本身在本輪研究未被修改（獨立
> 交叉核對後未發現需要推翻既有草稿的新事證，僅補強证据链與擴充決策選項）。

---

## 結論摘要

1. Antigravity plugin bundle 是官方定義的靜態檔案格式（`plugin.json` +
   optional `rules/`/`skills/`/`agents/`/`hooks.json`/`mcp_config.json`），
   apm-go 可以純檔案輸出的方式產生，不需呼叫 `agy` 本身；`agy plugin validate`
   可作為 conformance 驗證管道（已 live 確認，agy 1.1.1）。
2. apm-go 現行 antigravity 部署完全是「平鋪」模型：每型 primitive 各自一個固定
   或按名路徑（`.agents/rules/<n>.md`、`.agents/agents/<n>/agent.md`、
   `.agents/skills/<n>/`、單檔 `.agents/hooks.json`），MCP 另走合併寫入
   （`.agents/mcp_config.json`）。**只有 hooks 有真實的「覆蓋不合併」缺口**——
   MCP 早已用 name-key 合併解決，rules/agents/skills 因為是按名分檔，同名衝突
   在 adapter 之前的 `ResolvePrimitives` 就已經解決（first-declared-wins /
   local-wins），不會發生「不同內容互相覆蓋」的問題。
3. Uninstall 反向清理是**完全通用、以 lockfile 記錄的檔案清單為準**的模型
   （`RemoveDeployedFiles` + `cleanupEmptyParents`），不特化任何 primitive
   type 或路徑形狀——bundle 目錄（含 `plugin.json`）只要其檔案被正確寫入
   `deployed_files`/`deployed_file_hashes`，既有清理與空目錄修剪邏輯**不需要
   任何新程式碼**就能正確處理，包含「兩個 dep 的 bundle 相鄰、移除一個不動
   另一個」的 sibling-survival 語意（與現行 `ag-23` agents 目錄修剪同構）。
4. Python oracle 完全沒有 plugin bundle 概念（`hook_integrator.py` 是把多個
   hook 檔合併進**同一個共用檔**的 `"apm"` container key，而不是拆成每套件
   一個檔）——bundle 路線是 apm-go 對「hooks 不合併」缺口的**替代解法**（不是
   port Python 的合併演算法），必須在 spec 記為 documented extension。
5. **agy 版本已從研究基線 1.0.16 升到 1.1.1**（本機 `agy --version`）。多數
   B 節結論（plugin 子指令集、validate 認的 5 個 component、hooks namespaced
   shape、agents 巢狀 `agents/<name>/agent.md` 可被 validate 接受）在 1.1.1
   下 live 重驗**全部一致**；但 **`plugin.json` 的 `name` 欄位在 1.1.1 已變成
   硬性必要**（`{}` → `Error: plugin.json missing name`，exit 1）——這是相對
   07-05 研究（1.0.16 觀察 optional、預設用目錄名）的**行為 delta**，design.md
   已把它記為 R1/D5 並要求 plugin.json 一律寫 `name`，本研究已獨立重測確認。
6. `.trellis/tasks/07-11-antigravity-plugins-bundle/design.md` 與
   `implement.md` 已存在且與本研究獨立結論一致（D1 只 bundle 化 dependency
   套件、D3 MCP 不遷入、D4 bundle 名用 DepKey 末段 sanitize、D6 同套件多 hook
   檔仍在 bundle 內收斂為單檔 + 既有 overwrite 診斷）；本次未發現需要推翻既有
   草稿的新事證，僅補強其证据链（agy 1.1.1 重驗）與擴充「需拍板事項」的選項
   說明（見下方最後一節），設計/實作檔本身未修改。

---

## 逐項發現（file:line 證據）

### A. Plugin bundle 格式（來源：archive 07-05 研究，全文已讀）

- 官方定位：「namespaced bundles that package custom skills, background
  subagents, linting rules, MCP definitions, and event hooks into a single
  deployable asset」
  （`.trellis/tasks/archive/2026-07/07-05-antigravity-research/research/cli-plugins.md:16`）。
- Workspace 佈局（agy 內嵌文件 verbatim，非官網文件——官網文件的
  `~/.gemini/antigravity-cli/plugins/` 路徑實機不存在）：
  `plugins/<plugin_name>/` 內 `plugin.json`（required）+ optional
  `mcp_config.json`/`hooks.json`/`rules/*.md`/`skills/<n>/SKILL.md`；
  「A plugin must be contained within a subdirectory of a `plugins/` folder
  in a customization root (e.g., **`.agents/plugins/`**)」
  （`cli-plugins.md:142-156`）。
- `agy plugin validate` 認的 component 集合是 5 種：
  skills / agents / commands / mcpServers / hooks（`commands/` 會被 converted
  to skills）；`rules/` **從不出現在 validate 輸出**（不報 processed 也不報
  skipped）——`cli-plugins.md:110-125`。**本研究 2026-07-11 用 agy 1.1.1
  live 重驗（第 G 節）完全重現此行為**。
- 發現優先序（agy 內嵌 Customizations 文件 verbatim）：Workspace `.agents/`
  > declared（`plugins.json`/`skills.json`）> Global `~/.gemini/config/`
  > Built-in > Global Declared（`cli-plugins.md:164-172`）。
- Hooks 由 agy 自己做 lifecycle 合併（「Hooks defined in
  `plugins/<name>/hooks.json` are registered...」`cli-plugins.md:160`）——
  即 apm-go 不需要在自己的程式碼裡實作 JSON 層級的 hook 合併，只要每個套件
  各自一份完整 `hooks.json` 就自然免於「共用檔覆蓋」問題。
- 08 caveat：**manifest `name` 是否必要，07-05 研究當時（agy 1.0.16）觀察為
  optional**（embedded doc 明講「optional... defaults to directory name」，
  且 Google 自家 `firebase` plugin manifest 只有 `{"name": "firebase"}`，
  `cli-plugins.md:158,237`）；**本研究在 agy 1.1.1 下重測，此結論已不成立
  ——見第 G 節**。

### B. apm-go 現行 antigravity 部署面

| Primitive | 目的地 | 證據 |
|---|---|---|
| instructions | `.agents/rules/<name>.md`，byte-copy，不剝 frontmatter | `internal/deploy/antigravity.go:20`（documented deviation，`.trellis/spec/backend/antigravity-target-contract.md:68`） |
| agents | `.agents/agents/<name>/agent.md`，per-agent 目錄，byte-copy | `internal/deploy/antigravity.go:21-29`；documented extension，Python 無對應（`antigravity-target-contract.md:50-60`） |
| skills | `.agents/skills/<name>/` 遞迴複製（跨 target canonical，req-tg-003） | `internal/deploy/antigravity.go:17-18` → `internal/deploy/adapter.go:168-217`（`deploySkill`/`deploySkillTo`/`copyDirRecursive`） |
| hooks | 固定單檔 `.agents/hooks.json`，byte-copy；多來源後蓋前 + 僅發診斷、**不做 JSON 合併** | `internal/deploy/antigravity.go:30-31`；`internal/deploy/deploy.go:153-201`（`writtenBy` map，第 194-201 行的 overwrite 警告邏輯）；鎖定測試 `TestRun_MultipleHooksOverwriteDiagnostic`（`internal/deploy/deploy_test.go:1207-1227`，S-003） |
| MCP | 合併式 `.agents/mcp_config.json`，頂層 key `mcpServers`；stdio 用 `command`，其餘 transport 一律 `serverUrl` | `internal/deploy/mcp_antigravity.go:1-44`（`WriteMCP`/`antigravityMCPEntry`）；merge 機制在 `internal/deploy/mcp_common.go:222-296`（`mergeMCPServers`/`writeMergedMCPJSON`，name-key 合併，非覆蓋） |
| commands/prompts | 不支援（antigravity `SupportedTypes()` 未列） | `internal/deploy/antigravity.go:11-13` |

- Adapter 註冊與別名：`Adapters["antigravity"]`
  （`internal/deploy/adapter.go:36`）；alias/explicit-only 詳見
  `internal/deploy/adapter.go:118-135`（`explicitOnlyTargets`/
  `allAutoDetectableTargets`），與 spec 引用一致
  （`.trellis/spec/backend/antigravity-target-contract.md:33-46`）。
- `DeployRoots()` 回傳 `[".agents/"]`（`antigravity.go:9`）——`.agents/plugins/`
  仍落在同一 root 下，不需要新增 adapter root 宣告。
- **Primitive 衝突解決在 adapter 之前**：`ResolvePrimitives`
  （`internal/deploy/conflict.go:13-51`）以 `(Type, Name)` 為 key，全域（跨
  local/所有 dep）只留一個 winner——local 蓋 dependency（req-pr-002），
  同源類先宣告者勝（req-pr-003）。**這代表 bundle 化不會改變任何「同名該保留
  誰」的既有語意**，只改變 winner 落地的路徑。
- **plugin.json 的部署與現有的「plugin」概念是兩回事**：`internal/deploy/plugin.go`
  是**消費端**——讀取一個 dependency 套件自帶的
  `.claude-plugin/plugin.json`（Claude Code plugin manifest 格式）來發現
  skills/agents/commands（`plugin.go:12-28,42-85`），跟本任務要**產出**的
  antigravity `.agents/plugins/<pkg>/plugin.json` 是完全不同方向、不同 schema
  的兩個東西，只是恰好都叫 "plugin"。**命名風險**：新程式碼若沿用
  `pluginManifest`/`collectPluginPrimitives` 等識別字會與此檔衝突，應用不同
  前綴（design.md 已用 `antigravityBundleDir`/`BundleTarget`，未撞名）。

### C. Deploy pipeline 中「一次性寫入」的既有先例（BundleTarget 設計的依據）

- `MCPTarget` interface（`internal/deploy/adapter.go:27-30`）：與
  `TargetAdapter.DeployPrimitive`（每 primitive 呼叫一次）不同，`WriteMCP`
  是**每個 target 呼叫一次**，把所有 winner MCP servers 一次寫成一份合併檔。
  `deploy.go` 的 `Run()` 在 primitive 部署迴圈**之後**單獨跑一段
  MCP 寫入迴圈（`internal/deploy/deploy.go:226-269`），寫入的檔案一樣走
  `lockfile.HashFileBytes` 後 append 進 `result.PerDep[...]`（`deploy.go:252-267`）。
  這正是 design.md §4.3 `BundleTarget.FinalizeBundles` 的既有架構先例——
  「每 primitive 呼叫 N 次」vs「每 target 呼叫 1 次」在此 codebase 已有明確
  分工模式，不需要發明新機制。
- Skill 檔案跨 target 去重的先例：`deploy.go:174-192`
  （`deployedSkills` map，只對 `TypeSkills` 做「同一路徑只算一次」的去重）——
  這是目前**唯一**對「同一 primitive 多次呼叫收斂到同路徑」做去重的地方；
  plugin.json（若走「每個 DeployPrimitive call 都順手覆寫」而非
  `BundleTarget` 一次性寫入）會需要類似去重，design.md 選擇後者（一次性寫入）
  避開此問題，架構上更乾淨。

### D. Uninstall 反向清理模型

- `RemoveDeployedFiles(projectDir, files, hashes)`
  （`internal/deploy/uninstall.go:34-82`）是唯一刪除 target 檔案的地方，且
  **只刪呼叫者明確傳入的路徑清單**——不掃描目錄找「額外」內容，所以 bundle
  目錄裡任何未被記錄在 `deployed_files` 的使用者手動檔案天然不會被觸碰。
  三重防護：`archive.ContainedKey` 路徑逃逸檢查（:36-40）→ 檔案不存在視為
  已清除、非錯誤（:43-46）→ sha256 hash 比對，不符（含無紀錄可比對）就保留 +
  警告，不強刪（:52-71，un-053 安全線）。
- 成功刪除後呼叫 `cleanupEmptyParents(projectDir, filepath.Dir(target))`
  （:79，函式本體 :135-151）：從被刪檔案的目錄開始，逐層往上刪空目錄，直到
  遇到非空目錄或 `projectDir` 本身為止。**這個機制對任意深度的巢狀目錄都
  通用**，`.agents/plugins/<pkg>/hooks.json` 刪除後，若整個
  `.agents/plugins/<pkg>/` 只剩空目錄會被連帶修剪，若還有其他檔案（例如同
  套件另一 primitive 尚未被移除，或使用者手動放的檔案）則會在該層停止——
  跟現行 `ag-23`（`TestRemoveDeployedFiles_AntigravityAgentDirPrunedSiblingSurvives`，
  `.trellis/spec/backend/antigravity-target-contract.md:60`）驗證的「per-agent
  目錄修剪、sibling 存活」是同一套邏輯，**不需要為 bundle 寫任何特化程式碼**。
- Provenance 寫入面：`cmd/apm/install.go`（design.md 引用 :881-909）每次
  install 用 `result.PerDep[key].Files/Hashes` **整批覆蓋**該 dep 的
  `deployed_files`/`deployed_file_hashes`——意味著 `plugin.json` 若沒有在
  **每次** `Run()` 都被回報進 `PerDep`，下一次 re-install 就會從 lockfile
  「消失」而在下次 uninstall 時被當成「使用者手動檔案」保留（殘留空殼
  plugin 目錄）。這正是 design.md R3 風險與 §4.3「FinalizeBundles 冪等且
  每次都回報」的設計依據——本研究獨立確認此推論成立（`install.go` 整批覆蓋
  的語意 + `RemoveDeployedFiles` 的 hash-miss-keeps 語意兩者相乘的必然結果）。

### E. Primitive 收集模型（bundle 命名的資料來源）

- `PrimitiveType` 常數：`TypeInstructions/TypeAgents/TypeSkills/TypeCommands/
  TypeHooks/TypePrompts/TypeMCP`（`internal/deploy/primitive.go:13-21`）。
- `Primitive.DepKey`：本地為 `""`，dependency 為其 unique key（`RepoURL`
  或 `RepoURL/VirtualPath`，`internal/deploy/deploy.go:284-292` `DepRefKey`）
  ——這是 bundle 目錄命名唯一可用、已存在於 `Primitive` 結構上的欄位
  （`primitive.go:23-30`），不需要改動 `Primitive`/`TargetAdapter` 介面签名
  即可在 `DeployPrimitive(p Primitive, projectDir string)` 內部按
  `p.DepKey` 分流。
- Hooks 收集：`.apm/hooks/<name>.json` → `extractHookName`
  （`primitive.go:180-185`，收 `.json` 副檔名去除即為 name）——**同一個
  local 或同一個 dependency 若有多個 hook 檔（如 `pre.json`+`post.json`），
  在 ResolvePrimitives 層完全不衝突**（不同 Name），只有在 antigravity
  adapter 把它們都導向同一個固定路徑（`.agents/hooks.json` 或未來的
  `.agents/plugins/<pkg>/hooks.json`）時才產生「後蓋前」的檔案層級碰撞——
  這是 antigravity 這個 target **自己選擇的固定路徑架構**造成的，不是
  primitive 收集模型的缺陷。
- 本地路徑 dep 的 bundle 命名碰撞防護先例：`localModulesKey`/
  `sanitizePathSegment`（`cmd/apm/install.go:1320-1352`）——把任意（可能含
  `/`）identity 字串轉成單一安全路徑段，並附加 `sha256(...)[:8]` 後綴
  防止 basename 碰撞。這是 codebase 中**已存在、已通過安全審查**的
  「不可信/含分隔符字串 → 安全單一路徑段」慣例，design.md D4 選擇單純
  sanitize DepKey 末段（不加 hash 後綴）而非完整複用此慣例——差異與風險見
  最後一節「需拍板事項」。

### F. Python oracle 對照

- `apm/src/apm_cli/integration/hook_integrator.py`
  （引自 `.trellis/tasks/archive/2026-07/07-05-antigravity-research/research/antigravity-settings.md:70-72`）：
  `_MERGE_HOOK_TARGETS["antigravity"]` 走 `config_filename="hooks.json"`、
  `require_dir=True`（`.agents/` 不存在就整段跳過寫入）、
  `event_container_key="apm"`——**把所有 hook primitive 合併進同一個共用
  `hooks.json` 底下一個叫 `"apm"` 的巢狀 key，與使用者自訂的其他 hook-name
  並存，不覆蓋**。這是「正確地合併」而非「拆成多檔」，跟本任務要走的 plugin
  bundle 路線（每套件一份獨立檔）是**不同的解法**，兩者都能達成「不同套件
  hooks 互不覆蓋」的效果，但 apm-go 選 bundle 路線是因為：(a) 07-05 已拍板
  hooks 缺口跟著本 plugin task 一併評估（PRD 背景段）；(b) plugin bundle
  路線同時也順手解掉 rules/agents/skills 的「团队散佈單位」語意，一次到位；
  (c) 不需要在 apm-go 內實作 Python 那套「事件形狀分派＋key 改名」的合併
  演算法（`hook_integrator.py:278-332`，PreToolUse/PostToolUse 走巢狀
  `matcher+hooks`，PreInvocation/PostInvocation/Stop 走攤平陣列）。
- Python 完全沒有 plugin bundle 部署（`antigravity-settings.md:88,94-95,201`
  確認 apm-go 現行「複製、不合併、不轉換」是**已知且刻意**的簡化，
  `deploy_test.go:890-912`/`:1086-1125` 鎖定）——bundle 是 documented
  extension，需在 spec 記錄決策依據（PRD Requirements 段已明講）。

### G. agy 1.1.1 live 再驗證（本研究新做，2026-07-11，TEMP throwaway scratch）

執行環境：`agy --version` = `1.1.1`（研究基線 `.trellis/spec/backend/antigravity-target-contract.md`
全篇引用 1.0.16）。以下每筆都在系統 TEMP 下自建 fixture 目錄執行，未觸碰
`~/.gemini/`、`~/.apm`、本 repo 根目錄。

| # | 測試 | 結果 |
|---|---|---|
| G1 | 完整 fixture（`plugin.json`+`skills/demo/SKILL.md`+`rules/demo-rule.md`+`agents/demo-agent.md`+`hooks.json`）→ `agy plugin validate` | `[ok]` exit 0；`skills:1 processed`／`agents:1 processed`／`hooks:1 processed`；`commands`/`mcpServers` skipped (not found)；`rules/` 不出現（與 1.0.16 觀察一致） |
| G2 | `agents/<name>/agent.md`（apm-go 現行 agents primitive 的巢狀形狀，非扁平 `agents/<name>.md`） | `agents: 1 processed`——**apm-go 現行 agent 輸出格式可直接搬進 bundle，不需要格式轉換** |
| G3 | `plugin.json` 內容 `{}`（無 `name`） | `Error: plugin.json missing name`，exit 1 —— **與 07-05 研究「name optional」結論不同，1.1.1 已是硬性必要**（design.md R1/D5 已記錄，本研究獨立重現） |
| G4 | `plugin.json` 內容 `{"name": "minimal-plugin"}`（僅 name，無其他子目錄） | `[ok]` exit 0，其餘全部 `skipped (not found)`——manifest 本身足夠通過 validate |
| G5 | 完全沒有 `plugin.json` | `Error: missing plugin.json: ...The system cannot find the file specified.`，exit 1 |
| G6 | `plugin.json` 的 `name` 含 `.`/`_`/`-`（`owner.repo-name_123`）且與目錄名不同 | `[ok]` exit 0——validate **不驗** name 的 pattern、也不驗 name 是否等於目錄名 |

**結論**：design.md §2.2/§7-R1 引用的 agy 1.1.1 觀察（`plugin.json missing
name` 硬錯誤、5-component validate 集合、agents 巢狀可接受、rules 不出現在
輸出）本研究**逐項獨立重現，無出入**。唯一新增觀察是 G6（name 字元集與
目錄名不一致的容忍度）——design.md 未明講但也未與此矛盾，可作為
plugin.json `name` 欄位選值時的補充依據（見最後一節）。

---

## 風險

> 與 design.md §7 風險登記交叉核對後一致；此處按「研究視角」重述，並補充
> design.md 未展開的兩點（第 4、5 點）。

1. **hooks 缺口的修法範圍是「跨套件」，不是「單套件內」**——同一套件若自己
   就有多個 `.apm/hooks/*.json` 檔，bundle 化後仍會在該套件的
   `plugins/<pkg>/hooks.json` 上發生「後蓋前 + 診斷」（design.md D6 已明確
   接受此縮小後的殘留缺口，範圍從「全域」縮到「單套件」）。PRD AC2「兩套件
   各帶 hooks 同時安裝互不覆蓋」精準對應的是**跨套件**場景，此風險不影響
   AC 達成，但需要在 spec 寫清楚以免日後被誤認為「hooks 缺口已完全解決」。
2. **agy 版本 drift（1.0.16→1.1.1）**——`plugin.json` name 必要性改變是本
   研究實測到的行為變化實例；不能排除 `hooks.json` 巢狀 shape、`rules/`
   discover 語意等其他面向在未來版本也會變。Review gate 前應以當下已安裝
   的 agy 版本為準重跑本研究第 G 節探針（implement.md 前置已列
   `agy --version` 確認步驟）。
3. **plugin.json provenance 的 idempotency 是正確性的關鍵前提**——如果
   `BundleTarget.FinalizeBundles` 沒有在每次 `Run()`（包含只新增/移除其他
   primitive 的 re-install）都重寫並回報 `plugin.json` 的路徑+hash，下一次
   `install.go` 整批覆蓋 `deployed_files` 時就會遺漏它，導致下次 uninstall
   把它當「使用者手動檔案」保留（殘留空殼 `.agents/plugins/<pkg>/` 目錄，
   `plugin.json` 內容仍是舊的、但已與 lockfile 失聯）。design.md 已把這個
   拆成獨立測試點（implement.md Phase 2.1「連續兩次 Run 後仍在」）。
4. **DepKey 末段命名的碰撞面比「已知限制」更嚴重一階**——`bundleNameFromDepKey`
   （design.md 用法）只取 DepKey 最後一個 `/` 分段做 sanitize，不像
   `localModulesKey` 額外附加 `sha256[:8]`；兩個不同 owner 但同 repo 名
   （如 `acme/tool` 與 `other-org/tool`）會產生同一個 bundle 目錄名 `tool`。
   design.md 把這個標為「已知暴露、發診斷、known limitation」（design.md
   R4），而非阻擋性缺陷——但這代表**兩個不相關套件的 bundle 內容會被寫進
   同一個實體目錄**（不是「覆蓋整個 bundle」，而是各自的
   rules/skills/agents/hooks 子檔會混居在同一個 `plugins/<sameName>/` 下，
   且 `plugin.json` 的 `name` 欄位只能保留其中一個套件的值），比單純
   「檔名字串碰撞」更嚴重一階——review gate 應明確請示是否要比照
   `localModulesKey` 加 hash 後綴以徹底消除碰撞面，而非僅靠診斷訊息。
5. **ab_antigravity.py 對「無回歸」的兩種讀法直接影響驗收判準**——如果
   review gate 認定「無回歸」= 逐字保留現有斷言（含 dep agent 平鋪路徑），
   則 D1（dep 全遷入 bundle）與 AC2 無法同時成立（因為遷入後平鋪路徑就不
   再產出）；design.md 已選擇「更新 dep 相關斷言後全綠」的解讀並列為
   implement.md 前置事項之一——這是本任務**最高槓桿的單一決策**，其他所有
   設計選擇都建立在此解讀之上，必須在 review gate 第一個確認。

---

## 需拍板事項（附選項與建議）

> design.md §8 已列 4 項待拍板；以下依本研究交叉核對後，補充每項的完整選項
> 空間與建議理由（design.md 目前的預設選擇已標註 ★）。

### 1. ab_antigravity.py「無回歸」的定義（最高槓桿）

- **選項 A（design.md 預設 ★）**：允許更新腳本中「dep 相關」斷言以反映新的
  bundle 路徑（agent path 從 `.agents/agents/depagent/agent.md` 改為
  `.agents/plugins/dep-pkg/agents/depagent/agent.md` 等），local primitives
  的既有斷言保持不變；「無回歸」= 更新後腳本仍**全數 PASS**，且新增一段
  bundle 專屬驗證。
- **選項 B**：逐字保留現有全部斷言（含 dep 平鋪路徑），bundle 輸出作為
  **額外**（dual-write：dep primitives 同時寫平鋪路徑與 bundle 路徑）。
  優點是零腳本改動風險最低；缺點是每次 install 為 antigravity target 的
  dep primitives 產生雙倍檔案與雙倍 lockfile 條目，且「哪一份才是
  authoritative／agy 實際會不會重複發現同一份 skill 兩次」需要另外驗證
  （design.md R2 已點出「多 target 併裝時 dep skill 雙份被 agy 重複發現」
  的風險，dual-write 會讓這個風險在**單獨安裝 antigravity 也成立**，不只是
  多 target 情境）。
- **建議**：採選項 A（design.md 現有選擇）。理由：(a) PRD 已在 Requirements
  段明講「hooks 改走 per-plugin hooks.json」而非「新增 per-plugin
  hooks.json」，用詞是「改走」不是「並存」；(b) dual-write 直接違反 agy
  官方 plugin 語意（同一份 skill/agent 被 workspace canonical 路徑與
  plugin bundle 路徑各發現一次，`cli-plugins.md:164-172` 的優先序模型是
  針對「同名衝突」設計，不是針對「同套件兩份拷貝」設計，行為未定義）；
  (c) 選項 A 的風險（腳本要改）是一次性的、可控的實作成本，選項 B 的風險
  （雙份內容語意不明）是持續性的正確性疑慮。

### 2. bundle 化範圍：只 dependency 套件，還是也含 local primitives

- **選項 A（design.md 預設 ★，D1）**：只有 `p.DepKey != ""` 的 primitives
  進 bundle；local（`.apm/` 專案自身）primitives 維持現行平鋪輸出。
- **選項 B**：local primitives 也打包成一個「自己專案」的 bundle（例如用
  manifest 的 `m.Name` 當 bundle 名），與 dependency bundles 並列在
  `.agents/plugins/` 下。
- **選項 C**：全部 primitive（local + dep）都只走 bundle，徹底移除平鋪輸出。
- **建議**：選項 A。(a) PRD 全篇「套件」用語與 AC 場景（「兩套件各帶
  hooks」）都是在講 dependency，沒有一處提到本地 primitives 要改變行為；
  (b) `.agents/` 本身就是 workspace customization root，項目自己的
  primitives 放在 root 下的平鋪位置語意上就是「這個工作區自己的東西」，
  刻意包成一個 bundle 反而是不必要的間接層（YAGNI）；(c) 選項 C 會讓
  local-only 專案（無 dependency）的所有既有 antigravity 驗證全部作廢，
  風險/收益比最差，且 PRD Non-Goals 沒有授權這麼大範圍的行為改變。

### 3. bundle 目錄命名：DepKey 末段 sanitize（無 hash）vs 完整複用
   `localModulesKey` 慣例（附 `sha256[:8]` 後綴）

- **選項 A（design.md 預設 ★，D4）**：`bundleNameFromDepKey` 只取 DepKey
  最後一個 `/` 分段，非 `[A-Za-z0-9._-]` 字元轉 `-`；碰撞時發診斷，不阻擋。
- **選項 B**：比照 `cmd/apm/install.go:1320-1330` 的 `localModulesKey` 模式，
  bundle 目錄名 = `sanitize(basename) + "-" + sha256(DepKey)[:8]`；
  `plugin.json` 的 `name` 欄位可另外填一個人類可讀的值（如 basename），
  兩者脫鉤——這樣目錄名保證唯一，`name` 欄位保留可讀性（G6 已驗證 validate
  不要求兩者一致）。
- **建議**：本研究傾向**選項 B**（與 design.md 目前預設不同，見上方風險
  第 4 點的嚴重度分析）——選項 A 的碰撞後果不是「檔名衝突需要 rename」而是
  「兩個不相關套件的內容檔案混居同一個實體目錄、`plugin.json` 的 name 只能
  保留其中一個」，這已經越過「已知限制」的門檻，接近「不同套件互相污染彼此
  部署產物」的資料完整性問題，且 codebase 內已有現成、已審查過的解法可以
  零成本套用（同一顆 `sanitizePathSegment` 函式，只是放進
  `internal/deploy` 而非 `cmd/apm`，或直接匯出重用）。若 review gate 仍選
  選項 A，建議至少把「同 bundle 名碰撞」從 diagnostic-only 升級為安裝失敗
  （fail-closed），避免靜默資料混居。

### 4. plugin.json `name` 欄位取值

- **選項 A（design.md 預設 ★，D5）**：`name` = bundle 目錄名本身（兩者
  始終相同）。
- **選項 B**：`name` = 更可讀的值（例如 DepKey 的 basename，不含碰撞 hash
  後綴），與目錄名脫鉤。
- **建議**：此項與第 3 點連動——若第 3 點採選項 B（目錄名帶 hash 後綴），
  則 `name` 欄位建議同步採選項 B（human-readable，不含 hash），因為 G6 已
  live 驗證 `agy plugin validate` 完全不要求兩者一致，且 `agy plugin
  list`/`browse` 這類面向使用者的介面顯示的正是 `name`（07-05 研究
  `cli-plugins.md:191`「installed plugins along with the skills and
  subagents they expose」），可讀性在此有實際使用者價值；若第 3 點維持
  選項 A（無 hash），則 `name`=目錄名（design.md 現狀）已是最簡選擇，
  不需要額外欄位映射邏輯。

### 5.（design.md 未展開選項，本研究補充）skills 是否遷入 bundle vs 維持
   canonical 路徑

- **選項 A（design.md 預設 ★，D2）**：dep skills 也遷入
  `plugins/<pkg>/skills/<n>/`，不再寫 `.agents/skills/<n>/`。
- **選項 B**：dep skills 繼續走現行「跨 target canonical」路徑
  `.agents/skills/<n>/`（`internal/deploy/adapter.go:170-172`
  `deploySkill`，這條路徑是**所有支援 skills 的 target 共用**的收斂點，
  不是 antigravity 專屬），只有 rules/agents/hooks 遷入 bundle。
- **建議**：選項 A 對「plugin bundle 是完整可攜單位」的語意最一致（AC1
  的「plugin.json + 對應 primitives 子目錄」也隱含 skills 在內），但要注意
  `.agents/skills/` 路徑目前是**多個 target 共用**的收斂輸出（req-tg-003），
  只有 antigravity 一家把它「拿走」搬進 bundle 不影響其他 target（因為
  `DeployPrimitive` 是 per-adapter 呼叫，claude/copilot 等其他 adapter 仍
  各自呼叫 `deploySkill` 產出 `.agents/skills/`），但**如果同一次 install
  --target all,agy 之類同時選了 antigravity 又選了其他共用 `.agents/skills/`
  的 target**，`.agents/skills/<n>/` 這條路徑仍會因為其他 target 而存在
  ——agy 屆時可能會「同時」透過 workspace canonical `.agents/skills/`（如果
  agy 本身也把它當某種全域 skill 目錄掃描）與 `.agents/plugins/<pkg>/skills/`
  兩處看到同一個 skill，這正是 design.md R2 記錄的已知 caveat；本研究確認
  此為選項 A 的**已知且已被記錄**的代價，不是遺漏，維持 design.md 選擇。

---

## Caveats / Not found

- 未實測 `agy plugin install <local-dir>` 真正「安裝」一個 apm-go 產出的
  bundle 到 `~/.gemini/config/plugins/` 並在 TUI/`--print` 情境下被發現——
  本研究與 design.md 都只驗證到 `agy plugin validate`（靜態結構檢查）這一層，
  未做端到端的「agy 是否真的把 workspace `.agents/plugins/<pkg>/` 當
  plugin 自動載入」的 live 探測（07-05 研究的 `ag-27`/`ag-28` 探測的是**非
  bundle** 的平鋪路徑發現，不是 plugin 路徑）——這是 implement.md Phase 4.5
  唯一驗到 `validate` 層級、未驗到「discovery」層級的落差，建議在 review
  gate 前補一次 `--new-project` + `--print` 對 bundle 內 skill/agent 的
  發現探測（沿用 `antigravity-target-contract.md` §6 的驗證模式）。
- 未驗證 `plugin.json.disabled` marker 機制（07-05 研究僅 binary 字串推斷，
  未 live 驗）是否會與 apm-go 的 uninstall 邏輯互動（例如使用者手動
  disable 一個 apm-go 部署的 bundle 後，apm-go uninstall 是否仍能正確按
  hash 比對清理）——本研究判斷這超出本任務範圍（disable 是 agy 自己的
  執行期狀態，不改變 apm-go 寫入的檔案內容/hash），未進一步驗證。
- 未重新確認 `rules/` 在 agy 1.1.1 是否被實際「merged into the active rule
  set」（07-05 研究 Caveat 已明講此點未實測，只有 embedded 文件與官方頁
  背書；`antigravity-target-contract.md` §4 已記錄「agy CLI 不會自動把
  workspace rules 注入 system context」的既有 deviation）——本研究未重跑
  該 6-probe 矩陣，沿用既有 spec 結論。
