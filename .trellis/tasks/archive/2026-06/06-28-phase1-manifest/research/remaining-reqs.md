# Research: Phase 1 Remaining Requirements (8 reqs)

- **Query**: Spec analysis for req-mf-007/008/009/010/012/013/017/019
- **Scope**: internal (spec + Python reference + existing Go code)
- **Date**: 2026-06-28

---

## req-mf-007 -- String-form dependency ABNF parsing (Section 4.3.1)

### Spec Text

> Each `dependencies.apm` entry MAY be a string conforming to the following grammar (RFC 5234 ABNF):
>
> ```
> dependency       = url-form / shorthand-form / local-path-form
>
> url-form         = url-scheme clone-url
> url-scheme       = "https://" / "http://" / "ssh://git@" / "git@"
> clone-url        = host [ ":" port ] "/" owner "/" repo
>                    [ "/" virtual-path ] [ "#" ref ]
>
> shorthand-form   = [ host "/" ] owner "/" repo
>                    [ "/" virtual-path ] [ "#" ref ]
>
> local-path-form  = local-prefix path-tail
> local-prefix     = "./" / "../" / "/" / "~/" / ".\" / "..\" / "~\"
> path-tail        = 1*pchar
>
> host             = 1*( ALPHA / DIGIT / "-" / "." )
> port             = 1*DIGIT       ; range 1-65535
> owner            = 1*( ALPHA / DIGIT / "-" / "_" )
> repo             = 1*( ALPHA / DIGIT / "-" / "_" / "." )
> virtual-path     = segment *( "/" segment )
> segment          = 1*( ALPHA / DIGIT / "-" / "_" / "." )
> ref              = 1*VCHAR
> pchar            = ALPHA / DIGIT / "/" / "\" / ":" / "." / "-" / "_" / "~"
> ```
>
> **[req-mf-007]** A conforming consumer implementation MUST parse string-form `dependencies.apm` entries per the grammar above. Implementations MUST reject any string that does not satisfy one of the three productions, with a diagnostic identifying the offending entry.

### Three Forms -- Distinguishing Rules

1. **url-form**: Begins with `https://`, `http://`, `ssh://git@`, or `git@`. After the scheme, a `clone-url` follows: `host[:port]/owner/repo[/virtual-path][#ref]`.
2. **shorthand-form**: `[host/]owner/repo[/virtual-path][#ref]`. No scheme prefix. Host (if present) is distinguished by containing a `.` (e.g. `github.com/owner/repo`). Without host, bare `owner/repo`.
3. **local-path-form**: Begins with `./`, `../`, `/`, `~/`, `.\`, `..\`, or `~\`. Everything after the prefix is `1*pchar`.

### Already Implemented in Go

`manifest.go:validateDepBlock` handles:
- Object-form key-presence checks (lines 301-331): checks for `git`, `id`, `path`, `name` keys
- Local-path escape detection for scalar entries (line 290: `isLocalPath` + `containsEscape`)

**Not yet implemented**: The full ABNF grammar validation for string-form entries. Currently scalar entries in `dependencies.apm` are only checked for local-path escapes; no validation rejects strings that fail all three productions.

### Python Reference

`DependencyReference.parse()` (reference.py:1866-2000) implements a far broader parser than the ABNF grammar requires. It handles SCP shorthand (`git@host:path`), `ssh://` with ports, Artifactory paths, ADO 3-segment paths, GitLab subgroups, and more. The spec ABNF is the normative minimum; the Python impl is a superset.

### Complexity: MEDIUM

The ABNF is clean (three alternatives with clear prefixes), but the boundary between shorthand-form with host and url-form needs care. Scope choice: implement the spec-minimum ABNF first (three clean forms with regex/parse), not the full Python superset. The Python superset includes ADO, Artifactory, GitLab subgroup probing, etc. -- those are resolution-time concerns (Phase 2+), not Phase 1 parse-time grammar.

### Dependencies

