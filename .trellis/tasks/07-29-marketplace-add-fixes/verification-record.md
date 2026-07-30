# 驗證紀錄 — 07-29-marketplace-add-fixes（外部稽核第三輪修復）

> **狀態：implementer 已跑完下列指令，尚未經外部覆核。`task.py finish` 未執行。**
> 本檔逐項列出可證偽的紅/綠證據；任何「已修復」字樣後面都附著實際跑過的指令與
> 輸出。round 2 的驗證紀錄被本輪稽核指出兩處推翻（BLOCKING 1 structural fix 有
> Windows 逃逸漏洞、BLOCKING 2 的 CLI 印出時序仍錯），本檔取代該版本。

- 實作者：`trellis-implement`（round 3）
- 外部稽核（round 3）：main session 獨立重現後轉交 — 1 個 SECURITY 阻斷（Windows
  路徑遍歷）、1 個一般阻斷（noVerify 分類器缺口 + CLI 印出時序）、句型違規 3 處、
  3 個一行閘門逃逸口、1 個 MINOR
- 日期：2026-07-30

---

## BLOCKING 1（SECURITY）— Windows 相對本地 source 路徑遍歷

### 根因（讀過的程式碼位置）

`internal/marketplace/authoring/refcheck.go`（round 2 版本）`resolveCloneURL`
對 `isLocalPackageSource(source)` 為真的相對路徑呼叫 `filepath.Abs(source)`
直接回傳，沒有邊界檢查。`internal/manifest/mcp.go:258-263`
（round 2 版本）的 `ValidateMarketplaceSource` 只在 `strings.Split(source, "/")`
上找 `".."` 段，從未正規化 `\`。

主 session 重現（`internal/marketplace/authoring`，Go 測試，探針測試後刪除）：

```
src="./..\\..\\outside"  validate=true   resolveCloneURL="D:\...\apm-go\internal\outside"  escapes=true
src="./../../outside"    validate=false  (正確擋下)
src="./normal"           validate=true   resolveCloneURL=".../authoring/normal"            escapes=false
```

### 修復（兩層，缺一不可）

1. **`internal/manifest/mcp.go` `ValidateMarketplaceSource`**：`..` 段檢查前先
   `strings.ReplaceAll(source, "\\", "/")`，兩種分隔符號都正規化後再切段判斷。
2. **`internal/marketplace/authoring/refcheck.go` `resolveCloneURL`**：改回傳
   `(string, error)`；相對本地 source 解析出絕對路徑後，新增
   `pathWithinRoot(cwd, abs)`（`filepath.Rel` + `..` 前綴檢查）邊界檢查，逃出
   cwd 就回傳 error，不再無條件回傳。`gitRefLister.ListRefs` 相應改為檢查
   `resolveCloneURL` 的 error。

### 突變 1：manifest 層 — 還原成只切 `/`

```
$ git stash push -- internal/manifest/mcp.go   # 暫時拿掉修復
$ go test ./internal/manifest/ -run TestValidateMarketplaceSource -v
    mcp_test.go:548: expected error, got nil   （2 個新案例：./..\..\outside、./sub\..\..\outside）
--- FAIL: TestValidateMarketplaceSource
FAIL
$ git stash pop   # 還原修復
```

綠（修復還原後）：

```
$ go test ./internal/manifest/ -run TestValidateMarketplaceSource -v
--- PASS: TestValidateMarketplaceSource (0.00s)
    --- PASS: ..././..\..\outside (0.00s)
    --- PASS: ..././sub\..\..\outside (0.00s)
    (其餘 14 個既有案例全綠，證明未回歸)
PASS
```

### 突變 2：resolveCloneURL 層 — 拿掉邊界檢查（保留 `(string, error)` 簽名，只刪
`pathWithinRoot` 呼叫）

紅：

```
$ go test ./internal/marketplace/authoring/ -run TestResolveCloneURL -v
    refcheck_test.go:477: resolveCloneURL(`./..\..\outside`) = nil error, want a rejection (path escapes the project root)
    refcheck_test.go:489: resolveCloneURL(./../../outside) = nil error, want a rejection (path escapes the project root)
