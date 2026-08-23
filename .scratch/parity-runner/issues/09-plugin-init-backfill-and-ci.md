# 09 — Runner evidence backfill: `plugin init`, and CI evidence gate

**What to build:** (a) Every `plugin init --format/--claude-plugin` behaviour is re-established as runner evidence AFTER ticket 07 lands (so next-steps lines are the final ones). (b) CI runs the runner against the pinned Oracle on every push, uploads the evidence, and fails on any unwaived diff or missing self-test — replacing "go test passed" as the parity gate.

**Blocked by:** 02 — runner diff/gate; 03 — Oracle pin preflight; 04 — shared resolver; 07 — pack --format selector.

**Status:** ready-for-agent

**Oracle:** `commands/plugin/init.py`, `commands/init.py:129-475`, `commands/_helpers.py:621-662`, `bundle/formats.py`. Earlier spec stories 1–14. Evaluator guardrails: `.review/ticket-review.md` §F "06B", §E2.

## Part A — plugin init cases (`plugin-init-`)
- [ ] help
- [ ] no-flag, format-plugin, format-claude, format-claude-plugin, claude-plugin-flag — bundle of apm.yml + plugin.json compared as raw bytes (only `author`'s git-config-derived value normalised: set `GIT_CONFIG_GLOBAL` in the fixture to a file with a fixed `user.name` for both sides).
- [ ] format-agent-plugin — plugin.json ($schema first, extensions last) and mcp.json (sorted keys) raw bytes.
- [ ] normalise-upper, normalise-underscore, normalise-space (expect diffs: Oracle Click rejects, Target accepts — waived as F12 "spec-over-Oracle" with eval_plan_ref §8.3 / F12 note; these are the ONLY F12 waivers).
- [ ] conflict, empty, missing-arg, unknown, format-apm — exit 2, no project dir created (tree diff must be empty).
- [ ] existing-apmyml-yes, existing-pluginjson-only-yes, existing-mcpjson-agent-yes (warning line then overwrite line; non-TTY), existing-pluginjson-no-yes (refused, file intact).
- [ ] unicode-author (`user.name` = `名😀<`): plugin.json bytes equal (ensure_ascii).
- [ ] next-steps: the three lines appear verbatim on both sides (after 07).

## Part B — CI gate
- [ ] A GitHub Actions workflow `parity.yml` on push/PR: checks out this repo; checks out `microsoft/apm` at the SHA in `tools/parity/oracle.pin` (read the file, pass to `actions/checkout@v4 ref`); installs `uv`; `uv sync` in the Oracle; `go build -o bin/apm-go ./cmd/apm-go`; runs `go run ./tools/parity -cases tools/parity/cases -out parity-out` with `APM_TARGET_BIN=$PWD/bin/apm-go` and `APM_ORACLE_CMD="uv run --project $ORACLE_DIR apm"`.
- [ ] Uploads `parity-out/` as an artifact (retention 30 days) on success AND failure.
- [ ] The job fails when the runner exits non-zero (unwaived diff / self-test fail / preflight fail). A separate step prints `summary.txt` into the job log.
- [ ] `go test ./...` runs in the same workflow as a separate job; it is NOT the parity gate and the README/AGENTS.md say so in one sentence under Testing patterns.
- [ ] Network-free guarantee: the workflow runs the runner with no extra secrets; all cases use local fixtures; a grep in CI asserts no case `argv` contains `https://` or `github.com/` outside fixture paths.
- [ ] The earlier manual reports are annotated as superseded (same as ticket 08) for the plugin-init sections.
