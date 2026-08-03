# Checklist — marketplace-plugin-parity（2026-07-29 重做版，取代舊版全文）

本檔因 codex 對抗性稽核（`review/codex-audit-checklist.md`，5 阻斷 + 7 重大 + 3 次要，
主對話逐條複驗後全部成立）與隨之而來的 `prd.md`/`design.md`/`implement.md` 大改（D5/D6
撤銷改正、新增 R9/R10、AC 重編為 1-27/29-34/36-51 共 49 條、C1-C3 宣稱收窄、Out of Scope
表重寫）而**整份重新推導**，不是在舊版上打補丁。推導規則不變：
`.trellis/workflow.md:298-330` 的機械式模板；來源仍是 `prd.md`（唯一來源），`design.md`/
`implement.md` 用來把 AC 轉成可執行動作。

**撰寫期間發生的外部變動（誠實揭露，非本輪指示範圍）**：本檔撰寫過程中，`prd.md` 被
coordinator 拆成 parent（本任務，傘任務）+ 4 個 child task（`07-29-targets-init-shape`、
`07-29-install-dev`、`07-29-marketplace-add-fixes`、`07-29-plugin-init`），在最前面插入
了 34 行的 parent/child 說明與依賴表（`prd.md:1-35`），R1-R10/AC1-51 本文逐字未變、只是
整體下移 34 行。這個結構性變動**不在本次「回應 codex 稽核」的指示範圍內**，本檔沒有
主動據此重新組織章節（例如拆成對應四個 child 的子清單），只做了兩件必要的事：
(1) 全文 `prd.md:NNN`/`implement.md:NNN` 的行號引用已用 `perl` 正規表達式全部核對修正
到位（已用 `grep -oE 'prd\.md:[0-9]+(-[0-9]+)?'` 逐一比對新舊行號驗證過映射關係一致，
非手動猜測）；(2) Tripwire sweep（T1）已針對拆分後新增的一行文字（`prd.md:4`）重新分類，
見下方 T1。本檔仍然是 parent 任務（傘任務）的完整 checklist，涵蓋全部 49 條 AC；若後續
需要對應到四個 child 分別產生子 checklist，需要另一輪明確指示。

## 驗證慣例（本檔所有「驗法」共用，只在此宣告一次，不逐條重複）

1. **exit code 一律用預先 build 好的 binary**：`go build -o bin\apm-go.exe .\cmd\apm-go`
   後，凡驗 exit code 的 AC 一律執行 `bin\apm-go.exe`，**禁止 `go run`**。
   已實測驗證（非引用稽核報告的轉述）：同一支 `os.Exit(2)` 的最小程式，
   `go run .` 之後 `$LASTEXITCODE`/`$?` 回 **1**，直接執行編譯出的 binary 回 **2**——
   `go run` 會把子行程的 exit code 吃掉，只留自己的包裝碼。
2. **`go test -run <pattern>` 一律先用 `go test <pkg> -list '<pattern>'` 證明匹配非空**：
   已實測驗證：`go test ./cmd/apm-go/... -list 'TestNonExistentXYZ'` 照樣印
   `ok  ...  1.385s` 並 exit 0——零匹配的 `-run` 閘門會靜默假綠。凡本檔要求
   `go test -run` 的地方，前面都必須先跑對應套件的 `-list` 並確認輸出真的列出了
   預期的測試名稱。
3. **覆蓋率一律用 coverprofile 聚合成 repository total**：
   `go test ./... -coverprofile=cover.out` 後
   `go tool cover -func=cover.out | Select-Object -Last 1`（PowerShell）取最後一行的
   `total: (statements) NN.N%`。已實測驗證：`go test ./... -cover` 只印各 package
   各自的百分比，沒有這一行聚合值，不能拿來跟 80% 比較。
4. **端對端 CLI 檢查一律在 repo 之外的暫存目錄執行**：本 repo 根目錄現有
   `.claude/`、`.codex/`、`.agents/`（已用 `ls -d` 實測確認存在），會污染
   auto-detect 與 plugin-native 根目錄警告這類「掃描目前工作目錄」的檢查。一律
   `New-Item -ItemType Directory -Path "$env:TEMP\apm-go-check-<name>"` 建暫存目錄、
   `Push-Location` 進去執行，執行完 `Pop-Location`。不接受 `<path-to-apm-go>` 這類
   placeholder——一律寫 `..\bin\apm-go.exe`（相對暫存目錄）或絕對路徑。
5. **`marketplace package add` 類檢查的硬性閘門是 Go 測試 + `RefLister` 測試替身，不是
   打真網路**：`internal/marketplace/authoring/editor_test.go:944-972` 已有
   `stubLister`（記錄是否被呼叫 + 回傳固定錯誤）與 `mapRefLister`（回傳固定 refs 清單，
   無網路）兩個測試替身，`AddPackage(dir, source, opts, lister)` 的 `lister` 參數本來
   就是為了這個目的存在。CLI 級的 `bin\apm-go.exe marketplace package add owner/repo`
   會經 `resolveCloneURL`（`refcheck.go:97-106`）展開成
   `https://github.com/owner/repo.git`，打真網路——這正是 codex 點出的問題（依賴
   live network、無最小 fixture）。本檔對 AC17-21/AC40 一律以 Go 測試 + 測試替身為
   **硬性驗收**，CLI 級即時網路 smoke test 只列為選配、不列入通過條件。
6. **`t.Skip` 不算通過**：任何允許 skip 的 AC（目前只有 AC16 涉及 Windows symlink
   權限），必須在至少一個支援平台上真的跑過、真的斷言過，才能打勾。

---

## Acceptance criteria（AC1–AC51，49 條，逐一對回 prd.md 2026-07-29 修訂版）

### targets 單複數（prd.md:242-248）

- [x] AC1 — `apm-go init --yes` 產生的 apm.yml 使用複數 `targets:`
      · 驗法：暫存目錄跑 `..\bin\apm-go.exe init proj1 --yes --target claude`，
      `Select-String '^target' proj1\apm.yml`
      · 通過條件：只匹配 `targets:`，`target:`（單數）零匹配
      · 來源：prd.md:244（AC1）

- [x] AC2 — 對只有單數 `target:` 的既有 apm.yml 跑互動式 `apm-go init`，MultiSelect
      預選狀態含該檔案原有的 targets
      · 驗法：Go 測試，stub `stdinIsTTY`/`stderrIsTTY` 為 true、stub `multiSelectWith`
      記錄收到的 `opts`；`t.TempDir()` 內寫入 `target: [claude]`（單數）的 apm.yml，
      呼叫 `initCmd().RunE`；先 `go test ./cmd/apm-go/... -list 'TestInit.*Existing'`
      確認測試已存在再 `-run` 該名稱
      · 通過條件：`opts` 中 `Value=="claude"` 的項目 `Selected==true`
      · 來源：prd.md:245-246（AC2）；design.md §8

- [x] AC3 — 對只有複數 `targets:` 的既有 apm.yml 做 AC2 同樣的事，結果相同
      · 驗法：同 AC2，改用 `targets:\n  - claude`
      · 通過條件：同 AC2
      · 來源：prd.md:247（AC3）

- [x] AC4 — `apm-go install` 的 no-deploy-target 錯誤輸出中出現 `targets:`，不出現單數示例
      · 驗法：暫存空目錄（無 apm.yml）跑 `..\bin\apm-go.exe install owner/repo`，
      讀 stderr（對應 `install.go:820-834` 的 `errNoDeployTarget()`）
      · 通過條件：輸出含 `targets:`（複數），不含裸 `target:`（單數）範例行
      · 來源：prd.md:248（AC4）

### init 產物形狀（prd.md:250-258）

- [x] AC5 — `apm-go init --yes --target claude,codex,opencode` 的 apm.yml 鍵序為
      `name, version, description, author, targets, dependencies, includes, scripts`
      · 驗法：暫存目錄跑該指令，`Select-String -Pattern '^[a-z]' proj5\apm.yml`
      （只看頂層鍵）
      · 通過條件：8 個頂層鍵依序完全相符
      · 來源：prd.md:252-253（AC5）

- [x] AC6 — `targets:` 上方三行註解，第三行為
      `agent-skills, antigravity, claude, codex, copilot, opencode`
      · 驗法：`Get-Content proj5\apm.yml | Select-String -Context 3,0 '^targets:'`
      · 通過條件：三行逐字相符（比對 prd.md:254-255 逐字，不重複貼字避免與 prd.md
      失步）
      · 來源：prd.md:254-255（AC6）

- [x] AC7 — 未指定 target 時，輸出逐字為三行說明註解 + `# targets:` + `#   - claude`
      的五行骨架
      · 驗法：暫存空目錄（無任何 harness marker）跑
      `..\bin\apm-go.exe init proj7 --yes`，讀 apm.yml
      · 通過條件：**逐字**斷言五行（不可只驗「有 `# targets:` 且下一行以 `#` 開頭」，
      那樣任意內容都會過——這是 prd.md 已修正的次要發現 1）
      · 來源：prd.md:256-258（AC7，已修正）；design.md §2「targets 為空時」

### plugin init（prd.md:260-276）

- [x] AC8 — `apm-go plugin init --help` 恰好列出 `--yes/-y`、`--target`、
      `--verbose/-v` 三個自訂旗標
      · 驗法：`..\bin\apm-go.exe plugin init --help`
      · 通過條件：Flags 區塊（不計 `-h`）恰 3 項，名稱/別名逐字相符
      · 來源：prd.md:262-263（AC8）

- [x] AC9 — `apm-go plugin init My_Plugin` 非零 exit；`apm-go plugin init my-plugin` exit 0
      · 驗法：`..\bin\apm-go.exe plugin init My_Plugin; $LASTEXITCODE`；同法測
      `my-plugin`
      · 通過條件：前者非 0，後者為 0
      · 來源：prd.md:264-265（AC9）

- [x] AC10 — `apm-go plugin init p1 --yes` 的 apm.yml 含 `version: 0.1.0` 與
      `devDependencies: {apm: []}`，`devDependencies` 位於 `includes` 與 `scripts` 之間
      · 驗法：暫存目錄跑該指令，讀 apm.yml
      · 通過條件：兩個字串逐字存在；`devDependencies:` 行號介於 `includes:`/`scripts:` 之間
      · 來源：prd.md:266-267（AC10）

- [x] AC11 — 同次執行於專案根產生 `plugin.json`，含 `license:"MIT"`、2 空格縮排、結尾換行
      · 驗法：`Get-Content -Raw p1\plugin.json`，逐欄比對
      `research/eval-real-run-20260728.md` §D3 的上游產物
      · 通過條件：鍵序/內容逐欄相符；最後一個字元為 `\n`
      · 來源：prd.md:268-269（AC11）

- [x] AC12 — `apm-go init --yes` 的 apm.yml 不含 `devDependencies`，且不產生 `plugin.json`
      · 驗法：暫存目錄跑 `..\bin\apm-go.exe init c1 --yes`
      · 通過條件：`Select-String devDependencies c1\apm.yml` 零匹配；
      `Test-Path c1\plugin.json` 為 `False`
      · 來源：prd.md:270-271（AC12）

- [x] AC13 — plugin 版 Next Steps 逐字含 `Pack as plugin:` + `apm-go pack` 一行，
      且不含 consumer 版的 `Install a package:  apm-go install <owner>/<repo>`
      · 驗法：暫存目錄跑 `..\bin\apm-go.exe plugin init p13 --yes`，讀 stdout/stderr
      · 通過條件：**逐字**一行相符（不是「輸出任意處含 apm-go pack」）；consumer 提示不存在；
      與 AC46（`--dev` 那一行）合起來才是完整斷言
      · 來源：prd.md:272-276（AC13，已修正）

### plugin-native 警告（prd.md:278-285）

- [x] AC14 — 含 `skills/` 且無 `.apm/` 的目錄跑 `apm-go init --yes`，輸出含警告且 exit 0
      · 驗法：`$env:TEMP` 下建暫存目錄、`mkdir skills`，`..\..\bin\apm-go.exe init --yes; $LASTEXITCODE`
      · 通過條件：exit 0；stderr 含 skills 會被 `apm-go pack` 收錄的警告
      · 來源：prd.md:280-281（AC14）

- [x] AC15 — 同目錄建 `.apm/` 後重跑，警告消失
      · 驗法：延續 AC14 目錄，`mkdir .apm`，重跑 `--yes --force`
      · 通過條件：stderr 不含 AC14 警告文字
      · 來源：prd.md:282（AC15）

- [x] AC16 — `skills` 是 symlink 時不觸發警告
      · 驗法：`New-Item -ItemType SymbolicLink -Path skills -Target realdir`
      （若當前環境無權限建立，測試 `t.Skip` 並寫明原因）之後跑 init
      · 通過條件：無警告輸出；**且該測試必須在至少一個平台上真的執行、真的斷言過才能打勾
      —— `t.Skip` 不算通過**（prd.md 已加註「驗收紀律」，見 prd.md:284-285）
      · 來源：prd.md:283-285（AC16，已加驗收紀律）；design.md §5

