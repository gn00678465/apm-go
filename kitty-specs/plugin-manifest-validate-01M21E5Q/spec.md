# Mission Specification: Plugin manifest validate command

**Mission Branch**: `feat/plugin-validate`  
**Created**: 2026-09-09  
**Status**: Draft  
**Input**: User description: "為 apm-go 新增 `plugin validate`，關閉 issue #13 的第二項；規則依 Claude Code plugins-reference 與官方 plugin manifest schema；上游 apm（釘住 v0.29.0 與 main）沒有此指令，屬 apm-go 獨有新增。"

## Intent Summary (confirmed)

- Actor：plugin 作者（人或 CI）。Trigger：在 plugin 目錄執行 `apm-go plugin validate [path] [--strict] [-v]`。
- Outcome：依官方 schema 與 Claude Code 文件回報 Structure / Name / Fields / Paths / Unrecognized 五類結果與 Summary；exit 0 / 1 / 2。
- 恆真規則：只讀；manifest 探測順序同上游 `find_plugin_json`；apm-go 自己兩種 scaffold 零 warning；5 MiB 上限；任何輸入不崩潰。
- 邊界：路徑欄位只驗語法不驗存在；不掛在 `pack` 或 `init` 之前。
- 三個 Decision Moment（均已 resolved）：主要情境 = 手動 pre-pack 檢查 + CI `--strict`；四項恆真規則全數確認；路徑欄位 = 語法檢查。

## User Scenarios & Testing *(mandatory)*

### User Story 1 - 發布前確認 manifest 會被 Claude Code 接受 (Priority: P1)

plugin 作者在自己的 plugin 目錄執行 `apm-go plugin validate`，在 `pack` 或發布前得知 `plugin.json` 是否符合 Claude Code 的 manifest 規則：必要欄位、欄位型別、路徑欄位寫法，以及哪些欄位 Claude Code 不認得。

**Why this priority**: 這是 issue #13 尚未完成的那一半；沒有它，手改過的 manifest 要等 Claude Code 載入失敗才會被發現。

**Independent Test**: 對 `apm-go plugin init` 產生的兩種 scaffold 執行驗證，得到零錯誤零警告與 exit 0；對故意寫壞的 manifest 得到對應的錯誤行與 exit 1。

**Acceptance Scenarios**:

1. **Given** `plugin init demo --yes --target claude` 的輸出目錄，**When** 執行 `plugin validate demo`，**Then** 每個檢查類別印出 passed，Summary 為 0 warnings、0 errors，exit 0。
2. **Given** `plugin init --format agent-plugin` 的輸出（含 `$schema` 與 `extensions`），**When** 執行驗證，**Then** 同樣 0 warnings、0 errors。
3. **Given** manifest 內容為 `{}`，**When** 執行驗證，**Then** Name 類別印出 `missing required field 'name'`，Summary 有 1 error，exit 1。
4. **Given** manifest 為 `{"name":"x","keywords":"a"}`，**When** 執行驗證，**Then** Fields 類別印出 `'keywords' must be an array of strings`，exit 1。
5. **Given** manifest 為 `{"name":"x","skills":"skills/"}`（缺 `./` 前綴），**When** 執行驗證，**Then** Paths 類別印出 `'skills' must start with './'`，exit 1。
6. **Given** manifest 為 `{"name":"x","commands":"../outside/x.md"}` 或絕對路徑，**When** 執行驗證，**Then** Paths 類別印出該欄位不得逃出 plugin 根目錄，exit 1。
7. **Given** 目錄內沒有任何候選位置的 plugin.json，**When** 執行驗證，**Then** 印出 `no plugin.json found in <dir>` 並列出四個候選相對路徑，exit 1。
8. **Given** `path` 直接指向一個 JSON 檔，**When** 執行驗證，**Then** 直接驗證該檔，不做目錄探測。

---

### User Story 2 - CI 以嚴格模式擋下拼錯與殘留欄位 (Priority: P2)

CI 對 plugin 倉庫執行 `apm-go plugin validate --strict`，把「Claude Code 會忽略但作者多半打錯」的未識別欄位，以及非物件的 `metadata` / `experimental`，從警告升級為失敗。

