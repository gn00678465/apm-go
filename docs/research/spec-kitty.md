# Spec Kitty v3.2.5 作為 apm-go 的主要 SDD 工作流 — 研究筆記

日期：2026-08-29。研究範圍：唯讀檢視，未執行任何會改動本 repo 的指令。

## 0. 來源與可信度標記

| 標記 | 意義 |
|---|---|
| **[wheel]** | 從 PyPI 下載的 `spec_kitty_cli-3.2.5-py3-none-any.whl` 解包後的原始碼／打包資料，路徑以 site-packages 相對路徑表示（例如 `specify_cli/cli/commands/dispatch.py`）。這是 v3.2.5 的權威來源。 |
| **[help]** | 以 `uvx --from spec-kitty-cli==3.2.5 --with typer==0.24.2 spec-kitty <cmd> --help` 實際跑出的 3.2.5 說明文字（在隔離的 `HOME` 下執行，見 §1.3）。 |
| **[docs@v3.2.5]** | GitHub `Priivacy-ai/spec-kitty` 在 tag `v3.2.5` 的 `docs/` 內容（`raw.githubusercontent.com/Priivacy-ai/spec-kitty/v3.2.5/...`）。文件更新日期不一定與 3.2.5 同步，有些段落描述舊版行為，已個別標註。 |
| **[README@main]** | `Priivacy-ai/spec-kitty` `main` 分支 README（可能領先 3.2.5）。 |
| **[local]** | 本 repo 內已存在的檔案（`.kittify/`、`.claude/`、`.gitignore` 等），附行號。 |
| **未驗證** | 沒有從上述來源直接證實的敘述。 |

PyPI 套件名稱是 **`spec-kitty-cli`**（不是 `spec-kitty`），`pip show spec-kitty` 因此找不到。`curl https://pypi.org/pypi/spec-kitty-cli/json` 回傳：`version: 3.2.5`、`requires_python: >=3.11`、`Repository: https://github.com/Priivacy-ai/spec-kitty`、`Documentation: https://docs.spec-kitty.ai/`、summary「Spec Kitty, a tool for Specification Driven Development (SDD) agentic projects, with kanban and git worktree isolation.」。3.2.5 之後 PyPI 上只有 `3.2.6rc1`、`3.2.6rc2`（pre-release），3.2.5 仍是最新穩定版。

---

## 1. 安裝、PATH 與版本驗證

### 1.1 本機現況（[local]）

- `which spec-kitty`、`pip show spec-kitty-cli`、`uv tool list`、`ls ~/.local/bin` 都找不到 spec-kitty；`pipx` 未安裝。`uv tool list` 只有 `code-graph-rag`。
- `.kittify/agent_profiles_manifest.json` 每一筆的 `source_path` 都是 `/home/madao/.local/share/uv/tools/spec-kitty-cli/lib/python3.13/site-packages/doctrine/agent_profiles/built-in/*.agent.yaml`，`output_path` 都是 `/mnt/wsl/dev_shared/project/apm-go/.claude/agents/*.md`。這兩個路徑在本機都不存在 → 專案是在**另一個路徑／機器**用 `uv tool install spec-kitty-cli` 初始化的，manifest 內的絕對路徑在這台機器上已失效（見 §3.2）。
- `.kittify/metadata.yaml`（[local] L6-L8）：`schema_version: 3`、`version: 3.2.5`、`initialized_at: '2026-08-25T10:08:24'`；`environment.python_version: 3.13.15`。

### 1.2 官方安裝方式（[docs@v3.2.5] `docs/guides/install-and-upgrade.md` §"Install the CLI"；[README@main] §Quick Start）

```bash
pipx install spec-kitty-cli        # 官方首選："pipx is the preferred installer for the CLI"
uv tool install spec-kitty-cli     # 其他支援方式
python -m pip install spec-kitty-cli   # 僅在已啟用的 venv 內
```

文件明言「Do not use `--break-system-packages` as a normal Spec Kitty install path.」

### 1.3 ⚠ 3.2.5 與最新 typer 不相容（[wheel] 實測）

`spec_kitty_cli-3.2.5.dist-info/METADATA` 宣告 `Requires-Dist: typer>=0.24.1`（無上限）。以 `uvx --from spec-kitty-cli==3.2.5 spec-kitty --help` 解析到 typer 0.27.2 時，**import 就崩潰**：

```
File ".../specify_cli/orchestrator_api/commands.py", line 79, in <module>
    _CLICK_ABORTS: tuple[type, ...] = (click.Abort, _typer_click_module.exceptions.Abort)
AttributeError: module 'typer._click.exceptions' has no attribute 'Abort'
```

改用 `--with typer==0.24.2` 後一切正常，`spec-kitty --version` 印出 `spec-kitty-cli version 3.2.5`。typer 0.25.x / 0.26.x 是否可用：**未驗證**。

因此在這台機器的建議安裝指令（建議，非文件原文）：

```bash
uv tool install 'spec-kitty-cli==3.2.5' --with 'typer==0.24.2'
# 或 pipx install 'spec-kitty-cli==3.2.5' && pipx inject spec-kitty-cli 'typer==0.24.2'
spec-kitty --version          # 期望：spec-kitty-cli version 3.2.5
```

`uv tool install` 會把 shim 放到 `~/.local/bin/spec-kitty`；本 shell 的 PATH 已含 `~/.local/bin`（`code-graph-rag` 的 `cgr` 就在那裡可用）。

