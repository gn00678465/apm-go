//go:build windows

package main

// rootOpenExtraFlags is ORed into os.O_RDONLY for every manifest open that
// goes through (*os.Root).OpenFile. Windows has no O_NONBLOCK and no FIFO
// type, so there is nothing to add here; the constant exists so both
// platforms feed the same call site (openRootFile in plugin_validate.go).
//
// Win32 does have an open-without-following capability --
// FILE_FLAG_OPEN_REPARSE_POINT, which os.Root's own Lstat equivalent uses
// (os/root_windows.go) -- and using it does not disqualify it the way the
// earlier version of this comment claimed: for an ordinary file it opens
// normally, and for a symlink it opens the link object itself rather than
// following it, which is one way to implement a no-follow policy. It is
// still not usable here, for a narrower reason -- opened that way, a
// symlink candidate yields the link's own (non-regular) metadata and no
// readable data stream for the target, and this call must read the
// manifest's actual bytes. There is no windows analogue of O_NOFOLLOW for a
// data-read open; a symlink swapped into the boundary after the pre-open
// Lstat can still be followed by this Open as long as its target stays
// inside the boundary -- os.Root's own escape check refuses a target
// outside it, and the Fstat + os.SameFile comparison performed after this
// Open returns is what still catches a substituted in-boundary target, on
// both platforms alike.
const rootOpenExtraFlags = 0