**Why this priority**: Claude Code 的規則是未識別欄位只警告、plugin 仍會載入；作者在本機不會察覺打錯的欄位名，只有 CI 的嚴格模式能擋下。

**Independent Test**: 同一個含 `descripton` 欄位的 manifest，不加 `--strict` 得 exit 0 與 1 warning；加 `--strict` 得 exit 1，Summary 的計數不變。

**Acceptance Scenarios**:

1. **Given** manifest 為 `{"name":"x","descripton":"d"}`，**When** 執行驗證，**Then** Unrecognized 類別印出 `unrecognized field 'descripton' (did you mean 'description'?)`，Summary 1 warning、0 errors，exit 0。
2. **Given** 同一 manifest，**When** 加 `--strict`，**Then** Summary 仍為 1 warning、0 errors，exit 1。
3. **Given** manifest 為 `{"name":"x","publisher":"p"}`，**When** 執行驗證，**Then** 警告不附建議（沒有距離 2 以內的已知欄位）。
4. **Given** manifest 為 `{"name":"x","metadata":"a","experimental":[]}`，**When** 執行驗證，**Then** Fields 類別印出 2 個 warning（Claude Code 忽略非物件值），exit 0。
5. **Given** manifest 為 `{"name":"x","experimental":{"themes":1,"foo":1}}`，**When** 執行驗證，**Then** `experimental.themes` 型別錯誤為 error，`experimental.foo` 為 unrecognized warning。

---

### User Story 3 - 敵意或損壞的 manifest 得到明確診斷而非崩潰 (Priority: P3)

作者或自動化流程把損壞、過大、深巢狀或非 UTF-8 的檔案丟給驗證器時，得到一行 Structure 錯誤與 exit 1，其他檢查不列出；驗證過程不寫入、不修改任何檔案。

**Why this priority**: 驗證器會被用在 CI 與不受信任的第三方 plugin 上；崩潰或誤寫檔比誤報更糟。

**Independent Test**: 對 `{`、`[]`、空檔、含 0xFF 位元組、1 MiB 的 `[[[[…`、6 MiB 檔各執行一次，全部得到 Structure error 與 exit 1，且目錄快照前後位元相同。

**Acceptance Scenarios**:

1. **Given** 內容為 `{`，**When** 執行驗證，**Then** Structure 類別印出 `invalid JSON: …`，Name / Fields / Paths / Unrecognized 不出現，Summary 0 passed、0 warnings、1 error，exit 1。
2. **Given** 內容為 `[]`，**When** 執行驗證，**Then** Structure 印出 `top-level value must be an object`，exit 1。
3. **Given** 檔案超過 5 MiB，**When** 執行驗證，**Then** Structure 印出檔案超過大小上限，不讀取內容，exit 1。
4. **Given** 內容為 `{"name":"a","name":"b"}`，**When** 執行驗證，**Then** Structure 印出 `duplicate key 'name' (last value wins)` 為 warning，exit 0。
5. **Given** 任一上述輸入，**When** 驗證結束，**Then** 目標目錄的檔案樹與每個檔案的位元組與驗證前相同。

---

### Edge Cases

- `name` 為空字串、含空白、含控制字元、含雙向格式字元（U+200E/U+200F/U+202A–U+202E/U+2066–U+2069）→ 各自一個 Name error。
- `name` 非 kebab-case（例如 `My_Plugin`）→ Name warning，不是 error（官方 schema 只要求非空字串，文件說「prefer kebab-case」）。
- 目錄同時有多個候選位置的 plugin.json → 依上游 `find_plugin_json` 的順序取第一個：`plugin.json`、`.github/plugin/plugin.json`、`.claude-plugin/plugin.json`、`.cursor-plugin/plugin.json`；輸出第一行印出被選中的相對路徑。
- `dependencies` 元素為數字 → error；元素為缺 `name` 的物件 → error。
- `author` 存在但 `author.name` 非字串 → error。
- `hooks` / `mcpServers` / `lspServers` 為 inline 物件 → 合法，不檢查內容。
- 頂層 `themes` / `monitors`：官方 schema 允許，Claude Code 文件建議移到 `experimental` 之下 → warning 提示搬移。
- 兩個以上位置參數 → usage error，exit 2。
- `--verbose` → 在結果前列出 manifest 內出現的已知欄位，每行一個，依檔案順序。

