# Research: integrity 組（audit / doctor / policy / approve / deny）

- **Query**: 盤查 apm-go 對 Python apm 在完整性/治理/安全批准面的 5 個指令：`audit`（兩邊都有，疑似 DIVERGENT）、`doctor`、`policy`、`approve`、`deny`（後四者 Python only）。判類別、附證據（file:line + live probe transcript）、給嚴重度與處置建議。
- **Scope**: mixed（原始碼靜態分析 + TEMP scratch live probe，全程唯讀/scratch，未動任一方 repo 根目錄或 evals/test1）
- **Date**: 2026-07-12

## 摘要結論（先講重點）

1. **`audit` 是 DIVERGENT-SAME-NAME，且已用兩邊實跑證實**：apm-go `audit`（bare，唯一 flag 是 `-h`）只做 lockfile SHA-256 內容完整性重驗；Python `apm audit`（bare，21 個 flag）只做隱藏 Unicode 掃描 + 預設開啟的 drift 偵測，**完全不驗 SHA hash**。同一份被竄改的檔案：apm-go `audit` exit 1 攔下，Python bare `apm audit` exit 0「no issues found」放行。Python 真正等價於 apm-go `audit` 的邏輯藏在 `apm audit --ci` 的第 6 項檢查 `content-integrity` 裡，且該檢查前面還疊了 7 層檢查（fail-fast 預設會擋在前面，需要 `--no-fail-fast` 才看得到）。
2. **`doctor` 是 MISSING**，純診斷/資訊性指令（git/network/auth/marketplace config 健檢），嚴重度低——不影響任何操作的正確性或安全性，純粹是故障排除體驗落差。
3. **`policy` 是 MISSING，且不只是少一個指令**：apm-go 完全沒有 Python 的 org-policy（`apm-policy.yml`）治理層——沒有 discovery（org/URL/file + cache + extends chain）、沒有 dependencies/mcp/compilation/manifest/unmanaged-files 的 allow/deny 規則引擎，`install`/`audit` 都沒有 `--policy`/`--no-policy`/`--no-cache` flag。這代表任何組織用 `apm-policy.yml` 做集中治理（例如禁用某些依賴、限制 MCP transport、限制 compile target），**在 apm-go 上會靜默完全不生效**。嚴重度高，且範圍超出單一指令，建議另開 task 專門評估。
4. **`approve`/`deny` 是 MISSING，且不只是少兩個指令**：apm-go 完全沒有 Python 的 executable-primitives 批准閘門（`allowExecutables` block 解析、deny-by-default 執行、hook/bin 部署前檢查）。apm-go 目前無條件部署所有 hook/bin primitive（`internal/deploy/primitive.go` 的 `extractHookName` 等收集邏輯完全沒有批准檢查）。對於曾在 Python 側啟用 `allowExecutables:` 閘門的專案，遷移到 apm-go 後該閘門會**靜默失效**（YAML 區塊還在，但沒人讀它、沒人執行它），造成假的安全感。嚴重度高，建議另開安全性 task。

---

## `audit`

### 類別：DIVERGENT-SAME-NAME（已用兩邊實跑證實，非推測）

### 證據

**apm-go 原始碼**
- `cmd/apm/audit.go:15-56` — `auditCmd()`：唯一邏輯是讀 `apm.lock.yaml` → `lockfile.ParseLockfile` → `lockfile.VerifyDeployedState(lock, ".")` → 印出每個違規（`content-integrity violation: <path> expected <hash>, observed <hash>`）→ 有違規則 `Error: audit failed: N content-integrity violation(s)` exit 1；全部通過則印 `audit: N deployed files verified` exit 0。
- `internal/lockfile/audit.go:16-50` — `VerifyDeployedState`：對 lockfile 每個 dependency 的 `DeployedHashes` + `LocalDeployedHashes` 重算 SHA-256 並比對，缺檔視為違規（Observed 為空）；非 sha256 envelope 一律 fail closed。
- `--help` 輸出（實跑）：只有 `-h, --help`，**無 `--ci`/`--strip`/`--file`/`--format`/`--policy`/`--no-drift`/`--external` 等 21 個 Python flag 中的任何一個**。
- `apm-go audit` 也被 `install --frozen` 重用：`cmd/apm/install.go:365-373` 在任何網路存取前先呼叫同一個 `lockfile.VerifyDeployedState`，等於 apm-go 的 frozen install 同時做結構檢查（`lockfile.CheckFrozenInstall`, `internal/lockfile/frozen.go:34-54`：每個 manifest dep 都要有 lock entry）**和** SHA 內容驗證。

