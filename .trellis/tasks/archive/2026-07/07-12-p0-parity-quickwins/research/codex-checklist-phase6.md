# Phase 6 硬性 checklist — `install <bundle-path>` 消費回路（Review Gate C）

驗證對象：Phase 6 變更（git 未提交）
- M `cmd/apm/install.go`（+187）
- M `internal/pack/bundle/lockfile_pack.go`（+48，新增 `ParsePackMetadata`）
- ?? `internal/localbundle/{detect,verify,integrate}.go` + tests
- ?? `internal/archive/zip.go`（新增 `SafeExtractZip`）+ test
- ?? `cmd/apm/install_localbundle_test.go`

驗證紀律：**claim 不採信**。每一項須在真實原始碼/測試中指出檔案:行號證據，判定
`CONFIRMED`（證據充分成立）/ `REFUTED`（證據反駁）/ `UNCERTAIN`（證據不足）。
只要有一項 REFUTED 即 Phase 6 未通過。對抗性立場：預設想辦法反駁每一條。

## A. 安全性（BLOCK 級 — 任一 REFUTED = 阻擋）

- A1. **byte 竄改必拒**：bundle 內任一 `bundle_files` 列出的檔案內容被改一 byte，
  `VerifyBundleIntegrity` 必回傳非空 errs（hash mismatch），且 `runLocalBundleInstall`
  據此 return error、**零檔案部署、不寫 lockfile**。指出 verify.go 的 hash 比對與
  install.go 的 early-return（在 IntegrateLocalBundle 之前）。
- A2. **symlink 必拒**：bundle 目錄下任何 symlink（無論是否在 manifest）都被
  `VerifyBundleIntegrity` 標記拒絕。指出 WalkDir + Lstat + `os.ModeSymlink` 判斷。
- A3. **unlisted 檔必拒**：bundle 內存在 `bundle_files` 未列出（且非 apm.lock.yaml/
  plugin.json 白名單）的一般檔案，被標記為 tampering。指出第三段 WalkDir 反向檢查。
- A4. **path traversal 必拒**：`bundle_files` key 含 `..`/絕對路徑/磁碟機代號者被
  `safeBundleRelPath` 拒絕，不會寫出 bundle 根之外。
- A5. **archive traversal/symlink/caps 必拒**：`SafeExtractZip` 對 `..`、絕對路徑、
  symlink entry、超過 MaxEntries/MaxBytes 者一律 fail-closed，且 staging→rename
  失敗時 dest 不被污染。指出 zip.go 對應行。
- A6. **archive 抽取任何錯誤 fail-closed**：`DetectLocalBundle` 對 corrupt 或 security
  違規的 archive 一律回 error（不 fail-open）；install.go 將其表面為
  "bundle security check failed" 並 return，不 fall-through 到 registry 路徑。
- A7. **install 端 hash 讀取正確**：`ParsePackMetadata` 讀出的 `bundle_files` 值為
  bare hex；`normalizeHash` 同時接受 bare hex 與 `sha256:` 前綴，拒絕其他演算法前綴。

## B. 正確性 / parity

- B1. **早退位置**：local-bundle 偵測發生在 `runInstall` 最頂端，**先於讀取 apm.yml**，
  且只在 `len(packages) == 1` 時觸發。指出 install.go:182 前後。
- B2. **imperative deploy，不碰 apm.yml**：成功安裝只寫 `local_deployed_files`/
  `local_deployed_file_hashes` 到 apm.lock.yaml，**絕不建立/修改 apm.yml**。
- B3. **lockfile union merge**：`persistLocalBundleDeployment` 與既有
  `LocalDeployedFiles`/`Hashes` 聯集（additive），不覆蓋既有 local 部署或前一次
  bundle 安裝。
- B4. **target mismatch 只警告不 gate**：packed target 與 install target 不符時
  `CheckTargetMismatch` 只印警告，仍照常部署（不 refuse）。
- B5. **zero targets no-op**：解析不到任何 target 時印警告、return nil、不寫 lockfile。
- B6. **IM7**：archive 副檔名（.zip/.tar.gz/.tgz）但非合法 bundle（無 plugin.json）→
  targeted usage error（點名副檔名），**不**靜默 fall-through。
- B7. **flag 衝突**：local bundle 安裝時 `--skill`/`--allow-insecure` 被拒（點名 flag）；
  `--target` 仍有效（用於 ResolveTargets）。

## C. 零回歸（不得誤傷既有路徑）

- C1. **F1 不被誤用**：裸目錄（單一 positional path，但**無** plugin.json，例如本地
  path dependency checkout）必須 fall through 到既有 local-path dependency 安裝，
  `normalizeLocalDep`/F1 行為不變。指出 `DetectLocalBundle` 回 (nil,nil) + install.go
  的 handled=false fall-through，以及 regression 測試。
- C2. **一般 install 不受影響**：`len(packages) != 1`、registry 安裝、frozen、--mcp
  路徑完全不進 local-bundle 分支。
- C3. **既有 archive 下載路徑不被更動**：`SafeExtract`（tar.gz-only、拒 zip）維持原狀；
  `SafeExtractZip` 為獨立新 entry point，未觸碰 registry download call site。
- C4. **Rollback Points 邊界**：五個既有檔案中只有 `cmd/apm/install.go` 被觸碰；
  `internal/deploy/*`、`internal/manifest/*`、`internal/security/*`、`cmd/apm/pack.go`、
  `cmd/apm/audit.go` 皆未被本 Phase 修改（lockfile_pack.go 是 Phase 4 既有檔的加法，
  非五檔邊界內）。

## D. 工程品質

- D1. `go build ./... && go vet ./... && go test ./...` 全綠（22 套件）。
- D2. `internal/localbundle` 覆蓋率 ≥ 80%；`cmd/apm` 無下降。
- D3. 觸碰/新增檔 `gofmt -l` 乾淨、LF 換行。
- D4. `ab_marketplace_pack.py` 與 `ab_uninstall.py` 無新增 fail（相對既有 baseline）。
