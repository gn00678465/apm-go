# Phase 4-T Implementation Plan

## Step 1: DetectTargets unit tests
**File:** `internal/manifest/detect_test.go` (new)
**Verify:** `go test ./internal/manifest/ -run TestDetect`

Table-driven tests covering all 11 SignalWhitelist entries:
- `.claude/` dir → claude
- `CLAUDE.md` file → claude
- `.github/copilot-instructions.md` → copilot
- `.github/instructions/` dir → copilot
- `.github/agents/` dir → copilot
- `.github/prompts/` dir → copilot
- `.github/hooks/` dir → copilot
- `.codex/` dir → codex
- `.opencode/` dir → opencode
- `GEMINI.md` → antigravity (but filtered by explicitOnly in adapter layer)
- `AGENTS.md` → antigravity (same)
- No signal → empty
- Multiple signals → both targets returned

## Step 2: Fix antigravity adapter — remove agents, add hooks
**File:** `internal/deploy/antigravity.go`
**Verify:** `go test ./internal/deploy/ -run TestAntigravity`

- Remove `TypeAgents` from SupportedTypes (oracle omits agents for antigravity)
- Add `TypeHooks` to SupportedTypes
- Deploy hooks to `.agents/hooks.json` (simple file copy, not merge)

## Step 3: not_deployed negative tests
**File:** `internal/deploy/deploy_test.go`
**Verify:** `go test ./internal/deploy/ -run TestNotDeployed`

Per-target table-driven test: feed ALL primitive types, verify only supported types are deployed.

| Target | Must deploy | Must NOT deploy |
|--------|-----------|-----------------|
| claude | instructions, agents, skills, commands | prompts |
| codex | agents, skills | instructions, prompts, commands |
| copilot | instructions, prompts, agents, skills | commands |
| antigravity | instructions, skills, hooks | commands, prompts, agents |
| opencode | agents, commands, skills | instructions, hooks, prompts |
| agent-skills | skills | instructions, prompts, agents, commands, hooks |

## Step 4: cursor/windsurf negative tests
**File:** `internal/deploy/deploy_test.go`
**Verify:** `go test ./internal/deploy/ -run TestUnsupported`

- `--target cursor` → "no registered handler" diagnostic
- `--target windsurf` → "no registered handler" diagnostic

## Step 5: Copilot prompts test
**File:** `internal/deploy/deploy_test.go`
**Verify:** `go test ./internal/deploy/ -run TestCopilotPrompts`

Feed a prompt primitive, verify deployed to `.github/prompts/<name>.prompt.md`.

## Step 6: OracleMatch tests — load actual YAML
**File:** `internal/deploy/deploy_test.go`
**Verify:** `go test ./internal/deploy/ -run TestOracle`

Replace hardcoded maps with YAML loading from `conformance-kit/oracle/targets/expected/*.yaml`.
Add length assertions to codex/antigravity/opencode tests.

## Step 7: Full validation
**Verify:** `go test ./... -cover`, `go vet ./...`

All tests pass, deploy coverage ≥ 80%, Codex verification.
