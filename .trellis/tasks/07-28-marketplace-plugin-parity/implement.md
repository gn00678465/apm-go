# Implement：marketplace/plugin parity

依 `design.md` 的技術方案排執行順序。原則：**先低風險字串、再基礎設施、最後功能**，
每步都有可獨立驗證的收斂條件，任一步紅燈就停在該步。

全程遵守 TDD：每個步驟先寫會紅的測試，再實作到綠。

---

## 逐步外部閘門（每一步都要，不准留到 Step 9）

**為什麼**：規劃階段連續三次「宣告完成 → 外部檢查抓到實質缺陷」
（subagent 抓到 6 個 AC 覆蓋缺口 + 1 個 AC 自相矛盾；codex 抓到 5 個阻斷級，
其中一個推翻了使用者已裁定的 D5）。把驗證留到最後一步是同一個失敗形狀，
而實作階段的返工成本高一個量級。

**每個 Step 結束時，在進入下一步之前**：

```bash
git add -A && git diff --cached > /tmp/step-N.diff   # 只取該步的 diff
codex exec -s read-only - <<'EOF'
你是對抗性稽核者，目標是反駁不是確認。只看 <本步的 diff>。
1. 這個 diff 有沒有讓任何既有測試變成套套邏輯（把被測功能刪掉測試還是綠）？
2. 新增的測試有沒有真的會紅？把實作改壞一行，它會抓到嗎？
3. 有沒有任何宣稱（註解、commit message、進度回報）屬於
   .trellis/spec/guides/claim-evidence-guide.md 的七種句型卻沒有證據三件套？
4. 本步對應的 AC，驗法是否真的可執行、通過條件是否可否證？
找不到問題就明說找不到，不要湊數。
EOF
```

**閘門規則**：
- 阻斷級發現 → **停在該步**修完再往下，不累積到後面
- 我不得在未跑這個閘門的情況下說某一步「完成」
- 稽核結果逐步 append 到 `review/step-audits.md`，不覆蓋

**成本**：每步約 10 分鐘。9 步約 1.5 小時，換取錯誤不會積到 Step 9 才爆。

---

## Step 0 — 建立基準線

- [ ] `go build ./...` / `go vet ./...` / `go test ./... -cover` 全綠，記下當前覆蓋率數字
- [ ] 記下 `go test ./cmd/apm-go/ -run TestInit -v` 的現有測試清單（後續會有預期性轉紅）

**verify**：三個指令皆 exit 0；覆蓋率數字寫進 `implement.jsonl` 當比較基準。

---

## Step 1 — 純字串修正（R1.3、R6）

零結構風險，先落地降低後續 diff 噪音。

- [ ] `cmd/apm-go/install.go:829-832`：`target:` → `targets:`（兩處：說明句與 YAML 範例）
- [ ] `internal/marketplace/registry.go:248`：補註冊與 `marketplace list` 的補救指引

**verify**：AC4、AC22 的測試；`go test ./cmd/apm-go/ ./internal/marketplace/`

---

## Step 2 — target 集合單一來源（R8）

必須排在 Step 4（註解區塊）之前，因為註解清單要從這裡推導。

- [ ] `internal/manifest/target.go`：新增 `deployTargets` 切片，
      `SupportedTargets` 與 `adapterTargets` 皆由它導出
- [ ] `deployTargets` 內容：`copilot, claude, opencode, codex, antigravity, agent-skills`
      （前 5 個維持 `promptTargetsOrdered` 現有順序，`agent-skills` 附加尾端）
- [ ] 刪除 `cmd/apm-go/init.go:17-19` 的 `promptTargetsOrdered`，改用 `manifest.SupportedTargets`
- [ ] `CanonicalTargets` **不動**

**verify**：AC24（`init --target agent-skills` 成功）、AC25（三者同集合的鎖定測試）、
AC27（`targets: [cursor]` 仍解析成功 + req-tg-004 warning）