- **req-mf-009** (canonical normalization): parsing must produce host/owner/repo fields that normalization operates on
- **req-mf-008** (virtual packages): virtual-path detection depends on parsing producing the path segments
- **req-mf-016** (local-path): already partially implemented; the `local-path-form` production shares the prefix set

### Oracle Fixtures

- **Dedicated**: None. `invalid-no-source-key.yml` is tagged `req-mf-007` but it tests object-form key presence (entry `{alias: foo, ref: main}` with no source key), NOT the string-form ABNF grammar.
- **Indirect**: `valid-full.yml` contains string `contoso/common-prompts#^1.0.0` (shorthand-form), accepted as `outcome: accept`, but is tagged only `req-mf-005`.
- **Gap**: No fixture exercises ABNF rejection (e.g., a string that fails all three productions). New fixtures or unit tests needed.

---

## req-mf-008 -- Virtual package classification (Section 4.3.3)

### Spec Text

> **[req-mf-008]** A conforming consumer implementation MUST classify virtual packages by **file extension only** and MUST NOT infer kind from path segments. A `virtual_path` ending in `.prompt.md`, `.instructions.md`, `.agent.md`, or `.chatmode.md` is a file; any other path is a subdirectory. On-disk shape of a subdirectory virtual package is resolved by probing for `apm.yml` first.

### Classification Rules

| Extension | Type |
|---|---|
| `.prompt.md` | FILE |
| `.instructions.md` | FILE |
| `.agent.md` | FILE |
| `.chatmode.md` | FILE |
| anything else (no recognized extension) | SUBDIRECTORY |

Key constraint: classification is by extension **only**, never by path segment names like `prompts/`, `skills/`, `collections/`.

### Python Reference

`DependencyReference.VIRTUAL_FILE_EXTENSIONS` (reference.py:156-161):
```python
VIRTUAL_FILE_EXTENSIONS = (
    ".prompt.md", ".instructions.md", ".chatmode.md", ".agent.md",
)
```

`virtual_type` property (reference.py:202-215): checks `virtual_path.endswith(ext)` for each extension; returns `VirtualPackageType.FILE` or `VirtualPackageType.SUBDIRECTORY`.

### Complexity: LOW

Simple extension-suffix match. Four known extensions -> FILE, everything else -> SUBDIRECTORY.

### Dependencies

- **req-mf-007**: virtual-path is parsed as part of string-form grammar (the `virtual-path` production in the ABNF)

### Oracle Fixtures

- **Dedicated**: None.
- **Indirect**: None of the existing manifest fixtures exercise virtual package paths.
- **Gap**: Need unit tests for extension classification logic.

---

## req-mf-009 -- Canonical normalization (Section 4.3.4)

### Spec Text

> **[req-mf-009]** A conforming consumer implementation MUST normalise dependency entries to canonical form when rewriting the manifest. The canonical form strips **only** the host that matches the project's `default_host:` value (per req-mf-019) or, if `default_host:` is omitted, the consumer's declared implementation-default host. SCP-style git URLs (`git@host:owner/repo.git`) and `https://` URLs targeting the selected default host MUST be normalised to the shorthand form `owner/repo`. Non-default hosts MUST retain their FQDN. A consumer MUST NOT hard-code stripping of any specific host literal; the selection is configured per project.

### How It Works

1. Determine the "strippable host": `default_host` from manifest, or implementation-default (for apm-go: `github.com`).
2. If a dependency's host matches the strippable host:
   - Strip the host prefix
   - Strip `.git` suffix
   - Strip transport scheme (`https://`, `git@`, `ssh://`)
   - Result: `owner/repo[/virtual-path][#ref]`
3. If a dependency's host does NOT match:
   - Keep the FQDN: `gitlab.com/owner/repo[/virtual-path][#ref]`
4. No scheme/transport in canonical form.

### Python Reference

`DependencyReference.to_canonical()` (reference.py:304-346):
- Checks `is_default = host.lower() == default_host().lower()`
- Default host -> `result = self.repo_url` (no host prefix)
- Non-default host -> `result = f"{host_label}/{self.repo_url}"`

