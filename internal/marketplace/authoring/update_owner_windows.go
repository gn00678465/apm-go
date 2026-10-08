//go:build windows

package authoring

import "os"

// keepOwner does nothing: a Windows file has no uid or gid to carry over.
func keepOwner(*os.File, os.FileInfo) error { return nil }
