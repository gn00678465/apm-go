# Research：上游 v0.26.0 實機跑測產物分析（eval 20260728T140015Z）

## 素材

- 來源：`D:\Projects\apm-dev\evals\apm-20260728T140015Z-1-001`
- 內容：
  - `新文件 7.md` — 三個 repo 的實際指令 + stdout 逐字紀錄（含 commit SHA）
  - `新文件 8.txt` — 使用者追加的兩項研究待辦
  - `apm-plugin-verify/{single-plugin-repo, aggregator-repo, record-marketplace-repo}` — 上游 `apm` 0.26.0 實跑後的檔案系統產物（含 `.git`）
- 產物版本佐證：`single-plugin-repo/apm.lock.yaml:3` → `apm_version: 0.26.0`，與
  `research/upstream-marketplace-plugin.md` 的 pin（`634f7b603a8c` / v0.26.0）同版。

這份紀錄的價值在於：它是**產物層級**的證據，可以驗證前一份純讀原始碼的研究結論，
並揭露只讀 `commands/plugin/` 看不到的跨指令缺口。

---

## A. 驗證前一份研究的結論（產物佐證）

| 前次結論 | 產物佐證 | 判定 |
|---|---|---|
| plugin 模式多寫 `devDependencies: {apm: []}` | `single-plugin-repo/apm.yml`、`aggregator-repo/plugin-2/apm.yml` 皆有 | ✅ 成立 |
| plugin 模式多寫 `plugin.json` | 三個 plugin repo 根目錄皆有，欄位為 `{name, version, description, author:{name}, license:"MIT"}` | ✅ 成立 |
| `--yes` 時 version 用 `0.1.0` | 本次全為**互動式**執行，產物皆 `1.0.0`；未觸及 `--yes` 路徑 | ⚠️ 未被反證，也未被驗證 |
| 上游 canonical 為複數 `targets:` | 三份 plugin `apm.yml` 全部是 `targets:` | ✅ 成立 |
| 上游會插入 targets 註解區塊 | 見下節，取得逐字內容 | ✅ 成立 |

---

## B. 上游 `apm.yml` 的實際形狀（本專案 init 的直接對照組）

`single-plugin-repo/apm.yml` 逐字：

```yaml
name: design-taste
version: 1.0.0
description: APM project for design-taste
author: Madao
# Which agent platforms to deploy to.
# Resolution order: --target flag > this field > auto-detect from filesystem.
# Accepted values: agent-skills, antigravity, claude, codex, copilot, cursor, gemini, kiro, opencode, windsurf
targets:
  - claude
  - codex
  - opencode
dependencies:
  apm: []
  mcp: []
includes: auto
devDependencies:
  apm:
    - pbakaus/impeccable
scripts: {}
```

本專案實跑對照（`go build ./cmd/apm-go` 後於空目錄執行
`apm-go init design-taste --yes --target claude,codex,opencode`）：

```yaml
author: Madao
dependencies:
  apm: []
  mcp: []
description: APM project for design-taste
includes: auto
name: design-taste
scripts: {}
target:
  - claude
  - codex
  - opencode
version: 1.0.0
```

三個具體落差（皆為本次實跑觀察，非推測）：

### B1. 鍵序：本專案是字母序，上游是語意序

- 成因：`cmd/apm-go/init.go:229-246` 的 `buildManifestData` 回傳 `map[string]any`，
  `yamllib.Marshal`（go.yaml.in/yaml/v4）對 map 以鍵字母序輸出。
- 上游語意序為 `name → version → description → author → targets → dependencies →
  includes → devDependencies → scripts`。
- 影響：純可讀性 / diff 友善度；解析結果等價。
- 成本估計：改為 `yaml.Node` 或有序 struct 組裝，約 30–50 行；會動到既有 init 測試斷言（未清點）。

### B2. 單數 `target:` vs 複數 `targets:`（已知，此處為產物佐證）

- `cmd/apm-go/init.go:237`。已於前份研究列為 3b，使用者已裁定「讀寫都對齊複數」。

### B3. 缺 targets 註解區塊，且註解內的可選值清單與本專案不同

- 上游註解列出 10 個值：`agent-skills, antigravity, claude, codex, copilot, cursor,
  gemini, kiro, opencode, windsurf`。
- 本專案 `internal/manifest/target.go:25-27` 的 `SupportedTargets` 只有 5 個：
  `claude, codex, copilot, opencode, antigravity`；
  `adapterTargets`（`target.go:29-36`）多一個 `agent-skills`，共 6。
