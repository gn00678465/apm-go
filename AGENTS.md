## Overviews
這個專案是使用 golang 重新開發 microsoft/apm 的專案.

<!-- Available_COMMANDS:START -->
## Available commands
Golang 相關指令（於專案根目錄執行）：

| 指令 | 用途 |
|---|---|
| `go mod tidy` | 整理 `go.mod` / `go.sum` 相依 |
| `go build ./...` | 編譯整個專案（當前平台） |
| `go build -o bin/apm-go.exe ./cmd/apm-go`（Windows）/ `go build -o bin/apm-go ./cmd/apm-go`（其他平台） | 編譯二進位，輸出檔名永遠固定為 `apm-go`（不可用 `apm`/`apm.exe`） |
| `go build -trimpath -ldflags "-s -w" -o bin/apm-go.exe ./cmd/apm-go` | Release 尺寸編譯（去除除錯資訊與路徑，實測約小 29%；與 release workflow 同旗標） |
| `GOOS=windows GOARCH=amd64 go build -o bin/apm-go.exe ./cmd/apm-go` | 交叉編譯 Windows 二進位（PowerShell：`$env:GOOS='windows'; $env:GOARCH='amd64'; go build -o bin/apm-go.exe ./cmd/apm-go`） |
| `GOOS=linux GOARCH=amd64 go build -o bin/apm-go ./cmd/apm-go` | 交叉編譯 Linux 二進位（PowerShell：`$env:GOOS='linux'; $env:GOARCH='amd64'; go build -o bin/apm-go ./cmd/apm-go`） |
| `go run ./cmd/apm-go <args>` | 執行 apm-go CLI |
| `go test ./...` | 執行所有測試 |
| `go test ./... -cover` | 執行測試並顯示覆蓋率（目標 ≥ 80%） |
| `go test ./... -run <Name>` | 只執行符合名稱的測試 |
| `go fmt ./...` | 格式化程式碼 |
| `go vet ./...` | 靜態檢查 |

## Available skills

- context7: 當需要針對特定套件或功能查詢對新的文件時使用
- commit-message: 當需要撰寫原子化 commit message 時使用
<!-- Available_COMMANDS:START -->

<!-- TRELLIS:START -->
# Trellis Instructions

These instructions are for AI assistants working in this project.

This project is managed by Trellis. The working knowledge you need lives under `.trellis/`:

- `.trellis/workflow.md` — development phases, when to create tasks, skill routing
- `.trellis/spec/` — package- and layer-scoped coding guidelines (read before writing code in a given layer)
- `.trellis/workspace/` — per-developer journals and session traces
- `.trellis/tasks/` — active and archived tasks (PRDs, research, jsonl context)

If a Trellis command is available on your platform (e.g. `/trellis:finish-work`, `/trellis:continue`), prefer it over manual steps. Not every platform exposes every command.

If you're using Codex or another agent-capable tool, additional project-scoped helpers may live in:
- `.agents/skills/` — reusable Trellis skills
- `.codex/agents/` — optional custom subagents

Managed by Trellis. Edits outside this block are preserved; edits inside may be overwritten by a future `trellis update`.

<!-- TRELLIS:END -->

<!-- GUIDELINES:START -->
Behavioral guidelines to reduce common LLM coding mistakes. Merge with project-specific instructions as needed.

**Tradeoff:** These guidelines bias toward caution over speed. For trivial tasks, use judgment.

## 1. Think Before Coding

**Don't assume. Don't hide confusion. Surface tradeoffs.**

Before implementing:
- State your assumptions explicitly. If uncertain, ask.
- If multiple interpretations exist, present them - don't pick silently.
- If a simpler approach exists, say so. Push back when warranted.
- If something is unclear, stop. Name what's confusing. Ask.

## 2. Simplicity First

**Minimum code that solves the problem. Nothing speculative.**

- No features beyond what was asked.
- No abstractions for single-use code.
- No "flexibility" or "configurability" that wasn't requested.
- No error handling for impossible scenarios.
- If you write 200 lines and it could be 50, rewrite it.

