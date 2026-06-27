# Phase 0 — YAML safe-loader and round-trip core

## Goal

Implement the foundational YAML parsing layer for the apm-go CLI rewrite. This layer is the prerequisite for all subsequent phases (manifest, lockfile, policy parsing). It provides a shared safe-loader that enforces the OpenAPM v0.1 YAML safe subset and a re-serializer that preserves byte-equivalent round-trips.

## Scope (conformance-kit Phase 0 — 4 reqs)

| req | kw | class | § | Summary |
|-----|------|-------|------|---------|
| `req-mf-020` | MUST | Consumer | 4.1 | YAML safe subset: (a) scalars default to string unless explicitly `!!int/!!float/!!bool`; (b) reject `&anchor`/`*alias`; (c) reject custom (non-`!!`) tags; (d) no YAML 1.1 octal coercion (`0NN`). Applies to manifest, lockfile, and policy. |
| `req-ext-001` | MUST | Consumer | 4.1 | `x-[a-z][a-z0-9-]*` keys at any nesting level are vendor extensions: ignored semantically, no parse error, preserved byte-equivalent on round-trip. |
| `req-ext-002` | MUST | Producer | 4.1 | Design invariant: implementation never defines normative keys starting with `x-`. |
| `req-mf-006` | MUST | Consumer | 4.1 | Preserve unknown top-level keys when rewriting the manifest (forward/backward compat). |

## Out of scope

- Manifest field validation (Phase 1: `req-mf-001` through `req-mf-021`)
- Dependency string parsing (Phase 1-2)
- Lockfile structure validation (Phase 3)
- Policy parsing (Phase 6)
- All typed struct definitions for APMPackage, DependencyReference, etc. (later phases build typed accessors on top of the validated Node)

## Requirements

1. **Safe-loader**: Parse any YAML document to `yaml.Node` and validate the safe subset.
   - Reject anchors (`node.Anchor != ""`) and aliases (`node.Alias != nil`)
   - Reject custom tags (tags not starting with `!!`)
   - Implicit tag resolution (`!!int/!!float/!!bool` on unquoted scalars) is preserved in the Node tree for round-trip fidelity, but typed accessors (built in later phases) treat them as strings unless `TaggedStyle` is set
   - YAML 1.1 octal (`0NN`) is NOT coerced because yaml.v3 follows YAML 1.2

2. **Round-trip serializer**: Re-emit a validated `yaml.Node` to bytes with byte-equivalent output (spec permits trailing newline and flow-style normalization only).
   - Use `yaml.Encoder` with `SetIndent(2)`
   - Unknown top-level keys and `x-*` keys at any depth survive round-trip

3. **Minimal CLI surface** (as required by the conformance runner):
   - `apm validate <file>` — parse via safe-loader, exit 0 on success, non-zero + diagnostic on rejection
   - `apm normalize --stdout <file>` — parse via safe-loader, re-emit to stdout

4. **Design invariant** (`req-ext-002`): no normative key in any Go struct tag or constant starts with `x-`. Enforced by code review and a grep-based lint.

## Acceptance Criteria

- [ ] `apm validate` rejects `oracle/manifest/invalid-yaml-anchor-alias.yml` (anchor/alias) with non-zero exit
- [ ] `apm validate` accepts `oracle/manifest/valid-minimal.yml` with exit 0
- [ ] `apm normalize --stdout oracle/manifest/x-extension-roundtrip.yml` produces byte-equal output (x-* at top, registries, deps all preserved)
- [ ] `apm normalize --stdout oracle/lockfile/round-trip-unknown-fields.yml` preserves `x-acme-top`, `future_unknown_field`, `x-acme-pin`
- [ ] Go table-driven tests cover all 4 clauses of req-mf-020: (a) implicit type coercion detection, (b) anchor rejection, (c) alias rejection, (d) custom tag rejection, (e) YAML 1.1 octal non-coercion
- [ ] `go test ./... -race -cover` passes with ≥80% coverage on the yamlcore package
- [ ] `go vet ./...` and `go fmt` clean
- [ ] No `x-` prefixed constants, struct tags, or field names in production code (req-ext-002)
- [ ] Conformance runner (`run_conformance.py`) passes the Phase 0 subset when `APM_BIN` points to the built binary

## Constraints

- Authority: `conformance-kit/oracle/` fixtures and `EXPECTATIONS.yaml` are immutable oracle — code changes to pass; oracle never changes to fit code.
- Library: `go.yaml.in/yaml/v4` v4.0.0-rc.6 (switched from yaml.v3; v4 defaults to 2-space indent matching oracle fixtures, same Node API, maintained by official YAML org)
- CLI framework: to be decided (cobra or bare flags — keep minimal for Phase 0)
