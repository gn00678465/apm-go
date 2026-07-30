#requires -Version 7
# Tier 1 確定性閘門 — marketplace-add-fixes（無入邊、無出邊）

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
        # 2026-07-30 round-4：regex 擴充以涵蓋 go 編譯失敗（`# pkg [pkg.test]` +
        # 未縮排的 `path/file.go:12:3: ...`，見 07-29-install-dev/verify.ps1 同段註解）。
        $detail = ($out -split "`n" | Where-Object { $_ -match '^(FAIL|--- FAIL|\s+\S+_test\.go:|^#|\S+\.go:\d+:|panic:)' } |
                   Select-Object -First 12) -join "`n      "
        if (-not $detail) { $detail = ($out -split "`n" | Select-Object -Last 12) -join "`n      " }
        Fail $ac "$what 失敗（exit $LASTEXITCODE）`n      $detail"
        return $null
    }
    return $out
}

Write-Host "== Tier 1: marketplace-add-fixes ==" -ForegroundColor Cyan

$before = $script:fails.Count
$null = Exec 'AC-L1' 'go build ./...' { go build ./... }
if ($script:fails.Count -eq $before) { Pass 'AC-L1/build' }
$before = $script:fails.Count
$null = Exec 'AC-L1' 'go vet ./...' { go vet ./... }
if ($script:fails.Count -eq $before) { Pass 'AC-L1/vet' }

# ---- 全套件測試：必須先於任何個別 AC 檢查，且必須驗 exit code ----
# 個別 AC 的 -run 檢查只覆蓋本 task 相關套件；其他套件轉紅時它們不會動，
# 這是唯一能擋住「本 task 綠但把別處弄壞了」的閘門。
$before = $script:fails.Count
$null = Exec 'AC-L1' 'go test ./... -count=1（全套件）' { go test ./... -count=1 }
if ($script:fails.Count -eq $before) { Pass 'AC-L1/go-test-all' }

# ---- binary：先刪舊產物，避免 build 失敗時讀到上一次殘留的 binary ----
$bin = Join-Path $repo 'bin/apm-go.exe'
Remove-Item $bin -Force -ErrorAction SilentlyContinue
$before = $script:fails.Count
$null = Exec 'AC-L1' "go build -o $bin" { go build -o $bin ./cmd/apm-go }
if (-not (Test-Path $bin)) { Fail 'AC-L1' "build 後 $bin 不存在" }
elseif ($script:fails.Count -eq $before) { Pass 'AC-L1/binary' }

# ---- AC47 / R10：add 有 --category、set 沒有 ----
$ha = & $bin marketplace package add --help 2>&1 | Out-String
$hs = & $bin marketplace package set --help 2>&1 | Out-String
if ($ha -notmatch '--category') { Fail 'AC47' '`package add --help` 沒有 --category' } else { Pass 'AC47/flag' }
if ($hs -match  '--category')   { Fail 'AC49' '`package set` 不該有 --category（上游旗標集合）' } else { Pass 'AC49' }

