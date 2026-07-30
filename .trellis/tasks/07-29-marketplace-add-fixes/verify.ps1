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

# ExecTestJSON（MAJOR 2，外部稽核第四輪，2026-07-30）：`go test -run <pattern>`
# 只驗 exit code 擋不住 t.Skip -- 把測試本體改成 t.Skip(...) 之後，Go 對
# 「每個測試都跳過、沒有任何 FAIL」仍回傳 exit 0，Exec 因此把它當通過。
# 改用 `go test -json`：對 pattern 匹配到的每一個測試名稱（含子測試），
# 要求它「有出現 Action=pass」且「從未出現 Action=skip」。任何一個測試
# 一次都沒 pass、或曾經 skip，都判定失敗 -- 除非它的名稱匹配 $allowSkip
# （例如 BLOCKING 2 的 symlink 測試本身設計為在無法建立 symlink 的環境
# "可見地" t.Skip，這是環境限制、不是本閘門要抓的突變，見該測試自身註解）；
# 對這種例外，skip 只記錄一行提示、不算失敗，但仍要求它至少「跑過」
# （出現在 $seen 裡），不能是 -list 零匹配那種「根本沒跑到」。
#
# BLOCKING 2（外部稽核第五輪，2026-07-30）：這個函式從來沒有讀過
# $LASTEXITCODE，也從沒檢查過「沒有 Test 欄位」的套件層級事件 -- 舊迴圈的
# `if (-not $ev.Test) { continue }` 直接跳過它們，包括 Action=fail 的套件層
# 級事件（例如 TestMain 在 m.Run() 之後回傳非 0、或所有測試跑完後才 panic，
# 這兩種都不會產生任何 per-test 的 fail 事件，只會在套件層級留下一個
# fail）。序列「TestX pass -> 套件 fail」因此會一路綠燈到底。修法：
# (1) 呼叫後立刻存 $LASTEXITCODE；(2) 沒有 Test 欄位但 Action=fail 的事件
# 記為 $packageFailed；(3) 看起來像 JSON（以 "{" 開頭）卻解析失敗的行記為
# $malformed，不再靜默 continue；(4) 每一個測試都 pass、也沒有套件層級
# fail、也沒有解析失敗行之後，最後仍檢查一次 exit code，非 0 就不算過。
#
# BLOCKING 3（外部稽核第五輪，2026-07-30）：`-list` 只列頂層測試函式名稱，
# 對 table-driven 測試（每個案例用 `t.Run(name, ...)` 包成子測試）刪掉幾個
# case 之後，`-list` 回報的數量完全不會變（還是同一個頂層函式名稱）--
# 這裡新增 `-minCount` 參數，直接對 `go test -json` 逐測試事件觀察到的
# **相異測試/子測試名稱數**設下限；刪掉子測試會讓這個數字低於 $minCount
# 而轉紅，不再只看「有沒有東西通過」。
function ExecTestJSON {
    param([string]$ac, [string]$what, [string]$pkg, [string]$pattern, [string[]]$allowSkip = @(), [int]$minCount = 0)
    $out = & go test -json -run $pattern $pkg 2>&1
    $exitCode = $LASTEXITCODE
    # 找到過程中的 bug（外部稽核第五輪修復自查，2026-07-30）：PowerShell 的
    # `@{}` 對字串鍵預設「不分大小寫」比對 -- `$h=@{}; $h["C:foo"]="x";
    # $h["c:foo"]="y"; $h.Count` 回傳 1，不是 2。Go 的測試名稱是大小寫敏感
    # 的（round 5 新增的 `C:foo`/`c:foo` 兩個獨立測試案例就撞在一起，曾讓
    # -minCount 的計數少算 1，本檔自查時發現）。改用
    # System.Collections.Generic.Dictionary[string,string]，其字串鍵預設用
    # Ordinal（大小寫敏感）比較，兩個只差大小寫的測試名稱不會被誤判成同一個
    # 鍵而互相覆蓋，也不會讓 -minCount 因此少算。
    $seen = [System.Collections.Generic.Dictionary[string,string]]::new()
    $skipped = @()
    $allowedSkipped = @()
    $packageFailed = $false
    $malformed = @()
    foreach ($line in ($out -split "`n")) {
        if ($line -eq '') { continue }
        if ($line -notmatch '^\{') { $malformed += $line; continue }
        try { $ev = $line | ConvertFrom-Json -ErrorAction Stop } catch { $malformed += $line; continue }
        if (-not $ev.Test) {
            if ($ev.Action -eq 'fail') { $packageFailed = $true }
            continue
        }
        switch ($ev.Action) {
            'pass' { $seen[$ev.Test] = 'pass' }
            'skip' {
                if ($seen[$ev.Test] -ne 'pass') { $seen[$ev.Test] = 'skip' }
                $isAllowed = $false
                foreach ($p in $allowSkip) { if ($ev.Test -match $p) { $isAllowed = $true } }
                if ($isAllowed) { $allowedSkipped += $ev.Test } else { $skipped += $ev.Test }
            }
            'fail' { if ($seen[$ev.Test] -ne 'pass') { $seen[$ev.Test] = 'fail' } }
        }
    }
    if ($seen.Count -eq 0) {
        Fail $ac "${what}: -json 沒有產生任何逐測試事件（pattern 可能零匹配）"
        return
    }
    if ($malformed.Count -gt 0) {
        Fail $ac "${what}: 輸出含看起來像 JSON 卻無法解析的行（可能截斷或摻雜非 JSON 內容，BLOCKING 2 外部稽核第五輪），前幾行: $((($malformed | Select-Object -First 5)) -join ' | ')"
        return
    }
    $totalSeen = $seen.Count
    if ($minCount -gt 0 -and $totalSeen -lt $minCount) {
        Fail $ac "${what}: 只觀察到 $totalSeen 個測試/子測試事件，至少需要 $minCount 個（子測試可能被刪除，BLOCKING 3 外部稽核第五輪）"
        return
    }
    if ($packageFailed) {
        Fail $ac "${what}: 套件層級回報 Action=fail（例如 TestMain 在 m.Run() 之後回傳非 0，或測試後發生 panic）-- 即使每個個別測試都 pass 也算失敗（BLOCKING 2 外部稽核第五輪）"
        return
    }
    if ($skipped.Count -gt 0) {
        Fail $ac "${what}: 下列測試回報 Action=skip（t.Skip 不算通過）: $($skipped -join ', ')"
        return
    }
    if ($allowedSkipped.Count -gt 0) {
        Write-Host "  note [$ac] 環境限制導致以下測試可見地 skip（非本閘門失敗）: $($allowedSkipped -join ', ')" -ForegroundColor Yellow
        # 從 $seen 移除，讓下面的「必須 pass」檢查不對它們誤判失敗。
        foreach ($t in $allowedSkipped) { if ($seen[$t] -eq 'skip') { $seen.Remove($t) } }
    }
    $notPassed = @($seen.GetEnumerator() | Where-Object { $_.Value -ne 'pass' })
    if ($notPassed.Count -gt 0) {
        Fail $ac "${what}: 下列測試從未回報 Action=pass: $((($notPassed | ForEach-Object { $_.Key })) -join ', ')"
        return
    }
    if ($exitCode -ne 0) {
        Fail $ac "${what}: 每個測試都回報 pass、也沒有套件層級 fail 事件，但 go test 本身以非 0 結束（exit $exitCode，BLOCKING 2 外部稽核第五輪）"
        return
    }
    Pass $ac
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
  # BLOCKING 4（外部稽核第五輪，2026-07-30）：改用 ExecTestJSON，不再只驗
  # exit code -- t.Skip 逃逸口見該函式自身文件註解。
  ExecTestJSON 'AC21' "go test -json -run 'LocalSource'" './internal/marketplace/authoring/' 'LocalSource'
}

