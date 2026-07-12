# Implement: apm-go `compile` — 最小 agents-family 子集

> **核准 — 2026-07-11**（依 design.md 已核准拍板；6 點依建議值確認，見
> design.md 檔頭）。Step 0-4 已完成，見 checklist.md 逐項驗證與
> `.trellis/spec/backend/compile-contract.md`。

安全鐵則:所有 live 探測/A/B 一律在 TEMP scratch;絕不在 repo 根跑 compile
(repo 根有手寫 AGENTS.md,full 模式會整檔覆蓋);oracle 只跑 `compile`,
禁止 marketplace add/remove/update。

## Step 0 — 實作前 oracle probe(補研究缺口)

- [ ] scratch 專案加一個 `apm_modules/acme/dep/.apm/instructions/dep.instructions.md`,
      跑 oracle `compile --single-agents --no-links --no-constitution -t antigravity`,
      確認依賴 instruction 的 `<!-- Source: -->` relpath 形狀與排序位置
      (預期 `apm_modules/acme/dep/.apm/instructions/dep.instructions.md`)。
- [ ] 同 scratch 驗證:同名 instruction(local vs dep)oracle 只輸出 local 版
      (discovery.py:181-184 先到先贏)。
- [ ] 驗證 `applyTo: ['**/*.py']`(YAML list)oracle 的分組 heading 用第一元素。
- 驗證:probe 輸出貼進 task notes;與 design.md §4 不符處先改 design 再動工。

## Step 1 — internal/compile 套件(TDD:先測後寫)

- [ ] `internal/compile/frontmatter.go`:instruction frontmatter 解析
      (applyTo scalar/list-first-element、body 去 frontmatter)——重用/提升
      `deploy.claudeFrontmatterRE`(instructions_claude.go:13),不複製第二份 regex。
- [ ] `internal/compile/template.go`:分組(raw applyTo 字串為 key)、排序
      (global 組 relpath 序;pattern 組 pattern 字典序、組內 relpath 序)、
      空 body 過濾、Source/End source 包裹、header/footer(design §4)。
- [ ] `internal/compile/buildid.go`:placeholder 替換 + SHA256 前 12 hex(design §5)。
- [ ] `internal/compile/compile.go`:收集(重用 deploy 收集順序與
      `CollectLocalPrimitives`/`CollectDependencyPrimitives`,含先到先贏去重)→
      render → idempotent write(byte-equal 跳寫、temp+rename)。
- 驗證:`go test ./internal/compile/ -race -cover` 綠且 ≥ 80%。

## Step 2 — cmd/apm/compile.go

- [ ] cobra 指令 `compile`,flag 只有 `-t/--target`(design §2)。
- [ ] 專案 gate:無 apm.yml → exit 1(訊息對齊 oracle);無 instructions 內容 → exit 1。
- [ ] target 解析:`deploy.ResolveTargets` → 過濾 {antigravity, codex, opencode};
      空集或僅非 agents-family → exit 2 + `not implemented in apm-go yet`。
- [ ] `main.go` 註冊 `root.AddCommand(compileCmd())`。
- 驗證:`go build -o bin/apm-go.exe ./cmd/apm`;scratch 手測
      `apm-go compile -t antigravity` 產出正確 AGENTS.md;重跑印 idempotency 訊息。

## Step 3 — A/B 腳本

- [ ] `evals/ab_agents_compile.py`(模式仿 ab_instructions_applyto.py):
      案例=global/scoped/comma-list/brace/空 body/YAML-list applyTo/依賴 instruction/
      同名衝突;oracle 側 `--single-agents --no-links --no-constitution`;
      normalize Build ID 行、APM Version 行、`\r`;另驗兩邊 Build ID 重算自洽、
      idempotency(連跑兩次 byte-identical)、exit code 案例
      (無 apm.yml → 兩邊 1;claude-only target → Go 2,記 documented deviation)。
- [ ] docstring 記錄全部 documented deviations(design §1 Non-Goals + §4 兩行 normalize)。
- 驗證:`python evals/ab_agents_compile.py` 全 PASS(FAIL 0)。

## Step 4 — 全 repo 驗證 + spec 落檔

- [ ] `go fmt ./... && go vet ./... && go build ./... && go test ./... -cover` 全綠,
      新套件覆蓋 ≥ 80%。
- [ ] 新增 `.trellis/spec/backend/compile-contract.md`:輸出契約(design §4/§5)、
      target gating、exit codes、deviations 清單、A/B 腳本指引;
      `antigravity-target-contract.md` 補一行互見。
- [ ] deviation 記錄齊備(PRD AC:「A/B 對照通過,deviation 記錄」)。

## Review gate(全部勾完才可進 finish-work)

- [ ] design.md 的 6 個拍板點(findings.md「需拍板事項」)已由使用者確認,未確認前不動工
- [ ] A/B 全 PASS 且輸出貼進 task notes(證據留檔)
- [ ] 未觸碰 install/deploy/detect 既有行為(`git diff` 檢查改動面只有新增檔 + main.go 一行)
- [ ] repo 根 AGENTS.md 未被本輪任何測試改動(`git status` 乾淨)
- [ ] code-review(go-reviewer)無 CRITICAL/HIGH
