# Research: CLI Surface Parity — deps / lock / outdated / prune / view / cache / find / mcp

- **Query**: 盤查 apm-go 對 Python `deps`, `lock`, `outdated`, `prune`, `view`, `cache`, `find`, `mcp` 八個指令面的 parity（全部宣稱 Python-only）
- **Scope**: mixed（原始碼 file:line + Python 側 live probe，scratch under `%TEMP%/apm-parity-deps-scratch`，已於分析後刪除）
- **Date**: 2026-07-12

## Findings

### 前置驗證：apm-go 全 13 指令面

`bin/apm-go.exe --help` 枚舉：`audit compile completion experimental help init
install marketplace normalize pack uninstall update validate`。八個指令名稱
（`deps lock outdated prune view cache find mcp`）**全部不存在**，逐一實測：

```
$ apm-go.exe deps --help   -> Error: unknown command "deps" for "apm-go"
$ apm-go.exe lock --help   -> Error: unknown command "lock" for "apm-go" (Did you mean "pack"?)
$ apm-go.exe outdated --help -> unknown command
$ apm-go.exe prune --help    -> unknown command
$ apm-go.exe view --help     -> unknown command
$ apm-go.exe cache --help    -> unknown command
$ apm-go.exe find --help     -> unknown command
$ apm-go.exe mcp --help      -> unknown command
```

結論：本組全部八個指令判 **MISSING**（Python 有、apm-go 無）。其中 `mcp` 有部分
功能重疊（見該小節）。

---

### 1. `apm deps` — 依賴管理群組

**類別**: MISSING（整個群組；子指令 `deps update` 另標記見下）

**證據**（Python 原始碼）:
- 群組定義：`D:/Projects/apm-dev/apm/src/apm_cli/commands/deps/cli.py:266-269`
- `deps list`：`cli.py:388-471`（flags：`-g/--global`、`--all`、`--insecure`）
- `deps tree`：`cli.py:571-661`（flags：`-g/--global`；lockfile 來源優先，回退目錄掃描）
- `deps clean`：`cli.py:664-714`（flags：`--dry-run`、`-y/--yes`；整個移除 `apm_modules/`，不動 lockfile/已部署檔）
- `deps info`：`cli.py:913-931`（等同 `apm view <pkg>` 的本地元資料分支，委派 `view.py` 的 `display_package_info`/`resolve_package_path`）
- `deps why`：`D:/Projects/apm-dev/apm/src/apm_cli/commands/deps/why.py:1-248`（flags：`-g/--global`、`--json`；exit code 0=found/1=not-found-or-ambiguous/2=no-lockfile）
- `deps update`：`cli.py:717-910`——**已軟性淘汰**（第 784-789 行印出
  deprecation warning：`'apm deps update' is deprecated; use 'apm update' instead`）。

**Live probe（scratch，`mattpocock/skills` 為測試依賴，與既有 checklist 使用同一測試包）**：

```
$ apm deps list
APM Dependencies (Project)
┌───────────────────┬─────────┬────────┬─────────┬──────────────┬────────┬────────┬───────┐
│ Package           │ Version │ Source │ Prompts │ Instructions │ Agents │ Skills │ Hooks │
├───────────────────┼─────────┼────────┼─────────┼──────────────┼────────┼────────┼───────┤
│ mattpocock/skills │ 391a270 │ github │    -    │      -       │   -    │   21   │   -   │
└───────────────────┴─────────┴────────┴─────────┴──────────────┴────────┴────────┴───────┘

$ apm deps tree
parity-deps-scratch (local)
└── mattpocock/skills@391a270
    └── 21 skills

$ apm deps why mattpocock/skills
[i] mattpocock/skills@391a270  (direct dependency)
    mattpocock/skills   [declared in apm.yml]

$ apm deps info mattpocock/skills   # 與 apm view mattpocock/skills 輸出幾乎一致（缺 Ref/Commit 兩行，因未傳 project_root）
```

