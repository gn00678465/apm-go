# Implementation Plan: Plugin manifest validate command

**Branch**: `feat/plugin-validate` | **Date**: 2026-09-09 | **Spec**: [spec.md](spec.md)
**Input**: Feature specification from `/kitty-specs/plugin-manifest-validate-01M21E5Q/spec.md`

**Note**: This template is filled in by the `/spec-kitty.plan` command. See `packs/built-in/missions/mission-steps/software-dev/plan/prompt.md` for the execution workflow.

Planning questions answered: rule representation = hand-coded rule table in `internal/pluginjson` plus a schema sync test (Decision `01M21F6D43J15FE2DFRW3F4QEP`, resolved). No other planning question was open: language, dependency policy, project structure, and test approach are fixed by AGENTS.md / ARCHITECTURE.md and the confirmed spec.

## Summary

新增 `apm-go plugin validate [path] [--strict] [-v]`：只讀地定位 plugin.json（上游 `find_plugin_json` 的四個候選位置，或直接指定檔案），依 Claude Code plugins-reference 與官方 schema 檢查 Structure / Name / Fields / Paths / Unrecognized 五類，印出與 `marketplace validate` 同構的結果與 Summary，exit 0 / 1 / 2。驗證邏輯是一張手寫規則表（stdlib only）放在 `internal/pluginjson`，由一個讀 vendored 官方 schema 的同步測試防止欄位集合漂移；指令層只做定位、輸出與 exit code。

## Technical Context

**Language/Version**: Go 1.27（go.mod 既有；本機 go1.27.0 windows/amd64，CI ubuntu）
**Primary Dependencies**: stdlib only for the new code（`encoding/json`、`unicode`、`unicode/utf8`、`os`、`path`）；CLI 走既有 `github.com/spf13/cobra`；輸出走既有 `internal/ux`。`github.com/santhosh-tekuri/jsonschema/v5` 已在 go.mod，只在新的 `_test.go` 內讀 vendored schema 做同步斷言，不進 runtime import。
**Storage**: 檔案系統唯讀：只讀取被選中的那一個 plugin.json（≤ 5 MiB）；不寫任何檔案。
**Testing**: `go test ./...`；表驅動測試（每個 spec scenario 一個同名子測試）、`testing/quick` property（只含已知欄位且型別正確的物件 → 0 findings）、Go native fuzz（`FuzzValidateBytes`，seed 涵蓋所有 Structure 情境）、目錄快照比對（NFR-001）、`cmd/apm-go` 既有的 cobra 端到端測試模式。`tools/gate.sh` 在 WP 完成後跑（charter gate 3），`tools/gate/realexec.sh` 新增 happy 2 + adversarial 4 步。
**Target Platform**: 與 apm-go 相同：linux / darwin / windows 單一靜態二進位；測試在 Windows 與 ubuntu CI 都要綠。
**Project Type**: single（既有 Go module；新檔案落在 `internal/pluginjson` 與 `cmd/apm-go`）
**Performance Goals**: 5 MiB 以內的 manifest 在 1 秒內完成（NFR-005）；規則表為 O(欄位數)，拼字建議為 O(未識別鍵 × 已知鍵 × 鍵長²)，上限微小。
**Constraints**: 只讀（NFR-001）；不 panic（NFR-002）；不新增 module 依賴或 ARCHITECTURE.md §1 沒有的 in-module import 邊（C-002）；既有輸出契約不變、parity 96/0（C-001）；PRODUCT.md 狀態符號（C-003）；偏差註記與四份文件同變更更新（C-005）。
**Scale/Scope**: 一個新子指令、一個新的驗證器（約 300–400 行）、約 40 個測試情境、6 個 realexec 步驟、4 份文件更新。

## Charter Check

*GATE: Must pass before Phase 0 research. Re-check after Phase 1 design.*

Charter（`.kittify/charter/charter.md`）與三份 canonical documents 對照：

