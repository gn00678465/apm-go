# Codex agents MD→TOML 硬性驗證清單

> **圖例**：`[ ]` 待驗 · `[x]` 已驗——附證據才可勾。
>
> **判定規則**：每項命令與斷言全部成立才是 PASS；任一 exit code、檔案效果、TOML key/value、hash 或輸出 signature 不符即 FAIL。不得以目視「看起來正確」代替證據。

---

## 0. 執行前置與安全

- **Build**：只從 repo root 執行 `go build -o bin/apm-go.exe ./cmd/apm`，binary 固定叫 `apm-go.exe`。
- **Python oracle**：只用 `uv --project D:/Projects/apm-dev/apm run apm <args>`。
- **隔離**：所有 install / oracle / A/B / test1 重現都必須在 `%TEMP%` 的 throwaway scratch；禁止在 apm-go repo、Python oracle repo、`D:/Projects/apm-dev/evals/test1` 原目錄執行。
- **硬性禁止**：不得對 Python oracle 執行任何 `marketplace add`、`marketplace remove`、`marketplace update`；它們會改真實 `~/.apm/marketplaces.json`。
- **Exit code**：PowerShell 一律立即保存 `$LASTEXITCODE`，負向案例必須驗精確數值。

### [x] CAT-01 · 建立 TEMP scratch、基準 commit 與硬斷言 helper

**證據**：exit 0；scratch=`C:\Users\gn006\AppData\Local\Temp\apm-go-codex-agent-toml-adversarial-1783822986032`，位於 TEMP 且不等於三個受保護 tree；baseline=`53acf46438b7298848e84207d731db1d74fb2ba8`（40-hex）。

```powershell
$Repo = (Resolve-Path 'D:/Projects/apm-dev/apm-go').Path
$OracleRepo = (Resolve-Path 'D:/Projects/apm-dev/apm').Path
$Test1 = (Resolve-Path 'D:/Projects/apm-dev/evals/test1').Path
$TempRoot = [IO.Path]::GetFullPath([IO.Path]::GetTempPath())
$Scratch = Join-Path $TempRoot ('apm-go-codex-agent-toml-' + [guid]::NewGuid().ToString('N'))
$Base = (& git -C $Repo rev-parse HEAD).Trim()
function Assert([bool]$Condition, [string]$Message) { if (-not $Condition) { throw $Message } }

New-Item -ItemType Directory -Path $Scratch | Out-Null
$ScratchFull = [IO.Path]::GetFullPath($Scratch)
Assert ($ScratchFull.StartsWith($TempRoot, [StringComparison]::OrdinalIgnoreCase)) 'scratch is not under TEMP'
Assert ($ScratchFull -ne $Repo -and $ScratchFull -ne $OracleRepo -and $ScratchFull -ne $Test1) 'scratch aliases a protected tree'
Assert (Test-Path $Scratch -PathType Container) 'scratch directory missing'
Assert ($Base -match '^[0-9a-f]{40}$') 'baseline commit is not a full git hash'
```

預期：exit 0；`$Scratch` 是 `%TEMP%` 下的新目錄，與三個受保護 tree 不同；`$Base` 為 40-hex。後續所有暫存證據只寫 `$Scratch`。

**權威來源**：`.trellis/spec/conformance/cli-verification-checklist.md:17-24`；`research/findings.md:54-55`。

### [x] CAT-02 · 固定名稱 binary build

**證據**：`go build -o bin/apm-go.exe ./cmd/apm` exit 0；`bin/apm-go.exe` 存在，`bin/apm.exe` 不存在。

```powershell
Push-Location $Repo
try {
    & go build -o bin/apm-go.exe ./cmd/apm
    $Code = $LASTEXITCODE
} finally { Pop-Location }
$Bin = (Resolve-Path (Join-Path $Repo 'bin/apm-go.exe')).Path
Assert ($Code -eq 0) "fixed binary build exit=$Code"
Assert (Test-Path $Bin -PathType Leaf) 'bin/apm-go.exe missing'
```

預期：build exit 0，且 `bin/apm-go.exe` 存在；不得產生 `bin/apm.exe`。

**權威來源**：`.trellis/spec/conformance/cli-verification-checklist.md:19`；`prd.md:35`。

---

## 1. TDD、依賴與部署層契約

### [x] CAT-03 · TDD 先紅後綠證據不可省略

**證據**：以 `git stash push -u -- internal/deploy/codex.go internal/deploy/codex_agent.go` 真正移除 tracked+untracked 實作；red exit 1，8 處 `undefined: transformCodexAgent` 且 package `build failed`。pop 後 stash count、status、index、`codex_agent.go` hash 與 `codex.go` Git diff 全復原；green exit 0，兩個 named tests PASS、無 SKIP/FAIL。transcript：scratch `tdd-red.txt` / `tdd-green.txt`。

在只加入測試、尚未加入實作時先執行第一段；若驗證時實作已在 working tree，先用下列限定 path 的 stash 重現 red，再立即 pop 並硬比對 working diff 已復原。`-u` 不可省略，否則 untracked 的 `codex_agent.go` 不會被 stash。兩份 transcript 都留在 scratch。

