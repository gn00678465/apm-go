# Ticket 35 — implementing mission plugin-manifest-validate-01M21E5Q without lane worktrees (Windows)

Status: RULED (2026-09-09)

## What

`spec-kitty agent action implement WP01 --mission plugin-manifest-validate-01M21E5Q`
cannot complete on this machine. The claim fails inside
`Validate planning state` with:

```
Refusing to write artifact outside coordination worktree
(fd-relative no-follow writes unsupported on this platform):
<repo>/kitty-specs/plugin-manifest-validate-01M21E5Q/analysis-report.md
```

Traced call chain (spec-kitty 3.2.6 and 3.2.6.1, both reproduce):

```
cli/commands/agent/workflow.py implement
  -> _create_workspace
  -> cli/commands/implement.py _ensure_planning_artifacts_committed_git
  -> _commit_planning_artifacts_transaction
  -> coordination/atomic_write.py _open_confined_parent_fd
```

`_open_confined_parent_fd` raises unconditionally when the platform lacks
`os.supports_dir_fd` / `O_DIRECTORY` / `O_NOFOLLOW`. On Windows
`os.supports_dir_fd` is empty, so every confined write fails, including the
transaction's own rollback — which is why the rollback error masks the
original one.

The path is reached because the coordination branch
`kitty/mission-plugin-manifest-validate-01M21E5Q` (at `0211ec2`) is not an
ancestor of `feat/plugin-validate` and lacks 26 planning files (2318 lines),
so the claim tries to sync them onto the coordination ref first.

Not the cause, all verified: the analysis report is fresh and `ready`
(`43ee523`), `spec-kitty doctor coordination` is all-green,
`doctor mission-state --audit` reports zero errors, and charter preflight
passes for the `implement` consumer.

## Ruling

User ruling, 2026-09-09, quoted verbatim from the mission conversation:

> 2

where option 2 was: 「不用 lane worktree，直接在 `feat/plugin-validate` 上依 WP
提示實作。程式碼結果相同，但 charter 的 Branch Strategy 明文要求每個 mission
走 worktree lane，這屬於需要你裁定的偏差。」

## Scope

Applies to this mission only, on this Windows workstation, for as long as the
platform limitation stands. It suspends exactly one clause of the charter's
Branch Strategy — the per-work-package execution lane in `.worktrees/`. It
does not touch anything else, and none of the following are waived:

- the mission still reaches `main` only through a pull request;
- one approval is still required and the reviewer is not the implementer;
- test-first (RED observed before GREEN) still holds per work package;
- `sh tools/gate.sh -scope plugin-manifest-validate` must still pass;
- the output-contract rules, including ticket 34's realexec strength, still hold.

If spec-kitty gains a Windows-capable write path, or the work moves to a POSIX
host, lane worktrees resume and this exception ends.

## Acceptance criteria

- [ ] Each work package is implemented as its own commit series on
  `feat/plugin-validate`, in work-package order, with tests committed before
  the implementation they cover.
- [ ] Each work package is reviewed independently before the next one starts.
- [ ] The PR description quotes this ruling and names the platform limitation.
- [ ] `sh tools/gate.sh -scope plugin-manifest-validate` is green before the PR.
