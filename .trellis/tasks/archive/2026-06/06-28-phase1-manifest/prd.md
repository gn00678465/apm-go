# Phase 1 — Manifest (apm.yml) Parsing, Validation, and Init

## Goal

Implement the manifest layer: parse `apm.yml` into a typed `Manifest` struct, validate all fields per OpenAPM v0.1 §4.1–4.8, and provide an `apm init` command to scaffold new manifests.

## Scope (conformance-kit Phase 1 — 21 reqs + init command)

### 1A — Core structure and metadata (6 reqs)

| req | kw | class | Summary |
|-----|------|-------|---------|
| `req-mf-001` | MUST | P/C | Top-level must be mapping; reject non-mapping naming file |
| `req-mf-002` | MUST | P | `name` required, non-empty string |
| `req-mf-003` | MUST | P | `version` required, string |
| `req-mf-004` | SHOULD | P | `version` should match semver 2.0.0; non-blocking diagnostic if not |
| `req-mf-005` | MUST | P | Target validation: canonical ∪ {antigravity} ∪ x-vendor; reject others; `minimal` not explicit; aliases normalize |
| `req-mf-021` | MUST | P/C | Producer no `workspaces:`; Consumer non-blocking diagnostic |

### 1B — Registries and security (4 reqs)

| req | kw | class | Summary |
|-----|------|-------|---------|
| `req-mf-014` | MUST | P | `registries.<name>.url` must be https/http |
| `req-mf-015` | MUST | P | Unknown keys in `registries.<name>` (except x-*) rejected |
| `req-mf-019` | MUST | C | `default_host` is sole stripped host; impl-default documented |
| `req-sc-006` | MUST | C | http:// URL parse error unless insecure/loopback/RFC1918 |

### 1C — Dependency string parsing (6 reqs)

| req | kw | class | Summary |
|-----|------|-------|---------|
| `req-mf-007` | MUST | C | Parse string deps per ABNF (url/shorthand/local-path) |
| `req-mf-008` | MUST | C | Virtual package classified by extension only |
| `req-mf-009` | MUST | C | Canonical normalization: strip only default_host |
| `req-mf-010` | MUST | C | `git: parent` sentinel in transitive only |
| `req-mf-011` | MUST | C | Reject `id:` + `git:` conflict |
| `req-mf-016` | MUST | C | Local-path recognition; reject `..` escaping project root |

### 1D — MCP/placeholder/marketplace/policy (4 reqs)

| req | kw | class | Summary |
|-----|------|-------|---------|
| `req-mf-012` | MUST | C | Self-defined MCP transport/command/url validation |
| `req-mf-013` | MUST | C | Placeholder dispatch: `${VAR}`, `${env:VAR}`, `${input:}`, `${{ }}` |
| `req-mf-017` | MUST | P | Marketplace source path/URL validation |
| `req-mf-018` | MUST | C | `policy.hash_algorithm` in {sha256,sha384,sha512} only |

### 1E — Target routing (1 req)

| req | kw | class | Summary |
|-----|------|-------|---------|
| `req-tg-004` | MUST | C | Accept x-vendor targets; route or diagnose; no silent ignore. Removed targets (gemini/cursor/windsurf) → "no handler" diagnostic |

### 1F — `apm init` command (user requirement)

- Interactive or flag-driven scaffold of `apm.yml`
- Prompt for `name` (default: directory name), `version` (default: "0.1.0")
- Target selection: **only** `claude`, `codex`, `copilot`, `opencode`, `antigravity`
- Output conforming minimal `apm.yml` via `SafeDump`

## Execution Strategy

Phase 1 is large (21 reqs + init) but tightly coupled — don't pre-commit to a 4-way split.

**Step 1 (foundation)**: Manifest struct + core fields (mf-001~005, mf-021) + ALL target logic (mf-005 + tg-004) + `apm init` + validate dispatch decision.

**Step 2 (dep parsing)**: DependencyReference struct + ABNF string parsing (mf-007) + virtual package classification (mf-008) + canonical normalization (mf-009) + default_host threading (mf-019) + git:parent sentinel (mf-010). 5 reqs, tightly coupled.

**Step 3 (MCP/placeholder/marketplace)**: MCP validation (mf-012) + placeholder dispatch (mf-013) + marketplace source validation (mf-017). 3 reqs, independent of dep group.

**Note**: Zero dedicated oracle fixtures exist for Steps 2-3. All validation relies on unit tests.

## Constraints

- Build on `yamlcore.SafeLoad` from Phase 0 — parse to `yaml.Node` first, then extract typed fields
- Oracle fixtures are immutable (`conformance-kit/oracle/`)
- Supported deploy targets (for init and validation): `claude`, `codex`, `copilot`, `opencode`, `antigravity`
- Accepted target vocabulary (for parsing): canonical set ∪ {antigravity} ∪ x-vendor (per acceptance-checklist target policy)
- Removed targets (gemini, cursor, windsurf): vocabulary accepted, no adapter → diagnostic

## Acceptance Criteria

- [ ] All 21 Phase 1 reqs pass their oracle fixtures (where available)
- [ ] `apm init` creates valid `apm.yml` with only the 5 supported targets
- [ ] `apm validate` rejects all Phase 1 `invalid-*.yml` fixtures with correct diagnostics
- [ ] `go test ./... -cover` passes with ≥80% on manifest package
- [ ] `go vet ./...` clean
