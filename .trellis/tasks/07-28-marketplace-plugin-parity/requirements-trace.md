# 需求追溯矩陣（User Requirement Traceability）

> **為什麼需要這份檔案**
>
> 現有驗證鏈是 `checklist.md ← AC ← R ← prd.md`。
> 這條鏈能證明「實作符合 PRD」，但**證明不了「PRD 符合使用者要的」**。
> 如果在 PRD 層就會錯意，底下 82 條檢查會完美地驗證一個錯的東西。
>
> 這份矩陣補上缺的那一段：**使用者原話 → 我的解讀 → 落點 → 驗證方式**。
> 每一條需求都必須落到一個可驗證的地方，或被使用者明確裁定為排除。
> **「我覺得涵蓋到了」不算落點。**

## 狀態定義

| 狀態 | 意義 |
|---|---|
| ✅ AC | 有對應的 Acceptance Criteria，會被 checklist 驗到 |
| ✅ 產物 | 有具體交付物（研究文件、spec、branch 等），但不是 AC 型 |
| ⚠️ 需確認 | 我做了解讀但**未經使用者確認**，可能會錯意 |
| ⛔ 已裁定排除 | 使用者明確選擇不做（引用該次裁定） |
| ❌ 未落地 | 沒有落點 —— **這是缺陷** |

---

## A. 主動提出的需求

### U1 — 研究上游 marketplace/plugin 文件，分析本專案實作(Consume)與文件的缺口

> 原話：「research https://microsoft.github.io/apm/reference/cli/marketplace/ /
> https://microsoft.github.io/apm/reference/cli/plugin/ 首先分析目前專案的實作(Consume)與文件的缺口」

- **解讀**：先研究後分析，產出缺口清單。
- **落點**：`research/upstream-marketplace-plugin.md`（pin `634f7b603a8c`/v0.26.0）
- **狀態**：✅ 產物
- **驗證**：該檔案的四個「疑似缺口」逐一以上游 `file:line` 證偽，
  真缺口收斂為 `apm plugin` 整組缺席 + 兩個 targets 缺陷。
- **後續影響**：R1–R4 全部源自這份研究。

### U2 — 建立 branch，修正並補齊 marketplace/plugin 的錯誤與缺口

- **落點**：branch `feat/marketplace-plugin-parity`（`task.json` 已綁，base `main`）
- **狀態**：✅ 產物
- **驗證**：`git branch --show-current`

### U3 — 建 task，還有很多內容需要先研究

- **落點**：`07-28-marketplace-plugin-parity` + 4 個 child
- **狀態**：✅ 產物
- **驗證**：`python ./.trellis/scripts/task.py list`

### U6 — 我還有內容要提供，不要往前跑

> 原話：「我說還有部分內容需要研究...我都還沒有提供...」

- **解讀**：規劃階段暫停，等使用者補素材。
- **狀態**：✅ 產物（已遵守；素材於 U7 提供）
- **教訓**：這是本 session 第一次「我宣告階段完成但其實沒有」。

### U7 — 讀取 evals 資料夾並分析

> 原話：「讀取 D:\Projects\apm-dev\evals\apm-20260728T140015Z-1-001 資料夾內容並分析」

- **落點**：`research/eval-real-run-20260728.md`
- **狀態**：✅ 產物
- **驗證**：該檔案 A–F 節；證實前份研究結論、並新找到 C1–C7 七項缺口，
  其中 C1（`install --dev` 缺席）後來成為 R9。

### U8 — 建立各個 target agent 的 marketplace/plugin field 的 schema

> 原話（`新文件 8.txt` 第 1 行）：「建立各個 target agent 的 marketplace/plugin field 的 schema」

- **我原本的解讀**：研究並記錄各 target 的 schema 形狀 → **已被使用者否決**。
- **使用者裁定（2026-07-29）**：
  「建立一分獨立的 spec 文件 + 可執行的 schema 定義，**兩者必須同步**」
