# Research: Target Deploy System (Python apm)

- **Query**: How does microsoft/apm implement target auto-detection, adapters, primitive sourcing, skills convergence, and `.agents/` partitioning?
- **Scope**: internal (Python source at `D:\Projects\apm-dev\apm\`)
- **Date**: 2026-06-29

---

## 1. Target Auto-Detection

### Source Files

| File | Purpose |
|------|---------|
| `src/apm_cli/core/target_detection.py` | Detection signals, resolution algorithm, compile-family routing |
| `src/apm_cli/integration/targets.py` | `KNOWN_TARGETS` registry, `TargetProfile` dataclass, `active_targets()` |
| `src/apm_cli/install/phases/targets.py` | Install pipeline phase: wires detection into context |

### Two Code Paths (v2 is authoritative)

There are two detection implementations. The **v2 path** (`resolve_targets()` + `detect_signals()` + `SIGNAL_WHITELIST`) is the one that drives project-scope detection. The legacy `detect_target()` function (line 110 of `target_detection.py`) still exists but its return values are explicitly not consumed -- the call at `phases/targets.py:512` is commented "return values are not consumed by any downstream code". The Go port should implement v2 only.

### Resolution Priority (v2)

```
1. --target flag          (explicit CLI)
2. apm.yml targets: field (yaml config)
3. Auto-detect from SIGNAL_WHITELIST
4. Raise NoHarnessError if 0 signals, AmbiguousHarnessError if >= 2 targets
```

Note: legacy path falls back to `copilot` when nothing detected; v2 raises an error. User-scope detection (`active_targets_user_scope`) still uses directory-presence checks against `effective_root(user_scope=True)` and falls back to `copilot`.

### Detection Signal Whitelist (v2)

Defined at `target_detection.py:686-702` as `SIGNAL_WHITELIST`:

| Target    | Check Type | Path                                    |
|-----------|------------|-----------------------------------------|
| claude    | dir        | `.claude`                               |
| claude    | file       | `CLAUDE.md`                             |
| cursor    | dir        | `.cursor`                               |
| cursor    | file       | `.cursorrules` (legacy)                 |
| copilot   | file       | `.github/copilot-instructions.md`       |
| copilot   | dir        | `.github/instructions`                  |
| copilot   | dir        | `.github/agents`                        |
| copilot   | dir        | `.github/prompts`                       |
| copilot   | dir        | `.github/hooks`                         |
| codex     | dir        | `.codex`                                |
| gemini    | dir        | `.gemini`                               |
| gemini    | file       | `GEMINI.md`                             |
| opencode  | dir        | `.opencode`                             |
| windsurf  | dir        | `.windsurf`                             |
| kiro      | dir        | `.kiro`                                 |

**Critical**: copilot is NOT detected by bare `.github/` existence. It requires one of the 5 specific child paths listed above. The legacy `detect_target()` used bare `.github/` which would false-fire on any repo with CI.

### Targets That Are Never Auto-Detected

| Target        | Reason                                                         |
|---------------|----------------------------------------------------------------|
| antigravity   | Shares `.agents/` root with other targets; no unique signal    |
| agent-skills  | Cross-client meta-target; explicit `--target` only             |
| openclaw      | Experimental; flag-gated                                       |
| hermes        | Experimental; flag-gated                                       |
| copilot-cowork| Experimental; user-scope only; dynamic OneDrive path           |
| copilot-app   | Experimental; user-scope only; checks `~/.copilot/data.db`     |

These all have `detect_by_dir=False` in their `TargetProfile`.

### Alias Mapping

Defined at `target_detection.py:396-401`:

```python
TARGET_ALIASES = {
    "copilot": "vscode",
    "agents": "vscode",   # deprecated
    "vscode": "vscode",
    "agy": "antigravity",
}
```

The runtime-level alias map at `targets.py:449-453`:

```python
RUNTIME_TO_CANONICAL_TARGET = {
    "vscode": "copilot",
    "agents": "copilot",
    "intellij": "copilot",
}
```

---

## 2. Target Profiles (KNOWN_TARGETS)

Each target is a `TargetProfile` dataclass defined in `integration/targets.py:460-867`. Key fields:

```
TargetProfile:
  name            str              # canonical identifier
  root_dir        str              # top-level deploy directory (e.g. ".github")
  primitives      dict[str, PM]    # primitive_type -> PrimitiveMapping
  auto_create     bool             # create root_dir if missing
  detect_by_dir   bool             # eligible for auto-detection
  user_supported  bool|"partial"   # user-scope capability
  user_root_dir   str|None         # override root at user scope
  compile_family  str|None         # "vscode"|"claude"|"gemini"|"agents"|None
  pack_prefixes   tuple[str,...]   # lockfile path prefixes
  hooks_config_display str|None    # display path for hooks config
```

`PrimitiveMapping` defines where each primitive type lands:

```
PrimitiveMapping:
  subdir        str              # subdirectory under root (e.g. "rules", "agents")
  extension     str              # file extension (e.g. ".md", ".mdc", "/SKILL.md")
  format_id     str              # format transformer key (e.g. "cursor_rules")
  deploy_root   str|None         # override root_dir for this primitive only
  output_compare bool            # whether deployed file is format-transformed
```

### Per-Target Primitive Mappings

#### copilot (`root_dir=".github"`)

| Primitive     | subdir           | extension            | format_id              | deploy_root | Deploy path                                       |
|---------------|------------------|----------------------|------------------------|-----------|----------------------------------------------------|
| instructions  | `instructions`   | `.instructions.md`   | `github_instructions`  | -         | `.github/instructions/<name>.instructions.md`       |
| prompts       | `prompts`        | `.prompt.md`         | `github_prompt`        | -         | `.github/prompts/<name>.prompt.md`                  |
| agents        | `agents`         | `.agent.md`          | `github_agent`         | -         | `.github/agents/<name>.agent.md`                    |
| skills        | `skills`         | `/SKILL.md`          | `skill_standard`       | `.agents` | `.agents/skills/<name>/SKILL.md`                    |
| hooks         | `hooks`          | `.json`              | `github_hooks`         | -         | `.github/hooks/<name>.json`                         |
| canvas        | `extensions`     | (empty)              | `copilot_canvas`       | -         | `.github/extensions/...`                            |

User scope: `user_root_dir=".copilot"`, instructions override to concatenated single file.
Compile: `compile_family="vscode"` -> emits `AGENTS.md` + `.github/copilot-instructions.md`.
Generated: `copilot-instructions.md`.

#### claude (`root_dir=".claude"`)

| Primitive     | subdir     | extension   | format_id        | deploy_root | Deploy path                         |
|---------------|------------|-------------|------------------|-------------|--------------------------------------|
| instructions  | `rules`    | `.md`       | `claude_rules`   | -           | `.claude/rules/<name>.md`            |
| agents        | `agents`   | `.md`       | `claude_agent`   | -           | `.claude/agents/<name>.md`           |
| commands      | `commands` | `.md`       | `claude_command`  | -           | `.claude/commands/<name>.md`         |
| skills        | `skills`   | `/SKILL.md` | `skill_standard` | -           | `.claude/skills/<name>/SKILL.md`     |
| hooks         | `hooks`    | `.json`     | `claude_hooks`   | -           | merged into `.claude/settings.json`  |

Note: claude skills do NOT use `deploy_root=".agents"` -- they deploy to `.claude/skills/` natively.
User scope: honors `CLAUDE_CONFIG_DIR` env var (default `~/.claude`).
Compile: `compile_family="claude"` -> emits `CLAUDE.md`.
`auto_create=False`, `detect_by_dir=True`.

#### codex (`root_dir=".codex"`)

| Primitive | subdir   | extension    | format_id        | deploy_root | Deploy path                         |
|-----------|----------|--------------|------------------|-------------|--------------------------------------|
| agents    | `agents` | `.toml`      | `codex_agent`    | -           | `.codex/agents/<name>.toml`          |
| skills    | `skills` | `/SKILL.md`  | `skill_standard` | `.agents`   | `.agents/skills/<name>/SKILL.md`     |
| hooks     | (empty)  | `hooks.json` | `codex_hooks`    | -           | `.codex/hooks.json`                  |

No instructions, prompts, or commands primitives.
Compile: `compile_family="agents"` -> emits `AGENTS.md` only.
`pack_prefixes=(".codex/", ".agents/")` -- both dirs tracked in lockfile.

#### antigravity (`root_dir=".agents"`)

| Primitive     | subdir   | extension    | format_id              | deploy_root | Deploy path                         |
|---------------|----------|--------------|------------------------|-------------|--------------------------------------|
| instructions  | `rules`  | `.md`        | `antigravity_rules`    | -           | `.agents/rules/<name>.md`            |
| skills        | `skills` | `/SKILL.md`  | `skill_standard`       | -           | `.agents/skills/<name>/SKILL.md`     |
| hooks         | (empty)  | `hooks.json` | `antigravity_hooks`    | -           | `.agents/hooks.json`                 |

Explicit-only (`detect_by_dir=False`), not part of `--target all`.
Root is `.agents/` itself -- no separate target directory.
User scope: `user_root_dir=".gemini/antigravity-cli"`, partial (instructions+hooks excluded).
MCP: `.agents/mcp_config.json` (project) or `~/.gemini/config/mcp_config.json` (user).

#### opencode (`root_dir=".opencode"`)

| Primitive | subdir     | extension   | format_id          | deploy_root | Deploy path                             |
|-----------|------------|-------------|--------------------|-------------|-----------------------------------------|
| agents    | `agents`   | `.md`       | `opencode_agent`   | -           | `.opencode/agents/<name>.md`            |
| commands  | `commands` | `.md`       | `opencode_command`  | -           | `.opencode/commands/<name>.md`          |
| skills    | `skills`   | `/SKILL.md` | `skill_standard`   | `.agents`   | `.agents/skills/<name>/SKILL.md`        |

No instructions or hooks primitives.
User scope: `user_root_dir=".config/opencode"`, partial (hooks excluded).
Compile: `compile_family="agents"`.

#### agent-skills (`root_dir=".agents"`)

| Primitive | subdir   | extension   | format_id        | deploy_root | Deploy path                         |
|-----------|----------|-------------|------------------|-------------|--------------------------------------|
| skills    | `skills` | `/SKILL.md` | `skill_standard` | -           | `.agents/skills/<name>/SKILL.md`     |

Skills-only target. No agents, hooks, commands, or instructions.
Explicit-only (`detect_by_dir=False`), not part of `--target all`.
`auto_create=True`.

### Additional Targets (for completeness)

| Target   | root_dir     | Compile Family | Key Notes |
|----------|-------------|----------------|-----------|
| cursor   | `.cursor`   | `agents`       | instructions -> `.cursor/rules/<name>.mdc` (format transform); skills -> `.agents/skills/` |
| gemini   | `.gemini`   | `gemini`       | commands -> `.gemini/commands/<name>.toml`; skills -> `.agents/skills/` |
| windsurf | `.windsurf` | `agents`       | instructions -> `.windsurf/rules/<name>.md`; skills -> `.windsurf/skills/` (native, no `.agents/`) |
| kiro     | `.kiro`     | `agents`       | instructions -> `.kiro/steering/<name>.md`; skills -> `.kiro/skills/` (native) |

---

## 3. Primitive Sourcing (Read-Side)

### Source Files

| File | Purpose |
|------|---------|
| `src/apm_cli/primitives/discovery.py` | Local + dependency primitive scanning |
| `src/apm_cli/primitives/parser.py` | Frontmatter parsing for primitive files |

### Source Locations (Where APM Reads Primitives From)

Primitives are read from packages in `apm_modules/`. Each package may store primitives in:

| Source Directory                  | Primitive Types                  |
|-----------------------------------|----------------------------------|
| `.apm/instructions/`              | `*.instructions.md`              |
| `.apm/agents/`                    | `*.agent.md`                     |
| `.apm/skills/<name>/`             | `SKILL.md` (sub-skills)          |
| `.apm/prompts/`                   | `*.prompt.md`                    |
| `.apm/context/`                   | `*.context.md`                   |
| `.apm/memory/`                    | `*.memory.md`                    |
| `.github/instructions/`           | `*.instructions.md` (alt source) |
| `.github/agents/`                 | `*.agent.md` (alt source)        |
| Package root                      | `SKILL.md` (native skill)        |
| Package root `skills/`            | `<name>/SKILL.md` (skill bundle) |

### Discovery Priority

From `discover_primitives_with_dependencies()`:

```
1. Local .apm/ and .github/    (highest -- always wins)
2. Dependencies in declaration order from apm.yml (first declared wins)
3. Transitive deps from apm.lock
4. Local-bundle slugs from lockfile local_deployed_files
```

### Package Type Routing

The `get_effective_type()` function in `skill_integrator.py` determines how a package is processed:

| PackageType              | Effective Type  | Skill Installed? | Instructions Compiled? |
|--------------------------|-----------------|------------------|------------------------|
| `CLAUDE_SKILL`           | SKILL           | Yes              | No                     |
| `HYBRID`                 | SKILL           | Yes              | Yes                    |
| `SKILL_BUNDLE`           | SKILL           | Yes              | No                     |
| `MARKETPLACE_PLUGIN`     | SKILL           | Yes              | No                     |
| (default / instructions) | INSTRUCTIONS    | No               | Yes                    |

---

## 4. Skills Convergence

### Default Behavior

Most targets deploy skills to the **shared** `.agents/skills/<name>/SKILL.md` path via `deploy_root=".agents"` on the skills `PrimitiveMapping`. This is the "skills convergence" model.

Targets that converge to `.agents/skills/`:
- copilot (via `deploy_root=".agents"`)
- cursor (via `deploy_root=".agents"`)
- codex (via `deploy_root=".agents"`)
- gemini (via `deploy_root=".agents"`)
- opencode (via `deploy_root=".agents"`)

Targets that keep skills in their native directory (no `deploy_root` override):
- claude -> `.claude/skills/<name>/SKILL.md`
- windsurf -> `.windsurf/skills/<name>/SKILL.md`
- kiro -> `.kiro/skills/<name>/SKILL.md`
- antigravity -> `.agents/skills/<name>/SKILL.md` (native, root_dir IS `.agents/`)
- agent-skills -> `.agents/skills/<name>/SKILL.md` (native, root_dir IS `.agents/`)

### Skill Name Format (agentskills.io spec)

- 1-64 characters
- Lowercase alphanumeric + hyphens only (`[a-z0-9-]`)
- No consecutive hyphens
- No leading/trailing hyphens
- `normalize_skill_name()` auto-converts: `owner/repo` -> extract repo, camelCase -> hyphen-case, truncate to 64

### Legacy Skill Paths Opt-Out

`apply_legacy_skill_paths()` in `targets.py:870-896` resets `deploy_root` to `None` on every skills `PrimitiveMapping`, reverting to per-target native paths (e.g. `.github/skills/`, `.cursor/skills/`).

Activated by:
- `--legacy-skill-paths` CLI flag
- `APM_LEGACY_SKILL_PATHS=1` env var

### Sub-Skill Promotion

Packages can ship sub-skills under `.apm/skills/<name>/SKILL.md`. These are "promoted" to top-level entries at each target's skills root via `_promote_sub_skills()`. The promotion deduplicates across targets (same resolved path = skip) and respects managed-files ownership.

---

## 5. `.agents/` Shared Root Partitioning

The `.agents/` directory is a shared cross-tool root used by multiple targets. Here is how it is partitioned:

### Directory Ownership

| Path                          | Owner Target(s)                                  | Purpose                          |
|-------------------------------|--------------------------------------------------|----------------------------------|
| `.agents/skills/<name>/`      | copilot, cursor, codex, gemini, opencode (converged) + antigravity, agent-skills, openclaw, hermes (native) | Shared skill bundles |
| `.agents/rules/<name>.md`     | antigravity only                                 | Antigravity instruction rules    |
| `.agents/hooks.json`          | antigravity only                                 | Antigravity hook config          |
| `.agents/mcp_config.json`     | antigravity only (MCP adapter)                   | Antigravity MCP server config    |

### Orphan Cleanup Safety

The shared `.agents/skills/` directory has special cleanup logic (`_clean_orphaned_skills` at `skill_integrator.py:1827-1873`):

- When `is_agents_dir=True` (detected by `skills_dir.parent.name == ".agents"`), cleanup only removes skills that appear in the lockfile's `deployed_files` (via `_get_lockfile_owned_agent_skills()`).
- This prevents APM from deleting skills placed by other tools (Codex CLI, manual authoring, etc.).
- Non-`.agents/` skill directories (e.g. `.claude/skills/`) use standard npm-style orphan detection (any skill not matching an installed package name is removed).

### Cross-Target Deduplication

When multiple active targets converge to `.agents/skills/`, the skill integrator deduplicates by resolved path:

```python
resolved = skill_dir.resolve()
if resolved in seen_skill_dirs:
    continue  # skip, already deployed
seen_skill_dirs.add(resolved)
```

This means `.agents/skills/foo/SKILL.md` is written once even if copilot, cursor, and codex are all active targets.

---

## 6. MCP Server Configuration (Separate Subsystem)

MCP configuration is NOT part of the `TargetProfile` primitive system. It is handled by:

| File | Purpose |
|------|---------|
| `src/apm_cli/adapters/client/base.py` | `MCPClientAdapter` abstract base class |
| `src/apm_cli/adapters/client/claude.py` | Claude-specific MCP config writer |
| `src/apm_cli/adapters/client/codex.py` | Codex-specific MCP config writer |
| `src/apm_cli/adapters/client/copilot.py` | Copilot/VSCode MCP config writer |
| `src/apm_cli/adapters/client/antigravity.py` | Antigravity MCP config writer |
| `src/apm_cli/adapters/client/opencode.py` | OpenCode MCP config writer |
| (and others for cursor, gemini, windsurf, kiro, intellij, hermes, vscode) | |

Each adapter writes to its target's native MCP config location. The MCP integrator gates writes to only the active target set, so a runtime outside the set is skipped with a diagnostic message.

---

## 7. Format Transforms (Separate Subsystem)

Instruction deployment includes format transforms for targets with `output_compare=True`:

| format_id            | Transform                                               | Target    |
|----------------------|----------------------------------------------------------|-----------|
| `cursor_rules`       | Converts `.instructions.md` -> `.mdc` (Cursor rule fmt)  | cursor    |
| `claude_rules`       | Converts to Claude `.claude/rules/` format with paths     | claude    |
| `windsurf_rules`     | Converts to Windsurf rules format                         | windsurf  |
| `kiro_steering`      | Converts to Kiro steering format with `inclusion:` header | kiro      |
| `antigravity_rules`  | Converts to Antigravity plain markdown rules              | antigravity |

The set of format IDs is defined in `RULE_FORMATS` at `targets.py:28-29`. Transform logic lives in `integration/instruction_integrator.py`.

---

## 8. Key Constants for Go Port

### Canonical Deploy Directories

From `target_detection.py:717-726`:

```python
CANONICAL_DEPLOY_DIRS = {
    "claude": ".claude/",
    "copilot": ".github/",
    "cursor": ".cursor/",
    "codex": ".codex/",
    "gemini": ".gemini/",
    "opencode": ".opencode/",
    "windsurf": ".windsurf/",
    "kiro": ".kiro/",
}
```

### Compile Families

| Family    | Output                                      | Targets                                        |
|-----------|---------------------------------------------|-------------------------------------------------|
| `vscode`  | `AGENTS.md` + `.github/copilot-instructions.md` | copilot                                      |
| `claude`  | `CLAUDE.md`                                 | claude                                          |
| `gemini`  | `GEMINI.md`                                 | gemini                                          |
| `agents`  | `AGENTS.md` only                            | cursor, opencode, codex, antigravity, windsurf, kiro, hermes |

### All Canonical Targets (auto-detectable)

```python
ALL_CANONICAL_TARGETS = frozenset(
    {"vscode", "claude", "cursor", "opencode", "codex", "gemini", "windsurf", "kiro"}
)
```

### Explicit-Only Targets

```python
EXPLICIT_ONLY_TARGETS = frozenset({"agent-skills", "antigravity"})
```

### Experimental Targets

```python
EXPERIMENTAL_TARGETS = frozenset({"copilot-cowork", "copilot-app", "openclaw", "hermes"})
```

---

## Caveats / Not Found

- The Python codebase has two detection paths (legacy + v2). The Go port should implement v2 only. Legacy `detect_target()` is kept for "behavior parity" but its results are not consumed.
- User-scope detection uses a simpler directory-presence check (`active_targets_user_scope`), not `SIGNAL_WHITELIST`. The Go port needs both paths.
- MCP adapter details (per-target config file paths, JSON schema differences) are not fully documented here. See `adapters/client/*.py` for each target's native MCP config format.
- Format transform logic (how `.instructions.md` becomes `.mdc`, steering files, etc.) is not extracted here. See `integration/instruction_integrator.py` for the `_convert_to_*` branches.
- The `copilot-cowork` and `copilot-app` targets have dynamic root resolvers (OneDrive path, `~/.copilot/data.db`) that are not portable -- the Go port will need platform-specific implementations.
