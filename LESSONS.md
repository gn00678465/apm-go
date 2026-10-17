<!-- reviewed-through: 0cf2bd2 (2026-10-17 07:08 +0800) -->

# Lessons

apm-go's code is investigated, written, reviewed and merged by board agents, with an owner (or an acting owner) ruling on stop rules in issue comments. This file is the review log of that work: after merges, an agent reads what landed and records what had to be fixed again, what should not have merged, what was verified too thinly, and what was done right. Each lesson is a rule, then why, then evidence that `git`, `gh` or `board log <issue> --project apm-go` can check, then `Seen N×` with the dates. A lesson seen on 3 separate days moves into AGENTS.md "Promoted rules" and is removed from here. How to run a review is in AGENTS.md "Lessons review"; this file holds evidence, not rules.

Read the lessons before changing code; a lesson that applies to your change is a check you run before handing it over.

## Review of 2026-10-15 14:24 to 2026-10-17 07:08 (+0800)

Range `34dfa71..0cf2bd2`: 24 merged PRs (#84, #85, #90-#96, #98-#103, #105, #106, #108-#111, #117, #120, #121), 75 commits (51 non-merge, 24 merges), 326 files changed. No tag: `v0.4.0` is on `34dfa71`, the start of the range. Every PR came from a board card. 16 of the 24 cards had at least one failed review (30 failed verdicts out of 52), 18 stopped on stop rule 1 for an acting-owner ruling, 6 needed an intervention, and 2 timed out (#80, #114).

What went wrong:

- Not verified sections listed checks this machine could run: `normalize`/`validate` on `../dep` (PR #84), a local registry (#69 review), an offline marketplace (#70), Windows after the sandbox existed (PR #117), and a registry `--global` upgrade (PR #120). Both of the last two were run separately and posted on the PR minutes before the merge.
- One PR wrote a deviation into PRODUCT.md with no owner ruling: writing through a user's in-root symlink (`9968ee5`, PR #92). The next day #112 reversed it (`cf8aa7a`, PR #117).
- Merged code broke on Windows. `joinRealpath` (`22e71c6`, PR #90) and `oraclePathJoin` (`0636bf8`, PR #105) split paths only on `/`. The containment checks of PR #91/#92 do not see junctions (#116). All three were listed under Not verified, and the first Windows sandbox run found them (#88 comment 6216218590).
- A test from PR #91 (`c73e914`) was flaky because git repacked in the background. The gate of the next card caught it, and `4cda93d` fixed it.
- A test from PR #96 (`b901778`) asserted the bug's output as the expected value. PR #109 (`33b05f1`) replaced it with the Oracle's output.
- Some fixes were not applied to the whole class. Card #86 needed three rounds, card #70 three, and card #112 three, until a `use_skill` intervention. Upgrades from old or Oracle-written locks broke in three cards (#75, #113, #114). Reviewers caught each of these before merge.
- Some statements about the Oracle or PRODUCT.md were wrong. The issue bodies of #67, #89 and #112 were corrected by the card investigations, and a claim about registry `.git` was corrected by the #113 review.
- Upgrade-visible changes were written only in each PR's Risks section. #88 holds the release-note item of PR #101 alone, and no other place collects them.
- Process: agents removed shell-variable paths and stopped at a permission prompt (#104, #97). Board printed `queued` for #113 but ignored the card while #112 was open. A verification script hung for 284 minutes (#80). #88 grew to 151 open items before a batch ruling closed it (102 of them moved to issues #122-#131).

Done right: the Windows and registry checks were run before the merge and posted on PR #117 and PR #120, and PR #121 shipped with Linux and Windows Oracle matrices and no Not verified section. Each card investigation checked its issue against the Oracle before any code was written. Stop-rule reports carried GitNexus numbers and an output path, and each owner ruling set a closed scope in an issue comment.

## Lessons

**When a reviewer names one path that breaks a rule, list every path that rests on the same premise, fix them all in the same round, and write the list in the round's report.**
- A fix that covers only the case the reviewer named lets the next review find the next case. Each card below needed three rounds, and #70's intervention says its rules "only covered the case in front of it".
- Evidence: board 64 events 1884, 1907 (self-copy check: final destination, then source through the destination link); board 86 events 2141, 2165, 2201 (cleanup outside root, then write through a final symlink, then hand-over through a skill symlink); board 70 events 2412, 2437, 2460, intervene 2463; board 112 events 3281, 3304, 3328, intervene 3331 ("every failure came from the same premise: the owner of a path is the bucket that deploys it this run").
- Done right on 2026-10-16: after the `use_skill` intervention, board 112 event 3351 passed on the first round.
Seen 2× (2026-10-15, 2026-10-16)

**Write what the Oracle does only after you run Oracle 0.33.0 or cite the Oracle's file:line, and write what PRODUCT.md says only with the item quoted.**
- Issue bodies, comments and PRODUCT.md lines that stated the Oracle's behavior from memory were wrong, and each one sent a card or a reviewer to disprove it.
- Evidence: issue #89 body on what the Oracle deploys, corrected in issue #89 comment 6187796986; issue #67 exit code, corrected in issue #67 comment 6186847177; issue #112 body "PRODUCT.md has no rule for rule files", corrected in issue #112 comment 6215906800 and issue #88 comment 6215726615; `00b5975` comment and PRODUCT.md "Oracle removes registry `.git`", refuted by board 113 event 3391 and fixed in `300568e`; `1b84146` "as the Oracle prints them" (2026-10-14), corrected by `eb3de05`.
- Done right on 2026-10-15: the #67 and #89 investigations measured the Oracle and corrected the issue before any code was written.
Seen 2× (2026-10-15, 2026-10-16)

**Run a change that splits, joins or resolves paths, or that checks symlinks, in the Windows sandbox before merge, with a junction as well as a symlink.**
- CI runs only `ubuntu-latest`. Code that splits paths on `/`, or that relies on `os.Lstat` and `filepath.EvalSymlinks` to see links, passes on Linux and fails on Windows. Go reports a junction as `ModeIrregular`.
- Evidence: `22e71c6` (PR #90, `joinRealpath`) and `0636bf8` (PR #105, `oraclePathJoin`) fail on `\` paths, found in issue #88 comment 6216218590 and now in issue #127; PR #91/#92 containment checks bypassed by junctions, issue #116 (opened 2026-10-16, about 23 h after PR #92 merged).
- Done right on 2026-10-16: PR #117 comment 6218591422. Done right on 2026-10-17: PR #121 (Windows sandbox runs listed under Verified).
Seen 2× (2026-10-15, 2026-10-16)

**For any change that reads the lock, cleans stale or orphan files, or renames module keys, run install, update, frozen install and uninstall on a lock written by the previous release and on one written by Oracle 0.33.0 before handing over.**
- Each of these three cards passed its own tests and broke only on an upgrade path, which the reviewer found by running the old binary first.
- Evidence: board 75 event 2692 (`150bef8`: removed packages from legacy locks leave files); board 113 event 3391 (`00b5975`: upgrading an Oracle lock deletes a registry package's `.git/config`); board 114 event 3462 (`ce7a1b2`: old-clone cleanup deletes the new package; uninstall rewrites surviving keys), fixed in `ca1cf45`.
- Done right on 2026-10-17: PR #120 comment 6219879113 (upgrade run with the main and PR binaries on a main-written and an Oracle-written lock).
Seen 2× (2026-10-16, 2026-10-17)

**Before a stop check cites GitNexus, compare the index's `lastCommit` with `origin/main` and run `node .gitnexus/run.cjs analyze --index-only` when they differ.**
- Impact counts from an index 58 commits behind miss callers added in this round, and a stop decision is made on those numbers.
- Evidence: board 68 event 2235 and board 69 event 2280 (index `b73b894` while `internal/deploy` had changed in PR #91/#92); board 97 event 3063, board 107 event 3101, board 87 event 3194 (each says "stale"); board 112 event 3238 ("58 commits behind"); issue #112 comment 6215906800 (owner rebuilt the index at `e0716ff`).
- Done right on 2026-10-16: board 104 event 3008, board 113 event 3358. Done right on 2026-10-17: board 114 event 3419, board 115 event 3502 (index refreshed, output file named).
Seen 2× (2026-10-15, 2026-10-16)

**Do not write a new deviation from the Oracle into PRODUCT.md unless an owner ruling names it; when the fix needs one, stop with `board ask`.**
- PRODUCT.md is the contract that later cards follow. An unruled deviation there is read as a ruling, and reversing it the next day cost a full card.
- Evidence: `9968ee5` (PR #92) added "a deployed file that is a symlink to another place in its target root … is written where it points"; issue #112 comment 6215906800 ("#86 has no owner ruling on this point"); reversed by `cf8aa7a` (PR #117).
Seen 1× (2026-10-15)

**Create every git repository in a test through the `gitCmd` helper, which runs git with `-c maintenance.auto=false`, so a background repack cannot change a fixture during a snapshot comparison.**
- git 2.55 repacks in the background after a commit. A test that compares a fixture repository before and after a command then fails at random.
- Evidence: `c73e914` (PR #91) added `TestInstall_DependencyFileSymlinkOutsideFails`; board 69 event 2313 (gate failed on it); fixed in `4cda93d` (PR #94).
Seen 1× (2026-10-15)

**Take a test's expected stdout and lock from an Oracle 0.33.0 run of the same steps; never paste apm-go's current output in as the expected value.**
- A test that asserts current output protects the bug. The next card had to change it before its fix could pass.
- Evidence: `b901778` (PR #96) `TestInstall_StaleMCP_ServerMovedToDevDependenciesIsKept`; issue #97 comment 6211906602; corrected in `33b05f1` (PR #109).
Seen 1× (2026-10-16)

**Never run `rm` on a path built from a shell variable; write each rerun into a new directory, or delete with a literal absolute path.**
- The command stops at a permission prompt that no one answers, and the whole card waits for the user. The rule existed before this round (PR #63 for scripts, and the card notes).
- Evidence: board 104 event 3023 and board 97 event 3060 (`agent_blocked`); board 97 event 3069 (owner note naming the cause).
Seen 1× (2026-10-16)

**When a reviewer finding falls outside the card's ruled scope, ask for a scope ruling after the first round that reports it, not after the third.**
- Each resubmission without a ruling costs a review round and moves the card toward its stuck limit.
- Evidence: board 107 events 3134, 3156, 3178 (CRLF reported in rounds 11, 19 and 27), intervene 3181; issue #107 comment 6213403633 (deferred; now issue #119).
Seen 1× (2026-10-16)

**Once the owner has ruled on a stop rule for the card, check each later impact result against that ruling's closed scope and report `holds=false` with the comment URL while the symbols stay inside it.**
- A second stop on the same rule asks the owner the same question again.
- Evidence: board 86 events 2101, 2107 (stopped twice on rule 1), event 2113 (owner note added to stop the repeat).
- Done right on 2026-10-16: board 97 event 3070. Done right on 2026-10-17: board 114 event 3425.
Seen 1× (2026-10-15)

**Before committing new test fixtures, run `git status --short --ignored` and confirm that none of them is ignored (`*.out`, `apm_modules/`).**
- An ignored fixture is not committed, so the test passes locally and fails or tests nothing in CI.
- Evidence: board 82 event 2920 (orchestrator reject: 10 `testdata/deps/*.out` files and four parity `home_fixture/.apm/apm_modules/` trees ignored).
Seen 1× (2026-10-16)

**Run each verification script under `timeout` (for example `timeout 900 sh run.sh`) and send `board report checkpoint` before each step that can take more than a few minutes.**
- A hung script keeps the agent's turn busy until the board timer fires. Here that took 4 h 45 min.
- Evidence: board 80 event 2797 (orchestrator timed out in investigation, 4 h 45 min after claim).
- Done right on 2026-10-17: after the brief rewrite that added time limits (board 114 event 3434), the implementer finished in 22 minutes with 10 checkpoints (events 3436-3446).
Seen 1× (2026-10-16)

**After `board card start` while another card of the project is open, run `board log <n>`; if `card_found` shows `ignored: the project already has an open card`, start the card again when the open one closes.**
- The CLI prints `queued`, but the service drops the event. The card starts only when someone starts it again.
- Evidence: board 113 event 3236 (ignored at 2026-10-16 20:42), event 3356 (started again at 23:18 by the user).
Seen 1× (2026-10-16)

**Record a finding that is out of the card's scope on the issue that owns that behavior, and only when it needs an owner decision; do not append it to a catch-all issue.**
- A list that only grows needs a batch ruling. #88 reached 151 open items in two days; the ruling closed 40 directly (31 of them accepted differences) and split the other 102 into ten issues.
- Evidence: issue #88 comment 6226395157 (batch ruling; items split into issues #122-#131; new rule stated).
Seen 1× (2026-10-17)

**Rewrite the PR description for the new head before you push the head.**
- When a turn stops between the push and the description update, the PR shows green checks with a description of the old head.
- Evidence: board 114 event 3484 (`ca1cf45` pushed, PR #121 description still at `ce7a1b2`, mergeStateStatus CLEAN), event 3486 (user asked for the rewrite).
Seen 1× (2026-10-17)

## Moved to AGENTS.md

**Run every check this machine can run before you list it under "Not verified"; the Windows sandbox, the local fake registry, a local-directory marketplace and the installed `codex` CLI all count.**
- Moved to AGENTS.md "Promoted rules" on 2026-10-17; kept here as the evidence.
- An item left in Not verified moves the check to the merge step or to the next release. Here it was run separately before merge on two PRs, reviewers failed three rounds over it, and three of the Windows items left unverified turned out to be bugs.
- Before writing "cannot be verified here", try it once and write the command that failed. Two such claims were wrong and were corrected the same night.
- Evidence: PR #84 Not verified lists `normalize` and `validate` on `../dep` entries (board 64 event 1923); PR #85 Not verified "did not have Codex connect to the MCP server" while `codex` was installed and used as the review harness (board 68 event 2271); board 69 events 2335, 2355 (review fail: registry parity "testable locally"); issue #88 comment 6197554849 (item 34 "no registry server" was wrong); board 70 events 2460, 2463 and issue #88 comment 6199489219 (item 49 "no offline marketplace" was wrong); board 112 event 3259 (orchestrator reject: both Not verified items were doable); PR #117 Not verified "this machine has no Windows" after the sandbox run in issue #88 comment 6216218590; PR #120 Not verified "registry `--global` upgrade only has Go tests" (board 113 event 3407).
- Done right on 2026-10-16: PR #117 comment 6218591422 (Windows sandbox run, posted before merge). Done right on 2026-10-17: PR #120 comment 6219879113; PR #121 Verified section with Linux and Windows Oracle matrices (board 114 events 3442, 3443).
Seen 3× (2026-10-15, 2026-10-16, 2026-10-17)

**When a PR changes what an existing user sees after upgrading, add one line for it to the release-notes issue of the next tag in the same step that opens the PR; the PR's Risks section alone is not a record.**
- Moved to AGENTS.md "Promoted rules" on 2026-10-17; kept here as the evidence.
- Release notes are written from one list. A change that is written only in a PR body is easy to miss. Of the PRs below, only PR #101's item reached #88. The coordinator's statement that they were all in #88 is reported from conversation and is not in git, gh or board.
- Evidence: Risks with upgrade-visible changes and no tracking entry: PR #91, PR #92 (2026-10-15); PR #94, #100, #105, #106, #109, #110, #117 (2026-10-16); PR #120, #121 (2026-10-17). The only one in a tracking issue: issue #88 comment 6203884235 (PR #101). No open issue collects 0.4.1 release notes (`gh issue list --search "0.4.1"`).
- Done right on 2026-10-16: issue #88 comment 6203884235.
Seen 3× (2026-10-15, 2026-10-16, 2026-10-17)

## Observed, not entered (no rule an agent can follow, or cause not found)

- The instruction files have outgrown Claude Code's limit. ARCHITECTURE.md grew from 69,071 to 121,598 characters and PRODUCT.md from 21,890 to 76,231 in this range (`git show 34dfa71:ARCHITECTURE.md | wc -m` against `0cf2bd2`). Claude Code warned "over the 150.0k-char total limit" (163.5k on 2026-10-16, 216.7k on 2026-10-17). Its effect on the agents is not measured.
- Card #114 stopped twice with the same signature. The implementer transcript ends at 00:55:51 and the orchestrator transcript at 03:19:25, each right after a tool result, with no later model turn. The board timer fired at 02:26 and 06:49 (board 114 events 3433, 3482), not after `runMinutes` = 30. Herdr measures idle time as time since the screen changed (`board/src/herdr.rs:359`). Why the timer took 1.5 h and 3.5 h was not established.
- The Codex reviewer was blocked by the OpenAI content filter on symlink probing (board 86 event 2198), and Codex usage limits paused the review on cards #68, #69 and #104 (`agent_limited` events). These are tooling and capacity limits.
- Stop-rule reports for rules 2-4 are recorded as `stale: sent for step 2, the card is at step 3` whenever rule 1 holds (for example board 114 events 3420-3422). This is board behavior, not a finding about apm-go.
