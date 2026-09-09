# Data model: Plugin manifest validate command

All types live in `internal/pluginjson`（validator）or `cmd/apm-go`（rendering）. No persistence; nothing is written.

## Manifest（input）

| Field | Type | Notes |
|---|---|---|
| `Path` | string | 被選中的 plugin.json 的路徑（相對於使用者給的 `path`，用於首行輸出） |
| `Data` | `[]byte` | 原始位元組，≤ 5 MiB。上限由 CLI 在讀取前以 `could not read` 拒絕，驗證器收到的一律是已通過上限的位元組 |

Decoded shape: `map[string]json.RawMessage`（頂層鍵 → 原始值），加一次 token 流掃描取得鍵的**檔案順序**（`-v` 列表用）與重複鍵。

## Rule table（static）

| Field | Type | Notes |
|---|---|---|
| `Name` | string | 頂層鍵，或 `experimental.<key>` |
| `Kinds` | set of JSON kinds | `string` / `object` / `bool` / `array-of-string` / `string-or-array` / `string-array-or-object` / `dependency-list` |
| `Mismatch` | `error` \| `warning` | 型別不符時的等級；只有 `metadata`、`experimental` 是 `warning` |
| `IsPath` | bool | 值（string 或 string array 的每個元素）需通過路徑語法檢查 |
| `Source` | `schema` \| `docs` \| `apm-go` | 規則來源（C-004），同步測試據此對照 schema |

Invariants（由 `validate_schema_sync_test.go` 保證）:
- `{r.Name | r.Source == schema}` ⊇ schema `properties` 的鍵。
- `{r.Name | r.IsPath}` = schema 中帶 `^\./` pattern（string 分支）的欄位 ∪ 文件新增的 `workflows`。此不變式只涵蓋規則表中的頂層欄位；`experimental.themes` 與 `experimental.monitors` 的路徑檢查由 `checkExperimentalPathField` 專責處理，不是規則表的列，因此不列入這個集合。
- schema `required` = `["name"]`。

## Finding

| Field | Type | Notes |
|---|---|---|
| `Check` | enum | `Structure` / `Name` / `Fields` / `Paths` / `Unrecognized`（輸出順序固定） |
| `Level` | enum | `Error` / `Warning` |
| `Message` | string | spec 規定的措辭，例如 `'keywords' must be an array of strings` |

## Report

| Field | Type | Notes |
|---|---|---|
| `Findings` | `[]Finding` | 依 Check 順序；同一 Check 內 errors 先、warnings 後，各自依檔案順序（與 `marketplace validate` 的渲染順序一致，儲存順序即渲染順序） |
| `KnownFieldsPresent` | `[]string` | 依檔案順序的已知欄位（`-v`） |
| `StructureFailed` | bool | 為 true 時只有 Structure 的 findings，其餘 check 不列出也不計 passed |

Derived（rendering 層計算，不存於 Report）:
- `passed` = 沒有任何 finding 的 check 數（StructureFailed 時為 0）。
- `warnings` / `errors` = 各等級 finding 數。
- exit = 2（usage）／1（errors > 0，或 `--strict` 且 warnings > 0）／0。

## State transitions

無持久狀態。單次執行：locate → open（受 `os.Root` 邊界約束）→ size cap → bounded read → Validate → render → exit。定位或讀取階段失敗以 ` x <message>` 加 exit 1 結束，不印 Results 與 Summary；驗證階段的失敗則走正常的 Results 與 Summary。任何情形都不改變檔案系統。
