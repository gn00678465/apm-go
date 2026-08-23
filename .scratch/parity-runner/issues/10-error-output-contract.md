# 10 — Cross-command error/warning output contract (systemic F08)

**What to build:** A single decision, implemented once at the `ux` boundary, for where apm-go's user-facing errors and warnings go and how they are prefixed — so that every command matches the Oracle's observable contract (or a single documented, time-boxed deviation), instead of each command drifting on its own.

**Blocked by:** 02 — runner diff/gate attempt 2 (needs a body-level error comparator; see below).

**Status:** ready-for-agent

**Origin:** first real parity run (`/tmp/p5`) + `marketplace browse nonexistent` probe + evaluator ruling `.review/eval-ticket-05.md` §B/§C. Oracle: `core/command_logger.py:81-130` (`_rich_*` helpers → one stdout Console), `commands/_helpers.py:72-93` (`_get_console()`).

## The finding

| | Oracle | apm-go today |
|---|---|---|
| error channel | **stdout** | stderr |
| error prefix | `[x] ` | `Error: ` |
| warning channel/prefix | stdout, `[!] ` | stderr, `!` glyph |
| rich table under NO_COLOR/CI/no-TTY | still rendered (box-drawing, no ANSI) | plain fallback |

This is every marketplace command, `doctor`, and `search` — not one command. eval-plan §8.1 forbids normalising channel/severity away; §8.2 #8 lists output channel as an assertion; taxonomy F08 names stderr.

## Acceptance criteria

### Decision (first, recorded in this ticket file before code)
- [x] Choose ONE: (A) align apm-go to the Oracle — errors/warnings to stdout with `[x] `/`[!] ` via `ux`, rich renderers (tables/boxes) always on; or (B) keep apm-go's current contract as a product decision with a dated waiver and an owner. Write the choice, reason, and date here. Default recommendation from the evaluator is (A) as the end state.

**Decision: (A), 2026-08-23.** Confirmed with fresh evidence: `NO_COLOR=1 CI=1 apm-go marketplace browse nonexistent` writes `Error: ...` to stderr (exit 1); the pinned Oracle (`uv run --project .../apm apm marketplace browse nonexistent`, same env) writes `[x] Failed to browse marketplace: ...` to **stdout** (exit 1), nothing on stderr. Read `commands/_helpers.py:72-93`: `_get_console()` builds a `rich.Console(stderr=_console_stderr)` where the module-level `_console_stderr` defaults `False` and is only ever flipped by `set_console_stderr(True)` (reserved for a `--json` mode apm-go doesn't have yet) — so every Oracle command's console output, including `_rich_error`/`_rich_warning` (`utils/console.py:160-172`, prefixes from `STATUS_SYMBOLS` at `utils/console.py:37-61`: `"[x]"` error, `"[!]"` warning), lands on stdout by default. `core/command_logger.py:81-130`'s `CommandLogger.error`/`.warning` are thin wrappers over the same `_rich_error`/`_rich_warning`. No oracle code path routes ordinary errors to stderr. No concrete reason found against (A) — apm-go's current stderr/`Error: ` contract is simply cobra's untouched default (`c.PrintErrln(c.ErrPrefix(), err.Error())`, ErrPrefix() == "Error:"), not a deliberate product decision, so there is nothing to preserve. Proceeding with (A).
- [ ] If (B): every affected case gets its OWN `waivers.json` entry (no wildcard), `fields` limited to the channel/prefix difference, and the runner must first prove the error BODY and exit code are equal (next item) — otherwise the waiver is invalid.

### Runner support (needed for either choice)
- [x] Runner gains an `error_body` diff field: for cases with non-zero exit on either side, strip a leading `[x] `/`Error: `/`[!] `/`!` prefix and leading/trailing whitespace from the FIRST non-empty line of (stdout ∪ stderr) on each side and compare. A channel/prefix-only difference leaves `error_body` equal while `stdout`/`stderr` differ — so a waiver on `stdout`/`stderr` can never hide a wording or exit-code drift. Implemented in `tools/parity/diff.go` (`errorBody`/`stripErrorBodyPrefix`, wired into `diffCase`), covered by `TestDiffCase_ErrorBody_*` in `diff_test.go`.

### Implementation (if A)
- [x] `ux.Error`/`ux.Warn` (and Cobra's root error handler output) write to stdout with `[x] `/`[!] ` prefixes when mirroring the Oracle; one switch in `ux`, no per-command edits. Implemented as `internal/ux/printer.go`'s `errWriter` (redirects a caller-supplied `os.Stderr` to `os.Stdout`, every existing call site unchanged) + `oracleLine` (literal `"[x] "`/`"[!] "` prefix). `cmd/apm-go/main.go`'s root command sets `SilenceErrors: true` and prints the final error itself via `ux.Error` instead of cobra's default `Error:`/stderr.
- [ ] `ux.Table`/`ux.Section`/`ux.Box` render box-drawing without ANSI under NO_COLOR/CI/no-TTY instead of falling back to plain lines; `IsRich()` stays for prompt/spinner decisions only. **Deferred** — out of this commit's scope per direct instruction (`doctor.go`/`search.go`'s `IsRich()`-gated plain-vs-table fallback left untouched; `search.go` explicitly not to be touched, ticket 05 owns it). `ux.Table`/`Section`/`Box` themselves already always render box-drawing (lipgloss downsamples color only, never the border characters) — only the two commands' own plain-fallback branches remain.
- [x] Every existing `cmd/apm-go` test that asserted stderr/`Error:` is updated in the same commit; `go test ./cmd/apm-go/` green.
- [x] Runner cases `browse-unknown-marketplace`, `list-empty` added (per direct instruction, narrower than the ticket's original list): both now show a clean `exit_code`/`stderr` diff (the channel/prefix bug this ticket exists to fix) and only the pre-existing, out-of-scope message-wording + ticket-12 global-config-tree gap remains, field-precisely waived in `waivers.json`. `doctor-git-missing` (ticket 08's fixture doesn't exist yet) and the `search-*` cases (ticket 05's territory, `search.go` explicitly not touched here) are **not** in this commit. Full-corpus run (`tools/parity/cases`, real pinned Oracle) before/after this change: 27/30 → 25/30 unwaived, zero regressions (`pack-refuse-agent-plugin`/`pack-refuse-apm`'s existing waivers updated in the same commit: `stderr` no longer differs and drops out, `error_body` added — same documented exporter-not-implemented gap, not a new one).

### Either way
- [ ] `doctor`'s rich/plain split and `search`'s are governed by the same rule; no command chooses its own. **Deferred**, same reason as the `ux.Table` item above.
- [x] AGENTS.md "UX layer" paragraph updated in one sentence to state the contract.
