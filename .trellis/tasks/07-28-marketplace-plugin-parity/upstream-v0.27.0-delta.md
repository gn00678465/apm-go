# 上游基準線位移：v0.26.0 → v0.27.0

**建立日期**：2026-08-03
**觸發**：使用者指出「遭遇版本陷阱應該調整然後明確說明，不應該盲目做下去」，並已將
`D:/Projects/apm-dev/apm` 更新至最新。

## 0. 版本事實（一手，read-only）

```
$ git -C D:/Projects/apm-dev/apm describe --tags
v0.27.0-2-g703dd9e7
$ git show HEAD:pyproject.toml | grep '^version'
version = "0.27.0"
```

HEAD = `703dd9e7`（2026-08-02，= origin/main，detached）。
`git diff --stat v0.27.0..703dd9e7 -- src/` **輸出為空** → HEAD 的 2 個 commit 未觸及
`src/`，因此以 `v0.27.0` 為比對基準即等同於 origin/main。

本專案 parity 目標原訂 **v0.26.0**。差距：

```
$ git diff --stat v0.26.0..v0.27.0 -- src/
102 files changed, 8514 insertions(+), 1963 deletions(-)
```

## 1. 流程檢討：版本陷阱當時應該怎麼處理

先前工作階段中，本機上游 repo 工作區停在 v0.21.0-9，我用 `git show v0.26.0:<path>`
繞過它繼續比對。**繞過是對的，但只做了一半**：我修正了「讀到錯檔案」，卻沒有問
「v0.26.0 本身是不是還是現行基準？」——用一個未經檢查的假設取代了另一個。

正確處理應為三步，缺一不可：
1. **停**——不要用推定的版本繼續比對。
2. **量測落差**——`git describe` + `git diff --stat <目標>..<最新>`，把落差變成數字。
3. **明確說明並取得裁定**——基準線位移是範圍變更，不是實作細節，不該由我默默決定。

我當時只做了第 1、2 步的一半（僅對單一檔案），第 3 步完全沒做。

## 2. 先前結論的重新校驗（哪些仍成立）

以下檔案在 `git diff --stat v0.26.0..v0.27.0 -- src/` 的 102 檔清單中**不存在**，
即 v0.26.0→v0.27.0 未變動，故先前基於 v0.26.0 的比對結論**仍然成立**：

| 檔案 | 先前對應的驗證項 | 狀態 |
|---|---|---|
| `core/target_catalog.py` | explicit-only targets（`agent-skills`/`antigravity` 不進互動選單） | 仍成立 |
| `commands/init.py` | `marketplace init` border 輸出、`_PROMPT_TARGETS_ORDERED` | 仍成立 |
| `deps/plugin_parser.py` | X9（plugin.json 欄位集） | 仍成立 |
| `core/plugin_manifest.py` | G2/U2（disk-first 解析順序） | 仍成立 |
| `utils/helpers.py` | G2/U2（四個候選路徑順序） | 仍成立 |

`marketplace/output_mappers.py` **有變動**（+12），X9 的「零差異」結論因此對
v0.26.0 為真、對 v0.27.0 **已失效**——見下節 M1。

## 3. Marketplace / plugin 面的實質缺口（一手證據）

### M1. `tag_pattern` 未寫入 marketplace.json 的 `source`（producer 側）

上游 `output_mappers.py:262`（claude）、`:372`/`:382`（codex）新增：

```python
def _set_effective_tag_pattern(source_obj, pkg):
    if pkg.effective_tag_pattern:
        source_obj["tag_pattern"] = pkg.effective_tag_pattern
```

來源鏈（一手）：
- `builder.py:121` `ResolvedPackage.effective_tag_pattern: str = ""`
- `builder.py:635`（顯式 ref 路徑）`effective_tag_pattern=entry.tag_pattern or yml.build.tag_pattern`
- `builder.py:777`（version range 路徑）`pattern = entry.tag_pattern or yml.build.tag_pattern`，
  `builder.py:814` `effective_tag_pattern=pattern`
