# Research: apm-go-only 指令(normalize / validate / completion)+ experimental flag 對照

- **Query**: apm-go 專屬指令 normalize、validate、completion(cobra 內建)與兩邊都有的
  experimental 逐 flag 對照;產出對映表(是否對應 Python 內部邏輯的 CLI 化、是否已在
  spec 記為 documented extension)。
- **Scope**: mixed(原始碼比對 + scratch TEMP live probe,兩邊皆用環境變數隔離狀態目錄)
- **Date**: 2026-07-12

## 方法與安全隔離說明

- apm-go 側:`APM_CONFIG_DIR=<TEMP scratch>` 覆蓋 `internal/experimental/experimental.go:52-62`
  的 config 路徑解析,不動真實 `~/.apm/config.json`。
- Python oracle 側:`core/experimental.py` 的 `CONFIG_FILE` 寫死
  `os.path.expanduser("~/.apm")`(`src/apm_cli/config.py:14-15`),**沒有** env 覆蓋點。改用
  `USERPROFILE`/`HOME` 指到 TEMP scratch 目錄(Python `os.path.expanduser` 在 Windows 走
  `USERPROFILE`),已驗證此法能讓 config 落在 scratch 而非真實 home
  (`$SCRATCH_PY/.apm/config.json`,見下方 transcript),故 `experimental enable/disable`
  才敢實跑(唯讀 `list` 本來就安全)。未對任何真正有狀態的危險指令(publish/self-update/
  config 寫入 real home/approve/deny/cache 清除/marketplace 寫入)做實跑。

---

## 指令 1:`normalize`(apm-go only)

- **類別**: EXTENSION
- **證據**:
  - Go:`cmd/apm/main.go:78-103`(`normalizeCmd`)—— `SafeLoad` 解析、`SafeDump` 重新序列化、
    寫回 stdout;`--stdout` flag 是 no-op("kept for runner compatibility"),`Args: cobra.ExactArgs(1)`。
  - 註冊點:`cmd/apm/main.go:21` `root.AddCommand(normalizeCmd())`。
  - 首次引入:commit `44c2d1f`("新增 validate 與 normalize 子命令"),無對應 task/PRD,
    commit message 僅寫「normalize --stdout 輸出 round-trip 序列化結果」。
  - Python 33 指令清單(`approve audit cache compile config deny deps doctor experimental
    find init install list lock marketplace mcp outdated pack plugin policy preview prune
    publish run runtime search self-update targets uninstall unpack update view`)**沒有**
    `normalize` 這個字。
  - Live probe(scratch,見下方 transcript):對一份合法 manifest round-trip,輸出位元完全
    等於輸入(排版保留),證實它是 `internal/yamlcore` 的 debug/round-trip 探針,不是使用者
    工作流指令。
  - 使用證據:`cmd/apm/main_test.go` 沒有 `normalize` 專屬測試(只有 `validate` 的
    `TestValidateCmd_*`,見下),`normalize` 目前無單元測試涵蓋,純粹靠人工 CLI 呼叫除錯。
- **對映**: 不對應任何 Python CLI 指令。內部語意對應 apm-go 自己的
  `internal/yamlcore.SafeLoad`/`SafeDump`(`.trellis/spec/backend/quality-guidelines.md:480`
  一併記載該函式簽名),用途是讓開發者/CI 手動檢查「解析→重新序列化」是否位元穩定,類似
  Python 端 `ruamel.yaml` round-trip 測試但從未 CLI 化成一個指令。
- **是否已記為 documented extension**: **否**。翻過 `.trellis/spec/backend/` 全部 9 份文件
  (`antigravity-target-contract.md`、`compile-contract.md`、`database-guidelines.md`、
  `directory-structure.md`、`error-handling.md`、`experimental-flags.md`、`index.md`、
  `install-marketplace-contracts.md`、`logging-guidelines.md`、`quality-guidelines.md`),
  以及 `.trellis/spec/conformance/openapm-v0.1.md`,均無一句提及 `apm-go normalize`
  或「此為 debug tooling」的說明。
