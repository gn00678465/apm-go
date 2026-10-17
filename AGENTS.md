# apm-go

Go port of [microsoft/apm](https://github.com/microsoft/apm) — the Agent Package Manager — as a single static binary with no Python runtime. Compiles `.apm/` primitives (instructions, agents, chat modes, memory) into the root context files AI agents read on startup (`AGENTS.md`, `CLAUDE.md`, `GEMINI.md`), and installs/uninstalls packages and MCP server configurations from a marketplace.

The upstream Python implementation is the **oracle**: the term always means it, and code comments cite oracle file:line. Parity is deliberate but **partial** — the oracle's command surface is ported selectively, not API-for-API. Before treating a missing command or flag as a bug, check whether it is an intentional gap: `cmd/apm-go/*.go` comments mark each deviation with its reason, and a deviation that is not recorded there is a finding, not a feature.

## Canonical documents — read before writing code

Three files are the project's long-term memory. Read the relevant one before designing or changing anything, and treat a conflict between your plan and these files as a stop signal, not a judgment call:

- **`PRODUCT.md`** — product facts: users, purpose, positioning, operating constraints, output contract, and the **Terminal UI design** (status-symbol vocabulary, `init` / `plugin init` frame rules). Any user-visible output must match it.
- **`ARCHITECTURE.md`** — package layout, layer responsibilities, the internal import graph and its dependency rules, the data flows (install, compile, pack, marketplace, init, error/exit-code path), and cross-cutting concerns. New code goes in the layer that owns the concern; a new import edge that the graph forbids is a design change and needs a ruling.
- **`AGENTS.md`** (this file) — engineering rules and testing patterns.

Each topic lives in exactly one of them; reference, do not restate. When a change alters a fact recorded there, update that file in the same change.

**`LESSONS.md`** is the review log of merged work: what went wrong, with evidence and the days it was seen. Its lessons are candidate rules: breaking one does not block a PR, but read them before changing code, follow each one that applies, and say in the PR description what you did. A lesson becomes a mandatory rule only when it moves into "Promoted rules" below.

## GitNexus — code intelligence

This repo is indexed as **apm-go**. Use the MCP tools to navigate and to gate edits:

- **Before editing any function, class, or method**: `impact({target, direction: "upstream"})`; report the blast radius (callers, affected processes, risk) to the user, and stop for confirmation on HIGH or CRITICAL.
- **Before committing**: `detect_changes()` to confirm only the intended symbols and flows moved; `detect_changes({scope: "compare", base_ref: "main"})` for branch-level review.
- **Renames** go through `rename` — it follows the call graph.
- **Exploring unfamiliar code**: `query({search_query})` returns execution flows ranked by relevance; `context({name})` gives one symbol's callers, callees, and flows.
- **Security review**: `explain({target})` lists source→sink taint findings (needs `analyze --pdg`).

Index stale (reported by `gitnexus://repo/apm-go/context`)? `node .gitnexus/run.cjs analyze --index-only` from the project root. Keep `--index-only`: without it `analyze` regenerates `CLAUDE.md` and `AGENTS.md`, which are written by hand here. Task-specific workflows (exploring, impact analysis, debugging, refactoring, CLI) live in `.claude/skills/gitnexus/`.

## Build & test

```sh
go build -o bin/apm-go ./cmd/apm-go
go test ./...
```

Release-size build (same flags CI uses):

```sh
go build -trimpath -ldflags "-s -w" -o bin/apm-go ./cmd/apm-go
```

No Makefile or task runner — `go build` and `go test` are the only build/test entry points. The repository checks are one command, `sh .claude/skills/verify-apm-go/scripts/verify.sh check`: build, the suite in a shuffled order, `go vet`, `gofmt`, `staticcheck` (the list in `staticcheck.conf`), `govulncheck`, and `tools/realexec.sh`. It runs on the working tree as it is, on the Go version of `go.mod`'s `go` directive (it exports `GOTOOLCHAIN`), pins the two tool versions in the script, and writes its logs under `.scratch/verify/check/`. The `verify-apm-go` skill (`.claude/skills/verify-apm-go/`) also drives the built binary in a sandbox to prove a change the way a user would see it.

## Release

The version is injected at release build time: `release.yml` passes the pushed tag (without the `v` prefix) via `-ldflags "-X …/internal/version.Version=<tag>"`, so releasing needs no version-bump commit — tag a commit on main and push the tag. Local builds report `dev`.

`release.yml` gates on ancestry (fail-closed): the tagged commit must be an ancestor of `origin/main`; a tag on a feature branch is rejected. Release flow: feature PRs merge to main and never touch `internal/version/`; tag `vX.Y.Z-beta.N` from main at any point during integration; tag `vX.Y.Z-rc.N` when the feature set is frozen (only fixes and docs land after); tag `vX.Y.Z` on the same commit as the last verified rc.

Pre-release tags (containing `-`, e.g. `v0.3.0-beta.1`) are marked as GitHub prereleases so `install.sh` defaults to the latest stable; testers install one with `APM_GO_VERSION=X.Y.Z-rc.N`.

## Package layout

```
cmd/apm-go/          CLI commands (one file per cobra subcommand)
internal/
  archive/           tar/zip extraction with size/count caps
  compile/           .apm/ → AGENTS.md compilation (agents-family targets)
  credsec/           credential-in-YAML detection
  deploy/            per-target adapter pattern (claude, codex, copilot, …)
  experimental/      opt-in feature flags persisted in the user config
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
  rootfs/            write confinement: a directory handle every write is resolved against
  security/          security scanning gate (block/warn/ignore policy)
  selfupdate/        binary self-update from GitHub Releases
  semver/            SemVer parsing and comparison
  ux/                terminal output — spinners, tables, clack prompts, theming
  version/           single source of truth for release version
  yamlcore/          YAML safe-subset loader, round-trip patching
```

## Key conventions

**Oracle parity.** When the oracle handles a case, match its behaviour — edge cases, error messages, exit codes, output bytes. Cite oracle file:line in a comment when the mapping is non-obvious. Where apm-go deviates on purpose (a flag not yet ported, `apm-go` in place of `apm` in hint text), say so in a comment at the deviation with the reason.

**Git subprocesses** run under `gitops.ApplySecureGitEnv` (no credential prompts, transport allow-list) — every call site, including diagnostics. A subprocess that connects to a remote (`ls-remote`, `clone`, `fetch`) first passes `gitops.ValidateURLRewriteSafety` for that URL.

**YAML round-trip.** `apm.yml` is a user-edited file. All mutations go through `yamlcore` splice/patch helpers that preserve comments, ordering, and formatting — never marshal-and-rewrite. `apm.lock.yaml` is the exception since issue #50: `lockfile.WriteLockfile` writes the whole file in the Oracle's layout and carries over the keys it does not model; comments and quote style are not kept.

**JSON bytes.** Pack and scaffold output goes through `bundle.MarshalIndent`, which reproduces Python `json.dumps(indent=2)` with `ensure_ascii=True` (non-ASCII as `\uXXXX`). The marketplace builder is the one upstream writer with `ensure_ascii=False`; it uses `encoding/json` separately.

**Deploy adapters.** Each target platform (claude, codex, copilot, antigravity, opencode, agent-skills) is a `TargetAdapter` in `internal/deploy/`. Adding a target means adding an adapter file — the dispatch in `deploy.go` picks it up.

**Exit codes.** Commands needing a specific exit code wrap errors with `withExitCode()` in `cmd/apm-go/exitcode.go`; the root error handler unwraps it. Usage errors (the oracle's `click.UsageError`) are 2.

**Merging a PR.** Merge with `gh pr merge <N> --merge --match-head-commit <reviewed head> --delete-branch`, so the head branch is deleted on GitHub in the same step; then remove the local branch and its worktree. A branch whose PR has not merged is not deleted.

**Security scanning.** `internal/security/` is a credential/secret scanner with a policy gate (`ScanPolicy`: block, warn, or report; fail-closed — an unknown policy blocks). Today it runs in two places only: `pack` scans bundled sources under `WarnPolicy` (`internal/pack/bundle/producer.go`), and `audit` scans deployed files and reports critical/warning counts (`cmd/apm-go/audit_content.go`). It is **not** on the install/deploy path; do not describe or rely on it as a pre-deploy gate.

**UX layer.** All terminal output (colors, spinners, prompts, tables) goes through `internal/ux/`, which auto-detects TTY, NO_COLOR, and CI; user-facing text uses the `ux` printers, not `fmt.Print`. The status-symbol vocabulary, the interactive `init` / `plugin init` frame rules, and the rest of the terminal UI design live in `PRODUCT.md` ("Terminal UI design") — the single source; do not restate them here.

**Safe YAML subset.** `yamlcore.SafeLoad` rejects YAML features outside the OpenAPM safe subset (no anchors, no merge keys, no custom tags). All YAML ingestion goes through it.

## Testing patterns

Tests use `t.TempDir()` for filesystem isolation. A git repository a test creates is made through a helper that runs git with `-c maintenance.auto=false` (for example `gitCmd` in `cmd/apm-go/marketplace_authoring_test.go`): git repacks in the background after a commit, which changes a fixture in the middle of a before/after comparison. No global test fixtures — each test builds its own `apm.yml` / directory tree inline. External seams are injected, not mocked globally: `installDeps`, `doctorDeps` (git/env), and the `*ForTest` hooks in `internal/ux/testhooks.go` and `internal/pluginjson/testhooks.go`. Expected values come from the oracle's source or output, never recomputed the way the code does.

**CI detection is pinned, never inherited.** `cmd/apm-go` and `internal/ux` each have a `TestMain` that unsets `CI`, `GITHUB_ACTIONS`, `GITLAB_CI`, `BUILDKITE`, `TF_BUILD`, and `JENKINS_URL` before running their tests. Both packages have production code that branches on those variables — `install` defaults to frozen under CI (`lockfile.IsCIEnvironment`, req-lk-018) and `ux.isCI` suppresses interactivity — so without the pin the same test suite is green on a workstation and red in Actions. A test that wants CI semantics opts in with `t.Setenv("CI", "1")`, which overrides the baseline for that test only. Any new package whose code reads those variables needs the same `TestMain`.

**The machine's git configuration is pinned the same way.** `gitops.ValidateURLRewriteSafety` reads the effective git configuration before every network git subprocess, so an `insteadOf` rule on the workstation would decide whether a test passes. The `TestMain` of `cmd/apm-go`, `internal/gitops`, `internal/marketplace`, `internal/marketplace/authoring`, and `internal/marketplace/build` installs `gitops.EmptyGitConfigProbe` through `gitops.SetConfigProbeForTest`; a test of the check installs its own probe. A new package whose tests reach a network git call site needs the same line.

Schema sync tests (`internal/marketplace/build/`, `internal/pack/bundle/`) depend on conformance spec files under `spec/conformance/` — runtime inputs tracked in git, not generated. `TestParseDepString_AbsolutePath` in `internal/manifest` skips its two windows-drive-letter subtests outside `GOOS=windows` (ticket 09); everything else in it runs everywhere.

`go test ./...` passing is not the parity gate — `.github/workflows/parity.yml` runs `tools/parity` against the latest stable upstream release of the Oracle on every push/PR, resolving the version when the job runs, and is the authority on Oracle byte-for-byte parity; `go test` only verifies apm-go's own Go-level correctness (ticket 09).

## Promoted rules

Lessons seen on 3 separate days in `LESSONS.md`. These rules are mandatory: every PR description and every review checks each one, and a PR that breaks one is not ready. The original entries and their evidence stay in `LESSONS.md` under "Moved to AGENTS.md".

1. **Run every check this machine can run before you list it under "Not verified".** The Windows sandbox, a local fake registry, a local-directory marketplace and an installed agent CLI all count. Before writing "cannot be verified here", try it once and write the command that failed. (Promoted 2026-10-17.)
2. **When a PR changes what an existing user sees after upgrading, add one line for it to the release-notes issue of the next tag in the same step that opens the PR**; open that issue (`Release notes: vX.Y.Z`) when none exists. A PR's Risks section alone is not a record. (Promoted 2026-10-17.)
3. **Write what the Oracle does only after you run Oracle 0.33.0 or cite the Oracle's file:line, and write what PRODUCT.md says only with the item quoted.** This holds in issue bodies and comments too. A statement copied from an earlier issue or comment needs the same check, because that record can claim more than its run showed. (Promoted 2026-10-17.)

## Lessons review

Run a review after a board card's PR merges, or at the end of a day with merges. It covers `<reviewed-through>..origin/main`, the cursor on the first line of `LESSONS.md`.

1. Read the range: commits, each PR's body and comments, `board log <issue> --project apm-go` of each card, and issues opened, reopened or corrected in the range.
2. Look for work that had to be fixed again, work that should not have merged, verification that was too thin, and work done right that is worth repeating.
3. Write each finding as a lesson: a rule an agent can follow (imperative), why, evidence, an optional `Done right on <date>: <ref>`, and `Seen N× (<dates>)`. Seen counts separate days (UTC+8); several instances on one day count once. A new instance of an existing lesson adds its evidence and, on a new day, raises the count.
4. A finding that breaks a rule already written in this file or in the project's instructions goes under "Existing rules missed" in `LESSONS.md` with its evidence, not as a new lesson. A finding about the board tooling itself is not entered in `LESSONS.md`.
5. Check every reference before writing it: each sha with `git cat-file -t`, each PR, issue and comment with `gh`, each board event with `board log`. A finding whose evidence cannot be checked is not entered as a lesson.
6. A lesson seen on 3 separate days moves into "Promoted rules" in the same commit: the rule is added here, and its entry in `LESSONS.md` moves under "Moved to AGENTS.md" with its evidence.
7. Move the cursor to the reviewed head in the same commit. A review that does not move the cursor is not done.
8. Land it through a PR titled `lessons: <from>..<to> (<N> PRs)` whose description lists the new lessons, the raised counts and the promotions.
