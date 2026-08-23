# OpenAPM v0.1 — CLI 改寫驗收清單(Rust/Go)

> **用途**：把 OpenAPM v0.1 的規範陳述排成「改寫一個 APM CLI 時、依實作依賴階段(build order)逐項驗收」的清單。每一列對應規範裡一條 `req-*` 錨點,勾選代表你的實作已通過該條的驗證。
>
> **權威來源**：`apm/docs/src/content/docs/specs/openapm-v0.1.md`(規範本體,90 條)與 `apm/docs/public/specs/manifests/openapm-v0.1.requirements.yml`(機器可讀清單)。本檔是上述的衍生投影,衝突時以規範本體為準。
>
> **機器可讀對應**:見同目錄 `acceptance-coverage.yml`(可餵 CI 做覆蓋率檢查)。

## 範圍與決定

| 項目 | 決定 |
|---|---|
| Conformance class | **Consumer + Producer + Governance**,共 **89 條**(排除 Registry 的 `req-rg-001`,其 wire 契約 v0.1 保留給 v0.2) |
| `req-rg-001` | **不在範圍**(Registry server 角色) |
| SHOULD 條文 | **納入**,共 5 條(`req-mf-004` / `req-lk-007` / `req-lk-018` / `req-pr-005` / `req-sc-008`),以 `keyword=SHOULD` 標記、不阻擋驗收通過但建議實作 |
| 排序原則 | build order(Phase 0 → Phase 8) |
| v0.2 保留項 | 不驗:簽章/attestation、workspaces、reproducible build、yank、registry HTTP wire、i18n/IDNA |

### Target 政策(本次改寫的關鍵調整)

規範區分兩層,**「增減」只動部署層、不動詞彙層**,因此仍 100% 合規 v0.1:

- **詞彙層(vocabulary)** — `req-mf-005`(Producer 拒絕非 canonical 值)、`req-tg-004`(Consumer 接受 `x-<vendor>-<name>`)。**不變**:parser 仍接受規範 §4.2.1 的完整 canonical set(`copilot, claude, cursor, codex, gemini, opencode, windsurf, agent-skills, all`)。
- **部署層(deploy adapters)** — `req-tg-001/002/003`。**只實作 6 個**:

| 動作 | target | 處理 |
|---|---|---|
| 保留 | `claude`, `codex`, `copilot`, `opencode`, `agent-skills` | 實作完整 deploy adapter |
| **增** | `antigravity` | **plain `antigravity`(已定案,選項B)**。對齊生態/上游(`microsoft/apm#1650`)。代價:§4.2.1 尚未收錄它,故 `req-mf-005` 視為 **documented deviation**(pre-standard extension),須在 conformance statement 列明 |
| **減** | `gemini`(CLI 已死)、`cursor`、`windsurf` | 詞彙表仍接受 `--target X`,但無 adapter → 依 `req-tg-004` 發「unsupported / no registered handler」診斷(**不可靜默忽略**);於 conformance statement 列為 documented limitation |

> ⚠️ **`.agents/` 競用熱點**:你保留的 `codex`、`antigravity`、`agent-skills` 都寫進 `.agents/`,再加上 skills-convergence(`req-tg-003`)讓**所有**支援 skills 的 target 也寫 `.agents/skills/`。因此 `req-tg-002`「共用 deploy root 需按子目錄分區、各 target 只擁有自己的檔名樣式」在你這組合裡風險最高,務必寫專門的衝突/分區測試。