| Gate | 狀態 | 依據 |
|---|---|---|
| Go 單一 module、cobra under `cmd/apm-go`、libraries under `internal/`；無 Makefile | PASS | 新檔案：`cmd/apm-go/plugin_validate.go`、`internal/pluginjson/validate.go`；不加 script |
| 終端輸出走 `internal/ux`，PRODUCT.md 單一狀態詞彙、不印括號 | PASS | 指令層用 `ux.Progress` / `ux.Success` / `ux.Info` / `ux.Warn` / `ux.Error`；不用 `marketplace validate` 為 Oracle parity 保留的 `ux.Gear` / `ux.Check` `[+]` 形式 |
| TDD：每個行為先寫失敗測試，透過既有入口證明 RED | PASS（流程約束） | 每個 WP 先 commit 測試、觀察 RED，再實作；evidence 報告記 RED |
| 測試用 `t.TempDir()`、inline fixture、注入 seam、不全域 mock | PASS | 驗證器是純函式 `Validate(bytes) Report`；檔案定位與讀取用 `os` 直接對 TempDir 操作，不需 seam |
| `cmd/apm-go` 的 `TestMain` 已 pin CI 變數 | PASS | 新測試落在既有套件；`internal/pluginjson` 不讀 CI 變數 |
| Gate 1：`go build ./...`、`go test ./...` 綠 | PASS（WP 完成條件） | |
| Gate 2：輸出契約；任何使用者可見輸出／exit code／產生檔的變更要加或改 corpus case | **PASS（charter 例外條款）** | 這是 apm-go 獨有指令，Oracle 沒有對應命令，`tools/parity` 無法產生 Oracle 側輸出。使用者裁定（2026-09-09，選項 A，原文見 `.scratch/parity-runner/issues/34-oracle-less-command-output-contract.md`）：不加 parity case；以 `tools/gate/realexec.sh` 固定 stdout / exit code 為輸出契約，`cmd/apm-go/plugin.go` 的偏差註記引用 ticket 34，PR 描述引用裁定原文。既有 96 case 必須維持 0 unwaived。裁定已寫入 charter：Quality Gate 2 與 Exception Policy 新增「釘住 Oracle 沒有的 apm-go 獨有指令」這一類例外，限定於 ticket 指名的指令，並要求 realexec 的驗證強度與 corpus 相同（完整 stdout、stderr、exit code、檔案樹）。 |
| Gate 3：verification-gate 每個 WP 附 evidence 報告 | PASS（WP 完成條件） | `sh tools/gate.sh`，scope `plugin-manifest-validate` |
| GitNexus `impact` before editing any symbol；`detect_changes` before commit | PASS（流程約束） | 觸及的既有符號只有 `pluginCmd`（加子指令）；新符號無 upstream caller |
| ARCHITECTURE.md §1 依賴規則：新 import 邊要裁定 | PASS | `cmd/apm-go → pluginjson` 已存在；`pluginjson` 新增程式碼只 import stdlib；測試檔 import `jsonschema/v5`（測試依賴不在 §1 圖內，與 `pack/bundle/schema_sync_test.go` 相同先例） |
| 每個 mission 走 worktree lane、只經 PR 進 main | PASS | spec-kitty lanes；PR 先以 `feat/marketplace-plugin-parity` 為 base，#18 合併後改 `main` |
| AGENTS.md：未記錄的偏差是 finding | PASS | C-005：`plugin.go` 註記由「exactly one child (AC30)」改為記錄 #13 與本 mission |

Gate 2 對無 Oracle 對應指令的適用方式已依 charter Amendment Process 收進 charter 本文（`.kittify/charter/charter.md` 的 Quality Gates 與 Exception Policy，來源 `interview/answers.yaml`），並同步到 PRODUCT.md 的 Output contract 與 Terminology；ticket 34 保存裁定原文與驗證強度。NFR-005（1 秒）只有理由性論證，沒有可執行的計時檢查，屬有意的 rationale-only 覆蓋（Low priority）。

## Project Structure

### Documentation (this mission)