# ---- REGR-BLOCKING1（外部稽核，2026-07-30）：local source + --ref HEAD
# 不得印出「resolving」訊息（resolveRef 對 local source 從不解析任何東西）----
$listed = @(& go test ./cmd/apm-go/ -list 'TestMarketplacePackageAdd_LocalSource_ExplicitRefHead_NoMutableRefWarning' 2>&1 | Where-Object { $_ -match '^Test' })
if ($listed.Count -eq 0) { Fail 'REGR-B1' 'BLOCKING 1 回歸測試不存在（-list 零匹配）' } else {
  # BLOCKING 4（外部稽核第五輪，2026-07-30）：改用 ExecTestJSON。
  ExecTestJSON 'REGR-B1' "go test -json -run 'TestMarketplacePackageAdd_LocalSource_ExplicitRefHead_NoMutableRefWarning'" './cmd/apm-go/' 'TestMarketplacePackageAdd_LocalSource_ExplicitRefHead_NoMutableRefWarning'
}

# ---- REGR-BLOCKING2（外部稽核，2026-07-30）：`package set --ref` 在 local
# source 上必須仍經 lister 解析，不得被 add-only 的 mkt-046 短路清空 ----
$listed = @(& go test ./internal/marketplace/authoring/ -list 'TestSetPackage_LocalSource_MutableRef_ResolvesToConcreteSHA|TestSetPackage_LocalSource_UnrelatedFieldChange_PreservesVersion|TestResolveRef_LocalSource_SetMode_DoesNotShortCircuit_ResolvesViaLister|TestResolveRef_LocalSource_SetMode_ConcreteSHA_StoredVerbatim_NoListerCall' 2>&1 | Where-Object { $_ -match '^Test' })
if ($listed.Count -lt 4) { Fail 'REGR-B2' "BLOCKING 2 回歸測試 -list 只匹配 $($listed.Count) 個，需要 4 個" } else {
  # BLOCKING 4（外部稽核第五輪，2026-07-30）：改用 ExecTestJSON。
  ExecTestJSON 'REGR-B2' "go test -json -run 'TestSetPackage_LocalSource|TestResolveRef_LocalSource_SetMode'" './internal/marketplace/authoring/' 'TestSetPackage_LocalSource|TestResolveRef_LocalSource_SetMode'
}

