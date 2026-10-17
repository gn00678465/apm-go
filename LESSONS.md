<!-- reviewed-through: 0cf2bd2 (2026-10-17 07:08 +0800) -->

# 教訓

本檔是 apm-go 合併後的復盤紀錄。復盤讀已合併的改動，記下四種情況：修完又返工、不該合併、驗證太粗、做對值得照做。

每條教訓的格式：一句規則、原因、證據、`Seen N×` 與日期。證據要能用 `git`、`gh` 或 `board log <issue> --project apm-go` 查到。`Seen` 計算不同的日子（UTC+8）。

教訓與規則的地位不同：

- AGENTS.md「升格規則」是規則，必須遵守。違反就不能交付。
- 本檔的教訓是候選規則。違反不會擋下 PR，但不能略過：改程式前先讀，適用的照做，在 PR 說明寫出做了什麼。
- 同一條教訓在 3 個不同日子出現就升格。規則寫進 AGENTS.md「升格規則」；原條目連同證據移到本檔「已升格到 AGENTS.md」保留。

復盤步驟在 AGENTS.md「教訓復盤」。board 工具本身的問題不記在本檔。

## 2026-10-15 14:24 到 2026-10-17 07:08（+0800）的復盤

範圍 `34dfa71..0cf2bd2`：24 個已合併 PR（#84、#85、#90-#96、#98-#103、#105、#106、#108-#111、#117、#120、#121），75 個 commit（51 個非 merge、24 個 merge），改了 326 個檔案。範圍內沒有 tag，`v0.4.0` 在起點 `34dfa71`。

每個 PR 都來自 board 卡片，由 owner 授權合併。24 張卡中，16 張至少被審查退回一次（52 次審查有 30 次退回）。18 張因停止規則 1 停下等裁定，6 張需要介入，2 張逾時（#80、#114）。

出錯的地方：

- Not verified 列了這台機器能跑的檢查：PR #84 的 `normalize`、`validate` 處理 `../dep`；#69 審查時的本機 registry；#70 的離線 marketplace；sandbox 已可用後的 PR #117（Windows）；PR #120 的 registry `--global` 升級。後兩項在合併前另外補跑，結果貼在 PR 上。
- PR #92 的 `9968ee5` 沒有 owner 裁定，就在 PRODUCT.md 寫入一條和 Oracle 不同的行為：跟著使用者在目標根目錄內的 symlink 寫入。隔天 #112 推翻它（`cf8aa7a`，PR #117）。
- 合併的程式在 Windows 壞掉。`joinRealpath`（`22e71c6`，PR #90）與 `oraclePathJoin`（`0636bf8`，PR #105）只用 `/` 切路徑。PR #91、#92 的範圍檢查看不到 junction（#116）。這三處當時都列在 Not verified，第一次 Windows sandbox 實測就找出來（#88 留言 6216218590）。
- PR #91 的測試 `c73e914` 偶發失敗：git 在背景重新打包 fixture repo。下一張卡的 gate 抓到，`4cda93d` 修好。
- PR #96 的測試 `b901778` 把 bug 的輸出當預期值。PR #109 的 `33b05f1` 改成 Oracle 的輸出。
- 修正只修審查者指出的那一處。卡片 #86、#70、#112 各審了三輪，#112 在 `use_skill` 介入後才通過。三張卡（#75、#113、#114）在舊版或 Oracle 寫的 lock 上升級時出錯，都在合併前被審查者抓到。
- 對 Oracle 或 PRODUCT.md 的陳述有錯：#67、#89、#112 的 issue 本文由卡片調查更正；registry `.git` 的說法由 #113 的審查更正。
- 升級後使用者看得到的改變只寫在各 PR 的 Risks。#88 只收到 PR #101 的一條，沒有其他地方收集。
- agent 對由 shell 變數組成的路徑執行 `rm`（#104、#97），違反既有的專案規則。

做對的地方：Windows 與 registry 的檢查在合併前跑完，結果貼在 PR #117、PR #120。PR #121 附上 Linux 與 Windows 的 Oracle 對照，沒有 Not verified 段落。每張卡的調查都先對照 Oracle 查證 issue，才開始寫程式。

## 教訓

**審查者指出一條違反規則的路徑時，列出所有建立在同一個前提上的路徑，同一輪全部修好，並把清單寫進這一輪的回報。**
- 只修被點名的那一處，下一輪審查就會找到下一處。下列每張卡都審了三輪；#70 的介入說明寫到它的規則「only covered the case in front of it」。
- Evidence: board 64 events 1884, 1907（自我複製檢查：先是最終目的地，再是經過目的地連結的來源）；board 86 events 2141, 2165, 2201（根目錄外的清理、跟著最後一段 symlink 寫入、經過 skill symlink 的交接）；board 70 events 2412, 2437, 2460, intervene 2463；board 112 events 3281, 3304, 3328, intervene 3331（「every failure came from the same premise: the owner of a path is the bucket that deploys it this run」）。
- Done right on 2026-10-16: board 112 event 3351，`use_skill` 介入後第一輪就通過。
Seen 2× (2026-10-15, 2026-10-16)

