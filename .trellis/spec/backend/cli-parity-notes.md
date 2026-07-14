# CLI surface notes: dev-only extensions & same-name semantic differences

> Companion to `.trellis/spec/evals/cli-surface-parity-register.md` (P0
> triage items #3 and #6, task `07-12-p0-parity-quickwins`). This file is
> the durable spec-level record; the register tracks triage/resolution
> status per row.

## `audit` (bare): SHA-256 re-verification, not Python's Unicode scan

apm-go `audit` (bare, no flags besides `-h`) re-verifies every deployed
file's recorded SHA-256 hash against `apm.lock.yaml` and reports any
content-integrity violation. This differs from Python's `apm audit`
(bare): Python's bare invocation runs a hidden-Unicode scan and never
touches SHA-256 hashes at all. The two commands share the name `audit`
and both operate on an installed project, but their bare invocations
check different things -- `apm-go audit` does not detect hidden Unicode,
and Python's bare `apm audit` does not detect a tampered deployed file
(that check is buried behind `apm audit --ci` as its `content-integrity`
check). See `.trellis/spec/evals/cli-surface-parity-register.md` §3.1 for
the full transcript evidence. `apm-go audit --help` documents this same
contrast so it is discoverable from the CLI itself, not only from this
spec file.

## `normalize` / `validate` (top-level): dev-only CLI-ized tooling (EXTENSION)

`apm-go normalize <file>` and `apm-go validate <file>` are apm-go-only
commands -- Python has no top-level `normalize` or `validate` command.
They are classified EXTENSION in the parity register (documented,
intentional, low-risk additions, not a parity gap to close).

- `normalize <file>`: reads a YAML file through `yamlcore.SafeLoad` and
  re-emits it through `yamlcore.SafeDump`, a round-trip normalization CLI
  wrapper around the same safe-YAML parse/dump pair every other apm-go
  command uses internally. It exists as a developer/CI convenience for
  inspecting or reformatting a manifest, not as an end-user
  package-management workflow step.
- `validate <file>`: parses a single named file (manifest or lockfile
  shape, auto-detected via the `lockfile_version` key) and reports schema
  errors/warnings. This is a dev-only, single-file CLI tool -- distinct in
  scope from `marketplace validate NAME`, which validates a *configured
  marketplace entry* by name against the live marketplace registry, not
  an arbitrary file path. The two commands share the word "validate" but
  operate on different inputs and in different scopes; neither is a
  substitute for the other.

Both are intentionally-scoped, documented extensions: dev-only CLI
tooling with no Python-side equivalent, kept deliberately thin (no flags
beyond the file argument) rather than grown into a general-purpose
validation framework.

## `allowExecutables:` (manifest key): warned, not enforced

apm-go does not implement Python's `allowExecutables` deny-by-default
executable-primitives gate (`security/executables.py`: hooks, bin, MCP
primitives require explicit approval before deployment when the block is
present). `internal/manifest.ParseManifest` recognizes the key and both
(a) returns a `LevelWarning` `Diagnostic` for it and (b) prints the same
warning directly to stderr, so every command that parses a manifest
containing the block (`install`, `update`, `uninstall`, `mcp install`,
...) surfaces it -- not just callers that consume `ParseManifest`'s
returned diagnostics. This is a prompt, not a gate: apm-go deploys every
executable primitive unconditionally, identically with or without the
block present. See `.trellis/spec/evals/cli-surface-parity-register.md`
§4.1 for the full risk writeup; enforcing the gate itself (P1 #17) is out
of this note's scope.
