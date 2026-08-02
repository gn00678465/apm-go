# 驗證紀錄 — 07-29-plugin-init

> **狀態：待使用者驗證。任務未標記完成。** `task.py finish` 未執行。

- 實作者：`trellis-implement`（fresh agent，兩段式——中途兩次撞額度限制，
  皆從中斷點無損續作）
- 驗證者：主 session（獨立重跑、端到端探針、mutation，不採信子代理回報）
- 日期：2026-07-31

## Tier 1

```
pwsh -File .trellis/tasks/07-29-plugin-init/verify.ps1
→ TIER 1 GREEN（起點 13 RED → 0）· 覆蓋率 86.9%
```

主 session 親跑三次（半成品盤點、收貨、閘門補強後）皆與宣稱一致。

## 主 session 端到端探針（真 binary）

- `plugin init --yes`：apm.yml + 根目錄 plugin.json 齊備；plugin.json 逐欄位
  同上游 golden 形狀（`{name, version, description, author:{name},
  license:"MIT"}`）；Next Steps 印 `install --dev` 與 `Pack as plugin` 兩行
- **版本預設交叉核對**：`init -y` → `1.0.0`、`plugin init --yes` → `0.1.0`
  ——初看矛盾，經查 prd.md R3.3-b 為**刻意 delta**（上游行為），非缺口
- 非法名稱 `UPPER_dir` → exit 1（修正過探針本身量到 tail 的 bug 後重量）

## mutation 測試

| # | 執行者 | Mutation | 結果 |
|---|---|---|---|
| 1 | 子代理 | plugin.json license `"MIT"`→`"APACHE"` | ✅ golden 比對紅 |
| 2 | 子代理 | Next Steps 移除 `Pack as plugin` 行 | ✅ 閘門 AC13 紅 |
| 3 | 子代理 | `plugin init` RunE 繞過 `runInitCore` | ✅ AC52 clack 序列一致性測試紅（`init: [Intro Form MultiSelect Note Confirm Outro]` vs `[]`） |
| 4 | **主 session** | kebab-case regex 首字元類放寬 `[a-z]`→`[a-zA-Z]` | ❌ **兩層皆綠——缺口**：`My_Plugin`/`1abc` 探針在放寬後仍拒絕，大寫開頭合法尾巴無人測 |

### 缺口 #4 的補強（主 session 修閘門）

verify.ps1 補 `AC36/upper-first`（`Abc` 必拒）與 `AC36/hyphen-first`
（`-abc` 必拒）兩探針；mutation 重放證實：放寬 regex 時 `Abc` exit 0
（新探針會轉紅）、還原後 exit 1。完整閘門 GREEN。

## 附註

- `internal/pluginjson/` 為新套件（`Scaffold()`）：與 `pack` 的
  `bundle.Synthesize` **不是**重複事實來源——上游本來就有兩個不同的
  plugin.json 產生器（init 模板含 license MIT；pack 從 apm.yml 合成無
  license，見 research §3.4），兩者各對各的 golden。
- `go.mod` 無新增（AC23/AC-L2 閘門 + 主 session diff 皆空）。

## 外部稽核（codex）

（待 marketplace-add-fixes 產品面修正完成後，與該 task 合併一輪稽核以省
額度；零 BLOCKING 才得宣告可交付。）

---

# Round 7（外部稽核第七輪，2026-07-31）

> `trellis-implement` 修復本輪點名的 A-BLOCKING-1、A-MINOR-1。
> B-BLOCKING-1/2、B-MAJOR-1、B-MINOR-1 記在
> `07-29-marketplace-add-fixes/verification-record.md`。**implementer
> 本地跑過，尚未經外部覆核，`task.py finish` 未執行。**

## A-BLOCKING-1 — AC37 從未探過反斜線路徑