# ---- REGR-MAJOR1（外部稽核，2026-07-30）：codex-category 警告的兩個否定
# 測試（category 有給 + outputs 含 codex → 不警告；category 沒給 + outputs
# 不含 codex → 不警告），各自單獨能抓到報告中指出的那個突變 ----
$listed = @(& go test ./cmd/apm-go/ -list 'TestMarketplacePackageAdd_CategoryGiven_OutputsIncludeCodex_NoWarning|TestMarketplacePackageAdd_NoCategory_OutputsExcludeCodex_NoWarning' 2>&1 | Where-Object { $_ -match '^Test' })
if ($listed.Count -lt 2) { Fail 'REGR-M1' "MAJOR 1 回歸測試 -list 只匹配 $($listed.Count) 個，需要 2 個" } else {
  # BLOCKING 4（外部稽核第五輪，2026-07-30）：改用 ExecTestJSON。
  ExecTestJSON 'REGR-M1' "go test -json -run 'TestMarketplacePackageAdd_CategoryGiven_OutputsIncludeCodex_NoWarning|TestMarketplacePackageAdd_NoCategory_OutputsExcludeCodex_NoWarning'" './cmd/apm-go/' 'TestMarketplacePackageAdd_CategoryGiven_OutputsIncludeCodex_NoWarning|TestMarketplacePackageAdd_NoCategory_OutputsExcludeCodex_NoWarning'
}

# ---- REGR-MAJOR2（外部稽核，2026-07-30）：resolveRef 的 SHA/HEAD 邊界情境
# （40 字元非 hex、41 字元、大寫 SHA、混合大小寫 Head、refs/heads/HEAD）----
$listed = @(& go test ./internal/marketplace/authoring/ -list 'TestResolveRef_ShaPattern_40CharNonHex_ResolvesViaLister|TestResolveRef_ShaPattern_41Char_ResolvesViaLister|TestResolveRef_ShaPattern_UppercaseSHA_ResolvesViaLister|TestResolveRef_ExplicitHead_MixedCase_TitleCase|TestResolveRef_RefsHeadsHEAD_NotTreatedAsHeadKeyword' 2>&1 | Where-Object { $_ -match '^Test' })
if ($listed.Count -lt 5) { Fail 'REGR-M2' "MAJOR 2 邊界測試 -list 只匹配 $($listed.Count) 個，需要 5 個" } else {
  # BLOCKING 4（外部稽核第五輪，2026-07-30）：改用 ExecTestJSON。
  ExecTestJSON 'REGR-M2' "go test -json -run 'TestResolveRef_ShaPattern|TestResolveRef_ExplicitHead_MixedCase|TestResolveRef_RefsHeadsHEAD'" './internal/marketplace/authoring/' 'TestResolveRef_ShaPattern|TestResolveRef_ExplicitHead_MixedCase|TestResolveRef_RefsHeadsHEAD'
}

# ════════════════════════════════════════════════════════════════════════
# Round 2（外部稽核第二輪，2026-07-30）：BLOCKING 1/2、MAJOR 1/2/3 的閘門
# ════════════════════════════════════════════════════════════════════════

# ---- ROUND2-BLOCKING1：resolveRef 與 WillResolveMutableRefForAdd 必須共用
# 同一個分類器（classifyRefResolution），不得各自表述 -- 交叉積測試逐一比較
# 「預測會不會解析」與「resolveRef 實際有沒有走到 HEAD 解析分支」----
$listed = @(& go test ./internal/marketplace/authoring/ -list 'TestWillResolveMutableRefForAdd_MatchesResolveRefAcrossCrossProduct' 2>&1 | Where-Object { $_ -match '^Test' })
if ($listed.Count -eq 0) { Fail 'ROUND2-B1' 'BLOCKING 1 交叉積回歸測試不存在（-list 零匹配）' } else {
  # BLOCKING 4（外部稽核第五輪，2026-07-30）：改用 ExecTestJSON。
  ExecTestJSON 'ROUND2-B1' "go test -json -run 'TestWillResolveMutableRefForAdd_MatchesResolveRefAcrossCrossProduct'" './internal/marketplace/authoring/' 'TestWillResolveMutableRefForAdd_MatchesResolveRefAcrossCrossProduct'
}

