# Design: antigravity plugins bundle 部署

> **狀態：已實作（decided）**（2026-07-11，research agent 起草；拍板紀錄見 §8）。
> 實作已完成並通過 `go build/vet/test`、`agy plugin validate` 實機驗證與
> `evals/ab_antigravity.py`；§8 四項拍板結論、以及一項實作期新增決策（bundle
> 名碰撞由 diagnostic-only 升級為 fail-closed，見 §4.3 更新與 §8 附註）已記錄。

## 1. 目標與邊界

把 **dependency 套件**的 antigravity primitives 以 plugin bundle 形式部署到
`.agents/plugins/<pkg>/`，藉 per-plugin `hooks.json` 解掉共用 `.agents/hooks.json`
「覆蓋不合併」缺口；uninstall 沿用既有 per-file provenance 反向清理。

**邊界（不做）**：global scope（`~/.gemini/config/plugins/`）、plugins.json declared
註冊、`installed_version.json` 版本追蹤（PRD Non-Goals）；不新增 commands/prompts
支援；MCP 不遷入 bundle（§4.4）；Python oracle 無此能力（documented extension）。

## 2. 背景與證據基礎（實作前研究結論）

### 2.1 現行 antigravity 部署面（apm-go）

| Primitive | 現行目的地 | 證據 |
|---|---|---|
| instructions | `.agents/rules/<name>.md` byte-copy | `internal/deploy/antigravity.go:20` |
| agents | `.agents/agents/<name>/agent.md` | `internal/deploy/antigravity.go:29` |
| skills | `.agents/skills/<name>/`（跨 target canonical，req-tg-003） | `internal/deploy/antigravity.go:17-18`、`adapter.go:170-191` |
| hooks | 固定單檔 `.agents/hooks.json` byte-copy，多檔後蓋前＋診斷 | `internal/deploy/antigravity.go:31`、`deploy.go:154-155,194-201`、`deploy_test.go:1208`(S-003) |
| MCP | 合併式 `.agents/mcp_config.json`（`MCPTarget.WriteMCP`，非 DeployPrimitive） | `internal/deploy/mcp_antigravity.go:13-23`、`deploy.go:236-269` |

- 衝突解決在 adapter **之前**：`ResolvePrimitives`（`internal/deploy/conflict.go:13-51`）
  以 (Type,Name) 全域取一 winner（local 蓋 dep、先宣告勝）→ bundle 化不改變同名語意。
- provenance：`cmd/apm/install.go:881-909` 每次 install 以 `result.PerDep[key].Files/Hashes`
  **整批覆蓋**該 dep 的 `deployed_files`/`deployed_file_hashes`。
- uninstall：`cmd/apm/uninstall.go:239-246` → `RemoveDeployedFiles`
  （`internal/deploy/uninstall.go:34-82`：containment + hash 驗證(un-053) + 逐檔刪）
  → `cleanupEmptyParents`（:135-151）。MCP 反向走固定路徑清單
  `mcpRemoveTargets`（`mcp_remove.go:26-32`）＋ stale-diff（`cmd/apm/uninstall.go:267-282`）。
- 現行 codebase 無任何 `.agents/plugins/` 寫入者。

### 2.2 plugin bundle 格式實況

- 佈局（agy 內嵌文件，archive cli-plugins.md:142-156）：`.agents/plugins/<name>/` 下
  `plugin.json`（required）+ optional `hooks.json` / `mcp_config.json` / `rules/*.md` /
  `skills/<n>/SKILL.md`（agents/<n>/agent.md 亦被 validate 接受，contract §5）。
  hooks 由 agy 自行 lifecycle 合併（cli-plugins.md:160）。
- **agy 1.1.1 實測（2026-07-11，TEMP throwaway scratch）**：
  - `agy --version`=1.1.1（研究基線 1.0.16 已升版）；`plugin` 子指令集不變。
  - 完整 fixture validate `[ok]` exit 0（skills/agents/mcpServers/hooks 各 1 processed）。
  - **`plugin.json` 的 `name` 現為硬性必要**：`{}` → `Error: plugin.json missing name`
    （1.0.16 時代 optional 的結論已失效）。
  - name 含 `.` 可過、name ≠ 目錄名可過（validate 不驗 pattern/一致性）。
  - hooks.json namespaced shape `{"<hook-name>": {"<Event>": [...]}}` → `hooks: 1 processed`。
  - `rules/` 一律不出現在 validate 輸出（不報 processed 也不報 skipped）。