```powershell
$Pattern = '^(TestCodexAgentTransform|TestDeployCodexAgentTOML)$'
$BeforeDiff = @(& git -C $Repo diff --binary HEAD -- internal/deploy/codex.go)
$BeforeStatus = @(& git -C $Repo status --porcelain=v1 -uall)
$BeforeAgentHash = (Get-FileHash (Join-Path $Repo 'internal/deploy/codex_agent.go') -Algorithm SHA256).Hash
$BeforeStashCount = @(& git -C $Repo stash list).Count
& git -c core.autocrlf=false -C $Repo stash push -u -- internal/deploy/codex.go internal/deploy/codex_agent.go
Assert ($LASTEXITCODE -eq 0) 'red-light stash failed'
Assert (@(& git -C $Repo stash list).Count -eq $BeforeStashCount + 1) 'red-light stash was not created'
try {
    Push-Location $Repo
    try {
        $Red = @(& go test ./internal/deploy/ -run $Pattern -count=1 -v 2>&1)
        $RedCode = $LASTEXITCODE
    } finally { Pop-Location }
} finally {
    & git -c core.autocrlf=false -C $Repo stash pop
    Assert ($LASTEXITCODE -eq 0) 'red-light stash pop failed'
}
$Red | Set-Content -Encoding utf8 (Join-Path $Scratch 'tdd-red.txt')
$RedText = $Red | Out-String
$TestSource = Get-Content -Raw (Join-Path $Repo 'internal/deploy/codex_agent_test.go')
Assert ($RedCode -ne 0) "red run unexpectedly passed: exit=$RedCode"
Assert ($TestSource -match 'func TestCodexAgentTransform\(' -and $TestSource -match 'func TestDeployCodexAgentTOML\(') 'both red tests are not defined'
Assert ($RedText -match 'undefined: transformCodexAgent' -and $RedText -match 'build failed') 'implementation-absent compile-red signature missing'
Assert (@(& git -C $Repo stash list).Count -eq $BeforeStashCount) 'temporary stash remains after red run'
Assert ((@(& git -C $Repo diff --binary HEAD -- internal/deploy/codex.go) -join "`n") -ceq ($BeforeDiff -join "`n")) 'codex.go diff not restored'
Assert ((@(& git -C $Repo status --porcelain=v1 -uall) -join "`n") -ceq ($BeforeStatus -join "`n")) 'working-tree status not restored'
Assert ((Get-FileHash (Join-Path $Repo 'internal/deploy/codex_agent.go') -Algorithm SHA256).Hash -eq $BeforeAgentHash) 'codex_agent.go not restored'
```

```powershell
Push-Location $Repo
try {
    $Green = @(& go test ./internal/deploy/ -run $Pattern -count=1 -v 2>&1)
    $GreenCode = $LASTEXITCODE
} finally { Pop-Location }
$Green | Set-Content -Encoding utf8 (Join-Path $Scratch 'tdd-green.txt')
$GreenText = $Green | Out-String
Assert ($GreenCode -eq 0) "green run exit=$GreenCode"
foreach ($Name in @('TestCodexAgentTransform','TestDeployCodexAgentTOML')) {
    Assert ($GreenText -match ('--- PASS: ' + [regex]::Escape($Name))) "$Name PASS signature missing"
}
Assert ($GreenText -notmatch '--- SKIP:|--- FAIL:') 'green run contains SKIP/FAIL'
```

預期：test-only run 精確非零；實作缺席造成 package compile-red 時，以兩個測試定義存在 + 缺失實作符號/build-failed signature 為證（Go 不會在 package 編譯失敗時印 named RUN）；完成後 exit 0、兩個 named tests PASS、無 SKIP/FAIL，且 stash 前後 diff/status/hash 完全復原。

**權威來源**：`prd.md:30-32`；`agent_integrator.py:302-335`。

### [x] CAT-04 · 重用既有 TOML v2，禁止新增依賴

**證據**：`go.mod`/`go.sum` 相對 baseline diff=0；既有 `pelletier/go-toml/v2 v2.4.2` 存在，MCP writer 與 Codex agent path 使用相同 import。

