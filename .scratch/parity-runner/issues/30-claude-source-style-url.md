# 30 — `pack --claude-source-style url`

**What to build:** Add an apm-go-only `--claude-source-style [github|url]` selector to `pack`, preserving the Oracle's GitHub shorthand by default while allowing HTTPS GitHub source URLs. **Status:** CLOSED (2026-08-28).

**Origin:** Claude Code's `github` source form installs over SSH. Consumers without a GitHub SSH key observed `Permission denied (publickey)`. The pinned Oracle's `output_mappers.py:247-257` emits GitHub shorthand for every github.com package and exposes no general URL-form selector, so this is an intentional apm-go-only superset; the default remains byte-identical.

## Acceptance criteria

- [x] `--claude-source-style github` is the default and retains the Oracle's `{"source":"github","repo":"owner/repo"}` shape.
- [x] `url` maps github.com plain packages to `{"source":"url","url":"https://github.com/<owner>/<repo>"}`.
- [x] `url` maps github.com packages with a subdirectory to `git-subdir` with the same HTTPS URL; non-default hosts and local packages are unchanged.
- [x] ref/sha/tag_pattern values and JSON key order remain unchanged.
- [x] The style reaches normal writes, dry-runs, JSON output, and `--check-clean`; same-style checks pass and differing styles report drift with exit code 4.
- [x] Unknown values use the pack selector usage-error contract with exit code 2.
- [x] The pinned 80-case Oracle corpus has no new default-style differences; no Oracle case is added for this apm-go-only flag.

## Implementation

- `internal/marketplace/build/mapper.go`: added a Claude source-style option with a zero-value GitHub default and the HTTPS GitHub mapping.
- `internal/marketplace/build/output.go` and `drift_check.go`: threaded mapper options through the shared compose dispatch and drift gate.
- `cmd/apm-go/pack.go`: added and validated the apm-go-only selector, threading it through producer and release-gate paths.

## Tests

- `internal/marketplace/build/mapper_claude_source_style_test.go` covers both styles across GitHub plain/subdirectory and non-default-host plain/subdirectory sources with exact JSON bytes.
- `cmd/apm-go/pack_claude_source_style_test.go` covers two different GitHub owners, default-byte identity, normal/dry-run/JSON/check-clean behavior, and the unknown-value usage error.
- Final verification includes formatting, vet, race tests, the 80-case pinned corpus, and real binary execution.

## Status

CLOSED — 2026-08-28. The flag is documented as an intentional apm-go-only HTTPS escape hatch; the no-flag/default path remains the Oracle-compatible GitHub shorthand.
