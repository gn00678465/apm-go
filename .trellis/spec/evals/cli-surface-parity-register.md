# CLI 全指令面 Parity 缺口登記冊

> **Living Doc**：本檔案原產出於 `.trellis/spec/conformance/`（該目錄依 `.gitignore:44`
> 為本機專用、不入版控的 conformance authority），2026-07-12 遷移至
> `.trellis/spec/evals/` 以便入版控（`git check-ignore` 已驗證 `evals/` 不受忽略）。
> 遷移不影響 `conformance/cli-verification-checklist.md`、`conformance/openapm-v0.1.md`
> 兩份既有檔案的既有慣例，僅本檔案適用。任何 PRD 新增/修改 CLI 指令表面（新指令、新
> flag、改變既有指令行為）前，先查閱本檔對應列的既有分類；task 完成時同步更新本檔對應
> 列的狀態/證據——不要只在本次盤查任務裡讀寫一次就當作快照。更新規則與觸發時機見
> `.trellis/spec/guides/oracle-parity-gates.md`（Gate 5：登記冊 Living-Doc 更新規則）。

## 0. 檔頭

**用途**：一次性系統盤查 apm-go（13 指令）對 Python 原版 `apm`（explicit 命令枚舉 32 個，PRD 標稱「33 指令」，見 §0.3 caveat）的**全指令表面**，把「使用者逐一踩雷才發現不相符」的狀態，轉成一份分類齊全、可追溯的缺口登記冊。本文件是**盤查（audit）產出，不是修復**——所有修復動作依本文件 §5 的 triage 順序另開 task。

**日期**：2026-07-12

**來源**：本登記冊初稿由以下 6 份 research 檔案合成；2026-07-12 的 codex 對抗性抽驗另補正研究遺漏與錯誤，詳見文末「codex 抽驗記錄」。

| Research 檔 | 涵蓋範圍 |
|---|---|
| `research/group-crosscut.md` | 全域 flags（`--version`/`-v`）、`update` 殘餘 flag 面、install/uninstall/marketplace 2026-07-12 drift 快篩、exit code 慣例 |
| `research/group-deps.md` | `deps` `lock` `outdated` `prune` `view` `cache` `find` `mcp` 八個 Python-only 指令 |
| `research/group-extensions.md` | apm-go-only 的 `normalize` `validate` `completion`，以及兩邊都有的 `experimental` |
| `research/group-integrity.md` | `audit`（兩邊都有）、`doctor` `policy` `approve` `deny`（Python only） |
| `research/group-packaging.md` | `pack`（兩邊都有）、`plugin` `publish` `unpack` `search`（Python only） |
| `research/group-project.md` | `init`（兩邊都有）、`config` `targets` `list` `run` `preview`（Python only）、`compile`（COVERED-ELSEWHERE） |

**觸發背景**（`prd.md` 第 9-17 行，2026-07-12 使用者連續踩雷）：

1. `codex agents` byte-copy 假 TOML（已修，任務 `07-12-codex-agent-toml`）。
2. `pack` 同名異義：Python = plugin bundle 打包（SBOM/license 警告、plugin.json 合成、`build/` 輸出、內嵌 lockfile）；apm-go = 從 `marketplace:` block 產 `marketplace.json`——同名完全不同功能，使用者實跑輸出天差地遠。
3. 主 session 抽查另見 `audit` 同名異義：Python = 隱藏 Unicode 掃描；apm-go = lockfile 完整性重驗。

### 0.1 分類法（判定欄，逐字取自 `prd.md`）

| 類別 | 定義 | 風險 |
|---|---|---|
| **DIVERGENT-SAME-NAME** | 同名指令行為不同 | **最高**（靜默誤導） |
| **MISSING** | Python 有、apm-go 無 | 中（unknown command，可見） |
| **EXTENSION** | apm-go 有、Python 無 | 低（須為 documented extension） |
| **PARTIAL** | 同名同義但 flag/行為子集或偏差 | 中 |
| **PARITY-VERIFIED** | 已有 checklist/A-B 佐證 | — |
| **COVERED-ELSEWHERE** | 已由既有 conformance 清單涵蓋 | — |

### 0.2 方法論摘要

- 兩邊指令面完備性基準：apm-go `--help` 枚舉的 13 指令（`audit compile completion experimental help init install marketplace normalize pack uninstall update validate`，見 `group-deps.md` 前置驗證）× Python 33 指令枚舉（`group-extensions.md` §引言引用：`approve audit cache compile config deny deps doctor experimental find init install list lock marketplace mcp outdated pack plugin policy preview prune publish run runtime search self-update targets uninstall unpack update view`，實數 32，見 §0.3）。
- 同名指令一律做行為對照（scratch live probe + 原始碼），DIVERGENT 項在 §3 附兩邊實跑 transcript。
- 已涵蓋面不重掃：`install`/`uninstall`/`marketplace` 75 項清單（`cli-verification-checklist.md`）、`compile`（`compile-contract.md`，07-11 child）、`update` 的 local-deps 物化 + 零 target 閘門（`install-marketplace-contracts.md`，07-11 child）——本登記冊標 COVERED-ELSEWHERE 並引用來源，不重複列出子項。
- 有狀態指令（`publish`/`self-update`/`config` 寫入/`approve`/`deny`/`cache` 清除/`marketplace` 寫入）全程只做 help + 原始碼 + 唯讀探測，未實跑會改真實狀態的操作；全部 live probe 一律 TEMP/scratch 目錄，未污染任一方 repo。

### 0.3 Caveats（本次合成階段新增，非某一份 research 檔的既有結論）

1. **指令計數差 1**：`prd.md` 稱「Python 原版 33 指令」，但 6 份 research 檔實際枚舉並逐一分派研究的 Python 指令名稱只有 32 個（見 §0.2 清單）。合成階段未新增研究以查核第 33 個名稱是否存在（例如某個隱藏/deprecated 別名），僅如實記錄此落差，不臆測其身份。
2. **初稿遺漏 `runtime` 與 `self-update` 研究**：6 份 research 的分工沒有涵蓋這兩項。codex 抽驗已用兩邊 root `--help`、Python 子指令 `--help` 與原始碼補核；apm-go root help 均無此指令，因此兩項皆補判為 `MISSING`。依安全限制，`self-update` 僅跑 `--help`，未執行更新。
3. 全域層級的發現（`--version`/`-v/--verbose`/CLI 解析層 exit code）不對應單一 Python 指令名稱，而是對應 `apm` 頂層 group 或跨指令的框架行為；為了不遺漏這些真實缺口，§1 在指令聯集表之後另闢「全域/框架級項目」附表收錄。

---

## 1. 總覽表（兩邊指令聯集）

**主表**：一列一個頂層指令名稱，涵蓋 apm-go 13 指令 ∪ Python 32 指令的聯集（36 個不重複名稱）。多面向指令（如 `init`/`update`/`experimental`）的類別欄採「最高風險面向優先」原則標註複合類別，細節見 §2/§3/§4 對應小節。