### Complexity: MEDIUM

The normalization logic itself is simple (compare host, strip or keep). The complexity is in ensuring this is thread through all code paths: parsing produces host+repo_url separately, normalization applies at rewrite time, and the `default_host` field from the manifest must be threaded through.

### Dependencies

- **req-mf-019** (default_host): normalization depends on knowing which host to strip
- **req-mf-007** (string parsing): canonical normalization inputs come from parsed dependencies

### Oracle Fixtures

- **Dedicated**: None.
- **Indirect**: `valid-full.yml` sets `default_host: github.com` and has string dep `contoso/common-prompts#^1.0.0` (already in canonical form) plus `git: https://gitlab.example.com/acme/coding-standards.git` (non-default host, would stay as FQDN). No assertion on normalization output.
- **Gap**: No roundtrip test verifying canonical rewrite behavior. Need unit tests.

---

## req-mf-010 -- `git: parent` sentinel (Section 4.3.2)

### Spec Text

> **[req-mf-010]** A conforming consumer implementation MUST treat the literal sentinel `git: parent` as valid **only** inside a transitively resolved package whose clone coordinates are known to the resolver. The resolver MUST expand `parent` to the parent package's `host`, `repo_url`, and resolved `ref`, with `virtual_path` taken from `path`. The literal `parent` MUST NOT appear in the lockfile as durable identity (`repo_url` or `source`).

### Sentinel Rules

1. **Validity**: `git: parent` is valid ONLY in transitive deps (packages that are themselves dependencies, not the root manifest).
2. **Expansion**: The resolver expands `parent` -> the parent package's clone coordinates (host, repo_url, ref). The `path:` field becomes the `virtual_path`.
3. **Lockfile prohibition**: The literal string `"parent"` must never appear in lockfile `repo_url` or `source` fields.
4. **Required companion**: `path:` is REQUIRED when `git: parent` is used.
5. **No `type:` allowed**: `type` host-kind hint is rejected on `git: parent` entries.

### Python Reference

`DependencyReference.parse_from_dict()` (reference.py:830-870):
```python
if git_url == "parent":
    if host_type is not None:
        raise ValueError("'type' is only supported for remote git dependencies")
    path_raw = entry.get("path")
    if path_raw is None:
        raise ValueError("Object-style dependency with git: 'parent' requires a 'path' field")
    ...
    return cls(
        repo_url="_parent",
        host=None,
        ...
        is_parent_repo_inheritance=True,
    )
```

### Phase Boundary

Phase 1 scope: parse and validate `git: parent` + `path` in the object-form dict. Mark `is_parent_repo_inheritance=True`.

Phase 2 scope: actual expansion by the resolver (replacing `_parent` with real clone coordinates).

Phase 3 scope: lockfile assertion that `"parent"` never appears as `repo_url` or `source`.

### Complexity: LOW (Phase 1 parse-time), MEDIUM (Phase 2 resolver)

Phase 1 is just recognizing the literal `"parent"` and validating that `path:` is present and `type:` is absent.

### Dependencies

- No Phase 1 req depends on mf-010; it is self-contained at parse time.
- Resolution-time expansion depends on the resolver (Phase 2).

### Oracle Fixtures

- **Dedicated**: None.
- **Gap**: No fixture tests `git: parent` at parse time. Need valid and invalid cases.

---

## req-mf-012 -- MCP dependency validation (Section 4.3.6)

### Spec Text

> **[req-mf-012]** A conforming consumer implementation MUST reject any self-defined MCP server entry (one where `registry: false`) that: (a) omits `transport`; (b) sets `transport: stdio` but omits `command`; (c) sets `transport` to `http`, `sse`, or `streamable-http` but omits `url`. When `transport: stdio` is in effect, the `command` value MUST be a single binary path with no embedded whitespace **unless** the entry also supplies an `args` key (including an explicit empty list); a path containing spaces without an `args` sibling MUST be rejected at parse time.

### Transport Types and Required Fields

