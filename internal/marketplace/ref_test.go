package marketplace

import (
	"strings"
	"testing"
)

// TestParseRef_FallsThrough covers mkt-020: shapes that must NOT be
// recognized as a marketplace reference (ok=false, err=nil), so the caller
// falls through to the general dependency-string parser
// (manifest.ParseDepString).
func TestParseRef_FallsThrough(t *testing.T) {
	tests := []struct {
		name string
		in   string
	}{
		{"owner/repo shorthand (head contains /)", "owner/repo"},
		{"owner/repo@alias (head contains /)", "owner/repo@alias"},
		{"scp-style ssh remote (head contains :)", "git@host:o/r"},
		{"local relative path", "./local/path"},
		{"local relative dotdot path", "../local/path"},
		{"local absolute path", "/abs/path"},
		{"windows drive letter path (head contains :)", `C:\Users\foo\marketplace`},
		{"empty fragment: pkg@mkt#", "pkg@mkt#"},
		{"empty fragment with trailing space: pkg@mkt# ", "pkg@mkt# "},
		{"empty string", ""},
		{"no @ at all", "justapackage"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Arrange -- tt.in

			// Act
			plugin, mkt, ref, ok, err := ParseRef(tt.in)

			// Assert
			if err != nil {
				t.Fatalf("ParseRef(%q) returned unexpected error: %v", tt.in, err)
			}
			if ok {
				t.Fatalf("ParseRef(%q) = ok=true (plugin=%q, mkt=%q, ref=%q); want ok=false (fall through)", tt.in, plugin, mkt, ref)
			}
			if plugin != "" || mkt != "" || ref != "" {
				t.Fatalf("ParseRef(%q) fall-through case returned non-empty fields: plugin=%q mkt=%q ref=%q", tt.in, plugin, mkt, ref)
			}
		})
	}
}

// TestParseRef_Accepts covers the marketplace-reference shapes mkt-020/021
// require ParseRef to recognize and decompose.
func TestParseRef_Accepts(t *testing.T) {
	tests := []struct {
		name       string
		in         string
		wantPlugin string
		wantMkt    string
		wantRef    string
	}{
		{"plugin@marketplace, no ref", "pkg@mkt", "pkg", "mkt", ""},
		{"plugin@marketplace#ref", "pkg@mkt#v1.0", "pkg", "mkt", "v1.0"},
		{
			// Deviation (design.md, recorded for the A/B exception list): the
			// Python original's install-layer "/" not in package pre-check
			// breaks this shape, even though its own resolver/uninstall path
			// accepts it. ParseRef only implements the resolver semantics
			// (split "#" first), so a ref containing "/" is accepted here --
			// intentional, not an oversight.
			"plugin@marketplace#feature/branch (Go deviation: accepted, unlike Python install layer)",
			"pkg@mkt#feature/branch", "pkg", "mkt", "feature/branch",
		},
		{"surrounding whitespace is trimmed before parsing", " pkg@mkt ", "pkg", "mkt", ""},
		{
			// design.md rule 1: "#" is split at most once; a second "#"
			// inside the fragment stays part of ref.
			"second # stays part of ref", "pkg@mkt#a#b", "pkg", "mkt", "a#b",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Arrange -- tt.in

			// Act
			plugin, mkt, ref, ok, err := ParseRef(tt.in)

			// Assert
			if err != nil {
				t.Fatalf("ParseRef(%q) returned unexpected error: %v", tt.in, err)
			}
			if !ok {
				t.Fatalf("ParseRef(%q) = ok=false; want ok=true", tt.in)
			}
			if plugin != tt.wantPlugin || mkt != tt.wantMkt || ref != tt.wantRef {
				t.Fatalf("ParseRef(%q) = plugin=%q mkt=%q ref=%q; want plugin=%q mkt=%q ref=%q",
					tt.in, plugin, mkt, ref, tt.wantPlugin, tt.wantMkt, tt.wantRef)
			}
		})
	}
}

// TestParseRef_RejectsSemverRangeRef covers mkt-021: a CLI #REF suffix must
// be a raw git tag/branch/SHA, never a semver range constraint. Every
// character in the range charset must be individually rejected, and the
// error message must name "semver range" so the user knows why.
func TestParseRef_RejectsSemverRangeRef(t *testing.T) {
	tests := []struct {
		name string
		in   string
	}{
		{"caret range", "pkg@mkt#^1.0"},
		{"tilde range", "pkg@mkt#~1.0"},
		{"greater-than range", "pkg@mkt#>1.0"},
		{"less-than range", "pkg@mkt#<1.0"},
		{"equals range", "pkg@mkt#=1.0"},
		{"not-equals range", "pkg@mkt#!1.0"},
		{"compound range", "pkg@mkt#>=1.0 <2.0"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Arrange -- tt.in

			// Act
			plugin, mkt, ref, ok, err := ParseRef(tt.in)

			// Assert
			if err == nil {
				t.Fatalf("ParseRef(%q) returned no error; want a semver-range rejection", tt.in)
			}
			if !strings.Contains(err.Error(), "semver range") {
				t.Fatalf("ParseRef(%q) error = %q; want it to mention \"semver range\"", tt.in, err.Error())
			}
			if ok {
				t.Fatalf("ParseRef(%q) = ok=true alongside a non-nil error; want ok=false", tt.in)
			}
			if plugin != "" || mkt != "" || ref != "" {
				t.Fatalf("ParseRef(%q) error case returned non-empty fields: plugin=%q mkt=%q ref=%q", tt.in, plugin, mkt, ref)
			}
		})
	}
}
