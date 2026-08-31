package main

import (
	"io"
	"testing"
	"time"

	"github.com/apm-go/apm/internal/manifest"
	"github.com/apm-go/apm/internal/ux"
)

// TestTargetSelectOptions_ExcludesExplicitOnly is the targetSelectOptions
// half of the 2026-08-02 explicit-only-targets parity fix (Python apm_cli
// core/target_detection.py:430-431 / commands/init.py:629, v0.26.0):
// antigravity and agent-skills must never appear in the init/plugin-init
// MultiSelect option set, even when pre-selected via existing apm.yml
// targets or filesystem auto-detection (matching upstream's
// _prompt_target_selection, which builds its menu from
// `_PROMPT_TARGETS_ORDERED` filtered by `EXPLICIT_ONLY_TARGETS` before ever
// looking at prechecked state).
func TestTargetSelectOptions_ExcludesExplicitOnly(t *testing.T) {
	for explicitOnly := range manifest.ExplicitOnlyTargets {
		t.Run(explicitOnly, func(t *testing.T) {
			opts := targetSelectOptions([]string{explicitOnly}, []string{explicitOnly})
			for _, o := range opts {
				if o.Value == explicitOnly {
					t.Fatalf("targetSelectOptions offered explicit-only target %q even though it was detected/existing", explicitOnly)
				}
			}
		})
	}

	opts := targetSelectOptions(nil, nil)
	if len(opts) != len(manifest.PromptTargets) {
		t.Fatalf("targetSelectOptions(nil, nil) returned %d options, want %d (len(manifest.PromptTargets))",
			len(opts), len(manifest.PromptTargets))
	}
}

// runWithTimeout fails the test if fn does not return within d, guarding
// against interactiveTargetSelect accidentally looping forever (the HIGH #1
// regression this file exists to cover: a swallowed MultiSelect/Confirm
// error used to leave `selected` nil and `cont` at its zero value (false),
// which re-entered interactiveTargetSelect recursively on every aborted
// prompt with no way out).
func runWithTimeout(t *testing.T, d time.Duration, fn func()) {
	t.Helper()
	done := make(chan struct{})
	go func() {
		fn()
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(d):
		t.Fatal("function did not return within timeout; it likely looped or blocked on a prompt")
	}
}

// TestInteractiveTargetSelect_NonTTY_ReturnsDetectedDefaultsWithoutError
// proves interactiveTargetSelect's new (loop-based, error-returning) shape
// still behaves like a plain default lookup when prompting isn't possible:
// ux.MultiSelect returns the pre-selected (detected) values immediately, so
// the function returns on its first pass with no error.
func TestInteractiveTargetSelect_NonTTY_ReturnsDetectedDefaultsWithoutError(t *testing.T) {
	// Arrange
	restore := ux.SetTTYSeamsForTest(false, false, false)
	t.Cleanup(restore)

	// Act
	var got []string
	var err error
	runWithTimeout(t, time.Second, func() {
		got, err = interactiveTargetSelect(ux.NewClack(io.Discard), []string{"claude"}, nil)
	})

	// Assert
	if err != nil {
		t.Fatalf("interactiveTargetSelect() err = %v, want nil", err)
	}
	if len(got) != 1 || got[0] != "claude" {
		t.Fatalf("interactiveTargetSelect() = %v, want [claude]", got)
	}
}

// TestInteractiveTargetSelect_NonTTY_NoDetectedTargets_ContinuesWithoutPinning
// exercises the "nothing selected" branch (no detected/existing targets) with
// prompting unavailable: ux.MultiSelect returns an empty selection and
// ux.Confirm returns its default (true, "continue without pinning") without
// blocking, so the function must return (nil, nil) on its first pass rather
// than looping.
func TestInteractiveTargetSelect_NonTTY_NoDetectedTargets_ContinuesWithoutPinning(t *testing.T) {
	// Arrange
	restore := ux.SetTTYSeamsForTest(false, false, false)
	t.Cleanup(restore)

	// Act
	var got []string
	var err error
	runWithTimeout(t, time.Second, func() {
		got, err = interactiveTargetSelect(ux.NewClack(io.Discard), nil, nil)
	})

	// Assert
	if err != nil {
		t.Fatalf("interactiveTargetSelect() err = %v, want nil", err)
	}
	if len(got) != 0 {
		t.Fatalf("interactiveTargetSelect() = %v, want empty", got)
	}
}
