# Findings: pack BundleProducer/PluginManifestProducer parity

> 持久化說明：本檔由主 session 代 research agent 持久化（該輪 harness 擋下
> subagent 的報告類檔案寫入）；內容為 agent 回傳全文，未刪改。原掛在
> 07-12-pack-parity（已依使用者裁定併回本 task Phase 2a）。

- **Query**: 移植 Python `apm pack` 缺失的 `BundleProducer`/`PluginManifestProducer`，與既有 `MarketplaceProducer` 並存。
- **Scope**: mixed（Python 原始碼 file:line 交叉對照 + apm-go 原始碼 file:line；本輪未跑 live probe——`07-12-cli-surface-parity-audit` 的 `group-packaging.md` 已有 Probe A/B/C transcript 證明「producers 缺席」，本檔把它擴展成實作級契約）。
- **Date**: 2026-07-12
- **Authority chain**: `.trellis/spec/guides/oracle-parity-gates.md`（Gate 1/2/3 直接適用）；`.trellis/spec/evals/cli-surface-parity-register.md` §3.2 / packaging §1（上游結論——登記冊只到「兩 producer 完全缺席」，本檔補齊欄位級/合併演算法/安全掃描細節）。

## Gate 1 disposition 表（design.md 可直接引用）

| Producer | 現況處置 | 本 task 目標 |
|---|---|---|
| `MarketplaceProducer` | (i) 已 PARITY-VERIFIED（07-03-marketplace-pack） | 不變；必須與新 producer 共存於同一個 `detect_outputs` 等價路由表 |
| `BundleProducer` | (ii)（P0 quickwin 只到「偵測到但不支援 → 警告」） | 升級為 (i) 完整 parity |
| `PluginManifestProducer` | (ii)（同上） | 升級為 (i) 完整 parity |

---

## 1. 三 producer 完整觸發矩陣

### 1.1 Python: `detect_outputs`

`D:/Projects/apm-dev/apm/src/apm_cli/core/build_orchestrator.py:346-393`：

```python
def detect_outputs(apm_yml_path: Path) -> set[OutputKind]:
    ...
    if data and data.get("dependencies"):
        out.add(OutputKind.BUNDLE)
    if data and data.get("marketplace"):
        out.add(OutputKind.MARKETPLACE)
    legacy = apm_yml_path.parent / "marketplace.yml"
    if legacy.is_file():
        out.add(OutputKind.MARKETPLACE)
    targets = parse_targets_field(data)   # raises Conflicting/Empty/UnknownTargetError
    if any(t in PLUGIN_MANIFEST_ECOSYSTEMS for t in targets):   # {"claude", "copilot"}
        out.add(OutputKind.PLUGIN_MANIFEST)
    return out
```

`BuildOrchestrator.run`（`build_orchestrator.py:414-435`）：`outputs_needed` 為空 → **raise `BuildError`**（不是靜默 exit 0）：

```
"apm.yml has neither 'dependencies:' nor 'marketplace:' block, and 'target:'
does not include 'claude' or 'copilot'. Nothing to pack. ..."
```

（`pack.py:371-375` 把 `BuildError` 轉 `click.ClickException` → **exit 1**。）

**關鍵細節**：`target: codex` 單獨存在（無 deps/marketplace）在 Python **不是 no-op 而是 exit 1**——`PLUGIN_MANIFEST_ECOSYSTEMS = frozenset({"claude", "copilot"})`（`core/plugin_manifest.py:56-64`）不含 codex。

`data.get("dependencies")`/`data.get("marketplace")` 用 Python truthiness（空 dict/list/None 皆 falsy）——與 apm-go `hasMarketplaceConfig`（key 存在且非 null 即算，`pack.go:340-342`）有 `marketplace: {}` 邊角差異（既有，不在本 task 範圍，但 `hasDeps` 新邏輯若沿用同慣例需留意——見 1.3）。

### 1.2 apm-go 現況：`hasMarketplaceConfig` + `deferredPackInputs`（P0 quickwin 產物）

`cmd/apm/pack.go:104-126`——二元 gate：`if !hasMarketplaceConfig(".")` 才查 `deferredPackInputs` 走 warn-only。`deferredPackInputs`（`pack.go:357-373`）：

```go
hasDeps = len(m.ParsedDeps) > 0 || len(m.ParsedDevDeps) > 0 ||
    len(m.MCPServers) > 0 || len(m.MCPDevServers) > 0
hasTarget = len(m.Target) > 0   // 不看 target 值是哪個
```

**相對 Python `detect_outputs` 的缺口（本 task 必須修，不只是加 producer）**：

1. `hasTarget` 對**任何**非空 `target:` 都觸發（含 codex/opencode/cursor）。Python 只對 claude/copilot 觸發 PLUGIN_MANIFEST；`target: codex` 單獨存在應為 **exit 1 硬錯誤**。P0 警告 gate 需拆成三情境：
   - `target:` 含 claude/copilot → 觸發 `PluginManifestProducer`（本 task 目標）
   - `target:` 只有非 claude/copilot 值、無 deps/marketplace → **exit 1**（同 Python `BuildError` 訊息）；現行 P0 警告（exit 0）是過渡態非終態
   - 三觸發條件皆無 → 見下點（Python 連這裡都是 exit 1）
