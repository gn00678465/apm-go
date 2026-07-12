# Research: CLI 全指令面 parity 盤查 — group-project (init / config / targets / list / run / preview)

- **Query**: 本組指令 init（雙邊，行為對照）、config、targets、list、run、preview（Python only）。
  targets 特別注意 v2 target resolution（#1154）與 apm-go 無此指令的影響。compile 標
  COVERED-ELSEWHERE。
- **Scope**: mixed（internal 原始碼比對 + scratch live probe）
- **Date**: 2026-07-12

## 指令總覽（本組）

| 指令 | apm-go | Python | 類別 |
|---|---|---|---|
| init | 有 | 有 | **PARTIAL**（欄位/behavior 多處偏差，見下） |
| config | 無 | 有（get/set/unset group） | **MISSING** |
| targets | 無 | 有（group，`--json`/`--all`） | **MISSING**（high） |
| list | 無 | 有 | **MISSING** |
| run | 無 | 有 | **MISSING**（apm-go 自己的 init 卻教使用者打這個不存在的指令） |
| preview | 無 | 有（Python only） | **MISSING**（Python-only 功能） |
| compile | 有 | 有 | **COVERED-ELSEWHERE** — `.trellis/spec/backend/compile-contract.md`，不重掃 |

---

## 1. `init` — PARTIAL

### 證據：雙邊 --help

apm-go（`cmd/apm/init.go:22-170`，註冊於 `cmd/apm/main.go:22`）：
```
Usage:
  apm-go init [project-name] [flags]

Flags:
      --force           Overwrite existing apm.yml (alias for --yes on overwrite)
  -h, --help            help for init
      --target string   Comma-separated target list (skip prompt)
  -y, --yes             Skip interactive prompts and use auto-detected defaults
```

Python（`src/apm_cli/commands/init.py:64-89`，註冊於 `src/apm_cli/cli.py:170`）：
```
Usage: apm init [OPTIONS] [PROJECT_NAME]

Options:
  -y, --yes        Skip interactive prompts and use auto-detected defaults
  --plugin         (deprecated) Use 'apm plugin init' instead.
  --marketplace    (deprecated) Use 'apm marketplace init' instead.
  --target TARGET  Comma-separated target list (skip prompt, write directly)
  -v, --verbose    Show detailed output
  --help           Show this message and exit.
```

Flag 差異：
- apm-go 多一個 `--force`（Python 無此 flag，效果等同 `--yes` 覆蓋，低風險 EXTENSION）。
- apm-go 缺 `--plugin`／`--marketplace`（Python 端標記 deprecated，導向 `apm plugin init`／
  `apm marketplace init`）。apm-go 沒有 `plugin` 頂層指令（`plugin init` 屬於另一個未涵蓋指令，
  非本組），`marketplace init` apm-go 有（COVERED-ELSEWHERE, marketplace 75 項清單）。
  低-中嚴重度：功能路徑存在（marketplace init），純粹是 init 本身少了捷徑 flag，且 Python
  端本身在軟性淘汰中。
- apm-go 缺 `--verbose/-v`。

### 證據：scratch live probe — 產出 apm.yml 欄位差異

Scratch：`/tmp/apm-parity-init.EAVaiB/{go-init,py-init}`，兩邊皆 `init --yes`，同一 git
`user.name=Madao`。

**apm-go 輸出**（`cmd/apm/init.go:172-189` `buildManifestData`，`map[string]any` 經
`yamllib.Marshal` 序列化 → key 依字母序排序）：
```yaml
author: Madao
dependencies:
  apm: []
  mcp: []
description: APM project for go-init
includes: auto
name: go-init
scripts: {}
version: 1.0.0
```

**Python 輸出**（`src/apm_cli/commands/_apm_yml_writer.py` 経 `_create_minimal_apm_yml`，
呼叫點 `init.py:221`）：
```yaml
name: py-init
version: 1.0.0
description: APM project for py-init
author: Madao
# Which agent platforms to deploy to (uncomment to pin):
# targets:
#   - copilot
#   - claude

dependencies:
  apm: []
  mcp: []
includes: auto
scripts: {}
```

