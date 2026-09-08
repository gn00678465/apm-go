//go:build windows

package authoring

import (
	"errors"
	"os"
	"path/filepath"
	"syscall"
	"testing"
)

// TestStripExtendedLengthPrefix_UNC covers B-MAJOR-1's named regression
// (external audit round 9, 2026-07-31 follow-up): GetFinalPathNameByHandleW
// always returns a UNC target as `\\?\UNC\server\share`, and the ordinary
// spelling every other path in this package works with is
// `\\server\share` -- TWO leading backslashes. A prior version of this
// stripping logic prepended only one, producing the invalid `\server\share`.
// This is a pure-function unit test over the string transform itself (fake
// seam, no real UNC mount/share needed to exercise it).
func TestStripExtendedLengthPrefix_UNC(t *testing.T) {
	got := stripExtendedLengthPrefix(`\\?\UNC\server\share\sub`)
	want := `\\server\share\sub`
	if got != want {
		t.Errorf("stripExtendedLengthPrefix(%q) = %q, want %q", `\\?\UNC\server\share\sub`, got, want)
	}
}

// TestStripExtendedLengthPrefix_DriveLetter is the non-UNC sibling case:
// an ordinary drive-letter path's \\?\ prefix is stripped with no further
// UNC-specific rewriting.
func TestStripExtendedLengthPrefix_DriveLetter(t *testing.T) {
	got := stripExtendedLengthPrefix(`\\?\C:\Program Files\tool`)
	want := `C:\Program Files\tool`
	if got != want {
		t.Errorf("stripExtendedLengthPrefix(%q) = %q, want %q", `\\?\C:\Program Files\tool`, got, want)
	}
}

// query8dot3ShortName returns path's 8.3 short-name alias via
// syscall.GetShortPathName (standard library `syscall`, no new dependency),
// and ok=false when the volume has 8.3 short-name generation disabled (a
// common default on modern NTFS volumes for performance) -- detected by the
// returned string being identical to the input, since GetShortPathName
// itself does not otherwise report "disabled" as a distinct error.
func query8dot3ShortName(t *testing.T, path string) (short string, ok bool) {
	t.Helper()
	p, err := syscall.UTF16PtrFromString(path)
	if err != nil {
		t.Fatalf("UTF16PtrFromString(%q): %v", path, err)
	}
	buf := make([]uint16, 4096)
	n, err := syscall.GetShortPathName(p, &buf[0], uint32(len(buf)))
	if err != nil {
		return "", false
	}
	result := syscall.UTF16ToString(buf[:n])
	if result == path {
		return "", false
	}
	return result, true
}

// TestPathWithinRoot_Real8dot3ShortName_StillDetectedAsContained is
// B-MINOR-1's live, end-to-end repro (external audit round 8, 2026-07-31
// follow-up), independent of the fake-seam unit tests in canonicalize_test.go.
//
// root and target are BOTH given via root's own 8.3 short-name alias here --
// mirroring the realistic way this mismatch actually reaches pathWithinRoot
// in production: ResolveLocalSourceAgainstRoot's target is always
// filepath.Join(root, source), so root and target always share the exact
// same textual prefix within a single call (pathWithinRootLexical's raw
// pre-check, refcheck.go, would otherwise reject a target spelled
// differently from the root it was given alongside, before ever reaching
// real filesystem resolution -- that pre-check is a correct, necessary fast
// path, not a bug: an attacker-controlled target that AREN'T textually
// under the root string it was given is exactly what layer 1 exists to
// catch cheaply). The realistic risk is a process whose OWN root/cwd
// happens to be reported in 8.3 form (e.g. launched via a script or shortcut
// that passes a short path) -- both root and target then consistently carry
// that alias, and resolveRealPathJunctionAware/canonicalizeRealPathFn must
// still resolve them to the SAME real, long-form location as an ordinary
// call would, not treat the alias as if it names a different directory.
//
// Visibly t.Skip (not silently) when the temp volume has 8.3 short-name
// generation disabled -- a common modern default apm-go cannot control, and
// the one case in this whole B-MINOR-1 regression set that genuinely cannot
// be forced to run everywhere (unlike the directory-symlink-vs-junction
// fallback pattern elsewhere in this package, there is no privilege-free way
// to force 8.3 generation on for a single test's temp volume).
func TestPathWithinRoot_Real8dot3ShortName_StillDetectedAsContained(t *testing.T) {
	root := t.TempDir()
	if err := os.Mkdir(filepath.Join(root, "pkg"), 0o755); err != nil {
		t.Fatal(err)
	}

	shortRoot, ok := query8dot3ShortName(t, root)
	if !ok {
		t.Skip("SKIPPED: this volume has 8.3 short-name generation disabled (GetShortPathName returned the input unchanged) -- the 8.3-alias containment guard is untested by this run")
	}
	shortTarget := filepath.Join(shortRoot, "pkg")

	if !pathWithinRoot(shortRoot, shortTarget) {
		t.Errorf("pathWithinRoot(%q, %q) = false, want true: both are root's own 8.3 short-name alias plus its ordinary in-root child, the same physical directory tree pathWithinRoot(root, root+\"/pkg\") already accepts in long-name form", shortRoot, shortTarget)
	}
}

