# 29 — `pack` Codex category-required error wording

**What to build:** Match the Oracle when a marketplace declares the `codex` output but a package omits `category`. **Status:** CLOSED (2026-08-28).

**Origin:** ticket 28's live success-path probe exposed a separate validation-surface difference. The Oracle reports the configuration error below before producing any marketplace artifact; apm-go previously reached `CodexMapper.Compose` and reported a package-level error instead.

## Verified difference

With `marketplace.outputs` containing `codex` and package `pkg-a` missing `category`, `pack -m codex` produces:

```text
Oracle: Error: marketplace config error: packages must define 'category' when marketplace.outputs includes 'codex' (missing: pkg-a)
apm-go: [x] package "pkg-a" is missing category required for Codex output
```

The Oracle validates all configured outputs in `marketplace/yml_schema.py:1294-1304`, aggregates missing package names in declaration order, and wraps the schema error as `marketplace config error` in `core/build_orchestrator.py:168`; its CLI error handler renders the resulting line on stderr. The lower-level Oracle mapper wording is `package 'pkg-a' is missing category required for Codex output` (`marketplace/output_mappers.py:320-322`) and remains the defensive-composition contract.

## Acceptance criteria

- [x] Configured Codex output with one or more missing categories returns the Oracle's exact aggregate wording, including single quotes and package order.
- [x] The error is emitted as a bare `Error: ...` line on stderr, with exit code 1 and no output artifact written.
- [x] Direct `CodexMapper.Compose` retains the Oracle mapper wording with single quotes.
- [x] A parity case exercises `pack -m codex` with a fixture lacking `category`; the case has no unwaived diff.
- [x] Go tests lock the aggregate and mapper-level wording, and no existing package test regressions remain.

## Implementation

- `internal/marketplace/authoring/schema.go`: added `ValidateOutputRequirements`, separate from `LoadAuthoringConfig` so shared readers remain permissive while pack applies the Oracle's producer validation.
- `cmd/apm-go/pack.go`: validates configured outputs immediately after loading the authoring config, before filtering, resolving, or writing.
- `cmd/apm-go/exitcode.go` and `cmd/apm-go/main.go`: added the narrow bare-stderr error rendering used by this Oracle BuildError path.
- `internal/marketplace/build/codexmapper.go`: changed the defensive mapper error to the Oracle's single-quoted wording.

## Tests and parity

- `internal/marketplace/authoring/schema_test.go` asserts aggregate wording and declaration order.
- `internal/marketplace/build/codexmapper_test.go` asserts the exact mapper-level message.
- `cmd/apm-go/pack_test.go` asserts config validation also precedes `-m` filtering and writing.
- `tools/parity/cases/pack-marketplace-codex-missing-category/` is the new end-to-end parity fixture.
- The final corpus is required to report zero unwaived differences; only the separately accepted Rich-wrap waiver remains in the waiver file.
