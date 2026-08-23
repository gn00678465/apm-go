//go:build unix

package main

import (
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

	return s
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
