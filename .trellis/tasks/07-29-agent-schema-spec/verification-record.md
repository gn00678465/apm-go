# 驗證紀錄 — 07-29-agent-schema-spec

> **狀態：待使用者驗證。任務未標記完成。** `task.py finish` 未執行。

- 實作者：`trellis-implement`（兩輪：初版 + AS4 缺陷修正）
- 驗證者：主 session（獨立重跑與 mutation，不採信子代理回報）
- 外部稽核：codex `exec -s read-only`（進行中，結果見文末）
- 日期：2026-07-31

---

## Tier 1

```
pwsh -File .trellis/tasks/07-29-agent-schema-spec/verify.ps1
→ TIER 1 GREEN · AS1–AS7 + AC-L9 · 覆蓋率 87.0%
```

主 session 於子代理兩輪回報後各親跑一次，兩次皆 GREEN（非轉述）。

## 主 session Tier-2 抓到的實質缺陷（第一輪收貨）

**AS4 coupled oracle**：子代理初版的 golden 全部由 apm-go 自己的
`Compose`/`ToJSONValue` 生成 —— schema 與 golden 出自同一套 Go 型別，
互相驗證證明不了 AS4 原文「把 research 裡的**上游實跑產物**餵進去要通過」。

具體反例（主 session 親自核對）：上游 D1 逐字產物
（`eval-real-run-20260728.md:243-261`）的 claude plugin 含
`"category": "Productivity"`，而初版 `apm-claude-marketplace.schema.json`
是 `additionalProperties:false` 且無 `category` → 餵進去必然 validate 失敗。
Tier 1 閘門看不到這件事（它只驗測試名稱與通過），是 Tier-2 收貨查出來的。

修正（第二輪）：4 份 `upstream-*.golden.json`（D1/D2 逐字、D3 欄位集合）
+ claude schema 加 optional `category` + 防漂移白名單 `{"category"}`
+ 白名單過期反向斷言 + spec 文件記錄分歧。

## mutation 測試（四個漂移方向全部實際否證過）

| # | 執行者 | Mutation | 結果 |
|---|---|---|---|
| 1 | 子代理 | `ClaudeOwner`（Go 型別）加欄位 | ✅ `TestSchemaDrift_.../ClaudeOwner` 紅，還原後綠 |
| 2 | 子代理 | claude schema 單獨加 property | ✅ Drift 與 Sync 兩測試皆紅，還原後綠 |
| 3 | 子代理 | schema 拿掉 `category` property（upstream golden 不動） | ✅ `TestSchemaGolden_UpstreamClaudeMarketplace...` 紅 —— 證明上游產物那條路真的在被測 |
| 4 | **主 session** | spec 文件表格刪 `category` 列 | ✅ `TestSchemaSync_.../claude_marketplace` 紅：`only in schema: [category]` |
| 5 | **主 session** | `ClaudePlugin`（Go 型別）加回 `Category` 欄位 | ✅ 白名單過期斷言紅：`whitelist entries [category] now also exist in the Go type's json tags` |

5 之後 `git checkout` 還原，`SchemaDrift|SchemaSync` 全綠，
`git diff` 確認 `mapper.go` 與 spec 文件無殘留改動。

## 需要使用者裁定的 parity 分歧（本 task 不擅自處理）

上游 apm 0.26.0 的 claude `marketplace.json` **會輸出 `category`**
（`eval-real-run-20260728.md:263` 明文 + `:243-261` 逐字產物）；
apm-go 的 `ClaudeMapper` **刻意不輸出**（07-03 `marketplace-pack` mkt-052
修訂版裁定「`category` 只出現在 Codex 輸出」，`mapper_test.go:561` 鎖住）。
當時裁定依據的是對上游原始碼的解讀，與 07-28 實跑證據矛盾。
是否補回 `category` 對齊上游 = 產品決定，**未裁定**；
schema 以「optional + 白名單」同時容納兩邊，裁定後只需刪白名單一條即可轉紅提醒。

## 其他核對

- `verify.ps1` 未被實作方修改（`git diff --stat` 空）
- `git diff 3e450dd -- go.mod go.sum` 空 → AC-L9
- `pluginjson.go` 只加 documentation-only json tag；全 repo 唯一對
  manifest 型別的 `json.Marshal` 呼叫在 `client_local_test.go:17`，
  對象是 `marketplace.MarketplaceManifest`（不同型別），行為不受影響