```powershell
$ModDiff = @(& git -C $Repo diff --name-only $Base -- go.mod go.sum)
Assert ($LASTEXITCODE -eq 0) 'git diff for module files failed'
Assert ($ModDiff.Count -eq 0) ('go.mod/go.sum changed: ' + ($ModDiff -join ', '))
$GoMod = Get-Content -Raw (Join-Path $Repo 'go.mod')
$MCPWriter = Get-Content -Raw (Join-Path $Repo 'internal/deploy/mcp_codex.go')
$CodexFiles = (Get-ChildItem (Join-Path $Repo 'internal/deploy') -File -Filter 'codex*.go' | ForEach-Object { Get-Content -Raw $_.FullName }) -join "`n"
Assert ($GoMod -match 'github\.com/pelletier/go-toml/v2\s+v2\.4\.2') 'existing TOML module/version missing'
Assert ($MCPWriter -match '"github\.com/pelletier/go-toml/v2"') 'MCP writer TOML import missing'
Assert ($CodexFiles -match 'github\.com/pelletier/go-toml/v2') 'Codex agent path does not reuse existing TOML library'
```

預期：exit 0；`go.mod`/`go.sum` 相對 `$Base` 無變更；agent transformer 與 MCP writer 使用同一既有 `github.com/pelletier/go-toml/v2`。

**權威來源**：`prd.md:23`；`research/findings.md:39-40`；`internal/deploy/mcp_codex.go:9,110,117`。

### [x] CAT-05 · 部署層輸出路徑、合法 TOML 與三鍵齊全

**證據**：named unit exit 0/PASS、無 SKIP/FAIL；另建單 agent live scratch，遞迴列舉 `.codex/agents` 恰只有 `helper.toml`，parse 後 exact 三鍵且三值正確。

```powershell
Push-Location $Repo
try {
    $Out = @(& go test ./internal/deploy/ -run '^TestDeployCodexAgentTOML$' -count=1 -v 2>&1)
    $Code = $LASTEXITCODE
} finally { Pop-Location }
$Text = $Out | Out-String
Assert ($Code -eq 0) "deployment TOML test exit=$Code"
Assert ($Text -match '--- PASS: TestDeployCodexAgentTOML') 'deployment PASS signature missing'
Assert ($Text -notmatch '--- SKIP:|--- FAIL:') 'deployment test contains SKIP/FAIL'
```

測試內硬斷言：部署回傳且只寫 `.codex/agents/<p.Name>.toml`；輸出可由 TOML v2 parser 解析；解析後 key set 恰為 `name`、`description`、`developer_instructions`，不得接受 markdown byte-copy 冒充 `.toml`。

預期：exit 0、named test PASS；路徑維持 `<p.Name>.toml`，TOML parse 成功且恰三鍵。

**權威來源**：`prd.md:25,30-32`；`agent_integrator.py:330-335`；`research/findings.md:19-20,44`。

---

## 2. Python oracle 六點逐項語意

### [x] CAT-06 · Oracle #1：symlink source 必須拒讀

**證據**：Windows 環境實際建立 symlink 成功；named case exit 0/PASS、無 SKIP/FAIL，transformer 回 error，未 follow link。

```powershell
Push-Location $Repo
try {
    $Out = @(& go test ./internal/deploy/ -run '^TestCodexAgentTransform$/^rejects_symlink$' -count=1 -v 2>&1)
    $Code = $LASTEXITCODE
} finally { Pop-Location }
$Text = $Out | Out-String
Assert ($Code -eq 0) "symlink case exit=$Code"
Assert ($Text -match '--- PASS: TestCodexAgentTransform/rejects_symlink') 'symlink rejection PASS missing'
Assert ($Text -notmatch '--- SKIP:|--- FAIL:') 'symlink case SKIP/FAIL is not acceptable'
```

預期：exit 0；直接交給 Codex transformer/deploy path 的 symlink source 回傳 error，target 不存在；不得 follow link、不得以 collection 層已過濾作為跳過此案例的理由。

**權威來源**：`agent_integrator.py:308-309`；`research/findings.md:26,45-46`。

### [x] CAT-07 · Oracle #2：name fallback 剝除 `.agent`

**證據**：named case exit 0/PASS；測試本體硬斷言 `accessibility-runtime-tester.agent.md` → `accessibility-runtime-tester`。

```powershell
Push-Location $Repo
try {
    $Out = @(& go test ./internal/deploy/ -run '^TestCodexAgentTransform$/^name_fallback_strips_agent$' -count=1 -v 2>&1)
    $Code = $LASTEXITCODE
} finally { Pop-Location }
$Text = $Out | Out-String
Assert ($Code -eq 0) "name fallback case exit=$Code"
Assert ($Text -match '--- PASS: TestCodexAgentTransform/name_fallback_strips_agent') 'name fallback PASS missing'
```

預期：`accessibility-runtime-tester.agent.md` 且無有效 frontmatter name 時，TOML `name == "accessibility-runtime-tester"`；不得是 `accessibility-runtime-tester.agent`。

**權威來源**：`agent_integrator.py:314-316`；`research/findings.md:27`。

### [x] CAT-08 · Oracle #3a：開頭 frontmatter 覆寫 name/description，其他 key 忽略

**證據**：named case exit 0/PASS；測試本體斷言 name/description 覆寫、body 不含 frontmatter、model/tools 不進 exact 三鍵 TOML。

```powershell
Push-Location $Repo
try {
    $Out = @(& go test ./internal/deploy/ -run '^TestCodexAgentTransform$/^frontmatter_overrides$' -count=1 -v 2>&1)
    $Code = $LASTEXITCODE
} finally { Pop-Location }
$Text = $Out | Out-String
Assert ($Code -eq 0) "frontmatter case exit=$Code"
Assert ($Text -match '--- PASS: TestCodexAgentTransform/frontmatter_overrides') 'frontmatter PASS missing'
```

預期：只匹配檔案開頭 `^---\s*\n...\n---\s*\n?`；合法 YAML 的 `name`/`description` 覆寫 fallback；`model`、`tools` 等額外 key 不出現在 TOML；body 不含 frontmatter。

**權威來源**：`agent_integrator.py:296-299,320-326`；`research/findings.md:28-31`。

### [x] CAT-09 · Oracle #3b 負向：YAML 壞損靜默，frontmatter 仍切除

**證據**：named case exit 0/PASS；未閉合 quote 不回 error，name=`bad-yaml`、description=`""`、body=`bad body`，壞 YAML 未滲入 body。

```powershell
Push-Location $Repo
try {
    $Out = @(& go test ./internal/deploy/ -run '^TestCodexAgentTransform$/^malformed_yaml_is_silent$' -count=1 -v 2>&1)
    $Code = $LASTEXITCODE
} finally { Pop-Location }
$Text = $Out | Out-String
Assert ($Code -eq 0) "malformed YAML case exit=$Code"
Assert ($Text -match '--- PASS: TestCodexAgentTransform/malformed_yaml_is_silent') 'malformed YAML PASS missing'
```

預期：matched frontmatter 內放未閉合 quote 時 transformer 不回 error；TOML 仍可 parse，`name` 使用剝 `.agent` fallback、`description == ""`，`developer_instructions` 只有 frontmatter 後 body。若壞 YAML 原文滲入 body 或整次失敗即 FAIL。

**權威來源**：`agent_integrator.py:320-328`；`research/findings.md:28-31`。

### [x] CAT-10 · Oracle #4：description 預設空字串

**證據**：named case exit 0/PASS；no-frontmatter、無 description frontmatter、malformed frontmatter 三子案例都 parse 出存在且精確為空字串的 `description`。

```powershell
Push-Location $Repo
try {
    $Out = @(& go test ./internal/deploy/ -run '^TestCodexAgentTransform$/^description_defaults_empty$' -count=1 -v 2>&1)
    $Code = $LASTEXITCODE
} finally { Pop-Location }
$Text = $Out | Out-String
Assert ($Code -eq 0) "description default case exit=$Code"
Assert ($Text -match '--- PASS: TestCodexAgentTransform/description_defaults_empty') 'description default PASS missing'
```

預期：無 description、空 frontmatter、壞 YAML 三種輸出解析後都存在 `description` key 且值精確等於 `""`；不得省略、不得為 null。

**權威來源**：`agent_integrator.py:317,326,330-333`；`research/findings.md:32-34`。

### [x] CAT-11 · Oracle #5：三鍵 exact set，body 使用 `.strip()`

**證據**：named case exit 0/PASS；外側 whitespace 被去除、內部空行/縮排保留，parsed TOML key set 恰三鍵。

```powershell
Push-Location $Repo
try {
    $Out = @(& go test ./internal/deploy/ -run '^TestCodexAgentTransform$/^body_is_trimmed_and_keys_exact$' -count=1 -v 2>&1)
    $Code = $LASTEXITCODE
} finally { Pop-Location }
$Text = $Out | Out-String
Assert ($Code -eq 0) "body/key case exit=$Code"
Assert ($Text -match '--- PASS: TestCodexAgentTransform/body_is_trimmed_and_keys_exact') 'body/key PASS missing'
```

預期：含前後空白與換行的 body 解析後 `developer_instructions` 只去除兩端 whitespace、保留內部內容；key set 恰三鍵，沒有 frontmatter 額外 key。

**權威來源**：`agent_integrator.py:330-335`；`research/findings.md:33-34`。

### [x] CAT-12 · Oracle #6：無 frontmatter 時全文進 developer_instructions

**證據**：named case exit 0/PASS；普通 markdown 全文進 body；leading blank + `---...---` 負向 fixture 保留整段且使用 fallback。

```powershell
Push-Location $Repo
try {
    $Out = @(& go test ./internal/deploy/ -run '^TestCodexAgentTransform$/^no_frontmatter_uses_full_body$' -count=1 -v 2>&1)
    $Code = $LASTEXITCODE
} finally { Pop-Location }
$Text = $Out | Out-String
Assert ($Code -eq 0) "no-frontmatter case exit=$Code"
Assert ($Text -match '--- PASS: TestCodexAgentTransform/no_frontmatter_uses_full_body') 'no-frontmatter PASS missing'
```

預期：普通 markdown 全文 `.strip()` 後成為 `developer_instructions`；另以開頭先有空行再出現 `---` 的負向 fixture 證明 regex 不 match，該 `---...---` 仍屬 body，不得被誤切。

**權威來源**：`agent_integrator.py:296-299,318-320,333`；`research/findings.md:35`。

---

## 3. 真 binary A/B 與跨 target 回歸

### [x] CAT-13 · A/B fixture：兩邊真 CLI 都成功部署三種語意

**證據**：只在 TEMP scratch 執行；Go 與指定 `uv --project D:/Projects/apm-dev/apm` Python oracle 均 exit 0，各產生 `fm.toml`、`bad-yaml.toml`、`plain.toml`。

```powershell
$Seed = Join-Path $Scratch 'ab-seed'
$GoAB = Join-Path $Scratch 'ab-go'
$PyAB = Join-Path $Scratch 'ab-python'
New-Item -ItemType Directory -Force -Path (Join-Path $Seed '.apm/agents'),$GoAB,$PyAB | Out-Null
@'
name: codex-agent-ab
version: 1.0.0
target: [codex]
dependencies:
  apm: []
  mcp: []
