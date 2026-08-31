# 01 — Parity runner: isolated execution and raw capture

**What to build:** `go run ./tools/parity -cases <dir> -out <dir>` runs every case directory against the Oracle (`apm`) and the Target (`apm-go`) and writes, per side per case, a raw evidence record that a reviewer can replay by hand. Nothing is compared yet — this ticket is "capture everything, lose nothing".

**Blocked by:** None — can start immediately.

**Status:** done — e518d15, b9d5ca5, d176002, 44bc94a; verified .review/eval-ticket-01-r2.md (10/10)

**Spec:** `.review/spec-surface-gaps-and-parity-runner.md` stories 24–28, 34. Evaluator guardrails: `.review/ticket-review.md` §F "01A".

## Case manifest (the seam)

One directory per case under `<cases>/`:

- `case.json`: `{ "id", "argv": [...], "stdin": "", "env": {}, "rewrite_binary_name": false, "expected_taxonomy": ["F08"], "waiver": "" }`
- optional `fixture/` tree copied into the run cwd before execution.

`argv` is the tail after the binary name. Oracle command comes from `APM_ORACLE_CMD` (default `uv run --project /home/madao/projects/apm-mesh/apm apm`); Target from `APM_TARGET_BIN` (default `./bin/apm-go`).

## Acceptance criteria

- [ ] Each run gets its own fresh temp `cwd` (fixture materialised into it), temp `APM_CONFIG_DIR`, temp `HOME`. The runner never reads or writes the invoking user's real config; a test proves the real `~/.apm` is untouched after a run.
- [ ] Environment is an allow-list (`PATH`, `LANG`, `LC_ALL`) plus fixed `NO_COLOR=1 CI=1 TERM=dumb` plus the runner's own isolation vars (`HOME`, `APM_CONFIG_DIR`) plus `case.env` overrides. `env_delta` in evidence records EVERYTHING passed, including the isolation vars (they are evidence, not noise). A test asserts the key set is exactly {allow-list ∩ present} ∪ fixed ∪ isolation ∪ case.env — nothing else from the invoking shell leaks.
- [ ] stdin comes from `case.stdin`; the process has no TTY on any fd.
- [ ] 60s timeout kills the whole process group; partial stdout/stderr captured so far is still written, and the record is marked `timed_out: true`.
- [ ] Per side per case, `<out>/<side>/<id>/record.json` contains: `id`, full `argv` (binary + tail), `env_delta`, `exit_code`, `timed_out`, raw `stdout`, raw `stderr` — byte-exact, including non-UTF-8 bytes: `stdout`/`stderr` are ALWAYS written to `<out>/<side>/<id>/stdout.bin` / `stderr.bin` as raw bytes, and `record.json` carries `stdout_sha256`/`stderr_sha256`, `stdout_bytes`/`stderr_bytes` (length), and an inline `stdout`/`stderr` string ONLY when the bytes are valid UTF-8 and ≤ 64 KiB (else the field is omitted and consumers read the `.bin`). A test pipes `\xff\n` through a stub and asserts the `.bin` is `ff 0a` and the sha matches, and `tree`: a sorted list of `{path, kind(file|dir|symlink), size, sha256}` for everything under the run cwd, `APM_CONFIG_DIR`, AND the isolated `HOME` (amended in ticket 02 attempt 3: the Oracle ignores `APM_CONFIG_DIR` and writes to `$HOME/.apm/`) after the run, INCLUDING files that were in the fixture and are now gone (`kind: "deleted"`).
- [ ] Raw bytes of every file under cwd and `APM_CONFIG_DIR` after the run are copied to `<out>/<side>/<id>/fs/…` preserving relative paths. Empty files are copied too.
- [ ] `<out>/<side>.jsonl` has one line per case = the record without `stdout`/`stderr` bodies inlined when they exceed 64 KiB (then they are referenced by path). Never truncated silently.
- [ ] A header `<out>/run.json` records: timestamp, `APM_ORACLE_CMD`, `APM_TARGET_BIN`, and each binary's `--version` stdout.
- [ ] Unit tests cover fixture materialisation, env allow-list, tree listing with a deleted file, and timeout partial capture, using a stub shell script as both "binaries". `go test` never invokes the real Oracle.
- [ ] No normalisation, diffing, or gating in this ticket. Exit status of the runner is 0 if every case ran (even if it exited non-zero), 1 only on runner infrastructure failure.
