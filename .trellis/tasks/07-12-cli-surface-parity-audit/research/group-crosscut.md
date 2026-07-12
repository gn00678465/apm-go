# Research: crosscut — global flags / update 殘餘 flag 面 / install-uninstall-marketplace drift check / exit code & error 格式

- **Query**: 本組專屬指引(見 task 交接訊息)——(1) 全域 flags(`--version`/`-v/--verbose`);(2) `update` 未被 07-11 覆蓋的殘餘 flag 面;(3) install/uninstall/marketplace 對照 checklist 日期後的 help drift 快篩;(4) exit code 慣例與錯誤輸出格式彙整。
- **Scope**: mixed(原始碼定位 + live help/exit-code 探測,皆唯讀,無狀態變更)
- **Date**: 2026-07-12

---

## 1. 全域 flags — `--version` / `-v`,`--verbose`

### 證據

**apm-go 完全沒有 root-level flags(除 `-h/--help`)。**

- `cmd/apm/main.go:15-18` — root `cobra.Command{Use: "apm-go", Short: "..."}`,無 `Version:` 欄位、無 `PersistentFlags()`。Cobra 只在 `Version` 欄位非空時才自動掛 `--version`;本專案從未設定,所以 `apm-go --version` 與 `apm-go -v` 在到達任何子指令前就是 `unknown flag`/`unknown shorthand flag` usage error。
- Live 驗證(2026-07-12):
  ```
  $ bin/apm-go.exe --version
  Error: unknown flag: --version
  ...
  EXIT=1
  ```
- 對照 Python `src/apm_cli/cli.py:108-131`:`@click.group` 上掛兩個 eager/global option:
  - `--version`(`is_flag=True, callback=print_version, is_eager=True`)→ `apm --version` 印 `Agent Package Manager (APM) CLI version 0.21.0 (a9a883b3)`,exit 0(live 驗證)。
  - `-v/--verbose`(`is_flag=True`)→ 寫入 `ctx.obj["verbose"]`,呼叫 `_configure_logging(verbose=True)`(`cli.py:66-105`),把 `apm_cli` logger 提到 DEBUG——**對每一個子指令**一視同仁生效(在子指令解析之前套用),且與環境變數 `APM_LOG_LEVEL` 是等效機制之一(`cli.py:72-73`)。

**apm-go 沒有等價的「全域」verbose 機制**——它退而求其次,在**部分**子指令上各自掛了獨立的 `-v/--verbose` flag(語意是「多印幾行」,不是 logging level):

| 子指令 | 有 `-v/--verbose`? | 證據 |
|---|---|---|
| `uninstall` | 有 | `cmd/apm/uninstall.go:21`,live help 確認 |
| `pack` | 有 | live help 確認(`-v, --verbose  print extra diagnostics`) |
| `marketplace {update,remove,validate,init,check,audit}` | 有(07-11 前 C1 修復) | `cmd/apm/marketplace.go:355,503,563,626` |
| `marketplace package {add,set,remove}` | 有(同上 C1) | `cmd/apm/marketplace_package.go:138,212,265` |
| `install` | **無** | live help 枚舉(2026-07-12)確認無 `-v`/`--verbose`;checklist IN 矩陣 item 6 早已記錄(✗ unknown flag),本次重驗**沒有 drift** |
| `update` | **無** | live help 確認,見 §2 |
| `compile` / `validate` / `normalize` / `experimental` / `audit`(整合驗證) / `init` | **無** | live help 逐一枚舉(2026-07-12)——全部只有 `-h/--help`(`init` 另有 `-y/--yes`) |

**apm-go 也沒有 `APM_LOG_LEVEL` 等價環境變數**——`cmd/apm` 內對 `os.Getenv`/`os.LookupEnv` 的呼叫僅 2 處(`mcpinstall.go:313` 讀 `MCP_REGISTRY_URL`、`mcp_prompt.go:30` 讀憑證 env 名清單),沒有任何 debug/verbose 相關 env 讀取——因為 apm-go 根本沒有結構化 logging 框架,只用直接 `fmt.Println`/`fmt.Fprintln`,「verbose」只是各子指令自行決定要不要多印幾行 `[-]`/`[i]` 之類的 transcript。