- upstream golden 與 research 原文逐字比對一致（主 session 親自 diff）

## 外部稽核（codex）

### 第 1 輪（2026-07-31）：BLOCKING 4、MAJOR 2、MINOR 1

> 主 session 操作失誤：以 `tail -120` 擷取輸出，BLOCKING 第 1 條被截掉。
> 已核價的可見項全部成立；截掉的項目由收斂輪重新稽核補回（下輪改為完整落檔）。

| 級別 | 發現 | 主 session 核價 |
|---|---|---|
| BLOCKING | 防漂移只比欄位名——schema 把 `owner.email` 改 `integer` 全綠（golden 無此欄、名稱集合不變）；白名單可三處同步擴張而不紅 | 成立。修法：Kind↔type 逐欄比對 + golden 全欄位覆蓋 + 白名單鎖死為 `{"category"}` |
| BLOCKING | plugin.json 防漂移比的是 documentation-only json tag，真正的序列化器是手寫 `ToJSONValue`——`authorValue` 改成輸出字串，靜態 golden 與 substring 測試皆綠 | 成立（coupled-oracle 殘餘變體）。修法：live-output 測試呼叫真 `ToJSONValue` validate |
| BLOCKING | plugin 兩份「upstream」golden 是推導 fixture 非逐字（codex 可執行核對：research 全檔僅 2 個 json fence，皆為 marketplace D1/D2）；且餵兩個 schema 都 trivially 通過，未驗 mcpServers 差異 | 成立。修法：改名 derived-\*、spec 誠實記載 AS4 字面對 plugin 家族不可滿足、補 claude-golden 餵 copilot schema 必須 FAIL 的負向測試 |
| MAJOR | policy 未鎖 enum（`"BROKEN"` 可過）；remoteSource 只 required `source`（`{"source":"url"}` 缺 url 可過） | 成立。修法：enum + per-variant oneOf required（以 Go 實作不變量為準） |
| MAJOR | spec 第五欄「上游出處」多格只寫本地變數（`entry.Name`）甚至空白，不符 R1.2 | 成立。修法：以 research 引用的上游 file:line 補齊 |
| MINOR | spec :103 local-source 引用範圍錯（`mapper.go:214-244` 只有 remote；local 在 `:175`） | 成立。修正行號 |

codex 未找到反例的項目：AS2、AS3、AS4 marketplace 部分（逐字相等，LF 正規化後
496/463 bytes）、AS5（三個負向各自否證不同規則）、既有欄位集合比對、AC-L9
（codex 本次親跑 `git diff` 為空）。AS7 codex 沙箱無法重跑（Access denied），
以主 session 親跑為準。

## 第 3 輪實作（codex 第 1 輪的 6 項修正）與主 session 複驗

實作方回報 6 項全修；主 session 逐項複驗：

- 閘門親跑 GREEN（第 3 輪後兩次）
- 抽查到位：`derived-plugin-*.golden.json` 改名、policy enum 鎖定
  （`"enum": ["AVAILABLE"]`/`["ON_INSTALL"]`）、兩份 marketplace schema 各 3 個
  `oneOf`、`TestSchemaGolden_LiveOutput_PluginClaude/Copilot` 存在

### mutation 測試（主 session 獨立，第 3 輪後）

| # | Mutation | 結果 |
|---|---|---|
| C | `authorValue` 改回傳 `StringValue(a.Name)`（真序列化器） | ✅ `SchemaGolden_LiveOutput` 紅（`expected object, but got string` 形狀），還原後綠 |
| D | claude schema `owner.email` 型別改 `integer`（codex 第 1 輪的逐字反例） | ✅ **兩層獨立**轉紅：豐富化 golden validate 紅 + 新型別比對 drift 紅（`Go Kind string implies schema type "string", schema declares "integer"`），還原後綠 |

### 主 session 操作失誤（誠實記錄）

Mutation C 的還原用了 `git checkout -- pluginjson.go`，把子代理**尚未 commit**
的 json tag 修改一併洗掉（bundle drift 測試立即紅：`field Author.Name has no
usable json tag`——防線本身把這次誤傷抓出來了）。主 session 依第 1 輪 diff 逐字
重建，`go test ./internal/pack/bundle/ -count=1` 復綠，閘門重跑 GREEN。
教訓：對含未 commit 變更的檔案做 mutation，還原必須用保存的原文，不能用
`git checkout --`。

