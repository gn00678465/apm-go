# Design: pack 完整 parity + install 消費回路 + 共用 ContentScanner

> draft — 待 orchestrator/使用者 review

權威來源：`research/pack-parity-findings.md`（producer 契約全文）；本檔新增
ContentScanner/SecurityGate/.mcp.json 消毒器的完整移植契約（原標記「未研究」，
研究輪已展開）；`.trellis/spec/guides/oracle-parity-gates.md`（六道防線）；
`.trellis/spec/evals/cli-surface-parity-register.md` §3.1/§3.2。

**範圍鎖定（2026-07-12，使用者裁定）**：本 task 一次交付 pack 完整 parity
（BundleProducer + PluginManifestProducer）+ `install <bundle-path>` 消費回路
+ 共用 ContentScanner，兩處接線（pack warn-only + audit bare Unicode 掃描），
**不留可做而不做的 deferred**。「隱字掃描延後」決策已撤銷。

## Gate 1 disposition 表

| CLI 表面 | Python 完整行為 | 本 task 判定 | 說明 |
|---|---|---|---|
| `pack`：`BundleProducer` | dependencies: 非空 → plugin bundle | **(i) 完整 parity** | Phase 2/4 |
| `pack`：`PluginManifestProducer` | target/targets 含 claude/copilot → plugin.json | **(i) 完整 parity** | Phase 3 |
| `pack`：`MarketplaceProducer` | marketplace: 非空 → marketplace.json | **不變**（已 PARITY-VERIFIED） | 併入三路由 |
| `pack`：三 producer 獨立觸發 | 互不排斥、可同時觸發 | **(i)** | Phase 2 detectOutputs |
| `pack`：nothing-to-do | exit 1（`BuildError`） | **(i)** | Phase 2 |
| `pack`：隱字安全掃描 | warn-only，SecurityGate+WARN_POLICY | **(i)** | Phase 4，共用 internal/security |
| `install <bundle-path>` | 偵測/驗證/部署獨立路徑 | **(i) 完整 parity** | Phase 6 |
| `audit`（bare）隱藏 Unicode 掃描 | ContentScanner 掃描已部署檔 | **(i)** | Phase 7 |
| `audit`（bare）drift 偵測 | 部署檔 hash 對照 lockfile | **(i)——見下「audit drift 澄清」** | apm-go 既有 SHA 重驗**已是** hash-drift；Phase 7 確認是否達 bare-audit parity，殘留缺口於 Gate 6b 明列 |
| `audit --ci`/`--policy`/`--external`/`--format`/`-o`/`--strip` | lockfile 閘門/外部掃描器/結構化輸出/剝除 | **(iii) 獨立子系統，非本輪** | 各自完全獨立、apm-go 全無基礎；非「可做而不做」，是另一批功能 |
| `.mcp.json` reader + 消毒 | 讀專案根與每個 dep 的 `.mcp.json` | **(i)** | Phase 3 + Phase 4 |
| `bundle_files` hash 格式 | 裸 hex | **(i)**——刻意選裸 hex（Python 互通） | Phase 4 |
| `credsec` 檔案級拒收 | Python **無此功能** | **不接線**——Python 沒有，接了是超過 parity 不是達成 parity | 不排入 phase |
| `kiro` canonical target | Python 9 canonical 之一 | **(i)** | Phase 0 |
| `target:`/`targets:` 複數 + CSV sugar | 完整決策樹 | **(i)** | Phase 0（PluginManifest 路由前置） |
| `--format {plugin,apm}` | bundle 佈局二選一 | 本輪只做 plugin（=Python 預設） | flag 不存在，跟隨預設 |
| `--archive`/`-o`/`--check-versions`/`--check-clean` | 壓縮/自訂輸出/release gate | Python 自身也非預設路徑 | flag 不存在 |
| license/SBOM 警告 | authoring-only「license undeclared」文字 | 純文字，Phase 5 順帶移植 | 成本極低，一併做 |
| Python `--json` BUNDLE envelope 不完整 | 上游自身缺口 | 不適用 | apm-go 本輪不做 `--json` |