三個具體偏差：

1. **欄位順序**：apm-go 因為用 `map[string]any` + YAML marshal，永遠是字母序
   （author/dependencies/description/includes/name/scripts/version）；Python 是刻意的語意順序
   （name/version/description/author/[targets 註解]/dependencies/includes/scripts）。純格式差異，
   不影響語意，但每次 apm-go init 產出的檔案人類可讀性與 Python 慣例不同——低嚴重度，
   documented behavior 即可（除非要做逐字 diff 相容性）。
2. **無 targets 提示註解區塊**：Python 在未指定 `--target` 時會在檔案內留一段 comment
   說明 `targets:`/解析順序/可用值；apm-go 完全沒有等效輸出。純文件性缺口，低嚴重度。
3. **欄位名稱 `target` vs `targets`（見下方 §1a，這是本組最高嚴重度發現）**。

### 1a. `target:`（單數）vs `targets:`（複數）— **DIVERGENT，high**

Scratch：`--target copilot,claude`（`/tmp/apm-parity-init.EAVaiB/{go-init2,py-init2}`）：

apm-go 輸出（`init.go:172-189`：`data["target"] = targets`，鍵永遠是單數 `target`）：
```yaml
target:
  - copilot
  - claude
```

Python 輸出（v2 canonical field，`src/apm_cli/core/apm_yml.py:1-9` docstring 明載 #1154）：
```yaml
targets:
- claude
- vscode
```
（`copilot` 經 `TARGET_ALIASES = {"copilot": "vscode", ...}`
`src/apm_cli/core/target_detection.py:396-401` 正規化為內部 canonical `vscode`；apm-go 端
`TargetAliases = {"vscode": "copilot", "agents": "copilot", "agy": "antigravity"}`
（`internal/manifest/manifest.go:18-22`）方向相反，canonical 是 `copilot`。兩邊都接受兩種拼法作
input，這部分只是 canonical token 命名習慣不同，不是 bug。）

**真正的問題**：Python 自 #1154 起以 **`targets:`（複數）為 canonical schema**，`target:`（單數）
僅作為相容 sugar（`src/apm_cli/core/apm_yml.py:47-108` `parse_targets_field`，且明文禁止兩者並存：
`ConflictingTargetsError`，`src/apm_cli/models/apm_package.py:481-494` 同時保留兩個欄位值供
下游 gate 使用）。而 apm-go 的 `ParseManifest`（`internal/manifest/manifest.go:63-191`）**只認
`case "target":`（97 行），完全沒有 `case "targets":` 分支**——`targets:` 落入 157 行的
`default: // Unknown keys ... preserved by Node — no action needed`，**靜默忽略、零警告**。

**live 驗證**（scratch `/tmp/apm-parity-init.EAVaiB/plural-test`）：
```yaml
# apm.yml
name: plural-test
targets:
  - claude
  - copilot
```
```
$ apm-go validate apm.yml
EXIT=0   # 完全沒有任何警告，targets: 形同不存在
```

**影響鏈**：
- apm-go 自己的 `init.go:345-367 readExistingTargets()` 重新 init 既有專案時也只讀
  `doc["target"]`，若專案是被 Python（#1154 後版本）初始化、只有 `targets:`，apm-go 重新
  init 時會誤判「沒有 pin」，UI 顯示 auto-detect 結果而非真實既有 pin（雖然 confirm 面板
  仍可讓使用者發現不對，但預設值已經錯）。
- `compile`（COVERED-ELSEWHERE，不在此重扒 flag，但這是共用 manifest 解析層問題，不是
  compile 本身邏輯）與 `install`/`update` 同樣經由 `ParseManifest` 讀 target，理論上都會
  對一個只寫 `targets:`（Python v2 canonical）的專案完全忽略 pin、退回 auto-detect。
  `compile-contract.md:62` 自己寫的也是「`apm.yml target:`」（單數），代表既有 spec
  文件本身也還沒意識到 Python 端已經切換 canonical schema。

