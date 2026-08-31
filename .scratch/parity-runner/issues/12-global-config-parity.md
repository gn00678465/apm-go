# 12 — Global `~/.apm/config.json` parity (F09, every case)

**What to build:** Decide and implement what apm-go does about the Oracle's per-user global state files. Two found so far: (1) `$HOME/.apm/config.json` created on any invocation; (2) `$HOME/.cache/apm/last_version_check` written by commands that run the upstream version-check (seen on `doctor`). Each needs its own decision. The Oracle creates `$HOME/.apm/config.json` (`{"default_client": "vscode"}`, `config.py:15`) on ANY invocation, including `--version`; apm-go never creates it. Every runner case therefore shows a `tree` diff on that path.

**Blocked by:** 02 — runner diff/gate (attempt 4: HOME capture + UV_CACHE_DIR isolation make this visible).

**Status:** done — 60e037d; verified .review/eval-ticket-12.md PASS. Remaining 9 registry tree diffs are runner-caused (APM_CONFIG_DIR injection) → ticket 15.

**Origin:** orchestrator intervention on ticket 02 attempt 4 (`/tmp/p4`). Currently masked only on `version` and `doctor-help` by explicit, path-named waiver text; all other cases show it unwaived.

## Acceptance criteria

- [x] Read `config.py` fully: what keys exist (`default_client`, `allow_protocol_fallback`, …), which commands read them, and whether apm-go has equivalents (it honours `APM_CONFIG_DIR` for the marketplace registry only).
- [x] Decision recorded here: (A) apm-go mirrors the file (same path under `$HOME`, same default content, same create-on-first-run) — preferred if any ported command reads it; or (B) documented deviation with a runner **baseline** mechanism: `case.json` gains `"baseline": ["home/.apm/config.json"]` semantics or a global `baseline.json` listing Oracle-only artefacts that are excluded from `tree` comparison but still captured in evidence and listed in `diff/<id>.json` under `baseline_excluded`. No per-case waivers for a universal artefact.
- [x] After the chosen fix, `version` and `doctor-help` waivers drop the `tree` field again (return to stdout-only), and the path-named note in their reasons is removed.
- [x] `diff.jsonl` on the full case set shows no `tree` diff attributable to `home/.apm/config.json`.

## Decisions

**`$HOME/.apm/config.json` (and its parent dir `$HOME/.apm`) — Decision B, baseline exclusion.**

Read `config.py` in full (`apm/src/apm_cli/config.py`). `CONFIG_DIR`/`CONFIG_FILE` are
`~/.apm`/`~/.apm/config.json` (`config.py:14-15`). `ensure_config_exists()`
(`config.py:24-42`) creates the dir (mode `0700`) and, if the file doesn't
already exist, writes `{"default_client": "vscode"}` (mode `0600`) —
called from `get_config()` (`config.py:56`, itself the base of every getter/
setter in the module) and directly from `marketplace/registry.py:34,54` on
every registry lookup, so it fires on effectively any invocation including
`--version`. The module defines getters/setters for many keys beyond
`default_client`: `auto_integrate`, `temp_dir`, `install_target`,
`self_update_channel`, `self_update_install_dir`, `allow_protocol_fallback`,
`prefer_ssh`, `copilot_cowork_skills_dir`, `registries.*`,
`audit_on_install`, `external_scanners.*`, `mcp_registry_url`. Grepped
apm-go for every one of these key names, `config.json`, `CONFIG_FILE`, and
`APM_CONFIG_DIR`: apm-go has **no code path that reads or writes
`~/.apm/config.json`** — `APM_CONFIG_DIR` (apm-go's own env var) is an
unrelated, per-run sandboxed marketplace-registry directory, not this
oracle global-config path, and none of the config.json-backed features
(MCP default-client selection, self-update channel, protocol-fallback
preference, external-scanner config, etc.) have been ported. Mirroring the
file would create a file apm-go itself never reads — decision **(B)**:
excluded via `tools/parity/baseline.json`, not mirrored.

**`$HOME/.cache/apm/last_version_check` (and its parents) — Decision B, baseline exclusion.**

`get_update_cache_path()` (`utils/version_checker.py:261-271`) resolves to
`~/.cache/apm/last_version_check` and creates the parent dirs.
`save_version_check_timestamp()` (`utils/version_checker.py:301-308`)
touches that file whenever `check_for_updates()` runs and
`should_check_for_updates()` (cache absent or >86400s old) says to check;
`check_for_updates` is called from `_check_and_notify_updates()`
(`commands/_helpers.py:453-486`), wired into the CLI's post-command result
callback at `cli.py:178` — so it fires on commands that reach that
callback (seen on `doctor`) but not on `--version`/`--help`, which exit
before it runs. Grepped apm-go for `last_version_check`, `version_check`,
`UpdateCheck`, `check_for_updates`/`checkForUpdate`, and any GitHub
latest-release lookup: apm-go has **no update-check feature of any kind**
— no code compares the running version against a remote latest, and
nothing persists a check timestamp. Decision **(B)**: excluded via
`tools/parity/baseline.json`, not mirrored.

## Implementation

- Added `tools/parity/baseline.json`: a global, runner-owned list of exact
  Oracle-only paths (`home/.apm`, `home/.apm/config.json`, `home/.cache`,
  `home/.cache/apm`, `home/.cache/apm/last_version_check`), each with a
  non-empty `reason` and an `oracle_ref` source citation. Loaded and
  validated (exact paths only, no globs, non-empty reason, no duplicates)
  before any real case executes, exactly like `waivers.json` — an invalid
  `baseline.json` fails the run closed with exit 2.
- `diffTrees` now takes the baseline path set and, for any path it names
  that would otherwise land in `added`/`removed`/`changed`, moves it to
  a new `baseline_excluded` list on `diff/<id>.json`'s tree detail instead.
  Excluded paths do not count toward the case's `tree` diff field (so they
  need no per-case waiver), but the underlying evidence is still captured
  and the exclusion itself is still visible to a reviewer whenever a
  case's diff detail is written for some other reason (e.g. the
  `pack-refuse-*` cases' real `cwd/build` diff).
- `version` and `doctor-help` waivers dropped `tree`/`tree_paths` entirely
  (back to `stdout`-only) and the path-named sentence in their `reason`s
  was removed — their only tree diff was the now-baseline-excluded
  `home/.apm/config.json`.
- Other waivers whose `tree_paths` also happened to list
  `home/.apm/config.json`/`home/.cache/apm/last_version_check` alongside
  real product-specific paths (`pack-refuse-agent-plugin`, `pack-refuse-apm`,
  `browse-unknown-marketplace`, `list-empty`) were intentionally left
  untouched per this ticket's scope: their coverage check only requires
  every *actually differing* tree path to be listed, so the now-unused
  global-artefact entries in those waivers are harmless supersets, not a
  correctness problem.