- `yml_schema.py:609` `tag_pattern = raw.get("tagPattern", "v{version}")` — **有預設值**

⇒ `effective_tag_pattern` 在兩條路徑上都**恆為非空**，故 v0.27.0 對**所有 remote
source** 都會輸出 `"tag_pattern": "v{version}"`（除非使用者另行指定）。這是輸出
**形狀**的改變，不是選用欄位。

apm-go 現況：
```
$ grep -n 'tag_pattern\|TagPattern' internal/marketplace/build/mapper.go
（零命中）
```
⇒ **完全未輸出**。這會使 apm-go `pack` 產出的 marketplace.json 與上游同輸入下不一致。

### M2. `tag_pattern` 未從 marketplace.json 讀回（consumer 側）

上游 `models.py:325-330` 於 `MarketplacePlugin` 新增 `tag_pattern: str | None = None`，
並在 `_parse_plugin_entry`（`models.py:459-467`）以 `validate_tag_pattern` 驗證後帶入。

apm-go `internal/marketplace/models.go:240-258` 的 `MarketplacePlugin` 欄位為
`Name / Source / Description / Version / Tags / SourceMarketplace / Registry`
——**無 TagPattern**。

### M3. `tag_pattern` 驗證規則收緊（行為變更，非新增）

- v0.26.0 `yml_schema.py:558`：`if not any(ph in pattern for ph in _TAG_PLACEHOLDERS)`
  → 只要含 `{version}` **或** `{name}` 任一即通過。
- v0.27.0 `tag_pattern.py:validate_tag_pattern`：
  - 非字串或空白 → `TagPatternError`
  - 出現 `{version}`/`{name}` 以外的 `{...}` placeholder → 拒絕
  - `{version}` 出現次數 **必須恰為 1** → 拒絕 0 次或 2 次

⇒ `tagPattern: "{name}"` 在 v0.26.0 合法、v0.27.0 非法。

apm-go 現況：`grep -n TagPattern` 遍歷 `authoring/schema.go` / `build/builder.go` /
`build/errors.go` / `authoring/editor.go` / `authoring/refcheck.go`，
全部只是 `scalarString` 讀取、預設回填、傳入 `tagpattern.Compile`
——**任何層級皆無驗證**。

失效模式（推導自 `internal/marketplace/tagpattern/tagpattern.go:33-54`）：
pattern 若不含 `{version}`，`Compile` 產出的 regex 沒有 `version` 具名群組，
`ExtractVersion` 的 `re.SubexpIndex("version")` 回傳 -1 → 對**任何** tag 都回
`("", false)` → `FilterTags` 全數丟棄 → 最終呈現為「找不到符合版本」而非
「你的 tag_pattern 寫錯了」。**fail-silent，錯誤訊息指向錯的地方。**
（註：此為原始碼推導，尚未實跑重現。）

### M4. `build_tag_regex` 的 version 捕捉形狀不同

上游 `tag_pattern.py:build_tag_regex` 的 `{version}` 展開為 semver 形狀：
`\d+\.\d+\.\d+(?:-…)?(?:\+…)?`
apm-go `tagpattern.go:43` 展開為 `(?P<version>.+)`（貪婪任意字元）。

此差異**在 v0.26.0 即已存在**，非 v0.27.0 引入；記於此以免與 M1–M3 混淆。
未評估其實際影響面。

## 4. targets 面的變更（影響 07-29-targets-init-shape）

### T1. 空白 token 的 targets 現在會報錯

`core/apm_yml.py` 新增（v0.27.0）：
```python
tokens = [str(t).strip() for t in raw if str(t).strip()]
if not tokens:
    raise EmptyTargetsListError(_EMPTY_TARGETS_MESSAGE)
```
⇒ `targets: ["", "   "]` 從「靜默通過」變成 `EmptyTargetsListError`。

### T2. `target` 與 `targets` 並存改為解析期拒絕