Ask yourself: "Would a senior engineer say this is overcomplicated?" If yes, simplify.

## 3. Surgical Changes

**Touch only what you must. Clean up only your own mess.**

When editing existing code:
- Don't "improve" adjacent code, comments, or formatting.
- Don't refactor things that aren't broken.
- Match existing style, even if you'd do it differently.
- If you notice unrelated dead code, mention it - don't delete it.

When your changes create orphans:
- Remove imports/variables/functions that YOUR changes made unused.
- Don't remove pre-existing dead code unless asked.

The test: Every changed line should trace directly to the user's request.

## 4. Goal-Driven Execution

**Define success criteria. Loop until verified.**

Transform tasks into verifiable goals:
- "Add validation" → "Write tests for invalid inputs, then make them pass"
- "Fix the bug" → "Write a test that reproduces it, then make it pass"
- "Refactor X" → "Ensure tests pass before and after"

For multi-step tasks, state a brief plan:
```
1. [Step] → verify: [check]
2. [Step] → verify: [check]
3. [Step] → verify: [check]
```

Strong success criteria let you loop independently. Weak criteria ("make it work") require constant clarification.

---

**These guidelines are working if:** fewer unnecessary changes in diffs, fewer rewrites due to overcomplication, and clarifying questions come before implementation rather than after mistakes.
<!-- GUIDELINES:END -->

<!-- 專案專屬規則。位於 Trellis 管理區塊之外，`trellis update` 不會覆寫。 -->

## 5. 收斂性斷言禁令（fail-closed）

**背景**：反覆出現的「未完成 / 偷懶 / 遺漏 / 自作主張」是**同一個動作**——用一個沒有證據的終結性結論去停止工作。「延後」「架構性」「不可利用」「完成了」全是同一招：講一個結論，就不用再做了。這條規則把它 fail-closed。

> **2026-07-29 更新：絆線已從「詞表」升級為「句型」。**
> 詞表被證明會漏——`07-28-marketplace-plugin-parity` 有四個判斷被外部稽核推翻，
> 用的是「優於上游 / 純呈現層 / 功能等價 / 設計較佳 / 無缺口 / 成本大」，
> **與下面這張詞表零重疊**。完整規則與案例見
> [`.trellis/spec/guides/claim-evidence-guide.md`](.trellis/spec/guides/claim-evidence-guide.md)。
> 該守則涵蓋九種句型（比較 / 等價 / 化約 / 充分性 / 不存在 / 量級 / 時序 /
> 因果歸因 / 風險接受），下面的詞表是它的子集，保留於此作為最低標準。

> **2026-08-03 追加：「隱含基準」——句型偵測器的盲區。**
> 九種句型都在偵測**寫出來的句子**；但「拿 v0.26.0 當上游基準」這種前提
> 可以從頭到尾不落筆、只被反覆使用，因此詞表與句型都抓不到。實際案例：
> parity 比對時我發現上游工作區停在舊版、改用 `git show v0.26.0:` 繞過並記錄，
> 卻沒問「v0.26.0 還是不是現行基準」——上游其實已到 v0.27.0，差 102 檔／
> +8514 行，含我剛比對過的 `output_mappers.py`。
> 凡是「拿 A 對照 B」的工作（版本 parity / golden file / baseline 數字 /
> 對照組），基準本身就是一個 claim：**要有可重跑的版本座標、取用時當場
> 量測它與現況的落差（落差是零也要有那個「零」的輸出）、發現位移一律停下來
> 說明並取得裁定**——基準位移是範圍變更，不是實作細節；執行者可以決定怎麼做，
> 不能決定拿什麼當標準。完整要求見 `claim-evidence-guide.md` 同名小節。

**絆線詞（最低標準；完整句型規則見上方連結）**：寫下「延後 / 架構性 / 不可利用 / 不影響 / 已完成 / 完整 / 範圍外 / N/A / 其餘同理」任一個，**必須在同一處同時附上證據三件套**：

1. `file:line` — 實際讀過的程式碼路徑（不是「應該」，是讀過的位置）。
2. 威脅模型 / repro / 反例 — 誰可控、可得什麼；或重現步驟；或一個具體反例。
3. 成本估計 — 若結論是「延後 / 需大改」，估計修復規模。

