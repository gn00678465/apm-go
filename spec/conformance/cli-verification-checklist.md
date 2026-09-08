# CLI 驗證清單 — install / uninstall / marketplace

> **用途**:對 apm-go 的 `install` / `uninstall` / `marketplace` 指令面做**逐 flag、逐子指令**的主動驗收——先系統性檢驗、先修正,不等使用者踩雷才回頭找 bug。
>
> **權威來源**(三方比對,衝突時以此優先序裁定):
> 1. 官方文件:`D:\Projects\apm-dev\apm\docs\src\content\docs\reference\cli\{install,uninstall,marketplace}.md`(= microsoft.github.io/apm 的源,定義承諾的 UX)
> 2. Python 原版(行為 oracle):`D:\Projects\apm-dev\apm\src\apm_cli\`(click 定義 + 實作,file:line 佐證)
> 3. apm-go 現況:`cmd/apm-go/` + `internal/`(file:line 佐證)
> 4. OpenAPM v0.1 條文對映:同目錄 `openapm-v0.1.md` 的 `req-*`
>
> **產出方法**:4 個平行研究 agents(install / uninstall / marketplace-consume / marketplace-author)各自完整閱讀三方來源;標「(研究已實測)」者為研究期間已以真 binary live 驗證過的觀察,其餘為源碼推導、待本清單執行時確證。
>
> **圖例**:`[ ]` 待驗 · `[x]` 已驗(附證據才可勾)。狀態:✅ implemented · ◐ partial · ⚠ divergent · ✗ missing

---

## 0. 執行前置與安全注意(先讀)

- **Build**:`go build -o bin/apm-go.exe ./cmd/apm-go`(binary 永遠叫 `apm-go`)
- **Python oracle**:`uv --project D:\Projects\apm-dev\apm run apm <args>`
- **Exit code**:PowerShell 以 `$LASTEXITCODE` 讀取;每個負向案例都要驗數值,不只驗「非零」
- **隔離**:apm-go 的 marketplace registry 尊重 `$env:APM_CONFIG_DIR`;⚠ **Python 沒有對應機制**(`apm_cli/config.py` 硬編 `~/.apm`)——**任何 Python oracle 的 `marketplace add/remove/update` 都會直接寫真實 `~/.apm/marketplaces.json`**。執行前先備份:`Copy-Item ~\.apm\marketplaces.json ~\.apm\marketplaces.json.bak-checklist`。(本清單研究期間已實際發生一次誤覆寫,已自 `.bak` 還原——教訓已內建於此。)
- **[network]** 前綴 = 該步需要網路。真實套件測試用 `mattpocock/skills`(Claude plugin,`.claude-plugin/plugin.json`,20 skills)。
- 一律在 throwaway scratch 目錄執行,勿在本 repo 根目錄跑 install/uninstall。
- 已知既有偏差(**不在本清單重複追蹤**):target 集合為 apm-go 的 6 個(claude/codex/copilot/antigravity/opencode/agent-skills),非文件的 7 個 —— 見 `openapm-v0.1.md` Target 政策。

---

## 1. apm-go install

### 1.1 狀態總覽(36 項)

| # | item | 狀態 | 一句話 | req |
|---|------|------|--------|-----|
| 1 | `PACKAGE_REF...` | ◐ | owner/repo、#ref、virtual path、相對路徑、NAME@MKT 皆可;**絕對路徑被拒、本地 bundle(.zip/.tar.gz)無支援** | mf-007/008/009/016, rs-008 |
| 2 | `--update` | ✗ | unknown flag(獨立 `update` 子指令存在) | rs-011 |
| 3 | `--frozen` | ⚠ | 有;CI 自動 frozen;**內容完整性檢查超出文件的 structural-only 範圍**(強化) | lk-006/013/015/017/018 |
| 4 | `--dry-run` | ✗ | unknown flag,無預覽模式 | — |
| 5 | `--force` | ⚠ | 只在 `--mcp` 路徑有效;一般 collision-overwrite/安全掃描 bypass 不存在 | — |
| 6 | `--verbose/-v` | ✗ | unknown flag | — |
| 7 | `--target/-t` | ⚠ | 無 `-t`、**不分割逗號**、CLI 值不驗證、零 target 靜默 exit 0(doc: exit 2) | tg-001..004, mf-005 |
| 8 | `--exclude` | ✗ | unknown flag | — |
| 9 | `--only apm\|mcp` | ✗ | unknown flag;**且無任何機制可分離 apm/mcp 部署** | — |
| 10 | `-g/--global` | ✗ | unknown flag;install 完全無 user-scope 概念 | — |
| 11 | `--parallel-downloads` | ✗ | unknown flag;下載本來就是序列 | — |
| 12 | `--refresh` | ✗ | unknown flag;無持久快取層可繞過 | — |
| 13 | `--ssh` | ✗ | unknown flag;無 protocol preference 機制 | — |
| 14 | `--https` | ✗ | 同上 | — |
| 15 | `--allow-protocol-fallback` | ✗ | flag 與 env `APM_ALLOW_PROTOCOL_FALLBACK` 皆為 no-op | — |
| 16 | `--skill NAME` | ◐ | 有(repeatable、持久化正確);**`--skill '*'` reset sentinel 未處理** | pr-001, tg-003 |
| 17 | `--mcp NAME` | ✅ | 獨立宣告+部署路徑,conflict matrix 大致齊 | mf-012 |
| 18 | `--transport` | ✅ | 驗證 + 推斷皆有 | mf-012 |
| 19 | `--url` | ⚠ | **不限制 scheme 為 http(s)**(ftp:// 會過);憑證嵌入有擋 | mf-012/013 |
| 20 | `--env` | ✅ | stdio-only 閘門、KV 解析皆有 | mf-012, sc-007 |
| 21 | `--header` | ✅ | 需 --url;錯誤訊息不回顯 secret | mf-012, sc-007 |
| 22 | `--mcp-version` | ✅ | registry-only 閘門有 | mf-012 |
| 23 | `--registry` | ✅ | 預設 URL 一致;flag > env > default | mf-012 |
| 24 | exit codes | ⚠ | **兩層(0/1)vs Python 三層(0/1/2)**;usage error 全部 1;零 target 靜默 0 | tg-001, pl-002 |
| 25 | `--dev` | ✗ | unknown flag;**devDependencies 解析後被 install 靜默忽略** | — |
| 26 | `--root DIR` | ✗ | unknown flag | — |
| 27 | `--runtime`(legacy) | ✗ | unknown flag | — |
| 28 | `--legacy-skill-paths` | ✗ | flag 與 env 皆 no-op;永遠部署 `.agents/skills/` | tg-003 |
| 29 | `--no-policy` | ✗ | unknown flag;policy 層(Phase 6)整體未實作 | pl-001/002 |
| 30 | `--audit MODE` | ✗ | unknown flag;無 install 時 audit 整合點 | — |
| 31 | `--no-audit` | ✗ | 同上 | — |
| 32 | `--trust-transitive-mcp` | ◐ | 預設不信任(與 Python 同);**無 opt-in 途徑** | — |
| 33 | `--trust-canvas-extensions` | ✗ | canvas 概念整體不存在 | — |
| 34 | `--allow-insecure` | ✗ | **http:// 依賴無條件接受**(Python 預設拒絕)——安全缺口非單純缺 flag | sc-006/008 |
| 35 | `--allow-insecure-host` | ✗ | unknown flag | sc-006 |
| 36 | `--as ALIAS` | ✗ | unknown flag(本地 bundle 路徑整體缺) | — |

### 1.2 已實作路徑 — 逐項驗證程序

#### [ ] IN-01 · PACKAGE_REF 各形式(#1)
```powershell
# 各形式 A/B:每種在乾淨 scratch dir 跑 apm-go 與 oracle,diff apm.yml/apm.lock.yaml 的 repo_url/virtual_path/ref
bin\apm-go.exe install owner/repo#v1.0.0          # shorthand + ref
bin\apm-go.exe install owner/repo/subdir           # virtual 子目錄
bin\apm-go.exe install owner/repo/skill.prompt.md  # virtual 單檔(副檔名分類, req-mf-008)
bin\apm-go.exe install ./relative/pkg              # 相對路徑
# [network] 真實套件:
bin\apm-go.exe install mattpocock/skills           # apm_modules 齊、lockfile repo_url 正確
```
- 預期:上述形式與 oracle 的 identity 欄位逐 byte 相同
- Edge:`owner/repo@v1.0.0`(裸 @ref)兩邊都要拒(Python 給遷移指引;apm-go 是 regex 附帶拒絕——確認訊息可理解);`pkg@mkt#feature/branch`(ref 含 slash)為已知記錄的不一致,驗當前 Python 行為
- **(已實測 2026-07-09)本地目錄 dep 走 git clone**:是 git repo → 離線安裝成功、skills 正確部署;**非 git repo → exit 1 `clone ./dep: exit status 128`**(edge 落定:apm-go 的本地目錄 dep 必須是 git repo;順帶觀察:clone 後 `apm_modules/<dep>/.git/` 一併落盤——是否與 Python 一致待 A/B)
- 負向(已知缺口,見 1.3):絕對路徑、`./bundle.zip`

