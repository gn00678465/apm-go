# 新增 apm-go plugin init

> Child of `07-28-marketplace-plugin-parity`。
> 研究依據與技術設計見 parent 的 `research/`、`design.md` §3–§5、§8。

## Goal

補上上游唯一缺席的指令群 `apm plugin`，以及它的單一子指令 `init`
（含 6 個相對 consumer init 的差異），並補上兩個模式共用的
plugin-native 根目錄警告。

## 依賴（硬性，不可對調）

| 前置 task | 為什麼 |
|---|---|
| `07-29-targets-init-shape` | 三件事：(a) **有序 YAML 產生器**（`devDependencies` 要插在 `includes` 與 `scripts` 之間）、(b) 統一後的 `SupportedTargets`、(c) **ux 測試 seam**（`interactive.go` 三個 func 改 var）—— AC41 需要它 |
| `07-29-install-dev` | AC46 要求 Next Steps 印 `apm-go install --dev <owner>/<repo>`。該指令不存在時**不得印出** —— 印出跑不動的指令比少印一行傷害更大 |

> **ux seam 不屬於本 task**（2026-07-29 修正）。它原本被列在本 task 的執行步驟裡，
> 但 `targets-init-shape` 的 AC2/AC3 需要它 —— 形成
> 「targets 需要 plugin-init 的 seam / plugin-init 依賴 targets」的**循環**。
> 已移交 `targets-init-shape`（該檔 AC-L0）。本 task 是 seam 的**消費者**，不是建立者。
> 來源：codex 第二輪稽核阻斷 3。

**若 `07-29-install-dev` 未完成就要開始本 task**：Next Steps 只印
`Pack as plugin: apm-go pack` 一行，並在本檔記錄此偏離，待前置完成後補回。

## Requirements

對應 parent `prd.md` 的 **R3、R4**（逐字沿用）。摘要：

### R3 — `plugin` 指令群與 `plugin init`

1. `cmd/apm-go/main.go:25-35` 註冊 `plugin` group，**唯一**子指令 `init`。
2. 介面：`apm-go plugin init [PROJECT-NAME] [-y|--yes] [--target TARGETS] [-v|--verbose]`。
   `PROJECT-NAME` **可省略**（省略時取當前目錄名）。
