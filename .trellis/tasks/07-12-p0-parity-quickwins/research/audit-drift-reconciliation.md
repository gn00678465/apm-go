# audit drift 語意核對 -- apm-go SHA drift vs Python bare-audit drift

> Phase 7（`.trellis/tasks/07-12-p0-parity-quickwins/design.md`「audit drift
> 澄清」段落交付物）。逐項核對，非「drift 延後」式模糊帶過。來源皆為
> `D:/Projects/apm-dev/apm/src/apm_cli` 原始碼 file:line（非摘要轉述）。

## 結論先講：有真實缺口，不是全缺席也不是已達 parity

> **2026-07-13 修正**：本節與下方「`unintegrated`」小節原文宣稱 apm-go
> 對 `unintegrated` 「完全不覆蓋」/「零覆蓋」，經 codex 對抗性驗證
> （`research/codex-verify-phase7.md` B1，判定 REFUTED）指出
> `internal/lockfile/audit.go:29-33` 對 lockfile 已記錄但磁碟缺失的路徑
> 會 append violation，親跑 bare fixture 也確認得到 `exit 1` +
> `observed <missing>`，直接反駁「零覆蓋」的措辭。以下已改寫為精確描述：
> 覆蓋的是「lockfile 已記錄路徑缺失於磁碟」這個 `unintegrated` 最常見的
> 實務形態，未覆蓋的是「從未被記錄進 lockfile、但依當前 lockfile 重新
> 物化『應該』產出」這個更窄的邊界案例（見下方 `unintegrated` 小節）。

apm-go 既有 bare `audit`（`internal/lockfile.VerifyDeployedState` +
`internal/lockfile/audit.go:22-50`）對 Python drift 三分類的覆蓋並不對稱：
`modified`（**部分重疊**，機制不同，見下）、`unintegrated`（**部分重疊**
——tracked-but-missing-from-disk 的常見案例有覆蓋，never-recorded-但-應該
存在的邊界案例未覆蓋，見下）、`orphaned`（**完全不覆蓋**）。Phase 7 的
`--content` flag 補的是 Python bare audit 的「隱字掃描」pillar
（`ContentScanner`），**不是** drift pillar——`--content` 與 drift 是
Python 原始碼裡兩個獨立、可分別開關的機制（`_audit_content_scan` 內兩段
各自的 `if` 分支，`commands/audit.py:807-960`），本次不合併實作。

## Python bare `apm audit` 的兩根支柱（`commands/audit.py:1183-1221`）

1. **內容掃描**（`ContentScanner`，`security/content_scanner.py`）——掃描
   lockfile 記錄的已部署檔，找隱藏 Unicode 字元。**本 Phase `--content`
   對應此支柱。**
2. **install-replay drift 偵測**（`security.audit.fail_on_drift` 預設
   on，可用 `--no-drift` 關閉；`commands/audit.py:906-960` 呼叫
   `policy/ci_checks.py:_check_drift` → `install/drift.py:run_replay` +
   `diff_scratch_against_project`）——**這是本文件的核對對象**。bare audit
   對 drift 是 advisory（不擋 exit code，除非 org policy 開
   `fail_on_drift`）；`--ci` 模式才會讓 drift 失敗直接 gate exit 1
   （`commands/audit.py:639-654`）。

## Python drift 機制：真的「重放安裝」，不是雜湊比對

`install/drift.py:625-708`（`diff_scratch_against_project`）：在專案樹**外**
建一個 scratch 目錄（`_make_scratch_root`，`install/drift.py:127-142`，
`_assert_scratch_bound` 確保不落在專案樹內），對**目前的 lockfile** 用
本地快取（`cache_only=True`）重新跑一次完整安裝物化，產出 scratch 樹，
再拿 scratch 樹跟專案樹逐檔比對（僅比對「governed」target 目錄），
normalize 過（`_normalize`：line-ending/BOM/build-id 正規化後再比較，
避免正常環境差異誤報）。三分類（`install/drift.py:664-707`）：

| kind | 定義 | 觸發條件 |
|---|---|---|
| `modified` | scratch 與 project 都有此檔，正規化後內容不同 | 兩邊都存在但 bytes 不同 |
| `unintegrated` | scratch 有此檔，project 沒有 | 「現在重新安裝 lockfile 應該產出的檔」實際上沒被部署 |
| `orphaned` | project 有此檔且**已在 lockfile `deployed_files` 中被追蹤**，但 scratch 沒有 | 「以前部署過、lockfile 仍記錄，但重新安裝已不會再產出」（例如依賴內容/版本已變，殘留舊檔）|

