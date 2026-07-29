# Research：各 AI Agent 的 marketplace / plugin schema 與支援度

上游 pin：`microsoft/apm@634f7b603a8c`（v0.26.0），與其他兩份 research 同版。
本專案對照：`feat/marketplace-plugin-parity` HEAD。

---

## 0. 最重要的結構事實：這是**三個互不相同的支援集合**

「支援 target X」在 apm 裡不是一件事，是三件事，且三張清單彼此**不包含**：

| 軸 | 意義 | 上游支援 | 本專案支援 | 出處 |
|---|---|---|---|---|
| **A. 部署 targets** | `apm install` 把 skill/agent/command 部署到哪個 harness 的目錄 | **10** | **6** | 上游：`_helpers.py:704-727` 產的註解（eval 產物逐字佐證）<br>本專案：`internal/manifest/target.go:25-27` + `:29-36` |
| **B. marketplace 輸出** | `apm pack` 產生哪些 `marketplace.json` 方言 | **2**：claude, codex | **2**：claude, codex | 上游：`marketplace/output_mappers.py:330-333`<br>本專案：`internal/marketplace/build/output.go:33` |
| **C. plugin.json 生態** | `apm pack` 把 `plugin.json` 寫到哪些路徑 | **2**：claude, copilot | **2**：claude, copilot | 上游：`core/plugin_manifest.py:66-69`<br>本專案：`internal/pack/pluginmanifest/write.go:16-19` |

注意 B 與 C 的**非對稱**（上游與本專案都一樣）：

- **codex** 有 marketplace 輸出，但**沒有** `plugin.json` 生態。
- **copilot** 有 `plugin.json` 生態，但**沒有** marketplace 輸出。
- 只有 **claude** 兩者都有。
- 其餘 7 個 target（antigravity, cursor, gemini, kiro, opencode, windsurf, agent-skills）
  **只有部署，沒有任何發布側 schema**。

⇒ 使用者問的「各個 AI Agent 的 marketplace/plugin schema」，實際只存在 **3 份 schema**
（claude marketplace、codex marketplace、plugin.json 的 claude/copilot 兩變體）。
其餘 agent 在發布側是空的——這不是本專案的缺口，是上游的現況。

---

## 1. 軸 A：部署 targets —— 本專案其實有**三層**不同的集合

上游註解列的 10 個（`single-plugin-repo/apm.yml` 逐字）：

```
agent-skills, antigravity, claude, codex, copilot, cursor, gemini, kiro, opencode, windsurf
```

本專案 `internal/manifest/target.go` 有三個彼此不同的集合，全部逐行讀過：

| 集合 | 位置 | 內容 | 用途 |
|---|---|---|---|
| `CanonicalTargets` | `:5-17` | copilot, claude, cursor, codex, gemini, opencode, windsurf, agent-skills, kiro, antigravity（+ `all` 偽 target） = **10 + 1** | apm.yml `targets:` 的合法詞彙（`ValidateTarget`，`:38-52`） |
| `adapterTargets` | `:29-36` | claude, codex, copilot, opencode, antigravity, agent-skills = **6** | 真的有部署 adapter（`HasAdapter`，`:54-56`） |
| `SupportedTargets` | `:25-27` | claude, codex, copilot, opencode, antigravity = **5** | 只給 `init --target` 當白名單（唯一呼叫點 `cmd/apm-go/init.go:135`/`:142`） |

**關鍵修正**：`CanonicalTargets` 已經涵蓋上游那 10 個，一個不少。
所以「apm.yml 接受哪些值」這個問題上，本專案與上游**完全一致**。

寫了沒有 adapter 的 target（cursor / gemini / kiro / windsurf）**不會失敗**，
只會拿到一個 warning：`internal/manifest/manifest.go:218-225`
→ `no registered handler for target %q`（req-tg-004），`ParseManifest` 照常回傳。
`ValidateTarget` 另外接受 `x-<vendor>-<name>` 樣式的自訂 target（`:47-48`、`:61-`，req-tg-004）。

