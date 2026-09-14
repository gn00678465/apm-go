package main

import "testing"

// resolvingLabel feeds check -v's "Resolving <name> via <host>: <label>"
// line (check.py:130-139): a URL keeps its host and text, a host-prefixed
// shorthand expands to its clone URL, anything else resolves via the
// default host.
func TestResolvingLabel(t *testing.T) {
	cases := []struct{ source, wantHost, wantLabel string }{
		{"https://github.com/o/r.git", "github.com", "https://github.com/o/r.git"},
		{"https://gitlab.example.com/grp/sub/r", "gitlab.example.com", "https://gitlab.example.com/grp/sub/r"},
		{"github.com/o/r", "github.com", "https://github.com/o/r.git"},
		{"o/r", "default host", "o/r"},
		{"http://%zz/broken", "default host", "http://%zz/broken"},
	}
	for _, c := range cases {
		host, label := resolvingLabel(c.source)
		if host != c.wantHost || label != c.wantLabel {
			t.Errorf("resolvingLabel(%q) = (%q, %q), want (%q, %q)", c.source, host, label, c.wantHost, c.wantLabel)
		}
	}
}
