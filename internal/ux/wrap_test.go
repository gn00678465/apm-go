package ux

import (
	"os"
	"strings"
	"testing"
)

// marketplaceNotRegisteredMsgFor rebuilds cmd/apm-go's marketplaceNotRegisteredErr
// message shape without importing cmd/apm-go (a different module boundary),
// so these tests exercise wrapOracleText against the exact real, longer
// "apm-go" text apm-go actually prints -- not the pinned Oracle's shorter
// "apm" spelling.
func marketplaceNotRegisteredMsgFor(verb, name string) string {
	return "[x] Failed to " + verb + " marketplace: Marketplace '" + name + "' is not registered. " +
		"Run 'apm-go marketplace add https://github.com/OWNER/REPO' or " +
		"'apm-go marketplace add OWNER/REPO' to register it, or " +
		"'apm-go marketplace list' to see registered marketplaces."
}

// TestWrapOracleText_MatchesRealApmGoTextAt80Cols locks wrapOracleText's
// output for apm-go's REAL (never-folded, ticket 14 attempt 2 dropped
// oracleLogicalLen) message text at the Oracle's 80-cell fallback width.
// Ground truth was produced by running rich._wrap.divide_line (the pinned
// Oracle's own rich 15.0.0) directly against these exact "apm-go"-spelled
// strings -- not against the Oracle's own shorter "apm" bytes, which now
// legitimately wrap at different points (tracked by a field-precise stdout
// waiver on the two affected runner cases, not papered over here).
func TestWrapOracleText_MatchesRealApmGoTextAt80Cols(t *testing.T) {
	tests := []struct {
		name string
		in   string
		want string
	}{
		{
			name: "browse-unknown-marketplace error line",
			in:   marketplaceNotRegisteredMsgFor("browse", "nonexistent"),
			want: "[x] Failed to browse marketplace: Marketplace 'nonexistent' is not registered. \n" +
				"Run 'apm-go marketplace add https://github.com/OWNER/REPO' or 'apm-go \n" +
				"marketplace add OWNER/REPO' to register it, or 'apm-go marketplace list' to see \n" +
				"registered marketplaces.",
		},
		{
			name: "list-empty info line",
			in:   "[i] No marketplaces registered. Use 'apm-go marketplace add SOURCE' to register one (OWNER/REPO, HTTPS URL, SSH URL, or local path).",
			want: "[i] No marketplaces registered. Use 'apm-go marketplace add SOURCE' to register \n" +
				"one (OWNER/REPO, HTTPS URL, SSH URL, or local path).",
		},
		{
			name: "short message is returned unchanged",
			in:   "[x] short message",
			want: "[x] short message",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := wrapOracleText(tt.in, oracleFallbackWidth)
			if got != tt.want {
				t.Errorf("wrapOracleText() =\n%q\nwant\n%q", got, tt.want)
			}
		})
	}
}

// TestWrapOracleText_LongWordFoldsWithoutDataLoss is eval-ticket-14.md
// reproducer 2: a 120-character marketplace name is longer than the 80-cell
// width outright. Ground truth verified two ways: (a) live against the
// pinned Oracle (`marketplace browse` with the equivalent 120-'x' name,
// COLUMNS=80) confirms the Oracle folds the name across two lines via
// chop_cells rather than truncating it, and (b) rich._wrap.divide_line run
// directly on this exact "apm-go"-spelled string. The previous attempt's
// wrapOracleText took the `wordLength > width` branch without folding and
// silently dropped the remainder of the name -- this test would have caught
// that data-loss bug directly (every 'x' from the input must still be
// present in the output).
func TestWrapOracleText_LongWordFoldsWithoutDataLoss(t *testing.T) {
	longName := strings.Repeat("x", 120)
	in := marketplaceNotRegisteredMsgFor("browse", longName)

	got := wrapOracleText(in, oracleFallbackWidth)

	want := "[x] Failed to browse marketplace: Marketplace \n" +
		"'" + strings.Repeat("x", 79) + "\n" +
		strings.Repeat("x", 41) + "' is not registered. Run 'apm-go \n" +
		"marketplace add https://github.com/OWNER/REPO' or 'apm-go marketplace add \n" +
		"OWNER/REPO' to register it, or 'apm-go marketplace list' to see registered \n" +
		"marketplaces."
	if got != want {
		t.Errorf("wrapOracleText() =\n%q\nwant\n%q", got, want)
	}

	// "[x]" itself contributes one 'x' -- excluded so this counts only the
	// name's own characters.
	gotXCount := strings.Count(got, "x") - strings.Count(got, "[x]")
	if gotXCount != 120 {
		t.Errorf("wrapOracleText() lost data: got %d 'x' characters in output (excluding the \"[x]\" prefix), want all 120 preserved:\n%q", gotXCount, got)
	}
	for _, line := range strings.Split(got, "\n") {
		if n := oracleCellLen([]rune(line)); n > oracleFallbackWidth {
			t.Errorf("line %q is %d cells wide, want <= %d", line, n, oracleFallbackWidth)
		}
	}
}

