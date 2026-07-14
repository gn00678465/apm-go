# Codex adversarial verification — Phase 0–1

Date: 2026-07-12  
Oracle sources read directly (claims were not trusted):

- `D:/Projects/apm-dev/apm/src/apm_cli/security/content_scanner.py`
- `D:/Projects/apm-dev/apm/src/apm_cli/security/gate.py`
- `D:/Projects/apm-dev/apm/src/apm_cli/core/apm_yml.py:25-108`
- Current apm-go implementation and test bodies under `internal/manifest/` and `internal/security/`

## VERDICT: FAIL 2處

Phase 1's scanner range table and three special rules match the Python oracle. The two failures are in the Phase 0 target/targets decision tree.

## FAIL findings

### FAIL 1 — mutex error is not evaluated before either value branch

Python computes key presence and raises `ConflictingTargetsError` before reading either value (`apm_yml.py:53-58`). Go parses whichever key appears first and only notices the conflict when it reaches the second key (`internal/manifest/manifest.go:112-128`). An invalid first value therefore masks the schema conflict.

Adversarial fixtures:

| YAML shape | Python | apm-go |
|---|---|---|
| `targets: []` then `target: claude` | `ConflictingTargetsError` | empty `targets:` error |
| `target: bogus` then `targets: [claude]` | `ConflictingTargetsError` | `unknown target "bogus"` |

The existing conflict test is not discriminating: `internal/manifest/manifest_test.go:430-442` uses only valid values and asserts merely `err != nil`, so either the correct mutex error or an unrelated value error passes.

### FAIL 2 — mapping elements inside target lists are silently accepted

Python stringifies every list element before filtering and canonical validation (`apm_yml.py:82-84,94-98`); a mapping becomes a non-empty string and is rejected as an unknown target. Go copies `yaml.Node.Value` without checking the element kind (`internal/manifest/manifest.go:251-256,278-281`). A mapping node has an empty `Value`, so `validateTargetTokens` drops it as blank and the manifest succeeds.

Adversarial fixtures:

| YAML shape | Python | apm-go |
|---|---|---|
| `targets: [{foo: bar}]` | `UnknownTargetError` | exit 0 |
| `target: [{foo: bar}]` | `UnknownTargetError` | exit 0 |

This also contradicts the implementation comment that each list element is validated.

## Scanner range table — 35/35 exact

An independent AST/regex extractor compared ordered tuples `(start, end, severity, category, description)`. Both sides contain 35 rows; there are zero missing rows, boundary mismatches, severity mismatches, category mismatches, description mismatches, or order mismatches.

| # | Inclusive range | Severity | Python line | Go line | Result |
|---:|---|---|---:|---:|---|
| 1 | U+E0001..U+E007F | critical | 36-42 | 51 | MATCH |
| 2 | U+202A | critical | 44 | 52 | MATCH |
| 3 | U+202B | critical | 45 | 53 | MATCH |
| 4 | U+202C | critical | 46 | 54 | MATCH |
| 5 | U+202D | critical | 47 | 55 | MATCH |
| 6 | U+202E | critical | 48 | 56 | MATCH |
| 7 | U+2066 | critical | 49 | 57 | MATCH |
| 8 | U+2067 | critical | 50 | 58 | MATCH |
| 9 | U+2068 | critical | 51 | 59 | MATCH |
| 10 | U+2069 | critical | 52 | 60 | MATCH |
| 11 | U+E0100..U+E01EF | critical | 57-63 | 65 | MATCH |
| 12 | U+200B | warning | 65 | 67 | MATCH |
| 13 | U+200C | warning | 66 | 68 | MATCH |
| 14 | U+200D | warning | 67 | 69 | MATCH |
| 15 | U+2060 | warning | 68 | 70 | MATCH |
| 16 | U+FE00..U+FE0D | warning | 70-76 | 72 | MATCH |
| 17 | U+FE0E | warning | 77 | 73 | MATCH |
| 18 | U+00AD | warning | 78 | 74 | MATCH |
| 19 | U+200E | warning | 80 | 76 | MATCH |
| 20 | U+200F | warning | 81 | 77 | MATCH |
| 21 | U+061C | warning | 82 | 78 | MATCH |
| 22 | U+2061 | warning | 84-90 | 80 | MATCH |
| 23 | U+2062 | warning | 91 | 81 | MATCH |
| 24 | U+2063 | warning | 92 | 82 | MATCH |
| 25 | U+2064 | warning | 93 | 83 | MATCH |
| 26 | U+FFF9 | warning | 95 | 85 | MATCH |
| 27 | U+FFFA | warning | 96 | 86 | MATCH |
| 28 | U+FFFB | warning | 97 | 87 | MATCH |
| 29 | U+206A..U+206F | warning | 99 | 89 | MATCH |
| 30 | U+FE0F | info | 102 | 92 | MATCH |
| 31 | U+00A0 | info | 103 | 93 | MATCH |
| 32 | U+2000..U+200A | info | 104 | 94 | MATCH |
| 33 | U+205F | info | 105 | 95 | MATCH |
| 34 | U+3000 | info | 106 | 96 | MATCH |
| 35 | U+180E | info | 107 | 97 | MATCH |