（未被 lockfile 追蹤的專案內額外檔案刻意不算 orphaned——避免對使用者自寫
內容誤報，`install/drift.py:641-642` 註解明載。）

## apm-go 既有 SHA drift 機制：純雜湊比對，不重放安裝

`internal/lockfile/audit.go:22-50`（`VerifyDeployedState`）：對 lockfile
`deployed_file_hashes`（每個 dep entry + `local_deployed_file_hashes`）
**已記錄**的每一個 path，重算磁碟上該路徑檔案的 SHA-256，跟 lockfile
記錄的 hash envelope 比對。**沒有任何步驟去獨立算出「現在重裝這個
lockfile 應該產出什麼」**——它信任 lockfile 目前記錄的 hash 就是正確
基準，只驗證磁碟位元組沒有偏離這個基準。

## 逐分類核對

### `modified`（部分重疊，機制不同）

- **apm-go 覆蓋**：是，`VerifyDeployedState` 對每個已記錄路徑重算 hash，
  磁碟位元組被竄改 → 違規（`Observed` 非空，hash 不符）。
- **機制差異**：apm-go 是「跟 lockfile 記錄的固定 hash 比」；Python
  是「跟一次全新安裝重放的 scratch 樹內容比」。對「lockfile 從未更新過、
  磁碟被竄改」的常見案例兩者結論一致（都會抓到）。但若 lockfile 本身的
  記錄過期（例如 `resolved_hash`/內容應該變而 lockfile 未更新，這種情況
  在 apm-go 現行模型下理論上不該發生，因為 install/update 本身就是唯一
  寫入 lockfile 的路徑），apm-go 沒有獨立於 lockfile 之外的「應然值」可比對
  ——這正是下面 `unintegrated`/`orphaned` 缺口的根因。
- **判定**：**部分達成**（同一個「檔案被手動竄改」場景兩邊都能抓到），
  非完全 parity（比對基準不同：靜態記錄值 vs 動態重放值）。

### `unintegrated`（apm-go 部分重疊 -- 機制不同，非「零覆蓋」）

> 修正紀錄：本小節原文判定「apm-go 零覆蓋」，經 codex 對抗性驗證
> （`codex-verify-phase7.md` B1 REFUTED）核實後改寫如下，理由與證據見
> 上方「結論先講」修正註記。

- **Python 定義**：現在重跑安裝「應該」產出的檔，但專案裡實際沒有。
- **apm-go 現狀**：`VerifyDeployedState` 對 lockfile `deployed_file_hashes`
  的**每一個已記錄 path** 重算磁碟 hash；若該路徑對應的檔案在磁碟上不
  存在，`HashFileBytes` 回傳 err，`VerifyDeployedState` 會 append 一筆
  `Violation{Path: path, Observed: ""}`（`internal/lockfile/audit.go:29-33`），
  bare `apm-go audit` 對此會回報 violation 並以非零 exit code 結束、
  `Observed` 欄印 `<missing>`（`cmd/apm/audit.go` 的 violation 格式化，
  親跑 fixture 確認得到 `exit 1` + `observed <missing>`）。對「lockfile
  已記錄某檔案應部署、但該檔案被使用者手動刪除或部署失敗導致磁碟缺失」
  這個常見場景，apm-go **確實會偵測到**——這正是 `unintegrated` 在實務上
  最常出現的形態（lockfile 記錄與磁碟現況不一致，磁碟少了一個「應該在」
  的檔案）。因為 apm-go 模型下 install/update 是唯一寫入 lockfile
  `deployed_file_hashes` 的路徑（見上方「機制差異」段落），「lockfile 已
  記錄的路徑」在絕大多數情況下就等同於「一次全新重裝『應該』產出的路徑
  集合」，所以 apm-go 這個「已記錄路徑缺失於磁碟」的檢查，在實務上覆蓋了
  Python `unintegrated` 的主要觸發情境。
  **真正未覆蓋的缺口**是更窄的邊界案例：Python 的 `unintegrated` 判準來自
  **獨立重新物化**當前 lockfile 應該產出什麼（scratch replay），不依賴
  lockfile 本身「記錄了什麼路徑」；apm-go 完全依賴「lockfile 已記錄的
  path 集合」作為檢查範圍——若某個檔案**從未被記錄進 lockfile**（例如
  lockfile 寫入路徑本身有 bug、部分寫入後中斷導致某 entry 缺失、或
  lockfile 被手動編輯移除某 key），apm-go 沒有任何管道發現「這裡少了一個
  檔案」，因為它不會、也無法獨立算出「不看 lockfile 已記錄了什麼、只看
  依賴解析結果，現在應該部署什麼」。這個
  never-recorded-but-should-exist 的子集才是 apm-go 真正沒有覆蓋到的部分。
