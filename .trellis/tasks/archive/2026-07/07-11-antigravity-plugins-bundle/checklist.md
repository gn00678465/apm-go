# Antigravity plugin bundle 硬性驗證清單

> **用途**：驗證 dependency 套件以 `.agents/plugins/<pkg>/` 部署後，bundle 格式、hooks 隔離、provenance、uninstall 安全與既有 antigravity 契約均可被機械判定；任一「預期」不成立即 FAIL，不得以人工目視或「大致相同」取代。
>
> **圖例**：`[ ]` 待驗 · `[x]` 已驗——**附完整指令、exit code、關鍵輸出與檔案樹/hash 證據才可勾**。本檔初始全部為 `[ ]`。
>
> **權威優先序**：本 task PRD/已拍板 design → `.trellis/spec/` 契約 → agy 實機/官方 plugin 文件 → Python 原版（本功能無 bundle，僅作既有行為對照）→ research 發現。Python 與 bundle 衝突時，以 documented extension 明文決策為準。

---

## 0. 執行前置與安全（先做）

#### [x] AGB-001 · 固定名稱重建 apm-go binary

- 證據（2026-07-11）：原指令 exit `0`；`bin/apm-go.exe` 存在（`13,661,696` bytes），未產生其他名稱。

```powershell
Set-Location D:/Projects/apm-dev/apm-go
go build -o bin/apm-go.exe ./cmd/apm
$rc = $LASTEXITCODE
if ($rc -ne 0 -or -not (Test-Path ./bin/apm-go.exe -PathType Leaf)) { throw "build failed: rc=$rc" }
```

- 預期：exit `0`；只產生/更新 `bin/apm-go.exe`，不得改名成 `apm.exe` 或 `apm`。
- 權威來源：`AGENTS.md` Available commands；`implement.md:55,89`；`.trellis/spec/conformance/cli-verification-checklist.md:17-20`。

#### [x] AGB-002 · 工具版本與 agy drift 閘門

- 證據（2026-07-11）：三指令均 exit `0`；`go1.26.3 windows/amd64`、`uv 0.7.3`、`agy 1.1.1`。

```powershell
go version; if ($LASTEXITCODE -ne 0) { throw "go unavailable" }
uv --version; if ($LASTEXITCODE -ne 0) { throw "uv unavailable" }
agy --version; $agyRc = $LASTEXITCODE
if ($agyRc -ne 0) { throw "agy is required for AC1" }
```

- 預期：三者 exit `0`；`agy --version` 輸出 `1.1.1`。若不是 `1.1.1`，本項 FAIL 並先重跑 AGB-021～023 的正負 probes，不得沿用舊版結論。
- 權威來源：`research/antigravity-bundle-notes.md:217-236,251-255`（agy 1.1.1 live delta）；`design.md:43-50`；`implement.md:8`。

#### [x] AGB-003 · scratch 隔離與 binary 固定

- 證據（2026-07-11）：exit `0`；scratch = `C:/Users/gn006/AppData/Local/Temp/apm-go-ag-bundle-adversarial`，binary = repo 下的 `bin/apm-go.exe`，scratch 不在 repo 內。

```powershell
$Repo = (Resolve-Path D:/Projects/apm-dev/apm-go).Path
$Bin = (Resolve-Path "$Repo/bin/apm-go.exe").Path
$Scratch = Join-Path ([IO.Path]::GetTempPath()) ("apm-go-ag-bundle-" + [guid]::NewGuid())
$Proj = Join-Path $Scratch "project"
New-Item -ItemType Directory -Force $Proj | Out-Null
if ([IO.Path]::GetFullPath($Scratch).StartsWith([IO.Path]::GetFullPath($Repo), [StringComparison]::OrdinalIgnoreCase)) { throw "scratch is inside repo" }
```

- 預期：exit `0`；`$Scratch` 位於系統 TEMP 且不在 repo 內；後續所有 `install`/`uninstall` 的 `cwd` 必須是 `$Proj` 或其複本，禁止在 repo 根執行。
- 權威來源：`.trellis/spec/conformance/cli-verification-checklist.md:24`；`research/antigravity-bundle-notes.md:217-221`；`implement.md:63-67,101`。

#### [x] AGB-004 · Python oracle 使用邊界與真實 registry 零變更

- 證據（2026-07-11）：全程未執行 Python oracle marketplace add/remove/update；`~/.apm/marketplaces.json` 前後 SHA-256 均為 `9F752A5D…C2730B`。

```powershell
# 唯一允許的 Python oracle 呼叫形狀：
# uv --project D:/Projects/apm-dev/apm run apm <args>

$OracleRegistry = Join-Path $HOME ".apm/marketplaces.json"
$OracleBefore = if (Test-Path $OracleRegistry) { (Get-FileHash $OracleRegistry -Algorithm SHA256).Hash } else { "<absent>" }
# 執行本清單；嚴禁把 <args> 換成 marketplace add/remove/update。
$OracleAfter = if (Test-Path $OracleRegistry) { (Get-FileHash $OracleRegistry -Algorithm SHA256).Hash } else { "<absent>" }
if ($OracleBefore -ne $OracleAfter) { throw "Python oracle mutated real marketplace registry" }
```

- 預期：本清單結束前後 hash 完全相同（或都不存在）。**禁止執行 Python oracle 的 `marketplace add`、`marketplace remove`、`marketplace update`**；Python 無 plugin bundle，不得把其平鋪結果誤判為 AC1 的 oracle。
- 權威來源：`.trellis/spec/conformance/cli-verification-checklist.md:20-24`；Python `targets.py:666-687`、`hook_integrator.py:357-362`；`research/antigravity-bundle-notes.md:196-215`。

---

## 1. 可執行契約與單元鎖定

#### [x] AGB-005 · explicit-only 與 alias 契約不變

- 證據（2026-07-11）：增強版指令 exit `0`；四主測試及 GEMINI/AGENTS/alias subtests 全數明確 `PASS`，無 SKIP/no-tests。