**建議**：另開 task——apm-go manifest parser 應同時接受 `targets:`（優先）與 `target:`
（legacy sugar，含 CSV 拆分，見 §1b），並鏡射 Python 的互斥錯誤語意，否則任何用新版
Python apm CLI 初始化、之後被 apm-go 讀取的專案，target pin 會被靜默吃掉。

### 1b. 單數 `target:` 底下的 CSV sugar — **DIVERGENT，medium**

Python `parse_targets_field`（`src/apm_cli/core/apm_yml.py:86-105`）明文支援
`target: "a,b"` → `['a','b']`（docstring 第 6 行標註 "CSV sugar"）；live 驗證：
```
$ uv run python -c "from apm_cli.core.apm_yml import parse_targets_field; \
    print(parse_targets_field({'target': 'claude,codex'}))"
['claude', 'codex']
```

apm-go 的 `parseTargetField`（`internal/manifest/manifest.go:193-213`）對 `ScalarNode` 只會把
整個字串丟給 `ValidateTarget()`，**不做逗號拆分**：
```
$ apm-go validate apm.yml   # apm.yml 內 target: "claude,codex"
Error: apm.yml: unknown target "claude,codex"
EXIT=1
```
一個在 Python 端合法、可正常初始化的 apm.yml，在 apm-go 端會直接讓整個 manifest 解析失敗
（`validate`/`install`/`compile` 全部會炸）。中等嚴重度（CSV-under-singular-key 是相對冷門
寫法，但一旦踩到是完全阻斷）。

### 1c. 非互動 stdin（無 `--yes`）下的行為 — **DIVERGENT，medium**（apm-go 較穩健）

Scratch（乾淨 pipe，非 `/dev/null` 這種會被誤判為 char device 的邊界情況）：
```
$ printf '' | apm-go init            # /tmp/apm-parity-init.EAVaiB/go-fresh-pipe
[*] APM project initialized successfully!
EXIT=0   # apm.yml 成功建立（用預設值）

$ printf '' | uv run apm init        # /tmp/apm-parity-init.EAVaiB/py-fresh-pipe
Project name (py-fresh-pipe): [x] Error initializing project: EOF when reading a line
EXIT=1   # apm.yml 未建立
```

原因：apm-go 的 target 選擇路徑會判斷 TTY（`init.go:191-197 isInteractive()`），非互動時走
auto-detect 分支；但 Python 的 `_interactive_project_setup`（`init.py:359-414`，被
`_perform_init` 在 `not yes` 時無條件呼叫，`init.py:192-193`）**完全不檢查 TTY**，直接呼叫
`click.prompt()`，遇到 EOF 就丟原始例外，被最外層 `except Exception as e:` 接住變成空白訊息
的 `Error initializing project: EOF when reading a line`，`sys.exit(1)`。這是 Python 自身的
一致性缺口（`init.py:470-530` 的 `_resolve_init_targets` 反而有做 TTY 檢查）——不是 apm-go
要修的東西，但構成本組要求的「行為對照」DIVERGENT 事實：同樣的「CI/腳本忘記加 `--yes`」
情境，apm-go 優雅降級成功、Python oracle 直接崩潰退出。建議：記錄不做（apm-go 行為已經
比 oracle 更穩健，無需往後相容一個 bug）。

**附註（次要邊界情況）**：apm-go 的 `isInteractive()`（`os.Stdin.Stat().Mode()&os.ModeCharDevice`）
會把 `< /dev/null` 誤判為互動 TTY（因為 `/dev/null` 本身也是 char device），導致
`init < /dev/null`（無 `--yes`）意外跑完整互動精靈（每題都因 `scanner.Scan()` 立即 EOF 而
吃到 default 值，最終仍成功 exit 0，只是多印一堆精靈輸出到 stderr）。低嚴重度：功能結果
正確（仍建出合法 apm.yml），純粹是雜訊輸出，且只在 `/dev/null` 這個特定重導向下發生
（一般 CI 用的空 pipe 不受影響，見上）。

### 1d. Author / description 自動偵測 — PARITY-VERIFIED

