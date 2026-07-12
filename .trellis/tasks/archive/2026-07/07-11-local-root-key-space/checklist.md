# 存活 local root key 空間修復 — 硬性驗證清單

> **目的**：驗證 `uninstallRemainingRootKeys` 對存活 local root 只輸出
> `_local/<sanitizedBase>-<sha8>` module key，並證明 reachability BFS、stale-MCP
> 與既有刪除安全線均未弱化。
>
> **權威來源優先序**：任務 PRD／本專案 spec → 官方 uninstall 文件 → Python
> oracle → research 與 Go 現況。Go 特有的 `_local/<base>-<sha8>` 是本專案契約，
> Python oracle 不產生同一種合成 key，不得拿 Python 的不同內部表示推翻 spec。
>
> **圖例**：`[ ]` 待驗 · `[x]` 已驗——**附證據才可勾**。每項證據至少包含：
> 實際指令、exit code、關鍵輸出／斷言與檔案系統結果；只寫「PASS」不得勾選。

---

## 0. 執行前置與安全

- **Repo / Build**：在 `D:\Projects\apm-dev\apm-go` 執行；release-style binary
  指令固定為 `go build -o bin/apm-go.exe ./cmd/apm`，binary 名稱不得改成 `apm.exe`。
- **Python oracle**：`uv --project D:/Projects/apm-dev/apm run apm <args>`；PowerShell
  每次以 `$LASTEXITCODE` 驗精確 exit code，不得只看「有沒有錯誤文字」。
- **Scratch 隔離**：所有真 CLI `install` / `uninstall` 只可在 `%TEMP%` 下的新目錄執行；
  不得在本 repo 根目錄或 Python repo 根目錄執行。Go marketplace registry 必須把
  `$env:APM_CONFIG_DIR` 指向 scratch 子目錄。
- **禁止 oracle marketplace 寫入**：Python oracle 的 marketplace registry 固定使用真實
  `~/.apm/marketplaces.json`，不尊重 Go 的 `APM_CONFIG_DIR`。本清單**禁止**對 oracle
  執行 `marketplace add/remove/update`；本清單選用的 `ab_uninstall.py` 與
  `ab_antigravity.py` 不做這些寫入。若另行人工探測，必須先另開核准流程與備份，不可把
  備份視為本清單已授權寫入。
- **清理**：驗證結束刪除 scratch；不得 `git commit` / `git push`。任何預期外 repo diff
  一律 FAIL，不可以「順便修」掩蓋。

#### [x] PRE-01 · 工具、權威檔與 eval 前置完整

證據（2026-07-11）：原列 PowerShell 前置檢查逐項執行，exit `0`；`$missing` 與缺少工具清單均為空，`go`／`uv`／`python` 皆可解析。

```powershell
Set-Location D:/Projects/apm-dev/apm-go
$required = @(
  'go.mod',
  '.trellis/tasks/07-11-local-root-key-space/prd.md',
  '.trellis/spec/backend/install-marketplace-contracts.md',
  'D:/Projects/apm-dev/apm/src/apm_cli/commands/uninstall/engine.py',
  'D:/Projects/apm-dev/apm/docs/src/content/docs/reference/cli/uninstall.md',
  'D:/Projects/apm-dev/evals/ab_uninstall.py',
  'D:/Projects/apm-dev/evals/ab_antigravity.py'
)
$missing = $required | Where-Object { -not (Test-Path -LiteralPath $_) }
if ($missing -or -not (Get-Command go -ErrorAction SilentlyContinue) -or
    -not (Get-Command uv -ErrorAction SilentlyContinue) -or
    -not (Get-Command python -ErrorAction SilentlyContinue)) {
  $missing; exit 1
}
exit 0
```

預期：exit `0`、stdout/stderr 無缺檔或缺工具名稱、檔案系統零變動。

【權威】`.trellis/spec/conformance/cli-verification-checklist.md:17-24`；
`prd.md:19-33`。

#### [x] PRE-02 · scratch 與 registry 隔離成立

證據（2026-07-11）：原列 PowerShell 隔離檢查 exit `0`；建立 `C:\Users\gn006\AppData\Local\Temp\apm-go-local-root-key-43b5c5a7-53b0-456e-a799-3cba58e87407`，`APM_CONFIG_DIR` 位於其 `apm-config` 子目錄，且不在 repo；驗證後 `SCRATCH_REMOVED=True`。三個實作／測試檔 SHA-256 驗前驗後相同，repo status 除既有變更與本 checklist 外無新增變更。

