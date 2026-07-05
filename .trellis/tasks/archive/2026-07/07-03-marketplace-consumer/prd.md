# marketplace 消費端指令(add/list/browse/update/remove/validate)

Parent: `.trellis/tasks/07-03-marketplace-ecosystem`(完整背景、範圍決定、跨子任務 Non-Goals 見 parent prd.md)
權威清單:`.trellis/tasks/07-03-marketplace-ecosystem/marketplace-checklist.md` Phase M0 + Phase M1

## Goal

新增 `internal/marketplace` 套件(資料模型 + `~/.apm/marketplaces.json` 登錄檔存取)與 `cmd/apm/marketplace.go`(`marketplace add/list/browse/update/remove/validate` 六個子指令),對齊 Python 原版行為。這是整個 marketplace 生態系的地基,子任務 2(install 整合)依賴這裡的 fetch/登錄檔邏輯。

## Requirements

實作 checklist `mkt-001` ~ `mkt-019`(見 marketplace-checklist.md Phase M0/M1;`mkt-017` 是「不移植」項),重點:

- 資料模型:`MarketplaceSource`(name/url/ref/path/owner/repo/host/branch,`.Kind()` 分類 local/url/github/gitlab/git)、`MarketplacePlugin`(name/source/description/version/tags/source_marketplace;`registry` 鍵**解析需容忍、值忽略**,不做路由——mkt-005 修訂版)
- 登錄檔:`~/.apm/marketplaces.json` atomic write;名稱查詢不分大小寫、`add` 同名靜默取代(mkt-006);manifest 探測路徑順序(`marketplace.json` → `.github/plugin/marketplace.json` → `.claude-plugin/marketplace.json`)
- `add SOURCE`:SOURCE 判別順序(本地路徑 → 拒絕裸 http:// → SCP SSH → 完整 https URL(含 hosted marketplace.json 直連)→ OWNER/REPO 簡寫);`--host` 與完整 URL host **衝突時硬錯誤 exit 1**、相符/本地/SCP 不符時忽略並警告(mkt-011 修訂版);僅 github/gitlab 家族轉發 PAT;`#ref` fragment 與 `--ref` 互斥、未 pin 警告、alias 回退 manifest.name(mkt-018)
- `list`/`browse NAME`/`update [NAME]`/`remove NAME`/`validate NAME`;`marketplace build` 墓碑錯誤訊息指向 `apm pack`(mkt-019)

## Non-Goals

- `mkt-017`:原版 `--check-refs` 隱藏空殼旗標——不移植佔位邏輯,要嘛做真的,要嘛不出現這個旗標
- `mkt-060`:`marketplace search` 不是這個群組的子指令(那是頂層 `apm search`,屬於子任務 2 的 stretch 範圍)
- `mkt-061`:`marketplace doctor` 不存在(頂層 `apm doctor`,不在本生態系任務範圍內)
- `mkt-063`:`browse --json` 不存在,不要加

## Acceptance Criteria

- [ ] AC1:`mkt-001`~`mkt-016`、`mkt-018`、`mkt-019` 全部勾選,各自有測試佐證(`mkt-017` 以「旗標不存在」的負向測試佐證)
- [ ] AC2:`add SOURCE` 對每種 SOURCE 形狀(本地路徑/SCP SSH/https URL/OWNER-REPO 簡寫)各有正向測試,對裸 `http://` 有負向測試
- [ ] AC3:登錄檔寫入操作至少一個 fixture 是「已存在、含其他無關 marketplace 項目」的 `~/.apm/marketplaces.json`,驗證 add/remove 不動到無關項目(呼應 parent prd.md 的舊坑 1)
- [ ] AC4:對照 Python 原版的 A/B 測試(至少涵蓋 add 各種 SOURCE 形狀 + list + remove),腳本放 `D:\Projects\apm-dev\evals`,不進 apm-go repo
- [ ] AC5:`go build/vet/gofmt/test ./... -cover` 全綠

## Notes

- 這是 4 個子任務中唯一沒有前置依賴的,建議第一個開始執行。
