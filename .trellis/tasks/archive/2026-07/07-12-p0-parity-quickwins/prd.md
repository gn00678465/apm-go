# P0 parity quick wins（登記冊 §5）

## Goal

執行 `.trellis/spec/evals/cli-surface-parity-register.md` §5 的 6 個 P0 項：
全部是低成本、立即消除「靜默誤導」風險的修正。使用者已核准開工
（2026-07-12，於預防機制落檔後）。

## 背景

來源 = parity 登記冊 P0 表（每項的完整證據見登記冊對應節與
07-12-cli-surface-parity-audit 的 research/group-*.md）。
本 task 適用 `.trellis/spec/guides/oracle-parity-gates.md` 五道防線
（第一個試用案例）。

## 六項工作（= 登記冊 §5 P0 表）

| # | 項目 | 動作 | 型態 |
|---|---|---|---|
| 1 | `init.go:162` 虛假 `run` 提示 | 刪除或改寫該行，不承諾不存在的指令 | 碼(1 行) |
| 2 | `pack` 靜默無輸出 | `dependencies:`/`target:` 存在但無 `marketplace:` 時印警告（非靜默 nothing-to-do）；`--help` 註明 apm-go pack 的範圍限定（≠ Python pack 的 plugin bundle 打包） | 碼+文 |
| 3 | `audit` 語意落差 | `--help` 與 spec 明確記錄「apm-go audit（SHA 完整性重驗）≠ Python apm audit bare（Unicode 掃描）」 | 碼(help)+文 |
| 4 | `approve`/`deny` 靜默失效 | apm.yml 含 `allowExecutables` block 時印警告「此區塊在 apm-go 尚不生效」 | 碼 |
| 5 | `update --dry-run` | 補 flag：印 update plan（沿用 printUpdateSummary 輸出）後結束，不 materialize、不部署、不寫 lockfile；語意對照 Python plan-render（register §2/§3.3 D-1） | 碼 |
| 6 | `normalize`/`validate` 文件缺口 | .trellis/spec/backend/ 補記兩者為 dev-only CLI 化工具（documented extension），澄清 `validate` 與 `marketplace validate NAME` 撞詞但不同範疇 | 文 |

## Requirements

- 每項碼變更走 TDD；警告/help 文案有測試鎖定（防措辭漂移沿用 errNoDeployTarget 先例）。
- oracle-parity-gates Gate 2 適用：#2/#5 的驗證必含「被排除輸入」案例
  （pack：有 deps 無 marketplace；dry-run：確證零檔案系統效果——lockfile/
  apm_modules/target 目錄 byte-identical）。
- #4 警告是提示不是閘門：不改變部署行為（deny-by-default 移植是 P1 #17 的
  範圍，本項只消除「靜默失效」）。
- 全 repo build/vet/test 綠；觸碰檔 gofmt 乾淨；相關 A/B 腳本無回歸。
- 登記冊 living-doc 義務（Gate 5）：完成後更新登記冊對應列的處置狀態。

## Acceptance Criteria

- [x] 6 項各自完成且有測試/文件證據（checklist 逐項驗證）
      【commit `845944c`；文案以測試側獨立字面常數鎖定】
- [x] `update --dry-run`：plan 輸出正確 + 零副作用證明（前後檔案系統
      snapshot 比對）【throwaway scratch ModulesDir；e2e byte-identical 測試】
- [x] `pack` 警告在「有內容無 marketplace block」情境實跑出現；既有
      marketplace pack 行為無回歸（ab_marketplace_pack.py 14/14）
- [x] 全 repo `go build/vet/test ./...` 綠【18 套件】
- [x] 登記冊 P0 六列狀態更新（含 commit `845944c`；Round B 驗證 PASS）
- [x] codex 硬性 checklist 逐項對抗性驗證全過【兩輪 23/2→25/0，Round B 後 26/26】

## Non-Goals

- 不做 P1/P2 項（consent-gate 互動 confirm、targets: 複數 schema、
  approve/deny enforcement 等均另開）。
- ~~不改 pack/audit 的實際行為語意（只加警告與文件）~~
  **（2026-07-12 使用者裁定廢除此 Non-Goal，改列 Phase 2——見下）**

## Phase 1 完成記錄（2026-07-12）