**Python 原始碼**
- `src/apm_cli/commands/audit.py:1039-1296` — `audit` command：bare 模式（`_audit_content_scan`, line 807-1034）＝隱藏 Unicode 掃描（`ContentScanner`/`scan_lockfile_packages`）＋預設開啟的 drift 偵測（`_check_drift`, cache-only install-replay diff）。**不含任何 SHA-256 hash 比對邏輯**。
- `--ci` 模式（`_audit_ci_gate`, line 513-700）才叫 `run_baseline_checks`（`src/apm_cli/policy/ci_checks.py`），內含 8 項檢查（fail-fast 依序）：`lockfile-exists` → `ref-consistency` → `deployed-files-present` → `no-orphaned-packages` → `skill-subset-consistency` → `config-consistency` → **`content-integrity`**（line 619，即 `_check_content_integrity`, line 280-375：同時做隱藏 Unicode 掃描 + `compute_file_hash` 逐檔 SHA-256 比對 `deployed_file_hashes`）→ `includes-consent` → 選配 org-policy 檢查（`run_policy_checks`）。
- `src/apm_cli/commands/install.py:1581-1590` 的程式碼註解本身就承認這個心智模型陷阱：`--frozen` 只驗「LOCKFILE STRUCTURE」，並印出提示「Run 'apm audit' for on-disk content integrity」——但這句提示**與 bare `apm audit` 的實際行為不符**（見下方實跑證明，bare `apm audit` 不會抓到 hash 被竄改的檔案）。

### Live probe transcript（TEMP scratch，雙方對照，同一份被竄改的檔案）

Scratch 建置（`$TEMP/apm-go-audit-scratch-*`，非 repo 內）：
```
.claude/skills/file.txt = "hello"
apm.lock.yaml:
  lockfile_version: "2"
  dependencies:
    - repo_url: github.com/demo/pkg
      source: git
      deployed_files: [.claude/skills/file.txt]
      deployed_file_hashes:
        .claude/skills/file.txt: sha256:2cf24dba5fb0a30e26e83b2ac5b9e29e1b161e5c1fa7425e73043362938b9824
apm.yml:
  name: scratch-project
  version: 0.1.0
  dependencies:
    apm: [github.com/demo/pkg]
```

之後執行 `printf 'TAMPERED' > .claude/skills/file.txt`（雜湊變成 `06c9c46ca6d4...`，未更新 lockfile），再分別跑：

**apm-go audit（bare）**
```
$ apm-go.exe audit
content-integrity violation: .claude/skills/file.txt expected sha256:2cf24dba5fb0a30e26e83b2ac5b9e29e1b161e5c1fa7425e73043362938b9824, observed sha256:06c9c46ca6d42f6ee7994823417b7955695508131e8f21ec46ad2e3351af148a
Error: audit failed: 1 content-integrity violation(s) (first: .claude/skills/file.txt)
EXIT=1
```

**Python apm audit（bare，同一目錄，apm.yml 已存在）**
```
$ uv run apm audit
[>] Scanning all installed packages...
[>] Replaying install (cache-only)...
[!] drift skipped: install cache not populated (run 'apm install' first or pass --no-drift)

[*] 1 file(s) scanned -- no issues found
EXIT=0
```
→ **竄改被完全放行**（drift 檢查也因為沒有真的跑過 `apm install` 建立 cache 而被跳過；即使跑了，drift 是 install-replay diff，不是逐檔 SHA 比對）。