apm-go `manifest.DetectAuthor()`（`internal/manifest/detect.go:51-61`，`git config user.name`，
無值時 fallback `"Developer"`）與 Python `_auto_detect_author()`
（`src/apm_cli/commands/_helpers.py:559-575`，同樣邏輯）語意一致；live probe 兩邊皆正確讀出
`Madao`，description 預設樣板 `"APM project for {name}"` 兩邊逐字相同
（`_helpers.py:578-597` vs `init.go:77`）。判 PARITY-VERIFIED，不需處置。

---

## 2. `config` — MISSING

### 證據

Python `apm config`（`src/apm_cli/commands/config.py`，group，`invoke_without_command=True`，
註冊 `cli.py:182`）：
```
Usage: apm config [OPTIONS] COMMAND [ARGS]...
Commands:
  get    Get a configuration value
  set    Set a configuration value
  unset  Unset a configuration value
```
無子指令時（`config.py:111-241`）印出目前 apm.yml + 全域設定的 Rich table。

可設定 key（`config.py:64-85 _valid_config_keys`）：`auto-integrate`、`mcp-registry-url`、
`temp-dir`、`allow-protocol-fallback`、`prefer-ssh`；experimental flag 開啟後再加
`audit-on-install`、`external.<name>.llm`、`external.<name>.args`、
`copilot-cowork-skills-dir`、`registry.<name>.url/token/default`。

apm-go：`config config --help` → `Error: unknown command "config" for "apm-go"`
（`cmd/apm/main.go:14-35` 未註冊任何 `configCmd`）。

### 分析

apm-go 目前唯一持久化到 `~/.apm/config.json`（同一路徑慣例，`APM_CONFIG_DIR` 覆寫，
`internal/experimental/experimental.go:1-60`）的只有 `experimental enable/disable` 的旗標
狀態，沒有暴露任何其他 key 的讀寫指令。搜尋 `allow.protocol.fallback|prefer.ssh|temp.dir`
等字串在 apm-go 只出現在測試檔（`install_test.go` 等），production 程式碼未見對應功能
實作——即這些 config key 背後的功能本身（protocol fallback、SSH 偏好、temp-dir 覆寫、
registries 實驗性功能、external scanners、copilot-cowork）多數在 apm-go 尚未存在，`config`
整組指令的缺口與更大範圍的功能缺口一致，非單獨遺漏。

### 嚴重度與建議

**Medium**。多數底層功能本身未實作，`config` 指令本身的缺口是這些功能缺口的自然延伸；
但即使只是 `auto-integrate`（開關某種自動整合行為）這種較通用的 key，apm-go 使用者也完全
無法查詢/調整。建議：記錄不做，待對應底層功能（registries/external-scanners/
copilot-cowork/protocol-fallback）逐一補齊時再評估是否需要 `config` 入口；不需要單獨為
`config` 開 task。

---

## 3. `targets` — MISSING（**high**，本組重點）

### 證據：Python `--help`

```
Usage: apm targets [OPTIONS] COMMAND [ARGS]...

  Show resolved targets for the current project. If APM detects a target
  you don't intend (e.g. CLAUDE.md is documentation, not a Claude Code
  config), pin your targets explicitly in apm.yml.

Options:
  --json  Output as JSON instead of a table.
  --all   Include the agent-skills meta-target in JSON output.
```
（`src/apm_cli/commands/targets.py:23-46`，註冊 `cli.py:185`）

apm-go：`apm-go targets --help` → `Error: unknown command "targets" for "apm-go"`。

### v2 target resolution（#1154）與 `targets` 指令的關係

`src/apm_cli/core/target_detection.py:659-852` 是 #1154 引入的「v2 resolution algorithm」：
`resolve_targets(project_root, flag=None, yaml_targets=None)` 優先序
`flag > yaml_targets > auto-detect signals`，auto-detect 用 8 個 canonical 目標的
`SIGNAL_WHITELIST`（`target_detection.py:686-702`：claude/cursor/copilot/codex/gemini/
opencode/windsurf/kiro，多數目標有 2+ 個訊號檔案/資料夾），偵測到 0 個訊號丟
`NoHarnessError`、≥2 個不同目標且沒有 flag/yaml 覆寫時丟 `AmbiguousHarnessError`
（這兩個錯誤訊息本身就是「give the user actionable next step」設計，`render_no_harness_error`/
`render_ambiguous_error`）。