根因：`verify.ps1` 的 AC37 區塊只探過 `init 'a/b'`（正斜線），從未探過
`init 'a\b'`（反斜線）。`consumerValidateName`（`cmd/apm-go/init.go:73`）
現況已是 `strings.ContainsAny(pn, "/\\")`，兩種分隔符號都擋——**production
code 本身沒有 bug**，是**閘門本身有覆蓋缺口**：若未來有人把它「簡化」成
`strings.ContainsAny(pn, "/")`（漏掉反斜線），這個閘門過去完全驗不到，
因為根本沒有一個探針會用到反斜線。

修復：`verify.ps1` 新增 `init 'a\b'` 探針（`AC37/backslash`），與既有的
`AC37/forward-slash` 並列。

證據（在真的 binary 上直接驗證現況已符合，不是宣稱）：

```
$ bin/apm-go.exe init 'a\b' --yes
Error: invalid project name "a\\b"
exit=1
```

`verify.ps1` 執行：

```
  ok   [AC37/forward-slash]
  ok   [AC37/backslash]
```

## A-MINOR-1 — `ux.SetClackEventHookForTest` 未受 build-tag 保護即匯出

根因：`SetClackEventHookForTest`（`internal/ux/testhooks.go`）是一個
export 給 `cmd/apm-go` 測試用的 hook 安裝函式，活在一個**非** `_test.go`
的一般檔案裡（Go 語言限制：`_test.go` 檔的宣告無法被「另一個」套件的
測試引用，只能同套件內部或同套件的 external test package 使用，這正是
它原本活在一般檔案裡的唯一理由）——因此它會無條件編進每一次
`go build ./...`（release binary），即使沒有任何外部呼叫端。

**誠實評估的實際可利用性**：這個套件的 import path 是
`github.com/apm-go/apm/internal/ux`，Go 語言本身的 `internal/` 可見性
規則（https://go.dev/doc/go1.4#internalpackages）已經在編譯期擋掉任何
不在 `github.com/apm-go/apm/...` 這個 module 樹內的套件 import 它——
不管 release binary 裡實際含不含這個符號，外部呼叫端從來就無法透過
「import 這個套件當函式庫」的路徑觸及它；唯一觸及得到的方式是
build/fork 這個 repo 本身，而那已經等於擁有完整原始碼存取權。這也是
為什麼這條列為 MINOR 而非真正的安全漏洞——本輪的修復是二進位檔面積/
可稽核性的衛生工程，不是在補一個外部可觸及的洞。

**額外發現（實測，非本輪修復依據，僅記錄）**：即使完全不做本輪的
build-tag 隔離，單純用 `go tool nm` 檢查一個**未加任何 tag**、
`SetClackEventHookForTest` 仍無條件存在於原始碼的 release binary，
該符號一樣不會出現在連結後的執行檔裡——Go 連結器本身的 dead-code
elimination 已經因為「production 呼叫圖裡沒有任何路徑會呼叫到它」而在
連結時把它從最終執行檔拿掉了（`_test.go` 檔從不參與 `go build`，只有
`testhooks.go` 這個非-test 檔本身連結進 `ux` 套件的 object code，但沒有
任何 production 程式碼呼叫它，符合 Go 標準執行檔連結器的可達性剔除
條件）。這代表本輪修復前，「符號缺席」這件事其實已經靠連結器的
既有行為間接成立，只是不是一個**結構性保證**（連結器行為屬工具鏈細節，
理論上可能因建置模式改變如 `-buildmode=plugin`、reflection 等而失效）。
build-tag 隔離把這變成編譯期就無法通過的結構性保證，而非依賴連結器
的附帶行為。

修復：
1. `SetClackEventHookForTest` 移到新檔 `internal/ux/clackhook_shim.go`，
   掛 `//go:build apm_test_hooks`——未加這個 tag 的一般 `go build`/
   `go test` 完全不含這個檔案，符號不存在於編譯單元。
2. `cmd/apm-go`（唯一呼叫端）的 `driveInteractiveInit`
   （`plugin_init_interactive_test.go`）改呼叫一個套件內部的間接函式
   `installClackEventRecorder`，該函式有兩個 build-tag 對應版本：
   `plugin_init_clackhook_enabled_test.go`（同一個 tag，真的呼叫
   `ux.SetClackEventHookForTest`）與
   `plugin_init_clackhook_disabled_test.go`（`!apm_test_hooks`，no-op）。
   這樣未加 tag 的一般 `go test ./...` 仍能編譯並跑完
   `plugin_init_interactive_test.go` 其餘 5 個測試（AC38/AC41 的
   Form/MultiSelect/Confirm/version-default 覆蓋），只是 clack 事件
   記錄被 silently 停用。