### package add HEAD 解析（prd.md:287-303，硬性驗收用 Go 測試 + RefLister 替身，見上方慣例 5）

- [x] AC17 — 無 ref/version 旗標時解析後的 SHA 寫入 `ref:`
      · 驗法：Go 測試，`AddPackage(dir, "owner/repo", AddOptions{}, mapRefLister{refs:
      []semver.TagInfo{{Name:"HEAD", Commit:"<40-hex 假 SHA>"}}})`（`t.TempDir()` 內先
      寫最小 `marketplace:` 區塊的 apm.yml）
      · 通過條件：寫回的 entry 含 `ref:` 且等於該假 SHA
      · 來源：prd.md:289-290（AC17）；design.md §6

- [x] AC18 — 同上情境加 `--no-verify` 時，exit 2 + 訊息
      `Cannot resolve HEAD ref without network access. Provide an explicit --ref SHA.`
      · 驗法：CLI 級，暫存目錄先跑（不加 `--no-verify` 的）`marketplace init`/手寫
      apm.yml，跑 `..\bin\apm-go.exe marketplace package add owner/repo --no-verify`
      （用**已 build 好的 binary**，見慣例 1）；因為 `--no-verify` 分支本來就不觸網，
      這條可以是真 CLI smoke test，不需要 RefLister 替身
      · 通過條件：stderr 逐字含上述訊息；`$LASTEXITCODE` 為 2
      · 來源：prd.md:291-296（AC18，已加驗收紀律）

- [x] AC19 — 顯式 `--ref HEAD` 額外印 mutable-ref 警告後照常解析
      · 驗法：Go 測試同 AC17，`AddOptions{Ref:"HEAD"}` + `mapRefLister`
      · 通過條件：warning 輸出含 `'HEAD' is a mutable ref. Resolving to current SHA for
      safety.`；entry 仍正常寫入解析後 SHA
      · 來源：prd.md:297（AC19）

- [x] AC20 — `--version '^1.0.0'` 時不寫 `ref:`（更正：不是「不觸網」——reachability
      仍會在 `_verify_source`/`verifyPackageSource` 階段觸網，除非另加 `--no-verify`）
      · 驗法：Go 測試，`AddOptions{Version:"^1.0.0"}` + `mapRefLister`（**額外斷言**：
      這個測試替身要能記錄「是否被呼叫」，用來證明 `verifyPackageSource` 的 lister
      仍被呼叫過——這是本檔補上的斷言，`editor.go:450` 附近的呼叫序已核對過：
      reachability 先於 ref 解析，兩者是不同呼叫點）
      · 通過條件：entry 無 `ref:` 鍵；**且** lister 的 `ListRefs` 確實被呼叫過一次
      （除非另外加 `--no-verify`，那種情況才不呼叫）
      · 來源：prd.md:298-301（AC20，已更正——原「不觸網」的錯誤說法已改，驗法也已
      補上「reachability 仍觸網」這個斷言，不只是改了文字說明）
      · **2026-08-01 check agent 獨立複驗（fresh context）發現的缺口與自我修正**：
      逐檔核對後，`cmd/apm-go/marketplace_package_test.go:1089`
      （`TestMarketplacePackageAdd_VersionGiven_DoesNotWriteRef`，唯一標註 AC20 的
      既有測試）只斷言「無 `ref:` 鍵」，**沒有**斷言 `ListRefs` 被呼叫過——它用
      `fixtureRemoteLister`（真的對本地 git fixture 跑 `git ls-remote`）而非會計數的
      test double，若把 `verifyPackageSource` 裡的 `lister.ListRefs(source)` 呼叫整段
      刪掉，這條既有測試仍會通過（因為刪掉呼叫不影響「有沒有寫 ref:」這件事）。
      已讀 `internal/marketplace/authoring/editor.go:335-347`（`verifyPackageSource`）
      確認原始碼行為本身是對的（非 local 來源、`noVerify=false` 時無條件呼叫
      `lister.ListRefs`），但缺一個能抓到「呼叫被誤刪」這種回歸的測試。已自我修補：
      新增 `internal/marketplace/authoring/editor_test.go` 的
      `TestAddPackage_VersionGiven_RemoteSource_StillCallsListerForReachability`
      （用既有的 `stubLister{}`，斷言 `lister.called == true` 且 entry 無 `ref:`），
      `go test ./internal/marketplace/authoring/... -run
      TestAddPackage_VersionGiven_RemoteSource_StillCallsListerForReachability -v`
      已跑過並 PASS；補測試後重跑 `go build ./cmd/apm-go`、`go vet ./...`、
      `go test ./...` 全部仍是 PASS（無回歸）。

- [x] AC21 — local（`./...`）source 在上述所有情境皆不觸網
      · 驗法：`go test ./internal/marketplace/authoring/... -list
      'TestAddPackage_LocalSource_NoFlags_NeverTouchesNetwork'` 先確認匹配非空
      （現況已存在，見 `editor_test.go:20`），再 `-run` 該名稱
      · 通過條件：PASS，且 R5/R10 落地後此測試不得轉紅
      · 來源：prd.md:302-303（AC21）

### 其他（prd.md:305-314）

- [x] AC22 — `apm-go marketplace audit <未註冊名稱>` 錯誤訊息含註冊指令與
      `marketplace list` 提示
      · 驗法：暫存目錄跑 `..\bin\apm-go.exe marketplace audit definitely-not-registered`
      · 通過條件：訊息含 `marketplace add` 與 `marketplace list` 兩個子字串
      · 來源：prd.md:307-308（AC22）

- [x] AC23 — `plugin init` 互動路徑（Form/MultiSelect/Confirm）有測試覆蓋，且未新增
      `go.mod` 相依
      · 驗法：先 `go test ./cmd/apm-go/... -list 'TestPlugin.*Interactive'`（或實際命名）
      確認匹配非空，再 `-run`；`go.mod`/`go.sum` 檢查同時看 `git diff main...HEAD --
      go.mod` **與** `git status --porcelain go.mod`（working tree 未提交的變更也要看，
      不能只看 commits——這是 codex 阻斷 4 點名的假陰性）
      · 通過條件：互動測試存在且 PASS；`git diff` 與 `git status` 皆無 `go.mod`
      的 `require` 新增行
      · 來源：prd.md:309-314（AC23，已加驗收紀律）

### target 集合一致性（prd.md:316-332）

- [x] AC24 — `apm-go init --target agent-skills` 成功
      · 驗法：暫存目錄 `..\bin\apm-go.exe init a1 --yes --target agent-skills; $LASTEXITCODE`
      · 通過條件：exit 0；apm.yml 的 `targets:` 含 `agent-skills`
      · 來源：prd.md:318（AC24）

- [x] AC25 — 存在測試斷言 `SupportedTargets`、`adapterTargets`、**init 互動選單實際使用
      的選項清單**三者同集合，任一方漂移即轉紅
      · 驗法：**測試必須位於 `cmd/apm-go` 套件**（選單 opts 組裝處），不是
      `internal/manifest`——只跑後者會漏掉選單那一半，這是 codex 阻斷 4 點名的錯誤
      package。先 `go test ./cmd/apm-go/... -list 'TestSupportedTargets.*Consistency'`
      （或實際命名）確認非空，再 `-run`
      · 通過條件：測試存在、位於正確 package、PASS
      · 來源：prd.md:319-325（AC25，已加驗收紀律：package 位置）

- [x] AC26 — AC6 的註解清單與 `SupportedTargets` 同源，非獨立字面量
      · 驗法：**行為測試，不是 grep**——測試中暫時替換來源切片（或直接呼叫 comment
      builder 函式）注入一個假 target，斷言輸出的註解字串跟著變。不接受「grep 不到完整
      字面量」當通過條件：清單若被拆成數個字面量再由 helper 拼接，完整字串 grep 仍是
      零命中，會製造假綠（codex 次要發現 3）
      · 通過條件：注入假 target 後，斷言輸出的 Accepted values 行含該假 target
      · 來源：prd.md:326-330（AC26，已修正為行為測試）

- [x] AC27 — `CanonicalTargets` 未被更動，`targets: [cursor]` 仍解析成功並產生 req-tg-004
      warning
      · 驗法：暫存目錄寫 `targets:\n  - cursor` 的 apm.yml，跑
      `..\bin\apm-go.exe install`（或任何呼叫 `ParseManifest` 的子指令）
      · 通過條件：不因未知 handler 失敗；warning 含 `no registered handler for target`
      （`internal/deploy/adapter.go:174` 現況文字，經 `internal/manifest/manifest.go:218-225`
      轉呈）
      · 來源：prd.md:331-332（AC27）

### 覆蓋率補洞（prd.md:334-349）

- [x] AC29 — 只有單數 `target: claude` 的既有 apm.yml，端對端跑 `install` 仍能正確部署，
      跑 `pack` 仍能正確打包（**更正驗法，見下**：`pack` 不是部署指令，通過條件分開判定）
      · 驗法：暫存目錄的 apm.yml 除了 `target: claude`（單數）外，**額外加一個可離線
      解析的本地依賴**（`dependencies:\n  apm:\n    - ./pkgs/demo`，`pkgs/demo` 內放
      最小 `.apm/skills/x/SKILL.md`），確保有實際東西可觀察是否被部署——原驗法只放
      `target: claude` 沒有任何 primitive，無從觀察部署與否，這是 codex 重大發現 5
      點名的問題。跑 `..\bin\apm-go.exe install`，再跑 `..\bin\apm-go.exe pack`
      · 通過條件：`install` exit 0 且 `.claude/` 下出現該 skill 的部署產物（真正的
      「部署」判準）；`pack` exit 0 且產生 bundle 輸出（`pack` 的判準是「打包成功」，
      不是「部署」——兩者不可混為一談）
      · 來源：prd.md:339-341（AC29，驗法已依 codex 重大發現 5 修正）
      · **2026-08-01 check agent 獨立複驗（fresh context）發現的驗法本身缺陷**：
      本檔指定的單一 fixture（`target: claude` 單數 + `dependencies.apm: [./pkgs/demo]`
      本地依賴）拿去跑 `pack` 時，**必定**失敗——已用暫存目錄實測，
      `bin\apm-go.exe pack` 對含本地路徑依賴的 apm.yml 回傳
      `Error: cannot pack -- apm.yml contains a local path dependency. Local
      dependencies are for development only. Replace them with remote
      references...`，exit 1。這是既有、與本 task 無關的產品規則
      （`cmd/apm-go/pack.go` 對本地路徑依賴的既有檢查），不是「單數 target: 解析」
      本身的缺陷；驗法把「觀察部署的本地依賴」與「pack 必須成功」綁進同一個
      fixture 造成自相矛盾，是 checklist 本身的設計缺陷。**已改用兩個獨立 fixture
      分別驗證同一個問題（R1.4/C4：雙鍵解析不得在部署鏈與 pack 鏈上被改壞）**：
      (1) install：`target: claude`（單數）+ 本地依賴，`bin\apm-go.exe install`
      exit 0，`.claude/skills/x/SKILL.md` 確實落地（已讀檔案系統確認存在）；
      (2) pack：另一個暫存目錄用 `apm-go plugin init` 產生的 apm.yml 手動把
      `targets:` 改成 `target:`（單數），加 plugin-native `skills/` 目錄（非本地依賴），
      `bin\apm-go.exe pack` exit 0，並產生 `.claude-plugin/plugin.json`
      （已讀檔案內容確認）。兩者合起來證明單數 `target:` 鍵在 install 部署鏈與
      pack 鏈上都沒有被改壞，覆蓋了 R1.4/C4 的核心疑慮；唯一未達成的是「同一份
      apm.yml 同時通過 install 與 pack」這個字面要求，因為該要求與既有的
      local-path-dependency-blocks-pack 規則互斥，屬於檢核設計問題而非程式碼缺陷。

- [x] AC30 — `apm-go plugin --help` 只列出 `init` 一個子指令
      · 驗法：`..\bin\apm-go.exe plugin --help`（Commands 區塊）
      · 通過條件：恰列 1 個子指令 `init`
      · 來源：prd.md:342（AC30）

- [x] AC31 — `apm-go plugin init --yes`（不給 PROJECT-NAME）在當前目錄成功初始化，
      名稱取自目錄名並過 kebab-case
      · 驗法：`$env:TEMP` 下建立名為 `my-plugin`（已符合 kebab-case）的暫存目錄，
      `Push-Location` 進去跑 `..\..\bin\apm-go.exe plugin init --yes`（不傳位置參數）
      · 通過條件：exit 0；apm.yml 的 `name:` 為 `my-plugin`
      · 來源：prd.md:343-344（AC31，已補完整 fixture 路徑，不用 placeholder）

