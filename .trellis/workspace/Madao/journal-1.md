# Journal - Madao (Part 1)

> AI development session journal
> Started: 2026-06-27

---



## Session 1: Phase 0: YAML safe-loader and round-trip core

**Date**: 2026-06-27
**Task**: Phase 0: YAML safe-loader and round-trip core
**Branch**: `feat/phase-0-yaml-core`

### Summary

Implemented Phase 0 YAML core: SafeLoad (anchor/alias/custom-tag rejection, multi-doc rejection), SafeDump (byte-exact round-trip), IsVendorExtKey, CLI validate/normalize commands. Switched from yaml.v3 to yaml.v4. All 40 tests pass, 87.8% coverage, 21/21 A/B tests pass vs Python apm. Review fixes applied (SF-001~SF-004).

### Main Changes

(Add details)

### Git Commits

| Hash | Message |
|------|---------|
| `0deb4af` | (see git log) |
| `54df1fa` | (see git log) |
| `89bdda3` | (see git log) |
| `010f285` | (see git log) |

### Testing

- [OK] (Add test results)

### Status

[OK] **Completed**

### Next Steps

- None - task complete


## Session 2: Phase 1: Manifest parsing, validation, init, and review fixes

**Date**: 2026-06-28
**Task**: Phase 1: Manifest parsing, validation, init, and review fixes
**Branch**: `feat/phase-1-manifest`

### Summary

Implemented Phase 1 manifest layer: ParseManifest with 21 reqs (mf-001~021, tg-004, sc-006), DependencyReference ABNF parser, MCP validation, marketplace source validation, placeholder recognition, target vocabulary with aliases and auto-detection. Rewrote apm init as full interactive flow (prompts, numbered toggle target selector, confirmation panel, --yes mode, filesystem signal detection). Review fixes applied (init YAML safety, target:all false positive, insecure bool, CLI tests). 46/46 A/B tests pass, 85.7% manifest coverage.

### Main Changes

(Add details)

### Git Commits

| Hash | Message |
|------|---------|
| `2b2daad` | (see git log) |
| `b629604` | (see git log) |
| `8fc73aa` | (see git log) |
| `b34b239` | (see git log) |
| `3a265e1` | (see git log) |

### Testing

- [OK] (Add test results)

### Status

[OK] **Completed**

### Next Steps

- None - task complete


## Session 3: Phase 1: Manifest parsing, validation, init, and review fixes

**Date**: 2026-06-28
**Task**: Phase 1: Manifest parsing, validation, init, and review fixes
**Branch**: `feat/phase-1-manifest`

### Summary

Implemented Phase 1 manifest layer: ParseManifest with 21 reqs (core fields, target validation, registries, dep ABNF parsing, MCP validation, marketplace, placeholder recognition). Rewrote init with full interactive flow (metadata prompts, target toggle, auto-detection, confirmation). Three review rounds (opus-4.8, gpt-5.4, sonnet-4.6) with all findings fixed. 46/46 A/B tests pass vs Python apm.

### Main Changes

(Add details)

### Git Commits

| Hash | Message |
|------|---------|
| `2b2daad` | (see git log) |
| `b629604` | (see git log) |
| `8fc73aa` | (see git log) |
| `b34b239` | (see git log) |
| `3a265e1` | (see git log) |
| `14b9602` | (see git log) |
| `0d7650e` | (see git log) |

### Testing

- [OK] (Add test results)

### Status

[OK] **Completed**

### Next Steps

- None - task complete


## Session 4: Phase 2: Dependency Resolution Engine

**Date**: 2026-06-29
**Task**: Phase 2: Dependency Resolution Engine
**Branch**: `main`

### Summary

實作依賴解析引擎：semver wrapper (deps.dev/util/semver, 24/24 oracle)、BFS+fixpoint resolver、diamond tri-modal 衝突偵測、lock replay、why 診斷、update 邏輯。經 gpt-5.4+opus-4.6 review 後修正 9 項問題。

### Main Changes

(Add details)

### Git Commits

| Hash | Message |
|------|---------|
| `a9b8311` | (see git log) |
| `3af3370` | (see git log) |
| `f38164d` | (see git log) |
| `c2c944a` | (see git log) |
| `51dc086` | (see git log) |

### Testing

- [OK] (Add test results)

### Status

[OK] **Completed**

### Next Steps

- None - task complete


## Session 5: Phase 2+3: Resolver Engine + Lockfile Write + Install CLI

**Date**: 2026-06-29
**Task**: Phase 2+3: Resolver Engine + Lockfile Write + Install CLI
**Branch**: `main`

### Summary

