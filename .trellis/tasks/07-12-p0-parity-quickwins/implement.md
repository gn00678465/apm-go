# Implement: pack 完整 parity + install 消費回路 + 共用 ContentScanner

> draft — 待 orchestrator/使用者 review

TDD 每步：先寫測試（RED）→ 實作（GREEN）→ `go build ./... && go vet ./... &&
go test ./...`。本輪不留可做而不做的 deferred（Gate 1 disposition 表的 (iii)
項是獨立子系統/另一批功能，非切割）。

## Phase 0 — target/targets 解析前置（登記冊 P1 #7 承接）

- [ ] `internal/manifest/target.go`：`CanonicalTargets` 加 `"kiro"`（all/
      antigravity 是 apm-go EXTENSION 保留）
- [ ] 測試：`ValidateTarget("kiro")` 成功；既有 target 不回歸
- [ ] `internal/manifest/manifest.go`：新增 `case "targets":`（SequenceNode
      逐元素驗證、非 list scalar 視單元素、空 list → error）；`case "target":`
      ScalarNode 加 CSV sugar（Split "," + trim + ValidateTarget 逐元素）；
      loop 後檢查 target/targets 兩鍵並存 → error
- [ ] 測試（對照 findings §2.1 決策樹）：`targets: [claude,copilot]`；
      `targets: claude`(scalar→單元素)；`targets: []`→error；
      `target: "claude,copilot"`(CSV→兩元素，先紅後綠)；兩鍵並存→error；
      既有純 `target: claude` 不回歸
- 驗證：`go test ./internal/manifest/...`

## Phase 1 — internal/security：ContentScanner + SecurityGate（Review Gate A）

**逐條對照 `apm/src/apm_cli/security/content_scanner.py` 與 `gate.py` 原始碼，
不得依賴摘要。**

- [ ] `scanner.go`：ScanFinding struct；30 條 suspicious-range 表（含分類）；
      `ScanText`（isascii fast-path、逐行逐字元、mid-file BOM 特判、
      ZWJ-in-emoji 特判）；`ScanFile`（解碼/讀檔失敗→空 slice）；
      `HasCritical`/`Summarize`/`Classify`/`StripDangerous`
- [ ] 測試（每分類 ≥1 fixture）：純 ASCII→空(驗 fast-path 被走)、tag char→
      critical、9 bidi-override→critical、VS SMP→critical、zero-width→warning、
      ZWJ 夾 emoji→info、BOM 檔首→info/中間→warning、nbsp→info、
      StripDangerous(critical+warning 移除、info 與 emoji ZWJ 序列保留)
- [ ] `gate.go`：ScanPolicy(OnCritical + ForceOverrides)、WARN/BLOCK/REPORT
      預建、ScanVerdict(HasCritical/ShouldBlock/CriticalCount/WarningCount/
      FilesScanned/AllFindings)、ScanFiles(symlink 用 Lstat 跳過)、ScanText
- [ ] 測試：WARN_POLICY critical 不 ShouldBlock；BLOCK_POLICY critical+
      force=false→ShouldBlock；force=true+ForceOverrides→不 block；symlink 跳過
- **Review Gate A**：30 條 range + 3 特殊規則逐條核對原始碼，不接受「摘要即實作」
- 驗證：`go test ./internal/security/...`（無依賴，可獨立高覆蓋）

## Phase 2 — pack 三路由重寫（detectOutputs + nothing-to-do exit 1）

- [ ] `internal/pack/detect.go`：`DetectOutputs(hasDeps, hasMarketplace,
      targets) (bundle, marketplace, pluginManifest bool, err)`——矩陣最後
      列(有 target 非 claude/copilot 其餘皆空→err)
- [ ] 測試：矩陣 9 列全覆蓋（含 hasDeps 只看 ParsedDeps 的修正）
- [ ] `cmd/apm/pack.go`：runPack 改呼叫 DetectOutputs、三獨立 if 區塊；
      nothing-to-do→`return err`（exit 1）；兩警告常數暫留（Phase 5 移除）
- [ ] 測試（Gate 2 fail-loud）：純 deps→觸發 Bundle 分支(先用 TODO)、
      純 `target: codex`→exit 1、完全空 apm.yml→exit 1(現行 exit 0，行為變更)、
      既有 marketplace-only→exit 0 不回歸
- 驗證：`go test ./internal/pack/... ./cmd/apm/... -run Pack`；
  `ab_marketplace_pack.py` 14/14

## Phase 3 — PluginManifestProducer（Review Gate B 一部分）

- [ ] `pluginmanifest/synthesize.go`：窄範圍 apm.yml 再讀（8 欄位，對照
      `plugin_parser.py`）
- [ ] 測試：name 缺→error；author string→{name}；author dict 缺 name→丟棄；
      homepage/repository 字串化；keywords 單字串→list
- [ ] `bundle/mcpjson.go`：`ReadMCPServers`(symlink/parse 失敗→空)；
      `SanitizeServers`（逐字對照 `plugin_manifest.py:73-278`）
