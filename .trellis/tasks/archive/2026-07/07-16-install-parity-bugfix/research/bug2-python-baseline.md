# BUG-2 Python A/B 基準（implement.md 步驟 3）

## 可重現性資訊（codex L3）

- 日期：2026-07-16
- Python apm：v0.21.0（`a9a883b3`），執行方式 `uv --project /d/Projects/apm-dev/apm run apm`
- repo SHA：`mattpocock/skills @e9fcdf95b402d360f90f1db8d776d5dd450f9234`、
  `antfu/skills @a74f281a27dadc02397bc1a174b0f2c97531b6ae`
- 專案 fixture：`apm.yml`（name/version/description + `target: [claude]` +
  `dependencies.apm: []`），空目錄起跑
- lockfile 檔名：`apm.lock.yaml`

## 情境 1 ｜ 兩步驟（BUG-2 主情境）

```
apm install mattpocock/skills --skill grill-me
apm install antfu/skills --skill vue
```

- 第 1 步後：apm.yml `{git: mattpocock/skills, skills: [grill-me]}`；
  `.claude/skills/` = `grill-me`。
- 第 2 步後：apm.yml 兩筆各自帶子集；`.claude/skills/` = `grill-me, vue`。
- **結論：Python 無污染**。注意其模型：第 2 步輸出 `Installing 1 new package...`——
  **只處理新 package，既有 dep 完全不重佈署**（與 apm-go full-manifest re-deploy 模型不同；
  apm-go 修法不必改成 Python 模型，只需讓重佈署尊重子集，結果等價）。

## 情境 2 ｜ 同 repo 三階段（union / C3 對照）

```
apm install mattpocock/skills --skill code-review   # 既有 dep 已有 [grill-me]
```

- apm.yml 變為 `skills: [code-review, grill-me]` → **additive union 證實**（issue #1771）。
- lockfile `skill_subset: [code-review, grill-me]`。
- **但 `.claude/skills/` 只剩 `code-review`（+ vue）**——輸出
  `Cleaned 3 stale files from mattpocock/skills`，把 union 內的 grill-me 也清了。
- 隨後 bare `apm install`：`2 skill(s) integrated` → `.claude/skills/` 收斂回
  `code-review, grill-me, vue`。
- **Python 自身缺陷 P-D1**：同 dep 重裝時只佈署「當次 CLI 名單」並 stale-clean 其餘，
  造成 manifest（union）與檔案系統的**暫時性帳實不符**，直到下次 bare install 才收斂。
  **apm-go 不得複製此行為**：有效子集 = union，佈署即收斂（design §1.2c 的唯一計算點
  天然避免此缺陷）。

## 情境 3 ｜ unknown skill

```
apm install antfu/skills --skill zzz-not-exist-xyz
```

- **exit 0**，apm.yml 變為 `skills: [vue, zzz-not-exist-xyz]`、lockfile `skill_subset`
  同步記入幽靈名稱；後續每次 install 靜默忽略（不部署、無警告）。
- 且本次重裝再度觸發 P-D1：vue 被 stale-clean（bare install 後才回來）。
- **Python 自身缺陷 P-D2**：幽靈 skill 永久污染 manifest/lockfile、無任何診斷。
- **apm-go 政策定案依據（prd B2-6）**：採「新輸入不存在 → 報錯 + 原子性零變更」，
  **比 Python 嚴格**；依 prd「不污染帳本」原則，此差異為刻意、記錄在案。

## 其他觀察

- Python 純 local/skill 安裝也印 `[i] Added apm_modules/ to .gitignore`（R18 對照）。
- lockfile `deployed_files` 逐檔記錄（含目錄行 `.claude/skills/vue`）。
- `skill_subset` 排序去重持久化。

## 對 apm-go 實作的直接約束

1. union 語意照抄（情境 2 證實）；`--skill '*'` RESET 未在本輪重測（apm-go 既有測試已鎖）。
2. 佈署一律用有效 union（不佈「當次 CLI 名單」）——避免 P-D1。
3. unknown skill 報錯（刻意嚴於 Python 的 P-D2）。
4. stale-clean 機制 Python 已有（`Cleaned N stale files`）——apm-go 的 reconciliation
   （design §1.2g）語意對齊「按 dep 舊帳本減新集合」，但必須以 union 為新集合、
   不能以當次 CLI 名單（否則重蹈 P-D1）。
