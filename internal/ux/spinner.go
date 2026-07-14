package ux

import (
	"io"

	"github.com/pterm/pterm"
)

// Spin represents an in-progress spinner started by Spinner. On a rich
// writer it animates; otherwise (non-terminal writer / NO_COLOR / CI) no
// animation runs and each call prints a single plain line instead.
type Spin struct {
	w   io.Writer
	raw bool
	sp  *pterm.SpinnerPrinter
}

// spinnerIsRich decides whether Spinner should animate on w. It is a
// swappable seam so tests can exercise the live pterm.SpinnerPrinter
// lifecycle without a real terminal writer; production code always uses
// isRichWriter.
var spinnerIsRich = isRichWriter

// Spinner starts a progress spinner with the given text on w. If w is not
// a rich writer, no animation is shown; text is printed once as an info
// line instead.
//
// This calls pterm's real Start/UpdateText/Success/Fail directly, without
// wrapping them in a per-call pterm.EnableStyling()/DisableStyling() toggle:
// pterm's spinner runs its own render goroutine that reads pterm's global
// styling flag on a timer, without taking any lock, so any code that flips
// that flag from another goroutine while the spinner is active races with
// it under `go test -race`. Init() decides that flag once for the whole
// process before any spinner can be running, and nothing here ever changes
// it again.
func Spinner(w io.Writer, text string) *Spin {
	if !spinnerIsRich(w) {
		Info(w, "%s", text)
		return &Spin{w: w, raw: true}
	}

	sp, _ := pterm.DefaultSpinner.WithWriter(w).Start(text)
	return &Spin{w: w, sp: sp}
}

// Update changes the spinner's in-progress text.
func (s *Spin) Update(text string) {
	if s.raw {
		Info(s.w, "%s", text)
		return
	}
	s.sp.UpdateText(text)
}

// Success stops the spinner and prints a success line.
func (s *Spin) Success(msg string) {
	if s.raw {
		Success(s.w, "%s", msg)
		return
	}
	s.sp.Success(msg)
}

// Fail stops the spinner and prints an error line.
func (s *Spin) Fail(msg string) {
	if s.raw {
		Error(s.w, "%s", msg)
		return
	}
	s.sp.Fail(msg)
}