```
kitty-specs/plugin-manifest-validate-01M21E5Q/
├── plan.md              # This file (/spec-kitty.plan command output)
├── research.md          # Phase 0 output (/spec-kitty.plan command)
├── data-model.md        # Phase 1 output (/spec-kitty.plan command)
├── quickstart.md        # Phase 1 output (/spec-kitty.plan command)
├── contracts/           # Phase 1 output (/spec-kitty.plan command)
│   └── cli-plugin-validate.md
└── tasks.md             # Phase 2 output (/spec-kitty.tasks command - NOT created by /spec-kitty.plan)
```

### Source Code (repository root)

```
cmd/apm-go/
├── plugin.go                  # 既有：pluginCmd 加 AddCommand(pluginValidateCmd())；偏差註記改寫（C-005）
├── plugin_validate.go         # 新：cobra 子指令、manifest 定位、輸出、exit code（FR-001/002/008/009/010/011/012）
└── plugin_validate_test.go    # 新：端到端情境（US1/US2/US3 每個 acceptance scenario 一個子測試；--help；read-only 快照）

internal/pluginjson/
├── pluginjson.go              # 既有：scaffold（不改）
├── validate.go                # 新：Validate(bytes) Report、規則表、name 檢查、路徑語法、拼字建議（FR-003–007）
├── validate_test.go           # 新：表驅動單元測試 + testing/quick property（NFR-004）
├── validate_fuzz_test.go      # 新：FuzzValidateBytes（NFR-002）
├── validate_schema_sync_test.go  # 新：規則表 ⊇ vendored schema properties；路徑欄位 = schema 帶 ^\./ pattern 的欄位
└── testdata/
    └── claude-code-plugin.schema.json  # 新：schemastore 官方 schema 副本（測試用，與上游 tests/fixtures/schemas 同源）

tools/gate/
├── realexec.sh                # 既有：新增 happy 2（scaffold claude / agent-plugin 各驗一次）+ adversarial 4（invalid JSON、traversal path、typo --strict、缺 manifest）
└── mutants.txt                # 既有：新增 2 個 mutant（型別分支反轉；strict 不升級）

PRODUCT.md / ARCHITECTURE.md / README.md / README.zh-TW.md   # C-005 文件更新
```

**Structure Decision**: 沿用 ARCHITECTURE.md §2 的分層：驗證邏輯是純函式，放進已擁有 plugin.json 知識的 `internal/pluginjson`（只依賴 `bundle`；新程式碼只用 stdlib，不新增 in-module 邊）；定位、輸出、exit code 留在 `cmd/apm-go`，與其他指令一致（§1「Terminal output is emitted by cmd/apm-go…」）。不新建套件：`internal/pluginjson` 的名稱與職責（plugin.json）正好涵蓋驗證，另開套件只會多一條需裁定的邊。

## Complexity Tracking

*Fill ONLY if Charter Check has violations that must be justified*

| Violation | Why Needed | Simpler Alternative Rejected Because |
|-----------|------------|-------------------------------------|
| Gate 2（parity corpus）無法為此指令加 case | Oracle 沒有 `plugin validate`，parity runner 需要 Oracle 側輸出才能比較 | 「用 waiver 加一個永遠差異的 case」不成立（waiver 只允許 rendering 差異）；「用 pending case」也不對（pending 是 Oracle 有、apm-go 尚未對齊的案例）。改用 realexec 固定 stdout / exit code，並在偏差註記寫明 |

## Implementation Concern Map

*Include this section when the mission has multiple distinct architectural areas that inform how tasks are decomposed.*

> **Note**: Implementation concerns are NOT work packages and are NOT executable units.
> `/spec-kitty.tasks` translates these into executable WPs — one concern may become
> multiple WPs; multiple small concerns may merge into one WP. Do not label concerns
> with WP-style IDs or sequencing language.

### IC-01 — Validator core（規則表、五類檢查、Report 模型）

