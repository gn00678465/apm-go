# CLI contract: `apm-go plugin validate`

```
apm-go plugin validate [path] [--strict] [-v|--verbose]
```

| Element | Contract |
|---|---|
| `path` | 預設 `.`。目錄：依序探測 `plugin.json`、`.github/plugin/plugin.json`、`.claude-plugin/plugin.json`、`.cursor-plugin/plugin.json`，取第一個存在者。檔案：直接驗證。兩個以上位置參數 → usage error。 |
| `--strict` | warnings > 0 時 exit 1；印出的計數不變。 |
| `-v`, `--verbose` | 在 `Validation Results:` 前以 ` i ` 逐行列出 manifest 內出現的已知欄位（檔案順序）。 |
| `--help` | 列出兩個 flag；`plugin --help` 列出 `init` 與 `validate`。 |

## stdout（all status lines on stdout, PRODUCT.md symbols）

```
 > Validating plugin '<relative manifest path>'...
[ i <field>            ×N, only with -v ]

 i Validation Results:
 + Structure: passed
 + Name: passed
 ! Fields: 'metadata' should be an object; Claude Code ignores other values
 x Paths: 'skills' must start with './'
 ! Unrecognized: unrecognized field 'descripton' (did you mean 'description'?)

 i Summary: 2 passed, 2 warnings, 1 errors
```

- 每個 check 一行 `passed`，或每個 finding 一行（errors 先、warnings 後）。
- Structure 有 error 時：只印 Structure 的行，其餘 check 不出現，`Summary: 0 passed, 0 warnings, 1 errors`。
- 找不到 manifest：` x no plugin.json found in <dir> (looked in plugin.json, .github/plugin/plugin.json, .claude-plugin/plugin.json, .cursor-plugin/plugin.json)`，exit 1，無 Results／Summary。
- 無法讀取 manifest：` x could not read '<path>': <reason>`，exit 1，無 Results／Summary。此列涵蓋定位成功之後的所有讀取失敗：不是普通檔案（symlink、FIFO、裝置）、解析後落在指定路徑之外、超過 5 MiB 上限、以及作業系統回報的讀取錯誤。`<reason>` 為簡短原因，不得回傳完整系統路徑以外的內部細節。

## Messages（exact）

| Check | Message |
|---|---|
| Structure | `file exceeds 5 MiB cap (<n> bytes)` / `invalid UTF-8` / `invalid JSON: <decoder message>` / `top-level value must be an object` / `duplicate key '<k>' (last value wins)`（warning） |
| Name | `missing required field 'name'` / `'name' must be a string` / `'name' must not be empty` / `'name' must not contain spaces` / `'name' must not contain control characters` / `'name' must not contain bidirectional formatting characters` / `'name' is not kebab-case`（warning） |
| Fields | `'<field>' must be a string` / `… must be an object` / `… must be a boolean` / `… must be an array of strings` / `… must be an array of objects` / `… must be a string or array` / `… must be a string or array of objects` / `… must be a string, array, or object` / `'author.<k>' must be a string` / `'dependencies[<i>]' must be a string or object` / `'dependencies[<i>]' is missing 'name'` / `'dependencies[<i>].name' must be a string` / `'dependencies[<i>].version' must be a string` / `'metadata' should be an object; Claude Code ignores other values`（warning）/ `'experimental' should be an object; Claude Code ignores other values`（warning）/ `'experimental.<k>' must be a string or array` / `'experimental.monitors' must be a string or array of objects` |
| Paths | `'<field>' must start with './'` / `'<field>' must not be an absolute path` / `'<field>' must not contain '..'`；array 元素以 `'<field>[<i>]'` 命名 |
| Unrecognized | `unrecognized field '<k>'` [+ ` (did you mean '<known>'?)` when Damerau-Levenshtein ≤ 2] / `unrecognized field 'experimental.<k>'` / `'themes' belongs under 'experimental'`（warning）/ `'monitors' belongs under 'experimental'`（warning） |

## exit codes

| Condition | exit |
|---|---|
| errors == 0 且（無 `--strict` 或 warnings == 0） | 0 |
| errors > 0；或 `--strict` 且 warnings > 0；或找不到 manifest；或無法讀取 | 1（Summary 後靜默） |
| 位置參數 > 1、未知 flag | 2（stderr Usage 區塊） |

## Side effects

無。驗證前後目標目錄的檔案樹與位元組相同（realexec 以 `cmp` 驗證）。
