# P0 parity quick wins 硬性驗證清單

> **圖例**：`[ ]` 待驗 · `[x]` 已驗——附可重跑證據才可勾。
>
> **判定規則**：每項命令、exit code、輸出文案與檔案效果全部成立才是 PASS；任一 assertion 不符即 FAIL。不得以目視、單純 `go test` 綠燈或「程式碼看起來有處理」代替 live binary 證據。

---

## 0. 執行前置與安全

- **Build**：只在 repo root 執行 `go build -o bin/apm-go.exe ./cmd/apm`；binary 固定叫 `apm-go.exe`，不得產生或使用 `apm.exe`。
- **Python oracle**：只用 `uv --project D:/Projects/apm-dev/apm run apm <args>`；所有 probe cwd 都在 `%TEMP%` scratch。
- **隔離**：install / update / pack / audit / oracle / A/B 一律只在 `%TEMP%` 或腳本自行建立的 TEMP 目錄執行；禁止在 apm-go repo、Python oracle repo或 `D:/Projects/apm-dev/evals` 原目錄執行有副作用的 CLI。
- **硬性禁止**：不得對 Python oracle 執行 `marketplace add`、`marketplace remove`、`marketplace update`，也不得直接改 `~/.apm/marketplaces.json`。
- **Exit code**：PowerShell 每次外部命令後立即保存 `$LASTEXITCODE`；負向案例驗精確的零／非零條件，不接受被後續命令覆蓋。
- **TEMP 證據**：transcript、hash manifest、TDD red/green 輸出只寫 `$Scratch`；不得為了讓驗證通過而改 fixture 原檔或 oracle repo。

### [x] P0Q-01 · 建立 TEMP scratch、baseline 與 byte-tree helper

**驗證證據（2026-07-12）**：建立 `C:\Users\gn006\AppData\Local\Temp\apm-go-p0-parity-a0acd9283e474f2e89a6ca435a2a5b2a`；baseline `c6f05d28445b849d1f56f1529777db676af23ad5`；scratch 邊界與 absent/file/tree SHA helpers 全部 assertion 通過，後續 Q18/Q25 實際使用。

