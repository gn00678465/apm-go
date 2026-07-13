# Phase 6 對抗性驗證：`install <bundle-path>` 消費回路

A1 CONFIRMED — listed file 逐檔計算 SHA-256 並比對，CLI 在整合前遇 errs 立即返回；既有 E2E 另斷言 tamper 後 `.claude` 與 lockfile 均不存在（internal/localbundle/verify.go:77；cmd/apm/install.go:716；cmd/apm/install_localbundle_test.go:90）
A2 CONFIRMED — 第一段 WalkDir 對每個 entry 使用 Lstat/ModeSymlink，listed/unlisted 皆拒；既有兩種 fixture 在 Windows 會 skip，已另以真實 NTFS symlink 親自重現拒絕成功（internal/localbundle/verify.go:38；internal/localbundle/verify_test.go:43；internal/localbundle/verify_test.go:68）
A3 CONFIRMED — 第三段 WalkDir 只豁免 `apm.lock.yaml`/`plugin.json` 與 listed regular files，其餘全部列為 unlisted tampering；單檔與多檔測試均實跑通過（internal/localbundle/verify.go:96；internal/localbundle/verify_test.go:89；internal/localbundle/verify_test.go:106）
A4 CONFIRMED — `safeBundleRelPath` 同時拒絕空值、POSIX/平台絕對路徑、volume/磁碟機代號與任一 `..` segment；`../` fixture 與額外 `/absolute`、`C:\\absolute` 重現皆通過（internal/localbundle/verify.go:148；internal/localbundle/verify_test.go:121）
A5 CONFIRMED — ZIP 在抽取前限制 entries，逐 entry 先拒 symlink，再拒絕絕對/volume/`..`/root escape，以 capped reader 限制總 bytes；所有錯誤清 staging，成功才 rename，對應 traversal/symlink/兩種 cap 測試實跑全綠（internal/archive/zip.go:40；internal/archive/zip.go:57；internal/archive/zip.go:71；internal/archive/zip.go:80；internal/archive/zip.go:104；internal/archive/zip_test.go:91；internal/archive/zip_test.go:103；internal/archive/zip_test.go:113；internal/archive/zip_test.go:127）
A6 CONFIRMED — archive extraction 任一錯誤直接清 temp 並回 error；CLI 包裝為 `bundle security check failed` 且 handled=true，不進 registry fall-through；corrupt ZIP 既有測試與額外 CLI 重現均通過（internal/localbundle/detect.go:100；internal/localbundle/detect_test.go:139；cmd/apm/install.go:659）
A7 CONFIRMED — `ParsePackMetadata` 原樣讀取 `bundle_files` scalar（bare hex round-trip 測試存在），`normalizeHash` 接受 bare/`sha256:` 且拒任何其他帶冒號前綴；三種輸入已另行實跑（internal/pack/bundle/lockfile_pack.go:90；internal/pack/bundle/lockfile_pack_test.go:108；internal/localbundle/verify.go:127）

B1 CONFIRMED — sole-positional probe 是 `runInstall` 第一個 executable branch，位於 CI/frozen 判斷與 `os.ReadFile("apm.yml")` 之前，且 guard 明確為 `len(packages)==1`（cmd/apm/install.go:173；cmd/apm/install.go:180；cmd/apm/install.go:209）
B2 CONFIRMED — local path 直接 verify→resolve targets→integrate→persist，只寫 `apm.lock.yaml` 的 `LocalDeployedFiles/Hashes`；E2E 斷言不建立 apm.yml，另以既有 sentinel apm.yml 親自重現 byte-identical（cmd/apm/install.go:710；cmd/apm/install.go:766；cmd/apm/install_localbundle_test.go:57）
B3 CONFIRMED — persist 先把既有與本次 files 放入 set 後排序，hash map 只 additive/同路徑更新；額外連續兩次 persist 重現確認第一、第二次的 path/hash 全保留（cmd/apm/install.go:786；cmd/apm/install.go:800）
B4 CONFIRMED — mismatch helper 只回 warning string；caller 列印後無條件繼續 Integrate，codex-vs-claude mismatch 測試實跑仍部署 `.codex/agents/foo.toml`（internal/localbundle/verify.go:170；cmd/apm/install.go:729；cmd/apm/install_localbundle_test.go:161）
B5 CONFIRMED — target 解析為空時先印明確 warning 並 return nil，Integrate/persist 均未呼叫；測試實跑且確認無 lockfile（cmd/apm/install.go:720；cmd/apm/install_localbundle_test.go:141）
B6 CONFIRMED — archive-shaped regular file若抽取成功但找不到根 `plugin.json`，detector 回 nil，CLI 依 `.zip/.tar.gz/.tgz` 回 targeted usage error且 handled=true；另以合法無 plugin ZIP 親自重現精確 IM7 分支（internal/localbundle/detect.go:108；cmd/apm/install.go:664；cmd/apm/install.go:674）
B7 CONFIRMED — local bundle 彙總拒絕 `--skill`/啟用中的 `--allow-insecure` 並逐名列出；`--target` 傳入 ResolveTargets 且 mismatch target 實跑仍按該 target 部署（cmd/apm/install.go:682；cmd/apm/install.go:701；cmd/apm/install_localbundle_test.go:113；cmd/apm/install_localbundle_test.go:127；cmd/apm/install_localbundle_test.go:161）

