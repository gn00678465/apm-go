//go:build !windows

package authoring

import "os"

// isJunctionOrUnknownReparsePoint is a no-op on non-Windows platforms:
// there is no Windows-junction-style reparse point that a POSIX symlink's
// own os.ModeSymlink flag does not already correctly identify, so
// isReparsePoint (refcheck.go) never needs this second signal here. path is
// accepted (and ignored) only to keep this stub's signature identical to
// reparse_windows.go's real implementation, which does need it (B-MINOR-1,
// external audit round 7, 2026-07-31 follow-up: reading a reparse point's
// tag requires opening it by name). See reparse_windows.go's
// isJunctionOrUnknownReparsePoint for the Windows-specific gap this stub has
// nothing to do on this platform.
func isJunctionOrUnknownReparsePoint(path string, fi os.FileInfo) bool {
	return false
}