`apm targets` 指令原意就是把這整套 v2 resolution 的結果攤開給使用者看（表格：
TARGET/STATUS/SOURCE/DEPLOY DIR，或 `--json`），讓使用者在 `compile`/`install` 因
ambiguous-harness 卡住時，有地方可以查「apm 到底偵測到什麼」。

**apm-go 完全沒有這個指令**，對應到 apm-go 自己的 target 解析（`internal/deploy/adapter.go:98
ResolveTargets(flagTarget, manifestTargets, projectDir)`）：
- 沒有 `AmbiguousHarnessError`/`NoHarnessError` 概念，函式簽名顯示是簡單的
  flag→manifest→auto-detect 直接 fallback，不會在偵測到多個衝突訊號時主動報錯或攤開列表。
- `manifest.SignalWhitelist`（`internal/manifest/detect.go:22-28`）只有 **5 條訊號**
  （claude 的 `.claude/`+`CLAUDE.md`、copilot 的
  `.github/copilot-instructions.md`、codex 的 `.codex/`、opencode 的 `.opencode/`），
  **完全沒有 cursor/gemini/windsurf/kiro 的偵測訊號**——即使這些資料夾真的存在於專案裡，
  apm-go 的 auto-detect 也不會注意到，Python 端至少會列出來（`CANONICAL_TARGETS_ORDERED`
  含這 8 個）。
- 使用者在 apm-go 上没有任何方式回答「為什麼我的 compile 選了這個 target／為什麼沒選」，
  只能看 compile 的錯誤訊息（COVERED-ELSEWHERE，不在此重扒），若 compile 本身沒有給出等效
  診斷資訊，這是純粹的可觀測性缺口。

### 重要 caveat：Python 自己的 `targets` 指令其實有 bug，不是真的「resolved」

live 驗證（scratch `/tmp/apm-parity-init.EAVaiB/py-targets-probe`，apm.yml 明確寫
`targets: [claude]` 但沒有 `.claude/`/`CLAUDE.md`）：
```
$ apm targets
  TARGET       STATUS     SOURCE   DEPLOY DIR
  claude       inactive   needs CLAUDE.md   .claude/
  ...（其餘同樣全部 inactive）
```
`commands/targets.py:70` 呼叫的是 `resolve_targets(project_root)`——**沒有傳
`yaml_targets` 參數**，等於完全略過 apm.yml 裡的 pin，只看檔案系統訊號。對照
`grep resolve_targets(` 全庫結果：`install/phases/targets.py:588` 與
`integration/mcp_integrator.py:1098` 都正確帶入
`resolve_targets(project_root, flag=flag, yaml_targets=yaml_targets)`；但
`commands/targets.py:70`（CLI 指令本身）與 `policy/ci_checks.py:498` 都只傳
`project_root`。也就是說 **Python 的 `apm targets` 指令本身沒有真正反映
`apm.yml` 的 pin**，跟它自己的 docstring「Show resolved targets for the current
project」不符——它其實只顯示「若沒有任何 pin，auto-detect 會挑到什麼」。

**這個 caveat 不改變 MISSING 的判定，但降低了「apm-go 缺這個指令」造成的實際使用者痛感**：
即使 apm-go 補上等效指令，若逐字模仿 Python 現況，一樣不會反映 apm.yml pin（除非順便修這個
上游 bug）。因此處置建議請一併納入「是否要做得比 oracle 更正確」的產品判斷。

### 附帶發現：`apm targets set` 是 Python 自己的死引用

`init.py:634` 互動精靈提示文字寫「with 'apm targets set <target,...>'」，但
`commands/targets.py` 是一個 `@click.group`，**目前沒有任何 `@targets.command` 子指令**
（live 驗證：`apm targets set --help` → `Error: No such command 'set'.`）。純粹是 Python
自身文件債，不是 apm-go 要對齊的目標（apm-go 也不需要做出一個 Python 自己都沒有的
`targets set`）。

