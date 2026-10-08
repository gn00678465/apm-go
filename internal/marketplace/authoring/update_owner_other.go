//go:build !windows

package authoring

import (
	"os"
	"syscall"
)

// chownFile is a package-level var so a test can make the chown fail: an
// ordinary user cannot produce that failure on a file they own.
var chownFile = func(f *os.File, uid, gid int) error { return f.Chown(uid, gid) }

// keepOwner gives tmp the owner and group of the file orig describes. A
// temp file starts with the process's user and the group new files get in
// its directory, so a config file shared through another group would lose
// that group at the rename. Nothing is called when both already match.
func keepOwner(tmp *os.File, orig os.FileInfo) error {
	want := orig.Sys().(*syscall.Stat_t)
	var have syscall.Stat_t
	err := syscall.Fstat(int(tmp.Fd()), &have)
	if err == nil && (have.Uid != want.Uid || have.Gid != want.Gid) {
		err = chownFile(tmp, int(want.Uid), int(want.Gid))
	}
	return err
}
