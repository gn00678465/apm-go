# Research: Plugin manifest validate command

Date: 2026-09-09. Sources: upstream microsoft/apm（GitNexus index `apm` @ 3aa03655 與本機 clone；釘住 Oracle b75a02b1 = v0.29.0；GitHub `main` tree）、Claude Code plugins-reference（`code.claude.com/docs/en/plugins-reference`，2026-09-09 讀取）、apm-go 現有程式碼。

## R-01 上游有沒有可移植的 `plugin validate`

- **Decision**: 沒有；此指令為 apm-go 獨有新增，不進 parity corpus。
- **Rationale**: 釘住 Oracle 與 `main` 的 `src/apm_cli/commands/plugin/` 都只有 `__init__.py` 與 `init.py`。上游 runtime 完全不驗證 plugin.json：`validate_plugin_package`（`deps/plugin_parser.py:1065`）只判斷「有 plugin.json 且有 name，或有任一標準元件目錄」；官方 schema 只在 `tests/unit/test_plugin_exporter_schema.py` 用 `jsonschema.Draft7Validator` 驗 `pack` 的輸出。
- **Alternatives considered**: 等上游加入再移植（不可預期）；用 `marketplace validate` 代替（它驗的是 marketplace.json 的 plugin entries，不是 plugin.json）。

## R-02 manifest 定位順序

- **Decision**: 沿用上游 `find_plugin_json`（`utils/helpers.py:105`）：`plugin.json` → `.github/plugin/plugin.json` → `.claude-plugin/plugin.json` → `.cursor-plugin/plugin.json`，取第一個存在者；apm-go 的 `internal/pack/bundle/producer.go:493-496` 已有同一清單，直接重用。
- **Rationale**: 讓 `pack`、`install` 與 `validate` 看到同一個檔案；Claude Code 自己只讀 `.claude-plugin/plugin.json`，但 apm-go 的 scaffold 與 pack 輸出都在根目錄。
- **Alternatives considered**: `.claude-plugin` 優先（我方草稿原案）— 與上游及 pack 不一致，否決。

## R-03 規則來源與已知欄位集合

- **Decision**: 已知欄位 = 官方 schema properties ∪ Claude Code 文件欄位 ∪ apm-go scaffold 的 `extensions`。型別規則依文件（`metadata`／`experimental` 非物件只 warning）；路徑前綴規則依 schema（`^\./`）；`name` 依文件（非空、無空白／控制／雙向字元）加 schema（`minLength: 1`）。
- **Rationale**: 上游 vendored schema（schemastore `claude-code-plugin.json`）的 properties 為 `$schema agents author channels commands dependencies description homepage hooks keywords license lspServers mcpServers monitors name outputStyles repository settings skills themes userConfig version`，`required = ["name"]`，`additionalProperties` 未設。文件另列 `displayName`、`defaultEnabled`、`metadata`、`experimental`、`workflows`；`extensions` 是 `plugin init --format agent-plugin` 自己寫出的（`internal/pluginjson/pluginjson.go` `ScaffoldAgent`），驗證器不得對自家 scaffold 發 warning。
- **Alternatives considered**: 只用 schema（缺文件新增欄位，會對合法 manifest 誤報）；只用文件（缺 `settings`，且路徑前綴規則只在 schema 有）。

## R-04 規則的表示方式（Decision `01M21F6D43J15FE2DFRW3F4QEP`）

- **Decision**: 手寫規則表（stdlib only）+ 讀 vendored schema 的同步測試。
- **Rationale**: 訊息要依 spec 措辭；warning 降級、拼字建議、`extensions` 都不是 schema 能表達的；`internal/pluginjson` 不需新增 runtime 外部依賴。同步測試沿用 `internal/pack/bundle/schema_sync_test.go` 的反漂移模式：規則表 ⊇ schema properties、路徑欄位 = 帶 `^\./` pattern 的欄位、required = `["name"]`。
- **Alternatives considered**: 執行期 `jsonschema/v5` 驗證（訊息要再翻譯、warning 類規則仍要另寫、多一條 runtime 依賴）。

## R-05 路徑欄位：語法還是存在性

- **Decision**: 只驗語法：必須以 `./` 開頭、不得絕對路徑、不得含 `..` 段；不讀檔案系統。
- **Rationale**: schema 的 `^\./` pattern；上游 install 的 `_map_plugin_artifacts` 對不存在、symlink、逃出 plugin 根的路徑一律靜默丟棄，絕對路徑與 `..` 是已修補的 GHSA 漏洞（`tests/unit/test_plugin_parser.py::TestPathTraversalProtection`）。存在性檢查會違反 NFR-003（只讀一個檔）且上游沒有先例。
- **Alternatives considered**: 語法 + 存在性（v2 候選，需重新裁定 NFR-003）。

## R-06 敵意輸入的上限與行為

- **Decision**: 5 MiB 上限（讀檔前 `Stat`）；非 UTF-8 → Structure error；深巢狀交給 `encoding/json` 的內建深度上限（回傳 error）；重複鍵以 `json.Decoder` token 流偵測，回報 warning「last value wins」。
- **Rationale**: 上游 `_bounded_read_json`（`deps/plugin_parser.py:39`）用 `_MAX_PLUGIN_JSON_BYTES = 5 * 1024 * 1024`，並把 `RecursionError`／`MemoryError` 轉成 `ValueError`。Go 的 `encoding/json` 對 `map[string]json.RawMessage` 解碼時重複鍵取後者，與 Python `json.loads` 相同，因此為 warning 而非 error。
- **Alternatives considered**: 無上限（fuzz 下記憶體風險）；重複鍵為 error（與 Claude Code 實際載入行為不符）。

## R-07 輸出形狀與符號

- **Decision**: 與 `marketplace validate` 同構（首行 progress、`Validation Results:`、每類一行或多行 finding、空行、`Summary: N passed, N warnings, N errors`），但符號用 PRODUCT.md 的 ` + ` ` i ` ` ! ` ` x `；exit 1 用 `withSilentExitCode`、usage 用 `withUsageError`。
- **Rationale**: `marketplace validate` 的 `[+]`／`[*]` 是為 Oracle parity 保留的例外（ticket 22）；本指令無 Oracle 對應，適用 PRODUCT.md「Brackets are never printed」的通則。
- **Alternatives considered**: 完全複製 `marketplace validate` 的 glyph（違反 C-003）。

## R-08 Gate 2（parity corpus）對無 Oracle 指令的處置

- **Decision**: 不加 parity case；輸出契約由 `tools/gate/realexec.sh` 的固定步驟（stdout 子字串 + exit code + `cmp` 只讀）承擔，並在 `cmd/apm-go/plugin.go` 的偏差註記寫明。
- **Rationale**: parity runner 需要 Oracle 側輸出；waiver 只允許 rendering 差異；pending case 是「Oracle 有、apm-go 未對齊」的語意。
- **Alternatives considered**: 見 plan.md Complexity Tracking。

## Supply-chain check

本 mission 不新增、升級或移除任何依賴（C-002）。`santhosh-tekuri/jsonschema/v5` 已在 go.mod（既有測試依賴），只在 `_test.go` 使用；vendored schema 是純 JSON 資料檔，來源 schemastore，與上游 `tests/fixtures/schemas/claude-code-plugin.schema.json` 同源，檔頭註明來源與日期。無 lifecycle script、無 registry 下載。因無安全影響的依賴決策，adversarial-squad challenge pass 明確延後（`deferred_with_rationale`：no dependency change）。
