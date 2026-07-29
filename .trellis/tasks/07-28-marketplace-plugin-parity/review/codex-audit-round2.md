# Codex 對抗性稽核報告（第二輪）

- 稽核者：codex-cli 0.144.6，`codex exec -s read-only`
- 日期：2026-07-29
- 對象：(A) 重寫後的 `checklist.md` (B) 全新的 parent/child 拆分 (C) `claim-evidence-guide.md`
- **環境降級**：Windows sandbox helper 缺失（`codex-windows-sandbox-setup.exe: program not found`），
  shell 無法啟動；改用 node_repl 讀檔與文字分析。**因此本輪沒有實跑 `go build`/`go test`**，
  稽核者自述會把「文件靜態驗證」與「實際命令未跑」嚴格分開。

---

## 上一輪發現的修復驗證
<逐條：真的修好 / 只是換句話說 / 修出新問題>

## 阻斷級（實作前必須解決）
1. <缺陷> · 位置：<file:line> · 為什麼是錯的：<證據，含你實際讀過的 file:line> · 建議

## 重大 / 次要
...

## 我查過但站得住的
```

找不到問題就明說找不到，不要湊數。但說「找不到」之前，請確認你真的打開檔案看過。

warning: Skill descriptions were shortened to fit the 2% skills context budget. Codex can still see every skill, but some descriptions are shorter. Disable unused skills or plugins to leave more room for the rest.
hook: SessionStart
hook: SessionStart
hook: SessionStart
hook: SessionStart Completed
hook: SessionStart Completed
hook: SessionStart Completed
hook: UserPromptSubmit
hook: UserPromptSubmit
hook: UserPromptSubmit Completed
hook: UserPromptSubmit Completed
codex
我會先依 Trellis 的規劃流程讀取稽核規則與上一輪報告；Ponytail 只約束我避免把稽核擴張成改稿。這一輪全程唯讀、inline，不派子代理。接著我會把 A/B/C 三部分各自做成可重跑的驗證，最後再用獨立的唯讀 `codex exec` 反查結論。
exec
"C:\\Program Files\\PowerShell\\7\\pwsh.exe" -Command "Get-Content -Raw 'C:\\Users\\gn006\\.agents\\skills\\ponytail\\SKILL.md'; Get-Content -Raw 'D:\\Projects\\apm-dev\\apm-go\\.agents\\skills\\trellis-brainstorm\\SKILL.md'" in D:\Projects\apm-dev\apm-go
2026-07-28T22:19:35.862088Z ERROR codex_core::exec: exec error: windows sandbox: orchestrator_helper_launch_failed: setup refresh failed to launch helper: helper=codex-windows-sandbox-setup.exe, cwd=D:\Projects\apm-dev\apm-go, log=C:\Users\gn006\.codex\.sandbox\sandbox.2026-07-28.log, error=program not found
2026-07-28T22:19:35.862384Z ERROR codex_core::tools::router: error=execution error: Io(Custom { kind: Other, error: "windows sandbox: orchestrator_helper_launch_failed: setup refresh failed to launch helper: helper=codex-windows-sandbox-setup.exe, cwd=D:\\Projects\\apm-dev\\apm-go, log=C:\\Users\\gn006\\.codex\\.sandbox\\sandbox.2026-07-28.log, error=program not found" })
 exited -1 in 0ms:
execution error: Io(Custom { kind: Other, error: "windows sandbox: orchestrator_helper_launch_failed: setup refresh failed to launch helper: helper=codex-windows-sandbox-setup.exe, cwd=D:\\Projects\\apm-dev\\apm-go, log=C:\\Users\\gn006\\.codex\\.sandbox\\sandbox.2026-07-28.log, error=program not found" })
codex
本機唯讀 shell 因 Windows sandbox helper 缺失而無法啟動（不是專案指令失敗）。我會改用同一工作區的 Node 唯讀介面讀檔與執行純文字分析；若 Go/grep 指令也因此不可執行，我會把「文件靜態驗證」與「實際命令未跑」嚴格分開，不拿前者冒充後者。
mcp: node_repl/js started
mcp: node_repl/js (failed)
mcp: node_repl/js started
mcp: node_repl/js (completed)
唯讀對抗性稽核；先建立可重跑證據，再對每個結論做反例檢查。
  → 讀上一輪報告、claim-evidence 守則與 workflow 指定段落
  • 逐條驗證 checklist 五項修正、覆蓋率矩陣與 tripwire 命中
  • 重算 parent/child AC 分配、依賴與獨立驗收性
  • 攻擊七種句型分類並產生反例
  • 用獨立唯讀 Codex 檢查主要結論並整理稽核報告
mcp: node_repl/js started
mcp: node_repl/js (completed)
mcp: node_repl/js started
mcp: node_repl/js (completed)
mcp: node_repl/js started
mcp: node_repl/js (completed)
mcp: node_repl/js started
mcp: node_repl/js (completed)
codex
第一輪靜態閱讀已抓到一個可重現的驗法問題：文件把 repo 外的 `%TEMP%` 當成仍可用 `..\bin\apm-go.exe` 回到 repo，但這個相對路徑通常會落在 `%TEMP%\..\bin`，不是 `D:\Projects\apm-dev\apm-go\bin`。我會先不定級，等把所有引用這個慣例的 AC 與 child 獨立驗收鏈一起算完。下一步正在重跑 T1/T2 的實際命中與分類。
mcp: node_repl/js started
mcp: node_repl/js (completed)
mcp: node_repl/js started
mcp: node_repl/js (completed)
mcp: node_repl/js started
mcp: node_repl/js (completed)
mcp: node_repl/js started
mcp: node_repl/js (completed)
mcp: node_repl/js started
mcp: node_repl/js (completed)
mcp: node_repl/js started
mcp: node_repl/js (completed)
mcp: node_repl/js started
mcp: node_repl/js (completed)
mcp: node_repl/js started
mcp: node_repl/js (completed)
mcp: node_repl/js started
mcp: node_repl/js (completed)
codex
AC 數量重算與 parent 宣稱一致：parent 49、children 48，只有 AC51 未分配，沒有重複或額外編號。但「數量正確」不等於「分配正確」：我已確認 AC29 明文對應 R1.4/C4，卻被放進只承接 R3/R4 的 `plugin-init`；此外 parent 的 implement 計畫明寫 Step 3 必須先於 Step 5，因 AC2/AC3 依賴 UX seam，但 Step 3 被放在 `plugin-init`，而 `targets-init-shape` 仍宣稱無前置。這會形成第一個實質的獨立驗收破口。
mcp: codebase-memory-mcp/list_projects started
mcp: codebase-memory-mcp/list_projects (failed)
user cancelled MCP tool call
mcp: codebase-memory-mcp/index_status started
mcp: codebase-memory-mcp/index_status (failed)
user cancelled MCP tool call
mcp: node_repl/js started
mcp: node_repl/js (completed)
mcp: node_repl/js started
mcp: node_repl/js (completed)
mcp: node_repl/js started
mcp: node_repl/js (completed)
mcp: node_repl/js started
mcp: node_repl/js (failed)
mcp: node_repl/js started
mcp: node_repl/js (completed)
mcp: node_repl/js started
mcp: node_repl/js (completed)
mcp: node_repl/js started
mcp: node_repl/js (completed)
mcp: node_repl/js started
mcp: node_repl/js (completed)
mcp: node_repl/js started
mcp: node_repl/js (completed)
mcp: node_repl/js started
mcp: node_repl/js (completed)
mcp: node_repl/js started
mcp: node_repl/js (completed)
mcp: node_repl/js started
mcp: node_repl/js (completed)
已讀上一輪報告、守則與 workflow；已重跑 AC 編號與 T1/T2，正在逐格重做覆蓋率判斷。
  ✓ 讀上一輪報告、claim-evidence 守則與 workflow 指定段落
  → 逐條驗證 checklist 五項修正、覆蓋率矩陣與 tripwire 命中
  • 重算 parent/child AC 分配、依賴與獨立驗收性
  • 攻擊七種句型分類並產生反例
  • 用獨立唯讀 Codex 檢查主要結論並整理稽核報告
mcp: node_repl/js started
mcp: node_repl/js (completed)
mcp: node_repl/js started
mcp: node_repl/js (completed)
codex
目前覆蓋率矩陣不是只有措辭問題：至少有可構造的錯誤實作仍會全綠。例如 R5.5 要求 local source 在「上述所有情境」都不觸網，但 AC21 只跑 local+零旗標，AC40 也只是六個單點分支；「local + 顯式 HEAD」若誤觸網，現有矩陣仍抓不到。R10.4 自己標成「部分足以否證」後，又用「既有行為、沒有改動面」跳過專屬閘門，但 R10 會改 `AddPackage`，claude-only add 本來就在變更面內。現在我會讓 fresh-context 的唯讀 Codex 專門反駁這些候選缺陷，避免把我自己的推論當證據。
mcp: node_repl/js started
mcp: node_repl/js (failed)
tool call error: tool call failed for `node_repl/js`

Caused by:
    timed out awaiting tools/call after 300s
codex
獨立 Codex 在 5 分鐘工具上限內沒有回傳；這只代表外部檢查超時，不能拿來當通過或失敗。我會檢查該行程是否已結束；若沒有可回收輸出，依專案規則明確標成「外部複驗未取得結果」，不假裝自己重讀等同獨立驗證。文件與可執行的 `rg`/路徑解析證據仍會照常列出。
mcp: node_repl/js started
mcp: node_repl/js (completed)
mcp: node_repl/js started
mcp: node_repl/js (failed)
mcp: node_repl/js started
mcp: node_repl/js (completed)
mcp: node_repl/js started
mcp: node_repl/js (completed)
mcp: node_repl/js started
mcp: node_repl/js (completed)
mcp: node_repl/js started
mcp: node_repl/js (completed)
A/B/C 的唯讀稽核與可重跑證據已整理。外部 `codex exec -s read-only` 兩次嘗試皆因本機存取限制/超時未取得審查結果，將在報告中明示，不能算外部驗證。
  ✓ 讀上一輪報告、claim-evidence 守則與 workflow 指定段落
  ✓ 逐條驗證 checklist 五項修正、覆蓋率矩陣與 tripwire 命中
  ✓ 重算 parent/child AC 分配、依賴與獨立驗收性
  ✓ 攻擊七種句型分類並產生反例
  ✓ 用獨立唯讀 Codex 檢查主要結論並整理稽核報告
codex
## 上一輪發現的修復驗證

| 上一輪發現 | 判定 | 證據 |
|---|---|---|
| D5 錯估 `install --dev` 成本 | 需求已改正 | R9 已納入 parent（[prd.md:174](/D:/Projects/apm-dev/apm-go/.trellis/tasks/07-28-marketplace-plugin-parity/prd.md:174)），並建立 `install-dev` child（[prd.md:40](/D:/Projects/apm-dev/apm-go/.trellis/tasks/07-29-install-dev/prd.md:40)）。現行 dev 讀取鏈也確實存在於 [install.go:433](/D:/Projects/apm-dev/apm-go/cmd/apm-go/install.go:433) 等位置。 |
| D6/category 死結 | 功能規劃已改正，但驗收仍有缺口 | R10 加入 `--category` 與 AC47–50（[parent prd.md:190](/D:/Projects/apm-dev/apm-go/.trellis/tasks/07-28-marketplace-plugin-parity/prd.md:190)）；但 R10.4 只驗 codex 情境，見阻斷 2。 |
| R→AC 覆蓋表錯判 | 未修好 | 新表不再只寫「完整覆蓋」，但仍有多個錯誤實作可讓所有對應 AC 通過，見阻斷 2。 |
| 多條驗法假綠 | 部分修好，fixture 修出新問題 | `go run`、零匹配 `-run`、錯 package、coverprofile total、`t.Skip` 均已改；但 repo 外 fixture 的 binary 路徑不可執行，C5 working-tree 檢查也仍不足。 |
| T1/T2 結論錯誤 | T1 修好，T2 未修好 | 實跑 `rg`：T1 是 11 個匹配行、12 次詞彙命中，JSONL 0；與文件的「11 處」一致。T2 是 45 個匹配行、62 次命中，但文件沒有報數字或逐處分類。 |

其他上一輪項目：C2/C3 的宣稱已收窄、X5 已承認 PTY 覆蓋缺口、T3 已納入 JSONL、AC7/AC13/AC26 已改成較強驗法。X6 的 `file:line` 問題仍在，見重大 2。

## 阻斷級（實作前必須解決）

1. repo 外 fixture 全部指向錯誤的 binary · 位置：[checklist.md:39](/D:/Projects/apm-dev/apm-go/.trellis/tasks/07-28-marketplace-plugin-parity/checklist.md:39)、[checklist.md:44](/D:/Projects/apm-dev/apm-go/.trellis/tasks/07-28-marketplace-plugin-parity/checklist.md:44)、[checklist.md:155](/D:/Projects/apm-dev/apm-go/.trellis/tasks/07-28-marketplace-plugin-parity/checklist.md:155)、[checklist.md:286](/D:/Projects/apm-dev/apm-go/.trellis/tasks/07-28-marketplace-plugin-parity/checklist.md:286) · 為什麼是錯的：實際環境中 repo binary 是 `D:\Projects\apm-dev\apm-go\bin\apm-go.exe`；從 `%TEMP%\apm-go-check-ac18` 解析 `..\bin` 得到 `C:\Users\gn006\AppData\Local\Temp\bin\apm-go.exe`，`..\..\bin` 得到 `C:\Users\gn006\AppData\Local\bin\apm-go.exe`，兩者皆不存在。AC1、4、7、9、12–15、18、22、24、27、29–34、37、46、50 等 CLI 閘門因此照抄即失敗 · 建議：build 後先解析並保存 repo binary 的絕對路徑，所有 `Push-Location` 後的命令只用該絕對路徑。

2. 覆蓋率表仍把「部分驗到」寫成「足以否證」 · 位置：[checklist.md:650](/D:/Projects/apm-dev/apm-go/.trellis/tasks/07-28-marketplace-plugin-parity/checklist.md:650)–[691](/D:/Projects/apm-dev/apm-go/.trellis/tasks/07-28-marketplace-plugin-parity/checklist.md:691) · 為什麼是錯的：

   - R1.4 要保留 `target`、`targets` 雙鍵解析（[manifest.go:119](/D:/Projects/apm-dev/apm-go/internal/manifest/manifest.go:119)、[manifest.go:125](/D:/Projects/apm-dev/apm-go/internal/manifest/manifest.go:125)），AC29 只端對端測單數鍵。破壞複數部署解析仍可通過。
   - R3.3.a 指定精確 regex，但 AC9/AC36 沒測尾端 `-`、連續 `--`、第二字元大寫等。錯誤地拒絕合法 `abc-` 的 validator 仍可通過（[checklist.md:308](/D:/Projects/apm-dev/apm-go/.trellis/tasks/07-28-marketplace-plugin-parity/checklist.md:308)）。
   - R4.2/R4.3：AC15 只測 `skills + .apm`；AC39 只測 marker helper 回傳非空。其他 marker 在有 `.apm` 時仍警告，或對其他 marker 改成非零 exit，都不會被抓到（[checklist.md:159](/D:/Projects/apm-dev/apm-go/.trellis/tasks/07-28-marketplace-plugin-parity/checklist.md:159)、[checklist.md:329](/D:/Projects/apm-dev/apm-go/.trellis/tasks/07-28-marketplace-plugin-parity/checklist.md:329)）。
   - R5.2 要涵蓋顯式及隱含 HEAD + `--no-verify`；AC18 只測隱含情境。R5.5 要 local source 在「上述所有情境」皆不觸網，但 AC21/AC40 沒測 local × 各旗標的組合（[parent prd.md:136](/D:/Projects/apm-dev/apm-go/.trellis/tasks/07-28-marketplace-plugin-parity/prd.md:136)、[checklist.md:206](/D:/Projects/apm-dev/apm-go/.trellis/tasks/07-28-marketplace-plugin-parity/checklist.md:206)）。
   - R8.3 要四者同源；集合相等測試仍允許四份獨立字面量。R8.4 要 `CanonicalTargets` 不變，但 AC27 只測 `cursor`，其餘值可被刪除（現況集合見 [target.go:5](/D:/Projects/apm-dev/apm-go/internal/manifest/target.go:5)）。
   - R9.4 的 AC45 明寫 `package_type` 值「待實作後確認」（[checklist.md:380](/D:/Projects/apm-dev/apm-go/.trellis/tasks/07-28-marketplace-plugin-parity/checklist.md:380)）。`package_type: banana` 也會通過；parse/write/equality 亦未驗。現況型別、parser、semantic equality 均沒有該欄位（[types.go:4](/D:/Projects/apm-dev/apm-go/internal/lockfile/types.go:4)、[parse.go:175](/D:/Projects/apm-dev/apm-go/internal/lockfile/parse.go:175)、[write.go:330](/D:/Projects/apm-dev/apm-go/internal/lockfile/write.go:330)）。
   - R10.4 那格自己承認「部分足以否證」，卻沒有產生 G check（[checklist.md:691](/D:/Projects/apm-dev/apm-go/.trellis/tasks/07-28-marketplace-plugin-parity/checklist.md:691)）。R10 會修改 `AddPackage`，所以「claude-only 零改動面」不成立；錯誤的 outputs/category 條件可破壞 claude-only add 而 AC50 仍通過。

   · 建議：每個反例增加一條可獨立轉紅的 AC/G check；不能把已承認「部分」的列算入其餘 34 格。

3. child 拆分形成未宣告的 UX seam 依賴，AC29 也分錯 child · 位置：[targets child prd.md:32](/D:/Projects/apm-dev/apm-go/.trellis/tasks/07-29-targets-init-shape/prd.md:32)、[plugin child prd.md:52](/D:/Projects/apm-dev/apm-go/.trellis/tasks/07-29-plugin-init/prd.md:52)、[parent implement.md:80](/D:/Projects/apm-dev/apm-go/.trellis/tasks/07-28-marketplace-plugin-parity/implement.md:80)、[parent implement.md:118](/D:/Projects/apm-dev/apm-go/.trellis/tasks/07-28-marketplace-plugin-parity/implement.md:118) · 為什麼是錯的：

   - AC29 明文對應 R1.4/C4（[parent prd.md:339](/D:/Projects/apm-dev/apm-go/.trellis/tasks/07-28-marketplace-plugin-parity/prd.md:339)），應屬承接 R1 的 `targets-init-shape`，現在卻被放入只承接 R3/R4 的 `plugin-init`。
   - parent implement 明寫 Step 3 必須先於 Step 5，否則 AC2/AC3 無法斷言。現行 `confirmWith`、`multiSelectWith`、`inputFormWith` 確實仍是不可替換的普通 func（[interactive.go:84](/D:/Projects/apm-dev/apm-go/internal/ux/interactive.go:84)、[interactive.go:149](/D:/Projects/apm-dev/apm-go/internal/ux/interactive.go:149)、[interactive.go:195](/D:/Projects/apm-dev/apm-go/internal/ux/interactive.go:195)）。
   - `targets-init-shape` 的執行步驟沒有 Step 3；Step 3 被 `plugin-init` 承接，而 `plugin-init` 又宣告依賴 `targets-init-shape`。因此前者只做自身列出的改動後，AC2/AC3 無法驗。

   · 建議：把 Step 3 seam 與 AC29 移到 `targets-init-shape`，或建立先於兩者的共享 prerequisite；不可讓 targets 反向依賴 plugin。

4. parent C5 在三個 child 的獨立閘門中消失 · 位置：[parent prd.md:235](/D:/Projects/apm-dev/apm-go/.trellis/tasks/07-28-marketplace-plugin-parity/prd.md:235)、[targets child prd.md:47](/D:/Projects/apm-dev/apm-go/.trellis/tasks/07-29-targets-init-shape/prd.md:47)、[marketplace child prd.md:75](/D:/Projects/apm-dev/apm-go/.trellis/tasks/07-29-marketplace-add-fixes/prd.md:75)、[install child prd.md:62](/D:/Projects/apm-dev/apm-go/.trellis/tasks/07-29-install-dev/prd.md:62) · 為什麼是錯的：三個 child 的本地閘門只有 build/vet/coverage；新增第三方 dependency 後這些仍可通過。只有 `plugin-init` 的 AC23 明確檢查 `go.mod`。因此 child 被驗收後仍可能違反 parent C5 · 建議：把 C5 複製為每個 child 的本地 constraint/check。

5. Tripwire 的 T2 仍是未執行證據的宣稱 · 位置：[checklist.md:853](/D:/Projects/apm-dev/apm-go/.trellis/tasks/07-28-marketplace-plugin-parity/checklist.md:853)–[915](/D:/Projects/apm-dev/apm-go/.trellis/tasks/07-28-marketplace-plugin-parity/checklist.md:915) · 為什麼是錯的：相同 regex 實跑得到 T1 11 個匹配行、T2 45 個匹配行；T1 列出 11 行，T2 卻沒有實際數字或逐行分類，line 914 仍稱 T2 已執行並記錄實際命中數字。另如 `prd.md:276` 的「兩條合起來才是完整斷言」是充分性 claim，T1 只以「具體、可否證」豁免；但 claim guide [line 42](/D:/Projects/apm-dev/apm-go/.trellis/spec/guides/claim-evidence-guide.md:42) 與 [line 108](/D:/Projects/apm-dev/apm-go/.trellis/spec/guides/claim-evidence-guide.md:108) 沒有這個豁免 · 建議：T2 報出 45 行並逐處分類；所有真斷言按新句型規則附證據，不能只列四種概括性 meta 類別。

## 重大 / 次要

### 重大

1. X6 仍未滿足 `file:line` 硬規則 · 位置：[checklist.md:788](/D:/Projects/apm-dev/apm-go/.trellis/tasks/07-28-marketplace-plugin-parity/checklist.md:788) · 證據：它用 `grep ...` 零命中取代實際 code path，並自行宣稱「這本身就是第一件證據」。但守則明文要求實際讀過的 `file:line`（[claim-evidence-guide.md:51](/D:/Projects/apm-dev/apm-go/.trellis/spec/guides/claim-evidence-guide.md:51)）。若程式沒有 `studio` 符號，仍可引用實際讀過的命令註冊/能力分派路徑來證明沒有承接點；零 grep 不是 `file:line`。

2. C5 的 working-tree 修正仍不能判斷新增了什麼 · 位置：[checklist.md:620](/D:/Projects/apm-dev/apm-go/.trellis/tasks/07-28-marketplace-plugin-parity/checklist.md:620) · 證據：`git status --porcelain go.mod go.sum` 只輸出 `M go.mod` 等狀態，不輸出 `require` 新增行；所以 line 626 的通過條件無法由所列兩個命令計算。需再看 `git diff -- go.mod` 與 `git diff --cached -- go.mod`。

3. 七類句型確實有漏：**因果歸因／風險接受** · 位置：[claim-evidence-guide.md:35](/D:/Projects/apm-dev/apm-go/.trellis/spec/guides/claim-evidence-guide.md:35)–[45](/D:/Projects/apm-dev/apm-go/.trellis/spec/guides/claim-evidence-guide.md:45) · 反例：「這次資料遺失的根因是 DNS 抖動。」這是可讓調查停止的程式行為歸因，但不是比較、等價、化約、充分性、不存在、量級或時序句型。若歸因錯誤，真正的持久化 bug 會被放過。建議新增「因果／責任歸因」類；「此競態風險可接受」則是另一個未涵蓋的風險接受句型。

### 次要

1. `targets-init-shape` 宣稱自己是四個 child 中「唯一沒有前置」者，但 parent 表同時把 `install-dev`、`marketplace-add-fixes` 列為無前置 · 位置：[targets child prd.md:14](/D:/Projects/apm-dev/apm-go/.trellis/tasks/07-29-targets-init-shape/prd.md:14)、[parent prd.md:10](/D:/Projects/apm-dev/apm-go/.trellis/tasks/07-28-marketplace-plugin-parity/prd.md:10)–[13](/D:/Projects/apm-dev/apm-go/.trellis/tasks/07-28-marketplace-plugin-parity/prd.md:13)。

2. Windows child 文件仍使用 `tail -1` · 位置：[targets child prd.md:49](/D:/Projects/apm-dev/apm-go/.trellis/tasks/07-29-targets-init-shape/prd.md:49)。parent checklist 已改成 PowerShell 的 `Select-Object -Last 1`；child 的明列指令在純 PowerShell 環境不保證可用。


---

## 主對話的獨立複驗與處置（2026-07-29）

| Codex 發現 | 我的複驗 | 判定 | 處置 |
|---|---|---|---|
| 阻斷 3：child 拆分有未宣告的 ux seam 依賴，AC29 分錯 child | `grep -l "AC2 —" 07-29-*/prd.md` → AC2/AC3 在 `targets-init-shape`；`grep -n "Step 3" 07-29-*/prd.md` → seam 被指派給 `plugin-init`；`design.md:268` 是我自己寫的「沒有這個 seam 就無法斷言」 | **成立，且是循環**：targets 需要 plugin-init 的 seam / plugin-init 宣告依賴 targets | seam 移交 `targets-init-shape`（AC-L0）；AC29 移交 `targets-init-shape`；`plugin-init` 改為 seam 的消費者 |
| 阻斷 4：parent C5（不新增相依）在三個 child 的閘門中消失 | 三個 child 的本地閘門原本只有 build/vet/coverage | **成立** | 四個 child 各加 AC-L9，驗法用 `git diff -- go.mod` 而非 `git status --porcelain` |
| 重大 3：七類句型漏了因果歸因／風險接受 | 反例「這次資料遺失的根因是 DNS 抖動」確實不落在原七類，且能讓調查停止 | **成立** | `claim-evidence-guide.md` 補兩類 + 各自的證據要求；`guides/index.md` 觸發清單同步 |
| 次要 1：`targets-init-shape` 自稱「唯一沒有前置」與 parent 表矛盾 | parent 表把 `install-dev`、`marketplace-add-fixes` 也列為無前置 | **成立** | 改為「四者可並行，本 task 不是唯一」 |
| 次要 2：child 文件用 `tail -1`，Windows 主環境是 PowerShell | — | **成立** | 全部改 `Select-Object -Last 1` |
| 阻斷 1：repo 外 fixture 的相對 binary 路徑會解析錯 | `%TEMP%\x` 的 `..\bin` → `%TEMP%\bin`，不是 repo 的 bin | **成立** | 見下方「尚未處置」 |
| 阻斷 2：覆蓋率表仍把「部分驗到」寫成「足以否證」 | 未逐格複驗 | **未驗證** | 見下方「尚未處置」 |
| 阻斷 5：T2 仍是未執行證據的宣稱（實跑 T1 11 行 / T2 45 行） | 未複跑 | **未驗證** | 見下方「尚未處置」 |
| 重大 1：X6 用零命中的 grep 取代 `file:line` | — | 未複驗 | 見下方「尚未處置」 |
| 重大 2：C5 的 working-tree 檢查看不出新增了什麼 | 確認 `git status --porcelain` 只輸出 `M go.mod` | **成立** | 已併入 AC-L9 的驗法 |

### 尚未處置（阻斷 1、2、5 與重大 1）

這四項都在 `checklist.md` 內部，屬於該檔的重新推導範圍，**尚未修**。
它們阻擋 `requirements-trace.md` 收尾閘門的 G1。

**本輪環境降級的影響**：codex 沒能實跑 `go build`/`go test`，
所以「AC 驗法是否真的可執行」這一軸的稽核強度低於第一輪。
阻斷 1（相對路徑）是靠靜態路徑推算得到的，我複算後成立；
但其餘 CLI 閘門是否可執行，本輪**沒有實測佐證**。