Phase 2: 依賴解析引擎（semver/resolver/lockfile read）14 reqs + 9 項 review 修正。Phase 3: lockfile 序列化/hash/tree_sha256/frozen install/install CLI 18 reqs + 10 項 review 修正。經 gpt-5.4/opus-4.6/opus-4.8/gemini-3.5 multi-model review。

### Main Changes

(Add details)

### Git Commits

| Hash | Message |
|------|---------|
| `a9b8311` | (see git log) |
| `3af3370` | (see git log) |
| `f38164d` | (see git log) |
| `c2c944a` | (see git log) |
| `514df57` | (see git log) |
| `0a2e3b2` | (see git log) |
| `021f8c3` | (see git log) |
| `ffd959a` | (see git log) |

### Testing

- [OK] (Add test results)

### Status

[OK] **Completed**

### Next Steps

- None - task complete


## Session 6: Phase 4: Primitive Sourcing + Target Deploy

**Date**: 2026-06-30
**Task**: Phase 4: Primitive Sourcing + Target Deploy
**Branch**: `feat/phase-4-target-deploy`

### Summary

實作 Phase 4 deploy 層：6 個 target adapter、primitive 衝突解決、--skill 子集安裝、positional arg 支援。Codex (gpt-5.5) 外部驗證 Phase 2-4 共 11 項全部 PASS。Review Forge 發現 9 項問題修正 8 項。

### Main Changes

(Add details)

### Git Commits

| Hash | Message |
|------|---------|
| `4458d41` | (see git log) |
| `2aea588` | (see git log) |
| `d414d4d` | (see git log) |
| `ff1a53f` | (see git log) |
| `c22d535` | (see git log) |

### Testing

- [OK] (Add test results)

### Status

[OK] **Completed**

### Next Steps

- None - task complete


## Session 7: Phase 4-T: Per-Target Deploy Matrix + Review Fixes

**Date**: 2026-06-30
**Task**: Phase 4-T: Per-Target Deploy Matrix + Review Fixes
**Branch**: `feat/phase-4t-target-matrix`

### Summary

完成 4-T 逐 target 部署矩陣測試（detect_test 11 訊號、not_deployed 負向、cursor/windsurf 診斷、copilot prompts）。修正 antigravity adapter（移除 agents、新增 hooks）。Review Forge 三模型 review 6 項全修並由 opus 獨立驗證。補齊 codex/copilot hooks 部署、claude hooks 維持 settings.json deferred。Codex 黑箱驗證 4-T 矩陣 9/9 + hooks 5/5 全 PASS。

### Main Changes

(Add details)

### Git Commits

| Hash | Message |
|------|---------|
| `e27262f` | (see git log) |
| `f82f021` | (see git log) |
| `5d700f2` | (see git log) |
| `ad699f4` | (see git log) |

### Testing

- [OK] (Add test results)

### Status

[OK] **Completed**

### Next Steps

- None - task complete


## Session 8: Phase 5 Security Hardening

**Date**: 2026-06-30
**Task**: Phase 5 Security Hardening
**Branch**: `feat/phase-5-security`

### Summary

實作 OpenAPM v0.1 Phase 5 安全強化 (req-sc-001~008)：internal/archive 安全 tar.gz 解壓 (路徑逃逸/symlink/容器/大小/entry 上限)、internal/credsec 憑證與 host-class 防護 (PSL eTLD+1、跨 class redirect 丟憑證、非https 拒附、secret 遮罩)、apm audit 與 frozen install 重構 (磁碟完整性優先、registry archive 驗 hash 後安全解壓)。獨立 opus 審查發現並修補 repo_url 解壓逃逸高風險缺陷，再次獨立驗證確認 sound。原生 oracle 測試 + 黑箱二進位驗證全綠。

### Main Changes

(Add details)

### Git Commits

| Hash | Message |
|------|---------|
| `0c76f58` | (see git log) |
| `534c174` | (see git log) |

### Testing

- [OK] (Add test results)

### Status

[OK] **Completed**

### Next Steps

- None - task complete


## Session 9: Phase 5 Review Forge (code review + fix + verify)

**Date**: 2026-07-01
**Task**: Phase 5 Review Forge (code review + fix + verify)
**Branch**: `feat/phase-5-security`

### Summary

Review Forge 三模型審查 (opus-4.6/gemini-3.5/codex-gpt-5.5) synthesize 出 9 項，核准修 S1-S4：S1 stripPort 未加括號 IPv6 截斷 (host-class 誤合併)、S2 VerifyDeployedState 非 sha256 envelope fail-open、S3 NewAuthDropRedirect stdlib 剝憑證 doc、S4 MatchesSecretPattern 大小寫繞過。codex exec 獨立 verify 4/4 VERIFIED、無回歸。S5/S6 未核准、S7-S9 設計/追蹤保留。

