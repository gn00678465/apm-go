# Ticket 36 — apm.lock.yaml lists deployed files that no longer exist

Status: OPEN (2026-09-09)

## What

`apm.lock.yaml` records a deployed file that no longer exists:

```
local_deployed_files:
- .claude/agents/research.md
```

That path was removed by `e6c9afe` ("chore(trellis): 移除 trellis 工作流框架"),
2026-08-23, an ancestor of `f3683e9` — a pre-existing inconsistency on `main`,
not something the `plugin-manifest-validate-01M21E5Q` mission introduced.

An earlier draft of this ticket also claimed `.github/agents/research.agent.md`
for the same reason. That was wrong: it still existed at `f3683e9` and was
deleted by `0efb9e8` on this very branch, so its lockfile entry was this
branch's to clean up and has been removed here. Only the `.claude/agents`
entry remains open.

## Why it is not fixed here

Found during that mission's pre-merge audit. Fixing it means editing
`apm.lock.yaml`, which is a user-owned round-trip file outside the mission's
declared scope, and the repo's engineering rules say a pre-existing defect gets
recorded rather than folded into an unrelated change. Recorded here instead.

## Impact

`lockfile.VerifyDeployedState` treats the deployed set as the source of truth for
`audit` and for a frozen `install`. Entries pointing at absent files can make
those commands report drift that no longer reflects reality.

## Fix when picked up

Remove both entries and their `local_deployed_file_hashes` rows through the
round-trip patch helpers (`yamlcore`), never by rewriting the file, and confirm
`apm-go audit` is clean afterwards.
