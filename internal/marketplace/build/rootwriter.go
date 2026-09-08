package build

import (
	"encoding/hex"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"

	"crypto/rand"
)

// RootWriter confines every write to the directory it was opened on.
//
// It exists because EnsureWithinRoot cannot: that function resolves a path,
// checks it, and hands back a STRING. Every later os.MkdirAll / os.CreateTemp
// / os.Rename on that string walks the directory chain again, so a process
// that can write inside the project only has to swap an ancestor for a
// junction between the check and the write to redirect it. An external audit
// (2026-08-13) confirmed the window is reachable, and a local probe on
// go1.26.3/Windows reproduced it: against a directory replaced by a junction
// AFTER the handle was taken, os.WriteFile on the path string wrote outside
// the root, while every os.Root method refused with "path escapes from
// parent".
//
// os.OpenRoot pins a directory handle once; each method then resolves
// relative to that handle and refuses any component that leaves it, symlink
// and Windows junction alike. The window is closed by construction rather
// than narrowed.
//
// EnsureWithinRoot is still used for VALIDATION and for the paths shown to
// users (it fails closed on drive-relative sources, reports link cycles, and
// produces a resolved path worth printing). What it no longer does is decide
// where the bytes land.
type RootWriter struct {
	root *os.Root
	dir  string
}

// OpenRootWriter makes dir the confinement boundary, creating it when it does
// not exist yet.
//
// dir itself is trusted the same way EnsureWithinRoot's root argument always
// was: it comes from the command (the project root, an --output directory),
// not from the manifest data being packed. Everything passed to the methods
// below is untrusted and is confined to it.
func OpenRootWriter(dir string) (*RootWriter, error) {
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return nil, fmt.Errorf("create output root %q: %w", dir, err)
	}
	root, err := os.OpenRoot(dir)
	if err != nil {
		return nil, fmt.Errorf("open output root %q: %w", dir, err)
	}
	return &RootWriter{root: root, dir: dir}, nil
}

// Close releases the directory handle.
func (rw *RootWriter) Close() error {
	if rw == nil || rw.root == nil {
		return nil
	}
	return rw.root.Close()
}

// Rel converts p into the root-relative form os.Root requires. Callers may
// hold either form: EnsureWithinRoot deliberately accepts an ABSOLUTE output
// path that resolves inside the project, because --marketplace-path and an
// apm.yml override may both be written that way. Handing such a path straight
// to os.Root fails, so a writer that only accepted relative paths would refuse
// exactly what the containment gate had just approved.
//
// The conversion is done against rw.dir literally, not against a resolved
// path: the point of holding a handle is that no resolved string decides where
// the bytes land. It is a shape conversion, not a second containment check --
// EnsureWithinRoot remains the gate -- but it still fails closed on a path
// that lies outside the boundary rather than silently producing a "..".
func (rw *RootWriter) Rel(p string) (string, error) {
	if !filepath.IsAbs(p) {
		return filepath.Clean(p), nil
	}
	absDir, err := filepath.Abs(rw.dir)
	if err != nil {
		return "", fmt.Errorf("resolve output root %q: %w", rw.dir, err)
	}
	rel, err := filepath.Rel(absDir, filepath.Clean(p))
	if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return "", fmt.Errorf("path %q is outside %q", p, rw.dir)
	}
	return rel, nil
}

// Dir is the boundary directory, for messages.
func (rw *RootWriter) Dir() string { return rw.dir }

// Path renders rel as it should appear to a user. It is for display only --
// passing it back to os.* would reintroduce exactly the string-path walk this
// type exists to avoid.
func (rw *RootWriter) Path(rel string) string {
	return filepath.Join(rw.dir, filepath.FromSlash(rel))
}

// Sub narrows the boundary to rel, which must already be inside it. Used when
// a caller's containment base is a subdirectory that may not exist yet: the
// outer root creates it, the inner one confines writes to it.
func (rw *RootWriter) Sub(rel string) (*RootWriter, error) {
	clean := filepath.FromSlash(rel)
	if err := rw.root.MkdirAll(clean, 0o755); err != nil {
		return nil, fmt.Errorf("create %s: %w", rw.Path(rel), err)
	}
	sub, err := rw.root.OpenRoot(clean)
	if err != nil {
		return nil, fmt.Errorf("open %s: %w", rw.Path(rel), err)
	}
	return &RootWriter{root: sub, dir: rw.Path(rel)}, nil
}

