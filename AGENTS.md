# apm-go

Go port of [microsoft/apm](https://github.com/microsoft/apm) — the Agent Package Manager — as a single static binary with no Python runtime. Compiles `.apm/` primitives (instructions, agents, chat modes, memory) into the root context files AI agents read on startup (`AGENTS.md`, `CLAUDE.md`, `GEMINI.md`), and installs/uninstalls packages and MCP server configurations from a marketplace.

The upstream Python implementation is the **oracle**: the term always means it, and code comments cite oracle file:line. Parity is deliberate but **partial** — the oracle's command surface is ported selectively, not API-for-API. Before treating a missing command or flag as a bug, check whether it is an intentional gap: `cmd/apm-go/*.go` comments mark each deviation with its reason, and a deviation that is not recorded there is a finding, not a feature.

## GitNexus — code intelligence

This repo is indexed as **apm-go**. Use the MCP tools to navigate and to gate edits:

- **Before editing any function, class, or method**: `impact({target, direction: "upstream"})`; report the blast radius (callers, affected processes, risk) to the user, and stop for confirmation on HIGH or CRITICAL.
- **Before committing**: `detect_changes()` to confirm only the intended symbols and flows moved; `detect_changes({scope: "compare", base_ref: "main"})` for branch-level review.
- **Renames** go through `rename` — it follows the call graph.
- **Exploring unfamiliar code**: `query({search_query})` returns execution flows ranked by relevance; `context({name})` gives one symbol's callers, callees, and flows.
- **Security review**: `explain({target})` lists source→sink taint findings (needs `analyze --pdg`).

Index stale (reported by `gitnexus://repo/apm-go/context`)? `node .gitnexus/run.cjs analyze` from the project root. Task-specific workflows (exploring, impact analysis, debugging, refactoring, CLI) live in `.claude/skills/gitnexus/`.

## Build & test

```sh
go build -o bin/apm-go ./cmd/apm-go
go test ./...
```

Release-size build (same flags CI uses):

```sh
go build -trimpath -ldflags "-s -w" -o bin/apm-go ./cmd/apm-go
```

No Makefile or task runner — `go build` and `go test` are the only entry points.

## Release

Version lives in `internal/version/version.go` as a single const. The CI release workflow (`release.yml`) gates on the git tag matching that const — bump the const first, then tag.

Pre-release tags (containing `-`, e.g. `v0.3.0-beta.1`) are marked as GitHub prereleases so `install.sh` defaults to the latest stable.

## Package layout

```
cmd/apm-go/          CLI commands (one file per cobra subcommand)
internal/
  archive/           tar/zip extraction with size/count caps
  compile/           .apm/ → AGENTS.md compilation (agents-family targets)
  credsec/           credential-in-YAML detection
  deploy/            per-target adapter pattern (claude, codex, copilot, …)
  gitops/            hardened git clone, tag listing, env sanitization, stderr translation
  localbundle/       local package bundling
  lockfile/          apm.lock.yaml parse/write/audit/integrity
  manifest/          apm.yml parsing, validation, dependency refs
  marketplace/       registry client, resolver, authoring commands
  mcpregistry/       MCP server registry lookups
  pack/              plugin packaging and bundling
  pluginjson/        plugin.json / mcp.json scaffold, staged atomic commit
  registry/          package registry operations
  resolver/          dependency resolution, diamond detection, updates
  security/          security scanning gate (block/warn/ignore policy)
  semver/            SemVer parsing and comparison
  ux/                terminal output — spinners, tables, clack prompts, theming
  version/           single source of truth for release version
  yamlcore/          YAML safe-subset loader, round-trip patching
```

## Key conventions

**Oracle parity.** When the oracle handles a case, match its behaviour — edge cases, error messages, exit codes, output bytes. Cite oracle file:line in a comment when the mapping is non-obvious. Where apm-go deviates on purpose (a flag not yet ported, `apm-go` in place of `apm` in hint text), say so in a comment at the deviation with the reason.

**Git subprocesses** run under `gitops.ApplySecureGitEnv` (no credential prompts, transport allow-list) — every call site, including diagnostics.

**YAML round-trip.** `apm.yml` and `apm.lock.yaml` are user-edited files. All mutations go through `yamlcore` splice/patch helpers that preserve comments, ordering, and formatting — never marshal-and-rewrite.

**JSON bytes.** Pack and scaffold output goes through `bundle.MarshalIndent`, which reproduces Python `json.dumps(indent=2)` with `ensure_ascii=True` (non-ASCII as `\uXXXX`). The marketplace builder is the one upstream writer with `ensure_ascii=False`; it uses `encoding/json` separately.

**Deploy adapters.** Each target platform (claude, codex, copilot, antigravity, opencode, agent-skills) is a `TargetAdapter` in `internal/deploy/`. Adding a target means adding an adapter file — the dispatch in `deploy.go` picks it up.

**Exit codes.** Commands needing a specific exit code wrap errors with `withExitCode()` in `cmd/apm-go/exitcode.go`; the root error handler unwraps it. Usage errors (the oracle's `click.UsageError`) are 2.

**Security scanning.** `internal/security/` runs before deploy. `ScanPolicy` controls whether findings block, warn, or are ignored. The gate is fail-closed: unknown policy = block.

**UX layer.** All terminal output (colors, spinners, prompts, tables) goes through `internal/ux/`, which auto-detects TTY, NO_COLOR, and CI. User-facing text uses the `ux` printers, not `fmt.Print`; `ux.Plain` is for lines whose status glyph is part of the message; `ux.Error`/`ux.Warn` always land on stdout with the oracle's `"[x] "`/`"[!] "` prefixes, regardless of which stream the call site passes.

**Safe YAML subset.** `yamlcore.SafeLoad` rejects YAML features outside the OpenAPM safe subset (no anchors, no merge keys, no custom tags). All YAML ingestion goes through it.

## Testing patterns

Tests use `t.TempDir()` for filesystem isolation. No global test fixtures — each test builds its own `apm.yml` / directory tree inline. External seams are injected, not mocked globally: `installDeps`, `doctorDeps` (git/env), and the `*ForTest` hooks in `internal/ux/testhooks.go` and `internal/pluginjson/testhooks.go`. Expected values come from the oracle's source or output, never recomputed the way the code does.

**CI detection is pinned, never inherited.** `cmd/apm-go` and `internal/ux` each have a `TestMain` that unsets `CI`, `GITHUB_ACTIONS`, `GITLAB_CI`, `BUILDKITE`, `TF_BUILD`, and `JENKINS_URL` before running their tests. Both packages have production code that branches on those variables — `install` defaults to frozen under CI (`lockfile.IsCIEnvironment`, req-lk-018) and `ux.isCI` suppresses interactivity — so without the pin the same test suite is green on a workstation and red in Actions. A test that wants CI semantics opts in with `t.Setenv("CI", "1")`, which overrides the baseline for that test only. Any new package whose code reads those variables needs the same `TestMain`.

Schema sync tests (`internal/marketplace/build/`, `internal/pack/bundle/`) depend on conformance spec files under `spec/conformance/` — runtime inputs tracked in git, not generated. `TestParseDepString_AbsolutePath` in `internal/manifest` skips its two windows-drive-letter subtests outside `GOOS=windows` (ticket 09); everything else in it runs everywhere.

`go test ./...` passing is not the parity gate — `.github/workflows/parity.yml` runs `tools/parity` against the pinned Oracle on every push/PR and is the authority on Oracle byte-for-byte parity; `go test` only verifies apm-go's own Go-level correctness (ticket 09).