2. **「nothing to do」本身 exit code 與 Python 不同。** Python：`outputs_needed` 空 → exit 1（`BuildError`）。apm-go（P0 前後皆同）：exit 0（`pack.go:122-124` default 分支印訊息 `return nil`）。此為**獨立於本 task 主目標的既有缺口**——要不要把 exit 0 改 1 需在 PRD 有自己的 Gate 1 disposition 行；不改就記 documented deviation。
3. `hasDeps` 用了 `ParsedDevDeps`/`MCPServers`/`MCPDevServers`，Python 只看頂層 `dependencies:` key（**不含** `devDependencies:`）。只有 `devDependencies:` 的 manifest 在 Python 是空集合（BuildError，exit 1），apm-go P0 碼卻 `hasDeps=true` → 警告。**重寫觸發矩陣時要修**——`hasDeps` 只看 `m.ParsedDeps`（dev deps 只在 producer 內的檔案過濾階段起作用，見 §3.2）。

### 1.3 完整觸發矩陣（本 task `detectOutputs` 等價函式的目標語意）

| `dependencies:` | `marketplace:`（或 legacy yml） | `target:`/`targets:` 含 claude/copilot | 觸發 producer | Python exit（無錯誤時） |
|---|---|---|---|---|
| 空 | 空 | 否 | 無 | **1**（`BuildError` "Nothing to pack"） |
| 非空 | 空 | 否 | Bundle | 0 |
| 空 | 非空 | 否 | Marketplace | 0 |
| 空 | 空 | 是 | PluginManifest | 0 |
| 非空 | 非空 | 否 | Bundle + Marketplace | 0 |
| 非空 | 空 | 是 | Bundle + PluginManifest | 0 |
| 空 | 非空 | 是 | Marketplace + PluginManifest | 0 |
| 非空 | 非空 | 是 | 全三個 | 0 |
| 空 | 空 | 有 target 但值非 claude/copilot（如僅 codex） | 無 | **1**（同第 1 列） |

三 producer **獨立觸發互不阻擋**（`BuildOrchestrator.run` 迭代 `self._producers`；任一 `produce()` raise 即中止整輪，已完成 producer 的輸出**不回滾**，見 §7.3）。

---

## 2. `PluginManifestProducer` 完整契約

### 2.1 `target:`/`targets:` 欄位解析

`D:/Projects/apm-dev/apm/src/apm_cli/core/apm_yml.py:47-108`（`parse_targets_field`）完整決策樹：

1. 兩 key 並存（`targets:` 和 `target:`）→ `ConflictingTargetsError`。
2. `targets:` 存在：`None`/空 list → `EmptyTargetsListError`（教學文案 `apm_yml.py:63-78`）；非 list scalar → 視為單元素 list；list → 逐元素 `str().strip()`、濾空、對 `CANONICAL_TARGETS` 驗證（9 個：`claude copilot cursor opencode codex gemini windsurf kiro agent-skills`，`apm_yml.py:25-37`）。
3. `target:` 存在（`targets:` 不存在）：`None` → `[]`（視為未設，落入 auto-detect）；list → 逐元素驗證（list sugar，:90-98）；非 list scalar → **CSV sugar**：`"claude,copilot"` → `["claude","copilot"]`（:99-105）。
4. 皆無 → `[]`。

**apm-go 對照**（`internal/manifest/manifest.go:111-116` + `:217-238`）三個具體缺口：

1. 完全沒有 `case "targets"`——`targets:` 落入 `manifest.go:179-181` default 被靜默丟棄（登記冊 §3.4 transcript 已證）。
2. `target: "claude,copilot"`（CSV sugar）→ `ValidateTarget` 把整串當一個 token → **整個 manifest 解析失敗**（比「忽略 CSV」更糟）。
3. **`CANONICAL_TARGETS` 集合不一致**：Python 9 個（含 `kiro`、無 `all`/`antigravity`）；apm-go `CanonicalTargets`（`internal/manifest/target.go:5-16`）10 個（缺 `kiro`、多 `all`/`antigravity`）。`ValidateTarget("kiro")` 現況 → unknown target 錯誤。此為相關但獨立缺口，**是本 task 前置**：不能正確解析 `target:`/`targets:` 就無法判定「值是否屬於 `PLUGIN_MANIFEST_ECOSYSTEMS`」。

### 2.2 `PLUGIN_MANIFEST_ECOSYSTEMS` 與輸出路徑

`core/plugin_manifest.py:56-70`：

```python
PLUGIN_MANIFEST_ECOSYSTEMS: frozenset[str] = frozenset({"claude", "copilot"})
PLUGIN_ECOSYSTEM_PATHS: dict[str, str] = {
    "claude": ".claude-plugin/plugin.json",
    "copilot": ".github/plugin/plugin.json",
}
```

僅此兩生態系；輸出在**專案根**（不在 build/ 下）。`seen_paths` 去重防衛（`build_orchestrator.py:289-298`）。

