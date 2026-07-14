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

## 決策（使用者 2026-07-05 拍板）

- **explicit-only vs auto-detect → 對齊 Python,改回 explicit-only**。移除 apm-go
  `adapter.go` 對 antigravity 的 auto-detect,改為僅 `--target antigravity`(或 alias
  `agy`)顯式選取;antigravity 移出 `allAutoDetectableTargets`、加入 `explicitOnlyTargets`。
  需回歸測試:偵測階段不因 GEMINI.md/AGENTS.md 存在而選中 antigravity。
- sse `url` vs `serverUrl`:仍待中樞比對官方文件後定案(見下)。

## CLI 專項研究（2026-07-10,4 份報告:research/cli-{mcp,skills,plugins,subagents}.md）

以官方 CLI 文件 + 本機 `agy` 1.0.16 實機/binary 雙軌查證,關鍵結論:

1. **sse `url`/`serverUrl` 定案**:官方文件逐字明言 legacy `url`/`httpUrl` 不支援;
   binary 驗證訊息只認 `command|serverUrl`;內嵌 SSE 範例即用 `serverUrl`。
   → **所有 remote transport 一律寫 `serverUrl`**,移除 sse 特例(cli-mcp.md)。
2. **Custom agents 有靜態格式了**(CLI ≥1.0.16,推翻 2026-07-05「runtime-only」結論,
   該結論現僅對 IDE 成立):`.agents/agents/<name>/agent.md`(YAML frontmatter
   `name`/`description` + body = system prompt);全域 `~/.gemini/config/agents/<name>/agent.md`。
   Python 原版無此 mapping(cli-subagents.md)。
3. **Skills 部署驗證正確**:apm-go 現行 `.agents/skills/<name>/` 與 binary 路徑模板
   逐字吻合,無需修改(cli-skills.md)。
4. **Plugins 是可部署的靜態 bundle**:`.agents/plugins/<name>/`(`plugin.json` +
   skills/rules/hooks.json/mcp_config.json/agents/commands);agy 自行合併各 plugin
   的 hooks/MCP,可解 hooks.json 覆蓋缺口;`agy plugin validate` 可作驗證管道
   (cli-plugins.md)。
5. **全域 customization root 定案 = `~/.gemini/config/`**(實機 7 個 plugin + binary
   模板證實);Python 註解的 `~/.gemini/antigravity-cli/` 是 app-data,`/docs/cli/plugins`
   頁面路徑陳述過時。apm-go 無 user-scope,暫不受影響。
6. **Rules**:binary 含 `.agents/rules/` 路徑字串、內建文件 Quick Reference 明列 Rules、
   plugin rules 會 merge 進 active rule set —— CLI 有讀 rules 的證據;但「CLI(非 IDE)
   是否實際載入 workspace rules」未經實機黑箱驗證(read-only 約束)。

## 決策（使用者 2026-07-10 拍板）

- **agents primitive → 加入**。antigravity adapter 新增 `TypeAgents` →
  `.agents/agents/<name>/agent.md`(同 claude adapter 模式,多一層 per-agent 目錄)。
  Python 原版無此 mapping → 記為 **documented extension(超前上游)**。
  實作前先用本機 agy 實測掃描行為(cli-subagents.md Caveat:1.0.16 掃描行為是
  changelog 推論,未實測)。
- **rules → 維持原樣複製(byte-copy)**,frontmatter 不剝除記為 documented deviation。
  不確定 CLI 是否支援(vs 僅 IDE,參 /docs/rules-workflows)→ **需實機執行驗證**
  (部署 rule 後以 agy 確認是否載入);驗證結果記入本 PRD。
- **plugins → 另開 task**。依據:claude/codex 等現行 adapter 全部是 per-primitive
  散檔部署;Claude Code 自身也有 plugin 系統但 apm(Python/Go)都不產 claude plugin
  → antigravity 維持散檔一致。plugin bundle(可解 hooks 覆蓋缺口)記入缺口清單,
  由父任務決定是否開新 child。

## 盤查修訂（2026-07-10,Codex gpt-5.5 audit:0C/2H/4M/2L,全數採納）

1. **explicit 語意定義**(H1):explicit = `--target` flag **或** apm.yml `target:`
   明列 `antigravity`/`agy`。auto-detect 與 `all` 展開(flag 與 manifest 兩路徑)
   一律**不含** antigravity。測試矩陣見 AC。
2. **alias canonicalization 風險**(H2):`SplitTargetFlag` 走 `ValidateTarget` 會
   正規化 `agy`→`antigravity`,但 apm.yml `target:` 路徑是否在解析時正規化**待驗證**
   ——若原 token 直進 `ResolveTargets`,`filterSupported` 會把 `agy` 丟掉。
   實作須在 ResolveTargets 層級補測兩條路徑;manifest 路徑未正規化則一併修。
