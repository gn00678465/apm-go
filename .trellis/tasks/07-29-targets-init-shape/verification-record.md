# 驗證紀錄 — 07-29-targets-init-shape

> **狀態：待使用者驗證。任務未標記完成。**
> 依使用者指示：「任務結束不得翻完成，使用者會介入驗證」。
> `task.py finish` **未執行**，task 狀態維持 `in_progress`。

- 實作者：`trellis-implement` 子代理（三輪）
- 驗證者：主 session（獨立重跑，不採信子代理回報）
- 外部稽核：fresh-context subagent（兩輪對抗性稽核）
- 最後更新：2026-07-30

---

## 1. Tier 1 — 確定性閘門

```
pwsh -NoProfile -File .trellis/tasks/07-29-targets-init-shape/verify.ps1
→ TIER 1 GREEN, EXIT=0
  32 項全綠 · 覆蓋率 total 86.9% · internal/ux 96.8%
```

### 閘門本身的可信度：mutation 測試

**沒有做過 mutation 的閘門檢查等於不存在。** 以下每一條都由主 session 實際執行：
改壞被測的那一行 → 確認閘門/測試轉紅 → 還原。

| # | Mutation（改壞什麼） | 預期抓到的檢查 | 實際結果 |
|---|---|---|---|
| 1 | `target.go` `SupportedTargets = deployTargets[:5]` | `TestSupportedTargets_MatchesDeployTargetsExactly` | ✅ 紅：`len(SupportedTargets) = 5, len(deployTargets) = 6` |
| 1b | 同上，觀察**舊**測試 | `TestSupportedTargetsSet_MatchesAdapterTargetsAndPromptMenu` | ✅ **仍綠** —— 證實 B2 缺口為真 |
| 2 | `init.go` 刪掉 `lenientReadTargets` 退路 | `TestReadExistingTargets_LenientOnUnrelatedParseError` | ✅ 紅：`got [], want [claude]` |
| 3 | `pack.go:131` `targets = m.Target` → `nil` | `TestPack_LegacySingularTargetKey_StillResolves` | ✅ 紅：`'target:' does not include 'claude' or 'copilot'` |
| 4 | `manifestnode.go` devDependencies 移到 scripts 之後 | `TestBuildManifestNode_PluginMode_DevDependenciesKeyOrder` | ✅ 紅：印出實際與期望鍵序 |
| 5 | 註解第 2 行首字母改小寫 | **閘門 AC6**（逐行 `-ceq`） | ✅ 紅：印出逐字差異 |
| 6 | `manifestStrNode` 加 `Style: LiteralStyle` | M6 round-trip | ⚠️ **沒紅** —— 此 mutation 無效（YAML 編碼器夠健壯），改用 #7 |
| 7 | `manifestStrNode` 用 `SplitN(v,":",2)[0]` | `TestBuildManifestNode_SpecialCharacterScalars_RoundTrip` | ✅ 紅：`Description round-trip = "desc"` |
| 8 | `lenientReadTargets` 拿掉 `isSingular` 分支 | `TestReadExistingTargets_LenientDoesNotSplitCSVOnPluralKey` | ✅ 紅：`got [claude copilot], want nil` |
| 9 | `lenientReadTargets` 的 `root.Kind != MappingNode` 改成 `if false` | `TestReadExistingTargets_RootNotMapping_ReturnsNil` | ✅ 紅：`got [claude], want nil` |

**#6 誠實記錄**：這個 mutation 沒有讓測試轉紅，代表它不是一個有效的 mutation，
不代表測試沒用。改用 #7 才證明 M6 的斷言是活的。

**子代理回報但主 session 未複驗的 mutation**：`TestReadExistingTargets_BlankTokenAfterTrim_IsSkippedNotKept`
的 guard 被移除時**不會**轉紅（因為 `manifest.ValidateTarget("")` 已先擋掉）。
子代理主動回報了這一點而非隱瞞。**此測試鎖住的是外部可觀察行為，不是那一行 guard。**

---

## 2. Tier 2 — 外部對抗性稽核

### codex 不可用（記錄事實，非藉口）

`codex exec -s read-only` 完全跑不了：

```
setup refresh failed to launch helper:
helper=codex-windows-sandbox-setup.exe, error=program not found
```

`C:\Users\gn006\.codex\.sandbox-bin` 只有 `codex.exe` 與 `codex-command-runner`，
sandbox setup 執行檔遺失。**未以 `--sandbox danger-full-access` 繞過** —— 那會拿掉
稽核提示裡承諾的唯讀保證。改用 `AGENTS.md` §5 明列的替代方案：fresh-context subagent。

### 第一輪：1 個阻斷級

**`lenientReadTargets` 把單數鍵的 CSV 規則誤套用到複數鍵。**

- `manifest.go:265`（單數 `target:`）→ `strings.Split(raw, ",")`，**有** CSV 切分
- `manifest.go:312-313`（複數 `targets:`）→ `[]string{val.Value}`，**沒有** CSV 切分
- 舊的 `lenientReadTargets` 對所有 ScalarNode 一律切分

主 session 獨立複現（臨時探針，已刪除）：

```
apm.yml = name: p / version: "1.0.0" / targets: claude,copilot
ParseManifest: err=unknown target "claude,copilot"
readExistingTargets() = [claude copilot]     ← 憑空造出兩個「合法」target
```

修復後五個情境全部複驗：

