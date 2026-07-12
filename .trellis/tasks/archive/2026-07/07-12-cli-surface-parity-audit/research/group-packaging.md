# Research: CLI 全指令面 parity 盤查 — packaging 分組(pack/plugin/publish/unpack/search)

- **Query**: pack(兩邊都有，DIVERGENT 深掘重點)、plugin、publish、unpack、search(Python only) 的類別判定、行為對照、嚴重度與處置建議
- **Scope**: mixed(內部原始碼 file:line 對照 + TEMP scratch live probe 實跑；publish 只讀不實跑)
- **Date**: 2026-07-12

## 總覽

| 指令 | 類別 | 嚴重度 |
|---|---|---|
| `pack` | **DIVERGENT-SAME-NAME** | **HIGH** |
| `plugin` (`plugin init`) | MISSING | MEDIUM |
| `publish` | MISSING | MEDIUM |
| `unpack` | MISSING | LOW |
| `search` | MISSING | MEDIUM |

---

## 1. `pack` — DIVERGENT-SAME-NAME（本組深掘重點）

### 1.1 Python `apm pack` 的完整行為面

Python 的 `apm pack` 不是單一功能，而是一個由 `BuildOrchestrator`
(`D:/Projects/apm-dev/apm/src/apm_cli/core/build_orchestrator.py:401-435`)
依 `apm.yml` 內容**動態路由到最多三個 producer** 的組合指令：

| Producer | 觸發條件(`detect_outputs`, `build_orchestrator.py:346-393`) | 輸出 |
|---|---|---|
| `BundleProducer` | `apm.yml` 有非空 `dependencies:` 區塊 | `./build/<name>-<version>/`（目錄或 `--archive` 壓縮檔） |
| `MarketplaceProducer` | 有非空 `marketplace:` 區塊（或 legacy `marketplace.yml`） | `.claude-plugin/marketplace.json`（+ 可選 `.agents/plugins/marketplace.json`） |
| `PluginManifestProducer` | `target:`/`targets:` 含 `claude` 或 `copilot` | 專案根目錄 `.claude-plugin/plugin.json` 或 `.github/plugin/plugin.json` |

三個 producer **可同時觸發**（`apm.yml` 同時有 `dependencies:`、`marketplace:`、
`target: claude` 時三個都跑），對應 `_PACK_HELP`
(`commands/pack.py:24-61`) 明文的四種組合：「dependencies only / marketplace
only / target only / 全部同時有」。

**BundleProducer 細節**（`bundle/packer.py:30-297` → 預設 `--format plugin`
委派給 `bundle/plugin_exporter.py:416-675`）：
- 讀 `apm.lock.yaml`（若存在）+ `apm.yml`，收集 `.apm/{agents,skills,prompts→commands,instructions,commands,extensions}/`
  與根層 plugin-native 目錄（`plugin_exporter.py:86-129`）
- **plugin.json 合成**：`_find_or_synthesize_plugin_json`
  (`plugin_exporter.py:336-352` → `core/plugin_manifest.py:286+`) 找不到既有
  `plugin.json` 就從 `apm.yml` 的 name/version/description 合成一份；既有
  `plugin.json` 的 `agents`/`skills`/`commands`/`instructions` key 會被剝除
  （schema 不允許，因為這些目錄是 Claude Code 自動探索的 convention 目錄，
  `_update_plugin_json_paths`, `plugin_exporter.py:365-397`）
- **hooks/MCP 合併**：`.apm/hooks/*.json` + 根層 `hooks.json`/`hooks/` 深度合併成
  `hooks.json`；`.mcp.json` 的 `mcpServers` 合併（`plugin_exporter.py:212-284`）
- **license 提示（非 SBOM 生成本身）**：`export/authoring.py:32-52`
  `warn_if_license_undeclared` 只在 `apm.yml` 缺 `license:` 時印一則警告——訊息文字
  提到「SBOM 會記 NOASSERTION」，但**實際 SBOM 匯出是另一個指令**
  `apm lock export --format cyclonedx|spdx`（`commands/lock.py:283-309`
  → `export/sbom.py`），不在 `pack` 本身；`pack` 呼叫點只有
  `commands/pack.py:329-332`(bundle 產出前) 與 `commands/publish.py:96-100`
  （見下節）
