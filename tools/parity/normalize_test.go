//go:build unix

package main

import "testing"

// TestNormalizeString_OnlyTempPathChanges is the acceptance-mandated
// reproducer: a string containing command wording, field order, and an
// exit code sitting right next to a temp path must come out with ONLY the
// path substituted -- nothing else rewritten, reordered, or dropped.
func TestNormalizeString_OnlyTempPathChanges(t *testing.T) {
	cwd := "/tmp/apm-parity-abc123/cwd"
	input := `apm doctor --help exited 0 while cwd was ` + cwd + `/apm.yml`
	want := `apm doctor --help exited 0 while cwd was <TMP>/apm.yml`

	got := normalizeString(input, cwd, "", "", false)
	if got != want {
		t.Errorf("normalizeString =\n  %q\nwant\n  %q", got, want)
	}
}

func TestNormalizeString_CfgAndHomePaths(t *testing.T) {
	got := normalizeString("home=/tmp/x/home cfg=/tmp/x/config", "", "/tmp/x/config", "/tmp/x/home", false)
	want := "home=<HOME> cfg=<CFG>"
	if got != want {
		t.Errorf("normalizeString = %q, want %q", got, want)
	}
}

func TestNormalizeString_Duration(t *testing.T) {
	tests := []struct {
		in, want string
	}{
		{"took 12ms to run", "took <DUR> to run"},
		{"took 1.5s total", "took <DUR> total"},
		{"took 200ms.", "took <DUR>."},
		// "12seconds" must NOT match: no word boundary between "s" and "econds".
		{"in 12seconds flat", "in 12seconds flat"},
	}
	for _, tt := range tests {
		got := normalizeString(tt.in, "", "", "", false)
		if got != tt.want {
			t.Errorf("normalizeString(%q) = %q, want %q", tt.in, got, tt.want)
		}
	}
}

func TestNormalizeString_HexTokenBoundedByLength(t *testing.T) {
	tests := []struct {
		name, in, want string
	}{
		{"7 chars matches", "commit abcdef1 done", "commit <SHA> done"},
		{"40 chars matches", "sha " + fortyHex + " end", "sha <SHA> end"},
		{"6 chars too short", "id abcdef done", "id abcdef done"},
		{"41 chars too long, left untouched entirely", "sha " + fortyHex + "0 end", "sha " + fortyHex + "0 end"},
	}
	for _, tt := range tests {
		got := normalizeString(tt.in, "", "", "", false)
		if got != tt.want {
			t.Errorf("%s: normalizeString(%q) = %q, want %q", tt.name, tt.in, got, tt.want)
		}
	}
}

const fortyHex = "0123456789abcdef0123456789abcdef01234567"

func TestNormalizeString_HexRunNotSplitByPartialMatch(t *testing.T) {
	// "deadbeef" (8 hex chars) is a maximal run bounded by "x" and "y",
	// neither of which is hex -- the whole run replaces as one token.
	got := normalizeString("xdeadbeefy", "", "", "", false)
	want := "x<SHA>y"
	if got != want {
		t.Errorf("normalizeString = %q, want %q", got, want)
	}
}

func TestNormalizeString_Timestamp(t *testing.T) {
	tests := []struct{ in, want string }{
		{"at 2026-08-23T10:15:30Z done", "at <TS> done"},
		{"at 2026-08-23T10:15:30.123456+00:00 done", "at <TS> done"},
	}
	for _, tt := range tests {
		got := normalizeString(tt.in, "", "", "", false)
		if got != tt.want {
			t.Errorf("normalizeString(%q) = %q, want %q", tt.in, got, tt.want)
		}
	}
}

func TestNormalizeString_BinaryNameRewriteOnlyWhenFlagSet(t *testing.T) {
	in := "usage: apm-go doctor [OPTIONS]"

	if got := normalizeString(in, "", "", "", false); got != in {
		t.Errorf("rewriteBinaryName=false must not touch the string: got %q", got)
	}

	got := normalizeString(in, "", "", "", true)
	want := "usage: apm doctor [OPTIONS]"
	if got != want {
		t.Errorf("normalizeString(rewrite=true) = %q, want %q", got, want)
	}
}

func TestSandboxCwdFromHome(t *testing.T) {
	if got := sandboxCwdFromHome("/tmp/apm-parity-xyz/home"); got != "/tmp/apm-parity-xyz/cwd" {
		t.Errorf("sandboxCwdFromHome = %q, want %q", got, "/tmp/apm-parity-xyz/cwd")
	}
	if got := sandboxCwdFromHome(""); got != "" {
		t.Errorf("sandboxCwdFromHome(\"\") = %q, want empty", got)
	}
}

// TestSandboxPathsFromEnvDelta_ExtractsAllThree proves diffCase can derive
// a run's normalization paths purely from EnvDelta (acceptance: normalize
// "applied to a COPY; raw records untouched" -- diffCase never mutates a
// Record at all; it only reads EnvDelta here and the raw stdout.bin/
// stderr.bin bytes separately from disk).
func TestSandboxPathsFromEnvDelta_ExtractsAllThree(t *testing.T) {
	cwd, cfg, home := sandboxPathsFromEnvDelta(map[string]string{
		"HOME":           "/tmp/apm-parity-xyz/home",
		"APM_CONFIG_DIR": "/tmp/apm-parity-xyz/config",
	})
	if cwd != "/tmp/apm-parity-xyz/cwd" {
		t.Errorf("cwd = %q, want %q", cwd, "/tmp/apm-parity-xyz/cwd")
	}
	if cfg != "/tmp/apm-parity-xyz/config" {
		t.Errorf("cfg = %q, want %q", cfg, "/tmp/apm-parity-xyz/config")
	}
	if home != "/tmp/apm-parity-xyz/home" {
		t.Errorf("home = %q, want %q", home, "/tmp/apm-parity-xyz/home")
	}
}