// TestWrapOracleText_CJKCellWidth is eval-ticket-14.md reproducer 3: a name
// made of 20 repetitions of "市場" (40 CJK ideographs, 2 cells each = 80
// cells) is reachable via any unknown marketplace NAME argument. Ground
// truth verified both live against the pinned Oracle (COLUMNS=80) and via
// rich._wrap.divide_line run directly on this exact "apm-go"-spelled
// string: the Oracle counts each ideograph as 2 cells, folding 39 of them
// (78 cells) alongside the leading quote (79 cells total) before breaking,
// not all 40 on one line as apm-go's previous rune-count-only wrap did.
func TestWrapOracleText_CJKCellWidth(t *testing.T) {
	cjkName := strings.Repeat("市場", 20)
	in := marketplaceNotRegisteredMsgFor("browse", cjkName)

	got := wrapOracleText(in, oracleFallbackWidth)

	want := "[x] Failed to browse marketplace: Marketplace \n" +
		"'" + strings.Repeat("市場", 19) + "市" + "\n" +
		"場' is not registered. Run 'apm-go marketplace add \n" +
		"https://github.com/OWNER/REPO' or 'apm-go marketplace add OWNER/REPO' to \n" +
		"register it, or 'apm-go marketplace list' to see registered marketplaces."
	if got != want {
		t.Errorf("wrapOracleText() =\n%q\nwant\n%q", got, want)
	}
	if gotCount := strings.Count(got, "市") + strings.Count(got, "場"); gotCount != 40 {
		t.Errorf("wrapOracleText() lost CJK data: got %d ideographs, want 40:\n%q", gotCount, got)
	}
}

// TestWrapOracleText_HardNewlineResetsOffset is eval-ticket-14.md
// reproducer 4: a name containing a literal embedded newline ("one\ntwo")
// preserves that newline as a hard render boundary -- Rich's Text.wrap
// splits on "\n" and word-wraps each resulting line independently
// (rich/text.py:1201-1246's `for line in self.split(allow_blank=True)`),
// rather than treating "\n" as ordinary wrappable whitespace that carries
// the preceding line's cell offset across it. Ground truth verified live
// against the pinned Oracle and via divide_line run on each pre-split
// segment.
func TestWrapOracleText_HardNewlineResetsOffset(t *testing.T) {
	in := marketplaceNotRegisteredMsgFor("browse", "one\ntwo")

	got := wrapOracleText(in, oracleFallbackWidth)

	want := "[x] Failed to browse marketplace: Marketplace 'one\n" +
		"two' is not registered. Run 'apm-go marketplace add \n" +
		"https://github.com/OWNER/REPO' or 'apm-go marketplace add OWNER/REPO' to \n" +
		"register it, or 'apm-go marketplace list' to see registered marketplaces."
	if got != want {
		t.Errorf("wrapOracleText() =\n%q\nwant\n%q", got, want)
	}
}

// TestOracleConsoleWidth_HonorsColumns is eval-ticket-14.md reproducer 1:
// the pinned Oracle's Console.size only falls back to its literal 80
// default when COLUMNS is absent/invalid -- verified directly with
// COLUMNS=100 against the pinned Oracle (rich/console.py:979-985's
// is_dumb_terminal fallback never fires in the parity harness's piped,
// non-tty sandbox regardless of TERM=dumb, since is_terminal is always
// False there).
func TestOracleConsoleWidth_HonorsColumns(t *testing.T) {
	tests := []struct {
		name    string
		columns string
		unset   bool
		want    int
	}{
		{name: "unset falls back to 80", unset: true, want: 80},
		{name: "empty falls back to 80", columns: "", want: 80},
		{name: "valid digits honored", columns: "100", want: 100},
		{name: "non-digit falls back to 80", columns: "wide", want: 80},
		{name: "zero falls back to 80", columns: "0", want: 80},
		{name: "negative falls back to 80", columns: "-5", want: 80},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if tt.unset {
				t.Setenv("COLUMNS", "")
				os.Unsetenv("COLUMNS")
			} else {
				t.Setenv("COLUMNS", tt.columns)
			}
			if got := oracleConsoleWidth(); got != tt.want {
				t.Errorf("oracleConsoleWidth() = %d, want %d", got, tt.want)
			}
		})
	}
}

// TestWrapOracleText_HonorsColumns100 verifies wrapOracleText's own output
// at width=100 (as oracleLine would call it once oracleConsoleWidth reads
// COLUMNS=100), matching rich._wrap.divide_line run directly on this exact
// "apm-go"-spelled string at width 100 -- eval-ticket-14.md reproducer 1's
// downstream wrapping effect, not just the width-resolution function alone.
func TestWrapOracleText_HonorsColumns100(t *testing.T) {
	in := marketplaceNotRegisteredMsgFor("browse", "nonexistent")

	got := wrapOracleText(in, 100)

	want := "[x] Failed to browse marketplace: Marketplace 'nonexistent' is not registered. Run 'apm-go \n" +
		"marketplace add https://github.com/OWNER/REPO' or 'apm-go marketplace add OWNER/REPO' to register \n" +
		"it, or 'apm-go marketplace list' to see registered marketplaces."
	if got != want {
		t.Errorf("wrapOracleText() =\n%q\nwant\n%q", got, want)
	}
}