- **嚴重度**: low(無使用者踩雷跡象;功能單純、無副作用、不寫檔案除非重導向 stdout)。
- **建議**: 記錄不做修復(Python 端無對應指令可比,不存在 parity 缺口)。建議在
  `.trellis/spec/backend/` 補一句話(例如附掛在 `quality-guidelines.md` 的 yamlcore 段落
  或新開一小節)明說 `apm-go normalize`/`validate` 是 dev-only CLI 化除錯工具、非 Python
  parity 對象,避免未來稽核者重複踩「這是不是缺口」的疑惑。此為文件補完,非 code 修復,
  可與 `validate` 一起另開一個小 task 或直接併入本次登記冊的 doc 動作。

### Live probe transcript(scratch,僅 apm-go,唯讀無副作用)

```
$ ./bin/apm-go.exe normalize sample.yml
name: demo
version: "1.0.0"
description: demo project
author: tester
dependencies:
  apm: []
  mcp: {}
includes: auto
```
(輸入輸出逐位元組相同 —— round-trip 穩定,符合 `--stdout` flag 註解所述行為。)

---

## 指令 2:`validate`(apm-go only)

- **類別**: EXTENSION(注意:Python 端有一個**同名但不同範疇**的 `marketplace validate`
  子指令,細節見下方「撞名說明」)
- **證據**:
  - Go:`cmd/apm/main.go:37-76`(`validateCmd`)—— 讀檔 → `yamlcore.SafeLoad` → 檢查
    top-level 必須是 Mapping → 若含 `lockfile_version` key(`manifest.NodeHasKey`)則短路
    直接放行(lockfile bypass)→ 否則跑 `manifest.ParseManifest` 並把 diagnostics 印到
    stderr(`warning: ...`),`Args: cobra.ExactArgs(1)`。
  - 測試:`cmd/apm/main_test.go:181-206` 三個測試直接對
    `conformance/conformance-kit/oracle/{lockfile,manifest}/*.yml` 固定 fixture 跑
    `validateCmd()`——`TestValidateCmd_LockfileBypass`(`lockfile/v1-git-only.yml` 應該被
    lockfile 短路接受)、`TestValidateCmd_InvalidManifest`(`manifest/invalid-missing-name.yml`
    應該報錯)、`TestValidateCmd_ValidManifest`(`manifest/valid-minimal.yml` 應該通過)。
    這證實 `validate` 的真實用途是**對 conformance-kit oracle fixture 跑內部
    parser 的迴歸測試探針**,不是設計給終端使用者的工作流指令。
  - 首次引入:同 `normalize`,commit `44c2d1f`。
- **撞名說明(不是 DIVERGENT-SAME-NAME,是範疇完全不同的兩個 "validate")**:
  - Python 頂層 33 指令**沒有** `validate`。但 Python `marketplace` 群組下有
    `apm marketplace validate NAME`(`src/apm_cli/commands/marketplace/validate.py:14-89`)——
    對**已註冊的 marketplace 來源**(用名字查,`get_marketplace_by_name`)發網路請求
    (`fetch_marketplace(..., force_refresh=True)`),驗證其 `plugins` 清單結構
    (`validate_marketplace`),支援 `--check-refs`(hidden,尚未實作)、`-v/--verbose`。
  - apm-go 也有 `marketplace validate NAME`(`cmd/apm/marketplace.go:578`,
    `Use: "validate NAME"`)—— 這個才是與 Python `marketplace validate` 同名同義的
    對應項,已被 75 項 conformance checklist 收錄為 **MK-10**
    (`.trellis/spec/conformance/cli-verification-checklist.md:343`,狀態 `◐`)——
    **COVERED-ELSEWHERE,本次不重掃**。
  - 本節討論的頂層 `apm-go validate <file>` 是完全不同的第三個東西:吃**任意本地檔案
    路徑**、驗證**YAML safe-subset + manifest schema**、**不碰網路**、**不查 marketplace
    註冊表**。它與 `marketplace validate NAME` 只是共享中文/英文字面詞「validate」,
    行為、輸入形態、副作用全不相同。因為兩者在各自 CLI 樹的不同層級(頂層 vs
    `marketplace` 子群組),cobra/click 兩邊都不會產生指令解析衝突,但**人類使用者**
    輸入 `apm-go validate` 時若心裡想著 Python 的 `apm marketplace validate` 語意,
    會得到完全不對應的結果——這是文件/認知風險,不是指令派發風險。
