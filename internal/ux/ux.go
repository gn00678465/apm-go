package ux

import (
	"io"
	"os"

	"github.com/pterm/pterm"
	"golang.org/x/term"
)

// richMode gates interactive prompts (Confirm/InputText/Password/
// MultiSelect): true when stdin is a real TTY (for reading answers) *and*
// stderr is a real TTY (huh renders its forms to stderr), NO_COLOR is
// unset, and the process is not running in a CI environment.
//
// Structured/prefixed output (Success/Info/Warn/Error/Table/BulletList/
// Tree/Section/Spinner) does NOT use richMode: each call decides per-writer,
// via isRichWriter/renderForWriter, whether to keep or strip the ANSI pterm
// already produced, so redirecting one command's stdout to a file doesn't
// leak ANSI just because stdin/stderr happen to still be a TTY.
var richMode bool

// styleEnabled is pterm's process-wide styling decision, set exactly once by
// Init() (or left at the package init() default below) before any concurrent
// output goroutine - namely a spinner's background render loop - can exist.
// Nothing after Init() ever calls pterm.EnableStyling()/DisableStyling()
// again: doing so per-call (as an earlier revision did, guarded by a mutex)
// still raced under `go test -race`, because pterm's spinner render
// goroutine reads the same global flag (pterm.RawOutput) without taking any
// lock (see spinner.go and pterm.SpinnerPrinter.Start). Fixing your own
// writer's decision can never be a substitute for not mutating shared global
// state from multiple goroutines in the first place.
var styleEnabled bool

// init defaults pterm to plain-text output before Init() ever runs (e.g.
// package-level ux.* calls made by tests that never invoke main(), or a
// caller that forgets to wire Init() in).
func init() {
	pterm.DisableStyling()
}

// Init detects the current terminal environment (TTY / NO_COLOR / CI) and
// configures huh's interactive behavior and pterm's global styling flag
// accordingly. Call once during CLI startup, before any other ux function
// and before any goroutine that might call one (e.g. a background worker
// that reports progress via ux.Info) is started.
func Init() {
	richMode = isInteractive() && isStderrInteractive() && !noColorSet() && !isCI()

	styleEnabled = (isInteractive() || isStderrInteractive()) && !noColorSet() && !isCI()
	if styleEnabled {
		pterm.EnableStyling()
	} else {
		pterm.DisableStyling()
	}
}

// IsRich reports whether interactive prompts should be shown (real TTY on
// both stdin and stderr, no NO_COLOR, not CI). Interactive functions
// (Confirm/InputText/Password/MultiSelect) use this to decide whether to
// prompt or fall back to plain defaults.
func IsRich() bool {
	return richMode
}

// stdinIsTTY and stderrIsTTY are swappable seams for tests; production
// code always uses the default* implementations.
var (
	stdinIsTTY  = defaultStdinIsTTY
	stderrIsTTY = defaultStderrIsTTY
)

// isInteractive reports whether stdin is a real terminal, mirroring
// cmd/apm-go/init.go's isInteractive().
func isInteractive() bool {
	return stdinIsTTY()
}

// isStderrInteractive reports whether stderr is a real terminal. huh forms
// render to stderr by default, so the interactive gate needs this in
// addition to stdin.
func isStderrInteractive() bool {
	return stderrIsTTY()
}

func defaultStdinIsTTY() bool {
	return isTerminalFile(os.Stdin)
}

func defaultStderrIsTTY() bool {
	return isTerminalFile(os.Stderr)
}

// isTerminalFile reports whether f is a real terminal device. Unlike
// checking os.ModeCharDevice on f.Stat(), this correctly rejects things
// like /dev/null (Unix) or NUL (Windows), which are character devices but
// not terminals.
func isTerminalFile(f *os.File) bool {
	return term.IsTerminal(int(f.Fd()))
}

// isTerminalWriter reports whether w is a real terminal (as opposed to a
// pipe, redirected file, or in-memory buffer such as bytes.Buffer). Output
// functions use this, per call, to decide whether a specific writer should
// receive styled/animated output instead of relying on a single
// process-wide mode.
func isTerminalWriter(w io.Writer) bool {
	f, ok := w.(*os.File)
	if !ok {
		return false
	}
	return isTerminalFile(f)
}

// isRichWriter reports whether w should receive styled/animated output: the
// process-wide styling decision made once in Init() must be enabled, and w
// itself must be a real terminal.
func isRichWriter(w io.Writer) bool {
	return styleEnabled && isTerminalWriter(w)
}

// renderForWriter returns s exactly as pterm rendered it when w is a real
// terminal, and with ANSI escape codes stripped otherwise (e.g. a redirected
// file, a pipe, or an in-memory buffer such as bytes.Buffer).
//
// This makes styling decisions per-writer without ever touching pterm's
// global styling flag: s is always rendered the same way (via pterm's
// Sprint/Srender family, which reflects whatever Init() decided once for the
// whole process), and only the already-rendered string is adjusted per
// writer. That is what makes this safe to call concurrently with an active
// spinner: pterm's spinner render goroutine only ever reads its global
// styling flag, and nothing here ever writes to it after Init().
func renderForWriter(w io.Writer, s string) string {
	if isTerminalWriter(w) {
		return s
	}
	return pterm.RemoveColorFromString(s)
}

func noColorSet() bool {
	return os.Getenv("NO_COLOR") != ""
}

// isCI reports whether common CI environment variables are set.
func isCI() bool {
	if os.Getenv("CI") != "" {
		return true
	}
	for _, key := range []string{"GITHUB_ACTIONS", "GITLAB_CI", "BUILDKITE", "TF_BUILD", "JENKINS_URL"} {
		if os.Getenv(key) != "" {
			return true
		}
	}
	return false
}
