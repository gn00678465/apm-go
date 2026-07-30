#requires -Version 7
# Tier 1 確定性閘門 — plugin-init
#
# 這個 child 有 4 條入邊。依 loop-graph-engineering.md 模式 3：
#   「Coordinator 在上游驗證成功前不得推進到相依子任務，避免未經驗證的父節點污染子節點」
# ⇒ 入邊載荷檢查放在最前面，任一條斷掉就 exit 2（BLOCKED），不進行本 child 的 AC 檢查。
#   exit 2 = 被上游封鎖（不是本 child 的失敗）
#   exit 1 = 本 child 自己的 AC 失敗

$ErrorActionPreference = 'Stop'
$repo = (Resolve-Path "$PSScriptRoot/../../..").Path
Push-Location $repo
$script:fails = @(); $script:blocked = @()
function Fail($ac,$m){ $script:fails += "[$ac] $m"; Write-Host "  FAIL [$ac] $m" -ForegroundColor Red }
function Block($e,$m){ $script:blocked += "[$e] $m"; Write-Host "  BLOCKED [$e] $m" -ForegroundColor Magenta }
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

# ExecTestJSON（移植自 07-29-marketplace-add-fixes/verify.ps1，外部稽核第四、五輪
# 修復；完整推導過程與逐輪 bug 見該檔同名函式註解，這裡只保留行為必要的說明）：
# `go test -run <pattern>` 只驗 exit code 擋不住 t.Skip -- 每個測試都跳過、沒有
# 任何 FAIL 時 Go 仍回傳 exit 0。改用 `go test -json`：對 pattern 匹配到的每一個
# 測試名稱（含子測試），要求它「有出現 Action=pass」且「從未出現 Action=skip」。
#
# 同時修了三個曾經在對抗性稽核中被抓到的假綠：
# (1) 沒有 Test 欄位但 Action=fail 的套件層級事件（例如 TestMain 在 m.Run() 之後
#     回傳非 0）過去被直接 continue 跳過 -- 現在記為 $packageFailed 並轉紅。
# (2) 看起來像 JSON（以 "{" 開頭）卻解析失敗的行過去被靜默 continue -- 現在記為
#     $malformed 並轉紅，不再假設「解析失敗 = 不重要」。
# (3) `-list` 只列頂層測試函式名稱，對 table-driven 測試刪掉幾個 t.Run case 之後
#     `-list` 的數量完全不會變 -- 新增 `-minCount` 對 `-json` 逐測試事件觀察到的
#     相異測試/子測試名稱數設下限，刪案例會讓這個數字低於 $minCount 而轉紅。
# 另外 $seen 用 case-SENSITIVE 的 Dictionary，而非 PowerShell `@{}`（後者對字串鍵
# 預設不分大小寫，會讓只差大小寫的兩個測試名稱互相覆蓋、讓 -minCount 少算）。
function ExecTestJSON {
    param([string]$ac, [string]$what, [string]$pkg, [string]$pattern, [string[]]$allowSkip = @(), [int]$minCount = 0)
    $out = & go test -json -run $pattern $pkg 2>&1
    $exitCode = $LASTEXITCODE
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
        Fail $ac "${what}: 輸出含看起來像 JSON 卻無法解析的行（可能截斷或摻雜非 JSON 內容），前幾行: $((($malformed | Select-Object -First 5)) -join ' | ')"
        return
    }
    $totalSeen = $seen.Count
    if ($minCount -gt 0 -and $totalSeen -lt $minCount) {
        Fail $ac "${what}: 只觀察到 $totalSeen 個測試/子測試事件，至少需要 $minCount 個（子測試可能被刪除）"
        return
    }
    if ($packageFailed) {
        Fail $ac "${what}: 套件層級回報 Action=fail（例如 TestMain 在 m.Run() 之後回傳非 0，或測試後發生 panic）-- 即使每個個別測試都 pass 也算失敗"
        return
    }
    if ($skipped.Count -gt 0) {
        Fail $ac "${what}: 下列測試回報 Action=skip（t.Skip 不算通過）: $($skipped -join ', ')"
        return
    }
    if ($allowedSkipped.Count -gt 0) {
        Write-Host "  note [$ac] 環境限制導致以下測試可見地 skip（非本閘門失敗）: $($allowedSkipped -join ', ')" -ForegroundColor Yellow
        foreach ($t in $allowedSkipped) { if ($seen[$t] -eq 'skip') { $seen.Remove($t) } }
    }
    $notPassed = @($seen.GetEnumerator() | Where-Object { $_.Value -ne 'pass' })
    if ($notPassed.Count -gt 0) {
        Fail $ac "${what}: 下列測試從未回報 Action=pass: $((($notPassed | ForEach-Object { $_.Key })) -join ', ')"
        return
    }
    if ($exitCode -ne 0) {
        Fail $ac "${what}: 每個測試都回報 pass、也沒有套件層級 fail 事件，但 go test 本身以非 0 結束（exit $exitCode）"
        return
    }
    Pass $ac
}