- **對映**: 不對應任何 Python 頂層指令。Python 對 manifest schema 的驗證邏輯**內嵌**在
  `install`/`lock`/`compile` 等指令的執行路徑中(如 `deps/plugin_parser.py`、
  `deps/package_validator.py`),從未獨立 CLI 化成一個「餵檔案路徑 → 驗證 schema」的
  頂層指令。apm-go 的 `validate` 是把「呼叫 `manifest.ParseManifest` 這個內部函式」
  包成 CLI,方便對 conformance fixture 做離線迴歸測試(見上方 `main_test.go` 證據)。
- **是否已記為 documented extension**: **否**(同 `normalize`,搜過 `.trellis/spec/backend/`
  全部文件與 `openapm-v0.1.md` 均無記載)。
- **嚴重度**: low(功能本身無害且有測試覆蓋;風險僅在於「與 `marketplace validate`
  撞字面詞」造成的認知混淆,尚未觀察到使用者實際踩雷紀錄)。
- **建議**: 記錄不做修復。建議比照 `normalize` 一併在 spec 補記,並在補記時**明確點出
  與 `marketplace validate NAME`(MK-10)的範疇差異**,避免未來稽核者誤判成
  DIVERGENT-SAME-NAME(兩者其實是不同 CLI 樹節點,不會真的衝突,但值得一句話澄清)。

### Live probe transcript(scratch,僅 apm-go,唯讀)

```
$ ./bin/apm-go.exe validate sample.yml     # dependencies.mcp 誤寫成 map 而非 list
Error: <scratch>/sample.yml: dependencies.mcp must be a list
exit=1

$ ./bin/apm-go.exe validate lock.yml       # { lockfile_version: 1, dependencies: {} }
exit=0   # 無輸出 —— lockfile 短路生效

$ ./bin/apm-go.exe validate bad.yml        # 只有 version,無 name
Error: <scratch>/bad.yml: name is required and must be a non-empty string
exit=1

$ ./bin/apm-go.exe validate list.yml       # 頂層是 YAML 序列而非映射
Error: <scratch>/list.yml: top-level must be a YAML mapping
exit=1

$ ./bin/apm-go.exe validate doesnotexist.yml
Error: <scratch>/doesnotexist.yml: open <scratch>/doesnotexist.yml: The system cannot find the file specified.
exit=1
```

---

## 指令 3:`completion`(cobra 內建,apm-go only)

- **類別**: EXTENSION(框架樣板功能,非手寫業務邏輯)
- **證據**:
  - apm-go:`apm-go completion --help` 列出 `bash|fish|powershell|zsh` 四個子指令,由
    `spf13/cobra`(`go.mod`:`github.com/spf13/cobra v1.10.2`)自動掛載——`main.go` 完全
    沒有手寫 `completionCmd()`,是 cobra `root.AddCommand` 樹自動附加的框架行為(標準
    `Command.InitDefaultCompletionCmd()`)。
  - Python:live probe 確認 **沒有** `apm completion` 子指令：
    ```
    $ apm completion --help
    Error: No such command 'completion'.
    ```
    Python 用的是 `click` 框架內建的**環境變數觸發**機制,不是子指令:
    ```
    $ _APM_COMPLETE=bash_source apm
    _apm_completion() {
        local IFS=$'\n'
        local response
        response=$(env COMP_WORDS="${COMP_WORDS[*]}" COMP_CWORD=$COMP_CWORD _APM_COMPLETE=bash_complete $1)
        ...
    }
    ```
    （click 標準 `shell_complete` 機制,無需在 `apm_cli/cli.py` 手寫任何程式碼即可運作。）
- **對映**: 功能面對等(兩邊都能產生 shell 自動完成腳本),但**觸發 UX 完全不同**——
  cobra 是使用者主動打 `apm-go completion bash > ...`；click 是透過
  `eval "$(_APM_COMPLETE=bash_source apm)"` 之類的環境變數協定。因為 Python 端根本沒有
  `completion` 這個字面指令,所以這**不是**同名指令的 parity 問題,純粹是 apm-go 選用
  cobra 框架後「白送」的頂層字,Python 選用 click 框架則沒有對應字面指令。
