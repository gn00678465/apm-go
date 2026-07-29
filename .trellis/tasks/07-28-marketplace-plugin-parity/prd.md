# 補齊 marketplace/plugin 與上游 docs 的缺口

> **本檔為 parent（傘任務）。** 2026-07-29 拆成四個可獨立驗收的 child，
> 實作在 child 進行，本檔保留完整的需求／限制／AC 與研究依據當單一事實來源。

## 子任務與依賴

| Child | 涵蓋 | 前置 |
|---|---|---|
| `07-29-targets-init-shape` | R1、R2、R8 · AC1–7、24–27 | 無（**最先做**） |
| `07-29-install-dev` | R9 · AC42–45 | 無（可與上者並行） |
| `07-29-marketplace-add-fixes` | R5、R6、R10 · AC17–22、40、47–50 | 無（完全獨立） |
| `07-29-plugin-init` | R3、R4 · AC8–16、23、29–34、36–39、41、46 | **`targets-init-shape`**（有序 YAML 產生器 + 統一的 `SupportedTargets`）<br>**`install-dev`**（Next Steps 要印的 `--dev` 得先能跑） |

依賴寫在各 child 的 `prd.md` 裡，不靠樹狀位置暗示。

### 依賴圖（有型別的邊）

每條邊都載明**運送什麼**，而且**該載荷必須在下游的 `verify.ps1` 裡被實際檢查** ——
邊沒被檢查就等於邊不存在（`.trellis/spec/guides/loop-graph-engineering.md` 模式 4）。

```
targets-init-shape ──[E1: buildManifestNode(manifestSpec) 存在且輸出語意鍵序]──┐
targets-init-shape ──[E2: manifest.SupportedTargets 為 6 元素且含 agent-skills]─┤
targets-init-shape ──[E3: ux seam — interactive.go 三個 func 已改為 var]────────┤
                                                                                ├─→ plugin-init
install-dev ─────────[E4: `apm-go install --help` 含 --dev 且可執行]────────────┘

marketplace-add-fixes   （無入邊、無出邊）
agent-schema-spec       （無入邊、無出邊）
```

**無環驗證**（機械檢查，不是目視）：入度為 0 的有
`targets-init-shape`、`install-dev`、`marketplace-add-fixes`、`agent-schema-spec`；
移除它們後 `plugin-init` 入度歸零；集合清空 ⇒ **無環**。

**被封鎖下游清單**（模式 6）：

| 若此 child 的 verify 紅燈 | 被封鎖的下游 |
|---|---|
| `targets-init-shape` | `plugin-init`（E1/E2/E3 三條邊全斷） |
| `install-dev` | `plugin-init` 的 **AC46 一條**（E4）；其餘 AC 不受影響 |
| `marketplace-add-fixes` | 無 |
| `agent-schema-spec` | 無 |
| `plugin-init` | 無 |

### 雙層停止條件（模式 1）

每個 child 宣告完成必須**依序**通過：

1. **Tier 1 確定性**：`pwsh .trellis/tasks/<child>/verify.ps1` → exit 0。
   **先跑**，紅燈就不准往下。agent 不得用摘要滿足它。
2. **Tier 2 獨立稽核**：`codex exec -s read-only` 對該 child 的 diff 反向稽核，無阻斷級。

只有 Tier 2 通過而 Tier 1 沒跑 → **視同未驗證**。

### 重試上限（模式 5）

同一條 AC 修 **3 次**仍紅 → 停止重試，**升級給使用者**（附三次的 verify 輸出與各改了什麼）。
**升級 ≠ 延後**；把做不到寫成「延後 / 範圍外 / 不影響」是
`claim-evidence-guide.md` 禁止的收斂性斷言。

**AC 分割的機械驗證**（2026-07-29 實跑）：

```
parent: 49  child: 48
在 parent 但沒被任何 child 認領 → 51
在 child 但 parent 沒有        → （無）
被兩個以上 child 重複認領      → （無）
```

AC51（全域 `build`/`vet`/覆蓋率）刻意不分配 —— 由每個 child 各自的 `AC-L1` 取代，
因為覆蓋率門檻要在每個 child 各自成立，不是等到最後一次總驗。

## 為什麼要拆

規劃階段連續三次「宣告完成 → 外部檢查抓到實質缺陷」：
subagent 抓到 6 個 AC 覆蓋缺口 + 1 個 AC 自相矛盾；
codex 抓到 5 個阻斷級，其中一個推翻了已裁定的 D5。
一個 task 塞 10 個 R、49 條 AC，驗收閘門大到跑不完，就是同一個失敗形狀。
拆開之後每個 child 的閘門小到可以真的逐條驗，而不是打勾打到失去意義。

## Goal

補上 `apm-go plugin init`，並修正 init / marketplace 子指令與上游 apm v0.26.0 的
旗標、產物形狀與語意落差。

## 研究依據

