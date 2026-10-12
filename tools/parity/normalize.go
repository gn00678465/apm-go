//go:build unix

package main

import (
	"bytes"
	"path/filepath"
	"regexp"
	"strings"
)

// durationRe matches a bare duration token such as "12ms" or "1.5s" --
// acceptance: `\b\d+(\.\d+)?(ms|s)\b`.
var durationRe = regexp.MustCompile(`\b\d+(\.\d+)?(ms|s)\b`)

// hexRunRe finds MAXIMAL contiguous runs of hex characters. A run is only
// ever replaced when its own length is in [7,40] (see replaceHexTokens):
// matching the maximal run first, rather than `[0-9a-fA-F]{7,40}` directly,
// is what makes "bounded by non-hex" hold even for a run longer than 40 --
// a naive {7,40} match would consume the first 40 chars of a 45-char run
// and leave 5 more hex chars sitting right next to the substitution.
var hexRunRe = regexp.MustCompile(`[0-9a-fA-F]+`)

// isoTimestampRe matches an ISO-8601 timestamp with an optional fractional
// second and an optional "Z" or numeric offset.
var isoTimestampRe = regexp.MustCompile(`\d{4}-\d{2}-\d{2}T\d{2}:\d{2}:\d{2}(\.\d+)?(Z|[+-]\d{2}:?\d{2})?`)

// parityStatusGlyphs is the sanctioned F0x normalization class (ticket 33).
// The Oracle prefixes a status record with "[c] "; apm-go uses the
// centered width-3 form " c ". Both are reduced to a canonical token only at
// the start of a line, so bracketed or spaced text inside a message remains
// literal. Oracle [*] is its success/sparkles class; apm-go's centered * is
// the list class, while Oracle [#] is the corresponding list class.
var parityStatusGlyphs = []string{"+", "i", "!", "x", ">", "*", "#", "~", "-", "="}

var parityCanonicalStatusGlyphs = []string{"+", "i", "!", "x", ">", "*", "~", "-", "="}

// normalizeString applies ONLY the substitutions the ticket 02 spec allows,
// in an order chosen so each pass can't corrupt the next: the sandbox's own
// absolute paths first (a temp dir name can itself look like a hex or
// numeric token), then timestamps (contain digit runs that would otherwise
// be partially eaten by the hex pass), then durations, then hex tokens, and
// finally the binary-name rewrite (which is not a redaction and must not
// interact with any of the above). cwd/cfg/home are the run's own sandbox
// paths -- an empty string is a no-op ReplaceAll, so callers that don't
// have one of these (e.g. cwd derived from an absent HOME) can pass "".
func normalizeString(s, cwd, cfg, home string, rewriteBinaryName bool) string {
	if cwd != "" {
		s = strings.ReplaceAll(s, cwd, "<TMP>")
	}
	if cfg != "" {
		s = strings.ReplaceAll(s, cfg, "<CFG>")
	}
	if home != "" {
		s = strings.ReplaceAll(s, home, "<HOME>")
	}

	s = isoTimestampRe.ReplaceAllString(s, "<TS>")
	s = durationRe.ReplaceAllString(s, "<DUR>")
	s = replaceHexTokens(s)

	if rewriteBinaryName {
		s = strings.ReplaceAll(s, "apm-go", "apm")
	}

	s = normalizeStatusGlyphs(s)

	return s
}

// normalizeStatusGlyphs maps the two stream-renderer shapes to one canonical
// token. The canonical form keeps the message gap after <c>, independent of
// the source form's bracket or centered padding.
func normalizeStatusGlyphs(s string) string {
	lines := strings.Split(s, "\n")
	for i, line := range lines {
		lines[i] = canonicalNestedStatusGlyphLine(canonicalStatusGlyphLine(line))
	}
	return strings.Join(lines, "\n")
}

// nestedStatusGlyphPrefixes are the two places a status symbol is printed
// away from column 0: an indented row of a summary block, and a row under
// the "|" bar of a tree section.
var nestedStatusGlyphPrefixes = []string{"  ", "|  "}

// nestedStatusGlyphs is the set of symbols recognized there (issue #50
// ruling): the Oracle's "[*]" is the project's success class, as at column 0.
var nestedStatusGlyphs = map[string]string{"+": "+", ">": ">", "*": "+", "i": "i", "!": "!", "x": "x"}

// canonicalNestedStatusGlyphLine maps the Oracle's "<prefix>[c] " and
// apm-go's "<prefix> c " to "<prefix><c> ". It is the same difference of
// symbol shape canonicalStatusGlyphLine removes at column 0, and nothing
// wider: only these prefixes, only these symbols, and a different symbol or
// any other text stays a difference. It is kept apart from
// canonicalStatusGlyphLine so the error-body and wrap-folding rules, which
// treat only a column-0 symbol as the start of a record, do not change.
func canonicalNestedStatusGlyphLine(line string) string {
	for _, prefix := range nestedStatusGlyphPrefixes {
		rest, ok := strings.CutPrefix(line, prefix)
		if !ok || len(rest) < 4 {
			continue
		}
		if rest[0] == '[' && rest[2] == ']' && rest[3] == ' ' {
			if canonical, ok := nestedStatusGlyphs[rest[1:2]]; ok {
				return prefix + "<" + canonical + "> " + rest[4:]
			}
		}
		if rest[0] == ' ' && rest[2] == ' ' {
			if _, ok := nestedStatusGlyphs[rest[1:2]]; ok {
				return prefix + "<" + rest[1:2] + "> " + rest[3:]
			}
		}
	}
	return line
}