# ---- AC22 / R6：audit 錯誤訊息含補救指引 ----
# 2026-07-30：APM_CONFIG_DIR 隔離成獨立臨時目錄，讓這個探針讀到的是一個
# 已知的、空的 marketplaces.json 狀態，而不是耦合到開發機當下
# ~/.apm/marketplaces.json 裡實際登記了哪些 marketplace。
$probe = Join-Path $env:TEMP ("apm-mkt-" + [guid]::NewGuid().ToString('N'))
New-Item -ItemType Directory -Path $probe -Force | Out-Null
$probeConfigDir = Join-Path $env:TEMP ("apm-mkt-cfg-" + [guid]::NewGuid().ToString('N'))
New-Item -ItemType Directory -Path $probeConfigDir -Force | Out-Null
# MINOR 1（外部稽核，2026-07-30）：finally 區塊過去無條件 Remove-Item
# Env:\APM_CONFIG_DIR，若呼叫者本來就設有這個環境變數會被本探針清掉。
# 先存下「呼叫前是否存在」與其舊值，finally 精準還原。
$hadConfigDirEnv = Test-Path Env:\APM_CONFIG_DIR
if ($hadConfigDirEnv) { $prevConfigDirEnv = $env:APM_CONFIG_DIR }
try {
  Push-Location $probe
  $env:APM_CONFIG_DIR = $probeConfigDir
  $out = & $bin marketplace audit no-such-marketplace 2>&1 | Out-String
  if ($out -notmatch 'marketplace add') { Fail 'AC22' 'audit 未註冊錯誤訊息缺少 `marketplace add` 補救指引' }
  elseif ($out -notmatch 'marketplace list') { Fail 'AC22' 'audit 錯誤訊息缺少 `marketplace list` 提示' }
  else { Pass 'AC22' }
} finally {
  if ($hadConfigDirEnv) { $env:APM_CONFIG_DIR = $prevConfigDirEnv } else { Remove-Item Env:\APM_CONFIG_DIR -EA SilentlyContinue }
  Pop-Location -EA SilentlyContinue
  Remove-Item $probe -Recurse -Force -EA SilentlyContinue
  Remove-Item $probeConfigDir -Recurse -Force -EA SilentlyContinue
}

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
  @{ n='ZeroFlag';     p='LocalSource.*ZeroFlag|LocalZeroFlag|RemoteSource_ZeroFlag' },
  # ROUND2-MAJOR1（外部稽核第二輪，2026-07-30）：AC21 宣稱「所有情境」，但先前
  # 6 個分支裡沒有「local + 一般 mutable ref（非空、非 HEAD、非 SHA，例如
  # "main"）」這個組合 -- 這正是報告點名、能存活於前 6 條測試之下的突變。
  @{ n='OrdinaryMutableRef'; p='LocalSource_OrdinaryMutableRef' }
)
$missingLocal = @()
foreach ($b in $localBranches) {
  $m = @(& go test ./internal/marketplace/authoring/ -list $b.p 2>&1 | Where-Object { $_ -match '^Test' })
  if ($m.Count -eq 0) { $missingLocal += $b.n }
}
if ($missingLocal.Count -gt 0) {
  Fail 'AC21' ("local source 的下列情境沒有對應測試（宣稱是『所有情境』，粒度不匹配）：" + ($missingLocal -join '、'))
} else {
  $before = $script:fails.Count
  $null = Exec 'AC21' "go test -run 'LocalSource'" { go test ./internal/marketplace/authoring/ -run 'LocalSource' }
  if ($script:fails.Count -eq $before) { Pass 'AC21' }
}

# ---- REGR-BLOCKING1（外部稽核，2026-07-30）：local source + --ref HEAD
# 不得印出「resolving」訊息（resolveRef 對 local source 從不解析任何東西）----
$listed = @(& go test ./cmd/apm-go/ -list 'TestMarketplacePackageAdd_LocalSource_ExplicitRefHead_NoMutableRefWarning' 2>&1 | Where-Object { $_ -match '^Test' })
if ($listed.Count -eq 0) { Fail 'REGR-B1' 'BLOCKING 1 回歸測試不存在（-list 零匹配）' } else {
  $before = $script:fails.Count
  $null = Exec 'REGR-B1' "go test -run 'TestMarketplacePackageAdd_LocalSource_ExplicitRefHead_NoMutableRefWarning'" { go test ./cmd/apm-go/ -run 'TestMarketplacePackageAdd_LocalSource_ExplicitRefHead_NoMutableRefWarning' }
  if ($script:fails.Count -eq $before) { Pass 'REGR-B1' }
}

# ---- REGR-BLOCKING2（外部稽核，2026-07-30）：`package set --ref` 在 local
# source 上必須仍經 lister 解析，不得被 add-only 的 mkt-046 短路清空 ----
$listed = @(& go test ./internal/marketplace/authoring/ -list 'TestSetPackage_LocalSource_MutableRef_ResolvesToConcreteSHA|TestSetPackage_LocalSource_UnrelatedFieldChange_PreservesVersion|TestResolveRef_LocalSource_SetMode_DoesNotShortCircuit_ResolvesViaLister|TestResolveRef_LocalSource_SetMode_ConcreteSHA_StoredVerbatim_NoListerCall' 2>&1 | Where-Object { $_ -match '^Test' })
if ($listed.Count -lt 4) { Fail 'REGR-B2' "BLOCKING 2 回歸測試 -list 只匹配 $($listed.Count) 個，需要 4 個" } else {
  $before = $script:fails.Count
  $null = Exec 'REGR-B2' "go test -run 'TestSetPackage_LocalSource|TestResolveRef_LocalSource_SetMode'" { go test ./internal/marketplace/authoring/ -run 'TestSetPackage_LocalSource|TestResolveRef_LocalSource_SetMode' }
  if ($script:fails.Count -eq $before) { Pass 'REGR-B2' }
}

