# Phase 1 — Implementation Plan

## Step 1: Target logic (`internal/manifest/target.go`)

### 1.1 `target.go`

```
File: internal/manifest/target.go
```

- `CanonicalTargets` map — full vocabulary per §4.2.1 + antigravity
- `TargetAliases` map — `vscode`→`copilot`, `agents`→`copilot`
- `SupportedTargets` slice — claude, codex, copilot, opencode, antigravity (for init)
- `ValidateTarget(token string) (normalized string, error)`:
  1. `"minimal"` → error
  2. In `TargetAliases` → return alias value
  3. In `CanonicalTargets` → return as-is
  4. Matches `x-[a-z][a-z0-9-]*-[a-z][a-z0-9-]*` → return as-is (tg-004)
  5. Otherwise → error naming the token
- `IsVendorTarget(token string) bool` — regex match for x-vendor pattern
- `HasAdapter(token string) bool` — returns true only for `SupportedTargets` + `agent-skills`

### 1.2 `target_test.go` — table-driven

| Case | Input | Expected |
|------|-------|----------|
| canonical | `"claude"` | ok, `"claude"` |
| alias | `"vscode"` | ok, `"copilot"` |
| alias | `"agents"` | ok, `"copilot"` |
| antigravity | `"antigravity"` | ok |
| x-vendor | `"x-acme-tool"` | ok |
| minimal rejected | `"minimal"` | error |
| unknown | `"notarealtool"` | error contains "notarealtool" |
| x-vendor bad format | `"x-a"` | error (needs two segments) |
| removed has no adapter | `"gemini"` | valid target, `HasAdapter`=false |

Verify: `go test ./internal/manifest/ -run TestValidateTarget`

## Step 2: Manifest struct + parser (`internal/manifest/manifest.go`)

### 2.1 `manifest.go`

```
File: internal/manifest/manifest.go
```

- `Manifest` struct (see design.md)
- `Diagnostic` type with `Level`, `Req`, `Message`
- `ParseManifest(node *yaml.Node) (*Manifest, []Diagnostic, error)`:
  1. `node` is DocumentNode from `SafeLoad`; extract `root = node.Content[0]`
  2. mf-001: `root.Kind != MappingNode` → error naming file
  3. Walk mapping keys:
     - `name` → mf-002: required, non-empty string
     - `version` → mf-003: required, string; mf-004: SHOULD match semver regex → warning diagnostic
     - `target` → string or sequence of strings; validate each via `ValidateTarget`
     - `description`, `author`, `license`, `default_host`, `type`, `scripts`, `includes` → extract
     - `registries` → extract (validation in step 3)
     - `dependencies` / `devDependencies` → extract raw entries for structural checks
     - `workspaces` → mf-021: set flag, emit warning diagnostic
     - `x-*` keys → ignore (Phase 0 preserves them)
     - Unknown non-x keys → preserve (mf-006, Phase 0)
  4. Return Manifest + diagnostics

### 2.2 Dep entry structural checks (mf-007 partial, mf-011)

Inside `ParseManifest`, when processing `dependencies.apm[]`:
- For each object-form entry (mapping):
  - mf-011: has both `id:` and `git:` → error naming both keys
  - mf-007 (partial): object-form must have at least one source key (`git:`, `id:`, `path:`, `name:`) → error if missing
- String-form entries: defer full parsing to Phase 1C/2; for now, accept and store

### 2.3 `manifest_test.go`

Test against oracle fixtures:
- `valid-minimal.yml` → accept, name="my-project", version="1.0.0"
- `valid-full.yml` → accept, target contains antigravity
- `valid-workspaces-reserved.yml` → accept + warning containing "workspaces"
- `invalid-missing-name.yml` → error containing "name"
- `invalid-target.yml` → error containing "notarealtool"
- `invalid-both-id-git.yml` → error containing "id" and "git"
- `invalid-no-source-key.yml` → error (no source key)

Plus inline table-driven tests for:
- Non-mapping top-level (mf-001)
- Empty name (mf-002)
- Non-semver version warning (mf-004)
- `target: minimal` rejected (mf-005)
- Alias normalization (mf-005)
- mf-021 workspaces warning