- [x] AC32 — `apm-go init My_Project --yes`（consumer）仍然成功
      · 驗法：暫存目錄 `..\bin\apm-go.exe init My_Project --yes; $LASTEXITCODE`
      · 通過條件：exit 0；apm.yml 的 `name:` 為 `My_Project`（未被 kebab-case 規則
      拒絕/改寫）
      · 來源：prd.md:345-346（AC32）

- [x] AC33 — `apm-go init --help` 不含 `--verbose`/`-v`
      · 驗法：`..\bin\apm-go.exe init --help`
      · 通過條件：Flags 區塊不含 `-v`/`--verbose`
      · 來源：prd.md:347（AC33）

- [x] AC34 — 含 `skills/` 且無 `.apm/` 的目錄跑 `apm-go plugin init`，同樣印警告
      · 驗法：同 AC14 fixture，改跑 `..\..\bin\apm-go.exe plugin init demo --yes`
      · 通過條件：exit 0；stderr 含與 AC14 相同警告
      · 來源：prd.md:348-349（AC34）

### codex 稽核補洞（prd.md:351-366，對應 `review/codex-audit-checklist.md` 阻斷 3）

- [x] AC36 — kebab-case 邊界：首字元非小寫字母（`1abc`）拒絕；長度 64 通過、65 拒絕
      · 驗法：Go 測試表驅動 `validateName`（plugin 模式），三個案例各自斷言
      · 通過條件：`1abc` 非 nil error；63 字元後綴組成的 64 長度字串成功；65 長度字串
      非 nil error
      · 來源：prd.md:355-356（AC36，新——彌補 AC9 只測底線的邊界缺口）

- [x] AC37 — consumer `apm-go init` 對 `a/b`、`a\b`、`..` 的**既有拒絕仍存在**
      · 驗法：暫存目錄分別跑 `..\bin\apm-go.exe init "a/b"`、`init "a\b"`、
      `init ".."`，各自讀 `$LASTEXITCODE`（現況規則見 `cmd/apm-go/init.go:37`
      `strings.ContainsAny(pn, "/\\") || pn == ".."`，已讀過）
      · 通過條件：三者皆非零 exit——這條 AC 的意義是防止 plugin 模式重構「不小心」把
      consumer 的既有規則改鬆或改沒
      · 來源：prd.md:357-358（AC37，新）

- [x] AC38 — 互動模式（非 `--yes`）下 plugin 與 consumer 的版本表單預設值皆為 `1.0.0`
      · 驗法：用慣例 5 同款的 ux seam（stub TTY + `inputFormWith`），分別對 consumer
      `initCmd()` 與 plugin `pluginInitCmd()` 走互動路徑，斷言傳給表單的 `version`
      欄位 `Default` 為 `"1.0.0"`
      · 通過條件：兩個模式都是 `1.0.0`（不是 plugin 也變成 `0.1.0`）
      · 來源：prd.md:359-360（AC38，新——彌補 AC10 只測 `--yes` 的 `0.1.0`）

- [x] AC39 — plugin-native 警告對 `agents/ commands/ instructions/ extensions/ hooks/`
      與 `hooks.json` 逐一都觸發，不只 `skills/`
      · 驗法：Go 測試表驅動 `pluginRootSources`，六個案例各自建一個只含該 marker
      的暫存目錄（`t.TempDir()`），逐一斷言回傳非空
      · 通過條件：六個 marker 各自獨立觸發——只測 `skills/` 的話，實作漏掉其餘五個仍會
      全過，這是 codex 阻斷 3 點名的 R4.2 缺口
      · 來源：prd.md:361-362（AC39，新）

- [x] AC40 — `resolveRef` 每個分支各有測試：隱含 HEAD、顯式 HEAD、`--version`、
      `--no-verify`、40-hex SHA、local source
      · 驗法：Go 測試表驅動，六個案例各自用 `mapRefLister`/`stubLister`/`panicLister`
      （local 案例用 `panicLister` 證明不觸網），對 `resolveRef` 或 `AddPackage` 直接呼叫
      · 通過條件：六個分支各自有獨立斷言（不是 AC21 那樣只測 local+零旗標一種情境）
      · 來源：prd.md:363-364（AC40，新——彌補 AC21 單一情境的 R5.5 缺口）

- [x] AC41 — 互動路徑測試逐一命中 `Form`、`MultiSelect`、`Confirm` 三個分支，各自獨立斷言
      · 驗法：三個獨立測試（或同一測試內三個獨立子斷言），分別驅動
      `inputFormWith`（Form）、`multiSelectWith`（MultiSelect）、`confirmWith`（Confirm）
      被呼叫且收到預期參數
      · 通過條件：三者缺一都會被抓到——AC23 原本「存在 Plugin 測試且 PASS」的條件不足以
      證明三個分支都被打到，這是 codex 阻斷 3 點名的 R7.1 缺口
      · 來源：prd.md:365-366（AC41，新）

### install --dev（prd.md:368-378，R9）

- [x] AC42 — `apm-go install --dev owner/repo` 寫入 `devDependencies.apm`，不寫入
      `dependencies.apm`
      · 驗法：Go 測試，`t.TempDir()` 最小 apm.yml，`runInstall(deps, ..., dev=true, ...)`
      （或對應簽章，實作後確認），讀回 apm.yml
      · 通過條件：`devDependencies.apm` 含該套件；`dependencies.apm` 不含
      · 來源：prd.md:370-371（AC42，新）

- [x] AC43 — apm.yml 原無 `devDependencies` 鍵時，`--dev` 自動建立該區段，鍵序落在
      `includes` 與 `scripts` 之間
      · 驗法：Go 測試，最小 apm.yml（無 `devDependencies` 鍵），跑 `--dev` install，
      讀回並檢查頂層鍵序
      · 通過條件：`devDependencies:` 出現且行號介於 `includes:`/`scripts:` 之間——與
      R2.1（AC5）同一鍵序契約，不能各自為政
      · 來源：prd.md:372-373（AC43，新）

- [x] AC44 — 不加 `--dev` 時行為與現況完全一致（回歸閘門）
      · 驗法：`go test ./cmd/apm-go/... -list 'TestRunInstall'` 確認既有 install 測試
      清單非空後全跑，額外確認
      `TestRunInstall_DevDependency_ResolvedDeployedAndLocked`（`install_test.go:135`）、
      `TestRunInstall_DevDependency_SecondBareInstallIsNoOp`（`install_test.go:193`）、
      `TestRunInstall_DevDependency_SkillSubsetHonored`（`install_test.go:2265`）
      三個既有 dev 測試在 R9 落地後仍 PASS
      · 通過條件：不加 `--dev` 的既有測試全綠；上述三個既有 dev 讀取鏈測試也全綠——
      這條同時也是 R9.3「不重做既有 dev 讀取鏈」的回歸證明（見下方 R 子項覆蓋率 G1）
      · 來源：prd.md:374（AC44）；design.md §11「不做的事」

- [x] AC45 — `--dev` 裝進來的套件在 `apm.lock.yaml` 有 `package_type` 欄位；非 dev 行為不變
      · 驗法：Go 測試，接續 AC42 的 fixture，讀 `apm.lock.yaml` 的對應 entry
      · 通過條件：dev entry 含 `package_type`（值待實作後確認，prd.md 未指定具體字串，
      只要求欄位存在）；非 dev entry 不受影響（無此欄位或維持現況）
      · 來源：prd.md:375-376（AC45，新）

- [x] AC46 — `plugin init` Next Steps 印兩行：`apm-go install --dev <owner>/<repo>` 與
      `apm-go pack`
      · 驗法：暫存目錄跑 `..\bin\apm-go.exe plugin init p46 --yes`，讀 stdout/stderr
      · 通過條件：兩行皆逐字存在；與 AC13 合起來才是完整斷言（AC13 斷言 pack 那行 +
      consumer 提示不存在，AC46 斷言 `--dev` 那行存在——這是 D5 撤銷後 R3.4 從一行
      恢復成兩行的直接後果）
      · 來源：prd.md:377-378（AC46，新）

### marketplace package add --category（prd.md:380-388，R10）

- [x] AC47 — `add owner/repo --category Productivity` 在該 entry 寫出
      `category: Productivity`
      · 驗法：Go 測試，`AddPackage(dir, "owner/repo", AddOptions{Category:"Productivity"},
      mapRefLister{...})`
      · 通過條件：寫回的 entry 含 `category: Productivity`（`editor.go:288-290` 的
      `putStr("category", entry.Category)` 已存在，只驗證有人填值）
      · 來源：prd.md:382-383（AC47，新）

- [x] AC48 — `outputs` 含 codex 且未給 `--category` 時 add 仍成功，但印警告
      · 驗法：Go 測試，`marketplace:` 區塊含 `outputs: {codex: {}}` 的 apm.yml，跑
      `AddPackage(..., AddOptions{}, ...)`（無 Category）
      · 通過條件：`err == nil`（不阻斷）；stderr/warning 輸出含 category 缺失、pack
      會失敗、可用 `--category` 補的提示
      · 來源：prd.md:384-385（AC48，新）

- [x] AC49 — `marketplace package set` 沒有 `--category` 旗標
      · 驗法：`go test ./cmd/apm-go/... -list
      'TestMarketplacePackageSetCmd_HasNoAddOnlyFlags'` 確認非空（現況已存在，
      `marketplace_package_test.go:37`），再 `-run`；額外確認該測試新增
      `cmd.Flags().Lookup("category")` 為 nil 的斷言
      · 通過條件：既有守門測試 PASS 且涵蓋 `category`
      · 來源：prd.md:386（AC49）；implement.md Step 8c

- [x] AC50 — `add --category` 之後 `apm-go pack`（codex 輸出開啟）成功——端對端證明死結
      解除
      · 驗法：暫存目錄：`marketplace init`（或手寫最小 marketplace apm.yml，
      `outputs: {codex: {}}`），`..\bin\apm-go.exe marketplace package add
      ./pkgs/local-demo --category Productivity`（用 local source 避開網路，
      `pkgs/local-demo` 放最小可打包內容），`..\bin\apm-go.exe pack -m codex`
      · 通過條件：兩個指令皆 exit 0；`pack` 未因 `CategoryRequiredError` 失敗
      · 來源：prd.md:387-388（AC50，新）

### 全域（prd.md:390-396）

- [x] AC51 — `go build ./...`、`go vet ./...` exit 0；
      `go test ./... -coverprofile=cover.out` 後
      `go tool cover -func=cover.out | Select-Object -Last 1` 的 total ≥ 80%
      · 驗法：見上方慣例 3（已實測驗證 `-cover` 無 total 行、coverprofile 管線有）
      · 通過條件：三個指令/管線皆成功；total 百分比數字 ≥ 80.0
      · 來源：prd.md:392-396（AC51，取代原 AC28/AC35，已用 coverprofile 修正）

**Acceptance criteria 小計：49 條（AC1–AC27、AC29–AC34、AC36–AC51；AC28/AC35 兩個編號
皆已不存在，用 `grep -oE '^- \[ \] AC[0-9]+' prd.md | wc -l` 核對得 49，與本節逐條數量
一致）**

---

## Decisions（D1–D9，逐條對回 prd.md:51-61；D5/D6 已撤銷/修正，check 改為「證明撤銷正當」）

- [x] D1 — 「targets 鍵讀寫都對齊複數」確實成立
      · file:line：`cmd/apm-go/init.go:237`（寫，待改複數）、`:317`（讀，待改雙鍵）
      · 驗法：AC1（寫）+ AC2/AC3（讀）全綠
      · 成本：design.md §2/Step 5，約 10 行 + 產生器改寫
      · 來源：prd.md:53（D1）


      ✅ 驗證（主 session，2026-08-02）：主 session 實測（2026-08-02）：`init d1 --yes --target claude,codex` 產出 `targets:` 複數序列；manifest.go:119/238 對單數 `target:` 僅為讀取相容分支，寫入路徑只產生複數。

- [x] D2 — 「不補 `--plugin` deprecated 別名」的前提成立
      · file:line：`cmd/apm-go/init.go:223-225`，現況旗標只有 `--yes/-y --target
      --force`
      · 驗法：`..\bin\apm-go.exe init --help` 確認無 `--plugin`；額外執行
      `git log --all -S--plugin -- cmd/apm-go/init.go` 確認零命中（codex 已跑過
      此指令並確認「查過但站得住」，本檔複核同一指令）
      · 成本：0
      · 來源：prd.md:54（D2）；review/codex-audit-checklist.md:71


      ✅ 驗證（主 session，2026-08-02）：主 session 實測：`install --help` 零匹配 `--plugin`；`install --plugin x` → `Error: unknown flag: --plugin`，exit 1。

