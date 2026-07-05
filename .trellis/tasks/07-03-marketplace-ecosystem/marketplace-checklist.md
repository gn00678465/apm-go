# Marketplace 生態系驗收清單(消費端 + 發布端)

> **用途**:比照 `conformance/conformance-kit/acceptance-checklist.md` 的格式,把「移植 Python 原版 apm 的 marketplace 生態系」排成逐項可勾選的驗收清單。與 acceptance-checklist.md 不同:這些指令**不是** OpenAPM v0.1 的規範性(normative)要求——規範本體在 §7.1 參照類型清單第 5 項明文把 marketplace 列為「v0.1 非規範性,純 producer 端 authoring artifact」(注意:此敘述是 §7.1 的清單項,**不是** `req-rs-008` 的條文本體——`req-rs-008` 規範的是「依賴分類必須是條目本身的確定性函數」,consumer 端要求),`req-mf-017` 只規範 `marketplace.packages[].source` 的欄位驗證規則。因此本清單的權威來源是 **Python 原版原始碼行為**(逐項附檔案路徑/函式名/行號),不是規範條文;`req-mf-017` 一條例外,仍是 OpenAPM 規範性要求,獨立標註。
>
> **權威來源**:
> - 原始碼:`D:\Projects\apm-dev\apm\src\apm_cli\commands\marketplace\**`、`D:\Projects\apm-dev\apm\src\apm_cli\marketplace\**`
> - 指令文件:`D:\Projects\apm-dev\apm\docs\src\content\docs\consumer\installing-from-marketplaces.md`、`.../producer\publish-to-a-marketplace.md`、`.../reference\cli\marketplace.md`
> - 規範:`D:\Projects\apm-dev\apm\docs\src\content\docs\specs\openapm-v0.1.md` §4.7(req-mf-017)、§7.1(參照類型清單第 5 項;`req-rs-008` 相鄰但條文本體是依賴分類確定性,勿混用)
> - 即時驗證:本任務規劃階段的研究 agent 已對照真實 `uv run apm`(v0.21.0)跑過每個子指令的 `--help` 與至少一次端對端流程,發現源碼閱讀本身會漏掉的落差(見 Phase M5)。之後任何「原始碼寫的行為」與「live CLI 實測行為」衝突時,以 live CLI 實測為準。

## 範圍與決定

| 項目 | 決定 |
|---|---|
| 範圍 | **消費端 + 發布端全部**(使用者已確認):marketplace add/list/browse/update/remove/validate、`install <plugin>@<marketplace>[#ref]`、marketplace init/check/outdated/audit/migrate/package(add/remove/set)、`apm pack`(marketplace.json 產生器) |
| 前置依賴 | apm-go 目前**沒有** `apm pack` 指令,發布端最後一步(Phase M4)需要新建;消費端與 install 整合(Phase M0-M2)不依賴它,可獨立先做 |
| 相關但非本次兩項指令 | `apm search QUERY@MARKETPLACE`(頂層指令,非 `marketplace` 子指令組,但與 browse/install 共用 fetch 邏輯)——列為 Phase M6 stretch,依實作順序視時間決定是否納入同一輪 |
| 相關但非本次兩項指令 | `apm uninstall pkg@mkt`、`apm view pkg@mkt` 在原版也接受 marketplace 參照(`uninstall/engine.py:17-19,88-93`、`view.py:352-354,465-467` 均呼叫 `parse_marketplace_ref`)——本輪明確不做,install 完成後這兩個指令的語法對稱性需另開任務補齊,不可遺忘 |
| 明確排除(原版文件錯誤,不可移植) | `marketplace doctor`(不存在,`marketplace.md:29` synopsis 誤列;真正指令是頂層 `apm doctor`)、`marketplace publish`(完全不存在,純文件錯誤)、`browse --json`(不存在,e2e script 本身已標記未接入 CI)、`search.md:70` 的 `marketplace refresh`(不存在,真名是 `update`) |
| 明確排除(指令位置澄清,**非**文件錯誤) | `marketplace search`(不存在;真正指令是頂層 `apm search`。文件本身是對的——`marketplace.md:314` 與 `search.md:13` 均正確指向頂層指令;錯的只有原始碼內殘留的 docstring/錯誤訊息字串(`commands/marketplace/__init__.py:1351,1361` 仍寫 `apm marketplace search`),Go 版不可照抄這些字串) |
| 明確排除(原版真實 bug,修正不移植) | `package add` 對本地(`./...`)來源實質上無法使用(`_verify_source` 無條件 `git ls-remote`、`_resolve_ref` 無法在無網路下解析本地 HEAD);Go 版本須在對應邏輯特判本地來源,不可原樣複製此 bug |
| 部分排除(路由不移植,解析需容忍) | `MarketplacePlugin.registry` 欄位:**路由行為不移植**——原版只出貨了解析層(`models.py:309,409-441` 解析+semver 驗證,測試僅覆蓋解析),全庫無任何解析路徑消費 `plugin.registry`;其引用的設計文件 `docs/proposals/registry-api.md` 不存在於 repo,消費端文件也未提及(先前「文件提及但尚未出貨」的說法兩邊都不準確)。但 Go 版的 manifest 解析**必須容忍**(忽略、不報錯)含 `registry` 鍵的 plugin 條目,對齊原版 backwards-compat 行為 |

