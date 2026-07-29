# Loop & Graph Engineering（迴圈與圖工程守則）

> **目的**：把「遺漏 / 延後 / 半成品被宣告完成」從**靠自律**變成**靠結構擋住**。
> 這份守則是 2026-07 業界 loop engineering / graph engineering 模式的內化，
> 對照本專案 `07-28-marketplace-plugin-parity` 的實際失效歷史而寫。
>
> **研究依據**：原始 findings 已由 `trellis-research` 落盤於
> `.trellis/tasks/07-28-marketplace-plugin-parity/research/loop-graph-engineering.md`（228 行）。
> 本守則是那份 findings 的內化，已據其修正過一輪（2026-07-29）。
>
> **證據強度分級**（研究者自行標註，不要混用）：
> - 模式 1、2、5、6、9 —— 有論文或原始來源，可 `file:line` 回查
> - 模式 3、4 —— **單一案例歸納**（就是本專案自己的循環依賴修復），
>   尚未在其他拆分場景交叉驗證。當規則用可以，但不要當定律。
>
> **已知的研究覆蓋缺口**：研究環境無 WebSearch，是靠各站 `sitemap.xml` 直接定位 URL
> 取得內容，因此**只驗證了主對話指名的四個來源，未系統性搜尋是否還有更好的做法**。

## 為什麼需要（本專案的實證失效史）

規劃階段連續三次「宣告完成 → 外部檢查抓到實質缺陷」：

| 次序 | 我宣告 | 外部檢查抓到 |
|---|---|---|
| 1 | 「規劃三件套齊了」 | subagent：6 個 R 子項零 AC 覆蓋 + AC25 自相矛盾 |
| 2 | 「checklist 好了，數字都對」 | codex 第一輪：5 阻斷級，含推翻使用者已裁定的 D5 |
| 3 | 「拆分乾淨，0 重複 0 遺漏」 | codex 第二輪：拆分造出**循環依賴** + AC 分錯 child |

三次的共同結構：**我用自我檢查滿足了閘門**。

Sonar 對這個現象的命名最精準 —— **「兩個樂觀主義者互相同意」**
（two optimists agreeing）：當驗證層與被驗證層都是機率性判斷時，
它們會**相關地**收斂到錯誤的自信。

---

## 模式 1：雙層停止條件（Two-Tier Stop Condition）

來源：Sonar, *Loop engineering without verification is just automation*

一個迴圈要能停，必須通過兩層，**順序不可顛倒**：

| 層 | 性質 | 本專案的實作 |
|---|---|---|
| **Tier 1 — 確定性閘門** | 每次執行結果相同、可追溯到具體規則、**agent 無法用一段有自信的摘要滿足它** | 每個 child 的 `verify.ps1`：build / vet / 指定測試 / 指定 grep，exit code 決定成敗 |
| **Tier 2 — 獨立 LLM 驗證** | 不同 context、不同模型、反向目標（去反駁） | `codex exec -s read-only` 的對抗性稽核 |

### 硬規則

- **確定性層是硬停（hard halt）**，不是建議。它紅了就不准往下。
- 確定性層**必須先跑**。LLM 驗證是加在上面的，不是替代。
- **agent 不得自行判定「這條不適用」而跳過**。要跳過必須改 `verify.ps1` 並經人審。
- 只有 LLM 驗證通過而確定性層沒跑 → **視同未驗證**。

### 命名的失效模式

> **premature-completion loop**：agent 對一份半成品發出完成訊號。
> 沒有硬性客觀停止條件時，迴圈**不會大聲失敗，它會安靜地失敗** ——
> 對沒做完的工作宣告成功。

### 論文級佐證（VeriMAP, EACL 2026 / arXiv 2510.17109）

雙層不是憑感覺，有量化證據。VeriMAP 的 Verifier 有兩種 verification function：

| VF 類型 | 判定方式 | 對應本專案 |
|---|---|---|
| **Python VF** | 直譯器跑，deterministic | `verify.ps1`：build / vet / `-list` / coverprofile / exit code |
| **NL VF** | LLM 語意判定 | codex 反向稽核：「這條 AC 的斷言是否真的可否證」 |

兩者**以邏輯 AND 聚合** —— 任一 VF 失敗，整個 subtask 即失敗。**不准擇一**。

錯誤分析（論文 Table 4，BigCodeBench-Hard）：

```
純 NL verifier（MAP-V）      false positive = 71.62%
結構化 VF（VeriMAP）         false positive = 22.97%
```

⇒ **只靠 LLM 判斷會有七成的假通過**。這是「兩個樂觀主義者互相同意」的量化版本，
也是 `claim-evidence-guide.md` 要求證據三件套的實證理由 ——
三件套的作用就是把只能靠 NL 判斷的斷言，盡量轉成有 `file:line` 可核對的
Python-VF 風格檢查。

---

## 模式 2：每個子任務自帶「完成的定義」（Verification Module）