- **hidden-character 安全掃描**：warn-only（不阻擋），`plugin_exporter.py:568-593`
  用 `security.gate.SecurityGate` 掃過每個要打包的檔案
- **內嵌 `apm.lock.yaml`**：`plugin_exporter.py:632-660` 把 bundle 內每個檔案
  SHA-256 雜湊寫進 `pack.bundle_files`，供 install-time 完整性驗證
- 輸出佈局固定 `build/<name>-<version>/`（`_sanitize_bundle_name`,
  `plugin_exporter.py:52-61`），`--archive` 才壓成 zip/tar.gz 並刪掉目錄

**MarketplaceProducer**（`build_orchestrator.py:132-240`）委派給
`marketplace/builder.py` 的 `MarketplaceBuilder`——輸出到
`.claude-plugin/marketplace.json`（claude）/`.agents/plugins/marketplace.json`（codex）。

**PluginManifestProducer**（`build_orchestrator.py:248-338`）依
`target:`/`targets:` 呼叫 `core/plugin_manifest.py` 的
`build_plugin_manifest`/`write_plugin_manifest`，寫到專案根目錄（**不是**
`build/` 目錄下）的 `.claude-plugin/plugin.json`（claude）或
`.github/plugin/plugin.json`（copilot）——這是「作者宣告自己的專案是一個
claude/copilot plugin」的獨立產出面，與 BundleProducer 的 plugin.json（在
`build/<name>-<version>/plugin.json` 內）是兩份不同檔案、不同用途。

Exit codes（`_PACK_HELP`）：0 成功、1 build/runtime 錯誤、2 manifest schema
驗證錯誤、3 `--check-versions` 版本對齊失敗、4 `--check-clean` marketplace
working-tree drift。

### 1.2 apm-go `apm-go pack` 的完整行為面

`cmd/apm/pack.go:1-309` 的文件註解**明文承認**範圍窄化：

> "This sub-task's scope is marketplace.json generation only (design.md's
> 「範圍界定」) -- the Python original's plugin-bundling half of `apm pack`
> (--format/--archive/-o etc.) is out of scope"（`pack.go:24-28`）

apm-go pack 只實作 Python 三個 producer 中的**一個**（MarketplaceProducer 的
對等物）：

- `hasMarketplaceConfig` (`pack.go:285-309`) 檢查 `apm.yml` 的 `marketplace:`
  或 legacy `marketplace.yml`；**完全不檢查 `dependencies:` 或
  `target:`/`targets:`**
- 無 `marketplace:` 區塊 → 印 `[i] No 'marketplace:' block found ...
  nothing to do.` 並 exit 0（`pack.go:87-89`），**即使 `apm.yml` 有
  `dependencies:` 區塊或 `target: claude`，也完全靜默跳過，不產生任何輸出、
  不印警告**
- `build.ResolvePackages` (`internal/marketplace/build/builder.go:139`) →
  `build.ClaudeMapper{}.Compose` (`internal/marketplace/build/mapper.go:93`)
  / `build.CodexMapper{}.Compose`
  (`internal/marketplace/build/codexmapper.go:89`) → `build.WriteOutput`
  (`internal/marketplace/build/output.go:202`)
- Exit codes：0 成功、1 涵蓋所有 build/config 錯誤；**2/3/4 完全不存在**
  （`--check-versions`/`--check-clean` 旗標本身沒有實作，不是空殼 —
  `pack.go:30-36` 註解明確記錄這是刻意決定）

apm-go 沒有 `--format`、`--archive`、`--archive-format`、`-o/--output`、
`--force`、`--json`、`--legacy-skill-paths`、`--check-versions`、
`--check-clean` 這些 Python 旗標（全部屬於 BundleProducer/gate 半，本來就不
在 apm-go pack 的範圍內）。