### audit drift 澄清（不做為 disposition 的模糊帶過）

Python bare `audit` 有兩個 pillar：隱藏 Unicode 掃描 + drift。**apm-go 現行
bare `audit`（SHA-256 重驗 `deployed_file_hashes`）本身已是 hash-based drift
偵測**——它就是在檢查磁碟部署檔有沒有偏離 lockfile 記錄。所以「drift」不是
apm-go 完全缺席、需要新 install-replay 引擎的東西。Phase 7 的責任：
(a) 加上缺的 Unicode 掃描 pillar；(b) 逐項核對 apm-go 既有 SHA drift 與
Python drift 語意的差異（Python 是否額外偵測「lockfile 未記錄但磁碟存在」的
孤兒檔、或 replay 級的內容重生成差異）；(c) 若核對後發現真實語意缺口，於
Gate 6b「此修正不做什麼」**具體列出該缺口**（不是模糊一句「drift 延後」）。
這是「查證後誠實揭露殘餘」，不是「未查證就宣告延後」。

## 套件結構

```
internal/manifest/
  target.go       # +kiro；CanonicalTargets 保留 all/antigravity(apm-go EXTENSION)
  manifest.go     # +case "targets"；parseTargetField CSV sugar（僅 target: 單數）；衝突偵測

internal/security/                    # 全新，無 apm-go 既有等價
  scanner.go      # ScanFinding、30 條 suspicious-range 表、ScanText/ScanFile/HasCritical/Summarize/Classify/StripDangerous
  gate.go         # ScanPolicy/WARN_POLICY/BLOCK_POLICY/ScanVerdict/SecurityGate.ScanFiles/.ScanText

internal/pack/
  detect.go       # DetectOutputs：三 producer 獨立觸發矩陣 + nothing-to-do → exit 1
  bundle/
    producer.go    # BundleProducer.Produce 全流程編排
    collect.go     # 元件收集（Primitive → bundle-relative 路徑）
    merge.go       # file_map/hooks/mcp 三種合併語意（方向相反規則）
    mcpjson.go     # .mcp.json reader + 消毒（新演算法）
    lockfile_pack.go # apm.lock.yaml pack: 節包裝（裸 hex、排序）
  pluginmanifest/
    producer.go    # PluginManifestProducer.Produce
    synthesize.go  # plugin.json 欄位合成（窄範圍 apm.yml 再讀取）
    write.go       # skip-without-force 包裝

internal/localbundle/                 # 全新，install <bundle-path> 消費端
  detect.go       # DetectLocalBundle：plugin.json 根 / .zip / .tar.gz
  verify.go       # VerifyBundleIntegrity：symlink 全拒、hash 驗、unlisted-file tamper
  integrate.go    # IntegrateLocalBundle：部署進 resolved targets；寫 lockfile local_deployed_*

cmd/apm/
  pack.go         # runPack 重寫：三路由 + Python 執行序 + 錯誤中止不回滾
  install.go      # 早退路徑：偵測 bundle path → 繞過 resolver，走 internal/localbundle
  audit.go        # 新增 Unicode 掃描 flag 分支，SHA reverify 既有不變
```

## 設計取捨（Surgical Changes）

- PluginManifestProducer 需要的 `Homepage`/`Repository`/`Keywords`/結構化
  `Author` **不擴充 `manifest.Manifest`**——`pluginmanifest/synthesize.go`
  對 apm.yml 窄範圍二次讀取，只取 plugin.json 專用欄位，比照 `output.go`
  `LoadOutputPathOverrides` 的既有先例（避免污染已審查的公開 schema）。
