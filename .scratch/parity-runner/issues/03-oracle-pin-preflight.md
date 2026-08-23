# 03 — Parity runner: Oracle/Target pin preflight

**What to build:** Before any case runs, the runner proves it is comparing the right two binaries: the Oracle checkout is at the pinned commit, the Target binary was built from the current tree, and both facts are written into the evidence. Wrong pin = fail closed, no cases run.

**Blocked by:** 01 — Parity runner: isolated execution and raw capture.

**Status:** done — 91b955b; verified .review/eval-ticket-03.md (8/8)

**Spec:** story 24 header requirement; evaluator §E1.

## Acceptance criteria

- [ ] Pinned Oracle commit lives in one place: `tools/parity/oracle.pin` containing the full 40-char SHA (`c8d6cdec596e773a84b0839c33c28b6b0a217637`).
- [ ] Preflight resolves the Oracle repo dir from `APM_ORACLE_CMD` (the `--project` argument) and runs `git -C <dir> rev-parse HEAD`; mismatch with the pin → exit 2 with both SHAs printed; no cases run. Preflight never fetches, checks out, or otherwise mutates the Oracle repo.
- [ ] Preflight runs `git -C <dir> status --porcelain` on the Oracle; a dirty tree is recorded in `run.json` as `oracle_dirty: true` and printed as a warning (not fatal — reviewers decide).
- [ ] Target: `APM_TARGET_BIN` must resolve to an existing executable. Preflight records its absolute path, sha256, mtime, and `--version` stdout. If the path is a bare name found via `PATH`, preflight refuses (exit 2) — an explicit path is required so a stray `apm-go` on PATH cannot be compared by accident.
- [ ] Preflight records `git rev-parse HEAD` and `git status --porcelain` of the Target repo (cwd) in `run.json` as `target_commit` / `target_dirty`.
- [ ] Preflight records the `uv` version and the Oracle's `apm --version` output.
- [ ] All of the above lands in `run.json` under a `preflight` object; `diff.jsonl` consumers (ticket 02) read the pin from there, and the waiver validator in 02 compares `waivers[].oracle_commit` against `preflight.oracle_commit`.
- [ ] Unit tests use a throwaway git repo in `t.TempDir()` to cover: pin match, pin mismatch, dirty tree, bare-name target refused.