- ⚠️ **未驗證**：本專案是否本來就刻意只支援這 6 個 target（是產品決定），
  或是尚未補齊 cursor/gemini/kiro/windsurf。需在 PRD 前確認，否則直接照抄上游註解會
  宣稱支援不存在的 target。

---

## C. eval 揭露的新缺口（前一份研究沒抓到）

### C1. `apm install --dev` 完全缺席（critical path）

- 上游 plugin 作者流程三步：`apm plugin init` → `apm install --dev <owner>/<repo>` → `apm pack`
  （`新文件 7.md:20-21` 的 Next Steps 逐字印出這兩步）。
- 實跑佐證：`single-plugin-repo/apm.yml` 的 `devDependencies.apm` 內有
  `pbakaus/impeccable`，是 `apm install --dev pbakaus/impeccable` 寫入的。
- 本專案狀態：
  - `internal/manifest/manifest.go:160` **有** `case "devDependencies"` 的解析。
  - `cmd/apm-go/install.go:180-195` 的旗標表**沒有** `--dev`（已逐行看過該區塊）。
- ⇒ `apm-go plugin init` 印出的 Next Steps 第一步會是一個本專案跑不動的指令。
- 成本估計：`--dev` 需貫穿 install 的 apm.yml 持久化（`install.go:1768-1778`）、
  lockfile、以及 uninstall/update 對 dev 區塊的處理，屬中型工作（非一行旗標）。
  **建議獨立成子任務**，不要塞進 plugin init。

### C2. `install` 教學錯誤訊息也印單數 `target:`

- `cmd/apm-go/install.go:829-832`：
  `"2. Add a target: field to apm.yml, e.g.:"` / `"       target:"`。
- 上游同情境（`新文件 7.md:155-158`）印的是 `targets:`。
- 這與 B2 是同一個單複數缺陷的第二個現場，且這一處是**直接教使用者寫錯**。
- 成本估計：2 行字串 + 對應測試斷言。

### C3. `marketplace package add` 的 HEAD 解析行為不同

上游實跑（`新文件 7.md:104-110`、`:212-226`）：

```
$ apm marketplace package add DietrichGebert/ponytail        # 未給 --ref
[i] Resolved HEAD to 16f29800fd26
[+] Added package 'ponytail' from DietrichGebert/ponytail

$ apm marketplace package add emilkowalski/skills --no-verify   # 離線
[x] Cannot resolve HEAD ref without network access. Provide an explicit --ref SHA.
```

⇒ 上游即使**沒有** `--ref` 也會解析 HEAD，且 `--no-verify` 只跳過 reachability，
**不**跳過 HEAD 解析。

本專案 `internal/marketplace/authoring/editor.go:511-520` 的 `resolveRef` 註解明寫
「an empty ref short-circuits before ever calling lister」——沒給 `--ref` 就不解析、
不寫 `ref:`。

**已回讀上游原始碼定案（confirmed，非推測）**：

- `commands/marketplace/plugin/add.py:67` 無條件呼叫
  `ref = _resolve_ref(logger, source, ref, version, no_verify)`。
- `commands/marketplace/plugin/__init__.py:102-142` 的 `_resolve_ref`：
  - `:117-118` `version is not None` → 回傳 `None`（版本區間釘選，不需要 ref）
  - `:121-122` ref 已是 40-hex SHA → 原樣存
  - `:125` `is_head = ref is None or ref.upper() == "HEAD"`
    ⇒ **沒給 `--ref` 就是隱含 HEAD**
  - `:127-132` `is_head and no_verify` → 印
    `Cannot resolve HEAD ref without network access. Provide an explicit --ref SHA.`
    並 `sys.exit(2)`
  - `:133-137` 顯式給 `HEAD` 時額外印
    `'HEAD' is a mutable ref. Resolving to current SHA for safety.`
  - `:138-140` 否則 `RefResolver().resolve_ref_sha(source, "HEAD")`

- ⇒ 本專案 `editor.go:511-520` 的空 ref 短路是**真缺口**（不是刻意契約）：
  `apm-go marketplace package add owner/repo` 不給任何旗標時，
  上游會寫入 `ref: <sha>` pin，本專案不會，兩邊 apm.yml 產物直接分歧。
- 附帶查核：`--version` / `--ref` 互斥上游在 `add.py:53-57`，
  本專案 `cmd/apm-go/marketplace_package.go:37` +
  `internal/marketplace/authoring/editor.go:440`/`:519` 已有，**一致**。