# ---- REGR-MAJOR1（外部稽核，2026-07-30）：codex-category 警告的兩個否定
# 測試（category 有給 + outputs 含 codex → 不警告；category 沒給 + outputs
# 不含 codex → 不警告），各自單獨能抓到報告中指出的那個突變 ----
$listed = @(& go test ./cmd/apm-go/ -list 'TestMarketplacePackageAdd_CategoryGiven_OutputsIncludeCodex_NoWarning|TestMarketplacePackageAdd_NoCategory_OutputsExcludeCodex_NoWarning' 2>&1 | Where-Object { $_ -match '^Test' })
if ($listed.Count -lt 2) { Fail 'REGR-M1' "MAJOR 1 回歸測試 -list 只匹配 $($listed.Count) 個，需要 2 個" } else {
  $before = $script:fails.Count
  $null = Exec 'REGR-M1' "go test -run 'TestMarketplacePackageAdd_CategoryGiven_OutputsIncludeCodex_NoWarning|TestMarketplacePackageAdd_NoCategory_OutputsExcludeCodex_NoWarning'" { go test ./cmd/apm-go/ -run 'TestMarketplacePackageAdd_CategoryGiven_OutputsIncludeCodex_NoWarning|TestMarketplacePackageAdd_NoCategory_OutputsExcludeCodex_NoWarning' }
  if ($script:fails.Count -eq $before) { Pass 'REGR-M1' }
}

# ---- REGR-MAJOR2（外部稽核，2026-07-30）：resolveRef 的 SHA/HEAD 邊界情境
# （40 字元非 hex、41 字元、大寫 SHA、混合大小寫 Head、refs/heads/HEAD）----
$listed = @(& go test ./internal/marketplace/authoring/ -list 'TestResolveRef_ShaPattern_40CharNonHex_ResolvesViaLister|TestResolveRef_ShaPattern_41Char_ResolvesViaLister|TestResolveRef_ShaPattern_UppercaseSHA_ResolvesViaLister|TestResolveRef_ExplicitHead_MixedCase_TitleCase|TestResolveRef_RefsHeadsHEAD_NotTreatedAsHeadKeyword' 2>&1 | Where-Object { $_ -match '^Test' })
if ($listed.Count -lt 5) { Fail 'REGR-M2' "MAJOR 2 邊界測試 -list 只匹配 $($listed.Count) 個，需要 5 個" } else {
  $before = $script:fails.Count
  $null = Exec 'REGR-M2' "go test -run 'TestResolveRef_ShaPattern|TestResolveRef_ExplicitHead_MixedCase|TestResolveRef_RefsHeadsHEAD'" { go test ./internal/marketplace/authoring/ -run 'TestResolveRef_ShaPattern|TestResolveRef_ExplicitHead_MixedCase|TestResolveRef_RefsHeadsHEAD' }
  if ($script:fails.Count -eq $before) { Pass 'REGR-M2' }
}

# ════════════════════════════════════════════════════════════════════════
# Round 2（外部稽核第二輪，2026-07-30）：BLOCKING 1/2、MAJOR 1/2/3 的閘門
# ════════════════════════════════════════════════════════════════════════