# ---- ROUND2-MAJOR1：local + 一般 mutable ref（如 "main"）的缺口測試（見
# 上方 localBranches 的 OrdinaryMutableRef 項目已涵蓋 -list 存在性檢查，這裡
# 額外確認它實際能跑且通過）----
$listed = @(& go test ./internal/marketplace/authoring/ -list 'TestResolveRef_LocalSource_OrdinaryMutableRef_NeverTouchesNetwork' 2>&1 | Where-Object { $_ -match '^Test' })
if ($listed.Count -eq 0) { Fail 'ROUND2-M1' 'MAJOR 1 回歸測試不存在（-list 零匹配）' } else {
  # BLOCKING 4（外部稽核第五輪，2026-07-30）：改用 ExecTestJSON。
  ExecTestJSON 'ROUND2-M1' "go test -json -run 'TestResolveRef_LocalSource_OrdinaryMutableRef_NeverTouchesNetwork'" './internal/marketplace/authoring/' 'TestResolveRef_LocalSource_OrdinaryMutableRef_NeverTouchesNetwork'
}

# ---- ROUND2-MAJOR2：`set --ref` 在「相對路徑」本地 source 上必須透過正式
# 的 production lister（gitRefLister，非 mapRefLister 假件）真的解析成功，
# 不得被 resolveCloneURL 誤展開成 OWNER/REPO shorthand ----
$listed = @(& go test ./internal/marketplace/authoring/ -list 'TestGitRefLister_ListRefs_RelativeLocalSource_ProductionLister|TestSetPackage_RelativeLocalSource_MutableRef_ResolvesViaProductionLister' 2>&1 | Where-Object { $_ -match '^Test' })
if ($listed.Count -lt 2) { Fail 'ROUND2-M2' "MAJOR 2 相對路徑回歸測試 -list 只匹配 $($listed.Count) 個，需要 2 個" } else {
  # BLOCKING 4（外部稽核第五輪，2026-07-30）：改用 ExecTestJSON。
  ExecTestJSON 'ROUND2-M2' "go test -json -run 'TestGitRefLister_ListRefs_RelativeLocalSource_ProductionLister|TestSetPackage_RelativeLocalSource_MutableRef_ResolvesViaProductionLister'" './internal/marketplace/authoring/' 'TestGitRefLister_ListRefs_RelativeLocalSource_ProductionLister|TestSetPackage_RelativeLocalSource_MutableRef_ResolvesViaProductionLister'
}