Write-Host "== Tier 1: plugin-init ==" -ForegroundColor Cyan

$before = $script:fails.Count
$null = Exec 'AC-L1' 'go build ./...' { go build ./... }
if ($script:fails.Count -eq $before) { Pass 'AC-L1/build' }
$before = $script:fails.Count
$null = Exec 'AC-L1' 'go vet ./...' { go vet ./... }
if ($script:fails.Count -eq $before) { Pass 'AC-L1/vet' }

# binary：先刪舊產物，避免 build 失敗時讀到上一次殘留的 binary（入邊 E2 探測要用到）。
$bin = Join-Path $repo 'bin/apm-go.exe'
Remove-Item $bin -Force -ErrorAction SilentlyContinue
$before = $script:fails.Count
$null = Exec 'AC-L1' "go build -o $bin" { go build -o $bin ./cmd/apm-go }
if (-not (Test-Path $bin)) { Fail 'AC-L1' "build 後 $bin 不存在" }
elseif ($script:fails.Count -eq $before) { Pass 'AC-L1/binary' }

# ================= 入邊載荷檢查（模式 4：邊沒被檢查 = 邊不存在） =================
Write-Host "-- 入邊載荷 --" -ForegroundColor Cyan

# E1: targets-init-shape → 有序 YAML 產生器
$initGo = Get-Content "$repo/cmd/apm-go/init.go" -Raw
if ($initGo -notmatch 'buildManifestNode') { Block 'E1' 'targets-init-shape 尚未提供 buildManifestNode（有序 YAML 產生器）' } else { Pass 'E1' }

# E2: targets-init-shape → SupportedTargets 統一為 6 元素且含 agent-skills
# 注意：不可只 grep target.go 有沒有 'agent-skills' —— CanonicalTargets 與 adapterTargets
# 本來就有這個字串，會造成假綠（本檔第一次執行時就踩到了）。
# 必須實測 CLI 行為 + 確認 promptTargetsOrdered 已從 init.go 消失。
$e2ok = $true
$e2probe = Join-Path $env:TEMP ("apm-e2-" + [guid]::NewGuid().ToString('N'))
New-Item -ItemType Directory -Path $e2probe -Force | Out-Null
try {
  Push-Location $e2probe
  & $bin init e2-probe --yes --target agent-skills 2>&1 | Out-Null
  if ($LASTEXITCODE -ne 0) { $e2ok = $false }
} finally { Pop-Location -EA SilentlyContinue; Remove-Item $e2probe -Recurse -Force -EA SilentlyContinue }
$initSrc = Get-Content "$repo/cmd/apm-go/init.go" -Raw
if ($initSrc -match 'promptTargetsOrdered') { $e2ok = $false }
if (-not $e2ok) {
  Block 'E2' 'targets-init-shape 尚未統一 target 集合（--target agent-skills 仍被拒 或 promptTargetsOrdered 仍在 init.go）'
} else { Pass 'E2' }

