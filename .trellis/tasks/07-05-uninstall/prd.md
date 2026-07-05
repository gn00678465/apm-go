# 新增 uninstall 指令(移除套件+反向清理各 target)

## Goal

新增 `apm-go uninstall <pkg...>`,對照 Python 原版:從 apm.yml + apm_modules 移除
指定套件,反向清理該套件部署到各 target 的檔案,並更新 lockfile。

研究:`research/uninstall-parity.md`（Python 行為逐項 + apm-go 可重用點 + 難點）。

## 關鍵發現（來自研究,影響範圍界定）

- apm-go **完全沒有 uninstall,且 deploy pipeline 只有 additive copy、無任何刪檔路徑**。
- **可行性關鍵**:`LockedDep.DeployedFiles`/`DeployedHashes` 已在 `install.go:705-717`
  被填入——每個 dep 部署了哪些檔案有精準 provenance,反向清理可據此「只刪自己裝的」。
- **MCP 反向清理是缺口**:lockfile 無 `mcp_servers` 欄位,`mcp_common.go:222-224` 明講
  跨 uninstall 的 stale MCP entry 清理目前 out-of-scope。
- **`-g/--global` 在 apm-go 完全不存在**（無 InstallScope/user-scope 概念,install/update
  都寫死 cwd 相對路徑）。
- 可重用:`resolver/update.go:54-65` 的 `ResolvedBy` fixed-point BFS(對應 Python 的
  orphan 走訪)、`archive/extract.go` 的 `Contained`/`ContainedKey` 路徑安全防護。

## Requirements

- `uninstall <pkg...>`(可多個)+ `--dry-run` + `-v`;對照 Python CLI 語意。
- 移除:apm.yml 對應 dependency 條目 + apm_modules/<pkg> + lockfile 條目。
- **反向清理各 target**:用 `LockedDep.DeployedFiles` 精準刪除該 dep 部署的檔案
  (rules/agents/commands/skills;含 claude 的 `.claude/skills` 複本),刪前以路徑安全
  防護確認在 target root 內、且 hash 比對確認未被使用者手動改動(改動則保留+警告)。
- **transitive orphan**:移除後,僅被移除套件依賴、無其他人依賴的 transitive dep 一併清理
  (對照 Python;用 ResolvedBy BFS)。
- 套件不存在 → 明確錯誤(對照 Python);多套件部分失敗的行為對照 Python。
- 安全性(不可妥協):絕不刪除非本套件部署的檔案、不刪使用者手動建立的檔案。

## Acceptance Criteria

- [ ] `apm-go uninstall <pkg>` 移除 apm.yml 條目 + apm_modules/<pkg> + lockfile 條目
- [ ] 反向清理:該套件部署的 rules/agents/commands/skills 檔案被刪,其他套件的不受影響
- [ ] hash 不符(使用者改過)→ 保留該檔 + 警告,不靜默刪除
- [ ] `--dry-run` 只印將刪清單、零實際變更;`-v` 詳細輸出
- [ ] transitive orphan 清理正確;仍被他人依賴的 dep 保留
- [ ] 套件不存在 → 非零 exit + 清楚訊息
- [ ] A/B 對照 `uv run apm uninstall` 通過(deviation 記錄:見下)
- [ ] `go build/vet/test ./...` 全綠,新測試覆蓋 ≥ 80%

## Non-Goals / MVP 界定

- **`-g/--global` 本輪不做**(apm-go 無 user-scope 基礎,獨立大工程;標為 deviation)。
- **MCP entry 反向清理**:若本輪要做需先給 lockfile 加 `mcp_servers` provenance——
  評估後決定納入或標為後續(design.md 定案)。
- 不新增 Python 沒有的行為。
