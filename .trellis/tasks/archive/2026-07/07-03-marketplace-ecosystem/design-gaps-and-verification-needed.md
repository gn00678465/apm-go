# 缺少的 design.md 與對應查證項目

> 給負責補寫 `07-03-marketplace-install-ref`、`07-03-marketplace-authoring`、`07-03-marketplace-pack` 三份 design.md/implement.md 的模型看。權威清單是 `marketplace-checklist.md`(2026-07-03 複審版,已對 mkt-006/011/033/050/052 等關鍵項做過原始碼逐行抽查驗證)。`07-03-marketplace-consumer` 已有完整 design.md/implement.md,可當作格式與細節深度的範本,但注意:**該份 design.md 是在 checklist 複審修訂之前寫的,尚未回頭核對是否跟上 mkt-006/011/018/019 等修正版行為**——補寫其他三份之前,建議先重新核對並視需要更新 consumer 的 design.md。
>
> 每個子任務底下分兩類查證項目:
> - **(A) 需對照 Python 原始碼/live CLI 驗證** —— checklist 目前只到「讀源碼標行號」深度,尚未像 mkt-033 那樣逐行抽查或 live 實測,寫 design.md 前必須先做,否則會重蹈這次「registry 欄位/`--host` 行為寫反」的錯。
> - **(B) apm-go 既有程式碼可重用/需接線的點** —— 這部分我已經先查過 apm-go 自己的程式碼,直接列出重用點,design.md 應該基於這些既有結構設計,不要另起爐灶重複實作。

---

## 子任務 1:`07-03-marketplace-install-ref`(Phase M2 + M6 stretch)

### (A) 需對照原始碼驗證

1. **mkt-020 的攔截點設計決策**:checklist 已指出原版 `install.py:368` 的 `"/" not in package"` 粗檢查跟 resolver 的 `parse_marketplace_ref`(先切 `#` 再檢查)語意不一致,會讓 `pkg@mkt#feature/branch` 在原版 `install` 失敗但 `uninstall` 能過。design.md 必須**明確決定**:Go 版本要在哪一層做攔截(CLI 參數分派層,還是統一委派給 resolver 層?),並且這個決策要讓 `install`/未來的 `uninstall`/`view`(雖然本輪不做,但語法要一致)都走同一份攔截邏輯,不要在 CLI 層各自重複判斷。這是「刻意 deviation」,design.md 要把取捨寫清楚。
2. **mkt-023 GitHub Contents API 實際回應形狀**:目前只有「dispatch by kind」的描述,沒有像 mkt-033 那樣附精確的欄位驗證。寫 design.md 前**必須**對一個公開、免認證的小型 repo(例如任何有 `marketplace.json` 的公開 repo)做至少一次 live `curl https://api.github.com/repos/{owner}/{repo}/contents/{path}?ref={ref}`,確認:回應是否為 base64(`content`/`encoding` 欄位)、大檔案是否走不同路徑(GitHub Contents API 對 >1MB 檔案行為不同,需確認 marketplace.json 是否可能超過此限制、原版怎麼處理)。
3. **mkt-023 GitLab REST v4 實際回應形狀**:同上,需要確認原版用的是 `/repository/files/{path}/raw`(純文字)還是 `/repository/files/{path}`(JSON 包 base64 content)——這兩種 API 回應處理邏輯完全不同,checklist 目前沒有精確到這個層級,design.md 動筆前要先讀 Python 原始碼裡實際呼叫的那個 endpoint URL 逐字確認。
4. **mkt-026 dual-layer npm 行為的精確映射**:`_coerce_dict_plugin_type` 認 `type`/`source`/`kind` 三個鍵、manifest parser 只認 `type`/`source`——這個雙層差異在 Go 版本要怎麼對應到 `MarketplacePlugin.Source any` 的型別判斷,design.md 需要寫出具體的 Go 型別/case 邏輯,不能只寫「npm 拒絕」帶過。
5. **mkt-028 cross-repo 閘門的 enterprise host 判斷邏輯**:`*.ghe.com` 的判斷規則需要對照原始碼 `CrossRepoMisconfigRisk` 的實際實作(不只是 docstring),確認判斷條件的精確邊界(例如是否也涵蓋自架 GitHub Enterprise Server 的其他網域樣式,不只 `.ghe.com` 這個 SaaS 專屬後綴)。
6. **mkt-032 修法的具體機制**:checklist 已經把原版的資料遺失路徑追到很精確(`SystemExit(2)` 繞過 `except Exception`),但「Go 版本要怎麼修」還沒有具體設計——design.md 需要決定:是要求 `install` 在偵測到 marketplace ref 但缺 target 訊號時直接報錯(不寫入 apm.yml,要求使用者補齊 `--target` 後重跑同一條完整指令),還是把 provenance 暫存到某個中繼狀態跨呼叫保留?兩種設計各自的複雜度與使用者體感需要在 design.md 裡權衡並選一個。
7. **mkt-034/035 的持久化層在哪裡**:「ref-swap 偵測」與「shadow 偵測」需要記錄 `(marketplace, plugin, version) → ref` 的歷史 pin——這份狀態原版存在哪裡(是 lockfile 的一部分、還是像 `~/.apm/marketplaces.json` 一樣的獨立快取檔)完全沒有查證過,design.md 動筆前要先確認 `version_pins.py`/`shadow_detector.py` 的實際儲存機制,再決定 Go 版本要不要對齊同一個持久化位置。