### 2.3 plugin.json 內容合成：`build_plugin_manifest`

`core/plugin_manifest.py:338-380`：

1. **一律從 apm.yml 重新合成**（`synthesize_plugin_json_from_apm_yml`）——**不讀**專案根既有 plugin.json（與 §3 的 `_find_or_synthesize_plugin_json`「先找再合成」策略不同）。docstring 明言 apm.yml 是 source of truth。
2. `synthesize_plugin_json_from_apm_yml`（`deps/plugin_parser.py:930-990+`）欄位：
   - `name`（必要；缺 → `ValueError`，:960-961）
   - `version`、`description`（有才寫）
   - `author`：**string** → `{"name": author}`；**dict** → 只取 `name`（必要）/`email`/`url`，缺 `name` → **整個 author 欄位丟棄**（:969-981）
   - `license`（有才寫）、`homepage`（`str()` cast）、`repository`（`str()` cast）
   - `keywords`：單字串 → 包成單元素 list；list → 逐元素 `str()` cast
3. 剝除 convention-directory keys：`agents`/`skills`/`commands`/`instructions`（`plugin_manifest.py:369-370`）。
4. **逐生態系 MCP 規則**（:372-378）：`claude` → `collect_mcp_servers(project_root)`（讀專案根 `.mcp.json` 的 `mcpServers`），非空 → `manifest["mcpServers"]`；`copilot` → **一律移除**（非 Copilot schema）。

### 2.4 `.mcp.json` 讀取與 secret 消毒：`collect_mcp_servers`

`core/plugin_manifest.py:222-278`，完整規則：

- 讀 `<project_root>/.mcp.json` 的 `mcpServers`；缺/symlink/parse 失敗 → 空 dict。
- **每個 server 物件**遞迴消毒（`_sanitize_value`，:175-214）：
  - key 名精確等於 `env`/`environment`/`headers`/`authorization`，或含 `token`/`secret`/`password`/`credential`/`apikey`/`key` 子字串（刻意過寬）→ **整個 key 丟棄**。
  - 字串值另過 6 個 regex（:100-153）：URL userinfo、`--token=VALUE`、`--token VALUE`（含 list 上下文 lookahead）、`ENV=secret`、`Authorization: Bearer/Basic`、已知供應商 token 前綴（GitHub、OpenAI sk-、Slack xox*、AWS AKIA/ASIA、Google AIza、GitLab glpat-、npm、PyPI、HuggingFace、SendGrid、Supabase、Databricks）→ `***REDACTED***`。
  - 任何丟 key/redact 都印警告列出全部路徑。

**apm-go 對照**：`internal/deploy/mcpcollect.go` 讀的是 apm.yml `dependencies.mcp:`（**不是** `.mcp.json`）；apm-go 全庫**零**處讀 `.mcp.json`（grep 證實）。`internal/credsec/redact.go` 的 `Redactor` 是「已知字面值替換」，與 Python「未知值、由 key 名/值 pattern 推斷」是**不同問題形狀，不能直接重用**；新演算法可放 `internal/credsec` 但函式全新。`credsec.MatchesSecretPattern`（`.env`/`.pem`/`id_rsa` 等，`redact.go:39-58`）**零生產呼叫者**，其 doc 預期過 packed bundle 用例但從未接線——接線會是 apm-go-only 比 Python 嚴的 EXTENSION（Python 無檔案級拒收，只有 `.mcp.json` 欄位級 redact + warn-only 隱字掃描），需明確設計決策。

### 2.5 寫入：`write_plugin_manifest`

`core/plugin_manifest.py:388-467`——覆寫/dry-run/安全策略全文：

1. `ensure_path_within(output_path, project_root)` 圍堵檢查（寫入前）。
2. `dry_run=True` → info「Would write plugin manifest to ...」，**不寫**。
3. `output_path.exists()`：無 `--force` → warn「already exists; skipping ... Re-run with --force to overwrite it.」**不覆寫**；`--force` → warn「Overwriting ... (--force).」繼續。
4. `rel_path` 以 `.github/` 開頭（copilot）→ 額外 info：「Writing generated plugin manifest under .github/: ...」（GitHub Actions 對 .github/ 高信任）。
5. `mkdir(parents=True)` 後**再次**圍堵檢查（TOCTOU 防衛）。
6. 寫 `json.dumps(manifest, indent=2, sort_keys=False) + "\n"`（**保留插入順序**、尾端換行）。
7. 成功 → `[+] Generated plugin manifest: {output_path}`。

**apm-go 可重用**：`internal/marketplace/build/output.go`（mkt-054 atomic write + `EnsureWithinRoot`）同類問題，但其 `WriteOutput` 是「一律覆寫」——需薄包一層 skip-without-force。

---

## 3. `BundleProducer` 完整契約

### 3.1 兩種 bundle 格式：`--format plugin`（預設）vs `--format apm`（legacy）