- Python oracle 無 plugin bundle：`apm/src/apm_cli/integration/targets.py:666-687`
  antigravity profile 僅 instructions/skills/hooks 平鋪（hooks 走 `_MERGE_HOOK_TARGETS`
  合併進共用檔，`hook_integrator.py:357-362`）；無 agents、無 bundle。→ documented extension。

### 2.3 A/B 腳本現況（evals/ab_antigravity.py）

- oracle＝實機 schema（docstring 明示，Python 非 oracle）。
- 受本設計影響的斷言：dep agent `.agents/agents/depagent/agent.md`（§2）、uninstall
  路徑（§3）、live leg 手工打包 validate（§4）——dep 遷入 bundle 後**必須同步改寫**；
  local primitives 斷言（rules/skills/hooks/mcp 平鋪路徑）不動。

## 3. 決策（已依研究建議預設；待 review 拍板）

| # | 決策 | 選擇 | 依據 |
|---|---|---|---|
| D1 | bundle 化範圍 | **只 dep 套件（DepKey != ""）；local primitives 維持平鋪** | plugin 官方定位是團隊散佈單位；workspace `.agents/` 就是專案自身 customization root；PRD AC 語境全是「套件」；local 斷言/行為零 churn |
| D2 | 遷入型別 | **rules/agents/hooks/skills 四型全遷**；commands/prompts 續不支援 | AC1「plugin.json + 對應 primitives 子目錄」；uninstall 整目錄收斂；多 target 併裝 skills 重複發現記為 documented caveat（§7 R2） |
| D3 | MCP | **不遷**，維持共用 `.agents/mcp_config.json` | merge 機制無覆蓋缺口；per-plugin 化會破壞 `mcpRemoveTargets` 固定清單與 stale-diff；validate 對缺 mcp_config.json 仍 PASS（實測） |
| D4 | bundle 名 | **DepKey 末段**（`skillNameFromDepKey` 同規則，`primitive.go:197-203`），sanitize 到 `[A-Za-z0-9._-]`（其餘字元→`-`） | 確定性、零額外 IO、與 skill-bundle 命名先例一致；碰撞發診斷（§7 R4） |
| D5 | plugin.json | 最小 `{"name": "<bundle-name>"}`＋LF 結尾，位元組確定性；**每次 run 冪等重寫並回報**（provenance 不掉檔） | name 在 1.1.1 硬性必要；install.go:886 整批覆蓋 deployed_files |
| D6 | 同套件多 hook 檔 | 收斂到該 bundle 唯一 `hooks.json`，維持後蓋前＋`writtenBy` 診斷 | plugin 格式一檔限制；缺口從「全 install」縮到「單套件內」 |

## 4. 技術設計

### 4.1 目錄契約（deploy 後形狀）

```
<project>/
├── .agents/
│   ├── rules/<name>.md              # local instructions（不變）
│   ├── agents/<name>/agent.md       # local agents（不變）
│   ├── skills/<name>/               # local skills（不變；其他 target 的 canonical 亦在此）
│   ├── hooks.json                   # local hooks（不變）
│   ├── mcp_config.json              # 全部 MCP（local + dep，合併；不變）
│   └── plugins/
│       └── <pkg>/                   # 每個有 antigravity primitives 的 dep 一個
│           ├── plugin.json          # {"name": "<pkg>"}，必要
│           ├── rules/<name>.md      # dep instructions（byte-copy，不剝 frontmatter）
│           ├── agents/<name>/agent.md
│           ├── skills/<name>/...    # 遞迴複製（SKILL.md + 附件）
│           └── hooks.json           # dep hooks（byte-copy）
```

`<pkg>` = sanitize(DepKey 末段)；`_local/<base>-<sha8>` dep → `<base>-<sha8>`。

### 4.2 adapter 變更（internal/deploy/antigravity.go）

`DeployPrimitive` 依 `p.DepKey` 分流（local 分支 = 現行程式碼原樣）：