- `research/upstream-marketplace-plugin.md` — 上游原始碼對照（pin `634f7b603a8c` / v0.26.0）
- `research/eval-real-run-20260728.md` — 上游實機跑測產物分析（同版，`apm.lock.yaml:3` 佐證）
- `research/agent-schema-support-matrix.md` — 各 AI Agent 的 marketplace/plugin schema 與三軸支援度矩陣
- `review/codex-audit-checklist.md` — codex 對抗性稽核報告 + 主對話的獨立複驗（**推翻了 D5/D6 的事實基礎**）
- **`requirements-trace.md` — 使用者需求追溯矩陣 + 收尾閘門。
  這份是唯一驗證「PRD 是否符合使用者要的」的檔案；
  `checklist.md` 只驗「實作是否符合 PRD」，兩者缺一不可。
  目前有 2 項 ⚠️/❌ 阻擋交付（見該檔 D 節）。**

## 已裁定的範圍決定

| 決定 | 內容 |
|---|---|
| D1 | targets 鍵**讀寫都對齊複數** `targets:` |
| D2 | **不補** `apm-go init --plugin` deprecated 別名（本專案從未有此介面，無需被 deprecate） |
| D3 | targets 註解區塊的可選值**列本專案真能部署的 6 個**，不照抄上游的 10 個。<br>已知取捨：apm.yml 的 `CanonicalTargets`（`target.go:5-17`）其實接受全部 10 個，填缺 adapter 的 4 個只會拿到 req-tg-004 warning（`manifest.go:218-225`）而非失敗；註解列 6 個是刻意低報，避免引導使用者填沒用的值 |
| D8 | 一併修兩個**與上游無關的本專案內部不一致**：`agent-skills` 白名單、`promptTargetsOrdered` 漂移（見 R8） |
| D4 | 互動路徑驗證**走 `internal/ux` 的 TTY 接縫**，不引入真 PTY 相依 |
| ~~D5~~ | ~~`install --dev` 不在本 task，另開~~ **已撤銷（2026-07-29）**。撤銷理由：原裁定基於「成本大，貫穿 install/lock/update/uninstall」這個估計，但 codex 稽核證明 dev 相依機制**已經全通**——`install.go:433,457,1030-1035`、`update.go:94,303`、`uninstall_resolve.go:58,253,273`、`uninstall.go:586`、`pack.go:299`、`compile.go:76` 都有 dev 分支，且既有測試 `install_test.go:126` 已證明手寫 `devDependencies.apm` 能被解析、部署、寫入 lockfile。真正缺的只有旗標與持久化區段選擇。改由 **R9** 納入本 task |
| ~~D6~~ | ~~codex `category` 閘門刻意不對齊上游~~ **已修正（2026-07-29）**。原裁定宣稱「本專案較佳」，但 `editor.go:413-422` 的 `AddOptions` 沒有 `Category` 欄位，所以 apm-go 只是把失敗點從 add 移到 pack，還多留下一個無效的 apm.yml。改由 **R10** 補 `--category` 旗標，讓兩種情境都真的優於上游 |
| D7 | `marketplace check` 不表格化、`install` 不改寫入順序 —— 但**宣稱範圍已收窄**（見 C2、C3） |
| D9 | codex 稽核的其餘 7 項重大、3 項次要發現全部修進 PRD 與 checklist（2026-07-29 裁定） |
| D12 | **`plugin init` 必須與 `init` 同一種互動（clack）風格，且要有 AC 鎖住**（見 R11）。<br>先前的計畫只靠「複用共用本體所以會自動繼承」擔保，但**零個 AC 驗它** —— 實作若寫成獨立非互動指令，AC8–AC13、AC30、AC31、AC38、AC41 全部照過。這是「它會自動來」這種無證據擔保，與 `claim-evidence-guide.md` 禁止的斷言同類。由使用者提問揭露（2026-07-29） |
| D13 | **`marketplace init` 維持非互動**（與上游一致，非偏離）。<br>證據：上游 `commands/marketplace/init.py` 全檔 `click.confirm`/`click.prompt` 出現 **0 次**；本專案 `cmd/apm-go/marketplace_authoring.go` 的 init 只用 `ux.Success`/`ux.BulletList`/`ux.Section`，無 clack、無 prompt。**需要回歸閘門**防止實作 `plugin init` 時順手把 clack 也加進 `marketplace init`（見 R11.3） |

---

## Requirements

### R1 — targets 單複數全面對齊複數

1. `cmd/apm-go/init.go:308-330` 的 `readExistingTargets` 必須同時接受
   `target:`（單數，向後相容）與 `targets:`（複數）兩個鍵。
2. `cmd/apm-go/init.go:229-246` 的 `buildManifestData` 改寫入複數 `targets:`。
3. `cmd/apm-go/install.go:829-832` 的 no-deploy-target 教學文字改印 `targets:`。
4. 既有 `internal/manifest` 的雙鍵解析（`manifest.go:119`/`:125`/`:238`/`:240`）不得改動 ——
   舊 apm.yml 必須繼續能讀。