'@ | Set-Content -Encoding utf8 (Join-Path $Seed 'apm.yml')
$FrontmatterFixture = @'
---
name: Frontmatter Name
description: Frontmatter Description
model: ignored
tools: [ignored]
---

__EDGE_BODY__
'@
$FrontmatterFixture.Replace('__EDGE_BODY__','  BODY EDGE  ') | Set-Content -Encoding utf8 (Join-Path $Seed '.apm/agents/fm.agent.md')
@'
---
name: "unterminated
---

bad body
'@ | Set-Content -Encoding utf8 (Join-Path $Seed '.apm/agents/bad-yaml.agent.md')
@'

# No Frontmatter

plain body
'@ | Set-Content -Encoding utf8 (Join-Path $Seed '.apm/agents/plain.agent.md')
Copy-Item -LiteralPath (Join-Path $Seed 'apm.yml') -Destination $GoAB
Copy-Item -LiteralPath (Join-Path $Seed '.apm') -Destination $GoAB -Recurse
Copy-Item -LiteralPath (Join-Path $Seed 'apm.yml') -Destination $PyAB
Copy-Item -LiteralPath (Join-Path $Seed '.apm') -Destination $PyAB -Recurse

Push-Location $GoAB
try { $GoOut = @(& $Bin install --target codex 2>&1); $GoCode = $LASTEXITCODE } finally { Pop-Location }
Push-Location $PyAB
try { $PyOut = @(& uv --project $OracleRepo run apm install --target codex 2>&1); $PyCode = $LASTEXITCODE } finally { Pop-Location }
$GoOut | Set-Content -Encoding utf8 (Join-Path $Scratch 'ab-go.txt')
$PyOut | Set-Content -Encoding utf8 (Join-Path $Scratch 'ab-python.txt')
Assert ($GoCode -eq 0) "apm-go A/B install exit=$GoCode"
Assert ($PyCode -eq 0) "Python oracle A/B install exit=$PyCode"
foreach ($Name in @('fm.toml','bad-yaml.toml','plain.toml')) {
    Assert (Test-Path (Join-Path $GoAB ".codex/agents/$Name") -PathType Leaf) "Go output missing $Name"
    Assert (Test-Path (Join-Path $PyAB ".codex/agents/$Name") -PathType Leaf) "Python output missing $Name"
}
```

預期：兩個 install 都 exit 0；兩邊各產生三個同名 `.toml`。禁止用 source byte-copy 或預先手造 TOML 取代真 CLI 部署。

**權威來源**：`prd.md:33-34`；`research/findings.md:41-44,54-55`；`agent_integrator.py:302-335`。

### [x] CAT-14 · A/B 判定只比 parse-TOML 語意，不比 bytes

**證據**：comparator exit 0，輸出 `CAT-14: PASS (3/3 semantic TOML matches)`；每檔兩側 exact key set、三值與獨立 expected 全相等。

```powershell
$env:GO_AB = $GoAB
$env:PY_AB = $PyAB
@'
import os
from pathlib import Path
import toml

