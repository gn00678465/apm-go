# 驗證紀錄 — 07-29-marketplace-add-fixes（外部稽核第三輪 + 第四輪修復）

> **狀態：implementer 已跑完下列指令，尚未經外部覆核。`task.py finish` 未執行。**
> 本檔逐項列出可證偽的紅/綠證據；任何「已修復」字樣後面都附著實際跑過的指令與
> 輸出。round 2 的驗證紀錄被 round 3 稽核指出兩處推翻（BLOCKING 1 structural
> fix 有 Windows 逃逸漏洞、BLOCKING 2 的 CLI 印出時序仍錯）；round 3 的驗證
> 紀錄本身又被 round 4 稽核指出多處事實錯誤（呼叫點數量、交叉積案例數、
> 自相矛盾的段落，見下方 BLOCKING 4 節）——round 3 內文保留在上半部，round 4
> 的更正以「round 4 更正」標記內嵌其中，新發現的阻斷/一般問題另闢「Round 4」
> 節在本檔下半部。

- 實作者：`trellis-implement`（round 3、round 4）
- 外部稽核（round 3）：main session 獨立重現後轉交 — 1 個 SECURITY 阻斷（Windows
  路徑遍歷）、1 個一般阻斷（noVerify 分類器缺口 + CLI 印出時序）、句型違規 3 處、
  3 個一行閘門逃逸口、1 個 MINOR
- 外部稽核（round 4）：main session 獨立重現 BLOCKING 1、確認 BLOCKING 4 的
  證據後轉交 — 4 個阻斷（絕對/UNC 路徑、symlink 逃逸、`set --ref HEAD` 缺警告、
  本檔自己的證據錯誤）、5 個一般（耦合 oracle、t.Skip 逃逸、mixed-case 次數/
  嚴重程度、AC-L9 空閘門、未知 kind fail-open）
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

**round 4 更正（BLOCKING 4，外部稽核第四輪，2026-07-30）**：這裡原本寫「兩個
生產呼叫點」，被外部稽核指出實際是 3 個，main session 用
`git grep -n "ValidateMarketplaceSource("` 獨立確認過。第三個呼叫點
（`internal/marketplace/authoring/editor.go`
的 `AddPackage`，round 3 委交前就已存在——`git show f8021ef:.../editor.go`
可證，round 3 的統計本身就漏數了它，不是本輪新增）當時就沒被算進這裡的
「兩個」。逐一列出正確的三個：`internal/manifest/manifest.go:564`
（`validateMarketplaceBlock`，manifest 載入時）、
`internal/marketplace/authoring/schema.go:493`（`LoadAuthoringConfig`
解析 packages 時）、`internal/marketplace/authoring/editor.go`
的 `AddPackage`（round 4 之後行號在 673，因本輪在其上方新增了文件註解，
之前是 630）。三者都只是呼叫同一個函式，未各自重新實作規則，修復同樣自動
涵蓋三者。
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