// ── B-MAJOR-1 (external audit round 9, 2026-07-31 follow-up): the
// ERROR_PATH_NOT_FOUND -> VOLUME_NAME_GUID fallback in canonicalizeRealPath
// (canonicalize_windows.go:84-98) had no test coverage at all -- exercising
// it live requires a volume mounted without an assigned DOS drive letter,
// which cannot be created portably or privilege-free for a single test's
// temp directory. getFinalPathNameByHandleFn (a swappable seam over the
// real Win32 call, added alongside these tests) lets these simulate both
// branches without one. ──

// TestCanonicalizeRealPath_FallsBackToVolumeGUID_OnErrorPathNotFound is the
// fallback's positive case: when the default (VOLUME_NAME_DOS) request
// fails with ERROR_PATH_NOT_FOUND, canonicalizeRealPath must retry with the
// VOLUME_NAME_GUID flag and return ITS result -- not the initial failure.
//
// The retry's flags value is checked against the literal 0x1 (not this
// package's own volumeNameGUIDFlag const): comparing against the same const
// the production code reads would make this test pass even if that const
// were mutated (e.g. to VOLUME_NAME_NT's 0x2), since both sides would then
// agree on the wrong value. Mutation test performed while writing this test
// (not left in the tree): changed volumeNameGUIDFlag from 0x1 to 0x2 --
// this test failed with "unexpected flags 0x2" (the fake seam's switch has
// no case for it), while every other test in this package's canonicalize
// suite stayed green, confirming this is the one regression test that
// actually pins the constant's value. Reverted immediately after
// (`git diff` against canonicalize_windows.go showed no changes once the
// getFinalPathNameByHandleFn seam edit itself was excluded).
func TestCanonicalizeRealPath_FallsBackToVolumeGUID_OnErrorPathNotFound(t *testing.T) {
	dir := t.TempDir()

	orig := getFinalPathNameByHandleFn
	t.Cleanup(func() { getFinalPathNameByHandleFn = orig })

	const fakeGUIDResult = `\\?\Volume{00000000-0000-0000-0000-000000000000}\sub`
	var flagsSeen []uintptr
	getFinalPathNameByHandleFn = func(h syscall.Handle, flags uintptr) (string, error) {
		flagsSeen = append(flagsSeen, flags)
		switch flags {
		case 0: // FILE_NAME_NORMALIZED (0x0) | VOLUME_NAME_DOS (0x0): the default request
			return "", syscall.ERROR_PATH_NOT_FOUND
		case 1: // VOLUME_NAME_GUID, per winbase.h -- a literal, not volumeNameGUIDFlag
			return fakeGUIDResult, nil
		default:
			t.Fatalf("unexpected flags %#x passed to getFinalPathNameByHandleFn (want 0 on the first call, then literal 0x1 -- VOLUME_NAME_GUID -- on retry)", flags)
			return "", nil
		}
	}

	got, err := canonicalizeRealPath(dir)
	if err != nil {
		t.Fatalf("canonicalizeRealPath(%q) error = %v, want nil (the VOLUME_NAME_GUID fallback should have succeeded)", dir, err)
	}
	want := stripExtendedLengthPrefix(fakeGUIDResult)
	if got != want {
		t.Errorf("canonicalizeRealPath(%q) = %q, want %q (the VOLUME_NAME_GUID fallback's own result, prefix-stripped)", dir, got, want)
	}
	if len(flagsSeen) != 2 {
		t.Fatalf("getFinalPathNameByHandleFn called %d time(s), want exactly 2 (the default-flags attempt, then the VOLUME_NAME_GUID retry)", len(flagsSeen))
	}
	if flagsSeen[1] != 1 {
		t.Errorf("retry call flags = %#x, want %#x (VOLUME_NAME_GUID)", flagsSeen[1], 1)
	}
}

// TestCanonicalizeRealPath_NonPathNotFoundError_NoFallbackRetry is the
// fallback's negative case: a GetFinalPathNameByHandleW failure that is NOT
// ERROR_PATH_NOT_FOUND (e.g. the handle itself somehow became invalid) must
// fail closed immediately, with no VOLUME_NAME_GUID retry attempted at all --
// matching this function's existing convention of never guessing past an
// error it cannot positively explain.
func TestCanonicalizeRealPath_NonPathNotFoundError_NoFallbackRetry(t *testing.T) {
	dir := t.TempDir()

	orig := getFinalPathNameByHandleFn
	t.Cleanup(func() { getFinalPathNameByHandleFn = orig })

	injectedErr := syscall.Errno(6) // ERROR_INVALID_HANDLE, deliberately not ERROR_PATH_NOT_FOUND
	calls := 0
	getFinalPathNameByHandleFn = func(h syscall.Handle, flags uintptr) (string, error) {
		calls++
		return "", injectedErr
	}

	_, err := canonicalizeRealPath(dir)
	if err == nil {
		t.Fatal("canonicalizeRealPath returned a nil error, want the injected non-ERROR_PATH_NOT_FOUND failure to propagate (fail closed, no fallback)")
	}
	if !errors.Is(err, injectedErr) {
		t.Errorf("canonicalizeRealPath error = %v, want it to wrap the injected error %v", err, injectedErr)
	}
	if calls != 1 {
		t.Errorf("getFinalPathNameByHandleFn called %d time(s), want exactly 1 (no VOLUME_NAME_GUID retry for a non-ERROR_PATH_NOT_FOUND failure)", calls)
	}
}