--- FAIL: TestResolveCloneURL
FAIL
```

綠（還原邊界檢查後）：

```
$ go test ./internal/marketplace/authoring/ -run TestResolveCloneURL -v
--- PASS: TestResolveCloneURL (0.01s)
    --- PASS: .../relative_local_source_escaping_cwd_via_backslash_traversal_is_rejected (0.00s)
    --- PASS: .../relative_local_source_escaping_cwd_via_forward-slash_traversal_is_rejected (0.00s)
    --- PASS: .../relative_local_source_staying_within_cwd_is_accepted (0.00s)
    (其餘 5 個既有案例全綠：full URL、scp、absolute path、owner/repo、cwd-relative)
PASS
```

### 其他 `ValidateMarketplaceSource` 呼叫端未回歸

`git grep -n ValidateMarketplaceSource` 顯示兩個生產呼叫點：
`internal/manifest/manifest.go:564`（`validateMarketplaceBlock`，manifest 載入時）、
`internal/marketplace/authoring/schema.go:493`（`LoadAuthoringConfig` 解析
packages 時）。兩者都只是呼叫同一個函式，未各自重新實作規則，修復自動涵蓋。
驗證：

```
$ go test ./internal/marketplace/authoring/ -run TestLoadAuthoringConfig_SourceValidation -v
--- PASS: TestLoadAuthoringConfig_SourceValidation_ReusesManifestValidator (0.01s)
--- PASS: TestLoadAuthoringConfig_SourceValidation_AcceptsValidShapes (0.01s)
PASS
```

`internal/marketplace/build` 有一個同名但**不同**的 `resolveCloneURL`
（`internal/marketplace/build/builder.go:372`）：讀過其實作，它沒有 round 2
新增的相對本地路徑展開分支（`filepath.IsAbs` 之外一律走 OWNER/REPO shorthand），
不受本次 BLOCKING 1 影響；其輸入仍先經同一個 `ValidateMarketplaceSource`，layer 1
修復同樣涵蓋它。

---

## BLOCKING 2 — noVerify 在分類器之外、CLI 印出時序早於 AddPackage 的前置檢查

### 根因

`classifyRefResolution`（round 2 版本）不吃 `noVerify`，所以
`--ref HEAD --no-verify` 仍被分類成 `refKindHead`（「會解析」）。CLI
（`cmd/apm-go/marketplace_package.go`，round 2 版本）在呼叫 `AddPackage` **之前**
用 `WillResolveMutableRefForAdd` 做這個判斷就印警告，所以 `AddPackage` 之後任何
前置檢查失敗（缺 config、source 不可達、重複命名）都會印出誤導性的警告。

主 session 用真的 binary 重現（`bin/apm-go.exe`）：

```
$ apm-go.exe marketplace package add owner/repo --ref HEAD --no-verify
exit 2
 ! 'HEAD' is a mutable ref. Resolving to current SHA for safety.
