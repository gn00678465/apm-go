# Research: 實跑 A/B 輸出對照全掃描（apm-go vs Python apm）

- **Query**: 實際執行 apm-go 與 Python apm 兩個二進位（非讀原始碼），對 `test1`/`demo`/`antigravity`/`bundle-demo`/`design` 五個 fixture 跑 `install`/`uninstall`/mcp 流程，逐一比較 stdout/stderr，盤點差異並分類。取代人工逐則貼對照。
- **Scope**: dynamic（實跑二進位，非唯讀原始碼）；唯讀 production code，只在 temp 副本操作，未 commit。
- **Date**: 2026-07-15
- **對照文件**: `.trellis/tasks/07-14-init-tui-beautify/research/output-parity-audit.md`（R7–R16 靜態稽核，本文件的實跑版本，用來驗證/補完）

## 方法與環境

- apm-go：`/d/Projects/apm-dev/apm-go/bin/apm-go.exe`
- Python apm：`uv --project /d/Projects/apm-dev/apm run apm <args>`（版本 0.21.0，執行時有 "new version 0.25.0 available" 提示，不影響本次比較）
- 每個 fixture 複製「乾淨輸入」到 `scratchpad/ab-sweep/<fixture>/{go,py}`：
  - `test1`：`apm.yml` + `.apm/`（local 來源）+ `.claude-plugin/plugin.json`（確認後兩者是**來源**非部署產物，`.claude/`/`.codex/`/`.github/` 才是部署輸出，未複製）
  - `demo`/`antigravity`/`bundle-demo`/`design`：僅 `apm.yml`（其餘 `.agents/`/`.claude/`/`.codex/`/`.github/`/`.mcp.json`/`opencode.json` 皆確認為既有部署產物，非乾淨輸入，未複製）
- 網路可用（`git ls-remote` 驗證成功），5 個 fixture 全數完成，**無 SKIP**。
- 兩個環境細節在動手比對前先排除，避免誤判：
  1. Python 端有殘留全域設定 `~/.apm/config.json`（`default_client: vscode`）；用隔離 `HOME`/`USERPROFILE` 重跑確認**結果不變**——不是機器污染，是可重現的程式邏輯（見下方 F1）。
  2. bundle-demo 的補充測試在深層 scratchpad 路徑下 apm-go 曾因 Windows 路徑長度撞見 `git` pack `.keep` 檔名過長而失敗；換成短路徑 `C:\t\bdgo` 後**成功**，證明是本次稽核用深層暫存路徑造成的環境假象，非程式 bug，已於下方以附註排除。

## 逐指令對照總表

