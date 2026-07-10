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

- [ ] 研究報告：Python compile 行為面完整清單 + apm-go 落地範圍拍板記錄
- [ ] （若做）`apm-go compile` 產出與 `uv run apm compile` A/B 對照通過，
      deviation 記錄
- [ ] （若做）unit 覆蓋 ≥ 80%；全 repo build/vet/test 綠
- [ ] spec 更新或決策記錄落檔（.trellis/spec/backend/）

## Non-Goals

- 不在本 task 順手改 instructions 部署（applyTo parity 已由 07-11 child 完成）。
- 不做 user-scope compile。

## Notes

- 結構性大工程：若拍板實作，`task.py start` 前須補 `design.md` +
  `implement.md`，並考慮再拆 child。
