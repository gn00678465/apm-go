#requires -Version 7
# Tier 1 確定性閘門 — marketplace-add-fixes（無入邊、無出邊）

$ErrorActionPreference = 'Stop'
$repo = (Resolve-Path "$PSScriptRoot/../../..").Path
Push-Location $repo
$script:fails = @()
function Fail($ac,$m){ $script:fails += "[$ac] $m"; Write-Host "  FAIL [$ac] $m" -ForegroundColor Red }
function Pass($ac){ Write-Host "  ok   [$ac]" -ForegroundColor Green }

Write-Host "== Tier 1: marketplace-add-fixes ==" -ForegroundColor Cyan

& go build ./... 2>&1 | Out-Null
if ($LASTEXITCODE -ne 0) { Fail 'AC-L1' 'go build 非 0' } else { Pass 'AC-L1/build' }
& go vet ./... 2>&1 | Out-Null
if ($LASTEXITCODE -ne 0) { Fail 'AC-L1' 'go vet 非 0' } else { Pass 'AC-L1/vet' }

$bin = Join-Path $repo 'bin/apm-go.exe'
& go build -o $bin ./cmd/apm-go 2>&1 | Out-Null

# ---- AC47 / R10：add 有 --category、set 沒有 ----
$ha = & $bin marketplace package add --help 2>&1 | Out-String
$hs = & $bin marketplace package set --help 2>&1 | Out-String
if ($ha -notmatch '--category') { Fail 'AC47' '`package add --help` 沒有 --category' } else { Pass 'AC47/flag' }
if ($hs -match  '--category')   { Fail 'AC49' '`package set` 不該有 --category（上游旗標集合）' } else { Pass 'AC49' }

# ---- AC22 / R6：audit 錯誤訊息含補救指引 ----
$probe = Join-Path $env:TEMP ("apm-mkt-" + [guid]::NewGuid().ToString('N'))
New-Item -ItemType Directory -Path $probe -Force | Out-Null
try {
  Push-Location $probe
  $out = & $bin marketplace audit no-such-marketplace 2>&1 | Out-String
  if ($out -notmatch 'marketplace add') { Fail 'AC22' 'audit 未註冊錯誤訊息缺少 `marketplace add` 補救指引' }
  elseif ($out -notmatch 'marketplace list') { Fail 'AC22' 'audit 錯誤訊息缺少 `marketplace list` 提示' }
  else { Pass 'AC22' }
  Pop-Location
} finally { Pop-Location -EA SilentlyContinue; Remove-Item $probe -Recurse -Force -EA SilentlyContinue }

# ---- AC18：--no-verify 的 exit code 必須是 2（用 binary，不可用 go run） ----
# go run 會把子行程的 2 變成 1 —— 已實測，見 review/codex-audit-checklist.md 阻斷 4
$probe2 = Join-Path $env:TEMP ("apm-mkt2-" + [guid]::NewGuid().ToString('N'))
New-Item -ItemType Directory -Path $probe2 -Force | Out-Null
try {
  Push-Location $probe2
  @"
name: mkt-probe
version: 0.1.0
description: probe
marketplace:
  owner:
    name: acme-org
  outputs:
    claude: {}
  packages: []
"@ | Set-Content -Path (Join-Path $probe2 'apm.yml') -Encoding utf8
  & $bin marketplace package add owner/repo --no-verify 2>&1 | Out-Null
  if ($LASTEXITCODE -ne 2) { Fail 'AC18' "--no-verify 隱含 HEAD 應 exit 2，實際 $LASTEXITCODE" } else { Pass 'AC18' }
  Pop-Location
} finally { Pop-Location -EA SilentlyContinue; Remove-Item $probe2 -Recurse -Force -EA SilentlyContinue }

# ---- AC40：resolveRef 各分支測試存在（-list 先證明非零匹配） ----
$listed = @(& go test ./internal/marketplace/authoring/ -list 'ResolveRef|ImplicitHead' 2>&1 | Where-Object { $_ -match '^Test' })
if ($listed.Count -lt 3) { Fail 'AC40' "-list 只匹配 $($listed.Count) 個 resolveRef 分支測試，至少需 3" } else { Pass 'AC40' }

