# marketplace 子指令 stdout 省略盤查（vs upstream v0.27.0）

**盤查日期**：2026-08-06
**起因**:使用者驗證時發現 marketplace 子指令 stdout 內容過於省略
**方法**：逐指令比對上游輸出語句（`git show v0.27.0:src/apm_cli/commands/marketplace/...`）
與 apm-go 對應 RunE 的 ux.* 呼叫。原始碼層級比對；**未逐指令實跑兩邊**（涉及網路與
registry 狀態）。範圍 = `marketplace` 子指令；`apm search` / `apm doctor` 為上游
**頂層**指令（cli.py:220,224 註冊），不在此範圍。

## 有落差（依嚴重度排序）

### 1. `update`（無參數且 registry 為空）→ 零輸出
- apm-go `marketplace.go:488-504`：空 registry 時迴圈跑零次，exit 0、一行都不印。
- 上游 `__init__.py:980-982`：印 `No marketplaces registered.`。
- 另缺：單一目標的 `Refreshing marketplace 'x'...` start（:971）、全量的
  `Refreshing N marketplace(s)...` start（:983）與收尾 `Marketplace cache refreshed`（:993）。

### 2. `validate` 通過的檢查完全不顯示，Summary 是近似值
- 上游 `validate.py:29-80`：start 行、`Found N plugins`、空行 + `Validation Results:`
  標頭、**每個通過的檢查一行** `<check_name>: all plugins valid`，Summary 逐檢查計數。
- apm-go `marketplace.go:599-632`：只印 findings；乾淨 manifest 時只有一行 Summary。
  `summarizeFindings`（:649-663）自己的註解承認 passed 數是 approximation
  （1+len(plugins)−errors），不是逐檢查 tally。

### 3. `check` 無表格、通過項目預設不顯示、無 offline 通知
- 上游 `check.py` + `__init__.py:1246-1287`：`Entry Health Check` 表格，**每個 entry 一列**
  （Status/Package/Reachable/Version Found/Ref OK/Detail），收尾 `All N entries OK` 或
  `N entries have issues`；`--offline` 時印 `Offline mode -- only schema and cached-ref
  checks`（check.py:69-73）；verbose 印每個 entry 的解析目標（:142-144）。
- apm-go `marketplace_authoring.go:287-312`：預設只列失敗 bullet；通過項目僅 verbose
  顯示；無表格、無 Reachable/Version Found/Ref OK 分欄；無 offline 通知。
  （apm-go 多印一行上游沒有的 `pass rate: X/N`。）

### 4. `outdated` 缺 Range 欄，Current 欄恆為 "--"
- 上游表格六欄含 `Range`（`__init__.py:1203-1214`）；`Current` 由 cwd 的
  marketplace.json 填值（`_load_current_versions`，:1133-1148）。
- apm-go `marketplace_authoring.go:352-365`：表頭無 RANGE；`OutdatedPackages` 傳
  **nil** current-versions map，Current 恆 "--"。:329-334 的註解說「`apm pack`
  尚未落地」——**已過時**，pack 已存在，此接線一直沒補。

### 5. `add` 無進度行、成功行缺 plugin 數、缺 install 提示
- 上游 `__init__.py:630-635`：fetch 前印 `Registering marketplace 'x'...`（註解明寫
  generic-git probe 要 5-30s，避免空白畫面）；成功行 `registered (N plugins)`（:721-724）；
  別名來自 manifest 時印 `Install plugins with: apm install <plugin>@<name>`（:728-732）；
  verbose 有 Source/Source type/Ref/Detected path/Alias source/description（:701-711,725-726）。
- apm-go `marketplace.go:250-258`：無進度行；成功行為 `Added marketplace %q (kind: %s)`
  ——plugin 數只在 verbose；無 install 提示；verbose 只有 source/ref/plugins 三項。

### 6. `audit` 缺 start、計數行、標頭與結尾解釋段
- 上游 `audit.py:36-113`：`Auditing marketplace 'x'...`、`Checking N plugins...`、
  空行 + `Audit Results:` 標頭（有 findings 或 verbose 時）、結尾在有 bypass 時多一段
  解釋文字 + 文件 URL（:106-113）。
