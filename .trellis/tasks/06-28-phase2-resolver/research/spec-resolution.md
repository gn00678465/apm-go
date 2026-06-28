# Research: Phase 2 -- Dependency Resolution (OpenAPM v0.1 Spec Extraction)

- **Query**: Extract all dependency resolution semantics from OpenAPM v0.1 spec
- **Scope**: internal (spec document)
- **Date**: 2026-06-28
- **Source**: `D:\Projects\apm-dev\apm\docs\src\content\docs\specs\openapm-v0.1.md`

---

## 1. Reference Kind Classification (Section 7.1, req-rs-008)

A consumer MUST classify every dependency declaration into exactly one of five reference kinds, evaluated **in priority order**:

| Priority | Kind | Condition |
|----------|------|-----------|
| 1 | **local** | `local-path-form` strings, or object entries lacking both `git:` and `id:` with a `path:` to the filesystem |
| 2 | **registry** | Object entries with `id:` and (implicit or explicit) `registry:` |
| 3 | **git-semver** | Object or string entries whose `ref:` value matches a semver-range pattern (e.g. `^1.2.0`, `~2.0`, `>=1.0,<2.0`) |
| 4 | **git-literal** | Git URL or shorthand with a literal `ref:` (commit SHA, tag, branch) |
| 5 | **marketplace** | Non-normative in v0.1; producer-side authoring artifact only |

**Key constraint (req-rs-008):**

> A conforming **consumer** implementation MUST classify every dependency by the priority above as a deterministic function of the entry alone (no remote calls, no implementation defaults). Two conforming consumers presented with the same entry MUST produce the same kind classification.

### Relationship to ref-classification (req-rs-003, Section 7.3)

Separately from the five reference kinds above, **req-rs-003** classifies the `ref:` field of any git dependency into three sub-kinds:

1. **semver** -- the ref value parses as a semver range per Section 7.3.1
2. **literal** -- the ref value is a commit SHA, a literal tag (matching `v?\d+\.\d+\.\d+` or a non-semver tag), or a branch name
3. **none** -- the entry has no `ref:` at all