- 修復 commit：`845944c`（六項警告/文件止血）。checklist 26/26。
- 修復迴圈 1 輪：測試引用產品常數 → 改測試側獨立字面常數（codex mutation 抽測驗證辨識力）。
- **教訓（Gate 6 的由來）**：本階段報告以「26/26 全過」呈現，但使用者實跑
  `pack` 仍打不出 bundle——警告只是止血、完整 parity 未被任何 roadmap 項目
  承接，且報告未先講「此修正不做什麼」。使用者正當質疑後，Gate 6（6a 部分
  處置必排殘餘、6b 使用者情境重播+不做什麼前置）落檔（commit `d3387f8`）。

## Phase 2：pack / audit 完整 parity（2026-07-12 使用者裁定併入本 task）

> 使用者裁定：殘餘工作**排入原本聲稱完成的本 task**，不另開新任務。
> 原 07-12-pack-parity / 07-12-audit-parity 兩個新開任務併回本 task，
> 其 research 產物遷入本 task research/。

### 2a. pack 完整 parity（設計中）

- 移植 Python pack 缺失的兩個 producer：**BundleProducer**
  （`build/<name>-<ver>/` 佈局 + 內嵌 apm.lock.yaml）與
  **PluginManifestProducer**（plugin.json 合成 + `.github/plugin/` +
  `.claude-plugin/`），與既有 marketplace.json producer 並存（觸發矩陣
  依 research 定案）。
- Phase 1 加的兩條 pack 警告在對應路徑實作後移除/改寫。

**範圍決策（2026-07-12，研究揭露隱藏範圍後由使用者拍板）**：
研究（`research/pack-parity-findings.md` §6）發現 apm-go `install` 完全無法
消費 pack 產出的 bundle（零偵測/驗證/部署子系統）。使用者選定
**完整回路**——本 task 涵蓋 pack 兩 producer **加** `install <bundle-path>`
偵測/驗證/部署子系統，達成 apm-go 內部 pack→install 閉環。

次要決策（orchestrator 工程預設，記於 design.md disposition 表）：
- `.mcp.json` reader 新增（Python 的 bundle MCP 來源，非 apm.yml 模型）
- bundle_files hash 用裸 hex（Python 互通格式）
- 隱字掃描本輪延後（與 Phase 2b audit 的 ContentScanner 共用，一起做）
- credsec 檔案級拒收不接線（parity 不超過 oracle）
- nothing-to-do 改 exit 1（同 Python BuildError）
- `kiro` 加入 canonical targets；`target:`/`targets:` 複數+CSV 解析修正
  吸收為前置（登記冊 P1 #7 由本 task 承接）

- **完成標準（Gate 6b）**：使用者 test1 情境重播——`apm-go pack` 與
  `apm pack` 產出語意等價 bundle、**apm-go install 自己能消費**、報告附雙邊
  transcript 與殘餘 deviation 清單，「此修正不做什麼」前置。

### 2b. audit 完整 parity（使用者 2026-07-12 拍板 P1，pack 之後串行）

- 移植 Python bare audit 的隱藏 Unicode 掃描 + drift 偵測；與 apm-go
  既有 SHA 完整性重驗的並存語意（子指令/flag 分流）於 research 定案。

### Phase 2 Acceptance Criteria

- [x] pack 三 producer 觸發矩陣與 oracle 一致（A/B）；bundle 佈局/植入
      lockfile/plugin.json 合成語意等價【test1 雙邊 73 檔樹狀一致；commit `f547b4b`】
- [x] Gate 6b 重播：test1 情境雙邊 transcript 附於報告【`research/gate6b-report.md`；
      install 部署樹 byte-identical 222 檔（PATH_DIFF=0/HASH_DIFF=0）】
- [x] audit Unicode 掃描 parity（A/B）【`--content` exit 0/1/2；ContentScanner A/B
      2 hidden chars 兩邊一致；commit `f2453b8`】
- [x] 全 repo build/vet/test 綠；codex 硬性 checklist 逐項全過【23 套件；codex 三輪
      對抗性驗證 pack/install/audit 全 CONFIRMED（缺口修正後）】
- [x] 登記冊 §3.1/§3.2 兩個 DIVERGENT 項正式關閉【標 ✅ RESOLVED + 19a/19b 完成】
