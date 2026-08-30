# Product

<!-- impeccable:product-schema 1 -->

## Platform

web

The web platform covers the documentation surface (confirmed 2026-08-29). The product's other confirmed surface is the CLI's own terminal UI (`internal/ux/`), which is not a web/native platform: any work there is governed by the terminal constraints in Operating Context, not by web design guidance.

## Users

- **AI-agent users / developers** (primary): run `apm-go init`, `install`, `compile`, `update`, `uninstall`, `audit` inside their own repositories to produce and maintain the root context files AI agents read on startup (`AGENTS.md`, `CLAUDE.md`, `GEMINI.md`) and to manage package + MCP-server dependencies declared in `apm.yml` / `apm.lock.yaml`.
- **Marketplace / plugin authors** (primary): run `marketplace init`, `marketplace package add`, `marketplace audit`, `plugin init`, `pack` to author a marketplace, package skills/instructions, and publish Claude Code plugins (`marketplace.json`, `plugin.json`, `.mcp.json`).

Both audiences work in a terminal, frequently non-interactively (CI, agent-driven shells). Other audiences (platform / CI teams) exist in practice but were not confirmed as primary.

## Product Purpose

apm-go is an Agent Package Manager shipped as a single static Go binary with no interpreter or runtime dependency. It compiles `.apm/` primitives (instructions, agents, chat modes, memory) into agent context files, installs/uninstalls packages and MCP server configurations from marketplaces, and packages plugins for distribution.

Success: a user gets deterministic files, output, and exit codes from one binary installed via `install.sh` or a GitHub release, with no runtime to set up.

## Positioning

**Single static binary + a verified, extended command surface.** Output bytes, exit codes, and generated file trees are treated as a contract and gated in CI (`tools/parity`), so scripts and agents can depend on them. On that base apm-go ships its own additions — `pack --claude-source-style url`, `pack --json`, `--check-versions` / `--check-clean` release gates, top-level `doctor`, `normalize`, `validate` — each documented at its site in `cmd/apm-go/*.go`.

## Operating Context

- Runs in a terminal; output goes through `internal/ux/`, which auto-detects TTY, `NO_COLOR`, and CI (`CI`, `GITHUB_ACTIONS`, `GITLAB_CI`, `BUILDKITE`, `TF_BUILD`, `JENKINS_URL`). Non-TTY/CI disables spinners and prompts; `NO_COLOR` disables color but **not** prompting.
- `ux.Error` / `ux.Warn` always land on stdout.
- Under CI, `install` defaults to frozen (lockfile-only) mode.
- Users' `apm.yml` / `apm.lock.yaml` are hand-edited files; all mutations are round-trip patches that preserve comments, ordering, and formatting.
- Git is a subprocess run under a hardened environment (no credential prompts, transport allow-list). On this project's development machine there is no SSH key, which is why `--claude-source-style url` exists for installable marketplaces.
- Distribution: `install.sh` (defaults to latest stable; pre-release tags are GitHub prereleases), GitHub Releases, `go build`. Version lives in `internal/version/version.go` (currently `0.3.0-rc.1`).

### Terminal UI design

Presentation rules the CLI's own terminal output follows. Words, order, exit codes, and generated files are not presentation and are not covered here.

**Status symbols.** Every status line starts with one centered, colored, width-3 symbol from the single vocabulary in `internal/ux/colors.go`. Brackets are never printed, on any stream, interactive or piped.

| class | symbol |
|---|---|
| success | ` + ` |
| info | ` i ` |
| warning | ` ! ` |
| error | ` x ` |
| progress | ` > ` |
| list | ` * ` |
| plan diff update / remove / unchanged | ` ~ ` ` - ` ` = ` |

Call sites never choose a glyph; they call the `ux` printer for the class (`ux.Success`, `ux.Info`, `ux.Warn`, `ux.Error`, `ux.Progress`, `ux.List`). `ux.Plain` is for complete rows or text without a status symbol. Mixing symbol styles across commands was an earlier defect; one vocabulary is the fix.

**Interactive `init` / `plugin init`.** The interactive session is rendered as one clack-style transcript on stderr (`ux.NewClack`, `cmd/apm-go/init.go`): the prompts, an "About to create" note, a "Created project directory" progress line, and a single "Initializing" note that holds the success content (title, Created Files table, Next Steps, tips). Rules that must hold:

- the left frame border is continuous — no glyph or box line starts at column 0 inside the frame;
- status symbols inside the frame are the project symbols above;
- notes are capped to terminal width and wrap (`Clack.Note`), so a wide terminal or a long path never breaks the layout;
- consecutive steps keep a blank spacer line so a note does not sit directly against the previous prompt;
- the non-interactive path (`--yes`, non-TTY, CI) prints the same success content as plain status lines with the same symbols;
- `plugin init` shares the same core (`runInitCore`) and the same frame; it has its own flag set (`--verbose`, `--format`, `--claude-plugin`) and does not carry consumer init's `--force`;
- no docs/star footer is printed after the success block.

## Capabilities and Constraints

Command surface (`apm-go --help`): `audit`, `compile`, `completion`, `doctor`, `experimental`, `init`, `install`, `marketplace` (add/list/browse/update/remove/validate/init/package/audit), `normalize`, `pack`, `plugin`, `search`, `uninstall`, `update`, `validate`.

Constraints future work must preserve:

- Output contract: words, exit codes, output bytes, and file trees are gated in CI. Only line-wrap, box-drawing, help layout, status-symbol shape (normalized by the runner), timestamps, and paths may be waived; every waiver is recorded with its reason.
- Exit codes: usage errors are 2; specific codes via `withExitCode()`.
- JSON bytes from pack/scaffold go through `bundle.MarshalIndent` (2-space indent, non-ASCII escaped as `\uXXXX`); the marketplace builder alone emits UTF-8 unescaped.
- YAML ingestion is restricted to the OpenAPM safe subset (no anchors, merge keys, custom tags) — `spec/conformance/openapm-v0.1.md`.
- Credential scanning (`internal/security/`) runs in `pack` (warn policy) and `audit` (report); its policy gate is fail-closed (unknown policy = block). It is not part of install/deploy.
- Deploy targets: claude, codex, copilot, antigravity, opencode, agent-skills (adapter per target).
- Hint text says `apm-go`, not `apm`.
- No third-party docs/star footer in command output.

Terminology: **parity gate** = the `tools/parity` CI run that enforces the output contract; **waiver** = a recorded allowed difference with its reason; **pending case** = a parked corpus case for a documented design deviation; **ticket** = an issue file under `.scratch/parity-runner/issues/`.

Undecided: the lockfile's external interoperability format, and whether MCP servers belong in the lockfile (ticket 32 A/B rulings, out of scope for the current branch).

## Brand Commitments

- Name: `apm-go` (binary and command name).
- Voice: terse, factual CLI copy.
- No logo or visual identity assets exist.

## Evidence on Hand

- `README.md`, `README.zh-TW.md` — existing product copy in English and Traditional Chinese.
- `spec/conformance/` — OpenAPM safe-subset spec, agent/marketplace schema, CLI surface notes, verification checklist, dependency-ref and repr conformance tables.
- `tools/parity/cases/` — 96 byte-exact output-contract cases (0 unwaived diffs at HEAD); `tools/parity/cases-pending/` — 5 parked cases with README.
- `.scratch/parity-runner/issues/` — 35 ticket files documenting the output-contract backlog and rulings.
- CI: `.github/workflows/parity.yml` (parity gate), `release.yml`.
- Absent, must not be fabricated: testimonials, user counts, benchmarks, customers, pricing.

## Product Principles

1. **Output is a contract.** Words, bytes, exit codes, and file trees are gated in CI; an unrecorded difference is a bug until a waiver names it.
2. **Additions are opt-in supersets.** New flags and commands default to the existing behavior and are documented at their site in code.
3. **One vocabulary on every stream.** Status symbols, framing, and wording are the same in interactive, piped, and CI output; the ux layer, not call sites, decides presentation.
4. **User files are sacred.** `apm.yml`, lockfiles, and context files are patched, never rewritten.
5. **Fail closed.** Unknown security policy blocks; unknown waiver IDs fail the gate; unsafe YAML is rejected.

## Accessibility & Inclusion

- Terminal output must remain meaningful without color (`NO_COLOR`) and without a TTY (CI, pipes); status is carried by the symbol glyph, not color alone.
- Documentation is bilingual (English / Traditional Chinese) — the docs surface should preserve both.