### (B) apm-go 既有程式碼可重用

1. **`internal/manifest/depref.go::ParseDepDict`(280 行起)是 mkt-033 的正確接線點**。這個函式目前用 `keys["id"]`/`keys["git"]`/`keys["path"]` 分支判斷 dict 形式依賴的種類(仿照這個模式,`git: parent` 分支在 320 行附近可以直接當範本)。mkt-033 的 `{name, marketplace, version}` 應該加一個新的 `if keys["marketplace"]` 分支,產生一個新的 `DependencyReference`(需要新增 `IsMarketplace`/`MarketplaceName`/`MarketplacePluginName`/`MarketplaceVersionSpec` 欄位,對齊 Python 的 `is_marketplace`/`marketplace_name`/`marketplace_plugin_name`/`marketplace_version_spec`),**不要**另外寫一個平行的 dict 依賴解析器。
2. **`internal/resolver/diamond.go::pickHighestInIntersection`** 已經有對 `[]semver.TagInfo` 做約束交集挑選最高版本的邏輯,mkt-033 的 `version:` semver range 解析(對真實 tag 求值、無相符時回退原始 ref)應該重用這條既有路徑,不要重新寫一份 tag 比對邏輯。
3. **`internal/manifest/manifest.go:151` 已有 `case "marketplace":` 呼叫 `validateMarketplaceBlock`**——這是 req-mf-017 的既有實作,只驗證 `packages[].source`。install-ref 子任務不需要重新驗證這塊,但要注意:這個既有驗證跟 mkt-033(apm.yml **dependencies.apm** 底下的 marketplace 依賴)是兩個完全不同的資料位置(一個在 manifest 頂層 `marketplace:` authoring 區塊,一個在 `dependencies.apm` 清單裡),design.md 要把這兩者的差異講清楚,不要混為一談。

---

## 子任務 2:`07-03-marketplace-authoring`(Phase M3)

### (A) 需對照原始碼驗證

1. **mkt-041/042 的 exit code 與圖示邏輯**:checklist 已有精確行號(`outdated.py:83,100-104,127-128,158-160`),但這些行號本身還沒被抽查驗證過(這次只抽查了 mkt-006/011/033/050/052 四條)。design.md 動筆前建議至少讀過 `outdated.py` 全文一次,確認圖示分派的完整 if/elif 鏈,而不是只信任行號摘要。
2. **mkt-043 audit 的 NETWORK/PARSE 失敗分類**:「NO_MANIFEST/UNSUPPORTED_SOURCE 算 skipped,不觸發 `--strict` exit 1」這條需要對照 `marketplace/audit.py` 裡完整的失敗分類邏輯,確認還有沒有其他失敗類型未被清單提及。
3. **mkt-045 三個子指令的 atomic write + 回滾機制**:「寫後重新驗證失敗即回滾」的具體實作(是整份檔案先备份再寫、還是記憶體驗證通過才落盤)需要讀 `marketplace/yml_editor.py` 全文確認,這會直接影響 Go 版本要不要重用/擴充 `internal/yamlcore.PatchMappingPath`,還是這個場景的寫入型態(新增/修改/刪除 `packages[]` 陣列元素)跟 `--mcp` 當初的「只加一個 mapping 節點」不同,可能需要新的 patch 原語(例如「替換陣列中特定元素」而非「替換單一 key 的值」)。
4. **mkt-047 legacy `marketplace.yml` 偵測邏輯**:兩者互斥的判斷是「檔案存在即報錯」還是「檔案存在**且**內容非空才報錯」,需要讀源碼確認邊界情況(空的 legacy 檔案怎麼處理)。

