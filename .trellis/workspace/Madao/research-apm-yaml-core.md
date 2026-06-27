# Research: microsoft/apm YAML Core for Go Rewrite

- **Query**: Understand the YAML core of microsoft/apm for a Go rewrite
- **Scope**: Internal (source code analysis at D:\Projects\apm-dev\apm)
- **Date**: 2026-06-27

---

## 1. Project Overview

**What is microsoft/apm?**
APM (Agent Package Manager) is an open-source, community-driven dependency manager for AI agents. It is the equivalent of npm/pip/cargo but for AI agent configuration -- managing skills, prompts, instructions, hooks, plugins, and MCP servers across multiple AI coding agents (Claude Code, GitHub Copilot, Cursor, OpenCode, Codex, Gemini, Windsurf, Kiro).

**Language**: Python 3.10+ (package name: `apm-cli`, version 0.21.0)

**Key Dependencies**:
- `pyyaml>=6.0.0` -- primary YAML parsing (yaml.safe_load / yaml.safe_dump)
- `ruamel.yaml>=0.18.0` -- used in marketplace YAML editing (comment-preserving round-trip)
- `click>=8.0.0` -- CLI framework
- `rich>=13.0.0` / `rich-click>=1.7.0` -- CLI output formatting
- `python-frontmatter>=1.0.0` -- SKILL.md frontmatter parsing
- `GitPython>=3.1.0` -- git operations

**Entry point**: `apm_cli.cli:main` (Click-based CLI)

---

## 2. YAML File Formats

APM uses three primary YAML file types:

### 2.1. apm.yml (Project Manifest)

The central manifest file. Location: project root.
Filename constant: `APM_YML_FILENAME = "apm.yml"` (src/apm_cli/constants.py:21)

**Full schema (all fields)**:

```yaml
# REQUIRED
name: string                    # Package name
version: string                 # Semver version (e.g., "1.0.0")

# OPTIONAL metadata
description: string | null      # Package description
author: string | null           # Author name
license: string | null          # SPDX license expression (e.g., "MIT")

# OPTIONAL target selection (MUTEX: cannot have both target and targets)
target: string | null           # Singular: "claude" or CSV: "claude,copilot"
targets: list[string] | null    # Plural canonical form: ["claude", "copilot"]
# Valid targets: claude, copilot, cursor, opencode, codex, gemini, windsurf, kiro, agent-skills

# OPTIONAL package type
type: string | null             # "instructions" | "skill" | "hybrid" | "prompts"

# OPTIONAL includes (auto-publish opt-in)
includes: "auto" | list[string] | null

# OPTIONAL dependencies
dependencies:
  apm:                          # APM package dependencies (list)
    # String form (shorthand):
    - owner/repo
    - owner/repo#v1.0.0
    - owner/repo#branch-name
    - owner/repo/skills/my-skill           # virtual subdirectory
    - owner/repo/prompts/file.prompt.md    # virtual file
    - ./packages/local-pkg                 # local path
    - gitlab.com/group/repo
    - git@host:owner/repo.git
    - https://gitlab.com/owner/repo.git
    - ssh://git@host:7999/owner/repo.git

    # Object form (dict):
    - git: https://gitlab.com/acme/coding-standards.git
      path: instructions/security          # sub-path within repo
      ref: v2.0                            # git reference override
      alias: my-alias                      # custom install directory name
      type: gitlab                         # host kind hint (currently only "gitlab")
      allow_insecure: false                # allow http:// (default false)
      skills: [skill-a, skill-b]           # SKILL_BUNDLE subset selection

    # Object form (local path):
    - path: ./packages/my-shared-skills

    # Object form (monorepo parent inheritance):
    - git: parent
      path: prompts/review.prompt.md

    # Object form (marketplace):
    - name: gopls-lsp
      marketplace: claude-plugins-official
      version: "~2.1.0"                    # optional semver range

    # Object form (registry):
    - id: owner/repo
      version: "^1.2.0"
      registry: corp-main                  # optional, uses default if omitted
      path: prompts/foo.md                 # optional virtual sub-path
      alias: my-name                       # optional

  mcp:                          # MCP server dependencies (list)
    # String form (registry reference):
    - io.github.github/github-mcp-server

    # Object form:
    - name: io.github.github/github-mcp-server
      transport: http            # "stdio" | "sse" | "streamable-http" | "http"
      env:                       # environment variable overrides
        GITHUB_TOKEN: "..."
      args: {}                   # dict for overlays, list for self-defined args
      version: "1.0.0"           # pin specific version
      registry: false            # false = self-defined, string = custom registry URL
      package: npm               # "npm" | "pypi" | "oci"
      headers:                   # custom HTTP headers
        Authorization: "Bearer ..."
      tools: ["*"]               # restrict exposed tools
      url: https://...           # required for self-defined http/sse
      command: npx               # required for self-defined stdio
      extra: {}                  # harness-specific passthrough keys

  lsp:                          # LSP server dependencies (list)
    - name: gopls
      command: gopls
      args: ["-remote=auto"]
      extensionToLanguage:       # or extension_to_language (snake_case alias)
        ".go": go
      transport: stdio           # "stdio" | "socket"
      env: {}
      initializationOptions: {}
      settings: {}
      workspaceFolder: "."
      startupTimeout: 30
      shutdownTimeout: 10
      restartOnCrash: true
      maxRestarts: 3

# OPTIONAL dev dependencies (same structure as dependencies)
devDependencies:
  apm: [...]
  mcp: [...]
  lsp: [...]

# OPTIONAL scripts
scripts:
  test: "go test ./..."
  build: "go build ./..."

# OPTIONAL registries block
registries:
  corp-main:
    url: https://registry.corp.example.com/apm
  corp-other:
    url: https://other.example.com/apm
  default: corp-main             # optional: routes unscoped deps here

# OPTIONAL executable approval gate (npm v12-style)
allowExecutables:
  owner/repo#v1.0:
    hooks: true
```

