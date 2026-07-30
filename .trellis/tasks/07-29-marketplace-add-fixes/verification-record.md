# 驗證紀錄 — 07-29-marketplace-add-fixes（外部稽核第二輪修復）

> **狀態：待使用者驗證。任務未標記完成。** `task.py finish` 未執行。

- 實作者：`trellis-implement`（round 2）
- 外部稽核（round 2）：codex（read-only），推翻 round 1 的一項聲稱修復（BLOCKING 1
  結構性未修），並新增 1 阻斷 + 3 重大（含 4 個子項）
- 主 session：round 2 開始前已獨立重現 BLOCKING 1
- 日期：2026-07-30

本檔取代前一版（round 1）的驗證紀錄。round 1 版本本身也被本輪稽核指出
兩處違反 `.trellis/spec/guides/claim-evidence-guide.md` 的敘述句（見下方
「round 1 遺留的敘述句問題」一節）；本次重寫時一併移除。

---

## 修復對照表（round 2）

| # | 級別 | 位置 | 修復摘要 |
|---|---|---|---|
| BLOCKING 1 | 阻斷 | `internal/marketplace/authoring/editor.go` | 抽出 `classifyRefResolution` 作為唯一分類器，`resolveRef` 與 `WillResolveMutableRefForAdd` 都只呼叫它，不再各自表述 |
| MAJOR 1 | 重大 | `internal/marketplace/authoring/resolveref_test.go` | 補 `local + 一般 mutable ref（"main"）` 的缺口測試 |
| MAJOR 2 | 重大 | `internal/marketplace/authoring/refcheck.go` | `resolveCloneURL` 補上相對路徑本地 source 的分支，`set --ref` 在相對路徑本地套件上真的能解析 |
| MAJOR 3 | 重大 | `cmd/apm-go/marketplace_package.go`（測試） | 四個逃逸口：警告嚴重程度斷言（2 處）、CLI 層 `set --ref` 覆蓋、39 字元合法 hex 邊界 |

---

## BLOCKING 1 — `WillResolveMutableRefForAdd` 是第二個真相來源

### 主 session 的獨立重現（修復前）

`resolveRef`（add 模式：`skipLocalSource=true`, `implicitHeadOnEmpty=true`）
在下列條件全部成立時才會真正觸網解析 HEAD：

```
1. version == ""
2. !isLocalPackageSource(source)
3. (ref == "" 因 implicitHeadOnEmpty 而允許)
4. !shaRefPattern.MatchString(ref)      <-- 舊版 WillResolveMutableRefForAdd 完全沒有這一條
5. ref == "" || EqualFold(ref, "HEAD")
```

舊版 `WillResolveMutableRefForAdd` 手動重述了 1、2、5，但**漏掉了 4**。
`git grep -n WillResolveMutableRefForAdd -- '*test.go'`（修復前）零命中 ——
沒有任何測試把兩者鎖在一起，所以沒有東西會在未來出現分歧時發出警訊。

### 修復

`editor.go` 新增 `classifyRefResolution(source, ref, version string,
implicitHeadOnEmpty, skipLocalSource bool) refResolutionKind`，把 `resolveRef`
原本的 6 個分支收斂成 4 種結果（`refKindNone` / `refKindVerbatim` /
`refKindHead` / `refKindNamed`)。`resolveRef` 本身改為對這個分類結果做
`switch`；`WillResolveMutableRefForAdd` 改為

```go
func WillResolveMutableRefForAdd(source, ref, version string) bool {
	return classifyRefResolution(source, ref, version, true, true) == refKindHead
}
```

兩者現在只有**一個**函式在做這個判斷，不再有第二份手寫的分支列表。

### 突變測試 1：重新手寫（不共用分類器）的舊版 `WillResolveMutableRefForAdd`

把 `WillResolveMutableRefForAdd` 暫時改回不呼叫 `classifyRefResolution`、
改用大小寫敏感比對（`ref == "HEAD"`，而非 `EqualFold`）的手寫版本 ——
這正是「手動重述而非共用單一函式」這個結構性風險會實際造成分歧的具體例子：

**紅**：

