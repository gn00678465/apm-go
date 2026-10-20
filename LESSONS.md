<!-- reviewed-through: 334c0a8 (2026-10-20 10:30 +0800) -->

# Lessons

This file is the review log of merged apm-go work. A review reads what landed and records work that had to be fixed again, work that should not have merged, verification that was too thin, and work done right that is worth repeating.

Each lesson is a rule, then why, then evidence that `git`, `gh` or `board log <issue> --project apm-go` can check, then `Seen N×` with the dates. Seen counts separate days (UTC+8).

Rules and lessons differ in status:

- The rules in AGENTS.md "Promoted rules" are mandatory. A PR that breaks one is not ready.
- A lesson here is a candidate rule. Breaking one does not block a PR, but no lesson is optional: read the lessons before changing code, follow each one that applies to your change, and say in the PR description what you did for it.
- A lesson seen on 3 separate days is promoted. The rule goes into AGENTS.md "Promoted rules"; the entry and its evidence move to "Moved to AGENTS.md" below and stay there as the record.

How to run a review is in AGENTS.md "Lessons review". Findings about the board tooling itself are not entered here.

## Review of 2026-10-17 22:54 to 2026-10-20 10:30 (+0800)

Range `04f7b53..334c0a8`: 3 merged PRs (#149, #152, #153), 8 commits (5 non-merge, 3 merges), 186 files changed. Tags `v0.4.1-rc.1` and `v0.4.1` are on `7984723`, the merge of PR #149. PR #149 is the previous review and had no card. PR #152 came from card 126: two stops on rule 1 for one set of symbols (board 126 events 4491, 4497), ruled in issue #126 comment 6266899072; three more questions from the orchestrator (event 4507), ruled in comment 6267289737; three stops by the implementer (events 4518, 4566, 4610), ruled in comments 6267470396, 6270876048 and 6272218708; one timeout and intervention (events 4563, 4564); two orchestrator rejects (events 4578, 4608); one failed review (event 4598 on `ad49be0`); a pass on the second review (event 4628 on `2b282f4`); a CI parity failure caused by the Oracle's release 0.34.0 (event 4631), ruled in comment 6277600550; a pass on the third review (event 4669 on `89e59d2`). PR #152 says `Refs #126`, and #126 stays open for items 75 and 78 (scope comment 6266809423). PR #153 came from card 151: two stops on rule 1 (board 151 events 4676, 4686), ruled in issue #151 comments 6278256554 and 6278303322; one permission prompt (event 4708); one timeout and intervention (events 4732, 4733); one stop by the implementer (event 4779), ruled in comment 6280583753; one orchestrator reject (event 4788); a pass on the first review (event 4825 on `4f102b5`). Card files cited below are under `~/.config/board/projects/apm-go/cards/issue-<N>/` of the card named.

What went wrong:

- Card 126 wrote a new deviation into PRODUCT.md with no ruling: on Windows, apm-go does not print the Oracle's `Skipped non-agent asset` warning for a root `X.AGENT.MD`. The orchestrator caught it before the commit. This is the third day for that lesson, and it is promoted (below).
- The first review of PR #152 found that a root hook named `hooks/UPPER.JSON` was not deployed on Windows, where the Oracle's `Path.glob` matches it. The implementation kept a case-sensitive suffix check (existing porting lesson, below).
- Card 126's implementation replaced ruling 6270876048 item 1 with a simpler rule and reported that rule as the ruling. The orchestrator rejected the round (existing ruling lesson, below).
- Card 126 stopped on rule 1 four times and card 151 three times, each time after the first for symbols that the first stop could have listed (new lesson below).
- Card 151 validated `alias` in `apm.yml` but not in `apm.lock.yaml`. A changed lock made a frozen install change another package's checkout. The orchestrator caught it before the PR opened (new lesson below).
- Card 151's investigation and brief said that the Oracle does not check the fields of a registry entry. The investigation cited a file and no line, and the claim was wrong (existing rule missed, below).
- Card 151's implementer stopped at a permission prompt for `rm` when it reran its scenarios (existing rule missed, below).
- PR #153 closed issue #151 with all six boxes unticked (existing issue-items lesson, below).

No merged change was found broken.

Done right:

- Promoted rule 2 and the fix-round lesson: each PR's line in issue #150 was added in the step that opened the PR (PR #152 16:20:06Z, issue 16:20:17Z; PR #153 01:00:29Z, issue 01:00:44Z). The fix round of PR #152 changed its Behavior section and its line together (body 16:43:17Z, issue 16:43:26Z), and the line for the OpenCode label came before that commit was pushed (issue 21:26:46Z, body 21:30:15Z) (edit histories).
- Promoted rule 1: the only Not verified item of PR #152 and of PR #153 is macOS, and this machine has no macOS host or VM.
- The Windows, lock-upgrade and frozen lessons: PR #152 Verified lists runs on locks written by main and by Oracle 0.33.0, with the files present, on a clean clone and with the network off, for a project and for `--global`, and Windows runs with a junction and a symlink. PR #153 Verified lists locks written by main, `v0.4.1` and Oracle 0.33.0, and Windows runs of the alias directory with a junction and a symlink.
- The issue-items lesson: issue #126 has every item ticked that PR #152 covers, and items 75 and 78 stay open by scope comment 6266809423.

## Review of 2026-10-17 17:42 to 2026-10-17 22:54 (+0800)

Range `6e2c6fd..04f7b53`: 5 merged PRs (#143, #145-#148), 13 commits (8 non-merge, 5 merges), 93 files changed. No tag. PR #143 is the previous review, and PR #146 is a documentation change by the acting owner. Neither had a card. PR #145 came from card 122: one stop on rule 1 (board 122 event 3781), ruled in issue #122 comment 6232580233; two orchestrator rejects (events 3801, 3868); two failed reviews (event 3821 on `5ecc718`, event 3846 on `d7586bd`); a pass on the third review (event 3923). PR #147 came from card 144: one stop on rule 1 (board 144 event 3949), ruled in issue #144 comment 6234185763; one orchestrator reject (event 3983); one failed review (event 4001 on `22df9d3`); a supplementary ruling (comment 6234603992); a pass on the second review (event 4022). PR #148 came from card 124: one stop on rule 1 (board 124 event 4031), ruled in issue #124 comment 6234962470; one orchestrator reject (event 4049); a pass on the first review (event 4070). PR #148 says `Refs #124`, and #124 stays open for its 0.4.2 items. Card files cited below are under `~/.config/board/projects/apm-go/cards/issue-<N>/` of the card named.

What went wrong:

- Card 122 replaced the Oracle's comparison with rules of its own twice, and each review found the next case where they differ. `3a8f5b8` then ported `filecmp.dircmp` (existing porting lesson, below).
- Card 122 wrote what the Oracle does from inference twice: in PRODUCT.md and ARCHITECTURE.md, and in a code comment. The orchestrator rejected both (existing rule missed, below).
- Card 122's lesson account was in a PR comment, not the PR body, so two reviews failed it (existing rule missed, below).
- An orchestrator turn of card 122 stopped at a permission prompt for `rm` (existing rule missed, below).
- Cards 144 and 124 each broke `install --frozen` in a state that their scenario runs did not reach. The orchestrator caught both before the PR opened (new lesson below).
- Card 144's brief read ruling 6234185763 in a way its words do not say, and PR #147 left that reading under To confirm. The review failed on the ruling's words, and then the owner accepted the reading (new lesson below).
- PR #147 was opened while it conflicted with main, so CI ran no `pull_request` workflow until the rebase (new lesson below).

No merged change was found broken.

Done right:

- Promoted rule 2 and the fix-round lesson: each PR's line in issue #133 was added in the step that opened the PR (PR #145 11:16:35Z, issue 11:16:55Z; PR #147 13:42:47Z, issue 13:43:13Z; PR #148 14:49:46Z, issue 14:50:04Z). Each fix round that changed a Behavior section changed the line too (PR #145 body 11:43:58Z, issue 11:44:07Z, and issue 12:43:53Z, body 12:44:19Z; PR #147 body 13:53:09Z, issue 13:53:28Z; edit histories).
- Promoted rule 1: the second Not verified item of PR #148 needs a repository on github.com, which the owner had not authorized. Comment 6235321357 ruled on it before the merge, and the body links that comment (edit history, 14:51:04Z). The other Not verified items are macOS only.
- The same-premise lesson: card 122 `rounds/feedback-step-27.md` lists two other paths that rest on the premise of its first item (the `--global` copy fallback, and a symlink at a project path). Card 124 `rounds/feedback-step-7.md` lists `update`, `update --dry-run` and the `uninstall` hand-over. The next review of each passed (events 3923, 4070).
- The handoff-condition lesson: PR #145, #147 and #148 Verified carry the results that rulings 6232580233 (item 1), 6234185763 (condition 3) and 6234962470 (condition 3) ask for.
- The issue-items lesson: PR #145 closed issue #122 with 19 of its 20 boxes ticked. The 20th item, 新-113-4, moved to #124 by ruling 6232421913 (item 3), and PR #145 comment 6233259713 says so.
- The deviation lesson: PR #146 wrote only the items that rulings name. It put the new differences that it measured on #123 (comment 6233978732), not into PRODUCT.md. The deviation that PR #147 adds is the one that ruling 6234185763 names.

## Review of 2026-10-17 16:43 to 2026-10-17 17:42 (+0800)

Range `fc634f1..6e2c6fd`: 2 merged PRs (#141, #142), 5 commits (3 non-merge, 2 merges), 12 files changed. No tag. PR #141 is the previous review and had no card. The acting owner added `882d6e8` to it after it was opened (PR opened 08:51:43Z, commit 08:52:34Z). PR #142 came from card 127: no stop (board 127 events 3747-3750: every symbol it changes is LOW, and the CRITICAL ones are not changed), and a pass on the first review (event 3772). The card was claimed at 08:44:32Z (event 3745), before PR #141 merged at 08:55:17Z, and `f1008b3` has `fc634f1` as its parent. So the four lessons that the previous review added were not in the card's LESSONS.md. Card files cited below are under `~/.config/board/projects/apm-go/cards/issue-127/`.

What went wrong:

- Issue #127 said that the Oracle always writes Windows-native paths in its warnings. The card ran Oracle 0.33.0 on Windows, found that the Oracle writes relative paths with `/`, and `f1008b3` aligned the one warning that did not. This is the third day for that lesson, and it is promoted (below).
- PR #142 closed issue #127 with all 10 boxes unticked. The conclusions for the items closed without a code change are in a card file only (new lesson below).
- The LESSONS.md section of PR #142 leaves out a lesson that applies and that the implementer's report covers (existing rule missed below).
- The previous review cited a local session transcript as evidence, and `882d6e8` replaced it (existing rule missed below).

No merged change was found broken.

Done right:

- Before it closed an item that the Oracle has a counterpart for, the investigation ran Oracle 0.33.0 in the Windows sandbox for that item. Self-update has no counterpart, and the items with no file-system code were compared byte for byte with the same steps on Linux (`investigation.md`, "第 4 項", "第 5 項"). This includes the item marked "先查清楚", which it ran with a remote git package under `core.symlinks` false and true (PR #142 Verified; the header of issue #127 comment 6232075969 names the Oracle 0.33.0 runs). The investigation ended (board 127 event 3751) before any code was written.
- The deviation lesson: the PR step found two new Windows differences (`- C:dep` accepted, the path of a `subst` drive). It put both to the owner in issue #127 comment 6232075969 (edited at 09:36:19Z) and did not write them into PRODUCT.md. The only new deviation in PRODUCT.md is the `pack` junction one, ruled in comment 6229756422.
- The porting lesson, which was not yet in the card's base: `f1008b3` ports the form of Python 3.12's `ntpath.realpath(strict=True)` (one open of the whole path, and `check_eloop`'s 1921) in place of the POSIX walk one component at a time. Its comment cites the Oracle's `install/phases/local_content.py:139-148` (`internal/gitops/realpath_windows.go`).
- Promoted rule 1: the implementer's report lists three checks it did not run (`out/report.md`, "沒有驗證的"). Before the PR opened, the `subst` drive was run (PR #142 Verified), `C:dep` was run (comment 6232075969), and the English system language was tried, with the command that failed written in PR #142's Not verified section (`Set-WinUILanguageOverride -Language en-US`). The other Not verified item is macOS, and this machine has no macOS host or VM.
- Promoted rule 2: PR #142 was opened at 09:37:17Z, and its line was added to issue #133 at 09:37:25Z. The edit added only that line (the issue's edit history).
- The Windows and lock-upgrade lessons: PR #142 Verified lists symlinks and junctions, and install, update, frozen install, `deps tree` and uninstall on locks written by Oracle 0.33.0 and by main. The reviewer ran them again on a new build (board 127 events 3770-3772). Again main, not `v0.4.0`, wrote the old lock.

## Review of 2026-10-17 14:35 to 2026-10-17 16:43 (+0800)

Range `ad865d7..fc634f1`: 2 merged PRs (#139, #140), 6 commits (4 non-merge, 2 merges), 22 files changed. No tag. PR #139 is the previous review and had no card. PR #140 came from card 119: one stop on rule 1 for the CRITICAL blast radius of `HashFileBytes` (board 119 event 3651), ruled in issue #119 comment 6230883992; two failed reviews (event 3691 on `0b61a7c`, event 3717 on `b168eeb`), fixed in `b168eeb` and `c9c833b`; a supplementary ruling after the second (comment 6231670262); a pass on the third review (event 3741). Card files cited below are under `~/.config/board/projects/apm-go/cards/issue-119/`.

What went wrong:

- The first review found two regressions. The copy fallback of `install -g` wrote a bare CR as LF where the symlink deploy shows the source, so a lock written in one mode failed a frozen install in the other (new lesson below). The clone pin `git clone -c core.autocrlf=false` lost to a `core.autocrlf` that a parent process passes in `GIT_CONFIG_*`, where the Oracle code the brief cites passes `-c` before the subcommand (new lesson below).
- The fix round for the first finding kept the source's bytes in three cases for every deploy, not only for the `--global` copy fallback that needed it, and wrote this into PRODUCT.md as a deviation citing two rulings that do not name it, and into the PR's To confirm section. The orchestrator's feedback for that round asked for it. The second review failed it, and comment 6231670262 limited the exception to the copy fallback (existing lessons below, no count raised for the second).
- The PR body sent to each review left out results that the handoff conditions of comment 6230883992 ask for and the implementer's report had, and said nothing about the lessons. The acting owner added them after the third review began (new lesson and existing rule missed below). During that edit the body was written back empty for 17 s (new lesson below).

No merged change was found broken.

Done right:

- Promoted rule 2: PR #140 was opened at 07:37:54Z and its line was added to issue #133 at 07:38:04Z. Each fix round updated that line with the body (body 08:14:33Z, issue 08:14:44Z; body 08:39:27Z, issue 08:39:29Z), as the fix-round lesson asks.
- Promoted rule 1: the one Not verified item of PR #140 is a macOS run, and this machine has no macOS host or VM.
- The lock-upgrade and Windows lessons: the first round ran install, frozen install and uninstall on an old Windows lock, an Oracle lock and a CRLF checkout, with autocrlf true and false, in the Windows sandbox (`rounds/report-step-5.md:46-51`). The old lock was written by main, not `v0.4.0`, and the report says so (`:69`).
- The same-premise lesson: the feedback for the second round listed every git call that writes a working tree, three in all (`rounds/feedback-step-12.md`, section 2), and the second review found none left (event 3717).

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
- Done right on 2026-10-16: after the `use_skill` intervention, board 112 event 3351 passed on the first round. Done right on 2026-10-17: card 116 listed every check built on resolving symlinks before the first round (board 116 event 3546), fixed them all with one `go.mod` setting, named them in ARCHITECTURE.md "Windows links" (`cef28e5`), and passed on the first review (event 3579). Card 122 `rounds/feedback-step-27.md` lists two other paths on the premise of its first item, the `--global` copy fallback and a symlink at a project path, and the next review passed (board 122 event 3923). Card 124 `rounds/feedback-step-7.md` lists `update`, `update --dry-run` and the `uninstall` hand-over for the checkout reuse it rejects, and the next review passed (board 124 event 4070).
Seen 2× (2026-10-15, 2026-10-16)

**Run a change that splits, joins or resolves paths, or that checks symlinks, in the Windows sandbox before merge, with a junction as well as a symlink.**
- CI runs only `ubuntu-latest`. Code that splits paths on `/`, or that relies on `os.Lstat` and `filepath.EvalSymlinks` to see links, passes on Linux and fails on Windows. Go reports a junction as `ModeIrregular`.
- Evidence: `22e71c6` (PR #90, `joinRealpath`) and `0636bf8` (PR #105, `oraclePathJoin`) fail on `\` paths, found in issue #88 comment 6216218590 and now in issue #127; PR #91/#92 containment checks bypassed by junctions, issue #116 (opened 2026-10-16, about 23 h after PR #92 merged).
- Done right on 2026-10-16: PR #117 comment 6218591422. Done right on 2026-10-17: PR #121 (Windows sandbox runs listed under Verified); PR #136 Verified (25 scenarios, each with a junction and a symlink, on main, the branch and Oracle 0.33.0), run again by the reviewer in board 116 events 3575-3578; PR #142 Verified (relative, dangling and looping symlinks, and a junction that leads out, each compared with Oracle 0.33.0 on Windows), run again by the reviewer in board 127 events 3770-3772.
Seen 2× (2026-10-15, 2026-10-16)

**For any change that reads the lock, cleans stale or orphan files, or renames module keys, run install, update, frozen install and uninstall on a lock written by the previous release and on one written by Oracle 0.33.0 before handing over.**
- Each of these three cards passed its own tests and broke only on an upgrade path, which the reviewer found by running the old binary first.
- Evidence: board 75 event 2692 (`150bef8`: removed packages from legacy locks leave files); board 113 event 3391 (`00b5975`: upgrading an Oracle lock deletes a registry package's `.git/config`); board 114 event 3462 (`ce7a1b2`: old-clone cleanup deletes the new package; uninstall rewrites surviving keys), fixed in `ca1cf45`.
- Done right on 2026-10-17: PR #120 comment 6219879113 (upgrade run with the main and PR binaries on a main-written and an Oracle-written lock); PR #134 Verified (install, update and frozen install on a lock with `port: 443` written by the previous release and on an Oracle-written lock with a port; uninstall was not run), run again by the reviewer in board 115 event 3534; PR #136 Verified (install, update, frozen install and uninstall in the Windows sandbox on locks written by main and by Oracle 0.33.0; main, not `v0.4.0`, wrote the old lock); PR #142 Verified (install, update, frozen install, `deps tree` and uninstall in the Windows sandbox on locks written by Oracle 0.33.0 and by main, with a relative symlink and a `C:\` local dependency).
Seen 2× (2026-10-16, 2026-10-17)

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
- Evidence: issue #118 comment 6229823230 requires `install -g --frozen` to succeed under a restricted token. `230e9df` wrote into PRODUCT.md "A symlink inside a local skill is not deployed and not recorded in the lock, where a symlink deploy records it."; the first body of PR #138 (edit history, 05:20:12Z) listed the resulting frozen failure under To confirm; the first version of issue #118 comment 6230006003 (05:11:03Z) asked the owner whether to accept it. The reviewer reproduced it in the Windows sandbox and failed the round (board 118 event 3625, on `230e9df`). `27f95f3` makes the copy deploy record the same paths and hashes as a symlink deploy, and the next review passed (event 3644). The second body of PR #140 (edit history, 08:14:33Z) put under To confirm that three kinds of text keep the source's bytes where the Oracle writes LF, although ruling (c) of issue #119 comment 6230883992 had already decided it (issue #119 comment 6231670262, items 1 and 5).
Seen 1× (2026-10-17)

**When a fix round changes what a PR's Behavior section says, update the PR's line in the release-notes issue in the same step.**
- Release notes are written from that issue (promoted rule 2), so a case added only to the PR body is missing from them.
- Evidence: PR #138's body edit at 06:31:25Z (edit history) adds "或 skill 裡有 symlink 時" to the case where a skill directory is kept with a warning. Issue #133 was last edited at 05:20:29Z, and its PR #138 line names only "目錄裡有自己加的檔案或套件改過時".
- Done right on 2026-10-17: both fix rounds of PR #140 rewrote its Behavior section, and its line in issue #133 changed with it each time (body 08:14:33Z, issue 08:14:44Z; body 08:39:27Z, issue 08:39:29Z; both edit histories).
Seen 1× (2026-10-17)

**When a change alters what a deploy writes or records, run `install -g` by symlink and then `install -g --frozen` by the copy fallback (the restricted token in the Windows sandbox), and the reverse, before handing over.**
- PRODUCT.md promises that one lock restores under both ways of a `--global` deploy (issue #118). Each way has its own code, so a change can move one and not the other, and only a frozen install in the other mode shows it.
- Evidence: board 119 event 3691 (review of `0b61a7c`: with a bare CR in an agent, the frozen install exits 1 in both directions, and main exits 0); the first round's report has no restricted-token run (card 119 `rounds/report-step-5.md`); fixed in `b168eeb`. Board 118 event 3625 (`230e9df`, the copy fallback recorded other paths than the symlink deploy) was found the same way on the same day.
- Done right on 2026-10-17: from its second body on (edit history, 08:14:33Z), PR #140 lists the run "以管理員用連結部署 `install -g`、再以受限權限用複製跑 `--frozen`（以及反方向）" under Verified.
Seen 1× (2026-10-17)

**When you port a mechanism from the Oracle, port the form its cited lines use, not only the setting; where apm-go's form differs, test the case the Oracle's form handles.**
- A setting in the right value and the wrong place passes every ordinary test. Here `git clone -c` writes repository config, which a value a parent process passes in `GIT_CONFIG_COUNT` outranks; the Oracle passes `-c` before the subcommand, and its comment says why.
- Evidence: card 119 `investigation.md:66` reports that the Oracle adds `-c core.autocrlf=false` to each git subprocess (`cache/git_cache.py:63-92`); the fix it proposes (`investigation.md:81`) and the brief (`brief.md:46`) use `git clone -c`; `0b61a7c` wrote that form into PRODUCT.md "as in the Oracle's git cache"; board 119 event 3691 (finding 2, reproduced with the real loader and in the Windows sandbox); fixed in `b168eeb`.
- Done right on 2026-10-17: `f1008b3` ports Python 3.12's `ntpath.realpath(strict=True)` on Windows in its own form, one open of the whole path and `check_eloop`'s error 1921, not the POSIX walk one component at a time, and its comment says why (`internal/gitops/realpath_windows.go`). The card's base did not have this lesson yet.
- Evidence (second instance): card 122 decided whether a losing primitive is "the same" by rules of its own twice. The first compared the bytes of the two sources (`5ecc718`; review failed, board 122 event 3821). The second, from the orchestrator's feedback (`rounds/feedback-step-16.md`, section A), compared file paths and bytes, and deployed the loser into a temporary directory (`d7586bd`). The next review failed it for a skill with an empty directory and for an unwritable `TMPDIR` (event 3846). `rounds/feedback-step-24.md` names the cause: 「「相同」的判斷沒有照 Oracle 的比較函式移植」. `3a8f5b8` ports Python 3.12's `filecmp.dircmp` (`skillDirsEqual`) and computes in memory, and the third review passed (event 3923).
- Evidence (third instance): card 126 brief stage B said to find a package's root agents and hooks "照 Oracle 的 glob", without the glob's case rule, and `ad49be0` reused the case-sensitive `strings.HasSuffix` of `extractHookName` and `extractRootAgentName`. Python 3.12's `Path.glob` ignores case on Windows. The review found that `hooks/UPPER.JSON` was deployed by Oracle 0.33.0 and not by apm-go in the Windows sandbox, and failed the round (board 126 event 4598, on `ad49be0`; card 126 `rounds/findings-step-24.md`). `rounds/feedback-step-25.md` names the cause and gives the full rule: every glob the card added matches as `Path.glob` does, without case on Windows and with case elsewhere. `2b282f4` fixed it, and the next review passed (event 4628).
Seen 2× (2026-10-17, 2026-10-20)

**Before handing over, check the PR body against each handoff condition of the owner ruling, one by one, and carry over the implementer's result for each.**
- The reviewer and the owner read the body, not the card's report. A result left in the report is not handed over, and a body rewritten in a fix round can drop a result the earlier body had.
- Evidence: issue #119 comment 6230883992 asks for CRLF Go tests run on Linux and Windows, and for install, frozen install and uninstall on an old Windows lock, an Oracle lock and a CRLF checkout, with the results in the PR body. The implementer's report had them (card 119 `rounds/report-step-5.md:48-51,60`). The first body of PR #140 (07:37:54Z, edit history) has no frozen install on the old locks and no Windows run of the Go tests; the second (08:14:33Z) also drops the uninstall, and the third (08:39:27Z) also drops whether the stale cleanup left files. They were added at 08:41:57Z, after the third review had begun (board 119 event 3736, 08:39:48Z).
- Done right on 2026-10-17: the Verified sections of PR #145, #147 and #148 carry the results that issue #122 comment 6232580233 (item 1), issue #144 comment 6234185763 (condition 3) and issue #124 comment 6234962470 (condition 3) ask for.
Seen 1× (2026-10-17)

**Before you write a PR or issue body back from a file, check that the command that produced the file succeeded and that the file is not empty; run every `gh` command with `-R gn00678465/apm-go`.**
- Outside a git checkout `gh pr view <N>` fails and leaves an empty file, and writing that file back blanks the body. Every card edits the release-notes issue the same way (`gh api -X PATCH ... -F body=@<file>`).
- Evidence: PR #140's body edit history: empty at 08:41:09Z, restored at 08:41:26Z, then edited again at 08:41:40Z. The acting owner ran `gh pr view 140` without `-R` in a scratch directory, where it failed with `not a git repository`, and wrote the empty output file back with `gh pr edit --body-file`; the edit at 08:41:40Z checked the file's length first.
- Done right on 2026-10-17: the PR #142 line of issue #133 was added in one edit that left every earlier line as it was (edit history, 09:37:25Z).
Seen 1× (2026-10-17)

**When a PR closes an issue that lists items, account for every item where `gh` can read it: tick the box of each item that is done, and write the conclusion and its evidence for each item closed without a code change in the PR body or in a comment on the issue.**
- The issue closes when the PR merges. A conclusion that is only in a card file cannot be read on GitHub, so a later reader sees open boxes and cannot tell a checked item from a forgotten one.
- Evidence: issue #127 lists 10 items as `- [ ]`. PR #142 closed it at 09:42:33Z, and the body of the issue was never edited (edit history empty), so all 10 boxes are still open. The body of PR #142 covers the items it fixes, self-update and the `core.symlinks` item. It does not cover the Windows runs of the non-file-system fixes (c20-1, c23-1, c57-9, c62-8) or of the stale cleanup and command files (62, 72), the `pack --json` paths (58, c38-1), or why the claim of item 新-Windows 實測-6 was narrowed. The two comments on the issue (6229756422, 6232075969) do not cover them either. They are only in the card's `investigation.md` ("各項結論"), and the card notes asked to tick off those items with evidence (board 127 event 3745, `orchestrate`).
- Done right on 2026-10-17: PR #145 closed issue #122 with 19 of its 20 boxes ticked. The 20th item, 新-113-4, moved to #124 by issue #122 comment 6232421913 (item 3), and PR #145 comment 6233259713 says so. Done right on 2026-10-20: issue #126 has every item ticked that PR #152 covers; items 75 and 78 stay open by scope comment 6266809423.
- Evidence (second instance): PR #153 closed issue #151 at 02:30:49Z. The issue lists six items as `- [ ]`, its body was never edited (edit history empty), and all six boxes are still open, although the body of PR #153 describes each item.
Seen 2× (2026-10-17, 2026-10-20)

**When a change alters what install writes, adopts or reuses, run `install --frozen` on each state the change leaves, with the network off as well, and compare it with main before handing over.**
- A frozen install writes no lock, checks each deployed hash against the lock, and reuses the checkouts in `apm_modules/`. A state that an ordinary install handles can fail it. Both cards below ran frozen installs, but only on states that an ordinary run had just written, with the git server up.
- Evidence: card 144 `rounds/feedback-step-7.md` (orchestrator reject, board 144 event 3983): the install skips a user file, the user then makes it equal to the source, and `install --frozen` exits 1 with `expected <not in lockfile>`. Handoff condition 3 of issue #144 comment 6234185763 forbids that. Card 124 `rounds/feedback-step-7.md` (orchestrator reject, board 124 event 4049): after an install of `ref: main`, every `install --frozen` removed the checkout and cloned it again, so it failed with the network off, where main reuses the checkout. The brief requires a frozen install of a branch to stay as on main. Both were fixed before the PR opened.
Seen 1× (2026-10-17)

**When the brief or the implementation reads an owner ruling in a way its words do not say, stop with `board ask` before the PR opens. Do not leave the reading under To confirm for the reviewer.**
- The reviewer checks the PR against the ruling's words, so the round fails even when the owner then accepts the reading. The fix round then changes no code and waits for the ruling.
- Evidence: issue #144 comment 6234185763 (13:01:17Z) protects a skill directory when "舊 lock 在這個 bucket 沒有記錄". The brief was written after it (the investigation ended at board 144 event 3967, 13:05:09Z) and took the paths of every bucket as managed (card 144 `rounds/feedback-step-15.md`: "造成這次爭議的是 brief 規則 1 的解讀"). The first body of PR #147 (edit history, 13:42:47Z) put that reading under To confirm. The review began at 13:43:27Z (event 3997). Comment 6234603992, which accepts the reading, was posted at 13:44:13Z, and the review failed on the ruling's words at 13:46:53Z (event 4001, on `22df9d3`). The fix round changed no Go code (`rounds/feedback-step-15.md`), and the next review passed (event 4022).
- Evidence (second instance, a reading that was not flagged at all): issue #126 comment 6270876048 item 1 says to adopt a file only when the file on disk equals the content with its links rewritten. Card 126's implementation adopted no agent, prompt or skill whose links are rewritten, and its report headed that rule "裁定 1（留言 6270876048 第 1 項）：含改寫連結的檔不 adopt" (card 126 `rounds/report-step-15.md:7`). A file that the branch itself wrote was then never adopted after the lock was deleted. The orchestrator rejected the round (board 126 event 4578; `rounds/feedback-step-17.md`, section 1), and the next round implemented the ruling's words.
Seen 2× (2026-10-17, 2026-10-20)

**Rebase onto `origin/main` and resolve every conflict before you open a PR. Do not open a PR that GitHub reports as conflicting.**
- GitHub runs no `pull_request` workflow for a PR that does not merge cleanly, so CI tests only the branch head. The tree that the reviewer reads also changes at the rebase.
- Evidence: PR #146 changed PRODUCT.md and merged at 12:54:10Z. Card 144 committed `22df9d3` on `c4bbd68` at 13:40:56Z (board 144 event 3992). The first body of PR #147 (edit history, 13:42:47Z) says under Risks "本分支和 main 的 `PRODUCT.md` 衝突（#146 改了同一項），合併前要 rebase 並重新解決". `gh run list -R gn00678465/apm-go --branch fix/144-skip-unmanaged-user-files` shows only a `push` run for `22df9d3`. The first `pull_request` run is on `13127e6` (13:51:22Z), after the fix round rebased the branch onto `c4fc40d` (card 144 `rounds/feedback-step-15.md`, item 1).
Seen 1× (2026-10-17)

**Before the first stop on rule 1, run `impact` on every symbol the brief names and every function the change will edit, helpers that only pass a new argument down included, and put them all in one stop. Do not stop again for a set that a ruling already covers.**
- Each stop waits for the acting owner. A second ruling on symbols that the first stop could have listed adds a round and decides nothing new. Issue #151 comment 6278303322 asks for one stop ("不要為同一個 brief 分批停").
- Evidence: card 126 stopped on 11 symbols (board 126 event 4491), ruled in issue #126 comment 6266899072, and then stopped again on the same 11 (event 4497) until an owner note said that the ruling covers the card (event 4502). The implementer then stopped for 13 symbols that brief section 4 already named (event 4518; comment 6267470396: "brief 第 4 節點名要改的符號，都算在本裁定"), and again for two callers that each pass one new argument (event 4566, question 2; comment 6270876048 item 2). Card 151 stopped on 10 symbols (board 151 event 4676; comment 6278256554), then on 8 more that the brief changes (event 4686; comment 6278303322). The implementer then stopped for 13 more: internal helpers of ruled symbols, and functions that pass the alias directory or the http flag down (event 4779; comment 6280583753 item 1).
Seen 2× (2026-10-19, 2026-10-20)

**When a value from `apm.yml` becomes a path or a directory name, validate it the same way wherever else it is read, `apm.lock.yaml` included, and test a changed lock.**
- A lock comes from version control and can come from another person's PR. A check on `apm.yml` alone leaves the same value unchecked when a frozen install, `update` or `uninstall` reads it from the lock. The Oracle checks a lock's `alias` with the function it uses for `apm.yml` (`deps/lockfile.py:286-296`, cited in card 151 `rounds/feedback-step-13.md`).
- Evidence: card 151 checked `alias` in `apm.yml` (`parseAliasField`) and stored the lock's `alias` as it was. With `alias: o/skill` added to the lock entry of `o/pkg`, `install --frozen` exited 1, but first changed the checkout of the other package `apm_modules/o/skill` (`.git/HEAD`, `.git/config` and `.git/index` changed, `.git/shallow` gone). The orchestrator rejected the round (board 151 event 4788; `rounds/feedback-step-13.md`). The next round checks the lock's `alias` with the Oracle's text before anything is fetched or written (PR #153 Behavior: "lock 裡不合格的 alias 讓指令以 exit 1 結束，不動 `apm_modules/`").
Seen 1× (2026-10-20)

## Existing rules missed

A rule that already exists in AGENTS.md or the project's instructions and was broken again. These are not new lessons; each one is evidence that the rule is not being checked.

**AGENTS.md "Testing patterns": expected values come from the Oracle's source or output, never recomputed the way the code does.**
- A test that asserts current output protects the bug. The next card had to change it before its fix could pass.
- Evidence: `b901778` (PR #96) `TestInstall_StaleMCP_ServerMovedToDevDependenciesIsKept`; issue #97 comment 6211906602; corrected in `33b05f1` (PR #109).
Seen 1× (2026-10-16)

**Project rule: never run `rm` on a path built from a shell variable; delete with a literal absolute path, or write each rerun into a new directory.**
- The command stops at a permission prompt that no one answers, and the whole card waits for the user. The rule existed before this round (PR #63 for scripts, and the card notes).
- Evidence: board 104 event 3023 and board 97 event 3060 (`agent_blocked`); board 97 event 3069 (owner note naming the cause). Board 122 event 3778 (`agent_blocked`, the orchestrator's investigation, although the card notes repeat the rule in event 3776); board 122 event 3780 (the note that resumed it: the command ran `rm -rf dep` and `rm -f apm.lock.yaml` inside `sh -c`). Board 151 event 4708 (`agent_blocked`): the implementer reran its http scenarios with one command that deleted the old run directories with `rm -rf` and then ran `bash -c` (card 151 `blocked-implementer.txt`), although the card notes say to write each rerun into a new directory (board 151 event 4674). The intervention names the prompt as the cause of the timeout that followed and bans `rm` in any form (events 4732, 4733).
Seen 3× (2026-10-16, 2026-10-17, 2026-10-20)

**AGENTS.md "Canonical documents": for each lesson that applies, say in the PR description what you did.**
- The reviewer cannot check a lesson that the body does not mention. The board's notes for card 119 repeated the rule (board 119 events 3649, 3657).
- Evidence: PR #140's body said nothing about the lessons until 08:41:57Z (edit history), although the implementer's report had a section for them (card 119 `rounds/report-step-5.md:64-69`). The bodies of PR #136 and #138, both merged after the rule landed (`94d85b8`, PR #132), do not mention a lesson either (`gh pr view <N> --json body`). PR #142's body has a LESSONS.md section that names the Windows, lock-upgrade and deviation lessons. It leaves out the lesson on writing what the Oracle does, which applies because `f1008b3` states the Oracle's behavior in code comments and in PRODUCT.md, and which the implementer's report covers (card 127 `out/report.md`, "LESSONS.md 裡適用的教訓"). PR #145 put its lesson account in comment 6233259713 (created 11:17:38Z), not in the body, because "說明欄限 25 行". Two reviews failed it for that (board 122 events 3821 and 3846; card 122 `rounds/findings-step-15.md` finding 4, `rounds/findings-step-23.md` finding 3). The acting owner added a `## LESSONS.md` section to the body at 11:50:52Z (edit history).
Seen 1× (2026-10-17)

**AGENTS.md "Promoted rules" 3: write what the Oracle does only after you run Oracle 0.33.0 or cite the Oracle's file:line.**
- A statement inferred from apm-go's code reads like a measurement. The next round must disprove it, and the orchestrator rejected the round each time.
- Evidence: card 122 `rounds/feedback-step-8.md` (orchestrator reject, board 122 event 3801): PRODUCT.md and ARCHITECTURE.md said that a local package "is copied on every run, so it always has" a `Resolving` line, but Oracle 0.33.0 prints none for a package already in `apm_modules/`. Card 122 `rounds/feedback-step-27.md`, item 2 (orchestrator reject, event 3868): the `dirsEqual` comment and the report said that Python raises `RecursionError` on a symlink loop, but Oracle 0.33.0's `is_skill_dir_identical_to_source` returns True for that probe. The same file says that two statements of the orchestrator's own earlier feedback were wrong: the table that called the comparison `oracleText` (`rounds/feedback-step-24.md`, section A.2), and the rule there to read a file through its symlink, which caused the first item of that reject. Card 151 `investigation.md:66` says that the object form of a registry entry checks no extra fields, citing `registry_entry.py` with no line, and `rounds/brief-step-8.md:67` repeats it ("registry 形式（`id:`）不檢查，和 Oracle 一樣"). The implementer ran Oracle 0.33.0, which refuses such a field (`models/dependency/registry_entry.py:83-90`), and asked (board 151 event 4779, question 2). Issue #151 comment 6280583753 item 2 ruled to follow the Oracle, and comment 6280837480 corrects the premise.
Seen 2× (2026-10-17, 2026-10-20)

**AGENTS.md "Lessons review" step 5: check every reference with `git`, `gh` or `board log`; a finding whose evidence cannot be checked is not entered.**
- A path to a local session transcript can be read only on one machine and by one person, so neither a reviewer nor the owner can check the claim that rests on it.
- Evidence: `5834106` (PR #141) cited the acting owner's session transcript (`~/.claude/projects/.../369ffdad-....jsonl`) as the evidence for the empty-body lesson. `882d6e8` replaced it with the edit history of PR #140 before PR #141 merged.
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
- Release notes are written from one list. A change that is written only in a PR body is easy to miss. Of the PRs below, only PR #101's item reached #88.
- Evidence: Risks with upgrade-visible changes and no tracking entry: PR #91, PR #92 (2026-10-15); PR #94, #100, #105, #106, #109, #110, #117 (2026-10-16); PR #120, #121 (2026-10-17). The only one in a tracking issue: issue #88 comment 6203884235 (PR #101). No open issue collects 0.4.1 release notes (`gh issue list --search "0.4.1"`).
- Done right on 2026-10-16: issue #88 comment 6203884235.
Seen 3× (2026-10-15, 2026-10-16, 2026-10-17)

**Write what the Oracle does only after you run Oracle 0.33.0 or cite the Oracle's file:line, and write what PRODUCT.md says only with the item quoted.**
- Moved to AGENTS.md "Promoted rules" on 2026-10-17; kept here as the evidence.
- Issue bodies, comments and PRODUCT.md lines that stated the Oracle's behavior from memory were wrong, and each one sent a card or a reviewer to disprove it. The third day shows that a statement copied from an earlier record is no safer, because the record can claim more than its run showed.
- Evidence: issue #89 body on what the Oracle deploys, corrected in issue #89 comment 6187796986; issue #67 exit code, corrected in issue #67 comment 6186847177; issue #112 body "PRODUCT.md has no rule for rule files", corrected in issue #112 comment 6215906800 and issue #88 comment 6215726615; `00b5975` comment and PRODUCT.md "Oracle removes registry `.git`", refuted by board 113 event 3391 and fixed in `300568e`; `1b84146` "as the Oracle prints them" (2026-10-14), corrected by `eb3de05`. On 2026-10-17, issue #127 item 新-Windows 實測-6 (opened 2026-10-17 07:09 +0800) says "Oracle 一律用 Windows 原生路徑" for the paths in warnings. It neither ran the Oracle nor cited a line, and copies issue #88 comment 6216218590 ("Oracle 一律用 Windows 原生的絕對路徑"). That comment reports a Windows sandbox run, but its "一律" also covers relative paths. Card 127 ran Oracle 0.33.0 on Windows and found relative paths written with `/` (`investigation.md`, "第 2、9 項"). `f1008b3` prints the relative path of the claude rule warning with `/` "as the Oracle and compile's Skipping line print one" (`internal/deploy/instructions_claude.go`).
- Done right on 2026-10-15: the #67 and #89 investigations measured the Oracle and corrected the issue before any code was written. Done right on 2026-10-17: `ea217bf` cites the Oracle's `utils/github_host.py:437-532` and `reference.py:593-595,1690-1694`, and PR #134 Verified compares the lock fields of the https port cases with an Oracle 0.33.0 run; the PRODUCT.md item of `cef28e5` cites the Oracle's `utils/path_security.py:222-245`, `integration/mcp_integrator.py:78` and `compilation/inventory.py:61`, and board 116 event 3558 reports Oracle 0.33.0 run on Windows with a junction and with a symlink; card 127 ran Oracle 0.33.0 on Windows for each item of issue #127 that the Oracle has a counterpart for before closing it, and found the issue's claim wrong before writing code (PR #142 Verified, issue #127 comment 6232075969, board 127 event 3751).
Seen 3× (2026-10-15, 2026-10-16, 2026-10-17)

**Do not write a new deviation from the Oracle into PRODUCT.md unless an owner ruling names it; when the fix needs one, stop with `board ask`.**
- Moved to AGENTS.md "Promoted rules" on 2026-10-20; kept here as the evidence.
- PRODUCT.md is the contract that later cards follow. An unruled deviation there is read as a ruling, and reversing it the next day cost a full card.
- Evidence: `9968ee5` (PR #92) added "a deployed file that is a symlink to another place in its target root … is written where it points"; issue #112 comment 6215906800 ("#86 has no owner ruling on this point"); reversed by `cf8aa7a` (PR #117). `b168eeb` (PR #140) wrote into PRODUCT.md that a bare CR, text with a NUL and text with a CR before a CRLF keep the source's bytes, citing issue #119 comment 6230883992 and issue #118 comment 6229823230, neither of which names it; the orchestrator's feedback asked for it (card 119 `rounds/feedback-step-12.md:35`) and no `board ask` was sent (board 119 events 3691-3717); the review failed it (event 3717), issue #119 comment 6231670262 limited it to the `--global` copy fallback, and `c9c833b` rewrote it. On 2026-10-20, card 126 added to the PRODUCT.md item "Root agents and hooks" that on Windows apm-go does not print the Oracle's `Skipped non-agent asset` warning for a root `X.AGENT.MD` (card 126 `rounds/report-step-26.md:20`), a deviation that no ruling named. The orchestrator rejected the round and had the implementer take the clause out and ask (board 126 event 4608; `rounds/feedback-step-28.md`). The implementer asked (event 4610), issue #126 comment 6272218708 ruled it, and `2b282f4` writes the clause with that comment cited.
- Done right on 2026-10-17: board 116 event 3558 (a deviation the ruling did not name, asked before it was written), ruled in issue #116 comment 6229603360, then written by `cef28e5`. Card 127 put two new Windows differences to the owner in issue #127 comment 6232075969 (edited at 09:36:19Z) and kept them out of PRODUCT.md; `f1008b3` writes only the `pack` deviation, ruled in comment 6229756422. PR #146 writes only the deviations that rulings name, and puts the new differences that it measured on issue #123 (comment 6233978732), not into PRODUCT.md. The deviation that `13127e6` (PR #147) writes is the one that issue #144 comment 6234185763 names. Done right on 2026-10-19: card 126 asked before it wrote the deviations of the `--global` symlink deploy, the four rare cases and the adopt rule of item 64 (board 126 events 4507, 4566), ruled in issue #126 comments 6267289737 and 6270876048.
Seen 3× (2026-10-15, 2026-10-17, 2026-10-20)

## Observed, not entered

- The instruction files have outgrown Claude Code's limit. ARCHITECTURE.md grew from 69,071 to 121,598 characters and PRODUCT.md from 21,890 to 76,231 in this range (`git show 34dfa71:ARCHITECTURE.md | wc -m` against `0cf2bd2`). Claude Code warned "over the 150.0k-char total limit" (163.5k on 2026-10-16, 216.7k on 2026-10-17). Its effect on the agents is not measured.
- GitNexus does not find Go top-level functions, so the AGENTS.md `impact` gate cannot run on them. On the index of `4fb095a`, `node .gitnexus/run.cjs impact CheckWithin --direction upstream` and `context ParseManifest` return "not found", and `context materializeLocalCopy`, a method, is found. Board 116 event 3546 reports 38 of 5535 Function nodes named; card 116 gave the blast radius of the methods it could find. Card 118 found that a full rebuild fixes it: after `node .gitnexus/run.cjs analyze --index-only --force` the top-level functions are found, where 5497 of 5546 Function nodes had no name before (board 118 event 3586). Tracked in issue #130 comments 6229756641 and 6229823442.
- Card 126 timed out once after the implementer handed a stage to a background subagent and waited with no output (board 126 events 4563, 4564). The cause is the board service's limit of 30 minutes without output, so this is a finding about the board tooling. Card 151's notes tell the agents not to wait on a background subagent (board 151 event 4674).
- PR #152's CI parity check failed because the Oracle released 0.34.0 while the card ran (board 126 event 4631). Issue #126 comment 6277600550 ruled a one-line change of the OpenCode label (`89e59d2`) and kept the gate on the latest release. PRODUCT.md already says that a new upstream release can turn the gate red.
- The review of PR #153 gave its pass 76 seconds after the earlier attempt ended, with checkpoints for `go build` and `go test` only (board 151 events 4822-4825). Its prompt asks for those two checks alone (card 151 `messages/step-20-1.md`), and the reviewer harness had hit its usage limit before (event 4812). This is about the board's review prompt, so it is not entered.
