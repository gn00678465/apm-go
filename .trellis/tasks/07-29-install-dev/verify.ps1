#requires -Version 7
# Tier 1 確定性閘門 — install-dev
# 出邊載荷（供 plugin-init 檢查）：E4 `apm-go install --help` 含 --dev 且可執行

$ErrorActionPreference = 'Stop'
$repo = (Resolve-Path "$PSScriptRoot/../../..").Path
Push-Location $repo
$script:fails = @()
function Fail($ac,$m){ $script:fails += "[$ac] $m"; Write-Host "  FAIL [$ac] $m" -ForegroundColor Red }
function Pass($ac){ Write-Host "  ok   [$ac]" -ForegroundColor Green }

Write-Host "== Tier 1: install-dev ==" -ForegroundColor Cyan

& go build ./... 2>&1 | Out-Null
if ($LASTEXITCODE -ne 0) { Fail 'AC-L2' 'go build 非 0' } else { Pass 'AC-L2/build' }
& go vet ./... 2>&1 | Out-Null
if ($LASTEXITCODE -ne 0) { Fail 'AC-L2' 'go vet 非 0' } else { Pass 'AC-L2/vet' }

$bin = Join-Path $repo 'bin/apm-go.exe'
& go build -o $bin ./cmd/apm-go 2>&1 | Out-Null

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
    if ($y -match '(?ms)dependencies:\s*\n\s+apm:\s*\n\s+-\s*pbakaus/impeccable') {
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
  Pop-Location
} finally { Pop-Location -EA SilentlyContinue; Remove-Item $probe -Recurse -Force -EA SilentlyContinue }

# ---- AC-L1 / R9.3：三個既有 dev 測試必須都在且全綠 ----
$listed = @(& go test ./cmd/apm-go/ -list 'TestRunInstall_DevDependency' 2>&1 | Where-Object { $_ -match '^TestRunInstall_DevDependency' })
if ($listed.Count -ne 3) {
  Fail 'AC-L1' "-list 匹配到 $($listed.Count) 個，應為 3 個既有 dev 測試"
} else {
  & go test ./cmd/apm-go/ -run 'TestRunInstall_DevDependency' 2>&1 | Out-Null
  if ($LASTEXITCODE -ne 0) { Fail 'AC-L1' '既有 dev 讀取鏈測試轉紅 —— R9.3 被違反' } else { Pass 'AC-L1' }
}

# ---- AC45：lockfile package_type 欄位存在於 Go 端 ----
$hasField = (& git grep -n 'PackageType' -- 'internal/lockfile/*.go' 2>&1 | Out-String)
if ($hasField -notmatch 'PackageType') { Fail 'AC45' 'internal/lockfile 沒有 PackageType 欄位（目前只有字串字面量）' } else { Pass 'AC45' }

# ---- AC-L9：未新增第三方相依 ----
$newReq = @((& git diff -- go.mod); (& git diff --cached -- go.mod)) | Where-Object { $_ -match '^\+\s+\S+\s+v' }
if ($newReq) { Fail 'AC-L9' ("go.mod 新增 require：" + ($newReq -join '; ')) } else { Pass 'AC-L9' }

# ---- 覆蓋率 ----
& go test ./... -coverprofile="$repo/cover.out" 2>&1 | Out-Null
if (Test-Path "$repo/cover.out") {
  $t = & go tool cover -func="$repo/cover.out" | Select-Object -Last 1
  if ($t -match '(\d+\.\d+)%') { if ([double]$Matches[1] -lt 80) { Fail 'AC-L2' "覆蓋率 $($Matches[1])% < 80%" } else { Pass "AC-L2/coverage $($Matches[1])%" } }
} else { Fail 'AC-L2' '未產生 cover.out' }

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