| 指令 | apm-go | Python | 類別 | 嚴重度 | 處置建議 | 證據錨點 |
|---|---|---|---|---|---|---|
| `approve` | 無 | 有 | MISSING | **high** | 另開安全性 task，短期先加「`allowExecutables` 存在但不生效」警告 | integrity §approve/deny |
| `audit` | 有 | 有 | **DIVERGENT-SAME-NAME**（已雙邊實跑證實） | **high** | 不改行為；help/spec 明確記錄語意差異 | integrity §audit |
| `cache` | 無 | 有 | MISSING | medium | 先確認 apm-go 是否有等價本地快取層，再評估是否補 CLI 入口 | deps §6 |
| `compile` | 有 | 有 | **COVERED-ELSEWHERE** | — | 不重掃；見 `compile-contract.md`（但其 §62 對 `target:` 單複數的描述需隨 `init` §2 的 targets 修復一併更新，見 crosscut caveat） | project §7 |
| `completion` | 有 | 無 | EXTENSION | low | 記錄不做（cobra 框架標準樣板） | extensions §指令3 |
| `config` | 無 | 有 | MISSING | medium | 記錄不做；待底層功能（registries/external-scanners/protocol-fallback）落地再評估 | project §2 |
| `deny` | 無 | 有 | MISSING | **high** | 與 `approve` 合併同一安全性 task | integrity §approve/deny |
| `deps`（list/tree/clean/info/why） | 無 | 有（group） | MISSING | medium | 另開 task，優先 `deps why`/`list`/`tree`（唯讀瀏覽） | deps §1 |
| `doctor` | 無 | 有 | MISSING | low | 記錄不做，或視回饋另開低優先度 task | integrity §doctor |
| `experimental` | 有 | 有 | PARTIAL ＋ **DIVERGENT-SAME-NAME**（裸呼叫語意，見 §3.5） | medium | flags registry 子集維持現狀（有 spec 佐證）；子指令/flag 覆蓋率與裸呼叫語意另開 task | extensions §指令4 |
| `find` | 無 | 有 | MISSING | medium | 另開 task；底層資料（lockfile `deployed_files`）apm-go 應已具備，實作成本可能偏低 | deps §7 |
| `help` | 有 | 無（僅 `--help` flag） | EXTENSION（推定，未獨立研究） | low | 記錄不做；比照 `completion` 為 cobra 框架標準子指令（`group-deps.md` 前置驗證僅枚舉，未逐項分析 `help` 本身） | deps §前置驗證（enumeration only） |
| `init` | 有 | 有 | PARTIAL ＋ **DIVERGENT-SAME-NAME**（`target`/`targets` 欄位，見 §3.4） | **high** | 另開 task：manifest parser 同時支援 `targets:`（複數）與 `target:`（單數 legacy） | project §1 |
| `install` | 有 | 有 | **COVERED-ELSEWHERE**（2026-07-12 快篩確認無 drift） | — | 見 `cli-verification-checklist.md`（75 項） | crosscut §3 |
| `list` | 無 | 有 | MISSING | medium | 另開 task，與 `run` 一併規劃 | project §4 |
| `lock`（bare） | 無 | 有 | MISSING | medium | 另開 task，與 `install --frozen`/dry-run 需求合併規劃 | deps §2 |
| `lock export`（SBOM） | 無 | 有 | MISSING | **high** | 另開 task，優先度提升（供應鏈安全/合規面向） | deps §2 |
| `marketplace` | 有 | 有 | **COVERED-ELSEWHERE**（2026-07-12 快篩確認無 drift） | — | 見 `cli-verification-checklist.md`（75 項） | crosscut §3 |
| `mcp`（search/show/list） | 無 | 有 | MISSING | medium | 另開 task；可直接復用 `internal/mcpregistry.Client` 既有 search/get 邏輯 | deps §8 |
| `mcp install` | 部分（`install --mcp`） | 有（純轉發殼） | MISSING（殼）但功能已覆蓋 | low | 不需修 parity，文件化差異即可 | deps §8 |
| `normalize` | 有 | 無 | EXTENSION | low | 記錄不做；建議補一句 spec 註記說明其為 dev-only CLI 化工具 | extensions §指令1 |
| `outdated` | 無 | 有 | MISSING | **high** | 另開 task，優先度高（CI 友善唯讀健檢，目前只能用有副作用的 `update` 間接得知） | deps §3 |
| `pack` | 有 | 有 | **DIVERGENT-SAME-NAME**（已雙邊實跑證實） | **HIGH** | 三選一（修 parity／文件化範圍限定＋警告／記錄不做），見 §3.2 | packaging §1 |
| `plugin`（`init`） | 無 | 有 | MISSING | medium | 另開 task 視 plugin-author workflow 優先度決定 | packaging §2 |
| `policy` | 無 | 有 | MISSING | **high** | 不在本任務修；另開專責 task 先做「是否支援 org-policy 治理層」產品決策 | integrity §policy |
| `preview` | 無 | 有 | MISSING | low | 記錄不做，隨 `run`/`list` task 一併決議 | project §6 |
| `prune` | 無 | 有 | MISSING | **high** | 另開 task，優先度高（apm.yml 與磁碟狀態長期漂移無自動修復路徑） | deps §4 |
| `publish` | 無 | 有 | MISSING | medium | 另開 task（若要做，對齊已歸檔 `07-01-registry-consumer` 排除的 v0.2 範圍） | packaging §3 |
| `run` | 無 | 有 | MISSING（＋apm-go 自我矛盾） | **high** | 短期：拿掉/改寫 `init.go:162` 的虛假提示（立即，1 行）；長期：另開 task 評估是否實作 | project §5 |
| `runtime` | 無 | 有 | MISSING | low | 記錄不做；Python 明標 experimental，需求成立時另開 runtime-management task | codex 抽驗：`runtime --help`；`runtime.py:19-160` |
| `search` | 部分（`marketplace browse`，無 query 過濾） | 有 | MISSING | medium | 另開 task；或幫 `browse` 加 `--query`/`--limit` 達到等價效果（成本低於全新指令） | packaging §5 |
| `self-update` | 無 | 有 | MISSING | medium | 文件化手動升級路徑；若要補，另開 binary distribution/update task | codex 抽驗：`self-update --help`；`self_update.py:115-170` |
| `targets` | 無 | 有 | MISSING | **high** | 另開 task：補查詢指令＋補齊 `SignalWhitelist` 8 目標＋與 `init` 的 `target`/`targets` 雙鍵解析一併處理 | project §3 |
| `uninstall` | 有 | 有 | **COVERED-ELSEWHERE**（2026-07-12 快篩確認無 drift） | — | 見 `cli-verification-checklist.md`（75 項） | crosscut §3 |
| `unpack` | 部分（`install <bundle-path>` 可能是後繼者） | 有（Python 自身已標 deprecated） | MISSING | low | 記錄不做；若後續發現 `install` 未完整覆蓋其行為子集再另開 task | packaging §4 |
| `update` | 有 | 有 | **DIVERGENT-SAME-NAME**（consent-gate，見 §3.3）＋ MISSING（多個殘餘 flag）＋ COVERED-ELSEWHERE（local-deps 物化＋零 target 閘門部分，07-11 child） | **high** | 至少補 `--dry-run`（風險最低、獨立可做）；其餘見 §2 crosscut 逐 flag 表 | crosscut §2 |
| `validate`（頂層） | 有 | 無（頂層；`marketplace validate NAME` 是撞字但不同範疇的另一指令，= MK-10，COVERED-ELSEWHERE） | EXTENSION | low | 記錄不做；建議補記與 `marketplace validate NAME` 撞字面詞但範疇不同的澄清 | extensions §指令2 |
| `view`（本地元資料） | 無 | 有 | MISSING | medium | 另開 task，可與 `deps info` 合併規劃 | deps §5 |
| `view PKG versions` | 無 | 有 | MISSING | **high** | 另開 task，優先度應高於本地元資料分支（唯讀、獨立、探索升級目標必備） | deps §5 |

