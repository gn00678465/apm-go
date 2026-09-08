package main

import "errors"

// exitCodeError wraps an error with a specific process exit code, so
// main()'s root.Execute() error path can exit with something other than
// the default 1 it uses for every other command's error -- mkt-045's
// "package 子指令錯誤路徑 exit code 為 2" (`apm marketplace package
// add/remove/set`'s edit-failure exit code).
type exitCodeError struct {
	code   int
	err    error
	silent bool
	usage  bool
	bare   bool
	stderr bool
}

func (e *exitCodeError) Error() string { return e.err.Error() }
func (e *exitCodeError) Unwrap() error { return e.err }

// withExitCode wraps err so main()'s root.Execute() error path exits with
// code instead of the default 1. Returns nil unchanged.
func withExitCode(code int, err error) error {
	if err == nil {
		return nil
	}
	return &exitCodeError{code: code, err: err}
}

// withSilentExitCode is withExitCode for a command whose own output already
// told the user everything (doctor's table, e.g.) -- main()'s
// root.Execute() error branch skips its usual ux.Error("[x] %s", err) print
// for this error, printing nothing extra, while still exiting with code.
// This mirrors run_doctor's own contract (commands/doctor.py:26-30):
// `sys.exit(exit_code)` with no additional message, ever -- a plain
// withExitCode here would otherwise put an oracle-side stdout the Oracle
// never prints (ticket 08 investigation: every failing-critical-check
// doctor-* case showed apm-go appending "[x] critical environment check
// failed" that the pinned Oracle has no equivalent of).
func withSilentExitCode(code int, err error) error {
	if err == nil {
		return nil
	}
	return &exitCodeError{code: code, err: err, silent: true}
}

// isSilentExit reports whether err requests the silent treatment
// withSilentExitCode grants: main()'s root.Execute() error branch must
// still exit with its code, just without printing anything first.
func isSilentExit(err error) bool {
	var ec *exitCodeError
	return errors.As(err, &ec) && ec.silent
}

// withUsageError wraps err as a genuine Click UsageError equivalent (ticket
// 13, decision recorded in .scratch/parity-runner/issues/10-error-output-
// contract.md): a CLI-level mistake -- a bad/missing flag value, mutually
// exclusive selectors -- that the Oracle's OWN Click framework renders via
// UsageError.show(), completely bypassing the Oracle's custom
// CommandLogger/_rich_error console redirection ticket 10's decision (A)
// covers. Verified directly against the pinned Oracle for pack's
// --format conflict/empty/unknown and missing-argument cases: ALL FOUR
// land on STDERR (not decision (A)'s stdout) with a plain "Error: "
// prefix (not "[x] ") preceded by a "Usage: ...\nTry '... --help' for
// help.\n\n" block -- a narrower, Click-native contract distinct from
// ordinary runtime errors. main()'s root.Execute() error branch renders
// this instead of the usual ux.Error call when isUsageError is true.
//
// Deliberately NOT applied to every existing withExitCode(2, ...) call
// site: several (audit --content's warning-count gate, compile's
// target-not-implemented message, install's structured no-deploy-target
// teaching block, marketplace package add/set/remove's mkt-045 edit-
// failure convention) reuse exit code 2 for apm-go's own domain-specific
// reasons that do NOT correspond to an observed Oracle click.UsageError
// for that same operation -- some print their own error text directly
// already, and blindly adding this boilerplate to them would double-print
// or wrap a message that was never a CLI-usage mistake on the Oracle side.
// Scoped here to exactly what was verified: the shared --format/
// --claude-plugin selector (bundle_format.go), used by both `pack` and
// `plugin init`.
func withUsageError(err error) error {
	if err == nil {
		return nil
	}
	return &exitCodeError{code: 2, err: err, usage: true}
}

// withBareUsageError is withUsageError WITHOUT the "Usage: .../Try '...'
// for help." preamble -- verified directly against the pinned Oracle:
// `pack --format` (the flag present with no value at all) prints ONLY
// "Error: Option '--format' requires an argument." on stderr, no preamble,
// while every OTHER usage error from the same --format selector (a
// conflicting selector, an empty or unknown --format=VALUE) prints the
// full preamble. Click's parser raises this specific "missing option
// argument" error before a Context exists to render `ctx.get_usage()`
// from, unlike the other cases (which fail during/after value coercion,
// with a Context already in hand) -- this is a genuine, narrower Click
// behavior, not an inconsistency to normalize away.
func withBareUsageError(err error) error {
	if err == nil {
		return nil
	}
	return &exitCodeError{code: 2, err: err, usage: true, bare: true}
}

// withStderrError marks a producer/configuration error whose Oracle contract
// is a bare "Error: ..." line on stderr without Click's usage preamble. The
// marketplace producer uses this for the BuildError path corresponding to
// core/build_orchestrator.py:168 and commands/marketplace/__init__.py:165.
func withStderrError(err error) error {
	if err == nil {
		return nil
	}
	return &exitCodeError{code: 1, err: err, stderr: true}
}

func isStderrError(err error) bool {
	var ec *exitCodeError
	return errors.As(err, &ec) && ec.stderr
}

// isUsageError reports whether err requests withUsageError's Click-native
// rendering (Usage/Try-help block, stderr, plain "Error: " prefix) instead
// of the ordinary ux.Error("[x] %s", err) path.
func isUsageError(err error) bool {
	var ec *exitCodeError
	return errors.As(err, &ec) && ec.usage
}

// isBareUsageError reports whether err requests withBareUsageError's
// preamble-free rendering (see its own doc comment).
func isBareUsageError(err error) bool {
	var ec *exitCodeError
	return errors.As(err, &ec) && ec.usage && ec.bare
}

// exitCodeOf returns the process exit code err requests via withExitCode,
// the default 1 for any other non-nil error, or 0 for a nil error.
func exitCodeOf(err error) int {
	if err == nil {
		return 0
	}
	var ec *exitCodeError
	if errors.As(err, &ec) {
		return ec.code
	}
	return 1
}
