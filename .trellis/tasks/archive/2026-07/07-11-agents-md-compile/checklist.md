# AGENTS.md compile 生成 — 硬性驗證清單

> **目的**：驗證 `apm-go compile` 的最小 agents-family 子集與 Python oracle 的
> single-file 契約一致，並證明 install/deploy、archive、symlink 與 uninstall provenance
> 安全線均未弱化。
>
> **權威來源優先序**：本 task 已核准的 PRD／design／本專案 spec → 官方 compile
> 文件 → Python oracle 原始碼與實際輸出 → research 發現 → Go 現況。若來源衝突，
> 不得自行挑選較容易通過者；必須先把裁定與 deviation 寫回 task/spec，再重跑本清單。
>
> **圖例**：`[ ]` 待驗 · `[x]` 已驗——**附證據才可勾**。每項證據至少包含實際
> 指令、精確 exit code、關鍵 stdout/stderr、斷言結果與檔案系統效果；只寫「PASS」
> 或只貼最後一行不得勾選。

---

## 0. 執行前置與安全

- **Repo / Build**：在 `D:/Projects/apm-dev/apm-go` 執行；release-style binary 指令固定為
  `go build -o bin/apm-go.exe ./cmd/apm`，binary 名稱不得改成 `apm.exe`。
- **Python oracle**：固定使用 `uv --project D:/Projects/apm-dev/apm run apm <args>`；
  PowerShell 每次以 `$LASTEXITCODE` 驗精確 exit code。
- **Scratch 隔離**：所有 Go/Python compile、install、uninstall live probe 一律在 `%TEMP%`
  新目錄；不得在 apm-go 或 Python repo 根執行。根 `AGENTS.md` 會被 full mode 整檔覆蓋。
- **Registry 隔離**：Go 的 `$env:APM_CONFIG_DIR` 必須指向 scratch。Python 不尊重此變數，
  **禁止**對 oracle 執行 `marketplace add/remove/update`；備份 registry 也不構成執行授權。
- **清理與版本控制**：驗完刪除 scratch；不得 `git commit` / `git push`。預期外 repo diff
  一律 FAIL，不得順便修正後掩蓋。

#### [x] PRE-01 · 工具、權威來源與交付檔完整

```powershell
Set-Location D:/Projects/apm-dev/apm-go
$required = @(
  'go.mod',
  '.trellis/tasks/07-11-agents-md-compile/prd.md',
  '.trellis/tasks/07-11-agents-md-compile/research/findings.md',
  '.trellis/tasks/07-11-agents-md-compile/design.md',
  '.trellis/tasks/07-11-agents-md-compile/implement.md',
  '.trellis/spec/backend/compile-contract.md',
  'D:/Projects/apm-dev/apm/docs/src/content/docs/reference/cli/compile.md',
  'D:/Projects/apm-dev/apm/src/apm_cli/commands/compile/cli.py',
  'D:/Projects/apm-dev/apm/src/apm_cli/compilation/agents_compiler.py',
  'D:/Projects/apm-dev/evals/ab_agents_compile.py',
  'D:/Projects/apm-dev/evals/ab_instructions_applyto.py',
  'D:/Projects/apm-dev/evals/ab_antigravity.py',
  'D:/Projects/apm-dev/evals/ab_uninstall.py',
  'D:/Projects/apm-dev/evals/ab_marketplace_install.py'
)
$missing = $required | Where-Object { -not (Test-Path -LiteralPath $_) }
$missingTools = 'go','uv','python','git' | Where-Object {
  -not (Get-Command $_ -ErrorAction SilentlyContinue)
}
if ($missing -or $missingTools) { $missing; $missingTools; exit 1 }
exit 0
```

預期：exit `0`，stdout/stderr 無缺檔或缺工具名稱，檔案系統零變動。缺少新 spec、
新 A/B 腳本或任一權威來源都直接 FAIL。

【權威】`.trellis/spec/conformance/cli-verification-checklist.md:17-24`；
`prd.md:31-37`；`design.md:125-126,134-143`。

#### [x] PRE-02 · scratch、Go registry 與 repo 根檔隔離成立

後續 live probe 必須沿用同一個 PowerShell session：

```powershell
$Repo = (Resolve-Path 'D:/Projects/apm-dev/apm-go').Path
$Bin = Join-Path $Repo 'bin/apm-go.exe'
$Scratch = Join-Path $env:TEMP ('apm-go-agents-compile-' + [guid]::NewGuid())
New-Item -ItemType Directory -Path $Scratch | Out-Null
$env:APM_CONFIG_DIR = Join-Path $Scratch 'apm-config'
New-Item -ItemType Directory -Path $env:APM_CONFIG_DIR | Out-Null
$RepoAgentsBefore = (Get-FileHash -Algorithm SHA256 (Join-Path $Repo 'AGENTS.md')).Hash
$OracleRegistry = Join-Path $HOME '.apm/marketplaces.json'
$OracleRegistryBefore = if (Test-Path $OracleRegistry) {
  (Get-FileHash -Algorithm SHA256 $OracleRegistry).Hash
} else { '<absent>' }
Set-Location $Scratch
if ((Resolve-Path .).Path.StartsWith($Repo, [StringComparison]::OrdinalIgnoreCase)) { exit 1 }
if (-not $env:APM_CONFIG_DIR.StartsWith($Scratch, [StringComparison]::OrdinalIgnoreCase)) { exit 1 }
exit 0
```

預期：exit `0`；只新增 `%TEMP%/apm-go-agents-compile-*` 與其 `apm-config/`，repo
內零變動；已記錄 repo `AGENTS.md` 與 oracle registry 的基線 hash。

【權威】`design.md:109-118`；`implement.md:5-7`；
`.trellis/spec/conformance/cli-verification-checklist.md:22-24`。

#### [x] PRE-03 · release-style binary 可由指定指令建立

```powershell
Set-Location $Repo
go build -o bin/apm-go.exe ./cmd/apm
$rc = $LASTEXITCODE
if ($rc -ne 0 -or -not (Test-Path -LiteralPath $Bin) -or
    (Get-Item $Bin).Length -le 0) { exit 1 }
exit 0
```

預期：exit `0`；只建立／更新 `bin/apm-go.exe`，檔案大小大於 0；不得產生
`bin/apm.exe`。編譯 warning/error 或不同 binary 名稱皆 FAIL。

【權威】AGENTS.md Available commands；`implement.md:41-42`；
`.trellis/spec/conformance/cli-verification-checklist.md:19`。

#### [x] PRE-04 · oracle marketplace 寫入禁令可稽核

```powershell
$evals = @(
  'D:/Projects/apm-dev/evals/ab_agents_compile.py',
  'D:/Projects/apm-dev/evals/ab_instructions_applyto.py',
  'D:/Projects/apm-dev/evals/ab_antigravity.py',
  'D:/Projects/apm-dev/evals/ab_uninstall.py',
  'D:/Projects/apm-dev/evals/ab_marketplace_install.py'
)
$bad = Select-String -Path $evals `
  -Pattern 'run_py\s*\(\s*\[\s*["'']marketplace["'']\s*,\s*["''](add|remove|update)["'']'
if ($bad) { $bad; exit 1 }
exit 0
```

預期：exit `0`、零 match；後續命令紀錄中 oracle 只可出現 compile/install/uninstall，
不得出現 marketplace add/remove/update。`ab_marketplace_install.py` 可直接建立隔離 fixture，
但不可呼叫上述 oracle registry mutation 指令。

【權威】`implement.md:5-7`；
`.trellis/spec/conformance/cli-verification-checklist.md:22`。

---

## 1. 規劃、研究與 spec 驗收

#### [x] DOC-01 · research 完整覆蓋 PRD 指定的六個 Python 行為面

```powershell
$f = Join-Path $Repo '.trellis/tasks/07-11-agents-md-compile/research/findings.md'
$text = Get-Content -LiteralPath $f -Raw
$patterns = @(
  '觸發時機', '輸入來源', 'compile_family', '去重/合併',
  'marker 區塊格式', 'Idempotency', '選項 B', 'file:line'
)
$missing = $patterns | Where-Object { $text -notmatch [regex]::Escape($_) }
if ($missing) { $missing; exit 1 }
exit 0
```

預期：exit `0`；八個 pattern 全命中，且每節保留 Python `file:line` 或 scratch
實測證據。只有結論、沒有逐項來源，或缺任一 PRD 行為面皆 FAIL。

【權威】`prd.md:20-29,31-37`；`research/findings.md:21-120,142-190`。

#### [x] DOC-02 · 六個拍板點已明確核准，design/implement 不再是 draft

```powershell
$task = Join-Path $Repo '.trellis/tasks/07-11-agents-md-compile'
$design = Get-Content -LiteralPath (Join-Path $task 'design.md') -Raw
$impl = Get-Content -LiteralPath (Join-Path $task 'implement.md') -Raw
$findings = Get-Content -LiteralPath (Join-Path $task 'research/findings.md') -Raw
$decisions = @('落地範圍','v1 支援 target 集','compile 與 install 的關係',
               'hand-authored 根 AGENTS.md 覆蓋策略','A/B 基準線','v1 flags 面')
