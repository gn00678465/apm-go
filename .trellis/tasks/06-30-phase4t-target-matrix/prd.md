# Phase 4-T: Per-Target Deploy Matrix Tests & Fixes

## Goal

Close all gaps between the 4-T acceptance-checklist matrix and the current implementation. Add per-target tests, fix code-vs-oracle mismatches, implement missing hooks deploy for antigravity.

## Scope (from gap analysis)

| # | Gap | Action |
|---|-----|--------|
| 1 | Antigravity auto-detect | **Keep explicit-only** (user decision). Document as known deviation |
| 2 | DetectTargets() untested (10/11 signals) | Add `detect_test.go` |
| 3 | Hooks not deployed by any adapter | Implement file-copy hooks: antigravity→`.agents/hooks.json`, codex→`.codex/hooks.json`, copilot→`.github/hooks/<n>.json`. claude hooks = settings.json compile-merge (deferred, user decision) |
| 4 | `not_deployed` negative tests absent | Add per-target negative tests |
| 5 | cursor/windsurf negative tests missing | Add alongside existing gemini test |
| 6 | Copilot prompts untested | Add test |
| 7 | Antigravity agents: code deploys but oracle omits | Remove agents from antigravity SupportedTypes (oracle is authority) |
| 8 | compile_outputs | **Deferred** (by design) |
| 9 | MCP/hooks special paths | Antigravity hooks.json: implement. mcp_config.json: deferred |
| 10 | OracleMatch tests use hardcoded maps | Refactor to load oracle YAML |
| 11 | Missing length assertions | Add to codex/antigravity/opencode tests |

## Acceptance Criteria

1. `detect_test.go` covers all 11 SignalWhitelist entries
2. Each target has a `not_deployed` negative test
3. `--target cursor` and `--target windsurf` emit diagnostic
4. Copilot prompts test exists
5. Antigravity adapter matches oracle (no agents, hooks → `.agents/hooks.json`)
6. OracleMatch tests load actual oracle YAML files
7. All OracleMatch tests assert file count
8. All existing tests pass
9. Coverage ≥ 80% on deploy package
10. Verified by Codex, not self-verification
