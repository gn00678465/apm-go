package ux

import (
	"io"
	"os"

	"golang.org/x/term"
)

// richMode backs IsRich(): true when stdin is a real TTY (for reading
// answers) *and* stderr is a real TTY (huh renders its forms to stderr),
// NO_COLOR is unset, and the process is not running in a CI environment.
//
// Interactive prompts (Confirm/InputText/Password/MultiSelect/InputForm)
// gate on CanPrompt(), not richMode/IsRich(): see CanPrompt's doc comment for
// why NO_COLOR must not disable prompting.
var richMode bool

// styleEnabled decides whether Spinner should animate at all (as opposed to
// falling back to a single static line): true when both stdout and stderr
// are real terminals, NO_COLOR is unset, and the process is not running in
// CI. Coloring itself doesn't need this flag -- lipgloss.Fprint/Fprintln
// downsample/strip colors per-writer automatically (see printer.go/output.go)
// -- but animating a spinner makes no sense when either stream has been
// redirected away from a terminal or the process is non-interactive.
var styleEnabled bool

// Init detects the current terminal environment (TTY / NO_COLOR / CI) and
// configures huh's interactive behavior and the Spinner animation decision
// accordingly. Call once during CLI startup, before any other ux function
// and before any goroutine that might call one (e.g. a background worker
// that reports progress via ux.Info) is started.
func Init() {
	richMode = isInteractive() && isStderrInteractive() && !noColorSet() && !isCI()
	styleEnabled = stdoutIsTTY() && stderrIsTTY() && !noColorSet() && !isCI()
}

// IsRich reports whether interactive prompts should be shown (real TTY on
// both stdin and stderr, no NO_COLOR, not CI).
func IsRich() bool {
	return richMode
}

// CanPrompt reports whether a prompt can be shown at all: real TTY on both
// stdin (to read the answer) and stderr (huh renders forms there), and not
// running in CI. Interactive functions (Confirm/InputText/Password/
// MultiSelect/InputForm) gate on this, deliberately excluding NO_COLOR:
// NO_COLOR only means "don't colorize", not "don't ask questions" -- gating
// prompts on IsRich() (as an earlier revision did) meant a real, TTY-backed
// session with NO_COLOR set silently skipped every prompt and took its
// default, which is a UX regression NO_COLOR was never meant to cause.
func CanPrompt() bool {
	return stdinIsTTY() && stderrIsTTY() && !isCI()
}

// stdinIsTTY, stdoutIsTTY and stderrIsTTY are swappable seams for tests;
// production code always uses the default* implementations.
var (
	stdinIsTTY  = defaultStdinIsTTY
	stdoutIsTTY = defaultStdoutIsTTY
	stderrIsTTY = defaultStderrIsTTY
)

// isInteractive reports whether stdin is a real terminal. cmd/apm-go/init.go
// and mcp_prompt.go call this indirectly via CanPrompt() rather than
// duplicating their own os.ModeCharDevice-based check, which incorrectly
// treated a redirected non-terminal (e.g. /dev/null) as interactive.
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

func defaultStdoutIsTTY() bool {
	return isTerminalFile(os.Stdout)
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
// pipe, redirected file, or in-memory buffer such as bytes.Buffer). Spinner
// uses this, per call, to decide whether a specific writer can support
// animation at all: even when styleEnabled is true process-wide, animating a
// spinner into a non-terminal writer makes no sense.
func isTerminalWriter(w io.Writer) bool {
	f, ok := w.(*os.File)
	if !ok {
		return false
	}
	return isTerminalFile(f)
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
