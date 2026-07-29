# Codex 對抗性稽核報告（checklist.md）

- 稽核者：codex-cli 0.144.6，`codex exec -s read-only`
- 日期：2026-07-29
- 對象：`checklist.md`（70 條）
- 目的：`.trellis/workflow.md:302-306` 規定的獨立稽核層（失效去相關），提示詞為反向目標（去反駁而非確認）
- 稽核者自述方法：抽查 18 條 AC 驗法、重跑 T1/T2 regex、讀 Go 原始碼與 pin 住的上游原始碼；唯讀環境無法建立 go-build 暫存目錄，故未把未執行的測試算作通過

---

結論：這份 checklist 尚不足以當實作閘門。我找到 5 個阻斷級問題；其中 X1/X2 的成本論證與現行程式碼直接矛盾，Tripwire sweep 也有可重現的錯誤。

我抽查了 18 條 AC 驗法、重跑 T1/T2 regex、讀取相關 Go 原始碼及 pin 住的上游程式碼。唯讀環境禁止 Go 建立 `go-build` 暫存目錄，因此沒有把未能執行的測試當成通過。

## 阻斷級（實作前必須解決）

1. X1/X2 建立在「四個子系統完全沒有 dev 概念」的錯誤前提上 · 位置：[checklist.md:589](/D:/Projects/apm-dev/apm-go/.trellis/tasks/07-28-marketplace-plugin-parity/checklist.md:589)、[checklist.md:604](/D:/Projects/apm-dev/apm-go/.trellis/tasks/07-28-marketplace-plugin-parity/checklist.md:604) · 為什麼是錯的：`install` 已正規化、解析及部署 `ParsedDevDeps`（[install.go:430](/D:/Projects/apm-dev/apm-go/cmd/apm-go/install.go:430)、[install.go:451](/D:/Projects/apm-dev/apm-go/cmd/apm-go/install.go:451)、[install.go:1021](/D:/Projects/apm-dev/apm-go/cmd/apm-go/install.go:1021)）；`update` 明確處理 dev dependency（[update.go:303](/D:/Projects/apm-dev/apm-go/cmd/apm-go/update.go:303)）；`uninstall` 已有 `IsDev` 分支（[uninstall_resolve.go:53](/D:/Projects/apm-dev/apm-go/cmd/apm-go/uninstall_resolve.go:53)）。更直接的反例是現有測試已證明手寫 `devDependencies.apm` 能解析、部署並寫進 lockfile（[install_test.go:126](/D:/Projects/apm-dev/apm-go/cmd/apm-go/install_test.go:126)）。威脅模型：task 會產生 `devDependencies: {apm: []}`，卻不提供官方 CLI 填入它，交付的是無法走完上游 plugin 作者主流程的半套功能；而 X2 所稱「沒有 `--dev` 就沒有分類意義」也被手寫 dev dependency 的現有支援反證。成本重估：主要缺口是旗標 plumbing、持久化時選擇 prod/dev 區段，以及 lockfile 欄位的 parse/write/equality；粗估 5–8 個檔案、100–250 LOC，不是論證所稱的四套全新子系統 · 建議：在開始實作前重新裁定 X1/X2，至少把它們設成 plugin parity 的前置 child task；不能沿用目前的「大工程」結論。