### 嚴重度與建議

**High**（依 PRD 指定）。核心理由：這是 v2 target resolution（#1154）唯一的使用者可視化
入口，apm-go 完全沒有，且 apm-go 自己的 auto-detect 訊號表比 Python 窄（5 vs 8 個目標）、
manifest 解析層又不認得 Python v2 canonical 的 `targets:`（複數，見 §1a）——三個問題疊加，
使用者在 apm-go 上完全沒有辦法診斷「我的 target 到底是怎麼被決定的」。建議：另開 task，
範圍應包含（a）補 `apm-go targets` 唯讀查詢指令、（b）補齊 `SignalWhitelist` 到 8 個目標、
（c）與 §1a 的 `targets:`/`target:` 雙鍵解析一併處理（同一個 manifest 層改動）。是否要修正
Python 自身「忽略 apm.yml pin」的 bug 由使用者/PM 決定是否要「做得比 oracle 好」。

---

## 4. `list` — MISSING

### 證據

Python `apm list --help`：
```
Usage: apm list [OPTIONS]
  List available scripts in the current project
```
（`src/apm_cli/commands/list_cmd.py:19-101`，註冊 `cli.py:181` 為 `list`）

行為：讀 `apm.yml` 的 `scripts:` 區塊（`_helpers.py:546-551 _list_available_scripts`），
無 script 時印出範例 panel 教怎麼寫 `scripts:`；有 script 時列表格，`start` 若存在標記為
default script。

apm-go：`apm-go list --help` → `Error: unknown command "list" for "apm-go"`。

### 分析

apm-go 的 `internal/manifest/manifest.go:113-114` 其實**有**解析 `scripts:` 區塊
（`m.Scripts = parseStringMap(val)`），但全庫搜尋 `m.Scripts`/`.Scripts` 沒有任何其他讀取
點——也就是說 apm-go 的 `Manifest.Scripts` 欄位目前是**死欄位**：解析出來但沒有任何消費者。
apm-go 自己的 `init.go:187`（`buildManifestData`）仍然固定寫入 `scripts: {}` 到每個新專案的
apm.yml，延續一個目前完全沒有功能意義的欄位。

### 嚴重度與建議

**Medium**。`list` 本身功能簡單（純讀取 + 格式化），但它依賴的 `run`（見下）才是真正的
執行邏輯，兩者通常要一起補。單獨看 `list` 缺失只是「看不到已定義的 script 清單」，不算
危險，但與 `run` 一起看，代表 apm.yml 的 `scripts:` 區塊在 apm-go 上完全不可用
（寫了也没用）。建議：另開 task，與 `run` 一併規劃（是否要做 npm-like script runner
是產品範圍決策，不是純 parity 修復）。

---

## 5. `run` — MISSING（附帶 apm-go 自我矛盾的高嚴重度發現）

### 證據

Python `apm run --help`：
```
Usage: apm run [OPTIONS] [SCRIPT_NAME]
  Run a script with parameters (experimental)
Options:
  -p, --param TEXT  Parameter in format name=value
  -v, --verbose     Show detailed output
```
（`src/apm_cli/commands/run.py:20-92`，註冊 `cli.py:179`）

行為：無 `script_name` 時退回 `start` script（`_get_default_script`）；透過
`ScriptRunner`（`src/apm_cli/core/script_runner.py`，1135 行）執行——含 explicit script
優先、`.prompt.md` 檔案自動探索/自動編譯（呼叫 compile 管線產出 `.txt` 到
`.apm/compiled/`）、virtual package 自動安裝、runtime 偵測（`find_runtime_binary`）等，是
一套完整的 npm-script-like 子系統。

apm-go：`apm-go run --help` → `Error: unknown command "run" for "apm-go"`。

### apm-go 自我矛盾（高嚴重度）

`cmd/apm/init.go:162`：
```go
fmt.Fprintln(os.Stderr, "  * Run a script:       apm-go run <script>")
```
apm-go 自己的 `init` 指令在「Next steps」裡教使用者打 `apm-go run <script>`，但這個指令
**根本不存在**（live 驗證，見上）。這不是「跟 Python 比較」的落差，是 apm-go **自己對自己
使用者說謊**——每一個跑過 `apm-go init` 的人，只要照著提示打 `apm-go run xxx`，第一件事
就是撞到 `Error: unknown command "run"`。

