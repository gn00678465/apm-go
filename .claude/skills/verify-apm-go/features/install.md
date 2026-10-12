# Install, audit, uninstall

`install` resolves the dependencies of `apm.yml`, copies each into `apm_modules/`, deploys its primitives into the target's directories, and records every deployed file with its hash in `apm.lock.yaml`. `audit` re-checks those hashes. `uninstall` removes the package, its deployed files, and its `apm.yml` entry. This file covers a local-path dependency, the one source that needs no network.

## Sub-features

- `install-local` installs `./dep` and deploys one instruction to `.claude/rules/`.
- `install-frozen` re-runs `install` under CI and verifies against the lock without resolving.
- `audit-clean` reports the deployed files as verified.
- `audit-tampered` fails when a deployed file was edited.
- `uninstall-clean` removes the package and what it deployed.
- `uninstall-modified` keeps a deployed file the user edited.

## How to get to it (user POV)

- Run `apm-go install <path> --target <target>` to add and install one dependency.
- Run `apm-go install` to install what `apm.yml` declares.
- Run `apm-go audit` in an installed project.
- Run `apm-go uninstall <path>`.

## Driving it with verify.sh

Preconditions:

- `sandbox/work/proj` holds a copy of `tools/parity/cases/install-local-path-dep/fixture/` (copy its contents, including `dep/`): `cp -r tools/parity/cases/install-local-path-dep/fixture/. RUN/sandbox/work/proj/` after `mkdir`.
- Every drive uses `--cd proj`.

- **install-local.** Run `$V run RUN install-local --cd proj -- install ./dep --target claude`. Exit code `0`. stdout has ` + Validating 1 package...`, ` > Resolving ./dep...`, ` i Targets: claude  (source: --target flag)`, the block `   + ./dep (local)` with `  |-- 1 rule(s) integrated -> .claude/rules/`, and ` + Installed 1 APM dependency in <time>.`. The tree gains `proj/.claude/rules/example.md`, `proj/apm.lock.yaml` (mode `0600`), `proj/.gitignore`, and a copy of the package under `proj/apm_modules/_local/dep/`. `apm.yml` now lists `- ./dep` under `dependencies.apm`. `apm.lock.yaml` has one dependency with `repo_url: _local/dep`, `source: local`, `local_path: ./dep`, `deployed_files: [.claude/rules/example.md]`, a `deployed_file_hashes` entry whose sha256 equals the hash of that file in the tree, and a `deployments` row for the same file. A second `install` prints `1 rule(s) adopted` and `(files unchanged)` for the block.
- **audit-clean.** Run `$V run RUN audit --cd proj -- audit`. Exit code `0`, stdout ends ` + audit: 1 deployed files verified`. `audit.tree` equals `install-local.tree`.
- **install-frozen.** Run `$V run RUN install-ci --ci --cd proj -- install`. Exit code `0`. stdout is ` i CI environment detected, defaulting to frozen install` and ` + Frozen install: all dependencies pinned and verified`. The tree is unchanged.
- **audit-tampered.** Append a line to `RUN/sandbox/work/proj/.claude/rules/example.md` with a file tool, then run `$V run RUN audit-tampered --cd proj -- audit`. Exit code `1`. stdout has ` x content-integrity violation: .claude/rules/example.md expected sha256:… observed sha256:…` and ` x audit failed: 1 content-integrity violation(s) (first: .claude/rules/example.md)`.
- **uninstall-modified.** With the file still edited, run `$V run RUN uninstall-modified --cd proj -- uninstall ./dep`. Exit code `0`. stdout has ` ! keeping ".claude/rules/example.md": modified since deploy (hash mismatch)`, ` i Removed _local/dep from apm_modules/`, ` ! Kept 1 deployed file(s) that were modified or are shared`, and ` + Uninstall complete: Removed 1 package(s) from apm.yml, Removed 1 package(s) from apm_modules/`. The tree keeps `.claude/rules/example.md`, keeps `apm_modules/` as an empty directory, and loses `apm.lock.yaml`; `apm.yml` no longer lists `./dep`.
- **uninstall-clean.** In a second copy of the fixture (`sandbox/work/proj2`), run `install-local` and then `$V run RUN uninstall-clean --cd proj2 -- uninstall ./dep` without editing anything. Exit code `0`. stdout has ` i Cleaned 1 stale file from ./dep`. The tree loses `.claude/rules/example.md`, the content of `apm_modules/` (the empty directory stays) and `apm.lock.yaml`.
- **Proof.** The `.out` and `.rc` of each step, `apm.yml` and `apm.lock.yaml` read from the sandbox after `install-local`, and the tree diffs `install-local` → `audit` (empty) and `install-local` → `uninstall-clean`.

## Gotchas

- Without `--ci`, a plain `install` resolves again and may rewrite the lock. Frozen behaviour needs `--ci` or the `--frozen` flag.
- A local package is copied to `apm_modules/_local/<directory name>` and the lock's `repo_url` is `_local/<directory name>` (issue #50). A lock from before that change names it `_local/<name>-<8 hex>`; a frozen install reads such a lock as it is, and any other install converts it.
- The sandbox sits inside the apm-go repository. The helper sets `GIT_CEILING_DIRECTORIES` so git inside the sandbox does not discover that repository. The lock of a local-path dependency has no `resolved_commit` either way. A fixture that must be a git repository needs its own `git init` inside the sandbox.
- `install` adds `apm_modules/` to `.gitignore`, creating the file. That is one of the files the tree gains, and `uninstall` leaves it in place.
- stdout of `uninstall` prints the absolute sandbox path of `apm.yml`.
- A remote dependency (`owner/repo`, a marketplace reference) needs the network and a reachable host. It is not covered here.