- **Purpose**: 一個純函式 `Validate(data []byte) Report`，輸入 manifest 位元組、輸出 findings 與 summary；所有規則集中在一張表，讓每條規則可被單元測試與 mutant 逐一命中。
- **Relevant requirements**: FR-003、FR-004、FR-005、FR-006、FR-007、NFR-002、NFR-004、C-004
- **Affected surfaces**: `internal/pluginjson/validate.go`、`validate_test.go`、`validate_fuzz_test.go`
- **Sequencing/depends-on**: none
- **Risks**: Structure 階段需處理非 UTF-8（`utf8.Valid`）、重複鍵（`json.Decoder` token 流掃描一次；值本身用 `map[string]json.RawMessage` 解）與深巢狀（`encoding/json` 自身有 10000 層上限，回傳 error 不 panic，fuzz 驗證）；5 MiB 上限在讀檔前以 `Stat` 判斷（屬 IC-02）。已知欄位集合 = schema ∪ 文件 ∪ `extensions`，要在規則表旁註明每個欄位的來源。

### IC-02 — CLI 子指令（定位、輸出、exit code、flags）

- **Purpose**: 把 Report 渲染成與 `marketplace validate` 同構、但使用 PRODUCT.md 符號的輸出，並實作 manifest 定位順序、`--strict`、`-v`、exit 0/1/2。
- **Relevant requirements**: FR-001、FR-002、FR-008、FR-009、FR-010、FR-011、FR-012、NFR-001、NFR-003、NFR-005、C-003
- **Affected surfaces**: `cmd/apm-go/plugin_validate.go`、`plugin_validate_test.go`、`cmd/apm-go/plugin.go`（AddCommand + 偏差註記）
- **Sequencing/depends-on**: IC-01（需要 Report 型別）
- **Risks**: 定位順序必須與 `internal/pack/bundle/producer.go:493-496` 的候選清單一致（同一來源：上游 `find_plugin_json`），建議直接重用該清單而非再抄一份；`path` 為 symlink 指向 `path` 之外時只讀不跟隨（NFR-003）需在 `Lstat` 層決定；exit 1 走 `withSilentExitCode`，usage 走 `withUsageError`（`cobra.MaximumNArgs(1)` 的錯誤要對映到 exit 2）。

### IC-03 — Schema 同步與規則來源證據

- **Purpose**: 把 C-004「規則來源」變成可執行的反漂移測試：規則表的已知欄位 ⊇ vendored schema 的 `properties`，路徑欄位集合 = schema 中帶 `^\./` pattern 的欄位，`required` = `["name"]`。
- **Relevant requirements**: C-004、SC-002
- **Affected surfaces**: `internal/pluginjson/validate_schema_sync_test.go`、`internal/pluginjson/testdata/claude-code-plugin.schema.json`
- **Sequencing/depends-on**: IC-01
- **Risks**: schema 副本的來源與日期要寫在檔頭註解（schemastore `claude-code-plugin.json`，與上游 `tests/fixtures/schemas/` 同源）；`jsonschema/v5` 只能出現在 `_test.go`。

### IC-04 — 驗證閘門與文件

- **Purpose**: 讓 charter gate 3 與 C-005 可交付：realexec 新步驟、mutants、`tools/gate.sh` 跑出 evidence 報告；四份文件與偏差註記同變更更新。
- **Relevant requirements**: C-001、C-005、SC-001、SC-004、SC-005、SC-006
- **Affected surfaces**: `tools/gate/realexec.sh`、`tools/gate/mutants.txt`、`PRODUCT.md`（Capabilities 指令面）、`ARCHITECTURE.md`（§2 `pluginjson` 入口列、§3.5 補一句）、`README.md`、`README.zh-TW.md`、`.gate/report-plugin-manifest-validate/`（本機產物，不入 git）
- **Sequencing/depends-on**: IC-01、IC-02、IC-03
- **Risks**: realexec 的 read-only 檢查要用 `cmp` 比對驗證前後的 plugin.json；mutant 錨點必須是唯一字串（`gatetool replace` 要求恰好一處）；文件更新是 reviewer 檢查 C-005 的唯一證據。