```go
case TypeSkills:
    if p.DepKey == "" { return deploySkill(p, projectDir) }
    return deploySkillTo(p, projectDir, path.Join(antigravityBundleDir(p.DepKey), "skills"))
case TypeInstructions:
    if p.DepKey == "" { return deployFileToPath(p, fmt.Sprintf(".agents/rules/%s.md", p.Name), projectDir) }
    return deployFileToPath(p, path.Join(antigravityBundleDir(p.DepKey), "rules", p.Name+".md"), projectDir)
case TypeAgents:  // 同型：bundle 下 agents/<name>/agent.md
case TypeHooks:
    if p.DepKey == "" { return deployFileToPath(p, ".agents/hooks.json", projectDir) }
    return deployFileToPath(p, path.Join(antigravityBundleDir(p.DepKey), "hooks.json"), projectDir)
```

輔助函式（同檔）：

```go
// antigravityBundleDir returns ".agents/plugins/<sanitized leaf of depKey>".
func antigravityBundleDir(depKey string) string
// bundleNameFromDepKey: last "/" segment, chars outside [A-Za-z0-9._-] -> "-".
```

### 4.3 plugin.json 寫入與碰撞防護：新 optional interface（先例＝MCPTarget）

> **實作期更新（取代本節原草稿）**：碰撞處理由「diagnostic-only、仍照寫」改為
> **fail-closed**（§8 附註「實作期新增決策」）。因此 interface 拆成兩個方法，
> 分別在 deploy pipeline 的兩個不同時間點被呼叫：

```go
// BundleTarget is implemented by adapters that group dependency primitives
// into per-package bundles.
type BundleTarget interface {
    // ValidateBundleNames is called once per target, BEFORE any primitive is
    // deployed to ANY target in this Run() call, with the dep keys that WILL
    // receive at least one primitive under this adapter (computed from
    // ResolvePrimitives winners, before any file is written). Two different
    // dep keys sanitizing to the same bundle name must fail closed (non-nil
    // error, nothing written for either) rather than let their files mix.
    ValidateBundleNames(depKeys []string) error

    // FinalizeBundles is called once per target AFTER all primitives are
    // deployed, with the dep keys (first-deployed order) that actually
    // produced at least one file. Returns manifest rel-paths per depKey.
    // Idempotent: always rewrites and always returns the manifest path
    // (provenance survives re-install).
    FinalizeBundles(depKeys []string, projectDir string) (map[string][]string, error)
}
```

`deploy.go`'s `Run()` calls `ValidateBundleNames` for every target right after
`ResolvePrimitives` (step "3.5"), before the primitive deploy loop starts; a
collision aborts `Run()` entirely (`return nil, err`), so `install.go`'s
`deploy.Run` call fails and no lockfile is written — trivially satisfying
"nothing written before the error" since the deploy loop hasn't started yet.
`FinalizeBundles` keeps the original once-per-target-after-the-loop shape and
no longer needs to detect collisions itself (already fail-closed upstream).

`deploy.Run`（`deploy.go` 的 per-target primitive 迴圈之後、MCP 寫入之前）：

1. 迴圈中記錄 `bundledDeps []string`（去重、保序）：`p.DepKey != "" && len(files) > 0`
   且 adapter 實作 `BundleTarget`。
2. 呼叫 `FinalizeBundles(bundledDeps, projectDir)`；回傳的每個檔案照既有慣例
   hash（`lockfile.HashFileBytes`）後 append 進 `result.PerDep[depKey].Files/Hashes`，
   診斷進 `result.Diags`。
3. antigravity 實作：對每個 depKey 寫 `{"name": "<pkg>"}\n`（`json.MarshalIndent`
   固定形狀）；偵測 bundle 目錄碰撞（兩 depKey → 同名）→ 診斷
   `plugin bundle %q shared by %s and %s`，仍照寫（plugin.json 內容相同，rules 等
   同名檔已被 ResolvePrimitives 擋掉；跨名檔案混居記為 known limitation）。

### 4.4 MCP / uninstall / explicit-only：零變更

