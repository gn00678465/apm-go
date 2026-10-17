//go:build !windows

package deploy

// canSymlink is true: only Windows asks for a privilege to make a symlink.
func canSymlink() bool { return true }
