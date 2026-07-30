#requires -Version 7
# Tier 1 確定性閘門 — install-dev
# 出邊載荷（供 plugin-init 檢查）：E4 `apm-go install --help` 含 --dev 且可執行

$ErrorActionPreference = 'Stop'
$repo = (Resolve-Path "$PSScriptRoot/../../..").Path
Push-Location $repo
$script:fails = @()
function Fail($ac,$m){ $script:fails += "[$ac] $m"; Write-Host "  FAIL [$ac] $m" -ForegroundColor Red }
function Pass($ac){ Write-Host "  ok   [$ac]" -ForegroundColor Green }

# Exec 執行一個 native command 並「立刻」驗 exit code（同 targets-init-shape 的修法：
# 只看副作用/檔案存不存在會漏掉「指令其實失敗但留下舊產物」的情況）。
function Exec {
    param([string]$ac, [string]$what, [scriptblock]$cmd)
    $out = & $cmd 2>&1 | Out-String
    if ($LASTEXITCODE -ne 0) {
        $detail = ($out -split "`n" | Where-Object { $_ -match '^(FAIL|--- FAIL|\s+\S+_test\.go:)' } |
                   Select-Object -First 12) -join "`n      "
        if (-not $detail) { $detail = ($out -split "`n" | Select-Object -Last 12) -join "`n      " }
        Fail $ac "$what 失敗（exit $LASTEXITCODE）`n      $detail"
        return $null
    }
    return $out
}

Write-Host "== Tier 1: install-dev ==" -ForegroundColor Cyan

$before = $script:fails.Count
$null = Exec 'AC-L2' 'go build ./...' { go build ./... }
if ($script:fails.Count -eq $before) { Pass 'AC-L2/build' }
$before = $script:fails.Count
$null = Exec 'AC-L2' 'go vet ./...' { go vet ./... }
if ($script:fails.Count -eq $before) { Pass 'AC-L2/vet' }

# ---- 全套件測試：必須先於任何個別 AC 檢查，且必須驗 exit code ----
# 唯一能擋住「本 task 綠但把別處弄壞了」的閘門（含既有 dev 讀取鏈以外的任何回歸）。
$before = $script:fails.Count
$null = Exec 'AC-L2' 'go test ./... -count=1（全套件）' { go test ./... -count=1 }
if ($script:fails.Count -eq $before) { Pass 'AC-L2/go-test-all' }

# ---- binary：先刪舊產物，避免 build 失敗時讀到上一次殘留的 binary ----
$bin = Join-Path $repo 'bin/apm-go.exe'
Remove-Item $bin -Force -ErrorAction SilentlyContinue
$before = $script:fails.Count
$null = Exec 'AC-L2' "go build -o $bin" { go build -o $bin ./cmd/apm-go }
if (-not (Test-Path $bin)) { Fail 'AC-L2' "build 後 $bin 不存在" }
elseif ($script:fails.Count -eq $before) { Pass 'AC-L2/binary' }

# ---- E4 出邊載荷：--dev 旗標存在 ----
$h = & $bin install --help 2>&1 | Out-String
if ($h -notmatch '--dev') { Fail 'AC42' '`install --help` 沒有 --dev（E4 邊載荷未建立）' } else { Pass 'AC42/flag' }