- apm-go `marketplace_authoring_audit.go:49-58`：以上全部沒有，只有逐 plugin 輸出與
  Summary。

### 7. `package add/set` 的 ref 解析結果不回報
- 上游 `plugin/__init__.py:147-150,175-183`：HEAD/branch 解析為 SHA 後印
  `Resolved <ref> to <sha12>`；branch 另有 `'x' is a branch (mutable ref). Resolving to
  current SHA for safety.` 警告。
- apm-go：解析本身有做（`editor_test.go:1003-1018` 證明 branch→SHA），但只接了
  explicit-HEAD 警告 hook（`marketplace_package.go:173-176,275-277`）；
  **resolved SHA 從不回報**，branch 警告也沒有。使用者無從得知 apm.yml 被寫入哪個 SHA。
- 另：上游 offline 無法驗證 ref 時警告後照存（:159-164）；apm-go 硬失敗
  （editor.go:362 有記錄）——這是**行為**分歧（已知、故意），非 stdout 省略。

### 8. `list` 缺表格後的 browse 提示
- 上游 `__init__.py:883-886`：表格後印 `Use 'apm marketplace browse <name>' to see plugins`。
- apm-go `marketplace.go:386`：表格後直接結束。

### 9. `remove` 確認提示缺 source；取消用詞不同
- 上游 `__init__.py:1023-1026`：`Remove marketplace 'x' (github.com/o/r)?`（含 source）；
  取消印 `Cancelled`。
- apm-go `marketplace.go:543-551`：提示只有 name；取消印 `Aborted.`。
  `package remove` 同樣（上游 `Cancelled.`，apm-go `Aborted.`）。

## 無落差（逐一讀過確認）

- `browse`（apm-go 甚至多一行 fetch 成功回報）
- `init`（成功行、verbose Path、gitignore 警告、Next Steps box 均對齊）
- `migrate`（dry-run 標頭、成功兩行均對齊；verbose diff 是 apm-go 的加項）
- `package set/remove` 的成功行與確認閘門
- `build` tombstone（兩邊都是 error path；上游訊息多一句 marketplace.json 說明）

## 修正紀錄（2026-08-06，使用者裁定「全修」）

九項全部修畢。程式碼變更：

- `internal/marketplace/validator.go`：新增 `ValidationResult`/`ValidateChecks`
  （Schema/Names 兩個具名檢查，鏡射上游 validator.py）；`Validate` 改為其攤平。
- `internal/marketplace/models.go`：`MarketplaceManifest` 補 `Description` 欄位。
- `internal/marketplace/authoring/refcheck.go`：`CheckResult` 加
  `Reachable/VersionFound/RefOK` 分類；`checkPackage` 回傳完整分類。
- `internal/marketplace/authoring/editor.go`：`resolveRef` 新增 `OnRefResolved`
  hook（HEAD 與 named ref 解析成不同 SHA 時回報）；Add/SetOptions 各加欄位。
- `cmd/apm-go/marketplace.go`：add（Registering 進度行、成功行帶 plugin 數、
  install 提示、verbose 補 source type/alias source/description）、list（browse
  提示）、update（空 registry 通知、start/收尾行）、remove（提示帶 source、
  Cancelled）、validate（Validating/Found N/Validation Results/逐檢查行/真實
  Summary；移除 summarizeFindings 近似值）。
- `cmd/apm-go/marketplace_authoring.go`：check（offline 通知、Entry Health
  Check 六欄表格，全 entry 一列）、outdated（RANGE 欄、Current 讀
  ./marketplace.json、過時註解修正）。
- `cmd/apm-go/marketplace_authoring_audit.go`：Auditing/Checking N/Audit
  Results 標頭/結尾解釋段+URL、單複數修正。
- `cmd/apm-go/marketplace_package.go`：add/set 接 `OnRefResolved` 印
  `Resolved <ref> to <sha12>`；remove 改印 Cancelled；`shortSHA` helper。

