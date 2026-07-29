# Design：marketplace/plugin parity

對應 `prd.md` 的 R1–R8。本檔只寫技術設計，需求與 AC 不重複。

---

## 1. 邊界與影響面

| 層 | 檔案 | 改動性質 |
|---|---|---|
| CLI | `cmd/apm-go/main.go` | 註冊 `plugin` group（+1 行） |
| CLI | `cmd/apm-go/init.go` | 抽共用本體、有序 YAML 產生、plugin 模式分支 |
| CLI | `cmd/apm-go/plugin.go`（新） | `plugin` group + `plugin init` 的旗標與 wiring |
| CLI | `cmd/apm-go/install.go:829-832` | 字串（`target:` → `targets:`） |
| 領域 | `internal/manifest/target.go` | 三個 target 集合統一來源（R8） |
| 領域 | `internal/marketplace/authoring/editor.go` | `resolveRef` 隱含 HEAD（R5） |
| 領域 | `internal/marketplace/registry.go:248` | 錯誤訊息擴寫（R6） |
| UX | `internal/ux/interactive.go:84,149,195` | 三個 func 改為 var（測試 seam，見 §8） |
| CLI | `cmd/apm-go/install.go` 旗標表 + `:2012-2035` | `--dev` 與持久化區段參數化（R9，見 §11） |
| 領域 | `internal/lockfile/` | `package_type` 欄位（R9.4） |
| 領域 | `internal/marketplace/authoring/editor.go:413-422` | `AddOptions` 加 `Category`（R10，見 §12） |
| CLI | `cmd/apm-go/marketplace_package.go` | `add --category` 旗標（R10） |
| 新套件 | `internal/pluginjson/`（見 §4） | `plugin.json` 產生 |

**不動**：`internal/manifest/manifest.go` 的雙鍵解析、`CanonicalTargets`、
`internal/marketplace/build/*`（schema 已驗證一致）、`internal/pack/*`。

---

## 2. apm.yml 產生器：從 `map[string]any` 改為有序 `yaml.Node`

### 問題

現行 `cmd/apm-go/init.go:229-246` 的 `buildManifestData` 回傳 `map[string]any`，
`yamllib.Marshal` 對 map 以**鍵字母序**輸出，且 map 無處掛註解。
R2 要求語意鍵序 + `targets:` 上方三行註解，兩者 map 都做不到。

### 方案（已用 probe 驗證）

改為直接組裝 `*yaml.Node` 的 MappingNode，註解掛在 **key node 的 `HeadComment`**，
再走既有的 `yamlcore.SafeDump`。

驗證結果（`SafeDump` 內部為 `yaml.NewDumper(WithV3Defaults(), WithLineWidth(-1), WithIndent(2))`，
`internal/yamlcore/safe.go:45-57`）：

```yaml
name: design-taste
# Which agent platforms to deploy to.
# Resolution order: --target flag > this field > auto-detect from filesystem.
# Accepted values: agent-skills, antigravity, claude
targets:
  - claude
  - codex
scripts: {}
```

與上游實跑產物（`research/eval-real-run-20260728.md` §B）**逐字相符**：
`# ` 前綴、2 空格序列縮排、空 map 輸出 `{}`。無需自訂 emitter，無需新相依。

### 新簽章

```go
type manifestSpec struct {
    Name, Version, Description, Author string
    Targets  []string
    Plugin   bool   // true 時插入 devDependencies
}

func buildManifestNode(spec manifestSpec) *yaml.Node
```

鍵序固定為：
`name → version → description → author → targets → dependencies → includes → [devDependencies] → scripts`

`targets` 為空時：不輸出 `targets` 鍵，改在 `author` 之後掛一個帶骨架註解的
FootComment（R2.4）—— 註解內容與有 targets 時的三行前綴相同，末尾多兩行註解掉的
`# targets:` / `#   - claude`。

### 既有驗證管線保留

現行 `init.go:189-203` 的
`Marshal → SafeLoad → manifest.ParseManifest → SafeDump` 自我驗證鏈**必須保留**，
只是起點改成 `buildManifestNode` 產出的 node，省掉 `Marshal → SafeLoad` 兩步：