來源：VeriMAP / Coordinator-Implementor-Verifier (EACL 2026)

> 「分解時把任務產生與驗證設計**耦合**：每個子任務同時產出指令與驗證模組，
> 所以每個子任務抵達時就帶著自己的『完成定義』。」

### 對本專案的意義

有 AC 清單**不等於**有驗證模組。AC 是散文，驗證模組是**可執行的**。

每個 child task 必須有：

```
.trellis/tasks/<child>/verify.ps1     ← 確定性閘門，exit 0 = 通過
```

`verify.ps1` 的規則：
- 只做**確定性**檢查（build / vet / test / 檔案存在 / grep 命中數 / exit code）
- 任何一項失敗立即 `exit 1`，並印出**哪一條 AC** 失敗
- 不得包含需要人判斷的項目 —— 那些留給 Tier 2
- **它是 AC 的執行體，不是 AC 的摘要**。AC 改了，它就要改。

---

## 模式 3：驗證閘住依賴邊（Gate Execution on Verification）

來源：VeriMAP

> 「Coordinator 在上游驗證成功前**不得**推進到相依的子任務，
> 避免**未經驗證的父節點污染子節點**。」

### 硬規則

- 子任務的前置不是「做完了」，是「**verify.ps1 綠燈了**」。
- 前置紅燈時，下游**必須標記為 blocked**，不得「先做著等一下再合」。
- 前置若被跳過，下游的所有驗收結果**一律作廢**（父節點污染）。

### 本專案踩過的坑

`plugin-init` 宣告依賴 `targets-init-shape`，但 ux seam 被指派給 `plugin-init`，
而 `targets-init-shape` 的 AC2/AC3 需要那個 seam
→ **循環依賴**，兩邊都以為對方先做。
由 codex 第二輪抓到（`review/codex-audit-round2.md` 阻斷 3）。

⇒ **依賴邊必須是有向無環的，而且要機械檢查**（見模式 4）。

---

## 模式 4：有型別的邊（Typed Edges / State Contract）

來源：LangGraph / Microsoft Agent Framework 的 typed state

> 「若某個節點回傳了下游不預期的欄位，型別系統會在**定義時**抓到，
> 而不是在凌晨三點的正式環境。」

### 對本專案的意義

依賴不能只寫「A 依賴 B」這種散文。**邊要載明它運送什麼**：

```
targets-init-shape ──[有序 YAML 產生器 buildManifestNode(manifestSpec)]──→ plugin-init
targets-init-shape ──[manifest.SupportedTargets 統一後的 6 元素切片]────→ plugin-init
targets-init-shape ──[ux seam：interactive.go 三個 var]───────────────→ plugin-init
install-dev ────────[apm-go install --dev 可執行]──────────────────→ plugin-init
```

**每條邊的載荷都要在下游的 `verify.ps1` 裡被實際檢查**
（例如 `plugin-init` 的 verify 先確認 `buildManifestNode` 存在、
`SupportedTargets` 長度為 6、`install --dev` 在 `--help` 裡）。
邊沒被檢查 = 邊不存在。

---

## 模式 5：重試上限 + 升級，**不准降級為「延後」**

來源：VeriMAP（每子任務 3 次執行-驗證迴圈上限，另 5 次重規劃上限）

### 硬規則

- 同一個 AC 修 **3 次**仍紅 → **停止重試，升級給使用者**。
- 升級時必須附：失敗的 `verify.ps1` 輸出、三次嘗試各改了什麼、為何無效。
- **升級 ≠ 延後**。延後是「我決定這個不做了」，升級是「我做不到，你決定」。
  把做不到寫成「延後 / 範圍外 / 不影響」是
  `claim-evidence-guide.md` 明文禁止的收斂性斷言。
- 重規劃（改 AC 或改設計）上限 **5 次**，超過表示 PRD 有結構問題，回到規劃。

---

## 模式 9：驗證粒度必須追上宣稱粒度（Granularity Matching）

來源：VeriMAP 論文 §3.4 的 Olympiads case study（arXiv 2510.17109）

論文的案例：MAP-V 的 verifier 用「只看上一步 context」的泛用 NL 判斷，
漏掉了一個**看似合理但實際錯誤**的答案（`x=-2` 因分子分母有公因式而應被排除）。
VeriMAP 因為 planner 在下游 node 明確加了 Python VF（檢查 `free_symbols`），才抓到。

> **教訓**：verifier 的判斷粒度必須追上 planner 寫下的**宣稱粒度**。
> 不能用一個粗粒度檢查去覆蓋一個細粒度宣稱。

### 硬規則

需求裡每出現一次**全稱量詞**，驗證就必須逐項展開，不得用單點代表：

| 宣稱裡的字眼 | 驗證必須 |
|---|---|
| 所有 / 每個 / 逐一 / 全部 | 逐項各一條檢查，**數量對得上** |
| 任一 / 其中之一 | 至少驗到邊界的頭尾 |
| 不變 / 仍然 | 前後各驗一次，不是只驗後 |