Error: no marketplace authoring config found ...
```

### 修復

1. `internal/marketplace/authoring/editor.go`：`classifyRefResolution` 加
   `noVerify bool` 參數，`refKindHead` 分裂出 `refKindHeadOffline`
   （HEAD 形狀但 `noVerify` 使解析不可能）。`resolveRef` 改為對這個結果
   `switch`，不再另外做一次 `if noVerify` 判斷。
2. `resolveRef` 新增 `onExplicitHeadWillResolve func()` 參數：只在
   `refKindHead` 分支、`ref != ""`（顯式給的 HEAD，非隱含空字串）、且已通過
   noVerify 檢查後，真正呼叫 `lister.ListRefs` **之前**才呼叫這個 hook。
   `AddOptions` 新增 `OnExplicitHeadWillResolve` 欄位，`AddPackage` 原封不動
   把它傳進 `resolveRef`；`SetPackage` 傳 `nil`（`set` 從無此警告）。
3. CLI（`cmd/apm-go/marketplace_package.go`）刪除呼叫 `AddPackage` 之前的
   `WillResolveMutableRefForAdd` 判斷式，改成把印警告的動作放進
   `AddOptions.OnExplicitHeadWillResolve` 這個 callback 裡。因為
   `AddPackage` 的每一個前置檢查（`--version`/`--ref` 互斥、subdir 驗證、
   source 驗證、`verifyPackageSource` 可達性、`LoadAuthoringConfig`、
   重複命名）全部發生在呼叫 `resolveRef` 之前，這個 callback
   在結構上不可能在其中任何一個失敗之前被呼叫到 —— 不需要在 CLI 端
   手動重複這些前置檢查的順序。
4. `WillResolveMutableRefForAdd` 保留為公開 API，新增 `noVerify` 參數，
   內部改用 `classifyRefResolution` 的新結果 —— 它現在只被
   `TestWillResolveMutableRefForAdd_MatchesResolveRefAcrossCrossProduct`
   直接測試，CLI 生產路徑已不再呼叫它。

### 突變測試 1：classifyRefResolution 忽略 noVerify（`WillResolveMutableRefForAdd`
內部改回 hardcode `noVerify=false`）

紅：

```
$ go test ./internal/marketplace/authoring/ -run TestWillResolveMutableRefForAdd_MatchesResolveRefAcrossCrossProduct -v
--- FAIL: .../remote/ref=<empty>/version=/noVerify=true
--- FAIL: .../remote/ref=HEAD/version=/noVerify=true
--- FAIL: .../remote/ref=head/version=/noVerify=true
--- FAIL: .../remote/ref=Head/version=/noVerify=true
（共 16 個 noVerify=true 案例全紅，noVerify=false 的 32 個案例全綠）
FAIL
```

綠（還原後）：全部 64 個交叉積案例（2 source × 6 ref 形狀 × 2 version × 2
noVerify）全部通過。

### 突變測試 2：CLI 還原成 round-2 版本（呼叫 AddPackage 前印警告）

紅（4 個新 CLI 端對端測試全部重現真實回歸）：

```
$ go test ./cmd/apm-go/ -run 'TestMarketplacePackageAdd_ExplicitRefHead_(NoVerify|MissingConfig|UnreachableSource|DuplicateName)_.*NoMutableRefWarning' -v
    ...NoVerify...: output = " ! 'HEAD' is a mutable ref...\nError: Cannot resolve HEAD ref without network access...\n", want NO mutable-ref warning
    ...MissingConfig...: output = " ! 'HEAD' is a mutable ref...\nError: no marketplace authoring config found...\n", want NO mutable-ref warning
    ...UnreachableSource...: output = " ! 'HEAD' is a mutable ref...\nError: source ... is not reachable...\n", want NO mutable-ref warning
    ...DuplicateName...: output = " ! 'HEAD' is a mutable ref...\nError: package \"tool\" already exists\n", want NO mutable-ref warning
