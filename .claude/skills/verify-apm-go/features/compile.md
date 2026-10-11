# Compile instructions

`compile` collects the instruction files under `.apm/instructions/` (the project's own and those of installed dependencies) and writes them into one `AGENTS.md` at the project root. Running it again with nothing changed leaves the file as it is.

## Sub-features

- `compile-write` writes `AGENTS.md` from one instruction.
- `compile-idempotent` leaves `AGENTS.md` untouched on a second run.
- `compile-empty` fails when the project has no instruction.

## How to get to it (user POV)

- Run `apm-go compile` in a project whose `apm.yml` targets an agents-family platform.
- Run `apm-go compile --target <antigravity|codex|opencode>` to name the target.

## Driving it with verify.sh

Preconditions:

- `sandbox/work/comp` holds a copy of `tools/parity/cases-pending/compile-instruction/fixture/` (its `apm.yml` has `target: codex`; it has one file, `.apm/instructions/example.instructions.md`).
- `sandbox/work/empty` holds only an `apm.yml` with `name: e` and `version: 1.0.0`.

- **compile-write.** Run `$V run RUN compile --cd comp -- compile`. Exit code `0`. stdout is `  - .apm/instructions/example.instructions.md` and ` + Compiled 1 instruction(s) to AGENTS.md`. The tree gains exactly `./work/comp/AGENTS.md`. The file starts with `# AGENTS.md`, has a `<!-- Build ID: … -->` comment, and contains the body of the instruction (`Use the example.`).
- **compile-idempotent.** Run `$V run RUN compile-again --cd comp -- compile`. Exit code `0`. stdout is ` i No changes detected; preserving existing AGENTS.md for idempotency`. `diff RUN/evidence/compile.tree RUN/evidence/compile-again.tree` is empty.
- **compile-empty.** Run `$V run RUN compile-empty --cd empty -- compile`. Exit code `1`. stdout has ` x No instruction files found in .apm/ directory`. The tree gains no `AGENTS.md` under `empty/`.
- **Proof.** The three `.out` / `.rc` pairs, `AGENTS.md` read from the sandbox, and the empty tree diff of `compile-idempotent`.

## Gotchas

- An identical tree proves the bytes did not change, not that the file was not rewritten. To prove no rewrite, compare the file's modification time before and after (`stat -c %Y`).
- The exact text of `AGENTS.md` (header comments, `Source` markers, footer) is ruled to change to the Oracle's form by issue #50. Check for the instruction body and the `Build ID` comment, not the whole file, until that lands; then update this file.
- `compile-empty` prints the error line twice, once capitalised and once not. Do not count error lines.
- `compile` handles the agents-family targets only (`antigravity`, `codex`, `opencode`). A project whose only target is `claude` is outside this feature.
