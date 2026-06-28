# Research: apm init Interactive Flow (Complete)

- **Query**: Full analysis of Python `apm init` interactive flow, prompts, target selection, generated output, edge cases
- **Scope**: internal
- **Date**: 2026-06-28

## 1. Command Registration and CLI Options

**File**: `D:\Projects\apm-dev\apm\src\apm_cli\commands\init.py` (lines 64-88)

The `init` command is a Click command registered in `cli.py` line 170:
```python
cli.add_command(init)
```

### Click Decorators and Options

```python
@click.command(help="Initialize a new APM project")
@click.argument("project_name", required=False)          # Optional positional arg
@click.option("--yes", "-y", is_flag=True,
    help="Skip interactive prompts and use auto-detected defaults")
@click.option("--plugin", is_flag=True,
    help="(deprecated) Use 'apm plugin init' instead.")
@click.option("--marketplace", "marketplace_flag", is_flag=True,
    help="(deprecated) Use 'apm marketplace init' instead.")
@click.option("--target", "target_flag", type=TargetParamType(), default=None,
    help="Comma-separated target list (skip prompt, write directly)")
@click.option("--verbose", "-v", is_flag=True, help="Show detailed output")
@click.pass_context
def init(ctx, project_name, yes, plugin, marketplace_flag, target_flag, verbose):
```

**Key**: The `TargetParamType` is a custom Click ParamType that validates comma-separated targets (defined in `core/target_detection.py` lines 616-656).

---

## 2. Full Interactive Flow (Step by Step)

The main flow is in `_perform_init()` (lines 124-356). Here is the exact sequence:

### Phase 1: Project Name Resolution (lines 143-164)

1. If `project_name == "."`, treat as `None` (use current dir)
2. If `project_name` is provided and contains `/` or `\` or is `..`, reject with error
3. If `project_name` is provided, create directory and `os.chdir()` into it
4. If `project_name` is `None`, use current directory name

### Phase 2: Existing apm.yml Check (lines 176-189)

If `apm.yml` already exists:
- **Interactive mode**: Shows `"apm.yml already exists"` warning, then prompts:
  ```
  Continue and overwrite? [y/N]:
  ```
  Uses `click.confirm()` (NOT Rich). If user says no, prints `"Initialization cancelled."` and returns.
- **`--yes` mode**: Prints `"--yes specified, overwriting apm.yml..."` and continues.

### Phase 3: Metadata Collection (lines 191-196)

**Interactive mode** calls `_interactive_project_setup()` (lines 359-414):

```
Setting up your APM project...
Press ^C at any time to quit.

Project name [<current-dir-name>]: _
Version [1.0.0]: _
Description [APM project for <name>]: _
Author [<git-user-name>]: _
```

- Uses `rich.prompt.Prompt.ask()` with fallback to `click.prompt()` if Rich is not available
- Project name validation: loops until valid (no `/`, `\`, or `..`)
- Author auto-detected from `git config user.name` (fallback: `"Developer"`)
- Description auto-detected: `"APM project for <name>"`
- Version default: `"1.0.0"`

**`--yes` mode** calls `_get_default_config()` which auto-detects all values without prompting.

### Phase 4: Target Selection (lines 199-208)

Calls `_resolve_init_targets()` (lines 470-530). Priority:

1. `--target` flag provided -> use directly, no prompt
2. Non-interactive (`--yes` or non-TTY) -> auto-detect from filesystem signals, no prompt
3. Interactive (TTY) -> show numbered toggle prompt

#### Target Auto-Detection (Filesystem Signals)

Defined in `core/target_detection.py` `SIGNAL_WHITELIST` (lines 686-702):

| Signal Path | Target |
|---|---|
| `.claude/` (dir) | claude |
| `CLAUDE.md` (file) | claude |
| `.cursor/` (dir) | cursor |
| `.cursorrules` (file) | cursor |
| `.github/copilot-instructions.md` (file) | copilot |
| `.github/instructions/` (dir) | copilot |
| `.github/agents/` (dir) | copilot |
| `.github/prompts/` (dir) | copilot |
| `.github/hooks/` (dir) | copilot |
| `.codex/` (dir) | codex |
| `.gemini/` (dir) | gemini |
| `GEMINI.md` (file) | gemini |
| `.opencode/` (dir) | opencode |
| `.windsurf/` (dir) | windsurf |
| `.kiro/` (dir) | kiro |

#### Re-init: Seeding from Existing apm.yml

When `apm.yml` already exists, targets are read from existing file via `_read_existing_targets()` (lines 533-563):
- Reads `targets:` (plural, list form) first
- Falls back to `target:` (singular/CSV) for backwards compat
- These become pre-checked in the toggle prompt

#### Interactive Target Prompt (lines 604-672)

This is a **numbered toggle list** (NOT a multi-select checkbox widget). The prompt renders like this:

```
Select targets for this project:
  1. [ ] copilot
  2. [ ] claude
  3. [ ] cursor
  4. [ ] opencode
  5. [ ] codex
  6. [ ] gemini
  7. [ ] windsurf
  (no signals detected)