Verify: `go test ./internal/manifest/ -run TestParseManifest`

## Step 3: Registry validation

Inside `ParseManifest` registries extraction:
- mf-014: each `registries.<name>.url` must start with `https://` or `http://`
- mf-015: unknown keys in `registries.<name>` (except `url`, `insecure`, `aliases`, and `x-*`) → error naming key
- sc-006: `http://` URL → error unless `insecure: true` or host is loopback/RFC1918

Tests:
- `invalid-registry-scheme.yml` → error containing "bad"
- `invalid-registries-typo.yml` → error containing "urls"
- Inline: http with `insecure: true` → accept; http with loopback → accept

## Step 4: CLI updates

### 4.1 Update `validate` command

In `cmd/apm/main.go`:
1. After `SafeLoad`, extract `root = node.Content[0]`
2. Check `root.Kind == MappingNode` (mf-001)
3. Content-sniff: `nodeHasKey(root, "lockfile_version")` → skip manifest validation (stub)
4. Else: `manifest.ParseManifest(node)` → handle error/diagnostics
5. Warnings → stderr, exit 0; errors → stderr, exit 1

### 4.2 Add `init` command

```
apm init [--name NAME] [--version VERSION] [--target TARGET...] [--force]
```

1. Check `apm.yml` doesn't exist (unless `--force`)
2. Build manifest Node programmatically:
   - `name`: flag or directory basename
   - `version`: flag or `"0.1.0"`
   - `target`: validate each against `SupportedTargets` only (not full canonical — init is producer-side)
3. `SafeDump` → write `apm.yml`

### 4.3 `init` test

- `init --name test --version 1.0.0` → creates valid `apm.yml`
- Round-trip: pipe output through `SafeLoad` + `ParseManifest` → must pass
- `init --target claude` → file contains `target: claude`, no `targets:`, no `minimal`
- `init --target gemini` → error (not in `SupportedTargets`)
- `init` in directory with existing `apm.yml` → error (unless `--force`)

## Step 5: Dependency parsing (mf-007, mf-008, mf-009, mf-010, mf-019)

### 5.1 `internal/manifest/depref.go` — DependencyReference struct + parser

```
File: internal/manifest/depref.go
```

**DependencyReference struct** (parsed string/object dep entry):

```go
type DependencyReference struct {
    RepoURL     string   // "owner/repo" (canonical) or full URL
    Host        string   // e.g., "github.com", "" if default
    Owner       string
    Repo        string
    Reference   string   // branch/tag/SHA after #
    VirtualPath string   // sub-path for virtual packages
    VirtualType string   // "file" or "subdirectory" (mf-008)
    Alias       string
    IsLocal     bool
    LocalPath   string   // e.g., "./packages/foo"
    IsParent    bool     // git: parent sentinel (mf-010)
}
```

**ParseDepString(s string) (*DependencyReference, error)** — ABNF parser (mf-007):

1. Local-path detection: `isLocalPath(s)` → extract local path, validate escape (mf-016 already done)
2. URL-form: starts with `https://`, `http://`, `ssh://git@`, `git@` → parse host/port/owner/repo/virtual-path/ref
3. Shorthand-form: `[host/]owner/repo[/virtual-path][#ref]` → distinguish host by `.` in first segment
4. None of the above → reject with diagnostic

**ParseDepDict(entry *yaml.Node) (*DependencyReference, error)** — object-form parser:

1. Extract keys: `git`, `id`, `path`, `name`, `ref`, `alias`, `type`, `skills`
2. mf-011: reject both `id` and `git` (already done in Step 1)
3. mf-007: require at least one source key (already done)
4. mf-010: if `git == "parent"` → validate `path` required, `type` forbidden → mark `IsParent`
5. mf-016: if `path`, check escape (already done)

### 5.2 Virtual package classification (mf-008)

In `depref.go`, after parsing virtual-path:

```go
var virtualFileExtensions = []string{
    ".prompt.md", ".instructions.md", ".agent.md", ".chatmode.md",
}

func classifyVirtualPath(vp string) string {
    for _, ext := range virtualFileExtensions {
        if strings.HasSuffix(vp, ext) {
            return "file"
        }
    }
    return "subdirectory"
}
```