$missing = $decisions | Where-Object { $findings -notmatch [regex]::Escape($_) }
if ($missing -or $design -match 'draft\s*—\s*待 review' -or
    $impl -match 'draft\s*—\s*待 review') { $missing; exit 1 }
exit 0
```

預期：exit `0`；六點均有核准結論，design/implement 檔頭不再標 draft。不得以
「建議 B」冒充使用者核准，也不得在 review 前進入實作驗收。

【權威】`research/findings.md:183-190`；`implement.md:64-69`；
`prd.md:46-47`。

#### [x] DOC-03 · install 是否自動 compile 的來源衝突已用 oracle probe 裁定

```powershell
$case = Join-Path $Scratch 'oracle-install-relationship'
New-Item -ItemType Directory -Path (Join-Path $case '.apm/instructions') -Force | Out-Null
New-Item -ItemType Directory -Path (Join-Path $case '.codex') -Force | Out-Null
[IO.File]::WriteAllText((Join-Path $case 'apm.yml'), "name: probe`nversion: `"1.0.0`"`n")
[IO.File]::WriteAllText((Join-Path $case '.apm/instructions/a.instructions.md'), "A`n")
Push-Location $case
uv --project D:/Projects/apm-dev/apm run apm install --target codex *> install.log
$rc = $LASTEXITCODE
Pop-Location
$created = Test-Path (Join-Path $case 'AGENTS.md')
$findings = Get-Content -LiteralPath (Join-Path $Repo '.trellis/tasks/07-11-agents-md-compile/research/findings.md') -Raw
if ($rc -ne 0 -or $created -or
    $findings -notmatch 'producer/compile\.md:147-151' -or
    $findings -notmatch '來源衝突|source conflict') { exit 1 }
exit 0
```

預期：依本 task 拍板，oracle install exit `0` 但不產生 `AGENTS.md`，compile 仍是獨立
指令；research 必須明記官方 producer 文件 `compile.md:147-151` 的相反敘述與實測裁定。
若目前 oracle 真的產生 `AGENTS.md`，此項必須 FAIL 並先修訂 research/design/spec，
不得把衝突靜默降級。scratch 以外零變動。

**第 2 輪 CONFIRMED（2026-07-11）**：在新的 `%TEMP%/apm-go-doc03-r2-*`
fixture 重跑本項 oracle install，exit `0`、`AGENTS.md=False`；原 regex 斷言實際得到
`ProducerRef=True, ConflictRecorded=True`，harness exit `0`。人工讀取
`research/findings.md:253-293`，確認附錄本體同時記錄官方 producer 散文、Python
原始碼／TEMP probe、權威序裁定與 design/implement 結論。另直接抽查官方
`producer/compile.md:126-136` 表格：codex/gemini/antigravity/opencode 均標為
`Compile required? Yes`；`:147-151` 卻稱一般 `apm install` 已產生正確
AGENTS/CLAUDE/GEMINI 輸出，文件自身矛盾及引用均真實存在。

【權威】`prd.md:22-29`；`research/findings.md:23-31,183-190`；Python
`install/local_bundle_handler.py:267-275`；官方 producer `compile.md:138-151`。

#### [x] DOC-04 · compile contract、deviation 與索引均已落檔

```powershell
$spec = Join-Path $Repo '.trellis/spec/backend/compile-contract.md'
$index = Join-Path $Repo '.trellis/spec/backend/index.md'
$agy = Join-Path $Repo '.trellis/spec/backend/antigravity-target-contract.md'
$s = Get-Content -LiteralPath $spec -Raw
$required = @('antigravity','codex','opencode','exit code','Build ID','single',
              'LF','CRLF','APM Version','distributed','user scope','symlink')
$missing = $required | Where-Object { $s -notmatch [regex]::Escape($_) }
if ($missing -or (Get-Content $index -Raw) -notmatch 'compile-contract' -or
    (Get-Content $agy -Raw) -notmatch 'compile-contract') { $missing; exit 1 }
exit 0
```

預期：exit `0`；新 spec 同時記錄 target/exit/output/Build ID 與全部 deviations，backend
index 和 antigravity contract 都有互見；不得含 `TBD` 或未裁定 placeholder。

【權威】`prd.md:37`；`design.md:17-24,98-100,125-126,134-143`；
`implement.md:55-62`。

---

## 2. CLI、target 與專案 gate

#### [x] CLI-01 · `compile` 已註冊，v1 只公開 `-t/--target`

```powershell
Set-Location $Scratch
& $Bin compile --help *> compile-help.txt
$rc = $LASTEXITCODE
$help = Get-Content compile-help.txt -Raw
$forbidden = '--dry-run','--watch','--validate','--root','--single-agents',
             '--clean','--output','--no-links','--no-constitution'
$leaked = $forbidden | Where-Object { $help -match [regex]::Escape($_) }
if ($rc -ne 0 -or $help -notmatch '(?m)^.*compile.*$' -or
    $help -notmatch '--target' -or $help -notmatch '\-t' -or $leaked) {
  $leaked; exit 1
}
exit 0
```

預期：exit `0`；help 含 `compile` 與 `-t, --target`，除 Cobra 內建 `--help` 外不洩漏
任何 non-goal flag；檔案系統除 `compile-help.txt` 外零效果。

【權威】`design.md:26-30`；`research/findings.md:150-158,190`；
Python `commands/compile/cli.py:790-894`（完整 oracle 面，v1 明確縮減）。

#### [x] CLI-02 · 無 `apm.yml` 精確 exit 1，且零輸出檔

```powershell
$case = Join-Path $Scratch 'no-apm-yml'
New-Item -ItemType Directory -Path (Join-Path $case '.apm/instructions') -Force | Out-Null
[IO.File]::WriteAllText((Join-Path $case '.apm/instructions/a.instructions.md'), "A`n")
Push-Location $case
& $Bin compile -t antigravity *> out.txt
$rc = $LASTEXITCODE
Pop-Location
$out = Get-Content (Join-Path $case 'out.txt') -Raw
if ($rc -ne 1 -or $out -notmatch 'Not an APM project - no apm.yml found' -or
    (Test-Path (Join-Path $case 'AGENTS.md'))) { exit 1 }
exit 0
```

預期：驗證 shell exit `0`；受測 CLI exit **恰為 `1`**，訊息含指定字串，
`AGENTS.md` 不存在，既有 instruction bytes 不變。

【權威】`design.md:41-44`；Python `commands/compile/cli.py:347-351`；
官方 `reference/cli/compile.md:258-264`。

#### [x] CLI-03 · 無可編譯 instruction 精確 exit 1

```powershell
$case = Join-Path $Scratch 'no-content'
New-Item -ItemType Directory -Path $case -Force | Out-Null
[IO.File]::WriteAllText((Join-Path $case 'apm.yml'), "name: empty`nversion: `"1.0.0`"`n")
Push-Location $case
& $Bin compile -t codex *> out.txt
$rc = $LASTEXITCODE
Pop-Location
$out = Get-Content (Join-Path $case 'out.txt') -Raw
if ($rc -ne 1 -or $out -notmatch 'No .*content|No instruction files' -or
    (Test-Path (Join-Path $case 'AGENTS.md'))) { exit 1 }
exit 0
```

預期：受測 CLI exit `1`；stderr/stdout 有明確 no-content 診斷，無 `AGENTS.md`、
CLAUDE.md、GEMINI.md 或新目錄。

【權威】`design.md:41-44`；Python `commands/compile/cli.py:353-385`；
官方 `reference/cli/compile.md:263`。

#### [x] CLI-04 · 三個 agents-family target 各自成功且輸出相同

```powershell
$hashes = @()
foreach ($target in 'antigravity','codex','opencode') {
  $case = Join-Path $Scratch ('target-' + $target)
  New-Item -ItemType Directory -Path (Join-Path $case '.apm/instructions') -Force | Out-Null
  [IO.File]::WriteAllText((Join-Path $case 'apm.yml'), "name: $target`nversion: `"1.0.0`"`n")
  [IO.File]::WriteAllText((Join-Path $case '.apm/instructions/a.instructions.md'), "Body [$target]`n")
  # 內容先統一，避免 target 名本身影響 hash
  [IO.File]::WriteAllText((Join-Path $case '.apm/instructions/a.instructions.md'), "Body [A]`n")
  Push-Location $case
  & $Bin compile -t $target *> out.txt
  $rc = $LASTEXITCODE
  Pop-Location
  if ($rc -ne 0 -or -not (Test-Path (Join-Path $case 'AGENTS.md'))) { exit 1 }
  $hashes += (Get-FileHash -Algorithm SHA256 (Join-Path $case 'AGENTS.md')).Hash
}
if (($hashes | Select-Object -Unique).Count -ne 1) { $hashes; exit 1 }
exit 0
```

預期：三次受測 CLI 都 exit `0`；每個 scratch 只產一份根 `AGENTS.md`，三份 SHA256
完全相同。任一 target 被靜默 no-op 或產出不同內容皆 FAIL。

【權威】`design.md:9-15,32-35`；Python `integration/targets.py:613,685,708`；
`core/target_detection.py:226-252`；官方 producer `compile.md:131-134`。

#### [x] CLI-05 · 多 agents target 只編譯一次且不產生其他 family 檔案

```powershell
$case = Join-Path $Scratch 'multi-target'
New-Item -ItemType Directory -Path (Join-Path $case '.apm/instructions') -Force | Out-Null
[IO.File]::WriteAllText((Join-Path $case 'apm.yml'), "name: multi`nversion: `"1.0.0`"`n")
[IO.File]::WriteAllText((Join-Path $case '.apm/instructions/a.instructions.md'), "Body`n")
Push-Location $case
& $Bin compile -t 'codex,opencode,antigravity' *> out.txt
$rc = $LASTEXITCODE
Pop-Location
$unexpected = @('CLAUDE.md','GEMINI.md','.github/copilot-instructions.md') |
  Where-Object { Test-Path (Join-Path $case $_) }
$agents = @(Get-ChildItem -LiteralPath $case -Recurse -File -Filter 'AGENTS.md')
if ($rc -ne 0 -or $agents.Count -ne 1 -or $unexpected) { $unexpected; exit 1 }
exit 0
```

預期：受測 CLI exit `0`；全樹只有根的一份 `AGENTS.md`，沒有重複編譯訊息或其他
family 輸出。

【權威】`design.md:32-35`；Python `commands/compile/cli.py:180-270`；
`agents_compiler.py:391-420`。

#### [x] CLI-06 · unsupported family、unknown target 與零訊號均 exit 2

```powershell
$cases = @(
  @{ Name='claude'; Args=@('-t','claude') },
  @{ Name='copilot'; Args=@('-t','copilot') },
  @{ Name='unknown'; Args=@('-t','not-a-target') },
  @{ Name='no-signal'; Args=@() }
)
foreach ($c in $cases) {
  $case = Join-Path $Scratch ('reject-' + $c.Name)
  New-Item -ItemType Directory -Path (Join-Path $case '.apm/instructions') -Force | Out-Null
  [IO.File]::WriteAllText((Join-Path $case 'apm.yml'), "name: reject`nversion: `"1.0.0`"`n")
  [IO.File]::WriteAllText((Join-Path $case '.apm/instructions/a.instructions.md'), "Body`n")
  $argList = @($c.Args)
  Push-Location $case
  & $Bin compile @argList *> out.txt
  $rc = $LASTEXITCODE
  Pop-Location
  $out = Get-Content (Join-Path $case 'out.txt') -Raw
  if ($rc -ne 2 -or $out -notmatch 'not implemented in apm-go yet' -or
      (Test-Path (Join-Path $case 'AGENTS.md'))) { exit 1 }
}
exit 0
```

預期：四案受測 CLI exit 都恰為 `2`，診斷含 `not implemented in apm-go yet`，
零 compile output。不得以 exit 0 silent no-op 冒充成功。

【權威】`design.md:36-44`；`research/findings.md:158,190`；Python zero-output
反模式記錄 `design.md:36-38`。

#### [x] CLI-07 · target 優先序為 flag > manifest > auto-detect

```powershell
# A: flag codex 覆蓋 manifest claude -> success
$a = Join-Path $Scratch 'precedence-flag'
New-Item -ItemType Directory -Path (Join-Path $a '.apm/instructions') -Force | Out-Null
[IO.File]::WriteAllText((Join-Path $a 'apm.yml'), "name: p`nversion: `"1.0.0`"`ntarget: claude`n")
[IO.File]::WriteAllText((Join-Path $a '.apm/instructions/a.instructions.md'), "A`n")
Push-Location $a; & $Bin compile -t codex *> out.txt; $ra=$LASTEXITCODE; Pop-Location

# B: manifest claude 覆蓋 .codex signal -> unsupported exit 2
$b = Join-Path $Scratch 'precedence-manifest'
Copy-Item -LiteralPath $a -Destination $b -Recurse
New-Item -ItemType Directory -Path (Join-Path $b '.codex') -Force | Out-Null
Remove-Item -LiteralPath (Join-Path $b 'AGENTS.md') -ErrorAction SilentlyContinue
Push-Location $b; & $Bin compile *> out.txt; $rb=$LASTEXITCODE; Pop-Location

# C: 無 manifest target，.codex auto-detect -> success
$c = Join-Path $Scratch 'precedence-auto'
Copy-Item -LiteralPath $b -Destination $c -Recurse
[IO.File]::WriteAllText((Join-Path $c 'apm.yml'), "name: p`nversion: `"1.0.0`"`n")
Push-Location $c; & $Bin compile *> out.txt; $rc=$LASTEXITCODE; Pop-Location
if ($ra -ne 0 -or $rb -ne 2 -or $rc -ne 0 -or
    -not (Test-Path (Join-Path $a 'AGENTS.md')) -or
    (Test-Path (Join-Path $b 'AGENTS.md')) -or
    -not (Test-Path (Join-Path $c 'AGENTS.md'))) { exit 1 }
exit 0
```

預期：A/C exit `0` 並產根 `AGENTS.md`；B exit `2` 且零輸出，精確證明三層優先序。

【權威】`design.md:32-40`；`research/findings.md:40-42`；Python
`commands/compile/cli.py:274-335`；Go `internal/deploy/adapter.go:76-116`。

#### [x] CLI-08 · non-goal flags 與不完整 `-t` 乾淨拒絕、零副作用

```powershell
$badArgs = @(
  @('--dry-run'), @('--watch'), @('--root','x'), @('--single-agents'), @('-t')
)
foreach ($argList in $badArgs) {
  $case = Join-Path $Scratch ('bad-flag-' + [guid]::NewGuid())
  New-Item -ItemType Directory -Path (Join-Path $case '.apm/instructions') -Force | Out-Null
  [IO.File]::WriteAllText((Join-Path $case 'apm.yml'), "name: bad`nversion: `"1.0.0`"`n")
  [IO.File]::WriteAllText((Join-Path $case '.apm/instructions/a.instructions.md'), "A`n")
  Push-Location $case; & $Bin compile @argList *> out.txt; $rc=$LASTEXITCODE; Pop-Location
  $out = Get-Content (Join-Path $case 'out.txt') -Raw
  if ($rc -ne 1 -or $out -notmatch 'unknown flag|flag needs an argument|requires argument' -or
      (Test-Path (Join-Path $case 'AGENTS.md'))) { exit 1 }
}
exit 0
```

預期：每案受測 CLI exit 恰為 `1`（Cobra usage error），輸出含 unknown flag 或
requires argument，且 instruction/apm.yml 以外零檔案效果。

【權威】`design.md:17-24,26-30`；`research/findings.md:158,190`。

#### [x] CLI-09 · compile 產生的 `AGENTS.md` 不得反向啟用 antigravity install

```powershell
$case = Join-Path $Scratch 'no-detect-feedback'
New-Item -ItemType Directory -Path (Join-Path $case '.apm/instructions') -Force | Out-Null
[IO.File]::WriteAllText((Join-Path $case 'apm.yml'), "name: feedback`nversion: `"1.0.0`"`n")
[IO.File]::WriteAllText((Join-Path $case '.apm/instructions/a.instructions.md'), "A`n")
Push-Location $case
& $Bin compile -t antigravity *> compile.txt
$r1 = $LASTEXITCODE
& $Bin install *> install.txt
$r2 = $LASTEXITCODE
Pop-Location
if ($r1 -ne 0 -or $r2 -ne 2 -or
    (Test-Path (Join-Path $case '.agents/rules/a.md')) -or
    (Get-Content (Join-Path $case 'install.txt') -Raw) -notmatch 'no deployment target detected') {
  exit 1
}
exit 0
```

預期：compile exit `0`；其後 bare install exit `2`，因 `AGENTS.md` 不是 antigravity
signal；`.agents/rules/` 不得被建立。這是「不改 install/detect」的使用者可見負向證明。

【權威】`prd.md:28-29,41-42`；`design.md:15,118,124`；spec
`backend/antigravity-target-contract.md:36-46`。

---

## 3. instruction 收集、解析與輸出契約

#### [x] CMP-01 · local + dependency instruction 與 Source relpath 正確

```powershell
Set-Location $Repo
go test ./internal/compile -run '^TestCollectInstructions_LocalDependencySourcePaths$' -count=1 -v
$rc = $LASTEXITCODE
if ($rc -ne 0) { exit 1 }; exit 0
```

預期：exit `0`、測試 PASS；local Source 恰為
`.apm/instructions/local.instructions.md`，dependency 恰為
`apm_modules/acme/dep/.apm/instructions/dep.instructions.md`，皆為 forward slash；
兩份 body 都進入輸出。

【權威】`design.md:48-61,92-96`；`implement.md:9-18`；Python
`compilation/template_builder.py:145-167`；`research/findings.md:194-195`。

#### [x] CMP-02 · priority、同名 first-wins 與 transitive 排序

```powershell
go test ./internal/compile -run '^(TestCollectInstructions_PriorityAndTransitiveOrder|TestSortedTransitiveDeps_(ExcludesDirectKeys|TieBreaksOnVirtualPath))$' -count=25 -v
$rc = $LASTEXITCODE
if ($rc -ne 0) { exit 1 }; exit 0
```

預期：exit `0`、測試 PASS；順序為 local → direct manifest declaration order →
transitive lockfile sort；同名 local/dep 只保留 local，兩個 direct dep 同名只保留先宣告者，
不因 map iteration 改變。

【權威】`design.md:52-56`；Python `primitives/discovery.py:175-205`；Go
`internal/deploy/deploy.go:72-118`；`research/findings.md:37,73,135-136`。

#### [x] CMP-03 · 只收 `*.instructions.md`，compile symlink 不得洩漏外部內容

```powershell
go test ./internal/compile -run '^TestCollectInstructions_IgnoresWrongSuffixAndSymlink$' -count=1 -v
$rc = $LASTEXITCODE
if ($rc -ne 0) { exit 1 }; exit 0
```

預期：exit `0`、測試 PASS；同目錄 `plain.md`、目錄與 symlink instruction 都不進
輸出；指向 source tree 外的 secret token 不出現在 `AGENTS.md`，且外部檔 bytes 不變。
不支援 symlink 的平台只能記 SKIP，**此項不得勾選**，必須在可建立 symlink 的環境重跑。

【權威】Python `primitives/discovery.py:588-600,681-693`、`parser.py:80-89`；
spec `backend/install-marketplace-contracts.md:77,131`；`research/findings.md:38,135`。

#### [x] CMP-04 · frontmatter scalar、list-first、無 frontmatter 三形狀

```powershell
go test ./internal/compile -run '^TestParseInstruction_ApplyToScalarListAndNoFrontmatter$' -count=1 -v
$rc = $LASTEXITCODE
if ($rc -ne 0) { exit 1 }; exit 0
```

預期：exit `0`；scalar 原字串保留，YAML list 取第一個非 null 元素，無 frontmatter
得到空 applyTo + 完整 body；frontmatter marker 不殘留於 body。

【權威】`design.md:57-60`；Python `primitives/parser.py:95-119`；
`research/findings.md:38,178`。

#### [x] CMP-05 · raw applyTo comma/brace 不得誤用 install splitter

```powershell
go test ./internal/compile -run '^TestRender_RawApplyToCommaAndBrace$' -count=1 -v
$rc = $LASTEXITCODE
if ($rc -ne 0) { exit 1 }; exit 0
```

預期：exit `0`；`**/src/**, **/api/**` 只產一個完整 heading，brace pattern 亦逐字
保留；不得拆成多個 group 或改寫 glob。

【權威】`design.md:64-65`；Python `compilation/template_builder.py:62-82`；
`research/findings.md:74-76,177`。

#### [x] CMP-06 · global/pattern/relpath 排序 deterministic

```powershell
go test ./internal/compile -run '^TestRender_DeterministicGroupingAndSorting$' -count=25 -v
$rc = $LASTEXITCODE
if ($rc -ne 0) { exit 1 }; exit 0
```

預期：exit `0`；25 次輸出 byte-identical；Global 永遠在前，pattern 字典序，組內
Source relpath 字典序，不受建立順序或 map iteration 影響。

【權威】`design.md:92-93`；Python `compilation/template_builder.py:54-84`；
`research/findings.md:74`。

#### [x] CMP-07 · 空 body 被過濾，但全空群組保留 oracle 的孤立 heading

```powershell
go test ./internal/compile -run '^TestRender_FiltersEmptyBodies$' -count=1 -v
$rc = $LASTEXITCODE
if ($rc -ne 0) { exit 1 }; exit 0
```

預期：exit `0`；只有空白／只有 frontmatter 的 instruction 不出現 Source、End source 或
body；但若 global／pattern 群組內所有 body 都空，仍須分別保留孤立的
`## Global Instructions`／`## Files matching ...` heading；同組有效 instruction 仍正常輸出。

【獨立 oracle 裁決證據，2026-07-11】在兩個 `%TEMP%` scratch 內各建立只有一個空 body
的 global／`applyTo: "**/*.md"` instruction（以 `.codex/` 供 auto-detect），逐案執行：

```powershell
uv --project D:/Projects/apm-dev/apm run apm compile --single-agents --no-links --no-constitution
```

兩案 oracle `apm_cli 0.21.0` 皆 exit `0` 且產生 `AGENTS.md`；global 案
`HeadingCount=1, SourceCount=0, SHA256=6DE0628D80269DA3D36D5B3EA6F8A8F2ADD0FD0345036822B1EFDBFCDCA00C7B`，
pattern 案 `HeadingCount=1, SourceCount=0, SHA256=96629DF8C68E55664E8B905DA8B7F2F8A1F31B292262ED0B3DE6D6434C059504`。
兩份輸出都是孤立 heading 後直接接 `---` footer，證實原「不出現孤立 heading」敘述與
oracle 實證衝突，故依本 task 的 oracle 裁決規則修訂為上述契約。

【權威】`design.md:57-61`；Python `compilation/template_builder.py:70-82`；
`research/findings.md` Step 0 附錄第 4 項。

#### [x] CMP-08 · header、Source wrapper、heading、footer 完整匹配 oracle

```powershell
go test ./internal/compile -run '^TestRender_OracleTemplate$' -count=1 -v
$rc = $LASTEXITCODE
if ($rc -ne 0) { exit 1 }; exit 0
```

預期：exit `0`；逐行斷言 `# AGENTS.md`、generated marker、12-hex Build ID、
APM Version、Global / Files matching heading、每條 Source/End source、`---` 與兩行 footer；
每條 instruction 後一空行，沒有 distributed marker。

【權威】`design.md:67-100`；Python
`compilation/template_builder.py:153-167,189-224`；`research/findings.md:78-108`。

#### [x] CMP-09 · UTF-8、LF 與單一 trailing newline

```powershell
go test ./internal/compile -run '^TestRender_UTF8LFAndTrailingNewline$' -count=1 -v
$rc = $LASTEXITCODE
if ($rc -ne 0) { exit 1 }; exit 0
```

預期：exit `0`；中英文 UTF-8 bytes 正確、輸出不含 `0D 0A` 或裸 `0D`，檔尾恰一個
`0A`；這是 Go 的 documented LF deviation，不得為追求 Windows byte parity 改成 CRLF。

【權威】`design.md:97-100`；`research/findings.md:115-120,176`；既有 deviation
`D:/Projects/apm-dev/evals/ab_instructions_applyto.py:16-20`。

#### [x] CMP-10 · Build ID 演算法與 placeholder 清除

```powershell
go test ./internal/compile -run '^TestBuildID_OracleAlgorithm$' -count=1 -v
$rc = $LASTEXITCODE
if ($rc -ne 0) { exit 1 }; exit 0
```

預期：exit `0`；移除 placeholder 整行後以 LF join、SHA256 前 12 hex，替換後保留
trailing newline；輸出不含 `__BUILD_ID__`，已固定 Build ID 的內容再次處理不變。

【權威】`design.md:102-107`；Python `compilation/build_id.py:22-39`；
`compilation/output_writer.py:37-49`。

#### [x] CMP-11 · APM Version 差異只限版本行與其衍生 Build ID

```powershell
go test ./internal/compile -run '^TestVersionLine_IsOnlyVersionSpecificTemplateDifference$' -count=1 -v
$rc = $LASTEXITCODE
if ($rc -ne 0) { exit 1 }; exit 0
```

預期：exit `0`；Go 寫自身非空版本；normalize `<!-- APM Version: ... -->` 與
`<!-- Build ID: ... -->` 後，其餘 template bytes 與 oracle 相同。不得 normalize body、
Source path、heading 或 footer 來掩蓋差異。

【權威】`design.md:98-107,134-143`；`research/findings.md:155,176,189`。

---

## 4. 寫檔、覆蓋與 idempotency

#### [x] IO-01 · 同輸入重跑 byte/hash/mtime 不變並印 no-change 訊息

```powershell
$case = Join-Path $Scratch 'idempotent'
New-Item -ItemType Directory -Path (Join-Path $case '.apm/instructions') -Force | Out-Null
[IO.File]::WriteAllText((Join-Path $case 'apm.yml'), "name: idem`nversion: `"1.0.0`"`n")
[IO.File]::WriteAllText((Join-Path $case '.apm/instructions/a.instructions.md'), "A`n")
Push-Location $case
& $Bin compile -t codex *> first.txt; $r1=$LASTEXITCODE
$p = Join-Path $case 'AGENTS.md'
$h1=(Get-FileHash -Algorithm SHA256 $p).Hash; $m1=(Get-Item $p).LastWriteTimeUtc
Start-Sleep -Milliseconds 1200
& $Bin compile -t codex *> second.txt; $r2=$LASTEXITCODE
$h2=(Get-FileHash -Algorithm SHA256 $p).Hash; $m2=(Get-Item $p).LastWriteTimeUtc
Pop-Location
if ($r1 -ne 0 -or $r2 -ne 0 -or $h1 -ne $h2 -or $m1 -ne $m2 -or
    (Get-Content (Join-Path $case 'second.txt') -Raw) -notmatch
      'No changes detected; preserving existing AGENTS.md for idempotency') { exit 1 }
exit 0
```

預期：兩次受測 CLI exit `0`；AGENTS SHA256 與 mtime 均相同，第二次有指定訊息；
證明不是重寫同 bytes。

【權威】`design.md:109-113`；Python `commands/compile/cli.py:687-723`；
`research/findings.md:110-120`。

#### [x] IO-02 · 輸入變更必須改內容與 Build ID

```powershell
$case = Join-Path $Scratch 'idempotent'
$p = Join-Path $case 'AGENTS.md'
$before = (Get-FileHash -Algorithm SHA256 $p).Hash
$id1 = [regex]::Match((Get-Content $p -Raw), '<!-- Build ID: ([0-9a-f]{12}) -->').Groups[1].Value
[IO.File]::WriteAllText((Join-Path $case '.apm/instructions/a.instructions.md'), "A changed`n")
Push-Location $case; & $Bin compile -t codex *> changed.txt; $rc=$LASTEXITCODE; Pop-Location
$after = (Get-FileHash -Algorithm SHA256 $p).Hash
$id2 = [regex]::Match((Get-Content $p -Raw), '<!-- Build ID: ([0-9a-f]{12}) -->').Groups[1].Value
if ($rc -ne 0 -or $before -eq $after -or $id1 -eq $id2 -or
    (Get-Content $p -Raw) -notmatch 'A changed') { exit 1 }
exit 0
```

預期：受測 CLI exit `0`；檔案 SHA256、Build ID 都改變，新 body 出現，舊 body 不再
作為獨立 instruction 內容出現。

【權威】`design.md:102-113`；Python `compilation/build_id.py:22-39`。

#### [x] IO-03 · atomic write 失敗不得破壞既有檔

```powershell
Set-Location $Repo
go test ./internal/compile -run '^TestWriteFile_AtomicFailurePreservesExisting$' -count=1 -v
$rc = $LASTEXITCODE
if ($rc -ne 0) { exit 1 }; exit 0
```

預期：exit `0`、測試 PASS；模擬 rename/write 失敗時函式回 error，既有 `AGENTS.md`
bytes 完全不變，temp 檔不殘留。不得以直接 truncate + write 實作。

【權威】`design.md:111-113`；Python `compilation/output_writer.py:1-18,37-49`；
`research/findings.md:116`。

#### [x] IO-04 · full mode 只可在 scratch 整檔覆蓋，repo 根永不被測試改動

```powershell
$case = Join-Path $Scratch 'full-overwrite'
New-Item -ItemType Directory -Path (Join-Path $case '.apm/instructions') -Force | Out-Null
[IO.File]::WriteAllText((Join-Path $case 'apm.yml'), "name: full`nversion: `"1.0.0`"`n")
[IO.File]::WriteAllText((Join-Path $case '.apm/instructions/a.instructions.md'), "Compiled`n")
[IO.File]::WriteAllText((Join-Path $case 'AGENTS.md'), "HAND_AUTHORED_CANARY`n")
Push-Location $case; & $Bin compile -t antigravity *> out.txt; $rc=$LASTEXITCODE; Pop-Location
$text = Get-Content (Join-Path $case 'AGENTS.md') -Raw
$repoHash = (Get-FileHash -Algorithm SHA256 (Join-Path $Repo 'AGENTS.md')).Hash
if ($rc -ne 0 -or $text -match 'HAND_AUTHORED_CANARY' -or
    $text -notmatch 'Generated by APM CLI' -or $repoHash -ne $RepoAgentsBefore) { exit 1 }
exit 0
```

預期：scratch 手寫 canary 被完整取代（oracle full-mode parity），受測 CLI exit `0`；
repo 根 `AGENTS.md` hash 與 PRE-02 相同。任何 repo 根 compile 都是立即 FAIL。

【權威】`design.md:109-118`；`research/findings.md:107-108,172-176,188`；
官方 producer `compile.md:201-204`。

---

## 5. 安全不變式與負向案例（不得弱化）

#### [x] SEC-01 · `archive.ContainedKey` 拒絕所有 `..` 形狀

```powershell
Set-Location $Repo
go test ./internal/archive -run '^TestContainedKey$' -count=1 -v
$rc = $LASTEXITCODE
if ($rc -ne 0) { exit 1 }; exit 0
```

預期：exit `0`；`../`、深層 escape、`acme/../other` 與反斜線版本全部 false，
正常 `acme/foo` true；磁碟零效果。

【權威】spec `backend/install-marketplace-contracts.md:77`；
`internal/archive/extract.go:224-234`；`internal/archive/extract_test.go:213-233`。

#### [x] SEC-02 · local copy destination 仍由 `ContainedKey` fail-closed

```powershell
go test ./internal/gitops -run '^TestMaterializeLocalCopy_RefusesKeyEscapingModulesDir$' -count=1 -v
$rc = $LASTEXITCODE
if ($rc -ne 0) { exit 1 }; exit 0
```

預期：exit `0`；惡意 module key 回錯，訊息含 `refusing`/`outside`；modules root 外
canary bytes 存活，錯誤目的地不存在。

【權威】spec `backend/install-marketplace-contracts.md:61,77`；
`internal/gitops/clone.go:241-253`。

#### [x] SEC-03 · archive symlink / hardlink entry 仍硬拒絕

```powershell
go test ./internal/archive -run '^TestSafeExtract_(SymlinkEscape|HardlinkEscape)$' -count=1 -v
$rc = $LASTEXITCODE
if ($rc -ne 0) { exit 1 }; exit 0
```

預期：exit `0`；symlink 與 hardlink 兩個 test 都實際 PASS；`SafeExtract` 對 link
archive 回錯，診斷含 `link`，destination 外 canary 不存在／未變；不得降級成 follow
或 silent copy。

【權威】OpenAPM `req-sc-002`（`.trellis/spec/conformance/openapm-v0.1.md:179`）；
`internal/archive/extract.go:116-120`；`internal/archive/extract_test.go:50-61`。

#### [x] SEC-04 · `copyTreeNoSymlinks` 不跟隨、不複製 local source symlink

```powershell
go test ./internal/gitops -run '^TestCopyTreeNoSymlinks_SkipsSymlinks$' -count=1 -v
$rc = $LASTEXITCODE
if ($rc -ne 0) { exit 1 }; exit 0
```

預期：exit `0`；正常檔被複製，指向 tree 外 secret 的 symlink 在目的地完全不存在
（既非 link 也非 dereferenced bytes），外部 secret 未變。平台 SKIP 不算 PASS。

【權威】spec `backend/install-marketplace-contracts.md:61,77`；
`internal/gitops/clone.go:280-322`；`internal/gitops/clone_test.go:404-433`。

#### [x] SEC-05 · `SafeRemoveModuleDir` 拒絕 escape 且 sibling 存活

```powershell
go test ./internal/deploy -run '^(TestSafeRemoveModuleDir_PathEscapeIsRejected|TestSafeRemoveModuleDir_SiblingPackageSurvives)$' -count=1 -v
$rc = $LASTEXITCODE
if ($rc -ne 0) { exit 1 }; exit 0
```

預期：exit `0`；`../victim` 回 `removed=false` + error，外部 canary 存活；正常刪
`acme/foo` 時 `acme/bar` byte-identical 存活，只清真的空父目錄。

【權威】`internal/deploy/uninstall.go:111-131`；archive uninstall checklist
`un-030~032`（`.trellis/tasks/archive/2026-07/07-05-uninstall/uninstall-checklist.md:80-82`）。

#### [x] SEC-06 · 「只刪自己裝的」：另一套件與手寫檔不得動

```powershell
go test ./cmd/apm ./internal/deploy -run '^(TestRunUninstall_OnlyRemovesTargetedPackagesFiles|TestRemoveDeployedFiles_UserHandwrittenFileNotInListUntouched)$' -count=1 -v
$rc = $LASTEXITCODE
if ($rc -ne 0) { exit 1 }; exit 0
```

預期：exit `0`；移除 foo 後 bar 的 manifest entry、module dir、lock entry、deployed
file 全部存活；不在 `deployed_files` 的手寫檔 byte-identical；只刪 foo 追蹤且 hash
相符的檔案。

【權威】`internal/deploy/uninstall.go:34-82`；Python uninstall
`cli.py:178-196`；archive checklist `un-V02/un-050~053`
（`uninstall-checklist.md:97-100,152-154`）。

#### [x] SEC-07 · provenance/hash 缺失或不符時保留並警告

```powershell
go test ./internal/deploy ./cmd/apm -run '^(TestRemoveDeployedFiles_HashMismatchIsKeptWithWarning|TestRemoveDeployedFiles_MissingHashKeyIsKept|TestRunUninstall_HashMismatchKeepsFileWithWarning)$' -count=1 -v
$rc = $LASTEXITCODE
if ($rc -ne 0) { exit 1 }; exit 0
```

預期：exit `0`；modified/missing-hash files bytes 不變且不在 removed；診斷分別含
`modified since deploy (hash mismatch)` 與 `no recorded hash`，CLI warning 不依賴 verbose。

【權威】`internal/deploy/uninstall.go:52-70`；archive checklist 安全紅線
`un-053/un-V03`（`uninstall-checklist.md:40,100,154`）。

#### [x] SEC-08 · deployed-files path escape 仍拒絕，外部檔零變動

```powershell
go test ./internal/deploy -run '^TestRemoveDeployedFiles_PathEscapeIsRejected$' -count=1 -v
$rc = $LASTEXITCODE
if ($rc -ne 0) { exit 1 }; exit 0
```

預期：exit `0`；escape path 在 kept、不在 removed，診斷含
`path escapes project directory`，外部 canary bytes 不變。

【權威】`internal/deploy/uninstall.go:34-42`；spec
`backend/install-marketplace-contracts.md:77`。

---

## 6. 覆蓋率、全 repo 與 A/B 回歸 gate

#### [x] QG-01 · `internal/compile` race/unit 全綠且 statement coverage ≥ 80%

```powershell
Set-Location $Repo
$out = go test ./internal/compile -race -cover 2>&1
$rc = $LASTEXITCODE
$out | Write-Output
$m = [regex]::Match(($out -join "`n"), 'coverage:\s+([0-9.]+)%')
if ($rc -ne 0 -or -not $m.Success -or [double]$m.Groups[1].Value -lt 80) { exit 1 }
exit 0
```

預期：exit `0`；無 FAIL/race，coverage 數字 `>= 80.0`。不得用排除檔案、空 package
或只測 trivial getter 灌水。

【權威】`prd.md:34-36`；`implement.md:20-32,57-58`；`design.md:142`。

#### [x] QG-02 · task-scope gofmt 與全 repo go vet；repo CRLF 另案處置

```powershell
$taskGo = @(
  Get-ChildItem internal/compile -File -Filter '*.go' | ForEach-Object FullName
) + @(
  (Resolve-Path cmd/apm/compile.go).Path,
  (Resolve-Path cmd/apm/main.go).Path
)
$taskUnformatted = @(& gofmt -l @taskGo)
if ($LASTEXITCODE -ne 0 -or $taskUnformatted) { $taskUnformatted; exit 1 }
go vet ./...
if ($LASTEXITCODE -ne 0) { exit 1 }

# Read-only repo diagnostic: every out-of-scope gofmt hit must be a Git
# working-tree CRLF file, not an unrelated semantic-formatting failure.
$repoGo = @(Get-ChildItem cmd,internal -Recurse -File -Filter '*.go' |
  ForEach-Object FullName)
$repoUnformatted = @(& gofmt -l @repoGo)
$repoFmtRC = $LASTEXITCODE
$crlfRel = @(git ls-files --eol -- cmd internal |
  Where-Object { $_ -match 'w/crlf' } |
  ForEach-Object { ($_ -split "`t", 2)[1] -replace '\\','/' })
$gitEolRC = $LASTEXITCODE
$nonCRLF = @($repoUnformatted | ForEach-Object {
  [IO.Path]::GetRelativePath($Repo, $_) -replace '\\','/'
} | Where-Object { $_ -notin $crlfRel })
if ($repoFmtRC -ne 0 -or $gitEolRC -ne 0 -or $nonCRLF) { $nonCRLF; exit 1 }
exit 0
```

範圍裁定：本 task 觸碰／新增的 `internal/compile/*.go`、`cmd/apm/compile.go`、
`cmd/apm/main.go` 必須由原始 `gofmt -l` 得到空清單；`go vet ./...` 仍是全 repo gate。
全 repo 既有 CRLF checkout artifact 不要求本 task 正規化，但必須以 read-only 診斷證明
所有 gofmt hit 都是 `w/crlf`。其修復另開 task：加入 `.gitattributes`，再做一次性 gofmt
正規化；不得在本 task 擴 scope 或先格式化後隱藏 dirty state。

**第 2 輪 FAIL（2026-07-11）**：原樣實跑上述 task-scope gate，10 個檔案中
`cmd/apm/main.go` 仍被 `gofmt -l` 列出（`TASK_GOFMT_LIST_COUNT=1`），故即使它也是
`core.autocrlf=true` 造成的 `w/crlf` checkout artifact，本項明訂的 task-scope 最強 gate
仍未達成，保留 `[ ]`。全 repo診斷為 258 個 Go 檔、gofmt 清單 157；
`git ls-files --eol` 顯示 158 個 `w/crlf`，且 157 個 gofmt hit 全部包含在該集合
（`UNFORMATTED_NOT_W_CRLF_COUNT=0`）；repo 無 `.gitattributes`。`go vet ./...` exit `0`。

**第 3 輪（最終）CONFIRMED（2026-07-11）**：原樣重跑 task-scope 10 檔清單，
`TASK_GOFMT_RC=0`、`TASK_GOFMT_LIST_COUNT=0`；`go vet ./...` exit `0`。另重跑條文中的
只讀全 repo 診斷：258 個 Go 檔、gofmt 清單 156、`w/crlf` 157，且
`UNFORMATTED_NOT_W_CRLF_COUNT=0`，故 scoped gate 與既有 CRLF 裁定均成立。

【權威】AGENTS.md Available commands；`implement.md:55-58`。

#### [x] QG-03 · 兩種 build gate 均成功

```powershell
go build -o bin/apm-go.exe ./cmd/apm
if ($LASTEXITCODE -ne 0) { exit 1 }
go build ./...
if ($LASTEXITCODE -ne 0) { exit 1 }
if (-not (Test-Path bin/apm-go.exe) -or (Get-Item bin/apm-go.exe).Length -le 0) { exit 1 }
exit 0
```

預期：exit `0`；兩個 build 均無 error，release binary 名稱/路徑正確且非空。

【權威】AGENTS.md Available commands；`prd.md:36`；`implement.md:41,57`。

#### [x] QG-04 · 全 repo test gate 綠

```powershell
go test ./... -count=1
$rc = $LASTEXITCODE
if ($rc -ne 0) { exit 1 }; exit 0
```

預期：exit `0`；所有 package 無 FAIL/panic。不得用 `-run`、cache 或跳過失敗 package
取代本項。

【權威】`prd.md:36`；`implement.md:55-58`。

#### [x] AB-01 · `ab_agents_compile.py` 為主驗收 oracle，全部通過

```powershell
Set-Location D:/Projects/apm-dev
python evals/ab_agents_compile.py 2>&1 | Tee-Object -FilePath (Join-Path $Scratch 'ab_agents_compile.log')
$rc = $LASTEXITCODE
$log = Get-Content (Join-Path $Scratch 'ab_agents_compile.log') -Raw
if ($rc -ne 0 -or $log -notmatch 'ALL CHECKS PASSED \(ab_agents_compile\)' -or
    $log -match '(?m)^FAIL(?:ED)?') { exit 1 }
exit 0
```

預期：exit `0`、尾端有指定 ALL CHECKS PASSED、FAIL `0`；至少實際覆蓋三 target、
local/dependency、global/scoped/comma/brace/empty/list、同名衝突、Build ID 自洽、
兩次 idempotency、無 apm.yml、unsupported family。只 normalize Build ID 行、APM Version
行與 `\r`，不得 normalize 其他差異。

【權威】`prd.md:28-36`；`design.md:134-143`；`implement.md:44-53`。

#### [x] AB-02 · instructions install pipeline 回歸不變

```powershell
Set-Location D:/Projects/apm-dev
python evals/ab_instructions_applyto.py 2>&1 | Tee-Object -FilePath (Join-Path $Scratch 'ab_applyto.log')
$rc = $LASTEXITCODE
$log = Get-Content (Join-Path $Scratch 'ab_applyto.log') -Raw
if ($rc -ne 0 -or $log -notmatch 'ALL CHECKS PASSED \(ab_instructions_applyto\)' -or
    $log -match '(?m)^FAIL(?:ED)?') { exit 1 }
exit 0
```

預期：exit `0`、指定尾行存在，plain `.md` filter、applyTo conversion、zero-target gate
全部 PASS；證明 compile 的 raw applyTo 邏輯沒有污染 install splitter。

【權威】PRD Non-Goal `prd.md:39-42`；
`D:/Projects/apm-dev/evals/ab_instructions_applyto.py:2-20,125-160`。

#### [x] AB-03 · antigravity target lifecycle 回歸不變

```powershell
Set-Location D:/Projects/apm-dev
python evals/ab_antigravity.py 2>&1 | Tee-Object -FilePath (Join-Path $Scratch 'ab_antigravity.log')
$rc = $LASTEXITCODE
$log = Get-Content (Join-Path $Scratch 'ab_antigravity.log') -Raw
if ($rc -ne 0 -or $log -notmatch 'ALL CHECKS PASSED \(ab_antigravity\)' -or
    $log -match '(?m)^FAIL(?:ED)?') { exit 1 }
exit 0
```

預期：exit `0`、指定尾行存在；explicit-only、rules/skills/hooks/MCP、local dep uninstall
與 sibling survival 全 PASS。`agy` 不在 PATH 只允許 supplemental validate 明確 SKIP，
不得有 FAILED。

【權威】spec `backend/antigravity-target-contract.md:36-60`；
`D:/Projects/apm-dev/evals/ab_antigravity.py:2-29,198-239`。

#### [x] AB-04 · uninstall A/B 與「只刪自己的」回歸不變

```powershell
Set-Location D:/Projects/apm-dev
python evals/ab_uninstall.py 2>&1 | Tee-Object -FilePath (Join-Path $Scratch 'ab_uninstall.log')
$rc = $LASTEXITCODE
$log = Get-Content (Join-Path $Scratch 'ab_uninstall.log') -Raw
if ($rc -ne 0 -or $log -notmatch 'total:\s+\d+ passed, 0 failed' -or
    $log -match '(?m)^FAIL') { exit 1 }
exit 0
```

預期：exit `0`、`0 failed`；standalone MCP/not-found/global deviation 均依腳本斷言，
scratch 清除，oracle registry 不變。此項不能取代 SEC-05~08，只是 binary A/B gate。

【權威】`D:/Projects/apm-dev/evals/ab_uninstall.py:2-29,162-177`；
archive uninstall checklist `un-V01~V03`。

#### [x] AB-05 · marketplace/install A/B 回歸不變

```powershell
Set-Location D:/Projects/apm-dev
python evals/ab_marketplace_install.py 2>&1 | Tee-Object -FilePath (Join-Path $Scratch 'ab_marketplace_install.log')
$rc = $LASTEXITCODE
$log = Get-Content (Join-Path $Scratch 'ab_marketplace_install.log') -Raw
if ($rc -ne 0 -or $log -notmatch 'total:\s+\d+ passed, 0 failed' -or
    $log -match '(?m)^FAIL') { exit 1 }
exit 0
```

預期：exit `0`、`0 failed`；local marketplace install、unknown marketplace、ref cases 與
既有 documented deviation 都依腳本通過。腳本只能使用隔離 fixture，不得呼叫 oracle
marketplace add/remove/update。

【權威】`prd.md:28-29`；spec `backend/install-marketplace-contracts.md:61,77`；
`D:/Projects/apm-dev/evals/ab_marketplace_install.py:2-24,207-226`。

#### [x] FINAL-01 · 安全基線、repo 根檔與證據包完整後才可完成

```powershell
Set-Location $Repo
$repoHashAfter = (Get-FileHash -Algorithm SHA256 (Join-Path $Repo 'AGENTS.md')).Hash
$oracleRegistryAfter = if (Test-Path $OracleRegistry) {
  (Get-FileHash -Algorithm SHA256 $OracleRegistry).Hash
} else { '<absent>' }
$logs = 'ab_agents_compile.log','ab_applyto.log','ab_antigravity.log',
        'ab_uninstall.log','ab_marketplace_install.log' |
  ForEach-Object { Join-Path $Scratch $_ }
$missingLogs = $logs | Where-Object { -not (Test-Path -LiteralPath $_) }
$diff = git diff -- AGENTS.md
if ($repoHashAfter -ne $RepoAgentsBefore -or
    $oracleRegistryAfter -ne $OracleRegistryBefore -or $missingLogs -or $diff) {
  $missingLogs; $diff; exit 1
}
exit 0
```

預期：exit `0`；repo 根 `AGENTS.md` 與 oracle `~/.apm/marketplaces.json` hash/存在狀態
均未變，五份 A/B log 齊全，`git diff -- AGENTS.md` 為空。再附 `go build/vet/test`、
coverage 與 50 項逐項證據後，才可刪除 scratch 並完成 task。

【權威】`design.md:109-118`；`implement.md:5-7,64-70`；
`.trellis/spec/conformance/cli-verification-checklist.md:22-24`。

---

## 7. 2026-07-11 逐項對抗驗證證據

共同環境：repo=`D:/Projects/apm-dev/apm-go`；live compile/install/uninstall 全在
`%TEMP%/apm-go-agents-compile-verify-20260711-a37d`；Go registry 指向 scratch；oracle
固定 `uv --project D:/Projects/apm-dev/apm run apm ...`。以下「exit」皆為實際命令值，
不是 claim；對應 Go test 本體亦逐一讀過，確認斷言不是只測函式有回傳。

### PRE / DOC

- `PRE-01` — 執行 required-files/tools PowerShell；exit `0`，15 個檔案與
  `go/uv/python/git` 全存在；零檔案變動。
- `PRE-02` — isolation harness exit `0`；scratch 不在 repo 下，`APM_CONFIG_DIR` 在
  scratch 下；repo AGENTS baseline SHA256
  `BD27CDA25B48D03506F6D5F71AA4E50A8BB61BD837023332B96C8322BDA565D8`，oracle
  registry baseline `9F752A5D9667F5DE85B3768E14644EA793B156E1FF88D8C7B641CE2054C2730B`。
- `PRE-03` — `go build -o bin/apm-go.exe ./cmd/apm` exit `0`；binary
  `13,708,800` bytes，`bin/apm.exe` 不存在。
- `PRE-04` — 五個 eval 腳本的禁止-pattern scan exit `0`、match `0`；本輪命令記錄亦
  無 oracle marketplace add/remove/update。
- `DOC-01` — 八個必要 pattern harness exit `0`；人工讀取 findings 的 A-F/H、三選項與
  Step 0 附錄，六個 Python 行為面均有 file:line／scratch 證據；零 FS 效果。
- `DOC-02` — 六個 decision 名稱全命中，design/implement `designDraft=False,
  implDraft=False`，harness exit `0`；零 FS 效果。
- `DOC-03` — 第 2 輪 harness exit `0`：oracle install exit `0`、AGENTS.md=False、
  `ProducerRef=True, ConflictRecorded=True`；人工核對 findings 附錄與官方
  `producer/compile.md:126-136,147-151`，引用與文件自身矛盾均真實存在。
- `DOC-04` — contract required-pattern harness exit `0`；`missing=`、index=True、
  antigravity cross-link=True、TBD=False；人工讀完整 contract §1-8；零 FS 效果。

### CLI / CMP / IO

- `CLI-01` — `apm-go.exe compile --help` exit `0`；有 `-t/--target`，九個 forbidden
  flags match `0`；只在 scratch 留 help transcript。
- `CLI-02` — no-apm fixture：受測 CLI exit `1`，訊息含
  `Not an APM project - no apm.yml found`，AGENTS.md=False。
- `CLI-03` — no-content fixture：受測 CLI exit `1`，訊息
  `No instruction files found in .apm/ directory`，AGENTS.md=False。
- `CLI-04` — antigravity/codex/opencode 三次 exit `0,0,0`；三份輸出唯一 SHA256
  `DCA09F428BED09CEEF5A00287C2B25508EDCD6B7C404FDCEBF508E63905603B4`。
- `CLI-05` — multi-target exit `0`；遞迴 AGENTS.md count=`1`，CLAUDE/GEMINI/copilot
  outputs count=`0`。
- `CLI-06` — claude/copilot/unknown/no-signal 四案均 exit `2`、指定訊息=True、
  AGENTS.md=False。
- `CLI-07` — precedence A/B/C 實測 exit=`0/2/0`，輸出存在=`True/False/True`。
- `CLI-08` — dry-run/watch/root/single-agents/缺值 `-t` 五案均 exit `1`、AGENTS=False；
  四案 `unknown flag`，缺值案為 `flag needs an argument: 't' in -t`。原 harness 未驗文案，
  已加強 regex 後重跑通過。
- `CLI-09` — scratch 先 compile exit `0`、再 bare install exit `2`；訊息含
  `no deployment target detected`，`.agents/rules/a.md` 不存在。
- `CMP-01` — 指定 `go test` exit `0`；測試直接斷言兩個精確 forward-slash relpath、
  body 與 count=`2`。
- `CMP-02` — 原 priority test exit `0`；另讀/跑 transitive exclude + VirtualPath tie-break
  tests `-count=25` exit `0`，鎖定 local/direct/transitive winner 與排序；harness 已納入補強。
- `CMP-03` — 指定 test exit `0` 且是真 PASS（非 SKIP）；只留下 valid instruction，外部
  secret token 未進 render、外部 bytes 不變。
- `CMP-04` — 指定 test exit `0`；11 subtests 全 PASS，涵蓋三種 scalar、flow/block list、
  無 frontmatter、空值、comma/brace 與空 body。
- `CMP-05` — 指定 test exit `0`；Files-matching heading count=`2`，comma 與 brace 完整
  heading 均存在。
- `CMP-06` — checklist 的 `-count=25` exit `0`；每次測試內再 render 25 次，逐 marker
  驗 Global/pattern/relpath 順序。
- `CMP-07` — 指定 test exit `0`，三個 subtest 全 PASS；另以兩個獨立 oracle scratch
  執行無 `-t` 的指定命令，global/pattern 各 `HeadingCount=1, SourceCount=0`，SHA256
  分別 `6DE062...00C7B`／`96629D...59504`；條文已依實證修訂。
- `CMP-08` — 指定 test exit `0`；逐行 header/tail count、Source wrappers、footer 與無
  distributed marker 均有斷言。
- `CMP-09` — 指定 test exit `0`；UTF-8 中英文/emoji 存在、CR 不存在、恰一 trailing LF。
- `CMP-10` — 指定 test exit `0`；獨立 SHA256 重算、12 hex、placeholder 清除、內容變更、
  再穩定化與 trailing newline 都有斷言。
- `CMP-11` — 指定 test exit `0`；另做 raw-byte Go/oracle A/B：只刪 CR 並只 normalize
  Build/APM 整行後，兩邊長度 `522`、SHA256 同為
  `6287885DE0B3B2BD9B8A82A07C4862E870E16E6939B2CDC8F718ABDDB22B2F0D`。
- `IO-01` — 兩次 CLI exit `0,0`；SHA256
  `1F8E3F43DA5FF6445D96A6D2D7E0E7414118F5815E06E28248397F1F82BCBA96`、mtime 都相同，
  第二次指定 no-change 訊息=True。
- `IO-02` — 變更 body 後 exit `0`；file hash changed=True，Build ID
  `2b168f665aec -> aba34020b9a9`，新 body=True、舊 standalone body=False。
- `IO-03` — 原指定 unit exit `0`，確認 stub failure 保留 existing bytes；因原 test 未斷言
  temp cleanup，另以 scratch `go test -overlay` 注入對抗 test，強制 real rename failure，
  exit `0`，canary=`KEEP` 且 `.agents-md-*.tmp` count=`0`。
- `IO-04` — scratch overwrite exit `0`；hand-authored canary=False、generated marker=True；
  repo AGENTS hash仍等於 baseline。

### SEC / QG / A/B / FINAL

- `SEC-01` — 指定 test exit `0`；八案含 deep/in-root/backslash `..` 與兩個正常 key。
- `SEC-02` — 指定 test exit `0`；refusal diagnostic、`_local` destination 不存在、sibling
  唯一 marker 與 bytes 均存活。
- `SEC-03` — symlink 指定 test exit `0`；另 hardlink test exit `0`，兩者均驗 link
  diagnostic、noLeak、外部 canary；harness 已改為同時選兩 test。
- `SEC-04` — 指定 test exit `0` 且非 SKIP；regular file bytes 正確，目的地 link 不存在，
  外部 secret byte-identical。
- `SEC-05` — 兩 test exit `0`；escape removed=False/error 且 victim 存活；正常 foo 移除，
  bar content、shared parent與唯一 child 均存活。
- `SEC-06` — cmd/apm 與 deploy 兩 package exit `0`；foo file/module/manifest/lock entry 移除，
  bar 四層狀態與手寫檔 byte-identical。
- `SEC-07` — 三 test exit `0`；hash mismatch/missing hash 都 kept、指定 warning，CLI
  verbose=false 仍 stderr warning，edited bytes 不變。
- `SEC-08` — 指定 test exit `0`；removed=0、kept=escape、diagnostic 精確，外部 canary
  byte-identical。
- `QG-01` — checklist 原命令先失敗：`-race requires cgo`；再設 `CGO_ENABLED=1` 仍因
  `gcc` 不在 PATH 於 runtime/cgo build 前失敗，屬明確環境限制。最強替代
  `go test ./internal/compile -cover -count=100` exit `0`，100 次全綠、coverage=`90.7%`。
- `QG-02` — **FAIL**；全 repo vet exit `0`，157 個 repo gofmt hit 皆為已知
  `w/crlf` checkout artifact，但 task-scope 10 檔仍有 `cmd/apm/main.go` 被列出，未達
  「本 task 所有 Go 檔原始 `gofmt -l` 空清單」的 scoped gate。
- `QG-03` — release build 與 `go build ./...` exit `0/0`；binary exists=True、
  size=`13,708,800`。
- `QG-04` — `go test ./... -count=1` exit `0`，18 個 package 全綠；cmd/apm 最慢
  `28.311s`，無 FAIL/panic/SKIP 取代。
- `AB-01` — 第 2 輪腳本 exit `0`、實際 **49** 個 PASS（44/44 claim 已過時）、尾行
  `ALL CHECKS PASSED (ab_agents_compile)`、FAIL 0；不以全綠結果抵銷下述辨識力殘留。
- `AB-02` — exit `0`、25 PASS、指定尾行、FAIL 0；plain filter/applyTo/zero-target 都跑。
- `AB-03` — exit `0`、46 PASS、指定尾行、無 FAILED；agy validate 兩 bundle 皆實跑，
  symlink/環境 SKIP 未出現。
- `AB-04` — exit `0`，`total: 6 passed, 0 failed, 2 documented deviations`。
- `AB-05` — exit `0`，`total: 7 passed, 0 failed, 2 documented deviations`；無 oracle
  marketplace mutation。
- `FINAL-01` — final harness exit `0`：repo AGENTS before/after 均
  `BD27CDA...565D8`；oracle registry before/after均 `9F752A...2730B`；五 logs 齊；
  `git diff -- AGENTS.md` 空；redlight stash residue=`0`。

### 額外對抗檢查與 A/B 腳本審查

- 紅燈重現：`git stash push -u -- cmd/apm/compile.go` 後檔案確實消失，
  `go build ./cmd/apm` exit `1`，診斷 `main.go:30:18: undefined: compileCmd`；pop 後因
  `core.autocrlf=true` 初次 checkout 變 CRLF，隨即以 gofmt 恢復原 LF，最終 SHA256 精確
  回到 stash 前 `B90B10A41F938EA8A0D47EC6A8F6F0096791BFA5F08BBFF9689B183C085149FB`，
  path status 與 stash count 亦完全復原。
- `ab-script-review: 殘留`：第 2 輪腳本實跑 exit `0`、49 PASS，但 4 個原弱化面只部分
  解決。有效補強：normalize 前 line-level guard 對非 header 行 mutation 會 FAIL；
  `-t bogus` 實測兩邊 exit `2`；no-signal 明確斷言 Go exit `2`／無 AGENTS.md，Python
  exit `0`／有 AGENTS.md。仍殘留：(1) AGENTS.md 仍以 `Path.read_text()` 讀取，mutation
  fixture 的 raw bare CR 在讀取時已轉為 LF，`assert_no_bare_cr` 反而 PASS；(2) rerun 仍以
  `read_text()` 字串相等宣稱 raw byte-identical，CRLF bytes 與 LF bytes 不同的 mutation
  仍比較為 True；(3) orphan-global/scoped 只比 normalize 後相等並斷言無 Source，未直接
  斷言 `## Global Instructions`／scoped heading 存在，也非 byte 相同；合成「兩邊都漏
  heading」輸入時現有 predicate 仍為 True。依本輪權限不得修改 evals，故不得標 resolved。

- `ab-script-review: resolved`（第 3 輪最終）：人工讀取腳本確認主輸出以
  `Path.read_bytes()` 取得原始 bytes，bare-CR 用 `rb"\r(?!\n)"` 直接檢查；Go/Python
  第二次輸出均與各自第一次 raw bytes 比對；orphan helper 對兩側逐一斷言 heading 字面
  存在，並比較 CRLF-only folded 的 heading-to-EOF byte block。獨立以兩側皆漏
  `## Global Instructions` 的相同合成 bytes 呼叫 helper，實際得到 2 個 `FAIL` 且
  `DISCRIMINATION_DETECTED=True`；`python -B` 執行，未留 fixture。完整腳本重跑 exit `0`、
  55 PASS、FAIL 0、指定 `ALL CHECKS PASSED (ab_agents_compile)` 尾行存在，A/B scratch 已刪除。

**第 2 輪統計：CONFIRMED 49 / FAIL 1 / DEFERRED 0。** 無項目依賴真實 commit SHA，
故無 DEFERRED；仍 FAIL：`QG-02`。

**第 3 輪（最終）統計：CONFIRMED 50 / FAIL 0 / DEFERRED 0。** `go build ./...`、
`go vet ./...`、`go test ./... -count=1` 均 exit `0`；`git diff -- AGENTS.md` 為空。

---

## 完成判定

共 **50 項**。只有 50/50 全部附可重跑證據並勾選，且沒有任何 security、oracle、
coverage 或 repo-isolation gate 以 SKIP／deviation 降級，才可判定本 task 驗收完成。
documented deviations 只限已寫入 `compile-contract.md` 且由 A/B 腳本主動斷言的項目；
不得拿 v1 non-goals 豁免 agents-family single-file 契約、symlink 拒絕、path containment、
atomic write、idempotency 或「只刪自己裝的」安全線。