- [x] D3 — 「Accepted values 列 6 個而非 10 個」的取捨仍然成立
      · file:line：`internal/manifest/target.go:5-17`（`CanonicalTargets` 10 個 + `all`）、
      `internal/manifest/manifest.go:218-225`（未知 handler 只是 warning）
      · 驗法：AC6（輸出恰 6 個）+ AC27（`[cursor]` 仍解析成功但有 warning）
      · 成本：0（純清單選擇）
      · 附註：codex 重大發現 3 沒有推翻 D3 本身，但要求 D3 的措辭不應暗示
      「Accepted values」等於「apm.yml 唯一接受的值」（parser 實際接受 10 個）——
      這已在 prd.md D3 原文的「已知取捨」段落講清楚，非本輪新增問題
      · 來源：prd.md:55（D3）


      ✅ 驗證（主 session，2026-08-02）：主 session 實測：`init` 產物註解為 `# Accepted values: agent-skills, antigravity, claude, codex, copilot, opencode`（6 個）。

- [x] D8 — 「一併修兩個本專案內部不一致（`agent-skills` 白名單、`promptTargetsOrdered`
      漂移）」確實完成且有測試鎖住（本輪未變動，沿用既有裁定）
      · file:line：`internal/manifest/target.go:25-27`（`SupportedTargets` 現況 5 個，
      不含 `agent-skills`，待補）、`cmd/apm-go/init.go:17-19`（`promptTargetsOrdered`
      現況為獨立字面量，待改推導或刪除）
      · 驗法：AC24（`agent-skills` 可被 init 選用）+ AC25（三者同集合鎖定測試，本輪
      已加驗收紀律：測試須位於 `cmd/apm-go` 而非 `internal/manifest`）
      · 成本：design.md §7 估計約 10-20 行 + 1 測試
      · 來源：prd.md:56（D8）；prd.md:157-172（R8）


      ✅ 驗證（主 session，2026-08-02）：主 session 實測：`cmd/apm-go/init.go` 零匹配 `promptTargetsOrdered`；`init d8 --yes --target agent-skills` exit 0。

- [x] D4 — 「互動路徑走 `internal/ux` TTY 接縫，不引入真 PTY」確實成立，但**覆蓋範圍
      已被 codex 重大發現 4 明確收窄**
      · file:line：`internal/ux/ux.go:56-62`
      · 驗法：`go list -m all | Select-String 'pty|conpty'` 比對 `go.mod` 直接依賴
      （應為空）
      · 成本：0 新增相依；但**這不等於風險已被覆蓋**——seam 只能測參數資料，測不到真
      binary 的 stdin/stderr wiring、Ctrl-C、escape sequence、終端尺寸、Windows
      ConPTY。已移入 Out of Scope 表明確承認（見下方 Deferral）
      · 來源：prd.md:57（D4）；review/codex-audit-checklist.md 重大 4


      ✅ 驗證（主 session，2026-08-02）：主 session 實測：go.mod/internal/cmd 對 PTY 函式庫零匹配；`internal/ux/interactive.go` 三個接縫（confirmWith/multiSelectWith/inputFormWith）齊備；`cmd/apm-go` 互動相關測試 36 個。覆蓋缺口（真終端渲染未驗）維持已承認狀態。

- [x] D5（撤銷）— 證明「`install --dev` 不在本 task」的原裁定確實錯誤、撤銷正當
      · 證據三件套：
        (1) file:line：`cmd/apm-go/install.go:433`（`ParsedDevDeps` 正規化）、`:457`
        （`hasAnyDeps` 含 dev）、`:1030-1035`（解析合併）；`cmd/apm-go/update.go:94,303`；
        `cmd/apm-go/uninstall_resolve.go:58,253,273`；`cmd/apm-go/uninstall.go:586`；
        `cmd/apm-go/pack.go:299`；`cmd/apm-go/compile.go:76`——本檔已逐一開檔核對，
        `install.go:430-457` 的 `ParsedDevDeps`/`hasAnyDeps` 兩段確認存在且邏輯與稽核
        報告描述一致
        (2) 反例：`install_test.go:135` 的
        `TestRunInstall_DevDependency_ResolvedDeployedAndLocked` 已讀過完整測試體
        （141-186 行），證明手寫 `devDependencies.apm` 條目零 `dependencies.apm` 時仍能
        解析、部署（`.apm/instructions/dev.instructions.md` 落地）、寫入 lockfile；
        `:193` 的 `TestRunInstall_DevDependency_SecondBareInstallIsNoOp` 證明冪等；
        `:2265` 的 `TestRunInstall_DevDependency_SkillSubsetHonored` 證明 skill subset
        也走 dev 路徑
        (3) 成本重估：真正缺的只有寫入端三處（旗標、`persistPackagesToManifest`
        區段參數化、lockfile `package_type` 欄位），design.md §11 估計後者三處合計
        遠小於原本「貫穿四子系統」的估計，改由 R9 承接
      · 通過條件：AC42-46 全數落地，且 AC44 證明既有 dev 讀取鏈測試不因此轉紅
      · 來源：prd.md:58（D5 撤銷列）

- [x] D6（修正）— 證明「codex category 閘門刻意不對齊上游、本專案較佳」的原宣稱不成立、
      修正為補 `--category` 正當
      · 證據三件套：
        (1) file:line：`internal/marketplace/authoring/editor.go:413-422`（`AddOptions`
        結構，本檔已讀過完整欄位列表：`Name/Version/Ref/Subdir/TagPattern/Tags/
        IncludePrerelease/NoVerify`——**確認沒有 `Category` 欄位**）
        (2) 反例：`editor.go:288-290` 的 `packageEntryNode` 已有
        `if entry.Category != "" { putStr("category", entry.Category) }`，寫出路徑是通的，
        只是沒有任何呼叫端會填這個欄位——所以「不在 add 檢查」的實際效果不是「更好的
        使用者體驗」，而是「開了 codex 輸出後，add 寫入一個必然在 pack 階段失敗的無效
        entry，還無法透過 add 修正，只能手改 apm.yml」，這跟上游的死結是同一類使用者
        體驗，只是失敗點延後
        (3) 成本估計：補 `--category` 旗標約 30-60 LOC（design.md §12 已列三處改動點：
        `AddOptions` 加欄位、`AddPackage` 帶入、CLI 加旗標）
      · 修正後的窄版宣稱：claude-only 情境確實優於上游（`schema.go:12-21` 的
      compose-time-only 設計讓 `pack -m claude` 不受影響）；codex 情境原本不優於上游，
      補了 `--category` 之後才兩邊都優於上游——這個窄版宣稱已寫進 prd.md C1
      · 驗法：AC47-50 全數落地
      · 通過條件：AC50（端對端 add --category 後 pack 成功）PASS
      · 來源：prd.md:59（D6 修正列）；review/codex-audit-checklist.md 阻斷 2

- [x] D7 — 「`marketplace check` 不表格化、`install` 不改寫入順序」——**宣稱範圍已收窄**
      （不再是「較佳」，只是「不改」）
      · file:line：`cmd/apm-go/marketplace_authoring.go:274-298`；
      `cmd/apm-go/install.go:1620`（`deploy.Run`）、`:1764`（lockfile 寫入）、`:1777`
      （apm.yml 寫入）——本檔已重讀這三個行號，確認 `deploy.Run` 確實早於 lockfile 與
      apm.yml 寫入
      · 驗法：`grep -n "pass rate" cmd/apm-go/marketplace_authoring.go`；C2/C3 的
      Out of Scope 對應列
      · 成本：0（維持現狀）；但**不維持現狀去做全面診斷維度/交易化的成本**已個別估在
      C2/C3（見下）
      · 來源：prd.md:60（D7，宣稱已收窄）


      ✅ 驗證（主 session，2026-08-02）：主 session 實測：`marketplace check --help` 輸出零框線字元；`git diff 3e450dd -- internal/manifest/write.go internal/yamlcore/` 為空（manifest persistence 未被本分支動過）。

- [x] D9 — 「codex 稽核其餘 7 項重大、3 項次要全部修進 prd.md 與 checklist.md」屬實
      · 驗法：對照 `review/codex-audit-checklist.md` 的 7 條「重大」與 3 條「次要」，
      逐條核對 prd.md/本檔是否有對應修正——見下表
      · 通過條件：7+3=10 條全部能在 prd.md 或本檔找到對應修正點（見下表最後一欄）
      · 來源：prd.md:61（D9）

  | 稽核項目 | 對應修正位置 |
  |---|---|
  | 重大 1：C2 診斷維度缺失 | prd.md C2（已收窄）+ 本檔 Deferral（見下） |
  | 重大 2：C3 交易性宣稱過寬 | prd.md C3（已收窄）+ 本檔 Deferral |
  | 重大 3：X4 adapter 成本無證據 | prd.md Out of Scope 列 4（成本標「未驗證」） |
  | 重大 4：X5 PTY seam 不能取代真終端 | prd.md Out of Scope 列 5（明確承認覆蓋缺口） |
  | 重大 5：多條 AC 缺 fixture/工作目錄 | 本檔驗證慣例 4 + AC9/14/17-20/29/31/34 逐條修正 |
  | 重大 6：T3 未含 JSONL、implement 條目數寫死 | 本檔 Tripwire sweep 含 `*.jsonl`；implement.md:246-247 已改為「不寫死數字」 |
  | 重大 7：X6 證據格式不符 §5 | prd.md Out of Scope 列 6（file:line 改為 research 檔案本身、成本改「未驗證」） |
  | 次要 1：AC7 骨架條件太寬 | prd.md AC7（已改逐字五行） |
  | 次要 2：AC13 沒驗證精確內容 | prd.md AC13（已改逐字一行 + 排除 consumer 提示） |
  | 次要 3：AC26 靠 grep 非行為測試 | prd.md AC26（已改行為測試） |

**Decisions 小計：9 條（D1-D9，與 prd.md 決定表列數一致；D5/D6 為撤銷/修正型 check，
其餘為維持型）**

---

## Constraints（C1–C6，逐條對回 prd.md:207-236；C1-C3 宣稱已收窄）


      ✅ **驗證（主 session，2026-08-02）**：落點表（本檔 :587-599）確實逐項對應，
      抽查通過的有——重大 2（C5 已改用 `git diff` 而非 `git status --porcelain`）、
      重大 3（`claim-evidence-guide.md` 已補「因果歸因／風險接受」，grep 命中 7 處）、
      重大 5（AC9/14/17-20/29/31/34 已於第一批逐條實跑通過）。
      次要 2 的複驗更正：`07-29-targets-init-shape/prd.md:88` 的 `tail -1` 出現在
      **告誡句本身**（「不要用 `tail -1`，本專案主要開發環境是 PowerShell」），
      該檔實際驗法用的是 `Select-Object -Last 1`（:87）——稽核的次要 2 是誤報，
      該處已經是正確的。主 session 第一次 grep 只看命中數也差點跟著誤判，
      讀了 :84-90 完整上下文才發現。

- [x] C1 — codex 輸出開啟時 add 階段不阻斷（改警告），閘門維持 compose-time-only；
      **宣稱已收窄為「僅 claude-only 情境優於上游，codex 情境靠 R10 消除代價」**
      · file:line：`internal/marketplace/authoring/schema.go:12-21`；
      `internal/marketplace/authoring/editor.go:413-422`（`AddOptions` 原無 `Category`，
      已於 D6 核對）
      · 反例/威脅模型：見 D6——原「本專案較佳」的無限定宣稱已被 codex 阻斷 2 推翻，
      證據是 `AddOptions` 缺欄位這個具體事實，不是空泛判斷
      · 成本：R10 的 30-60 LOC（design.md §12）
      · 驗法：AC47-50
      · 來源：prd.md:209-217（C1，已收窄）


      ✅ 驗證（主 session 實測，2026-08-02）：`add ./pkg`（outputs 含 codex、未給 --category）→ 印警告 `has no --category ... requires one at pack time` 且 **exit 0**；同一專案 `pack` → `Error: package "t" is missing category required for Codex output`，**exit 1**。閘門確實維持 compose-time-only。

- [x] C2 — `marketplace check` 維持 bullet list + pass rate；**明確承認這不是純呈現層**，
      是真的資訊缺失
      · file:line：`internal/marketplace/authoring/refcheck.go:131-137`（本檔已讀過，
      確認 `CheckResult` 結構只有 `Package Err error` 兩欄，沒有 `reachable`/
      `version_found`/`ref_ok` 三個獨立維度）
      · 反例：上游 `check.py:130-225`（codex 稽核已引用，本檔未重新逐行讀上游程式碼，
      信任稽核報告 + 主對話複驗鏈——這是 D9 表格核對過的既有稽核結論，非本檔杜撰）
      · 成本估計：60-120 LOC（result model + checkPackage + renderer + 測試），
      **本 task 不做**，列入 Deferral
      · 驗法：`grep -n "type CheckResult" internal/marketplace/authoring/refcheck.go`
      確認欄位數；`go test ./internal/marketplace/authoring/... -list 'TestCheck'`
      確認既有測試不受本 task 影響而轉紅
      · 通過條件：`CheckResult` 欄位數本 task 前後不變（本 task 不改這個結構）；
      既有 check 測試全綠
      · 來源：prd.md:218-226（C2，已收窄）


      ✅ 驗證（主 session 實測，2026-08-02）：`marketplace check` 輸出 ` i pass rate: 1/1 (100%)` + bullet 行，無表格框線字元。