### R2 — init 產物形狀對齊上游

1. apm.yml 鍵序改為語意序：
   `name → version → description → author → targets → dependencies → includes → scripts`
   （plugin 模式在 `includes` 與 `scripts` 之間插入 `devDependencies`）。
2. `targets:` 上方插入註解區塊，逐字為：

   ```
   # Which agent platforms to deploy to.
   # Resolution order: --target flag > this field > auto-detect from filesystem.
   # Accepted values: <本專案支援清單>
   ```

   第三行的清單依 D3 列本專案真能部署的 6 個（字母序）：
   `agent-skills, antigravity, claude, codex, copilot, opencode`。
3. 該清單**不得是第四份獨立字面量**，必須與 R8 統一後的 `SupportedTargets` 同源推導。
4. 沒有選任何 target 時，比照上游插入註解掉的 targets 骨架。

### R3 — 新增 `apm-go plugin` 指令群與 `plugin init`

1. 於 `cmd/apm-go/main.go:25-35` 註冊 `plugin` group，唯一子指令 `init`。
2. 介面：`apm-go plugin init [PROJECT-NAME] [-y|--yes] [--target TARGETS] [-v|--verbose]`。
3. 本體複用 `cmd/apm-go/init.go` 既有流程，plugin 模式相對 consumer 的差異：
   - **a.** 專案名必須通過 kebab-case 驗證 `^[a-z][a-z0-9-]{0,63}$`，不通過即失敗
     （consumer 模式維持現行的 `/` `\` `..` 檢查，不得因此變嚴）。
   - **b.** `--yes` 時 version 預設 `0.1.0`（consumer 為 `1.0.0`）；
     互動模式的表單預設值維持 `1.0.0`（上游實跑產物佐證）。
   - **c.** apm.yml 多寫 `devDependencies: {apm: []}`。
   - **d.** 於專案根額外寫 `plugin.json`：

     ```json
     {
       "name": "<name>",
       "version": "<version>",
       "description": "<description>",
       "author": { "name": "<author>" },
       "license": "MIT"
     }
     ```

     縮排 2 空格、結尾補一個換行。
   - **e.** Next steps 改為 plugin 作者版（見 R3.4）。
   - **f.** 具備 `-v/--verbose` 旗標（consumer 的 `apm-go init` 目前沒有，本 task 不替它補）。
4. **Next Steps 印兩行，與上游一致**（原本因 D5 只印一行，D5 撤銷後恢復）：
   `Add dev dependencies:  apm-go install --dev <owner>/<repo>` 與
   `Pack as plugin:  apm-go pack`。
   前提是 R9 的 `--dev` 同 task 落地——若 R9 因故未完成，這一行必須拿掉，
   不可印出本專案跑不動的指令。見 AC46。

### R4 — plugin-native 根目錄來源警告

1. `apm-go init` 與 `apm-go plugin init` **共用本體都要跑**（上游同樣是共用本體邏輯）。
2. 觸發條件：專案根存在 `agents/ skills/ commands/ instructions/ extensions/ hooks/`
   任一目錄（非 symlink），或 `hooks.json` 檔案，**且**根目錄沒有 `.apm/` 目錄。
3. 行為：印警告提醒這些檔案仍會被 `apm-go pack` 收錄。純警告，不阻斷。

### R5 — `marketplace package add` 的隱含 HEAD 解析

1. `internal/marketplace/authoring/editor.go` 的 `resolveRef`：**沒給 `--ref`** 時
   視為隱含 HEAD，對 remote source 解析為具體 SHA 後寫入 `ref:`。
2. `--no-verify` 且需要解析 HEAD（顯式或隱含）時，印
   `Cannot resolve HEAD ref without network access. Provide an explicit --ref SHA.`
   並以 exit code 2 結束。
3. 顯式傳 `--ref HEAD` 時，額外印
   `'HEAD' is a mutable ref. Resolving to current SHA for safety.`
4. `--version` 有給時不解析 ref（維持現行互斥行為，`marketplace_package.go:37`）。
5. **不得破壞** mkt-046 的既有契約：local (`./`) source 永不觸網
   （`editor_test.go:18` 的 `panicLister` 測試必須維持綠燈）。

### R6 — `marketplace audit` 錯誤訊息補救指引

`internal/marketplace/registry.go:248` 的 `marketplace %q is not registered`
後面補上註冊與查詢的具體指令，語意對齊上游。

### R7 — 互動路徑測試覆蓋

1. 透過 `internal/ux/ux.go:56-62` 的 `stdinIsTTY` / `stderrIsTTY` 接縫，
   為 `plugin init` 的互動路徑（Form / MultiSelect / Confirm）補測試。
2. 不新增 PTY 相依（`creack/pty`、`charmbracelet/x/conpty` 目前僅存在於 `go.sum`）。
3. 「studio」一詞使用者未澄清所指，**不納入本 task 範圍**。

### R8 — 修正本專案的兩個 target 集合內部不一致

（與上游 parity 無關，是研究過程順帶發現的自有缺陷。）

1. `internal/manifest/target.go:25-27` 的 `SupportedTargets` 補上 `agent-skills`，
   使其與 `adapterTargets`（`:29-36`）同集合。
   - 現況缺陷：`agent-skills` 有部署 adapter（`HasAdapter` 回 true），
     但 `apm-go init --target agent-skills` 會被 `init.go:141-142` 拒為
     "not supported by init"；手寫進 apm.yml 卻可正常部署。
2. `cmd/apm-go/init.go:17-19` 的 `promptTargetsOrdered` 不再是獨立字面量，
   改為自 `SupportedTargets` 推導，並加測試防止兩者漂移。
   - 現況缺陷：兩份清單各自宣告（同集合、不同順序），漂移不會被任何測試抓到。
3. 完成後，以下四者必須同源、不可各自宣告：
   `SupportedTargets`、`adapterTargets`、`promptTargetsOrdered`、R2.2 註解的 Accepted values。
4. `CanonicalTargets`（`target.go:5-17`）**不得更動** —— 它是 apm.yml 的合法詞彙，
   與上游 `CANONICAL_TARGETS` parity，涵蓋 10 個 target 是正確的。

### R9 — `apm-go install --dev`（原 D5，已撤銷延後）

1. `install` 新增 `--dev` 旗標：把 positional packages 寫入 `devDependencies.apm`
   而非 `dependencies.apm`。
2. `cmd/apm-go/install.go:2012-2035` 的 `persistPackagesToManifest` 目前把
   `"dependencies"` 寫死（`:2024`），需參數化為可指定區段；
   缺 `devDependencies` 鍵時比照現行 `dependencies` 的建立邏輯自動建立。
3. **不重做既有的 dev 解析/部署鏈** —— 那些已經存在且有測試覆蓋
   （`install_test.go:126`）。本需求只補「寫入端」。
4. lockfile 的 `package_type` 分類（原 Out of Scope 第二列）一併納入：
   `internal/lockfile/write.go:20` 已有這個鍵在欄位排序白名單裡，
   但全 repo 沒有對應的 Go 欄位（`grep PackageType` 零命中），目前永不輸出。
5. `plugin init` 的 Next Steps 因此**恢復印兩行**（推翻 R3.4 的原偏離）：
   `Add dev dependencies: apm-go install --dev <owner>/<repo>` 與
   `Pack as plugin: apm-go pack`。

### R11 — 互動風格的歸屬與鎖定（2026-07-29 由使用者提問揭露）

1. **`plugin init` 必須走與 `init` 相同的 clack 互動路徑**：
   `!yes && ux.CanPrompt()` 時建立 `ux.NewClack`，依序經過
   Banner → Intro → Form（metadata）→ MultiSelect（targets）→ Note → Confirm → Outro
   （對應 `cmd/apm-go/init.go:62-65`、`:111`、`:289`、`:174`、`:177`、`:212`）。
2. 這個保證**不得只靠「複用共用本體」的設計意圖**，必須有 AC 可否證 ——
   實作若改成獨立的非互動指令，必須有測試轉紅。
3. **`marketplace init` 維持非互動**（`ux.Success`/`BulletList`/`Section`，
   無 clack、無 prompt），與上游一致（上游 `commands/marketplace/init.py` 的
   `click.confirm`/`click.prompt` 出現 0 次）。這需要**回歸閘門**：
   實作 `plugin init` 時不得順手把 clack 帶進 `marketplace init`。

### R10 — `marketplace package add --category`（原 D6，已修正）

1. `internal/marketplace/authoring/editor.go:413-422` 的 `AddOptions` 新增
   `Category string` 欄位，並在 `packageEntryNode` 寫出（`editor.go:288-290`
   已有 `putStr("category", entry.Category)`，只是目前沒人填）。
2. `cmd/apm-go/marketplace_package.go` 的 `add` 新增 `--category` 旗標。
   **只加在 `add`**，`set` 維持上游旗標集合不變。
3. `marketplace.outputs` 含 `codex` 且未給 `--category` 時，印警告說明
   pack 階段會失敗、可用 `--category` 補；**不阻斷 add**
   （維持 `schema.go:12-21` 的 compose-time-only 閘門設計）。
4. **刻意優於上游**：上游 `add` 沒有 `--category`，所以開了 codex 輸出後
   CLI 再也無法新增任何 package（`research/eval-real-run-20260728.md` §C4
   實跑三次皆失敗）。補了旗標之後，apm-go 在 claude-only 與 codex 兩種情境
   都能正常 add 且 pack 成功。

---

## Constraints

- **C1（刻意偏離上游，已於 2026-07-29 收窄並補強）**：`marketplace package add`
  在 `outputs` 含 codex 時，**不**於 add 階段**阻斷**（改為警告，見 R10.3），
  category 的硬性閘門維持 compose-time-only（`schema.go:12-21`）。
  **收窄後的宣稱**：這在 claude-only 情境確實優於上游（上游整個 add 都被擋，
  apm-go 的 `pack -m claude` 仍可成功）；但在 codex 情境，原本的做法只是把失敗點
  從 add 移到 pack，還多留下一個無效的 apm.yml —— 這個代價由 R10 的 `--category`
  旗標消除，**不是**由「維持現狀」消除。
  （原宣稱「本專案行為優於上游」被 codex 稽核推翻，證據：`editor.go:413-422`
  的 `AddOptions` 沒有 `Category` 欄位。見 `review/codex-audit-checklist.md` 阻斷 2。）
- **C2（刻意偏離上游，宣稱已收窄）**：`marketplace check` 維持 bullet list + pass rate
  輸出（`cmd/apm-go/marketplace_authoring.go:275-297`），不改成上游的 rich 表格。
  **這不是純呈現層差異**：上游 result model 分別保存 `reachable`、`version_found`、
  `ref_ok` 三個診斷維度（上游 `check.py:130-225`），而 apm-go 的 `CheckResult`
  只有 `Package` 與單一 `Err`（`internal/marketplace/authoring/refcheck.go:131`）。
  使用者因此無法區分「remote 連不上」「沒有版本 tag」「ref 不存在」。
  **本 task 的裁定僅為「不改 UI 形狀」**，資訊等價與否是一個未裁定的獨立問題，
  成本估計 60–120 LOC（result model + checkPackage + renderer + 測試），
  列入 Out of Scope 並附此成本。
- **C3（刻意偏離上游，宣稱已收窄）**：`install` **不改動 apm.yml 的寫入順序**。
  **收窄理由**：原宣稱「本專案 fail-closed 順序較佳」過寬。實際上
  `deploy.Run` 在 `install.go:1620` 就已改動部署目標，lockfile 到 `:1764` 才寫，
  apm.yml 更晚到 `:1777` —— 讓 deploy 成功而 lockfile 寫入失敗，就會留下
  「部署檔已變、lock/apm.yml 未更新」的狀態。**apm.yml 寫得比 resolution 晚是事實**，
  但這不等於 install 整體具備交易性。全面交易化需涵蓋 deploy/lock/manifest 三者，
  估計 200+ LOC 與 failure-injection 測試，不在本 task。
- **C7（golden 來源約束，2026-07-29 依 U9 新增）**：任何拿上游輸出當比對基準的 AC，
  該基準**必須是在 PTY 下執行 `apm` 取得的實際 stdout**，不得使用非 TTY 的降級輸出。
  理由：上游用 rich/click，非 TTY 時會**降級**（無框線、無顏色、寬度不同），
  拿降級輸出當 golden 會比對到錯的東西。
  **現有素材已驗證符合**：`新文件 7.md` 含 35 處 rich 專屬框線字元
  （`:12` 的 `┏━━━┳━━━┓`、`:19` 的 `╭─── Next Steps ───╮`），只在 TTY 下輸出。
  未來若需補捕獲新的上游輸出，必須同樣在 PTY 下進行並保留框線字元當佐證。
- **C4**：向後相容——既有寫著單數 `target:` 的 apm.yml 必須繼續能被讀取與部署。
- **C5**：不新增第三方相依。
- **C6**：測試覆蓋率維持 ≥ 80%。

---

## Acceptance Criteria

### targets 單複數

- [ ] AC1：`apm-go init --yes` 產生的 apm.yml 使用 `targets:`（複數）。
- [ ] AC2：對一份只有單數 `target:` 的既有 apm.yml 跑互動式 `apm-go init`，
      MultiSelect 的預選狀態包含該檔案原有的 targets（覆寫舊 pin 的 repro 不再成立）。
- [ ] AC3：對一份只有複數 `targets:` 的既有 apm.yml 做 AC2 同樣的事，結果相同。
- [ ] AC4：`apm-go install` 的 no-deploy-target 錯誤輸出中出現 `targets:`，不出現單數示例。

### init 產物形狀

- [ ] AC5：`apm-go init --yes --target claude,codex,opencode` 產生的 apm.yml 鍵序為
      `name, version, description, author, targets, dependencies, includes, scripts`。
- [ ] AC6：同一份輸出的 `targets:` 上方存在三行註解，第三行的 Accepted values 為
      `agent-skills, antigravity, claude, codex, copilot, opencode`。
- [ ] AC7：未指定任何 target 時，輸出含註解掉的 targets 骨架，**逐字**為
      三行說明註解 + `# targets:` + `#   - claude`（design.md §2 指定的形狀）。
      不可只驗「有 `# targets:` 且下一行以 `#` 開頭」——那樣任意內容都會過。