`bundle/packer.py:66-78`：`pack_bundle()` 依 `fmt` 分派——`"plugin"`（預設）→ `plugin_exporter.export_plugin_bundle`；`"apm"`（legacy）→ packer.py 自身邏輯。**本 task 只做 plugin 格式**（= Python 預設；`BuildOptions.bundle_format` 預設 "plugin"，`build_orchestrator.py:41`；CLI `--format` 預設同，`pack.py:154-158`）。本 task 不加 `--format` flag（§8），故 apm-legacy 分支無任何輸入路徑可觸發——「跟隨預設」，design.md 明記一行即可。

### 3.2 元件收集順序與來源

`bundle/plugin_exporter.py:416-676`（`export_plugin_bundle`）完整順序：

1. `migrate_lockfile_if_needed` + 讀 `apm.lock.yaml`（**lockfile 可選**——缺就跳過步驟 6 的依賴迴圈，只收 root package 內容）。
2. 讀 apm.yml：`name`/`version`（fallback `"0.0.0"`）。
3. **Guard**：`get_apm_dependencies()` 中任一 `dep_ref.is_local` → 立即 `raise ValueError`（與 `packer.py:122-130` 同文案）——local deps **整包拒絕**，不是逐個跳過。
4. `_find_or_synthesize_plugin_json`（「先找再合成」）——`suppress_missing_warning=_has_marketplace_block(apm_yml_path)`：**apm.yml 有 marketplace: block 時抑制「No plugin.json found」info**。
5. `dev_dep_urls = _get_dev_dependency_urls(apm_yml_path)`——`devDependencies.apm` 的 `(repo_url, virtual_path)` 集合，供步驟 6 過濾。
6. **依賴收集迴圈**（accumulator：`file_map`/`collisions`/`merged_hooks`/`merged_mcp`，迭代 `lockfile.get_all_dependencies()`）：
   - `is_dev`（lockfile flag 優先，fallback 步驟 5 URL 匹配）→ **整個跳過**。
   - `install_path` 目錄缺 → 跳過。
   - `_collect_apm_components`（`install_path/.apm/`）：`agents/`→`agents/` 平坦；`skills/`→`skills/` 遞迴；`prompts/`→`commands/` 遞迴 + `.prompt.md`→`.md` 改名；`instructions/`→`instructions/` 遞迴；`commands/`→`commands/` 遞迴；`extensions/`→`extensions/` 遞迴原樣。
   - `_collect_root_plugin_components`：dep **根層**的 plugin-native 慣例目錄（agents/skills/commands/instructions/extensions，遞迴）也收。
   - `_collect_bare_skill`：dep 根有 `SKILL.md` 且 skills 尚未收到 → 整個 dep 目錄（排除 apm.yml/apm.lock.yaml/plugin.json）映入 `skills/{slug}/`；slug 先用 `dep.virtual_path`（`_normalize_bare_skill_slug` 清理）否則 repo_url 末段。
   - `_merge_file_map`（first-writer-wins / `--force`→last-writer-wins，衝突必印，§3.3）。
   - Hooks：`.apm/hooks/*.json` deep-merge + 根層 `hooks.json`/`hooks/*.json` deep-merge（**dep 之間** first-wins，`overwrite=False`）。
   - MCP：`_collect_mcp(install_path)` 讀該 dep 的 `.mcp.json`（同 §2.4 消毒）——**dep 之間** first-wins。
7. **root package 自身元件**（`project_root/.apm` + 根層慣例目錄）：同收集器，**最後** merge——無 `--force`（first-writer-wins）時 **dep 的同名檔壓過 root package 的**——與下面 hooks/mcp 的方向**相反**，移植時極易寫反。
8. Hooks：root package 以 **`overwrite=True`** merge——**root 覆蓋 dep**。
9. MCP：root package `.mcp.json` 以 **`overwrite=True`** merge——同 hooks 方向。
10. 全部 collision 警告印出。

**衝突規則總表（逐格移植，不可一句 first-wins 帶過）**：

| 資料型 | dep vs dep | root package vs dep |
|---|---|---|
| `file_map`（agents/skills/commands/instructions/extensions） | lockfile 順序 first-wins（`--force`→last-wins） | **dep 贏**（root 最後 merge、無 force 被擋） |
| `hooks.json`（deep merge） | first-wins（`overwrite=False`） | **root 覆蓋 dep**（`overwrite=True`） |
| `.mcp.json`（deep merge） | first-wins（`overwrite=False`） | **root 覆蓋 dep**（`overwrite=True`） |

### 3.3 衝突處理：`_merge_file_map`

`plugin_exporter.py:683-709`：無 `--force`：已存在 → 印 collision（列兩個 owner），**不覆寫**；`--force`：同訊息（文字改 last writer wins），**覆寫**。兩情況都必印警告——`--force` 只改誰贏，不消音。

### 3.4 輸出佈局與 dry-run

`plugin_exporter.py:546-566`：

