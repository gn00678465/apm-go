//go:build windows

package authoring

import "os"

// keepOwner does nothing: a Windows file has no uid or gid to carry over.
func keepOwner(*os.File, os.FileInfo) error { return nil }

// hasSecondHardLink is not checked on Windows: os.FileInfo carries no link
// count there.
func hasSecondHardLink(os.FileInfo) bool { return false }
