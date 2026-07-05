# Design: marketplace 發布端指令(init/check/outdated/audit/migrate/package)

權威清單:`../07-03-marketplace-ecosystem/marketplace-checklist.md` Phase M3(mkt-040~047,2026-07-03 修訂版)。
前置:無(與 consumer/install 平行)。操作對象是**本地專案 apm.yml 的 `marketplace:` 區塊**,不是全域登錄檔。

## 套件結構

```
internal/marketplace/authoring/
  schema.go     # marketplace: 區塊資料模型 + 載入/驗證(mkt-047 互斥檢查)
  template.go   # init 的 scaffold 範本(原始文字,含註解)
  editor.go     # packages[] 手術式編輯(add/remove/set)+ atomic write + 寫後驗證回滾
  refcheck.go   # check/outdated 共用:git ls-remote tag 對照(複用 internal/gitops + internal/semver)
  audit.go      # audit 的依賴分類(marketplace / local / bypass)
  migrate.go    # marketplace.yml → apm.yml marketplace: 區塊(註解保留)

cmd/apm/marketplace_authoring.go  # init/check/outdated/audit/migrate/package 子指令接線
```

`package` 子指令組掛在 `marketplace` 群組下(`apm marketplace package add|remove|set`),與 consumer 子任務的六個消費端指令同一個 `marketplaceCmd()`。

## 資料模型與載入(`schema.go`,mkt-047)

```go
type AuthoringConfig struct {
    Owner    Owner       // name, url
    Build    Build       // tagPattern
    Outputs  []string    // "claude", "codex", ...
    Packages []PackageEntry // name, description, source, version|ref(互斥), subdir, tagPattern, tags, includePrerelease, category
}
// PackageEntry.Tags 的載入規則(adversarial 抽查發現,`yml_schema.py:896-913`):
// = yml 的 `tags` + `keywords` 兩個鍵**合併去重**後的結果——pack 的輸出直接用這個值,
// authoring 的 schema 載入必須複製這個合併,否則 pack 輸出的 tags 會與原版不一致。

func LoadAuthoringConfig(dir string) (*AuthoringConfig, ConfigSource, error)
```

載入規則(對齊原版 `detect_config_source`,邊界已對照 `migration.py:87-111` 驗證):
- apm.yml 有**非 null** `marketplace:` 鍵(`_has_marketplace_block` 語意)且 `marketplace.yml` **檔案存在**(單純 `exists()`——**空的 legacy 檔也算存在、也觸發互斥錯誤**,gaps A4)→ **硬錯誤**(兩者互斥,絕不合併讀取)
- 只有其一 → 讀該來源;legacy 來源印棄用警告(提示 `apm marketplace migrate`)
- 都沒有 → 明確錯誤(提示 `apm marketplace init`)

req-mf-017(規範性,producer):`packages[].source` 驗證——**重用既有實作** `internal/manifest.ValidateMarketplaceSource`(`manifest.go:426` 的 `validateMarketplaceBlock` 已在 manifest 載入時執行;gaps B2),`LoadAuthoringConfig` 與 editor 寫入後驗證都呼叫同一個函式,**不得**在 authoring 套件重新實作一份 source 驗證規則。

## `init`(mkt-040)

- scaffold 進 apm.yml 的 `marketplace:` 區塊;apm.yml 不存在時先建最小殼(name/version/description)
- 範本是**原始文字**(不走 yaml Marshal——需要精確的註解與排版),以文字附加到 apm.yml 尾端;apm.yml 已有 `marketplace:` 時無 `--force` 拒絕
- 形狀:`owner.name/owner.url`、`build.tagPattern: "v{version}"`、`outputs: claude: {}` + 註解 `# codex: {}`、`packages[]` 範例 + 註解掉的本地套件範例 + `# category` 提示
- **偏離原版**:範例註解不使用 `ref: main`(會被 pack 的 HeadNotAllowed 擋下,mkt-040 修訂版的陷阱),改用 tag 例(如 `# ref: v1.0.0`)
- flags:`--force`、`--no-gitignore-check`(檢查 .gitignore 是否忽略 marketplace.json 輸出,有 → 警告)、`--name`、`--owner`、`-v`