# ---- ROUND2-BLOCKING1：resolveRef 與 WillResolveMutableRefForAdd 必須共用
# 同一個分類器（classifyRefResolution），不得各自表述 -- 交叉積測試逐一比較
# 「預測會不會解析」與「resolveRef 實際有沒有走到 HEAD 解析分支」----
$listed = @(& go test ./internal/marketplace/authoring/ -list 'TestWillResolveMutableRefForAdd_MatchesResolveRefAcrossCrossProduct' 2>&1 | Where-Object { $_ -match '^Test' })
if ($listed.Count -eq 0) { Fail 'ROUND2-B1' 'BLOCKING 1 交叉積回歸測試不存在（-list 零匹配）' } else {
  $before = $script:fails.Count
  $null = Exec 'ROUND2-B1' "go test -run 'TestWillResolveMutableRefForAdd_MatchesResolveRefAcrossCrossProduct'" { go test ./internal/marketplace/authoring/ -run 'TestWillResolveMutableRefForAdd_MatchesResolveRefAcrossCrossProduct' }
  if ($script:fails.Count -eq $before) { Pass 'ROUND2-B1' }
}

# ---- ROUND2-MAJOR1：local + 一般 mutable ref（如 "main"）的缺口測試（見
# 上方 localBranches 的 OrdinaryMutableRef 項目已涵蓋 -list 存在性檢查，這裡
# 額外確認它實際能跑且通過）----
$listed = @(& go test ./internal/marketplace/authoring/ -list 'TestResolveRef_LocalSource_OrdinaryMutableRef_NeverTouchesNetwork' 2>&1 | Where-Object { $_ -match '^Test' })
if ($listed.Count -eq 0) { Fail 'ROUND2-M1' 'MAJOR 1 回歸測試不存在（-list 零匹配）' } else {
  $before = $script:fails.Count
  $null = Exec 'ROUND2-M1' "go test -run 'TestResolveRef_LocalSource_OrdinaryMutableRef_NeverTouchesNetwork'" { go test ./internal/marketplace/authoring/ -run 'TestResolveRef_LocalSource_OrdinaryMutableRef_NeverTouchesNetwork' }
  if ($script:fails.Count -eq $before) { Pass 'ROUND2-M1' }
}

# ---- ROUND2-MAJOR2：`set --ref` 在「相對路徑」本地 source 上必須透過正式
# 的 production lister（gitRefLister，非 mapRefLister 假件）真的解析成功，
# 不得被 resolveCloneURL 誤展開成 OWNER/REPO shorthand ----
$listed = @(& go test ./internal/marketplace/authoring/ -list 'TestGitRefLister_ListRefs_RelativeLocalSource_ProductionLister|TestSetPackage_RelativeLocalSource_MutableRef_ResolvesViaProductionLister' 2>&1 | Where-Object { $_ -match '^Test' })
if ($listed.Count -lt 2) { Fail 'ROUND2-M2' "MAJOR 2 相對路徑回歸測試 -list 只匹配 $($listed.Count) 個，需要 2 個" } else {
  $before = $script:fails.Count
  $null = Exec 'ROUND2-M2' "go test -run 'TestGitRefLister_ListRefs_RelativeLocalSource_ProductionLister|TestSetPackage_RelativeLocalSource_MutableRef_ResolvesViaProductionLister'" { go test ./internal/marketplace/authoring/ -run 'TestGitRefLister_ListRefs_RelativeLocalSource_ProductionLister|TestSetPackage_RelativeLocalSource_MutableRef_ResolvesViaProductionLister' }
  if ($script:fails.Count -eq $before) { Pass 'ROUND2-M2' }
}

