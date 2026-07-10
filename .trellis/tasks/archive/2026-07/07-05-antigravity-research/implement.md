# Implement — antigravity CLI 對齊修正(研究定案後的三個小 fix + 實機驗證)

> 前置:prd.md 兩輪決策(2026-07-05 / 2026-07-10)已定案;研究依據 research/cli-*.md。
> 原版 Python apm 不作為本輪 oracle —— A/B 以本機 `agy` 1.0.16
> (C:\Users\gn006\AppData\Local\agy\bin\agy.exe)實機驗證替代。

## Step 0: 實機前置驗證(寫程式前,一次性)

在 scratchpad 建立試驗 workspace(不碰真實專案與 ~/.gemini):

- [ ] **agents 掃描實測**:建 `.agents/agents/probe-agent/agent.md`(frontmatter
      name/description + body),啟動 agy 確認 `/agents` 面板列出 probe-agent。
      1.0.16 掃描行為是 changelog 推論(cli-subagents.md Caveat),必須實測。
- [ ] **rules 載入實測**(使用者 2026-07-10 指定;Codex M3 修訂):probe rule 用
      **真實 APM instructions 形狀** —— YAML frontmatter(`applyTo: "**"` 等)+ body
      含可辨識指示(如「回覆開頭必須帶 [PROBE-RULE-LOADED] 標記」)。
      同時驗兩件事:(a) CLI 是否載入 `.agents/rules/`;(b) byte-copy 保留的
      frontmatter 是否被忽略/污染 prompt(deviation 是否安全)。
- [ ] agy TUI 為互動式,無非互動 flag(研究已確認無 `agy agents`/`agy mcp` 子指令)。
      自動化困難時改為:引導使用者以 `! agy` 手動驗證,結果記入 prd.md。
      **證據格式**(Codex M4):記錄 agy version、workspace tree、輸入 prompt、
      observed output;無法驗證 → 對應 AC 標 blocked,不得默默通過。
- 驗證結果任一為「不支援」→ 回 prd.md 修訂對應決策再繼續(rules 不支援則部署照舊
  但 deviation 註記升級;agents 不支援則該 fix 暫緩、記缺口)。

## Step 1: sse→serverUrl fix(獨立,最小)

- [ ] `internal/deploy/mcp_antigravity.go`:`antigravityMCPEntry` 移除
      `if r.Transport == "sse" { e["url"] = r.URL }` 分支,非 stdio 一律
      `e["serverUrl"] = r.URL`
- [ ] `internal/deploy/mcp_writers_test.go:116-133`:
      `TestWriteMCP_Antigravity_SSEUsesURLField` → 改名 + 斷言 sse 輸出 `serverUrl`
      且**不得**有 `url`(TDD:先改測試 RED → 改實作 GREEN)
- [ ] `conformance/conformance-kit/oracle/targets/expected/antigravity.yaml`(git-ignored
      本機檔):`http_field: serverUrl` 已通用,確認無 sse 例外註記即可
- 驗證:`go test ./internal/deploy/ -run TestWriteMCP`

## Step 2: explicit-only 對齊(2026-07-05 拍板)

- [ ] `internal/deploy/adapter.go`:antigravity 加入 `explicitOnlyTargets`、
      移出 `allAutoDetectableTargets()`;更新 :118-122 註解(原註解引
      acceptance-checklist 的 auto-detect 結論,已被使用者決策推翻 —— 註明對齊
      Python `EXPLICIT_ONLY_TARGETS` 與拍板日期)
- [ ] `internal/manifest/detect.go`:`SignalWhitelist` 移除 `GEMINI.md`/`AGENTS.md`
      兩條(Python v2 SIGNAL_WHITELIST 亦無 antigravity 條目)
- [ ] `internal/manifest/target.go`:`TargetAliases` 補 `"agy": "antigravity"`
- [ ] **alias 流通驗證**(Codex H2):`SplitTargetFlag` 走 `ValidateTarget` 已正規化
      flag 路徑;**apm.yml `target:` 路徑是否於解析時正規化須查證** —— 若原 token
      直進 `ResolveTargets`,`filterSupported` 會把 `agy` 丟掉。未正規化則於
      manifest 解析或 ResolveTargets 補 canonicalize。
