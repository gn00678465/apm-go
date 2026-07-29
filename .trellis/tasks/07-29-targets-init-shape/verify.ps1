#requires -Version 7
# Tier 1 確定性閘門 — targets-init-shape
#
# 契約（.trellis/spec/guides/loop-graph-engineering.md 模式 2）：
#   - 只做確定性檢查；任一項失敗立即 exit 1 並印出失敗的 AC 編號
#   - 這是 AC 的「執行體」，不是摘要。AC 改了，這支就要改。
#   - agent 不得自行判定某條「不適用」而跳過；要跳過必須改本檔並經人審。
#
# 出邊載荷（供 plugin-init 檢查）：E1 buildManifestNode / E2 SupportedTargets / E3 ux seam

$ErrorActionPreference = 'Stop'
$repo = (Resolve-Path "$PSScriptRoot/../../..").Path
Push-Location $repo
$script:fails = @()

function Fail($ac, $msg) { $script:fails += "[$ac] $msg"; Write-Host "  FAIL [$ac] $msg" -ForegroundColor Red }
function Pass($ac)       { Write-Host "  ok   [$ac]" -ForegroundColor Green }

# Exec 執行一個 native command 並「立刻」驗 exit code。
#
# 2026-07-29 codex Tier 2 阻斷 1：本腳本原本多處只看副作用（檔案存不存在）而不看
# exit code。實測反證：改壞 internal/lockfile 的一個測試後，`go test ./...` exit 1，
# 但 coverprofile 照樣寫出 total 86.7%，於是整份閘門回報 TIER 1 GREEN / exit 0 ——
# 閘門犯了它自己要防的那個錯。所有 native 呼叫一律走這裡。
function Exec {
    param([string]$ac, [string]$what, [scriptblock]$cmd)
    $out = & $cmd 2>&1 | Out-String
    if ($LASTEXITCODE -ne 0) {
        # 失敗時必須印出實際原因。原本只回報 exit code，讓「閘門紅了」變成
        # 一個無法診斷的黑箱 —— 升級給人時對方也拿不到可行動的資訊。
        $detail = ($out -split "`n" | Where-Object { $_ -match '^(FAIL|--- FAIL|\s+\S+_test\.go:)' } |
                   Select-Object -First 12) -join "`n      "
        if (-not $detail) { $detail = ($out -split "`n" | Select-Object -Last 12) -join "`n      " }
        Fail $ac "$what 失敗（exit $LASTEXITCODE）`n      $detail"
        return $null
    }
    return $out
}

Write-Host "== Tier 1: targets-init-shape ==" -ForegroundColor Cyan

# ---- 全域：build / vet ----
$null = Exec 'AC-L1' 'go build ./...' { go build ./... }
if ($script:fails.Count -eq 0) { Pass 'AC-L1/build' }
$before = $script:fails.Count
$null = Exec 'AC-L1' 'go vet ./...' { go vet ./... }
if ($script:fails.Count -eq $before) { Pass 'AC-L1/vet' }

# ---- 全套件測試：必須先於任何個別 AC 檢查，且必須驗 exit code ----
# 個別 AC 的 -run 檢查只覆蓋本 task 的測試；其他套件轉紅時它們全都不會動，
# 所以這一條是唯一能擋住「本 task 綠但把別處弄壞了」的閘門。
$before = $script:fails.Count
$null = Exec 'AC-L1' 'go test ./... -count=1（全套件）' { go test ./... -count=1 }
if ($script:fails.Count -eq $before) { Pass 'AC-L1/go-test-all' }

# ---- binary：所有 CLI 閘門一律用絕對路徑的預先 build 產物 ----
# （codex 第一輪阻斷 1：從 temp dir 用 ..\bin 相對路徑會解析錯）
# 先刪舊產物：只驗 Test-Path 會讓 build 失敗時讀到上一次的 binary。
$bin = Join-Path $repo 'bin/apm-go.exe'
Remove-Item $bin -Force -ErrorAction SilentlyContinue
$before = $script:fails.Count
$null = Exec 'AC-L1' "go build -o $bin" { go build -o $bin ./cmd/apm-go }
if (-not (Test-Path $bin)) { Fail 'AC-L1' "build 後 $bin 不存在" }
elseif ($script:fails.Count -eq $before) { Pass 'AC-L1/binary' }