**版本字串側面確認**:apm-go 內部其實有一個寫死的版本字面值 `"0.1.0"`(`cmd/apm/install.go:690`,`newLock.APMVersion = "0.1.0"`,寫入 lockfile provenance),但從未透過任何 CLI 路徑暴露給使用者——沒有 build-time ldflags 版本注入,也沒有 `--version` 出口。`compile-contract.md` §4 已經記錄這個字面值會讓 apm-go 與 oracle 的 Build ID 哈希天生不同,但那是內部佐證,不是使用者可見的版本查詢。

**次要旁證(非本節主題,順帶記錄)**:apm-go 每個 (sub)command 都有 Cobra 自動掛的 `-h` 短旗標;Python 只有 `--help`,`apm -h` 直接是 `No such option: -h`——方向相反的小 EXTENSION,不構成問題,記錄即可。

### 分類 / 嚴重度 / 建議

| 項 | 類別 | 嚴重度 | 建議 |
|---|---|---|---|
| 全域 `--version` | **MISSING** | medium — 乾淨的 unknown-flag 失敗(不誤導),但違反幾乎所有 CLI 的基本慣例;support/bug-report 流程(「請貼 `apm --version` 輸出」)在 apm-go 上完全不可用 | 修:root `cobra.Command.Version` 欄位 + build-time ldflags 注入(`-X main.version=...`),順便把 install.go:690 的字面 `"0.1.0"` 換成同一個變數,兩處版本字串統一來源 |
| 全域 `-v/--verbose`(logging-level 語意) | **MISSING**(apm-go 用不對等的 per-command「多印幾行」flag 局部替代) | medium — 已涵蓋子指令(uninstall/marketplace/pack)體驗尚可,但 install/update 這兩個最常用指令完全沒有任何 verbose 出口,使用者無法「加 -v 看更多」的心智模型全域適用 | 決策:(a) 補一個真正 root-level `-v` 並下傳到目前缺 verbose 的子指令(install/update/compile/audit/validate),或 (b) 記錄為 documented deviation(apm-go 選擇 opt-in 逐指令加,不做全域 logging level) |
| `APM_LOG_LEVEL` 等價 env var | MISSING | low — 附屬於上一項,同一決策即可覆蓋 | 隨 verbose 決策一併處理或記錄不做 |
| root `-h` 有、Python 無 | EXTENSION | low(cosmetic,方向對使用者友善) | 記錄不做,無需修 |

---

## 2. `update` 殘餘 flag 面(07-11 未覆蓋部分)

### 範圍澄清

`.trellis/spec/backend/install-marketplace-contracts.md` §2 與 D1/D2/D3 已經覆蓋 **local-deps 物化** 與 **零 target 閘門** 兩塊——那是 `runUpdate` 內部「有 flag 之後」的資料流正確性。本節要盤的是**整個 flag/參數表面本身**,那塊完全沒被 07-11 觸碰。

### 兩邊 `update --help` 全 flag 對照(2026-07-12 live)

Python(`apm update --help`):

```
-y, --yes                     Skip the confirmation prompt (for CI / automation)
--dry-run                     Render the update plan and exit without changing anything
-v, --verbose                 Show unchanged deps and detailed pipeline diagnostics
-g, --global                  Refresh user-scope dependencies (~/.apm/) instead of the current project
--force                       Overwrite locally-authored files on collision
--parallel-downloads INTEGER  Max concurrent package downloads (0 to disable parallelism) [default: 4]
-t, --target TARGET           Agent target(s) to update for ...
--help
```
Args: `[PACKAGES]...`(可變數量、可多個具名套件同時更新)