- 成本估計：`resolveRef` 增加 implicit-HEAD 分支 + `no_verify` 的 exit-2 錯誤路徑，
  約 25–40 行 + 3 個測試（隱含 HEAD 解析、`--no-verify` 離線 exit 2、顯式 HEAD 警告）。
  需注意不能破壞 mkt-046 的「local source 永不觸網」既有測試（`editor_test.go:18`）。

### C4. `marketplace package add` 在 codex 輸出開啟時的 category 閘門位置不同

上游實跑（`新文件 7.md:212-227`）：開了 `outputs.codex` 後，

```
$ apm marketplace package add emilkowalski/skills
[i] Resolved HEAD to 70744e3816f1
[x] packages must define 'category' when marketplace.outputs includes 'codex' (missing: example-package, skills)
```

`--no-verify --ref <sha>` 也一樣被擋。且 `add` **沒有** `--category` 旗標
（前份研究已證 `set.py:20-37` 無此旗標；`add` 同樣沒有）。

⇒ 使用者紀錄的原話：「嘗試加入 package 無法透過指令新增, 必須透過編輯 apm.yml 檔案」。
**這是上游的死結**：開了 codex 輸出後，CLI 無法再新增任何 package。

本專案 `internal/marketplace/authoring/schema.go:12-21` 明文記載相反的設計決定：
category 閘門「deliberately does NOT live here」，只在 compose 時（`codexmapper.go`
的 `CategoryRequiredError`）觸發，理由是 `apm pack -m claude` 不該被 codex-only 規則擋。

- 判定：**本專案行為優於上游**，且已有書面理由。不建議為了 parity 退化。
- 需要的是一個明確的 PRD 決策項：「刻意不對齊 upstream 的 add-time category 閘門」。

### C5. `marketplace check` 的輸出形狀差異

上游是 rich 表格（`新文件 7.md:94-101`），欄位為
`Status | Package | Reachable | Version Found | Ref OK | Detail`，
失敗 detail 例：`Git authentication failed during ls-remote.`

本專案 `cmd/apm-go/marketplace_authoring.go:275-297` 是 bullet list
（`ux.BulletList`）+ `pass rate: n/m (x%)` 一行。

- 判定：純呈現層差異，功能等價（前份研究已證 `--offline` 行為一致）。
- 是否要對齊表格屬 UX 決策，非缺口。

### C6. `marketplace audit <name>` 錯誤訊息缺少補救指引

上游（`新文件 7.md:169-171`）：

```
[x] Failed to audit marketplace: Marketplace 'aggregator-repo' is not registered. Run 'apm marketplace add
https://github.com/OWNER/REPO' or 'apm marketplace add OWNER/REPO' to register it, or 'apm marketplace list' to see
registered marketplaces.
```

本專案 `internal/marketplace/registry.go:248`：`marketplace %q is not registered`，
無後續補救句。

- 成本估計：一處字串擴寫 + 測試斷言。

### C7. install 失敗時的 apm.yml 交易性

上游（`新文件 7.md:135-161`）先寫 apm.yml（`[*] Updated apm.yml with 1 new package(s)`），
失敗後回滾（`[i] apm.yml restored to its previous state.`）。

本專案 `cmd/apm-go/install.go:748` / `:766` 的註解記載相反策略：
所有解析完成 **之後** 才寫（步驟 9，`install.go:1768-1778`），fail-closed，無需回滾。

- 判定：**本專案設計較佳**，無缺口。此處記錄以免日後誤判為缺漏。

---

## D. 各 target 的 marketplace/plugin 產物 schema（回應「新文件 8.txt」第 1 項）

以下皆為上游實跑產物逐字，可直接當本專案的 golden 對照。

### D1. `.claude-plugin/marketplace.json`（claude output）

`record-marketplace-repo/.claude-plugin/marketplace.json`：

```json
{
  "name": "my-marketplace",
  "owner": { "name": "acme-org", "url": "https://github.com/acme-org" },
  "plugins": [
    {
      "name": "impeccable",
      "description": "The design language that makes your AI harness better at design.",
      "category": "Productivity",
      "source": {
        "source": "github",
        "repo": "pbakaus/impeccable",
        "ref": "fc2e694afca1ac0cc384b4fe56bab3335fea7912",
        "sha": "fc2e694afca1ac0cc384b4fe56bab3335fea7912"
      }
    }
  ]
}
```

註：`category` 在 claude 輸出裡**也會被帶出**（雖然只有 codex 才強制要求）。

### D2. `.agents/plugins/marketplace.json`（codex output）

`record-marketplace-repo/.agents/plugins/marketplace.json`：

