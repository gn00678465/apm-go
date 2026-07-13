# Gate 6b 報告 — pack 完整 parity + install 消費回路（test1 情境重播）

> 依 `oracle-parity-gates.md` Gate 6b 規定：**「此修正不做什麼」寫在統計之前**。
> oracle = Python `apm` v0.23.1（實測 binary，非 checked-out source）。
> 重播 fixture = 使用者 `D:/Projects/apm-dev/evals/test1`（`mattpocock/skills` 依賴，
> 巢狀 skills，targets copilot/claude/opencode/codex）。

---

## 一、此修正不做什麼（未達 parity 的殘留，先講）

### pack
- **`pack:` metadata `target` 欄**：apm-go 恆寫 `all`，Python 依 pack 設定寫 `minimal`。
  純資訊性 metadata，不影響 bundle 內容或 install 行為；bundle_files 雜湊與檔案樹皆一致。
- **plugin.json 換行**：apm-go 寫 LF，Python 在 Windows 寫平台原生 CRLF。內容正規化後
  完全相同；install 端兩邊都對文字檔做 CRLF→LF 正規化，故部署後 byte-identical。

### install（本地 bundle 消費）
- **compile-only target 的 instructions 暫存未移植**：Python 對無 native instructions
  primitive 的 target（opencode/codex）把 bundle `instructions/*` stage 到
  `apm_modules/<slug>/.apm/instructions/` 供 `apm compile` 合併；apm-go 無此 bundle
  compile-staging，改以 diagnostic 明確 skip（test1 用 claude/copilot 不觸發此分支）。
- **canvas `extensions/*` 丟棄**：apm-go 無 `--trust-canvas-extensions` 旗標與 canvas
  primitive 型別；與 Python 預設（旗標關）行為一致——一律丟棄不部署。
- **console「Installed N file(s)」計數**：apm-go 去重後計數，可能少於 Python 未去重的
  raw 計數（多 target 路由到同一路徑時）；純顯示差異，磁碟狀態與 lockfile 不受影響。

### audit（Phase 7）
- **install-replay drift 未移植**：Python bare `apm audit` 預設另跑 install-replay drift
  偵測（三分類 modified/unintegrated/orphaned）。apm-go 既有 SHA 重驗覆蓋 `modified`
  子集與 `unintegrated` 的「已追蹤檔遭刪」子集，**未覆蓋** never-recorded-but-should-exist
  超集與 `orphaned` 全類——需獨立 install-replay 引擎（見 `audit-drift-reconciliation.md`）。
- **`--ci`/`--policy`/`--external`/`--format`/`-o`/`--strip`**：各為獨立子系統，apm-go
  全無基礎，非本輪範圍。

---

## 二、達成的 parity（統計）

### pack 雙邊（test1，主 session 親跑）
| 項目 | apm-go pack | apm(Python) pack | 結果 |
|---|---|---|---|
| exit | 0 | 0 | = |
| bundle 檔數 | 73 | 73 | = |
| plugin.json 輸出 | .claude-plugin/ + .github/plugin/ | 同 | = |
| bundle 樹（除 apm.lock.yaml 時間戳） | — | — | **72/73 byte-identical；plugin.json 正規化後相同** |

直接消除使用者最初的抱怨「apm-go pack 產不出 bundle / 與 apm pack 不符」。

### install 雙邊（test1 bundle，--target claude,copilot，主 session + codex 各自親跑）
| 項目 | 結果 |
|---|---|
| 部署檔路徑集 | apm-go 222 = Python 222，**PATH_DIFF=0** |
| 逐檔 sha256 內容 | **HASH_DIFF=0（全部 byte-identical）** |
| 兩層巢狀 skills | 全部署（修正前只 4/72，修正後與 Python 一致） |
| verbatim 檔名 | `.agent.md`/`.instructions.md` 保留，未二次 transform |
| CRLF→LF 文字正規化 | 與 Python 一致（自造 CRLF fixture 驗證同 hash） |
| 竄改 bundle 檔 | exit 1 `Hash mismatch`，零部署，無 lockfile |
| NTFS junction 逃逸 | 拒絕（reparse point 偵測；mutation 證明辨識力） |
| apm.yml | 未建立/未改（imperative deploy） |

codex 獨立雙邊 diff 佐證：`GO_PATH_COUNT=403 PY_PATH_COUNT=403 PATH_DIFF=0`、
`HASH_DIFF_COUNT=0`。

### ContentScanner A/B（隱字掃描 pack 端接線）
含 1 critical(bidi-override U+202E) + 1 warning(zero-width U+200B) 的 fixture：
apm-go pack 與 Python apm pack 皆報 **2 hidden character(s)**，訊息結構一致（非 tautology）。

### audit --content（Phase 7）
親造三 fixture：clean→exit 0、warning-only(U+200B)→exit 2、critical(U+202E)→exit 1
（輸出含 file:line:col）；bare audit SHA 路徑零回歸。

### 回歸
`ab_marketplace_pack.py` 14/14；`ab_uninstall.py` 6/0（2 既有 documented deviation）；
全 repo `go build/vet/test ./...` 綠（23 套件）。

---

## 三、事故紀錄（透明）
- gate6b install 改寫 agent 的驗證誤刪使用者 test1 fixture（`.apm`/`apm_modules`/
  `apm.lock.yaml`/部署目錄）。主 session 從本 session 稍早的 scratch 副本
  （`tmp/gate6b/go`）還原 `.apm`(2 源檔)/`apm_modules`(143)/`apm.lock.yaml`，並以當前
  binary 重生部署/build 狀態，fixture 已完整可用。後續所有 agent/codex 皆嚴令唯讀 test1。
- codex round 2 重驗 B2 junction 逃逸時被 OpenAI cybersecurity 內容過濾阻擋，改由主 session
  mutation 測試（改回 symlink-only→junction 測試 RED）替代驗證，證據更強。