3. 六個相對 consumer 的 delta（上游 file:line 見 parent research）：
   - **a** 專案名須過 kebab-case `^[a-z][a-z0-9-]{0,63}$`；
     consumer 的 `/` `\` `..` 檢查（`init.go:35-39`）**不得因此變嚴或消失**
   - **b** `--yes` 時 version `0.1.0`；**互動表單預設值兩模式皆 `1.0.0`**
   - **c** apm.yml 多寫 `devDependencies: {apm: []}`
   - **d** 專案根額外寫 `plugin.json`：
     `{name, version, description, author:{name}, license:"MIT"}`，
     2 空格縮排、結尾補一個換行
   - **e** Next steps 換成 plugin 作者版
   - **f** 具備 `-v/--verbose`；consumer 的 `init` **不得因此獲得**這個旗標
4. Next Steps 印兩行（前提是前置 task 已完成，見「依賴」）。

### R4 — plugin-native 根目錄警告

1. `apm-go init` 與 `apm-go plugin init` **共用本體都要跑**。
2. 觸發：根目錄存在 `agents/ skills/ commands/ instructions/ extensions/ hooks/`
   任一目錄（**非 symlink**）或 `hooks.json` 檔案，**且**根目錄沒有 `.apm/`。
3. 純警告不阻斷。

## Acceptance Criteria

沿用 parent `prd.md` 的 **AC8–AC16、AC23、AC30–AC34、AC36–AC39、AC41、AC46、AC52**。
（AC29 已移交 `07-29-targets-init-shape` —— 它對應 R1.4／parent C4，屬 targets 的範圍，
不屬本 task 的 R3/R4。來源：codex 第二輪稽核阻斷 3。）

### plugin init 介面與產物

- [ ] AC8 — `plugin init --help` 只有 `--yes/-y`、`--target`、`--verbose/-v`
- [ ] AC9 — `plugin init My_Plugin` 失敗；`my-plugin` 成功
- [ ] AC10 — `--yes` 產物含 `version: 0.1.0` 與 `devDependencies: {apm: []}`，
      且後者位於 `includes` 與 `scripts` 之間
- [ ] AC11 — 根目錄 `plugin.json` 逐欄符合 R3.3.d（含 `license: "MIT"`、縮排、結尾換行）
- [ ] AC12 — **回歸閘門**：`init --yes` 產物**不含** `devDependencies`、**不產生** `plugin.json`
- [ ] AC13 — Next Steps **逐字**含 `Pack as plugin:` + `apm-go pack`，
      且**不含** consumer 版的 `Install a package:` 提示
- [ ] AC30 — `apm-go plugin --help` 只列出 `init` 一個子指令
- [ ] AC31 — `plugin init --yes` **不給 PROJECT-NAME** 時取目錄名並過 kebab-case
- [ ] AC46 — Next Steps 印兩行，含 `apm-go install --dev <owner>/<repo>`
      （依賴 `07-29-install-dev`）

### consumer 模式不得被污染（反向閘門）

- [ ] AC32 — `apm-go init My_Project --yes` **仍然成功**（kebab-case 不得洩漏到 consumer）
- [ ] AC33 — `apm-go init --help` **不含** `--verbose`/`-v`
- [ ] AC37 — consumer 對 `a/b`、`a\b`、`..` 的**既有拒絕仍然存在**

### 名稱驗證邊界

- [ ] AC36 — 首字元非小寫字母（`1abc`）拒絕；長度 64 通過、65 拒絕
- [ ] AC38 — **互動模式**下 plugin 與 consumer 的版本表單預設值**皆為 `1.0.0`**

### plugin-native 警告

- [ ] AC14 — 含 `skills/` 且無 `.apm/` 時 `apm-go init` 印警告且 **exit 0**
- [ ] AC15 — 建立 `.apm/` 後警告消失
- [ ] AC16 — `skills` 是 symlink 時不觸發
      （**skip 不算通過**，須在至少一個平台真的執行過）
- [ ] AC34 — **`plugin init`** 在同樣情境也印警告（AC14 只驗了 `init` 那一半）
- [ ] AC39 — `agents/ commands/ instructions/ extensions/ hooks/` 與 `hooks.json`
      **逐一**都會觸發，不只 `skills/`

### 互動風格鎖定（R11.1/R11.2，D12）

- [ ] AC52 — **`plugin init` 走 clack 互動路徑，呼叫序列與 consumer `init` 相同**。
      驗法：stub TTY seam 為 true + stub `interactive.go` 三個 func，
      記錄 `Banner/Intro/Form/MultiSelect/Note/Confirm/Outro` 呼叫序列，
      斷言兩個模式序列一致。
      **可否證性要求**：把 `plugin init` 改寫成獨立非互動指令時這條必須轉紅；
      若不會紅，它就是假綠，不算通過。

      > 為什麼需要這條：原本的計畫只靠「複用 `init.go` 共用本體所以會自動繼承 clack」
      > 來擔保，但**零個 AC 驗它**。實作若寫成獨立非互動指令，
      > AC8–AC13、AC30、AC31、AC38、AC41 全部照過，沒有一條會紅。
      > 由使用者提問揭露（2026-07-29，parent D12）。

### 互動路徑測試

- [ ] AC23 — 互動路徑有測試覆蓋且**未新增任何 `go.mod` 相依**
      （`go.mod` 檢查須同時看未提交的 working tree）
- [ ] AC41 — `Form`、`MultiSelect`、`Confirm` 三分支**各有獨立斷言**，
      不是一個測試跑過就算

本 task 專屬：

- [ ] AC-L1 — `go build ./...`、`go vet ./...` exit 0；覆蓋率 **total** ≥ 80%。
      驗法（PowerShell）：`go test ./... -coverprofile=cover.out;`
      `go tool cover -func=cover.out | Select-Object -Last 1`
- [ ] AC-L2 — **parent C5 的本地閘門**：未新增任何第三方相依。
      驗法：`git diff -- go.mod; git diff --cached -- go.mod` —— 都不得出現新 `require` 行。

## Constraints

- **不補** `apm-go init --plugin` deprecated 別名（parent D2）。
  證據：`init.go:223-225` 無此旗標，`git log --all -S--plugin -- cmd/apm-go/init.go`
  零命中，無舊使用者需要 migration。
- **不引入真 PTY 相依**（parent D4）。**明確承認的覆蓋缺口**：
  `ux` 接縫只能測參數資料，測不到真 binary 的 stdin/stderr wiring、Ctrl-C、
  escape sequence、終端尺寸。成本估計：單一 Windows PTY smoke 約 80–150 LOC。
- **ux 測試 seam 由 `07-29-targets-init-shape` 建立**（該檔 AC-L0），本 task 僅使用。
  AC41 依賴它。
- **C7（golden 來源約束）**：AC13 與 AC46 比對的上游 Next Steps 文字，
  基準**必須**取自 PTY 捕獲的上游 stdout
  （`evals/apm-20260728T140015Z-1-001/新文件 7.md:19-22`，該處含 rich 框線字元
  `╭─── Next Steps ───╮`，證明是 TTY 模式輸出）。
  不得使用非 TTY 的降級輸出當基準 —— 上游 rich/click 在非 TTY 下會少掉框線與顏色。
  來源需求：`requirements-trace.md` U9（使用者澄清「PTY 執行 apm 的實際 stdout」）。

  **注意**：本 task 比對的是**文字內容**（`Pack as plugin:` / `apm-go pack` 等），
  不是框線渲染。apm-go 自己在 TTY 模式下的渲染輸出**沒有任何測試覆蓋**
  （所有測試都走 `ux.CanPrompt()` 為 false 的純文字路徑），
  這一點列在 parent Out of Scope，成本 80–150 LOC，**不在本 task**。

## 驗收紀律（全域，沿用 parent）

- 驗 exit code 一律用預先 build 的 `bin/apm-go.exe`，**禁止 `go run`**
- 以 `go test -run <pattern>` 當閘門前，先 `go test -list <pattern>` 證明匹配非空
- `t.Skip` 不算通過
- 端對端 AC 用 `t.TempDir` fixture，不得在 repo 內跑（會被既有 `.claude/`、
  `.codex/` marker 污染），也不得留 `<path-to-...>` placeholder
- 任何判斷句遵守 `.trellis/spec/guides/claim-evidence-guide.md`

## 執行步驟

對應 parent `implement.md` 的 **Step 6（6a→6b→6c）、Step 7**。
（Step 3 的 ux seam 已移交 `07-29-targets-init-shape`，本 task 直接使用。）
逐步 codex 閘門規則適用。