### Main Changes

(Add details)

### Git Commits

| Hash | Message |
|------|---------|
| `63cd195` | (see git log) |
| `545f6ed` | (see git log) |

### Testing

- [OK] (Add test results)

### Status

[OK] **Completed**

### Next Steps

- None - task complete


## Session 10: Registry HTTP Consumer + credsec wiring + experimental gate

**Date**: 2026-07-01
**Task**: Registry HTTP Consumer + credsec wiring + experimental gate
**Branch**: `feat/phase-5-security`

### Summary

新增 internal/registry HTTP 下載 consumer 接線 credsec (sc-003/005/007/008) 與 lk-013；install 寫入 lockfile v2 並支援 frozen 離線+網路 replay (fail-closed)；新增 internal/experimental 旗標子系統與 apm experimental 指令，將 registry 存取 gate 於實驗性旗標 (僅 gate 網路、維持 oracle 相容)。codex 多輪外部驗證 + 對原版 apm-cli 0.21.0 live A/B (resolved_hash 位元一致)。

### Main Changes

(Add details)

### Git Commits

| Hash | Message |
|------|---------|
| `304fb86` | (see git log) |

### Testing

- [OK] (Add test results)

### Status

[OK] **Completed**

### Next Steps

- None - task complete


## Session 11: MCP install 解析與 target 部署 (req-mf-013 + mcp deploy)

**Date**: 2026-07-02
**Task**: MCP install 解析與 target 部署 (req-mf-013 + mcp deploy)
**Branch**: `feat/mcp-resolve-deploy`

### Summary

實作 mf-013 placeholder 解析矩陣（bake/translate x 5 FieldPos）與 4 個 target 的 MCP config writer（antigravity/claude/codex/copilot），含 primitive 收集/override、lockfile provenance 歸屬、權限強制。全程經 codex exec 多輪外部審查（每步驟 review gate）修正 HIGH/MEDIUM 發現；額外修正兩個由本任務 AC 曝露的既有缺口：lockfile 頂層 local_deployed_* 欄位序列化/no-op 比對遺漏、零依賴專案永遠無法部署。

### Main Changes

(Add details)

### Git Commits

| Hash | Message |
|------|---------|
| `87a736b` | (see git log) |
| `8a8cb8c` | (see git log) |
| `c9d9d80` | (see git log) |
| `6739ea1` | (see git log) |
| `c125cd9` | (see git log) |

### Testing

- [OK] (Add test results)

### Status

[OK] **Completed**

### Next Steps

- None - task complete


## Session 12: 修復 Phase 0-5 驗證確認的 FAIL/MISSING 缺口（B/C.1/C.2/A）

**Date**: 2026-07-02
**Task**: 修復 Phase 0-5 驗證確認的 FAIL/MISSING 缺口（B/C.1/C.2/A）
**Branch**: `feat/mcp-resolve-deploy`

### Summary

修復 req-lk-007 checkout 驗證缺口（含 raw commit SHA 無法傳給 git clone --branch 的關鍵缺陷，由 advisor 於 codex exec 額度用盡時發現並經實測重現確認）；修正 antigravity 自動偵測被誤排除、copilot 偵測訊號過寬兩項 target 偵測缺陷；新增 apm update 指令（req-rs-011/012、req-lk-010），過程中 codex review 額外揪出 frozen 拒絕副作用順序、CI 自動 frozen 缺少 override、apm_modules 清除路徑跳脫等 3 項安全性缺陷並修正。C.3（minimal fallback）因屬新增行為而非修 bug、且兩次使用者確認皆逾時無回應，依慣例採用 Recommended 選項 descoped 至後續獨立任務。

### Main Changes

(Add details)

### Git Commits

| Hash | Message |
|------|---------|
| `ffcd034` | (see git log) |
| `e59780b` | (see git log) |
| `d8c94eb` | (see git log) |
| `6cc825c` | (see git log) |

### Testing

- [OK] (Add test results)

### Status

[OK] **Completed**

### Next Steps

- None - task complete


## Session 13: codex 複核揪出 apm_modules 路徑跳脫防護的其餘缺口並修復

**Date**: 2026-07-02
**Task**: codex 複核揪出 apm_modules 路徑跳脫防護的其餘缺口並修復
**Branch**: `feat/mcp-resolve-deploy`

### Summary

