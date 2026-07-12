# Research: AGENTS.md compile 生成 — Python 行為面全清單與 apm-go 落地拍板

- **Query**: Python `apm compile` 完整行為面(觸發時機、輸入來源、compile_family 分流、去重/合併、marker 格式、idempotency)+ apm-go 落地範圍三選項拍板建議
- **Scope**: mixed(Python oracle 原始碼 + apm-go 原始碼 + TEMP scratch 實測)
- **Date**: 2026-07-11
- **Oracle 版本**: apm_cli 0.21.0(實測輸出的 `<!-- APM Version: 0.21.0 -->`)

---

## 結論摘要

1. Python `apm compile` 是**獨立指令**,install 不會自動觸發 compile;install 只對 compile-only targets 印「Run 'apm compile'」提示,並把 bundle instructions 暫存到 `apm_modules/<slug>/.apm/instructions/` 供 compile 讀取。
2. compile 的路由靠 `TargetProfile.compile_family`(vscode/claude/gemini/agents/None)+ 四個 `should_compile_*` 判定函式;antigravity 屬 `compile_family="agents"`,顯式選中時只產出 AGENTS.md。
3. **去重(dedup)只對 vscode target 生效**——agents family(antigravity/codex/opencode 等)永遠把 instructions 完整寫進 AGENTS.md(issue #1678),所以最小子集**完全不需要實作 dedup**。
4. 輸出格式已實測拿到 byte-level 證據:單檔模式(`--single-agents`)兩次執行 sha256 完全相同(idempotent);Build ID = 去掉自身行後內容的 SHA256 前 12 hex;Windows 上 Python 寫 CRLF。
5. Python compile 相關面(compilation/ + primitives/ + commands/compile/)合計約 **8,700 行**,其中 context_optimizer(1,328 行,distributed 佈局引擎)、link_resolver(732 行)、claude_formatter(389 行)都是全 parity 才需要的重量級件——全 parity 不現實。
6. **拍板建議:選項 B(最小 agents-family 子集)**——新增 `apm-go compile`,只支援 agents family targets(antigravity/codex/opencode),只產出單一根 AGENTS.md(對照 oracle `--single-agents --no-links --no-constitution` 模式),A/B 可比。依據:apm-go 目前 codex/opencode adapters 完全不部署 instructions(功能黑洞,不只是 antigravity 缺口),而最小子集有明確可驗收的 oracle 契約。已依此起草 design.md + implement.md(draft)。

---

## 逐項發現(附 file:line 證據)

### A. 觸發時機:compile 是獨立指令,install 不附帶

| 事實 | 證據 |
|---|---|
| `apm compile` 是獨立 click command,無任何 install 端自動呼叫 | `apm/src/apm_cli/commands/compile/cli.py:790-937` |
| install 對 compile-only targets(profile 無 instructions primitive,如 opencode/codex/gemini)把 bundle instructions 暫存到 `apm_modules/<slug>/.apm/instructions/`,等 compile 合併 | `apm/src/apm_cli/install/services.py:894-903`(issue #1207 D2.b) |
| install 完成後只印提示「Run 'apm compile'」,不執行 | `apm/src/apm_cli/install/local_bundle_handler.py:267-272` |
| `--root` 讓 compile 寫到別的 deploy root(scratch 驗證用),與 install --root 共用實作 | `cli.py:882-894`,`install/root_redirect.py:93-102` |
| 另有 `--watch`(檔案變更自動重編)與 `--validate`(只驗證不編譯)模式 | `cli.py:811-812, 388-428, 431-467` |

### B. 輸入來源與 target 解析

| 事實 | 證據 |
|---|---|
| primitives 探索:local `.apm/`(最高優先)→ 直接依賴宣告順序 → plugins(最低);同名衝突 local 贏、先宣告贏 | `apm/src/apm_cli/primitives/discovery.py:175-205` |
| instructions 只認 `*.instructions.md`;frontmatter 用 python-frontmatter 解析;applyTo 可為 YAML list → 取第一個非 null 元素轉字串 | `apm/src/apm_cli/primitives/parser.py:80-81, 95-119` |
| 專案 gate:無 apm.yml → error + exit 1;無任何可編譯內容(無 apm_modules、無 .apm 內容、無 constitution)→ error + exit 1(dry-run 不 exit) | `cli.py:347-351, 364-385` |
| target 解析優先序:--target flag > apm.yml target:/targets: > detect_target() 目錄訊號自動偵測(>=2 個訊號 → "all") | `cli.py:274-335`,`apm/src/apm_cli/core/target_detection.py:180-223` |
| 多 target list 會折成 compiler-family frozenset(讀 TargetProfile.compile_family),單一 agents-family 名稱直接原樣通過 | `cli.py:172-271`(_resolve_compile_target) |
| antigravity 是 explicit-only:不在 --target all 展開內,help 文字明示 | `cli.py:802` |

### C. compile_family 分流

TargetProfile.compile_family 欄位定義與值域:`apm/src/apm_cli/integration/targets.py:244-257`。

| family | targets(file:line) | 產出 |
|---|---|---|
| vscode | copilot/vscode(targets.py:492) | AGENTS.md + .github/copilot-instructions.md |
| claude | claude(targets.py:519) | CLAUDE.md(.claude/rules/ 由 install 負責) |
| gemini | gemini(targets.py:639) | GEMINI.md stub(import AGENTS.md)+ AGENTS.md |
| agents | cursor(:562)、kiro(:591)、opencode(:613)、**antigravity(:685)**、codex(:708)、windsurf(:746)、hermes(:816) | 只有 AGENTS.md |
| None | agent-skills、openclaw、copilot-cowork | 無 compile 產出(成功 no-op,agents_compiler.py:404-417) |

判定函式(`apm/src/apm_cli/core/target_detection.py`):

- should_compile_agents_md(:226-252):清單 = vscode/opencode/codex/gemini/**antigravity(:246)**/windsurf/kiro/hermes/all/minimal
- should_compile_claude_md(:255-267):claude/all
- should_compile_gemini_md(:270-282):gemini/all
- should_compile_copilot_instructions_md(:285-306):vscode/all only(codex/cursor/opencode 絕不產生)

路由主體在 AgentsCompiler.compile:`apm/src/apm_cli/compilation/agents_compiler.py:346-420`。

### D. 去重/合併邏輯

| 事實 | 證據 |
|---|---|
| can_dedup_agents_md_instructions 只在 target 恰為 "vscode"(或 frozenset{"vscode"})時回 True;codex/opencode/windsurf/gemini/antigravity 以 AGENTS.md 為唯一 instructions 來源,**絕不 dedup**(issue #1678) | `target_detection.py:309-335` |
| AGENTS.md dedup 觸發條件 = .github/instructions/ 底下有 .md(由 install 部署)→ AGENTS.md 略去 instructions 段 | `agents_compiler.py:499-513`,共用偵測 helper `:60-78` |
| CLAUDE.md 對應 dedup vs .claude/rules/ | `agents_compiler.py:787-798` |
| --no-dedup / --force-instructions 全域退出 dedup | `cli.py:863-881`,`agents_compiler.py:120-122, 492-498` |
| 同名 instruction 衝突在 discovery 層解決(local 贏、先宣告贏),compile 不再合併同名 | `discovery.py:181-184` |
| 分組合併:無 applyTo → "## Global Instructions" 一組;有 applyTo → 按 **raw applyTo 字串**(不拆逗號)分組 "## Files matching"heading,組間按 pattern 字典序、組內按相對路徑排序;空 body 的 instruction 被過濾 | `apm/src/apm_cli/compilation/template_builder.py:54-84` |

**注意**:compile 的分組 key 是 raw applyTo 字串,與 install 端(claude rules 轉換)的 parseApplyTo 逗號拆分**不是同一套**——Go 實作不可誤用 parseApplyTo 來分組。

### E. AGENTS.md marker 區塊格式(單檔模式,scratch 實測 + 原始碼)

`template_builder.py:189-224`(generate_agents_md_template)+ 實測輸出:

```
# AGENTS.md
<!-- Generated by APM CLI from .apm/ primitives -->
<!-- Build ID: 2608eb1b8352 -->
<!-- APM Version: 0.21.0 -->

## Global Instructions

<!-- Source: .apm/instructions/global.instructions.md -->
Global body [G1].
<!-- End source: .apm/instructions/global.instructions.md -->

## Files matching `**/*.go`

<!-- Source: .apm/instructions/gostyle.instructions.md -->
Go body [S1].
<!-- End source: .apm/instructions/gostyle.instructions.md -->

---
*This file was generated by APM CLI. Do not edit manually.*
*To regenerate: `apm compile`*
```

- 單檔模式每條 instruction 固定包 Source/End source 註解(`template_builder.py:153-167`);body 取 strip 後內容。
- **distributed 模式 header marker 不同**:`<!-- Generated by APM CLI from distributed .apm/ primitives -->`,且預設無 Source 註解(`distributed_compiler.py:635-669`;source_attribution 預設 off,`agents_compiler.py:113-115`)。
- 可選 managed_section 模式(apm.yml compilation.agents_md.mode):只改 apm:start/apm:end 標記之間(`agents_compiler.py:127-129, 1477-1502`,issue #1540/#1764)。
- **full 模式對根 AGENTS.md 是無條件整檔覆蓋**(無 hand-authored marker 檢查;marker 保護只存在於 copilot-instructions.md `:1317-1332` 與 CLAUDE.md 刪除路徑 `:960-977`)。

### F. Idempotency

| 事實 | 證據 |
|---|---|
| 設計上不放 timestamp,改用 deterministic Build ID | `agents_compiler.py:1-6`,`compilation/constants.py:12-18` |
| Build ID 演算法:去掉 placeholder 行後,其餘行以 LF join,SHA256 取前 12 hex;保留原 trailing newline | `apm/src/apm_cli/compilation/build_id.py:22-39` |
| 所有 compile 輸出走單一 chokepoint CompiledOutputWriter(mkdir + atomic replace-on-rename) | `apm/src/apm_cli/compilation/output_writer.py:37-49` |
| 單檔 CLI 路徑:內容無變化時印 "No changes detected; preserving existing AGENTS.md for idempotency" 不重寫 | `cli.py:687-723` |
| **實測**:同輸入連跑兩次 compile --single-agents --no-links --no-constitution -t antigravity,AGENTS.md sha256 完全相同(d74e37cb...) | scratch 實測 2026-07-11 |
| **實測**:Windows 上 Python 寫出 CRLF(od 檢視);Build ID 雜湊在寫檔前以 LF 計算 | scratch 實測 od 輸出 |
| **實測**:distributed 模式會依 applyTo + 實際檔案樹放置子目錄 AGENTS.md(src/api/** + 存在 src/api/x.go → 產出 ./AGENTS.md + ./src/api/AGENTS.md) | scratch 實測 2026-07-11 |

### G. Python compile 面規模(全 parity 工作量依據)

wc -l 實測(不含測試):compilation/ 合計 5,904 行(agents_compiler 1,708、context_optimizer 1,328、distributed_compiler 1,019、link_resolver 732、claude_formatter 389、其餘 728)+ primitives/ 1,347 行 + commands/compile/ 1,361 行 ≈ **8,700 行**。另掛 security gate、output formatters、constitution injector、watch 模式。

### H. apm-go 現況

| 事實 | 證據 |
|---|---|
| 無 compile 指令:root 只註冊 validate/normalize/init/install/update/uninstall/audit/experimental/marketplace/pack | `apm-go/cmd/apm/main.go:20-29` |
| SignalWhitelist 已無 GEMINI.md/AGENTS.md 訊號(antigravity explicit-only,2026-07-05 拍板)——archive 研究 B.4 寫的「detect.go:22-23 有 AGENTS.md 訊號」已被此決策推翻 | `apm-go/internal/manifest/detect.go:16-28`;spec `.trellis/spec/backend/antigravity-target-contract.md:36-46` |
| **codex adapter 不支援 TypeInstructions** → instructions 對 codex 完全不落地(Python 靠 compile 補) | `apm-go/internal/deploy/codex.go:11-13` |
| **opencode adapter 同樣不支援 TypeInstructions** | `apm-go/internal/deploy/opencode.go:11-13` |
| antigravity adapter 只把 instructions byte-copy 到 .agents/rules/<name>.md,不維護 AGENTS.md | `apm-go/internal/deploy/antigravity.go:19-20` |
| 可重用件 1:primitive 收集(local .apm/ + apm_modules/<key>/.apm/),*.instructions.md 專屬過濾已對齊 oracle | `apm-go/internal/deploy/primitive.go:32-88, 152-161` |
| 可重用件 2:deploy.Run 的收集順序 local → 直接依賴宣告序 → transitive(lockfile 排序),與 Python discovery 優先序同構 | `apm-go/internal/deploy/deploy.go:72-118` |
| 可重用件 3:frontmatter regex(oracle 同款 DOTALL pattern)+ applyTo 逗號/brace 拆分(僅 install 端用) | `apm-go/internal/deploy/instructions_claude.go:13, 38-71` |
| 既有 A/B 慣例:documented deviations(CRLF/LF、cosmetic frontmatter)以斷言而非失敗處理 | `evals/ab_instructions_applyto.py:16-20` |

---

## 三選項拍板分析(本 child 主交付)

### 選項 A:全 parity

- **內容**:compile CLI 全 flags(watch/validate/dry-run/clean/root/chatmode/constitution/links/dedup)、distributed 佈局(context_optimizer)、claude/gemini/vscode families、managed_section、orphan cleanup、security gate。
- **依據**:oracle 相關面 ≈ 8,700 行(見 G 節),其中 context_optimizer(1,328 行)與 link_resolver(732 行)是行為黑盒,byte-parity A/B 難度極高(distributed 佈局依賴專案檔案樹掃描)。
- **粗略工作量**:數週、需拆 5+ child task。**不建議本輪**。

### 選項 B:最小 agents-family 子集(建議)

- **內容**:新 apm-go compile 指令 + internal/compile 套件;只支援 agents family(antigravity/codex/opencode);只產出專案根單一 AGENTS.md,語意對照 oracle compile --single-agents --no-links --no-constitution -t <agents-family>;idempotent(Build ID + 內容不變不重寫);dedup 天然免做(D 節:agents family 永不 dedup)。
- **依據**:
  - 功能黑洞:codex/opencode 在 apm-go 完全收不到 instructions(H 節),不只是 antigravity 的 AGENTS.md 缺口;
  - oracle 契約可驗收:單檔模式輸出已實測 byte-stable、格式簡單(E/F 節),A/B 只需 normalize Build ID / APM Version / CRLF 三處 documented deviations;
  - 積木現成:primitive 收集、依賴順序、frontmatter 解析都已在 apm-go 存在(H 節)。
- **粗略工作量**:Go 新碼 ~400-600 行 + 測試 ~400 行 + A/B 腳本 ~200 行;1-2 個工作 session。
- **明確不做(documented deviations)**:distributed 佈局、CLAUDE.md/GEMINI.md/copilot-instructions.md、constitution 注入、markdown link 解析、chatmode、watch/validate/clean/root、managed_section、vscode dedup、user scope(PRD non-goal)。

### 選項 C:本輪不做

- **內容**:只留決策記錄(比照缺口清單處置慣例)。
- **依據**:compile 是結構性功能,與當前 feat/marketplace-install 主線疏離;可等 marketplace 線收斂後另開 task。
- **代價**:codex/opencode instructions 黑洞繼續存在;antigravity 顯式 target 的 AGENTS.md 維護缺口保留;下次做仍要重跑本輪研究(已由本檔持久化,成本已 sunk)。

### 拍板建議

**選 B**。已依 B 起草 design.md + implement.md(檔頭標 draft — 待 review);若 review 拍板改 C,把本節 C 的依據謄進 .trellis/spec/backend/ 決策記錄即可驗收(PRD:「定案若為不做:留決策記錄與依據即可驗收」)。

---

## 風險

1. **Distributed 預設模式落差**:oracle 預設是 distributed,apm-go v1 只做單檔——A/B 必須 pin --single-agents,且使用者跨工具對照時輸出不同(header marker 也不同,E 節)。屬 documented deviation,但要寫進 spec。
2. **整檔覆蓋 hand-authored AGENTS.md**:Python full 模式無 marker 保護(E 節),parity 意味 apm-go compile 也會覆蓋使用者手寫的根 AGENTS.md(**含 apm-go repo 自己的 AGENTS.md**——測試絕不可在 repo 根執行 compile;A/B 全程 scratch)。是否要比 oracle 更保守(marker 檢查)是拍板點 #4。
3. **APM Version 行 + Build ID 必然不同**(版本字串不同 → 雜湊不同):A/B 需 normalize 這兩行後比對,並各自驗證兩邊 Build ID 演算法自洽(重算驗證)。
4. **applyTo 分組 key 陷阱**:compile 分組用 raw 字串,不可重用 install 端 parseApplyTo(D 節注意欄)。
5. **YAML frontmatter 解析深度**:Python 用完整 YAML 解析(list 值取第一元素,parser.py:95-119);apm-go 現行是行掃描。scalar 情境 A/B 可過;list 值 applyTo 需補最小處理或記 deviation。
6. **stdout/警告訊息不比對**:A/B 只比檔案與 exit code,警告文案(如 "No 'applyTo' pattern specified")不承諾 parity。

---

## 需拍板事項(附選項與建議)

1. **落地範圍**:A 全 parity / B 最小 agents-family 子集 / C 本輪不做 → **建議 B**(依據見上節)。
2. **v1 支援 target 集**:(a) 只 antigravity;(b) antigravity+codex+opencode(全部 agents-family adapters)→ **建議 (b)**:三者輸出完全相同(同一份根 AGENTS.md),邊際成本為零,順帶補 codex/opencode instructions 黑洞。
3. **compile 與 install 的關係**:(a) install 附帶自動 compile;(b) 獨立指令 + install 尾端提示 → **建議 (b)**,對齊 Python(A 節);v1 甚至可先不加提示(提示屬 install 行為變更,PRD non-goal 邊緣)。
4. **hand-authored 根 AGENTS.md 覆蓋策略**:(a) 完全對齊 oracle(無條件覆蓋);(b) 比 oracle 保守——無 APM marker 時拒寫 + 警告 → **建議 (a) 對齊 oracle 以利 A/B**,並在 design.md 記載 (b) 為候選加固,待 oracle 行為變更(上游 issue #1540 方向)再跟進。
5. **A/B 基準線與 normalize 規則**:oracle 側固定 compile --single-agents --no-links --no-constitution -t <target>;比對時 normalize Build ID 行、APM Version 行、CR → **建議照此定案**(慣例同 ab_instructions_applyto.py 的 documented deviations)。
6. **v1 flags 面**:(a) 只 -t/--target;(b) 加 --dry-run → **建議 (a)**,dry-run 等有真實需求再補(YAGNI)。

## Caveats / Not Found

- 未實測 oracle --single-agents 搭配 dependency instructions(apm_modules/...)時 Source 註解相對路徑的確切形狀(推定為 apm_modules/<key>/.apm/instructions/x.instructions.md,依 portable_relpath 對 source_dir 相對化,template_builder.py:151-160);implement.md 已排入實作前 probe(Step 0)。**已於 Step 0 解決,見下節。**
- 未實測 --no-constitution 下 injector 回傳的 c_status 精確值(只確認可觀察行為:第二次跑印 idempotency 訊息且檔案不變)。
- oracle minimal target(無任何目錄訊號時的 fallback)行為未納入 v1 範圍,未深查。

---

## Step 0 實作前 oracle probe 結果(2026-07-11,apm_cli 0.21.0,全程 TEMP scratch)

補上 implement.md Step 0 排定、design.md §3/§4 標記「實作前先 probe oracle 確認」的三項,另加一項意外發現(第 4 項)。

1. **依賴 instruction 的 Source relpath 形狀 — 依 dependency 型別而異**:
   - **git 風格依賴**(apm.yml `dependencies.apm: [acme/dep]`,手動放
     `apm_modules/acme/dep/.apm/instructions/dep.instructions.md`,無需真的
     git clone):oracle 輸出 `<!-- Source: apm_modules/acme/dep/.apm/instructions/dep.instructions.md -->`
     ——**與 design.md §3/§4 預測完全一致**,已在 unit test
     (`TestCollectInstructions_LocalDependencySourcePaths`)與
     `evals/ab_agents_compile.py` 鎖定。
   - **local-path 依賴**(apm.yml `dependencies.apm: [./dep-pkg]`,經
     `apm install --target antigravity` materialize 到
     `apm_modules/_local/dep-pkg/.apm/instructions/...`):oracle 的
     Source relpath 是 **`dep-pkg/.apm/instructions/dep.instructions.md`**
     ——原始宣告路徑(project root 的 sibling 目錄),*不是*
     staged 的 `apm_modules/_local/dep-pkg/...` 路徑!這是因為 Python
     的 local primitive scan 會遞迴掃描整個 project root(排除
     apm_modules/ 底下的檔案),而 `dep-pkg/` 本身不在 apm_modules/
     底下,所以被當成「local」找到,顯示原始路徑。apm-go 的收集邏輯
     固定用 `apm_modules/<key>/...`(key 對 local dep 是
     `_local/<name>`,與 apm-go 既有 dependency 慣例一致)——
     **確認為 documented deviation**,已寫入
     `.trellis/spec/backend/compile-contract.md` §4、§8,
     evals/ab_agents_compile.py 刻意只用 git 風格依賴避開此差異。
2. **同名衝突(local vs dependency)**:local `shared.instructions.md`
   贏,dependency 版本(不同內容)完全不出現在輸出中——確認
   discovery.py:181-184「先到先贏、local 贏」語意,與 apm-go
   `deploy.ResolvePrimitives` 既有邏輯同構。
3. **YAML list applyTo 取第一元素**:flow 形式
   `['**/*.py', '**/*.rb']` 與 block 形式(`- '**/*.py'\n  - '**/*.rb'`)
   皆確認 heading 只用第一元素 `**/*.py`,第二元素完全不出現。
4. **意外發現(非原排定項目):空 body 的孤立 heading 是真實 oracle 行為,
   非直覺假設**。以為「全組皆空 body 時 heading 也該省略」是合理推測,
   但兩個獨立 scratch 案例(僅一個空 body 的 global instruction;僅一個
   空 body 的 pattern-scoped instruction)均顯示 oracle 仍輸出
   `## Global Instructions` / `` ## Files matching `<pattern>` ``
   heading,heading 後直接接空行再接 `---` footer,無任何 Source 區塊。
   根因:`template_builder.py:70-82` 的 heading 輸出判斷式
   `if globals_:` / `if pattern_groups:` 只看「該組是否有任何
   instruction 物件」,不看 body 是否為空;body 是否輸出是迴圈內
   `if instruction.content.strip():` 的獨立判斷。**apm-go 已依此
   oracle 實測行為原樣鏡射**(`internal/compile/template.go` 的
   `renderInstructionsContent`),而非採用「更直覺但實際上錯誤」的
   no-orphan-heading 假設。Test lock:
   `TestRender_FiltersEmptyBodies`(兩個 orphan heading 子測試)。
   **注意**:此發現與原 checklist.md CMP-07 撰寫時的預期敘述
   (「不出現孤立 heading」)不一致——CMP-07 的敘述應以本 probe 的
   實測結果為準(oracle 是唯一權威),而非反過來要求 apm-go 產出
   「更合理但與 oracle 不同」的輸出。

---

## 附錄:install↔compile 關係——官方文件與實測行為的來源衝突裁定(DOC-03,2026-07-11)

section A(觸發時機)的三條結論全部立足於 Python **原始碼**
(`cli.py:790-937`、`install/services.py:894-903`、
`local_bundle_handler.py:267-275`——均已列在 A 節表格)。但官方
**producer 文件**另有一段與此矛盾的敘述,構成需要明確裁定的
**來源衝突(source conflict)**:

| 來源 | 敘述 | 證據 |
|---|---|---|
| 官方 producer 文件 | 「`apm install` runs compile internally as part of its integrate phase, so a normal `apm install` on a clean checkout already produces correct AGENTS.md / CLAUDE.md / GEMINI.md output.」 | `apm/docs/src/content/docs/producer/compile.md:147-151` |
| 原始碼 + 實測 | `apm install` 完成後只對 compile-only target(如 codex)印提示「Run 'apm compile'」,不會自動執行 compile;codex/gemini/opencode 等 target 的 instructions 只是暫存到 `apm_modules/<slug>/.apm/instructions/` 等 compile 讀取 | section A 表格既有引用(見上) |

**裁定依據(TEMP scratch 實測,2026-07-11,apm_cli 0.21.0)**:比照
checklist.md DOC-03 步驟,在 scratch 建立 `apm.yml`(name: probe)+
`.apm/instructions/a.instructions.md` + `.codex/`,執行:

```
$ uv --project D:/Projects/apm-dev/apm run apm install --target codex
exit code: 0
stdout 節錄:
[>] Installing dependencies from apm.yml...
[i] Targets: codex  (source: --target flag)
  [+] <project root> (local)
  |-- (files unchanged)
[i] Added apm_modules/ to .gitignore
[i] No changes -- install state already up to date in 0.1s.
AGENTS.md: 不存在(Test-Path / ls 均確認)
```

實測確認 exit `0` 且**不產生 `AGENTS.md`**——與
`producer/compile.md:147-151` 的敘述直接矛盾。裁定依權威序
(oracle 實際執行行為 + 原始碼 > 文件散文敘述其中一段):**compile 是
獨立指令,install 不會自動觸發 compile**。`producer/compile.md:147-151`
這段敘述對 codex 等 compile-only target 不準確——該同一份文件在
`:126-136`「Where instructions land」表格明確把 codex/opencode/gemini/
antigravity 標為「Compile required? Yes」,與 `:147-151`「一般 install
已經產生正確輸出」前後矛盾,屬**文件自身內部不一致**,非本研究誤讀
或裁決偏頗。design.md/implement.md「(b) 獨立指令」拍板維持不變;
PRD Requirement 3「與現行 install/deploy 流程的關係」已由此來源衝突
裁定完整回答,不需要修改 design/spec 的既有結論。