```powershell
$Repo = (Resolve-Path 'D:/Projects/apm-dev/apm-go').Path
$Scratch = Join-Path $env:TEMP ("apm-go-local-root-key-" + [guid]::NewGuid())
New-Item -ItemType Directory -Path $Scratch | Out-Null
$env:APM_CONFIG_DIR = Join-Path $Scratch 'apm-config'
New-Item -ItemType Directory -Path $env:APM_CONFIG_DIR | Out-Null
Set-Location $Scratch
if ((Resolve-Path .).Path.StartsWith($Repo, [System.StringComparison]::OrdinalIgnoreCase)) { exit 1 }
if (-not $env:APM_CONFIG_DIR.StartsWith($Scratch, [System.StringComparison]::OrdinalIgnoreCase)) { exit 1 }
exit 0
```

預期：exit `0`；只新增 `%TEMP%/apm-go-local-root-key-*` 與其 `apm-config/`，repo
內零新增／修改；後續 live probe 的 cwd 必須維持在此 scratch。

【權威】`.trellis/spec/conformance/cli-verification-checklist.md:22-24`。

---

## 1. TDD 與 key-space 契約

#### [x] LKS-01 · 修復前重現測試必須紅，且同時證明兩個使用者可見後果

證據（2026-07-11，第 2 輪）：依本輪特別授權執行 `git stash push -- cmd/apm/uninstall.go`（exit `0`）後跑原列三案，`go test` exit `1`。producer 四個 prod/dev × relative/absolute subtest 均實得 `local:<path>`、預期 `_local/dep-pkg-<sha8>`；diamond 案輸出 `Removed 1 package(s) (+1 transitive orphan(s))`，並實際斷言 X module 與 lock entry 消失；MCP 案實際斷言 `.mcp.json` 為空且 lock `MCPServers=[]`，證明 `srvA` 被誤清。隨即 `git stash pop` exit `0`；stash 數回復、前後 `git diff -- cmd/apm/uninstall.go` 精確相同，`--numstat` 仍只有 `1 1 cmd/apm/uninstall.go`，現況唯一 hunk 為 `remaining[k]` → `remaining[uninstallRemovalKey(k)]`，無其他 production diff。

先新增下列三個測試，**尚未改 production code 前**執行：

```powershell
Set-Location D:/Projects/apm-dev/apm-go
go test ./cmd/apm -run '^(TestUninstallRemainingRootKeys_LocalRootUsesModulesKey|TestRunUninstall_SurvivingLocalRootProtectsDiamondTransitive|TestRunUninstall_SurvivingLocalRootMCPServerSurvives)$' -count=1 -v
$LASTEXITCODE
```

預期：`go test` exit `1`；輸出含 `--- FAIL:`，且證據必須分別顯示：(a) producer
實得 `local:./dep-pkg`、預期 `_local/dep-pkg-<8 lowercase hex>`；(b) shared transitive
被錯列為刪除／已消失；(c) `srvA` 被錯判 stale／從 target 或 lock 移除。若只讓一個人造
assert 失敗、沒有重現 (b)(c)，FAIL。測試只能寫 `t.TempDir()`／`chdirTemp`，repo fixture
不得被改寫。

【權威】`prd.md:23-30`；`research/local-root-key-space-gap.md:219-240,268-274`。

#### [x] LKS-02 · production 修復維持一行、且不碰已修好的 removal 路徑

證據（2026-07-11）：原列 `git diff --unified=0`／`--numstat` 驗證 exit `0`；唯一 hunk 為 `remaining[k] = true` → `remaining[uninstallRemovalKey(k)] = true`，numstat 精確為 `1  1  cmd/apm/uninstall.go`。

```powershell
Set-Location D:/Projects/apm-dev/apm-go
$d = git diff --unified=0 -- cmd/apm/uninstall.go
$ok = $d -match '(?m)^-\s*remaining\[k\] = true\s*$' -and
      $d -match '(?m)^\+\s*remaining\[uninstallRemovalKey\(k\)\] = true\s*$'
$changed = git diff --numstat -- cmd/apm/uninstall.go
if (-not $ok -or $changed -notmatch '^1\s+1\s+cmd/apm/uninstall.go$') { $d; exit 1 }
exit 0
```

預期：exit `0`；production diff 只有 `remaining[k]` →
`remaining[uninstallRemovalKey(k)]` 一換一；`uninstallRemovalKey`、
`removedIdentities` 比較、其他 uninstall key 處理均零變動。

【權威】`prd.md:17,35-38`；`research/local-root-key-space-gap.md:276-294`；
`cmd/apm/uninstall.go:173-192,395-410`。

#### [x] LKS-03 · 同一批重現測試修復後全綠

證據（2026-07-11）：原列三案命令 exit `0`；producer、diamond transitive、MCP survivor 三案各有 `--- PASS:`，尾行 `ok github.com/apm-go/apm/cmd/apm`。逐一讀取測試本體，三案皆含實際狀態斷言，非 skip／空測試。

