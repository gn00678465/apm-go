# Gate 6b 修正硬性 checklist — 本地 bundle 安裝改為 verbatim-tree-copy（parity 修復）

背景：Phase 8 Gate 6b test1 雙邊重播揭露 Phase 6 的 `IntegrateLocalBundle` 用錯模型
（把 plugin-final bundle 當 apm 源碼二次 transform，漏巢狀 skills、錯改檔名），
對 test1 bundle 只部署 4/72 檔。已改寫為 Python `integrate_local_bundle`
（`apm/src/apm_cli/install/services.py:702`，oracle 版本 v0.23.1）的 verbatim
per-target 檔案樹複製模型。

驗證對象（git 未提交）：
- M `internal/localbundle/integrate.go`（改寫）+ `integrate_test.go` + `testutil_test.go`
- M `cmd/apm/install.go`（`runLocalBundleInstall` 呼叫點簽名）+ `install_localbundle_test.go`

驗證紀律：**claim 不採信**，親自實跑。對抗性：預設反駁每條。任一 REFUTED = FAIL。
Python apm 可用（cmd `apm` v0.23.1，**相對路徑**呼叫）。

## A. parity（BLOCK — 核心）

- A1. **部署樹與 Python byte-identical**：對 test1 bundle（`mattpocock/skills` 依賴，
  巢狀 skills），`apm-go install <bundle> --target claude,copilot` 的部署檔案樹
  （`find .claude .github .agents | sort`）與 `apm install` **路徑集相同**。親自造 bundle
  跑雙邊 diff。
- A2. **內容 byte-identical**：上述兩樹每個檔案 sha256 相同（含巢狀 skill、agent、
  instruction）。親跑逐檔 hash diff。
- A3. **巢狀 skills 全部署**：`skills/<category>/<name>/SKILL.md` 兩層巢狀的 skill 檔
  全部出現在部署樹（舊模型漏掉這些）。
- A4. **verbatim 檔名**：agent 保留 `.agent.md`（非被 strip 成 `.md`）、instruction 保留
  `.instructions.md`（非轉成 `rules/`），與 Python 一致。
- A5. **CRLF→LF 正規化**：文字檔（.md/.json/.toml/.txt/.yaml/.yml，且 decode 為合法 UTF-8）
  部署時 CRLF→LF 正規化，與 Python `_normalized_bundle_text` 一致。造一個 CRLF 內容的
  bundle 檔驗證部署後為 LF 且與 Python 同 hash。
- A6. **plugin.json/.mcp.json/apm.lock.yaml 不部署**：這三個 metadata 檔（大小寫不敏感）
  被過濾，不出現在部署樹。

## B. 安全性維持（BLOCK）

- B1. **竄改拒絕**：竄改 bundle 內任一檔（含巢狀 skill）→ install exit 1 `Hash mismatch`、
  零部署、無 lockfile。親跑。
- B2. **路徑穿越/symlink 拒絕**：`verify.go` 的 traversal/symlink 防護未被削弱
  （`deployBundleFile` 對每個目的地 `EnsureWithinRoot` 包住；bundle_files key 穿越拒絕）。
- B3. **不碰 apm.yml**：成功安裝只寫 `local_deployed_files`/`local_deployed_file_hashes`，
  apm.yml 未建立/未改。
- B4. **verify.go/detect.go 未被改**（hash/symlink/unlisted 邏輯已 Phase 6 codex 驗過）。
  `git diff --name-only HEAD` 佐證。

## C. 零回歸（BLOCK）

- C1. `go build ./... && go vet ./... && go test ./... -count=1` 全綠（23 套件）。親跑。
- C2. 既有 Phase 6 install 測試已更新為新 verbatim 佈局的正確斷言（非殘留舊 transform
  斷言）；新增巢狀-skill 部署測試。
- C3. `internal/security`、`internal/deploy`、`pack.go`/`audit.go`/`manifest.go`/`target.go`
  無本次 diff（五檔邊界；本次只動 localbundle + install.go）。
- C4. `ab_marketplace_pack.py`/`ab_uninstall.py` 無新增 fail。

## D. 工程 + 誠實 deviation

- D1. `gofmt -l` 觸碰檔乾淨、LF；localbundle 覆蓋率 ≥ 80%、cmd/apm 不低於 84.4%。
- D2. **documented deviation 誠實且準確**：compile-only target（opencode/codex 無 instructions
  primitive）的 `instructions/*` 未做 apm_modules staging（Python 有）、canvas `extensions/*`
  丟棄——這些必須在報告/註解明列，且**不得誇大或縮小**（對照 Python 實際行為）。
