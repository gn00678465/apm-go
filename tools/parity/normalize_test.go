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

func TestNormalizeString_StatusGlyphsCanonicalizeOnlyAtLineStart(t *testing.T) {
	tests := []struct {
		name string
		in   string
		want string
	}{
		{"oracle bracket form", "[i] fetching", "<i> fetching"},
		{"oracle success sparkle form", "[*] completed", "<+> completed"},
		{"tui centered form", " i fetching", "<i> fetching"},
		{"all supported classes", "[+] ok\n i info\n ! warn\n x err\n > run\n * list\n[#] list\n[~] update\n - remove\n[=] equal", "<+> ok\n<i> info\n<!> warn\n<x> err\n<>> run\n<*> list\n<*> list\n<~> update\n<-> remove\n<=> equal"},
		{"midline bracket untouched", "prefix [i] inside", "prefix [i] inside"},
		{"bracket in message untouched", "[x] body contains [i] literally", "<x> body contains [i] literally"},
		{"non-width-three indent untouched", "  i indented", "  i indented"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := normalizeString(tt.in, "", "", "", false); got != tt.want {
				t.Errorf("normalizeString(%q) = %q, want %q", tt.in, got, tt.want)
			}
		})
	}
}

// A status symbol on an indented row or under a tree bar differs between the
// two sides in shape only ("[+]" and " + "). The rule is narrow: the same
// symbol in both shapes is equal, and a different symbol, a different prefix,
// or any other bracket or text is not.
func TestNormalizeString_NestedStatusGlyphs(t *testing.T) {
	equal := []struct{ name, oracle, target string }{
		{"tree row, progress", "|  [>]  demo (self-defined, http)", "|   >  demo (self-defined, http)"},
		{"tree row, success", "|  [+]  demo -> Claude (configured)", "|   +  demo -> Claude (configured)"},
		{"tree row, one space after", "|  [+] demo (already configured)", "|   + demo (already configured)"},
		{"indented row", "  [+] ./dep (local)", "   + ./dep (local)"},
		{"indented info", "  [i] note", "   i note"},
		{"indented warning", "  [!] careful", "   ! careful"},
		{"indented error", "  [x] failed", "   x failed"},
		{"oracle sparkle is success", "  [*] done", "   + done"},
	}
	for _, tt := range equal {
		t.Run("equal/"+tt.name, func(t *testing.T) {
			o := normalizeString(tt.oracle, "", "", "", false)
			g := normalizeString(tt.target, "", "", "", false)
			if o != g {
				t.Errorf("normalized %q = %q, %q = %q; want them equal", tt.oracle, o, tt.target, g)
			}
		})
	}

	different := []struct{ name, oracle, target string }{
		{"different symbol", "|  [+]  demo", "|   >  demo"},
		{"different text", "|  [+]  demo", "|   +  other"},
		{"different gap after the symbol", "|  [+]  demo", "|   + demo"},
		{"symbol outside the set", "  [~] demo", "   ~ demo"},
		{"other bracketed text", "  [a] demo", "   a demo"},
		{"word in brackets", "  [ok] demo", "   ok demo"},
		{"symbol later in the line", "name [+] demo", "name  +  demo"},
		{"other prefix", "    [+] demo", "     + demo"},
		{"bracket form against bare text", "  [+] demo", "  + demo"},
	}
	for _, tt := range different {
		t.Run("different/"+tt.name, func(t *testing.T) {
			o := normalizeString(tt.oracle, "", "", "", false)
			g := normalizeString(tt.target, "", "", "", false)
			if o == g {
				t.Errorf("normalized %q and %q are both %q; want them different", tt.oracle, tt.target, o)
			}
		})
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

func TestNormalizeTreeFileContent(t *testing.T) {
	tests := []struct {
		name string
		path string
		in   string
		want string
	}{
		{
			name: "lockfile version line is replaced, the rest kept",
			path: "cwd/apm.lock.yaml",
			in:   "lockfile_version: '1'\napm_version: 0.33.0\ndependencies: []\n",
			want: "lockfile_version: '1'\napm_version: <APM_VERSION>\ndependencies: []\n",
		},
		{
			name: "version line without a trailing newline keeps none",
			path: "cwd/apm.lock.yaml",
			in:   "a: 1\napm_version: dev",
			want: "a: 1\napm_version: <APM_VERSION>",
		},
		{
			name: "CRLF terminator is kept",
			path: "cwd/apm.lock.yaml",
			in:   "apm_version: dev\r\na: 1\r\n",
			want: "apm_version: <APM_VERSION>\r\na: 1\r\n",
		},
		{
			name: "indented key is not the top-level one",
			path: "cwd/apm.lock.yaml",
			in:   "pack:\n  apm_version: 0.33.0\n",
			want: "pack:\n  apm_version: 0.33.0\n",
		},
		{
			name: "only the first top-level line is replaced",
			path: "cwd/apm.lock.yaml",
			in:   "apm_version: 1\napm_version: 2\n",
			want: "apm_version: <APM_VERSION>\napm_version: 2\n",
		},
		{
			name: "a longer key is not the version key",
			path: "cwd/apm.lock.yaml",
			in:   "apm_versions: 0.33.0\n",
			want: "apm_versions: 0.33.0\n",
		},
		{
			name: "lockfile in a subdirectory is untouched",
			path: "cwd/sub/apm.lock.yaml",
			in:   "apm_version: 0.33.0\n",
			want: "apm_version: 0.33.0\n",
		},
		{
			name: "another file in cwd is untouched",
			path: "cwd/other.yaml",
			in:   "apm_version: 0.33.0\n",
			want: "apm_version: 0.33.0\n",
		},
		{
			name: "lockfile under HOME is untouched",
			path: "home/apm.lock.yaml",
			in:   "apm_version: 0.33.0\n",
			want: "apm_version: 0.33.0\n",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			in := []byte(tt.in)
			got := string(normalizeTreeFileContent(tt.path, in))
			if got != tt.want {
				t.Errorf("normalizeTreeFileContent(%q) = %q, want %q", tt.path, got, tt.want)
			}
			if string(in) != tt.in {
				t.Errorf("input was modified in place: %q", in)
			}
		})
	}
}