## 外部稽核（codex）第 2 輪：BLOCKING 5、MAJOR 1、MINOR 0

codex 本輪以 node_repl 做記憶體內 mutation（不改檔），發現全數集中在
**防漂移機制的縱深**，不再是第 1 輪那種「檢查根本不存在」級別：

| # | 發現 | 主 session 核價 |
|---|---|---|
| B1 | plugin 家族的上游逐字產物仍不存在（改名 derived 只修了冒名，AS4 字面仍缺輸入） | 成立。**主 session 找到解法**：eval 工作目錄仍在磁碟（`D:/Projects/apm-dev/evals/apm-20260728T140015Z-1-001/`），親自讀出兩份逐字產物，AC 不用改 |
| B2 | 白名單鎖名不鎖位：owner 加 category + 該 case 加白名單 → 聯集不變全綠 | 成立。修法：逐 case 精確映射鎖死 |
| B3 | oneOf required 取交集後只做子集檢查：github branch 拿掉 required repo → drift 不動、schema 已弱化 | 成立。修法：per-variant 缺欄位負向測試（資料驅動） |
| B4 | live-output 只比頂層鍵：authorValue 丟掉 email/url append 仍綠 | 成立。修法：live 輸出與 committed golden 整棵樹比對 |
| B5 | spec↔schema 只同步欄位名，型別/必填/enum 欄不同步 | 成立（超出 PRD R3.1 字面）。修法：解析型別欄+必填欄比對；散文欄（預設值/出處/enum 文字）明寫不機器同步 |
| M1 | codex url variant 允許 mapper 永不輸出的 repo；「每個 optional 都被 golden 行使」對 codex repo 不成立 | 成立。修法：拿掉該 property + goOnlyAllowed 白名單 + 負向測試 |

codex 同輪確認已收斂：6 項第 1 輪修正全部存在且有效（各附否證嘗試）、
D1/D2 upstream fixture 正規化後逐位元組相同、AS5 三反例各自否證不同規則、
AC-L9 codex 親跑為空。第 1 輪被截斷的項目本輪完整重查，無遺漏項復發。

## 第 4 輪實作（codex 第 2 輪的 6 項修正）與主 session 複驗

實作方回報 6 項全修（含它自己做的兩個 mutation：github required 拿掉 repo →
負向紅/drift 不動；authorValue 丟 email/url → live-output 整棵樹比對紅）。
附帶修正一個既有 golden 缺陷：`apm-plugin-copilot.golden.json` 原先缺 `license`
（第 1 輪「由真 ToJSONValue 生成」的宣稱對這份不精確），整棵樹比對逼出後補正。

主 session 複驗：

- 閘門親跑 GREEN
- 上游 plugin golden 與 eval 原檔 `diff` **逐位元組一致**（init/pack 兩份）
- AS5 頂層負向測試 5 個（≥ 閘門 -minCount 3）

### mutation 測試（主 session 獨立，第 4 輪後）

| # | Mutation | 結果 |
|---|---|---|
| E | spec 表格 `license` 型別欄 string→object | ✅ `SchemaSync_SpecMatchesSchemaTypesAndRequiredness` 紅（`spec 型別 "object" implies schema type "object", schema declares "string"`），還原後綠 |
| F | codex schema url branch required 拿掉 `url` | ✅ `SchemaReject_RemoteSourceVariants/codex_url_missing_url` 紅（預期 validation error 未出現），還原後綠 |

還原後 `git diff --stat` 無殘留、全部 Schema 測試綠。

累計 mutation 總表：**11 個**（主 session 5 + 實作方 6），涵蓋：Go 加欄位、
schema 加/減屬性、spec 表格刪列、spec 型別欄改值、白名單過期、真序列化器
（author 形狀 + 巢狀欄位丟失）、per-variant required 弱化（claude/codex 各一）、
上游 golden 對 schema 的 category 路徑。每個都有紅→還原→綠的完整循環。

## 外部稽核（codex）第 3 輪：BLOCKING 3、MAJOR 1、MINOR 0

發現面持續收窄（第 1 輪「檢查不存在」→ 第 2 輪「檢查太淺」→ 第 3 輪「分支死角」）：