### 1.1 全域 / 框架級項目（不對應單一指令名稱，附表）

這些發現來自 `group-crosscut.md`，作用於 `apm`/`apm-go` 頂層 group 或跨越全部子指令，不屬於「單一指令名稱」的聯集列，但同樣是真實缺口，獨立列出避免遺漏：

| 項目 | 類別 | 嚴重度 | 處置建議 | 證據錨點 |
|---|---|---|---|---|
| 全域 `--version` | MISSING | medium | 修：cobra `Version` 欄位＋ldflags 版本注入，統一 `install.go:690` 字面值來源 | crosscut §1 |
| 全域 `-v/--verbose`（logging-level 語意）＋ `APM_LOG_LEVEL` 等價 env | MISSING | medium | 決策：補真全域 verbose（尤其 install/update）或記 documented deviation | crosscut §1 |
| root `-h` 有（apm-go）／無（Python） | EXTENSION | low | 記錄不做 | crosscut §1 |
| CLI 解析層 exit code（unknown flag/command）永遠 1（apm-go／cobra）vs 永遠 2（Python／click） | **DIVERGENT-SAME-NAME**（框架級，覆蓋全部 13 指令） | medium | 一次性 cobra `FlagErrorFunc`/錯誤分流修法，覆蓋全部 13 指令 | crosscut §4.2 |
| 語意層（業務邏輯）exit code 個案差異 | COVERED-ELSEWHERE | — | 見既有 checklist/spec，不重複記錄 | crosscut §4.2 |
| `[x]/[!]/[+]/[i]` 訊息前綴慣例 | PARITY-VERIFIED | — | 無動作 | crosscut §4.1 |

### 1.2 類別統計

| 類別 | 數量 | 涵蓋範圍 |
|---|---|---|
| DIVERGENT-SAME-NAME | 6 | `audit`、`pack`、`init`、`update`、`experimental`（皆含至少一項已證實的同名分歧子項）＋全域 CLI 解析層 exit code |
| MISSING | 25 | `approve`/`cache`/`config`/`deny`/`deps`/`doctor`/`find`/`list`/`lock`(+export)/`mcp`/`outdated`/`plugin`/`policy`/`preview`/`prune`/`publish`/`run`/`runtime`/`search`/`self-update`/`targets`/`unpack`/`view`（23 指令，`lock`/`view` 各自的高嚴重度子項已併入母列計數）＋全域 `--version`／`-v/--verbose`（2 項） |
| EXTENSION | 4 | `completion`、`help`、`normalize`、`validate` |
| COVERED-ELSEWHERE | 4 | `compile`、`install`、`marketplace`、`uninstall` |

（此統計以 §1 主表 36 列 + §1.1 附表計入 DIVERGENT-SAME-NAME/MISSING 各一項框架級/全域發現後之總數為準，供 triage 全局參考；細節分歧與嚴重度以各指令列的完整敘述為準，不可只看本表數字下結論。）

---

## 2. `update` 逐 flag 對照（crosscut §2 完整複製，因其細度超出單列摘要）

`update` 是本登記冊中兼具 DIVERGENT-SAME-NAME（consent-gate，§3.3）與大量 MISSING/PARTIAL 殘餘 flag 的指令，crosscut 組做了完整的 flag-by-flag 對照，此處完整保留供 triage 直接引用：

| Python flag | apm-go 現況 | 類別 | 嚴重度 | 處置建議 |
|---|---|---|---|---|
| `-y/--yes` | 不存在（但語意上永遠等同已 `--yes`） | MISSING | 併入 D-1（§3.3） | 併入 D-1 決策 |
| `--dry-run` | 不存在 | MISSING | **high**（隨 D-1） | 優先修，風險最低、獨立可做 |
| `-v/--verbose` | 不存在 | MISSING | low-medium | 併入全域 verbose 決策（§1.1） |
| `-g/--global` | 不存在 | MISSING | low | 記錄不做，COVERED-ELSEWHERE（uninstall un-090/091 定案 A 精神） |
| `--force` | 不存在 | MISSING | low | COVERED-ELSEWHERE：checklist item 5 |
| `--parallel-downloads` | 不存在 | MISSING | low | COVERED-ELSEWHERE：checklist item 11 |
| `-t/--target` | 不存在，原始碼自陳 gap（`update.go:197-199` 註解：「apm-go's update has no --target flag (Python parity gap, out of this task's scope)」） | MISSING | medium | 建議修，改動面小（`deploy.ResolveTargets` 已支援） |
| `PACKAGES...`(多個) vs 單一 `[package]` | `MaximumNArgs(1)` | PARTIAL | low-medium | 視需求放寬 `MaximumNArgs` |

證據錨點：`crosscut §2` 全節（含 D-1 的完整根因分析）。

---

## 3. DIVERGENT-SAME-NAME 專節

本節收錄全部已被兩邊實跑或原始碼交叉證實的「同名指令行為不同」項目，每項附兩邊行為摘要、transcript、根因、建議。

### 3.1 `audit` — bare 呼叫兩邊邏輯完全不重疊

**兩邊行為**：
- apm-go `audit`（bare，唯一 flag 是 `-h`）：讀 `apm.lock.yaml` → `lockfile.ParseLockfile` → `lockfile.VerifyDeployedState(lock, ".")`，對每個 dependency 的 `DeployedHashes`/`LocalDeployedHashes` 重算 SHA-256 並比對；有違規 exit 1，全過 exit 0。
- Python `apm audit`（bare，21 個 flag）：只做隱藏 Unicode 掃描（`ContentScanner`）＋預設開啟的 drift 偵測（install-replay diff），**完全不含 SHA-256 內容比對**。Python 真正等價於 apm-go `audit` 的邏輯藏在 `apm audit --ci` 第 7 項檢查 `content-integrity`（`ci_checks.py:280-375`），且該檢查前面疊了 6 層 fail-fast 檢查，預設看不到。

**Transcript（同一份被竄改的檔案，TEMP scratch）**：
```
$ apm-go.exe audit
content-integrity violation: .claude/skills/file.txt expected sha256:2cf24dba..., observed sha256:06c9c46c...
Error: audit failed: 1 content-integrity violation(s) (first: .claude/skills/file.txt)
EXIT=1

$ uv run apm audit
[>] Scanning all installed packages...
[>] Replaying install (cache-only)...
[!] drift skipped: install cache not populated (run 'apm install' first or pass --no-drift)
[*] 1 file(s) scanned -- no issues found
EXIT=0
```
竄改被 Python bare `audit` 完全放行；同一份竄改改用 `apm audit --ci --no-policy --no-fail-fast --no-drift` 才會在 8 項檢查裡的 `content-integrity` 抓到，且雜湊值與 apm-go 輸出的雜湊值完全一致（證實兩邊在做同一件底層事情，只是曝露方式不同）。

**根因**：apm-go 把「SHA 完整性重驗」抽成 `audit` bare 的唯一職責；Python 把同一段邏輯埋在 `--ci` 模式第 7 項檢查裡，bare 模式的職責是完全不同的隱藏 Unicode 掃描。兩邊命名巧合完全同名，但語意零重疊。