### plugin init

- [ ] AC8：`apm-go plugin init --help` 列出 `--yes/-y`、`--target`、`--verbose/-v`，
      且沒有其他旗標。
- [ ] AC9：`apm-go plugin init My_Plugin` 以非零 exit code 失敗（kebab-case 驗證）；
      `apm-go plugin init my-plugin` 成功。
- [ ] AC10：`apm-go plugin init p1 --yes` 產生的 apm.yml 含 `version: 0.1.0` 與
      `devDependencies: {apm: []}`，且 `devDependencies` 位於 `includes` 與 `scripts` 之間。
- [ ] AC11：同一次執行於專案根產生 `plugin.json`，內容與 R3.3.d 的形狀逐欄相符
      （含 `license: "MIT"`、2 空格縮排、結尾換行）。
- [ ] AC12：`apm-go init --yes` 產生的 apm.yml **不含** `devDependencies`，
      且 **不產生** `plugin.json`（consumer 模式未被污染）。
- [ ] AC13：`plugin init` 的 Next Steps 輸出**逐字**含
      `Pack as plugin:` + `apm-go pack` 這一行，且**不含** consumer 版的
      `Install a package:  apm-go install <owner>/<repo>` 提示。
      （斷言精確一行，不是「輸出任意處含 apm-go pack」——後者連 consumer 提示混進來
      都抓不到。`--dev` 那一行由 AC46 管，兩條合起來才是完整的 Next Steps 斷言。）

