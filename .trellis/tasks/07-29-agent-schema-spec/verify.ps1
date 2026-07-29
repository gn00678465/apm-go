#requires -Version 7
# Tier 1 確定性閘門 — agent-schema-spec（無入邊、無出邊）
# 對應 AS1–AS7 + AC-L9

$ErrorActionPreference = 'Stop'
$repo = (Resolve-Path "$PSScriptRoot/../../..").Path
Push-Location $repo
$script:fails = @()
function Fail($ac,$m){ $script:fails += "[$ac] $m"; Write-Host "  FAIL [$ac] $m" -ForegroundColor Red }
function Pass($ac){ Write-Host "  ok   [$ac]" -ForegroundColor Green }

# Exec 執行一個 native command 並「立刻」驗 exit code。
#
# 2026-07-29 codex Tier 2 阻斷 1（原見於 targets-init-shape，同類問題在本檔複現）：
# 本腳本原本的覆蓋率步驟只看 coverprofile 檔案存不存在，不看 `go test ./...` 的
# exit code。實測反證：對 internal/lockfile/archivebytes_test.go:15 插入
# `t.Errorf("MUTATION")` 後，`go test ./...` exit 1，但 coverprofile 照樣寫出
# total 86.9%，於是 `AS7/coverage` 那一行印成 `ok`——閘門犯了它自己要防的錯，
# 且本檔案原本完全沒有「全套件測試」這一道獨立於 AS1–AS6 的把關。
# 所有 native 呼叫一律走這裡。
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

Write-Host "== Tier 1: agent-schema-spec ==" -ForegroundColor Cyan

$before = $script:fails.Count
$null = Exec 'AS7' 'go build ./...' { go build ./... }
if ($script:fails.Count -eq $before) { Pass 'AS7/build' }
$before = $script:fails.Count
$null = Exec 'AS7' 'go vet ./...' { go vet ./... }
if ($script:fails.Count -eq $before) { Pass 'AS7/vet' }

# ---- 全套件測試：必須先於任何個別 AC 檢查，且必須驗 exit code ----
# AS1–AS6 的 -run 檢查只鎖定 schema 相關的測試名稱 pattern；其他套件（例如
# internal/lockfile）被改壞時，那些檢查完全不會動——這是本檔在 mutation 測試中
# 被抓到的缺口。這一條是唯一能擋住「本 task 綠但把別處弄壞了」的閘門。
$before = $script:fails.Count
$null = Exec 'AS7' 'go test ./... -count=1（全套件）' { go test ./... -count=1 }
if ($script:fails.Count -eq $before) { Pass 'AS7/go-test-all' }

# ---- AS1：spec 文件存在且涵蓋三個產物家族 ----
$specs = @(Get-ChildItem "$repo/.trellis/spec/conformance" -Filter '*.md' -EA SilentlyContinue |
           Where-Object { (Get-Content $_.FullName -Raw) -match 'marketplace\.json' -and (Get-Content $_.FullName -Raw) -match 'plugin\.json' })
if ($specs.Count -eq 0) {
  Fail 'AS1' '.trellis/spec/conformance/ 下找不到涵蓋 marketplace.json 與 plugin.json 的 spec 文件'
} else {
  $spec = Get-Content $specs[0].FullName -Raw
  foreach ($fam in @('\.claude-plugin/marketplace\.json', '\.agents/plugins/marketplace\.json', '\.github/plugin/plugin\.json')) {
    if ($spec -notmatch $fam) { Fail 'AS1' "spec 缺產物家族：$fam" }
  }
  if ($script:fails.Count -eq 0) { Pass 'AS1' }

  # ---- AS2：記錄 codex source.url shorthand 上游瑕疵 ----
  if ($spec -notmatch 'shorthand') { Fail 'AS2' 'spec 未記錄 codex source.url 的 owner/repo shorthand 瑕疵' }
  elseif ($spec -notmatch 'codexmapper\.go:\d+') { Fail 'AS2' 'spec 未引用 codexmapper.go 的實際行號' }
  else { Pass 'AS2' }

  # ---- AS3：記錄三軸支援度 ----
  $axes = @('部署','marketplace 輸出','plugin.json 生態')
  $missing = $axes | Where-Object { $spec -notmatch [regex]::Escape($_) }
  if ($missing) { Fail 'AS3' ("spec 缺三軸說明：" + ($missing -join '、')) } else { Pass 'AS3' }
}