# ---- ROUND2-MAJOR3：四個一行逃逸口 -- 警告嚴重程度（非只 grep 訊息文字）、
# CLI 層的 `set --ref` 覆蓋（非只呼叫 authoring 層）、39 字元合法 hex 邊界 ----
$listed = @(& go test ./cmd/apm-go/ -list 'TestMarketplacePackageSet_RefFlag_ResolvesViaListerThroughCLI|TestMarketplacePackageSet_RefFlag_BranchName_ResolvesViaListerThroughCLI' 2>&1 | Where-Object { $_ -match '^Test' })
if ($listed.Count -lt 2) { Fail 'ROUND2-M3-CLI' "MAJOR 3 的 CLI 層 set --ref 回歸測試 -list 只匹配 $($listed.Count) 個，需要 2 個（含 round-3 補的分支名 fixture，見 ROUND3-MAJOR-BRANCHFIXTURE 註記）" } else {
  # BLOCKING 4（外部稽核第五輪，2026-07-30）：改用 ExecTestJSON。
  ExecTestJSON 'ROUND2-M3-CLI' "go test -json -run 'TestMarketplacePackageSet_RefFlag_ResolvesViaListerThroughCLI|TestMarketplacePackageSet_RefFlag_BranchName_ResolvesViaListerThroughCLI'" './cmd/apm-go/' 'TestMarketplacePackageSet_RefFlag_ResolvesViaListerThroughCLI|TestMarketplacePackageSet_RefFlag_BranchName_ResolvesViaListerThroughCLI'
}
$listed = @(& go test ./internal/marketplace/authoring/ -list 'TestResolveRef_ShaPattern_39CharValidHex_ResolvesViaLister' 2>&1 | Where-Object { $_ -match '^Test' })
if ($listed.Count -eq 0) { Fail 'ROUND2-M3-39CHAR' 'MAJOR 3 的 39 字元邊界測試不存在（-list 零匹配）' } else {
  # BLOCKING 4（外部稽核第五輪，2026-07-30）：改用 ExecTestJSON。
  ExecTestJSON 'ROUND2-M3-39CHAR' "go test -json -run 'TestResolveRef_ShaPattern_39CharValidHex_ResolvesViaLister'" './internal/marketplace/authoring/' 'TestResolveRef_ShaPattern_39CharValidHex_ResolvesViaLister'
}
# ROUND3-MAJOR-SEVERITY（外部稽核第三輪，2026-07-30）：PRD 明講 t.Skip 不算
# 通過，而先前這個閘門只 -list、從不 -run -- 一個把測試體改成 t.Skip() 的突變
# 會讓 -list 依舊非零、卻永遠不會被本閘門的 exit code 檢查逮到。
# MAJOR 2（外部稽核第四輪，2026-07-30）：光是「改成真的 -run」仍不夠 -- `go
# test -run <pattern>` 對「pattern 匹配到的測試全部 t.Skip、沒有任何 FAIL」
# 一樣回傳 exit 0，Exec 只驗 exit code 一樣會誤判過關。改用 ExecTestJSON：
# 直接讀 `go test -json` 逐測試的 Action 欄位，要求每個匹配到的測試都出現過
# Action=pass、且從未出現 Action=skip。
$listed = @(& go test ./cmd/apm-go/ -list 'TestMarketplacePackageAdd_ExplicitRefHead_PrintsMutableRefWarning|TestMarketplacePackageAdd_OutputsIncludeCodex_NoCategory_WarnsButSucceeds' 2>&1 | Where-Object { $_ -match '^Test' })
if ($listed.Count -lt 2) { Fail 'ROUND2-M3-SEVERITY' "MAJOR 3 的嚴重程度斷言宿主測試 -list 只匹配 $($listed.Count) 個，需要 2 個" } else {
  ExecTestJSON 'ROUND2-M3-SEVERITY' "go test -json -run '...'" './cmd/apm-go/' 'TestMarketplacePackageAdd_ExplicitRefHead_PrintsMutableRefWarning|TestMarketplacePackageAdd_OutputsIncludeCodex_NoCategory_WarnsButSucceeds'
}

# ════════════════════════════════════════════════════════════════════════
# Round 3（外部稽核第三輪，2026-07-30）：BLOCKING 1（Windows 路徑逃逸的兩層
# 防禦）、BLOCKING 2（noVerify 進分類器 + CLI 警告時序）、MAJOR（Head 混合
# 大小寫、分支名 fixture、嚴重程度閘門真的 -run）的閘門
# ════════════════════════════════════════════════════════════════════════

# ---- ROUND3-BLOCKING1-MANIFEST：ValidateMarketplaceSource 必須同時擋
# "/" 與 "\" 形式的 '..' 逃逸（Windows 相對路徑遍歷），round-4 起也含絕對/UNC
# 路徑案例（BLOCKING 1，外部稽核第四輪）。改用 ExecTestJSON（MAJOR 2，第四
# 輪）：table-driven 的每個 t.Run 子測試都個別要求 Action=pass、不得 skip。
#
# BLOCKING 3（外部稽核第五輪，2026-07-30）：`-list` 只列頂層測試函式名稱
# （這裡永遠是 1，不論表格裡刪掉幾個 t.Run 子測試案例都不會變），所以上面
# 那個「$listed.Count -eq 0」存在性檢查完全擋不住「刪掉 mcp_test.go 裡的
# 幾個 case」這種突變（main session 實測：刪掉 round 4 新增的 5 個絕對/UNC
# 案例後，TestValidateMarketplaceSource 剩下的 25 個案例仍全數通過，舊式
# 「-list 存在性 + Exec 只驗 exit code」閘門依舊回報綠燈——已用
# `git stash`/`git stash pop` 實際刪除又還原這 5 個案例驗證過）。改用
# -minCount：round 5 的 TestValidateMarketplaceSource 目前 ExecTestJSON
# 觀察到的相異測試/子測試事件數是 31（30 個子測試 + 1 個頂層 TestXxx 自己
# 的事件），用
# `go test -json -run TestValidateMarketplaceSource ./internal/manifest/ |
#  grep -oE '"Test":"[^"]+"' | sort -u | wc -l` 實測確認為 31；刪掉任何一個
# 案例都會讓這個數字低於 31 而轉紅（已用同樣的刪除-還原方式對 ExecTestJSON
# 本身重現過：25 個子測試 + 1 個頂層事件 = 26 < 31，正確轉紅）。
$listed = @(& go test ./internal/manifest/ -list 'TestValidateMarketplaceSource' 2>&1 | Where-Object { $_ -match '^Test' })
if ($listed.Count -eq 0) { Fail 'ROUND3-B1-MANIFEST' 'BLOCKING 1 manifest 層回歸測試不存在（-list 零匹配）' } else {
  ExecTestJSON 'ROUND3-B1-MANIFEST' "go test -json -run 'TestValidateMarketplaceSource'" './internal/manifest/' 'TestValidateMarketplaceSource' -minCount 31
}