**apm-go 對應面**：apm-go 沒有任何 `deps` 子指令。`deps list`/`deps tree`/
`deps why`/`deps info` 的資訊在 apm-go 只能靠讀 `apm.lock.yaml`（人工）或
`audit`（完整性重驗，非瀏覽）取得，沒有等價指令。`deps clean` 等同「手動
`rm -rf apm_modules/`」，apm-go 也無此指令但使用者可自行操作檔案系統。

**嚴重度**: **medium** — `deps why` 是唯一能回答「這個套件為什麼被裝進來」
的工具（依賴圖反查，含 JSON 輸出給腳本用），`deps tree`/`deps list` 是日常
除錯必備的瀏覽指令。 `deps update` 因已被 Python 自身標記 deprecated 且已由
`apm update` 取代，**不需另開追蹤**（COVERED-ELSEWHERE：`apm update` 的
local-deps + 零 target 閘門已由 07-11 child 涵蓋，deps update 只是同一功能
的過時別名）。

**處置建議**: 另開 task 評估是否要補 `apm deps list`/`tree`/`why`（瀏覽類，
唯讀，風險低、使用者痛點高）；`deps clean`/`deps info` 優先度較低（`info`
的資訊已被規劃中的 `view` 涵蓋，見下）。

---

### 2. `apm lock` — 只寫 lockfile、不部署檔案

**類別**: MISSING

**證據**：
- 群組 + 主指令：`D:/Projects/apm-dev/apm/src/apm_cli/commands/lock.py:84-241`
  - flags：`-v/--verbose`、`-g/--global`、`--update`、`--no-policy`、
    `-t/--target`（範圍限制 policy 檢查，不影響部署——第 122-126 行 docstring
    明確聲明 "No files are deployed regardless of this value"）、
    `--parallel-downloads`（預設 4）
  - 核心行為（docstring 第 16-23 行）：resolve+download phase 照跑（含網路），
    但 targets/cleanup/post-deps-local/audit phase 全部跳過；integrate phase
    仍執行但因 target set 為空所以不部署任何檔案。
- 子指令 `lock export`：`lock.py:244-341`（flags：`-f/--format
  [cyclonedx|spdx]`、`-o/--output`、`-g/--global`、`--timestamp`）——**純讀
  lockfile 產 SBOM**，不重新 resolve/hash，不連網（docstring 第 286-289
  行："never re-resolves, re-hashes, or touches the network"）。

**Live probe（scratch）**：
```
$ apm lock
  [+] mattpocock/skills @391a2701 (cached)
[+] Lockfile written to apm.lock.yaml
# diff apm.lock.yaml 前後完全一致（無變更需要寫入時仍會覆寫）；.claude/skills/ 內容未被觸碰（部署階段確認未執行）

$ apm lock export --format cyclonedx
{
  "bomFormat": "CycloneDX",
  "components": [{"bom-ref": "pkg:generic/skills@391a2701...", "name": "mattpocock/skills", ...}],
  ...
}

$ apm lock export --format spdx
{ "SPDXID": "SPDXRef-DOCUMENT", ... "packages": [...] }
```
兩種 SBOM 格式皆驗證可正常輸出。

**apm-go 對應面**：apm-go 沒有「只產生 lockfile、不部署」的模式——`install`
一律連動部署（除非 dry-run 類 flag，需另查 install 逐 flag 清單，
COVERED-ELSEWHERE 由另一組覆蓋）。`lock export` 的 SBOM/inventory 匯出在
apm-go **完全沒有等價功能**（無 CycloneDX/SPDX 匯出路徑）。

**嚴重度**: **medium**（`lock` 主指令：CI 場景常見「先鎖版本、審核後才部署」
的工作流缺口）／**high**（`lock export`：SBOM 匯出屬合規/供應鏈安全常見需求，
完全空白且無替代路徑，risk 較高因為使用者可能誤以為 apm-go 沒有 SBOM 能力
而漏做合規檢查——不是「誤導」而是「靜默無此功能」）。

**處置建議**: `lock export`（SBOM）建議另開 task 評估——屬於安全/合規面
向，優先度可能高於單純的 CLI 便利性缺口；`lock`（bare）可與 `install
--frozen`/未來的 dry-run 需求合併規劃。