apm-go(`update --help`,`cmd/apm/update.go:21-45`):

```
--frozen      refuse a scoped update against a frozen install (req-rs-012); auto-enabled in CI
-h, --help
--no-frozen   override CI auto-frozen detection to allow a scoped update
```
Args: `[package]`(`cobra.MaximumNArgs(1)` — `update.go:28`,**最多一個**具名套件)

**逐項對照**:

| Python flag | apm-go 現況 | 證據 |
|---|---|---|
| `-y/--yes` | 不存在 **且行為上不需要**——apm-go 從不詢問確認(見下方 D-1) | `update.go` 全文無 confirm/prompt 呼叫 |
| `--dry-run` | 不存在,無預覽模式 | 同上;`update.go` 沒有任何 dry-run 分支 |
| `-v/--verbose` | 不存在 | live help 確認 |
| `-g/--global` | 不存在(與 apm-go 全指令面「無 user-scope」的既有模式一致,uninstall 側已用 un-090/091 定案 A 明確拒絕;update 這裡連「明確拒絕」的錯誤路徑都沒有,直接 unknown flag) | live help 確認 |
| `--force` | 不存在。與 install 側既有已知限制一致(checklist item 5:「一般 collision-overwrite … 不存在」)——不是 update 特有的新缺口,是既有 install 缺口在 update 上的自然延伸 | — |
| `--parallel-downloads` | 不存在;apm-go 下載本來就是序列(checklist item 11 同款既有缺口) | — |
| `-t/--target` | **不存在,且原始碼自陳是已知 gap**:`update.go:198` 呼叫 `deploy.ResolveTargets("", m.Target, ".")`——`targetFlag` 參數永遠是空字串 | `cmd/apm/update.go:197-199` 註解原文:「apm-go's `update` has no --target flag (Python parity gap, out of this task's scope)」 |
| `PACKAGES...`(多個) | 只能一個(`MaximumNArgs(1)`) | `update.go:28` |

### D-1(新發現,本組首次記錄)—— `update` 完全沒有互動確認閘門(DIVERGENT-SAME-NAME)

這不是「缺一個 flag」而是**整條 consent-gate 邏輯在 apm-go 側不存在**,比殘餘 flag 清單本身更重要:

- Python `update.py:469-527` `_confirm_plan_application()` + `_plan_callback()`:
  - 若 `plan.has_changes == False`(沒有任何變更)→ 印「All dependencies already at their latest matching refs.」,**直接 return False,不寫 lockfile、不問**。
  - 若有變更且 `--dry-run`→ 印計畫,`return False`,不套用。
  - 若有變更、`--yes`→ 直接套用。
  - 若有變更、無 `--yes`、非互動 shell(`not _stdin_is_tty()`)→ `_rich_error("Cannot prompt for confirmation in non-interactive shell. Re-run with --yes to apply, or --dry-run to preview.")`,`sys.exit(1)`——**拒絕套用**。
  - 若有變更、互動 shell → `click.confirm("Apply these changes?", default=False)`,使用者答 `N` → 印「No changes applied.」,不套用。
- apm-go `runUpdate`(`update.go:47-208`)**全程沒有任何 prompt/confirm/stdin-tty 檢查**——不論有無變更、是否為互動 shell,一律直接算出 plan 就往下 `deployAndFinalize`,把新 lockfile 寫下去、重新部署檔案。`printUpdateSummary`(`update.go:248-277`)只是印出「哪些變了」,不是「要不要套用」的關卡。

**已被既有 spec 部分承接**:`install-marketplace-contracts.md` D3 已經提到「apm-go has no plan/consent gate」,但那段的落點是在解釋 D3(零 target exit 行為)的**成因**,不是把「整體無 consent gate」列為獨立項目。本組把它拉出來當作 crosscut 的一級發現,因為它解釋了為什麼 `-y`/`--dry-run`/非互動拒絕這三個 flag 在 apm-go 側「消失」不是三個獨立小缺口,而是同一根因的三個症狀。

