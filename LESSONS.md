<!-- reviewed-through: ad865d7 (2026-10-17 14:35 +0800) -->

# Lessons

This file is the review log of merged apm-go work. A review reads what landed and records work that had to be fixed again, work that should not have merged, verification that was too thin, and work done right that is worth repeating.

Each lesson is a rule, then why, then evidence that `git`, `gh` or `board log <issue> --project apm-go` can check, then `Seen N×` with the dates. Seen counts separate days (UTC+8).

Rules and lessons differ in status:

- The rules in AGENTS.md "Promoted rules" are mandatory. A PR that breaks one is not ready.
- A lesson here is a candidate rule. Breaking one does not block a PR, but no lesson is optional: read the lessons before changing code, follow each one that applies to your change, and say in the PR description what you did for it.
- A lesson seen on 3 separate days is promoted. The rule goes into AGENTS.md "Promoted rules"; the entry and its evidence move to "Moved to AGENTS.md" below and stay there as the record.

How to run a review is in AGENTS.md "Lessons review". Findings about the board tooling itself are not entered here.

## Review of 2026-10-17 12:37 to 2026-10-17 14:35 (+0800)

Range `4fb095a..ad865d7`: 2 merged PRs (#137, #138), 5 commits (3 non-merge, 2 merges), 11 files changed. No tag. PR #137 is the previous review and had no card. PR #138 came from card 118: one stop on rule 1 for the CRITICAL blast radius of `checkFileDest` (board 118 event 3586), ruled in issue #118 comment 6229823230; one failed review (event 3625, on `230e9df`), fixed in `27f95f3`; a pass on the second review (event 3644).

What went wrong: the first round handed over a frozen-install failure that the ruling's handoff conditions forbid. It was written into PRODUCT.md and the PR's To confirm section as a known limitation (new lesson below). The fix round added a case to the PR's Behavior section and not to the PR's line in issue #133 (new lesson below). No merged change was found broken.

Done right:

- Promoted rule 2: PR #138 was opened at 05:20:12Z and its line was added to issue #133 at 05:20:29Z (the issue's edit history).
- Promoted rule 1: the one Not verified item of PR #138 is a macOS run, and this machine has no macOS host or VM.
- The Windows lesson: PR #138 ran its Windows scenarios under a restricted token and as administrator (PR #138 Verified), because the bug exists only for an account that cannot make a symlink.

## Review of 2026-10-17 11:49 to 2026-10-17 12:37 (+0800)

Range `456df11..4fb095a`: 2 merged PRs (#135, #136), 5 commits (3 non-merge, 2 merges), 7 files changed. No tag. PR #135 is the previous review and had no card. PR #136 came from card 116: one stop on rule 1 for the Windows effect of `godebug winsymlink=0` on every symlink check (board 116 event 3546), ruled in issue #116 comment 6229460753; one question about a deviation that ruling did not name (event 3558), ruled in comment 6229603360; a pass on the first review (event 3579).

What went wrong: nothing in this range had to be fixed again, and no merged change was found broken.

Done right:

- The deviation lesson was followed. The Windows runs found a third case where apm-go is stricter than Oracle 0.33.0 (`compile` on a junction in a dependency that leaves the package). The implementer stopped with `board ask` (event 3558), and `cef28e5` wrote it into PRODUCT.md only after comment 6229603360 ruled it.
- Promoted rule 2: PR #136 was opened at 04:29:30Z and its line was added to issue #133 at 04:29:42Z (the issue's edit history).
- Promoted rule 1: PR #136 has no Not verified section. The Windows, Oracle 0.33.0 and lock-upgrade runs are under Verified.
- The Windows lesson: PR #136 ran 25 scenarios in the Windows sandbox, each with a junction and with a symlink, on main, the branch and Oracle 0.33.0, and reproduced the deletions on main. The reviewer ran them again on a new build (board 116 events 3575-3578).

## Review of 2026-10-17 07:08 to 2026-10-17 11:49 (+0800)

Range `0cf2bd2..456df11`: 2 merged PRs (#132, #134), 9 commits (7 non-merge, 2 merges), 11 files changed. No tag. PR #132 (the lessons loop) had no card. PR #134 came from card 115: one stop on rule 1 for the CRITICAL blast radius of `resolveCloneURL` and `listTags` (board 115 event 3502), ruled in issue #115 comment 6228958193, then a pass on the first review (event 3536).

What went wrong: nothing in this range had to be fixed again, and no merged change was found broken. Issue #133, opened in this range, misses five upgrade-visible changes merged since `v0.4.0` (PR #85, #91, #92, #93, #94), three of which the previous review lists under "Moved to AGENTS.md".

Done right:

- Promoted rule 2 was followed from the first PR after it. Issue #133 (`Release notes: v0.4.1`) was opened 73 s after PR #132 merged, with the items of 11 earlier PRs filled in. PR #134 was opened at 03:42:06Z and its line was added to issue #133 at 03:42:23Z (the issue's edit history).
- Promoted rule 1: PR #134 lists one Not verified item, the `:80` normalization over a real http connection, and its Verified section names the command that fails (`--allow-insecure`: `transport 'http' not allowed`, issue #124).
- PR #134 ran the lock-upgrade lesson and named it, compared the no-port output, lock and file tree of the main and PR binaries byte for byte, and installed a dependency with a port in the Windows sandbox. The reviewer ran the old-lock and no-port comparisons again (board 115 event 3534).

## Review of 2026-10-15 14:24 to 2026-10-17 07:08 (+0800)

Range `34dfa71..0cf2bd2`: 24 merged PRs (#84, #85, #90-#96, #98-#103, #105, #106, #108-#111, #117, #120, #121), 75 commits (51 non-merge, 24 merges), 326 files changed. No tag: `v0.4.0` is on `34dfa71`, the start of the range. Every PR came from a board card and was merged under the owner's authorization. 16 of the 24 cards had at least one failed review (30 failed verdicts out of 52), 18 stopped on stop rule 1 for an acting-owner ruling, 6 needed an intervention, and 2 timed out (#80, #114).

What went wrong:

- Not verified sections listed checks this machine could run: `normalize`/`validate` on `../dep` (PR #84), a local registry (#69 review), an offline marketplace (#70), Windows after the sandbox existed (PR #117), and a registry `--global` upgrade (PR #120). The last two were run separately and posted on the PR before the merge.
- One PR wrote a deviation into PRODUCT.md with no owner ruling: writing through a user's in-root symlink (`9968ee5`, PR #92). The next day #112 reversed it (`cf8aa7a`, PR #117).
- Merged code broke on Windows. `joinRealpath` (`22e71c6`, PR #90) and `oraclePathJoin` (`0636bf8`, PR #105) split paths only on `/`. The containment checks of PR #91/#92 do not see junctions (#116). All three were listed under Not verified, and the first Windows sandbox run found them (#88 comment 6216218590).
- A test from PR #91 (`c73e914`) was flaky because git repacked in the background. The gate of the next card caught it, and `4cda93d` fixed it.
- A test from PR #96 (`b901778`) asserted the bug's output as the expected value. PR #109 (`33b05f1`) replaced it with the Oracle's output.
- Some fixes were not applied to the whole class. Card #86 needed three rounds, card #70 three, and card #112 three, until a `use_skill` intervention. Upgrades from old or Oracle-written locks broke in three cards (#75, #113, #114). Reviewers caught each of these before merge.
- Some statements about the Oracle or PRODUCT.md were wrong. The issue bodies of #67, #89 and #112 were corrected by the card investigations, and a claim about registry `.git` was corrected by the #113 review.
- Upgrade-visible changes were written only in each PR's Risks section. #88 holds the release-note item of PR #101 alone, and no other place collects them.
- An agent ran `rm` on a shell-variable path again (#104, #97), against an existing project rule.

Done right: the Windows and registry checks were run before the merge and posted on PR #117 and PR #120, and PR #121 shipped with Linux and Windows Oracle matrices and no Not verified section. Each card investigation checked its issue against the Oracle before any code was written.

## Lessons

**When a reviewer names one path that breaks a rule, list every path that rests on the same premise, fix them all in the same round, and write the list in the round's report.**
- A fix that covers only the case the reviewer named lets the next review find the next case. Each card below needed three rounds, and #70's intervention says its rules "only covered the case in front of it".
- Evidence: board 64 events 1884, 1907 (self-copy check: final destination, then source through the destination link); board 86 events 2141, 2165, 2201 (cleanup outside root, then write through a final symlink, then hand-over through a skill symlink); board 70 events 2412, 2437, 2460, intervene 2463; board 112 events 3281, 3304, 3328, intervene 3331 ("every failure came from the same premise: the owner of a path is the bucket that deploys it this run").
- Done right on 2026-10-16: after the `use_skill` intervention, board 112 event 3351 passed on the first round. Done right on 2026-10-17: card 116 listed every check built on resolving symlinks before the first round (board 116 event 3546), fixed them all with one `go.mod` setting, named them in ARCHITECTURE.md "Windows links" (`cef28e5`), and passed on the first review (event 3579).
Seen 2× (2026-10-15, 2026-10-16)

**Write what the Oracle does only after you run Oracle 0.33.0 or cite the Oracle's file:line, and write what PRODUCT.md says only with the item quoted.**
- Issue bodies, comments and PRODUCT.md lines that stated the Oracle's behavior from memory were wrong, and each one sent a card or a reviewer to disprove it.
- Evidence: issue #89 body on what the Oracle deploys, corrected in issue #89 comment 6187796986; issue #67 exit code, corrected in issue #67 comment 6186847177; issue #112 body "PRODUCT.md has no rule for rule files", corrected in issue #112 comment 6215906800 and issue #88 comment 6215726615; `00b5975` comment and PRODUCT.md "Oracle removes registry `.git`", refuted by board 113 event 3391 and fixed in `300568e`; `1b84146` "as the Oracle prints them" (2026-10-14), corrected by `eb3de05`.
- Done right on 2026-10-15: the #67 and #89 investigations measured the Oracle and corrected the issue before any code was written. Done right on 2026-10-17: `ea217bf` cites the Oracle's `utils/github_host.py:437-532` and `reference.py:593-595,1690-1694`, and PR #134 Verified compares the lock fields of the https port cases with an Oracle 0.33.0 run; the PRODUCT.md item of `cef28e5` cites the Oracle's `utils/path_security.py:222-245`, `integration/mcp_integrator.py:78` and `compilation/inventory.py:61`, and board 116 event 3558 reports Oracle 0.33.0 run on Windows with a junction and with a symlink.
Seen 2× (2026-10-15, 2026-10-16)

**Run a change that splits, joins or resolves paths, or that checks symlinks, in the Windows sandbox before merge, with a junction as well as a symlink.**
- CI runs only `ubuntu-latest`. Code that splits paths on `/`, or that relies on `os.Lstat` and `filepath.EvalSymlinks` to see links, passes on Linux and fails on Windows. Go reports a junction as `ModeIrregular`.
- Evidence: `22e71c6` (PR #90, `joinRealpath`) and `0636bf8` (PR #105, `oraclePathJoin`) fail on `\` paths, found in issue #88 comment 6216218590 and now in issue #127; PR #91/#92 containment checks bypassed by junctions, issue #116 (opened 2026-10-16, about 23 h after PR #92 merged).
- Done right on 2026-10-16: PR #117 comment 6218591422. Done right on 2026-10-17: PR #121 (Windows sandbox runs listed under Verified); PR #136 Verified (25 scenarios, each with a junction and a symlink, on main, the branch and Oracle 0.33.0), run again by the reviewer in board 116 events 3575-3578.
Seen 2× (2026-10-15, 2026-10-16)

**For any change that reads the lock, cleans stale or orphan files, or renames module keys, run install, update, frozen install and uninstall on a lock written by the previous release and on one written by Oracle 0.33.0 before handing over.**
- Each of these three cards passed its own tests and broke only on an upgrade path, which the reviewer found by running the old binary first.
- Evidence: board 75 event 2692 (`150bef8`: removed packages from legacy locks leave files); board 113 event 3391 (`00b5975`: upgrading an Oracle lock deletes a registry package's `.git/config`); board 114 event 3462 (`ce7a1b2`: old-clone cleanup deletes the new package; uninstall rewrites surviving keys), fixed in `ca1cf45`.
- Done right on 2026-10-17: PR #120 comment 6219879113 (upgrade run with the main and PR binaries on a main-written and an Oracle-written lock); PR #134 Verified (install, update and frozen install on a lock with `port: 443` written by the previous release and on an Oracle-written lock with a port; uninstall was not run), run again by the reviewer in board 115 event 3534; PR #136 Verified (install, update, frozen install and uninstall in the Windows sandbox on locks written by main and by Oracle 0.33.0; main, not `v0.4.0`, wrote the old lock).
Seen 2× (2026-10-16, 2026-10-17)

**Do not write a new deviation from the Oracle into PRODUCT.md unless an owner ruling names it; when the fix needs one, stop with `board ask`.**
- PRODUCT.md is the contract that later cards follow. An unruled deviation there is read as a ruling, and reversing it the next day cost a full card.
- Evidence: `9968ee5` (PR #92) added "a deployed file that is a symlink to another place in its target root … is written where it points"; issue #112 comment 6215906800 ("#86 has no owner ruling on this point"); reversed by `cf8aa7a` (PR #117).
- Done right on 2026-10-17: board 116 event 3558 (a deviation the ruling did not name, asked before it was written), ruled in issue #116 comment 6229603360, then written by `cef28e5`.
Seen 1× (2026-10-15)

**Before committing new test fixtures, run `git status --short --ignored` and confirm that none of them is ignored (`*.out`, `apm_modules/`).**
- An ignored fixture is not committed, so the test passes locally and fails or tests nothing in CI.
- Evidence: board 82 event 2920 (orchestrator reject: 10 `testdata/deps/*.out` files and four parity `home_fixture/.apm/apm_modules/` trees ignored).
Seen 1× (2026-10-16)

**When you open or fill a release-notes issue, list every PR merged since the previous tag (`git log --first-parent <tag>..origin/main`) and check each one's Behavior and Risks for an upgrade-visible change; do not fill it from memory or from one list.**
- Release notes are written from that issue, so a change missing there is missing from the release notes.
- Evidence: issue #133, opened on 2026-10-17, holds the items of PR #100, #101, #103, #105, #106, #108-#110, #117, #120 and #121. It has no item for PR #85 (the next `install` drops `id` from `.codex/config.toml`), PR #91 (a package with a symlink that leaves it no longer installs), PR #92 (`uninstall` fails and `install` skips paths when a directory or file below `apm_modules/` or `.claude/` is linked outside), PR #93 (`enabled` written to `opencode.json` and the lock) or PR #94 (a `targets:` list removes the package's files on other targets). All five merged after `v0.4.0` (`34dfa71`); PR #91, #92 and #94 are listed in this file under "Moved to AGENTS.md". The five items, and those of PR #84 and #111, were added to issue #133 after this review.
Seen 1× (2026-10-17)

**Check each limitation you find against the handoff conditions of the owner ruling before you hand over. When it breaks one, fix it in the same round: do not write it into PRODUCT.md or the PR as a known limitation, and do not put it to the owner as a question.**
- The handoff condition has already decided the question, so the reviewer fails the round and the fix costs a full review cycle. Here the limitation came from a fallback that records other lock paths than the primary way: a lock moves between accounts and machines, so a frozen install with one way fails on a lock the other way wrote.
- Evidence: issue #118 comment 6229823230 requires `install -g --frozen` to succeed under a restricted token. `230e9df` wrote into PRODUCT.md "A symlink inside a local skill is not deployed and not recorded in the lock, where a symlink deploy records it."; the first body of PR #138 (edit history, 05:20:12Z) listed the resulting frozen failure under To confirm; the first version of issue #118 comment 6230006003 (05:11:03Z) asked the owner whether to accept it. The reviewer reproduced it in the Windows sandbox and failed the round (board 118 event 3625, on `230e9df`). `27f95f3` makes the copy deploy record the same paths and hashes as a symlink deploy, and the next review passed (event 3644).
Seen 1× (2026-10-17)

**When a fix round changes what a PR's Behavior section says, update the PR's line in the release-notes issue in the same step.**
- Release notes are written from that issue (promoted rule 2), so a case added only to the PR body is missing from them.
- Evidence: PR #138's body edit at 06:31:25Z (edit history) adds "或 skill 裡有 symlink 時" to the case where a skill directory is kept with a warning. Issue #133 was last edited at 05:20:29Z, and its PR #138 line names only "目錄裡有自己加的檔案或套件改過時".
Seen 1× (2026-10-17)

## Existing rules missed

A rule that already exists in AGENTS.md or the project's instructions and was broken again. These are not new lessons; each one is evidence that the rule is not being checked.

**AGENTS.md "Testing patterns": expected values come from the Oracle's source or output, never recomputed the way the code does.**
- A test that asserts current output protects the bug. The next card had to change it before its fix could pass.
- Evidence: `b901778` (PR #96) `TestInstall_StaleMCP_ServerMovedToDevDependenciesIsKept`; issue #97 comment 6211906602; corrected in `33b05f1` (PR #109).
Seen 1× (2026-10-16)

**Project rule: never run `rm` on a path built from a shell variable; delete with a literal absolute path, or write each rerun into a new directory.**
- The command stops at a permission prompt that no one answers, and the whole card waits for the user. The rule existed before this round (PR #63 for scripts, and the card notes).
- Evidence: board 104 event 3023 and board 97 event 3060 (`agent_blocked`); board 97 event 3069 (owner note naming the cause).
Seen 1× (2026-10-16)

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
- Release notes are written from one list. A change that is written only in a PR body is easy to miss. Of the PRs below, only PR #101's item reached #88.
- Evidence: Risks with upgrade-visible changes and no tracking entry: PR #91, PR #92 (2026-10-15); PR #94, #100, #105, #106, #109, #110, #117 (2026-10-16); PR #120, #121 (2026-10-17). The only one in a tracking issue: issue #88 comment 6203884235 (PR #101). No open issue collects 0.4.1 release notes (`gh issue list --search "0.4.1"`).
- Done right on 2026-10-16: issue #88 comment 6203884235.
Seen 3× (2026-10-15, 2026-10-16, 2026-10-17)

## Observed, not entered

- The instruction files have outgrown Claude Code's limit. ARCHITECTURE.md grew from 69,071 to 121,598 characters and PRODUCT.md from 21,890 to 76,231 in this range (`git show 34dfa71:ARCHITECTURE.md | wc -m` against `0cf2bd2`). Claude Code warned "over the 150.0k-char total limit" (163.5k on 2026-10-16, 216.7k on 2026-10-17). Its effect on the agents is not measured.
- GitNexus does not find Go top-level functions, so the AGENTS.md `impact` gate cannot run on them. On the index of `4fb095a`, `node .gitnexus/run.cjs impact CheckWithin --direction upstream` and `context ParseManifest` return "not found", and `context materializeLocalCopy`, a method, is found. Board 116 event 3546 reports 38 of 5535 Function nodes named; card 116 gave the blast radius of the methods it could find. Card 118 found that a full rebuild fixes it: after `node .gitnexus/run.cjs analyze --index-only --force` the top-level functions are found, where 5497 of 5546 Function nodes had no name before (board 118 event 3586). Tracked in issue #130 comments 6229756641 and 6229823442.
