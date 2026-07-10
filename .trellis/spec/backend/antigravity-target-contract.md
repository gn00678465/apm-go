# Antigravity Target Contract

> Executable contracts for the `antigravity` deploy target (Google Antigravity, CLI alias `agy`), established in task 07-05-antigravity-research (branch `feat/marketplace-install`, commits `d72dc6a` / `c6ef3f7` / `3471e45`). Evidence: official docs + agy **1.0.16** binary/live verification (`.trellis/tasks/07-05-antigravity-research/research/cli-*.md`).
>
> **Language**: English.

---

## 1. MCP writer: `serverUrl` for ALL remote transports (d72dc6a)

- Signature: `antigravityMCPEntry(r *ResolvedMCPServer) (map[string]any, bool, string)` in `internal/deploy/mcp_antigravity.go`.
- Output file `.agents/mcp_config.json`, top-level key `mcpServers`, mode `ResolveBake` (agy has NO runtime `${VAR}` interpolation — verified: no `os.ExpandEnv` on the MCP path in the binary).

| Transport | Fields written |
|---|---|
| `stdio` | `command` (+ `args`, `env` when present) |
| `sse`, `http`, `streamable-http` (any non-stdio) | `serverUrl` (+ `headers` when present) |

**Wrong vs correct**:

```go
// WRONG (pre-d72dc6a): sse special case
if r.Transport == "sse" { e["url"] = r.URL } else { e["serverUrl"] = r.URL }

// CORRECT: all remote transports
e := map[string]any{"serverUrl": r.URL}
```

Why: official docs verbatim — "Legacy fields like `url` or `httpUrl` are not supported"; the agy 1.0.16 binary validator only accepts `command|serverUrl` as transport discriminators ("MCP server %q must have either command or serverUrl"). Test lock: `TestWriteMCP_Antigravity_SSEUsesServerUrlField` (`mcp_writers_test.go`) — sse entry MUST have `serverUrl`, MUST NOT have `url`.

---

## 2. Explicit-only target selection (c6ef3f7, BREAKING)

- `explicitOnlyTargets` (`internal/deploy/adapter.go`) contains `antigravity` (and `agent-skills`); `allAutoDetectableTargets()` = `{claude, codex, copilot, opencode}` — antigravity excluded.
- `SignalWhitelist` (`internal/manifest/detect.go`) has NO GEMINI.md/AGENTS.md entries: those are cross-tool files (also read by opencode/agent-skills tooling) and must not auto-enable antigravity. Aligns with Python `EXPLICIT_ONLY_TARGETS={"agent-skills","antigravity"}` (user decision 2026-07-05).
- Alias: `TargetAliases["agy"] = "antigravity"` (`internal/manifest/target.go`). BOTH selection paths canonicalize through `manifest.ValidateTarget`: the `--target` flag via `deploy.SplitTargetFlag`, apm.yml `target:` via `parseTargetField` (`internal/manifest/manifest.go`) — so the alias needs exactly one table entry and `filterSupported` never sees a raw `agy`.

| Selection | Result |
|---|---|
| `--target antigravity` / `--target agy` | deploys ✓ |
| apm.yml `target: [antigravity]` / `[agy]` | deploys ✓ |
| `--target all` / apm.yml `target: [all]` | antigravity NOT included |
| GEMINI.md / AGENTS.md present, no explicit selection | NOT detected, nothing deployed |

**BREAKING**: users who relied on GEMINI.md/AGENTS.md auto-detection must now select antigravity explicitly. Test lock: `TestResolveTargets_AntigravityExplicitSelection` (+ `..._FlagAllExcludesAntigravity`, `..._ManifestAllExcludesAntigravity`, `..._AntigravityNotAutoDetected`).

---

## 3. Agents primitive (3471e45 — documented extension ahead of Python upstream)

- `antigravityAdapter.SupportedTypes()` includes `TypeAgents`; mapping:
  ```go
  case TypeAgents:
      return deployFileToPath(p, fmt.Sprintf(".agents/agents/%s/agent.md", p.Name), projectDir)
  ```
