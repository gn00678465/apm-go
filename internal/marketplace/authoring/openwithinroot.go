// This file (openwithinroot.go) implements OpenLocalFileWithinRoot: a
// check-AFTER-open replacement for the check-then-open pattern
// metadata.go's local apm.yml enrichment (internal/marketplace/build) used
// to follow -- resolveLocalSourceAgainstRoot (refcheck.go) validates a PATH
// STRING, and the caller only opens/reads the file at that path moments
// later, via a completely separate os.Open call. That gap is a TOCTOU
// (time-of-check-to-time-of-use), not merely a defense-in-depth nicety:
// resolveLocalSourceAgainstRoot's own containment check (pathWithinRoot,
// refcheck.go) already re-runs a fresh, real-filesystem-aware check at every
// read site specifically because mkt-046 lets `add` reference a source
// before its directory exists on disk, so a path validated as "within root"
// at one instant can be swapped -- a symlink/junction retarget, or a
// delete+recreate at the same path -- to point somewhere else entirely
// before the subsequent os.Open ever runs. Re-running the containment check
// closes the window between two SEPARATE checks; it does nothing about the
// window between the LAST check and the actual open.
//
// OpenLocalFileWithinRoot closes that remaining window structurally: it
// opens the file FIRST, then verifies -- using the ALREADY-OPEN handle, not
// a second path-based lookup -- that what was actually opened resolves
// within root. No swap that happens after the open can change what a
// caller reads from the returned *os.File: the open handle's underlying
// file identity is fixed the moment os.OpenFile returns.
package authoring

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
)

// ErrLocalFileEscapesRoot is OpenLocalFileWithinRoot's sentinel for "the
// file that was actually opened does not resolve within root" -- including
// the fail-closed case where the platform-specific handle canonicalization
// itself could not positively verify the answer (this package's established
// convention throughout, see pathWithinRoot's own doc comment: "cannot
// positively verify" is treated identically to "rejected", never silently
// accepted). Callers must treat this distinctly from an ordinary
// os.OpenFile failure (not found, permission denied, etc., returned
// unwrapped so callers can still errors.Is against the standard io/fs
// sentinels): this is a security violation -- the on-disk target no longer
// matches what the caller's containment expectations required -- not
// "nothing here."
var ErrLocalFileEscapesRoot = errors.New("local file resolves outside the project root")

// OpenLocalFileWithinRoot opens relPath (joined against root) and, using
// the file's OWN already-open handle -- never a second, fresh path-based
// lookup -- verifies that the file actually opened genuinely resolves
// within root. See this file's own header comment for why "open first, then
// verify the handle" is required instead of "verify the path, then open",
// and canonicalFilePath (openwithinroot_windows.go/openwithinroot_linux.go/
// openwithinroot_darwin.go/openwithinroot_other.go) for the per-platform
// mechanism used to ask an open handle/descriptor "what do you actually
// point at."
//
// On any rejection (open failure, or a handle that resolves outside root)
// the file is closed before returning; callers never receive a handle they
// must remember to close on an error path.
func OpenLocalFileWithinRoot(root, relPath string) (*os.File, error) {
	absRoot, err := filepath.Abs(root)
	if err != nil {
		return nil, fmt.Errorf("resolve project root %q: %w", root, err)
	}
	path := filepath.Join(absRoot, relPath)

	f, err := os.OpenFile(path, os.O_RDONLY, 0)
	if err != nil {
		return nil, err
	}

	ok, verifyErr := handleWithinRootFn(f, absRoot)
	if verifyErr != nil {
		f.Close()
		return nil, fmt.Errorf("%w: verifying %q against project root %q: %v", ErrLocalFileEscapesRoot, path, absRoot, verifyErr)
	}
	if !ok {
		f.Close()
		return nil, fmt.Errorf("%w: %q (project root %q)", ErrLocalFileEscapesRoot, path, absRoot)
	}
	return f, nil
}

// handleWithinRootFn is handleWithinRoot behind a package-level var -- the
// same seam-injection pattern canonicalizeRealPathFn/osLstat (refcheck.go)
// already use in this package -- so tests can substitute a fake without
// depending on a real platform-specific handle-canonicalization API being
// exercised in the test environment.
var handleWithinRootFn = handleWithinRoot

// handleWithinRoot reports whether f's own resolved real path (via
// canonicalFilePathFn, the platform-specific "what does this open handle
// actually point at" primitive) is root or a descendant of it. root is
// resolved through filepath.EvalSymlinks first, for the same reason
// pathWithinRoot's own root-canonicalization does (refcheck.go): root may
// itself sit behind a symlink/mount without that having any bearing on
// whether f escapes it -- both sides must be compared in the same,
// symlink-resolved terms. Any EvalSymlinks failure on root fails closed
// (root itself could not be positively resolved), matching this whole
// package's established convention.
func handleWithinRoot(f *os.File, root string) (bool, error) {
	targetReal, err := canonicalFilePathFn(f)
	if err != nil {
		return false, err
	}
	rootReal, err := filepath.EvalSymlinks(root)
	if err != nil {
		return false, fmt.Errorf("resolve project root %q: %w", root, err)
	}
	return pathWithinRootLexical(rootReal, targetReal), nil
}

// canonicalFilePathFn is canonicalFilePath behind a package-level var, for
// the same test-seam reason as handleWithinRootFn above.
var canonicalFilePathFn = canonicalFilePath