```
$ go test ./internal/marketplace/authoring/ -run TestWillResolveMutableRefForAdd_MatchesResolveRefAcrossCrossProduct -v
=== RUN   TestWillResolveMutableRefForAdd_MatchesResolveRefAcrossCrossProduct/remote/ref=head/version=
    resolveref_test.go:...: WillResolveMutableRefForAdd("owner/repo", "head", "") = false, but resolveRef's actual HEAD-branch resolution = true (lister called: true, got: "1111111111111111111111111111111111111a")
--- FAIL: TestWillResolveMutableRefForAdd_MatchesResolveRefAcrossCrossProduct/remote/ref=head/version= (0.00s)
--- FAIL: TestWillResolveMutableRefForAdd_MatchesResolveRefAcrossCrossProduct (0.00s)
FAIL
```

**綠**（還原為呼叫 `classifyRefResolution` 後）：全部 20 個交叉積案例（2 個
source 形狀 × 5 種 ref 形狀 × 2 種 version 狀態）全部通過。

### 新測試

`internal/marketplace/authoring/resolveref_test.go`：
`TestWillResolveMutableRefForAdd_MatchesResolveRefAcrossCrossProduct` ——
對 (source: local/remote) × (ref: ""/HEAD/head/40-hex SHA/main) ×
(version: ""/已給) 的完整交叉積，用一個會分別記錄「HEAD」與「main」兩個不同
commit 的 `spyRefLister`，比較「`WillResolveMutableRefForAdd` 的預測」與
「`resolveRef` 實際是否經過 HEAD 分支解析（而非完全不觸網、逐字存 SHA、或
解析一般具名 ref）」。

---

## MAJOR 1 — local 矩陣缺「一般 mutable ref」情境

AC21 宣稱「所有情境」，但先前 6 個 local 分支測試裡沒有
`local + --ref main`（非空、非 HEAD、非 SHA 的一般具名 ref）這個組合。

### 突變（報告原文）

```go
if skipLocalSource && isLocalPackageSource(source) &&
    (ref == "" || strings.EqualFold(ref, "HEAD") || shaRefPattern.MatchString(ref)) {
```

### 紅（套用上述突變後）

```
$ go test ./internal/marketplace/authoring/ -run TestResolveRef_LocalSource_OrdinaryMutableRef_NeverTouchesNetwork -v
=== RUN   TestResolveRef_LocalSource_OrdinaryMutableRef_NeverTouchesNetwork
panic: ListRefs must not be called: this package should never touch the network

goroutine ...:
github.com/apm-go/apm/internal/marketplace/authoring.panicLister.ListRefs(...)
	.../refcheck_test.go:59
github.com/apm-go/apm/internal/marketplace/authoring.resolveRef(...)
	.../editor.go:452
github.com/apm-go/apm/internal/marketplace/authoring.TestResolveRef_LocalSource_OrdinaryMutableRef_NeverTouchesNetwork(...)
	.../resolveref_test.go:439
FAIL
```

`add ./pkg --ref main` 在這個突變下會真的呼叫 `lister.ListRefs` 對一個
local source 觸網 —— 直接違反 mkt-046。`panicLister` 證明了這一點。

### 綠（還原後）

```
$ go test ./internal/marketplace/authoring/ -run TestResolveRef_LocalSource_OrdinaryMutableRef_NeverTouchesNetwork -v
=== RUN   TestResolveRef_LocalSource_OrdinaryMutableRef_NeverTouchesNetwork
--- PASS: TestResolveRef_LocalSource_OrdinaryMutableRef_NeverTouchesNetwork (0.00s)
```

新測試：`TestResolveRef_LocalSource_OrdinaryMutableRef_NeverTouchesNetwork`
（`resolveref_test.go`）。`verify.ps1` 的 AC21 `localBranches` 表也補了
`OrdinaryMutableRef` 這一項，讓「宣稱涵蓋所有情境」的驗證粒度追上宣稱本身。

---

## MAJOR 2 — 相對路徑本地 source 在 `resolveCloneURL` 被誤展開

### 我對 spec 的結論（依指示明確陳述，不挑方便的讀法）

**結論：contract 要求成功，不是 fail-closed 可接受。**