```
buildManifestNode → SafeDump → SafeLoad → ParseManifest（驗證）→ 寫檔用 SafeDump 的輸出
```

先 Dump 再 Load 再驗證，是為了確保**寫進磁碟的那份 bytes** 通過驗證，
而不是驗證一個記憶體中的 node（避免 emitter 產出與驗證對象不一致）。

### 相容風險

`ParseManifest` 走的是 `targets` 鍵（`manifest.go:125`/`:240` 已支援），無需改動。
既有 init 測試若斷言字母序或單數 `target:`，會轉紅——**這是預期的**，
逐一改為斷言新鍵序（AC5）。

---

## 3. `init` 與 `plugin init` 的共用本體

上游 `commands/init.py:126-320` 是單一函式，用 `plugin=True` 參數分流。
本專案照同樣的形狀，但把「模式」收斂成一個值物件而非散落的 bool：

```go
type initMode struct {
    plugin        bool
    defaultYesVer string   // "1.0.0" | "0.1.0"
    validateName  func(string) error
    nextSteps     []string
}

var consumerMode = initMode{...}
var pluginMode   = initMode{...}

func runInit(cmd *cobra.Command, args []string, mode initMode, opts initOpts) error
```

- `initCmd()` 傳 `consumerMode`，`pluginInitCmd()` 傳 `pluginMode`。
- 兩個 cobra command **各自宣告旗標**（consumer 無 `-v`，plugin 有），
  不共用 FlagSet——避免 R3.3.f 的 `--verbose` 洩漏到 consumer。

### 六個 delta 的落點

| delta | 落點 |
|---|---|
| a. kebab-case 驗證 | `mode.validateName`；consumer 維持現行 `/` `\` `..` 檢查不變 |
| b. `--yes` version | `mode.defaultYesVer`，只影響 `init.go:97-101` 的 `--yes` 分支；互動表單預設值兩模式皆 `1.0.0` |
| c. `devDependencies` | `manifestSpec.Plugin` |
| d. `plugin.json` | Phase 6 之後，`mode.plugin` 為真時呼叫 `pluginjson.Write`（§4） |
| e. Next steps | `mode.nextSteps`，clack 與純文字兩條輸出路徑共用 |
| f. `--verbose` | 只在 `pluginInitCmd()` 宣告 |

### 名稱驗證的雙層關係

`plugin init my_plugin` 時：目錄建立**發生在名稱驗證之後**。
現行 `init.go:35-46` 是「驗證 → MkdirAll → Chdir」，
plugin 模式只是把 `validateName` 換成更嚴的規則，順序不變 —— 不會留下半建好的目錄。

---

## 4. `plugin.json` 產生：新套件 `internal/pluginjson`

**不重用** `internal/pack/bundle.PluginManifest`（`pluginjson.go:30-43`）。理由：

- `bundle.PluginManifest` 的語意是「**從 apm.yml 合成**」（`Synthesize`），
  上游對應 `synthesize_plugin_json_from_apm_yml`，且不含 `license` 的硬編值。
- `plugin init` 要的是「**樣板常數**」，`license` 硬寫 `"MIT"` 且不進 apm.yml
  （`research/agent-schema-support-matrix.md` §3.4 已證這是上游刻意的行為）。
- 兩者共用會讓 `pack` 的合成路徑意外拿到硬編 license，改變 SBOM 行為。

新套件只做一件事：

```go
package pluginjson

