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

**Single static binary + a verified, extended command surface.** Output bytes, exit codes, and generated file trees are treated as a contract and gated in CI (`tools/parity`), so scripts and agents can depend on them. On that base apm-go ships its own additions — `pack --claude-source-style url`, `pack --json`, `--check-versions` / `--check-clean` release gates, top-level `doctor`, `normalize`, `validate`, `marketplace package update` — each documented at its site in `cmd/apm-go/*.go`. Upstream additions are ported on the same contract; `pack --strict-metadata`, the read-only `pack --check-clean`, and the Git URL rewrite safety check came from upstream 0.29.1.

## Operating Context

- Runs in a terminal; output goes through `internal/ux/`, which auto-detects TTY, `NO_COLOR`, and CI (`CI`, `GITHUB_ACTIONS`, `GITLAB_CI`, `BUILDKITE`, `TF_BUILD`, `JENKINS_URL`). Non-TTY/CI disables spinners and prompts; `NO_COLOR` disables color but **not** prompting.
- `ux.Error` / `ux.Warn` always land on stdout.
- Under CI, `install` defaults to frozen (lockfile-only) mode.
- A user's `apm.yml` is a hand-edited file; every mutation is a round-trip patch that preserves comments, ordering, and formatting. `apm.lock.yaml` is written whole in the Oracle's layout (issue #50): keys apm-go does not model survive, comments and the quote style of the old file do not.
- Git is a subprocess run under a hardened environment (no credential prompts, transport allow-list). On this project's development machine there is no SSH key, which is why `--claude-source-style url` exists for installable marketplaces.
- Distribution: `install.sh` (defaults to latest stable; pre-release tags are GitHub prereleases), GitHub Releases, `go build`. The release version is injected from the git tag at release build time (`internal/version/version.go`); local builds report `dev`.

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

Command surface (`apm-go --help`): `audit`, `compile`, `completion`, `doctor`, `experimental`, `init`, `install`, `marketplace` (add/list/browse/update/remove/validate/init/package/audit), `normalize`, `pack`, `plugin` (init/validate), `search`, `self-update`, `uninstall`, `update`, `validate`.

Constraints future work must preserve:

- Output contract: words, exit codes, output bytes, and file trees are gated in CI. Only line-wrap, box-drawing, help layout, status-symbol shape (normalized by the runner), timestamps, and paths may be waived; every waiver is recorded with its reason. The gate runs against the latest stable upstream release of the Oracle, resolved when CI runs, so a new upstream release can turn the gate red with no change in this repository. An apm-go-only command that the Oracle does not have cannot have a corpus case; its contract is pinned instead by `tools/realexec.sh` at the same strength (full stdout, stderr, exit code, file tree) and recorded in a ticket that quotes the user ruling - ticket 34 covers `plugin validate`, ticket 37 covers `marketplace package update`, and the exemption never extends by analogy to another command.
- Exit codes: usage errors are 2; specific codes via `withExitCode()`. `pack` exits 3 for a failed `--check-versions`, 4 for a failed `--check-clean`, and 5 for a failed `--strict-metadata`; 5 is decided before any producer runs, and 3 wins over 4.
- JSON bytes from pack/scaffold go through `bundle.MarshalIndent` (2-space indent, non-ASCII escaped as `\uXXXX`); the marketplace builder alone emits UTF-8 unescaped.
- YAML ingestion is restricted to the OpenAPM safe subset (no anchors, merge keys, custom tags) — `spec/conformance/openapm-v0.1.md`.
- Marketplace metadata certification (`pack`): each resolved package has one metadata outcome (`fetched`, `empty`, `failed`, `offline`, `local`, `explicit`), reported in `pack --json` as `metadata_enrichment`. Only the Claude output uses package metadata. `--strict-metadata` fails with exit 5 before anything is written when a selected output's metadata could not be fetched. `--check-clean` is always read-only (it implies `--dry-run`), reuses the metadata of the same run, and reports an output whose metadata could not be fetched as `uncertifiable` (exit 4) without comparing it. An outcome's cause never carries raw error text, a URL, or a credential.
- Git URL rewrite safety: before every network git subprocess, apm-go reads the effective `url.*.insteadOf` rules and refuses a rewrite that downgrades the transport, adds credentials, or moves an authenticated remote to another host or origin; a config that cannot be read fails the operation instead of being skipped.
- Credential scanning (`internal/security/`) runs in `pack` (warn policy) and `audit` (report); its policy gate is fail-closed (unknown policy = block). It is not part of install/deploy.
- Deploy targets: claude, codex, copilot, antigravity, opencode, agent-skills (adapter per target).
- `plugin validate` has no upstream Oracle equivalent (apm-go-only, closing the second half of issue #13); its output contract is fixed by `tools/realexec.sh`, not a `tools/parity` corpus case.
- `marketplace package update` has no upstream Oracle equivalent (apm-go-only, issue #25): it writes the upgrade `marketplace outdated` reports for a SHA-pinned package back to the authoring config, replacing only the `version` and `ref` values. A SHA pin with no version that has a `subdir` moves only when the content of that directory (its git object id) at the default-branch tip differs from the pinned commit; with the same content nothing is written, and `marketplace outdated` reports the entry as up to date (issue #39). Its output contract is fixed by `tools/realexec.sh` (ticket 37), not a `tools/parity` corpus case; the steps there are network-free, so the path that writes is covered by `go test` with a canned `RefLister` and a canned `SubdirObjectReader`.
- Lockfile (`apm.lock.yaml`): the layout is the Oracle's (field order, quoting, list indentation, a `deployments` section, no `generated_at`); the file has mode `0600`. MCP servers are recorded in it as `mcp_servers`, `mcp_configs` and `mcp_target_servers`, by `install` and by `install --mcp`; an MCP config file such as `.mcp.json` is not listed as a deployed file. A deployed skill's directory (`<target root>/skills/<name>`) is listed in `deployed_files` and `deployments` with no content hash, as the Oracle lists it, by a project install and by `install --global`. The keys of an MCP server's `env` and `headers` keep the order they were declared in, in the lock, in `apm.yml` and in the Claude `.mcp.json`. Whether the lock has an `apm_version` line follows the command and the flag, also for a lock that already exists and changes in nothing else: `install` and `install --mcp` write it for a lock that records a dependency or an MCP server, `--no-provenance` leaves it out, and a lock of the project's own content alone keeps the line it had. The value of an existing line is not a reason to write the lock again; when a lock is written, is to have the line, and the value of its line is empty, the line is written with the current version. `install --mcp` applies this also when it ends without recording anything (a registry entry that does not change, a server that no target takes): an existing lock whose line has to come or go is then written whole, like every other lock, and one that already agrees is not touched. The lock has the line when the document has a top-level `apm_version` key, whatever its value or quoting. A user who declines to replace an entry changes no file. Deviation: the Oracle has no such flag and does not add a missing line. A lock in the format before issue #50 is read without error: a frozen install uses it as it is, every other install, and `update`, writes it again in the current format and removes the `apm_modules/` directory the old lock named. The entry fields of git and registry sources were ported from the Oracle's source and have not been measured against it.
- Local-path packages: the package is copied to `apm_modules/_local/<directory name>` and the lock records `repo_url: _local/<directory name>`, `source: local` and `local_path`, as the Oracle does. `install <local path> --skill` writes the `apm.yml` entry as `path:` with `skills:`, as the Oracle does. In `apm.yml` the object form `- path: <value>` takes the same values as the string form and reads the same path; `- path: dep` is refused with the Oracle's reason text and exit 1, and no file is changed. As in the Oracle, a path that starts with `//` and a UNC path (`\\host\share`) are refused in both forms, and whitespace around a path is dropped in both, so `- './dep '` is `./dep`; when `install <local path> --skill` writes the entry, the path it writes has no whitespace around it either. The text before the reason is apm-go's own (for `install`: `validate apm.yml: dependency entry <N> (path: "<value>"): ...`). Deviations: two local packages with the same directory name end `install` with an error before anything is written (the Oracle copies the second over the first and exits 0); an `alias` the entry already has stays after `install <local path> --skill` (the Oracle drops it); `install <local path>` with no skill subset to write does not rewrite an entry that already exists, so a value with whitespace around it stays as written (the Oracle rewrites the entry; one case of ticket 32 item 18). A local package that declares a local package of its own is refused, and the refused copy does not stay in `apm_modules/`.
- `install`, `uninstall` and `install --mcp` print the Oracle's success transcript. Deviation: apm-go does not print the Oracle's two `Skipped inactive experimental resolver for target ...` lines. The directories the Oracle leaves empty (`apm_modules/` after `uninstall`, `.claude/` after `install --mcp`) are left too; the Oracle's cache directories under `~/.cache/apm/` are not created. A runtime failure of `install` or `update` is one error line, with no usage text.
- `compile`: `AGENTS.md` has the Oracle's format. Deviations: apm-go writes only the root `AGENTS.md` and does not distribute instructions into subdirectories, and its terminal text is its own short transcript, not the Oracle's analysis and placement table.
- `marketplace init`: the Next Steps panel has the Oracle's layout. Deviations: the template's commented example says `# ref: v1.0.0` (`pack` rejects a branch ref), and the text says `apm-go pack`.
- A skill directory the user replaced by a symlink: while the package still has the skill, `install` replaces the symlink by the skill (a directory for a project, the deploy's own symlink for `--global`) and writes nothing through it. Once the skill is gone from the package, or at `uninstall`, the symlink is the user's: nothing is deleted through it, also a file with the same bytes as the package's, it stays, one warning names it, and `install` drops its paths from the lock, so the warning is not repeated and a frozen install and `audit` do not check the user's files. For `--global` this holds for a symlink pointed outside the package it was deployed from; the same goes for an agent or command symlink.
- `uninstall`: deviations kept by ruling: a package that is not installed is a warning with exit 0 (the Oracle exits 1), and `uninstall <MCP server name>` removes that server (the Oracle rejects the name). `uninstall` deletes `apm.lock.yaml` only when it records nothing and has no top-level key apm-go does not model; a lock with such a key (`x-*`, `lsp_*`, `mcp_config_provenance`, any unknown key) stays, with `dependencies: []` and those keys as they were (the Oracle deletes it).
- Rule for every command: a line in the Oracle's output that sends the user to an Oracle documentation path or URL is not printed. It is applied where such a line is met; the command surface has not been swept for it.
- Parity runner normalization beyond paths, timestamps and hex tokens: the shape of a status symbol at column 0 and directly after the prefixes `"  "` and `"|  "` (symbols `+ > * i ! x` only), and the value of the top-level `apm_version:` line of `cwd/apm.lock.yaml`. Every other byte of a file tree is compared.
- Hint text says `apm-go`, not `apm`.
- No third-party docs/star footer in command output.

Terminology: **parity gate** = the `tools/parity` CI run that enforces the output contract; **waiver** = a recorded allowed difference with its reason; **pending case** = a parked corpus case for a documented design deviation; **realexec contract** = the output contract of an apm-go-only command with no Oracle counterpart, pinned by `tools/realexec.sh` rather than the parity corpus, one named command per ticket; **ticket** = an issue file under `.scratch/parity-runner/issues/`.

Decided in issue #50 (ticket 32 A/B rulings): the lockfile follows the Oracle's format, and MCP servers are recorded in it.

## Brand Commitments

- Name: `apm-go` (binary and command name).
- Voice: terse, factual CLI copy.
- No logo or visual identity assets exist.

## Evidence on Hand

- `README.md`, `README.zh-TW.md` — existing product copy in English and Traditional Chinese.
- `spec/conformance/` — OpenAPM safe-subset spec, agent/marketplace schema, CLI surface notes, verification checklist, dependency-ref and repr conformance tables.
- `tools/parity/cases/` — 110 output-contract cases (0 unwaived diffs at HEAD); `tools/parity/cases-pending/` — no parked case, only its README.
- `.scratch/parity-runner/issues/` — ticket files documenting the output-contract backlog and rulings.
- CI: `.github/workflows/parity.yml` (parity gate), `release.yml`.
- Absent, must not be fabricated: testimonials, user counts, benchmarks, customers, pricing.

## Product Principles

1. **Output is a contract.** Words, bytes, exit codes, and file trees are gated in CI; an unrecorded difference is a bug until a waiver names it.
2. **Additions are opt-in supersets.** New flags and commands default to the existing behavior and are documented at their site in code.
3. **One vocabulary on every stream.** Status symbols, framing, and wording are the same in interactive, piped, and CI output; the ux layer, not call sites, decides presentation.
4. **User files are sacred.** `apm.yml` and context files are patched, never rewritten. The lockfile is machine-written in one layout, the Oracle's, and keeps every key it does not model.
5. **Fail closed.** Unknown security policy blocks; unknown waiver IDs fail the gate; unsafe YAML is rejected.

## Accessibility & Inclusion

- Terminal output must remain meaningful without color (`NO_COLOR`) and without a TTY (CI, pipes); status is carried by the symbol glyph, not color alone.
- Documentation is bilingual (English / Traditional Chinese) — the docs surface should preserve both.