```powershell
$Repo = (Resolve-Path 'D:/Projects/apm-dev/apm-go').Path
$OracleRepo = (Resolve-Path 'D:/Projects/apm-dev/apm').Path
$Evals = (Resolve-Path 'D:/Projects/apm-dev/evals').Path
$Task = Join-Path $Repo '.trellis/tasks/07-12-p0-parity-quickwins'
$TempRoot = [IO.Path]::GetFullPath([IO.Path]::GetTempPath())
$Scratch = Join-Path $TempRoot ('apm-go-p0-parity-' + [guid]::NewGuid().ToString('N'))
$Base = (& git -C $Repo rev-parse HEAD).Trim()
function Assert([bool]$Condition, [string]$Message) { if (-not $Condition) { throw $Message } }
function Get-FileState([string]$Path) {
    if (-not (Test-Path -LiteralPath $Path -PathType Leaf)) { return '__ABSENT__' }
    $f = Get-Item -LiteralPath $Path
    return "F`t$($f.Length)`t$((Get-FileHash -LiteralPath $Path -Algorithm SHA256).Hash)"
}
function Get-TreeManifest([string]$Root) {
    if (-not (Test-Path -LiteralPath $Root -PathType Container)) { return @('__ABSENT__') }
    $rootFull = [IO.Path]::GetFullPath($Root).TrimEnd('\','/')
    $rows = @('D`t.')
    $rows += Get-ChildItem -LiteralPath $rootFull -Recurse -Directory -Force |
        Sort-Object FullName | ForEach-Object {
            'D`t' + $_.FullName.Substring($rootFull.Length).TrimStart('\','/').Replace('\','/')
        }
    $rows += Get-ChildItem -LiteralPath $rootFull -Recurse -File -Force |
        Sort-Object FullName | ForEach-Object {
            $rel = $_.FullName.Substring($rootFull.Length).TrimStart('\','/').Replace('\','/')
            "F`t$rel`t$($_.Length)`t$((Get-FileHash -LiteralPath $_.FullName -Algorithm SHA256).Hash)"
        }
    return @($rows)
}

New-Item -ItemType Directory -Path $Scratch | Out-Null
$ScratchFull = [IO.Path]::GetFullPath($Scratch)
Assert ($ScratchFull.StartsWith($TempRoot, [StringComparison]::OrdinalIgnoreCase)) 'scratch is not under TEMP'
Assert ($ScratchFull -notin @($Repo,$OracleRepo,$Evals)) 'scratch aliases a protected tree'
Assert ($Base -match '^[0-9a-f]{40}$') 'baseline is not a full commit hash'
$OracleMarketplace = Join-Path $HOME '.apm/marketplaces.json'
$OracleMarketplaceBefore = Get-FileState $OracleMarketplace
```

預期：exit 0；scratch 位於 `%TEMP%` 且與三個受保護 tree 不同；baseline 為 40-hex；helper 能同時分辨 absent、空目錄、路徑集合與每個檔案 bytes。

**權威來源**：`prd.md` Requirements（TEMP 隔離、零副作用）；`.trellis/spec/guides/oracle-parity-gates.md` Gate 2；同期 `.trellis/tasks/07-12-codex-agent-toml/checklist.md` §0。

### [x] P0Q-02 · 固定名稱 binary build

**驗證證據（2026-07-12）**：`go build -o bin/apm-go.exe ./cmd/apm` exit 0；檔名逐字為 `apm-go.exe`，SHA-256 `8883F1394603EDA2B2B3CE5CDF87F3411C0FC1BF0BA64C338074A43221C8B825`；全部 live probe 使用此 binary。

```powershell
Push-Location $Repo
try {
    & go build -o bin/apm-go.exe ./cmd/apm
    $BuildCode = $LASTEXITCODE
} finally { Pop-Location }
$Bin = (Resolve-Path (Join-Path $Repo 'bin/apm-go.exe')).Path
Assert ($BuildCode -eq 0) "fixed binary build exit=$BuildCode"
Assert (Test-Path -LiteralPath $Bin -PathType Leaf) 'bin/apm-go.exe missing'
Assert ([IO.Path]::GetFileName($Bin) -ceq 'apm-go.exe') 'wrong binary name'
```

預期：build exit 0，且後續所有 live probe 都使用 `$Bin`；不得以 `go run` 或舊的 scratch binary 代替。

**權威來源**：project `AGENTS.md` Available commands；`prd.md` Requirements。

### [x] P0Q-03 · Oracle 入口唯讀 smoke 與 marketplace 狀態基準

**驗證證據（2026-07-12）**：三個 oracle help 均於 `$Scratch` cwd 執行且 exit 0；`~/.apm/marketplaces.json` 前後皆為 length 278、SHA-256 `9F752A5D9667F5DE85B3768E14644EA793B156E1FF88D8C7B641CE2054C2730B`。

```powershell
Push-Location $Scratch
try {
    $OracleHelp = @(& uv --project $OracleRepo run apm --help 2>&1)
    $OracleHelpCode = $LASTEXITCODE
    $OraclePackHelp = @(& uv --project $OracleRepo run apm pack --help 2>&1)
    $OraclePackHelpCode = $LASTEXITCODE
    $OracleAuditHelp = @(& uv --project $OracleRepo run apm audit --help 2>&1)
    $OracleAuditHelpCode = $LASTEXITCODE
} finally { Pop-Location }
Assert ($OracleHelpCode -eq 0 -and $OraclePackHelpCode -eq 0 -and $OracleAuditHelpCode -eq 0) 'oracle help smoke failed'
Assert ((Get-FileState $OracleMarketplace) -ceq $OracleMarketplaceBefore) 'oracle marketplace registry changed during help smoke'
```

預期：三個 read-only help 都 exit 0，且 `~/.apm/marketplaces.json` 的 absent/length/SHA 狀態完全不變。任何後續 oracle 操作若要求 marketplace 寫入，立即 FAIL，不得執行。

**權威來源**：`.trellis/spec/evals/cli-surface-parity-register.md` §0.2 安全慣例；`oracle-parity-gates.md` Gate 1/2；本 task `prd.md`。

### [x] P0Q-04 · 六項 code surface 的 TDD red→green 證據

**驗證證據（2026-07-12）**：依授權 `git stash push -- cmd/apm/init.go` 後同一 `$P0Pattern` red exit 1，精確得到 `--- FAIL: TestInitCmd_DoesNotSuggestRun` 與舊 `apm-go run <script>` 斷言；`stash pop` 後 diff/status/stash stack byte-for-byte 復原。green exit 0，七個 named tests 全部 `--- PASS`、零 SKIP/FAIL；transcript：`$Scratch/p0-tdd-red.txt`、`p0-tdd-green.txt`。

測試先寫、實作未寫時跑 red；完成實作後以相同 pattern 跑 green。兩次輸出留在 scratch，不能事後只補一份永遠綠的測試。

```powershell
$P0Pattern = '^(TestInitCmd_DoesNotSuggestRun|TestPackCmd_HelpDocumentsMarketplaceOnlyScope|TestRunPack_NoMarketplaceDeferredInput|TestAuditCmd_HelpDocumentsSemanticDifference|TestRunInstall_AllowExecutablesWarning|TestUpdateCmd_DryRunFlag|TestRunUpdate_DryRunPlanNoSideEffects)$'

# RED：只在 tests 已加入、production implementation 尚未加入時執行。
Push-Location $Repo
try { $Red = @(& go test ./cmd/apm -run $P0Pattern -count=1 -v 2>&1); $RedCode = $LASTEXITCODE } finally { Pop-Location }
$Red | Set-Content -Encoding utf8 (Join-Path $Scratch 'p0-tdd-red.txt')
Assert ($RedCode -ne 0) 'TDD red unexpectedly passed'
Assert (($Red | Out-String) -match '--- FAIL:') 'TDD red lacks a failing assertion signature'

# GREEN：production implementation 完成後執行。
Push-Location $Repo
try { $Green = @(& go test ./cmd/apm -run $P0Pattern -count=1 -v 2>&1); $GreenCode = $LASTEXITCODE } finally { Pop-Location }
$Green | Set-Content -Encoding utf8 (Join-Path $Scratch 'p0-tdd-green.txt')
$GreenText = $Green | Out-String
Assert ($GreenCode -eq 0) "TDD green exit=$GreenCode"
foreach ($Name in @(
    'TestInitCmd_DoesNotSuggestRun',
    'TestPackCmd_HelpDocumentsMarketplaceOnlyScope',
    'TestRunPack_NoMarketplaceDeferredInput',
    'TestAuditCmd_HelpDocumentsSemanticDifference',
    'TestRunInstall_AllowExecutablesWarning',
    'TestUpdateCmd_DryRunFlag',
    'TestRunUpdate_DryRunPlanNoSideEffects'
)) { Assert ($GreenText -match ('--- PASS: ' + [regex]::Escape($Name))) "$Name PASS missing" }
Assert ($GreenText -notmatch '--- SKIP:|--- FAIL:') 'green run contains SKIP/FAIL'
```

預期：red 精確非零且有 assertion failure；green exit 0、七個 named tests 全 PASS、無 SKIP。若 red transcript 是編譯環境壞掉、缺依賴或無關測試失敗，不算 TDD red。

**權威來源**：`prd.md` Requirements「每項碼變更走 TDD」；同 Requirements「警告/help 文案有測試鎖定」。

---

## 1. P0 #1：`init` 不再承諾不存在的 `run`

### [x] P0Q-05 · 真 binary `init` 成功且 Next steps 無 `run` 虛假提示

**驗證證據（2026-07-12）**：TEMP live `init . --yes --target codex` exit 0 並建立 `apm.yml`；輸出保留 `apm-go install`，不含 `apm-go run`/`Run a script`；`run --help` 非零且為 unknown command。

```powershell
$InitDir = Join-Path $Scratch 'init-no-run-promise'
New-Item -ItemType Directory -Path $InitDir | Out-Null
Push-Location $InitDir
try { $InitOut = @(& $Bin init . --yes --target codex 2>&1); $InitCode = $LASTEXITCODE } finally { Pop-Location }
$InitText = $InitOut | Out-String
Assert ($InitCode -eq 0) "init exit=$InitCode"
Assert (Test-Path (Join-Path $InitDir 'apm.yml') -PathType Leaf) 'init did not create apm.yml'
Assert ($InitText -match 'APM project initialized successfully') 'init success signature missing'
Assert ($InitText -match 'apm-go install') 'valid install next-step disappeared'
Assert ($InitText -notmatch 'apm-go\s+run|Run a script') 'init still promises the missing run command'

$MissingRun = @(& $Bin run --help 2>&1)
$MissingRunCode = $LASTEXITCODE
Assert ($MissingRunCode -ne 0) 'run unexpectedly exists; P0 #1 scope assumption changed'
Assert (($MissingRun | Out-String) -match 'unknown command.*run') 'missing-run negative signature absent'
```

預期：`init` exit 0 且建立合法 `apm.yml`；保留有效 install 指引，但不出現任何 `apm-go run`／`Run a script`；`run --help` 仍是 unknown command（本 task 沒有偷做 P1/P2 runner）。

**權威來源**：`prd.md` #1；parity register §4.8、§5 P0 #1；`cmd/apm/init.go` `initCmd` Phase 7。

### [x] P0Q-06 · `init` 負向文案有測試本體鎖定

**驗證證據（2026-07-12）**：已逐行讀 `TestInitCmd_DoesNotSuggestRun`；本體逐字常數 `apm-go run <script>` 並以 `strings.Contains` 負向斷言，同時鎖定 `Run a script` 與有效 install 提示。

```powershell
$InitTests = (Get-ChildItem (Join-Path $Repo 'cmd/apm') -Filter '*_test.go' -File |
    ForEach-Object { Get-Content -Raw $_.FullName }) -join "`n"
$InitCase = [regex]::Match($InitTests, '(?s)func TestInitCmd_DoesNotSuggestRun\(.*?(?=\nfunc |\z)')
Assert ($InitCase.Success) 'init no-run named test body missing'
Assert ($InitCase.Value -match [regex]::Escape('apm-go run <script>')) 'test does not lock the removed full promise'
Assert ($InitCase.Value -match 'Contains|NotContains|not.*contain|unexpected') 'test body lacks a negative output assertion'
```

預期：測試不是只驗 exit 0；其本體明確以完整舊文案 `apm-go run <script>` 做負向斷言，防止日後回歸。

**權威來源**：`prd.md` Requirements（文案測試鎖定）；register §4.8；既有 `cmd/apm/main_test.go` command-output 測試慣例。

---

## 2. P0 #2：`pack` 範圍限定與 Gate 2 排除輸入

### [x] P0Q-07 · `pack --help` 明說 marketplace-only，且 help 文字被測試鎖定

**驗證證據（2026-07-12）**：live help exit 0，含 `marketplace.json`、`Python`、`plugin bundle` 與 only/does-not 限定；named test 本體含三 token 並逐字鎖定 live scope line。

```powershell
$PackHelp = @(& $Bin pack --help 2>&1)
$PackHelpCode = $LASTEXITCODE
$PackHelpText = $PackHelp | Out-String
Assert ($PackHelpCode -eq 0) "pack --help exit=$PackHelpCode"
foreach ($Token in @('marketplace.json','Python','plugin bundle')) {
    Assert ($PackHelpText -match [regex]::Escape($Token)) "pack help missing $Token"
}
Assert ($PackHelpText -match '(?i)only|limited|does not') 'pack help lacks explicit scope-limiting language'
$PackScopeLine = ($PackHelp | Where-Object { $_ -match '(?i)plugin bundle' } | Select-Object -First 1).ToString().Trim()
Assert ($PackScopeLine.Length -gt 20) 'full pack scope help line was not captured'

$PackTests = Get-Content -Raw (Join-Path $Repo 'cmd/apm/pack_test.go')
$HelpCase = [regex]::Match($PackTests, '(?s)func TestPackCmd_HelpDocumentsMarketplaceOnlyScope\(.*?(?=\nfunc |\z)')
Assert ($HelpCase.Success) 'pack help named test missing'
foreach ($Token in @('marketplace.json','Python','plugin bundle')) {
    Assert ($HelpCase.Value -match [regex]::Escape($Token)) "pack help test does not lock $Token"
}
Assert ($PackTests -match [regex]::Escape($PackScopeLine)) 'full pack scope help line is not locked verbatim in tests'
```

預期：help 同時說清 apm-go 只產 marketplace JSON，且不等同 Python 的 plugin-bundle pack；測試本體鎖定三個辨識詞，不只檢查 flag 存在。

**權威來源**：`prd.md` #2；register §3.2（根因與建議 2）、§5 P0 #2；`cmd/apm/pack.go` `packCmd`。

### [x] P0Q-08 · Gate 2 Probe A：有 dependencies、無 marketplace 必須警告而非靜默

**驗證證據（2026-07-12）**：自建 deps-only fixture live exit 0，stderr 出現完整 `[warn] ... dependencies: ... no 'marketplace:' ... plugin bundle`；未產生 `build/` 或 `.claude-plugin/marketplace.json`。

```powershell
$PackDeps = Join-Path $Scratch 'pack-deps-no-marketplace'
New-Item -ItemType Directory -Path $PackDeps | Out-Null
@'
name: pack-deps
version: 1.0.0
dependencies:
  apm:
    - acme/tool
'@ | Set-Content -Encoding utf8 (Join-Path $PackDeps 'apm.yml')
Push-Location $PackDeps
try { $PackDepsOut = @(& $Bin pack 2>&1); $PackDepsCode = $LASTEXITCODE } finally { Pop-Location }
$PackDepsText = $PackDepsOut | Out-String
Assert ($PackDepsCode -eq 0) "pack deps-only exit=$PackDepsCode"
Assert ($PackDepsText -match '(?i)\[warn\].*dependenc') 'deps-only deferred branch did not warn'
Assert ($PackDepsText -match '(?i)marketplace|plugin bundle|Python') 'warning does not explain the uncovered scope'
Assert (-not (Test-Path (Join-Path $PackDeps 'build'))) 'apm-go unexpectedly built a Python-style bundle'
Assert (-not (Test-Path (Join-Path $PackDeps '.claude-plugin/marketplace.json'))) 'marketplace output appeared without marketplace config'
$PackWarning = ($PackDepsOut | Where-Object { $_ -match '(?i)\[warn\].*dependenc' } | Select-Object -First 1).ToString().Trim()
Assert ($PackWarning.Length -gt 20) 'full pack warning line was not captured'
```

預期：exit 0 但至少一行 `[warn]` 明確指出 dependencies 被排除／未打包；沒有 `build/` 或 marketplace output。單純原本的 `nothing to do` info 不算 PASS。

**權威來源**：`prd.md` Requirements/AC（有 deps 無 marketplace）；oracle Gate 2 lines 84-89；register §3.2 Probe A。

### [x] P0Q-09 · Gate 2 Probe C：有 target、無 marketplace 也不得靜默

**驗證證據（2026-07-12）**：自建 target-only fixture live exit 0，stderr 出現 target deferred warning；未產生 `plugin.json` 或 `build/`。

```powershell
$PackTarget = Join-Path $Scratch 'pack-target-no-marketplace'
New-Item -ItemType Directory -Path $PackTarget | Out-Null
@'
name: pack-target
version: 1.0.0
target:
  - claude
'@ | Set-Content -Encoding utf8 (Join-Path $PackTarget 'apm.yml')
Push-Location $PackTarget
try { $PackTargetOut = @(& $Bin pack 2>&1); $PackTargetCode = $LASTEXITCODE } finally { Pop-Location }
$PackTargetText = $PackTargetOut | Out-String
Assert ($PackTargetCode -eq 0) "pack target-only exit=$PackTargetCode"
Assert ($PackTargetText -match '(?i)\[warn\].*target') 'target-only deferred branch did not warn'
Assert (-not (Test-Path (Join-Path $PackTarget 'plugin.json'))) 'apm-go unexpectedly produced Python PluginManifestProducer output'
Assert (-not (Test-Path (Join-Path $PackTarget 'build'))) 'apm-go unexpectedly produced a bundle'
$PackTargetWarning = ($PackTargetOut | Where-Object { $_ -match '(?i)\[warn\].*target' } | Select-Object -First 1).ToString().Trim()
Assert ($PackTargetWarning.Length -gt 20) 'full target warning line was not captured'
```

預期：exit 0、target deferred branch 有 warning，且仍不實作 Python 的 `plugin.json`/bundle producer。

**權威來源**：`prd.md` #2；register §3.2 Probe C 與建議 2；oracle Gate 1 outcome (ii)、Gate 2。

### [x] P0Q-10 · `pack` 真正 nothing-to-do 負向案例不濫報 warning

**驗證證據（2026-07-12）**：只有 name/version 的 fixture exit 0，含 `nothing to do`、零 `[warn]`；tree manifest 僅根目錄與既有 `apm.yml`，無新增路徑。

```powershell
$PackEmpty = Join-Path $Scratch 'pack-truly-empty'
New-Item -ItemType Directory -Path $PackEmpty | Out-Null
@'
name: pack-empty
version: 1.0.0
'@ | Set-Content -Encoding utf8 (Join-Path $PackEmpty 'apm.yml')
Push-Location $PackEmpty
try { $PackEmptyOut = @(& $Bin pack 2>&1); $PackEmptyCode = $LASTEXITCODE } finally { Pop-Location }
$PackEmptyText = $PackEmptyOut | Out-String
Assert ($PackEmptyCode -eq 0) "pack empty exit=$PackEmptyCode"
Assert ($PackEmptyText -match '(?i)nothing to do|no .marketplace:. block') 'genuine no-op info missing'
Assert ($PackEmptyText -notmatch '(?i)\[warn\]') 'genuine empty manifest received a false warning'
Assert ((Get-TreeManifest $PackEmpty).Count -eq 2) 'pack empty wrote an unexpected file or directory'
```

預期：只有 `apm.yml` 的真正 no-op 維持 exit 0 + info，沒有 warning，也沒有任何新增檔案／目錄。

**權威來源**：`cmd/apm/pack.go` `runPack`/`hasMarketplaceConfig` 既有 nothing-to-do 行為；`prd.md` #2 僅要求對 deferred inputs 警告。

### [x] P0Q-11 · `pack` warning 完整文案與三分支測試本體鎖定

**第 2 輪驗證證據（2026-07-12）**：已讀 `pack_test.go`：`wantPackDepsWarning`/`wantPackTargetWarning` 是測試側獨立完整字面，斷言未引用產品常數，且同一 named test 覆蓋 deps、target、真正 empty 三分支。辨識力抽測將產品文案 `will not` 暫改為 `will now` 後，`TestRunPack_NoMarketplaceDeferredInput` 精確因完整文案不符而 red（exit 1）；還原後 named test 與全部 pack 相關測試 green。`pack.go` SHA-256、整體 binary diff/status/stash hash 均與抽測前完全相同。

```powershell
$PackTests = Get-Content -Raw (Join-Path $Repo 'cmd/apm/pack_test.go')
$PackCase = [regex]::Match($PackTests, '(?s)func TestRunPack_NoMarketplaceDeferredInput\(.*?(?=\nfunc |\z)')
Assert ($PackCase.Success) 'pack deferred-input table test missing'
foreach ($Token in @('dependencies','target','marketplace')) {
    Assert ($PackCase.Value -match [regex]::Escape($Token)) "pack test body missing $Token branch"
}
Assert ($PackTests -match [regex]::Escape($PackWarning)) 'live full pack warning is not locked verbatim in pack_test.go'
Assert ($PackTests -match [regex]::Escape($PackTargetWarning)) 'live full target warning is not locked verbatim in pack_test.go'
Assert ($PackCase.Value -match 'warn' -and $PackCase.Value -match 'nothing to do') 'test does not distinguish warn from genuine no-op'
```

預期：同一測試本體至少覆蓋 deps、target、真正 empty 三分支；live 捕捉的完整 warning 逐字存在於測試，不接受只鎖 `[warn]` 前綴。

**權威來源**：`prd.md` Requirements（措辭漂移防護）；oracle Gate 2；register §3.2 Probe A/C。

---

## 3. P0 #3：`audit` 同名異義明示、行為不改

### [x] P0Q-12 · `audit --help` 明示 SHA integrity ≠ Python bare Unicode，且有測試鎖定

**驗證證據（2026-07-12）**：live help exit 0 且含 SHA/Python/Unicode/differs；已讀 named test，本體逐字鎖定 live contrast line；backend spec 同時通過語意對照 regex。

```powershell
$AuditHelp = @(& $Bin audit --help 2>&1)
$AuditHelpCode = $LASTEXITCODE
$AuditHelpText = $AuditHelp | Out-String
Assert ($AuditHelpCode -eq 0) "audit --help exit=$AuditHelpCode"
foreach ($Token in @('SHA','Python','Unicode')) {
    Assert ($AuditHelpText -match [regex]::Escape($Token)) "audit help missing $Token"
}
Assert ($AuditHelpText -match '(?i)not|differs|does not') 'audit help lacks explicit semantic contrast'
$AuditContrastLine = ($AuditHelp | Where-Object { $_ -match '(?i)Unicode' } | Select-Object -First 1).ToString().Trim()
Assert ($AuditContrastLine.Length -gt 20) 'full audit contrast help line was not captured'

$AuditTests = (Get-ChildItem (Join-Path $Repo 'cmd/apm') -Filter '*audit*_test.go' -File |
    ForEach-Object { Get-Content -Raw $_.FullName }) -join "`n"
$AuditHelpCase = [regex]::Match($AuditTests, '(?s)func TestAuditCmd_HelpDocumentsSemanticDifference\(.*?(?=\nfunc |\z)')
Assert ($AuditHelpCase.Success) 'audit help named test missing'
foreach ($Token in @('SHA','Python','Unicode')) {
    Assert ($AuditHelpCase.Value -match [regex]::Escape($Token)) "audit help test missing $Token"
}
Assert ($AuditTests -match [regex]::Escape($AuditContrastLine)) 'full audit contrast help line is not locked verbatim in tests'
$AuditSpecDocs = (Get-ChildItem (Join-Path $Repo '.trellis/spec/backend') -Filter '*.md' -File |
    ForEach-Object { Get-Content -Raw $_.FullName }) -join "`n"
Assert ($AuditSpecDocs -match '(?is)audit.{0,1200}SHA.{0,1200}Python.{0,1200}Unicode') 'backend spec lacks the audit semantic contrast'
```

預期：help 清楚表明 apm-go bare audit 是 deployed-file hash 重驗，不是 Python bare audit 的 hidden-Unicode 掃描；測試鎖定辨識詞。

**權威來源**：`prd.md` #3；register §3.1、§5 P0 #3；`cmd/apm/audit.go` `auditCmd`。

### [x] P0Q-13 · 真 binary `audit` 仍是 hash gate：乾淨過、竄改失敗

**驗證證據（2026-07-12）**：自建 hook fixture install exit 0；clean audit exit 0 且 `deployed files verified`；竄改 `.codex/hooks.json` 後 audit 非零，含 `content-integrity violation`、expected/observed SHA-256。

```powershell
$AuditDir = Join-Path $Scratch 'audit-hash-behavior'
New-Item -ItemType Directory -Force -Path (Join-Path $AuditDir '.apm/hooks') | Out-Null
@'
name: audit-probe
version: 1.0.0
target:
  - codex
'@ | Set-Content -Encoding utf8 (Join-Path $AuditDir 'apm.yml')
'{"Stop":[{"hooks":[{"type":"command","command":"echo ok"}]}]}' |
    Set-Content -Encoding utf8 (Join-Path $AuditDir '.apm/hooks/probe.json')
Push-Location $AuditDir
try {
    $InstallOut = @(& $Bin install 2>&1); $InstallCode = $LASTEXITCODE
    $AuditClean = @(& $Bin audit 2>&1); $AuditCleanCode = $LASTEXITCODE
    Add-Content -Encoding utf8 (Join-Path $AuditDir '.codex/hooks.json') "`nTAMPERED"
    $AuditBad = @(& $Bin audit 2>&1); $AuditBadCode = $LASTEXITCODE
} finally { Pop-Location }
$AuditCleanText = $AuditClean | Out-String
$AuditBadText = $AuditBad | Out-String
Assert ($InstallCode -eq 0) "audit fixture install exit=$InstallCode"
Assert ($AuditCleanCode -eq 0 -and $AuditCleanText -match 'deployed files verified') 'clean audit did not pass'
Assert ($AuditBadCode -ne 0) 'tampered audit unexpectedly passed'
Assert ($AuditBadText -match 'content-integrity violation') 'tamper diagnostic missing'
Assert ($AuditBadText -match 'expected sha256:' -and $AuditBadText -match 'observed sha256:') 'expected/observed hashes missing'
```

預期：clean audit exit 0；只改 deployed bytes 後 exit 非零，並列出 path、expected/observed SHA。不得因補 help 而改成 Unicode scanner 或放寬既有 integrity gate。

**權威來源**：register §3.1 transcript/根因；`cmd/apm/audit.go:15-56`；`internal/lockfile/audit.go`。

---

## 4. P0 #4：`allowExecutables` 警告但不改部署

### [x] P0Q-14 · 有 `allowExecutables` block：警告出現，hook 仍照既有路徑部署

**驗證證據（2026-07-12）**：`allowExecutables: {}` fixture install exit 0；stderr 明示 `[warn]`、block not effective；`.codex/hooks.json` 與 lockfile provenance 均存在，部署未被 gate。

```powershell
$AllowOn = Join-Path $Scratch 'allow-executables-on'
New-Item -ItemType Directory -Force -Path (Join-Path $AllowOn '.apm/hooks') | Out-Null
@'
name: allow-on
version: 1.0.0
target:
  - codex
allowExecutables: {}
'@ | Set-Content -Encoding utf8 (Join-Path $AllowOn 'apm.yml')
$HookBody = '{"Stop":[{"hooks":[{"type":"command","command":"echo unchanged"}]}]}'
$HookBody | Set-Content -Encoding utf8 (Join-Path $AllowOn '.apm/hooks/probe.json')
Push-Location $AllowOn
try { $AllowOnOut = @(& $Bin install 2>&1); $AllowOnCode = $LASTEXITCODE } finally { Pop-Location }
$AllowOnText = $AllowOnOut | Out-String
$AllowOnHook = Join-Path $AllowOn '.codex/hooks.json'
Assert ($AllowOnCode -eq 0) "allowExecutables install exit=$AllowOnCode"
Assert ($AllowOnText -match '(?i)\[?warn') 'allowExecutables warning level missing'
Assert ($AllowOnText -match [regex]::Escape('allowExecutables')) 'warning does not name the ignored block'
Assert ($AllowOnText -match '(?i)not enforced|not effective|does not take effect|not supported') 'warning does not say the block is ineffective in apm-go'
Assert (Test-Path $AllowOnHook -PathType Leaf) 'hook deployment was incorrectly gated'
Assert ((Get-Content -Raw (Join-Path $AllowOn 'apm.lock.yaml')) -match [regex]::Escape('.codex/hooks.json')) 'deployed hook missing from lockfile'
$AllowWarning = ($AllowOnOut | Where-Object { $_ -match 'allowExecutables' } | Select-Object -First 1).ToString().Trim()
$AllowOnHash = (Get-FileHash $AllowOnHook -Algorithm SHA256).Hash
```

預期：exit 0；完整 warning 明說 block 在 apm-go 不生效；`.codex/hooks.json` 與 lockfile provenance 仍存在。警告不是安全閘門。

**權威來源**：`prd.md` #4/Requirements；register §4.1、§5 P0 #4；`internal/manifest/manifest.go` `ParseManifest` unknown-key path；`internal/deploy/primitive.go` `CollectLocalPrimitives`。

### [x] P0Q-15 · 無 block：無警告；兩案例部署 bytes 完全相同

**驗證證據（2026-07-12）**：無 block fixture exit 0 且零 `allowExecutables` 警告；on/off `.codex/hooks.json` SHA-256 同為 `03DC2358FD45446F92CB742BCEDBDA7D7510B2F6CD0BCCEFA738F8EB890D8504`。

```powershell
$AllowOff = Join-Path $Scratch 'allow-executables-off'
New-Item -ItemType Directory -Force -Path (Join-Path $AllowOff '.apm/hooks') | Out-Null
@'
name: allow-off
version: 1.0.0
target:
  - codex
'@ | Set-Content -Encoding utf8 (Join-Path $AllowOff 'apm.yml')
$HookBody | Set-Content -Encoding utf8 (Join-Path $AllowOff '.apm/hooks/probe.json')
Push-Location $AllowOff
try { $AllowOffOut = @(& $Bin install 2>&1); $AllowOffCode = $LASTEXITCODE } finally { Pop-Location }
$AllowOffText = $AllowOffOut | Out-String
$AllowOffHook = Join-Path $AllowOff '.codex/hooks.json'
Assert ($AllowOffCode -eq 0) "no-block install exit=$AllowOffCode"
Assert ($AllowOffText -notmatch [regex]::Escape('allowExecutables')) 'warning emitted without the block'
Assert (Test-Path $AllowOffHook -PathType Leaf) 'baseline hook deployment missing'
$AllowOffHash = (Get-FileHash $AllowOffHook -Algorithm SHA256).Hash
Assert ($AllowOffHash -eq $AllowOnHash) 'allowExecutables block changed deployed hook bytes'
```

預期：無 block 時完全沒有該警告；on/off 兩側 hook 輸出 SHA-256 相同。若有 block 時少部署、改寫或重排 hook，立即 FAIL。

**權威來源**：`prd.md` Requirements「提示不是閘門、部署行為不變」；register §4.1（Python opt-in 與 apm-go 現況）；`internal/deploy/primitive.go`。

### [x] P0Q-16 · `allowExecutables` 完整文案與 on/off/deploy 測試本體鎖定

**第 2 輪驗證證據（2026-07-12）**：已讀 `install_test.go`/`manifest_test.go`：兩者各自以測試側獨立 `wantAllowExecutablesWarning` 完整字面斷言，未引用產品常數；E2E 同時覆蓋 with/without block 並以 SHA-256 比對部署 bytes。辨識力抽測將產品文案 `unconditionally` 暫改為 `conditionally` 後，manifest unit test 與 install E2E 均精確因完整文案不符而 red（exit 1/1）；還原後兩者 green。`manifest.go` SHA-256 與整體工作樹指紋完全復原。

```powershell
$InstallTests = (Get-ChildItem (Join-Path $Repo 'cmd/apm') -Filter '*_test.go' -File |
    ForEach-Object { Get-Content -Raw $_.FullName }) -join "`n"
$AllowCase = [regex]::Match($InstallTests, '(?s)func TestRunInstall_AllowExecutablesWarning\(.*?(?=\nfunc |\z)')
Assert ($AllowCase.Success) 'allowExecutables named test body missing'
Assert ($InstallTests -match [regex]::Escape($AllowWarning)) 'live full allowExecutables warning is not locked verbatim in tests'
foreach ($Token in @('allowExecutables','hooks.json','with','without')) {
    Assert ($AllowCase.Value -match [regex]::Escape($Token)) "allowExecutables test missing $Token"
}
Assert ($AllowCase.Value -match 'Equal|DeepEqual|bytes|hash|SHA') 'test does not prove deploy output unchanged'
```

預期：測試本體同時驗 block 有警告、無 block 無警告、hook 仍部署且內容不變；不得只測 `ParseManifest` 回傳一個 diagnostic 就宣稱完成。

**權威來源**：`prd.md` Requirements；register §4.1；`internal/deploy/primitive.go:32-197`（實際部署路徑）。

---

## 5. P0 #5：`update --dry-run` plan + 零副作用

### [x] P0Q-17 · `update --dry-run` flag/help wiring 與測試鎖定

**驗證證據（2026-07-12）**：live help exit 0；完整行 `--dry-run ... preview the update plan without applying it: no apm.lock.yaml write, no apm_modules/ mutation, no target deploy` 存在，且 named test 以獨立 literal 逐字鎖定。

```powershell
$UpdateHelp = @(& $Bin update --help 2>&1)
$UpdateHelpCode = $LASTEXITCODE
$UpdateHelpText = $UpdateHelp | Out-String
Assert ($UpdateHelpCode -eq 0) "update --help exit=$UpdateHelpCode"
Assert ($UpdateHelpText -match '(?m)^\s+--dry-run\b') '--dry-run flag missing'
Assert ($UpdateHelpText -match '(?i)plan|preview') '--dry-run help does not promise a plan/preview'
Assert ($UpdateHelpText -match '(?i)without.*(write|apply|change|deploy)') '--dry-run help does not promise no mutation'
$DryHelpLine = ($UpdateHelp | Where-Object { $_ -match '(?i)--dry-run' } | Select-Object -First 1).ToString().Trim()
Assert ($DryHelpLine.Length -gt 20) 'full --dry-run help line was not captured'

$UpdateTests = (Get-ChildItem (Join-Path $Repo 'cmd/apm') -Filter 'update*_test.go' -File |
    ForEach-Object { Get-Content -Raw $_.FullName }) -join "`n"
$FlagCase = [regex]::Match($UpdateTests, '(?s)func TestUpdateCmd_DryRunFlag\(.*?(?=\nfunc |\z)')
Assert ($FlagCase.Success) 'update dry-run flag test missing'
Assert ($FlagCase.Value -match [regex]::Escape('dry-run')) 'flag test does not lock the flag name/help'
Assert ($UpdateTests -match [regex]::Escape($DryHelpLine)) 'full --dry-run help line is not locked verbatim in tests'
```

預期：help 精確列出 flag，並同時承諾印 plan 與不寫入／不部署；named test 鎖定 CLI wiring。

**權威來源**：`prd.md` #5；register §2 `--dry-run` 列、§3.3 D-1、§5 P0 #5；`cmd/apm/update.go` `updateCmd`。

### [x] P0Q-18 · Gate 2 live：plan 印出，lockfile/apm_modules/target 前後 byte-identical

**驗證證據（2026-07-12）**：真 git v1.0.0→v1.5.0 fixture dry-run exit 0；transcript 為 `Update plan for apm.yml`、`./remote: v1.0.0 -> v1.5.0`。lockfile file-state、`apm_modules/` 與 `.claude/` 路徑/length/SHA manifests 前後 `Compare-Object` 均 0 差異；transcript：`$Scratch/update-dry-run-live.txt`。

```powershell
$UpdateDir = Join-Path $Scratch 'update-dry-run-live'
$Remote = Join-Path $UpdateDir 'remote'
New-Item -ItemType Directory -Force -Path (Join-Path $Remote '.apm/skills/demo') | Out-Null
Push-Location $Remote
try {
    & git init | Out-Null
    & git config user.name test
    & git config user.email test@example.com
    "name: dep`nversion: '1.0.0'`n" | Set-Content -Encoding utf8 apm.yml
    "# demo v1`n" | Set-Content -Encoding utf8 .apm/skills/demo/SKILL.md
    & git add .; & git commit -m v1 | Out-Null; & git tag v1.0.0
    $GitV1Code = $LASTEXITCODE
} finally { Pop-Location }
@'
name: update-consumer
version: 1.0.0
target:
  - claude
dependencies:
  apm:
    - git: ./remote
      ref: "^1.0.0"
'@ | Set-Content -Encoding utf8 (Join-Path $UpdateDir 'apm.yml')
Push-Location $UpdateDir
try { $InitialInstall = @(& $Bin install 2>&1); $InitialInstallCode = $LASTEXITCODE } finally { Pop-Location }
Assert ($GitV1Code -eq 0 -and $InitialInstallCode -eq 0) 'update fixture setup failed'
Assert (Test-Path (Join-Path $UpdateDir '.claude/skills/demo/SKILL.md') -PathType Leaf) 'target fixture missing before dry-run'

Push-Location $Remote
try {
    "name: dep`nversion: '1.5.0'`n" | Set-Content -Encoding utf8 apm.yml
    "# demo v1.5`n" | Set-Content -Encoding utf8 .apm/skills/demo/SKILL.md
    & git add .; & git commit -m v1.5 | Out-Null; & git tag v1.5.0
    $GitV15Code = $LASTEXITCODE
    $V15Head = (& git rev-parse HEAD).Trim()
} finally { Pop-Location }
Assert ($GitV15Code -eq 0 -and $V15Head -match '^[0-9a-f]{40}$') 'new remote tag setup failed'

$LockBefore = Get-FileState (Join-Path $UpdateDir 'apm.lock.yaml')
$ModulesBefore = Get-TreeManifest (Join-Path $UpdateDir 'apm_modules')
$TargetBefore = Get-TreeManifest (Join-Path $UpdateDir '.claude')
Push-Location $UpdateDir
try { $DryOut = @(& $Bin update --dry-run 2>&1); $DryCode = $LASTEXITCODE } finally { Pop-Location }
$DryText = $DryOut | Out-String
$LockAfter = Get-FileState (Join-Path $UpdateDir 'apm.lock.yaml')
$ModulesAfter = Get-TreeManifest (Join-Path $UpdateDir 'apm_modules')
$TargetAfter = Get-TreeManifest (Join-Path $UpdateDir '.claude')
$DryOut | Set-Content -Encoding utf8 (Join-Path $Scratch 'update-dry-run-live.txt')

Assert ($DryCode -eq 0) "update --dry-run exit=$DryCode"
Assert ($DryText -match [regex]::Escape('Update plan for apm.yml')) 'plan heading missing'
Assert ($DryText -match 'v1\.0\.0' -and $DryText -match 'v1\.5\.0') 'old/new versions missing from plan'
Assert ($LockAfter -ceq $LockBefore) 'lockfile bytes changed during dry-run'
Assert (@(Compare-Object $ModulesBefore $ModulesAfter).Count -eq 0) 'apm_modules tree/bytes changed during dry-run'
Assert (@(Compare-Object $TargetBefore $TargetAfter).Count -eq 0) 'target tree/bytes changed during dry-run'
```

預期：exit 0 且印出 `Update plan for apm.yml` 與 `v1.0.0 -> v1.5.0`；lockfile、`apm_modules/`、`.claude/` 的 absent/目錄/檔名/length/SHA manifest 前後零差異。尤其不得先 `RemoveAll`/重 clone 再說「最後內容一樣」。

**權威來源**：`prd.md` Requirements/AC；oracle Gate 2；register §3.3 D-1；`cmd/apm/update.go` `runUpdate`（現有 clear→plan→`deployAndFinalize` 流程）與 `printUpdateSummary`。

### [x] P0Q-19 · 非 dry-run 既有 update 仍套用；零副作用測試本體完整

**驗證證據（2026-07-12）**：同 fixture 隨後正常 update exit 0；lock 含 v1.5.0、target skill bytes 為 v1.5，installed HEAD 與 remote HEAD 同為 `038ab11aefd3061e624db04e82323ec002e63b20`，證明 dry-run 未吃掉真變更。已讀 test 本體，直接檢查 plan、lock bytes、module/target snapshots。

```powershell
Push-Location $UpdateDir
try { $ApplyOut = @(& $Bin update 2>&1); $ApplyCode = $LASTEXITCODE } finally { Pop-Location }
$AppliedLock = Get-Content -Raw (Join-Path $UpdateDir 'apm.lock.yaml')
$AppliedSkill = Get-Content -Raw (Join-Path $UpdateDir '.claude/skills/demo/SKILL.md')
$InstalledHead = (& git -C (Join-Path $UpdateDir 'apm_modules/remote') rev-parse HEAD).Trim()
Assert ($ApplyCode -eq 0) "normal update exit=$ApplyCode"
Assert ($AppliedLock -match 'v1\.5\.0') 'normal update did not rewrite lock to v1.5.0'
Assert ($AppliedSkill -match 'demo v1\.5') 'normal update did not redeploy target bytes'
Assert ($InstalledHead -eq $V15Head) 'normal update did not materialize v1.5 checkout'

$DryCase = [regex]::Match($UpdateTests, '(?s)func TestRunUpdate_DryRunPlanNoSideEffects\(.*?(?=\nfunc |\z)')
Assert ($DryCase.Success) 'dry-run zero-side-effect named test missing'
foreach ($Token in @('Update plan for apm.yml','apm.lock.yaml','apm_modules')) {
    Assert ($DryCase.Value -match [regex]::Escape($Token)) "dry-run test body missing $Token"
}
Assert ($DryCase.Value -match '\.claude|target') 'dry-run test does not snapshot a target directory'
Assert ($DryCase.Value -match 'Equal|DeepEqual|SHA|hash|bytes') 'dry-run test lacks byte/content equality assertions'
```

預期：同一 fixture 隨後不帶 flag 時真正更新 lock/module/target；unit/integration test 本體直接鎖 plan 與三個零副作用面，不能只測「沒有呼叫 deploy mock」。

**權威來源**：`prd.md` #5/Requirements；register §3.3；`cmd/apm/update_e2e_test.go` `TestRunUpdate_RealGitSemver_ResolvesToNewTag` 既有 apply 路徑。

---

## 6. P0 #6：`normalize` / `validate` documented extensions

### [x] P0Q-20 · backend spec 明列 dev-only extension 與 validate 撞詞邊界

**驗證證據（2026-07-12）**：backend docs 五 token、SafeLoad/SafeDump contract、兩種 validate scope 與 Python 無 top-level extension 四組 regex 全通過。

```powershell
$BackendDocs = (Get-ChildItem (Join-Path $Repo '.trellis/spec/backend') -Filter '*.md' -File |
    ForEach-Object { "`n<!-- $($_.Name) -->`n" + (Get-Content -Raw $_.FullName) }) -join "`n"
foreach ($Token in @('normalize','validate','dev-only','EXTENSION','marketplace validate')) {
    Assert ($BackendDocs -match [regex]::Escape($Token)) "backend spec missing $Token"
}
Assert ($BackendDocs -match '(?is)normalize.{0,800}SafeLoad.{0,300}SafeDump|SafeLoad.{0,300}SafeDump.{0,800}normalize') 'normalize dev-tool contract missing'
Assert ($BackendDocs -match '(?is)validate.{0,1200}marketplace validate.{0,500}(different|distinct|scope|範疇)') 'two validate scopes are not explicitly distinguished'
Assert ($BackendDocs -match '(?is)Python.{0,500}(no|without|does not).{0,300}(normalize|top-level validate)|(normalize|top-level validate).{0,800}Python') 'extension classification lacks Python comparison'
```

預期：`.trellis/spec/backend/` 中有可搜尋的正式契約：兩者都是 dev-only CLI 化工具／EXTENSION；頂層 `validate <file>` 與 `marketplace validate NAME` 只是撞字，輸入與範疇不同。

**權威來源**：`prd.md` #6；register §5 P0 #6；`group-extensions.md` 指令 1/2；`cmd/apm/main.go` `normalizeCmd`/`validateCmd`。

### [x] P0Q-21 · 真 CLI 與 oracle 證明 extension/撞詞，不改既有功能

**驗證證據（2026-07-12）**：TEMP fixture `normalize`/top-level `validate` 與兩個 apm-go help 均 exit 0；Python top-level normalize/validate 均非零，Python `marketplace validate --help` exit 0；全部 oracle 呼叫在 scratch。

```powershell
$ExtDir = Join-Path $Scratch 'dev-only-extensions'
New-Item -ItemType Directory -Path $ExtDir | Out-Null
"name: ext`nversion: '1.0.0'`n" | Set-Content -Encoding utf8 (Join-Path $ExtDir 'apm.yml')
Push-Location $ExtDir
try {
    $NormOut = @(& $Bin normalize apm.yml 2>&1); $NormCode = $LASTEXITCODE
    $ValidateOut = @(& $Bin validate apm.yml 2>&1); $ValidateCode = $LASTEXITCODE
    $TopValidateHelp = @(& $Bin validate --help 2>&1); $TopValidateHelpCode = $LASTEXITCODE
    $MarketValidateHelp = @(& $Bin marketplace validate --help 2>&1); $MarketValidateHelpCode = $LASTEXITCODE
    $PyNormalize = @(& uv --project $OracleRepo run apm normalize --help 2>&1); $PyNormalizeCode = $LASTEXITCODE
    $PyValidate = @(& uv --project $OracleRepo run apm validate --help 2>&1); $PyValidateCode = $LASTEXITCODE
    $PyMarketValidate = @(& uv --project $OracleRepo run apm marketplace validate --help 2>&1); $PyMarketValidateCode = $LASTEXITCODE
} finally { Pop-Location }
Assert ($NormCode -eq 0 -and (($NormOut | Out-String) -match 'name:\s+ext')) 'normalize round-trip failed'
Assert ($ValidateCode -eq 0) "top-level validate exit=$ValidateCode"
Assert ($TopValidateHelpCode -eq 0 -and (($TopValidateHelp | Out-String) -match 'validate\s+<file>')) 'top-level validate file scope missing'
Assert ($MarketValidateHelpCode -eq 0 -and (($MarketValidateHelp | Out-String) -match '(?i)validate\s+.*name|marketplace')) 'marketplace validate NAME scope missing'
Assert ($PyNormalizeCode -ne 0 -and $PyValidateCode -ne 0) 'Python unexpectedly has top-level normalize/validate'
Assert ($PyMarketValidateCode -eq 0) 'Python marketplace validate disappeared'
```

預期：apm-go 兩個 dev tools 仍正常；兩個 apm-go validate help 的參數範疇可辨；Python 無兩個頂層 extension，但有 marketplace validate。所有 oracle 呼叫只讀且在 scratch。

**權威來源**：`group-extensions.md` 指令 1/2 live/source mapping；register 總覽表 `normalize`/`validate`；`cmd/apm/main.go:37-103`。

---

## 7. 負向範圍、格式與全 repo 回歸

### [x] P0Q-22 · 小型批次 scope guard：不偷做 P1/P2、不加依賴

**驗證證據（2026-07-12）**：相對 baseline 的 tracked+untracked 全落在 allowlist，out-of-scope count 0；`go.mod`/`go.sum` 零 diff；root help 無 approve/deny，update help 無 `--yes`。

```powershell
$Changed = @(& git -C $Repo diff --name-only $Base)
$Untracked = @(& git -C $Repo ls-files --others --exclude-standard)
$AllChanged = @($Changed + $Untracked | Sort-Object -Unique)
$Allowed = '^(cmd/apm/(init\.go|pack\.go|audit\.go|update\.go|.*_test\.go)|internal/manifest/manifest(_test)?\.go|\.trellis/spec/backend/[^/]+\.md|\.trellis/spec/evals/cli-surface-parity-register\.md|\.trellis/tasks/07-12-p0-parity-quickwins/.*)$'
$OutOfScope = @($AllChanged | Where-Object { $_.Replace('\','/') -notmatch $Allowed })
Assert ($OutOfScope.Count -eq 0) ('out-of-scope changed files: ' + ($OutOfScope -join ', '))
Assert (@(& git -C $Repo diff --name-only $Base -- go.mod go.sum).Count -eq 0) 'go.mod/go.sum changed'

$RootHelp = @(& $Bin --help 2>&1); $RootHelpCode = $LASTEXITCODE
$RootHelpText = $RootHelp | Out-String
Assert ($RootHelpCode -eq 0) 'root help failed'
Assert ($RootHelpText -notmatch '(?m)^\s+(approve|deny)\s') 'P1 approve/deny commands were implemented in this quick-win task'
Assert ($UpdateHelpText -notmatch '(?m)^\s+(-y,\s+)?--yes\b') 'update consent/--yes P1 scope leaked in'
```

預期：production 變更只落在四個既有 command files 與 manifest diagnostic 路徑，測試/指定 spec/task 文件可變；無新 dependency；沒有 approve/deny enforcement、update consent gate 等 non-goals。

**權威來源**：`prd.md` Non-Goals；register §5 P0/P1 分界；Ponytail/surgical-change project guidelines。

### [x] P0Q-23 · 本任務觸碰 Go 檔 gofmt gate（Windows CRLF 安全）

**驗證證據（2026-07-12）**：機械列舉 13 個 touched Go files；TEMP LF mirror 上 `gofmt -l` exit 0、輸出空，未改 source。

```powershell
$TouchedGo = @(
    @(& git -C $Repo diff --name-only $Base -- '*.go') +
    @(& git -C $Repo ls-files --others --exclude-standard -- '*.go') |
    Sort-Object -Unique
)
Assert ($TouchedGo.Count -gt 0) 'no touched Go files found'
$LFRoot = Join-Path $Scratch 'gofmt-lf-normalized'
$LFFiles = @()
foreach ($Rel in $TouchedGo) {
    $Source = Join-Path $Repo $Rel
    $Dest = Join-Path $LFRoot $Rel
    New-Item -ItemType Directory -Force -Path (Split-Path $Dest -Parent) | Out-Null
    $Text = [Text.Encoding]::UTF8.GetString([IO.File]::ReadAllBytes($Source)).Replace("`r`n", "`n")
    [IO.File]::WriteAllText($Dest, $Text, [Text.UTF8Encoding]::new($false))
    $LFFiles += $Dest
}
$Unformatted = @(& gofmt -l @LFFiles)
$FmtCode = $LASTEXITCODE
Assert ($FmtCode -eq 0) "gofmt exit=$FmtCode"
Assert ($Unformatted.Count -eq 0) ('substantively unformatted: ' + ($Unformatted -join ', '))
```

預期：tracked + untracked touched Go files 機械列舉無漏檔；TEMP LF mirror 的 `gofmt -l` exit 0 且輸出空。不得修改 source 來掩蓋驗證結果。

**權威來源**：project `AGENTS.md` `go fmt ./...`；同期 `07-12-codex-agent-toml/checklist.md` CAT-19。

### [x] P0Q-24 · 全 repo build / vet / test gate

**驗證證據（2026-07-12）**：`go build ./...`、`go vet ./...`、`go test ./... -count=1` exit `0/0/0`；全輸出無 FAIL，四個核心 behavior tests 無 SKIP。

```powershell
Push-Location $Repo
try {
    & go build ./...; $AllBuildCode = $LASTEXITCODE
    & go vet ./...; $VetCode = $LASTEXITCODE
    $AllTests = @(& go test ./... -count=1 2>&1); $AllTestCode = $LASTEXITCODE
} finally { Pop-Location }
$AllTestText = $AllTests | Out-String
Assert ($AllBuildCode -eq 0) "go build ./... exit=$AllBuildCode"
Assert ($VetCode -eq 0) "go vet ./... exit=$VetCode"
Assert ($AllTestCode -eq 0) "go test ./... exit=$AllTestCode"
Assert ($AllTestText -notmatch '(?m)^FAIL\s|--- FAIL:|--- SKIP: Test(InitCmd_DoesNotSuggestRun|RunPack_NoMarketplaceDeferredInput|RunInstall_AllowExecutablesWarning|RunUpdate_DryRunPlanNoSideEffects)') 'full test output contains FAIL or required-test SKIP'
```

預期：三命令 exit `0/0/0`；全 repo 無 FAIL，四個核心 behavior tests 不得 SKIP。

**權威來源**：`prd.md` Requirements/AC；project `AGENTS.md` Available commands。

### [x] P0Q-25 · `ab_marketplace_pack.py` / `ab_uninstall.py` 快篩與 oracle 狀態不變

**驗證證據（2026-07-12）**：兩腳本 exit 0：pack `14 passed, 0 failed`，uninstall `6 passed, 0 failed, 2 documented deviations`；oracle marketplace SHA 未變。另因腳本自述只做 semantic、非 byte-exact，補做授權 stash baseline/current marketplace fixture：兩邊 exit `0/0`、CLI output byte-equal、輸出 tree 5 entries 零差異；claude JSON SHA `C3633FD5FA7BAC90F2321684FDBBE0FF80706DDCF58E439CF37D4A2F1A4B5B97`，codex JSON SHA `6F828669EFF4389E7B0567ADF41B6582F48A4CA923E2115A3A64E29320BB8738`；diff/status/stash 完全復原。

```powershell
Push-Location $Evals
try {
    $PackAB = @(& python ./ab_marketplace_pack.py 2>&1); $PackABCode = $LASTEXITCODE
    $UninstallAB = @(& python ./ab_uninstall.py 2>&1); $UninstallABCode = $LASTEXITCODE
} finally { Pop-Location }
$PackABText = $PackAB | Out-String
$UninstallABText = $UninstallAB | Out-String
$PackAB | Set-Content -Encoding utf8 (Join-Path $Scratch 'ab-marketplace-pack.txt')
$UninstallAB | Set-Content -Encoding utf8 (Join-Path $Scratch 'ab-uninstall.txt')
Assert ($PackABCode -eq 0) "ab_marketplace_pack.py exit=$PackABCode"
Assert ($PackABText -match 'total:\s+\d+ passed,\s+0 failed') 'marketplace pack A/B has failures'
Assert ($UninstallABCode -eq 0) "ab_uninstall.py exit=$UninstallABCode"
Assert ($UninstallABText -match 'total:\s+\d+ passed,\s+0 failed,\s+\d+ documented deviations') 'uninstall A/B has failures or lost deviation accounting'
Assert ((Get-FileState $OracleMarketplace) -ceq $OracleMarketplaceBefore) 'oracle marketplace registry changed during A/B scripts'
```

預期：兩腳本 exit 0、各自 `0 failed`；pack 有 marketplace 的既有 JSON path/parsed shape/雙 output/dry-run 行為無回歸，uninstall 快篩無回歸；oracle marketplace 狀態前後完全相同。

**權威來源**：`prd.md` Requirements/AC；`D:/Projects/apm-dev/evals/ab_marketplace_pack.py` scenario 1-5；`D:/Projects/apm-dev/evals/ab_uninstall.py` scenario 1-3。

---

## 8. Gate 5：parity register living-doc 與 commit 後補驗

### [x] P0Q-26 · P0 六列狀態、權威證據與 implementation commit 可追溯

**PASS（2026-07-12，主 session 於 fix commit 後執行 Round B）**：六列 commit 佔位
已回填 `845944c`；Round B 腳本實跑輸出 `P0Q-26 Round B: PASS (commits=845944c)`
——六列 SHA 可解析、無殘留佔位字串。
（Round A 記錄：六列皆完成且有處置證據，佔位一致，依規則 commit 前不勾選。）

本項分兩輪；**commit 前只能記錄 DEFERRED，不能勾選**。Round A 先把六列狀態改成完成、各列 evidence 寫 `commit: 待 commit 後補驗`；使用者之後自行 commit，再做 Round B 並以真 SHA 取代六個 placeholder。

```powershell
$RegisterPath = Join-Path $Repo '.trellis/spec/evals/cli-surface-parity-register.md'
$Register = Get-Content -Raw $RegisterPath
$P0Section = [regex]::Match($Register, '(?s)### P0（立即可做.*?(?=\n### P1)')
Assert ($P0Section.Success) 'register P0 section missing'
$Rows = @($P0Section.Value -split "`n" | Where-Object { $_ -match '^\|\s*[1-6]\s*\|' })
Assert ($Rows.Count -eq 6) "register P0 row count=$($Rows.Count)"
foreach ($N in 1..6) {
    $Row = $Rows | Where-Object { $_ -match ('^\|\s*' + $N + '\s*\|') } | Select-Object -First 1
    Assert ($Row -match '已完成|RESOLVED|DONE') "P0 row $N status not completed"
    Assert ($Row -match '證據|test|help|warning|spec|dry-run|commit') "P0 row $N lacks resolution evidence"
}

# Round A（commit 前）：六列都必須明示待 commit，結果記 DEFERRED，不得勾選。
$PendingRows = @($Rows | Where-Object { $_ -match 'commit:\s*待 commit 後補驗' })
Assert ($PendingRows.Count -in 0,6) 'commit placeholder is only partially applied across six rows'
if ($PendingRows.Count -eq 6) {
    'P0Q-26 Round A: DEFERRED — six rows complete; commit pending' |
        Set-Content -Encoding utf8 (Join-Path $Scratch 'register-round-a.txt')
    throw 'P0Q-26 remains unchecked until implementation commit exists'
}

# Round B（使用者 commit 後）：每列都要有可解析 SHA；可重複同一 atomic commit。
$Commits = @()
foreach ($N in 1..6) {
    $Row = $Rows | Where-Object { $_ -match ('^\|\s*' + $N + '\s*\|') } | Select-Object -First 1
    $Match = [regex]::Match($Row, '(?i)commit:\s*`?([0-9a-f]{7,40})`?')
    Assert ($Match.Success) "P0 row $N implementation commit missing"
    $Commits += $Match.Groups[1].Value
}
foreach ($Commit in @($Commits | Sort-Object -Unique)) {
    & git -C $Repo cat-file -e ($Commit + '^{commit}')
    Assert ($LASTEXITCODE -eq 0) "recorded commit does not resolve: $Commit"
}
Assert ($Register -notmatch 'commit:\s*待 commit 後補驗') 'pending commit placeholder remains after Round B'
```

預期：六列逐項有完成狀態、處置證據與 commit 欄。commit 前六列一致標 `待 commit 後補驗` 並保持本項未勾；commit 後六列皆換成可解析的真 SHA（可相同），register 不留假 SHA／partial placeholder。

**權威來源**：`prd.md` Acceptance Criteria「P0 六列狀態更新（含 commit）」；oracle Gate 5 lines 166-190；parity register 檔頭 Living Doc 與 §5 P0 表。

---

驗證完成時摘要格式：`VERDICT: CONFIRMED <n> / FAIL <n> / DEFERRED <n>`；只有 P0Q-01～P0Q-26 全部有可重跑證據、且 P0Q-26 Round B 完成後，才能宣告 task 完成。

VERDICT: CONFIRMED 25 / FAIL 0 / DEFERRED 1

驗證摘要（2026-07-12，第 1 輪）：`VERDICT: CONFIRMED 23 / FAIL 2 / DEFERRED 1`（FAIL 為測試引用產品常數未逐字鎖定文案）
驗證摘要（2026-07-12，第 2 輪）：`VERDICT: CONFIRMED 25 / FAIL 0 / DEFERRED 1`（mutation 辨識力抽測紅→綠）
驗證摘要（2026-07-12，P0Q-26 Round B commit 後補驗）：`VERDICT: CONFIRMED 26 / FAIL 0 / DEFERRED 0` —— 全數通過。
