# Research: Phase 2 Dependency Resolution - Semver Libraries

- **Query**: Which Go library can pass the semver-dialect.json oracle, or do we need a custom implementation?
- **Scope**: mixed (internal oracle + external library research)
- **Date**: 2026-06-28

---

## Task 1: Oracle Analysis (semver-dialect.json)

**File**: `D:\Projects\apm-dev\conformance-kit\oracle\resolution\semver-dialect.json`

The oracle covers three categories, 24 total test cases:

### range_match (20 test cases)

| # | Range | Version | Expected | Category |
|---|---|---|---|---|
| 01 | `^1.2.3` | `1.2.3` | true | caret basic |
| 02 | `^1.2.3` | `1.9.0` | true | caret basic |
| 03 | `^1.2.3` | `2.0.0` | false | caret major boundary |
| 04 | `^0.2.3` | `0.2.9` | true | caret 0.x narrowing |
| 05 | `^0.2.3` | `0.3.0` | false | caret 0.x narrowing |
| 06 | `^0.0.3` | `0.0.3` | true | caret 0.0.x pin |
| 07 | `^0.0.3` | `0.0.4` | false | caret 0.0.x pin |
| 08 | `~1.2.3` | `1.2.9` | true | tilde basic |
| 09 | `~1.2.3` | `1.3.0` | false | tilde minor boundary |
| 10 | `~1.2` | `1.2.0` | true | tilde partial |
| 11 | `~1.2` | `1.3.0` | false | tilde partial boundary |
| 12 | `>=1.0.0 <2.0.0` | `1.5.0` | true | explicit range |
| 13 | `>=1.0.0 <2.0.0` | `2.0.0` | false | explicit range boundary |
| 14 | `1.2.3 - 1.5.0` | `1.5.0` | true | hyphen range inclusive |
| 15 | `1.2.3 - 1.5.0` | `1.5.1` | false | hyphen range boundary |
| 16 | `^1 \|\| ^2` | `2.3.0` | true | OR range |
| 17 | `*` | `9.9.9` | true | wildcard |
| 18 | `^1.0.0` | `1.5.0-alpha` | false | prerelease excluded (no opt-in) |
| 19 | `>=1.2.0-alpha <1.3.0` | `1.2.0-beta` | true | same-tuple prerelease opt-in |
| 20 | `>=1.2.0-alpha <1.3.0` | `1.3.0-alpha` | false | different-tuple prerelease rejected |

### Edge cases covered

- **Caret 0.x narrowing**: `^0.2.3` = `>=0.2.3 <0.3.0`, `^0.0.3` = `>=0.0.3 <0.0.4` (cases 04-07)
- **Prerelease same-tuple rule**: Only versions with the same `[major, minor, patch]` as a comparator with a prerelease are allowed to match (cases 18-20)
- **Hyphen ranges**: `1.2.3 - 1.5.0` is `>=1.2.3 <=1.5.0` (cases 14-15)
- **OR (||) ranges**: `^1 || ^2` (case 16)

### tag_selection (3 test cases)

| # | Range | Tags | Selected | Note |
|---|---|---|---|---|
| 21 | `^1.2.0` | v1.2.0, v1.3.5, v1.9.9, v2.0.0 | v1.9.9 | highest match |
| 22 | `~1.2.0` | v1.2.0, v1.2.7, v1.3.0 | v1.2.7 | tilde constrains minor |
| 23 | `^1.0.0` | v1.0.0, v1.5.0-rc.1 | v1.0.0 | rc excluded |

These require: range matching + version sorting + highest-matching selection + prerelease exclusion from non-prerelease ranges.

### build_metadata_tie (1 test case)

| # | Tags | Selected | Rule |
|---|---|---|---|
| 24 | v1.0.0+build.1, v1.0.0+build.2 | v1.0.0+build.2 | req-rs-014: bytewise ASCII highest |

This is a library-independent gap. Per semver 2.0.0 spec, build metadata MUST be ignored in version precedence. No SemVer-2.0-compliant library will ever implement this. The tie-break is a post-filter on raw tag strings that the resolver always has in hand -- a simple bytewise ASCII comparison of the build metadata suffix. Required regardless of library choice.

---

## Task 2: Go Semver Library Research

### Libraries Evaluated

#### 1. deps.dev/util/semver (Google)

- **Module**: `deps.dev/util/semver` (go-gettable, confirmed: `v0.0.0-20260617025149-7d3577045631`)
- **License**: Apache-2.0
- **Source**: https://github.com/google/deps.dev (monorepo, `util/semver/` subdirectory)
- **Dependencies**: zero (stdlib only)
- **Multi-system**: NPM, Cargo, PyPI, RubyGems, Maven, NuGet, Go

**Oracle Results (empirically tested via harness)**:
- range_match: **20/20 pass**
- tag_selection: **3/3 pass** (using `Match` + manual sort via `Compare`)
- build_metadata_tie: N/A (library-independent gap, custom post-filter needed)

**How it works**:
- `depsdev.NPM.ParseConstraint(rangeStr)` returns a `*Constraint`
- `constraint.Match(versionStr)` returns bool
- `depsdev.NPM.Parse(versionStr)` returns `*Version` for manual comparison
- Prerelease same-tuple matching is implemented in `span.contains()` (file `span.go:178-217`): it checks that the version's `[major,minor,patch]` equals either the span's min or max, AND that endpoint has a prerelease tag
- Supports all node-semver operators: `^`, `~`, `||`, hyphen ranges, wildcards, comparisons