**Python apm audit --ci（同一目錄，`--no-policy --no-fail-fast --no-drift` 讓所有 8 項都跑完）**
```
$ uv run apm audit --ci --no-policy --no-fail-fast --no-drift
┌──────────┬──────────────────────────┬───────────────────────────────────────┐
│ Status   │ Check                    │ Message                               │
├──────────┼──────────────────────────┼───────────────────────────────────────┤
│ [+]      │ lockfile-exists          │ Lockfile present                      │
│          │ ref-consistency          │ 1 ref mismatch(es)...                 │
│ [+]      │ deployed-files-present   │ All deployed files present on disk    │
│          │ no-orphaned-packages     │ 1 orphaned package(s)...              │
│ [+]      │ skill-subset-consistency │ Skill subset selections match ...     │
│ [+]      │ config-consistency       │ No MCP configs to check               │
│          │ content-integrity        │ 1 file(s) with hash drift ...         │
│ [+]      │ includes-consent         │ No local content deployed ...         │
└──────────┴──────────────────────────┴───────────────────────────────────────┘
  content-integrity details:
    - hash-drift: .claude/skills/file.txt (dep=github.com/demo/pkg,
      expected=2cf24dba5fb0..., actual=06c9c46ca6d4...)
[x] 3 of 8 check(s) failed
EXIT=1
```
→ 這裡的 `expected=2cf24dba5fb0...` / `actual=06c9c46ca6d4...` 與 apm-go `audit` 報的雜湊值完全一致，確認兩邊在做同一件底層事情，只是 **Python 把它埋在 `--ci` 模式第 6 項檢查裡（且預設 fail-fast 會被前面 `ref-consistency`/`no-orphaned-packages` 擋住看不到），apm-go 把它抽成 bare `audit` 的唯一職責**。

也額外確認（正向 case）：修好雜湊、未竄改時，apm-go `audit` 印 `audit: 1 deployed files verified` exit 0（乾淨通過）。

### 嚴重度：**high**

同名指令 `apm audit` / `apm-go audit`，bare 呼叫（使用者最常見的用法，零 flag）在兩邊做**完全不同、互不重疊**的事：
- 從 Python 使用者角度：習慣 `apm audit` 抓隱藏 Unicode，換到 apm-go 執行同名指令會拿到 SHA 完整性報告，卻**不會被告知**任何隱藏字元問題（apm-go 完全沒有 Unicode 掃描能力，全庫搜尋 `unicode`/`homoglyph`/`invisible` relevant 命中為零）。
- 從 apm-go 使用者角度：習慣 `audit` 會抓內容竄改，若日後改用 Python apm 且沿用 bare `apm audit`，**竄改會被放行**（如上方 transcript 證實），必須額外知道要加 `--ci --no-fail-fast` 或依賴前面 7 項檢查全過才會看到 `content-integrity`。
- PRD 觸發背景第 3 點的猜測（"Python=隱藏 Unicode 掃描，apm-go=lockfile 完整性重驗"）**已用原始碼+實跑證實為真**，且進一步定位到 Python 真正對應邏輯的確切位置（`ci_checks.py:280-375` 的 `_check_content_integrity`）。

### 處置建議

- **不建議「修 parity」成兩邊行為一致**（bare `audit` 改變語意會是破壞性變更，兩邊使用者都已依賴各自現有行為）。
- 建議：在 apm-go `audit --help` 或 README/spec 明確記錄「apm-go audit ≠ Python apm audit（bare）；等價於 Python `apm audit --ci --no-fail-fast` 的 `content-integrity` 檢查裡的 hash 部分，不含 Unicode 掃描」，避免使用者帶著 Python 心智模型誤判 apm-go audit 的覆蓋範圍。
- 若要補齊語意對稱，可另開 task 評估是否要在 apm-go 加一個獨立的隱藏 Unicode 掃描能力（目前完全沒有）——但那是新功能，非本次 parity 盤查範圍。
- 記錄本項到 `.trellis/spec/evals/cli-surface-parity-register.md`（登記冊；2026-07-12 由
  gitignored 的 `.trellis/spec/conformance/` 遷移至此以入版控）。