**影響**:互動終端機下,使用者在 Python 跑 `apm update` 會先看到計畫再選擇是否套用;同樣情境下 `apm-go update` 不問就套用(改 apm.yml 派生的 apm.lock.yaml、重新部署檔案)。對自動化場景這其實更方便(等同永遠 `--yes`),但對互動使用者是一個「同名指令、不同安全語意」的落差——且沒有 `--dry-run` 可以先看不改。

### 分類 / 嚴重度 / 建議

| 項 | 類別 | 嚴重度 | 建議 |
|---|---|---|---|
| D-1:consent-gate 整體缺失 | **DIVERGENT-SAME-NAME** | **high** — 靜默套用而非拒絕/詢問,且無 `--dry-run` 逃生門,是本任務分類法定義的「最高風險」型態(使用者以為在 Python 上會被攔或被問,在 apm-go 上直接生效) | 至少補 `--dry-run`(獨立可做、風險最低、立即降低盲改風險);`-y`/互動 confirm 視 UX 決策另評(是否要讓 apm-go 對互動終端機也擋一下) |
| `--dry-run` | MISSING | high(隨 D-1 一併) | 同上,建議優先 |
| `-t/--target` | MISSING(原始碼自陳 out-of-scope) | medium — apm-go 全指令面中 `install`/`compile` 都有 `-t`,`update` 獨缺,是 apm-go **自身**指令面不一致,不只是對 Python 缺 | 建議補:`deploy.ResolveTargets` 早已支援,`update.go:200` 只要把 `""` 換成一個新 `--target` flag 值即可,改動面小 |
| `-v/--verbose` | MISSING | low-medium(併入 §1 全域 verbose 決策) | 隨 §1 決策 |
| `-y/--yes` | MISSING(但語意上 apm-go 永遠等同已 `--yes`) | 併入 D-1 | 併入 D-1 決策 |
| `-g/--global` | MISSING | low — 與 apm-go 全指令面「無 user-scope」既有模式一致,COVERED-ELSEWHERE 可引用 uninstall 側 un-090/091 定案 A 的精神(但 update 目前連明確拒絕訊息都沒有,只是 unknown flag) | 記錄不做(隨 user-scope 整體決策) |
| `--force` | MISSING | low — 與 install 既有缺口同根因 | COVERED-ELSEWHERE:checklist item 5 |
| `--parallel-downloads` | MISSING | low — apm-go 無下載併發層,既有缺口 | COVERED-ELSEWHERE:checklist item 11 |
| `PACKAGES...`(多個) vs 單一 `[package]` | PARTIAL | low-medium — 一次只能 scoped-update 一個套件,多套件更新需要跑多次 `update` | 若要修:`update.go:28` 的 `MaximumNArgs(1)` 放寬為多值 + `directGitSemverUpdateScope`/`PlanScopedUpdate` 需要接受多 pkg |

---

## 3. install / uninstall / marketplace — checklist 日期後 help drift 快篩

`cli-verification-checklist.md` 最後一次内容更新落在 2026-07-11(task 07-11-instructions-applyto-parity,§8;整份 75 項核心清單本體註記「產出自 … workflow(2026-07-09)」)。本組於 2026-07-12 對三個指令(含全部子指令)重新枚舉 `--help`,逐一與清單內文字比對。

### 方法與結果