### 嚴重度與建議

**High**（因自我矛盾的 broken promise，而非單純跨工具 parity 落差）。建議二選一，優先度高：
（a）短期：把 `init.go:162` 那行提示拿掉或改成不承諾 `run` 存在的措辭（1 行改動，立即消除
UX 破洞）；（b）長期：另開 task 評估是否要實作 `run`/`list`（連同 `scripts:` 死欄位一起
復活）。(a) 建議立刻做，不需要等完整 `run` 實作。

---

## 6. `preview` — MISSING（Python only）

### 證據

Python `apm preview --help`：
```
Usage: apm preview [OPTIONS] [SCRIPT_NAME]
  Preview a script's compiled prompt files
Options:
  -p, --param TEXT
  -v, --verbose
```
（`src/apm_cli/commands/run.py:95-209`，與 `run` 同檔，註冊 `cli.py:180`）

行為：`run` 的 dry-run 版本——只顯示 original command / compiled command（`.prompt.md` →
`.apm/compiled/*.txt` 的自動編譯結果）與會產生的檔案清單，不實際執行，最後提示
`Use 'apm run {script_name}' to execute.`。

apm-go：無對應指令，且無 `run` 可依附。

### 嚴重度與建議

**Low**（相依於 `run`）。`preview` 本身是 `run` 的除錯／預覽輔助功能，若 `run`/`list` 不做，
`preview` 自然也不用做。建議：記錄不做，待 `run`/`list` 的 task 決議後一併評估，不需要
獨立追蹤。

---

## 7. `compile` — COVERED-ELSEWHERE

依任務指示不重掃 flag 級別對照。既有覆蓋：`.trellis/spec/backend/compile-contract.md`。
本次研究僅在追查 §1a/§3 的 manifest `target`/`targets` 欄位解析問題時，注意到
`compile-contract.md:62`「Reuses `deploy.ResolveTargets` unchanged (flag > apm.yml
target: > ...)」這段描述本身仍以單數 `target:` 為前提，尚未反映 Python v2 canonical
schema（`targets:` 複數）——**不在此重新驗證 compile 的 flag 行為**，僅作為 §1a 修復
task 的旁證留存，實際 compile flag/行為對照請沿用該文件既有結論。

---

## 本組總表