```json
{
  "name": "my-marketplace",
  "interface": { "displayName": "my-marketplace" },
  "plugins": [
    {
      "name": "impeccable",
      "source": {
        "source": "url",
        "url": "pbakaus/impeccable",
        "ref": "fc2e694afca1ac0cc384b4fe56bab3335fea7912",
        "sha": "fc2e694afca1ac0cc384b4fe56bab3335fea7912"
      },
      "policy": { "installation": "AVAILABLE", "authentication": "ON_INSTALL" },
      "category": "Productivity"
    }
  ]
}
```

- 無 `owner`、無 `description`。
- `source.source` 是字面 `"url"`，但 `url` 欄位放的是 `pbakaus/impeccable`
  這個 **owner/repo shorthand，不是真正的 URL**。這看起來是上游瑕疵，但既然是產物事實，
  對齊時要照做（否則 codex 端會讀不到）。
- 本專案 `internal/marketplace/build/codexmapper.go:26-47, :103` 的結構
  （`interface.displayName`、`policy{AVAILABLE, ON_INSTALL}`、`category`）與此一致。
- **已驗證一致**：`composeCodexSource`（`codexmapper.go:129-152`）在 default host
  且無 subdir 時產生 `{"source":"url","url":<pkg.SourceRepo 原樣>}`，
  `:127` 註解明寫「on the default host this is a bare "owner/repo" string, not a full
  URL, exactly as the Python original composes it」，`:150-151` 再補 `ref`/`sha`。
  ⇒ 與 D2 產物同形，**無缺口**。

### D3. plugin 專案的兩個 `plugin.json`（欄位不同！）

`single-plugin-repo` 同時存在兩份，內容不同：

| 檔案 | 產生者 | 內容 |
|---|---|---|
| `plugin.json`（repo 根） | `apm plugin init` | `{name, version, description, author:{name}, license:"MIT"}` |
| `.claude-plugin/plugin.json` | `apm pack` | `{name, version, description, author:{name}}` — **無 license** |
| `build/design-taste-1.0.0/plugin.json` | `apm pack`（bundle 內） | 同根目錄那份，**有 license** |

推論：`pack` 生成的 `.claude-plugin/plugin.json` 是從 `apm.yml` 重建的，而 apm.yml 沒有
`license:` 欄位（`apm pack` 也確實印了 `[!] No 'license:' field in apm.yml`），
所以 license 落空；bundle 內那份則是複製作者手上的 `plugin.json`。

- 本專案對照點：`internal/pack/pluginmanifest/write.go:16-19` 的
  `PluginEcosystemPaths` = `{claude: .claude-plugin/plugin.json, copilot: .github/plugin/plugin.json}`。
- ⚠️ **未驗證**：本專案 bundle 內是否也複製根目錄 `plugin.json`（因為本專案根本還不會產生
  這個檔案 —— 沒有 `plugin init`）。

### D4. plugin bundle 的實際內容

`single-plugin-repo/build/design-taste-1.0.0/` 只有兩個檔案：
`plugin.json` + `apm.lock.yaml`（123 KB）。
對應 stdout `[*] Packed 1 file(s) -> build/design-taste-1.0.0`。

原因：該 repo 沒有任何 plugin-native 根目錄（`agents/ skills/ commands/ ...`）；
`--dev` 裝進來的 skill 落在 `.agents/skills/` 與 `.claude/skills/`，屬部署產物不是來源。
⇒ 這反向佐證了前份研究的「plugin-native 根目錄來源警告」（`commands/init.py:224-234`）
存在的意義。

### D5. `apm.lock.yaml` 的 plugin 相關欄位

`single-plugin-repo/apm.lock.yaml:1-9`：

```yaml
lockfile_version: '1'
generated_at: '2026-07-27T23:57:33.334511+00:00'
apm_version: 0.26.0
dependencies:
- repo_url: pbakaus/impeccable
  name: impeccable
  host: github.com
  resolved_commit: 5bec5408e5ad52f44691f30639ee80d00e9713d8
  version: 4.0.2
  package_type: marketplace_plugin      # ← 值得注意
  deployed_files: [...]
```

`package_type: marketplace_plugin` — 透過 `--dev` 從 marketplace 裝進來的 plugin 型別標記。

**已查證：本專案不會輸出這個鍵。** `"package_type"` 在整個 `internal/` 只出現兩次，
都是字串字面量而非欄位值：`internal/lockfile/write.go:20`（欄位排序白名單）與
`:494`/`:501`（刻意排除清單的註解與 map）。全 repo `grep PackageType` 無任何 Go 欄位或
變數。⇒ 本專案 lockfile 的 dependency entry 沒有 `package_type` 分類。