### 1.4 ⚠ 首次執行會寫入 `$HOME`，且本機有一個會讓所有指令崩潰的權限問題（[wheel] + 本機實測）

`specify_cli/__init__.py` L105-L115 `main_callback`：除了 `next` 快速路徑外，**每次**執行都會先跑

```python
ensure_runtime()                 # 建立 ~/.kittify/（missions/、cache/version.lock …）
ensure_global_agent_skills()     # 建立 ~/.claude/skills/spk-*、~/.agents/skills、~/.qwen/skills、~/.kilocode/skills …
ensure_global_agent_commands()   # 建立 ~/.claude/commands/spec-kitty.*.md、~/.gemini/commands/*.toml …
```

註解寫明「FR-002: Ensure global runtime (~/.kittify/) is populated and current.」這是 ADR「Global Slash Command Installation」/「Global Skill Installation with Per-Project Symlinks」的設計（[docs@v3.2.5] `docs/adr/3.x/2026-04-08-3-global-skill-installation-per-project-symlinks.md`）。

本次研究在真實 `$HOME` 下跑過一次 `--help`，實際產生了：`~/.kittify/{cache,missions}`、`~/.claude/commands/spec-kitty.*.md`（15 個）、`~/.claude/skills/`（66 個 `spk-*` / `spec-kitty-*` 目錄）、`~/.qwen/skills`、`~/.kilocode/skills`、`~/.agents/skills`、`~/.gemini/commands/spec-kitty.analyze.toml`。這些都在 `$HOME`，**沒有碰到本 repo**。之後的 help 擷取全部改在 `HOME=<scratchpad>/fakehome` 下執行。

本機問題：`~/.gemini/commands/` 是 `root:root` 擁有（`drwxrwxrwx root root`），`agent_commands.py` L275 對寫完的檔案做 `chmod(... & ~0o222)` 時丟出 `PermissionError: [Errno 1] Operation not permitted: '/home/madao/.gemini/commands/spec-kitty.analyze.toml'`，`ensure_global_agent_commands()` 直接 re-raise（L364-L366 `logger.warning("Command sync failed; version lock not updated"); raise`），所以**在這台機器上任何 `spec-kitty <子指令>` 都會在進入子指令前崩潰**，直到修好 `~/.gemini` 的擁有者為止（建議：`sudo chown -R madao:madao ~/.gemini`，或刪掉該目錄讓它重建）。這與本 repo 無關，是 host 環境問題。

相關環境變數（[wheel] `specify_cli/runtime/home.py` L30-L33、`specify_cli/__init__.py` L146、L36）：`SPEC_KITTY_HOME`（改寫全域狀態根目錄；[docs@v3.2.5] CHANGELOG 3.2.x 條目「`SPEC_KITTY_HOME` now isolates *all* local Spec Kitty state」）、`SPEC_KITTY_NO_UPGRADE_CHECK=1`（關掉升級提示）、`SPEC_KITTY_TEST_MODE`。`~/.claude/commands` 等 agent 目錄是用 `Path.home()` 算的（`agent_commands.py` L70），`SPEC_KITTY_HOME` 是否也改寫它們：**未驗證**。

### 1.5 驗證版本是否與專案相符

- CLI：`spec-kitty --version` → `spec-kitty-cli version 3.2.5`（[help]）。
- 專案：`.kittify/metadata.yaml` `spec_kitty.version: 3.2.5` / `schema_version: 3`（[local]）。
- 對照：`spec-kitty upgrade --dry-run --json | jq '.pending_migrations'` 為空陣列即代表不需專案遷移（[docs@v3.2.5] `docs/api/upgrade-lifecycle.md` §"When is a project upgrade required?"）。
- 若不符，`main_callback` 的 `check_schema_version` 會擋下大部分指令：專案舊於 CLI → exit 4；專案新於 CLI → exit 5（`--yes` 無法繞過）；metadata 損毀 → exit 6（[help] `spec-kitty upgrade --help` "Exit codes (R-08)"）。`--help`、`--version`、`upgrade --dry-run`、`upgrade --cli` 永遠可用。

---

## 2. 完整任務（mission）生命週期

Spec Kitty 3.2.5 的核心迴圈（[README@main] 第一段）：

```text
spec -> plan -> tasks -> next -> review -> accept -> merge
```

所有任務產物放在 **`kitty-specs/<mission-slug>/`**（不是 `specs/`）。`spec-kitty specify --help` 的說明就是「Create a feature scaffold in kitty-specs/.」（[help]）。目前本 repo 還沒有 `kitty-specs/`、`kitty-ops/`、`.worktrees/` 目錄（[local]）。

### 2.1 slash command 與 CLI 的關係（[wheel] + [help]）