### (B) apm-go 既有程式碼可重用

1. **`internal/manifest/manifest.go:132` 已支援 `devDependencies` 解析**——mkt-043 的 audit 指令要求同時掃 `dependencies` 與 `devDependencies`,這條既有支援已經存在,直接重用現有的 manifest 解析結果即可,不需要另外處理 `devDependencies` 的讀取。
2. **`internal/manifest/manifest.go:426 validateMarketplaceBlock` + `ValidateMarketplaceSource`** 是 `init`/`check`/`package add/remove/set` 都要共用的既有驗證邏輯入口——這幾個發布端指令寫入/修改 `packages[]` 後,都應該呼叫這條既有驗證確保沒有違反 req-mf-017,而不是自己重新實作一份 source 驗證。
3. **`internal/yamlcore.PatchMappingPath`(`--mcp` 任務新增,已測試過 CRLF、註解保留兩個邊界案例)**——`init`(插入新的 `marketplace:` 頂層區塊)、`migrate`(把整個 `marketplace:` 區塊從另一份檔案折入)這兩個操作形態接近既有的「插入新 key」場景,可以直接呼叫;但 `package add/remove/set` 是操作**陣列元素**(`packages[]` 裡新增/移除/修改一筆),這是 `PatchMappingPath` 目前**沒有**涵蓋的場景(它只處理「替換/插入一個 mapping key 的值」,不處理「陣列裡插入/刪除一個元素同時保留其餘元素格式」)。design.md 需要明確決定:擴充 `PatchMappingPath` 支援陣列元素操作,還是為這個場景寫一個新的、範圍更窄的 patch 函式。**這是本子任務最大的技術不確定性,值得在 design.md 花最多篇幅講清楚。**

---

## 子任務 3:`07-03-marketplace-pack`(Phase M4)

### (A) 需對照原始碼驗證(這個子任務目前驗證深度最淺,風險最高)

1. **mkt-050 只抽查了 `output_mappers.py:55-90`(top-level doc 欄位),plugin 級的 source 字串→結構化 dict 合成邏輯(`:185-201`)、version 重寫邏輯(`:115-137`)、pluginRoot 剝除(`:150-178`)完全沒有抽查驗證過**——這是全清單裡验证深度最淺、但實作複雜度最高的一條。寫 design.md 前**必須**把 `output_mappers.py` 全文讀過一次(不只是引用的行號區間),原文引用的四個子邏輯每一個都要在 design.md 裡寫出對應的 Go 演算法草稿,而不是只複述 checklist 的摘要句子。
2. **mkt-052 的 plugin 級條件式欄位清單**(`name`/`source` + 條件式 `description`/`author`/`license`/`repository`/`tags`/`homepage`)每個「條件式」欄位的**觸發條件**分別是什麼(哪個資料來源提供、什麼情況下省略),目前 checklist 沒有寫,需要讀原始碼補上,否則 Go 版本很容易漏掉某個條件分支。
3. **mkt-054 的 apm.yml 覆寫語法**:`marketplace.<fmt>.output` 的確切 YAML 路徑形狀(是 `marketplace.outputs.claude.output` 還是別的巢狀路徑?)與 `apm pack --marketplace-path FORMAT=PATH` 的解析規則(FORMAT 值域、多個 `--marketplace-path` 可否重複給),需要讀 `commands/pack.py` 的 flag 定義確認。
4. **mkt-055 的 `--check-versions`/`--check-clean` 語意**:目前只列了 exit code(3/4),沒有描述這兩個旗標具體檢查什麼(版本對齊指的是 apm.yml 宣告版本跟輸出 marketplace.json 裡的版本一致?"drift" 指的是跟已提交檔案比對 diff?),需要讀源碼補上行為描述才能設計對應邏輯。

