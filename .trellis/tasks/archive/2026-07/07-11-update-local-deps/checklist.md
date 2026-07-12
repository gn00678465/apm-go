# 硬性驗證清單 — `apm update` local deps + 零 target 閘門

> **用途**：驗收 `update-local-deps` 的 Python oracle 定案、Go 修復、安全不變式、負向案例、文件收尾與全量回歸。任何「大致相同」、未核對 exit code、未附輸出／檔案證據或被 `SKIP` 的硬性案例，都算 FAIL。
>
> **圖例**：`[ ]` 待驗 · `[x]` 已驗——附證據才可勾。
>
> **判定規則**：每項需在項目下附執行日期、commit、完整命令、exit code 與必要的輸出／檔案 hash；只要一個明列斷言不成立，該項即 FAIL。
>
> **權威優先序**：Python oracle 源碼／實測 → `.trellis/spec/` 契約 → 本 task `design.md`／`research/findings.md` → apm-go 實作與測試。

---

## 0. 執行前置與安全

### [x] ULD-00 · 隔離 scratch、固定命令與 oracle 安全線

**PASS (2026-07-11, round 2, HEAD `2bad674`)**: helper 參數改為 `$CliArgs` 並以 `@CliArgs` splat；`Invoke-Go ... @('--help')` / `Invoke-Oracle ... @('--help')` 均 exit 0 且輸出 `Usage:`，證明 args 真正轉送。Scratch `C:\Users\gn006\AppData\Local\Temp\apm-update-check-c4f57466c0f8427dadfdc0f620d23212` 位於 TEMP、repo 外；全輪未對 oracle 執行 marketplace 子命令，結束後 scratch 已刪除。

在同一個 PowerShell session 執行下列初始化；後續 live 項目均使用這些 helper：

```powershell
$ErrorActionPreference = 'Stop'
$Repo = [IO.Path]::GetFullPath('D:/Projects/apm-dev/apm-go')
$OracleRoot = [IO.Path]::GetFullPath('D:/Projects/apm-dev/apm')
$Go = Join-Path $Repo 'bin/apm-go.exe'
$Scratch = Join-Path ([IO.Path]::GetTempPath()) ('apm-update-check-' + [guid]::NewGuid().ToString('N'))
New-Item -ItemType Directory -Path $Scratch | Out-Null

if ($Scratch.StartsWith($Repo, [StringComparison]::OrdinalIgnoreCase)) {
    throw "scratch must not be inside repo: $Scratch"
}
Set-Location $Repo

function Assert([bool]$Condition, [string]$Message) {
    if (-not $Condition) { throw "ASSERTION FAILED: $Message" }
}

function Set-DepContent([string]$Source, [string]$Token) {
    New-Item -ItemType Directory -Force -Path (Join-Path $Source '.apm/instructions') | Out-Null
    New-Item -ItemType Directory -Force -Path (Join-Path $Source '.apm/agents') | Out-Null
    Set-Content -Encoding utf8 -Path (Join-Path $Source 'apm.yml') -Value "name: dep-pkg`nversion: '1.0.0'"
    Set-Content -Encoding utf8 -Path (Join-Path $Source '.apm/instructions/style.instructions.md') -Value "---`napplyTo: '**/*.go'`n---`nrule-$Token"
    Set-Content -Encoding utf8 -Path (Join-Path $Source '.apm/agents/helper.md') -Value "agent-$Token"
}