C1 CONFIRMED — 無 `plugin.json` 的目錄由 detector 回 `(nil,nil)`，try 分支回 handled=false，既有 F1 流程才執行 `normalizeLocalDep`；真實 regression 測試實跑並產生 local lock entry（internal/localbundle/detect.go:82；cmd/apm/install.go:180；cmd/apm/install.go:284；cmd/apm/install_localbundle_test.go:193）
C2 CONFIRMED — `len(packages)!=1` 直接略過 probe；一般單一 package 若非現存 filesystem path 立刻 fall through；`--mcp` 在 `installCmd` 先路由到 `runMCPInstall`；registry fresh+frozen replay 與 MCP E2E 均實跑通過（cmd/apm/install.go:133；cmd/apm/install.go:180；cmd/apm/install.go:654；cmd/apm/registry_e2e_test.go:90；cmd/apm/mcpinstall_test.go:672）
C3 CONFIRMED — 舊 `SafeExtract` 仍明確只收 gzip/tar 並拒 ZIP；新增 `SafeExtractZip` 只有 local-bundle 的 `extractZipArchive` 呼叫，registry `SafeExtract` inbound call site 保留，舊拒 ZIP 測試實跑通過（internal/archive/extract.go:51；internal/archive/zip.go:31；internal/localbundle/detect.go:124；internal/localbundle/detect.go:129；internal/archive/extract_test.go:172）
C4 CONFIRMED — `git status --short`/`git diff --name-only HEAD` 顯示既有範圍只有 `cmd/apm/install.go` 與 Phase 4 的 `lockfile_pack.go` 被改，`deploy`/`manifest`/`security`/`pack.go`/`audit.go` 均無 Phase 6 diff（cmd/apm/install.go:173；internal/pack/bundle/lockfile_pack.go:90）

D1 CONFIRMED — 在專案根親跑 `go build ./...`、`go vet ./...`、`go test -count=1 ./...` 全部 exit 0；`go list ./...` 實際為 23 packages，checklist 的 22 是過時計數但綠燈條件成立（go.mod:1；cmd/apm/install.go:173；internal/localbundle/verify.go:34）
D2 CONFIRMED — 親跑 coverage：`internal/localbundle` 81.6% ≥ 80%；同一工作樹 `cmd/apm` 84.1%，並以 HEAD archive 獨立跑出基線 82.8%，故未下降而是 +1.3pp（internal/localbundle/verify_test.go:13；cmd/apm/install_localbundle_test.go:57）
D3 CONFIRMED — 對 13 個本 Phase 觸碰/新增 Go 檔執行 `gofmt -l` 無輸出，逐 byte 掃描無 CR（全為 LF）（cmd/apm/install.go:1；internal/localbundle/detect.go:1；internal/archive/zip.go:1；internal/pack/bundle/lockfile_pack.go:1）
D4 CONFIRMED — 親跑兩個 A/B harness：marketplace pack 14 passed/0 failed；uninstall 6 passed/0 failed，另有 2 個腳本既定 documented deviations，無新增 fail（D:/Projects/apm-dev/evals/ab_marketplace_pack.py:235；D:/Projects/apm-dev/evals/ab_uninstall.py:176）

OVERALL: PASS (22/22 confirmed)