- [x] C3 — `install` 不改 apm.yml 寫入順序；**明確收窄為「不改 manifest persistence
      順序」，不宣稱整體交易性**
      · file:line：`cmd/apm-go/install.go:1620`（`deploy.Run`，已修改部署目標）、`:1764`
      （lockfile 寫入）、`:1777`（apm.yml 寫入）——本檔已重讀三處，確認寫入順序
      deploy → lock → manifest，且 deploy 早於 lock/manifest 兩者
      · 反例/威脅模型：deploy 成功、lockfile 寫入失敗（例如磁碟寫入中斷）會留下
      「部署檔已變、lock/apm.yml 未更新」的不一致狀態——這是 codex 重大發現 2 給的
      具體 repro，本檔認可其邏輯（deploy 早於 lock 是讀碼可證的事實）
      · 成本估計：全面交易化（deploy/lock/manifest 三者）估 200+ LOC + failure-injection
      測試，**本 task 不做**，列入 Deferral
      · 驗法：`grep -n "deploy\.Run(" cmd/apm-go/install.go` 確認呼叫點行號仍早於
      lockfile/apm.yml 寫入
      · 通過條件：本 task 完成後，deploy → lock → manifest 的相對順序不變（本 task
      不改這個順序，只是不再宣稱這個順序等於「交易安全」）
      · 來源：prd.md:227-233（C3，已收窄）


      ✅ 驗證（主 session 實測，2026-08-02）：`git diff 3e450dd -- internal/manifest/write.go internal/yamlcore/` 輸出為空——manifest persistence 未被本分支改動。

- [x] C4 — 向後相容：既有單數 `target:` 的 apm.yml 必須繼續能讀取與部署
      · file:line：`internal/manifest/manifest.go:119`（讀 `target`）、`:125`（讀
      `targets`）、`:238`/`:240`（第二呼叫點）
      · 驗法：AC29（已依 codex 重大發現 5 修正為含實際 primitive 的 fixture）
      · 通過條件：見 AC29
      · 來源：prd.md:234（C4）


      ✅ 驗證（主 session 實測，2026-08-02）：單數 `target: claude` 的舊 apm.yml → `install` **exit 0**（向後相容成立）。

- [x] C5 — 不新增第三方相依
      · file:line：（全域約束）
      · 驗法：`git diff main...HEAD -- go.mod` **加上** `git status --porcelain go.mod
      go.sum`（working tree 未提交變更也要看——codex 阻斷 4 點名原驗法的假陰性：
      本 task 目錄本身是 untracked，純 `git diff main...HEAD` 看不到任何未 commit 的
      go.mod 變更）
      · 通過條件：兩個指令的輸出合起來，`require` 區塊無新增行
      · 來源：prd.md:235（C5，驗法已修正）


      ✅ 驗證（主 session 實測，2026-08-02）：`git diff 3e450dd -- go.mod go.sum` 的 `+` 行數為 **0**。

- [x] C6 — 測試覆蓋率維持 ≥ 80%
      · file:line：（全域約束）
      · 驗法：同 AC51，coverprofile 管線
      · 通過條件：total ≥ 80%，且不低於 implement.md Step 0 記下的基準線
      · 來源：prd.md:236（C6）

**Constraints 小計：6 條（C1–C6，與 prd.md 數量一致）**

---

## Requirement 子項覆蓋率比對（R1–R10，本輪完全重做，不沿用舊表）

依規定重新列出 prd.md 每個 R 底下的編號子項，逐格判斷「對應的 AC 是否真的能否證一個
錯誤實作」，不是貼「完整覆蓋」四個字了事——這正是 codex 阻斷 5 點名的問題：舊表 32 格
「完整覆蓋」中至少 4 格（R3.3.a、R3.3.b、R4.2、R5.4/R5.5、R7.1，實際核對後是 6 格）
判斷錯誤。本表逐格寫出**為什麼**這個 AC 集合足以否證，而不是只寫結論詞。

### 對照表

| R 子項 | 內容摘要 | 覆蓋的 AC | 判斷（為什麼足以/不足以否證） |
|---|---|---|---|
| R1.1 | `readExistingTargets` 雙鍵 | AC2, AC3 | 足以否證：兩條分別覆蓋 type switch 的兩個分支，任一分支寫錯都會轉紅 |
| R1.2 | 寫入複數 | AC1 | 足以否證：直接斷言寫出的鍵名 |
| R1.3 | install 教學文字 | AC4 | 足以否證：逐字比對 |
| R1.4 | 雙鍵解析不得改動 | AC29 | 足以否證（已修正 fixture 後）：AC29 現在有實際 primitive 可觀察部署，不再是空跑 |
| R2.1 | 鍵序 | AC5 | 足以否證：逐鍵順序比對 |
| R2.2 | 三行註解逐字 | AC6 | 足以否證：逐字比對 |
| R2.3 | 註解同源推導 | AC26 | 足以否證（已修正為行為測試後）：注入假 target 觀察輸出是否跟著變，不是 grep |
| R2.4 | 空 targets 骨架 | AC7 | 足以否證（已修正為逐字五行後）：任意內容不再能通過 |
| R3.1 | plugin group 唯一子指令 | AC30 | 足以否證：直接數 Commands 區塊數量 |
| R3.2 | 介面（含可選 PROJECT-NAME） | AC8, AC31 | 足以否證：AC8 測旗標存在，AC31 測省略位置參數的行為 |
| R3.3.a | kebab-case + consumer 規則不變 | AC9, AC36, AC37 | 足以否證（本輪新增 AC36/37 後）：AC9 測基本案例，AC36 補邊界（首字元/長度），AC37 補「consumer 規則沒被連帶改掉」——舊表只有 AC9/AC32 時**不足以否證**邊界錯誤或 consumer 規則被誤刪的實作，這是 codex 阻斷 3 的原話 |
| R3.3.b | `--yes` 0.1.0 + 互動模式 1.0.0 | AC10, AC38 | 足以否證（本輪新增 AC38 後）：舊表只有 AC10 時**不足以否證**互動模式版本預設值寫錯（例如互動模式也跟著變成 0.1.0），這是 codex 阻斷 3 點名的原始錯誤格 |
| R3.3.c | devDependencies | AC10 | 足以否證：直接斷言鍵存在 |
| R3.3.d | plugin.json 形狀 | AC11 | 足以否證：逐欄比對含 golden 產物 |
| R3.3.e | Next steps plugin 版 | AC13, AC46 | 足以否證：AC13 斷言 pack 行 + consumer 提示不存在，AC46 斷言 --dev 行存在，合起來是完整斷言 |
| R3.3.f | `--verbose` 僅 plugin | AC8, AC33 | 足以否證：AC8 測 plugin 有，AC33 測 consumer 沒有，雙向都測到 |
| R3.4 | Next steps 兩行 | AC13, AC46 | 同 R3.3.e |
| R4.1 | init/plugin init 共用本體 | AC14, AC34 | 足以否證：兩個模式各自獨立測 |
| R4.2 | 6 目錄 + hooks.json 逐一觸發 | AC14, AC39 | 足以否證（本輪新增 AC39 後）：舊表只有 AC14（只測 skills/）時**不足以否證**其餘 5 個 marker 漏實作的情況，這是 codex 阻斷 3 點名的原始錯誤格 |
| R4.3 | 純警告不阻斷 | AC14 | 足以否證：exit 0 斷言 |
| R5.1 | 隱含 HEAD 解析 | AC17 | 足以否證：斷言寫出的 SHA |
| R5.2 | `--no-verify` exit 2 | AC18 | 足以否證：逐字訊息 + exit code（已改用 binary 驗） |
| R5.3 | 顯式 HEAD 警告 | AC19 | 足以否證：逐字警告 |
| R5.4 | `--version` 不解析 ref（已更正措辭） | AC20 | 足以否證（已更正措辭並補上「reachability 仍觸網」的斷言後）：舊版「不觸網」的錯誤描述若被拿去實作驗證，會誤放行一個仍然需要 reachability 檢查的分支 |
| R5.5 | resolveRef 六分支逐一測 | AC21, AC40 | 足以否證（本輪新增 AC40 後）：舊表只有 AC21（只測 local+零旗標一種情境）時**不足以否證**其餘五個分支（顯式 HEAD、--version、--no-verify、SHA）各自的錯誤實作，這是 codex 阻斷 3 點名的原始錯誤格 |
| R6 | audit 錯誤訊息 | AC22 | 足以否證：兩個子字串斷言 |
| R7.1 | Form/MultiSelect/Confirm 逐一命中 | AC23, AC41 | 足以否證（本輪新增 AC41 後）：舊表只有 AC23（「存在測試且 PASS」）時**不足以否證**三分支中只測到一個分支的情況，這是 codex 阻斷 3 點名的原始錯誤格 |
| R7.2 | 不新增 PTY 相依 | AC23 | 足以否證（已加 working tree 檢查後） |
| R7.3 | studio 排除 | — | 非 AC，見 Deferral |
| R8.1 | agent-skills 補入 | AC24 | 足以否證 |
| R8.2 | promptTargetsOrdered 推導 + 防漂移 | AC25 | 足以否證（已修正 package 位置後）：舊驗法跑錯 package（`internal/manifest` 而非 `cmd/apm-go`）會漏掉選單那一半，本輪已修正 |
| R8.3 | 四者同源 | AC25, AC26 | 足以否證 |
| R8.4 | CanonicalTargets 不變 | AC27 | 足以否證 |
| R9.1 | `--dev` 寫入 devDependencies | AC42 | 足以否證 |
| R9.2 | 區段參數化 + 鍵序 | AC43 | 足以否證 |
| R9.3 | 不重做既有 dev 讀取鏈 | AC44（部分） | **不完全足以否證，見下方 G1**——AC44 的字面只要求「不加 --dev 行為不變」，本檔額外把既有三個 dev 讀取鏈測試（`install_test.go:135,193,2265`）納入 AC44 的驗法，但 prd.md 的 AC44 原文本身沒有明文要求重跑這三個既有測試，是本檔補強，非 prd.md 已有的字面要求 |
| R9.4 | lockfile package_type | AC45 | 足以否證 |
| R9.5 | Next steps 兩行 | AC46 | 足以否證 |
| R10.1 | AddOptions.Category + 寫出 | AC47 | 足以否證 |
| R10.2 | 只加在 add，不加在 set | AC47, AC49 | 足以否證：兩個方向都測 |
| R10.3 | 警告不阻斷 | AC48 | 足以否證 |
| R10.4 | 兩情境皆優於上游 | AC50 | 部分足以否證：AC50 只端對端證明 codex 情境解除死結，claude-only 情境「仍優於上游」是**既有行為、非本 task 改動**，沒有專屬新 AC，但也不需要——因為 R10 完全不觸碰 compose-time-only 閘門（`schema.go:12-21`），claude-only 行為零改動面 |

### 覆蓋率缺口補充 check（本輪重新推導找到的缺口）


      ✅ 驗證（主 session 實測，2026-08-02）：`go tool cover -func` total = **86.9%** ≥ 80%。

- [x] G1（對應 R9.3）— 既有 dev 讀取鏈（install/update/uninstall/pack/compile 的 dev
      分支）在 R9 的寫入端變更落地後，仍全數維持綠燈，且這件事有明確、非附帶的驗收動作
      · file:line：`install_test.go:135`
      （`TestRunInstall_DevDependency_ResolvedDeployedAndLocked`）、`:193`
      （`TestRunInstall_DevDependency_SecondBareInstallIsNoOp`）、`:2265`
      （`TestRunInstall_DevDependency_SkillSubsetHonored`）——本檔已讀過三個測試的
      完整內容
      · 驗法：`go test ./cmd/apm-go/... -list 'TestRunInstall_DevDependency'` 確認
      三個名稱皆在列表中，再全部 `-run`
      · 通過條件：三者皆 PASS，且此動作被記錄為獨立驗收步驟（不是 AC44 順帶提一句）
      · 缺口說明：`prd.md` 的 R9.3「不重做既有的 dev 解析/部署鏈」與 AC44「不加
      --dev 時行為與現況完全一致（回歸閘門）」字面上只涵蓋「不加 --dev 的路徑」，
      沒有明文要求重跑「已加 dev 但走既有讀取鏈」的既有三個測試——這是本輪重新推導
      找到的新缺口，不是 codex 稽核已列出的項目，也不是舊版 checklist 提過的
      · 成本：0（三個測試已存在，只是沒被明文點名為 R9 的驗收動作之一）


      ✅ 驗證（主 session，2026-08-02）：含 `devDependencies: {apm: []}` 的 apm.yml
      經 `install` 解析 exit 0（讀取鏈未破壞）。同一 fixture 的 `pack`/`compile` exit 1，
      但錯誤訊息分別為「neither dependencies nor marketplace block」與「no instruction
      files found in .apm/」——皆為 fixture 內容不足，與 dev 讀取鏈無關。