# ---- AC-L0：ux seam 已建立（出邊 E3） ----
$ia = Get-Content "$repo/internal/ux/interactive.go" -Raw
$seamOk = ($ia -match '(?m)^var\s+confirmWith\s*=') -and
          ($ia -match '(?m)^var\s+multiSelectWith\s*=') -and
          ($ia -match '(?m)^var\s+inputFormWith\s*=')
if (-not $seamOk) { Fail 'AC-L0' 'interactive.go 的三個 func 尚未改為 package-level var（E3 邊載荷）' } else { Pass 'AC-L0' }

# ---- AC-L3：seam 自身在 internal/ux 內有測試（含 restore 還原） ----
# 消費端（cmd/apm-go）用到 seam 不等於 seam 被測到：沒有測試證明 restore() 真的還原時，
# 一個外洩的 stub 會靜默解除後續所有互動測試的武裝，而且不會有任何紅燈。
$seamTests = @(& go test ./internal/ux/ -list 'PromptSeam|SeamRestore' 2>&1 | Where-Object { $_ -match '^Test' })
if ($seamTests.Count -eq 0) {
  Fail 'AC-L3' '-list 零匹配：internal/ux 內沒有 SetPromptSeamsForTest 自身的測試'
} else {
  & go test ./internal/ux/ -run 'PromptSeam|SeamRestore' 2>&1 | Out-Null
  if ($LASTEXITCODE -ne 0) { Fail 'AC-L3' 'seam 測試紅燈' } else { Pass "AC-L3（$($seamTests.Count) 個）" }
}
# internal/ux 覆蓋率有專屬門檻：terminal-ux-contract.md §6 記載 89.9%，spec 下限 80%。
$uxCov = (& go test ./internal/ux/ -cover 2>&1 | Out-String)
if ($uxCov -match 'coverage:\s*(\d+\.\d+)%') {
  $c = [double]$Matches[1]
  if ($c -lt 80) { Fail 'AC-L3' "internal/ux 覆蓋率 $c% < spec 下限 80%" } else { Pass "AC-L3/ux-coverage $c%" }
} else { Fail 'AC-L3' '無法解析 internal/ux 覆蓋率' }

# ---- AC24 / E2：SupportedTargets 為 6 元素且含 agent-skills ----
$tg = Get-Content "$repo/internal/manifest/target.go" -Raw
if ($tg -notmatch 'agent-skills') { Fail 'AC24' 'target.go 找不到 agent-skills' }
$probe = Join-Path $env:TEMP ("apm-verify-" + [guid]::NewGuid().ToString('N'))
New-Item -ItemType Directory -Path $probe -Force | Out-Null
try {
  Push-Location $probe
  & $bin init as-probe --yes --target agent-skills 2>&1 | Out-Null
  if ($LASTEXITCODE -ne 0) { Fail 'AC24' '--target agent-skills 仍被拒絕' } else { Pass 'AC24' }

  # ---- AC1 / AC5 / AC6 / E1：產物形狀 ----
  Pop-Location; Push-Location $probe
  $p2 = Join-Path $probe 'shape'
  New-Item -ItemType Directory -Path $p2 -Force | Out-Null
  Push-Location $p2
  & $bin init shape-probe --yes --target claude,codex,opencode 2>&1 | Out-Null
  $yml = Join-Path $p2 'shape-probe/apm.yml'
  if (-not (Test-Path $yml)) {
    Fail 'AC1' '未產生 apm.yml'
  } else {
    $lines = Get-Content $yml
    $text  = ($lines -join "`n")

    if ($text -notmatch '(?m)^targets:')      { Fail 'AC1' 'apm.yml 未使用複數 targets:' } else { Pass 'AC1' }
    if ($text -match  '(?m)^target:')         { Fail 'AC1' 'apm.yml 仍有單數 target:' }

    # AC5 語意鍵序
    $keys = $lines | Where-Object { $_ -match '^[a-zA-Z]' } | ForEach-Object { ($_ -split ':')[0] }
    $want = @('name','version','description','author','targets','dependencies','includes','scripts')
    $got  = $keys | Where-Object { $want -contains $_ }
    if (($got -join ',') -ne ($want -join ',')) {
      Fail 'AC5' ("鍵序不符。want=[{0}] got=[{1}]" -f ($want -join ','), ($got -join ','))
    } else { Pass 'AC5' }

    # AC6 三行註解 + Accepted values 六個
    $idx = ($lines | Select-String -Pattern '^targets:').LineNumber
    if (-not $idx) {
      Fail 'AC6' '找不到 targets: 行'
    } else {
      $head = $lines[($idx-4)..($idx-2)]
      if (($head -join "`n") -notmatch 'Which agent platforms to deploy to') { Fail 'AC6' 'targets 上方缺註解第 1 行' }
      if (($head -join "`n") -notmatch 'Resolution order')                  { Fail 'AC6' 'targets 上方缺註解第 2 行' }
      $accepted = 'agent-skills, antigravity, claude, codex, copilot, opencode'
      if (($head -join "`n") -notmatch [regex]::Escape($accepted)) {
        Fail 'AC6' "Accepted values 不是預期的六個：$accepted"
      } else { Pass 'AC6' }
    }
  }
  Pop-Location
} finally {
  Pop-Location -ErrorAction SilentlyContinue
  Remove-Item $probe -Recurse -Force -ErrorAction SilentlyContinue
}

