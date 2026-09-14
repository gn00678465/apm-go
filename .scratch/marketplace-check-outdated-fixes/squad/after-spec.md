# Squad record — after-spec

- scope: marketplace-check-outdated-fixes
- cut: after-spec
- spec reviewed: specs/marketplace-check-outdated-fixes/SPEC.md v0.1（commit 3bf13db）
- source state reviewed: 3bf13db
- lenses: scope、input space、repo reality、test mapping。四個獨立唯讀 opus context；輸入為任務契約（Codex findings 與使用者裁定）、SPEC v0.1、source state、lens brief。repo reality 與 test mapping 在 repo 外暫存副本重現；input space 以 importlib 載入 oracle 模組與 PyYAML 實跑。
- question: 這份草稿是否涵蓋 Codex findings 與使用者裁定的規則、規則的輸入空間，以及 repo 的實際狀態？

## Findings（合併後，依嚴重度）

- [HIGH] specs/marketplace-check-outdated-fixes/SPEC.md:61 — SC-F13 的期望「pattern `{version}` 下 tag `v1.2.0` 不成為候選」與 oracle 相反：設定的 pattern 零匹配時 oracle 推斷 `v{version}`，`v1.2.0` 仍是候選 — oracle `commands/marketplace/__init__.py:979-990`；實跑 `infer_tag_pattern("v1.2.0")="v{version}"`；apm-go `refcheck.go:1203-1208` 同一 fallback；scope、input space、repo reality 三個 lens 各自發現 — class 1 — 改寫：單一 pattern 擷取層拒絕 `v1.2.0`；fixture 加 `1.0.0` 讓 fallback 不觸發時候選只有 `1.0.0`；只有 `v1.2.0` 時 usedPattern 為 `v{version}` — status: fixed
- [HIGH] specs/marketplace-check-outdated-fixes/SPEC.md:42,65 — SC-F3 用無限 reader，mutant `manifest-read-uncapped` 下會 OOM，mutate.sh 只把 `--- FAIL|panic:` 算殺死，其餘記 BROKEN，gate 失敗 — `tools/gate/mutate.sh:54-59` — class 1（草稿自己的 SC-F14 宣告無法成立）— 改用有限計數 reader（交出 N+K 後回 EOF），goroutine 加明確期限，斷言讀取數 ≤ N+1 — status: fixed
- [MEDIUM] specs/marketplace-check-outdated-fixes/SPEC.md:15-18 — 使用者原話是「第 3 項：算是 spec 缺口」，當時編號為 Codex 轉交表（3 = tag 推斷）；草稿改用新編號寫成「第 6 項」，屬錯誤引用；第 1、3 項放在「使用者裁定」下但無原話 — 對話紀錄 — class 1 — 加編號對照表，引用原話；第 1、3 項改標為 orchestrator 依原 SPEC 的分類 — status: fixed
- [MEDIUM] specs/marketplace-check-outdated-fixes/SPEC.md:26,48 — D-f 在 SHA 釘選加 display version 的 offline（refcheck.go:1031-1036）與 ListRefs 失敗（:1039-1042）路徑同樣顯示 current map 的 SHA，草稿只收 no-tags 路徑 — 暫存副本重現 offline 列 `Current=<40-hex>` — class 1（原 SPEC:80 D-f「只有存在候選 tag 時才渲染 Current」）— SC-F6 補兩個子測試 — status: fixed
- [MEDIUM] specs/marketplace-check-outdated-fixes/SPEC.md:26 — 無 version 的 SHA 釘選（tip 路徑）在 offline、ListRefs 失敗、`Remote advertised no HEAD` 三條路徑顯示 current map 的 40 字元 SHA，成功時是 sha12 — refcheck.go:1112-1118 — class 2 — 決定 D-2，建議 `--`
- [MEDIUM] specs/marketplace-check-outdated-fixes/SPEC.md:48 — SC-F6 沒有保護非 SHA 列：range 條目無 tag 時 oracle 保留 current map 值（outdated.py:92-103），把所有 no-tags 列的 Current 清掉的實作仍會全綠 — refcheck_test.go 中非 nil current 的測試都有 tag — class 1（違反原 SPEC 的 oracle parity 原則）— SC-F6 加回歸子測試，Must NOT 補「非 SHA 列的 Current 來源不變」— status: fixed
- [MEDIUM] specs/marketplace-check-outdated-fixes/SPEC.md:27,54 — SC-F9 未定型別判準；yaml v4（YAML 1.2）與 PyYAML（YAML 1.1）雙向不一致：`yes`/`on` v4 為 `!!str`、PyYAML 為 bool；`0o17` v4 為 `!!int`、PyYAML 為 str；`AddPackage(Name:"yes")` 寫出未加引號的 `name: yes` — 實跑 yaml v4 與 PyYAML 6.0.3；editor.go:285-286 — class 2 — 決定 D-1，建議以 resolved tag `!!str` 判定，差異記為偏離
- [MEDIUM] specs/marketplace-check-outdated-fixes/SPEC.md:27,54 — `source` 型別檢查與空字串訊息互動未定；`source: ""` 由 `TestLoadAuthoringConfig_EmptySource_Rejected`（schema_test.go:291-307）與原 SC-A2 固定為 `marketplace source is empty`；`source: 123` 在 3bf13db 已 exit 2，只有訊息不同 — 暫存副本重現 — class 1（改用 requireNonEmptyString 會違反草稿「要改的斷言：無」與原 SC-A2）— 定順序：null → is required；非 `!!str` 或非 scalar → must be a non-empty string；空字串 → 保留既有訊息；缺陷表註明 source 只改訊息 — status: fixed
- [MEDIUM] specs/marketplace-check-outdated-fixes/SPEC.md:68-77 — Must NOT 未沿用原 SPEC v4 中相關條目（:100、:102、:104 後半、:106、:109），第 4 項改的是 pack、doctor、package 共用的 LoadAuthoringConfig — 原 SPEC:23 D-c、:102 — class 1 — 加「原 SPEC v4 的 Must NOT 全部仍有效」— status: fixed
- [MEDIUM] tools/gate/mutants.txt:15,19 — 既有 mutant `sha-match-by-name-only`（old 在 refcheck.go:904）與 `current-render-ignores-pattern`（old 在 :1063）的錨點會被第 1、5 項改寫，錨點消失時 mutate.sh exit 2 — tools/gate/mutants.txt — class 1（草稿 SC-F14 只寫新增，gate 會失敗）— 允許更新這兩個 mutant 的 old/new，語意與殺死它的測試不變，記入 Revisions — status: fixed
- [MEDIUM] specs/marketplace-check-outdated-fixes/SPEC.md:101,107 — D-1（v0.1）只提 GATE_SCOPE_PKGS，覆蓋率讀 SCOPE_PATHS（gate.sh:38），Setup plan 未列 tools/gate.sh；scope lens 指出若把 oracle 文法放在 tagpattern（已在 scope），D-1 不存在且 `semver.IsValid` 契約不變 — tools/gate.sh:38-39；IsValid 只有 refcheck.go:1192、tagpattern.go:158 兩個呼叫者，由本 branch 新增 — class 2 — 採設計解：文法放入 tagpattern，刪除 branch 新增的 `semver.IsValid`，不改 gate.sh；隨核准一併確認 — status: fixed
- [MEDIUM] specs/marketplace-check-outdated-fixes/SPEC.md:42 — SC-F3 的讀取函式未命名，RED 只會是編譯失敗（red.sh 記 `failed (collection)`）；錯誤文字未定 — refcheck_sha.go:304 `"%s at the pinned ref exceeds %d bytes"` — class 1（情境無法對應同名測試）— 命名 `readCapped(r io.Reader, max int) ([]byte, error)`，錯誤文字沿用現有格式 — status: fixed
- [MEDIUM] specs/marketplace-check-outdated-fixes/SPEC.md:43-44 — SC-F4、SC-F5 在 3bf13db 已通過（既有 `TestGitManifestVersionFetcher_OversizedManifest_Errors` 已斷言 `exceeds`），「git 子程序已結束」無量測方法 — refcheck_sha_edges_test.go:87-96 實跑 PASS — class 1（草稿把回歸寫成 RED）— 標為回歸；SC-F4 改為縮短 listRefsTimeout 後斷言錯誤為 `exceeds` 而非 `timed out` 且耗時低於期限 — status: fixed
- [MEDIUM] tools/gate/red.sh:27 — RED 重建只看新增（status A）的 `_test.go`，以檔案為單位 — `awk '$1=="A" && $2 ~ /_test\.go$/'` — class 1（草稿 Setup plan 未規定，RED 無法重建）— 每組 RED 情境一個新測試檔，回歸情境另放新檔 — status: fixed
- [MEDIUM] specs/marketplace-check-outdated-fixes/SPEC.md:50 — `v` 前綴規則未定：`V1.0.0`、`vv1.0.0` 在 check 與 outdated 都會誤判 — 實跑 `IsDisplayVersion("V1.0.0")=true`、`NPM.Compare("1.0.0","V1.0.0")=1` — class 2 — 建議只去除一個小寫 `v`，與 check 側 `semver.StripVPrefix`（原 SPEC:61）一致；`V`、`vv` 列入明確排除
- [MEDIUM] specs/marketplace-check-outdated-fixes/SPEC.md:66 — SC-F15 未加 `--offline`，修正回歸時 `name: 123` 會載入並對 github.com 執行 ls-remote，違反 gate 不連網 — realexec.sh:203-205 — class 1 — 加 `--offline` — status: fixed
- [MEDIUM] specs/marketplace-check-outdated-fixes/SPEC.md:37 — SC-F1 未定 prober 形式；主案例需計數 fake，子測試「S 存在」在 3bf13db 靠名稱命中而通過；git 2.53 實測 `git branch <40-hex>` exit 0，fetch 同名分支得 `not our ref` — 暫存副本實跑 — class 1（情境描述無法直接成為測試）— 主案例計數 fake；子測試以計數器包住真實 gitCommitProber，斷言確有探測；先取 SHA 再建分支避免 rev-parse 歧義警告 — status: fixed
- [LOW] specs/marketplace-check-outdated-fixes/SPEC.md:24 — 缺陷 1 帶 display version 時結果不是通過，而是 `Reachable=false` 的 `vanished` 錯誤 — refcheck_sha.go:118-120；暫存副本重現 — class 1 — 缺陷表補述，SC-F1 加帶 version 子測試 — status: fixed
- [LOW] internal/marketplace/authoring/refcheck.go:898 — SC-F1 讓 SHA 比對再偏離 oracle（`check.py:166-175` 只以名稱比對），草稿未標偏離，現有註解引用 `check.py:151-157` 已過時 — b75a02b1 check.py — class 1（原 SPEC:27 要求標記偏離）— SC-F1 標（偏離），GREEN 更正註解 — status: fixed
- [LOW] specs/marketplace-check-outdated-fixes/SPEC.md:38 — SC-F2 缺大寫 40-hex、7 字元縮寫；`ref: main` 依賴 init.defaultBranch；Windows 上同 repo 無法同時建小寫與大寫同名分支 — refcheck_test.go:41；git 實跑 — class 1（Must NOT「非小寫 40-hex 比對不變」無覆蓋）— 補子測試，各自獨立 RG，分支名動態取得 — status: fixed
- [LOW] specs/marketplace-check-outdated-fixes/SPEC.md:49 — SC-F7「Current 欄為 `--`」子字串比對分不出欄位 — 同列多欄為 `--` — class 1 — 改為斷言輸出不含該 SHA 的 40 字元與 12 字元前綴 — status: fixed
- [LOW] specs/marketplace-check-outdated-fixes/SPEC.md:50,65 — SC-F8 只有 Current、LatestInRange 為 RED；refcheck.go:1145 去 `v` 對結果無影響，mutant 放那裡必存活 — 暫存副本 `CompareVersions("1.0.0","v1.0.0")=-1` — class 1 — 標明回歸欄位；mutant 作用在 :1063 — status: fixed
- [LOW] specs/marketplace-check-outdated-fixes/SPEC.md:65 — 新 mutant 錨點可能不唯一：`v.Kind != yaml.ScalarNode` 在 schema.go 出現 3 次 — grep -cF — class 1 — 錨點取含函式特有 token 的整行 — status: fixed
- [LOW] specs/marketplace-check-outdated-fixes/SPEC.md:55 — SC-F10 標 parity，但 oracle `str(value)` 會正規化數值（`1.10`→`'1.1'`、`0x1F`→`'31'`、`true`→`'True'`），`version: [1, 2]` oracle 接受 — PyYAML 實跑 — class 2 — 記為明確排除：apm-go 保留原始 scalar 文字、拒絕非 scalar，行為不變 — status: fixed
- [LOW] specs/marketplace-check-outdated-fixes/SPEC.md:59 — SC-F11 期望值取得方式照文字無法重現（未註冊 sys.modules 會在 @dataclass 失敗）；Python `\d` 接受 Unicode 數字、`$` 接受結尾換行 — 實跑 `parse_semver("١.٢.٣")`、`("1.2.3\n")` 為接受 — class 1 — 記錄確切指令；ASCII-only 與結尾換行列為明確偏離並各加一列 — status: fixed
- [LOW] specs/marketplace-check-outdated-fixes/SPEC.md:43 — SC-F4 未涵蓋 apm.yml fallback 讀取超限 — refcheck_sha.go:139 — class 1 — 補子測試 — status: fixed
- [LOW] specs/marketplace-check-outdated-fixes/SPEC.md:71 — Must NOT 未涵蓋 `IsDisplayVersion` 與 semver 其他匯出函式 — build/metadata.go:390 委派 IsDisplayVersion；resolver/diamond.go:22,42 用 StripVPrefix — class 1 — 補 Must NOT — status: fixed
- [LOW] specs/marketplace-check-outdated-fixes/SPEC.md:75 — Must NOT「子程序保留 ApplySecureGitEnv/WaitDelay/pinGitLocale」對 showAtFetchHead 無斷言 — authoring 測試 grep 為空 — class 1 — 抽出 show 命令建構函式並加形狀測試 — status: fixed
- [LOW] specs/marketplace-check-outdated-fixes/SPEC.md:70,72 — 兩條 Must NOT 為 diff 形狀，gate 無對應層 — class 1 — evidence 記錄檢查指令與輸出 — status: fixed
- [LOW] specs/marketplace-check-outdated-fixes/SPEC.md:26,29 — 引用行號偏差：`_VERSION_RX` 在 tag_pattern.py:148-154；`parse_semver` 呼叫在 _shared.py:45-47；loadCurrentMarketplaceVersions 為 478-504 — class 1 — 更正 — status: fixed
- [LOW] specs/marketplace-check-outdated-fixes/SPEC.md:60 — SC-F12「依此順序列出」措辭不準（順序來自 ls-remote refname 排序）；選 `^1.1.0` 的原因未寫（`^1.0.0` 時 `1` 滿足範圍，非 RED） — 暫存副本 — class 1 — 改寫 — status: fixed
- [LOW] specs/marketplace-check-outdated-fixes/SPEC.md:27 — oracle 同一型別規則涵蓋 subdir、tag_pattern、owner.name、marketplace name/description/version，草稿未納入也未排除 — yml_schema.py:609,836-838,878-880,1108-1125 — class 2 — 列入明確排除（Codex 未提，使用者範圍為 Codex findings）— status: fixed
- [LOW] internal/marketplace/authoring/refcheck.go:1134,1145 — SHA 釘選與 `+build` tag 並存時 CompareVersions 字串 tie-break 誤報 `[!]` — 實跑 `NPM.Compare("1.2.3+a","1.2.3+b")=0` — class 3 — 記入明確排除
- [LOW] internal/marketplace/authoring/refcheck_sha.go:222、refcheck.go:67 — fetch stderr 與 ls-remote stdout 也寫入無上限緩衝區 — class 3 — 記入明確排除
- [LOW] internal/marketplace/authoring/refcheck.go:740-747 — annotated tag object SHA 命中 commit 欄位，不探測即通過 — class 3 — 記入明確排除
- [LOW] specs/marketplace-check-outdated-fixes/SPEC.md:77 — 「每個遠端套件至多一次 ls-remote」無計數測試（沿用原 SPEC）— class 3 — evidence 標為未量測
- [LOW] cmd/apm-go/marketplace_authoring.go:479 — CLI 讀 cwd 的 `./marketplace.json`，pack 的 claude 預設輸出在 `.claude-plugin/`，缺陷 3 只在根目錄有檔案時觸發；oracle `__init__.py:942` 同路徑 — class 3 — 背景段註明觸發條件
- [INFO] test mapping lens 在暫存副本中發現 `TestDoctor_ExecGit_TimesOutDespiteOrphanedGrandchildHoldingPipesOpen` 單獨執行失敗，原因未調查 — class 3 — evidence honest notes 記錄，RED 前在 worktree 基準跑一次確認

## 分歧與裁定

- D-1（v0.1）：scope lens 建議把文法放在 tagpattern 以免改 gate；repo reality 與 test mapping 建議同時改 SCOPE_PATHS 與 GATE_SCOPE_PKGS。裁定：放在 tagpattern。理由：tagpattern 已在 gate scope，修改集中在 marketplace 層，不需要 gate.sh 授權；`semver.IsValid` 由本 branch 新增且只有兩個呼叫者，刪除不影響 resolver/install。

## 讓步

- 四個 lens 都沒有跑完整 `go test ./...`、gate 或 parity corpus；沒有檢查真實遠端（GitHub 是否接受 40-hex 分支名與 SHA fetch）；沒有審 ARCHITECTURE.md 行號錨點與輸出措辭。
- 本機 oracle 在 v0.30.0（8c2e0d9c），被引用的 semver.py、tag_pattern.py、_shared.py、yml_schema.py 對 pin b75a02b1 diff 為空；`commands/marketplace/__init__.py` 有變更，但未逐行比對 `_extract_tag_versions` 函式本體。
