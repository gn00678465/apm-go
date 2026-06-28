# Phase 1 — Technical Design

## Key Architectural Decision: `validate` document-type dispatch

### Problem

The conformance runner calls `apm validate <path>` with no type hint. In Phase 0 this was fine — safe-subset is type-agnostic. Phase 1 breaks it: manifest validation requires `name` (mf-002), but lockfile fixtures have no `name` and must still be accepted as valid lockfiles.

### Decision: default-to-manifest, content-sniff lockfile

When `apm validate <file>` is called:

1. `SafeLoad(data)` — Phase 0 safe-subset check (always runs)
2. Content-sniff: if top-level mapping has key `lockfile_version` → treat as lockfile (Phase 3 validation, stub for now)
3. Otherwise → treat as manifest → run manifest validation

Why not filename-based dispatch: the spec says manifest is `apm.yml` and lockfile is `apm.lock.yaml`, but the oracle deliberately uses arbitrary fixture filenames. Path-sniffing the `manifest/` parent directory would be overfitting to the oracle layout (ADR-0002 anti-cheat).

Why not policy: policy files have a distinct shape (`enforcement`, `extends`, `allow`, `deny`) but there's no single required key. Defer to Phase 6.

Phase 1 consequences:
- Phase 1 `invalid-*.yml` manifest fixtures → correctly rejected
- Lockfile fixtures (`v1-git-only.yml`, etc.) → detected by `lockfile_version` → skip manifest validation → accepted
- Policy fixtures → fall through to manifest validation → may show expected reds (same discipline as Phase 0)

## Package Layout

```
internal/
  yamlcore/          # Phase 0 (exists)
    safe.go
    vendor_ext.go

  manifest/          # Phase 1 (new)
    manifest.go      # Manifest struct + ParseManifest
    manifest_test.go
    target.go        # Target vocabulary, validation, alias normalization
    target_test.go
    validate.go      # ValidateManifest — field-level checks
    validate_test.go

cmd/
  apm/
    main.go          # Updated: validate dispatch + init command
```

## Core Types

### `manifest.Manifest`

```go
type Manifest struct {
    Name         string
    Version      string
    Description  string
    Author       string
    License      string
    DefaultHost  string
    Target       []string            // normalized from string or list; spec only defines `target` (no plural `targets:`)
    Type         string              // "instructions"/"skill"/"hybrid"/"prompts"
    Scripts      map[string]string
    Includes     any                 // "auto" or []string
    Registries   map[string]Registry
    Dependencies DependencyBlock     // stub for Phase 1C
    DevDeps      DependencyBlock     // stub
    Workspaces   bool                // true if key present (for mf-021 diagnostic)

    // Round-trip support
    node         *yaml.Node          // original validated Node (unexported)
}

type Registry struct {
    URL      string
    Insecure bool
    Aliases  []string
    Extra    map[string]any // x-* keys
}

type DependencyBlock struct {
    APM  []any // raw entries, parsed in Phase 1C
    MCP  []any // raw entries, parsed in Phase 1D
}
```

### `manifest.ParseManifest`

```go
func ParseManifest(node *yaml.Node) (*Manifest, []Diagnostic, error)
```

- Takes a validated `*yaml.Node` from `SafeLoad`
- Returns `(*Manifest, diagnostics, error)`
- Hard errors (MUST violations) → error return
- Soft warnings (SHOULD, mf-004/mf-021) → diagnostics slice + nil error
- Walks the Node tree extracting fields by key name
- Unknown top-level keys are preserved in the Node (Phase 0 guarantee)

### `manifest.ValidateManifest`

```go
func ValidateManifest(m *Manifest) []Diagnostic
```

Post-parse validation for cross-field constraints.

## Target Logic (mf-005 + tg-004)

### Vocabulary table

```go
var CanonicalTargets = map[string]bool{
    "copilot": true, "claude": true, "cursor": true,
    "codex": true, "gemini": true, "opencode": true,
    "windsurf": true, "agent-skills": true, "all": true,
    "antigravity": true, // pre-standard, tracking #1650
}

var TargetAliases = map[string]string{
    "vscode": "copilot",
    "agents": "copilot",
}

var SupportedTargets = []string{
    "claude", "codex", "copilot", "opencode", "antigravity",
}
```

### Parse-time target validation (mf-005)

1. If `"minimal"` → reject: "target 'minimal' must not be set explicitly"
2. If in `TargetAliases` → normalize (e.g., `vscode` → `copilot`)
3. If in `CanonicalTargets` → accept
4. If matches `x-[a-z][a-z0-9-]*-[a-z][a-z0-9-]*` → accept (tg-004 vendor extension)
5. Otherwise → reject: "unknown target %q"