- Claude Code 的 slash command 檔案是**全域**的 `~/.claude/commands/spec-kitty.*.md`（[docs@v3.2.5] `docs/api/supported-agents.md` L15、L25："Claude Code | `~/.claude/` | `commands/` | `/spec-kitty.*`"），由 CLI 啟動時同步（§1.4），專案內 `.claude/commands/` 不存在（[local]）。
- 產生的 15 個指令：`spec-kitty.{accept,analyze,charter,dashboard,implement,merge,plan,research,review,specify,status,tasks,tasks-finalize,tasks-outline,tasks-packages}.md`（本機 `~/.claude/commands` 實際列表；每個檔頭都有 `<!-- spec-kitty-command-version: 3.2.5 -->`）。
- 大部分 slash command 是「thin shim」：例如 `spec-kitty.implement.md` 全文只有「Run this exact command and treat its output as authoritative. … `spec-kitty agent action implement $ARGUMENTS --agent claude`」；`spec-kitty.merge.md` 則是 `spec-kitty merge $ARGUMENTS`。`metadata.yaml` 的 `schema_capabilities.thin_shims: true`（[local] L13）指的就是這件事。
- `spec-kitty.specify.md` 不是 thin shim，是完整的訪談流程 prompt，並且開頭要求 agent 每個 session 先跑一次 `spec-kitty upgrade --agent-check --json`。
- `.kittify/missions/software-dev/` 的範本與 action prompt 在 wheel 內的 `doctrine/missions/software-dev/{templates/spec-template.md,plan-template.md,tasks-template.md,task-prompt-template.md, actions/{specify,plan,tasks,implement,review,retrospect}/}`（[wheel]）；全域副本在 `~/.kittify/missions/`。

### 2.2 各階段（[docs@v3.2.5] `docs/api/slash-commands.md`，除非另註）

| 階段 | slash command | 底層 CLI | 產生檔案 | agent 要做的事 |
|---|---|---|---|---|
| **Specify** | `/spec-kitty.specify [description]` | `spec-kitty agent mission create <slug> --json`（[help]：「Creates mission directory in kitty-specs/ and commits to the current branch」）；直接 CLI `spec-kitty specify MISSION [--mission-type software-dev\|research] [--topology single_branch\|lanes\|coord\|lanes_with_coord]` 只建骨架 | `kitty-specs/<feature>/spec.md`、`meta.json`、`checklists/requirements.md` | 在 repo root checkout 執行；做 discovery interview、確認 intent summary、選 mission type，再呼叫 `mission create`。「NO worktrees are created」（`~/.claude/commands/spec-kitty.specify.md`）。 |
| **Plan** | `/spec-kitty.plan [notes]` | `spec-kitty agent mission setup-plan`；直接 CLI `spec-kitty plan --mission <slug>` 只 scaffold `plan.md` | `plan.md`、`research.md`、`data-model.md`、`contracts/`、`quickstart.md`，並更新 agent context 檔（如 `CLAUDE.md`） | 規劃訪談；寫 Implementation Concern Map（IC-##）（[docs@v3.2.5] `docs/api/file-structure.md` "Feature Directory Contents"） |
| **Tasks** | `/spec-kitty.tasks [notes]`（3.2.5 另有 `tasks-outline` / `tasks-packages` / `tasks-finalize` 三段式指令，[local] `~/.claude/commands`） | `spec-kitty agent mission finalize-tasks`；直接 CLI `spec-kitty tasks --mission <slug>`「Finalize tasks metadata after task generation」 | `tasks.md`、`wps.yaml`、`tasks/WPxx-*.md`（平鋪目錄）、`lanes.json` | 讀 spec/plan，切成 work package（WP），每個 WP 一個 prompt 檔；`finalize-tasks` 依「File ownership overlap」與「Explicit dependencies」把 WP 分配到 execution lane（[docs@v3.2.5] `docs/architecture/execution-lanes.md` §Parallelism Preservation） |
| **Implement** | `/spec-kitty.implement [WP_ID]` | Step 1 `spec-kitty agent action implement WP## --agent claude`（顯示 prompt、把 WP 移到 `in_progress`）；Step 2 `spec-kitty implement WP##`（建立或重用 lane worktree；[help] 標為「Internal」相容介面） | `.worktrees/<slug>-<mid8>-lane-<id>/`、`.kittify/workspaces/*`、WP 檔案的 `lane:` 更新 | 在印出的 workspace 路徑內實作；只碰 WP `owned_files`；完成後 `spec-kitty agent tasks move-task WP## --to for_review --agent claude` |
| **Review** | `/spec-kitty.review [WP_ID]` | `spec-kitty agent action review WP##` + `move-task --to approved` 或 `--to planned --review-feedback-file <md>` | WP 檔案的 `review_feedback: feedback://...`、`tasks.md` 勾選 | 結構化審查；通過 → `approved`（merge pending），退回 → `planned` |
| **Accept** | `/spec-kitty.accept` | `spec-kitty accept [--mission] [--mode auto\|pr\|local\|checklist] [--test <cmd>]...`（[help]） | acceptance-matrix 等 | 所有 WP 應為 `approved`/`done`；`--test` 可重複，紀錄實際跑過的驗證指令 |
| **Merge** | `/spec-kitty.merge [--push]` | `spec-kitty merge [--strategy merge\|squash\|rebase（預設 squash）] [--delete-branch/--keep-branch] [--remove-worktree/--keep-worktree] [--push] [--target] [--dry-run --json] [--resume/--abort]`（[help]） | 合併到 target branch、刪 lane branch 與 worktree | 之後跑 `/spec-kitty-mission-review`、`spec-kitty retrospect summary` |
| **Next（runtime loop）** | — | `spec-kitty next --agent claude --mission <slug> [--result success\|failed\|blocked] [--json]`（[help]：「Agents call this command repeatedly in a loop. The system inspects the mission state machine, evaluates guards, and returns a deterministic decision with an action and prompt file.」） | — | README 建議 tasks 之後用 `next` 讓 runtime 決定下一步，取代手動 `/spec-kitty.implement`/`/spec-kitty.review` |