- **判定**：**部分重疊**（tracked-but-missing-from-disk 的主要/常見案例
  有覆蓋，機制是「跟 lockfile 記錄比對」而非「跟獨立重放結果比對」）；
  **never-recorded-but-should-exist 的邊界案例未覆蓋**——這部分才需要
  install-replay 引擎才能偵測，不是「完全零覆蓋」。apm-go 沒有、也不打算
  在本 Phase 補上 install-replay 引擎（見下方 disposition）。

### `orphaned`（apm-go 零覆蓋 -- 真實缺口）

- **Python 定義**：lockfile 仍追蹤、磁碟仍存在，但重新安裝已不會再產出
  （殘留舊檔，例如依賴版本/內容變動後舊檔沒被清乾淨）。
- **apm-go 現狀**：同上，`VerifyDeployedState` 沒有「重新計算現在應該
  產出什麼」的能力，因此無法判斷某個已記錄檔案是不是「重裝後就不會再有
  的殘留」——它只能確認「記錄的雜湊 vs 磁碟現況」一致與否，記錄本身
  是否已經過時（該被移除卻還在 lockfile 裡）不在它的偵測範圍。
- **判定**：**REFUTED / 缺口**。

## Disposition（不是「延後」，是超出本輪範圍的獨立子系統）

Python 的 drift 偵測需要：(a) 專案樹外的 scratch 目錄生命週期管理、
(b) 對目前 lockfile 執行一次完整、只讀、僅用本地快取的重新物化安裝、
(c) governed-target-dir 過濾 + normalize 過的三向樹狀 diff、(d) canvas
bundle 等特殊排除規則。這是一個獨立的「install-replay 引擎」，其複雜度
與工作量遠超「呼叫既有 ContentScanner」（Phase 7 `--content` 的範圍），
也超過 `design.md` Gate 1 disposition 表原先框定的「共用 ContentScanner
兩處接線」。

**判定**：`unintegrated` 的 never-recorded-but-should-exist 邊界案例，與
`orphaned` 整類，歸類為 disposition 表的 **(iii) 獨立子系統，非本輪**——
與 `--ci`/`--policy`/`--external`/`--format`/`-o`/`--strip` 同一類：apm-go
目前完全沒有 install-replay 的任何基礎設施（沒有 scratch 物化、沒有
governed-dir 樹狀 diff），這不是「可做而不做」的切割，是需要獨立設計/
實作的另一批功能。**必須在 Gate 6b「此修正不做什麼」具體列出**（而非
「audit drift 延後」這種模糊帶過），且不得重蹈 2026-07-13 codex B1 揭露的
過度宣稱（見上方修正註記）：

> apm-go `audit --content` 只補齊 Python bare audit 的隱字掃描 pillar。
> Python bare audit 預設另外執行的 install-replay drift 偵測（三分類：
> `modified`/`unintegrated`/`orphaned`，見
> `research/audit-drift-reconciliation.md`）未被移植。apm-go 現有的
> SHA-256 `deployed_file_hashes` 重驗（bare `audit`，無 `--content`）比對
> 基準是 lockfile 記錄值，不是重新安裝的重放值：`modified` 部分重疊
> （同一個「檔案被手動竄改」場景兩邊都能抓到，機制不同）；`unintegrated`
> 部分重疊（lockfile 已記錄但磁碟缺失的常見案例會被抓到並回報
> `<missing>` violation，但「從未被記錄進 lockfile、卻依當前 lockfile
> 重新物化『應該』產出」的邊界案例偵測不到）；`orphaned`
> （已追蹤但重裝後不會再產出的殘留檔）**完全無偵測能力**——apm-go 沒有
> 「重新計算現在應該產出什麼」的能力，無法判斷某個已記錄檔案是不是重裝後
> 不會再有的殘留。上述未覆蓋的部分都需要獨立 install-replay 引擎的另一批
> 功能，非本輪「共用 ContentScanner」範圍內的切割。