### 本任務要防的「舊坑」(依本專案先前 16 個 session 的實際教訓)

> 這些不是 marketplace 特有的規則,是這個專案反覆踩過、值得在動工前明列成檢查項的**流程**坑,適用於本任務每個 Phase:

1. **不能只用全新產生的 fixture 測試** —— session 16 的 YAML 格式破壞 bug 在 11 輪 codex 審查 + A/B 測試全綠的情況下活了下來,因為 fixture 從未包含「已存在、手動排版過」的真實檔案。本任務每個寫入 `apm.yml`/`~/.apm/marketplaces.json` 的路徑,測試矩陣都要包含至少一份「已存在、含其他無關內容」的 fixture,而非只測全新檔案。
2. **「不在範圍」的判斷要標明權威來源,不能只靠假設** —— session 11 曾把 registry-backed MCP 解析標成「本任務不在範圍」,結果是誤判(Python 原版其實有做),導致 session 16 要回頭補。本清單每個「排除」項都已標明「文件錯誤」/「未出貨」/「真實 bug」三種不同權威依據,實作時不可再自行擴大排除範圍而不註記依據。
3. **讀原始碼不夠,同一功能要對照 live CLI 實測** —— MCP registry client 任務曾因為只讀 Python 源碼就對 API 欄位形狀做錯誤假設(`transport_type` vs `type`)。本清單的 Phase M0-M4 每一條都已由研究 agent 對照 live CLI 驗證過,實作時發現任何「源碼寫的」與「這份清單寫的」不一致,以清單標註的實測結果為準,不要重新相信源碼字面。
4. **同一類 bug 要全庫掃描,不能只修回報的那一處** —— path traversal 防護曾在 3 個不同檔案分別踩坑(`update.go`、`gitops/clone.go`、`registry/loader.go`),因為第一次只修了回報的那個呼叫點。本任務所有跨 host/URL 驗證邏輯(mkt-011、mkt-028)若之後要調整,必須先 grep 全庫找出其他呼叫同類驗證的地方,不能只改一處。
5. **「加速」不等於「降低驗證嚴謹度」** —— 前一輪任務曾把使用者「提升完成速度」誤讀成「停止驗證迴圈」。本任務規模大,時間壓力下的正確做法是縮小單次迭代範圍或跑得更頻繁,而不是跳過驗證步驟。

---

## Phase M0 — 資料模型與登錄檔(所有 marketplace 指令的地基)

| ✓ | id | 權威 | 驗證內容 | 對照 |
|---|----|----|----------|------|
| [ ] | `mkt-001` | 源碼+實測 | `MarketplaceSource` 資料模型:`name/url/ref/path/owner/repo/host/branch`;`.kind` 屬性依 URL 形狀分類為 `local\|url\|github\|gitlab\|git` | `marketplace/models.py` |
| [ ] | `mkt-002` | 實測 | `~/.apm/marketplaces.json` 登錄檔:atomic write(temp+rename)、目錄建立時嘗試 `0700`(Windows 上該權限位會被忽略,Go 版本比照,不視為缺陷) | 實測登錄檔內容(見研究報告 Part A) |
| [ ] | `mkt-003` | 源碼 | 探測 marketplace manifest 的路徑順序(依序嘗試,第一個存在即用):`marketplace.json` → `.github/plugin/marketplace.json` → `.claude-plugin/marketplace.json` | `client.py::_MARKETPLACE_PATHS`/`_auto_detect_path` |
| [ ] | `mkt-004` | 源碼 | Marketplace 別名/名稱格式限制:`^[a-zA-Z0-9._-]+$`(因為要出現在 `plugin@marketplace` 語法的 `@` 右側) | `_ALIAS_PATTERN` |
| [ ] | `mkt-005` | 源碼 | `MarketplacePlugin` 資料模型:`name/source/description/version/tags/source_marketplace`;`registry` 欄位**路由不移植但解析需容忍**(見範圍表:manifest 含 `registry` 鍵不得報錯,欄位值忽略即可) | `marketplace/models.py` |
| [ ] | `mkt-006` | 源碼 | 登錄檔查詢與寫入語意:marketplace 名稱查詢**不分大小寫**(`registry.py:95-98`);`add` 對同名(不分大小寫)marketplace **靜默取代**既有登錄項(`registry.py:104-108`),不報錯不確認 | `marketplace/registry.py` |

---

## Phase M1 — 消費端子指令