| fixture/指令 | apm-go 輸出（摘要） | apm 輸出（摘要） | 差異 | 分類 | 對應 R / 新發現 |
|---|---|---|---|---|---|
| test1 install | `ℹ No dependencies to install` / `ℹ Targets: copilot, claude, codex` / `(local)` 樹（5 行，逐檔列） / `✓ Installed 0 dependencies` | `[>] Installing…` / `[i] Targets: claude, codex, copilot` / `[+] <project root> (local)` 樹（2 行，聚合：3 agents→3 targets、2 instructions→2 dirs） / `[i] Added apm_modules/ to .gitignore` / `[*] Installed 1 APM dependency in 0.1s.` | 符號集不同；target 列出順序不同（go 依 apm.yml 原順序，py 似固定 canonical 順序）；go 樹逐檔不聚合 vs py 聚合；go 未印耗時；go 未寫/未提及 `.gitignore`；**go 說「0 dependencies」但實際部署了 5 個 local 檔案，py 說「1 dependency」** | 甲(符號/耗時)、乙(樹聚合=R10b、target 順序)、丙(local=0 dep 矛盾) | R10b(樹聚合)、**R16 本體**（空 apm.yml + local 部署矛盾，完全命中題目描述）；新發現 F5(.gitignore 未建立/未提及) |
| demo install | `ℹ No dependencies to install` / `ℹ Targets: copilot, claude, opencode, codex` / `✓ Installed 0 dependencies`（**完全沒有 MCP 相關輸出**，但實際上把 4 個 target 的 MCP config 全部部署成功，lockfile 有 `mcp_servers: [github]`） | 在乾淨 HOME 下：`+- MCP Servers (1)` → `[x] Skipping all MCP config writes -- apm.yml 'targets' field is invalid.` `[x] Unknown target 'vscode'`（清單/修法指引）→ `[i] No changes... in 0.1s.`（**MCP 完全沒部署，0 檔案**） | apm-go 静默成功部署 4 個 target 的 MCP 檔案但 stdout 完全不提；Python 因本機偵測不到已安裝的 copilot/codex runtime 而 fallback 成 `["vscode"]`，該 token 未過 apm.yml 宣告的 target 白名單驗證，整個 MCP 寫入被 gate 掉、印一大段錯誤，且**該錯誤與 fixture 本身完全無關**（apm.yml 宣告的 4 個 target 全部合法） | 丙（apm-go 端=R13 本體：presentation-only 缺口；py 端=環境相依真實邏輯 bug） | **R13 本體**（apm-go install 主流程缺 MCP 摘要，完全命中）；**新發現 F1**（Python `install --mcp` runtime auto-detect fallback "vscode" 誤觸發 targets 白名單，導致合法 target 專案的 MCP 部署整個失敗，且訊息與使用者的 apm.yml 毫無關聯，極具誤導性） |
| demo uninstall `github`（apm.yml 靜態宣告的 mcp dep） | `✓ Removed 1 package(s)` / `✓ apm_modules: removed 0 directories`；apm.yml 的 `dependencies.mcp` 清空；`.mcp.json` 清成 `{"mcpServers": {}}` | `[x] Invalid package format: github. Use 'owner/repo' or 'plugin-name@marketplace' format.` / `[!] No packages found in apm.yml to remove`；apm.yml **完全不變** | Python 的 `uninstall` 只認得 `owner/repo` 格式，無法移除裸名的 `dependencies.mcp` 項目；apm-go 可以 | 丙（apm-go 增強／deviation） | 已知既有結論（`evals/ab_uninstall.py` docstring 明列的 "un-019" deviation），**非新發現**，本次用 apm.yml 靜態宣告的 mcp dep（而非 `install --mcp` 動態加的）再次驗證同一結論成立 |
| antigravity install | `ℹ Installing…` / `ℹ Resolving mattpocock/skills…` / `✓ Resolved 1 dependency` / `ℹ Targets: antigravity` / 2 條 `!` MCP 警告（`io.github.github/github-mcp-server` 缺 header、`API_TOKEN` undefined）/ **45 行**逐子目錄樹 / `✓ Installed 1 dependency` / `•mattpocock/skills (depth 1)`（**無 hash**） | `[x] Unknown target 'antigravity'`（清單/修法指引）→ `[!] Install interrupted after 3.1s.`，**exit code 2**，完全未進入 resolve/deploy | Python **完全不認識 `antigravity` 這個 target**（`CANONICAL_TARGETS` 沒有它），整個 fixture 在 Python 端無法執行任何實質流程 | 丙（apm-go-only 功能，非 bug——已知既有能力邊界） | 45 行樹=**R10b 實例**（大規模驗證）；`(depth 1)` 無 hash=**R10a 實例**；**新發現 F2**：`antigravity` target 是 apm-go 專屬擴充，Python 完全不支援，此 fixture 上**沒有內容 parity 可言**，只能單邊（apm-go）驗證輸出品質 |
| antigravity uninstall `mattpocock/skills` | dry-run：逐項列出 `•mattpocock/skills (dependencies.apm)` + `•apm_modules/mattpocock/skills: exists`；正式執行：`✓ Removed 1 package(s)` + `✓ apm_modules: removed 1 directory`（**無套件名**） | 無法比較（Python 從未成功安裝過這個 fixture，見上） | dry-run 詳細 vs 正式執行只剩數字 | 乙 | **R7 本體**（大規模驗證，非新發現）；py 側 SKIP，理由=F2 |
| bundle-demo install（as-authored，無 `target:` 欄位） | `Error: no deployment target detected; pass --target <name> or add a target: to apm.yml` + **完整 Cobra usage/flags 全部印出**（14 行 flag 說明），exit 2 | `[x] No harness detected` + 詳列掃描過的 harness marker 清單（`.claude/`、`.cursor/`…14 種）+ 具體修法建議（3 個選項）+ `Or declare in apm.yml: targets: - claude`，exit 2 | 兩邊都正確 fail-closed（無 target 不亂猜），**行為 parity**，但**錯誤訊息品質差距極大**：py 給結構化診斷（掃了什麼、怎麼修），go 只給一句話 + 一整包跟這個錯誤無關的 flag 用法 dump | 乙（訊息內容缺 R7/R11 同型：資料明明有〔go 明明知道自己掃過哪些目錄〕但沒印） | **新發現 F3**：`install` 無 target 時的錯誤訊息，apm-go 應仿照 Python 印出「掃描過哪些 harness marker / 具體修法」而非讓 Cobra 預設把全部 flag 說明砸出來——這是使用者第一次設定失敗時看到的畫面，能見度高 |
| bundle-demo（補充：加 `--target claude`，未動 fixture 檔案）→ 兩個同名 repo 各自宣告同名 skill `karpathy-guidelines` | `!` 警告：`skills "karpathy-guidelines" from dependency:multica-ai/... shadowed by dependency:forrestchang/... (first-declared wins)` + `! multica-ai/andrej-karpathy-skills deployed 0 files to any target`；磁碟上只有 forrestchang 一份，lockfile 正確歸屬單一 owner；之後 uninstall 被 shadow 的一方不影響倖存部署 | **完全沒有警告**：兩個 `[+] ... 1 skill(s) integrated -> .claude/skills/` 都印成功；磁碟上只剩一份 `SKILL.md`（後裝的靜默覆蓋前者）；**`apm.lock.yaml` 把同一個檔案路徑 + 同一個 content_hash 同時記在兩個不同 dependency 底下**（雙重歸屬，帳目不實） | apm-go 偵測衝突、明確警告、正確歸屬單一 owner；Python 靜默覆蓋、雙重宣稱成功、lockfile 內部不一致 | **丙（Python 端實質 bug，非 apm-go 缺口）** | **新發現 F4，本次稽核最高優先級發現**：與題目提示的「BUG-1 大小寫重複 dep」不同（那是同一包名大小寫變體重複宣告），這是**兩個不同 repo 內含同名 skill 造成的部署路徑碰撞**，Python 端完全沒有偵測/警告機制，且 lockfile 產生雙重歸屬的髒資料，之後任一方 uninstall 都有刪錯/漏刪風險。apm-go 在這一項上**行為優於 Python**，不是要「追平」Python，而是要在 R7/R12 補輸出時**保留**這個既有的衝突偵測優勢，不要在聚合輸出時把 `!` 警告一起精簡掉 |
| design install | `ℹ Installing…` / `✓ Resolved 2 dependencies` / `ℹ Targets: claude, codex, antigravity` / **65 行**逐子目錄樹（兩個 dep 各自展開）/ `✓ Installed 2 dependencies` / 兩個 `•dep (depth 1)`（**均無 hash**） | `[x] Unknown target 'antigravity'` → `[!] Install interrupted after 3.3s.`，exit 2，未進入 resolve/deploy 之外的任何步驟 | 同 antigravity：`target: [claude, codex, antigravity]` 含 `antigravity`，Python 直接拒絕整個 target 清單（即使 claude/codex 合法，混合到不支援的 target 一樣整組失敗） | 丙（apm-go-only 功能，非 bug） | 65 行樹=**R10b 實例**（本次稽核中最大量的一次）；無 hash=**R10a 實例**；F2 的第二個實例（`antigravity` 混在合法 target 清單中，Python 一樣整組拒絕，不會只跳過不支援的那個） |
| design uninstall `Leonxlnx/taste-skill` | dry-run 詳列 `•Leonxlnx/taste-skill (dependencies.apm)` + `•apm_modules/Leonxlnx/taste-skill: exists`；正式執行只剩 `✓ Removed 1 package(s)` + `✓ apm_modules: removed 1 directory` | 無法比較（F2） | dry-run 詳細 vs 正式執行只剩數字 | 乙 | R7 本體（再次驗證）；py 側 SKIP，理由=F2 |