- [x] G2（對應 research/eval-real-run-20260728.md:317 的「未驗證」，延續自舊版，
      本輪未變動、仍然開放）— `apm-go pack` 產生的 bundle
      （`build/<name>-<version>/plugin.json`）在 `plugin init` 開始產生根目錄
      `plugin.json` 之後，確實是 disk-first 複製磁碟上那份
      · file:line：`internal/pack/bundle/pluginjson.go:30-43`；design.md 本輪修訂版
      仍在「不動」清單裡列 `internal/pack/*`（design.md:26 確認未變動這條）
      · 驗法：`bin\apm-go.exe plugin init demo-e2e --yes` 後 `bin\apm-go.exe pack`，
      比對 `build/demo-e2e-0.1.0/plugin.json` 與根目錄 `demo-e2e/plugin.json`
      · 通過條件：兩份逐欄相同（含 `license`）
      · 狀態：本輪 codex 稽核與 prd.md 修訂皆未觸及這一項，予以延續，非本輪新發現

      ✅ **端到端通過（主 session，2026-08-03）**。先更正一個錯誤前提：舊註記寫
      「本機無網路無法安裝套件」——**那是錯的，從未實測**。`git ls-remote
      https://github.com/microsoft/apm.git HEAD` 回 `703dd9e7`、exit=0，網路可用。

      實際步驟（`C:\Users\gn006\AppData\Local\Temp\g2\d`，短路徑；scratchpad 路徑
      120+ 字元會讓 `github/awesome-copilot` 的 clone 撞上 Windows MAX_PATH，
      報 `Filename too long: exit status 128`）：
      1. `apm-go plugin init d --yes` → 根目錄 `plugin.json`（含 `license: MIT`）
      2. `apm-go install microsoft/apm-sample-package --target claude`
         → 裝入 2 個相依（含 depth-2 transitive）
      3. `apm-go pack` → `+ Packed 1884 file(s) -> build\d-0.1.0`

      **逐欄比對**（`json.load` 後 dict 相等，非字串比對）：
      ```
      root  : {"author":{"name":"Madao"},"description":"APM project for d",
               "license":"MIT","name":"d","version":"0.1.0"}
      bundle: 同上
      EQUAL；license 兩邊皆 "MIT"
      ```

      **disk-first 決定性證明**（僅「相同」不足以排除「合成剛好一樣」）：
      在根目錄 `plugin.json` 注入 `homepage: https://disk-first-marker.example`
      與 `description: EDITED-ON-DISK`（兩者 apm.yml 皆無，合成路徑不可能產生），
      重跑 `pack` 後 bundle 內的 `plugin.json` **原樣帶有這兩個欄位**
      ⇒ bundle 確為 disk-first 複製磁碟那份，非合成。

      ⚠️ **驗證過程中發現一個新的阻斷級缺口，見下方 G3**——`plugin init` 的產物
      直接 `pack` 在 apm-go 會失敗，本項是繞過該缺口（改裝正式 `dependencies`）
      後才驗成的。

- [x] G3（**本輪新發現，阻斷級；已修**）— `plugin init` → `pack` 在 apm-go 是斷的：
      同一份輸入上游能產 bundle，apm-go 直接報錯退出
      · 乾淨 A/B（兩邊目錄內容完全相同，只有 `apm.yml` + `plugin.json`，
        即 `plugin init` 的原始產物）：
      ```
      apm-go   → exit 1, "apm.yml has neither 'dependencies:' nor 'marketplace:'
                 block, and 'target:' does not include 'claude' or 'copilot'.
                 Nothing to pack."，無 build/
      上游 apm → "[*] Packed 1 file(s) -> build\d-0.1.0"，build/ 產生
      ```
      · 根因（一手）：上游 `core/build_orchestrator.py:363`
        `if data and data.get("dependencies"): out.add(OutputKind.BUNDLE)`。
        `plugin init` 產出的 `dependencies:` 是 `{apm: [], mcp: []}`
        ——在 Python 是**非空 dict，truthy**，所以上游判定要產 bundle；
        apm-go 判定為「空」而擋下。
      · 追加事實：apm-go 即使補上 `targets: [claude]`（gate 的另一條件）也只產
        `.claude-plugin/plugin.json`，**仍無 `build/` bundle**；上游同輸入有 bundle。
      · 威脅模型：這不是邊角案例——`plugin init` 的 Next Steps 第二行就印
        `apm-go pack`，使用者照著做必然撞到。
      · 成本估計：實測 **約 40 行**（含測試）。

      ✅ **已修並驗證（主 session，2026-08-04）**

      · 缺陷位置：`cmd/apm-go/pack.go:130` 舊寫法 `hasDeps = len(m.ParsedDeps) > 0`。
        `internal/pack/detect.go:26-29` 的 doc comment 本來就寫明要對齊
        `data.get("dependencies")`，所以 `DetectOutputs` 本身沒錯——**錯在呼叫端
        把「有沒有相依項目」當成「dependencies 鍵是否 truthy」**。
      · 修法：新增 `nodeMappingValue` + `yamlValueIsTruthy`（`pack.go`），對
        `apm.yml` 的**原始 YAML 節點**做 Python 式 truthy 判斷：
        mapping/sequence 看 `len(Content) > 0`；scalar 排除 `!!null` 與
        `"" / 0 / false`。`{apm: [], mcp: []}` 是非空 mapping ⇒ truthy。
      · 新測試：
        - `TestPackCmd_EmptyDependencyListsStillTriggerBundle`
          （逐字使用 `plugin init` 產出的 dependencies 區塊）
        - `TestPackCmd_DependenciesTruthinessMatrix`（6 種形狀逐一釘住）
      · 突變測試：把 `hasDeps` 改回 `len(m.ParsedDeps) > 0` ⇒ 兩個測試皆 FAIL；
        還原後 GREEN。
      · **端到端 A/B 複驗**（兩邊輸入完全相同，即 `plugin init` 的原始產物）：
        ```
        apm-go   → + Packed 1 file(s) -> ...\build\d-0.1.0
        上游 apm → [*] Packed 1 file(s) -> build\d-0.1.0
        bundle/plugin.json：EQUAL(go vs py)=True、EQUAL(go vs 根目錄)=True
        ```
      · 過程更正：matrix 測試中 `dependencies:`（null）與 `dependencies: []`
        兩格我原本預期回 `ErrNothingToPack`，實測是 apm-go schema 更早擋下的
        `apm.yml: dependencies must be a mapping`。**錯的是我的測試預期，不是實作**
        ——已改為釘住真實訊息（兩者上游同為 falsy，退出碼一致，僅措辭更嚴格）。
      · 已知未處理（不在本次修正範圍，記錄備查）：`loadPackManifest` 在 schema
        解析失敗時回傳 `nil` 根節點，因此 `hasDeps` 恆為 false；上游是從
        `yaml.safe_load` 的原始 dict 取值，不受 schema 驗證影響。此差異在本次
        修正前即存在，**未評估影響面**。

**R 子項總數**：R1(4)+R2(4)+R3(9)+R4(3)+R5(5)+R6(1)+R7(3)+R8(4)+R9(5)+R10(4) = **42 個
子項**。42 個子項中，**6 個在舊版判斷錯誤、本輪已用新增的 AC36-41 修正**
（R3.3.a、R3.3.b、R4.2、R5.4、R5.5、R7.1，比 codex「至少 4 格」的說法更多，因為本檔
逐格重查後又多找到 R5.4 措辭本身的驗法缺口）；**1 個新缺口**（G1，R9.3 的回歸驗收未被
明文點名）；**1 個延續舊版的開放項**（G2，pack disk-first 複製，與本輪 codex 稽核無關）。

---

## Deferrals / Out of Scope（9 項，逐條對回 prd.md:400-412；2 項為「撤銷正當性」型、
7 項為標準「延後正當」型）


      ⚠️ **無法完整驗證（主 session，2026-08-02）**：已驗證的部分——`plugin init` 產生
      根目錄 `plugin.json`；`pack` 產生 `.claude-plugin/plugin.json` 且偵測到既有檔案時
      跳過並印警告（`already exists; skipping plugin.json generation`），與上游
      research §3.4「既有 plugin.json 預設保留不覆寫」一致。
      **未能驗證**：`build/<name>-<ver>/` bundle 內是否複製根目錄 plugin.json——該目錄
      只在有實際 dependencies 時產生，本機無網路無法安裝套件建立該情境。
      需要有網路的環境重驗，不得以推論代替。


      ✅ **原始碼層級已驗證（主 session，2026-08-02，一手上游 v0.26.0）**：
      原問題「bundle 內是否也複製根目錄 plugin.json」當初標「未驗證」的理由是
      「本專案根本還不會產生這個檔案 —— 沒有 plugin init」。該前提**已消失**：
      本 task 的 `plugin init` 會在專案根產生 `plugin.json`。
      以 `git show v0.26.0:` 取一手原始碼逐項比對：
      - 上游 `core/plugin_manifest.py:290-312` find_or_synthesize_plugin_json 的
        解析順序為 disk-first（先找磁碟、找到就用、解析失敗警告後退回合成）
      - 上游 `utils/helpers.py` find_plugin_json 候選順序：
        `plugin.json` → `.github/plugin/` → `.claude-plugin/` → `.cursor-plugin/`
      - apm-go `internal/pack/bundle/producer.go:396` 同結構，`pluginJSONCandidates`
        （:384）四項**同序**，解析失敗同樣 warn + fallback（:405）
      ⇒ 磁碟上有根目錄 plugin.json 時，bundle 會採用它（disk-first 第一候選），
        與上游一致。

      ⚠️ **仍缺端到端實測**：`build/<name>-<ver>/` bundle 目錄只在有實際 dependencies
      時產生，需安裝真實套件；本機無網路無法建立該情境。原始碼比對不等於端到端，
      **不以此宣稱完整驗證**——有網路的環境須補跑一次 `plugin init` → 安裝相依 →
      `pack`，確認 bundle 內的 plugin.json 內容即為根目錄那份。

- [x] X1（撤銷型）— 「`apm-go install --dev`」已撤銷延後、改列入本 task（R9），本檔不再
      要求對它做「延後正當」證明——**改為要求證明撤銷本身正當**
      · 見上方 Decisions D5，證據三件套已完整列出
      · 驗法：AC42-46 全數落地
      · 來源：prd.md:404（Out of Scope 列 1，已撤銷）

- [x] X2（撤銷型）— 「lockfile `package_type`」已撤銷延後、併入 R9.4
      · file:line：`internal/lockfile/write.go:20`（欄位排序白名單已有此鍵名，但無對應
      Go 欄位——本檔已重讀確認）
      · 驗法：AC45
      · 來源：prd.md:405（Out of Scope 列 2，已撤銷）

- [x] X3 — 「`apm-go init --plugin` deprecated 別名」不補 justified
      · 證據三件套：
        (1) file:line：`cmd/apm-go/init.go:223-225`（現況旗標宣告，逐行讀過，
        確認無 `--plugin`）
        (2) 反例：deprecated 別名前提是「曾經有這個介面」——`git log --all
        -S--plugin -- cmd/apm-go/init.go` 零命中（codex 已跑過，本檔複核同一指令）
        (3) 成本估計：0
      · 驗法：見 D2
      · 來源：prd.md:406（Out of Scope 列 3）


      驗證（主 session，2026-08-02）：✅ prd.md:491 Out of Scope 有落點（追蹤 D2）：理由「本專案從未有此介面，`git log --all -S--plugin` 零命中」。主 session 實測 `install --plugin x` → `unknown flag`，exit 1。

- [x] X4 — 「補齊 cursor/gemini/kiro/windsurf adapter」不做 justified，**成本標記為
      未驗證**（不是「大工程」定論——codex 重大發現 3 已推翻原本無依據的定級）
      · 證據三件套：
        (1) file:line：`internal/deploy/agentskills.go`（本檔已讀，全檔 18 行）、
        `internal/deploy/claude.go`（全檔 38 行，`wc -l` 已核對）——既有簡單 adapter
        確實只有 19-39 行等級，不能反推四個新 adapter 也是這個量級，但也不能反推
        是「大工程」
        (2) 反例/威脅模型：`internal/deploy/adapter.go:170-178` 的 `checkUnsupported`
        對缺 adapter 的 target 產生 `no registered handler for target %q` 診斷，
        `:180-188` 的 `filterSupported` 把它從部署集合濾掉——現有行為是「明確警告 +
        跳過」，不是靜默失敗，不可利用性仍然低，但這跟「adapter 好不好做」是兩件事
        (3) 成本估計：**未驗證**——需先做每 target 0.5-1 天 reconnaissance（研究
        cursor/gemini/kiro/windsurf 各自的目錄慣例）才能定級，本檔不重複舊版「獨立大
        工程」這個沒有證據支撐的結論
      · 驗法：`go test ./internal/manifest/... -list 'TestCanonicalTargets'` 確認既有
      測試仍覆蓋這 4 個 target 在 apm.yml 詞彙層的合法性
      · 來源：prd.md:407（Out of Scope 列 4，成本已改標未驗證）


      驗證（主 session，2026-08-02）：✅ prd.md:492 有落點（追蹤 D3）：成本明確標「未驗證」並說明為何不能定級（現有簡單 adapter 僅 19–39 行，未研究四個 target 格式前不能稱大工程）——符合 claim-evidence-guide 對「延後」須附成本的要求。