### plugin-native 警告

- [ ] AC14：在含 `skills/` 目錄且無 `.apm/` 的目錄跑 `apm-go init --yes`，
      輸出含 plugin-native 根目錄警告，且指令仍成功（exit 0）。
- [ ] AC15：同目錄再建立 `.apm/` 後重跑，警告消失。
- [ ] AC16：`skills` 是 symlink 時不觸發警告。
      **驗收紀律**：Windows 建不出 symlink 時測試可 `t.Skip`，但 **skip 不算通過** ——
      這條 AC 必須在至少一個平台上真的執行過才能打勾（CI 或手動於支援的環境）。

### package add HEAD 解析

- [ ] AC17：`apm-go marketplace package add owner/repo`（無任何 ref/version 旗標）
      將解析後的具體 SHA 寫入該 entry 的 `ref:`。
- [ ] AC18：同上情境加 `--no-verify` 時，輸出
      `Cannot resolve HEAD ref without network access. Provide an explicit --ref SHA.`
      且 exit code 為 2。
      **驗收紀律**：exit code 必須用**預先 build 好的 binary**（`bin/apm-go.exe`）驗，
      不可用 `go run` —— 實測 `os.Exit(2)` 經 `go run` 只會回 1
      （見 `review/codex-audit-checklist.md` 阻斷 4）。
