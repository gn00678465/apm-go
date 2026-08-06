# 上游 v0.27.0 → v0.28.0 delta 量測

**量測日期**：2026-08-06
**方法**：GitHub compare API（`gh api repos/microsoft/apm/compare/v0.27.0...v0.28.0`）。
本地上游 repo（D:/Projects/apm-dev/apm）**沒有 v0.28.0 tag**（只有 v0.26.0/v0.27.0，
HEAD=v0.27.0-2-g703dd9e7）；依唯讀規則未 fetch，全部證據來自 API 回傳的 patch。
**v0.28.0 = `dcbaf654`**（即 e2e 實跑時 `--ref HEAD` 解析到的同一個 SHA——遠端
HEAD 已被打上 v0.28.0）。

## 總量

- **19 commits / 166 檔 / +8980 −1260**
- src/apm_cli 佔 44 檔；tests 62 檔、docs 41 檔
- 相依 bump 3 個（gitpython/aiohttp/postcss）

## plugin/marketplace 範圍內的變更（7 項）

### 1. ⚠ G3 後繼：`dependencies:` 判定規則改變（48c6a95e）

`core/build_orchestrator.py` `detect_outputs`：

```python
- if data and data.get("dependencies"):          # v0.27: Python truthiness
+ if data and data.get("dependencies") is not None:   # v0.28: 非 None 即算
```

v0.27 語意：`dependencies: {}`（空 dict，falsy）**不**產 bundle。
v0.28 語意：`dependencies: {}` **產** bundle（只有整個 key 缺席或 null 才不產）。

apm-go 的 G3 修正（`cmd/apm-go/pack.go` `yamlValueIsTruthy`：mapping/sequence 以
`len(Content) > 0` 判定）精確鏡射 **v0.27** 的 truthiness——在 v0.28 基準下
`dependencies: {}` 一案就不再等價。若基準抬升，`yamlValueIsTruthy` 的呼叫點要改成
「非 null 即真」。docstring 也明示 `including {}`。

### 2. 巢狀 HTTPS source path（5a6213fd）

`marketplace/yml_schema.py`：`SOURCE_RE` 的 HTTPS 分支從 `owner/repo` 兩段改為
`path(/path)+` 任意深度（`_HTTPS_REPOSITORY_PAT`）；`split_host_from_source` 回傳
「完整 repository path」而非 owner/repo；錯誤訊息加註 `(nested paths allowed)`。
shorthand `host.tld/owner/repo` 維持兩段。
→ apm-go 的 source 驗證（manifest.ValidateMarketplaceSource / authoring 端）沿用
兩段規則，`https://gitlab.com/group/subgroup/repo` 會被拒。

### 3. audit 支援本地 marketplace + --strict 加嚴（4fe1dde2）

- `marketplace/audit.py`：`marketplace_source.kind == "local"` 且 plugin source 為
  字串時，改走新的 `resolver.resolve_local_plugin_path`（symlink-aware containment）
  直接讀本地 `apm.yml`——本地 plugin 從 UNSUPPORTED_SOURCE/skipped 變成**真的被稽核**。
  （對照：apm-go e2e 實跑本地 marketplace 全部 skipped，v0.27 行為。）
- `commands/marketplace/audit.py` --strict 語意加嚴：
  - 零 plugin 被稽核（ok=0 且 bypass=0）→ exit 1，「cannot verify supply-chain integrity」
  - skipped > 0 → exit 1，並提示 `--strict --verbose` 看 skip 原因
- **stdout 變更**：Summary 行只在「全 clean 且無 skipped/error」才用 success 符號，
  否則降為 info——我們 2026-08-06 剛對齊的 audit Summary 在 v0.28 又動了。

### 4. pack 有效輸出路徑統一（7eae5172）

新 `output_profiles.resolve_effective_output_path`（優先序：`--marketplace-path`
override > 明示 output_specs path > profile 設定屬性 > 預設），
builder `_output_path` / codex writer / drift gate / build_orchestrator 四處統一改用；
drift gate 簽名加 `output_overrides`；`_emit_drift_recipe` 的復原指令會帶
`--marketplace-path <fmt>=<path>`（shlex.quote）。--check-versions 的表格分支補印
error_messages。

### 5. version_check 接受 plugin collection（d7a409d7）

`marketplace/version_check.py`：本地套件無 `apm.yml` 時 fallback 讀 `plugin.json`
的 `version`（1MB 上限、拒 symlink、containment、ASCII 檢查），新增 4 個失敗
reason 碼。→ apm-go 無 `pack --check-versions`（旗標缺口清單內），暫無對應面。

### 6. 新 target 詞彙：grok-cloud / grok-build；kiro agents（ca0d7cd2 / 6d0d23e3）

`bundle/lockfile_enrichment.py` 新增 grok-cloud/grok-build cross-target map，且
cross-map 從「後者覆蓋」改為**多目標 fan-out**（同一來源可映射到多個目的地、
移除 `break`）；`core/target_catalog.py`/`target_detection.py` 擴充。kiro 新增
agents primitive 部署（`.kiro/agents/`）。
→ apm-go `CanonicalTargets` 無 grok-cloud/grok-build；internal/pack/bundle 的
cross-target 對應表與「單一覆蓋」語意需對照。

### 7. package add 傳遞 host（5a6213fd 的一部分）