#### [ ] IN-02 · --frozen(#3)
```powershell
# a. 無 lockfile:
bin\apm-go.exe install --frozen        # 預期 stderr 'frozen install requires a lockfile', exit 1(oracle 同)
# b. CI 自動 frozen(req-lk-018):
$env:CI='1'; bin\apm-go.exe install    # 預期出現 CI-frozen 訊息;測畢 Remove-Item Env:CI
# c. 內容完整性(apm-go 強化,超出 doc 範圍——記為 strengthening 非 bug):
#    正常 install → 手改一個 deployed 檔的 bytes → install --frozen → 預期 path/expected/observed 錯誤
```
- Edge:direct dep 在 apm.yml 但不在 lockfile → 兩邊 exit 1;孤兒 lockfile entry → 兩邊靜默容忍;`--skill` + `--frozen` → apm-go 明確拒絕(install.go:161-164),確認 Python 對應行為

#### [ ] IN-03 · --skill(#16)
```powershell
# [network] 具名選擇 + 持久化:
bin\apm-go.exe install <owner/skill-bundle> --skill review --skill refactor
#   → apm.yml 得 {git:..., skills:[review, refactor]};只部署那兩個 .agents/skills/<n>/
bin\apm-go.exe install --skill foo     # 無 positional → exit 1 明確錯誤(apm-go 比 Python 嚴,OK)
```
- **關鍵負向(P1,見 1.3)**:`--skill '*'` 與不帶 --skill 的部署數比對——若較少即 reset sentinel 壞掉

#### [ ] IN-04 · --mcp 系列(#17–23)
```powershell
bin\apm-go.exe install --mcp fetch -- npx -y @mcp/server-fetch
#   → apm.yml dependencies.mcp: {name, registry:false, transport:stdio, command, args};與 oracle diff 形狀
bin\apm-go.exe install --mcp api --url https://example.com/mcp        # transport 推斷為 http
bin\apm-go.exe install --mcp api --transport bogus --url https://x    # exit 1 'unknown MCP transport'(已實測)
bin\apm-go.exe install --mcp api --url https://user:pass@x/mcp        # exit 1 憑證嵌入拒絕(已實測)
bin\apm-go.exe install --mcp fs --env K=V -- npx foo                  # env 塊正確
bin\apm-go.exe install --mcp fs --env K=V --url https://x             # exit 1 env 需 stdio(已實測)
bin\apm-go.exe install --mcp api --header X=1                         # exit 1 header 需 --url(已實測)
bin\apm-go.exe install --mcp api --url https://x --header "Authorization=Bearer s"  # headers 塊正確;錯誤路徑不回顯值
bin\apm-go.exe install --mcp x --mcp-version 1.2.3 --url https://x    # exit 1 version 僅限 registry(已實測)
bin\apm-go.exe install --mcp foo --registry https://custom -- npx foo # exit 1 registry 僅限 registry-resolved(已實測)
# [network] registry 解析:--mcp io.github.github/github-mcp-server(±--mcp-version、±--registry)與 oracle diff apm.yml
```
- **(已實測 2026-07-09)衝突矩陣批次 14/14 通過**,另含:--mcp+positional、--skill+--mcp、`--mcp -x`(dash 名)、`--transport ''`、`--env BADPAIR`、--force 無 --mcp、--skill 無 positional —— 全部 exit 1、訊息具體
- **修正研究預期**:`--mcp fetch` 單獨使用(無 -- 無 --url)不是 stdio 錯誤,而是走 **registry 查詢**路徑 → `MCP server "fetch" not found in registry`(與 Python 的 registry-resolved 語意一致;該步驟含網路查詢)
- Edge:重複 `--env K=`(後者贏)兩邊一致待驗

#### [ ] IN-05 · --trust-transitive-mcp 預設行為(#32)
- flag 本身:unknown flag, exit 1(已實測 2026-07-09)
- [network] 安裝一個 transitive(depth>1)自帶 self-defined MCP 的套件:apm-go 僅出診斷、**不部署**(與 Python 預設同);記錄「無 opt-in」為缺口

### 1.3 行為差異與安全缺口 — 需決策(修 or 記 deviation)

#### [ ] IN-D1 🔴 http:// 依賴無安全閘門(#34;req-sc-006/008)
```powershell
bin\apm-go.exe install http://example.com/owner/repo.git
# 觀察:無任何 insecure 閘門,直接進入 git clone(缺口);oracle 同指令 → clone 前即以政策訊息拒絕
```
- **(已實測 2026-07-09)**:apm-go 印 `[>] Resolving example.com/owner/repo...` 後直接 clone(失敗於 exit status 128,因 repo 不存在;若 repo 真實存在即會經未加密 HTTP 安裝)。Python 在**任何網路動作前**印 `[x] http://... -- HTTP dependency (unencrypted)` + 補救教學(`allow_insecure: true` 或 `--allow-insecure`)、`All packages failed validation. Nothing to install.` —— 閘門確認存在;惟 **Python 此路徑實測 exit 0**(非研究推測的 1,疑似 Python 自身 quirk,實作時勿照抄其 exit code)
- 另驗 `registries.<n>.url` 用 http:// 是否有 req-sc-006 的 loopback/私網例外閘門(獨立檢查)
- **決策**:實作預設拒絕 + `--allow-insecure`(建議,安全相關不宜省;拒絕時 exit code 建議非零,優於 Python 現行行為)

#### [ ] IN-D2 🔴 --target 四重差異(#7)
```powershell
bin\apm-go.exe install -t claude <pkg>                 # 'unknown shorthand flag: t' → 缺 -t(已實測 exit 1)
bin\apm-go.exe install --target claude,cursor <pkg>    # 整串當單一 target → 'no registered handler for "claude,cursor"'
bin\apm-go.exe install --target garbage <pkg>          # 不經 vocabulary 驗證,靜默 0 target
bin\apm-go.exe install <pkg>   # 無任何 target 訊號的乾淨目錄 → $LASTEXITCODE 應為 0(doc/oracle: 2 + teaching message)
```
- Edge:`--target all` 集合 = apm-go 5 個 auto-detectable vs Python 8 個;`agent-skills` 兩邊都必須 explicit-only
- **決策**:至少補逗號分割 + `-t` + 未知值診斷;exit 2 語意併入 IN-D6

#### [ ] IN-D3 🟡 絕對路徑與本地 bundle(#1 殘項)
```powershell
bin\apm-go.exe install C:\abs\path\pkg      # 'path is absolute; only relative paths are allowed'(oracle 接受)
# oracle: apm pack --format plugin --archive 產 bundle 後
bin\apm-go.exe install ./bundle.zip          # 被當 git 來源 clone → 失敗(oracle:unpack+deploy 成功且不動 apm.yml)
```
- 注意:此缺口與 3.x 的 local marketplace 完全不可用(MK-D1)同根因(`depref.go:58-60` 拒絕絕對路徑)