- MCP：`WriteMCP` 與 `mcp_antigravity.go` 原樣；bundle 內不產 mcp_config.json。
- uninstall：bundle 內所有檔（含 plugin.json）都在 per-dep `deployed_files` →
  `RemoveDeployedFiles` 逐檔 hash 驗證刪除 → `cleanupEmptyParents` 修剪
  `.agents/plugins/<pkg>/` 乃至 `.agents/plugins/`。使用者手動放入 bundle 的檔案
  不在紀錄 → 不刪、目錄保留（AC3 的「不誤刪」由既有 un-053 安全線保證）。
  手改 plugin.json → hash 不符 → 保留＋警告（殘留空殼 plugin，行為正確，記 spec）。
- explicit-only / alias / ResolvePrimitives / DeployRoots(`.agents/`)：全部不動。

### 4.5 相容性與 rollback 形狀

- **升級路徑**：舊版部署的平鋪 dep 檔案在新版 re-install 後不會自動回收
  （deployed_files 整批換新、舊檔失去紀錄）——與既有「換版殘檔」行為一致（R8）。
  乾淨遷移路徑＝先 `uninstall` 再 `install`；在 spec/發行說明記錄。
- **rollback**：revert commit 即回平鋪部署；已產出的 bundle 目錄對舊版是未知檔案，
  舊版 uninstall 依 lockfile 紀錄仍能清掉（紀錄的是 bundle 路徑）；agy 對
  `.agents/plugins/` 的發現不影響平鋪檔案的發現。無 lockfile schema 變更、
  無單向資料遷移 → rollback 安全。
- Python parity：本設計為 documented extension（Python 無 bundle）；
  instructions byte-copy／agents extension 等既有 deviation 延續適用於 bundle 內。

## 5. 資料流（install → uninstall）

```
install --target agy
  CollectLocalPrimitives / CollectDependencyPrimitives   (不變)
  → ResolvePrimitives (全域同名唯一 winner)               (不變)
  → antigravityAdapter.DeployPrimitive
      DepKey==""  → 平鋪路徑（現行）
      DepKey!=""  → .agents/plugins/<pkg>/... (新)
  → FinalizeBundles → plugin.json per bundled dep (新)
  → WriteMCP → .agents/mcp_config.json                    (不變)
  → install.go: PerDep.Files/Hashes → deployed_files      (不變)

uninstall <pkg>
  → deployed_files (含 bundle 全部檔案 + plugin.json)
  → RemoveDeployedFiles (hash 驗證) → cleanupEmptyParents  (不變)
  → RemoveMCPServersFromTargets(.agents/mcp_config.json)   (不變)
```

## 6. 測試契約（鎖定點）

1. dep 四型 primitives 落 bundle 路徑、local 四型維持平鋪（table-driven）。
2. 兩 dep 各帶 hook 檔 → 兩份 `hooks.json`、無 `overwrites` 診斷、各自 byte-equal 來源（AC2）。
3. plugin.json 內容/permission/換行確定性；首次與 re-install 後都在該 dep 的
   deployed_files+hashes（R3 鎖定）。
4. bundle 名碰撞 → 診斷存在、不 error。
5. 同套件兩 hook 檔 → 單 hooks.json + `overwrites` 診斷（D6 鎖定）。
6. uninstall：bundle 整目錄消失；bundle 內使用者手動檔存活且目錄保留；手改
   plugin.json 保留＋警告（AC3）。
7. 既有鎖定不破：`TestWriteMCP_Antigravity_SSEUsesServerUrlField`、
   `TestResolveTargets_Antigravity*`、`TestRun_AgentSameNameCollision_FirstDeclaredWins`
   （antigravity 分支斷言路徑改 bundle）、S-003（改為 local hooks 情境）。
8. 實機：`agy plugin validate .agents/plugins/<pkg>` PASS（AC1）；
   ab_antigravity.py 更新後全綠＋新增 bundle 驗證段（AC4）。

## 7. 風險登記