[i] Tip: select the tools your team uses. You can change this later
    with 'apm targets set <target,...>' or edit apm.yml directly.
[i] Type a number to toggle, ranges like '1-3' or '1,3,5' for multiple,
    'all' / 'none' to flip every entry, or press Enter to confirm.

Toggle (1-7, ranges, 'all'/'none', or Enter to confirm): _
```

Display order (constant `_PROMPT_TARGETS_ORDERED`, line 53-61):
```python
["copilot", "claude", "cursor", "opencode", "codex", "gemini", "windsurf"]
```

**Note**: `agent-skills` and `antigravity` are excluded from the prompt (`EXPLICIT_ONLY_TARGETS`).

When signals are detected, items are pre-checked with `[x]` and annotated:
```
  1. [ ] copilot
  2. [x] claude  (detected .claude/)
  3. [x] cursor  (detected .cursor/)
```

#### Toggle Input Parsing (`_parse_toggle_input`, lines 566-601)

Accepts:
- Single number: `3`
- CSV: `1,3,5`
- Range: `1-3`
- Mixed: `1,3-5,7`
- Keywords: `all` (toggle all) / `none` (toggle all -- same as all, acts as toggle)
- Empty string / `done`: confirm and exit the toggle loop

After each toggle, the list re-renders showing updated `[x]`/`[ ]` state.

#### Empty Selection Handling (lines 662-672)

If user confirms with nothing selected:
```
[!] No targets selected. APM will auto-detect targets from your
    filesystem on every compile (e.g. .github/ -> copilot).
    To pin targets later: apm targets set <target,...>

Continue without pinning targets? [Y/n]: _
```

If user says yes -> `None` returned (no `targets` key in apm.yml)
If user says no -> re-enters the target selection prompt (recursive call)

### Phase 5: Confirmation Summary Panel (lines 211-212)

**Interactive mode only**. Calls `_confirm_setup_summary()` (lines 417-453):

Rich panel (or text fallback):
```
+-- About to create --+
| name: my-project     |
| version: 1.0.0       |
| description: ...     |
| author: Developer    |
| targets: claude, ... |
+----------------------+

Is this OK? [Y/n]: _
```

If targets is empty: shows `"(none -- auto-detect at compile time)"`

If user says no -> `sys.exit(0)` with `"Aborted."` message.

### Phase 6: File Generation (lines 218-249)

1. Creates `apm.yml` via `_create_minimal_apm_yml()` (in `_helpers.py`, lines 656-727)
2. If `--plugin`, creates `plugin.json` via `_create_plugin_json()` (in `_helpers.py`, lines 636-653)
3. If `--marketplace`, appends marketplace block to apm.yml

### Phase 7: Success Output (lines 248-352)

```
[*] APM project initialized successfully!

+-- Created Files ---+
| File    | Desc     |
| apm.yml | Project  |
+--------------------+

+--- Next Steps -------+
| * Install a package: apm install <owner>/<repo>  |
| * Run a script:      apm run <script>            |
| * Build a plugin:    apm plugin init              |
| * Publishing?:       apm marketplace init         |
+------------------------------------------------------+

  Docs: https://microsoft.github.io/apm  |  Star: https://github.com/microsoft/apm
```

Additional conditional output:
- **agentrc integration** (lines 296-325): If no agent instructions exist (AGENTS.md, .github/copilot-instructions.md, .github/instructions/), suggests `agentrc init` or shows URL tip
- **Codex tip** (lines 328-333): If `.codex/` dir exists, suggests `--target agent-skills`

---

## 3. Generated apm.yml Structure

From `_create_minimal_apm_yml()` (lines 656-727), the YAML fields in order:

```yaml
name: my-project
version: 1.0.0
description: APM project for my-project
author: Developer