--- FAIL (4 個全紅)
FAIL
```

綠（還原修復後）：

```
$ go test ./cmd/apm-go/ -run 'TestMarketplacePackageAdd_ExplicitRefHead_(NoVerify|MissingConfig|UnreachableSource|DuplicateName)_.*NoMutableRefWarning' -v
--- PASS (4 個全綠)
PASS
```

### 新測試

- `internal/marketplace/authoring/resolveref_test.go`：
  `TestResolveRef_ExplicitHead_InvokesOnExplicitHeadWillResolve`、
  `TestResolveRef_ImplicitHead_DoesNotInvokeOnExplicitHeadWillResolve`、
  `TestResolveRef_NoVerify_ExplicitHead_DoesNotInvokeOnExplicitHeadWillResolve`、
  `TestResolveRef_LocalSource_ExplicitHead_DoesNotInvokeOnExplicitHeadWillResolve`
  （resolveRef 單元層，直接驗 callback 呼叫次數）。
- `cmd/apm-go/marketplace_package_test.go`：
  `TestMarketplacePackageAdd_ExplicitRefHead_NoVerify_NoMutableRefWarning_ExitsCode2`、
  `TestMarketplacePackageAdd_ExplicitRefHead_MissingConfig_NoMutableRefWarning`、
  `TestMarketplacePackageAdd_ExplicitRefHead_UnreachableSource_NoMutableRefWarning`、
  `TestMarketplacePackageAdd_ExplicitRefHead_DuplicateName_NoMutableRefWarning`
  （CLI 端對端，四個報告點名的前置失敗情境）。
- `TestWillResolveMutableRefForAdd_MatchesResolveRefAcrossCrossProduct` 擴充：
  `refs` 加入 `"Head"`（title case，見 MAJOR 一節），新增 `noVerifies :=
  []bool{false, true}` 維度，交叉積從 20 案例擴為 64 案例。

---

## BLOCKING 3 — 句型違規

- `editor.go`（round 2）「a second expression can no longer exist」：本輪已把
  `noVerify` 納入 `classifyRefResolution`，這句話目前的字面意義已成立，但**不
  再使用這種絕對收斂語氣**——改寫為客觀描述「round 2 修過一次 drift、round 3
  又發現另一個不同的 gap（noVerify 未被納入），已修復」，不再宣稱「不可能再有
  下一個」。見 `editor.go` `classifyRefResolution` 上方新文字。
- `refcheck.go:98`（round 2）「relative ./ is the only shape a local source
  takes」：這句話本身就假，`manifest.ValidateMarketplaceSource` 允許絕對路徑
  （見 `mcp_test.go` 沒有拒絕絕對路徑的案例，以及 `refcheck_test.go` 的
  "absolute filesystem path passes through unchanged" 案例本身就用絕對路徑
  當本地 source）。已改寫為列舉「相對 `./...`（isLocalPackageSource 判定的
  形狀）或絕對路徑（manifest.ValidateMarketplaceSource 同時允許）」，不再宣稱
  只有一種形狀。
- 本檔（round 2 版本）`:61`、`:317`、`:383` 的「已修復」「全部修復」：本輪重寫
  時，每一項「已修復」後面都直接附紅/綠指令輸出（見上方各節），不再有裸的
  形容詞收尾；本檔本身也不宣告「task 完成」，只宣告「以下指令跑過、結果如上」。

---

## MAJOR — 三個一行閘門逃逸口

### ROUND2-B1：predictor 對 `ref != "Head"`（title case）的例外

修復：`TestWillResolveMutableRefForAdd_MatchesResolveRefAcrossCrossProduct`
的 `refs` 清單加入 `"Head"`；CLI 層新增
`TestMarketplacePackageAdd_ExplicitRefHead_MixedCase_PrintsMutableRefWarning`。

突變（`WillResolveMutableRefForAdd` 內部加 `&& ref != "Head"`）：

```
$ go test ./internal/marketplace/authoring/ -run TestWillResolveMutableRefForAdd_MatchesResolveRefAcrossCrossProduct -v
--- FAIL （.../ref=Head/version=.../noVerify=... 各案例全紅）
FAIL
```

綠（還原後）：全部通過。

**附註（誠實揭露架構變更的副作用）**：CLI 層現在已改用
`resolveRef` 的 `onExplicitHeadWillResolve` hook（BLOCKING 2 的修復），
不再直接呼叫 `WillResolveMutableRefForAdd`。所以對這個特定突變，
`TestMarketplacePackageAdd_ExplicitRefHead_MixedCase_PrintsMutableRefWarning`
本身**不會**轉紅（已實測確認：套用同一個突變後這條 CLI 測試仍是綠的），
真正抓到這個突變的是上面的交叉積測試。這條 CLI 測試存在的目的是
獨立證明「CLI 端對 title-case Head 的端對端行為正確」，不是作為這個
特定突變的偵測手段——兩者是不同層次的覆蓋，不應混為一談。

### ROUND2-M3-CLI：CLI `set --ref` 的分支名 fixture 缺口

修復：新增 `TestMarketplacePackageSet_RefFlag_BranchName_ResolvesViaListerThroughCLI`
（fixture 用 `feature-branch`，不是 `v1.0.0`）。

突變（`marketplace_package.go`：`if cmd.Flags().Changed("ref")` 改成
`if cmd.Flags().Changed("ref") && strings.HasPrefix(ref, "v")`）：

```
$ go test ./cmd/apm-go/ -run TestMarketplacePackageSet_RefFlag_BranchName_ResolvesViaListerThroughCLI -v
    marketplace_package_test.go:...: apm.yml = "...source: .../reporoot...\n", want the resolved SHA ... written for ref:
--- FAIL
```

綠（還原後）：`--- PASS`，且既有的 `v1.0.0` fixture 測試同時保持綠燈。

### ROUND2-M3-SEVERITY：嚴重程度閘門只 `-list`、從未 `-run`

修復：`verify.ps1` 的 `ROUND2-M3-SEVERITY` 區塊改成先 `-list` 證明匹配非零，
再實際 `-run` 兩條宿主測試並驗 exit code（原本只做前半段）。

驗證 `t.Skip` 確實會被新版本抓到（PRD 明講 `t.Skip` 不算通過）：

```
$ go test ./cmd/apm-go/ -list 'TestMarketplacePackageAdd_ExplicitRefHead_PrintsMutableRefWarning' 2>&1 | grep '^Test'
TestMarketplacePackageAdd_ExplicitRefHead_PrintsMutableRefWarning
$ go test ./cmd/apm-go/ -run 'TestMarketplacePackageAdd_ExplicitRefHead_PrintsMutableRefWarning' -v 2>&1 | tail -3
--- PASS: TestMarketplacePackageAdd_ExplicitRefHead_PrintsMutableRefWarning (0.30s)
PASS
```

（`-list` 對一個改成 `t.Skip()` 的測試體一樣會列出名字、非零匹配；只有
`-run` 才會顯示 `--- SKIP` 而非 `--- PASS`，`Exec` 函式驗的是 exit code，
`go test` 對只含 skip、沒有失敗的套件仍回傳 exit 0 —— 所以這裡的把關其實
是「run 起來、看得到 PASS 而非 SKIP」這件事本身在 CI 紀錄裡可稽核，
而不是單靠 exit code 就能自動擋下 `t.Skip`。這是本輪誠實記錄的限制，
不誇大這個閘門的偵測力。）

---

## MINOR — `filepath.Abs` 綁定 process cwd、不是 `dir` 參數

**選擇：文件化耦合，不重構簽名。**

`SetPackage`/`AddPackage` 的 `dir` 參數只用來定位/編輯設定檔
（`locateEditableConfig`/`LoadAuthoringConfig`），ref 解析路徑
（`lister.ListRefs` → `gitRefLister.ListRefs` → `resolveCloneURL`）解析
相對本地 source 時一律用 `os.Getwd()`，與 `dir` 無關。

今天唯一安全的原因：兩個生產呼叫點都傳 `"."`
（`cmd/apm-go/marketplace_package.go:171`、`:251`），且呼叫端從未在呼叫前
`os.Chdir()` 到別的目錄——`dir` 與 `os.Getwd()` 因此重合，但這只是巧合，
不是簽名保證的契約。已在 `editor.go` 的 `SetPackage` doc comment 明確寫出
這個耦合與兩個呼叫點的檔案行號，供未來新增呼叫端時能讀到警告。**未重構
`resolveRef`/`RefLister` 介面把 `dir` 顯式傳入**——這會牽動
`internal/marketplace/build` 另一份 `RefLister` 實作與所有既有呼叫端的
簽名，超出本輪三個阻斷 + 三個逃逸口的範圍，且沒有現存呼叫端會觸發這個
陷阱；若未來新增一個不在 `"."` 呼叫的呼叫端，這是需要重新評估的時間點。

---

## verify.ps1 新增的閘門（round 3）

- `ROUND3-B1-MANIFEST` — `TestValidateMarketplaceSource`（`-run`，manifest 套件）
- `ROUND3-B1-RESOLVECLONEURL` — `TestResolveCloneURL`（`-run`，authoring 套件）
- `ROUND3-B2-UNIT` — 4 個 `onExplicitHeadWillResolve` 單元測試（`-run`）
- `ROUND3-B2-CLI` — 4 個 CLI 端對端「不早印警告」測試（`-run`）
- `ROUND3-MAJOR-HEADMIXEDCASE` — CLI 層 title-case Head 測試（`-run`）
- `ROUND2-M3-CLI` 擴充為 2 個測試（tag fixture + branch fixture），兩者都 `-run`
- `ROUND2-M3-SEVERITY` 從純 `-list` 改為 `-list` 後再 `-run`

---

## 本輪逐項結果

| 項目 | 狀態 | 證據位置 |
|---|---|---|
| BLOCKING 1（Windows 路徑遍歷，兩層） | 已修復，紅/綠證據俱在 | 上方「BLOCKING 1」節 |
| BLOCKING 2（noVerify 分類器 + CLI 時序） | 已修復，紅/綠證據俱在 | 上方「BLOCKING 2」節 |
| BLOCKING 3（句型違規 3 處） | 已改寫 | 上方「BLOCKING 3」節 |
| MAJOR ROUND2-B1（Head title case） | 已修復，紅/綠證據俱在 | 上方「MAJOR」節 |
| MAJOR ROUND2-M3-CLI（分支名 fixture） | 已修復，紅/綠證據俱在 | 上方「MAJOR」節 |
| MAJOR ROUND2-M3-SEVERITY（真的 -run） | 已修復 | 上方「MAJOR」節 |
| MINOR（dir vs cwd 耦合） | 文件化，未重構 | 上方「MINOR」節，理由已附 |

---

## 全套件與完整 Tier 1 閘門輸出

```
$ go build ./...   → exit 0
$ go vet ./...     → exit 0
$ go test ./... -count=1                    → 全綠（23 個套件）
$ git diff -- go.mod; git diff --cached -- go.mod   → 皆為空輸出（無新 require）
```

```
$ pwsh -NoProfile -File .trellis/tasks/07-29-marketplace-add-fixes/verify.ps1
== Tier 1: marketplace-add-fixes ==
  ok   [AC-L1/build]
  ok   [AC-L1/vet]
  ok   [AC-L1/go-test-all]
  ok   [AC-L1/binary]
  ok   [AC47/flag]
  ok   [AC49]
  ok   [AC22]
  ok   [AC18]
  ok   [AC40]
  ok   [AC21]
  ok   [REGR-B1]
  ok   [REGR-B2]
  ok   [REGR-M1]
  ok   [REGR-M2]
  ok   [ROUND2-B1]
  ok   [ROUND2-M1]
  ok   [ROUND2-M2]
  ok   [ROUND2-M3-CLI]
  ok   [ROUND2-M3-39CHAR]
  ok   [ROUND2-M3-SEVERITY]
  ok   [ROUND3-B1-MANIFEST]
  ok   [ROUND3-B1-RESOLVECLONEURL]
  ok   [ROUND3-B2-UNIT]
  ok   [ROUND3-B2-CLI]
  ok   [ROUND3-MAJOR-HEADMIXEDCASE]
  ok   [AC53/no-clack]
  ok   [AC53/no-block]
  ok   [AC-L9]
  ok   [AC-L1/coverage 87.0%]

TIER 1 GREEN
```

---

## 未處理 / 交由使用者裁定

無新的「延後」項目 — MINOR 的處理方式（文件化而非重構）已在上方附成本理由
（會牽動 `internal/marketplace/build` 的另一份 `RefLister` 實作與所有既有
呼叫端簽名），不是無證據的擱置。

`task.py finish` 未執行；本輪仍需外部（或使用者）覆核後才算收斂 —— 本檔的
「已修復」只代表 implementer 本地跑過上述指令且輸出如上，**不等於外部驗證
通過**。