依據：`.trellis/spec/backend/install-marketplace-contracts.md:87` ——
「`--ref` mutable value ... is resolved to a concrete SHA via
`RefLister.ListRefs` before write ... `set` always resolves a given ref
(no `--no-verify` escape hatch on `set`)」—— 這句話對「local source」沒有
任何例外條款。而且 `internal/manifest/mcp.go:265-270`
(`ValidateMarketplaceSource`) 規定本地 source **必須**以 `"./"` 開頭 ——
也就是說，任何透過 `add` 合法寫入的本地套件，其 `source` 欄位**只可能**是
相對路徑，不可能是絕對路徑。round 1 驗證紀錄用「絕對路徑本地 repo」當作
「本地套件」的手動複驗案例，嚴格來說並未通過 `isLocalPackageSource`（它
不是 `"./"` 開頭）；round 1 的 BLOCKING 2 修復本身要求「`set` 必須解析
local source 的 ref，不得短路」，而唯一真正存在的 local source 形狀（相對
路徑）在 round 1 修復後**仍然**因為 `resolveCloneURL` 的既有缺陷而失敗 ——
所以「相對路徑本地 repo 解析失敗」不是可接受的 fail-closed 邊界，是
BLOCKING 2 修復尚未真正涵蓋到的缺口。

### 根因

`internal/marketplace/authoring/refcheck.go` 的 `resolveCloneURL` 原本只認
三種形狀（含 `://`、`git@` 開頭、`filepath.IsAbs`），其餘一律當
`OWNER/REPO` shorthand 展開成 `https://github.com/<source>.git`。一個
`"./repo"` 相對路徑落在這三種之外，被誤展開成
`https://github.com/./repo.git`。

### 修復

`resolveCloneURL` 新增第四個分支：`isLocalPackageSource(source)` 為真時，
用 `filepath.Abs(source)` 把相對路徑轉為絕對路徑後回傳（複用既有的
`isLocalPackageSource`，不是新概念）。

### 紅（修復前，暫時拿掉新分支重現）

```
$ go test ./internal/marketplace/authoring/ -run 'TestResolveCloneURL|TestGitRefLister_ListRefs_RelativeLocalSource_ProductionLister|TestSetPackage_RelativeLocalSource_MutableRef_ResolvesViaProductionLister' -v
=== RUN   TestSetPackage_RelativeLocalSource_MutableRef_ResolvesViaProductionLister
    editor_test.go:1032: SetPackage with a relative local source's --ref returned error: could not resolve ref "v1.0.0" for "./pkgs/tool": git ls-remote https://github.com/./pkgs/tool.git: remote: Repository not found.
--- FAIL: TestSetPackage_RelativeLocalSource_MutableRef_ResolvesViaProductionLister (0.83s)
=== RUN   TestResolveCloneURL
    refcheck_test.go:441: resolveCloneURL(./repo) = "https://github.com/./repo.git", want ".../repo" (the local cwd-relative path, not a GitHub shorthand expansion)
--- FAIL: TestResolveCloneURL (0.00s)
=== RUN   TestGitRefLister_ListRefs_RelativeLocalSource_ProductionLister
    refcheck_test.go:490: ListRefs(./repo) returned error: git ls-remote https://github.com/./repo.git: remote: Not Found
--- FAIL: TestGitRefLister_ListRefs_RelativeLocalSource_ProductionLister (0.51s)
FAIL
```

（這個「紅」是對真實 GitHub 發出了一次會 404 的網路請求 —— 正是缺陷本身
的具體展現：本該解析本地 repo 的呼叫，錯把它當成了一個遠端 GitHub 專案。）

### 綠（修復後）

```
$ go test ./internal/marketplace/authoring/ -run 'TestResolveCloneURL|TestGitRefLister_ListRefs_RelativeLocalSource_ProductionLister|TestSetPackage_RelativeLocalSource_MutableRef_ResolvesViaProductionLister' -v
--- PASS: TestSetPackage_RelativeLocalSource_MutableRef_ResolvesViaProductionLister (0.30s)
--- PASS: TestResolveCloneURL (0.00s)
    --- PASS: TestResolveCloneURL/relative_local_source_resolves_against_cwd,_not_as_OWNER/REPO_shorthand (0.00s)
--- PASS: TestGitRefLister_ListRefs_RelativeLocalSource_ProductionLister (0.21s)
PASS
```

### 新測試

- `TestResolveCloneURL`（新增子測試）—— 單元層級，`refcheck_test.go`。
- `TestGitRefLister_ListRefs_RelativeLocalSource_ProductionLister` ——
  用**正式的 `gitRefLister{}`**（不是 `mapRefLister` 假件）對一個透過相對
  路徑指到的真實 git repo 跑 `git ls-remote`，`refcheck_test.go`。
