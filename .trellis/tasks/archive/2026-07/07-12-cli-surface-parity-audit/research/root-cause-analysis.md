# Research: 流程根因分析（RCA）— codex agents byte-copy / pack 同名異義 / audit 同名異義

- **Query**: 回溯三個已知缺陷（pack 同名異義、audit 同名異義、codex agents byte-copy 假
  TOML）各自源頭任務的 prd/design/implement/research 文件與 git 歷史，回答 (a) 缺陷在流程
  哪一步進來、(b) 為什麼既有驗證沒攔住、(c) 三案例是否共享同一個系統性根因，並提出可落地的
  預防機制提案。
- **Scope**: internal（`.trellis/tasks/archive/**`、`.trellis/tasks/07-12-codex-agent-toml/`、
  git log/show、`internal/deploy/`、`cmd/apm/audit.go`、`cmd/apm/pack.go` 原始碼）
- **Date**: 2026-07-12

## 方法

對每個案例：(1) 用 `git log --diff-filter=A` 定位引入 commit，(2) 讀該 commit 所屬 task 的
`prd.md`/`design.md`/`research/*`/`implement.md`/`check.jsonl`，(3) 確認研究/設計階段是否
真的讀過 Python 原始碼對應行為、AC 是否涵蓋會暴露缺陷的情境、外部審查（opus/codex）的驗證
目標是什麼。全程唯讀，未修改任何 production 檔案。

---

## Case 1：`codex agents` byte-copy 假 TOML

### 時間軸（file:line 佐證）

1. **2026-06-29，commit `2aea588`**（task `06-29-phase4-target-deploy`）引入
   `internal/deploy/codex.go`：`TypeAgents` 走 `deployFileToPath(p,
   fmt.Sprintf(".codex/agents/%s.toml", p.Name), projectDir)`——純 byte-copy，只是把來源
   markdown 檔案改副檔名成 `.toml`。
2. **2026-06-30，commit `5d700f2`**（task `06-30-phase4t-target-matrix`，review 修復 6 項）
   —— 6 項修正裡沒有任何一項碰 codex agents 內容轉換；`implement.md:63` 只有
   「Add length assertions to codex/antigravity/opencode tests」這類**檔案數量**斷言。
3. **2026-07-12**，使用者對 `evals/test1` fixture 做 live 驗證才發現：輸出的
   `.codex/agents/*.toml` 是 markdown 內容、不是合法 TOML，Codex CLI 無法解析
   （task `07-12-codex-agent-toml`，prd.md:11-13：「lockfile hash 與來源相同為證」）。
4. 同日修復（commit `197fe98`）：新增 `transformCodexAgent`/`deployCodexAgentTOML`，鏡射
   Python `agent_integrator.py:302 _write_codex_agent` 的六點語意。

**存活時間**：`2aea588`→使用者發現，橫跨 13 天、至少一輪 review 修復（`5d700f2`）而未被攔下。

### (a) 缺陷在流程哪一步進來

研究階段其實**已經抓到訊號、但沒有追下去**：`06-29-phase4-target-deploy/research/
python-apm-research.md:159-163` 的「Per-Target Primitive Mappings」表格對 codex agents 明確
記了一欄 `format_id = codex_agent`（欄位定義見同檔 :120：「format_id: str # format
transformer key」），這代表研究**已經知道** codex agents 有一個具名的轉換器，不是單純檔案
複製。但同一份研究文件的「§7 Format Transforms（Separate Subsystem）」（:362-374）只展開了
**instructions 類型**的 5 個 format_id（`cursor_rules`/`claude_rules`/`windsurf_rules`/
`kiro_steering`/`antigravity_rules`），完全沒有展開 `codex_agent`（agents 類型）實際做什麼
——研究記錄了「這個 key 存在」這個事實，卻沒有點開它的轉換邏輯本體（`agent_integrator.py`）。