**風險**：`SupportedTargets` 從 5 變 6 會改變 `init --target` 的錯誤訊息內容
（`init.go:141-142` 的 allowed 清單）與互動選單項數。現有斷言這兩者的測試會轉紅——預期內。

---

## Step 3 — ux 測試 seam（design.md §8）

必須排在 Step 5/6 之前，否則 AC2/AC3 無法斷言。

- [ ] `internal/ux/interactive.go:84,149,195`：`confirmWith` / `multiSelectWith` /
      `inputFormWith` 三個 func 改為 package-level var（函式體不動）
- [ ] 在 `internal/ux` 加一個測試輔助（僅測試檔可見）以還原 stub
- [ ] 冒煙測試：stub 三者 + TTY var，確認 `Clack.MultiSelect` 走 stub 而非 huh

**verify**：`go test ./internal/ux/`；`go build ./...` 確認沒有 func-vs-var 的呼叫端破壞

**注意**：不改動任何函式體、不改簽章、不新增相依。這步的 diff 應該只有三行的
`func X(...)` → `var X = func(...)` 加對應的收尾括號。

---

## Step 4 — apm.yml 有序產生器（R2）

- [ ] 新增 `manifestSpec` 與 `buildManifestNode`（`cmd/apm-go/init.go`）
- [ ] 鍵序：`name, version, description, author, targets, dependencies, includes, [devDependencies], scripts`
- [ ] `targets` key node 掛三行 `HeadComment`，第三行由 `manifest.SupportedTargets`
      排序後 join（**不得是字面量**）
- [ ] 無 targets 時輸出註解掉的骨架
- [ ] 改寫 Phase 6 管線為 `buildManifestNode → SafeDump → SafeLoad → ParseManifest → 寫檔`
- [ ] 刪除舊的 `buildManifestData`
- [ ] 同時完成 R1.2（寫複數 `targets:`）—— 新產生器天然就是複數

**verify**：AC1、AC5、AC6、AC7、AC26

**已驗證的技術前提**：`yaml.Node` + key node 的 `HeadComment` 經 `yamlcore.SafeDump`
（`WithV3Defaults(), WithLineWidth(-1), WithIndent(2)`）輸出 `# ` 前綴註解與 2 空格序列縮排，
與上游產物逐字相符。此結論由 probe 實測，非推論。

**預期轉紅**：所有斷言 apm.yml 字母序或單數 `target:` 的既有 init 測試。逐一改斷言，
**不要**改回產生器。

---

## Step 5 — `readExistingTargets` 讀雙鍵（R1.1）

- [ ] `cmd/apm-go/init.go:308-330`：先查 `targets`，找不到再退回 `target`
- [ ] 兩個鍵都用同一段 type switch（string / []any）

**verify**：AC2、AC3 —— 用 Step 3 的 seam 斷言 MultiSelect 收到的
`opts[i].Selected` 含既有 targets

---

## Step 6 — `plugin` 指令群與 `plugin init`（R3）

本步最大，拆成三個子步，每個子步各自綠燈再往下。

### 6a. 共用本體重構（行為不變）

- [ ] 抽出 `initMode` 值物件與 `runInit(cmd, args, mode, opts)`
- [ ] `initCmd()` 傳 `consumerMode`，行為與現在**完全一致**

**verify**：Step 0 記下的既有 init 測試（經 Step 4 更新斷言後）全綠 —— 這步不該改變任何行為

### 6b. `plugin.json` 產生器

- [ ] 新增 `internal/pluginjson` 套件與 `Scaffold`
- [ ] 欄位序 `name, version, description, author.name, license`，
      `license` 硬寫 `"MIT"`，2 空格縮排，結尾換行
- [ ] 序列化用 `bundle.MarshalIndent`
- [ ] **不重用** `bundle.PluginManifest`（理由見 design.md §4）

**verify**：AC11 的逐欄比對（拿 `research/` 裡的 eval 產物當 golden）

### 6c. 接上 `plugin init`