| # | 發現 | 主 session 核價 |
|---|---|---|
| B1 | copilot schema 的型別/必填完全在 drift 檢查外（只被扁平欄位集合比對）——required 加 version 全綠 | 成立。修法：copilot ≡ claude−mcpServers 遞迴深度錨定 |
| B2 | oneOf 分支契約三逃逸：required 加嚴（github+ref）不被抓；後分支型別弱化（git-subdir url→`{}`）取第一分支型別；spec 選填→必填 flip 過交集邏輯 | 成立。修法：per-variant 最小正向表 + 錯型別負向表 + spec 必填欄 ⇔ 交集同步 |
| B3 | spec 型別解析 HasPrefix 靜默吞錯字（`stringly-not-a-type`→string；`string 或 integer`→skip） | 成立。修法：精確詞彙表 fail-closed |
| M1 | goOnlyAllowed 理由宣稱兩 branch 拒 repo，負向只測 url branch | 成立。修法：git-subdir 補 repo-forbidden 負向 |

同輪確認已收斂：6 項第 2 輪修正全部有效（上游 plugin fixture SHA-256 逐位元組
比對、白名單逐 case 映射移位會被擋、8 個 per-variant 負向全部拒絕、整棵樹比對
無新脆弱性、12 張表 54 欄位列解析無未識別文字、codex 兩 branch 均無 repo）；
AS1–AS5 通過（10 組正向配對、12 個負向情境獨立驗證）；AC-L9 codex 親跑三種
diff 皆空。AS6 因上述三反例未通過。

## 第 5 輪實作（codex 第 3 輪的 4 項修正）與主 session 複驗

實作方回報 4 項全修，含它自己的 4 個 mutation（copilot required+version、
github required+ref、git-subdir url→`{}`、spec 型別欄 garbage → 各自對應的
新防線紅）。主 session 複驗：

- 閘門親跑 GREEN
- AS5 頂層負向測試 6 個（↑1，`-minCount 3` 不退步）

### mutation 測試（主 session 獨立，第 5 輪後）

| # | Mutation | 結果 |
|---|---|---|
| G | copilot schema **巢狀** `author.email` 型別 string→integer | ✅ 深度錨定紅（完整 shape diff），證明遞迴真的到 $defs.author 層；還原後綠 |
| H | spec `repo` 列選填→必填 flip（codex 反例 c 原樣） | ✅ `spec says required=true ("必填"), schema required list says false` 紅；還原後綠 |

還原後全 Schema 測試綠、working tree 無殘留。

累計 mutation 總表：**17 個**（主 session 7 + 實作方 10），每個都有
紅→還原→綠完整循環，覆蓋防漂移機制的全部維度：欄位名（Go/schema/spec 三向）、
型別（頂層與巢狀、schema 側與 spec 側）、required（加嚴與弱化兩方向、
oneOf per-branch、spec flip）、白名單（過期與移位）、真序列化器（形狀與巢狀
欄位丟失）、上游產物路徑（category）。

## 外部稽核（codex）第 4 輪：BLOCKING 4、MAJOR 1、MINOR 1

| # | 發現 | 主 session 核價 |
|---|---|---|
| B1 | 型別詞彙表 `HasPrefix("string（")` 仍可括號逃逸——第 5 輪「fail-closed」宣稱的直接反例 | 成立。修法：封閉字面量集合（exact match） |
| B2 | schemaShape 不投影 items/oneOf/$ref——巢狀 enum、items 弱化、$ref 別名替換全綠 | 成立。修法：遞迴投影 + $ref 就地解析 + 未認識鍵 fail-closed |
| B3 | 錯型別負向只測代表欄位——git-subdir `ref:{}` 可過 | 成立。修法：per-branch × per-property 程式化生成 |
| B4 | schema enum 擴張（AVAILABLE+BROKEN）spec 不動仍綠——踩 R3.1 第 2 條字面 | 成立。修法：spec enum 精確記法 + 程式化走訪 schema 全部 enum 比對 |
| M1 | build 側缺 live compose 測試（bundle 有 live ToJSONValue，marketplace 的 minimal 正向是硬編碼文件）；退化輸入（空 SourceRepo）compose 輸出 schema 拒絕的形狀 | 成立。修法：兩個 mapper 的 live compose→validate 測試；退化情境記為前置條件不改產品碼 |
| MINOR | spec 對 parser 的描述過期 | 成立。同步 |