# ---- AC42/AC43：--dev 寫入 devDependencies 且鍵序正確 ----
$probe = Join-Path $env:TEMP ("apm-dev-" + [guid]::NewGuid().ToString('N'))
New-Item -ItemType Directory -Path $probe -Force | Out-Null
try {
  Push-Location $probe
  @"
name: dev-probe
version: 1.0.0
description: probe
author: t
targets:
  - claude
dependencies:
  apm: []
  mcp: []
includes: auto
scripts: {}
"@ | Set-Content -Path (Join-Path $probe 'apm.yml') -Encoding utf8

  & $bin install --dev pbakaus/impeccable 2>&1 | Out-Null
  $y = Get-Content (Join-Path $probe 'apm.yml') -Raw -ErrorAction SilentlyContinue
  if ($null -eq $y) {
    Fail 'AC42' 'apm.yml 消失'
  } else {
    if ($y -notmatch '(?ms)devDependencies:\s*\n\s+apm:\s*\n\s+-\s*pbakaus/impeccable') {
      Fail 'AC42' '套件未寫入 devDependencies.apm'
    } else { Pass 'AC42' }
    # 反向斷言必須 (a) 行首錨定、(b) 大小寫敏感。PowerShell 的 -match 預設
    # 不分大小寫，而 "devDependencies:" 含有子字串 "Dependencies:"，
    # 所以未錨定的 -match 會在正確產物上誤報。實測三種寫法：
    #   只有 devDependencies 的正確產物 → -match=True(誤報) / ^+-cmatch=False
    #   真的被重複寫入 dependencies      → -match=True        / ^+-cmatch=True
    # 即加上錨定與大小寫敏感後仍抓得到真缺陷，只是不再誤報。
    if ($y -cmatch '(?ms)^dependencies:\s*\n\s+apm:\s*\n\s+-\s*pbakaus/impeccable') {
      Fail 'AC42' '套件被誤寫入 dependencies.apm'
    }
    # AC43 鍵序：devDependencies 必須在 includes 與 scripts 之間
    $ks = ($y -split "`n" | Where-Object { $_ -match '^[a-zA-Z]' } | ForEach-Object { ($_ -split ':')[0] })
    $i_inc = [array]::IndexOf($ks,'includes'); $i_dev = [array]::IndexOf($ks,'devDependencies'); $i_scr = [array]::IndexOf($ks,'scripts')
    if ($i_dev -lt 0)               { Fail 'AC43' '沒有 devDependencies 鍵' }
    elseif (-not ($i_inc -lt $i_dev -and $i_dev -lt $i_scr)) {
      Fail 'AC43' "devDependencies 鍵序錯：includes=$i_inc dev=$i_dev scripts=$i_scr"
    } else { Pass 'AC43' }
  }
} finally { Pop-Location -EA SilentlyContinue; Remove-Item $probe -Recurse -Force -EA SilentlyContinue }

# ---- AC-L1 / R9.3：三個既有 dev 測試必須都在且全綠（-list 先證明匹配非空） ----
$listed = @(& go test ./cmd/apm-go/ -list 'TestRunInstall_DevDependency' 2>&1 | Where-Object { $_ -match '^TestRunInstall_DevDependency' })
if ($listed.Count -ne 3) {
  Fail 'AC-L1' "-list 匹配到 $($listed.Count) 個，應為 3 個既有 dev 測試"
} else {
  $before = $script:fails.Count
  $null = Exec 'AC-L1' "go test -run 'TestRunInstall_DevDependency'" { go test ./cmd/apm-go/ -run 'TestRunInstall_DevDependency' }
  if ($script:fails.Count -eq $before) { Pass 'AC-L1' }
}

# ---- AC42-cross：跨區段搬家（2026-07-30 Tier 2 阻斷級）----
# 「已宣告」判定原本只看 m.ParsedDeps，persistPackagesToManifest 也只掃它要寫的
# 那一個區段，兩層都是單區段視野 → install --dev X 在 X 已存在於 dependencies 時
# 會把 X 重複寫進兩個區段，且情境 B 連帶讓 lockfile 的 package_type 變空字串。
# 使用者裁定（2026-07-30）：搬家，不是重複也不是報錯。
$xTests = @(
  'TestRunInstall_Dev_ExistingDevDependency_BareInstall_MovesToNonDev'
  'TestRunInstall_Dev_ExistingDependency_DevInstall_MovesToDev'
)
foreach ($n in $xTests) {
  $m = @(& go test ./cmd/apm-go/ -list "^$n$" 2>&1 | Where-Object { $_ -match '^Test' })
  if ($m.Count -ne 1) { Fail 'AC42-cross' "-list 未精確匹配到 $n（得到 $($m.Count) 個）"; continue }
  $before = $script:fails.Count
  $null = Exec 'AC42-cross' "go test -run $n" { go test ./cmd/apm-go/ -run "^$n$" -count=1 }
  if ($script:fails.Count -eq $before) { Pass "AC42-cross/$n" }
}