使用者要求對已完成並封存的 07-02-conformance-fail-fixes 工作再送一輪 codex exec 複核，發現先前只在 cmd/apm/update.go 修的路徑跳脫防護（manifest 的 RepoURL/VirtualPath 未過濾 .. 導致刪除範圍跳脫或誤中 sibling 套件目錄），其實源頭在更底層、觸及範圍更廣的 internal/gitops/clone.go（LoadPackage 的 req-lk-007 stale checkout 修復，一般 install 就會走到）與 internal/registry/loader.go（registry 套件解壓）兩處完全沒有防護。抽出共用的 archive.ContainedKey 取代三處各自為政的防護邏輯，經第 2 輪 codex 複核確認修正正確、全專案無其他遺漏。

### Main Changes

(Add details)

### Git Commits

| Hash | Message |
|------|---------|
| `dec6d55` | (see git log) |

### Testing

- [OK] (Add test results)

### Status

[OK] **Completed**

### Next Steps

- None - task complete


## Session 14: 修正 --skill 子集部署的全域過濾範圍錯誤

**Date**: 2026-07-02
**Task**: 修正 --skill 子集部署的全域過濾範圍錯誤
**Branch**: `feat/mcp-resolve-deploy`

### Summary

req-pr-001/req-tg-003 驗證時發現 apm install --skill 的過濾範圍是整個依賴圖而非目標套件,會誤傷 local skills 與其他已裝依賴;修正為以 dep key 限定範圍(deploy.SkillFilter),並新增 fail-loud 防護(--frozen 併用、未給套件、套件因判重未解析進圖時皆明確報錯而非靜默無效)。三輪 codex exec 審查各發現一批殘留問題並修正,最終確認乾淨。

### Main Changes

(Add details)

### Git Commits

| Hash | Message |
|------|---------|
| `2774ebc` | (see git log) |

### Testing

- [OK] (Add test results)

### Status

[OK] **Completed**

### Next Steps

- None - task complete


## Session 15: apm install --mcp CLI 旗標

**Date**: 2026-07-03
**Task**: apm install --mcp CLI 旗標
**Branch**: `feat/mcp-resolve-deploy`

### Summary

實作 apm install --mcp NAME [flags],對齊 Python 原版行為,修正使用者回報的 unknown flag: --mcp 問題。新增 internal/mcpregistry(MCP Registry v0.1 client)、cmd/apm/mcpinstall.go(三分支:自訂 stdio/自訂 url/registry 查詢)、install.go 旗標串接、manifest.ValidateMCP 憑證與 placeholder 驗證強化、deploy 層部署後憑證再檢查。經 11 輪 codex 唯讀審查修正 31 個真實問題(集中在憑證外洩防護、identity 一致性、flag 判斷邏輯),加 1 輪人工複審收斂。go build/vet/gofmt/test -cover 全綠;對照真實 Python 原版的 A/B 測試 15/15 PASS(含使用者原始回報指令),測試腳本依使用者要求移至 repo 外部 D:\Projects\apm-dev\evals。

### Main Changes

(Add details)

### Git Commits

| Hash | Message |
|------|---------|
| `e2848f6` | (see git log) |
| `8e19324` | (see git log) |

### Testing

- [OK] (Add test results)

### Status

[OK] **Completed**

### Next Steps

- None - task complete


## Session 16: 修正 --mcp 寫入格式破壞與 registry-backed 一般安裝落差

**Date**: 2026-07-03
**Task**: 修正 --mcp 寫入格式破壞與 registry-backed 一般安裝落差
**Branch**: `feat/mcp-resolve-deploy`

### Summary

使用者在 D:\Projects\apm-dev\evals\demo\apm.yml 實測時發現兩個真實問題:(1) apm install --mcp 寫回 apm.yml 時,yaml.NewEncoder 的 80 字元自動換行把整份文件(含無關內容)重新格式化甚至截斷字串資料;(2) apm.yml 宣告的 registry-backed mcp 項目在一般 apm install(非 --mcp 旗標)時被無條件跳過,不符 Python 原版行為。修正:internal/yamlcore 新增 PatchMappingPath 做位元級別手術式 YAML 寫入(只換動實際變更的 mapping 路徑,其餘 bytes 完全保留),trellis-check 審查另外抓出並修正 CRLF 換行混用與註解遺失兩個邊界案例;internal/mcpregistry 新增 ResolveDeployable 共用解析邏輯,internal/deploy/mcpcollect.go 接上後一般 apm install 也能正確解析並部署 registry-backed MCP 項目。全程以真實 demo 專案與 A/B 測試(15/15 PASS)驗證。

### Main Changes

(Add details)

### Git Commits