# Which agent platforms to deploy to.
# Resolution order: --target flag > this field > auto-detect from filesystem.
# Accepted values: vscode, agents, copilot, claude, cursor, opencode, codex,
# gemini, antigravity, windsurf, kiro, agent-skills, all
targets:
  - claude
  - copilot

dependencies:
  apm: []
  mcp: []

includes: auto

scripts: {}
```

When NO targets selected, the targets section is commented out:
```yaml
# Which agent platforms to deploy to (uncomment to pin):
# targets:
#   - copilot
#   - claude

dependencies:
  apm: []
  mcp: []
```

When `--plugin` mode: adds `devDependencies: {apm: []}` and uses version `0.1.0` for `--yes` mode.

---

## 4. Libraries Used

| Library | Usage |
|---|---|
| `click` | CLI framework, decorators, prompts (`click.prompt`, `click.confirm`, `click.echo`) |
| `rich` | Pretty output: `rich.console.Console`, `rich.prompt.Prompt`, `rich.prompt.Confirm`, `rich.panel.Panel`, `rich.table.Table` |
| `colorama` | Fallback ANSI color support (`Fore`, `Style`) when Rich unavailable |
| `PyYAML` | YAML parsing/writing (`yaml.safe_load`, `yaml.safe_dump`) |

Rich is optional -- every Rich usage has a `try/except ImportError` fallback to click/colorama.

---

## 5. Non-Interactive Mode (`--yes` / `-y`)

When `--yes` is passed:
1. Metadata: auto-detected (git user name, dir name, default version `1.0.0`)
2. Existing apm.yml: silently overwrites (logs `"--yes specified, overwriting apm.yml..."`)
3. Targets: auto-detected from filesystem signals; if none found, omits `targets` key entirely
4. No confirmation panel shown
5. Plugin version defaults to `0.1.0`

Non-TTY stdin (piped input) also triggers non-interactive target resolution:
- `_stdin_is_tty()` returns False -> skips target prompt
- Emits provenance log: `"Non-interactive stdin: skipping target prompt..."`

---

## 6. Edge Cases

### apm.yml Already Exists
- Interactive: prompts `"Continue and overwrite?"` (click.confirm)
- `--yes`: auto-overwrites
- On re-init, existing targets are read and pre-checked in the toggle

### Project Name Validation
- Rejects `/`, `\`, `..`
- Interactive mode loops until valid name entered
- Non-interactive exits with error code 1

### Plugin Name Validation
- Must match `^[a-z][a-z0-9-]{0,63}$` (kebab-case, starts with letter, max 64 chars)

### Empty Directory
- Works fine -- uses directory name as project name
- No signals detected -> targets section commented out

### `project_name == "."`
- Treated as `None` -- init in current directory

### Deprecated Flags
- `--plugin`: prints deprecation warning to stderr, suggests `apm plugin init`
- `--marketplace`: prints deprecation warning to stderr, suggests `apm marketplace init`

---

## 7. Go Rewrite Comparison (Current State)

**File**: `D:\Projects\apm-dev\apm-go\cmd\apm\main.go` (lines 99-178)

The current Go `initCmd()` is **drastically incomplete**:

| Feature | Python | Go |
|---|---|---|
| Interactive metadata prompts | Yes (name, version, description, author) | No -- flags only |
| `--yes` / `-y` flag | Yes | No |
| Target interactive toggle | Yes (numbered list with toggle) | No -- `--target` flag only |
| Target auto-detection | Yes (filesystem signals) | No |
| Existing apm.yml handling | Interactive confirm or --yes overwrite | `--force` flag (error if exists) |
| Generated fields | name, version, description, author, targets, dependencies, includes, scripts | name, version, target only |
| Description field | Auto-detected + prompted | Missing |
| Author field | Auto-detected from git + prompted | Missing |
| Dependencies section | `{apm: [], mcp: []}` | Missing |
| Includes field | `"auto"` | Missing |
| Scripts field | `{}` | Missing |
| Target comments | Detailed comment headers | Missing |
| Rich/colorama output | Yes (panels, tables, colors) | Plain stderr |
| Next steps panel | Yes (with agentrc integration) | Missing |
| Plugin mode | Yes (plugin.json + devDependencies) | Missing |
| Marketplace mode | Yes (marketplace block appended) | Missing |
| Project name validation | Yes (no `/`, `\`, `..`) | Missing |
| Confirmation summary | Yes (Rich panel) | Missing |
| Version default | Interactive: 1.0.0, Plugin+yes: 0.1.0 | 0.1.0 |

### Go uses `--force` instead of interactive confirm
The Go version returns an error `"apm.yml already exists; use --force to overwrite"` whereas Python uses interactive `click.confirm("Continue and overwrite?")` or silent overwrite with `--yes`.

### Go uses `yamllib.Marshal` directly
The Python version uses `yaml.safe_dump` with specific formatting options (`default_flow_style=False`, `sort_keys=False`, `allow_unicode=True`) and then post-processes to add target comment headers.

---

## 8. Related Files

| File Path | Description |
|---|---|
| `apm\src\apm_cli\commands\init.py` | Main init command implementation |
| `apm\src\apm_cli\commands\_helpers.py` | Shared helpers: `_create_minimal_apm_yml`, `_get_default_config`, `_auto_detect_author`, `_auto_detect_description`, `_validate_project_name`, `_validate_plugin_name`, `_create_plugin_json` |
| `apm\src\apm_cli\cli.py` | Command registration |
| `apm\src\apm_cli\core\target_detection.py` | Target validation, signal detection, `TargetParamType`, `SIGNAL_WHITELIST`, `EXPLICIT_ONLY_TARGETS` |
| `apm\src\apm_cli\utils\console.py` | Rich console helpers, status symbols, `_rich_panel`, `_create_files_table` |
| `apm\src\apm_cli\utils\yaml_io.py` | YAML I/O with UTF-8 |
| `apm\src\apm_cli\constants.py` | `APM_YML_FILENAME = "apm.yml"` |
| `apm\src\apm_cli\commands\plugin\init.py` | Plugin init subcommand (delegates to `_perform_init` with `plugin=True`) |
| `apm\src\apm_cli\marketplace\init_template.py` | Marketplace block template |
| `apm\tests\unit\test_init_command.py` | Tests covering interactive flow, target prompt, edge cases |
| `apm\tests\unit\test_init_command_selection.py` | Tests for toggle parser, resolve targets, confirm summary |

---

## 9. Exact Prompt Text Reference

### Overwrite Confirmation
```
Continue and overwrite? [y/N]:
```

### Interactive Setup Header
```
Setting up your APM project...
Press ^C at any time to quit.
```

### Metadata Prompts (in order)
```
Project name [<default>]:
Version [1.0.0]:
Description [APM project for <name>]:
Author [<git-user-name>]:
```

### Target Selection Header
```
Select targets for this project:
```

### Target List Items
```
  1. [ ] copilot
  2. [ ] claude
  3. [ ] cursor
  4. [ ] opencode
  5. [ ] codex
  6. [ ] gemini
  7. [ ] windsurf
```

With signals:
```
  2. [x] claude  (detected .claude/)
```

### Target Selection Tips
```
[i] Tip: select the tools your team uses. You can change this later
    with 'apm targets set <target,...>' or edit apm.yml directly.
[i] Type a number to toggle, ranges like '1-3' or '1,3,5' for multiple,
    'all' / 'none' to flip every entry, or press Enter to confirm.
```

### Target Toggle Prompt
```
Toggle (1-7, ranges, 'all'/'none', or Enter to confirm):
```

### Empty Selection Warning
```
[!] No targets selected. APM will auto-detect targets from your
    filesystem on every compile (e.g. .github/ -> copilot).
    To pin targets later: apm targets set <target,...>

Continue without pinning targets? [Y/n]:
```

### Confirmation Panel
```
+--- About to create ---+
| name: my-project       |
| version: 1.0.0         |
| description: ...       |
| author: Developer      |
| targets: claude, ...   |
+------------------------+

Is this OK? [Y/n]:
```

### Success
```
[*] APM project initialized successfully!
```

### Abort
```
Aborted.
```

### Initialization Cancelled (overwrite declined)
```
Initialization cancelled.
```