---

## `doctor`

### 類別：MISSING（Python only）

### 證據

- Python：`src/apm_cli/commands/doctor.py:1-31`（頂層 `apm doctor`，thin wrapper）→ `src/apm_cli/commands/marketplace/doctor.py:24-247` 的 `run_doctor`。實際檢查：
  1. git 是否在 PATH（`git --version`）
  2. network 可達性（`git ls-remote https://github.com/git/git.git HEAD`，5s timeout）
  3. auth token 偵測（`AuthResolver.resolve("github.com")`，純資訊性，永不失敗）
  4. marketplace config 存在性/可解析性（`apm.yml` 的 `marketplace:` block 或 legacy `marketplace.yml`）
  5. format coverage（已設定 vs 支援的 marketplace output 格式，純資訊性）
  6. 重複 package 名稱檢查（`_find_duplicate_names`）
  7. version alignment 檢查（`check_version_alignment`）
  - Exit code：只有 check 1（git）和 2（network）算 critical，其餘全部 `informational=True` 永遠算通過；任一 critical 失敗才 exit 1。
- `--help`（實跑）：`apm doctor [OPTIONS]`，只有 `-v/--verbose` 一個 flag。
- apm-go：全庫（`internal/`, `cmd/`）搜尋 `doctor` 僅命中 `cmd/apm/marketplace.go:156` 的註解與既有 checklist `cli-verification-checklist.md:372`，兩者都是指 **`apm marketplace doctor`**（mkt-061，Python 這邊其實也不存在這個 marketplace 子指令，是頂層 `apm doctor`）——`.trellis/spec/conformance/cli-verification-checklist.md:372` 已把 `doctor` 標為「範圍外(刻意不建,勿當缺口)」，但那個決策的 context 是「marketplace 子指令群不需要 doctor 子指令」，**不等於「頂層 `apm doctor` 已被評估過」**——本次盤查是對頂層指令的獨立確認。
- apm-go 沒有任何等價的環境健檢（git/network/auth/config）機制；`go run ./cmd/apm --help` 的 13 個指令清單裡沒有 `doctor`。

### 嚴重度：**low**

純診斷/資訊性工具，不參與任何寫入或治理決策，corev 功能（install/uninstall/update/audit）不依賴它。缺席只影響「使用者自助故障排除」的體驗（例如 network/auth 有問題時，Python 使用者可以先跑 `apm doctor` 快速定位，apm-go 使用者要靠 error message 自行判斷）。

### 處置建議

- documented extension 差異的反面（missing convenience command）：記錄不做，或視使用者回饋另開低優先度 task。不建議列入本輪修復排期。

---

## `policy`

### 類別：MISSING（Python only）——且底層治理層在 apm-go 完全不存在，不只是少一個 CLI 入口

### 證據

**Python 側（`apm policy status`，唯一子指令）**
- `src/apm_cli/commands/policy.py:268-371` — `policy` group 只有一個子指令 `status`。`--help` 實跑：`--policy-source TEXT`、`--no-cache`、`--json`、`-o/--output [table|json]`、`--check`。
- 用途：對 org-policy（`apm-policy.yml`）discovery 結果做唯讀診斷快照——outcome（found/absent/no_git_remote/empty/disabled/malformed/...）、cache age、`extends` chain、各段規則計數（`dependencies_deny/allow/require`、`mcp_deny/allow/transports_allowed`、`compilation_targets_allowed`、`manifest_required_fields`、`unmanaged_files_directories`）。**永遠 exit 0**（除非 `--check`），設計上安全給 CI/SIEM 用。
- 底層依賴：`src/apm_cli/policy/discovery.py`（`discover_policy` / `discover_policy_with_chain`，org/URL/file 三種來源、cache、`extends` chain 解析）、`src/apm_cli/policy/schema.py`（`ApmPolicy` 完整規則 schema）、`src/apm_cli/policy/policy_checks.py`（`run_policy_checks`，實際套用規則）。這整組治理層同時被 `apm install`（`--policy`/`--no-policy`/`--no-cache`）、`apm audit --ci --policy`、`apm audit --policy`（bare 模式也會 auto-discover）共用。

