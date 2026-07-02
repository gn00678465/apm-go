# 修正 --mcp 寫入格式破壞與 registry-backed 一般安裝落差

時間限制:使用者原要求 10 分鐘內修正完畢;10 分鐘內完成 P0(關閉換行寬度,消除資料毀損)並誠實回報排版仍非 100% 保留、P1 未動工。使用者後續明確要求「a,b 都做」(完整位元級格式保留 + P1),於是繼續完成兩者。

## Goal

在 `D:\Projects\apm-dev\evals\demo\apm.yml` 實測時發現兩個真實問題,需修正:

1. **P0 - 資料破壞**:`apm-go install --mcp NAME -- CMD` 寫回 `apm.yml` 時,把整份文件(含完全無關的 `dependencies.apm` 清單)重新序列化成自動換行的壓縮 flow style,破壞使用者原本手動排版過的格式。
2. **P1 - 功能落差**:`apm.yml` 的 `dependencies.mcp` 清單中,registry-backed 項目(無 `registry: false`)在一般 `apm install`(非 `--mcp` 旗標)時被無條件跳過(`internal/deploy/mcpcollect.go`),只印 diagnostic。已向 Python 原版確認:原版一般 `install` 會實際解析並部署這類項目。`internal/mcpregistry` 套件已存在,可直接複用。

## Requirements

- 修 P0:根因是 `internal/yamlcore.SafeDump` 用 `yaml.NewEncoder`(套用 `WithV3Defaults()`,80 字元自動換行寬度)整份重新序列化整棵 Node 樹。第一階段先關閉換行寬度(`WithLineWidth(-1)`)消除資料毀損;第二階段新增 `internal/yamlcore.PatchMappingPath`,對 `dependencies.mcp` 路徑做位元級別手術式替換(只換動到的那一段文字,其餘原始 bytes 完全不動),`cmd/apm/mcpinstall.go` 的寫入路徑改用它、找不到符合條件時 fallback 回整份 `SafeDump`。
- 修 P1:`internal/deploy/mcpcollect.go`(`collectMCPPrimitives`)對 registry-backed 項目呼叫新增的 `internal/mcpregistry.ResolveDeployable`(從 `cmd/apm/mcpinstall.go` 的 `resolveFromRegistry` 抽出的共用邏輯,兩處呼叫點共用同一份憑證驗證/transport 判斷,不重複維護)實際解析並部署,解析失敗只記 diagnostic、不中斷整體安裝。

## Acceptance Criteria

- [x] AC1:對一份手動排版過的 `apm.yml`(`dependencies.apm`/`dependencies.mcp` 皆為多行 flow style)執行 `apm-go install --mcp NAME -- CMD`,執行後除了新增的 mcp 項目本身,其餘既有內容(含格式,逐項換行排版)位元級別不變。驗證:`internal/yamlcore/patch_test.go` 三個案例(既有 flow seq 追加、新建 mcp key、新建 dependencies key)+ 對 `D:\Projects\apm-dev\evals\demo\apm.yml` 實測,`dependencies.apm` 區塊與 `/workspace` 引數皆逐位元組保留。
- [x] AC2:`apm.yml` 宣告 registry-backed mcp 項目時,`apm-go install`(不含 `--mcp` 旗標)能解析並部署到 target config。驗證:`internal/deploy/mcpcollect_test.go` 三個新案例(成功解析、查無此名不致命、需要 header 時的 diagnostic)+ 對 demo 專案實測,`io.github.github/github-mcp-server` 透過真實 registry 解析後正確寫入 `.mcp.json`。
- [x] AC3:`go build/vet/gofmt/test ./...` 全綠。

## Notes

- Keep `prd.md` focused on requirements, constraints, and acceptance criteria.
- Lightweight tasks can remain PRD-only.