---

### 3. `apm outdated` — 檢查 locked 依賴是否落後上游

**類別**: MISSING

**證據**：`D:/Projects/apm-dev/apm/src/apm_cli/commands/outdated.py:391-426`
（Click 指令定義）；核心比對邏輯 `_check_one_dep`（252-388 行）：
- registry 來源依賴 → 委派 `deps/registry/outdated.py` 的
  `check_registry_locked_dep`（262 行）
- marketplace 來源依賴 → `_check_marketplace_ref`（135-218 行）比對
  installed ref vs marketplace manifest 目前 ref
- 完整 SHA pin → `_check_revision_pin_ref`（221-249 行）找最新 annotated tag
- tag pin → semver 比較候選 tag（298-358 行）
- branch pin → 比對 remote tip SHA vs locked SHA（359-388 行）

flags：`-g/--global`、`-v/--verbose`（顯示可用 tag 清單）、
`-j/--parallel-checks`（預設 4，0=循序）。

**Live probe（scratch，剛安裝完立即檢查）**：
```
$ apm outdated
[*] All dependencies are up-to-date
```
（符合預期：剛裝的 branch-pin 依賴，locked SHA = remote tip SHA。）

**apm-go 對應面**：完全無此功能。apm-go 的 `update` 指令會「重新解析並直接
覆寫 lockfile」，但沒有「只檢查、不動 lockfile」的唯讀預覽路徑——想知道
「有沒有東西過期」必須先跑 `update`（有副作用）才能看到 diff。

**嚴重度**: **high** — 這是明確的行為缺口而非同名分歧：使用者想做的
「唯讀健檢」在 apm-go 只能透過有副作用的 `update` 間接達成，等於缺少
「先看後動」的安全檢查步驟，對 CI 攔截（例如 nightly outdated-report）
場景是硬缺口。

**處置建議**: 另開 task，優先度偏高（CI 友善的唯讀檢查，且邏輯可大量複用
`update` 既有的 remote-ref 比對程式碼）。

---

### 4. `apm prune` — 移除 apm.yml 外的殘留套件

**類別**: MISSING

**證據**：`D:/Projects/apm-dev/apm/src/apm_cli/commands/prune.py:24-169`
- flag：`--dry-run`
- 邏輯：讀 `apm.yml` 建立「預期安裝路徑」（含祖先展開，
  `_build_expected_install_paths`/`_expand_with_ancestors`，`_helpers.py`
  共用），掃描 `apm_modules/` 實際安裝內容（`_scan_installed_packages`），
  差集即孤兒套件；**不只刪 `apm_modules/` 底下的套件目錄，還會清除該套件
  在 `deployed_files`（lockfile 記錄）指向的、已部署到 target（如
  `.claude/skills/`）的檔案**，並從 lockfile 移除該套件 entry
  （103-158 行）。與 `_check_orphaned_packages` 共用孤兒判定邏輯，刻意避免
  祖先展開錯誤遮蔽真孤兒（69-74 行註解強調此為破壞性指令必須與唯讀顯示路徑
  行為一致）。

**Live probe（scratch：先清空 apm.yml 的 dependencies 製造孤兒，再 prune）**：
```
$ apm prune --dry-run
[!] Found 1 orphaned package(s):
[!]   - mattpocock/skills (would be removed)
[*] Dry run complete - no changes made

$ apm prune
[!] Found 1 orphaned package(s):
[!]   - mattpocock/skills
[i] + Removed mattpocock/skills
[i] + Cleaned 21 deployed integration file(s)
[*] Pruned 1 orphaned package(s)
```
確認：`apm_modules/mattpocock/skills/` 整個目錄被刪除，且 `.claude/skills/`
目錄本身也被清空並移除（21 個已部署 skill 檔案連同空目錄一併清理）。