- **落點**：新 child task **`07-29-agent-schema-spec`**（R1 spec 文件 /
  R2 可執行 schema / R3 防漂移機制 · AC1–AC7）
- **狀態**：✅ AC
- **`research/agent-schema-support-matrix.md` 的定位改變**：
  從「交付物」降級為「**素材**」。它提供欄位清單與 golden 產物，
  但不是這條需求的交付。
- **當初漏掉的原因（留作教訓）**：我把「有沒有落差」當成「要不要建立 schema」回答了。
  查證方式：`grep -rn "schema" */prd.md | grep AC` 在修正前是**零命中** ——
  一條使用者明確提出的需求，在 82 條 checklist 裡沒有任何一條驗它。

### U9 — 用 PTY 執行 apm 取得實際 stdout，作為指令驗證的比對基準

> 原話（`新文件 8.txt` 第 2 行）：「使用 偽終端（Pseudo-Terminal） 驗證指令與 studio」
>
> 使用者澄清（2026-07-29）：「這是打錯字, 正確的是 **PTY 執行 apm 的實際 stdout**」

- **正確解讀**：驗證 apm-go 的指令輸出時，**上游參考輸出必須是在 PTY 下執行
  `apm` 取得的實際 stdout**，不能用非 TTY 的降級輸出。
- **為什麼重要**：上游用 rich/click，**非 TTY 時輸出會降級** ——
  沒有框線、沒有顏色、寬度不同。拿降級輸出當 golden 會比對到錯的東西。
- **現有素材是否符合**：✅ **符合，已驗證**。
  `新文件 7.md` 含 35 處 rich 專屬框線字元（`╭ ╰ ┏ ┗ │ ┃ ─ ━`），
  例如 `:12` 的 `┏━━━┳━━━┓`、`:19` 的 `╭─── Next Steps ───╮`。
  這些只在 TTY 下輸出，證明素材是 PTY 捕獲的。
- **落點**：
  1. parent `prd.md` 新增 **C7**（golden 來源約束）
  2. `07-29-plugin-init` 的輸出比對 AC（AC13、AC46）指定使用該 PTY 捕獲基準
- **狀態**：✅ AC
- **與先前「ux 接縫」裁定的關係**：**兩者不衝突，目的不同**。
  - ux 接縫（D4）＝ 測 apm-go 自己的互動**邏輯**（傳給 MultiSelect 的 opts 等）
  - 本項（U9）＝ 取得上游的**渲染輸出**當比對基準
  我先前把兩者混為一談，才會把這條誤記成「PTY 端對端測試，排除」。
- **仍然成立的覆蓋缺口**：apm-go 自己**在 TTY 模式下的渲染輸出**目前沒有任何測試
  （所有測試都走 `ux.CanPrompt()` 為 false 的純文字路徑）。
  這一點記在 parent Out of Scope，成本 80–150 LOC。

### U10 — 各個 AI Agent 的 marketplace/plugin 的 schema / 支援度

- **落點**：`research/agent-schema-support-matrix.md` 全檔，特別是 §0 的三軸表與 §4 的總表
- **狀態**：✅ 產物
- **驗證**：三軸（部署 targets / marketplace 輸出 / plugin.json 生態）各有
  上游與 apm-go 的 `file:line` 對照；結論「B、C 兩軸零缺口」已被 codex 獨立複核。
- **影響**：直接導致 D3（註解列 6 個）與 R8（三集合單一來源）。

### U11 — 用 subagent(opus) 建立 checklist，供 implement 完成後確認

- **落點**：`checklist.md`（968 行、82 條）
- **狀態**：✅ 產物
- **驗證**：`grep -c '\- \[[ x]\]' checklist.md` → 82

### U12 — 用 codex 驗證 checklist

- **落點**：`review/codex-audit-checklist.md`（第一輪）+ 第二輪進行中
- **狀態**：✅ 產物
- **驗證**：第一輪 5 阻斷級全部經主對話獨立複驗屬實並修復。