```powershell
go test ./cmd/apm -run '^(TestUninstallRemainingRootKeys_LocalRootUsesModulesKey|TestRunUninstall_SurvivingLocalRootProtectsDiamondTransitive|TestRunUninstall_SurvivingLocalRootMCPServerSurvives)$' -count=1 -v
$LASTEXITCODE
```

預期：exit `0`；三案各有 `--- PASS:`，尾行為 `ok .../cmd/apm`；不得以 skip、刪除
assert 或只改 fixture 取得綠燈。檔案系統效果只存在測試 temp 目錄。

【權威】`prd.md:27-30`；`research/local-root-key-space-gap.md:23-27`。

#### [x] LKS-04 · local key 轉譯矩陣完整（prod/dev × relative/absolute）

證據（2026-07-11）：原列命令 exit `0`；`prod_relative`、`prod_absolute`、`dev_relative`、`dev_absolute` 四 subtest 全 PASS。測試本體逐案斷言 map 長度 `1`、含 `localModulesKey(resolveLocalSourceAbs(path))`、key 符合 `^_local/[^/]+-[0-9a-f]{8}$` 且無 `local:` prefix。

```powershell
go test ./cmd/apm -run '^TestUninstallRemainingRootKeys_LocalRootUsesModulesKey$' -count=1 -v
$LASTEXITCODE
```

預期：exit `0`；table test 至少四 case：`dependencies.apm` 與
`devDependencies.apm` 各含 relative `./dep-pkg`、absolute local path。每案結果 map
恰含 `localModulesKey(resolveLocalSourceAbs(path))`，符合
`^_local/[^/]+-[0-9a-f]{8}$`，且沒有任何以 `local:` 起始的 key；輸出各 subtest PASS。

【權威】`prd.md:21-24`；`cmd/apm/install.go:1277-1329`；
`research/local-root-key-space-gap.md:83-89,305-309`；spec
`backend/install-marketplace-contracts.md:58-70,74-75`。

#### [x] LKS-05 · identity-space 篩選語意不變

證據（2026-07-11）：原列兩案命令 exit `0`，兩案均 PASS。測試本體確認 `removedIdentities["local:./dep-pkg"]` 使結果 map 為空，且非 local `acme/foo` 結果精確維持單一同名 key。

```powershell
go test ./cmd/apm -run '^(TestUninstallRemainingRootKeys_RemovedLocalRootExcluded|TestUninstallRemainingRootKeys_NonLocalKeyUnchanged)$' -count=1 -v
$LASTEXITCODE
```

預期：exit `0`；被移除的 `local:./dep-pkg` 必須在轉譯前由
`removedIdentities` 排除，不能以 `_local/...` 重新出現；git／marketplace root 的 key
逐 byte 不變。兩案 PASS、temp 外零檔案效果。

【權威】`research/local-root-key-space-gap.md:64-67,289-294`；
`cmd/apm/uninstall.go:127-139,186-192,400-410`。

---

## 2. Reachability、MCP 與 CLI 可觀察結果

#### [x] LKS-06 · BFS 從真實 `_local/...` 目錄走到傳遞依賴

證據（2026-07-11）：原列命令 exit `0`，唯一測試 PASS。fixture 自建 `apm_modules/_local/<key>/apm.yml` 與含 local/X 兩 key 的 lock；斷言 reachable 精確長度 `2`、同時含 local root 與 `acme/transitive-of-a`、不含 `local:./dep-pkg`。

```powershell
go test ./cmd/apm -run '^TestReachableFromRemainingRoots_LocalModulesKeyWalksTransitive$' -count=1 -v
$LASTEXITCODE
```

預期：exit `0`；fixture 的 `apm_modules/_local/dep-pkg-<sha8>/apm.yml` 宣告
`acme/transitive-of-a`，lock 同時含兩 key；結果集合恰含 local root 與 transitive，
不得嘗試讀 `apm_modules/local:./dep-pkg/`。輸出為該測試 PASS。

【權威】`cmd/apm/uninstall.go:310-349`；`research/local-root-key-space-gap.md:91-126`；
Python oracle `engine.py:421-441`；官方 `uninstall.md:80`。

#### [x] LKS-07 · real uninstall 保護 diamond shared transitive，且只移除 B

證據（2026-07-11，第 2 輪）：原列命令 exit `0`，測試 PASS。逐行審讀 fixture：A 與 B 的真實 `apm.yml` 都宣告 `acme/transitive-of-a`，而 X 的 `ResolvedBy` 刻意只指向 B，已形成真 diamond。測試 capture stdout，斷言精確 `Removed 1 package(s)`、無 `transitive orphan`、不含 A key/X；另逐項斷言 A/X module 與 lock、A manifest entry 存活，B module、deployed file、lock 與 manifest entry 消失。