// canonicalStatusGlyphLine recognizes only the exact line-leading forms.
// In particular, a mid-line "[i]" and a line beginning with two spaces are
// not status records and must pass through unchanged.
func canonicalStatusGlyphLine(line string) string {
	for _, glyph := range parityStatusGlyphs {
		oracle := "[" + glyph + "] "
		oracleGlyph := glyph
		switch glyph {
		case "*":
			// Rich's sparkles/gear status is the project's success class.
			oracleGlyph = "+"
		case "#":
			// Rich's list status is the project's list class.
			oracleGlyph = "*"
		}
		oracleCanonical := "<" + oracleGlyph + "> "
		if strings.HasPrefix(line, oracle) {
			return oracleCanonical + line[len(oracle):]
		}
		tui := " " + glyph + " "
		tuiGlyph := glyph
		if glyph == "#" {
			tuiGlyph = "*"
		}
		tuiCanonical := "<" + tuiGlyph + "> "
		if strings.HasPrefix(line, tui) {
			return tuiCanonical + line[len(tui):]
		}
		for _, canonicalGlyph := range parityCanonicalStatusGlyphs {
			if strings.HasPrefix(line, "<"+canonicalGlyph+"> ") {
				return line
			}
		}
	}
	return line
}

func isCanonicalStatusGlyphLine(line string) bool {
	for _, glyph := range parityCanonicalStatusGlyphs {
		if strings.HasPrefix(line, "<"+glyph+"> ") {
			return true
		}
	}
	return false
}

// replaceHexTokens replaces every maximal run of hex characters whose
// length is between 7 and 40 (acceptance: "7-40 hex-char tokens bounded by
// non-hex") with "<SHA>". Runs shorter than 7 or longer than 40 are left
// untouched entirely, not partially collapsed.
func replaceHexTokens(s string) string {
	return hexRunRe.ReplaceAllStringFunc(s, func(run string) string {
		if len(run) >= 7 && len(run) <= 40 {
			return "<SHA>"
		}
		return run
	})
}

// sandboxCwdFromHome derives a run's sandbox cwd path from its recorded
// HOME value. sandbox.go lays cwd/home/config out as siblings under one
// MkdirTemp root (Cwd: root/cwd, Home: root/home, ConfigDir: root/config);
// HOME is always present in a Record's EnvDelta (env.go's buildEnv sets it
// unconditionally), so this needs no new Record field just to recover the
// one sandbox path EnvDelta doesn't already carry. Returns "" if home is
// empty (nothing to derive from).
func sandboxCwdFromHome(home string) string {
	if home == "" {
		return ""
	}
	return filepath.Join(filepath.Dir(home), "cwd")
}

// sandboxPathsFromEnvDelta extracts a run's own (cwd, config dir, home)
// sandbox paths from its Record.EnvDelta -- env.go's buildEnv always sets
// HOME and APM_CONFIG_DIR, and sandboxCwdFromHome recovers cwd from HOME
// without needing a separate Record field for it.
func sandboxPathsFromEnvDelta(envDelta map[string]string) (cwd, cfg, home string) {
	home = envDelta["HOME"]
	cfg = envDelta["APM_CONFIG_DIR"]
	cwd = sandboxCwdFromHome(home)
	return cwd, cfg, home
}

// lockfileTreePath is the one tree path normalizeTreeFileContent rewrites.
const lockfileTreePath = "cwd/apm.lock.yaml"

var (
	lockfileVersionKey  = []byte("apm_version:")
	lockfileVersionLine = []byte("apm_version: <APM_VERSION>")
)

// normalizeTreeFileContent is the ONLY normalisation keyed on a file's own
// path: every other tree file gets normalizeString alone, the same
// substitutions stdout and stderr get. Owner ruling R12 on issue #50: the
// lockfile records the version of the binary that wrote it (the Oracle's
// release number, "dev" for the gate-built apm-go), so the two sides can
// never agree on that line and it says nothing about the lockfile contract.
//
// Exactly one line changes: the first line of cwd/apm.lock.yaml that starts
// at column 0 with "apm_version:". Its line terminator is kept, so a missing
// trailing newline or a CRLF still differs. An indented apm_version key, a
// second top-level one, and a lockfile at any other path stay as written.
func normalizeTreeFileContent(path string, data []byte) []byte {
	if path != lockfileTreePath {
		return data
	}
	for start := 0; start < len(data); {
		end := bytes.IndexByte(data[start:], '\n')
		if end < 0 {
			end = len(data)
		} else {
			end += start
		}
		if bytes.HasPrefix(data[start:end], lockfileVersionKey) {
			tail := end
			if tail > start && data[tail-1] == '\r' {
				tail--
			}
			out := make([]byte, 0, len(data)+len(lockfileVersionLine))
			out = append(out, data[:start]...)
			out = append(out, lockfileVersionLine...)
			return append(out, data[tail:]...)
		}
		start = end + 1
	}
	return data
}