### 1.3 Scratch live probe — 三種 transcript 實證

安全鐵則：全程在 `%TEMP%\apm-pack-probe*` 建立獨立 scratch 專案，未動任何
repo 根目錄；Python 端執行的是唯讀分析型 `apm pack`（不牽涉 publish/
self-update 等寫入外部狀態的指令）。

**Probe A — 只有 `dependencies:`（無 `marketplace:`）**

`apm.yml`：
```yaml
name: probe-pkg
version: 0.1.0
dependencies:
  apm: []
```
（另有 `.apm/skills/hello/SKILL.md`）

Python 端：
```
$ uv run apm pack -v
[!] No 'license:' field in apm.yml; the SBOM will record NOASSERTION ...
[i] No plugin.json found; synthesising from apm.yml.
[*] Packed 2 file(s) -> build\probe-pkg-0.1.0
    skills/hello/SKILL.md
    plugin.json
[i] Plugin bundle ready -- ...
[i] Share with: apm install build\probe-pkg-0.1.0
```
→ 產生 `build/probe-pkg-0.1.0/{plugin.json, skills/hello/SKILL.md}`。

apm-go 端（同一個 `apm.yml`，`bin/apm-go.exe pack -v`）：
```
[i] No 'marketplace:' block found (neither apm.yml's marketplace: block nor a legacy marketplace.yml exist); nothing to do.
```
exit 0，**完全沒有寫任何檔案**（`find . -maxdepth 2` 前後一致，`build/`
目錄根本不存在）。

**Probe B — 同時有 `dependencies:` 與 `marketplace:`（Python 文件明示的
「both blocks present」情境）**

`apm.yml` 追加：
```yaml
marketplace:
  owner:
    name: acme-org
    url: https://github.com/acme-org
  build:
    tagPattern: "v{version}"
  outputs:
    claude: {}
  packages:
    - name: local-tool
      source: ./local-pkg
      description: A locally vendored tool
      version: 0.1.0
```
（`local-pkg/apm.yml` 為本地套件，未觸網）

Python 端：同時產出 `build/probe-pkg-0.1.0/{plugin.json, skills/...}`
**與** `.claude-plugin/marketplace.json`（內容：
`{"name":"probe-pkg","owner":{...},"plugins":[{"name":"local-tool",...,"source":"./local-pkg"}]}`）。

apm-go 端：只產出 `.claude-plugin/marketplace.json`，內容**與 Python 端
byte-for-byte 相同**（marketplace.json 生成本身對這個案例是 parity 的）；
`build/` 目錄完全不存在，`dependencies:` 區塊被靜默忽略，無任何警告提示
使用者「pack 沒有處理你的 dependencies 區塊」。

**Probe C — 只有 `target: claude`（無 `dependencies:`/`marketplace:`）**

`apm.yml`：
```yaml
name: probe-pkg2
version: 0.1.0
target: claude
```

Python 端：
```
[+] Generated plugin manifest: .../.claude-plugin/plugin.json
```
內容 `{"name":"probe-pkg2","version":"0.1.0","description":"..."}`，寫在
**專案根目錄**（不是 `build/` 下）。

apm-go 端：同樣印 `[i] No 'marketplace:' block found ... nothing to do.`，
exit 0，什麼都不寫。

### 1.4 結論

- MarketplaceProducer 這一半（`marketplace:` → `marketplace.json`）在
  apm-go 是 **PARITY-VERIFIED**（Probe B 已證明簡單案例 byte-identical；
  完整的 mapper 演算法對照見已歸檔的
  `.trellis/tasks/archive/2026-07/07-03-marketplace-pack/design.md`，
  該子任務的 AC4 要求跑過 A/B 測試）。