```powershell
go test ./cmd/apm -run '^TestRunUninstall_SurvivingLocalRootProtectsDiamondTransitive$' -count=1 -v
$LASTEXITCODE
```

預期：exit `0`。fixture 必須為存活 local A + 被移除 git B + A/B 共用 X，且 X 的
`ResolvedBy` 刻意指向 B。執行 `runUninstall(["acme/pkgB"])` 後：

- `apm_modules/_local/dep-pkg-<sha8>/`、`apm_modules/acme/transitive-of-a/` 存在；
- A、X 的 lock entries 與 A 的 apm.yml entry 存在；
- B 的 apm_modules、lock entry、apm.yml entry、B 專屬 deployed file 均不存在；
- stdout summary 只列 B／真孤兒，不得列 A 或 X；測試輸出 `--- PASS:`。

【權威】`prd.md:15,23-24`；`research/local-root-key-space-gap.md:111-122,221-230`；
Python oracle `engine.py:421-455`；官方 `uninstall.md:18,78-85`。

#### [x] LKS-08 · 存活 local root 的 MCP 不得被判 stale

證據（2026-07-11，第 2 輪）：原列命令 exit `0`，測試 PASS。測試現以 `os.Pipe` capture stderr 並持續斷言不得含 `srvA`；同時解析 `.mcp.json` 與重讀 lockfile，精確斷言 `srvA` 兩處存活、B-only `srvB` 兩處移除，證明 stale cleanup 未被整段關閉。

```powershell
go test ./cmd/apm -run '^TestRunUninstall_SurvivingLocalRootMCPServerSurvives$' -count=1 -v
$LASTEXITCODE
```

預期：exit `0`；uninstall B 後，A 自己宣告的 `srvA` 同時仍存在 target MCP config
（例如 `.mcp.json` 的 server entry）與 `apm.lock.yaml` 的 `mcp_servers`；若 fixture 另放
B-only server，該 server 必須被移除，證明不是把 stale cleanup 整段關掉。輸出該測試
PASS，stderr 不得有移除 `srvA` 的 warning。

【權威】`prd.md:15-16,23-24`；`cmd/apm/uninstall_mcp.go:47-76`；
`research/local-root-key-space-gap.md:128-160,221-230`；Python oracle
`engine.py:693-724`；官方 `uninstall.md:83`。

#### [x] LKS-09 · dry-run 與 real-run 使用相同修正後 root key，且 dry-run 零寫入

證據（2026-07-11，第 2 輪）：原列命令 exit `0`，測試 PASS。測試逐檔 snapshot A/B/X 三棵 module trees 並於 dry-run 後比對檔案集合與 bytes；`apm.yml`、lock、`.mcp.json` 亦逐 byte 相同。capture stdout 後斷言 plan 含 B、不含 A/X、含 `[dry-run]` 且不含 `[+] Removed`，確認走 preview 路徑而非 real-run。

```powershell
go test ./cmd/apm -run '^TestRunUninstall_SurvivingLocalRootDryRunKeepsSharedTransitive$' -count=1 -v
$LASTEXITCODE
```

預期：exit `0`；dry-run plan 不得把 A/X 列入 removal，必須列 B；執行前後
`apm.yml`、`apm.lock.yaml`、A/B/X module trees、target MCP config 的 byte/hash snapshot
完全相同。輸出是 preview/plan 樣式而非成功刪除樣式，測試本身 PASS。

【權威】`cmd/apm/uninstall.go:113-153,214-230`；官方 `uninstall.md:32,50-54,103`；
research `local-root-key-space-gap.md:162-175`。

#### [x] LKS-10 · devDependencies 下的存活 local root 走同一條完整保護路徑

證據（2026-07-11，第 2 輪）：原列命令 exit `0`，測試 PASS。A 放在 `devDependencies.apm`；測試斷言 A/X module、lock entries、target `srvA`、lock `srvA` 與 dev manifest entry 全存活；B module、lock/manifest entry、target/lock 的 `srvB` 均移除，完整覆蓋 prod 同一路徑。

```powershell
go test ./cmd/apm -run '^TestRunUninstall_SurvivingLocalDevRootProtectsTransitiveAndMCP$' -count=1 -v
$LASTEXITCODE
```

預期：exit `0`；A 改放 `devDependencies.apm` 後，uninstall B 仍保留 A、A 的 transitive
與 `srvA`（module dirs、lock entries、target config 全存活），B 正常移除；輸出 PASS。

【權威】`cmd/apm/uninstall.go:409-410`；`research/local-root-key-space-gap.md:305-309`；
官方 `uninstall.md:78`。

---

## 3. 安全不變式與負向案例（不得弱化）

#### [x] SEC-01 · `archive.ContainedKey` 拒絕所有 `..` 形狀

