package authoring

// This file has no build constraint (unlike reparse_windows.go/
// reparse_other.go): the bit arithmetic below has no OS dependency at all,
// and keeping it portable lets its own unit test (refcheck_test.go) run on
// every platform CI happens to use, rather than being silently skipped
// outside Windows -- see isNameSurrogateReparseTag's own doc comment
// (B-MINOR-1, external audit round 7, 2026-07-31 follow-up).

// reparseTagNameSurrogateBit is IO_REPARSE_TAG_NAME_SURROGATE's bit position
// within a Windows reparse tag, per winnt.h's own IsReparseTagNameSurrogate
// macro: `((tag) & 0x20000000)`. Microsoft's documented meaning: a reparse
// point whose tag has this bit set redirects path resolution to another
// name entirely (a symbolic link, or a directory junction/mount point) --
// the only kinds of reparse point that can ever make a path resolve
// somewhere other than where it lexically appears to. A reparse point
// carrying this bit UNSET (e.g. a OneDrive/Cloud Files placeholder, NTFS
// deduplication, or an AppExecLink) attaches alternate out-of-band data at
// the SAME name; it is not, by itself, a path-traversal concern.
const reparseTagNameSurrogateBit = 0x20000000

// isNameSurrogateReparseTag reports whether tag is a Microsoft "name
// surrogate" reparse tag.
//
// B-MINOR-1 (external audit round 7, 2026-07-31 follow-up): a prior version
// of isJunctionOrUnknownReparsePoint (reparse_windows.go) treated ANY file
// carrying Windows' FILE_ATTRIBUTE_REPARSE_POINT bit as something that must
// be resolved via os.Readlink, and treated any Readlink failure other than
// "does not exist" as fail-closed (rejected) further up the call chain
// (resolveReparsePointTarget, refcheck.go). A OneDrive/Cloud Files
// placeholder, an NTFS deduplication reparse point, or an AppExecLink
// (Windows Store app shortcut) all set that SAME attribute bit but have no
// "target path" in the symlink/junction sense at all -- os.Readlink cannot
// resolve them, so every ordinary file inside a project stored on a
// OneDrive-synced drive used to be rejected outright as an unresolvable
// reparse point: a false positive having nothing to do with path traversal.
// Reading the reparse point's own TAG (readReparseTag, reparse_windows.go,
// via FSCTL_GET_REPARSE_POINT) and checking Microsoft's own name-surrogate
// bit lets isJunctionOrUnknownReparsePoint tell these apart: only a genuine
// name surrogate is treated as something this package must resolve and
// contain-check; anything else is treated as an ordinary file/directory.
//
// Split out as this pure, portable function (rather than inlining the bit
// test directly in reparse_windows.go) specifically so it can be unit-tested
// directly against Microsoft's own documented tag VALUES (see
// TestIsNameSurrogateReparseTag in refcheck_test.go) without needing to
// construct a real OneDrive/Cloud Files placeholder on disk -- doing so
// would require a registered Cloud Files sync provider, not practical in a
// unit test. This does NOT, by itself, prove readReparseTag's live
// DeviceIoControl plumbing is wired correctly end to end against a real
// placeholder; that residual gap is documented on readReparseTag itself.
func isNameSurrogateReparseTag(tag uint32) bool {
	return tag&reparseTagNameSurrogateBit != 0
}