測試：新增 `TestValidateChecks`（internal）、update 空 registry/start+收尾、
list 提示、add plugin 數、validate 逐檢查行、check 表格/offline 通知、
outdated Current+RANGE、package add Resolved 行（共 9 個新測試/斷言組）；
更新 2 個 Aborted→Cancelled、2 個 check 舊格式斷言。突變驗證：拿掉空
registry 通知 → `TestMarketplaceUpdate_EmptyRegistryReportsInsteadOfSilence`
轉紅（實跑確認），還原後綠。`go build ./... && go vet ./... &&
go test ./... -count=1` 全綠（exit 0）。

## 端到端實跑驗證（2026-08-06）

完整逐指令輸出見同目錄 `stdout-parity-e2e.log`（可重跑腳本：本機 scratchpad
`e2e.sh`；隔離 `APM_CONFIG_DIR`，本地 fixture + 真 GitHub 唯讀 `git ls-remote
microsoft/apm`）。九項逐一對應：

| # | 缺口 | 實跑證據（log 內行） |
|---|---|---|
| 1 | update 空 registry 靜默 | `i No marketplaces registered.` [exit 0]；全量路徑 `Refreshing 2 marketplace(s)...` + `+ Marketplace cache refreshed` |
| 2 | validate 通過檢查不顯示 | `Validation Results:` + `+ Schema: all plugins valid` + `+ Names: all plugins valid` + `Summary: 2 passed, 0 warnings, 0 errors`；失敗例 `x Names: duplicate plugin name "DUP"` → `1 passed, 0 warnings, 1 errors` [exit 1] |
| 3 | check 無表格/offline 通知 | 六欄表格（REACHABLE/VERSION FOUND/REF OK/DETAIL），通過列 `+ apm + + + OK` 與失敗列同表；`i Offline mode -- only schema and cached-ref checks` |
| 4 | outdated 缺 RANGE、Current 恆 "--" | 表格 `CURRENT=v0.26.0 RANGE=>=0.26.0 LATEST-IN-RANGE=v0.28.0`（Current 來自 cwd marketplace.json，對真 GitHub tags） |
| 5 | add 無進度/plugin 數/提示 | `i Registering marketplace "mkt"...` → `+ Marketplace "acme" registered (2 plugins)` → `i Install plugins with: apm-go install <plugin>@acme`；`-v` 列 source type/alias source |
| 6 | audit 缺標頭 | `i Auditing marketplace "acme"...` + `i Checking 2 plugins...` + `i Audit Results:` + Summary |
| 7 | package add/set 不回報 SHA | `! 'HEAD' is a mutable ref...` + `i Resolved HEAD to dcbaf654cf6d` + `+ Added package "apm"`；測試另證 log 的 SHA == apm.yml 寫入值 |
| 8 | list 缺 browse 提示 | 表格後 `i Use 'apm-go marketplace browse <name>' to see plugins` |
| 9 | remove 提示/用詞 | 程式碼 `Remove marketplace %q (%s)?` + Cancelled；e2e 走 `--yes` 路徑（互動提示由 `TestMarketplaceRemove_InteractiveExplicitNo_AbortsCleanly` 以 stubConfirm 驗證） |

**基準位移警示**：e2e 的 outdated 實跑顯示上游現有 **v0.28.0** tag（本盤查與
修正的基準仍是使用者裁定的 v0.27.0）。基準是否再抬升屬使用者裁定，此處僅記錄。

## 未驗證 / 範圍外

- 上游 rich 表格 vs apm-go ux.Table 的邊框/樣式差異不在本盤查範圍（親任務 prd.md
  已列 Out of Scope）。
- `remove` 的互動提示文字未在 e2e 實跑（需真 TTY），由 stub 測試覆蓋。
- 上游 offline 無法驗證 ref 時「警告後照存」vs apm-go 硬失敗：已知故意的行為
  分歧（editor.go:362），本輪不動。