**apm-go 側**
- 全庫搜尋 `policy` 命中的都是無關的重試/redirect policy（`internal/credsec`, `internal/registry/client.go` 等），或是單一硬編碼規則：`internal/manifest/insecure_dep.go:5-24` 的 `CheckInsecureDependencyScheme`——只做「拒絕非 TLS `http://` git 依賴，除非 `--allow-insecure`」，**是 Python `insecure_policy.py` 裡一條規則的 1:1 移植**，不是可設定的治理引擎，沒有 discovery/schema/allow-deny-list/cache/chain 概念。
- `go run ./cmd/apm install --help` 實跑確認：**沒有 `--policy`/`--no-policy`/`--no-cache` flag**。`go run ./cmd/apm audit --help` 同樣沒有。
- apm-go 13 個頂層指令中沒有 `policy`。

### 嚴重度：**high**

這不是單純「少一個診斷指令」——是**整個組織級治理層在 apm-go 不存在**。若某組織已經用 Python apm 部署 `apm-policy.yml` 做集中治理（例如封鎖某些依賴來源、限制 MCP transport 只能 stdio、限制 compile target、要求 manifest 必填欄位、限制 unmanaged files 目錄），開發者一旦改用 apm-go：
- 這些規則**靜默完全不生效**（沒有任何錯誤或警告，因為 apm-go 根本不知道 `apm-policy.yml` 這個檔案的存在）。
- 屬於 PRD 分類法定義的「最高風險：靜默誤導」等級（雖然形式上是 MISSING 而非 DIVERGENT-SAME-NAME，但風險本質相同——組織以為治理適用於所有工具鏈，實際上 apm-go 是治理真空）。

### 處置建議

- **不建議在本任務內修**（範圍遠超一個指令，需要移植整個 discovery + schema + enforcement 引擎，並串接進 install/audit/compile 三處呼叫點）。
- 強烈建議**另開專責 task**：先評估「apm-go 是否需要支援 org-policy 治理層」這個產品決策（可能的選項：a) 完整移植 b) 至少做 discovery+`policy status`唯讀診斷，enforcement 留待未來 c) 明確記錄不支援並在文件警示組織管理者），而不是直接照單全收移植。
- 若決定不支援，至少應在 apm-go README/CLI help 明確警示：「apm-go 不讀取/不強制 apm-policy.yml；若貴組織依賴 org policy 治理，apm-go 尚不提供對等保護」，避免組織誤判防護範圍。

---

## `approve` / `deny`

### 類別：MISSING（Python only，兩指令合併討論——同一份安全機制的一體兩面）

### 證據

**Python 側**
- `src/apm_cli/commands/approve.py:1-266` — 檔案開頭明講：「mirrors npm v12's `npm approve-scripts` / `npm deny-scripts`」。
- 機制（`src/apm_cli/security/executables.py:1-37`）：APM 套件可宣告三種 executable primitive——**hooks**、**MCP servers**、**bin/ executables**（會在使用者機器上執行任意程式碼）。當專案 `apm.yml` 宣告 `allowExecutables` block 時，啟用 **deny-by-default**：未經批准的 primitive 不會被部署。**若專案完全沒宣告該 block，維持向後相容行為（全部部署，不閘門）**——即這是 opt-in 機制，不是預設開啟的保護。
- `ENFORCED_EXEC_TYPES = (hooks, bin)`（`executables.py:34`）——連 Python 自己這條線也承認 **MCP 目前不受這個閘門實際執行**（`# MCP is excluded because MCPIntegrator does not yet honour the approval state -- surfacing it in the UI would create a false-assurance control`），只有 hooks 和 bin 是真正被 enforce 的。
- `approve` 指令（`approve.py:42-91`）：`apm approve <pkgs>` / `--pending`（列出未批准的）/ `--all`（全部批准），寫入 `apm.yml` 的 `allowExecutables` block。
- `deny` 指令（`approve.py:93-122`）：`apm deny <pkgs>`（必填至少一個 package），從 `allowExecutables` block 移除條目（撤銷批准）。
- `--help` 實跑：`approve` 有 `--pending`/`--all`；`deny` 只有位置參數 `PACKAGES...`（必填）。

