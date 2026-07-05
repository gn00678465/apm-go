# Implement: marketplace 發布端指令

TDD:每步先寫測試(RED)→ 實作(GREEN)→ `go build ./... && go vet ./... && go test ./...`。
與 consumer/install 子任務平行可做;僅步驟 6(audit)依賴 consumer 的 registry+Fetch,若未合入則該步後移。

## 執行順序

### 步驟 1 — 資料模型與載入(mkt-047 + req-mf-017)
- [ ] `internal/marketplace/authoring/schema.go`:`AuthoringConfig`、`LoadAuthoringConfig`(apm.yml `marketplace:` / legacy `marketplace.yml` 偵測)
- [ ] 測試:兩者並存 → 硬錯誤;只有 legacy → 讀取 + 棄用警告;都沒有 → 明確錯誤
- [ ] 測試(req-mf-017,規範性):source 含 `..` 段、userinfo/port/query URL、非 https 遠端、不以 `./` 開頭的本地 → 各自拒絕——**重用 `internal/manifest.ValidateMarketplaceSource`**,不重新實作(gaps B2)
- [ ] 測試(mkt-047 邊界):**空的** `marketplace.yml` + apm.yml 有 `marketplace:` → 仍觸發互斥硬錯誤(legacy 判斷是檔案存在,非內容非空)
- 驗證:`go test ./internal/marketplace/authoring/...`

### 步驟 2 — `init`(mkt-040)
- [ ] `template.go` 原始文字範本 + `cmd` 接線:`--force`/`--no-gitignore-check`/`--name`/`--owner`/`-v`
- [ ] apm.yml 不存在 → 先建最小殼;已有 `marketplace:` → 無 `--force` 拒絕;`.gitignore` 忽略 marketplace.json 輸出 → 警告
- [ ] 測試:scaffold 輸出與 checklist 附的 live 輸出逐段比對(AC2;owner/build/outputs/packages/註解範例/`# category` 提示);**範例註解不含 `ref: main`**(mkt-040 修訂版陷阱,測試斷言範本無此字串)
- [ ] fixture 含「已存在、手動排版過」的 apm.yml,驗證 append 不動既有內容(舊坑 1)
- 驗證:PASS

### 步驟 3 — `check`(mkt-041)
- [ ] `refcheck.go`:remote 套件 `git ls-remote` 對照 pin ref/semver range;本地套件跳過
- [ ] 測試:本地 git repo fixture(`t.TempDir()` 內 `git init` + tag);本地來源零網路(fake lister panic 斷言);任一失敗 exit 1;`--offline` 無快取 → 失敗
- 驗證:PASS

### 步驟 4 — `outdated`(mkt-042 修訂版)
- [ ] 五種圖示(`[+]`/`[!]`/`[*]`/`[i]`/`[x]`)語意照 design 表;exit 1 僅由 upgradable 計數驅動
- [ ] 測試:每種圖示至少一案例;「no matching tags」顯示 `[!]` 但**不**觸發 exit 1;`[x]` 不影響 exit code;`--include-prerelease` 行為
- 驗證:PASS

### 步驟 5 — `package add/remove/set`(mkt-045, mkt-046)
- [ ] `yamlcore.SpliceSequenceElement`(design 選項 3——**可行性已 spike 驗證,22 斷言全過含 CRLF**,span 規則見 design):add/remove/set 三種 op,用「已存在、含註解與手動排版」fixture 寫 RED 測試鎖行為(含「元素間獨立註解併入前一元素 span」的明示測試);flow-style/結構不符 fallback 到整段 value 替換(附警告),絕不整份 re-encode
- [ ] `editor.go`:基於 SpliceSequenceElement 的編輯 + 記憶體驗證先於落盤 + atomic write + 失敗回寫原文;三個子指令接線(旗標表見 design)
- [ ] 測試:`--version`/`--ref` 互斥;add 專屬 `--name`/`-s`;set 的三態 `--include-prerelease`(未給不動既有值);remove 非互動無 `-y` → exit 1;錯誤路徑 exit 2
- [ ] 測試(回滾):注入寫後驗證失敗 → 檔案內容回到原文
- [ ] **回歸測試(mkt-046 修正,AC3)**:`package add ./pkgs/tool` 無任何旗標、零網路(fake lister panic 斷言)成功;remote source 才走 ls-remote,`--no-verify` 跳過
- [ ] fixture 含「已存在、含手動註解與排版」的 apm.yml,驗證編輯只動目標條目(舊坑 1)
- 驗證:PASS(Review Gate A)

### 步驟 6 — `audit`(mkt-043 修訂版;依賴 consumer 的 registry+Fetch)
- [ ] `audit.go`:分類器(marketplace 形 dict/字串文法、local、bypass 含 `{git:}` 物件);掃 dependencies **與 devDependencies**
- [ ] fetch 狀態四分類;`--strict` 只計 bypass + NETWORK/PARSE;無 strict 一律 exit 0
- [ ] 建議文字指向 dict 形式 `{name: X, marketplace: Y}`(測試斷言建議文字**不含**字串形式 `X@Y` 的寫法——原版矛盾不照抄)
- 驗證:PASS

### 步驟 7 — `migrate`(mkt-044)
- [ ] `migrate.go`:yaml.Node 註解保留移植 + `--force|--yes/-y` 同義、`--dry-run` 印 diff 不寫檔、成功後刪 `marketplace.yml`
- [ ] 測試(AC4):legacy 檔含註解與手動排版 → 移植後 apm.yml 的 `marketplace:` 區塊保留註解、apm.yml 既有其他區塊 bytes 不變;已有非空區塊無 `--force` 拒絕;`--dry-run` 零寫入零刪除
- 驗證:PASS(Review Gate B)

### 步驟 8 — A/B 測試(AC5)
- [ ] `D:\Projects\apm-dev\evals\ab_marketplace_authoring.py`(對齊 `ab_mcp_install.py` 慣例)
- [ ] 涵蓋:init scaffold(語意比對,`ref: main` 差異列例外)、package add 本地來源(**例外**:原版壞、Go 修正,註明 mkt-046)、package add remote、migrate 註解保留、check/outdated exit code
- 驗證:對照 `uv run apm` 通過(例外項除外)

### 步驟 9 — 全域驗證
- [ ] `go build/vet/gofmt/test ./... -cover` 全綠
- [ ] 至少一輪 codex exec 唯讀審查,修正發現的問題並補回歸測試
- [ ] checklist mkt-040~047 逐條打勾(mkt-046 標 fixed)

## Review Gates
- **A**(步驟 5 後):editor 的原子性與回滾——不得存在任何繞過「atomic+驗證+回滾」的第二條寫檔路徑;mkt-046 的零網路證明用 fake lister panic,不是時序推測
- **B**(步驟 7 後):migrate 的註解保留是 Node 移植,不是整份 re-encode(diff 檢查 apm.yml 其他區塊 bytes 不變)
- **C**(步驟 8 後):A/B 例外清單每項引用 checklist 條目依據

## Rollback Points
步驟 1-7 全在新套件 `internal/marketplace/authoring` + `cmd/apm/marketplace_authoring.go`,對既有檔案唯一修改是 `marketplaceCmd()` 掛子指令(一處)。每步獨立 commit。

## 已知延後項目
- `outputs` 的 codex `category` 必填驗證屬於 pack 子任務的 config 載入閘門(mkt-053),本子任務的 schema.go 只保留欄位,不做該驗證(避免兩處重複;pack 子任務實作時再決定放 schema 層或 pack 層)
- `apm doctor` 的 marketplace config 檢查段:不在本生態系任務範圍(parent prd Non-Goals)
