# marketplace package add 的 HEAD 解析與 category

> Child of `07-28-marketplace-plugin-parity`。
> 研究依據與技術設計見 parent 的 `research/`、`design.md` §6 與 §12。

## Goal

把 `marketplace package add` 的 ref 解析語意對齊上游，補上 `--category` 消除
codex 輸出的死結，並補齊 `marketplace audit` 的錯誤訊息。

## 依賴

- **前置**：無。與其他三個 child 完全獨立，可任意時機並行。
- **後續**：無 child 依賴本 task。

## Requirements

對應 parent `prd.md` 的 **R5、R6、R10**（逐字沿用）。

### R5 — 隱含 HEAD 解析

上游 `commands/marketplace/plugin/__init__.py:102-142` 的 `_resolve_ref`：
`is_head = ref is None or ref.upper() == "HEAD"` —— **沒給 `--ref` 就是隱含 HEAD**。
apm-go `internal/marketplace/authoring/editor.go:511-520` 目前直接短路。

判定順序（**local 判定必須排在隱含-HEAD 之前**，否則打破 mkt-046 契約）：

```
version != ""             → 回傳 ""（不解析）                [現行，保留]
isLocalPackageSource(src) → 回傳 ""（不觸網）                [mkt-046，保留]
ref 是 40-hex SHA         → 原樣回傳                         [現行，保留]
ref == "" 或 HEAD：
    noVerify → exit 2 + "Cannot resolve HEAD ref without network access. Provide an explicit --ref SHA."
    顯式 "HEAD" → 先印 mutable-ref 警告
    → lister.ListRefs(src) 解析 → 回傳 SHA
其他（tag/branch）        → 現行邏輯不變
```

### R6 — `marketplace audit` 錯誤訊息

`internal/marketplace/registry.go:248` 的 `marketplace %q is not registered`
補上註冊與 `marketplace list` 的具體指令。

### R10 — `--category` 旗標

1. `editor.go:413-422` 的 `AddOptions` 加 `Category string`。
   寫出路徑 `editor.go:288-290` 已存在（`putStr("category", entry.Category)`），
   只是目前沒有呼叫端會填。
2. `cmd/apm-go/marketplace_package.go` 的 `add` 加 `--category`。**只加在 add**；
   `set` 維持上游旗標集合（`marketplace_package_test.go:36-43` 已有守門測試）。
3. `outputs` 含 codex 且未給 `--category` 時印警告，**不阻斷**
   （保住 `schema.go:12-21` 的 compose-time-only 閘門，讓 `pack -m claude` 仍能成功）。

## Acceptance Criteria

沿用 parent `prd.md` 的 **AC17–AC22、AC40、AC47–AC50、AC53**。

- [ ] AC17 — 零旗標 add 把解析後的 SHA 寫入 `ref:`
- [ ] AC18 — `--no-verify` 時輸出指定訊息且 **exit code 2**
      （必須用預先 build 的 binary 驗，`go run` 會把 2 變成 1）
- [ ] AC19 — `--ref HEAD` 額外印 mutable-ref 警告後照常解析
- [ ] AC20 — `--version '^1.0.0'` 時**不寫 `ref:`**
      （注意：仍會為 reachability 觸網，除非 `--no-verify` —— 上游 `add.py:62-64`
      的 `_verify_source` 在 `_resolve_ref` 之前，apm-go `editor.go:450` 同序）
- [ ] AC21 — **回歸閘門**：local source 在所有情境皆不觸網
      （`editor_test.go` 的 `panicLister` 測試維持綠燈；這條紅了代表判定順序寫反）
- [ ] AC22 — audit 錯誤訊息含註冊指令與 `marketplace list` 提示
- [ ] AC40 — `resolveRef` 每個分支各有測試：隱含 HEAD、顯式 HEAD、`--version`、
      `--no-verify`、40-hex SHA、local source
- [ ] AC47 — `add --category Productivity` 在 apm.yml 寫出 `category: Productivity`
- [ ] AC48 — `outputs` 含 codex 且未給 `--category` 時 add **仍成功**但印警告
- [ ] AC49 — `package set` **沒有** `--category` 旗標
- [ ] AC50 — `add --category` 後 `pack`（codex 輸出開啟）成功 —— 端對端證明死結解除

### 互動風格回歸閘門（R11.3，D13）

- [ ] AC53 — **`marketplace init` 仍為非互動**（與上游一致，非偏離）。
      驗法：`marketplaceInitCmd` 函式體不得出現 `ux.NewClack` / `ck.Form` /
      `ck.MultiSelect` / `ck.Confirm`；有 TTY 時執行不得阻塞等待輸入。
      證據：上游 `commands/marketplace/init.py` 的 `click.confirm`/`click.prompt`
      出現 0 次；本專案現況只用 `ux.Success`/`BulletList`/`Section`。

      > 這條的用途是防止實作 `07-29-plugin-init` 時，
      > 順手把 clack 也加進 `marketplace init`。

本 task 專屬：

- [ ] AC-L1 — `go build ./...`、`go vet ./...` exit 0；coverprofile total ≥ 80%

- [ ] AC-L9 — **parent C5 的本地閘門**：未新增任何第三方相依。
      驗法：`git diff -- go.mod; git diff --cached -- go.mod` —— 都不得出現新 `require` 行。
      （`git status --porcelain` 只給 `M go.mod`，看不出新增了什麼，不足以判定。）

## Constraints

**C1（刻意偏離上游，宣稱已收窄）**：category 的硬性閘門維持 compose-time-only。
這在 claude-only 情境確實優於上游（上游整個 add 都被擋，apm-go 的 `pack -m claude`
仍可成功）；codex 情境的無效中間狀態由 `--category` 消除，**不是**由「維持現狀」消除。
證據：`editor.go:413-422` 的 `AddOptions` 原本沒有 `Category` 欄位
（見 parent `review/codex-audit-checklist.md` 阻斷 2）。

## 驗收紀律（全域，沿用 parent）

- 驗 exit code 一律用預先 build 的 `bin/apm-go.exe`，**禁止 `go run`**
- 以 `go test -run <pattern>` 當閘門前，先 `go test -list <pattern>` 證明匹配非空
- `t.Skip` 不算通過
- AC17–AC20 需要最小 marketplace apm.yml fixture 與可控的 ref lister，
  **不得依賴 live network**
- 任何判斷句遵守 `.trellis/spec/guides/claim-evidence-guide.md`

## 執行步驟

對應 parent `implement.md` 的 **Step 1（audit 訊息）、Step 8、Step 8c**。
逐步 codex 閘門規則適用。