function New-UpdateFixture(
    [string]$Name,
    [ValidateSet('manifest','signal','none')][string]$Target = 'manifest',
    [ValidateSet('dependencies','devDependencies')][string]$Bucket = 'dependencies',
    [bool]$Absolute = $false
) {
    $Project = Join-Path $Scratch $Name
    New-Item -ItemType Directory -Path $Project | Out-Null
    $Source = if ($Absolute) { Join-Path (Join-Path $Scratch ($Name + '-external')) 'dep-pkg' } else { Join-Path $Project 'dep-pkg' }
    Set-DepContent $Source 'v1'
    $DepSpec = if ($Absolute) { $Source.Replace('\','/') } else { './dep-pkg' }
    $TargetYaml = if ($Target -eq 'manifest') { "target:`n  - claude`n" } else { '' }
    Set-Content -Encoding utf8 -Path (Join-Path $Project 'apm.yml') -Value (
        "name: root`nversion: '1.0.0'`n$TargetYaml${Bucket}:`n  apm:`n    - '$DepSpec'"
    )
    if ($Target -eq 'signal') { New-Item -ItemType Directory -Path (Join-Path $Project '.claude') | Out-Null }
    [pscustomobject]@{ Project = $Project; Source = $Source; DepSpec = $DepSpec }
}

function Invoke-Go([string]$Cwd, [string[]]$CliArgs) {
    Push-Location $Cwd
    try { $Out = @(& $Go @CliArgs 2>&1); $Code = $LASTEXITCODE }
    finally { Pop-Location }
    [pscustomobject]@{ ExitCode = $Code; Output = ($Out | Out-String) }
}

function Invoke-Oracle([string]$Cwd, [string[]]$CliArgs) {
    Push-Location $Cwd
    try { $Out = @(& uv --project $OracleRoot run apm @CliArgs 2>&1); $Code = $LASTEXITCODE }
    finally { Pop-Location }
    [pscustomobject]@{ ExitCode = $Code; Output = ($Out | Out-String) }
}
```

預期：scratch 是 `%TEMP%` 下的新目錄且不在 repo 內；`$Repo`、`$OracleRoot` 均存在。**禁止**在 repo 根執行 install/update/uninstall。**禁止對 Python oracle 執行任何 `marketplace add/remove/update`**；Python 會直接改真實 `~/.apm/marketplaces.json`，一旦誤執行，本輪 oracle 證據作廢並須先從備份還原。

**權威來源**：`.trellis/spec/conformance/cli-verification-checklist.md:17-24`；`research/findings.md:6`；`implement.md:42-46`。

### [x] ULD-01 · 建置固定 binary 並確認 Python oracle 可執行

**PASS (2026-07-11, round 2, HEAD `2bad674`)**: 原樣執行 `go build -o bin/apm-go.exe ./cmd/apm` (exit 0)、`bin/apm-go.exe --help` (exit 0)、`uv --project D:/Projects/apm-dev/apm run apm --help` (exit 0)；binary SHA-256 `5D9F593918B2D60C369D27926AD2F4B3EB5FCC57B6F38C88E460D09FA2F0BFC5`。

```powershell
Push-Location $Repo
go build -o bin/apm-go.exe ./cmd/apm
$BuildExit = $LASTEXITCODE
& $Go --help *> $null
$GoExit = $LASTEXITCODE
& uv --project $OracleRoot run apm --help *> $null
$OracleExit = $LASTEXITCODE
Pop-Location
Assert ($BuildExit -eq 0) 'binary build exit must be 0'
Assert ($GoExit -eq 0) 'apm-go --help exit must be 0'
Assert ($OracleExit -eq 0) 'Python oracle --help exit must be 0'
Assert (Test-Path $Go -PathType Leaf) 'bin/apm-go.exe must exist'
```

預期：四個斷言全過；binary 名稱只能是 `apm-go.exe`。

**權威來源**：`.trellis/spec/conformance/cli-verification-checklist.md:19-21`；project `AGENTS.md` Available commands；`implement.md:40,67`。

---

## 1. Python oracle 定案（只在 scratch 執行）

### [x] ULD-02 · Oracle fresh update：local dep 會 materialize、deploy、寫 provenance

**PASS (2026-07-11, round 2)**: 修正 helper 後原樣 block exit 0；oracle copy、rule、agent、`_local/dep-pkg` lock provenance 全部存在；lock SHA-256 `0F0D31D7381E05FC163A016695A40F671B2FC1650483DD0C91B30E1AAFED24F3`。

```powershell
$F = New-UpdateFixture 'py-fresh-target' -Target manifest
$R = Invoke-Oracle $F.Project @('update','--yes')
$Lock = Get-Content -Raw (Join-Path $F.Project 'apm.lock.yaml')
Assert ($R.ExitCode -eq 0) "oracle fresh update exit=$($R.ExitCode)"
Assert (Test-Path (Join-Path $F.Project 'apm_modules/_local/dep-pkg/.apm/instructions/style.instructions.md')) 'oracle local copy missing'
Assert (Test-Path (Join-Path $F.Project '.claude/rules/style.md')) 'oracle rule deployment missing'
Assert (Test-Path (Join-Path $F.Project '.claude/agents/helper.md')) 'oracle agent deployment missing'
Assert ($Lock -match 'repo_url:\s+_local/dep-pkg') 'oracle lock repo_url must be _local/dep-pkg'
Assert ($Lock -match 'deployed_files:' -and $Lock -match 'deployed_file_hashes:') 'oracle lock provenance missing'
```

預期：exit 0；copy、兩類部署產物與 lockfile provenance 全存在。

**權威來源**：Python `commands/update.py:529-541`、`install/phases/resolve.py:573-617`、`install/sources.py:172-242`；`research/findings.md:10,36-40,49`；PRD AC `prd.md:38-40`。

### [x] ULD-03 · Oracle existing lock + 內容變更：plan unchanged 短路，不 refresh

**PASS (2026-07-11, round 2)**: 原樣兩次 oracle update exit `0,0`；第二次輸出 `already at their latest matching refs`，source 改為 v2 後 copied/deployed 仍皆為 `agent-v1`。

```powershell
$F = New-UpdateFixture 'py-plan-unchanged' -Target manifest
$First = Invoke-Oracle $F.Project @('update','--yes')
Assert ($First.ExitCode -eq 0) 'oracle baseline update must exit 0'
Set-DepContent $F.Source 'v2'
$Second = Invoke-Oracle $F.Project @('update','--yes')
$Copied = Get-Content -Raw (Join-Path $F.Project 'apm_modules/_local/dep-pkg/.apm/agents/helper.md')
$Deployed = Get-Content -Raw (Join-Path $F.Project '.claude/agents/helper.md')
Assert ($Second.ExitCode -eq 0) "oracle unchanged update exit=$($Second.ExitCode)"
Assert ($Second.Output -match 'already at their latest matching refs') 'oracle unchanged message missing'
Assert ($Copied -match 'agent-v1' -and $Deployed -match 'agent-v1') 'oracle must leave copied/deployed v1 on unchanged plan'
```

預期：第二次 exit 0，輸出 unchanged 訊息，source 已是 v2 但 materialized/deployed 仍是 v1。

**權威來源**：Python `commands/update.py:500-510`、`install/phases/resolve.py:475-496`、`install/plan.py:200-229`；`research/findings.md:39-41,50-52`；design D2 `design.md:124-126`。

### [x] ULD-04 · Oracle 零 target + plan 有變更：exit 2、先 materialize、不寫 lockfile

**PASS (2026-07-11, round 2)**: 原樣 oracle `update --yes` exit 2，輸出含 `No harness detected`；resolve-time materialized dir 存在，lockfile 與 `.claude` 均不存在。

```powershell
$F = New-UpdateFixture 'py-zero-target-new' -Target none
$R = Invoke-Oracle $F.Project @('update','--yes')
Assert ($R.ExitCode -eq 2) "oracle zero-target changed-plan exit=$($R.ExitCode)"
Assert ($R.Output -match 'No harness detected') 'oracle teaching message missing'
Assert (Test-Path (Join-Path $F.Project 'apm_modules/_local/dep-pkg')) 'oracle resolve-time materialization missing'
Assert (-not (Test-Path (Join-Path $F.Project 'apm.lock.yaml'))) 'oracle must not write lockfile after target failure'
Assert (-not (Test-Path (Join-Path $F.Project '.claude'))) 'oracle must not deploy without target'
```

預期：exit **2**；`No harness detected`；resolve 副作用的 local copy 存在，但無 lockfile、無 target 部署。

**權威來源**：Python `install/pipeline.py:565-603,629-636`、`core/errors.py:22-31,71-94`、`core/target_detection.py:799-807`；`research/findings.md:12,38,42,53`。

### [x] ULD-05 · Oracle 零 target + plan unchanged：plan gate 先 exit 0

**PASS (2026-07-11, round 2)**: 原樣兩次 oracle update exit `0,0`；第二次命中 unchanged plan 訊息且無 `No harness detected`，lock SHA-256 前後均 `73FA7BD7FBA7D3A058093B242998516A63F62AEF1DE35CADF083B3BF4CB09DDC`。

```powershell
$F = New-UpdateFixture 'py-zero-target-unchanged' -Target signal
$First = Invoke-Oracle $F.Project @('update','--yes')
Assert ($First.ExitCode -eq 0) 'oracle baseline must exit 0'
Remove-Item -LiteralPath (Join-Path $F.Project '.claude') -Recurse -Force
$Before = (Get-FileHash (Join-Path $F.Project 'apm.lock.yaml') -Algorithm SHA256).Hash
$Second = Invoke-Oracle $F.Project @('update','--yes')
$After = (Get-FileHash (Join-Path $F.Project 'apm.lock.yaml') -Algorithm SHA256).Hash
Assert ($Second.ExitCode -eq 0) "oracle zero-target unchanged exit=$($Second.ExitCode)"
Assert ($Second.Output -match 'already at their latest matching refs') 'oracle unchanged message missing'
Assert ($Second.Output -notmatch 'No harness detected') 'target phase must not run after unchanged plan gate'
Assert ($Before -eq $After) 'oracle unchanged update must not rewrite lockfile'
```

預期：第二次 exit 0；沒有 `No harness detected`；lockfile byte hash 不變。

**權威來源**：Python `commands/update.py:500-510`、`install/pipeline.py:597-603`；`research/findings.md:12,54,67`；design D3 `design.md:126`。

---

## 2. TDD 證據 gate

### [x] ULD-06 · RED：production fix 前，指定測試必須因正確缺口而紅

**PASS (2026-07-11, round 2 RED)**: 精確執行 `git stash push -- cmd/apm/update.go` → targeted tests exit 1 → `git stash pop`；5 個指定 tests 全紅（local materialize/provenance、fresh-install bytes、scoped token、zero-target）。因 `core.autocrlf=true`，pop 後先用 gofmt 恢復 LF，再核對 `update.go` stash 前後 SHA-256 均 `2F26DF9D9F2EB96E70BC2C75E8D31BD7884043476CA46B8FEADC658BC3D08694`、binary diff SHA-256 均 `1F3DF0C331CB20AD3E118DFB13247AD48C6D58917DE48EBF96D1F3479B0E038B`，stash count 仍 0、gofmt clean。

```powershell
go test ./cmd/apm/ -run 'TestRunUpdate_LocalDep|TestRunUpdate_DepsPresentZeroTarget|TestRunUpdate_Scoped_LocalPathToken' -count=1 -v
```

預期：在 production code 修改前 exit 1；失敗證據至少分別指出：(1) local dep 未 materialize/deploy 或 lock key 變裸路徑、(2) 零 target 未回 exit 2／lockfile 被改、(3) scoped `./dep-pkg` normalization 後找不到。若沒有帶 commit/time 的 RED log，不能補勾此項。

**權威來源**：PRD AC `prd.md:39-40`；`design.md:146-149`；`implement.md:13-17,26-27`。

### [x] ULD-07 · GREEN：同一批 targeted tests 全綠且無 skip

**PASS (2026-07-11, round 2)**: 復原後 `go test ./cmd/apm/ -run '^TestRunUpdate_' -count=1 -v` exit 0；19 個 tests 全部 PASS、無 SKIP/FAIL，涵蓋 targeted 的 5 個 local/zero-target/scoped cases。

```powershell
$Out = @(& go test ./cmd/apm/ -run 'TestRunUpdate_LocalDep|TestRunUpdate_DepsPresentZeroTarget|TestRunUpdate_Scoped_LocalPathToken' -count=1 -v 2>&1)
$Code = $LASTEXITCODE
$Text = $Out | Out-String
Assert ($Code -eq 0) "targeted update tests exit=$Code"
Assert ($Text -match '--- PASS: TestRunUpdate_LocalDep') 'local-dep PASS evidence missing'
Assert ($Text -match '--- PASS: TestRunUpdate_DepsPresentZeroTarget') 'zero-target PASS evidence missing'
Assert ($Text -match '--- PASS: TestRunUpdate_Scoped_LocalPathToken') 'scoped-local PASS evidence missing'
Assert ($Text -notmatch '--- SKIP:|--- FAIL:') 'targeted suite must have no SKIP/FAIL'
```

預期：exit 0；三族測試皆出現 PASS，無 SKIP/FAIL。

**權威來源**：`implement.md:18-21,28-35,63-65`；design C1-C3 `design.md:22-80`。

---

## 3. apm-go live 行為

### [x] ULD-08 · Relative prod dep：fresh-copy、部署刷新、穩定 `_local` key 與 provenance

**PASS (2026-07-11, round 2)**: 原樣 install/update exit `0,0`；key 穩定為 `_local/dep-pkg-45e35597`、stale canary 消失、materialized/rule/agent 皆刷新為 v2，provenance 存在且無 bare key；lock SHA-256 `EE93A94129117972664D49B2F1C4F622BC15ACAFE3F362B2C023EDA319CFB17D`。

```powershell
$F = New-UpdateFixture 'go-relative-refresh' -Target manifest
$Install = Invoke-Go $F.Project @('install')
Assert ($Install.ExitCode -eq 0) 'baseline install must exit 0'
$LocalBefore = @(Get-ChildItem (Join-Path $F.Project 'apm_modules/_local') -Directory)
Assert ($LocalBefore.Count -eq 1 -and $LocalBefore[0].Name -match '^dep-pkg-[0-9a-f]{8}$') 'expected one hashed local module dir'
$Key = '_local/' + $LocalBefore[0].Name
Set-Content -Encoding utf8 -Path (Join-Path $LocalBefore[0].FullName 'stale-only.txt') -Value 'must disappear'
Set-DepContent $F.Source 'v2'
$Update = Invoke-Go $F.Project @('update')
$Lock = Get-Content -Raw (Join-Path $F.Project 'apm.lock.yaml')
$LocalAfter = @(Get-ChildItem (Join-Path $F.Project 'apm_modules/_local') -Directory)
Assert ($Update.ExitCode -eq 0) "go update exit=$($Update.ExitCode)"
Assert ($LocalAfter.Count -eq 1 -and ('_local/' + $LocalAfter[0].Name) -eq $Key) 'local key must stay stable'
Assert (-not (Test-Path (Join-Path $LocalAfter[0].FullName 'stale-only.txt'))) 'stale copy must be removed before recopy'
Assert ((Get-Content -Raw (Join-Path $LocalAfter[0].FullName '.apm/agents/helper.md')) -match 'agent-v2') 'materialized content not refreshed'
Assert ((Get-Content -Raw (Join-Path $F.Project '.claude/agents/helper.md')) -match 'agent-v2') 'agent deployment not refreshed'
Assert ((Get-Content -Raw (Join-Path $F.Project '.claude/rules/style.md')) -match 'rule-v2') 'rule deployment not refreshed'
Assert ($Lock -match ('repo_url:\s+' + [regex]::Escape($Key))) 'stable _local lock entry missing'
Assert ($Lock -match 'deployed_files:' -and $Lock -match 'deployed_file_hashes:') 'deployed provenance missing'
Assert ($Lock -notmatch 'repo_url:\s+\./dep-pkg') 'bare local path lock entry is forbidden'
```

預期：exit 0；stale copy 消失、module/deploy 全為 v2、key 不變且 hashes 非空，lockfile 無裸 `./dep-pkg` 條目。

**權威來源**：spec §4 `.trellis/spec/backend/install-marketplace-contracts.md:56-77`；design C1 `design.md:22-39`；Go `install.go:1258-1336`、`clone.go:250-277`；`research/findings.md:11,23-25,49-50`。

### [x] ULD-09 · Update 結果與同內容 fresh install 的部署 bytes 一致

**PASS (2026-07-11, round 2)**: 原樣 update/fresh-install A/B 全過；rule SHA-256 均 `4AC8B5C2E7E62BAE149401CF8CB9CA16E3CF9943FE5B06821EB28C6A06E0EF81`，agent SHA-256 均 `83A8F36EBA754E3DAA9C6EE745A1428A7B9933E731272400126599892F1B2722`。

```powershell
$U = New-UpdateFixture 'go-update-side' -Target manifest
Assert ((Invoke-Go $U.Project @('install')).ExitCode -eq 0) 'update-side baseline install failed'
Set-DepContent $U.Source 'v2'
Assert ((Invoke-Go $U.Project @('update')).ExitCode -eq 0) 'update-side update failed'

$I = New-UpdateFixture 'go-install-side' -Target manifest
Set-DepContent $I.Source 'v2'
Assert ((Invoke-Go $I.Project @('install')).ExitCode -eq 0) 'fresh install side failed'

foreach ($Rel in @('.claude/rules/style.md','.claude/agents/helper.md')) {
    $UH = (Get-FileHash (Join-Path $U.Project $Rel) -Algorithm SHA256).Hash
    $IH = (Get-FileHash (Join-Path $I.Project $Rel) -Algorithm SHA256).Hash
    Assert ($UH -eq $IH) "$Rel differs between update and fresh install"
}
```

預期：兩個 CLI run 均 exit 0；rule/agent 的 SHA-256 逐檔相同。

**權威來源**：PRD AC `prd.md:39-40`；design C1 `design.md:24-30`；`research/findings.md:10,49-52`。

### [x] ULD-10 · 四格矩陣：relative/absolute × prod/dev 全部 update

**PASS (2026-07-11, round 2)**: 原樣 `dependencies/devDependencies × relative/absolute` matrix 4/4 皆 install/update exit 0；每格只有一個 hashed local dir 且 deployed agent 為 v2。

```powershell
foreach ($Bucket in @('dependencies','devDependencies')) {
    foreach ($Absolute in @($false,$true)) {
        $Name = "matrix-$Bucket-$Absolute"
        $F = New-UpdateFixture $Name -Target manifest -Bucket $Bucket -Absolute $Absolute
        Assert ((Invoke-Go $F.Project @('install')).ExitCode -eq 0) "$Name baseline install failed"
        Set-DepContent $F.Source 'v2'
        $R = Invoke-Go $F.Project @('update')
        $Dirs = @(Get-ChildItem (Join-Path $F.Project 'apm_modules/_local') -Directory)
        Assert ($R.ExitCode -eq 0) "$Name update exit=$($R.ExitCode)"
        Assert ($Dirs.Count -eq 1 -and $Dirs[0].Name -match '^dep-pkg-[0-9a-f]{8}$') "$Name local module key invalid"
        Assert ((Get-Content -Raw (Join-Path $F.Project '.claude/agents/helper.md')) -match 'agent-v2') "$Name deploy not refreshed"
    }
}
```

預期：4/4 case exit 0，均只有一個 hashed `_local` module 且部署 v2。

**權威來源**：design C1 `design.md:24-29`；spec §3/§4 `.trellis/spec/backend/install-marketplace-contracts.md:46-64`；Go install normalization `cmd/apm/install.go:306-311`。

### [x] ULD-11 · Scoped relative local token `update ./dep-pkg` 保持可用

**PASS (2026-07-11, round 2)**: 原樣 `apm-go update ./dep-pkg` exit 0、無 not-found，deployed agent 為 `agent-v2`。

```powershell
$F = New-UpdateFixture 'go-scoped-relative' -Target manifest
Assert ((Invoke-Go $F.Project @('install')).ExitCode -eq 0) 'baseline install failed'
Set-DepContent $F.Source 'v2'
$R = Invoke-Go $F.Project @('update','./dep-pkg')
Assert ($R.ExitCode -eq 0) "scoped relative update exit=$($R.ExitCode)"
Assert ($R.Output -notmatch 'package .* not found') 'relative token must match normalized key'
Assert ((Get-Content -Raw (Join-Path $F.Project '.claude/agents/helper.md')) -match 'agent-v2') 'scoped relative update did not refresh deployment'
```

預期：exit 0，無 `package not found`，部署刷新。

**權威來源**：design C3 `design.md:78-80`；Python local key `install/plan.py:57-71`；`research/findings.md:29,55,66,76`。

### [x] ULD-12 · Scoped absolute local token 保持可用

**PASS (2026-07-11, round 2)**: 原樣 `apm-go update <TEMP absolute dep path>` exit 0、無 not-found，deployed agent 為 `agent-v2`。

```powershell
$F = New-UpdateFixture 'go-scoped-absolute' -Target manifest -Absolute $true
Assert ((Invoke-Go $F.Project @('install')).ExitCode -eq 0) 'absolute baseline install failed'
Set-DepContent $F.Source 'v2'
$R = Invoke-Go $F.Project @('update',$F.Source)
Assert ($R.ExitCode -eq 0) "scoped absolute update exit=$($R.ExitCode)"
Assert ($R.Output -notmatch 'package .* not found') 'absolute token must match normalized key'
Assert ((Get-Content -Raw (Join-Path $F.Project '.claude/agents/helper.md')) -match 'agent-v2') 'scoped absolute update did not refresh deployment'
```

預期：exit 0，無 `package not found`，absolute token 只匹配其 local dep。

**權威來源**：design C1/C3 `design.md:24,78-80`；spec §4 `.trellis/spec/backend/install-marketplace-contracts.md:58-64`；Go `install.go:1282-1317`。

### [x] ULD-13 · Go 零 target + plan 有變更：plan 先印、exit 2、零 persistent partial write

**PASS (2026-07-11, round 2)**: 原樣 update exit 2；`Update plan for apm.yml` 在 combined output offset 4，teaching error 在 offset 72。Lock SHA-256 前後均 `E77AE9CDCA8D1628F73D07BFB488D8A12640876C58F0621594BDE2684860A8D5`、manifest 均 `F3E46B9C2719D883A0484579447700B3E27118F57F9216712508F75663483721`，無 `.claude` deploy。

```powershell
$F = New-UpdateFixture 'go-zero-target-change' -Target signal
Assert ((Invoke-Go $F.Project @('install')).ExitCode -eq 0) 'baseline install failed'
Remove-Item -LiteralPath (Join-Path $F.Project '.claude') -Recurse -Force
$Second = Join-Path $F.Project 'dep-new'
Set-DepContent $Second 'v2'
Add-Content -Encoding utf8 -Path (Join-Path $F.Project 'apm.yml') -Value "    - './dep-new'"
$LockBefore = (Get-FileHash (Join-Path $F.Project 'apm.lock.yaml') -Algorithm SHA256).Hash
$ManifestBefore = (Get-FileHash (Join-Path $F.Project 'apm.yml') -Algorithm SHA256).Hash
$R = Invoke-Go $F.Project @('update')
$LockAfter = (Get-FileHash (Join-Path $F.Project 'apm.lock.yaml') -Algorithm SHA256).Hash
$ManifestAfter = (Get-FileHash (Join-Path $F.Project 'apm.yml') -Algorithm SHA256).Hash
$PlanAt = $R.Output.IndexOf('Update plan for apm.yml')
$GateAt = $R.Output.IndexOf('no deployment target detected')
Assert ($R.ExitCode -eq 2) "zero-target changed-plan exit=$($R.ExitCode)"
Assert ($PlanAt -ge 0 -and $GateAt -gt $PlanAt) 'plan must print before teaching error'
Assert ($LockBefore -eq $LockAfter) 'zero-target gate must not rewrite lockfile'
Assert ($ManifestBefore -eq $ManifestAfter) 'zero-target gate must not rewrite apm.yml'
Assert (-not (Test-Path (Join-Path $F.Project '.claude'))) 'zero-target gate must not deploy'
```

預期：exit **2**；combined output 中 plan 在 teaching message 前；lockfile/apm.yml byte hash 不變；無部署。resolve-time materialization 可發生，不算 persistent lock/manifest partial write。

**權威來源**：design C2 `design.md:41-76,104-107`；spec §2 `.trellis/spec/backend/install-marketplace-contracts.md:34-42`；`research/findings.md:73-75`。

### [x] ULD-14 · Empty manifest + existing empty lock：零 deps 不觸發 target gate

**PASS (2026-07-11, round 2, strengthened)**: 原樣 `update` exit 0 且輸出 `Already up to date`，無 teaching error；lock SHA-256 前後均 `41DAECA5A1F47850F81B24C2F28BAC090B237055196690D73144FB34E954ECDF`、manifest 均 `937F8AD1118C9079530ACB70F71ABA205AE1C5AE001AF87C173FFF4D5AC83970`，無 modules 或 deploy。成功簽章與零副作用斷言可排除裸 CLI 空洞通過。

```powershell
$P = Join-Path $Scratch 'go-empty-update'
New-Item -ItemType Directory -Path $P | Out-Null
Set-Content -Encoding utf8 (Join-Path $P 'apm.yml') "name: empty`nversion: '1.0.0'"
Set-Content -Encoding utf8 (Join-Path $P 'apm.lock.yaml') "lockfile_version: '1'`ndependencies: []"
$ManifestBefore = (Get-FileHash (Join-Path $P 'apm.yml') -Algorithm SHA256).Hash
$LockBefore = (Get-FileHash (Join-Path $P 'apm.lock.yaml') -Algorithm SHA256).Hash
$R = Invoke-Go $P @('update')
Assert ($R.ExitCode -eq 0) "empty update exit=$($R.ExitCode)"
Assert ($R.Output -match 'Already up to date') 'empty update success signature missing'
Assert ($R.Output -notmatch 'no deployment target detected') 'empty update must not hit target gate'
Assert ($ManifestBefore -eq (Get-FileHash (Join-Path $P 'apm.yml') -Algorithm SHA256).Hash) 'empty update rewrote apm.yml'
Assert ($LockBefore -eq (Get-FileHash (Join-Path $P 'apm.lock.yaml') -Algorithm SHA256).Hash) 'empty update rewrote lockfile'
Assert (-not (Test-Path (Join-Path $P 'apm_modules'))) 'empty update must not materialize'
Assert (-not (Test-Path (Join-Path $P '.claude'))) 'empty update must not deploy'
```

預期：exit 0，輸出 `Already up to date`，無 teaching error；manifest/lockfile byte hash 不變，無 materialize/deploy。

**權威來源**：design C2 `design.md:43-47`；spec §2 `.trellis/spec/backend/install-marketplace-contracts.md:38-40`。

### [x] ULD-15 · Missing lockfile：維持 fail-loud exit 1 且零副作用

**PASS (2026-07-11, round 2)**: 原樣 `update` exit 1，輸出含 `apm-go update requires an existing apm.lock.yaml`；無 modules/deploy，manifest hash 不變。

```powershell
$F = New-UpdateFixture 'go-no-lock' -Target manifest
$ManifestBefore = (Get-FileHash (Join-Path $F.Project 'apm.yml') -Algorithm SHA256).Hash
$R = Invoke-Go $F.Project @('update')
Assert ($R.ExitCode -eq 1) "missing-lock update exit=$($R.ExitCode)"
Assert ($R.Output -match 'requires an existing apm.lock.yaml') 'missing-lock error missing'
Assert (-not (Test-Path (Join-Path $F.Project 'apm_modules'))) 'missing-lock update must not materialize'
Assert (-not (Test-Path (Join-Path $F.Project '.claude'))) 'missing-lock update must not deploy'
Assert ($ManifestBefore -eq (Get-FileHash (Join-Path $F.Project 'apm.yml') -Algorithm SHA256).Hash) 'apm.yml changed'
```

預期：exit **1**；錯誤明確；無 modules、deploy 或 manifest mutation。

**權威來源**：Go `cmd/apm/update.go:74-76`；design non-goal/D1 `design.md:18,124`；`research/findings.md:30,49,77`。

### [x] ULD-16 · Unknown scoped local token：exit 1、lockfile 不動

**PASS (2026-07-11, round 2)**: 原樣 `update ./missing` exit 1 且輸出 package not found；lock SHA-256 前後均 `E200D48D5D969CEE13ED154023FD8F5891BC6E770FB9021C5E0C4E731F145D89`。

```powershell
$F = New-UpdateFixture 'go-scoped-missing' -Target manifest
Assert ((Invoke-Go $F.Project @('install')).ExitCode -eq 0) 'baseline install failed'
$Before = (Get-FileHash (Join-Path $F.Project 'apm.lock.yaml') -Algorithm SHA256).Hash
$R = Invoke-Go $F.Project @('update','./missing')
$After = (Get-FileHash (Join-Path $F.Project 'apm.lock.yaml') -Algorithm SHA256).Hash
Assert ($R.ExitCode -eq 1) "unknown scoped token exit=$($R.ExitCode)"
Assert ($R.Output -match 'package .* not found in manifest') 'not-found error missing'
Assert ($Before -eq $After) 'not-found scoped update must not rewrite lockfile'
```

預期：exit **1**；錯誤含 package not found；lockfile hash 不變。

**權威來源**：resolver `internal/resolver/update.go:42-63`；design C3 `design.md:78-80`。

### [x] ULD-17 · 連續兩次 update 不得重現裸 key／provenance data loss

**PASS (2026-07-11, round 2)**: 原樣 v2/v3 連續兩次 update 皆 exit 0；每次恰一個 hashed `_local` row、無 bare key且 provenance 存在；最終 lock SHA-256 `56BC6967851ED8F58EA478B1490DEC8FCDEE914A7E0D7EF3F578AEAF68B6BAA8`。

```powershell
$F = New-UpdateFixture 'go-repeat-update' -Target manifest
Assert ((Invoke-Go $F.Project @('install')).ExitCode -eq 0) 'baseline install failed'
foreach ($Token in @('v2','v3')) {
    Set-DepContent $F.Source $Token
    Assert ((Invoke-Go $F.Project @('update')).ExitCode -eq 0) "update $Token failed"
    $Lock = Get-Content -Raw (Join-Path $F.Project 'apm.lock.yaml')
    $RepoRows = [regex]::Matches($Lock, '(?m)^\s*-?\s*repo_url:').Count
    Assert ($RepoRows -eq 1) "expected exactly one dependency, got $RepoRows"
    Assert ($Lock -match 'repo_url:\s+_local/dep-pkg-[0-9a-f]{8}') 'hashed local key missing'
    Assert ($Lock -notmatch 'repo_url:\s+\./dep-pkg') 'bare key reappeared'
    Assert ($Lock -match 'deployed_files:' -and $Lock -match 'deployed_file_hashes:') 'provenance lost'
}
```

預期：兩次皆 exit 0；每次 lock 只有一個 hashed local entry，且 deployed files/hashes 保持存在。

**權威來源**：`research/findings.md:11,25,50,65`；design C1/compatibility `design.md:28-30,112-116`。

---

## 4. 安全不變式與回歸

### [x] ULD-18 · `archive.ContainedKey` 同時守住 update purge 與 local materialize destination

**PASS (2026-07-11, round 2 spot-check)**: containment 相關三包重跑 exit 0；`TestContainedKey`、2 個 gitops destination guards、2 個 update purge guards 全部 PASS，無 SKIP/FAIL。

```powershell
go test ./internal/archive/ -run '^TestContainedKey$' -count=1 -v
Assert ($LASTEXITCODE -eq 0) 'ContainedKey unit test failed'
go test ./internal/gitops/ -run '^(TestLoadPackage_RefusesVirtualPathEscapingModulesDir|TestMaterializeLocalCopy_RefusesKeyEscapingModulesDir)$' -count=1 -v
Assert ($LASTEXITCODE -eq 0) 'gitops containment tests failed'
go test ./cmd/apm/ -run '^(TestRunUpdate_RefusesVirtualPathEscapingApmModules|TestRunUpdate_RefusesParentSegmentStayingInsideApmModules)$' -count=1 -v
Assert ($LASTEXITCODE -eq 0) 'update containment tests failed'
```

預期：三個命令 exit 0；所有 canary/sibling survival assertions PASS；任何 SKIP/FAIL 都不合格。

**權威來源**：spec §4 security invariant `.trellis/spec/backend/install-marketplace-contracts.md:61,64,77`；Go `internal/archive/extract.go:213-234`、`cmd/apm/update.go:110-128`、`internal/gitops/clone.go:241-253`；design C4 `design.md:82-88`。

### [x] ULD-19 · Symlink 安全拒絕：`copyTreeNoSymlinks` 不 follow、不 copy

**PASS (2026-07-11, round 2 spot-check)**: 兩個 copy/symlink named tests 重跑 exit 0、皆 PASS、無 SKIP/FAIL；同批 gitops safety suite 共 4/4 PASS。

```powershell
$Out = @(& go test ./internal/gitops/ -run '^(TestMaterializeLocalCopy_CopiesTreeUnderModulesDir|TestCopyTreeNoSymlinks_SkipsSymlinks)$' -count=1 -v 2>&1)
$Code = $LASTEXITCODE
$Text = $Out | Out-String
$CopyImpl = Get-Content -Raw (Join-Path $Repo 'internal/gitops/clone.go')
Assert ($Code -eq 0) "copy/symlink tests exit=$Code"
Assert ($Text -match '--- PASS: TestMaterializeLocalCopy_CopiesTreeUnderModulesDir') 'copy happy path PASS missing'
Assert ($Text -match '--- PASS: TestCopyTreeNoSymlinks_SkipsSymlinks') 'symlink rejection PASS missing'
Assert ($Text -notmatch '--- SKIP:|--- FAIL:') 'symlink SKIP/FAIL is not acceptable; rerun where symlink creation is supported'
Assert ($CopyImpl -match 'ModeSymlink[\s\S]{0,120}continue') 'symlink skip guard missing from copyTreeNoSymlinks'
Assert ($CopyImpl -match '!info\.Mode\(\)\.IsRegular\(\)[\s\S]{0,120}continue') 'non-regular-file skip guard missing from copyTreeNoSymlinks'
```

預期：regular files copy；source 內 symlink 無論指向哪裡都不被 dereference 或落到 destination；非 regular file 同樣不 copy。這裡「拒絕」的契約是 skip link 本身，不是讓整次 update 報錯。

**權威來源**：spec §4 `.trellis/spec/backend/install-marketplace-contracts.md:61,77`；Go `internal/gitops/clone.go:280-322`、`clone_test.go:404-432`；design C4 `design.md:84-87`。

### [x] ULD-20 · `LocalSourcePath` 僅 runtime：absolute source 不得洩入 lockfile

**PASS (2026-07-11, round 2)**: `TestNormalizeLocalDep` exit 0、7 個 named subtests PASS；absolute live install/update 均 exit 0，lock 不含 runtime field 或 native/slash absolute source、含 hashed `_local` key；lock SHA-256 `E1CFC1CC2C21A52E7F2B9FD394C6DE914D8B2508746AF42A279BDB211F7F0414`。

```powershell
go test ./cmd/apm/ -run '^TestNormalizeLocalDep$' -count=1 -v
Assert ($LASTEXITCODE -eq 0) 'normalizeLocalDep test failed'
$F = New-UpdateFixture 'go-runtime-only-path' -Target manifest -Absolute $true
Assert ((Invoke-Go $F.Project @('install')).ExitCode -eq 0) 'absolute baseline install failed'
Assert ((Invoke-Go $F.Project @('update')).ExitCode -eq 0) 'absolute update failed'
$Lock = Get-Content -Raw (Join-Path $F.Project 'apm.lock.yaml')
Assert ($Lock -notmatch 'LocalSourcePath|local_source_path') 'runtime-only field serialized'
Assert ($Lock -notmatch [regex]::Escape($F.Source)) 'native absolute source path leaked into lockfile'
Assert ($Lock -notmatch [regex]::Escape($F.Source.Replace('\','/'))) 'slash-normalized absolute source path leaked into lockfile'
Assert ($Lock -match 'repo_url:\s+_local/dep-pkg-[0-9a-f]{8}') 'contained synthetic key missing'
```

預期：unit PASS；lockfile 只含 `_local/...` synthetic key，不含 runtime field 或 absolute source。apm.yml 對 out-of-tree absolute dep 保留 absolute path是既定契約，不算洩漏。

**權威來源**：spec §4 `.trellis/spec/backend/install-marketplace-contracts.md:60-64`；Go `cmd/apm/install.go:1258-1300`；design C4 `design.md:84-88`。

### [x] ULD-21 · Live「只刪自己裝的」：clean owned 刪、手改 owned 留、user sibling 留

**PASS (2026-07-11, round 2, corrected contract)**: 以真實 `uninstall ./dep-pkg`（無不存在的 `--yes`）原樣重跑 exit 0；輸出 `modified since deploy (hash mismatch)`，clean rule 刪除、edited owned 與 user sibling 保留、local module/manifest entry 移除，最後一個 dep 移除後 lockfile 整檔不存在。

```powershell
$F = New-UpdateFixture 'go-uninstall-ownership' -Target manifest
Assert ((Invoke-Go $F.Project @('install')).ExitCode -eq 0) 'baseline install failed'
Set-DepContent $F.Source 'v2'
Assert ((Invoke-Go $F.Project @('update')).ExitCode -eq 0) 'update failed'
$LocalDir = @(Get-ChildItem (Join-Path $F.Project 'apm_modules/_local') -Directory)[0].FullName
Set-Content -Encoding utf8 -Path (Join-Path $F.Project '.claude/agents/helper.md') -Value 'USER EDIT'
Set-Content -Encoding utf8 -Path (Join-Path $F.Project '.claude/agents/user-owned.md') -Value 'USER OWNED'
$R = Invoke-Go $F.Project @('uninstall','./dep-pkg')
Assert ($R.ExitCode -eq 0) "uninstall exit=$($R.ExitCode)"
Assert ($R.Output -match 'hash mismatch|modified since deploy') 'edited-file keep diagnostic missing'
Assert (-not (Test-Path (Join-Path $F.Project '.claude/rules/style.md'))) 'clean owned rule should be removed'
Assert ((Get-Content -Raw (Join-Path $F.Project '.claude/agents/helper.md')) -match 'USER EDIT') 'edited owned file must be kept'
Assert ((Get-Content -Raw (Join-Path $F.Project '.claude/agents/user-owned.md')) -match 'USER OWNED') 'unrecorded sibling must be kept'
Assert (-not (Test-Path $LocalDir)) 'owned materialized module should be removed'
Assert ((Get-Content -Raw (Join-Path $F.Project 'apm.yml')) -notmatch '\./dep-pkg') 'manifest local dep should be removed'
Assert (-not (Test-Path (Join-Path $F.Project 'apm.lock.yaml'))) 'last dependency removal should delete lockfile'
```

預期：exit 0；只移除 hash 相符的 clean file；hash mismatch 與未記錄 sibling 都保留；local module/manifest entry 正確移除；最後一個 dep 移除後 lockfile 整檔刪除。

**權威來源**：Go `internal/deploy/uninstall.go:12-33,34-81`、`cmd/apm/uninstall.go:173-191`；spec §4 uninstall translation `.trellis/spec/backend/install-marketplace-contracts.md:74-77`；`research/findings.md:65`。

### [x] ULD-22 · Uninstall 單元安全網：hash、path、sibling、local-key translation

**PASS (2026-07-11, round 2 spot-check)**: deploy 6 個與 cmd 2 個 uninstall safety tests 重跑 exit `0,0`，共 8 個 named tests PASS、無 SKIP/FAIL。

```powershell
go test ./internal/deploy/ -run '^(TestRemoveDeployedFiles_(NormalRemoval|HashMismatchIsKeptWithWarning|MissingHashKeyIsKept|PathEscapeIsRejected)|TestSafeRemoveModuleDir_(SiblingPackageSurvives|PathEscapeIsRejected))$' -count=1 -v
Assert ($LASTEXITCODE -eq 0) 'deploy uninstall safety tests failed'
go test ./cmd/apm/ -run '^(TestPrepareUninstallPlan_LocalDepRemovalKeysUseModulesKey|TestRunUninstall_LocalPathDependencyRemovesModulesLockAndDeployedFiles)$' -count=1 -v
Assert ($LASTEXITCODE -eq 0) 'local uninstall key/round-trip tests failed'
```

預期：兩命令 exit 0，全部列名測試 PASS，無 SKIP/FAIL。

**權威來源**：Go `internal/deploy/uninstall_test.go:31-110,282-370`、`cmd/apm/uninstall_test.go:948-1002`；design compatibility `design.md:114-116`。

### [x] ULD-23 · Scoped frozen refusal 在任何 mutation 前發生

**PASS (2026-07-11, round 2)**: 原樣 scoped frozen update exit 1、輸出含 frozen；canary 存活，lock SHA-256 前後均 `B51E6DA09F2C8D1E5DA3F6123D7E45985D072402CBF40C3AE00C4B5E51B0A35D`。

```powershell
$F = New-UpdateFixture 'go-frozen-scope' -Target manifest
Assert ((Invoke-Go $F.Project @('install')).ExitCode -eq 0) 'baseline install failed'
$LocalDir = @(Get-ChildItem (Join-Path $F.Project 'apm_modules/_local') -Directory)[0].FullName
$Canary = Join-Path $LocalDir 'frozen-canary.txt'
Set-Content -Encoding utf8 -Path $Canary -Value 'must survive'
$Before = (Get-FileHash (Join-Path $F.Project 'apm.lock.yaml') -Algorithm SHA256).Hash
$R = Invoke-Go $F.Project @('update','./dep-pkg','--frozen')
$After = (Get-FileHash (Join-Path $F.Project 'apm.lock.yaml') -Algorithm SHA256).Hash
Assert ($R.ExitCode -eq 1) "frozen scoped update exit=$($R.ExitCode)"
Assert ($R.Output -match 'frozen') 'frozen refusal message missing'
Assert (Test-Path $Canary) 'frozen refusal mutated apm_modules'
Assert ($Before -eq $After) 'frozen refusal rewrote lockfile'
```

預期：exit **1**；canary 與 lock hash 均不變。

**權威來源**：Go `cmd/apm/update.go:53-59`；existing regression `cmd/apm/update_test.go:92-119`；design non-goals `design.md:14-18`。

### [x] ULD-24 · Git dep update 語意與既有 update tests 無回歸

**PASS (2026-07-11, round 2)**: `go test ./cmd/apm/ -run '^TestRunUpdate_' -count=1 -v` exit 0，19 個 tests PASS、無 SKIP/FAIL；列出的 5 個 git-semver/CI/frozen tests 均 PASS。

```powershell
$Out = @(& go test ./cmd/apm/ -run '^TestRunUpdate_' -count=1 -v 2>&1)
$Code = $LASTEXITCODE
$Text = $Out | Out-String
Assert ($Code -eq 0) "all TestRunUpdate_ tests exit=$Code"
foreach ($Name in @(
    'TestRunUpdate_Full_ReResolvesToNewestAndRewritesLock',
    'TestRunUpdate_Scoped_OnlyNamedPackageChanges',
    'TestRunUpdate_Scoped_NoFrozenOverridesCIAutoFrozen',
    'TestRunUpdate_GitSemver_InstallPathClearedEvenWhenTagUnchanged',
    'TestRunUpdate_GitSemver_DevDependency_InstallPathClearedEvenWhenTagUnchanged'
)) { Assert ($Text -match ('--- PASS: ' + [regex]::Escape($Name))) "$Name PASS missing" }
Assert ($Text -notmatch '--- SKIP:|--- FAIL:') 'update suite contains SKIP/FAIL'
```

預期：exit 0；五個受零-target fixture 影響的既有測試與所有其他 `TestRunUpdate_*` 均 PASS；git semver/frozen 行為不變。

**權威來源**：PRD non-goal `prd.md:45-48`；design scope/回歸清單 `design.md:13-18,134-144`；`implement.md:29-35`。

---

## 5. Spec、PRD 與 review 收尾

### [x] ULD-25 · Spec §4 Warning 已替換，D1/D2/D3 與真 commit 可驗

**PASS (2026-07-11, 主 session 於 fix commit 後補驗)**: spec §4 佔位文字已換為
「Fixed (task 07-11-update-local-deps; commit: `105b2f6`, 2026-07-11)」；下方驗證
腳本實跑輸出 `ULD-25: PASS (commit=105b2f6)`——舊 Warning 不存在、D1/D2/D3 齊、
commit 可 resolve 且 diff-tree 含 `cmd/apm/update.go`。
（round 2 記錄：舊 Warning 無、fixed 記錄與 D1/D2/D3 結構皆通過；regex 只接受
本 task 的 Fixed 行且要求 commit 實改 `cmd/apm/update.go`，不再誤抓 `171fd87`。）

```powershell
$SpecPath = Join-Path $Repo '.trellis/spec/backend/install-marketplace-contracts.md'
$Spec = Get-Content -Raw $SpecPath
$Section4 = (($Spec -split '## 4\. Local / absolute-path dependency materialization \(F1\)',2)[1] -split '\n## 5\.',2)[0]
Assert ($Section4 -notmatch 'Warning / follow-up \(F1 gap\).*does NOT call') 'old F1 warning still present'
Assert ($Section4 -match '(?i)update' -and $Section4 -match '(?i)fixed|修復') '§4 fixed record missing'
foreach ($Id in @('D1','D2','D3')) {
    Assert ($Spec -match ('(?m)^\|\s*' + $Id + '\s*\|')) "deviation $Id missing"
}
$CommitMatch = [regex]::Match($Section4, '(?m)^> \*\*Fixed \(task 07-11-update-local-deps; commit:\s*`?([0-9a-f]{7,40})`?\s*[,;)]')
Assert ($CommitMatch.Success) '§4 update fix commit hash missing'
$Commit = $CommitMatch.Groups[1].Value
& git -C $Repo cat-file -e ($Commit + '^{commit}')
Assert ($LASTEXITCODE -eq 0) 'recorded update fix commit does not resolve'
$CommitFiles = @(& git -C $Repo diff-tree --no-commit-id --name-only -r $Commit)
Assert ($CommitFiles -contains 'cmd/apm/update.go') 'recorded commit is not this task update implementation fix'
```

預期：舊 Warning 不存在；§4 有已修記錄；deviations 表有 D1/D2/D3；Fixed 記錄行中的 hash 是可解析且實際修改 `cmd/apm/update.go` 的 commit。

**權威來源**：PRD AC `prd.md:42-43`；design documented deviations `design.md:120-126`；`implement.md:49-54`；目前 spec Warning `.trellis/spec/backend/install-marketplace-contracts.md:72`。

### [x] ULD-26 · PRD 四項 AC 全勾且 oracle 記錄指向 findings

**PASS (2026-07-11, round 2 spot-check)**: AC 恰 4/4 `[x]`、無 unchecked；PRD 指向 findings，P1/P2/P3/P3b 全部存在。

```powershell
$Prd = Get-Content -Raw (Join-Path $Repo '.trellis/tasks/07-11-update-local-deps/prd.md')
$Ac = (($Prd -split '## Acceptance Criteria',2)[1] -split '## Non-Goals',2)[0]
Assert ([regex]::Matches($Ac, '(?m)^- \[x\]').Count -eq 4) 'expected exactly four checked ACs'
Assert ($Ac -notmatch '(?m)^- \[ \]') 'unchecked AC remains'
Assert ($Prd -match 'research/findings\.md') 'PRD must point to oracle findings'
$Findings = Get-Content -Raw (Join-Path $Repo '.trellis/tasks/07-11-update-local-deps/research/findings.md')
foreach ($Marker in @('P1','P2','P3','P3b')) { Assert ($Findings -match ('\b' + $Marker + '\b')) "oracle marker $Marker missing" }
```

預期：AC 恰好 4/4 `[x]`；PRD 有 findings link；P1/P2/P3/P3b 證據仍在。

**權威來源**：PRD `prd.md:36-43`；oracle matrix `research/findings.md:45-56`；`implement.md:55`。

### [x] ULD-27 · Review gate：CRITICAL/HIGH = 0，C1-C4 全部 PASS

**PASS (2026-07-11, round 2 adversarial re-review)**: 受查範圍 `HEAD 2bad674..working tree`，tracked files `update.go/update_test.go/update_e2e_test.go` + new `update_local_test.go`（SHA-256 `B462F02232BE55E927A125B54E59FF84B867FE72B0B8F3E3CD4BE06ABC600CA3`）；`git diff --check` clean。結論 `CRITICAL = 0`、`HIGH = 0`；C1 materialize/provenance、C2 fail-closed/heading order/no-write、C3 relative+absolute scoped token、C4 containment/symlink/runtime-only/uninstall safety 全部 PASS。

硬性斷言：review 證據必須列出受查 commit/diff 範圍，結論中 `CRITICAL = 0`、`HIGH = 0`，並逐條標記 design C1（materialize/provenance）、C2（零 target）、C3（scoped local token）、C4（安全不變式）為 PASS；任何未處置 CRITICAL/HIGH 或只寫總評而無 C1-C4 對映均 FAIL。

**權威來源**：`implement.md:56-59`；design contracts `design.md:20-88`。

---

## 6. 全 repo 品質與 A/B 回歸 gate

### [x] ULD-28 · gofmt 只讀檢查乾淨

**PASS (2026-07-11, round 2)**: 補強 gate 為本 task 四個 Go files；`gofmt -l cmd/apm/update.go cmd/apm/update_test.go cmd/apm/update_e2e_test.go cmd/apm/update_local_test.go` exit 0、輸出空白。

```powershell
$Unformatted = @(& gofmt -l cmd/apm/update.go cmd/apm/update_test.go cmd/apm/update_e2e_test.go cmd/apm/update_local_test.go)
Assert ($LASTEXITCODE -eq 0) 'gofmt command failed'
Assert ($Unformatted.Count -eq 0) ('unformatted files: ' + ($Unformatted -join ', '))
```

預期：exit 0 且輸出空白。

**權威來源**：`implement.md:39`；Go formatting gate。

### [x] ULD-29 · 全 repo build + 固定 binary build

**PASS (2026-07-11, round 2)**: `go build ./...` 與固定 binary build 均 exit 0；`bin/apm-go.exe` SHA-256 `5D9F593918B2D60C369D27926AD2F4B3EB5FCC57B6F38C88E460D09FA2F0BFC5`。

```powershell
go build ./...
Assert ($LASTEXITCODE -eq 0) 'go build ./... failed'
go build -o bin/apm-go.exe ./cmd/apm
Assert ($LASTEXITCODE -eq 0) 'fixed binary build failed'
Assert (Test-Path (Join-Path $Repo 'bin/apm-go.exe') -PathType Leaf) 'fixed binary missing'
```

預期：兩個 build 均 exit 0；`bin/apm-go.exe` 存在。

**權威來源**：PRD AC `prd.md:41`；`implement.md:40,66-67`；project `AGENTS.md` Available commands。

### [x] ULD-30 · 全 repo vet

**PASS (2026-07-11, round 2)**: `go vet ./...` exit 0，0 diagnostic。

```powershell
go vet ./...
Assert ($LASTEXITCODE -eq 0) 'go vet ./... failed'
```

預期：exit 0，無 vet diagnostic。

**權威來源**：PRD AC `prd.md:41`；`implement.md:39,66`。

### [x] ULD-31 · 全 repo tests + `cmd/apm` coverage ≥ 80%

**PASS (2026-07-11, round 2)**: `go test ./...` exit 0（17 packages）；`go test ./cmd/apm/ -cover -count=1` exit 0，coverage `86.1%`。

```powershell
go test ./...
Assert ($LASTEXITCODE -eq 0) 'go test ./... failed'
$CoverOut = @(& go test ./cmd/apm/ -cover -count=1 2>&1)
$CoverCode = $LASTEXITCODE
$CoverText = $CoverOut | Out-String
$M = [regex]::Match($CoverText, 'coverage:\s+([0-9.]+)%')
Assert ($CoverCode -eq 0) "cmd/apm coverage test exit=$CoverCode"
Assert ($M.Success) 'coverage percentage missing'
Assert ([double]$M.Groups[1].Value -ge 80.0) "coverage below 80%: $($M.Groups[1].Value)%"
```

預期：全 repo exit 0；`cmd/apm` coverage 至少 80.0%。

**權威來源**：PRD AC `prd.md:41`；`implement.md:41`；project test coverage target。

### [x] ULD-32 · A/B：MCP install parity 無回歸

**PASS (2026-07-11, round 2, network)**: exit 0；`Results: 14/15 passed, 0 failed, 1 skipped, 3 documented deviations`、`VERDICT: PASS`、無 `[FAIL]`。

```powershell
$Out = @(& python D:/Projects/apm-dev/evals/ab_mcp_install_parity.py 2>&1)
$Code = $LASTEXITCODE
$Text = $Out | Out-String
Assert ($Code -eq 0) "ab_mcp_install_parity exit=$Code"
Assert ($Text -match 'VERDICT: PASS') 'PASS verdict missing'
Assert ($Text -notmatch '\[FAIL\]|VERDICT: FAIL') 'A/B reported failure'
```

預期：exit 0、`VERDICT: PASS`、0 failed；該腳本既定 interactive TTY `SKIP` 可接受。此項標記 **[network]**。

**權威來源**：`implement.md:47`；`D:/Projects/apm-dev/evals/ab_mcp_install_parity.py:20-25,74-84,252-261`。

### [x] ULD-33 · A/B：instructions applyTo / 零 target 無回歸

**PASS (2026-07-11, round 2)**: exit 0；26 個 PASS signature（含 summary），結尾 `ALL CHECKS PASSED (ab_instructions_applyto)`，無 FAIL/FAILED。

```powershell
$Out = @(& python D:/Projects/apm-dev/evals/ab_instructions_applyto.py 2>&1)
$Code = $LASTEXITCODE
$Text = $Out | Out-String
Assert ($Code -eq 0) "ab_instructions_applyto exit=$Code"
Assert ($Text -match 'ALL CHECKS PASSED \(ab_instructions_applyto\)') 'success signature missing'
Assert ($Text -notmatch '(?m)^FAIL\s|FAILED:') 'A/B reported failure'
```

預期：exit 0、success signature 存在、無 FAIL/FAILED；腳本自身 scratch 清理完成。

**權威來源**：`implement.md:47`；`D:/Projects/apm-dev/evals/ab_instructions_applyto.py:2-19,92-160`。

### [x] ULD-34 · A/B：uninstall provenance 路徑無回歸

**PASS (2026-07-11, round 2)**: exit 0；summary `total: 6 passed, 0 failed, 2 documented deviations`，無 `[FAIL]`。

```powershell
$Out = @(& python D:/Projects/apm-dev/evals/ab_uninstall.py 2>&1)
$Code = $LASTEXITCODE
$Text = $Out | Out-String
Assert ($Code -eq 0) "ab_uninstall exit=$Code"
Assert ($Text -match 'failed,') 'summary line missing'
Assert ($Text -match '0 failed') 'A/B failed count is not zero'
Assert ($Text -notmatch '\[FAIL\]') 'A/B reported failure'
```

預期：exit 0、summary 為 `0 failed`、無 `[FAIL]`；documented deviations 不算失敗。

**權威來源**：`implement.md:47`；`D:/Projects/apm-dev/evals/ab_uninstall.py:2-28,162-181`；「只刪自己裝的」實作 `internal/deploy/uninstall.go:12-33`。

---

驗證摘要（2026-07-11，第 1 輪）：`VERDICT: CONFIRMED 15 / FAIL 19 / DEFERRED 1`（大宗 FAIL 為 checklist 自身 PowerShell helper `$Args` 遮蔽缺陷；實作側真缺陷 2 項：ULD-13 heading、ULD-28 gofmt）
驗證摘要（2026-07-11，第 2 輪，harness 修復後全面重驗）：`VERDICT: CONFIRMED 34 / FAIL 0 / DEFERRED 1`
驗證摘要（2026-07-11，ULD-25 commit 後補驗）：`VERDICT: CONFIRMED 35 / FAIL 0 / DEFERRED 0` —— 全數通過。