#### [ ] IN-D4 🟡 devDependencies 靜默忽略(#25)
```powershell
# 手寫 devDependencies: {apm: [owner/repo]} 進 apm.yml → bin\apm-go.exe install
# 觀察:不下載、不部署、無警告(oracle 會安裝)。uninstall 卻掃 dev(不對稱!)
```
- **決策**:實作 dev 安裝,或至少警告「devDependencies 尚未支援」

#### [ ] IN-D5 🟡 --skill '*' sentinel(#16)
```powershell
bin\apm-go.exe install <skill-bundle> --skill '*'   # 若部署數 < 全量 → 過濾了字面 '*',sentinel 壞
```

#### [ ] IN-D6 🟡 exit code contract(#24)
```powershell
bin\apm-go.exe install --bogus-flag                 ; $LASTEXITCODE   # 1(oracle: 2)
bin\apm-go.exe install --mcp f --url https://x -- npx bar ; $LASTEXITCODE  # 1(oracle: 2, UsageError)
# 零 target 靜默 0 → 見 IN-D2
```
- **決策**:是否引入三層 exit code(usage=2)。若不修,在 apm-go 文件記 deviation

#### [ ] IN-D7 🟢 --force 範圍(#5)、--url scheme(#19)、--frozen 強化(#3)
```powershell
bin\apm-go.exe install owner/repo --force            # exit 1 '...require --mcp'(已實測;oracle:全域 flag)
bin\apm-go.exe install --mcp a --url ftp://x --transport http  # 已實測:exit 0,ftp:// 條目持久化進 apm.yml(oracle 拒非 http/https scheme)——差異確認
```
- --frozen 內容檢查超出 doc:記 strengthening(不修)

### 1.4 缺失 flag 負向矩陣(逐一執行,確認乾淨拒絕、無副作用)

每列:`bin\apm-go.exe install <flag> [arg]` → 預期 stderr `unknown flag`(或 `unknown shorthand flag`),exit 1,**零檔案副作用**。oracle 欄為 Python 行為摘要(供 scope 決策)。

> **(已實測 2026-07-09)**:下表全部列 + `--trust-transitive-mcp` + `--allow-insecure` 共 24 個 probe 批次執行——全數 unknown flag、exit 1、apm.yml md5 不變、無新檔。✓ 勾選僅代表「負向檢查通過」;scope 決策仍懸(見第 5 節)。

| ✓ | flag | oracle 行為 | scope 決策建議 |
|---|------|------------|----------------|
| [x] | `--update` | 重解析至最新 ref(deprecated) | 已有 `update` 子指令 → 記 deviation |
| [x] | `--dry-run` | 印 install plan,零寫入 | 建議實作(uninstall 已有,對稱性) |
| [x] | `--verbose` / `-v` | 逐檔路徑+完整錯誤 context | 建議實作(低成本高價值) |
| [x] | `--exclude VALUE` | 跳過單一 runtime(含 MCP 部署) | 隨 --target 修補一併決策 |
| [x] | `--only apm\|mcp` | 選擇性安裝 | 真實缺口(兩者必同裝)→ 待決策 |
| [x] | `-g` / `--global` | user scope `~/.apm/` | 與 uninstall -g(定案 A)同批決策 |
| [x] | `--parallel-downloads N` | 併發下載(預設 4) | 效能項,可延後 |
| [x] | `--refresh` | 繞過持久快取 | apm-go 無快取層 → 記 deviation |
| [x] | `--ssh` / `--https` | shorthand 傳輸偏好(互斥, exit 2) | 待決策 |
| [x] | `--allow-protocol-fallback` | 恢復寬鬆 fallback 鏈 | 同上批 |
| [x] | `--dev` | 寫入 devDependencies | 見 IN-D4 |
| [x] | `--root DIR` | 寫入重導向 | 可延後 |
| [x] | `--runtime VALUE` | --target 的 legacy 別名 | 記 deviation 即可 |
| [x] | `--legacy-skill-paths` | per-client skill 路徑 | 記 deviation(collapsed 路徑為 spec 方向) |
| [x] | `--no-policy` | 跳過 org policy | policy 層(Phase 6)未實作 → 隨 Phase 6 |
| [x] | `--audit` / `--no-audit` | install 時內容 audit | 隨 audit 整合決策 |
| [x] | `--trust-canvas-extensions` | canvas 部署 | 整體 feature 不存在 → 記 out-of-scope |
| [x] | `--allow-insecure-host H` | transitive http 白名單 | 隨 IN-D1 一併 |
| [x] | `--as ALIAS` | 本地 bundle 顯示名 | 隨 IN-D3 bundle 支援一併 |

- [x] env var 惰性(已實測 2026-07-09):本地 git-dir dep 安裝場景下,`APM_LEGACY_SKILL_PATHS=1` + `APM_ALLOW_PROTOCOL_FALLBACK=1` 行為無變化——skills 仍只部署 converged `.agents/skills/`(+claude adapter 的 `.claude/skills/`),零 legacy per-client 路徑檔案。

---

## 2. apm-go uninstall(8 項)

> 本指令已有任務級深度清單(`.trellis/tasks/07-05-uninstall/uninstall-checklist.md`,40+ un-0xx 項)。本節為釋出級複驗面;已知接受的限制:**un-054**(共用部署檔 Phase-2 復原,刻意不做)、**un-090/091**(-g 明確拒絕,定案 A)。
>
> **(已實測 2026-07-09,本地批次)**:零 args → exit 1(C8 落定);not-found → exit 0 + 'No packages found';無 apm.yml → exit 1;`-g` 空目錄 → exit 1 'not supported yet'(先於 manifest 讀取,UN-04 核心);flow 部分移除 → `apm: [acme/bar]` 保持 flow;flow 清空 → `apm: []`;block 部分移除 → 註解(`# top comment`、行內 `# keep me`)逐字存活;--dry-run → md5 零差異 + '[dry-run] no changes made'(UN-02/07 本地面全過)。