// Scaffold writes the plugin init template plugin.json to dir.
// Field order: name, version, description, author.name, license.
func Scaffold(dir string, name, version, description, author string) error
```

序列化沿用 `bundle.MarshalIndent`（已保證欄位序、不 HTML-escape），
輸出補一個結尾換行 —— 與上游 `json.dumps(indent=2) + "\n"` 一致。

**覆寫政策**：`plugin init` 對既有 `plugin.json` 的處理跟 apm.yml 同調 ——
走同一個 `--yes` / `--force` / 互動確認閘門，不另立規則。

---

## 5. plugin-native 根目錄警告（R4）

放在共用本體，兩個模式都跑。判定邏輯獨立成純函式以便測試：

```go
// internal/manifest 或 cmd/apm-go/init.go 內部
func pluginRootSources(root string) []string
```

- 目錄清單 `agents/ skills/ commands/ instructions/ extensions/ hooks/` 是常數
  （對應上游 `bundle/plugin_layout.py:5-12`）。
- **symlink 必須排除**：用 `os.Lstat` 判 `ModeSymlink`，不能用 `os.Stat`
  （`os.Stat` 會跟隨 symlink，導致 AC16 失敗）。
- `hooks.json` 用 `Lstat` + `Mode().IsRegular()`。
- `.apm/` 存在時整個檢查短路。

輸出走 `ck.Warn` / `ux.Warn` 兩條路徑，不阻斷（AC14 要求 exit 0）。

---

## 6. `resolveRef` 的隱含 HEAD（R5）

現行 `editor.go` 的 `resolveRef` 在 ref 為空時直接短路。改為：

```
version != ""            → 回傳 ""（不解析、不觸網）        [現行行為，保留]
isLocalPackageSource(src) → 回傳 ""（不觸網）               [mkt-046 契約，保留]
ref 是 40-hex SHA        → 原樣回傳                        [現行行為，保留]
ref == "" 或 EqualFold(ref,"HEAD")：
    noVerify → exit 2 + "Cannot resolve HEAD ref without network access. ..."
    ref == "HEAD"（顯式）→ 先印 mutable-ref 警告
    → lister.ListRefs(src) 解析 HEAD → 回傳 SHA
其他（tag/branch 名）    → 現行解析邏輯不變
```

**關鍵順序**：local 判定必須排在隱含-HEAD 判定**之前**，否則
`package add ./local-pkg`（零旗標）會落進隱含 HEAD 而觸網，
打破 `editor_test.go:18` 的 `panicLister` 契約（AC21）。

**exit code 2** 沿用 `cmd/apm-go/marketplace_package.go` 既有的 exit-2 機制
（該檔案的 add/remove/set 已有此約定），不新增 exit-code 基礎設施。

---

## 7. target 集合單一來源（R8）

現況四份獨立字面量。收斂為：

```go
// internal/manifest/target.go
// 單一事實來源：有部署 adapter 的 target，順序即 init 互動選單順序。
var deployTargets = []string{
    "copilot", "claude", "opencode", "codex", "antigravity", "agent-skills",
}

