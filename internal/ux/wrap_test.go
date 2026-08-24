package ux

import (
	"strings"
	"testing"
)

// TestWrapOracleText_MatchesPinnedOracleBytes locks wrapOracleText's output
// for apm-go's actual (apm -> apm-go substituted) message text. Ground truth
// was produced by running rich._wrap.divide_line(msg, 80) (the pinned
// Oracle's own rich 15.0.0) with every "apm-go" folded to "apm" for the
// fit/crop decisions only (oracleLogicalLen's job) -- verified: applying the
// harness's own normalizeString (apm-go -> apm, tools/parity/normalize.go)
// to each "want" value below reproduces the Oracle's raw captured bytes for
// `marketplace browse nonexistent` / `marketplace list` (empty registry)
// exactly, which is the actual ticket-14 acceptance bar, not a re-derivation
// from reading wrap.go's own logic a second time.
func TestWrapOracleText_MatchesPinnedOracleBytes(t *testing.T) {
	tests := []struct {
		name string
		in   string
		want string
	}{
		{
			name: "browse-unknown-marketplace error line",
			in:   "[x] Failed to browse marketplace: Marketplace 'nonexistent' is not registered. Run 'apm-go marketplace add https://github.com/OWNER/REPO' or 'apm-go marketplace add OWNER/REPO' to register it, or 'apm-go marketplace list' to see registered marketplaces.",
			want: "[x] Failed to browse marketplace: Marketplace 'nonexistent' is not registered. \n" +
				"Run 'apm-go marketplace add https://github.com/OWNER/REPO' or 'apm-go marketplace add \n" +
				"OWNER/REPO' to register it, or 'apm-go marketplace list' to see registered \n" +
				"marketplaces.",
		},
		{
			name: "list-empty info line",
			in:   "[i] No marketplaces registered. Use 'apm-go marketplace add SOURCE' to register one (OWNER/REPO, HTTPS URL, SSH URL, or local path).",
			want: "[i] No marketplaces registered. Use 'apm-go marketplace add SOURCE' to register one\n" +
				"(OWNER/REPO, HTTPS URL, SSH URL, or local path).",
		},
		{
			name: "short message is returned unchanged",
			in:   "[x] short message",
			want: "[x] short message",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := wrapOracleText(tt.in, oracleWrapWidth)
			if got != tt.want {
				t.Errorf("wrapOracleText() =\n%q\nwant\n%q", got, tt.want)
			}
		})
	}
}

// TestWrapOracleText_FoldedToApmMatchesOracleRawBytes is the acceptance bar
// itself: folding "apm-go" -> "apm" in wrapOracleText's own output (the same
// substitution tools/parity/normalize.go's normalizeString applies before
// diffing) must reproduce the Oracle's raw captured bytes for
// browse-unknown-marketplace/list-empty exactly -- bytes read directly from
// /tmp/t14out/oracle/{browse-unknown-marketplace,list-empty}/stdout.bin
// during ticket 14's investigation, not re-typed from memory.
func TestWrapOracleText_FoldedToApmMatchesOracleRawBytes(t *testing.T) {
	tests := []struct {
		name       string
		apmGoInput string
		oracleWant string
	}{
		{
			name:       "browse-unknown-marketplace",
			apmGoInput: "[x] Failed to browse marketplace: Marketplace 'nonexistent' is not registered. Run 'apm-go marketplace add https://github.com/OWNER/REPO' or 'apm-go marketplace add OWNER/REPO' to register it, or 'apm-go marketplace list' to see registered marketplaces.",
			oracleWant: "[x] Failed to browse marketplace: Marketplace 'nonexistent' is not registered. \n" +
				"Run 'apm marketplace add https://github.com/OWNER/REPO' or 'apm marketplace add \n" +
				"OWNER/REPO' to register it, or 'apm marketplace list' to see registered \n" +
				"marketplaces.",
		},
		{
			name:       "list-empty",
			apmGoInput: "[i] No marketplaces registered. Use 'apm-go marketplace add SOURCE' to register one (OWNER/REPO, HTTPS URL, SSH URL, or local path).",
			oracleWant: "[i] No marketplaces registered. Use 'apm marketplace add SOURCE' to register one\n" +
				"(OWNER/REPO, HTTPS URL, SSH URL, or local path).",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			wrapped := wrapOracleText(tt.apmGoInput, oracleWrapWidth)
			folded := strings.ReplaceAll(wrapped, "apm-go", "apm")
			if folded != tt.oracleWant {
				t.Errorf("folded wrapOracleText() =\n%q\nwant (Oracle raw bytes)\n%q", folded, tt.oracleWant)
			}
		})
	}
}