- [ ] 回歸測試(**ResolveTargets 層級**,兩路徑;Codex H1 測試矩陣):
      - `--target antigravity` ✓ / `--target agy` ✓
      - apm.yml `target: [antigravity]` ✓ / `target: [agy]` ✓
      - `--target all` 不含 antigravity / apm.yml `target: [all]` 不含
      - 專案根僅有 GEMINI.md / AGENTS.md 時 `DetectTargets` 不回傳 antigravity
      - `--target gemini` 的 checkUnsupported diag 行為維持(無 adapter,不受本輪影響)
      - 既有 detect_test.go / adapter 相關測試同步修正
- 驗證:`go test ./internal/manifest/ ./internal/deploy/`

## Step 3: agents primitive(2026-07-10 拍板,documented extension)

- [ ] `internal/deploy/antigravity.go`:`SupportedTypes` 加 `TypeAgents`;
      `DeployPrimitive` 加 case:
      `deployFileToPath(p, fmt.Sprintf(".agents/agents/%s/agent.md", p.Name), projectDir)`
- [ ] `internal/deploy/deploy_test.go` support matrix(:930-936 附近):antigravity
      的 unsupported 清單移除 `TypeAgents`,新增部署路徑斷言
      (`.agents/agents/<name>/agent.md`,注意 per-agent 目錄層)
- [ ] 原樣複製(byte-copy),不轉換 frontmatter —— 與 adapter 全域模式一致
- [ ] **生命週期回歸**(Codex M1):
      - install 後 lockfile/部署記錄含 `.agents/agents/<name>/agent.md`
      - uninstall 清除該檔且保留 sibling agent 目錄
      - 同名 agents 衝突:比對 claude adapter 現行語意(`.claude/agents/<name>.md`
        同名行為),antigravity 對齊並以測試鎖定(last-wins + 診斷,或現行語意)
- 驗證:`go test ./internal/deploy/ ./cmd/apm/`(uninstall 回歸)

## Step 4: 全量驗證 + A/B(agy 實機)

- [ ] `go build ./...` && `go vet ./...` && `go test ./...`(全綠)
- [ ] `go test ./... -cover`(新增/修改套件 ≥ 80%)
- [ ] **A/B-1 結構比對**:fixture 專案 `apm-go install --target antigravity`,
      逐欄斷言:`.agents/mcp_config.json`(sse server 有 `serverUrl` 無 `url`)、
      `.agents/skills/<n>/SKILL.md`、`.agents/agents/<n>/agent.md`、
      `.agents/rules/<n>.md` —— 對照 research/cli-*.md 的 binary 驗證 schema
- [ ] **A/B-2 agy 實機驗證(supplemental smoke,Codex L1)**:將部署產物包成
      plugin fixture(`plugin.json` + skills/ + agents/ + mcp_config.json + hooks.json)
      → `agy plugin validate <dir>` 應回報各 component processed。
      **主 oracle 是 A/B-1 結構比對**;validate 僅輔助,plugin 系統變動不得使本 task
      失敗(rules 不在 validate 範圍,以 Step 0 結果為準)
- [ ] Step 0 的 probe workspace 以 apm-go 產物重跑一次(部署→agy 發現)

## Step 5: 收尾

- [ ] prd.md:回填 Step 0/4 驗證結果與 AC 勾選
- [ ] spec 更新(trellis-update-spec):antigravity target 契約
      (serverUrl、explicit-only、agents extension、deviations)
- [ ] commit(原子化,無 attribution footer)→ /trellis:finish-work

## Rollback points

- 每 Step 一個 commit;Step 0 驗證不過 → 只影響對應 Step,其餘照常。
- explicit-only 若使用者反悔(auto-detect 有官方依據)→ 單獨 revert Step 2 commit。