# E3: targets-init-shape → ux seam
$ia = Get-Content "$repo/internal/ux/interactive.go" -Raw
$seam = ($ia -match '(?m)^var\s+confirmWith\s*=') -and ($ia -match '(?m)^var\s+multiSelectWith\s*=') -and ($ia -match '(?m)^var\s+inputFormWith\s*=')
if (-not $seam) { Block 'E3' 'targets-init-shape 尚未建立 ux seam（AC41 需要它）' } else { Pass 'E3' }

# E4: install-dev → --dev 可用（只封鎖 AC46，不封鎖整個 child）
$devReady = ((& $bin install --help 2>&1 | Out-String) -match '--dev')
if (-not $devReady) { Write-Host "  WARN [E4] install --dev 尚未就緒 → 僅 AC46 被封鎖，其餘 AC 照驗" -ForegroundColor Yellow }

if ($script:blocked.Count -gt 0) {
  Write-Host ""
  Write-Host "BLOCKED — 上游未驗證，不得推進（避免父節點污染子節點）" -ForegroundColor Magenta
  $script:blocked | ForEach-Object { Write-Host "  $_" -ForegroundColor Magenta }
  Write-Host "先讓 07-29-targets-init-shape 的 verify.ps1 綠燈再回來。" -ForegroundColor Yellow
  Pop-Location; exit 2
}

# ---- 全套件測試：必須先於任何個別 AC 檢查，且必須驗 exit code ----
# 個別 AC 的 -run 檢查只覆蓋本 task 相關套件；其他套件轉紅時它們不會動，
# 這是唯一能擋住「本 task 綠但把別處弄壞了」的閘門。
$before = $script:fails.Count
$null = Exec 'AC-L1' 'go test ./... -count=1（全套件）' { go test ./... -count=1 }
if ($script:fails.Count -eq $before) { Pass 'AC-L1/go-test-all' }

# ================= 本 child 的 AC =================
Write-Host "-- 本 child AC --" -ForegroundColor Cyan

# AC30: plugin group 只有一個子指令
$ph = & $bin plugin --help 2>&1 | Out-String
if ($LASTEXITCODE -ne 0 -and $ph -notmatch 'init') { Fail 'AC30' '`apm-go plugin` 指令群不存在' }
else {
  $subs = @($ph -split "`n" | Where-Object { $_ -match '^\s{2,}[a-z][a-z-]*\s{2,}' } | ForEach-Object { ($_.Trim() -split '\s+')[0] } | Where-Object { $_ -ne 'help' -and $_ -ne 'completion' })
  if ($subs.Count -ne 1 -or $subs[0] -ne 'init') { Fail 'AC30' ("plugin 子指令應只有 init，實際：" + ($subs -join ',')) } else { Pass 'AC30' }
}

# AC8: plugin init 旗標集合
$ih = & $bin plugin init --help 2>&1 | Out-String
foreach ($f in @('--yes','--target','--verbose')) { if ($ih -notmatch [regex]::Escape($f)) { Fail 'AC8' "plugin init 缺旗標 $f" } }
if ($ih -match '--force') { Fail 'AC8' 'plugin init 不該有 --force（上游沒有）' }
if ($script:fails.Count -eq 0) { Pass 'AC8' }

# AC33: consumer init 不得獲得 --verbose（反向閘門）
$ch = & $bin init --help 2>&1 | Out-String
if ($ch -match '(?m)^\s+-v,\s*--verbose' -or $ch -match '--verbose') { Fail 'AC33' 'consumer `init --help` 出現 --verbose，plugin 旗標洩漏' } else { Pass 'AC33' }

