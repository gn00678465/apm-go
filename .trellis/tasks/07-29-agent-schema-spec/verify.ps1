#requires -Version 7
# Tier 1 確定性閘門 — agent-schema-spec（無入邊、無出邊）
# 對應 AS1–AS7 + AC-L9

$ErrorActionPreference = 'Stop'
$repo = (Resolve-Path "$PSScriptRoot/../../..").Path
Push-Location $repo
$script:fails = @()
function Fail($ac,$m){ $script:fails += "[$ac] $m"; Write-Host "  FAIL [$ac] $m" -ForegroundColor Red }
function Pass($ac){ Write-Host "  ok   [$ac]" -ForegroundColor Green }

Write-Host "== Tier 1: agent-schema-spec ==" -ForegroundColor Cyan

& go build ./... 2>&1 | Out-Null
if ($LASTEXITCODE -ne 0) { Fail 'AS7' 'go build 非 0' } else { Pass 'AS7/build' }
& go vet ./... 2>&1 | Out-Null
if ($LASTEXITCODE -ne 0) { Fail 'AS7' 'go vet 非 0' } else { Pass 'AS7/vet' }

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
  & go test ./... -run 'SchemaGolden|SchemaValidateUpstream' 2>&1 | Out-Null
  if ($LASTEXITCODE -ne 0) { Fail 'AS4' 'golden 產物 schema 驗證失敗' } else { Pass 'AS4' }
}
$listedNeg = @(& go test ./... -list 'SchemaReject|SchemaNegative' 2>&1 | Where-Object { $_ -match '^Test' })
if ($listedNeg.Count -lt 3) { Fail 'AS5' "-list 只匹配 $($listedNeg.Count) 個負向測試，至少需 3（缺 category / 缺 owner / author 給字串）" }
else {
  & go test ./... -run 'SchemaReject|SchemaNegative' 2>&1 | Out-Null
  if ($LASTEXITCODE -ne 0) { Fail 'AS5' '負向測試未通過' } else { Pass 'AS5' }
}

# ---- AS6：防漂移測試存在 ----
# 注意：AS6 的完整驗收（實際加欄位確認轉紅再還原）是 Tier 2 的人工/LLM 步驟，
# 這裡只做確定性的「測試存在且綠燈」；不得以此代替 Tier 2。
$listedDrift = @(& go test ./... -list 'SchemaDrift|SchemaSync' 2>&1 | Where-Object { $_ -match '^Test' })
if ($listedDrift.Count -eq 0) {
  Fail 'AS6' '-list 零匹配：找不到防漂移測試（Go 型別 ↔ schema ↔ spec 三者同步）'
} else {
  & go test ./... -run 'SchemaDrift|SchemaSync' 2>&1 | Out-Null
  if ($LASTEXITCODE -ne 0) { Fail 'AS6' '防漂移測試紅燈' } else { Pass 'AS6/exists' }
  Write-Host "  NOTE [AS6] 「加欄位確認轉紅再還原」屬 Tier 2，本閘門不代替" -ForegroundColor Yellow
}

# ---- AC-L9：未新增第三方相依（本 task 風險最高：JSON Schema validator 可能誘使加相依） ----
$newReq = @((& git diff -- go.mod); (& git diff --cached -- go.mod)) | Where-Object { $_ -match '^\+\s+\S+\s+v' }
if ($newReq) { Fail 'AC-L9' ("go.mod 新增 require：" + ($newReq -join '; ') + " —— 需先取得使用者裁定") } else { Pass 'AC-L9' }

# ---- 覆蓋率 ----
& go test ./... -coverprofile="$repo/cover.out" 2>&1 | Out-Null
if (Test-Path "$repo/cover.out") {
  $t = & go tool cover -func="$repo/cover.out" | Select-Object -Last 1
  if ($t -match '(\d+\.\d+)%') { if ([double]$Matches[1] -lt 80) { Fail 'AS7' "覆蓋率 $($Matches[1])% < 80%" } else { Pass "AS7/coverage $($Matches[1])%" } }
} else { Fail 'AS7' '未產生 cover.out' }

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
