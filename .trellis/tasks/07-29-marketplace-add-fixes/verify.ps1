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
# 2026-07-31 外部稽核第六輪 BLOCKING 2：-minCount 只鎖「相異名稱數量」，
# 抓得到刪案例、抓不到「把安全案例換成同數量的無害案例」。新增 -requireTests：
# 逐名要求指定測試/子測試「必須出現且 Action=pass」（exact match，大小寫敏感；
# skip 不算——因此 -requireTests 名單優先於 -allowSkip）。安全關鍵案例一律
# 以身份鎖定，不再只靠數量。
function ExecTestJSON {
    # 2026-07-31 外部稽核第六輪 B-MAJOR-1（主 session 自己寫出來的 bug）：
    # AC53/behavioral 那次呼叫傳了 `-tags 'apm_test_hooks'`，但這個函式當時
    # **沒有 $tags 參數** —— PowerShell 對未宣告的具名參數不報錯，靜默塞進
    # $args 丟棄，於是實際執行的 go test 從來沒加過 tag。A 版（plugin-init）
    # 早就實作了 $tags，B 版沒同步；同一形狀的假閘門（-tags 只存在於文字而
    # 非實際指令）在 A 版自查時出現過一次，這裡以另一種方式重演。
    # 補上 $tags，並在函式尾端斷言沒有未預期的位置參數殘留（$args 非空即紅），
    # 讓下一個「傳了不存在的具名參數」直接轉紅而不是靜默吞掉。
    param([string]$ac, [string]$what, [string]$pkg, [string]$pattern, [string[]]$allowSkip = @(), [int]$minCount = 0, [string[]]$requireTests = @(), [string]$tags = '')
    if ($args.Count -gt 0) {
        Fail $ac "${what}: ExecTestJSON 收到未宣告的參數 $($args -join ' ')（PowerShell 會靜默吞掉，形成假閘門）"
        return
    }
    if ($tags) {
        $out = & go test -tags $tags -json -run $pattern $pkg 2>&1
    } else {
        $out = & go test -json -run $pattern $pkg 2>&1
    }
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
    $missingRequired = @($requireTests | Where-Object { -not $seen.ContainsKey($_) -or $seen[$_] -ne 'pass' })
    if ($missingRequired.Count -gt 0) {
        Fail $ac "${what}: 下列身份鎖定的安全測試未出現或未 pass（skip 也算失敗，BLOCKING 2 外部稽核第六輪——改名/替換/跳過皆轉紅）: $($missingRequired -join ' ; ')"
        return
    }
    if ($skipped.Count -gt 0) {
        Fail $ac "${what}: 下列測試回報 Action=skip（t.Skip 不算通過）: $($skipped -join ', ')"
        return
    }
    if ($allowedSkipped.Count -gt 0) {
        Write-Host "  note [$ac] 環境限制導致以下測試可見地 skip（非本閘門失敗）: $($allowedSkipped -join ', ')" -ForegroundColor Yellow
        # 從 $seen 移除，讓下面的「必須 pass」檢查不對它們誤判失敗。
        # 2026-07-31（B-MAJOR-1/lstat-real-acl 是第一個真的在這個環境觸發過
        # allowSkip 分支的閘門，才第一次曝露這個既有 bug）：
        # Dictionary[string,string].Remove() 回傳 bool，沒有用 [void]/$null=
        # 吞掉時 PowerShell 會把每次呼叫的回傳值直接印到主控台（一行裸的
        # "True"），純粹是輸出雜訊，不影響任何閘門的 PASS/FAIL 判定。
        foreach ($t in $allowedSkipped) { if ($seen[$t] -eq 'skip') { [void]$seen.Remove($t) } }
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

# B-MINOR-2（外部稽核第八輪，2026-07-31）：--category 的 pflag Usage 字串裡一對
# 反引號（`` `pack` ``）被 pflag.UnquoteUsage 當成 metavar 覆寫，讓 --help 印出
# "--category pack" 而非 "--category string"。上面 AC47/flag 只驗字串 --category
# 有沒有出現，抓不到 metavar 被換掉這件事——新增身份鎖定。
ExecTestJSON 'B-MINOR-2' "go test -json -run 'TestMarketplacePackageAdd_CategoryFlagHelp_MetavarIsString'" './cmd/apm-go/' 'TestMarketplacePackageAdd_CategoryFlagHelp_MetavarIsString' -requireTests @('TestMarketplacePackageAdd_CategoryFlagHelp_MetavarIsString')

# 2026-07-31 外部稽核第六輪 BLOCKING 3：--category 過去只驗 help 有旗標，
# AC47（實際寫入 category）與 AC50（add→pack codex 端對端）的行為測試存在
# 卻沒有接進閘門——刪掉它們 + 把寫入路徑改壞，閘門仍綠。以身份鎖定接入。
ExecTestJSON 'AC47/behavior' "go test -json -run 'TestMarketplacePackageAdd_CategoryFlag_WritesCategory'" './cmd/apm-go/' 'TestMarketplacePackageAdd_CategoryFlag_WritesCategory' -requireTests @('TestMarketplacePackageAdd_CategoryFlag_WritesCategory')
ExecTestJSON 'AC50/behavior' "go test -json -run 'TestMarketplacePackageAdd_CategoryFlag_ThenPackCodex_Succeeds'" './cmd/apm-go/' 'TestMarketplacePackageAdd_CategoryFlag_ThenPackCodex_Succeeds' -requireTests @('TestMarketplacePackageAdd_CategoryFlag_ThenPackCodex_Succeeds')

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

# 2026-07-31 外部稽核第五輪 B-MAJOR-3：上面的 AC22 只探 CLI 輸出，而 CLI 的
# marketplaceNotRegisteredErr 自己就會補指引 —— 把 registry.go 的函式庫層錯誤
# 還原成裸訊息，CLI 探針照樣綠。PRD R6 要求的是 registry.go 本身的錯誤契約，
# 逐名鎖定驗證它的那個測試。
ExecTestJSON 'AC22/library-layer' "go test -json -run 'TestRemoveSource_UnregisteredError_IncludesRemediation'" './internal/marketplace/' 'TestRemoveSource_UnregisteredError_IncludesRemediation' -requireTests @('TestRemoveSource_UnregisteredError_IncludesRemediation')

# ---- AC18：--no-verify 的 exit code 必須是 2（用 binary，不可用 go run） ----
# go run 會把子行程的 2 變成 1 —— 已實測，見 review/codex-audit-checklist.md 阻斷 4
#
# A-BLOCKING-3（外部稽核第九輪，2026-07-31）：先前把 stderr 直接丟
# `Out-Null`，只驗 exit code，完全沒斷言錯誤訊息本身的內容——把訊息文字改壞
# （甚至換成一句不相干但同樣 exit 2 的錯誤）這條閘門依舊全綠。改為捕捉輸出，
# 同時驗 exit code 與訊息逐字內容（與 cmd/apm-go/marketplace_package_test.go
# 的 TestMarketplacePackageAdd_NoVerify_ImplicitHead_ExitsCode2 斷言的同一句
# 上游對齊訊息一致）。
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
  $ac18Out = & $bin marketplace package add owner/repo --no-verify 2>&1 | Out-String
  if ($LASTEXITCODE -ne 2) {
    Fail 'AC18' "--no-verify 隱含 HEAD 應 exit 2，實際 $LASTEXITCODE（輸出：$($ac18Out.Trim())）"
  } elseif ($ac18Out -notmatch [regex]::Escape('Cannot resolve HEAD ref without network access. Provide an explicit --ref SHA.')) {
    Fail 'AC18' "exit code 是 2，但訊息不是上游對齊文字，實際：$($ac18Out.Trim())"
  } else { Pass 'AC18' }
} finally { Pop-Location -EA SilentlyContinue; Remove-Item $probe2 -Recurse -Force -EA SilentlyContinue }
ExecTestJSON 'AC18/unit' "go test -json -run 'TestMarketplacePackageAdd_NoVerify_ImplicitHead_ExitsCode2'" './cmd/apm-go/' 'TestMarketplacePackageAdd_NoVerify_ImplicitHead_ExitsCode2' -requireTests @('TestMarketplacePackageAdd_NoVerify_ImplicitHead_ExitsCode2')

# ---- AC17/AC20（外部稽核第九輪，2026-07-31）：行為測試存在卻從未被本檔
# 引用過——marketplace_package_test.go:813 的 AC17 測試（零旗標 add 對遠端
# 來源寫入解析後的 HEAD SHA）與 :1089 的 AC20 測試（--version 時完全不解析
# /寫入 ref）都不在任何一條閘門的 -run/-requireTests 名單裡：刪掉任一個，
# 或把它們的斷言拔掉，本檔仍然全綠。以身份鎖定接入（逐字名稱，非用猜的
# ——用 `go test -list` 先撈出真實函式名稱確認存在）。----
ExecTestJSON 'AC17/behavior' "go test -json -run 'TestMarketplacePackageAdd_ZeroFlags_RemoteSource_WritesResolvedHeadSHA'" './cmd/apm-go/' 'TestMarketplacePackageAdd_ZeroFlags_RemoteSource_WritesResolvedHeadSHA' -requireTests @('TestMarketplacePackageAdd_ZeroFlags_RemoteSource_WritesResolvedHeadSHA')
ExecTestJSON 'AC20/behavior' "go test -json -run 'TestMarketplacePackageAdd_VersionGiven_DoesNotWriteRef'" './cmd/apm-go/' 'TestMarketplacePackageAdd_VersionGiven_DoesNotWriteRef' -requireTests @('TestMarketplacePackageAdd_VersionGiven_DoesNotWriteRef')

# ---- AC40：resolveRef 各分支測試存在（外部稽核第九輪，2026-07-31：先前只
# -list 且只要求數量 >= 3，數量門檻本身太弱——刪掉 27 個子測試、只留 3 個
# 完全不相干的分支一樣會通過，且從不實際 -run。改用 ExecTestJSON：
# -minCount 鎖住目前 `go test ./internal/marketplace/authoring/ -list
# 'ResolveRef|ImplicitHead'` 實測到的 30 個相異測試名稱，並以 -requireTests
# 身份鎖定 AC40 逐字列出的六個分支各一個代表測試：隱含 HEAD、顯式 HEAD、
# --version、--no-verify、40-hex SHA（ConcreteSHA，非法 hex 的 40 字元走
# lister 是另一個獨立分支，不代表這裡）、local source ----
ExecTestJSON 'AC40' "go test -json -run 'ResolveRef|ImplicitHead'" './internal/marketplace/authoring/' 'ResolveRef|ImplicitHead' -minCount 30 -requireTests @(
  'TestResolveRef_ImplicitHead_RemoteSource_ResolvesViaLister',
  'TestResolveRef_ExplicitHead_RemoteSource_ResolvesViaLister',
  'TestResolveRef_VersionGiven_SkipsResolution_NoListerCall',
  'TestResolveRef_NoVerify_ImplicitHead_ReturnsOfflineError',
  'TestResolveRef_ConcreteSHA_ReturnedAsIs_NoListerCall',
  'TestResolveRef_LocalSource_ExplicitHead_NeverTouchesNetwork'
)

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
  # 2026-07-31 第六輪 BLOCKING 2：-minCount 之外再以身份鎖定兩個反斜線
  # traversal 安全案例（被替換成同數量的無害案例時，數量檢查不會動）。
  #
  # B-BLOCKING-2（外部稽核第六輪，2026-07-31）：上面三個身份鎖定案例全部與
  # percent-decode 無關 -- `maxPercentDecodeRounds`（mcp.go）從 8 改成 1 這種
  # 突變不會被 -minCount（案例總數不變）或上面三個 requireTests（都不是
  # percent-encoded 案例）擋下：`./%252e%252e/outside` 需要恰好 2 輪解碼才能
  # 還原成 ".."（"%252e%252e" -> "%2e%2e" -> ".."），改成只解 1 輪會讓這個
  # 案例從「拒絕」變成「通過」，但案例數量、其餘身份鎖定案例的通過與否都
  # 不受影響 -- 是本函式自己文件註解點名、卻沒有任何閘門守住的逃逸口。補上
  # 這三個 percent-decode 身份鎖定案例（單輪、雙輪、大寫十六進位各一）關閉它。
  ExecTestJSON 'ROUND3-B1-MANIFEST' "go test -json -run 'TestValidateMarketplaceSource'" './internal/manifest/' 'TestValidateMarketplaceSource' -minCount 31 -requireTests @(
    'TestValidateMarketplaceSource/./..\..\outside',
    'TestValidateMarketplaceSource/./sub\..\..\outside',
    'TestValidateMarketplaceSource/D:\outside\repo',
    'TestValidateMarketplaceSource/./%2e%2e/%2e%2e/outside',
    'TestValidateMarketplaceSource/./%252e%252e/outside',
    'TestValidateMarketplaceSource/./%2E%2E/outside'
  )
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
  # 2026-07-31 第六輪 BLOCKING 1+2：撤銷 allowSkip 並以身份鎖定兩個 symlink
  # 逃逸測試——它們是唯二能抓 pathWithinRoot symlink 回歸的測試，skip 即假綠
  # （無 symlink 特權的環境由測試側的 junction fallback 解決，而非閘門放行）。
  #
  # B-MAJOR-1（外部稽核第六輪，2026-07-31）：pathWithinRoot 的 junction 第三層
  # 原本對任何存在的 reparse point 一律拒絕，從不解析它實際指向哪裡——這既會
  # 誤判「root 自己被 junction 中介」的正常專案佈局，也會誤判「junction 指向
  # root 內部」的正常本地套件佈局。改為 resolveRealPathJunctionAware（透過
  # os.Readlink 真的解析 junction 目標,而非只看 Lstat 的 ModeSymlink 位元）後,
  # 案例總數從 11 變成 14（新增 2 個回歸測試 + 既有 1 個逃逸測試，見下方
  # `go test -json -run TestResolveCloneURL ./internal/marketplace/authoring/ |
  #  grep -oE '"Test":"[^"]+"' | sort -u | wc -l` 實測確認為 14）；以身份鎖定
  # 新增的兩個「junction 不應被誤拒」案例，防止被同數量的無害案例替換掉。
  ExecTestJSON 'ROUND3-B1-RESOLVECLONEURL' "go test -json -run 'TestResolveCloneURL'" './internal/marketplace/authoring/' 'TestResolveCloneURL' -minCount 14 -requireTests @(
    'TestResolveCloneURL/relative_local_source_escaping_cwd_via_a_directory_symlink_is_rejected',
    'TestResolveCloneURL/relative_local_source_with_a_dangling_leaf_under_an_escaping_symlinked_parent_is_rejected',
    'TestResolveCloneURL_JunctionWithinRoot_Accepted',
    'TestResolveCloneURL_ProjectRootBehindJunction_LocalSourceWithinRoot_Accepted'
  )
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

# ---- B-BLOCKING-2（外部稽核第八輪，2026-07-31）：round 7 補的四支迴歸測試
# （SECRET-NESTED 巢狀 junction 逃逸、apm.yml leaf symlink 逃逸、reparse tag
# name-surrogate 判斷）從未被本檔任何一行引用過——它們存在於原始碼裡，`go
# test ./...`（全套件跑）當然會執行到並算進總體 PASS 統計，但本檔（逐項
# 身份鎖定的安全閘門）完全沒有單獨點名它們：把任何一個刪掉、改壞、或換成
# t.Skip，本檔仍然全綠。以身份鎖定接入，其中兩個（NestedJunctionEscape 的
# 兩個子測試）在無 junction/symlink 特權的環境下可能 t.Skip——本機（此次
# 執行環境）兩者皆真的跑過並 PASS（非 skip），故用 -requireTests 逐一鎖定
# 精確名稱；-allowSkip 保留給 leaf-symlink 測試，因為檔案層級的 reparse
# point（不同於目錄）沒有像 mklink /J junction 那樣免特權的替代建立方式可
# 用（hardlink 不成立：它不是 reparse point，走到它不會觸發任何 containment
# 檢查，用它取代symlink 只會做出一個保證恆綠、什麼都測不到的假閘門——見
# verification-record.md 本輪節的說明），本機雖然實測為真的 PASS，仍保留
# -allowSkip 讓其他機器上的環境限制以「可見 skip」呈現而非讓整支閘門紅掉。
ExecTestJSON 'B-BLOCKING-2/nested-junction-unit' "go test -json -run 'TestResolveLocalSourceAgainstRoot_NestedJunctionEscape_Rejected'" './internal/marketplace/authoring/' 'TestResolveLocalSourceAgainstRoot_NestedJunctionEscape_Rejected' -minCount 3 -allowSkip @('nested_junction_chain_escapes_through_an_intermediate_component', 'cycle_A->B->A_fails_closed') -requireTests @('TestResolveLocalSourceAgainstRoot_NestedJunctionEscape_Rejected')
ExecTestJSON 'B-BLOCKING-2/nested-junction-e2e' "go test -json -run 'TestPack_NestedJunctionEscape_Rejected'" './cmd/apm-go/' 'TestPack_NestedJunctionEscape_Rejected' -requireTests @('TestPack_NestedJunctionEscape_Rejected')
# B-BLOCKING-2（外部稽核第九輪，2026-07-31 follow-up）：上一行先前把
# TestPack_LocalSourceApmYmlLeafSymlinkEscape_Rejected 自己的精確名稱放進
# -allowSkip，等於明文允許這個唯二驗證「檔案層級 symlink」逃逸的安全回歸
# 測試本身可見地 t.Skip 並仍視為通過——但 PRD 明定 t.Skip 不算通過，這是
# 本檔對自己文件化原則的直接違反。改為單純 -requireTests：具備檔案 symlink
# 建立特權的環境（本機即是；或任何已開啟 Windows Developer Mode / 以
# SeCreateSymbolicLinkPrivilege 身分執行的環境）必須讓它真的跑且 PASS，
# 不得以 skip 蒙混——沒有這個特權本身就是這個閘門的失敗，不是被容許的例外。
ExecTestJSON 'B-BLOCKING-2/leaf-symlink-e2e' "go test -json -run 'TestPack_LocalSourceApmYmlLeafSymlinkEscape_Rejected'" './cmd/apm-go/' 'TestPack_LocalSourceApmYmlLeafSymlinkEscape_Rejected' -requireTests @('TestPack_LocalSourceApmYmlLeafSymlinkEscape_Rejected')
ExecTestJSON 'B-BLOCKING-2/reparse-tag' "go test -json -run 'TestIsNameSurrogateReparseTag'" './internal/marketplace/authoring/' 'TestIsNameSurrogateReparseTag' -minCount 6 -requireTests @('TestIsNameSurrogateReparseTag')

# ---- B-MAJOR-1（外部稽核第八輪，2026-07-31）：longestExistingAncestor 與
# resolveRealPathJunctionAware 過去把「Lstat 失敗」一律當成「不存在」，包括
# ACL/權限拒絕之類其實應該 fail-closed 的錯誤。三個 fake-osLstat 單元測試 +
# 一個真實 icacls ACL 拒絕的端對端測試（本機環境該 deny ACE 對本行程的
# os.Lstat 無效——極可能因為以擁有者/較高權限身分執行繞過了一般 DACL 檢查
# ——因此會可見地 t.Skip，用 -allowSkip 容許） ----
# B-BLOCKING-2（外部稽核第十輪，2026-07-31）：下面這條與 canon-real8dot3
# 那條各自把「被驗測試自己的精確名稱」放進 -allowSkip -- 用 t.Skip("mutation")
# 實測過，這個形狀確實讓兩條規則在測試本體被永久跳過時仍回報 ok（與
# leaf-symlink 先前被拿掉 -allowSkip 的理由同形狀）。這裡刻意保留（不同於
# leaf-symlink），因為上面三個 fake-osLstat 測試（-requireTests、無
# -allowSkip、不受環境影響）已獨立覆蓋 fail-closed 邏輯本身；完整的
# mutation 證據、威脅模型與成本估計見
# verification-record.md「B-BLOCKING-2（外部稽核第十輪）」一節。
ExecTestJSON 'B-MAJOR-1/lstat-ancestor' "go test -json -run 'TestLongestExistingAncestor_NonNotExistLstatError_FailsClosed'" './internal/marketplace/authoring/' 'TestLongestExistingAncestor_NonNotExistLstatError_FailsClosed' -requireTests @('TestLongestExistingAncestor_NonNotExistLstatError_FailsClosed')
ExecTestJSON 'B-MAJOR-1/lstat-resolve' "go test -json -run 'TestResolveRealPathJunctionAware_NonNotExistLstatError_FailsClosed'" './internal/marketplace/authoring/' 'TestResolveRealPathJunctionAware_NonNotExistLstatError_FailsClosed' -requireTests @('TestResolveRealPathJunctionAware_NonNotExistLstatError_FailsClosed')
ExecTestJSON 'B-MAJOR-1/lstat-pathwithinroot' "go test -json -run 'TestPathWithinRoot_NonNotExistLstatError_FailsClosed'" './internal/marketplace/authoring/' 'TestPathWithinRoot_NonNotExistLstatError_FailsClosed' -requireTests @('TestPathWithinRoot_NonNotExistLstatError_FailsClosed')
ExecTestJSON 'B-MAJOR-1/lstat-real-acl' "go test -json -run 'TestPathWithinRoot_RealACLDeniedComponent_FailsClosed'" './internal/marketplace/authoring/' 'TestPathWithinRoot_RealACLDeniedComponent_FailsClosed' -minCount 1 -allowSkip @('TestPathWithinRoot_RealACLDeniedComponent_FailsClosed')

# ---- B-MAJOR-1（外部稽核第九輪，2026-07-31 follow-up）：canonicalize_windows.go
# 的兩個獨立 bug ----
# (1) stripExtendedLengthPrefix 把 `\\?\UNC\server\share` 還原成
#     `\server\share`（少一個反斜線；正確應為 `\\server\share`），
#     每一條經過此路徑的 UNC local source 都會被還原成一個無效的 UNC
#     拼法，破壞後續 containment 比對。
# (2) canonicalizeRealPath 過去固定只用 VOLUME_NAME_DOS 呼叫
#     GetFinalPathNameByHandleW；沒有 drive letter 的 volume（只掛載在
#     NTFS mount point 下）該呼叫必定回傳 ERROR_PATH_NOT_FOUND，即使控制柄
#     開啟成功、路徑確實存在——fail-closed 契約因此把一個合法路徑誤判為
#     canonicalization 失敗並拒絕。改為 DOS 失敗且錯誤正是
#     ERROR_PATH_NOT_FOUND 時退回 VOLUME_NAME_GUID（不依賴 drive letter），
#     其他任何失敗原因仍立即 fail-closed。
# 純字串轉換（1）用 fake-seam 單元測試涵蓋（不必真掛載 UNC 分享），身份鎖定：
ExecTestJSON 'B-MAJOR-1/unc-strip' "go test -json -run 'TestStripExtendedLengthPrefix_UNC'" './internal/marketplace/authoring/' 'TestStripExtendedLengthPrefix_UNC' -requireTests @('TestStripExtendedLengthPrefix_UNC')

# 2026-07-31 第五輪 B-MAJOR-2：GUID fallback 分支（DOS 回 ERROR_PATH_NOT_FOUND
# 時改用 VOLUME_NAME_GUID=0x1 重試）實作存在但沒有任何測試觸及——把 0x1 改成
# 0x2 或整段刪掉，既有 strip 測試都不會動。逐名鎖定驅動該分支的兩個測試
# （正向 fallback + 非 PATH_NOT_FOUND 錯誤不重試的反向）。
ExecTestJSON 'B-MAJOR-2/guid-fallback' "go test -json -run 'TestCanonicalizeRealPath_(FallsBackToVolumeGUID_OnErrorPathNotFound|NonPathNotFoundError_NoFallbackRetry)'" './internal/marketplace/authoring/' 'TestCanonicalizeRealPath_FallsBackToVolumeGUID_OnErrorPathNotFound|TestCanonicalizeRealPath_NonPathNotFoundError_NoFallbackRetry' -requireTests @(
  'TestCanonicalizeRealPath_FallsBackToVolumeGUID_OnErrorPathNotFound',
  'TestCanonicalizeRealPath_NonPathNotFoundError_NoFallbackRetry'
)
ExecTestJSON 'B-MAJOR-1/driveletter-strip' "go test -json -run 'TestStripExtendedLengthPrefix_DriveLetter'" './internal/marketplace/authoring/' 'TestStripExtendedLengthPrefix_DriveLetter' -requireTests @('TestStripExtendedLengthPrefix_DriveLetter')

# ---- B-MINOR-1（外部稽核第八輪，2026-07-31）：pathWithinRoot 的最終比對過去
# 是純字串比較（filepath.Rel），8.3 短檔名/UNC/Volume-GUID 等別名可能讓同一
# 個實體目錄比對失敗。canonicalizeRealPathFn（Windows 用
# GetFinalPathNameByHandleW，經 syscall.NewLazyDLL 直接呼叫，未新增相依）
# 三個 fake-seam 單元測試（失敗即 fail-closed、確實被呼叫兩次、別名後仍判定
# contained）+ 一個真實 8.3 短檔名端對端測試（本機該 volume 確實啟用短檔名
# 產生，故實測 PASS，非 skip；-allowSkip 仍保留給短檔名產生被停用的其他
# 機器） ----
# B-BLOCKING-2（外部稽核第十輪，2026-07-31）：canon-real8dot3 這條與上面
# lstat-real-acl 同形狀的 -allowSkip 自我豁免，mutation 證據、保留（不移除）
# 的理由、威脅模型與成本估計見 verification-record.md「B-BLOCKING-2（外部
# 稽核第十輪）」一節——上面三個 fake-seam 測試（-requireTests、無
# -allowSkip）已獨立覆蓋 canonicalize 的 fail-closed 邏輯本身。
ExecTestJSON 'B-MINOR-1/canon-failclosed' "go test -json -run 'TestPathWithinRoot_CanonicalizationFailure_FailsClosed'" './internal/marketplace/authoring/' 'TestPathWithinRoot_CanonicalizationFailure_FailsClosed' -requireTests @('TestPathWithinRoot_CanonicalizationFailure_FailsClosed')
ExecTestJSON 'B-MINOR-1/canon-wiring' "go test -json -run 'TestPathWithinRoot_UsesCanonicalizedPaths_NotRawStrings'" './internal/marketplace/authoring/' 'TestPathWithinRoot_UsesCanonicalizedPaths_NotRawStrings' -requireTests @('TestPathWithinRoot_UsesCanonicalizedPaths_NotRawStrings')
ExecTestJSON 'B-MINOR-1/canon-alias' "go test -json -run 'TestPathWithinRoot_CanonicalizationNormalizesAliasedSpelling_StillContained'" './internal/marketplace/authoring/' 'TestPathWithinRoot_CanonicalizationNormalizesAliasedSpelling_StillContained' -requireTests @('TestPathWithinRoot_CanonicalizationNormalizesAliasedSpelling_StillContained')
ExecTestJSON 'B-MINOR-1/canon-real8dot3' "go test -json -run 'TestPathWithinRoot_Real8dot3ShortName_StillDetectedAsContained'" './internal/marketplace/authoring/' 'TestPathWithinRoot_Real8dot3ShortName_StillDetectedAsContained' -minCount 1 -allowSkip @('TestPathWithinRoot_Real8dot3ShortName_StillDetectedAsContained')

# ---- AC53：回歸閘門 —— marketplace init 必須維持非互動（D13） ----
# 防止實作 plugin-init 時順手把 clack 帶進 marketplace init。
#
# B-MAJOR-1（外部稽核第七輪，2026-07-31）：round 6 的黑名單本身還有一個
# 逃逸口，且不需要任何別名把戲就能觸發——一個 Go dot import
# (`import . "charm.land/huh/v2"`) 的別名 token 字面上是 `.`，`\w+` 這個
# regex（round 6 的 $huhIdents 偵測）永遠配不到 `.`；更根本的是 dot import
# 之後 huh 的每一個 exported 名稱都變成「裸」呼叫（`NewForm(...)`，完全沒有
# `huh.` 或任何別名前綴），regex 掃全文字根本沒有東西可以比對。改用真的
# Go AST 解析（TestMarketplaceInitCmd_NoInteractiveComponents，
# cmd/apm-go/ac53_interactive_gate_test.go）取代 regex：
# (1) 掃 marketplace_authoring.go 的 import 宣告，若對任何「互動性」套件
#     （charm.land/huh、internal/ux）使用 dot import，直接判定失敗——
#     不論有沒有實際呼叫，這種 import 本身就讓任何字串黑名單失效；
# (2) 對具名/別名 import，解析出實際綁定的識別字，再檢查
#     marketplaceInitCmd 函式體（含巢狀函式字面值）裡是否有透過該識別字
#     的呼叫；huh 的話任何呼叫都算違規（huh 全部匯出都是互動元件），
#     ux 的話只比對既有黑名單的特定 selector（NewClack/InputText/Password/
#     MultiSelect/InputForm/Confirm），避免把 ux.Success/BulletList 這類
#     既有合法用法也一併擋下。
# 可否證性：這條閘門本身已用 mutation 驗證過——暫時把 dot import 加回
# marketplace_authoring.go 並掛一個裸 `NewForm` 呼叫，
# TestMarketplaceInitCmd_NoInteractiveComponents 會轉紅；還原後轉綠
# （implementer 本地跑過，見 verification-record.md）。
#
# B-BLOCKING-1（外部稽核第八輪，2026-07-31）：上面 (1)(2) 描述的版本只掃
# marketplaceInitCmd 自己的函式本體——把互動呼叫搬進一個獨立的 package-level
# helper function（或一個直接別名到互動 selector 的 package-level var，如
# `var f = ux.Confirm`）再由 marketplaceInitCmd 呼叫該 helper，可以完全繞過
# 掃描，不需要任何 alias 花招。已改為（bounded）呼叫圖走訪：從
# marketplaceInitCmd 開始，任何透過具名識別字呼叫到的 package-level
# func/var（跨本套件每一個非 _test.go 檔案，不只 marketplace_authoring.go）
# 都會被遞迴掃描；package-level var 直接別名到一個已綁定識別字的 selector
# 也會被追蹤。同樣已用 mutation 驗證過三種情境（helper+var alias 在同檔、
# helper+var alias 跨檔、dot import）皆會轉紅，見 verification-record.md。
$listed = @(& go test ./cmd/apm-go/ -list 'TestMarketplaceInitCmd_NoInteractiveComponents' 2>&1 | Where-Object { $_ -match '^Test' })
if ($listed.Count -eq 0) { Fail 'AC53/no-clack' 'TestMarketplaceInitCmd_NoInteractiveComponents 不存在（-list 零匹配）' } else {
  ExecTestJSON 'AC53/no-clack' "go test -json -run 'TestMarketplaceInitCmd_NoInteractiveComponents'" './cmd/apm-go/' 'TestMarketplaceInitCmd_NoInteractiveComponents'
}

# 2026-07-31 外部稽核第五輪 B-BLOCKING-1：AST 靜態分析對這題有已證實的根本
# 上限——漏報（函式值存進 struct/slice/map 後呼叫、generic receiver 的
# *ast.IndexExpr 型別運算式讓方法根本不進 callables）與誤報（同名方法保守展開
# 會把不可達的方法判成違規）同時惡化。改以**行為驗證**為權威層：強制
# CanPrompt() 為真並樁掉三個 prompt seam，執行 marketplaceInitCmd，斷言沒有
# 任何 prompt 被呼叫——不論經過幾層間接都攔得到。AST 測試保留為輔助層。
# 實作方已用 generic-receiver mutation 證實：AST 測試維持綠，行為測試轉紅。
$listedBehavioral = @(& go test -tags apm_test_hooks ./cmd/apm-go/ -list 'TestMarketplaceInitCmd_NoInteractivePrompt_Behavioral' 2>&1 | Where-Object { $_ -match '^Test' })
if ($listedBehavioral.Count -eq 0) {
  Fail 'AC53/behavioral' 'TestMarketplaceInitCmd_NoInteractivePrompt_Behavioral 不存在（-list 零匹配）'
} else {
  ExecTestJSON 'AC53/behavioral' "go test -tags apm_test_hooks -json -run 'TestMarketplaceInitCmd_NoInteractivePrompt_Behavioral'" './cmd/apm-go/' 'TestMarketplaceInitCmd_NoInteractivePrompt_Behavioral' -tags 'apm_test_hooks' -requireTests @('TestMarketplaceInitCmd_NoInteractivePrompt_Behavioral')

# AC53/stdin（2026-08-04，外部稽核 codex 發現、主 session 實跑重現）：
# 上面兩層都是「名字導向」—— AST 閘門走 huh/ux 綁定的識別字，seam 閘門只數
# 走 ux prompt seam 的呼叫。直接讀 os.Stdin（fmt.Fscan / bufio.NewReader(os.Stdin)
# / os.Stdin.Read）兩者皆非，全部維持綠色。
# 已實測：把 `if !force { var p string; fmt.Fscan(os.Stdin, &p) }` 插進
# marketplaceInitCmd，三層閘門全綠，但實際二進位在 live stdin 下阻塞
# （`sleep 60 | timeout 8 apm-go marketplace init` → rc=124；stdin=/dev/null → rc=0）。
# go test 抓不到的原因就是測試行程的 stdin 會立即 EOF。
# 新閘門改為行為導向：把 os.Stdin 換成永不供給資料、write end 保持開啟的 pipe，
# 任何讀取都會阻塞，10 秒逾時即判失敗 —— 對「所有讀 stdin 的行為」有效，
# 不需要事先知道要黑名單哪個函式。
$listed = @(& go test -tags apm_test_hooks ./cmd/apm-go/ -list 'TestMarketplaceInitCmd_DoesNotReadStdin' 2>&1 | Where-Object { $_ -match '^Test' })
if ($listed.Count -eq 0) {
  Fail 'AC53/stdin' 'TestMarketplaceInitCmd_DoesNotReadStdin 不存在（-list 零匹配）'
} else {
  ExecTestJSON 'AC53/stdin' "go test -tags apm_test_hooks -json -run 'TestMarketplaceInitCmd_DoesNotReadStdin'" './cmd/apm-go/' 'TestMarketplaceInitCmd_DoesNotReadStdin' -tags 'apm_test_hooks' -requireTests @('TestMarketplaceInitCmd_DoesNotReadStdin')
}
}

# B-BLOCKING-1（外部稽核第十輪，2026-07-31）：上面那條行為測試從不傳
# --force，所以從未真的執行到 spliceMarketplaceBlock 的覆蓋分支——一個只在
# --force 為真時才讀 os.Stdin 的 generic-receiver mutation（既不會被
# AC53/no-clack 的 AST 閘門攔到，也不會被上面的行為測試攔到，因為它從不
# 傳 --force）完全不會被本檔任何一行擋下。新增
# TestMarketplaceInitCmd_ForceFlag_NoInteractivePrompt_Behavioral：對一個
# 已有 marketplace: 區塊的 apm.yml 跑 --force（真的走覆蓋分支），並把
# os.Stdin 導向一個永不寫入、命令結束前也不關閉的 pipe -- 一個會讀 stdin
# 的 mutation 會卡住直到逾時，用與 plugin_init_interactive_test.go 的
# driveInteractiveInit 相同的 goroutine+timeout 手法偵測。已用
# generic-receiver mutation 實測驗證過：這條測試轉紅，
# TestMarketplaceInitCmd_NoInteractiveComponents（AST 閘門）維持綠——證據見
# verification-record.md。
$listedForce = @(& go test ./cmd/apm-go/ -list 'TestMarketplaceInitCmd_ForceFlag_NoInteractivePrompt_Behavioral' 2>&1 | Where-Object { $_ -match '^Test' })
if ($listedForce.Count -eq 0) {
  Fail 'B-BLOCKING-1/force-behavioral' 'TestMarketplaceInitCmd_ForceFlag_NoInteractivePrompt_Behavioral 不存在（-list 零匹配）'
} else {
  ExecTestJSON 'B-BLOCKING-1/force-behavioral' "go test -json -run 'TestMarketplaceInitCmd_ForceFlag_NoInteractivePrompt_Behavioral'" './cmd/apm-go/' 'TestMarketplaceInitCmd_ForceFlag_NoInteractivePrompt_Behavioral' -requireTests @('TestMarketplaceInitCmd_ForceFlag_NoInteractivePrompt_Behavioral')
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
  # 2026-07-31 第六輪 MAJOR 2：舊 regex `^\+\s+\S+\s+v` 只抓 require 區塊內
  # 「+ 後有縮排」的行——合法的單行 `require module vX` directive 與 go.sum
  # 新增行都是 `+` 後直接接文字，完全不匹配。改為：go.mod 的 require 行
  #（區塊內縮排式與單行式皆抓）+ go.sum 的任何新增內容行都算新增相依訊號。
  $newReq = @($d1) | Where-Object {
    $_ -match '^\+\s+\S+\s+v\d' -or          # require ( ... ) 區塊內
    $_ -match '^\+require\s+\S+\s+v\d' -or   # 單行 require directive
    $_ -match '^\+\S+/\S+\s+v\d.*\bh1:'      # go.sum 新增行（module vX h1:hash）
  }
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