3. **Breaking change 記錄**(M2):僅靠 GEMINI.md/AGENTS.md auto-detect 的既有使用者,
   升級後不再自動部署 antigravity;migration = 明示 `--target antigravity` 或 apm.yml
   `target:`。`gemini` canonical target 維持現狀(無 adapter,僅 checkUnsupported
   diag),不在本輪。
4. **手動驗證證據格式**(M4):Step 0 實機驗證若走人工,須記錄 agy version、
   workspace tree、輸入 prompt、observed output;無法驗證時對應 AC 標 **blocked**,
   不得默默通過。
5. **probe rule 形狀**(M3):rules 實測須用真實 APM instructions 形狀
   (YAML frontmatter `applyTo:` + body marker),同時驗「是否載入」與
   「frontmatter byte-copy 是否污染 prompt」。
6. **驗證 oracle 主從**(L1):A/B-1 `.agents/` 結構比對為主 oracle;
   `agy plugin validate` 為 supplemental smoke(plugin 系統變動不得使本 task 失敗)。
7. **agents 路徑生命週期**(M1):agents primitive 需附 uninstall/lockfile 回歸
   (uninstall 清 `.agents/agents/<name>/agent.md`、保留 sibling)與同名衝突語意測試
   (對齊 claude adapter 現行語意並以測試鎖定)。

## Step 0 實機驗證結果（2026-07-10,agy 1.0.16,`--print` 非互動模式）

> 證據格式依盤查修訂 #4。workspace = scratchpad `agy-probe/`(git init),tree:
> `.agents/agents/probe-agent/agent.md`、`.agents/rules/probe-rule.md`(YAML
> frontmatter `applyTo` + marker)、`.agents/rules/plain-rule.md`(無 frontmatter)、
> `.agents/skills/probe-skill/SKILL.md`。probe log 留存於同目錄 agy-probe*.log。

| # | Probe(prompt 摘要) | 條件 | 結果 |
|---|---|---|---|
| 1-2 | 列 rules/agents/skills | `default-cli-project`(未註冊) | ❌ 全部不可見(僅全域) |
| 3 | 列 rules/agents/skills + 遵循 rules | `--new-project` | ✅ probe-agent/probe-skill/兩條 rules 全列出;回覆帶兩個 rule marker |
| 4 | 中性問候 | `--project`(重用) | ❌ 無 rule marker |
| 5 | 禁工具盤問 context 內 rules | `--project`(重用) | 僅 `user_global` |
| 6 | 中性問候 | 全新 `--new-project` | ❌ 無 rule marker |

**結論**:
1. **前置 gate:workspace 必須註冊為 project**(`--new-project`/`--project`),
   否則 `.agents/` 完全不掃描(關鍵 gotcha,記入 spec)。
2. **agents ✅**:`.agents/agents/<name>/agent.md` 於 1.0.16 實機被發現
   → Step 3(agents primitive)gate 通過。
3. **skills ✅**:workspace skill 正常發現(sanity check)。
4. **rules ⚠(deviation 註記升級)**:`.agents/rules/*.md` 可被發現/讀取
   (probe 3 可列出、讀取後可遵循,frontmatter 不阻礙發現),但 CLI `--print`
   模式**不自動注入 system context**(probe 4/5/6:僅 `user_global` 注入,中性
   prompt 不遵循)。與使用者原判斷「CLI 不支援自訂 rules」實質相符——精確語意:
   **可發現、非 always-on 強制**(IDE 的 Manual/Always On/Model Decision 啟用模式
   在 CLI 無對應面板)。處置:依拍板維持 byte-copy 部署(檔案落點正確,啟用語意
   屬 agy 自身行為);TUI 互動模式未測(非阻塞,標註即可)。

## Acceptance Criteria

- [x] 研究報告完成:5 份報告存在(research/antigravity-settings.md +
      cli-{mcp,skills,plugins,subagents}.md),每個決策均有 evidence 引用(L2 收緊)
- [x] explicit-only 對齊已實作(commit `c6ef3f7`),測試矩陣全綠:
      `--target antigravity` ✓、`--target agy` ✓、apm.yml `target: [antigravity]` ✓、
      apm.yml `target: [agy]` ✓、`--target all` 不含 ✗、apm.yml `target: [all]` 不含 ✗、
      僅 GEMINI.md/AGENTS.md 存在時不偵測 ✗;H2 查證:manifest 解析
      (manifest.go parseTargetField)已走 ValidateTarget 正規化,alias 一處覆蓋兩路徑
- [x] sse url/serverUrl 已定案:**修,改 serverUrl**(cli-mcp.md 官方+binary 雙證據)
- [x] sse→serverUrl fix 已實作(commit `d72dc6a`;測試翻轉
      TestWriteMCP_Antigravity_SSEUsesServerUrlField;golden 已為通用 serverUrl 無需改)
