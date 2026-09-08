package ux

import "testing"

// stripANSI returns s without its escape sequences. It is a test helper: the
// assertions that need the visible text of a styled render would otherwise
// pull github.com/charmbracelet/x/ansi in as a direct dependency -- a test
// import promotes it from // indirect exactly as a production import would.
//
// huh and lipgloss render these strings with CSI sequences only: ESC, an
// introducer byte, parameter bytes, then a final byte in @-~.
func stripANSI(s string) string {
	var b []byte
	for i := 0; i < len(s); {
		if s[i] != 0x1b {
			b = append(b, s[i])
			i++
			continue
		}
		i += 2 // ESC and the introducer byte (e.g. '[' for CSI)
		for i < len(s) && (s[i] < '@' || s[i] > '~') {
			i++
		}
		i++ // the final byte closes the sequence
	}
	return string(b)
}

func TestStripANSI(t *testing.T) {
	cases := []struct {
		in   string
		want string
	}{
		{"", ""},
		{"plain", "plain"},
		{"\x1b[m", ""},
		{"\x1b[38;2;45;212;191mYes\x1b[m", "Yes"},
		{"\x1b[0mA\x1b[1;31mB\x1b[m", "AB"},
		{"│\x1b[m ", "│ "},
		{"\x1b[m│ 中文", "│ 中文"},
		// Truncated sequence: everything after ESC is consumed, not leaked.
		{"ok\x1b[38;2;45", "ok"},
	}
	for _, c := range cases {
		if got := stripANSI(c.in); got != c.want {
			t.Errorf("stripANSI(%q) = %q, want %q", c.in, got, c.want)
		}
	}
}