證據（2026-07-11）：原列命令 exit `0`，`TestContainedKey` PASS；讀取 table test 確認正常 `acme/foo`/深層正常 key 為 true，`../../../evil`、`..`、`acme/..`、`acme/../other`、深層 escape 與反斜線版本均逐案斷言 false。

```powershell
go test ./internal/archive -run '^TestContainedKey$' -count=1 -v
$LASTEXITCODE
```

預期：exit `0`；`../`、深層 escape、`acme/../other`（仍在 root 內但指向 sibling）與
反斜線版本全部回傳 false，正常 `acme/foo` 回傳 true；輸出 PASS、磁碟零效果。

【權威】spec `backend/install-marketplace-contracts.md:77`；
`internal/archive/extract.go:224-234`；`internal/archive/extract_test.go:213-233`；
archive uninstall checklist `un-031`（`uninstall-checklist.md:80-82`）。

#### [x] SEC-02 · local copy destination 仍受 `ContainedKey` fail-closed

證據（2026-07-11，第 2 輪）：原列命令 exit `0`，測試 PASS。惡意 `_local/../victim` 回錯，診斷持續斷言含 `refusing` 或 `outside`；`apm_modules/_local` 完全未建立，victim 目錄仍只含原 marker，marker bytes 精確等於 `must survive`。

```powershell
go test ./internal/gitops -run '^TestMaterializeLocalCopy_RefusesKeyEscapingModulesDir$' -count=1 -v
$LASTEXITCODE
```

預期：exit `0`；惡意 module key 使 materialization 回錯，錯誤含 `refusing`／
`outside`，`apm_modules` 外 canary 原 bytes 存活且錯誤目的地不存在；輸出 PASS。

【權威】spec `backend/install-marketplace-contracts.md:61,77`；
`internal/gitops/clone.go:241-253`。

#### [x] SEC-03 · archive symlink / hardlink entry 仍硬拒絕

證據（2026-07-11，第 2 輪）：執行 `go test ./internal/archive -run '^TestSafeExtract_(SymlinkEscape|HardlinkEscape)$' -count=1 -v`，exit `0`，兩案均 PASS、無 SKIP。symlink 使用 oracle fixture；hardlink 以 `tar.TypeLink` + `../../etc/passwd` 合成真 entry。兩案都斷言 error 含 `link`、dest 與 `.apmtmp` 不存在，並讀回 destination 外 canary bytes 精確未變。

```powershell
go test ./internal/archive -run '^TestSafeExtract_(SymlinkEscape|HardlinkEscape)$' -count=1 -v
$LASTEXITCODE
```

預期：exit `0`；`SafeExtract` 對 symlink／hardlink archive 均回錯，診斷含 `link`，
destination 外 canary 不存在／未變；兩案輸出 PASS。不得把拒絕降級為跟隨或靜默複製。

【權威】spec `backend/install-marketplace-contracts.md:77`；
`internal/archive/extract.go:116-120`；`internal/archive/extract_test.go:50-61`。

#### [x] SEC-04 · `copyTreeNoSymlinks` 不跟隨、不複製 local source symlink

證據（2026-07-11，第 2 輪）：原列命令 exit `0`，測試 PASS、無 SKIP。目的地正常檔讀回 bytes 精確為 `in-tree`；`Lstat(dst/leak.txt)` 證明 link 與 dereferenced copy 都不存在；source tree 外 secret 亦讀回精確原 bytes `must not be copied`。

```powershell
go test ./internal/gitops -run '^TestCopyTreeNoSymlinks_SkipsSymlinks$' -count=1 -v
$LASTEXITCODE
```

預期：exit `0`；正常檔被複製，指向 source tree 外 secret 的 symlink 在目的地完全
不存在（既非 link 也非 dereferenced bytes），外部 secret 未變；不支援 symlink 的平台
只能以明確 `SKIP` 附平台證據，不能冒充已驗 PASS。

【權威】spec `backend/install-marketplace-contracts.md:61,77`；
`internal/gitops/clone.go:280-322`；Python 同類原則
`bundle/packer.py:258-270`。

#### [x] SEC-05 · `SafeRemoveModuleDir` 拒絕 escape，且 sibling package 存活

證據（2026-07-11，第 2 輪）：原列兩案命令 exit `0`，兩案 PASS。escape 案斷言 error、`removed=false` 與外部 victim 存活；正常案的 sibling 改為含真實 `apm.yml` bytes，刪 foo 後讀回 bar bytes 精確相同，並斷言 `apm_modules/acme` 只剩 `bar`，證明 cleanup 只移除真的空父鏈。

```powershell
go test ./internal/deploy -run '^(TestSafeRemoveModuleDir_PathEscapeIsRejected|TestSafeRemoveModuleDir_SiblingPackageSurvives)$' -count=1 -v
$LASTEXITCODE
```