- `TestSetPackage_RelativeLocalSource_MutableRef_ResolvesViaProductionLister`
  —— `SetPackage` 層級的端對端回歸，同樣用正式 lister，`editor_test.go`。

---

## MAJOR 3 — 四個逃逸口

### 3a／3c：警告嚴重程度只驗訊息文字，未驗 severity（REGR-B1、REGR-M1）

**根因**：`ux.Warn` 與 `ux.Info` 印出的訊息文字完全相同，只有行首的
置中符號欄位不同（` ! ` vs ` i `，見 `internal/ux/printer.go`）。原本的
測試只 `strings.Contains(out, "訊息文字")`，偵測不到嚴重程度被降級。

**修復**：`cmd/apm-go/marketplace_package_test.go` 新增
`assertLineSeverity(t, out, marker, wantSymbol)`：找出含 `marker` 的那一行，
斷言它以 ` <symbol> ` 開頭。套用到兩個既有測試：
`TestMarketplacePackageAdd_ExplicitRefHead_PrintsMutableRefWarning`（AC19）、
`TestMarketplacePackageAdd_OutputsIncludeCodex_NoCategory_WarnsButSucceeds`
（AC48）。

**突變 A**：`marketplace_package.go` 的 mutable-ref 警告改
`ux.Warn` → `ux.Info`

紅：
```
$ go test ./cmd/apm-go/ -run TestMarketplacePackageAdd_ExplicitRefHead_PrintsMutableRefWarning -v
    marketplace_package_test.go:765: line " i 'HEAD' is a mutable ref. Resolving to current SHA for safety.", want it to start with " ! " (severity)
--- FAIL
```
綠（還原後）：`--- PASS`

**突變 B**：`marketplace_package.go` 的缺 category 警告改
`ux.Warn` → `ux.Info`

紅：
```
$ go test ./cmd/apm-go/ -run TestMarketplacePackageAdd_OutputsIncludeCodex_NoCategory_WarnsButSucceeds -v
    marketplace_package_test.go:890: line " i package \"tool\" has no --category; ...", want it to start with " ! " (severity)
--- FAIL
```
綠（還原後）：`--- PASS`

### 3b：CLI 層的 `set --ref` 沒有獨立覆蓋（REGR-B2）

**根因**：round 1 的 REGR-B2 測試只呼叫 `authoring.SetPackage`（跳過 CLI 層
的 cobra `RunE`），所以 `marketplace_package.go` 裡把 `cmd.Flags().Changed
("ref")` 判斷弄壞（例如意外加上一個恆假條件）不會被任何既有測試看見。

**修復**：新增
`TestMarketplacePackageSet_RefFlag_ResolvesViaListerThroughCLI`
（`marketplace_package_test.go`），透過 `runMarketplaceCmd` 真的執行
`package set NAME --ref v1.0.0`（cobra 的 `RunE`，不是直接呼叫 authoring
函式），對一個真實本地 git repo fixture 斷言解析後的 SHA 被寫入 apm.yml。

**突變**：`marketplace_package.go:229` 附近，
`if cmd.Flags().Changed("ref")` → `if false && cmd.Flags().Changed("ref")`

紅：
```
$ go test ./cmd/apm-go/ -run TestMarketplacePackageSet_RefFlag_ResolvesViaListerThroughCLI -v
    marketplace_package_test.go:290: apm.yml = "...source: C:/.../reporoot...\n", want the resolved SHA 5e000e51ac803d4b39fcdb7e81cc5cd63fd59d18 written for ref: (a --ref mutation at the CLI layer must not be silently ignored)
--- FAIL
```
綠（還原後）：`--- PASS`

### 3d：39 字元合法 hex 邊界（REGR-M2）

**根因**：既有邊界測試涵蓋「40 字元但含非 hex」「41 字元」「大寫」三種，
但沒有「39 個全部合法的小寫 hex 字元」這個邊界 —— `{40}` → `{39,40}` 這種
放寬長度下限的突變不會被上述三者任何一個抓到。

**修復**：新增 `TestResolveRef_ShaPattern_39CharValidHex_ResolvesViaLister`
（`resolveref_test.go`）。