## `check` / `outdated`(mkt-041/042,`refcheck.go`)

共用:每個 remote package 一次 `git ls-remote --tags --heads`(複用 `internal/gitops.RealTagLister` 模式;需要 heads 時擴充),tag 過濾用 `build.tagPattern`/`packages[].tagPattern`(`{version}`/`{name}` 佔位),semver 比對用 `internal/semver`。

- `check [--offline] [-v]`:本地(`./`)來源**跳過**網路直接 pass;remote 驗證 pin 的 ref/range 存在;失敗聚合計數,任一失敗 exit 1;`--offline` 無快取可用視為失敗(不寬貸)
- `outdated [--offline] [--include-prerelease] [-v]`(mkt-042 修訂版):
  - `[+]` current == latest-in-range
  - `[!]` range 內有可升級(**計入** exit 1)——同圖示也用於「no matching tags」(**不計入**)
  - `[*]` latest overall ≠ latest in range(range 外任何更新,不限 major)
  - `[i]` 已 pin ref 或無 range,略過
  - `[x]` 遠端抓取失敗,**不影響 exit code**
  - exit 1 僅由 upgradable 計數驅動,否則 exit 0

## `audit NAME [--strict] [-v]`(mkt-043 修訂版,`audit.go`)

> mkt-041/042/043 的行號與行為(圖示 if/elif 鏈、失敗四分類、strict 計數)已由 2026-07-03 checklist 複審時的獨立 agent **全文抽查驗證**(`outdated.py`/`check.py`/兩份 `audit.py` 全讀),非僅行號摘要——gaps A1/A2 的查證前提已滿足;`FetchStatus` 確認只有 OK/NO_MANIFEST/UNSUPPORTED_SOURCE/NETWORK_ERROR/PARSE_ERROR 五種,無遺漏分類。

對**已註冊**的 marketplace(依賴 consumer 的 registry+Fetch;此為本子任務唯一跨子任務依賴,若 consumer 未合入,audit 步驟後移)。devDependencies 掃描直接重用 `internal/manifest` 既有解析(`manifest.go:132` 已支援 devDependencies,gaps B1),不另寫讀取邏輯:
1. fetch marketplace manifest → 每個 plugin 抓其 repo 內 pin ref(未 pin 回退 HEAD)的 `apm.yml`
2. 掃 `dependencies` **與 `devDependencies`** 的 apm 條目,分類:marketplace 形(dict `{name, marketplace}` 或字串符合 `pkg@mkt` 文法)/ local(`./`)/ **bypass**(其餘 git ref 與 `{git:}` 物件)
3. bypass → 列出 + 建議文字(**指向 dict 形式** `{name: X, marketplace: Y}`,不是原版那個解析器不收的字串形式)
4. fetch 結果分四類:OK / NO_MANIFEST / UNSUPPORTED_SOURCE(skipped,不觸發 strict)/ NETWORK・PARSE(unverifiable,觸發 strict)
5. `--strict`:bypass 或 unverifiable > 0 → exit 1;無 `--strict` 一律 exit 0

## `migrate [--force|--yes/-y] [--dry-run] [-v]`(mkt-044,`migrate.go`)

- 要求 `marketplace.yml` 與 `apm.yml` 都存在;先驗證 legacy 檔可解析
- 註解保留:用 `yaml.v3` 的 `yaml.Node` 讀 legacy 檔(v3 Node 保留註解),把 `marketplace:` 區塊節點**移植**進 apm.yml 的 Node 樹,再以 `internal/yamlcore` 手術式寫回(不整份 re-encode——AC4;比照 `PatchMappingPath` 教訓)
- apm.yml 已有非空 `marketplace:` → 無 `--force` 拒絕
- `--dry-run`:印 unified diff,不寫檔、不刪檔
- 成功後 `os.Remove("marketplace.yml")`

## `package add/remove/set`(mkt-045/046,`editor.go`)

