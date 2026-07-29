# Research: Loop Engineering / Graph Engineering — 防止多輪稽核收斂失敗的驗證迴路設計

- **Query**: 研究 loop engineering / graph engineering，為 `07-28-marketplace-plugin-parity` 設計一個可長期複用的「驗證迴路」機制，目標是防堵目前反覆出現的 premature-completion（過早宣告完成）與 verification theatre（驗證劇場）問題。
- **Scope**: mixed（外部 blog/論文 + 本 task 既有文件的內部案例）
- **Date**: 2026-07-29

---

## 背景材料（使用者已知悉，僅列出摘要作交叉引用錨點，不重複全文）

以下四篇文章使用者已提供入口點；本輪已把全文擷取存檔於暫存區並通讀，這裡只記錄**與本 task 直接相關的可操作結論**，細節見下方各節引用。

| 來源 | URL | 一句話定位 |
|---|---|---|
| Sonar Blog（2026-06-11） | https://www.sonarsource.com/blog/loop-engineering-without-verification-is-just-automation/ | 提出 two-tier stop condition：LLM verifier（advisory）+ deterministic gate（hard halt），並命名 premature-completion loop 與「two optimists agreeing」失效模式 |
| LangChain Blog（2026-06-16） | https://www.langchain.com/blog/the-art-of-loop-engineering | 四層迴路棧：Agent loop → Verification loop → Event-driven loop → Hill-climbing loop，第四層把 trace 分析結果寫回改善前三層 |
| Augment Code Guide（2026-04-20，更新至 2026-06-18） | https://www.augmentcode.com/guides/coordinator-implementor-verifier | Coordinator-Implementor-Verifier（CIV）三角色 + 兩層巢狀迴路；直接引用 VeriMAP（EACL 2026）為「最詳細的已發表機制描述」 |
| Zylos Research（2026-04-14） | https://zylos.ai/research/2026-04-14-graph-based-agent-workflow-orchestration-production | 2026 年圖編排框架收斂到的共同原語：typed state、conditional edges、checkpointing、interrupt/resume、subgraphs |

---

## 1. VeriMAP（EACL 2026）—— Planner 產生「可執行驗收條件」的機制細節

- **論文**：Xu, Zhang, Mitra, Hruschka, *Verification-Aware Planning for Multi-Agent Systems*, EACL 2026 Long Paper（`aclanthology.org/2026.eacl-long.353`），arXiv 版本 `arxiv.org/abs/2510.17109`（Megagon Labs）。

### 架構（四模組，對應本 task 的四個現有角色）

| VeriMAP 模組 | 職責 | 對應本 task 現況 |
|---|---|---|
| **Verification-Aware Planner** | 把任務拆成 DAG（node=subtask，edge=dependency），**同時**為每個 node 產生 Verification Functions（VFs） | 目前 `prd.md`/`checklist.md` 由主對話一次性寫成，R→AC 對照表是**事後**補的，不是**與拆解同步生成**的 |
| **Executor** | 只看到自己 node 的 instruction + 上游 context，不看全域任務 | 對應 child task 的 implementor |
| **Verifier** | 對每個 VF 跑判定：Python VF 用直譯器（deterministic）、NL VF 用 LLM 呼叫；**邏輯 AND 聚合**，任一 VF 失敗即整個 subtask 失敗 | 對應 codex 稽核；但目前稽核是**跑完全部之後**才做，不是逐 node 即時 gate |
| **Coordinator** | 拓樸排序 → 逐 node 執行「execute→verify」重試迴圈（預設上限 **3** 次）→ 全部重試失敗才觸發 replanning（上限 **5** 輪）→ 迭代上限保證 termination | 對應主對話的角色，但**沒有機器可讀的重試/replan 上限**，目前是人工判斷「還要不要再跑一輪 codex」 |

### 兩個直接可搬的設計決策