# ---- ROUND3-BLOCKING1-RESOLVECLONEURL：resolveCloneURL 自己的第二層邊界
# 檢查（即使 manifest 層被繞過，解析出的絕對路徑逃出 cwd 也必須被拒絕；
# round-4 起也含 symlink 逃逸案例，BLOCKING 2）。同樣改用 ExecTestJSON。
#
# BLOCKING 3（外部稽核第五輪，2026-07-30）：同上，TestResolveCloneURL 也是
# table-driven（部分案例用 for 迴圈裡的 t.Run，部分是獨立 t.Run 區塊），
# `-list` 一樣測不出刪掉子測試 -- main session 實測：刪掉 round 4 新增的
# symlink 子測試後，其餘 8 個子測試仍全綠，舊式「-list 存在性 + Exec 只驗
# exit code」閘門依舊回報通過（exit 0）。加上 -minCount：ExecTestJSON 觀察
# 到的相異測試/子測試事件數（含本輪 MAJOR 1 新增的 dangling-leaf 案例）目前
# 是 11（10 個子測試 + 1 個頂層 TestXxx 自己的事件），用
# `go test -json -run TestResolveCloneURL ./internal/marketplace/authoring/ |
#  grep -oE '"Test":"[^"]+"' | sort -u | wc -l` 實測確認為 11；main session
# 對「刪掉 symlink 子測試」這個突變用同樣方式重現過：降到 10（9 個子測試 +
# 1 個頂層事件）< 11，正確轉紅。
$listed = @(& go test ./internal/marketplace/authoring/ -list 'TestResolveCloneURL' 2>&1 | Where-Object { $_ -match '^Test' })
if ($listed.Count -eq 0) { Fail 'ROUND3-B1-RESOLVECLONEURL' 'BLOCKING 1 resolveCloneURL 回歸測試不存在（-list 零匹配）' } else {
  ExecTestJSON 'ROUND3-B1-RESOLVECLONEURL' "go test -json -run 'TestResolveCloneURL'" './internal/marketplace/authoring/' 'TestResolveCloneURL' -allowSkip @('directory_symlink', 'dangling_leaf') -minCount 11
}

# ---- ROUND3-BLOCKING2：mutable-ref 警告必須綁在 resolveRef 實際要解析
# HEAD 的那一刻（onExplicitHeadWillResolve hook），不得在 AddPackage 任何
# 前置檢查失敗前就先印 -- 涵蓋 resolveRef 單元層與 CLI 端對端兩層 ----
$listed = @(& go test ./internal/marketplace/authoring/ -list 'TestResolveRef_ExplicitHead_InvokesOnExplicitHeadWillResolve|TestResolveRef_ImplicitHead_DoesNotInvokeOnExplicitHeadWillResolve|TestResolveRef_NoVerify_ExplicitHead_DoesNotInvokeOnExplicitHeadWillResolve|TestResolveRef_LocalSource_ExplicitHead_DoesNotInvokeOnExplicitHeadWillResolve' 2>&1 | Where-Object { $_ -match '^Test' })
if ($listed.Count -lt 4) { Fail 'ROUND3-B2-UNIT' "BLOCKING 2 的 resolveRef 單元層回歸測試 -list 只匹配 $($listed.Count) 個，需要 4 個" } else {
  # BLOCKING 4（外部稽核第五輪，2026-07-30）：改用 ExecTestJSON。
  ExecTestJSON 'ROUND3-B2-UNIT' "go test -json -run 'TestResolveRef_.*OnExplicitHeadWillResolve'" './internal/marketplace/authoring/' 'TestResolveRef_.*OnExplicitHeadWillResolve'
}
$listed = @(& go test ./cmd/apm-go/ -list 'TestMarketplacePackageAdd_ExplicitRefHead_NoVerify_NoMutableRefWarning_ExitsCode2|TestMarketplacePackageAdd_ExplicitRefHead_MissingConfig_NoMutableRefWarning|TestMarketplacePackageAdd_ExplicitRefHead_UnreachableSource_NoMutableRefWarning|TestMarketplacePackageAdd_ExplicitRefHead_DuplicateName_NoMutableRefWarning' 2>&1 | Where-Object { $_ -match '^Test' })
if ($listed.Count -lt 4) { Fail 'ROUND3-B2-CLI' "BLOCKING 2 的 CLI 端對端回歸測試 -list 只匹配 $($listed.Count) 個，需要 4 個" } else {
  # BLOCKING 4（外部稽核第五輪，2026-07-30）：改用 ExecTestJSON。
  ExecTestJSON 'ROUND3-B2-CLI' "go test -json -run 'TestMarketplacePackageAdd_ExplicitRefHead_(NoVerify|MissingConfig|UnreachableSource|DuplicateName)_.*NoMutableRefWarning'" './cmd/apm-go/' 'TestMarketplacePackageAdd_ExplicitRefHead_(NoVerify|MissingConfig|UnreachableSource|DuplicateName)_.*NoMutableRefWarning'
}

