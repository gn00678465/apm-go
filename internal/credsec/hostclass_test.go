package credsec

import "testing"

func TestSameHostClass(t *testing.T) {
	aliases := map[string][]string{
		"registry.example.com": {"mirror.elsewhere.net"},
	}
	tests := []struct {
		name, a, b string
		want       bool
	}{
		// spec §3 / §10.3 worked example
		{"subdomain shares eTLD+1", "github.contoso.com", "contoso.com", true},
		{"distinct eTLD+1 differ", "github.contoso.com", "github.com", false},
		{"same host", "github.com", "github.com", true},
		{"two github subdomains", "api.github.com", "codeload.github.com", true},
		{"port stripped", "github.com:443", "github.com", true},
		{"alias group", "registry.example.com", "mirror.elsewhere.net", true},
		{"alias not transitive to others", "mirror.elsewhere.net", "github.com", false},
		{"localhost singleton vs ip", "localhost", "127.0.0.1", false},
		{"identical degenerate", "localhost", "localhost", true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := SameHostClass(tt.a, tt.b, aliases); got != tt.want {
				t.Errorf("SameHostClass(%q,%q)=%v want %v", tt.a, tt.b, got, tt.want)
			}
		})
	}
}

func TestHostClass_eTLD1(t *testing.T) {
	if got := HostClass("github.contoso.com"); got != "contoso.com" {
		t.Errorf("eTLD+1 = %q want contoso.com", got)
	}
	if got := HostClass("localhost"); got != "localhost" {
		t.Errorf("degenerate host should be its own class, got %q", got)
	}
}

// TestHostClass_RequiresRealPSL locks in that classification uses the Public
// Suffix List, not a naive "last two labels" heuristic. Under the PSL,
// `github.io` is itself a public suffix, so `foo.github.io` and `bar.github.io`
// are DIFFERENT registrable domains (credentials MUST NOT be shared) — a
// last-two-labels approximation would wrongly merge both onto `github.io`.
func TestHostClass_RequiresRealPSL(t *testing.T) {
	if HostClass("foo.github.io") == HostClass("bar.github.io") {
		t.Errorf("PSL violation: foo.github.io and bar.github.io must be distinct classes, got %q",
			HostClass("foo.github.io"))
	}
	if SameHostClass("foo.github.io", "bar.github.io", nil) {
		t.Errorf("foo.github.io and bar.github.io must NOT share a credential scope")
	}
	// Sanity: same registrable domain still merges.
	if HostClass("foo.github.io") != "foo.github.io" {
		t.Errorf("foo.github.io eTLD+1 should be itself, got %q", HostClass("foo.github.io"))
	}
}