$probe = Join-Path $env:TEMP ("apm-pi-" + [guid]::NewGuid().ToString('N'))
New-Item -ItemType Directory -Path $probe -Force | Out-Null
try {
  Push-Location $probe

  # AC9 / AC36: kebab-case 驗證與邊界
  & $bin plugin init My_Plugin --yes 2>&1 | Out-Null
  if ($LASTEXITCODE -eq 0) { Fail 'AC9' 'plugin init My_Plugin 應失敗' } else { Pass 'AC9/reject' }
  & $bin plugin init 1abc --yes 2>&1 | Out-Null
  if ($LASTEXITCODE -eq 0) { Fail 'AC36' 'plugin init 1abc（首字元非小寫字母）應失敗' } else { Pass 'AC36/first-char' }
  $n64 = 'a' * 64; $n65 = 'a' * 65
  & $bin plugin init $n65 --yes 2>&1 | Out-Null
  if ($LASTEXITCODE -eq 0) { Fail 'AC36' '長度 65 應被拒絕' } else { Pass 'AC36/65' }
  & $bin plugin init $n64 --yes 2>&1 | Out-Null
  if ($LASTEXITCODE -ne 0) { Fail 'AC36' '長度 64 應通過' } else { Pass 'AC36/64' }

  # AC32: consumer 不得被 kebab-case 污染（反向閘門）
  & $bin init My_Project --yes 2>&1 | Out-Null
  if ($LASTEXITCODE -ne 0) { Fail 'AC32' 'consumer `init My_Project` 應仍然成功（kebab-case 洩漏到 consumer）' } else { Pass 'AC32' }

  # AC37: consumer 既有拒絕仍在
  & $bin init 'a/b' --yes 2>&1 | Out-Null
  if ($LASTEXITCODE -eq 0) { Fail 'AC37' 'consumer 對 a/b 的既有拒絕消失了' } else { Pass 'AC37' }

  # AC10 / AC11 / AC12: 產物
  $d = Join-Path $probe 'ok-plugin'
  New-Item -ItemType Directory -Path $d -Force | Out-Null
  Push-Location $d
  & $bin plugin init my-plugin --yes --target claude 2>&1 | Out-Null
  $py = Join-Path $d 'my-plugin/apm.yml'; $pj = Join-Path $d 'my-plugin/plugin.json'
  if (-not (Test-Path $py)) { Fail 'AC10' 'plugin init 未產生 apm.yml' }
  else {
    $y = Get-Content $py -Raw
    if ($y -notmatch '(?m)^version:\s*0\.1\.0') { Fail 'AC10' '--yes 的 version 應為 0.1.0' } else { Pass 'AC10/version' }
    if ($y -notmatch '(?ms)devDependencies:') { Fail 'AC10' '缺 devDependencies' } else { Pass 'AC10/devDeps' }
    $ks = ($y -split "`n" | Where-Object { $_ -match '^[a-zA-Z]' } | ForEach-Object { ($_ -split ':')[0] })
    $ii=[array]::IndexOf($ks,'includes'); $id=[array]::IndexOf($ks,'devDependencies'); $is=[array]::IndexOf($ks,'scripts')
    if (-not ($ii -lt $id -and $id -lt $is)) { Fail 'AC10' "devDependencies 鍵序錯 includes=$ii dev=$id scripts=$is" } else { Pass 'AC10/order' }
  }
  if (-not (Test-Path $pj)) { Fail 'AC11' '未產生根目錄 plugin.json' }
  else {
    $raw = Get-Content $pj -Raw
    $j = $raw | ConvertFrom-Json
    if ($j.license -ne 'MIT') { Fail 'AC11' "plugin.json license 應為 MIT，實際 $($j.license)" }
    if ($null -eq $j.author -or $null -eq $j.author.name) { Fail 'AC11' 'plugin.json author 應為 {name:...} 物件' }
    if (-not $raw.EndsWith("`n")) { Fail 'AC11' 'plugin.json 結尾缺換行' }
    if ($raw -notmatch '(?m)^  "name"') { Fail 'AC11' 'plugin.json 應為 2 空格縮排' }
    if ($script:fails.Count -eq 0) { Pass 'AC11' }
  }

  # AC12 反向閘門：consumer 不得產生 plugin.json / devDependencies
  $d2 = Join-Path $probe 'consumer'
  New-Item -ItemType Directory -Path $d2 -Force | Out-Null
  Pop-Location; Push-Location $d2
  & $bin init c-probe --yes --target claude 2>&1 | Out-Null
  # 2026-07-30 round-4 修復：absence-only 斷言（「沒有 plugin.json / devDependencies」）
  # 先前沒驗 $LASTEXITCODE —— 若 init 指令本身失敗（例如殘留目錄衝突），c-probe/
  # 底下什麼檔案都不會產生，兩個「不存在」斷言會在指令根本沒跑成功的情況下雙雙
  # 假綠通過。exit code 非 0 時直接 Fail，不讓下面的 absence 檢查頂替它。
  if ($LASTEXITCODE -ne 0) {
    Fail 'AC12' "consumer \`init c-probe\` 本身失敗（exit $LASTEXITCODE），absence 斷言不成立"
  } else {
    if (Test-Path (Join-Path $d2 'c-probe/plugin.json')) { Fail 'AC12' 'consumer init 產生了 plugin.json' } else { Pass 'AC12/no-plugin-json' }
    $cy = Get-Content (Join-Path $d2 'c-probe/apm.yml') -Raw -EA SilentlyContinue
    if ($cy -and $cy -match 'devDependencies') { Fail 'AC12' 'consumer init 產生了 devDependencies' } else { Pass 'AC12/no-devDeps' }
  }

  # AC13 / AC46: Next Steps
  Pop-Location; Push-Location $d
  $ns = & $bin plugin init ns-probe --yes --target claude 2>&1 | Out-String
  if ($ns -notmatch 'Pack as plugin') { Fail 'AC13' 'Next Steps 缺 `Pack as plugin:`' }
  if ($ns -notmatch 'apm-go pack')    { Fail 'AC13' 'Next Steps 缺 `apm-go pack`' }
  if ($ns -match 'Install a package') { Fail 'AC13' 'Next Steps 混入 consumer 版的 `Install a package:` 提示' }
  if ($script:fails.Count -eq 0) { Pass 'AC13' }
  if ($devReady) {
    if ($ns -notmatch 'install --dev') { Fail 'AC46' 'E4 已就緒但 Next Steps 未印 `apm-go install --dev`' } else { Pass 'AC46' }
  } else {
    if ($ns -match 'install --dev') { Fail 'AC46' 'E4 未就緒卻印出跑不動的 `install --dev`（PRD 明文禁止）' } else { Write-Host "  skip [AC46] E4 未就緒；已確認未誤印" -ForegroundColor Yellow }
  }

  # AC14/15/16/34/39: plugin-native 根目錄警告
  Pop-Location
  foreach ($src in @('agents','skills','commands','instructions','extensions','hooks')) {
    $w = Join-Path $probe "warn-$src"; New-Item -ItemType Directory -Path (Join-Path $w $src) -Force | Out-Null
    Push-Location $w
    $o1 = & $bin init "w-$src" --yes --target claude 2>&1 | Out-String
    if ($o1 -notmatch 'plugin' -and $o1 -notmatch 'pack') { Fail 'AC39' "根目錄有 $src/ 卻未印 plugin-native 警告（init）" }
    $o2 = & $bin plugin init "wp-$src" --yes --target claude 2>&1 | Out-String
    if ($o2 -notmatch 'plugin' -and $o2 -notmatch 'pack') { Fail 'AC34' "根目錄有 $src/ 卻未印警告（plugin init）" }
    Pop-Location
  }
  $wj = Join-Path $probe 'warn-hooksjson'; New-Item -ItemType Directory -Path $wj -Force | Out-Null
  '{}' | Set-Content (Join-Path $wj 'hooks.json')
  Push-Location $wj
  $o3 = & $bin init w-hj --yes --target claude 2>&1 | Out-String
  if ($o3 -notmatch 'plugin' -and $o3 -notmatch 'pack') { Fail 'AC39' 'hooks.json 未觸發警告' }
  # AC15: 有 .apm/ 時警告消失
  New-Item -ItemType Directory -Path (Join-Path $wj '.apm') -Force | Out-Null
  $o4 = & $bin init w-hj2 --yes --target claude 2>&1 | Out-String
  # 2026-07-30 round-4：同 AC12 的 absence-only 缺口 —— 若 init 本身失敗，
  # 錯誤輸出通常也不含 'plugin-native'，先前會被誤判成「警告消失」的正確案例。
  if ($LASTEXITCODE -ne 0) { Fail 'AC15' "init w-hj2 本身失敗（exit $LASTEXITCODE），absence 斷言不成立" }
  elseif ($o4 -match 'plugin-native') { Fail 'AC15' '有 .apm/ 時警告仍出現' }
  Pop-Location
  if ($script:fails.Count -eq 0) { Pass 'AC14/15/34/39' }
} finally { Pop-Location -EA SilentlyContinue; Remove-Item $probe -Recurse -Force -EA SilentlyContinue }

