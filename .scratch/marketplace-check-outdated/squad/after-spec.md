# Squad record — after-spec

- scope: marketplace-check-outdated
- cut: after-spec
- spec reviewed: specs/marketplace-check-outdated/SPEC.md v0.1
- source state: 0b053fea0846b4bdb5a027db980c18179e62df20
- lenses: scope, input space, repo reality, test mapping（四個獨立唯讀 context，輸入為任務契約、SPEC v0.1、source state、lens brief）
- question: 這份草稿是否涵蓋任務的規則、規則的輸入空間、以及 repo 的現況？

## Findings（合併後，依嚴重度）

- [BLOCKING] internal/marketplace/authoring/editor.go:1034 — schema 收緊放在 `LoadAuthoringConfig` 會讓 `SetPackage` 讀取階段就拒絕既有測試 fixture（editor_test.go 五個 `TestSetPackage_*`、schema_test.go 五處、pack_test.go 兩處的 `source: owner/repo` 無 version 無 ref） — SPEC v0.1 未定義收緊的作用時機 — class 2 — 決策：loader 一律嚴格（oracle 的 set.py、doctor.py 都走嚴格 loader），fixture 補 `ref`/`version`，列入 SPEC「要改的 fixture」；已折入 v1 並列為送審決策 D-c — status: folded
- [BLOCKING] cmd/apm-go/doctor.go:210,258 — `TestDoctor_MarketplaceConfig_ApmYml` 的 fixture（大小寫重複、無 version/ref）在嚴格 loader 下走 config error 路徑，doctor 自身的 `checkDuplicateNames` 變成不可達 — oracle doctor.py:240 同樣把此情況報為 `apm.yml marketplace block has errors: …`，其 check 6 同樣不可達 — class 2 — v1 把該測試列入要改的斷言，`checkDuplicateNames` 保留為與 oracle 相同的防禦層 — status: folded
- [BLOCKING] specs/marketplace-check-outdated/SPEC.md SC-B8 — 「與 pack 一致」不成立：pack 的 HEAD 拒絕訊息是 `resolves to branch/HEAD ref … pin to a tag or commit SHA instead`（build/errors.go），oracle check.py 只有 `Ref 'HEAD' not found`，括號提示是 apm-go-only — class 1 — v1 改寫為「與 pack 同樣拒絕 HEAD，訊息不同」，提示句標記 apm-go-only — status: fixed
- [MAJOR] specs SC-B2/SC-B3 — 伺服器端關閉 `uploadpack.allowAnySHA1InWant` 時，存在但非 tip 的 SHA 也回 `not our ref`，與不存在無法區分 — class 2 — 送審決策 D-a：歸為 not found 並在 failure model 記錄限制 — status: folded
- [MAJOR] specs SC-C1 — 未定義 Current 欄如何得出（SHA→tag 反查或由 version 渲染）、SHA 命中多個 tag、或 SHA 與 version tag 的 commit 不一致時的行為 — class 2 — v1 明定 Current = 以有效 tag_pattern 渲染 `version`，不做 SHA→tag 反查，SHA 與 tag commit 是否一致交給 `check` — status: folded
- [MAJOR] specs SC-C1 — `version` 為 range（`^1.0.0`）時 SHA 釘選的語意未定 — class 2 — 送審決策 D-b：range 版本的 SHA 釘選維持 `Pinned to ref; skipped` — status: folded
- [MAJOR] cmd/apm-go/marketplace_authoring.go:290 — `DuplicatePackageNames` 在嚴格 loader 下於 check 內不可達 — oracle check.py 的 `_warn_duplicate_names` 同樣不可達，註解自稱 defence-in-depth — class 2 — v1 記錄保留理由 — status: folded
- [MINOR] internal/marketplace/authoring/editor.go:556 — 大寫 40-hex 的分類未定 — class 2 — v1 明列沿用小寫慣例，大寫視為具名 ref — status: folded
- [MINOR] specs SC-B1 — SHA 恰為分支 tip（非 tag）是否也免探測未測 — class 2 — v1 加子情境 — status: folded
- [MINOR] D:/Projects2/apm/src/apm_cli/commands/marketplace/outdated.py `except Exception` — 「靜默 exit 1」只限 upgradable>0 路徑，非預期例外仍印 `Failed to check outdated packages: …` — class 1 — v1 明寫 — status: fixed
- [MINOR] specs SC-D4 — 缺少覆蓋 Current 渲染邏輯的 mutant；四個 mutant 的確切 `old` 字串要等 GREEN 後才能寫 — class 2 — v1 加第五個 mutant，setup plan 註明 GREEN 後同一 commit 寫入並跑 mutate.sh 驗證唯一性 — status: folded
- [MINOR] specs SC-C8 — tag_pattern 推斷與 SHA 釘選路徑的疊加未明列 — class 2 — v1 於 SC-C1 明寫渲染前先推斷 — status: folded
- [MINOR] specs SC-B12 — check 的 `include_prerelease` 覆蓋屬補齊 parity（check.py:190 → __init__.py `_extract_tag_versions`），SPEC 背景未登記 — class 2 — v1 背景補記 — status: folded
- [MINOR] specs 明確排除 — `subdir 感知` 缺理由 — class 2 — v1 補「oracle check/outdated 不讀 subdir」 — status: folded
- [MINOR] specs SC-A1 — `name: ""` 的 oracle 訊息是 `'packages[0].name' must be a non-empty string`，與缺鍵不同 — class 2 — v1 加子情境 — status: folded

## 正向確認

- scope：四項裁定與既有缺口清單全部對應到情境，無遺漏、無窄化。
- repo reality：SPEC 對 (a) loader 缺四規則、(b) name-only 比對與前綴剝除、(c) 合成 HEAD、(d) `verbose` 未用、(e) 多印 error 行與 `withSilentExitCode`、(g) 本地 git repo 可當 authoring 層 Source、(i) realexec 可離線跑 D1–D3、(j) oracle 字串逐字相符，皆已用檔案或指令證實。
- test mapping：所有情境在 base ref 上都會失敗，非 vacuous；wording 類情境逐句與 oracle 相符。

## 讓步（各 lens 自陳）

- 未實際在乾淨環境執行 realexec；`build/reflister.go` 是否依賴 authoring 的合成 HEAD 未追；CommitProber 相關情境（B4–B6）依賴尚未存在的程式碼，只能確認 helper 先例存在；oracle `__init__.py` 未逐行核對。