1. `output_files = sorted(file_map.keys())`（**排序**，確定性）；+ `"hooks.json"`（merged_hooks 非空）、`".mcp.json"`（merged_mcp 非空）、`"plugin.json"`（**恆附加**）。
2. `_sanitize_bundle_name`（:52-61）：name/version 各過 `[^a-zA-Z0-9._-]`→`-` + strip 頭尾 `-`；結果仍含 `..`/`/`/`\\` → 整個變 `"unnamed"`（雙重防衛）。
3. `bundle_dir = output_dir / f"{safe_name}-{safe_version}"`，`ensure_path_within(bundle_dir, output_dir)`。
4. **Dry-run**：立即回傳 `PackResult(bundle_path=..., files=output_files)`——**整個跳過安全掃描**（§3.5）、**零寫入**。

### 3.5 隱字安全掃描（warn-only）

`plugin_exporter.py:568-593`：

- **只在非 dry-run 跑**。走 `file_map` 的**來源**檔（copy 前）：symlink 跳過；目錄 → `SecurityGate.scan_files`；一般檔 → `scan_text`（`errors="replace"` 寬鬆解碼）。
- `WARN_POLICY`（`security/gate.py:44`：`on_critical="warn", force_overrides=False`）——`--force` 對掃描結果**零影響**；只計總數印一條彙總警告（「Bundle contains N hidden character(s) ... run 'apm audit' to inspect before publishing」），**永不擋** pack，無逐檔明細。
- apm-go **零**等價隱字掃描器（grep 證實）。Python `ContentScanner`（`security/content_scanner.py`）偵測規則**本輪未展開**——Gate 3 明標「尚未研究，design 不得假設是簡單邏輯」。

### 3.6 內嵌 lockfile：`apm.lock.yaml` 的 `pack:` 節

`bundle/lockfile_enrichment.py:180-276`（`enrich_lockfile_for_pack`）完整格式：

```yaml
pack:
  format: plugin        # 或 "apm"
  target: all           # list 時逗號串接
  packed_at: 2026-07-12T...isoformat
  mapped_from: [...]    # 只在跨 target remap 發生時出現
  bundle_files:          # 只在傳入 bundle_files 時出現
    agents/foo.md: <sha256 hex,無前綴>
    plugin.json: <sha256 hex>
# --- 原 lockfile 內容接續 ---
```

**關鍵格式細節**（不逐位對齊，§6 的 install 回路就讀不動 hash）：

1. `bundle_files` hash 值是**裸 hex**（`hashlib.sha256(...).hexdigest()`，`plugin_exporter.py:646`），**無** `sha256:` 前綴。apm-go `lockfile.HashFileBytes`（`hash.go:13-16,36-49`）輸出 **`sha256:`+hex**——直接重用格式就不匹配。Python 自己的 `_normalize_hash`（`bundle/local_bundle.py:268-279`）兩種都收（有前綴就剝），所以**格式不匹配不會壞 Python 消費 apm-go bundle**——但 A/B byte 比對需 design.md 明選「照 Python 裸 hex」vs「apm-go 前綴慣例」並記錄為刻意選擇。
2. `bundle_files` key **排序**（`dict(sorted(...))`，`lockfile_enrichment.py:269`）。
3. `local_deployed_files`/`local_deployed_file_hashes` **整組剝除**（:232-234，issue #887）。apm-go `lockfile.Lockfile` 結構**零** `pack:` 概念——需新序列化包裝層（不動 `SerializeLockfile` 本體）。
4. `pack.target` **純資訊 metadata**——install 端不讀它決定部署 target，只供 `check_target_mismatch` **警告**（不擋）。

### 3.7 壓縮檔（`.zip`/`.tar.gz`）——本 task Goal 外

`plugin_exporter.py:664-673`：`--archive` 壓縮後刪除來源目錄。`--archive` 預設 False——同 §3.1「跟隨預設」處理，design.md 明記一行。

---

## 4. license/SBOM 邊界（澄清，非實作範圍）

`export/authoring.py:32-53`（`warn_if_license_undeclared`）：

- **只在 authoring 路徑觸發**（對自己的 apm.yml pack/publish）；consuming 路徑完全靜默（docstring :8-10 明言刻意不對稱）。
- 呼叫點：`commands/pack.py:329-332`（`BuildOrchestrator().run()` **之前**，且 `if not json_output:`——`--json` 下永不出現）。
- **此警告不產 SBOM 檔**；SBOM 是 `apm lock export --format cyclonedx|spdx`（`commands/lock.py:283-309`，登記冊 P1 #15 另 track，本 task 範圍外）。
- **本 task 唯一可能入範圍**：移植這條「license undeclared」警告文字（純文字低成本）；apm-go 已解析的 `m.License`（`manifest.go:52,105-106`）可直接用。

---

## 5. 訊息格式與 exit code 對照

### 5.1 Python 渲染（非 JSON 模式）

`commands/pack.py:584-650`（`_render_bundle_result`）：dry-run 有 mapped_count → `[dry-run] Would remap N file(s): src/ -> dst/`；有檔案 → `[dry-run] Would pack N file(s) -> {bundle_path}` + 逐檔 `  {f}`；無檔案 → `_warn_empty`。非 dry-run：`Mapped N file(s)...`（有 remap 時）；`[+] Packed N file(s) -> {bundle_path}{size_suffix}`；`fmt=="plugin"` 加固定行「Plugin bundle ready -- contains plugin.json plus plugin-native directories (agents/, skills/, commands/, ...) and an embedded apm.lock.yaml for install-time integrity verification.」；結尾 `Share with: apm install {bundle_path}`。

