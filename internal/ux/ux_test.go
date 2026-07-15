package ux

import (
	"testing"

	"github.com/pterm/pterm"
)

// withStdinTTY overrides the stdin-is-a-TTY seam for the duration of the
// test, restoring the previous implementation afterward.
func withStdinTTY(t *testing.T, tty bool) {
	t.Helper()
	prev := stdinIsTTY
	stdinIsTTY = func() bool { return tty }
	t.Cleanup(func() { stdinIsTTY = prev })
}

// withStdoutTTY overrides the stdout-is-a-TTY seam for the duration of the
// test, restoring the previous implementation afterward.
func withStdoutTTY(t *testing.T, tty bool) {
	t.Helper()
	prev := stdoutIsTTY
	stdoutIsTTY = func() bool { return tty }
	t.Cleanup(func() { stdoutIsTTY = prev })
}

// withStderrTTY overrides the stderr-is-a-TTY seam for the duration of the
// test, restoring the previous implementation afterward.
func withStderrTTY(t *testing.T, tty bool) {
	t.Helper()
	prev := stderrIsTTY
	stderrIsTTY = func() bool { return tty }
	t.Cleanup(func() { stderrIsTTY = prev })
}

// TestInit_StyleEnabledDecision proves styleEnabled (the process-wide
// styling flag pterm's own output rendering is governed by, see ux.go) is
// only true when *both* stdout and stderr are real terminals -- unlike
// IsRich()/richMode, which only checks stdin+stderr. This "both-TTY" rule is
// what lets output functions (Success/Info/Warn/Error/Table/...) call
// pterm's native WithWriter(w).Printfln/Render directly without any
// per-writer stripping: if either decorated stream is redirected, styling is
// off everywhere instead of leaking ANSI into whichever stream is still a
// terminal.
func TestInit_StyleEnabledDecision(t *testing.T) {
	tests := []struct {
		name        string
		stdoutTTY   bool
		stderrTTY   bool
		noColor     string
		ci          string
		wantEnabled bool
	}{
		{name: "stdout+stderr tty, no NO_COLOR, no CI", stdoutTTY: true, stderrTTY: true, wantEnabled: true},
		{name: "non-tty stdout", stdoutTTY: false, stderrTTY: true, wantEnabled: false},
		{name: "non-tty stderr", stdoutTTY: true, stderrTTY: false, wantEnabled: false},
		{name: "tty but NO_COLOR set", stdoutTTY: true, stderrTTY: true, noColor: "1", wantEnabled: false},
		{name: "tty but CI set", stdoutTTY: true, stderrTTY: true, ci: "true", wantEnabled: false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Arrange
			withStdoutTTY(t, tt.stdoutTTY)
			withStderrTTY(t, tt.stderrTTY)
			t.Setenv("NO_COLOR", tt.noColor)
			t.Setenv("CI", tt.ci)

			// Act
			Init()

			// Assert
			if styleEnabled != tt.wantEnabled {
				t.Fatalf("styleEnabled = %v, want %v", styleEnabled, tt.wantEnabled)
			}
			if pterm.RawOutput == tt.wantEnabled {
				t.Fatalf("pterm.RawOutput = %v, want %v (RawOutput is the inverse of styling-enabled)", pterm.RawOutput, !tt.wantEnabled)
			}
		})
	}
}

func TestInit_RichModeDecision(t *testing.T) {
	tests := []struct {
		name      string
		stdinTTY  bool
		stderrTTY bool
		noColor   string
		ci        string
		wantRich  bool
	}{
		{name: "stdin+stderr tty, no NO_COLOR, no CI", stdinTTY: true, stderrTTY: true, wantRich: true},
		{name: "non-tty stdin", stdinTTY: false, stderrTTY: true, wantRich: false},
		{name: "non-tty stderr", stdinTTY: true, stderrTTY: false, wantRich: false},
		{name: "tty but NO_COLOR set", stdinTTY: true, stderrTTY: true, noColor: "1", wantRich: false},
		{name: "tty but CI set", stdinTTY: true, stderrTTY: true, ci: "true", wantRich: false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Arrange
			withStdinTTY(t, tt.stdinTTY)
			withStderrTTY(t, tt.stderrTTY)
			t.Setenv("NO_COLOR", tt.noColor)
			t.Setenv("CI", tt.ci)

			// Act
			Init()

			// Assert
			if got := IsRich(); got != tt.wantRich {
				t.Fatalf("IsRich() = %v, want %v", got, tt.wantRich)
			}
		})
	}
}

// TestCanPrompt_IgnoresNoColor proves CanPrompt's key difference from
// IsRich: a real TTY session with NO_COLOR set can still prompt (NO_COLOR
// only means "don't colorize", not "don't ask questions").
func TestCanPrompt_IgnoresNoColor(t *testing.T) {
	tests := []struct {
		name         string
		stdinTTY     bool
		stderrTTY    bool
		noColor      string
		ci           string
		wantCanPromp bool
	}{
		{name: "stdin+stderr tty, no NO_COLOR, no CI", stdinTTY: true, stderrTTY: true, wantCanPromp: true},
		{name: "stdin+stderr tty, NO_COLOR set", stdinTTY: true, stderrTTY: true, noColor: "1", wantCanPromp: true},
		{name: "non-tty stdin", stdinTTY: false, stderrTTY: true, wantCanPromp: false},
		{name: "non-tty stderr", stdinTTY: true, stderrTTY: false, wantCanPromp: false},
		{name: "tty but CI set", stdinTTY: true, stderrTTY: true, ci: "true", wantCanPromp: false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Arrange
			withStdinTTY(t, tt.stdinTTY)
			withStderrTTY(t, tt.stderrTTY)
			t.Setenv("NO_COLOR", tt.noColor)
			t.Setenv("CI", tt.ci)

			// Act
			got := CanPrompt()

			// Assert
			if got != tt.wantCanPromp {
				t.Fatalf("CanPrompt() = %v, want %v", got, tt.wantCanPromp)
			}
		})
	}
}

func TestIsCI_DetectsCommonCIEnvVars(t *testing.T) {
	tests := []struct {
		name string
		key  string
	}{
		{name: "GITHUB_ACTIONS", key: "GITHUB_ACTIONS"},
		{name: "GITLAB_CI", key: "GITLAB_CI"},
		{name: "BUILDKITE", key: "BUILDKITE"},
		{name: "TF_BUILD", key: "TF_BUILD"},
		{name: "JENKINS_URL", key: "JENKINS_URL"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Arrange
			t.Setenv("CI", "")
			t.Setenv(tt.key, "1")

			// Act
			got := isCI()

			// Assert
			if !got {
				t.Fatalf("isCI() = false, want true when %s is set", tt.key)
			}
		})
	}
}