var SupportedTargets = deployTargets              // init --target 白名單
var adapterTargets   = setOf(deployTargets)       // HasAdapter
```

- `promptTargetsOrdered`（`cmd/apm-go/init.go:17-19`）刪除，改用 `manifest.SupportedTargets`。
- R2.2 註解第三行由 `SupportedTargets` 排序後 join 產生，不是字面量。
- 順序取現行 `promptTargetsOrdered` 的（copilot 優先），`agent-skills` 附加在尾端 ——
  互動選單的既有順序不變，只多一項，對現有使用者衝擊最小。
- `CanonicalTargets` 保持獨立且不動（它是 apm.yml 詞彙，涵蓋 10 個 + `all` 是正確的）。

一個測試同時鎖住三件事：`SupportedTargets` 與 `adapterTargets` 同集合、
註解清單由它推導、選單由它推導（AC25/AC26）。

---

## 8. 互動路徑測試（R7）

`internal/ux/ux.go:56-62` 已有 `stdinIsTTY` / `stderrIsTTY` / `stdoutIsTTY`
三個可抽換 var。測試策略：

**已查證**：huh 層目前**沒有**注入點。`internal/ux/interactive.go:84`、`:149`、`:195`
的 `confirmWith` / `multiSelectWith` / `inputFormWith` 都是普通 func。
而 `Clack.Confirm`/`Form`/`MultiSelect`（`clack.go:201`/`:217`/`:248`）在
`CanPrompt()` 為假時直接走非互動 fallback ——
所以「只把 TTY var stub 成 true」會讓測試真的進 huh 然後卡住。

### 需要新增的 seam（3 行）

把那三個 func 改成 package-level var：

```go
var confirmWith     = func(theme huh.Theme, prompt string, def bool) (bool, error) { ... }
var multiSelectWith = func(theme huh.Theme, title, description string, showHelp bool, opts []Option) ([]string, error) { ... }
var inputFormWith   = func(theme huh.Theme, title string, showHelp bool, fields []Field) (map[string]string, error) { ... }
```

與 `ux.go:56-62` 既有的 `stdinIsTTY` / `stderrIsTTY` 完全同一種手法
（該處註解自稱 "swappable seams for tests; production code always uses the
default* implementations"），是本 codebase 自己的既有慣例，不是新發明。
不新增任何相依。

### 這個 seam 是必要而非加分項

AC2 / AC3 斷言的是「MultiSelect 的**預選狀態**包含既有 targets」——
預選狀態只存在於傳給 `multiSelectWith` 的 `opts` 裡，
沒有這個 seam 就無法斷言，R1.1 的修正等於沒有測試保護。

### 測試形狀

```
stub stdinIsTTY/stderrIsTTY → true
stub multiSelectWith → 記錄收到的 opts，回傳固定選擇
stub inputFormWith   → 回傳固定值
stub confirmWith     → 回傳 true
執行 plugin init / init，斷言 opts[i].Selected 與產物
```

不引入 PTY（`creack/pty` / `charmbracelet/x/conpty` 僅存在於 `go.sum`，
拉成直接相依且 Windows 需走 ConPTY，成本不成比例）。

---

## 9. 相容性

| 情境 | 行為 |
|---|---|
| 既有 apm.yml 寫單數 `target:` | 繼續可讀可部署（`manifest.go:119`/`:238` 不動） |
| 既有 apm.yml 寫複數 `targets:` | 同上 |
| 對既有 apm.yml 跑 `init` | 走既有覆寫閘門；新產物用複數 + 新鍵序 |
| 既有 `targets: [cursor]` | 照常解析 + req-tg-004 warning（不變） |
| 既有 `.claude-plugin/plugin.json` | 不受影響（`pack` 路徑未改） |
| 既有 marketplace apm.yml | 不受影響，除了新 `package add` 會多寫 `ref:` |

**唯一的行為破壞**：`marketplace package add owner/repo` 零旗標時，
之前不觸網、不寫 `ref:`，之後會觸網解析 HEAD。
離線環境下這會從「靜默成功」變成「失敗」——但這正是與上游對齊的目的
（避免寫出未 pin 的 entry）。`--no-verify` 之下給出明確的 exit 2 與補救指示。

---

## 11. `install --dev`（R9）

### 為什麼成本比原估計小一個量級

原估計「貫穿 install/lock/update/uninstall」是錯的。實際讀過的證據：

| 子系統 | 已有的 dev 支援 |
|---|---|
| install | `install.go:433` 正規化 `ParsedDevDeps`、`:457` `hasAnyDeps` 含 dev、`:1030-1035` 解析時合併 |
| update | `update.go:94`、`:303-305` 的 `directGitSemverUpdateScope` 明載 dev 同 scope |
| uninstall | `uninstall_resolve.go:58` 的 `IsDev`、`:253`/`:273` 掃 dev 區段、`uninstall.go:586` 分流 |
| pack | `pack.go:299-300` 建 `devKeys` |
| compile | `compile.go:76` 合併 dev |
| 測試 | `install_test.go:126` `TestRunInstall_DevDependency_ResolvedDeployedAndLocked` 已證明手寫 dev entry 能解析→部署→入 lockfile |

⇒ **讀取端全通**。缺的只有寫入端。

### 實際要改的三處

1. **旗標**：`install` 加 `--dev`（bool）。
2. **持久化區段參數化**：`persistPackagesToManifest`（`install.go:2012`）目前在
   `:2024` 把 `"dependencies"` 寫死。改為接受區段名參數，
   `devDependencies` 不存在時比照 `:2029-2035` 既有的建立邏輯自動建立。
   **鍵序**：新建的 `devDependencies` 必須落在 `includes` 與 `scripts` 之間（R2.1），
   不能直接 append 到 mapping 尾端 —— 這與 §2 的有序產生器是同一個鍵序契約。
3. **lockfile `package_type`**：`internal/lockfile/write.go:20` 的欄位排序白名單
   已列出這個鍵，`:494`/`:501` 的排除清單也提到它，但全 repo `grep PackageType`
   零命中 —— 是宣告了卻沒有對應 Go 欄位。需補欄位 + parse/write/equality。

### 不做的事

不重構既有的 dev 讀取鏈。那些已經有測試覆蓋，動它只會增加回歸面。

---

## 12. `marketplace package add --category`（R10）

`packageEntryNode`（`editor.go:288-290`）**已經**有
`if entry.Category != "" { putStr("category", entry.Category) }` ——
寫出路徑是通的，只是沒有任何呼叫端會填這個欄位。

所以改動只有三處：

1. `AddOptions`（`editor.go:413-422`）加 `Category string`
2. `AddPackage` 把 `opts.Category` 帶進 entry
3. `cmd/apm-go/marketplace_package.go` 的 `add` 加 `--category` 旗標
   （**只加在 add**；`set` 維持上游旗標集合，見 `marketplace_package_test.go:36-43`
   已有的「set 不得有 add-only 旗標」測試）

### 警告而非阻斷

`outputs` 含 codex 且該 entry 缺 category 時，add 印警告後**照常寫入**。
理由：`schema.go:12-21` 的 compose-time-only 閘門設計要保住 —— 它讓
`apm-go pack -m claude` 在有 codex-missing-category 的 package 時仍能成功，
這一點確實優於上游（上游整個 add 都被擋）。
補了 `--category` 之後，codex 情境也不再有無效中間狀態，兩邊都優於上游。

---

## 13. 刻意不做的事（附理由，避免日後被當成遺漏）

- **`marketplace package add` 的 add-time `category` **阻斷**（改為警告）**：
  維持 `schema.go:12-21` 的 compose-time-only 閘門，讓 `pack -m claude` 仍能成功。
  上游的阻斷式做法在 add 沒有 `--category` 的前提下是死結
  （`research/eval-real-run-20260728.md` §C4 實跑三次皆失敗）。
  但**原本「維持現狀即較佳」的宣稱不成立** —— 見 §12 與 `prd.md` C1 的收窄版本；
  補 `--category` 才真的解決。
- **`marketplace check` 的三個診斷維度**（`reachable`/`version_found`/`ref_ok`）：
  本 task 只裁定不改 UI 形狀。**這不是純呈現層** ——
  `refcheck.go:131` 的 `CheckResult` 只有 `Package` + 單一 `Err`，
  上游 `check.py:130-225` 有三個獨立維度。資訊等價與否是未裁定的獨立問題，
  成本 60–120 LOC。
- **`install` 的全面交易化**：本 task 不改 apm.yml 的寫入順序。
  **收窄理由**：apm.yml 寫得比 resolution 晚是事實（`:1777` vs `:748`/`:766`），
  但 `deploy.Run` 在 `:1620` 就已改動部署目標、lockfile 到 `:1764` 才寫，
  所以「deploy 成功、lock 失敗」仍會留下不一致狀態。
  不可用這三個行號宣稱 install 整體具交易性。全面交易化估 200+ LOC + failure injection。
- **cursor/gemini/kiro/windsurf adapter**：產品邊界（`prd.md` D3）。
  **成本未驗證** —— 現有簡單 adapter 只有 19–39 行
  （`internal/deploy/agentskills.go`、`claude.go`），未做 reconnaissance 前
  不能定級為「大工程」。
- **真 PTY 端對端**：§8 的 seam 只能測參數資料，測不到真 binary 的
  stdin/stderr wiring、Ctrl-C、escape sequence、終端尺寸。這是**明確承認的覆蓋缺口**，
  不是「已被 seam 覆蓋」。

（`install --dev` 已從本清單移除 —— 見 §11，改為納入本 task。）