- `bin/apm-go.exe install --help`(2026-07-12)——21 個 flag,逐一比對 checklist §1.1(36 項編號清單)與 §1.4(19 個缺失 flag 矩陣):**無新增、無消失**。特別確認 `--allow-insecure`(S1 修復)、`-t/--target`(F2 修復)、`--skill`/`--mcp` 系列均在,且沒有清單未記錄的新 flag 冒出。
- `bin/apm-go.exe uninstall --help`(2026-07-12)——`--dry-run`/`-g,--global`/`-h,--help`/`-v,--verbose` 四項,與 checklist §2(UN-02/03/04)逐字相符,**無 drift**。
- `bin/apm-go.exe marketplace --help`(2026-07-12)——12 個子指令(add/audit/browse/build/check/init/list/migrate/outdated/package/remove/update/validate),與 checklist §3/§4 涵蓋的子指令集合(consume 13 項 + authoring 18 項所涉及的子指令)**逐一比對無新增子指令**。`build` 仍是 tombstone(MK-13)。
- Python 側對照重跑(`apm install/uninstall/marketplace --help`,2026-07-12)——三邊輸出與 checklist 記載的 flag 清單、子指令清單**逐字相符**,包含 `install --help` 的完整 30+ flag(runtime/exclude/only/update/dry-run/force/frozen/verbose/trust-transitive-mcp/trust-canvas-extensions/parallel-downloads/dev/target/allow-insecure/allow-insecure-host/global/ssh/https/allow-protocol-fallback/mcp 系列/skill/no-policy/audit/no-audit/refresh/legacy-skill-paths/as/root)未見新增或移除。

### 結論

**兩邊在 2026-07-09/07-11 至 2026-07-12 之間,install/uninstall/marketplace 的 flag 面與子指令集合皆無 drift。** 這是一個「確認無變化」的陰性結果,不是新缺口。

### 分類 / 嚴重度 / 建議

| 項 | 類別 | 嚴重度 | 建議 |
|---|---|---|---|
| install/uninstall/marketplace flag-面 2026-07-12 快篩 | **COVERED-ELSEWHERE**(`cli-verification-checklist.md` 75 項,證實仍然有效、無 drift) | — | 無需另開;下次重大變更(install.go/uninstall.go/marketplace*.go 有新 commit)時才需要重跑本快篩 |

---

## 4. Exit code 慣例 与 錯誤輸出格式

### 4.1 訊息前綴符號(`[x]/[!]/[+]/[i]`)—— 已高度一致,非缺口

Python `utils/console.py:42-46` 定義:`info="[i]"`、`warning="[!]"`、`error="[x]"`、`check="[+]"`、`cross="[x]"`。apm-go side 廣泛沿用同一組符號(`grep -rn '\[x\]\|\[!\]\|\[+\]\|\[i\]' cmd/apm` 命中 115 處、23 個檔案,涵蓋 `install.go`/`uninstall.go`/`marketplace*.go`/`pack.go`/`init.go`/`audit.go` 等主要指令)。**這塊視覺慣例兩邊已經高度一致**,不是本任務要記的缺口——列出是為了让「exit code 慣例」這節聚焦在真正有落差的地方。

### 4.2 Exit code 階層 —— 架構性系統差異(非逐指令,是框架預設值差異)

**Python(click)三層**:
- `0`——成功
- `1`——一般錯誤(`click.ClickException`、未捕捉例外、`click.Abort`〔含使用者對確認提示答 N 之外的中止路徑,如 EOF〕、`sys.exit(1)` 的顯式呼叫)
- `2`——`click.UsageError`(及其子類)自動觸發,涵蓋兩種來源:(a) click 自身的 CLI 語法錯誤(`No such option`、`No such command`、缺必要參數)—— **click 框架預設值,每個子指令都適用,不用個別宣告**;(b) 專案自訂的語意驗證錯誤刻意繼承 `click.UsageError` 來借用 exit 2(例:`core/errors.py:22-55` 的 `TargetResolutionError`/`NoHarnessError`/`UnknownTargetError`/`AmbiguousHarnessError`/`ConflictingTargetsError`/`EmptyTargetsListError` 全部繼承 `click.UsageError`)。