```powershell
$out = go test ./internal/deploy -run 'TestResolveTargets_(AntigravityNotAutoDetected|AntigravityExplicitSelection|FlagAllExcludesAntigravity|ManifestAllExcludesAntigravity)$' -count=1 -v 2>&1
$rc = $LASTEXITCODE; $text = $out -join "`n"
if ($rc -ne 0 -or $text -match 'no tests to run|(?m)^--- SKIP:') { throw "explicit-only suite failed: rc=$rc" }
```

- 預期：exit `0`、所有列名測試 `PASS`；`agy`/`antigravity` 顯式選取有效，`all`、`GEMINI.md`、`AGENTS.md` 不得隱式啟用。
- 權威來源：`.trellis/spec/backend/antigravity-target-contract.md:33-46`；`internal/deploy/adapter.go:118-145`；PRD `prd.md:27-28`。

#### [x] AGB-006 · dep 四型進 bundle、local 四型維持平鋪

- 證據（2026-07-11）：`TestRun_AntigravityBundlePaths` / `LocalPathsUnchanged` 皆 PASS；測試本體逐檔讀取 dep 四型（含 skill 附件）並斷言 local 四個平鋪路徑。

```powershell
go test ./internal/deploy -run 'TestRun_Antigravity(BundlePaths|LocalPathsUnchanged)$' -count=1 -v
```

- 預期：exit `0`；測試必須逐表斷言 dependency `instructions/agents/skills/hooks` 分別落 `plugins/<pkg>/rules/<n>.md`、`agents/<n>/agent.md`、`skills/<n>/...`、`hooks.json`；local 仍落 `.agents/rules`、`.agents/agents`、`.agents/skills`、`.agents/hooks.json`。若測試不存在（`warning: no tests to run`）即 FAIL。
- 權威來源：PRD AC1 `prd.md:38-39`；`design.md:64-71,75-94,190-193`（D1/D2）；`implement.md:13-25`。

#### [x] AGB-007 · plugin.json 最小 manifest、hash provenance 與 re-install 冪等

- 證據（2026-07-11）：兩測試 PASS；本體精確比對 `{"name": "tool"}\n`、PerDep path/hash，第二次 Run 的 manifest hash 與首次相同。

```powershell
go test ./internal/deploy -run 'TestRun_AntigravityPluginManifest(Provenance|Reinstall)$' -count=1 -v
```

- 預期：exit `0` 且不得 `no tests to run`；manifest JSON 僅有非空字串 `name`、UTF-8 無 BOM、末尾單一 LF；首次及連續第二次 `Run` 後，`plugin.json` 路徑與 SHA-256 都存在於同一 dep 的 `PerDep.Files/Hashes`，兩次 bytes/hash 相同。
- 權威來源：`design.md:69-71,121-147,194-195`（D4/D5/R3）；`cmd/apm/install.go:881-888`（整批覆蓋）；`research/antigravity-bundle-notes.md:256-262`；agy 1.1.1 G3/G4 `research/...:227-230`。

#### [x] AGB-008 · bundle 名是安全單一路徑段，禁止 containment 退化

- 證據（2026-07-11）：兩套 Go tests 皆 PASS（empty/dot/dotdot/slash/backslash/drive/unsafe + ContainedKey）；額外 TEMP `.hidden` dep 實裝產生非隱藏單段 `pkghidden-40438f47`。

```powershell
$out = go test ./internal/deploy -run 'TestBundleNameFromDepKey|TestAntigravityBundleDir' -count=1 -v 2>&1
if ($LASTEXITCODE -ne 0 -or ($out -join "`n") -match 'no tests to run|(?m)^--- SKIP:') { throw "bundle-name suite failed" }
$out = go test ./internal/archive -run '^TestContainedKey$' -count=1 -v 2>&1
if ($LASTEXITCODE -ne 0 -or ($out -join "`n") -match 'no tests to run|(?m)^--- SKIP:') { throw "ContainedKey suite failed" }
```

- 預期：兩命令 exit `0` 且不得 `no tests to run`；空字串、`.`、`..`、前導點、`/`、`\`、磁碟代號或其他非 `[A-Za-z0-9._-]` 輸入不得產生空段、隱藏段或跨出 `.agents/plugins/`；`ContainedKey` 的 `../`、中段 `..`、反斜線 `..` cases 全為拒絕。
- 權威來源：`internal/archive/extract.go:213-234`；`internal/archive/extract_test.go:213-232`；`cmd/apm/install.go:1320-1351`（既有安全命名先例）；research `antigravity-bundle-notes.md:188-194,263-273`。

#### [x] AGB-009 · 不同 DepKey 同 basename 不得混居

- 證據（2026-07-11）：測試 PASS；`acme/tool` + `other-org/tool` 回傳同時命名兩 dep 的 error，result=nil，`.agents/plugins` 完全不存在。

```powershell
go test ./internal/deploy -run '^TestRun_AntigravityBundleNameCollision$' -count=1 -v
```

- 預期：exit `0` 且不得 `no tests to run`；`acme/tool` 與 `other-org/tool` 必須產生不同 bundle 目錄，**或**在寫入任何 bundle 檔前 fail-closed 回傳非 nil error。只發 warning 後把兩套件檔案混進同一目錄為 FAIL；兩 dep 的 `plugin.json`/hooks/rules 不得交叉歸屬。
- 權威來源：research `antigravity-bundle-notes.md:263-273,328-346`（資料污染風險與建議）；`cmd/apm/install.go:1320-1329`（hash 防碰撞先例）；安全不變式「只管理自身 provenance」。

#### [x] AGB-010 · 跨套件 hooks 隔離（PRD AC2）

- 證據（2026-07-11）：測試 PASS；dep-a/dep-b `hooks.json` 分別 byte-equal `aHook`/`bHook`，diagnostics 無 `overwrites`。

```powershell
go test ./internal/deploy -run '^TestRun_AntigravityTwoDependencyHooksIsolated$' -count=1 -v
```

- 預期：exit `0` 且不得 `no tests to run`；兩 dep 各有一份不同路徑的 `hooks.json`，各自與來源 byte-equal；diagnostics 不含 `overwrites`，任一 dep 的 hook bytes 不得出現在另一 dep bundle。
- 權威來源：PRD AC2 `prd.md:40`；`design.md:190-193`；`implement.md:16-17`；官方/agy 內嵌文件摘錄 `cli-plugins.md:158-162`。

#### [x] AGB-011 · 同套件多 hooks 的既知殘留行為不得被誤宣稱已解決

- 證據（2026-07-11）：Go test PASS，且 TEMP 真 binary 雙 hook fixture 兩次 install 均報 `overwrites`，確定 winner=`b.json`，兩次 SHA-256 均 `D80D81AF…271ED8D0`。

```powershell
go test ./internal/deploy -run '^TestRun_AntigravitySameDependencyHooksOverwriteDiagnostic$' -count=1 -v
```

- 預期：exit `0` 且不得 `no tests to run`；同一 dep 兩個 hook primitive 收斂為該 bundle 唯一 `hooks.json`，輸出含 `overwrites` 與目標路徑；結果必須依既有 winner/迭代順序確定，不可靜默或產生第二個未記 provenance 的 hooks 檔。
- 權威來源：`design.md:71,193-197`（D6）；research `antigravity-bundle-notes.md:245-250`；現行診斷 `internal/deploy/deploy.go:153-155,194-201`。

#### [x] AGB-012 · primitive 衝突仍在 adapter 前解決

- 證據（2026-07-11）：增強版指令 exit `0`；local-wins / first-wins / claude+antigravity subtests 全 PASS，本體並斷言 loser 無 winner 路徑 provenance。

```powershell
$out = go test ./internal/deploy -run 'TestResolvePrimitives_(LocalOverridesDep|FirstDeclaredWins)$|TestRun_AgentSameNameCollision_FirstDeclaredWins$' -count=1 -v 2>&1
if ($LASTEXITCODE -ne 0 -or ($out -join "`n") -match 'no tests to run|(?m)^--- SKIP:') { throw "conflict suite failed" }
```

- 預期：exit `0`；local 同名勝 dep、同 source class 第一個宣告者勝；loser 不得寫入 bundle，也不得在 loser 的 provenance 記 winner 路徑。
- 權威來源：`.trellis/spec/backend/antigravity-target-contract.md:52-60`；`internal/deploy/conflict.go:13-52`；`design.md:27-30,200-202`。

#### [x] AGB-013 · MCP 維持共用 merge writer，不進 bundle

- 證據（2026-07-11）：修正不存在的 test pattern 後 writer/merge tests PASS；TEMP 真 binary 再驗 foreign top-level/user server 保留、managed server 新增、bundle MCP=0。

```powershell
$out = go test ./internal/deploy -run 'TestWriteMCP_Antigravity|TestWriteMCP_MergePreservesForeignKeysAndOtherServers' -count=1 -v 2>&1
$rc = $LASTEXITCODE; $text = $out -join "`n"
if ($rc -ne 0 -or $text -match 'no tests to run|(?m)^--- SKIP:' -or $text -notmatch 'PASS: TestWriteMCP_MergePreservesForeignKeysAndOtherServers') { throw "MCP writer/merge suite failed: rc=$rc" }
```

- 預期：exit `0`；遠端 transport 僅用 `serverUrl`，stdio 用 `command/args/env`；輸出仍是 `.agents/mcp_config.json`/`mcpServers`，保留未管理的使用者/外部 keys；任何 bundle 下 `mcp_config.json` 均為 FAIL。
- 權威來源：`.trellis/spec/backend/antigravity-target-contract.md:9-29`；`internal/deploy/mcp_antigravity.go:9-43`；`internal/deploy/mcp_common.go:222-295`；`design.md:68,149-157`（D3）。

#### [x] AGB-014 · support matrix 與 Non-Goals 不擴張

- 證據（2026-07-11）：`TestNotDeployed_PerTarget/antigravity` 明確 PASS；commands/prompts 未部署，TEMP tree 亦無 declared/version-tracking/commands 產物。

```powershell
go test ./internal/deploy -run '^TestNotDeployed_PerTarget$/antigravity' -count=1 -v
```

- 預期：exit `0` 且 antigravity subtest `PASS`；commands/prompts 不部署。fixture 終態不得出現 `~/.gemini/config/plugins` 寫入、`.agents/plugins.json`、`installed_version.json` 或 bundle 內 `commands/`。
- 權威來源：PRD Non-Goals `prd.md:46-50`；`internal/deploy/antigravity.go:9-13`；`design.md:11-13,64-68`。

---

## 2. 真 binary：bundle 佈局、內容與 agy conformance

#### [x] AGB-015 · 建立可重複的雙 dependency scratch fixture

- 證據（2026-07-11）：exit `0`；TEMP fixture 建立 `16` 個 source files，local + dep-a + dep-b 所需四型均存在，repo 內無 fixture。

```powershell
$dirs = @(
  "$Proj/.apm/instructions", "$Proj/.apm/agents", "$Proj/.apm/skills/local-skill", "$Proj/.apm/hooks",
  "$Proj/dep-a/.apm/instructions", "$Proj/dep-a/.apm/agents", "$Proj/dep-a/.apm/skills/a-skill/scripts", "$Proj/dep-a/.apm/hooks",
  "$Proj/dep-b/.apm/instructions", "$Proj/dep-b/.apm/agents", "$Proj/dep-b/.apm/skills/b-skill", "$Proj/dep-b/.apm/hooks"
)
$dirs | ForEach-Object { New-Item -ItemType Directory -Force $_ | Out-Null }
$Utf8NoBom = [Text.UTF8Encoding]::new($false)
function Write-Fixture([string]$Path, [string]$Content) { [IO.File]::WriteAllText($Path, $Content, $Utf8NoBom) }
Write-Fixture "$Proj/apm.yml" @'
name: ag-bundle-fixture
version: "1.0.0"
dependencies:
  apm:
    - ./dep-a
    - ./dep-b