- `.mcp.json` 消毒不進 `internal/credsec`——`credsec.Redactor` 是「已知字面
  值替換」，這裡是「未知值、由 key 名/regex pattern 推斷」，資料形狀不同
  （findings §2.4）；新函式落 `internal/pack/bundle/mcpjson.go`。
  **6 條 redaction regex + key 名規則必須逐字對照 `plugin_manifest.py:73-278`
  原始碼**（本檔列語意供設計參考，implement 步驟要求對照原檔逐字核對，Gate 3）：
  1. URL userinfo `scheme://userinfo@` → `scheme://***REDACTED***@`
  2. `--flag=value`（flag 含 token/secret/password/credential/apikey/key，
     忽略大小寫）→ value 換 REDACTED
  3. `--flag value`（空白分隔同字串）→ 同上
  4. `ENV_NAME=value`（ENV_NAME 含關鍵字子字串）→ value 換 REDACTED
  5. `Bearer|Basic <token>`（≥8 字元）→ token 換 REDACTED
  6. 14 種已知供應商 token 前綴（GitHub `gh[posur]_`/`github_pat_`、OpenAI
     `sk-(proj-)?`、Stripe `sk_(live|test)_`、Slack `xox[baprs]-`、AWS
     `A(KIA|SIA)`、Google `AIza`、GitLab `glpat-`、npm `npm_`、PyPI `pypi-`、
     HuggingFace `hf_`、SendGrid `SG.`、Supabase `sbp_`、Databricks `dapi`）
     → 整串換 REDACTED
  - Key 級丟棄（遞迴任意深度）：精確等於 `env`/`environment`/`headers`/
    `authorization`，或（lower + 去底線後）含 `token`/`secret`/`password`/
    `credential`/`apikey`/`key` 子字串 → 整個 key 丟棄不下探。
  - List 語境：`["--token", "sk-abc"]` flag/value 分離時，前元素匹配
    secret-flag-name → 下一字串元素整個換 REDACTED。
  - server 名稱（top-level key）不消毒，只消毒 server 物件內部。
  - 任何丟棄/redact 印一行警告列出全部 path。
- ContentScanner 30 條 range **必須逐條核對** `content_scanner.py:33-108`
  原始檔（本檔摘要僅供參考）。三條特殊規則不可漏：(a) ZWJ(U+200D) 夾在兩
  emoji 之間降級 info；(b) BOM(U+FEFF) 檔首 info、其餘位置 warning；
  (c) `isascii()` fast-path（純 ASCII 直接回空 findings）。

## 三 producer 完整觸發矩陣（findings §1.3 定案）

| deps | marketplace(或 legacy yml) | target 含 claude/copilot | 觸發 | exit |
|---|---|---|---|---|
| 空 | 空 | 否 | 無 | **1** |
| 非空 | 空 | 否 | Bundle | 0 |
| 空 | 非空 | 否 | Marketplace | 0 |
| 空 | 空 | 是 | PluginManifest | 0 |
| 非空 | 非空 | 否 | Bundle+Marketplace | 0 |
| 非空 | 空 | 是 | Bundle+PluginManifest | 0 |
| 空 | 非空 | 是 | Marketplace+PluginManifest | 0 |
| 非空 | 非空 | 是 | 全三個 | 0 |
| 空 | 空 | 有 target 但非 claude/copilot | 無 | **1** |

`hasDeps` 修正為**只看 `m.ParsedDeps`**（現行 P0 碼誤含 ParsedDevDeps/
MCPServers/MCPDevServers，純 devDependencies: manifest 會誤觸發，Python 對
應是 exit 1）。三檢查獨立（非二元 gate）；執行序固定 Bundle→Marketplace→
PluginManifest；任一 producer error → 立即中止、已完成輸出**不回滾**
（findings §7.3，回滾是過度工程）。Phase 1 兩條警告常數在對應 producer
落地後移除。

## PluginManifestProducer 契約

觸發：target/targets 解析後與 `{claude, copilot}` 交集非空（依賴 Phase 0）。
`synthesize`：`name` 必要；version/description/license 有才寫；author
string→`{name}`，dict→name(必要)/email/url；homepage/repository 字串化；
keywords 單字串包 list。剝除 agents/skills/commands/instructions key。MCP：
claude 附消毒後 mcpServers、copilot 一律不附。輸出：claude→`.claude-plugin/
plugin.json`、copilot→`.github/plugin/plugin.json`（**專案根**）。JSON 2-space
indent、保留插入序、尾換行。無 `--force` 且存在 → skip 警告不覆寫；`--force`
→ overwrite 警告；`.github/` 額外 info 行。

