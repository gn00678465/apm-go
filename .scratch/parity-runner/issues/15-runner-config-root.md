# 15 — Runner: stop injecting `APM_CONFIG_DIR`; registry root = sandbox `$HOME/.apm` on both sides

**What to build:** The runner no longer forces `APM_CONFIG_DIR` into the child env. Both CLIs then store their marketplace registry under the isolated `$HOME/.apm/` (apm-go already falls back there when the var is unset, `internal/marketplace/registry.go:108-121`), so the 9 `tree` diffs that only reflect the runner's own env choice disappear — and apm-go's explicit-override behaviour is verified by one dedicated case instead.

**Blocked by:** 12 — baseline.json (so the remaining tree diffs are known to be registry-only).

**Status:** ready-for-agent

**Origin:** `.review/eval-ticket-12.md` §5: the 9 unwaived registry `tree` diffs are `home/.apm/marketplaces.json` (Oracle) vs `config/marketplaces.json` (Target), caused by `tools/parity/env.go:48-50` injecting `APM_CONFIG_DIR`. Taxonomy F04.

## Acceptance criteria

- [ ] `buildEnv` no longer sets `APM_CONFIG_DIR`; the isolation-var set becomes `HOME` + `UV_CACHE_DIR`. A case may still set `APM_CONFIG_DIR` via `case.env` (it is no longer a protected key) — tests updated accordingly.
- [ ] Sandbox keeps a `config/` dir only if some case sets `APM_CONFIG_DIR` to it; evidence roots are cwd ∪ HOME (drop the `config` root when unused; `sandboxPathsFromEnvDelta` tolerates its absence).
- [ ] Ticket 01 AC1 ("real `~/.apm` untouched") still holds because `HOME` is isolated — re-run that test.
- [ ] New case `registry-explicit-config-dir`: `env: {"APM_CONFIG_DIR": "<sandbox cwd>/altcfg"}` (runner expands `<TMP>` in case.env values — add that), `setup_argv` does `marketplace add ./fixture --name skills`, `argv` is `marketplace list`. Expected and recorded: Target writes `cwd/altcfg/marketplaces.json`; Oracle ignores the var and writes `home/.apm/marketplaces.json`. This ONE case carries a path-precise `tree` waiver (both paths listed, taxonomy F04, reason: "apm-go honours APM_CONFIG_DIR as an explicit override; the Oracle has no such variable — intentional environment-semantics extension, spec §Out of Scope to add") so the extension is evidence, not hidden.
- [ ] All `search-*`, `validate-*`, `browse-*`, `list-*` cases show NO `tree` diff after this change; `browse-unknown-marketplace` / `list-empty` drop their registry `tree` waivers.
- [ ] `waivers.json` and `baseline.json` validation unchanged; self-test still passes.
