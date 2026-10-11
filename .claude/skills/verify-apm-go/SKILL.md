---
name: verify-apm-go
description: Drive the real apm-go binary (CLI and its interactive init TUI) in a throwaway sandbox and capture evidence — stdout, stderr, exit code, resulting file tree. Use to prove a change to a command works the way a user would see it, to reproduce a reported behaviour, or to check a command before and after a fix. Not a replacement for tools/gate.sh or the parity gate.
---

# Verify apm-go

apm-go is a short-lived CLI: there is no server to keep alive. You build the binary once, then every drive runs it in its own sandbox. One interactive surface exists (`init` / `plugin init` on a TTY); it is driven through tmux.

All commands below go through one helper. Run it from anywhere inside the repo:

```sh
V=.claude/skills/verify-apm-go/scripts/verify.sh
```

This skill answers "what does the binary do when a user runs it". It does not replace the project gates: `go test ./...` checks Go-level correctness, `tools/gate.sh` is the verification gate, and `tools/parity` (CI) is the authority on Oracle parity. See `AGENTS.md`.

## Launch

```sh
$V build            # go build ./cmd/apm-go -> .scratch/verify/bin/apm-go
RUN=$($V new NAME)  # creates .scratch/verify/runs/<timestamp>-NAME/{sandbox,evidence}
```

Ready means `$V doctor` exits 0. `RUN` is an absolute path; shell variables do not survive between tool calls, so keep the printed path and pass it literally.

The sandbox has two directories: `sandbox/work` (the working directory of every drive) and `sandbox/home` (`HOME`; `APM_CONFIG_DIR` is `sandbox/home/.apm`). Put fixtures in `sandbox/work` with ordinary file tools before driving.

Teardown is `$V clean RUN` (see Cleanup).

## Doctor

```sh
$V doctor
```

Read-only. It fails when: there is no binary; `--version` is not `apm-go version dev`; a non-test `.go` file, `go.mod` or `go.sum` is newer than the binary; the binary was built from a commit other than `HEAD`; `git` is missing. Every failure line names the fix, which is always `$V build`. It also reports sandboxes left by earlier runs.

Run it before the first drive, and again after any drive that failed for a reason you do not understand. A stale binary is the usual cause of "my fix does not show".

## Drive

Non-interactive (the normal case):

```sh
$V run RUN STEP [--ci] [--cd DIR] [--stdin FILE] -- ARGS...
```

- Runs `apm-go ARGS...` with working directory `sandbox/work` (or `sandbox/work/DIR`).
- `STEP` names the evidence files and must be unique within the run.
- The environment is built from nothing: only `PATH` is inherited. No token, proxy, or CI variable of your shell reaches the binary. `NO_COLOR=1`, `TERM=dumb`, and `GIT_CEILING_DIRECTORIES` (so git inside the sandbox does not discover the apm-go repository that surrounds it) are set.
- `--ci` adds `CI=1`. Without it the binary sees no CI variable. `install` behaves differently under CI (frozen by default), so choose deliberately.
- stdin is `/dev/null` unless `--stdin FILE` is given; no prompt can block a drive.
- Prints the exit code, stdout, and stderr. A non-zero exit code of apm-go is a result, not a helper failure: the helper itself exits 0.

Interactive (`init` / `plugin init` prompts only):

```sh
$V tty RUN                       # tmux shell in sandbox/work; $APMGO is the binary
$V keys RUN '$APMGO init demo' Enter
$V expect RUN 'Project name'     # waits up to 10s for the text, prints the screen
$V keys RUN Enter                # tmux send-keys syntax: Enter, Space, Down, C-c, or literal text
$V expect RUN 'Done!' final      # third argument saves evidence/final.screen
```

Always `expect` the next prompt before sending keys to it; keys sent early are typed into the previous field. Before a second command in the same shell, send `$V keys RUN C-u` to clear stray terminal replies from the prompt. The recipe with the real prompt strings is in [features/init.md](features/init.md).

Feature recipes live in [features/](features/README.md). Read the index, then the file for the feature you are proving. A proof that drives one entry point is incomplete when the feature file lists others.

## Evidence

Every `run` step writes five files to `RUN/evidence/`:

| file | content |
|---|---|
| `STEP.cmd` | working directory, CI flag, argv |
| `STEP.out` | stdout, complete |
| `STEP.err` | stderr, complete |
| `STEP.rc` | exit code |
| `STEP.tree` | the whole sandbox after the step: `sha256  path` per file, `-> target  path` per symlink, `dir  path` per empty directory |

Interactive steps write `NAME.screen` when `expect` is given a third argument.

Proof standards:

- Drive the command a user would type. Do not call internal functions or test hooks.
- Capture the action and the resulting state. For a command that writes, compare the `.tree` of the step before with the step after (`diff A.tree B.tree`); for a command that must not write (`audit`, `plugin validate`, `--dry-run`), show that the two trees are identical.
- Verify side effects by reading the file from the sandbox (`apm.yml`, `apm.lock.yaml`, the deployed file), not only the status line that claims it.
- A status line on stdout is not proof of the stream: `ux.Error` and `ux.Warn` go to stdout by design, and `pack --json` moves status lines to stderr. Check `.out` and `.err` separately.
- `--dry-run` is verified by the tree, not by its name.
- Network: recipes in the feature map use local fixtures only. A drive that needs a remote (a GitHub dependency, a remote marketplace, `self-update`, `doctor`'s network checks) is outside what this skill proves; report it as unreachable with the command you would have run.

## Cleanup

```sh
$V clean RUN
```

Removes `RUN/sandbox` and kills the tmux server of that run (its own socket, `apmgo-verify-<run name>`). It prints how many evidence files remain. `RUN/evidence` is never removed; cite that path in your report.

Clean after every run, including a failed one. Clean only runs you started; `$V doctor` lists sandboxes that are still present. `.scratch/` is gitignored, so nothing here shows in `git status`.

## Helpers

`scripts/verify.sh` is the only helper. `$V` with no argument prints its usage. Subcommands: `build`, `doctor`, `new`, `run`, `tty`, `keys`, `expect`, `clean` — each shown above.