## (a) 新發現清單（不在 R7–R16 / BUG-1 內）

1. **F1 — Python `install` 的 MCP runtime 自動偵測 fallback 誤傷合法 target 專案**（`demo` fixture，丙）
   實測：`demo/apm.yml` 宣告 `target: [copilot, claude, opencode, codex]`（4 個合法 canonical target），且 `dependencies.mcp` 只有 1 個 `github` server。Python 在**本機偵測不到已安裝的 copilot/codex CLI/runtime** 時，`mcp_integrator_install.py` 會 fallback `target_runtimes = ["vscode"]`，而這個 fallback token 隨後被拿去對 `apm.yml` 宣告的 target 白名單做驗證（`parse_targets_field`），"vscode" 不在 `CANONICAL_TARGETS` 裡，整個 MCP 寫入被 gate 掉，印出「apm.yml 'targets' field is invalid / Unknown target 'vscode'」——但 apm.yml 的 targets 根本沒問題，錯誤訊息完全誤導使用者去改 apm.yml。已用隔離 `HOME` 重跑排除本機殘留設定污染的可能性，此為可重現的程式邏輯本身。apm-go 沒有這種 runtime auto-detect 依賴，直接照 apm.yml 宣告的 target 清單部署，4 個 target 的 MCP config 全部成功寫入。
   → 這是 **Python 側的邏輯缺陷**，非 apm-go 需要追平的行為；反而要小心不要把這個 fallback 邏輯抄進 apm-go。