預期：exit `0`；`../victim` 回 `removed=false` + error，外部 canary 存活；正常刪
`acme/foo` 時 `acme/bar` 與內容 byte-identical 存活，只清空真的空父目錄；兩案 PASS。

【權威】`internal/deploy/uninstall.go:84-130`；官方 `uninstall.md:79,85`；
archive checklist `un-030~032`（`uninstall-checklist.md:80-82`）。

#### [x] SEC-06 · 「只刪自己裝的」：另一套件與使用者手寫檔不得動

證據（2026-07-11，第 2 輪）：原列跨兩 package 命令 exit `0`，兩測試均 PASS。CLI 案移除 foo 的 manifest/module/lock/deployed file，並讀回 bar deployed file 精確為原 `bar rule`；deploy 案只刪列入 provenance 的檔，讀回未列入的手寫檔精確為原 `user notes`。

```powershell
go test ./cmd/apm ./internal/deploy -run '^(TestRunUninstall_OnlyRemovesTargetedPackagesFiles|TestRemoveDeployedFiles_UserHandwrittenFileNotInListUntouched)$' -count=1 -v
$LASTEXITCODE
```

預期：exit `0`；移除 foo 後 bar 的 manifest entry、module dir、lock entry、deployed
file 全部存活；不在 `deployed_files` 的手寫檔 byte-identical 存活；只刪 foo 追蹤且 hash
相符的檔案。兩 package 的測試輸出皆 PASS。

【權威】官方 `uninstall.md:20,81`；Python oracle `cli.py:178-196`；
`internal/deploy/uninstall.go:12-33`；archive checklist
`un-V02/un-050~053`（`uninstall-checklist.md:97-100,152-154`）。

#### [x] SEC-07 · provenance/hash 不足或不符時一律保留並警告

證據（2026-07-11，第 2 輪）：原列三案命令 exit `0`，三案 PASS。unit tests 精確斷言 diagnostics 分別含 `modified since deploy (hash mismatch)` 與 `no recorded hash`，removed/kept 集合正確且兩檔 bytes 未變；CLI 以 `Verbose:false` capture stderr，仍必須含 hash-mismatch warning，並讀回 user-edited bytes。

```powershell
go test ./internal/deploy ./cmd/apm -run '^(TestRemoveDeployedFiles_HashMismatchIsKeptWithWarning|TestRemoveDeployedFiles_MissingHashKeyIsKept|TestRunUninstall_HashMismatchKeepsFileWithWarning)$' -count=1 -v
$LASTEXITCODE
```

預期：exit `0`；modified file 與 missing-hash file 都存在且 bytes 不變，removed list
不含它們；診斷分別含 `modified since deploy (hash mismatch)` 與
`no recorded hash`，CLI stderr warning 不受 verbose 控制；三案 PASS。

【權威】`internal/deploy/uninstall.go:19-31,52-70`；archive checklist 安全紅線
`un-053/un-V03`（`uninstall-checklist.md:40,100,154`）。

#### [x] SEC-08 · deployed-files path escape 仍拒絕，外部檔零變動

證據（2026-07-11，第 2 輪）：原列命令 exit `0`，測試 PASS。斷言 removed 空、kept 精確為 `../escaped-uninstall-victim.txt`、唯一診斷含 `path escapes project directory`；外部 canary 讀回 bytes 精確為 `should never be deleted`。

```powershell
go test ./internal/deploy -run '^TestRemoveDeployedFiles_PathEscapeIsRejected$' -count=1 -v
$LASTEXITCODE
```

預期：exit `0`；`../escaped-uninstall-victim.txt` 不在 removed、在 kept，診斷含
`path escapes project directory`，外部 canary bytes 不變；輸出 PASS。

【權威】spec `backend/install-marketplace-contracts.md:77`；
`internal/deploy/uninstall.go:34-40`；`internal/deploy/uninstall_test.go:101-126`。

#### [x] SEC-09 · 被移除 local dep 的既有 `uninstallRemovalKey` 路徑無回歸

證據（2026-07-11，第 2 輪）：原列兩案命令 exit `0`，兩案 PASS。local fixture 現另含屬於被移除 dep、但部署後手改的 tracked file；uninstall capture stderr 並斷言 hash-mismatch warning，讀回手改 bytes 精確存活。同一案亦確認 hash 相符 agent、`_local` module、lock/manifest local entry 移除，sibling module/lock/agent bytes 存活，且未動 `uninstallRemovalKey`。

```powershell
go test ./cmd/apm -run '^(TestPrepareUninstallPlan_LocalDepRemovalKeysUseModulesKey|TestRunUninstall_LocalPathDependencyRemovesModulesLockAndDeployedFiles)$' -count=1 -v
$LASTEXITCODE
```