**建議**：不建議修 parity 成一致（兩邊使用者都已依賴各自現有行為，屬破壞性變更）。應在 `apm-go audit --help`/README/spec 明確記錄「apm-go audit ≠ Python apm audit（bare）；等價於 Python `apm audit --ci --no-fail-fast` 的 `content-integrity` 檢查裡的 hash 部分，不含 Unicode 掃描」。

證據錨點：`integrity §audit` 全節。

---

### 3.2 `pack` — 同名完全不同功能（本任務觸發背景 #2 的教科書案例）

**兩邊行為**：
- Python `apm pack` 由 `BuildOrchestrator` 依 `apm.yml` 內容動態路由到最多三個 producer：`BundleProducer`（`dependencies:` 非空 → plugin bundle 打包到 `build/<name>-<version>/`，含 hooks/MCP 合併、hidden-char 掃描、內嵌 lockfile SHA-256）、`MarketplaceProducer`（`marketplace:` 非空 → `.claude-plugin/marketplace.json`）、`PluginManifestProducer`（`target:`/`targets:` 含 claude/copilot → 專案根目錄 `plugin.json`）。三者可同時觸發。
- apm-go `pack.go` 文件註解明文承認範圍窄化為「只做 marketplace.json 生成」（`pack.go:24-28`），只實作 `MarketplaceProducer` 一個 producer；`hasMarketplaceConfig` 完全不檢查 `dependencies:`/`target:`；無 `marketplace:` 區塊時印 `[i] No 'marketplace:' block found ... nothing to do.` exit 0，**即使 `apm.yml` 有 `dependencies:` 或 `target: claude` 也完全靜默跳過，不產生任何輸出、不印警告**。

**Transcript（三種 Probe，TEMP scratch）**：

Probe A（只有 `dependencies:`）——Python 產出 `build/probe-pkg-0.1.0/{plugin.json, skills/hello/SKILL.md}`；apm-go：
```
[i] No 'marketplace:' block found (neither apm.yml's marketplace: block nor a legacy marketplace.yml exist); nothing to do.
```
exit 0，完全沒有寫任何檔案，`build/` 目錄根本不存在。

Probe C（只有 `target: claude`）——Python 產出專案根目錄 `.claude-plugin/plugin.json`；apm-go：同樣印「nothing to do」，exit 0，什麼都不寫。

Probe B（`dependencies:`＋`marketplace:` 同時存在）——Python 同時產出 bundle 與 `marketplace.json`；apm-go 只產出 `marketplace.json`（且內容與 Python byte-for-byte 相同，這一半是 PARITY-VERIFIED），`dependencies:` 被靜默忽略，無任何警告。

**根因**：apm-go 的 `pack` 是已歸檔子任務 `07-03-marketplace-pack` 的產出，該子任務設計文件本身刻意把範圍收斂成「只做 marketplace.json 產生器」，明確排除 plugin bundle 打包——是已知、有意識的範圍決策，不是遺漏。但這個範圍決策從未曝露給終端使用者：指令名稱、指令存在、exit code 0 全部正常，只有輸出內容是空的，比一般 MISSING 指令更危險。

**建議**（三選一，留給 triage）：
1. 修 parity：另開 task 補 BundleProducer/PluginManifestProducer，工作量大。
2. **documented extension（成本最低、風險降最多）**：`--help`/README 明確寫「本指令只做 marketplace.json 生成」，並在 `dependencies:`/`target:` 存在但 `marketplace:` 不存在時**印一則警告**而非完全靜默 `nothing to do`。
3. 記錄不做：若確認 apm-go 生態不需要 plugin bundle 打包，在 register 明確記錄決策依據。

證據錨點：`packaging §1` 全節（含 §1.1-1.5）。

---

### 3.3 `update` — consent-gate 整體缺失（D-1）

**兩邊行為**：
- Python `update.py:469-527`：無變更 → 直接 return，不寫 lockfile、不問；有變更＋`--dry-run` → 印計畫、不套用；有變更＋`--yes` → 直接套用；有變更、無 `--yes`、非互動 shell → `_rich_error("Cannot prompt for confirmation in non-interactive shell. Re-run with --yes to apply, or --dry-run to preview.")`，exit 1，**拒絕套用**；有變更、互動 shell → `click.confirm("Apply these changes?", default=False)`，答 N 則不套用。
- apm-go `runUpdate`（`update.go:47-208`）**全程沒有任何 prompt/confirm/stdin-tty 檢查**——不論有無變更、是否互動 shell，一律直接算出 plan 就往下 `deployAndFinalize`，寫新 lockfile、重新部署檔案。`printUpdateSummary` 只是印「哪些變了」，不是「要不要套用」的關卡。

**影響**：互動終端機下，Python 使用者跑 `apm update` 會先看到計畫再選擇是否套用；同樣情境 `apm-go update` 不問就套用（改 `apm.lock.yaml`、重新部署檔案）。對自動化場景更方便（等同永遠 `--yes`），但對互動使用者是「同名指令、不同安全語意」的落差，且沒有 `--dry-run` 逃生門。

**根因**：`-y`/`--dry-run`/非互動拒絕這三個 flag 在 apm-go 側的消失，不是三個獨立小缺口，而是同一根因（整條 consent-gate 邏輯不存在）的三個症狀。既有 `install-marketplace-contracts.md` D3 曾提到「apm-go has no plan/consent gate」，但那段落點是解釋零 target exit 行為的成因，不是把「整體無 consent gate」列為獨立項目——本組首次把它拉出來當一級發現。

**建議**：至少補 `--dry-run`（獨立可做、風險最低、立即降低盲改風險）；`-y`/互動 confirm 是否要讓 apm-go 對互動終端機也擋一下，屬 UX 決策另評。

證據錨點：`crosscut §2` D-1 小節。

---

### 3.4 `init` — `target:`（單數）vs `targets:`（複數）canonical schema 靜默不相容

**兩邊行為**：
- Python 自 #1154 起以 **`targets:`（複數）為 v2 canonical schema**（`apm_yml.py:1-9`），`target:`（單數）僅作相容 sugar，且明文禁止兩者並存（`ConflictingTargetsError`）。
- apm-go `ParseManifest`（`manifest.go:63-191`）**只認 `case "target":`（97 行），完全沒有 `case "targets":` 分支**——`targets:` 落入 157 行的 `default: // Unknown keys ... preserved by Node`，**靜默忽略、零警告**。

**Transcript**：
```yaml
# apm.yml
name: plural-test
version: 0.1.0
targets:
  - claude
  - copilot
```
```
$ apm-go validate apm.yml
EXIT=0   # 完全沒有任何警告，targets: 形同不存在
```

**影響鏈**：apm-go 自己的 `init.go:345-367 readExistingTargets()` 重新 init 既有專案時也只讀 `doc["target"]`——若專案是被 Python（#1154 後版本）初始化、只有 `targets:`，apm-go 重新 init 時會誤判「沒有 pin」；`compile`/`install`/`update` 同樣經由 `ParseManifest` 讀 target，理論上都會對只寫 `targets:` 的專案完全忽略 pin，退回 auto-detect。`compile-contract.md:62` 自身描述仍以單數 `target:` 為前提，尚未反映 Python v2 canonical schema。