1. **VF 有兩種，不是只有一種**：
   - **Python VF**（deterministic，可重現）：型別、格式、結構化正確性。對應本 task 的 `go build`、`go vet`、`go test -list` 先行、`coverprofile` 聚合這類「跑得出 pass/fail 的指令」。
   - **NL VF**（semantic，經 LLM 判定）：語意是否符合、開放式判斷。對應本 task 的「這個 AC 的斷言句是否真的可否證」這類**只能靠稽核者讀懂語意**的檢查。
   - **關鍵**：論文的 Verifier **對兩種 VF 一視同仁地聚合**（AND），但錯誤分析（Table 4）顯示 NL-only 的 verifier（對照組 MAP-V）**false positive 率遠高於**「planner 產生的、以結構化輸出為基礎的」VeriMAP VF（例：BigCodeBench-Hard 上 MAP-V FP=71.62% vs VeriMAP FP=22.97%）。**結論對應到本 task**：`claim-evidence-guide.md` 的證據三件套本質上是在把「只能靠 NL VF 判斷的斷言」盡量轉換成「有 file:line + repro 可核對的 Python-VF 風格檢查」——這與 VeriMAP 的實證發現方向一致：結構化、可執行的檢查比開放式 NL 判斷更不容易兩個樂觀者互相同意。

2. **Coordinator 的 gate 規則是「不允許帶病往下游走」**：
   > "VeriMAP gates execution on verification: the Coordinator cannot proceed to dependent subtasks until upstream verification succeeds, preventing unverified parents from poisoning children."（引自 Augment Code guide，對 VeriMAP 機制的轉述）
   - 這正是 codex round 2 阻斷 3 抓到的問題的**反面案例**：`targets-init-shape` 與 `plugin-init` 之間出現了「子任務互相宣告依賴對方尚未驗證的產出」的循環，等於兩個 unverified parent 互相毒化對方。VeriMAP 的 DAG 語意規定邊只能單向、且下游只能在上游 verified 之後才能開始——本 task 現行的 4-child 拆分**在 round 2 之前並不滿足這條規則**（見下方第 3 節的具體修法）。

3. **Case study 的教訓**（Olympiads 範例，Section 3.4）：MAP-V 的 verifier 用「只看上一步 context」的 generic NL 判斷，漏掉了一個看似合理但實際錯誤的答案（`x=-2` 因分子分母有公因式而應被排除）；VeriMAP 因為 planner 在下游 node 明確加了 Python VF（檢查 `free_symbols`）並帶入上游必要 context，才抓到錯誤。**對應本 task**：round 2 稽核發現的「R5.5 要求 local source 在『上述所有情境』都不觸網，但 AC21/AC40 只測了單點」——這正是「verifier 判斷範圍窄於 planner 宣稱的範圍」的同一類錯誤，VeriMAP 的解法是**verifier 的判斷粒度必須追上 planner 寫下的宣稱粒度**，不能用一個粗粒度檢查去覆蓋一個細粒度宣稱。

4. **量化的重試/replan 預算**：VeriMAP 預設「每個 subtask 重試 3 次」「replanning 上限 5 輪」，超過即回報失敗而非無限迴圈。**本 task 現況**：codex 稽核已經跑了 2 輪（round 1 五個阻斷級、round 2 五個阻斷級，其中 3 個是 round 1 遺留 + fixture 修出的新問題），沒有機器可讀的「第 3 輪如果還有阻斷級就升級為人工決策」規則——這是可以直接借用的機制。

---

## 2. Formal / Deterministic Gate 與 Loop 的結合模式

### 2.1 Forge — Formal-Method-Guided Vibe Coding（arXiv 2606.22413，2026-06-21）

- 作者：Wei, Zhu, Wang, Woodcock, Yan, Foster, Ji。
- **核心設計**：LLM 是 draft generator，一條 **Model-Driven Engineering (MDE) chain** 是 discriminator。LLM 生成 Java 原始碼後，pipeline 用 model transformation 抽出三種形式化 artefact，分別交給互補的 verifier：
  - Dafny（deductive verification）
  - FDR4（CSP refinement checking）
  - Isabelle（Z-Machines theorem proving）
  - **每一次驗證失敗都被轉成一個「結構化修正提示（structured correction prompt）」**，驅動下一輪 code-generation。開發者全程不需要讀懂這些形式化模型。
