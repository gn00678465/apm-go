# antigravity plugins bundle 部署

## Goal

讓 apm-go 能把套件以 antigravity plugin bundle 形式部署到
`.agents/plugins/<pkg>/`，並藉 plugin 路線解掉 hooks.json「覆蓋不合併」缺口。

## 背景（研究已完成，見 archive/2026-07/07-05-antigravity-research/research/cli-plugins.md）

- Plugin 是 antigravity 官方的 bundle 單位：「Skills、Rules、Hooks、MCP Server
  Configurations 打包成 single deployable unit」，workspace 放
  `.agents/plugins/<plugin_name>/`。
- Bundle 結構：`plugin.json`（必要，實務上僅 `name` 也合法，甚至欄位可省略
  預設用目錄名）＋ optional `mcp_config.json` / `hooks.json` / `skills/` /
  `agents/` / `rules/`。
- `agy plugin validate` 實機驗證過此結構（07-05 研究 B3 段）。
- **hooks 缺口的關係**：目前 apm-go 對 antigravity 的 hooks 部署是寫共用
  `.agents/hooks.json`，多套件會互相覆蓋不合併（07-05-antigravity-research
  prd 缺口清單「hooks.json 覆蓋不合併 → 隨 plugin task 一併評估」）。plugin
  bundle 每套件一份 `hooks.json`，天然免除合併需求——這是 2026-07-10 拍板走
  plugin 路線的主因。
- 發現優先序（內嵌 Customizations 文件）：Workspace `.agents/` >
  declared（plugins.json）> global `~/.gemini/config/` > built-in。

## Requirements

- `install --target antigravity`（explicit-only 語意不變）將套件部署為
  `.agents/plugins/<pkg>/` bundle：`plugin.json` + 對應 primitives 子目錄。
- hooks 改走 per-plugin `hooks.json`；既有共用 `.agents/hooks.json` 寫入路徑
  的處置（遷移或並存）須定案並記錄。
- uninstall 反向清理 plugin bundle 目錄，維持「只刪自己裝的」不變式。
- Python 原版沒有 plugin bundle 部署——此為 documented extension，偏離須在
  spec 記錄決策依據。
- 既有 ab_antigravity.py（evals/）重跑無回歸；新增 bundle 部署驗證段。

## Acceptance Criteria

- [x] install 產出 `.agents/plugins/<pkg>/plugin.json` + 內容子目錄，
      `agy plugin validate` 實機 PASS
      【agy 1.1.1 對兩個真實生成 bundle validate [ok] exit 0，含 N processed
      精確數量斷言（checklist AGB 系列 + ab_antigravity.py live 段）】
- [x] 兩套件各帶 hooks 同時安裝互不覆蓋
      【per-plugin hooks.json；Go 測試（cross/same-package isolation）+
      ab_antigravity.py 兩 dependency bundles fixture】
- [x] uninstall 完整清理 bundle 目錄且不誤刪使用者手動檔案
      【零特例反向清理（provenance 驅動）；sibling 存活 + 手寫檔存活測試鎖定】
- [x] unit 覆蓋 ≥ 80%；ab_antigravity.py 重跑無回歸
      【internal/deploy 88.5% / cmd/apm 86.1%；ab_antigravity.py 46 斷言全綠
      （腳本經補強：byte-identical 四型、精確 key set、processed 數量）】
- [x] spec 更新：antigravity-target-contract.md 記錄 bundle 佈局與
      documented extension 決策【新增 §7，commit `6a66e07`】

## 完成記錄（2026-07-11）

- 實作 commit：`6a66e07`（BundleTarget 介面 + antigravity bundle 路由 +
  fail-closed 同名碰撞 + 12 新測試）。
- 拍板落地：dependency primitives 進 bundle、local primitives 維持扁平路徑
  （D1）；同名碰撞採 fail-closed（比 design 原稿 diagnostic-only 嚴格，
  research 風險 #4 的處置）；plugin.json 最小 manifest（key set 恰為 name）。
- 硬性 checklist（39 項）codex 對抗性驗證兩輪：第 1 輪 CONFIRMED 38/FAIL 1
  （AGB-033 `-race` 無 cgo toolchain 環境不可行）；第 2 輪以環境限制記錄 +
  最強替代驗證（vet + 全量/focused -count=1）收斂——最終 39/39。
- ab-script-review：第 1 輪抓到 5 個 A/B 斷言弱化點，補強後 resolved。
- 派工紀錄：實作 dispatch prompt 曾發生 mojibake，agent 以 checklist 為準據
  自行收斂決策，主 session 事後審核接受（fail-closed 選擇優於原裁示）。

## Non-Goals

- 不做 global scope（`~/.gemini/config/plugins/`）部署。
- 不做 plugins.json declared 註冊管理。
- 不處理 marketplace `installed_version.json` 版本追蹤機制。

## Notes

- 複雜任務：`task.py start` 前須補 `design.md`（bundle 佈局、既有 rules/skills
  部署路徑是否遷入 bundle 的相容性）與 `implement.md`。