`commands/marketplace/plugin/add.py`：`split_host_from_source(source)` 先拆
host，`_verify_source`/`_resolve_ref` 接 `host=` 參數（`RefResolver(host=...)`），
help 文字明示三種 SOURCE 形態。→ apm-go `package add` 的驗證與解析目前不拆 host。

## 範圍相鄰（目前 plugin-init 任務相關）

- **8998d0ee**：`apm init` 拒絕空/全空白專案名；名字派生失敗時 fallback
  （新 `core/project_name.py`，`_helpers._validate_project_name` 移過去留 alias）；
  互動與非互動路徑的錯誤訊息都改為「must not be empty or whitespace-only …」。
  `commands/plugin/` 本身零變更。

## 範圍外（依既有裁定屬其他任務）

install/*（7 檔，含新 argv.py/local_bundle_paths.py/target_hints.py）、
integration/*（4 檔，agent_integrator +259）、compilation/*（2 檔）、
adapters/client/*（vscode +108）、61881ca6 效能 refactor、mcp/hooks/compile 修正、
kiro/grok 的 deploy 面實作。

## 對「基準是否抬升」的量化輸入

| 項 | 若維持 v0.27.0 | 若抬到 v0.28.0 |
|---|---|---|
| G3（項1） | apm-go 現行為正確 | 需改 truthiness 為非 null 即真（小） |
| audit（項3） | 現行為正確（本地=skipped） | 本地稽核 + strict 加嚴 + Summary 條件化（中） |
| source regex（項2/7） | 現行為正確 | SOURCE 驗證與 host 拆分（中） |
| pack 輸出路徑（項4） | — | 視 apm-go pack 現況對照（中） |
| target 詞彙（項6） | — | grok*/kiro agents 屬 deploy 面，跨任務 |

## 未驗證聲明（量測時點）

- 以上為 compare API 的 patch 層級閱讀；量測時本地無 tag、未 fetch。
  （2026-08-06 稍後使用者已 fetch v0.28.0 tag，本地 `git rev-parse
  v0.28.0^{commit}` = `dcbaf654`，與 API 量測一致。）
- 166 檔中 tests/docs 未逐檔讀；src/apm_cli 範圍外 32 檔僅按 commit 標題歸類。

## 七項處理結果（2026-08-06，使用者裁定「抬到 v0.28.0，先補範圍內 7 項」）

端到端抽驗輸出見同目錄 `upstream-v0.28.0-e2e.log`；單元/CLI 測試
`go test ./... -count=1` 全綠。

| 項 | 結果 | 證據 |
|---|---|---|
| 1 dependencies 非 null 即真 | **已修** — `pack.go` `yamlValueIsTruthy`→`yamlValueIsNotNull`；matrix 測試更新（改期望前實跑，唯一轉紅的正是 `empty_mapping` 格） | e2e：`dependencies: {}` → `Would pack 1 file(s)` |
| 2 巢狀 HTTPS source | **已修** — `mcp.go` `marketplaceHTTPSRepoPattern`（僅 https 分支放寬；shorthand 維持兩段，測試同時釘住反例） | e2e：`add https://gitlab.com/group/subgroup/repo --no-verify` 寫入成功 |
| 3 audit 本地稽核 + strict + Summary | **已修** — `authoring/audit.go` `fetchLocalPluginApmYML`（plugin_root 組合 + 雙分隔符 traversal 拒絕 + pathWithinRoot containment）；cmd 層 strict 兩道新閘 + Summary 條件化；3 個新 CLI 測試 | e2e：本地 bypass 被抓出（`1 dependency bypasses`）、`--strict` 因 1 skipped exit 1 |
| 4 pack 有效輸出路徑 | **無病灶** — 上游修的是自家四個呼叫點不一致；apm-go 只有一個解析點（`pack.go:445` ResolveOutputPath）與一個寫出點（`pack.go:467` 唯一 build.WriteOutput 生產呼叫，grep 全 repo 佐證），優先序 CLI > 設定(map 形優先) > 預設已同 v0.28 語意 | `output.go:54-66` |
| 5 version_check plugin.json fallback | **無對應面** — apm-go pack 無 `--check-versions`（`pack.go:80-86` 全旗標清單）、`VersionAlignment` 全 repo 零命中；上游變更僅作用於該 gate 內部。若未來補 gate（估 ~250 行 version_check + pack 接線），需直接以 v0.28 形態實作 | pack 旗標缺口清單（cli-surface-parity 文件） |
| 6 grok target / fan-out | **部分修** — `grok-build` 進 CanonicalTargets（v0.28 `manifest_target_names()` 含之；`grok-cloud` 上游 experimental-gated 故不進）。cross-target fan-out 所在的 `_filter_files_by_target` 在 apm-go **無對應面**（bundle producer 僅存 target metadata 字串，`producer.go:498-502`；filter/prefix 邏輯 grep 零命中）——屬 pack bundle 深度缺口，非 v0.28 新增。kiro/grok 的 deploy adapter 屬 install/deploy 面（另一任務） | `target.go` + e2e：host-prefixed `gitlab.com/gitlab-org/gitlab-runner --ref HEAD` 解析出真 SHA |
| 7 package add host 拆分 | **已修** — `refcheck.go` `SplitHostFromSource` + `resolveCloneURL` host 路由（host-prefixed shorthand 原本一律誤導向 github.com——v0.27 期即存在的錯誤，一併修正）；add help 文字更新 | e2e：對 gitlab.com 真 ls-remote，`Resolved HEAD to afc6aa4f6cfc` |