**apm-go 對應面**：apm-go 無 `prune`。`uninstall` 需要**明確指名套件**才能
移除；沒有「自動比對 apm.yml 找出孤兒」的功能。若使用者手動編輯 apm.yml
移除一個依賴後忘記手動 `uninstall`，apm-go 會讓孤兒套件與其部署檔案永久殘留
（`apm_modules/` 與 target 目錄都不會自動清理）。

**嚴重度**: **high** — 這不只是「少一個便利指令」，而是資料一致性缺口：
apm.yml 與實際磁碟狀態會隨時間漂移且無自動修復路徑，長期會累積殭屍套件與
殭屍部署檔案（可能造成 `apm find`/`audit` 類指令的判讀混淆，雖然 apm-go
沒有 `find`，但其 `audit`——lockfile 完整性重驗——同樣可能因殭屍檔案而誤判）。

**處置建議**: 另開 task，優先度高；實作可重用 apm-go 既有的 `uninstall`
刪除邏輯（deployed_files 清理路徑應該已存在，只差「自動找出孤兒清單」這一
層）。

---

### 5. `apm view` — 套件元資料 / 遠端版本列表

**類別**: MISSING

**證據**：`D:/Projects/apm-dev/apm/src/apm_cli/commands/view.py:432-491`
- 用法：`apm view PACKAGE [FIELD]`，`FIELD` 目前僅支援 `versions`
  （`VALID_FIELDS = ("versions",)`，第 26 行）
- flag：`-g/--global`
- 三條分支（447-491 行）：
  1. `FIELD=versions` → `display_versions`（338-409 行）：純遠端查詢，
     不需要套件已安裝，用 `GitHubPackageDownloader.list_remote_refs`；若
     package 是 `NAME@MARKETPLACE` 形式則改查 marketplace manifest
     （`_display_marketplace_plugin`，227-335 行）
  2. 無 FIELD 但 package 符合 `NAME@MARKETPLACE` → 同樣走 marketplace 分支
  3. 預設（本地已安裝套件）→ `display_package_info`（118-224 行）讀取
     `apm_modules/` 下的套件目錄，顯示 name/version/description/author/
     source/ref/commit/install_path/context files 統計/workflows/hooks 計數；
     若提供 `project_root` 會額外查 lockfile 補上 Ref/Commit
- `resolve_package_path`（34-89 行）：先做路徑穿越防護
  （`validate_path_segments`/`ensure_path_within`），再直接匹配或掃描兩層
  找套件；找不到則列出 `apm_modules/` 下所有可用套件並 exit 1。
- 註記：Python 舊名 `apm info` 保留為隱藏向後相容別名（第 6 行 docstring）。

**Live probe（scratch）**：
```
$ apm view mattpocock/skills
┌── Package Info: mattpocock/skills ──┐
│ Name: mattpocock-skills             │
│ Version: 391a270                    │
│ Description: No description         │
│ Author: Unknown                     │
│ Source: local                       │
│ Commit: 391a2701dd94                │
│ Install Path: .../apm_modules/mattpocock/skills │
│ Context Files: * No context files found │
│ Agent Workflows: * No agent workflows found │
└──────────────────────────────────────┘

$ apm view mattpocock/skills versions
Available versions: mattpocock/skills
┌─────────────────────────┬────────┬──────────┐
│ Name                    │ Type   │ Commit   │
├─────────────────────────┼────────┼──────────┤
│ v1.1.0                  │ tag    │ d574778f │
│ v1.0.1                  │ tag    │ 2454c95d │
│ v1.0.0                  │ tag    │ bddb833c │
│ ...(多個 branch)         │ branch │ ...      │
```

**apm-go 對應面**：完全無此功能。使用者無法在 apm-go 查詢「這個套件有哪些
可用的 tag/branch」（`view PKG versions` 是探索升級目標的主要入口，
與 `outdated` 互補），也無法快速檢視某個已安裝套件的摘要資訊（目前只能
翻 `apm.lock.yaml` 原始 YAML）。

**嚴重度**: **high** — `view PKG versions` 是「決定要 pin 到哪個版本」的
前置探索工具，沒有它使用者只能手動開瀏覽器查 GitHub tags/branches；
`view PKG`（本地元資料）則是日常除錯常用但非關鍵（可被 `deps info`
[同樣 MISSING] 或直接讀 lockfile 取代）。