同輪確認：第 3 輪 4 項修正全部有效（各附否證）；AS1–AS5、AC-L9 通過
（fixture 逐位元組 159/139 bytes、三種 git diff 皆空）；AS6 被上述反例推翻。

## 停損準則（主 session 預先登記，2026-07-31）

觀察：第 3、4 輪的 BLOCKING 已全部集中在**防漂移驗證器自身的投影深度**——
這在對「擁有驗證器寫入權的未來修改者」的威脅模型下是無底的
（任何有限投影都存在更深的逃逸路徑）。為避免「無限打磨」與「偷懶收尾」兩種
失敗，預先登記停損準則：

**第 6 輪實作完成、codex 第 5 輪稽核後：**
- 若仍有「功能級/AC 字面級」BLOCKING（如本輪 B4 踩 R3.1 字面、M1 缺 live
  compose）→ 繼續修。
- 若剩餘 BLOCKING 全部屬「防漂移驗證器對蓄意多點竄改的縱深」species（即
  AC 字面已滿足、所有已知單點漂移路徑已鎖、反例需要同時改動驗證器自身或
  多處配合）→ 停止打磨，以「AC 字面滿足 + N 輪強化 + 殘餘威脅模型明文記錄」
  的形式交付使用者裁定。理由：repo 內一致性檢查器在攻擊者可改檢查器的
  威脅模型下不可能自我完備，最終權威是 code review，不是更深的投影。

## 第 6 輪實作（codex 第 4 輪的 6 項修正）與主 session 複驗

實作方回報 5 項全修（1a+1b 合一機制），含 4 個 mutation（spec 型別欄 garbage、
enum+BROKEN、copilot 巢狀 oneOf、git-subdir `ref:{}` → 各自新防線紅）。
主 session 複驗：閘門親跑 GREEN；AS5 頂層負向 6 個不退步。

### mutation 測試（主 session 獨立，第 6 輪後）

| # | Mutation | 結果 |
|---|---|---|
| I | copilot `author` 的 `$ref` 換成 inline `{"type":"object"}`（codex 第 4 輪 $ref 別名反例的簡化版） | ✅ 深度錨定紅（$ref 就地解析後 shape diff），還原後綠 |
| J | spec 側 enum 擴張 `AVAILABLE`→`AVAILABLE、LOCAL_ONLY` | ✅ 撞封閉字面量集合 fail-closed 紅（錯誤訊息明示「add an exact literal, do not loosen to prefix」），還原後綠 |

累計 **21 個 mutation**，全部紅→還原→綠。

## 外部稽核（codex）第 5 輪（判定輪）：BLOCKING 4（全非縱深）、MAJOR 0、MINOR 0

依停損準則：4 條皆屬功能級/單點漂移 → **繼續修**，未觸發停損。

| # | species | 發現 | 主 session 複核 |
|---|---|---|---|
| B1 | 功能級 | spec「空 SourceRepo 被上游驗證擋下」是錯誤宣稱——`authoring/schema.go` 的 `if source != ""` 跳過驗證 | **主 session 端到端重現**（見下）。spec 改口 + 產品缺陷候選記入裁定清單 |
| B2 | 單點漂移 | family 扁平聯集遮蔽單表刪列（刪 ClaudeOwner.name，同名欄位在他表遮蔽） | 成立。修法：逐子表對逐 schema 節點 |
| B3 | 單點漂移 | items/additionalProperties 未鎖（tags.items→`{}`、additionalProperties→true 全綠） | 成立。修法：結構不變量程式化走訪 |
| B4 | 單點漂移 | type 陣列擴張（`["string","boolean"]`）逃逸 | 成立。修法：type 必須單一字串 fail-closed |

### B1 主 session 端到端重現（2026-07-31，真 binary）

```
apm.yml: packages: [{name: ghost-pkg, source: "", ref: aaaa…(40)}]
bin/apm-go.exe pack → 成功（僅 license/metadata 警告；中途嘗試 clone https://github.com/.git）
產物 .claude-plugin/marketplace.json 的 plugin.source =
  {"source":"github","ref":"aaa…","sha":"aaa…"}   ← 缺 repo，schema 正確拒絕
```