# ---- AC21：mkt-046 回歸閘門（local source 在「所有情境」永不觸網） ----
# 模式 9（驗證粒度必須追上宣稱粒度）：AC21 的宣稱含全稱量詞「所有情境」，
# 不得只 grep 兩個 pattern 就當覆蓋。AC40 列了 6 個分支，local 版本也必須逐一。
$localBranches = @(
  @{ n='ImplicitHead'; p='LocalSource.*ImplicitHead|LocalImplicitHead' },
  @{ n='ExplicitHead'; p='LocalSource.*ExplicitHead|LocalExplicitHead' },
  @{ n='Version';      p='LocalSource.*Version|LocalVersion' },
  @{ n='NoVerify';     p='LocalSource.*NoVerify|LocalNoVerify' },
  @{ n='ConcreteSHA';  p='LocalSource.*SHA|LocalSHA' },
  @{ n='ZeroFlag';     p='LocalSource.*ZeroFlag|LocalZeroFlag|RemoteSource_ZeroFlag' }
)
$missingLocal = @()
foreach ($b in $localBranches) {
  $m = @(& go test ./internal/marketplace/authoring/ -list $b.p 2>&1 | Where-Object { $_ -match '^Test' })
  if ($m.Count -eq 0) { $missingLocal += $b.n }
}
if ($missingLocal.Count -gt 0) {
  Fail 'AC21' ("local source 的下列情境沒有對應測試（宣稱是『所有情境』，粒度不匹配）：" + ($missingLocal -join '、'))
} else {
  & go test ./internal/marketplace/authoring/ -run 'LocalSource' 2>&1 | Out-Null
  if ($LASTEXITCODE -ne 0) { Fail 'AC21' 'mkt-046 回歸測試轉紅 —— 判定順序可能寫反' } else { Pass 'AC21' }
}

# ---- AC53：回歸閘門 —— marketplace init 必須維持非互動（D13） ----
# 防止實作 plugin-init 時順手把 clack 帶進 marketplace init。
$auth = Get-Content "$repo/cmd/apm-go/marketplace_authoring.go" -Raw
# 只取 marketplaceInitCmd 的函式體，不掃整個檔案（其他指令可能合法使用 ux 元件）
$m = [regex]::Match($auth, '(?s)func\s+marketplaceInitCmd\s*\([^)]*\)\s*\*cobra\.Command\s*\{.*?\n\}')
if (-not $m.Success) {
  Fail 'AC53' '找不到 marketplaceInitCmd 函式體，無法驗證非互動性'
} else {
  $body = $m.Value
  $banned = @('ux.NewClack','ck.Form','ck.MultiSelect','ck.Confirm','ck.Banner','ck.Intro','ck.Outro')
  $hit = $banned | Where-Object { $body -match [regex]::Escape($_) }
  if ($hit) {
    Fail 'AC53' ("marketplace init 出現互動元件（應維持非互動，D13）：" + ($hit -join '、'))
  } else { Pass 'AC53/no-clack' }
}
# 行為面：非互動指令在無 stdin 的情況下必須立即回傳，不得阻塞等待輸入
$probe3 = Join-Path $env:TEMP ("apm-mi-" + [guid]::NewGuid().ToString('N'))
New-Item -ItemType Directory -Path $probe3 -Force | Out-Null
try {
  Push-Location $probe3
  $job = Start-Job -ScriptBlock { param($b,$d) Set-Location $d; & $b marketplace init 2>&1 | Out-String } -ArgumentList $bin,$probe3
  if (Wait-Job $job -Timeout 15) {
    Receive-Job $job | Out-Null
    Pass 'AC53/no-block'
  } else {
    Stop-Job $job -Force
    Fail 'AC53' 'marketplace init 阻塞超過 15 秒 —— 疑似等待互動輸入'
  }
  Remove-Job $job -Force -EA SilentlyContinue
  Pop-Location
} finally { Pop-Location -EA SilentlyContinue; Remove-Item $probe3 -Recurse -Force -EA SilentlyContinue }

# ---- AC-L9 ----
$newReq = @((& git diff -- go.mod); (& git diff --cached -- go.mod)) | Where-Object { $_ -match '^\+\s+\S+\s+v' }
if ($newReq) { Fail 'AC-L9' ("go.mod 新增 require：" + ($newReq -join '; ')) } else { Pass 'AC-L9' }

# ---- 覆蓋率 ----
& go test ./... -coverprofile="$repo/cover.out" 2>&1 | Out-Null
if (Test-Path "$repo/cover.out") {
  $t = & go tool cover -func="$repo/cover.out" | Select-Object -Last 1
  if ($t -match '(\d+\.\d+)%') { if ([double]$Matches[1] -lt 80) { Fail 'AC-L1' "覆蓋率 $($Matches[1])% < 80%" } else { Pass "AC-L1/coverage $($Matches[1])%" } }
} else { Fail 'AC-L1' '未產生 cover.out' }

Pop-Location
Write-Host ""
if ($script:fails.Count -gt 0) {
  Write-Host "TIER 1 RED — $($script:fails.Count) 項失敗：" -ForegroundColor Red
  $script:fails | ForEach-Object { Write-Host "  $_" -ForegroundColor Red }
  Write-Host "被封鎖的下游：無" -ForegroundColor Yellow
  exit 1
}
Write-Host "TIER 1 GREEN" -ForegroundColor Green
exit 0
