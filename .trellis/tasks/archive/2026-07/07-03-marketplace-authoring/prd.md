# marketplace 發布端指令(init/check/outdated/audit/migrate/package)

Parent: `.trellis/tasks/07-03-marketplace-ecosystem`(完整背景、範圍決定、跨子任務 Non-Goals 見 parent prd.md)
權威清單:`.trellis/tasks/07-03-marketplace-ecosystem/marketplace-checklist.md` Phase M3

## 前置依賴

無——可與子任務 1(consumer)、2(install)平行進行。操作對象是本地專案自己的 `apm.yml`(`marketplace:` 區塊),跟子任務 1/2 操作的全域登錄檔是不同的資料。子任務 4(`apm pack`)依賴這裡產出的 `marketplace:` 區塊資料模型。

## Goal

新增 `marketplace init/check/outdated/audit/migrate/package(add/remove/set)` 七個發布端子指令,操作本地 `apm.yml` 的 `marketplace:` authoring 區塊(或 legacy 獨立 `marketplace.yml`)。

## Requirements

實作 checklist `mkt-040` ~ `mkt-047`(見 marketplace-checklist.md Phase M3,含 2026-07-03 複審修訂),重點:

- `init`:scaffold 進 **apm.yml 的 `marketplace:` 區塊**(非獨立檔),精確 YAML 形狀(owner/build.tagPattern/outputs/packages,含註解範例);範本不得引導使用者寫會被 pack 擋下的 `ref: main`(mkt-040 修訂版)
- `check [--offline]`/`outdated [--offline] [--include-prerelease]`:對照真實 git tag 驗證可解析性/可升級性;outdated 圖示語意依 mkt-042 修訂版(`[*]` 是 range 外任何更新、`[!]` 過載、`[x]` 不影響 exit code、exit 1 僅由 upgradable 計數驅動)
- `audit NAME [--strict]`:偵測繞過 marketplace pinning 的直接 git ref 依賴;掃 `dependencies` 與 `devDependencies`;`--strict` 只計 NETWORK/PARSE 失敗;建議文字指向 **dict 形式**依賴(mkt-043 修訂版——原版建議的字串形式解析器不收,不可照抄)
- `migrate`:折疊 legacy `marketplace.yml` 進 `apm.yml`,保留註解往返編輯,完成後刪除舊檔
- `package add/remove/set`:編輯 `packages[]`;旗標集依 mkt-045 修訂版(`--name`/`-s` 為 add 專屬、set 的 `--include-prerelease` 是三態);atomic write + 寫後重新驗證失敗即回滾;錯誤路徑 exit code 對齊原版(**2**)

## Non-Goals / 刻意改善(不移植原版 bug)

- `mkt-046`:**必須修正**原版「本地來源實質上裝不進 `package add`」的 bug——`_verify_source`/`_resolve_ref` 邏輯要特判本地路徑來源、略過網路探測,不可原樣複製這個缺陷
- `mkt-017`:`--check-refs` 空殼旗標不移植(見 consumer 子任務,但這裡的 `check` 指令是真正做驗證的,不要跟這個混淆)

## Acceptance Criteria

- [ ] AC1:`mkt-040`~`mkt-047` 全部勾選,各自有測試佐證
- [ ] AC2:`init` 的 scaffold 輸出跟 marketplace-checklist.md 附的 live 輸出逐行比對一致
- [ ] AC3:`mkt-046` 有回歸測試,對本地(`./...`)來源執行 `package add` 不需要塞假 SHA 就能成功
- [ ] AC4:`migrate` 有測試驗證保留原有註解(不是整份重新序列化——比照先前 `--mcp` 任務對 `internal/yamlcore.PatchMappingPath` 的教訓,YAML 編輯要手術式,不要整份重寫破壞格式)
- [ ] AC5:對照 Python 原版的 A/B 測試,至少涵蓋 init scaffold、package add(含本地來源修正案例)、migrate;腳本放 `D:\Projects\apm-dev\evals`
- [ ] AC6:`go build/vet/gofmt/test ./... -cover` 全綠

## Notes

- `migrate`/`package add/remove/set` 都涉及編輯既有 `apm.yml`,務必複用(或至少參考)`internal/yamlcore.PatchMappingPath` 的手術式編輯模式,不要走全文件 decode/mutate/re-encode 的舊路(那條路已知會破壞使用者手動排版過的格式)。
