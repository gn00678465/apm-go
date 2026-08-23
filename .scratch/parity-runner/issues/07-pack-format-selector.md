# 07 — `pack --format` / `--claude-plugin` selector

**What to build:** `apm-go pack` accepts upstream's five `--format` choices and `--claude-plugin`. Claude-family selectors produce exactly today's bundle and stamp `pack.format: claude-plugin` in the embedded lockfile; `agent-plugin` and `apm` are refused with a clear "not yet supported" usage error before any file is written. `plugin init`'s next steps are restored to upstream's `pack --format …` lines. Runner cases prove every branch.

**Blocked by:** 02 — runner diff/gate; 04 — shared format-selector resolver.

**Status:** done (selector/refusal/lockfile/next-steps) — 9cad398; verified .review/eval-ticket-07.md. Runner-evidence AC FAIL is pre-existing pack output/help drift split into ticket 13.

**Oracle:** `commands/pack.py:155-175,318-326` (flags + resolution), `bundle/lockfile_enrichment.py:249` (`pack.format`), `commands/init.py:322-327` (next steps). Spec stories 17–23. Evaluator guardrails: `.review/ticket-review.md` §D, §F "05".

## Acceptance criteria

### Selector surface
- [ ] `--format` uses the shared 5-choice resolver from ticket 04; `--help` shows `--format [plugin|agent-plugin|claude|claude-plugin|apm]` with upstream's description, followed by one sentence: "apm-go currently implements only the Claude plugin bundle; agent-plugin and apm are accepted but refused."
- [ ] `--claude-plugin` bool flag with upstream's help text.
- [ ] Resolution happens before any producer, lockfile read/write, marketplace build, or output-directory creation. A test pre-creates nothing, runs a refused invocation, and asserts the cwd tree is byte-identical before/after (use the runner's tree listing helper or `filepath.Walk`+sha).
- [ ] Conflict / unknown / empty / missing-argument errors are the same strings and exit 2 as `plugin init` (shared helpers from 04). `--format apm` is NOT an unknown value here — it parses, then is refused (next item).
- [ ] `agent-plugin` and `apm` → `withExitCode(2, …)` with message `bundle format 'agent-plugin' is not yet supported by apm-go; use --format claude-plugin` (resp. `'apm'`). Never reaches the Claude exporter.
- [ ] `plugin`, `claude`, `claude-plugin`, `--claude-plugin`, and no selector all take the existing Claude path. A test asserts the bundle tree + every file's bytes are identical across the five invocations (modulo `packed_at`).

### Lockfile
- [ ] The embedded bundle `apm.lock.yaml`'s `pack:` section carries `format: claude-plugin` (the resolved selector's canonical value, as upstream `BundleFormat.lock_value`), via the existing `PackMetadata.Format`. Key order unchanged: format, target, packed_at, ….

### plugin init next steps
- [ ] Both plugin modes print upstream's three lines: "Add dev dependencies:    apm-go install --dev <owner>/<repo>", "Pack as Agent Plugins v1:             apm-go pack --format agent-plugin", "Pack as Claude plugin:                apm-go pack --format claude-plugin". The ticket-3-era "intentional deviation" comment is removed. The existing next-steps test that walks each line and checks the flag exists on the named command must still pass (now `pack` has `--format`).

### Runner evidence
- [ ] Fixture: a minimal plugin project (apm.yml + plugin.json + one skill file). Cases (`pack-`): help, no-flag, format-plugin, format-claude, format-claude-plugin, claude-plugin-flag, conflict, empty, missing-arg, unknown, refuse-agent-plugin, refuse-apm.
- [ ] Claude-path cases: `diff.jsonl` clean on exit, stdout/stderr (normalised), tree, and all artifact bytes except `packed_at` (normalised as `<TS>`).
- [ ] `refuse-agent-plugin` and `refuse-apm`: Oracle succeeds and writes a bundle; Target exits 2 and writes nothing. These two cases get field-precise `waivers.json` entries (`fields: ["exit_code","stdout","stderr","tree"]`, `taxonomy: "F01"`, `eval_plan_ref: "§8.3 row 1 / §2.2"`, reason naming the missing exporters). No other pack case is waived.