2. **F2 — `antigravity` target 是 apm-go 專屬擴充，Python 完全不支援**（`antigravity`、`design` 兩個 fixture，丙／能力邊界）
   Python 的 `CANONICAL_TARGETS` 集合裡沒有 `antigravity`，`apm install` 一遇到就直接 `[x] Unknown target 'antigravity'` + exit 2，即使清單裡混著其他合法 target（`design` 的 `[claude, codex, antigravity]`）也是整組拒絕，不會只跳過不支援的那個。這代表 **`antigravity`/`design` 這兩個 fixture 上，apm-go 與 Python 之間沒有「內容 parity」可比較**——Python 端從未進入 resolve/deploy，是純粹的功能落差而非輸出差異。已知 `.trellis/tasks/archive/2026-07/07-11-antigravity-plugins-bundle/` 是 apm-go 專門為此新增支援的既有任務，屬預期的既有擴充，非新 bug，但**先前的 R7–R16 静態稽核從未點名這個「無法比較」的邊界**，本次是第一次用實跑確認並記錄下來，避免未來稽核誤以為這兩個 fixture 可以做逐字 parity 比對。

3. **F3 — `install` 無 target 時的錯誤訊息，apm-go 品質遠低於 Python**（`bundle-demo` as-authored，乙）
   两邊都正確 fail-closed（沒有 target 訊號時不亂猜、不 silently 部署到 copilot），**行為本身 parity**。但錯誤訊息內容差距大：
   - apm-go：`Error: no deployment target detected; pass --target <name> or add a target: to apm.yml`，然後把 `install` 指令**全部 14 個 flag 的用法**印出來（Cobra 預設行為），對「怎麼解決 target 缺失」毫無幫助。
   - Python：`[x] No harness detected` + 明確列出它掃描過的 14 種 harness marker（`.claude/`、`.cursor/`、`.github/copilot-instructions.md`…）+ 3 個具體修法（`apm targets`／`--target claude`／`--target copilot`）+ `apm.yml` 寫法範例。
   apm-go 完全知道自己掃過哪些目錄（signal-detection 邏輯本來就要跑過這些路徑才能判定「沒找到」），只是沒有把這份清單印出來——與 R7/R11 同型的「資料已算出、只是沒印」缺口，但發生在**最容易被使用者第一次踩到的錯誤路徑**上，能見度極高，建議提高優先度單獨處理。