**Gaps**:
- No built-in `MaxSatisfying` helper -- must filter + sort manually
- Published as part of Google's deps.dev monorepo, not a dedicated semver package (but go-gettable as a standalone module)

#### 2. Masterminds/semver v3

- **Module**: `github.com/Masterminds/semver/v3` (v3.5.0)
- **Stars**: 1,423
- **License**: MIT
- **Dependencies**: zero (stdlib only)
- **Actively maintained**: Yes (last updated 2026-06-26)

**Oracle Results (empirically tested via harness)**:
- range_match: **19/20 pass** (1 failure)
- tag_selection: **3/3 pass** (using `Check` + `GreaterThan`)
- build_metadata_tie: N/A (library-independent gap, inferred same as deps.dev)

**Failing case**:
- Case 20: `>=1.2.0-alpha <1.3.0` vs `1.3.0-alpha` -- expected `false`, got `true`
- Root cause: Masterminds uses "any prerelease in AND group enables ALL prereleases in range" instead of node-semver's "same-tuple" rule. When `>=1.2.0-alpha` has a prerelease, `containsPre[i]` becomes true for the entire AND group, allowing `1.3.0-alpha` to match even though its tuple `[1,3,0]` differs from the comparator's `[1,2,0]`.
- Code location: `constraints.go` line ~104: `c.check(v, (cs.IncludePrerelease || cs.containsPre[i]))`

**Strengths**:
- Most popular Go semver library
- Explicitly designed for npm/Cargo range compatibility (README states this)
- `IncludePrerelease` property on `Constraints` struct
- All range operators supported: `^`, `~`, `||`, hyphen, wildcards, comparisons
- Correct `^0.x` narrowing behavior

#### 3. blang/semver v4

- **Module**: `github.com/blang/semver/v4`
- **Stars**: 1,049
- **License**: MIT
- **Actively maintained**: Stable/maintenance mode

**Assessment (from documentation, not empirically tested)**:
- Supports: basic comparisons (`>`, `>=`, `<`, `<=`, `=`, `!=`), `||`, wildcards
- **Does NOT support**: `^` (caret), `~` (tilde), hyphen ranges
- **Disqualified**: Cannot parse ~14 of 20 oracle range_match test cases (all caret cases 1-7, tilde cases 8-11, hyphen cases 14-15, OR-with-caret case 16)

#### 4. CodeClarityCE/utility-node-semver

- **Module**: `github.com/CodeClarityCE/utility-node-semver`
- **Stars**: 0
- **License**: AGPL-3.0 (restrictive)
- **Last updated**: 2026-02-08

**Assessment (from source review, not empirically tested)**:
- Purpose-built node-semver implementation with `Satisfies`, `MaxSatisfying` helpers
- Supports NodeJS and Composer ecosystems
- Has prerelease same-tuple logic in `evaluator/Evaluator.go`, but with a suspected bug: checks `v.EQ(cRange.EndVersion, true)` which may allow matching against a range endpoint even when that endpoint has no prerelease tag. Would likely fail case 20 similar to Masterminds.
- AGPL-3.0 license makes it impractical for most projects
- Zero community adoption

#### 5. Other Discoveries

- **microsoft/typescript-go** (`internal/semver/`): Internal implementation, not importable
- **google/osv-scalibr** (`semantic/version-semver.go`): Uses semver for vulnerability matching, different use case
- GitHub searches for "node-semver golang", "node semver go", "npm semver golang" returned no other dedicated Go implementations

---

## Summary: Oracle Pass Rates

| Library | range_match (20) | tag_selection (3) | build_metadata_tie (1) | Importable | License | Method |
|---|---|---|---|---|---|---|
| deps.dev/util/semver | **20/20** | **3/3** | custom needed | Yes | Apache-2.0 | empirical |
| Masterminds/semver v3 | 19/20 | **3/3** | custom needed | Yes | MIT | empirical |
| blang/semver v4 | ~6/20 | not tested | custom needed | Yes | MIT | documented |
| CodeClarityCE/node-semver | ~19/20 | not tested | custom needed | Yes | AGPL-3.0 | source review |

Note: build_metadata_tie is a library-independent requirement. It is a post-filter on raw tag strings (bytewise ASCII comparison), not something any SemVer-2.0-compliant library should implement.

---

## Answer: Which Library Can Pass the Oracle?

**Best oracle fit: `deps.dev/util/semver`** -- passes 23/23 library-addressable cases (20/20 range_match + 3/3 tag_selection). The remaining 1 case (build_metadata_tie) is a library-independent custom step.

**Runner-up: `Masterminds/semver v3`** -- passes 22/23 library-addressable cases. Fails case 20 (prerelease same-tuple edge case). Could be fixed with a ~20-30 line wrapper around `Constraints.Check()` that pre-checks the same-tuple rule before delegating.

**Custom implementation is NOT needed.** deps.dev covers all node-semver semantics tested by the oracle. The only custom code needed is:
1. A `MaxSatisfying` helper (filter matching versions, sort, pick highest) -- straightforward with `Match` + `Compare`
2. Build-metadata tie-breaking via bytewise ASCII comparison of the `+...` suffix

---

## Harness Location

The throwaway verification harness is at:
`C:/Users/gn006/AppData/Local/Temp/claude/D--Projects-apm-dev-apm-go/52f23c3d-ecd5-4e40-bc08-48b3ab0c8a60/scratchpad/harness/main.go`

It can be re-run against updated oracle files:
```
go run . <path-to-semver-dialect.json>
```