# ---- 2026-07-30 第二輪根因分析（同一 session 的 codex 對抗性稽核）：問題 1
# （跨區段搬家丟失 ref:/alias: 等 metadata，removeMatchingEntry 只回傳 bool
# 不回傳被移除的節點）與問題 3（`--frozen --dev X` 印出 moved 訊息卻從未真正
# 持久化，因為 frozen 分支在 persistPackagesToManifest 之前就 return）。
# 教訓（本檔自己的 verification-record.md 已記錄過三次）：子代理常常忘記把
# 新測試接進這支閘門 -- 沒接進來，測試存在也不會有紅燈保護。
$followupTests = @(
  'TestRunInstall_Dev_CrossSectionMove_PreservesEntryMetadata'
  'TestRunInstall_Dev_MoveOutOfDevWithLegacyLock_StillPersistsManifestMove'
  'TestRunInstall_DevWithFrozen_Errors'
  'TestRunInstall_DevWithoutPackages_Errors'
)
foreach ($n in $followupTests) {
  $m = @(& go test ./cmd/apm-go/ -list "^$n$" 2>&1 | Where-Object { $_ -match '^Test' })
  if ($m.Count -ne 1) { Fail 'AC42-followup' "-list 未精確匹配到 $n（得到 $($m.Count) 個）"; continue }
  $before = $script:fails.Count
  $null = Exec 'AC42-followup' "go test -run $n" { go test ./cmd/apm-go/ -run "^$n$" -count=1 }
  if ($script:fails.Count -eq $before) { Pass "AC42-followup/$n" }
}

# ---- AC45：lockfile package_type 欄位存在於 Go 端 ----
$hasField = (& git grep -n 'PackageType' -- 'internal/lockfile/*.go' 2>&1 | Out-String)
if ($hasField -notmatch 'PackageType') { Fail 'AC45' 'internal/lockfile 沒有 PackageType 欄位（目前只有字串字面量）' } else { Pass 'AC45' }

# ---- AC-L9：未新增第三方相依 ----
$newReq = @((& git diff -- go.mod); (& git diff --cached -- go.mod)) | Where-Object { $_ -match '^\+\s+\S+\s+v' }
if ($newReq) { Fail 'AC-L9' ("go.mod 新增 require：" + ($newReq -join '; ')) } else { Pass 'AC-L9' }

# ---- 覆蓋率：唯一檔名寫在 repo 內、驗 exit code、用完刪除 ----
$cov = "$repo/apmcov-" + [guid]::NewGuid().ToString('N') + ".out"
$before = $script:fails.Count
$null = Exec 'AC-L2' 'go test ./... -coverprofile' { go test ./... -count=1 "-coverprofile=$cov" }
if ($script:fails.Count -eq $before) {
  if (-not (Test-Path $cov)) {
    Fail 'AC-L2' 'go test 未產生 coverprofile'
  } else {
    $t = (& go tool cover "-func=$cov" | Select-Object -Last 1)
    if ($t -match '(\d+\.\d+)%') { if ([double]$Matches[1] -lt 80) { Fail 'AC-L2' "覆蓋率 $($Matches[1])% < 80%" } else { Pass "AC-L2/coverage $($Matches[1])%" } }
    else { Fail 'AC-L2' '無法解析覆蓋率' }
  }
}
Remove-Item $cov -Force -ErrorAction SilentlyContinue

Pop-Location
Write-Host ""
if ($script:fails.Count -gt 0) {
  Write-Host "TIER 1 RED — $($script:fails.Count) 項失敗：" -ForegroundColor Red
  $script:fails | ForEach-Object { Write-Host "  $_" -ForegroundColor Red }
  Write-Host "被封鎖的下游：plugin-init 的 AC46（E4 邊）" -ForegroundColor Yellow
  exit 1
}
Write-Host "TIER 1 GREEN" -ForegroundColor Green
exit 0