`06-29-phase4-target-deploy/design.md`「Explicitly Out of Scope」把這個訊號**用一句廣義文字
吞掉**：「Format transforms (.instructions.md → .mdc for cursor, etc.) — plain file copy
only」（design.md:221）。這句話字面上只舉了 instructions 類型的例子（cursor `.mdc`），卻被
拿來覆蓋 agents 類型的 `codex_agent` 轉換——design.md 的「File Placement Rules」表格
（:131-141）只記了 codex agents 的**輸出路徑**（`.codex/agents/<name>.toml`，來自 oracle
`targets/expected/codex.yaml` 的檔案清單），沒有交叉檢查「這個路徑的副檔名 `.toml` 隱含一個
與來源完全不同的**結構化格式**，不是改個副檔名就能滿足」。design 決策把「格式轉換」一律歸類
成「未來階段」，沒有區分「cosmetic 副檔名/路徑轉換（可延後）」與「內容結構轉換（改副檔名但
不轉內容會產生不可解析的檔案，是功能缺陷不是延後項）」這兩種本質不同的東西。

### (b) 為什麼既有驗證沒攔住

驗證鏈全程只檢查**檔案放置**（路徑是否在正確位置、是否恰好對應 oracle 記錄的
`deployed_files` 清單），從未檢查**內容有效性**（該路徑的位元組是否真的能被目標格式的
parser 解析）：

- `06-29-phase4-target-deploy` 的 AC（prd.md:52-65）第 6-9 項全部是「檔案在哪裡」「哪些
  primitive 類型有沒有被部署」，沒有一項是「部署出的檔案內容是否合法」。
- `06-30-phase4t-target-matrix`（review 修復輪）新增的測試方向同樣是「長度/存在性斷言」
  （見上方時間軸第 2 點），`5d700f2` 的 commit message 六項修復（S-001~S-006）全部是關於
  oracle 比對方式、覆寫偵測、hooks 內容，沒有一項是「用 TOML parser 解析輸出檔案」。
- 兩輪都以 oracle YAML 的 `deployed_files` 清單為比對基準——oracle fixture 本身只記錄
  「這個路徑應該存在」，不記錄「這個路徑的位元組應該能被 TOML parser 解析」，所以順著 oracle
  走的驗證邏輯天生看不到這個缺陷類別。

### 本案結論

研究**捕捉到了訊號**（`format_id: codex_agent` 這個事實）但沒有展開其語意；設計把「格式轉換」
當成鐵板一塊的單一延後項，用一個舉例（cursor `.mdc`）覆蓋了性質不同的另一個 format_id
（`codex_agent`）；兩輪驗證都只做「檔案是否存在於正確路徑」，從未做「檔案內容是否為合法目標
格式」這一層檢查。

---

## Case 2：`pack` 同名異義（marketplace.json 產生器 vs plugin bundle 打包）

### 時間軸（file:line 佐證）

1. **2026-07-03**，父任務 `07-03-marketplace-ecosystem/prd.md` 做子任務拆分（:26-33）：
   第 4 個子任務被直接命名為「`07-03-marketplace-pack`（Phase M4）：`apm pack`
   （marketplace.json 產生器）」——**在拆分的當下就已經把範圍收斂成「marketplace.json 產生
   器」**，父 PRD 的「背景」段落（:1-9）完全沒有提及 Python `apm pack` 還有 plugin bundle
   打包（BundleProducer）與專案根 plugin.json 產出（PluginManifestProducer）這兩個功能面，
   只把整個 marketplace 生態系框成「apm marketplace 指令組 + 發布端」。
2. 子任務 `07-03-marketplace-pack/design.md:6-8`「範圍界定（重要）」明文記錄這個收斂決策：
   「原版 `apm pack` 同時負責 plugin 打包…與 marketplace.json 產生。**本子任務只做
   marketplace.json 產生**（parent prd 的範圍決定）」——**這不是隱藏的疏漏，是有意識、有落
   檔的範圍決定**，且同一句話決定了「無 `marketplace:` 區塊時 exit 0、印訊息，不報錯」。
3. **2026-07-03**，父任務的 `design-gaps-and-verification-needed.md` 記錄了對這個子任務
   進行的**兩輪高強度對抗性審查**（第一輪：全文讀 `output_mappers.py`；第二輪 Adversarial
   抽查 B，5 項）——但這 5 項全部是**演算法正確性**（Claude/Codex 欄位表、sourceBase、
   tagpattern、builder 細節、exit code），**沒有一項重新檢視「只做 marketplace.json」這個
   範圍決定本身是否安全**。