- **可搬的抽象**：這是「**verification failure → structured correction prompt → next iteration**」的顯式資料流，而不是把失敗訊息原封不動丟回去讓 LLM 自己猜。**對應本 task**：codex 稽核報告目前的格式（阻斷級條目附 `位置 / 為什麼是錯的 / 建議`）已經接近這個模式，但「建議」欄位目前是自然語言散文，不是可以被下一輪 subagent 直接消費的結構化欄位（例如「哪個檔案的哪一行要改成什麼」）。可以考慮把稽核報告的「建議」欄位改成更貼近 Forge 的 structured correction prompt 格式：`{file, line, current_claim, required_evidence_type, suggested_fix}`。

### 2.2 Agentic AI-based Coverage Closure for Formal Verification（arXiv 2603.03147，2026-03-03）

- 作者：Pothireddypalli, Raman, Gadde, Kumar。屬硬體驗證（IC/RTL）領域，但其「coverage closure loop」的三步驟結構與本 task 的 R→AC 覆蓋率矩陣問題**同構**：
  1. **Automated gap classification**：分析 coverage report，把未覆蓋的 RTL 區域分類成 control-flow branch gap 與 statement-level gap。
     - **對應本 task**：round 2 稽核阻斷 2 做的正是這件事——把「覆蓋率表宣稱『足以否證』」逐格拆解成具體的 gap 類型（缺尾端符號邊界、缺 `--no-verify` 分支組合、缺欄位值域測試等）。
  2. **Targeted property generation**：LLM 針對「每一個未覆蓋位置」產生新的 SystemVerilog property（等同於新增一條 AC/assertion），而不是重寫整份驗證計畫。
     - **對應本 task**：round 2 的建議正是「每個反例增加一條可獨立轉紅的 AC/G check」，而不是重寫整份 checklist——兩者是同一個修復顆粒度原則。
  3. **Iterative agentic coverage closure**：新 property 併入後，**重新跑一次 coverage report**，迴圈直到達到預先定義的門檻（threshold），而不是跑一次就假設涵蓋了。
     - **對應本 task 的缺口**：目前沒有一個「覆蓋率矩陣的門檻」與「重新計算覆蓋率」的機制——`checklist.md` 的 R→AC 對照表在 round 1 被推翻一次、round 2 又被推翻一次，兩輪都是**人工重新逐格判斷**，沒有把「gap classification → 補 AC → 重算覆蓋率」變成一個可重跑、可終止的迴圈定義（例如：什麼時候可以宣告覆蓋率矩陣「收斂」？目前答案是「codex 這輪找不到新反例」，這是主觀停止條件，不是門檻式停止條件）。

### 2.3 兩篇論文的共同結構（可直接寫進本 task 的驗證迴路設計）

```
未覆蓋/未驗證項目清單
   │
   ▼
分類（哪一類缺口：格式 / 分支 / 邊界 / 語意）
   │
   ▼
針對「每一個」缺口生成一條新的可執行檢查（不是重寫整份文件）
   │
   ▼
重新跑一次「全部檢查」（不是只跑新增的）
   │
   ▼
達到門檻？ ── 否 ──┐
   │ 是              │
   ▼                 │
標記收斂 + 記錄門檻值 ◄┘（迴圈，帶上限）
```

---

## 3. DAG-based 依賴／Edge Contract —— 用本 task 自己的循環依賴案例還原設計

這一節不是外部研究，是**讀本 task 既有文件**得到的內部案例，用來把上面兩節的抽象原則落地成本 task 可以直接複用的具體規則。

### 3.1 已發生的循環依賴（round 2 阻斷 3，已修復）

讀 `D:\Projects\apm-dev\apm-go\.trellis\tasks\07-29-targets-init-shape\prd.md:19-27`：

