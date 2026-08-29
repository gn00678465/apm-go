# 32 — command-semantics gaps exposed by ticket 31's new corpus cases

**What to build:** make five command families produce the Oracle's words AND
on-disk footprint, then move their cases from `tools/parity/cases-pending/`
back into `tools/parity/cases/`. **Status:** open (2026-08-29; verifier brief
10 leaves five documented deviations pending user ruling).

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
- [ ] Each of the five cases runs green in `cases/` with at most a rendering-class waiver (wrap/box/timestamps/paths), every word and every written byte matching the Oracle; the `tree` field is never waived.
- [x] `marketplace-add-nonexistent` added and green after a field-scoped
  stdout rendering waiver; its tree remains unwaived.
- [ ] Where a footprint difference is a deliberate apm-go design (e.g. `_local/<hash>` materialisation), it is recorded as a dated deviation comment with the reason AND the user has ruled on it -- not decided by the implementor.
- [ ] `tools/parity/cases-pending/` is empty at close.