### Deploy-time target routing (tg-004)

For targets without a registered adapter (gemini, cursor, windsurf, and unregistered x-vendor):
- Emit diagnostic: "no registered handler for target %q"
- NOT silent ignore, NOT crash

This is informational at parse time; actual deploy routing happens in Phase 4.

## `apm init` Command

```
apm init [--name NAME] [--version VERSION] [--target TARGET]
```

1. `--name` defaults to current directory basename
2. `--version` defaults to `"0.1.0"`
3. `--target` accepts only `SupportedTargets` (claude/codex/copilot/opencode/antigravity); can be repeated; omitting produces no `target:` key (NOT `minimal`)
4. Build `Manifest` struct → serialize via `SafeDump`
5. Write to `apm.yml` in current directory (refuse if already exists, unless `--force`)

Non-interactive path required for testing. Test: pipe `init` output through `SafeLoad` + `ParseManifest` — must pass.

## `validate` Command Update

Phase 0's validate only ran `SafeLoad`. Phase 1 adds manifest validation:

```go
func runValidate(path string) error {
    data, _ := os.ReadFile(path)
    node, err := yamlcore.SafeLoad(data)  // Phase 0
    if err != nil { return err }

    // mf-001: top-level must be mapping
    // SafeLoad returns DocumentNode; the actual mapping is node.Content[0]
    root := node.Content[0]
    if root.Kind != yaml.MappingNode {
        return fmt.Errorf("%s: top-level must be a YAML mapping", path)
    }

    // Content-sniff: lockfile? (check for lockfile_version key in root mapping)
    if nodeHasKey(root, "lockfile_version") {
        return nil  // stub: lockfile validation deferred to Phase 3
    }

    // Default: manifest
    m, diags, err := manifest.ParseManifest(node)
    if err != nil { return err }

    moreDiags := manifest.ValidateManifest(m)
    diags = append(diags, moreDiags...)

    for _, d := range diags {
        fmt.Fprintf(os.Stderr, "warning: %s\n", d.Message)
    }
    return nil  // warnings don't fail
}
```

## Diagnostic Type

```go
type Diagnostic struct {
    Level   DiagLevel // Error, Warning
    Req     string    // e.g., "req-mf-004"
    Message string
}
```

Warnings are emitted to stderr but don't change exit code (exit 0).
Errors cause early return with non-zero exit.

## Phase 1 Oracle Fixture Mapping

| Fixture | Outcome | Reqs | Phase 1 behavior |
|---------|---------|------|-----------------|
| `valid-minimal.yml` | accept | mf-001~003 | Parse + validate → ok |
| `valid-full.yml` | accept | mf-005 | antigravity accepted |
| `valid-workspaces-reserved.yml` | warn | mf-021 | accept + "workspaces" warning |
| `invalid-missing-name.yml` | reject | mf-002 | "name" in diagnostic |
| `invalid-target.yml` | reject | mf-005 | "notarealtool" in diagnostic |
| `invalid-no-source-key.yml` | reject | mf-007 | no source key in dep entry |
| `invalid-both-id-git.yml` | reject | mf-011 | "id" and "git" in diagnostic |
| `invalid-registry-scheme.yml` | reject | mf-014, sc-006 | "bad" in diagnostic |
| `invalid-registries-typo.yml` | reject | mf-015 | "urls" in diagnostic |
| `invalid-localpath-escape.yml` | reject | mf-016 | ".." in diagnostic |
| `invalid-hash-algorithm.yml` | reject | mf-018 | "md5" in diagnostic |
| `x-extension-roundtrip.yml` | roundtrip | ext-001 | Phase 0 handles this |
| lockfile fixtures | accept | — | Content-sniffed as lockfile, skip manifest validation |

## Dep Entry Structural Checks (Step 1 scope)

mf-007 and mf-011 are key-presence checks on dep entry mappings, not dependency resolution. Folded into Step 1:

- `invalid-no-source-key.yml`: object-form entry `{alias: foo, ref: main}` has no source key (no `git:`, `id:`, `path:`, `name:`) → reject
- `invalid-both-id-git.yml`: entry has both `id:` and `git:` → reject naming both keys

These require walking `dependencies.apm[]` entries and checking key presence, not parsing dependency strings.

## Boundary: What Phase 1 Does NOT Build

- Dependency resolution (Phase 2)
- Lockfile struct/validation (Phase 3)
- Deploy adapters (Phase 4)
- Full dependency string parsing internals — Phase 1 parses enough to validate structure but the resolver is Phase 2
- Policy enforcement (Phase 6)
- `targets:` (plural) — not defined in OpenAPM v0.1 spec; only `target` (singular) exists