| Transport | Required Field | Notes |
|---|---|---|
| `stdio` | `command` | Single binary path; spaces only allowed if `args` key also present |
| `http` | `url` | Remote endpoint URL |
| `sse` | `url` | Remote endpoint URL |
| `streamable-http` | `url` | Remote endpoint URL |

### Command-with-Spaces Rule

- `command: "npx -y @some/server"` and `args` is absent -> **REJECT**
- `command: "npx"` with `args: ["-y", "@some/server"]` -> **ACCEPT**
- `command: "/opt/My App/server"` with `args: []` (explicit empty list) -> **ACCEPT** (author takes responsibility)
- Key check: `args is None` (not `not args`) -- an explicit empty `args: []` signals deliberate intent.

### Self-Defined Detection

An entry is self-defined when `registry: false`. Registry entries (default, or `registry: true`, or `registry: "some-url"`) skip the transport/command/url checks.

### Python Reference

`MCPDependency.validate()` (mcp.py:218-339): strict mode (`registry is False`) checks:
- `transport` must be present
- `transport == "stdio"` -> `command` required
- `transport in ("http", "sse", "streamable-http")` -> `url` required
- stdio command whitespace check: `any(ch.isspace() for ch in command) and args is None` -> reject

### Already Implemented in Go

`manifest.go:validateDepBlock` (line 296): comment says `// mcp, lsp entries: deferred to Phase 1D`. MCP validation is NOT yet implemented.

### Complexity: MEDIUM

Parsing MCP entries (string vs. object form), distinguishing registry vs. self-defined, validating transport-dependent required fields, and implementing the command-with-spaces rule. The MCPDependency struct itself needs to be defined.

### Dependencies

- No dependency on other Phase 1 reqs (operates on `dependencies.mcp`, not `dependencies.apm`).
- **req-mf-013** (placeholders): env/headers values in MCP entries may contain placeholders, but mf-012 is about structural validation, not placeholder resolution.

### Oracle Fixtures

- **Dedicated**: None.
- **Indirect**: `valid-full.yml` contains a self-defined MCP entry with `transport: stdio`, `command: my-mcp-server`, `args: []`. This would be accepted. But no targeted test for rejection.
- **Gap**: Need fixtures for: missing transport, missing command with stdio, missing url with http/sse, command-with-spaces rejection.

---

## req-mf-013 -- Placeholder dispatch (Section 4.5)

### Spec Text

> Values inside `mcp[].env` and `mcp[].headers` MAY contain three placeholder syntaxes:
>
> | Syntax | Source | Resolution |
> |---|---|---|
> | `${VAR}` | host environment | Normalised to `${env:VAR}` for native interpolation; resolved at install for others. |
> | `${env:VAR}` | host environment | Passed through where natively supported; resolved at install otherwise. |
> | `${input:<id>}` | interactive prompt | Native where supported; otherwise the placeholder MUST NOT be silently rendered as literal text. |
>
> GitHub Actions templates (`${{ ... }}`) MUST be left untouched.
>
> **[req-mf-013]** A conforming consumer implementation MUST resolve `${VAR}`, `${env:VAR}`, and `${input:<id>}` placeholders per the dispatch matrix above and MUST NOT emit a generated config file in which an unsupported placeholder is silently passed through as literal text. When an unsupported placeholder is encountered for the active target, the consumer MUST emit a diagnostic and MAY refuse to write the generated config.

### Dispatch Matrix

| Placeholder | Parse-time (Phase 1) | Config-gen (Phase 4) |
|---|---|---|
| `${VAR}` | Recognize; normalize to `${env:VAR}` | Resolve from host env or pass through if target supports native `${env:}` |
| `${env:VAR}` | Recognize; preserve | Same as above |
| `${input:<id>}` | Recognize; preserve | Native targets (VS Code) pass through; others must not silently render as literal |
| `${{ ... }}` | **Leave untouched** (GitHub Actions template) | Pass through verbatim |

### Phase Boundary

