# Phase 0 — Implementation Plan

## Dependency

Pin `go.yaml.in/yaml/v4 v4.0.0-rc.6` exactly. Round-trip byte-exactness is validated against this specific version. Do not `go get @latest` — emitter behavior changes in a pre-release can break byte-equality.

## Step 1: `internal/yamlcore/` package

### 1.1 `vendor_ext.go` — IsVendorExtKey

```
File: internal/yamlcore/vendor_ext.go
```

- `func IsVendorExtKey(key string) bool` — returns true if key matches `x-[a-z][a-z0-9-]*`
- Use a compiled `regexp` or hand-written check (simple enough for hand-written)

Verify: `go test ./internal/yamlcore/ -run TestIsVendorExtKey`

### 1.2 `safe.go` — SafeLoad + SafeDump

```
File: internal/yamlcore/safe.go
```

**SafeLoad(data []byte) (*yaml.Node, error)**

1. `yaml.Unmarshal(data, &doc)` → `yaml.Node`
2. Walk tree recursively via helper `validateNode(n *yaml.Node) error`:
   - `n.Anchor != ""` → `fmt.Errorf("YAML anchors are not allowed")`
   - `n.Alias != nil` (Kind == AliasNode) → `fmt.Errorf("YAML aliases are not allowed")`
   - `n.ShortTag() != "" && !strings.HasPrefix(n.ShortTag(), "!!")` → `fmt.Errorf("custom YAML tag %q is not allowed", n.Tag)`
   - Recurse into `n.Content`
3. Return validated `*yaml.Node`

Tag rule: spec says "non-`!!` tags" are custom. Check via `ShortTag()` prefix, NOT an allow-list. Standard `!!` tags like `!!timestamp`, `!!binary` are permitted.

**SafeDump(doc *yaml.Node) ([]byte, error)**

1. `bytes.Buffer` + `yaml.NewEncoder(&buf)`
2. `enc.SetIndent(2)`
3. `enc.Encode(doc)`, `enc.Close()`
4. Return `buf.Bytes()`

Verify: `go test ./internal/yamlcore/ -run TestSafe`

### 1.3 `safe_test.go` — table-driven tests

```
File: internal/yamlcore/safe_test.go
```

**SafeLoad rejection tests (req-mf-020 b/c):**

| Case | Input | Expected |
|------|-------|----------|
| anchor on scalar | `name: &n p` | error contains "anchor" |
| alias on scalar | `name: &n p\ndesc: *n` | error contains "alias" |
| anchor on mapping | `&m\n  a: 1` | error contains "anchor" |
| anchor on sequence | `items: &s\n  - a` | error contains "anchor" |
| custom tag | `foo: !custom bar` | error contains "custom" and "!custom" |
| standard !! tag | `foo: !!timestamp 2026-01-01` | NO error (permitted) |
| standard !!binary | `foo: !!binary aGVsbG8=` | NO error (permitted) |

**SafeLoad acceptance tests:**

| Case | Input | Expected |
|------|-------|----------|
| minimal manifest | `name: p\nversion: "1.0.0"` | no error |
| with x-* keys | oracle `x-extension-roundtrip.yml` | no error |
| with unknown fields | oracle `round-trip-unknown-fields.yml` | no error |

**SafeLoad property tests (req-mf-020 a/d — Node-level only):**

| Case | Input | Assert |
|------|-------|--------|
| implicit int preserved as string-value | `val: 42` | `Value=="42"`, `TaggedStyle==false` |
| explicit !!int has TaggedStyle | `val: !!int 42` | `TaggedStyle==true` |
| octal-looking not coerced | `val: 0755` | `Value=="0755"`, `TaggedStyle==false` |
| YAML 1.2 octal | `val: 0o755` | `Value=="0o755"`, `TaggedStyle==false` |

> Note: (a) and (d) are only fully behaviorally testable once typed accessor functions land in Phase 1. Phase 0 asserts Node-level properties; label tests accordingly.