### 需要使用者裁定（追加）：空 source 的載入層驗證

上游 `yml_schema.py` 的 `SOURCE_RE` 對空字串不匹配（會報錯）；apm-go
`authoring/schema.go`（`if source != ""` 跳過 `ValidateMarketplaceSource`）
讓空 source 通過並產出 Claude Code 無法消費的畸形 entry。修法一行
（拿掉空字串豁免）但屬產品行為變更、且涉及 `marketplace-add-fixes` 已交付
範圍 → 待使用者裁定後另行處理。

同輪確認：第 4 輪 5 項修正全部有效（封閉字面量 11 種 0 未識別、bundle 投影含
$ref/items/oneOf/未知鍵 fail-closed、18+2 程式化負向、live Compose 真呼叫）；
AS1–AS5、AC-L9 通過（plugin fixture SHA-256 與原檔逐位元組同）。

## 第 7 輪實作（codex 第 5 輪的 4 項修正）與主 session 複驗

> 過程插曲：第 7 輪首次執行因 session 限額中斷於讀碼階段——主 session 驗證
> 工作樹無半成品（build 綠、全 Schema 測試綠）後原單續派，完成。

實作方回報 4 項全修，含 4 個 mutation（刪 ClaudeOwner.name 列、tags.items→`{}`、
additionalProperties→true、ref.type→陣列 → 各自新防線紅）。主 session 複驗：
閘門親跑 GREEN。

### mutation 測試（主 session 獨立，第 7 輪後）

| # | Mutation | 結果 |
|---|---|---|
| K | spec **檔尾**（family 段落外）加 bogus 欄位表 | ❌ **綠——逃逸**。子代理「discovers any unmapped ### sub-table」宣稱過寬：掃描只涵蓋三個 family 段落內 |
| K' | 同 bogus 子表插在 Claude 段落**內** | ✅ 紅，fail-closed 訊息正確 |
| L | bundle claude schema `keywords.items.type` 改 `["string","number"]` | ✅ 深度錨定紅，還原後綠 |

K 的逃逸已界定 species（單點漂移：未來新增 `## 新 family` 段落帶表格會靜默
脫離同步）並發回微修（掃描改全檔 + 非欄位表顯式豁免清單）。

### 主 session 操作失誤（第二次，誠實記錄）

K/L 第一次執行時用 `git checkout --` 還原 **untracked 新檔**——checkout 直接
報錯（幸未誤傷），但 `&&` 鏈斷裂導致 bogus 表殘留 spec、L 未執行，且第一次
K 的「綠」觀測混在損壞的指令鏈裡不可信。以 python 精確剝除殘留、改用 cp
備份重做後才得到上表的可信結果。教訓（第二次同類）：**mutation 還原一律
cp 備份；`git checkout --` 對 untracked 檔無效、對含未 commit 修改的檔會誤傷。**
已把此警告寫進發給實作方的微修單。

## 第 7.5 輪（K 逃逸微修）與主 session 複驗

實作方：`TestSchemaSync_AllFieldTablesAreMapped` 改**全檔掃描**（欄位表表頭
regex 全文匹配 + 最近前置標題歸屬 + specSchemaCases/foreignSubHeadings 雙套件
互認），並附帶修正 CJK 標題直接當 t.Run 名稱導致 `go test -json` 在 Windows
PowerShell 下不可解析的問題（subtestLabel 抽 ASCII token）。

主 session 複驗：
- 檔尾 bogus 欄位表 → **兩套件皆紅**（第 7 輪的 K 逃逸已封）；還原後綠
- 閘門親跑 GREEN
- 「2 張不同表頭的表格自然排除」宣稱已核實：那兩張是交叉引用表
  （`| 產物家族 |` 等），非欄位表——排除正確。「發明變體表頭的欄位表」
  歸入縱深 species（殘餘威脅模型）

## 外部稽核（codex）第 6 輪（判定輪）：BLOCKING 3（全單點漂移）

依停損準則：三條均踩 R3.1 字面 → 繼續修，未觸發停損。