# ---- ROUND3-MAJOR-HEADMIXEDCASE（ROUND2-B1 的存活突變修補）：CLI 層也要
# 有一個 title-case "Head" 的 fixture，不只交叉積測試 ----
$listed = @(& go test ./cmd/apm-go/ -list 'TestMarketplacePackageAdd_ExplicitRefHead_MixedCase_PrintsMutableRefWarning' 2>&1 | Where-Object { $_ -match '^Test' })
if ($listed.Count -eq 0) { Fail 'ROUND3-MAJOR-HEADMIXEDCASE' 'CLI 層 title-case Head 回歸測試不存在（-list 零匹配）' } else {
  # BLOCKING 4（外部稽核第五輪，2026-07-30）：改用 ExecTestJSON。
  ExecTestJSON 'ROUND3-MAJOR-HEADMIXEDCASE' "go test -json -run 'TestMarketplacePackageAdd_ExplicitRefHead_MixedCase_PrintsMutableRefWarning'" './cmd/apm-go/' 'TestMarketplacePackageAdd_ExplicitRefHead_MixedCase_PrintsMutableRefWarning'
}

# ════════════════════════════════════════════════════════════════════════
# Round 4（外部稽核第四輪，2026-07-30）：BLOCKING 1（絕對/UNC 路徑、symlink
# 逃逸）、BLOCKING 3（`set --ref HEAD` 警告）、MAJOR 1（cross-product 直接
# 斷言）、MAJOR 3（add 端 mixed-case 的次數/嚴重程度）、MAJOR 5（未知
# refResolutionKind fail-closed）的閘門
# ════════════════════════════════════════════════════════════════════════

# ---- ROUND4-BLOCKING1-ABSOLUTE：ValidateMarketplaceSource 必須擋絕對/UNC
# 路徑（不只 '..' 段），resolveCloneURL 對「已驗證合法的相對本地路徑」逃出
# cwd 也要擋（既有 ROUND3-B1 兩個閘門已覆蓋 resolveCloneURL 本身；這裡只需
# 新增 manifest 層絕對/UNC 案例本身有沒有跑到，沿用 ROUND3-B1-MANIFEST 的
# -run 即可，已含新案例，不必新增獨立閘門）----

# ---- ROUND4-BLOCKING1-SYMLINK：resolveCloneURL 對「本地路徑內的 symlink
# 指到專案根外」也要擋 -- 沿用既有 TestResolveCloneURL，該閘門已在上方
# ROUND3-B1-RESOLVECLONEURL 升級為 ExecTestJSON（含 -allowSkip，容許此
# 環境無法建立 symlink 時可見地 t.Skip，但不容許真的 FAIL 或悄悄零匹配），
# 新子測試自動被涵蓋，不必重複一個獨立閘門。

# ---- ROUND4-BLOCKING3：`set --ref HEAD` 必須印出與 add 相同的 mutable-ref
# 警告（SetPackage 過去對 onExplicitHeadWillResolve 硬寫 nil）----
$listed = @(& go test ./internal/marketplace/authoring/ -list 'TestSetPackage_ExplicitRefHead_InvokesOnExplicitHeadWillResolve|TestSetPackage_NonHeadRef_DoesNotInvokeOnExplicitHeadWillResolve' 2>&1 | Where-Object { $_ -match '^Test' })
if ($listed.Count -lt 2) { Fail 'ROUND4-B3-UNIT' "BLOCKING 3 單元層回歸測試 -list 只匹配 $($listed.Count) 個，需要 2 個" } else {
  ExecTestJSON 'ROUND4-B3-UNIT' "go test -json -run '...'" './internal/marketplace/authoring/' 'TestSetPackage_ExplicitRefHead_InvokesOnExplicitHeadWillResolve|TestSetPackage_NonHeadRef_DoesNotInvokeOnExplicitHeadWillResolve'
}
$listed = @(& go test ./cmd/apm-go/ -list 'TestMarketplacePackageSet_RefFlagHead_PrintsMutableRefWarning|TestMarketplacePackageSet_RefFlagHead_MixedCase_PrintsMutableRefWarningOnce' 2>&1 | Where-Object { $_ -match '^Test' })
if ($listed.Count -lt 2) { Fail 'ROUND4-B3-CLI' "BLOCKING 3 CLI 端對端回歸測試 -list 只匹配 $($listed.Count) 個，需要 2 個" } else {
  ExecTestJSON 'ROUND4-B3-CLI' "go test -json -run '...'" './cmd/apm-go/' 'TestMarketplacePackageSet_RefFlagHead_PrintsMutableRefWarning|TestMarketplacePackageSet_RefFlagHead_MixedCase_PrintsMutableRefWarningOnce'
}