keys = {"name", "description", "developer_instructions"}
expected = {
    "fm.toml": {
        "name": "Frontmatter Name",
        "description": "Frontmatter Description",
        "developer_instructions": "BODY EDGE",
    },
    "bad-yaml.toml": {
        "name": "bad-yaml",
        "description": "",
        "developer_instructions": "bad body",
    },
    "plain.toml": {
        "name": "plain",
        "description": "",
        "developer_instructions": "# No Frontmatter\n\nplain body",
    },
}
for filename, want in expected.items():
    go_doc = toml.load(Path(os.environ["GO_AB"]) / ".codex" / "agents" / filename)
    py_doc = toml.load(Path(os.environ["PY_AB"]) / ".codex" / "agents" / filename)
    assert set(go_doc) == keys, (filename, "Go keys", set(go_doc))
    assert set(py_doc) == keys, (filename, "Python keys", set(py_doc))
    assert go_doc == py_doc, (filename, go_doc, py_doc)
    assert go_doc == want, (filename, go_doc, want)
print("CAT-14: PASS (3/3 semantic TOML matches)")
'@ | & uv --project $OracleRepo run python -
$Code = $LASTEXITCODE
Assert ($Code -eq 0) "semantic comparator exit=$Code"
```

預期：exit 0 且印出 `CAT-14: PASS (3/3 semantic TOML matches)`；每檔兩邊 key set 與三值完全相等。TOML 原始 bytes、quote style、多行序列化格式不得列入 PASS/FAIL。

**權威來源**：`prd.md:33-34`；`research/findings.md:41-43`；`agent_integrator.py:330-335`。

### [x] CAT-15 · claude / opencode / copilot agents 維持 byte-copy

**證據**：四 target live install exit 0；source 與三個 copy 的 SHA-256 均為 `01B75D789155D5CC49D7930B9834EBB42933C678C77E553DCE51A5865942A90F`；Codex 為 `B4FE5BBAA417AAC8BA03368635C773294E0AC380619C8FC17421F64AD15442F1`；named regression PASS。

```powershell
$AllTargets = Join-Path $Scratch 'all-targets-copy-lock'
New-Item -ItemType Directory -Path $AllTargets | Out-Null
Copy-Item -LiteralPath (Join-Path $Seed 'apm.yml') -Destination $AllTargets
Copy-Item -LiteralPath (Join-Path $Seed '.apm') -Destination $AllTargets -Recurse
Push-Location $AllTargets
try { $InstallOut = @(& $Bin install --target claude,opencode,copilot,codex 2>&1); $InstallCode = $LASTEXITCODE } finally { Pop-Location }
Assert ($InstallCode -eq 0) "four-target install exit=$InstallCode"