### (B) apm-go 既有程式碼可重用

1. **`internal/resolver/diamond.go::pickHighestInIntersection` + `internal/semver` 套件**(Phase 2 就有)理論上可以重用於 mkt-051 的「遠端套件依 semver range 對照真實 git tag 解析」,但 pack 情境跟 install-ref 的 resolver 路徑是否共用同一組工具函式,還是各自獨立呼叫,design.md 需要決定(建議共用,避免兩處各自實作一份 semver-tag 比對邏輯,重蹈 `internal/mcpregistry.ResolveDeployable` 抽出共用邏輯的教訓)。
2. **是否需要新的 `internal/markettag`(或類似)套件對應 Python 的 `marketplace/tag_pattern.py`**(`tagPattern: "v{version}"` 這種帶佔位符的 tag 格式樣板)——apm-go 目前**沒有**任何等價的 tag-pattern 樣板解析邏輯(`internal/resolver`/`internal/semver` 都是處理已知形狀的 tag/semver,不處理使用者自訂樣板)。這是一個**全新、目前完全沒有著落**的子元件,design.md 必須把它獨立列出來,不能假設「重用既有套件就夠」。

---

## 跨三個子任務的共通建議

- 三份 design.md 完成後,建議都先過一次跟這次「checklist 複審」一樣強度的獨立抽查(挑 3-5 條影響最大的設計決策,對照 Python 原始碼逐行核對),再進入實作,不要假設「模型讀過源碼寫的 design 就是對的」——這正是這次 session 已經證明過的事:第一輪讀源碼寫出來的東西,關鍵行為方向都可能整個寫反(mkt-006/011)。
- 更正:先前這裡誤寫「`marketplace-consumer` 的 design.md 尚未同步 mkt-011/018/019 修正」——實際核對後(`grep mkt-011/mkt-018/mkt-019 design.md`)確認**已經**同步更新(第 77/79/87/88 行皆為修訂後版本),這條建議本身是我沒查證就下的錯誤結論,已刪除,不要照做。

---

## 查證結果紀錄(2026-07-03,逐項對照原始碼/live 驗證後寫回各 design/implement)

### install-ref
- **A1(攔截層)** ✅ 已決策並寫入 design:單一 `marketplace.ParseRef`,CLI 層禁止重複判斷;resolver 語意(先切 `#`),deviation 記 A/B 例外。
- **A2(GitHub Contents API)** ✅ live curl 驗證 + 源碼(`client.py:384-388,405`):原版帶 `Accept: application/vnd.github.v3.raw` → 回應是**原始檔案內容,無 base64**(無此 header 才是 base64 envelope);raw 型別同時避開 >1MB base64 限制。**consumer design/implement 原寫「base64 解碼」是錯的,已修正**;checklist mkt-023 已精確化。
- **A3(GitLab REST v4)** ✅ 源碼(`client.py:391-400`):`/projects/{urlenc(owner/repo)}/repository/files/{urlenc(path)}/raw?ref=` **raw 純文字端點**,`PRIVATE-TOKEN` header;不是 JSON+base64 形式。已寫入 consumer design。
- **A4(npm 雙層)** ✅ 已寫入 design:resolve 層 `coercePluginType` 認 type/source/kind 三鍵,manifest 層只認 type/source 兩鍵;`kind: npm` 變體在 resolve 層才報錯。
- **A5(enterprise host 邊界)** ✅ 源碼(`github_host.py:170-196`):邊界是 `is_github_hostname && != github.com` = `*.ghe.com` **∪ GITHUB_HOST 設定的 GHES**——checklist 原寫「僅 *.ghe.com」不完整,mkt-028 已加註,design 已含三案例測試要求。
- **A6(mkt-032 修法)** ✅ design 已寫兩案取捨:採「target 解析先於 apm.yml 寫入」(單次原子化,零新狀態);pending 暫存案因引入新狀態源的失效模式而否決。
- **A7(mkt-034/035 持久化)** ✅ 源碼(`version_pins.py:1-65`):pin 存 `~/.apm/cache/marketplace/version-pins.json`,扁平 dict `{"mkt/plugin/version": "ref"}`,鍵 lowercase,fail-open;shadow(`shadow_detector.py`)走 fetch_or_cache 掃其他登錄項、錯誤吞掉。design/implement 已更新(原寫的 `marketplace_ref_pins.json` 路徑已改正)。
- **B1(ParseDepDict 接線)** ✅ design 已指名 `depref.go:280` 分支模式 + 沿用既有 `Source: "marketplace"` 欄位 + 新增三欄位。
- **B2(diamond.go 重用)** ✅ design 決策:用同底層的 `internal/semver.MaxSatisfying`(單一 range 情境),不 export resolver 內部 diamond 函式。
- **B3(兩個資料位置)** ✅ design 已明文區分 `marketplace:` authoring 區塊(req-mf-017 已有 `manifest.go:426` 現成實作,不碰)與 `dependencies.apm` dict 依賴(mkt-033)。

