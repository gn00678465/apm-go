<!-- reviewed-through: 8b832c0 (2026-10-20 20:58 +0800) -->
# Lessons

Candidate rules from reviews of merged apm-go work. Breaking a lesson does not block a PR, but read them before changing code, follow each one that applies, and say in the PR description what you did. How a review adds to this file is in AGENTS.md "Lessons review".

Each entry is a rule, one sentence of why, one checkable reference for each day it was seen (UTC+8), and `Seen N×`. A lesson seen on 3 separate days moves into AGENTS.md "Promoted rules" and leaves this file. Each review's summary is in the description of its `lessons:` PR.

## Lessons

**Run a change that splits, joins or resolves paths, or that checks symlinks, in the Windows sandbox before merge, with a junction as well as a symlink.**
- CI runs only on Linux, where code that splits paths on `/` or reads links with `os.Lstat` passes and fails on Windows.
- 2026-10-15: `22e71c6` (PR #90, `joinRealpath` splits paths only on `/`).
- 2026-10-16: issue #116 (junctions bypass the containment checks of PR #91 and #92).
Seen 2×

**For any change that reads the lock, cleans stale or orphan files, or renames module keys, run install, update, frozen install and uninstall on a lock written by the previous release and on one written by Oracle 0.33.0 before handing over.**
- These changes pass their own tests and break only on the upgrade path.
- 2026-10-16: board 75 event 2692 (packages removed from a legacy lock leave their files).
- 2026-10-17: board 114 event 3462 (the old-clone cleanup deletes the new package).
Seen 2×

**Before committing new test fixtures, run `git status --short --ignored` and confirm that none of them is ignored (`*.out`, `apm_modules/`).**
- An ignored fixture is not committed, so the test passes locally and fails or tests nothing in CI.
- 2026-10-16: board 82 event 2920 (10 `testdata/deps/*.out` files and four `home_fixture/.apm/apm_modules/` trees ignored).
Seen 1×

**When you open or fill a release-notes issue, list every PR merged since the previous tag (`git log --first-parent <tag>..origin/main`) and check each one's Behavior and Risks for an upgrade-visible change.**
- Release notes are written from that issue, so a change missing there is missing from them.
- 2026-10-17: issue #133 (opened without PR #85, #91, #92, #93 and #94).
Seen 1×

**Check each limitation you find against the handoff conditions of the owner ruling before you hand over; when it breaks one, fix it in the same round instead of recording it as a known limitation or a question.**
- The ruling has already decided it, so the round fails review and the fix costs a full cycle.
- 2026-10-17: board 118 event 3625 (a frozen install failure written into PRODUCT.md and To confirm, which issue #118 comment 6229823230 forbids).
Seen 1×

**When a fix round changes what a PR's Behavior section says, update the PR's line in the release-notes issue in the same step.**
- Release notes are written from that issue, so a case added only to the PR body is missing from them.
- 2026-10-17: PR #138 body edit at 06:31:25Z, after the last edit of its issue #133 line at 05:20:29Z (edit histories).
Seen 1×

**When a change alters what a deploy writes or records, run `install -g` by symlink and then `install -g --frozen` by the copy fallback (the restricted token in the Windows sandbox), and the reverse, before handing over.**
- One lock has to restore under both ways of a `--global` deploy, and only a frozen install in the other mode shows a difference.
- 2026-10-17: board 119 event 3691 (a bare CR in an agent fails the frozen install in both directions).
Seen 1×

**When you port a mechanism from the Oracle, port the form its cited lines use, not only the setting; where apm-go's form differs, test the case the Oracle's form handles.**
- A setting with the right value in the wrong place passes every ordinary test.
- 2026-10-17: board 119 event 3691 (`git clone -c core.autocrlf=false` loses to `GIT_CONFIG_*`, where the Oracle passes `-c` before the subcommand).
- 2026-10-20: board 126 event 4598 (root hooks matched case-sensitively, where Python's `Path.glob` ignores case on Windows).
Seen 2×

**Before handing over, check the PR body against each handoff condition of the owner ruling, one by one, and carry over the implementer's result for each.**
- The reviewer and the owner read the body, not the card's report.
- 2026-10-17: PR #140 edit history (the results issue #119 comment 6230883992 asks for were added at 08:41:57Z, after the third review began).
Seen 1×

**Before you write a PR or issue body back from a file, check that the command that produced it succeeded and that the file is not empty; run every `gh` command with `-R gn00678465/apm-go`.**
- Outside a git checkout `gh pr view <N>` fails and leaves an empty file, and writing it back blanks the body.
- 2026-10-17: PR #140 edit history (body empty at 08:41:09Z, restored at 08:41:26Z).
Seen 1×

**When a PR closes an issue that lists items, account for every item where `gh` can read it: tick each item that is done, and write the conclusion and evidence for each item closed without a code change in the PR body or an issue comment.**
- A conclusion kept only in a card file cannot be read on GitHub, so a forgotten item looks like a checked one.
- 2026-10-17: issue #127 (closed by PR #142 with all 10 boxes open).
- 2026-10-20: issue #151 (closed by PR #153 with all 6 boxes open).
Seen 2×

**When a change alters what install writes, adopts or reuses, run `install --frozen` on each state the change leaves, with the network off as well, and compare it with main before handing over.**
- A frozen install writes no lock, checks every deployed hash and reuses `apm_modules/`, so a state an ordinary install handles can fail it.
- 2026-10-17: board 144 event 3983 (a skipped file the user then made equal to the source fails `install --frozen`).
- 2026-10-20: board 123 event 5157 (the frozen install's deploy in place ignored an MCP registry failure).
Seen 2×

**When the brief or the implementation reads an owner ruling in a way its words do not say, stop with `board ask` before the PR opens; do not leave the reading under To confirm for the reviewer.**
- The reviewer checks the PR against the ruling's words, so the round fails even when the owner then accepts the reading.
- 2026-10-17: board 144 event 4001 (review failed on the words of issue #144 comment 6234185763).
- 2026-10-20: board 126 event 4578 (orchestrator reject: the implementation replaced issue #126 comment 6270876048 item 1 with a simpler rule).
Seen 2×

**Rebase onto `origin/main` and resolve every conflict before you open a PR; do not open a PR that GitHub reports as conflicting.**
- GitHub runs no `pull_request` workflow for a PR that does not merge cleanly.
- 2026-10-17: PR #147 (its first `pull_request` run is on `13127e6`, after the rebase).
Seen 1×

**Before the first stop on rule 1, run `impact` on every symbol the brief names and every function the change will edit, helpers that only pass a new argument down included, and put them all in one stop.**
- Each stop waits for the acting owner, and a second ruling on symbols the first stop could have listed decides nothing new.
- 2026-10-19: board 126 events 4491 and 4497 (two stops on the same 11 symbols).
- 2026-10-20: board 151 events 4686 and 4779 (8, then 13 more symbols, after the first stop on 10).
Seen 2×

**When a value from `apm.yml` becomes a path or a directory name, validate it the same way wherever else it is read, `apm.lock.yaml` included, and test a changed lock.**
- A lock comes from version control, so a check on `apm.yml` alone leaves the value unchecked for a frozen install, `update` and `uninstall`.
- 2026-10-20: board 151 event 4788 (an `alias` in the lock made a frozen install change another package's checkout).
Seen 1×

## Existing rules missed

A rule already in AGENTS.md or the project's instructions that was broken again. These are not lessons and are not promoted; each entry is evidence that the rule is not being checked.

**AGENTS.md "Testing patterns": expected values come from the Oracle's source or output, never recomputed the way the code does.**
- 2026-10-16: `b901778` (PR #96, a test that asserts the bug's output; corrected in `33b05f1`).
Seen 1×

**Project rule: never run `rm` on a path built from a shell variable or a glob; delete with a literal absolute path, or write each rerun into a new directory.**
- The command stops at a permission prompt that no one answers, and the card waits.
- 2026-10-16: board 97 event 3060.
- 2026-10-17: board 122 event 3778.
- 2026-10-20: board 151 event 4708.
Seen 3×

**AGENTS.md "Canonical documents": for each lesson that applies, say in the PR description what you did.**
- 2026-10-17: PR #145 (the account was in comment 6233259713, not the body; board 122 event 3821).
Seen 1×

**AGENTS.md "Promoted rules" 1: run every check this machine can run before you list it under "Not verified".**
- 2026-10-20: board 156 event 5530 (PR #164 left the legacy `name:` entry, runnable here, under Not verified).
Seen 1×

**AGENTS.md "Promoted rules" 3: write what the Oracle does only after you run Oracle 0.33.0 or cite the Oracle's file:line.**
- 2026-10-17: board 122 event 3801 (a `Resolving` line claimed for every local package).
- 2026-10-20: issue #151 comment 6280837480 (the claim that the registry form checks no extra fields).
Seen 2×

**AGENTS.md "Lessons review" step 5: check every reference with `git`, `gh` or `board log`.**
- 2026-10-17: `5834106` (cited a local session transcript; replaced in `882d6e8`).
Seen 1×