`models/apm_package.py:561-566`：原本並存時兩者都解析、`canonical_targets = ()`，
把衝突延到 install 期；v0.27.0 改為呼叫 `parse_targets_field(data)` 直接拋錯，
後接 `raise AssertionError("unreachable: conflicting target keys were accepted")`。
配合 `canonical_package_target_config` 保留兩個鍵，使 canonical parser
「在任何 MCP 寫入之前」就攔下。

### T1/T2 的 apm-go 對應行為（已查證，2026-08-03）

**T1 = 真缺口。** `internal/manifest/manifest.go:292-297` 的 doc comment 明文記載舊行為：

> blank elements are filtered without re-checking emptiness (matches Python: only
> `targets: []`/null errors, an all-blank list quietly resolves to zero targets)

`parseTargetsField` 只在 `!!null`（:299）與 `len(Content)==0`（:302）兩種情況報錯，
其餘交給 `validateTargetTokens`（:323-334），該函式對全空白輸入回傳 `nil, nil`。
⇒ `targets: ["", "  "]` 目前靜默通過，需補上「過濾後為零 → 報錯」。

修正範圍限 `parseTargetsField`，**不可動 `validateTargetTokens`**：後者同時被
`parseTargetField`（單數，:255-275）使用，而上游 v0.27.0 未變更 `parse_target_field`，
單數路徑的 `target: ""` → nil 行為必須保留。

**T2 = 已對齊，無需修改。** `internal/manifest/manifest.go:91-96`：

```go
// mutex check runs before either key's value is read (apm_yml.py:53-58)
if hasConflictingTargetKeys(root) {
    return nil, nil, fmt.Errorf("apm.yml must not define both 'target:' and 'targets:'; use only one")
}
```

apm-go 只有單一解析路徑且該路徑已在解析期拒絕，等同 v0.27.0 統一後的行為。
上游之所以需要改，是因為它有兩條路徑（`apm_yml.py` 的 mutex 與
`apm_package.py:561` 的寬鬆分支），v0.27.0 才把後者收攏。apm-go 無此分裂。
`manifest_test.go:432-469` 已鎖定此行為（錯誤字串以獨立字面量複製，防止措辭漂移）。

## 5. 尚未掃描的範圍（明確聲明）

102 個變更檔中，本次只掃了 marketplace / targets 面（約 13 檔）。
**未掃描**：`install/`（約 25 檔）、`integration/`（10 檔）、`adapters/client/`（6 檔）、
`deps/`（12 檔）、`core/auth.py`、`security/`、`policy/`、`commands/uninstall/`。
這些落在目前 parity 任務範圍外，但屬於專案整體的上游落差，**不得以「已比對完成」概括**。

## 6. 裁定與現況

**使用者裁定（2026-08-03）**：parity 基準線**抬至 v0.27.0**，範圍限本文件已查證的
M1–M3 + T1（T2 已對齊、M4 排除、其餘約 89 檔未掃描）。

**實作現況：尚未開始。**

一度建立過任務 `08-03-upstream-v0270-delta` 並派工實作，但**該任務與派工均未取得
使用者指名許可**，經使用者指正後已全部回退：任務目錄刪除、parent `task.json`
還原、R4 的程式碼改動還原。本文件與 `checklist.md` 的 X9 校正、
`claim-evidence-guide.md` 的「隱含基準」小節予以保留（屬使用者要求的「明確說明」）。

⇒ 後續要動工，需使用者**指名許可**「建立任務」與「派遣 subagent」兩件事
（見 `AGENTS.md`「四類需指名許可的動作」）。本文件在那之前只作為證據存放，
不代表任何工作已排程。

**回退時已驗證的一項事實**（供將來實作參考，非結論）：R4 的修正一度實作並跑出
RED→GREEN（`targets: ["", "   "]` 修正前靜默通過、修正後報錯，`internal/manifest`
覆蓋率 88.4%），但該實作漏做約定的突變測試，且已隨回退移除。**不得視為已驗證**，
將來重做時必須從 RED 重新開始。
