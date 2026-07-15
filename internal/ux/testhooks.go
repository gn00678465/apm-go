package ux

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