- BundleProducer（`dependencies:` → plugin bundle 目錄/archive）與
  PluginManifestProducer（`target:`/`targets:` → 專案根 plugin.json）
  兩個 producer 在 apm-go **完全缺席**——不是「行為不同」，是「同名指令下
  這兩個功能面根本不存在，且靜默不報錯、不警告」。這正是 PRD 觸發背景 #2
  描述的「同名完全不同功能，使用者實跑輸出天差地遠」的教科書案例：一個
  對 `dependencies:` 專案跑 `apm-go pack` 期望拿到 plugin bundle 的使用者，
  只會看到「無 marketplace 區塊，無事可做」，得不到任何輸出也得不到任何
  「這功能不存在」的明確錯誤——**這比單純的 MISSING 指令更危險**，因為
  指令名稱、指令存在、exit code 0 全部正常，只有輸出內容是空的。
- `apm pack` 對應 apm-go 哪個功能：**marketplace authoring/pack 子任務
  （已歸檔 `07-03-marketplace-pack`）刻意把範圍收斂成「只做
  marketplace.json 產生器」**，設計文件本身承認這只是 Python `apm pack`
  三個功能面裡的一個，plugin 打包（BundleProducer）「不在此輪」
  （design.md:8）。這是一個**已知、有意識做出的範圍決策**，不是遺漏。
- Python 的 `apm pack`（plugin bundle 打包 + 專案根 plugin.json 兩個面）
  在 apm-go **完全缺席**，且 apm-go 也沒有其他指令頂替（`compile` 是
  agents-family AGENTS.md 編譯，與 plugin.json/bundle 打包無關，見
  `cmd/apm/compile.go:17-23` 的自我註記——已由 07-11 child task 涵蓋，
  COVERED-ELSEWHERE，但那是另一個功能，不能替代 pack 的 bundle 面）。

### 1.5 嚴重度與處置建議

**嚴重度：HIGH**（DIVERGENT-SAME-NAME 分類法定義的最高風險：靜默誤導；
本案例的靜默程度比一般 DIVERGENT 更嚴重——完全無輸出、exit 0、無警告）。

處置建議（三選一，留給 triage）：
1. **修 parity**：另開 task 把 BundleProducer（plugin bundle 打包）與
   PluginManifestProducer（`target:` → 專案根 plugin.json）補進
   `apm-go pack`，讓同名指令行為對齊。工作量大（bundle 打包涉及
   hooks/MCP 合併、hidden-char 掃描、lockfile 內嵌 SHA-256 manifest 等一整
   套邏輯，目前 apm-go 完全沒有對應套件）。
2. **documented extension（範圍限定文件化）**：維持現狀，但在
   `apm-go pack --help`／README 明確寫「本指令只做 marketplace.json 生成，
   不含 Python `apm pack` 的 plugin bundle 打包」，並在 `dependencies:`/
   `target:` 存在但 `marketplace:` 不存在時**印一則警告**（而不是完全靜默
   `nothing to do`），提醒使用者這兩個區塊不會被處理——這是**成本最低、
   風險降最多**的立即動作。
3. 記錄不做：若團隊確認 apm-go 生態不需要 plugin bundle 打包（例如
   plugin 打包功能已被其他工具/流程取代），在 register 中明確記錄決策
   依據。

---

## 2. `plugin`（`apm plugin init`）— MISSING

### 2.1 Python 端

`apm plugin`（`src/apm_cli/commands/plugin/__init__.py:1-22`）是一個只有
一個子指令 `init` 的 group：

```
Usage: apm plugin [OPTIONS] COMMAND [ARGS]...
  Scaffold and manage plugins (plugin-author workflows)
Commands:
  init  Scaffold a plugin (creates plugin.json + apm.yml)
```

`apm plugin init`（`src/apm_cli/commands/plugin/init.py:1-45`）是
`apm init --plugin`（deprecated legacy flag，`commands/init.py:89-121`）的
薄包裝，兩者共用 `_perform_init(..., plugin=True, ...)`
(`commands/init.py:124-356`)：
```
Usage: apm plugin init [OPTIONS] [PROJECT_NAME]
Options:
  -y, --yes        Skip interactive prompts and use auto-detected defaults
  --target TARGET  Comma-separated target list (skip prompt, write directly)
  -v, --verbose    Show detailed output
```