- [ ] 測試（每條 redaction ≥1 fixture，Gate 3 逐條）：4 key 精確匹配→丟棄；
      accessKey/API_KEY substring→丟棄；URL userinfo；`--token=x`；
      `["--token","x"]` list 分離；`API_KEY=x`；`Bearer x`；供應商前綴
      (ghp_/sk-/AKIA 三代表)；server 名含 key 不消毒；巢狀任意深度丟棄；
      droppedPaths 非空/空
- [ ] `pluginmanifest/write.go`：skip-without-force 包裝 WriteOutput
- [ ] 測試：無 force 不覆寫、force 覆寫、`.github/` 額外 info
- [ ] `pluginmanifest/producer.go`：組裝 synthesize+mcpjson(claude only)+write
- [ ] 測試：`target: [claude,copilot]`→兩 plugin.json，claude 帶消毒 mcpServers、
      copilot 不帶；JSON 縮排/尾換行/鍵序
- 驗證：`go build/vet/test ./internal/pack/pluginmanifest/... -cover`

## Phase 4 — BundleProducer（Review Gate B）

- [ ] `bundle/collect.go`：包一層轉 Primitive → bundle-relative 路徑
      (agents 平坦/skills 遞迴/prompts→commands+.prompt.md→.md/instructions/
      commands/extensions 遞迴，對照 findings §3.2)
- [ ] `bundle/merge.go`：三合併，**兩相反方向獨立測試**：dep-vs-dep first-wins
      (force→last)；root-vs-dep file_map **dep 贏**；root-vs-dep hooks/mcp
      **root 贏**(overwrite)
- [ ] 測試（每方向 ≥1 衝突 fixture 斷言贏家）：dep-vs-dep first-wins；
      root/dep 同 `agents/foo.md`→dep 贏；root/dep 同 hooks key→root 贏
      (與前條相反，兩獨立斷言)；Local dep→Produce error
- [ ] `bundle/lockfile_pack.go`：pack: 節序列化(裸 hex、key 排序)+deserialize
- [ ] 測試：裸 hex(與 HashFileBytes 比對確認差異刻意)、key 排序、round-trip
- [ ] `bundle/producer.go`：收集→合併→sanitize name→EnsureWithinRoot→dry-run
      提前返回→非 dry-run 才跑 ScanFiles/ScanText(WARN_POLICY,來源檔)→彙總
      警告→寫檔→hooks.json/.mcp.json(非空)/plugin.json(恆)→pack: 節
- [ ] 測試：dry-run 零寫入+**掃描器零呼叫**(mock)；非 dry-run critical char→
      印警告仍成功；--force 對掃描零影響；sanitize name `../../etc`→unnamed；
      output_files 排序+條件式+恆附 plugin.json
- **Review Gate B**：.mcp.json 6 regex 逐字核對；file_map/hooks/mcp 方向表
  兩相反規則各自驗證
- 驗證：`go build/vet/test ./internal/pack/bundle/... -cover`

## Phase 5 — pack CLI 接線 + P0 警告退場

- [ ] `cmd/apm/pack.go`：三分支真呼叫 producer；新增 `--force`；`--dry-run`
      擴展到新 producer
- [ ] 移除 packDepsWarning/packTargetWarning 及列印點；舊警告測試改斷言新行為
- [ ] 測試（Gate 2 fixture 替換舊「印警告」斷言）：deps-only→build/ 真產出；
      target-only→.claude-plugin/plugin.json 產出；三者皆有→三輸出+訊息序
      Bundle→Marketplace→PluginManifest；一 producer 失敗→已完成輸出不回滾
- [ ] license/SBOM「license undeclared」警告文字移植（authoring path，用
      既有 `m.License`）
- 驗證：`go build/vet/gofmt/test ./... -cover`；`ab_marketplace_pack.py` 14/14

## Phase 6 — install <bundle-path> 消費回路（Review Gate C）

- [x] `localbundle/detect.go`：`DetectLocalBundle`(目錄根 plugin.json/.zip/
      .tar.gz→BundleInfo；皆非→nil,nil)
- [x] 測試：三形狀各 fixture；像本地依賴但無 plugin.json→nil,nil
- [x] `localbundle/verify.go`：`VerifyBundleIntegrity`(無 lockfile→warn 不擋；
      symlink 一律拒；逐檔裸 hex；反向 unlisted-file 檢查)
- [x] 測試：乾淨全過；竄改位元組→error 列路徑；symlink(列/未列各 fixture)→
      error；多 unlisted 檔→error；路徑穿越檔名→error
- [x] `localbundle/integrate.go`：`IntegrateLocalBundle`(部署 plugin-native→
      resolved targets；零 target→warn 不失敗；check_target_mismatch 警告)
- [x] 測試：正常部署；零 target；target mismatch 印警告仍部署
- [x] `cmd/apm/install.go`：runInstall 最前插 DetectLocalBundle 分支→繞過
      resolver、verify+integrate、寫專案 lockfile local_deployed_*
- [x] 測試：`install build/pkg-1.0.0/` 成功部署+lockfile；一般 install 不回歸；
      flag 衝突→彙總 usage error
- **Review Gate C**：竄改+symlink 拒絕測試通過；未誤用 F1 normalizeLocalDep ✅
- 驗證：`go build/vet/test ./internal/localbundle/... ./cmd/apm/... -cover` ✅