注意：`spec-kitty review`（頂層 CLI）在 3.2.5 的語意是「Validate a merged mission: WP lane check, dead-code scan, BLE001 audit. Writes kitty-specs/<slug>/mission-review-report.md」（[help]），是**merge 後**的 mission-level 審查；WP-level 審查走 `agent action review` / `/spec-kitty.review`。`--check-residual` 會去讀 `.github/workflows/ci-quality.yml` 的 `-m` 表達式——那是 spec-kitty 自己 repo 的 CI 檔名，對 apm-go 不適用（未驗證它在找不到該檔時的行為）。

WP 的 lane 值域（[docs@v3.2.5] `docs/api/configuration.md` "Work Package Frontmatter"）：`planned`, `claimed`, `in_progress`（別名 `doing`）, `for_review`, `in_review`, `approved`, `done`, `blocked`, `canceled`。「`approved` means review passed and merge pending; `done` means merged/integrated.」

### 2.3 mission review 與 retrospective（[README@main] §Quick Start；[docs@v3.2.5] slash-commands §/spec-kitty.merge）

merge 後的標準順序：`/spec-kitty-mission-review` → 確認 `.kittify/missions/<mission_id>/retrospective.yaml` 存在（若無：`spec-kitty retrospect create --mission <slug>`）→ `spec-kitty retrospect summary`（唯讀）→ `spec-kitty agent retrospect synthesize --mission <slug> [--apply]`。README 表格：「Every completed mission generates a retrospective by default. Tune via `.kittify/config.yaml#retrospective` or charter」。

---

## 3. 輕量派工（dispatch）與 Op

### 3.1 指令（[wheel] `specify_cli/cli/commands/dispatch.py` L1-L6、L196-L216；[help]）

模組 docstring：「This is the single public standalone governance surface. It routes the request, loads governance context, opens an Op record, and returns synchronously. It never spawns a separate LLM call.」

```
spec-kitty dispatch "<request verbatim>" [--profile <profile-id>] [--json]
```

- 無 `--profile` 時由 `ActionRouter` 依請求文字選 profile；路由模糊 → stderr 輸出 `{"error":"routing_failed","error_code":...,"candidates":[...]}` 並 exit 1；找不到 profile → `PROFILE_NOT_FOUND`，建議「Run 'spec-kitty profiles list' to see available profiles.」（L136-L170）。
- `_detect_actor()`（L52-L58）：有 `CLAUDE_CODE_ENTRYPOINT` 環境變數 → actor `claude`；`CODEX_CLI` → `codex`；否則 `operator`。
- 成功時印 Profile / Action / Router confidence / Invocation ID / Governance Context 面板；若治理內容不可用則提示「Governance context unavailable. Run 'spec-kitty charter synthesize'.」（L98-L101）。
- Op 保持 OPEN；結尾印關閉指令（L120-L127）：

```
spec-kitty profile-invocation complete --invocation-id <id> --outcome <done|failed|abandoned> [--evidence <file>] [--artifact <path>] [--commit <sha>]
Unclosed Ops are reported by `spec-kitty doctor ops` and swept to 'abandoned' when stale.
```

`profile-invocation complete` 的 `--invocation-id`/`--outcome` 必填，`--artifact` 可重複，`--commit` 單一，`--evidence` 只接受 execution record（[help]）。`spec-kitty doctor ops --close-stale --threshold <hours>`（預設 24h）會把逾期 Op 以 `closed_by=doctor_sweep` 關成 abandoned；`spec-kitty invocations list [--profile] [--limit 20] [--json]` 列出本地紀錄（[help]）。

