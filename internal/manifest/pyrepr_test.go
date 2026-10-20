package manifest

import "testing"

// PyRepr is Python 3.12's repr() of a str; the expected values are what
// repr() printed for each input.
func TestPyRepr(t *testing.T) {
	tests := []struct{ in, want string }{
		{"plain/path", `'plain/path'`},
		{"it's", `"it's"`},
		{`say "hi"`, `'say "hi"'`},
		{`both ' and "`, `'both \' and "'`},
		{`back\slash`, `'back\\slash'`},
		{"new\nline\ttab\r", `'new\nline\ttab\r'`},
		{"\x7f\x01", `'\x7f\x01'`},
		{"caf\u00e9", "'caf\u00e9'"},
		{"zw\u200bsp", `'zw\u200bsp'`},
		{"\U0001f600", "'\U0001f600'"},
		{"bad\xffbyte", `'bad\udcffbyte'`},
	}
	for _, tt := range tests {
		if got := PyRepr(tt.in); got != tt.want {
			t.Errorf("PyRepr(%q) = %s, want %s", tt.in, got, tt.want)
		}
	}
}
