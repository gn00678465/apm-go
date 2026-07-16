package ux

import (
	"bytes"
	"strings"
	"testing"
	"time"
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
	if strings.Contains(out, "\x1b[") {
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
	var sp *Spin

	// Act
	runWithTimeout(t, time.Second, func() {
		sp = Spinner(&buf, "fetching plugins")
		sp.Update("resolving deps")
		sp.Success("done")
	})

	// Assert: a rich-mode spinner animates on its own goroutine and Success
	// stops it cleanly without blocking; the final line lands in the buffer.
	if sp == nil {
		t.Fatal("Spinner() returned nil")
	}
	if !strings.Contains(buf.String(), "done") {
		t.Fatalf("spinner output %q missing final success message", buf.String())
	}
}

func TestSpinner_RichFailStopsWithoutBlocking(t *testing.T) {
	// Arrange
	setRichMode(t, true)
	var buf bytes.Buffer
	var sp *Spin

	// Act
	runWithTimeout(t, time.Second, func() {
		sp = Spinner(&buf, "fetching plugins")
		sp.Fail("network error")
	})

	// Assert
	if sp == nil {
		t.Fatal("Spinner() returned nil")
	}
	if !strings.Contains(buf.String(), "network error") {
		t.Fatalf("spinner output %q missing failure message", buf.String())
	}
}

// TestSpinner_RichDoubleTerminalCallDoesNotPanic proves finish is idempotent:
// a Success followed by a Fail (e.g. an explicit success plus a deferred
// failure cleanup) must not panic with "close of closed channel".
func TestSpinner_RichDoubleTerminalCallDoesNotPanic(t *testing.T) {
	// Arrange
	setRichMode(t, true)
	var buf bytes.Buffer

	// Act + Assert: completes within the timeout and does not panic.
	runWithTimeout(t, time.Second, func() {
		sp := Spinner(&buf, "fetching")
		sp.Success("done")
		sp.Fail("late error")
	})
}

// TestSpinner_MultipleUpdatesThenSuccess proves Update can be called
// repeatedly against the live animation goroutine (guarded by Spin.mu)
// without deadlocking or racing, before the spinner is stopped.
func TestSpinner_MultipleUpdatesThenSuccess(t *testing.T) {
	// Arrange
	setRichMode(t, true)
	var buf bytes.Buffer

	// Act
	runWithTimeout(t, 2*time.Second, func() {
		sp := Spinner(&buf, "step 1")
		for i := 0; i < 20; i++ {
			sp.Update("step 2")
		}
		sp.Success("all done")
	})

	// Assert
	if !strings.Contains(buf.String(), "all done") {
		t.Fatalf("spinner output %q missing final success message", buf.String())
	}
}