- [ ] 新增 `cmd/apm-go/plugin.go`：`plugin` group + `plugin init`
- [ ] 旗標僅 `--yes/-y`、`--target`、`--verbose/-v`
- [ ] `pluginMode`：kebab-case 驗證、`--yes` 版本 `0.1.0`、`devDependencies`、
      `plugin.json`、plugin 版 Next Steps
- [ ] Next Steps **只印** `Pack as plugin: apm-go pack`，不印 `--dev`（R3.4）
- [ ] `cmd/apm-go/main.go` 註冊

**verify**：AC8、AC9、AC10、AC12、AC13

**AC12 是回歸閘門**：`init --yes` 產物不得含 `devDependencies`、不得產生 `plugin.json`。
plugin 模式若洩漏到 consumer，這條會抓到。

---

## Step 7 — plugin-native 根目錄警告（R4）

- [ ] `pluginRootSources(root string) []string` 純函式
- [ ] 目錄用 `os.Lstat` + `IsDir()` 且**排除 symlink**（`ModeSymlink`）
- [ ] `hooks.json` 用 `Lstat` + `Mode().IsRegular()`
- [ ] `.apm/` 存在即短路
- [ ] 接進共用本體，兩個模式都跑；`ck.Warn` / `ux.Warn` 兩條輸出路徑

**verify**：AC14（有 `skills/` → 有警告且 exit 0）、AC15（有 `.apm/` → 無警告）、
AC16（symlink 不觸發）

**AC16 的陷阱**：Windows 建立 symlink 需要權限。測試若無法建 symlink 應 `t.Skip`
而非假綠 —— 用 `os.Symlink` 的 error 判斷，並在 skip 訊息寫明原因。
**但 skip 不等於通過**：AC16 必須在至少一個平台上真的執行過才能打勾，
否則等於 symlink 行為從未被測試也能勾選。

**AC39 的擴充**：R4.2 的觸發條件涵蓋 6 個目錄 + `hooks.json`，
測試要**逐一**覆蓋，不能只測 `skills/` —— 只測一個的話，實作漏掉其餘五個仍會全過。

---

## Step 8 — `package add` 隱含 HEAD（R5）

- [ ] `internal/marketplace/authoring/editor.go` 的 `resolveRef` 改為 design.md §6 的判定順序
- [ ] **local 判定必須排在隱含-HEAD 之前**
- [ ] `--no-verify` + 需解析 HEAD → exit 2 + 指定訊息
- [ ] 顯式 `--ref HEAD` → 先印 mutable-ref 警告
- [ ] exit 2 沿用 `marketplace_package.go` 既有機制

**verify**：AC17、AC18、AC19、AC20、AC21

**AC21 是回歸閘門**：`editor_test.go:18` 的 `panicLister` 測試必須維持綠燈 ——
它證明 local source 零旗標時完全不觸網。這條紅了代表判定順序寫反。

---

## Step 8b — `install --dev`（R9）

必須排在 Step 6c 之前完成，否則 `plugin init` 的 Next Steps（AC46）會印出跑不動的指令。

- [ ] `install` 加 `--dev` 旗標
- [ ] `persistPackagesToManifest`（`install.go:2012`）的 `"dependencies"`（`:2024`）
      參數化為區段名；`devDependencies` 不存在時比照 `:2029-2035` 建立
- [ ] 新建的 `devDependencies` 鍵序落在 `includes` 與 `scripts` 之間（與 Step 4 同一契約）
- [ ] lockfile 補 `PackageType` 欄位 + parse/write/equality
      （`internal/lockfile/write.go:20` 已有鍵名，但無對應 Go 欄位）
- [ ] **不動**既有 dev 讀取鏈（install/update/uninstall/pack/compile 都已有分支且有測試）

**verify**：AC42、AC43、AC44（回歸閘門：不加 `--dev` 行為不變）、AC45

## Step 8c — `marketplace package add --category`（R10）

