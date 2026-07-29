package ux

import "charm.land/huh/v2"

// Concurrency premise (2026-07-30 codex Tier 2 M3): SetPromptSeamsForTest
// and SetTTYSeamsForTest mutate package-level vars with no locking. This is
// safe ONLY because neither internal/ux's own tests nor its consumers
// (cmd/apm-go) call t.Parallel anywhere in this repo (verified via
// `grep -rn "t.Parallel()"` across the whole tree, zero matches at the time
// of this comment) -- go test runs a package's Test* functions sequentially
// by default, so there is no concurrent access to guard against today. If a
// future test in either package introduces t.Parallel while these seams are
// in use, this premise breaks and the swap needs a mutex (or per-goroutine
// isolation) before that lands, not after a race is observed.

// SetPromptSeamsForTest overrides the confirmWith/multiSelectWith/
// inputFormWith seams that back Confirm/MultiSelect/InputForm (and Clack's
// wrappers) for the duration of a test in another package, where the real
// seams are unexported -- the same purpose SetTTYSeamsForTest serves for
// stdinIsTTY/stdoutIsTTY/stderrIsTTY.
//
// This exists so callers outside internal/ux (e.g. cmd/apm-go's init tests)
// can observe or stub the exact field/opts a caller builds -- e.g. asserting
// MultiSelect's Selected preselection, which only exists in the opts
// multiSelectWith receives, or driving `init`'s real interactiveTargetSelect
// end to end without a live terminal.
//
// A nil argument leaves that seam untouched. Returns a restore func that
// must be invoked (e.g. via t.Cleanup) to put the originals back.
func SetPromptSeamsForTest(
	confirm func(theme huh.Theme, prompt string, def bool) (bool, error),
	multiSelect func(theme huh.Theme, title, description string, showHelp bool, opts []Option) ([]string, error),
	inputForm func(theme huh.Theme, title string, showHelp bool, fields []Field) (map[string]string, error),
) (restore func()) {
	prevConfirm := confirmWith
	prevMultiSelect := multiSelectWith
	prevInputForm := inputFormWith
	if confirm != nil {
		confirmWith = confirm
	}
	if multiSelect != nil {
		multiSelectWith = multiSelect
	}
	if inputForm != nil {
		inputFormWith = inputForm
	}
	return func() {
		confirmWith = prevConfirm
		multiSelectWith = prevMultiSelect
		inputFormWith = prevInputForm
	}
}

// SetTTYSeamsForTest overrides the stdin/stdout/stderr TTY-detection seams
// that back CanPrompt and Init's richMode/styleEnabled calculation for the
// duration of a test in another package, where the real seams
// (stdinIsTTY/stdoutIsTTY/stderrIsTTY) are unexported. It returns a restore
// func that must be invoked (e.g. via t.Cleanup) to put the originals back.
//
// This exists so callers outside internal/ux (e.g. cmd/apm-go's tests) can
// drive the real ux.CanPrompt()/ux.Confirm() production code path against a
// forced TTY state, instead of only ever exercising it through a
// package-local stub of the caller's own seam (e.g. marketplace.go's
// richCheck/confirmFn), which proves nothing about whether CanPrompt's own
// TTY detection is actually wired correctly end to end.
func SetTTYSeamsForTest(stdinTTY, stdoutTTY, stderrTTY bool) (restore func()) {
	prevStdin := stdinIsTTY
	prevStdout := stdoutIsTTY
	prevStderr := stderrIsTTY
	stdinIsTTY = func() bool { return stdinTTY }
	stdoutIsTTY = func() bool { return stdoutTTY }
	stderrIsTTY = func() bool { return stderrTTY }
	return func() {
		stdinIsTTY = prevStdin
		stdoutIsTTY = prevStdout
		stderrIsTTY = prevStderr
	}
}