# ---- ROUND2-MAJOR3：四個一行逃逸口 -- 警告嚴重程度（非只 grep 訊息文字）、
# CLI 層的 `set --ref` 覆蓋（非只呼叫 authoring 層）、39 字元合法 hex 邊界 ----
$listed = @(& go test ./cmd/apm-go/ -list 'TestMarketplacePackageSet_RefFlag_ResolvesViaListerThroughCLI' 2>&1 | Where-Object { $_ -match '^Test' })
if ($listed.Count -eq 0) { Fail 'ROUND2-M3-CLI' 'MAJOR 3 的 CLI 層 set --ref 回歸測試不存在（-list 零匹配）' } else {
  $before = $script:fails.Count
  $null = Exec 'ROUND2-M3-CLI' "go test -run 'TestMarketplacePackageSet_RefFlag_ResolvesViaListerThroughCLI'" { go test ./cmd/apm-go/ -run 'TestMarketplacePackageSet_RefFlag_ResolvesViaListerThroughCLI' }
  if ($script:fails.Count -eq $before) { Pass 'ROUND2-M3-CLI' }
}
$listed = @(& go test ./internal/marketplace/authoring/ -list 'TestResolveRef_ShaPattern_39CharValidHex_ResolvesViaLister' 2>&1 | Where-Object { $_ -match '^Test' })
if ($listed.Count -eq 0) { Fail 'ROUND2-M3-39CHAR' 'MAJOR 3 的 39 字元邊界測試不存在（-list 零匹配）' } else {
  $before = $script:fails.Count
  $null = Exec 'ROUND2-M3-39CHAR' "go test -run 'TestResolveRef_ShaPattern_39CharValidHex_ResolvesViaLister'" { go test ./internal/marketplace/authoring/ -run 'TestResolveRef_ShaPattern_39CharValidHex_ResolvesViaLister' }
  if ($script:fails.Count -eq $before) { Pass 'ROUND2-M3-39CHAR' }
}
# 嚴重程度斷言本身就在 TestMarketplacePackageAdd_ExplicitRefHead_PrintsMutableRefWarning
# 與 TestMarketplacePackageAdd_OutputsIncludeCodex_NoCategory_WarnsButSucceeds
# 內部（assertLineSeverity），這兩條已經是 AC19/AC48 既有閘門的一部分，
# 本節只額外用 -list 證明它們仍然存在，避免被誤刪。
$listed = @(& go test ./cmd/apm-go/ -list 'TestMarketplacePackageAdd_ExplicitRefHead_PrintsMutableRefWarning|TestMarketplacePackageAdd_OutputsIncludeCodex_NoCategory_WarnsButSucceeds' 2>&1 | Where-Object { $_ -match '^Test' })
if ($listed.Count -lt 2) { Fail 'ROUND2-M3-SEVERITY' "MAJOR 3 的嚴重程度斷言宿主測試 -list 只匹配 $($listed.Count) 個，需要 2 個" } else { Pass 'ROUND2-M3-SEVERITY' }

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
} finally { Pop-Location -EA SilentlyContinue; Remove-Item $probe3 -Recurse -Force -EA SilentlyContinue }

# ---- AC-L9 ----
# 2026-07-30 round-4：git diff 本身失敗時先前會被無聲吞掉，見
# 07-29-install-dev/verify.ps1 同段註解。
$d1 = & git diff -- go.mod 2>&1; $d1Exit = $LASTEXITCODE
$d2 = & git diff --cached -- go.mod 2>&1; $d2Exit = $LASTEXITCODE
if ($d1Exit -ne 0 -or $d2Exit -ne 0) {
  Fail 'AC-L9' "git diff -- go.mod（exit $d1Exit）或 --cached（exit $d2Exit）本身失敗，無法判定是否新增相依"
} else {
  $newReq = @($d1; $d2) | Where-Object { $_ -match '^\+\s+\S+\s+v' }
  if ($newReq) { Fail 'AC-L9' ("go.mod 新增 require：" + ($newReq -join '; ')) } else { Pass 'AC-L9' }
}

# ---- 覆蓋率：唯一檔名寫在 repo 內、驗 exit code、用完刪除 ----
$cov = "$repo/apmcov-" + [guid]::NewGuid().ToString('N') + ".out"
$before = $script:fails.Count
$null = Exec 'AC-L1' 'go test ./... -coverprofile' { go test ./... -count=1 "-coverprofile=$cov" }
if ($script:fails.Count -eq $before) {
  if (-not (Test-Path $cov)) {
    Fail 'AC-L1' 'go test 未產生 coverprofile'
  } else {
    $t = (& go tool cover "-func=$cov" | Select-Object -Last 1)
    if ($t -match '(\d+\.\d+)%') { if ([double]$Matches[1] -lt 80) { Fail 'AC-L1' "覆蓋率 $($Matches[1])% < 80%" } else { Pass "AC-L1/coverage $($Matches[1])%" } }
    else { Fail 'AC-L1' '無法解析覆蓋率' }
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