> 「修正前是一個循環：AC2/AC3 斷言 MultiSelect 的**預選狀態**，而預選狀態只存在於傳給 `multiSelectWith` 的 opts 裡（parent `design.md:268` 自己寫明『沒有這個 seam 就無法斷言』）；但 Step 3 原本被指派給 `plugin-init`，而 `plugin-init` 又宣告依賴本 task → 本 task 需要 plugin-init，plugin-init 需要本 task。」

這正是 VeriMAP DAG 語意會直接擋下的錯誤：**一個 node 的 verification function 依賴另一個 node 尚未產出的 seam，而那個 node 又反向宣告依賴本 node**——在 DAG 上這不是「循環」，是「邊根本沒有畫對方向」。

### 3.2 修復後的 edge contract（本 task 目前實際採用的規則）

修復做法是把 **AC-L0（ux seam 建立）**移到 `targets-init-shape`，讓它變成 `plugin-init` 的**前置**而非反向依賴：

```
targets-init-shape
  ├─ AC-L0: 建立 ux 測試 seam（confirmWith/multiSelectWith/inputFormWith → package-level var）
  ├─ AC2/AC3: 用 seam 斷言 MultiSelect 預選狀態
  └─ AC29: R1.4 端對端部署測試（round 2 從 plugin-init 移入，因為它驗的是 R1，不是 R3/R4）
         │
         ▼（單向依賴：plugin-init 消費 targets-init-shape 的 seam）
plugin-init
  └─ 使用已建立的 seam 驗 R3/R4 的互動流程
```

### 3.3 可提煉的通用規則（供本 task 及後續 child task 拆分時複用）

1. **邊的方向必須用「誰的 verification function 需要誰的產出」來畫，不是用「誰的需求編號在前」來畫。** 本 task 的錯誤根源是 AC 依 R 編號（R1→R2→R3→R4）被分派到對應的 child，但**測試手段**（ux seam）跨越了 R 編號邊界——R2/R3 的驗收需要一個屬於「基礎設施」而非「屬於任何一個 R」的共用前置。
2. **一個 child 宣告「無前置」時，必須同時確認它的 AC 驗法沒有引用任何尚不存在的測試 seam / helper / fixture。** 這是 round 2 阻斷 3 能被抓到的關鍵檢查點：讀 `implement.md` 的 Step 順序（Step 3 必須先於 Step 5）與讀 child prd.md 的「前置」欄位是否一致——**兩份文件各自宣稱不矛盾，不代表組合起來沒有循環**，必須把兩份文件的依賴宣告**在同一張圖上**核對。
3. **Parent 層級的 constraint（C5：不新增第三方相依）必須複製成每個 child 的本地 gate，而不是只留在 parent 的閘門。** 這是 round 2 阻斷 4：三個 child 的本地 build/vet/coverage 閘門原本沒有繼承 parent 的「不新增相依」約束，導致「child 通過」與「parent 通過」出現落差——對應到 DAG 語意就是「parent 的 global invariant 沒有被下推成每個 node 的 local verification function」，這正是 VeriMAP「每個 subtask 都要有自己的 VF，不能只靠全域一次性檢查」的同一個原則。

### 3.4 可操作的「edge contract」檢查清單（給下一次做 child 拆分時用）

- [ ] 對每個 child，列出它的每一條 AC「驗法」實際呼叫到的 helper / seam / fixture；反查這些 helper 是哪個 child 的哪個 Step 建立的。
- [ ] 把「child → 前置 child」的宣告與「AC 驗法 → helper 建立者」的反查結果畫成同一張圖；若出現雙向邊，就是循環，必須重新指派 Step 歸屬（如 3.2 的修法）。
- [ ] Parent 層級的 global invariant（相依限制、覆蓋率門檻等）逐條複製為每個 child 的本地 AC-L 系列檢查，不允許「只在 parent 驗一次」。

---

## 4. 可執行的「完整性 / 追溯性」計算方式

### 4.1 本 task 已有的實作範例