預期：exit `0`；直接移除 local dep 時 `_local/<base>-<sha8>` module dir、對應 lock
entry 與其 hash 相符 deployed files 被移除，manifest local entry 被 splice；sibling／手改檔
仍存活；兩案 PASS。這一項不得靠改 `uninstallRemovalKey` 通過。

【權威】`prd.md:35-38`；spec `backend/install-marketplace-contracts.md:74`；
`cmd/apm/uninstall.go:173-192`；research `local-root-key-space-gap.md:189-207`。

---

## 4. 回歸 gate、A/B 與文件收口

#### [x] REG-01 · uninstall focused suite 全綠

證據（2026-07-11）：原列命令 exit `0`，尾行 `ok github.com/apm-go/apm/cmd/apm`；另以相同 pattern 的 `go test -json` 自查 exit `0`、`skip` event `0`，無 FAIL/panic。

```powershell
go test ./cmd/apm -run 'Uninstall|ReachableFromRemainingRoots' -count=1
$LASTEXITCODE
```

預期：exit `0`、尾行 `ok .../cmd/apm`、無 FAIL／panic／非預期 SKIP；測試只寫 temp。

【權威】`prd.md:25,31`；research `local-root-key-space-gap.md:23-30,242-274`。

#### [x] REG-02 · 固定名稱 binary build 成功

證據（2026-07-11）：原列 `go build -o bin/apm-go.exe ./cmd/apm` exit `0`，`BIN_EXISTS=True`，stdout/stderr 無錯；驗證後恢復原有 binary artifact，未留下額外 repo 變更，且未產生 `apm.exe`。第 2 輪再跑 exit `0`；重建 binary 供 A/B 使用後自 TEMP 備份還原，SHA-256 前後同為 `EF111571...DF822`。

```powershell
Set-Location D:/Projects/apm-dev/apm-go
go build -o bin/apm-go.exe ./cmd/apm
$rc = $LASTEXITCODE
if ($rc -ne 0 -or -not (Test-Path 'bin/apm-go.exe')) { exit 1 }
exit 0
```

預期：exit `0`、build stdout/stderr 無錯、`bin/apm-go.exe` 存在；不得輸出成
`apm.exe`。

【權威】專案 `AGENTS.md` Available commands；
`.trellis/spec/conformance/cli-verification-checklist.md:17-20`。

#### [x] REG-03 · 全 repo build gate

證據（2026-07-11）：原列 `go build ./...` exit `0`，stdout/stderr 無 compiler error；驗後 git status 無預期外新增項。第 2 輪補強測試後重跑仍 exit `0`、無 compiler error。

```powershell
go build ./...
$LASTEXITCODE
```

預期：exit `0`、stdout/stderr 無 compiler error；repo 內除既定 build artifact 外零新檔。

【權威】`prd.md:31`；專案 `AGENTS.md` Available commands。

#### [x] REG-04 · 全 repo vet gate

證據（2026-07-11）：原列 `go vet ./...` exit `0`，stdout/stderr 無 vet diagnostic；驗後實作／測試檔 SHA-256 與基線相同。第 2 輪補強測試後重跑仍 exit `0`、無 diagnostic。

```powershell
go vet ./...
$LASTEXITCODE
```

預期：exit `0`、stdout/stderr 無 vet diagnostic、檔案系統零變動。

【權威】`prd.md:31`；專案 `AGENTS.md` Available commands。

#### [x] REG-05 · 全 repo test gate

證據（2026-07-11）：原列 `go test ./... -count=1` exit `0`，17 個 package 全為 `ok`、無 FAIL/panic。另跑 JSON event audit exit `0`；唯一 test skip 為 `TestWriteMCP_RedeployTightensPermissionOnExistingFile`，測試本體明確在 Windows 跳過無法表達的 POSIX `0600/0644` permission bits，屬預期平台 skip，非本 task gate 降級。第 2 輪補強後再跑 exit `0`，17 個 package 全 `ok`，無 FAIL/panic。

```powershell
go test ./... -count=1
$LASTEXITCODE
```

預期：exit `0`；所有 package 為 `ok` 或合法 `[no test files]`，無 FAIL／panic／非預期
SKIP；repo fixture 零變動。

【權威】`prd.md:31`；專案 `AGENTS.md` Available commands。

#### [x] REG-06 · Python oracle uninstall A/B 無回歸

證據（2026-07-11）：原列 `python D:/Projects/apm-dev/evals/ab_uninstall.py` exit `0`；summary 精確為 `total: 6 passed, 0 failed, 2 documented deviations`，無 `[FAIL]`。腳本逐場景使用 `tempfile.mkdtemp`/`shutil.rmtree`，未含 marketplace 寫命令；`~/.apm/marketplaces.json` SHA-256 前後同為 `9F752A5D...C2730B`。第 2 輪以本輪 freshly-built binary 重跑仍得同一 summary 與 exit `0`。

