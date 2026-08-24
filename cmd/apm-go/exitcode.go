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
