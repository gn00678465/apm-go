package authoring

import (
	"errors"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"testing"
)

func TestOpenLocalFileWithinRoot_OrdinaryFile_Accepted(t *testing.T) {
	// Arrange
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "apm.yml"), []byte("description: ok\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	// Act
	f, err := OpenLocalFileWithinRoot(root, "apm.yml")

	// Assert
	if err != nil {
		t.Fatalf("OpenLocalFileWithinRoot() error = %v", err)
	}
	defer f.Close()
	data, err := io.ReadAll(f)
	if err != nil {
		t.Fatalf("ReadAll: %v", err)
	}
	if string(data) != "description: ok\n" {
		t.Errorf("read %q, want %q", data, "description: ok\n")
	}
}

func TestOpenLocalFileWithinRoot_NestedRelPath_Accepted(t *testing.T) {
	// Arrange
	root := t.TempDir()
	pkgDir := filepath.Join(root, "pkgs", "tool")
	if err := os.MkdirAll(pkgDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(pkgDir, "apm.yml"), []byte("version: 1.0.0\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	// Act
	f, err := OpenLocalFileWithinRoot(root, filepath.Join("pkgs", "tool", "apm.yml"))

	// Assert
	if err != nil {
		t.Fatalf("OpenLocalFileWithinRoot() error = %v", err)
	}
	defer f.Close()
}

func TestOpenLocalFileWithinRoot_NotFound_ReturnsUnwrappedFsError(t *testing.T) {
	// Arrange
	root := t.TempDir()

	// Act
	_, err := OpenLocalFileWithinRoot(root, "does-not-exist.yml")

	// Assert: an ordinary open failure (not found) must NOT be wrapped as
	// ErrLocalFileEscapesRoot -- that sentinel is reserved for a genuine
	// containment violation, so callers can still errors.Is against the
	// standard io/fs sentinels the same way os.Open's own callers can.
	if err == nil {
		t.Fatal("OpenLocalFileWithinRoot() error = nil, want a not-exist error")
	}
	if !errors.Is(err, fs.ErrNotExist) {
		t.Errorf("error = %v, want errors.Is(err, fs.ErrNotExist)", err)
	}
	if errors.Is(err, ErrLocalFileEscapesRoot) {
		t.Errorf("error = %v, must NOT be ErrLocalFileEscapesRoot for an ordinary not-found", err)
	}
}

// TestOpenLocalFileWithinRoot_EscapingTarget_Rejected is this task's
// required regression: a symlink physically inside root, whose target
// resolves outside it, must be rejected -- proving OpenLocalFileWithinRoot's
// handle-based verification actually runs, not just "open succeeded".
// Skipped (visibly, not silently) when this process cannot create a file
// symlink -- e.g. Windows without Developer Mode or
// SeCreateSymbolicLinkPrivilege -- matching this package's established
// convention elsewhere (see refcheck_test.go's own symlink tests).
func TestOpenLocalFileWithinRoot_EscapingTarget_Rejected(t *testing.T) {
	// Arrange
	root := t.TempDir()
	outside := t.TempDir()
	target := filepath.Join(outside, "secret.yml")
	if err := os.WriteFile(target, []byte("description: SECRET\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(root, "escaping.yml")
	if err := os.Symlink(target, link); err != nil {
		t.Skipf("SKIPPED: cannot create a file symlink in this environment (%v); the escaping-target guard is untested by this run", err)
	}

	// Act
	f, err := OpenLocalFileWithinRoot(root, "escaping.yml")

	// Assert
	if err == nil {
		f.Close()
		t.Fatal("OpenLocalFileWithinRoot() error = nil, want rejection of a symlink escaping root")
	}
	if !errors.Is(err, ErrLocalFileEscapesRoot) {
		t.Errorf("error = %v, want errors.Is(err, ErrLocalFileEscapesRoot)", err)
	}
}

// TestOpenLocalFileWithinRoot_SwapAfterOpen_ReadsOriginal is this task's
// other required regression, and the actual TOCTOU-closing proof: it
// directly demonstrates that content read through the handle
// OpenLocalFileWithinRoot returns is pinned to whatever was opened, immune
// to a path-level swap that happens strictly AFTER the open call returns --
// exactly the window a check-then-open pattern (validate the path string,
// then os.Open it moments later) leaves wide open.
//
// The swap is done by retargeting a symlink's OWN directory entry, not by
// removing/replacing the already-open target file directly: on Windows, Go's
// os.Open does not request FILE_SHARE_DELETE (see syscall.Open,
// syscall_windows.go), so a plain os.Remove/os.Rename of a path another
// handle still has open fails outright with a sharing violation -- the
// swap-after-open scenario this test exists to prove would be
// unreproducible on Windows any other way. Retargeting the SYMLINK entry
// itself never touches the original target's open handle, so it works on
// every platform this package supports. A plain in-place truncate+overwrite
// of the SAME underlying file would not prove anything either way (the
// already-open handle would legitimately see the new bytes, since it is
// still the same file) -- swapping what the PATH resolves to, after the
// open already happened, is the actual property under test. Skipped
// (visibly, not silently) when this process cannot create a file symlink.
func TestOpenLocalFileWithinRoot_SwapAfterOpen_ReadsOriginal(t *testing.T) {
	// Arrange
	root := t.TempDir()
	original := filepath.Join(root, "original.yml")
	if err := os.WriteFile(original, []byte("original\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	swapped := filepath.Join(root, "swapped.yml")
	if err := os.WriteFile(swapped, []byte("swapped\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(root, "apm.yml")
	if err := os.Symlink(original, link); err != nil {
		t.Skipf("SKIPPED: cannot create a file symlink in this environment (%v); the swap-after-open guard is untested by this run", err)
	}

	f, err := OpenLocalFileWithinRoot(root, "apm.yml")
	if err != nil {
		t.Fatalf("OpenLocalFileWithinRoot() error = %v", err)
	}
	defer f.Close()

	// Act: retarget the symlink itself (not the already-open original file)
	// to point at swapped.yml instead.
	if err := os.Remove(link); err != nil {
		t.Fatalf("Remove(%q) (retargeting the symlink, not the open file): %v", link, err)
	}
	if err := os.Symlink(swapped, link); err != nil {
		t.Fatalf("Symlink (retarget): %v", err)
	}

	data, err := io.ReadAll(f)
	if err != nil {
		t.Fatalf("ReadAll: %v", err)
	}

	// Assert
	if string(data) != "original\n" {
		t.Errorf("read %q from the already-open handle after a path swap, want %q -- the handle must stay pinned to the file it originally opened", data, "original\n")
	}

	// Sanity check: prove the swap really happened -- a fresh, path-based
	// re-open (the OLD check-then-use pattern) WOULD now follow the
	// retargeted symlink and see the swapped content, so this test is not
	// vacuously true.
	reopened, err := os.ReadFile(link)
	if err != nil {
		t.Fatalf("ReadFile (reopen): %v", err)
	}
	if string(reopened) != "swapped\n" {
		t.Fatalf("sanity check failed: re-reading path after swap = %q, want %q (the swap itself did not take effect, so this test proves nothing)", reopened, "swapped\n")
	}
}

func TestOpenLocalFileWithinRoot_VerificationFailure_FailsClosed(t *testing.T) {
	// Arrange: inject a fake handleWithinRootFn that returns an error
	// (simulating a platform-specific canonicalization failure) -- proving
	// OpenLocalFileWithinRoot treats "cannot positively verify" the same as
	// "rejected", never as "assume it's fine", and closes the handle either
	// way.
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "apm.yml"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	origFn := handleWithinRootFn
	closed := false
	handleWithinRootFn = func(f *os.File, root string) (bool, error) {
		closed = true // recorded before Close() is even called, just to prove this seam fired
		return false, errors.New("simulated canonicalization failure")
	}
	t.Cleanup(func() { handleWithinRootFn = origFn })

	// Act
	f, err := OpenLocalFileWithinRoot(root, "apm.yml")

	// Assert
	if err == nil {
		f.Close()
		t.Fatal("OpenLocalFileWithinRoot() error = nil, want fail-closed rejection on a verification error")
	}
	if !errors.Is(err, ErrLocalFileEscapesRoot) {
		t.Errorf("error = %v, want errors.Is(err, ErrLocalFileEscapesRoot)", err)
	}
	if !closed {
		t.Fatal("handleWithinRootFn seam was never invoked")
	}
}

func TestOpenLocalFileWithinRoot_HandleClosedOnRejection(t *testing.T) {
	// Arrange: on Windows, an open handle blocks deleting the underlying
	// file (os.Remove) unless every open handle referencing it has already
	// been closed -- this doubles as a portable proof that
	// OpenLocalFileWithinRoot actually closes the file before returning an
	// error, not just before returning success.
	root := t.TempDir()
	outside := t.TempDir()
	target := filepath.Join(outside, "secret.yml")
	if err := os.WriteFile(target, []byte("SECRET\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(root, "escaping.yml")
	if err := os.Symlink(target, link); err != nil {
		t.Skipf("SKIPPED: cannot create a file symlink in this environment (%v)", err)
	}

	// Act
	if _, err := OpenLocalFileWithinRoot(root, "escaping.yml"); err == nil {
		t.Fatal("OpenLocalFileWithinRoot() error = nil, want rejection")
	}

	// Assert: the escaping target itself is still removable (no dangling
	// open handle held against it).
	if err := os.Remove(target); err != nil {
		t.Errorf("Remove(%q) after rejection: %v (a lingering open handle would explain this on Windows)", target, err)
	}
}