### U13 — 修正「宣告完成後才被使用者發現問題」的結構性問題

> 原話：「常常計畫完成後又讓使用者發現問題, 才在事後講一些廢話…
> 目前這個任務屬於大型任務, 使用者覺得依定會出現此問題」

- **落點**：三項結構性修正（使用者三項全選）
  1. `.trellis/spec/guides/claim-evidence-guide.md` — 絆線從詞表改句型
  2. `implement.md` 開頭 — 逐步 codex 閘門
  3. 4 個 child task 拆分
  4. `AGENTS.md` §5 — 完成宣告兩條硬規矩
- **狀態**：✅ 產物
- **驗證**：`ls .trellis/spec/guides/claim-evidence-guide.md`；
  `task.py list` 顯示 `[0/4 done]`；`grep -n "完成宣告的兩條硬規矩" AGENTS.md`

### U15 — `plugin init` 必須與 `init` 同互動風格並鎖住；`marketplace init` 維持非互動

> 原話（2026-07-29）：「目前的 marketplace init / plugin init 是否會採用與 init 的
> 動模式相同的風格」→ 追問後裁定：「**必須要補進, marketplace init 維持非互動**」

- **這一問揭露的缺陷**：計畫原本只靠「`plugin init` 複用 `init.go` 共用本體
  所以會自動繼承 clack」來擔保互動風格，**但零個 AC 驗它**
  （`grep -niE "clack|banner|intro|outro|風格" 07-29-plugin-init/prd.md` → 零命中）。
  實作若寫成獨立的非互動指令，AC8–AC13、AC30、AC31、AC38、AC41 **全部照過**。
  這是「它會自動來」這種無證據擔保，與「成本大」「純呈現層」同類。
- **落點**：parent D12/D13 → R11 → **AC52**（`07-29-plugin-init`）、
  **AC53**（`07-29-marketplace-add-fixes`）
- **狀態**：✅ AC
- **AC53 是回歸閘門**：`marketplace init` 現況本來就非互動（與上游一致 ——
  上游 `commands/marketplace/init.py` 的 `click.confirm`/`click.prompt` 出現 0 次），
  這條的用途是防止實作 `plugin init` 時順手把 clack 帶進去。
  **已實跑，現況 PASS**（`AC53/no-clack`、`AC53/no-block` 皆綠）。

### U14 — 必須完整確認任務結束後可以完整覆蓋計畫與使用者需求

- **落點**：**本檔案** + 文末的收尾閘門
- **狀態**：✅ 產物（本檔）
- **驗證**：見文末「收尾閘門」——這是 task 結束時必跑的檢查。

---

## B. AskUserQuestion 的裁定（每一條都必須在 PRD 裡找得到）

| # | 裁定 | PRD 落點 | 驗證 |
|---|---|---|---|
| D1 | targets 鍵讀寫都對齊複數 | `prd.md` D1 → R1 → AC1–AC4 | ✅ AC |
| D2 | 不補 `init --plugin` | `prd.md` D2 → Out of Scope | ✅（含 `git log -S--plugin` 零命中的證據） |
| D3 | 註解列 6 個而非 10 個 | `prd.md` D3 → R2.2 → AC6 | ✅ AC |
| D4 | 互動驗證走 ux 接縫，不引入 PTY | `prd.md` D4 → R7 → AC23、AC41 | ✅ AC |
| D5 | ~~`--dev` 另開~~ **已撤銷** | `prd.md` D5（劃線）→ R9 → AC42–46 | ✅ AC（撤銷理由附證據） |
| D6 | ~~category 閘門不對齊上游~~ **已修正** | `prd.md` D6（劃線）→ R10 → AC47–50 | ✅ AC |
| D7 | check 不表格化、install 不改寫入順序 | `prd.md` C2、C3（宣稱已收窄） | ✅（Out of Scope 附成本） |
| D8 | 修 `agent-skills` 白名單 + `promptTargetsOrdered` 漂移 | `prd.md` D8 → R8 → AC24–AC27 | ✅ AC |
| D9 | codex 其餘發現全部修進 PRD 與 checklist | `prd.md` D9 → AC36–41 等 | ✅ AC |
| D10 | 三項結構性修正全做 | 見 U13 | ✅ 產物 |
| D11 | 完成宣告：外部驗證 + 可執行證據，兩者都要 | `AGENTS.md` §5 | ✅ 產物 |