| 指令/子項 | 類別 | 證據 | 嚴重度 | 處置建議 |
|---|---|---|---|---|
| init（整體） | PARTIAL | `init.go:22-170` vs `init.py:64-121`；scratch `/tmp/apm-parity-init.EAVaiB/{go-init,py-init}` | — | 見下列子項 |
| init：欄位順序（字母序 vs 語意序） | PARTIAL | `init.go:172-189`（map 字母序）vs `_apm_yml_writer.py` | Low | 記錄不做（格式差異，不影響語意） |
| init：缺 targets 提示註解 | PARTIAL | 同上 scratch | Low | 記錄不做（純文件性） |
| init：`target:` 單數 vs `targets:` 複數 canonical schema | **DIVERGENT-SAME-NAME** | `init.go:172-189` + `manifest.go:97,155-157` vs `apm_yml.py:1-108`（#1154）；scratch `go-init2`/`py-init2`；live `plural-test` 驗證 apm-go 靜默忽略 `targets:` | **High** | 另開 task：manifest parser 同時支援兩鍵，鏡射互斥錯誤語意 |
| init：單數 `target:` 下 CSV sugar（`"a,b"`） | DIVERGENT | `manifest.go:193-213`（不拆分）vs `apm_yml.py:86-105`（拆分）；live 驗證 apm-go `validate` 直接報錯 | Medium | 與上一項一併修（同一段 parseTargetField） |
| init：非互動 stdin 無 `--yes` | DIVERGENT | `init.go:191-197` vs `init.py:359-414`(`_interactive_project_setup` 未查 TTY)；scratch `go-fresh-pipe`(exit 0) vs `py-fresh-pipe`(exit 1, EOF) | Medium | 記錄不做——apm-go 行為已優於 oracle，不需回頭相容 Python 的 crash |
| init：`isInteractive()` 對 `/dev/null` 誤判為 TTY | 邊界 bug | `init.go:191-197`；`init < /dev/null` 意外跑完整精靈但仍 exit 0 | Low | 記錄不做（結果仍正確，只是多印雜訊） |
| init：`--plugin`/`--marketplace`/`--verbose` flag 缺失 | PARTIAL | `--help` 對照 | Low-Medium | 記錄不做（Python 端已軟性淘汰；marketplace init 有替代路徑，COVERED-ELSEWHERE） |
| init：author/description 自動偵測 | PARITY-VERIFIED | `detect.go:51-61` vs `_helpers.py:559-597`；live probe 兩邊皆 `Madao` | — | 無需處置 |
| config | MISSING | `config.py:111-665` vs `main.go` 未註冊；`apm-go config --help` → unknown command | Medium | 記錄不做，待底層功能（registries/external-scanners/protocol-fallback）落地再評估 |
| targets | MISSING | `targets.py:23-136`（v2 resolution `target_detection.py:659-852`，#1154）vs 無對應；`internal/deploy/adapter.go:98` 無 ambiguous/no-harness 概念；`manifest/detect.go:22-28` 只 5 個訊號 vs Python 8 個 | **High** | 另開 task：補查詢指令 + 補齊 SignalWhitelist 8 目標 + 與 `target`/`targets` 雙鍵解析一併處理 |
| targets：Python `apm targets` 本身未讀 apm.yml pin（bug caveat） | caveat，非判定項 | `commands/targets.py:70` vs `install/phases/targets.py:588`／`mcp_integrator.py:1098` 正確帶入 `yaml_targets` | — | 併入上一項 task 的產品決策（是否要做得比 oracle 正確） |
| targets：`apm targets set` 為 Python 自身死引用 | caveat，非判定項 | `init.py:634` 提示 vs `targets.py` 無 `set` 子指令；live 驗證 `No such command 'set'` | — | 不影響 apm-go 對齊範圍 |
| list | MISSING | `list_cmd.py:19-101` vs 無對應；`manifest.go:113-114` 有解析 `scripts:` 但無消費者（死欄位） | Medium | 另開 task，與 `run` 一併規劃 |
| run | MISSING（+ 自我矛盾） | `run.py:20-92` vs 無對應；`init.go:162` 提示 `apm-go run <script>` 但指令不存在 | **High**（因自我矛盾） | 短期：拿掉/改寫 `init.go:162` 提示（立即）；長期：另開 task 評估是否實作 |
| preview | MISSING（Python only） | `run.py:95-209` vs 無對應，且無 `run` 可依附 | Low | 記錄不做，隨 `run`/`list` task 一併決議 |
| compile | COVERED-ELSEWHERE | `.trellis/spec/backend/compile-contract.md` | — | 不重掃；註記該文件的 `target:` 描述可能需隨 §1a task 更新 |

## Caveats / Not Found

- 未深入 `install`/`update`/`uninstall`/`mcp integrator` 內部如何具體消費
  `manifest.Target`（apm-go）或 `target_value`/`targets_value`（Python），因這些屬於
  COVERED-ELSEWHERE 範圍（install/uninstall/marketplace 75 項清單、update 07-11 child）；
  §1a/§3 的發現僅止於 manifest 解析層與 `init`/`targets` 指令本身。
- `codex agents TOML`（§9，07-12-codex-agent-toml）與本組指令無直接關聯，未重掃。
- 全部 live probe 均在 `/tmp/apm-parity-init.EAVaiB/` 下自建 scratch（Windows Git Bash
  `/tmp` 對應臨時目錄），未觸碰 apm-go/apm/evals 任一專案根目錄；`config`/`targets`（寫入
  子指令，若未來實作）、`marketplace add/remove` 等有狀態指令本次僅做 `--help` + 原始碼
  分析，未實跑會改真實狀態的操作。