2. C1 只是把 category 失敗從 add 移到 pack，還會製造跨工具死結 · 位置：[checklist.md:374](/D:/Projects/apm-dev/apm-go/.trellis/tasks/07-28-marketplace-plugin-parity/checklist.md:374) · 為什麼是錯的：Go 的 `AddOptions` 根本沒有 category 欄位（[editor.go:410](/D:/Projects/apm-dev/apm-go/internal/marketplace/authoring/editor.go:410)），所以 codex output 開啟時，`apm-go package add` 會先寫入缺 category 的 entry，之後 `CodexMapper.Compose` 必然失敗（[codexmapper.go:95](/D:/Projects/apm-dev/apm-go/internal/marketplace/build/codexmapper.go:95)）。上游之所以擋住，是其 YAML editor 採「原子寫入、全檔重驗證、失敗回復」契約，確保每次 CLI edit 後 config 仍有效（[上游 yml_editor.py](https://github.com/microsoft/apm/blob/634f7b603a8c/src/apm_cli/marketplace/yml_editor.py#L1-L15)）。跨工具 repro：用 apm-go 新增缺 category 的 package，再用上游執行任一會載入並重驗證 marketplace 的編輯指令，會進入文件聲稱已避開的同一死結。成本估計：若新增 `--category` 並串到 `AddOptions`/YAML/test，約 30–60 LOC；只警告則更小，但仍留下無效中間狀態 · 建議：重新裁定「CLI 必須維持有效 config」還是「允許無效中間狀態」，並加入 apm-go → 上游的互通 repro；目前的「本專案較佳」沒有成立。

3. R→AC 對照表宣稱 33 個子項皆充分覆蓋，但至少 6 個仍有實質缺口 · 位置：[checklist.md:438](/D:/Projects/apm-dev/apm-go/.trellis/tasks/07-28-marketplace-plugin-parity/checklist.md:438) · 為什麼是錯的：

   - R3.3.a：AC9 只測 underscore，未測首字元、64/65 長度邊界；AC32 只證明 `My_Project` 可用，沒有證明 consumer 原有的 `/`、`\`、`..` 拒絕仍存在（現行規則在 [init.go:35](/D:/Projects/apm-dev/apm-go/cmd/apm-go/init.go:35)）。
   - R3.3.b：互動模式預設 `1.0.0` 完全沒有專屬 AC；AC10 只測 `--yes` 的 `0.1.0`。
   - R4.2：AC14–16 只測 `skills/`；實作即使漏掉 `agents/ commands/ instructions/ extensions/ hooks/ hooks.json` 仍可全過。
   - R5.4：AC20 把「不解析 ref」錯寫成「不觸網」。上游會先驗證 source reachability，再進 `_resolve_ref` 的 version 短路（[上游 add.py](https://github.com/microsoft/apm/blob/634f7b603a8c/src/apm_cli/commands/marketplace/plugin/add.py#L55-L70)、[上游 __init__.py](https://github.com/microsoft/apm/blob/634f7b603a8c/src/apm_cli/commands/marketplace/plugin/__init__.py#L102-L119)）；目前 Go 同樣在 [editor.go:450](/D:/Projects/apm-dev/apm-go/internal/marketplace/authoring/editor.go:450) 先跑 reachability。
   - R5.5：AC21 只執行 local + 零旗標測試（[editor_test.go:15](/D:/Projects/apm-dev/apm-go/internal/marketplace/authoring/editor_test.go:15)），沒有覆蓋需求所稱的 explicit HEAD、version、no-verify 等分支。
   - R7.1：AC23 的條件只是「存在 Plugin 測試且 PASS」，沒有斷言 Form、MultiSelect、Confirm 三條分支各被命中。

   建議：撤回對照表的總結，依上述子項補成可各自失敗的 AC。

4. 多條驗法會假綠，不能當硬性驗收 · 位置：[AC18](/D:/Projects/apm-dev/apm-go/.trellis/tasks/07-28-marketplace-plugin-parity/checklist.md:141)、[AC23](/D:/Projects/apm-dev/apm-go/.trellis/tasks/07-28-marketplace-plugin-parity/checklist.md:175)、[AC25](/D:/Projects/apm-dev/apm-go/.trellis/tasks/07-28-marketplace-plugin-parity/checklist.md:189)、[C5](/D:/Projects/apm-dev/apm-go/.trellis/tasks/07-28-marketplace-plugin-parity/checklist.md:412)、[AC35](/D:/Projects/apm-dev/apm-go/.trellis/tasks/07-28-marketplace-plugin-parity/checklist.md:273) · 為什麼是錯的：

   - AC18 用 `go run ...; echo $LASTEXITCODE` 驗 child exit 2；Go 自己明載 `go run` 的 exit status 不等於被執行 binary（[run.go:56](</C:/Program Files/Go/src/cmd/go/internal/run/run.go:56>)）。
   - AC23 的 `go test ... -run Plugin`、AC25 預想的 `-run TestSupportedTargets` 在零匹配時也可 exit 0；兩者都沒有先列出實際執行的測試。
   - AC25 的指令指向 `internal/manifest`，但「互動選單實際 opts」位於 `cmd/apm-go`；目前驗法跑錯 package。
   - C5 的 `git diff main...HEAD -- go.mod` 只比較 commits，忽略未提交 working tree。現況 task 目錄是 untracked，該命令仍看不到它，已可重現同類假陰性。
   - AC35 要求「總覆蓋率」，但 `go test ./... -cover` 輸出各 package 百分比，不產生 repository total，故 ≥80% 沒有可計算的通過值。
   - AC16 允許 `t.Skip` 後仍視為過關，等於 symlink 行為從未被測也能勾選。

   建議：AC18 改跑已建好的 `bin/apm-go.exe`；所有 `-run` 閘門先用 `go test -list` 證明匹配集合；C5 同時檢查 working tree；覆蓋率以 coverprofile 聚合；硬性 AC 不接受 skip 作為成功。

5. T1/T2 的結論可直接重現為錯誤，而且「meta」分類掩蓋了真正的判斷 · 位置：[checklist.md:750](/D:/Projects/apm-dev/apm-go/.trellis/tasks/07-28-marketplace-plugin-parity/checklist.md:750) · 為什麼是錯的：相同 regex 實跑後，T1 不是零匹配，命中 `implement.md:173` 的 `延後`；T2 是 67 個匹配行，不是條目內反覆聲稱的 66。文件末尾 [checklist.md:864](/D:/Projects/apm-dev/apm-go/.trellis/tasks/07-28-marketplace-plugin-parity/checklist.md:864) 自己也寫「最終 67」，與 T2 主體矛盾。更嚴重的是，lines 438–470 的 32 個「完整覆蓋」不是 meta：它們是對 AC 是否足以否證 R 子項的判斷；R3.3.b、R4.2、R5.4、R5.5 已提供具體反例，證明至少數格判斷為假 · 建議：T1 改記實際命中並分類；T2 不得把覆蓋矩陣整批視作 meta，必須逐格做判斷稽核。

## 重大（應該解決）

1. C2/X7 把診斷資訊缺失誤稱為純格式差異 · 位置：[checklist.md:386](/D:/Projects/apm-dev/apm-go/.trellis/tasks/07-28-marketplace-plugin-parity/checklist.md:386)、[checklist.md:677](/D:/Projects/apm-dev/apm-go/.trellis/tasks/07-28-marketplace-plugin-parity/checklist.md:677) · 為什麼是錯的：上游結果模型分別保存 `reachable`、`version_found`、`ref_ok` 和 detail（[上游 check.py](https://github.com/microsoft/apm/blob/634f7b603a8c/src/apm_cli/commands/marketplace/check.py#L130-L225)）；Go 的 `CheckResult` 只有 `Package` 與單一 `Err`（[refcheck.go:131](/D:/Projects/apm-dev/apm-go/internal/marketplace/authoring/refcheck.go:131)）。這不是把相同資料從 table 改成 bullets，而是 Go 根本沒有三個診斷維度。威脅模型：使用者無法區分 remote unreachable、沒有版本 tag、ref 不存在；除錯與自動化診斷能力下降。成本估計：約 60–120 LOC，涉及 result model、checkPackage、renderer 與測試 · 建議：可以保留 bullet UI，但必須先裁定是否要求資訊等價；目前論證不能支撐 X7。

2. C3 只證明 apm.yml 的寫入較晚，不能宣稱 install 整體具交易性 · 位置：[checklist.md:394](/D:/Projects/apm-dev/apm-go/.trellis/tasks/07-28-marketplace-plugin-parity/checklist.md:394) · 為什麼是錯的：`deploy.Run` 在 [install.go:1620](/D:/Projects/apm-dev/apm-go/cmd/apm-go/install.go:1620) 已修改部署目標，lockfile 到 [install.go:1764](/D:/Projects/apm-dev/apm-go/cmd/apm-go/install.go:1764) 才寫，apm.yml 更晚到 [install.go:1777](/D:/Projects/apm-dev/apm-go/cmd/apm-go/install.go:1777)。repro：讓 deployment 成功、再令 lockfile 寫入失敗，部署檔已變但 lock/apm.yml 未更新。完整交易要涵蓋 deploy、lock、manifest，估計至少 200+ LOC 與 failure-injection tests；這確實可能不適合本 task，但不能用目前三個行號宣稱整體策略較佳 · 建議：把結論縮窄成「本 task 不改 manifest persistence 順序」，不要稱為全域交易安全證明。

3. X4 的「四個 adapter 是大工程」沒有成本證據，且 warning 並不等於支援 · 位置：[checklist.md:630](/D:/Projects/apm-dev/apm-go/.trellis/tasks/07-28-marketplace-plugin-parity/checklist.md:630) · 為什麼是錯的：現有簡單 adapter 僅 19–39 行，例如 [agentskills.go:1](/D:/Projects/apm-dev/apm-go/internal/deploy/agentskills.go:1)、[claude.go:1](/D:/Projects/apm-dev/apm-go/internal/deploy/claude.go:1)；未研究四個 target 的實際格式前，不能直接定級為「大」。此外 unsupported target 會在 [adapter.go:170](/D:/Projects/apm-dev/apm-go/internal/deploy/adapter.go:170) 產生 diagnostic，接著被 [adapter.go:180](/D:/Projects/apm-dev/apm-go/internal/deploy/adapter.go:180) 從部署集合濾掉，功能確實沒有執行。成本應先做每 target 0.5–1 天 reconnaissance，再依是否可重用 adapter 判定 · 建議：X4 的產品邊界可以保留，但成本欄應標為未驗證；D3 的註解也不應叫 `Accepted values`，因為 parser 實際接受 10 個（[target.go:5](/D:/Projects/apm-dev/apm-go/internal/manifest/target.go:5)）。

4. X5 的 seam 不能取代使用者要求的 PTY 驗證 · 位置：[checklist.md:647](/D:/Projects/apm-dev/apm-go/.trellis/tasks/07-28-marketplace-plugin-parity/checklist.md:647) · 為什麼是錯的：checklist 說既有 TTY seam 足以驅動互動流程，但 design 自己承認只 stub TTY 會真的進入 huh 並卡住（[design.md:241](/D:/Projects/apm-dev/apm-go/.trellis/tasks/07-28-marketplace-plugin-parity/design.md:241)），所以還必須把三個 huh 函式改為 stub。這只能測參數資料，測不到真 binary 的 stdin/stderr wiring、Ctrl-C、escape sequence、終端尺寸及 Windows ConPTY。成本估計：單一 Windows PTY smoke 約 1–2 天、80–150 LOC；跨平台矩陣更高。`go.mod` 無直接 PTY dependency、`go.sum` 有傳遞 dependency 這項事實成立，但不等於風險已被覆蓋 · 建議：至少保留一條真 binary + ConPTY smoke，或明確承認此 task 不驗證真終端整合。

5. 多個端對端指令缺 fixture 或工作目錄，照抄不能執行 · 位置：AC9、AC14、AC17–20、AC29、AC31、AC34 · 為什麼是錯的：AC14/34 說在空目錄建立 `skills/` 後跑 `go run ./cmd/apm-go`，但離開 repo 後該 package path不存在；留在 repo 又會受到現有 `.claude/`、`.codex/` marker 污染。AC31 仍是 `<path-to-apm-go>` placeholder。AC17–20 沒提供最小 marketplace YAML，且依賴 live network。AC29 只放 `target: claude`，沒有 dependency 或 local primitive，卻要求觀察部署產物；`pack` 本身也不是部署命令 · 建議：這些 AC 統一使用預先 build 的絕對 binary、`t.TempDir` fixture、fake/local git remote，以及明列的最小 apm.yml。

6. T3 說要掃 JSONL，實際驗法沒有包含；implement 又仍寫 63 條 · 位置：[checklist.md:816](/D:/Projects/apm-dev/apm-go/.trellis/tasks/07-28-marketplace-plugin-parity/checklist.md:816)、[implement.md:173](/D:/Projects/apm-dev/apm-go/.trellis/tasks/07-28-marketplace-plugin-parity/implement.md:173) · 為什麼是錯的：T3 的動機點名 `check.jsonl`/`implement.jsonl`，但列出的範圍只有 md、research、git diff 與 commit message；JSONL 可含真斷言卻完全沒被掃。implement 的 63 與 checklist 的 70 不一致，執行者可照 plan 少走 7 條 · 建議：把實際 glob 與條目數改成單一可執行來源，不要靠敘述同步。

7. X6 沒有符合專案規則要求的證據三件套 · 位置：[checklist.md:663](/D:/Projects/apm-dev/apm-go/.trellis/tasks/07-28-marketplace-plugin-parity/checklist.md:663) · 為什麼是錯的：第一件「file:line」引用的是 research 文件，不是實際 code path；第三件寫「無法估計」，並非成本估計。產品判斷本身合理——素材只有「studio」一詞（[eval research:372](/D:/Projects/apm-dev/apm-go/.trellis/tasks/07-28-marketplace-plugin-parity/research/eval-real-run-20260728.md:372)），不應猜測實作——但它不符合 AGENTS §5 的硬格式 · 建議：把它保留為未釐清的產品問題，而不是宣稱已具備技術證明。

## 次要（可選）

1. AC7 對註解骨架的條件太寬 · 位置：[checklist.md:62](/D:/Projects/apm-dev/apm-go/.trellis/tasks/07-28-marketplace-plugin-parity/checklist.md:62) · 為什麼是錯的：任何 `# targets:` 加任意 `#   - garbage` 都可通過，但 design 指定最後一行應為 `#   - claude`（[design.md:71](/D:/Projects/apm-dev/apm-go/.trellis/tasks/07-28-marketplace-plugin-parity/design.md:71)） · 建議：逐字斷言五行骨架。

2. AC13 沒有驗證 plugin 作者版 Next Steps 的精確內容 · 位置：[checklist.md:106](/D:/Projects/apm-dev/apm-go/.trellis/tasks/07-28-marketplace-plugin-parity/checklist.md:106) · 為什麼是錯的：只要輸出任意位置含 `apm-go pack` 且沒有 `--dev` 就算過；consumer 的 install 提示、錯誤標籤或多餘步驟都不會被抓 · 建議：斷言精確一行 `Pack as plugin: apm-go pack`，並確認 consumer 提示不存在。

3. AC26 主要靠 source grep，不是行為測試 · 位置：[checklist.md:204](/D:/Projects/apm-dev/apm-go/.trellis/tasks/07-28-marketplace-plugin-parity/checklist.md:204) · 為什麼是錯的：第二份清單若拆成數個字面量、經 helper 拼接，完整字串 grep 仍為零；測試也沒有真的新增一個 target 後觀察 comment 跟著變 · 建議：測試中暫時替換來源切片或直接呼叫 comment builder，驗證輸出由輸入集合決定。

## 我查過但站得住的

- X3：現行 `init.go:223-225` 無 `--plugin`；另跑 `git log --all -S--plugin -- cmd/apm-go/init.go` 也無歷史匹配，因此「本專案沒有舊使用者需要 migration alias」站得住。
- X8：Claude/Codex marketplace mapper 的欄位與 source 分支、Claude/Copilot plugin manifest 路徑和 mcpServers 差異，實碼均能對上研究列出的上游結構；未找到新的 schema 落差。
- C3 的窄版事實：resolution 與 lockfile 建構確實發生在 apm.yml 寫入之前；問題只在文件把這個事實擴張成整體交易安全。
- X5 的 dependency 事實：PTY 套件確實只在 `go.sum`，不在 `go.mod`。
- X6 的產品處理：目前沒有足夠資訊定義「studio」，不猜測功能是合理的；缺陷在證據格式與狀態表述。
- AC1、AC5、AC6、AC10–12、AC22、AC24、AC27、AC30、AC32、AC33 的通過條件大致可否證；未找到同級的判斷缺陷。

---

## 主對話的獨立複驗（2026-07-29）

不照單全收。以下每條都在主對話裡實際重跑過：

| Codex 發現 | 複驗方式 | 判定 |
|---|---|---|
| 阻斷 1：dev 機制已存在 | `grep -rn "ParsedDevDeps\|IsDev"` → 命中 `install.go:433,457,1030-1035`、`update.go:94,303`、`uninstall_resolve.go:58,253,273`、`uninstall.go:586`、`pack.go:299`、`compile.go:76`；並讀到既有測試 `install_test.go:126` `TestRunInstall_DevDependency_ResolvedDeployedAndLocked` | **成立**。主對話原本的「成本大」估計錯誤 |
| 阻斷 2：C1 站不住 | 讀 `editor.go:413-422`，`AddOptions` 確無 `Category` 欄位 | **成立** |
| 阻斷 3：R5.4 的 AC20 說錯 | 讀上游 `add.py:62-67`：`_verify_source` 在 `_resolve_ref` **之前**；apm-go `editor.go:450` 同序 | **成立**，`--version` 仍會觸網做 reachability |
| 阻斷 4：`go run` 不傳 exit code | 實測：`os.Exit(2)` → `go run` 回 **1**，直接跑 binary 回 **2** | **成立** |
| 阻斷 5：T1 已失效 | `grep` 命中 `implement.md:173` 的「延後」——該行是 T1 跑完之後才由主對話寫入 | **成立**，反證 T3 有必要 |

### 對 Codex 論述的一處修正

阻斷 2 說 apm-go「只是把 category 失敗從 add 移到 pack」。這在 codex 輸出開啟時成立，
但不完全等價：`schema.go:12-21` 的 compose-time-only 設計讓 `apm-go pack -m claude`
在有 codex-missing-category 的 package 時**仍能成功**，上游則整個 add 都被擋。
所以 apm-go 在 claude-only 情境確實較佳；codex 的批評精確地講是
「較佳的部分被過度推廣成全面較佳，且新增了無效中間狀態這個代價」。
裁定：補 `--category` 旗標，讓兩個情境都真的優於上游。

## 據此做出的裁定（使用者 2026-07-29）

1. **D5 撤銷**：`install --dev` 納入本 task（原裁定基於錯誤成本估計）
2. **C1 改為補 `--category` 旗標**（30–60 LOC），不再宣稱「維持現狀較佳」
3. Codex 其餘 7 項重大、3 項次要發現全部修進 `prd.md` 與 `checklist.md`