## Requirements *(mandatory)*

### Functional Requirements

| ID | Title | User Story | Priority | Status |
|----|-------|------------|----------|--------|
| FR-001 | 指令與參數 | As a plugin author, I want `apm-go plugin validate [path] [--strict] [-v\|--verbose]` with `path` defaulting to the current directory so that I can validate any plugin checkout. | High | Open |
| FR-002 | manifest 定位 | As a plugin author, I want a directory `path` probed in the upstream order `plugin.json`, `.github/plugin/plugin.json`, `.claude-plugin/plugin.json`, `.cursor-plugin/plugin.json`, and a file `path` validated directly, so that the same manifest apm pack and Claude Code read is the one validated. | High | Open |
| FR-003 | Structure 檢查 | As a plugin author, I want invalid-UTF-8, invalid-JSON, and non-object manifests reported as one Structure error that suppresses the other checks, so that a broken file gives one clear diagnosis. 定位成功之後的讀取失敗（不是普通檔案、逃出指定路徑、超過 5 MiB、作業系統錯誤）不走 Structure，而是 contracts/cli-plugin-validate.md 的 `could not read` 一族：只印該行、無 Results 與 Summary、exit 1。 | High | Open |
| FR-004 | Name 檢查 | As a plugin author, I want a missing, non-string, empty, whitespace-, control-, or bidi-containing `name` reported as an error and a non-kebab-case name as a warning, so that the plugin can be namespaced by Claude Code. | High | Open |
| FR-005 | Fields 型別檢查 | As a plugin author, I want every recognized field checked against its documented type (string / object / boolean / array of strings / string-or-array / string-array-or-object / dependency list), with non-object `metadata` and `experimental` as warnings and every other mismatch as an error, so that a manifest Claude Code would refuse to load fails here first. | High | Open |
| FR-006 | Paths 語法檢查 | As a plugin author, I want each path-typed field value (`skills`, `commands`, `agents`, `workflows`, `hooks`, `mcpServers`, `outputStyles`, `lspServers`, `themes`, `monitors`, `experimental.themes`, `experimental.monitors` when given as strings or string arrays) required to start with `./`, not be absolute, and contain no `..` segment, so that manifests the official schema rejects or that upstream drops as traversal attempts are caught without touching the filesystem. | High | Open |
| FR-007 | Unrecognized 檢查 | As a plugin author, I want top-level and `experimental.*` keys outside the recognized set reported as warnings, with a `(did you mean 'X'?)` hint when a recognized key is within edit distance 2, so that typos are visible without blocking load. | High | Open |
| FR-008 | 嚴格模式 | As a CI maintainer, I want `--strict` to make any warning fail the run (exit 1) while leaving the printed counts unchanged, so that CI can gate on manifest hygiene. | High | Open |
| FR-009 | 輸出格式 | As a plugin author, I want a `Validating plugin '<relative manifest path>'...` line, then per-check lines using the project status symbols, then `Summary: N passed, N warnings, N errors`, so that output reads like `marketplace validate`. | Medium | Open |
| FR-010 | Exit codes | As a CI maintainer, I want exit 0 on success, 1 on any error (or any warning under `--strict`) with no extra line after Summary, and 2 on usage errors, so that scripts can branch on the result. | High | Open |
| FR-011 | Verbose | As a plugin author, I want `-v` to list the recognized fields present in the manifest before the results, so that I can see what was actually checked. | Low | Open |
| FR-012 | Help surface | As a user, I want `plugin --help` to list `init` and `validate`, and `plugin validate --help` to document `--strict` and `-v, --verbose`, so that the command is discoverable. | Medium | Open |

### Non-Functional Requirements