# AC41: 三個互動分支「各有獨立斷言」—— 模式 9：逐一驗，不是總數 >= 3 就算
# （總數判斷會被三個 MultiSelect 測試滿足，Form/Confirm 一個都沒有也照樣過）
# 2026-07-30：原本這條只用 `-list` 確認「有名稱匹配的測試存在」，從不執行它。
# 一個整支 t.Skip() 或什麼都不斷言的測試同樣能滿足它 —— 這正是
# marketplace-add-fixes 第五輪稽核抓到的同一種假綠（13 條閘門受影響）。
# 改為存在性檢查通過後，用 ExecTestJSON 實跑並要求 Action=pass、不得有 skip。
$branchMissing = @()
foreach ($b in @('Form','MultiSelect','Confirm')) {
  $m = @(& go test ./cmd/apm-go/ -list $b 2>&1 | Where-Object { $_ -match '^Test' })
  if ($m.Count -eq 0) { $branchMissing += $b }
}
if ($branchMissing.Count -gt 0) {
  Fail 'AC41' ("互動分支缺獨立斷言：" + ($branchMissing -join '、') + "（不可用總數 >= 3 代替逐一）")
} else {
  $before41 = $script:fails.Count
  foreach ($b in @('Form','MultiSelect','Confirm')) {
    ExecTestJSON 'AC41' "go test -json -run $b" './cmd/apm-go/' $b
  }
  if ($script:fails.Count -eq $before41) { Pass 'AC41' }
}