- [ ] `AddOptions`（`editor.go:413-422`）加 `Category string`
- [ ] `AddPackage` 帶入；寫出路徑 `editor.go:288-290` 已存在，不需改
- [ ] `add` 加 `--category` 旗標；**`set` 不加**
- [ ] `outputs` 含 codex 且缺 category 時印警告，**不阻斷**

**verify**：AC47、AC48、AC49（`set` 仍無此旗標——`marketplace_package_test.go:36-43`
已有的守門測試要維持綠燈）、AC50（端對端 pack 成功）

## Step 9 — 收斂

- [ ] `go fmt ./...`
- [ ] `go build ./...` / `go vet ./...`
- [ ] `go test ./... -coverprofile=cover.out` 後
      `go tool cover -func=cover.out | tail -1` 取 **total** ≥ 80% 且不低於 Step 0 基準線
      （`-cover` 只印各 package 百分比，沒有 repository total，不能當閘門）
- [ ] **先 `go build -o bin/apm-go.exe ./cmd/apm-go`**，所有驗 exit code 的檢查一律跑這個
      binary，不可用 `go run`（實測 `os.Exit(2)` 經 `go run` 只回 1）
- [ ] 所有以 `go test -run <pattern>` 當閘門的檢查，先用 `go test -list <pattern>`
      證明匹配集合非空（`-run` 零匹配同樣 exit 0）
- [ ] 手動端對端：空目錄跑 `bin/apm-go.exe plugin init demo-plugin --yes`，
      逐欄比對 apm.yml 與 plugin.json 對上 `research/eval-real-run-20260728.md` §B 的產物
- [ ] 逐條走 `prd.md` 的全部 AC，把有測試佐證的打勾；**沒有佐證的不准打勾；
      `t.Skip` 不算佐證**
- [ ] 逐條走 `checklist.md`，含每個 Deferral 的「證明此項延後正當」與 Tripwire sweep
      （sweep 範圍須含 `checklist.md` 自己與 `*.jsonl`）
- [ ] 用 `trellis-check` 做品質驗證

**verify**：AC51 + `prd.md` 的 AC 全數有對應測試 + `checklist.md` 全數成立

> AC 與 checklist 的條目數以 `prd.md` / `checklist.md` 當下的實際內容為準，
> 這裡不再寫死數字 —— 寫死會與來源漂移（codex 稽核已抓到一次 63 vs 70 的不一致）。

---

## 執行順序依賴圖

```
Step 0
  ├─ Step 1（獨立）
  ├─ Step 2 ──────────┐
  ├─ Step 3 ──────┐   │
  │               │   ↓
  │               │  Step 4 ──→ Step 5
  │               │              │
  │               └──────────────┤
  │                              ↓
  │               Step 8b ──→ Step 6（6a→6b→6c）──→ Step 7
  ├─ Step 8（獨立）
  ├─ Step 8c（獨立）
  └────────────────────────────────────────────────→ Step 9
```

Step 1、Step 8、Step 8c 與其他步驟無依賴，可先做或最後做。
Step 2、3 是 Step 4/5/6 的前置，不可對調。
**Step 8b（`--dev`）必須早於 Step 6c** —— `plugin init` 的 Next Steps（AC46）
要印 `apm-go install --dev`，這個指令得先能跑。

---

## 不在本 task（實作時若手癢請回頭看這裡）

- cursor / gemini / kiro / windsurf 的部署 adapter（成本**未驗證**，非「大工程」定論）
- `marketplace check` 的三個診斷維度 `reachable`/`version_found`/`ref_ok`
  （這是真的資訊缺失，不是純呈現層；估 60–120 LOC）
- `install` 的全面交易化（deploy/lock/manifest 三者；估 200+ LOC + failure injection）
- 真 PTY 端對端測試（明確承認的覆蓋缺口）
- 「studio」相關驗證（需求未成形，非技術延後）

（`install --dev` 已移出此清單 → Step 8b；`package add --category` → Step 8c。）
- 真 PTY 測試
- 把 `license` 寫進 apm.yml（會偏離上游並改變 SBOM 行為）