Phase 1 scope: Recognize and validate the three placeholder syntaxes in `env`/`headers` values. Reject malformed placeholders. Leave `${{ }}` untouched.

Phase 4 scope: Actual dispatch to target-specific resolution (the "MUST NOT emit" constraint fires at config-generation time, not parse time).

### Python Reference

`base.py` regexes:
```python
_INPUT_VAR_RE = re.compile(r"\$\{input:([^}]+)\}")
_ENV_VAR_RE = re.compile(r"\$\{(?:env:)?([A-Za-z_][A-Za-z0-9_]*)\}")
```

The `_ENV_VAR_RE` intentionally does NOT match `${input:...}` (the `env:` prefix is optional but the captured group is `[A-Za-z_][A-Za-z0-9_]*` which excludes `input:`). It also does NOT match `${{ ... }}` because the second `{` fails the identifier class.

### Complexity: MEDIUM

Phase 1 parse-time: recognize three syntaxes via regex, leave `${{ }}` alone. Phase 4 config-gen: the "MUST NOT silently emit" enforcement is more complex and target-dependent.

### Dependencies

- **req-mf-012** (MCP validation): placeholders appear inside MCP `env`/`headers` fields; mf-012 must parse MCP entries before mf-013 can validate their values.

### Oracle Fixtures

- **Dedicated**: None.
- **Indirect**: `valid-full.yml` has no placeholder examples in its MCP entry (env is absent).
- **Gap**: Need fixtures with `${VAR}`, `${env:VAR}`, `${input:id}`, and `${{ }}` in MCP env/headers.

---

## req-mf-017 -- Marketplace source validation (Section 4.7)

### Spec Text

> **[req-mf-017]** A conforming **producer** implementation MUST validate every `marketplace.packages[].source` value against the following rules and MUST reject any entry that fails them at parse time: (a) `..` path segments are refused; (b) URL forms with userinfo (`user@host`), ports, or query strings are refused; (c) URL schemes other than `https://` are refused for remote sources; (d) local sources MUST begin with `./`.

### Validation Rules

| Rule | What to check |
|---|---|
| (a) No `..` segments | Reject any `source` containing `..` as a path segment |
| (b) No userinfo/port/query | Reject URLs with `user@`, `:port`, or `?query` |
| (c) HTTPS only for remote | Remote URLs must use `https://` scheme only |
| (d) Local starts with `./` | Local paths must begin with `./` |

### Accepted Source Shapes (from Python regex `SOURCE_RE`)

```
https://<host>/<owner>/<repo>[.git]   -- remote HTTPS
<host>/<owner>/<repo>                  -- host-prefixed shorthand
<owner>/<repo>                         -- bare shorthand
./...                                  -- local path
```

### Python Reference

`marketplace/yml_schema.py` (lines 100-108):
```python
SOURCE_RE = re.compile(
    r"^(?:"
    rf"https://{_HOST_PAT}/{_OWNER_REPO_PAT}(?:\.git)?"
    rf"|{_HOST_PAT}/{_OWNER_REPO_PAT}"
    rf"|{_OWNER_REPO_PAT}"
    r"|\./.*"
    r")$"
)
```

Plus `validate_path_segments()` for traversal defense (rejects `..`).

### Complexity: MEDIUM

Need to parse the `marketplace` block, iterate `packages[].source`, and validate each against the four rules. The marketplace block is a complex structure but for Phase 1 the validation is focused on `source` values.

### Dependencies

- No dependency on other Phase 1 reqs. Marketplace validation is self-contained within the `marketplace` block.
- Note: This is a **producer** requirement (not consumer), but the conformance-kit may still test it.

### Oracle Fixtures

- **Dedicated**: None.
- **Gap**: No marketplace fixture exists in the oracle. Need unit tests.

---

## req-mf-019 -- default_host (Section 4.2.4)

### Spec Text

