# Squad record — after-implement

- scope: marketplace-check-outdated-fixes
- cut: after-implement
- spec reviewed: specs/marketplace-check-outdated-fixes/SPEC.md v1 (approved)
- source state reviewed: 3fedb17 (gate run 3 all layers green)
- lenses: contract vs implementation, input space at code level, diff hygiene and scope (with live evidence). Three independent read-only opus contexts; inputs were the task contract (user rulings), SPEC v1, the source state, and a lens brief. Probes ran in scratch copies (`git archive` / temporary worktrees) and a binary built from 3fedb17.
- question: does the code do what the SPEC says, all of it, and nothing else?

## Findings (merged, by severity)

- [MEDIUM] internal/marketplace/authoring/fixes_read_capped_test.go:83,89 — SC-F16 asserts only the last four args and three env keys; SPEC requires the exact command `git -C <dir> show FETCH_HEAD:<path>` and the ApplySecureGitEnv keys. A mutant adding `-c core.sshCommand=evil` and replacing ApplySecureGitEnv with a single `GIT_TERMINAL_PROMPT=0` still passed — contract lens, scratch copy `go test -run TestNewShowCmd_ShapeAndSecureEnv` → PASS — class 1 — assert full Args and every `gitops.SecureGitEnv()` entry — status: fixed (1cd635b)
- [MEDIUM] internal/marketplace/authoring/refcheck.go:1026 — a local (`./…`) package with a lowercase 40-hex ref shows Current `--` instead of the current-map value; SPEC D-f/D-2 list only offline, ListRefs error, no HEAD, no tags. Before the change (3d59291) Current was the map value — all three lenses reproduced it (unit probe and binary `outdated --offline`) — class 2 — user ruling 2026-09-16 「還原為原本的值 (Recommended)」→ D-4, SC-F20, implemented 1cd635b (RED) / fe28b5d
- [LOW] internal/marketplace/authoring/refcheck.go:1160 — D-3 strips the leading `v` only when rendering Current; `outdatedShaAgainstTags` compares against the unstripped version, so with tags `v1.0.0`, `v1.0.0+build` the row is `[!]` for `1.0.0` and `[+]` for `v1.0.0` — input-space lens probe — class 1 (SC-F8 requires Status and Upgradable identical to the bare version; D-3) — strip in the comparison, add a `+build` subtest to SC-F8 — status: fixed (1cd635b, fe28b5d)
- [LOW] cmd/apm-go (no test) — SC-F9 says `check` and `outdated` exit 2 for a non-string name; only realexec `check --offline` covers it end to end — contract lens grep of realexec.sh — class 1 — add a realexec `outdated --offline` step — status: fixed (fe28b5d)
- [LOW] specs/marketplace-check-outdated-fixes/SPEC.md:82 — SC-F9 `name: [a]` subtest passed before the fix (regression), recorded in red.md but not in SPEC Revisions — diff lens — class 1 — add a Revisions entry — status: fixed (102f282)
- [LOW] internal/marketplace/authoring/fixes_read_capped_edges_test.go — added after gate run 2 for coverage, not in Setup plan or Revisions; committed after the implementation it covers (5680959 after 2d4671b) — diff and contract lenses — class 1 — add a Revisions entry; evidence ordering marks it as a follow-up test with a throwaway-mutant proof — status: fixed (102f282)
- [LOW] tools/gate/mutants.txt — `sha-name-match-restored` (`if shaRefPattern.MatchString(ref) {` → `if false {`) also removes commit matching, so SC-B1 alone kills it; `sha-match-by-name-only` no longer describes its effect — diff lens scratch run — class 1 (SC-F14 says the new mutant is killed by SC-F1) — make the mutant restore only name matching; rename the other `sha-commit-match-removed` — status: fixed (fe28b5d)
- [LOW] internal/marketplace/authoring/refcheck.go:1024,1182 — comment cites "marketplace-check-outdated-fixes D-f" (D-f belongs to the archived SPEC); `versionTagCandidates` doc still says "a capture must parse as semver" — diff lens grep — class 1 — correct both comments — status: fixed (fe28b5d)
- [LOW] internal/marketplace/tagpattern/tagpattern.go:148-155 — IsOracleVersion accepts numbers beyond uint64; `semver.IsPrerelease` then fails to parse and returns false, so `v99999999999999999999.0.0-rc.1` stays a candidate without include-prerelease; the Oracle filters it — input-space probe — class 2 — orchestrator default, overturnable at v2 approval: record as a deviation, no code change (D-5)
- [LOW] internal/marketplace/authoring/refcheck_sha.go:318,341 — refactor 6178686 changed an unreachable path: a non-cap read error after a clean Wait is returned without the `git show <path>:` prefix and with partial data, and the process is waited without cancel first; commit text said "no behaviour change for callers" — contract and diff lenses — class 2 — orchestrator default, overturnable at v2 approval: no code change, recorded (D-6)
- [LOW] internal/marketplace/authoring/refcheck_sha.go:303-318 — with the Git for Windows launcher (`Git\cmd\git.exe`) on PATH, the over-cap path returns after WaitDelay (2.04s) instead of immediately; gate uses the mingw64 git (24ms); SC-F4's 3s budget does not cover the launcher case; whether the grandchild outlives Wait was not measured — contract lens probe — class 3 — evidence honest notes
- [INFO] internal/marketplace/authoring/schema.go:730-731 — D-1's deviation examples are incomplete: apm-go accepts `name: 1:20`, `190:20:30`, `=` (PyYAML: int, int, ConstructorError); apm-go rejects `09`, `+.5`, `1.0e3` (PyYAML: str) — input-space probe with PyYAML 6.0.3 — class 2 — rule unchanged; SPEC notes the examples are not exhaustive
- [INFO] internal/marketplace/authoring/refcheck_sha.go:311-316 — `git show` timing out reports an empty message (`git show <path>: `); same text at 3d59291 — input-space probe with a fake git — class 3 — evidence honest notes
- [INFO] ARCHITECTURE.md §3.4 — the data-flow text changed beyond the Setup plan's "anchors" wording; consistent with AGENTS.md "update the owning doc in the same change" — diff lens — class 3 — no action

## Positive confirmations

- RED commits contain only tests and red.md; GREEN commits contain only product code and mutants.txt.
- `git diff --name-only 3d59291..3fedb17 -- specs/archive/marketplace-check-outdated/` empty; `git diff 3d59291..3fedb17 -- '*_test.go' | grep '^-[^-]'` empty.
- Every oracle citation checked at b75a02b1; every ARCHITECTURE anchor checked at 3fedb17; all 25 mutant anchors unique.
- Live binary (3fedb17, offline): `name: 123` and `source: [a/b]` exit 2 with the Oracle messages for both `check` and `outdated`; `name: yes` passes schema; SHA rows show Current `--`, the range row keeps `v0.9.0`.

## Concessions

- No lens re-ran the full gate or `go test ./...`; real remote SHA probing and streaming abort against GitHub were not exercised; parity corpus not run.