綠（還原後）：全部 48 個交叉積案例（2 source × 6 ref 形狀 × 2 version × 2
noVerify = 48，**round 4 更正**：round 3 版本這裡誤植為「64 個」，且與同一
節上方「16 個 noVerify=true 全紅、noVerify=false 的 32 個全綠」自己算出的
16+32=48 互相矛盾——BLOCKING 4，外部稽核第四輪，2026-07-30 點名）全部通過。

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
  []bool{false, true}` 維度，交叉積從 20 案例（2×5×2）擴為 48 案例
  （2×6×2×2）。**round 4 更正**：round 3 版本這裡誤植為「64 案例」
  （BLOCKING 4，外部稽核第四輪，2026-07-30）。

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

round 3 修復：`verify.ps1` 的 `ROUND2-M3-SEVERITY` 區塊改成先 `-list` 證明
匹配非零，再實際 `-run` 兩條宿主測試並驗 exit code（原本只做前半段）。

**round 4 更正（BLOCKING 4，外部稽核第四輪，2026-07-30）**：round 3 版本
這裡的標題「驗證 `t.Skip` 確實會被新版本抓到」與正文自相矛盾——正文自己
承認「`go test` 對只含 skip、沒有失敗的套件仍回傳 exit 0」，也就是
`Exec`（只驗 exit code）**其實抓不到** `t.Skip`；round 3 把「run 起來人
工看得到 PASS 而非 SKIP」說成是把關，但這只是「CI 紀錄裡可稽核」，不是
「自動擋下」，兩者不是同一件事，round 3 版本把兩者混為一談。

MAJOR 2（外部稽核第四輪，2026-07-30）把這個真正修好了：新增
`ExecTestJSON`（`verify.ps1`）改用 `go test -json`，逐一讀每個測試的
`Action` 欄位，要求「有出現過 `pass`」且「從未出現 `skip`」——這是自動化
判斷，不需要人工看 `-v` 輸出。`ROUND2-M3-SEVERITY` 本身已改接這個新機制。
證據（把其中一條測試體暫時改成 `t.Skip(...)` 後）：

```
$ go test ./cmd/apm-go/ -run 'TestMarketplacePackageAdd_ExplicitRefHead_PrintsMutableRefWarning' 2>&1; echo "exit=$?"
exit=0        # 仍是 exit 0 -- 證實正文原本的說法（Exec 抓不到）
$ go test ./cmd/apm-go/ -json -run 'TestMarketplacePackageAdd_ExplicitRefHead_PrintsMutableRefWarning' 2>&1 | grep '"Action":"skip"'
{"Time":"...","Action":"skip","Package":"...","Test":"TestMarketplacePackageAdd_ExplicitRefHead_PrintsMutableRefWarning"}
```

`ExecTestJSON` 看到這個 `Action:"skip"` 行就會回報失敗，即使 exit code 是
0。完整的紅/綠證據見本檔案 BLOCKING 3／MAJOR 2 一節（下方新增段落）。

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

## 本輪（round 3）逐項結果

**round 4 更正（BLOCKING 4，外部稽核第四輪，2026-07-30）**：這張表原本每一
列只寫「已修復」，沒有重複「implementer 本地跑過、未經外部覆核」這個限定
——本檔最上方（`:3`）和最下方（原 `:404-406`）雖然都寫了這個限定，但表格
本身讀起來像一個獨立、無條件的完成宣告，與上下兩處的限定互相矛盾（一個讀者
只看這張表會誤以為「外部已核可」）。下面每一列都補回這個限定，不再只在頭尾
各寫一次。

| 項目 | 狀態（implementer 本地紅/綠證據，**未經外部覆核**） | 證據位置 |
|---|---|---|
| BLOCKING 1（Windows 路徑遍歷，兩層） | 紅/綠證據俱在 | 上方「BLOCKING 1」節 |
| BLOCKING 2（noVerify 分類器 + CLI 時序） | 紅/綠證據俱在 | 上方「BLOCKING 2」節 |
| BLOCKING 3（句型違規 3 處） | 已改寫（無紅/綠適用，屬文字修訂） | 上方「BLOCKING 3」節 |
| MAJOR ROUND2-B1（Head title case） | 紅/綠證據俱在 | 上方「MAJOR」節 |
| MAJOR ROUND2-M3-CLI（分支名 fixture） | 紅/綠證據俱在 | 上方「MAJOR」節 |
| MAJOR ROUND2-M3-SEVERITY（真的 -run） | 紅/綠證據俱在，**但 round 4 發現這條閘門本身仍有 t.Skip 逃逸口，見下方 round 4 節** | 上方「MAJOR」節 |
| MINOR（dir vs cwd 耦合） | 文件化，未重構 | 上方「MINOR」節，理由已附 |

round 4（外部稽核第四輪）逐項結果見本檔最下方新增的「round 4 逐項結果」節。

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

## round 3 的「未處理 / 交由使用者裁定」

無新的「延後」項目 — MINOR 的處理方式（文件化而非重構）已在上方附成本理由
（會牽動 `internal/marketplace/build` 的另一份 `RefLister` 實作與所有既有
呼叫端簽名），不是無證據的擱置。

---

# Round 4（外部稽核第四輪，2026-07-30）

> main session 獨立重現了 BLOCKING 1、確認了 BLOCKING 4 的證據；4 個阻斷
> （BLOCKING 1-4）+ 5 個一般（MAJOR 1-5）。以下逐項紅/綠證據；**同上，
> implementer 本地跑過不等於外部驗證通過，`task.py finish` 未執行。**

## BLOCKING 1 — 絕對/UNC 路徑完全繞過邊界檢查

根因：`internal/manifest/mcp.go` 的 `ValidateMarketplaceSource` 只切段找
`".."`，從沒檢查過「這整串字面上就是一個絕對路徑/UNC 路徑」——一個不以
`"."` 開頭、也不含 `"://"` 的字串（`D:\outside\repo`、
`C:\Windows\Temp\evil`、`\\server\share\repo`、`/etc/passwd`）直接落到最後
「shorthand form -- accepted」分支被接受。main session 用 Go 探針重現：

```
src="D:\\outside\\repo"       validate=PASS  resolveCloneURL err=false
src="C:\\Windows\\Temp\\evil" validate=PASS  resolveCloneURL err=false
src="\\\\server\\share\\repo" validate=PASS  resolveCloneURL err=false
src="./..\\..\\outside"       validate=FAIL  resolveCloneURL err=true   (round-3 修復仍有效)
```

修復：`ValidateMarketplaceSource` 新增 `isAbsoluteOrUNCSource`（純字串前綴
判斷，不用 OS-native 的 `filepath.IsAbs`，見函式註解），在 `..` 段檢查
**之前**攔截。

紅（暫時拿掉修復）：

```
$ git stash push -- internal/manifest/mcp.go
$ go test ./internal/manifest/ -run TestValidateMarketplaceSource -v
    mcp_test.go:561: expected error, got nil   (D:\outside\repo)
    mcp_test.go:561: expected error, got nil   (C:\Windows\Temp\evil)
    mcp_test.go:561: expected error, got nil   (\\server\share\repo)
    mcp_test.go:561: expected error, got nil   (/etc/passwd)
    mcp_test.go:561: expected error, got nil   (//server/share/repo)
--- FAIL: TestValidateMarketplaceSource
FAIL
$ git stash pop
```

綠（還原修復）：

```
$ go test ./internal/manifest/ -run TestValidateMarketplaceSource -v
--- PASS: TestValidateMarketplaceSource (0.00s)
    (全部 21 案例，含新增的 5 個絕對/UNC 案例，全綠)
PASS
```

### 決定：只修 `ValidateMarketplaceSource`，`resolveCloneURL` 的 `filepath.IsAbs`
分支維持原樣

`resolveCloneURL`（`internal/marketplace/authoring/refcheck.go`）自己也有
一個 `if filepath.IsAbs(source) { return source, nil }` 分支，同樣沒有邊界
檢查。**沒有動它**，理由（已寫進該函式自己的 doc comment）：

1. 三個生產呼叫點（`manifest.go:564`、`schema.go:493`、
   `editor.go` 的 `AddPackage`）全都先呼叫 `ValidateMarketplaceSource`，
   驗證通過後才可能讓 `source` 走到 `resolveCloneURL` —— 修好驗證層即可
   讓這個分支在生產路徑上永遠收不到絕對/UNC 路徑，等同已關閉。
2. `internal/marketplace/authoring` 套件自己的單元測試（`refcheck_test.go`
   的 `TestCheckPackages`/`TestOutdatedPackages` 等、`editor_test.go` 的
   `AddPackage`/`SetPackage` 測試）大量直接在記憶體建構
   `PackageEntry{Source: <絕對路徑>}`，刻意繞過 `ValidateMarketplaceSource`
   （這是它們自己的既有慣例，不是本輪新增），依賴 `resolveCloneURL` 對絕對
   路徑原樣接受。若連這裡也收緊，會牽動 `refcheck_test.go`/`editor_test.go`
   數十個既有測試的 fixture，且不會增加生產路徑上的防護（因為驗證層已經
   堵住唯一的生產入口）。

殘留風險已寫進 doc comment：若未來有新呼叫端繞過
`LoadAuthoringConfig`/`ValidateMarketplaceSource` 直接建構
`PackageEntry`/呼叫 `AddPackage`/`SetPackage`，這個分支仍會原樣接受絕對
路徑。這不是「聲稱已完全消除」，是明寫的已知邊界。

### 需要轉換掉絕對路徑 fixture 的測試（逐一列出）

以下測試原本把一個真實 git repo 的**絕對路徑**直接當 marketplace source
字串使用，通過 `ValidateMarketplaceSource`/`AddPackage`/`SetPackage`/
`LoadAuthoringConfig` 這條驗證鏈——round 4 的驗證器修復後全部會在驗證那一步
就被拒絕，必須轉換。分兩類：

**類別 A（`set` 對 local source 本來就會呼叫 lister，改成 chdir + `./repo`
相對路徑後行為不變）：**
- `cmd/apm-go/marketplace_package_test.go`：
  `TestMarketplacePackageSet_RefFlag_ResolvesViaListerThroughCLI`、
  `TestMarketplacePackageSet_RefFlag_BranchName_ResolvesViaListerThroughCLI`

**類別 B（`add`/`check`/`outdated` 對 local source 從不觸網，改成相對路徑
會讓測試意圖落空——改用 `withFixtureRemoteLister`／`fixtureRemoteBuildLister`：
CLI 端 source 字串換成合規的 `"owner/repo"`，同時把
`authoring.DefaultRefLister`/`build.DefaultRefLister` 暫時換成一個忽略
source、直接對固定絕對路徑跑真的 `git ls-remote` 的測試替身）：**
- `cmd/apm-go/marketplace_package_test.go`：
  `TestMarketplacePackageAdd_RemoteSource_GoesThroughLsRemote_RealGitFixture`、
  `TestMarketplacePackageAdd_UnreachableRemoteSource_Fails`、
  `TestMarketplacePackageAdd_ZeroFlags_RemoteSource_WritesResolvedHeadSHA`、
  `TestMarketplacePackageAdd_ExplicitRefHead_PrintsMutableRefWarning`、
  `TestMarketplacePackageAdd_ExplicitRefHead_MixedCase_PrintsMutableRefWarning`、
  `TestMarketplacePackageAdd_ExplicitRefHead_MissingConfig_NoMutableRefWarning`、
  `TestMarketplacePackageAdd_ExplicitRefHead_UnreachableSource_NoMutableRefWarning`、
  `TestMarketplacePackageAdd_ExplicitRefHead_DuplicateName_NoMutableRefWarning`、
  `TestMarketplacePackageAdd_VersionGiven_DoesNotWriteRef`
- `cmd/apm-go/marketplace_authoring_test.go`：
  `TestMarketplaceCheck_RemotePackagePinnedRefFound_RealGitFixture`、
  `TestMarketplaceCheck_RemotePackagePinnedRefMissing_ExitsNonZero`、
  `TestMarketplaceOutdated_UpgradablePackage_ExitsNonZero`、
  `TestMarketplaceOutdated_NoMatchingTags_DoesNotExitNonZero`、
  `TestMarketplaceOutdated_FetchFailure_DoesNotExitNonZero`
- `cmd/apm-go/pack_test.go`：
  `TestPackCmd_RemotePackage_ResolvesAgainstRealGitTags`
  （用 `internal/marketplace/build` 自己的 `DefaultRefLister`/`RemoteRef`
  型別，新增 `fixtureRemoteBuildLister`；注意 `build.RemoteRef.Name` 保留
  完整 `refs/tags/...` 前綴，跟 `authoring` 的 `semver.TagInfo` 不同，第一次
  實作時因為沿用 authoring 的「去前綴」寫法而 0 候選 tag，重新讀
  `reflister.go` 的 doc comment 才發現要保留前綴）。

**類別 C（in-package 單元測試，不經 CLI 驗證，但 `AddPackage`/`SetPackage`
自己會呼叫 `ValidateMarketplaceSource`）：**
- `internal/marketplace/authoring/editor_test.go`：
  `TestAddPackage_RefHEAD_ResolvesToConcreteSHA`（新增 `realRepoLister`
  測試替身，直接重用 `newListRefsCmd`/`parseRefsOutput`，跳過
  `resolveCloneURL` 的字串轉換）

**沒有轉換、維持原樣的**：`internal/marketplace/authoring/refcheck_test.go`
的 `TestCheckPackages`/`TestOutdatedPackages` 等（直接建構
`AuthoringConfig{Packages: []PackageEntry{{Source: <絕對路徑>, ...}}}`，
從不呼叫 `ValidateMarketplaceSource`，不受本輪修復影響，見上一節「決定」）。

---

## BLOCKING 2 — `pathWithinRoot` 是純字串比對，symlink 仍可逃逸

根因：`pathWithinRoot`（`refcheck.go`）只用 `filepath.Rel` 做字串層級的
`".."` 判斷，從不碰檔案系統。一個實際存在、位於專案根**內**、但指向根外
的目錄 symlink，其路徑字串本身不含任何 `".."`，`filepath.Rel` 判定「在
root 內」，但 `git ls-remote` 在 OS 層會真的跟著 symlink 走出去。

紅（探針，之後刪除）：

```
pathWithinRoot(project, project/linked-pointing-outside) = true (want false)
BUG REPRODUCED: symlink escape not detected
```

修復：`pathWithinRoot` 先做原本的字串檢查（更名為
`pathWithinRootLexical`），再用 `filepath.EvalSymlinks` 分別解析 root 與
target 的真實路徑，對這組「已展開 symlink」的路徑再檢查一次。任一邊
`EvalSymlinks` 失敗（路徑還不存在）則放行給字串檢查的結果 -- 不存在的路徑
沒有 symlink 可以跟。

新測試：`TestResolveCloneURL` 新增
「relative local source escaping cwd via a directory symlink is rejected」
子測試，在無法建立 symlink 的環境（權限不足）會 `t.Skip` 並附清楚訊息，
不靜默跳過。

紅（暫時把 `pathWithinRoot` 改回純字串版本）：

```
$ go test ./internal/marketplace/authoring/ -run TestResolveCloneURL -v
    refcheck_test.go:520: resolveCloneURL(./linked) = nil error, want a rejection (symlink resolves outside the project root)
--- FAIL: TestResolveCloneURL
FAIL
```

綠（還原修復）：

```
$ go test ./internal/marketplace/authoring/ -run TestResolveCloneURL -v
--- PASS: TestResolveCloneURL (0.01s)
    --- PASS: .../relative_local_source_escaping_cwd_via_a_directory_symlink_is_rejected (0.00s)
    (其餘 8 個既有案例全綠)
PASS
```

本機（Windows，本次執行環境）實際建立 symlink 成功、測試真的跑了斷言，
不是走 skip 分支——但這件事本身環境相依（例如 CI 若無 Developer Mode 或
`SeCreateSymbolicLinkPrivilege` 會 skip），`verify.ps1` 的 `ExecTestJSON`
用 `-allowSkip` 明確容許這一條測試 skip（見下方 MAJOR 2 一節），不會把
環境限制誤判成本閘門失敗，但也不會靜默放過「這條測試從沒被 -list 匹配到」
這種更嚴重的情況。

---

## BLOCKING 3 — `set --ref HEAD` 從不印 mutable-ref 警告

根因：`SetPackage`（`editor.go`）呼叫 `resolveRef` 時，`onExplicitHeadWillResolve`
參數硬寫 `nil`。上游 `plugin/set.py:80` 呼叫的 `_resolve_ref` 與
`plugin/__init__.py:120-137` 對 `add`/`set` 是同一份邏輯，兩者都會印警告；
apm-go 這裡只有 `add` 走了這個 hook。

修復：`SetOptions` 新增 `OnExplicitHeadWillResolve func()` 欄位，
`SetPackage` 把它原封不動傳進 `resolveRef`；CLI（`marketplace_package.go`
的 `marketplacePackageSetCmd`）跟 `add` 一樣接上同一句警告文字。

紅（單元層，暫時把 `SetPackage` 的呼叫改回硬寫 `nil`）：

```
$ go test ./internal/marketplace/authoring/ -run 'TestSetPackage_ExplicitRefHead_InvokesOnExplicitHeadWillResolve|TestSetPackage_NonHeadRef_DoesNotInvokeOnExplicitHeadWillResolve' -v
    editor_test.go:948: OnExplicitHeadWillResolve called 0 times, want exactly 1
--- FAIL: TestSetPackage_ExplicitRefHead_InvokesOnExplicitHeadWillResolve
```

紅（CLI 端，暫時拿掉 CLI 的接線）：

```
$ go test ./cmd/apm-go/ -run 'TestMarketplacePackageSet_RefFlagHead_PrintsMutableRefWarning|TestMarketplacePackageSet_RefFlagHead_MixedCase_PrintsMutableRefWarningOnce' -v
    marketplace_package_test.go:375: output = " + Updated package \"tool\"\n", want the mutable-ref warning (BLOCKING 3)
--- FAIL: TestMarketplacePackageSet_RefFlagHead_PrintsMutableRefWarning
--- FAIL: TestMarketplacePackageSet_RefFlagHead_MixedCase_PrintsMutableRefWarningOnce
```

綠（還原兩處修復後）：全部 4 個新測試（單元層 2 個、CLI 層 2 個）全綠。

---

## BLOCKING 4 — 本檔自己的證據錯誤

修正內容見上方各節內嵌的「round 4 更正」段落：
1. `ValidateMarketplaceSource` 生產呼叫點由「2 個」更正為「3 個」，補上
   `editor.go` 的 `AddPackage`（round 3 就漏數了，不是本輪新增的呼叫點）。
2. 交叉積案例數由「64」更正為「48」（2×6×2×2，並修正與同段「16+32」自相
   矛盾的算術）；用 `go test -v | grep -c 'PASS: Test.../'` 實測確認就是
   48 個子測試。
3. `ROUND2-M3-SEVERITY` 一節標題與正文自相矛盾（標題說「t.Skip 確實會被
   抓到」，正文承認「exit code 抓不到」）：改寫為誠實描述現況，並指向
   MAJOR 2 這輪真正做到的 `ExecTestJSON` 修復。
4. 本節「本輪逐項結果」表格每一列補回「未經外部覆核」限定，不再只在頭尾
   各寫一次造成表格本身讀起來像無條件完成宣告。
5. 兩處「structurally impossible」已改寫為附 file:line + 具體依賴條件的
   版本（`editor.go` 的 `resolveRef` doc comment、`marketplace_package.go`
   的 `add` RunE 內嵌註解），不再是無證據的絕對語氣——見這兩個檔案本身。

---

## MAJOR 1 — cross-product 測試是同根同源的耦合 oracle

`WillResolveMutableRefForAdd` 與 `resolveRef` 都呼叫同一個
`classifyRefResolution`，所以對分類器本身的突變（報告點名的
`if noVerify && ref != "Head"`），兩邊會「一起錯得一樣」，`predicted ==
actuallyResolvedViaHead` 仍然成立，舊的比較式測試看不出來。

新增 `TestResolveRef_CrossProduct_MatchesDirectSpecOracle`：對同一組
48 案例，改成從測試自己寫的 spec 表（不呼叫 `classifyRefResolution`/
`WillResolveMutableRefForAdd` 任何一個）直接算出「lister 該不該被呼叫」
「該回什麼值/要不要錯誤」，再跟 `resolveRef` 的實際行為比對。

突變（`classifyRefResolution` 加 `&& ref != "Head"`）：

```
$ go test ./internal/marketplace/authoring/ -run 'TestWillResolveMutableRefForAdd_MatchesResolveRefAcrossCrossProduct|TestResolveRef_CrossProduct_MatchesDirectSpecOracle' -v
--- PASS: TestWillResolveMutableRefForAdd_MatchesResolveRefAcrossCrossProduct   # 舊測試：耦合 oracle，抓不到，還是綠
--- FAIL: TestResolveRef_CrossProduct_MatchesDirectSpecOracle                   # 新測試：獨立算出的 oracle，抓到了
    .../remote/ref=Head/version=/noVerify=true: lister called = false, want true (...)
```

這組紅/綠對照本身就是 MAJOR 1 論點的直接證明：同一個突變，舊測試綠、新
測試紅。還原突變後兩者皆綠。

---

## MAJOR 2 — `-run` 擋不住 t.Skip

`go test -run <pattern>` 對「pattern 匹配到的測試全部 `t.Skip`、沒有任何
`FAIL`」一樣回傳 exit 0，`Exec`（只驗 exit code）因此把它當通過。

修復：`verify.ps1` 新增 `ExecTestJSON`，改用 `go test -json`，要求 pattern
匹配到的每個測試名稱都「出現過 `Action:"pass"`」且「從未出現
`Action:"skip"`」（`-allowSkip` 參數例外處理 BLOCKING 2 那條刻意設計為
環境限制下可見地 skip 的測試，只記一行提示、不算失敗，但仍要求它至少
被 `-list` 匹配到，不能悄悄零匹配）。

突變（把 `TestMarketplacePackageAdd_ExplicitRefHead_PrintsMutableRefWarning`
的測試本體換成 `t.Skip(...)`）：

```
$ go test ./cmd/apm-go/ -run 'TestMarketplacePackageAdd_ExplicitRefHead_PrintsMutableRefWarning'; echo "exit=$?"
ok  	github.com/apm-go/apm/cmd/apm-go	1.462s
exit=0        # 舊機制（Exec）會誤判為通過
$ go test ./cmd/apm-go/ -json -run '...' | grep '"Action":"skip"'
{"Action":"skip","Test":"TestMarketplacePackageAdd_ExplicitRefHead_PrintsMutableRefWarning",...}
$ pwsh -c "...載入 ExecTestJSON 定義後呼叫..."
FAIL [TESTGATE] probe: 下列測試回報 Action=skip（t.Skip 不算通過）: TestMarketplacePackageAdd_ExplicitRefHead_PrintsMutableRefWarning
```

`ExecTestJSON` 正確攔下這個突變；還原突變後 `ROUND2-M3-SEVERITY`（已改接
`ExecTestJSON`）恢復綠燈。

---

## MAJOR 3 — mixed-case hook 測試沒驗次數也沒驗嚴重程度

`add`/`set` 兩邊各自既有的「Head」CLI 測試都只做子字串比對，抓不到「callback
對 Head 觸發兩次」或「Head 專屬降級成 `ux.Info`」這兩種突變。

修復：兩邊都改成同時斷言 `strings.Count(...) == 1`（次數）與
`assertLineSeverity(..., ux.SymbolWarn)`（嚴重程度）：
`TestMarketplacePackageAdd_ExplicitRefHead_MixedCase_PrintsMutableRefWarning`
（就地加強既有測試）、新增
`TestMarketplacePackageSet_RefFlagHead_MixedCase_PrintsMutableRefWarningOnce`。

突變（`add` 的 CLI 接線對 `ref == "Head"` 降級成 `ux.Info`）：

```
$ go test ./cmd/apm-go/ -run TestMarketplacePackageAdd_ExplicitRefHead_MixedCase_PrintsMutableRefWarning -v
    marketplace_package_test.go:967: line " i 'HEAD' is a mutable ref. Resolving to current SHA for safety.", want it to start with " ! " (severity)
--- FAIL
```

還原後綠。

---

## MAJOR 4 — AC-L9 在乾淨已 commit 樹上是空閘門

原本只驗 `git diff -- go.mod`（工作樹）與 `git diff --cached -- go.mod`
（暫存區）——commit 之後兩者都是空字串，任何已 commit 的新 `require` 行
完全看不到。

修復：改成 `git diff 7ddd410^ -- go.mod go.sum`（`7ddd410` 是本 task 系列
第一個 commit，`^` 取其父提交，即本 task 開始改動前的狀態；只給一個 ref
時 `git diff` 是拿它跟目前工作目錄比，因此同時涵蓋「已 commit 的每一輪」
與「尚未 commit 的變更」）。

由於不得在本 repo 執行 `git commit`（任務規則），改在
scratchpad 的一個獨立 scratch git repo 重現同樣的場景（不影響本 repo 歷史）：

```
$ cd <scratch-repo>; git init; echo 'require foo v1.0.0' > go.mod; git commit  # BASE
$ echo 'require foo v1.0.0 / newdep/evil v9.9.9' > go.mod; git commit          # 新增一行 require 並 commit
$ git diff -- go.mod; git diff --cached -- go.mod
（兩者皆空輸出 —— 舊機制在這棵「乾淨已 commit」樹上完全看不到新增的 require）
$ git diff <BASE> -- go.mod
+	newdep/evil v9.9.9
（新機制正確顯示新增的 require 行）
```

`verify.ps1` 本身在真實 repo 上跑（`AC-L9` 閘門）維持綠燈，因為本輪確實
沒有新增任何 `require`。

---

## MAJOR 5 — `resolveRef` 的 `default` 把未知 kind 當 `refKindNamed`

修復：把 `resolveRef` 拆成 `resolveRef`（算 kind）+ `resolveRefForKind`
（依 kind 分派，可被測試直接餵一個假的 kind 值），`default` 改回傳明確
錯誤，不再落到會呼叫 lister 的 `refKindNamed` 分支。

新增 `TestResolveRefForKind_UnrecognizedKind_FailsClosed_NeverTouchesLister`，
用 `panicLister` 證明未知 kind 不會呼叫 lister。

紅（暫時把 `default` 改回舊的「當作 refKindNamed」邏輯）：

```
$ go test ./internal/marketplace/authoring/ -run TestResolveRefForKind_UnrecognizedKind_FailsClosed_NeverTouchesLister -v
panic: ListRefs must not be called: this package should never touch the network
--- FAIL
```

還原後綠。

---

## round 4 全套件與 Tier 1 閘門輸出

```
$ go build ./...   → exit 0
$ go vet ./...     → exit 0
$ go test ./... -count=1 → 全綠
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
  ok   [ROUND4-B3-UNIT]
  ok   [ROUND4-B3-CLI]
  ok   [ROUND4-MAJOR1]
  ok   [ROUND4-MAJOR3]
  ok   [ROUND4-MAJOR5]
  ok   [AC53/no-clack]
  ok   [AC53/no-block]
  ok   [AC-L9]
  ok   [AC-L1/coverage 87.0%]

TIER 1 GREEN
```

---

## round 4 未處理事項

- BLOCKING 1 的殘留邊界：`resolveCloneURL` 的 `filepath.IsAbs` 分支本身
  未加邊界檢查，只靠上游驗證層堵住——已在上方「決定」小節寫明是刻意選擇、
  附成本理由（會牽動數十個既有單元測試 fixture），不是無證據的擱置。
- `internal/marketplace/build/builder.go` 有一份同名但獨立的
  `resolveCloneURL`，同樣有 `filepath.IsAbs` 直接放行的分支——讀過其
  呼叫鏈，輸入一樣先經同一個 `ValidateMarketplaceSource`，round 4 的修復
  同樣涵蓋它，未單獨修改（out of scope：本輪報告的四個阻斷都沒有點名
  這個檔案）。
- `ExecTestJSON` 只套用到本輪新增/受影響的閘門與 `ROUND2-M3-SEVERITY`
  （報告明確點名的那一條），沒有回頭把 round 1-3 既有的所有 `Exec` 型
  閘門全部改寫成 JSON 版本——那些閘門仍只驗 exit code，理論上仍有同樣的
  t.Skip 逃逸口，只是本輪報告點名的具體逃逸口（ROUND2-M3-SEVERITY）已關閉。
  這是明確的殘留範圍，不是聲稱「全部閘門都已加固」。
- `task.py finish` 未執行；本輪同上，仍需外部（或使用者）覆核後才算收斂。