// MkdirAll creates rel and its parents inside the boundary.
func (rw *RootWriter) MkdirAll(rel string) error {
	if rel == "" || rel == "." {
		return nil
	}
	if err := rw.root.MkdirAll(filepath.FromSlash(rel), 0o755); err != nil {
		return fmt.Errorf("create directory %s: %w", rw.Path(rel), err)
	}
	return nil
}

// RemoveAll deletes rel and everything under it, inside the boundary.
func (rw *RootWriter) RemoveAll(rel string) error {
	if err := rw.root.RemoveAll(filepath.FromSlash(rel)); err != nil {
		return fmt.Errorf("remove %s: %w", rw.Path(rel), err)
	}
	return nil
}

// Stat reports on rel inside the boundary.
func (rw *RootWriter) Stat(rel string) (os.FileInfo, error) {
	return rw.root.Stat(filepath.FromSlash(rel))
}

// ReadFile reads rel from inside the boundary.
func (rw *RootWriter) ReadFile(rel string) ([]byte, error) {
	return rw.root.ReadFile(filepath.FromSlash(rel))
}

// FS exposes the boundary as an fs.FS, for walks. Reads go through the same
// handle as writes, so a walk cannot be steered outside by a link planted
// mid-walk -- which matters when the walk's result is a hash manifest that
// later gets treated as authoritative.
func (rw *RootWriter) FS() fs.FS {
	return rw.root.FS()
}

// WriteFile creates rel's parent directories and writes data, truncating an
// existing file in place.
//
// Callers that must not write through a hard link planted at rel want
// WriteFileAtomic instead: truncating in place writes through every name the
// inode has, which no path check can see.
func (rw *RootWriter) WriteFile(rel string, data []byte, mode os.FileMode) error {
	if err := rw.MkdirAll(parentOf(rel)); err != nil {
		return err
	}
	if err := rw.root.WriteFile(filepath.FromSlash(rel), data, mode); err != nil {
		return fmt.Errorf("write %s: %w", rw.Path(rel), err)
	}
	return nil
}

// WriteFileAtomic writes data to rel through a temp file in the same
// directory plus a rename, creating rel's parents first.
//
// Two properties, both load-bearing:
//
//   - The rename replaces a directory entry instead of truncating an inode,
//     so a hard link planted at rel keeps its own contents. Lstat cannot tell
//     a hard link from an ordinary file, so no amount of path checking
//     substitutes for this.
//   - A NEW file's mode is the one passed to open(2), which the kernel
//     narrows by the process umask -- the behaviour os.WriteFile(path, data,
//     0o644) always had. os.CreateTemp cannot be used for this: it hardcodes
//     0600 and ignores umask, so reproducing the mode afterwards takes an
//     explicit chmod, and that chmod is what silently widened permissions
//     past the caller's umask (external audit, 2026-08-13). An EXISTING file
//     keeps the mode it already has, which is also what os.WriteFile did --
//     it applies its mode argument only on create.
func (rw *RootWriter) WriteFileAtomic(rel string, data []byte) error {
	if err := rw.MkdirAll(parentOf(rel)); err != nil {
		return err
	}
	clean := filepath.FromSlash(rel)

	mode := os.FileMode(0o644)
	existing, replacing := os.FileMode(0), false
	if info, err := rw.root.Stat(clean); err == nil {
		existing, replacing = info.Mode(), true
		mode = existing.Perm()
	}

	tmpRel, f, err := rw.createTemp(parentOf(rel), mode)
	if err != nil {
		return fmt.Errorf("create temp file for %s: %w", rw.Path(rel), err)
	}
	// No-op once the rename below succeeds.
	defer func() { _ = rw.root.Remove(tmpRel) }()

	if replacing {
		// Restore the FULL mode, not just the permission bits: os.WriteFile,
		// which this replaced, never touched an existing file's mode at all,
		// so setuid/setgid/sticky survived an overwrite. Passing only Perm()
		// to open(2) silently drops them (external audit, 2026-08-13).
		//
		// Chmod on the open file rather than on the path: os.Root.Chmod is
		// documented as racy on Unix, and fchmod on a descriptor we already
		// hold has nothing to race against. It runs only when replacing --
		// on a NEW file it would undo the umask the open just applied.
		if err := f.Chmod(existing & (os.ModePerm | os.ModeSetuid | os.ModeSetgid | os.ModeSticky)); err != nil {
			f.Close()
			return fmt.Errorf("preserve mode of %s: %w", rw.Path(rel), err)
		}
	}

	if _, err := f.Write(data); err != nil {
		f.Close()
		return fmt.Errorf("write temp file for %s: %w", rw.Path(rel), err)
	}
	if err := f.Close(); err != nil {
		return fmt.Errorf("close temp file for %s: %w", rw.Path(rel), err)
	}
	if err := rw.root.Rename(tmpRel, clean); err != nil {
		return fmt.Errorf("write %s: %w", rw.Path(rel), err)
	}
	return nil
}

