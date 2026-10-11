# apm-go verification map

This directory is the maintained source for verifying the user-facing behaviour of apm-go. Read this index before driving the binary, then use the matching feature file as the recipe.

## Baseline preconditions

- `V=.claude/skills/verify-apm-go/scripts/verify.sh`, run from inside the repo.
- `$V doctor` exits 0.
- A fresh run from `$V new NAME`; its printed path is `RUN` in every recipe.
- Fixtures are created inside `RUN/sandbox/work` before the first drive. Several recipes copy a fixture from `tools/parity/cases/` or `tools/parity/cases-pending/`; those directories are read-only inputs.

## Driving conventions

- Start every recipe from an empty `sandbox/work` unless its preconditions say otherwise.
- Treat every command as literal. Keep names and flags unchanged.
- Non-interactive drives go through `$V run RUN STEP ... -- ARGS`. Interactive drives go through `$V tty` / `keys` / `expect`.
- Paths in output are absolute sandbox paths. Compare against the path shape, not a recorded string.
- Recorded output in these files was observed at commit `1e42c30`. It tells you what to look for; it is not an expected-value table. The parity corpus and `tools/gate/realexec.sh` own exact bytes.

## Proof and skip reporting

- CLI proof is the five evidence files of the step: command, stdout, stderr, exit code, tree.
- Mutation proof includes the file read back from the sandbox and a tree diff against the previous step.
- Read-only proof is an identical tree before and after.
- Record the feature ID and the entry point used with every artifact.
- Report an unreachable path with the command attempted and the unmet precondition (network, a credential, a TTY).
- Do not report a skipped entry point as verified through a different one.

## Feature entry contract

Each feature file starts with an H1 title and one paragraph describing the user-visible behaviour. It then uses exactly four H2 sections in this order.

1. `Sub-features` lists short IDs with one line for each behaviour.
2. `How to get to it (user POV)` lists every user entry point.
3. `Driving it with verify.sh` starts with `Preconditions:` and uses labeled bullets that pair each user action with an exact command and observable result.
4. `Gotchas` lists traps that can waste or invalidate a verification run.

Keep implementation details out of the map. Name only user paths, stable handles, required state, commands, and observable proof.

## Features

- [Initialize a project](./init.md) covers `init` and `plugin init`, non-interactive and on a TTY.
- [Install, audit, uninstall](./install.md) covers a local-path dependency through install, frozen install, audit, and uninstall.
- [Compile instructions](./compile.md) covers `compile` writing `AGENTS.md` and leaving it untouched on a repeat.
- [Pack a plugin](./pack.md) covers `pack` in bundle, archive, dry-run and JSON forms, and `plugin validate`.
- [Marketplace](./marketplace.md) covers authoring a marketplace, registering it, browsing and searching it.

## Not mapped yet

Commands with no feature file: `update`, `install --mcp`, `install --global`, `doctor`, `experimental`, `normalize`, `validate`, `self-update`, `completion`, and the remote paths of `install` and `marketplace` (`check`, `outdated`, `package update`). Add a file before claiming one of them verified.