**apm-go(cobra)預設只有兩層**:
- `0`——成功
- `1`——**cobra 框架預設值,對「所有」CLI 語法錯誤(unknown flag、unknown command、bad arg count)一律回這個值**——`main.go:32-34` 的 `root.Execute()` 錯誤路徑除非用 `withExitCode` 包過,否則 `exitCodeOf` 一律回 1(`exitcode.go:27-38`)。**這不是逐指令設定,是 cobra 沒有內建「usage error → 2」概念的框架級預設**,所以每一個 apm-go 子指令的 flag/arg 解析錯誤都固定是 1,除非該指令的 `RunE` 主動用 `withExitCode(2, …)` 包裝一個**語意層**(不是解析層)錯誤。
- apm-go 目前只有少數地方主動選擇性借用 2:`install.go:75`(`withExitCode(2, ...)`,寫入前置驗證)、`install.go:627-628`(`errNoDeployTarget()`,零 target 教學錯誤)、`compile.go:64`(target-family 不支援)、`marketplace_package.go:106,120,166,196,257`(`package add/set/remove` 的重複/未找到/互斥等語意編輯錯誤)。

**Live 驗證(2026-07-12,消除 pipe 遮蔽 exit code 的常見坑)**:

```
# 頂層未知指令
$ bin/apm-go.exe bogus-command   → EXIT=1   (cobra "unknown command")
$ apm bogus-command               → EXIT=2   (click "No such command")

# 子指令未知 flag(任兩個子指令皆同款,證明是框架級而非個別指令行為)
$ bin/apm-go.exe install --bogus-flag   → EXIT=1
$ apm install --bogus-flag              → EXIT=2
$ bin/apm-go.exe compile --bogus-flag   → EXIT=1
$ apm compile --bogus-flag              → EXIT=2
```

**這解釋了 checklist 裡「看起來零散」的多個 P2 項其實同一根因**:`C8`(uninstall 零 args exit 1 vs 2)、`IN-D6`(install unknown flag / usage error exit 1 vs 2)、`MA-18` 部分(package guard exit 2 vs 1——這項反過來,因為 apm-go 是**主動**選擇借用 2,不是框架預設,所以與其他「CLI 解析層」的項目方向相反,不要混為一談)。真正的框架級差異只有一條規則:**apm-go 的 CLI 解析錯誤(cobra 自動產生的 usage error)永遠是 1,Python 的 CLI 解析錯誤(click 自動產生的 usage error)永遠是 2**;任何語意層(業務邏輯)錯誤的 exit code 都是兩邊各自逐案決定,不受這條框架規則約束,個案差異已由既有 checklist/spec 逐項記錄,COVERED-ELSEWHERE。

### 分類 / 嚴重度 / 建議

| 項 | 類別 | 嚴重度 | 建議 |
|---|---|---|---|
| `[x]/[!]/[+]/[i]` 訊息前綴慣例 | PARITY-VERIFIED(視覺慣例已對齊,非正式逐字 A/B,但廣泛沿用同符號集) | — | 無動作 |
| CLI 解析層 exit code(unknown flag/command、bad arg count)永遠 1 vs 永遠 2 | **DIVERGENT-SAME-NAME**(框架級,適用於全部 13 個 apm-go 指令,不是單一指令的行為差異) | medium — 對人類使用者是 cosmetic(反正非 0 都代表失敗);對**腳本化 CI 判斷「是我打錯参数」還是「業務邏輯失敗」**的呼叫方是實質差異,因為 Python 腳本可以用 `if exit==2: print("usage error")` 分流,apm-go 目前做不到 | 修法路徑明確且風險低:cobra 支援 `rootCmd.SilenceErrors=true` + 自訂 `SetFlagErrorFunc`/在 `main()` 的 `root.Execute()` 錯誤路徑判斷是否為 `*cobra.Command` 解析錯誤(如 `strings.Contains(err.Error(), "unknown flag")` 太脆弱,建議用 cobra 0.9+ 的 `FParseErrWhitelist`/自訂 `FlagErrorFunc` 回傳一個統一被 `exitCodeOf` 辨識為 2 的 sentinel error);屬於一次性框架層修改,可覆蓋全部 13 指令,不需逐指令補丁 |
| 語意層(業務邏輯)exit code 個案差異 | COVERED-ELSEWHERE | — | 見 `cli-verification-checklist.md` C7/C8/IN-D6/MA-18、`install-marketplace-contracts.md` 全文;不重複記錄個案 |