3. 唯一真正斷言 `cap.clackEvents` 的
   `TestInitVsPluginInit_ClackSequenceParity`（AC52）移到自己獨立、同樣
   掛 `apm_test_hooks` tag 的檔案 `plugin_init_clacksequence_test.go`
   ——未加 tag 時這個測試根本不存在於編譯單元（`-list` 零匹配），
   不是「存在但斷言一個永遠空的 slice 而產生假綠」。
4. `verify.ps1` 既有的 AC52 `-list`/`ExecTestJSON` 呼叫補上
   `-tags apm_test_hooks`（原本會因為這個 tag 隔離而讓 AC52 本身
   `-list` 零匹配，本地自查時發現並修正——`ExecTestJSON` 本身也補上
   `$tags` 參數，因為第一版只把 `-tags ...` 寫進純文字的 `$what`
   說明參數，從沒真的傳給實際執行的 `go test` 指令，是文字說明跟
   實際行為脫節的假閘門，本地自查時發現並修正，非外部稽核點名）。
5. 新增兩條 `verify.ps1` 閘門：`A-MINOR-1/symbol-absent`（對 `$bin`
   跑 `go tool nm`，斷言不含 `SetClackEventHookForTest`）、
   `A-MINOR-1/untagged-excludes-clacksequence`（未加 tag 時
   `-list TestInitVsPluginInit_ClackSequenceParity` 必須零匹配，
   反向證明是「編譯期排除」而非「靜默 skip」）。

證據：

```
$ go build ./...                          → exit 0（未加 tag）
$ go vet ./... ; go vet -tags apm_test_hooks ./...  → 皆 exit 0
$ go test ./... -count=1                  → 全綠（未加 tag，含 5 個
  plugin_init_interactive_test.go 測試，clack 記錄停用但其餘斷言不受影響）
$ go test -tags apm_test_hooks ./... -count=1 → 全綠（加 tag，含
  TestInitVsPluginInit_ClackSequenceParity 本身）

$ go build -o bin/apm-go.exe ./cmd/apm-go  # 未加 tag，同 release 指令
$ go tool nm bin/apm-go.exe | grep -i SetClackEventHookForTest
（空輸出，grep exit 1 —— 符號確實缺席）

$ go test ./cmd/apm-go/ -list TestInitVsPluginInit_ClackSequenceParity
（空輸出 —— 未加 tag 時測試不存在於編譯單元）
$ go test -tags apm_test_hooks ./cmd/apm-go/ -list TestInitVsPluginInit_ClackSequenceParity
TestInitVsPluginInit_ClackSequenceParity
$ go test -tags apm_test_hooks ./cmd/apm-go/ -run TestInitVsPluginInit_ClackSequenceParity -v
--- PASS: TestInitVsPluginInit_ClackSequenceParity (0.08s)
PASS
```

`verify.ps1` 執行：

```
  ok   [AC52/test]              # 現在用 -tags apm_test_hooks 呼叫
  ok   [A-MINOR-1/symbol-absent]
  ok   [A-MINOR-1/untagged-excludes-clacksequence]
```

## 本輪 Tier 1 閘門輸出

```
$ pwsh -NoProfile -File .trellis/tasks/07-29-plugin-init/verify.ps1
TIER 1 GREEN（覆蓋率 86.9%）
```

## round 7 未處理事項

- `task.py finish` 未執行；仍需外部（或使用者）覆核後才算收斂。

---

# 外部稽核第十輪（2026-07-31）—— 2 項修復

## A-BLOCKING-1 — hooks.json-only 情境下 `plugin init` 的警告缺乏 e2e/單元回歸

### 根因

