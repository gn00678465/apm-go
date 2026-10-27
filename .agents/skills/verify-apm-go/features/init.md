# Initialize a project

`init` creates a project directory with an `apm.yml`. `plugin init` does the same and adds a `plugin.json`. On a TTY both ask for metadata and targets in a framed transcript; with `--yes`, without a TTY, or under CI they use defaults and print plain status lines.

## Sub-features

- `init-yes` creates `<name>/apm.yml` with defaults and no prompt.
- `init-overwrite` runs again on an existing project with `--yes` and overwrites `apm.yml`.
- `init-tty` asks metadata, targets and a confirmation, then writes what was answered.
- `init-abort` leaves the prompt with `^C`.
- `plugin-init-yes` creates `apm.yml` and `plugin.json`.

## How to get to it (user POV)

- Run `apm-go init <name> --yes` in a terminal or a script.
- Run `apm-go init <name>` in a terminal and answer the prompts.
- Run `apm-go plugin init <name> --yes --target claude`.

## Driving it with verify.sh

Preconditions:

- `sandbox/work` is empty.
- `tmux` is on `PATH` for `init-tty` and `init-abort`.

- **init-yes.** Run `$V run RUN init-yes -- init demo --yes`. Exit code `0`. stdout has ` > Created project directory: demo`, ` + APM project initialized successfully!`, a `Created Files` table listing `apm.yml`, and a `Next Steps` panel. `init-yes.tree` gains exactly one file, `./work/demo/apm.yml`; it starts with `name: demo` and `version: 1.0.0`.
- **init-overwrite.** Run `$V run RUN init-again -- init demo --yes`. Exit code `0`. stdout adds ` ! apm.yml already exists` and ` i --yes specified, overwriting: apm.yml`. The tree lists the same single file.
- **init-tty.** Run `$V tty RUN`, then `$V keys RUN '$APMGO init tui' Enter` and `$V expect RUN 'Author'`. The screen shows the frame `┌  Setting up your APM project` and one form, `Project metadata`, with four fields: `Project name`, `Version`, `Description`, `Author`, each prefilled. Send `$V keys RUN Enter` four times (one per field), then `$V expect RUN 'Select targets for this project'`. The list is `copilot`, `claude`, `opencode`, `codex`, `antigravity`, none selected. Send `$V keys RUN Down`, `$V keys RUN Space`, then `$V expect RUN '\[•\] claude'`. Send `$V keys RUN Enter` and `$V expect RUN 'About to create'`; the note lists name, version, description, author and `targets:     claude`. Send `$V keys RUN Enter` to accept `Is this OK?`, then `$V expect RUN 'Done!' init-tty`. The transcript ends with an `Initializing` note that holds the success content, then `└  Done!`. `sandbox/work/tui/apm.yml` has `targets:` with `- claude`.
- **init-abort.** Start as in `init-tty`, `$V expect RUN 'Author'`, then `$V keys RUN C-c` and `$V expect RUN 'user aborted'`. The screen shows ` x read project metadata: user aborted` and the shell prompt `verify$` returns.
- **plugin-init-yes.** Run `$V run RUN plugin-init -- plugin init plug --yes --target claude`. Exit code `0`. The `Created Files` table lists `apm.yml` and `plugin.json`. The tree gains `./work/plug/apm.yml` and `./work/plug/plugin.json`; `plugin.json` has `"name": "plug"` and `"version": "0.1.0"`; `apm.yml` has `targets:` with `- claude`.
- **Proof.** For the non-interactive steps, the `.out`, `.rc` and `.tree` files. For `init-tty`, `evidence/init-tty.screen` plus `apm.yml` read from the sandbox.

## Gotchas

- `$V run` is never interactive: stdin is `/dev/null` and there is no TTY, so `init` without `--yes` takes the non-interactive path there. Only `$V tty` reaches the prompts.
- After the TUI exits, the shell prompt can show stray characters such as `2026;2$y2027;0$y` (late terminal replies). Send `$V keys RUN C-u` before typing the next command, or they become part of it.
- The terminal type inside tmux must be `tmux-256color` (the helper sets it). With an `xterm` type the form accepts keys but the screen does not redraw, and `expect` times out.
- Send keys only after `expect` has shown the prompt. Text typed early lands in whichever field has focus.
- Typing into `Project name` changes the `name:` in `apm.yml` but not the directory, which is the argument given to `init`.
- The project directory is created before the prompts. After `init-abort` the directory `tui/` exists and is empty.
- `init-yes` prints `Created project directory: demo` on the overwrite run too, when the directory already exists.