| # | 發現 | 主 session 複核 |
|---|---|---|
| B1 | 重複 heading 前綴的第二張欄位表——AllFieldTablesAreMapped 的 HasPrefix 視為已映射、逐表 sync 只讀 first-match 那張 | 成立。修法：逐 occurrence 精確綁定 + 同 case 第二張表即紅 |
| B2 | oneOf 聯集允許欄位偷渡錯誤分支（github 加 path：聯集不變、walker 綠、WrongType 反而幫它測 number） | 成立。修法：per-branch 精確 property 集合字面量 |
| B3 | map value 型別未鎖（author additionalProperties→`{}` 接受 name:42；walker 例外太寬） | 成立。修法：map[string]V 交叉推導 + 例外鎖精確形狀 |

同輪確認：第 5 輪 4+1 修正全部有效（空 source 改口與 schema.go:492 相符、
逐子表刪列即紅、檔尾一般 bogus 表 unmapped=1、walker 三反例各自報錯、
type 陣列兩側都抓）；AS1 十二表逐表集合差為空（含 category/repo 兩個精確
例外）、AS4 八組 golden 通過 + plugin fixture 逐位元組同、AS5 通過、
AC-L9 三種 diff 皆 0 bytes。

## 第 8 輪實作（codex 第 6 輪的 3 項修正）與主 session 複驗

> 過程插曲：實作方於完成修正後、寫 implement.jsonl 前因累積上下文達上限
> （"Prompt is too long"）終止。主 session 直接驗證工作樹：三項修正皆已落地、
> 全 Schema 測試綠、無半成品。日誌由主 session 補記。該子代理上下文已滿，
> 後續若需實作將以 fresh agent + 精簡派工單重起。

### mutation 測試（主 session 獨立，第 8 輪後）

| # | Mutation | 結果 |
|---|---|---|
| M1 | 檔尾加重複 `### owner（ClaudeOwner）` 第二張表 | ✅ 兩個 sync 測試紅，訊息含兩個 byte offset `[5183 28250]` |
| M2 | claude `remoteSourceGithub` 加 `path` property | ✅ `TestSchemaDrift_RemoteSourceBranchExactProperties` 紅 |
| M3 | claude author `additionalProperties` 改 `{}` | ✅ **雙層紅**（結構 walker 精確形狀 + Go map[string]V 交叉推導） |

還原後全綠、無殘留。閘門親跑 GREEN。累計 mutation **28 個**。

## 外部稽核（codex）第 7 輪（判定輪）：BLOCKING 3（單點漂移）、MAJOR 1（功能級）

| # | species | 發現 | 主 session 複核 |
|---|---|---|---|
| B1 | 單點漂移 | enum 只比聯集：branch discriminator enum 擴成 `["github","url"]` 聯集不變 | 成立 |
| B2 | 單點漂移 | 自由物件節點（author/metadata/mcpServers）可加 `required` 等約束——fixture 恰好都有該鍵所以全綠，但合法輸出會被拒 | 成立 |
| B3 | 單點漂移 | Plugin.source 的 oneOf topology 未鎖：可加 `{"type":"number"}` 分支 | 成立 |
| M1 | 功能級 | spec 兩處「範圍外」判斷缺成本估計（claim-evidence guide :30-47 要求） | 成立 |

同輪確認：第 6 輪三項修正全部有效；AS1–AS5、AC-L9 通過；AS6 因上述三反例不通過。

### 主 session 裁定：路線變更（逐投影補洞 → 雜湊封印）

第 5/6/7 輪的 BLOCKING 全是「schema 單點改動、既有投影看不見」同一 species 的
連續變體——逐投影補洞被證明是跑步機（每輪 3-5 條、粒度遞減、無收斂跡象）。
改採**一次性封閉機制**：spec 對照表記錄四份自撰 schema 的 SHA-256，測試比對
實際檔案雜湊。任何 schema 位元組變更而 spec 不動 → 紅 = R3.1 第 2 條
「schema 改了，spec 文件沒跟著改」的**字面完備**實作，此 species 全滅
（含 B1/B2/B3 與所有未來變體）。既有語意投影全部保留，降級為診斷層
（回答「漂移在哪」）。殘餘：蓄意同時改 schema+hash 而不改語意描述 =
spec 編輯，diff 可見，屬縱深（code review 職責）。

## 第 9/10 輪實作（fresh agent）與主 session 複驗

