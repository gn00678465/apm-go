package main

import "errors"

// exitCodeError wraps an error with a specific process exit code, so
// main()'s root.Execute() error path can exit with something other than
// the default 1 it uses for every other command's error -- mkt-045's
// "package 子指令錯誤路徑 exit code 為 2" (`apm marketplace package
// add/remove/set`'s edit-failure exit code).
type exitCodeError struct {
	code int
	err  error
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