---

## C. 收尾閘門（task 結束時必跑，缺一不可）

「完整覆蓋」要同時滿足**兩個方向**，只做一個不算：

### 方向 1 — 計畫覆蓋（實作 ⊨ 計畫）

- [ ] G1：`checklist.md` 全部 82 條逐條驗過，每條附可執行證據
- [ ] G2：每個 child 的 AC 全數有測試佐證（`t.Skip` 不算）
- [ ] G3：AC 分割的機械驗證重跑，仍為 0 重複 0 遺漏
      （指令見 `prd.md` 的「AC 分割的機械驗證」）
- [ ] G4：每個 Deferral 的「證明此排除正當」逐條成立
- [ ] G5：Tripwire sweep（含 `checklist.md` 自己與 `*.jsonl`）無無證據的句型違規

### 方向 2 — 需求覆蓋（計畫 ⊨ 使用者要的）← 本檔的職責

- [ ] G6：**本檔 A 節每一條**的狀態欄不得有 ❌ 或 ⚠️
      —— 有的話代表存在未確認的解讀或未落地的需求
- [ ] G7：**本檔 B 節每一條裁定**都能在 PRD 找到落點且未被實作悄悄推翻
- [ ] G8：重新讀一次使用者的原話（不是我的摘要），確認沒有新的漏項
      —— 摘要會遺失資訊，這一步不能用摘要代替
- [ ] G9：所有 ⛔「已裁定排除」的項目，在交付說明裡**主動列出**，
      不能讓使用者自己發現少了東西

### 閘門規則

- 任一條不成立 → **不得宣告 task 完成**
- 依 `AGENTS.md` §5：宣告完成必須附外部驗證結果 + 可執行證據
- G6 有 ⚠️ 時，正確動作是**去問使用者**，不是自己裁定成 ⛔

---

## D. 待確認事項（阻擋 G6）

### ~~#1 — U8 的解讀~~ ✅ 已解決（2026-07-29）

使用者裁定：spec 文件 + 可執行 schema 定義，兩者必須同步。
已建 child task `07-29-agent-schema-spec`（AC1–AC7，含 R3 防漂移機制）。

### ~~#2 — U9b「studio」~~ ✅ 已解決（2026-07-29）

使用者澄清：**「studio」是打錯字，正確是「PTY 執行 apm 的實際 stdout」**。
已落到 parent `prd.md` 的 C7（golden 來源約束）與 `07-29-plugin-init` 的 C7。
現有素材已驗證符合（35 處 rich 框線字元）。

**教訓**：我當初讀不懂就寫成「使用者未澄清所指，不納入本 task 範圍」，
把一個**錯字**升格成了一個範圍決定。正確動作是問，而且要單獨問 ——
第一次問的時候我把它綁在另一個問題裡，回答只選了另一半，這條就掉了。

---

## ✅ D 節目前為空 —— 無阻擋 G6 的未確認事項

A 節 14 條需求全部為 ✅，無 ⚠️、無 ❌。
（此狀態需在收尾閘門 G6 重新確認，不得沿用此處的結論。）

---

## 維護規則

- 使用者每提出一項新需求，**先加進本檔 A 節**，再決定要不要進 PRD。
- 本檔的 ⚠️ 與 ❌ 是**阻擋交付的**，不是待辦清單。
- 本檔不得用摘要撰寫 —— A 節的「原話」欄必須是使用者的原始文字。
