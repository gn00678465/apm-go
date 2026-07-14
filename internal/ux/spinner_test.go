package ux

import (
	"bytes"
	"strings"
	"sync"
	"testing"
)

func TestSpinner_NonRichPrintsPlainLinesWithoutAnimation(t *testing.T) {
	// Arrange
	setRichMode(t, false)
	var buf bytes.Buffer

	// Act
	sp := Spinner(&buf, "fetching plugins")
	sp.Update("resolving deps")
	sp.Success("done")
	out := buf.String()

	// Assert
	if ansiEscape.MatchString(out) {
		t.Fatalf("spinner output contains ANSI escape codes: %q", out)
	}
	for _, want := range []string{"fetching plugins", "resolving deps", "done"} {
		if !strings.Contains(out, want) {
			t.Fatalf("spinner output %q missing %q", out, want)
		}
	}
}

func TestSpinner_NonRichFailPrintsErrorLine(t *testing.T) {
	// Arrange
	setRichMode(t, false)
	var buf bytes.Buffer

	// Act
	sp := Spinner(&buf, "fetching plugins")
	sp.Fail("network error")
	out := buf.String()

	// Assert
	if !strings.Contains(out, "network error") {
		t.Fatalf("spinner output %q missing failure message", out)
	}
}

func TestSpinner_RichStartsAndStopsWithoutBlocking(t *testing.T) {
	// Arrange
	setRichMode(t, true)
	var buf bytes.Buffer

	// Act
	sp := Spinner(&buf, "fetching plugins")
	sp.Update("resolving deps")
	sp.Success("done")

	// Assert: a rich-mode spinner returns a live SpinnerPrinter and does not
	// panic/block when driven through its lifecycle.
	if sp == nil {
		t.Fatal("Spinner() returned nil")
	}
}

func TestSpinner_RichFailStopsWithoutBlocking(t *testing.T) {
	// Arrange
	setRichMode(t, true)
	var buf bytes.Buffer

	// Act
	sp := Spinner(&buf, "fetching plugins")
	sp.Fail("network error")

	// Assert: fail path also stops the spinner cleanly.
	if sp == nil {
		t.Fatal("Spinner() returned nil")
	}
}

// TestSpinner_ConcurrentOutputDoesNotRaceWithRenderLoop is a regression test
// for a data race an earlier revision had: while a rich spinner is active,
// its pterm render goroutine reads pterm's global styling flag
// (pterm.RawOutput) on a timer without taking any lock (see
// pterm.SpinnerPrinter.Start). That earlier revision decided styling
// per-writer by toggling that same flag on every ux.Success/Info/Warn/Error/
// Table/... call, guarded only by a mutex the spinner's own goroutine never
// took - so it raced under `go test -race` whenever any of those functions
// ran concurrently with an active spinner.
//
// The current implementation never mutates pterm's global styling flag
// outside of Init(), so nothing here should race. This machine's Go
// toolchain has no C compiler, so -race cannot be exercised locally; run
// this test with `go test -race` in CI to confirm.
func TestSpinner_ConcurrentOutputDoesNotRaceWithRenderLoop(t *testing.T) {
	// Arrange: start a real animated spinner (spins up pterm's background
	// render goroutine), matching what Init() would configure in a rich
	// terminal session.
	setRichMode(t, true)
	var spinnerBuf bytes.Buffer
	sp := Spinner(&spinnerBuf, "installing plugins")

	// Act: hammer other ux output functions, targeting writers other than
	// the spinner's, from several goroutines while the spinner is active.
	var wg sync.WaitGroup
	for i := 0; i < 20; i++ {
		wg.Add(1)
		go func(n int) {
			defer wg.Done()
			var buf bytes.Buffer
			Info(&buf, "step %d", n)
			Success(&buf, "step %d done", n)
			Table(&buf, []string{"NAME"}, [][]string{{"plugin"}})
		}(i)
	}
	wg.Wait()
	sp.Success("done")

	// Assert: no panic/deadlock above, and the spinner's own lifecycle
	// still completed normally.
	if !strings.Contains(spinnerBuf.String(), "done") {
		t.Fatalf("spinner output %q missing final success message", spinnerBuf.String())
	}
}