| ID | Title | Requirement | Category | Priority | Status |
|----|-------|-------------|----------|----------|--------|
| NFR-001 | 只讀 | 驗證前後，目標目錄的檔案樹與每個檔案的位元組完全相同；驗證器不建立、修改、刪除任何檔案（以快照比對驗證）。 | Security | High | Open |
| NFR-002 | 不崩潰 | 對任意輸入位元組（fuzz 至少 30 秒且 seed 涵蓋所有 Structure 情境）驗證器不 panic、不耗盡記憶體；超過 5 MiB 的檔案在讀取內容前即拒絕。 | Reliability | High | Open |
| NFR-003 | 讀取範圍 | 驗證器只讀取被選中的那一個 manifest 檔；不跟隨路徑欄位、不讀取父目錄、不跟隨 symlink 到 `path` 之外。 | Security | High | Open |
| NFR-004 | 零誤報 | apm-go 自己兩種 `plugin init` 格式的輸出必須 0 warnings、0 errors；只含已知欄位且型別正確的任意 manifest（property test 至少 100 個隨機樣本）必須 0 warnings、0 errors。 | Correctness | High | Open |
| NFR-005 | 回應時間 | 5 MiB 以內的 manifest 在 1 秒內完成驗證。 | Performance | Low | Open |

### Constraints

| ID | Title | Constraint | Category | Priority | Status |
|----|-------|------------|----------|----------|--------|
| C-001 | 既有輸出契約不變 | `plugin init`、`pack`、`marketplace validate` 的 stdout / stderr / exit code / 檔案樹位元不變；`tools/parity` 維持 96 case、0 unwaived。 | Technical | High | Open |
| C-002 | 無新依賴 | 不新增任何 module 依賴；不新增 ARCHITECTURE.md 依賴圖上沒有的 import 邊。 | Technical | High | Open |
| C-003 | 狀態符號詞彙 | 輸出使用 PRODUCT.md 的單一狀態符號詞彙，不印括號。 | Technical | High | Open |
| C-004 | 規則來源 | 已知欄位集合 = 官方 schema（schemastore `claude-code-plugin.json`，上游 vendored 副本）∪ Claude Code plugins-reference 文件欄位 ∪ apm-go 自己 scaffold 寫出的 `extensions`；型別規則以文件為準，路徑前綴規則以 schema 為準。 | Business | High | Open |
| C-005 | 偏差紀錄 | 此指令為 apm-go 獨有新增（上游 v0.29.0 與 main 的 `plugin` 指令群只有 `init`）；`cmd/apm-go/plugin.go` 的偏差註記、PRODUCT.md 指令面、ARCHITECTURE.md、README（en / zh-TW）在同一變更內更新。 | Technical | High | Open |
| C-006 | 不掛在 pack 之前 | v1 不在 `pack` 或 `init` 內自動執行驗證（上游無此先例）。 | Business | Medium | Open |
| C-007 | 路徑存在性不在範圍 | v1 只驗路徑欄位的語法，不驗檔案是否存在（上游 install 對不存在的路徑靜默丟棄）。 | Business | Medium | Open |

### Key Entities

- **Manifest**: 一個 plugin.json 文件；屬性為其頂層鍵與值、所在相對路徑、大小。
- **Finding**: 一筆驗證結果；屬性為檢查類別（Structure / Name / Fields / Paths / Unrecognized）、等級（error / warning）、訊息。
- **Summary**: 通過的類別數、warning 數、error 數；決定 exit code 的唯一依據（配合 `--strict`）。

## Success Criteria *(mandatory)*

### Measurable Outcomes

- **SC-001**: apm-go 自己產生的兩種 plugin scaffold 以驗證器檢查得到 0 warnings、0 errors、exit 0。
- **SC-002**: 本規格列出的每一個 acceptance scenario 與 edge case 各有一個同名自動化測試，且全部通過。
- **SC-003**: 對不合法輸入集合（invalid JSON、非物件、空檔、非 UTF-8、深巢狀）驗證器 100% 以 Structure error 結束，0 次 panic；超大檔由 CLI 在讀取前以 `could not read` 拒絕，不進入驗證器。
- **SC-004**: 驗證前後目標目錄快照 100% 相同。
- **SC-005**: 既有輸出契約：parity gate 96 case、0 unwaived；`go test ./...` 全綠。
- **SC-006**: issue #13 的兩項需求（`plugin init`、`plugin validate`）皆可由使用者在同一個 release 內執行。