`buildCharLookup` also uses `cp <= end`, matching Python's inclusive `range(start, end + 1)`.

## Three special scanner rules

All three implementation rules match `content_scanner.py` and the selected tests passed with `-count=1`.

| Rule | Source comparison | Assertion body inspected | Dynamic result |
|---|---|---|---|
| ZWJ U+200D between two emoji → info; outside emoji → warning | `scanner.go:131-147,205-209` matches Python's backward VS16/skin-tone skip and immediate forward emoji check | `scanner_test.go:116-158` asserts exactly one finding, info/expected description for emoji, and warning/zero-width outside emoji; skin-tone case is separate | PASS |
| BOM U+FEFF at file start → info/bom; elsewhere → warning/zero-width | `scanner.go:180-202` matches Python's `line_idx == 0 && col_idx == 0` special case | `scanner_test.go:160-198` asserts start, mid-line, and start-of-second-line behavior and positions | PASS |
| Pure ASCII fast path | `scanner.go:152-159,164-171` checks bytes against `unicode.MaxASCII` before line/rune allocation | `scanner_test.go:15-45`: the ScanText result assertion is paired with direct `isASCII` assertions for ASCII, empty, non-ASCII, U+200B, U+0080, and U+007F; this avoids relying only on the otherwise-tautological empty finding result | PASS |

Targeted command:

```text
go test ./internal/security/... -run 'Test(IsASCII|ScanText_(PureASCIIFastPathReturnsNil|ZWJBetweenEmojiIsInfo|ZWJOutsideEmojiContextIsWarning|ZWJSkipsSkinToneModifierWhenLookingBackward|BOMAtFileStartIsInfo|BOMMidLineIsWarning|BOMAtSecondLineStartIsStillWarning|SuspiciousCategories))$' -v -count=1
PASS
```

The 35-row category fixture covers one representative per range, including all nine bidi override/isolate entries. Exact range endpoints were established by the independent source-table extraction above, not inferred from this representative-only test.

## SecurityGate comparison

The Phase 1 contract listed in `implement.md` is implemented equivalently:

- `ScanPolicy.EffectiveBlock` and `BlockPolicy`/`WarnPolicy`/`ReportPolicy`: match.
- `ScanVerdict` counts, `AllFindings`, and verdict construction: match for the claimed fields.
- `ScanFiles`: complete walk, symlink skip, unreadable-entry continuation, relative portable path, file count: source logic matches.
- `ScanText`: one virtual file scanned, no force override, same verdict construction: match.

Gate tests passed except `TestSecurityGate_ScanFiles_SkipsSymlinks`, which is hard-coded to skip on Windows (`gate_test.go:84-113`). Source comparison confirms the skip logic, but this run does not claim a dynamic Windows symlink proof.

Python's additional `ScanVerdict.has_findings`, `SecurityGate.report`, `ignore_symlinks`, and `ignore_non_content` are absent from `gate.go`. They are not listed in this task's Phase 1 implementation contract and are therefore recorded as out-of-claim surface, not counted among the two FAIL findings. If `gate.go` is intended to claim whole-file parity, that broader claim is false.

## Phase 0 named decision-tree cases

| Case | Result |
|---|---|
| `targets: [claude, copilot]` | PASS |
| `targets: claude` scalar sugar | PASS |
| `target: "claude,copilot"` CSV sugar, including spaces | PASS |
| valid `target:` + `targets:` conflict, both key orders | PASS |
| `targets: []` and `targets: null` reject | PASS |
| `target: []` and `target: null` silently produce zero targets | PASS (temporary CLI fixtures included the exact `target: []` case) |
| `kiro` accepted by `ValidateTarget` | PASS; manifest validation emits the existing no-adapter warning, not an error |
| invalid first value under a two-key conflict | **FAIL 1** |
| mapping element inside singular/plural list | **FAIL 2** |

The Go-only `all`, `antigravity`, aliases, and `x-vendor` target support were intentionally retained by the design; they are extensions, not counted as Phase 0 regressions.

## Test sensitivity / red-light reproduction

Authorized mutation check:

1. `git stash push -- internal/manifest/manifest.go`
2. Target decision-tree tests failed red in 8 cases (plural parsing, empty/null rejection, CSV, null auto-detect, conflict).
3. `git stash pop` succeeded.
4. `manifest.go` hash before and after was identical: `5efe4d46c36a4f6776b21d636ce9afcb720d53fb`.

This proves the existing happy-path tests are coupled to the implementation, but it does not cure the two missing adversarial assertions described above.

## Required build / vet / test / coverage

```text
go build ./internal/manifest/... ./internal/security/...
PASS

go vet ./internal/manifest/... ./internal/security/...
PASS

go test ./internal/manifest/... ./internal/security/... -cover -count=1
internal/manifest  coverage: 86.5% of statements
internal/security  coverage: 98.3% of statements
PASS
```

`git diff --check` passed. `gofmt -l` reports the repository's existing CRLF-formatted manifest files (including `manifest.go`) as whole-file line-ending differences; the newly added security files and changed target/test files were not listed. No formatting rewrite was performed because this verification was prohibited from changing program code.