> 📝 **antigravity token = plain `antigravity`(已定案,選項B)**:與生態/上游/companion 一致、可互通(APM issue **microsoft/apm#1650** 提案 `antigravity`/`agy`)。**合規影響**:§4.2.1 canonical set 尚未收錄它,故你的實作是把 `antigravity` 當「pre-standard 已接受 target」加入——這對 Producer `req-mf-005` 是一處 **documented deviation**(嚴格 v0.1 會拒絕),**不**走 `req-tg-004` 的 `x-vendor` 機制。落實兩件事:① conformance statement 明列「accepts pre-standard target `antigravity`, tracking microsoft/apm#1650」;② 你的 `req-mf-005` 接受集 = §4.2.1 canonical ∪ `{antigravity}` ∪ `x-<vendor>-<name>`。日後 §4.2.1 收錄後此 deviation 自動消失,無需改 token。

> 🔬 **研究同時推翻 companion 的兩個假設(已回填下方 4-T)**:(1) antigravity **會**自動偵測——signal 是專案根的 `GEMINI.md` 或 `AGENTS.md`(非 companion 說的 explicit-only);(2) MCP 設定用鍵 `mcpServers`、HTTP 用欄位 `serverUrl`(非 `url`)、且**不支援 `${VAR}` 執行期插值**(只吃字面值)→ 對此 target,`req-mf-013` 的 `${VAR}/${env:VAR}` 必須在 **install 時就解析**,`${input:}` 不可靜默寫成字面。antigravity 為 Go 單一執行檔、沿用 `~/.gemini/` 為 user home、專案 scope MCP 目前有「只認 HOME 層、專案層被忽略」的已知 bug(google-antigravity/antigravity-cli#60)。

### 怎麼用這份清單

1. 依 Phase 0→8 順序實作;每條寫一個引用該 `req-XXX` 的測試(`req-cf-002` 要求)。
2. **fixture 欄語意(testdata 已被移除,需重編)**:`conformance-kit/testdata/` 已清空,故 `seed fixture` 欄列的路徑代表「**要重新編寫的 fixture 檔名**」。上游參考副本仍在 **`apm/tests/fixtures/spec-conformance/<同名>`** 與 schema **`apm/docs/public/specs/schemas/`**,重編時以它們為對照(但須當作 oracle 重新驗證、不可盲抄)。標 `—` 者連上游種子都沒有,完全自製。重編規則見 **Phase V** 與末段「Conformance corpus 重編計畫」。
3. 全部勾完後,依 §11.2 產出 conformance statement(本檔末附範本)。

圖例:`[ ]` 待辦 · `keyword` MUST/SHOULD · `class` P=Producer C=Consumer G=Governance · ⏳ 待研究 → 多數已由 antigravity 研究回填

---

## Phase 0 — 解析核心與文件保真(所有文件的地基)

每個後續 phase 都依賴這層;先做。

| ✓ | req | kw | class | § | 驗證內容 | seed fixture |
|---|-----|----|----|---|----------|--------------|
| [ ] | `req-mf-020` | MUST | C | 4.1 | YAML 安全子集:scalar 預設為字串(除非 `!!int/!!float/!!bool`);`&anchor`/`*alias` 拒絕;非 `!!` 自訂 tag 拒絕;不套用 YAML 1.1 八進位強制(`0NN`)。對 manifest/lockfile/policy 三者皆適用 | `manifest/invalid-yaml-anchor-alias.yml` |
| [ ] | `req-ext-001` | MUST | C | 4.1 | 任意巢狀層級符合 `x-[a-z][a-z0-9-]*` 的鍵視為 vendor extension:語意解讀時忽略、不可 parse error、round-trip byte 等價保留(含 deps/mcp/lockfile entry/policy 子塊) | `manifest/x-extension-roundtrip.yml` |
| [ ] | `req-ext-002` | MUST | P | 4.1 | 設計不變式:你的實作**不得**定義任何 `x-` 開頭的規範鍵(該 namespace 專屬 vendor) | — |
| [ ] | `req-mf-006` | MUST | C | 4.1 | 改寫 manifest 時保留未知 top-level 鍵(向後/向前相容,不丟資料) | — |

> 🛠 **Impl note**:三份 YAML 都要走同一個 safe-loader。Rust:`serde_yaml` 需自行擋 anchor/alias 與 tag(預設會解 alias);Go:`gopkg.in/yaml.v3` 同樣要顯式拒絕。八進位:確保 `0755` 不被當數字。

---

## Phase 1 — Manifest(apm.yml)解析與驗證

| ✓ | req | kw | class | § | 驗證內容 | seed fixture |
|---|-----|----|----|---|----------|--------------|
| [ ] | `req-mf-001` | MUST | P/C | 4.1 | top-level 必須是 mapping;Consumer 對非 mapping 報錯並指名檔案 | `manifest/valid-minimal.yml` |
| [ ] | `req-mf-002` | MUST | P | 4.1 | 必含非空字串 `name` | `manifest/invalid-missing-name.yml` |
| [ ] | `req-mf-003` | MUST | P | 4.1 | 必含字串 `version` | `manifest/valid-minimal.yml` |
| [ ] | `req-mf-004` | SHOULD | P | 4.1 | `version` 應符 semver 2.0.0 regex;不符時 Consumer 發非阻擋診斷;數字型 version 必須加引號避免 int/float 強制 | — |
| [ ] | `req-mf-005` | MUST | P | 4.2.1 | **[target]** 拒絕非 canonical set 且非 alias(`vscode`→`copilot`、`agents`→`copilot`)且非 `x-[a-z…]-[a-z…]` 的 target 值;診斷指名違規 token。`minimal` 不可顯式設定。**本實作接受集 = §4.2.1 canonical ∪ `{antigravity}`(pre-standard,tracking #1650,列為 documented deviation)∪ x-vendor** | `manifest/invalid-target.yml` |
| [ ] | `req-mf-007` | MUST | C | 4.3.1 | 依 ABNF 解析字串式依賴(url-form / shorthand-form / local-path-form);不符三式之一即報錯指名 | `manifest/invalid-no-source-key.yml` |
| [ ] | `req-mf-008` | MUST | C | 4.3.3 | virtual package **只看副檔名**分類:`.prompt.md/.instructions.md/.agent.md/.chatmode.md`=檔案,其餘=子目錄;子目錄先探 `apm.yml`。不得從路徑段推斷 | — |
| [ ] | `req-mf-009` | MUST | C | 4.3.4 | 改寫時正規化:**只**剝除等於 `default_host` 的 host;SCP 式 `git@host:o/r.git` 與指向 default host 的 https 正規化為 `owner/repo`;非 default host 保留 FQDN。不得硬編特定 host | — |
| [ ] | `req-mf-010` | MUST | C | 4.3.2 | `git: parent` sentinel 僅在已知 clone 座標的 transitive 包內有效;展開為 parent 的 host/repo_url/ref,`virtual_path`取自 `path`;`parent` 不得作為 lockfile 持久身分 | — |
| [ ] | `req-mf-011` | MUST | C | 4.3.2 | 同一物件式 entry 同時設 `id:` 與 `git:` → 報錯指名衝突鍵 | `manifest/invalid-no-source-key.yml`(相關) |
| [ ] | `req-mf-012` | MUST | C | 4.3.6 | 自定義 MCP(`registry:false`)缺 `transport` / stdio 缺 `command` / http\|sse\|streamable-http 缺 `url` → 拒絕;stdio 的 command 含空白但無 `args` 兄弟鍵 → 拒絕 | — |
| [ ] | `req-mf-013` | MUST | C | 4.5 | 依分派矩陣解析 `${VAR}`/`${env:VAR}`/`${input:<id>}`;不支援的 placeholder 不得靜默當字面寫出,發診斷並可拒寫;GitHub Actions `${{…}}` 原樣保留 | — |
| [ ] | `req-mf-014` | MUST | P | 4.2.3 | 每個 `registries.<name>.url` 必須以 `https://` 或 `http://` 開頭;其他 scheme parse 時拒絕 | `manifest/invalid-registry-scheme.yml` |
| [ ] | `req-mf-015` | MUST | P | 4.2.3 | `registries.<name>` 內未知鍵(除 `x-*`)parse 時拒絕(typo guard) | `manifest/invalid-registries-typo.yml` |
| [ ] | `req-mf-016` | MUST | C | 4.3.5 | 認得 `./ ../ / ~/ .\ ..\ ~\` 為 local-path;正規化後含逃出專案根的 `..` → 拒絕並指名 | — |
| [ ] | `req-mf-017` | MUST | P | 4.7 | `marketplace.packages[].source` 驗證:拒 `..` 段;拒含 userinfo/port/query 的 URL;remote 非 `https://` 拒;local 必須 `./` 開頭 | — |
| [ ] | `req-mf-018` | MUST | C | 4.6.1 | `policy.hash_algorithm` 只接受 `sha256/sha384/sha512`;`md5/sha1`/其他 parse 時拒;省略時由 `<algo>:` 前綴推斷 | — |
| [ ] | `req-mf-019` | MUST | C | 4.2.4 | 有 `default_host` 時,它是 `req-mf-009` 唯一剝除的 host;省略時可用實作預設 host 但須在 conformance statement 記載;不得剝除其他 host | — |
| [ ] | `req-mf-021` | MUST | P/C | 4.8 | v0.1 Producer 不得寫 top-level `workspaces:`;Consumer 遇到時發**非阻擋**診斷(指名保留給 v0.2)、不附加語意、不使 install 失敗 | — |
| [ ] | `req-tg-004` | MUST | C | 4.2.1 | **[target]** parse 時接受 `x-<vendor>-<name>` target;路由到 vendor handler;無 handler 時發診斷指名(**不可靜默忽略**)。← `antigravity` **不**走這條(它走 mf-005 接受集);此條保留給未來真正的 x-vendor target,以及被減的 `gemini/cursor/windsurf` 的「無 handler→診斷」 | — |
| [ ] | `req-sc-006` | MUST | C | 4.2.3 | **[security/parse]** `registries.<name>.url` 用 `http://` → parse 錯,除非 (a)`insecure:true` 或 (b)host 為 loopback(`127.0.0.0/8`,`::1`)或 RFC1918 私網 | `manifest/invalid-registry-scheme.yml`(相關) |

---

## Phase 2 — 依賴解析(determinism 是重中之重)

> 規範核心目標:**兩個獨立實作對同一輸入產出等價結果**。本 phase 每條都要可重現。

| ✓ | req | kw | class | § | 驗證內容 | seed fixture |
|---|-----|----|----|---|----------|--------------|
| [ ] | `req-rs-008` | MUST | C | 7.1 | 將每個依賴**僅依 entry 本身**(無遠端呼叫、無預設)分類成 5 種 reference kind(local/registry/git-semver/git-literal/marketplace),依列序判定 | — |
| [ ] | `req-rs-003` | MUST | C | 7.3 | 將 `ref:` 分為 semver / literal(SHA、`v?\d+\.\d+\.\d+` 或非 semver tag、branch)/ none | — |
| [ ] | `req-rs-007` | MUST | C | 7.3 | semver range 一律以 **node-semver** dialect 評估(無實作自由裁量)。對照 oracle | `resolution/semver-dialect.json` |
| [ ] | `req-rs-014` | MUST | C | 7.3.1 | 兩 tag 僅差 build-metadata(等精度)→ 選整串 tag 名 bytewise ASCII 最高者 | `resolution/semver-dialect.json`(相關) |
| [ ] | `req-rs-002` | MUST | C | 7.3 | git-semver:列遠端 tag、annotated tag peel 到 commit、丟棄不解析的 tag(無診斷)、依 range 過濾、pin 最高;預設排除 pre-release(除非 §7.3.1 opt-in);寫 `req-lk-008` 三欄 | — |
| [ ] | `req-rs-001` | MUST | C | 7.2 | BFS + 各 manifest 宣告順序;diamond tri-modal:① 交集非空選最高(記 `resolved_by`)② 交集空 **fail-closed** 並列兩條 root→conflict 鏈(不可靜默 first-wins)③ `nest` opt-in(v0.1 拒,見 rs-013) | — |
| [ ] | `req-rs-013` | MUST | C | 7.2 | 拒絕 `dependencies.conflict_resolution: nest` 的 v0.1 manifest,診斷指名其保留給 v0.2 並引用 §7.2(3) | — |
| [ ] | `req-rs-010` | MUST | C | 7.2 | 空交集診斷格式:每條鏈以 `<owner>/<repo>@<constraint>` 用 `->` 串接、兩條都列、對給定 install plan 為 deterministic | — |
| [ ] | `req-rs-006` | MUST | C | 7.2 | transitive 深度上限預設 **50**(Governance 可經 `max_depth` 收緊);超限 fail 並指名觸頂的鏈 | — |
| [ ] | `req-rs-004` | MUST | C | 7.5 | semver-range entry 與 lock 對照:`constraint` **字元級相等**才視為無 drift(含空白差異也觸發 re-resolve) | — |
| [ ] | `req-rs-009` | MUST | C | 7.5.1 | registry 依賴可由 `apm.yml` 任一 registry 或 policy mirror 滿足,**前提是 bytes hash 等於 `resolved_hash`**;`resolved_url` 僅 advisory(URL 不符不失敗);hash 不符 fail-closed(見 lk-013) | — |
| [ ] | `req-rs-005` | MUST | C | 7.6 | 「why」診斷:由 lock **bottom-up** 走到 root、回傳含 target 的 root→target 鏈、依路徑 tuple 字典序、純離線、cycle-safe、deterministic | — |
| [ ] | `req-rs-011` | MUST | C | 7.7 | `apm update`(無參數):對**現行** manifest 約束 re-resolve 每個 direct dep、改寫 pin 為新最高、連帶 re-resolve transitive、遵守 `require_pinned_constraint`(pl-007) | — |
| [ ] | `req-rs-012` | MUST | C | 7.7 | `apm update <name>`:範圍限該包及其子樹、其餘維持原 pin、無 override flag 時拒絕對 frozen install 操作 | — |

> 🛠 **Impl note**:semver 用成熟函式庫但**必須**對齊 node-semver 行為(尤其 `^0.x` 收窄、pre-release opt-in、`||` OR、hyphen range)。Rust `semver` crate 的 caret 與 node-semver 在 `0.x` 有差異,需自行校正或用 node-semver 移植;Go `Masterminds/semver` 同樣要驗 `semver-dialect.json` 全表。

---

## Phase 3 — Lockfile(apm.lock.yaml)寫入與完整性

| ✓ | req | kw | class | § | 驗證內容 | seed fixture |
|---|-----|----|----|---|----------|--------------|
| [ ] | `req-lk-001` | MUST | C | 5.1 | top-level mapping 至少含 `lockfile_version`(字串)與 `dependencies`(list);其餘規範鍵與 `x-*` 允許 | `lockfile/v1-git-only.yml` |
| [ ] | `req-lk-002` | MUST | C | 5.4 | 有任一 `source: registry` entry → 必為 `"2"`;否則 `"1"`/`"2"` 皆可;**單調**不可由 `"2"` 降回 `"1"`;讀時兩版皆容忍 | `lockfile/v2-with-registry.yml` |
| [ ] | `req-lk-003` | MUST | C | 5.2 | git 來源必記 `repo_url`+`resolved_commit`;registry 來源必記 `resolved_url`+`resolved_hash`(外加 `repo_url` 身分) | `lockfile/v2-with-registry.yml` |
| [ ] | `req-lk-011` | MUST | C | 5.2 | 未設欄位省略(無 `null` 佔位);未知欄位 round-trip 保留(含 `x-*`) | `lockfile/round-trip-unknown-fields.yml` |
| [ ] | `req-lk-014` | MUST | C | 5.2 | lockfile 每層(top + per-entry)`x-*` 鍵 round-trip 保留 | `lockfile/round-trip-unknown-fields.yml` |
| [ ] | `req-lk-016` | MUST | C | 5.2 | 所有 digest 以 `<algo>:<hex>` 信封輸出(`resolved_hash`/`deployed_file_hashes`/`local_deployed_file_hashes`/`content_hash`/`tree_sha256`);algo∈{sha256,sha384,sha512};**讀**時容忍 64 字裸 hex 當 sha256,**寫**時必出信封 | `integrity/bare-hex-reader.frozen.yaml` |
| [ ] | `req-lk-012` | MUST | C | 5.2 | `deployed_file_hashes`/`local_deployed_file_hashes` = 寫入磁碟 bytes 的 SHA-256 信封;目錄(以 `/` 結尾)無 hash | — |
| [ ] | `req-lk-015` | MUST | C | 5.6.4 | 每個 git 來源 entry 記 `tree_sha256`(canonical git tree hash);frozen install 與 `apm audit` 時由 `resolved_commit` 工作樹重算、不符 fail-closed 並列 expected/observed | — |
| [ ] | `req-lk-005` | MUST | C | 5.5 | 僅差 `generated_at`/`apm_version` 視為語意等價(no-op install 不改寫);可由 `--no-provenance` 省略二者;`dependencies` 依 (`repo_url`,`virtual_path`) 升序;寫回正規化排序 | — |
| [ ] | `req-lk-008` | MUST | C | 5.6 | 每個 git-semver entry 記 `constraint`(原文逐字)/`resolved_tag`(字面)/`resolved_at`(ISO8601 UTC,advisory,不可當 replay tie-breaker) | — |
| [ ] | `req-lk-009` | MUST | C | 5.6 | manifest 現行約束等於鎖定 `constraint` 時 replay 鎖定的 `resolved_tag`;不同則 re-resolve | — |
| [ ] | `req-lk-010` | MUST | C | 5.6 | 對 direct git-semver 做明確 update 時,先清 install path 再 re-resolve(即使 resolved tag 不變也讓 download callback 重跑) | — |
| [ ] | `req-lk-004` | MUST | C | 5.4 | 不認得的 `lockfile_version` → 拒絕操作,診斷明白給出「升級 consumer 或由 manifest 重生 lockfile」選項 | — |
| [ ] | `req-lk-006` | MUST | C | 5.5 | frozen-install 模式:lockfile 永不寫/改寫;任一 direct dep 無 pin 即失敗(v0.1 經 `--frozen` opt-in) | `integrity/security-baseline-2.3.1.frozen.yaml` |
| [ ] | `req-lk-007` | SHOULD | C | 5.5 | 本地 checkout 已等於鎖定 commit 時應跳過下載;此優化不得改變可觀察結果(裝後狀態與全新安裝相同) | — |
| [ ] | `req-lk-018` | SHOULD | C | 5.5 | `CI` 環境變數為 truthy(存在且非 `""`/`"0"`/`"false"`,不分大小寫)時應預設 frozen;使用者可顯式覆寫 | — |
| [ ] | `req-lk-013` | MUST | C | 5.2 | **[integrity]** 解壓前先驗 registry archive bytes 的 SHA-256 == `resolved_hash`;不符 fail-closed(列 entry/expected/actual)、**不得**(部分)解壓 | `integrity/hash-mismatch.frozen.yaml` |
| [ ] | `req-lk-017` | MUST | C | 5.2 | **[integrity]** frozen install 與 `apm audit` 時重驗每個 `deployed_file_hashes`/`local_deployed_file_hashes` 對磁碟 bytes;不符 fail-closed 列 path/expected/observed | `integrity/deployed-file-mismatch.frozen.yaml` |

---

## Phase 4 — Primitive 來源與 target 部署 ⭐ 本次 target 增減的落點

| ✓ | req | kw | class | § | 驗證內容 | seed fixture |
|---|-----|----|----|---|----------|--------------|
| [ ] | `req-pr-001` | MUST | C | 8.2 | 每個 primitive 附來源歸屬:專案自有 `.apm/` → `local`;來自解析依賴 → `dependency:<name>` | — |
| [ ] | `req-pr-002` | MUST | C | 8.3 | 同名同型時 local primitive 覆蓋 dependency primitive;衝突記入診斷且可被使用者檢視 | — |
| [ ] | `req-pr-003` | MUST | C | 8.3 | 依 manifest 宣告順序處理(direct 先、transitive 依 lockfile 順序);同名同型時**首個宣告**的依賴勝,後者不取代 | — |
| [ ] | `req-tg-001` | MUST | C | 8.4 | **[target]** 僅當已註冊偵測 predicate 觸發才啟用 target;無其他檔案訊號可替代/增補;`agent-skills` **不可**自動偵測(須 `--target` 或 manifest);無訊號可 fallback `minimal`(只出 `AGENTS.md`) | — |
| [ ] | `req-tg-002` | MUST | C | 8.5 | **[target]** 只部署到該 target 已註冊 deploy root;不得寫到 root 外(視為實作缺陷非警告);共用 root(如 `.agents/`)依子目錄分區、各 target 只擁有自己檔名樣式 ← **你的高風險區** | — |
| [ ] | `req-tg-003` | MUST | C | 8.5 | **[target]** 支援 skills 的每個 target 把 skills 部署到 `.agents/skills/<name>/SKILL.md`(除非使用者顯式 opt-out skill-convergence)。**claude 例外**:companion(targets-matrix.md:35)與 Python 參考實作(`integration/targets.py` claude skills 無 `deploy_root`)都把 claude 列為 target-native 例外,只部署 `.claude/skills/<n>/`(issue #10,task 07-22) | — |
| [ ] | `req-pr-001`/`req-tg-003` | MUST | C | 8.2/8.5 | **[--skill]** `apm install <skill-collection> --skill <skill-name>` 只部署被選 skill 到 `.agents/skills/<name>/SKILL.md`;未選 skill 不得落盤;選擇需持久化到 `apm.yml` 的 `skills:` 與 `apm.lock.yaml` 的 `skill_subset` | `targets/skill-subset/` |

### 4-T 逐 target 部署矩陣(每格都要有對應測試)

> deploy root / 檔名樣式來源:`apm/docs/src/content/docs/reference/targets-matrix.md`(companion,非規範,additive)。
> antigravity 標 ⏳ 者待背景研究確認後鎖定;確認前**先以 companion 值起頭並標 unverified**。

| ✓ | target | 偵測(tg-001) | deploy root(tg-002) | skills 路徑(tg-003) | 支援 primitives | 注意 |
|---|--------|--------------|----------------------|---------------------|------------------|------|
| [ ] | `claude` | `.claude/` 或 `CLAUDE.md` | `.claude/` | `.claude/skills/<n>/SKILL.md`(target-native 例外,不寫 `.agents/`;issue #10) | instructions/agents/skills/commands/hooks/mcp(無 prompts) | hooks 併入 `.claude/settings.json`;compile 出 `CLAUDE.md` |
| [ ] | `codex` | `.codex/` | `.codex/` + `.agents/` | `.agents/skills/<n>/SKILL.md` | agents/skills/hooks/mcp | agents 為 `.toml`;compile 僅出 `AGENTS.md`;**與 antigravity/agent-skills 共用 `.agents/`** |
| [ ] | `copilot` | `.github/copilot-instructions.md` | `.github/`(user scope `~/.copilot/`) | `.agents/skills/<n>/SKILL.md` | instructions/prompts/agents/skills/hooks/mcp | user scope instructions 串接成單檔 |
| [ ] | `antigravity`(pre-standard,見📝) | 專案根 `GEMINI.md` **或** `AGENTS.md`(研究修正:**會**自動偵測,非 explicit-only) | `.agents/`(專案);`~/.gemini/` + `~/.gemini/antigravity-cli/`(user) | `.agents/skills/<n>/SKILL.md`(user:`~/.gemini/antigravity-cli/skills/`) | instructions(`GEMINI.md`/`AGENTS.md`/`.agents/rules/<n>.md`)、skills、hooks、mcp、agents(動態子代理) | hooks=`.agents/hooks.json`(事件 PreToolUse/PostToolUse/PreInvocation/PostInvocation/Stop/SessionStart,JSON stdin/stdout);mcp=`.agents/mcp_config.json` 鍵 `mcpServers`、HTTP 用 `serverUrl`、**無 `${VAR}` 插值**(→ install 時解析,見 req-mf-013);prompts/commands 已併入 skills;**與 codex/agent-skills 共用 `.agents/`** |
| [ ] | `opencode` | `.opencode/` | `.opencode/`(user `~/.config/opencode/`) | `.agents/skills/<n>/SKILL.md` | agents/commands/skills/mcp | 無 hooks 概念,hooks primitive 靜默跳過 |
| [ ] | `agent-skills` | **永不**自動偵測(`--target` only) | `.agents/` | `.agents/skills/<n>/SKILL.md` | 僅 skills | 跨 client 共享 skill bundle;**與 codex/antigravity 共用 `.agents/`** |

被減 target 的負向測試:
| ✓ | 案例 | 期望 |
|---|------|------|
| [ ] | `--target gemini` / `--target cursor` / `--target windsurf` | parse 接受(詞彙表保留)、部署階段依 `req-tg-004` 發「no registered handler」診斷、**不靜默忽略**、非崩潰 |

---

## Phase 5 — 安全強化(橫切於解析/下載/解壓/部署)

| ✓ | req | kw | class | § | 驗證內容 | seed fixture |
|---|-----|----|----|---|----------|--------------|
| [ ] | `req-sc-001` | MUST | C | 10.4 | 每個部署檔算並記 SHA-256(同 lk-012);audit 時重驗;`deployed_files` 中磁碟 hash 不符 → 報 content-integrity 違規 | `integrity/deployed-file-mismatch.frozen.yaml`(相關) |
| [ ] | `req-sc-002` | MUST | C | 10.9 | 拒絕任何解壓路徑含 `..`、絕對路徑、或軟/硬連結的 archive entry;遇第一個即 fail-closed;清理部分解壓 | `integrity/zip-slip.tar.gz` |
| [ ] | `req-sc-004` | MUST | C | 10.5 | archive 容器須 tar.gz(`application/gzip`);拒 `application/zip`;解壓大小上限預設 **100MB**;entry 數上限預設 **10,000**;違反在解壓前 fail-closed | `integrity/oversize.tar.gz` |
| [ ] | `req-sc-003` | MUST | C | 10.3 | 跨 host class 重導向時丟棄原 host 的 `Authorization`(及其他憑證);目的 host class 憑證可重新解析 | — |
| [ ] | `req-sc-005` | MUST | C | 10.3 | 兩 hostname 視為同 host class 只能基於 (a) 相同 eTLD+1(Public Suffix List)或 (b) `registries.<n>.aliases`;不得用 DNS CNAME / TLS SAN / HTTP 重導向 合併 | — |
| [ ] | `req-sc-007` | MUST | C | 10.3 | 憑證(token/basic-auth/bearer)不得出現在診斷/log/錯誤/打包產物/lockfile/audit 記錄;以來源描述(如 `GITHUB_APM_PAT env var`)指代而非字面;Producer 拒打包符合 secret-pattern 的檔(預設 `.env`/`.env.*`/`*.pem`/`*.key`/`id_rsa`/`id_ed25519`,可經 policy 擴充) | — |
| [ ] | `req-sc-008` | SHOULD | C | 10.3 | 對 scheme 非 `https://` 的 git-over-HTTP fetch 應拒附憑證,除非目的為 loopback 或 registry `insecure:true` | — |

> 🛠 **Impl note**:eTLD+1 需 Public Suffix List 函式庫(Rust `publicsuffix`/`psl`;Go `golang.org/x/net/publicsuffix`)——**不可**自己用「最後兩段」近似(`github.contoso.com` 應同 class 於 `contoso.com` 而非 `github.com`)。tar.gz 解壓務必在 streaming 階段就擋 size/entry/symlink,別先全解再檢查。

---

## Phase 6 — 治理 / policy 閘門(Governance class)

| ✓ | req | kw | class | § | 驗證內容 | seed fixture |
|---|-----|----|----|---|----------|--------------|
| [ ] | `req-pl-001` | MUST | G | 6.1 | 發現順序:① `--policy <ref>` ② 註冊的 discovery provider(依設定順序);無 provider 命中則不套用 policy | — |
| [ ] | `req-pl-011` | MUST | G | 6.1.1 | discovery 為「可註冊、有序」provider 清單;預設順序須記於 conformance statement;可經 policy `discovery:` 區塊按專案選;不得把特定 host 慣例硬編為唯一路徑 | — |
| [ ] | `req-pl-012` | MUST | G | 6.1.1 | 識別「專案 remote」:有 `origin` 用之;無 origin 但恰一 remote 用之;多個非 origin → fail-closed 列候選;無 remote → 不嘗試 | — |
| [ ] | `req-pl-002` | MUST | G | 6.2 | 有效 `enforcement=block` 且至少一違規 → 在**任何 byte 寫入磁碟前**中止 install | — |
| [ ] | `req-pl-010` | MUST | G | 6.2 | 有效 `fetch_failure=block` 且 policy 無法 fetch/parse(含 transitive `extends:` fetch 失敗)→ fail-closed 中止 | — |
| [ ] | `req-pl-003` | MUST | G | 6.4 | `extends:` 鏈深度上限 **5**;偵測循環並拒絕、診斷列出循環成員 | `policy/invalid-extends-cycle.yml` |
| [ ] | `req-pl-004` | MUST | G | 6.4 | `extends:` 釘在 leaf policy 的 host class;跨 host class extend parse 時拒 | — |
| [ ] | `req-pl-006` | MUST | G | 6.4 | 依合併表合併鏈:enforcement 取嚴、allow 交集、deny/require 聯集去重保序、max_depth 取 min、各 stricter-wins、security.* 邏輯 OR…(逐欄驗) | `policy/valid-extends.yml` |
| [ ] | `req-pl-005` | MUST | G | 6.5 | allow/deny/require 三態:省略/`null`=無意見(透明);`[]`=顯式空(覆寫 parent);`[…]`=按表合併 | — |
| [ ] | `req-pl-007` | MUST | G | 6.3.1 | `require_pinned_constraint:true` 時把**direct** dep 標違規:(a)無 ref (b)`*` (c)裸 branch (d)無上界的 `>=X.Y`;transitive 不標 | — |
| [ ] | `req-pl-008` | MUST | G | 6.3.1 | 同上,把以下視為 pinned(不違規):40 字 SHA、`v?\d+\.\d+\.\d+` 字面 tag、有上界的 range、`source:registry`、local-path | — |
| [ ] | `req-pl-009` | MUST | G | 6.6 | 遇未知 top-level policy 鍵 → 警告(絕不 parse error);`x-*` 鍵不警告且靜默保留 | — |
| [ ] | `req-pl-013` | MUST | G | 6.8 | `security.integrity.require_hashes:true` 時,任一非 local 待裝依賴缺 `content_hash` → fail-closed(lockfile 缺/不可讀亦同);local 豁免 | `policy/security-integrity.yml` |
| [ ] | `req-pl-014` | MUST | G | 6.8 | `security.audit.fail_on_drift:true` 時,偵測到 drift 或 drift check 無法完成 → audit 非零退出;僅因 cache miss 略過不改退出碼;旗標關時偵測到 drift 僅報告不改退出碼 | `policy/security-integrity.yml`(相關) |
| [ ] | `req-pl-015` | MUST | G | 6.3.5 | 對已填充 primitive target 樹評估時,unmanaged 報告完整性:(a)surface 每個未記於 lockfile 且未被 `exclude` glob 命中的檔 (b)附原因/衝突 note/可推斷的 primitive type (c)被 `exclude` 命中者即使也命中 deny 亦不 surface | — |

---

## Phase 7 — Producer / 發行契約

| ✓ | req | kw | class | § | 驗證內容 | seed fixture |
|---|-----|----|----|---|----------|--------------|
| [ ] | `req-pr-004` | MUST | P | 7.8 | 為 git-semver 消費發布的 tag 必須指向 `apm.yml.version` 等於該 tag(容許前導 `v`)的 commit;tag 名須符 semver regex;Consumer checkout 後 SHOULD 驗對齊並發非阻擋診斷 | — |
| [ ] | `req-pr-005` | SHOULD | P | 7.8 | 發行 tag 應以可公開驗證機制簽署(sigstore/GPG/SSH-signed tag);v0.1 consumer 不強制驗證(feed v0.2 provenance) | — |

> 注:`apm pack --create-tag`/`--push` 在 v0.1 **未定義**,producer 不得依賴;發行走 git host 機制(如 `gh release create` 或 `microsoft/apm-action mode: release`)。

---

## Phase 8 — 一致性自測與聲明

| ✓ | req | kw | class | § | 驗證內容 | seed fixture |
|---|-----|----|----|---|----------|--------------|
| [ ] | `req-cf-001` | MUST | C | 12.5 | 任何合規 manifest/lockfile 的 **idempotent round-trip**:重 parse+序列化產出 byte 等價檔(僅容許尾換行與 YAML flow 風格的正規化);未知 top-level 鍵、`x-*`、不理解的欄位逐字保留 | `manifest/x-extension-roundtrip.yml` · `lockfile/round-trip-unknown-fields.yml` |
| [ ] | `req-cf-002` | MUST | C | 12.3 | 發布 conformance statement:對宣告 class 內每條 `req-XXX`,列出 fixture 路徑與 exercise 它的 assertion | — |

> 注:`req-cf-002` §12.3 文字提到 Consumer 或 Producer,但 §11.1 指明 Appendix C 衝突時優先;Appendix C 與 machine-readable manifest 目前皆列為 Consumer,故本清單以 `C` 追蹤。

---

## 出範圍 / 延後(不在本次驗收)

- `req-rg-001`(Registry 字節不可變 trust anchor)— Registry class,v0.2 wire 才正規。
- v0.2 保留:publisher 簽章/attestation、workspaces/monorepo 共用 lockfile、reproducible build、version yank/deprecate、registry HTTP wire、IDNA/NFC/bidi i18n、`conflict_resolution: nest` 的 on-disk layout。

## Phase V — 驗證完整性 / 防作弊控制(process controls,非 OpenAPM 規範條文)

> **要解的問題**:把這份清單交給(AI 或人類)實作者時,其目標函數可能扭曲成「用最少 token 讓測試綠燈」,於是**改測試遷就程式碼、過度 mock、刪斷言、skip/xfail**。下列控制不是 OpenAPM 的 `req-*`,而是**對「驗證程序本身」的防護**,目的是讓「作弊比真做還難」。
>
> **核心原則**:把 **oracle(什麼叫正確)與 implementation(程式碼)分離,且 oracle 對實作者唯讀**。實作者只能改**程式碼**去通過,永遠不能改 **oracle**(fixtures + 期望結果 + 規範條文)去遷就程式碼。testdata 重編時,所有 fixture 都按此原則產出。

### A. Oracle 不可變 / 來源可稽核(擋「改測試」)
| ✓ | 控制 |
|---|------|
| [ ] | fixtures 與「期望結果」放在獨立、實作者唯讀的位置(如 `conformance/oracle/`),與 production code 不同 owner |
| [ ] | 每個 fixture 記 SHA-256 於 `oracle/CHECKSUMS.sha256`;CI 重算,任何 fixture/期望值變動即 fail |
| [ ] | `CODEOWNERS` + branch protection:改 `conformance/oracle/**` 或 `acceptance-coverage.yml` 需獨立人類審核,不可由實作 agent 自核 |
| [ ] | 同一 PR/commit **同時**改 production code 與 oracle → CI 擋下,除非掛經審核的 `spec-errata` 標籤並附規範依據 |
| [ ] | 期望值**從規範(req-XXX)與外部 oracle 推導**(`semver-dialect.json` 全表、真實惡意 tarball、真實 git repo),**不得**由被測實作自己產生 golden |

### B. 黑箱優先於白箱(擋「過度 mock」)
| ✓ | 控制 |
|---|------|
| [ ] | conformance 測試以 **subprocess 驅動真實建置出的 binary**:真 `apm.yml` 進、真 `apm.lock.yaml`/部署檔/exit code 出——**無內部可 mock** |
| [ ] | 不得 mock 受測單元(parser/resolver/hasher/extractor);只允許對真外部性(時鐘、網路 egress)用**錄製的 fixture**替身 |
| [ ] | 解析/完整性測試用**本地真 git repo + 本地 registry 吐真 tar.gz bytes**,不 stub 網路回應 |
| [ ] | 禁止「測試裡重寫一份簡化邏輯來比對」——比對對象是規範/外部 oracle,不是實作的影子複製 |

### C. 斷言強度 + 負向控制(擋「刪斷言/弱斷言」)
| ✓ | 控制 |
|---|------|
| [ ] | 每條 parse/validate req 同時有**正向**(valid→accept)與**負向**(invalid→reject)fixture:accept-everything 作弊敗在負向、reject-everything 敗在正向 |
| [ ] | 斷言鎖規範要求的**可觀察輸出**:exit code、byte 等價(round-trip)、**精確 hash 值**、規範指定的**診斷子字串**(規範反覆要求「diagnostic MUST name X/expected/actual」→ 斷言 X 真的出現)。stub 無法在不真做的情況下產出正確 hash/診斷 |
| [ ] | 輸出用 golden 檔逐位元比對(lockfile、部署樹、診斷文字) |
| [ ] | 禁止「斷言恆真」(`assert!(true)`、比對空集合、`assert err != nil` 卻不看內容) |

### D. 變異測試(擋「空殼測試」——測試存在但其實沒驗到)
| ✓ | 控制 |
|---|------|
| [ ] | 對 production code 跑 mutation testing(Rust `cargo-mutants`;Go `gremlins`/`go-mutesting`):若 mutant 存活(改壞程式碼測試仍綠)→ 該行為的測試是空殼,**修測試而非調指標** |
| [ ] | 設每個 req 的 mutation kill-rate 門檻;直接捕捉「刪斷言」「測了卻沒驗」 |

### E. 覆蓋率對映(必要非充分)+ orphan 閘門
| ✓ | 控制 |
|---|------|
| [ ] | 用規範自己的 §12.3 orphan-check:每條 `req-XXX` ≥1 個引用測試、每個引用測試對映到已宣告 req(雙向無孤兒);機器來源=`acceptance-coverage.yml` |
| [ ] | 引用測試須**實際執行**該 req 的 production 路徑(coverage-tagged),不是只存在 |
| [ ] | 覆蓋率**不**作為唯一閘門(可被無斷言測試刷高)——必與 D 的變異測試並用 |

### F. 禁止靜默縮減(擋「token 最小化」壓力)
| ✓ | 控制 |
|---|------|
| [ ] | conformance 測試禁用 `#[ignore]`/`t.Skip`/`xfail`/註解掉斷言;CI 把 skipped 計為 **FAIL** |
| [ ] | 任何豁免需 tracked、人審、時限的 waiver 並附理由;不可由實作 agent 自行豁免 |
| [ ] | 閘門是「對**完整且不可變**的 corpus 二元 PASS」——無法靠縮小範圍變綠(呼應本專案 CLAUDE.md:completion is counted, skips are failure states) |

### G. 裁判與球員分離(擋「自己改自己的考卷」)
| ✓ | 控制 |
|---|------|
| [ ] | 寫實作的 agent ≠ 擁有/編寫 conformance 斷言的 agent ≠ 宣告 PASS 的角色;由獨立 grader(CI job 或 fresh-context reviewer agent)對不可變 corpus 出裁決 |
| [ ] | `req-cf-002` 的 conformance statement 由 grader 核對,**非**實作自證(可借本專案 `harness:gen-eval-pair` / `code-review:code-review-loop` 的球員兼裁判防護) |

### H. 決定性 / 重放(擋「僥倖綠燈、mock 掩蓋不決定性」)
| ✓ | 控制 |
|---|------|
| [ ] | 整個 corpus 必須決定性(規範要求 resolution/診斷決定性);連跑兩次輸出不同 = FAIL |

---

## Conformance corpus 重編計畫(testdata 已移除 → 重建)

你已清空 `conformance-kit/testdata/`。重建時依 Phase V 把它建成「不可變 oracle」。建議結構與最小集:

```
conformance/
  oracle/                      # 實作者唯讀;CHECKSUMS.sha256 釘選
    CHECKSUMS.sha256
    manifest/   valid-*.yml  invalid-*.yml  x-extension-roundtrip.yml
    lockfile/   v1-git-only.yml  v2-with-registry.yml  round-trip-unknown-fields.yml
    policy/     valid-extends.yml  invalid-extends-cycle.yml  security-integrity.yml
    integrity/  hash-mismatch.frozen.yaml  deployed-file-mismatch.frozen.yaml
                bare-hex-reader.frozen.yaml  zip-slip.tar.gz  oversize.tar.gz
    resolution/ semver-dialect.json(外部 oracle 全表)
    targets/    每個支援 target 的「輸入專案 → 期望部署樹」golden(claude/codex/copilot/antigravity/opencode/agent-skills)
  runner/                      # subprocess 驅動真 binary,純黑箱
```

重編優先序(對映 fixture 欄):
1. **先做負向 + 整合**:Phase 1 的 `invalid-*`(擋 accept-everything 作弊)、Phase 5 惡意 tarball(zip-slip/symlink/超大/超量 entry)、Phase 3 整合性(hash-mismatch / deployed-file-mismatch)。
2. **決定性 oracle**:`semver-dialect.json` 全表(req-rs-007/014)、round-trip golden(req-cf-001)。
3. **逐 target golden**:6 個支援 target 的部署樹(特別是 `.agents/` 共用分區 req-tg-002),antigravity 用研究值。
4. 對照來源(唯讀參考、須重驗不可盲抄):上游 `apm/tests/fixtures/spec-conformance/`、schema `apm/docs/public/specs/schemas/`、`apm/docs/public/specs/manifests/openapm-v0.1.requirements.yml`。

> 我可以接著把這個 corpus 的實際檔案產出來(含 CHECKSUMS、惡意 tarball、6 個 target golden、orphan-check 與 runner 骨架)。這是一塊獨立的大工,等你說要不要做、以及 antigravity token 用 (A) 還是 (B)。

---

## Conformance Statement 範本(§11.2 / req-cf-002)

```
Implementation: <your-cli> <version>  (Rust|Go)
Conforms to: OpenAPM v0.1
Claimed classes: Producer, Consumer, Governance
Implementation-default host: github.com        # req-mf-019
Local-path content_hash walk order: <platform-native|...>   # §5.6.4 editorial
OPTIONAL features implemented: --frozen, apm update, deps why, --no-provenance, ...
Deploy targets supported: claude, codex, copilot, antigravity, opencode, agent-skills
Documented deviations / limitations:
  - Accepts pre-standard target `antigravity` (not in §4.2.1 canonical set), tracking microsoft/apm#1650.  # req-mf-005 deviation
  - gemini / cursor / windsurf: vocabulary accepted, no deploy adapter (req-tg-004 diagnostic).
Per-req test index:
  req-mf-001 -> tests/.../manifest_toplevel.rs::reject_non_mapping  (fixture: manifest/valid-minimal.yml)
  ... (89 列) ...
```

---

_衍生自 OpenAPM v0.1(`openapm-v0.1.md`,90 條,排除 req-rg-001 = 89)。fixture 標 `—` 者為 v0.1 seed set 未涵蓋、須自補。_