**附帶發現（同根因、同一 task 範圍）**：
- **CSV sugar under 單數 `target:`**（DIVERGENT，medium）：Python `parse_targets_field` 支援 `target: "a,b"` → `['a','b']`；apm-go 的 `parseTargetField`（`manifest.go:193-213`）不做逗號拆分，`target: "claude,codex"` 在 apm-go 直接讓整個 manifest 解析失敗（`Error: apm.yml: unknown target "claude,codex"`），一個 Python 端合法的 apm.yml 在 apm-go 端會讓 `validate`/`install`/`compile` 全部炸掉。
- **非互動 stdin 無 `--yes`**（DIVERGENT，medium，**apm-go 較穩健**）：`printf '' | apm-go init` 用預設值成功建檔 exit 0；`printf '' | uv run apm init` EOF 崩潰 exit 1（Python 自身一致性缺口，`_interactive_project_setup` 不查 TTY）。此項判定**記錄不做**，不需要 apm-go 回頭相容 Python 的 crash 行為。

**建議**：另開 task，manifest parser 同時支援 `targets:`（優先）與 `target:`（legacy sugar，含 CSV 拆分），並鏡射 Python 的互斥錯誤語意；與 `targets` 指令（§4.3）、`compile-contract.md` 的欄位描述更新一併處理（同一段 manifest 層改動）。

證據錨點：`project §1a`、`§1b`、`§1c`。

---

### 3.5 `experimental` — 裸呼叫語意不同（help vs list）

**兩邊行為**：
- Python：`ctx.invoked_subcommand is None` 時明確 `ctx.invoke(list_flags)`（`experimental.py:156-158`）——裸跑 = 印出目前的 flag 列表。
- apm-go：`experimentalCmd()` group 沒有設 `RunE`，cobra 對「有子指令、無 Run」的 group 預設印 help、exit 0——裸跑 = 印 help，**不會**印出 flag 狀態。

**Transcript**：
```
$ ./bin/apm-go.exe experimental
Manage experimental feature flags
Usage:
  apm-go experimental [command]
Available Commands:
  disable     Disable an experimental feature
  enable      Enable an experimental feature
  list        List experimental features and their status
...
exit=0

$ USERPROFILE=<scratch> apm experimental
                             Experimental Features
┌───────────────────────┬────────────┬────────────────────────────────────────┐
│ Flag                  │ Status     │ Description                            │
├───────────────────────┼────────────┼────────────────────────────────────────┤
│ verbose-version       │ disabled   │ ...
...
[i] Tip: apm experimental enable <name>
exit=0
```

**根因**：cobra 對無 `RunE` 的子指令 group 的預設行為（印 help）與 Python click `invoke_without_command=True` 手動指定的行為（轉發到 `list`）不同，屬於框架預設值差異，並非 apm-go 刻意設計。

**建議**：另開 task 評估群組裸呼叫預設印 `list`，對齊 Python `invoke_without_command` 行為；可與 §4 補齊 `experimental reset` 一併規劃。嚴重度 medium：兩邊都 exit 0，不算報錯，影響侷限在互動便利性，非資料損毀或危險誤導。

證據錨點：`extensions §4c`。

---

### 3.6 全域：CLI 解析層 exit code（1 vs 2，框架級）

**兩邊行為**：apm-go（cobra）對「所有」CLI 語法錯誤（unknown flag、unknown command、bad arg count）一律回 exit 1（框架預設，`main.go:32-34`/`exitcode.go:27-38`，非逐指令設定）；Python（click）對應的 `UsageError` 自動觸發 exit 2（框架預設，涵蓋 click 自身語法錯誤與專案自訂繼承 `UsageError` 的語意驗證錯誤）。

**Transcript**：
```
$ bin/apm-go.exe bogus-command   → EXIT=1   (cobra "unknown command")
$ apm bogus-command               → EXIT=2   (click "No such command")

$ bin/apm-go.exe install --bogus-flag   → EXIT=1
$ apm install --bogus-flag              → EXIT=2
$ bin/apm-go.exe compile --bogus-flag   → EXIT=1
$ apm compile --bogus-flag              → EXIT=2
```
兩個不同子指令印證這是框架級差異，不是單一指令的行為分歧。

**影響**：對人類使用者是 cosmetic（非 0 都代表失敗）；對**腳本化 CI 判斷「打錯參數」還是「業務邏輯失敗」**的呼叫方是實質差異——Python 腳本可用 `if exit==2: print("usage error")` 分流，apm-go 目前做不到。

**建議**：一次性 cobra `FlagErrorFunc`/`SilenceErrors` 修法，讓 CLI 解析層錯誤統一被 `exitCodeOf` 辨識為 2，可覆蓋全部 13 指令，不需逐指令補丁。語意層（業務邏輯）exit code 個案差異維持 COVERED-ELSEWHERE，不受此規則約束。

證據錨點：`crosscut §4.2`。

---

## 4. 高嚴重度 MISSING 專節

本節收錄全部嚴重度判定為 **high** 的 MISSING 項目，依風險性質分三類：安全/治理機制缺席（4.1-4.3）、CI/合規向唯讀健檢缺席（4.4-4.7）、自我矛盾/可觀測性缺席（4.8）。

### 4.1 `approve` / `deny` — executable-primitives 批准閘門整體缺席

Python 側（`security/executables.py:1-37`，檔案開頭明講「mirrors npm v12's `npm approve-scripts`/`npm deny-scripts`」）：APM 套件可宣告三種 executable primitive——hooks、MCP servers、bin/ executables。當專案 `apm.yml` 宣告 `allowExecutables` block 時啟用 **deny-by-default**：未經批准的 primitive 不會被部署（opt-in 機制，未宣告則維持向後相容全部部署）。`ENFORCED_EXEC_TYPES = (hooks, bin)`——MCP 本身 Python 也承認尚未被 enforce。`approve <pkgs>`（`--pending`/`--all`）寫入批准；`deny <pkgs>`（必填 packages）撤銷批准。

apm-go 側：全庫搜尋 `allowExecutables`/`ExecutableDeclaration`/`executable`（大小寫不敏感）於 `internal/`：**零命中**。`internal/deploy/primitive.go:32-197` 的 `CollectLocalPrimitives`/`CollectDependencyPrimitives` 收集並部署 hook primitive，**全程沒有任何批准/閘門檢查**。無 `approve`/`deny` 指令。

**風險**：這是任意程式碼執行面（`.claude/rules/common/security.md` 明確列為 security review trigger 等級的攻擊面）。曾在 Python 側啟用 `allowExecutables` block 的專案改用 apm-go 後，`apm.yml` 裡的 block 還在（大概率被當未知欄位忽略，不報錯），但 apm-go 完全不讀它、不執行它——**所有 hook/bin 无條件部署，閘門形同虛設，且無任何警告告知使用者**，是 PRD 定義「DIVERGENT-SAME-NAME 最高風險（靜默誤導）」的精神，即使形式上是 MISSING。連 `approve --pending` 這種可視性工具都沒有，apm-go 使用者甚至無法列出「哪些安裝套件帶有 executable primitive」。

**建議**：不建議在本任務內修（涉及安全機制設計＋schema 變更＋install pipeline 插入閘門檢查點）。強烈建議另開安全性專責 task，優先順序建議高於 `policy`（涉及終端使用者機器任意程式碼執行防線）。短期成本最低、風險降最多的手段：若 `apm.yml` 含 `allowExecutables` block 但 apm-go 不執行它，印一則警告（「此區塊在 apm-go 尚不生效」）。

證據錨點：`integrity §approve/deny`。

---