`D:\Projects\apm-dev\apm-go\.trellis\tasks\07-28-marketplace-plugin-parity\requirements-trace.md` 本身就是一份 Requirements Traceability Matrix（RTM）的雛形，且已經內建了「兩個方向都要覆蓋」的原則（文末「收尾閘門」）：

- **方向 1 — 計畫覆蓋（實作 ⊨ 計畫）**：G1–G5，對應「checklist 是否被逐條驗過」。
- **方向 2 — 需求覆蓋（計畫 ⊨ 使用者要的）**：G6–G9，對應「PRD 是否真的涵蓋使用者原話」。

這與 VeriMAP／coverage-closure 論文共同缺的一塊互補：**兩篇論文都只處理「planner 產出的 DAG 是否被 verifier 驗過」（方向 1），沒有處理「planner 的分解本身是否偏離了原始需求」（方向 2）**。`requirements-trace.md` 的 U8（`schema` 需求曾經被漏掉，因為「有沒有落差」被誤答成「要不要建立」）就是方向 2 失效的具體案例——`grep -rn "schema" */prd.md | grep AC` 在修正前零命中，證明**光靠「checklist 每條都驗過」的方向 1 完整性，抓不出「需求根本沒有落地」的方向 2 缺陷**。

### 4.2 與 coverage-closure 論文的「門檻」概念對照

`requirements-trace.md` 目前的收斂條件是**質化的**（狀態欄不得有 ❌ 或 ⚠️），沒有像 coverage-closure 論文那樣的**量化門檻**（例如「未覆蓋位置數 = 0」或「coverage % ≥ threshold」）。可以考慮的量化補強（僅供設計參考，非本輪已驗證的建議，本研究不主張立即採用）：

- R→AC 對照表的「完整覆蓋」判斷可以量化成「每個反例是否都對應到至少一條**會轉紅**的 AC/G check」，而不是質化的「已否證/部分否證」欄位——round 2 阻斷 2 已經指出這個質化欄位在多處失準（R1.4、R3.3.a、R4.2/4.3、R5.2/5.5、R8.3/8.4、R9.4、R10.4）。

---

## 5. Convergence Failure / Loop Engineering 失效模式

### 5.1 已命名的失效模式（來自背景材料，直接對應本 task 已發生的事）

| 失效模式 | 定義（來源） | 本 task 對應的具體事件 |
|---|---|---|
| **Premature-completion loop** | Sonar：「an agent signals completion on a half-done job... it fails quietly, declaring success on work that isn't done」 | `claim-evidence-guide.md` 記錄的四個被推翻斷言（「本專案優於上游」「純呈現層差異」「設計較佳無缺口」「成本大」）——四句共同結構是「對程式碼下判斷，卻沒讀那段會推翻自己的程式碼」 |
| **Two optimists agreeing** | Sonar：「If that judgment comes from the same model that did the work or from a second model asked politely to review — you have two optimists agreeing」 | round 1 稽核報告自述「同模型自我重讀與原錯誤相關」（`claim-evidence-guide.md:109-111`），這正是為什麼規則要求「未經外部驗證不得宣告完成」 |
| **Premature task completion（CIV 五大失效模式之一）** | Augment：「An agent joins a workflow mid-stream and concludes the task is done... No shared progress state; absent stop conditions」 | round 2 稽核本身在環境降級（Windows sandbox 無法啟動）時，**沒有**因此宣稱「稽核完成」，而是把「文件靜態驗證」與「實際命令未跑」分開列出——這是正確示範，但同一份報告也記錄了「獨立 codex 複驗 5 分鐘逾時，明確標成『外部複驗未取得結果』，不假裝自己重讀等同獨立驗證」，說明這個失效模式的**誘惑**在本 task 是真實出現過的，只是被守則擋住了 |
| **Verification theatre（本研究命名，非直接引用）** | 對照 Sonar 的「A failing build is a fact; an opinion is a starting point」 | round 2 阻斷 1（repo 外 fixture 的相對路徑解析錯誤）與阻斷 5（T2 tripwire 沒有實際跑出數字，只宣稱「已執行」）都是「看起來有驗證動作，但驗證本身不可執行或未執行」的案例——**動作存在不代表驗證存在** |

