# Implement: apm uninstall

TDD:每步先寫測試(RED)→ 實作(GREEN)→ `go build/vet/test ./...`。
輸入:`uninstall-checklist.md`(權威) + `design.md` + `research/uninstall-parity.md`。
scope:核心 + MCP stale(定案 B);`-g` 報未支援(定案 A)。

## 執行順序(分階段,可分次 Sonnet 派工;每階段獨立 commit)

### 步驟 1 — 前置:lockfile MCPServers 欄位 + install 寫入(un-060, N3)
- [x] `lockfile/types.go`:`Lockfile` 加 `MCPServers []string`;parse/write/序列化/knownKeys 五處清單同步(比照 provenance 欄位慣例)
- [x] `install.go` `deployAndFinalize`:記錄本次部署的 MCP server 名單寫入 `MCPServers`
- [x] 測試:round-trip 保留 MCPServers;既有 install 測試不破;舊 lockfile(無此欄位)讀取 fail-open(空集)
- 驗證:`go test ./internal/lockfile/... ./cmd/apm/... -count=1`

### 步驟 2 — lockfile Delete API(un-070~072, N3)
- [x] `Lockfile.RemoveKeys(keys []string)`:從 slice 移除 + 重建 index;`FindByKey` 仍正確
- [x] key 比對用既有 unique key(對照 identity.py:非 github.com 才加 host 前綴——確認 apm-go 既有行為一致,不一致則補齊)
- [x] 測試:刪單/多 key、刪後查找、清空判定

### 步驟 3 — deploy 反向刪檔能力(N1, un-050~053)⚠️安全紅線
- [x] 新檔 `internal/deploy/uninstall.go`:`RemoveDeployedFiles(projectDir, files, hashes)`
  - path-containment(重用 `archive/extract.go` `Contained`)→ 逃逸拒絕
  - **hash 保護**:現檔 hash ≠ `hashes[path]` → 保留 + 警告(不刪)
  - 相符 → 刪;`cleanupEmptyParents` 清空母資料夾
- [x] **測試(舊坑 1 必含)**:正常刪除、hash 不符保留+警告、path 逃逸拒絕、使用者手寫檔(不在 files 清單)不動、空母資料夾清理
- 驗證:`go test ./internal/deploy/... -run Remove -count=1`

### 步驟 4 — apm.yml 移除(N4, un-020~022)
- [x] `removePackagesFromManifest`:yamlcore node 級刪除 `dependencies.apm`/`devDependencies.apm` 命中條目(忽略 ref/alias 比對);dev 空殼清理(cli.py:151-156);保留其他條目與排版
- [x] **測試(舊坑 1)**:fixture 含「已存在、手動排版、多依賴」;刪 prod、刪 dev、dev 清空刪 key、devDependencies 合成殼整段刪、其他依賴與排版保留
- 驗證:PASS

### 步驟 5 — transitive orphan BFS(N5, un-040~043)
- [x] 抽出/重用 `resolver/update.go:54-65` ResolvedBy BFS 於「移除後找孤兒」;`actualOrphans = orphans - remaining`
- [x] 測試:A→B 鏈,uninstall A 且 B 無他人依賴→B 入孤兒;B 另有 parent→保留;多層鏈
- 驗證:PASS

### 步驟 6 — 目標解析與比對(N6, un-010~019)
- [ ] owner/repo·URL·SSH·FQDN 正規化 + 忽略 ref/alias 的 identity 比對
- [ ] marketplace 記法:重用 `ParseRef`;新寫 lockfile 離線優先 → registry fallback(--dry-run 跳過)→ supply-chain guard(canonical 不在 lockfile 拒絕);`#ref` 忽略
- [ ] **standalone MCP(un-019,增強)**:apm 套件找不到 → 比對 `dependencies.mcp` server 名稱;命中歸類 MCP-removal 目標
- [ ] 測試:五種輸入形狀解析到同 canonical;marketplace 命中 lockfile;supply-chain guard 用會 panic 的 fake registry 證明拒絕先於任何移除;#ref 被忽略;not-found 警告續行;全 not-found 不變更退出;`uninstall <mcp-name>` 命中 dependencies.mcp
- 驗證:PASS(Review Gate A:supply-chain guard 先於移除)