只有形容詞、沒有證據 = **缺陷，不是結論**。不確定時只能寫「未驗證」，**不能寫「延後」——延後是一個 claim，不是免死的範圍決定**。

**適用範圍**：research、PRD、code review、進度回報，以及任何「我可以停手了」的判斷點。這條規則優先於「趕快收尾」的衝動。**實作階段與規劃階段一視同仁**。

**完成宣告的兩條硬規矩**（2026-07-29 新增，`claim-evidence-guide.md` 有完整說明）：

1. **未經外部驗證不得宣告完成。** 任何「完成 / 齊了 / 可以進下一階段」都必須附一個外部檢查的結果（`codex exec -s read-only` 或 fresh-context subagent）。沒有就只能寫「我做到 X，尚未驗證」。同模型自我重讀與原錯誤相關，不算驗證（理由見 `.trellis/workflow.md:304`）。
2. **完成宣告一律附可執行證據。** 不准用形容詞收尾；附上實際跑過的指令與輸出，讓對方能自己重跑推翻。

**寫下任何判斷前的反向檢查**：「如果我錯了，哪一段程式碼會證明我錯？我讀過它了嗎？」沒讀過就只能寫「未驗證」。

**偵測器**：任何人看到上述絆線詞而旁邊沒有證據三件套，即為缺陷；且它本該被 checklist 推導步驟擋下，不該靠人工抓。驗證面的對應機制（checklist 推導、絆線觸發的獨立審查、成本排序）見 `.trellis/workflow.md` 的「Verification Checklist & Convergent-Claim Tripwire」。

## 6. 四類需指名許可的動作（2026-08-03，使用者裁定）

**背景**：`07-28-marketplace-plugin-parity` 期間重複出現同一種違規——**把「對 A 的許可」當成「對 B 的許可」，或把某個必要步驟判定成「只是機械動作」而跳過**。實際案例：

- 使用者裁定「基準線抬到 v0.27.0」→ 被當成「可以建立任務」
- 使用者說「繼續，我不在電腦前」→ 被當成「可以自行派遣 subagent」
- workflow 明文要求取得建立任務同意 → 被判定成「任務檔只是記帳」而跳過
- subagent 在上游唯讀 repo 執行 git 寫入、`git checkout --` 兩次誤用（其中一次抹掉 subagent 未提交的產出）→ 同樣是「這個動作無害」的自行判定

這與 §5 是同一個病灶：做一個**沒有證據的收斂性判斷**，然後用它關掉一道關卡。差別在於 §5 關掉的是調查，這裡關掉的是**使用者的決定權**。

**規則**：執行下列任一動作前，必須有**指名該動作**的明確許可。**不得由相鄰許可推導**——對範圍的裁定不是對流程的許可，對某次操作的許可不延伸到下一次。

1. **建立 / 啟動 / 完成 Trellis 任務**（`task.py create` / `start` / `finish` / `archive`）
2. **派遣 subagent**（任何 Agent / Task 工具呼叫）
3. **對 `apm-go` repo 以外的路徑寫入**——特別包含上游參考 repo `D:/Projects/apm-dev/apm` 的**任何** git 寫入（`checkout` / `switch` / `restore` / `fetch` / `pull`）。該 repo 一律唯讀，只能 `git show <tag>:<path>`
4. **任何會丟棄未提交內容的操作**（`git checkout -- <path>` / `git restore` / `git reset --hard` / `git clean` / 覆寫未讀過的檔案）

**判斷不確定是否屬於這四類時，一律當作屬於。**

**為什麼寫成檔案而不是口頭承諾**：session 會被壓縮，對話中的行為承諾不跨過壓縮，檔案會。承諾「我會更小心」已被證明無效——需要的是移除自行裁量，不是增加警覺。

**取得許可後的義務**：把「我判定這件事已獲許可，依據是使用者的哪一句話」**寫出來**，讓使用者能當場推翻。默默認定自己有許可，等同沒有許可。
