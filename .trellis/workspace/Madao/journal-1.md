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


## Session 20: install/marketplace CLI 驗證清單 + P0/P1/P2 修復

**Date**: 2026-07-10
**Task**: install/marketplace CLI 驗證清單 + P0/P1/P2 修復
**Branch**: `feat/marketplace-install`

### Summary

4-agent workflow 產出 install/uninstall/marketplace 全 flag 三方比對驗證清單(75 項)。修復 P0 安全 2 項(http:// 依賴閘門、--subdir traversal)、P1 5 項(--target、ref→SHA、--skill '*'、devDeps、local marketplace/絕對路徑 copy 模型)、P2 6 項(verbose/set 防呆/add 驗證/confirm 安全/--mcp url scheme/check dup-name)。每項 sonnet subagent TDD + 主 session 親自 full-suite/安全/live 驗證。獨立 trellis-check 複查揪出並修正 F4 --ref HEAD HIGH bug。新增 backend/install-marketplace-contracts.md 契約 spec。全程 17/17 packages 綠。

### Main Changes

(Add details)

### Git Commits

| Hash | Message |
|------|---------|
| `f4bdcac` | (see git log) |
| `f8d70f3` | (see git log) |
| `a1a07f4` | (see git log) |
| `e079599` | (see git log) |
| `13f1290` | (see git log) |
| `9cc6d51` | (see git log) |
| `07b443d` | (see git log) |
| `e43baf9` | (see git log) |
| `6d2232b` | (see git log) |
| `e718d8f` | (see git log) |
| `0a73736` | (see git log) |
| `0f63f1e` | (see git log) |

### Testing

- [OK] (Add test results)

### Status

[OK] **Completed**

### Next Steps

- None - task complete


## Session 21: antigravity CLI 研究定案、三修正實作與硬性 checklist 驗證

**Date**: 2026-07-10
**Task**: antigravity CLI 研究定案、三修正實作與硬性 checklist 驗證
**Branch**: `feat/marketplace-install`

### Summary

antigravity CLI 專項研究(4 subagent:MCP/Skills/Plugins/Subagents,官方文件+agy 1.0.16 binary 雙軌)定案 serverUrl 與新能力面;Codex 盤查(0C/2H/4M/2L 全採納)後實作三修正:sse->serverUrl(d72dc6a)、explicit-only+agy alias(c6ef3f7,BREAKING)、agents primitive(3471e45,documented extension);agy 實機 6-probe 驗出 rules 非 always-on 與 project 註冊 gotcha;A/B 全 PASS(結構比對+plugin validate+實機發現)。補做硬性 checklist(conformance §7,32 項全勾),live 驗證抓到 local dep uninstall key 脫節真缺陷並修復(171fd87);spec 新增 antigravity-target-contract.md 與 uninstall key 契約(d13b577/8d5c516)。follow-up:存活 local root key 空間不一致、plugins bundle 另開 task。

### Main Changes

(Add details)

### Git Commits

| Hash | Message |
|------|---------|
| `d72dc6a` | (see git log) |
| `c6ef3f7` | (see git log) |
| `3471e45` | (see git log) |
| `7ada7fb` | (see git log) |
| `d13b577` | (see git log) |
| `171fd87` | (see git log) |
| `8d5c516` | (see git log) |

### Testing

- [OK] (Add test results)

### Status

[OK] **Completed**

### Next Steps

- None - task complete


## Session 22: runtime-parity-gaps 父任務確認與 instructions applyTo parity 實作

**Date**: 2026-07-11
**Task**: runtime-parity-gaps 父任務確認與 instructions applyTo parity 實作
**Branch**: `feat/marketplace-install`

### Summary

確認父任務 runtime-parity-gaps 跨 child AC(6/6,3 支既有 A/B 腳本重跑無回歸,補 ab_antigravity.py 23/23)。live A/B 發現 claude instructions applyTo 語意遺失等 6 項分歧,開 child 07-11-instructions-applyto-parity:claude applyTo->paths 轉換(對照 Python 逐 case,04f4e58)、收集過濾僅收 *.instructions.md(配對契約)、零 target 閘門納入 local primitives(ccc2c9d)。四層驗證:unit 18 case、ab_instructions_applyto.py 25/25、Codex full-access 自由驗證 PASS-with-notes、Codex 硬性 checklist 對抗性逐項重驗 19/19 CONFIRMED。follow-up 記錄:MCP-only 閘門、Python 收集含 package root+symlink 過濾、update 閘門。

### Main Changes

(Add details)

### Git Commits

| Hash | Message |
|------|---------|
| `a440b85` | (see git log) |
| `04f4e58` | (see git log) |
| `ccc2c9d` | (see git log) |
| `6556088` | (see git log) |
| `33bcb24` | (see git log) |
| `27328d1` | (see git log) |

### Testing

- [OK] (Add test results)

### Status

[OK] **Completed**

### Next Steps

- None - task complete


## Session 23: runtime-parity-gaps 父任務狀態確認與收官 archive

**Date**: 2026-07-11
**Task**: runtime-parity-gaps 父任務狀態確認與收官 archive
**Branch**: `feat/marketplace-install`

### Summary

確認父任務 07-05-runtime-parity-gaps 完成狀態:5 個 children(uninstall/opencode-mcp/antigravity-research/mcp-install-parity/instructions-applyto-parity)全數 archive/2026-07/,跨 child AC 6/6 全勾附證據(A/B 腳本 4 支重跑無回歸、17 套件綠、覆蓋率達標),working tree clean。無新 code commit,純確認後 archive 父任務。殘留 follow-up 4 項已記錄於父 prd.md(plugins bundle、AGENTS.md compile、local root key 空間不一致、update 不 materialize local deps),待決策另開 task 或記錄不做。

### Main Changes

(Add details)

### Git Commits

(No commits - planning session)

### Testing

- [OK] (Add test results)

### Status

[OK] **Completed**

### Next Steps

- None - task complete


## Session 24: pack/audit 完整 parity 收尾：Phase 6-8 + cmd 目錄改名

**Date**: 2026-07-14
**Task**: pack/audit 完整 parity 收尾：Phase 6-8 + cmd 目錄改名
**Branch**: `feat/marketplace-install`

### Summary

完成 07-12-p0-parity-quickwins 剩餘 Phase：Phase 6 install<bundle-path>消費回路(閉環)、Phase 7 audit --content 隱字掃描、Phase 8 Gate 6b test1 雙邊重播。Gate 6b 揭露並修復 Phase 6 install 重大 parity 缺口(錯用二次-transform 模型只部署 4/72 檔→改寫為 Python verbatim-tree-copy 達 byte-identical 222 檔)。codex 三輪對抗性驗證各抓真實缺口(A6 大寫 metadata/B2 NTFS junction 逃逸/C2 巢狀測試、audit B1/D4)全修並重驗。另將 cmd/apm 目錄改名 cmd/apm-go。登記冊 §3.1/§3.2 pack/audit DIVERGENT 正式關閉。證據:archive/2026-07/07-12-p0-parity-quickwins/research/gate6b-report.md

### Main Changes

(Add details)

### Git Commits

| Hash | Message |
|------|---------|
| `d1bfad8` | (see git log) |
| `7c95c48` | (see git log) |
| `f2453b8` | (see git log) |
| `276c56b` | (see git log) |
| `6585876` | (see git log) |
| `63857c1` | (see git log) |
| `0766389` | (see git log) |
| `440857c` | (see git log) |

### Testing

- [OK] (Add test results)

### Status

[OK] **Completed**

### Next Steps

- None - task complete


## Session 25: 甲類美化收尾:commit 整合、版本 0.2.0、PR #3、codex 終驗

**Date**: 2026-07-16
**Task**: 甲類美化收尾:commit 整合、版本 0.2.0、PR #3、codex 終驗
**Branch**: `feat/init-tui-lipgloss`

### Summary

整合 4 個甲類 commit 為 4ebfb5a feat(ux): 甲類輸出美化（R8/R9/R10/R14/R19）並推送;版本升級 0.2.0（c9e5f8b）;建立 draft PR #3（pterm→lipgloss/huh 遷移 + 甲類美化,完整 body）;codex 對抗式終驗 main...HEAD 完整 diff（high effort）回報無 CRITICAL/HIGH,checklist 機器項全 PASS（ux 覆蓋率 89.8%、X1/X2/A7/B8/B11 grep 清零、go test -count=1 全綠）。殘留:R8/R19 真終端目視驗收待人工;乙類（R7/R11-R18 parity）與丙類（BUG-1）另立子任務未建。

### Main Changes

(Add details)

### Git Commits

| Hash | Message |
|------|---------|
| `4ebfb5a` | (see git log) |
| `c9e5f8b` | (see git log) |

### Testing

- [OK] (Add test results)

### Status

[OK] **Completed**

### Next Steps

- None - task complete


## Session 26: 安全殘留全掃 + HIGH-B 封閉 + 流程護欄落地 + #4/#5 合併

**Date**: 2026-07-17
**Task**: 安全殘留全掃 + HIGH-B 封閉 + 流程護欄落地 + #4/#5 合併
**Branch**: `main`

### Summary

PR #6 分支修完 C2(deploy 符號連結任意讀)/H2/M1(HTTP body LimitReader)/M2(MCP header 明文警示)/M3(YAML 巢狀深度上限);PR #4 分支完整評估並封閉 HIGH-B(resolver 深度守衛,非架構性延後);AGENTS.md §5 收斂性斷言禁令 + workflow.md checklist 護欄與 tripwire(fail-closed)落地;解 #4->#5 depref.go 衝突(union: skills 守衛 + ParseDepString)與 CaseFold 測試改 fixtureLoader 語意衝突;#4/#5 合併至 main 並驗證全綠。

### Main Changes

(Add details)

### Git Commits

| Hash | Message |
|------|---------|
| `a4afe79` | (see git log) |
| `a8418b6` | (see git log) |
| `b402605` | (see git log) |
| `eafab5b` | (see git log) |
| `f583183` | (see git log) |
| `b123802` | (see git log) |
| `ae5bed8` | (see git log) |
| `8dec1ea` | (see git log) |
| `6443464` | (see git log) |

### Testing

- [OK] (Add test results)

### Status

[OK] **Completed**

### Next Steps

- None - task complete


## Session 27: PR #6/#7 收尾:驗證、ready、審查備援補記與合併

**Date**: 2026-07-17
**Task**: PR #6/#7 收尾:驗證、ready、審查備援補記與合併
**Branch**: `main`

### Summary

以 Fable 視角重新梳理四個流程問題:確認推導規則為承重牆、詞表僅偵測器;補 #7 缺口 — 獨立審查目的是失效去相關,codex 不可用時 fallback = fresh-context 同模型反駁式 agent(6a30d8f)。#6(安全批次 C2/H2/M1/M2/M3)與 #7(流程護欄)trial-merge 驗證全綠後轉 ready,均已由使用者合併至 main。

### Main Changes

(Add details)

### Git Commits

| Hash | Message |
|------|---------|
| `6a30d8f` | (see git log) |

### Testing

- [OK] (Add test results)

### Status

[OK] **Completed**

### Next Steps

- None - task complete


## Session 28: release pipeline 全鏈驗證:act 本地 + 真實 tag e2e(裝/測/移除)

**Date**: 2026-07-18
**Task**: release pipeline 全鏈驗證:act 本地 + 真實 tag e2e(裝/測/移除)
**Branch**: `feat/release-install`

### Summary

act 本地跑 workflow 抓到守門 sed CRLF 缺陷並健壯化;使用者於分支推 v0.2.1 tag 觸發真實 Actions(run 29616001903 全綠);release 實測全過:AC1 七資產、AC2 irm 實裝 0.2.1、install.sh WSL 實裝、AC5 竄改 SHA256SUMS 拒收、AC6 兩平台 uninstall 實跑+冪等、D1 302;checklist 全項附證據後於分支上歸檔任務(收尾記錄依新規則隨 PR 合併,不直接進 main)。

### Main Changes

(Add details)

### Git Commits

| Hash | Message |
|------|---------|
| `038fded` | (see git log) |
| `e8a6ba9` | (see git log) |

### Testing

- [OK] (Add test results)

### Status

[OK] **Completed**

### Next Steps

- None - task complete


## Session 29: README 雙語 + 編譯指令文件;PR 未經同意開啟之違規記錄

**Date**: 2026-07-18
**Task**: README 雙語 + 編譯指令文件;PR 未經同意開啟之違規記錄
**Branch**: `docs/readme`

### Summary

docs/readme 分支:英文/繁中 README(指令表自 --help 實抄)+ release 尺寸編譯指令三處文件(實測 17.2MB→12.2MB, -29%)。違規記錄:承諾「內容結構先過目」卻直接 commit+push+開 PR #9,未經審閱;根因為把單步批准擴大成整條鏈(與 version.go 直改、finish-work 直進 main 同型)。修正共識:開 PR 等對外動作一律先取得明確同意。PR #8(release,已驗證)/#9(README,未審)去留由使用者決定。

### Main Changes

(Add details)

### Git Commits

| Hash | Message |
|------|---------|
| `e9f4810` | (see git log) |
| `c0886ac` | (see git log) |

### Testing

- [OK] (Add test results)

### Status

[OK] **Completed**

### Next Steps

- None - task complete