$Source = Join-Path $AllTargets '.apm/agents/fm.agent.md'
$SourceHash = (Get-FileHash $Source -Algorithm SHA256).Hash
foreach ($Rel in @('.claude/agents/fm.md','.opencode/agents/fm.md','.github/agents/fm.agent.md')) {
    $Path = Join-Path $AllTargets $Rel
    Assert (Test-Path $Path -PathType Leaf) "$Rel missing"
    Assert ((Get-FileHash $Path -Algorithm SHA256).Hash -eq $SourceHash) "$Rel is no longer byte-copy"
}
$Codex = Join-Path $AllTargets '.codex/agents/fm.toml'
Assert ((Get-FileHash $Codex -Algorithm SHA256).Hash -ne $SourceHash) 'Codex output is still source byte-copy'

Push-Location $Repo
try { $UnitOut = @(& go test ./internal/deploy/ -run '^TestOtherTargetsAgentCopyUnchanged$' -count=1 -v 2>&1); $UnitCode = $LASTEXITCODE } finally { Pop-Location }
$UnitText = $UnitOut | Out-String
Assert ($UnitCode -eq 0) "copy regression unit exit=$UnitCode"
Assert ($UnitText -match '--- PASS: TestOtherTargetsAgentCopyUnchanged') 'copy regression PASS missing'
```

預期：install exit 0；三個非 Codex target 與 source SHA-256 完全相同，Codex 不同且是轉換結果；named regression test PASS。

**權威來源**：`prd.md:24,42`；`research/findings.md:21,51-53`。

### [x] CAT-16 · filename、lockfile hash、audit、uninstall 仍走 generic 機制

**證據**：lock path=`.codex/agents/fm.toml`，hash=`sha256:b4fe5bbaa417aac8ba03368635c773294e0ac380619c8fc17421f64ad15442f1` 與部署 bytes 相同；audit exit 0；三個 generic lifecycle named tests PASS。

```powershell
$Rel = '.codex/agents/fm.toml'
$Deployed = Join-Path $AllTargets $Rel
$Actual = 'sha256:' + (Get-FileHash $Deployed -Algorithm SHA256).Hash.ToLowerInvariant()
$Lock = Get-Content -Raw (Join-Path $AllTargets 'apm.lock.yaml')
Assert ($Lock -match ('(?m)^\s*-\s*' + [regex]::Escape($Rel) + '\s*$')) 'deployed_files path missing or filename changed'
Assert ($Lock -match ('(?m)^\s*' + [regex]::Escape($Rel) + ':\s*' + [regex]::Escape($Actual) + '\s*$')) 'lock hash is not transformed output hash'

Push-Location $AllTargets
try { $AuditOut = @(& $Bin audit 2>&1); $AuditCode = $LASTEXITCODE } finally { Pop-Location }
$AuditText = $AuditOut | Out-String
Assert ($AuditCode -eq 0) "audit exit=$AuditCode"
Assert ($AuditText -match 'audit:\s+\d+ deployed files verified') 'audit success signature missing'