- [x] agents primitive 已實作(commit `3471e45`;`.agents/agents/<name>/agent.md` +
      support matrix 測試 + agy 實機掃描驗證 ✅(Step 0 probe 3)+ uninstall 回歸
      (per-agent 目錄剪枝、sibling 存活)+ 同名衝突 first-declared-wins 表驅動鎖定
      (claude/antigravity 同構,於 ResolvePrimitives 層裁決))
- [x] rules 實機驗證完成(Step 0 probe 1-6:可發現、非 always-on;frontmatter 不阻礙;
      維持 byte-copy,deviation 註記升級 —— 見「Step 0 實機驗證結果」)
- [x] A/B 驗證完成(2026-07-10,ab-fixture,agy 1.0.16):
      - A/B-1a explicit-only:AGENTS.md 存在、無 --target → 不部署(PASS)
      - A/B-1b `--target agy` → 5 類 primitive 全落地(PASS)
      - A/B-1c 逐欄斷言:mcpServers/sse→serverUrl(無 url)/stdio→command +
        agents/rules/skills/hooks 四組位元組一致(PASS)
      - A/B-2 supplemental `agy plugin validate`:skills 1/agents 1(巢狀
        agent.md 接受)/mcpServers 2/hooks 1 全 processed(PASS;另發現
        validate 會查 stdio command 是否在 PATH)
      - A/B-3 agy 實機發現:reviewer/fixture-skill/fixture-rule 全被列出(PASS)
      - 覆蓋率:deploy 87.7% / manifest 86.2% / cmd/apm 85.8%(全 ≥80%)
- [x] AGENTS.md 生成缺口:本 parent 不做,記錄為後續 task(缺口清單)
- [x] 缺口清單交付:每項含處置分類,見下表

## 硬性 checklist 驗證（2026-07-10,使用者指示補做）

- 清單:`.trellis/spec/conformance/cli-verification-checklist.md` §7(git-ignored 本機檔),
  **32 項全數 `[x]`**(live + unit 雙證據;7.1 選取 10、7.2 MCP 6、7.3 primitives 5、
  7.4 生命週期 4、7.5 agy 實機 4、7.6 品質關卡 3)。
- **checklist 抓到真缺陷(ag-23/25)**:local path dep 無法 uninstall —— uninstall 以
  `local:<path>` 合成 key 組模組路徑(`apm_modules/local:./dep-pkg` 非法),與 install
  F1 key `_local/<base>-<sha8>` 脫節;deployed 檔/lockfile/apm.yml 全未清。
  **已修**(commit `171fd87`):`uninstallRemovalKey` key 空間翻譯 + manifest splice
  local 合成 key;live 重驗 ag-23 6/6、ag-25 hash guard 均 PASS。
  此缺陷先前僅靠 unit(RemoveDeployedFiles 層)誤判 PASS,live 全流程才暴露 ——
  佐證硬性 checklist 的必要性。
- **新 follow-up(記錄,未修)**:`uninstallRemainingRootKeys` 對「存活的 local root」
  仍在 `local:` 空間,與 reachability BFS / stale-MCP 檢查的 `_local/` 空間不一致 ——
  存活 local dep 的傳遞依賴可能未被 reachability 保護、其 MCP 可能被誤判 stale。
  修法同款一行翻譯,待後續 task。

## 缺口清單（處置分類）

| 缺口 | 處置 | 依據 |
|---|---|---|
| sse→`url` 錯誤欄位 | **本輪修** | cli-mcp.md 定案 |
| auto-detect 偏離 Python | **本輪修**(2026-07-05 拍板) | prd 決策 |
| `agy` alias 缺失 | **本輪修**(隨 explicit-only 一併) | Python `TARGET_ALIASES["agy"]` |
| agents primitive 缺失 | **本輪修**(2026-07-10 拍板,documented extension) | cli-subagents.md |
| plugin bundle 部署 | **另開 task**(2026-07-10 拍板) | cli-plugins.md §D |
| hooks.json 覆蓋不合併 | 隨 plugin task 一併評估(plugin 路線可免實作合併) | cli-plugins.md §D.2 |
| instructions frontmatter 不剝除 | documented deviation(維持 byte-copy) | 2026-07-10 拍板 |
| AGENTS.md compile 生成 | 結構性大工程,**不在本 parent**,記錄為後續 task | antigravity-settings.md B.4 |
| user/global scope 部署 | apm-go 全 repo 性缺口,非 antigravity 專屬,不在本輪 | antigravity-settings.md B.3 |

## Non-Goals

- 不在本 child 實作 compile/AGENTS.md 生成(結構性大工程,另議)。
- sse 修正若成立僅做該最小 fix,不順手改其他 MCP adapter。