### 5.2 Sonar 的核心論點（可直接採用的設計原則）

> "Put the probabilistic checker first, where its judgment about intent adds value. Put the deterministic gate last, as the thing the loop actually stops on... A loop that has only the first tier is the Ralph Wiggum loop with extra steps."

**對應本 task 的落地建議（僅描述對照，非本研究主張立即修改流程）**：目前的稽核順序是「codex（LLM-based，探索式，找出候選缺陷）→ 主對話獨立複驗（讀 file:line、實跑指令）」——這已經符合「probabilistic first, deterministic last」的順序。但 round 2 報告本身指出還有 4 項「主對話尚未複驗」的發現（阻斷 1、2、5、重大 1），也就是說**deterministic 這一關目前還沒有跑完**，這正是 Sonar 論點裡「the gate is the thing you build last and trust most」尚未完成的部分。

---

## 6. Review Artifact / Provenance 設計（reviewer 看什麼，不看什麼）

### 6.1 Augment CIV 的具體設計

> "In a CIV workflow... a human reviews the Coordinator's spec, DAG, and parallelism plan before any code is written, then reviews Verifier output and approves commit, PR, and merge, **seeing passed subtasks and retry history rather than a raw diff**."

以及對 Verifier 元件的表格化拆解（見 fetched 全文，已存於暫存區，此處引用其結構）：

| Verifier 元件 | 機制 | 對應本 task |
|---|---|---|
| Spec compliance（formal） | Formal equivalence checking | 對應「AC 斷言是否精確等於 design.md 的字面規格」（round 1 重大 1：AC7 對註解骨架的條件太寬，未逐字斷言五行骨架） |
| Automated testing | Dedicated test executor agent | 對應 `go test -run` 前先 `go test -list` 證明匹配非空 |
| Verification function assignment | Planner 為每個 subtask 產生 verification module，全部須過 | 對應 R→AC 對照表，但如第 2.2 節所述，目前沒有「重新計算覆蓋率」的迴圈 |
| Feedback format | Structured diagnostics（compiler errors, counterexamples） | 對應 codex 稽核報告的「位置 / 為什麼是錯的」欄位；**尚未**結構化到可被下一輪 subagent 直接消費（見第 2.1 節 Forge 的建議） |
| Retry limit | 每 subtask 預設 3 次；replanning 上限 5 輪 | 本 task 目前沒有機器可讀的輪數上限（見第 1 節第 4 點） |

### 6.2 五個實作案例的角色映射對照表（Augment 原文表格，供設計參考）

| CIV 角色 | Osmani | Composio | GitHub Spec Kit | Anthropic |
|---|---|---|---|---|
| Coordinator | Planner | Orchestrator | Specify + Plan | Lead Agent |
| Implementor | Worker | Spawned subagents | Implement | Subagent/Worker |
| Verifier | Judge | CI Review + Merge | PR Review（human） | Evaluator |
| Isolation | Per-agent context | Docker sandbox | Version-controlled artifacts | Git worktrees |

**關鍵分歧點**（Augment 原文明列三項，直接抄錄以供本 task 對照自身選擇）：
1. **Runtime vs. human Verifier**：VeriMAP/Cosmos 用自動化 gate；Spec Kit 完全靠人類 PR review。**本 task 目前是混合**：codex 是自動化的 LLM verifier（但不是 deterministic），最終仍需人類（使用者）核准裁定。
2. **Static vs. shared context**：Spec Kit 的 artifact 寫一次就消費；Cosmos 是雙向的（agent 邊做邊寫回）。**本 task 的 `requirements-trace.md` 和 `checklist.md` 目前是單向寫入後被稽核讀取**，稽核發現的問題再手動寫回主文件——沒有自動化的雙向同步。
3. **Isolation primitive**：per-agent context alone 留下 file-overwrite 風險；worktree 才能真正隔離。**本 task 是單一 branch 單一工作目錄**，不涉及並行 Implementor，這項風險本身不適用，但可作為未來若要並行拆分 child 實作時的提醒。

