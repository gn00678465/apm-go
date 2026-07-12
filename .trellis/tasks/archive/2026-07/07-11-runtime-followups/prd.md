# runtime-parity-gaps 殘留 follow-up 收整

## Goal

收整 07-05-runtime-parity-gaps 父任務（archive/2026-07/）確認記錄中的 4 項殘留
follow-up，各開 child 獨立驗收。本父任務只負責 task map、跨 child AC 與最終
整合確認，不直接承載實作。

## 背景（來源證據）

4 項皆出自 `archive/2026-07/07-05-runtime-parity-gaps/prd.md` 的
「父任務確認記錄（2026-07-10）」：

1. **plugins bundle 部署** — 07-05-antigravity-research 拍板另開 task
   （該 prd 缺口清單：「plugin bundle 部署 → 另開 task」；hooks.json 覆蓋
   不合併缺口隨 plugin 路線一併評估）。
2. **AGENTS.md compile 生成** — 原列 Non-Goal 的結構性大工程；apm-go 完全
   沒有 compile 步驟（antigravity-settings.md B.4）。
3. **存活 local root key 空間不一致** — `uninstallRemainingRootKeys` 對存活
   local root 仍用 `local:` 空間，與 reachability BFS / stale-MCP 的 `_local/`
   空間脫節（07-05-antigravity-research prd「新 follow-up」段；spec
   `backend/install-marketplace-contracts.md` 亦記錄）。
4. **`apm update` 不 materialize local deps（F1 gap）** — spec
   `backend/install-marketplace-contracts.md` §4 Warning。

## 子任務（task map）

| child | 交付 | 型態 |
|---|---|---|
| 07-11-antigravity-plugins-bundle | `.agents/plugins/<pkg>/` bundle 部署 + hooks 覆蓋缺口處置 | 實作(中大) |
| 07-11-agents-md-compile | compile 可行性/範圍定案 → 分階段實作或記錄不做 | 研究先行(大) |
| 07-11-local-root-key-space | key 空間翻譯修復 + 回歸測試 | 實作(輕量) |
| 07-11-update-local-deps | update 行為對照 Python 定案 → 修復或 documented deviation | 實作(中) |
| 07-12-codex-agent-toml | codex agents MD→TOML 轉換 parity（live 驗證追加發現） | 實作(輕量) |

排序建議：local-root-key-space（一行修）先行；update-local-deps 次之（需先
A/B 對照定案 scope）；plugins-bundle 中大型；agents-md-compile 最後（研究
先行，可能拍板不做）。

## Acceptance Criteria（跨 child）

- [ ] 4 個 child 各自獨立驗收、archive（拍板「記錄不做」也算驗收，須留決策記錄）
- [ ] 實作類 child 完成後既有 A/B 腳本（evals/）重跑無回歸
- [ ] 全 repo `go build/vet/test ./...` 全綠；新功能測試覆蓋 ≥ 80%
- [ ] 涉及契約變更者更新 `.trellis/spec/backend/` 對應 spec
      （install-marketplace-contracts.md、antigravity-target-contract.md）

## Non-Goals

- 不新增 Python 原版沒有的功能（antigravity plugins/agents 等 documented
  extension 除外，須留決策記錄）。
- user/global scope 部署（全 repo 性缺口，非本輪）。