| # | 風險 | 處置 |
|---|---|---|
| R1 | agy 1.1.1 vs 1.0.16 未重測面 drift | 驗收全以本機 1.1.1 實測為準；spec 註版本；validate/help 已重測 |
| R2 | 多 target 併裝時 dep skill 雙份（canonical + bundle）被 agy 重複發現 | documented caveat 記入 spec；antigravity 單獨安裝無此問題 |
| R3 | plugin.json 只在新建時回報 → re-install 後 provenance 掉檔 | 設計上 FinalizeBundles 每次 run 都寫都回報（測試 6.3 鎖定） |
| R4 | DepKey 末段撞名 → bundle 混檔 | 與 skillNameFromDepKey 同型既有暴露；診斷＋known limitation |
| R5 | validate 不驗 rules/ → AC1 無法證明 rules 載入 | 既知；contract §4 已記 rules 非注入式，不擴驗 |
| R7 | 手改 plugin.json → uninstall 殘留空殼 plugin | un-053 預期行為；診斷已說明；記 spec |
| R8 | 換版舊檔殘留 bundle 內 | 全 target 既有行為，不擴 scope，記錄 |

## 8. 拍板紀錄（2026-07-11，實作完成後回填）

1. **D2 skills 遷入 vs 留 canonical**——**維持預設：遷入** `plugins/<pkg>/skills/<n>/`。
   依據：research 交叉核對後確認此為唯一與 AC1「plugin.json + 對應 primitives
   子目錄」語意一致的選項；R2（多 target 併裝時 dep skill 雙份被 agy 重複發現）
   已記為已知 caveat（spec §7.7），antigravity 單獨安裝無此問題。
2. **ab_antigravity.py「無回歸」＝更新 dep 斷言後全綠**——**採用**（唯一與
   D1/D2 遷移方案相容的解讀）。已執行：`evals/ab_antigravity.py` 的 dep agent
   斷言改為動態探測 bundle 目錄、uninstall 斷言改為 bundle 目錄整體消失、新增
   bundle 驗證段（plugin.json、hooks 隔離）、live leg 改為直接對
   apm-go 實際產出的 bundle 跑 `agy plugin validate`。實測全數 PASS。
3. **D4 bundle 名：DepKey 末段 sanitize（無 hash）vs 完整複用
   `localModulesKey`（含 hash 後綴）**——**維持預設：DepKey 末段、無 hash**，
   但採納 research 的 fallback 建議：**碰撞處理由 diagnostic-only 升級為
   fail-closed**（§4.3 已更新為 `BundleTarget.ValidateBundleNames`，deploy
   pipeline 開始寫檔前呼叫，碰撞則整個 `Run()` 回傳 error，不寫入任何一方的
   任何檔案）。理由：
   - 對非 local-path 的一般 git 依賴（如 `acme/tool` vs `other-org/tool`），
     兩者的 DepKey 末段確實會碰撞（沒有 hash 後綴消解）——fail-closed 避免
     research 風險登記第 4 點指出的「兩套件內容檔案混居同一實體目錄」資料
     污染問題，且不需要新增 hash 後綴這個額外欄位/命名慣例。
   - 對 materialized local-path 依賴（`./dep-a` 這類），`cmd/apm/install.go`
     的 `localModulesKey` 早已對 DepKey 本身附加 `sha256(abs)[:8]` 後綴
     （`_local/<base>-<hash8>`），所以取 DepKey 末段時天然帶有這個 hash——
     此類依賴實測（2026-07-11 TEMP scratch）不會碰撞：`./acme/tool` 與
     `./other-org/tool` 分別產出 `tool-79daa3bf`／`tool-ca8a114f`。
   - 因此「無 hash」只在一般 git 依賴才有真實碰撞面，且該面已被 fail-closed
     完全封閉（不是弱化為「診斷但仍寫」）——不需要再疊加 D4 選項 B
     （目錄名附加 hash）。test lock：`TestRun_AntigravityBundleNameCollision`。
4. **D1 local primitives 維持平鋪**——**維持**。無任何 local primitive 輸出
   路徑變動；`TestRun_AntigravityLocalPathsUnchanged` 鎖定。

實作完成證據：`go build ./... && go vet ./... && go test ./... -count=1` 全綠；
`internal/deploy`/`cmd/apm` coverage 88.5%/86.1%（≥80% gate）；`agy plugin
validate` 對 apm-go 實際產出的兩個 bundle 目錄實機 PASS（含 negative probes：
缺 name / 缺 plugin.json 皆 exit 1）；`evals/ab_antigravity.py` 全數 PASS；
`evals/ab_uninstall.py` 無回歸（6 passed, 0 failed, 2 documented deviations，
與此任務無關的既有 deviation）。