4. **F4 — 跨 repo 同名 skill 碰撞，Python 端靜默覆蓋 + lockfile 雙重歸屬，apm-go 端有偵測+警告+正確歸屬**（`bundle-demo` 補充測試，丙，本次最重要發現）
   `forrestchang/andrej-karpathy-skills` 與 `multica-ai/andrej-karpathy-skills` 兩個不同的 GitHub repo 都各自內含一個叫 `karpathy-guidelines` 的 skill，同時宣告在 `dependencies.apm` 時：
   - **apm-go**：偵測到部署路徑碰撞，印出 `! skills "karpathy-guidelines" from dependency:multica-ai/... shadowed by dependency:forrestchang/... (first-declared wins)` + `! multica-ai/andrej-karpathy-skills deployed 0 files to any target`。磁碟上只有 forrestchang 這份，lockfile 正確地只把該檔案歸屬給 forrestchang 一個 dependency。之後 `uninstall multica-ai/...`（被 shadow 的一方）只移除空的 `apm_modules` 目錄，forrestchang 部署的檔案完全不受影響。
   - **Python**：**完全沒有碰撞偵測**，兩個 `[+] ... 1 skill(s) integrated -> .claude/skills/` 都印「成功」。實測磁碟上 `.claude/skills/karpathy-guidelines/SKILL.md` 只有一份（後裝的一方靜默覆蓋前者），但 `apm.lock.yaml` 把**同一個檔案路徑、同一個 content_hash**，同時記錄在 `forrestchang/andrej-karpathy-skills` 與 `multica-ai/andrej-karpathy-skills` 兩個 dependency 條目底下——這是內部不一致的髒帳目。後續 `uninstall forrestchang/...`（其中一個「擁有者」）該次實測沒有把檔案刪掉（因為另一個 lockfile 條目仍指著它），但 stdout 印出 `[+] Cleaned up 1 integrated skills`，對使用者而言是誤導性的成功訊息（實際上什麼都沒清理，只是恰好沒删錯）。
   注意：**這不是題目提到的 BUG-1**（BUG-1 是同一包名的大小寫變體重複宣告造成的重複計算；這裡是兩個完全不同的 repo URL，只是內含同名 skill 檔案路徑）。這是本次稽核唯一一項「apm-go 行為明顯優於 Python」且屬於真實資料完整性風險的發現，建議在後續補 R7/R12 的輸出時，**保留**這個衝突偵測 + `!` 警告機制，不要在做輸出精簡/聚合時把它一併簡化掉。

5. **F5 — `test1` fixture：apm-go 在純 local-only 安裝時未建立/未提及 `.gitignore`**（乙，次要）
   Python 安裝完 local 內容後印出 `[i] Added apm_modules/ to .gitignore` 並建立 `.gitignore`（即使這次沒有任何 apm_modules 內容，只有純 local 部署）。apm-go 完全沒有建立 `.gitignore`，也没有任何相關訊息。嚴重度低（沒有 apm_modules 目錄時，`.gitignore` 條目本身沒有實際作用），但屬於「Python 有做、apm-go 完全沒有對應行為/訊息」的落差，記錄供後續判斷是否要補。

## (b) 丙類（業務邏輯）bug 清單 — 依重要性排序

1. **F4（最重要）**：Python `install` 跨 repo 同名 skill 碰撞時靜默覆蓋 + lockfile 雙重歸屬同一檔案路徑，且 uninstall 後印出誤導性的「Cleaned up」訊息。**這是 Python 側的真實資料完整性 bug**，apm-go 在同一情境下行為正確（偵測、警告、正確歸屬、uninstall 不誤刪）。不需要修 apm-go；需要注意的是**不要在 R7/R12 補輸出精簡時，把 apm-go 現有的衝突警告機制弱化或省略掉**。
2. **F1**：Python `install` 的 MCP runtime auto-detect fallback（`["vscode"]`）在偵測不到本機已安裝 runtime 時，會誤觸發 targets 白名單檢查、把合法專案的 MCP 部署整個擋掉，且錯誤訊息（"apm.yml 'targets' field is invalid"）與實際問題（本機沒裝 runtime，不是 apm.yml 的問題）完全對不上。同樣是 Python 側 bug，不影響 apm-go；但如果 apm-go 未來要加類似的 runtime auto-detect 功能，這是一個要避開的錯誤設計範例。
3. （BUG-1，題目已知項，本次未重新觸發驗證——本次 fixture 中沒有刻意構造大小寫重複依賴的場景，聚焦在實際 5 個既有 fixture 的既定內容，未新增額外案例去驗證 BUG-1 本身。）

以上 2 項丙類發現皆為 **Python 端**的邏輯缺陷，**没有找到新的 apm-go 端業務邏輯 bug**（apm-go 這次实测中的問題全部屬於乙類「presentation-only／輸出訊息不完整」，或是 F2 這種「功能邊界，非 bug」）。

## (c) 前 5 高優先（跨甲/乙/丙综合）