# ---- AS4 / AS5：可執行 schema —— 正向 golden 通過、負向變體失敗 ----
$listedPos = @(& go test ./... -list 'SchemaGolden|SchemaValidateUpstream' 2>&1 | Where-Object { $_ -match '^Test' })
if ($listedPos.Count -eq 0) { Fail 'AS4' '-list 零匹配：找不到「上游 golden 產物驗證通過」的測試' }
else {
  $before = $script:fails.Count
  $null = Exec 'AS4' "go test -run 'SchemaGolden|SchemaValidateUpstream'" { go test ./... -run 'SchemaGolden|SchemaValidateUpstream' }
  if ($script:fails.Count -eq $before) { Pass 'AS4' }
}
$listedNeg = @(& go test ./... -list 'SchemaReject|SchemaNegative' 2>&1 | Where-Object { $_ -match '^Test' })
if ($listedNeg.Count -lt 3) { Fail 'AS5' "-list 只匹配 $($listedNeg.Count) 個負向測試，至少需 3（缺 category / 缺 owner / author 給字串）" }
else {
  $before = $script:fails.Count
  $null = Exec 'AS5' "go test -run 'SchemaReject|SchemaNegative'" { go test ./... -run 'SchemaReject|SchemaNegative' }
  if ($script:fails.Count -eq $before) { Pass 'AS5' }
}

# ---- AS6：防漂移測試存在 ----
# 注意：AS6 的完整驗收（實際加欄位確認轉紅再還原）是 Tier 2 的人工/LLM 步驟，
# 這裡只做確定性的「測試存在且綠燈」；不得以此代替 Tier 2。
$listedDrift = @(& go test ./... -list 'SchemaDrift|SchemaSync' 2>&1 | Where-Object { $_ -match '^Test' })
if ($listedDrift.Count -eq 0) {
  Fail 'AS6' '-list 零匹配：找不到防漂移測試（Go 型別 ↔ schema ↔ spec 三者同步）'
} else {
  $before = $script:fails.Count
  $null = Exec 'AS6' "go test -run 'SchemaDrift|SchemaSync'" { go test ./... -run 'SchemaDrift|SchemaSync' }
  if ($script:fails.Count -eq $before) { Pass 'AS6/exists' }
  Write-Host "  NOTE [AS6] 「加欄位確認轉紅再還原」屬 Tier 2，本閘門不代替" -ForegroundColor Yellow
}

# ---- AC-L9：未新增第三方相依（本 task 風險最高：JSON Schema validator 可能誘使加相依） ----
$newReq = @((& git diff -- go.mod); (& git diff --cached -- go.mod)) | Where-Object { $_ -match '^\+\s+\S+\s+v' }
if ($newReq) { Fail 'AC-L9' ("go.mod 新增 require：" + ($newReq -join '; ') + " —— 需先取得使用者裁定") } else { Pass 'AC-L9' }

# ---- 覆蓋率：唯一檔名寫在 repo 內、驗 exit code、用完刪除 ----
$cov = "$repo/apmcov-" + [guid]::NewGuid().ToString('N') + ".out"
$before = $script:fails.Count
$null = Exec 'AS7' 'go test ./... -coverprofile' { go test ./... -count=1 "-coverprofile=$cov" }
if ($script:fails.Count -eq $before) {
  if (-not (Test-Path $cov)) {
    Fail 'AS7' 'go test 未產生 coverprofile'
  } else {
    $t = (& go tool cover "-func=$cov" | Select-Object -Last 1)
    if ($t -match '(\d+\.\d+)%') { if ([double]$Matches[1] -lt 80) { Fail 'AS7' "覆蓋率 $($Matches[1])% < 80%" } else { Pass "AS7/coverage $($Matches[1])%" } }
    else { Fail 'AS7' '無法解析覆蓋率' }
  }
}
Remove-Item $cov -Force -ErrorAction SilentlyContinue

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
