# Research: Phase 4-T Target Deploy Matrix -- Gap Analysis

- **Query**: What is MISSING from the current apm-go Phase 4 implementation to satisfy the 4-T target deploy matrix in the acceptance checklist?
- **Scope**: internal (codebase + conformance-kit oracle)
- **Date**: 2026-06-30

## Measurement Baseline

Gaps are measured against:
- `D:\Projects\apm-dev\conformance-kit\acceptance-checklist.md` -- the authoritative acceptance checklist (Section "4-T")
- `D:\Projects\apm-dev\conformance-kit\oracle\targets\expected\*.yaml` -- per-target oracle golden files
- `D:\Projects\apm-dev\conformance-kit\acceptance-coverage.yml` -- machine-readable coverage matrix

## Existing Test Inventory

### `internal/deploy/deploy_test.go` -- all test functions

| Test Name | What It Covers | Oracle Verified |
|---|---|---|
| `TestResolveTargets_FlagOverrides` | --target flag overrides detection | No oracle |
| `TestResolveTargets_ManifestTargets` | manifest `targets:` field | No oracle |
| `TestResolveTargets_AutoDetect` | `.claude/` dir triggers claude | No oracle |
| `TestResolveTargets_NoSignal` | empty dir -> no targets | No oracle |
| `TestResolveTargets_AgentSkillsNotAutoDetected` | agent-skills never auto-detected (req-tg-001) | No oracle |
| `TestResolveTargets_AntigravityNotAutoDetected` | antigravity never auto-detected (req-tg-001) | No oracle |
| `TestResolveTargets_UnsupportedTargetDiag` | `--target gemini` emits diagnostic | No oracle |
| `TestDeployClaude_OracleMatch` | claude deployed_files match oracle | Hardcoded map |
| `TestDeployCopilot_OracleMatch` | copilot deployed_files match oracle | Hardcoded map |
| `TestDeployCodex_OracleMatch` | codex deployed_files match oracle | Hardcoded map |
| `TestDeployAntigravity_OracleMatch` | antigravity deployed_files match oracle | Hardcoded map |
| `TestDeployOpenCode_OracleMatch` | opencode deployed_files match oracle | Hardcoded map |
| `TestDeployAgentSkills_SkillsOnly` | agent-skills deploys skills only | Hardcoded map |
| `TestDeployRootConstraint` | req-tg-002: files under registered roots | All 6 adapters |
| `TestSkillConvergence` | req-tg-003: all targets -> `.agents/skills/` | All 6 adapters |
| `TestDeploySkill_BundleWithSiblings` | skill dir with siblings copied | No oracle |
| `TestRun_FullPipeline` | end-to-end local+dep deploy | claude only |
| `TestRun_ConflictResolution` | req-pr-002: local wins | claude only |
| `TestRun_SkillDeduplication` | multi-target skill dedup | claude+codex+copilot |
| `TestRun_NoTargets` | no targets -> nothing deployed | No oracle |
| `TestRun_DeployedFilesKeyMatch` | lockfile key alignment | claude only |

### `internal/deploy/primitive_test.go` -- primitive collection tests

| Test Name | What It Covers |
|---|---|
| `TestCollectLocalPrimitives` | .apm/ structure -> 4 primitive types |
| `TestCollectDependencyPrimitives` | dep .apm/ collection + source attribution |
| `TestCollectDependencyPrimitives_SkillBundle` | root SKILL.md detection |
| `TestCollectDependencyPrimitives_SkillCollection` | skills/<name>/SKILL.md collection |
| `TestExtractInstructionName` | filename parsing |
| `TestExtractAgentName` | filename parsing |

### `internal/deploy/conflict_test.go` -- conflict resolution tests

(Not read but exists at `internal/deploy/conflict_test.go`)

## Gap Analysis

---

### GAP 1: Antigravity Auto-Detection Discrepancy (CRITICAL)

**Checklist says**: antigravity DOES auto-detect via `GEMINI.md` or `AGENTS.md` signals (acceptance-checklist.md line 163: "research fix: DOES auto-detect, NOT explicit-only").

**Oracle says**: `detect: ["GEMINI.md", "AGENTS.md"]` (antigravity.yaml line 2).

**Code says**: antigravity is in `explicitOnlyTargets` map (adapter.go:68-71) and filtered out by `filterExplicitOnly()` (adapter.go:78-86).

**Test says**: `TestResolveTargets_AntigravityNotAutoDetected` (deploy_test.go:67-81) explicitly asserts antigravity is NOT auto-detected, citing req-tg-001.

**Spec says**: spec-research.md line 139 lists antigravity as "explicit only (shares .agents/, no unique signal)" under "Targets that are NEVER auto-detected."

**Summary of conflict**: Three sources conflict on this point:
1. `detect.go` SignalWhitelist maps `GEMINI.md`/`AGENTS.md` -> antigravity (detection IS wired)
2. `adapter.go` explicitOnlyTargets strips antigravity from auto-detect results (override)
3. Acceptance checklist "research fix" says antigravity DOES auto-detect