### 4.2 `policy` — org-policy 治理層整體不存在

Python 側（`commands/policy.py:268-371` `status` 子指令 + `policy/discovery.py`/`schema.py`/`policy_checks.py`）：對 org-policy（`apm-policy.yml`）discovery 結果做唯讀診斷快照（outcome/cache age/`extends` chain/各段規則計數）。這整組治理層同時被 `apm install`（`--policy`/`--no-policy`/`--no-cache`）、`apm audit --ci --policy`/`apm audit --policy`（bare 也會 auto-discover）共用。

apm-go 側：全庫搜尋 `policy` 命中的都是無關的重試/redirect policy，或單一硬編碼規則 `internal/manifest/insecure_dep.go:5-24`（只做「拒絕非 TLS `http://` git 依賴」，是 Python `insecure_policy.py` 一條規則的 1:1 移植，不是可設定的治理引擎，無 discovery/schema/allow-deny-list/cache/chain 概念）。`install --help`/`audit --help` 實跑確認**沒有** `--policy`/`--no-policy`/`--no-cache` flag。

**風險**：若某組織已用 Python apm 部署 `apm-policy.yml` 做集中治理（封鎖某些依賴來源、限制 MCP transport、限制 compile target、要求 manifest 必填欄位），開發者改用 apm-go 後這些規則**靜默完全不生效**，沒有任何錯誤或警告，因為 apm-go 根本不知道 `apm-policy.yml` 這個檔案的存在。組織以為治理適用於所有工具鏈，實際上 apm-go 是治理真空。

**建議**：不在本任務內修（範圍遠超一個指令）。另開專責 task 先做「apm-go 是否需要支援 org-policy 治理層」產品決策（完整移植 / 至少 discovery+唯讀診斷 / 明確記錄不支援並警示）。若決定不支援，至少應在 README/CLI help 明確警示。

證據錨點：`integrity §policy`。

---

### 4.3 `targets` — v2 target resolution（#1154）唯一使用者可視化入口缺席

Python `apm targets --help`（`targets.py:23-46`）：`--json`/`--all`，把 v2 resolution algorithm（`target_detection.py:659-852`，`flag > yaml_targets > auto-detect signals`，8 個 canonical 目標的 `SIGNAL_WHITELIST`）攤開給使用者看，讓使用者在 `compile`/`install` 因 ambiguous-harness 卡住時能查「apm 到底偵測到什麼」。

apm-go：`apm-go targets --help` → `unknown command`。對應的 `internal/deploy/adapter.go:98 ResolveTargets` 沒有 `AmbiguousHarnessError`/`NoHarnessError` 概念，是簡單的 flag→manifest→auto-detect 直接 fallback。`manifest.SignalWhitelist`（`internal/manifest/detect.go:22-28`）只有 **5 條訊號**（claude/copilot/codex/opencode），完全沒有 cursor/gemini/windsurf/kiro 的偵測訊號，Python 端至少有 8 個。

**Caveat（降低但不改變判定）**：Python 自己的 `apm targets` 指令本身有 bug——`commands/targets.py:70` 呼叫 `resolve_targets(project_root)` 沒有傳 `yaml_targets` 參數，等於完全略過 `apm.yml` 的 pin，只看檔案系統訊號，與其自身 docstring「Show resolved targets for the current project」不符。這代表即使 apm-go 補上等效指令，若逐字模仿 Python 現況，一樣不會反映 `apm.yml` pin。

**風險**：apm-go 使用者完全沒有辦法診斷「我的 target 到底是怎麼被決定的」，且與 §3.4 的 `target`/`targets` 欄位解析問題、auto-detect 訊號表狹窄問題三者疊加。

**建議**：另開 task，範圍應含（a）補 `apm-go targets` 唯讀查詢指令、（b）補齊 `SignalWhitelist` 到 8 個目標、（c）與 §3.4 的 `target`/`targets` 雙鍵解析一併處理（同一段 manifest 層改動）。是否修正 Python 自身「忽略 apm.yml pin」的 bug 由產品決定是否要「做得比 oracle 好」。

證據錨點：`project §3`。

---

### 4.4 `outdated` — 無唯讀「先看後動」健檢路徑

Python `outdated.py:391-426`：對每個 locked 依賴（registry/marketplace/SHA pin/tag pin/branch pin 五種來源）比對上游是否有更新，純唯讀，不動 lockfile。apm-go：完全無此功能，`update` 指令會「重新解析並直接覆寫 lockfile」，沒有「只檢查、不動 lockfile」的路徑——想知道「有沒有東西過期」必須先跑有副作用的 `update` 才能看到 diff。

**風險**：對 CI 攔截（例如 nightly outdated-report）場景是硬缺口，缺少「先看後動」的安全檢查步驟。

**建議**：另開 task，優先度偏高，邏輯可大量複用 `update` 既有的 remote-ref 比對程式碼。

證據錨點：`deps §3`。

---

### 4.5 `prune` — apm.yml 與磁碟狀態長期漂移、無自動修復路徑

Python `prune.py:24-169`：讀 `apm.yml` 建立「預期安裝路徑」，掃描 `apm_modules/` 實際安裝內容，差集即孤兒套件；不只刪 `apm_modules/` 底下目錄，還清除該套件在 `deployed_files`（lockfile）指向的、已部署到 target 的檔案，並從 lockfile 移除該套件 entry。Live probe 證實：`apm prune` 正確刪除孤兒套件目錄與 21 個已部署 skill 檔案連同空目錄。

apm-go：無 `prune`。`uninstall` 需要**明確指名套件**才能移除，沒有「自動比對 apm.yml 找出孤兒」的功能。若使用者手動編輯 apm.yml 移除依賴後忘記手動 uninstall，孤兒套件與其部署檔案會**永久殘留**。

**風險**：不只是少一個便利指令，是資料一致性缺口——apm.yml 與實際磁碟狀態會隨時間漂移且無自動修復路徑，長期累積殭屍套件與殭屍部署檔案，可能造成 `audit` 誤判。

**建議**：另開 task，優先度高；實作可重用 apm-go 既有的 `uninstall` 刪除邏輯，只差「自動找出孤兒清單」這一層。

證據錨點：`deps §4`。

---

### 4.6 `view PKG versions` — 探索升級目標的必備工具缺席

Python `view.py:432-491`：`apm view PACKAGE versions` 純遠端查詢（不需套件已安裝），用 `GitHubPackageDownloader.list_remote_refs` 列出所有可用 tag/branch，是「決定要 pin 到哪個版本」的前置探索工具，與 `outdated` 互補。

apm-go：完全無此功能，使用者無法查詢「這個套件有哪些可用的 tag/branch」，只能手動開瀏覽器查 GitHub。（`view PKG`——本地元資料分支——嚴重度僅 medium，可被 `deps info`\[同樣 MISSING\]或直接讀 lockfile 部分取代，不列入本節。）

**建議**：另開 task；`view PKG versions` 优先度應優先於本地元資料分支，因為它是唯讀、不依賴已安裝狀態、直接對接 GitHub API 的相對獨立小工具。

證據錨點：`deps §5`。

---

### 4.7 `lock export`（SBOM：CycloneDX/SPDX） — 供應鏈合規功能完全空白

Python `lock.py:244-341`：純讀 lockfile 產 CycloneDX/SPDX SBOM，不重新 resolve/hash，不連網。Live probe 驗證兩種格式皆可正常輸出。