**處置建議**: 另開 task；`view PKG versions` 优先度應優先於本地元資料分支，
因為它是「唯讀、不依賴已安裝狀態、直接對接 GitHub API」的相對獨立小工具。

---

### 6. `apm cache` — 本地套件快取管理（本組僅唯讀探測，未清除快取）

**類別**: MISSING

**證據**：`D:/Projects/apm-dev/apm/src/apm_cli/commands/cache.py:1-138`
- `cache info`（13-55 行）：顯示 cache root、Git repositories(db)/checkouts
  計數（`GitCache.get_cache_stats()`）、HTTP cache entries 計數
  （`HttpCache.get_stats()`）、總大小（bytes 格式化為人類可讀，128-138 行）
- `cache clean`（57-90 行）：**危險操作**，`--force`/`-f` 或 `--yes`/`-y`
  跳過互動確認，否則 `click.confirm`；清空全部 git repos+checkouts+HTTP
  cache（`git_cache.clean_all()` + `http_cache.clean_all()`）
- `cache prune`（93-126 行）：**危險操作**，`--days`（預設 30）依 mtime
  刪除逾期 git checkout；docstring 明確聲明（103-106 行）「不排除仍被
  lockfile 引用的項目——新舊只看檔案時間戳，不看引用關係」

**Live probe（唯讀，`cache info` 只查詢不寫入，遵循「cache 只唯讀」的指引，
未執行 `cache clean`/`cache prune`）**：
```
$ apm cache info
[i] Cache root: C:\Users\gn006\AppData\Local\apm\cache
[#]   Git repositories (db):    25
[#]   Git checkouts:            43
[#]   HTTP cache entries:       36
[#]   Total size:               1.4 GB
[#]     Git:                    1.4 GB
[#]     HTTP:                   7.6 MB
```
注意：cache 是**機器/使用者層級共用快取**（非本次 scratch 專案專屬），此
數字反映本機所有 apm 專案累積的快取內容，非本測試新產生。

**apm-go 對應面**：完全無此功能——apm-go 目前無獨立的本地 git/http 快取層
（或即使內部有快取機制，也未曝露任何 CLI 入口讓使用者查看/清理/瘦身）。

**嚴重度**: **medium** — `cache info` 唯讀查詢缺口影響有限（使用者頂多不
知道快取多大）；`cache clean`/`cache prune` 缺口影響稍大——若 apm-go 有
類似的本地快取但無法從 CLI 清理，長期會佔用磁碟空間且使用者無自助排除
手段（需手動找檔案系統路徑刪除）。

**處置建議**: 需先確認 apm-go 底層是否真的有等價的本地 git/http 快取實作
（本次未深入 apm-go cache 內部實作，僅確認 CLI 層無此指令）；若有快取但
無管理介面，屬於中優先度的可用性缺口，另開 task；若 apm-go 每次都是全新
clone/下載無快取，則是不同架構選擇，不算「缺口」而是「文件化的架構差異」。

---

### 7. `apm find` — 反查已部署檔案的來源套件

**類別**: MISSING

**證據**：`D:/Projects/apm-dev/apm/src/apm_cli/commands/find.py:1-242`
- 用法：`apm find FILE_PATH [--source] [--path]`
- 核心：`build_reverse_index`（27-57 行）從 `apm.lock.yaml` 的
  `deployed_files`（每個 `LockedDependency`）+
  `local_deployed_files`（workspace 自有檔案，鍵為 `.`）建立
  「部署路徑 → 擁有者套件 key 清單」反向索引；支援單一檔案被多個套件共同
  宣告（如 `AGENTS.md` 由多包合併寫入）
- `--source`：附加解析後的來源（`_format_origin`，65-95 行）——OCI registry
  URL / local path / git ref / git tag / commit（優先序見 docstring）
- `--path`：印出完整 root-to-target 依賴鏈（`compute_why`，與 `deps why`
  共用 `why_walker` 模組）