Op 紀錄的落地位置（[wheel] `specify_cli/invocation/writer.py` L16-L17、`record.py` L149、`lifecycle.py` L44）：`kitty-ops/<invocation_id>.jsonl`、`kitty-ops/lifecycle.jsonl`、索引 `kitty-ops/ops-index.jsonl`。本 repo `.gitignore` 只忽略 `kitty-ops/ops-index.jsonl`（[local] L91；CHANGELOG 3.2.x「The Op-index performance cache is now gitignored (#2341)」），**其餘 Op 紀錄是要進版控的**。

### 3.2 Profiles（[local] `.kittify/agent_profiles_manifest.json`）

manifest 的用途（[wheel] `specify_cli/tool_surface/profiles/manifest.py` docstring）：「records every native agent profile file this tool has written, keyed by output path, together with its SHA-256 content hash and the source profile URN / tool / format … the manifest is the *state* record (what was installed) separate from the projection *policy* (what should exist).」

本 repo 的 17 個 profile（全部 `source_layer: builtin`、`format: claude-agent`、`tool_key: claude`，投影到 `.claude/agents/<id>.md`）：

`architect-alphonso`, `curator-carla`, `debugger-debbie`, `designer-dagmar`, `doctrine-daphne`, `frontend-freddy`, `generic-agent`, `implementer-ivan`, `java-jenny`, `node-norris`, `paula-patterns`, `planner-priti`, `python-pedro`, `randy-reducer`, `researcher-robbie`, `retrospective-facilitator`, `reviewer-renata`

用法：`spec-kitty dispatch "..." --profile reviewer-renata`；`spec-kitty profiles list [--all|--show-available]`、`spec-kitty profiles show <id> [--all]`（[help]）。沒有 Go 專屬 profile；`generic-agent` / `implementer-ivan` / `reviewer-renata` / `debugger-debbie` 是對 Go 專案有意義的幾個（判斷，非來源）。

manifest 的 `output_path` 是另一台機器的絕對路徑（§1.1），且 `.claude/agents/` 在本機不存在（`.gitignore` L98 `.claude/*` 只保留 `CLAUDE.md`、`rules/`、`settings.json`）。修復方式應是 `spec-kitty doctor tool-surfaces [--tool claude] --fix`（[help]：「Audit (and optionally repair) every configured tool surface」）或 `spec-kitty agent config sync --create-missing`；實際效果**未驗證**（本研究不執行寫入指令）。

### 3.3 Claude Code hooks（[local] `.claude/settings.json`；[wheel]）

```json
"SessionStart": [{"hooks":[{"type":"command","command":"spec-kitty session-start"}]}],
"Stop":         [{"hooks":[{"type":"command","command":"spec-kitty session-stop"}]}]
```

- `session-start`（[wheel] `specify_cli/cli/commands/session_start.py` docstring）：往上找 `.kittify/` 目錄，找到就印 orientation block（`SessionPresenceManager._build_content()`）與 open Ops 清單；「Exit 0 guarantee … All exceptions are swallowed」；`<200ms`，升級檢查在背景子程序跑。
- `session-stop`（`session_stop.py`）：只掃描 Ops 目錄，列出未關閉的 Op 與關閉指令；不跑 git。
- 目前 `spec-kitty` 不在 PATH，兩個 hook 都會靜默失敗（Claude Code 對 hook 非零退出的處理不在本研究範圍）。

### 3.4 `.claude/CLAUDE.md` orientation block（[local] 全文引用）

```
<!-- spec-kitty:orientation -->
**Spec Kitty v3.2.5** — project: unknown (healthy)

Two usage patterns:
- **Full mission** (spec → plan → tasks → implement → review → merge):
  trigger: "spec out", "create a mission", "write a spec", "plan this"
  → run `/spec-kitty.specify`
- **Lightweight dispatch** (ad-hoc fix, question, or advice — no mission created):
  trigger: "hey spec kitty", "use spec kitty to", "spec kitty <anything>"
  → **ALWAYS run `spec-kitty dispatch "<request verbatim>"` — do NOT answer directly.**
  If you know the right profile, pass it to skip routing:
  `spec-kitty dispatch "<request verbatim>" --profile <profile-id>`
  Reason: `spec-kitty dispatch` loads governance context, routes the request,
  and opens the Op. Skipping it produces ungoverned, untracked responses.
  After finishing the work, close the Op with the command printed in the capsule
  (`spec-kitty profile-invocation complete --invocation-id <id> --outcome <done|failed|abandoned>`).
<!-- /spec-kitty:orientation -->
```

這個區塊由 `specify_cli/session_presence/writers/markdown_rules.py` 管理（標記 `SECTION_OPEN/CLOSE` 定義在 `session_presence/content.py` L12-L13），升級時由 migration `m_3_2_0rc39_refresh_orientation_block.py` 重寫；「project: unknown」代表 init 當時尚無 charter/專案名稱（推測，未驗證）。

---

## 4. 治理／Charter

### 4.1 檔案與生成鏈（[wheel] `charter/sync.py` L1-L8、L40-L50；`charter/compiler.py` L195-L219；`charter/bundle.py` L13；[help] `charter --help`）

```
.kittify/charter/interview/answers.yaml   ← spec-kitty charter interview（[docs@v3.2.5] charter-commands.md）
.kittify/charter/charter.md               ← spec-kitty charter generate（人可讀的專案原則；唯一手寫/可編輯來源）
.kittify/charter/references.yaml          ← generate 時 compiler 寫出（compiler.py L217-L219）
.kittify/charter/governance.yaml          ┐
.kittify/charter/directives.yaml          ├ spec-kitty charter sync 從 charter.md 派生（sync.py：「Write governance/directives/metadata files」）
.kittify/charter/metadata.yaml            ┘ （含 charter.md hash 與 timestamp，用來判斷 staleness）
.kittify/charter/context-state.json       ← runtime 狀態（bundle.py L13「runtime state written by …」；context.py L598）
.kittify/charter/generated/               ← LLM harness 產生的 doctrine YAML（charter synthesize 的輸入）
.kittify/doctrine/                        ← charter synthesize 的輸出（專案層 doctrine；含 PROVENANCE.md）
```

`spec-kitty charter --help` 子指令：`activate`, `deactivate`, `interview`, `generate`, `context`, `sync`, `status`, `synthesize`, `resynthesize`, `lint`, `preflight`, `bundle`, `mission-type`, `list`, `pack`（[help]）。

`charter generate` 的行為契約（[help]）：在 git working tree 內成功時會 `git add` 產生的 `charter.md`（staging，不 commit）；不在 git repo 內會 exit 非零；`charter.md` 是 symlink 時拒絕執行。

「constitution」在 3.x 已改名：migration `m_3_1_1_charter_rename.py` 把 `.kittify/memory/constitution.md` 搬到 `charter/charter.md`，並移除 `spec-kitty.constitution.md` 指令（[wheel] L55-L156）。`charter/schemas.py` L120 仍提到 `spec/constitution.md` 作為「supporting references only」。

### 4.2 為什麼 charter 的部分檔案被 gitignore（[local] `.gitignore` L74-L78）

被忽略：`context-state.json`、`directives.yaml`、`governance.yaml`、`metadata.yaml`、`references.yaml`。**未**被忽略：`charter.md`、`interview/`、`generated/`、`.kittify/doctrine/`。

這與 §4.1 的生成鏈一致：`charter.md` 是來源，其餘四個 YAML 是 `charter sync`/`generate` 的派生物，`context-state.json` 是 runtime 狀態。`charter/sync.py` 的 `ensure_charter_bundle_fresh` / `post_save_hook` 會在 staleness 檢查後自動重生成（L307-L337：找不到 `governance.yaml` 時「Using empty governance config」並 warning）。這個 gitignore 區塊是 `spec-kitty init` 寫的（`.gitignore` L55 marker「# Added by Spec Kitty CLI (auto-managed)」，對應 `specify_cli/gitignore_manager.py` L106）。

**結論（判斷）**：維持現狀是對的——只要 `charter.md` 進版控，其他人 clone 後第一次跑任何治理指令就會重建 YAML。不建議手動把它們從 ignore 移除，否則 `spec-kitty upgrade` 會再加回來（`ensure_entries` 是冪等的 append，gitignore_manager.py L109）。

### 4.3 `canonical-events.jsonl` 與 `memory/`（[local] + [wheel]）

- `.kittify/canonical-events.jsonl` 目前只有一行 `ProjectInitialized` 事件（`event_type`、`project_uuid: 48077b8a-…`、`runtime_version: 3.2.5`、`schema_version: "5.0.0"`）。`specify_cli/status/lifecycle_events.py` L12 稱之為「Project-level log (`<repo_root>/.kittify/canonical-events.jsonl`)」；mission 層另有 `kitty-specs/**/status.events.jsonl`、`mission-events.jsonl`（本 repo `.gitattributes` 已設 `kitty-specs/**/status.events.jsonl merge=spec-kitty-event-log`，對應隱藏指令 `spec-kitty merge-driver-event-log`，[wheel] `cli/commands/__init__.py` L222）。`metadata.yaml` 的 `schema_capabilities.event_log_authority: true` 表示事件日誌是狀態的權威來源。
- `.kittify/memory/`：`init.py` L157 註解「.kittify/memory/ (project-local memory/context files)」；目前只有 `templates/POWERSHELL_SYNTAX.md`（`specify_cli/template/manager.py` L46 從 `doctrine/toolguides/POWERSHELL_SYNTAX.md` 複製；供 Windows agent 用，對本 WSL 專案無作用）。

### 4.4 其他被 gitignore 的 `.kittify/*`（[local] L73-L90）

`.dashboard`（dashboard daemon 元資料）、`derived/`、`dossiers/`、`encoding-provenance/`、`events/`、`logs/`、`merge-state.json`（merge 中斷復原狀態，[wheel] `merge.py` docstring「MergeState is created at merge start」）、`migrations/`、`runtime/`、`skills-manifest.json`、`sync-state.json`、`workspaces/`。CHANGELOG 3.2.x：「`.kittify/migrations/` and `.kittify/logs/` are now gitignored」。各目錄的精確內容除 merge-state/workspaces 外**未逐一驗證**。

---

## 5. Worktree／分支模型與 `auto_commit`

### 5.1 拓撲（[wheel] `merge.py` L1-L17；[docs@v3.2.5] `execution-lanes.md`、`git-worktrees.md`）

- 「Lane worktrees are the only supported execution topology.」（merge.py L3）
- Worktree 位置是 **`.worktrees/<human-slug>-<mid8>-lane-<id>/`**，不是 `.kittify/workspaces/`。`.kittify/workspaces/` 放的是「persistent workspace context files … Created during `spec-kitty implement` … Cleaned up during merge」（[wheel] `specify_cli/workspace/context.py` L1-L14），也就是 worktree 的**索引/上下文**，本 repo 已 gitignore（L90）並在 `.gitattributes` 標 `-diff`。
- 分支命名（execution-lanes.md §Naming）：mission branch `kitty/mission-<slug>-<mid8>`、lane branch `kitty/mission-<slug>-<mid8>-lane-a`；`mid8` 是 mission ULID 前 8 碼。
- 3.2.5 新增 `--topology`（[help] `specify`、`agent mission create`）：`single_branch | lanes | coord | lanes_with_coord`，預設 `coord`；「Coordination-bearing shapes (coord, lanes_with_coord) mint a coordination branch; branch-flat shapes (single_branch, lanes) do not.」CHANGELOG 3.2.5：coord branch 放 lifecycle（status/notes/trace），primary branch 放 stable planning（spec/plan/WP outlines）。
- planning 階段（specify/plan/tasks）在 repo root checkout；只有 `code_change` WP 才開 worktree，`planning_artifact` WP 直接在 repo root（`core/worktree.py` L9-L11）。

### 5.2 Merge 流程（[wheel] merge.py L3-L7；[help]）

「1. Merge each lane branch into the mission branch. 2. Merge the mission branch into the target branch.」預設 `--strategy squash`、`--delete-branch`、`--remove-worktree`；`--push` 才推遠端；中斷可 `--resume`/`--abort`；`--dry-run --json` 可預覽。

### 5.3 `auto_commit: true` 的意義（[wheel] `specify_cli/core/agent_config.py` L28-L43；[local] `config.yaml` L11）

```python
auto_commit: Whether agents should auto-commit status changes.
    When False, agents may stage changes but MUST NOT create
    commits unless explicitly instructed. Per-command flags
    (--auto-commit/--no-auto-commit) override this setting.
```

它控制的是 **spec-kitty 自己做的狀態/規劃 commit**（`agent mission create` 建立 mission 後 commit、`move-task` 的「Automatically commit WP file changes to target branch (default: from project config)」、`implement` 的「Auto-commit status and planning changes」），不是實作程式碼的 commit。CHANGELOG 3.2.x 亦提到「one canonical per-checkout auto-commit seam」。安全 commit 路徑：`spec-kitty safe-commit <files> -m <msg> [--to-branch <branch>]`（[help]：`--to-branch` 在 v3.3 會變必填）與 `spec-kitty spec-commit <files> -m <msg>`（把 spec 產物提交到 mission 的 coordination/primary 位置）。

與本 repo 規範的互動（判斷）：AGENTS.md/git-workflow 規則要求「Commit or push only when the user asks」；`auto_commit: true` 會讓 spec-kitty 在 specify/move-task 時自動產生 `kitty-specs/` 的 commit（`agent mission create` 的說明明寫「commits to the current branch」）。要避免，可 `spec-kitty agent config set auto_commit false`，或逐指令加 `--no-auto-commit`。

---

## 6. 多 agent 設定

### 6.1 `agents.available: [claude]`（[wheel] `agent_config.py` L96-L109、L149-L161；[help] `agent config`）

`get_configured_agents()` 註解：「This is the DEFINITIVE list of available agents, set during init.」用途：

- 決定哪些 harness 的 hook/目錄要同步：`_sync_harness_hooks`「a harness is synced only when its agent key is in `config.available`」（`agent/config.py` L639-L655）。
- `spec-kitty agent config sync` 預設「removes orphaned directories (present but not configured)」（[help]）——**注意**：本 repo 的 `.claude/` 有團隊自有的 `CLAUDE.md`、`rules/`、`settings.json`；只要 `claude` 留在 `available` 就不會被當 orphan。
- 合法 key（`core/config.py` L5-L23 `AI_CHOICES`）：`copilot, claude, gemini, cursor, qwen, opencode, codex, windsurf, kilocode, auggie, q, kiro, antigravity, vibe, pi, letta`（`roo` 已移除）。

### 6.2 加入 codex（[help] `agent config add`；[docs@v3.2.5] `docs/guides/harnesses/codex.md`、`supported-agents.md` L42、L204-L209）

```bash
spec-kitty agent config add codex      # 更新 config.yaml 並建立目錄
```

Codex 不用 slash command，而是**專案內** Agent Skills：`.agents/skills/spec-kitty.<command>/SKILL.md`，manifest 在 `.kittify/command-skills-manifest.json`；在 Codex 內用 `$spec-kitty.specify "..."`（`$` 前綴）。`spec-kitty agent config sync` 可重生成；`spec-kitty doctor skills` 檢查 manifest drift。本 repo `.gitignore` L72 已忽略 `.agents/skills/`（init 預設），代表 Codex 的 skill 檔在每台機器由 CLI 重建。`dispatch` 會靠 `CODEX_CLI` 環境變數把 actor 記成 `codex`（§3.1）。

### 6.3 `lint_on_edit`（[wheel] `agent/config.py` L33、L658-L682；[help] `lint`、`agent config set`）

`false`（本 repo 現值）。設 `true` 並跑 `spec-kitty agent config sync --sync-hooks` 後，會在 `.claude/settings.json` 註冊一個 `PostToolUse` hook 執行 `spec-kitty lint`（`ClaudeCodeHookRegistrar`，「preserves all unrelated keys/hooks」）。但 `spec-kitty lint` 是「Run ruff and mypy on a file」（[help]）——**Python 專用**，對 Go 專案沒有用。建議保持 `false`。

---

## 7. 升級與「DO NOT EDIT MANUALLY」

### 7.1 `spec-kitty upgrade`（[help]；[docs@v3.2.5] `docs/api/upgrade-lifecycle.md`）

兩種升級：CLI（`pipx upgrade spec-kitty-cli` / `uv tool upgrade spec-kitty-cli`）與專案（`spec-kitty upgrade [--project]`）。「The CLI never silently upgrades a project. Projects never silently upgrade the CLI.」

專案升級會：讀 `config.yaml` 只處理已設定的 agent、刷新 agent command 目錄、更新 `.kittify/` scaffold、把 `.kittify/metadata.yaml` 的 schema 版本往上推；**不**動 git 狀態與 `kitty-specs/`。建議流程：`spec-kitty upgrade --dry-run --json | jq .pending_migrations` → `spec-kitty upgrade --yes` → `git diff .kittify/ .claude/ .agents/skills/` → commit。

slash command 檔內建的 `spec-kitty upgrade --agent-check --json` / `--agent-choice` 流程（§2.1）在 `upgrade --help` 的 Options 清單裡**沒有列出**（只有 `--dry-run --force --target --json --verbose --no-worktrees --cli --project --yes --no-nag`），是否為隱藏參數：**未驗證**。

### 7.2 「DO NOT EDIT MANUALLY」檔案（[wheel] `specify_cli/upgrade/metadata.py` L214、`upgrade/runner.py` L547；[local]）

該標頭只出現在 `.kittify/metadata.yaml`（「# Auto-generated by spec-kitty init/upgrade # DO NOT EDIT MANUALLY」）。它是 schema gate 與 migration 帳本（`migrations.applied`），手改會導致 exit 6（metadata corrupt）或跳過遷移。其他「機器擁有」但沒有此標頭的檔案：`agent_profiles_manifest.json`（state record，§3.2）、`canonical-events.jsonl`（append-only 事件）、`.gitignore` 的 auto-managed 區塊、`~/.claude/commands/spec-kitty.*.md`（CLI 每次啟動會依 `~/.kittify/cache/version.lock` 重新同步並 chmod 成唯讀，`agent_commands.py` L272-L275）。可手編的：`.kittify/config.yaml`（但官方途徑是 `spec-kitty agent config set`）、`.kittify/charter/charter.md`。

---

## 8. 對 apm-go 的建議日常工作流（**以下為建議，非來源事實**）

前提事實：apm-go 是 Go CLI，`go test ./...` 不是 parity gate，`.github/workflows/parity.yml` + `tools/parity` 才是（AGENTS.md）。spec-kitty 的 `lint`、`review --check-residual` 都是為 Python/它自己的 repo 設計的，不能直接套用。

1. **一次性環境修復**：修 `~/.gemini` 擁有者（§1.4）→ `uv tool install 'spec-kitty-cli==3.2.5' --with 'typer==0.24.2'` → `spec-kitty --version` → 在 repo root `spec-kitty upgrade --dry-run`（預期無 pending）→ `spec-kitty doctor tool-surfaces --json` 看 profile 投影是否需要 `--fix`（會建立被 gitignore 的 `.claude/agents/*.md`，不影響版控）。
2. **先做 charter**：跑 `/spec-kitty.charter`（或 `spec-kitty charter interview --profile minimal` → `charter generate`），把 AGENTS.md 的硬規則寫進 `charter.md`：oracle parity 原則、`yamlcore` round-trip、`gitops.ApplySecureGitEnv`、exit code 慣例、「parity gate 才是驗收」。之後 `dispatch` 與各 action 的 Governance Context 才不會是空的。只提交 `charter.md`。
3. **決定 `auto_commit`**：因 AGENTS.md 要求只在使用者要求時 commit，建議 `spec-kitty agent config set auto_commit false`，改在 mission 各階段結束時用 `spec-kitty spec-commit kitty-specs/<slug>/... -m "..."` 明確提交規劃產物（commit message 走 git-workflow.md 的格式）。
4. **Mission 用在跨檔案的功能／parity ticket**（例如新的 pack 旗標、新 deploy adapter）：`/spec-kitty.specify` → `/spec-kitty.plan` → `/spec-kitty.tasks`；在 tasks 階段把 WP 的 `owned_files` 對齊 `cmd/apm-go/*.go` / `internal/<pkg>/` 的邊界，讓 lane 計算把會互踩的 WP 收進同一 lane。
5. **實作迴圈**：`spec-kitty next --agent claude --mission <slug> --json` 驅動；每個 WP 在 `.worktrees/...` 內做，並在 WP prompt 的 Definition of Done 加上 `go build ./... && go test ./...` 與對應的 `tools/parity` case；`accept` 時用 `--test "go test ./..." --test "go run ./tools/parity ..."` 記錄實際跑過的驗證。GitNexus 的 `impact`/`detect_changes` 規則照舊在 worktree 內執行（`.gitnexus` 索引是主 checkout 的，worktree 內 HEAD 不同會被判 stale——**未驗證**）。
6. **Merge**：`spec-kitty merge --dry-run --json` 先看，再 `spec-kitty merge`（預設 squash、刪 lane branch/worktree、不 push）；push 與 PR 仍由使用者決定，用現有 `git-assistant:pull-request` skill 開 PR，讓 parity CI 跑。之後 `/spec-kitty-mission-review`、`spec-kitty retrospect summary`。
7. **小修／問題**用 dispatch：「hey spec kitty …」→ `spec-kitty dispatch "<原文>" [--profile debugger-debbie|reviewer-renata|generic-agent]`，做完 `profile-invocation complete --outcome done --commit <sha>`；`kitty-ops/*.jsonl` 會進版控，建議與該次工作一起 commit。`session-stop` hook 會提醒未關閉的 Op。
8. **不要用**：`lint_on_edit`（ruff/mypy）、`review --check-residual`、dashboard 以外的 SaaS/tracker/sync 功能（README：「Hosted tracker and sync integrations are optional」）。
9. **待驗證再決定**：`SPEC_KITTY_HOME` 是否能把 `~/.claude/commands` 的寫入也隔離；`spec-kitty review`（mission-level）對 Go repo 的 dead-code scan 是否有意義；`--topology single_branch` 是否比預設 `coord` 更適合單人＋CI 的 repo（3.2.5 CHANGELOG 說 no-coordination 拓撲下「all primary」，會少一條 coordination branch）。

---

## 9. 本次研究未驗證／未做的事

- 沒有執行 `spec-kitty init/upgrade/specify/dispatch/doctor --fix` 等任何寫入 repo 的指令。
- typer 0.25/0.26 相容性；`SPEC_KITTY_HOME` 對 agent command 目錄的影響；`upgrade --agent-check` 旗標是否存在；`doctor tool-surfaces --fix` 是否能修復 manifest 的跨機器絕對路徑；`review --check-residual` 在沒有 `ci-quality.yml` 時的行為；`.kittify/{derived,dossiers,encoding-provenance,events,runtime}` 的精確內容。
- `docs/` 中 `configuration.md`、`file-structure.md` 的 `.kittify/templates/`、`.kittify/scripts/` 描述屬於 0.11–2.x 的專案內範本模型；3.2.5 已改為全域 `~/.kittify/missions/` + wheel 內 `doctrine/missions/`（本機 `~/.kittify/missions/{software-dev,plan,documentation,research}` 實際存在；`.claudeignore` L5-L7 仍列出 `.kittify/templates/`、`.kittify/missions/`、`.kittify/scripts/` 是為了相容）。