`_render_marketplace_result`（:652-696，既有不變）。**Plugin manifest 無專屬非 JSON renderer**——使用者可見輸出全在 `write_plugin_manifest`/`build_plugin_manifest` 內（§2.5 logger），非 post-hoc render。

### 5.2 Exit codes（`pack.py:55-61` `_PACK_HELP`）

| Exit | 意義 | 觸發 |
|---|---|---|
| 0 | 成功 | 全 producer 成功（或個別 producer 無新東西可寫，如 plugin manifest 全 skip） |
| 1 | Build/runtime 錯誤 | 任一 producer raise `BuildError`（含 §1.1 Nothing to pack）；`_parse_path_overrides`/`_parse_marketplace_filter` 驗證失敗 |
| 2 | manifest schema 驗證錯誤 | **help 文有記載但本輪未追到直接觸發點**——Gate 3 標「未研究」，design 不得假設 |
| 3 | `--check-versions` 不對齊 | `pack.py:539-540`（release gate，範圍外） |
| 4 | `--check-clean` drift | `pack.py:541-542`（同上） |

apm-go 現況（`pack.go:46-52` 註解）：**只有 0/1**。兩個新 producer 走既有慣例（producer 錯誤 → non-nil error → exit 1）即**匹配** Python exit-1 語意。2/3/4 都在 Goal 外——design.md 明記處置即可。

---

## 6. `apm install build/<name>-<ver>`（或壓縮檔）回路——**重大缺口**

**最重要的阻斷性發現**：就算 BundleProducer 輸出與 Python byte-identical，**apm-go 目前完全沒有能力消費它**。

### 6.1 Python：`install <bundle-path>` 是完全獨立的 early-exit 路徑

`commands/install.py:1254-1332`：

1. `detect_local_bundle`（`bundle/local_bundle.py:222-260`）探測第一個 positional：根層有 `plugin.json` 的目錄；或 `.zip`（解到 temp、`_find_extracted_root` 找 plugin.json）；或 `.tar.gz`/`.tgz` 同邏輯；皆非 → `None` 落回一般解析。
2. 偵測成功 → **完全繞過**依賴 resolver/registry/org-policy gate（`install/local_bundle_handler.py:9-11` docstring 明言），呼叫 `install_local_bundle`：
   - 讀 bundle 內嵌 `apm.lock.yaml`（缺 → warn「Bundle has no apm.lock.yaml -- skipping integrity check...」**不擋**）。
   - 有 → `verify_bundle_integrity`（`local_bundle.py:282-357`）：bundle 內**任何** symlink 一律拒（即使不在 manifest 列）；逐檔 hash 驗 `pack.bundle_files`（含 `validate_path_segments`/`ensure_path_within` 防穿越）；**反向檢查**——bundle 內存在但 `pack.bundle_files` 未列的檔（apm.lock.yaml/plugin.json 除外）→ tampering 訊號（Unlisted bundle file）。任何錯 → 全列 + `click.Abort()`（**exit 1 全中止**，無部分安裝）。
   - 解析 targets（一般 `--target`/auto-detect）；零 target → warn + return（不部署，不算失敗）。
   - `check_target_mismatch`（§3.6——警告不擋）。
   - `integrate_local_bundle`（`install/services.py:702+`）——bundle 的 plugin-native 檔**直接**部署進各 resolved target 部署根；**與一般依賴部署管線平行且獨立的部署碼路徑**。
   - 結果寫進**專案** lockfile 的 `local_deployed_files`/`local_deployed_file_hashes`——**不動 apm.yml**（「imperative deploys, not declarative dependencies」）。
   - bundle 級 `.mcp.json` → 經 `MCPIntegrator.install` 寫入各 target 原生 MCP 設定格式（bundle 的 .mcp.json 是 metadata，永不原樣部署）。
   - 支援 `--as ALIAS` 與 `--trust-canvas-extensions`。
3. `--as` 等一般 install flags 與 local-bundle 路徑**互斥**——衝突 flag → 一條彙總 `click.UsageError`。
4. IM7（`install.py:1306-1330`）：存在但非合法 bundle 的路徑（無 plugin.json）→ 區分 legacy `--format apm` bundle（有 apm.lock.yaml 無 plugin.json）給 `apm unpack` 指引。

### 6.2 apm-go：**此路徑完全不存在**