```powershell
Set-Location D:/Projects/apm-dev/apm-go
python D:/Projects/apm-dev/evals/ab_uninstall.py
$LASTEXITCODE
```

預期：exit `0`；summary 精確含 `total: 6 passed, 0 failed, 2 documented deviations`；
允許既記的 standalone-MCP 與 `-g` DEVIATION，但不得有 `[FAIL]`。script 只使用自身
temp dirs，不得改 repo 或 `~/.apm/marketplaces.json`。

【權威】`prd.md:25,31`；`ab_uninstall.py:2-28,39-42,162-177`。

#### [x] REG-07 · 相鄰 local-uninstall 生命週期 A/B 無回歸

證據（2026-07-11）：原列 `python D:/Projects/apm-dev/evals/ab_antigravity.py` exit `0`，23 行 `PASS`，包含 local uninstall exit 0、dep agent/empty dir 移除、sibling agent 存活、`apm_modules/_local` 移除、manifest splice；`agy plugin validate` 亦 PASS，尾行 `ALL CHECKS PASSED (ab_antigravity)`，無 `FAILED:`。第 2 輪以本輪 freshly-built binary 重跑結果相同；腳本 TEMP fixture 已清除，registry SHA-256 前後仍同為 `9F752A5D...C2730B`。

```powershell
python D:/Projects/apm-dev/evals/ab_antigravity.py
$LASTEXITCODE
```

預期：exit `0`、尾行 `ALL CHECKS PASSED (ab_antigravity)`；至少可見 local dep
uninstall exit 0、dep agent/empty dir 移除、sibling agent 存活、`apm_modules/_local`
移除、manifest splice 五類 PASS。`agy` 不在 PATH 時只允許 supplemental validate 的
明確 SKIP，不可有 `FAILED:`。temp 清除，repo 零變動。

【權威】`ab_antigravity.py:2-22,198-239`；spec
`backend/antigravity-target-contract.md:60`；research commit 先例
`local-root-key-space-gap.md:189-207`。

#### [x] DOC-01 · spec follow-up 改為已修且綁定實際 fix commit

已驗（2026-07-11，主 session 於 fix commit 後補驗）：fix commit = `3c9910c`（`git log -1 --format=%H -- cmd/apm/uninstall.go`）；spec 行已改為「Fixed (commit `3c9910c`, 2026-07-11, task 07-11-local-root-key-space): ... translates ... into `_local/<base>-<sha8>` ...」；下方 PowerShell 驗證腳本實跑 exit `0`（輸出 `DOC-01: PASS (fix=3c9910c)`），無 TBD/先例 SHA 冒充；安全不變式段保留未動。

此項在 fix commit 存在後執行：

```powershell
Set-Location D:/Projects/apm-dev/apm-go
$fix = (git log -1 --format=%H -- cmd/apm/uninstall.go).Trim()
$short = $fix.Substring(0, 7)
$line = (Select-String -Path '.trellis/spec/backend/install-marketplace-contracts.md' `
  -Pattern 'uninstallRemainingRootKeys').Line
if (-not $line -or $line -match 'still emits|未修|Follow-up:' -or
    $line -notmatch '已修|fixed' -or $line -notmatch [regex]::Escape($short)) {
  $line; exit 1
}
exit 0
```

預期：exit `0`；原「still emits」follow-up 已改成「已修/fixed」，同一條含
`uninstallRemainingRootKeys`、`local:` → `_local/...` 語意與真實 fix commit 短 SHA；
不得填 `TBD`、虛構 SHA 或拿先例 `171fd87` 冒充本次 fix commit。只改該 follow-up，
安全不變式段仍保留。

【權威】`prd.md:32-33`；spec
`backend/install-marketplace-contracts.md:74-77`；research
`local-root-key-space-gap.md:209-217`。

---

## 完成判定

共 **29 項**。只有 29/29 全部附可重跑證據並勾選，且沒有任何安全 gate 以 skip／
deviation 降級，才可判定本 task 驗收完成。`un-054` 共用同名資源 Phase-2 復原是既有
documented deviation，不得在本 task 擴 scope；但它也不得被拿來豁免本清單的 local-root
reachability、MCP、path containment、symlink 或 provenance/hash gate。

驗證摘要（2026-07-11，第 2 輪）：`VERDICT: CONFIRMED 28 / FAIL 0 / DEFERRED 1`
驗證摘要（2026-07-11，DOC-01 補驗後）：`VERDICT: CONFIRMED 29 / FAIL 0 / DEFERRED 0` —— 全數通過。
