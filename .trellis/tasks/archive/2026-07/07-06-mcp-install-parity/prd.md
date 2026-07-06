# install --mcp apm.yml 格式與 token 互動 parity

## Goal

補齊 apm-go `install --mcp` 相對 Python 原版（microsoft/apm）的兩個 install 缺口：
1. apm.yml `dependencies.mcp` 寫入格式（flow → block style，對齊原版）。
2. registry-resolved MCP server 需要憑證時的**互動詢問**（原版會 prompt `token` 等
   required 變數，apm-go 目前只印診斷、不詢問）。

對照基準為 Python 原版；apm-go 只部署 remote 端點的既有邊界不變。

## 背景（已驗證，詳見 research/findings.md）

- **#1** 根因：`init.go` 寫出 `mcp: []`（flow style），install 重讀時 SequenceNode
  繼承 flow，append 後仍 flow。清掉該序列的 FlowStyle 即與原版 block 一致（已實測）。
- **#2** 原版於 registry 解析後彙整 server 宣告的 required 變數並互動詢問（含 secret
  隱藏輸入、沿用既有環境變數、CI/E2E 跳過）。apm-go `resolveFromRegistry` 只拿到
  header 名稱並印診斷，不詢問、不注入。
- **#3**（非本任務範圍）`--header requires --url` 原版 E9 亦如此，apm-go 已一致，
  不改碼；補 #2 即涵蓋「registry server 帶 token」需求。

## Requirements

### R1 — apm.yml 寫成 block style（#1）
- `apm install --mcp <name>` 新增/取代 `dependencies.mcp` 條目後，該序列以 block style
  輸出（每項一行 `- ...`），與原版一致。
- 僅影響 `dependencies.mcp` 序列；apm.yml 其他位元組（含 `dependencies.apm`、註解、
  手工排版）維持不變（沿用既有 `PatchMappingPath` surgical patch 精神）。
- bare-string / mapping 兩種條目型態皆正確 block 化。

### R2 — registry 憑證互動詢問（#2）
- 安裝 registry-resolved MCP server 時，若該 server 宣告了 required 且尚未提供的憑證，
  在互動式 TTY 詢問使用者輸入；secret 類（token/key/secret/password/api）隱藏輸入。
- 已存在於環境變數者沿用、不詢問；非 required 者不阻擋安裝。
- **非互動情境**（非 TTY、CI/E2E env、或已用 `--header` 提供）**不 prompt**，維持既有
  診斷輸出，安裝不被阻塞。
- 詢問到的憑證需實際套用到部署（deploy 到各 target 的 MCP 設定），使安裝後可用。
- apm.yml 的持久化策略須避免將明碼 secret 寫入被 commit 的 apm.yml（詳見 design.md 定案）。

### R3 — 衝突條目互動 confirm-replace（D2，使用者 2026-07-06 納入）
- 既有條目與新值不同且未帶 `--force` 時，行為對齊 Python 原版 `writer.py` 三態：
  - `--force`：靜默取代（沿用既有）。
  - **互動 TTY**：顯示「已存在 + 取代 diff」，詢問 `Replace …? [y/N]`（預設 N）；
    同意→取代，拒絕→**skipped（視同 unchanged：不寫 apm.yml、不 deploy）**。
  - **非互動（非 TTY / CI）**：報錯 "Use --force to replace (non-interactive)"，apm.yml 不動。
- diff 呈現對齊原版 `_diff_entry`（bare↔bare 顯示 `old -> new`；mapping 逐 key，缺值 `<absent>`）。

### 共同約束
- 不破壞既有 `install` / `install --mcp` / deploy 行為與既有測試。
- apm-go「只部署 remote、不支援 package/stdio registry 解析」的既有邊界不變。
- 沿用既有 registry client / resolve / deploy 架構，不新增 Python 原版沒有的功能。
- 安全：不得把使用者輸入的 secret 明碼寫入版本控管檔（apm.yml）。

## Acceptance Criteria

- [ ] AC1：`apm-go install --mcp io.github.github/github-mcp-server`（全新 apm.yml）後，
  `dependencies.mcp` 為 block style；`dependencies.apm` 與其他內容不受影響。
- [ ] AC2：mapping 型態條目（self-defined / registry+version）新增後亦為 block style。
- [ ] AC3：互動 TTY 下安裝需憑證的 registry server 會 prompt，secret 隱藏輸入；輸入後
  憑證套用到部署、安裝成功。
- [ ] AC4：非 TTY / CI / 已提供憑證 情境不 prompt，維持既有診斷、不阻塞（既有測試綠）。
- [ ] AC5：apm.yml 不含明碼 secret（依 design.md 定案的持久化策略驗證）。
- [ ] AC8（D2）：衝突條目在互動 TTY 顯示 diff 並詢問取代（預設 N）；拒絕→不變更；
  非互動→報錯 "non-interactive"、apm.yml 不動；`--force` 仍靜默取代。
- [ ] AC6：`go build ./...`、`go vet ./...`、`go test ./...` 全綠；新增邏輯測試覆蓋 ≥ 80%。
- [ ] AC7：A/B 對照（`D:\Projects\apm-dev\evals`，比照 marketplace 慣例）記錄與原版差異，
  例外項標為 deviation。

## Non-Goals

- 不改 `--header requires --url`（#3，已與原版一致）。
- 不新增 package/stdio registry 解析（apm-go 既有非目標）。
- 不實作原版 github 特例的 `GitHubTokenManager` token 自動取得（如需，另案）。
- 不處理 remote 以外的新 transport / target。
