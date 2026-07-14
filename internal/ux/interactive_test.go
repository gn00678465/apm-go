package ux

import (
	"errors"
	"io"
	"testing"
	"time"

	"charm.land/huh/v2"
	"github.com/pterm/pterm"
)

// stubRunField replaces the huh field-execution seam with fn for the
// duration of the test, so rich-mode field construction can be exercised
// without a real interactive terminal.
func stubRunField(t *testing.T, fn func(huh.Field) error) {
	t.Helper()
	prev := runField
	runField = fn
	t.Cleanup(func() { runField = prev })
}

// setRichMode overrides the package's richMode decision directly (bypassing
// Init/TTY detection), and forces CanPrompt() to the same boolean via the
// stdinIsTTY/stderrIsTTY seams (plus clearing CI, which CanPrompt also
// checks), so interactive-function tests are deterministic and never depend
// on the test runner's actual stdin/stderr/environment. It also overrides
// the spinnerIsRich seam the same way, so Spin tests can exercise the live
// pterm.SpinnerPrinter lifecycle without a real terminal writer, and keeps
// pterm's global styling state in sync, mirroring what Init()/Spinner() do
// in production.
func setRichMode(t *testing.T, rich bool) {
	t.Helper()
	prevRich := richMode
	prevSpinnerIsRich := spinnerIsRich
	prevRaw := pterm.RawOutput
	richMode = rich
	spinnerIsRich = func(io.Writer) bool { return rich }
	withStdinTTY(t, rich)
	withStderrTTY(t, rich)
	t.Setenv("CI", "")
	if rich {
		pterm.EnableStyling()
	} else {
		pterm.DisableStyling()
	}
	t.Cleanup(func() {
		richMode = prevRich
		spinnerIsRich = prevSpinnerIsRich
		if prevRaw {
			pterm.DisableStyling()
		} else {
			pterm.EnableStyling()
		}
	})
}

// runWithTimeout fails the test if fn does not return within the given
// duration, guarding against accidentally invoking a blocking huh form.
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
		t.Fatal("function did not return within timeout; it likely blocked on a huh prompt")
	}
}

func TestConfirm_NonTTYReturnsDefaultWithoutBlocking(t *testing.T) {
	// Arrange
	setRichMode(t, false)
	var got bool
	var err error

	// Act
	runWithTimeout(t, time.Second, func() {
		got, err = Confirm("proceed?", true)
	})

	// Assert
	if err != nil {
		t.Fatalf("Confirm() err = %v, want nil", err)
	}
	if got != true {
		t.Fatalf("Confirm() = %v, want default true", got)
	}
}

func TestInputText_NonTTYReturnsDefaultWithoutBlocking(t *testing.T) {
	// Arrange
	setRichMode(t, false)
	var got string
	var err error

	// Act
	runWithTimeout(t, time.Second, func() {
		got, err = InputText("name", "my-plugin")
	})

	// Assert
	if err != nil {
		t.Fatalf("InputText() err = %v, want nil", err)
	}
	if got != "my-plugin" {
		t.Fatalf("InputText() = %q, want %q", got, "my-plugin")
	}
}

func TestPassword_NonTTYReturnsEmptyWithoutBlocking(t *testing.T) {
	// Arrange
	setRichMode(t, false)
	var got string
	var err error

	// Act
	runWithTimeout(t, time.Second, func() {
		got, err = Password("token")
	})

	// Assert
	if err != nil {
		t.Fatalf("Password() err = %v, want nil", err)
	}
	if got != "" {
		t.Fatalf("Password() = %q, want empty string", got)
	}
}

func TestMultiSelect_NonTTYReturnsSelectedDefaultsWithoutBlocking(t *testing.T) {
	// Arrange
	setRichMode(t, false)
	opts := []Option{
		{Label: "claude", Value: "claude", Selected: true},
		{Label: "cursor", Value: "cursor", Selected: false},
		{Label: "codex", Value: "codex", Selected: true},
	}
	var got []string
	var err error

	// Act
	runWithTimeout(t, time.Second, func() {
		got, err = MultiSelect("targets", opts)
	})

	// Assert
	if err != nil {
		t.Fatalf("MultiSelect() err = %v, want nil", err)
	}
	want := []string{"claude", "codex"}
	if len(got) != len(want) {
		t.Fatalf("MultiSelect() = %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("MultiSelect() = %v, want %v", got, want)
		}
	}
}

