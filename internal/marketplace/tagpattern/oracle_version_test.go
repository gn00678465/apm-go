package tagpattern

import "testing"

// SPEC marketplace-check-outdated-fixes SC-F11: IsOracleVersion accepts
// exactly what the Oracle's parse_semver accepts, minus decision D-b.
//
// Expected values were produced on 2026-09-15 by running the Oracle's own
// code with Python 3:
//
//	spec = importlib.util.spec_from_file_location("sv", "D:/Projects2/apm/src/apm_cli/marketplace/semver.py")
//	module = importlib.util.module_from_spec(spec); sys.modules["sv"] = module; spec.loader.exec_module(module)
//	module.parse_semver(s) is not None
//
// The Oracle checkout was 8c2e0d9c (v0.30.0); its semver.py is identical
// to the pinned b75a02b1. The last two rows are the D-b deviation: Python's
// \d matches Unicode digits and $ stops before a trailing newline, so the
// Oracle accepts both; apm-go does not.
func TestIsOracleVersion_Table(t *testing.T) {
	for _, tc := range []struct {
		in   string
		want bool
	}{
		{"1.2.3", true},
		{"1.2.3-rc.1", true},
		{"1.2.3+build.1", true},
		{"1.2.3-rc.1+b", true},
		{"01.2.3", true},
		{"1.2.3-01", true},
		{"1", false},
		{"1.2", false},
		{"v1.2.3", false},
		{"1.2.3.4", false},
		{"1.2.3-", false},
		{"1.2.3+", false},
		{"1.2.3-rc..1", false},
		{"١.٢.٣", false},
		{"1.2.3\n", false},
	} {
		if got := IsOracleVersion(tc.in); got != tc.want {
			t.Errorf("IsOracleVersion(%q) = %v, want %v", tc.in, got, tc.want)
		}
	}
}