1. **F4 — 跨 repo 同名 skill 碰撞的資料完整性風險**（丙，Python bug，供參考／確認 apm-go 現有優勢不要被輸出精簡蓋掉）——嚴重度最高，因為牽涉 lockfile 帳目失真與潛在誤刪風險，即使是 Python 端的問題，也建議在 apm-go 補 R7/R12 輸出時，明確把「shadowed / deployed 0 files」這類衝突警告保留在精簡後的摘要中，不要被聚合邏輯吃掉。
2. **R16（`test1` 實測命中）— 空 apm.yml + local 部署時「Installed 0 dependencies」與實際部署 5 個檔案矛盾**——題目點名項，本次為第一次拿到**完整實測 stdout 逐字稿**作為修復對照基準。
3. **R13（`demo` 實測命中）— `install` 主流程完全不印 MCP 部署摘要**——本次證實不只是「presentation 不完整」，而是**使用者完全看不到 apm-go 悄悄部署了 4 個 target 的 MCP config**，資訊落差比静態稽核描述的更嚴重（原文件只說「presentation-only」，實測顯示 stdout 對這件事**只字未提**）。
4. **新發現 F3 — `install` 無 target 偵測失敗時的錯誤訊息品質**——高能見度（使用者第一次設定專案最容易撞到），apm-go 目前把整包 Cobra flag 說明砸出來，對比 Python 的結構化診斷差距明顯，建議納入下一輪 R 编号。
5. **R10b（`antigravity`/`design` 大規模實測）— 部署樹逐子目錄列印，未依 kind→target 聚合**——本次用真實網路依賴（45 行、65 行）驗證了静態稽核描述的規模有多誇張，是目前所有實測輸出中「洗版」最嚴重的一項，應優先處理。

## (d) 各 fixture dump 檔路徑

全部位於 `C:/Users/gn006/AppData/Local/Temp/claude/D--Projects-apm-dev-apm-go/a89d1d9b-7463-4619-9216-9a15889c4126/scratchpad/ab-sweep/dumps/`：

| fixture | 檔案 |
|---|---|
| test1 | `test1-install-go.log`、`test1-install-py.log` |
| demo | `demo-install-go.log`、`demo-install-py.log`、`demo-install-py-cleanhome.log`（隔離 HOME 重跑，排除機器污染）、`demo-uninstall-mcp.log` |
| antigravity | `antigravity-install-go.log`、`antigravity-install-py.log`、`antigravity-uninstall-go.log` |
| bundle-demo | `bundle-demo-install-go.log`、`bundle-demo-install-py.log`（as-authored，無 target，兩邊皆 fail-closed）、`bundle-demo-collision-supplementary.log`（補充：`--target claude`，F4 碰撞發現的完整逐字稿） |
| design | `design-install-go.log`、`design-install-py.log`、`design-uninstall-go.log` |

對應的 temp 專案目錄（乾淨輸入 + 實際部署後的完整檔案樹，供需要時複查）：`.../scratchpad/ab-sweep/{test1,demo,antigravity,bundle-demo,design}/{go,py}/`，以及補充測試用的短路徑 `C:\t\bdgo`、`C:\t\bdpy`。

## Caveats / 未涵蓋範圍

- 未對 `test1`/`demo` 之外的 fixture 做「重複執行 install（既存 dep 灰化，R9）」的實測——R9 需要對同一 dep 跑兩次 install 觀察灰化呈現，這次聚焦在單次乾淨安裝 + uninstall 的落差，R9 沿用既有静態稽核結論，未重新驗證。
- 未驗證 `mcp install/uninstall` standalone 模式（`install --mcp NAME`）的 A/B，因為 `evals/ab_uninstall.py` 已經對這個場景做過詳盡的 A/B（un-019 deviation），本次改聚焦在 apm.yml 靜態宣告的 `dependencies.mcp` 這個之前未覆蓋的路徑（見 demo fixture）。
- `bundle-demo` 原始 `apm.yml` 宣告的 `marketplace.packages[0].source: ./pkgs/tool-a` 指向一個不存在的路徑（`bundle-demo/pkgs/` 目錄從未建立），這個問題只影響 `marketplace` 系列子指令（`validate`/`build`），不影響本次測試的 `install`/`uninstall`，未深入追查，記錄於此供未來稽核 `marketplace` 指令時留意。
- 檔案位元組級別比較（例如 `test1` 兩邊部署檔案的 CRLF vs LF 差異，導致 lockfile 內 hash 不同）**不在本次 stdout/stderr 差異範圍內**，僅在稽核過程中順手發現、快速排除為「非 bug」（單純換行符差異），未列入正式發現。
- 全部 5 個 fixture 均**無 SKIP**：網路可用、Windows 路徑長度問題已定位為環境假象並排除、`antigravity`/`design` 的 Python 側雖無法產出實質 parity 比較，但已完整記錄原因（F2）而非放棄不測。

## Completed: 5/5 fixtures swept
