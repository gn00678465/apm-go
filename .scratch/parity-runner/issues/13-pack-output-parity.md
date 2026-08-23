# 13 — `pack` success output + `--help` parity (pre-existing)

**What to build:** `apm-go pack`'s success output matches the Oracle's line set and wording, paths are printed relative to cwd as the Oracle prints them, and `pack --help` carries the Oracle's semantic flag set.

**Blocked by:** 10 — error/warning output contract (so channel/prefix are already aligned and only content remains).

**Status:** ready-for-agent

**Origin:** runner cases from ticket 07 (`.review/eval-ticket-07.md` §7.3). Selector/refusal/lockfile/next-steps all PASS; these are pre-existing `pack` gaps the runner exposed.

**Oracle:** `commands/pack.py` (success path logging), `bundle/packer.py` (what is printed per file), `commands/pack.py:155-200` (help).

## Findings to close
- [ ] Target prints every bundled file (`skills/hello/SKILL.md`, `plugin.json`); Oracle prints a three-line `[*]`/`[i]` summary only. Match the Oracle's line set at default verbosity (per-file listing only under `-v` if the Oracle does that).
- [ ] Target prints the absolute `<TMP>/build/…` output path; Oracle prints relative `build/…`. Print relative to cwd.
- [ ] `pack --help` `help_semantic` diff is empty (flags, defaults, descriptions) except the deviation sentence ticket 07 added, which is appended to `--format`'s description and must be the ONLY description delta.
- [ ] Usage errors (conflict / empty / missing-arg): Oracle's Click prints a `Usage: … Try 'apm pack --help' for help.` block before the error; decide once for all commands whether apm-go mirrors that boilerplate (record in ticket 10's decision) — then `pack-format-conflict` / `-empty` show no `stderr` diff or a single shared, path-precise F01 waiver.
- [ ] Runner: all 12 `pack-*` cases show only ticket-12 `tree` paths (or none once 12 lands) and the two refusal waivers.