The code and test enforce explicit-only. The acceptance checklist's "research fix" note says the opposite. This needs a definitive policy decision before writing tests.

---

### GAP 2: No `DetectTargets()` Unit Tests

**File**: `internal/manifest/detect.go`

No file `detect_test.go` exists. `DetectTargets()` is only exercised indirectly through `TestResolveTargets_AutoDetect` (which tests `.claude/` dir only).

**Untested detection signals from SignalWhitelist** (detect.go:17-28):

| Signal | Target | Test Coverage |
|---|---|---|
| `.claude/` dir | claude | Indirect via ResolveTargets |
| `CLAUDE.md` file | claude | NONE |
| `.github/copilot-instructions.md` file | copilot | NONE |
| `.github/instructions/` dir | copilot | NONE |
| `.github/agents/` dir | copilot | NONE |
| `.github/prompts/` dir | copilot | NONE |
| `.github/hooks/` dir | copilot | NONE |
| `.codex/` dir | codex | NONE |
| `.opencode/` dir | opencode | NONE |
| `GEMINI.md` file | antigravity | NONE (used only in negative test) |
| `AGENTS.md` file | antigravity | NONE (used only in negative test) |

---

### GAP 3: Hooks Primitive -- Defined But Not Deployed by Any Adapter

**Defined**: `TypeHooks` exists in primitive.go:16 and is collected via `collectFromAPMDir` (primitive.go:84).

**No adapter supports hooks**: None of the 6 adapters list `TypeHooks` in their `SupportedTypes()`:

| Adapter | SupportedTypes (code) | Oracle expects hooks? |
|---|---|---|
| claude | instructions, agents, skills, commands | Checklist says yes |
| codex | agents, skills | Checklist says yes |
| copilot | instructions, prompts, agents, skills | Checklist says yes |
| antigravity | instructions, skills, agents | Oracle says hooks.json |
| opencode | agents, commands, skills | Checklist says no |
| agent-skills | skills | Oracle says no |

**Oracle expectations** (antigravity.yaml):
- `hooks: { file: .agents/hooks.json }` -- antigravity expects hooks deployed to `.agents/hooks.json`
- `mcp: { file: .agents/mcp_config.json, key: mcpServers }` -- antigravity expects MCP config

**Code reality**: No code references `hooks.json`, `settings.json` (for hooks), or `mcp_config.json` anywhere in `internal/deploy/`.

**Archived PRD says** (06-29 prd.md line 68): "Compile outputs (CLAUDE.md, AGENTS.md, copilot-instructions.md) -- future phase" and line 69: "MCP server config deployment (separate adapter subsystem)". However, hooks deployment (non-compile, non-MCP) is listed as a supported primitive in the checklist's 4-T matrix for claude/codex/copilot/antigravity.

---

### GAP 4: not_deployed Negative Tests Are Absent

The oracle specifies `not_deployed` lists per target -- primitives that an adapter does NOT support. No tests verify that unsupported primitives are silently skipped (negative assertion).

| Target | `not_deployed` (oracle) | Negative Test? |
|---|---|---|
| claude | prompts | NONE |
| codex | instructions, prompts, commands | NONE |
| copilot | (none listed) | N/A |
| antigravity | commands, prompts | NONE |
| opencode | instructions, hooks | NONE |
| agent-skills | instructions, prompts, agents, commands, hooks, mcp | Partial (instructions only in `TestDeployAgentSkills_SkillsOnly`) |

Only `TestDeployAgentSkills_SkillsOnly` partially covers this: it feeds an instruction primitive and verifies only the skill is deployed. But it does not assert that the instruction was explicitly skipped or that the count is exactly 1 for the right reason.

---

### GAP 5: Cursor and Windsurf Negative Tests Missing

**Checklist section** "Reduced targets negative tests" (line 167-170) requires:

| Case | Status |
|---|---|
| `--target gemini` | Covered by `TestResolveTargets_UnsupportedTargetDiag` |
| `--target cursor` | NOT tested |
| `--target windsurf` | NOT tested |

All three should emit "no registered handler" diagnostic per req-tg-004. Only gemini is tested.

---

### GAP 6: Copilot Prompts -- Supported But Untested

`copilotAdapter.SupportedTypes()` includes `TypePrompts` (copilot.go:12), and the DeployPrimitive switch handles it (copilot.go:21-22, deploying to `.github/prompts/<name>.prompt.md`).

No test exercises prompt deployment for copilot. No oracle fixture includes a prompt. The copilot oracle does not list `not_deployed: [prompts]` (unlike claude which does), implying prompts are expected to work.

---

### GAP 7: Antigravity Agents -- Code Supports But Oracle Omits