- **Per-agent directory, fixed filename `agent.md`** — unlike claude's flat `.claude/agents/<name>.md`. Static format exists since Antigravity CLI **1.0.16** (JSON→Markdown transition per changelog); live-verified discovered by agy.
- Byte-copy, no frontmatter transform (adapter-wide convention). Python upstream has NO antigravity agents mapping — this is an intentional apm-go extension (user decision 2026-07-10).
- Collision semantics: same-name agents are resolved BEFORE any adapter runs, in `ResolvePrimitives` (`internal/deploy/conflict.go`) — first-declared wins (req-pr-003), local overrides dependency (req-pr-002). The `writtenBy` overwrite diagnostic in `deploy.go` only covers different-named primitives converging on one FIXED path (e.g. `.agents/hooks.json`), never per-name paths. Test lock: `TestRun_AgentSameNameCollision_FirstDeclaredWins` (table-driven claude + antigravity).
- Uninstall: fully generic over `DeployPrimitive`-returned paths (lockfile `deployed_files`/`deployed_file_hashes` → `deploy.RemoveDeployedFiles` → `cleanupEmptyParents` prunes the empty per-agent dir, stops at non-empty ancestors). Test lock: `TestRemoveDeployedFiles_AntigravityAgentDirPrunedSiblingSurvives`.

---

## 4. Documented deviations (intentional — keep apm-go behavior)

| id | apm-go | Python | evidence / rationale |
|---|---|---|---|
| instructions frontmatter | byte-copy, frontmatter NOT stripped (`.agents/rules/<name>.md`) | strips YAML frontmatter (`_convert_to_antigravity_rules`) | live-verified: agy discovers and can follow a rule WITH frontmatter; no official frontmatter semantics |
| agents primitive | deployed (`.agents/agents/<name>/agent.md`) | no mapping | CLI ≥1.0.16 static format; apm-go ahead of upstream |
| rules enforcement expectation | files deployed; **agy CLI does NOT auto-inject workspace rules into system context** (only `user_global`) — rules are discoverable/readable by the agent on demand | n/a | 6-probe live matrix on agy 1.0.16 `--print` (task prd.md "Step 0 實機驗證結果"); deployment stays correct — activation semantics are agy's own concern |

---

## 5. agy operational gotchas (for verification work)

> **Warning**: agy only scans workspace `.agents/` when the workspace is **registered as a project** (`--new-project` on first run, `--project <id>` to reuse). Under the default `default-cli-project` (no folder resources), workspace rules/skills/agents are ALL invisible — a probe that skips registration falsely concludes "not supported".

- `agy --print "<prompt>"` runs a single prompt non-interactively (with `--print-timeout`) — use it for automated verification; there is NO `agy mcp`/`agy skills`/`agy agents` subcommand (TUI slash commands only).
- `agy plugin validate <dir>` verifies apm-go output formats with the real binary: accepts nested `agents/<name>/agent.md`, reports skills/agents/commands/mcpServers/hooks processed, and **resolves stdio `command` on PATH** (a fake command aborts validation) — use a real executable in fixtures. It does not check `rules/`.
- Global customization root is `~/.gemini/config/` (NOT `~/.gemini/antigravity-cli/`, which is app-data; several official CLI doc pages state stale paths).

---

## 6. Verification pattern (A/B without the Python oracle)

Python apm is not the oracle for antigravity CLI features. Pattern used (task Step 4, all PASS):

1. **Primary — structural compare**: fixture project → `apm-go install --target agy` → assert file tree + field-level JSON (`serverUrl` not `url`; byte-identity for agents/rules/skills/hooks) against binary-verified schemas.
2. **Supplemental — `agy plugin validate`** on outputs packaged as a plugin (must not gate the task: plugin subsystem changes shouldn't fail target work).
3. **Live discovery**: register fixture as agy project, `--print` ask to list agents/skills/rules — apm-go-deployed names must appear.
