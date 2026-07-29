# Research：upstream `apm marketplace` / `apm plugin` 對照

## 上游基準（pin）

- repo: `microsoft/apm`，default branch `main`
- commit: `634f7b603a8c`（2026-07-25T11:07:28Z）
- latest release tag: `v0.26.0`
- 取得方式：`gh api repos/microsoft/apm/git/trees/HEAD?recursive=1` + `raw.githubusercontent.com` 直抓原始碼
- 本地快取：`<scratchpad>/upstream/`（session 用完即棄，結論已抄錄於此檔）

docs 頁面（研究起點）：
- https://microsoft.github.io/apm/reference/cli/marketplace/ ← `docs/src/content/docs/reference/cli/marketplace.md`
- https://microsoft.github.io/apm/reference/cli/plugin/ ← `docs/src/content/docs/reference/cli/plugin.md`

---

## 結論一：`apm marketplace` 無功能缺口

13 個子指令本專案全數已註冊（`cmd/apm-go/marketplace.go:154-166`）：
consumer `add/list/browse/update/remove/validate`、author `init/migrate/check/audit/outdated`、
package `add/set/remove`。額外一個 `build` tombstone 指向 `pack`（`marketplace.go:662-663`）。

初次 docs 比對時列出 4 項疑似落差，逐一查上游原始碼後**全部證偽**：

### 1. `package set --name` — 上游沒有這個旗標

- 上游 `src/apm_cli/commands/marketplace/plugin/set.py:20-37` 的 click option 只有
  `--version --ref --subdir --tag-pattern --tags --include-prerelease --verbose/-v`。
- docs 那句 "same flags as add except `--no-verify`" 是**文件簡寫錯誤**，`--name` 不在其中。
- 本專案 `cmd/apm-go/marketplace_package.go:207-217` 旗標集合與上游逐項相同。
- → 不需修改。可另行回報上游 docs。

### 2. `check --offline` 語意 — 行為等價，只有 help 字串不同

- 上游 `commands/marketplace/check.py:57` help：`Schema + cached-ref checks only (no network)`。
- 但 `marketplace/ref_resolver.py` 的 cache 是 **process 內記憶體、TTL 5 分鐘**
  （`ref_resolver.py:1` docstring、`:119` `RefCache`、`:126` `_DEFAULT_TTL_SECONDS`、
  `:231` 每個 RefResolver 自建 `RefCache()`、`:586` `close()` 清空）。
- offline 模式下 cache **永遠不會被填**：`list_remote_refs` 在 `:390` 命中 `self._offline`
  就 `raise OfflineMissError`，而 `_cache.put` 只在 `:421` / `:445`（真的跑過 git 之後）才發生。
- ⇒ 全新 process 跑 `apm marketplace check --offline`，每個 remote entry 都會落到
  `check.py:198-207` 的 `error="No cached refs (offline)"` 並計入 `failure_count`；local entry 通過。
- 本專案 `internal/marketplace/authoring` 的行為相同：
  `refcheck_test.go:105-118`（offline + 有 pin 的 remote package → `Err != nil`，且
  `panicLister` 證明根本沒碰 lister）、`:120-133`（local package 仍通過）。
- → **可觀察行為一致**。唯一差異是 `cmd/apm-go/marketplace_authoring.go:303` 的 help 字串描述角度不同。

### 3. `--branch` 被 hidden — 上游也 hidden

- 上游 `commands/marketplace/__init__.py:510`：`@click.option("--branch", "-b", ..., hidden=True)`。
- 本專案 `cmd/apm-go/marketplace.go:257`：`MarkHidden("branch")`。
- → 一致。docs 有列但 CLI 不顯示，是上游 docs 與 CLI 自身的不同步。

### 4. `marketplace init --owner` 預設 — 皆為 `acme-org`

- 上游 `marketplace/init_template.py:85` 與 `:158`：`owner=owner or "acme-org"`。
- 本專案 `cmd/apm-go/marketplace_authoring.go:124` help 標註 `(default: acme-org)`。
- → 一致。

### 附帶查核

- `marketplace init` 上游旗標為 `--force --no-gitignore-check --name --owner --verbose/-v`
  （`commands/marketplace/init.py:18-32`），**沒有** `--yes`。
  docs `plugin.md` 的 aggregator 範例寫 `apm marketplace init --yes` 是上游 docs 的錯誤。
  本專案旗標集合與上游一致（`marketplace_authoring.go:121-125`）。
- 上游 `search` 是 top-level command（`commands/marketplace/__init__.py:1342`），
  不是 marketplace 子指令；本專案 `marketplace.go:145-148` 已註記刻意不實作，且現行 docs 頁面也未列出。

---

## 結論二：`apm plugin` 整組缺席（唯一實質缺口）

