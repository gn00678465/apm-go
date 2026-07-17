# 最終 A/B 終驗（implement.md 步驟 28）

## 可重現性資訊

- 日期：2026-07-16
- apm-go：branch `feat/install-parity-bugfix`（`40f8047`，含全部 8 個工作 commit），
  `go build -o bin/apm-go.exe ./cmd/apm-go`
- Python apm 基準：v0.21.0（`a9a883b3`），詳見 `research/bug2-python-baseline.md`
- repo SHA：`mattpocock/skills @e9fcdf95…`、`antfu/skills @a74f281a…`
- fixture：空專案 + `apm.yml`（`target: [claude]`、`dependencies.apm: []`）

## BUG-2 實彈結果（apm-go 修復後 vs Python 基準）

| 情境 | Python 0.21.0 | apm-go 修復後 | 判定 |
|---|---|---|---|
| 兩步驟（repo_a --skill x → repo_b --skill y） | 無污染（模型：只處理新 package） | **無污染**：repo_a 維持 1 skill（修復前 22）；apm.yml/lockfile 帳實一致 | ✅ 對齊 |
| 同 repo 二次 --skill（union） | apm.yml union 但**只佈當次名單**、grill-me 被 stale-clean（P-D1，bare install 才收斂） | apm.yml union `[code-review, grill-me]` 且**佈署即收斂**（兩個 skill 全在） | ✅ **優於 Python** |
| unknown skill（新輸入） | exit 0 靜默入帳（P-D2，幽靈名稱永久污染） | **exit 1**、`Error: --skill …: unknown skill(s) …`、apm.yml 零變更（diff 驗證） | ✅ 刻意嚴格（prd B2-6） |
| `--skill '*'` RESET | （既有語意） | apm.yml 塌回 scalar、22 skills 全量佈署、他 dep 子集不受影響 | ✅ |
| `--skill " "` 空白 | （未測） | 報錯拒絕（H6 防空 slice，審核輪抓到並修補） | ✅ |
| bare install / update 尊重 persisted 子集 | ✅（Python 讀回鏈路） | ✅（TestUpdate_RespectsSkillSubset + evals/test1 實跑：mattpocock 1 skill） | ✅ 對齊 |

## BUG-1

- e2e：`TestInstall_CaseFoldDedup` 全狀態斷言（Resolved 1、apm.yml 單 entry、lockfile 單 dep、
  apm_modules 單目錄、無 shadowed/0-files 噪音）+ 四支守衛（不同 repo 不合併、selector 衝突
  警告、舊 lockfile 升級相容、BUG-1×BUG-2 RESET 交互）。
- 交互測試逼出並修復獨立既有 bug：lockfile `depSemanticEqual` 漏比 `SkillSubset`
  （RESET 無其他變更時被 Already-up-to-date 短路吞掉）。

## 乙類實彈

| 項 | 實跑 | 結果 |
|---|---|---|
| R17 | `evals/bundle-demo` 無 target install | exit 2、結構化診斷（5 marker + 3 修法 + apm.yml 範例）、**無 Cobra flags dump**；一般 flag 錯誤仍印 usage（守衛測試） |
| R16 | 空 deps + `.apm/instructions` fixture | 無 `No dependencies to install` 矛盾行、樹 + `+ Installed local project` |
| R7/R11/R12a-e/R13/R15 | 單元/e2e 測試 | 全綠（見 implement.md 回填的測試名） |
| 既有真實專案 | `evals/test1` bare install | mattpocock 子集尊重（1 skill）、local 樹、無回歸 |

## 品質門檻

- `go build ./...` / `go vet ./...` / `go test ./... -count=1`：全綠（24 packages）
- `-race`：本機無 cgo，交 CI（既有記錄限制）
- codex 閘門：每 commit 過閘（含最終總閘 high effort 抓到 2 HIGH → `40f8047` 修復 + 複審無 C/H）

## 結論

BUG-2/BUG-1 修復並以 Python 基準對照完成；兩處刻意偏離 Python 均有依據並記錄
（unknown skill 嚴格化＝不污染帳本原則；union 佈署即收斂＝避免 Python P-D1）。
乙類 R7–R17 全數落地（R18 決策不補、R12e 已實作）。