⇒ 對 R2.2 註解區塊的意涵：「Accepted values」若照字面指「apm.yml 接受的值」，
**列 10 個才是正確的**，且不會誤導——填了會拿到明確 warning 而非失敗。
列 6 個則會漏掉本專案確實接受的 4 個值。這與先前基於「填了會失敗」的假設所做的裁定不符，
需重新確認（見第 5 節待決事項 #1）。

### 兩個附帶發現

**1a. `agent-skills` 有 adapter 卻不在 `SupportedTargets`**

`apm-go init --target agent-skills` 會被 `init.go:141-142` 拒為
"not supported by init"，但 apm.yml 手寫 `targets: [agent-skills]` 可以正常部署
（`HasAdapter("agent-skills")` 為 true）。

- **未驗證**：刻意（不由 init 引導選擇）或疏漏；`target.go:25-27` 無註解說明。
- 成本估計：若為疏漏，加進 `SupportedTargets` 是 1 行 + 1 測試；若刻意，補一行註解。

**1b. `promptTargetsOrdered` 是第四個清單**

`cmd/apm-go/init.go:17-19` 的互動 MultiSelect 選項固定為
`copilot, claude, opencode, codex, antigravity` — 5 個，與 `SupportedTargets` 同集合但不同順序，
且是各自獨立宣告的字面量。兩者漂移不會被任何測試抓到。

- 成本估計：改為由 `SupportedTargets` 推導 + 一個順序測試，約 10 行。

缺 adapter 的 4 個（cursor, gemini, kiro, windsurf）已由使用者裁定**不在本 task 補**
（`prd.md` D3 / Out of Scope）—— 這部分不受上述修正影響。

---

## 2. 軸 B：marketplace.json 的兩種方言

兩份 schema 都有 eval 實跑產物可當 golden（`record-marketplace-repo/`）。

### 2.1 Claude 方言 → `.claude-plugin/marketplace.json`

實跑產物：

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

文件層欄位（上游 `output_mappers.py:120-141`）：

| 欄位 | 條件 |
|---|---|
| `name` | 必有，經 `_sanitized_name_with_diagnostic` 正規化 |
| `description` | 只有 `config.description_overridden` 為真才輸出 |
| `version` | 只有 `config.version_overridden` 為真才輸出 |
| `owner` | 必有，`{name, email?, url?}` 三欄依序、缺的省略 |
| `metadata` | `config.metadata` 非空才輸出 |
| `plugins` | 必有 |

`source` 的四種形狀（上游 `output_mappers.py` 對應區段；
本專案 `internal/marketplace/build/mapper.go:204-234` 註解逐條對應）：

1. local → `{"source":"local","path":<entry.source>}`
2. 有 subdir → `{"source":"git-subdir","url":…,"path":…}`
3. 非預設 host → `{"source":"url","url":"https://<host>/<repo>"}`
4. 其餘 → `{"source":"github","repo":"<owner>/<repo>"}`

`ref` / `sha` 在已知時附加於任一形狀（`mapper.go:72-79` 的 `RemoteSource`，
`omitempty` 語意與 Python 的條件賦值一致）。

**判定：本專案與上游一致，無缺口。**

### 2.2 Codex 方言 → `.agents/plugins/marketplace.json`

實跑產物：

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

上游 `output_mappers.py:287-327` 的組裝順序：
`name → interface.displayName → plugins[{name, source, policy, category}]`。

與 Claude 方言的差異（全部是**刻意的**，不是欄位遺漏）：

- **無** `owner`、**無** `description`、**無** `version`、**無** `metadata`
- `interface.displayName` 用**未經 sanitize** 的 `config.name`；頂層 `name` 用 sanitize 過的
- `policy` 是固定值，不可設定
- `category` **必填**，缺就 `BuildError`（`output_mappers.py:311-314`）
- `source` 只有三種形狀，**沒有** github shorthand：
  local → `{"source":"local","path":…}`；有 subdir → `git-subdir`；其餘一律 `"url"`
- ⚠️ 預設 host 時 `source.url` 放的是 **`owner/repo` shorthand，不是真 URL**
  （上游 `_remote_source_url` 在無 `source_url`、無 `host` 時回傳 `None`，
  呼叫端 fallback 到 `pkg.source_repo`）。看起來是上游瑕疵，但**必須照做**。