### 步驟 7 — MCP 清理(N7, un-060~065, 定案 B:transitive + standalone)
- [ ] **共用底層**:各 target(claude/codex/copilot/antigravity/opencode)擴充「read→del 指定 server key→write」路徑
- [ ] (a) transitive:`stale = old_mcp_servers - new`;走共用底層;`lockfile.MCPServers = new`
- [ ] (b) standalone(增強):命中 dependencies.mcp 的 server → 從 apm.yml dependencies.mcp 刪條目(對稱 upsertMCPEntry,yamlcore node 級) + 走共用底層 + 從 lockfile.MCPServers 移除
- [ ] 測試:裝 2 個 MCP server 於多 target,uninstall 貢獻其一的套件→該 server 從各 target 消失、另一保留(transitive);`install --mcp foo`→`uninstall foo`→ apm.yml dependencies.mcp 無 foo、各 target 無 foo、lockfile 更新(standalone);lockfile 舊版無欄位 fail-open
- 驗證:PASS

### 步驟 8 — CLI orchestration + dry-run + -g 未支援(N8/N9, un-001~004, un-080~081, un-090/091, un-100~103)
- [ ] `cmd/apm/uninstall.go`:串接步驟 3-7 成 13 步管線;`--dry-run`/`-v`;`-g` 註冊但報「未支援」明確錯誤
- [ ] `main.go` 註冊 `uninstallCmd()`
- [ ] dry-run:步驟 1-3 記憶體、零寫入、跳 registry;摘要 + not-found 警告;例外 exit 1
- [ ] 測試:--help 旗標集恰為 dry-run/-v/-g/--help;dry-run 零寫入;-g 報未支援;apm.yml 不存在 err;摘要格式
- 驗證:`go build/vet/gofmt/test ./... -cover` 全綠

### 步驟 9 — Phase V 整合 + A/B(un-V01~V08)
- [ ] `internal/deploy` 或 `cmd/apm` e2e:install→uninstall 往返(多 target 多 primitive 型)全清;只刪自己的;hash 保護;orphan;共用資源 Phase2 還原;lockfile 清空刪檔
- [ ] `D:\Projects\apm-dev\evals\ab_uninstall.py`:對照 `uv run apm uninstall`,apm.yml/lockfile/apm_modules/各 target 檔案最終狀態逐項比對;deviation(`-g` 不做)明確記錄
- 驗證:A/B 0 failed(deviation 除外)

## 中樞驗證檢查清單(每階段完成後,中樞逐項親跑)
1. [ ] 親自重跑 build/vet/test 全綠;既有 install/deploy/lockfile 測試未破
2. [ ] 真機:install 一個含 skills/agents/commands/instructions/hooks/MCP 的本地套件到多 target → uninstall → 確認 deployed_files 全消、apm.yml/lockfile/apm_modules 乾淨、使用者手寫檔還在
3. [ ] 真機負向:手改一個已部署檔 → uninstall → 該檔保留 + 警告
4. [ ] 跑 A/B,要求 0 failed(deviation 除外)
5. [ ] 派 adversarial Explore 深查:hash 保護繞過、path 逃逸、Phase2 共用資源、supply-chain guard 時序、orphan remaining 排除、MCP 反向移除只刪 stale

## Review Gates
- A(步驟 6 後):supply-chain guard 先於任何移除(fake registry panic 斷言)
- B(步驟 3+步驟 9 後):安全刪除紅線——hash 不符保留、path 逃逸拒絕、只刪自己的(含既有內容 fixture)
- C(步驟 9 後):A/B 非 tautology(至少一個檔案級 diff 證明比對器能抓差異)

## 已知 deviation
- `-g/--global` 報未支援(定案 A,另開 task)
- 其餘對齊 Python;實作中若發現原版 bug 於對應 checklist 條目標註

## Rollback Points
步驟 1-2(lockfile)、3(deploy 刪檔)可先獨立合入且不影響既有流程(無人呼叫)。
步驟 8 才把 CLI 接上。每步獨立 commit。
