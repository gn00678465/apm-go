# 補齊 runtime 對 Python 原版的缺口

## Goal

補齊 apm-go 相對 microsoft/apm（Python 原版）的三個 runtime 缺口：`uninstall`
指令、`install --mcp` 的 opencode 設定、antigravity 各種設定。以 Python 原版
為對照基準（比照 marketplace 生態系的做法，實作後以 A/B 對照 `uv run apm`）。

## 背景（已 confirm 的缺口，附證據）

1. **缺 `uninstall` 指令**
   - apm-go `cmd/apm/main.go` root 未註冊 uninstall。
   - Python 有 `commands/uninstall/cli.py`：`uninstall <pkg...>` + `--dry-run` /
     `-v` / `-g`；語意=從 apm.yml + apm_modules 移除並 sync 各 target 整合。
2. **`install --mcp` 缺 opencode**
   - apm-go 實作 `MCPTarget`/`WriteMCP` 的只有 claude/codex/copilot/antigravity；
     `opencodeAdapter` 無 MCP 能力。
   - Python `adapters/client/opencode.py`：寫專案根 `opencode.json` 的 `mcp` key
     （非 `mcp.json`），且僅在 `.opencode/` 存在時才寫。
3. **antigravity 設定需研究**（研究已完成,見 07-05-antigravity-research/research/）
   - apm-go antigravity adapter 已覆蓋 rules/skills/hooks/mcp_config.json。
   - Python 宣告完整面：`AGENTS.md + .agents/rules/ + .agents/skills/ +
     .agents/hooks.json + .agents/mcp_config.json`（alias `agy`）。
   - **已查證的真實分歧**：Python `target_detection.py:393`
     `EXPLICIT_ONLY_TARGETS={"agent-skills","antigravity"}` 明確是 explicit-only
     （理由：`.agents/` 是跨工具共用目錄,無 antigravity 專屬目錄可偵測）;但
     apm-go `adapter.go:79-90` **auto-detect** antigravity。此分歧需在 child 定案
     （apm-go 的偏離是否正確——「IDE runtime 會讀 AGENTS.md」不等於「apm 該自動
     部署到 antigravity」,因 AGENTS.md 也被 opencode/agent-skills 共用）。
   - **研究另發現疑似 bug**：mcp_antigravity.go 對 sse transport 寫 `url`,但官方
     文件(2026-07-05 抓取)稱 sse 僅 `serverUrl` 合法;Python 端則寫 `url`/`httpUrl`。
     三方不一致,child 須驗證定案。

## Requirements

- 三項各自對照 Python 原版達成行為 parity（例外項須明確記錄為 deviation）。
- 不破壞既有 deploy/install 行為與既有測試；沿用現有 adapter/註冊模式。
- 每項完成後補 A/B 對照腳本（放 `D:\Projects\apm-dev\evals`，比照 marketplace 慣例）。
- 安全性：uninstall 的檔案刪除須「只刪自己裝的」，不得誤刪使用者手動檔案。

## 子任務（task map）

| child | 交付 | 型態 |
|---|---|---|
| 07-05-uninstall | `apm-go uninstall <pkg...>` + `--dry-run`/`-v`/`-g` + 反向清理 | 實作(複雜) |
| 07-05-opencode-mcp | `opencodeAdapter` 加 `WriteMCP`,寫 `opencode.json` mcp | 實作(中小) |
| 07-05-antigravity-research | 研究報告:設定面 + explicit-only 分歧定案 + 缺口清單 | 研究先行 |

排序建議：antigravity-research（研究，釐清分歧）可先行或平行；opencode-mcp 最單純可快速補齊；uninstall 最複雜（反向清理）最後。實際排序見各 child implement.md。

## Acceptance Criteria（跨 child）

- [ ] `apm-go uninstall` 存在且對照 Python 行為 parity（A/B 通過，deviation 記錄）
- [ ] `install --mcp` 在 opencode target 產生 `opencode.json` mcp 區塊（格式對照 Python）
- [ ] antigravity 研究報告產出：設定面完整清單、explicit-only 分歧定案、apm-go 缺口與修正建議
- [ ] 全 repo `go build/vet/test ./...` 全綠;新功能測試覆蓋 ≥ 80%
- [ ] 三個 child 各自可獨立驗收、archive

## Non-Goals

- 不新增 Python 原版沒有的功能。
- 不處理 antigravity 以外的新 target（cursor/gemini/windsurf/kiro 等本輪不碰）。
- uninstall 的 `-g/--global` 若牽涉 user-scope 複雜度過高,可標 MVP 後續（於 child PRD 定）。