# ---- ROUND4-MAJOR1：cross-product 的直接 spec oracle（不共用
# classifyRefResolution 這個分類器，避免與 predictor 同一個根因一起錯）----
$listed = @(& go test ./internal/marketplace/authoring/ -list 'TestResolveRef_CrossProduct_MatchesDirectSpecOracle' 2>&1 | Where-Object { $_ -match '^Test' })
if ($listed.Count -eq 0) { Fail 'ROUND4-MAJOR1' 'MAJOR 1 直接斷言回歸測試不存在（-list 零匹配）' } else {
  ExecTestJSON 'ROUND4-MAJOR1' "go test -json -run 'TestResolveRef_CrossProduct_MatchesDirectSpecOracle'" './internal/marketplace/authoring/' 'TestResolveRef_CrossProduct_MatchesDirectSpecOracle'
}

# ---- ROUND4-MAJOR3：add 端 mixed-case "Head" 警告也要驗次數與嚴重程度
# （既有 ROUND3-MAJOR-HEADMIXEDCASE 只驗子字串，已就地加強斷言，這裡確認
# 加強後的版本本身可跑、可過）----
ExecTestJSON 'ROUND4-MAJOR3' "go test -json -run 'TestMarketplacePackageAdd_ExplicitRefHead_MixedCase_PrintsMutableRefWarning'" './cmd/apm-go/' 'TestMarketplacePackageAdd_ExplicitRefHead_MixedCase_PrintsMutableRefWarning'

# ---- ROUND4-MAJOR5：resolveRefForKind 對未知 kind 必須 fail-closed，不得
# 落到 refKindNamed 的網路解析分支 ----
$listed = @(& go test ./internal/marketplace/authoring/ -list 'TestResolveRefForKind_UnrecognizedKind_FailsClosed_NeverTouchesLister' 2>&1 | Where-Object { $_ -match '^Test' })
if ($listed.Count -eq 0) { Fail 'ROUND4-MAJOR5' 'MAJOR 5 fail-closed 回歸測試不存在（-list 零匹配）' } else {
  ExecTestJSON 'ROUND4-MAJOR5' "go test -json -run 'TestResolveRefForKind_UnrecognizedKind_FailsClosed_NeverTouchesLister'" './internal/marketplace/authoring/' 'TestResolveRefForKind_UnrecognizedKind_FailsClosed_NeverTouchesLister'
}

# ---- ROUND4-MAJOR4：AC-L9 不得只驗工作樹/暫存區 diff -- 已於下方 AC-L9
# 區塊本身修正為對照 task base commit（7ddd410^），見該區塊註解 ----

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
#
# MAJOR 4（外部稽核第四輪，2026-07-30）：上一版只驗工作樹/暫存區 diff，在一棵
# 乾淨的已 commit 樹上是空閘門 -- commit 之後才新增的一行 require 完全不會被
# 這兩個 diff 看到（工作樹和暫存區此時都是空的）。改成對照本 task 的 base
# commit（`7ddd410^`，本 task 系列第一個 commit `7ddd410` 的父提交，即本 task
# 開始改動前的狀態）：`git diff <base> -- go.mod go.sum` 同時涵蓋「已經
# commit 的每一輪修復」與「目前尚未 commit 的變更」（因為只給一個 ref 時，
# git diff 是拿該 ref 與目前工作目錄比較，未 commit 的部分也算在內）。
$taskBase = '7ddd410^'
$d1 = & git diff $taskBase -- go.mod go.sum 2>&1; $d1Exit = $LASTEXITCODE
if ($d1Exit -ne 0) {
  Fail 'AC-L9' "git diff $taskBase -- go.mod go.sum（exit $d1Exit）本身失敗，無法判定是否新增相依`n      $($d1 -join "``n      ")"
} else {
  $newReq = @($d1) | Where-Object { $_ -match '^\+\s+\S+\s+v' }
  if ($newReq) { Fail 'AC-L9' ("go.mod/go.sum 相對 task base（$taskBase）新增 require：" + ($newReq -join '; ')) } else { Pass 'AC-L9' }
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