# ---- AC2/AC3/AC4/AC7/AC26/AC27/AC29：逐條要求對應測試存在且綠 ----
# 每條先用 -list 證明匹配非空（-run 零匹配也會 exit 0，會假綠），再跑。
$acTests = @(
  @{ ac='AC2/AC3'; p='TestInteractiveTargetSelect_PreselectsExistingTargets'; pkg='./cmd/apm-go/' }
  @{ ac='AC2/AC3'; p='TestReadExistingTargets_BothKeys';                      pkg='./cmd/apm-go/' }
  @{ ac='AC4';     p='TestErrNoDeployTarget_TeachesPluralTargetsKey';          pkg='./cmd/apm-go/' }
  @{ ac='AC7';     p='TestBuildManifestNode_NoTargets_CommentedSkeleton';      pkg='./cmd/apm-go/' }
  @{ ac='AC26';    p='TestTargetsCommentLines_DerivedFromInput';               pkg='./cmd/apm-go/' }
  @{ ac='AC27';    p='TestCanonicalTargets_UnchangedAndCursorStillParses';     pkg='./internal/manifest/' }
  @{ ac='AC29';    p='TestInstall_LegacySingularTargetKey_StillDeploys';       pkg='./cmd/apm-go/' }
)
foreach ($t in $acTests) {
  $m = @(& go test $t.pkg -list $t.p 2>&1 | Where-Object { $_ -match '^Test' })
  if ($m.Count -eq 0) {
    Fail $t.ac "-list 零匹配：找不到測試 $($t.p)（$($t.pkg)）"
    continue
  }
  & go test $t.pkg -run $t.p 2>&1 | Out-Null
  if ($LASTEXITCODE -ne 0) { Fail $t.ac "$($t.p) 紅燈" } else { Pass "$($t.ac)/$($t.p)" }
}