`plugin=True` 時的差異化行為（`commands/init.py:167-294`）：
- 專案名稱要通過 `_validate_plugin_name`（kebab-case、字母開頭、≤64 字）
- `apm.yml` 用 `_create_minimal_apm_yml(config, plugin=True)` 產生（含
  `devDependencies` 骨架）
- 額外產生一份 `plugin.json`（`_create_plugin_json`）
- `--yes` 模式下版本預設 `0.1.0`
- 「Next steps」提示改成 plugin-author 導向（`apm install --dev`、
  `apm pack`）而非 consumer 導向

### 2.2 apm-go 端

`bin/apm-go.exe init --help` 沒有任何 `--plugin`/`-y`(對應項待查) 等價
旗標；`apm-go` 的 13 指令面完全沒有 `plugin` 這個 group 或子指令
（見指令總表）。原始碼面：`cmd/apm/init.go` 內搜尋 `plugin`/`Plugin`
無任何命中——連 legacy `--plugin` flag 的等價物都不存在。

### 2.3 嚴重度與處置建議

**嚴重度：MEDIUM**（MISSING 分類法定義：unknown command，使用者會清楚
看到「找不到這個指令」而非被誤導，但這是 plugin 作者上手流程的入口點，
缺失會讓 plugin 作者無法用 apm-go 走「一鍵 scaffold plugin.json +
apm.yml」的路徑，只能手動寫兩份檔案）。

處置建議：另開 task 視 apm-go 的 plugin-author workflow 優先度決定是否
補上 `apm-go plugin init`（或 `apm-go init --plugin`）；若近期不打算做，
記錄不做即可——功能本身不複雜（本質是 `apm-go init` 的一個變體
+ plugin.json 樣板寫出）。

---

## 3. `publish` — MISSING（絕不實跑，僅 --help + 原始碼 + 唯讀探測）

### 3.1 Python 端

`src/apm_cli/commands/publish.py:1-288`。`apm publish` 把目前目錄打包成
一份**扁平 registry zip**（`apm.yml` + `.apm/` 在 archive 根層，**不是**
`apm pack --archive` 的 plugin bundle 包裝——docstring 特別強調兩者不同，
`publish.py:26-28,193-203`），透過
`PUT /v1/packages/{owner}/{repo}/versions/{version}` 上傳到
`apm.yml` 的 `registries:` 區塊指定的 registry
(`docs/proposals/registry-api.md §5.3`)。

```
Usage: apm publish [OPTIONS]
Options:
  --registry TEXT       Registry name (from apm.yml 'registries:' block).
  --package OWNER/REPO  Package identity to publish as.  [required]
  --zip FILE             Path to a pre-built .zip archive. Skips the pack step.
  --dry-run              Preview without uploading.
  -v, --verbose          Show detailed output.
```

前置閘門：`require_package_registry_enabled("apm publish")`
(`publish.py:75`) — 整個指令被 `apm experimental enable registries`
gate 住，屬於 Python 端自己也標記為 experimental 的功能。

其他行為要點：
- 沒有 `--zip` 時，`_pack_archive` (`publish.py:193-249`) 現場打包
  `.apm/` + `apm.yml` + 標準文件（README/CHANGELOG/LICENSE，
  npm-style，大小寫不敏感比對）成 `{name}-{version}.zip`
- 同樣有 `warn_if_license_undeclared` 提示（`publish.py:96-100`，
  與 pack 共用 `export/authoring.py`）
- HTTP 錯誤碼轉譯成人類可讀訊息（409 版本已存在且不可變、422 驗證失敗、
  403 無權限、401 認證失敗，`publish.py:252-287`）

### 3.2 apm-go 端

`apm-go` 13 指令面沒有 `publish`。但**並非完全空白基礎設施**：

- `internal/experimental/experimental.go:29-32` 已定義同名
  `"registries"` experimental flag（`experimental list` 可見
  `registries  disabled  Enable REST-based APM package registries in apm.yml.`），
  與 Python 端閘門同名對齊