| Hash | Message |
|------|---------|
| `45dc394` | (see git log) |
| `79c26fe` | (see git log) |

### Testing

- [OK] (Add test results)

### Status

[OK] **Completed**

### Next Steps

- None - task complete


## Session 17: marketplace 生態系收尾:4 個實測 bug 修正 + 歷史整併(19→9)

**Date**: 2026-07-05
**Task**: marketplace 生態系收尾:4 個實測 bug 修正 + 歷史整併(19→9)
**Branch**: `feat/marketplace-install`

### Summary

Sonnet subagent 實作/Fable 查核模式修 4 個實測 bug:(1)browse 改用 Python 原版 rich 樣式表格並修執行檔名提示;(2)全域掃除 18 個 user-facing 站點硬編碼的 apm→apm-go;(3)req-tg-003 讓 skills 只進 .agents/skills 導致 Claude Code 讀不到,claude target 額外複製一份到 .claude/skills(Copy 非 symlink),並修跨 target skill 判重先跳過導致複本遺失的地雷;(4)診斷 update DietrichGebert/ponytail 查無非 bug(對齊 Python get_marketplace_by_name alias-only 語意),依使用者選擇只改善錯誤訊息(basename 建議+已註冊清單)。最後將 branch 19 commit 整併為 9 個 phase-based commit:發現原 cb1ff75 是跨 M2/M3/M4 的修編譯 fixup,按檔案拆進對應 phase,達成每個 commit 均可獨立編譯(優於原歷史)+ final tree 與 backup 逐位元組相同。備份分支 backup/pre-squash-marketplace-install 保留。

### Main Changes

(Add details)

### Git Commits

| Hash | Message |
|------|---------|
| `93c08b4` | (see git log) |
| `3c47e91` | (see git log) |
| `bdbe9b2` | (see git log) |
| `4d3305d` | (see git log) |

### Testing

- [OK] (Add test results)

### Status

[OK] **Completed**

### Next Steps

- None - task complete


## Session 18: runtime parity 缺口:建父/子 task + 完成 opencode-mcp(1/3)

**Date**: 2026-07-05
**Task**: runtime parity 缺口:建父/子 task + 完成 opencode-mcp(1/3)
**Branch**: `feat/marketplace-install`

### Summary

確認 apm-go 相對 Python 原版三個 runtime 缺口(uninstall/opencode-mcp/antigravity),建父 task 07-05-runtime-parity-gaps + 三 child,派三個 Sonnet research 子代理蒐證(反向清理可行性靠 LockedDep.DeployedFiles、opencode MCP 格式、antigravity 分歧)。完成第一個 child opencode-mcp:新增 mcp_opencode.go 讓 opencodeAdapter 寫 opencode.json 的 mcp key(對照 Python _to_opencode_format:stdio command 單陣列+environment、remote 統一 url、enabled),補 managedMCPKeys。真機 smoke + A/B 對照真實 Python 8/0 逐欄 key+值 parity。antigravity 使用者拍板對齊 Python 改 explicit-only(待做)。

### Main Changes

(Add details)

### Git Commits

| Hash | Message |
|------|---------|
| `a645e64` | (see git log) |
| `93e4b29` | (see git log) |
| `665743c` | (see git log) |

### Testing

- [OK] (Add test results)

### Status

[OK] **Completed**

### Next Steps

- None - task complete


## Session 19: install --mcp parity: #1 block style / #2 憑證互動 / D2 confirm / M8 per-target header + A/B

**Date**: 2026-07-07
**Task**: install --mcp parity: #1 block style / #2 憑證互動 / D2 confirm / M8 per-target header + A/B
**Branch**: `feat/marketplace-install`

### Summary

補齊 install --mcp 對 Python 原版的缺口：#1 apm.yml block style、#2 registry 憑證互動詢問、D2 衝突 confirm 三態、M8 各 target header 憑證改保留變數(claude ${VAR} / opencode {env:VAR} / codex bearer_token_env_var，不烘明碼 secret)。A/B(ab_mcp_install_parity.py) 14/15 PASS 並揪出 canPromptCreds 少檢查 stdout TTY 的 bug(已修)。全庫 build/vet/test 綠、覆蓋率 83%。

### Main Changes

(Add details)

### Git Commits

| Hash | Message |
|------|---------|
| `706faf6` | (see git log) |
| `a907697` | (see git log) |
| `0f34fb2` | (see git log) |
| `f20dcc2` | (see git log) |
| `0daddd1` | (see git log) |

### Testing

- [OK] (Add test results)

### Status

[OK] **Completed**

### Next Steps

- None - task complete
