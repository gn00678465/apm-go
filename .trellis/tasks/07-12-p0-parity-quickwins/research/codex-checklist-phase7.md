# Phase 7 硬性 checklist — audit bare Unicode 內容掃描接線（共用 ContentScanner 第二接點）

驗證對象：Phase 7 變更（git 未提交）
- M `cmd/apm/audit.go`（新增 `--content` flag 分支）
- ?? 可能新增 `cmd/apm/audit_content.go` + `*_test.go`
- ?? `.trellis/tasks/07-12-p0-parity-quickwins/research/audit-drift-reconciliation.md`

驗證紀律：**claim 不採信**。每項須在真實原始碼/測試/實跑中指出「檔案:行號」或
實跑輸出證據，判定 `CONFIRMED`/`REFUTED`/`UNCERTAIN`。任一 REFUTED = Phase 7 未過。
對抗性立場：預設先想辦法反駁每一條。允許並鼓勵實跑（build/vet/test、自造含隱藏字元
的部署檔 fixture 跑 `apm-go audit --content`）。

## A. 正確性（BLOCK 級）

- A1. **bare audit 行為零改變**：`apm-go audit`（無 `--content`）仍是 SHA-256 重驗
  `deployed_file_hashes`，程式路徑與輸出與 Phase 7 之前相同。指出 audit.go 的 bare
  分支未被更動的證據，並確認既有 SHA 測試（TestAudit_DeployedFileMismatch 等）全 PASS。
- A2. **掃描來源跨 deps + local**：`--content` 掃描的檔案集合 =
  每個 `lock.Dependencies[i].DeployedFiles` ∪ `lock.LocalDeployedFiles`
  （非只掃其中一種）。指出程式列舉兩個來源的碼；若只掃一種 → REFUTED。
- A3. **共用掃描器（非重造輪子）**：逐檔呼叫 `internal/security.ScanFile`
  （或 ScanText），未在 cmd/apm 內重新實作 suspicious-range 掃描邏輯。
- A4. **exit code 正確**：全乾淨→exit 0；含 critical→exit 1；僅 warning（無 critical）
  →exit 2。用既有 `withExitCode`。親自造三種 fixture 實跑驗證三個 exit code。
- A5. **critical 輸出含檔案 + 位置**：exit 1 時輸出逐檔列出路徑與位置（line/col 或
  codepoint offset），不是只印一個總數。實跑檢查輸出內容。

## B. 語意 parity / drift（BLOCK 級文件交付）

- B1. **drift 核對文件存在且具體**：`research/audit-drift-reconciliation.md` 存在，
  逐項比對 apm-go 既有 SHA drift 與 Python bare-audit drift（是否偵測 lockfile 未記錄
  但磁碟存在的孤兒檔/replay 級差異），**明確結論是否有真實缺口**。若文件用「drift 延後」
  之類模糊帶過而無逐項核對 → REFUTED。
- B2. **help 明列不做什麼**：`audit --help`/Long 明寫 `--content` 掃描**不含** drift
  replay 與 `--ci`/`--policy`/`--external`/`--format`/`-o`/`--strip` 等獨立子系統。

## C. 零回歸（BLOCK 級）

- C1. **既有 audit 測試 100% 不回歸**：`go test ./cmd/apm/... -run Audit -count=1` 全 PASS
  （baseline：TestAuditCmd_HelpDocumentsSemanticDifference、TestAudit_DeployedFileMismatch、
  以及 marketplace audit 系列全 PASS）。親跑比對。
- C2. **internal/security 未被改**：Phase 7 只呼叫 security 套件 API，未修改其任何檔。
  用 `git diff --name-only HEAD` 佐證 internal/security 無 diff。
- C3. **五檔邊界**：既有檔只有 `cmd/apm/audit.go` 被本 Phase 改（外加新檔）；
  pack.go/install.go/manifest.go/target.go 無 Phase 7 diff。

## D. 工程品質

- D1. `go build ./... && go vet ./... && go test ./...` 全綠。親跑。
- D2. `cmd/apm` 覆蓋率不低於 Phase 7 前（84.1%）。親跑比對。
- D3. 觸碰/新增 Go 檔 `gofmt -l` 乾淨、LF 換行。
- D4. 文案以測試側獨立字面常數鎖定（非引用產品常數；抽測其辨識力）。