- 查詢正規化：`\` → `/`、去除開頭 `./`（204-207 行）；
  `_lookup_in_index`（123-153 行）支援精確匹配與目錄前綴匹配（最長前綴優先）
- Exit code：0=tracked、1=untracked、2=無 lockfile/lockfile 損毀

**Live probe（scratch）**：
```
$ apm find .claude/skills/ask-matt
mattpocock/skills

$ apm find .claude/skills/ask-matt --source
mattpocock/skills  mattpocock/skills@391a2701dd94

$ apm find .claude/skills/ask-matt --path
mattpocock/skills
  apm.yml -> mattpocock/skills

$ apm find does/not/exist.md
[x] 'does/not/exist.md' is not tracked by any installed package in apm.lock.yaml.
$ echo exit=$?
exit=1
```

**apm-go 對應面**：完全無此功能。apm-go 的 lockfile 同樣記錄
`deployed_files`（`internal/lockfile` 套件應有等價欄位，用於 `audit` 的
完整性重驗），理論上補這個功能的資料基礎已存在，只差 CLI 入口 + 反向索引
邏輯。

**嚴重度**: **medium** — 屬於除錯/稽核便利工具（「這個檔案是哪個套件裝進來
的？」），非核心工作流阻斷項，但對排查多套件合併寫入同一檔案（如
`AGENTS.md`、`CLAUDE.md`）的衝突很有價值。

**處置建議**: 另開 task；因為底層資料（lockfile 的 deployed_files）
apm-go 應該已經有等價結構（`audit` 指令的完整性重驗必然要讀這些欄位），
實作成本可能偏低，值得列入近期候選。

---

### 8. `apm mcp` — MCP registry 探索/安裝群組（**與 apm-go `install --mcp` 有重疊面**）

**類別**: MISSING（`mcp search`/`mcp show`/`mcp list` 三個唯讀探索子指令）
＋ **PARTIAL-OVERLAP**（`mcp install` 的安裝路徑已被 `install --mcp` 部分
覆蓋，但覆蓋不完整，見下）

**證據（Python）**：`D:/Projects/apm-dev/apm/src/apm_cli/commands/mcp.py`
- 群組：79-82 行
- `mcp install`（85-138 行）：**純轉發**——`Add an MCP server to apm.yml.
  Alias for 'apm install --mcp'`（88 行 help 文字），實作直接把參數改組成
  `["install", "--mcp", name, ...]` 呼叫 `cli.main()`（114-138 行）。也就是
  說 Python 側這個子指令本身沒有獨立邏輯，等同 `apm install --mcp`。
- `mcp search`（141-231 行）：`QUERY` 全文檢索 registry，flags
  `--limit`（預設 10）、`-v/--verbose`
- `mcp show`（233-419 行）：`SERVER_NAME` 查單一 server 詳情（含 remotes/
  packages/安裝指引三張表），flag `-v/--verbose`
- `mcp list`（422-511 行）：列出 registry 全部可用 server，flag
  `--limit`（預設 20）、`-v/--verbose`
- registry URL 決議鏈（`_build_registry_with_diag`，18-46 行）：
  `MCP_REGISTRY_URL` env > `apm config mcp-registry-url` > 預設公開 registry

**證據（apm-go）**：`internal/mcpregistry/client.go` + `resolve.go`
- `Client.FindServerByReference`（client.go:161-187）：以 `--registry` 傳入
  的 base URL 呼叫 MCP Registry v0.1 API，`GET /v0.1/servers?search=` 做
  fuzzy match，再 `GET /v0.1/servers/{name}/versions/{version}` 拉完整
  server record——**這其實就是 Python `mcp search` + `mcp show` 底層打的
  同一組 API**，但 apm-go 只在 `install --mcp NAME --registry URL` 這條
  安裝路徑內部呼叫，**沒有任何 CLI 指令把這個 client 的查詢能力單獨曝露
  給使用者**（無法「先搜尋看有哪些候選」，只能盲猜精確名稱直接裝）。
- `ResolveDeployable`（resolve.go:29-77）：明確限制**只支援 remote
  transport**（http/sse/streamable-http），第 37-43 行對「只有
  package-based/stdio 安裝方式」的 server（`info.HasPackages=true` 但
  `Remotes` 為空）回傳明確錯誤：`"only provides package-based (stdio)
  installation, which apm-go does not yet support"`。