**寫 Oracle 怎麼做之前，先實際跑 Oracle 0.33.0，或引用 Oracle 的 file:line。寫 PRODUCT.md 怎麼說時，引用原文。**
- 憑記憶描述 Oracle 行為的 issue 本文、留言與 PRODUCT.md 句子都錯了。每一處都要一張卡或一位審查者去推翻。
- Evidence: issue #89 本文對 Oracle 部署內容的描述，由 issue #89 留言 6187796986 更正；issue #67 的 exit code，由 issue #67 留言 6186847177 更正；issue #112 本文「PRODUCT.md has no rule for rule files」，由 issue #112 留言 6215906800 與 issue #88 留言 6215726615 更正；`00b5975` 的註解與 PRODUCT.md「Oracle removes registry `.git`」，由 board 113 event 3391 推翻，在 `300568e` 修正；`1b84146` 的「as the Oracle prints them」（2026-10-14），由 `eb3de05` 更正。
- Done right on 2026-10-15: #67 與 #89 的調查先實測 Oracle、更正 issue，才開始寫程式。
Seen 2× (2026-10-15, 2026-10-16)

**改動會切割、合併或解析路徑，或會檢查 symlink 時，合併前在 Windows sandbox 跑過，symlink 與 junction 都要測。**
- CI 只跑 `ubuntu-latest`。只用 `/` 切路徑的程式，或靠 `os.Lstat`、`filepath.EvalSymlinks` 辨認連結的程式，在 Linux 通過、在 Windows 失敗。Go 把 junction 回報為 `ModeIrregular`。
- Evidence: `22e71c6`（PR #90，`joinRealpath`）與 `0636bf8`（PR #105，`oraclePathJoin`）處理 `\` 路徑時失敗，在 issue #88 留言 6216218590 找到，現由 issue #127 追蹤；PR #91、#92 的範圍檢查被 junction 繞過，issue #116（2026-10-16 開立，PR #92 合併後約 23 小時）。
- Done right on 2026-10-16: PR #117 留言 6218591422。Done right on 2026-10-17: PR #121（Verified 段落列出 Windows sandbox 的執行結果）。
Seen 2× (2026-10-15, 2026-10-16)

**改動會讀 lock、清理舊檔或孤兒檔，或會改模組 key 時，交出前用上一版寫的 lock 與 Oracle 0.33.0 寫的 lock，各跑一次 install、update、frozen install、uninstall。**
- 下列三張卡都通過自己的測試，只在升級路徑上出錯。審查者先跑舊版執行檔才找到。
- Evidence: board 75 event 2692（`150bef8`：舊格式 lock 裡已移除的套件留下檔案）；board 113 event 3391（`00b5975`：在 Oracle 的 lock 上升級時刪掉 registry 套件的 `.git/config`）；board 114 event 3462（`ce7a1b2`：清理舊 clone 時刪掉新套件；uninstall 改寫留下套件的 key），在 `ca1cf45` 修正。
- Done right on 2026-10-17: PR #120 留言 6219879113（用 main 與 PR 的執行檔，在 main 寫的 lock 與 Oracle 寫的 lock 上各跑一次升級）。
Seen 2× (2026-10-16, 2026-10-17)

**沒有 owner 裁定點名時，不在 PRODUCT.md 新增和 Oracle 不同的行為。修正需要這種偏離時，用 `board ask` 停下。**
- PRODUCT.md 是後續卡片遵守的契約。寫在那裡的偏離會被讀成裁定。隔天推翻它，花掉一整張卡。
- Evidence: `9968ee5`（PR #92）加入「a deployed file that is a symlink to another place in its target root … is written where it points」；issue #112 留言 6215906800（「#86 has no owner ruling on this point」）；由 `cf8aa7a`（PR #117）推翻。
Seen 1× (2026-10-15)

**提交新的測試 fixture 前，跑 `git status --short --ignored`，確認沒有任何 fixture 被忽略（`*.out`、`apm_modules/`）。**
- 被忽略的 fixture 不會進 commit。測試在本機通過，到 CI 就失敗，或什麼都沒測到。
- Evidence: board 82 event 2920（協調者退回：10 個 `testdata/deps/*.out` 檔案與四個 parity 的 `home_fixture/.apm/apm_modules/` 目錄被忽略）。
Seen 1× (2026-10-16)