4. `cmd/apm/pack.go:24-36` 的程式碼註解也忠實記錄了這個範圍決定，並引用 design.md。
5. **2026-07-12**，主 session 抽查（本任務背景第 2 點）用 scratch 三組 probe（A：只有
   `dependencies:`、B：`dependencies:`+`marketplace:` 並存、C：只有 `target: claude`）證實
   Probe A/C 情境下 apm-go `pack` 印「無 marketplace 區塊，無事可做」、exit 0、**完全不寫任
   何檔案**，而 Python 端在同樣輸入下會產出 plugin bundle（Probe A）或專案根 plugin.json
   （Probe C）（詳細 transcript 見 `research/group-packaging.md` §1.3）。

### (a) 缺陷在流程哪一步進來

範圍決定發生在**父任務拆分那一刻**（時間軸第 1 點），而不是子任務 design.md 撰寫時才決定——
子任務只是忠實記錄了一個更早、更上游就已定案的邊界。父 PRD 拆分子任務時，用的是「Python 這個
指令叫 `apm pack`，我們需要一個能產生 marketplace.json 的指令，就叫它 `apm pack`」的思路，
**沒有先做「Python `apm pack` 的完整行為面是什麼、我們準備覆蓋幾分之幾」的盤點**，就直接把
子任務命名、定範圍。這個盤點直到本任務（`07-12-cli-surface-parity-audit`）才第一次做
（`group-packaging.md` §1.1 的三個 producer 表）。AC 裡唯一涉及 Python 對照的 AC4（prd.md
:35）把 A/B 測試範圍限定在「子任務 3 scaffold 出的範例專案」——這個範例專案**天生只有
`marketplace:` 區塊**，所以 AC4 從設計上就不可能覆蓋到「有 `dependencies:`/`target:` 但無
`marketplace:` 的專案會發生什麼」這個後來被證實是本案最危險的情境。

### (b) 為什麼既有驗證沒攔住

兩輪對抗性審查的**選題標準**（design-gaps-and-verification-needed.md:114-121，「挑 3-5 條
影響最大的設計決策」）把「範圍決定本身是否安全」排除在外，只審「範圍內的演算法對不對」。這
是一個**選題偏誤**：對抗性審查越是嚴謹地驗證「marketplace.json 的欄位映射有沒有做對」，就越
不會有餘力去問「這個功能只做一半，範圍外的使用者會發生什麼」這種更上游的問題——因為那個問題
不屬於「這份 design.md 涵蓋的決策」，審查者是對著 design.md 逐條核對，design.md 沒寫的東西
審查者也不會主動去 Python 端翻出來對照。AC4 的 A/B 測試同樣是「驗證我們做的東西做對了」，不是
「驗證我們沒做的東西有沒有被使用者踩到」——測試 fixture 的選擇範圍與實作範圍同構，天生無法
暴露範圍外的風險。

### 本案結論

範圍決定本身有留下文件（不是遺漏型缺陷），且範圍內的實作品質經過兩輪高強度對抗性審查、有
A/B 測試佐證。缺陷來自**審查與測試的範圍恆等於實作範圍**，沒有任何步驟去驗證「範圍外的輸入
會不會被誤判為『無事可做』而不是『這個功能不存在』」——而根據本任務分類法，「同名指令 exit 0
+ 無輸出 + 無警告」正是風險最高的模式。

---

## Case 3：`audit` 同名異義（lockfile SHA 完整性重驗 vs Python 隱藏 Unicode 掃描）

### 時間軸（file:line 佐證）

1. **2026-06-30**，task `06-30-phase5-security` 引入 `cmd/apm/audit.go`（commit `0c76f58`）。
   `req-sc-001`（prd.md:27）文字：「Every deployed file has a recorded SHA-256…**`apm audit`**
   (and frozen install, lk-017) re-verifies on-disk bytes」——**規範文字裡的「audit」一詞
   來自 OpenAPM v0.1 spec §10.4**（`research/spec-and-oracle.md:14`：「re-verify on
   **audit**」），不是來自對 Python CLI 現有 `apm audit` 指令行為的研究。