'@
Write-Fixture "$Proj/dep-a/apm.yml" "name: dep-a`nversion: `"1.0.0`"`n"
Write-Fixture "$Proj/dep-b/apm.yml" "name: dep-b`nversion: `"1.0.0`"`n"
Write-Fixture "$Proj/.apm/instructions/local.instructions.md" "local-rule`n"
Write-Fixture "$Proj/.apm/agents/local.agent.md" "local-agent`n"
Write-Fixture "$Proj/.apm/skills/local-skill/SKILL.md" "local-skill`n"
Write-Fixture "$Proj/.apm/hooks/local.json" '{"local-hook":{"Stop":[]}}'
Write-Fixture "$Proj/dep-a/.apm/instructions/a.instructions.md" "a-rule`n"
Write-Fixture "$Proj/dep-a/.apm/agents/a.agent.md" "a-agent`n"
Write-Fixture "$Proj/dep-a/.apm/skills/a-skill/SKILL.md" "a-skill`n"
Write-Fixture "$Proj/dep-a/.apm/skills/a-skill/scripts/helper.txt" "a-helper`n"
Write-Fixture "$Proj/dep-a/.apm/hooks/a.json" '{"a-hook":{"Stop":[]}}'
Write-Fixture "$Proj/dep-b/.apm/instructions/b.instructions.md" "b-rule`n"
Write-Fixture "$Proj/dep-b/.apm/agents/b.agent.md" "b-agent`n"
Write-Fixture "$Proj/dep-b/.apm/skills/b-skill/SKILL.md" "b-skill`n"
Write-Fixture "$Proj/dep-b/.apm/hooks/b.json" '{"b-hook":{"Stop":[]}}'
```

- 預期：exit `0`；fixture 只存在 `$Scratch`，兩 dep 各具 rules/agents/skills/hooks，local 同具四型；未建立任何 repo 內檔案。
- 權威來源：`design.md:75-94` 目錄契約；PRD AC1/AC2 `prd.md:38-40`。

#### [x] AGB-016 · explicit install 成功且無跨 dep overwrite 診斷

- 證據（2026-07-11）：exit `0`，Targets=`antigravity`，無 `overwrites`；恰有 bundles `dep-a-6272f583`、`dep-b-0db7dbed`。

```powershell
Push-Location $Proj
try { $installOut = & $Bin install --target agy 2>&1; $installRc = $LASTEXITCODE } finally { Pop-Location }
$installOut | Set-Content "$Scratch/install.out"
if ($installRc -ne 0) { throw "install rc=$installRc" }
if (($installOut -join "`n") -notmatch 'Targets:.*antigravity') { throw "missing canonical target output" }
if (($installOut -join "`n") -match 'overwrites') { throw "cross-dependency overwrite diagnostic" }
```

- 預期：exit `0`；輸出 canonical target `antigravity`；不含 `overwrites`；恰有兩個 bundle 目錄。
- 權威來源：PRD `prd.md:27-31,40`；`design.md:171-182`；`implement.md:65-67`。

#### [x] AGB-017 · 實際檔案樹與 byte-copy

- 證據（2026-07-11）：加強為 dep-a/dep-b 兩邊四型共 `9` 組 source/destination SHA-256 對，全部相同；skill 附件與兩份獨立 hooks 均在 tree 中。

```powershell
$Plugins = @(Get-ChildItem "$Proj/.agents/plugins" -Directory)
if ($Plugins.Count -ne 2) { throw "expected 2 bundles, got $($Plugins.Count)" }
$A = @($Plugins | Where-Object { (Get-Content "$($_.FullName)/hooks.json" -Raw) -match 'a-hook' })
$B = @($Plugins | Where-Object { (Get-Content "$($_.FullName)/hooks.json" -Raw) -match 'b-hook' })
if ($A.Count -ne 1 -or $B.Count -ne 1 -or $A[0].FullName -eq $B[0].FullName) { throw "hooks are not isolated" }
$pairs = @(
  @("$Proj/dep-a/.apm/instructions/a.instructions.md", "$($A[0].FullName)/rules/a.md"),
  @("$Proj/dep-a/.apm/agents/a.agent.md", "$($A[0].FullName)/agents/a/agent.md"),
  @("$Proj/dep-a/.apm/skills/a-skill/SKILL.md", "$($A[0].FullName)/skills/a-skill/SKILL.md"),
  @("$Proj/dep-a/.apm/skills/a-skill/scripts/helper.txt", "$($A[0].FullName)/skills/a-skill/scripts/helper.txt"),
  @("$Proj/dep-a/.apm/hooks/a.json", "$($A[0].FullName)/hooks.json"),
  @("$Proj/dep-b/.apm/instructions/b.instructions.md", "$($B[0].FullName)/rules/b.md"),
  @("$Proj/dep-b/.apm/agents/b.agent.md", "$($B[0].FullName)/agents/b/agent.md"),
  @("$Proj/dep-b/.apm/skills/b-skill/SKILL.md", "$($B[0].FullName)/skills/b-skill/SKILL.md"),
  @("$Proj/dep-b/.apm/hooks/b.json", "$($B[0].FullName)/hooks.json")
)
foreach ($p in $pairs) {
  if (-not (Test-Path $p[1] -PathType Leaf)) { throw "missing $($p[1])" }
  if ((Get-FileHash $p[0]).Hash -ne (Get-FileHash $p[1]).Hash) { throw "byte mismatch $($p[1])" }
}
```

- 預期：exit `0`；四型路徑完全符合目錄契約；skills 附件遞迴複製；所有列出的來源/目的 SHA-256 相同；兩 hooks 路徑不同。
- 權威來源：PRD AC1/AC2 `prd.md:38-40`；`design.md:85-94,190-193`；agy bundle 佈局 `cli-plugins.md:141-160`。

#### [x] AGB-018 · local primitives 與 MCP 路徑不被 bundle 遷移

- 證據（2026-07-11）：exit `0`；local rules/agents/skills/hooks `4/4` 存在，bundle 內 `mcp_config.json` 計數 `0`。

```powershell
$localPaths = @(
  "$Proj/.agents/rules/local.md",
  "$Proj/.agents/agents/local/agent.md",
  "$Proj/.agents/skills/local-skill/SKILL.md",
  "$Proj/.agents/hooks.json"
)
foreach ($p in $localPaths) { if (-not (Test-Path $p -PathType Leaf)) { throw "missing local output $p" } }
if (Get-ChildItem "$Proj/.agents/plugins" -Recurse -Filter mcp_config.json) { throw "MCP migrated into bundle" }
```

- 預期：exit `0`；local 四型仍平鋪；bundle 內零 `mcp_config.json`。fixture 未宣告 MCP 時，共用 `.agents/mcp_config.json` 可不存在。
- 權威來源：`design.md:64-71,75-94,149-157`（D1/D3）；research `antigravity-bundle-notes.md:27-43`。

#### [x] AGB-019 · plugin.json schema、名稱與輸出樣式

- 證據（2026-07-11）：兩 manifest 均僅 `name` key、name==dir、UTF-8 no BOM、LF-terminated；exit `0`。

```powershell
foreach ($plug in $Plugins) {
  $manifestPath = Join-Path $plug.FullName "plugin.json"
  $bytes = [IO.File]::ReadAllBytes($manifestPath)
  $json = Get-Content $manifestPath -Raw | ConvertFrom-Json
  $keys = @($json.PSObject.Properties.Name)
  if ($keys.Count -ne 1 -or $keys[0] -ne 'name' -or [string]::IsNullOrWhiteSpace($json.name)) { throw "invalid minimal manifest: $manifestPath" }
  if ($json.name -ne $plug.Name) { throw "name/dir mismatch: $manifestPath" }
  if ($bytes.Length -lt 2 -or $bytes[-1] -ne 10 -or ($bytes.Length -ge 3 -and $bytes[0] -eq 0xEF -and $bytes[1] -eq 0xBB -and $bytes[2] -eq 0xBF)) { throw "manifest must be UTF-8 no BOM with LF" }
}
```

- 預期：exit `0`；每 bundle 恰一個 `plugin.json`；唯一 key 為非空 `name`，值等於目錄名；無 BOM、以 LF 結尾。
- 權威來源：`design.md:69-71,87,144-147`（D5）；research G3/G4/G6 `antigravity-bundle-notes.md:227-230`；官方 manifest required 見 `cli-plugins.md:146-158`，1.1.1 delta 以 live 結果為準。

#### [x] AGB-020 · lockfile ownership、hash 完整性與第二次 install 不掉 manifest

- 證據（2026-07-11）：re-install exit `0`；2 個 manifest hash 穩定，lock 有 `15` 個 sha256 envelope；逐 dep section 目視/機械核對所有 bundle file 路徑與 hash 皆歸正確 dep。

```powershell
$before = @{}
foreach ($plug in $Plugins) { $before[$plug.Name] = (Get-FileHash "$($plug.FullName)/plugin.json" -Algorithm SHA256).Hash }
Push-Location $Proj
try { & $Bin install --target agy *> "$Scratch/reinstall.out"; $reinstallRc = $LASTEXITCODE } finally { Pop-Location }
if ($reinstallRc -ne 0) { throw "reinstall rc=$reinstallRc" }
$lockText = Get-Content "$Proj/apm.lock.yaml" -Raw
foreach ($plug in $Plugins) {
  $rel = ".agents/plugins/$($plug.Name)/plugin.json"
  if ($lockText -notmatch [regex]::Escape($rel)) { throw "manifest absent from deployed_files: $rel" }
  $section = [regex]::Match($lockText, "(?ms)^  - repo_url: [^\r\n]*$([regex]::Escape($plug.Name))[^\r\n]*.*?(?=^  - repo_url: |\z)").Value
  if (-not $section -or $section -notmatch [regex]::Escape("${rel}: sha256:") -or $section -notmatch 'sha256:[0-9a-f]{64}') { throw "manifest missing from the correct dep/hash section: $rel" }
  foreach ($file in Get-ChildItem $plug.FullName -Recurse -File) {
    $fileRel = $file.FullName.Substring($Proj.Length + 1).Replace('\', '/')
    if ($section -notmatch [regex]::Escape($fileRel) -or $section -notmatch [regex]::Escape("${fileRel}: sha256:")) { throw "bundle file lacks per-dep path/hash provenance: $fileRel" }
  }
  if ($before[$plug.Name] -ne (Get-FileHash "$($plug.FullName)/plugin.json").Hash) { throw "manifest bytes changed on reinstall" }
}
```

- 預期：第二次 install exit `0`；每個 bundle 全部 deployed files（含 `plugin.json`）均在正確 dep 的 `deployed_files` 並有 `deployed_file_hashes`；manifest hash 與首次相同。
- 權威來源：`cmd/apm/install.go:881-888`；`design.md:121-147,182-186,194-195`；research R3 `antigravity-bundle-notes.md:256-262`。

#### [x] AGB-021 · agy 對 apm-go 實際 bundle validate PASS（PRD AC1）

- 證據（2026-07-11）：agy 1.1.1 對兩個生成 bundle 均 exit `0` + `[ok]`，各自 skills/agents/hooks 皆 `1 processed`。

```powershell
foreach ($plug in $Plugins) {
  $v = & agy plugin validate $plug.FullName 2>&1
  $rc = $LASTEXITCODE
  $text = $v -join "`n"
  if ($rc -ne 0 -or $text -notmatch '\[ok\]' -or $text -notmatch 'skills\s*:\s*1 processed' -or $text -notmatch 'agents\s*:\s*1 processed' -or $text -notmatch 'hooks\s*:\s*1 processed') { throw "agy validate failed for $($plug.FullName): $text" }
}
```

- 預期：每個 bundle exit `0`、含 `[ok]`，且 skills/agents/hooks 各 `1 processed`；`rules/` 不出現在 validator 摘要不是 PASS 證據也不是失敗，rules 由 AGB-017 byte/tree assertion 驗。
- 權威來源：PRD AC1 `prd.md:38-39`；research G1/G2 `antigravity-bundle-notes.md:223-226`；`.trellis/spec/backend/antigravity-target-contract.md:74-80`。

#### [x] AGB-022 · agy 負向：name 缺失必須 fail-closed

- 證據（2026-07-11）：TEMP bundle manifest=`{}` 時 exit 恰為 `1`，含 `plugin.json missing name`，無 `[ok]`。

```powershell
$BadName = Join-Path $Scratch "bad-missing-name"
Copy-Item $Plugins[0].FullName $BadName -Recurse
Set-Content "$BadName/plugin.json" '{}' -NoNewline
$out = & agy plugin validate $BadName 2>&1; $rc = $LASTEXITCODE
if ($rc -ne 1 -or (($out -join "`n") -notmatch 'plugin.json missing name')) { throw "unexpected missing-name result: rc=$rc out=$out" }
```

- 預期：exit **恰為 `1`**；輸出含 `plugin.json missing name`；不得出現 `[ok]`。
- 權威來源：research G3 `antigravity-bundle-notes.md:227`；`design.md:43-50`。

#### [x] AGB-023 · agy 負向：plugin.json 缺失必須 fail-closed

- 證據（2026-07-11）：TEMP bundle 刪除 manifest 後 exit 恰為 `1`，含 `missing plugin.json`，無 `[ok]`。

```powershell
$NoManifest = Join-Path $Scratch "bad-no-manifest"
Copy-Item $Plugins[0].FullName $NoManifest -Recurse
Remove-Item "$NoManifest/plugin.json"
$out = & agy plugin validate $NoManifest 2>&1; $rc = $LASTEXITCODE
if ($rc -ne 1 -or (($out -join "`n") -notmatch 'missing plugin.json')) { throw "unexpected missing-manifest result: rc=$rc out=$out" }
```

- 預期：exit **恰為 `1`**；輸出含 `missing plugin.json`；不得出現 `[ok]`。
- 權威來源：research G5 `antigravity-bundle-notes.md:229`；archive research `cli-plugins.md:123-126`。

#### [x] AGB-024 · 不產生 global/declared/version-tracking 副作用

- 證據（2026-07-11）：exit `0`；`.agents/plugins.json`、`installed_version.json`、bundle `commands/` 計數均 `0`，未調用 agy lifecycle mutations。

```powershell
if (Test-Path "$Proj/.agents/plugins.json") { throw "declared registration created" }
if (Get-ChildItem "$Proj/.agents/plugins" -Recurse -Filter installed_version.json) { throw "version tracking created" }
if (Get-ChildItem "$Proj/.agents/plugins" -Recurse -Directory | Where-Object Name -eq 'commands') { throw "commands unexpectedly deployed" }
```

- 預期：exit `0`；三類 out-of-scope 產物皆不存在；驗證只讀 workspace bundle，不呼叫 `agy plugin install/uninstall/enable/disable`。
- 權威來源：PRD Non-Goals `prd.md:46-50`；research caveat `cli-plugins.md:235-243`；`design.md:11-13`。

---

## 3. 安全不變式與 uninstall「只刪自己裝的」

#### [x] AGB-025 · archive symlink/hardlink 必須拒絕且零洩漏

- 證據（2026-07-11）：修正 harness 使兩者都執行；`TestSafeExtract_SymlinkEscape` 與 `HardlinkEscape` 均明確 PASS，無 SKIP。

```powershell
$out = go test ./internal/archive -run '^TestSafeExtract_(SymlinkEscape|HardlinkEscape)$' -count=1 -v 2>&1
$rc = $LASTEXITCODE
if ($rc -ne 0 -or ($out -join "`n") -match 'no tests to run|(?m)^--- SKIP:' -or ($out -join "`n") -notmatch 'PASS: TestSafeExtract_SymlinkEscape' -or ($out -join "`n") -notmatch 'PASS: TestSafeExtract_HardlinkEscape') { throw "archive link guards failed" }
```

- 預期：test command exit `0`；內部被測 `SafeExtract` 必須回 error（diagnostic 含 `link`）且 destination 無洩漏；測試不得 SKIP。
- 權威來源：`internal/archive/extract.go:116-121`（link guard 先於 path guard）；`internal/archive/extract_test.go:50-60`；OpenAPM `req-sc-002`。

#### [x] AGB-026 · copyTreeNoSymlinks 與 local destination ContainedKey guard 不得弱化

- 證據（2026-07-11）：修正 `SkipsSymlinks` 被 case-insensitive `SKIP` 誤命中後，兩測試均 PASS，真 symlink 已建立且外部 secret/兄弟 marker 存活。

```powershell
$out = go test ./internal/gitops -run 'TestCopyTreeNoSymlinks_SkipsSymlinks|TestMaterializeLocalCopy_RefusesKeyEscapingModulesDir' -count=1 -v 2>&1
$rc = $LASTEXITCODE; $text = $out -join "`n"
if ($rc -ne 0 -or $text -match 'no tests to run|(?m)^--- SKIP:' -or $text -notmatch 'PASS: TestCopyTreeNoSymlinks_SkipsSymlinks' -or $text -notmatch 'PASS: TestMaterializeLocalCopy_RefusesKeyEscapingModulesDir') { throw "local copy safety guard failed" }
```

- 預期：exit `0`、兩測試明確 PASS 且不得 SKIP；外部 secret symlink 不得被 follow/copy；含 `..` 的 `_local` key 必須被拒，sibling marker 存活。
- 權威來源：`internal/gitops/clone.go:241-249,273-318`；`internal/gitops/clone_test.go:372-432`；`internal/archive/extract.go:213-234`。

#### [x] AGB-027 · bundle E2E 不可重新引入已跳過的 local symlink

- 證據（2026-07-11）：Windows `SymbolicLink` 實體建立成功；install exit `0`，正常 SKILL.md 在 bundle，modules/bundle 的 `leak.txt` 均 0，secret bytes 零出現。

```powershell
$SymlinkProj = Join-Path $Scratch "symlink-project"
$SymlinkDep = Join-Path $Scratch "symlink-dep"
New-Item -ItemType Directory -Force "$SymlinkProj", "$SymlinkDep/.apm/skills/leak-skill" | Out-Null
Set-Content "$SymlinkProj/apm.yml" "name: symlink-project`nversion: `"1.0.0`"`ndependencies:`n  apm:`n    - $($SymlinkDep -replace '\\','/')`n" -NoNewline
Set-Content "$SymlinkDep/apm.yml" "name: symlink-dep`nversion: `"1.0.0`"`n" -NoNewline
Set-Content "$SymlinkDep/.apm/skills/leak-skill/SKILL.md" "safe`n" -NoNewline
$Secret = Join-Path $Scratch "outside-secret.txt"; Set-Content $Secret "MUST-NOT-DEPLOY" -NoNewline
New-Item -ItemType SymbolicLink -Path "$SymlinkDep/.apm/skills/leak-skill/leak.txt" -Target $Secret | Out-Null
Push-Location $SymlinkProj
try { & $Bin install --target agy *> "$Scratch/symlink-install.out"; $rc = $LASTEXITCODE } finally { Pop-Location }
if ($rc -ne 0) { throw "symlink fixture install rc=$rc" }
if (-not (Get-ChildItem "$SymlinkProj/.agents/plugins" -Recurse -Filter SKILL.md -File)) { throw "normal SKILL.md missing from bundle" }
if (Get-ChildItem "$SymlinkProj/apm_modules" -Recurse -Force | Where-Object Name -eq 'leak.txt') { throw "symlink reached materialized package" }
if (Get-ChildItem "$SymlinkProj/.agents/plugins" -Recurse -Force | Where-Object Name -eq 'leak.txt') { throw "symlink reached bundle" }
foreach ($file in Get-ChildItem "$SymlinkProj/.agents/plugins" -Recurse -File) {
  if ((Get-Content $file.FullName -Raw) -match 'MUST-NOT-DEPLOY') { throw "external bytes leaked into $($file.FullName)" }
}
```

- 預期：install exit `0`；正常 `SKILL.md` 存在於 bundle；`leak.txt` 在 materialized package 與 bundle 都不存在，外部 secret bytes 零出現。若平台無法建立 symlink，本項 FAIL，須移至支援 symlink 的環境執行，不得勾選 SKIP。
- 權威來源：`copyTreeNoSymlinks` 契約 `internal/gitops/clone.go:280-318`；research 的安全不變式要求；Python「never bundle symlinks」對照見該函式註解 `:285`。

#### [x] AGB-028 · RemoveDeployedFiles 三重 guard 與只刪明列檔案

- 證據（2026-07-11）：5 個列名測試全數明確 PASS，無 SKIP/no-tests；本體各自斷言 kept/diag/原 bytes 存活。

```powershell
$out = go test ./internal/deploy -run 'TestRemoveDeployedFiles_(HashMismatchIsKeptWithWarning|MissingHashKeyIsKept|PathEscapeIsRejected|UserHandwrittenFileNotInListUntouched|TargetIsDirectoryIsKept)$' -count=1 -v 2>&1
$rc = $LASTEXITCODE; $text = $out -join "`n"
if ($rc -ne 0 -or $text -match 'no tests to run|SKIP') { throw "RemoveDeployedFiles safety suite failed" }
```

- 預期：exit `0`；path escape、hash mismatch、missing hash、directory-at-file-path 全部保留並診斷；未列入 `deployed_files` 的手寫檔 untouched。不得用 bundle 目錄 `RemoveAll` 取代逐檔刪除。
- 權威來源：`internal/deploy/uninstall.go:12-33,34-81`；`internal/deploy/uninstall_test.go:49-148,255-279`；PRD AC3 `prd.md:41`。

#### [x] AGB-029 · 正常 uninstall 只移除指定 dep bundle，sibling/local 存活

- 證據（2026-07-11）：真 binary `uninstall ./dep-a` exit `0`；dep-a bundle 消失，dep-b hooks bytes、local agent、plugins root 均存活。

```powershell
$APath = $A[0].FullName; $BPath = $B[0].FullName
Push-Location $Proj
try { $out = & $Bin uninstall ./dep-a 2>&1; $rc = $LASTEXITCODE } finally { Pop-Location }
if ($rc -ne 0) { throw "uninstall dep-a rc=$rc out=$out" }
if (Test-Path $APath) { throw "dep-a bundle not fully pruned" }
if (-not (Test-Path $BPath -PathType Container)) { throw "sibling dep-b bundle removed" }
if (-not (Test-Path "$Proj/.agents/agents/local/agent.md" -PathType Leaf)) { throw "local primitive removed" }
if (-not (Test-Path "$Proj/.agents/plugins" -PathType Container)) { throw "shared plugins root removed despite sibling" }
```

- 預期：exit `0`；dep-a 已記錄且 hash 未變的檔全刪、dep-a 空 bundle dir 被修剪；dep-b bundle、local outputs、共享 `.agents/plugins/` 全存活。
- 權威來源：PRD AC3 `prd.md:41`；`design.md:149-156,184-187,198-199`；`internal/deploy/uninstall.go:73-80,133-151`。

#### [x] AGB-030 · bundle 內使用者手動檔案必須存活

- 證據（2026-07-11）：清除複本中 stale generated state 後，uninstall exit `0`；`USER-NOTES.md` SHA-256 `8DFEF3FA…E354AE` 不變，bundle 內僅剩該手寫檔。

```powershell
$ManualProj = Join-Path $Scratch "manual-copy"
Copy-Item $Proj $ManualProj -Recurse
# Copy-Item 會帶入原 project 的已生成狀態；複本路徑又會改變 local dep hash。
# 只清掉 generated state，保留 fixture source，避免舊/新 bundle 並存的 harness 假失敗。
foreach ($generated in @("$ManualProj/.agents", "$ManualProj/apm_modules", "$ManualProj/apm.lock.yaml")) {
  if (Test-Path $generated) { Remove-Item $generated -Recurse -Force }
}
# 以乾淨 fixture 重建 dep-b 後定位其唯一 bundle，再放未記錄檔。
Push-Location $ManualProj
try { & $Bin install --target agy *> "$Scratch/manual-install.out"; if ($LASTEXITCODE -ne 0) { throw "setup install failed" } } finally { Pop-Location }
$ManualPlugin = @(Get-ChildItem "$ManualProj/.agents/plugins" -Directory | Where-Object { (Get-Content "$($_.FullName)/hooks.json" -Raw) -match 'b-hook' })
if ($ManualPlugin.Count -ne 1) { throw "cannot identify dep-b bundle" }
Set-Content "$($ManualPlugin[0].FullName)/USER-NOTES.md" "keep me" -NoNewline
Push-Location $ManualProj
try { $out = & $Bin uninstall ./dep-b 2>&1; $rc = $LASTEXITCODE } finally { Pop-Location }
if ($rc -ne 0 -or -not (Test-Path "$($ManualPlugin[0].FullName)/USER-NOTES.md" -PathType Leaf) -or (Get-Content "$($ManualPlugin[0].FullName)/USER-NOTES.md" -Raw) -ne 'keep me') { throw "manual file was not preserved" }
```

- 預期：exit `0`；手動檔 bytes 不變、bundle dir 因非空而保留；其他由 dep-b 安裝且 hash 相符的檔可刪除。不得掃描後整目錄刪除。
- 權威來源：`internal/deploy/uninstall.go:12-17`；research `antigravity-bundle-notes.md:143-160`；`design.md:152-156`。

#### [x] AGB-031 · 手改 plugin.json 必須保留並明確警告

- 證據（2026-07-11）：修正 Windows path 參數轉送後 exit `0`；tampered SHA-256 `5A95C6F5…17EC6A3` 不變，stderr 含路徑 + `modified since deploy (hash mismatch)`，其他已記錄檔已刪。

```powershell
$TamperProj = Join-Path $Scratch "tamper-project"
Copy-Item $SymlinkProj $TamperProj -Recurse
foreach ($generated in @("$TamperProj/.agents", "$TamperProj/apm_modules", "$TamperProj/apm.lock.yaml")) {
  if (Test-Path $generated) { Remove-Item $generated -Recurse -Force }
}
Push-Location $TamperProj
try { & $Bin install --target agy *> "$Scratch/tamper-install.out"; if ($LASTEXITCODE -ne 0) { throw "setup install failed" } } finally { Pop-Location }
$TamperManifest = (Get-ChildItem "$TamperProj/.agents/plugins" -Recurse -Filter plugin.json -File | Select-Object -First 1).FullName
Set-Content $TamperManifest '{"name":"user-edited","note":"keep"}' -NoNewline
Push-Location $TamperProj
$depArg = $SymlinkDep -replace '\\','/' # must match the forward-slash string written to apm.yml
try { $out = & $Bin uninstall $depArg 2>&1; $rc = $LASTEXITCODE } finally { Pop-Location }
$text = $out -join "`n"
if ($rc -ne 0 -or -not (Test-Path $TamperManifest) -or (Get-Content $TamperManifest -Raw) -notmatch 'user-edited' -or $text -notmatch 'modified since deploy \(hash mismatch\)') { throw "tampered manifest guard failed: rc=$rc out=$text" }
```

- 預期：exit `0`；手改 manifest 原 bytes 保留；stderr 含檔案路徑與 `modified since deploy (hash mismatch)`；殘留非空 bundle dir 是正確安全結果。
- 權威來源：PRD AC3 `prd.md:41`；`internal/deploy/uninstall.go:52-70`（un-053）；`design.md:154-156,215`（R7）。

#### [x] AGB-032 · 空目錄修剪停止於 non-empty ancestor，永不刪 project root

- 證據（2026-07-11）：3 個列名測試全 PASS，無 SKIP/no-tests；本體斷言整鏈修剪、sibling halt、projectDir 存活。

```powershell
$out = go test ./internal/deploy -run 'TestRemoveDeployedFiles_(CleansUpEmptyParentDirectories|StopsCleanupWhenSiblingRemains|AntigravityAgentDirPrunedSiblingSurvives)$' -count=1 -v 2>&1
if ($LASTEXITCODE -ne 0 -or ($out -join "`n") -match 'no tests to run|(?m)^--- SKIP:') { throw "empty-parent cleanup suite failed" }
```

- 預期：exit `0` 且不得 `no tests to run`；空 ancestors 被逐層修剪，遇 sibling 立刻停止，`projectDir` 永遠存在。bundle 不得另寫放寬版 cleanup。
- 權威來源：`internal/deploy/uninstall.go:133-151`；`internal/deploy/uninstall_test.go:167-253`；research `antigravity-bundle-notes.md:152-160`。

---

## 4. 回歸、覆蓋率、A/B 與 spec gate

#### [x] AGB-033 · race gate 環境限制記錄（environment-blocked substitute）

- 原要求與不可行證據（第 1 輪，2026-07-11）：原始兩個 focused `-race` 命令均 rc=`2`，輸出 `go: -race requires cgo; enable cgo by setting CGO_ENABLED=1`；額外設 `CGO_ENABLED=1` 後仍因本機 PATH 無 `gcc` 而 build failed。本機沒有可用 cgo toolchain，且專案 `AGENTS.md` Available commands 未列 `-race`，故原 gate 在此環境不可執行；這是環境限制記錄，不代表 race 已通過。

```powershell
$out = go test ./internal/deploy/ -race -count=1 2>&1
$deployRc = $LASTEXITCODE; $deployText = $out -join "`n"
$out = go test ./cmd/apm/ -race -run TestRunUninstall -count=1 2>&1
$uninstallRc = $LASTEXITCODE; $uninstallText = $out -join "`n"
if ($deployRc -ne 0 -or $uninstallRc -ne 0 -or $deployText -match 'no tests to run|DATA RACE' -or $uninstallText -match 'no tests to run|DATA RACE') { throw "race gate failed: deploy=$deployRc uninstall=$uninstallRc" }
```

- 已接受的最強可行替代驗證（第 2 輪，2026-07-11）：下列四命令實跑均 rc=`0`；全 repo 17 個 package PASS，focused `internal/deploy` 與 `TestRunUninstall` 亦 PASS。替代組合包含靜態檢查、全 repo 非快取測試，以及兩個觸及面 focused 非快取測試，未以較弱的單一命令取代。

```powershell
go vet ./...
if ($LASTEXITCODE -ne 0) { throw "go vet ./... failed" }
go test ./... -count=1
if ($LASTEXITCODE -ne 0) { throw "go test ./... failed" }
go test ./internal/deploy -count=1
if ($LASTEXITCODE -ne 0) { throw "focused internal/deploy failed" }
go test ./cmd/apm -run TestRunUninstall -count=1
if ($LASTEXITCODE -ne 0) { throw "focused TestRunUninstall failed" }
```

- 復驗條件：本機或 CI 提供可用 cgo toolchain（`CGO_ENABLED=1` 且 `gcc`/等效 C compiler 可由 Go 找到）時，必須重跑上方原始兩個 `-race` 命令；兩者 exit `0`、無 `DATA RACE`/`FAIL`/`no tests to run` 後，才可把本環境替代證據升級為原始 race gate PASS。
- 權威來源：`implement.md:25,37-38,47-49`；`design.md:149-157`。

#### [x] AGB-034 · repo-wide build/vet/test release gate

- 證據（2026-07-11）：`go build ./...` exit `0`；`go vet ./...` exit `0`；`go test ./... -count=1` 全 package PASS（約 30.2s）。

```powershell
go build ./...
if ($LASTEXITCODE -ne 0) { throw "go build ./... failed" }
go vet ./...
if ($LASTEXITCODE -ne 0) { throw "go vet ./... failed" }
go test ./... -count=1
if ($LASTEXITCODE -ne 0) { throw "go test ./... failed" }
```

- 預期：三命令依序 exit `0`；零 compile/vet/test failure。這是 AC4 的必要但非充分條件。
- 權威來源：PRD AC4 `prd.md:42`；`implement.md:51-55`；`AGENTS.md` Available commands。

#### [x] AGB-035 · 觸及套件 coverage 各自 ≥ 80%

- 證據（2026-07-11）：兩指令 exit `0`；`internal/deploy=88.5%`、`cmd/apm=86.1%`。

```powershell
foreach ($pkg in @('./internal/deploy','./cmd/apm')) {
  $out = go test $pkg -cover -count=1 2>&1
  $rc = $LASTEXITCODE; $text = $out -join "`n"
  $m = [regex]::Match($text, 'coverage:\s+([0-9]+(?:\.[0-9]+)?)%')
  if ($rc -ne 0 -or -not $m.Success -or [double]$m.Groups[1].Value -lt 80) { throw "$pkg coverage gate failed: rc=$rc out=$text" }
}
```

- 預期：兩 package command exit `0`；`internal/deploy` 與 `cmd/apm` 各自 coverage 數值皆 `>= 80.0%`，不可用全 repo 加權平均掩蓋任一觸及套件不足。
- 權威來源：PRD AC4 `prd.md:42`；`implement.md:53-54`；既有基線 `.trellis/spec/conformance/cli-verification-checklist.md:597-601`。

#### [x] AGB-036 · antigravity 專屬 eval 全綠且直接驗生成 bundle

- 第 2 輪證據（2026-07-11）：以獨立系統 TEMP scratch 實跑，腳本 rc=`0`、`PASS=46`、`FAIL=0`、`SKIP=0`，末行 `ALL CHECKS PASSED (ab_antigravity)`；實作者聲稱的 `40/40` 未採信，實際目前共有 46 個 PASS 斷言。兩個 live validate leg 均 PASS，並精確驗 `dep-pkg: skills=1, agents=1, hooks=1`、`dep-pkg-2: hooks=1`。腳本沒有呼叫 Python oracle marketplace，scratch 已清除。腳本斷言強度的特別審查見文末 `ab-script-review`。

```powershell
$Scratch = Join-Path $env:TEMP ("ab-antigravity-r2-" + [guid]::NewGuid())
New-Item -ItemType Directory -Path $Scratch | Out-Null
$oldTemp = $env:TEMP; $oldTmp = $env:TMP
try {
  $env:TEMP = $Scratch; $env:TMP = $Scratch
  $out = python D:/Projects/apm-dev/evals/ab_antigravity.py 2>&1
  $rc = $LASTEXITCODE; $text = $out -join "`n"
  $passCount = @($out | Where-Object { $_ -match '^PASS  ' }).Count
  $failCount = @($out | Where-Object { $_ -match '^FAIL  ' }).Count
  $skipCount = @($out | Where-Object { $_ -match '^SKIP  ' }).Count
  if ($rc -ne 0 -or $passCount -ne 46 -or $failCount -ne 0 -or $skipCount -ne 0 -or $text -notmatch 'ALL CHECKS PASSED \(ab_antigravity\)') { throw "ab_antigravity.py rc=$rc pass=$passCount fail=$failCount skip=$skipCount" }
} finally {
  $env:TEMP = $oldTemp; $env:TMP = $oldTmp
  Remove-Item $Scratch -Recurse -Force -ErrorAction SilentlyContinue
}
```

- 預期：exit `0`；精確 46 個 `PASS`、零 `FAIL`/`SKIP`，末行含 `ALL CHECKS PASSED (ab_antigravity)`；dep 路徑斷言已改為 bundle，且 live leg 對 apm-go **實際生成**的 `.agents/plugins/<pkg>` 執行 `agy plugin validate`，不得再手工 repack 平鋪輸出。
- 權威來源：PRD AC4 `prd.md:42`；`implement.md:56-67`；補強後腳本 `D:/Projects/apm-dev/evals/ab_antigravity.py:148-199,216-399`；research `antigravity-bundle-notes.md:274-279,288-310`。

#### [x] AGB-037 · generic uninstall eval 無回歸

- 證據（2026-07-11）：exit `0`；`total: 6 passed, 0 failed, 2 documented deviations`，deviation 仍僅原有 standalone MCP / `-g`。

```powershell
$out = python D:/Projects/apm-dev/evals/ab_uninstall.py 2>&1
$rc = $LASTEXITCODE; $text = $out -join "`n"
if ($rc -ne 0 -or $text -notmatch 'total:\s+\d+ passed, 0 failed, \d+ documented deviations') { throw "ab_uninstall.py rc=$rc" }
```

- 預期：exit `0`；總結行符合 `total: <N> passed, 0 failed, <N> documented deviations`；既有 standalone MCP、not-found、`-g` documented deviations 不因 bundle 變更漂移。
- 權威來源：`D:/Projects/apm-dev/evals/ab_uninstall.py:1-28,162-177`；`design.md:149-157`（MCP/uninstall 零特例）。

#### [x] AGB-038 · antigravity spec 已記錄 documented extension（PRD AC5）

- 證據（2026-07-11）：12/12 hard needles 全命中；逐節讀取 §7.1–7.8 確認 fail-closed、MCP 不遷入、same-dep hook caveat、uninstall/provenance/migration/Python deviation 均有實質內容。

```powershell
$Spec = '.trellis/spec/backend/antigravity-target-contract.md'
$required = @(
  'Plugin bundle deployment',
  'documented extension',
  '.agents/plugins/',
  'plugin.json',
  'name',
  'hooks.json',
  'mcp_config.json',
  '1.1.1',
  'Python',
  'symlink',
  'ContainedKey',
  'hash mismatch'
)
$text = Get-Content $Spec -Raw
foreach ($needle in $required) { if ($text -notmatch [regex]::Escape($needle)) { throw "spec missing: $needle" } }
```

- 預期：exit `0`；spec 明列 bundle 佈局、DepKey→安全 bundle 名規則、1.1.1 `name` 必填 delta、跨 dep hooks 隔離/同 dep 多 hooks caveat、MCP 不遷入、local 平鋪、Python 無 bundle 的 deviation、symlink/containment/hash/uninstall 安全線、re-install provenance 與遷移 caveat。
- 權威來源：PRD AC5 `prd.md:43-44`；`implement.md:69-75`；`design.md:159-169,206-216`；research `antigravity-bundle-notes.md:245-279`。

#### [x] AGB-039 · 最終證據包與工作樹範圍

- 第 2 輪證據（2026-07-11）：收尾 `git status --short` 的路徑集合與本輪執行前 baseline 完全相同，沒有新增越權路徑；tracked 仍僅本 task 既有的 spec、task.json、`internal/deploy` 4 檔，untracked 仍為既有 task artifacts/新增測試與實作檔（另有本輪開始前已存在的 `07-11-agents-md-compile` untracked 集合）。本輪唯一內容修改是本 `checklist.md`；`git diff --check` rc=`0`，checklist no-index check rc=`1`（新檔有 diff 的預期值）且零 whitespace 訊息，final checklist `727` 行；未改程式碼/evals，未 commit/push。

```powershell
git status --short
git diff --check
$Checklist = '.trellis/tasks/07-11-antigravity-plugins-bundle/checklist.md'
$whitespace = git diff --no-index --check -- /dev/null $Checklist 2>&1
if ($LASTEXITCODE -gt 1 -or $whitespace) { throw "untracked checklist has whitespace errors: $whitespace" }
git ls-files --error-unmatch $Checklist 2>$null | Out-Null
if ($LASTEXITCODE -eq 0) {
  git diff -- $Checklist
} else {
  $checklistDiff = git diff --no-index -- /dev/null $Checklist 2>&1
  if ($LASTEXITCODE -notin 0,1) { throw "cannot render untracked checklist diff" }
  $checklistDiff
}
```

- 預期：三命令 exit `0`；`git diff --check` 零輸出；驗證紀錄附 AGB-001～038 的 command、exit code、關鍵輸出及 scratch tree/hash。執行本「建立 checklist」任務時唯一新增/修改檔必須是本檔；不得 commit/push。既存其他 dirty files 必須逐項標成 pre-existing，不得改寫或清理。
- 權威來源：本 task 使用者限制；`.trellis/spec/conformance/cli-verification-checklist.md:13`（附證據才可勾）；`implement.md:94-101` review gate。

---

## 特別審查（不計入 39 項）

`ab-script-review: resolved`

- 弱化點 1 已解：`dep-pkg` fixture 同時提供 rules/agents/skills/hooks 四型，四個目的檔逐一以來源與輸出的 `read_bytes()` 精確比較；dep-pkg-2 hooks 亦為 byte-identical。
- 弱化點 2 已解：新增 hooks-only `dep-pkg-2`，先各自與來源 bytes 比對，再斷言兩個 dependency bundle 的 hooks bytes 不同且 local hooks 不含任一 dep hook。
- 弱化點 3 已解：先 uninstall dep-pkg 並驗 dep-pkg-2 sibling bundle/hooks byte-identical 存活；再於 dep-pkg-2 bundle 放未記錄 `USER-NOTES.md`，uninstall 後驗 generated manifest/hooks 移除、手寫檔與非空 bundle 目錄存活。
- 弱化點 4 已解：`manifest_report()` 斷言 JSON key set **恰為** `{"name"}`，並同時鎖定 name=目錄名、無 BOM、LF 結尾。
- 弱化點 5 已解：`validate_bundle()` 以 regex 解析每個 component 的整數 `N processed` 並精確比 expected count；實跑兩個 apm-go 真實生成 bundle，分別驗 `skills=1/agents=1/hooks=1` 與 `hooks=1`。

---

_覆蓋：39 項；PRD AC1～AC5、安全不變式、負向案例、repo-wide gate、coverage 與對應 `ab_*.py` 均有獨立 PASS/FAIL 判準。_