// CreateAtomic opens rel for STREAMING writes with the same rename-on-commit
// shape WriteFileAtomic gives a whole buffer. It exists for outputs whose size
// is not known in advance and should not be held in memory -- a plugin bundle
// archive, for one -- where os.Create through a path string would both leave
// the boundary and truncate whatever inode the name currently points at.
//
// The caller must Commit to publish the file. Close aborts an uncommitted
// write and removes the temp file, so `defer f.Close()` after a Commit is the
// correct pattern: Commit marks the file done and Close then does nothing.
//
// Unlike WriteFileAtomic this preserves only an existing file's PERMISSION
// bits, not setuid/setgid/sticky. The callers are fresh build outputs, and
// carrying those bits onto a streamed archive has no established meaning.
func (rw *RootWriter) CreateAtomic(rel string) (*AtomicFile, error) {
	if err := rw.MkdirAll(parentOf(rel)); err != nil {
		return nil, err
	}
	clean := filepath.FromSlash(rel)

	mode := os.FileMode(0o644)
	if info, err := rw.root.Stat(clean); err == nil {
		mode = info.Mode().Perm()
	}
	tmpRel, f, err := rw.createTemp(parentOf(rel), mode)
	if err != nil {
		return nil, fmt.Errorf("create temp file for %s: %w", rw.Path(rel), err)
	}
	return &AtomicFile{rw: rw, f: f, tmpRel: tmpRel, dest: clean, rel: rel}, nil
}

// AtomicFile is an in-progress CreateAtomic write. Nothing is visible at the
// destination until Commit.
type AtomicFile struct {
	rw     *RootWriter
	f      *os.File
	tmpRel string
	dest   string
	rel    string
	done   bool
}

// Write streams to the temp file.
func (a *AtomicFile) Write(p []byte) (int, error) { return a.f.Write(p) }

// Commit closes the temp file and renames it over the destination.
func (a *AtomicFile) Commit() error {
	if a.done {
		return nil
	}
	a.done = true
	if err := a.f.Close(); err != nil {
		_ = a.rw.root.Remove(a.tmpRel)
		return fmt.Errorf("close temp file for %s: %w", a.rw.Path(a.rel), err)
	}
	if err := a.rw.root.Rename(a.tmpRel, a.dest); err != nil {
		_ = a.rw.root.Remove(a.tmpRel)
		return fmt.Errorf("write %s: %w", a.rw.Path(a.rel), err)
	}
	return nil
}

// Close aborts an uncommitted write; after Commit it is a no-op.
func (a *AtomicFile) Close() error {
	if a.done {
		return nil
	}
	a.done = true
	_ = a.f.Close()
	return a.rw.root.Remove(a.tmpRel)
}

// createTemp opens a uniquely named file under dir (relative to the
// boundary) with O_EXCL, so a name an attacker guessed and pre-created is an
// error rather than a target. The name is random for the same reason
// os.CreateTemp's is; what differs is that the mode reaches open(2), which is
// what lets umask apply.
func (rw *RootWriter) createTemp(dir string, mode os.FileMode) (string, *os.File, error) {
	for attempt := 0; attempt < 1000; attempt++ {
		var buf [8]byte
		if _, err := rand.Read(buf[:]); err != nil {
			return "", nil, err
		}
		name := ".apm-" + hex.EncodeToString(buf[:]) + ".tmp"
		rel := name
		if dir != "" && dir != "." {
			rel = filepath.Join(filepath.FromSlash(dir), name)
		}
		f, err := rw.root.OpenFile(rel, os.O_RDWR|os.O_CREATE|os.O_EXCL, mode)
		if err == nil {
			return rel, f, nil
		}
		if !os.IsExist(err) {
			return "", nil, err
		}
	}
	return "", nil, fmt.Errorf("could not find an unused temp name in %s", rw.Path(dir))
}

// parentOf returns rel's directory in slash form, or "" when rel sits
// directly in the boundary.
func parentOf(rel string) string {
	slashed := filepath.ToSlash(rel)
	idx := strings.LastIndex(slashed, "/")
	if idx < 0 {
		return ""
	}
	return slashed[:idx]
}