`runInitCore`（`init.go:171`）的 `detectPluginNativeRoot` 呼叫本身**沒有**
`mode.plugin` 分支（讀過程式碼確認：`if sources := detectPluginNativeRoot(originalCwd); len(sources) > 0`
對兩個模式一視同仁），但 `pluginwarn_test.go` 既有的所有測試都直接呼叫
`detectPluginNativeRoot` 本體，`verify.ps1` 先前也只驗過 `init`（consumer）
在「唯一來源是 hooks.json」情境下的警告（`:442`），從沒驗過 `plugin init`
同一情境——若未來在呼叫點加上

```go
sources := detectPluginNativeRoot(originalCwd)
if mode.plugin && len(sources) == 1 && sources[0] == "hooks.json" { sources = nil }
```

現有測試矩陣完全不會發現。

### 修復

1. 新增 Go 單元回歸 `TestPluginInitCmd_HooksJSONOnly_StillWarns`
   （`pluginwarn_test.go`）：對只有 `hooks.json`（無其他 pluginNativeDirs、
   無 `.apm/`）的目錄跑 `pluginInitCmd()`，斷言 stderr 含 `plugin-native`。
2. `verify.ps1` 的 hooks.json 警告區塊新增 `plugin init` 對照探針
   （`$o3p`），並新增 `A-BLOCKING-1/hooksjson-pluginmode` 身份鎖定閘門
   （`-requireTests`）。

### 突變驗證（實測）

暫時在 `init.go` 加入上述 mutation：

```
$ go test ./cmd/apm-go/ -run 'TestPluginInitCmd_HooksJSONOnly_StillWarns' -v
--- FAIL: TestPluginInitCmd_HooksJSONOnly_StillWarns
    pluginwarn_test.go:215: `apm-go plugin init` with only hooks.json present
    (no other pluginNativeDirs, no .apm/) printed no plugin-native warning;
    output: ... APM plugin initialized successfully! ...
FAIL
```

還原後（`cp` 回 mutation 前的 `init.go`，`git diff -- cmd/apm-go/init.go`
相對 mutation 前無殘留變更）：

```
$ go test ./cmd/apm-go/ -run 'TestPluginInitCmd_HooksJSONOnly_StillWarns' -v
--- PASS: TestPluginInitCmd_HooksJSONOnly_StillWarns (0.04s)
PASS
```

## A-MAJOR-1 — AC16 的三個 symlink/junction 分支只驗輸出內容，從未檢查 exit code

### 根因

`verify.ps1:505/519/535`（round 9 版本）的 AC16 三個分支
（skills-junction、apm-junction、hooksjson-hardlink）只用
`$oX -match/-notmatch 'plugin-native'` 判斷，從未讀 `$LASTEXITCODE`——若
`$bin init` 本身因為其他原因（例如殘留目錄衝突）失敗，輸出可能恰好不含
`plugin-native`，會被誤判成「AC16 通過」，但命令其實已經以非 0 結束。

### 修復

三個分支都在 `Pop-Location` 前立刻存 `$LASTEXITCODE`，並在比對輸出內容
之前先檢查 exit code（(a)/(b) 要求 0；(c) 同樣要求 0，因為警告不阻斷是
AC14 的明文要求）。

### 驗證

```
$ pwsh -NoProfile -File .trellis/tasks/07-29-plugin-init/verify.ps1
  ok   [AC16/skills-junction]
  ok   [AC16/apm-junction]
  ok   [AC16/hooksjson-hardlink]
...
TIER 1 GREEN
```

## 本輪全套件與 Tier 1 閘門輸出

```
$ go build ./...   → exit 0
$ go vet ./...     → exit 0
$ go test ./... -count=1 → 全綠（含新增的 TestPluginInitCmd_HooksJSONOnly_StillWarns）
$ pwsh -NoProfile -File .trellis/tasks/07-29-plugin-init/verify.ps1
TIER 1 GREEN（覆蓋率 86.9%）
```

## 本輪未處理事項

- `task.py finish` 未執行；仍需外部（或使用者）覆核後才算收斂。

---

