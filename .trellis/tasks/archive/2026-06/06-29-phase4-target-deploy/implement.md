# Phase 4 Implementation Plan

## Execution Order

### Step 1: Primitive types and collection
**Files:** `internal/deploy/primitive.go`
**Verify:** `go build ./internal/deploy/`

- Define `PrimitiveType` constants: instructions, agents, skills, commands, hooks, prompts
- Define `Primitive` struct: Name, Type, Source (string), DepKey (string), SrcPath (string)
- `CollectLocalPrimitives(projectDir) → []Primitive` — scan `.apm/` subdirectories
  - `.apm/instructions/*.md` → type=instructions
  - `.apm/agents/*.md` → type=agents
  - `.apm/skills/<name>/SKILL.md` → type=skills, name from dir
  - `.apm/commands/*.md` → type=commands
  - `.apm/hooks/*.json` → type=hooks
  - `.apm/prompts/*.md` → type=prompts
  - All with Source="local"
- `CollectDependencyPrimitives(depKey, modulePath) → []Primitive` — same scan, Source="dependency:<depKey>"
- Also detect SKILL.md at package root (skill bundle) and `skills/<name>/SKILL.md` (skill collection)

### Step 2: Conflict resolution
**Files:** `internal/deploy/conflict.go`
**Verify:** `go test ./internal/deploy/ -run TestConflict`

- `ResolvePrimitives(primitives []Primitive) → (winners []Primitive, diags []string)`
- Key = (Name, Type) pair
- Local always wins over dependency (req-pr-002), emits diagnostic
- First-declared dependency wins among deps (req-pr-003), emits diagnostic
- Input ordering: locals first, then direct deps in manifest order, then transitive in lockfile sorted order (repo_url, virtual_path)

### Step 3: Target adapter interface and registry
**Files:** `internal/deploy/adapter.go`
**Verify:** `go build ./internal/deploy/`

- `TargetAdapter` interface: Name(), DeployRoots(), SupportedTypes(), DeployPrimitive(p, projectDir) ([]string, error)
- `var Adapters = map[string]TargetAdapter{...}` — populated with all 6 adapters
- `ResolveTargets(flagTarget string, manifestTargets []string, projectDir string) ([]string, error)` — priority: flag > manifest > DetectTargets() > empty
- Unsupported target with no adapter → return diagnostic, not error

### Step 4: Implement 6 adapters
**Files:** `internal/deploy/claude.go`, `codex.go`, `copilot.go`, `antigravity.go`, `opencode.go`, `agentskills.go`
**Verify:** `go build ./internal/deploy/`

Each adapter: struct implementing TargetAdapter.
- DeployPrimitive: read source file → write to target path → ensure parent dirs
- Skills always → `.agents/skills/<name>/SKILL.md` (all adapters, req-tg-003)
- Unsupported primitive types for a target → skip silently (e.g. prompts for claude)
- Returns relative file paths of written files

### Step 5: Deploy orchestrator
**Files:** `internal/deploy/deploy.go`
**Verify:** `go build ./internal/deploy/`

- `DeployResult` struct: PerDep map[string]*DepDeployResult, Diags []string
- `DepDeployResult` struct: Files []string, Hashes map[string]string
- `Run(targets []string, projectDir string, m *manifest.Manifest, resolved *resolver.ResolutionResult, lock *lockfile.Lockfile) (*DeployResult, error)`
- Pipeline:
  1. Collect locals → append to ordered list
  2. Collect direct deps (manifest ParsedDeps order) → append
  3. Collect transitive deps (lockfile sorted order, skip direct) → append
  4. Resolve conflicts → winners + diags
  5. For each active target: for each winner: adapter.DeployPrimitive()
  6. Track deployed files per dep key
  7. Compute hashes for deployed files

### Step 6: Integrate into install.go
**Files:** `cmd/apm/install.go`
**Verify:** `go build ./cmd/apm && go test ./cmd/apm/`

- Add `--target` flag to install command (string, optional)
- After building newLock (step 5), call deploy phase:
  - resolveTargets(flagTarget, m.Target, ".")
  - if targets > 0: deploy.Run()
  - populate newLock.Dependencies[i].DeployedFiles and DeployedHashes from deployResult.PerDep
  - populate newLock.LocalDeployedFiles and LocalDeployedHashes from deployResult.PerDep[""]
  - print conflict diags to stderr
  - print deployed summary to stdout
- Unsupported target with no adapter → diagnostic to stderr, continue

### Step 7: Tests
**Files:** `internal/deploy/deploy_test.go`, `internal/deploy/conflict_test.go`, `internal/deploy/primitive_test.go`
**Verify:** `go test ./internal/deploy/ -cover` (target ≥ 80%)

Table-driven tests:
- req-pr-001: source attribution (local=".apm/", dependency="apm_modules/<key>/.apm/")
- req-pr-002: local overrides dependency (same name+type), diagnostic emitted
- req-pr-003: declaration-order priority, first dep wins
- req-tg-001: detection predicates fire correctly; agent-skills/antigravity NOT auto-detected
- req-tg-002: deploy writes only under registered roots
- req-tg-003: all adapters deploy skills to `.agents/skills/<name>/SKILL.md`
- Negative: unsupported target (gemini) emits diagnostic
- Each adapter: correct file placement per design.md table

### Step 8: Full verification
**Verify:** `go test ./... -cover`, `go vet ./...`, `go fmt ./...`

- All existing tests pass (no regressions)
- New deploy tests pass with ≥ 80% coverage
- Sub-agent (opus) verification of each req

## Validation Commands

```bash
go build ./...                    # compiles
go test ./... -cover              # all tests pass, coverage check
go vet ./...                      # no issues
go fmt ./...                      # formatted
```
