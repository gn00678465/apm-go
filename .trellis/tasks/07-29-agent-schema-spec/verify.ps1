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
# 所有「決定通過/失敗」的 native 呼叫一律走這裡。
# 2026-07-30 round-4 更正：這句話原本寫成「所有 native 呼叫」，不精確 ——
# 本檔下面仍有幾處 `& go test ./... -list ...` 探測性呼叫沒有走 Exec（實測：
# `-list` 在有其他套件建置失敗時整體 exit 非 0，但仍會把匹配到的測試名稱印在
# stdout，既有的 `.Count -eq 0` 判斷依然能正確依匹配數判定，不會因此假綠）；
# `git diff -- go.mod` 才是唯一真的有假綠風險的例外，已在 AC-L9 那段獨立補上
# exit code 檢查。
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
# 2026-07-30：改用 ExecTestJSON（移植自 marketplace-add-fixes），不再只驗 exit
# code -- t.Skip 逃逸口見該函式定義處註解。
$listedPos = @(& go test ./... -list 'SchemaGolden|SchemaValidateUpstream' 2>&1 | Where-Object { $_ -match '^Test' })
if ($listedPos.Count -eq 0) { Fail 'AS4' '-list 零匹配：找不到「上游 golden 產物驗證通過」的測試' }
else {
  ExecTestJSON 'AS4' "go test -json -run 'SchemaGolden|SchemaValidateUpstream'" './...' 'SchemaGolden|SchemaValidateUpstream'
}
$listedNeg = @(& go test ./... -list 'SchemaReject|SchemaNegative' 2>&1 | Where-Object { $_ -match '^Test' })
if ($listedNeg.Count -lt 3) { Fail 'AS5' "-list 只匹配 $($listedNeg.Count) 個負向測試，至少需 3（缺 category / 缺 owner / author 給字串）" }
else {
  # -minCount 3：AS5 明講至少 3 種負向情境，`-list` 只列頂層測試名稱，若三種情境
  # 是同一個頂層函式底下的 t.Run 子測試，刪掉其中一兩個不會被上面的 -list 存在性
  # 檢查抓到（BLOCKING 3 同類缺口，見 ExecTestJSON 定義處）。
  ExecTestJSON 'AS5' "go test -json -run 'SchemaReject|SchemaNegative'" './...' 'SchemaReject|SchemaNegative' -minCount 3
}

# ---- AS6：防漂移測試存在 ----
# 注意：AS6 的完整驗收（實際加欄位確認轉紅再還原）是 Tier 2 的人工/LLM 步驟，
# 這裡只做確定性的「測試存在且綠燈」；不得以此代替 Tier 2。
$listedDrift = @(& go test ./... -list 'SchemaDrift|SchemaSync' 2>&1 | Where-Object { $_ -match '^Test' })
if ($listedDrift.Count -eq 0) {
  Fail 'AS6' '-list 零匹配：找不到防漂移測試（Go 型別 ↔ schema ↔ spec 三者同步）'
} else {
  ExecTestJSON 'AS6/exists' "go test -json -run 'SchemaDrift|SchemaSync'" './...' 'SchemaDrift|SchemaSync'
  Write-Host "  NOTE [AS6] 「加欄位確認轉紅再還原」屬 Tier 2，本閘門不代替" -ForegroundColor Yellow
}

# ---- AC-L9：未新增第三方相依（本 task 風險最高：JSON Schema validator 可能誘使加相依） ----
# 2026-07-30：改為對照本分支的 base commit（3e450dd，`git merge-base HEAD main`
# 實測得出），而非只驗工作樹/暫存區 diff -- 在一棵乾淨的已 commit 樹上，
# 工作樹和暫存區 diff 都是空的，commit 之後才新增的一行 require 完全看不到
# （MAJOR 4，外部稽核第四輪，見 07-29-install-dev/verify.ps1 同段註解的完整推導）。
# `git diff <base> -- go.mod go.sum` 對單一 ref 是拿該 ref 與目前工作目錄比較，
# 未 commit 的部分也算在內，同時涵蓋已 commit 與尚未 commit 的變更。
$taskBase = '3e450dd'
$d1 = & git diff $taskBase -- go.mod go.sum 2>&1; $d1Exit = $LASTEXITCODE
if ($d1Exit -ne 0) {
  Fail 'AC-L9' "git diff $taskBase -- go.mod go.sum（exit $d1Exit）本身失敗，無法判定是否新增相依`n      $($d1 -join "``n      ")"
} else {
  $newReq = @($d1) | Where-Object { $_ -match '^\+\s+\S+\s+v' }
  if ($newReq) { Fail 'AC-L9' ("go.mod/go.sum 相對 task base（$taskBase）新增 require：" + ($newReq -join '; ') + " —— 需先取得使用者裁定") } else { Pass 'AC-L9' }
}

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