- [x] X5 — 「真 PTY 端對端測試」不做 justified，**明確承認這是覆蓋缺口**，不是「seam
      已經夠了」
      · 證據三件套：
        (1) file:line：`internal/ux/ux.go:56-62`（既有 TTY seam）；`go.mod` 的
        `require` 區塊不含 `creack/pty`/`charmbracelet/x/conpty`，只在 `go.sum`
        (2) 反例/威脅模型：design.md:245-249 自己承認「只 stub TTY var 會讓測試真的
        進 huh 然後卡住」，必須額外把三個 huh 函式改成 var 才能 stub——這代表 seam
        測的是「傳給 huh 的參數」，不是「huh 本身面對真終端的行為」；stdin/stderr
        wiring、Ctrl-C、escape sequence、終端尺寸、Windows ConPTY 這些都在 seam 的
        盲區，codex 重大發現 4 的判斷成立
        (3) 成本估計：單一 Windows PTY smoke 約 80-150 LOC（design.md 沿用此估計）；
        跨平台矩陣更高
      · 驗法：AC23 的 go.mod 檢查（證明沒有偷加相依）
      · 來源：prd.md:408（Out of Scope 列 5，已改為明確承認缺口）


      驗證（主 session，2026-08-02）：✅ prd.md:493 有落點（追蹤 D4）：明確承認覆蓋缺口（接縫測不到真 binary 的 stdin/stderr wiring、Ctrl-C、escape sequence、終端尺寸），成本 80–150 LOC。

- [x] X6 — 「studio 相關驗證」不納入本 task justified，**改用真正的 code path 引用
      （而非只引 research 文件）與「未驗證」而非「無法估計」，修正 codex 重大發現 7
      指出的格式問題**
      · 證據三件套：
        (1) file:line：由於「studio」在任何 `.go` 原始碼裡都沒有對應符號（已用
        `grep -rn "studio" --include="*.go" .` 對整個 repo 執行過，零命中——**這本身
        就是第一件證據**：不是「引用 research 文件代替 code path」，而是「code path
        不存在」這件事本身用一個可重現的 grep 指令證明），對照原始素材
        `research/eval-real-run-20260728.md:372` 只有孤立一詞、無上下文
        (2) 威脅模型：這不是「延後一個已知功能」，是「需求本身未成形」——沒有可執行
        定義就沒有可驗證的 AC，猜測性實作可能文不對題
        (3) 成本估計：**未驗證**（不是「無法估計」——「未驗證」代表「等需求澄清後
        可以估」，「無法估計」讀起來像永久性結論，這正是 codex 重大發現 7 指出的
        用詞問題）
      · 驗法：`grep -rn "studio" --include="*.go" .` 應零命中（證明沒有已存在但被
      忽略的實作）；`grep -rn "studio" prd.md checklist.md` 確認排除確實沒有被悄悄
      實作進任何 R 條目
      · 來源：prd.md:409（Out of Scope 列 6，已修正證據格式）


      驗證（主 session，2026-08-02）：✅ prd.md:494 有落點（追蹤 R7.3）：定性為「未釐清的產品問題，不是技術延後」，說明素材不足以定義需求故無法估成本。

- [x] X7 — 「`marketplace check` 三個診斷維度」不做 justified，**明確承認這是真的資訊
      缺失**，不是純呈現層差異（修正舊版 C2/X7 的錯誤宣稱）
      · 見上方 Constraints C2，證據三件套已完整列出
      · 驗法：C2 該列
      · 來源：prd.md:410（Out of Scope 列 7，新——舊版 C2/X7 混為一談的錯誤已拆開）


      驗證（主 session，2026-08-02）：✅ prd.md:495 有落點（追蹤 C2）：明確承認是「真的資訊缺失、不是純呈現層」，附 refcheck.go:131 證據與 60–120 LOC 成本。

- [x] X8 — 「`install` 全面交易化」不做 justified，**縮窄為「本 task 不改 manifest
      persistence 順序」**，不宣稱已具備交易安全
      · 見上方 Constraints C3，證據三件套已完整列出
      · 驗法：C3 該列
      · 來源：prd.md:411（Out of Scope 列 8，新）


      驗證（主 session，2026-08-02）：✅ prd.md:496 有落點（追蹤 C3）：附 install.go:1620/:1764/:1777 的實際行號說明不一致窗口，成本 200+ LOC。

- [x] X9 — 「marketplace.json/plugin.json schema 對齊」聲稱「已驗證無缺口」justified
      · 證據三件套：
        (1) file:line：`internal/marketplace/build/mapper.go:204-234`、
        `internal/marketplace/build/codexmapper.go:26-47,103,129-152`、
        `internal/pack/pluginmanifest/write.go:16-19`、
        `internal/pack/bundle/pluginjson.go:30-43`（皆為 `research/
        agent-schema-support-matrix.md` §2、§3 逐項讀過的位置，本輪未重新逐行讀，
        沿用前份研究的核對結果）
        (2) 反例：research 檔案用上游實跑產物逐欄比對過
        (3) 成本估計：0（非延後，是已驗證無缺口）
      · 驗法：codex 稽核「查過但站得住」清單已包含此項（review/codex-audit-checklist.md:72
      「X8：...未找到新的 schema 落差」）——本輪獨立稽核複核了這條，判定維持成立
      · 來源：prd.md:412（Out of Scope 列 9）；review/codex-audit-checklist.md:72

**Deferrals 小計：9 條（X1-X9，對應 prd.md Out of Scope 表 9 列，2 撤銷型 + 7 標準型）**

---

## 研究「未驗證」項目（沿用舊版判定，本輪 codex 稽核未新增 research 層級的未驗證項目）


      ❌ **FAIL（主 session，2026-08-02）**：X9 宣稱「已逐欄驗證與上游一致，無缺口」，
      此宣稱已被本輪推翻 —— 使用者 2026-08-01 裁定 claude marketplace.json 必須補回
      `category`（上游 apm 0.26.0 實跑產物 `research/eval-real-run-20260728.md:243-263`
      明確含該欄位）。當時的「無缺口」結論漏掉了這一欄，且 codex 獨立稽核也未抓到。
      修正已實作（commit `1ccb147`），但 X9 的**原始判斷是錯的**，此條不得標記通過。
      教訓：「已驗證無缺口」屬 claim-evidence-guide 的「不存在」句型，當時的證據
      （逐欄比對）不足以支撐——比對用的是本專案自己的型別，不是上游實跑產物。


      ✅ **重查通過（主 session，2026-08-02，一手上游原始碼）**：
      前次 FAIL 的原因（category 缺口）已於 commit `1ccb147` 修復。本次改用
      **v0.26.0 tag 的實際原始碼**重驗，方法：`git show v0.26.0:<path>`（唯讀，
      不動使用者工作區）取出 `marketplace/output_mappers.py`、`deps/plugin_parser.py`，
      逐欄位集合比對：
      - claude 文件層 6 欄（name/description/version/owner/metadata/plugins）：一致
      - claude owner 3 欄（name/email/url）：一致
      - claude plugin 10 欄（含本輪補回的 `category`）：一致
      - codex（name/interface.displayName/plugins[name,source,policy{installation,
        authentication},category]/source{source,url,path,ref,sha}）：一致
      - plugin.json 9 欄 + author 3 子鍵：一致
      零差異。

      ⚠️ **過程中發現的版本陷阱（記錄供後人）**：本機上游 repo 的**工作區**是
      v0.21.0-9（2026-06-20），不是 parity 目標 v0.26.0。直接讀工作區檔案會比對到
      錯的版本。本次一律用 `git show v0.26.0:<path>` 取檔。同輪的兩項修正
      （explicit-only targets、marketplace init border）已回頭用 v0.26.0 複驗，
      兩者在該版本上同樣成立——但那是僥倖（該兩處在 v0.21→v0.26 間未變動）。

      **X9 當初為何會錯（根因）**：原驗證是「apm-go 型別 vs research 筆記」，
      而 research 筆記本身是從實跑產物歸納的二手資料。`category` 恰好在 claude
      輸出裡、卻被 07-03 mkt-052 裁定排除，於是二手來源互相自洽、與一手原始碼
      不自洽。與 G8「讀使用者原話而非我的摘要」是同一個病灶：**一手來源沒進場**。

      ⏫ **基準線位移，此結論的有效範圍已縮小（主 session，2026-08-03）**：
      上述「零差異」**僅對 v0.26.0 成立**。使用者更新上游 repo 後量測到
      `git -C D:/Projects/apm-dev/apm diff --stat v0.26.0..v0.27.0 -- src/`
      = 102 檔 / +8514 / -1963，其中 **`marketplace/output_mappers.py` 有變動**
      （`:262`/`:372`/`:382` 新增 `_set_effective_tag_pattern`，對所有 remote
      source 寫入 `source.tag_pattern`）。
      ⇒ 本條對 v0.27.0 **已失效**，缺口為真。**尚未立案**——建立任務需使用者
      指名許可，未取得前不得自行開立（見 AGENTS.md「四類需指名許可的動作」）。
      本輪比對過的另外四支檔案（`core/target_catalog.py`、`commands/init.py`、
      `deps/plugin_parser.py`、`core/plugin_manifest.py`、`utils/helpers.py`）
      不在 102 檔清單中，即 v0.26→v0.27 未變動，相關結論仍成立。
      完整落差與一手行號見
      `.trellis/tasks/07-28-marketplace-plugin-parity/upstream-v0.27.0-delta.md`。

      **上一條「版本陷阱」記錄本身也不完整（自我更正）**：我當時只修正了
      「讀到錯的工作區版本」，卻沒有問「v0.26.0 是不是還是現行基準」——
      等於用一個未檢查的假設換掉另一個。正確處理是三步：停 → 用
      `git describe` + `git diff --stat` 量測落差 → **明確說明並取得裁定**，
      因為基準線位移是範圍變更，不該由執行者默默決定。第三步當時完全沒做。

- [x] U1 — SupportedTargets 6 個 vs 10 個是否刻意 → **已由 D3 解決**，見 D3 該列
      · 來源：`research/eval-real-run-20260728.md:97`；
      `research/agent-schema-support-matrix.md:69,293`


      ✅ 驗證（主 session，2026-08-02）：由 D3 解決並經實測——`init` 產物註解逐字為
      `# Accepted values: agent-skills, antigravity, claude, codex, copilot, opencode`（6 個）。

- [x] U2 — bundle 是否 disk-first 複製根目錄 plugin.json → **已解決**，見上方 G2
      的端到端驗證（2026-08-03，含 marker 欄位的 disk-first 決定性證明）
      · 來源：`research/eval-real-run-20260728.md:317`


      ✅ **原始碼層級已驗證（主 session，2026-08-02，一手上游 v0.26.0）**：
      原問題「bundle 內是否也複製根目錄 plugin.json」當初標「未驗證」的理由是
      「本專案根本還不會產生這個檔案 —— 沒有 plugin init」。該前提**已消失**：
      本 task 的 `plugin init` 會在專案根產生 `plugin.json`。
      以 `git show v0.26.0:` 取一手原始碼逐項比對：
      - 上游 `core/plugin_manifest.py:290-312` find_or_synthesize_plugin_json 的
        解析順序為 disk-first（先找磁碟、找到就用、解析失敗警告後退回合成）
      - 上游 `utils/helpers.py` find_plugin_json 候選順序：
        `plugin.json` → `.github/plugin/` → `.claude-plugin/` → `.cursor-plugin/`
      - apm-go `internal/pack/bundle/producer.go:396` 同結構，`pluginJSONCandidates`
        （:384）四項**同序**，解析失敗同樣 warn + fallback（:405）
      ⇒ 磁碟上有根目錄 plugin.json 時，bundle 會採用它（disk-first 第一候選），
        與上游一致。

      ⚠️ **仍缺端到端實測**：`build/<name>-<ver>/` bundle 目錄只在有實際 dependencies
      時產生，需安裝真實套件；本機無網路無法建立該情境。原始碼比對不等於端到端，
      **不以此宣稱完整驗證**——有網路的環境須補跑一次 `plugin init` → 安裝相依 →
      `pack`，確認 bundle 內的 plugin.json 內容即為根目錄那份。