### 2.2. apm.lock.yaml (Lockfile)

Pins resolved dependency tree for reproducible installs.
Filename constant: `APM_LOCK_FILENAME = "apm.lock"` (but actual file is `apm.lock.yaml`)

```yaml
lockfile_version: "1"           # or "2" when registry/semver resolution present
generated_at: "2026-06-16T23:58:41.693187+00:00"  # ISO 8601
apm_version: "0.20.0"

dependencies:                   # list of LockedDependency entries
  - repo_url: string            # REQUIRED: e.g., "owner/repo" or "_local/pkg-name"
    host: string | null         # e.g., "github.com", "gitlab.com"
    host_type: string | null    # currently only "gitlab"
    port: int | null            # non-standard SSH/HTTPS port (1-65535)
    registry_prefix: string | null  # e.g., "artifactory/github"
    resolved_commit: string | null  # exact SHA
    resolved_ref: string | null     # branch/tag name
    version: string | null          # registry version
    virtual_path: string | null     # sub-path for virtual packages
    is_virtual: bool                # default false
    depth: int                      # default 1 (1 = direct, 2+ = transitive)
    resolved_by: string | null      # parent's repo_url
    package_type: string | null     # "apm_package", "claude_skill", "hybrid", etc.
    deployed_files: list[string]    # sorted, deduped file paths deployed
    deployed_file_hashes:           # map: relative_path -> "sha256:<hex>"
      ".agents/skills/foo/SKILL.md": "sha256:abcd..."
    source: string | null           # "local", "registry", or null (=git)
    local_path: string | null       # e.g., "./packages/foo" or "../sibling"
    content_hash: string | null     # SHA-256 of package file tree
    is_dev: bool                    # default false
    discovered_via: string | null   # marketplace name (provenance)
    marketplace_plugin_name: string | null
    source_url: string | null       # canonical marketplace source URL
    source_digest: string | null    # sha256 of marketplace manifest
    is_insecure: bool               # default false
    allow_insecure: bool            # default false
    skill_subset: list[string]      # sorted skill names for SKILL_BUNDLE
    # v2 registry fields:
    resolved_url: string | null     # download URL for registry packages
    resolved_hash: string | null    # sha256 trust anchor
    # v2 git-semver fields:
    constraint: string | null       # original semver range (e.g., "^1.2.0")
    resolved_tag: string | null     # concrete tag resolved
    resolved_at: string | null      # ISO timestamp of resolution
    declared_license: string | null # SPDX expression from manifest

# Flat sections (top-level, not per-dependency):
mcp_servers: list[string]           # sorted server names
mcp_configs:                        # server name -> config dict
  server-name:
    transport: http
    url: https://...
lsp_servers: list[string]
lsp_configs:
  server-name: {...}
local_deployed_files: list[string]  # files deployed from local (non-dep) content
local_deployed_file_hashes:         # map: path -> sha256
```