```
plural CSV (invalid)        Parse=unknown target "claude,copilot" → []
plural CSV, no version      Parse=unknown target "claude,copilot" → []
singular CSV, no version    Parse=version is required             → [claude copilot]
singular alias, no version  Parse=version is required             → [copilot]
plural seq, no version      Parse=version is required             → [claude codex]
```

單數 CSV 糖與 `vscode`→`copilot` alias 正規化都保住，複數 CSV 正確被拒。

### 第二輪：0 阻斷級、1 重大、5 條零覆蓋分支

**重大 —— 一句被證偽的宣稱。** 原註解寫「fallback for a document that fails
ParseManifest **for a reason unrelated to targets**」。實測反證：

```
apm.yml = name: p / version: "1.0.0" / targets: [claude, {foo: bar}]
Parse = unknown target "<!!map>"        ← 失敗原因就是 targets 自己
readExistingTargets() = [claude]        ← 仍然 fallback 了
```

`manifest.go:100-196` 的單一 range 迴圈遇到第一個錯誤就 return，早於
`manifest.go:204` 的 version 檢查，所以 targets 自身的錯誤同樣會讓整份文件失敗。

**裁定：保留行為，只修宣稱。** 理由（不是斷言）：

1. blast radius 由稽核者 grep 全 repo 確認 —— `readExistingTargets` /
   `lenientReadTargets` 只有 `init.go:147` **一個**呼叫點，回傳值只餵
   `targetSelectOptions` 的預選 map，從不寫回 apm.yml、從不參與部署目標解析
2. 使用者跑互動式 `init` 本來就是要**覆寫** apm.yml；從壞檔案裡撈出唯一合法的
   `claude` 來預選，比整組丟掉更有用
3. best-effort 搶救本來就是這個函式的定位 —— 缺陷在註解把它寫成了別的東西

**5 條零覆蓋分支**（`go tool cover` 確認 0 次命中）已全部補測試並納入閘門：
`init.go` 的 SafeLoad 失敗、非 DocumentNode、頂層非 mapping、無 target 鍵、
token trim 後為空。

---

## 3. AC 逐條證據

| AC | 驗法 | 結果 |
|---|---|---|
| AC1 | binary 跑 `init --yes --target claude,codex,opencode`，檢查產物 | ✅ 複數 `targets:`，無單數殘留 |
| AC2/AC3 | 5 個測試：BothKeys（含 CSV 糖 / alias）、LenientOnUnrelatedParseError、LenientDoesNotSplitCSVOnPluralKey、PreselectsExistingTargets | ✅ 全綠，mutation #2/#8 證實 |
| AC4 | 函式層 + **CLI 層**（`installCmd().Execute()`）兩個測試 | ✅ |
| AC5 | 產物鍵序逐一比對 | ✅ |
| AC6 | 閘門逐行 `-ceq`，Accepted values 六個 | ✅ mutation #5 證實 |
| AC7 | 連續五行 `strings.Contains` 逐字比對 | ✅ |
| AC24 | binary `init --target agent-skills` | ✅ exit 0 |
| AC25 | **3 個**測試實跑（新增與來源 `deployTargets` 的雙向比對） | ✅ mutation #1/#1b 證實 |
| AC26 | 替換來源切片的行為測試 | ✅ |
| AC27 | `CanonicalTargets` 未動 | ✅ |
| AC29 | install **與 pack** 兩條鏈 | ✅ mutation #3 證實 |
| AC-L0 | 三個 func 已為 package-level var | ✅ |
| AC-L3 | seam 自身 3 個測試，含 restore 還原；ux 覆蓋率 96.8% | ✅ |
| AC-L1 | build / vet / 全套件 test / coverage 86.9% | ✅ |
| AC-L2 | `git diff -- go.mod` 與 `--cached` 皆無新 require | ✅ |

---

## 4. 這一輪抓到的流程問題（留作紀錄）

1. **子代理兩次未遵循硬性約束**
   - 第一次：明確要求「只改 `verify.ps1`，不得動任何 `.go`」，它改了 9 個 Go 檔並
     把功能一起實作完 → 「閘門先紅」那個可否證的中間狀態被跳過。
     補救：改用 mutation 測試取得更強的保證。
   - 第二次：要求把 5 個新測試加進 `$acTests`，它沒加 → 測試存在但閘門不知道，
     刪掉也不會紅。由主 session 補上並實跑驗證。
   **教訓**：子代理的回報必須逐項對照原始指令查核，不能只看「有沒有綠」。

2. **codex 環境壞掉時不得降低驗證標準**
   沒有用 full-access 繞過，改用同樣被 `AGENTS.md` §5 認可的 fresh-context subagent。

---

## 5. 仍未處理 / 交由使用者裁定

- **AC6 閘門的索引風險（不可達，未修）**：`verify.ps1` 的
  `$lines[($idx-4)..($idx-2)]` 若 `$idx <= 4`，PowerShell 的負索引會從陣列尾端
  環繞取值而非報錯。目前**沒有任何呼叫路徑**餵給它無 targets 的產物，
  所以不可達。兩輪稽核都判定為觀察項而非缺陷。**未修，明列於此。**
- **`internal/ux` 的 seam 是 package-level 可變狀態**：目前安全的前提是
  「全 repo 無 `t.Parallel()`」，兩輪稽核各自 grep 驗證為零命中，且 Go 的
  test binary 逐 package 獨立 process、不共享記憶體。此前提已寫進
  `testhooks.go` 的註解。**若日後有人加 `t.Parallel()`，這個前提就失效**，
  但目前沒有任何機制會偵測到 —— 這是一個已知且未被閘門守住的缺口。