- `internal/registry/client.go:36-180` 已有 registry **consumer**
  端（`ListVersions`/`Download`/`FetchURL`/`ArchiveURL`）——這是已歸檔
  子任務 `07-01-registry-consumer` 的產出，其 prd.md 第 23-26 行**明文
  記錄**「Registry server 端（`PUT`/publish）」是**刻意排除、留給 v0.2**：
  > "Out of scope (record as explicitly deferred, not silently dropped):
  > ... Registry **server** wire (`PUT`/publish — v0.2 per spec)."
- 因此 `publish` 對 apm-go 而言是消費/生產不對稱的已知缺口：使用者可以
  用 `apm-go install`（透過 `registries:` 設定）從 REST registry
  **拉**套件，但無法用 apm-go 本身**推**套件到 registry——仍要靠 Python
  `apm publish` 或手動 `curl PUT`。

### 3.3 嚴重度與處置建議

**嚴重度：MEDIUM**（雖然 Python 端本身也是 experimental/非預設功能，
但已有明確的 v0.2 路線圖承諾，且消費端已經做了一半——這是一個「已規劃、
未完成」的缺口，比一般 MISSING 更值得追蹤）。

處置建議：另開 task（若要做，直接對齊已歸檔 `07-01-registry-consumer`
排除的「v0.2」範圍）；短期記錄不做——原始碼與 archived task 已經把
「為什麼不做、何時做」記錄清楚，不需要本次登記冊重複發明。

---

## 4. `unpack` — MISSING（低優先）

### 4.1 Python 端

`apm unpack`（`src/apm_cli/commands/pack.py:725-843`，委派
`bundle/unpacker.py:37+` 的 `unpack_bundle`）已經是 **Python 自己標記為
deprecated** 的指令：

```
Usage: apm unpack [OPTIONS] BUNDLE_PATH
  [Deprecated] Extract an APM bundle into the current project. Use
  'apm install <bundle-path>' instead -- this command will be removed
  in a future release.
Options:
  -o, --output PATH          Target directory (default: current directory).
  --skip-verify               Skip bundle completeness check.
  --dry-run                   Show what would be unpacked without writing
  --force                      Deploy despite critical hidden-character findings.
  --trust-canvas-extensions    Deploy executable canvas extensions (.github/extensions/) from the bundle.
  -v, --verbose                Show detailed unpacking information
```

執行時第一行就印棄用警告（`pack.py:763-766`）。行為：additive-only 解壓
（只寫 lockfile `deployed_files` 列出的檔案，不刪除既有檔案，同名檔案
以 bundle 版本覆蓋）、完整性驗證（比對 lockfile）、hidden-character
安全掃描（critical findings 預設擋下，`--force` 可覆蓋）、canvas
extension 執行檔預設封鎖（`--trust-canvas-extensions` 才解封）、
bundle target 與目標專案 target 不一致時警告。

### 4.2 apm-go 端

13 指令面沒有 `unpack`。但 `apm-go install --help` 已有
`--max-archive-bytes`/`--max-entries`（`cmd/apm/install.go` 對應
req-sc-004 zip-bomb 防護）等旗標，顯示 apm-go 的 `install` 指令**本身
已經承接了「從本地/遠端 bundle 安裝」的能力**——這正是 Python 端自己
建議的替代路徑（"Use 'apm install <bundle-path>' instead"）。`install`
的完整行為（含是否涵蓋 local bundle 目錄/archive 安裝、additive-only
語意、hidden-char 掃描對齊）屬於已列為 COVERED-ELSEWHERE 的
install/uninstall/marketplace 75 項 checklist 範疇，本組未重新逐項驗證，
僅在此記錄「Python 自己也在淘汰 unpack，且 apm-go install 已具備
zip-bomb 防護等對應安全機制」這個脈絡性事實。

### 4.3 嚴重度與處置建議