func TestMultiSelect_NonTTYReturnsEmptyWhenNoneSelected(t *testing.T) {
	// Arrange
	setRichMode(t, false)
	opts := []Option{
		{Label: "claude", Value: "claude"},
		{Label: "cursor", Value: "cursor"},
	}

	// Act
	got, err := MultiSelect("targets", opts)

	// Assert
	if err != nil {
		t.Fatalf("MultiSelect() err = %v, want nil", err)
	}
	if len(got) != 0 {
		t.Fatalf("MultiSelect() = %v, want empty", got)
	}
}

func TestConfirm_RichModeBuildsFieldAndPropagatesRunResult(t *testing.T) {
	// Arrange
	setRichMode(t, true)
	stubRunField(t, func(huh.Field) error { return nil })

	// Act
	got, err := Confirm("proceed?", true)

	// Assert
	if err != nil {
		t.Fatalf("Confirm() err = %v, want nil", err)
	}
	if got != true {
		t.Fatalf("Confirm() = %v, want true", got)
	}
}

func TestConfirm_RichModePropagatesRunError(t *testing.T) {
	// Arrange
	setRichMode(t, true)
	wantErr := errors.New("boom")
	stubRunField(t, func(huh.Field) error { return wantErr })

	// Act
	_, err := Confirm("proceed?", false)

	// Assert
	if !errors.Is(err, wantErr) {
		t.Fatalf("Confirm() err = %v, want %v", err, wantErr)
	}
}

func TestInputText_RichModeBuildsFieldAndPropagatesRunResult(t *testing.T) {
	// Arrange
	setRichMode(t, true)
	stubRunField(t, func(huh.Field) error { return nil })

	// Act
	got, err := InputText("name", "my-plugin")

	// Assert
	if err != nil {
		t.Fatalf("InputText() err = %v, want nil", err)
	}
	if got != "my-plugin" {
		t.Fatalf("InputText() = %q, want %q", got, "my-plugin")
	}
}

func TestPassword_RichModeBuildsFieldAndPropagatesRunResult(t *testing.T) {
	// Arrange
	setRichMode(t, true)
	stubRunField(t, func(huh.Field) error { return nil })

	// Act
	got, err := Password("token")

	// Assert
	if err != nil {
		t.Fatalf("Password() err = %v, want nil", err)
	}
	if got != "" {
		t.Fatalf("Password() = %q, want empty (stub never sets a value)", got)
	}
}

func TestMultiSelect_RichModePropagatesRunError(t *testing.T) {
	// Arrange
	setRichMode(t, true)
	wantErr := errors.New("aborted")
	stubRunField(t, func(huh.Field) error { return wantErr })
	opts := []Option{{Label: "claude", Value: "claude", Selected: true}}

	// Act
	_, err := MultiSelect("targets", opts)

	// Assert
	if !errors.Is(err, wantErr) {
		t.Fatalf("MultiSelect() err = %v, want %v", err, wantErr)
	}
}

func TestMultiSelect_RichModeBuildsOptionsAndPropagatesRunResult(t *testing.T) {
	// Arrange
	setRichMode(t, true)
	stubRunField(t, func(huh.Field) error { return nil })
	opts := []Option{
		{Label: "claude", Value: "claude", Selected: true},
		{Label: "cursor", Value: "cursor"},
	}

	// Act
	got, err := MultiSelect("targets", opts)

	// Assert
	if err != nil {
		t.Fatalf("MultiSelect() err = %v, want nil", err)
	}
	// The stub never mutates the bound value, so it stays at its zero value.
	if len(got) != 0 {
		t.Fatalf("MultiSelect() = %v, want empty (stub never sets a value)", got)
	}
}