> **[req-mf-019]** A conforming consumer implementation that encounters a `default_host:` value MUST treat that value as the only host stripped by canonical normalisation per req-mf-009. When the manifest omits `default_host:`, the consumer MAY apply its implementation-default host but MUST document that choice in its conformance statement (see Section 11.2). A consumer MUST NOT strip any host other than the one selected by `default_host:` or the declared implementation-default.

### How It Interacts with mf-009

1. If `default_host: example.com` is set in `apm.yml`, then `example.com` is the ONLY host that canonical normalization strips.
2. If `default_host` is omitted, the implementation's default (for apm-go: `github.com`) is used.
3. The consumer MUST NOT hard-code stripping any specific host -- it must respect the `default_host` value.

### Already Implemented in Go

`manifest.go:86`: `case "default_host": m.DefaultHost = val.Value` -- the field is parsed into `Manifest.DefaultHost`. However, no validation (e.g., format check) is applied, and it's not yet threaded through to normalization logic.

### Complexity: LOW

Parsing is trivial (already done). The complexity is in threading it through to mf-009's normalization logic, which is a mf-009 concern.

### Dependencies

- **req-mf-009** depends on mf-019 to know which host to strip.
- mf-019 is upstream; it just provides the value.

### Oracle Fixtures

- **Dedicated**: None.
- **Indirect**: `valid-full.yml` sets `default_host: github.com`, accepted as `outcome: accept` (tagged mf-005 only).
- **Gap**: No fixture tests behavior when `default_host` is set to a non-github.com value, or when it's omitted.

---

## Summary Matrix

| Req | Section | Complexity | Phase 1 Scope | Key Dependencies | Dedicated Oracle | Indirect Oracle |
|---|---|---|---|---|---|---|
| mf-007 | 4.3.1 | MEDIUM | Parse string deps per ABNF; reject invalid | mf-009, mf-008, mf-016 | None (existing fixture tests object-form only) | valid-full.yml (accept) |
| mf-008 | 4.3.3 | LOW | Classify virtual-path by extension | mf-007 | None | None |
| mf-009 | 4.3.4 | MEDIUM | Canonical normalization at rewrite | mf-019, mf-007 | None | valid-full.yml (has default_host) |
| mf-010 | 4.3.2 | LOW (parse) | Validate `git: parent` + path, mark flag | None (Phase 1); resolver (Phase 2) | None | None |
| mf-012 | 4.3.6 | MEDIUM | Validate self-defined MCP transport/cmd/url | None | None | valid-full.yml (stdio+args) |
| mf-013 | 4.5 | MEDIUM | Recognize placeholders in env/headers | mf-012 | None | None |
| mf-017 | 4.7 | MEDIUM | Validate marketplace source paths/URLs | None | None | None |
| mf-019 | 4.2.4 | LOW | Parse default_host (already done); thread to mf-009 | mf-009 (downstream) | None | valid-full.yml (has field) |

### Existing Go Code State

| Req | What Exists | What Remains |
|---|---|---|
| mf-007 | Object-form key-presence check in `validateDepEntry`; local-path escape in scalar path | Full ABNF string-form grammar validation for scalar entries |
| mf-008 | Not implemented | Virtual package extension classification |
| mf-009 | Not implemented | Canonical normalization logic |
| mf-010 | Not implemented | `git: parent` sentinel parse + validation |
| mf-012 | Comment `// mcp, lsp entries: deferred to Phase 1D` | MCPDependency struct + full validation |
| mf-013 | Not implemented | Placeholder regex recognition in env/headers |
| mf-017 | Not implemented | Marketplace source validation |
| mf-019 | `default_host` parsed into `Manifest.DefaultHost` | Thread value to normalization; no format validation |

### Phase Boundaries

Some reqs have enforcement that spans multiple phases:

- **mf-010**: Parse-time validation (Phase 1) + resolver expansion (Phase 2) + lockfile assertion (Phase 3)
- **mf-013**: Placeholder recognition (Phase 1) + config-gen enforcement "MUST NOT silently emit" (Phase 4)
- **mf-009**: Normalization at manifest rewrite (Phase 1) but also used at lockfile identity construction (Phase 3)