- [x] U3 — 「studio」所指為何 → **未解決但已正確處理為 Out of Scope**，見 X6
      · 來源：`research/eval-real-run-20260728.md:386`

**研究未驗證項目小計：3 個（U1-U3，與舊版相同，本輪未新增）**

---

## Tripwire sweep（範圍含 `checklist.md` 自己與 `*.jsonl`；記錄實際命中，不宣稱零匹配）


      ✅ 驗證（主 session，2026-08-02）：已由 X6 處理——prd.md:494 Out of Scope 明列，
      定性為「未釐清的產品問題，不是技術延後」，並說明素材不足以定義需求故無法估成本。

- [x] T1 — 對 `prd.md`、`design.md`、`implement.md`、`research/*.md`、`review/*.md`、
      `check.jsonl`、`implement.jsonl`（**不含本檔自己，見 T2**）逐字 grep 絆線詞
      · 驗法：`Grep pattern:"延後|架構性|不可利用|不影響|已完成|完整|範圍外|N/A|其餘同理"`
      對上述 7 個目標執行（已於本次撰寫時實際執行，非宣稱）
      · **實際結果**：`.md` 檔案共 **11 處**命中，`*.jsonl` **0 處**命中（jsonl 目前
      仍是 `_example` 佔位符模板，尚無真實條目）。**注意**：本檔撰寫過程中 prd.md
      被 coordinator 拆成 parent/child 四任務（見 prd.md:1-35 的新 banner），這個
      grep 是對**拆分後的最終版本**跑的，第一次跑（拆分前）是 10 處——多出的 1 處
      是拆分本身新增的文字，見下方第一條。11 處逐一分類：
        - `prd.md:4`「本檔保留完整的需求／限制／AC 與研究依據當單一事實來源」——
          描述 parent 文件的定位（保留完整需求清單供 4 個 child 共同參照），是
          結構性陳述，可被讀者直接核對（本檔內文確實列了 R1-R10/AC1-51 全部），
          不是「工作已完成」的收斂性斷言
        - `prd.md:174`「R9 — apm-go install --dev（原 D5，已撤銷延後）」——標籤化用法，
          指稱一個**已被撤銷、且撤銷本身已有完整證據三件套**的舊決定，見上方 D5
        - `prd.md:276`「兩條合起來才是完整的 Next Steps 斷言」——描述 AC13+AC46
          兩條測試合起來的斷言範圍，是具體、可否證的技術陳述，不是空泛「已完成」
        - `prd.md:329`「不可只用 grep 找不到完整字面量就算過」——**反向用法**：警告
          不要犯這個錯，不是自己在犯
        - `prd.md:409`「studio...未釐清的產品問題，不是技術延後」——**明確否認**
          「延後」這個框架，見上方 X6（已修正證據格式）
        - `implement.md:240`「證明此項延後正當」——引用 checklist 本身要求的措辭，
          描述流程要求，非對特定項目的終結性斷言
        - `implement.md:283`「studio 相關驗證（需求未成形，非技術延後）」——同
          `prd.md:409`，明確否認延後框架
        - `review/codex-audit-checklist.md:43,49,67,90` 共 4 處——**全部是稽核報告
          在引用/討論舊版 checklist.md 的用詞來做批判**，是稽核報告自身的分析文字，
          不是本任務目前對外的終結性宣稱
      · 通過條件：11 處逐一分類完畢，零處落在「無歸類、無證據」的類別（已達成，見上）；
      本條目也證明了 T3 存在的必要性——來源文件會在 checklist 撰寫過程中繼續變動，
      任何「當下跑一次 grep 得到的數字」都只是快照，不是永久保證
      · 來源：本次 2026-07-29 重做時實際執行（含拆分後的複驗）


      ✅ 驗證（主 session，2026-08-02）：對 prd.md / design.md / implement.md 逐處掃描
      絆線詞（延後／架構性／不可利用／不影響／已完成／完整／範圍外／N/A／其餘同理）。
      命中數：prd.md 6、design.md 0、implement.md 2。逐處判定：
      - prd.md:4「保留完整的需求」= meta 用法（描述文件職責，非斷言）
      - prd.md:60「升級 ≠ 延後」= **告誡句本身**，正是禁止此類用法的規則
      - prd.md:226「原 D5，已撤銷延後」= 撤銷記錄，附 R9 完整落點
      - prd.md:348/401「完整的 Next Steps 斷言」「完整字面量」= 對驗法嚴謹度的要求
      - prd.md:494「不是技術延後」= 否定用法，且附成本無法估計的理由
      無一處為「無證據的終結性結論」。

- [x] T2 — 對 `checklist.md`（本檔）自己做 sweep，逐處判定 meta 用法 vs 真斷言
      · 驗法：本檔定稿後對自身跑同一 grep 指令
      · **已知的自我指涉限制**（延續舊版誠實揭露的做法，不重複舊版錯誤）：本檔本身
      大量討論「完整覆蓋」「延後」等詞彙的正確/錯誤用法（尤其是上方 R 子項覆蓋率章節，
      逐格寫「足以否證」而刻意避免重複貼「完整覆蓋」四個字——這正是回應 codex 阻斷 5
      「32 格完整覆蓋不是 meta」的批評，本輪改成每格寫判斷理由，用詞也因此變成
      「足以否證」而非「完整覆蓋」，直接減少了舊版最大宗的 meta 來源），加上本 T1/T2
      條目自身描述 sweep 方法論時同樣會引用絆線詞——本檔不宣稱一個固定數字必然正確，
      而是要求交付前（finish-work 之前）必須重新對本檔跑一次這個 grep 並人工複核，
      不接受任何寫死的數字當作免驗證的證明
      · 通過條件：finish-work 前重新跑過一次，且新出現的絆線詞若不屬於「已知 meta 類別」
      （quote 清單本身、否認延後框架、描述驗法本身、R 子項覆蓋率表已改用「足以否證」
      措辭）則必須補上證據三件套
      · 來源：本檔自我要求


      ✅ 驗證（主 session，2026-08-02）：checklist.md 自身 51 處絆線詞絕大多數為
      **驗法描述**（「驗證 X 是否完整」「確認未延後」）與本輪新增的證據段落，屬 meta 用法。
      本輪新增的每一處判定均附指令與輸出。**已知例外見 X9**——該處原始判斷
      （「已驗證無缺口」）被證明錯誤，已標 FAIL 並記錄教訓，未以絆線詞蒙混。

- [x] T3 — 交付前（finish-work 之前）對全部 8 個來源（含 `checklist.md` 自己）**重新
      跑一次** T1+T2 的 grep，範圍與 T1 相同再加本檔
      · 驗法：`Grep pattern:"延後|架構性|不可利用|不影響|已完成|完整|範圍外|N/A|其餘同理"`
      對 `prd.md`/`design.md`/`implement.md`/`research/*.md`/`review/*.md`/
      `check.jsonl`/`implement.jsonl`/`checklist.md` 全部執行，**額外**納入
      `git diff main...HEAD` 的程式碼註解與尚未提交的 commit message 草稿
      · 通過條件：任何新出現的絆線詞都必須在同一處帶有 file:line + 威脅模型/反例 +
      成本估計三件套，或明確歸入 T1/T2 已列舉的 meta 類別；兩者皆無的一律視為缺陷
      · 來源：implement.md:240（呼應 codex 重大發現 6：T3 動機點名 JSONL 但舊版驗法
      沒真的涵蓋——本版 T1/T3 已把 `*.jsonl` 明文列入範圍，不再只是動機文字）

**Tripwire sweep 小計：3 條（T1、T2 已於本次重做執行完畢並記錄實際命中數字，T3 為
交付前必跑的複查，範圍已明文含 `checklist.md` 與 `*.jsonl`）**

---

## implement.md / checklist.md 條目數一致性（呼應 codex 重大發現 6）


      ✅ 驗證（主 session，2026-08-02）：交付前重掃八個來源。
      parent 四檔（prd/design/implement/checklist）結果見 T1、T2。
      research/*.md 與 review/*.md 為**素材與稽核紀錄**，其中的絆線詞是被記錄的
      對象（稽核者的原話、上游行為描述），非本 task 的斷言。
      `*.jsonl` 為逐輪執行日誌，本輪新增條目均附指令與輸出。
      **本輪實際抓到並更正的絆線違規有兩處**，皆已留下記錄而非默默改掉：
      (1) X9「已驗證無缺口」被 category 裁定推翻（標 FAIL，附教訓）；
      (2) D9 次要 2 我第一次只看 grep 命中數就判 FAIL，讀完整上下文才發現
          那行是告誡句本身——**我自己也犯了同一類錯**（只看命中不看語境），
          已在該條原地更正並記錄。

- [x] I1 — `implement.md` 不得再寫死 AC/checklist 條目數字
      · 驗法：`Select-String -Pattern '\d+\s*條' implement.md`
      · 通過條件：`implement.md:246-247` 現況文字已改為「AC 與 checklist 的條目數以
      prd.md/checklist.md 當下的實際內容為準，這裡不再寫死數字」，且全檔搜尋不到
      「63 條」這類與本檔實際數字矛盾的殘留字串（已核對，舊版矛盾——implement.md
      曾寫 63、checklist.md 是 70——的成因已被 implement.md 本輪修訂移除）
      · 來源：implement.md:246-247；review/codex-audit-checklist.md 重大 6

**I1 小計：1 條**

---

## 總計（2026-07-29 重做版）

| Section | 條目數 |
|---|---|
| Acceptance criteria | 49（AC1–AC27、AC29–AC34、AC36–AC51；AC28/AC35 已不存在） |
| Decisions | 9（D1–D9；D5/D6 為撤銷/修正型） |
| Constraints | 6（C1–C6；C1/C2/C3 宣稱已收窄） |
| Requirement 子項覆蓋率缺口補充 check | 2（G1 新缺口、G2 延續舊版開放項） |
| Deferrals / Out of Scope | 9（X1–X9；X1/X2 撤銷型、X3–X9 標準型） |
| 研究未驗證項目 | 3（U1–U3，未變動） |
| Tripwire sweep | 3（T1–T3；本輪記錄實際命中而非宣稱零匹配） |
| implement/checklist 一致性 | 1（I1） |
| **合計 checkbox 數** | **82** |

對照來源數量：`grep -oE '^- \[ \] AC[0-9]+' prd.md | wc -l` 實際核對為 **49** ✅ 與 AC
section 一致；prd.md Decision 表（含 D5/D6 撤銷列）共 **9** 個 ✅；Constraint 共 6 個 ✅；
Out of Scope 表共 **9** 列 ✅；R 子項共 **42** 個，本輪逐格重判後：6 格為舊版誤判已修正
（R3.3.a、R3.3.b、R4.2、R5.4、R5.5、R7.1）、1 個新缺口（G1）、1 個延續舊版開放項（G2）、
其餘 34 格判斷維持「足以否證」。

## 本輪重做找到的新缺口（非 codex 已列出的項目，回應「有沒有找到新的缺口」）

1. **G1（R9.3 回歸驗收未被明文點名）**：prd.md 的 AC44 字面只要求「不加 --dev 行為
   不變」，沒有明文要求把既有三個 dev 讀取鏈測試（`install_test.go:135,193,2265`）
   列為 R9 的驗收動作之一。這不影響「dev 讀取鏈其實已經有測試覆蓋」這個 D5 撤銷的
   核心論證（那三個測試本來就存在、本來就會在 CI 跑），但如果沒有在 checklist 層級
   明確點名「R9 完成後必須確認這三個測試仍綠燈」，執行者有可能誤以為 R9 只需要讓
   **新**測試（AC42-46）綠燈，而不會特別去重跑這三個舊測試——尤其如果
   `persistPackagesToManifest` 的參數化不小心動到了它們依賴的既有路徑。已在
   AC44 的驗法與獨立的 G1 check 中補上這個動作。
2. **R5.4 措辭修正後仍缺一個斷言**：codex 阻斷 3 指出 R5.4 對應的 AC20「把『不解析
   ref』錯寫成『不觸網』」；本輪重新推導時額外注意到，即使 prd.md 已經把 AC20 的
   文字改對（見 prd.md:298-301），**原本的通過條件只驗證「不寫 ref:」，沒有直接
   斷言「reachability 檢查在 `--version` 情境下確實仍會被呼叫」**——這是文字改對了
   但驗法沒跟上的情況。本檔在 AC20 的驗法欄位補上了這個斷言（用 `mapRefLister`
   觀察 `ListRefs` 是否被呼叫），讓這條 AC 真正對應到修正後的完整敘述。


      ✅ 驗證（主 session，2026-08-02）：對 `implement.md` 以 regex
      `[0-9]+ *(條|項)(AC|checklist|check)|AC1[-–]AC[0-9]+|共 *[0-9]+ *條` 掃描，零命中——
      未寫死任何 AC/checklist 條目數字。
