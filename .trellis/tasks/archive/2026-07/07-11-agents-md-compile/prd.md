# AGENTS.md compile 生成

## Goal

定案並（若拍板做）補齊 apm-go 缺失的 AGENTS.md compile 步驟：把已安裝套件的
instructions 編譯進 `AGENTS.md`，對照 Python `compilation/agents_compiler.py`。

## 背景（見 archive/2026-07/07-05-antigravity-research/research/antigravity-settings.md B.4）

- Python：`should_compile_agents_md` 清單含 antigravity（`compile_family=
  "agents"`，targets.py:685）——顯式選中 antigravity 時 `apm compile` 會把
  instructions 編譯進 `AGENTS.md`；另有 vscode 系的
  `can_dedup_agents_md_instructions` 去重/合併邏輯。
- apm-go：**完全沒有 compile 步驟**。全 repo `AGENTS.md` 只出現在偵測訊號
  用途（detect.go 等 4 檔）；`cmd/apm` 無 `compile` 指令、`internal` 無對應
  `agents_compiler` 套件。「這一塊是空白，不是做了但不同」。
- 07-05-antigravity-research 與 07-05-runtime-parity-gaps 均列為 Non-Goal
  （結構性大工程），拍板記錄為後續 task——即本 task。

## Requirements

- **研究先行**：先定案範圍再動工。至少回答：
  1. Python `apm compile` 的完整行為面（觸發時機、輸入來源、compile_family
     分流、去重/合併、marker 區塊格式、idempotency）。
  2. apm-go 是否全面 parity，或先做 antigravity/agents family 的最小子集。
  3. 與現行 install/deploy 流程的關係（compile 是獨立指令還是 install 附帶）。
- 定案若為「本輪不做」：留決策記錄與依據即可驗收（比照缺口清單處置慣例）。
- 定案若為「做」：以 Python 為 oracle 的 A/B 對照腳本（evals/）為驗收基準；
  不破壞既有 deploy/install 行為。

## Acceptance Criteria

- [x] 研究報告：Python compile 行為面完整清單 + apm-go 落地範圍拍板記錄
      【research/findings.md：行為面全清單（file:line）+ 三選項拍板分析，
      定案選項 B（最小 agents-family 子集）；含 DOC-03 官方文件來源衝突裁定】
- [x] （若做）`apm-go compile` 產出與 `uv run apm compile` A/B 對照通過，
      deviation 記錄【ab_agents_compile.py 49 檢查全綠（raw-byte 比對、
      orphan heading 字面斷言、no-signal deviation 案）；deviations 記於
      spec compile-contract.md】
- [x] （若做）unit 覆蓋 ≥ 80%；全 repo build/vet/test 綠
      【internal/compile 90.7%；18 套件全綠】
- [x] spec 更新或決策記錄落檔（.trellis/spec/backend/）
      【新 spec compile-contract.md（commit `7cb779a`）+ index.md 與
      antigravity-target-contract.md 交叉引用】

## 完成記錄（2026-07-11）

- 實作 commit：`7cb779a`（internal/compile 新套件 + cmd/apm compile 指令）。
- 拍板：選項 B 全採 research 建議——targets = antigravity/codex/opencode
  （同一份根 AGENTS.md，順帶補 codex/opencode instructions 黑洞）、獨立指令、
  覆蓋策略對齊 oracle、v1 flags 僅 -t/--target。
- 硬性 checklist（50 項）codex 對抗性驗證三輪：48/2 → 49/1 → 50/50。
  過程裁定：CMP-07 條文依 codex 獨立 oracle 重現修訂（all-empty-body 孤立
  heading 為 oracle 實際行為）；QG-02 範圍裁定為 task-scope gofmt gate +
  全 repo 157 檔 CRLF 記 pre-existing 另案。
- ab-script-review 兩輪：read_text() universal-newline 陷阱修正
  （read_bytes() 原始位元組斷言）+ 辨識力自測（合成缺 heading 必 FAIL）。
- repo 根 AGENTS.md 全程未觸碰（三輪皆 git diff 取證）。
- follow-up（另案）：全 repo 157 檔 CRLF/gofmt 正規化（.gitattributes +
  一次性 gofmt），見 checklist QG-02 記錄。

## Non-Goals

- 不在本 task 順手改 instructions 部署（applyTo parity 已由 07-11 child 完成）。
- 不做 user-scope compile。

## Notes

- 結構性大工程：若拍板實作，`task.py start` 前須補 `design.md` +
  `implement.md`，並考慮再拆 child。