> 收貨插曲：fresh agent 第一次交付**靜默略過主修法**（雜湊封印），只做了三條
> 針對性診斷投影 + MAJOR 成本估計。主 session 收貨時抓到偏離、退回補做——
> 「派工單偏離」模式再次得到收貨檢查的實證。第二次交付補齊。

最終落地：
- 三條診斷投影（per-branch discriminator enum、自由物件節點禁約束
  ＋bundle 補完整結構 walker、interface 欄位 oneOf topology 鎖）——實作方
  對 codex 三條原樣反例驗證雙層紅
- **雜湊封印**：spec 對照表四份自撰 schema 的 SHA-256 欄 +
  `TestSchemaSync_SchemaFileHashesMatchSpec`（build/bundle 互認、缺列/多列/
  不符全紅）；spec 加更新順序告誡（先語意後 hash）
- MAJOR：spec 兩處範圍判斷補成本估計（含 LOC 與 repro）

### mutation 測試（主 session 獨立，第 10 輪後）

| # | Mutation | 結果 |
|---|---|---|
| N | codex schema `title` 字串單一位元組變更（語意無關欄位） | ✅ hash 測試紅，訊息含 spec/actual 兩雜湊與引導文字；還原後綠 |

雜湊封印語意：schema 檔任何位元組變更而 spec 不動 → 紅。
「schema 改了 spec 沒跟著改」的單點漂移 species 自此不可構造
（殘餘：同時改 schema+hash = 可見的 spec 編輯 = 縱深/code review 職責）。
閘門親跑 GREEN。累計 mutation 32 個。

## 外部稽核（codex）第 8 輪（判定輪）：**BLOCKING 0、MAJOR 0、MINOR 1**

codex 本輪結論（scratchpad/codex-r8-full.txt 完整落檔）：

- **雜湊封印對「schema 單檔改動」沒有找到逃逸**——codex 自行重播了缺列/
  重列/大小寫/非法 hex/64 位錯誤值等路徑，並在過程中**自我更正了一次
  中間誤判**（65 位 hex 其實會因 closing backtick 變缺列而紅）
- 第 7 輪三條反例雙層防線確認（診斷投影紅 + bytes hash 變更）
- AS1–AS7 + AC-L9 無阻斷缺口（12 表集合差為空、10 組 golden 通過、
  三負向案例正確、AC-L9 三 diff 0 bytes）
- MINOR 1：malformed 重複 hash 列被靜默忽略——不削弱封印
  （schema 變更時有效列仍 mismatch），但推翻註解「多列全紅」的廣義宣稱。
  已派修（fail-closed 逐列驗證）。
- 殘餘威脅（縱深）：同時改 schema + spec hash 而不更新語意敘述——需雙檔
  蓄意配合，且結構性變更仍可能被診斷投影攔截；最終權威為 code review。

**依預登記停損準則：非縱深 species = 0 → 收斂達成。**

## 第 11 輪（MINOR 收尾）與最終複驗

實作方：hash 表列偵測改「第二欄含 .schema.json 路徑」為準、hash 欄形狀
獨立驗證（malformed 內容逐字入錯誤訊息）。
主 session 最終複驗：malformed 63 位重複列 → 紅（訊息逐字列出兩列 hash 內容）；
還原後全綠、無殘留；閘門親跑 GREEN。

## 終態（2026-07-31）

- **Tier 1**：GREEN（主 session 親跑 10+ 次，最終覆蓋率 87.0%）
- **Tier 2**：codex 8 輪對抗稽核，BLOCKING 軌跡 4→5→3→4→4→3→3→**0**；
  第 8 輪判定 BLOCKING 0、MAJOR 0，非縱深 species 歸零
- **mutation 總計 34 個**（主 session 12 + 實作方 22），全部紅→還原→綠
- **AS1–AS7 + AC-L9 全數通過**，AS6 以雜湊封印（R3.1.2 字面完備）+
  多層診斷投影收斂
- **待使用者裁定**（產品層，本 task 未擅自處理）：
  1. claude marketplace 輸出是否補回 `category`（上游實跑會輸出；
     07-03 mkt-052 裁定不輸出；證據 eval-real-run :243-263）
  2. 是否移除空 source 驗證豁免（`authoring/schema.go:492`；上游 SOURCE_RE
     會拒絕；主 session 端到端重現畸形產物；修約 1 行 + 10-15 行測試）
- **狀態：待使用者驗證。`task.py finish` 未執行。**