### 5.3 Canonical normalization (mf-009 + mf-019) — GROUNDWORK, no CLI caller

**Invariant**: `ToCanonical` is a pure, unit-tested function with **no CLI caller in Phase 1**. `normalize` stays byte-exact (Phase 0 req-cf-001). The consumer of canonical normalization is lockfile identity construction (Phase 3). Do NOT wire into the round-trip path.

```go
func (d *DependencyReference) ToCanonical(defaultHost string) string
```

1. If `d.Host == ""` or `d.Host == defaultHost` → `owner/repo[/virtual-path][#ref]`
2. Otherwise → `host/owner/repo[/virtual-path][#ref]`
3. Strip `.git` suffix from repo

`defaultHost` comes from `Manifest.DefaultHost` or implementation default `"github.com"`.

### 5.4 `depref_test.go` — table-driven

**String-form parsing tests:**
- `owner/repo` → shorthand, host=""
- `owner/repo#v1.0.0` → shorthand with ref
- `github.com/owner/repo` → shorthand with host
- `gitlab.com/owner/repo/skills/my-skill` → virtual subdirectory
- `owner/repo/prompts/review.prompt.md` → virtual file
- `https://gitlab.com/acme/repo.git` → url-form
- `git@gitlab.com:acme/repo.git` → SCP url-form
- `ssh://git@host:7999/owner/repo.git` → ssh url-form with port
- `./packages/local` → local-path
- `../outside` → local-path escape → error
- `not valid` → error (no form matches)

**Object-form tests:**
- `git: parent` + `path: x` → IsParent=true
- `git: parent` without `path` → error
- `git: parent` with `type: gitlab` → error

**Canonical normalization tests:**
- `github.com/owner/repo` + default `github.com` → `owner/repo`
- `gitlab.com/owner/repo` + default `github.com` → `gitlab.com/owner/repo`
- `owner/repo` (no host) → `owner/repo` (unchanged)

### 5.5 Wire into ParseManifest

Update `validateDepBlock` to parse all entries (string + object) through the new parsers, replacing the current partial checks.

**Invariant**: dep parsing is read-only on the `yaml.Node`. Extract into structs, never mutate the Node. `normalize` re-emits the Node; Phase 0 byte-exactness depends on it being untouched.

### 5.6 Regression gate

After Step 5, re-run ALL Step 1 accept fixtures — they must still exit 0. Step 5 adds ABNF rejection for string deps; a previously-accepted fixture could regress.

**Canary**: `valid-full.yml` contains `contoso/common-prompts#^1.0.0` (shorthand + ref `^1.0.0`). ABNF `ref = 1*VCHAR` admits `^1.0.0`, so it should pass — but this must be proven, not assumed.

```bash
# Re-run Step 1 accept fixtures
bin/apm.exe validate conformance-kit/oracle/manifest/valid-minimal.yml      # exit 0
bin/apm.exe validate conformance-kit/oracle/manifest/valid-full.yml         # exit 0 (canary)
bin/apm.exe validate conformance-kit/oracle/manifest/x-extension-roundtrip.yml  # exit 0
bin/apm.exe validate conformance-kit/oracle/manifest/valid-workspaces-reserved.yml  # exit 0 + warning
```

## Step 6: MCP/placeholder/marketplace (mf-012, mf-013, mf-017)

### 6.1 `internal/manifest/mcp.go` — MCPDependency struct + validation

```
File: internal/manifest/mcp.go
```

```go
type MCPDependency struct {
    Name      string
    Transport string // "stdio" | "sse" | "http" | "streamable-http"
    Command   string
    Args      *[]string // nil = absent, empty = explicit []
    URL       string
    Env       map[string]string
    Headers   map[string]string
    Registry  any // nil=default, false=self-defined, string=custom URL
}
```

**ParseMCPEntry(entry *yaml.Node) (*MCPDependency, error)** + **ValidateMCP(m *MCPDependency) error**

mf-012 validation:
- Self-defined (`Registry == false`):
  - Missing `transport` → error
  - `stdio` without `command` → error
  - `http`/`sse`/`streamable-http` without `url` → error
  - `stdio` command with spaces and `Args == nil` → error