**apm-go 側**
- 全庫搜尋 `allowExecutables`/`ExecutableDeclaration`/`executable`（大小寫不敏感）於 `internal/`：**零命中**。
- `internal/deploy/primitive.go:32-197`——`CollectLocalPrimitives`/`CollectDependencyPrimitives`/`extractHookName` 等函式**收集並部署 hook primitive，全程沒有任何批准/閘門檢查**。
- apm-go 13 個頂層指令中沒有 `approve` 也沒有 `deny`。
- apm-go 完全沒有解析或寫入 `apm.yml` 的 `allowExecutables` key 的邏輯（manifest schema 搜尋同樣零命中）。

### 嚴重度：**high**

這是本組五個指令裡風險最高的一項，理由：
1. Hooks/bin executables 是**任意程式碼執行**面（`.claude/rules/common/security.md` 明確把「Authentication or authorization code」「External API calls」等列為 security review trigger，這個機制本質上是同一等級的攻擊面：第三方/transitive 依賴帶來的可執行程式碼）。
2. Python 的機制雖是 opt-in，但一旦專案啟用（`allowExecutables` block 存在），就形成一個明確的、可稽核的 deny-by-default 供應鏈防線——類似 npm v12 的 `allowScripts`。
3. **遷移風險是靜默的**：一個曾在 Python apm 下宣告 `allowExecutables` block 並依賴它擋掉未批准 hook 的專案，若改用 apm-go：
   - `apm.yml` 裡的 `allowExecutables` block 還在（apm-go 大概率當成未知欄位忽略，不會報錯）。
   - 但 apm-go 完全不讀它、不執行它——**所有 hook/bin 无條件部署**，閘門形同虛設。
   - 沒有任何警告或錯誤訊息告知使用者「這個安全機制在 apm-go 上不生效」——這正是 PRD 定義「DIVERGENT-SAME-NAME 最高風險（靜默誤導）」的精神，即使形式上是 MISSING 分類。
4. 沒有 `approve --pending` 這種可視性工具，apm-go 使用者甚至無法簡單列出「哪些安裝的套件帶有 executable primitive」——連知情權都沒有，遑論批准/拒絕。

### 處置建議

- **不建議在本任務內修**（涉及安全機制設計 + `apm.yml` schema 變更 + install pipeline 插入閘門檢查點，範圍大）。
- 強烈建議**另開安全性專責 task**，優先順序建議高於 `policy`（因為 `policy` 至少是 opt-in 的組織治理，`approve`/`deny` 涉及的是终端使用者機器上的任意程式碼執行防線）。該 task 應至少涵蓋：
  a) 是否移植 `allowExecutables` 解析 + deny-by-default enforcement（至少 hooks/bin，比照 Python 現況）。
  b) 若近期不移植，是否要在 apm-go install 時對「來源套件含 hook/bin primitive」做**最低限度的可視性提示**（例如安裝時列出新增的 hook/bin，即使不做批准閘門）。
  c) 若 `apm.yml` 含 `allowExecutables` block 但 apm-go 不執行它，是否應該印一則警告（「此區塊在 apm-go 尚不生效」）而非靜默忽略——這是成本最低、能立即消除「靜默誤導」風險的手段，建議優先做。

---

## 本組總表