# ---- AC25：三集合同源的鎖定測試存在且非零匹配 ----
# 兩個精確的測試名，各自 -list 證明存在後「必須實跑」。
# codex Tier 2 阻斷 1：原本只 -list 就 Pass，測試紅了也照過。
$ac25 = @(
  @{ pkg='./cmd/apm-go/';        name='TestSupportedTargetsSet_MatchesAdapterTargetsAndPromptMenu' }
  @{ pkg='./internal/manifest/'; name='TestAdapterTargetsSet_MatchesDeployTargets' }
)
$ac25ok = $true
foreach ($t in $ac25) {
  $m = @(& go test $t.pkg -list "^$($t.name)$" 2>&1 | Where-Object { $_ -match '^Test' })
  if ($m.Count -ne 1) { Fail 'AC25' "-list 未精確匹配到 $($t.name)（得到 $($m.Count) 個）"; $ac25ok = $false; continue }
  & go test $t.pkg -run "^$($t.name)$" -count=1 2>&1 | Out-Null
  if ($LASTEXITCODE -ne 0) { Fail 'AC25' "$($t.name) 紅燈"; $ac25ok = $false }
}
if ($ac25ok) { Pass 'AC25（2 個測試實跑）' }

# ---- AC-L9 / parent C5：未新增第三方相依 ----
# git status --porcelain 只給 "M go.mod"，看不出新增什麼，必須看 diff
$d1 = & git diff -- go.mod
$d2 = & git diff --cached -- go.mod
$newReq = @($d1; $d2) | Where-Object { $_ -match '^\+\s+\S+\s+v' }
if ($newReq) { Fail 'AC-L9' ("go.mod 新增了 require：" + ($newReq -join '; ')) } else { Pass 'AC-L9' }

# ---- 覆蓋率 total ----
# 覆蓋率：用唯一檔名，避免讀到上一次殘留的結果；並驗 exit code。
# codex Tier 2 阻斷 1：測試失敗時 go test 仍會寫出 coverprofile，
# 原本只 Test-Path 就採信，導致紅測試也能通過 80% 門檻。
# coverprofile 必須寫在 repo 內：實測寫到 $env:TEMP 時 `go test` 回 exit 0 且印出
# 覆蓋率，檔案卻不存在（沙箱無聲擋掉），於是「檔案不存在」被誤判成測試失敗。
# 用唯一檔名避免讀到殘留，並在用完刪除 —— 舊版把 cover.out 留在 repo 根目錄當地雷。
$cov = "$repo/apmcov-" + [guid]::NewGuid().ToString('N') + ".out"
$before = $script:fails.Count
# 整個參數必須加引號：PowerShell 對 native command 的 `-flag=$var` 在值含反斜線時
# 會拆壞參數，而 go test 仍回 exit 0 並印出覆蓋率 —— 檔案卻不存在。實測對照：
#   go test ... -coverprofile=$cov     → exit 0, 檔案不存在
#   go test ... "-coverprofile=$cov"   → exit 0, 檔案存在
$null = Exec 'AC-L1' 'go test ./... -coverprofile' { go test ./... -count=1 "-coverprofile=$cov" }
if ($script:fails.Count -eq $before) {
  if (-not (Test-Path $cov)) {
    Fail 'AC-L1' 'go test 未產生 coverprofile'
  } else {
    # 同樣要整個參數加引號，理由見上方 -coverprofile 的註解。
    $tot = (& go tool cover "-func=$cov" | Select-Object -Last 1)
    if ($tot -match '(\d+\.\d+)%') {
      if ([double]$Matches[1] -lt 80) { Fail 'AC-L1' "覆蓋率 total $($Matches[1])% < 80%" } else { Pass "AC-L1/coverage $($Matches[1])%" }
    } else { Fail 'AC-L1' '無法解析覆蓋率 total' }
  }
}
Remove-Item $cov -Force -ErrorAction SilentlyContinue

Pop-Location

Write-Host ""
if ($script:fails.Count -gt 0) {
  Write-Host "TIER 1 RED — $($script:fails.Count) 項失敗：" -ForegroundColor Red
  $script:fails | ForEach-Object { Write-Host "  $_" -ForegroundColor Red }
  Write-Host ""
  Write-Host "被封鎖的下游：plugin-init（E1/E2/E3 三條邊）" -ForegroundColor Yellow
  Write-Host "同一條 AC 修 3 次仍紅 → 停止重試，升級給使用者。不得寫成延後。" -ForegroundColor Yellow
  exit 1
}
Write-Host "TIER 1 GREEN — 接著跑 Tier 2（codex 反向稽核）才可宣告完成" -ForegroundColor Green
exit 0
