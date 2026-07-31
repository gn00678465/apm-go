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
    param([string]$ac, [string]$what, [string]$pkg, [string]$pattern, [string[]]$allowSkip = @(), [int]$minCount = 0, [string]$tags = '', [string[]]$requireTests = @())
    # A-MINOR-1（外部稽核第七輪，2026-07-31）：新增 $tags 參數，讓呼叫端能對
    # 走 apm_test_hooks build tag 隔離的測試（TestInitVsPluginInit_
    # ClackSequenceParity）實際加上 -tags -- 先前只在 $what 這個純文字說明
    # 參數裡寫「go test -tags ...」，從沒真的傳給下面這行實際執行的指令，
    # 是文字說明跟實際行為脫節的假閘門（本檔自查時發現，未經外部稽核點名）。
    if ($tags) {
        $out = & go test -tags $tags -json -run $pattern $pkg 2>&1
    } else {
        $out = & go test -json -run $pattern $pkg 2>&1
    }
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
    # 2026-07-31 第五輪 A-MAJOR-2：-minCount 只鎖數量、pattern 只鎖存在，都抓不到
    # 「刪掉指定測試但同 pattern 還有別的測試」。-requireTests 逐名要求出現且 pass
    # （exact match、大小寫敏感；skip 不算，因此優先於 -allowSkip）。
    $missingRequired = @($requireTests | Where-Object { -not $seen.ContainsKey($_) -or $seen[$_] -ne 'pass' })
    if ($missingRequired.Count -gt 0) {
        Fail $ac "${what}: 下列身份鎖定的測試未出現或未 pass（改名/刪除/跳過皆轉紅）: $($missingRequired -join ' ; ')"
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
#
# A-MINOR-1（外部稽核第六輪，2026-07-31）：舊版只驗「有沒有包含
# {--yes,--target,--verbose}」+「沒有 --force」，這是子集檢查，不是全集
# 檢查——多加一個從沒設想過的 --extra-foo 旗標一樣會通過，因為閘門從不檢查
# 「僅有」這幾個。改成從 --help 的 Flags: 區塊逐行解析出實際的長旗標名稱
# 集合，和預期集合做嚴格相等比較（多一個少一個都轉紅），不再只挑幾個名字
# 個別 grep。
$ih = & $bin plugin init --help 2>&1 | Out-String
$ihFlagsSection = ($ih -split '(?m)^Flags:')[-1]
$actualFlags = @([regex]::Matches($ihFlagsSection, '(?m)^\s*(?:-\w,\s*)?--([A-Za-z][\w-]*)') |
  ForEach-Object { $_.Groups[1].Value } | Sort-Object -Unique)
$wantFlags = @('help', 'target', 'verbose', 'yes') | Sort-Object
if (($actualFlags -join ',') -ne ($wantFlags -join ',')) {
  Fail 'AC8' "plugin init --help 旗標集合 = [$($actualFlags -join ', ')]，want 恰好 [$($wantFlags -join ', ')]（多或少一個旗標都算失敗）"
} else { Pass 'AC8' }

# AC33: consumer init 不得獲得 --verbose（反向閘門）
$ch = & $bin init --help 2>&1 | Out-String
if ($ch -match '(?m)^\s+-v,\s*--verbose' -or $ch -match '--verbose') { Fail 'AC33' 'consumer `init --help` 出現 --verbose，plugin 旗標洩漏' } else { Pass 'AC33' }

# ---- AC31（外部稽核第九輪，2026-07-31）：先前完全沒有閘門 ----
# PRD:73／init.go:137 的 `else if mode.plugin` 分支：`plugin init --yes` 省略
# PROJECT-NAME 時，取當前目錄的 basename 並過 kebab-case 驗證。合法目錄名應
# 成功且 apm.yml 的 name 等於目錄名；非法目錄名應失敗且錯誤訊息是名稱驗證
# （不是其他原因，例如目錄建立失敗）。
$acc31 = Join-Path $env:TEMP ("apm-ac31-" + [guid]::NewGuid().ToString('N'))
New-Item -ItemType Directory -Path $acc31 -Force | Out-Null
try {
  # (a) 合法 kebab-case 目錄名：省略參數應成功，apm.yml 的 name 應等於目錄名
  $goodDir = Join-Path $acc31 'my-good-plugin'
  New-Item -ItemType Directory -Path $goodDir -Force | Out-Null
  Push-Location $goodDir
  & $bin plugin init --yes --target claude 2>&1 | Out-Null
  $exit31a = $LASTEXITCODE
  Pop-Location
  if ($exit31a -ne 0) {
    Fail 'AC31' "合法目錄名 my-good-plugin 下省略 PROJECT-NAME 應成功，實際 exit $exit31a"
  } else {
    $y31 = Get-Content (Join-Path $goodDir 'apm.yml') -Raw -EA SilentlyContinue
    if (-not $y31 -or $y31 -notmatch '(?m)^name:\s*my-good-plugin\s*$') {
      Fail 'AC31' "apm.yml 的 name 應等於目錄名 my-good-plugin，實際內容：$y31"
    } else { Pass 'AC31/valid-dirname-succeeds' }
  }

  # (b) 非法目錄名（Bad_Dir）：省略參數應失敗，且錯誤是名稱驗證本身
  $badDir = Join-Path $acc31 'Bad_Dir'
  New-Item -ItemType Directory -Path $badDir -Force | Out-Null
  Push-Location $badDir
  $out31b = & $bin plugin init --yes --target claude 2>&1 | Out-String
  $exit31b = $LASTEXITCODE
  Pop-Location
  if ($exit31b -eq 0) {
    Fail 'AC31' "非法目錄名 Bad_Dir 下省略 PROJECT-NAME 應失敗，實際成功"
  } elseif ($out31b -notmatch 'invalid plugin name') {
    Fail 'AC31' "非法目錄名 Bad_Dir 失敗了，但錯誤不是名稱驗證（可能是其他原因）：$($out31b.Trim())"
  } else { Pass 'AC31/invalid-dirname-rejected' }
} finally { Remove-Item $acc31 -Recurse -Force -EA SilentlyContinue }

$probe = Join-Path $env:TEMP ("apm-pi-" + [guid]::NewGuid().ToString('N'))
New-Item -ItemType Directory -Path $probe -Force | Out-Null
try {
  Push-Location $probe

  # AC9 / AC36: kebab-case 驗證與邊界
  & $bin plugin init My_Plugin --yes 2>&1 | Out-Null
  if ($LASTEXITCODE -eq 0) { Fail 'AC9' 'plugin init My_Plugin 應失敗' } else { Pass 'AC9/reject' }
  & $bin plugin init 1abc --yes 2>&1 | Out-Null
  if ($LASTEXITCODE -eq 0) { Fail 'AC36' 'plugin init 1abc（首字元非小寫字母）應失敗' } else { Pass 'AC36/first-char' }
  # 2026-07-31 主 session mutation 自查：regex 首字元類放寬成 [a-zA-Z] 時，
  # My_Plugin（底線）與 1abc（數字）兩探針都仍然拒絕 → 閘門假綠。補上
  # 「大寫開頭 + 合法尾巴」與「連字號開頭」兩個恰好落在字元類邊界上的探針。
  & $bin plugin init Abc --yes 2>&1 | Out-Null
  if ($LASTEXITCODE -eq 0) { Fail 'AC36' 'plugin init Abc（大寫開頭、尾巴合法）應失敗' } else { Pass 'AC36/upper-first' }
  # 2026-07-31 外部稽核第三輪 A-BLOCKING-1（主 session 自查確認）：這條探針
  # 原本寫成 `plugin init -- -abc --yes`——`--` 之後的 `--yes` 也被當成位置
  # 參數，cobra 因 `accepts at most 1 arg(s), received 2` 先失敗，於是不論
  # 名稱驗證是否被放寬，探針都會「非 0 = 通過」，是假綠。旗標必須寫在 `--`
  # 之前，讓 `-abc` 是唯一的位置參數，錯誤才會來自 pluginValidateName。
  $hyphenOut = & $bin plugin init --yes -- -abc 2>&1 | Out-String
  if ($LASTEXITCODE -eq 0) { Fail 'AC36' 'plugin init -abc（連字號開頭）應失敗' }
  elseif ($hyphenOut -notmatch 'invalid plugin name') {
    Fail 'AC36' "plugin init -abc 失敗了，但錯誤不是名稱驗證（可能又是 arity/旗標解析假綠）：$($hyphenOut.Trim())"
  } else { Pass 'AC36/hyphen-first' }
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
  if ($LASTEXITCODE -eq 0) { Fail 'AC37' 'consumer 對 a/b 的既有拒絕消失了' } else { Pass 'AC37/forward-slash' }
  # A-BLOCKING-1（外部稽核第七輪，2026-07-31）：本閘門先前只探過 `a/b`
  # （正斜線），從沒探過 `a\b`（反斜線）——consumerValidateName（init.go）
  # 現況是 `strings.ContainsAny(pn, "/\\")`，兩種分隔符號都擋，但若未來
  # 有人把它「簡化」成只驗 `strings.ContainsAny(pn, "/")`（少了反斜線），
  # 這條閘門過去完全驗不到，因為根本沒有一個探針會用到反斜線。補上獨立探針。
  & $bin init 'a\b' --yes 2>&1 | Out-Null
  if ($LASTEXITCODE -eq 0) { Fail 'AC37' 'consumer 對 a\b（反斜線）的既有拒絕消失了' } else { Pass 'AC37/backslash' }
  # A-BLOCKING-4（外部稽核第九輪，2026-07-31）：本檔先前只探過 a/b、a\b 兩個
  # 分隔符號案例，從未探過 consumerValidateName 拒絕的另一半條件 `pn == ".."`
  # 本身 -- 只靠 main_test.go:506 的既有單元測試涵蓋，本閘門測不到它是否仍被
  # 拒絕。這裡補上端對端探針；下方另有 ExecTestJSON 對該單元測試本身做身份
  # 鎖定。
  & $bin init '..' --yes 2>&1 | Out-Null
  if ($LASTEXITCODE -eq 0) { Fail 'AC37' 'consumer 對 ".."（字面兩個點）的既有拒絕消失了' } else { Pass 'AC37/dotdot' }

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
    # A-BLOCKING-3（外部稽核第九輪，2026-07-31）：先前只驗 license、author
    # 是否「存在」、結尾換行與縮排，name/version/description 的實際值從未
    # 比對過 -- 例如把三者的值互相接錯（name 寫成 description 的內容）一樣
    # 會通過。這裡逐欄位比對成 CLI 引數（my-plugin/0.1.0）與衍生預設值
    # （"APM project for my-plugin"）算出的期望值。author 例外：
    # manifest.DetectAuthor() 讀本機 git config user.name，機器間不同，不能
    # 寫死期望字串，維持既有的「存在且為 {name:...} 物件」檢查。
    if ($j.name -ne 'my-plugin') { Fail 'AC11' "plugin.json name 應為 my-plugin，實際 $($j.name)" }
    if ($j.version -ne '0.1.0') { Fail 'AC11' "plugin.json version 應為 0.1.0，實際 $($j.version)" }
    if ($j.description -ne 'APM project for my-plugin') { Fail 'AC11' "plugin.json description 應為 'APM project for my-plugin'，實際 $($j.description)" }
    if ($j.license -ne 'MIT') { Fail 'AC11' "plugin.json license 應為 MIT，實際 $($j.license)" }
    if ($null -eq $j.author -or $null -eq $j.author.name) { Fail 'AC11' 'plugin.json author 應為 {name:...} 物件' }
    if (-not $raw.EndsWith("`n")) { Fail 'AC11' 'plugin.json 結尾缺換行' }
    if ($raw -notmatch '(?m)^  "name"') { Fail 'AC11' 'plugin.json 應為 2 空格縮排' }
    if ($script:fails.Count -eq 0) { Pass 'AC11/e2e-fields' }
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

  # A-MINOR-1（外部稽核第六輪，2026-07-31）：上面 AC13/AC46 只驗「有沒有含
  # 指定文字」，從不驗「Next steps 底下恰好是這幾行、順序如此」——多印一行
  # (例如意外重複、或混入第三行提示) 不會被抓到，只要原本兩行還在。這裡逐行
  # 解析「Next steps」標題之後（跳過空行）的內容，與預期的兩行做完整字串
  # 相等比較（前提 E4 已就緒；E4 未就緒時只有 Pack as plugin 一行，已由上面
  # 的 absence 檢查涵蓋，不重複驗證這個分支的行數）。
  if ($devReady) {
    $nsLines = @($ns -replace "`r", '' -split "`n")
    $headerIdx = [Array]::IndexOf($nsLines, 'Next steps')
    if ($headerIdx -lt 0) {
      Fail 'AC46' "Next Steps 找不到 `Next steps` 標題行；完整輸出：$ns"
    } else {
      $rest = @($nsLines[($headerIdx + 1)..($nsLines.Length - 1)] | Where-Object { $_ -ne '' })
      $wantLines = @(
        ' i Install a dev dependency:  apm-go install --dev <owner>/<repo>',
        ' i Pack as plugin:  apm-go pack'
      )
      if (($rest -join "`n") -ne ($wantLines -join "`n")) {
        Fail 'AC46' "Next Steps 逐行內容不符：`n實際: $($rest -join ' | ')`n預期: $($wantLines -join ' | ')"
      } else { Pass 'AC46/exact-lines' }
    }
  }

  # AC14/15/16/34/39: plugin-native 根目錄警告
  Pop-Location
  foreach ($src in @('agents','skills','commands','instructions','extensions','hooks')) {
    $w = Join-Path $probe "warn-$src"; New-Item -ItemType Directory -Path (Join-Path $w $src) -Force | Out-Null
    Push-Location $w
    # A-BLOCKING-2（外部稽核第九輪，2026-07-31）：原本判斷「輸出同時不含
    # 'plugin' 與 'pack' 才失敗」對 AC34 是假綠 -- plugin init 本身的成功
    # 訊息就含 "APM plugin initialized successfully!"，Next Steps 也固定印
    # "Pack as plugin: apm-go pack"，兩者都不必依賴這條警告就會出現，所以
    # 把 detectPluginNativeRoot 的警告整段拿掉、只限定 consumer 模式
    # （`!mode.plugin`）閘門仍然全線通過。改用警告訊息本身獨有的片語
    # "plugin-native"（init.go:172 `Found plugin-native content (...)`），
    # 與 AC15/AC16 既有的斷言用同一個唯一片語，不與正常成功輸出的任何字串
    # 重疊。
    # 2026-07-31 外部稽核第五輪 A-BLOCKING-1：這兩條原本只比對訊息片語，
    # **從不檢查 exit code**——AC14 明文要求「警告不阻斷、exit 0」，若警告
    # 分支印完訊息後 return 一個錯誤，訊息斷言照樣通過、閘門全綠，但真
    # binary 已經 exit 1。訊息與 exit code 兩者都要驗。
    $o1 = & $bin init "w-$src" --yes --target claude 2>&1 | Out-String
    $e1 = $LASTEXITCODE
    if ($o1 -notmatch 'plugin-native') { Fail 'AC39' "根目錄有 $src/ 卻未印 plugin-native 警告（init）" }
    if ($e1 -ne 0) { Fail 'AC14' "init 在 $src/ 警告情境下 exit $e1（AC14 要求警告不阻斷、exit 0）" }
    $o2 = & $bin plugin init "wp-$src" --yes --target claude 2>&1 | Out-String
    $e2 = $LASTEXITCODE
    if ($o2 -notmatch 'plugin-native') { Fail 'AC34' "根目錄有 $src/ 卻未印 plugin-native 警告（plugin init）" }
    if ($e2 -ne 0) { Fail 'AC14' "plugin init 在 $src/ 警告情境下 exit $e2（AC14 要求警告不阻斷、exit 0）" }
    Pop-Location
  }
  $wj = Join-Path $probe 'warn-hooksjson'; New-Item -ItemType Directory -Path $wj -Force | Out-Null
  '{}' | Set-Content (Join-Path $wj 'hooks.json')
  Push-Location $wj
  $o3 = & $bin init w-hj --yes --target claude 2>&1 | Out-String
  if ($o3 -notmatch 'plugin' -and $o3 -notmatch 'pack') { Fail 'AC39' 'hooks.json 未觸發警告' }
  # A-BLOCKING-1（外部稽核第十輪，2026-07-31）：這裡先前只驗過 `init`（consumer
  # 模式）在「唯一來源是 hooks.json」情境下的警告，從沒驗過 `plugin init`
  # 同一情境——兩者共用同一個 runInitCore/detectPluginNativeRoot 呼叫點，但
  # 假設呼叫點被加上一個只排除 hooks.json-only 且 mode.plugin 的分支
  # （例如 `if mode.plugin && len(sources) == 1 && sources[0] == "hooks.json"
  # { sources = nil }`），這裡完全不會發現。補上 plugin init 的對照探針；
  # 對應的 Go 單元回歸見 pluginwarn_test.go 的
  # TestPluginInitCmd_HooksJSONOnly_StillWarns（已用同一個突變驗證過會轉紅）。
  $o3p = & $bin plugin init wp-hj --yes --target claude 2>&1 | Out-String
  if ($o3p -notmatch 'plugin-native') { Fail 'AC39' 'hooks.json 未觸發警告（plugin init）' }
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

# ---- A-BLOCKING-1 身份鎖定（外部稽核第十輪，2026-07-31）----
# 上面的 plugin init + hooks.json-only 探針只驗端對端輸出；
# TestPluginInitCmd_HooksJSONOnly_StillWarns（pluginwarn_test.go）是唯一
# 從 runInitCore 呼叫點層級（而非 detectPluginNativeRoot 本體）驗證這件事的
# 單元測試，用 -requireTests 身份鎖定，防止它被改名/刪除/替換成不斷言的
# 空測試而不被本閘門發現。
ExecTestJSON 'A-BLOCKING-1/hooksjson-pluginmode' "go test -json -run 'TestPluginInitCmd_HooksJSONOnly_StillWarns'" './cmd/apm-go/' 'TestPluginInitCmd_HooksJSONOnly_StillWarns' -requireTests @('TestPluginInitCmd_HooksJSONOnly_StillWarns')

# ---- AC11 身份鎖定（外部稽核第九輪 A-BLOCKING-3，2026-07-31）----
# 上面 AC11/e2e-fields 只驗端對端 CLI 跑出來的一組固定引數；
# internal/pluginjson.TestScaffold_MatchesUpstreamGolden 是唯一逐位元組比對
# 真正上游 golden fixture（testdata/upstream-plugin-init.golden.json）的
# 測試 -- 驗的是完整欄位順序、2 空格縮排、結尾換行同時成立，不只是值本身。
# 用 -requireTests 身份鎖定，防止這個測試被改名/刪除/替換成同名但不比對
# golden 的空測試而不被本閘門發現。
ExecTestJSON 'AC11/golden' "go test -json -run 'TestScaffold_MatchesUpstreamGolden'" './internal/pluginjson/' 'TestScaffold_MatchesUpstreamGolden' -requireTests @('TestScaffold_MatchesUpstreamGolden')

# ---- AC37 身份鎖定（外部稽核第九輪 A-BLOCKING-4，2026-07-31）----
# 上面 AC37/dotdot 只驗端對端 exit code；main_test.go:506 的
# TestInitCmd_ProjectNameWithDotDotRejected 是既有的單元測試，用
# -requireTests 身份鎖定，防止它被改名/刪除/替換成不斷言的空測試。
ExecTestJSON 'AC37/dotdot-unit' "go test -json -run 'TestInitCmd_ProjectNameWithDotDotRejected'" './cmd/apm-go/' 'TestInitCmd_ProjectNameWithDotDotRejected' -requireTests @('TestInitCmd_ProjectNameWithDotDotRejected')

# ---- AC16（外部稽核第六輪 A-BLOCKING-1，2026-07-31）：專門的 symlink fixture
# ---- 端對端驗證，先前只暗示透過既有測試涵蓋，這裡新增明確斷言 ----
# 用真的 mklink（目錄用 /J junction，不需要特權；檔案用 /H hardlink，同樣
# 不需要特權），不得因為建立失敗就靜默 skip -- 建立失敗時直接 Fail，因為這
# 代表這個環境完全沒有驗到 AC16。
$acc16 = Join-Path $env:TEMP ("apm-ac16-" + [guid]::NewGuid().ToString('N'))
New-Item -ItemType Directory -Path $acc16 -Force | Out-Null
try {
  # (a) skills 是 junction 指到專案外真實目錄 -> 不觸發警告
  $realSkills = Join-Path $acc16 'real-skills'; New-Item -ItemType Directory -Path $realSkills -Force | Out-Null
  $projA = Join-Path $acc16 'proj-a'; New-Item -ItemType Directory -Path $projA -Force | Out-Null
  $linkA = Join-Path $projA 'skills'
  $mklinkA = & cmd /c mklink /J $linkA $realSkills 2>&1
  if ($LASTEXITCODE -ne 0) {
    Fail 'AC16' "無法建立 skills 的 junction 驗證 AC16（$mklinkA）"
  } else {
    Push-Location $projA
    $oa = & $bin init ac16-a --yes --target claude 2>&1 | Out-String
    $exitA = $LASTEXITCODE
    Pop-Location
    # A-MAJOR-1（外部稽核第十輪，2026-07-31）：這三個分支先前只驗輸出內容
    # （match/notmatch 'plugin-native'），從未檢查 exit code -- 若 init 本身
    # 因為其他原因失敗（例如殘留目錄衝突），輸出可能恰好不含 'plugin-native'
    # 字串，會被誤判成「AC16 通過（正確地不觸發警告）」，但其實整個命令
    # 已經以非 0 結束，AC16 從未被真正驗到。
    if ($exitA -ne 0) { Fail 'AC16' "init ac16-a（skills junction 情境）exit $exitA，AC16 未被真正驗到" }
    elseif ($oa -match 'plugin-native') { Fail 'AC16' "symlink 的 skills/（junction）不該觸發警告，實際輸出：$oa" } else { Pass 'AC16/skills-junction' }
  }

  # (b) .apm 是 junction 指到專案外真實目錄，且根目錄有 real skills/ -> 仍不觸發警告
  $realApm = Join-Path $acc16 'real-apm'; New-Item -ItemType Directory -Path $realApm -Force | Out-Null
  $projB = Join-Path $acc16 'proj-b'; New-Item -ItemType Directory -Path (Join-Path $projB 'skills') -Force | Out-Null
  $linkB = Join-Path $projB '.apm'
  $mklinkB = & cmd /c mklink /J $linkB $realApm 2>&1
  if ($LASTEXITCODE -ne 0) {
    Fail 'AC16' "無法建立 .apm 的 junction 驗證 AC16（$mklinkB）"
  } else {
    Push-Location $projB
    $ob = & $bin init ac16-b --yes --target claude 2>&1 | Out-String
    $exitB = $LASTEXITCODE
    Pop-Location
    if ($exitB -ne 0) { Fail 'AC16' "init ac16-b（.apm junction 情境）exit $exitB，AC16 未被真正驗到" }
    elseif ($ob -match 'plugin-native') { Fail 'AC16' "symlink 的 .apm/（junction）應短路整個檢查，實際輸出：$ob" } else { Pass 'AC16/apm-junction' }
  }

  # (c) hooks.json 是 hardlink 指到專案外真實檔案 -> 仍觸發警告（A-BLOCKING-1：
  # hooks.json 沒有 PRD 的 symlink 排除，跟 skills/ 不同）
  $realHooks = Join-Path $acc16 'real-hooks.json'
  '{}' | Set-Content $realHooks
  $projC = Join-Path $acc16 'proj-c'; New-Item -ItemType Directory -Path $projC -Force | Out-Null
  $linkC = Join-Path $projC 'hooks.json'
  $mklinkC = & cmd /c mklink /H $linkC $realHooks 2>&1
  if ($LASTEXITCODE -ne 0) {
    Fail 'AC16' "無法建立 hooks.json 的 hardlink 驗證 A-BLOCKING-1（$mklinkC）"
  } else {
    Push-Location $projC
    $oc = & $bin init ac16-c --yes --target claude 2>&1 | Out-String
    $exitC = $LASTEXITCODE
    Pop-Location
    if ($exitC -ne 0) { Fail 'AC16' "init ac16-c（hooks.json hardlink 情境）exit $exitC（警告不該阻斷，AC14 要求 exit 0）" }
    elseif ($oc -notmatch 'plugin-native') { Fail 'AC16' "symlink/hardlink 的 hooks.json 應觸發警告（A-BLOCKING-1），實際輸出：$oc" } else { Pass 'AC16/hooksjson-hardlink' }
  }
} finally { Remove-Item $acc16 -Recurse -Force -EA SilentlyContinue }

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

# 2026-07-31 外部稽核第五輪 A-MAJOR-2：A-MAJOR-1（互動表單名稱驗證）的三個
# 回歸測試只被上面那個廣泛的 `Form` pattern 涵蓋 —— 把三個測試全刪掉，既有的
# TestPluginInitInteractive_Form_DefaultsMatchModeAndPrefill 仍然滿足 `Form`
# pattern，AC41 照樣綠，這條修正會從閘門上完全消失。逐名身份鎖定。
ExecTestJSON 'A-MAJOR-1/form-name-validation' "go test -json -run 'Form_(InvalidNameRejected|ConsumerName)'" './cmd/apm-go/' 'TestPluginInitInteractive_Form_InvalidNameRejected|TestInitInteractive_Form_ConsumerName' -requireTests @(
  'TestPluginInitInteractive_Form_InvalidNameRejected',
  'TestInitInteractive_Form_ConsumerNameStillAcceptsUnderscore',
  'TestInitInteractive_Form_ConsumerNameRejectsPathSeparator'
)

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
#
# A-MINOR-1（外部稽核第七輪，2026-07-31）：TestInitVsPluginInit_
# ClackSequenceParity 現在活在一個 apm_test_hooks build-tag 隔離的檔案
# （plugin_init_clacksequence_test.go）——因為它依賴的
# ux.SetClackEventHookForTest 本身也移到同一個 tag 之後（同稽核輪的
# A-MINOR-1 修復：release binary 不該內含這個測試專用掛勾點）。不加
# -tags apm_test_hooks 時這條測試根本不存在於編譯單元裡（-list 零匹配，
# 不是被跳過），所以這裡的 -list/-run 都要加上這個 tag，否則會誤判成
# 「測試消失了」而非「測試被有意隔離」。
$seqTests = @(& go test -tags apm_test_hooks ./cmd/apm-go/ -list 'ClackSequence|InteractiveParity|ClackParity' 2>&1 | Where-Object { $_ -match '^Test' })
if ($seqTests.Count -eq 0) {
  Fail 'AC52' '-list 零匹配（-tags apm_test_hooks）：找不到「clack 呼叫序列 plugin init vs init 一致」的測試'
} else {
  # 2026-07-30：改用 ExecTestJSON（移植自 marketplace-add-fixes），不再只驗
  # exit code -- 原本的手動 -run + $LASTEXITCODE 檢查一樣會被「每個匹配到的
  # 測試全部 t.Skip」騙過（Go 仍回傳 exit 0），見該函式定義處註解。
  ExecTestJSON 'AC52/test' "go test -tags apm_test_hooks -json -run 'ClackSequence|InteractiveParity|ClackParity'" './cmd/apm-go/' 'ClackSequence|InteractiveParity|ClackParity' -tags 'apm_test_hooks'
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
  # 2026-07-31 外部稽核第五輪 A-MAJOR-1：舊 regex `^\+\s+\S+\s+v` 只抓
  # require ( ... ) 區塊內「+ 後有縮排」的行——合法的單行
  # `+require module vX` 與 go.sum 的 `+module vX h1:...` 都是 `+` 後直接
  # 接文字，完全不匹配。B 部（marketplace-add-fixes）早已修正同一缺陷，
  # A 部沒同步，本輪補上（三形態，與 B 部同一組判斷）。
  $newReq = @($d1) | Where-Object {
    $_ -match '^\+\s+\S+\s+v\d' -or          # require ( ... ) 區塊內
    $_ -match '^\+require\s+\S+\s+v\d' -or   # 單行 require directive
    $_ -match '^\+\S+/\S+\s+v\d.*\bh1:'      # go.sum 新增行
  }
  if ($newReq) { Fail 'AC23' ("go.mod/go.sum 相對 task base（$taskBase）新增 require：" + ($newReq -join '; ')) } else { Pass 'AC23/no-new-deps' }
}

# ---- A-MINOR-1（外部稽核第七輪，2026-07-31）：ux.SetClackEventHookForTest
# 改用 apm_test_hooks build tag 隔離，驗兩件事：(1) release binary（未加
# -tags，同 AGENTS.md 記載的 build 指令）確實不含該符號；(2) 加上
# -tags apm_test_hooks 後 AC52（TestInitVsPluginInit_ClackSequenceParity）
# 仍然存在且通過 -- 不是被 tag 隔離之後就「消失且沒人注意到」。
# 上面的 AC-L1/build、AC-L1/binary、AC-L1/go-test-all 已經是未加 -tags 的
# 呼叫，proves 一般 `go build ./...`/`go test ./...` 不受影響；這裡只新增
# tag 相關的兩項專屬檢查。
$symbolCheck = & go tool nm $bin 2>&1
if ($LASTEXITCODE -ne 0) {
  Fail 'A-MINOR-1' "go tool nm `$bin 本身失敗（exit $LASTEXITCODE），無法驗證符號缺席"
} else {
  $hit = $symbolCheck | Select-String -Pattern 'SetClackEventHookForTest' -SimpleMatch
  if ($hit) { Fail 'A-MINOR-1' "release binary（未加 -tags）仍含 SetClackEventHookForTest 符號：$hit" }
  else { Pass 'A-MINOR-1/symbol-absent' }
}
# 反向確認：未加 -tags 時同一個測試名稱必須「不存在」（-list 零匹配），
# 而不是存在但被跳過 -- 證明它是被排除編譯，不是靜默 skip。
$listedSeqUntagged = @(& go test ./cmd/apm-go/ -list 'TestInitVsPluginInit_ClackSequenceParity' 2>&1 | Where-Object { $_ -match '^Test' })
if ($listedSeqUntagged.Count -gt 0) { Fail 'A-MINOR-1' '未加 -tags apm_test_hooks 時 TestInitVsPluginInit_ClackSequenceParity 仍被 -list 列出，build tag 隔離未生效' }
else { Pass 'A-MINOR-1/untagged-excludes-clacksequence' }

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