# Round 11（fresh-context 對照上游 v0.26.0，2026-08-02）—— 2 項修正

> 依派工指示，用 fresh context 對照官方原始碼
> `D:/Projects/apm-dev/apm/src/apm_cli/`（`git checkout v0.26.0`，比對後已還原
> 該 repo 至原本的 detached HEAD `a9a883b3` + stash pop，未留副作用）找出的
> 兩處先前 round 未涵蓋的落差。**`trellis-implement` 本地跑過，尚未經外部覆核，
> `task.py finish` 未執行。**

## 落差 1 — interactive MultiSelect 選單未排除 explicit-only targets

### 根因（file:line + 反例）

上游 `core/target_detection.py:430-431`：
`EXPLICIT_ONLY_TARGETS = frozenset(capability.name for capability in
TARGET_CAPABILITIES.values() if capability.explicit_only)`，逐一核對
`core/target_catalog.py:150-177` 只有 `antigravity`（explicit_only=True，理由：
`.agents/` 共用根目錄，無獨有訊號可偵測）與 `agent-skills`
（explicit_only=True，理由：跨 client 部署位置而非單一工具）兩者為真。
`commands/init.py:629`：
`targets = [t for t in _PROMPT_TARGETS_ORDERED if t not in EXPLICIT_ONLY_TARGETS]`
——互動選單建構時就先濾掉這兩個，即使 `prechecked`（既有 apm.yml 或偵測訊號）
包含它們，`_prompt_target_selection` 回傳的 `chosen` 也不會含它們
（`commands/init.py:621-687` 通讀確認：`chosen = [targets[i] for i in
range(len(targets)) if selected[i]]`，`targets` 本身已排除，沒有事後合併邏輯）。
`--target` 旗標路徑完全不受影響（`manifest_targets_from_target_option` 無此
過濾）。

apm-go 修正前：`cmd/apm-go/init.go:420-421`（修正前行號）
`targetSelectOptions` 直接迭代 `manifest.SupportedTargets`（6 個，含
antigravity/agent-skills），MultiSelect 會把這兩個 explicit-only target
一併列為可勾選選項——與上游行為不符。`--target` 路徑（`init.go:258`）本來就
已正確使用 `SupportedTargets`，不受影響。

### 修復

1. `internal/manifest/target.go` 新增 `ExplicitOnlyTargets`（`map[string]bool`，
   `{"agent-skills": true, "antigravity": true}`）與 `PromptTargets`
   （`deployTargets` 過濾掉 `ExplicitOnlyTargets` 後的切片，保留原順序）。
2. `cmd/apm-go/init.go` 的 `targetSelectOptions` 改迭代 `manifest.PromptTargets`
   （原本迭代 `manifest.SupportedTargets`）。`--target` 驗證路徑
   （`init.go:258`）維持用 `SupportedTargets`，未改動。
3. **既有 AC25 鎖定測試的語意隨之修正**（非靜默覆蓋）：
   - `cmd/apm-go/manifestnode_test.go` 的
     `TestSupportedTargetsSet_MatchesAdapterTargetsAndPromptMenu`（AC25）
     原本斷言「prompt 選單 == SupportedTargets 全集」，這個前提本身在上游
     行為前是錯的。改為斷言「prompt 選單 == SupportedTargets 扣掉
     ExplicitOnlyTargets」，並新增雙向檢查（menu 不含 explicit-only、
     非 explicit-only 的 SupportedTargets 成員都在 menu 裡）。
   - `cmd/apm-go/plugin_init_interactive_test.go` 的
     `TestPluginInitInteractive_MultiSelect_OffersFullSupportedTargetsMenu`
     同樣從比對 `len(manifest.SupportedTargets)` 改為
     `len(manifest.PromptTargets)`，並新增「選項集合不含任何 explicit-only
     target」的逐項斷言。