- [ ] AC19：`--ref HEAD` 額外印 mutable-ref 警告後照常解析。
- [ ] AC20：`--version '^1.0.0'` 時**不寫 `ref:`**。
      （更正：原本寫「不觸網」是錯的——上游 `add.py:62-64` 的 `_verify_source`
      在 `_resolve_ref` **之前**執行，apm-go `editor.go:450` 同序，
      所以 remote source 仍會為了 reachability 觸網，除非加 `--no-verify`。）
- [ ] AC21：local (`./...`) source 在上述所有情境皆不觸網
      （`editor_test.go` 的 `panicLister` 測試維持綠燈）。

### 其他

- [ ] AC22：`apm-go marketplace audit <未註冊名稱>` 的錯誤訊息含註冊指令與
      `marketplace list` 的提示。
- [ ] AC23：`plugin init` 的互動路徑（Form / MultiSelect / Confirm）有測試覆蓋，
      且未新增任何 `go.mod` 相依。
      **驗收紀律**：凡以 `go test -run <pattern>` 當閘門的 AC，必須先用
      `go test -list <pattern>` 證明匹配集合非空 —— `-run` 零匹配時同樣 exit 0，
      會造成假綠。`go.mod` 相依的檢查要同時看未提交的 working tree，
      不可只用 `git diff main...HEAD -- go.mod`。

### target 集合一致性（R8）

- [ ] AC24：`apm-go init --target agent-skills` 成功（不再被拒）。
- [ ] AC25：存在一個測試斷言 `SupportedTargets`、`adapterTargets`、
      **init 互動選單實際使用的選項清單**三者為同一集合；任一方新增/移除 target
      而其他未同步時該測試轉紅。
      （用詞說明：design.md §7 會刪除 `promptTargetsOrdered` 這個變數本身，
      所以斷言對象是「選單實際用的清單」而非該變數名，否則此 AC 字面上不可滿足。）
      **驗收紀律**：這個測試位於 `cmd/apm-go`（選單 opts 的組裝處），
      不是 `internal/manifest` —— 只跑 `internal/manifest` 會漏掉選單那一半。