2. 對整個 `06-30-phase5-security` 任務目錄（`prd.md`/`design.md`/
   `research/spec-and-oracle.md`/`research/review-cycle.md`/`implement.md`/`check.jsonl`）
   做全文搜尋 `audit.py`/`commands/audit`/`ContentScanner`/`_check_drift`/`homoglyph`/
   `unicode`（大小寫不敏感）：**零命中**。Python 端確實存在 `src/apm_cli/commands/audit.py`
   （已現場確認檔案存在），但整個任務的研究/設計/審查過程沒有任何一處讀過這個檔案。
3. `design.md:151-157`「`cmd/apm/audit.go`（sc-001）」段落直接定義 `apm audit` 指令為
   「讀 lockfile → 重驗 SHA-256 → 印違規」，沒有任何一句話討論「Python 端是否已經有一個叫
   `audit` 的指令、它做什麼」。
4. `review-cycle.md` 記錄的兩輪外部審查（opus 兩輪 + codex 黑箱）驗證目標明確是「**對照
   spec + oracle fixture**」（Round 1 verdicts 逐項對照 sc-001~sc-008 與 lk-013/016/017；
   codex 黑箱測的 7 個 case 也全部是 oracle fixture 案例），沒有任何一項驗證是「對照 Python
   `apm audit` 的既有行為」。
5. **2026-07-12**，本任務 `group-integrity.md` 用雙邊 live probe 首次證實：同一份被竄改的
   檔案，apm-go `audit`（bare）exit 1 攔下，Python `apm audit`（bare）exit 0「no issues
   found」放行——Python 真正的 SHA 比對邏輯藏在 `apm audit --ci` 第 6 項檢查
   `content-integrity`（`ci_checks.py:280-375`），且預設 fail-fast 會被前面 6 項檢查擋住看
   不到。

### (a) 缺陷在流程哪一步進來

