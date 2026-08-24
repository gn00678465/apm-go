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
