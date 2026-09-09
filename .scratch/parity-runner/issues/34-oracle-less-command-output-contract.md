# Ticket 34 — output contract for apm-go-only commands with no Oracle counterpart

Status: RULED (2026-09-09)

## What

`apm-go plugin validate` (mission `plugin-manifest-validate-01M21E5Q`, issue #13)
has no counterpart in the pinned Oracle (`b75a02b1`, v0.29.0) nor on upstream
`main`: `src/apm_cli/commands/plugin/` contains only `init.py`. The parity
runner compares apm-go against Oracle output, so no corpus case can exist for
this command; a waiver cannot apply (nothing to waive against) and a pending
case would misstate the situation (pending means "the Oracle differs", not
"the Oracle has nothing").

## Ruling

User ruling, 2026-09-09, quoted verbatim from the mission conversation:

> A

where option A was: 「無 Oracle 對應的 apm-go 獨有指令，輸出契約以
`tools/gate/realexec.sh` 固定，不進 parity corpus；裁定原文寫進 PR 描述，並在
`.scratch/parity-runner/issues/` 開一張 ticket 記錄；WP04 的 realexec／mutants
維持。」

This is the charter Exception Policy's third mechanism for the class of
commands the Oracle does not have. It applies only to that class; every command
the Oracle does have keeps the waiver / pending-case rule unchanged.

## Why

The charter's output-contract gate exists to keep apm-go byte-compatible with
the Oracle. For a command the Oracle lacks, the compatible thing to protect is
apm-go's own stated contract (`kitty-specs/plugin-manifest-validate-01M21E5Q/contracts/cli-plugin-validate.md`).
`tools/gate/realexec.sh` already pins stdout substrings, exit codes, and a
read-only `cmp` for the plugin/marketplace commands; extending it is the
mechanism that actually executes the binary on the contract's inputs.

## Acceptance criteria

- [ ] `cmd/apm-go/plugin.go` records the deviation at the command site: no
  Oracle counterpart, contract pinned by `tools/gate/realexec.sh`, this ticket.
- [ ] `tools/gate/realexec.sh` gains the `plugin validate` happy and adversarial
  steps listed in WP04 (stdout substrings, exit codes, `cmp` before/after).
- [ ] The PR description quotes this ruling.
- [ ] Existing parity corpus stays at 96 cases / 0 unwaived.