- `cmd/apm/install.go` 對 `bundle`/`Bundle`/`isBundle` 零命中（grep 證實）。
- `--max-archive-bytes`/`--max-entries` + `archive.SafeExtract`（`install.go:34-37,146-158,375-464`）**只用於 registry 來源 .tar.gz 解壓**——與「使用者把 bundle 路徑當 positional」是**完全不同的觸發脈絡**。**登記冊 §4（unpack 節）的「apm-go install 已有 zip-bomb 防護」容易誤讀為「已支援 bundle install」——本研究澄清那是 registry-download-path-only。**
- apm-go 既有 local dep 概念（F1：`normalizeLocalDep`/`materializeLocalCopy`）前置條件是來源目錄有 **apm.yml**——bundle 根只有 `plugin.json`（**無 apm.yml**），前置直接不成立。
- `internal/lockfile/types.go` 的 `LocalDeployedFiles`/`LocalDeployedHashes` 欄位**已存在**（:44-49），現況唯一 writer 是 `cmd/apm/install.go:890-894`（專案自身 `.apm/` local primitives）——Python 同欄位雙用途。**lockfile schema 層不需新欄位，只缺新 writer**；缺口全在「偵測 + 驗證 + 部署」流程層。

### 6.3 結論（design.md/工作量估計直接引用）

**本 task 只做 pack 端的話，產出的 `build/<name>-<ver>/` 目前完全無法被 `apm-go install` 消費。** 這不是本 task 的 bug，是**必須明寫進 PRD 的 Gate 1 disposition**：

- **選項 (i) 完整 parity**：範圍涵蓋 pack 兩 producer **加** `install <bundle-path>` 偵測/驗證/部署子系統——遠大於 Goal 的「移植兩個 producer」字面（工作量訊號見 §9）。
- **選項 (iii) 明確延後**：pack 端先落地，`install <bundle-path>` 另開 follow-up，但 PRD 必須明寫：「本 task 產出的 bundle 目前只能被 Python `apm install` 或其他讀 plugin.json 的工具消費，apm-go 自己裝不了」——必須主動告知使用者，否則重演登記冊「同名指令、使用者期待落空」的失效模式（這次在 artifact 層）。
- **不建議**：完全忽略——Goal 明列「內嵌 lockfile」，其唯一用途就是 install 時完整性驗證；沒有消費端它就是死資料。

---

## 7. `MarketplaceProducer` 互動（既有 producer 需要的變更）

### 7.1 觸發路由重寫

`runPack` 現況是**二元 gate**（`if !hasMarketplaceConfig(".")`）——落地後需改為 §1.3 矩陣的**三個獨立檢查**（各 producer 自判、互不排斥）。現結構隱含「marketplace 優先、其餘只在 marketplace 缺席時檢查」，與 Python「三者獨立可同時觸發」矛盾（矩陣 5-8 列現結構根本表達不了；如 `marketplace:`+`dependencies:` 並存現在只跑 marketplace 邏輯、`dependencies:` 被靜默忽略——登記冊 Probe B 已證）。

### 7.2 執行順序

Python `BuildOrchestrator._producers` 預設順序 `[Bundle, Marketplace, PluginManifest]`（`build_orchestrator.py:411`）——**決定訊息輸出順序**。A/B transcript 比對需 apm-go 同序。

### 7.3 錯誤中止語意

任一 producer raise → **立即中止整輪**，已完成 producer 輸出**不回滾**（無交易語意，:427-434 無 per-producer try/except）。apm-go 同語意移植——**做回滾反而是偏離 oracle 的過度工程**。

---

## 8. Flags 對照（本 task 必要 vs Goal 外）

| Python flag | 用途 | apm-go 現況 | 本 task 需要? |
|---|---|---|---|
| `--format {plugin,apm}` | bundle 佈局 | 無 | **否**（§3.1 plugin-only=預設，flag 本身不需存在） |
| `-t/--target`（deprecated） | bundle 資訊 metadata | 無 | 否（Python 自己都 deprecate；pack.target 由 install 端決定） |
| `--archive`/`--archive-format` | 壓縮輸出 | 無 | **否**（§3.7） |
| `-o/--output`（預設 `./build`） | bundle 輸出根 | 無 | **或可省**——省略 flag（恆用預設）可接受，design.md 明記「無 -o override，輸出根固定 ./build」 |
| `--dry-run` | 預覽 | **有**（`pack.go:87`，現只服務 marketplace） | **是**——需擴展到兩新 producer，遵守 §3.4「dry-run 整個跳過安全掃描」 |
| `--force` | 衝突覆寫策略 | 無 | **是**——§3.3 file_map 衝突與 §2.5 既有檔覆寫都依賴它 |
| `-v/--verbose` | 細節 | **有**（`pack.go:90`） | 建議擴展，非阻斷 |
| `--offline`/`--include-prerelease` | marketplace-only | **有** | 不變 |
| `--check-versions`/`--check-clean` | release gate | 無（刻意） | 否 |
| `-m/--marketplace`/`--marketplace-path` | marketplace-only | **有** | 不變 |
| `--json` | 機器可讀 | 無 | 否——且 **Python 自己的 `--json` 對 BUNDLE producer 是不完整的**（`pack.py:498` 硬編 `"bundle": None`；:503-509 envelope 迴圈無 `OutputKind.BUNDLE` 分支）——上游自身不完整，明標不可當 oracle |
| `--legacy-skill-paths` | 跨 client skill 佈局 | 無 | 否（綁 §3.6 pack.target/mapped_from 機制，爆炸半徑大） |