4. 新增兩個派工指定的測試：
   - `cmd/apm-go/init_targetselect_test.go`：
     `TestTargetSelectOptions_ExcludesExplicitOnly`——即使
     `detected`/`existing` 顯式包含 explicit-only target，
     `targetSelectOptions` 回傳的選項集合仍不含它；並斷言
     `targetSelectOptions(nil, nil)` 的長度等於 `len(manifest.PromptTargets)`。
   - `cmd/apm-go/main_test.go`：`TestInitCmd_TargetFlag_AcceptsExplicitOnly`——
     `apm-go init --yes --target antigravity`／`--target agent-skills`
     皆 exit 0（真的跑 `initCmd().Execute()`，不是 grep 原始碼）。

### 突變驗證（實測，Edit 工具暫改 `init.go` 再還原，非宣稱）

```
$ [暫將 targetSelectOptions 內 manifest.PromptTargets 改回 manifest.SupportedTargets]
$ go test ./cmd/apm-go/... -run 'TestTargetSelectOptions_ExcludesExplicitOnly|TestSupportedTargetsSet_MatchesAdapterTargetsAndPromptMenu|TestPluginInitInteractive_MultiSelect_OffersFullSupportedTargetsMenu' -v
--- FAIL: TestTargetSelectOptions_ExcludesExplicitOnly (agent-skills, antigravity 子測試皆 FAIL)
--- FAIL: TestSupportedTargetsSet_MatchesAdapterTargetsAndPromptMenu
--- FAIL: TestPluginInitInteractive_MultiSelect_OffersFullSupportedTargetsMenu
$ [還原 init.go]
$ go build ./... && go test ./cmd/apm-go/... -run '同上 pattern' -v
--- PASS ×3
```

## 落差 2 — `marketplace init` 的 Next Steps 沒有邊框

### 根因（file:line + 反例）

上游 `commands/marketplace/init.py:108-126`：`_rich_panel(...,
title=" Next Steps", style="cyan")`（`utils/console.py:175-191`：
`Panel(content, title=title, border_style=style)`，TTY 不可用或 Rich
未安裝時才降級為 `--- {title} ---` 純文字）。apm-go 修正前
`cmd/apm-go/marketplace_authoring.go:110-116`（修正前行號）用
`ux.Section(w, "Next steps")` + `ux.BulletList(...)`——**永遠**是無邊框純文字，
不是「TTY 才有邊框」的對齊選擇，而是完全沒有邊框渲染路徑。

parent `prd.md:299-305`（C7）與本檔（`prd.md`）Constraints 段落已明文：
本 task 比對的是**文字內容**、apm-go 自身 TTY 邊框渲染**沒有測試覆蓋**且
明列為 Out of Scope（估 80–150 LOC）——這件事沒有變；本輪要修的是
「完全沒有邊框」這個更基本的落差（哪怕是非 TTY 降級形狀，上游也至少有
`--- Next Steps ---` 這種文字邊框標記，apm-go 原本連這個都沒有）。

### 修復

`cmd/apm-go/marketplace_authoring.go`：Next Steps 區塊改用既有的
`internal/ux.Box`（`internal/ux/output.go:134-138`，lipgloss
`RoundedBorder`，先前只有 `internal/ux/output_test.go` 用到，production code
零呼叫端——init.go 的「About to create」摘要已改用 clack 風格的
`ck.Note`，`ux.Box` 变成死碼但函式仍在）。**特意不用 `ck.Note`**：
`ck.Note` 會 `fireClackEvent("Note")`（`internal/ux/clack.go:196-197`），
是 AC52 clack 呼叫序列追蹤機制的一部分，屬於「互動流程」的視覺語彙
（`◇` 開頭，暗示自己是某個互動步驟序列的一環）；`marketplace init` 依
D13/AC53 明文鎖定為非互動指令，混用 `ck.Note` 會讓 AC53 的
「零互動元件」語意變得曖昧（即使 `ck.Note` 本身不阻塞，仍是 clack 家族
API）。`ux.Box` 是純 lipgloss 渲染、不掛 clack 事件、不依賴 `*Clack` 實例，
已被 `ac53_interactive_gate_test.go:24-28` 的白名單邏輯歸類為
「plain, non-blocking output helper」一類（該檔案的 denylist
`interactiveUXSelectors` 沒有 `Box`）。

