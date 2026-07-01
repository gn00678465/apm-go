# Experimental Feature Flags

Convention for gating opt-in / unstable features in apm-go. Established by the
registry HTTP consumer (task 07-01-registry-consumer), mirroring the original
apm-cli `apm experimental` subsystem.

## Mechanism
- `internal/experimental`: static flag registry + `IsEnabled/Enable/Disable/RequireEnabled/All`.
- Persisted to `$APM_CONFIG_DIR/config.json` (default `~/.apm/config.json`). The env
  override makes the store test-isolatable (`t.Setenv("APM_CONFIG_DIR", t.TempDir())`).
- CLI: `apm experimental list|enable|disable <flag>`. Missing/corrupt config reads as
  disabled; unknown flags error.

## Two hard rules

1. **Never gate a security control.** Flags gate feature AVAILABILITY only. When a flag
   is on, every hash-verify, credential gate, redaction, and safe-extract still runs.
   (Same invariant as the original apm-cli.) `~/.apm/config.json` is user-writable and
   carries only user-equivalent trust.

2. **Never gate conformance-graded behavior — gate the smallest runtime boundary.**
   apm-go is graded against the read-only OpenAPM v0.1 oracle. Gate ONLY the new,
   not-yet-in-oracle runtime (e.g. live registry HTTP: `/versions`+`/download`, frozen
   network replay). Do NOT gate anything the oracle requires unconditionally — manifest
   parsing, lockfile schema, or offline integrity paths. Gating a graded path fails
   conformance even though it matches the original's coarser gate. This still gives the
   same user-visible behavior (feature refused until enabled).

## Verify
A gated feature needs both: a negative test (refused with the enable hint when off) and
proof the oracle-required paths stay green with the flag defaulting off (`go test ./...`
with an empty `APM_CONFIG_DIR`).