**判定：本專案 `internal/marketplace/build/codexmapper.go:26-47, :103, :129-152` 與上游一致。**
`codexmapper.go:127` 的註解明寫 "on the default host this is a bare owner/repo string,
not a full URL, exactly as the Python original composes it" —— 連瑕疵都對齊了。**無缺口。**

### 2.3 `outputs:` 只接受這兩個鍵

- 上游：`MARKETPLACE_OUTPUT_MAPPERS`（`output_mappers.py:330-333`）只有 claude / codex。
- 本專案：`internal/marketplace/build/output.go:33`
  `KnownOutputFormats = map[string]bool{"claude": true, "codex": true}`，
  `:39-41` 的 switch 同樣只有兩個 case。
- 本專案 `internal/marketplace/authoring/schema.go:431-435` 的 `parseOutputs`
  同時接受 map 形式（`outputs: {claude: {}, codex: {}}`）與序列形式。

**無缺口。**

---

## 3. 軸 C：plugin.json 的兩個生態

### 3.1 輸出路徑

| 生態 | 路徑 |
|---|---|
| claude | `.claude-plugin/plugin.json` |
| copilot | `.github/plugin/plugin.json` |

上游 `core/plugin_manifest.py:66-69`；本專案 `internal/pack/pluginmanifest/write.go:16-19`。
**完全一致。**

本專案額外在 `write.go:66-68` 對 `.github/` 開頭的路徑印一行 info
（GitHub Actions 對該目錄的產生內容授予較高信任），註解說明來自上游同一契約。

### 3.2 欄位集合

由 apm.yml 合成（上游 `deps/plugin_parser.py` 的 `synthesize_plugin_json_from_apm_yml`；
本專案 `internal/pack/bundle/pluginjson.go:30-43` 的 `PluginManifest`，註解自稱 "field-for-field"）：

```
name, version, description, author, license, homepage, repository, keywords, mcpServers
```

`author` 為 `{"name": …}` 的物件形式（兩個生態皆同）。

**唯一的生態差異**（上游 `core/plugin_manifest.py:376-382`）：

| 生態 | `mcpServers` |
|---|---|
| claude | 有 `.mcp.json` 時收錄，並**剝除帶憑證的鍵**（`env / environment / headers / authorization`，`plugin_manifest.py:77-78`） |
| copilot | **一律省略**（不屬於 Copilot plugin manifest schema） |

本專案 `internal/pack/bundle/pluginjson.go:39-42` 的註解逐字對應
（"only set for the claude ecosystem; nil/empty means omit the mcpServers key entirely"）。

另一條共同規則：`agents / skills / commands / instructions` 這四個慣例目錄鍵
**永遠從 manifest 移除**（`plugin_manifest.py:373-375`），因為 host 會自動探索。

### 3.3 覆寫政策

上游 `plugin_manifest.py:406-413`：既有 `plugin.json` 預設**保留不覆寫**（印警告、跳過），
只有 `apm pack --force` 才覆蓋。理由是防止被竄改的 `.mcp.json` 悄悄取代人工審過的檔案。

本專案 `internal/pack/pluginmanifest/write.go:26-33`、`:58-64` 完全相同的三態行為
（dry-run / 存在且無 force / 存在且 force）。**無缺口。**

### 3.4 三份 plugin.json 的欄位差異（eval 實測，容易誤判為 bug）

`single-plugin-repo` 同時存在三份，**內容不同是正確的**：

| 檔案 | 產生者 | 來源 | license |
|---|---|---|---|
| `plugin.json`（repo 根） | `apm plugin init` | 樣板常數 | **有**（硬寫 `"MIT"`） |
| `.claude-plugin/plugin.json` | `apm pack` | 從 apm.yml **重新合成** | **無**（apm.yml 沒有 `license:` 欄位） |
| `build/<name>-<ver>/plugin.json` | `apm pack`（bundle 內） | **複製磁碟上的** `plugin.json` | **有** |