**Live probe（Python，唯讀查詢，未寫入任何狀態）**：
```
$ apm mcp search fetch --limit 3
+ Found 3 MCP servers
  microsoft/clarity-mcp-server        Fetch Clarity analytics via MCP clients.
  io.github.Wopee-io/wopee-mcp        AI testing agents for web apps...
  ai.keenable/web-search              Live web search and clean-markdown...

$ apm mcp list --limit 5
+ Showing 5 MCP servers
  microsoft/markitdown, io.github.netdata/mcp-server,
  io.github.upstash/context7, io.github.ChromeDevTools/chrome-devtools-mcp,
  microsoft/playwright-mcp

$ apm mcp show io.github.upstash/context7
Deployment Type: Local Package (npm: @upstash/context7-mcp)
```
`context7` 是 package-based(npm) server，**沒有 remote endpoint**——若在
apm-go 執行 `install --mcp io.github.upstash/context7 --registry ...`，
按 `resolve.go:37-43` 的邏輯會直接報錯「不支援 stdio 安裝」，與 Python 端
`mcp show`（純資訊展示，不判斷是否可裝）行為分歧但屬預期內的架構限制，
非 bug。

**重疊面總結**：
| Python 子指令 | apm-go 對應 | 覆蓋狀態 |
|---|---|---|
| `mcp install NAME [-- cmd]`（stdio 或 remote，透過 `apm install --mcp` 轉發） | `install --mcp NAME [--url/--transport/--registry/...]` | **remote transport 部分覆蓋**；stdio package-based server 因為要用 `--` 分隔符轉發任意子命令，apm-go `install --mcp` flag 清單需另組覆蓋（COVERED-ELSEWHERE，由 install 逐 flag 清單那組負責） |
| `mcp search QUERY` | 無 | MISSING |
| `mcp show SERVER_NAME` | 無 | MISSING |
| `mcp list` | 無 | MISSING |

**嚴重度**:
- `mcp search`/`mcp show`/`mcp list`（探索三兄弟）：**medium** ——
  `install --mcp` 要求使用者已經知道精確的 server 名稱，沒有這三個查詢
  指令等於「盲裝」，體驗缺口明確但不阻斷核心工作流（可以先用瀏覽器查
  registry 網站）。
- 整體 `mcp` 群組定位：**low-medium**——因為 apm-go 已經把「裝」的核心
  路徑覆蓋了（`install --mcp`），只是缺「查」的路徑，不算靜默誤導
  （沒有同名分歧，是老實的 unknown command）。

**處置建議**: `mcp search`/`mcp list` 可考慮另開小 task 直接復用
`internal/mcpregistry.Client.searchServers`（目前是 unexported 方法，
需要曝露或加一層 CLI 包裝）；`mcp show` 同理復用 `getServer`。三個都是
唯讀查詢、無狀態風險，實作成本應該不高，且底層 HTTP client 已存在只差
CLI 層。`mcp install` 本身視為 documented alias 差異（Python 有獨立子
指令殼、apm-go 沒有殼但功能已存在於 `install --mcp`），**不需修 parity**，
建議在使用者文件註明「apm-go 用 `install --mcp` 取代 `mcp install`」即可。

---

## 本組總表