#### [ ] UN-01 · PACKAGES... 各形式與 identity 解析 · ✅
```powershell
# 多套件一次移除;owner/repo 與 HTTPS URL 解析到同一 entry;virtual-path 精準匹配
bin\apm-go.exe uninstall acme/foo acme/bar
bin\apm-go.exe uninstall https://github.com/owner/repo   # 與 owner/repo 同 identity
# [network] 完整 A/B:install→uninstall mattpocock/skills,兩邊終態樹 diff
```
- Edge:devDependencies-only 套件(#1549 回歸)、同套件傳兩次、marketplace ref 帶 #ref(識別時忽略)、owner/repo 與 name@mkt 混用

#### [ ] UN-02 · --dry-run · ✅
- 前後快照 apm.yml/apm.lock.yaml/apm_modules/target 檔 hash → **零差異**,exit 0
- stdout 含 `[dry-run]`、無 `[+] Removed`
- marketplace ref 無 lockfile anchor → 明確「cannot preview」警告,仍 exit 0
- Edge:與 -v 併用;無 lockfile 時 orphan 段不印不炸;全部 not-found 仍 exit 0

#### [ ] UN-03 · --verbose / -v · ✅
- `-v` 出現逐檔 `[-] <path>`、逐 dir `[-] apm_modules/<key>`;無 -v 只有 summary;`--verbose` 與 `-v` 輸出相同
- Edge:hash-mismatch 保留檔的診斷**不受** -v 控制(無條件印 stderr)

#### [ ] UN-04 · -g / --global · ⚠(定案 A:明確拒絕)
```powershell
bin\apm-go.exe uninstall -g acme/foo   # 任何目錄:exit ≠0, 'user scope ... not supported yet'
# 空目錄(無 apm.yml)也要出同樣錯誤(證明 -g 檢查先於 manifest 讀取)
```
- Edge:-g + --dry-run 仍須立即拒;**絕不可**靜默當 project-scope 執行

#### [ ] UN-05 · MCP standalone 移除(apm-go 增強;Python 做不到)· ⚠
```powershell
bin\apm-go.exe install --mcp foo -- npx x  →  bin\apm-go.exe uninstall foo
# apm.yml dependencies.mcp 無 foo;target MCP config(.mcp.json 等)無 foo;lockfile mcp_servers 更新
# oracle 同序列 → 'not found in apm.yml'、零變更(證明 apm-go 為 additive 增強)
```
- Edge:同名 apm-package 與 MCP server 並存 → apm identity 優先;devDependencies.mcp 也要能掃到

#### [ ] UN-06 · 逐 target 反向清理 · ◐(un-054 已知限制)
- [network] 完整回合:install(20 skills)→ uninstall → deployed_files 全消、apm_modules 消、lockfile 消/空、空父目錄修剪
- 手寫檔存活:target 目錄裡非 deployed_files 記錄的檔案不可動
- **un-054 重現(記錄用)**:兩套件共用同一部署路徑 → 移除其一,共用檔**會**被刪(Python Phase-2 會復原)——預期行為=已接受限制,寫明於報告
- Edge:hash-mismatch 檔保留+診斷;lockfile 路徑逃逸 entry 拒絕(Contained guard);apm_modules 已被手刪 → no-op;hooks 從 .claude/settings.json 拼接移除而非刪整檔

#### [ ] UN-07 · flow vs block YAML(近期 bug 區)· ✅
```powershell
# block:byte-identical(除被移除行)
# flow 單項:apm: [pkg] → apm: [](不炸 'unexpected document shape')
# flow 部分:apm: [a, b] → 移 a 後 b 存活、仍 flow
go test ./internal/manifest/... -run TestRemovePackagesFromManifest -v
```
- Edge:多行 flow(`apm: [\n  a,\n  b\n]`);同檔 apm flow + dev block 混合;dev flow 清空時不刪非 apm 的 devDependencies 兄弟鍵(req-cf-001 鬆對映)

#### [ ] UN-08 · exit codes / error surface · ◐
```powershell
bin\apm-go.exe uninstall                      ; $LASTEXITCODE  # cobra usage error(oracle: click 2)→ 記錄實際值
bin\apm-go.exe uninstall acme/nope            # exit 0 + stderr not-found 警告, apm.yml 不動
bin\apm-go.exe uninstall acme/foo   # 無 apm.yml 的目錄:exit 1 'apm.yml not found'
# 部分部署狀態:手刪 apm_modules 後 uninstall 仍成功(SafeRemove no-op)
```
- Edge:損毀 apm.yml / 損毀 lockfile → 各自明確 parse error exit 1;中斷後重跑不炸(已知窄縫:manifest 已寫、lockfile 未寫 → 該 key 的 apm_modules 清理丟失,記錄之)

---

## 3. apm-go marketplace — consume(13 項)

> ⚠ 本節所有 oracle 步驟先做第 0 節的 marketplaces.json 備份。apm-go 側一律 `$env:APM_CONFIG_DIR=<scratch>`。

#### [ ] MK-01 · add SOURCE 各形式 · ✅
```powershell
# 本地 fixture(免網路):<tmp>\mkt\marketplace.json = {"name":"m","plugins":[{"name":"p","source":"./x"}]}
bin\apm-go.exe marketplace add <tmp>\mkt --name fixture     # '[+] Added ... (kind: local)'(研究已實測)
bin\apm-go.exe marketplace add justonesegment               # exit 1 'not a recognized SOURCE shape'(研究已實測)
bin\apm-go.exe marketplace add http://example.com/o/r       # exit 1 'does not support http://'(研究已實測)
bin\apm-go.exe marketplace add file:///<tmp>/mkt --name ff  # kind: local
# [network] owner/repo、HOST/OWNER/REPO、https URL(±#ref)、SCP git@host:o/r.git、hosted marketplace.json URL
```
- Edge:控制字元拒絕;percent-encoded traversal 拒絕;非 FQDN 首段當 owner;`https://github.com/repo`(單段)拒 vs 泛 git host 單段可;`./local#weird` 不做 fragment 切割
- 兩邊 entry 存進 registry 的 url 字串形式 diff(已知可能不同)

#### [ ] MK-02 · add --name · ⚠(缺口已確認)
- 明確 --name 永遠贏;manifest name 無效(regex `^[a-zA-Z0-9._-]+$` 外)→ 警告+fallback
- **缺口(已實測 2026-07-09)**:`--name 'bad name!'` → apm-go **接受、exit 0**、原樣入 registry(Python exit 1 拒絕)。含空白/`!` 的名稱會破壞 `plugin@marketplace` 語法 → 建議補 alias 驗證

#### [ ] MK-03 · add --ref · ✅
- `--ref my-branch` 存入 registry;`https://...#v1 --ref v2` → exit 1 fragment 衝突(研究已實測);無 ref 預設 `main`
- Edge:--ref 給非 git 來源(url/local)→ 靜默忽略不炸;非 https SOURCE 的 `#` 不切割

#### [ ] MK-04 · add --branch(deprecated 別名)· ✅
- 隱藏於 --help(研究已實測);單獨用=--ref;與 --ref 併用 exit 1 —— **錯誤文字是 cobra 通用模板 vs Python 自訂句**(cosmetic 差異,記錄)
- Edge:`--branch ''` 顯式空值的 Changed() 判定與 Python `is not None` 對齊與否

#### [ ] MK-05 · add --host · ✅(含一個潛在缺口)
- shorthand 無內嵌 host → 套用;URL host 衝突 → exit 1 'conflicts with'(研究已實測);local/hosted-json → warn+忽略
- **潛在缺口**:`--host 'not a valid host!!'` —— Python 前置 FQDN 驗證 exit 1;apm-go 疑似無 → 實測
- Edge:大小寫不敏感一致 host 不警告

#### [ ] MK-06 · list · ⚠
- SOURCE 欄:apm-go 印 raw URL/路徑 vs Python compact display_source(github→owner/repo、local→~縮短)(研究已實測)
- `--verbose` 加 HOST 欄(Python 任何 verbosity 都沒有)
- 空 registry → 友善訊息 exit 0
- **決策**:對齊 display_source 或記 deviation

#### [ ] MK-07 · browse NAME · ⚠
- box table 與 Python rich 輸出逐字比對;`--verbose` 多一行 `N plugin(s) in NAME`(Python rich 路徑永不印)——記 deviation 或移除 gate
- 未註冊 NAME → exit 1 + apm-go 獨有 'Registered:'/'Did you mean' 提示(增強,記錄);0 plugins → `[!] ... has no plugins` exit 0
- Edge:長 description 換行對齊;無 description/version 用 `--`;NAME 大小寫不敏感

#### [ ] MK-08 · update [NAME] · ◐
- 具名失敗 fatal exit 1;無名迴圈逐一 refresh、單一失敗僅 stderr 警告、整體 exit 0
- **缺口**:無 `--verbose/-v`(doc 承諾每個子指令都有)(研究已實測)
- Edge:空 registry 無名 → 訊息 exit 0;NAME 大小寫不敏感

#### [ ] MK-09 · remove NAME · ◐(⚠ isInteractive 通道相依,已定性)
- `--yes` 立即移除(已實測 exit 0);重複移除 → exit 1 not registered(已實測)
- **通道相依行為(2026-07-09 雙通道實測定性)**:
  - PowerShell(pwsh -NonInteractive):防護正常 → `requires -y/--yes in a non-interactive environment`,exit 1,entry 保留 ✓
  - git-bash pipe:isInteractive 誤判為互動 → 印 prompt → stdin EOF → `Aborted.`、**exit 0 且未刪** —— CI/pipe 腳本誤判風險(exit 0 ≠ removed)
  - 建議修法:confirm prompt 讀到 EOF/讀取錯誤時視同「需要 --yes」→ exit 1(而非 Aborted exit 0);`package remove` 同病同修(見 C10)
- **缺口**:無 `--verbose/-v`

#### [ ] MK-10 · validate NAME · ◐
- per-finding 行 + `Summary: N passed, N warnings, N errors`、errors>0 → exit 1(研究已實測 exit 0 案例)
- 計數法:apm-go `total=1+len(plugins)` 為近似 vs Python per-check 計數 → 用重複名/空 name fixture 比對 N 值
- **缺口**:無 `--verbose`;無 `--check-refs`(Python 隱藏 no-op flag —— 刻意省略,已記錄,驗 unknown flag 即可)
- Edge:缺 source 的 plugin → error exit 1;fetch 失敗 → Summary 之前就 exit 1

#### [ ] MK-11 · install PLUGIN@MKT 握手 · ⚠(P1 缺口)
```powershell
# 🔴 local-kind marketplace:
bin\apm-go.exe install hello-skill@fixture
# → exit 1 'resolved to a local filesystem path ... no apm.yml dependency-string form'(研究已實測)
# oracle 同場景 → 成功寫入絕對路徑 string dep ——— apm-go 對 local marketplace 完全不可用
# [network] github-kind marketplace 則走完整 parity:persisted string + lockfile 與 oracle diff
bin\apm-go.exe install plugin@unregistered   # exit 1 'is not registered'
bin\apm-go.exe install nope@fixture          # exit 1 ErrPluginNotFound → 指向 browse
bin\apm-go.exe install plugin@mkt#^1.0.0     # exit 1 semver-range-in-ref 拒絕(兩邊同)
```
- Edge:同名 plugin 在兩個 marketplace → shadow 警告不中止;ref 變更 → ref-swap 警告不中止;dict-source plugin 不受 local-path bug 影響
- **決策**:修 depref 絕對路徑限制(與 IN-D3 同根因)或記「local marketplace 不支援」

#### [ ] MK-12 · exit codes / registry 韌性 · ✅(含一個差異)
- 全子指令:成功 0 / 失敗 1;重複 add 同名 → **靜默取代**、exit 0(已實測,且立即生效——後續 browse/validate 讀到新來源)
- **差異(已實測)**:損毀 marketplaces.json → apm-go `parse marketplace registry: invalid character...` exit 1 vs Python 警告+空 registry fallback → 決策:對齊或記 deviation
- Edge:同名不同 kind 取代亦靜默(已實測:local→local 換路徑)

#### [ ] MK-13 · build(tombstone)· ✅
- `marketplace build` → exit 1 指向 `pack`(研究已實測);任意尾隨 args 同;不出現在 `marketplace --help` 清單
- 措辭比 Python 少第二句(cosmetic)

> 範圍外(刻意不建,勿當缺口):`marketplace search`(頂層 `apm search`)、`doctor`、`publish`、`browse --json`。

---

## 4. apm-go marketplace — authoring + package(18 項)

> **(已實測 2026-07-09,本地批次)** MA-01…06/08 的 apm-go 本地面全過:init fresh(--name/--owner 三處生效)/ re-run exit 1 且 md5 不變 / --force 保留手寫註解 / 既有 apm.yml 時 --name 被忽略 / gitignore 警告 ± --no-gitignore-check / migrate dry-run 零觸碰 → migrate 成功刪 legacy → re-run exit 1。package 系列:零 flag local add exit 0(mkt-046 修正)/ dup exit 2 / version+ref 互斥 exit 2 / `-v` unknown flag exit 1 / set 零 flag no-op exit 0(C2)/ set --version 清 ref / set not-found exit 2 / remove --yes exit 0 / re-remove exit 2 / no-config guard exit 2(C7)。**oracle A/B 面與 [network] 面未跑**。

#### [ ] MA-01 · init(無 flag)· ✅
- 空目錄:產 apm.yml(name/version/description + marketplace: 塊,owner 預設 acme-org、tagPattern v{version}、example-package)
- 重跑無 --force → exit 1 'Use --force',apm.yml byte 不變
- 與 oracle diff:結構同;已知刻意差異=範例 `ref: v1.0.0` vs Python `ref: main`
- Edge:top-level 非 mapping 拒絕;`marketplace:` 顯式 null 視為不存在(不需 --force);CRLF 不混行尾

#### [ ] MA-02 · init --force · ✅
- 塊被替換(新 owner 生效);**apm.yml 其他行(含手寫註解)逐字存活**(apm-go 外科拼接強於 Python 全檔重編——兩邊都要驗註解存活)
- Edge:--force + --name(既有 apm.yml)→ name 仍被忽略;`marketplace:` 是 scalar → 明確錯誤不毀檔

#### [ ] MA-03 · init --name · ✅
- 只在 apm.yml **新建**時生效;既有 apm.yml → 靜默忽略(研究已實測)
- Edge:`--name ''` → fallback 預設;YAML 特殊字元(`:`、`#`)→ 產出仍可 parse

#### [ ] MA-04 · init --owner · ✅
- owner.name / owner.url(github.com/{owner})/ packages[0].source 三處生效;省略→acme-org
- Edge:owner 含 `/` → 兩邊都不驗、逐字寫入(記錄)

#### [ ] MA-05 · init --no-gitignore-check · ✅(文件有、原清單漏列)
- .gitignore 有精準行 `marketplace.json` → init 出警告;帶 flag → 無警告;永不影響 exit code
- Edge:行尾空白/註解不匹配(精準行比對);`*.json` 也在 pattern 集要警告;無 .gitignore no-op

#### [ ] MA-06 · migrate(預設)· ✅
- marketplace.yml → apm.yml 的 marketplace: 塊(**註解保留**),marketplace.yml 刪除,exit 0
- 無 marketplace.yml → exit 1 'nothing to migrate';無 apm.yml → exit 1 'run apm init first'
- Edge:legacy 檔 schema 驗證失敗(如 source 含 `../`)→ 動 apm.yml 前就拒

#### [ ] MA-07 · migrate --force(= --yes/-y)· ✅
- 三種拼法各自驗(等效);無 force 且塊已存在 → exit 1 不動檔
- Edge:apm.yml(含塊)+ marketplace.yml **並存** → mkt-047 互斥錯誤優先於 force 分支(兩邊優先序一致)

#### [ ] MA-08 · migrate --dry-run · ✅
- stdout 出 unified diff 預覽;apm.yml 與 marketplace.yml **零變動**;exit 0
- Edge:--dry-run 無 --force 且塊已存在 → 仍要出既有塊錯誤(不得靜默預覽)

#### [ ] MA-09 · check(預設)· ◐
- [network] 全部可解析 → `[+] all N package(s) verified` exit 0;壞 ref → `[x] <name>` exit 1
- local(./)source 免網路直接 pass
- **缺口 a**:無重複套件名警告(Python 有,warning-only)——fixture 驗差異
- **缺口 b**:無 per-package `host:` / `marketplace.sourceBase` 支援,一律解析 github.com —— 非預設 host 來源會錯誤解析,較大 feature gap,記錄待 scope
- Edge:legacy marketplace.yml → 讀取 + `[warn] reading legacy...` 提示

#### [ ] MA-10 · check --offline · ✅(刻意 MVP 簡化)
- 有 pin(ref/version)的 remote 套件 → 一律失敗 'no cached refs'(apm-go 無 ref 快取;Python 有快取語意、錯誤文字不同)(研究已實測)
- 全 local 或無 pin → exit 0
- 差異:Python 有 'Offline mode' banner,apm-go 無(cosmetic)

#### [ ] MA-11 · audit NAME · ✅
- [network] 註冊真 marketplace → audit:per-plugin clean/bypass/skipped/unverifiable 分類 + Summary 行;未註冊 → exit 1 + Registered 提示(研究已實測)
- Edge:NO_MANIFEST vs UNSUPPORTED_SOURCE vs fetch-error 三桶分明(僅最後者計入 --strict);unpinned ref → fallback HEAD

#### [ ] MA-12 · audit --strict · ✅
- bypass 或 unverifiable > 0 → exit 1;無 --strict 同 findings 僅警告 exit 0
- Edge:只有 skipped → 仍 exit 0;零 plugins → exit 0

#### [ ] MA-13 · audit -v · ✅
- -v 加印 clean(`[+] ... marketplace-resolved`)與 skipped(`[i] ... skipped (...)`)行;bypass/unverifiable 無條件印
- Edge:-v 不改 exit code 決策

#### [ ] MA-14 · package add SOURCE · ⚠(兩個 P1 缺口 + 一個刻意修正)
```powershell
bin\apm-go.exe marketplace package add ./bare-pkg
#   零 flag local 成功 —— 刻意修正 Python 的 mkt-046 bug(Python 要求 version/ref 之一, exit 2)= 預期偏差
bin\apm-go.exe marketplace package add ./x --ref main --no-verify
#   🔴 讀 apm.yml:ref: main 逐字寫入 —— 未解析 mutable ref → SHA(doc 明文承諾 auto-resolve)(研究已實測)
bin\apm-go.exe marketplace package add ./y --subdir "../../etc" --no-verify
#   🔴 被接受逐字寫入 —— 無 path-traversal 驗證(Python exit 2 'Invalid subdir')(研究已實測)
bin\apm-go.exe marketplace package add ./dup --no-verify --version 1.0.0   # 第二次 → exit 2 already exists(大小寫不敏感)
bin\apm-go.exe marketplace package add ./b --version 1.0.0 --ref abc --no-verify  # exit 2 互斥
# [network] --tag-pattern 'v{version}' --tags a,b --include-prerelease 全欄位寫入
```
- Edge:SOURCE 本身含 `..`(req-mf-017)兩邊都拒;--tags 空段修剪;字面 40-hex SHA 不驗存在(兩邊同)

#### [ ] MA-15 · package set NAME · ⚠
- 只改傳入欄位;version/ref 互斥且互清;NAME 不存在 → exit 2;大小寫不敏感
- **缺口 1**:零 flag → apm-go 靜默 no-op exit 0(Python exit 1 'No fields specified')(研究已實測)
- **缺口 2**:--ref 同樣不解析 SHA(set 在 Python **必**連網解析,比 add 更嚴)
- **缺口 3**:--subdir 同 MA-14 無驗證
- Edge:`--tags ''` → apm-go 清空既有 tags vs Python 不動(特定差異,實測);`--include-prerelease` tri-state 不被零 flag 呼叫重置

#### [ ] MA-16 · package remove NAME(--yes)· ✅
- --yes 立即移除;非互動無 --yes → **exit 1**(mkt-045 記錄的唯一例外碼);not found → exit 2;互動拒絕 → 'Aborted.' exit 0 不刪
- Edge:移除最後一個 → packages 成空序列仍為合法 YAML;-y/--yes 併用無錯

#### [ ] MA-17 · package add/set/remove 缺 -v/--verbose · ✗
- 三個子指令 `-v` 與 `--verbose` 都是 unknown flag exit 1(研究已實測);Python 全部接受;doc 全域承諾
- 一致性問題:同 CLI 內 init/migrate/check/audit 可 -v、package 系列不行

#### [ ] MA-18 · exit codes(authoring 面)· ⚠
- init 既有塊 / check 失敗 / audit --strict → exit 1(與 Python 同)
- package 自身編輯錯(重複、not-found、互斥)→ exit 2(與 Python 同)
- **差異**:'no marketplace config found' 與 'apm.yml+marketplace.yml 並存' 兩個 guard —— apm-go exit 2 vs Python exit 1(研究已實測)→ 決策:接受簡化或對齊
- Edge:損毀 apm.yml → 兩邊 parse error 不靜默成功;apm.yml 存在但無 marketplace: 鍵 → 同 'no config found' 路徑

---

## 5. 缺陷與待決策彙總(依嚴重度)

### P0 — 安全(✅ 已修復並驗證)

| ✓ | 項 | 位置 | 內容 | commit |
|---|----|------|------|--------|
| [x] | S1 | install(IN-D1) | 新增 `--allow-insecure`;預設拒絕所有 http:// git 依賴(含 CLI positional + apm.yml + devDeps),clone 前 fail-closed,flag-only 對齊 Python。錯誤不洩漏憑證 | `f4bdcac` |
| [x] | S2 | package add/set(MA-14/15) | `validateSubdir` 拒絕 `.`/`..` 段與絕對路徑,add/set 兩路徑皆驗,exit 2 | `f8d70f3` |

### P1 — 功能斷裂 / 文件承諾違反(✅ 已修復並驗證)

| ✓ | 項 | 位置 | 內容 | commit |
|---|----|------|------|--------|
| [x] | F1 | install↔marketplace(MK-11, IN-D3) | depref 接受絕對本地路徑;persist 相對(樹內)/絕對(樹外);loader 改 copy 進 `apm_modules/_local/<name>/`(對齊 Python、跳 symlink、ContainedKey 守衛);絕對 local bundle 一併解決。**真 binary in-tree/out-tree + round-trip 已驗** | `07b443d` |
| [x] | F2 | install(IN-D2/D6) | `-t` 簡寫、逗號分割、`ValidateTarget` 驗證、零 target fail-closed exit 2 + 診斷 | `a1a07f4` |
| [x] | F3 | install(IN-D4) | `collectResolutionRootDeps`(真 root 納 dev,transitive 不外溢);install/update/deploy/frozen 全面納 dev;pack dev 排除為既有獨立缺口 | `9cc6d51` |
| [x] | F4 | package add/set(MA-14/15) | mutable ref 經 RefLister 解析為 SHA;40-hex 原樣;空 ref 保 mkt-046 | `e079599` |
| [x] | F5 | install(IN-D5) | `--skill '*'` reset(deploy + persist 兩層);清除既有 subset | `13f1290` |

> **注**:F4 已知限制 — `--ref HEAD` 無法解析(RefLister 走 --tags --heads,無 HEAD 條目),分支/tag 正常;列為 follow-up。

### P2 — 一致性 / UX(✅ 真 bug 已修 / 📄 deviation 已記)

| ✓ | 項 | 位置 | 狀態 |
|---|----|------|------|
| [x] | C1 | marketplace ×6 | ✅ `e43baf9`:update/remove/validate + package×3 補 --verbose/-v |
| [x] | C2 | package set | ✅ `e43baf9`:零 flag → exit 1「No fields specified」;live 驗 |
| [ ] | C3 | registry | 📄 deviation:損毀 marketplaces.json apm-go fail-fast(較安全)vs Python 靜默空 fallback;保留並記 conformance statement |
| [ ] | C4 | list/browse | 📄 deviation:SOURCE 欄 raw、--verbose HOST 欄、browse -v summary 行(cosmetic) |
| [x] | C5 | add | ✅ `e43baf9`:--name(alias)/--host(FQDN)驗證非法 exit 1;live 驗 |
| [x] | C6 | check | ✅ `e718d8f`:duplicate-name 警告(非阻擋);host:/sourceBase 屬更大 feature,另 scope |
| [ ] | C7 | package guard | 📄 deviation:no-config/both-config exit 2 vs Python 1(uniform exit 2 可接受) |
| [ ] | C8 | uninstall | 📄 deviation:零 args exit 1(cobra)vs 2(click) |
| [x] | C9 | install --mcp --url | ✅ `6d2232b`:限制 scheme 為 http/https(原接受 ftp://);live 驗 |
| [x] | C10 | remove(MK-09, MA-16) | ✅ `e43baf9`:非互動/EOF confirm 視同需 --yes → exit 非零不移除(修 CI footgun),兩 remove 路徑同修;git-bash pipe live 驗:exit 1 且 entry 未刪 |
| [ ] | — | add --branch | 📄 deviation:互斥訊息 cobra 模板 vs Python 自訂句(cosmetic) |

### Scope 決策 — 大宗 missing flags(見 1.4 矩陣)
- [ ] 對 1.4 的 19 個 flag 逐一標記:實作 / 記 deviation / out-of-scope,寫入 conformance statement(`openapm-v0.1.md` 範本的 documented deviations 段)

### 既有已接受限制(勿重複開單)
- un-054 共用部署檔 Phase-2 復原(uninstall)· un-090/091 -g 明確拒絕 · --check-refs 省略 · marketplace build tombstone 措辭 · `ref: v1.0.0` init 範本差異 · package add 零 flag local 成功(mkt-046 的刻意修正)· --frozen 內容檢查強化 · MCP standalone uninstall(增強)· diamond-orphan reachability 修正(增強,Python 自身有此 bug)

---

## 6. OpenAPM req-* 對映(本清單引用之條文)

| req | 對應項 |
|-----|--------|
| req-mf-005 / tg-001..004 | IN-D2(--target 詞彙/偵測/部署/診斷) |
| req-mf-007/008/009/016 | IN-01(PACKAGE_REF 解析與分類) |
| req-mf-012 | IN-04(--mcp 系列驗證) |
| req-mf-013 | IN-04 / IN-D7(placeholder URL) |
| req-mf-017 | MA-14(package source 驗證) |
| req-rs-008 | IN-01 / MK-11(五類 reference kind 分類) |
| req-rs-011 | 1.4 --update(→ `update` 子指令承載) |
| req-lk-006/013/015/017/018 | IN-02(--frozen 家族) |
| req-pr-001 / tg-003 | IN-03(--skill 子集持久化與部署) |
| req-sc-006/008 | IN-D1 / S1(insecure http) |
| req-sc-007 | IN-04(--env/--header 不洩漏 secret) |
| req-pl-001/002 | 1.4 --no-policy(Phase 6 未實作) |
| req-cf-001 | UN-07(YAML 序列風格,鬆對映) |

---

_覆蓋:install 36 + uninstall 8 + marketplace-consume 13 + marketplace-authoring 18 = **75/75** 研究項全數納入。產出自 4-agent 三方比對 workflow(2026-07-09);任何與源碼演進的衝突以重新執行驗證步驟為準。_

---

## 7. antigravity target(2026-07-10,task 07-05-antigravity-research)

> 驗證環境:agy 1.0.16(`C:\Users\gn006\AppData\Local\agy\bin\apm... agy.exe`)、
> binary `bin/apm-go.exe`(commits d72dc6a/c6ef3f7/3471e45 後)。
> live fixture:scratchpad `ab-fixture2/proj`(含 local path dep `./dep-pkg` 提供 agents primitive)。
> ⚠ agy 實機驗證需 `--new-project`/`--project` 註冊 workspace,否則 `.agents/` 不掃描(ag-28)。
> 產生的 probe project 項目驗畢須自 `~/.gemini/config/projects/` 清除。

### 7.1 Target 選取 — explicit-only(10 項)

- [x] ag-01 `--target antigravity` 顯式部署生效。驗:live `install --target antigravity` Targets 行;unit `TestResolveTargets_AntigravityExplicitSelection/flag_antigravity`【證據】live Targets: antigravity(source: --target)exit 0 + unit PASS
- [x] ag-02 `--target agy` alias 生效。驗:live 全量部署;unit `.../flag_agy_alias`【證據】live 全量部署 exit 0(ab-fixture2)+ unit PASS
- [x] ag-03 apm.yml `target: [antigravity]` 生效。驗:live Targets 行(source: manifest);unit `.../manifest_target_antigravity`【證據】live Targets: antigravity(source: apm.yml)+ unit PASS
- [x] ag-04 apm.yml `target: [agy]` 生效(manifest 解析正規化)。驗:live;unit `.../manifest_target_agy_alias` + `TestParseManifest_AgyAliasNormalization`【證據】live target:[agy]→Targets: antigravity(正規化)+ unit PASS
- [x] ag-05 `--target all` 展開不含 antigravity。驗:live Targets 行無 antigravity 且無 `.agents/rules|agents|mcp_config.json|hooks.json`;unit `TestResolveTargets_FlagAllExcludesAntigravity`【證據】live Targets: claude,codex,copilot,opencode(無 antigravity、無 .agents 專屬輸出)+ unit PASS
- [x] ag-06 apm.yml `target: [all]` 不含 antigravity。驗:live;unit `TestResolveTargets_ManifestAllExcludesAntigravity`【證據】live target:[all] 同 ag-05 + unit PASS
- [x] ag-07 僅 GEMINI.md 存在不觸發偵測。驗:live 無 target install → exit 2(deps 存在+零 target)且無 antigravity 部署;unit `TestDetectTargets_AllSignals`【證據】live GEMINI.md alone→exit 2、無 antigravity、無 .agents + unit TestDetectTargets_AllSignals PASS
- [x] ag-08 僅 AGENTS.md 存在不觸發偵測。驗:live(ab-fixture 已驗:exit 0「No dependencies」無 .agents);unit 同上【證據】live AGENTS.md alone(ab-fixture:exit 0 無部署)+ unit 同上 PASS
- [x] ag-09 `--target gemini` 詞彙合法但無 adapter → 非致命 "no registered handler" 診斷。驗:live 輸出含診斷【證據】live 輸出含 'no registered handler for target "gemini"'(exit 2,零可用 target+deps)
- [x] ag-10 `--target agy,claude` 逗號多 token。驗:live Targets 行同列 antigravity 與 claude【證據】live Targets: antigravity, claude;雙側檔案落地

### 7.2 MCP writer(6 項)

- [x] ag-11 sse → `serverUrl`,禁 legacy `url`。驗:live JSON 斷言;unit `TestWriteMCP_Antigravity_SSEUsesServerUrlField`【證據】live sse-probe 僅 serverUrl + unit TestWriteMCP_Antigravity_SSEUsesServerUrlField PASS
- [x] ag-12 http/streamable-http → `serverUrl`。驗:live http entry 斷言【證據】live http-probe 僅 serverUrl(無 url/httpUrl)
- [x] ag-13 stdio → `command`/`args`/`env`。驗:live 斷言三欄【證據】live stdio-probe command=git、args=[--version]、env 存在
- [x] ag-14 remote `headers` 保留。驗:live sse entry headers 斷言【證據】live sse-probe headers.Authorization 保留
- [x] ag-15 頂層 key `mcpServers`、路徑 `.agents/mcp_config.json`。驗:live【證據】live 頂層唯一 key mcpServers,檔案 .agents/mcp_config.json
- [x] ag-16 `${VAR}` 安裝期 bake(ResolveBake,agy 無 runtime 插值)。驗:live env 值 `${AB_PROBE_TOKEN}` → 輸出為解析後字面值【證據】live env TOKEN=${AB_PROBE_TOKEN}→輸出 'resolved-token-12345'(bake 成立)

### 7.3 Primitives 部署(5 項)

- [x] ag-17 instructions → `.agents/rules/<name>.md` 位元組一致(frontmatter 保留 = documented deviation)。驗:live cmp【證據】live cmp 位元組一致(frontmatter 保留)
- [x] ag-18 skills → `.agents/skills/<name>/` 全樹複製。驗:live cmp SKILL.md + 子檔【證據】live cmp SKILL.md + scripts/helper.sh 全樹一致
- [x] ag-19 agents → `.agents/agents/<name>/agent.md` 位元組一致(per-agent 目錄)。驗:live cmp;unit `TestDeployAntigravity_AgentsPerAgentDirectory`【證據】live cmp reviewer 一致 + unit PASS
- [x] ag-20 hooks → `.agents/hooks.json` 位元組一致。驗:live cmp【證據】live cmp hooks.json 一致
- [x] ag-21 commands/prompts 不部署(support matrix 排除)。驗:live `.apm/commands|prompts` 來源存在但 `.agents` 下無對應輸出;unit `TestNotDeployed_PerTarget/antigravity`【證據】live .apm/commands|prompts 來源存在、.agents 無對應輸出 + unit TestNotDeployed_PerTarget PASS

### 7.4 生命週期(4 項)

- [x] ag-22 lockfile `deployed_files` 記錄 dep 的 agents 路徑。驗:live apm.lock 含 `.agents/agents/depagent/agent.md`【證據】live apm.lock.yaml:27,29 含 .agents/agents/depagent/agent.md(deployed_files+hash)
- [x] ag-23 uninstall 反向清理:agent.md 刪除、空 per-agent 目錄剪枝、sibling(local reviewer)存活。驗:live uninstall dep;unit `TestRemoveDeployedFiles_AntigravityAgentDirPrunedSiblingSurvives`【證據】缺陷已修(uninstallRemovalKey 將 local:<path> 譯為 _local/<base>-<sha8>;manifest splice 補 local 合成 key)。live 重驗 6/6:exit 0、agent.md 刪、目錄剪枝、sibling 存活、apm_modules/_local 移除、apm.yml+lock 清空。unit TestRunUninstall_LocalPathDependencyRemovesModulesLockAndDeployedFiles
- [x] ag-24 同名 agents:first-declared wins + shadow 診斷(ResolvePrimitives 層,與 claude 同構)。驗:unit `TestRun_AgentSameNameCollision_FirstDeclaredWins`(claude+antigravity 表驅動)【證據】unit TestRun_AgentSameNameCollision_FirstDeclaredWins PASS(claude+antigravity)
- [x] ag-25 手改部署檔後 uninstall 保留(hash guard)。驗:live 改 depagent/agent.md 再 uninstall → 檔案保留【證據】live:手改後 uninstall 輸出 'keeping ... modified since deploy (hash mismatch)',檔案+USER EDIT 保留,模組目錄仍移除

### 7.5 agy 實機 A/B(4 項)

- [x] ag-26 `agy plugin validate` 認可 apm-go 全輸出格式(skills/agents 巢狀/mcpServers/hooks processed)。驗:live(⚠ validate 會查 stdio command 在 PATH)【證據】live validate:skills 1/agents 1/mcpServers 2/hooks 1 processed(ab-plugin)
- [x] ag-27 agy 實機發現部署產物(project 註冊後列出 agents/skills/rules 名稱)。驗:live `--print` + `--new-project`【證據】live --print+--new-project 列出 reviewer/fixture-skill/fixture-rule
- [x] ag-28 未註冊 project 時 `.agents/` 全部不可見(gotcha)。驗:live `default-cli-project` probe【證據】live probe 1-2:default-cli-project 下全部不可見
- [x] ag-29 rules 可發現但非 always-on 注入(僅 `user_global` 在 context;deviation 依據)。驗:live 6-probe 矩陣(prd.md Step 0)【證據】live probe 4/5/6:context 僅 user_global、中性 prompt 不遵循

### 7.6 品質關卡(3 項)

- [x] ag-30 `go build ./... && go vet ./... && go test ./...` 全綠。驗:重跑【證據】修復後重跑:build/vet/test 全綠(17 套件)
- [x] ag-31 觸及套件覆蓋率 ≥80%。驗:`go test -cover`(deploy/manifest/cmd/apm-go)【證據】cmd/apm-go 85.9% / deploy 87.7% / manifest 86.2%
- [x] ag-32 本輪修改 .go 檔 gofmt 乾淨(LF 正規化檢查,CRLF checkout 假訊號排除)。驗:`tr -d '\r' | gofmt -l`【證據】本輪全部修改檔 LF 正規化 gofmt 乾淨(adapter.go 既有 doc-comment 偏差為 HEAD 既存)

---

## 8. instructions applyTo pipeline(2026-07-11,task 07-11-instructions-applyto-parity)

> commits 04f4e58(轉換+過濾)/ ccc2c9d(閘門)。oracle = Python `_convert_to_claude_rules`
> + `parse_apply_to`/`yaml_double_quote`。live A/B = `evals/ab_instructions_applyto.py`
>(25/25 PASS,兩邊真 subprocess 對照)。unit = `instructions_claude_test.go` 18 case 表測。
> **Codex 對抗性逐項重驗(2026-07-11,--sandbox danger-full-access,gpt-5.5):19/19 CONFIRMED**
> —— 每項由 Codex 自跑指名測試並讀測試本體、自建 %TEMP% fixture 重現 live 項、自跑品質關卡;
> 無實質出入(2 條 cosmetic:其 bash 無 gofmt 改走 cmd;LF/CRLF 為既記 deviation)。

### 8.1 claude applyTo→paths 轉換(10 項)

- [x] ai-01 單一 glob → `paths:` 清單。【unit `single glob` + A/B `single` 3 斷言 PASS】
- [x] ai-02 頂層逗號清單分割(trim、去空段)。【unit + A/B `commalist` PASS】
- [x] ai-03 brace alternation `{css,scss}` 內逗號不切。【unit + A/B `brace` PASS】
- [x] ai-04 applyTo 值引號剝除(單/雙/混合,cutset Trim)。【unit 3 case PASS】
- [x] ai-05 有 frontmatter 無 applyTo → frontmatter 整段剝除(無條件 rule)。【unit + A/B `noapply` PASS】
- [x] ai-06 無 frontmatter → 直通(僅去前導空行)。【unit + A/B `nofm` PASS】
- [x] ai-07 glob 含 `"`/`\` → YAML 1.2 double-quote 逸出。【unit escape case PASS】
- [x] ai-08 copilot instructions 維持 byte-copy(applyTo 為其原生語意)。【unit `TestDeployOtherTargets_InstructionsStayByteIdentical` PASS】
- [x] ai-09 antigravity instructions 維持 byte-copy(07-05 documented deviation 不重開)。【同上測試鎖定】
- [x] ai-10 轉換位於 claude adapter 層 → local 與 dependency instructions 一體適用。【instructions_claude.go:112 deployClaudeInstructions 為 TypeInstructions 唯一路徑;lockfile hash 於寫後計算,provenance 不受影響】

### 8.2 收集過濾配對契約(2 項)

- [x] ai-11 `.apm/instructions/` 僅收 `*.instructions.md`;plain `.md` 忽略。【unit `TestCollectInstructions_OnlyInstructionsMDCollected` + A/B 兩邊 `plain .md ignored` PASS;survey 確認 cmd/apm-go 無依賴 plain 收集】
- [x] ai-12 agents/commands/hooks/prompts 收集規則不變。【`extractAgentName` 等未動;全套件回歸綠】

### 8.3 零 target 閘門(4 項)

- [x] ai-13 local prims + 零 target → exit 2 + 教學訊息,無部署無 lockfile。【unit 表測(instructions/agents 兩變體)+ A/B 兩邊 exit 2 PASS】
- [x] ai-14 空專案 → exit 0(兩邊)。【unit + A/B PASS】
- [x] ai-15 prims + `.claude/` 訊號 → 正常部署 exit 0(閘門不過火)。【unit `_StillDeploys` 回歸 PASS】
- [x] ai-16 deps-present 既有 F2 閘門不變;錯誤共用 `errNoDeployTarget()` 防漂移。【既有 F2 測試綠;install.go:624-629】

### 8.4 品質關卡(3 項)

- [x] ai-17 `go build/vet/test ./...` 全綠。【本輪兩 commit 後重跑 SUITE ALL-GREEN】
- [x] ai-18 觸及套件覆蓋率 ≥80%。【deploy 88.2%(轉換函式 100%)/ cmd/apm-go 綠】
- [x] ai-19 修改檔 gofmt LF 乾淨。【tr -d '\r' | gofmt -l 全空】
