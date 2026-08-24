package main

import "strings"

// containsUnwrapped reports whether want appears in out once both sides'
// whitespace runs are collapsed to single spaces -- a Contains assertion
// that must tolerate the Oracle's console-width word-wrap inserting a "\n"
// wherever a space happened to fall (ticket 14's wrapOracleText, internal/
// ux/wrap.go's oracleLine hook), which several pre-existing assertions
// asserting a single contiguous substring were not written to expect.
func containsUnwrapped(out, want string) bool {
	return strings.Contains(joinFields(out), joinFields(want))
}

func joinFields(s string) string {
	return strings.Join(strings.Fields(s), " ")
}

// containsUnwrappedPath is containsUnwrapped's counterpart for asserting a
// literal file path appears in output: a path has no internal whitespace,
// so wrap.go's chop_cells folding (a path can exceed the console width as
// one unbroken token -- e.g. a t.TempDir() path embedding a long test name
// -- and gets folded mid-word, with no space inserted at all) can split it
// with a bare "\n" in the middle of a character run. containsUnwrapped's
// space-joining would insert a WRONG space at that exact point instead of
// nothing, permanently breaking the match; simply removing embedded
// newlines is correct here specifically because a path is guaranteed not
// to contain a legitimate literal newline of its own.
func containsUnwrappedPath(out, path string) bool {
	return strings.Contains(strings.ReplaceAll(out, "\n", ""), path)
}