| ✓ | id | 權威 | 驗證內容 | 對照 |
|---|----|----|----------|------|
| [ ] | `mkt-010` | 源碼+實測 | `marketplace add SOURCE [-n/--name] [-r/--ref] [--host] [-v]`(另有隱藏棄用別名 `--branch/-b`,與 `--ref` 同給時硬錯誤):SOURCE 自動判別順序 —— 本地路徑形式(`/`、`./`、`../`、`~/`、`.\`、`..\`、`~\`、裸 `~`、`file://`、Windows 磁碟機代號)→ 拒絕裸 `http://` → SCP 式 SSH(`git@host:path`)→ 完整 `https://` URL(含指向 hosted `marketplace.json` 的直連 URL,登錄為 kind=url、ref/path 置空)→ `OWNER/REPO`/`HOST/OWNER/REPO` 簡寫 | `commands/marketplace/__init__.py::_parse_marketplace_source` |
| [ ] | `mkt-011` | 源碼 | `--host` 行為分三種(先前「一律忽略並警告」的說法不準):與完整 HTTPS URL(含 marketplace.json URL)的 host **衝突**時是**硬錯誤 exit 1**(`__init__.py:331-338,372-379`);與 URL host 相符、SOURCE 是本地路徑、或 SCP-SSH host 不符時,忽略並發警告(`:577-596`);僅 github/gitlab 家族 host 會被信任轉發 `GITHUB_APM_PAT`/`GITLAB_APM_PAT`,其餘 host 純走 `git` subprocess、不帶 token | `AuthResolver.classify_host` |
| [ ] | `mkt-012` | 源碼 | `marketplace list [-v]`:無引數,列出所有已註冊 marketplace(Name/Source/Ref/Path) | — |
| [ ] | `mkt-013` | 源碼+實測 | `marketplace browse NAME [-v]`:強制重新抓取(force-refresh)、渲染 Plugin/Description/Version/Install 表、印出 `apm install <plugin-name>@{name}` 提示 | — |
| [ ] | `mkt-014` | 源碼 | `marketplace update [NAME]`:給定名稱只刷新該 marketplace 快取,省略時刷新全部已註冊項目 | — |
| [ ] | `mkt-015` | 源碼 | `marketplace remove NAME [-y/--yes]`:無 `-y` 需互動確認;非互動/CI 環境無 `-y` 時 exit 1(不可靜默略過確認) | — |
| [ ] | `mkt-016` | 源碼 | `marketplace validate NAME`:對**已註冊**的 marketplace(不是本地 authoring config)驗證,印 `Summary: N passed, N warnings, N errors`,任何 error 時 exit 1 | `marketplace.validator::validate_marketplace` |
| [ ] | `mkt-017` | 實測 | **明確不移植**:原版 `--check-refs` 隱藏旗標(`hidden=True`)目前只印「尚未實作」的佔位訊息,不做任何檢查;Go 版本要嘛做出真正的 ref 可達性檢查,要嘛完全不出現這個旗標,不可移植一個誤導使用者的空殼旗標 | 實測(見研究報告 Part A) |
| [ ] | `mkt-018` | 源碼 | `marketplace add` 的 ref/alias 補充行為:HTTPS SOURCE 支援 `#ref` fragment(與 `--ref`/`--branch` 同給時硬錯誤,`__init__.py:536-551,741-750`);https git URL 未 pin ref 時發「Pin this git marketplace with a #ref」警告(`:764-774`);`--name` 未給時 alias 依序回退 manifest.name(需通過 alias 格式檢查,不合法時警告並退回 repo 名,`:677-699`) | `commands/marketplace/__init__.py::add` |
| [ ] | `mkt-019` | 源碼 | `apm marketplace build` 墓碑:已移除的子指令,呼叫時硬性 UsageError 並指向 `apm pack`(`__init__.py:94-99`);Go 版需決定是否保留此遷移提示(建議保留,防止舊文件/腳本靜默失敗) | `MarketplaceGroup.get_command` |

---

## Phase M2 — `install <plugin>@<marketplace>[#ref]` 解析與部署整合

| ✓ | id | 權威 | 驗證內容 | 對照 |
|---|----|----|----------|------|
| [ ] | `mkt-020` | 源碼+實測 | 語法 `PLUGIN@MARKETPLACE` 或 `PLUGIN@MARKETPLACE#REF`;字串在 `#` 之前若含 `/` 或 `:` 一律不視為 marketplace 參照(讓 `owner/repo`、`owner/repo@alias`、`git@host:o/r` 正常落回一般 git 依賴解析)。⚠️ 原版 install 層另有一道**更粗**的前置閘門 `"/" not in package`(`install.py:368`,檢查整條字串含 `#` 後的 ref),導致 `pkg@mkt#feature/branch`(ref 含斜線)在 `apm install` 進不了 marketplace 路徑而失敗——與 resolver 特意先切 `#` 支援斜線 ref 的設計矛盾,且 uninstall 走的是 resolver 規則。Go 版須明確決定複製此 quirk 或修正並登記 deviation(A/B 對照 live CLI 時此處會分歧) | `marketplace/resolver.py::parse_marketplace_ref`,`_MARKETPLACE_RE` |
| [ ] | `mkt-021` | 源碼+實測 | **僅限 CLI `pkg@mkt#REF` 後綴語法**:`#REF` 必須是原始 git ref(tag/branch/SHA),含 `[~^<>=!]` 任一字元時明確報錯,訊息需指名「semver range 不支援,請用原始 tag/branch/SHA」。⚠️ 此限制**不是全域規則**——apm.yml dict 形式的 `version:` 欄位明文支援 semver range(見 mkt-033),不可把拒絕邏輯做到 resolver 的 version_spec 路徑上 | `_SEMVER_RANGE_CHARS` |
| [ ] | `mkt-022` | 源碼+實測 ⚠️反直覺 | Marketplace 名稱查詢**只**對照全域登錄檔 `~/.apm/marketplaces.json`,**不會**去讀目前專案自己 `apm.yml` 裡的 `marketplace:` 區塊;必須寫一個負向回歸測試明確鎖定這個行為(這條容易被直覺誤實作成「順便查一下本地 apm.yml」) | `resolve_marketplace_plugin` |
| [ ] | `mkt-023` | 源碼+實測 | 依 `source.kind` 分派抓取方式:`github`→Contents API **+ `Accept: application/vnd.github.v3.raw`**(回應=原始檔案內容而非 base64 envelope,已 live curl 驗證;避開 >1MB base64 限制);`gitlab`→REST v4 **`/repository/files/{urlenc(path)}/raw`** 純文字端點(`PRIVATE-TOKEN` header);`git`→sparse clone;`local`→直接讀檔/裸 repo `git show`/工作樹讀取;`url`→直接 HTTPS GET(含 SHA-256 digest、ETag 快取、10MB 上限)。原版另有 env 閘控的 registry-proxy 前置嘗試,不移植 | `client.py:384-400,405-417` |
| [ ] | `mkt-024` | 源碼 | Plugin 名稱查詢**不分大小寫**;查無時報 `PluginNotFoundError` | — |
| [ ] | `mkt-025` | 源碼 | 本地 marketplace(`source.kind=="local"`)+ plugin.source 為純相對路徑字串時,直接解析成本地磁碟路徑(fast path),不經過依賴字串往返 | — |
| [ ] | `mkt-026` | 源碼 | `plugin.source` 為 dict 時的映射規則:`github`→`owner/repo[/path]#ref`;`git-subdir`/`gitlab`→同形狀;`url`→經 `DependencyReference.parse` 重新推導;純相對路徑字串→`marketplace_owner/marketplace_repo/relpath`;`npm` 形狀**明確拒絕**——注意雙層行為:`type: npm` 在 manifest 解析階段就被丟棄(`models.py:380-382`,browse 看不到、install 報 PluginNotFoundError),`resolve_plugin_source` 的 npm ValueError 只有 `kind: npm` 鍵變體才會觸發(`_coerce_dict_plugin_type` 認 type/source/kind 三鍵,parser 只認 type/source)。Go 版兩層都要對齊,否則使用者可見錯誤不同 | `resolve_plugin_source` |
| [ ] | `mkt-027` | 源碼 | 非 GitHub 家族 host(自架 GitLab/通用 git/ADO/Gitea)+ marketplace 內子目錄 plugin,建立**結構化** `{git:, path:, ref:}` 參照,避免巢狀 group 路徑歧義 | — |
| [ ] | `mkt-028` | 源碼+文件 ⚠️安全性 | Cross-repo dependency-confusion fail-closed 閘門:enterprise GitHub 家族 marketplace(**邊界精確化**:`is_github_hostname` 為真且非 `github.com`,即 `*.ghe.com` **加上** `GITHUB_HOST` 環境變數設定的 GHES host——`github_host.py:170-196`;先前只寫 `*.ghe.com` 不完整)上的 plugin 若宣告**跨 repo**且**未限定 host** 的裸 `repo: owner/repo` 依賴 → 必須在任何對外網路探測**之前**拒絕(因為未限定 host 的 `owner/repo` 預設會被當成 `github.com`,可被公開 GitHub 上搶注同名 repo 利用)。已驗證原版確為 fail-closed:拒絕在 `install.py:406-425`,先於 `:509-524` 的探測;⚠️ `resolver.py:884-892` 有**過時註解**仍描述舊版 #1305 advisory 行為(僅驗證失敗時提示),讀碼時勿被誤導,以 `CrossRepoMisconfigRisk` docstring 與 install.py 實際行為為準 | `CrossRepoMisconfigRisk`,`producer/publish-to-a-marketplace.md` 範例 |
| [ ] | `mkt-029` | 源碼+實測 | Marketplace 解析出的依賴,最終收斂成跟一般 git/local 依賴**完全相同**的 primitive 型別(instructions/agents/skills/mcp);沒有獨立的 marketplace primitive 型別 | `apm_resolver.py::_resolve_marketplace_dep` |
| [ ] | `mkt-030` | 源碼+實測 | `apm.yml` 實際寫入的是**已解析**的純字串/物件參照,不含任何 marketplace 中繼資料;未解析狀態(`is_marketplace=True`)一律不得序列化到 apm.yml(需要一個「解析先於持久化」的不變式守衛,原版是直接 raise ValueError) | `to_apm_yml_entry()` |
| [ ] | `mkt-031` | 實測 | Lockfile provenance:`discovered_via`/`marketplace_plugin_name`/`source_url`/`source_digest` 四個欄位是**純附加**中繼資料,不影響依賴身分(`get_unique_key()`)或解析結果;這四個欄位**不是** OpenAPM 規範欄位(§5.2 表中沒有,但 `reference/lockfile-spec.md:145-146` 有記載,引用時以後者為準),Go 版本沿用是為了與真實世界 lockfile 相容,而非規範要求。補充:(a) provenance 會參與 `is_semantically_equivalent`(`lockfile.py:745-748`),新的無 provenance 條目會**主動覆寫**舊條目(mkt-032 機制的一環);(b) `source_url`/`source_digest` 只在 kind=url 的 marketplace 有值(`client.py:921-922`) | 實測 lockfile 內容(見研究報告 Part B) |
| [ ] | `mkt-032` | 實測 ⚠️改善不移植 | **原版已知資料遺失 bug,Go 版本要修正、不要複製**:`apm install pkg@mkt`(無 `--target`、專案尚無 harness 訊號)會先把已解析依賴寫進 `apm.yml`,再中斷等待 `--target`;若接續呼叫的第二次 `apm install`/`apm install --target X` **沒有**重新帶 `pkg@mkt`,provenance 欄位會直接消失、不留痕跡。Go 版本應該把 provenance 暫存(例如寫進一個 pending 狀態或要求同一次呼叫必須帶齊 marketplace 參照 + target),不可原樣複製這個資料遺失路徑。機制精確化(已源碼追實):run 1 的 provenance 是**從未寫入** lockfile——target 閘門的 `NoHarnessError→SystemExit(2)` 發生在 lockfile phase 之前,且 SystemExit 繞過 `except Exception` 使 apm.yml 不回滾(`install.py:1509→614` 先寫、`pipeline.py:633-636` 閘門、`:870` lockfile);provenance 只在 CLI 帶 marketplace ref 時附加(`install.py:1788-1789`),不會從 apm.yml/舊 lockfile 重建。回歸測試應斷言此淨效果 | 實測兩段式安裝流程(見研究報告 Part B) |
| [ ] | `mkt-033` | 源碼+實測 ⚠️核心缺口補列 | apm.yml dict 形式 marketplace 依賴:`dependencies.apm` 支援手寫 `{name: X, marketplace: Y, version: Z}`(`reference.py:748-792`),root 與**傳遞依賴**都會走 marketplace 解析(`apm_resolver.py:547,565,711`);`version:` **支援 semver range**(實測 `~1.2.0` 通過,由 `resolve_version_constraint` 對真實 tag 解析、無相符 tag 且非嚴格 range 時回退原始 ref,`resolver.py:922-958`);驗證規則:`marketplace` 鍵不可與 `git`/`path`/`registry`/`id` 併用、未知鍵拒絕、name/marketplace 需符合 `[a-zA-Z0-9._-]+`;**字串形式 `pkg@mkt` 在 apm.yml 會被拒絕**(實測:`Shorthand '@alias' is not supported`),需負向測試鎖定 | `DependencyReference.parse_from_dict` |
| [ ] | `mkt-034` | 源碼 ⚠️安全性 | 兩個 advisory 安全機制(原清單遺漏):(a) ref-swap 偵測——以 `(marketplace, plugin, version)` 為鍵記錄 ref pin,再次解析發現 ref 變更時警告「may indicate a ref swap attack」(`version_pins.check_ref_pin/record_ref_pin`,`resolver.py:968-994`);(b) shadow 偵測——同名 plugin 存在於其他已註冊 marketplace 時警告(`shadow_detector.detect_shadows`,`resolver.py:1003-1019`,偵測失敗不得中斷安裝) | `resolve_marketplace_plugin` |
| [ ] | `mkt-035` | 源碼 | Marketplace 註冊 ref 的傳播(#1811):marketplace 以非 `main`/`HEAD` 的 `--ref` 註冊、plugin 為相對字串 source 且 canonical 無 `#ref` 時,自動附加 `#{source.ref}`(`resolver.py:908-916`);in-marketplace 子目錄 plugin 的 effective-ref 回退同理(`:828-830`) | `resolve_marketplace_plugin` |

---

## Phase M3 — 發布端子指令(操作本地 `apm.yml` 的 `marketplace:` 區塊)

| ✓ | id | 權威 | 驗證內容 | 對照 |
|---|----|----|----------|------|
| [ ] | `mkt-040` | 實測 | `marketplace init [--force] [--no-gitignore-check] [--name] [--owner] [-v/--verbose]`:scaffold 進 **apm.yml 的 `marketplace:` 區塊**(apm.yml 不存在時先建最小殼,`init.py:39-91`),不是獨立 marketplace.yml;精確形狀(`owner.name/owner.url`、`build.tagPattern`、`outputs.claude/#codex`、`packages[]` 含 name/description/source/version 與註解掉的本地套件範例、`# category` 提示)。⚠️ 範本註解建議的 `ref: main` 例子照抄會被 `apm pack` 的 HeadNotAllowedError 擋下(pack 未暴露 allow-head 旗標,見 mkt-055)——scaffold 內容與 pack 閘門的既有矛盾,Go 版範本應避免引導使用者踩雷 | 研究報告 Part A 附完整 live 輸出 |
| [ ] | `mkt-041` | 源碼 | `marketplace check [--offline]`:本地來源略過網路檢查;遠端來源用 `git ls-remote` 對照 pin 的 ref/semver range 是否真實存在;任何失敗 exit 1 | — |
| [ ] | `mkt-042` | 源碼 | `marketplace outdated [--offline] [--include-prerelease] [-v]`:狀態圖示語意(修正版)—— `[+]` 已最新;`[!]` **過載**:range 內可升級(計入 exit 1)**與**「No matching tags found」(不計入,`outdated.py:100-104`)共用同一圖示;`[*]` = latest overall ≠ latest in range,即 **range 外有任何更新版本**(不限 major,`:127-128`);`[i]` 已 pin 或無 range 略過;另有 `[x]` 遠端抓取失敗(`:83`,**不影響 exit code**)。exit 1 僅由 `upgradable` 計數器驅動(`:158-160`) | `commands/marketplace/outdated.py` |
| [ ] | `mkt-043` | 源碼 | `marketplace audit NAME [--strict] [-v]`:對**已註冊**的 marketplace,抓每個 plugin 自己 pin 版本的 `apm.yml`(未 pin 時回退 `HEAD`),掃 `dependencies` **與 `devDependencies`** 的 apm 區(`marketplace/audit.py:150`),標記繞過 marketplace 簡寫、直接用裸 git ref / `{git:}` 物件的項目;`--strict` 時繞過或 NETWORK/PARSE 抓取失敗 exit 1(NO_MANIFEST/UNSUPPORTED_SOURCE 算 skipped,**不**觸發)。⚠️ 原版矛盾不可照抄:audit 的建議文字要人改寫成 `pkg@<marketplace>` **字串**依賴(`audit.py:100-107`),但依賴解析器不收字串形式(見 mkt-033),Go 版建議文字應指向 dict 形式 | `commands/marketplace/audit.py` + `marketplace/audit.py` |
| [ ] | `mkt-044` | 源碼 | `marketplace migrate [--force\|--yes/-y] [--dry-run]`:把獨立 `marketplace.yml` 折入 `apm.yml` 的 `marketplace:` 區塊(保留註解的往返編輯),完成後**刪除** `marketplace.yml`;已存在區塊時無 `--force` 拒絕覆寫 | — |
| [ ] | `mkt-045` | 源碼 | `marketplace package add/remove/set`:編輯本地 authoring config 的 `packages[]`。旗標**並非完全共用**(先前說法不準):`--version`/`--ref` 互斥(指令層 + editor 層雙重把關)、`--tag-pattern`、`--tags` 兩邊皆有;`--name` 與 `-s` 短旗標為 **add 專屬**;`--include-prerelease` 在 add 是普通 flag、在 set 是**三態**(default=None);`add` 另有 `--no-verify`;`remove` 有 `--yes/-y`(非互動無 `-y` 時 exit 1)。寫入為 atomic write(temp+fsync+rename)+ 寫後重新驗證失敗即回滾;package 子指令錯誤路徑 exit code 為 **2**(非 1) | `marketplace/yml_editor.py` + `plugin/add.py`/`set.py`/`remove.py` |
| [ ] | `mkt-046` | 實測+源碼 ⚠️修正不移植 | **原版已知缺陷,Go 版本要修正、不要複製**:`package add` 對本地(`./...`)來源無任何特判——`_verify_source` 會把 `./foo` 拼成 `https://{host}/./foo.git` 去 `git ls-remote`(`ref_resolver.py:209` + `github_host.py:378-382`),`_resolve_ref` 也無法離線解析本地 HEAD。**修正先前過強的表述**:繞過方式不只假 SHA——`--no-verify` 會跳過 `_verify_source`(`add.py:63`),且給了 `--version` 時 `_resolve_ref` 直接短路(`plugin/__init__.py:112-114`),`package add ./foo --no-verify --version '>=0.0.0'` 可離線成功。Go 版本仍須特判本地來源(預設路徑就該可用,而非靠 `--no-verify` 繞);回歸測試斷言「本地來源不加旗標即可 add」,不可斷言「假 SHA 是唯一解」 | 研究報告 Part D-5 + `plugin/__init__.py:78-147` |
| [ ] | `mkt-047` | 源碼 | 獨立 `marketplace.yml`(legacy)與 `apm.yml` 內 `marketplace:` 區塊互斥共存 → 硬性報錯,不可兩者都讀 | — |

---

## Phase M4 — `apm pack`(發布端最後一步:產生 marketplace.json)

> 前置依賴:apm-go 目前完全沒有 `apm pack` 指令,此 Phase 需要從零實作,建議獨立成一個子任務,由 Phase M3 完成、有實際 `marketplace:` 區塊可用之後再開始。

| ✓ | id | 權威 | 驗證內容 | 對照 |
|---|----|----|----------|------|
| [ ] | `mkt-050` | 源碼+實測 | **改寫(原「packages→plugins 唯一轉換」敘述錯誤)**:builder/mapper 做多項欄位級轉換 —— (a) `packages:`→`plugins:` 改名;(b) 遠端套件的 `source` 字串**合成為結構化 dict**(`{source: github, repo}` / `{source: url, url}` / `{source: git-subdir, url, path}`)並附解析後 `ref` 與 40 字元 `sha`(`output_mappers.py:185-201`),`subdir`→`source.path`;(c) `version` 重寫:semver range 不原樣輸出,僅「顯示版本」保留,否則取遠端 apm.yml metadata(`:115-137`,curator 條目值優先);(d) 剝除 APM 專用欄位(build/tagPattern/include_prerelease 等);(e) 本地 source 做 pluginRoot 剝除(`:150-178`) | `marketplace/builder.py` + `output_mappers.py::ClaudeMarketplaceMapper.compose` |
| [ ] | `mkt-051` | 源碼 | 本地套件略過 git 驗證;遠端套件依 semver range 或明確 ref,對照真實 git tag(`git ls-remote`)解析出實際版本/commit | `ResolvedPackage` |
| [ ] | `mkt-052` | 實測+源碼 | **改寫(原欄位清單錯誤)**:Claude 輸出頂層為 `name`、`owner` + 條件式 `description`/`version`/`metadata`(`output_mappers.py:63-77`);plugin 級為 `name`、`source` + 條件式 `description`/`version`/`author`/`license`/`repository`/`tags`/`homepage`(`:89-201`)。**`category` 不出現在 Claude 輸出**——只存在於 Codex mapper(`:261`)。schema 本身(`tests/fixtures/schemas/claude-code-marketplace.schema.json`)是 **informational**、非 OpenAPM 規範性文件,Go 版本只需相容輸出子集,不必完整實作整份上游 schema(hooks/mcpServers/lspServers/channels/userConfig/monitors 等 Claude-Code 原生欄位不在 apm 範圍) | `output_mappers.py` |
| [ ] | `mkt-053` | 源碼 | `outputs` 含 `codex` 時,每個 package 的 `category` 為必填(scaffold 註解已提示);雙層把關:config 載入時 `yml_schema.py:1293-1302` 硬錯誤 + Codex mapper compose 時 BuildError(`output_mappers.py:257-261`);Claude profile 無此要求,確為 codex 條件式 | `output_profiles.py:82` |
| [ ] | `mkt-054` | 源碼 | 輸出位置**不是 repo 根目錄**:claude → `.claude-plugin/marketplace.json`、codex → `.agents/plugins/marketplace.json`(`output_profiles.py:70,79`);outputs 含兩者時**兩份都寫**(`build_orchestrator.py:190-228`);路徑可用 apm.yml `marketplace.<fmt>.output` 或 `apm pack --marketplace-path FORMAT=PATH` 覆寫 | `output_profiles.py` |
| [ ] | `mkt-055` | 源碼 | `apm pack` 的 marketplace 相關閘門與 exit code:0 成功、1 build 錯誤、2 schema 驗證、3 `--check-versions` 版本對齊失敗、4 `--check-clean` 產物 drift(`pack.py:55-61,539-541`);`--offline` 只用快取 refs;branch ref/`HEAD` 觸發 `HeadNotAllowedError` 且 pack **未暴露** allow-head 旗標(呼應 mkt-040 的範本陷阱);`-m/--marketplace` 可過濾輸出格式(`claude,codex`/`all`/`none`) | `commands/pack.py` |

---

## Phase M5 — 文件落差修正(這些是原版文件本身的錯誤,Go 版本不可移植)

> 全部由研究 agent 對照真實 `uv run apm --help` 實測確認。CLI reference 文件(`docs/reference/cli/marketplace.md`)描述的指令不保證都是真的存在的指令,**live CLI 才是 ground truth**。

| ✓ | id | 驗證內容 |
|---|----|----------|
| [ ] | `mkt-060` | `apm marketplace search` **不存在**;真正指令是頂層 `apm search QUERY@MARKETPLACE`(`cli.py:192` 把它掛在根層,不是 `marketplace` 子群組)。**歸類修正:這不是文件錯誤**——`marketplace.md:314` 與 `search.md:13` 都正確指向頂層指令;錯的是原始碼內殘留的 docstring/錯誤訊息字串(`commands/marketplace/__init__.py:1351,1361` 仍寫 `apm marketplace search`)。Go 版本實作成頂層 `apm search`,且 help/錯誤訊息字串**不可照抄**原版殘留 |
| [ ] | `mkt-061` | `apm marketplace doctor` **不存在**(`marketplace.md:29` synopsis 誤列了它——這條才是真的文件錯誤);真正指令是頂層 `apm doctor`。**路徑修正**:helper 在 `commands/marketplace/doctor.py::run_doctor`,被 `commands/doctor.py` 包裝成頂層指令;`src/apm_cli/marketplace/doctor.py` 不存在 |
| [ ] | `mkt-062` | `apm marketplace publish` **完全不存在**,任何形式都沒有(文件裡兩段互相矛盾的旗標清單都是假的:synopsis `marketplace.md:31` 列 `--targets/--dry-run/--no-pr`,範例 `:304-309` 卻用 `--draft`);不要實作這個指令。注意頂層 `apm publish` 是**另一個真實存在**的 registry 上傳指令(`cli.py:169`),勿混淆 |
| [ ] | `mkt-063` | `marketplace browse --json` **不存在**(browse 只收 NAME + `--verbose`);e2e script 裡用到它(`scripts/e2e/marketplace_local_e2e.sh:48`)但那支 script 本身已標記「Not wired into CI」,不是可信來源 |
| [ ] | `mkt-064` | `search.md:70` 的「Related」段落寫了不存在的 `marketplace refresh` 子指令,真名是 `update`(`__init__.py:955`);Go 版文件不可照抄 |

---

## Phase M6(stretch,視時間決定是否納入本輪)— `apm search`

| ✓ | id | 權威 | 驗證內容 |
|---|----|----|----------|
| [ ] | `mkt-070` | 文件+實測 | `apm search QUERY@MARKETPLACE [--limit N(預設 20)]`:與 `marketplace browse` 共用 client 層 fetch,但**快取行為不同**——browse 強制 `force_refresh=True`,search 走快取(`client.py:1001-1009` vs `__init__.py:907`);語法用 `@` 分隔(`rsplit("@", 1)`)查詢字串與目標 marketplace 名稱 |

---

## Phase V(沿用 acceptance-checklist.md 的驗證完整性控制,適用本任務全程)

> 完整定義見 `conformance/conformance-kit/acceptance-checklist.md` 的 Phase V(A-H 八類控制)。本任務額外強調:

| ✓ | 控制 |
|---|------|
| [ ] | 每個 marketplace 子指令至少一個 fixture 用**已存在、含其他無關內容**的 `apm.yml`/`marketplaces.json`(呼應本清單開頭「舊坑 1」),不能只測全新產生的檔案 |
| [ ] | mkt-022(marketplace 查詢不讀本地 apm.yml)、mkt-030(未解析狀態禁止序列化)這類「反直覺/安全不變式」條目,必須有**負向**測試鎖定,不能只驗證 happy path |
| [ ] | mkt-032、mkt-046(修正原版 bug 的兩項)必須各自有一個「原版行為會怎樣壞掉」的回歸測試,證明 Go 版本確實不會重現該缺陷,而不只是「看起來沒問題」 |
| [ ] | mkt-033 的兩個負向測試:(a) apm.yml 字串形式 `pkg@mkt` 依賴必須被拒絕(與原版一致);(b) dict 形式 `version:` 為 semver range 時必須能解析——防止把 mkt-021 的 `#REF` 限制誤植到這條路徑 |
| [ ] | Phase M5(文件落差)各項各自要有一個測試斷言「這個指令組不存在該子指令」(或存在於正確的位置),防止之後有人依文件字面把幽靈指令加回來 |

---

## 每個子任務完成時的自我聲明範本

```
Implemented: apm marketplace <subset>  (Go)
Parity target: apm (Python) v0.21.0
Checklist items covered: mkt-0XX, mkt-0XX, ...
Deliberately NOT ported (see Phase M5/bug-fix items): mkt-060, mkt-061, mkt-062, mkt-063, mkt-064, mkt-032(fixed), mkt-046(fixed)
Deviations from Python original (intentional improvements): <list>
Test evidence: go test ./... -cover 全綠;A/B 對照(如適用);至少一個「已存在檔案」fixture 測試
```

---

_衍生自 Python `apm` v0.21.0 原始碼(`D:\Projects\apm-dev\apm`)+ 文件(`D:\Projects\apm-dev\apm\docs`)+ live CLI 實測。OpenAPM v0.1 規範只涵蓋 `req-mf-017`(marketplace.packages[].source 驗證,producer)一條;「marketplace 列為非規範性依賴類型」出自 §7.1 參照類型清單第 5 項(非 `req-rs-008` 條文本體——該條規範的是依賴分類確定性)。其餘皆為 CLI 行為對齊,非規範要求。2026-07-03 依逐項源碼複審修訂:重寫 mkt-050/052、修正 mkt-011/021/042/046 表述、重新歸類 mkt-060、新增 mkt-006/018/019/033/034/035/054/055/064。_