### authoring
- **A1(mkt-041/042 行號)** ✅ 已由本 session checklist 複審的獨立 agent **全文抽查**(`outdated.py`/`check.py` 全讀,含完整 if/elif 鏈)——修訂版 checklist 即該次抽查產物;design 已註記。
- **A2(audit 失敗分類)** ✅ `FetchStatus` enum 確認只有 OK/NO_MANIFEST/UNSUPPORTED_SOURCE/NETWORK_ERROR/PARSE_ERROR 五種,無遺漏;design 已註記。
- **A3(yml_editor 機制)** ✅ 原版 `_write_and_validate`:寫入→重新載入驗證→失敗以備存原文回寫。Go 設計改良為記憶體驗證先於落盤(觀測行為相同)。`PatchMappingPath` 確認**不涵蓋**陣列元素操作 → design 已選定方案:新增窄範圍 `yamlcore.SpliceSequenceElement`(add/remove/set 三 op,byte-span 拼接),fallback 整段 value 替換,絕不整份 re-encode;implement 把「先打通此技術驗證點」列為步驟 5 第一項。
- **A4(mkt-047 邊界)** ✅ 源碼(`migration.py:87-111`):apm 側要求 `marketplace` 鍵**非 null**,legacy 側是**檔案存在即算**(空檔也觸發互斥錯誤);design/implement 已加邊界測試。
- **B1(devDependencies)** ✅ design 已指名重用 `manifest.go:132` 既有解析。
- **B2(ValidateMarketplaceSource)** ✅ design/implement 已改為重用 `manifest.go:426` 現成實作,刪除原「自行實作 req-mf-017 驗證」的寫法。
- **B3(PatchMappingPath 陣列)** ✅ 見 A3——已展開為三選項取捨並選定,標為本子任務最大技術風險。

### pack
- **A1(mkt-050 全文)** ✅ `output_mappers.py` 已全文讀過(:40-423):source 合成四規則(本地字串+pluginRoot 剝除/git-subdir/url(host-prefixed)/github shorthand + ref/sha)、`_is_display_version` 精確規則、curator-wins(`_apply_field_with_precedence`)、`_subtract_plugin_root` 三個錯誤邊界、duplicate-name 警告——design 已改寫為逐欄演算法表。
- **A2(mkt-052 條件式欄位)** ✅ 逐欄觸發條件已入 design 表(含先前漏掉的 `owner.email`、description/version 需 **overridden** 才輸出)。**重大發現:Codex 輸出與 Claude 差異大**——頂層 `interface.displayName`、plugin 級固定 `policy` 區塊、本地 source 是 dict、遠端無 github shorthand;原 design「基礎同 Claude + category」已重寫。
- **A3(mkt-054 覆寫語法)** ✅ 源碼(`yml_schema.py:675-747` + `pack.py`):**兩種** YAML 形式(`outputs.<name>.path` map 形式 + `marketplace.<fmt>.output` 相容形式);`--marketplace-path FORMAT=PATH` 可重複、FORMAT 限 known、PATH 過 traversal 驗證;`APM_MARKETPLACE_*_PATH` env **宣告未實作**(planned v0.15)→ Go 不實作。已入 design/implement。
- **A4(check-versions/check-clean 語意)** ✅ 對照 pack.py 旗標 help:前者驗證 `marketplace.versioning.strategy`(lockstep|tag_pattern|per_package)對齊、exit 3;後者 regen 到暫存路徑 diff 磁碟檔、exit 4、永不寫檔。語意已記入 design 延後註記,供後續任務當行為基準。
- **B1(semver 重用)** ✅ design:重用 `internal/semver`(與 diamond.go 同底層),不重寫比對。
- **B2(tag-pattern 元件)** ✅ design 已獨立列出 `tagpattern.go` 為**全新元件**(apm-go 無對應),並規定 authoring/pack 兩子任務共用同一份、後做者必須重用。

