//go:build !windows

package authoring

// canonicalizeRealPath is a no-op on non-Windows: the 8.3-short-name/
// volume-GUID/UNC-loopback alias ambiguity B-MINOR-1 (external audit round 8,
// 2026-07-31 follow-up) addresses is Windows-specific -- POSIX filesystems
// have no equivalent "the same directory, several distinct path spellings"
// concept this package needs to canonicalize away.
func canonicalizeRealPath(path string) (string, error) {
	return path, nil
}