進入點是 **PRD 撰寫階段的需求文字**：`req-sc-001` 直接借用了 OpenAPM spec 裡「re-verify on
audit」這個規範性動詞短語，把它字面翻譯成一個叫 `audit` 的 CLI 指令，**沒有先做一個一行
的檢查**——「Python CLI 樹裡是否已經有一個同名指令？如果有，它的既有語意是什麼？」。這與
case 1、case 2 不同：case 1/2 都做過 Python 原始碼研究（只是深度或範圍不夠），case 3 的
研究**從未以 Python `apm audit` 為研究對象**，因為整個任務的框架是「Phase 5 安全強化」，
權威依據明確寫在 `design.md:3-9`「Authority」段落——只有 spec 文字（`openapm-v0.1.md
§10.3–§10.9）與 immutable oracle，完全不含「與 Python CLI 既有指令做名稱/行為比對」這一類
輸入源。

### (b) 為什麼既有驗證沒攔住

兩輪外部審查（opus + codex）的**驗證目標從任務框架設定的那一刻就已經被限定**：`design.md`
的「Verification（anti-cheat, Phase V）」段落（:199-207）明文只提兩個比對對象——native go
test 對照 immutable oracle、外部黑箱對照 spec 行為——**Python CLI 既有行為完全不在這個
驗證契約列出的比對對象清單裡**。所以審查者（無論是 opus 或 codex）即使技術上有能力去讀
Python 原始碼，也沒有被要求這麼做，因為「這個指令是否與 Python 同名指令行為一致」根本不是
這次審查被賦予的任務。這與 case 2 的「選題偏誤」性質相同、但更根本：case 2 是「審查範圍
恰好等於實作範圍」，case 3 是「審查範圍從一開始就沒有把『是否存在同名 Python 指令』列為
應該檢查的維度」——因為任務本身被定義成純粹的 spec-conformance 工作，而不是 Python-parity
工作，兩種工作在這個專案的既有流程裡是**兩條不相交的檢查路徑**。

### 本案結論

本案是三個案例裡「Python 原始碼從未被讀過」的唯一一個——不是研究深度不夠，是研究對象一開始
就沒有包含 Python 現有的同名指令。根因在於：spec 驅動的功能開發與 Python-CLI-parity 驗證
在既有工作流裡是分離的兩條檢查路徑，沒有一個步驟強制「當 spec 文字促使你選用的 CLI 指令名稱
恰好撞上 Python 既有指令名稱時，必須先確認兩者是否同一件事」。

---

## 共通模式（Common Pattern）：Reused-Name Blind Spot（沿用既有指令名稱的盲點）

三個案例表面上是三種不同的失敗（內容轉換遺漏、範圍窄化、CLI 命名撞名），但拆解後共享同一個
結構：

| 面向 | Case 1 codex TOML | Case 2 pack | Case 3 audit |
|---|---|---|---|
| 借用了什麼名稱/表面 | `.toml` 副檔名（暗示目標格式） | `pack` 這個 Python 既有指令名 | `audit` 這個 Python 既有指令名 |
| 研究是否讀過 Python 對應行為 | 有，但只記錄 format_id 存在、未展開轉換邏輯 | 有，且對**已決定範圍內**的部分做了兩輪高強度對抗審查 | **完全沒有**——任務框架排除了這個比對維度 |
| 範圍窄化發生在哪一步 | design.md 用一句廣義「format transforms 延後」覆蓋了性質不同的 agents 轉換 | 父任務拆分子任務的當下就已把 `pack` 定義為「只做 marketplace.json」 | PRD 需求文字直接把 spec 用詞「audit」翻譯成指令名，未經比對 |
| 既有驗證為何沒攔住 | 驗證只查「檔案路徑是否正確」，不查「檔案內容是否合法目標格式」 | 驗證（A/B 測試 + 對抗審查）範圍恆等於實作範圍，不覆蓋範圍外輸入 | 驗證契約本身沒有把「Python 同名指令行為」列為比對對象 |
| 使用者實際踩雷方式 | 部署出的檔案是假 TOML，Codex CLI 無法解析 | 對 dependencies-only/target-only 專案跑 pack，exit 0 卻什麼都不寫、無警告 | bare `audit` 在兩邊做完全不重疊的檢查，竄改可能被其中一邊放行 |

**共通根因**：這個專案的「與 Python 原版對齊」工作，實務上是**逐指令、逐 task 各自決定驗證
範圍**，而每次驗證範圍的邊界都被劃定在「這次任務打算實作的東西」，不是「這個 CLI 表面
（指令名稱、副檔名格式）在 Python 端真正代表的完整語意」。研究/設計/審查三個關卡都繼承了
同一個邊界，導致邊界外的落差沒有任何一關被設計成會主動去看。這正是本任務
（`07-12-cli-surface-parity-audit`）存在的理由——這次是用一個**獨立於任何單一實作任務**、
以「Python 完整指令表面」為基準的盤查，才第一次系統性挖出這三個各自埋藏數天到數週的缺陷。

---

## 系統性預防機制提案

以下提案依 PRD 要求分類：可落地為 spec 準則 / PRD 模板 gate / 登記冊 living-doc 更新規則。
**這些提案尚未落檔，需使用者確認後才寫入 `.trellis/spec/`**（PRD AC 要求）。

### 提案 1：PRD 模板 gate ——「同名指令完整表面盤點」

**目標案例**：Case 2（pack）、Case 3（audit），可推廣預防未來任何新指令撞名。

當一個新 task 要新增或修改的 apm-go CLI 指令名稱（含子指令）與 Python `apm <verb>` 既有指令
同名（不分大小寫），PRD 撰寫時**必須**新增一個小節，逐項列出：

1. Python 該指令的完整行為面（每個子功能/分支 + file:line 出處）——不是「我要做的那部分」，
   是「Python 這個名字底下實際存在的全部東西」（可參考本任務 `group-packaging.md`/
   `group-integrity.md` 的盤點深度當範本）。
2. 本 task 打算覆蓋哪一部分，未覆蓋的每一項必須明確選擇下列三選一並寫入 PRD：
   - (i) 本輪完整實作（parity）
   - (ii) 本輪不做，但**偵測到會觸發該未覆蓋分支的輸入時必須印警告/報錯**，禁止靜默
     no-op/exit 0
   - (iii) 明確記錄不做、附另開 task 的追蹤引用
3. 若 PRD 撰寫時 Python 沒有同名指令（如 case 3 的 spec 用詞恰好撞名），也要留一行「已確認
   Python 無此名稱指令」或「已確認 Python 有同名指令且語意為 X，本 task 與其（相容/不相容）」
   當作已檢查過的證據，而不是留白。

### 提案 2：驗證 gate ——「範圍外輸入必須被測、且輸出格式必須真正被解析」

**目標案例**：Case 1（codex TOML 內容）、Case 2（pack 範圍外輸入）。

- 任何 A/B 測試或 oracle 對照，只要牽涉到「本 task 刻意縮小 Python 原始範圍」的指令，測試
  fixture **不得只涵蓋已實作範圍**；必須至少有一組 fixture 專門構造「範圍外會觸發的輸入」
  （對應提案 1 的 (ii)），並斷言看到警告/錯誤，而不是斷言「exit 0 且無輸出」視為通過。
- 任何部署/輸出動作，若目標路徑的副檔名或已知位置隱含一個結構化格式（TOML/JSON/YAML/…），
  驗證**必須**用該格式的 parser 實際解析輸出位元組並斷言成功，不能只斷言「檔案存在於 oracle
  記錄的路徑」。這條規則可直接寫入 `.trellis/spec/backend/` 的既有 quality-guidelines 或
  另立一節，避免重演「路徑對、內容錯」的落差。

### 提案 3：研究 gate ——「format_id/轉換器只記存在不夠，必須展開」

**目標案例**：Case 1。

研究文件在記錄 Python 端一個具名轉換器/format_id 存在時（例如 primitive mapping 表裡的
`format_id` 欄位），**不得只記欄位值**；要嘛展開該轉換器的實際邏輯（file:line 佐證其輸入
輸出形狀），要嘛明確標註「此轉換邏輯尚未研究，設計階段不可假設為 plain copy」。這條可併入
`trellis-before-dev`/研究類 skill 的既有檢查清單。

### 提案 4：PRD 模板 gate ——「spec 用詞撞名檢查」

**目標案例**：Case 3，專門補提案 1 沒完全覆蓋的情境（spec 驅動而非 Python-parity 驅動的
任務容易忘記查 Python）。

任何 Requirements 段落的用詞直接來自 spec 文字（而非直接對照 Python CLI），若該用詞恰好是
一個常見動詞/名詞、有機會被實作成 CLI 指令名稱，撰寫 PRD 前必須用一行檢查「Python CLI 樹是否
已有同名指令」（`grep -r "^def <name>" src/apm_cli/commands/` 或查 `apm --help`/`apm <name>
--help` 即可，成本極低）。這條規則特別要求「純 spec-conformance 導向」的任務（如本例的
Phase 5 安全強化）也要跑這個檢查，不能因為任務框架是 spec-first 就跳過 Python-parity 檢查
——這是本提案要打破的「兩條不相交檢查路徑」問題。

### 提案 5：登記冊 living-doc 更新規則

**目標**：讓本任務的產出（`cli-surface-parity-register.md`）不只是一次性快照。

在 `.trellis/workflow.md`（或 `trellis-before-dev` skill）新增規則：任何 PRD 提案新增/修改
CLI 指令表面（新指令、新 flag、改變既有指令行為）時，**必須**先查閱
`cli-surface-parity-register.md` 對應項目的既有分類（若尚無項目則視為新增，完成後補登記），
並在該 task 完成時同步更新登記冊該列的狀態/證據——把登記冊變成每次觸碰 CLI 表面時的標準檢查
起點，而不是只在本次盤查任務裡被讀寫一次的文件。

---

## Caveats / Not Found

- 三案例的「既有驗證為何沒攔住」分析基於任務歸檔的 `check.jsonl`/`implement.md`/
  `research/*` 文件與 git commit message；沒有存取當時 review 的完整互動逐字稿（若有更細的
  session log 不在任務歸檔範圍內，本文件無法進一步還原）。
- 提案 1-5 是根據三案例歸納出的建議，**尚未與使用者確認、也尚未寫入 `.trellis/spec/`**——
  依 PRD AC 最後一項，需等使用者確認後由使用者或主 session 另行落檔（本 research 檔案本身
  不是 spec，只是 RCA 證據與提案初稿）。
- 未逐一驗證 `.trellis/workflow.md`/`trellis-before-dev` skill 目前的既有內容是否已有部分
  重疊機制（例如是否已有「PRD 需列 Python 對照」的既有要求但三個案例當時未被觸發）——若主
  session 決定採納提案，建議先讀一次這兩份文件確認提案是新增還是強化既有規則，避免重複定義。