- 影響：跨工具讀 lockfile 時，無法從本專案產物分辨 `marketplace_plugin` 與一般 apm 套件。
- 但這與 C1（`--dev` 缺席）綁在一起 —— 沒有 `--dev` 就不會有 marketplace_plugin 型別的
  dev 相依。建議併入 C1 子任務評估，不單獨處理。

---

## E. 使用者追加的研究待辦（新文件 8.txt）

原文兩行：

1. 「建立各個 target agent 的 marketplace/plugin field 的 schema」
   → D 節已從實跑產物取得 claude / codex 兩份 marketplace.json 與 plugin.json 的實際形狀。
   仍缺：copilot（`.github/plugin/plugin.json`）與其餘 target 的 marketplace 形狀
   （上游 `outputs:` 目前只支援 claude / codex 兩鍵，見 `aggregator-repo/apm.yml` 的
   template 註解 —— 所以「各個 target」在 marketplace 層面實際只有這兩個）。

2. 「使用 偽終端（Pseudo-Terminal）驗證指令與 studio」
   → 這是**驗證手段**的需求，不是功能需求。查證現況：

   - `internal/ux/ux.go:52-62`：`CanPrompt()` = `stdinIsTTY() && stderrIsTTY() && !isCI()`，
     且三個 TTY 偵測都是**可抽換的測試接縫**（`:56-62` 註解明寫 "swappable seams for tests"）。
     ⇒ 互動路徑不必真的開 PTY 就能在單元測試裡驅動。
   - PTY 函式庫現況：`creack/pty v1.1.24`、`charmbracelet/x/xpty v0.1.3`、
     `charmbracelet/x/conpty v0.1.1` 只出現在 `go.sum`，`go.mod` 的 require 區塊
     （直接與 indirect 皆）沒有它們 —— 是 huh/bubbletea 的傳遞相依，目前沒有被建置進來。
     要做真 PTY 端對端測試需新增一個直接相依。
   - Windows 主要開發平台需走 ConPTY（`charmbracelet/x/conpty`），跨平台成本高於 Linux。

   ⇒ 建議：**PRD 先用 `ux` 接縫覆蓋互動路徑**（低成本、跨平台），
   真 PTY 端對端只在接縫測不到的情境（終端跳脫序列、游標控制）才引入。
   「studio」一詞在素材裡沒有更多上下文，**未驗證**其所指為何，需向使用者確認。

---

## F. 缺口清單（依成本排序，供 PRD 取捨）

| # | 項目 | 類型 | 成本 | 來源 |
|---|---|---|---|---|
| 1 | install 教學文字 `target:` → `targets:` | 缺陷 | 極小（2 行 + 測試） | C2 |
| 2 | `readExistingTargets` 讀不到複數 `targets` | 缺陷 | 小（~10 行 + 1 測試） | 前份 3a |
| 3 | init 寫檔改複數 `targets:` | 對齊 | 小（1 行 + 測試斷言） | 前份 3b / B2 |
| 4 | `marketplace audit` 錯誤訊息補救指引 | 對齊 | 小 | C6 |
| 5 | `apm-go plugin init`（6 項 delta） | 功能缺口 | 中（複用 init.go） | 前份 結論二 |
| 6 | apm.yml 鍵序 + targets 註解區塊 | 對齊 | 中（30–50 行 + 測試） | B1 / B3 |
| 7 | plugin-native 根目錄來源警告 | 功能缺口 | 中 | 前份 結論二 |
| 8 | `package add` 隱含 HEAD 未解析 / `--no-verify` 未 exit 2 | 缺陷 | 中（25–40 行 + 3 測試） | C3 |
| 9 | 互動路徑測試覆蓋（先用 `ux` 接縫，非 PTY） | 測試基建 | 中 | E2 |
| 10 | `install --dev` / devDependencies 寫入（含 lockfile `package_type`） | 功能缺口 | **大**（貫穿 install/lock/update/uninstall） | C1 + D5 |
| — | SupportedTargets 是 6 還是 10 | **需使用者裁定**（產品決定，非技術問題） | — | B3 |
| — | 「studio」所指為何 | **需使用者澄清** | — | E2 |
| — | add-time category 閘門刻意不對齊 | 決策項（非缺口，已有書面理由） | — | C4 |
| — | codex `source.url` shorthand | 已驗證一致，非缺口 | — | D2 |
| — | `check` 表格化、install 交易性 | 非缺口（已有較佳設計 / 純 UX） | — | C5 / C7 |
