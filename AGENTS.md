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

**絆線詞**：寫下「延後 / 架構性 / 不可利用 / 不影響 / 已完成 / 完整 / 範圍外 / N/A / 其餘同理」任一個，**必須在同一處同時附上證據三件套**：

1. `file:line` — 實際讀過的程式碼路徑（不是「應該」，是讀過的位置）。
2. 威脅模型 / repro / 反例 — 誰可控、可得什麼；或重現步驟；或一個具體反例。
3. 成本估計 — 若結論是「延後 / 需大改」，估計修復規模。

只有形容詞、沒有證據 = **缺陷，不是結論**。不確定時只能寫「未驗證」，**不能寫「延後」——延後是一個 claim，不是免死的範圍決定**。

**適用範圍**：research、PRD、code review、進度回報，以及任何「我可以停手了」的判斷點。這條規則優先於「趕快收尾」的衝動。

**偵測器**：任何人看到上述絆線詞而旁邊沒有證據三件套，即為缺陷；且它本該被 checklist 推導步驟擋下，不該靠人工抓。驗證面的對應機制（checklist 推導、絆線觸發的獨立審查、成本排序）見 `.trellis/workflow.md` 的「Verification Checklist & Convergent-Claim Tripwire」。