- [ ] AC26：AC6 的註解清單與 `SupportedTargets` 同源，非獨立字面量。
      **驗法必須是行為測試**：測試中暫時替換來源切片（或直接呼叫 comment builder）
      加入一個假 target，斷言註解輸出跟著變。
      不可只用 grep 找不到完整字面量就算過 —— 清單若被拆成數個字面量再拼接，
      grep 一樣是零命中。
- [ ] AC27：`CanonicalTargets` 內容未被更動，`targets: [cursor]` 仍解析成功並產生
      req-tg-004 warning。

### 覆蓋率補洞（由 checklist 機械推導比對 R 子項 vs AC 後補上）

以下 6 條對應 R1.4/C4、R3.1、R3.2、R3.3.a 反向、R3.3.f 反向、R4.1 ——
這些子項原本沒有任何 AC 覆蓋。

- [ ] AC29：對一份**只有單數 `target:`** 的既有 apm.yml，端對端跑
      `apm-go install` 與 `apm-go pack` 仍能正確部署（不只驗 `readExistingTargets`，
      要驗 `internal/manifest` 的雙鍵解析在部署鏈上真的沒被改壞）。對應 R1.4 / C4。
- [ ] AC30：`apm-go plugin --help` 只列出 `init` 一個子指令。對應 R3.1。
- [ ] AC31：`apm-go plugin init --yes`（**不給 PROJECT-NAME**）在當前目錄成功初始化，
      名稱取自目錄名並通過 kebab-case 驗證。對應 R3.2。
- [ ] AC32：`apm-go init My_Project --yes`（consumer 模式）**仍然成功** ——
      plugin 的 kebab-case 規則不得洩漏到 consumer。對應 R3.3.a 反向。
- [ ] AC33：`apm-go init --help` **不含** `--verbose`/`-v`。對應 R3.3.f 反向。
- [ ] AC34：在含 `skills/` 且無 `.apm/` 的目錄跑 **`apm-go plugin init`**，
      同樣印出 plugin-native 根目錄警告（AC14 只驗了 `init` 那一半）。對應 R4.1。

### codex 稽核補洞（2026-07-29，對應 `review/codex-audit-checklist.md` 阻斷 3）

原 R→AC 對照表宣稱 33 個子項皆已覆蓋，codex 獨立重做後找到 6 個仍有實質缺口。

- [ ] AC36：kebab-case 驗證的**邊界**有測試：首字元非小寫字母（`1abc`）、
      長度 64（通過）與 65（拒絕）。AC9 只測了底線。對應 R3.3.a。
- [ ] AC37：consumer `apm-go init` 對 `a/b`、`a\b`、`..` 的**既有拒絕仍然存在**
      （`init.go:35-39` 的規則不得因 plugin 模式重構而消失）。對應 R3.3.a。
- [ ] AC38：**互動模式**（非 `--yes`）下 plugin 與 consumer 的版本表單預設值**皆為 `1.0.0`**。
      AC10 只測了 `--yes` 的 `0.1.0`。對應 R3.3.b。
- [ ] AC39：plugin-native 警告對 `agents/ commands/ instructions/ extensions/ hooks/`
      與 `hooks.json` **逐一**都會觸發，不只 `skills/`。對應 R4.2。
- [ ] AC40：`resolveRef` 的每個分支各有測試：隱含 HEAD、顯式 HEAD、`--version`、
      `--no-verify`、40-hex SHA、local source。AC21 只測了 local + 零旗標。對應 R5.5。
- [ ] AC41：互動路徑測試**逐一命中** `Form`、`MultiSelect`、`Confirm` 三個分支
      （各自有獨立的斷言，不是一個測試跑過就算）。對應 R7.1。

### install --dev（R9）

- [ ] AC42：`apm-go install --dev owner/repo` 把該套件寫入 `devDependencies.apm`，
      **不**寫入 `dependencies.apm`。
- [ ] AC43：apm.yml 原本沒有 `devDependencies` 鍵時，`--dev` 會自動建立該區段，
      且鍵序符合 R2.1（在 `includes` 與 `scripts` 之間）。
- [ ] AC44：不加 `--dev` 時行為與現況完全一致（寫入 `dependencies.apm`）——回歸閘門。
- [ ] AC45：`--dev` 裝進來的套件在 `apm.lock.yaml` 有 `package_type` 欄位；
      非 dev 的既有行為不變。
- [ ] AC46：`plugin init` 的 Next Steps **印兩行**，含
      `apm-go install --dev <owner>/<repo>` 與 `apm-go pack`（推翻原 R3.4 的偏離）。

### marketplace package add --category（R10）