- **是否已記為 documented extension**: 否(未見於 spec),但這是 cobra 眾所皆知的樣板
  功能,業界慣例上通常不需要逐字記載。
- **嚴重度**: low。
- **建議**: 記錄不做。若要提升使用者體感一致性,可選擇性在使用文件(非 spec 契約層級)
  提一句「兩邊安裝 shell completion 的方式不同」,但非本次 audit 必要動作。

---

## 指令 4:`experimental`(兩邊都有,PARTIAL —— 逐 flag 對照)

- **類別**: PARTIAL(同名同義的子指令集合,但 flags registry、子指令數量、旗標覆蓋率均為
  Python 的子集;另外發現一個「同名但行為不同」的裸呼叫差異,見下方獨立小節)
- **證據(檔案位置)**:
  - Go 指令面:`cmd/apm/experimental.go:1-61`(`list` 16-29、`enable` 31-45、
    `disable` 47-58;三者皆無自訂 flag,只有 cobra 內建 `-h`)。
  - Go flags registry:`internal/experimental/experimental.go:28-34` —— 目前**只有一個**
    flag:`registries`。核心 API:`IsEnabled`(99-102)、`Enable`(104-115)、`Disable`
    (117-125)、`RequireEnabled`(127-133),全部走 `$APM_CONFIG_DIR/config.json`
    (`configPath` 52-62)。
  - Python 指令面:`src/apm_cli/commands/experimental.py`——group(146-158,
    `invoke_without_command=True`、群組層 `-v/--verbose`)、`list`(161-223,
    `--enabled`/`--disabled`/`-v/--verbose`/`--json`)、`enable`(226-253,`-v/--verbose`）、
    `disable`(256-281,`-v/--verbose`)、`reset`(284-362,`[NAME]` 可選 + `-y/--yes` +
    `-v/--verbose`)。
  - Python flags registry:`src/apm_cli/core/experimental.py:57-142` —— **九個** flags:
    `verbose_version`、`copilot_cowork`、`copilot_app`、`marketplace_authoring`、
    `registries`、`canvas`、`external_scanners`、`openclaw`、`hermes`。
  - apm-go 既有 spec:`.trellis/spec/backend/experimental-flags.md`(全文已讀)明確記載
    「CLI: `apm experimental list|enable|disable <flag>`」與兩條安全鐵則(不可 gate
    security control;不可 gate conformance-graded 行為,只能 gate oracle 尚未涵蓋的最小
    runtime 邊界,舉例 registry HTTP consumer)。**這代表 flags registry 子集是刻意設計,
    不是遺漏**——其餘 8 個 Python flag 各自對應 apm-go 尚未實作的 target/功能:
    `canvas`→`.apm/extensions/` bundle 部署(apm-go 無此 target)、`copilot_cowork`/
    `copilot_app`→ Copilot Cowork/App 部署(apm-go 無此 target,`--trust-canvas-extensions`
    在既有 checklist `.trellis/spec/conformance/cli-verification-checklist.md:67,212`
    也標「canvas 概念整體不存在」,COVERED-ELSEWHERE)、`marketplace_authoring`→ apm-go
    的 marketplace authoring 指令組本來就無條件存在（不像 Python 用 experimental flag
    閘門）、`external_scanners`→ apm-go 無 SARIF/external scanner 整合、`openclaw`/
    `hermes`→ apm-go 無這兩個 deploy target、`verbose_version`→ apm-go `--version`
    沒有這個 verbose 分支。**這 8 個 flag 的缺席是「功能本身不存在」的自然結果,不是
    experimental 子系統的 bug。**

### 4a. Flags registry 差異(1/9)—— 判定:符合既有 spec 設計,非缺口

| Python flag | apm-go 有此 flag? | 原因 |
|---|---|---|
| `verbose_version` | 否 | apm-go `--version` 無此擴充分支 |
| `copilot_cowork` | 否 | apm-go 無 Copilot Cowork target |
| `copilot_app` | 否 | apm-go 無 Copilot App target |
| `marketplace_authoring` | 否(但功能本身無條件存在) | apm-go marketplace authoring 指令組不走 flag 閘門 |
| `registries` | **是** | 唯一實作,對應 07-01-registry-consumer task |
| `canvas` | 否 | apm-go 無 canvas extension,checklist 已記「概念整體不存在」 |
| `external_scanners` | 否 | apm-go 無 SARIF/external scanner 整合 |
| `openclaw` | 否 | apm-go 無 OpenClaw target |
| `hermes` | 否 | apm-go 無 Hermes target |

### 4b. 子指令與 flag 覆蓋率差異(判定:MISSING / PARTIAL,有感知落差)

| 項目 | Python | apm-go | 差異判定 |
|---|---|---|---|
| `list` | `--enabled`/`--disabled`(互斥 filter)、`-v/--verbose`、`--json` | 無任何自訂 flag | **PARTIAL**(功能子集) |
| `enable <name>` | `-v/--verbose` | 無 | **PARTIAL** |
| `disable <name>` | `-v/--verbose` | 無 | **PARTIAL** |
| `reset [name]` | 存在,`-y/--yes`、`-v/--verbose` | **完全不存在** | **MISSING** |
| 群組層 `-v/--verbose` | 存在 | 不存在 | **PARTIAL** |
| 未知 flag 名稱時的建議訊息 | `difflib` 相似字建議("Did you mean: X?") | 只印 `unknown experimental feature "X"`,無建議 | **PARTIAL**(較弱的錯誤訊息) |
| `--json` 輸出 | list 支援結構化 JSON(含 `source: config/default`) | 不支援 | 與 apm-go **全域**沒有 `--json` 慣例一致(grep 全部 `cmd/apm/*.go` 找不到任一個 `--json` flag),非本指令獨有落差 |

### 4c. 裸呼叫行為差異(獨立標記為 DIVERGENT-SAME-NAME,附兩邊 transcript)

同一個指令詞 `experimental`,**不帶任何子指令**時,兩邊行為不同:

- Python:`ctx.invoked_subcommand is None` 時明確 `ctx.invoke(list_flags)`
  (`experimental.py:156-158`)——裸跑 = 印出目前的 flag 列表(即 `list` 的行為)。
- apm-go:`experimentalCmd()` group 沒有設定 `RunE`,cobra 對「有子指令、且無 Run」的
  group 預設行為是印 help、`exit 0`——裸跑 = 印 help,**不會**印出 flag 狀態。

```
$ ./bin/apm-go.exe experimental          # 裸呼叫
Manage experimental feature flags
Usage:
  apm-go experimental [command]
Available Commands:
  disable     Disable an experimental feature
  enable      Enable an experimental feature
  list        List experimental features and their status
...
exit=0

$ USERPROFILE=<scratch> apm experimental          # 裸呼叫(Python)
                             Experimental Features
┌───────────────────────┬────────────┬────────────────────────────────────────┐
│ Flag                  │ Status     │ Description                            │
├───────────────────────┼────────────┼────────────────────────────────────────┤
│ verbose-version       │ disabled   │ ...
...
[i] Tip: apm experimental enable <name>
exit=0
```

同樣是「同名指令 + 相同零參數呼叫方式」,一邊給你狀態列表、一邊給你 help 文字——兩者
都是 `exit 0`,不算報錯,但語意不同,判定為 **DIVERGENT-SAME-NAME**(裸呼叫這個具體
互動點),嚴重度定 medium(不是 high):不是資料損毀或危險誤導,單純是「使用者想查
flag 狀態卻打了裸指令」時體感不一致,且兩邊都不算失敗(exit 0),影響範圍侷限在
互動便利性。

### 4d. `experimental reset`(未知子指令)的靜默 fallback —— cobra 框架機制,非 apm-go 自訂 bug

```
$ ./bin/apm-go.exe experimental reset
Manage experimental feature flags
Usage:
  apm-go experimental [command]
...
exit=0

$ ./bin/apm-go.exe experimental bogus-sub     # 任何未知子指令都一樣
... (同上,印 help)
exit=0

# 對照:根指令層級的未知子指令行為不同 ——
$ ./bin/apm-go.exe bogus-toplevel-command
Error: unknown command "bogus-toplevel-command" for "apm-go"
Run 'apm-go --help' for usage.
exit=1
```

追查 `spf13/cobra@v1.10.2` 原始碼(`args.go:24-39`,`legacyArgs`)證實這是 cobra 的既定
設計:

```go
// legacyArgs validation has the following behaviour:
// - root commands with no subcommands can take arbitrary arguments
// - root commands with subcommands will do subcommand validity checking
// - subcommands will always accept arbitrary arguments
func legacyArgs(cmd *Command, args []string) error {
	if !cmd.HasSubCommands() {
		return nil
	}
	if !cmd.HasParent() && len(args) > 0 {   // 只有「無父層」的根指令才做未知子指令檢查
		return fmt.Errorf("unknown command %q for %q%s", ...)
	}
	return nil
}
```

即**只有根指令**(`!cmd.HasParent()`)才會對未知子指令報 `unknown command` 錯誤;任何
巢狀子群組(如 `experimental`、`marketplace`)遇到未知子指令,cobra 一律吞掉、印該群組
的 help、`exit 0`。這代表 `apm-go experimental reset`(Python 有這個子指令,apm-go
沒有)不會像打錯字一樣明確報錯,而是安靜地印出 help——若被腳本呼叫(如
`apm-go experimental reset --yes` 之類遷移腳本),腳本會看到 `exit 0` 誤判為成功。
這是 cobra 框架的通用行為(理論上 apm-go 全部巢狀群組都適用,非 `experimental` 專屬),
但因為 `reset` 是 Python 確實存在、apm-go 確實缺席的子指令,此處的「靜默無錯誤」放大了
MISSING 子指令的風險等級。

- **嚴重度**: medium(flags registry 子集本身低風險且有 spec 佐證;但 `reset` 子指令
  整體缺席 + list/enable/disable 全系列 flag 缺席 + 裸呼叫語意不同 + 未知子指令靜默
  fallback,四者疊加後,使用者從 Python 遷移過來時容易在腳本化/自動化場景踩雷)。
- **建議**:
  1. Flags registry 子集(4a):**維持現狀**,已有 `.trellis/spec/backend/experimental-flags.md`
     佐證是刻意設計,不需修。
  2. 子指令/flag 覆蓋率(4b)與裸呼叫語意(4c):**另開 task** 評估是否補
     `experimental reset`(至少支援 `apm-go experimental reset [flag]`)、`list` 的
     `--enabled`/`--disabled`/`-v`,以及群組裸呼叫預設印 `list`。`--json` 可暫緩
     （apm-go 目前全域無 JSON 輸出慣例,不宜只在這一個指令單獨加）。
  3. cobra 未知子指令靜默 fallback(4d)是框架級行為,不建議只為 `experimental` 一個
     群組客製修正;若要修,應該是「全 CLI 一致的未知子指令錯誤處理」的獨立 task
     （例如替每個有子指令的 group 設 `RunE`/`ValidArgs` 或自訂 `Args` 驗證器），
     超出本次 audit 範圍,僅記錄在此供後續 triage 參考。

---

## 對映表(本組總覽)

| apm-go 指令 | Python 對應 | 對應關係 | 已在 spec 記為 documented extension? |
|---|---|---|---|
| `normalize <file>` | (無) | apm-go 內部 `yamlcore.SafeLoad`/`SafeDump` round-trip 的 CLI 化除錯工具;Python 無對應字面指令,round-trip 驗證埋在其 YAML 函式庫測試裡,未 CLI 化 | 否 |
| `validate <file>` | (無頂層對應;`marketplace validate NAME` 是同字面詞但不同範疇的**另一個**指令,= MK-10,COVERED-ELSEWHERE) | apm-go 內部 `manifest.ParseManifest`(+ OpenAPM safe subset + lockfile bypass)的 CLI 化除錯/迴歸測試工具(見 `main_test.go` 對 `conformance-kit/oracle/manifest` fixture 的測試);Python 對應驗證邏輯內嵌在 install/lock/compile 執行路徑,未獨立 CLI 化 | 否 |
| `completion [bash\|fish\|powershell\|zsh]` | (無;click 用 `_APM_COMPLETE` 環境變數機制,無子指令,live probe 確認 `apm completion` → `No such command`) | 功能對等(shell 自動完成)但觸發機制/UX 不同;純 cobra 框架樣板,apm-go 未手寫任何邏輯 | 否(業界慣例通常不需個別記載) |
| `experimental list\|enable\|disable` | `experimental list\|enable\|disable\|reset`(同名同義,PARTIAL) | flags registry 1/9(其餘 8 個對應 apm-go 未實作的 target/功能,已有 spec 佐證);子指令 3/4(缺 `reset`);`list`/`enable`/`disable` 全系列缺自訂 flag;群組裸呼叫語意不同(help vs list);未知子指令 cobra 框架級靜默 fallback(help+exit0,非 apm-go 自訂) | **是** —— `.trellis/spec/backend/experimental-flags.md` 記載 CLI 面為 `list\|enable\|disable` 與 flag 子集的安全/範圍理由;但**未提及**缺 `reset`、缺 filter/verbose/json flag、裸呼叫語意差異這幾點,屬於既有文件未涵蓋的殘餘落差 |

## 本組總表

| 指令 | 類別 | 嚴重度 | 處置建議 |
|---|---|---|---|
| `normalize` | EXTENSION | low | 記錄不做(Python 無對應指令);建議補一句 spec 註記說明其為 dev-only CLI 化工具 |
| `validate`(頂層) | EXTENSION | low | 記錄不做;建議補記與 `marketplace validate NAME`(MK-10)撞字面詞但範疇不同的澄清,避免未來誤判為 DIVERGENT-SAME-NAME |
| `completion` | EXTENSION | low | 記錄不做(cobra 標準樣板功能) |
| `experimental` flags registry(1/9) | PARTIAL(設計內) | low | 維持現狀,已有 `experimental-flags.md` 佐證 |
| `experimental` 子指令/flag 覆蓋率(缺 `reset`、缺 filter/verbose flag) | MISSING(子指令)+ PARTIAL(flag) | medium | 另開 task 評估補 `reset` + `list`/`enable`/`disable` flag 對齊;`--json` 暫緩(全域無此慣例) |
| `experimental` 裸呼叫語意(help vs list) | DIVERGENT-SAME-NAME | medium | 另開 task 評估群組裸呼叫預設印 `list`,對齊 Python `invoke_without_command` 行為 |
| cobra 未知子指令靜默 fallback(`experimental reset` 等) | (框架級觀察,非單一指令缺口) | low(單看)/需注意(疊加 reset 缺席後放大風險) | 記錄不做修復本身(cobra 設計),但建議與 `reset` 缺口一併排入同一個修復 task 評估,因為兩者疊加會讓腳本誤判 `reset` 成功 |

## Caveats / Not Found

- 未找到任何既有 task/PRD 解釋 `normalize`/`validate` 頂層指令的設計動機——僅能從
  commit message(`44c2d1f`)與 `main_test.go` 的 fixture 用法反推其為 dev/CI 工具。
  若有更早的口頭決策未落檔,本文件的推論以現有可查證的程式碼與測試為準。
- cobra 未知子指令靜默 fallback(4d)只驗證了 `experimental` 群組與根指令兩個對照點;
  未逐一窮舉 apm-go 其餘巢狀群組(`marketplace`、`install`、`uninstall` 等)是否同樣受
  影響——依 cobra 原始碼邏輯(`args.go:24-39`)推斷應該對所有「非根、有子指令」的群組
  一致適用,但未逐一 live probe 驗證,留給後續 triage task 視需要覆核。
- Python `experimental list --disabled` 單獨過濾未逐一 live probe(僅測了 `list`、
  `list --json`、`list --enabled --disabled` 互斥錯誤、`enable`/`disable` 循環),因為
  `--enabled`/`--disabled` 對稱實作、風險極低,依程式碼閱讀(`experimental.py:192-198`)
  已足以判定行為,未列完整 transcript。
