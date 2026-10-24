---
name: apm-go-help
description: 回答 apm-go 專案的使用與開發問題：怎麼用某個指令、怎麼建置與測試、該用哪個 skill 或工具、跑壞了怎麼修、怎麼開 PR 與發版。輸入 /apm-go-help 加上問題。
disable-model-invocation: true
---

# apm-go help

回答使用者關於 apm-go 的問題，附上一段他可以直接送出的提示，並連到答案出處的檔案。這是求助問題，不要開始做事：使用者問的是怎麼做，而實作、審查、比對都很花 token，讓他自己決定何時送出提示。

訊息如果是要求做事，例如「修好 #166」，就不是求助問題。照 `AGENTS.md` 做那件事，不用這個 skill。

這個檔案把問題對應到持有答案的檔案，細節由那些檔案負責。引用前先讀過你指向的檔案，兩者不一致時以那個檔案為準。連結用 repo 內的相對路徑，使用者在自己的 checkout 打得開。

## 找出需求

從訊息與對話推斷需求。具體的情況（「哪個 skill 跑 Oracle 比對？」）直接進對應段落。仍不清楚時，問一題選擇題，只回答他選的那段：

- 使用 apm-go 的某個指令
- 建置、測試、驗證一個改動
- 挑一個 skill 或工具
- 修一次跑壞的工作
- 開 PR、合併、發版

會改變答案的狀態，先查再說，只在它改變答案時提起：

- `bin/apm-go` 或 `.scratch/verify/bin/apm-go` 比原始碼舊：先 `sh .claude/skills/verify-apm-go/scripts/verify.sh doctor`。
- `~/.cache/apm-go-verify/oracle/.venv/bin/apm` 不存在：要跟 Oracle 比對就得先重建它（見「修一次跑壞的工作」）。
- `board status` 有等使用者的卡片：問題和那張卡有關時先處理它，用 `/board`。

## 使用 apm-go 的某個指令

指令的行為、輸出文字、結束碼和寫出的檔案，以 `PRODUCT.md` 為準：先在「Capabilities and Constraints」找那個指令或行為的條目，引用條目原文。安裝方式在 [README.zh-TW.md](../../../README.zh-TW.md)。旗標以 `apm-go <cmd> --help` 為準，不要背。

和 Oracle（microsoft/apm）不同的地方，`PRODUCT.md` 條目裡寫成 `Deviation:`。Oracle 是參考不是規格：差異本身不是 bug，使用者真的碰到問題才是。

## 建置、測試、驗證一個改動

| 要做的事 | 指令或檔案 |
|---|---|
| 建置與單元測試 | `go build -o bin/apm-go ./cmd/apm-go`、`go test ./...` |
| 宣稱改動就緒前的全套檢查 | `sh .claude/skills/verify-apm-go/scripts/verify.sh check`（build、亂序測試、vet、gofmt、staticcheck、govulncheck、`tools/realexec.sh`） |
| 用使用者的角度驅動真的執行檔、留證據 | [`verify-apm-go`](../verify-apm-go/SKILL.md)，各指令的做法在它的 `features/` |
| 改函式前看影響範圍 | GitNexus：`impact`，見 [`gitnexus`](../gitnexus/) 與 `AGENTS.md`「GitNexus」 |
| 跟 Oracle 0.33.0 比一個行為 | 手動跑 `tools/parity`，或在兩個新目錄各跑一次再比；步驟在 `ARCHITECTURE.md` §5 |
| 程式該放哪一層、能不能加這條 import | `ARCHITECTURE.md` §1、§2 |

測試的規則（`TestMain` 固定 CI 變數與 git 設定、expected 值從預期行為來而不是照程式重算）在 `AGENTS.md`「Testing patterns」。寫出 Oracle 怎麼做之前，要先實際跑過 Oracle 0.33.0 或引用它的 file:line（`AGENTS.md` 強制規則 3）。

## 挑一個 skill 或工具

