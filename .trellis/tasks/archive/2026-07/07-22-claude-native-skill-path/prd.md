# claude target skill 部署改為原生路徑唯一

## Goal

Fix [issue #10](https://github.com/gn00678465/apm-go/issues/10)：claude adapter 安裝 skill 時不再寫 canonical `.agents/skills/`，只寫原生 `.claude/skills/`，對齊 Python upstream 實作與 targets-matrix registry。

## Background（證據）

- 現況：`internal/deploy/claude.go` `deploySkillClaude` 同時寫 `.agents/skills/<name>/` 與 `.claude/skills/<name>/`；target 只有 claude 時多出一個沒有工具會讀的 `.agents/` 目錄，造成使用者混淆（issue #10）。
- Python upstream（`D:\Projects\apm-dev\apm\src\apm_cli\integration\targets.py:513`）：claude 的 skills mapping 無 `deploy_root=".agents"`，只部署 `.claude/skills/`。
- targets-matrix registry（`apm/docs/src/content/docs/reference/targets-matrix.md:23,35`）：claude 的 deploy root 只有 `.claude/`；明文「Claude, Windsurf, and Kiro keep target-native skill directories」。
- req-tg-002：target 不得寫出 registry 註冊 root 之外 → claude 寫 `.agents/` 反而與 registry 衝突。
- 06-29 task 的「Spec wins over Python's claude native path」決定據此推翻（user 決定 2026-07-22，方向 A）。

## Requirements

1. claude adapter 的 TypeSkills 部署只寫 `.claude/skills/<name>/`，不再寫 `.agents/skills/<name>/`。
2. `claudeAdapter.DeployRoots()` 移除 `.agents/`（回到 registry 註冊的 `.claude/` 唯一）——需先確認 DeployRoots 的所有使用點（uninstall／清理邏輯）不會因此漏刪既有檔案。
3. 多 target 情境不受影響：claude+codex 時 `.agents/skills/` 仍由 codex 等自行寫出；跨 target 的 skill 檔案 dedup 邏輯（`internal/deploy/deploy.go` 中 deployedSkills 註解與行為）同步更新。
4. stdout 部署摘要（`cmd/apm-go/install.go` `deployedFilesTree`）在 claude-only 時只顯示 `.claude/skills/`——由檔案清單自動推導，不需 hack，但需測試驗證。
5. lockfile `deployed_files`/`deployed_file_hashes` 不再記錄 claude 產生的 `.agents/skills/` 路徑。
6. 升級情境：既有專案已存在由舊版寫出的 `.agents/skills/`，重新 install 後由 stale-file 清理機制處理或至少不報錯（確認 uninstall/update 對舊路徑的行為，不得 hash 驗證爆錯）。

## Out of scope

- `--legacy-skill-paths` / `APM_LEGACY_SKILL_PATHS` opt-out 開關（方案 B，另案處理）。
- windsurf / kiro target（apm-go 尚未實作）。

## Acceptance Criteria

- [ ] `target: [claude]` 安裝含 skill 的套件後，專案內只有 `.claude/skills/<name>/`，無 `.agents/skills/`。
- [ ] `target: [claude, codex]` 安裝後 `.agents/skills/<name>/`（codex）與 `.claude/skills/<name>/`（claude）皆存在。
- [ ] claude-only 時 stdout 摘要顯示 `N skill(s) -> .claude/skills/`，不含 `.agents/skills/`。
- [ ] lockfile 中 claude-only 安裝的 skill 檔案路徑全部以 `.claude/skills/` 開頭。
- [ ] 既有含 `.agents/skills/`（舊版部署）的專案重跑 install / uninstall 不報錯。
- [ ] `go test ./...` 全數通過（含更新後的 `TestSkillConvergence` 等）。
- [ ] `.trellis/spec/` 相關文件（terminal-ux-contract / antigravity-target-contract 等提及 req-tg-003 收斂處）同步更新 claude 例外。
