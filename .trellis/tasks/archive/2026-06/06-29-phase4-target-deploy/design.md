# Phase 4 Design: Primitive Sourcing + Target Deploy

## Authority

File placement verified against oracle at `conformance-kit/oracle/targets/expected/*.yaml`.
The oracle input is `targets/_input/` with primitives in `.apm/{instructions,agents,skills,commands}/`.

## Architecture

Phase 4 adds one new package `internal/deploy/` and extends install.go.

### Package Layout

```
internal/deploy/
  primitive.go     — Primitive type, source attribution, collection from .apm/ dirs
  conflict.go      — Conflict resolution (local wins, declaration-order)
  adapter.go       — TargetAdapter interface, registry, target resolution
  claude.go        — Claude adapter
  codex.go         — Codex adapter
  copilot.go       — Copilot adapter
  antigravity.go   — Antigravity adapter
  opencode.go      — OpenCode adapter
  agentskills.go   — Agent-skills adapter
  deploy.go        — Orchestrator: detect → collect → resolve → deploy
  deploy_test.go   — Tests
```

## Data Flow

```
install.go
  │
  ├─ parse manifest (existing)
  ├─ resolve deps (existing)
  ├─ clone deps to apm_modules/ (existing)
  │
  ├─ NEW: determine active targets
  │    priority: --target flag > manifest target: > DetectTargets() > no-deploy
  │
  ├─ NEW: collect primitives (ordered)
  │    1. scan local .apm/ directory → source="local"
  │    2. for each dep in manifest declaration order (direct deps):
  │       scan apm_modules/<key>/.apm/ → source="dependency:<key>"
  │    3. for each transitive dep in lockfile sorted order (repo_url, virtual_path):
  │       scan apm_modules/<key>/.apm/ → source="dependency:<key>"
  │
  ├─ NEW: resolve conflicts (per (name, type) pair)
  │    local always wins over dependency (req-pr-002)
  │    first-declared dependency wins over later (req-pr-003)
  │    conflicts recorded as diagnostics to stderr
  │
  ├─ NEW: deploy to targets
  │    for each active target:
  │      adapter.DeployPrimitive() for each resolved primitive
  │      all targets deploy skills to .agents/skills/<name>/SKILL.md (req-tg-003)
  │      record deployed files per dependency for lockfile
  │
  ├─ populate per-dep DeployedFiles/DeployedHashes in lockfile (existing fields)
  └─ write lockfile (existing)
```

## Key Types

```go
type PrimitiveType string
// "instructions", "agents", "skills", "commands", "hooks", "prompts"

type Primitive struct {
    Name     string        // e.g. "demo", "helper", "hello"
    Type     PrimitiveType
    Source   string        // "local" or "dependency:<key>"
    DepKey   string        // dependency unique key (empty for local)
    SrcPath  string        // absolute path to source file/dir
}

type DeployResult struct {
    PerDep map[string]*DepDeployResult // keyed by dep unique key ("" = local)
    Diags  []string
}

type DepDeployResult struct {
    Files  []string          // relative paths of deployed files
    Hashes map[string]string // path → sha256:<hex>
}
```

## Primitive Name Extraction

Source files in `.apm/` use naming conventions. Extract primitive name by stripping known suffixes:

| source pattern | type | name extraction |
|---------------|------|-----------------|
| `.apm/instructions/<name>.instructions.md` | instructions | strip `.instructions.md` |
| `.apm/instructions/<name>.md` | instructions | strip `.md` |
| `.apm/agents/<name>.agent.md` | agents | strip `.agent.md` |
| `.apm/agents/<name>.md` | agents | strip `.md` |
| `.apm/skills/<name>/SKILL.md` | skills | dir name |
| `.apm/commands/<name>.md` | commands | strip `.md` |
| `.apm/hooks/<name>.json` | hooks | strip `.json` |
| `.apm/prompts/<name>.prompt.md` | prompts | strip `.prompt.md` |
| `.apm/prompts/<name>.md` | prompts | strip `.md` |
| `SKILL.md` (package root) | skills | package name |
| `skills/<name>/SKILL.md` | skills | dir name |

## Target Adapter Interface

```go
type TargetAdapter interface {
    Name() string
    DeployRoots() []string
    SupportedTypes() []PrimitiveType
    DeployPrimitive(p Primitive, projectDir string) ([]string, error)
}
```

## File Placement Rules (oracle-verified)

### Claude (deploy root: `.claude/`)
Oracle: `targets/expected/claude.yaml`

| type | deploy path | oracle evidence |
|------|------------|-----------------|
| instructions | `.claude/rules/<name>.md` | `.claude/rules/demo.md` |
| agents | `.claude/agents/<name>.md` | `.claude/agents/helper.md` |
| commands | `.claude/commands/<name>.md` | `.claude/commands/hello.md` |
| skills | `.agents/skills/<name>/SKILL.md` | `.agents/skills/demo/SKILL.md` |
| prompts | NOT SUPPORTED | `not_deployed: [prompts]` |
| hooks | DEFERRED | not in oracle deployed_files |