## BundleProducer 契約

觸發：ParsedDeps 非空。Guard：任一直接 dep `IsLocal` → 整包拒絕（硬錯誤）。
收集順序（findings §3.2）：lockfile 依賴迴圈（dev-dep 跳過）→ `.apm/` 元件
+ dep 根層慣例目錄 + bare-skill；root package **最後合併**，file_map 方向與
hooks/mcp **相反**（file_map dep 贏；hooks/mcp root 贏 `overwrite=true`）。
衝突：無 force 印 collision 不覆寫，force 印覆寫，兩者都印警告。輸出：
`sorted(file_map)` + 條件式 hooks.json/.mcp.json + 恆附 plugin.json；
`_sanitize_bundle_name` 雙重防衛；`./build/<safe_name>-<safe_version>/`；
EnsureWithinRoot 圍堵。Dry-run：立即回傳清單、零寫入、**跳過安全掃描**。
掃描（非 dry-run）：走來源檔、symlink 跳過、`SecurityGate.ScanFiles/ScanText
(WARN_POLICY)`、彙總警告（總數）、`--force` 對掃描零影響、永不擋。內嵌
lockfile pack: 節：format/target(逗號串,純 metadata)/packed_at/bundle_files
(裸 hex、key 排序)；`local_deployed_*` 剝除；`SerializeLockfile` 不動，
`lockfile_pack.go` 獨立包裝層。

## install <bundle-path> 消費回路

早退插在 `runInstall` **最前面**（讀 apm.yml 前），對齊 Python
`install.py:1254`。`DetectLocalBundle`：plugin.json 根目錄/.zip/.tar.gz →
繞過 resolver/registry；偵測不到 → 原流程不變。`VerifyBundleIntegrity`：無
內嵌 lockfile → warn 不擋；有 → symlink **一律拒**、逐檔裸 hex 驗、反向
unlisted-file tamper 檢查；任何錯 → 全列 + exit 1 全中止。`IntegrateLocalBundle`：
解析 target、零 target → warn 不算失敗、check_target_mismatch(讀 pack.target
只警告)、plugin-native 檔**直接**部署(平行獨立碼路徑)、寫專案 lockfile
`LocalDeployedFiles`/`LocalDeployedHashes`(schema 已存在只缺 writer、不動
apm.yml)、bundle 級 .mcp.json 經既有 MCP 部署。**刻意不重用**
`normalizeLocalDep`/F1（前置需 apm.yml，bundle 根只有 plugin.json，形狀不同）
與 registry 的 `archive.SafeExtract` 呼叫點（可呼叫底層解壓函式，路徑全新）。

## audit 掃描接線

bare（無 flag）**維持 SHA-256 重驗不變**。新增 flag（命名 Phase 7 與 codex
確認，暫定 `--content`）觸發 Unicode 掃描：讀 lockfile `DeployedFiles`(跨
deps+local)→逐檔 `internal/security.ScanFile`→exit 0(clean)/1(critical)/
2(warning-only)；純文字輸出。drift 語意核對見上「audit drift 澄清」。

## Rollback Points

新碼全在新套件；既有檔改動限五處（pack.go/audit.go/install.go/manifest.go/
target.go）。每新套件、每接線點獨立 commit。

## Gate 6b 完成標準

test1 fixture（deps+`target: claude`+marketplace 三者皆有）：
1. `apm-go pack` vs `apm pack` 雙邊 transcript，逐 producer 比對。
2. `apm-go install build/<name>-<ver>/`（apm-go 自 pack 的 bundle）成功部署、
   lockfile 正確。
3. 竄改測試：改 bundle 內一檔位元組 → `apm-go install` 拒絕並列路徑。
4. 報告「此修正不做什麼」列在統計**之前**：audit 的 --ci/--policy/--external/
   --format/-o/--strip（獨立子系統）、若核對後 drift 有真實語意缺口則具體
   列出、pack --format apm/--archive/-o/--check-versions/--check-clean、
   credsec 檔案級拒收。
