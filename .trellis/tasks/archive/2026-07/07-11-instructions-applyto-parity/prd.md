# install instructions applyTo 轉換與零 target 閘門 parity

> Parent: `07-05-runtime-parity-gaps`。源自 2026-07-10「`install` 未帶參數時
> `.apm/instructions/**`/`.apm/agents/**` 作動方式」live A/B 驗證
>(fixture: scratchpad noargs-{go,py})。

## 背景 — 原版的配對契約(使用者 2026-07-11 點出)

Python 原版中 **`.instructions.md` 命名與 `applyTo` frontmatter 是一組契約**:
`find_instruction_files`(`instruction_integrator.py:48-52`)只收 `*.instructions.md`,
其 `applyTo` 由 `_FORMAT_CONVERTERS`(:40-46)按 target 轉換成該工具的原生 scoping
語意。apm-go 目前**收了檔名卻丟了語意**——原樣複製,`applyTo` 對 Claude Code 無效。

## Live A/B 發現(2026-07-10,兩邊同構 fixture)

| # | 面向 | apm-go | Python | 評級 |
|---|---|---|---|---|
| 1 | claude instructions 內容 | byte-copy(`applyTo:` 保留) | `applyTo: "**/*.go"` → `paths:\n  - "**/*.go"`(`_convert_to_claude_rules`,:670-703) | **P1 修** |
| 2 | local primitives + 零 target | silent exit 0 不部署(hasAnyDeps 只算 deps) | exit 2 `No harness detected` | **P2 修** |
| 3 | 收集過濾 | `.apm/instructions/` 任意 `*.md`(fallback,`primitive.go:152-160`) | 僅 `*.instructions.md` | **P2 修**(對齊配對契約) |
| 4 | agents 內容 | byte-copy(LF 保真) | 文字重寫(Windows CRLF) | deviation:apm-go 較保真,**不改** |
| 5 | `.gitignore` 自動補 apm_modules/ | 無 | 有 | cosmetic,不改(記錄) |
| 6 | 空專案(無 primitives/deps/signal) | exit 0 | exit 0 | 一致 ✅ |

Python 轉換器適用範圍(對 apm-go 的映射):
- `claude_rules`:applyTo→`paths:` — **apm-go 缺,本 task 主體**
- copilot:不在轉換表(applyTo 為 copilot 原生語意)→ 兩邊 byte-copy 已對齊,不動
- `antigravity_rules`(剝除 frontmatter):**已於 07-05-antigravity-research 拍板
  documented deviation(維持 byte-copy),不重開**
- `cursor_rules`/`windsurf_rules`/`kiro_steering`:apm-go 無此三 target adapter,範圍外

## Requirements

1. **claude instructions 轉換**(P1):部署到 `.claude/rules/<n>.md` 時,將
   `applyTo` frontmatter 轉為 `paths:` 清單,行為對照 Python
   `_convert_to_claude_rules`(:670-703;實作前先完整閱讀該函式邊界:
   無 frontmatter、無 applyTo、多值、非字串值等 case)。其他 target 的
   instructions 路徑不受影響(copilot/antigravity 維持 byte-copy)。
2. **零 target 閘門擴充**(P2):`install` 於「deps **或 local primitives** 存在
   且零 resolvable target」時 exit 2 + 教學訊息(對照 Python `No harness detected`;
   訊息措辭沿用 apm-go 既有 `no deployment target detected...`)。空專案維持 exit 0。
   注意勿破壞 F2 既有契約(`install-marketplace-contracts.md` §2 矩陣需同步更新)。
3. **收集過濾收斂**(P2):`.apm/instructions/` 僅收 `*.instructions.md`
   (`extractInstructionName` 移除 plain `.md` fallback),對齊配對契約。
   ⚠ 需先盤點既有測試/fixtures 是否依賴 plain `.md` 收集;agents/commands/prompts
   的收集規則不動。
4. deviation 記錄:#4 agents 行尾、#5 .gitignore 記入 spec(install-marketplace-contracts
   或新節),不實作。
5. A/B 腳本:`D:\Projects\apm-dev\evals\ab_instructions_applyto.py`(比照慣例),
   覆蓋上表 #1/#2/#3/#6 的兩邊對照。

## Acceptance Criteria

- [x] claude 轉換:live A/B 語意等價(5 case × paths/body 斷言全 PASS);
      unit 18 case 表測含轉換函式 100% 覆蓋(commit `04f4e58`)
- [x] 零 target 閘門:local primitives only → exit 2(兩邊 A/B 一致);
      空專案 exit 0;F2 deps-present 回歸綠;共用 errNoDeployTarget() 防漂移
      (commit `ccc2c9d`)
- [x] 收集過濾:plain `.md` 不收(unit + A/B 兩邊);survey 確認 cmd/apm 無依賴
- [x] deviation(agents CRLF / .gitignore)記入 spec deviations 表;
      F2 矩陣拆列 + quality-guidelines 過時列修正;新增 spec §8 instructions pipeline 契約
- [x] `ab_instructions_applyto.py` 25/25 PASS;build/vet/test 全綠;
      deploy 88.2%(轉換函式 100%)
- [x] conformance 清單 §8:19 項硬性驗證全數 `[x]`(live+unit 雙證據)

## 完成記錄與 follow-up(2026-07-11)

- commits:`04f4e58`(轉換+過濾)、`ccc2c9d`(閘門+spec 矩陣)。
- **Codex 獨立驗證(2026-07-11,`--sandbox danger-full-access`,gpt-5.5)**:
  **PASS-with-notes**,0 CRITICAL/HIGH/MEDIUM;轉換 parity 逐項吻合無語意分歧;
  runtime 證據獨立重現(build/vet/test 綠 + ab 腳本 25/25)。
- **Codex 硬性 checklist 逐項重驗(同日,使用者指示補做)**:conformance §8 全 19 項
  以對抗性方式重現(claim 不採信、自跑測試+讀測試本體+自建 fixture)——
  **19/19 CONFIRMED**,無實質出入。
- **follow-up(範圍外,記錄待決策)**:
  1. manifest 僅有 `dependencies.mcp` + 零 target 仍靜默 exit 0(閘門只計 apm deps
     與 `.apm/` primitives;Codex LOW-C 獨立相符)。Python 該情境行為未查證。
  2. **收集範圍差異(Codex LOW-B 新發現)**:Python `find_files_by_glob` 除
     `.apm/instructions/` 外也掃 **package root** 的 `*.instructions.md`,且跳過
     symlink/hardlink;Go 僅掃 `.apm/instructions/`、無 link 過濾。既有範圍差異
     非本輪回歸;待決策:補 parity 或記 documented deviation。
  3. `update` 不走零 target 閘門(既有 gap,與 update 不 materialize local deps 同族)。
  4. 未測邊角(INFO):applyTo 值含 tab/CR 的逸出、未閉合開括號 —— 轉換函式已定義
     行為,補測待前三項定案時一併。

## Non-Goals

- 不新增 cursor/windsurf/kiro target。
- 不重開 antigravity frontmatter 剝除決策(維持 deviation)。
- 不改 agents 行尾行為(byte-copy 保真較優)。
- 不做 `.gitignore` 自動補寫。
