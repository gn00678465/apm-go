package ux

import (
	"bytes"
	"regexp"
	"testing"

	"github.com/pterm/pterm"
)

var ansiEscape = regexp.MustCompile("\x1b\\[[0-9;]*[a-zA-Z]")

// withDisabledStyling forces pterm into raw/no-color mode for the
// duration of the test, restoring the previous state afterward.
func withDisabledStyling(t *testing.T) {
	t.Helper()
	wasRaw := pterm.RawOutput
	pterm.DisableStyling()
	t.Cleanup(func() {
		if !wasRaw {
			pterm.EnableStyling()
		}
	})
}

func TestPrinterGolden_NoANSIWhenStylingDisabled(t *testing.T) {
	withDisabledStyling(t)

	tests := []struct {
		name    string
		fn      func(w *bytes.Buffer)
		wantSub string
	}{
		{
			name:    "Success",
			fn:      func(w *bytes.Buffer) { Success(w, "%s done", "task") },
			wantSub: "task done",
		},
		{
			name:    "Info",
			fn:      func(w *bytes.Buffer) { Info(w, "fetching %s", "pkg") },
			wantSub: "fetching pkg",
		},
		{
			name:    "Warn",
			fn:      func(w *bytes.Buffer) { Warn(w, "deprecated flag") },
			wantSub: "deprecated flag",
		},
		{
			name:    "Error",
			fn:      func(w *bytes.Buffer) { Error(w, "failed: %v", "boom") },
			wantSub: "failed: boom",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Arrange
			var buf bytes.Buffer

			// Act
			tt.fn(&buf)
			out := buf.String()

			// Assert
			if ansiEscape.MatchString(out) {
				t.Fatalf("%s: output contains ANSI escape codes: %q", tt.name, out)
			}
			if !bytes.Contains([]byte(out), []byte(tt.wantSub)) {
				t.Fatalf("%s: output %q does not contain %q", tt.name, out, tt.wantSub)
			}
		})
	}
}