### 6.2 Placeholder recognition (mf-013) — RECOGNITION ONLY, no parse-time rejection

Phase 1 scope: recognize the three placeholder forms and leave `${{ }}` untouched. The spec defines resolution behavior ("MUST NOT silently emit"), not a malformed-placeholder syntax to reject at parse time. The enforcement fires at Phase 4 config-gen, not here.

Build the recognition helpers as groundwork; do NOT write tests asserting parse-time rejection of placeholder syntax (that would be testing invented behavior the spec doesn't require).

```go
var (
    envVarRe   = regexp.MustCompile(`\$\{(?:env:)?([A-Za-z_][A-Za-z0-9_]*)\}`)
    inputVarRe = regexp.MustCompile(`\$\{input:([^}]+)\}`)
    actionsRe  = regexp.MustCompile(`\$\{\{.*?\}\}`)
)
```

### 6.3 Marketplace source validation (mf-017)

In `manifest.go` or `marketplace.go`:

Validate `marketplace.packages[].source`:
- No `..` path segments
- URL: no userinfo/port/query, HTTPS only for remote
- Local: must start with `./`

### 6.4 Tests

**MCP tests:**
- Self-defined missing transport → error
- stdio missing command → error
- http missing url → error
- stdio command with spaces, no args → error
- stdio command with spaces, args: [] → accept

**Placeholder tests:**
- `${TOKEN}` recognized as env var
- `${env:TOKEN}` recognized
- `${input:api-key}` recognized
- `${{ secrets.TOKEN }}` left untouched

**Marketplace tests:**
- Source `./packages/foo` → accept
- Source `../escape` → error (`..)
- Source `https://example.com/repo` → accept
- Source `http://example.com/repo` → error (not https)
- Source with `user@host` → error

## Verification

```bash
go test ./... -cover       # target ≥80% on manifest package
go vet ./...
go build -o bin/apm.exe ./cmd/apm

# Phase 1 gate
bin/apm.exe validate conformance-kit/oracle/manifest/valid-minimal.yml       # exit 0
bin/apm.exe validate conformance-kit/oracle/manifest/invalid-missing-name.yml # exit 1, "name"
bin/apm.exe validate conformance-kit/oracle/manifest/invalid-target.yml       # exit 1, "notarealtool"
bin/apm.exe validate conformance-kit/oracle/manifest/invalid-both-id-git.yml  # exit 1, "id"+"git"
bin/apm.exe validate conformance-kit/oracle/manifest/valid-workspaces-reserved.yml # exit 0, stderr "workspaces"

# Lockfile fixtures still accepted (content-sniff → skip manifest validation)
bin/apm.exe validate conformance-kit/oracle/lockfile/v1-git-only.yml          # exit 0

# init test
bin/apm.exe init --name test --version 1.0.0 --target claude
bin/apm.exe validate apm.yml                                                  # exit 0
```

## Anti-cheat note (ADR-0002)

Steps 5-6 have ZERO oracle fixtures — all tests are self-authored. Mutation testing (gremlins/go-mutesting) must run before claiming these reqs done. Self-authored tests that pass are necessary but not sufficient; a survived mutant = a test that doesn't actually verify the behavior.

## File checklist

| File | Purpose | Step |
|------|---------|------|
| `internal/manifest/target.go` | Target vocabulary, validation, aliases | 1 ✅ |
| `internal/manifest/target_test.go` | Target validation tests | 1 ✅ |
| `internal/manifest/manifest.go` | Manifest struct, ParseManifest, Diagnostic | 1 ✅ |
| `internal/manifest/manifest_test.go` | Parse/validate tests + oracle fixtures | 1 ✅ |
| `cmd/apm/main.go` | Updated validate + init command | 1 ✅ |
| `cmd/apm/main_test.go` | CLI tests | fix ✅ |
| `internal/manifest/depref.go` | DependencyReference struct + ABNF parser | 5 |
| `internal/manifest/depref_test.go` | Dep parsing + normalization tests | 5 |
| `internal/manifest/mcp.go` | MCPDependency struct + validation + placeholder | 6 |
| `internal/manifest/mcp_test.go` | MCP + placeholder tests | 6 |