差異來源：`build_plugin_manifest` 的 docstring（`plugin_manifest.py:366-369`）明寫
「intentionally do NOT consult an on-disk plugin.json here (unlike
find_or_synthesize_plugin_json, which is the disk-first reader used by the bundle exporter)」。

⇒ 生成路徑是 apm.yml-first，bundle 匯出路徑是 disk-first。這解釋了 license 的有無。
`apm pack` 也確實印了 `[!] No 'license:' field in apm.yml`。

**對本專案的意涵**：`plugin init` 會在根目錄寫死 `"license": "MIT"`，但不寫進 apm.yml，
所以 `pack` 之後 `.claude-plugin/plugin.json` 會沒有 license 且印警告。
這是上游的既有行為，PRD 的 R3.3.d 照做即可，**不要**順手把 license 也寫進 apm.yml
（那會偏離上游且改變 SBOM 行為）。

---

## 4. 支援度總表（供 PRD / design.md 引用）

apm-go 的 A 軸拆成三欄，因為它有三層不同的集合（見第 1 節）：

| Agent | 上游部署 | apm.yml 收（`CanonicalTargets`） | 真能部署（`adapterTargets`） | `init --target` 收（`SupportedTargets`） | B. marketplace 輸出 | C. plugin.json |
|---|---|---|---|---|---|---|
| claude | ✅ | ✅ | ✅ | ✅ | ✅ `.claude-plugin/marketplace.json` | ✅ `.claude-plugin/plugin.json`（含 mcpServers） |
| codex | ✅ | ✅ | ✅ | ✅ | ✅ `.agents/plugins/marketplace.json` | ❌ |
| copilot | ✅ | ✅ | ✅ | ✅ | ❌ | ✅ `.github/plugin/plugin.json`（無 mcpServers） |
| opencode | ✅ | ✅ | ✅ | ✅ | ❌ | ❌ |
| antigravity | ✅ | ✅ | ✅ | ✅ | ❌ | ❌ |
| agent-skills | ✅ | ✅ | ✅ | ⚠️ **否**（見 1a） | ❌ | ❌ |
| cursor | ✅ | ✅ | ❌ warning | ❌ | ❌ | ❌ |
| gemini | ✅ | ✅ | ❌ warning | ❌ | ❌ | ❌ |
| kiro | ✅ | ✅ | ❌ warning | ❌ | ❌ | ❌ |
| windsurf | ✅ | ✅ | ❌ warning | ❌ | ❌ | ❌ |

- 「❌ warning」= apm.yml 收得下，但 `ParseManifest` 回一個 req-tg-004 warning
  `no registered handler for target ...`（`internal/manifest/manifest.go:218-225`），不阻斷。
- B 欄與 C 欄：**上游與 apm-go 完全一致**，逐格皆已於本檔第 2、3 節以 file:line 對照。
- 落差全部集中在 A 軸的後兩欄（adapter 覆蓋率與 init 白名單），apm.yml 詞彙層無落差。

---

## 5. 本節產出的待決事項

| # | 事項 | 類型 |
|---|---|---|
| 1 | **D3 需重新裁定**：註解的 Accepted values 要列 10（apm.yml 實際接受的，與上游同）還是 6（有 adapter 的） | 先前裁定「列 6」是基於「填了會失敗」的錯誤前提；實際只會 warning（`manifest.go:218-225`） |
| 2 | `agent-skills` 在 `adapterTargets` 但不在 `SupportedTargets` | **未驗證**是刻意或疏漏；成本 1 行 + 1 測試（若補）或 1 行註解（若刻意） |
| 3 | `promptTargetsOrdered`（`init.go:17-19`）與 `SupportedTargets` 是兩份獨立字面量，漂移無測試防護 | 成本約 10 行 |
| 4 | cursor / gemini / kiro / windsurf 的部署 adapter | 已裁定 Out of Scope（PRD D3），不受 #1 影響 |
| 5 | `plugin init` 寫死 `license: "MIT"` 但不寫進 apm.yml | 照上游做，PRD R3.3.d 已涵蓋；**不要**自作主張改善 |