The connection: a git dependency whose `ref:` classifies as `semver` becomes reference kind **git-semver** (priority 3). One whose `ref:` classifies as `literal` becomes **git-literal** (priority 4). One with `ref:` = `none` falls through to git-literal as well (defaults to the repo's default branch).

---

## 2. BFS Resolution Algorithm (Section 7.2, req-rs-001)

### Traversal Order

> A conforming **consumer** implementation MUST resolve dependencies by **breadth-first** traversal of the dependency tree, in the **declaration order** of each manifest.

### Diamond Conflict: Tri-Modal Policy (req-rs-001)

When the same package identity (`<owner>/<repo>` or registry identity) is reached via multiple constraint paths (a "diamond"), the consumer MUST apply:

#### Mode 1: Intersection-pick (default)

> If every reachable constraint for the identity has a non-empty intersection, the consumer MUST select the **highest** version satisfying every constraint in the intersection. The selected version is recorded in the lockfile; the chain that contributed the binding tightest constraint is recorded as `resolved_by`.

#### Mode 2: Empty-intersection fail-closed

> If the intersection of reachable constraints is empty, the install MUST fail with a diagnostic naming both root-to-conflict chains. Silent first-wins resolution MUST NOT be substituted.

#### Mode 3: Nest mode (opt-in, reserved for v0.2)

> The manifest MAY declare `dependencies.conflict_resolution: nest`, which instructs the consumer to allow multiple versions of the same identity co-existing under distinct deploy paths (npm-style nesting). Nest mode is OPTIONAL in v0.1; its on-disk layout normative pin is reserved for v0.2.

**Critical v0.1 rule (req-rs-013):**

> A conforming **consumer** encountering `dependencies.conflict_resolution: nest` in a v0.1 manifest MUST refuse the install with a normative diagnostic naming the key as reserved-for-v0.2 and citing Section 7.2 clause (3).

### Empty-Intersection Diagnostic Format (req-rs-010)

> A conforming **consumer** implementation producing an empty-intersection diagnostic per req-rs-001 clause (2) MUST format the diagnostic so that it lists, for each chain, the ordered sequence of `<owner>/<repo>@<constraint>` entries from the root manifest to the conflicting entry, separated by `->`. Both chains MUST be named; the diagnostic MUST be deterministic for a given install plan.

### Worked Example: 3-Chain Intersection

From the spec (informative):

```yaml
# Manifest:
dependencies:
  apm:
    - acme/foo#^1.2.0     # direct: depth 1, constraint ^1.2.0
    - acme/bar#^2.0.0     # direct: depth 1, constraint ^2.0.0
# acme/bar transitively pulls acme/foo#^1.5.0 (depth 2).
# Intersection of ^1.2.0 and ^1.5.0 is [>=1.5.0, <2.0.0]: pick highest
# tag in [1.5.0, 2.0.0) per req-rs-001 clause (1).
# If acme/bar instead pulled acme/foo#^2.0.0, intersection is empty
# and install fails closed per req-rs-001 clause (2).
```

Lockfile fragment for the 3-chain case (`resolved_by` records the chain contributing the **tightest** constraint):

```yaml
# Three chains reach acme/foo:
#   root -> acme/foo#^1.2.0                       (depth 1, lo=1.2.0)
#   root -> acme/bar#^2.0.0 -> acme/foo#^1.5.0    (depth 2, lo=1.5.0)
#   root -> acme/baz#^3.0.0 -> acme/qux#^1.0.0 -> acme/foo#~1.7.0
#                                                  (depth 3, lo=1.7.0)
# Intersection: [>=1.7.0, <2.0.0]. Pick highest tag in that range.
# The tightest lower bound is contributed by acme/baz -> acme/qux,
# so `resolved_by` records that chain.
dependencies:
  - repo_url: github.com/acme/foo
    resolved_tag: v1.7.4
    constraint: "~1.7.0"
    resolved_by: "acme/baz#^3.0.0 -> acme/qux#^1.0.0 -> acme/foo#~1.7.0"
    depth: 3
```

---

## 3. Semver Dialect Details (Section 7.3.1, req-rs-007)

### Normative Dialect

> OpenAPM v0.1 pins the semver-range dialect to **node-semver** with version precedence and pre-release ordering inherited from **Semantic Versioning 2.0.0** Section 11.

**req-rs-007:**

> A conforming **consumer** implementation MUST evaluate every semver range expression in a manifest or lockfile under the **node-semver** dialect as pinned in Section 7.3.1. No implementation-defined hedging is permitted.

### Range Operators (normative)

| Operator | Semantics |
|----------|-----------|
| `^x.y.z` | Compatible-with-X: `>= x.y.z, < (x+1).0.0` when `x > 0`; `>= 0.y.z, < 0.(y+1).0` when `x == 0` and `y > 0`; `>= 0.0.z, < 0.0.(z+1)` when `x == 0` and `y == 0` |
| `~x.y.z` | Approximately equivalent: `>= x.y.z, < x.(y+1).0`. `~x.y` (no patch) is equivalent to `>= x.y.0, < x.(y+1).0` |
| `>=`, `>`, `<=`, `<`, `=` | Comparator-form: standard inequality on semver precedence |
| `x.y.z`, `*` | Wildcard: any version (subject to pre-release exclusion) |
| Range list (comma or whitespace) | Logical AND: `>=1.0.0, <2.0.0` matches versions satisfying both comparators |
| `\|\|` | Logical OR: `^1 \|\| ^2` matches versions satisfying either range list |
| `x.y.z - a.b.c` | Hyphen range: equivalent to `>= x.y.z, <= a.b.c` |

### `0.x` Quirk (critical for implementation)

> Per semver 2.0.0 Section 4, anything `0.x.y` is considered unstable; the caret operator narrows accordingly: **`^0.2.3` matches `>= 0.2.3, < 0.3.0`**, NOT `>= 0.2.3, < 1.0.0`. This is the node-semver convention and is adopted normatively.

### Build Metadata (req-rs-014)

> Build metadata (anything after `+` in a version) MUST be ignored for precedence comparisons per semver 2.0.0 Section 10. Two versions differing only in build metadata compare as equal.

**Tie-breaking rule (req-rs-014):**

> When two candidate tags have equal precedence under semver 2.0.0 Section 11 (i.e. they differ only in build-metadata identifier), a conforming **consumer** MUST select the tag whose name compares highest under **bytewise ASCII ordering** of the full tag string. This rule eliminates non-determinism in build-metadata ties.

### Pre-release Ordering

> Pre-release ordering follows semver 2.0.0 Section 11: numeric identifiers compare numerically, ASCII alphanumeric identifiers compare lexicographically in ASCII order, numeric identifiers always have lower precedence than alphanumeric identifiers, and a larger set of pre-release fields has higher precedence than a smaller set when all preceding fields are equal.

### Pre-release Opt-in (normative, two conditions)

A pre-release tag MAY be selected **only when at least one** of the following is true:

1. **Range-based opt-in:** The manifest range expression itself contains a pre-release identifier on the **same `[major, minor, patch]` tuple** as the candidate tag. Example: `>=1.2.0-alpha <1.3.0` permits `1.2.0-beta` and `1.2.0`, but does NOT permit `1.3.0-alpha`.

2. **Explicit opt-in:** The manifest dependency entry declares `prerelease: true` (explicit opt-in across the whole range).

> When neither (1) nor (2) holds, every candidate tag with a non-empty pre-release identifier MUST be discarded from the candidate set before highest-match selection.

### Determinism

> The selection function is a deterministic function of (range expression, candidate-tag set, opt-in signal). Two conforming consumers presented with the same inputs MUST select the same tag.

---

## 4. Git-Semver Resolution Flow (Section 7.3, req-rs-002)

The full resolution procedure for a git-semver dependency:

1. List the remote git tags of the repository
2. Dereference annotated tags to their peeled commit object (lightweight and annotated tags treated equivalently thereafter)
3. Discard any tag whose name fails to parse under the semver dialect of Section 7.3.1 **without diagnostic**
4. Filter the remainder to those matching the manifest's semver range under the same dialect
5. Pin the **highest** matching tag in the lockfile
6. Pre-release tags MUST be excluded from selection unless explicit opt-in is signalled per Section 7.3.1
7. The selected tag, the original constraint, and the resolution timestamp MUST be written per req-lk-008

---

## 5. Lockfile Replay Semantics (Section 7.5, req-rs-004 + req-lk-009)

### Character-Level Equality Rule

**req-rs-004:**

> A conforming **consumer** implementation MUST treat a manifest entry whose `ref:` is a semver range as equivalent to its locked counterpart (no drift) when, and only when, the locked `constraint` value is **character-equal** to the manifest's current range. Any difference, **including whitespace**, MUST trigger re-resolution.

**req-lk-009:**

> A conforming **consumer** implementation MUST replay a previously locked git-semver resolution (reusing the locked `resolved_tag`) when the manifest's current semver constraint is **equal** to the locked `constraint`. A different manifest constraint MUST trigger re-resolution against the remote.

### Lockfile Fields for git-semver (req-lk-008)

A conforming consumer MUST record on every git-semver lockfile entry:

| Field | Rule |
|-------|------|
| `constraint` | The original semver range from the manifest (verbatim) |
| `resolved_tag` | The literal tag string the range resolved to |
| `resolved_at` | ISO 8601 UTC timestamp of the resolution event; advisory, MUST NOT be used as tie-breaker in replay |

### Update Purge Rule (req-lk-010)

> A conforming **consumer** implementation MUST, when performing an explicit update operation against a direct git-semver dependency, purge the dependency's install path before re-resolving so that the download callback re-runs even when the resolved tag is unchanged. This guards against the regression where a cached install path masks a re-resolution event.

### Mirror Resolution (Section 7.5.1, req-rs-009)

> Trust is anchored on the recorded `resolved_hash`, not on the recorded `resolved_url`. A mismatch between the mirror URL and `resolved_url` MUST NOT fail the install when the hash matches. A hash mismatch MUST fail closed per req-lk-013, regardless of which registry served the bytes.

---

## 6. Update Semantics (Section 7.7)

### Full Update (req-rs-011) -- `apm update` without package argument

> A conforming **consumer** implementation MUST, when invoked without a package argument:
> 1. Re-resolve every direct dependency against its **current** manifest constraint (holding the manifest unchanged)
> 2. Rewrite the lockfile pins to the new highest matching version for each direct dep
> 3. Re-resolve all transitive dependencies as a side-effect
> 4. Honour the active Governance policy's `require_pinned_constraint` rule (req-pl-007)

### Scoped Update (req-rs-012) -- `apm update <name>`

> A conforming **consumer** implementation MUST:
> 1. Scope re-resolution to the named package and its subtree only
> 2. Hold every other resolved entry at its prior pin
> 3. Refuse to operate on a frozen install (see req-lk-006) without an explicit override flag

### Reserved

> Range-widening update modes (for example `apm update --aggressive`, which would mutate the manifest's range upper bounds) are **reserved for v0.2**.

---

## 7. Depth Limit (Section 7.2, req-rs-006)

> A conforming **consumer** implementation MUST stop transitive resolution at a configurable depth cap whose default value is **50**. The Governance class MAY tighten this cap via `policy.dependencies.max_depth` (see Section 6.3.1). Exceeding the cap MUST cause the install to fail with a diagnostic naming the chain at which the cap was reached.

---

## 8. "Why" Diagnostic (Section 7.6, req-rs-005)

> A conforming **consumer** implementation that exposes a "why is this dependency present" diagnostic command MUST compute the answer by walking the lockfile **bottom-up** from the target entry to the root, returning the set of root-to-target chains that include the target. Chains MUST be returned in **lexicographic order** of the root-to-target path tuple. The walker MUST operate offline against the lockfile alone, MUST be safe against cycles (no infinite recursion), and MUST produce deterministic output for a given lockfile.

---

## 9. Frozen Install (Section 5.5, req-lk-006 + req-lk-018)

### req-lk-006

> A conforming **consumer** implementation MUST support a frozen-install mode in which the lockfile is never written or rewritten and the install fails on any direct dependency for which the lockfile has no pin. The frozen-install operation is opt-in in v0.1 via `--frozen` (or equivalent).

### CI Default (req-lk-018)

> A conforming **consumer** implementation SHOULD default to frozen-install behaviour when the `CI` environment variable is truthy (defined as: present and not the literal strings `""`, `"0"`, `"false"`, case-insensitive).

---

## 10. Conflict Resolution: Nest Rejection (v0.1 reserved, req-rs-013)

> A conforming **consumer** implementation MUST refuse to install a v0.1 manifest declaring `dependencies.conflict_resolution: nest`, emitting a normative diagnostic that names the `conflict_resolution: nest` key as reserved for v0.2 and cites Section 7.2 clause (3).

---

## 11. Dependency Reference ABNF (Section 4.3.1)

```abnf
dependency       = url-form / shorthand-form / local-path-form

url-form         = url-scheme clone-url
url-scheme       = "https://" / "http://" / "ssh://git@" / "git@"
clone-url        = host [ ":" port ] "/" owner "/" repo
                   [ "/" virtual-path ] [ "#" ref ]

shorthand-form   = [ host "/" ] owner "/" repo
                   [ "/" virtual-path ] [ "#" ref ]

local-path-form  = local-prefix path-tail
local-prefix     = "./" / "../" / "/" / "~/" / ".\" / "..\" / "~\"
path-tail        = 1*pchar

host             = 1*( ALPHA / DIGIT / "-" / "." )
port             = 1*DIGIT       ; range 1-65535
owner            = 1*( ALPHA / DIGIT / "-" / "_" )
repo             = 1*( ALPHA / DIGIT / "-" / "_" / "." )
virtual-path     = segment *( "/" segment )
segment          = 1*( ALPHA / DIGIT / "-" / "_" / "." )
ref              = 1*VCHAR
pchar            = ALPHA / DIGIT / "/" / "\" / ":" / "." / "-" / "_" / "~"
```

### Object Form Identity Keys (Section 4.3.2)

| Field | Required | Notes |
|-------|----------|-------|
| `git` | yes for git-sourced; mutually excl. `id` | Clone URL or shorthand. Special value `parent` defined below. |
| `id` | yes for registry-sourced; mutually excl. `git` | `<owner>/<repo>` registry identity |
| `registry` | no | Registry name; defaults to project default if omitted |
| `version` | yes (registry form) | Opaque version selector; semver range when registry publishes semver |
| `ref` | no | Branch, tag, semver range, or commit SHA (git form) |
| `path` | no / yes (local form) | Subpath within repo, or local filesystem path |
| `alias` | no | Local alias |
| `skills` | no | Skill-subset selection for skill collections |

**req-mf-011:** Both `id:` and `git:` on the same entry MUST be rejected.

**req-mf-010:** The literal sentinel `git: parent` is valid only inside a transitively resolved package whose clone coordinates are known. The resolver MUST expand `parent` to the parent package's `host`, `repo_url`, and resolved `ref`.

---

## 12. Producer Release Contract (Section 7.8, req-pr-004)

> A conforming **producer** publishing a git tag intended for consumption via git-semver MUST ensure that the tag points at a commit whose `apm.yml` `version` field is equal to the tag (modulo an OPTIONAL leading `v` prefix). For example, the tag `v2.3.1` MUST point at a commit whose `apm.yml` contains `version: "2.3.1"`.

Tag name MUST match:
```
^v?(0|[1-9]\d*)\.(0|[1-9]\d*)\.(0|[1-9]\d*)(-((0|[1-9]\d*|\d*[a-zA-Z-][0-9a-zA-Z-]*)(\.(0|[1-9]\d*|\d*[a-zA-Z-][0-9a-zA-Z-]*))*))?(\+([0-9a-zA-Z-]+(\.[0-9a-zA-Z-]+)*))?$
```

---

## 13. Conformance Requirement Index (Section 7.11)

All resolution-related req-IDs:

| Req ID | Section | Subject |
|--------|---------|---------|
| req-rs-001 | 7.2 | BFS traversal + tri-modal diamond policy |
| req-rs-002 | 7.3 | git-semver tag listing and highest-match selection |
| req-rs-003 | 7.3 | ref classification (semver / literal / none) |
| req-rs-004 | 7.5 | Lockfile replay: character-equal constraint check |
| req-rs-005 | 7.6 | "why" diagnostic: bottom-up lockfile walk |
| req-rs-006 | 7.2 | Depth cap: default 50, governance override |
| req-rs-007 | 7.3 | node-semver dialect pinning |
| req-rs-008 | 7.1 | Deterministic reference-kind classification |
| req-rs-009 | 7.5.1 | Mirror resolution: hash-anchored trust |
| req-rs-010 | 7.2 | Empty-intersection diagnostic chain format |
| req-rs-011 | 7.7 | Full update: re-resolve all direct deps |
| req-rs-012 | 7.7 | Scoped update: named package + subtree only |
| req-rs-013 | 7.2 | Nest mode: refuse in v0.1 with diagnostic |
| req-rs-014 | 7.3.1 | Build-metadata tie-breaking: bytewise ASCII |
| req-pr-004 | 7.8 | Producer tag-to-version alignment |
| req-pr-005 | 7.8 | Producer tag signing (SHOULD) |
| req-lk-008 | 5.6 | Record constraint + resolved_tag + resolved_at |
| req-lk-009 | 5.6 | Replay locked tag when constraint unchanged |
| req-lk-010 | 5.6 | Purge install path on explicit update |

---

## Caveats / Not Found

- **Nest mode on-disk layout**: Not defined in v0.1; reserved for v0.2 workspaces semantics (Section 4.8)
- **Range-widening update**: `apm update --aggressive` reserved for v0.2
- **Version withdrawal**: yank/deprecate/supersede reserved for v0.2 (Section 7.9)
- **Registry wire contract**: Not normative in v0.1 (Appendix B)
- **`policy.dependencies.max_depth`**: Exact policy field shape defined in Section 6.3.1, not extracted here (governance layer detail)