`antigravityAdapter.SupportedTypes()` includes `TypeAgents` (antigravity.go:12), deploying to `.agents/agents/<name>.md` (antigravity.go:21-22).

The oracle `antigravity.yaml` lists `deployed_files` as only `rules/demo.md` + `skills/demo/SKILL.md`. No agent file is listed in expected output.

`TestDeployAntigravity_OracleMatch` sidesteps this by feeding only instructions + skills (no agent primitive in the fixture).

However, the golden `_input` fixture (`conformance/conformance-kit/oracle/targets/_input/`) DOES contain `.apm/agents/helper.agent.md`. A real golden-run against `_input` with the antigravity adapter would produce `.agents/agents/helper.md` -- a file not listed in the oracle's `deployed_files`. This is a code-vs-oracle mismatch.

---

### GAP 8: compile_outputs -- Explicitly Deferred

| Target | compile_outputs (oracle) | Implemented? |
|---|---|---|
| claude | CLAUDE.md | No |
| codex | AGENTS.md | No |
| copilot | .github/copilot-instructions.md | No |
| antigravity | AGENTS.md | No |
| opencode | (none listed) | N/A |
| agent-skills | (none listed) | N/A |

The archived Phase 4 PRD (line 68) explicitly lists compile outputs as "future phase -- out of scope." This is a known deferral, not a missing implementation. No test or code for compile_outputs exists.

---

### GAP 9: MCP Config Paths -- Not Implemented, Not Tested

Oracle antigravity.yaml specifies:
- `mcp: { file: .agents/mcp_config.json, key: mcpServers, http_field: serverUrl, var_interpolation: false }`
- `hooks: { file: .agents/hooks.json }`

No code in `internal/deploy/` references `mcp_config.json` or `hooks.json`. The archived PRD (line 69) defers MCP to a "separate adapter subsystem." Whether `hooks.json` is also deferred or expected as part of Phase 4 hooks deployment is ambiguous from the PRD text.

---

### GAP 10: Oracle Match Tests Use Hardcoded Maps, Not Oracle YAML Files

All "OracleMatch" tests (e.g., `TestDeployClaude_OracleMatch`) embed the expected file list as Go `map[string]bool` literals. They reference the oracle only in comments.

The authoritative oracle files live at `conformance-kit/oracle/targets/expected/*.yaml`. The Go tests do not load or parse these files. If the oracle is updated, the Go tests would silently drift.

The real golden `_input -> expected` comparison is an external `APM_BIN`-driven runner (per `EXPECTATIONS.yaml` line 45: `outcome: golden, expected_dir: expected`). So "tested in Go" is not equivalent to "conformance-verified."

---

### GAP 11: Codex OracleMatch Test -- Missing Length Assertion

`TestDeployCodex_OracleMatch` (deploy_test.go:185-216) checks individual files against the expected map but does NOT assert that `len(deployed) == len(expected)`. An adapter that deployed extra unexpected files would pass the test. Both `TestDeployAntigravity_OracleMatch` and `TestDeployOpenCode_OracleMatch` have the same missing assertion.

In contrast, `TestDeployClaude_OracleMatch` and `TestDeployCopilot_OracleMatch` DO assert `len(deployed) != len(expected)`.

---

## Summary Table

| # | Gap | Severity | Status |
|---|---|---|---|
| 1 | Antigravity auto-detect: checklist says YES, code says NO | Policy decision needed | Conflict |
| 2 | `DetectTargets()` has no unit tests; 10/11 signals untested | High | Missing |
| 3 | Hooks primitive defined but no adapter deploys it | Medium | Not implemented |
| 4 | `not_deployed` negative tests absent for all targets | High | Missing |
| 5 | `--target cursor` and `--target windsurf` negative tests | Medium | Missing |
| 6 | Copilot prompts: supported in code, never tested | Low | Missing test |
| 7 | Antigravity agents: code supports, oracle omits | Low | Mismatch |
| 8 | compile_outputs (CLAUDE.md, AGENTS.md, etc.) | N/A | Deferred (by design) |
| 9 | MCP/hooks special paths (.agents/hooks.json, mcp_config.json) | Medium | Not implemented |
| 10 | OracleMatch tests hardcode expectations, don't load oracle YAML | Medium | Structural |
| 11 | Codex/antigravity/opencode OracleMatch lack length assertion | Low | Missing assertion |

## Caveats

- The acceptance checklist and oracle may themselves conflict on antigravity detection (GAP 1). Resolution requires a decision from the project owner.
- Hooks/MCP scope boundaries are ambiguous: the archived PRD defers "MCP server config" and "compile outputs" but does not explicitly address hook file deployment (`.agents/hooks.json`, `.claude/settings.json`).
- The "OracleMatch" tests in Go are NOT the conformance golden test. The conformance runner (`conformance-kit/runner/run_conformance.py`) drives the real binary as a subprocess. The Go tests are unit-level coverage only.
