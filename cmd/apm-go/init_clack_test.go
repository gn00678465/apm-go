package main

import (
	"io"
	"os"
	"strings"
	"testing"

	"github.com/apm-go/apm/internal/ux"
)

// captureStderr redirects os.Stderr for the duration of fn and returns what
// was written. The old terminal-ux-contract §3 citation that described init
// success output as stderr-only is retired: ticket 19 Finding 2 aligns init
// and plugin init with the Oracle's stdout-only success block. This helper
// remains for the interactive Clack transcript and other stderr assertions.
func captureStderr(t *testing.T, fn func()) string {
	t.Helper()

	r, w, err := os.Pipe()
	if err != nil {
		t.Fatalf("os.Pipe() err = %v", err)
	}
	orig := os.Stderr
	os.Stderr = w

	fn()

	os.Stderr = orig
	if err := w.Close(); err != nil {
		t.Fatalf("close pipe writer: %v", err)
	}
	out, err := io.ReadAll(r)
	if err != nil {
		t.Fatalf("read captured stderr: %v", err)
	}
	return string(out)
}

// clackGlyphs are the transcript/banner characters that must never reach a
// non-interactive run's output. The shared Oracle-aligned success table/panel
// legitimately uses box-drawing vertical bars, so "│" is not a Clack-only
// marker anymore.
var clackGlyphs = []string{"█", "╗", "┌", "◇", "└"}

// TestInitCmd_NonInteractiveRunsPrintNoBannerOrTranscript pins the gating in
// PRD R1/R4: the clack transcript and the block-art banner belong to
// interactive runs only. A --yes run and a non-TTY run must keep the plain
// prefix output they had before issue #14, so scripts and CI logs are
// unchanged.
func TestInitCmd_NonInteractiveRunsPrintNoBannerOrTranscript(t *testing.T) {
	tests := []struct {
		name string
		args []string
	}{
		{name: "--yes", args: []string{"--yes", "--target", "claude"}},
		{name: "non-TTY without --yes", args: []string{"--target", "claude"}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Arrange
			dir := t.TempDir()
			origDir, _ := os.Getwd()
			if err := os.Chdir(dir); err != nil {
				t.Fatalf("chdir: %v", err)
			}
			t.Cleanup(func() { os.Chdir(origDir) })

			restore := ux.SetTTYSeamsForTest(false, false, false)
			t.Cleanup(restore)

			// Act
			var runErr error
			var stdout string
			stderr := captureStderr(t, func() {
				stdout = captureStdout(t, func() {
					cmd := initCmd()
					cmd.SetArgs(tt.args)
					runErr = cmd.Execute()
				})
			})

			// Assert
			if runErr != nil {
				t.Fatalf("init failed: %v", runErr)
			}
			for _, glyph := range clackGlyphs {
				if strings.Contains(stdout, glyph) || strings.Contains(stderr, glyph) {
					t.Fatalf("non-interactive init emitted clack glyph %q:\nstdout=%s\nstderr=%s", glyph, stdout, stderr)
				}
			}
			if !strings.Contains(stdout, "APM project initialized successfully!") {
				t.Fatalf("non-interactive init lost its plain success output:\n%s", stdout)
			}
			if stderr != "" {
				t.Fatalf("non-interactive init success wrote to stderr:\n%s", stderr)
			}
		})
	}
}