| 指令 | 類別 | 嚴重度 | 處置建議 |
|---|---|---|---|
| `deps`（群組：list/tree/clean/info/why） | MISSING | medium | 另開 task，優先 `deps why`/`deps list`/`deps tree`（唯讀瀏覽） |
| `deps update` | MISSING（但 Python 側已 deprecated，由 `apm update` 取代） | — | 不需另開；COVERED-ELSEWHERE（`apm update` 已涵蓋，07-11 child） |
| `lock`（bare，只寫 lockfile 不部署） | MISSING | medium | 另開 task，與 `install --frozen`/dry-run 需求合併規劃 |
| `lock export`（SBOM：CycloneDX/SPDX） | MISSING | **high** | 另開 task，優先度提升（供應鏈安全/合規面向） |
| `outdated` | MISSING | **high** | 另開 task，優先度高（CI 友善唯讀健檢，目前只能用有副作用的 `update` 間接得知） |
| `prune` | MISSING | **high** | 另開 task，優先度高（apm.yml 與磁碟狀態長期漂移無自動修復路徑，含已部署檔案殘留） |
| `view`（本地元資料） | MISSING | medium | 另開 task；可與 `deps info` 合併規劃（同源功能） |
| `view PKG versions`（遠端 tag/branch 列表） | MISSING | **high** | 另開 task，優先度應高於本地元資料分支（唯讀、獨立、探索升級目標必備） |
| `cache info` | MISSING | medium | 需先確認 apm-go 是否有等價本地快取層；若有則另開 task 補查詢介面 |
| `cache clean`/`cache prune` | MISSING | medium | 同上，需先確認底層是否存在對應快取機制 |
| `find` | MISSING | medium | 另開 task；底層資料（lockfile deployed_files）apm-go 應已具備，實作成本可能偏低 |
| `mcp install` | MISSING（殼）但功能已由 `install --mcp` 覆蓋 | low | 不需修 parity，文件化差異即可（COVERED-ELSEWHERE 部分，install 逐 flag 清單那組） |
| `mcp search`/`mcp show`/`mcp list` | MISSING | medium | 另開 task；可直接復用 `internal/mcpregistry.Client` 既有的 search/get 邏輯，只需加 CLI 層 |

**整體觀察**：本組八個指令名稱在 apm-go 皆為乾淨的 `unknown command`（無
DIVERGENT-SAME-NAME 風險，安全鐵則要求的「同名指令行為對照」不適用於本組
——沒有一個指令是兩邊都存在的同名指令），因此本組沒有 DIVERGENT 項、沒有
兩邊 transcript 需要並列比較。風險模式與 `pack`/`audit` 那種「同名陷阱」
不同，屬於「使用者輸入指令直接得到明確錯誤」，可見度較高，優先度排序應
依「該功能對日常工作流的必要性」而非「靜默誤導風險」：`outdated`、
`prune`、`view PKG versions`、`lock export`（SBOM）四項判定 **high**，是
本組建議優先修復/補齊的候選。

## Caveats / Not Found

- `cache` 一節僅做 `cache info`（唯讀）live probe，未執行 `cache clean`／
  `cache prune`（依安全鐵則，任何清除快取操作一律禁止實跑），因此無法
  100% 確認 `cache clean --force`/`cache prune --days N` 在邊界情況
  （例如快取為空、`--days 0`）的精確輸出文案，僅由原始碼靜態分析涵蓋。
- 未深入 apm-go 內部是否存在與 Python `GitCache`/`HttpCache` 等價的本地
  快取實作（超出本組任務範圍：本組只需盤查 CLI 指令面缺口，`cache`
  一節的「apm-go 對應面」判斷限定在「CLI 層無此指令」，未斷言 apm-go
  完全沒有底層快取機制）。
- `mcp install`（stdio package-based server，經 `--` 轉發任意子命令）與
  apm-go `install --mcp` 的逐 flag 對照**不在本組範圍**——PRD 已標記
  install 的完整 flag 清單由另一組（75 項 checklist）COVERED-ELSEWHERE，
  本節只確認「安裝路徑功能已被部分覆蓋」這個事實，不重覆逐 flag 比對。
- `outdated` 的 live probe 僅涵蓋「剛裝完、branch pin、up-to-date」情境；
  未實測 tag-pin outdated / SHA-pin outdated / marketplace-sourced dep
  outdated 三種分支的實際輸出（原始碼已讀懂邏輯，但未逐分支跑 transcript，
  因為需要人為製造「已落後上游」的情境，超出本次安全唯讀探測的效率考量）。