| 使用者想要 | skill 或工具 |
|---|---|
| 證明改動在真的執行檔上有效 | [`verify-apm-go`](../verify-apm-go/SKILL.md) |
| 讓看板上的 issue 自動經過調查、實作、審查、開 PR | `/board`（使用者全域的 skill；`board card start <N>`） |
| 看懂一段程式、追呼叫者、評估改名 | [`gitnexus`](../gitnexus/) 底下的子 skill |
| 跟 Oracle 逐案比對 | `tools/parity`（手動，不在 CI） |
| 合併後整理經驗 | `AGENTS.md`「Lessons review」，結果進 `LESSONS.md` |
| 開或改 PR | `git-assistant:pull-request` |
| 寫 commit | `commit` |
| 在 herdr 裡叫另一個 agent 審查 | `herdr`，例如 codex 跑 `$ponytail-review` |
| 找專案說明的入口 | `/apm-go-help` |

相近的選擇：

- `verify-apm-go` 證明 apm-go 自己的行為；`tools/parity` 拿它跟 Oracle 比。兩者互不取代。
- `go test` 驗證程式；`verify.sh check` 是開 PR 前的整套檢查，也是 board 的 `gate`。
- `/board` 跑完整流程，適合一張 issue 一張卡；直接請 agent 修，適合幾個小項目一起做、一次開 PR。

## 修一次跑壞的工作

| 症狀 | 處理 |
|---|---|
| 改了程式但行為沒變 | 執行檔是舊的。`verify.sh doctor`，再 `verify.sh build`。 |
| 本機綠、CI 紅（或相反） | 新套件讀了 `CI` 類環境變數或 git 設定卻沒有 `TestMain` 固定它們，見 `AGENTS.md`「Testing patterns」。 |
| 測試在本機過、CI 沒測到 | fixture 被 `.gitignore` 擋掉（`*.out`、`apm_modules/`）。commit 前跑 `git status --short --ignored`。 |
| agent 卡在權限提示不動 | 對由變數或 glob 組成的路徑跑了 `rm`。改用字面上的絕對路徑，或每次重跑寫進新目錄。 |
| 幾個 `verify.sh check` 同時跑互相弄壞 | 它們共用 `~/.cache/apm-go-verify/tmp`。各自加 `XDG_CACHE_HOME=<各自的目錄> GOCACHE=$HOME/.cache/go-build`。 |
| 找不到 Oracle | 重建：`git clone --depth 1 --branch v0.33.0 https://github.com/microsoft/apm.git ~/.cache/apm-go-verify/oracle`，再用 Python 3.12 建 `.venv` 並 `pip install -e .`。 |
| Linux 上過、Windows 上壞 | 路徑切分或 symlink 判斷。用 Windows sandbox 跑一次，junction 和 symlink 都要測（`LESSONS.md`）。 |
| 升級後舊 lock 出錯 | 用前一版和 Oracle 0.33.0 各寫一份 lock，跑 install、update、frozen install、uninstall（`LESSONS.md`）。 |
| board 卡片停在 `needs_user` | `/board`，照 `board status` 印出的原因處理。 |

## 開 PR、合併、發版

- PR：用 `git-assistant:pull-request` 開。PR 說明要逐條交代 `AGENTS.md` 的強制規則，以及每條適用的 lesson 你怎麼處理。
- 升級後使用者看得到的改動，開 PR 的同一步就要在下一版的 `Release notes: vX.Y.Z` issue 加一行（強制規則 2）。
- 合併：`gh pr merge <N> --merge --match-head-commit <審過的 head> --delete-branch -R gn00678465/apm-go`，再刪本機 branch 與 worktree。每個 `gh` 指令都加 `-R gn00678465/apm-go`。
- 發版：在 main 上打 tag 並 push，不需要改版號的 commit。順序是 `vX.Y.Z-beta.N` → `vX.Y.Z-rc.N` → `vX.Y.Z`，見 `AGENTS.md`「Release」。

## 回覆

先給答案，再給至多一段放在程式碼區塊裡的範例提示（有合適的就從 [`references/recipes.md`](references/recipes.md) 改寫），最後附出處檔案的連結。除非使用者要整張地圖，否則保持簡短。用繁體中文回答。