## 再次違反的既有規則

這裡記已寫在 AGENTS.md 或專案指示、又被違反的規則。它們不是新教訓，而是規則沒被檢查到的證據。

**AGENTS.md「Testing patterns」：預期值取自 Oracle 的原始碼或輸出，不照程式的算法重算。**
- 斷言目前輸出的測試會保護 bug。下一張卡得先改掉它，修正才能通過。
- Evidence: `b901778`（PR #96）`TestInstall_StaleMCP_ServerMovedToDevDependenciesIsKept`；issue #97 留言 6211906602；在 `33b05f1`（PR #109）更正。
Seen 1× (2026-10-16)

**專案規則：不對由 shell 變數組成的路徑執行 `rm`。刪除時寫死完整路徑，或每次重跑改用新目錄。**
- 這個指令會停在權限確認，沒人回應，整張卡就等著使用者。這條規則在本輪之前就存在（腳本見 PR #63，另寫在卡片指示）。
- Evidence: board 104 event 3023 與 board 97 event 3060（`agent_blocked`）；board 97 event 3069（owner 註記指出原因）。
Seen 1× (2026-10-16)

## 已升格到 AGENTS.md

**這台機器能跑的檢查，不列進 Not verified。Windows sandbox、本機的假 registry、本機目錄的 marketplace、已安裝的 `codex` CLI 都算。**
- 2026-10-17 升格到 AGENTS.md「升格規則」，證據留在這裡。
- 留在 Not verified 的項目，會把檢查推到合併那一步或下一版。本輪有兩個 PR 在合併前另外補跑；審查者為此退回三輪；留下沒驗的 Windows 項目中有三項確實是 bug。
- 寫「這裡無法驗證」之前，先試一次，並寫出失敗的指令。本輪有兩個這種說法是錯的，當晚就被更正。
- Evidence: PR #84 的 Not verified 列了 `../dep` 項目的 `normalize`、`validate`（board 64 event 1923）；PR #85 的 Not verified 寫「did not have Codex connect to the MCP server」，但 `codex` 已安裝，還是審查用的 harness（board 68 event 2271）；board 69 events 2335, 2355（審查退回：registry 對照「testable locally」）；issue #88 留言 6197554849（第 34 項「沒有 registry server」是錯的）；board 70 events 2460, 2463 與 issue #88 留言 6199489219（第 49 項「沒有離線 marketplace」是錯的）；board 112 event 3259（協調者退回：兩個 Not verified 項目都做得到）；PR #117 的 Not verified 寫「this machine has no Windows」，但 issue #88 留言 6216218590 已貼出 sandbox 實測；PR #120 的 Not verified 寫 registry `--global` 升級「only has Go tests」（board 113 event 3407）。
- Done right on 2026-10-16: PR #117 留言 6218591422（合併前跑 Windows sandbox 並貼出結果）。Done right on 2026-10-17: PR #120 留言 6219879113；PR #121 的 Verified 段落附上 Linux 與 Windows 的 Oracle 對照（board 114 events 3442, 3443）。
Seen 3× (2026-10-15, 2026-10-16, 2026-10-17)

**PR 改變升級後使用者看到的結果時，開 PR 的同一步，在下一版的發布說明 issue 記一行。只寫在 PR 的 Risks 不算紀錄。**
- 2026-10-17 升格到 AGENTS.md「升格規則」，證據留在這裡。
- 發布說明從一份清單寫成。只寫在 PR 說明裡的改變容易漏掉。下列 PR 中，只有 PR #101 的那一條進了 #88。
- Evidence: 升級影響只寫在 Risks、沒有追蹤紀錄的 PR：PR #91、PR #92（2026-10-15）；PR #94、#100、#105、#106、#109、#110、#117（2026-10-16）；PR #120、#121（2026-10-17）。唯一進了追蹤 issue 的：issue #88 留言 6203884235（PR #101）。目前沒有收集 0.4.1 發布說明的 open issue（`gh issue list --search "0.4.1"`）。
- Done right on 2026-10-16: issue #88 留言 6203884235。
Seen 3× (2026-10-15, 2026-10-16, 2026-10-17)

## 觀察（未列為教訓）

- 指示檔已超過 Claude Code 的上限。本範圍內 ARCHITECTURE.md 從 69,071 字元長到 121,598 字元，PRODUCT.md 從 21,890 字元長到 76,231 字元（`git show 34dfa71:ARCHITECTURE.md | wc -m` 對照 `0cf2bd2`）。Claude Code 警告「over the 150.0k-char total limit」（2026-10-16 為 163.5k，2026-10-17 為 216.7k）。對 agent 的影響沒有量過。