| 指令 | 類別 | 證據（file:line / transcript） | 嚴重度 | 處置建議 |
|---|---|---|---|---|
| `audit` | DIVERGENT-SAME-NAME（已實跑證實） | apm-go: `cmd/apm/audit.go:15-56`, `internal/lockfile/audit.go:16-50`；Python: `commands/audit.py:807-1034`（bare）、`policy/ci_checks.py:280-375`（`--ci` 的 `content-integrity`）；TEMP scratch 雙邊 transcript（見上）證實同一竄改檔案 apm-go exit 1 / Python bare exit 0 | **high** | 不改行為；在 help/spec 明確記錄語意差異，避免使用者帶錯心智模型；登記入 `cli-surface-parity-register.md` |
| `doctor` | MISSING（Python only） | Python: `commands/doctor.py:1-31` + `commands/marketplace/doctor.py:24-247`（7 項檢查，僅 git/network 為 critical）；apm-go: 13 指令清單無此項，全庫搜尋 `doctor` 僅命中無關的既有「marketplace doctor 刻意不建」決策（`cli-verification-checklist.md:372`，範圍不同） | **low** | 記錄不做，或視回饋另開低優先度 task |
| `policy` | MISSING（Python only，底層治理層整體缺席） | Python: `commands/policy.py:268-371`（`status` 子指令）+ `policy/discovery.py`/`schema.py`/`policy_checks.py`；apm-go: 全庫僅有 `internal/manifest/insecure_dep.go:5-24` 單一硬編碼規則（非治理引擎），`install --help`/`audit --help` 實跑確認無 `--policy`/`--no-policy`/`--no-cache` | **high** | 不在本任務修；另開專責 task 先做「是否支援 org-policy 治理層」產品決策；若不支援應在文件明確警示組織管理者 |
| `approve` | MISSING（Python only，安全批准機制整體缺席） | Python: `commands/approve.py:42-91` + `security/executables.py:1-37`（deny-by-default opt-in 閘門，mirrors npm v12 `allowScripts`）；apm-go: `allowExecutables`/`ExecutableDeclaration` 全庫零命中，`internal/deploy/primitive.go` 無條件收集/部署 hook | **high** | 不在本任務修；另開安全性專責 task；短期可先加「apm.yml 含 allowExecutables 但不生效」警告 |
| `deny` | MISSING（Python only，與 approve 同一機制） | Python: `commands/approve.py:93-122`（`deny_cmd`，撤銷批准）；apm-go 同上，零命中 | **high** | 同 `approve`，兩者合併同一 task 處置 |

**本組 5 項小結**：1 項 DIVERGENT-SAME-NAME（`audit`，已用雙邊實跑證實，高風險但不建議改行為，改記錄）；4 項 MISSING，其中 `doctor` 低風險可暫緩，`policy`/`approve`/`deny` 三項共 3 個指令高風險，且都指向「apm-go 缺少對應的底層治理/安全機制，不只是缺 CLI 入口」，建議各自另開 task 處理（`policy` 一個治理層 task，`approve`+`deny` 合併一個安全批准機制 task）。

## Caveats / Not Found

- 未對 `approve --pending`/`--all`/`deny` 做任何寫入型 live probe（依安全鐵則，只做 `--help` + 原始碼 + 唯讀分析）。
- `policy status` 因 apm-go 完全沒有對應指令，未做雙邊 live probe 對照（無另一邊可比較）；僅確認 apm-go 側缺席的證據。
- `doctor` 同理未做雙邊 live probe（apm-go 無此指令）；僅讀 Python 原始碼確認其 7 項檢查內容。
- `apm audit --ci` 的完整 8 項檢查鏈在本次 scratch 中因手工建構的 `apm.yml`/`apm.lock.yaml` 不完全符合真實 `apm install` 產出格式，`ref-consistency`/`no-orphaned-packages` 兩項顯示未通過（誤判，非真實 drift）——但這不影響本次要驗證的核心結論（`content-integrity` 檢查本身確實抓到了 hash drift，且數值與 apm-go 一致），已用 `--no-fail-fast` 讓完整 8 項檢查跑完並取得證據。
- 未深入評估 apm-go 是否有計畫在 `install`/`compile` 之外的其他路徑間接支援 org-policy 或 executable 批准（例如透過 marketplace 側或 codex agents TOML 路徑）；本次搜尋範圍涵蓋 `internal/` 全部 `.go` 檔案且信心高（zero hits），但若日後新增檔案應重新搜尋確認。