---

## 本組總表

| # | 項目 | 類別 | 嚴重度 | 處置建議 |
|---|---|---|---|---|
| 1 | 全域 `--version` | MISSING | medium | 修:cobra `Version` 欄位 + ldflags 版本注入,統一 install.go:690 字面值來源 |
| 2 | 全域 `-v/--verbose`(logging-level 語意)+ `APM_LOG_LEVEL` 等價 | MISSING | medium | 決策:補真全域 verbose(尤其 install/update)或記 documented deviation |
| 3 | root `-h` 有(apm-go)/ 無(Python) | EXTENSION | low | 記錄不做 |
| 4 | `update`:consent-gate 整體缺失(D-1,含 `-y`/隱含永遠套用) | **DIVERGENT-SAME-NAME** | **high** | 至少補 `--dry-run`;`-y`/互動 confirm 另評 UX |
| 5 | `update --dry-run` | MISSING | high(併入 #4) | 優先修,風險最低、獨立可做 |
| 6 | `update -t/--target` | MISSING(原始碼自陳 gap,`update.go:197-199`) | medium | 建議修,改動面小(`deploy.ResolveTargets` 已支援) |
| 7 | `update -v/--verbose` | MISSING | low-medium | 併入 #2 決策 |
| 8 | `update -g/--global` | MISSING | low | 記錄不做,COVERED-ELSEWHERE(uninstall un-090/091 定案 A 精神) |
| 9 | `update --force` | MISSING | low | COVERED-ELSEWHERE:checklist item 5 |
| 10 | `update --parallel-downloads` | MISSING | low | COVERED-ELSEWHERE:checklist item 11 |
| 11 | `update` 只能單一 `[package]` vs Python `[PACKAGES]...` | PARTIAL | low-medium | 視需求放寬 `MaximumNArgs` |
| 12 | install/uninstall/marketplace 2026-07-12 help drift 快篩 | COVERED-ELSEWHERE(確認無 drift) | — | 無需動作,`cli-verification-checklist.md` 75 項仍然有效 |
| 13 | `[x]/[!]/[+]/[i]` 訊息前綴慣例 | PARITY-VERIFIED | — | 無動作 |
| 14 | CLI 解析層 exit code(1 vs 2,框架級) | **DIVERGENT-SAME-NAME** | medium | 一次性 cobra `FlagErrorFunc`/錯誤分流修法,覆蓋全部 13 指令 |
| 15 | 語意層 exit code 個案差異 | COVERED-ELSEWHERE | — | 見既有 checklist/spec,不重複記錄 |

---

## Caveats / Not Found

- `--version`/`-v` 的「build 需要」修法(ldflags 注入)未實測——本組只確認現況缺口與程式碼掛點(`main.go`、`install.go:690`),未驗證 CI/build 腳本目前是否已有版本號來源可接。
- `update` 的 `-t/--target` 修法只做了可行性判讀(`deploy.ResolveTargets` 簽名已支援 flag 參數),未實際改碼驗證(遵守審計唯讀原則,本任務不修復)。
- 沒有對 `update --force` 語意做即時 collision 實測(即故意製造一個手改檔案再跑 `update` 觀察是否覆蓋)——因為 checklist 既有的 install 側「--force 只在 --mcp 路徑有效」結論已經足以判定 update 這裡是同根因延伸,不需要重覆一次即時探測;若之後決定修復再重新驗證。
- exit code 章節的「一次性修法」只給出方向(cobra `FlagErrorFunc`/`SilenceErrors`),未落地成 PoC diff——本任務範圍是盤查不是修復。