apm-go：完全沒有等價功能（無 CycloneDX/SPDX 匯出路徑）。

**風險**：SBOM 匯出屬合規/供應鏈安全常見需求，完全空白且無替代路徑——不是「誤導」而是「靜默無此功能」，使用者可能誤以為 apm-go 沒有 SBOM 能力而漏做合規檢查。

**建議**：另開 task 評估，屬安全/合規面向，優先度可能高於單純的 CLI 便利性缺口。

證據錨點：`deps §2`。

---

### 4.8 `run` — apm-go 自我矛盾（`init` 教使用者打一個不存在的指令）

Python `run.py:20-92`：完整的 npm-script-like 子系統（`.prompt.md` 自動探索/編譯、virtual package 自動安裝、runtime 偵測）。apm-go：無 `run` 指令。

**apm-go 自我矛盾**：`cmd/apm/init.go:162` 在「Next steps」裡印 `apm-go run <script>`，但這個指令**根本不存在**——每一個跑過 `apm-go init` 的人，只要照著提示打 `apm-go run xxx`，第一件事就是撞到 `Error: unknown command "run"`。這不是跨工具 parity 落差，是 apm-go **自己對自己使用者說謊**。

**建議**：（a）短期：把 `init.go:162` 那行提示拿掉或改成不承諾 `run` 存在的措辭（1 行改動，立即消除 UX 破洞，建議立刻做，不需等完整 `run` 實作）；（b）長期：另開 task 評估是否要實作 `run`/`list`（連同 `scripts:` 死欄位一起復活——`manifest.go:113-114` 已解析 `scripts:` 但全庫無任何讀取消費者）。

證據錨點：`project §5`。

---

## 5. Triage 建議順序（修復 Roadmap 草案）

排序原則：P0 = 成本低（≤ 數行或純文件）、能立即消除「靜默誤導」風險的動作；P1 = 需要新 task 但範圍明確、風險/效益比高的中期項目；P2 = 範圍較大或依賴產品決策的長期項目；「記錄不做」= 已有充分理由判定不需修復。

### P0（立即可做，成本低、風險降幅大）

| # | 項目 | 動作 | 來源 |
|---|---|---|---|
| 1 | `init.go:162` 虛假 `run` 提示 | 刪除或改寫該行，不承諾不存在的指令（1 行改動） | project §5 |
| 2 | `pack` 靜默無輸出 | 在 `dependencies:`/`target:` 存在但 `marketplace:` 不存在時印警告，而非完全靜默 `nothing to do`；`--help`/README 註明範圍限定 | packaging §1.5 |
| 3 | `audit` 語意落差 | `--help`/README/spec 明確記錄「apm-go audit ≠ Python apm audit（bare）」 | integrity §audit |
| 4 | `approve`/`deny` 靜默失效 | 若 `apm.yml` 含 `allowExecutables` block，印警告「此區塊在 apm-go 尚不生效」 | integrity §approve/deny |
| 5 | `update` 無預覽 | 補 `--dry-run`（風險最低、獨立可做） | crosscut §2 D-1 |
| 6 | `normalize`/`validate` 文件缺口 | 在 `.trellis/spec/backend/` 補記兩者為 dev-only CLI 化工具，並澄清 `validate` 與 `marketplace validate NAME`（MK-10）撞字面詞但範疇不同 | extensions §指令1/2 |

### P1（中期，範圍明確的獨立 task）

| # | 項目 | 動作 | 來源 |
|---|---|---|---|
| 7 | `init`/`compile`/`targets` manifest 欄位解析 | manifest parser 同時支援 `targets:`（v2 canonical）與 `target:`（legacy，含 CSV 拆分），鏡射 Python 互斥錯誤語意 | project §1a/§1b |
| 8 | `targets` 指令缺席 | 補 `apm-go targets` 唯讀查詢 + 補齊 `SignalWhitelist` 至 8 目標（與 #7 同一 manifest 層改動一併處理） | project §3 |
| 9 | CLI 解析層 exit code | 一次性 cobra `FlagErrorFunc`，讓 usage error 統一回 2，覆蓋全部 13 指令 | crosscut §4.2 |
| 10 | `update -t/--target` | 補 flag，`deploy.ResolveTargets` 已支援，改動面小 | crosscut §2 |
| 11 | `update` consent-gate 剩餘部分 | `-y`/互動 confirm 是否要讓 apm-go 對互動終端機也擋一下（UX 決策） | crosscut §2 D-1 |
| 12 | `outdated` | 補唯讀健檢指令，複用 `update` 既有 remote-ref 比對邏輯 | deps §3 |
| 13 | `prune` | 補孤兒偵測+清理指令，複用 `uninstall` 刪除邏輯 | deps §4 |
| 14 | `view PKG versions` | 補遠端 tag/branch 查詢指令 | deps §5 |
| 15 | `lock export`（SBOM） | 補 CycloneDX/SPDX 匯出，供應鏈合規面向 | deps §2 |
| 16 | `policy` | 專責 task 先做「是否支援 org-policy 治理層」產品決策 | integrity §policy |
| 17 | `approve`/`deny` | 專責安全性 task 評估是否移植 `allowExecutables` deny-by-default enforcement | integrity §approve/deny |
| 18 | 全域 `--version` | cobra `Version` 欄位 + ldflags 注入，統一 `install.go:690` 字面值來源 | crosscut §1 |
| 19 | `experimental` 覆蓋率與裸呼叫 | 補 `reset` 子指令、`list`/`enable`/`disable` flag 對齊、群組裸呼叫預設印 `list` | extensions §4b/§4c |
### P2（長期，範圍大或需先決策）

| # | 項目 | 動作 | 來源 |
|---|---|---|---|
| 20 | `runtime`/`self-update` 缺席 | 先文件化不支援；需求成立時分成 runtime management 與 binary distribution/update 兩個 task | codex 抽驗記錄 |
| 21 | `deps`（list/tree/why/clean/info） | 唯讀瀏覽類優先（`why`/`list`/`tree`） | deps §1 |
| 22 | `find` | 反查已部署檔案來源，底層資料 apm-go 應已具備 | deps §7 |
| 23 | `mcp search`/`show`/`list` | 復用 `internal/mcpregistry.Client` 既有邏輯 | deps §8 |
| 24 | `cache info`/`clean`/`prune` | 先確認底層是否有等價快取層，再評估補 CLI | deps §6 |
| 25 | `config` | 待底層功能（registries/external-scanners/protocol-fallback）落地再評估 | project §2 |
| 26 | `list`/`run`/`preview` script runner | 是否做 npm-like script runner 是產品範圍決策 | project §4/§5/§6 |
| 27 | `plugin init` | plugin-author workflow 入口，本質是 `init` 變體 + plugin.json 樣板 | packaging §2 |
| 28 | `publish` | 對齊已歸檔 `07-01-registry-consumer` 排除的 v0.2 範圍 | packaging §3 |
| 29 | `search` | 補頂層別名或幫 `marketplace browse` 加 `--query`/`--limit` | packaging §5 |
| 30 | 全域 `-v/--verbose` + `APM_LOG_LEVEL` | 決策：補真全域 verbose 或記 documented deviation | crosscut §1 |
| 31 | `update -g/--global`/`--force`/`--parallel-downloads` | 多數 COVERED-ELSEWHERE，隨對應既有缺口一併處理 | crosscut §2 |
| 32 | `update` 多套件 `PACKAGES...` | 放寬 `MaximumNArgs(1)` | crosscut §2 |