內容（四行 Next Steps 文字）逐字不變，只換容器。

### 新增測試

`cmd/apm-go/marketplace_authoring_test.go`：
`TestMarketplaceInitCmd_NextStepsRenderedInBorderedBox`——斷言輸出同時含
`╭ ╮ ╰ ╯ │` 五個 RoundedBorder 字元（而非只驗「有 Next steps 字樣」）與四行
step 文字皆逐字保留。

### 突變驗證（實測）

```
$ [暫將 marketplace_authoring.go 的 ux.Box 呼叫改回 ux.Section+ux.BulletList]
$ go test ./cmd/apm-go/... -run TestMarketplaceInitCmd_NextStepsRenderedInBorderedBox -v
--- FAIL（5 個邊框字元斷言全部失敗，實際輸出貼在錯誤訊息裡，確認是舊的
    純文字 "Next steps\n * 1. ..." 形狀）
$ [還原]
$ go build ./... && go test ./cmd/apm-go/... -run 同上 -v
--- PASS
```

真 binary 端對端確認（`bin/apm-go.exe marketplace init`，臨時目錄執行，
執行後已刪除）：

```
 + Created apm.yml with 'marketplace:' block

╭─────────────────────────────────────────────────────────────────────────────────────────╮
│ Next steps                                                                              │
│ 1. Edit the 'marketplace:' block in apm.yml to add your packages                        │
│ 2. Run 'apm-go pack' to generate .claude-plugin/marketplace.json                        │
│ 3. Add 'codex' to marketplace.outputs to also generate .agents/plugins/marketplace.json │
│ 4. Commit apm.yml and the generated marketplace file(s)                                 │
╰─────────────────────────────────────────────────────────────────────────────────────────╯
```

**未驗證/已知限制**（誠實列出，不宣稱完成）：右緣在極長行上有輕微
未對齊（ambiguous-width 計算，`ux.Box` 既有行為，非本輪新增缺陷，未深究
根因）；`ux.Box` 的邊框字元組（`╭╮╰╯│─`）與上游 Rich Panel 的邊框字元組
未逐字元比對是否相同 glyph 集合——本輪只驗證「有邊框」這個布林事實，不驗
「邊框字元與上游位元組相同」，此為 C7/Out of Scope 既有邊界內。

## 本輪跨任務回歸驗證

五個 child task 的 verify.ps1 全數重跑，皆 TIER 1 GREEN（涵蓋率均
87.0%，`agent-schema-spec` 為既有值不受本輪影響）：

```
$ pwsh -File .trellis/tasks/07-29-plugin-init/verify.ps1          → TIER 1 GREEN
$ pwsh -File .trellis/tasks/07-29-targets-init-shape/verify.ps1   → TIER 1 GREEN（含 AC25 三個子測試）
$ pwsh -File .trellis/tasks/07-29-marketplace-add-fixes/verify.ps1 → TIER 1 GREEN（含 AC53/no-clack、AC53/behavioral、AC53/no-block）
$ pwsh -File .trellis/tasks/07-29-install-dev/verify.ps1          → TIER 1 GREEN
$ pwsh -File .trellis/tasks/07-29-agent-schema-spec/verify.ps1    → TIER 1 GREEN
```

`go build ./...`、`go vet ./...`、`go test ./... -count=1`（全套件，未加
`-tags`）皆 exit 0/全綠。`git diff -- go.mod go.sum` 與
`git diff --cached -- go.mod go.sum` 皆空（未新增相依，AC-L2 沿用）。

## 本輪未處理事項

- 本輪修改觸及 `internal/manifest/target.go` 與 `cmd/apm-go/init.go`，兩者
  同時是 `07-29-targets-init-shape` 的入邊來源（E1/E2/E3）；已用該 task 自己
  的 verify.ps1 重驗，但**尚未同步更新該 task 自己的 verification-record.md**
  ——留給該 task 的下一次 implement/check 輪次記錄，或由使用者裁定是否需要。
- `task.py finish` 未執行；仍需外部（或使用者）覆核後才算收斂。