**Round-trip tests (req-ext-001, req-mf-006, req-cf-001):**

| Case | Fixture | Assert |
|------|---------|--------|
| x-extension round-trip | `x-extension-roundtrip.yml` | `SafeDump(SafeLoad(src)) == src` byte-exact |
| unknown fields round-trip | `round-trip-unknown-fields.yml` | byte-exact |
| minimal round-trip | `valid-minimal.yml` | byte-exact |

Verify: `go test ./internal/yamlcore/ -race -cover` — target ≥80% on yamlcore package

## Step 2: CLI entry point

### 2.1 `cmd/apm/main.go`

```
File: cmd/apm/main.go
```

Minimal cobra setup with root command + two sub-commands.

### 2.2 `validate` command

```
apm validate <file>
```

1. Read file via `os.ReadFile(args[0])`
2. `yamlcore.SafeLoad(data)`
3. Error → `fmt.Fprintf(os.Stderr, "%s: %v\n", filename, err)`, `os.Exit(1)`
4. OK → `os.Exit(0)` (silent)

### 2.3 `normalize` command

```
apm normalize --stdout <file>
```

1. Read file via `os.ReadFile(args[0])`
2. `yamlcore.SafeLoad(data)` → node
3. `yamlcore.SafeDump(node)` → bytes
4. `os.Stdout.Write(bytes)` — NOT `fmt.Print` (avoid double trailing newline)

## Step 3: Build + verify

### 3.1 Build

```bash
go build -o bin/apm.exe ./cmd/apm    # Windows
```

### 3.2 Phase 0 gate (targeted invocations)

These three commands constitute the Phase 0 acceptance gate:

```bash
# 1. Reject anchor/alias (req-mf-020b)
bin/apm validate conformance-kit/oracle/manifest/invalid-yaml-anchor-alias.yml
# Expected: non-zero exit

# 2. Accept valid manifest (baseline)
bin/apm validate conformance-kit/oracle/manifest/valid-minimal.yml
# Expected: exit 0

# 3. Byte-exact round-trip (req-ext-001, req-mf-006)
bin/apm normalize --stdout conformance-kit/oracle/manifest/x-extension-roundtrip.yml > tmp_out.yml
diff tmp_out.yml conformance-kit/oracle/manifest/x-extension-roundtrip.yml
# Expected: no diff

# 4. Lockfile unknown fields round-trip
bin/apm normalize --stdout conformance-kit/oracle/lockfile/round-trip-unknown-fields.yml > tmp_out2.yml
diff tmp_out2.yml conformance-kit/oracle/lockfile/round-trip-unknown-fields.yml
# Expected: no diff
```

### 3.3 Full test suite

```bash
go test ./... -race -cover
go vet ./...
go fmt ./...
```

### 3.4 req-ext-002 lint

```bash
grep -rn '"x-' internal/ cmd/ | grep -v '_test.go' | grep -v 'vendor_ext'
# Expected: no hits
```

## Scope guard

The full conformance runner (`run_conformance.py`) iterates ALL of EXPECTATIONS.yaml — it has no `--phase` filter. Running it now will show Phase 1+ failures (e.g., `invalid-missing-name.yml` expects reject but Phase 0 `validate` exits 0 on it because name validation is Phase 1). These are **expected reds, not regressions**. Do NOT add field validation to make them green — that bleeds Phase 1 into Phase 0.

The Phase 0 gate is the targeted invocations in §3.2 plus Go unit tests.

## File checklist

| File | Purpose |
|------|---------|
| `internal/yamlcore/safe.go` | SafeLoad, SafeDump |
| `internal/yamlcore/safe_test.go` | Table-driven tests |
| `internal/yamlcore/vendor_ext.go` | IsVendorExtKey |
| `internal/yamlcore/vendor_ext_test.go` | Vendor ext key tests |
| `cmd/apm/main.go` | CLI entry point + validate/normalize commands |
| `go.mod` / `go.sum` | yaml.v4 dependency |