### 記錄不做（已有充分理由，不排入修復排期）

| 項目 | 理由 | 來源 |
|---|---|---|
| `doctor` | 純診斷/資訊性指令，不影響任何操作正確性或安全性 | integrity §doctor |
| `unpack` | Python 上游自己在淘汰；`install <bundle-path>` 可能已是功能對等後繼者 | packaging §4 |
| `deps update` | Python 側已 deprecated，功能已被 `apm update` 取代（07-11 child COVERED-ELSEWHERE） | deps §1 |
| `mcp install`（殼） | 功能已被 `install --mcp` 覆蓋，純文件化差異 | deps §8 |
| root `-h` / `completion` / `help` | cobra 框架標準樣板功能，非手寫業務邏輯 | crosscut §1、extensions §指令3 |
| `init` 非互動 stdin 崩潰差異 | apm-go 行為已優於 Python oracle，不需回頭相容其 crash | project §1c |
| `init` `/dev/null` 誤判 TTY 邊界情況 | 結果仍正確，只是多印雜訊，低嚴重度 | project §1c 附註 |
| `init` `--plugin`/`--marketplace`/`--verbose` flag 缺失 | Python 端已軟性淘汰；`marketplace init` 有替代路徑 | project §1 |
| `experimental` flags registry 子集（1/9） | 已有 `.trellis/spec/backend/experimental-flags.md` 佐證是刻意設計 | extensions §4a |
| cobra 未知子指令靜默 fallback | 框架級行為，不建議只為單一群組客製修正；若要修應是全 CLI 一致的獨立 task | extensions §4d |
| install/uninstall/marketplace 2026-07-12 快篩 | 確認無 drift，`cli-verification-checklist.md` 75 項仍然有效 | crosscut §3 |
| 語意層 exit code 個案差異 | 已由既有 checklist/spec 逐項記錄 | crosscut §4.2 |
| `[x]/[!]/[+]/[i]` 訊息前綴慣例 | 已高度一致，PARITY-VERIFIED | crosscut §4.1 |
| `init` author/description 自動偵測 | 兩邊邏輯與輸出一致，PARITY-VERIFIED | project §1d |

---

## 附：驗收自查（對照 `prd.md` Acceptance Criteria）

- [x] 登記冊涵蓋兩邊 root `--help` 的全部指令（Python 32 個 + apm-go 13 個，聯集 36 項），每列類別非空；codex 抽驗補判原研究漏掉的 `runtime`/`self-update` 為 MISSING
- [x] 每個同名指令有行為對照結論與證據；6 個 DIVERGENT-SAME-NAME 項（含 1 個框架級）均附兩邊 transcript（見 §3）
- [x] 全部 MISSING/EXTENSION/PARTIAL 有嚴重度與處置建議（見 §1 主表 + §1.1 附表）
- [x] codex 已完成一輪對抗性抽驗（見下節）
- [x] 登記冊落檔入版控路徑 `.trellis/spec/evals/cli-surface-parity-register.md`（2026-07-12
      由 `.trellis/spec/conformance/`（gitignored）遷移至此，`git check-ignore` 驗證
      不受忽略；本次任務未執行 git commit，依指示保留給使用者）

---

## codex 抽驗記錄（2026-07-12）

**完備性**：依指定命令重新 build `bin/apm-go.exe`，實跑兩邊 root `--help`。apm-go 13 個、Python 32 個，聯集 36 個頂層名稱；§1 主表 36 個名稱逐一存在且類別欄均非空。原 research 未處理的 `runtime`/`self-update` 另以 Python `--help` 與原始碼補核，兩者在 apm-go root help 均不存在，已補判 MISSING；`self-update` 未實跑。

**DIVERGENT 全數重現**（fixture 均位於 `%TEMP%/apm-go-codex-parity-audit`）：

1. `audit`：同一被竄改檔案，apm-go 回報相同 SHA-256 drift、exit 1；Python bare 掃描後 exit 0。
2. `pack`：A（dependencies only）Python 產 bundle、Go 無輸出；B（dependencies + marketplace）Python 產 bundle + marketplace、Go 只產 marketplace，兩邊 `marketplace.json` SHA-256 相同；C（target only）Python 產 plugin manifest、Go無輸出。
3. `update`：由 `microsoft/apm` v0.22.0 放寬 constraint，兩邊皆解析到 v0.24.1；Python 在空 stdin 顯示計畫後拒絕、exit 1、lockfile 未變，Go 無確認直接套用、exit 0、lockfile 改變。
4. `init`：兩邊 `--yes --target copilot,claude` 分別寫出 `target:`/`targets:`；Python 接受 CSV sugar、Go 拒絕；空 stdin 下 Go 成功建檔、Python EOF exit 1。另以含必填 `version` 的 plural fixture 確認 Go `validate` exit 0，證實 `targets:` 被靜默忽略。
5. `experimental`：裸呼叫 Go 印 help、Python 印 flag 狀態表，兩邊 exit 0。
6. CLI 解析層 exit code：unknown command、`install --bogus-flag`、`compile --bogus-flag` 三組皆為 Go exit 1、Python exit 2。

**high 原始碼證據抽核（5 項）**：

- `audit`：`cmd/apm/audit.go:15-56`、`internal/lockfile/audit.go:22-50` 與 Python `audit.py:807-1295`、`ci_checks.py:280-375` 均存在並支持 bare 語意分歧；`content-integrity` 在 baseline 順序為第 7 項。
- `pack`：`cmd/apm/pack.go:24-28,83-89,285-309` 明示且實作 marketplace-only；Python `build_orchestrator.py:346-435` 實作三 producer 路由。
- `init` schema：`internal/manifest/manifest.go:63-191` 僅有 `case "target"`（97 行），未知鍵在 155-157 行忽略；Python `apm_yml.py:1-108` 明定 plural canonical、CSV sugar 與互斥錯誤。
- `approve`/`deny`：Python `security/executables.py:1-34` 與 `commands/approve.py:55-122` 支持 deny-by-default gate 與寫入/撤銷；apm-go `internal/` 對 `allowExecutables`/`ExecutableDeclaration` 精確搜尋零命中，`internal/deploy/primitive.go:32-75` 的收集路徑無批准檢查。
- `run`：`cmd/apm/init.go:158-162` 確實提示不存在的 `apm-go run`，Go root help 無 `run`；Python `commands/run.py:20-92` 有完整 command handler。

**修正 6 處**：

1. `runtime`：由未分類補判 MISSING（low），補 `--help`/source 證據。
2. `self-update`：由未分類補判 MISSING（medium），只核 `--help`/source、未實跑；同步修正統計與 triage。
3. `audit`：`content-integrity` 的序位由第 6 改為第 7，前置檢查數由 7 改為 6。
4. `init targets:` transcript：fixture 補上必填 `version: 0.1.0`，避免先被無關 schema error 阻斷。
5. `init` 建議的 `targets` 專節引用由錯誤 `§4.7` 改為 `§4.3`。
6. high MISSING 專節導言由不存在的 `4.8-4.9` 改為 `4.8`，並把「分兩類」改為「分三類」。

**安全邊界**：未執行 oracle marketplace add/remove/update、publish、self-update、config 寫入、approve/deny；未碰 `evals/test1`；所有 fixture 與命令副作用均隔離在 TEMP scratch。
