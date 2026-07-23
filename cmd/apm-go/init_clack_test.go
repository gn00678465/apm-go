package main

import (
	"io"
	"os"
	"strings"
	"testing"

	"github.com/apm-go/apm/internal/ux"
)

// captureStderr redirects os.Stderr for the duration of fn and returns what
// was written. init writes its human-facing output straight to os.Stderr (the
// stream contract in terminal-ux-contract §3), so the process-level stream is
// what has to be inspected.
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
// non-interactive run's output.
var clackGlyphs = []string{"█", "╗", "┌", "◇", "│", "└"}

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
			out := captureStderr(t, func() {
				cmd := initCmd()
				cmd.SetArgs(tt.args)
				runErr = cmd.Execute()
			})

			// Assert
			if runErr != nil {
				t.Fatalf("init failed: %v", runErr)
			}
			for _, glyph := range clackGlyphs {
				if strings.Contains(out, glyph) {
					t.Fatalf("non-interactive init emitted clack glyph %q:\n%s", glyph, out)
				}
			}
			if !strings.Contains(out, "APM project initialized successfully!") {
				t.Fatalf("non-interactive init lost its plain success output:\n%s", out)
			}
		})
	}
}