上游只有一個子指令：

```
apm plugin init [PROJECT_NAME] [-y] [--target TARGETS] [-v]
```

- group 定義：`commands/plugin/__init__.py:16-21`
- 指令定義：`commands/plugin/init.py:17-44`，本體只是
  `_perform_init(project_name, yes, plugin=True, marketplace_flag=False, target_flag, verbose, source="plugin")`
- 共用本體：`commands/init.py:126-320`，**與 `apm init` 是同一段程式碼**

本專案 `cmd/apm-go/main.go:25-35` 沒有 `plugin` group。

### plugin 模式相對 consumer init 的 6 個差異（上游 file:line）

| # | 差異 | 上游位置 |
|---|---|---|
| a | 專案名要過 kebab-case 驗證 `^[a-z][a-z0-9-]{0,63}$`，不過就 exit 1 | `commands/init.py:169-175` + `commands/_helpers.py:610-617` |
| b | `--yes` 時 version 預設 `0.1.0`（consumer 是 `1.0.0`） | `commands/init.py:217-218`；consumer 預設見 `_helpers.py:600-607` |
| c | `apm.yml` 多一個 `devDependencies: {apm: []}` | `_helpers.py:693-694` |
| d | 額外寫 `plugin.json` | `commands/init.py:237-238` + `_helpers.py:636-653` |
| e | Next steps 換成 plugin 作者版（`Pack as plugin: apm pack`） | `commands/init.py:289-292` |
| f | 有 `--verbose/-v` 旗標 | `commands/plugin/init.py:29` |

`plugin.json` 內容（`_helpers.py:644-653`）：

```json
{
  "name": "<config.name>",
  "version": "<config.version, 預設 0.1.0>",
  "description": "<config.description, 預設 \"\">",
  "author": { "name": "<config.author, 預設 \"\">" },
  "license": "MIT"
}
```

寫法：`json.dumps(indent=2)` 再補一個結尾換行。

### 共用本體裡本專案也沒有的一段（consumer + plugin 都會跑）

plugin-native 根目錄來源警告（`commands/init.py:224-234`）：
若 project root 存在 `agents/ skills/ commands/ instructions/ extensions/ hooks/` 任一目錄
（非 symlink）或 `hooks.json` 檔案，且 root 沒有 `.apm/` 目錄，就印警告提醒這些檔案仍會被
`apm pack` 收錄。清單定義於 `bundle/plugin_layout.py:5-24`。

本專案 `cmd/apm-go/init.go` 全檔無對應邏輯。

### 上游保留的 deprecated 別名

`apm init --plugin`（`commands/init.py:72-74` 旗標宣告、`:102-104` 印
`[!] 'apm init --plugin' is deprecated. Run: apm plugin init` 到 stderr）。

本專案 `init.go:223-225` 只有 `--yes/-y --target --force`，從未有過 `--plugin`，
因此沒有「需要被 deprecate 的既有介面」。

---

## 結論三：本專案 init 的兩個 targets 相關缺陷（研究過程中發現，非 docs 比對出來的）

### 3a. `readExistingTargets` 只讀單數 `target`（真缺陷）

- `cmd/apm-go/init.go:308-330` 只查 `doc["target"]`。
- 但 `internal/manifest/manifest.go:119`/`:125` 與 `:238`/`:240` 兩個鍵都收。
- 威脅模型 / repro：使用者用上游 `apm init` 產生 apm.yml（寫的是複數 `targets:`，見下），
  再在同一目錄跑 `apm-go init` → `readExistingTargets` 回傳 nil →
  `init.go:154` 的 MultiSelect 預選狀態遺失既有 targets，
  使用者若直接 Enter 就會把原本 pin 的 targets 覆寫掉。
- 成本估計：`init.go:317` 的 type switch 加一個 `targets` 分支，約 10 行 + 1 個測試。

### 3b. 寫檔用單數 `target:`，上游 canonical 是複數 `targets:`

- 本專案 `cmd/apm-go/init.go:237`：`data["target"] = targets`。
- 上游 `_helpers.py:673-683` 註解明示複數 list 形式為 canonical，
  即使呼叫端傳單數 CSV 也「normalise on disk to plural list form」。
- 影響：產出的 apm.yml 與上游不同形。本專案 parser 兩者皆收，所以不會自我打結，
  但跨工具 round-trip 時形狀會漂移。
- 成本估計：改一行 + 更新受影響的 golden/測試斷言（未清點測試數量）。

### 3c.（次要）上游會在 apm.yml 插入 targets 註解區塊

`_helpers.py:704-727`：有 targets 時插入三行說明註解；沒有時插入註解掉的 targets 骨架。
本專案 `buildManifestData` 無此行為。純可讀性差異。