**突變**：`editor.go:334`，`^[0-9a-f]{40}$` → `^[0-9a-f]{39,40}$`

紅：
```
$ go test ./internal/marketplace/authoring/ -run TestResolveRef_ShaPattern_39CharValidHex_ResolvesViaLister -v
    resolveref_test.go:298: resolveRef = "0123456789abcdef0123456789abcdef0123456", want the resolved commit "0123456789abcdef0123456789abcdef01234567" (a 39-char string must not be treated as a concrete SHA, even if every character is valid hex)
--- FAIL
```
綠（還原後）：`--- PASS`

---

## round 1 遺留的敘述句問題（本輪一併處理）

round 1 的驗證紀錄本身在下列三處使用了缺乏證據支撐的斷言句型（本輪稽核
指出）；本次重寫直接刪除/取代這些段落，不再保留：

- 舊 `:125`「presentational-only concern」刪除理由「不影響理解」——沒有
  反例佐證。本次重寫不再需要為刪除一段註解本身寫因果性論證，只客觀記錄
  「已刪除」這件事（見上方修復對照表，不再附加無證據的動機說明）。
- 舊 `:375`「correct fail-closed」——已被 MAJOR 2 一節推翻並附上
  `install-marketplace-contracts.md:87` 依據 + 修復。
- 舊 `:383`「三個阻斷全部修復」——本輪逐項列出可證偽的個別結果（見下方
  「本輪逐項結果」），不再使用單一籠統斷言。

---

## 本輪逐項結果（可證偽，取代舊版 `:383` 的籠統陳述）

| 項目 | 狀態 | 證據 |
|---|---|---|
| BLOCKING 1（分類器共用） | 已修復 | 上方「BLOCKING 1」節：紅/綠 + 交叉積測試 20 案例全綠 |
| MAJOR 1（local 一般 mutable ref） | 已修復 | 上方「MAJOR 1」節：紅（panic）/綠 |
| MAJOR 2（相對路徑本地 source） | 已修復 | 上方「MAJOR 2」節：紅（真實 404 網路請求）/綠 + 3 個新測試 |
| MAJOR 3a（REGR-B1 severity） | 已修復 | 上方「MAJOR 3」節突變 A：紅/綠 |
| MAJOR 3b（REGR-B2 CLI 層） | 已修復 | 上方「MAJOR 3」節：紅/綠 + CLI 層新測試 |
| MAJOR 3c（REGR-M1 severity） | 已修復 | 上方「MAJOR 3」節突變 B：紅/綠 |
| MAJOR 3d（REGR-M2 39 字元） | 已修復 | 上方「MAJOR 3」節：紅/綠 |

---

## verify.ps1 新增的閘門

- `ROUND2-B1` — BLOCKING 1 交叉積回歸測試存在且通過
- `ROUND2-M1` — MAJOR 1 local-ordinary-mutable-ref 回歸測試存在且通過
- `ROUND2-M2` — MAJOR 2 相對路徑本地 source（production lister）回歸測試存在且通過（2 個）
- `ROUND2-M3-CLI` — MAJOR 3 CLI 層 `set --ref` 回歸測試存在且通過
- `ROUND2-M3-39CHAR` — MAJOR 3 39 字元邊界回歸測試存在且通過
- `ROUND2-M3-SEVERITY` — MAJOR 3 嚴重程度斷言宿主測試存在（-list 非零）
- `localBranches` 表新增 `OrdinaryMutableRef` 項（AC21 既有迴圈的一部分）

---

## 全套件與完整 Tier 1 閘門輸出

```
$ go build ./...   → exit 0
$ go vet ./...     → exit 0
$ go test ./... -count=1                    → 全綠（23 個套件）
$ go test ./... -count=1 -coverprofile=...  → total 87.0%（≥ 80% 門檻）
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
  ok   [AC53/no-clack]
  ok   [AC53/no-block]
  ok   [AC-L9]
  ok   [AC-L1/coverage 87.0%]

TIER 1 GREEN
```

---

## 未處理 / 交由使用者裁定

無新的「延後」項目。BLOCKING 1、MAJOR 1、MAJOR 2、MAJOR 3（四子項）皆已
修復並附紅/綠證據，且每一項都用報告點名的確切突變驗證過。

`task.py finish` 未執行；本輪仍需外部（或使用者）覆核後才算收斂。
