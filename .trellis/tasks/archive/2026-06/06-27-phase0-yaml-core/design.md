# Phase 0 — Technical Design

## Architecture Decision

Build on `yaml.Node` (from `go.yaml.in/yaml/v4`), not struct/map unmarshal.

Rationale — this single decision makes all 4 reqs tractable:

| req | Why Node works |
|-----|---------------|
| req-mf-020(a) scalars=string | `node.Value` is always the literal string; implicit `!!int/!!float/!!bool` tags are detectable via `TaggedStyle=false` |
| req-mf-020(b) reject anchor/alias | `node.Anchor != ""` and `node.Alias != nil` — only Node preserves these; struct/map decode silently resolves aliases |
| req-mf-020(c) reject custom tags | `node.Tag` prefix check — straightforward |
| req-mf-020(d) no YAML 1.1 octal | yaml.v4 is YAML 1.2; `0755` resolves to `!!int` implicitly, but safe-loader treats implicit scalars as strings |
| req-ext-001 / req-mf-006 round-trip | Node preserves key order, unknown keys, and `x-*` at any depth; re-emit produces byte-exact output |

Later phases layer typed accessors on top of the validated Node without discarding it.

## Spike Results (verified)

| Test | Result |
|------|--------|
| `yaml.v4` `TaggedStyle` distinguishes implicit vs explicit tags | OK — `Style&TaggedStyle != 0` iff tag is author-written |
| Anchor/Alias detection via Node fields | OK — `Anchor`, `Alias` fields present on v4 `yaml.Node` |
| Custom tag `!foo` visible in `node.Tag` | OK |
| Round-trip `x-extension-roundtrip.yml` | byte-exact with `SetIndent(2)` |
| Round-trip `round-trip-unknown-fields.yml` | byte-exact — `future_unknown_field`, `x-acme-*` survive |
| Round-trip all lockfile fixtures | byte-exact |

## Package Layout

```
cmd/
  apm/
    main.go              # CLI entry point (cobra)

internal/
  yamlcore/
    safe.go              # SafeLoad, SafeDump — the shared safe-loader
    safe_test.go         # Table-driven tests for all req-mf-020 clauses
    vendor_ext.go        # IsVendorExtKey helper (x-[a-z][a-z0-9-]*)
    vendor_ext_test.go
```

Why `internal/yamlcore/`:
- `internal/` prevents external import (this is implementation, not a public API)
- `yamlcore/` is the universal YAML layer used by all later packages (manifest, lockfile, policy parsers)
- Kept separate from `cmd/` for testability

## Core API

### `yamlcore.SafeLoad`

```go
func SafeLoad(data []byte) (*yaml.Node, error)
```

1. `yaml.Unmarshal(data, &doc)` → parse to `yaml.Node` tree
2. Walk tree, reject on first violation:
   - Any node with `Anchor != ""` → error: "YAML anchors are not allowed"
   - Any node with `Alias != nil` (Kind == AliasNode) → error: "YAML aliases are not allowed"
   - Any node whose `ShortTag()` is non-empty and does not start with `!!` → error: "custom YAML tag %q is not allowed" (spec says "non-`!!` tags"; do NOT use an allow-list — standard tags like `!!timestamp` and `!!binary` are permitted)
3. Return the validated `*yaml.Node` (the DocumentNode wrapper)

Note: We do NOT modify the Node tree. Implicit tags (`!!int` on `42`) remain — the round-trip serializer needs them to reproduce the original output. The "scalars are strings" semantic is enforced by typed accessor functions built in later phases, using `TaggedStyle` to distinguish.

### `yamlcore.SafeDump`

```go
func SafeDump(doc *yaml.Node) ([]byte, error)
```

1. Create `yaml.Encoder` writing to `bytes.Buffer`
2. `enc.SetIndent(2)` (explicit; round-trip byte-exactness validated against yaml.v4 v4.0.0-rc.6)
3. `enc.Encode(doc)` → re-emit the Node tree
4. Return bytes

### `yamlcore.IsVendorExtKey`

```go
func IsVendorExtKey(key string) bool
```

Returns true if `key` matches `x-[a-z][a-z0-9-]*`. Used by later phases to skip vendor extension keys during semantic interpretation (req-ext-001).

## CLI Surface (conformance runner contract)

The runner (`run_conformance.py`) drives the binary with these sub-commands:

| Command | Contract | Phase 0 reqs |
|---------|----------|-------------|
| `apm validate <file>` | Exit 0 on valid, non-zero + diagnostic on invalid | req-mf-020 |
| `apm normalize --stdout <file>` | Re-emit parsed document to stdout | req-ext-001, req-mf-006, req-cf-001 |

Phase 0 implements only these two commands. Later phases add `install`, `semver-eval`, etc.

CLI framework: **cobra** — minimal setup, widely used, supports sub-commands cleanly. Only `validate` and `normalize` commands wired for Phase 0.

### `validate` command

```
apm validate <file>
```

1. Read file bytes
2. `yamlcore.SafeLoad(data)` 
3. If error → print diagnostic to stderr, exit 1
4. If ok → exit 0 (silent success)

### `normalize` command

```
apm normalize --stdout <file>
```

1. Read file bytes
2. `yamlcore.SafeLoad(data)` → validated Node
3. `yamlcore.SafeDump(node)` → round-tripped bytes
4. Write to stdout via `os.Stdout.Write(b)` — NOT `fmt.Print` (encoder already emits trailing newline; double-newline breaks byte-equality)

## req-ext-002 Enforcement

Design invariant: no `x-` prefixed normative key in production code.

Enforcement: a grep-based lint in the check phase:

```bash
grep -rn '"x-' internal/ cmd/ | grep -v '_test.go' | grep -v 'vendor_ext'
```

Any hit (outside test files and the `IsVendorExtKey` helper itself) is a violation.

## Error Format

Diagnostics follow the conformance runner assertion pattern — the error message must contain the substrings specified in `EXPECTATIONS.yaml` `must_contain` fields.

For Phase 0, the only negative fixture is `invalid-yaml-anchor-alias.yml` which has no `must_contain` — just needs non-zero exit. But we include descriptive messages for debuggability.

Format: `<file>: <description>` on stderr.

## Boundary: What Phase 0 Does NOT Build

- No typed struct definitions (`APMPackage`, `DependencyReference`, etc.)
- No field validation (required fields, target validation, etc.)
- No dependency parsing
- No lockfile structure validation
- The `SafeLoad` function does not interpret manifest/lockfile/policy semantics — it only enforces the YAML safe subset

These are layered on top of the validated Node in later phases.