### 2.3. apm-policy.yml (Policy File)

Governance file for org-wide controls (not detailed here since it's separate from core YAML parsing).

---

## 3. Core Data Model

### 3.1. APMPackage (src/apm_cli/models/apm_package.py:224)

The central dataclass representing a parsed apm.yml:

```python
@dataclass
class APMPackage:
    name: str                                          # REQUIRED
    version: str                                       # REQUIRED
    description: str | None = None
    author: str | None = None
    license: str | None = None
    source: str | None = None                          # source location (for deps)
    resolved_commit: str | None = None                 # resolved commit SHA
    dependencies: dict[str, list[DependencyReference | str | dict]] | None = None
    dev_dependencies: dict[str, list[DependencyReference | str | dict]] | None = None
    scripts: dict[str, str] | None = None
    package_path: Path | None = None                   # path to package dir
    source_path: Path | None = None                    # anchor dir for relative deps
    target: str | list[str] | None = None              # singular target field
    targets: list[str] | None = None                   # plural targets field
    type: PackageContentType | None = None             # instructions/skill/hybrid/prompts
    includes: str | list[str] | None = None            # "auto" or list of paths
    registries: dict[str, str] | None = None           # name -> base URL
    default_registry: str | None = None
    allow_executables: dict[str, dict[str, bool]] | None = None
```

Key methods:
- `from_apm_yml(apm_yml_path, source_path)` -- classmethod, loads and caches
- `_parse_dependency_dict(raw_deps, label)` -- parses deps/devDeps sections
- `get_apm_dependencies()` -> list[DependencyReference]
- `get_mcp_dependencies()` -> list[MCPDependency]
- `get_lsp_dependencies()` -> list[LSPDependency]
- `get_dev_apm_dependencies()` -> list[DependencyReference]

### 3.2. DependencyReference (src/apm_cli/models/dependency/reference.py:49)

Represents a parsed APM dependency entry:

```python
@dataclass
class DependencyReference:
    repo_url: str                          # e.g., "user/repo"
    host: str | None = None                # e.g., "github.com"
    host_type: str | None = None           # "gitlab" or None
    port: int | None = None                # non-standard port
    explicit_scheme: str | None = None     # "ssh", "https", "http", or None
    reference: str | None = None           # branch/tag/SHA (e.g., "main", "v1.0.0")
    alias: str | None = None               # custom install directory name
    virtual_path: str | None = None        # sub-path for virtual packages
    is_virtual: bool = False
    ado_organization: str | None = None    # Azure DevOps org
    ado_project: str | None = None         # Azure DevOps project
    ado_repo: str | None = None            # Azure DevOps repo
    is_local: bool = False                 # local filesystem dep
    local_path: str | None = None          # e.g., "./packages/my-pkg"
    is_parent_repo_inheritance: bool = False
    artifactory_prefix: str | None = None
    is_insecure: bool = False
    allow_insecure: bool = False
    skill_subset: list[str] | None = None  # sorted skill names
    ssh_user: str | None = None            # SSH username (default "git")
    source: str | None = None              # "git", "registry", "local", or None
    registry_name: str | None = None
    is_marketplace: bool = False
    marketplace_name: str | None = None
    marketplace_plugin_name: str | None = None
    marketplace_version_spec: str | None = None
```

Key methods:
- `parse(dependency_str)` -- classmethod, parses ANY string format
- `parse_from_dict(entry)` -- classmethod, parses object-form dict
- `to_canonical()` -> str -- scheme-free identity string
- `get_identity()` -> str -- identity without ref/alias
- `get_unique_key()` -> str -- dedup key
- `to_apm_yml_entry()` -> str | dict -- for serialization back to apm.yml
- `to_github_url()` -> str -- full HTTPS URL
- `is_local_path(dep_str)` -- static, detects "./", "../", "/", "~/", "C:\"

### 3.3. MCPDependency (src/apm_cli/models/dependency/mcp.py:43)

```python
@dataclass
class MCPDependency:
    name: str                              # REQUIRED, regex validated
    transport: str | None = None           # "stdio" | "sse" | "streamable-http" | "http"
    env: dict[str, str] | None = None
    args: Any | None = None                # dict (overlay) or list (positional)
    version: str | None = None
    registry: Any | None = None            # None=default, False=self-defined, str=custom URL
    package: str | None = None             # "npm" | "pypi" | "oci"
    headers: dict[str, str] | None = None
    tools: list[str] | None = None
    url: str | None = None
    command: str | None = None
    extra: dict[str, Any] | None = None    # passthrough keys
```

Key methods:
- `from_string(s)` -- registry reference
- `from_dict(d)` -- full object parsing with legacy `type` -> `transport` mapping
- `to_dict()` -- serialization (extra keys merged at top level)
- `validate(strict)` -- validates name regex, URL scheme, CRLF in headers, command path

### 3.4. LSPDependency (src/apm_cli/models/dependency/lsp.py:18)

```python
@dataclass
class LSPDependency:
    name: str                              # REQUIRED
    command: str | None = None
    args: list[str] | None = None
    extension_to_language: dict[str, str] | None = None
    transport: str | None = None           # "stdio" | "socket"
    env: dict[str, str] | None = None
    initialization_options: Any | None = None
    settings: Any | None = None
    workspace_folder: str | None = None
    startup_timeout: int | None = None
    shutdown_timeout: int | None = None
    restart_on_crash: bool | None = None
    max_restarts: int | None = None
```

### 3.5. LockedDependency (src/apm_cli/deps/lockfile.py:48)

```python
@dataclass
class LockedDependency:
    repo_url: str                          # REQUIRED
    host: str | None = None
    host_type: str | None = None
    port: int | None = None
    registry_prefix: str | None = None
    resolved_commit: str | None = None
    resolved_ref: str | None = None
    version: str | None = None
    virtual_path: str | None = None
    is_virtual: bool = False
    depth: int = 1
    resolved_by: str | None = None
    package_type: str | None = None
    deployed_files: list[str] = field(default_factory=list)
    deployed_file_hashes: dict[str, str] = field(default_factory=dict)
    source: str | None = None
    local_path: str | None = None
    content_hash: str | None = None
    is_dev: bool = False
    discovered_via: str | None = None
    marketplace_plugin_name: str | None = None
    source_url: str | None = None
    source_digest: str | None = None
    is_insecure: bool = False
    allow_insecure: bool = False
    skill_subset: list[str] = field(default_factory=list)
    resolved_url: str | None = None
    resolved_hash: str | None = None
    constraint: str | None = None
    resolved_tag: str | None = None
    resolved_at: str | None = None
    declared_license: str | None = None
    _unknown_fields: dict[str, Any] = field(default_factory=dict)  # forward compat
```

### 3.6. LockFile (src/apm_cli/deps/lockfile.py:476)

```python
@dataclass
class LockFile:
    lockfile_version: str = "1"
    generated_at: str = ...                # ISO 8601 UTC
    apm_version: str | None = None
    dependencies: dict[str, LockedDependency] = {}  # key = unique_key
    mcp_servers: list[str] = []
    mcp_configs: dict[str, dict] = {}
    lsp_servers: list[str] = []
    lsp_configs: dict[str, dict] = {}
    local_deployed_files: list[str] = []
    local_deployed_file_hashes: dict[str, str] = {}
```

Key methods:
- `from_yaml(yaml_str)` -- classmethod, deserializes YAML string
- `to_yaml()` -> str -- serializes to YAML string
- `write(path)` / `read(path)` -- file I/O
- `add_dependency(dep)` -- auto-promotes lockfile_version to "2" when needed

### 3.7. Supporting Enums

```python
class PackageType(Enum):            # src/apm_cli/models/validation.py:17
    APM_PACKAGE = "apm_package"
    CLAUDE_SKILL = "claude_skill"
    HOOK_PACKAGE = "hook_package"
    HYBRID = "hybrid"
    MARKETPLACE_PLUGIN = "marketplace_plugin"
    SKILL_BUNDLE = "skill_bundle"
    INVALID = "invalid"

class PackageContentType(Enum):     # src/apm_cli/models/validation.py:33
    INSTRUCTIONS = "instructions"
    SKILL = "skill"
    HYBRID = "hybrid"
    PROMPTS = "prompts"

class GitReferenceType(Enum):       # src/apm_cli/models/dependency/types.py:8
    BRANCH = "branch"
    TAG = "tag"
    COMMIT = "commit"

class VirtualPackageType(Enum):     # src/apm_cli/models/dependency/types.py:26
    FILE = "file"
    SUBDIRECTORY = "subdirectory"

class InstallMode(Enum):            # src/apm_cli/constants.py:10
    ALL = "all"
    APM = "apm"
    MCP = "mcp"
```

### 3.8. Canonical Targets

```python
CANONICAL_TARGETS = frozenset({     # src/apm_cli/core/apm_yml.py:25
    "claude", "copilot", "cursor", "opencode",
    "codex", "gemini", "windsurf", "kiro", "agent-skills",
})
```

---

## 4. YAML Parsing Logic

### 4.1. YAML I/O Layer (src/apm_cli/utils/yaml_io.py)

All YAML file operations go through this module for consistent UTF-8 encoding:

```python
# Reading
def load_yaml(path) -> dict | None:
    with open(path, encoding="utf-8") as fh:
        return yaml.safe_load(fh)

# Writing
def dump_yaml(data, path, *, sort_keys=False):
    with open(path, "w", encoding="utf-8") as fh:
        yaml.safe_dump(data, fh, default_flow_style=False, sort_keys=False, allow_unicode=True)

# String serialization
def yaml_to_str(data, *, sort_keys=False) -> str:
    return yaml.safe_dump(data, default_flow_style=False, sort_keys=False, allow_unicode=True)

# Atomic file write
def write_yaml_text_atomic(path, content, *, tmp_suffix=".tmp"):
    # Writes to sibling tmp file then os.replace() for atomicity
```

YAML library: PyYAML (`yaml.safe_load` / `yaml.safe_dump`), NOT ruamel.yaml for core operations. ruamel.yaml is only used in marketplace YAML editing (comment-preserving round-trip).

### 4.2. apm.yml Parsing Flow

`APMPackage.from_apm_yml(apm_yml_path, source_path)`:

1. **Cache check**: keyed by `(resolved_path, resolved_source_path)`
2. **Load YAML**: `load_yaml(apm_yml_path)` -> dict via `yaml.safe_load`
3. **Type check**: data must be dict
4. **Required fields**: `name` and `version` must be present
5. **Parse registries block**: `_parse_registries_block(data, path)` validates
6. **Parse dependencies**: `_parse_dependency_dict(raw_deps)`:
   - For `apm:` list entries:
     - String -> `DependencyReference.parse(dep_entry)`
     - Dict -> `DependencyReference.parse_from_dict(dep_entry)`
   - For `mcp:` list entries:
     - String -> `MCPDependency.from_string(dep)`
     - Dict -> `MCPDependency.from_dict(dep)`
   - For `lsp:` list entries:
     - String -> `LSPDependency.from_string(dep)`
     - Dict -> `LSPDependency.from_dict(dep)`
7. **Parse devDependencies**: same structure as dependencies
8. **Resolve effective registries**: merges apm.yml + policy + config.json
9. **Route unscoped deps to default registry**: if configured
10. **Parse allowExecutables**: npm v12-style approval gate
11. **Parse type field**: validates against PackageContentType enum
12. **Parse includes**: "auto" or list of strings
13. **Parse target/targets**: validates against CANONICAL_TARGETS, mutex check
14. **Construct APMPackage**: all fields populated
15. **Cache result**: store in `_apm_yml_cache`

### 4.3. DependencyReference.parse() Flow

Handles all string dependency formats:
1. Empty/control character check
2. Local path detection (`./`, `../`, `/`, `~/`, `C:\`)
3. Protocol-relative URL rejection (`//`)
4. Shorthand alias rejection (`@alias` syntax retired)
5. Embedded subpath guard (e.g., `git@host:org/repo/skills/foo.git`)
6. Virtual package detection (file extensions, subdirectory paths)
7. SSH parsing:
   a. `ssh://` protocol URL (preserves port)
   b. SCP shorthand (`git@host:owner/repo.git`)
8. HTTPS/HTTP/shorthand parsing
9. Final validation (ADO fields, character validation)

### 4.4. Lockfile Parsing

`LockFile.from_yaml(yaml_str)`:
1. `yaml.safe_load(yaml_str)` -> dict
2. Extract top-level fields (lockfile_version, generated_at, apm_version)
3. For each entry in `dependencies` list: `LockedDependency.from_dict(dep_data)`
4. Extract mcp_servers, mcp_configs, lsp_servers, lsp_configs
5. Extract local_deployed_files, local_deployed_file_hashes
6. Synthesize virtual self-entry (key ".") for local content

`LockedDependency.from_dict(data)`:
- Handles backward compat: `deployed_skills` -> `deployed_files` migration
- Validates port (1-65535), host_type (only "gitlab")
- Preserves unknown fields for forward compatibility

---

## 5. Validation Rules and Constraints

### 5.1. apm.yml Validation
- `name`: REQUIRED, string
- `version`: REQUIRED, string, warning if not semver (x.y.z)
- `dependencies`: must be dict with `apm:` / `mcp:` / `lsp:` keys, each a list
- `target` and `targets`: MUTUALLY EXCLUSIVE (ConflictingTargetsError)
- `targets`: non-empty list if present (EmptyTargetsListError)
- Each target token: must be in CANONICAL_TARGETS (UnknownTargetError)
- `type`: must be one of "instructions", "skill", "hybrid", "prompts"
- `includes`: must be "auto" or list of strings
- `registries`: must be dict, each entry must have `url:` (https/http), no `token:` allowed

### 5.2. DependencyReference Validation
- Name regex for MCP: `^[a-zA-Z0-9@_][a-zA-Z0-9._@/:=-]{0,127}$`
- Alias: `^[a-zA-Z0-9._-]+$`
- Path segments: no `..` traversal allowed (PathTraversalError)
- SSH user: validated against allowlist
- Port: 1-65535
- URL schemes: only http/https for MCP URLs
- No CRLF in HTTP headers
- No embedded subpaths in git URLs (e.g., `git@host:org/repo/skills/name.git`)
- Virtual file extensions: `.prompt.md`, `.instructions.md`, `.chatmode.md`, `.agent.md`
- Removed extensions rejected: `.collection.yml`, `.collection.yaml`

### 5.3. Package Type Detection Cascade
1. MARKETPLACE_PLUGIN: plugin.json OR .claude-plugin/ directory
2. HYBRID: root SKILL.md AND apm.yml
3. CLAUDE_SKILL: root SKILL.md only
4. SKILL_BUNDLE: nested skills/<x>/SKILL.md
5. APM_PACKAGE: apm.yml with .apm/ or declared deps
6. HOOK_PACKAGE: hooks/*.json only
7. INVALID: nothing recognizable

---

## 6. Package/Directory Structure

### 6.1. Source Tree (src/apm_cli/)

```
src/apm_cli/
  cli.py                    # Click CLI entry point, command registration
  config.py                 # ~/.apm/config.json management
  constants.py              # File/dir name constants, enums
  factory.py                # Client/PackageManager factory
  drift.py                  # Drift detection logic

  core/
    apm_yml.py              # Target field parsing (parse_targets_field)
    auth.py                 # Authentication/token management
    build_orchestrator.py   # Plugin manifest building
    errors.py               # Target resolution error hierarchy
    operations.py           # Core install/uninstall operations
    scope.py                # Dependency scope management
    target_detection.py     # Harness auto-detection

  models/
    __init__.py             # Re-exports all model types
    apm_package.py          # APMPackage, PackageInfo dataclasses
    validation.py           # PackageType, validate_apm_package()
    format_detection.py     # Package format detection
    results.py              # InstallResult, PrimitiveCounts
    dependency/
      __init__.py           # Re-exports
      reference.py          # DependencyReference (2100 lines, core parsing)
      mcp.py                # MCPDependency
      lsp.py                # LSPDependency
      types.py              # GitReferenceType, RemoteRef, ResolvedReference
      identity.py           # Canonical string/unique key builders

  deps/
    lockfile.py             # LockFile, LockedDependency
    verifier.py             # Dependency verification
    apm_resolver.py         # APM dependency resolver
    dependency_graph.py     # Dependency graph
    clone_engine.py         # Git clone operations
    path_anchoring.py       # Relative path resolution for transitive deps
    plugin_parser.py        # Plugin directory normalization
    registry/               # Package registry support
    ...

  commands/
    init.py                 # `apm init` -- scaffold apm.yml
    install.py              # `apm install`
    lock.py                 # `apm lock`
    update.py               # `apm update`
    audit.py                # `apm audit`
    compile/                # `apm compile` -- output to target format
    uninstall/              # `apm uninstall`
    _apm_yml_writer.py      # Write-back for skill subset in apm.yml
    _helpers.py             # Shared command helpers
    ...

  utils/
    yaml_io.py              # UTF-8 YAML I/O (load_yaml, dump_yaml, yaml_to_str)
    github_host.py          # Host detection/validation
    path_security.py        # Path traversal prevention
    ...

  install/                  # Install pipeline phases
  integration/              # Target-specific integrators
  compilation/              # Compile/output logic
  security/                 # Security scanning
  policy/                   # Policy enforcement
  bundle/                   # Pack/unpack operations
  cache/                    # Git/HTTP caching
  export/                   # SBOM export (CycloneDX, SPDX)
  marketplace/              # Marketplace operations
```

### 6.2. CLI Commands

From cli.py imports (all commands):

| Command | Module | Description |
|---|---|---|
| `apm init` | commands/init.py | Scaffold new apm.yml |
| `apm install` | commands/install.py | Install from apm.yml |
| `apm uninstall` | commands/uninstall/ | Remove dependencies |
| `apm lock` | commands/lock.py | Resolve & write lockfile |
| `apm update` | commands/update.py | Refresh refs & rewrite lock |
| `apm outdated` | commands/outdated.py | Show drifted deps |
| `apm audit` | commands/audit.py | Validate lockfile integrity |
| `apm compile` | commands/compile/ | Output to target format |
| `apm doctor` | commands/doctor.py | Diagnose env problems |
| `apm deps` | commands/deps/ | Dependency tree commands |
| `apm find` | commands/find.py | Find installed packages |
| `apm view` | commands/view.py | View package info |
| `apm list` | commands/list_cmd.py | List installed packages |
| `apm cache` | commands/cache.py | Cache management |
| `apm config` | commands/config.py | Configuration management |
| `apm targets` | commands/targets.py | Show supported targets |
| `apm run` | commands/run.py | Execute scripts |
| `apm prune` | commands/prune.py | Remove orphaned packages |
| `apm pack` | commands/pack.py | Bundle package |
| `apm publish` | commands/publish.py | Publish package |
| `apm mcp` | commands/mcp.py | MCP server management |
| `apm runtime` | commands/runtime.py | Runtime management |
| `apm policy` | commands/policy.py | Policy management |
| `apm marketplace` | commands/marketplace/ | Marketplace operations |
| `apm plugin` | commands/plugin/ | Plugin management |
| `apm approve`/`deny` | commands/approve.py | Executable approval |
| `apm self-update` | commands/self_update.py | Self-update |
| `apm experimental` | commands/experimental.py | Feature flags |
| `apm preview` | commands/run.py | Preview compiled output |

---

## 7. Error Handling Patterns

### 7.1. Error Hierarchy (core/errors.py)
All target resolution errors inherit from `click.UsageError` (exit code 2):
- `TargetResolutionError` (base)
  - `NoHarnessError`
  - `AmbiguousHarnessError`
  - `UnknownTargetError`
  - `ConflictingTargetsError`
  - `EmptyTargetsListError`

### 7.2. Validation Errors
- `ValueError` raised for invalid YAML format, missing fields, bad dependency strings
- `FileNotFoundError` for missing apm.yml
- `PathTraversalError` (custom) for `..` in paths
- `InvalidVirtualPackageExtensionError` for bad virtual file extensions
- `InvalidSemverRangeError` for malformed semver constraints

### 7.3. Error Message Style
Three-section structure:
1. Headline (what APM saw) -- prefixed with `[x]`
2. Actionable commands (3+ lines starting with `apm `)
3. apm.yml snippet showing correct usage

---

## 8. Key Design Decisions for Go Rewrite

### 8.1. YAML Library Choice
- Python uses PyYAML `safe_load`/`safe_dump` for all core operations
- Go equivalent: `gopkg.in/yaml.v3` (supports safe loading by default)
- ruamel.yaml only needed for comment-preserving marketplace edits (can defer)

### 8.2. Caching
- `APMPackage.from_apm_yml()` uses a module-level cache keyed by `(resolved_path, resolved_source_path)`
- Cache prevents re-parsing the same file; `clear_apm_yml_cache()` for test isolation
- Go: consider `sync.Map` or similar for caching

### 8.3. Dependency String Parsing Complexity
- `DependencyReference.parse()` is the most complex parser (~2100 lines in reference.py)
- Handles: shorthand, FQDN, SSH SCP, ssh://, https://, http://, local paths, Azure DevOps, GitLab subgroups, Artifactory VCS, virtual packages
- This is the highest-risk area for the Go rewrite

### 8.4. Forward Compatibility
- `LockedDependency._unknown_fields` preserves unrecognized keys through round-trips
- Lockfile version auto-promotes from "1" to "2" based on content
- Go: use map[string]interface{} for unknown fields

### 8.5. Security Boundaries
- Path traversal prevention via `validate_path_segments()` (rejects `..`)
- `ensure_path_within()` for install path containment
- URL scheme validation (only http/https for MCP)
- CRLF injection prevention in HTTP headers
- SSH username validation against allowlist
- No tokens in apm.yml (must use env vars or config)

---

## Caveats / Not Found

- The `templates/` directory does not exist in the project (apm init generates scaffolds programmatically)
- Policy file parsing (`apm-policy.yml`) was not deeply investigated (separate concern from core YAML)
- Marketplace YAML editing uses `ruamel.yaml` for comment preservation but this is a secondary concern
- The project has 2300+ files total; this research focused on the YAML core (models, parsing, serialization, validation)