Push-Location $Repo
try {
    $LifeOut = @(& go test ./internal/deploy/ -run '^(TestRun_DeployedFilesKeyMatch|TestRemoveDeployedFiles_(NormalRemoval|HashMismatchIsKeptWithWarning))$' -count=1 -v 2>&1)
    $LifeCode = $LASTEXITCODE
} finally { Pop-Location }
$LifeText = $LifeOut | Out-String
Assert ($LifeCode -eq 0) "generic lifecycle tests exit=$LifeCode"
foreach ($Name in @('TestRun_DeployedFilesKeyMatch','TestRemoveDeployedFiles_NormalRemoval','TestRemoveDeployedFiles_HashMismatchIsKeptWithWarning')) {
    Assert ($LifeText -match ('--- PASS: ' + [regex]::Escape($Name))) "$Name PASS missing"
}
Assert ($LifeText -notmatch '--- SKIP:|--- FAIL:') 'generic lifecycle tests contain SKIP/FAIL'
```

預期：檔名仍為 `<p.Name>.toml`；lock 記錄轉換後 bytes 的 SHA-256；audit exit 0；既有 generic removal/hash-guard tests 全 PASS，無 Codex 專用 lock/uninstall/audit 分支。

**權威來源**：`prd.md:25-26`；`research/findings.md:44,47-49`；`cmd/apm/audit.go:12-18,34-53`。

### [x] CAT-17 · evals/test1 場景只在 scratch 複本重現

**證據**：只以外部 `$Bin` 在 scratch 複本重跑；install/parse/audit 均 exit 0，TOML exact 三鍵與關鍵值通過；原 `evals/test1` 13 檔前後 tree hash manifest diff=0。

```powershell
function Get-TreeManifest([string]$Root) {
    @(Get-ChildItem -LiteralPath $Root -Recurse -File | Sort-Object FullName | ForEach-Object {
        $RelPath = $_.FullName.Substring($Root.Length).TrimStart('\','/')
        "$RelPath`t$((Get-FileHash $_.FullName -Algorithm SHA256).Hash)"
    })
}
$Test1Before = Get-TreeManifest $Test1
$Test1Copy = Join-Path $Scratch 'evals-test1-copy'
New-Item -ItemType Directory -Path $Test1Copy | Out-Null
Get-ChildItem -LiteralPath $Test1 -Force | Copy-Item -Destination $Test1Copy -Recurse -Force
Assert (Test-Path (Join-Path $Test1Copy 'apm.yml') -PathType Leaf) 'test1 scratch copy malformed'

Push-Location $Test1Copy
try { $ReplayOut = @(& $Bin install --target copilot,claude,opencode,codex,antigravity 2>&1); $ReplayCode = $LASTEXITCODE } finally { Pop-Location }
Assert ($ReplayCode -eq 0) "test1 scratch replay exit=$ReplayCode"
$Test1TOML = Join-Path $Test1Copy '.codex/agents/accessibility-runtime-tester.toml'
Assert (Test-Path $Test1TOML -PathType Leaf) 'test1 Codex TOML missing'
$env:TEST1_TOML = $Test1TOML
@'
import os, toml
doc = toml.load(os.environ["TEST1_TOML"])
assert set(doc) == {"name", "description", "developer_instructions"}
assert doc["name"] == "Accessibility Runtime Tester"
assert doc["description"].startswith("Runtime accessibility specialist")
assert doc["developer_instructions"].startswith("# Accessibility Runtime Tester")
print("test1 TOML: PASS")
'@ | & uv --project $OracleRepo run python -
$ParseCode = $LASTEXITCODE
Assert ($ParseCode -eq 0) "test1 TOML parse exit=$ParseCode"
Push-Location $Test1Copy
try { $AuditOut = @(& $Bin audit 2>&1); $AuditCode = $LASTEXITCODE } finally { Pop-Location }
Assert ($AuditCode -eq 0) "test1 audit exit=$AuditCode"

$Test1After = Get-TreeManifest $Test1
$OriginalDiff = @(Compare-Object $Test1Before $Test1After)
Assert ($OriginalDiff.Count -eq 0) 'original evals/test1 changed'
```

預期：scratch 複本 install exit 0；Codex 檔可 parse 且三鍵/關鍵值正確；audit exit 0；`D:/Projects/apm-dev/evals/test1` 全 tree hash manifest 前後完全相同。不得執行複本內舊 `apm-go.exe`。

**權威來源**：`prd.md:11-12,36`；`research/findings.md:3-13,54-55`。

---

## 4. Spec 與品質關卡

### [x] CAT-18 · Spec 落地且記錄可解析 implementation commit

**證據（2026-07-12，主 session 於 fix commit 後補驗）**：spec §9 佔位已換為
`commit: \`197fe98\`, 2026-07-12`；下方驗證腳本實跑輸出 `CAT-18: PASS
(commit=197fe98)`——7 個契約 token 齊、commit 可解析且 diff-tree 含
`internal/deploy/codex.go`。
（round 1 記錄：§9 已存在，契約 token 7/7 通過；`commit:` 為 pending 佔位，
依規則不以假 SHA 通過。）

```powershell
$SpecPath = Join-Path $Repo '.trellis/spec/backend/install-marketplace-contracts.md'
$Spec = Get-Content -Raw $SpecPath
$Section = [regex]::Match($Spec, '(?is)codex agents.{0,3000}developer_instructions.{0,3000}')
Assert ($Section.Success) 'Codex agents contract section missing'
foreach ($Token in @('name','description','developer_instructions','frontmatter','malformed','strip','\.agent')) {
    Assert ($Section.Value -match $Token) "spec contract token missing: $Token"
}
$CommitMatch = [regex]::Match($Section.Value, '(?i)commit:\s*`?([0-9a-f]{7,40})`?')
Assert ($CommitMatch.Success) 'spec implementation commit missing'
$Commit = $CommitMatch.Groups[1].Value
& git -C $Repo cat-file -e ($Commit + '^{commit}')
Assert ($LASTEXITCODE -eq 0) 'recorded implementation commit does not resolve'
$CommitFiles = @(& git -C $Repo diff-tree --no-commit-id --name-only -r $Commit)
Assert ($CommitFiles -contains 'internal/deploy/codex.go') 'recorded commit does not modify Codex adapter'
```

預期：spec 明文化六點語意、恰三鍵與 `.agent` fallback；其中記錄的 implementation `commit:` hash 可解析且實際修改 Codex adapter。若契約落在另一既有 Codex spec，僅可等價調整 `$SpecPath`，其餘斷言不放寬。

**權威來源**：`prd.md:37-38`；`research/findings.md:24-35`；`.trellis/spec/backend/install-marketplace-contracts.md`。

### [x] CAT-19 · 本任務觸碰的 Go 檔 gofmt gate

**證據**：修正 harness 後機械取得 tracked+untracked 共 3 檔。Windows `core.autocrlf=true` 下 raw `gofmt -l` 只列 `codex_agent.go`；`gofmt -d` 證實 99/99 行僅 CRLF→LF。TEMP LF-normalized mirror 的 `gofmt -l` exit 0 且輸出空，無實質格式差異。

```powershell
$TrackedGo = @(& git -C $Repo diff --name-only $Base -- '*.go')
Assert ($LASTEXITCODE -eq 0) 'cannot enumerate tracked touched Go files'
$UntrackedGo = @(& git -C $Repo ls-files --others --exclude-standard -- '*.go')
Assert ($LASTEXITCODE -eq 0) 'cannot enumerate untracked touched Go files'
$TouchedGo = @($TrackedGo + $UntrackedGo | Sort-Object -Unique)
Assert ($TouchedGo.Count -gt 0) 'no touched Go file found'
Push-Location $Repo
try { $RawUnformatted = @(& gofmt -l @TouchedGo); $RawFmtCode = $LASTEXITCODE } finally { Pop-Location }
Assert ($RawFmtCode -eq 0) "raw gofmt command exit=$RawFmtCode"

# Windows core.autocrlf may make gofmt -l report an otherwise formatted CRLF
# working file. Normalize only CRLF in TEMP, then run the formatter gate over
# the complete tracked+untracked set without changing any source file.
$LFRoot = Join-Path $Scratch 'gofmt-lf-normalized'
$LFFiles = @()
foreach ($Rel in $TouchedGo) {
    $Dest = Join-Path $LFRoot $Rel
    New-Item -ItemType Directory -Force -Path (Split-Path $Dest -Parent) | Out-Null
    $Text = [Text.Encoding]::UTF8.GetString([IO.File]::ReadAllBytes((Join-Path $Repo $Rel))).Replace("`r`n", "`n")
    [IO.File]::WriteAllText($Dest, $Text, [Text.UTF8Encoding]::new($false))
    $LFFiles += $Dest
}
$Unformatted = @(& gofmt -l @LFFiles)
$FmtCode = $LASTEXITCODE
Assert ($FmtCode -eq 0) "LF-normalized gofmt command exit=$FmtCode"
Assert ($Unformatted.Count -eq 0) ('substantively unformatted files: ' + ($Unformatted -join ', '))
if ($RawUnformatted.Count -gt 0) {
    "environment limitation: raw CRLF working files reported by gofmt: $($RawUnformatted -join ', ')" |
        Set-Content -Encoding utf8 (Join-Path $Scratch 'cat-19-crlf-environment.txt')
}
```

預期：檢查集合由 `$Base..working tree` 的 tracked diff 加 untracked Go files 機械取得，不得手選漏檔；LF-normalized gofmt exit 0 且輸出空白。raw gate 若只因 Windows CRLF 有輸出，必須留下環境限制記錄，且不得修改 Go 檔來掩蓋結果。

**權威來源**：`prd.md:35`；project `AGENTS.md` Available commands；`.trellis/spec/conformance/cli-verification-checklist.md:19`。

### [x] CAT-20 · 全 repo build / vet / test 全綠

**證據**：`go build ./...` / `go vet ./...` / `go test ./... -count=1` 依序 exit `0/0/0`；full test output 無 FAIL。

```powershell
Push-Location $Repo
try {
    & go build ./...
    $BuildCode = $LASTEXITCODE
    & go vet ./...
    $VetCode = $LASTEXITCODE
    $TestOut = @(& go test ./... -count=1 2>&1)
    $TestCode = $LASTEXITCODE
} finally { Pop-Location }
$TestText = $TestOut | Out-String
Assert ($BuildCode -eq 0) "go build ./... exit=$BuildCode"
Assert ($VetCode -eq 0) "go vet ./... exit=$VetCode"
Assert ($TestCode -eq 0) "go test ./... exit=$TestCode"
Assert ($TestText -notmatch '(?m)^FAIL\s|--- FAIL:') 'full test output contains FAIL'
```

預期：三命令依序 exit `0,0,0`；全 repo tests 無 FAIL。

**權威來源**：`prd.md:35`；project `AGENTS.md` Available commands。

### [x] CAT-21 · deploy package coverage 不低於 80%

**證據**：`go test ./internal/deploy/ -cover -count=1` exit 0；statement coverage=`88.3%`。

```powershell
Push-Location $Repo
try {
    $CoverOut = @(& go test ./internal/deploy/ -cover -count=1 2>&1)
    $CoverCode = $LASTEXITCODE
} finally { Pop-Location }
$CoverText = $CoverOut | Out-String
$Match = [regex]::Match($CoverText, 'coverage:\s+([0-9.]+)%')
Assert ($CoverCode -eq 0) "deploy coverage test exit=$CoverCode"
Assert ($Match.Success) 'coverage percentage missing'
Assert ([double]$Match.Groups[1].Value -ge 80.0) "deploy coverage below 80%: $($Match.Groups[1].Value)%"
```

預期：exit 0，且 `internal/deploy` statement coverage ≥ 80.0%。

**權威來源**：`prd.md:30-32,35`；project `AGENTS.md` test coverage target。

---

驗證摘要（2026-07-12，第 1 輪）：`VERDICT: CONFIRMED 20 / FAIL 0 / DEFERRED 1`（另修 CAT-03/CAT-19 兩處 harness 缺陷）
驗證摘要（2026-07-12，CAT-18 commit 後補驗）：`VERDICT: CONFIRMED 21 / FAIL 0 / DEFERRED 0` —— 全數通過。
