package main

import (
	"errors"
	"testing"
)

func TestWithExitCode_NilErrPassesThrough(t *testing.T) {
	if err := withExitCode(2, nil); err != nil {
		t.Fatalf("withExitCode(2, nil) = %v, want nil", err)
	}
	if err := withSilentExitCode(2, nil); err != nil {
		t.Fatalf("withSilentExitCode(2, nil) = %v, want nil", err)
	}
}

func TestExitCodeOf(t *testing.T) {
	if got := exitCodeOf(nil); got != 0 {
		t.Errorf("exitCodeOf(nil) = %d, want 0", got)
	}
	if got := exitCodeOf(errors.New("plain")); got != 1 {
		t.Errorf("exitCodeOf(plain) = %d, want 1", got)
	}
	if got := exitCodeOf(withExitCode(2, errors.New("x"))); got != 2 {
		t.Errorf("exitCodeOf(withExitCode(2,...)) = %d, want 2", got)
	}
	if got := exitCodeOf(withSilentExitCode(1, errors.New("x"))); got != 1 {
		t.Errorf("exitCodeOf(withSilentExitCode(1,...)) = %d, want 1", got)
	}
}

// isSilentExit is main()'s root.Execute() error-branch signal (ticket 08):
// a silent error must still exit with its code but never trigger
// ux.Error's extra "[x] <message>" print.
func TestIsSilentExit(t *testing.T) {
	if isSilentExit(errors.New("plain")) {
		t.Error("a plain error must not be silent")
	}
	if isSilentExit(withExitCode(1, errors.New("x"))) {
		t.Error("withExitCode must not be silent")
	}
	if !isSilentExit(withSilentExitCode(1, errors.New("x"))) {
		t.Error("withSilentExitCode must be silent")
	}
	if isSilentExit(nil) {
		t.Error("nil must not be silent")
	}
}