### 本專案的現場（這條規則直接抓到我們的缺陷）

| 需求的宣稱粒度 | 原本的驗證粒度 | 處置 |
|---|---|---|
| R5.5「local source 在**上述所有情境**皆不觸網」 | AC21 只測 local + 零旗標**一個**情境 | 已補 AC40（六個分支逐一） |
| R4.2 觸發條件涵蓋**六個目錄 + hooks.json** | AC14–16 只測 `skills/` **一個** | 已補 AC39，`verify.ps1` 逐一迴圈七個來源 |
| R7.1「Form / MultiSelect / Confirm **三條**分支」 | AC23「有測試覆蓋」**一句** | 已補 AC41，`-list` 要求匹配 ≥ 3 |

⇒ **開工前對每個 child 做一次「全稱量詞掃描」**：
把 PRD 裡的所有 / 每個 / 逐一挑出來，確認 `verify.ps1` 是逐項展開而不是單點。

---

## 模式 6：失敗定位與下游封鎖偵測

來源：graph engineering 的 failure localization

> 「當節點失敗時，失敗是被**局部化**的：圖結構能指出是哪一步產生錯誤、
> 它的輸入是什麼、以及**哪些下游節點因此被封鎖**。」

### 硬規則

任一 child 的 `verify.ps1` 紅燈時，必須立刻產出：

1. 失敗的**節點**與**具體 AC 編號**
2. 該節點的**輸入**（前置提供的邊載荷是否齊全）
3. **被封鎖的下游清單**

第 3 項是最常被漏掉的 —— 它防止「一個 child 卡住，其他人繼續做，
最後才發現全部要重來」。

---

## 模式 7：共享進度狀態（Shared Progress State）

來源：CIV 把「無共享進度狀態、缺停止條件」列為**具名失效模式**

### 對本專案的實作

單一機器可讀的進度來源，不靠散文同步：

- `python ./.trellis/scripts/task.py list` = 節點狀態
- 每個 child 的 `verify.ps1` 最近一次 exit code = 邊是否可通行
- `review/step-audits.md` = 逐步稽核的事件日誌（append-only，不覆蓋）

**禁止**在多份文件裡各寫一份數字（例如 checklist 82 條 vs implement 63 條）。
codex 第一輪就抓到過這種漂移。數字只能有一個來源。

---

## 模式 8：登山迴圈（Hill Climbing）—— 把稽核發現餵回守則

來源：LangChain, *The Art of Loop Engineering* 的 Loop 4

> 「回頭的箭頭不只是回到頂端 —— 它**伸進去直接更新 agent loop 本身**。」

### 硬規則

每一次外部稽核抓到的**失效類型**（不是個別缺陷），必須回寫到 `.trellis/spec/guides/`。

本專案已發生兩次，兩次都成立：

| 稽核發現 | 回寫到哪 |
|---|---|
| 四個被推翻的斷言與絆線詞表零重疊 | `claim-evidence-guide.md` —— 詞表升級為七種句型 |
| 「根因是 DNS 抖動」不落在七種句型裡 | 同上 —— 補「因果歸因」與「風險接受」兩類 |

修完個別缺陷卻不回寫守則 = 同類缺陷下次還會發生。

---

## 檢查表：開工前，每個 child 必須齊備

- [ ] `verify.ps1` 存在，且**實際跑過一次會紅**（用未實作的狀態驗證它不是空殼）
- [ ] 前置邊的載荷已在本 child 的 `verify.ps1` 裡被檢查
- [ ] 依賴圖無環（機械檢查，不是目視）
- [ ] 每條 AC 都能對應到 `verify.ps1` 的某一項，或明確標記為 Tier 2（需人/LLM 判斷）
- [ ] 被封鎖下游清單已寫明

## 檢查表：宣告某個 child 完成前

- [ ] Tier 1：`verify.ps1` exit 0（**先跑**）
- [ ] Tier 2：`codex exec -s read-only` 對該 child 的 diff 做反向稽核，無阻斷級
- [ ] 下游 child 的邊載荷檢查通過（證明沒有污染子節點）
- [ ] 若有 AC 未過 → 依模式 5 升級，**不得寫成延後**
- [ ] 若稽核抓到新的失效**類型** → 依模式 8 回寫守則

---

## 來源

- Sonar — [Loop engineering without verification is just automation](https://www.sonarsource.com/blog/loop-engineering-without-verification-is-just-automation/)
- LangChain — [The Art of Loop Engineering](https://www.langchain.com/blog/the-art-of-loop-engineering)
- Augment Code — [Coordinator-Implementor-Verifier Pattern](https://www.augmentcode.com/guides/coordinator-implementor-verifier)（含 VeriMAP, EACL 2026）
- Zylos Research — [Graph-Based Agent Workflow Orchestration in Production](https://zylos.ai/research/2026-04-14-graph-based-agent-workflow-orchestration-production/)