- [ ] AC47：`apm-go marketplace package add owner/repo --category Productivity`
      在 apm.yml 的該 entry 寫出 `category: Productivity`。
- [ ] AC48：`outputs` 含 codex 且**未給** `--category` 時 add **仍然成功**，
      但印出警告說明 pack 會失敗、可用 `--category` 補。
- [ ] AC49：`marketplace package set` **沒有** `--category` 旗標（維持上游旗標集合）。
- [ ] AC50：走完 `add --category` 之後 `apm-go pack`（codex 輸出開啟）成功 ——
      端對端證明死結已解除。

### 互動風格鎖定（R11）

- [ ] AC52：**`plugin init` 走 clack 互動路徑**，且呼叫序列與 consumer `init` **相同**。
      驗法：stub `ux` 的 TTY seam 為 true + stub `interactive.go` 的三個 func，
      記錄 `Banner/Intro/Form/MultiSelect/Note/Confirm/Outro` 的呼叫序列，
      斷言 `plugin init` 與 `init` 的序列一致（複用同一本體的可否證證明）。
      **這條必須在「把 plugin init 改寫成獨立非互動指令」時轉紅** —— 否則它是假綠。
- [ ] AC53：**回歸閘門 —— `marketplace init` 仍為非互動**。
      驗法：`cmd/apm-go/marketplace_authoring.go` 的 `marketplaceInitCmd` 函式體內
      **不得出現** `ux.NewClack`、`ck.Form`、`ck.MultiSelect`、`ck.Confirm`；
      且在有 TTY 的情境下跑 `marketplace init` **不得阻塞等待輸入**。
      對應 D13：與上游一致，不是偏離。

### 全域

- [ ] AC51：`go build ./...`、`go vet ./...` exit 0；
      `go test ./... -coverprofile=cover.out` 後以
      `go tool cover -func=cover.out | tail -1` 取 **total** ≥ 80%。
      （更正：`go test ./... -cover` 只印各 package 百分比，不產生 repository total，
      原本的 AC35 沒有可計算的通過值。）

---

## Out of Scope（明列，避免日後誤判為遺漏）

| 項目 | 理由 | 追蹤 |
|---|---|---|
| ~~`apm-go install --dev`~~ | **已移出 Out of Scope**，改由 R9 納入本 task（原成本估計被證明錯誤） | D5 撤銷 |
| ~~lockfile `package_type`~~ | **已移出**，併入 R9.4 | D5 撤銷 |
| `apm-go init --plugin` deprecated 別名 | 本專案從未有此介面；`git log --all -S--plugin -- cmd/apm-go/init.go` 零命中，無舊使用者需要 migration | D2 |
| 補齊 cursor / gemini / kiro / windsurf adapter | 產品邊界決定。**成本標記為未驗證** —— 現有簡單 adapter 只有 19–39 行（`internal/deploy/agentskills.go`、`claude.go`），在未研究這四個 target 的實際格式前不能定級為「大工程」；需先做每 target 0.5–1 天的 reconnaissance 才能估。（原「獨立大工程」的定級被 codex 稽核指出無成本證據） | D3 |
| 真 PTY 端對端測試 | 需新增直接相依，Windows 須走 ConPTY。**明確承認的覆蓋缺口**：`ux` 接縫只能測參數資料，測不到真 binary 的 stdin/stderr wiring、Ctrl-C、escape sequence、終端尺寸。成本估計：單一 Windows PTY smoke 約 80–150 LOC | D4 |
| 「studio」相關驗證 | **未釐清的產品問題，不是技術延後**。素材只有「studio」一詞（`research/eval-real-run-20260728.md:372`），沒有足以定義需求的資訊；在需求成形前無法估計成本 | R7.3 |
| `marketplace check` 的三個診斷維度（`reachable` / `version_found` / `ref_ok`） | 這是**真的資訊缺失**，不是純呈現層。`refcheck.go:131` 的 `CheckResult` 只有 `Package` + 單一 `Err`。成本估計 60–120 LOC（result model + checkPackage + renderer + 測試） | C2 |
| `install` 全面交易化（deploy / lock / manifest 三者） | `install.go:1620` 的 `deploy.Run` 已改動部署目標，lock 到 `:1764`、apm.yml 到 `:1777` 才寫；deploy 成功而 lock 失敗會留下不一致狀態。成本估計 200+ LOC + failure-injection 測試 | C3 |
| marketplace.json（claude/codex）與 plugin.json（claude/copilot）的 schema 對齊 | 已逐欄驗證與上游一致，無缺口；含 codex `source.url` shorthand 這個上游瑕疵也已對齊。codex 獨立稽核複核後同樣未找到新落差 | `research/agent-schema-support-matrix.md` §2、§3 |

## Notes

- Keep `prd.md` focused on requirements, constraints, and acceptance criteria.
- 本 task 屬複雜任務：`design.md` 與 `implement.md` 需在 `task.py start` 前補齊。
