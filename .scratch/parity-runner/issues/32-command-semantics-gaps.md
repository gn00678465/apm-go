# 32 — command-semantics gaps exposed by ticket 31's new corpus cases

**What to build:** make five command families produce the Oracle's words AND
on-disk footprint, then move their cases from `tools/parity/cases-pending/`
back into `tools/parity/cases/`. **Status:** complete (2026-10-12, issue #50). The owner ruled on each
deviation (rulings R1-R20 of issue #50); the five cases and six new ones are
in `tools/parity/cases/`, and `tools/parity/cases-pending/` holds no case.
The sections below up to "Scope ruling" are the record of the ticket as it
was opened; "Result (issue #50)" is the outcome.

**Origin:** ticket 31 added 20 cases for commands that had none. Fifteen went
green (glyph/wording fixes); five exposed differences deeper than output
formatting. Each is a `stdout` + `tree` unwaived diff at pin b75a02b1.

| case | what differs (verifier report 10) |
|---|---|
| `compile-instruction` | apm-go prints a lean summary and writes a different `AGENTS.md`; the Oracle prints its analysis/table/warning surface (commands/compile/cli.py) |
| `install-local-path-dep` | local deps materialise as `_local/<hash>` with different lockfile/apm_modules footprint vs the Oracle's local install plan; the missing `.gitignore` update was fixed, but the documented footprint difference remains |
| `marketplace-init` | generated `apm.yml` marketplace template content and the Next Steps panel differ |
| `mcp-install-offline` | Oracle prints discovery/configuration records and writes a different `.mcp.json`/lockfile footprint |
| `uninstall-local` | removal wording, `apm.yml` update wording and module/integrated-file cleanup footprint differ |

Plus one wording branch found by the orchestrator's probe (2026-08-29):
`marketplace add <nonexistent-dir> --name x` -> Oracle
`[x] Failed to register marketplace: Failed to fetch marketplace 'x': local marketplace path does not exist: <dir>. Run 'apm marketplace update ...'`
(commands/marketplace/__init__.py) while apm-go prints the manifest-missing
sentence that belongs to the existing-dir branch. Needs its own case
`marketplace-add-nonexistent`.

## Needs user ruling (2026-08-29, verifier brief 10)

The following are five documented apm-go design deviations. They remain in
`tools/parity/cases-pending/`; no parity waiver covers their `tree` fields.
They require an explicit product decision before implementation changes can
claim ticket 32 complete.

- `compile-instruction`: apm-go intentionally implements only the minimal
  agents-family compiler and does not port the Oracle's distributed-analysis,
  report, warning, or template surface (`cmd/apm-go/compile.go:18-24`,
  `internal/compile/template.go:24-27`).
- `marketplace-init`: the scaffold deliberately uses `ref: v1.0.0` because
  apm-go pack rejects branch/HEAD refs, and the Rich Next Steps panel is only
  approximated by the non-interactive ux box
  (`internal/marketplace/authoring/template.go:30-38`,
  `cmd/apm-go/marketplace_authoring.go:114-127`).
- `install-local-path-dep`: local dependencies deliberately use the
  content-addressed `_local/<base>-<sha8>` materialization and corresponding
  git-shaped lockfile record rather than the Oracle's `_local/<base>` local
  source record (`cmd/apm-go/install.go:2716-2730,2777-2832`). The missing
  `.gitignore` update was fixed; this remaining footprint still needs a
  product decision.
- `mcp-install-offline`: standalone `install --mcp` deliberately declares
  and deploys independently of the package pipeline and leaves
  `apm.lock.yaml` untouched; its summary is presentation-only
  (`cmd/apm-go/mcpinstall.go:37-40,171-181`).
- `uninstall-local`: local-path dependencies deliberately materialize under
  the content-addressed `_local/<base>-<sha8>` key, and uninstall translates
  the manifest path back into that key rather than using the Oracle's
  `_local/<base>` path (`cmd/apm-go/uninstall.go:174-186`). The compact
  uninstall summary is also an intentional R7 surface with its rationale
  recorded in `cmd/apm-go/uninstall.go:620-629`.

## Acceptance criteria
- [x] Each of the five cases runs green in `cases/`. `install-local-path-dep`
  has a stdout waiver for the two experimental-resolver lines apm-go does not
  print (R10, R18); `compile-instruction` has a stdout waiver (R3, R18);
  `uninstall-local` has a stdout waiver for the Oracle's line break at column
  80 and `marketplace-init` has a stdout waiver for three padding spaces in
  the panel and a tree waiver for `cwd/apm.yml` only (R4, R7, owner ruling of
  2026-10-12); `mcp-install-offline` has no waiver. The criterion "the `tree`
  field is never waived" was changed by ruling R7 for that one named file.
- [x] `marketplace-add-nonexistent` added and green after a field-scoped
  stdout rendering waiver; its tree remains unwaived.
- [x] Every deliberate difference is recorded with its ruling: in
  `PRODUCT.md` ("Capabilities and Constraints"), in the waiver reasons, and
  at the code site. The `_local/<base>-<sha8>` materialisation was not kept:
  ruling R1 replaced it with the Oracle's `_local/<name>`.
- [x] `tools/parity/cases-pending/` is empty at close (only its README).

## Scope ruling (2026-08-29): this branch stops at the marketplace surface

The branch is `feat/marketplace-plugin-parity`; the user ruled the audit had
sprawled. `marketplace-init` is the only pending case in scope: its two pure
bugs are fixed here (the apm-go-only 12-line comment block above
`marketplace:` is gone; the blank-line separator apm-go wrote before the
block is gone -- the Oracle round-trips through ruamel and writes neither,
commands/marketplace/init.py:85-91). Its scaffold now differs from the
Oracle's by exactly one byte sequence: the ruled `# ref: v1.0.0` example
(kept because apm-go's pack rejects branch refs; template.go's comment). It
stays in `cases-pending/` because `tree` is never waived.

`compile-instruction`, `install-local-path-dep`, `uninstall-local` and
`mcp-install-offline` -- and the lockfile-interop / MCP-in-lockfile A/B
rulings -- are OUT OF SCOPE for this branch and are handed to a follow-up
branch with this ticket. Their pure bugs (lockfile YAML style, uninstall
creating a module dir, AGENTS.md template drift incl. the `apm compile`
binary name, mcp apm.yml indentation and `.mcp.json` trailing newline) were
never written down in this file (it had no "Part 2"); issue #50 listed them
again and fixed them, see "Result" below.

## Result (issue #50, 2026-10-12)

Measured against Oracle v0.33.0 (`18c4c43c`) in a clean HOME.

- Lock: Oracle layout, `deployments`, `mcp_servers` / `mcp_configs` /
  `mcp_target_servers`, no `generated_at`, mode 0600 (R1, R2, R5, R8, R19).
  For a local package, for project content only, and for a self-defined MCP
  server the file is byte-identical to the Oracle's apart from the value of
  `apm_version`. **The lock of git and registry sources was not measured
  against the Oracle.**
- Local packages live at `apm_modules/_local/<name>`; a lock in the old
  format is converted by a non-frozen install and read as it is by a frozen
  one (R16).
- `install`, `uninstall`, `install --mcp`: the Oracle's success transcript
  (R10). `compile`: the Oracle's `AGENTS.md` (R11), apm-go's own terminal
  text (R3). A runtime failure of `install`, `update` or `compile` is printed
  once (R15).
- The pure bugs: lockfile YAML style (fixed, Oracle layout); `uninstall`
  creating a module directory (it now only keeps an existing `apm_modules/`,
  empty, as the Oracle does); `AGENTS.md` template (fixed); `mcp:` list
  indentation in `apm.yml` (a list apm-go starts has the Oracle's layout; a
  list the user indented keeps theirs); `.mcp.json` trailing newline (fixed,
  key order kept).
- Runner: the status-symbol normalization also covers a symbol directly
  after `"  "` and `"|  "`; `cwd/apm.lock.yaml`'s `apm_version:` value is
  replaced before the tree comparison (R12).

## Recorded differences that were not changed

Each line gives the Oracle's behaviour, apm-go's, and the exit codes. Items
1-15 are the investigation's measurements; items 2, 12 and 14 were measured
again during the implementation with the same result. Items 16-21 were found
during the implementation.

1. `install` with no `apm.yml`. Oracle: `[x] No apm.yml found` and two hint
   lines. apm-go: `read apm.yml: open apm.yml: no such file or directory`.
   Both exit 1.
   Aligned by issue #81: apm-go prints the Oracle's three lines, with
   `apm-go` in the hints, and exits 1; with a package it creates `apm.yml`
   first, as the Oracle does (PRODUCT.md, "A missing `apm.yml`").
2. `install ./nope`. Oracle: `[x] ./nope -- path does not exist`, then `All
   packages failed validation. Nothing to install.` apm-go: `resolve: loading
   package _local/nope: local dependency source ...: no such file or
   directory`. Both exit 1.
3. `install --mcp` for a name that is already declared. Oracle: exit 2,
   stdout `[!] Install interrupted after 0.0s.`, a usage error on stderr
   whose message has the absolute path of `apm.yml`. apm-go: exit 1.
4. `install --mcp` with no `apm.yml`. Oracle: exit 2 (`No harness detected`).
   apm-go: exit 1.
5. `install --frozen`. Oracle: the deploy summary and `Lockfile presence
   verified. Run 'apm audit' for on-disk content integrity.` apm-go: `Frozen
   install: all dependencies pinned and verified`. Exit codes were not
   recorded for this item.
6. `CI=1`: apm-go's `install` defaults to frozen; the Oracle's does not.
7. `update`. Oracle: `'apm update' refreshes APM dependencies...`, `Checking
   upstream for revision-pin freshness...`, `All dependencies already at
   their latest matching refs.` apm-go: the deploy summary and `Already up to
   date`. With no `apm.yml` the Oracle hands over to `self-update` and exits
   0; apm-go exits 1. `update --dry-run`: the Oracle prints the same three
   lines; apm-go prints nothing.
8. `uninstall` of a package that is not installed. Oracle: exit 1, stops.
   apm-go: a warning, exit 0 (kept, R15).
9. `uninstall <MCP server name>`. Oracle: exit 1 (`Invalid package format`).
   apm-go: removes that server (kept, R15).
10. `uninstall` with no `apm.yml`: different message, both exit 1.
11. `compile` error paths. Oracle: the error is followed by an `[i]` hint
    line, with different words. apm-go: no hint line. For target claude the
    Oracle writes `CLAUDE.md`; apm-go exits 2.
12. A local package that declares a local package. Oracle: installs it to
    `_local/<sha12>/<name>`. apm-go: refuses with exit 1 (supply-chain
    guard, kept); since R20 the refused copy is removed.
13. `pack --json`: stdout is the same. apm-go prints the `No 'license:'
    field...` warning on stderr; the Oracle's stderr is empty.
14. Target codex. Oracle: `install` creates an empty `.codex/` and its
    `deployments` rows have `target: codex`. apm-go: neither. (A skill's
    directory in `deployed_files` and `deployments` is aligned for every
    target of a project install; see 22 for `--global`.)
15. The lock fields of git and registry sources and the terminal text for
    remote packages were not run against the Oracle. Known field differences
    from the Oracle's source: it writes `name`, `version` and `package_type`
    for these entries and no `source` key for git; it writes
    `lockfile_version: '2'` for a git-semver entry; it lowercases a GitHub
    repository name. apm-go writes `source: git`, keeps its own
    `tree_sha256`, and does none of the others.
16. `.mcp.json` mode. Oracle: 0644. apm-go: 0600. (The key order inside
    `env` and `headers` is aligned for target claude: `.mcp.json`, `apm.yml`
    and the lock keep the order given on the command line. The MCP files of
    the other targets were not measured.)
17. A local package that deploys no file. Both print its header and `(files
    unchanged)` and count it. The Oracle also creates an empty `.claude/`;
    apm-go does not, and prints one more line, ` ! _local/dep deployed 0
    files to any target`. Both exit 0.
18. `install ./dep` when `./dep` is already in `apm.yml`. Oracle: `[+] ./dep
    (updated ref in apm.yml)` and `[*] Updated apm.yml dependency entries`.
    apm-go: ` + ./dep (already in apm.yml)` and ` > No new packages to add`.
    Both exit 0.
19. `install --global`: both write `scope: project` in the `deployments`
    rows (measured against the Oracle with a new `HOME`); no difference.
20. A waived difference, for the record: the Oracle's Rich console breaks a
    long status line at column 80 (`uninstall-local`), and the
    `marketplace init` panel row that names `apm-go pack` has three fewer
    padding spaces than the Oracle's row for `apm pack`.
21. The parity runner normalizes sandbox paths, timestamps and hex tokens in
    text files of the tree before it compares them, so the tree comparison is
    not a comparison of raw bytes; `PRODUCT.md` describes what is normalized.
22. `install --global` with a skill. Oracle: copies the files and lists the
    skill's directory (`.claude/skills/<name>`, `content_hash: null`) in
    `deployed_files` and `deployments`. apm-go: deploys a symlink and does
    not list the directory. The Oracle also prints `Installing to user scope
    (~/.apm/)`, a support-scope warning, `Created .../apm.yml`, `Targets
    set: claude (persisted to ...)` and a `<project root> (local)` block,
    prints no `Targets:` line, and creates `~/.apm/config.json`; apm-go
    creates `~/.apm/.gitignore`.
23. `.apm/prompts/` and `.apm/commands/` of a local package, target claude.
    Oracle: deploys `.apm/prompts/ask.prompt.md` as `.claude/commands/ask.md`
    and ignores `.apm/commands/go.md`. apm-go: deploys `.apm/commands/go.md`
    as `.claude/commands/go.md` and ignores the prompt. The line and the lock
    rows have the same form on both sides.
24. Stale cleanup during `install` (a deployed file whose source was
    removed). Oracle: `Cleaned 3 stale files from ./dep`, no path lines, and
    the summary `Installed 1 APM dependency (3 stale files cleaned) in
    0.0s.`. apm-go: `from _local/dep`, one line per path, and no
    parenthesized part in the summary. The files removed and the lock
    written are the same.
25. A failed `install <pkg>`: apm-go prints `Installing...`, `Resolving...`
    and `Updated apm.yml with N new package(s)` after the resolve returns,
    so a run that fails in the resolve prints none of them. A run that fails
    later (a deploy error) still prints the `Updated` line.
26. Differences met while the changed lines got their tests (step 21). None
    was changed; no test asserts the apm-go side of 26a-26k as correct.
    a. A local package with no `apm.yml`, `SKILL.md` or `plugin.json` (only
       `hooks/`, or only `skills/<x>/SKILL.md`). Oracle: refuses it (`Local
       package is not a valid APM package ...`, exit 1). apm-go: installs it
       and writes `package_type: hook_package` / `skill_bundle`.
    b. `name` / `version` of a local package that has only `plugin.json` or
       only `SKILL.md`. Oracle: records them (`version: unknown` for the
       skill). apm-go: records neither.
    c. A local directory name with a backslash (`./we\ird`). Oracle: lock
       `repo_url: _local/ird`, directory `apm_modules/_local/we\ird`. apm-go:
       `_local/we_ird` in both.
    d. A project with an empty `.apm/instructions/`. Oracle: prints the
       `<project root> (local)` block with `(files unchanged)`. apm-go: no
       block.
    e. `.mcp.json` bytes. Oracle: escapes non-ASCII (`café`), rewrites
       the number `1.50` of an existing file as `1.5`, writes `"args": []`.
       apm-go: raw UTF-8, keeps `1.50`, omits an empty `args`.
    f. A server that `.mcp.json` already has and `apm.yml` declares. Oracle:
       leaves the entry and counts one server. apm-go: rewrites the managed
       keys and counts it.
    g. `install --mcp <name>` a second time. Oracle: `MCP server '<name>'
       already declared; checking integrations` ... `Verified MCP server
       '<name>' integration`. apm-go: the first-time transcript.
    h. `install --mcp one --target codex` after claude. Oracle: the lock
       names codex only. apm-go: claude and codex. A target without MCP
       support: the Oracle prints `Skipped targets without MCP support:
       agent-skills` after the block and creates `.agents/`; apm-go prints
       its own line before the block and creates nothing. `.claude` being a
       file: Oracle exit 1, apm-go a warning and exit 0.
    i. An MCP server a package declares. Oracle: prints `Trusting direct
       dependency MCP ...`, labels it `(self-defined, stdio)`, writes
       `mcp_configs.<name>` and `mcp_config_provenance`, and leaves the
       server in `.mcp.json` when the package is uninstalled. apm-go: none of
       these; it removes the server with the package.
    j. A registry MCP server. Oracle: two more lines (`Using MCP registry`,
       `Looking up ...`), `.mcp.json` key `remote` with type `http`, and an
       http registry needs `MCP_REGISTRY_ALLOW_HTTP=1`. apm-go: key
       `io.example/remote`, type `streamable-http`, no such requirement. The
       lock is the same.
    k. `uninstall` writes an MCP server that `apm.yml` declares but that was
       never deployed into the lock's `mcp_servers`; the Oracle keeps only
       recorded ones. `install ./dep` indents the new `apm.yml` list item by
       four spaces, the Oracle by two. A `deployments` row that is not a
       mapping: Oracle error, apm-go skips the row. A lock with `lsp_*`
       keys: the Oracle's `target: lsp` deployment row is, from the code,
       not re-derived by apm-go (not run).
    l. `install --mcp` when `.mcp.json` is not valid JSON. Oracle: warns,
       rewrites the file from scratch, exit 0. apm-go: refuses with exit 1
       and keeps the file.
27. Two deliberate deviations from the pull request review (step 31).
    a. `uninstall` of the last package or MCP server when the lock has a
       top-level key apm-go does not model. Oracle: deletes the lock.
       apm-go: keeps it, with `dependencies: []` and the key as it was
       (`PRODUCT.md`: such keys survive).
    b. `install` on a lock that has no `apm_version` line and records a
       dependency or an MCP server. Oracle: does not add the line. apm-go:
       writes the lock again with the line unless `--no-provenance` is
       given, and with the flag removes an existing line. `install --mcp`
       follows the flag too. Both leave a line with another value alone.
