# CLI 全指令面 parity 盤查

## Goal

一次性系統盤查 apm-go（13 指令）對 Python 原版（33 指令）的**全指令表面**，
產出分類齊全的缺口登記冊，終結「使用者逐一踩雷才發現不相符」的狀態。
本任務是**盤查（audit），不是修復**——修復依登記冊 triage 另開 task。

## 觸發背景（2026-07-12 使用者連續踩雷）

1. `codex agents` byte-copy 假 TOML（已修，07-12-codex-agent-toml）。
2. `pack` 同名異義：Python = plugin bundle 打包（SBOM/license 警告、
   plugin.json 合成、build/ 輸出、內嵌 lockfile）；apm-go = 從
   `marketplace:` block 產 marketplace.json——同名完全不同功能，
   使用者實跑輸出天差地遠。
3. 主 session 抽查另見 `audit` 同名異義：Python = 隱藏 Unicode 掃描；
   apm-go = lockfile 完整性重驗。

## 分類法（登記冊的判定欄）

| 類別 | 定義 | 風險 |
|---|---|---|
| DIVERGENT-SAME-NAME | 同名指令行為不同 | **最高**（靜默誤導） |
| MISSING | Python 有、apm-go 無 | 中（unknown command，可見） |
| EXTENSION | apm-go 有、Python 無 | 低（須為 documented extension） |
| PARTIAL | 同名同義但 flag/行為子集或偏差 | 中 |
| PARITY-VERIFIED | 已有 checklist/A-B 佐證 | — |
| COVERED-ELSEWHERE | 已由既有 conformance 清單涵蓋 | — |

## Requirements

- 兩邊全指令 × 全 flag 枚舉（help + 原始碼 file:line），逐指令判類別。
- 同名指令必做行為對照（scratch live probe + 原始碼），DIVERGENT 須附
  兩邊實跑 transcript。
- 已涵蓋面不重掃：install/uninstall/marketplace 75 項清單（conformance/
  cli-verification-checklist.md）、compile（07-11 child）、update local-deps
  + 零 target 閘門（07-11 child）——登記冊標 COVERED-ELSEWHERE 引用即可。
- 產出：`.trellis/spec/evals/cli-surface-parity-register.md`
  （入版控；2026-07-12 由 gitignored 的 `.trellis/spec/conformance/` 遷移至此），
  逐指令一列：類別/證據（file:line + transcript）/嚴重度/
  處置建議（修 parity / documented extension / 記錄不做 / 另開 task）。
- 安全鐵則：對有狀態指令（publish/self-update/config/approve/deny/cache
  清除/marketplace 寫入）**只做 help + 原始碼 + 唯讀探測**，絕不實跑
  會改真實狀態的操作；live probe 一律 TEMP scratch。

## Acceptance Criteria

- [x] 登記冊涵蓋兩邊全部指令（Python 32+apm-go 13 聯集 36 名，無遺漏）
      【codex 抽驗以兩邊 --help 實跑逐一核對；PRD 標稱 33 與實數 32 差 1
      已記入登記冊 §0.3 caveat】
- [x] 每個同名指令有行為對照結論與證據；DIVERGENT 項附兩邊 transcript
      【6 項 DIVERGENT 全數由 codex 於 scratch 獨立重現】
- [x] 全部 MISSING/EXTENSION/PARTIAL 有嚴重度與處置建議
      【類別統計：DIVERGENT 6 / MISSING 23 / EXTENSION 4 / COVERED 4】
- [x] codex 對登記冊做一輪對抗性抽驗
      【VERDICT: PASS-with-corrections 6 處（runtime/self-update 補判、
      章節交叉引用修正等），修正已落檔登記冊「codex 抽驗記錄」節】
- [x] 登記冊落檔：已產出並移至 .trellis/spec/evals/cli-surface-parity-register.md
      （539 行）。**decision（2026-07-12）**：不修改 `.gitignore` 對
      `conformance/` 的既有規則（`cli-verification-checklist.md`/
      `openapm-v0.1.md` 兩份既有檔案的本機專用慣例維持不變），改把本登記冊
      移至新目錄 `.trellis/spec/evals/`（`git check-ignore` 已驗證不受忽略）
      以達成入版控；登記冊本身新增 Living-Doc 章節，更新規則見預防機制
      提案 #5 / `oracle-parity-gates.md` Gate 5
- [x] **流程根因分析（RCA，2026-07-12 使用者追加）**：回溯已知缺陷
      （pack 同名異義、audit 同名異義、codex agents byte-copy）各自源頭
      任務的 prd/design/implement/research 文件與 git 歷史，回答：
      (a) 缺陷在流程哪一步進來（研究漏了什麼？AC 缺了什麼 gate？）
      (b) 為什麼既有驗證（review gate/checklist/A-B）沒攔住
      (c) 系統性預防機制提案（可落地為 spec 準則 / PRD 模板 gate /
      登記冊 living-doc 更新規則），確保新功能不再重演
      【research/root-cause-analysis.md：三案例 file:line 時間軸 + 共通模式
      「Reused-Name Blind Spot」+ 提案 1-5】
- [x] RCA 結論與預防機制經使用者確認後落檔 .trellis/spec/（另立
      guideline 或併入既有 guides）
      【落檔為 `.trellis/spec/guides/oracle-parity-gates.md`（5 個 gate，逐項
      對應提案 1-5），並在 `.trellis/spec/guides/index.md` 加入索引與觸發
      條件；PRD 模板（`task_store.py` 的通用 `_default_prd_content`）與
      `trellis-brainstorm`/`trellis-before-dev` skill 本體判斷為框架層
      共用檔案（`trellis update` 管理、影響所有任務類型），維持不改，改由
      guides/index.md 的既有「Quick Reference: Thinking Triggers」機制承接
      觸發——與 cross-layer/code-reuse 兩份既有 guide 採用同一整合點，
      避免把 CLI-parity 專屬檢查塞進所有任務都會看到的通用模板】

## Non-Goals

- 本任務不修任何缺口（含 pack）——修復依 triage 另開。
- 不掃 MCP registry 生態外部服務行為。
