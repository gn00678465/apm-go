# Phase 4: Primitive Sourcing + Target Deploy

## Overview

Phase 4 adds the deployment layer to `apm install`: after dependencies are resolved and cloned, primitives (skills, agents, instructions, etc.) are collected from local `.apm/` and dependency packages, conflicts are resolved, and files are deployed to target-specific locations on disk.

## Requirements

### Primitive Sourcing

| req | keyword | description |
|-----|---------|-------------|
| req-pr-001 | MUST | Every primitive carries source attribution: `local` (from `.apm/`) or `dependency:<name>` (from resolved dep) |
| req-pr-002 | MUST | Local primitive overrides dependency primitive of same (name, type); conflict recorded in diagnostics |
| req-pr-003 | MUST | Dependencies processed in manifest declaration order (direct first, transitive in lockfile sorted order by (repo_url, virtual_path)); first-declared wins for same (name, type) |

### Target Detection & Deploy

| req | keyword | description |
|-----|---------|-------------|
| req-tg-001 | MUST | Target activates ONLY when its registered detection predicate fires; `agent-skills` NEVER auto-detected; if no target detected, deploy phase skips (spec says MAY fallback to minimal; we choose no-deploy) |
| req-tg-002 | MUST | Deploy only under registered deploy root(s); writing outside is implementation defect; shared `.agents/` partitioned by subdirectory |
| req-tg-003 | MUST | Skills deploy to `.agents/skills/<name>/SKILL.md` for all skill-supporting targets (unless opt-out). Spec wins over Python's claude native path |

### Target Adapter Matrix (6 adapters)

| target | detection signal | deploy root(s) | supported primitives | notes |
|--------|-----------------|----------------|---------------------|-------|
| claude | `.claude/` or `CLAUDE.md` | `.claude/` | instructions, agents, skills, commands, hooks | hooks → `.claude/settings.json` (compile deferred; Phase 4 copies to hooks subdir) |
| codex | `.codex/` | `.codex/` + `.agents/` | agents, skills, hooks | agents as `.toml`; hooks → `.codex/hooks.json` |
| copilot | `.github/copilot-instructions.md` or `.github/instructions/` or `.github/agents/` or `.github/prompts/` or `.github/hooks/` | `.github/` | instructions, prompts, agents, skills, hooks | |
| antigravity | explicit `--target` only (no auto-detect) | `.agents/` | instructions, skills, hooks, agents | pre-standard; hooks → `.agents/hooks.json` |
| opencode | `.opencode/` | `.opencode/` | agents, commands, skills | no hooks support |
| agent-skills | explicit `--target` only | `.agents/` | skills only | cross-client bundle |

### Negative Tests (reduced targets)

| case | expected |
|------|----------|
| `--target gemini` / `--target cursor` / `--target windsurf` | parse accepts, deploy emits "no registered handler" diagnostic per req-tg-004, not silent, not crash |

## Constraints

- Manifest `target:` field and `DetectTargets()` already exist (Phase 1)
- Install pipeline already resolves + clones deps into `apm_modules/`
- `DeployedFiles` / `DeployedHashes` fields already exist in lockfile types
- Must not break existing `validate`, `normalize`, `init`, `install` commands
- Style must match existing codebase (yaml.Node-based, DI via interfaces, table-driven tests)
- No oracle fixtures exist for Phase 4 targets (testdata cleared per acceptance-checklist)
- Python apm is reference implementation but spec wins where they diverge

## Acceptance Criteria

1. `apm install` detects targets and deploys primitives to correct locations
2. Local `.apm/` primitives override dependency primitives (same name/type)
3. Conflict diagnostics printed to stderr
4. `--target <name>` overrides auto-detection
5. `agent-skills` and `antigravity` never auto-detected
6. No files written outside registered deploy roots
7. Skills converge to `.agents/skills/<name>/SKILL.md` for all targets
8. `DeployedFiles` and `DeployedHashes` populated per-entry in lockfile
9. Unsupported targets (gemini, cursor, windsurf) emit diagnostic, not crash
10. All existing tests pass; new tests cover each req
11. Verified by external sub-agent (opus), not self-verification

## Explicitly Out of Scope

- Compile outputs (CLAUDE.md, AGENTS.md, copilot-instructions.md) — future phase
- MCP server config deployment (separate adapter subsystem) — MCP primitives treated as file copy
- Format transforms (.instructions.md → .mdc for cursor, etc.) — plain file copy only
- `--legacy-skill-paths` opt-out — mentioned in code, not wired
- User-scope deploy (`~/` paths) — project-scope only
- Orphan cleanup of stale deployed files — future phase