旗標(mkt-045 修訂版,**非完全共用**):

| flag | add | set | remove |
|---|---|---|---|
| `--name` | ✓(僅 add) | — | — |
| `--version` / `--ref` | ✓ 互斥 | ✓ 互斥 | — |
| `--subdir` | ✓ 含 `-s` 短旗標 | ✓ 無短旗標 | — |
| `--tag-pattern` / `--tags` | ✓ | ✓ | — |
| `--include-prerelease` | flag | **三態**(未給=不動) | — |
| `--no-verify` | ✓(僅 add) | — | — |
| `--yes/-y` | — | — | ✓(非互動無 -y → exit 1) |

- **陣列元素編輯策略(本子任務最大技術不確定性,gaps B3)**:`internal/yamlcore.PatchMappingPath` 只支援「替換/插入一個 mapping key 的 value span」,**不涵蓋**「在 sequence 裡增/刪/改一個元素、其餘元素 bytes 不動」。三個選項:
  1. 直接用 `PatchMappingPath` 替換整個 `marketplace.packages` 的 value——會把**所有**既有 packages 條目重新序列化,使用者手動排版/註解可能被改寫,違反 AC 的手術式要求 → 不採用為主路徑
  2. 擴充 `PatchMappingPath` 支援 sequence 索引 → 泛化成本高、動到 `--mcp` 任務已驗證過的既有程式 → 不採用
  3. **(採用,可行性已 spike 驗證)** 新增範圍更窄的 `yamlcore.SpliceSequenceElement(src, doc, path, op, idx, newNode)`:利用 yaml.Node 的 Line/Column 定位目標元素的 byte span——`add` = 在 sequence span 尾端插入渲染後的元素文字;`remove` = 刪除單一元素 span;`set` = 以重新渲染的節點替換單一元素 span。結構不符合預期(flow style、非 block sequence)時回傳 ok=false,**fallback 鏈**:選項 3 → 選項 1(整段 value 替換,附警告)→ 絕不整份 re-encode
  **Spike 結果(2026-07-03,`go.yaml.in/yaml/v4 v4.0.0-rc.6`,22 項斷言全過)**:sequence 元素節點確實帶可用的 Line/Column;remove/set/add 三種 op 的 byte-span 拼接可行——序列內註解、其他條目的行內註解、無關區塊全部 byte 不動,結果可重新解析,**CRLF 來源的行尾原樣保留**。span 規則已驗證:元素 span = 該元素首行起,到下一元素首行(或最後元素時,掃到第一個縮排 ≤ 父鍵縮排的非空非註解行);元素前的引導註解歸屬 sequence 而非元素(remove 首元素時註解保留)。已知邊界留給實作:flow-style 直接 fallback;元素**之間**的獨立註解行會併入前一元素的 span(remove 時一起消失——與 ruamel 行為近似,可接受,測試明示)。
- 編輯完成後 atomic write(temp+fsync+rename)+ **寫後重新載入驗證,失敗回寫原文**(回滾)。原版機制(已對照 `yml_editor.py::_write_and_validate` 驗證):寫入新內容 → 重新載入驗證 → 失敗時用備存的原文 atomic write 回寫並報錯。Go 版小幅改良:先在**記憶體**驗證新 bytes 再落盤(避免磁碟短暫出現壞檔),觀測行為相同
- 錯誤路徑 exit code **2**(對齊原版;remove 的非互動守衛例外用 1)
- **mkt-046 修正(不移植原版缺陷)**:source 是本地(`./`)→ `verifySource` 與 ref 解析**直接跳過網路**,預設路徑即可成功——不需要 `--no-verify`、不需要 `--version`、更不需要假 SHA。remote source 才走 `git ls-remote` 驗證(`--no-verify` 跳過)。回歸測試:`package add ./pkgs/tool` 無任何旗標離線成功

## 錯誤處理

- 所有 YAML 寫入走 editor 的 atomic+驗證+回滾路徑,不得有第二條直接寫檔的路徑
- 網路錯誤訊息不回顯憑證(credsec 慣例)