# AC38: 互動模式下 plugin 與 consumer 的版本表單預設值「皆為」1.0.0
# 模式 9：宣稱是「兩個模式皆」，必須兩邊各驗一次。原本 verify.ps1 完全沒有這條 ——
# 由全稱量詞掃描抓到（PRD 有 AC38，閘門卻是 0 處檢查）。
# 2026-07-30：與 AC41 同一個修法 —— 原本只 `-list` 不執行。
$acc = @()
foreach ($mode in @('Init','PluginInit')) {
  $m = @(& go test ./cmd/apm-go/ -list "${mode}.*InteractiveVersionDefault|${mode}.*VersionDefault" 2>&1 | Where-Object { $_ -match '^Test' })
  if ($m.Count -eq 0) { $acc += $mode }
}
if ($acc.Count -gt 0) {
  Fail 'AC38' ("互動模式版本預設值 1.0.0 缺測試的模式：" + ($acc -join '、'))
} else {
  $before38 = $script:fails.Count
  foreach ($mode in @('Init','PluginInit')) {
    ExecTestJSON 'AC38' "go test -json -run ${mode}VersionDefault" './cmd/apm-go/' "${mode}.*InteractiveVersionDefault|${mode}.*VersionDefault"
  }
  if ($script:fails.Count -eq $before38) { Pass 'AC38' }
}

