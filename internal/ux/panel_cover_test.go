//go:build linux

package ux

import (
	"bytes"
	"os"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"unsafe"
)

// The expected panel is what Rich's Console().print(Panel(body, title=title))
// printed with NO_COLOR=1 COLUMNS=24, the Oracle's _rich_panel call.
func TestPanel_WhitespaceOnlyLineIsAnEmptyRow(t *testing.T) {
	t.Setenv("COLUMNS", "24")
	var buf bytes.Buffer

	Panel(&buf, " Next Steps", []string{"first\n   \nlast one  "})

	want := strings.TrimPrefix(`
╭────  Next Steps ─────╮
│ first                │
│                      │
│ last one             │
╰──────────────────────╯
`, "\n")
	if got := buf.String(); got != want {
		t.Errorf("Panel output differs from Rich's\n got:\n%s\nwant:\n%s", got, want)
	}
}

func ptyIoctl(f *os.File, request uintptr, arg unsafe.Pointer) error {
	if _, _, errno := syscall.Syscall(syscall.SYS_IOCTL, f.Fd(), request, uintptr(arg)); errno != 0 {
		return errno
	}
	return nil
}

// openTerminal returns the terminal side of a new pseudo-terminal that is
// cols wide.
func openTerminal(t *testing.T, cols uint16) *os.File {
	t.Helper()
	master, err := os.OpenFile("/dev/ptmx", os.O_RDWR, 0)
	if err != nil {
		t.Skipf("no pseudo-terminal here: %v", err)
	}
	t.Cleanup(func() { master.Close() })
	var unlock int32
	if err := ptyIoctl(master, syscall.TIOCSPTLCK, unsafe.Pointer(&unlock)); err != nil {
		t.Skipf("no pseudo-terminal here: %v", err)
	}
	var number uint32
	if err := ptyIoctl(master, syscall.TIOCGPTN, unsafe.Pointer(&number)); err != nil {
		t.Skipf("no pseudo-terminal here: %v", err)
	}
	terminal, err := os.OpenFile("/dev/pts/"+strconv.Itoa(int(number)), os.O_RDWR|syscall.O_NOCTTY, 0)
	if err != nil {
		t.Skipf("no pseudo-terminal here: %v", err)
	}
	t.Cleanup(func() { terminal.Close() })
	size := struct{ rows, cols, xpixel, ypixel uint16 }{rows: 24, cols: cols}
	if err := ptyIoctl(terminal, syscall.TIOCSWINSZ, unsafe.Pointer(&size)); err != nil {
		t.Fatal(err)
	}
	return terminal
}

func setOrUnsetEnv(t *testing.T, key, value string) {
	t.Helper()
	t.Setenv(key, value)
	if value == "" {
		os.Unsetenv(key)
	}
}

// Each expected panel is what Rich printed for the same body on a
// 40-column pseudo-terminal (`script -qec "stty cols 40; env -i NO_COLOR=1
// TERM=... python -c 'Console().print(Panel(...))'"`). Panel writes to a
// buffer here: the width comes from the standard streams, not the writer.
func TestPanel_WidthOnATerminalFollowsRich(t *testing.T) {
	const (
		wide40 = `
╭──────────────────────────────────────╮
│ on a terminal                        │
╰──────────────────────────────────────╯
`
		wide50 = `
╭────────────────────────────────────────────────╮
│ on a terminal                                  │
╰────────────────────────────────────────────────╯
`
		wide80 = `
╭──────────────────────────────────────────────────────────────────────────────╮
│ on a terminal                                                                │
╰──────────────────────────────────────────────────────────────────────────────╯
`
	)
	tests := []struct {
		name                 string
		term, columns, lines string
		want                 string
	}{
		{"terminal size when COLUMNS is unset", "xterm", "", "", wide40},
		{"COLUMNS wins over the terminal size", "xterm", "50", "", wide50},
		{"dumb terminal is 80 wide", "dumb", "", "", wide80},
		{"dumb terminal ignores COLUMNS without LINES", "dumb", "50", "", wide80},
		{"dumb terminal takes COLUMNS with LINES", "dumb", "50", "10", wide50},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			terminal := openTerminal(t, 40)
			stdin, stdout, stderr := os.Stdin, os.Stdout, os.Stderr
			t.Cleanup(func() { os.Stdin, os.Stdout, os.Stderr = stdin, stdout, stderr })
			os.Stdin, os.Stdout, os.Stderr = terminal, terminal, terminal
			setOrUnsetEnv(t, "TERM", tt.term)
			setOrUnsetEnv(t, "COLUMNS", tt.columns)
			setOrUnsetEnv(t, "LINES", tt.lines)
			var buf bytes.Buffer

			Panel(&buf, "", []string{"on a terminal"})

			if got, want := buf.String(), strings.TrimPrefix(tt.want, "\n"); got != want {
				t.Errorf("Panel output differs from Rich's\n got:\n%s\nwant:\n%s", got, want)
			}
		})
	}
}
