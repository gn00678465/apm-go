package ux

import (
	"io"
	"sync"
	"time"

	lgspinner "charm.land/bubbles/v2/spinner"
	"charm.land/lipgloss/v2"
)

// spinnerFPS is the frame interval used to animate Spin. huh/spinner itself
// only exposes a synchronous Title().Action(fn).Run() API (no way to update
// the title or report success/failure mid-flight from the caller), which
// doesn't fit Spin's Update/Success/Fail contract. Spin instead drives the
// same underlying charm.land/bubbles/v2/spinner frame set on its own ticker,
// the way pterm's SpinnerPrinter did.
const spinnerFPS = 100 * time.Millisecond

var spinnerStyle = lipgloss.NewStyle().Foreground(lipgloss.Color(ColorBrand))

// spinnerIsRich decides whether Spinner should animate on w: the process-wide
// styling decision (see ux.go) plus w's own terminal-ness.
var spinnerIsRich = func(w io.Writer) bool { return styleEnabled && isTerminalWriter(w) }

// Spin represents an in-progress spinner started by Spinner. On a rich
// writer it animates; otherwise (non-terminal writer / NO_COLOR / CI) no
// animation runs and each call prints a single plain line instead.
type Spin struct {
	w        io.Writer
	raw      bool
	mu       sync.Mutex
	text     string
	stop     chan struct{}
	done     chan struct{}
	stopOnce sync.Once
}

// Spinner starts a progress spinner with the given text on w. If w is not
// a rich writer, no animation is shown; text is printed once as an info
// line instead.
func Spinner(w io.Writer, text string) *Spin {
	if !spinnerIsRich(w) {
		Info(w, "%s", text)
		return &Spin{w: w, raw: true}
	}

	s := &Spin{w: w, text: text, stop: make(chan struct{}), done: make(chan struct{})}
	go s.animate()
	return s
}

func (s *Spin) animate() {
	defer close(s.done)

	model := lgspinner.New(lgspinner.WithSpinner(lgspinner.Dot), lgspinner.WithStyle(spinnerStyle))
	ticker := time.NewTicker(spinnerFPS)
	defer ticker.Stop()

	s.render(model.View())
	for {
		select {
		case <-s.stop:
			s.clearLine()
			return
		case <-ticker.C:
			model, _ = model.Update(model.Tick())
			s.render(model.View())
		}
	}
}

func (s *Spin) render(frame string) {
	s.mu.Lock()
	text := s.text
	s.mu.Unlock()
	lipgloss.Fprint(s.w, "\r\x1b[K"+frame+" "+text)
}

func (s *Spin) clearLine() {
	lipgloss.Fprint(s.w, "\r\x1b[K")
}

// Update changes the spinner's in-progress text.
func (s *Spin) Update(text string) {
	if s.raw {
		Info(s.w, "%s", text)
		return
	}
	s.mu.Lock()
	s.text = text
	s.mu.Unlock()
}

// Success stops the spinner and prints a success line.
func (s *Spin) Success(msg string) {
	s.finish()
	Success(s.w, "%s", msg)
}

// Fail stops the spinner and prints an error line.
func (s *Spin) Fail(msg string) {
	s.finish()
	Error(s.w, "%s", msg)
}

// finish stops the animation goroutine (if any) and waits for it to clear
// its line before the caller prints the final Success/Fail line. It is
// idempotent and safe to call more than once (e.g. a deferred Fail after an
// explicit Success): sync.Once ensures the stop channel is closed exactly
// once, avoiding a "close of closed channel" panic.
func (s *Spin) finish() {
	if s.raw {
		return
	}
	s.stopOnce.Do(func() {
		close(s.stop)
		<-s.done
	})
}
