package ux

import "testing"

// withStdinTTY overrides the stdin-is-a-TTY seam for the duration of the
// test, restoring the previous implementation afterward.
func withStdinTTY(t *testing.T, tty bool) {
	t.Helper()
	prev := stdinIsTTY
	stdinIsTTY = func() bool { return tty }
	t.Cleanup(func() { stdinIsTTY = prev })
}

// withStderrTTY overrides the stderr-is-a-TTY seam for the duration of the
// test, restoring the previous implementation afterward.
func withStderrTTY(t *testing.T, tty bool) {
	t.Helper()
	prev := stderrIsTTY
	stderrIsTTY = func() bool { return tty }
	t.Cleanup(func() { stderrIsTTY = prev })
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