# AC52: plugin init 走 clack 互動路徑，呼叫序列與 consumer init 相同（D12）
# 這條不能只 grep 原始碼有沒有 ux.NewClack —— 那會被「有引用但沒走到」滿足。
# 必須有一個記錄呼叫序列並比對兩模式的測試。
$seqTests = @(& go test ./cmd/apm-go/ -list 'ClackSequence|InteractiveParity|ClackParity' 2>&1 | Where-Object { $_ -match '^Test' })
if ($seqTests.Count -eq 0) {
  Fail 'AC52' '-list 零匹配：找不到「clack 呼叫序列 plugin init vs init 一致」的測試'
} else {
  # 2026-07-30：改用 ExecTestJSON（移植自 marketplace-add-fixes），不再只驗
  # exit code -- 原本的手動 -run + $LASTEXITCODE 檢查一樣會被「每個匹配到的
  # 測試全部 t.Skip」騙過（Go 仍回傳 exit 0），見該函式定義處註解。
  ExecTestJSON 'AC52/test' "go test -json -run 'ClackSequence|InteractiveParity|ClackParity'" './cmd/apm-go/' 'ClackSequence|InteractiveParity|ClackParity'
}
# 輔助（非充分）：pluginInitCmd 必須實際走到共用本體，不得自成一套非互動流程
$pluginSrc = Get-ChildItem "$repo/cmd/apm-go" -Filter 'plugin*.go' -EA SilentlyContinue |
             Where-Object { $_.Name -notmatch '_test\.go$' } | ForEach-Object { Get-Content $_.FullName -Raw }
if ($pluginSrc) {
  $joined = $pluginSrc -join "`n"
  if ($joined -notmatch 'runInit|initMode|consumerMode|pluginMode') {
    Fail 'AC52' 'plugin 指令未走共用本體（找不到 runInit/initMode）—— 可能自成一套流程'
  } else { Pass 'AC52/shared-body' }
}
Write-Host "  NOTE [AC52] 「改寫成獨立非互動指令會轉紅」屬 Tier 2 反向驗證，本閘門不代替" -ForegroundColor Yellow

# AC23 / AC-L2: 未新增相依
# 2026-07-30：改為對照本分支的 base commit（3e450dd，`git merge-base HEAD main`
# 實測得出），而非只驗工作樹/暫存區 diff -- 在一棵乾淨的已 commit 樹上，工作樹
# 和暫存區 diff 都是空的，commit 之後才新增的一行 require 完全看不到（MAJOR 4，
# 外部稽核第四輪，見 07-29-install-dev/verify.ps1 同段註解的完整推導）。
# `git diff <base> -- go.mod go.sum` 對單一 ref 是拿該 ref 與目前工作目錄比較，
# 未 commit 的部分也算在內，同時涵蓋已 commit 與尚未 commit 的變更。
$taskBase = '3e450dd'
$d1 = & git diff $taskBase -- go.mod go.sum 2>&1; $d1Exit = $LASTEXITCODE
if ($d1Exit -ne 0) {
  Fail 'AC23' "git diff $taskBase -- go.mod go.sum（exit $d1Exit）本身失敗，無法判定是否新增相依`n      $($d1 -join "``n      ")"
} else {
  $newReq = @($d1) | Where-Object { $_ -match '^\+\s+\S+\s+v' }
  if ($newReq) { Fail 'AC23' ("go.mod/go.sum 相對 task base（$taskBase）新增 require：" + ($newReq -join '; ')) } else { Pass 'AC23/no-new-deps' }
}

# 覆蓋率：唯一檔名寫在 repo 內、驗 exit code、用完刪除。
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