> ### Phase 6 狀態：**完成（DONE）** — 2026-07-13
>
> 實作：`internal/localbundle/{detect,verify,integrate}.go`、
> `internal/archive/zip.go`（`SafeExtractZip`）、`cmd/apm/install.go`（runInstall
> 早退 + tryLocalBundleInstall/runLocalBundleInstall/persistLocalBundleDeployment）、
> `internal/pack/bundle/lockfile_pack.go` 的 `ParsePackMetadata`。
>
> 驗證（雙軌，claim 不採信）：
> - **主 session 親自**：`go build/vet/test ./...` 全綠；localbundle 81.6%、
>   cmd/apm 84.1%（升 +1.3pp）。真實 CLI 閉環實測（bin/apm-go.exe）：
>   pack→install（目錄）部署 3 檔 + lockfile `local_deployed_*`、apm.yml 未建立；
>   pack→install（.zip 走 SafeExtractZip）成功；byte 竄改→`Hash mismatch` exit 1
>   零部署；unlisted 檔→`Unlisted bundle file` exit 1 零部署。
> - **codex 對抗性驗證**：`research/codex-verify-phase6.md` 判定
>   **OVERALL PASS 22/22**（硬性 checklist A/B/C/D 全 CONFIRMED）。含獨立重現：
>   真實 NTFS symlink 拒絕（A2）、`/absolute`+`C:\absolute` 穿越（A4）、
>   registry fresh+frozen + MCP E2E 零回歸（C2）、HEAD 基線覆蓋率對照（D2）。
>
> **教訓（codex 卡死根因）**：codex exec 卡在 `Reading additional input from
> stdin...`——背景任務 stdin 無 EOF 導致阻塞（非 `| tail` 緩衝）。修法：
> `codex exec ... < /dev/null`。續作以此為準。

## Phase 7 — audit Unicode 掃描接線（共用掃描器第二接點）

- [ ] `cmd/apm/audit.go`：新增 flag(命名與 codex 確認,暫定 `--content`)觸發
      掃描；bare 行為不變(既有 SHA 測試零回歸,開工前跑 baseline)
- [ ] 掃描邏輯：讀 lockfile DeployedFiles(跨 deps+local)→逐檔 ScanFile→
      exit 0/1/2；純文字輸出
- [ ] **drift 語意核對**（design.md「audit drift 澄清」）：逐項比對 apm-go
      既有 SHA drift 與 Python drift(是否偵測 lockfile 未記錄但磁碟存在的孤兒/
      replay 級差異)；核對結果寫入報告；有真實缺口則 Gate 6b 具體列出
- [ ] 測試：全乾淨→exit 0；critical char→exit 1 列檔案+位置；warning-only→
      exit 2；既有 SHA 測試 100% 不回歸
- [ ] --help/Long 更新：明寫本掃描不含 drift replay/--ci/--external 等(Gate 6b)
- 驗證：`go test ./cmd/apm/... -run Audit -cover`

## Phase 8 — 全域驗證 + Gate 6b 重播 + 登記冊更新（Review Gate D）

1. [ ] `go build/vet/test ./... -cover` 全綠；`gofmt -l` 新檔乾淨
2. [ ] `go build -o bin/apm-go.exe ./cmd/apm`
3. [ ] test1 fixture 雙邊重播：pack 三輸出逐一比對；`install build/<name>-<ver>/`
      成功+lockfile；竄改測試 install 拒絕；完整雙邊 transcript 存報告
4. [ ] `ab_marketplace_pack.py` 14/14 不回歸；`ab_uninstall.py` 不回歸
5. [ ] ContentScanner A/B fixture：Python `apm pack` 隱字警告數 vs apm-go 相同
      (fixture 含 ≥1 critical + 1 warning，非 tautology)
6. [ ] codex 硬性 checklist 逐項對抗性驗證：合併方向表、6 regex、裸 hex install
      消費、nothing-to-do exit 1 不誤傷、kiro/CSV 不回歸
7. [ ] adversarial 發現分級修復(CRITICAL/HIGH 必修)，重跑 1-4
8. [ ] 報告（Gate 6b 順序）：**先寫「此修正不做什麼」**再寫統計
9. [ ] 更新 `cli-surface-parity-register.md` §3.1/§3.2(Gate 5 living-doc)
10. [ ] 最終整合 commit + register 更新；勾選 implement.md 與 prd.md Phase 2 AC

## Review Gates 總覽

- A（Phase 1 後）：ContentScanner 逐條核對原始碼
- B（Phase 3/4 後）：.mcp.json 6 regex 逐字；合併方向表兩相反規則
- C（Phase 6 後）：install 竄改/symlink 拒絕；未誤用 F1
- D（Phase 8）：Gate 6b 重播 transcript；「此修正不做什麼」在統計之前

## Rollback Points

Phase 0/1 獨立(internal/manifest、internal/security 互不依賴)。Phase 2-5(pack)
與 6(install)與 7(audit)除 Phase 1 共用外互相獨立。五接線檔每 phase 各一 commit。