## 第二輪:提升信心度(2026-07-03,獨立 adversarial 抽查 ×2 + 一手追碼 + spike)

### 一手驗證(主 context 親自執行)
- **apm-go `runInstall` 全流程追碼** → **推翻 mkt-032 原設計前提**:Go 無 target 硬閘門、單趟寫 lockfile+apm.yml(`install.go:409,477,668,673`),Python 的中止路徑結構上不存在;真正風險是 `buildLockfile` 從零重建(`:492-551`)導致裸 `apm install` 抹掉 provenance。install-ref design/implement 已改為 **carry-forward 方案**(取代順序重排),回歸測試改為三段式。
- **SpliceSequenceElement spike**(`go.yaml.in/yaml/v4 v4.0.0-rc.6`,22 斷言全過):sequence 元素 byte-span 拼接可行(add/remove/set、序列內註解、CRLF 全數保留)。authoring 最大技術不確定性**降級為已驗證**,span 規則寫入 design。

### Adversarial 抽查 A(install-ref design,4 項)
- ParseRef **NEEDS-AMENDMENT** → 已修:整條輸入先 strip;`pkg@mkt#`(空 fragment)必須 fall through(head-only 重寫漏掉原版整串 regex 的「有 # 則 ref ≥1 字元」約束)。
- mkt-028 邊界 **HOLDS**:GHES-via-GITHUB_HOST 案例逐分支走查確認會觸發;`GITHUB_HOST` 是單一 host 非逗號清單;resolver 的 `_needs_canonical_host_prefix` docstring 與程式碼矛盾(以程式碼為準)。
- mkt-033 **NEEDS-AMENDMENT** → 已修:name 必填(獨立錯誤訊息)、version 選填非空字串且 parse 不驗格式、大小寫保留、RepoURL 用 `_marketplace/<mkt>/<name>` 佔位符(避免空鍵互撞)。
- Go 接線前提 **HOLDS + 兩個地雷** → 已寫入:`depref.go:363` 既有 `name` 分支會 shadow marketplace 條目(**分支必須最先**);lockfile 序列化是五處顯式清單(parse switch/entryFieldOrder/serializeEntry/knownEntryFields/depSemanticEqual),漏 knownEntryFields 會雙重輸出。

### Adversarial 抽查 B(pack design,5 項)
- Claude/Codex 欄位表 **HOLDS**(逐欄重推導比對一致);Codex 對查無 entry 靜默跳過的不對稱 → Go 統一 fail loud。
- **sourceBase 缺口** → 已修:原版 sourceBase 會讓 github.com 也輸出 url 形式;本輪明確延後、ResolvedPackage 補 Host/SourceRepo/Subdir/Tags 欄位。
- tagpattern **HOLDS** + 補精確化:`^...$` 錨定、大小寫敏感、`infer_tag_pattern_from_refs` 只有 outdated 用(pack 不需要)。
- builder 細節 **HOLDS** + 三個註記:SHA regex 只收小寫;同名 tag 優先於 branch(不觸發 HeadNotAllowed);原版錯誤訊息提示不存在的 `--allow-head` 旗標,Go 不可照抄。
- `supports_cli_output_override` **確認是死欄位**(全庫無讀取者、其 docstring 引用的 `--marketplace-output` 旗標不存在),Go 設計忽略它是正確的;codex 的 `--marketplace-path` 覆寫實際可用。
- **exit code 修正**:pack 路徑的 config/schema 錯誤實際 **exit 1**(BuildError→ClickException),非 pack.py docstring 寫的 2;design 已改。
- **tags 合併規則**:`entry.tags` = yml `tags`+`keywords` 合併去重(`yml_schema.py:896-913`)→ 已補進 authoring design 的 PackageEntry 載入規則。