### Codex (deploy roots: `.codex/`, `.agents/`)
Oracle: `targets/expected/codex.yaml`

| type | deploy path | oracle evidence |
|------|------------|-----------------|
| agents | `.codex/agents/<name>.toml` | `.codex/agents/helper.toml` |
| skills | `.agents/skills/<name>/SKILL.md` | `.agents/skills/demo/SKILL.md` |
| instructions | NOT SUPPORTED | `not_deployed: [instructions, prompts, commands]` |
| prompts | NOT SUPPORTED | |
| commands | NOT SUPPORTED | |
| hooks | DEFERRED | not in oracle deployed_files |

### Copilot (deploy root: `.github/`)
Oracle: `targets/expected/copilot.yaml`

| type | deploy path | oracle evidence |
|------|------------|-----------------|
| instructions | `.github/instructions/<name>.instructions.md` | `.github/instructions/demo.instructions.md` |
| agents | `.github/agents/<name>.agent.md` | `.github/agents/helper.agent.md` |
| skills | `.agents/skills/<name>/SKILL.md` | `.agents/skills/demo/SKILL.md` |
| prompts | `.github/prompts/<name>.prompt.md` | (not in oracle input but type supported) |
| hooks | DEFERRED | not in oracle deployed_files |

Note: copilot keeps the `.instructions.md` and `.agent.md` suffixes in output. Claude strips them.

### Antigravity (deploy root: `.agents/`)
Oracle: `targets/expected/antigravity.yaml`

| type | deploy path | oracle evidence |
|------|------------|-----------------|
| instructions | `.agents/rules/<name>.md` | `.agents/rules/demo.md` |
| skills | `.agents/skills/<name>/SKILL.md` | `.agents/skills/demo/SKILL.md` |
| agents | `.agents/agents/<name>.md` | (type supported per Python research) |
| commands | NOT SUPPORTED | `not_deployed: [commands, prompts]` |
| prompts | NOT SUPPORTED | |
| hooks | DEFERRED (`hooks: { file: .agents/hooks.json }`) | not in oracle deployed_files |

### OpenCode (deploy root: `.opencode/`)
Oracle: `targets/expected/opencode.yaml`

| type | deploy path | oracle evidence |
|------|------------|-----------------|
| agents | `.opencode/agents/<name>.md` | `.opencode/agents/helper.md` |
| commands | `.opencode/commands/<name>.md` | `.opencode/commands/hello.md` |
| skills | `.agents/skills/<name>/SKILL.md` | `.agents/skills/demo/SKILL.md` |
| instructions | NOT SUPPORTED | `not_deployed: [instructions, hooks]` |
| hooks | NOT SUPPORTED | `not_deployed: [instructions, hooks]` |

### Agent-Skills (deploy root: `.agents/`)
Oracle: `targets/expected/agent-skills.yaml`

| type | deploy path | oracle evidence |
|------|------------|-----------------|
| skills | `.agents/skills/<name>/SKILL.md` | `.agents/skills/demo/SKILL.md` |
| (all others) | NOT SUPPORTED | `not_deployed: [instructions, prompts, agents, commands, hooks, mcp]` |

## `.agents/` Partitioning (req-tg-002)

| subdirectory | owner targets |
|-------------|---------------|
| `.agents/skills/` | ALL skill-supporting targets (convergence, req-tg-003) |
| `.agents/rules/` | antigravity only |
| `.agents/agents/` | antigravity only |

## Conflict Resolution (req-pr-002, req-pr-003)

Input ordering matters. Primitives arrive in this order:
1. Local `.apm/` primitives (always first)
2. Direct deps in manifest declaration order
3. Transitive deps in lockfile sorted order (repo_url, virtual_path)

For each (name, type) key:
- First occurrence wins
- Local overriding dependency → diagnostic
- Dependency shadowed by earlier dependency → diagnostic

## Integration with install.go

Add `--target` flag. Deploy runs BEFORE the no-op check (so DeployedFiles/DeployedHashes are populated before semantic comparison). Sequence:

```
resolve → clone → collect primitives → resolve conflicts → deploy →
populate DeployedFiles in newLock → no-op check → write lockfile
```

## Explicitly Out of Scope

- Compile outputs (CLAUDE.md, AGENTS.md, copilot-instructions.md) — listed in oracle `compile_outputs` but separate subsystem
- Hooks merge (claude→settings.json, codex→hooks.json, antigravity→hooks.json) — N→1 merge doesn't fit per-primitive interface; deferred
- MCP server config deployment — separate adapter subsystem
- Format transforms — plain file copy only
- `--legacy-skill-paths` opt-out — future
- User-scope deploy — project-scope only
- Orphan cleanup — future