**嚴重度：LOW**（MISSING，但 Python 上游自己都在淘汰這個指令，且
apm-go 已有 `install <bundle-path>` 路徑可能是功能對等的後繼者——需要
另一輪聚焦在 `install` 本身的 checklist 驗證才能確認是否 100% 覆蓋
unpack 的行為子集，不在本組範圍）。

處置建議：記錄不做。若後續發現 `apm-go install` 未完整覆蓋 unpack 的
additive-only/hidden-char/canvas-block 語意，屆時再由 install checklist
的複查另開 task；不建議專門為一個上游都要移除的指令另開實作任務。

---

## 5. `search` — MISSING（`apm marketplace search` 的頂層別名）

### 5.1 Python 端

`apm search` 不是獨立實作，是 `apm marketplace search` 的**別名**
（`cli.py:35,192`：`from apm_cli.commands.marketplace import search as
marketplace_search` → `cli.add_command(marketplace_search, name="search")`）。
本體在 `src/apm_cli/commands/marketplace/__init__.py:1341-1400+`：

```
Usage: apm search [OPTIONS] QUERY@MARKETPLACE
  Search plugins in a marketplace (QUERY@MARKETPLACE)
Options:
  --limit INTEGER  Max results to show  [default: 20]
  -v, --verbose    Show detailed output
```

行為：解析 `QUERY@MARKETPLACE` 格式（`@` 分隔，兩段都必須非空）→
`get_marketplace_by_name` 查已註冊的 marketplace（查無 → exit 1）→
`search_marketplace(query, source)[:limit]`（`marketplace/client.py`）
對該 marketplace 的 plugin 清單做查詢過濾 → 印結果表格；查無結果時提示
`apm marketplace browse <name>` 看全部。純唯讀操作，不寫入任何狀態。

### 5.2 apm-go 端

13 指令面沒有頂層 `search`。`apm-go marketplace` 有 `browse NAME`
（`marketplace browse --help`：「Force-refresh and list the plugins in a
registered marketplace」），但 `browse` **只列出全部**，沒有
`QUERY@MARKETPLACE` 式的關鍵字過濾/`--limit` 參數——是相鄰但不等價的能力
（`browse` = Python 的「查無結果時建議跑的那個指令」，`search` = 帶查詢
過濾的窄化版）。

### 5.3 嚴重度與處置建議

**嚴重度：MEDIUM**（MISSING；`browse` 提供了部分替代路徑——使用者可以
`apm-go marketplace browse NAME | grep query` 手動過濾，功能性缺口不算
致命，但體驗上是退化：少了原生 query 語法與 `--limit`）。

處置建議：另開 task 視優先度決定是否在 `apm-go marketplace` 底下補
`search`（或幫 `browse` 加 `--query`/`--limit` 旗標達到等價效果，成本
低於全新指令）；若優先度低，記錄不做。

---

## Caveats / Not Found

- SBOM 生成本身（`apm lock export --format cyclonedx|spdx`）不在本組
  範圍內（屬於 `lock`/`deps` 指令家族，另一組負責）；本文件只釐清
  `pack`/`publish` 對它的**引用關係**（license 未宣告警告訊息提到 SBOM，
  但 SBOM 匯出動作本身是另一個指令），避免與 PRD 描述的「pack 的
  SBOM/license 檢查」產生誤解——精確地說 pack 只做 license 缺失的
  **提示**，不做 SBOM 生成。
- `unpack` 對應的 `apm-go install <bundle-path>` 完整行為子集覆蓋度
  未在本組逐項驗證（COVERED-ELSEWHERE 候選，留待 install checklist
  複查確認）。
- `plugin init`/`search` 的 apm-go 對應功能（`init --plugin` 等價物、
  `marketplace search` 等價物）目前查無任何原始碼痕跡（非部分實作，是
  完全空白）。
- Probe A/B/C 的 scratch 目錄執行後已清除（`%TEMP%\apm-pack-probe*`），
  未留下殘留檔案；過程中沒有對 `D:/Projects/apm-dev/apm-go`、
  `D:/Projects/apm-dev/apm`、`D:/Projects/apm-dev/evals/test1` 做任何寫入。