---

## 檔案與素材存放位置（供之後引用，非最終文件）

以下是本輪研究過程中擷取全文並暫存的檔案（不在 repo 內，Windows 暫存目錄，供同一 session 內快速回查；**跨 session 不保證存在**，關鍵引用已摘錄進本文件正文）：

- `C:\Users\gn006\AppData\Local\Temp\verimap_full.txt` — VeriMAP arXiv HTML 全文純文字化
- `C:\Users\gn006\AppData\Local\Temp\sonar1.txt` — Sonar blog 全文
- `C:\Users\gn006\AppData\Local\Temp\lc1.txt` — LangChain blog 全文
- `C:\Users\gn006\AppData\Local\Temp\civ.txt` — Augment Code CIV guide 全文
- `C:\Users\gn006\AppData\Local\Temp\zylos1.txt` — Zylos graph orchestration 研究全文
- `C:\Users\gn006\AppData\Local\Temp\coverage_full.txt` — arXiv 2603.03147 全文
- `C:\Users\gn006\AppData\Local\Temp\a1.html` / `a2.html` — arXiv 2606.22413 / 2603.03147 abstract 頁面

---

## Caveats / Not Found

1. **外部搜尋工具不可用**：本次研究環境未提供 `mcp__exa__*` 或任何 WebSearch 工具（僅有 Read/Write/Glob/Grep/Bash/Skill）。Google、Bing、DuckDuckGo（html 與 lite 兩種端點）、searx.be、mojeek 均因反爬蟲機制回傳空結果、429、403 或需要 JS 才能取得結果，**不是資料不存在**，是這些搜尋引擎的 scripted-access 防護擋下了搜尋請求。實際內容是靠**站內 sitemap.xml 直接定位文章 URL**（`sonarsource.com/sitemap.xml`、`www.langchain.com/sitemap.xml`、`www.augmentcode.com/sitemap.xml`、`zylos.ai/sitemap.xml`）+ 直接 fetch 該 URL 取得，因此**未系統性搜尋這四個來源之外是否還有其他相關文章**，只驗證了使用者已指名的四篇。
2. **VeriMAP 的原始碼倉庫未讀**：DuckDuckGo 搜尋結果中出現 `github.com/megagonlabs/veriMAP`，但本輪未實際 clone 或讀取該倉庫程式碼，僅讀了論文全文（含附錄目錄，但未展開附錄 A–D 的實際 prompt 文字與訓練細節，因 PDF 無法在本環境渲染，`pdftoppm` 未安裝）。若要核對 VeriMAP 的 verification function 實際 Python 語法或 prompt 模板，需要另外讀附錄或原始碼。
3. **第 3 節（DAG edge contract）是本研究對既有修復案例的**歸納**，不是外部驗證過的通用理論**——它只反映 `07-29-targets-init-shape/prd.md` 這一個修復案例的結構，尚未在本專案的其他 child 拆分場景中重複驗證過是否普適。若要當作固定規則採用，建議至少再找一個獨立案例交叉驗證。
4. **第 4 節「量化門檻」是設計參考，非本研究主張的具體改動**——`requirements-trace.md` 目前的質化收斂條件（狀態欄無 ❌/⚠️）是否要換成量化門檻，屬於流程設計決策，本研究只指出兩者的差異與 coverage-closure 論文的對照，未評估改動成本。
5. 本文件的第 1、2、5、6 節內容全部有 `file:line` 等級的來源可回查（見上方暫存檔案路徑與 arXiv/URL），第 3、4 節屬於「讀本專案既有文件後的歸納」，來源是 `07-29-targets-init-shape/prd.md:19-27` 與 `requirements-trace.md` 全檔，已在正文標註。