**必要 flag 結論**：`--dry-run` 正確擴展 + 新 `--force`。六個排除 flag（`-o`/`--archive`/`--format`/`--json`/`--legacy-skill-paths`/`-t`）design.md 逐一記處置行。

---

## 9. apm-go 可重用元件盤點

| Python 元件 | apm-go 等價/可重用 | 重用度 |
|---|---|---|
| `_collect_apm_components`/`_collect_root_plugin_components` | `internal/deploy/primitive.go` `CollectLocalPrimitives`/`CollectDependencyPrimitives`（同樣走 `.apm/{instructions,agents,commands,hooks,prompts,skills}/`） | **高**——遍歷結構匹配；輸出形狀不同（Primitive 餵 per-target 部署轉換，bundle 要 bundle-relative 平坦路徑），需新「Primitive → bundle 輸出路徑」映射層，不能直接呼叫部署管線 |
| `_dep_install_path`/`get_install_path` | `internal/compile/compile.go:71-94` `CollectInstructions` 的 apm_modules 遍歷 + `deploy.DepRefKey` + `sortedTransitiveDeps` | **高**——compile 套件已實作 BundleProducer 需要的「local → direct deps（manifest 序）→ transitive deps（lockfile 序）」遍歷骨架，幾乎可照抄 |
| `find_or_synthesize_plugin_json`/`synthesize_plugin_json_from_apm_yml` | 無直接等價；`manifest.Manifest` **缺** `Homepage`/`Repository`/`Keywords`，`Author` 僅 scalar（§2.3） | **低**——需擴 `manifest.Manifest`（3 新欄位 + 結構化 author）或獨立 raw-YAML reader |
| `_deep_merge`（hooks/mcp 合併，深度上限） | 無等價（internal/ 無遞迴 map-merge 工具） | **低**——全新；深度上限（`_MAX_MERGE_DEPTH = 20`）與 overwrite 語意（§3.2 表）精確移植 |
| `collect_mcp_servers`（`.mcp.json` 讀 + secret 消毒） | `mcpcollect.go`（讀 apm.yml，非 .mcp.json）；`credsec/redact.go`（字面替換，非 pattern 推斷） | **低**——資料來源不同（§2.4），消毒演算法全新；`credsec` 可作套件位置但函式不可直接呼叫 |
| `_sanitize_bundle_name` | 無直接等價，邏輯簡單 | 中（簡單但全新） |
| `write_plugin_manifest` atomic write + 圍堵 | `internal/marketplace/build/output.go` `EnsureWithinRoot`/`WriteOutput` | **中**——圍堵概念可抄；`WriteOutput`「一律覆寫」≠「skip without --force」，需新包裝 |
| 隱字掃描（`SecurityGate`） | **完全沒有**（§3.5 證實零命中） | **零**——from scratch 與否需決策；Gate 3 標 Python 演算法未研究 |
| `pack.bundle_files` hash manifest | `internal/lockfile/hash.go`（`sha256:` 前綴 ≠ Python 裸 hex，§3.6） | **中**——演算法可用，格式需決策 |
| `apm.lock.yaml` `pack:` 節序列化 | `SerializeLockfile`（零 pack: 概念） | **低**——新包裝層，不污染通用 writer |
| `install <bundle-path>` 偵測/驗證/部署 | **完全沒有**（§6.2 證實） | **零**——見 §6，本 task 範圍決策的最大變數 |

---

## 交叉引用

- 登記冊 §3.2、packaging §1：本檔上游（缺席判定）；本檔補實作契約細節。
- oracle-parity-gates：Gate 1（本 task 即 disposition-(i) 落地）；Gate 2（§1.3/§6.3 輸入輸出須經真 parser/真流程驗證）；Gate 3（§3.5 隱字掃描已標未研究）。
- install-marketplace-contracts §4：§6.2 引其「local dep 需 apm.yml」前置證明 bundle-install 非同問題形狀，F1 邏輯不可重用。
- archive/2026-07/07-03-marketplace-pack（design.md）：MarketplaceProducer 等價物的既有先例；§7 重構 `runPack` 時參考其結構決策。

## Caveats / Not Found

- `security/content_scanner.py`（`ContentScanner` 實際偵測規則）**未展開**。§3.5 依 Gate 3 標記；design.md 不得假設是簡單邏輯。
- Python `--json` 對 BUNDLE producer envelope **不完整**（上游自身狀態，非本研究缺口）。
- Exit code 2（manifest schema 驗證錯誤）未追到直接觸發路徑（§5.2 標記）。
- **本輪未跑 live probe**——全部結論為原始碼交叉對照；group-packaging.md 的 Probe A/B/C 已覆蓋「新 producer 完全缺席」的 live 證據；實作時至少需為 §3.2 衝突規則表、§3.6 pack: 節格式、§6 install 回路建立自己的 A/B fixture。
- `synthesize_plugin_json_from_apm_yml` 讀到 `keywords` 欄位為止（§2.3）；:990 之後是否有額外欄位/後處理**未確認**——若後續發現缺欄位，回頭重讀 `plugin_parser.py:990+`。
